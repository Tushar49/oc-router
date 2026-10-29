package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Summary aggregates usage. Recent ranges come from raw events; ranges older
// than the retention window come from the daily rollup; a range crossing the
// boundary is merged. Status and outcome filters force a raw-only response
// because the rollup does not carry those dimensions.
func (s *Store) Summary(ctx context.Context, filter Filter) (Summary, error) {
	from, to, err := normalizeRange(filter)
	if err != nil {
		return Summary{}, err
	}
	if filter.GroupBy == "" {
		filter.GroupBy = "provider_model"
	}
	if filter.Sort == "" {
		filter.Sort = "total_tokens"
	}
	if filter.Order != "asc" {
		filter.Order = "desc"
	}
	if filter.Limit <= 0 {
		filter.Limit = 100
	}
	if filter.Limit > 500 {
		filter.Limit = 500
	}
	filter.From, filter.To = from, to

	s.mu.Lock()
	retention := s.retentionDays
	s.mu.Unlock()
	cutoff := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -retention)

	result := Summary{Range: Range{From: from, To: to, Timezone: "UTC"}}
	rowFilters := filter.Status != nil || filter.Outcome != ""
	switch {
	case rowFilters:
		rawFrom := from
		if rawFrom.Before(cutoff) {
			rawFrom = cutoff
			result.Truncated = true
		}
		totals, groups, err := s.rawSummary(ctx, filter, rawFrom, to)
		if err != nil {
			return Summary{}, err
		}
		result.Totals, result.Groups, result.Source = totals, groups, "raw"
	case to.After(cutoff) && from.Before(cutoff):
		dailyTotals, dailyGroups, err := s.dailySummary(ctx, filter, from, cutoff)
		if err != nil {
			return Summary{}, err
		}
		rawTotals, rawGroups, err := s.rawSummary(ctx, filter, cutoff, to)
		if err != nil {
			return Summary{}, err
		}
		result.Totals = dailyTotals
		result.Totals.add(rawTotals)
		result.Groups = mergeGroups(dailyGroups, rawGroups)
		result.Source = "mixed"
	case !to.After(cutoff):
		totals, groups, err := s.dailySummary(ctx, filter, from, to)
		if err != nil {
			return Summary{}, err
		}
		result.Totals, result.Groups, result.Source = totals, groups, "rollup"
	default:
		totals, groups, err := s.rawSummary(ctx, filter, from, to)
		if err != nil {
			return Summary{}, err
		}
		result.Totals, result.Groups, result.Source = totals, groups, "raw"
	}
	result.Totals.finish()
	for index := range result.Groups {
		result.Groups[index].finish()
	}
	sortGroups(result.Groups, filter.Sort, filter.Order)
	if len(result.Groups) > filter.Limit {
		result.Groups = result.Groups[:filter.Limit]
		result.Truncated = true
	}
	if result.Groups == nil {
		result.Groups = []Group{}
	}
	return result, nil
}

// Timeseries returns daily totals for the requested range. The rollup is
// upserted on insert, so it also covers recent days without reading events.
func (s *Store) Timeseries(ctx context.Context, filter Filter) ([]Point, error) {
	from, to, err := normalizeRange(filter)
	if err != nil {
		return nil, err
	}
	if filter.Status != nil || filter.Outcome != "" {
		return s.rawTimeseries(ctx, filter, from, to)
	}
	where, args := dailyWhere(filter, from, to)
	query := `SELECT day,
		COALESCE(SUM(requests), 0), COALESCE(SUM(success), 0), COALESCE(SUM(errors), 0),
		COALESCE(SUM(input_tokens), 0), COALESCE(SUM(cached_tokens), 0), COALESCE(SUM(cache_write_tokens), 0),
		COALESCE(SUM(output_tokens), 0), COALESCE(SUM(reasoning_tokens), 0), COALESCE(SUM(total_tokens), 0),
		COALESCE(SUM(cost_micro_usd), 0), COALESCE(SUM(usage_reported), 0), COALESCE(SUM(priced_requests), 0)
	FROM usage_daily WHERE ` + strings.Join(where, " AND ") + ` GROUP BY day ORDER BY day`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query usage timeseries: %w", err)
	}
	defer rows.Close()
	points := make([]Point, 0)
	for rows.Next() {
		var point Point
		if err := rows.Scan(&point.Day, &point.Requests, &point.Success, &point.Errors,
			&point.InputTokens, &point.CachedTokens, &point.CacheWriteTokens,
			&point.OutputTokens, &point.ReasoningTokens, &point.TotalTokens,
			&point.costMicroUSD, &point.usageReported, &point.priced); err != nil {
			return nil, fmt.Errorf("scan usage timeseries: %w", err)
		}
		point.finish()
		points = append(points, point)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read usage timeseries: %w", err)
	}
	return points, nil
}

func (s *Store) rawTimeseries(ctx context.Context, filter Filter, from, to time.Time) ([]Point, error) {
	where, args := rawWhere(filter)
	where = append([]string{"ts >= ?", "ts < ?"}, where...)
	args = append([]any{formatTS(from), formatTS(to)}, args...)
	query := `SELECT day, COUNT(*), COALESCE(SUM(success), 0),
		COALESCE(SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(input_tokens), 0), COALESCE(SUM(cached_tokens), 0), COALESCE(SUM(cache_write_tokens), 0),
		COALESCE(SUM(output_tokens), 0), COALESCE(SUM(reasoning_tokens), 0), COALESCE(SUM(total_tokens), 0),
		COALESCE(SUM(cost_micro_usd), 0),
		COALESCE(SUM(CASE WHEN usage_source = 'provider' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN cost_micro_usd IS NOT NULL THEN 1 ELSE 0 END), 0)
	FROM usage_event WHERE ` + strings.Join(where, " AND ") + ` GROUP BY day ORDER BY day`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query raw usage timeseries: %w", err)
	}
	defer rows.Close()
	points := make([]Point, 0)
	for rows.Next() {
		var point Point
		if err := rows.Scan(&point.Day, &point.Requests, &point.Success, &point.Errors,
			&point.InputTokens, &point.CachedTokens, &point.CacheWriteTokens,
			&point.OutputTokens, &point.ReasoningTokens, &point.TotalTokens,
			&point.costMicroUSD, &point.usageReported, &point.priced); err != nil {
			return nil, fmt.Errorf("scan raw usage timeseries: %w", err)
		}
		point.finish()
		points = append(points, point)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read raw usage timeseries: %w", err)
	}
	return points, nil
}

// Events returns raw usage events newest first. beforeID paginates by the
// monotonically increasing row id.
func (s *Store) Events(ctx context.Context, filter Filter, beforeID int64, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 5000 {
		limit = 5000
	}
	where, args := rawWhere(filter)
	if !filter.From.IsZero() {
		where = append(where, "ts >= ?")
		args = append(args, formatTS(filter.From.UTC()))
	}
	if !filter.To.IsZero() {
		where = append(where, "ts < ?")
		args = append(args, formatTS(filter.To.UTC()))
	}
	if beforeID > 0 {
		where = append(where, "id < ?")
		args = append(args, beforeID)
	}
	query := `SELECT id, request_id, ts, day, provider, model, endpoint, tier, server_key_fp, session_fp,
		status, outcome, success, error_code, latency_ms, ttft_ms,
		input_tokens, cached_tokens, cache_write_tokens, output_tokens, reasoning_tokens, total_tokens,
		usage_source, pricing_source, cost_micro_usd, cost_input_micro_usd, cost_output_micro_usd, cost_cache_micro_usd,
		stream, attempts, proxy_kind, proxy_id, saver_json, meta_json
	FROM usage_event WHERE ` + strings.Join(where, " AND ") + ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query usage events: %w", err)
	}
	defer rows.Close()
	events := make([]Event, 0)
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read usage events: %w", err)
	}
	return events, nil
}

func (s *Store) rawSummary(ctx context.Context, filter Filter, from, to time.Time) (Totals, []Group, error) {
	where, args := rawWhere(filter)
	where = append([]string{"ts >= ?", "ts < ?"}, where...)
	args = append([]any{formatTS(from), formatTS(to)}, args...)
	prefix := ` FROM usage_event WHERE ` + strings.Join(where, " AND ")

	var totals Totals
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(success), 0),
		COALESCE(SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(input_tokens), 0), COALESCE(SUM(cached_tokens), 0), COALESCE(SUM(cache_write_tokens), 0),
		COALESCE(SUM(output_tokens), 0), COALESCE(SUM(reasoning_tokens), 0), COALESCE(SUM(total_tokens), 0),
		COALESCE(SUM(CASE WHEN usage_source = 'provider' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN cost_micro_usd IS NOT NULL THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(cost_micro_usd), 0)`+prefix, args...).Scan(
		&totals.Requests, &totals.Success, &totals.Errors,
		&totals.InputTokens, &totals.CachedTokens, &totals.CacheWriteTokens,
		&totals.OutputTokens, &totals.ReasoningTokens, &totals.TotalTokens,
		&totals.usageReported, &totals.priced, &totals.costMicroUSD,
	); err != nil {
		return Totals{}, nil, fmt.Errorf("query usage totals: %w", err)
	}
	totals.finish()

	columns := groupColumns(filter.GroupBy)
	columnSQL := strings.Join(columns, ", ")
	query := `SELECT ` + columnSQL + `, COUNT(*), COALESCE(SUM(success), 0),
		COALESCE(SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(input_tokens), 0), COALESCE(SUM(cached_tokens), 0), COALESCE(SUM(cache_write_tokens), 0),
		COALESCE(SUM(output_tokens), 0), COALESCE(SUM(reasoning_tokens), 0), COALESCE(SUM(total_tokens), 0),
		COALESCE(SUM(CASE WHEN usage_source = 'provider' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN cost_micro_usd IS NOT NULL THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(cost_micro_usd), 0), COALESCE(MAX(pricing_source), '')` + prefix +
		` GROUP BY ` + columnSQL
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return Totals{}, nil, fmt.Errorf("query usage groups: %w", err)
	}
	defer rows.Close()
	var groups []Group
	for rows.Next() {
		group, err := scanGroup(rows, len(columns), filter.GroupBy)
		if err != nil {
			return Totals{}, nil, err
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return Totals{}, nil, fmt.Errorf("read usage groups: %w", err)
	}
	return totals, groups, nil
}

func (s *Store) dailySummary(ctx context.Context, filter Filter, from, to time.Time) (Totals, []Group, error) {
	where, args := dailyWhere(filter, from, to)
	prefix := ` FROM usage_daily WHERE ` + strings.Join(where, " AND ")
	var totals Totals
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(requests), 0), COALESCE(SUM(success), 0), COALESCE(SUM(errors), 0),
		COALESCE(SUM(input_tokens), 0), COALESCE(SUM(cached_tokens), 0), COALESCE(SUM(cache_write_tokens), 0),
		COALESCE(SUM(output_tokens), 0), COALESCE(SUM(reasoning_tokens), 0), COALESCE(SUM(total_tokens), 0),
		COALESCE(SUM(usage_reported), 0), COALESCE(SUM(priced_requests), 0), COALESCE(SUM(cost_micro_usd), 0)`+
		prefix, args...).Scan(
		&totals.Requests, &totals.Success, &totals.Errors,
		&totals.InputTokens, &totals.CachedTokens, &totals.CacheWriteTokens,
		&totals.OutputTokens, &totals.ReasoningTokens, &totals.TotalTokens,
		&totals.usageReported, &totals.priced, &totals.costMicroUSD,
	); err != nil {
		return Totals{}, nil, fmt.Errorf("query daily totals: %w", err)
	}
	totals.finish()

	columns := groupColumns(filter.GroupBy)
	columnSQL := strings.Join(columns, ", ")
	query := `SELECT ` + columnSQL + `, COALESCE(SUM(requests), 0), COALESCE(SUM(success), 0), COALESCE(SUM(errors), 0),
		COALESCE(SUM(input_tokens), 0), COALESCE(SUM(cached_tokens), 0), COALESCE(SUM(cache_write_tokens), 0),
		COALESCE(SUM(output_tokens), 0), COALESCE(SUM(reasoning_tokens), 0), COALESCE(SUM(total_tokens), 0),
		COALESCE(SUM(usage_reported), 0), COALESCE(SUM(priced_requests), 0), COALESCE(SUM(cost_micro_usd), 0), ''` +
		prefix + ` GROUP BY ` + columnSQL
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return Totals{}, nil, fmt.Errorf("query daily groups: %w", err)
	}
	defer rows.Close()
	var groups []Group
	for rows.Next() {
		group, err := scanGroup(rows, len(columns), filter.GroupBy)
		if err != nil {
			return Totals{}, nil, err
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return Totals{}, nil, fmt.Errorf("read daily groups: %w", err)
	}
	return totals, groups, nil
}

func groupColumns(groupBy string) []string {
	switch groupBy {
	case "provider":
		return []string{"provider"}
	case "model":
		return []string{"model"}
	case "endpoint":
		return []string{"endpoint"}
	case "day":
		return []string{"day"}
	case "server_key":
		return []string{"COALESCE(server_key_fp, '')"}
	default:
		return []string{"provider", "model"}
	}
}

func scanGroup(rows *sql.Rows, columns int, groupBy string) (Group, error) {
	values := make([]string, columns)
	targets := make([]any, columns)
	for index := range values {
		targets[index] = &values[index]
	}
	var group Group
	targets = append(targets, &group.Requests, &group.Success, &group.Errors,
		&group.InputTokens, &group.CachedTokens, &group.CacheWriteTokens,
		&group.OutputTokens, &group.ReasoningTokens, &group.TotalTokens,
		&group.usageReported, &group.priced, &group.costMicroUSD, &group.PricingSource)
	if err := rows.Scan(targets...); err != nil {
		return Group{}, fmt.Errorf("scan usage group: %w", err)
	}
	switch groupBy {
	case "provider":
		group.Provider = values[0]
	case "model":
		group.Model = values[0]
	case "endpoint":
		group.Endpoint = values[0]
	case "day":
		group.Day = values[0]
	case "server_key":
		group.ServerKeyFP = values[0]
	default:
		group.Provider = values[0]
		group.Model = values[1]
	}
	group.finish()
	return group, nil
}

func scanEvent(rows *sql.Rows) (Event, error) {
	var (
		event                                        Event
		ts                                           string
		serverKey, session, errorCode, pricingSource sql.NullString
		ttft, cost, costInput, costOutput, costCache sql.NullInt64
		proxyKind, proxyID, saver, meta              sql.NullString
		successInt                                   int64
	)
	if err := rows.Scan(&event.ID, &event.RequestID, &ts, &event.Day, &event.Provider, &event.Model,
		&event.Endpoint, &event.Tier, &serverKey, &session, &event.Status, &event.Outcome, &successInt,
		&errorCode, &event.LatencyMS, &ttft, &event.InputTokens, &event.CachedTokens, &event.CacheWriteTokens,
		&event.OutputTokens, &event.ReasoningTokens, &event.TotalTokens, &event.UsageSource, &pricingSource,
		&cost, &costInput, &costOutput, &costCache, &event.Stream, &event.Attempts, &proxyKind, &proxyID,
		&saver, &meta); err != nil {
		return Event{}, fmt.Errorf("scan usage event: %w", err)
	}
	parsed, err := time.Parse(tsLayout, ts)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339Nano, ts)
	}
	if err != nil {
		return Event{}, fmt.Errorf("parse usage timestamp %q: %w", ts, err)
	}
	event.TS = parsed
	event.Success = successInt != 0
	event.ServerKeyFP = nullStringPtr(serverKey)
	event.SessionFP = nullStringPtr(session)
	event.ErrorCode = nullStringPtr(errorCode)
	event.TTFTMS = nullInt64Ptr(ttft)
	event.PricingSource = nullStringPtr(pricingSource)
	event.CostMicroUSD = nullInt64Ptr(cost)
	event.CostInputMicroUSD = nullInt64Ptr(costInput)
	event.CostOutputMicroUSD = nullInt64Ptr(costOutput)
	event.CostCacheMicroUSD = nullInt64Ptr(costCache)
	event.ProxyKind = nullStringPtr(proxyKind)
	event.ProxyID = nullStringPtr(proxyID)
	event.SaverJSON = nullStringPtr(saver)
	event.MetaJSON = nullStringPtr(meta)
	return event, nil
}

func normalizeRange(filter Filter) (time.Time, time.Time, error) {
	to := filter.To.UTC()
	if to.IsZero() {
		to = time.Now().UTC()
	}
	from := filter.From.UTC()
	if from.IsZero() {
		from = to.Add(-7 * 24 * time.Hour)
	}
	if !to.After(from) {
		return time.Time{}, time.Time{}, errors.New("usage range end must be after start")
	}
	return from, to, nil
}

func rawWhere(filter Filter) ([]string, []any) {
	where := []string{"1 = 1"}
	args := []any{}
	if filter.Provider != "" {
		where = append(where, "provider = ?")
		args = append(args, filter.Provider)
	}
	if filter.Model != "" {
		where = append(where, "model = ?")
		args = append(args, filter.Model)
	}
	if filter.Endpoint != "" {
		where = append(where, "endpoint = ?")
		args = append(args, filter.Endpoint)
	}
	if filter.Tier != "" {
		where = append(where, "tier = ?")
		args = append(args, filter.Tier)
	}
	if filter.ServerKeyFP != "" {
		where = append(where, "server_key_fp = ?")
		args = append(args, filter.ServerKeyFP)
	}
	if filter.Outcome != "" {
		where = append(where, "outcome = ?")
		args = append(args, filter.Outcome)
	}
	if filter.Status != nil {
		where = append(where, "status = ?")
		args = append(args, *filter.Status)
	}
	return where, args
}

func dailyWhere(filter Filter, from, to time.Time) ([]string, []any) {
	where := []string{"day >= ?", "day <= ?"}
	args := []any{from.UTC().Format(dayLayout), to.UTC().Add(-time.Nanosecond).Format(dayLayout)}
	if filter.Provider != "" {
		where = append(where, "provider = ?")
		args = append(args, filter.Provider)
	}
	if filter.Model != "" {
		where = append(where, "model = ?")
		args = append(args, filter.Model)
	}
	if filter.Endpoint != "" {
		where = append(where, "endpoint = ?")
		args = append(args, filter.Endpoint)
	}
	if filter.Tier != "" {
		where = append(where, "tier = ?")
		args = append(args, filter.Tier)
	}
	if filter.ServerKeyFP != "" {
		where = append(where, "server_key_fp = ?")
		args = append(args, filter.ServerKeyFP)
	}
	return where, args
}

func (t *Totals) finish() {
	t.Currency = "USD"
	if t.Requests > 0 {
		t.UsageCoverage = float64(t.usageReported) / float64(t.Requests)
		t.CostCoverage = float64(t.priced) / float64(t.Requests)
	}
	if t.priced > 0 {
		value := float64(t.costMicroUSD) / 1e6
		t.EstimatedCostUSD = &value
	}
}

func (t *Totals) add(other Totals) {
	t.Requests += other.Requests
	t.Success += other.Success
	t.Errors += other.Errors
	t.InputTokens += other.InputTokens
	t.CachedTokens += other.CachedTokens
	t.CacheWriteTokens += other.CacheWriteTokens
	t.OutputTokens += other.OutputTokens
	t.ReasoningTokens += other.ReasoningTokens
	t.TotalTokens += other.TotalTokens
	t.costMicroUSD += other.costMicroUSD
	t.usageReported += other.usageReported
	t.priced += other.priced
}

func mergeGroups(first, second []Group) []Group {
	merged := make(map[string]Group, len(first)+len(second))
	for _, group := range append(append([]Group(nil), first...), second...) {
		key := group.Provider + "\x00" + group.Model + "\x00" + group.Endpoint + "\x00" + group.Day + "\x00" + group.ServerKeyFP
		existing, ok := merged[key]
		if !ok {
			merged[key] = group
			continue
		}
		existing.add(group.Totals)
		if existing.PricingSource == "" {
			existing.PricingSource = group.PricingSource
		}
		existing.finish()
		merged[key] = existing
	}
	groups := make([]Group, 0, len(merged))
	for _, group := range merged {
		groups = append(groups, group)
	}
	return groups
}

func sortGroups(groups []Group, key, order string) {
	value := func(group Group) float64 {
		switch key {
		case "requests":
			return float64(group.Requests)
		case "success":
			return float64(group.Success)
		case "errors":
			return float64(group.Errors)
		case "input_tokens":
			return float64(group.InputTokens)
		case "cached_tokens":
			return float64(group.CachedTokens)
		case "cache_write_tokens":
			return float64(group.CacheWriteTokens)
		case "output_tokens":
			return float64(group.OutputTokens)
		case "reasoning_tokens":
			return float64(group.ReasoningTokens)
		case "cost", "cost_micro_usd", "estimated_cost_usd":
			return float64(group.costMicroUSD)
		case "usage_coverage":
			return group.UsageCoverage
		case "cost_coverage":
			return group.CostCoverage
		default:
			return float64(group.TotalTokens)
		}
	}
	sort.SliceStable(groups, func(i, j int) bool {
		left, right := value(groups[i]), value(groups[j])
		if math.Abs(left-right) > 1e-9 {
			if order == "asc" {
				return left < right
			}
			return left > right
		}
		leftKey := groups[i].Provider + groups[i].Model + groups[i].Endpoint + groups[i].Day + groups[i].ServerKeyFP
		rightKey := groups[j].Provider + groups[j].Model + groups[j].Endpoint + groups[j].Day + groups[j].ServerKeyFP
		if order == "asc" {
			return leftKey < rightKey
		}
		return leftKey > rightKey
	})
}

func nullStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func nullInt64Ptr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}
