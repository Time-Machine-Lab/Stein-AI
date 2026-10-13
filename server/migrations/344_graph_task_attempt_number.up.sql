CREATE UNIQUE INDEX CONCURRENTLY graph_task_attempt_number_idx ON graph_task_attempt (node_run_id, attempt_no);
