DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM graph_run WHERE state NOT IN ('succeeded','rejected','failed','cancelled')) THEN
  RAISE EXCEPTION 'Cannot remove Graph while active or execution-unknown runs remain';
 END IF;
END $$;
DROP TABLE graph_command, graph_run_event, graph_operation, graph_artifact, graph_approval, graph_submission, graph_task_attempt, graph_issue_owner, graph_node_run, graph_run, graph_version, graph_definition;
