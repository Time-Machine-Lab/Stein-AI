import type { GraphNodeRun, GraphRun, GraphRunAction } from "./types";
import type { Issue, RuntimeDevice } from "../types";

export function isGraphManagedIssue(
  issue:
    | Pick<Issue, "execution_mode" | "graph_run_id" | "graph_node_run_id">
    | null
    | undefined,
): boolean {
  return (
    issue?.execution_mode === "graph" ||
    Boolean(issue?.graph_run_id || issue?.graph_node_run_id)
  );
}

export interface GraphActor {
  userId: string;
  memberId?: string;
  role?: string;
}
export const GRAPH_TERMINAL_STATES = new Set([
  "succeeded",
  "rejected",
  "failed",
  "cancelled",
]);
const activeStates = new Set(["active", "blocked", "waiting_approval"]);

export function isGraphRuntimeSupported(
  runtime: Pick<RuntimeDevice, "provider" | "metadata">,
  requireV2 = false,
): boolean {
  const capabilities = Array.isArray(runtime.metadata.capabilities)
    ? runtime.metadata.capabilities
    : [];
  return (
    capabilities.includes("graph-execution-v2") ||
    (!requireV2 &&
      runtime.provider === "codex" &&
      capabilities.includes("graph-execution-v1"))
  );
}

export function canManageGraphRun(
  run: GraphRun | null | undefined,
  actor: GraphActor,
): boolean {
  return Boolean(
    run &&
    actor.userId &&
    (run.initiatorId === actor.userId ||
      actor.role === "owner" ||
      actor.role === "admin"),
  );
}

export function graphRunActions(
  run: GraphRun | null | undefined,
  actor: GraphActor,
): GraphRunAction[] {
  if (!run || !canManageGraphRun(run, actor) || run.archivedAt) return [];
  if (GRAPH_TERMINAL_STATES.has(run.state)) return ["archive"];
  if (!activeStates.has(run.state)) return [];
  switch (run.controlState) {
    case "run":
      return ["pause", "cancel"];
    case "pause":
      return ["resume", "cancel"];
    default:
      return [];
  }
}

export function nodeCanRetry(node: GraphNodeRun): boolean {
  if (
    node.state !== "blocked" ||
    node.blockReason === "execution_unknown" ||
    node.canRetry !== true
  )
    return false;
  if (node.nodeType === "http")
    return Boolean(
      node.effect &&
      ["unknown", "retryable", "completed"].includes(node.effect.state) &&
      node.effect.attempts < 3,
    );
  if (!["agent", "script"].includes(node.nodeType)) return false;
  const attempts = node.attempts;
  const last = [...attempts].sort((a, b) => b.attemptNo - a.attemptNo)[0];
  if (!last || last.attemptNo >= node.maxAttempts || node.maxAttempts <= 0)
    return false;
  return ["completed", "failed", "cancelled"].includes(last.state);
}

export function canApproveGraphRun(run: GraphRun, actor: GraphActor): boolean {
  return (
    run.controlState === "run" &&
    activeStates.has(run.state) &&
    !run.archivedAt &&
    graphPendingApprovals(run, actor.memberId).length > 0
  );
}

export function graphPendingApprovals(run: GraphRun, memberId?: string) {
  const approvals = run.approvals?.length
    ? run.approvals
    : run.approval
      ? [run.approval]
      : [];
  return approvals.filter(
    (approval) =>
      approval.state === "pending" &&
      approval.candidateDigest &&
      memberId &&
      (approval.approverIds?.length
        ? approval.approverIds
        : [approval.approverId]
      ).includes(memberId) &&
      !approval.decisions?.some((decision) => decision.memberId === memberId),
  );
}

export function canReplaceGraphApprover(
  run: GraphRun,
  actor: GraphActor,
): boolean {
  return (
    Boolean(
      actor.userId && (actor.role === "owner" || actor.role === "admin"),
    ) &&
    ((["active", "waiting_approval"].includes(run.state) &&
      (run.approval?.state === "pending" ||
        run.approvals?.some((gate) => gate.state === "pending"))) ||
      (run.state === "blocked" &&
        run.blockReason === "approver_unavailable")) &&
    run.controlState !== "cancel_requested" &&
    !run.archivedAt
  );
}

export function canDetachGraphRun(
  run: GraphRun | null | undefined,
  actor: GraphActor,
): boolean {
  return Boolean(
    run &&
    run.canDetach === true &&
    GRAPH_TERMINAL_STATES.has(run.state) &&
    canManageGraphRun(run, actor),
  );
}
