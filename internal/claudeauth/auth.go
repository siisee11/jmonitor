package claudeauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/dev/jmonitor/internal/account"
)

const defaultAccountKey = "claude::default"

type credentialsFile struct {
	ClaudeAiOauth struct {
		AccessToken      string `json:"accessToken"`
		RefreshToken     string `json:"refreshToken"`
		SubscriptionType string `json:"subscriptionType"`
		RateLimitTier    string `json:"rateLimitTier"`
	} `json:"claudeAiOauth"`
}

func DefaultCredentialsPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".claude", ".credentials.json")
}

func DiscoverAccountSnapshot() (*account.Snapshot, error) {
	if raw := strings.TrimSpace(os.Getenv("CLAUDE_CREDENTIALS_JSON")); raw != "" {
		snapshot, err := parseAccountSnapshotData([]byte(raw), "env:CLAUDE_CREDENTIALS_JSON")
		if err != nil {
			return nil, err
		}
		return &snapshot, nil
	}

	path := DefaultCredentialsPath()
	if path != "" {
		snapshot, err := ParseAccountSnapshot(path)
		if err == nil {
			return &snapshot, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}

	if runtime.GOOS == "darwin" {
		snapshot, err := discoverFromKeychain()
		if err != nil {
			return nil, err
		}
		if snapshot != nil {
			return snapshot, nil
		}
	}

	return nil, nil
}

func ParseAccountSnapshot(path string) (account.Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return account.Snapshot{}, fmt.Errorf("read claude credentials: %w", err)
	}
	return parseAccountSnapshotData(data, path)
}

func parseAccountSnapshotData(data []byte, sourcePath string) (account.Snapshot, error) {
	var creds credentialsFile
	if err := json.Unmarshal(data, &creds); err != nil {
		return account.Snapshot{}, fmt.Errorf("parse claude credentials: %w", err)
	}

	if strings.TrimSpace(creds.ClaudeAiOauth.AccessToken) == "" {
		return account.Snapshot{}, fmt.Errorf("missing claude access token")
	}

	return account.Snapshot{
		AccountKey:    defaultAccountKey,
		ChatGPTUserID: "claude",
		Email:         "Claude Code",
		Plan:          normalizePlan(creds.ClaudeAiOauth.SubscriptionType, creds.ClaudeAiOauth.RateLimitTier),
		AccessToken:   strings.TrimSpace(creds.ClaudeAiOauth.AccessToken),
		RefreshToken:  strings.TrimSpace(creds.ClaudeAiOauth.RefreshToken),
		SourcePath:    sourcePath,
	}, nil
}

func discoverFromKeychain() (*account.Snapshot, error) {
	user := strings.TrimSpace(os.Getenv("USER"))
	if user == "" {
		out, err := exec.Command("id", "-un").Output()
		if err == nil {
			user = strings.TrimSpace(string(out))
		}
	}
	if user == "" {
		return nil, nil
	}

	out, err := exec.Command("security", "find-generic-password", "-s", "Claude Code-credentials", "-a", user, "-w").Output()
	if err != nil {
		return nil, nil
	}

	snapshot, err := parseAccountSnapshotData(out, "keychain:Claude Code-credentials")
	if err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func normalizePlan(subscriptionType, rateLimitTier string) string {
	if plan := strings.ToLower(strings.TrimSpace(subscriptionType)); plan != "" {
		return plan
	}
	if tier := strings.ToLower(strings.TrimSpace(rateLimitTier)); tier != "" {
		return tier
	}
	return "claude"
}
