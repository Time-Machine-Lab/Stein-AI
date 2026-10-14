import { isAgentRuntimeBound } from "@multica/core/agents";
import type { GraphBuilderDirectory, GraphDraftAi } from "@multica/core/graphs";
import { isRuntimeUsableForUser } from "@multica/core/runtimes";
import type { Agent, RuntimeDevice } from "@multica/core/types";

/**
 * Which teammates an AI-authored draft may use, and what the model is told
 * about them.
 *
 * Derived on every turn rather than stored once, because the metadata the model
 * needs — is the machine up, does this teammate have a model it can run — is
 * live. What IS stored is the *selection* (see `GraphDraftAi`): the version of
 * this directory that the conversation was started against, so a teammate
 * joining or leaving the workspace later cannot shift what an alias means
 * halfway through a conversation.
 */

/** Agents a draft could be handed to: live ones only. Archived is retired. */
export function graphBuilderCandidates(agents: Agent[]): Agent[] {
  return agents.filter((agent) => !agent.archived_at);
}

/**
 * Whether a node assigned to this teammate could actually be published.
 *
 * Mirrors the server's readiness verdict as far as the client can see it: a
 * runtime it is bound to, that this user may use, that is up, and a model to
 * run. It is advisory — the publish path re-checks everything — but it is what
 * the model is told, so that it prefers a teammate whose step will work.
 */
export function graphAgentReady(
  agent: Agent,
  runtimes: RuntimeDevice[],
  currentUserId: string | null,
): boolean {
  if (!agent.model.trim() || !isAgentRuntimeBound(agent)) return false;
  const runtime = runtimes.find((device) => device.id === agent.runtime_id);
  return (
    !!runtime &&
    runtime.status === "online" &&
    isRuntimeUsableForUser(runtime, currentUserId)
  );
}

/**
 * The directory for one conversation, in alias order: index 0 is
 * `agent_default`, so the executor the user chose has to come first.
 *
 * An agent in the selection that is no longer in the workspace is dropped
 * rather than passed through as an empty entry: the model cannot assign work to
 * someone who is gone, and a nameless alias would be a step the user cannot
 * publish. An agent the *draft* still uses is not lost by that — the protocol
 * appends actors the directory omits, so it survives as an unnamed out-of-scope
 * teammate.
 */
export function graphBuilderDirectory(
  selection: Pick<GraphDraftAi, "scopeIds" | "approverId">,
  agents: Agent[],
  runtimes: RuntimeDevice[],
  currentUserId: string | null,
): GraphBuilderDirectory {
  const byId = new Map(agents.map((agent) => [agent.id, agent]));
  const ordered = [
    ...(byId.has(selection.scopeIds[0] ?? "")
      ? [selection.scopeIds[0]!]
      : []),
    ...selection.scopeIds.slice(1).filter((id) => byId.has(id)),
  ];
  return {
    agents: ordered.map((id) => {
      const agent = byId.get(id)!;
      const runtime = runtimes.find((device) => device.id === agent.runtime_id);
      return {
        id: agent.id,
        name: agent.name,
        ...(agent.description ? { description: agent.description } : {}),
        inScope: true,
        graphReady: graphAgentReady(agent, runtimes, currentUserId),
        ...(runtime
          ? { runtime: { provider: runtime.provider, online: runtime.status === "online" } }
          : {}),
        ...(agent.skills.length
          ? { skills: agent.skills.map((skill) => skill.name) }
          : {}),
      };
    }),
    approverIds: [selection.approverId],
  };
}

/** The executor first, then the rest of the scope in the order it was chosen. */
export function graphScopeWithExecutor(
  executorId: string,
  scopeIds: string[],
): string[] {
  return [executorId, ...scopeIds.filter((id) => id !== executorId)];
}
