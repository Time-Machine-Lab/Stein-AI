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
  if (agent.archived_at || !agent.model.trim() || !isAgentRuntimeBound(agent))
    return false;
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
 * Keep unavailable teammates in their original slots so an in-flight reply
 * cannot resolve an alias to a different teammate. The model sees those slots
 * as out of scope and not ready for execution.
 */
export function graphBuilderDirectory(
  selection: Pick<GraphDraftAi, "scopeIds" | "approverId">,
  agents: Agent[],
  runtimes: RuntimeDevice[],
  currentUserId: string | null,
): GraphBuilderDirectory {
  const byId = new Map(agents.map((agent) => [agent.id, agent]));
  return {
    agents: selection.scopeIds.map((id) => {
      const agent = byId.get(id);
      if (!agent || agent.archived_at)
        return { id, name: "", inScope: false, graphReady: false };
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
