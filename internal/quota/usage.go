package quota

import "time"

type SnapshotWindow struct {
	Slot             string
	Name             string
	UsedPercent      float64
	RemainingPercent float64
	WindowMinutes    int
	ResetsAt         *time.Time
}

type NormalizedUsage struct {
	PlanType       string
	CreditsBalance *float64
	Windows        []SnapshotWindow
	RawJSON        []byte
}
