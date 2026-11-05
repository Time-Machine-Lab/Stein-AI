import type {
  GraphDefinition,
  GraphEdge,
  GraphNodeDefinition,
  GraphNodeRun,
  GraphRun,
} from "./types";

/**
 * Pure helpers behind the AI-first Graph studio: inserting a step on an
 * existing connection, reading a run's progress, and grouping recent runs by
 * the graph they belong to. Kept free of React so they are testable on their
 * own and reusable from mobile.
 */

/** Node types that cannot sit in the middle of a single connection. */
const NOT_INSERTABLE = new Set(["start", "end", "join"]);

export function canInsertGraphNode(type: string): boolean {
  return !NOT_INSERTABLE.has(type);
}

/** The first free `<type>_<n>` key in a scope, and its ordinal. */
export function nextGraphNodeKey(
  definition: GraphDefinition,
  type: string,
): { key: string; index: number } {
  let index = definition.nodes.filter((node) => node.type === type).length + 1;
  let key = `${type}_${index}`;
  while (definition.nodes.some((node) => node.nodeKey === key))
    key = `${type}_${++index}`;
  return { key, index };
}

/**
 * Replaces the connection `edge` with `edge.from -> node -> edge.to`.
 *
 * The incoming side keeps the original port, so a step inserted on a branch's
 * "true" path stays on that path. A branch inserted in the middle continues the
 * original target on its own "true" outcome; its "false" outcome is left for
 * the user to connect.
 */
export function insertGraphNodeOnEdge(
  definition: GraphDefinition,
  edge: GraphEdge,
  node: GraphNodeDefinition,
): GraphDefinition {
  if (!canInsertGraphNode(node.type))
    throw new Error("This step cannot be inserted on a connection.");
  if (definition.nodes.some((item) => item.nodeKey === node.nodeKey))
    throw new Error("Node keys must be unique identifiers.");
  const index = definition.edges.findIndex(
    (item) =>
      item.from === edge.from &&
      item.to === edge.to &&
      (item.port ?? "") === (edge.port ?? ""),
  );
  if (index < 0) throw new Error("The connection no longer exists.");
  const original = definition.edges[index]!;
  const incoming: GraphEdge = {
    from: original.from,
    to: node.nodeKey,
    ...(original.port ? { port: original.port } : {}),
  };
  const outgoing: GraphEdge = {
    from: node.nodeKey,
    to: original.to,
    ...(node.type === "branch" ? { port: "true" } : {}),
  };
  const positions = definition.layout?.nodes;
  const from = positions?.[original.from];
  const to = positions?.[original.to];
  return {
    ...definition,
    schemaVersion: 2,
    nodes: [...definition.nodes, node],
    edges: [
      ...definition.edges.slice(0, index),
      incoming,
      outgoing,
      ...definition.edges.slice(index + 1),
    ],
    ...(definition.layout
      ? {
          layout: {
            ...definition.layout,
            nodes: {
              ...definition.layout.nodes,
              ...(from && to
                ? {
                    [node.nodeKey]: {
                      x: Math.round((from.x + to.x) / 2),
                      y: Math.round((from.y + to.y) / 2) + 80,
                    },
                  }
                : {}),
            },
          },
        }
      : {}),
  };
}

/** Node states that count as finished for a run's progress bar. */
const DONE_STATES = new Set(["succeeded", "skipped", "completed", "approved"]);
/** Synthetic nodes that do no work and would inflate the step count. */
const STRUCTURAL = new Set(["start", "end"]);

export interface GraphRunProgress {
  done: number;
  total: number;
  failed: number;
}

/**
 * How far a run has come, counted over the root scope's working steps. Nested
 * loop and subgraph iterations are left out: their counts change as the run
 * advances, which would make the bar move backwards.
 */
export function graphRunProgress(run: Pick<GraphRun, "nodes" | "scopes">): GraphRunProgress {
  const rootScope = run.scopes?.find((scope) => !scope.parent_node_run_id)?.id;
  const nodes = (run.nodes ?? []).filter(
    (node: GraphNodeRun) =>
      !STRUCTURAL.has(node.nodeType) &&
      (rootScope ? node.scopeId === rootScope || !node.scopeId : true),
  );
  return {
    done: nodes.filter((node) => DONE_STATES.has(node.state)).length,
    total: nodes.length,
    failed: nodes.filter((node) => node.state === "failed").length,
  };
}

/**
 * Recent runs by graph, newest first, at most `perGraph` each. The list page
 * reads a graph's health from this instead of asking the server once per row.
 */
export function groupLatestRuns(
  runs: GraphRun[],
  perGraph = 7,
): Map<string, GraphRun[]> {
  const sorted = [...runs].sort(
    (a, b) => Date.parse(b.createdAt || "") - Date.parse(a.createdAt || ""),
  );
  const grouped = new Map<string, GraphRun[]>();
  for (const run of sorted) {
    if (!run.graphId) continue;
    const list = grouped.get(run.graphId) ?? [];
    if (list.length < perGraph) list.push(run);
    grouped.set(run.graphId, list);
  }
  return grouped;
}

const TERMINAL = new Set(["succeeded", "failed", "rejected", "cancelled"]);

/**
 * How long a run has taken: to its finish, or to `now` while it is live. A
 * terminal run without `finishedAt` falls back to its last update, the same
 * rule the server's duration statistics use.
 */
export function graphRunDurationMs(
  run: Pick<GraphRun, "state" | "createdAt" | "updatedAt" | "finishedAt">,
  now = Date.now(),
): number | null {
  const start = Date.parse(run.createdAt || "");
  if (Number.isNaN(start)) return null;
  const end = run.finishedAt
    ? Date.parse(run.finishedAt)
    : TERMINAL.has(run.state)
      ? Date.parse(run.updatedAt || "")
      : now;
  if (Number.isNaN(end)) return null;
  return Math.max(0, end - start);
}

/**
 * The step a run event is about. Events carry a bare reference in `message`:
 * a task id (attempt.created, node.submitted), a `<scope>/<nodeKey>` pair
 * (node.accepted, approval.requested), or a node run id. Anything else is not
 * a reference and returns undefined.
 */
export function graphEventNode(
  run: Pick<GraphRun, "nodes">,
  message: unknown,
): GraphNodeRun | undefined {
  if (typeof message !== "string" || !message) return undefined;
  const nodes = run.nodes ?? [];
  const slash = message.indexOf("/");
  if (slash > 0) {
    const scope = message.slice(0, slash);
    const key = message.slice(slash + 1);
    return (
      nodes.find((node) => node.scopeId === scope && node.nodeKey === key) ??
      nodes.find((node) => node.nodeKey === key)
    );
  }
  return nodes.find(
    (node) =>
      node.id === message ||
      node.attempts.some((attempt) => attempt.taskId === message) ||
      node.submissions.some(
        (submission) =>
          submission.id === message || submission.task_id === message,
      ),
  );
}

/** The step whose attempt produced an artifact, from its producer task. */
export function graphArtifactProducer(
  run: Pick<GraphRun, "nodes">,
  taskId: unknown,
): GraphNodeRun | undefined {
  if (typeof taskId !== "string" || !taskId) return undefined;
  return (run.nodes ?? []).find((node) =>
    node.attempts.some((attempt) => attempt.taskId === taskId),
  );
}

/** Node keys an AI turn touched, for marking them on the canvas. */
export function graphChangeMarks(
  added: string[],
  changed: string[],
): Record<string, "added" | "changed"> {
  return {
    ...Object.fromEntries(changed.map((key) => [key, "changed" as const])),
    ...Object.fromEntries(added.map((key) => [key, "added" as const])),
  };
}
