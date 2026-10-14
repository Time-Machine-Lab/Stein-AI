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
  type GraphRecord,
} from "@multica/core/graphs";
import labels from "../locales/en/graphs.json";
import issueLabels from "../locales/en/issues.json";
import { ApiError } from "@multica/core/api/client";
import { GraphEditor } from "./graph-editor";

const mocks = vi.hoisted(() => ({
  save: vi.fn(),
  generate: vi.fn(),
  enabled: true,
  // Every existing case here exercises the legacy path; chat cases opt in.
  builderEnabled: false,
  runtimes: [] as unknown[],
  agents: [
    {
      id: "agent-1",
      name: "Builder",
      model: "deepseek",
      custom_args: ["--verbose"],
      runtime_config: { effort: "high" },
    },
  ],
  replace: vi.fn(),
  actor: { userId: "user-1", role: "admin", memberId: "member-1" },
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
}));
vi.mock("../navigation", () => ({
  useNavigation: () => ({ replace: mocks.replace }),
}));
vi.mock("@multica/core/paths", () => ({
  useWorkspaceSlug: () => "acme",
  useWorkspacePaths: () => ({
    graphDetail: (id: string) => `/acme/graphs/${id}`,
  }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({
    queryKey: ["agents"],
    queryFn: async () => [],
    initialData: mocks.agents,
  }),
  memberListOptions: () => ({
    queryKey: ["members"],
    queryFn: async () => [],
    initialData: [
      { id: "member-1", name: "Ada" },
      { id: "member-2", name: "Grace" },
    ],
  }),
}));
vi.mock("./shared", async (original) => ({
  ...(await original<object>()),
  useGraphActor: () => mocks.actor,
}));
vi.mock("@multica/core/graphs", async (original) => ({
  ...(await original<object>()),
  useSaveGraph: () => ({ mutateAsync: mocks.save, isPending: false }),
  useValidateGraph: () => ({
    mutate: vi.fn(),
    reset: vi.fn(),
    isPending: false,
  }),
  usePublishGraph: () => ({
    mutate: vi.fn(),
    reset: vi.fn(),
    isPending: false,
  }),
  useGenerateGraph: () => ({ mutateAsync: mocks.generate }),
  graphGenerationOptions: () => ({
    queryKey: ["generation-options"],
    queryFn: async () => ({
      enabled: mocks.enabled,
      builderEnabled: mocks.builderEnabled,
    }),
    initialData: {
      enabled: mocks.enabled,
      builderEnabled: mocks.builderEnabled,
    },
  }),
}));
vi.mock("@multica/core/runtimes", () => ({
  runtimeListOptions: () => ({
    queryKey: ["runtimes"],
    queryFn: async () => [],
    initialData: mocks.runtimes,
  }),
}));
// The two AI modes are their own units, with their own tests; these cases are
// about which one the editor mounts and what it hands them.
vi.mock("./graph-ai-setup-panel", () => ({
  GraphAiSetupPanel: ({ onManual }: { onManual: () => void }) => (
    <div>
      <p>Setup step</p>
      <button type="button" onClick={onManual}>
        Build manually
      </button>
    </div>
  ),
}));
vi.mock("./graph-ai-workspace", () => ({
  GraphAiWorkspace: ({
    ai,
    onMissing,
  }: {
    ai: { sessionId: string };
    onMissing: () => void;
  }) => (
    <div aria-label="Design conversation">
      <p>{ai.sessionId}</p>
      <button type="button" onClick={onMissing}>
        Conversation lost
      </button>
    </div>
  ),
}));
// Canvas pointer/drag behavior is covered in browser acceptance; these tests isolate document state and proposal semantics.
vi.mock("./graph-canvas", () => ({
  useGraphNodeLabels: () => ({
    start: "Start",
    end: "End",
    agent: "Agent",
    approval: "Approval",
    branch: "Branch",
    parallel: "Parallel",
    join: "Join",
    loop: "Loop",
    subgraph: "Subflow",
    delay: "Delay",
    http: "HTTP",
    script: "Script",
  }),
  GraphCanvas: ({
    definition,
    onSelect,
  }: {
    definition: GraphDefinition;
    onSelect?: (key: string) => void;
  }) => (
    <div aria-label="Canvas">
      {definition.nodes.map((node) => (
        <button
          type="button"
          key={node.nodeKey}
          onClick={() => onSelect?.(node.nodeKey)}
        >{`Node ${node.nodeKey}`}</button>
      ))}
    </div>
  ),
}));
vi.mock("../editor/readonly-content", () => ({
  ReadonlyContent: ({ content }: { content: string }) => <span>{content}</span>,
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn() } }));

function editor(graph?: GraphRecord) {
  return render(
    <QueryClientProvider
      client={
        new QueryClient({
          defaultOptions: { queries: { staleTime: Infinity } },
        })
      }
    >
      <GraphEditor wsId="ws-1" graph={graph} />
    </QueryClientProvider>,
  );
}
function manualEditor(graph?: GraphRecord) {
  const result = editor(graph);
  if (!graph)
    fireEvent.click(screen.getByRole("button", { name: "Build manually" }));
  return result;
}
function generatedDefinition() {
  return createSerialDefinition(
    "Research and review",
    [
      {
        nodeKey: "research",
        type: "agent",
        agentId: "agent-1",
        instructions: "Research the request",
        executionMode: "none",
        maxAttempts: 2,
        timeoutSeconds: 60,
        inputs: {},
        outputSchema: { type: "object" },
      },
    ],
    "member-1",
  );
}
function generate() {
  fireEvent.change(screen.getByLabelText("What should this workflow do?"), {
    target: { value: "Research and review our release" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Generate workflow" }));
}
function agent(id: string, name: string) {
  return {
    id,
    name,
    model: "deepseek",
    custom_args: [] as string[],
    runtime_config: { effort: "high" },
  };
}
const draftKey = graphDraftKey("ws-1", "user-1");
beforeEach(() => {
  vi.clearAllMocks();
  window.localStorage.clear();
  useGraphDraftStore.setState({ documents: {} });
  mocks.enabled = true;
  mocks.builderEnabled = false;
  mocks.runtimes = [];
  mocks.agents = [agent("agent-1", "Builder")];
  mocks.actor.role = "admin";
  mocks.save.mockResolvedValue({ id: "graph-1", revision: 1 });
});

describe("AI graph proposals", () => {
  it("edits a readable step name without changing execution bindings", () => {
    manualEditor();
    fireEvent.change(screen.getByLabelText("Step name"), {
      target: { value: "Research the release" },
    });
    expect(
      useGraphDraftStore.getState().documents[draftKey]?.definition.nodes[1],
    ).toMatchObject({ nodeKey: "implement", label: "Research the release" });
  });
  it("allows existing agents with custom runtime settings and selects the current reviewer", () => {
    editor();
    expect(
      screen.getByRole("combobox", { name: "Workflow agent" }),
    ).toHaveTextContent("Builder");
    expect(
      screen.getByRole("combobox", { name: "Final approver" }),
    ).toHaveTextContent("Ada");
  });
  it("requires Apply before replacing a draft and never saves or runs a generated proposal", async () => {
    mocks.generate.mockResolvedValue({
      definition: generatedDefinition(),
      summary: "Added review",
    });
    editor();
    generate();
    expect(
      await screen.findByRole("button", { name: "Apply changes" }),
    ).toBeEnabled();
    expect(
      useGraphDraftStore.getState().documents[draftKey]?.definition.name,
    ).toBe("");
    expect(mocks.save).not.toHaveBeenCalled();
    expect(mocks.replace).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Apply changes" }));
    expect(screen.getByLabelText("Name")).toHaveValue("Research and review");
    fireEvent.click(screen.getByRole("button", { name: "Undo" }));
    expect(screen.getByLabelText("Name")).toHaveValue("");
    fireEvent.click(screen.getByRole("button", { name: "Redo" }));
    expect(screen.getByLabelText("Name")).toHaveValue("Research and review");
  });
  it("sends the current graph and conversation during refinement", async () => {
    mocks.generate.mockResolvedValue({
      definition: generatedDefinition(),
      summary: "Added review",
    });
    editor();
    generate();
    fireEvent.click(
      await screen.findByRole("button", { name: "Apply changes" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Draft with AI" }));
    generate();
    await waitFor(() => expect(mocks.generate).toHaveBeenCalledTimes(2));
    expect(mocks.generate.mock.calls[1]![0]).toMatchObject({
      currentDefinition: { name: "Research and review" },
      messages: [
        { role: "user", content: "Research and review our release" },
        { role: "assistant", content: "Added review" },
        { role: "user", content: "Research and review our release" },
      ],
    });
  });
  it("discards a proposal without changing existing work", async () => {
    mocks.generate.mockResolvedValue({ definition: generatedDefinition() });
    manualEditor();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Existing work" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Draft with AI" }));
    generate();
    fireEvent.click(
      await screen.findByRole("button", { name: "Discard proposal" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Build manually" }));
    expect(screen.getByLabelText("Name")).toHaveValue("Existing work");
  });
  it("blocks applying an older proposal after concurrent manual edits", async () => {
    let resolve!: (value: { definition: GraphDefinition }) => void;
    mocks.generate.mockImplementation(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    editor();
    generate();
    fireEvent.click(screen.getByRole("button", { name: "Build manually" }));
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Newer edits" },
    });
    await act(async () => resolve({ definition: generatedDefinition() }));
    expect(
      screen.getByRole("button", { name: "Apply changes" }),
    ).toBeDisabled();
    expect(screen.getByLabelText("Name")).toHaveValue("Newer edits");
    expect(screen.getByRole("alert")).toHaveTextContent("Your graph changed");
  });
  it("preserves the prompt and surfaces a localized generation timeout", async () => {
    mocks.generate.mockRejectedValue(
      new ApiError("Provider timeout", 504, "Gateway Timeout", {
        code: "generation_timeout",
      }),
    );
    editor();
    generate();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Generation took too long",
    );
    expect(screen.getByLabelText("What should this workflow do?")).toHaveValue(
      "Research and review our release",
    );
  });
  it("keeps manual authoring available when AI is unconfigured", () => {
    mocks.enabled = false;
    editor();
    expect(
      screen.getByRole("button", { name: "Generate workflow" }),
    ).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Build manually" }));
    expect(screen.getByLabelText("Name")).toBeEnabled();
  });
});

describe("Chat authoring", () => {
  beforeEach(() => {
    mocks.builderEnabled = true;
  });
  it("hands AI mode to the setup step until a conversation exists", () => {
    editor();
    expect(screen.getByText("Setup step")).toBeVisible();
    // The one-shot form and its request state belong to the other path.
    expect(
      screen.queryByRole("button", { name: "Generate workflow" }),
    ).toBeNull();
    expect(
      screen.queryByRole("combobox", { name: "Workflow agent" }),
    ).toBeNull();
  });
  it("opens the bound conversation and falls back to the setup step without it", () => {
    editor();
    act(() =>
      useGraphDraftStore.getState().bindAi(draftKey, {
        sessionId: "session-1",
        agentId: "carrier-1",
        executorId: "agent-1",
        scopeIds: ["agent-1"],
        approverId: "member-1",
        runtimeId: "runtime-1",
        appliedMessageId: null,
      }),
    );
    expect(screen.getByLabelText("Design conversation")).toHaveTextContent(
      "session-1",
    );
    fireEvent.click(screen.getByRole("button", { name: "Conversation lost" }));
    expect(screen.getByText("Setup step")).toBeVisible();
    expect(useGraphDraftStore.getState().documents[draftKey]?.ai).toBeNull();
  });
  it("keeps the one-shot form on a server that has no builder", () => {
    mocks.builderEnabled = false;
    editor();
    expect(
      screen.getByRole("button", { name: "Generate workflow" }),
    ).toBeVisible();
    expect(screen.queryByText("Setup step")).toBeNull();
  });
});


describe("Graph production editor", () => {
  it("recovers a workspace/user draft after unmount and offers explicit discard", () => {
    const first = manualEditor();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Unpublished" },
    });
    first.unmount();
    useGraphDraftStore.setState({ documents: {} });
    manualEditor();
    expect(screen.getByLabelText("Name")).toHaveValue("Unpublished");
    expect(screen.getByRole("status")).toHaveTextContent("recovered");
    fireEvent.click(
      screen.getByRole("button", { name: "Discard local changes" }),
    );
    expect(screen.getByLabelText("Name")).toHaveValue("");
  });
  it("edits nested loop bodies without replacing the main flow", () => {
    manualEditor();
    fireEvent.change(screen.getByRole("combobox", { name: "Add node" }), {
      target: { value: "loop" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add node" }));
    fireEvent.click(screen.getByRole("button", { name: "Edit inner flow" }));
    fireEvent.change(screen.getByRole("combobox", { name: "Add node" }), {
      target: { value: "agent" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add node" }));
    fireEvent.change(screen.getByLabelText("Instructions"), {
      target: { value: "Inspect this iteration" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Main flow" }));
    const definition =
      useGraphDraftStore.getState().documents[draftKey]!.definition;
    expect(
      definition.nodes
        .find((node) => node.type === "loop")
        ?.loop?.body.nodes.find((node) => node.type === "agent")?.instructions,
    ).toBe("Inspect this iteration");
    expect(
      definition.nodes.find((node) => node.nodeKey === "implement"),
    ).toBeDefined();
  });
  it("preserves per-step changes while switching selected nodes", () => {
    manualEditor();
    fireEvent.change(screen.getByLabelText("Instructions"), {
      target: { value: "First" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add node" }));
    fireEvent.change(screen.getByLabelText("Instructions"), {
      target: { value: "Second" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Node implement" }));
    expect(screen.getByLabelText("Instructions")).toHaveValue("First");
    fireEvent.click(screen.getByRole("button", { name: "Node agent_2" }));
    expect(screen.getByLabelText("Instructions")).toHaveValue("Second");
  });
  it("preserves imported control keys and waits for confirmed save before navigating", async () => {
    let resolve!: (value: { id: string; revision: number }) => void;
    mocks.save.mockImplementation(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    manualEditor();
    const imported = createSerialDefinition(
      "Imported",
      [{ ...generatedDefinition().nodes[1]!, nodeKey: "work" }],
      "member-1",
      { type: "object" },
      { start: "begin", approval: "review", end: "finish" },
    );
    fireEvent.change(screen.getByLabelText("Definition JSON"), {
      target: { value: exportGraphDefinition(imported) },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Import JSON", hidden: true }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save draft" }));
    expect(
      mocks.save.mock.calls[0]![0].definition.nodes.map(
        (node: { nodeKey: string }) => node.nodeKey,
      ),
    ).toEqual(["begin", "work", "review", "finish"]);
    expect(mocks.replace).not.toHaveBeenCalled();
    await act(async () => resolve({ id: "graph-1", revision: 1 }));
    expect(mocks.replace).toHaveBeenCalledWith("/acme/graphs/graph-1");
  });
  it("preserves current work after malformed imports", () => {
    manualEditor();
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Keep" },
    });
    fireEvent.change(screen.getByLabelText("Definition JSON"), {
      target: { value: '{"schema_version":1}' },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Import JSON", hidden: true }),
    );
    expect(screen.getByLabelText("Name")).toHaveValue("Keep");
    expect(screen.getByRole("alert")).toBeVisible();
    expect(mocks.save).not.toHaveBeenCalled();
  });
  it("keeps newer unknown schemas read-only", () => {
    manualEditor({
      id: "graph-1",
      workspaceId: "ws-1",
      name: "Future",
      revision: 1,
      definition: { ...generatedDefinition(), schemaVersion: 3 },
      createdAt: "",
      updatedAt: "",
      versions: [],
    });
    expect(screen.getByLabelText("Name")).toBeDisabled();
    expect(screen.getByRole("button", { name: "Save draft" })).toBeDisabled();
  });
});
