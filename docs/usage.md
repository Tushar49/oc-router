# Usage and cost

Status: Phase 2 implementation. The gateway writes one metadata-only row per
client inference request to a local SQLite database and serves summaries,
events, daily timeseries, CSV/JSON export, and on-demand backups.

## Configuration

```json
"usage": {
  "enabled": true,
  "database": "usage.db",
  "retention_days": 365,
  "batch_size": 100,
  "flush_interval_ms": 100,
  "query_timeout_ms": 5000
}
```

- `database` is resolved against the directory of `config.json`; an absolute
  path is used as-is. The default is `usage.db` beside the config.
- `retention_days` accepts `7` to `3650`. Raw events older than the cutoff are
  rolled up and deleted; daily rows are kept.
- `batch_size` and `flush_interval_ms` bound the asynchronous writer.
- `query_timeout_ms` is the admin summary/events/timeseries read deadline.
- Changing these values requires a restart. Other configuration saves preserve
  the current `usage` block.

The database and its `-wal`/`-shm` files are runtime artifacts and are ignored
by git. Backups are written beside the database as `backup-<UTC>.db`.

## Capture

The recorder is attached inside the telemetry middleware, so it sees the
canonical `RequestMeta` after routing and streaming finish:

- non-streaming: after the upstream response is fully read;
- streaming: after `[DONE]`, `response.completed`, `message_stop`, an error, or
  a client disconnect;
- upstream error: the HTTP status and terminal outcome, with whatever usage
  arrived;
- client cancel: the partial usage, `outcome=client_canceled`, and
  `success=0`.

The response path never waits for SQLite. Events enter a bounded queue (at
least 1024 entries) and one writer batches them. A full queue drops events,
reported in the `usage_recorder` warning log; the gateway still serves the
request.

## Canonical tokens

`protocol.Usage.Input` is the bridge-wide prompt total, including cache reads
and cache writes. The usage row stores the non-overlapping parts:

| Field | Derivation |
|---|---|
| `input_tokens` | `max(Input - Cached - CacheCreation, 0)` |
| `cached_tokens` | `Cached` |
| `cache_write_tokens` | `CacheCreation` |
| `output_tokens` | `Output` (reasoning stays inside output) |
| `reasoning_tokens` | `Reasoning` (display only) |
| `total_tokens` | `input + cached + cache_write + output` |

Provider `total_tokens` is not trusted for UI sums. If it differs from the
canonical sum, the provider value is kept in `meta_json.provider_total_tokens`
for audit.

`usage_source` is `provider` when the provider reported a usage object,
`partial` when only some token counts arrived, and `unavailable` when no usage
arrived. The UI must not present inferred numbers as exact.

## Cost

Prices come from the OpenCode metadata registry
(`internal/models.PricingRegistry`) and are stored in the event as:

- `cost_micro_usd` (integer micro-USD, 1e-6 USD);
- `cost_input_micro_usd`;
- `cost_output_micro_usd`;
- `cost_cache_micro_usd` (cache read + cache write);
- `pricing_source` (`opencode`, `hardcoded`, or `none`).

The formula is a half-up round of each component:

```
input  = round(input_tokens       * input_per_mtok)
output = round(output_tokens      * output_per_mtok)
cache  = round(cached_tokens      * cache_read_per_mtok)
       + round(cache_write_tokens * cache_write_per_mtok)
total  = input + output + cache
```

A missing price for a token component that was actually used leaves the cost
`NULL` and `pricing_source=none`; the UI shows `—`, not `$0.00`. A model whose
name contains `free`, or whose metadata price is zero, is priced at `$0.00`.
Cost is not computed for `partial` or `unavailable` usage because the token
counts are incomplete.

## Storage

Migration `0001_init.sql` creates:

- `usage_event`: one row per client request, indexed by time, day,
  provider/model, status and server-key fingerprint;
- `usage_daily`: an upserted UTC-day rollup (`requests`, `success`, `errors`,
  tokens, cost, `usage_reported`, `priced_requests`);
- `rollup_state`: committed day rollups, used before deletion;
- `pricing`: reserved for manual price overrides.

The schema is embedded and forward-only. A database written by a newer binary
fails startup with `ErrSchemaNewer`; back it up and use the newer build.

`usage_event.request_id` is a new internal UUID, not the upstream request ID.
`session_fp`, `server_key_fp`, and `proxy_id` are short SHA-256 fingerprints.
The raw session, API key, proxy URL, and IP addresses are never stored.

## API

All routes are authenticated by the admin session; the mutating backup route
also requires CSRF.

| Method | Route | Notes |
|---|---|---|
| GET | `/api/usage/summary` | `from`, `to`, `group_by`, `sort`, `order`, filters |
| GET | `/api/usage/events` | `before_id`, `limit` (max 500), filters |
| GET | `/api/usage/timeseries` | daily totals for the range |
| GET | `/api/usage/export` | `format=csv|json`, active filters |
| POST | `/api/usage/backup` | `VACUUM INTO` beside the database |

Filters: `provider`, `model`, `endpoint`, `tier`, `server_key`, `status`,
`outcome`. Times accept RFC3339 or `YYYY-MM-DD`; a date-only `to` is inclusive
through the end of that UTC day.

`group_by`: `provider`, `model`, `provider_model` (default), `endpoint`, `day`,
`server_key`. Summary rows are raw events for recent ranges, the daily rollup
for ranges older than retention, and a merged `mixed` response when the range
crosses the cutoff. Status/outcome filters force raw-only results and set
`truncated=true` when the requested start predates raw retention.

## Retention and backup

The retention loop runs one minute after startup and then daily:

1. find days older than the cutoff without a completed rollup;
2. recompute `usage_daily` from raw events and commit `rollup_state`;
3. delete only raw rows on days with a completed rollup;
4. checkpoint the WAL.

Deletion is skipped if a rollup fails. `POST /api/usage/backup` writes a
consistent `backup-<timestamp>.db`; restore is manual: stop the process,
replace the database, restart.

## Privacy and limits

- No prompts, responses, tool arguments, file paths, cookies, authorization
  headers, or raw API keys.
- `meta_json` is an allowlist: schema version, provider total discrepancy, and
  attempt outcome.
- The database is local and **not encrypted at rest**.
- One row is written per client request, not per upstream retry. Failed
  attempts' unreported token consumption is not invented.
- Latency is end-to-end; `ttft_ms` is the first body write. Daily rollups do
  not carry status/outcome dimensions or latency percentiles.
