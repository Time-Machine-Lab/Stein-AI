CREATE UNIQUE INDEX CONCURRENTLY graph_approval_pending_idx ON graph_approval (run_id) WHERE state = 'pending';
