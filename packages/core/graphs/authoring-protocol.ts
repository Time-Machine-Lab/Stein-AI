import { escapeJsonStringControlCharacters } from "../model-json";
import { definitionToWire } from "./definition";
import { GraphDefinitionSchema } from "./schemas";
import { graphUtf8Length } from "./text";
import type { GraphDefinition, GraphNodeDefinition } from "./types";

/**
 * Wire format between the Graph editor's AI mode and the hidden graph builder
 * agent — the graph counterpart of `agents/builder-protocol.ts`, with the same
 * two asymmetric directions:
 *
 *   - Outbound, the composer's plain text is wrapped in a JSON envelope that
 *     also carries the current draft and the teammates the draft may assign
 *     work to, so every turn re-states the full decision context instead of
 *     relying on the model's memory of the previous turn.
 *   - Inbound, the builder appends one `<graph_draft>` JSON block to its
 *     natural language reply; the block replaces the draft and is stripped
 *     before the message is rendered.
 *
 * Everything in the envelope is alias-only. A workspace identity never leaves
 * the client, and an alias the directory does not contain fails the merge
 * instead of being written into the draft, so `agent_default` cannot silently
 * become an agent the user never picked and a model that writes a raw UUID has
 * its reply rejected rather than published.
 */
export const GRAPH_BUILDER_INPUT_PREFIX = "MULTICA_GRAPH_BUILDER_INPUT\n";

/** Mirrors the server's graph.MaxDefinitionBytes. */
export const GRAPH_BUILDER_MAX_DEFINITION_BYTES = 256 * 1024;

/** Teammates past this are reported as truncated. Mirrors the server's directory limit. */
const DIRECTORY_LIMIT = 40;
const SKILL_LIMIT = 12;
const NAME_RUNES = 120;
const DESCRIPTION_RUNES = 255;
const SKILL_RUNES = 64;

export interface GraphBuilderRuntime {
  provider: string;
  online: boolean;
}

/**
 * One teammate the draft may assign work to. It carries what is needed to pick
 * an executor and nothing that identifies or configures one: no owner,
 * instructions, environment or MCP configuration.
 */
export interface GraphBuilderAgent {
  /** Workspace agent ID. Resolved locally; never serialized into the request. */
  id: string;
  name: string;
  description?: string;
  /**
   * false for a teammate the draft may keep but the model should not assign new
   * work to: someone the user left out of scope, or an agent only the existing
   * draft uses.
   */
  inScope: boolean;
  /**
   * false when the teammate has no model or no compatible runtime, so a node
   * assigned to it cannot be published until that is configured.
   */
  graphReady: boolean;
  runtime?: GraphBuilderRuntime;
  skills?: string[];
}

export interface GraphBuilderDirectory {
  /**
   * The teammates the draft may use. The FIRST entry is the default executor
   * and reads as `agent_default`; the rest become `agent_1`, `agent_2`, ... in
   * order. An agent the current draft uses but this list omits still gets an
   * alias (see `definitionToAliases`), so it survives a round trip.
   */
  agents: GraphBuilderAgent[];
  /**
   * Workspace member IDs the draft may approve with. The first is the chosen
   * approver and reads as `approver_default`; the rest become `approver_1`, ...
   */
  approverIds: string[];
  /** The caller already cut the list at DIRECTORY_LIMIT. */
  agentsTruncated?: boolean;
}

export interface GraphDraftPayload {
  summary?: unknown;
  definition?: unknown;
}

export type GraphDraftMergeFailure =
  | "unparsable"
  | "invalid"
  | "unknown_alias"
  | "too_large";

export type GraphDraftMerge =
  | { ok: true; definition: GraphDefinition; summary?: string }
  | {
      ok: false;
      reason: GraphDraftMergeFailure;
      /** The alias that is not in the directory, for `unknown_alias`. */
      alias?: string;
      /** The aliases a reply may use, for `unknown_alias`. */
      availableAliases?: string[];
      /** The first failing field path, for `invalid`. Diagnostic, not localized. */
      detail?: string;
    };

/**
 * Reads the draft block out of a reply. `null` covers both "this turn asked a
 * question and proposed nothing" and "the block is not valid JSON"; callers
 * that need to tell those apart ask `hasGraphDraftBlock` as well.
 *
 * Never call this on a reply that is still being streamed: half a block is not
 * a smaller draft, it is an unparsable one.
 */
export function parseGraphDraft(content: string): GraphDraftPayload | null {
  const match = content.match(/<graph_draft>([\s\S]*?)<\/graph_draft>/);
  if (!match?.[1]) return null;
  return parseBlock(match[1]);
}

/**
 * True when a reply opens a draft block, whether or not it closes one. A stream
 * produces exactly that open end, so on its own it is not evidence of a
 * malformed block.
 */
export function hasGraphDraftBlock(content: string): boolean {
  return content.includes("<graph_draft>");
}

/**
 * Removes the block from a reply before a human reads it. The block is
 * machinery: the draft it carries is already on the canvas beside the
 * transcript, so the transcript keeps only the prose.
 *
 * Two patterns, not one. The closed form is what history holds. The unclosed
 * one is what streaming produces: for the seconds it takes the model to emit
 * the JSON there is an opening tag and no closing tag, which the closed-form
 * pattern cannot match, and the raw payload used to scroll past the reader on
 * every single turn.
 *
 * The leading `\s*` takes the whitespace the model left between its prose and
 * the block, so removing it does not strand a blank line.
 */
export function stripGraphDraft(content: string): string {
  return content
    .replace(/\s*<graph_draft>[\s\S]*?<\/graph_draft>/g, "")
    .replace(/\s*<graph_draft>[\s\S]*$/, "")
    .trim();
}

export function encodeGraphBuilderInput(
  request: string,
  definition: GraphDefinition,
  directory: GraphBuilderDirectory,
): string {
  const envelope = buildAliases(definition, directory);
  return (
    GRAPH_BUILDER_INPUT_PREFIX +
    JSON.stringify(
      {
        user_request: request,
        current_definition: applyAliases(definition, envelope.aliases),
        agents: wireAgents(envelope.entries),
        ...(envelope.agentsTruncated ? { agents_truncated: true } : {}),
        approvers: [...envelope.aliases.byAlias.approvers.keys()].sort(),
      },
      null,
      2,
    )
  );
}

export function decodeGraphBuilderInput(content: string): string {
  if (!content.startsWith(GRAPH_BUILDER_INPUT_PREFIX)) return content;
  try {
    const parsed = JSON.parse(
      content.slice(GRAPH_BUILDER_INPUT_PREFIX.length),
    ) as { user_request?: unknown };
    return typeof parsed.user_request === "string"
      ? parsed.user_request
      : content;
  } catch {
    return content;
  }
}

export interface GraphBuilderRestore {
  id: string;
  content: string;
}

/**
 * Chooses which cancelled prompt the graph composer should adopt.
 *
 * Two sources, never both: cancelling a task the daemon never started answers
 * synchronously (`cancelled_chat_message.restore_to_input`), while cancelling a
 * started-but-empty one defers the judgment and delivers the prompt later as a
 * durable chat_draft_restore row. The durable copy is the raw chat_message
 * content, i.e. still in the encoded wire form, so it is decoded here exactly as
 * the synchronous path decodes its own.
 */
export function pickGraphBuilderRestore(
  synchronous: GraphBuilderRestore | null,
  durable: { id: string; content: string } | null,
): GraphBuilderRestore | null {
  if (synchronous) return synchronous;
  if (!durable) return null;
  return { id: durable.id, content: decodeGraphBuilderInput(durable.content) };
}

/**
 * The definition as the model reads it: every actor replaced by its alias, in
 * the wire (snake_case) shape the model writes back. Wire rather than camelCase
 * because the two directions have to be each other's inverse —
 * `mergeGraphDraft(current, { definition: definitionToAliases(current, directory) },
 * directory)` restores `current` exactly, which is what makes a reply that
 * echoes the draft unchanged a no-op instead of a small data loss.
 *
 * Actors the directory omits are appended to the alias table rather than left
 * as IDs. A draft can reference a teammate the user has since dropped from the
 * scope, or an agent that no longer exists, and an envelope has no way to
 * describe that other than as an unnamed teammate the draft already uses.
 */
export function definitionToAliases(
  definition: GraphDefinition,
  directory: GraphBuilderDirectory,
): Record<string, unknown> {
  const { aliases } = buildAliases(definition, directory);
  return applyAliases(definition, aliases);
}

/**
 * Applies a reply's draft to the current one.
 *
 * A reply with no block is not a failure — the builder is allowed to answer a
 * question — so the caller filters those out before calling. What can fail here
 * is a block that is present and unusable, and no failure merges anything: the
 * canvas keeps the last good definition and the caller reports why. Aliases are
 * resolved against the draft as it is now, so a reply that arrives after a
 * manual edit is applied to that edit rather than to the draft it answered.
 */
export function mergeGraphDraft(
  current: GraphDefinition,
  payload: GraphDraftPayload,
  directory: GraphBuilderDirectory,
): GraphDraftMerge {
  const definition = payload?.definition;
  if (
    definition === null ||
    typeof definition !== "object" ||
    Array.isArray(definition)
  )
    return { ok: false, reason: "unparsable" };

  const parsed = GraphDefinitionSchema.safeParse(definition);
  if (!parsed.success) {
    const issue = parsed.error.issues[0];
    return {
      ok: false,
      reason: "invalid",
      detail: issue
        ? `${issue.path.join(".") || "(root)"}: ${issue.message}`
        : undefined,
    };
  }

  const { aliases } = buildAliases(current, directory);
  const resolved = resolveAliases(parsed.data, aliases);
  if (!resolved.ok)
    return {
      ok: false,
      reason: "unknown_alias",
      alias: resolved.alias,
      availableAliases: [
        ...aliases.byAlias.agents.keys(),
        ...aliases.byAlias.approvers.keys(),
      ].sort(),
    };

  if (
    graphUtf8Length(JSON.stringify(definitionToWire(resolved.definition))) >
    GRAPH_BUILDER_MAX_DEFINITION_BYTES
  )
    return { ok: false, reason: "too_large" };

  const summary =
    typeof payload.summary === "string" ? payload.summary.trim() : "";
  return {
    ok: true,
    definition: resolved.definition,
    ...(summary ? { summary } : {}),
  };
}

interface GraphBuilderActorMap {
  agents: Map<string, string>;
  approvers: Map<string, string>;
}

interface GraphBuilderAliases {
  /** alias -> workspace ID: how a reply's aliases are resolved. */
  byAlias: GraphBuilderActorMap;
  /** workspace ID -> alias: how the envelope is written. */
  byId: GraphBuilderActorMap;
}

interface GraphBuilderEnvelope {
  aliases: GraphBuilderAliases;
  /** The directory as the model reads it: aliased, bounded, plus discovered actors. */
  entries: { alias: string; agent: GraphBuilderAgent }[];
  agentsTruncated: boolean;
}

/**
 * Assigns every actor of `definition` an alias: the directory first, then
 * anything the draft uses that the directory omitted. Both directions rebuild
 * this from the same pair, so an alias means the same thing to the reply that
 * wrote it and to the merge that applies it.
 *
 * Aliases are positional — `agent_default` for the first teammate the directory
 * offers, then `agent_1`, `agent_2`, ... — so they only mean anything next to
 * the directory they were built from, which is why each turn restates it.
 */
function buildAliases(
  definition: GraphDefinition,
  directory: GraphBuilderDirectory,
): GraphBuilderEnvelope {
  const byAlias: GraphBuilderActorMap = {
    agents: new Map(),
    approvers: new Map(),
  };
  const byId: GraphBuilderActorMap = { agents: new Map(), approvers: new Map() };
  const entries: { alias: string; agent: GraphBuilderAgent }[] = [];

  const addAgent = (agent: GraphBuilderAgent) => {
    const alias =
      byAlias.agents.size === 0
        ? "agent_default"
        : `agent_${byAlias.agents.size}`;
    byAlias.agents.set(alias, agent.id);
    byId.agents.set(agent.id, alias);
    entries.push({ alias, agent });
  };
  const addApprover = (id: string) => {
    const alias =
      byAlias.approvers.size === 0
        ? "approver_default"
        : `approver_${byAlias.approvers.size}`;
    byAlias.approvers.set(alias, id);
    byId.approvers.set(id, alias);
  };

  for (const agent of directory.agents.slice(0, DIRECTORY_LIMIT)) {
    if (!byId.agents.has(agent.id)) addAgent(agent);
  }
  for (const id of directory.approverIds) {
    if (!byId.approvers.has(id)) addApprover(id);
  }

  // An actor the directory does not offer still gets an alias: the draft's own
  // teammates are not the model's to drop, and numbering continues past the
  // directory so a discovered actor never takes an alias already handed out.
  visitNodes(definition, (node) => {
    if (node.agentId && !byId.agents.has(node.agentId))
      addAgent({
        id: node.agentId,
        name: "",
        inScope: false,
        graphReady: false,
      });
    for (const id of approverIdsOf(node))
      if (!byId.approvers.has(id)) addApprover(id);
  });

  return {
    aliases: { byAlias, byId },
    entries,
    agentsTruncated:
      directory.agentsTruncated === true ||
      directory.agents.length > DIRECTORY_LIMIT,
  };
}

function applyAliases(
  definition: GraphDefinition,
  aliases: GraphBuilderAliases,
): Record<string, unknown> {
  // Every actor carries an alias: `buildAliases` walked this same definition to
  // build the table it is read from here.
  return definitionToWire(
    mapActors(definition, {
      agent: (id) => aliases.byId.agents.get(id)!,
      approver: (id) => aliases.byId.approvers.get(id)!,
    }),
  );
}

/**
 * Resolves aliases back to workspace IDs, reporting the first alias the
 * directory does not offer. The partially resolved definition is discarded by
 * the caller: a reply either applies whole or not at all, because a draft that
 * half-applies is a graph the user never asked for.
 */
function resolveAliases(
  definition: GraphDefinition,
  aliases: GraphBuilderAliases,
): { ok: true; definition: GraphDefinition } | { ok: false; alias: string } {
  const unknown = { alias: "" };
  const resolve =
    (table: Map<string, string>) =>
    (alias: string): string => {
      const id = table.get(alias);
      if (id === undefined) {
        if (!unknown.alias) unknown.alias = alias;
        return alias;
      }
      return id;
    };
  const resolved = mapActors(definition, {
    agent: resolve(aliases.byAlias.agents),
    approver: resolve(aliases.byAlias.approvers),
  });
  return unknown.alias
    ? { ok: false, alias: unknown.alias }
    : { ok: true, definition: resolved };
}

/**
 * Rewrites every actor of a definition at any nesting depth. Loop bodies and
 * subgraphs carry their own nodes with their own actors, and one that assigns
 * work inside a loop leaks exactly as much as a top-level node.
 */
function mapActors(
  definition: GraphDefinition,
  actor: {
    agent: (value: string) => string;
    approver: (value: string) => string;
  },
): GraphDefinition {
  return {
    ...definition,
    nodes: definition.nodes.map((node) => ({
      ...node,
      ...(node.agentId ? { agentId: actor.agent(node.agentId) } : {}),
      ...(node.approverId
        ? { approverId: actor.approver(node.approverId) }
        : {}),
      ...(node.approval
        ? {
            approval: {
              ...node.approval,
              approverIds: node.approval.approverIds.map(actor.approver),
            },
          }
        : {}),
      ...(node.loop
        ? { loop: { ...node.loop, body: mapActors(node.loop.body, actor) } }
        : {}),
      ...(node.subgraph
        ? {
            subgraph: {
              definition: mapActors(node.subgraph.definition, actor),
            },
          }
        : {}),
    })),
  };
}

function visitNodes(
  definition: GraphDefinition,
  visit: (node: GraphNodeDefinition) => void,
): void {
  for (const node of definition.nodes) {
    visit(node);
    if (node.loop) visitNodes(node.loop.body, visit);
    if (node.subgraph) visitNodes(node.subgraph.definition, visit);
  }
}

function approverIdsOf(node: GraphNodeDefinition): string[] {
  return [
    ...(node.approverId ? [node.approverId] : []),
    ...(node.approval?.approverIds ?? []),
  ];
}

function wireAgents(
  entries: { alias: string; agent: GraphBuilderAgent }[],
): Record<string, unknown>[] {
  return entries.map(({ alias, agent }) => ({
    alias,
    name: boundedText(agent.name, NAME_RUNES),
    ...(agent.description
      ? { description: boundedText(agent.description, DESCRIPTION_RUNES) }
      : {}),
    ...(alias === "agent_default" ? { default: true } : {}),
    in_scope: agent.inScope,
    graph_ready: agent.graphReady,
    ...(agent.runtime
      ? {
          runtime: {
            provider: agent.runtime.provider,
            online: agent.runtime.online,
          },
        }
      : {}),
    ...(agent.skills?.length
      ? {
          skills: agent.skills
            .slice(0, SKILL_LIMIT)
            .map((skill) => boundedText(skill, SKILL_RUNES)),
        }
      : {}),
  }));
}

/**
 * Keeps a user-written name, description or skill name to one bounded line so
 * it cannot reshape the envelope it is embedded in: a teammate's description
 * arrives in the same JSON object as the instructions the model follows.
 */
function boundedText(value: string, limit: number): string {
  const collapsed = [...value]
    .map((character) =>
      // A control character is not whitespace to `\s`, so it is neutralized
      // first; the escape it could carry has to be gone before the line is
      // collapsed into the surrounding JSON.
      (character.codePointAt(0) ?? 0) < 0x20 || character === "\u007f"
        ? " "
        : character,
    )
    .join("")
    .replace(/\s+/g, " ")
    .trim();
  const runes = [...collapsed];
  return runes.length > limit ? runes.slice(0, limit).join("") : collapsed;
}

function parseBlock(raw: string): GraphDraftPayload | null {
  const direct = parseJsonObject(raw);
  if (direct) return direct;
  // Some runtime-backed models emit literal line breaks inside string values
  // even when the contract asks for compact JSON. Repairing only the control
  // characters inside strings keeps every other syntax error an error.
  const repaired = escapeJsonStringControlCharacters(raw);
  return repaired === raw ? null : parseJsonObject(repaired);
}

function parseJsonObject(raw: string): GraphDraftPayload | null {
  try {
    const value: unknown = JSON.parse(raw);
    return value !== null && typeof value === "object" && !Array.isArray(value)
      ? (value as GraphDraftPayload)
      : null;
  } catch {
    return null;
  }
}
