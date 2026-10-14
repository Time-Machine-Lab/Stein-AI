-- Restores the table exactly as 372 created it, so a rollback past this point
-- leaves the schema the older pipeline's code expects. 373 and 374 add no
-- columns, only indexes; those come back with their own down migrations.
CREATE TABLE graph_generation (
 id uuid NOT NULL, workspace_id uuid NOT NULL, requester_id uuid NOT NULL, agent_id uuid NOT NULL,
 request jsonb NOT NULL, actors jsonb NOT NULL, draft jsonb,
 summary text NOT NULL DEFAULT '', submissions integer NOT NULL DEFAULT 0,
 last_error text NOT NULL DEFAULT '',
 failure_code text NOT NULL DEFAULT '', failure_message text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX CONCURRENTLY graph_generation_id_idx ON graph_generation(id);
CREATE INDEX CONCURRENTLY graph_generation_requester_idx ON graph_generation(workspace_id,requester_id,created_at DESC);
