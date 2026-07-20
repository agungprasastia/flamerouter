CREATE TABLE IF NOT EXISTS proxy_pools (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    type TEXT NOT NULL DEFAULT 'http',
    host TEXT NOT NULL,
    port INTEGER NOT NULL,
    username TEXT DEFAULT '',
    password TEXT DEFAULT '',
    is_active INTEGER DEFAULT 1,
    created_at TEXT DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS disabled_models (
    model TEXT PRIMARY KEY,
    disabled_at TEXT DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS custom_models (
    id TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    model_id TEXT NOT NULL,
    display_name TEXT DEFAULT '',
    capabilities TEXT DEFAULT '{}',
    created_at TEXT DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS request_details (
    id TEXT PRIMARY KEY,
    timestamp TEXT DEFAULT (datetime('now')),
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    connection_id TEXT DEFAULT '',
    status_code INTEGER DEFAULT 0,
    duration_ms INTEGER DEFAULT 0,
    prompt_tokens INTEGER DEFAULT 0,
    completion_tokens INTEGER DEFAULT 0,
    request_body TEXT DEFAULT '',
    response_preview TEXT DEFAULT '',
    error_text TEXT DEFAULT '',
    client TEXT DEFAULT '',
    source_format TEXT DEFAULT '',
    target_format TEXT DEFAULT ''
);

CREATE TABLE IF NOT EXISTS usage_daily (
    date TEXT NOT NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    requests INTEGER DEFAULT 0,
    prompt_tokens INTEGER DEFAULT 0,
    completion_tokens INTEGER DEFAULT 0,
    PRIMARY KEY (date, provider, model)
);

CREATE TABLE IF NOT EXISTS kv (
    scope TEXT NOT NULL,
    key TEXT NOT NULL,
    value TEXT DEFAULT '',
    PRIMARY KEY (scope, key)
);

-- Add columns to provider_connections (safe re-run: migrate ignores duplicate column)
ALTER TABLE provider_connections ADD COLUMN consecutive_use_count INTEGER DEFAULT 0;
ALTER TABLE provider_connections ADD COLUMN last_used_at TEXT DEFAULT '';
ALTER TABLE provider_connections ADD COLUMN test_status TEXT DEFAULT 'active';
