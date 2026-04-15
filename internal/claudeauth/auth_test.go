package claudeauth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseAccountSnapshot(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, ".credentials.json")
	payload := map[string]any{
		"claudeAiOauth": map[string]any{
			"accessToken":      "claude-access-token",
			"refreshToken":     "claude-refresh-token",
			"subscriptionType": "Max",
			"rateLimitTier":    "team",
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal credentials payload: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write credentials file: %v", err)
	}

	snapshot, err := ParseAccountSnapshot(path)
	if err != nil {
		t.Fatalf("ParseAccountSnapshot returned error: %v", err)
	}

	if snapshot.AccountKey != defaultAccountKey {
		t.Fatalf("unexpected account key: %q", snapshot.AccountKey)
	}
	if snapshot.Email != "Claude Code" {
		t.Fatalf("unexpected email: %q", snapshot.Email)
	}
	if snapshot.Plan != "max" {
		t.Fatalf("unexpected plan: %q", snapshot.Plan)
	}
	if snapshot.AccessToken != "claude-access-token" {
		t.Fatalf("unexpected access token: %q", snapshot.AccessToken)
	}
}

func TestParseAccountSnapshotData(t *testing.T) {
	t.Parallel()

	snapshot, err := parseAccountSnapshotData([]byte(`{
		"claudeAiOauth": {
			"accessToken": "token",
			"refreshToken": "refresh",
			"rateLimitTier": "team"
		}
	}`), "env:test")
	if err != nil {
		t.Fatalf("parseAccountSnapshotData returned error: %v", err)
	}
	if snapshot.SourcePath != "env:test" {
		t.Fatalf("unexpected source path: %q", snapshot.SourcePath)
	}
	if snapshot.Plan != "team" {
		t.Fatalf("unexpected plan: %q", snapshot.Plan)
	}
}
