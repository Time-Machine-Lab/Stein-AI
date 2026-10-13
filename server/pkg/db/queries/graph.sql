-- name: IsGraphManagedIssue :one
SELECT EXISTS (SELECT 1 FROM graph_issue_owner WHERE issue_id = $1 AND workspace_id = $2)::boolean;

-- name: IsGraphTask :one
SELECT EXISTS (SELECT 1 FROM graph_task_attempt WHERE task_id = $1)::boolean;

-- name: GetGraphTaskManifest :one
SELECT a.manifest, a.manifest_hash, a.run_id, a.node_run_id, a.attempt_no,
       a.execution_generation, a.input_hash, a.final_receipt, a.stopped_at,
       r.state AS run_state, r.control_state, n.state AS node_state,
       n.current_task_id, r.initiator_id, r.workspace_id
FROM graph_task_attempt a
JOIN graph_run r ON r.id = a.run_id
JOIN graph_node_run n ON n.id = a.node_run_id
WHERE a.task_id = $1 AND a.workspace_id = $2;

-- name: CheckGraphTaskAdmission :one
SELECT EXISTS (
  SELECT 1 FROM graph_task_attempt a
  JOIN graph_run r ON r.id = a.run_id
  JOIN graph_node_run n ON n.id = a.node_run_id
  WHERE a.task_id = $1 AND r.state = 'active' AND r.control_state = 'run'
    AND n.state = 'active' AND n.current_task_id = a.task_id
    AND a.stopped_at IS NULL AND a.final_receipt IS NULL
)::boolean;

-- name: ScheduleGraphTaskReconciliation :exec
INSERT INTO graph_operation (operation_key, workspace_id, run_id, kind)
SELECT 'terminal:' || task_id::text, workspace_id, run_id, 'reconcile'
FROM graph_task_attempt WHERE task_id = $1
ON CONFLICT (operation_key) DO UPDATE SET consumed_at = NULL, available_at = now();

-- name: ListGraphRunDocuments :many
SELECT document FROM graph_run WHERE workspace_id = $1 ORDER BY created_at DESC LIMIT 100;

-- name: ListGraphDefinitionDocuments :many
SELECT document FROM graph_definition WHERE workspace_id = $1 AND archived_at IS NULL ORDER BY updated_at DESC LIMIT 100;
