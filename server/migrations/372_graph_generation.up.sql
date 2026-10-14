-- One row per AI authoring request served by a runtime agent. id is the
-- authoring task's agent_task_queue.id; the relationship is enforced by the
-- application, never by a foreign key. request is the alias-only context the
-- authoring agent reads, and actors is the server-private alias -> UUID map
-- used to resolve the submitted draft.
CREATE TABLE graph_generation (
 id uuid NOT NULL, workspace_id uuid NOT NULL, requester_id uuid NOT NULL, agent_id uuid NOT NULL,
 request jsonb NOT NULL, actors jsonb NOT NULL, draft jsonb,
 summary text NOT NULL DEFAULT '', submissions integer NOT NULL DEFAULT 0,
 last_error text NOT NULL DEFAULT '',
 failure_code text NOT NULL DEFAULT '', failure_message text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
