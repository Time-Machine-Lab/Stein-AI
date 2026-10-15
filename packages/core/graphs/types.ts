export type JsonObject = Record<string, unknown>;

export interface GraphInputBinding {
  source: string;
  nodeKey?: string;
  path?: string;
  value?: unknown;
  fields?: Record<string, GraphInputBinding>;
  optional?: boolean;
  default?: unknown;
}

export interface GraphCondition {
  op: string;
  left?: GraphInputBinding;
  right?: GraphInputBinding;
  conditions?: GraphCondition[];
}
export interface GraphEdge {
  from: string;
  to: string;
  port?: string;
}
export interface GraphLayout {
  nodes: Record<string, { x: number; y: number }>;
  viewport?: { x: number; y: number; zoom: number };
}

export interface GraphNodeDefinition {
  nodeKey: string;
  label?: string;
  type: string;
  agentId?: string;
  instructions?: string;
  inputs?: Record<string, GraphInputBinding>;
  outputSchema?: JsonObject;
  executionMode?: string;
  maxAttempts?: number;
  timeoutSeconds?: number;
  approverId?: string;
  condition?: GraphCondition;
  approval?: { approverIds: string[]; mode: string; quorum?: number };
  loop?: {
    body: GraphDefinition;
    maxIterations: number;
    condition: GraphCondition;
    next: Record<string, GraphInputBinding>;
  };
  subgraph?: { definition: GraphDefinition };
  delay?: { seconds: number };
  http?: {
    method: string;
    url: string;
    headers?: Record<string, GraphInputBinding>;
    body?: GraphInputBinding;
    timeoutSeconds: number;
    maxResponseBytes: number;
  };
  script?: { language: string; code: string; timeoutSeconds: number };
}

export interface GraphDefinition {
  schemaVersion: number;
  name: string;
  inputSchema: JsonObject;
  nodes: GraphNodeDefinition[];
  edges: GraphEdge[];
  outputSchema?: JsonObject;
  outputs?: Record<string, GraphInputBinding>;
  maxConcurrency?: number;
  layout?: GraphLayout;
}

export interface GraphAuthoringMessage {
  role: "user" | "assistant";
  content: string;
}

export interface GraphGenerationRequest {
  prompt: string;
  agentId: string;
  approverId: string;
  currentDefinition?: GraphDefinition;
  messages?: GraphAuthoringMessage[];
}

export interface GraphGenerationResult {
  definition: GraphDefinition;
  summary?: string;
}

export interface GraphGenerationOptions {
  /** The legacy server-model path: one server-side model call per request. */
  enabled: boolean;
  /** The chat-based creation path. false on servers that predate it. */
  builderEnabled: boolean;
}

/**
 * A Graph-design conversation: a chat session on a hidden carrier agent.
 *
 * Nothing about the draft comes back here — the definition lives in the
 * editor's own draft document, and every turn re-states it in the request
 * envelope, so the server never holds a second copy to drift.
 */
export interface GraphBuilderSession {
  sessionId: string;
  /** The hidden carrier; archived or deleted with the session. */
  builderAgentId: string;
  /** Where the conversation runs — cloned from the chosen executor. */
  runtimeId: string;
  model: string;
  /** The executor a new node falls back to (`agent_default`). */
  defaultAgentId: string;
  /** The approver a new approval node falls back to (`approver_default`). */
  approverId: string;
}

/**
 * One unfinished Graph-design conversation, for the case where the client's own
 * draft document is gone. It carries no definition by design: the draft is the
 * client's, and the server has never held a copy to hand back.
 */
export interface GraphBuilderSessionSummary {
  sessionId: string;
  title: string;
  runtimeId: string;
  createdAt: string;
  updatedAt: string;
  /** Still in the wire format — the client decodes it, the server does not. */
  lastMessageContent: string;
  lastMessageRole: string;
  lastMessageAt: string;
}

export interface GraphVersion {
  id: string;
  graphId: string;
  version: number;
  definition: GraphDefinition;
  contentHash: string;
  createdAt: string;
}

export interface GraphRecord {
  id: string;
  workspaceId: string;
  name: string;
  revision: number;
  definition: GraphDefinition;
  createdBy?: string;
  archivedAt?: string | null;
  createdAt: string;
  updatedAt: string;
  versions: GraphVersion[];
}

export interface GraphDiagnostic {
  code: string;
  message: string;
  path?: string;
  nodeKey?: string;
}

export interface GraphValidation {
  valid: boolean;
  diagnostics: GraphDiagnostic[];
}

export interface GraphAttempt {
  taskId: string;
  attemptNo: number;
  generation: number;
  state: string;
  manifest: JsonObject;
  receipt?: JsonObject | null;
}

export interface GraphNodeRun {
  id: string;
  nodeKey: string;
  label?: string;
  nodeType: string;
  state: string;
  issueId?: string | null;
  input: JsonObject;
  inputHash: string;
  maxAttempts: number;
  // Attempts a person unlocked beyond maxAttempts. Each one is a deliberate
  // decision on a blocked node, so they sit outside the published budget and
  // the node's usable budget is maxAttempts + grantedAttempts.
  grantedAttempts: number;
  canRetry: boolean;
  canResolveBlock: boolean;
  attempts: GraphAttempt[];
  submissions: JsonObject[];
  acceptedSubmissionId?: string | null;
  blockReason?: string | null;
  instanceKey?: string;
  scopeId?: string;
  iteration?: number;
  output?: JsonObject;
  selectedPort?: string;
  startedAt?: string;
  finishedAt?: string;
  nextAttemptAt?: string;
  effect?: {
    state: string;
    attempts: number;
    leaseUntil?: string;
    invocationId?: string;
  };
}

export interface GraphApproval {
  id: string;
  approverId: string;
  candidateDigest: string;
  revision: number;
  state: string;
  nodeRunId?: string;
  approverIds?: string[];
  mode?: string;
  quorum?: number;
  decisions?: {
    memberId: string;
    userId: string;
    decision: string;
    reason?: string;
    decidedAt: string;
  }[];
  candidate?: JsonObject;
}

export interface GraphRun {
  id: string;
  workspaceId: string;
  rootIssueId: string;
  versionId: string;
  initiatorId: string;
  state: string;
  controlState: string;
  revision: number;
  input: JsonObject;
  canDetach: boolean;
  inputHash: string;
  nodes: GraphNodeRun[];
  events: JsonObject[];
  artifacts: JsonObject[];
  approval?: GraphApproval | null;
  blockReason?: string | null;
  archivedAt?: string | null;
  createdAt: string;
  updatedAt: string;
  graphId?: string;
  graphName?: string;
  definition?: GraphDefinition;
  previousRunId?: string;
  output?: JsonObject;
  finishedAt?: string;
  scopes?: JsonObject[];
  approvals?: GraphApproval[];
}

export type GraphRunAction =
  "pause" | "resume" | "cancel" | "retry" | "resolve-block" | "archive";
export interface GraphCommand {
  expectedRevision: number;
  idempotencyKey: string;
  nodeRunId?: string;
  reason?: string;
}
export interface GraphStartRequest {
  versionId: string;
  rootIssueId: string;
  input: JsonObject;
  runtimeBindings: Record<string, string>;
  repository?: {
    repositoryId: string;
    baseSha: string;
    commitSha?: string;
    artifactId?: string;
  };
  deadline?: string;
  idempotencyKey: string;
  previousRunId?: string;
}
export interface GraphSearchParams {
  q?: string;
  state?: string;
  graphId?: string;
  cursor?: string;
  limit?: number;
}
export interface GraphStatistics {
  total: number;
  active: number;
  waitingApproval: number;
  blocked: number;
  succeeded: number;
  failed: number;
  rejected: number;
  cancelled: number;
  totalDurationMs: number;
  totalCostUsd: number;
  totalTokens: number;
  unpricedTasks?: number;
}
export interface GraphSearchResult<T> {
  items: T[];
  nextCursor?: string;
  total: number;
  stats?: GraphStatistics;
}
export interface GraphRepositoryOption {
  id: string;
  name: string;
  provider: string;
  url: string;
  defaultBranch?: string;
  projectId?: string;
  commitsSupported?: boolean;
}
export interface GraphCommitOption {
  sha: string;
  message: string;
  author: string;
  createdAt: string;
}
export interface GraphTriggerConfig {
  cron?: string;
  timezone?: string;
  projectId?: string;
  statuses?: string[];
  events?: string[];
  rootTitle?: string;
  rootDescription?: string;
}
export interface GraphTriggerWrite {
  name: string;
  kind: "schedule" | "webhook" | "task_event";
  versionId: string;
  enabled: boolean;
  config: GraphTriggerConfig;
  input: JsonObject;
  runtimeBindings: Record<string, string>;
  repository?: GraphStartRequest["repository"];
  expectedRevision?: number;
  idempotencyKey?: string;
}
export interface GraphTrigger extends GraphTriggerWrite {
  id: string;
  graphId: string;
  revision: number;
  createdAt: string;
  updatedAt: string;
  nextFireAt?: string;
  lastError?: string;
  webhookUrl?: string;
  secret?: string;
}
export interface GraphApprovalRequest extends GraphCommand {
  approvalId: string;
  candidateDigest: string;
  decision: "approved" | "rejected";
}
