import { z } from "zod";
import { graphUtf8Length } from "./text";
import type {
  GraphBuilderSession,
  GraphBuilderSessionSummary,
  GraphInputBinding,
  GraphCondition,
  GraphDefinition,
  GraphNodeDefinition,
} from "./types";

const object = z.record(z.string(), z.unknown());
const id = z.string().min(1);
const timestamp = z.string().catch("");
const nodeLabel = z
  .string()
  .refine(
    (value) => graphUtf8Length(value) <= 200,
    "Step name exceeds 200 UTF-8 bytes",
  )
  .optional();
const reason = z.string().nullable().optional();

export const GraphInputBindingSchema: z.ZodType<GraphInputBinding> = z.lazy(
  () =>
    z
      .object({
        source: z.string(),
        node_key: z.string().optional(),
        path: z.string().optional(),
        value: z.unknown().optional(),
        fields: z.record(z.string(), GraphInputBindingSchema).optional(),
        optional: z.boolean().optional(),
        default: z.unknown().optional(),
      })
      .transform((value) => ({
        source: value.source,
        nodeKey: value.node_key,
        path: value.path,
        value: value.value,
        fields: value.fields,
        optional: value.optional,
        default: value.default,
      })),
);

export const GraphConditionSchema: z.ZodType<GraphCondition> = z.lazy(() =>
  z.object({
    op: z.string(),
    left: GraphInputBindingSchema.optional(),
    right: GraphInputBindingSchema.optional(),
    conditions: z.array(GraphConditionSchema).optional(),
  }),
);
const bindings = z.record(z.string(), GraphInputBindingSchema);
export const GraphDefinitionSchema: z.ZodType<GraphDefinition> = z.lazy(() =>
  z
    .object({
      schema_version: z.number().int(),
      name: z.string(),
      input_schema: object,
      output_schema: object.optional(),
      outputs: bindings.optional(),
      max_concurrency: z.number().int().optional(),
      layout: z
        .object({
          nodes: z.record(
            z.string(),
            z.object({ x: z.number(), y: z.number() }),
          ),
          viewport: z
            .object({ x: z.number(), y: z.number(), zoom: z.number() })
            .optional(),
        })
        .optional(),
      nodes: z.array(GraphNodeDefinitionSchema),
      edges: z.array(
        z.object({ from: id, to: id, port: z.string().optional() }),
      ),
    })
    .transform((value) => ({
      schemaVersion: value.schema_version,
      name: value.name,
      inputSchema: value.input_schema,
      nodes: value.nodes,
      edges: value.edges,
      outputSchema: value.output_schema,
      outputs: value.outputs,
      maxConcurrency: value.max_concurrency,
      layout: value.layout,
    })),
);
export const GraphNodeDefinitionSchema: z.ZodType<GraphNodeDefinition> = z.lazy(
  () =>
    z
      .object({
        node_key: id,
        label: nodeLabel,
        type: z.string(),
        agent_id: z.string().optional(),
        instructions: z.string().optional(),
        inputs: z.record(z.string(), GraphInputBindingSchema).optional(),
        output_schema: object.optional(),
        execution_mode: z.string().optional(),
        max_attempts: z.number().int().optional(),
        timeout_seconds: z.number().int().optional(),
        approver_id: z.string().optional(),
        condition: GraphConditionSchema.optional(),
        approval: z
          .object({
            approver_ids: z.array(id),
            mode: z.string(),
            quorum: z.number().int().optional(),
          })
          .transform((v) => ({
            approverIds: v.approver_ids,
            mode: v.mode,
            quorum: v.quorum,
          }))
          .optional(),
        loop: z
          .object({
            body: GraphDefinitionSchema,
            max_iterations: z.number().int(),
            condition: GraphConditionSchema,
            next: bindings,
          })
          .transform((v) => ({
            body: v.body,
            maxIterations: v.max_iterations,
            condition: v.condition,
            next: v.next,
          }))
          .optional(),
        subgraph: z.object({ definition: GraphDefinitionSchema }).optional(),
        delay: z.object({ seconds: z.number() }).optional(),
        http: z
          .object({
            method: z.string(),
            url: z.string(),
            headers: bindings.optional(),
            body: GraphInputBindingSchema.optional(),
            timeout_seconds: z.number(),
            max_response_bytes: z.number(),
          })
          .transform((v) => ({
            method: v.method,
            url: v.url,
            headers: v.headers,
            body: v.body,
            timeoutSeconds: v.timeout_seconds,
            maxResponseBytes: v.max_response_bytes,
          }))
          .optional(),
        script: z
          .object({
            language: z.string(),
            code: z.string(),
            timeout_seconds: z.number(),
          })
          .transform((v) => ({
            language: v.language,
            code: v.code,
            timeoutSeconds: v.timeout_seconds,
          }))
          .optional(),
      })
      .transform((value) => ({
        nodeKey: value.node_key,
        label: value.label,
        type: value.type,
        agentId: value.agent_id,
        instructions: value.instructions,
        inputs: value.inputs,
        outputSchema: value.output_schema,
        executionMode: value.execution_mode,
        maxAttempts: value.max_attempts,
        timeoutSeconds: value.timeout_seconds,
        approverId: value.approver_id,
        condition: value.condition,
        approval: value.approval,
        loop: value.loop,
        subgraph: value.subgraph,
        delay: value.delay,
        http: value.http,
        script: value.script,
      })),
);

// `builder_enabled` is advertised by servers that support the chat-based
// creation flow; `enabled` still means the legacy server-model path. Each is
// defaulted independently so a server that sends neither (or an older one
// that never heard of builder_enabled) degrades to the legacy/unavailable
// branch rather than throwing.
export const GraphGenerationOptionsSchema = z
  .object({
    enabled: z.boolean().catch(false),
    builder_enabled: z.boolean().catch(false),
  })
  .transform((value) => ({
    enabled: value.enabled,
    builderEnabled: value.builder_enabled,
  }));

export const GraphGenerationResultSchema = z.object({
  definition: GraphDefinitionSchema,
  summary: z.string().optional(),
});

export const GraphBuilderSessionSchema = z
  .object({
    session_id: z.string(),
    builder_agent_id: z.string(),
    runtime_id: z.string(),
    model: z.string().catch(""),
    default_agent_id: z.string(),
    approver_id: z.string(),
  })
  .transform((value) => ({
    sessionId: value.session_id,
    builderAgentId: value.builder_agent_id,
    runtimeId: value.runtime_id,
    model: value.model,
    defaultAgentId: value.default_agent_id,
    approverId: value.approver_id,
  }));

// A session that failed to parse is a session that cannot be addressed, so the
// caller sees an empty id and treats the start as having failed, rather than
// binding a conversation id the server never confirmed.
export const EMPTY_GRAPH_BUILDER_SESSION: GraphBuilderSession = {
  sessionId: "",
  builderAgentId: "",
  runtimeId: "",
  model: "",
  defaultAgentId: "",
  approverId: "",
};

export const GraphBuilderSessionSummarySchema = z
  .object({
    session_id: z.string(),
    title: z.string().catch(""),
    runtime_id: z.string().catch(""),
    created_at: z.string().catch(""),
    updated_at: z.string().catch(""),
    last_message_content: z.string().catch(""),
    last_message_role: z.string().catch(""),
    last_message_at: z.string().catch(""),
  })
  .transform((value) => ({
    sessionId: value.session_id,
    title: value.title,
    runtimeId: value.runtime_id,
    createdAt: value.created_at,
    updatedAt: value.updated_at,
    lastMessageContent: value.last_message_content,
    lastMessageRole: value.last_message_role,
    lastMessageAt: value.last_message_at,
  }));

export const GraphBuilderSessionListSchema = z.object({
  sessions: z.array(GraphBuilderSessionSummarySchema).catch([]),
});

export const EMPTY_GRAPH_BUILDER_SESSION_LIST: {
  sessions: GraphBuilderSessionSummary[];
} = { sessions: [] };

export const GraphVersionSchema = z
  .object({
    id,
    graph_id: id,
    version: z.number().int().positive(),
    definition: GraphDefinitionSchema,
    content_hash: z.string(),
    created_at: timestamp,
  })
  .transform((value) => ({
    id: value.id,
    graphId: value.graph_id,
    version: value.version,
    definition: value.definition,
    contentHash: value.content_hash,
    createdAt: value.created_at,
  }));

export const GraphRecordSchema = z
  .object({
    id,
    workspace_id: id,
    name: z.string(),
    revision: z.number().int().nonnegative(),
    definition: GraphDefinitionSchema,
    creator_id: z.string().optional(),
    archived_at: reason,
    created_at: timestamp,
    updated_at: timestamp,
    versions: z
      .array(GraphVersionSchema)
      .nullish()
      .transform((value) => value ?? []),
  })
  .transform((value) => ({
    id: value.id,
    workspaceId: value.workspace_id,
    name: value.name,
    revision: value.revision,
    definition: value.definition,
    createdBy: value.creator_id,
    archivedAt: value.archived_at,
    createdAt: value.created_at,
    updatedAt: value.updated_at,
    versions: value.versions,
  }));

export const GraphValidationSchema = z.object({
  valid: z.boolean(),
  diagnostics: z
    .array(
      z
        .object({
          code: z.string(),
          message: z.string(),
          path: z.string().optional(),
          node_key: z.string().optional(),
        })
        .transform((value) => ({
          code: value.code,
          message: value.message,
          path: value.path,
          nodeKey: value.node_key,
        })),
    )
    .nullish()
    .transform((value) => value ?? []),
});

export const GraphApprovalSchema = z
  .object({
    id,
    approver_id: z.string().catch(""),
    candidate_digest: id,
    revision: z.number().int().nonnegative(),
    state: z.string(),
    node_run_id: z.string().optional(),
    approver_ids: z.array(id).optional(),
    mode: z.string().optional(),
    quorum: z.number().optional(),
    candidate: object.optional(),
    decisions: z
      .array(
        z
          .object({
            member_id: id,
            user_id: z.string().catch(""),
            decision: z.string(),
            reason: z.string().optional(),
            decided_at: timestamp,
          })
          .transform((v) => ({
            memberId: v.member_id,
            userId: v.user_id,
            decision: v.decision,
            reason: v.reason,
            decidedAt: v.decided_at,
          })),
      )
      .optional(),
  })
  .transform((v) => ({
    id: v.id,
    approverId: v.approver_id,
    candidateDigest: v.candidate_digest,
    revision: v.revision,
    state: v.state,
    nodeRunId: v.node_run_id,
    approverIds: v.approver_ids,
    mode: v.mode,
    quorum: v.quorum,
    candidate: v.candidate,
    decisions: v.decisions,
  }));

export const GraphRunSchema = z
  .object({
    id,
    workspace_id: id,
    root_issue_id: id,
    version_id: id,
    initiator_id: z.string().catch(""),
    state: z.string(),
    control_state: z.string(),
    revision: z.number().int().nonnegative(),
    can_detach: z.boolean().catch(false),
    graph_id: z.string().optional(),
    graph_name: z.string().optional(),
    definition: GraphDefinitionSchema.optional(),
    previous_run_id: z.string().nullish(),
    output: object.nullish(),
    finished_at: z.string().nullish(),
    scopes: z.array(object).optional(),
    approvals: z.array(GraphApprovalSchema).optional(),
    input: object.catch({}),
    input_hash: z.string().catch(""),
    nodes: z
      .array(
        z
          .object({
            id,
            node_key: id,
            label: nodeLabel,
            node_type: z.string(),
            state: z.string(),
            issue_id: reason,
            input: object.catch({}),
            input_hash: z.string().catch(""),
            max_attempts: z.number().int().nonnegative().catch(0),
            granted_attempts: z.number().int().nonnegative().catch(0),
            can_retry: z.boolean().catch(false),
            can_resolve_block: z.boolean().catch(false),
            accepted_submission_id: reason,
            block_reason: reason,
            effect: z
              .object({
                state: z.string(),
                attempts: z.number().int().nonnegative(),
                lease_until: z.string().optional(),
                invocation_id: z.string().optional(),
              })
              .transform((v) => ({
                state: v.state,
                attempts: v.attempts,
                leaseUntil: v.lease_until,
                invocationId: v.invocation_id,
              }))
              .optional(),
            instance_key: z.string().optional(),
            scope_id: z.string().optional(),
            iteration: z.number().optional(),
            output: object.nullish(),
            selected_port: z.string().nullish(),
            started_at: z.string().nullish(),
            finished_at: z.string().nullish(),
            next_attempt_at: z.string().nullish(),
            attempts: z
              .array(
                z
                  .object({
                    task_id: id,
                    attempt_no: z.number().int().positive(),
                    generation: z.number().int().nonnegative(),
                    state: z.string(),
                    manifest: object.catch({}),
                    receipt: object.nullish(),
                  })
                  .transform((value) => ({
                    taskId: value.task_id,
                    attemptNo: value.attempt_no,
                    generation: value.generation,
                    state: value.state,
                    manifest: value.manifest,
                    receipt: value.receipt,
                  })),
              )
              .nullish()
              .transform((value) => value ?? []),
            submissions: z
              .array(object)
              .nullish()
              .transform((value) => value ?? []),
          })
          .transform((value) => ({
            id: value.id,
            nodeKey: value.node_key,
            label: value.label,
            nodeType: value.node_type,
            state: value.state,
            issueId: value.issue_id,
            input: value.input,
            inputHash: value.input_hash,
            maxAttempts: value.max_attempts,
            grantedAttempts: value.granted_attempts,
            attempts: value.attempts,
            canRetry: value.can_retry,
            canResolveBlock: value.can_resolve_block,
            effect: value.effect,
            submissions: value.submissions,
            acceptedSubmissionId: value.accepted_submission_id,
            blockReason: value.block_reason,
            instanceKey: value.instance_key,
            scopeId: value.scope_id,
            iteration: value.iteration,
            output: value.output ?? undefined,
            selectedPort: value.selected_port ?? undefined,
            startedAt: value.started_at ?? undefined,
            finishedAt: value.finished_at ?? undefined,
            nextAttemptAt: value.next_attempt_at ?? undefined,
          })),
      )
      .nullish()
      .transform((value) => value ?? []),
    events: z
      .array(object)
      .nullish()
      .transform((value) => value ?? []),
    artifacts: z
      .array(object)
      .nullish()
      .transform((value) => value ?? []),
    approval: GraphApprovalSchema.nullish(),
    block_reason: reason,
    archived_at: reason,
    created_at: timestamp,
    updated_at: timestamp,
  })
  .transform((value) => ({
    id: value.id,
    workspaceId: value.workspace_id,
    rootIssueId: value.root_issue_id,
    versionId: value.version_id,
    initiatorId: value.initiator_id,
    state: value.state,
    controlState: value.control_state,
    revision: value.revision,
    canDetach: value.can_detach,
    input: value.input,
    inputHash: value.input_hash,
    nodes: value.nodes,
    events: value.events,
    artifacts: value.artifacts,
    approval: value.approval,
    blockReason: value.block_reason,
    archivedAt: value.archived_at,
    createdAt: value.created_at,
    updatedAt: value.updated_at,
    graphId: value.graph_id,
    graphName: value.graph_name,
    definition: value.definition,
    previousRunId: value.previous_run_id ?? undefined,
    output: value.output ?? undefined,
    finishedAt: value.finished_at ?? undefined,
    scopes: value.scopes,
    approvals: value.approvals,
  }));

export const GraphListSchema = z.array(GraphRecordSchema);
export const GraphRunListSchema = z.array(GraphRunSchema);

const counter = z.number().nonnegative().catch(0);
export const GraphStatisticsSchema = z
  .object({
    total: counter,
    active: counter,
    waiting_approval: counter,
    blocked: counter,
    succeeded: counter,
    failed: counter,
    rejected: counter,
    cancelled: counter,
    total_duration_ms: counter,
    total_cost_usd: counter,
    total_tokens: counter,
    unpriced_tasks: counter,
  })
  .transform((v) => ({
    total: v.total,
    active: v.active,
    waitingApproval: v.waiting_approval,
    blocked: v.blocked,
    succeeded: v.succeeded,
    failed: v.failed,
    rejected: v.rejected,
    cancelled: v.cancelled,
    totalDurationMs: v.total_duration_ms,
    totalCostUsd: v.total_cost_usd,
    totalTokens: v.total_tokens,
    unpricedTasks: v.unpriced_tasks,
  }));
export const GraphSearchSchema = z
  .object({
    items: GraphListSchema,
    next_cursor: z.string().nullish(),
    total: counter,
    stats: GraphStatisticsSchema.optional(),
  })
  .transform((v) => ({
    items: v.items,
    nextCursor: v.next_cursor ?? undefined,
    total: v.total,
    stats: v.stats,
  }));
export const GraphRunSearchSchema = z
  .object({
    items: GraphRunListSchema,
    next_cursor: z.string().nullish(),
    total: counter,
    stats: GraphStatisticsSchema.optional(),
  })
  .transform((v) => ({
    items: v.items,
    nextCursor: v.next_cursor ?? undefined,
    total: v.total,
    stats: v.stats,
  }));
export const GraphStartOptionsSchema = z.object({
  repositories: z.array(
    z
      .object({
        id,
        name: z.string(),
        provider: z.string(),
        url: z.string(),
        default_branch: z.string().optional(),
        project_id: z.string().optional(),
        commits_supported: z.boolean().optional(),
      })
      .transform((v) => ({
        id: v.id,
        name: v.name,
        provider: v.provider,
        url: v.url,
        defaultBranch: v.default_branch,
        projectId: v.project_id,
        commitsSupported: v.commits_supported,
      })),
  ),
});
export const GraphCommitsSchema = z
  .object({
    commits: z.array(
      z
        .object({
          sha: id,
          message: z.string(),
          author: z.string(),
          created_at: timestamp,
        })
        .transform((v) => ({
          sha: v.sha,
          message: v.message,
          author: v.author,
          createdAt: v.created_at,
        })),
    ),
    next_cursor: z.string().nullish(),
  })
  .transform((v) => ({
    commits: v.commits,
    nextCursor: v.next_cursor ?? undefined,
  }));
export const GraphTriggerSchema = z
  .object({
    id,
    graph_id: id,
    version_id: id,
    name: z.string(),
    kind: z.enum(["schedule", "webhook", "task_event"]),
    enabled: z.boolean(),
    revision: z.number().int().nonnegative(),
    config: z
      .object({
        cron: z.string().optional(),
        timezone: z.string().optional(),
        project_id: z.string().optional(),
        statuses: z.array(z.string()).optional(),
        events: z.array(z.string()).optional(),
        root_title: z.string().optional(),
        root_description: z.string().optional(),
      })
      .transform((v) => ({
        cron: v.cron,
        timezone: v.timezone,
        projectId: v.project_id,
        statuses: v.statuses,
        events: v.events,
        rootTitle: v.root_title,
        rootDescription: v.root_description,
      })),
    input: object,
    runtime_bindings: z.record(z.string(), z.string()),
    repository: z
      .object({
        repository_id: id,
        base_sha: id,
        commit_sha: z.string().optional(),
        artifact_id: z.string().optional(),
      })
      .transform((v) => ({
        repositoryId: v.repository_id,
        baseSha: v.base_sha,
        commitSha: v.commit_sha,
        artifactId: v.artifact_id,
      }))
      .optional(),
    created_at: timestamp,
    updated_at: timestamp,
    next_fire_at: z.string().nullish(),
    last_error: z.string().nullish(),
    webhook_url: z.string().optional(),
    secret: z.string().optional(),
  })
  .transform((v) => ({
    id: v.id,
    graphId: v.graph_id,
    versionId: v.version_id,
    name: v.name,
    kind: v.kind,
    enabled: v.enabled,
    revision: v.revision,
    config: v.config,
    input: v.input,
    runtimeBindings: v.runtime_bindings,
    repository: v.repository,
    createdAt: v.created_at,
    updatedAt: v.updated_at,
    nextFireAt: v.next_fire_at ?? undefined,
    lastError: v.last_error ?? undefined,
    webhookUrl: v.webhook_url,
    secret: v.secret,
  }));
export const GraphTriggerListSchema = z.object({
  items: z.array(GraphTriggerSchema),
});
export const GraphTriggerSecretSchema = z
  .object({ secret: z.string().min(1), webhook_url: z.string().min(1) })
  .transform((v) => ({ secret: v.secret, webhookUrl: v.webhook_url }));
export const GraphTriggerEventsSchema = z.object({
  items: z.array(
    z
      .object({
        id,
        state: z.string(),
        run_id: z.string().optional(),
        error: z.string().optional(),
        created_at: timestamp,
        updated_at: timestamp,
      })
      .transform((v) => ({
        id: v.id,
        state: v.state,
        runId: v.run_id,
        error: v.error,
        createdAt: v.created_at,
        updatedAt: v.updated_at,
      })),
  ),
});
