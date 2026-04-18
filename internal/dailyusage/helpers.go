package dailyusage

import (
	"fmt"
	"strings"
	"time"
)

func parseTimestamp(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("missing timestamp")
	}
	return time.Parse(time.RFC3339Nano, value)
}

func extractModel(payload map[string]any) string {
	if payload == nil {
		return ""
	}

	if model := asNonEmptyString(payload["model"]); model != "" {
		return model
	}
	if model := asNonEmptyString(payload["model_name"]); model != "" {
		return model
	}

	if info, ok := payload["info"].(map[string]any); ok {
		if model := extractModel(info); model != "" {
			return model
		}
	}
	if metadata, ok := payload["metadata"].(map[string]any); ok {
		if model := extractModel(metadata); model != "" {
			return model
		}
	}

	return ""
}

func asNonEmptyString(value any) string {
	if typed, ok := value.(string); ok {
		return strings.TrimSpace(typed)
	}
	return ""
}

func maxInt64(value, minimum int64) int64 {
	if value < minimum {
		return minimum
	}
	return value
}
