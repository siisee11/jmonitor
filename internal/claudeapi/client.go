package claudeapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dev/jmonitor/internal/quota"
)

const defaultBaseURL = "https://api.anthropic.com/api/oauth/usage"

var defaultBaseURLForTest = defaultBaseURL

const oauthTokenURL = "https://console.anthropic.com/v1/oauth/token"
const oauthClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"

var ErrRateLimited = errors.New("claudeapi: rate limited")

type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return ErrRateLimited.Error()
}

func (e *RateLimitError) Is(target error) bool {
	return target == ErrRateLimited
}

type OAuthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

type Client struct {
	http *http.Client
}

type usageResponse map[string]*quotaEntry

type quotaEntry struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    *string  `json:"resets_at"`
	IsEnabled   *bool    `json:"is_enabled"`
}

func New() *Client {
	return &Client{
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) FetchUsage(ctx context.Context, accessToken, planType string) (quota.NormalizedUsage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, defaultBaseURLForTest, nil)
	if err != nil {
		return quota.NormalizedUsage{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("User-Agent", "claude-code/2.1.69")

	resp, err := c.http.Do(req)
	if err != nil {
		return quota.NormalizedUsage{}, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return quota.NormalizedUsage{}, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return quota.NormalizedUsage{}, &RateLimitError{RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"))}
	}
	if resp.StatusCode != http.StatusOK {
		return quota.NormalizedUsage{}, fmt.Errorf("usage api status %d", resp.StatusCode)
	}

	var raw usageResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return quota.NormalizedUsage{}, fmt.Errorf("decode usage response: %w", err)
	}

	normalized := quota.NormalizedUsage{
		PlanType: planType,
		RawJSON:  body,
	}
	if window := normalizeWindow("five_hour", "five_hour", 5*60, raw["five_hour"]); window != nil {
		normalized.Windows = append(normalized.Windows, *window)
	}
	if window := normalizeWindow("weekly", "weekly", 7*24*60, pickWeekly(raw)); window != nil {
		normalized.Windows = append(normalized.Windows, *window)
	}

	return normalized, nil
}

func RefreshToken(ctx context.Context, refreshToken string) (*OAuthTokenResponse, error) {
	payload, err := json.Marshal(map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"client_id":     oauthClientID,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal refresh request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauthTokenURL, strings.NewReader(string(payload)))
	if err != nil {
		return nil, fmt.Errorf("create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "claude-code/2.1.69")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute refresh request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return nil, fmt.Errorf("read refresh response: %w", err)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, &RateLimitError{RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"))}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("refresh api status %d", resp.StatusCode)
	}

	var tokenResp OAuthTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("decode refresh response: %w", err)
	}
	if tokenResp.AccessToken == "" || tokenResp.RefreshToken == "" {
		return nil, fmt.Errorf("refresh response missing rotated tokens")
	}
	return &tokenResp, nil
}

func pickWeekly(raw usageResponse) *quotaEntry {
	if entry := raw["seven_day"]; isEnabled(entry) {
		return entry
	}
	if entry := raw["seven_day_sonnet"]; isEnabled(entry) {
		return entry
	}
	return nil
}

func isEnabled(entry *quotaEntry) bool {
	if entry == nil || entry.Utilization == nil {
		return false
	}
	if entry.IsEnabled != nil && !*entry.IsEnabled {
		return false
	}
	return true
}

func normalizeWindow(slot, name string, windowMinutes int, entry *quotaEntry) *quota.SnapshotWindow {
	if !isEnabled(entry) {
		return nil
	}

	used := clampPercent(*entry.Utilization)
	remaining := 100 - used
	var resetsAt *time.Time
	if entry.ResetsAt != nil && *entry.ResetsAt != "" {
		if ts, err := time.Parse(time.RFC3339, *entry.ResetsAt); err == nil {
			utc := ts.UTC()
			resetsAt = &utc
		}
	}

	return &quota.SnapshotWindow{
		Slot:             slot,
		Name:             name,
		UsedPercent:      used,
		RemainingPercent: remaining,
		WindowMinutes:    windowMinutes,
		ResetsAt:         resetsAt,
	}
}

func clampPercent(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if ts, err := time.Parse(time.RFC1123, value); err == nil {
		delay := time.Until(ts)
		if delay > 0 {
			return delay
		}
	}
	return 0
}
