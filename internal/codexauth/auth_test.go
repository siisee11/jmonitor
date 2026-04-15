package codexauth

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseAccountSnapshot(t *testing.T) {
	t.Parallel()

	const accountID = "acct_123"
	const userID = "user_456"
	token := testJWT(t, map[string]any{
		"email": "User@Example.com",
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": accountID,
			"chatgpt_user_id":    userID,
			"chatgpt_plan_type":  "Pro",
		},
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "sample.auth.json")
	payload := map[string]any{
		"tokens": map[string]any{
			"id_token":      token,
			"access_token":  "access-token",
			"refresh_token": "refresh-token",
			"account_id":    accountID,
		},
		"last_refresh": "2026-04-15T10:11:12Z",
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal auth payload: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}

	snapshot, err := ParseAccountSnapshot(path)
	if err != nil {
		t.Fatalf("ParseAccountSnapshot returned error: %v", err)
	}

	if snapshot.AccountKey != userID+"::"+accountID {
		t.Fatalf("unexpected account key: %q", snapshot.AccountKey)
	}
	if snapshot.ChatGPTAccountID != accountID {
		t.Fatalf("unexpected account id: %q", snapshot.ChatGPTAccountID)
	}
	if snapshot.ChatGPTUserID != userID {
		t.Fatalf("unexpected user id: %q", snapshot.ChatGPTUserID)
	}
	if snapshot.Email != "user@example.com" {
		t.Fatalf("unexpected email: %q", snapshot.Email)
	}
	if snapshot.Plan != "pro" {
		t.Fatalf("unexpected plan: %q", snapshot.Plan)
	}
	if snapshot.LastRefresh == nil {
		t.Fatal("expected last refresh to be set")
	}
}

func TestParseAccountSnapshotRejectsMismatchedAccount(t *testing.T) {
	t.Parallel()

	token := testJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "acct_from_jwt",
			"chatgpt_user_id":    "user_456",
		},
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "sample.auth.json")
	payload := map[string]any{
		"tokens": map[string]any{
			"id_token":     token,
			"access_token": "access-token",
			"account_id":   "acct_from_file",
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal auth payload: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}

	if _, err := ParseAccountSnapshot(path); err == nil {
		t.Fatal("expected account mismatch error")
	}
}

func testJWT(t *testing.T, claims map[string]any) string {
	t.Helper()

	header := mustJSON(t, map[string]any{"alg": "none", "typ": "JWT"})
	payload := mustJSON(t, claims)
	return base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload) + "."
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()

	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return data
}
