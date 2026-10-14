CREATE TABLE graph_trigger (
 id uuid NOT NULL, workspace_id uuid NOT NULL, graph_id uuid NOT NULL,
 version_id uuid NOT NULL, creator_id uuid NOT NULL, kind text NOT NULL,
 enabled boolean NOT NULL DEFAULT false, revision bigint NOT NULL DEFAULT 1,
 document jsonb NOT NULL, secret_hash text NOT NULL DEFAULT '',
 next_fire_at timestamptz, last_error text NOT NULL DEFAULT '',
 deleted_at timestamptz, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK(kind IN ('schedule','webhook','task_event'))
);
CREATE TABLE graph_trigger_event (
 id uuid NOT NULL, workspace_id uuid NOT NULL, trigger_id uuid NOT NULL,
 event_key text NOT NULL, request_hash text NOT NULL,
 snapshot jsonb NOT NULL, input jsonb NOT NULL, root_issue_id uuid NOT NULL,
 state text NOT NULL DEFAULT 'pending', run_id uuid, error text NOT NULL DEFAULT '',
 attempts integer NOT NULL DEFAULT 0, available_at timestamptz NOT NULL DEFAULT now(),
 lease_until timestamptz, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK(state IN ('pending','starting','started','failed','skipped'))
);
