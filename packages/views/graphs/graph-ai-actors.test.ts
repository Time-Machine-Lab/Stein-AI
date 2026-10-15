import { describe, expect, it } from "vitest";
import {
  createSerialDefinition,
  definitionToAliases,
  mergeGraphDraft,
} from "@multica/core/graphs";
import type { Agent, RuntimeDevice } from "@multica/core/types";
import { graphAgentReady, graphBuilderDirectory } from "./graph-ai-actors";

const agents = ["a", "b", "c"].map((id) => ({
  id,
  name: id,
  model: "claude",
  runtime_id: "runtime-1",
  runtime_bound: true,
  archived_at: null,
  skills: [],
})) as unknown as Agent[];
const runtimes = [{
  id: "runtime-1",
  status: "online",
  provider: "claude",
  owner_id: "user-1",
  visibility: "private",
}] as unknown as RuntimeDevice[];
const selection = { scopeIds: ["a", "b", "c"], approverId: "member-1" };
const definition = createSerialDefinition("Review", [{
  nodeKey: "review",
  type: "agent",
  agentId: "b",
  instructions: "Review",
  executionMode: "none",
  maxAttempts: 16,
  timeoutSeconds: 21600,
  outputSchema: { type: "object" },
}], "member-1");

describe("Graph authoring actor aliases", () => {
  it.each(["a", "b"])("preserves response aliases when agent %s disappears", (missing) => {
    const original = graphBuilderDirectory(selection, agents, runtimes, "user-1");
    const payload = { definition: definitionToAliases(definition, original) };
    const updated = graphBuilderDirectory(
      selection,
      agents.filter((agent) => agent.id !== missing),
      runtimes,
      "user-1",
    );
    expect(updated.agents.map((agent) => agent.id)).toEqual(selection.scopeIds);
    expect(updated.agents.find((agent) => agent.id === missing)).toMatchObject({
      inScope: false,
      graphReady: false,
    });
    expect(mergeGraphDraft(definition, payload, updated)).toMatchObject({
      ok: true,
      definition,
    });
  });

  it("marks archived agents unavailable without renumbering their aliases", () => {
    const archived = { ...agents[1]!, archived_at: "2026-10-15T00:00:00Z" };
    const directory = graphBuilderDirectory(selection, [agents[0]!, archived, agents[2]!], runtimes, "user-1");
    expect(directory.agents[1]).toMatchObject({ id: "b", inScope: false, graphReady: false });
    expect(graphAgentReady(archived, runtimes, "user-1")).toBe(false);
  });
});
