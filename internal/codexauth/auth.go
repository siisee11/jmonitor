package codexauth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type AccountSnapshot struct {
	AccountKey       string
	ChatGPTAccountID string
	ChatGPTUserID    string
	Email            string
	Plan             string
	AccessToken      string
	RefreshToken     string
	IDToken          string
	LastRefresh      *time.Time
	SourcePath       string
}

type authFile struct {
	OpenAIAPIKey string `json:"OPENAI_API_KEY"`
	Tokens       struct {
		IDToken      string `json:"id_token"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		AccountID    string `json:"account_id"`
	} `json:"tokens"`
	LastRefresh string `json:"last_refresh"`
}

type jwtClaims struct {
	Email string `json:"email"`
	Auth  struct {
		ChatGPTAccountID string `json:"chatgpt_account_id"`
		ChatGPTUserID    string `json:"chatgpt_user_id"`
		UserID           string `json:"user_id"`
		ChatGPTPlanType  string `json:"chatgpt_plan_type"`
	} `json:"https://api.openai.com/auth"`
}

func DiscoverAccountSnapshots(codexHome string) ([]AccountSnapshot, error) {
	accountsDir := filepath.Join(codexHome, "accounts")
	entries, err := os.ReadDir(accountsDir)
	if err != nil {
		return nil, fmt.Errorf("read accounts dir: %w", err)
	}

	snapshots := make([]AccountSnapshot, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".auth.json") {
			continue
		}
		path := filepath.Join(accountsDir, entry.Name())
		snapshot, err := ParseAccountSnapshot(path)
		if err != nil {
			continue
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, nil
}

func ParseAccountSnapshot(path string) (AccountSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return AccountSnapshot{}, fmt.Errorf("read auth file: %w", err)
	}

	var auth authFile
	if err := json.Unmarshal(data, &auth); err != nil {
		return AccountSnapshot{}, fmt.Errorf("parse auth file: %w", err)
	}

	if auth.Tokens.AccessToken == "" || auth.Tokens.IDToken == "" || auth.Tokens.AccountID == "" {
		return AccountSnapshot{}, fmt.Errorf("missing required token fields")
	}

	claims, err := parseJWTClaims(auth.Tokens.IDToken)
	if err != nil {
		return AccountSnapshot{}, fmt.Errorf("parse id_token: %w", err)
	}

	userID := claims.Auth.ChatGPTUserID
	if userID == "" {
		userID = claims.Auth.UserID
	}
	if userID == "" {
		return AccountSnapshot{}, fmt.Errorf("missing chatgpt_user_id")
	}
	if claims.Auth.ChatGPTAccountID == "" {
		return AccountSnapshot{}, fmt.Errorf("missing jwt account id")
	}
	if claims.Auth.ChatGPTAccountID != auth.Tokens.AccountID {
		return AccountSnapshot{}, fmt.Errorf("account id mismatch")
	}

	var lastRefresh *time.Time
	if auth.LastRefresh != "" {
		if ts, err := time.Parse(time.RFC3339, auth.LastRefresh); err == nil {
			lastRefresh = &ts
		}
	}

	email := strings.ToLower(strings.TrimSpace(claims.Email))
	return AccountSnapshot{
		AccountKey:       userID + "::" + auth.Tokens.AccountID,
		ChatGPTAccountID: auth.Tokens.AccountID,
		ChatGPTUserID:    userID,
		Email:            email,
		Plan:             normalizePlan(claims.Auth.ChatGPTPlanType),
		AccessToken:      auth.Tokens.AccessToken,
		RefreshToken:     auth.Tokens.RefreshToken,
		IDToken:          auth.Tokens.IDToken,
		LastRefresh:      lastRefresh,
		SourcePath:       path,
	}, nil
}

func parseJWTClaims(token string) (jwtClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return jwtClaims{}, fmt.Errorf("invalid jwt")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return jwtClaims{}, fmt.Errorf("decode payload: %w", err)
	}

	var claims jwtClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return jwtClaims{}, fmt.Errorf("unmarshal claims: %w", err)
	}
	return claims, nil
}

func normalizePlan(plan string) string {
	switch strings.ToLower(strings.TrimSpace(plan)) {
	case "free":
		return "free"
	case "plus":
		return "plus"
	case "pro":
		return "pro"
	case "prolite":
		return "prolite"
	case "team":
		return "team"
	case "business":
		return "business"
	case "enterprise":
		return "enterprise"
	case "edu":
		return "edu"
	default:
		return strings.ToLower(strings.TrimSpace(plan))
	}
}
