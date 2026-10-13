import type { GraphDefinition, GraphInputBinding, GraphNodeDefinition, JsonObject } from "./types";
import { GraphDefinitionSchema } from "./schemas";

export function createSerialDefinition(
  name: string,
  steps: GraphNodeDefinition[],
  approverId: string,
  inputSchema: JsonObject = { type: "object", additionalProperties: true },
  controlKeys: { start: string; approval: string; end: string } = { start: "start", approval: "approval", end: "end" },
): GraphDefinition {
  const nodes: GraphNodeDefinition[] = [
    { nodeKey: controlKeys.start, type: "start" },
    ...steps,
    { nodeKey: controlKeys.approval, type: "approval", approverId },
    { nodeKey: controlKeys.end, type: "end" },
  ];
  return {
    schemaVersion: 1, name, inputSchema, nodes,
    edges: nodes.slice(1).map((node, index) => ({ from: nodes[index]!.nodeKey, to: node.nodeKey })),
  };
}

export function bindingToWire(binding: GraphInputBinding): Record<string, unknown> {
  return {
    source: binding.source,
    ...(binding.nodeKey !== undefined ? { node_key: binding.nodeKey } : {}),
    ...(binding.path !== undefined ? { path: binding.path } : {}),
    ...(binding.value !== undefined ? { value: binding.value } : {}),
    ...(binding.fields ? { fields: Object.fromEntries(Object.entries(binding.fields).map(([key, value]) => [key, bindingToWire(value)])) } : {}),
  };
}

export function definitionToWire(definition: GraphDefinition) {
  return {
    schema_version: definition.schemaVersion,
    name: definition.name,
    input_schema: definition.inputSchema,
    nodes: definition.nodes.map((node) => ({
      node_key: node.nodeKey, type: node.type,
      ...(node.agentId !== undefined ? { agent_id: node.agentId } : {}),
      ...(node.instructions !== undefined ? { instructions: node.instructions } : {}),
      ...(node.inputs !== undefined ? { inputs: Object.fromEntries(Object.entries(node.inputs).map(([key, value]) => [key, bindingToWire(value)])) } : {}),
      ...(node.outputSchema !== undefined ? { output_schema: node.outputSchema } : {}),
      ...(node.executionMode !== undefined ? { execution_mode: node.executionMode } : {}),
      ...(node.maxAttempts !== undefined ? { max_attempts: node.maxAttempts } : {}),
      ...(node.timeoutSeconds !== undefined ? { timeout_seconds: node.timeoutSeconds } : {}),
      ...(node.approverId !== undefined ? { approver_id: node.approverId } : {}),
    })),
    edges: definition.edges,
  };
}

/** Persisted node order is not execution order; follow the published edge chain. */
export function orderedAgentNodes(definition: GraphDefinition): GraphNodeDefinition[] {
  const result: GraphNodeDefinition[] = [];
  const visited = new Set<string>();
  let node = definition.nodes.find((candidate) => candidate.type === "start");
  while (node && !visited.has(node.nodeKey)) {
    visited.add(node.nodeKey);
    if (node.type === "agent") result.push(node);
    const nextKey = definition.edges.find((edge) => edge.from === node!.nodeKey)?.to;
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
  if (Array.isArray(value)) return `[${value.map(canonicalImportValue).join(",")}]`;
  if (value !== null && typeof value === "object") {
    return `{${Object.entries(value).sort(([a], [b]) => a.localeCompare(b)).map(([key, item]) => `${JSON.stringify(key)}:${canonicalImportValue(item)}`).join(",")}}`;
  }
  return JSON.stringify(value);
}

/** Validate the editor's lossless V1 subset. Saving/publishing also runs server validation. */
export function importGraphDefinition(text: string): GraphDefinition {
  if (new TextEncoder().encode(text).length > 256 * 1024) throw new Error("Definition exceeds the 256 KiB limit.");
  const raw: unknown = JSON.parse(text);
  const definition = GraphDefinitionSchema.parse(raw);
  if (definition.schemaVersion !== 1) throw new Error("Only Graph schema version 1 can be imported.");
  if (canonicalImportValue(raw) !== canonicalImportValue(JSON.parse(exportGraphDefinition(definition)))) {
    throw new Error("Definition contains unsupported fields; importing would discard data.");
  }
  if (!definition.name.trim() || new TextEncoder().encode(definition.name).length > 200) throw new Error("Name must contain 1 to 200 bytes.");
  const nodes = new Map(definition.nodes.map((node) => [node.nodeKey, node]));
  if (nodes.size !== definition.nodes.length || definition.nodes.some((node) => !/^[a-zA-Z][a-zA-Z0-9_-]{0,63}$/.test(node.nodeKey))) throw new Error("Node keys must be unique identifiers of at most 64 characters.");
  const starts = definition.nodes.filter((node) => node.type === "start");
  const approvals = definition.nodes.filter((node) => node.type === "approval");
  const ends = definition.nodes.filter((node) => node.type === "end");
  const agents = definition.nodes.filter((node) => node.type === "agent");
  if (starts.length !== 1 || approvals.length !== 1 || ends.length !== 1 || agents.length < 1 || agents.length > 16 || definition.nodes.length !== agents.length + 3) throw new Error("V1 requires start, 1–16 agent steps, approval and end.");
  if (definition.edges.length !== definition.nodes.length - 1) throw new Error("V1 requires a single serial edge chain.");
  const ordered: GraphNodeDefinition[] = [];
  const visited = new Set<string>();
  let node: GraphNodeDefinition | undefined = starts[0];
  while (node && !visited.has(node.nodeKey)) {
    visited.add(node.nodeKey);
    ordered.push(node);
    const edges = definition.edges.filter((edge) => edge.from === node!.nodeKey);
    if (edges.length > 1) throw new Error("Parallel edges are not supported in V1.");
    node = edges.length ? nodes.get(edges[0]!.to) : undefined;
  }
  if (node || ordered.length !== nodes.size || ordered.at(-1)?.type !== "end" || ordered.at(-2)?.type !== "approval" || ordered.slice(1, -2).some((item) => item.type !== "agent")) throw new Error("Edges must form start → agent steps → approval → end.");
  if (definition.inputSchema.type !== "object") throw new Error("Run input schema must have type object.");
  if (!approvals[0]!.approverId) throw new Error("Approval requires a member ID.");
  for (const control of definitionToWire(definition).nodes.filter((item) => item.type !== "agent")) {
    const allowed = control.type === "approval" ? ["node_key", "type", "approver_id"] : ["node_key", "type"];
    if (Object.keys(control).some((key) => !allowed.includes(key))) throw new Error(`Control node ${control.node_key} contains fields the editor cannot preserve.`);
  }
  for (const agent of agents) {
    if (!agent.agentId || !agent.instructions?.trim() || agent.outputSchema?.type !== "object" || !["none", "produce_commit", "verify_commit"].includes(agent.executionMode ?? "") || !agent.maxAttempts || agent.maxAttempts < 1 || agent.maxAttempts > 3 || !agent.timeoutSeconds || agent.timeoutSeconds < 1 || agent.timeoutSeconds > 86400) throw new Error(`Agent step ${agent.nodeKey} is incomplete or exceeds V1 limits.`);
  }
  return definition;
}
