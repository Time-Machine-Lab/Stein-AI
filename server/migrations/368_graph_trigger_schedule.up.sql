CREATE INDEX CONCURRENTLY graph_trigger_schedule_idx ON graph_trigger(next_fire_at,id) WHERE enabled AND kind='schedule' AND deleted_at IS NULL;
