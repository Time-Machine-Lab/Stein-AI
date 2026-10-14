import { describe, expect, it } from "vitest";
import {
  definitionToWire,
  importGraphDefinition,
  executionNodes,
  connectGraphNodes,
  renameGraphNode,
  graphDefinitionChanges,
} from "./definition";
import type { GraphDefinition } from "./types";
const definition: GraphDefinition = {
  schemaVersion: 2,
  name: "Review",
  inputSchema: { type: "object" },
  maxConcurrency: 4,
  nodes: [
    { nodeKey: "start", type: "start" },
    {
      nodeKey: "check",
      type: "branch",
      condition: {
        op: "eq",
        left: { source: "run_input", path: "/ready" },
        right: { source: "constant", value: true },
      },
    },
    { nodeKey: "yes", type: "delay", delay: { seconds: 1 } },
    { nodeKey: "no", type: "delay", delay: { seconds: 1 } },
    { nodeKey: "join", type: "join" },
    { nodeKey: "end", type: "end" },
  ],
  edges: [
    { from: "start", to: "check" },
    { from: "check", to: "yes", port: "true" },
    { from: "check", to: "no", port: "false" },
    { from: "yes", to: "join" },
    { from: "no", to: "join" },
    { from: "join", to: "end" },
  ],
  layout: {
    nodes: { start: { x: 30, y: 40 } },
    viewport: { x: 1, y: 2, zoom: 1 },
  },
};
describe("Graph V2 authoring", () => {
  it("preserves readable labels in nested definitions and rejects oversized UTF-8 labels", () => {
    const raw = definitionToWire(definition);
    raw.nodes[0]!.label = "开始研究";
    const parsed = importGraphDefinition(JSON.stringify(raw));
    expect(parsed.nodes[0]).toMatchObject({
      nodeKey: "start",
      label: "开始研究",
    });
    expect(definitionToWire(parsed).nodes[0]!.label).toBe("开始研究");
    raw.nodes[0]!.label = "研".repeat(67);
    expect(() => importGraphDefinition(JSON.stringify(raw))).toThrow();
  });
  it("round trips branch ports, conditions and layout losslessly", () => {
    expect(
      importGraphDefinition(JSON.stringify(definitionToWire(definition))),
    ).toEqual(definition);
  });
  it("preserves nested runtime paths and conditional input fallbacks", () => {
    const body: GraphDefinition = {
      ...definition,
      nodes: [
        {
          nodeKey: "work",
          type: "script",
          agentId: "a",
          script: { language: "python", code: "pass", timeoutSeconds: 10 },
          inputs: {
            maybe: {
              source: "node_output",
              nodeKey: "prior",
              path: "/value",
              default: 0,
              optional: true,
            },
          },
        },
      ],
      edges: [],
    };
    expect(
      executionNodes({
        ...definition,
        nodes: [
          {
            nodeKey: "repeat",
            type: "loop",
            loop: {
              body,
              maxIterations: 3,
              condition: {
                op: "eq",
                left: { source: "constant", value: false },
                right: { source: "constant", value: true },
              },
              next: {},
            },
          },
        ],
      }).map((item) => item.path),
    ).toEqual(["repeat/body/work"]);
    expect(definitionToWire(body).nodes[0]?.inputs?.maybe).toMatchObject({
      node_key: "prior",
      default: 0,
      optional: true,
    });
  });
  it("rejects connections which create cycles and replaces an occupied branch port", () => {
    expect(() =>
      connectGraphNodes(definition, { from: "join", to: "check" }),
    ).toThrow();
    const next = connectGraphNodes(definition, {
      from: "check",
      to: "join",
      port: "true",
    });
    expect(
      next.edges.filter(
        (edge) => edge.from === "check" && edge.port === "true",
      ),
    ).toEqual([{ from: "check", to: "join", port: "true" }]);
  });
  it("renames references, edges and positions atomically", () => {
    const next = renameGraphNode(
      {
        ...definition,
        outputs: {
          result: { source: "node_output", nodeKey: "yes", path: "/a" },
        },
      },
      "yes",
      "accepted",
    );
    expect(next.edges.some((edge) => edge.to === "accepted")).toBe(true);
    expect(next.outputs?.result?.nodeKey).toBe("accepted");
    expect(() => renameGraphNode(definition, "yes", "no")).toThrow();
  });
  it("describes AI node changes without treating layout-only edits as behavior changes", () => {
    const next = {
      ...definition,
      nodes: definition.nodes.filter((node) => node.nodeKey !== "no"),
      name: "Changed",
    };
    expect(graphDefinitionChanges(definition, next)).toMatchObject({
      removed: ["no"],
      added: [],
      changed: [],
      renamed: true,
    });
  });
});
