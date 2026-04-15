package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dev/jmonitor/internal/account"
	"github.com/dev/jmonitor/internal/quota"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

type Account struct {
	ID                int64      `json:"id"`
	AccountKey        string     `json:"accountKey"`
	ChatGPTAccountID  string     `json:"chatgptAccountId"`
	ChatGPTUserID     string     `json:"chatgptUserId"`
	Email             string     `json:"email"`
	Plan              string     `json:"plan"`
	SourcePath        string     `json:"sourcePath"`
	LastPolledAt      *time.Time `json:"lastPolledAt"`
	LastSuccessAt     *time.Time `json:"lastSuccessAt"`
	LastError         *string    `json:"lastError"`
	LastErrorAt       *time.Time `json:"lastErrorAt"`
	FiveHourRemaining *float64   `json:"fiveHourRemaining"`
	FiveHourUsed      *float64   `json:"fiveHourUsed"`
	FiveHourResetsAt  *time.Time `json:"fiveHourResetsAt"`
	WeeklyRemaining   *float64   `json:"weeklyRemaining"`
	WeeklyUsed        *float64   `json:"weeklyUsed"`
	WeeklyResetsAt    *time.Time `json:"weeklyResetsAt"`
}

type HistoryPoint struct {
	CapturedAt       time.Time  `json:"capturedAt"`
	UsedPercent      float64    `json:"usedPercent"`
	RemainingPercent float64    `json:"remainingPercent"`
	WindowMinutes    int        `json:"windowMinutes"`
	ResetsAt         *time.Time `json:"resetsAt,omitempty"`
}

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

func (s *Store) Migrate(ctx context.Context) error {
	ddl := `
create table if not exists accounts (
  id bigserial primary key,
  account_key text not null unique,
  chatgpt_account_id text not null,
  chatgpt_user_id text not null,
  email text not null default '',
  plan text not null default '',
  source_path text not null,
  token_hash text not null,
  last_refresh_at timestamptz,
  last_polled_at timestamptz,
  last_success_at timestamptz,
  last_error text,
  last_error_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create table if not exists quota_snapshots (
  id bigserial primary key,
  account_id bigint not null references accounts(id) on delete cascade,
  captured_at timestamptz not null,
  window_slot text not null,
  window_name text not null,
  used_percent double precision not null,
  remaining_percent double precision not null,
  window_minutes integer not null default 0,
  resets_at timestamptz,
  plan_type text not null default '',
  credits_balance double precision,
  raw_json jsonb not null,
  created_at timestamptz not null default now(),
  unique(account_id, captured_at, window_slot)
);

create index if not exists quota_snapshots_account_window_captured_idx
  on quota_snapshots(account_id, window_name, captured_at desc);
`
	_, err := s.pool.Exec(ctx, ddl)
	return err
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *Store) UpsertAccount(ctx context.Context, snapshot account.Snapshot) (int64, error) {
	var lastRefresh *time.Time
	if snapshot.LastRefresh != nil {
		lastRefresh = snapshot.LastRefresh
	}

	var id int64
	err := s.pool.QueryRow(ctx, `
insert into accounts (
  account_key, chatgpt_account_id, chatgpt_user_id, email, plan,
  source_path, token_hash, last_refresh_at, updated_at
) values ($1,$2,$3,$4,$5,$6,$7,$8,now())
on conflict (account_key) do update set
  chatgpt_account_id = excluded.chatgpt_account_id,
  chatgpt_user_id = excluded.chatgpt_user_id,
  email = excluded.email,
  plan = excluded.plan,
  source_path = excluded.source_path,
  token_hash = excluded.token_hash,
  last_refresh_at = excluded.last_refresh_at,
  updated_at = now()
returning id
`, snapshot.AccountKey, snapshot.ChatGPTAccountID, snapshot.ChatGPTUserID, snapshot.Email, snapshot.Plan, snapshot.SourcePath, HashToken(snapshot.AccessToken), lastRefresh).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("upsert account: %w", err)
	}
	return id, nil
}

func (s *Store) RecordPollSuccess(ctx context.Context, accountID int64, usage quota.NormalizedUsage, capturedAt time.Time) error {
	rawJSON := json.RawMessage(usage.RawJSON)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, w := range usage.Windows {
		_, err := tx.Exec(ctx, `
insert into quota_snapshots (
  account_id, captured_at, window_slot, window_name, used_percent, remaining_percent,
  window_minutes, resets_at, plan_type, credits_balance, raw_json
) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
on conflict (account_id, captured_at, window_slot) do nothing
`, accountID, capturedAt, w.Slot, w.Name, w.UsedPercent, w.RemainingPercent, w.WindowMinutes, w.ResetsAt, usage.PlanType, usage.CreditsBalance, rawJSON)
		if err != nil {
			return fmt.Errorf("insert quota snapshot: %w", err)
		}
	}

	_, err = tx.Exec(ctx, `
update accounts
set last_polled_at = $2,
    last_success_at = $2,
    last_error = null,
    last_error_at = null,
    updated_at = now()
where id = $1
`, accountID, capturedAt)
	if err != nil {
		return fmt.Errorf("update account success state: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (s *Store) RecordPollFailure(ctx context.Context, accountID int64, capturedAt time.Time, pollErr error) error {
	_, err := s.pool.Exec(ctx, `
update accounts
set last_polled_at = $2,
    last_error = $3,
    last_error_at = $2,
    updated_at = now()
where id = $1
`, accountID, capturedAt, pollErr.Error())
	return err
}

func (s *Store) ListAccounts(ctx context.Context) ([]Account, error) {
	rows, err := s.pool.Query(ctx, `
select
  a.id,
  a.account_key,
  a.chatgpt_account_id,
  a.chatgpt_user_id,
  a.email,
  a.plan,
  a.source_path,
  a.last_polled_at,
  a.last_success_at,
  a.last_error,
  a.last_error_at,
  fh.remaining_percent,
  fh.used_percent,
  fh.resets_at,
  wk.remaining_percent,
  wk.used_percent,
  wk.resets_at
from accounts a
left join lateral (
  select remaining_percent, used_percent, resets_at
  from quota_snapshots
  where account_id = a.id and window_name = 'five_hour'
  order by captured_at desc
  limit 1
) fh on true
left join lateral (
  select remaining_percent, used_percent, resets_at
  from quota_snapshots
  where account_id = a.id and window_name = 'weekly'
  order by captured_at desc
  limit 1
) wk on true
order by coalesce(nullif(a.email, ''), a.account_key), a.account_key
`)
	if err != nil {
		return nil, fmt.Errorf("query accounts: %w", err)
	}
	defer rows.Close()

	var accounts []Account
	for rows.Next() {
		var account Account
		err := rows.Scan(
			&account.ID,
			&account.AccountKey,
			&account.ChatGPTAccountID,
			&account.ChatGPTUserID,
			&account.Email,
			&account.Plan,
			&account.SourcePath,
			&account.LastPolledAt,
			&account.LastSuccessAt,
			&account.LastError,
			&account.LastErrorAt,
			&account.FiveHourRemaining,
			&account.FiveHourUsed,
			&account.FiveHourResetsAt,
			&account.WeeklyRemaining,
			&account.WeeklyUsed,
			&account.WeeklyResetsAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan account row: %w", err)
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

func (s *Store) AccountHistory(ctx context.Context, accountID int64, windowName string, limit int) ([]HistoryPoint, error) {
	if limit <= 0 {
		limit = 288
	}
	rows, err := s.pool.Query(ctx, `
select captured_at, used_percent, remaining_percent, window_minutes, resets_at
from quota_snapshots
where account_id = $1 and window_name = $2
order by captured_at desc
limit $3
`, accountID, windowName, limit)
	if err != nil {
		return nil, fmt.Errorf("query history: %w", err)
	}
	defer rows.Close()

	var points []HistoryPoint
	for rows.Next() {
		var point HistoryPoint
		if err := rows.Scan(&point.CapturedAt, &point.UsedPercent, &point.RemainingPercent, &point.WindowMinutes, &point.ResetsAt); err != nil {
			return nil, fmt.Errorf("scan history point: %w", err)
		}
		points = append(points, point)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i, j := 0, len(points)-1; i < j; i, j = i+1, j-1 {
		points[i], points[j] = points[j], points[i]
	}
	return points, nil
}
