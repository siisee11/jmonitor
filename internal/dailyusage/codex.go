package dailyusage

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dev/jmonitor/internal/pricing"
	"github.com/dev/jmonitor/internal/store"
)

var codexPricingPrefixes = []string{"openai/", "azure/", "openrouter/openai/"}

var codexModelAliases = map[string]string{
	"gpt-5-codex":   "gpt-5",
	"gpt-5.3-codex": "gpt-5.2-codex",
}

const codexFallbackModel = "gpt-5"

type codexEnvelope struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

type codexPayload struct {
	Type string          `json:"type"`
	Info json.RawMessage `json:"info"`
}

type codexUsageInfo struct {
	LastTokenUsage  json.RawMessage `json:"last_token_usage"`
	TotalTokenUsage json.RawMessage `json:"total_token_usage"`
}

type codexRawUsage struct {
	InputTokens           int64 `json:"input_tokens"`
	CachedInputTokens     int64 `json:"cached_input_tokens"`
	CacheReadInputTokens  int64 `json:"cache_read_input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
	TotalTokens           int64 `json:"total_tokens"`
}

type codexUsage struct {
	InputTokens       int64
	CachedInputTokens int64
	OutputTokens      int64
	TotalTokens       int64
}

func CollectCodex(ctx context.Context, codexHome string, fetcher *pricing.Fetcher, capturedAt time.Time) ([]store.DailyUsageRow, error) {
	sessionsDir := filepath.Join(codexHome, "sessions")
	if _, err := os.Stat(sessionsDir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat codex sessions dir: %w", err)
	}

	aggregates := map[string]*aggregate{}
	modelUsage := map[string]map[string]pricing.TokenUsage{}

	err := filepath.WalkDir(sessionsDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(strings.ToLower(d.Name()), ".jsonl") {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := processCodexFile(path, aggregates, modelUsage); err != nil {
			return nil
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	for dayKey, perModel := range modelUsage {
		target := aggregates[ProviderCodex+"::"+dayKey]
		if target == nil {
			continue
		}
		for modelName, usage := range perModel {
			price, ok, err := fetcher.Lookup(ctx, modelName, codexPricingPrefixes, codexModelAliases)
			if err != nil || !ok {
				continue
			}
			target.EstimatedCostUSD += pricing.CalculateCost(usage, price, "")
		}
	}

	return toRows(aggregates, capturedAt), nil
}

func processCodexFile(path string, aggregates map[string]*aggregate, modelUsage map[string]map[string]pricing.TokenUsage) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)

	var previousTotals *codexRawUsage
	var currentModel string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var envelope codexEnvelope
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			continue
		}

		switch envelope.Type {
		case "turn_context":
			if model := extractModelFromRaw(envelope.Payload); model != "" {
				currentModel = model
			}
		case "event_msg":
			var payload codexPayload
			if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
				continue
			}
			if payload.Type != "token_count" {
				continue
			}

			var info codexUsageInfo
			if err := json.Unmarshal(payload.Info, &info); err != nil {
				continue
			}

			lastUsage := normalizeCodexUsage(info.LastTokenUsage)
			totalUsage := normalizeCodexUsage(info.TotalTokenUsage)

			rawUsage := lastUsage
			if rawUsage == nil && totalUsage != nil {
				rawUsage = subtractCodexUsage(totalUsage, previousTotals)
			}
			if totalUsage != nil {
				previousTotals = totalUsage
			} else if lastUsage != nil {
				previousTotals = lastUsage
			}
			if rawUsage == nil {
				continue
			}

			delta := convertCodexUsage(rawUsage)
			if delta.InputTokens == 0 && delta.CachedInputTokens == 0 && delta.OutputTokens == 0 {
				continue
			}

			model := extractModelFromRaw(envelope.Payload)
			if model == "" {
				model = currentModel
			}
			if model == "" {
				model = codexFallbackModel
			} else {
				currentModel = model
			}

			ts, err := parseTimestamp(envelope.Timestamp)
			if err != nil {
				continue
			}
			dayKey := dateKey(ts)

			key := ProviderCodex + "::" + dayKey
			target := aggregates[key]
			if target == nil {
				target = &aggregate{UsageDate: dayKey, Provider: ProviderCodex}
				aggregates[key] = target
			}
			target.add(delta.InputTokens, 0, delta.CachedInputTokens, delta.OutputTokens, delta.TotalTokens)

			perModel := modelUsage[dayKey]
			if perModel == nil {
				perModel = map[string]pricing.TokenUsage{}
				modelUsage[dayKey] = perModel
			}
			usage := perModel[model]
			usage.InputTokens += delta.InputTokens
			usage.CacheReadTokens += delta.CachedInputTokens
			usage.OutputTokens += delta.OutputTokens
			perModel[model] = usage
		}
	}

	return scanner.Err()
}

func normalizeCodexUsage(raw json.RawMessage) *codexRawUsage {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}

	var usage codexRawUsage
	if err := json.Unmarshal(raw, &usage); err != nil {
		return nil
	}
	if usage.CachedInputTokens == 0 && usage.CacheReadInputTokens > 0 {
		usage.CachedInputTokens = usage.CacheReadInputTokens
	}
	if usage.TotalTokens <= 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	return &usage
}

func subtractCodexUsage(current, previous *codexRawUsage) *codexRawUsage {
	if current == nil {
		return nil
	}

	var previousInput, previousCached, previousOutput, previousReasoning, previousTotal int64
	if previous != nil {
		previousInput = previous.InputTokens
		previousCached = previous.CachedInputTokens
		previousOutput = previous.OutputTokens
		previousReasoning = previous.ReasoningOutputTokens
		previousTotal = previous.TotalTokens
	}

	return &codexRawUsage{
		InputTokens:           maxInt64(current.InputTokens-previousInput, 0),
		CachedInputTokens:     maxInt64(current.CachedInputTokens-previousCached, 0),
		OutputTokens:          maxInt64(current.OutputTokens-previousOutput, 0),
		ReasoningOutputTokens: maxInt64(current.ReasoningOutputTokens-previousReasoning, 0),
		TotalTokens:           maxInt64(current.TotalTokens-previousTotal, 0),
	}
}

func convertCodexUsage(raw *codexRawUsage) codexUsage {
	if raw == nil {
		return codexUsage{}
	}
	cached := raw.CachedInputTokens
	if cached > raw.InputTokens {
		cached = raw.InputTokens
	}
	total := raw.TotalTokens
	if total <= 0 {
		total = raw.InputTokens + raw.OutputTokens
	}
	return codexUsage{
		InputTokens:       raw.InputTokens,
		CachedInputTokens: cached,
		OutputTokens:      raw.OutputTokens,
		TotalTokens:       total,
	}
}

func extractModelFromRaw(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	return extractModel(payload)
}
