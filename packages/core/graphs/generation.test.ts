import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { createSerialDefinition, definitionToWire } from "./definition";
import { graphGenerationOptions } from "./queries";

afterEach(() => vi.unstubAllGlobals());

const definition = createSerialDefinition(
  "Research and summarize",
  [
    {
      nodeKey: "research",
      type: "agent",
      agentId: "agent-1",
      instructions: "Gather sources",
      executionMode: "none",
      maxAttempts: 2,
      timeoutSeconds: 3600,
      inputs: { context: { source: "run_input", path: "" } },
      outputSchema: { type: "object" },
    },
  ],
  "member-1",
);

function respond(body: unknown) {
  const fetcher = vi
    .fn()
    .mockResolvedValue(new Response(JSON.stringify(body), { status: 200 }));
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}

describe("Graph generation API", () => {
  it("sends a prompt and selected workspace identities and returns a parsed local definition", async () => {
    const fetcher = respond({ definition: definitionToWire(definition) });
    const result = await new ApiClient("").generateGraph({
      prompt: "Research the topic",
      agentId: "agent-1",
      approverId: "member-1",
    });
    expect(fetcher).toHaveBeenCalledOnce();
    expect(fetcher.mock.calls[0]![0]).toBe("/api/graphs/generate");
    expect(JSON.parse(fetcher.mock.calls[0]![1].body)).toEqual({
      prompt: "Research the topic",
      agent_id: "agent-1",
      approver_id: "member-1",
    });
    expect(result.definition).toEqual(definition);
  });

  it.each([{}, { definition: null }, { definition: { schema_version: 1 } }])(
    "rejects malformed generation without pretending a draft exists: %j",
    async (body) => {
      respond(body);
      await expect(
        new ApiClient("").generateGraph({
          prompt: "Research",
          agentId: "agent-1",
          approverId: "member-1",
        }),
      ).rejects.toThrow("Invalid Graph generation response");
    },
  );

  it("rejects an unsupported generated workflow instead of silently dropping its steps", async () => {
    respond({
      definition: { ...definitionToWire(definition), schema_version: 3 },
    });
    await expect(
      new ApiClient("").generateGraph({
        prompt: "Research",
        agentId: "agent-1",
        approverId: "member-1",
      }),
    ).rejects.toThrow();
  });

  it("rejects unknown generated behavior rather than dropping it during parsing", async () => {
    respond({
      definition: {
        ...definitionToWire(definition),
        condition: "only_on_failure",
      },
    });
    await expect(
      new ApiClient("").generateGraph({
        prompt: "Research",
        agentId: "agent-1",
        approverId: "member-1",
      }),
    ).rejects.toThrow("Invalid Graph generation response");
  });
  it("sends the editable graph and conversation when requesting a proposal", async () => {
    const fetcher = respond({
      definition: definitionToWire(definition),
      summary: "Added a review step",
    });
    const messages = [{ role: "user" as const, content: "Add a review" }];
    const result = await new ApiClient("").generateGraph({
      prompt: "Refine",
      agentId: "agent-1",
      approverId: "member-1",
      currentDefinition: definition,
      messages,
    });
    expect(JSON.parse(fetcher.mock.calls[0]![1].body)).toMatchObject({
      current_definition: definitionToWire(definition),
      messages,
    });
    expect(result.summary).toBe("Added a review step");
  });

  it.each([{}, { enabled: "true" }, null])(
    "does not enable generation on malformed options: %j",
    async (body) => {
      respond(body);
      expect(await new ApiClient("").getGraphGenerationOptions()).toEqual({
        enabled: false,
      });
    },
  );

  it("reads configured availability and separates its cache between workspaces", async () => {
    respond({ enabled: true });
    expect(await new ApiClient("").getGraphGenerationOptions()).toEqual({
      enabled: true,
    });
    expect(graphGenerationOptions("a").queryKey).not.toEqual(
      graphGenerationOptions("b").queryKey,
    );
    expect(graphGenerationOptions("").enabled).toBe(false);
  });
});
