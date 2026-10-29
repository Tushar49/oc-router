package admin

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"opencode2api/internal/config"
	modelcatalog "opencode2api/internal/models"
	"opencode2api/internal/protocol"
	"opencode2api/internal/store"
	"opencode2api/internal/telemetry"
	"opencode2api/internal/usage"
)

type adminPricer struct{}

func (adminPricer) Price(string) (modelcatalog.Price, bool) {
	input, output := 1.0, 2.0
	return modelcatalog.Price{Input: &input, Output: &output}, true
}

func TestUsageAdminEndpoints(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	service, err := usage.New(usage.Options{
		Config:     config.UsageConfig{Enabled: true, RetentionDays: 7, BatchSize: 1, FlushIntervalMS: 10, QueryTimeoutMS: 1000},
		ConfigPath: configPath,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Pricer:     adminPricer{},
	})
	if err != nil {
		t.Fatalf("usage.New: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = service.Close(closeCtx)
	})
	recorded := time.Now().UTC().Add(-time.Second)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		meta := telemetry.MetaFromRequest(r)
		meta.Model = "paid-model"
		meta.Tier = "zen"
		meta.Channel = "key"
		meta.Session = "ses_secret"
		meta.UsageReported = true
		meta.Usage = protocol.Usage{Input: 1000, Output: 100, Total: 1100}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	request = request.WithContext(telemetry.WithRequestMeta(request.Context(), &telemetry.RequestMeta{}))
	service.Middleware(handler).ServeHTTP(httptest.NewRecorder(), request)
	waitWritten(t, service)

	server := &Server{usage: service, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	from := recorded.Add(-time.Minute).Format(time.RFC3339Nano)
	to := recorded.Add(time.Minute).Format(time.RFC3339Nano)
	query := url.Values{"from": {from}, "to": {to}}

	summaryRecorder := httptest.NewRecorder()
	server.handleUsageSummary(summaryRecorder, httptest.NewRequest(http.MethodGet, "/api/usage/summary?"+query.Encode(), nil))
	if summaryRecorder.Code != http.StatusOK {
		t.Fatalf("summary status = %d: %s", summaryRecorder.Code, summaryRecorder.Body.String())
	}
	var summary store.Summary
	if err := json.Unmarshal(summaryRecorder.Body.Bytes(), &summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if summary.Totals.Requests != 1 || summary.Totals.TotalTokens != 1100 || summary.Totals.EstimatedCostUSD == nil {
		t.Fatalf("unexpected summary: %+v", summary.Totals)
	}
	if summary.Groups == nil {
		t.Fatal("summary groups is null, want an array")
	}

	eventsRecorder := httptest.NewRecorder()
	server.handleUsageEvents(eventsRecorder, httptest.NewRequest(http.MethodGet, "/api/usage/events?"+query.Encode(), nil))
	if eventsRecorder.Code != http.StatusOK {
		t.Fatalf("events status = %d: %s", eventsRecorder.Code, eventsRecorder.Body.String())
	}
	var events struct {
		Events []store.Event `json:"events"`
	}
	if err := json.Unmarshal(eventsRecorder.Body.Bytes(), &events); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	if len(events.Events) != 1 || events.Events[0].SessionFP == nil {
		t.Fatalf("unexpected events: %+v", events.Events)
	}
	if strings.Contains(eventsRecorder.Body.String(), "ses_secret") {
		t.Fatal("raw session leaked into events")
	}

	seriesRecorder := httptest.NewRecorder()
	server.handleUsageTimeseries(seriesRecorder, httptest.NewRequest(http.MethodGet, "/api/usage/timeseries?"+query.Encode(), nil))
	if seriesRecorder.Code != http.StatusOK {
		t.Fatalf("timeseries status = %d: %s", seriesRecorder.Code, seriesRecorder.Body.String())
	}
	var series struct {
		Points []store.Point `json:"points"`
	}
	if err := json.Unmarshal(seriesRecorder.Body.Bytes(), &series); err != nil {
		t.Fatalf("decode timeseries: %v", err)
	}
	if len(series.Points) != 1 {
		t.Fatalf("unexpected points: %+v", series.Points)
	}

	csvRecorder := httptest.NewRecorder()
	server.handleUsageExport(csvRecorder, httptest.NewRequest(http.MethodGet, "/api/usage/export?format=csv&"+query.Encode(), nil))
	if csvRecorder.Code != http.StatusOK {
		t.Fatalf("csv status = %d: %s", csvRecorder.Code, csvRecorder.Body.String())
	}
	if !strings.Contains(csvRecorder.Body.String(), "input_tokens") || strings.Contains(csvRecorder.Body.String(), "ses_secret") {
		t.Fatalf("unexpected csv: %s", csvRecorder.Body.String())
	}

	backupRecorder := httptest.NewRecorder()
	server.handleUsageBackup(backupRecorder, httptest.NewRequest(http.MethodPost, "/api/usage/backup", nil))
	if backupRecorder.Code != http.StatusOK {
		t.Fatalf("backup status = %d: %s", backupRecorder.Code, backupRecorder.Body.String())
	}
	var backup struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(backupRecorder.Body.Bytes(), &backup); err != nil {
		t.Fatalf("decode backup: %v", err)
	}
	if info, err := os.Stat(backup.Path); err != nil || info.Size() == 0 {
		t.Fatalf("backup file missing: %v", err)
	}
}

func waitWritten(t *testing.T, service *usage.Service) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for service.Stats()["written"] == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if service.Stats()["written"] == 0 {
		t.Fatal("usage writer did not flush")
	}
}
