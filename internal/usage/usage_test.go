package usage

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"opencode2api/internal/config"
	modelcatalog "opencode2api/internal/models"
	"opencode2api/internal/protocol"
	"opencode2api/internal/store"
	"opencode2api/internal/telemetry"
)

func TestNormalizeChat(t *testing.T) {
	meta := &telemetry.RequestMeta{
		Model: "free-model", Tier: "zen", Channel: "key", ServerKeyID: "0123456789abcdef",
		Session: "ses_secret", UsageReported: true, Stream: true, Attempts: 2,
		Usage: protocol.Usage{Input: 1000, Cached: 200, CacheCreation: 100, Output: 50, Reasoning: 10, Total: 1050},
	}
	event, ok := Normalize(meta, "/v1/chat/completions", 200, time.Now().Add(-time.Second), time.Time{})
	if !ok {
		t.Fatal("Normalize returned false")
	}
	if event.InputTokens != 700 || event.CachedTokens != 200 || event.CacheWriteTokens != 100 ||
		event.OutputTokens != 50 || event.ReasoningTokens != 10 || event.TotalTokens != 1050 {
		t.Fatalf("unexpected token normalization: %+v", event)
	}
	if event.Provider != "opencode" || event.Endpoint != "chat" || event.UsageSource != "provider" {
		t.Fatalf("unexpected routing fields: %+v", event)
	}
	if event.SessionFP == nil || *event.SessionFP == "ses_secret" || len(*event.SessionFP) != 16 {
		t.Fatalf("session was not hashed: %+v", event.SessionFP)
	}
	if event.Success != true || event.Outcome != "success" {
		t.Fatalf("unexpected outcome: %+v", event)
	}
	if event.MetaJSON == nil {
		t.Fatal("meta_json missing")
	}
}

func TestNormalizeAnonymous(t *testing.T) {
	event, _ := Normalize(&telemetry.RequestMeta{
		Model: "free-model", Tier: "zen", Anonymous: true, Channel: "anonymous",
	}, "/v1/chat/completions", 200, time.Now(), time.Time{})
	if event.Provider != "anonymous" || event.Tier != "anonymous" {
		t.Fatalf("anonymous routing fields: %+v", event)
	}
}

func TestNormalizePartialAndUnavailable(t *testing.T) {
	partial, _ := Normalize(&telemetry.RequestMeta{
		Model: "m", Tier: "go", Channel: "key",
		Usage: protocol.Usage{Input: 10, Output: 0},
	}, "/v1/responses", 200, time.Now(), time.Time{})
	if partial.UsageSource != "partial" || partial.Provider != "opencode-go" || partial.TotalTokens != 10 {
		t.Fatalf("unexpected partial event: %+v", partial)
	}
	unavailable, _ := Normalize(&telemetry.RequestMeta{
		Model: "m", Tier: "zen", Channel: "key",
	}, "/v1/messages", http.StatusBadGateway, time.Now(), time.Time{})
	if unavailable.UsageSource != "unavailable" || unavailable.Success || unavailable.Outcome != "server_error" {
		t.Fatalf("unexpected unavailable event: %+v", unavailable)
	}
	if unavailable.ErrorCode == nil || *unavailable.ErrorCode != "http_502" {
		t.Fatalf("unexpected error code: %v", unavailable.ErrorCode)
	}
}

func TestNormalizeClientCanceled(t *testing.T) {
	event, _ := Normalize(&telemetry.RequestMeta{
		Model: "m", Tier: "zen", Channel: "key", Outcome: "client_canceled", Stream: true,
		Usage: protocol.Usage{Input: 40, Cached: 10, Output: 5},
	}, "/v1/chat/completions", 200, time.Now().Add(-time.Second), time.Time{})
	if event.Success || event.Outcome != "client_canceled" || event.ErrorCode == nil {
		t.Fatalf("unexpected canceled event: %+v", event)
	}
	if event.TotalTokens != 45 {
		t.Fatalf("total = %d, want 45", event.TotalTokens)
	}
}

func TestApplyPrice(t *testing.T) {
	one := 1.5
	two := 2.0
	read := 0.1
	write := 0.2
	event := store.Event{
		Model: "paid", UsageSource: "provider", InputTokens: 1_000_000, CachedTokens: 200_000,
		CacheWriteTokens: 100_000, OutputTokens: 500_000,
	}
	ApplyPrice(&event, Price{Input: &one, Output: &two, CacheRead: &read, CacheWrite: &write, Source: "manual"}, true)
	if event.CostMicroUSD == nil || *event.CostMicroUSD != 2_540_000 {
		t.Fatalf("cost = %v, want 2540000", event.CostMicroUSD)
	}
	if event.CostInputMicroUSD == nil || *event.CostInputMicroUSD != 1_500_000 ||
		event.CostOutputMicroUSD == nil || *event.CostOutputMicroUSD != 1_000_000 ||
		event.CostCacheMicroUSD == nil || *event.CostCacheMicroUSD != 40_000 {
		t.Fatalf("unexpected cost breakdown: %+v", event)
	}
	missing := event
	missing.CostMicroUSD = nil
	missing.CachedTokens = 0
	missing.CacheWriteTokens = 0
	ApplyPrice(&missing, Price{Input: &one, Output: &two}, true)
	if missing.CostMicroUSD == nil || *missing.CostMicroUSD != 2_500_000 {
		t.Fatalf("missing cache price with zero cache tokens should be priced: %v", missing.CostMicroUSD)
	}
	unknown := event
	unknown.CostMicroUSD = nil
	ApplyPrice(&unknown, Price{Input: &one, Output: &two}, true)
	if unknown.CostMicroUSD != nil || unknown.PricingSource == nil || *unknown.PricingSource != "none" {
		t.Fatalf("missing cache price with cache tokens should be unknown: %+v", unknown)
	}
	free := store.Event{Model: "some-free-model", UsageSource: "provider", InputTokens: 10}
	ApplyPrice(&free, Price{}, false)
	if free.CostMicroUSD == nil || *free.CostMicroUSD != 0 || *free.PricingSource != "hardcoded" {
		t.Fatalf("free model should cost zero: %+v", free)
	}
	unavailable := store.Event{Model: "paid", UsageSource: "unavailable"}
	ApplyPrice(&unavailable, Price{Input: &one}, true)
	if unavailable.CostMicroUSD != nil {
		t.Fatalf("unavailable usage should not be priced: %+v", unavailable)
	}
}

type fakePricer struct{}

func (fakePricer) Price(string) (modelcatalog.Price, bool) {
	input, output := 1.0, 2.0
	return modelcatalog.Price{Input: &input, Output: &output}, true
}

func TestServiceMiddlewareRecords(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	service, err := New(Options{
		Config:     config.UsageConfig{Enabled: true, RetentionDays: 7, BatchSize: 1, FlushIntervalMS: 10, QueryTimeoutMS: 1000},
		ConfigPath: configPath,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Pricer:     fakePricer{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		meta := telemetry.MetaFromRequest(r)
		meta.Model = "paid-model"
		meta.Tier = "zen"
		meta.Channel = "key"
		meta.UsageReported = true
		meta.Usage = protocol.Usage{Input: 1000, Output: 100, Total: 1100}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	started := time.Now()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	meta := &telemetry.RequestMeta{}
	request = request.WithContext(telemetry.WithRequestMeta(request.Context(), meta))
	recorder := httptest.NewRecorder()
	service.Middleware(handler).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := service.Close(closeCtx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	database, err := store.Open(service.Path())
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer database.Close()
	events, err := database.Events(context.Background(), store.Filter{From: started.Add(-time.Minute), To: started.Add(time.Minute)}, 0, 10)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].CostMicroUSD == nil || *events[0].CostMicroUSD != 1200 {
		t.Fatalf("unexpected cost: %v", events[0].CostMicroUSD)
	}
	if events[0].TTFTMS == nil {
		t.Fatal("ttft missing")
	}
}

func TestDatabasePath(t *testing.T) {
	dir := t.TempDir()
	path, err := databasePath(filepath.Join(dir, "config.json"), "usage.db")
	if err != nil {
		t.Fatalf("databasePath: %v", err)
	}
	if path != filepath.Join(dir, "usage.db") {
		t.Fatalf("path = %q", path)
	}
	if _, err := databasePath("", "usage.db"); err == nil {
		t.Fatal("expected error for relative path without config path")
	}
}
