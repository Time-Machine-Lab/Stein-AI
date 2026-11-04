import {
  createSerialDefinition,
  type GraphDefinition,
  type GraphNodeDefinition,
} from "@multica/core/graphs";
import { defaultCondition } from "./graph-bindings";

export const objectSchema = { type: "object", additionalProperties: true };

export function blankDefinition(name = ""): GraphDefinition {
  return {
    ...createSerialDefinition(
      name,
      [
        {
          nodeKey: "implement",
          type: "agent",
          agentId: "",
          instructions: "",
          inputs: { context: { source: "run_input", path: "" } },
          outputSchema: objectSchema,
          executionMode: "none",
          maxAttempts: 16,
          timeoutSeconds: 21600,
        },
      ],
      "",
    ),
    schemaVersion: 2,
    maxConcurrency: 4,
  };
}

function emptyBody(name: string): GraphDefinition {
  return {
    schemaVersion: 2,
    name,
    inputSchema: objectSchema,
    outputSchema: objectSchema,
    outputs: {},
    nodes: [
      { nodeKey: "start", type: "start" },
      { nodeKey: "end", type: "end" },
    ],
    edges: [{ from: "start", to: "end" }],
  };
}

/** A fresh step of `type` with the defaults the editor has always used. */
export function newGraphNode(
  type: string,
  nodeKey: string,
): GraphNodeDefinition {
  const base = { nodeKey, type };
  switch (type) {
    case "agent":
      return { ...blankDefinition().nodes[1]!, nodeKey };
    case "approval":
      return { ...base, approval: { approverIds: [], mode: "all" } };
    case "branch":
      return { ...base, condition: defaultCondition() };
    case "delay":
      return { ...base, delay: { seconds: 60 } };
    case "http":
      return {
        ...base,
        outputSchema: objectSchema,
        http: {
          method: "GET",
          url: "",
          timeoutSeconds: 30,
          maxResponseBytes: 65536,
        },
      };
    case "script":
      return {
        ...base,
        agentId: "",
        inputs: {},
        outputSchema: objectSchema,
        script: { language: "javascript", code: "", timeoutSeconds: 60 },
      };
    case "loop":
      return {
        ...base,
        inputs: {},
        outputSchema: objectSchema,
        loop: {
          body: emptyBody(nodeKey),
          maxIterations: 16,
          condition: {
            op: "eq",
            left: { source: "constant", value: false },
            right: { source: "constant", value: true },
          },
          next: {},
        },
      };
    case "subgraph":
      return {
        ...base,
        inputs: {},
        outputSchema: objectSchema,
        subgraph: { definition: emptyBody(nodeKey) },
      };
    default:
      return base;
  }
}

/** The loop body or subgraph definition at `path`, or the root. */
export function getScope(
  definition: GraphDefinition,
  path: string[],
): GraphDefinition {
  for (const key of path) {
    const node = definition.nodes.find((item) => item.nodeKey === key);
    if (node?.loop) definition = node.loop.body;
    else if (node?.subgraph) definition = node.subgraph.definition;
    else break;
  }
  return definition;
}

export function replaceScope(
  definition: GraphDefinition,
  path: string[],
  updated: GraphDefinition,
): GraphDefinition {
  if (!path.length) return updated;
  return {
    ...definition,
    schemaVersion: 2,
    nodes: definition.nodes.map((node) => {
      if (node.nodeKey !== path[0]) return node;
      if (node.loop) {
        const body = replaceScope(node.loop.body, path.slice(1), updated);
        return {
          ...node,
          outputSchema: body.outputSchema,
          loop: { ...node.loop, body },
        };
      }
      if (node.subgraph) {
        const child = replaceScope(
          node.subgraph.definition,
          path.slice(1),
          updated,
        );
        return {
          ...node,
          outputSchema: child.outputSchema,
          subgraph: { definition: child },
        };
      }
      return node;
    }),
  };
}

/** Whether any scope on `path` is a loop body (loop-only bindings apply). */
export function scopeInLoop(
  definition: GraphDefinition,
  path: string[],
): boolean {
  return path.some(
    (key, index) =>
      getScope(definition, path.slice(0, index)).nodes.find(
        (node) => node.nodeKey === key,
      )?.type === "loop",
  );
}
