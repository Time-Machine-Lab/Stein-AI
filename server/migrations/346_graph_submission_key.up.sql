CREATE UNIQUE INDEX CONCURRENTLY graph_submission_key_idx ON graph_submission (task_id, idempotency_key);
