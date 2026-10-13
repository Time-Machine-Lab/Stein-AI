DROP TRIGGER IF EXISTS graph_task_write_fence ON agent_task_queue;
DROP TRIGGER IF EXISTS graph_issue_write_fence ON issue;
DROP FUNCTION IF EXISTS graph_guard_task_write();
DROP FUNCTION IF EXISTS graph_guard_issue_write();
DROP FUNCTION IF EXISTS graph_task_claim_allowed(uuid);
DROP TABLE IF EXISTS graph_attachment_upload;
