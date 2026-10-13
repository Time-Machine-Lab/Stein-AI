CREATE FUNCTION graph_guard_attachment_delete() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF EXISTS(SELECT 1 FROM graph_artifact WHERE id=OLD.id AND workspace_id=OLD.workspace_id AND document->>'type'='attachment_ref') THEN
    RAISE EXCEPTION 'graph_artifact_retained: accepted Graph artifacts are immutable' USING ERRCODE='P0001';
  END IF;
  RETURN OLD;
END $$;
CREATE TRIGGER graph_attachment_retention BEFORE DELETE ON attachment
FOR EACH ROW EXECUTE FUNCTION graph_guard_attachment_delete();
