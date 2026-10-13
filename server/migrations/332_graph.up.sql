CREATE TABLE graph_definition (
 id uuid NOT NULL, workspace_id uuid NOT NULL, creator_id uuid NOT NULL,
 revision bigint NOT NULL CHECK (revision > 0), document jsonb NOT NULL,
 archived_at timestamptz, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE graph_version (
 id uuid NOT NULL, workspace_id uuid NOT NULL, graph_id uuid NOT NULL,
 version integer NOT NULL CHECK (version > 0), document jsonb NOT NULL,
 content_hash text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE graph_run (
 id uuid NOT NULL, workspace_id uuid NOT NULL, root_issue_id uuid NOT NULL,
 version_id uuid NOT NULL, initiator_id uuid NOT NULL, state text NOT NULL,
 control_state text NOT NULL DEFAULT 'run', revision bigint NOT NULL DEFAULT 1,
 document jsonb NOT NULL, archived_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK (state IN ('active','waiting_approval','blocked','succeeded','rejected','failed','cancelled')),
 CHECK (control_state IN ('run','pause','cancel_requested'))
);
CREATE TABLE graph_node_run (
 id uuid NOT NULL, workspace_id uuid NOT NULL, run_id uuid NOT NULL, node_key text NOT NULL,
 issue_id uuid, state text NOT NULL, current_task_id uuid, input_hash text NOT NULL DEFAULT '',
 document jsonb NOT NULL
);
CREATE TABLE graph_issue_owner (
 issue_id uuid NOT NULL, workspace_id uuid NOT NULL, run_id uuid NOT NULL,
 node_run_id uuid, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE graph_task_attempt (
 task_id uuid NOT NULL, workspace_id uuid NOT NULL, run_id uuid NOT NULL, node_run_id uuid NOT NULL,
 attempt_no integer NOT NULL CHECK (attempt_no BETWEEN 1 AND 3),
 execution_generation bigint NOT NULL CHECK (execution_generation > 0),
 input_hash text NOT NULL, manifest jsonb NOT NULL, manifest_hash text NOT NULL,
 final_receipt jsonb, stopped_at timestamptz, executor_id text, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE graph_submission (
 id uuid NOT NULL, workspace_id uuid NOT NULL, run_id uuid NOT NULL, node_run_id uuid NOT NULL,
 task_id uuid NOT NULL, idempotency_key text NOT NULL, content_hash text NOT NULL,
 document jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE graph_approval (
 id uuid NOT NULL, workspace_id uuid NOT NULL, run_id uuid NOT NULL,
 state text NOT NULL, document jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE graph_artifact (
 id uuid NOT NULL, workspace_id uuid NOT NULL, run_id uuid NOT NULL, node_run_id uuid NOT NULL,
 task_id uuid NOT NULL, content_hash text NOT NULL, document jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE graph_operation (
 operation_key text NOT NULL, workspace_id uuid NOT NULL, run_id uuid NOT NULL,
 kind text NOT NULL, available_at timestamptz NOT NULL DEFAULT now(), consumed_at timestamptz,
 last_error text, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE graph_run_event (
 id uuid NOT NULL, workspace_id uuid NOT NULL, run_id uuid NOT NULL,
 sequence bigint NOT NULL, document jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE graph_command (
 workspace_id uuid NOT NULL, command_key text NOT NULL, request_hash text NOT NULL,
 response jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
