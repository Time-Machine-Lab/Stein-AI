CREATE UNIQUE INDEX CONCURRENTLY graph_run_active_root_idx ON graph_run (root_issue_id) WHERE state NOT IN ('succeeded','rejected','failed','cancelled');
