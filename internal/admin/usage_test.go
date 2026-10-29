package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseUsageTime(t *testing.T) {
	from, err := parseUsageTime("2026-10-01", false)
	if err != nil {
		t.Fatalf("parse from: %v", err)
	}
	if got := from.Format(time.RFC3339); got != "2026-10-01T00:00:00Z" {
		t.Fatalf("from = %s", got)
	}
	to, err := parseUsageTime("2026-10-01", true)
	if err != nil {
		t.Fatalf("parse to: %v", err)
	}
	if got := to.Format(time.RFC3339); got != "2026-10-02T00:00:00Z" {
		t.Fatalf("to = %s", got)
	}
	if _, err := parseUsageTime("not-a-date", false); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestParseUsageFilter(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/usage/summary?from=2026-10-01&to=2026-10-02&group_by=day&status=429&limit=25", nil)
	recorder := httptest.NewRecorder()
	filter, ok := parseUsageFilter(recorder, request)
	if !ok {
		t.Fatalf("parseUsageFilter failed: %s", recorder.Body.String())
	}
	if filter.GroupBy != "day" || filter.Limit != 25 || filter.Status == nil || *filter.Status != 429 {
		t.Fatalf("unexpected filter: %+v", filter)
	}
	if filter.To.Sub(filter.From) != 48*time.Hour {
		t.Fatalf("range = %v", filter.To.Sub(filter.From))
	}
	bad := httptest.NewRequest(http.MethodGet, "/api/usage/summary?order=sideways", nil)
	if _, ok := parseUsageFilter(httptest.NewRecorder(), bad); ok {
		t.Fatal("expected invalid order to fail")
	}
}
