package admin

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"opencode2api/internal/httpx"
	"opencode2api/internal/store"
)

func (a *Server) handleUsageSummary(w http.ResponseWriter, r *http.Request) {
	if !a.usageReady(w) {
		return
	}
	filter, ok := parseUsageFilter(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), a.usage.QueryTimeout())
	defer cancel()
	summary, err := a.usage.Summary(ctx, filter)
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "usage_query_failed", err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, summary)
}

func (a *Server) handleUsageEvents(w http.ResponseWriter, r *http.Request) {
	if !a.usageReady(w) {
		return
	}
	filter, ok := parseUsageFilter(w, r)
	if !ok {
		return
	}
	beforeID, _ := strconv.ParseInt(r.URL.Query().Get("before_id"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	ctx, cancel := context.WithTimeout(r.Context(), a.usage.QueryTimeout())
	defer cancel()
	events, err := a.usage.Events(ctx, filter, beforeID, limit)
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "usage_query_failed", err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (a *Server) handleUsageTimeseries(w http.ResponseWriter, r *http.Request) {
	if !a.usageReady(w) {
		return
	}
	filter, ok := parseUsageFilter(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), a.usage.QueryTimeout())
	defer cancel()
	points, err := a.usage.Timeseries(ctx, filter)
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "usage_query_failed", err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"points": points})
}

func (a *Server) handleUsageExport(w http.ResponseWriter, r *http.Request) {
	if !a.usageReady(w) {
		return
	}
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format != "csv" && format != "json" {
		writeAdminError(w, http.StatusBadRequest, "invalid_format", "format must be csv or json")
		return
	}
	filter, ok := parseUsageFilter(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	events, truncated, err := exportEvents(ctx, a, filter)
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "usage_export_failed", err.Error())
		return
	}
	filename := "usage-" + time.Now().UTC().Format("20060102-150405")
	if format == "json" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`.json"`)
		w.Header().Set("Cache-Control", "no-store")
		payload := map[string]any{
			"metadata": map[string]any{
				"generated_at":   time.Now().UTC(),
				"schema_version": 1,
				"truncated":      truncated,
				"filters":        filterView(filter),
			},
			"events": events,
		}
		_ = json.NewEncoder(w).Encode(payload)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`.csv"`)
	w.Header().Set("Cache-Control", "no-store")
	writer := csv.NewWriter(w)
	_ = writer.Write([]string{
		"id", "request_id", "ts", "day", "provider", "model", "endpoint", "tier",
		"server_key_fp", "session_fp", "status", "outcome", "success", "error_code",
		"latency_ms", "ttft_ms", "input_tokens", "cached_tokens", "cache_write_tokens",
		"output_tokens", "reasoning_tokens", "total_tokens", "usage_source", "pricing_source",
		"cost_micro_usd", "cost_input_micro_usd", "cost_output_micro_usd", "cost_cache_micro_usd",
		"stream", "attempts", "proxy_kind", "proxy_id", "saver_json", "meta_json",
	})
	for _, event := range events {
		_ = writer.Write([]string{
			strconv.FormatInt(event.ID, 10), event.RequestID, event.TS.UTC().Format(time.RFC3339Nano), event.Day,
			event.Provider, event.Model, event.Endpoint, event.Tier, ptrString(event.ServerKeyFP), ptrString(event.SessionFP),
			strconv.Itoa(event.Status), event.Outcome, strconv.FormatBool(event.Success), ptrString(event.ErrorCode),
			strconv.FormatInt(event.LatencyMS, 10), ptrInt64(event.TTFTMS), strconv.FormatInt(event.InputTokens, 10),
			strconv.FormatInt(event.CachedTokens, 10), strconv.FormatInt(event.CacheWriteTokens, 10),
			strconv.FormatInt(event.OutputTokens, 10), strconv.FormatInt(event.ReasoningTokens, 10),
			strconv.FormatInt(event.TotalTokens, 10), event.UsageSource, ptrString(event.PricingSource),
			ptrInt64(event.CostMicroUSD), ptrInt64(event.CostInputMicroUSD), ptrInt64(event.CostOutputMicroUSD),
			ptrInt64(event.CostCacheMicroUSD), strconv.FormatBool(event.Stream), strconv.Itoa(event.Attempts),
			ptrString(event.ProxyKind), ptrString(event.ProxyID), ptrString(event.SaverJSON), ptrString(event.MetaJSON),
		})
	}
	writer.Flush()
}

func (a *Server) handleUsageBackup(w http.ResponseWriter, r *http.Request) {
	if !a.usageReady(w) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	path, err := a.usage.Backup(ctx)
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "usage_backup_failed", err.Error())
		return
	}
	if a.logger != nil {
		a.logger.Info("usage database backup created", "component", "usage", "event", "usage_backup", "path", path)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"path": path})
}

func (a *Server) usageReady(w http.ResponseWriter) bool {
	if a.usage == nil {
		writeAdminError(w, http.StatusServiceUnavailable, "usage_disabled", "usage database is disabled")
		return false
	}
	return true
}

func exportEvents(ctx context.Context, a *Server, filter store.Filter) ([]store.Event, bool, error) {
	const pageSize = 1000
	const maxRows = 200000
	all := make([]store.Event, 0, pageSize)
	var before int64
	for {
		events, err := a.usage.Events(ctx, filter, before, pageSize)
		if err != nil {
			return nil, false, err
		}
		all = append(all, events...)
		if len(events) < pageSize {
			return all, false, nil
		}
		before = events[len(events)-1].ID
		if len(all) >= maxRows {
			return all[:maxRows], true, nil
		}
	}
}

func parseUsageFilter(w http.ResponseWriter, r *http.Request) (store.Filter, bool) {
	query := r.URL.Query()
	var filter store.Filter
	var err error
	if value := strings.TrimSpace(query.Get("from")); value != "" {
		filter.From, err = parseUsageTime(value, false)
		if err != nil {
			writeAdminError(w, http.StatusBadRequest, "invalid_range", "from: "+err.Error())
			return filter, false
		}
	}
	if value := strings.TrimSpace(query.Get("to")); value != "" {
		filter.To, err = parseUsageTime(value, true)
		if err != nil {
			writeAdminError(w, http.StatusBadRequest, "invalid_range", "to: "+err.Error())
			return filter, false
		}
	}
	filter.Provider = strings.TrimSpace(query.Get("provider"))
	filter.Model = strings.TrimSpace(query.Get("model"))
	filter.Endpoint = strings.TrimSpace(query.Get("endpoint"))
	filter.Tier = strings.TrimSpace(query.Get("tier"))
	filter.ServerKeyFP = strings.TrimSpace(query.Get("server_key"))
	filter.Outcome = strings.TrimSpace(query.Get("outcome"))
	if value := strings.TrimSpace(query.Get("status")); value != "" {
		status, err := strconv.Atoi(value)
		if err != nil || status < 100 || status > 599 {
			writeAdminError(w, http.StatusBadRequest, "invalid_status", "status must be an HTTP status code")
			return filter, false
		}
		filter.Status = &status
	}
	filter.GroupBy = strings.TrimSpace(query.Get("group_by"))
	filter.Sort = strings.TrimSpace(query.Get("sort"))
	filter.Order = strings.TrimSpace(query.Get("order"))
	if filter.Order != "" && filter.Order != "asc" && filter.Order != "desc" {
		writeAdminError(w, http.StatusBadRequest, "invalid_order", "order must be asc or desc")
		return filter, false
	}
	if value := strings.TrimSpace(query.Get("limit")); value != "" {
		limit, err := strconv.Atoi(value)
		if err != nil || limit < 1 {
			writeAdminError(w, http.StatusBadRequest, "invalid_limit", "limit must be a positive integer")
			return filter, false
		}
		filter.Limit = limit
	}
	return filter, true
}

func parseUsageTime(value string, end bool) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		parsed, err := time.Parse(layout, value)
		if err != nil {
			continue
		}
		if layout == "2006-01-02" && end {
			parsed = parsed.Add(24 * time.Hour)
		}
		return parsed.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("expected RFC3339 or YYYY-MM-DD")
}

func filterView(filter store.Filter) map[string]any {
	view := map[string]any{
		"provider": filter.Provider, "model": filter.Model, "endpoint": filter.Endpoint,
		"tier": filter.Tier, "server_key": filter.ServerKeyFP, "outcome": filter.Outcome,
		"group_by": filter.GroupBy, "sort": filter.Sort, "order": filter.Order,
	}
	if !filter.From.IsZero() {
		view["from"] = filter.From.UTC()
	}
	if !filter.To.IsZero() {
		view["to"] = filter.To.UTC()
	}
	if filter.Status != nil {
		view["status"] = *filter.Status
	}
	return view
}

func ptrString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func ptrInt64(value *int64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatInt(*value, 10)
}
