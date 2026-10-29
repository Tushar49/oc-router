package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	database, err := Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func testEvent(day time.Time, model string, input, output int64) Event {
	requestID := model + "-" + day.Format("20060102")
	serverKey := "0123456789abcdef"
	session := "fedcba9876543210"
	cost := input + output
	return Event{
		RequestID: requestID, TS: day.Add(12 * time.Hour), Day: day.Format(dayLayout),
		Provider: "opencode", Model: model, Endpoint: "chat", Tier: "zen",
		ServerKeyFP: &serverKey, SessionFP: &session, Status: 200, Outcome: "success", Success: true,
		LatencyMS: 12, InputTokens: input, OutputTokens: output, TotalTokens: input + output,
		UsageSource: "provider", CostMicroUSD: &cost, Stream: true, Attempts: 1,
	}
}

func TestInsertSummaryAndEvents(t *testing.T) {
	database := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(24 * time.Hour)
	first := testEvent(now, "free-model", 300, 70)
	second := testEvent(now, "free-model", 100, 30)
	second.RequestID = "second"
	second.CostMicroUSD = nil
	if err := database.InsertBatch(ctx, []Event{first, second}); err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}
	summary, err := database.Summary(ctx, Filter{From: now, To: now.Add(24 * time.Hour), GroupBy: "provider_model"})
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if summary.Source != "raw" {
		t.Fatalf("source = %q, want raw", summary.Source)
	}
	if summary.Totals.Requests != 2 || summary.Totals.InputTokens != 400 || summary.Totals.OutputTokens != 100 {
		t.Fatalf("unexpected totals: %+v", summary.Totals)
	}
	if summary.Totals.CostCoverage != 0.5 {
		t.Fatalf("cost coverage = %v, want 0.5", summary.Totals.CostCoverage)
	}
	if len(summary.Groups) != 1 || summary.Groups[0].Requests != 2 {
		t.Fatalf("unexpected groups: %+v", summary.Groups)
	}
	events, err := database.Events(ctx, Filter{}, 0, 1)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 1 || events[0].RequestID != "second" {
		t.Fatalf("unexpected first page: %+v", events)
	}
	next, err := database.Events(ctx, Filter{}, events[0].ID, 10)
	if err != nil {
		t.Fatalf("Events next: %v", err)
	}
	if len(next) != 1 || next[0].RequestID != first.RequestID {
		t.Fatalf("unexpected second page: %+v", next)
	}
}

func TestRetentionRollsUpAndDeletesRaw(t *testing.T) {
	database := testStore(t)
	database.SetRetentionDays(7)
	ctx := context.Background()
	today := time.Now().UTC().Truncate(24 * time.Hour)
	oldDay := today.AddDate(0, 0, -10)
	old := testEvent(oldDay, "old-model", 500, 100)
	old.RequestID = "old"
	recent := testEvent(today, "new-model", 200, 50)
	recent.RequestID = "recent"
	if err := database.InsertBatch(ctx, []Event{old, recent}); err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}
	deleted, rolled, err := database.Retention(ctx)
	if err != nil {
		t.Fatalf("Retention: %v", err)
	}
	if deleted != 1 || rolled != 1 {
		t.Fatalf("deleted=%d rolled=%d, want 1/1", deleted, rolled)
	}
	events, err := database.Events(ctx, Filter{From: oldDay, To: oldDay.Add(24 * time.Hour)}, 0, 10)
	if err != nil {
		t.Fatalf("Events old: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("old raw events remain: %+v", events)
	}
	summary, err := database.Summary(ctx, Filter{From: oldDay, To: time.Now().UTC().Add(time.Hour), GroupBy: "provider_model"})
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if summary.Source != "mixed" {
		t.Fatalf("source = %q, want mixed", summary.Source)
	}
	if summary.Totals.Requests != 2 || summary.Totals.TotalTokens != 850 {
		t.Fatalf("unexpected mixed totals: %+v", summary.Totals)
	}
	points, err := database.Timeseries(ctx, Filter{From: oldDay, To: time.Now().UTC().Add(time.Hour)})
	if err != nil {
		t.Fatalf("Timeseries: %v", err)
	}
	if len(points) != 2 {
		t.Fatalf("points = %d, want 2", len(points))
	}
	status := 200
	filtered, err := database.Summary(ctx, Filter{From: oldDay, To: time.Now().UTC().Add(time.Hour), Status: &status})
	if err != nil {
		t.Fatalf("filtered Summary: %v", err)
	}
	if filtered.Source != "raw" || !filtered.Truncated {
		t.Fatalf("status filter should be raw and truncated: %+v", filtered)
	}
}

func TestBackupAndSchemaVersion(t *testing.T) {
	database := testStore(t)
	path, err := database.Backup(context.Background())
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		t.Fatalf("backup missing or empty: %v, size=%d", err, info.Size())
	}
	if _, err := database.db.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES(99, 'now')`); err != nil {
		t.Fatalf("insert newer schema: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := Open(database.Path()); !errors.Is(err, ErrSchemaNewer) {
		t.Fatalf("Open newer schema error = %v, want ErrSchemaNewer", err)
	}
}
