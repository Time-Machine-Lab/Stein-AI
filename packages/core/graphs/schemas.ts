import { z } from "zod";
import type { GraphInputBinding } from "./types";

const object = z.record(z.string(), z.unknown());
const id = z.string().min(1);
const timestamp = z.string().catch("");
const reason = z.string().nullable().optional();

export const GraphInputBindingSchema: z.ZodType<GraphInputBinding> = z.lazy(() => z.object({
  source: z.string(), node_key: z.string().optional(), path: z.string().optional(),
  value: z.unknown().optional(), fields: z.record(z.string(), GraphInputBindingSchema).optional(),
}).transform((value) => ({
  source: value.source, nodeKey: value.node_key, path: value.path, value: value.value, fields: value.fields,
})));

export const GraphDefinitionSchema = z.object({
  schema_version: z.number().int(), name: z.string(), input_schema: object,
  nodes: z.array(z.object({
    node_key: id, type: z.string(), agent_id: z.string().optional(), instructions: z.string().optional(),
    inputs: z.record(z.string(), GraphInputBindingSchema).optional(), output_schema: object.optional(),
    execution_mode: z.string().optional(), max_attempts: z.number().int().optional(),
    timeout_seconds: z.number().int().optional(), approver_id: z.string().optional(),
  }).transform((value) => ({
    nodeKey: value.node_key, type: value.type, agentId: value.agent_id, instructions: value.instructions,
    inputs: value.inputs, outputSchema: value.output_schema, executionMode: value.execution_mode,
    maxAttempts: value.max_attempts, timeoutSeconds: value.timeout_seconds, approverId: value.approver_id,
  }))),
  edges: z.array(z.object({ from: id, to: id })),
}).transform((value) => ({
  schemaVersion: value.schema_version, name: value.name, inputSchema: value.input_schema, nodes: value.nodes, edges: value.edges,
}));

export const GraphVersionSchema = z.object({
  id, graph_id: id, version: z.number().int().positive(), definition: GraphDefinitionSchema,
  content_hash: z.string(), created_at: timestamp,
}).transform((value) => ({
  id: value.id, graphId: value.graph_id, version: value.version, definition: value.definition,
  contentHash: value.content_hash, createdAt: value.created_at,
}));

export const GraphRecordSchema = z.object({
  id, workspace_id: id, name: z.string(), revision: z.number().int().nonnegative(), definition: GraphDefinitionSchema,
  creator_id: z.string().optional(), archived_at: reason, created_at: timestamp, updated_at: timestamp,
  versions: z.array(GraphVersionSchema).nullish().transform((value) => value ?? []),
}).transform((value) => ({
  id: value.id, workspaceId: value.workspace_id, name: value.name, revision: value.revision, definition: value.definition,
  createdBy: value.creator_id, archivedAt: value.archived_at, createdAt: value.created_at, updatedAt: value.updated_at,
  versions: value.versions,
}));

export const GraphValidationSchema = z.object({
  valid: z.boolean(), diagnostics: z.array(z.object({
    code: z.string(), message: z.string(), path: z.string().optional(), node_key: z.string().optional(),
  }).transform((value) => ({ code: value.code, message: value.message, path: value.path, nodeKey: value.node_key }))).nullish().transform((value) => value ?? []),
});

export const GraphRunSchema = z.object({
  id, workspace_id: id, root_issue_id: id, version_id: id, initiator_id: z.string().catch(""),
  state: z.string(), control_state: z.string(), revision: z.number().int().nonnegative(),
  can_detach: z.boolean().catch(false),
  input: object.catch({}), input_hash: z.string().catch(""),
  nodes: z.array(z.object({
    id, node_key: id, node_type: z.string(), state: z.string(), issue_id: reason,
    input: object.catch({}), input_hash: z.string().catch(""), max_attempts: z.number().int().nonnegative().catch(0),
    can_retry: z.boolean().catch(false), can_resolve_block: z.boolean().catch(false),
    accepted_submission_id: reason, block_reason: reason,
    attempts: z.array(z.object({
      task_id: id, attempt_no: z.number().int().positive(), generation: z.number().int().nonnegative(), state: z.string(),
      manifest: object.catch({}), receipt: object.nullish(),
    }).transform((value) => ({
      taskId: value.task_id, attemptNo: value.attempt_no, generation: value.generation, state: value.state,
      manifest: value.manifest, receipt: value.receipt,
    }))).nullish().transform((value) => value ?? []),
    submissions: z.array(object).nullish().transform((value) => value ?? []),
  }).transform((value) => ({
    id: value.id, nodeKey: value.node_key, nodeType: value.node_type, state: value.state, issueId: value.issue_id,
    input: value.input, inputHash: value.input_hash, maxAttempts: value.max_attempts, attempts: value.attempts,
    canRetry: value.can_retry, canResolveBlock: value.can_resolve_block,
    submissions: value.submissions, acceptedSubmissionId: value.accepted_submission_id, blockReason: value.block_reason,
  }))).nullish().transform((value) => value ?? []),
  events: z.array(object).nullish().transform((value) => value ?? []),
  artifacts: z.array(object).nullish().transform((value) => value ?? []),
  approval: z.object({
    id, approver_id: id, candidate_digest: id, revision: z.number().int().nonnegative(), state: z.string(),
  }).transform((value) => ({
    id: value.id, approverId: value.approver_id, candidateDigest: value.candidate_digest, revision: value.revision, state: value.state,
  })).nullish(),
  block_reason: reason, archived_at: reason, created_at: timestamp, updated_at: timestamp,
}).transform((value) => ({
  id: value.id, workspaceId: value.workspace_id, rootIssueId: value.root_issue_id, versionId: value.version_id,
  initiatorId: value.initiator_id, state: value.state, controlState: value.control_state, revision: value.revision,
  canDetach: value.can_detach,
  input: value.input, inputHash: value.input_hash, nodes: value.nodes, events: value.events, artifacts: value.artifacts,
  approval: value.approval, blockReason: value.block_reason, archivedAt: value.archived_at,
  createdAt: value.created_at, updatedAt: value.updated_at,
}));

export const GraphListSchema = z.array(GraphRecordSchema);
export const GraphRunListSchema = z.array(GraphRunSchema);
