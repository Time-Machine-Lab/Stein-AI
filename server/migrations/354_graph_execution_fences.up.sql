-- Every writer locks the Issue before reading its ownership, including legacy
-- enqueue paths. This serializes root ownership acquisition with normal work.
-- Claim may already hold the agent capacity lock. NOWAIT refuses a busy Graph
-- run instead of creating an inverse lock dependency with its coordinator.
CREATE FUNCTION graph_task_claim_allowed(task_uuid uuid) RETURNS boolean LANGUAGE plpgsql VOLATILE AS $$
DECLARE
  binding graph_task_attempt%ROWTYPE;
  workflow graph_run%ROWTYPE;
BEGIN
  SELECT * INTO binding FROM graph_task_attempt WHERE task_id=task_uuid;
  IF NOT FOUND THEN RETURN true; END IF;
  BEGIN
    SELECT * INTO workflow FROM graph_run WHERE id=binding.run_id FOR UPDATE NOWAIT;
  EXCEPTION WHEN lock_not_available THEN RETURN false;
  END;
  IF NOT FOUND OR workflow.state<>'active' OR workflow.control_state<>'run'
      OR binding.final_receipt IS NOT NULL OR binding.stopped_at IS NOT NULL THEN RETURN false; END IF;
  IF workflow.document->>'deadline' IS NOT NULL
      AND (workflow.document->>'deadline')::timestamptz<=now() THEN RETURN false; END IF;
  RETURN EXISTS (SELECT 1 FROM graph_node_run WHERE id=binding.node_run_id
      AND run_id=workflow.id AND state='active' AND current_task_id=task_uuid);
END $$;

CREATE FUNCTION graph_guard_task_write() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE owner graph_issue_owner%ROWTYPE;
BEGIN
  -- Only enqueue/retarget can race root ownership acquisition. Taking the
  -- Issue lock on status updates would invert the coordinator's lock order.
  -- The legacy owner fence already holds KEY SHARE on the Issue. NO KEY
  -- UPDATE is compatible with other enqueuers' KEY SHARE locks while still
  -- conflicting with Graph takeover's FOR UPDATE. Upgrading to FOR UPDATE
  -- here would deadlock concurrent ordinary enqueues for the same Issue.
  IF TG_OP='INSERT' OR NEW.issue_id IS DISTINCT FROM OLD.issue_id THEN
    PERFORM 1 FROM issue WHERE id=NEW.issue_id FOR NO KEY UPDATE;
  END IF;
  IF TG_OP='UPDATE' THEN
    SELECT * INTO owner FROM graph_issue_owner WHERE issue_id=OLD.issue_id;
  END IF;
  IF owner.issue_id IS NULL THEN
    SELECT * INTO owner FROM graph_issue_owner WHERE issue_id=NEW.issue_id;
  END IF;
  IF NOT FOUND THEN RETURN NEW; END IF;
  IF NOT EXISTS (SELECT 1 FROM graph_task_attempt a WHERE a.task_id=NEW.id
      AND a.run_id=owner.run_id AND a.node_run_id=owner.node_run_id
      AND a.workspace_id=owner.workspace_id) THEN
    RAISE EXCEPTION 'graph_owned: use Graph run controls' USING ERRCODE='P0001';
  END IF;
  IF TG_OP='UPDATE' AND (
      NEW.context IS DISTINCT FROM OLD.context OR
      NEW.trigger_comment_id IS DISTINCT FROM OLD.trigger_comment_id OR
      NEW.coalesced_comment_ids IS DISTINCT FROM OLD.coalesced_comment_ids OR
      NEW.handoff_note IS DISTINCT FROM OLD.handoff_note OR
      NEW.originator_user_id IS DISTINCT FROM OLD.originator_user_id OR
      NEW.runtime_mcp_overlay IS DISTINCT FROM OLD.runtime_mcp_overlay OR
      NEW.agent_id IS DISTINCT FROM OLD.agent_id OR
      NEW.runtime_id IS DISTINCT FROM OLD.runtime_id OR
      NEW.issue_id IS DISTINCT FROM OLD.issue_id) THEN
    RAISE EXCEPTION 'graph_owned: attempt inputs are immutable' USING ERRCODE='P0001';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER graph_task_write_fence BEFORE INSERT OR UPDATE ON agent_task_queue
FOR EACH ROW EXECUTE FUNCTION graph_guard_task_write();

CREATE FUNCTION graph_guard_issue_write() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE owner_run uuid;
BEGIN
  IF TG_OP='INSERT' THEN
    IF NEW.parent_issue_id IS NULL THEN RETURN NEW; END IF;
    PERFORM 1 FROM issue WHERE id=NEW.parent_issue_id FOR UPDATE;
    SELECT run_id INTO owner_run FROM graph_issue_owner WHERE issue_id=NEW.parent_issue_id;
    IF FOUND AND owner_run::text IS DISTINCT FROM current_setting('multica.graph_run_id',true) THEN
      RAISE EXCEPTION 'graph_owned: child issues are created by Graph' USING ERRCODE='P0001';
    END IF;
    RETURN NEW;
  END IF;
  SELECT run_id INTO owner_run FROM graph_issue_owner WHERE issue_id=OLD.id;
  IF NOT FOUND THEN
    -- Moving an ordinary Issue into a Graph would also introduce an unmanaged child.
    IF TG_OP='UPDATE' AND NEW.parent_issue_id IS DISTINCT FROM OLD.parent_issue_id AND NEW.parent_issue_id IS NOT NULL THEN
      PERFORM 1 FROM issue WHERE id=NEW.parent_issue_id FOR UPDATE;
      IF EXISTS(SELECT 1 FROM graph_issue_owner WHERE issue_id=NEW.parent_issue_id) THEN
        RAISE EXCEPTION 'graph_owned: cannot move an issue into a Graph' USING ERRCODE='P0001';
      END IF;
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
  END IF;
  IF TG_OP='DELETE' THEN
    RAISE EXCEPTION 'graph_owned: archive the Graph run instead' USING ERRCODE='P0001';
  END IF;
  IF owner_run::text IS DISTINCT FROM current_setting('multica.graph_run_id',true)
     AND (to_jsonb(NEW)-ARRAY['title','priority','due_date','position','updated_at'])
       IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['title','priority','due_date','position','updated_at']) THEN
    RAISE EXCEPTION 'graph_owned: execution fields are controlled by Graph' USING ERRCODE='P0001';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER graph_issue_write_fence BEFORE INSERT OR UPDATE OR DELETE ON issue
FOR EACH ROW EXECUTE FUNCTION graph_guard_issue_write();

-- Hashes are computed from uploaded bytes, never accepted from the agent.
CREATE TABLE graph_attachment_upload (
 attachment_id uuid NOT NULL, workspace_id uuid NOT NULL, task_id uuid NOT NULL,
 content_hash text NOT NULL, size_bytes bigint NOT NULL, media_type text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
