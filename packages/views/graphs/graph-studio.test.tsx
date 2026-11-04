import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createSerialDefinition,
  exportGraphDefinition,
  graphDraftKey,
  useGraphDraftStore,
  type GraphDefinition,
  type GraphEdge,
  type GraphRecord,
} from "@multica/core/graphs";
import labels from "../locales/en/graphs.json";
import issueLabels from "../locales/en/issues.json";
import type { GraphAiTurn } from "./graph-studio-chat";
import { GraphDetailPage } from "./graph-detail-page";

const mocks = vi.hoisted(() => ({
  api: {
    saveGraph: vi.fn(),
    validateGraph: vi.fn(),
    publishGraph: vi.fn(),
    archiveGraph: vi.fn(),
    setChatSessionArchived: vi.fn(),
  },
  graph: null as unknown,
  pathname: "/acme/graphs/new",
  search: "",
  replace: vi.fn(),
  actor: { userId: "user-1", role: "admin", memberId: "member-1" },
  applyTurn: null as
    | ((definition: GraphDefinition, turn: GraphAiTurn) => void)
    | null,
}));

vi.mock("../i18n", () => ({
  useT: (namespace: string) => ({
    t: (
      selector: (value: unknown) => string,
      options?: Record<string, unknown>,
    ) => {
      let text = selector(namespace === "issues" ? issueLabels : labels);
      for (const [key, value] of Object.entries(options ?? {}))
        text = text.replace(`{{${key}}}`, String(value));
      return text;
    },
  }),
  useTimeAgo: () => () => "just now",
}));
vi.mock("../navigation", () => ({
  useNavigation: () => ({
    pathname: mocks.pathname,
    searchParams: new URLSearchParams(mocks.search),
    replace: mocks.replace,
    push: vi.fn(),
  }),
  AppLink: ({ href, children, ...props }: React.ComponentProps<"a">) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspaceSlug: () => "acme",
  useWorkspacePaths: () => ({
    graphs: () => "/acme/graphs",
    graphDetail: (id: string) => `/acme/graphs/${id}`,
  }),
}));
vi.mock("@multica/core/api", async (original) => ({
  ...(await original<object>()),
  api: mocks.api,
}));
vi.mock("@multica/core/graphs", async (original) => ({
  ...(await original<object>()),
  graphDetailOptions: () => ({
    queryKey: ["graph-detail"],
    queryFn: async () => mocks.graph,
    initialData: mocks.graph,
  }),
  graphSearchOptions: () => ({
    queryKey: ["graph-search"],
    queryFn: async () => ({ items: [], total: 0 }),
    initialData: { items: [], total: 0 },
  }),
  graphGenerationOptions: () => ({
    queryKey: ["generation-options"],
    queryFn: async () => ({ enabled: false, builderEnabled: true }),
    initialData: { enabled: false, builderEnabled: true },
  }),
}));
vi.mock("@multica/core/runtimes", () => ({
  runtimeListOptions: () => ({
    queryKey: ["runtimes"],
    queryFn: async () => [],
    initialData: [],
  }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({
    queryKey: ["agents"],
    queryFn: async () => [],
    initialData: [{ id: "agent-1", name: "Builder", model: "claude" }],
  }),
  memberListOptions: () => ({
    queryKey: ["members"],
    queryFn: async () => [],
    initialData: [{ id: "member-1", name: "Ada" }],
  }),
}));
vi.mock("./shared", async (original) => ({
  ...(await original<object>()),
  useGraphActor: () => mocks.actor,
}));
vi.mock("@multica/ui/hooks/use-mobile", () => ({ useIsCompact: () => false }));
vi.mock("react-resizable-panels", () => ({
  useDefaultLayout: () => ({ defaultLayout: undefined, onLayoutChanged: vi.fn() }),
}));
vi.mock("@multica/ui/components/ui/resizable", () => ({
  ResizablePanelGroup: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  ResizablePanel: ({ children }: { children: React.ReactNode }) => (
    <section>{children}</section>
  ),
  ResizableHandle: () => null,
}));
vi.mock("@xyflow/react", () => ({
  Panel: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));
// Pointer and drag behavior is covered in browser acceptance. The stand-in
// canvas exposes what the studio hands it: nodes, marks, issues, the insert
// callback, and selection.
vi.mock("./graph-canvas", () => ({
  GraphCanvas: ({
    definition,
    onSelect,
    onInsertOnEdge,
    marks,
    issues,
    children,
  }: {
    definition: GraphDefinition;
    onSelect?: (key: string) => void;
    onInsertOnEdge?: (edge: GraphEdge, type: string) => void;
    marks?: Record<string, string>;
    issues?: Record<string, number>;
    children?: React.ReactNode;
  }) => (
    <div aria-label="Canvas">
      {children}
      {definition.nodes.map((node) => (
        <button
          type="button"
          key={node.nodeKey}
          data-mark={marks?.[node.nodeKey] ?? ""}
          data-issues={issues?.[node.nodeKey] ?? 0}
          onClick={() => onSelect?.(node.nodeKey)}
        >{`Node ${node.nodeKey}`}</button>
      ))}
      {definition.edges[0] && onInsertOnEdge && (
        <button
          type="button"
          onClick={() => onInsertOnEdge(definition.edges[0]!, "approval")}
        >
          Insert on first edge
        </button>
      )}
    </div>
  ),
}));
vi.mock("./graph-node-catalog", async (original) => ({
  ...(await original<object>()),
  GraphAddNodeMenu: ({
    onPick,
    disabled,
  }: {
    onPick: (type: string) => void;
    disabled?: boolean;
  }) => (
    <div>
      {["agent", "loop", "approval"].map((type) => (
        <button
          key={type}
          type="button"
          disabled={disabled}
          onClick={() => onPick(type)}
        >{`Add ${type}`}</button>
      ))}
    </div>
  ),
}));
vi.mock("./graph-studio-chat", () => ({
  GraphStudioChat: ({
    onApplyTurn,
    prefill,
  }: {
    onApplyTurn: (definition: GraphDefinition, turn: GraphAiTurn) => void;
    prefill: { content: string } | null;
  }) => {
    mocks.applyTurn = onApplyTurn;
    return <div aria-label="Composer">{prefill?.content ?? ""}</div>;
  },
}));
vi.mock("./graph-run-list", () => ({
  GraphRunList: ({ graphId }: { graphId?: string }) => (
    <p>Runs of {graphId}</p>
  ),
}));
vi.mock("./graph-start-form", () => ({
  GraphStartForm: () => <p>Run settings</p>,
}));
vi.mock("./graph-triggers", () => ({
  GraphTriggers: () => <p>Trigger settings</p>,
}));
vi.mock("../editor/readonly-content", () => ({
  ReadonlyContent: ({ content }: { content: string }) => <span>{content}</span>,
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

const draftKey = graphDraftKey("ws-1", "user-1");
const storedGraphKey = graphDraftKey("ws-1", "user-1", "graph-1");

function work(nodeKey: string) {
  return {
    nodeKey,
    type: "agent",
    agentId: "agent-1",
    instructions: `Do ${nodeKey}`,
    executionMode: "none",
    maxAttempts: 2,
    timeoutSeconds: 60,
    inputs: {},
    outputSchema: { type: "object" },
  };
}
function record(definition: GraphDefinition): GraphRecord {
  return {
    id: "graph-1",
    workspaceId: "ws-1",
    name: definition.name,
    revision: 3,
    definition,
    createdBy: "user-1",
    createdAt: "",
    updatedAt: "",
    versions: [],
  };
}

function page(create = true) {
  mocks.pathname = create ? "/acme/graphs/new" : "/acme/graphs/graph-1";
  return render(
    <QueryClientProvider
      client={
        new QueryClient({
          defaultOptions: { queries: { staleTime: Infinity, retry: false } },
        })
      }
    >
      <GraphDetailPage create={create} />
    </QueryClientProvider>,
  );
}
function blank() {
  const result = page();
  fireEvent.click(screen.getByRole("button", { name: labels.studio.ux.blank }));
  return result;
}
const document = (key = draftKey) =>
  useGraphDraftStore.getState().documents[key]!;

beforeEach(() => {
  vi.clearAllMocks();
  window.localStorage.clear();
  useGraphDraftStore.setState({ documents: {} });
  mocks.graph = null;
  mocks.search = "";
  mocks.actor.role = "admin";
  mocks.api.saveGraph.mockResolvedValue({ id: "graph-1", revision: 4 });
});

describe("New graph entry", () => {
  it("starts blank into the studio and recovers the draft after a reload", () => {
    const first = blank();
    fireEvent.change(screen.getByLabelText(labels.editor.name), {
      target: { value: "Unpublished" },
    });
    first.unmount();
    useGraphDraftStore.setState({ documents: {} });
    // A dirty draft skips the entry screen: there is already work to edit.
    page();
    expect(screen.getByLabelText(labels.editor.name)).toHaveValue("Unpublished");
    expect(screen.getByRole("status")).toHaveTextContent("recovered");
    fireEvent.click(
      screen.getByRole("button", { name: labels.production.discard_local }),
    );
    expect(document().definition.name).toBe("");
  });
});

describe("Graph studio editing", () => {
  it("defaults new agent steps to 16 attempts and six hours", () => {
    blank();
    fireEvent.click(screen.getByRole("button", { name: "Add agent" }));
    const added = document().definition.nodes.find(
      (node) => node.nodeKey === "agent_2",
    );
    expect(added).toMatchObject({ maxAttempts: 16, timeoutSeconds: 21600 });
  });
  it("opens a step in the drawer and keeps per-step edits while switching", () => {
    blank();
    fireEvent.click(screen.getByRole("button", { name: "Node implement" }));
    fireEvent.change(screen.getByLabelText(labels.editor.instructions), {
      target: { value: "First" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add agent" }));
    fireEvent.change(screen.getByLabelText(labels.editor.instructions), {
      target: { value: "Second" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Node implement" }));
    expect(screen.getByLabelText(labels.editor.instructions)).toHaveValue(
      "First",
    );
    fireEvent.click(screen.getByRole("button", { name: "Node agent_2" }));
    expect(screen.getByLabelText(labels.editor.instructions)).toHaveValue(
      "Second",
    );
  });
  it("edits nested loop bodies without replacing the main flow", () => {
    blank();
    fireEvent.click(screen.getByRole("button", { name: "Add loop" }));
    fireEvent.click(
      screen.getByRole("button", { name: labels.production.nested_edit }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Add agent" }));
    fireEvent.change(screen.getByLabelText(labels.editor.instructions), {
      target: { value: "Inspect this iteration" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: labels.production.root_flow }),
    );
    const definition = document().definition;
    expect(
      definition.nodes
        .find((node) => node.type === "loop")
        ?.loop?.body.nodes.find((node) => node.type === "agent")?.instructions,
    ).toBe("Inspect this iteration");
    expect(
      definition.nodes.find((node) => node.nodeKey === "implement"),
    ).toBeDefined();
  });
  it("inserts a step on a connection", () => {
    blank();
    const before = document().definition;
    const first = before.edges[0]!;
    fireEvent.click(
      screen.getByRole("button", { name: "Insert on first edge" }),
    );
    const after = document().definition;
    const inserted = after.nodes.find(
      (node) => !before.nodes.some((item) => item.nodeKey === node.nodeKey),
    )!;
    expect(inserted.type).toBe("approval");
    expect(after.edges).toContainEqual({ from: first.from, to: inserted.nodeKey });
    expect(after.edges).toContainEqual({ from: inserted.nodeKey, to: first.to });
    expect(after.edges).not.toContainEqual(first);
  });
  it("opens the run input settings from the start step", () => {
    blank();
    fireEvent.click(screen.getByRole("button", { name: "Node start" }));
    expect(screen.getByText(labels.studio.ux.input_hint)).toBeVisible();
  });
  it("hands a step to the AI composer", () => {
    blank();
    fireEvent.click(screen.getByRole("button", { name: "Node implement" }));
    fireEvent.click(
      screen.getByRole("button", { name: labels.studio.ux.ask_ai_step }),
    );
    expect(screen.getByLabelText("Composer")).toHaveTextContent(
      "About the step “implement”",
    );
  });
});

describe("AI turns on the canvas", () => {
  it("marks what a turn changed and undoes the whole turn", () => {
    blank();
    const before = document().definition;
    const next = {
      ...before,
      nodes: [...before.nodes, { ...work("review"), type: "agent" }],
    };
    act(() =>
      mocks.applyTurn!(next, {
        messageId: "m1",
        added: ["review"],
        changed: [],
        removed: [],
        summary: "Added a review",
      }),
    );
    expect(screen.getByRole("button", { name: "Node review" })).toHaveAttribute(
      "data-mark",
      "added",
    );
    expect(
      screen.getByText("AI updated the flow: +1 ~0 −0", { exact: false }),
    ).toBeVisible();
    fireEvent.click(
      screen.getByRole("button", { name: labels.studio.ux.undo_turn }),
    );
    expect(screen.queryByRole("button", { name: "Node review" })).toBeNull();
  });
  it("clears the marks on a hand edit", () => {
    blank();
    const before = document().definition;
    act(() =>
      mocks.applyTurn!(
        { ...before, name: "Renamed" },
        {
          messageId: "m1",
          added: [],
          changed: ["implement"],
          removed: [],
          summary: "",
        },
      ),
    );
    expect(
      screen.getByRole("button", { name: "Node implement" }),
    ).toHaveAttribute("data-mark", "changed");
    fireEvent.click(screen.getByRole("button", { name: "Add agent" }));
    expect(
      screen.getByRole("button", { name: "Node implement" }),
    ).toHaveAttribute("data-mark", "");
  });
});

describe("Saving and publishing", () => {
  it("imports JSON and waits for a confirmed save before navigating", async () => {
    let resolve!: (value: { id: string; revision: number }) => void;
    mocks.api.saveGraph.mockImplementation(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    blank();
    const imported = createSerialDefinition(
      "Imported",
      [{ ...work("work") } as GraphDefinition["nodes"][number]],
      "member-1",
      { type: "object" },
      { start: "begin", approval: "review", end: "finish" },
    );
    fireEvent.click(
      screen.getByRole("button", { name: labels.studio.ux.flow_settings }),
    );
    fireEvent.click(screen.getByRole("tab", { name: labels.studio.ux.tab_more }));
    fireEvent.change(screen.getByLabelText(labels.editor.definition_json), {
      target: { value: exportGraphDefinition(imported) },
    });
    fireEvent.click(
      screen.getByRole("button", { name: labels.editor.import_json }),
    );
    fireEvent.click(screen.getByRole("button", { name: labels.editor.save }));
    await waitFor(() => expect(mocks.api.saveGraph).toHaveBeenCalled());
    expect(
      mocks.api.saveGraph.mock.calls[0]![0].nodes.map(
        (node: { nodeKey: string }) => node.nodeKey,
      ),
    ).toEqual(["begin", "work", "review", "finish"]);
    expect(mocks.replace).not.toHaveBeenCalled();
    await act(async () => resolve({ id: "graph-1", revision: 1 }));
    expect(mocks.replace).toHaveBeenCalledWith("/acme/graphs/graph-1");
  });
  it("keeps current work after a malformed import", () => {
    blank();
    fireEvent.change(screen.getByLabelText(labels.editor.name), {
      target: { value: "Keep" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: labels.studio.ux.flow_settings }),
    );
    fireEvent.click(screen.getByRole("tab", { name: labels.studio.ux.tab_more }));
    fireEvent.change(screen.getByLabelText(labels.editor.definition_json), {
      target: { value: '{"schema_version":1}' },
    });
    fireEvent.click(
      screen.getByRole("button", { name: labels.editor.import_json }),
    );
    expect(screen.getByLabelText(labels.editor.name)).toHaveValue("Keep");
    expect(screen.getAllByRole("alert").length).toBeGreaterThan(0);
    expect(mocks.api.saveGraph).not.toHaveBeenCalled();
  });
  it("saves, validates and stops at the problems, pointing at their steps", async () => {
    const definition = createSerialDefinition(
      "Release",
      [work("implement") as GraphDefinition["nodes"][number]],
      "member-1",
    );
    mocks.graph = record(definition);
    mocks.api.validateGraph.mockResolvedValue({
      valid: false,
      diagnostics: [
        { code: "agent", message: "Choose an agent", nodeKey: "implement" },
      ],
    });
    page(false);
    fireEvent.change(screen.getByLabelText(labels.editor.name), {
      target: { value: "Release 2" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: labels.editor.publish }),
    );
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Node implement" }),
      ).toHaveAttribute("data-issues", "1"),
    );
    expect(mocks.api.saveGraph).toHaveBeenCalledOnce();
    expect(mocks.api.validateGraph).toHaveBeenCalledWith("graph-1");
    expect(mocks.api.publishGraph).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "1 problems" })).toBeVisible();
    expect(document(storedGraphKey).dirty).toBe(false);
  });
  it("publishes a valid graph in one click", async () => {
    mocks.graph = record(
      createSerialDefinition(
        "Release",
        [work("implement") as GraphDefinition["nodes"][number]],
        "member-1",
      ),
    );
    mocks.api.validateGraph.mockResolvedValue({ valid: true, diagnostics: [] });
    mocks.api.publishGraph.mockResolvedValue({});
    page(false);
    fireEvent.click(
      screen.getByRole("button", { name: labels.editor.publish }),
    );
    await waitFor(() => expect(mocks.api.publishGraph).toHaveBeenCalled());
    // Nothing was dirty, so nothing was saved first.
    expect(mocks.api.saveGraph).not.toHaveBeenCalled();
    expect(mocks.api.publishGraph.mock.calls[0]![1]).toMatchObject({
      expectedRevision: 3,
    });
  });
  it("keeps newer unknown schemas read-only", () => {
    mocks.graph = record({
      ...createSerialDefinition("Future", [], "member-1"),
      schemaVersion: 3,
    });
    page(false);
    expect(screen.getByLabelText(labels.editor.name)).toBeDisabled();
    expect(
      screen.getByRole("button", { name: labels.editor.publish }),
    ).toBeDisabled();
  });
});

describe("Graph workspace", () => {
  beforeEach(() => {
    mocks.graph = record(createSerialDefinition("Release", [], "member-1"));
  });
  it("shows this graph's runs and keeps the draft across tabs", () => {
    page(false);
    fireEvent.change(screen.getByLabelText(labels.editor.name), {
      target: { value: "Unsaved" },
    });
    fireEvent.click(
      screen.getAllByRole("tab", { name: labels.studio.ux.runs_tab })[0]!,
    );
    expect(screen.getByText("Runs of graph-1")).toBeVisible();
    fireEvent.click(
      screen.getAllByRole("tab", { name: labels.studio.edit_tab })[0]!,
    );
    expect(screen.getByLabelText(labels.editor.name)).toHaveValue("Unsaved");
  });
  it("opens the run dialog from an issue link", () => {
    mocks.search = "issue=issue-1";
    page(false);
    expect(screen.getByText("Run settings")).toBeVisible();
  });
  it("opens the run dialog from the header", () => {
    page(false);
    expect(screen.queryByText("Run settings")).toBeNull();
    fireEvent.click(
      screen.getByRole("button", { name: labels.studio.ux.run_once }),
    );
    expect(screen.getByText("Run settings")).toBeVisible();
  });
});
