-- Index on request_details(connection_id, timestamp DESC) for connection usage queries
CREATE INDEX IF NOT EXISTS idx_request_details_conn ON request_details(connection_id, timestamp DESC);
