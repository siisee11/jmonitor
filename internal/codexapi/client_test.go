package codexapi

import "testing"

func TestNormalizeWindowNamesPrimaryAndSecondary(t *testing.T) {
	t.Parallel()

	primary := normalizeWindow("pro", "primary", &window{
		UsedPercent:        42.5,
		LimitWindowSeconds: 5 * 60 * 60,
	}, true)
	if primary.Name != "five_hour" {
		t.Fatalf("expected five_hour, got %q", primary.Name)
	}
	if primary.RemainingPercent != 57.5 {
		t.Fatalf("unexpected remaining percent: %v", primary.RemainingPercent)
	}

	secondary := normalizeWindow("pro", "secondary", &window{
		UsedPercent:        10,
		LimitWindowSeconds: 7 * 24 * 60 * 60,
	}, true)
	if secondary.Name != "weekly" {
		t.Fatalf("expected weekly, got %q", secondary.Name)
	}
}

func TestNormalizeWindowInfersWeeklyForFreePlan(t *testing.T) {
	t.Parallel()

	free := normalizeWindow("free", "primary", &window{
		UsedPercent:        120,
		LimitWindowSeconds: 7 * 24 * 60 * 60,
	}, false)
	if free.Name != "weekly" {
		t.Fatalf("expected weekly, got %q", free.Name)
	}
	if free.RemainingPercent != 0 {
		t.Fatalf("expected remaining percent clamp at 0, got %v", free.RemainingPercent)
	}
}

func TestFallbackURL(t *testing.T) {
	t.Parallel()

	got, ok := fallbackURL("https://chatgpt.com/backend-api/wham/usage")
	if !ok {
		t.Fatal("expected fallback url")
	}
	if got != "https://chatgpt.com/api/codex/usage" {
		t.Fatalf("unexpected fallback url: %q", got)
	}
}
