CREATE TABLE usage_event (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  request_id TEXT NOT NULL UNIQUE,
  ts TEXT NOT NULL,
  day TEXT NOT NULL,
  provider TEXT NOT NULL,
  model TEXT NOT NULL,
  endpoint TEXT NOT NULL,
  tier TEXT NOT NULL,
  server_key_fp TEXT,
  session_fp TEXT,
  status INTEGER NOT NULL,
  outcome TEXT NOT NULL,
  success INTEGER NOT NULL,
  error_code TEXT,
  latency_ms INTEGER NOT NULL DEFAULT 0,
  ttft_ms INTEGER,
  input_tokens INTEGER NOT NULL DEFAULT 0,
  cached_tokens INTEGER NOT NULL DEFAULT 0,
  cache_write_tokens INTEGER NOT NULL DEFAULT 0,
  output_tokens INTEGER NOT NULL DEFAULT 0,
  reasoning_tokens INTEGER NOT NULL DEFAULT 0,
  total_tokens INTEGER NOT NULL DEFAULT 0,
  usage_source TEXT NOT NULL,
  pricing_source TEXT,
  cost_micro_usd INTEGER,
  cost_input_micro_usd INTEGER,
  cost_output_micro_usd INTEGER,
  cost_cache_micro_usd INTEGER,
  stream INTEGER NOT NULL DEFAULT 0,
  attempts INTEGER NOT NULL DEFAULT 1,
  proxy_kind TEXT,
  proxy_id TEXT,
  saver_json TEXT,
  meta_json TEXT
);

CREATE INDEX idx_usage_ts ON usage_event(ts);
CREATE INDEX idx_usage_day ON usage_event(day);
CREATE INDEX idx_usage_provider_model ON usage_event(provider, model);
CREATE INDEX idx_usage_status ON usage_event(status);
CREATE INDEX idx_usage_key ON usage_event(server_key_fp);

CREATE TABLE usage_daily (
  day TEXT NOT NULL,
  provider TEXT NOT NULL,
  model TEXT NOT NULL,
  endpoint TEXT NOT NULL,
  tier TEXT NOT NULL,
  server_key_fp TEXT NOT NULL DEFAULT '',
  requests INTEGER NOT NULL DEFAULT 0,
  success INTEGER NOT NULL DEFAULT 0,
  errors INTEGER NOT NULL DEFAULT 0,
  input_tokens INTEGER NOT NULL DEFAULT 0,
  cached_tokens INTEGER NOT NULL DEFAULT 0,
  cache_write_tokens INTEGER NOT NULL DEFAULT 0,
  output_tokens INTEGER NOT NULL DEFAULT 0,
  reasoning_tokens INTEGER NOT NULL DEFAULT 0,
  total_tokens INTEGER NOT NULL DEFAULT 0,
  cost_micro_usd INTEGER NOT NULL DEFAULT 0,
  usage_reported INTEGER NOT NULL DEFAULT 0,
  priced_requests INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (day, provider, model, endpoint, tier, server_key_fp)
);

CREATE INDEX idx_daily_day ON usage_daily(day);

CREATE TABLE rollup_state (
  day TEXT PRIMARY KEY,
  complete INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL
);

CREATE TABLE pricing (
  provider TEXT NOT NULL,
  model TEXT NOT NULL,
  input_per_mtok REAL,
  output_per_mtok REAL,
  cache_read_per_mtok REAL,
  cache_write_per_mtok REAL,
  currency TEXT NOT NULL DEFAULT 'USD',
  source TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (provider, model)
);
