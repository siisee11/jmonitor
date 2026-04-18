package dailyusage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dev/jmonitor/internal/pricing"
)

func TestCollectCodex(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sessionDir := filepath.Join(root, "sessions", "2026", "04", "17")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}

	content := `{"timestamp":"2026-04-17T10:00:00Z","type":"turn_context","payload":{"model":"openrouter/free"}}
{"timestamp":"2026-04-17T10:00:01Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":1200,"cached_input_tokens":200,"output_tokens":500,"reasoning_output_tokens":0,"total_tokens":1700},"model":"openrouter/free"}}}
{"timestamp":"2026-04-17T12:00:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":2000,"cached_input_tokens":300,"output_tokens":900,"reasoning_output_tokens":0,"total_tokens":2900},"model":"openrouter/free"}}}
`
	file := filepath.Join(sessionDir, "rollout.jsonl")
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}

	rows, err := CollectCodex(context.Background(), root, pricing.New(), time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("collect codex: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}

	row := rows[0]
	if row.Provider != ProviderCodex {
		t.Fatalf("unexpected provider: %q", row.Provider)
	}
	if row.InputTokens != 2000 {
		t.Fatalf("unexpected input tokens: %d", row.InputTokens)
	}
	if row.CacheRead != 300 {
		t.Fatalf("unexpected cache read tokens: %d", row.CacheRead)
	}
	if row.OutputTokens != 900 {
		t.Fatalf("unexpected output tokens: %d", row.OutputTokens)
	}
	if row.TotalTokens != 2900 {
		t.Fatalf("unexpected total tokens: %d", row.TotalTokens)
	}
	if row.RequestCount != 2 {
		t.Fatalf("unexpected request count: %d", row.RequestCount)
	}
	if row.EstimatedCostUSD != 0 {
		t.Fatalf("expected free model cost to be 0, got %f", row.EstimatedCostUSD)
	}
}
