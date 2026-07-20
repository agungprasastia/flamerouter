CREATE TABLE IF NOT EXISTS settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS api_keys (
  id         TEXT PRIMARY KEY,
  name       TEXT NOT NULL,
  key_id     TEXT NOT NULL,
  key_hash   TEXT NOT NULL,
  machine_id TEXT,
  is_active  INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS provider_connections (
  id                      TEXT PRIMARY KEY,
  provider                TEXT NOT NULL,
  auth_type               TEXT NOT NULL,
  name                    TEXT NOT NULL,
  priority                INTEGER NOT NULL DEFAULT 0,
  is_active               INTEGER NOT NULL DEFAULT 1,
  api_key                 TEXT,
  access_token            TEXT,
  refresh_token           TEXT,
  expires_at              TEXT,
  test_status             TEXT,
  last_error              TEXT,
  rate_limited_until      TEXT,
  provider_specific_data  TEXT,
  base_url                TEXT,
  created_at              TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS provider_nodes (
  id       TEXT PRIMARY KEY,
  type     TEXT NOT NULL,
  name     TEXT NOT NULL,
  prefix   TEXT NOT NULL,
  api_type TEXT NOT NULL,
  base_url TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS model_aliases (
  alias        TEXT PRIMARY KEY,
  target_model TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS combos (
  id     TEXT PRIMARY KEY,
  name   TEXT NOT NULL UNIQUE,
  models TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS usage_entries (
  id                 INTEGER PRIMARY KEY AUTOINCREMENT,
  provider           TEXT NOT NULL,
  model              TEXT NOT NULL,
  prompt_tokens      INTEGER NOT NULL DEFAULT 0,
  completion_tokens  INTEGER NOT NULL DEFAULT 0,
  connection_id      TEXT,
  created_at         TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_usage_created ON usage_entries(created_at);
CREATE INDEX IF NOT EXISTS idx_conn_provider ON provider_connections(provider);
