-- Capture the event in the issue transaction. Failed issue mutations cannot
-- dispatch Graphs; workers recover committed events even after process failure.
CREATE FUNCTION graph_capture_issue_trigger() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE trigger_row graph_trigger%ROWTYPE; event_type text; event_id uuid;
BEGIN
 IF NEW.parent_issue_id IS NOT NULL
 OR NULLIF(current_setting('multica.graph_run_id',true),'') IS NOT NULL
 OR NULLIF(current_setting('multica.graph_trigger_event_id',true),'') IS NOT NULL THEN RETURN NEW; END IF;
 IF EXISTS(SELECT 1 FROM graph_issue_owner WHERE issue_id=NEW.id)
 OR EXISTS(SELECT 1 FROM graph_trigger_event WHERE root_issue_id=NEW.id) THEN RETURN NEW; END IF;
 IF TG_OP='INSERT' THEN event_type:='created';
 ELSIF NEW.status IS DISTINCT FROM OLD.status THEN event_type:='status_changed';
 ELSE RETURN NEW; END IF;
 FOR trigger_row IN SELECT * FROM graph_trigger WHERE workspace_id=NEW.workspace_id AND kind='task_event' AND enabled AND deleted_at IS NULL
 AND document#>>'{config,project_id}'=NEW.project_id::text
 AND (document#>'{config,events}') ? event_type
 AND (COALESCE(jsonb_array_length(document#>'{config,statuses}'),0)=0 OR (document#>'{config,statuses}') ? NEW.status)
 ORDER BY id LOOP
  -- The caller may already hold an issue row lock. Waiting for a deleting
  -- workspace here would invert workspace -> issue teardown lock order.
  -- Fail this mutation atomically and let the client retry instead.
  PERFORM id FROM workspace WHERE id=NEW.workspace_id FOR KEY SHARE NOWAIT;
  IF NOT FOUND THEN RAISE EXCEPTION 'Workspace unavailable for task event' USING ERRCODE='40001'; END IF;
  event_id:=gen_random_uuid();
  INSERT INTO graph_trigger_event(id,workspace_id,trigger_id,event_key,request_hash,snapshot,input,root_issue_id)
  VALUES(event_id,NEW.workspace_id,trigger_row.id,'issue:'||event_id::text,md5(NEW.id::text||event_type||NEW.status),trigger_row.document,trigger_row.document->'input',NEW.id);
 END LOOP;
 RETURN NEW;
END $$;
CREATE TRIGGER graph_issue_trigger_event AFTER INSERT OR UPDATE OF status ON issue FOR EACH ROW EXECUTE FUNCTION graph_capture_issue_trigger();
