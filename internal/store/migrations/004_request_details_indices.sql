-- Add indices for request_details table to optimize connection-specific and timestamp-ordered queries
CREATE INDEX IF NOT EXISTS idx_request_details_conn ON request_details(connection_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_request_details_ts ON request_details(timestamp DESC);
