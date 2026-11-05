import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import labels from "../locales/en/graphs.json";
import { GraphRunPage } from "./graph-run-page";

const box = vi.hoisted(() => ({
  run: {} as Record<string, unknown>,
  approve: vi.fn(),
}));

vi.mock("../i18n", () => ({
  useT: () => ({
    t: (
      selector: (value: typeof labels) => string,
      options?: Record<string, unknown>,
    ) => {
      let text = selector(labels);
      for (const [key, value] of Object.entries(options ?? {}))
        text = text.replace(`{{${key}}}`, String(value));
      return text;
    },
  }),
  useTimeAgo: () => () => "just now",
}));
vi.mock("@xyflow/react", () => ({
  Panel: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));
vi.mock("../navigation", () => ({
  useNavigation: () => ({
    pathname: "/acme/graphs/runs/run-1",
    searchParams: new URLSearchParams(),
  }),
  AppLink: ({ href, children, ...props }: React.ComponentProps<"a">) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
}));
vi.mock("../common/task-transcript", () => ({
  TranscriptButton: () => <button type="button">Transcript</button>,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => ({
    userId: "user-1",
    member: { id: "member-1" },
    role: "owner",
  }),
}));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    graphs: () => "/acme/graphs",
    graphRun: (id: string) => `/acme/graphs/runs/${id}`,
    graphDetail: (id: string) => `/acme/graphs/${id}`,
    issueDetail: (id: string) => `/acme/issues/${id}`,
    attachmentPreview: (id: string) => `/acme/attachments/${id}`,
  }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({
    queryKey: ["members"],
    queryFn: async () => [],
    initialData: [],
  }),
  agentListOptions: () => ({
    queryKey: ["agents"],
    queryFn: async () => [],
    initialData: [],
  }),
}));
vi.mock("@multica/core/graphs", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/graphs")>();
  return {
    ...actual,
    graphRunOptions: () => ({
      queryKey: ["run"],
      queryFn: async () => box.run,
      initialData: box.run,
    }),
    graphNodeTasksOptions: () => ({
      queryKey: ["node-tasks"],
      queryFn: async () => [],
      initialData: [],
    }),
    graphStartOptions: () => ({
      queryKey: ["start-options"],
      queryFn: async () => ({ repositories: [] }),
      initialData: { repositories: [] },
    }),
    useControlGraphRun: () => ({ mutate: vi.fn(), isPending: false }),
    useApproveGraphRun: () => ({ mutate: box.approve, isPending: false }),
    useReplaceGraphApprover: () => ({ mutate: vi.fn(), isPending: false }),
  };
});
// The canvas is React Flow and the value renderer has its own tests, so this
// file only asserts how the run page wires them together.
vi.mock("./graph-result-view", () => ({
  GraphValueView: ({ value }: { value: unknown }) => (
    <pre data-testid="value">{JSON.stringify(value)}</pre>
  ),
  GraphArtifactCards: () => <p>Artifacts</p>,
}));
vi.mock("./graph-canvas", () => ({
  GraphStateLegend: () => <p>Legend</p>,
  GraphCanvas: ({
    onSelect,
    states,
    selectedKey,
    details,
  }: {
    onSelect?: (key: string) => void;
    states?: Record<string, string>;
    selectedKey?: string;
    details?: Record<string, string>;
  }) => (
    <div
      data-testid="graph-flow"
      data-selected={selectedKey ?? ""}
      data-details={JSON.stringify(details ?? {})}
    >
      {Object.entries(states ?? {}).map(([key, state]) => (
        <button key={key} type="button" onClick={() => onSelect?.(key)}>
          {`flow:${key}:${state}`}
        </button>
      ))}
    </div>
  ),
}));

const definition = {
  schemaVersion: 2,
  nodes: [
    { nodeKey: "start", type: "start", label: "Start" },
    { nodeKey: "research", type: "agent", label: "Research" },
    { nodeKey: "end", type: "end", label: "End" },
  ],
  edges: [],
};

function node(
  id: string,
  nodeKey: string,
  state: string,
  extra: Record<string, unknown> = {},
) {
  return {
    id,
    nodeKey,
    nodeType: nodeKey === "research" ? "agent" : nodeKey,
    label: nodeKey[0]!.toUpperCase() + nodeKey.slice(1),
    state,
    attempts: [],
    maxAttempts: 3,
    input: {},
    output: null,
    submissions: [],
    ...extra,
  };
}

function makeRun(overrides: Record<string, unknown> = {}) {
  return {
    id: "run-1",
    rootIssueId: "issue-1",
    initiatorId: "user-1",
    state: "succeeded",
    controlState: "run",
    revision: 4,
    graphName: "Release research",
    definition,
    input: {},
    inputHash: "hash-1",
    versionId: "version-1",
    output: { brief: { conclusion: "Ship it" } },
    artifacts: [],
    events: [],
    nodes: [
      node("n1", "start", "succeeded"),
      node("n2", "research", "succeeded", {
        output: { summary: "three facts" },
      }),
      node("n3", "end", "succeeded"),
    ],
    ...overrides,
  };
}

function renderPage() {
  return render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <GraphRunPage />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  box.run = makeRun();
});

describe("Graph run page", () => {
  it("gives the run a flow pane and a record pane side by side", () => {
    renderPage();
    const flow = screen.getByTestId("run-flow");
    const rail = screen.getByTestId("run-rail");
    // One grid, two panes: the page is a fixed-height workspace, so a run with
    // fifty-seven sources cannot lengthen it.
    expect(rail.parentElement).toBe(flow.parentElement);
    expect(flow.compareDocumentPosition(rail)).toBe(
      Node.DOCUMENT_POSITION_FOLLOWING,
    );
  });

  it("keeps the run result one tab away from the step", () => {
    renderPage();
    expect(screen.getByTestId("node-detail")).toBeVisible();
    fireEvent.click(
      screen.getByRole("tab", { name: labels.studio.ux.tab_output }),
    );
    expect(screen.getByTestId("run-result")).toHaveTextContent("Ship it");
  });

  it("shows how far the run has come", () => {
    renderPage();
    expect(screen.getByRole("progressbar")).toHaveAttribute(
      "aria-valuenow",
      "1",
    );
    expect(screen.getByText("1 / 1 steps")).toBeVisible();
  });

  it("opens on the node that produced the result, not the synthetic end node", () => {
    renderPage();
    expect(screen.getByRole("heading", { level: 2, name: "Research" })).toBeVisible();
    expect(screen.getByTestId("graph-flow")).toHaveAttribute(
      "data-selected",
      "research",
    );
  });

  it("shows what each node produced on the node itself", () => {
    renderPage();
    const details = JSON.parse(
      screen.getByTestId("graph-flow").getAttribute("data-details") ?? "{}",
    );
    expect(details.research).toBe("three facts");
    expect(details.start).toBe("");
  });

  it("opens on the node that needs attention when one does", () => {
    box.run = makeRun({
      state: "blocked",
      nodes: [
        node("n1", "start", "succeeded"),
        node("n2", "research", "blocked", {
          blockReason: "execution_unknown",
          output: { summary: "three facts" },
        }),
        node("n3", "end", "pending"),
      ],
    });
    renderPage();
    expect(screen.getByRole("heading", { level: 2, name: "Research" })).toBeVisible();
    expect(
      screen.getByText(
        "Execution has not been confirmed stopped. A replacement attempt cannot start yet.",
      ),
    ).toBeVisible();
    expect(screen.getByTestId("graph-flow")).toHaveAttribute(
      "data-selected",
      "research",
    );
  });

  it("switches the record pane when a node is selected on the flow", () => {
    renderPage();
    fireEvent.click(
      screen.getByRole("button", { name: "flow:end:succeeded" }),
    );
    expect(screen.getByRole("heading", { level: 2, name: "End" })).toBeVisible();
    expect(screen.getByTestId("graph-flow")).toHaveAttribute(
      "data-selected",
      "end",
    );
  });

  it("surfaces a pending approval above the flow and gates rejection on a reason", () => {
    box.run = makeRun({
      state: "waiting_approval",
      approvals: [
        {
          id: "gate-1",
          nodeRunId: "n2",
          state: "pending",
          candidateDigest: "digest-1",
          approverId: "member-1",
          mode: "all",
          quorum: 1,
          decisions: [],
          candidate: { outputs: { research: { summary: "three facts" } } },
        },
      ],
    });
    renderPage();
    expect(screen.getByText("Your approval is needed: Research")).toBeVisible();
    const approve = screen.getByRole("button", {
      name: labels.studio.ux.approve,
    });
    expect(
      screen
        .getByTestId("run-flow")
        .compareDocumentPosition(approve) & Node.DOCUMENT_POSITION_PRECEDING,
    ).toBeTruthy();
    fireEvent.click(approve);
    expect(box.approve.mock.calls[0]![0]).toMatchObject({
      approvalId: "gate-1",
      decision: "approved",
    });
    // Rejecting needs a reason, so it opens the whole gate first.
    expect(screen.queryByLabelText("Reason")).toBeNull();
    fireEvent.click(
      screen.getByRole("button", { name: labels.studio.ux.reject }),
    );
    const reject = screen.getByRole("button", { name: "Reject candidate" });
    expect(reject).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Reason"), {
      target: { value: "Evidence is thin" },
    });
    expect(reject).toBeEnabled();
  });

  it("says where a blocked run is stuck and opens that step", () => {
    box.run = makeRun({
      state: "blocked",
      nodes: [
        node("n1", "start", "succeeded"),
        node("n2", "research", "blocked", { blockReason: "Runtime offline" }),
        node("n3", "end", "pending"),
      ],
    });
    renderPage();
    expect(
      screen.getByText("The run is blocked at Research.", { exact: false }),
    ).toBeVisible();
    fireEvent.click(
      screen.getByRole("button", { name: labels.studio.ux.open_step }),
    );
    expect(screen.getByTestId("graph-flow")).toHaveAttribute(
      "data-selected",
      "research",
    );
  });

  it("keeps the reason field out of the header when nothing is being decided", () => {
    renderPage();
    expect(screen.queryByLabelText("Reason")).toBeNull();
  });

  // The engine ends every run that is not a success by requesting cancellation -
  // that is how it stops advancing while it drains live attempts - and the run
  // keeps the marker afterwards. A rejected run therefore has to render as
  // rejected and nothing else, or it reads as a cancellation still in progress
  // that no button on the page could ever finish.
  it("drops the control state from a run that has already ended", () => {
    box.run = makeRun({ state: "rejected", controlState: "cancel_requested" });
    renderPage();
    expect(screen.getByText("Rejected")).toBeVisible();
    expect(screen.queryByText("Cancelling")).toBeNull();
    expect(
      screen.queryByText(
        "Cancellation requested. Waiting for execution to stop.",
      ),
    ).toBeNull();
  });

  it("still says a run that is being stopped is stopping", () => {
    box.run = makeRun({ state: "active", controlState: "cancel_requested" });
    renderPage();
    expect(
      screen.getByText(
        "Cancellation requested. Waiting for execution to stop.",
      ),
    ).toBeVisible();
  });

  it("falls back to a node list when the run carries no definition", () => {
    box.run = makeRun({ definition: undefined });
    renderPage();
    expect(screen.queryByTestId("graph-flow")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /Research/ }));
    expect(screen.getByRole("heading", { level: 2, name: "Research" })).toBeVisible();
  });
});
