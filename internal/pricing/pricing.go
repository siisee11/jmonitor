package pricing

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	liteLLMPricingURL      = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"
	defaultTieredThreshold = int64(200_000)
)

type ModelPricing struct {
	InputCostPerToken              float64
	OutputCostPerToken             float64
	CacheCreationInputCostPerToken float64
	CacheReadInputCostPerToken     float64
	InputCostAbove200kPerToken     float64
	OutputCostAbove200kPerToken    float64
	CacheCreateAbove200kPerToken   float64
	CacheReadAbove200kPerToken     float64
	FastMultiplier                 float64
}

type TokenUsage struct {
	InputTokens         int64
	CacheCreationTokens int64
	CacheReadTokens     int64
	OutputTokens        int64
}

type datasetEntry struct {
	InputCostPerToken              float64 `json:"input_cost_per_token"`
	OutputCostPerToken             float64 `json:"output_cost_per_token"`
	CacheCreationInputCostPerToken float64 `json:"cache_creation_input_token_cost"`
	CacheReadInputCostPerToken     float64 `json:"cache_read_input_token_cost"`
	InputCostAbove200kPerToken     float64 `json:"input_cost_per_token_above_200k_tokens"`
	OutputCostAbove200kPerToken    float64 `json:"output_cost_per_token_above_200k_tokens"`
	CacheCreateAbove200kPerToken   float64 `json:"cache_creation_input_token_cost_above_200k_tokens"`
	CacheReadAbove200kPerToken     float64 `json:"cache_read_input_token_cost_above_200k_tokens"`
	ProviderSpecificEntry          struct {
		Fast float64 `json:"fast"`
	} `json:"provider_specific_entry"`
}

type Fetcher struct {
	http     *http.Client
	ttl      time.Duration
	mu       sync.Mutex
	cachedAt time.Time
	cached   map[string]ModelPricing
}

func New() *Fetcher {
	return &Fetcher{
		http: &http.Client{Timeout: 10 * time.Second},
		ttl:  6 * time.Hour,
	}
}

func (f *Fetcher) Lookup(ctx context.Context, model string, prefixes []string, aliases map[string]string) (ModelPricing, bool, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return ModelPricing{}, false, nil
	}
	if isFreeModel(model) {
		return ModelPricing{}, true, nil
	}

	pricing, err := f.load(ctx)
	if err != nil {
		return ModelPricing{}, false, err
	}

	candidates := matchingCandidates(model, prefixes, aliases)
	for _, candidate := range candidates {
		if value, ok := pricing[candidate]; ok {
			return value, true, nil
		}
	}

	lowerCandidates := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		lowerCandidates = append(lowerCandidates, strings.ToLower(candidate))
	}
	for key, value := range pricing {
		keyLower := strings.ToLower(key)
		for _, candidate := range lowerCandidates {
			if strings.Contains(keyLower, candidate) || strings.Contains(candidate, keyLower) {
				return value, true, nil
			}
		}
	}

	return ModelPricing{}, false, nil
}

func (f *Fetcher) load(ctx context.Context) (map[string]ModelPricing, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.cached) > 0 && time.Since(f.cachedAt) < f.ttl {
		return f.cached, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, liteLLMPricingURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create pricing request: %w", err)
	}

	resp, err := f.http.Do(req)
	if err != nil {
		if len(f.cached) > 0 {
			return f.cached, nil
		}
		return nil, fmt.Errorf("fetch pricing dataset: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if len(f.cached) > 0 {
			return f.cached, nil
		}
		return nil, fmt.Errorf("pricing dataset status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		if len(f.cached) > 0 {
			return f.cached, nil
		}
		return nil, fmt.Errorf("read pricing dataset: %w", err)
	}

	raw := map[string]datasetEntry{}
	if err := json.Unmarshal(body, &raw); err != nil {
		if len(f.cached) > 0 {
			return f.cached, nil
		}
		return nil, fmt.Errorf("decode pricing dataset: %w", err)
	}

	converted := make(map[string]ModelPricing, len(raw))
	for modelName, entry := range raw {
		converted[strings.TrimSpace(modelName)] = ModelPricing{
			InputCostPerToken:              entry.InputCostPerToken,
			OutputCostPerToken:             entry.OutputCostPerToken,
			CacheCreationInputCostPerToken: entry.CacheCreationInputCostPerToken,
			CacheReadInputCostPerToken:     entry.CacheReadInputCostPerToken,
			InputCostAbove200kPerToken:     entry.InputCostAbove200kPerToken,
			OutputCostAbove200kPerToken:    entry.OutputCostAbove200kPerToken,
			CacheCreateAbove200kPerToken:   entry.CacheCreateAbove200kPerToken,
			CacheReadAbove200kPerToken:     entry.CacheReadAbove200kPerToken,
			FastMultiplier:                 maxFloat(entry.ProviderSpecificEntry.Fast, 1),
		}
	}

	f.cached = converted
	f.cachedAt = time.Now()
	return f.cached, nil
}

func CalculateCost(usage TokenUsage, pricing ModelPricing, speed string) float64 {
	cost := tieredCost(usage.InputTokens, pricing.InputCostPerToken, pricing.InputCostAbove200kPerToken) +
		tieredCost(usage.OutputTokens, pricing.OutputCostPerToken, pricing.OutputCostAbove200kPerToken) +
		tieredCost(usage.CacheCreationTokens, pricing.CacheCreationInputCostPerToken, pricing.CacheCreateAbove200kPerToken) +
		tieredCost(usage.CacheReadTokens, pricing.CacheReadInputCostPerToken, pricing.CacheReadAbove200kPerToken)

	if strings.EqualFold(strings.TrimSpace(speed), "fast") && pricing.FastMultiplier > 0 {
		cost *= pricing.FastMultiplier
	}
	return cost
}

func tieredCost(tokens int64, basePrice, tieredPrice float64) float64 {
	if tokens <= 0 {
		return 0
	}

	if tieredPrice > 0 && tokens > defaultTieredThreshold {
		below := defaultTieredThreshold
		above := tokens - defaultTieredThreshold
		return (float64(below) * basePrice) + (float64(above) * tieredPrice)
	}

	return float64(tokens) * basePrice
}

func matchingCandidates(model string, prefixes []string, aliases map[string]string) []string {
	seen := map[string]struct{}{}
	var candidates []string

	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		candidates = append(candidates, value)
	}

	add(model)
	for _, prefix := range prefixes {
		add(prefix + model)
	}

	if alias := strings.TrimSpace(aliases[model]); alias != "" {
		add(alias)
		for _, prefix := range prefixes {
			add(prefix + alias)
		}
	}

	return candidates
}

func isFreeModel(model string) bool {
	normalized := strings.ToLower(strings.TrimSpace(model))
	if normalized == "openrouter/free" {
		return true
	}
	return strings.HasPrefix(normalized, "openrouter/") && strings.HasSuffix(normalized, ":free")
}

func maxFloat(value, fallback float64) float64 {
	if value > 0 {
		return value
	}
	return fallback
}
