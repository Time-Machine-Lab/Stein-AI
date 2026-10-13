CREATE INDEX CONCURRENTLY graph_trigger_event_pending_idx ON graph_trigger_event(available_at,id) WHERE state IN ('pending','starting');
