export type JsonObject = Record<string, unknown>;

export interface GraphInputBinding {
  source: string;
  nodeKey?: string;
  path?: string;
  value?: unknown;
  fields?: Record<string, GraphInputBinding>;
}

export interface GraphNodeDefinition {
  nodeKey: string;
  type: string;
  agentId?: string;
  instructions?: string;
  inputs?: Record<string, GraphInputBinding>;
  outputSchema?: JsonObject;
  executionMode?: string;
  maxAttempts?: number;
  timeoutSeconds?: number;
  approverId?: string;
}

export interface GraphDefinition {
  schemaVersion: number;
  name: string;
  inputSchema: JsonObject;
  nodes: GraphNodeDefinition[];
  edges: { from: string; to: string }[];
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
  nodeType: string;
  state: string;
  issueId?: string | null;
  input: JsonObject;
  inputHash: string;
  maxAttempts: number;
  canRetry: boolean;
  canResolveBlock: boolean;
  attempts: GraphAttempt[];
  submissions: JsonObject[];
  acceptedSubmissionId?: string | null;
  blockReason?: string | null;
}

export interface GraphApproval {
  id: string;
  approverId: string;
  candidateDigest: string;
  revision: number;
  state: string;
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
}

export type GraphRunAction = "pause" | "resume" | "cancel" | "retry" | "resolve-block" | "archive";
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
  repository?: { repositoryId: string; baseSha: string; commitSha?: string; artifactId?: string };
  deadline?: string;
  idempotencyKey: string;
}
export interface GraphApprovalRequest extends GraphCommand {
  approvalId: string;
  candidateDigest: string;
  decision: "approved" | "rejected";
}
