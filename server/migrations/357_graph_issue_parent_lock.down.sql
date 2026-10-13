CREATE OR REPLACE FUNCTION graph_guard_issue_write() RETURNS trigger LANGUAGE plpgsql AS $$
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
