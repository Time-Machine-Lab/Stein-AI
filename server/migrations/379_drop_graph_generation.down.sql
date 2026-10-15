-- Restores the table exactly as 372 created it, so a rollback past this point
-- leaves the schema the older pipeline's code expects. Migrations 378 and 377
-- then restore each index concurrently in its own standalone statement.
CREATE TABLE graph_generation (
 id uuid NOT NULL, workspace_id uuid NOT NULL, requester_id uuid NOT NULL, agent_id uuid NOT NULL,
 request jsonb NOT NULL, actors jsonb NOT NULL, draft jsonb,
 summary text NOT NULL DEFAULT '', submissions integer NOT NULL DEFAULT 0,
 last_error text NOT NULL DEFAULT '',
 failure_code text NOT NULL DEFAULT '', failure_message text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
