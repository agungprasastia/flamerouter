-- Add cached_tokens and cost columns to usage_daily and request_details
ALTER TABLE usage_daily ADD COLUMN cached_tokens INTEGER DEFAULT 0;
ALTER TABLE usage_daily ADD COLUMN cost REAL DEFAULT 0.0;

ALTER TABLE request_details ADD COLUMN cached_tokens INTEGER DEFAULT 0;
ALTER TABLE request_details ADD COLUMN cost REAL DEFAULT 0.0;
