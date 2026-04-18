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

var claudePricingPrefixes = []string{
	"anthropic/",
	"claude-3-5-",
	"claude-3-",
	"claude-",
	"openrouter/openai/",
}

type claudeEntry struct {
	Timestamp string   `json:"timestamp"`
	Type      string   `json:"type"`
	UUID      string   `json:"uuid"`
	RequestID string   `json:"requestId"`
	CostUSD   *float64 `json:"costUSD"`
	Message   *struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			InputTokens         int64  `json:"input_tokens"`
			OutputTokens        int64  `json:"output_tokens"`
			CacheCreationTokens int64  `json:"cache_creation_input_tokens"`
			CacheReadTokens     int64  `json:"cache_read_input_tokens"`
			Speed               string `json:"speed"`
		} `json:"usage"`
	} `json:"message"`
}

type claudeUsageEntry struct {
	timestamp    time.Time
	model        string
	speed        string
	inputTokens  int64
	cacheCreate  int64
	cacheRead    int64
	outputTokens int64
	totalTokens  int64
	costUSD      *float64
}

func CollectClaude(ctx context.Context, fetcher *pricing.Fetcher, capturedAt time.Time) ([]store.DailyUsageRow, error) {
	roots, err := claudeTranscriptRoots()
	if err != nil {
		return nil, err
	}
	if len(roots) == 0 {
		return nil, nil
	}

	entries := map[string]claudeUsageEntry{}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if d.IsDir() || !strings.HasSuffix(strings.ToLower(d.Name()), ".jsonl") {
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err := processClaudeFile(path, entries); err != nil {
				return nil
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	aggregates := map[string]*aggregate{}
	for _, entry := range entries {
		dayKey := dateKey(entry.timestamp)
		key := ProviderClaude + "::" + dayKey
		target := aggregates[key]
		if target == nil {
			target = &aggregate{UsageDate: dayKey, Provider: ProviderClaude}
			aggregates[key] = target
		}
		target.add(entry.inputTokens, entry.cacheCreate, entry.cacheRead, entry.outputTokens, entry.totalTokens)

		if entry.costUSD != nil {
			target.EstimatedCostUSD += *entry.costUSD
			continue
		}

		price, ok, err := fetcher.Lookup(ctx, entry.model, claudePricingPrefixes, nil)
		if err != nil || !ok {
			continue
		}
		target.EstimatedCostUSD += pricing.CalculateCost(pricing.TokenUsage{
			InputTokens:         entry.inputTokens,
			CacheCreationTokens: entry.cacheCreate,
			CacheReadTokens:     entry.cacheRead,
			OutputTokens:        entry.outputTokens,
		}, price, entry.speed)
	}

	return toRows(aggregates, capturedAt), nil
}

func processClaudeFile(path string, entries map[string]claudeUsageEntry) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)

	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var entry claudeEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.Type != "assistant" || entry.Message == nil || entry.Message.Usage == nil {
			continue
		}

		ts, err := parseTimestamp(entry.Timestamp)
		if err != nil {
			continue
		}

		key := strings.TrimSpace(entry.RequestID)
		if key == "" {
			key = strings.TrimSpace(entry.Message.ID)
		}
		if key == "" {
			key = strings.TrimSpace(entry.UUID)
		}
		if key == "" {
			key = fmt.Sprintf("%s:%d", path, lineNumber)
		}

		usage := entry.Message.Usage
		record := claudeUsageEntry{
			timestamp:    ts,
			model:        strings.TrimSpace(entry.Message.Model),
			speed:        strings.TrimSpace(usage.Speed),
			inputTokens:  usage.InputTokens,
			cacheCreate:  usage.CacheCreationTokens,
			cacheRead:    usage.CacheReadTokens,
			outputTokens: usage.OutputTokens,
			totalTokens:  usage.InputTokens + usage.CacheCreationTokens + usage.CacheReadTokens + usage.OutputTokens,
			costUSD:      entry.CostUSD,
		}
		if record.totalTokens == 0 {
			continue
		}

		existing, ok := entries[key]
		if !ok || shouldReplaceClaude(existing, record) {
			entries[key] = record
		}
	}

	return scanner.Err()
}

func shouldReplaceClaude(current, next claudeUsageEntry) bool {
	if next.outputTokens != current.outputTokens {
		return next.outputTokens > current.outputTokens
	}
	if current.costUSD == nil && next.costUSD != nil {
		return true
	}
	if current.model == "" && next.model != "" {
		return true
	}
	return false
}

func claudeTranscriptRoots() ([]string, error) {
	var bases []string
	if raw := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				bases = append(bases, part)
			}
		}
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home dir: %w", err)
		}
		bases = append(bases, filepath.Join(home, ".config", "claude"), filepath.Join(home, ".claude"))
	}

	seen := map[string]struct{}{}
	var roots []string
	for _, base := range bases {
		for _, name := range []string{"projects", "sessions"} {
			root := filepath.Join(base, name)
			info, err := os.Stat(root)
			if err != nil || !info.IsDir() {
				continue
			}
			if _, ok := seen[root]; ok {
				continue
			}
			seen[root] = struct{}{}
			roots = append(roots, root)
		}
	}
	return roots, nil
}
