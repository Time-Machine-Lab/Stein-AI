CREATE INDEX CONCURRENTLY graph_operation_pending_idx ON graph_operation (available_at) WHERE consumed_at IS NULL;
