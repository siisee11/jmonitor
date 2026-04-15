package codexapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dev/jmonitor/internal/quota"
)

const defaultBaseURL = "https://chatgpt.com/backend-api/wham/usage"

type Client struct {
	http *http.Client
}

type UsageResponse struct {
	PlanType  string     `json:"plan_type"`
	RateLimit rateLimits `json:"rate_limit"`
	Credits   *credits   `json:"credits,omitempty"`
}

type rateLimits struct {
	PrimaryWindow   *window `json:"primary_window"`
	SecondaryWindow *window `json:"secondary_window"`
}

type window struct {
	UsedPercent        float64 `json:"used_percent"`
	ResetAt            int64   `json:"reset_at"`
	LimitWindowSeconds int64   `json:"limit_window_seconds"`
}

type credits struct {
	Balance json.Number `json:"balance,omitempty"`
}

func New() *Client {
	return &Client{
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) FetchUsage(ctx context.Context, accessToken, accountID string) (quota.NormalizedUsage, error) {
	body, err := c.fetch(ctx, defaultBaseURL, accessToken, accountID)
	if err != nil {
		return quota.NormalizedUsage{}, err
	}

	var resp UsageResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return quota.NormalizedUsage{}, fmt.Errorf("decode usage response: %w", err)
	}

	normalized := quota.NormalizedUsage{
		PlanType: resp.PlanType,
		RawJSON:  body,
	}
	if resp.Credits != nil {
		if balance, err := resp.Credits.Balance.Float64(); err == nil {
			normalized.CreditsBalance = &balance
		}
	}

	if resp.RateLimit.PrimaryWindow != nil {
		normalized.Windows = append(normalized.Windows, normalizeWindow(resp.PlanType, "primary", resp.RateLimit.PrimaryWindow, resp.RateLimit.SecondaryWindow != nil))
	}
	if resp.RateLimit.SecondaryWindow != nil {
		normalized.Windows = append(normalized.Windows, normalizeWindow(resp.PlanType, "secondary", resp.RateLimit.SecondaryWindow, true))
	}

	return normalized, nil
}

func (c *Client) fetch(ctx context.Context, endpoint, accessToken, accountID string) ([]byte, error) {
	body, status, err := c.doRequest(ctx, endpoint, accessToken, accountID)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		if fallback, ok := fallbackURL(endpoint); ok {
			return c.fetchDirect(ctx, fallback, accessToken, accountID)
		}
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("usage api status %d", status)
	}
	return body, nil
}

func (c *Client) fetchDirect(ctx context.Context, endpoint, accessToken, accountID string) ([]byte, error) {
	body, status, err := c.doRequest(ctx, endpoint, accessToken, accountID)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("usage api status %d", status)
	}
	return body, nil
}

func (c *Client) doRequest(ctx context.Context, endpoint, accessToken, accountID string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("ChatGPT-Account-Id", accountID)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}
	return body, resp.StatusCode, nil
}

func fallbackURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	switch {
	case strings.Contains(u.Path, "/backend-api/wham/usage"):
		u.Path = strings.Replace(u.Path, "/backend-api/wham/usage", "/api/codex/usage", 1)
	case strings.Contains(u.Path, "/api/codex/usage"):
		u.Path = strings.Replace(u.Path, "/api/codex/usage", "/backend-api/wham/usage", 1)
	default:
		return "", false
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), true
}

func normalizeWindow(planType, slot string, w *window, hasSecondary bool) quota.SnapshotWindow {
	name := "unknown"
	windowMinutes := int(w.LimitWindowSeconds / 60)
	if hasSecondary && slot == "primary" {
		name = "five_hour"
	} else if slot == "secondary" {
		name = "weekly"
	} else if windowMinutes >= 10080 || strings.EqualFold(planType, "free") {
		name = "weekly"
	} else if windowMinutes > 0 && windowMinutes <= 300 {
		name = "five_hour"
	}

	remaining := 100 - w.UsedPercent
	if remaining < 0 {
		remaining = 0
	}
	if remaining > 100 {
		remaining = 100
	}

	var resetsAt *time.Time
	if w.ResetAt > 0 {
		ts := time.Unix(w.ResetAt, 0).UTC()
		resetsAt = &ts
	}

	return quota.SnapshotWindow{
		Slot:             slot,
		Name:             name,
		UsedPercent:      w.UsedPercent,
		RemainingPercent: remaining,
		WindowMinutes:    windowMinutes,
		ResetsAt:         resetsAt,
	}
}
