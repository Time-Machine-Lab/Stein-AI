import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import {
  createSerialDefinition,
  definitionToAliases,
  encodeGraphBuilderInput,
  graphDraftKey,
  useGraphDraftStore,
  type GraphDefinition,
  type GraphDraftAi,
} from "@multica/core/graphs";
import type { Agent, ChatMessage, RuntimeDevice } from "@multica/core/types";
import labels from "../locales/en/graphs.json";
import { graphBuilderDirectory } from "./graph-ai-actors";
import { GraphAiWorkspace } from "./graph-ai-workspace";

const mocks = vi.hoisted(() => ({
  messages: [] as ChatMessage[],
  pending: false,
  missing: false,
  onOpenEditor: vi.fn(),
  onMissing: vi.fn(),
  runtimes: [] as RuntimeDevice[],
  transform: undefined as ((content: string) => string) | undefined,
}));

vi.mock("../i18n", () => ({
  useT: () => ({
    t: (
      selector: (value: unknown) => string,
      options?: Record<string, unknown>,
    ) => {
      let text = selector(labels);
      for (const [key, value] of Object.entries(options ?? {}))
        text = text.replace(`{{${key}}}`, String(value));
      return text;
    },
  }),
}));
// The session, the composer, the transcript and the canvas all have their own
// behavior and their own tests. What is under test here is what this component
// does with the messages they hand it.
vi.mock("./use-graph-builder-session", () => ({
  useGraphBuilderSession: () => ({
    messages: mocks.messages,
    missing: mocks.missing,
    messagesLoading: false,
    pendingTask: undefined,
    pending: mocks.pending,
    starting: false,
    closing: false,
    error: null,
    restoreDraftRequest: null,
    handleRestoreDraftApplied: vi.fn(),
    start: vi.fn(),
    destroy: vi.fn(async () => true),
    send: vi.fn(async () => true),
    stop: vi.fn(),
  }),
}));
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
vi.mock("../chat/components/chat-message-list", () => ({
  ChatMessageSkeleton: () => <p>Loading the conversation</p>,
  ChatMessageList: ({
    messages,
    transformContent,
  }: {
    messages: { id: string; role: string; content: string }[];
    transformContent?: (content: string) => string;
  }) => {
    mocks.transform = transformContent;
    return (
      <div aria-label="Transcript">
        {messages.map((message) => (
          <p key={message.id} data-role={message.role}>
            {transformContent ? transformContent(message.content) : message.content}
          </p>
        ))}
      </div>
    );
  },
}));
vi.mock("../chat/components/chat-input", () => ({
  ChatInput: ({ disabled }: { disabled?: boolean }) => (
    <button type="button" disabled={disabled}>
      Send message
    </button>
  ),
}));
vi.mock("./graph-canvas", () => ({
  GraphCanvas: ({ definition }: { definition: GraphDefinition }) => (
    <div aria-label="Canvas">
      {definition.name}:{definition.nodes.map((node) => node.nodeKey).join(",")}
    </div>
  ),
}));

const agents = [
  {
    id: "agent-1",
    name: "Builder",
    description: "",
    model: "claude",
    runtime_id: "runtime-1",
    runtime_bound: true,
    archived_at: null,
    skills: [],
  },
  {
    id: "agent-2",
    name: "Reviewer",
    description: "",
    model: "claude",
    runtime_id: "runtime-1",
    runtime_bound: true,
    archived_at: null,
    skills: [],
  },
] as unknown as Agent[];
const online = {
  id: "runtime-1",
  status: "online",
  provider: "claude",
  owner_id: "user-1",
  visibility: "private",
} as unknown as RuntimeDevice;

const ai: GraphDraftAi = {
  sessionId: "session-1",
  agentId: "carrier-1",
  executorId: "agent-1",
  scopeIds: ["agent-1", "agent-2"],
  approverId: "member-1",
  runtimeId: "runtime-1",
  appliedMessageId: null,
};
const draftKey = graphDraftKey("ws-1", "user-1");
const directory = graphBuilderDirectory(ai, agents, [online], "user-1");

function step(nodeKey: string, agentId: string) {
  return {
    nodeKey,
    type: "agent" as const,
    agentId,
    instructions: `Do ${nodeKey}`,
    executionMode: "none" as const,
    maxAttempts: 2,
    timeoutSeconds: 60,
    inputs: {},
    outputSchema: { type: "object" },
  };
}
const current = createSerialDefinition("Research", [step("research", "agent-1")], "member-1");
const next = createSerialDefinition(
  "Research and review",
  [step("research", "agent-1"), step("review", "agent-2")],
  "member-1",
);

/** A reply in the wire form the contract asks for. */
function reply(summary: string, definition: unknown, prose = "Here it is.") {
  return `${prose}\n<graph_draft>${JSON.stringify({ summary, definition })}</graph_draft>`;
}
function aliased(definition: GraphDefinition) {
  return definitionToAliases(definition, directory);
}
function turn(id: string, content: string, role: "user" | "assistant") {
  return { id, role, content } as ChatMessage;
}

/**
 * The slice of the editor this component is mounted into: the definition, the
 * history and the conversation come off the draft document, exactly as they do
 * there. Merging is asserted through the store rather than through a spy
 * because the store is where "the draft followed the conversation" lives.
 */
function Harness() {
  const draft = useGraphDraftStore((state) => state.documents[draftKey]);
  return (
    <GraphAiWorkspace
      draftKey={draftKey}
      ai={ai}
      definition={draft?.definition ?? current}
      agents={agents}
      runtimes={mocks.runtimes}
      currentUserId="user-1"
      canUndo={Boolean(draft?.past.length)}
      onApplyDefinition={(updated) =>
        useGraphDraftStore.getState().edit(draftKey, updated)
      }
      onOpenEditor={mocks.onOpenEditor}
      onMissing={mocks.onMissing}
    />
  );
}
function draftDocument() {
  return useGraphDraftStore.getState().documents[draftKey]!;
}
const canvas = () => screen.getByLabelText("Canvas").textContent;

beforeEach(() => {
  vi.clearAllMocks();
  window.localStorage.clear();
  useGraphDraftStore.setState({ documents: {} });
  useGraphDraftStore.getState().open(draftKey, current, 0);
  useGraphDraftStore.getState().bindAi(draftKey, ai);
  mocks.messages = [];
  mocks.pending = false;
  mocks.missing = false;
  mocks.runtimes = [online];
});

describe("Graph design workspace", () => {
  it("merges a reply into the draft the canvas and the store both read", () => {
    mocks.messages = [
      turn("m1", encodeGraphBuilderInput("research, then review", current, directory), "user"),
      turn("m2", reply("Added a review step", aliased(next)), "assistant"),
    ];
    render(<Harness />);
    expect(canvas()).toBe("Research and review:start,research,review,approval,end");
    expect(draftDocument().definition.name).toBe("Research and review");
    expect(screen.getByText("Added a review step")).toBeVisible();
    // The reply has been merged, so it is never merged a second time: this is
    // what makes a refresh idempotent.
    expect(draftDocument().ai?.appliedMessageId).toBe("m2");
    // Both of the reply's teammate aliases resolved to real agents.
    const nodes = draftDocument().definition.nodes;
    expect(nodes.find((node) => node.nodeKey === "review")?.agentId).toBe("agent-2");
  });
  it("reads a reply against the alias order the conversation was started with", () => {
    const wire = aliased(next) as { nodes: { node_key: string; agent_id: string }[] };
    expect(wire.nodes.find((node) => node.node_key === "research")?.agent_id).toBe(
      "agent_default",
    );
    expect(wire.nodes.find((node) => node.node_key === "review")?.agent_id).toBe(
      "agent_1",
    );
  });
  it("keeps the merged draft across a reload without merging it again", () => {
    mocks.messages = [
      turn("m1", encodeGraphBuilderInput("research, then review", current, directory), "user"),
      turn("m2", reply("Added a review step", aliased(next)), "assistant"),
    ];
    const first = render(<Harness />);
    expect(draftDocument().past).toHaveLength(1);
    first.unmount();
    // A reload: the messages come back from the server, the draft from storage.
    useGraphDraftStore.setState({ documents: {} });
    useGraphDraftStore.getState().open(draftKey, current, 0);
    render(<Harness />);
    expect(canvas()).toBe("Research and review:start,research,review,approval,end");
    // No second merge means no second undo step and no phantom edit.
    expect(draftDocument().past).toHaveLength(0);
    expect(draftDocument().ai?.appliedMessageId).toBe("m2");
  });
  it("leaves a turn that proposes nothing alone", () => {
    mocks.messages = [turn("m1", "What should the first step deliver?", "assistant")];
    render(<Harness />);
    expect(canvas()).toBe("Research:start,research,approval,end");
    expect(draftDocument().past).toHaveLength(0);
    expect(screen.queryByRole("alert")).toBeNull();
  });
  it("does not judge a reply while it is still being written", () => {
    mocks.pending = true;
    mocks.messages = [
      turn("m1", 'One moment.<graph_draft>{"summary":"x","definit', "assistant"),
    ];
    const view = render(<Harness />);
    // Half a block is what a stream looks like, not a malformed draft.
    expect(screen.queryByRole("alert")).toBeNull();
    expect(draftDocument().ai?.appliedMessageId).toBeNull();
    mocks.pending = false;
    view.rerender(<Harness />);
    expect(screen.getByRole("alert")).toHaveTextContent(
      "The draft in this reply is not valid JSON",
    );
  });
  it("keeps the previous draft and explains a block it cannot read", () => {
    mocks.messages = [
      turn("m2", 'Sorry.<graph_draft>{"definition":{', "assistant"),
    ];
    render(<Harness />);
    expect(canvas()).toBe("Research:start,research,approval,end");
    expect(draftDocument().past).toHaveLength(0);
    expect(screen.getByRole("alert")).toHaveTextContent(
      "not valid JSON, so the canvas is unchanged",
    );
    expect(draftDocument().ai?.appliedMessageId).toBe("m2");
  });
  it("refuses an alias the directory does not offer and names the ones it does", () => {
    const wire = aliased(next) as { nodes: { node_key: string; agent_id: string }[] };
    wire.nodes.find((node) => node.node_key === "review")!.agent_id = "agent_9";
    mocks.messages = [turn("m2", reply("Added a review step", wire), "assistant")];
    render(<Harness />);
    expect(canvas()).toBe("Research:start,research,approval,end");
    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent("agent_9");
    expect(alert).toHaveTextContent(
      "Available teammates: agent_1, agent_default, approver_default",
    );
  });
  it("hides the block from the transcript, mid-stream included", () => {
    mocks.messages = [
      turn("m2", 'Here it is.\n<graph_draft>{"definition":{"nodes"', "assistant"),
    ];
    render(<Harness />);
    expect(mocks.transform).toBeTypeOf("function");
    // What the reader sees: the prose, and nothing of the payload.
    expect(screen.getByLabelText("Transcript")).toHaveTextContent("Here it is.");
    expect(screen.getByLabelText("Transcript")).not.toHaveTextContent(
      "graph_draft",
    );
  });
  it("undoes a merge back to the previous version", () => {
    mocks.messages = [
      turn("m2", reply("Added a review step", aliased(next)), "assistant"),
    ];
    render(<Harness />);
    fireEvent.click(screen.getByRole("button", { name: "Undo" }));
    expect(canvas()).toBe("Research:start,research,approval,end");
  });
  it("holds the conversation while its runtime is offline", () => {
    mocks.runtimes = [{ ...online, status: "offline" } as unknown as RuntimeDevice];
    render(<Harness />);
    expect(screen.getByRole("button", { name: "Send message" })).toBeDisabled();
    expect(screen.getByText("Runtime offline")).toBeVisible();
  });
  it("hands a conversation the server no longer has back to the caller", () => {
    mocks.missing = true;
    render(<Harness />);
    expect(mocks.onMissing).toHaveBeenCalledOnce();
  });
});

describe("Graph design workspace exit", () => {
  it("keeps the draft but forgets the conversation once the server confirms", async () => {
    mocks.messages = [turn("m2", reply("Added a review step", aliased(next)), "assistant")];
    render(<Harness />);
    fireEvent.click(screen.getByRole("button", { name: "Exit" }));
    fireEvent.click(
      within(screen.getByRole("alertdialog")).getByRole("button", {
        name: "Exit",
      }),
    );
    await waitFor(() =>
      expect(useGraphDraftStore.getState().documents[draftKey]?.ai).toBeNull(),
    );
    // Ending the conversation does not undo the work it produced.
    expect(draftDocument().definition.name).toBe("Research and review");
  });
});
