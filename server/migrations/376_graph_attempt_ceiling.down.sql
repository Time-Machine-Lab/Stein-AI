-- Restores the published-budget cap. This fails if any row already records an
-- attempt above 3, which is exactly the state the up migration made possible:
-- those rows are attempts a person authorized, so remove them deliberately
-- rather than expecting this to succeed on a database that has any.
ALTER TABLE graph_task_attempt DROP CONSTRAINT IF EXISTS graph_task_attempt_attempt_no_check;
ALTER TABLE graph_task_attempt ADD CONSTRAINT graph_task_attempt_attempt_no_check CHECK (attempt_no BETWEEN 1 AND 3);
