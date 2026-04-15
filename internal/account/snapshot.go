package account

import "time"

type Snapshot struct {
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
