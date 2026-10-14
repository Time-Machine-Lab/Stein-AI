CREATE INDEX CONCURRENTLY graph_trigger_workspace_idx ON graph_trigger(workspace_id,graph_id) WHERE deleted_at IS NULL;
