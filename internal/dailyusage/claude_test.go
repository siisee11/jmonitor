package dailyusage

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dev/jmonitor/internal/pricing"
)

func TestCollectClaude(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "projects", "demo")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir projects: %v", err)
	}

	content := `{"timestamp":"2026-04-17T08:00:00Z","type":"assistant","requestId":"req-1","costUSD":0.015,"message":{"id":"msg-1","model":"claude-sonnet-4-20250514","usage":{"input_tokens":100,"cache_creation_input_tokens":20,"cache_read_input_tokens":10,"output_tokens":50}}}
{"timestamp":"2026-04-17T08:00:01Z","type":"assistant","requestId":"req-1","costUSD":0.02,"message":{"id":"msg-1","model":"claude-sonnet-4-20250514","usage":{"input_tokens":100,"cache_creation_input_tokens":20,"cache_read_input_tokens":10,"output_tokens":80}}}
{"timestamp":"2026-04-17T10:00:00Z","type":"assistant","requestId":"req-2","costUSD":0.03,"message":{"id":"msg-2","model":"claude-opus-4-20250514","usage":{"input_tokens":200,"cache_creation_input_tokens":0,"cache_read_input_tokens":40,"output_tokens":120}}}
`
	file := filepath.Join(projectDir, "session.jsonl")
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	t.Setenv("CLAUDE_CONFIG_DIR", root)

	rows, err := CollectClaude(context.Background(), pricing.New(), time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("collect claude: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}

	row := rows[0]
	if row.Provider != ProviderClaude {
		t.Fatalf("unexpected provider: %q", row.Provider)
	}
	if row.InputTokens != 300 {
		t.Fatalf("unexpected input tokens: %d", row.InputTokens)
	}
	if row.CacheCreation != 20 {
		t.Fatalf("unexpected cache creation tokens: %d", row.CacheCreation)
	}
	if row.CacheRead != 50 {
		t.Fatalf("unexpected cache read tokens: %d", row.CacheRead)
	}
	if row.OutputTokens != 200 {
		t.Fatalf("unexpected output tokens: %d", row.OutputTokens)
	}
	if row.TotalTokens != 570 {
		t.Fatalf("unexpected total tokens: %d", row.TotalTokens)
	}
	if row.RequestCount != 2 {
		t.Fatalf("unexpected request count: %d", row.RequestCount)
	}
	if math.Abs(row.EstimatedCostUSD-0.05) > 1e-9 {
		t.Fatalf("unexpected estimated cost: %f", row.EstimatedCostUSD)
	}
}
