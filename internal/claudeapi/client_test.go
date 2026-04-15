package claudeapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchUsageNormalizesFiveHourAndWeekly(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("anthropic-beta"); got != "oauth-2025-04-20" {
			t.Fatalf("unexpected anthropic-beta header: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"five_hour": {"utilization": 45.2, "resets_at": "2026-04-15T15:00:00Z"},
			"seven_day": {"utilization": 12.8, "resets_at": "2026-04-21T00:00:00Z"},
			"seven_day_sonnet": {"utilization": 5.1, "resets_at": "2026-04-21T00:00:00Z"}
		}`))
	}))
	defer server.Close()

	client := &Client{http: server.Client()}
	originalURL := defaultBaseURLForTest
	defaultBaseURLForTest = server.URL
	defer func() { defaultBaseURLForTest = originalURL }()

	usage, err := client.FetchUsage(context.Background(), "token", "max")
	if err != nil {
		t.Fatalf("FetchUsage returned error: %v", err)
	}

	if usage.PlanType != "max" {
		t.Fatalf("unexpected plan type: %q", usage.PlanType)
	}
	if len(usage.Windows) != 2 {
		t.Fatalf("expected 2 windows, got %d", len(usage.Windows))
	}
	if usage.Windows[0].Name != "five_hour" || usage.Windows[0].RemainingPercent != 54.8 {
		t.Fatalf("unexpected five_hour window: %+v", usage.Windows[0])
	}
	if usage.Windows[1].Name != "weekly" || usage.Windows[1].RemainingPercent != 87.2 {
		t.Fatalf("unexpected weekly window: %+v", usage.Windows[1])
	}
}

func TestFetchUsageReturnsRetryAfterOn429(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "120")
		http.Error(w, `{"error":{"type":"rate_limit_error"}}`, http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := &Client{http: server.Client()}
	originalURL := defaultBaseURLForTest
	defaultBaseURLForTest = server.URL
	defer func() { defaultBaseURLForTest = originalURL }()

	_, err := client.FetchUsage(context.Background(), "token", "pro")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
	var rlErr *RateLimitError
	if !errors.As(err, &rlErr) {
		t.Fatalf("expected RateLimitError, got %T", err)
	}
	if rlErr.RetryAfter != 120000000000 {
		t.Fatalf("unexpected retry after: %v", rlErr.RetryAfter)
	}
}
