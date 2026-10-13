import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { graphIssueSearchOptions, graphKeys } from "./queries";
import {
  canApproveGraphRun,
  canDetachGraphRun,
  canReplaceGraphApprover,
  graphRunActions,
  isGraphRuntimeSupported,
  nodeCanRetry,
} from "./controls";
import {
  createSerialDefinition,
  definitionToWire,
  exportGraphDefinition,
  importGraphDefinition,
} from "./definition";
import { GraphRunSchema } from "./schemas";

afterEach(() => vi.unstubAllGlobals());

const runWire = {
  id: "run-1",
  workspace_id: "ws-1",
  root_issue_id: "issue-1",
  version_id: "version-1",
  initiator_id: "user-1",
  state: "active",
  control_state: "run",
  revision: 2,
  input: { user_name: "Ada" },
  input_hash: "hash",
  nodes: [],
  events: [],
};

function respond(body: unknown) {
  const fetcher = vi
    .fn()
    .mockResolvedValue(new Response(JSON.stringify(body), { status: 200 }));
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}

describe("Graph API boundary", () => {
  it("preserves business input keys while mapping protocol fields", async () => {
    respond(runWire);
    const result = await new ApiClient("").getGraphRun("run-1");
    expect(result?.rootIssueId).toBe("issue-1");
    expect(result?.input).toEqual({ user_name: "Ada" });
  });

  it("returns a safe unavailable detail for malformed identity or revision", async () => {
    respond({ ...runWire, revision: "2" });
    expect(await new ApiClient("").getGraphRun("run-1")).toBeNull();
  });

  it("keeps unknown states visible with no enabled controls", async () => {
    respond({ ...runWire, state: "future_state" });
    const result = await new ApiClient("").getGraphRun("run-1");
    expect(result?.state).toBe("future_state");
    expect(
      graphRunActions(result, { userId: "user-1", role: "member" }),
    ).toEqual([]);
  });

  it("does not report a successful control if its response is malformed", async () => {
    respond({ ok: true });
    await expect(
      new ApiClient("").controlGraphRun("run-1", "pause", {
        expectedRevision: 2,
        idempotencyKey: "operation-1",
      }),
    ).rejects.toThrow();
  });

  it("sends expected revision and idempotency with the command", async () => {
    const fetcher = respond(runWire);
    await new ApiClient("").controlGraphRun("run-1", "retry", {
      expectedRevision: 2,
      idempotencyKey: "operation-1",
      nodeRunId: "node-1",
    });
    expect(JSON.parse(fetcher.mock.calls[0]![1].body)).toEqual({
      expected_revision: 2,
      idempotency_key: "operation-1",
      node_run_id: "node-1",
    });
  });

  it("defaults a malformed list to an empty list", async () => {
    respond({ graphs: null });
    expect(await new ApiClient("").listGraphs()).toEqual([]);
  });
});

describe("Graph controls", () => {
  const actor = { userId: "user-1", role: "member" };
  it("requires the initiator or administrator", () => {
    const run = GraphRunSchema.parse(runWire);
    expect(graphRunActions(run, actor)).toContain("pause");
    expect(graphRunActions(run, { ...actor, userId: "other" })).toEqual([]);
  });
  it("offers resume while paused, and no resume once cancellation starts", () => {
    expect(
      graphRunActions(
        GraphRunSchema.parse({ ...runWire, control_state: "pause" }),
        actor,
      ),
    ).toEqual(["resume", "cancel"]);
    expect(
      graphRunActions(
        GraphRunSchema.parse({ ...runWire, control_state: "cancel_requested" }),
        actor,
      ),
    ).toEqual([]);
  });
  it("never retries an unknown execution or an exhausted budget", () => {
    const base = {
      id: "node-1",
      node_key: "build",
      node_type: "agent",
      state: "blocked",
      can_retry: true,
      max_attempts: 2,
      input: {},
      input_hash: "hash",
      attempts: [
        {
          task_id: "task-1",
          attempt_no: 1,
          generation: 1,
          state: "failed",
          manifest: {},
          receipt: {},
        },
      ],
    };
    const node = GraphRunSchema.parse({
      ...runWire,
      state: "blocked",
      nodes: [base],
    }).nodes[0]!;
    expect(nodeCanRetry(node)).toBe(true);
    expect(nodeCanRetry({ ...node, nodeType: "script" })).toBe(true);
    expect(nodeCanRetry({ ...node, blockReason: "execution_unknown" })).toBe(
      false,
    );
    expect(nodeCanRetry({ ...node, maxAttempts: 1 })).toBe(false);
    expect(
      nodeCanRetry({
        ...node,
        attempts: [{ ...node.attempts[0]!, state: "running" }],
      }),
    ).toBe(false);
  });
  it("only offers HTTP recovery for a confirmed stopped effect with remaining attempts", () => {
    const node = GraphRunSchema.parse({
      ...runWire,
      nodes: [
        {
          id: "http",
          node_key: "send",
          node_type: "http",
          state: "blocked",
          can_retry: true,
          effect: {
            state: "unknown",
            attempts: 1,
            invocation_id: "call",
            lease_until: "now",
            claim_token: "token",
          },
        },
      ],
    }).nodes[0]!;
    expect(nodeCanRetry(node)).toBe(true);
    expect(
      nodeCanRetry({
        ...node,
        effect: { ...node.effect!, state: "dispatched" },
      }),
    ).toBe(false);
    expect(
      nodeCanRetry({ ...node, effect: { ...node.effect!, attempts: 3 } }),
    ).toBe(false);
    expect(nodeCanRetry({ ...node, effect: undefined })).toBe(false);
  });
  it("isolates query keys between workspaces", () => {
    expect(graphKeys.run("a", "same")).not.toEqual(graphKeys.run("b", "same"));
  });

  it("requires server-confirmed detachment eligibility and a terminal run", () => {
    const terminal = GraphRunSchema.parse({
      ...runWire,
      state: "cancelled",
      can_detach: true,
    });
    expect(canDetachGraphRun(terminal, actor)).toBe(true);
    expect(canDetachGraphRun({ ...terminal, canDetach: false }, actor)).toBe(
      false,
    );
    expect(canDetachGraphRun({ ...terminal, state: "active" }, actor)).toBe(
      false,
    );
    expect(canDetachGraphRun(terminal, { userId: "other" })).toBe(false);
    expect(GraphRunSchema.parse(runWire).canDetach).toBe(false);
  });

  it("binds approval to the exact member and restricts approver replacement to administrators", () => {
    const run = GraphRunSchema.parse({
      ...runWire,
      state: "waiting_approval",
      approval: {
        id: "approval-1",
        approver_id: "member-1",
        candidate_digest: "digest",
        revision: 1,
        state: "pending",
      },
    });
    expect(canApproveGraphRun(run, { ...actor, memberId: "member-1" })).toBe(
      true,
    );
    expect(canApproveGraphRun(run, { ...actor, memberId: "other" })).toBe(
      false,
    );
    expect(
      canApproveGraphRun(
        { ...run, controlState: "pause" },
        { ...actor, memberId: "member-1" },
      ),
    ).toBe(false);
    expect(canReplaceGraphApprover(run, actor)).toBe(false);
    expect(canReplaceGraphApprover(run, { ...actor, role: "admin" })).toBe(
      true,
    );
  });

  it("offers only Codex runtimes advertising Graph execution support", () => {
    expect(
      isGraphRuntimeSupported({
        provider: "codex",
        metadata: { capabilities: ["graph-execution-v1"] },
      }),
    ).toBe(true);
    expect(isGraphRuntimeSupported({ provider: "codex", metadata: {} })).toBe(
      false,
    );
    expect(
      isGraphRuntimeSupported({
        provider: "claude",
        metadata: { capabilities: ["graph-execution-v1"] },
      }),
    ).toBe(false);
    expect(
      isGraphRuntimeSupported({
        provider: "claude",
        metadata: { capabilities: ["graph-execution-v2"] },
      }),
    ).toBe(true);
    expect(
      isGraphRuntimeSupported(
        {
          provider: "custom",
          metadata: { capabilities: ["graph-execution-v2"] },
        },
        true,
      ),
    ).toBe(true);
    expect(
      isGraphRuntimeSupported(
        {
          provider: "codex",
          metadata: { capabilities: ["graph-execution-v1"] },
        },
        true,
      ),
    ).toBe(false);
  });
});

describe("serial step editor model", () => {
  it("always builds explicit start, agent steps, approval and end with ordered edges", () => {
    const definition = createSerialDefinition(
      "Build",
      [
        {
          nodeKey: "implement",
          type: "agent",
          agentId: "agent-1",
          instructions: "Implement",
          executionMode: "none",
          maxAttempts: 2,
          timeoutSeconds: 3600,
          inputs: {},
          outputSchema: { type: "object" },
        },
      ],
      "member-1",
    );
    const wire = definitionToWire(definition);
    expect(wire.nodes.map((n) => n.node_key)).toEqual([
      "start",
      "implement",
      "approval",
      "end",
    ]);
    expect(wire.edges).toEqual([
      { from: "start", to: "implement" },
      { from: "implement", to: "approval" },
      { from: "approval", to: "end" },
    ]);
    expect(wire.nodes[1]).toMatchObject({
      max_attempts: 2,
      execution_mode: "none",
    });
  });

  it("round-trips a whole definition without renaming business keys or control nodes", () => {
    const definition = createSerialDefinition(
      "Build",
      [
        {
          nodeKey: "build_step",
          type: "agent",
          agentId: "agent-1",
          instructions: "Build",
          executionMode: "none",
          maxAttempts: 2,
          timeoutSeconds: 3600,
          inputs: {
            user_name: {
              source: "object",
              fields: {
                some_key: { source: "constant", value: { user_id: "Ada" } },
              },
            },
          },
          outputSchema: {
            type: "object",
            properties: { output_key: { type: "string" } },
          },
        },
      ],
      "member-1",
      { type: "object", properties: { input_key: { type: "string" } } },
      { start: "begin", approval: "review", end: "finish" },
    );
    const text = exportGraphDefinition(definition);
    const imported = importGraphDefinition(text);
    expect(exportGraphDefinition(imported)).toBe(text);
    expect(imported.nodes.map((node) => node.nodeKey)).toEqual([
      "begin",
      "build_step",
      "review",
      "finish",
    ]);
    expect(
      imported.nodes[1]!.inputs?.user_name?.fields?.some_key?.value,
    ).toEqual({ user_id: "Ada" });
  });

  it("rejects unknown fields, future versions, cycles and incomplete agent definitions on import", () => {
    const wire = definitionToWire(
      createSerialDefinition(
        "Build",
        [
          {
            nodeKey: "build",
            type: "agent",
            agentId: "agent-1",
            instructions: "Build",
            executionMode: "none",
            maxAttempts: 1,
            timeoutSeconds: 60,
            outputSchema: { type: "object" },
          },
        ],
        "member-1",
      ),
    );
    expect(() =>
      importGraphDefinition(JSON.stringify({ ...wire, schedule: {} })),
    ).toThrow("unsupported fields");
    expect(() =>
      importGraphDefinition(JSON.stringify({ ...wire, schema_version: 3 })),
    ).toThrow("versions 1 and 2");
    expect(() =>
      importGraphDefinition(
        JSON.stringify({
          ...wire,
          edges: [
            { from: "start", to: "build" },
            { from: "build", to: "start" },
            { from: "approval", to: "end" },
          ],
        }),
      ),
    ).toThrow("Edges must form");
    expect(() =>
      importGraphDefinition(
        JSON.stringify({
          ...wire,
          nodes: wire.nodes.map((node) =>
            node.type === "agent" ? { ...node, max_attempts: 4 } : node,
          ),
        }),
      ),
    ).toThrow("V1 limits");
  });
});

describe("graph issue search", () => {
  it("does not query the server until the user has typed something", () => {
    // /api/issues/search rejects an empty q with 400, and the start form mounts
    // with an empty search box, so an enabled empty query surfaces a bogus error.
    expect(graphIssueSearchOptions("ws", "").enabled).toBe(false);
    expect(graphIssueSearchOptions("ws", "   ").enabled).toBe(false);
    expect(graphIssueSearchOptions("ws", "login bug").enabled).toBe(true);
    expect(graphIssueSearchOptions("", "login bug").enabled).toBe(false);
  });
});
