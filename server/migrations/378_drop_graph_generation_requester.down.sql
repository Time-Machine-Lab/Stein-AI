CREATE INDEX CONCURRENTLY IF NOT EXISTS graph_generation_requester_idx ON graph_generation(workspace_id,requester_id,created_at DESC);
