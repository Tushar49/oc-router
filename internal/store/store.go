// Package store owns the local SQLite database used for operational metadata.
//
// The schema is forward-only and embedded. Usage rows contain metadata only:
// token counts, prices, status and fingerprints. Prompts, responses, tool
// arguments, raw keys, raw session IDs and IP addresses are never written.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	// tsLayout is fixed-width so lexicographic comparisons and indexes are
	// stable. All values are UTC.
	tsLayout  = "2006-01-02T15:04:05.000000000Z07:00"
	dayLayout = "2006-01-02"
)

// ErrSchemaNewer is returned when the database was written by a newer build.
var ErrSchemaNewer = errors.New("usage database schema is newer than this build")

//go:embed migrations/*.sql
var migrations embed.FS

// Event is one client request's metadata-only usage row.
type Event struct {
	ID                 int64     `json:"id"`
	RequestID          string    `json:"request_id"`
	TS                 time.Time `json:"ts"`
	Day                string    `json:"day"`
	Provider           string    `json:"provider"`
	Model              string    `json:"model"`
	Endpoint           string    `json:"endpoint"`
	Tier               string    `json:"tier"`
	ServerKeyFP        *string   `json:"server_key_fp,omitempty"`
	SessionFP          *string   `json:"session_fp,omitempty"`
	Status             int       `json:"status"`
	Outcome            string    `json:"outcome"`
	Success            bool      `json:"success"`
	ErrorCode          *string   `json:"error_code,omitempty"`
	LatencyMS          int64     `json:"latency_ms"`
	TTFTMS             *int64    `json:"ttft_ms,omitempty"`
	InputTokens        int64     `json:"input_tokens"`
	CachedTokens       int64     `json:"cached_tokens"`
	CacheWriteTokens   int64     `json:"cache_write_tokens"`
	OutputTokens       int64     `json:"output_tokens"`
	ReasoningTokens    int64     `json:"reasoning_tokens"`
	TotalTokens        int64     `json:"total_tokens"`
	UsageSource        string    `json:"usage_source"`
	PricingSource      *string   `json:"pricing_source,omitempty"`
	CostMicroUSD       *int64    `json:"cost_micro_usd,omitempty"`
	CostInputMicroUSD  *int64    `json:"cost_input_micro_usd,omitempty"`
	CostOutputMicroUSD *int64    `json:"cost_output_micro_usd,omitempty"`
	CostCacheMicroUSD  *int64    `json:"cost_cache_micro_usd,omitempty"`
	Stream             bool      `json:"stream"`
	Attempts           int       `json:"attempts"`
	ProxyKind          *string   `json:"proxy_kind,omitempty"`
	ProxyID            *string   `json:"proxy_id,omitempty"`
	SaverJSON          *string   `json:"saver_json,omitempty"`
	MetaJSON           *string   `json:"meta_json,omitempty"`
}

// Filter selects usage rows. From is inclusive and To exclusive.
type Filter struct {
	From        time.Time
	To          time.Time
	Provider    string
	Model       string
	Endpoint    string
	Tier        string
	ServerKeyFP string
	Outcome     string
	Status      *int
	GroupBy     string
	Sort        string
	Order       string
	Limit       int
}

// Totals is the common aggregate shape returned by the usage API.
type Totals struct {
	Requests         int64    `json:"requests"`
	Success          int64    `json:"success"`
	Errors           int64    `json:"errors"`
	InputTokens      int64    `json:"input_tokens"`
	CachedTokens     int64    `json:"cached_tokens"`
	CacheWriteTokens int64    `json:"cache_write_tokens"`
	OutputTokens     int64    `json:"output_tokens"`
	ReasoningTokens  int64    `json:"reasoning_tokens"`
	TotalTokens      int64    `json:"total_tokens"`
	EstimatedCostUSD *float64 `json:"estimated_cost_usd"`
	Currency         string   `json:"currency"`
	UsageCoverage    float64  `json:"usage_coverage"`
	CostCoverage     float64  `json:"cost_coverage"`

	costMicroUSD  int64
	usageReported int64
	priced        int64
}

// Group is one aggregated usage bucket.
type Group struct {
	Totals
	Provider      string `json:"provider,omitempty"`
	Model         string `json:"model,omitempty"`
	Endpoint      string `json:"endpoint,omitempty"`
	Day           string `json:"day,omitempty"`
	ServerKeyFP   string `json:"server_key_fp,omitempty"`
	PricingSource string `json:"pricing_source,omitempty"`
	UsageSource   string `json:"usage_source,omitempty"`
}

// Range is the UTC window a summary covers.
type Range struct {
	From     time.Time `json:"from"`
	To       time.Time `json:"to"`
	Timezone string    `json:"timezone"`
}

// Summary is an aggregated response over a range.
type Summary struct {
	Range     Range   `json:"range"`
	Totals    Totals  `json:"totals"`
	Groups    []Group `json:"groups"`
	Source    string  `json:"source"`
	Truncated bool    `json:"truncated,omitempty"`
}

// Point is one timeseries bucket. Totals are flattened into the point.
type Point struct {
	Day string `json:"day"`
	Totals
}

// Store is the SQLite handle. One connection serializes the async writer and
// short read queries; WAL keeps readers from blocking the writer.
type Store struct {
	mu            sync.Mutex
	db            *sql.DB
	path          string
	dir           string
	retentionDays int
}

// Open opens or creates the database at path, applies pending migrations and
// fails closed when the file was written by a newer schema.
func Open(path string) (*Store, error) {
	path = filepath.Clean(path)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create usage database directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open usage database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA synchronous = NORMAL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("configure usage database: %w", err)
		}
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping usage database: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(path, 0o600)
	}
	return &Store{db: db, path: path, dir: dir, retentionDays: 365}, nil
}

// Path returns the database file path.
func (s *Store) Path() string { return s.path }

// SetRetentionDays configures how long raw events are kept. Rollups remain.
func (s *Store) SetRetentionDays(days int) {
	if days < 1 {
		days = 365
	}
	s.mu.Lock()
	s.retentionDays = days
	s.mu.Unlock()
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

func migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	type migration struct {
		version int
		name    string
	}
	list := make([]migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		prefix := strings.SplitN(entry.Name(), "_", 2)[0]
		version, err := strconv.Atoi(prefix)
		if err != nil {
			return fmt.Errorf("invalid migration name %q", entry.Name())
		}
		list = append(list, migration{version: version, name: entry.Name()})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].version < list[j].version })
	if len(list) == 0 {
		return errors.New("no usage migrations embedded")
	}
	latest := list[len(list)-1].version

	var current int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if current > latest {
		return fmt.Errorf("%w: database version %d, binary supports %d; back up the database and use the newer build", ErrSchemaNewer, current, latest)
	}
	for _, item := range list {
		if item.version <= current {
			continue
		}
		body, err := migrations.ReadFile("migrations/" + item.name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", item.name, err)
		}
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", item.name, err)
		}
		for _, statement := range strings.Split(string(body), ";") {
			statement = strings.TrimSpace(statement)
			if statement == "" {
				continue
			}
			if _, err := tx.Exec(statement); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("apply migration %s: %w", item.name, err)
			}
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)`, item.version, formatTS(time.Now())); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", item.name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", item.name, err)
		}
	}
	return nil
}

// InsertBatch writes events and their daily rollups in one transaction.
func (s *Store) InsertBatch(ctx context.Context, events []Event) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin usage batch: %w", err)
	}
	defer tx.Rollback()
	insertEvent, err := tx.PrepareContext(ctx, `INSERT INTO usage_event (
		request_id, ts, day, provider, model, endpoint, tier, server_key_fp, session_fp,
		status, outcome, success, error_code, latency_ms, ttft_ms,
		input_tokens, cached_tokens, cache_write_tokens, output_tokens, reasoning_tokens, total_tokens,
		usage_source, pricing_source, cost_micro_usd, cost_input_micro_usd, cost_output_micro_usd, cost_cache_micro_usd,
		stream, attempts, proxy_kind, proxy_id, saver_json, meta_json
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(request_id) DO NOTHING`)
	if err != nil {
		return fmt.Errorf("prepare usage insert: %w", err)
	}
	defer insertEvent.Close()
	upsertDaily, err := tx.PrepareContext(ctx, `INSERT INTO usage_daily (
		day, provider, model, endpoint, tier, server_key_fp,
		requests, success, errors, input_tokens, cached_tokens, cache_write_tokens,
		output_tokens, reasoning_tokens, total_tokens, cost_micro_usd,
		usage_reported, priced_requests, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(day, provider, model, endpoint, tier, server_key_fp) DO UPDATE SET
		requests = usage_daily.requests + 1,
		success = usage_daily.success + excluded.success,
		errors = usage_daily.errors + excluded.errors,
		input_tokens = usage_daily.input_tokens + excluded.input_tokens,
		cached_tokens = usage_daily.cached_tokens + excluded.cached_tokens,
		cache_write_tokens = usage_daily.cache_write_tokens + excluded.cache_write_tokens,
		output_tokens = usage_daily.output_tokens + excluded.output_tokens,
		reasoning_tokens = usage_daily.reasoning_tokens + excluded.reasoning_tokens,
		total_tokens = usage_daily.total_tokens + excluded.total_tokens,
		cost_micro_usd = usage_daily.cost_micro_usd + excluded.cost_micro_usd,
		usage_reported = usage_daily.usage_reported + excluded.usage_reported,
		priced_requests = usage_daily.priced_requests + excluded.priced_requests,
		updated_at = excluded.updated_at`)
	if err != nil {
		return fmt.Errorf("prepare daily upsert: %w", err)
	}
	defer upsertDaily.Close()

	for _, event := range events {
		if event.RequestID == "" {
			return errors.New("usage event request_id is required")
		}
		if event.TS.IsZero() {
			event.TS = time.Now().UTC()
		}
		event.TS = event.TS.UTC()
		if event.Day == "" {
			event.Day = event.TS.Format(dayLayout)
		}
		if event.Outcome == "" {
			event.Outcome = "unknown"
		}
		if event.UsageSource == "" {
			event.UsageSource = "unavailable"
		}
		if event.Attempts < 1 {
			event.Attempts = 1
		}
		success := int64(0)
		if event.Success {
			success = 1
		}
		reported := int64(0)
		if event.UsageSource == "provider" {
			reported = 1
		}
		priced := int64(0)
		cost := int64(0)
		if event.CostMicroUSD != nil {
			priced = 1
			cost = *event.CostMicroUSD
		}
		stream := int64(0)
		if event.Stream {
			stream = 1
		}
		result, err := insertEvent.ExecContext(ctx,
			event.RequestID, formatTS(event.TS), event.Day, event.Provider, event.Model, event.Endpoint, event.Tier,
			nullableString(event.ServerKeyFP), nullableString(event.SessionFP), event.Status, event.Outcome, success,
			nullableString(event.ErrorCode), event.LatencyMS, nullableInt64(event.TTFTMS),
			event.InputTokens, event.CachedTokens, event.CacheWriteTokens, event.OutputTokens, event.ReasoningTokens, event.TotalTokens,
			event.UsageSource, nullableString(event.PricingSource), nullableInt64(event.CostMicroUSD),
			nullableInt64(event.CostInputMicroUSD), nullableInt64(event.CostOutputMicroUSD), nullableInt64(event.CostCacheMicroUSD),
			stream, event.Attempts, nullableString(event.ProxyKind), nullableString(event.ProxyID),
			nullableString(event.SaverJSON), nullableString(event.MetaJSON),
		)
		if err != nil {
			return fmt.Errorf("insert usage event: %w", err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("read usage insert result: %w", err)
		}
		if rows == 0 {
			continue
		}
		serverKey := ""
		if event.ServerKeyFP != nil {
			serverKey = *event.ServerKeyFP
		}
		if _, err := upsertDaily.ExecContext(ctx,
			event.Day, event.Provider, event.Model, event.Endpoint, event.Tier, serverKey,
			success, 1-success, event.InputTokens, event.CachedTokens, event.CacheWriteTokens,
			event.OutputTokens, event.ReasoningTokens, event.TotalTokens, cost,
			reported, priced, formatTS(time.Now()),
		); err != nil {
			return fmt.Errorf("upsert daily usage: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit usage batch: %w", err)
	}
	return nil
}

// RollupDay recomputes usage_daily for one UTC day from raw events and marks
// the day complete. It is idempotent.
func (s *Store) RollupDay(ctx context.Context, day string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin rollup %s: %w", day, err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM usage_daily WHERE day = ?`, day); err != nil {
		return fmt.Errorf("clear daily usage %s: %w", day, err)
	}
	now := formatTS(time.Now())
	if _, err := tx.ExecContext(ctx, `INSERT INTO usage_daily (
		day, provider, model, endpoint, tier, server_key_fp,
		requests, success, errors, input_tokens, cached_tokens, cache_write_tokens,
		output_tokens, reasoning_tokens, total_tokens, cost_micro_usd,
		usage_reported, priced_requests, updated_at
	)
	SELECT day, provider, model, endpoint, tier, COALESCE(server_key_fp, ''),
		COUNT(*), SUM(success), SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END),
		SUM(input_tokens), SUM(cached_tokens), SUM(cache_write_tokens),
		SUM(output_tokens), SUM(reasoning_tokens), SUM(total_tokens),
		SUM(COALESCE(cost_micro_usd, 0)),
		SUM(CASE WHEN usage_source = 'provider' THEN 1 ELSE 0 END),
		SUM(CASE WHEN cost_micro_usd IS NOT NULL THEN 1 ELSE 0 END),
		?
	FROM usage_event WHERE day = ?
	GROUP BY day, provider, model, endpoint, tier, COALESCE(server_key_fp, '')`, now, day); err != nil {
		return fmt.Errorf("roll up daily usage %s: %w", day, err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO rollup_state(day, complete, updated_at) VALUES(?, 1, ?)
		ON CONFLICT(day) DO UPDATE SET complete = 1, updated_at = excluded.updated_at`, day, now); err != nil {
		return fmt.Errorf("mark rollup %s: %w", day, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit rollup %s: %w", day, err)
	}
	return nil
}

// Retention rolls up and then deletes raw events older than the configured
// retention window. Deletion only touches days whose rollup committed.
func (s *Store) Retention(ctx context.Context) (int64, int, error) {
	s.mu.Lock()
	days := s.retentionDays
	s.mu.Unlock()
	cutoff := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -days).Format(dayLayout)
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT e.day FROM usage_event e
		LEFT JOIN rollup_state r ON r.day = e.day AND r.complete = 1
		WHERE e.day < ? AND r.day IS NULL ORDER BY e.day`, cutoff)
	if err != nil {
		return 0, 0, fmt.Errorf("find days to roll up: %w", err)
	}
	var pending []string
	for rows.Next() {
		var day string
		if err := rows.Scan(&day); err != nil {
			rows.Close()
			return 0, 0, fmt.Errorf("scan rollup day: %w", err)
		}
		pending = append(pending, day)
	}
	if err := rows.Close(); err != nil {
		return 0, 0, fmt.Errorf("close rollup days: %w", err)
	}
	for _, day := range pending {
		if err := s.RollupDay(ctx, day); err != nil {
			return 0, 0, err
		}
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM usage_event
		WHERE day < ? AND EXISTS (SELECT 1 FROM rollup_state r WHERE r.day = usage_event.day AND r.complete = 1)`, cutoff)
	if err != nil {
		return 0, 0, fmt.Errorf("delete expired usage: %w", err)
	}
	deleted, _ := result.RowsAffected()
	if deleted > 0 {
		_, _ = s.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	}
	return deleted, len(pending), nil
}

// Backup writes a consistent copy of the database beside it with VACUUM INTO.
func (s *Store) Backup(ctx context.Context) (string, error) {
	base := "backup-" + time.Now().UTC().Format("20060102T150405Z")
	for index := 0; index < 100; index++ {
		name := base
		if index > 0 {
			name = fmt.Sprintf("%s-%d", base, index)
		}
		dest := filepath.Join(s.dir, name+".db")
		if _, err := os.Stat(dest); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("check backup path: %w", err)
		}
		query := "VACUUM INTO '" + strings.ReplaceAll(dest, "'", "''") + "'"
		if _, err := s.db.ExecContext(ctx, query); err != nil {
			return "", fmt.Errorf("backup usage database: %w", err)
		}
		if runtime.GOOS != "windows" {
			_ = os.Chmod(dest, 0o600)
		}
		return dest, nil
	}
	return "", errors.New("could not choose a unique backup path")
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func formatTS(value time.Time) string {
	return value.UTC().Format(tsLayout)
}
