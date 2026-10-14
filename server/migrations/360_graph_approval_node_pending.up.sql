CREATE UNIQUE INDEX CONCURRENTLY graph_approval_node_pending_idx ON graph_approval (run_id, COALESCE(node_run_id, '00000000-0000-0000-0000-000000000000'::uuid)) WHERE state = 'pending';
