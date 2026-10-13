import type { GraphNodeRun, GraphRun, GraphRunAction } from "./types";
import type { Issue, RuntimeDevice } from "../types";

export function isGraphManagedIssue(issue: Pick<Issue, "execution_mode" | "graph_run_id" | "graph_node_run_id"> | null | undefined): boolean {
  return issue?.execution_mode === "graph" || Boolean(issue?.graph_run_id || issue?.graph_node_run_id);
}

export interface GraphActor { userId: string; memberId?: string; role?: string }
export const GRAPH_TERMINAL_STATES = new Set(["succeeded", "rejected", "failed", "cancelled"]);
const activeStates = new Set(["active", "blocked", "waiting_approval"]);

export function isGraphRuntimeSupported(runtime: Pick<RuntimeDevice, "provider" | "metadata">): boolean {
  return runtime.provider === "codex" && Array.isArray(runtime.metadata.capabilities)
    && runtime.metadata.capabilities.includes("graph-execution-v1");
}

export function canManageGraphRun(run: GraphRun | null | undefined, actor: GraphActor): boolean {
  return Boolean(run && actor.userId && (run.initiatorId === actor.userId || actor.role === "owner" || actor.role === "admin"));
}

export function graphRunActions(run: GraphRun | null | undefined, actor: GraphActor): GraphRunAction[] {
  if (!run || !canManageGraphRun(run, actor) || run.archivedAt) return [];
  if (GRAPH_TERMINAL_STATES.has(run.state)) return ["archive"];
  if (!activeStates.has(run.state)) return [];
  switch (run.controlState) {
    case "run": return ["pause", "cancel"];
    case "pause": return ["resume", "cancel"];
    default: return [];
  }
}

export function nodeCanRetry(node: GraphNodeRun): boolean {
  if (node.nodeType !== "agent" || node.state !== "blocked" || node.blockReason === "execution_unknown" || node.canRetry !== true) return false;
  const attempts = node.attempts;
  const last = [...attempts].sort((a, b) => b.attemptNo - a.attemptNo)[0];
  if (!last || last.attemptNo >= node.maxAttempts || node.maxAttempts <= 0) return false;
  return ["completed", "failed", "cancelled"].includes(last.state);
}

export function canApproveGraphRun(run: GraphRun, actor: GraphActor): boolean {
  return run.state === "waiting_approval" && run.controlState === "run"
    && run.approval?.state === "pending" && Boolean(run.approval.candidateDigest)
    && Boolean(actor.memberId && run.approval.approverId === actor.memberId);
}

export function canReplaceGraphApprover(run: GraphRun, actor: GraphActor): boolean {
  return Boolean(actor.userId && (actor.role === "owner" || actor.role === "admin"))
    && (run.state === "waiting_approval" || (run.state === "blocked" && run.blockReason === "approver_unavailable"))
    && run.controlState !== "cancel_requested" && !run.archivedAt;
}

export function canDetachGraphRun(run: GraphRun | null | undefined, actor: GraphActor): boolean {
  return Boolean(run && run.canDetach === true && GRAPH_TERMINAL_STATES.has(run.state) && canManageGraphRun(run, actor));
}
