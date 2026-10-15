-- A person clearing a blocked node authorizes one attempt beyond that node's
-- published max_attempts, so an attempt number above 3 is legitimate. The
-- ceiling for a given attempt is recorded where the budget is known: on the
-- attempt's own manifest (`attempt_ceiling`), read back by
-- graph.CanonicalManifest, and on the node run record (granted_attempts).
-- What stays true for every attempt is only that its number is at least 1, so
-- that is all the table needs to guarantee. The published budget itself is
-- still capped at 1..3 by graph.ValidateDefinition.
ALTER TABLE graph_task_attempt DROP CONSTRAINT IF EXISTS graph_task_attempt_attempt_no_check;
ALTER TABLE graph_task_attempt ADD CONSTRAINT graph_task_attempt_attempt_no_check CHECK (attempt_no >= 1);
