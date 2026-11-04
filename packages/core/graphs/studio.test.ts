import { describe, expect, it } from "vitest";
import {
  graphChangeMarks,
  graphRunProgress,
  groupLatestRuns,
  insertGraphNodeOnEdge,
  nextGraphNodeKey,
} from "./studio";
import type { GraphDefinition, GraphNodeRun, GraphRun } from "./types";

const base: GraphDefinition = {
  schemaVersion: 2,
  name: "Flow",
  inputSchema: { type: "object" },
  nodes: [
    { nodeKey: "start", type: "start" },
    { nodeKey: "check", type: "branch" },
    { nodeKey: "write", type: "agent" },
    { nodeKey: "end", type: "end" },
  ],
  edges: [
    { from: "start", to: "check" },
    { from: "check", to: "write", port: "true" },
    { from: "check", to: "end", port: "false" },
    { from: "write", to: "end" },
  ],
  layout: {
    nodes: {
      start: { x: 0, y: 0 },
      check: { x: 300, y: 0 },
      write: { x: 600, y: 0 },
      end: { x: 900, y: 0 },
    },
  },
};

describe("insertGraphNodeOnEdge", () => {
  it("splits a plain connection and places the step between its ends", () => {
    const next = insertGraphNodeOnEdge(
      base,
      { from: "start", to: "check" },
      { nodeKey: "agent_1", type: "agent" },
    );
    expect(next.edges.slice(0, 2)).toEqual([
      { from: "start", to: "agent_1" },
      { from: "agent_1", to: "check" },
    ]);
    expect(next.edges).toHaveLength(5);
    expect(next.layout?.nodes.agent_1).toEqual({ x: 150, y: 80 });
  });

  it("keeps a branch outcome on the incoming side", () => {
    const next = insertGraphNodeOnEdge(
      base,
      { from: "check", to: "write", port: "true" },
      { nodeKey: "review", type: "approval" },
    );
    expect(next.edges).toContainEqual({
      from: "check",
      to: "review",
      port: "true",
    });
    expect(next.edges).toContainEqual({ from: "review", to: "write" });
  });

  it("continues an inserted branch on its true outcome", () => {
    const next = insertGraphNodeOnEdge(
      base,
      { from: "write", to: "end" },
      { nodeKey: "branch_2", type: "branch" },
    );
    expect(next.edges).toContainEqual({
      from: "branch_2",
      to: "end",
      port: "true",
    });
  });

  it("refuses missing connections, duplicate keys and structural steps", () => {
    expect(() =>
      insertGraphNodeOnEdge(
        base,
        { from: "start", to: "end" },
        { nodeKey: "x", type: "agent" },
      ),
    ).toThrow();
    expect(() =>
      insertGraphNodeOnEdge(
        base,
        { from: "start", to: "check" },
        { nodeKey: "write", type: "agent" },
      ),
    ).toThrow();
    expect(() =>
      insertGraphNodeOnEdge(
        base,
        { from: "start", to: "check" },
        { nodeKey: "join_1", type: "join" },
      ),
    ).toThrow();
  });
});

describe("nextGraphNodeKey", () => {
  it("skips keys already taken", () => {
    const definition = {
      ...base,
      nodes: [...base.nodes, { nodeKey: "agent_2", type: "start" }],
    };
    expect(nextGraphNodeKey(definition, "agent")).toEqual({
      key: "agent_3",
      index: 3,
    });
  });
});

function nodeRun(
  nodeKey: string,
  nodeType: string,
  state: string,
  scopeId = "root",
): GraphNodeRun {
  return {
    id: nodeKey,
    nodeKey,
    nodeType,
    state,
    scopeId,
    input: {},
    inputHash: "",
    maxAttempts: 1,
    grantedAttempts: 0,
    canRetry: false,
    canResolveBlock: false,
    attempts: [],
    submissions: [],
  };
}

describe("graphRunProgress", () => {
  it("counts root working steps only", () => {
    expect(
      graphRunProgress({
        scopes: [{ id: "root" }, { id: "loop", parent_node_run_id: "x" }],
        nodes: [
          nodeRun("start", "start", "succeeded"),
          nodeRun("a", "agent", "succeeded"),
          nodeRun("b", "branch", "skipped"),
          nodeRun("c", "agent", "failed"),
          nodeRun("d", "approval", "waiting_approval"),
          nodeRun("inner", "agent", "succeeded", "loop"),
          nodeRun("end", "end", "pending"),
        ],
      }),
    ).toEqual({ done: 2, total: 4, failed: 1 });
  });
});

describe("groupLatestRuns", () => {
  it("groups newest first and caps each graph", () => {
    const run = (id: string, graphId: string | undefined, createdAt: string) =>
      ({ id, graphId, createdAt }) as GraphRun;
    const grouped = groupLatestRuns(
      [
        run("1", "g1", "2026-01-01T00:00:00Z"),
        run("2", "g1", "2026-01-03T00:00:00Z"),
        run("3", "g1", "2026-01-02T00:00:00Z"),
        run("4", "g2", "2026-01-01T00:00:00Z"),
        run("5", undefined, "2026-01-05T00:00:00Z"),
      ],
      2,
    );
    expect(grouped.get("g1")?.map((item) => item.id)).toEqual(["2", "3"]);
    expect(grouped.get("g2")?.map((item) => item.id)).toEqual(["4"]);
    expect(grouped.size).toBe(2);
  });
});

describe("graphChangeMarks", () => {
  it("lets an added mark win over a changed one", () => {
    expect(graphChangeMarks(["a"], ["a", "b"])).toEqual({
      a: "added",
      b: "changed",
    });
  });
});
