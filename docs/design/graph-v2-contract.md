# Graph V2 wire contract

V1 retains `schema_version: 1` and its immutable serial semantics. V2 uses `schema_version: 2`. Go wire definitions live in `server/internal/graph/types_v2.go` and `definition.go`.

## Definition

Existing `name`, `nodes`, `edges`, `input_schema` remain. New optional fields:

- `max_concurrency`: integer 1–32; omitted means 4. This is a run-wide limit, including child scopes.
- `output_schema`: object JSON schema and `outputs`: field-to-binding map. Child loop/subgraph definitions must declare their output schema; an empty output object is allowed.
- `layout`: `{nodes: {[node_key]: {x:number,y:number}}, viewport?: {x:number,y:number,zoom:number}}`. Positions are finite and bounded; zoom is 0.1–4. Layout does not affect readiness.

Each scope has exactly one `start` and one `end`, at most 128 nodes and 256 edges. Topology is an acyclic directed graph; recursion only exists through bounded embedded bodies. The whole definition has at most four child nesting levels and at most 4,096 execution instances at the declared loop bounds. The overall serialized definition limit remains 256 KiB.

## Nodes and edges

All existing node fields remain. Each new node has exactly its matching configuration property:

All node types may declare `label` (at most 200 UTF-8 bytes) for a human-readable display name. `key` remains the stable binding and edge identity. Run node records freeze the optional label alongside that identity; renaming a later draft does not change historical runs.

| type | Configuration |
| --- | --- |
| `branch` | `condition: Condition`; exactly two outgoing edges, `port: "true"` and `port: "false"` |
| `parallel` | No configuration; at least two outgoing edges |
| `join` | No configuration; at least two incoming edges; waits until every incoming path has resolved and runs if at least one selected path succeeded |
| `loop` | `inputs` for initial child input; `output_schema`; `loop: {body: Definition,max_iterations:1..100,condition:Condition,next:{[field]:InputBinding}}` |
| `subgraph` | `inputs`; `output_schema`; `subgraph: {definition: Definition}` |
| `delay` | `delay: {seconds:1..86400}` |
| `http` | `output_schema`; `http: {method:"GET"|"POST"|"PUT"|"PATCH"|"DELETE"|"HEAD",url:string,headers?:{[header]:InputBinding},body?:InputBinding,timeout_seconds:1..300,max_response_bytes:1..262144}` |
| `script` | `agent_id`; `inputs`; `output_schema`; `execution_mode:"none"`; `max_attempts:1..3`; `timeout_seconds` matching the script timeout; `script: {language:"javascript"|"python",code:string,timeout_seconds:1..3600}` |
| `approval` | `approval: {approver_ids:string[],mode:"all"|"any"|"quorum",quorum?:integer}`; 1–32 unique members; `quorum` only for `mode:"quorum"` |

Approval may appear at any point, including in child scopes. `approver_id` remains accepted in V2 as a single-member approval, but cannot be combined with `approval`.

Edges remain `{from:string,to:string}` with optional `port`. Only branch edges use ports; ordinary nodes have one incoming and one outgoing edge. Start has no incoming and one outgoing; end has one incoming and no outgoing. Use explicit parallel/join nodes for fan-out/fan-in.

Scripts run on the selected existing agent runtime without an LLM, never on the coordinator. HTTP execution must apply network destination restrictions, response bounds, timeouts, and durable effect handling in the service; the pure definition library only validates configuration syntax and bounds.

## Bindings and conditions

Existing binding sources remain `run_input`, `node_output`, `constant`, `object`. New source `iteration_index` returns the one-based current iteration and is valid only in loop scopes. Child `run_input` means that child's input, never its parent's input. Parent values must be explicitly bound through `inputs`.

Reference bindings may set `optional:true` (missing value resolves to null) or `default:<JSON value>` (missing value resolves to that value). A reference to a conditional ancestor must specify either. References must point to a dependency ancestor in the same scope; siblings and future nodes are rejected. Constants and object bindings cannot use `optional` or `default`.

`Condition` is a typed bounded AST:

```json
{"op":"gte","left":{"source":"node_output","node_key":"review","path":"/score"},"right":{"source":"constant","value":80}}
```

Operators: `eq`, `ne` (same JSON type, exact numbers); `gt`, `gte`, `lt`, `lte` (numbers or strings of the same type); `exists` (left only, checks reference presence); `all`, `any` (1–32 child `conditions`); `not` (one child `conditions`). Operand operators cannot also contain `conditions`. No `eval`, JavaScript expressions, regex execution, or implicit type coercion. Trees are bounded by depth and total predicate count.

## Loop semantics

Loops use do-while semantics: iteration 1 runs once with `node.inputs`. At body completion, exports are resolved and validated, then `loop.condition` runs in the completed child's scope (`run_input`, accepted body node outputs, and `iteration_index`). False completes the loop and publishes that iteration's exports. True resolves `loop.next` in the same completed child's scope, validates against the body input schema, and creates a distinct next scope. True at `max_iterations` terminates with an explicit iteration-budget error; it must never silently report success. Nested loops each have their own iteration index.

Subgraphs run one isolated child scope and publish the child's validated exports.

## Pure Go helpers and durable state

- `OrderedNodes` / `TopologicalNodes`: deterministic topological order for V2, serial order for V1.
- `PlanReady(definition, states, selectedPorts) (ReadinessPlan,error)`: returns ordered `ready`, `skipped`, and `blocked` reasons. Branch selections are supplied from persisted decisions, never re-evaluated implicitly.
- `ResolveScopedInputs(bindings, EvaluationContext)` / `ResolveBinding(binding, EvaluationContext)` / `EvaluateCondition(condition, EvaluationContext)`.
- `EvaluationContext`: `RunInput`, accepted `Outputs` indexed by local node key, and one-based `IterationIndex` (zero outside loops).
- Execution storage must use a distinct scope/instance identity. `node_key` remains the local definition key. `NodeSpec.instance_key` identifies the actual nested iteration instance and prevents task/artifact collisions.

`WalkNodes(definition, visitor)` yields stable definition paths: `nodeKey` at the root, `loopKey/body/childKey` inside a loop, and `subgraphKey/definition/childKey` inside a subgraph. Scoped instance keys consist of slash-separated alphanumeric/underscore/hyphen segments (up to 128 bytes each, 512 bytes total).

The pure library neither dispatches effects nor mutates durable state. The service records branch decisions, node skips, child scopes, iteration inputs, approvals, and effects transactionally and enforces the global concurrency and instance budgets.

## Durable service response and controls

V2 run responses include `schema_version`, `graph_id`, `graph_name`, the immutable `definition`, optional `output` and `finished_at`, and `scopes`. Each scope has `{id,parent_node_run_id?,definition_path,iteration,input,output?,state}`. Nodes additionally expose `{instance_key,scope_id,definition_path,iteration,selected_port?,started_at?,finished_at?,wake_at?}`. `node_key` stays local to its definition; instance IDs distinguish loop iterations. Runtime bindings and publication snapshots use static definition paths from `WalkNodes`.

`approvals` retains every gate and its history. The compatibility `approval` field points to the first pending gate. Each approval includes `node_run_id`, `approver_ids`, `mode`, optional `quorum`, immutable `candidate` plus `candidate_digest`, and `decisions:[{member_id,user_id,decision,reason?,decided_at}]`. Candidate evidence contains `{version_hash,input_hash,node_run_id,inputs,outputs,artifacts}`. A member votes once per approval ID. A rejection closes the gate only when the remaining possible approvals cannot meet the threshold. Reassignment requires `node_run_id` when gates are ambiguous and `previous_approver_id` for a multi-member roster, creates a new approval ID, resets votes, and records the invalidated gate.

An HTTP invocation uses the node-run UUID as its fixed `Idempotency-Key`, and a fresh durable claim token per bounded attempt. Dispatch commits before invoking the executor outside the transaction. GET/HEAD may retry at most three times. Ambiguous outcomes for mutating methods become `effect_outcome_unknown`; explicit retry requires a reason and never changes the invocation ID. Expired or replaced claim completions cannot overwrite the current result. Responses expose `{status_code,body}`; every HTTP status is data for branch conditions. Reserved transport headers and `Idempotency-Key` cannot be overridden by the definition.

Run documents have a 16 MiB budget, with 1 MiB reserved for stop receipts, terminal transitions, and recent events. Payload admission fails with `storage_budget_exhausted`; cancellation and physical stop proof remain writable. The document retains the latest 500 events and latest/accepted submissions; complete events and proposals remain in their independent tables. Each node instance permits at most 100 submitted proposals across its bounded attempts.

Failure, rejection, and cancellation drain every active physical attempt before the run becomes terminal. An execution with unknown stop state remains visibly blocked and reserves its concurrency slot. Repository input follows accepted dependency ancestors across subgraphs and previous loop iterations; incomparable parallel commits produce `ambiguous_repository_input`, since merging independent code delivery remains outside this version's scope.
