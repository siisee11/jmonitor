package dailyusage

import (
	"sort"
	"time"

	"github.com/dev/jmonitor/internal/store"
)

const (
	ProviderClaude = "claude"
	ProviderCodex  = "codex"
)

type aggregate struct {
	UsageDate        string
	Provider         string
	InputTokens      int64
	CacheCreation    int64
	CacheRead        int64
	OutputTokens     int64
	TotalTokens      int64
	RequestCount     int
	EstimatedCostUSD float64
}

func (a *aggregate) add(input, cacheCreation, cacheRead, output, total int64) {
	a.InputTokens += input
	a.CacheCreation += cacheCreation
	a.CacheRead += cacheRead
	a.OutputTokens += output
	a.TotalTokens += total
	a.RequestCount++
}

func sortRows(rows []store.DailyUsageRow) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].UsageDate == rows[j].UsageDate {
			return rows[i].Provider < rows[j].Provider
		}
		return rows[i].UsageDate > rows[j].UsageDate
	})
}

func toRows(aggregates map[string]*aggregate, capturedAt time.Time) []store.DailyUsageRow {
	rows := make([]store.DailyUsageRow, 0, len(aggregates))
	for _, item := range aggregates {
		rows = append(rows, store.DailyUsageRow{
			UsageDate:        item.UsageDate,
			Provider:         item.Provider,
			InputTokens:      item.InputTokens,
			CacheCreation:    item.CacheCreation,
			CacheRead:        item.CacheRead,
			OutputTokens:     item.OutputTokens,
			TotalTokens:      item.TotalTokens,
			RequestCount:     item.RequestCount,
			EstimatedCostUSD: item.EstimatedCostUSD,
			CapturedAt:       capturedAt,
		})
	}
	sortRows(rows)
	return rows
}

func dateKey(timestamp time.Time) string {
	return timestamp.In(time.Local).Format("2006-01-02")
}
