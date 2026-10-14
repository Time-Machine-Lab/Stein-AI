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
        runtimeEnabled: false,
      });
    },
  );

  it("reads configured availability and separates its cache between workspaces", async () => {
    respond({ enabled: true });
    expect(await new ApiClient("").getGraphGenerationOptions()).toEqual({
      enabled: true,
      runtimeEnabled: false,
    });
    expect(graphGenerationOptions("a").queryKey).not.toEqual(
      graphGenerationOptions("b").queryKey,
    );
    expect(graphGenerationOptions("").enabled).toBe(false);
  });

  it.each([
    [{ runtime_enabled: true }, { enabled: false, runtimeEnabled: true }],
    [
      { enabled: true, runtime_enabled: true },
      { enabled: true, runtimeEnabled: true },
    ],
    [{ enabled: "true" }, { enabled: false, runtimeEnabled: false }],
  ])("folds the two availability signals of %j", async (body, expected) => {
    respond(body);
    expect(await new ApiClient("").getGraphGenerationOptions()).toEqual(expected);
  });
});

describe("Graph runtime authoring API", () => {
  const view = {
    id: "generation-1",
    status: "queued",
    agent_id: "agent-1",
    submissions: 0,
    created_at: "2026-10-14T09:00:00Z",
    updated_at: "2026-10-14T09:00:00Z",
  };

  it("queues a scoped authoring request and parses the queued view", async () => {
    const fetcher = respond(view);
    const created = await new ApiClient("").createGraphGeneration({
      prompt: "Draft a release workflow",
      defaultAgentId: "agent-1",
      approverId: "member-1",
    });
    expect(fetcher.mock.calls[0]![0]).toBe("/api/graphs/generations");
    expect(fetcher.mock.calls[0]![1].method).toBe("POST");
    expect(JSON.parse(fetcher.mock.calls[0]![1].body)).toEqual({
      prompt: "Draft a release workflow",
      default_agent_id: "agent-1",
      approver_id: "member-1",
    });
    expect(created).toEqual({
      id: "generation-1",
      status: "queued",
      agentId: "agent-1",
      definition: undefined,
      summary: undefined,
      error: undefined,
      submissions: 0,
      createdAt: "2026-10-14T09:00:00Z",
      updatedAt: "2026-10-14T09:00:00Z",
    });
  });

  it("sends the scope, the editable graph, and the conversation when they are present", async () => {
    const fetcher = respond(view);
    const messages = [
      { role: "user" as const, content: "Add a review step" },
      { role: "assistant" as const, content: "Added one" },
    ];
    await new ApiClient("").createGraphGeneration({
      prompt: "Refine",
      defaultAgentId: "agent-1",
      agentScope: ["agent-1", "agent-2"],
      approverId: "member-1",
      currentDefinition: definition,
      messages,
    });
    expect(JSON.parse(fetcher.mock.calls[0]![1].body)).toEqual({
      prompt: "Refine",
      default_agent_id: "agent-1",
      agent_scope: ["agent-1", "agent-2"],
      approver_id: "member-1",
      current_definition: definitionToWire(definition),
      messages,
    });
  });

  it.each([
    {},
    { status: "queued" },
    { id: 7, status: "queued" },
    null,
    "queued",
  ])("refuses to report a queued generation for malformed body %j", async (body) => {
    respond(body);
    await expect(
      new ApiClient("").createGraphGeneration({
        prompt: "Draft",
        defaultAgentId: "agent-1",
        approverId: "member-1",
      }),
    ).rejects.toThrow("Invalid Graph generation response");
  });

  it("reads a generation, keeping an unrecognized status generic instead of failing the poll", async () => {
    respond({ ...view, status: "paused", error: undefined });
    const fetched = await new ApiClient("").getGraphGeneration("generation-1");
    expect(fetched?.status).toBe("failed");
    expect(fetched?.error).toBeUndefined();
  });

  it("keeps a succeeded generation's draft and summary", async () => {
    respond({
      ...view,
      status: "succeeded",
      definition: definitionToWire(definition),
      summary: "Added a review step",
    });
    const fetched = await new ApiClient("").getGraphGeneration("generation-1");
    expect(fetched?.definition).toEqual(definition);
    expect(fetched?.summary).toBe("Added a review step");
  });

  it("returns null instead of throwing when a generation cannot be read", async () => {
    respond({ nonsense: true });
    expect(
      await new ApiClient("").getGraphGeneration("generation-1"),
    ).toBeNull();
  });

  it("cancels the addressed generation", async () => {
    const fetcher = respond({ ...view, status: "cancelled" });
    const cancelled = await new ApiClient("").cancelGraphGeneration(
      "generation-1",
    );
    expect(fetcher.mock.calls[0]![0]).toBe(
      "/api/graphs/generations/generation-1/cancel",
    );
    expect(fetcher.mock.calls[0]![1].method).toBe("POST");
    expect(cancelled.status).toBe("cancelled");
  });

  it("refuses a cancel response that describes a different generation", async () => {
    respond({ ...view, id: "generation-2", status: "cancelled" });
    await expect(
      new ApiClient("").cancelGraphGeneration("generation-1"),
    ).rejects.toThrow("Invalid Graph generation response");
  });
});
