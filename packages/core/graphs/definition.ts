import type {
  GraphCondition,
  GraphDefinition,
  GraphEdge,
  GraphInputBinding,
  GraphNodeDefinition,
  JsonObject,
} from "./types";
import { graphUtf8Length } from "./text";
import { GraphDefinitionSchema } from "./schemas";

export function createSerialDefinition(
  name: string,
  steps: GraphNodeDefinition[],
  approverId: string,
  inputSchema: JsonObject = { type: "object", additionalProperties: true },
  controlKeys: { start: string; approval: string; end: string } = {
    start: "start",
    approval: "approval",
    end: "end",
  },
): GraphDefinition {
  const nodes: GraphNodeDefinition[] = [
    { nodeKey: controlKeys.start, type: "start" },
    ...steps,
    { nodeKey: controlKeys.approval, type: "approval", approverId },
    { nodeKey: controlKeys.end, type: "end" },
  ];
  return {
    schemaVersion: 1,
    name,
    inputSchema,
    nodes,
    edges: nodes
      .slice(1)
      .map((node, index) => ({
        from: nodes[index]!.nodeKey,
        to: node.nodeKey,
      })),
  };
}

export function bindingToWire(
  binding: GraphInputBinding,
): Record<string, unknown> {
  return {
    source: binding.source,
    ...(binding.nodeKey !== undefined ? { node_key: binding.nodeKey } : {}),
    ...(binding.path !== undefined ? { path: binding.path } : {}),
    ...(binding.value !== undefined ? { value: binding.value } : {}),
    ...(binding.fields
      ? {
          fields: Object.fromEntries(
            Object.entries(binding.fields).map(([key, value]) => [
              key,
              bindingToWire(value),
            ]),
          ),
        }
      : {}),
    ...(binding.optional !== undefined ? { optional: binding.optional } : {}),
    ...(binding.default !== undefined ? { default: binding.default } : {}),
  };
}

export function conditionToWire(
  condition: GraphCondition,
): Record<string, unknown> {
  return {
    op: condition.op,
    ...(condition.left ? { left: bindingToWire(condition.left) } : {}),
    ...(condition.right ? { right: bindingToWire(condition.right) } : {}),
    ...(condition.conditions
      ? { conditions: condition.conditions.map(conditionToWire) }
      : {}),
  };
}
const bindingsToWire = (bindings: Record<string, GraphInputBinding>) =>
  Object.fromEntries(
    Object.entries(bindings).map(([key, value]) => [key, bindingToWire(value)]),
  );

export function definitionToWire(definition: GraphDefinition): {
  schema_version: number;
  name: string;
  input_schema: JsonObject;
  nodes: Array<
    Record<string, unknown> & {
      node_key: string;
      type: string;
      inputs?: Record<string, Record<string, unknown>>;
    }
  >;
  edges: GraphEdge[];
  [key: string]: unknown;
} {
  return {
    schema_version: definition.schemaVersion,
    name: definition.name,
    input_schema: definition.inputSchema,
    ...(definition.outputSchema !== undefined
      ? { output_schema: definition.outputSchema }
      : {}),
    ...(definition.outputs !== undefined
      ? { outputs: bindingsToWire(definition.outputs) }
      : {}),
    ...(definition.maxConcurrency !== undefined
      ? { max_concurrency: definition.maxConcurrency }
      : {}),
    ...(definition.layout !== undefined ? { layout: definition.layout } : {}),
    nodes: definition.nodes.map((node) => ({
      node_key: node.nodeKey,
      type: node.type,
      ...(node.label !== undefined ? { label: node.label } : {}),
      ...(node.agentId !== undefined ? { agent_id: node.agentId } : {}),
      ...(node.instructions !== undefined
        ? { instructions: node.instructions }
        : {}),
      ...(node.inputs !== undefined
        ? {
            inputs: Object.fromEntries(
              Object.entries(node.inputs).map(([key, value]) => [
                key,
                bindingToWire(value),
              ]),
            ),
          }
        : {}),
      ...(node.outputSchema !== undefined
        ? { output_schema: node.outputSchema }
        : {}),
      ...(node.executionMode !== undefined
        ? { execution_mode: node.executionMode }
        : {}),
      ...(node.maxAttempts !== undefined
        ? { max_attempts: node.maxAttempts }
        : {}),
      ...(node.timeoutSeconds !== undefined
        ? { timeout_seconds: node.timeoutSeconds }
        : {}),
      ...(node.approverId !== undefined
        ? { approver_id: node.approverId }
        : {}),
      ...(node.condition ? { condition: conditionToWire(node.condition) } : {}),
      ...(node.approval
        ? {
            approval: {
              approver_ids: node.approval.approverIds,
              mode: node.approval.mode,
              ...(node.approval.quorum !== undefined
                ? { quorum: node.approval.quorum }
                : {}),
            },
          }
        : {}),
      ...(node.loop
        ? {
            loop: {
              body: definitionToWire(node.loop.body),
              max_iterations: node.loop.maxIterations,
              condition: conditionToWire(node.loop.condition),
              next: bindingsToWire(node.loop.next),
            },
          }
        : {}),
      ...(node.subgraph
        ? {
            subgraph: {
              definition: definitionToWire(node.subgraph.definition),
            },
          }
        : {}),
      ...(node.delay ? { delay: node.delay } : {}),
      ...(node.http
        ? {
            http: {
              method: node.http.method,
              url: node.http.url,
              timeout_seconds: node.http.timeoutSeconds,
              max_response_bytes: node.http.maxResponseBytes,
              ...(node.http.headers
                ? { headers: bindingsToWire(node.http.headers) }
                : {}),
              ...(node.http.body
                ? { body: bindingToWire(node.http.body) }
                : {}),
            },
          }
        : {}),
      ...(node.script
        ? {
            script: {
              language: node.script.language,
              code: node.script.code,
              timeout_seconds: node.script.timeoutSeconds,
            },
          }
        : {}),
    })),
    edges: definition.edges,
  };
}

/** Persisted node order is not execution order; follow the published edge chain. */
export function orderedAgentNodes(
  definition: GraphDefinition,
): GraphNodeDefinition[] {
  if (definition.schemaVersion === 2)
    return executionNodes(definition).map(({ node }) => node);
  const result: GraphNodeDefinition[] = [];
  const visited = new Set<string>();
  let node = definition.nodes.find((candidate) => candidate.type === "start");
  while (node && !visited.has(node.nodeKey)) {
    visited.add(node.nodeKey);
    if (node.type === "agent") result.push(node);
    const nextKey = definition.edges.find(
      (edge) => edge.from === node!.nodeKey,
    )?.to;
    node = definition.nodes.find((candidate) => candidate.nodeKey === nextKey);
  }
  return result;
}

export function parseJsonObject(text: string): JsonObject {
  const value: unknown = JSON.parse(text);
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new Error("Expected a JSON object.");
  }
  return value as JsonObject;
}

export function exportGraphDefinition(definition: GraphDefinition): string {
  return JSON.stringify(definitionToWire(definition), null, 2);
}

function canonicalImportValue(value: unknown): string {
  if (Array.isArray(value))
    return `[${value.map(canonicalImportValue).join(",")}]`;
  if (value !== null && typeof value === "object") {
    return `{${Object.entries(value)
      .sort(([a], [b]) => a.localeCompare(b))
      .map(
        ([key, item]) => `${JSON.stringify(key)}:${canonicalImportValue(item)}`,
      )
      .join(",")}}`;
  }
  return JSON.stringify(value);
}

/** Validate the editor's lossless V1 subset. Saving/publishing also runs server validation. */
export function importGraphDefinition(text: string): GraphDefinition {
  if (graphUtf8Length(text) > 256 * 1024)
    throw new Error("Definition exceeds the 256 KiB limit.");
  const raw: unknown = JSON.parse(text);
  const definition = GraphDefinitionSchema.parse(raw);
  if (![1, 2].includes(definition.schemaVersion))
    throw new Error("Only Graph schema versions 1 and 2 can be imported.");
  if (
    canonicalImportValue(raw) !==
    canonicalImportValue(JSON.parse(exportGraphDefinition(definition)))
  ) {
    throw new Error(
      "Definition contains unsupported fields; importing would discard data.",
    );
  }
  if (!definition.name.trim() || graphUtf8Length(definition.name) > 200)
    throw new Error("Name must contain 1 to 200 bytes.");
  const nodes = new Map(definition.nodes.map((node) => [node.nodeKey, node]));
  if (
    nodes.size !== definition.nodes.length ||
    definition.nodes.some(
      (node) => !/^[a-zA-Z][a-zA-Z0-9_-]{0,63}$/.test(node.nodeKey),
    )
  )
    throw new Error(
      "Node keys must be unique identifiers of at most 64 characters.",
    );
  if (definition.schemaVersion === 2) {
    if (definition.nodes.length > 128 || definition.edges.length > 256)
      throw new Error("Graph exceeds node or connection limits.");
    if (
      definition.nodes.some(
        (node) =>
          ![
            "start",
            "end",
            "agent",
            "approval",
            "branch",
            "parallel",
            "join",
            "loop",
            "subgraph",
            "delay",
            "http",
            "script",
          ].includes(node.type),
      )
    )
      throw new Error("Unsupported node type.");
    return definition;
  }
  const starts = definition.nodes.filter((node) => node.type === "start");
  const approvals = definition.nodes.filter((node) => node.type === "approval");
  const ends = definition.nodes.filter((node) => node.type === "end");
  const agents = definition.nodes.filter((node) => node.type === "agent");
  if (
    starts.length !== 1 ||
    approvals.length !== 1 ||
    ends.length !== 1 ||
    agents.length < 1 ||
    agents.length > 16 ||
    definition.nodes.length !== agents.length + 3
  )
    throw new Error("V1 requires start, 1–16 agent steps, approval and end.");
  if (definition.edges.length !== definition.nodes.length - 1)
    throw new Error("V1 requires a single serial edge chain.");
  const ordered: GraphNodeDefinition[] = [];
  const visited = new Set<string>();
  let node: GraphNodeDefinition | undefined = starts[0];
  while (node && !visited.has(node.nodeKey)) {
    visited.add(node.nodeKey);
    ordered.push(node);
    const edges = definition.edges.filter(
      (edge) => edge.from === node!.nodeKey,
    );
    if (edges.length > 1)
      throw new Error("Parallel edges are not supported in V1.");
    node = edges.length ? nodes.get(edges[0]!.to) : undefined;
  }
  if (
    node ||
    ordered.length !== nodes.size ||
    ordered.at(-1)?.type !== "end" ||
    ordered.at(-2)?.type !== "approval" ||
    ordered.slice(1, -2).some((item) => item.type !== "agent")
  )
    throw new Error("Edges must form start → agent steps → approval → end.");
  if (definition.inputSchema.type !== "object")
    throw new Error("Run input schema must have type object.");
  if (!approvals[0]!.approverId)
    throw new Error("Approval requires a member ID.");
  for (const control of definitionToWire(definition).nodes.filter(
    (item) => item.type !== "agent",
  )) {
    const allowed =
      control.type === "approval"
        ? ["node_key", "label", "type", "approver_id"]
        : ["node_key", "label", "type"];
    if (Object.keys(control).some((key) => !allowed.includes(key)))
      throw new Error(
        `Control node ${control.node_key} contains fields the editor cannot preserve.`,
      );
  }
  for (const agent of agents) {
    if (
      !agent.agentId ||
      !agent.instructions?.trim() ||
      agent.outputSchema?.type !== "object" ||
      !["none", "produce_commit", "verify_commit"].includes(
        agent.executionMode ?? "",
      ) ||
      !agent.maxAttempts ||
      agent.maxAttempts < 1 ||
      agent.maxAttempts > 3 ||
      !agent.timeoutSeconds ||
      agent.timeoutSeconds < 1 ||
      agent.timeoutSeconds > 86400
    )
      throw new Error(
        `Agent step ${agent.nodeKey} is incomplete or exceeds V1 limits.`,
      );
  }
  return definition;
}

/** Stable definition paths bind every nested execution independently of iteration IDs. */
export function executionNodes(
  definition: GraphDefinition,
  prefix = "",
): { path: string; node: GraphNodeDefinition }[] {
  return definition.nodes.flatMap((node) => {
    const path = `${prefix}${node.nodeKey}`;
    if (node.type === "agent" || node.type === "script")
      return [{ path, node }];
    if (node.loop) return executionNodes(node.loop.body, `${path}/body/`);
    if (node.subgraph)
      return executionNodes(node.subgraph.definition, `${path}/definition/`);
    return [];
  });
}

export function graphAncestors(
  definition: GraphDefinition,
  nodeKey: string,
): GraphNodeDefinition[] {
  const found = new Set<string>();
  function visit(key: string) {
    for (const edge of definition.edges.filter((item) => item.to === key))
      if (!found.has(edge.from)) {
        found.add(edge.from);
        visit(edge.from);
      }
  }
  visit(nodeKey);
  return definition.nodes.filter((node) => found.has(node.nodeKey));
}

export function connectGraphNodes(
  definition: GraphDefinition,
  edge: GraphEdge,
): GraphDefinition {
  const source = definition.nodes.find((node) => node.nodeKey === edge.from),
    target = definition.nodes.find((node) => node.nodeKey === edge.to);
  if (
    !source ||
    !target ||
    edge.from === edge.to ||
    source.type === "end" ||
    target.type === "start" ||
    graphAncestors(definition, edge.from).some(
      (node) => node.nodeKey === edge.to,
    )
  )
    throw new Error(
      "This connection would create a cycle or connect incompatible ports.",
    );
  if (source.type === "branch" && !["true", "false"].includes(edge.port ?? ""))
    throw new Error("Choose a branch outcome.");
  if (source.type !== "branch" && edge.port)
    throw new Error("Only a branch has outcome ports.");
  const edges = definition.edges.filter(
    (item) =>
      !(
        item.from === edge.from &&
        item.to === edge.to &&
        item.port === edge.port
      ) &&
      !(
        source.type !== "parallel" &&
        item.from === edge.from &&
        item.port === edge.port
      ) &&
      !(target.type !== "join" && item.to === edge.to),
  );
  return { ...definition, schemaVersion: 2, edges: [...edges, edge] };
}

export function renameGraphNode(
  definition: GraphDefinition,
  key: string,
  nextKey: string,
): GraphDefinition {
  if (key === nextKey) return definition;
  if (
    !/^[a-zA-Z][a-zA-Z0-9_-]{0,63}$/.test(nextKey) ||
    definition.nodes.some((node) => node.nodeKey === nextKey)
  )
    throw new Error("Node keys must be unique identifiers.");
  const binding = (item: GraphInputBinding): GraphInputBinding => ({
    ...item,
    ...(item.nodeKey === key ? { nodeKey: nextKey } : {}),
    ...(item.fields ? { fields: map(item.fields) } : {}),
  });
  const map = (values: Record<string, GraphInputBinding>) =>
    Object.fromEntries(
      Object.entries(values).map(([name, value]) => [name, binding(value)]),
    );
  const condition = (value: GraphCondition): GraphCondition => ({
    ...value,
    ...(value.left ? { left: binding(value.left) } : {}),
    ...(value.right ? { right: binding(value.right) } : {}),
    ...(value.conditions
      ? { conditions: value.conditions.map(condition) }
      : {}),
  });
  const positions = { ...definition.layout?.nodes };
  if (positions[key]) {
    positions[nextKey] = positions[key];
    delete positions[key];
  }
  return {
    ...definition,
    nodes: definition.nodes.map((node) => ({
      ...node,
      nodeKey: node.nodeKey === key ? nextKey : node.nodeKey,
      ...(node.inputs ? { inputs: map(node.inputs) } : {}),
      ...(node.condition ? { condition: condition(node.condition) } : {}),
      ...(node.http
        ? {
            http: {
              ...node.http,
              ...(node.http.body ? { body: binding(node.http.body) } : {}),
              ...(node.http.headers ? { headers: map(node.http.headers) } : {}),
            },
          }
        : {}),
    })),
    edges: definition.edges.map((edge) => ({
      ...edge,
      from: edge.from === key ? nextKey : edge.from,
      to: edge.to === key ? nextKey : edge.to,
    })),
    ...(definition.outputs ? { outputs: map(definition.outputs) } : {}),
    ...(definition.layout
      ? { layout: { ...definition.layout, nodes: positions } }
      : {}),
  };
}

export function graphDefinitionChanges(
  before: GraphDefinition,
  after: GraphDefinition,
) {
  const old = new Map(before.nodes.map((node) => [node.nodeKey, node]));
  return {
    added: after.nodes
      .filter((node) => !old.has(node.nodeKey))
      .map((node) => node.nodeKey),
    removed: before.nodes
      .filter(
        (node) => !after.nodes.some((item) => item.nodeKey === node.nodeKey),
      )
      .map((node) => node.nodeKey),
    changed: after.nodes
      .filter(
        (node) =>
          old.has(node.nodeKey) &&
          canonicalImportValue(old.get(node.nodeKey)) !==
            canonicalImportValue(node),
      )
      .map((node) => node.nodeKey),
    renamed: before.name !== after.name,
    connections:
      canonicalImportValue(before.edges) !== canonicalImportValue(after.edges),
    inputs:
      canonicalImportValue(before.inputSchema) !==
      canonicalImportValue(after.inputSchema),
  };
}

export function layoutGraph(definition: GraphDefinition): GraphDefinition {
  const depths = new Map<string, number>();
  const remaining = new Set(definition.nodes.map((node) => node.nodeKey));
  for (let pass = 0; pass < definition.nodes.length && remaining.size; pass++) {
    for (const key of remaining) {
      const parents = definition.edges
        .filter((edge) => edge.to === key)
        .map((edge) => edge.from);
      if (parents.every((parent) => depths.has(parent))) {
        depths.set(
          key,
          parents.length
            ? Math.max(...parents.map((parent) => depths.get(parent)!)) + 1
            : 0,
        );
        remaining.delete(key);
      }
    }
  }
  const rowCounts = new Map<number, number>();
  const nodes = Object.fromEntries(
    definition.nodes.map((node, index) => {
      const depth = depths.get(node.nodeKey) ?? index,
        row = rowCounts.get(depth) ?? 0;
      rowCounts.set(depth, row + 1);
      return [node.nodeKey, { x: depth * 300, y: row * 160 }];
    }),
  );
  return { ...definition, layout: { nodes } };
}
