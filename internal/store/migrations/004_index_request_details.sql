-- Index on request_details(timestamp DESC) for usage and logs queries
CREATE INDEX IF NOT EXISTS idx_request_details_timestamp ON request_details(timestamp DESC);
