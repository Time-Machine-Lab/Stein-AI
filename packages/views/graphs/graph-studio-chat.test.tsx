import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createSerialDefinition,
  definitionToAliases,
  encodeGraphBuilderInput,
  graphDraftKey,
  useGraphDraftStore,
  type GraphDefinition,
  type GraphDraftAi,
} from "@multica/core/graphs";
import type {
  Agent,
  ChatMessage,
  MemberWithUser,
  RuntimeDevice,
} from "@multica/core/types";
import labels from "../locales/en/graphs.json";
import { graphBuilderDirectory } from "./graph-ai-actors";
import { GraphStudioChat, type GraphAiTurn } from "./graph-studio-chat";

const mocks = vi.hoisted(() => ({
  messages: [] as ChatMessage[],
  pending: false,
  missing: false,
  transform: undefined as ((content: string) => string) | undefined,
  start: vi.fn(),
  send: vi.fn(async (_content: string) => true),
  destroy: vi.fn(async () => true),
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
  useTimeAgo: () => () => "just now",
}));
vi.mock("../navigation", () => ({
  AppLink: ({ href, children }: { href: string; children: React.ReactNode }) => (
    <a href={href}>{children}</a>
  ),
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    runtimes: () => "/acme/runtimes",
    agentDetail: (id: string) => `/acme/agents/${id}`,
  }),
}));
vi.mock("@multica/core/api", () => ({
  api: {
    listGraphBuilderSessions: async () => [],
    deleteChatSession: vi.fn(),
  },
}));
// The session, the composer and the transcript have their own tests. What is
// under test here is what this column does with what they hand it.
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
    start: mocks.start,
    destroy: mocks.destroy,
    send: mocks.send,
    stop: vi.fn(),
  }),
}));
vi.mock("./graph-actor-picker", () => ({
  GraphActorPicker: ({ label, value }: { label: string; value: string }) => (
    <span>
      {label}={value}
    </span>
  ),
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
          <p key={message.id}>
            {transformContent
              ? transformContent(message.content)
              : message.content}
          </p>
        ))}
      </div>
    );
  },
}));
vi.mock("../chat/components/chat-input", () => ({
  ChatInput: ({
    disabled,
    onSend,
    leftAdornment,
    restoreDraftRequest,
  }: {
    disabled?: boolean;
    onSend: (
      content: string,
      ids: undefined,
      commit: () => void,
    ) => unknown;
    leftAdornment?: React.ReactNode;
    restoreDraftRequest?: { content: string } | null;
  }) => (
    <div>
      {leftAdornment}
      <output aria-label="Composer">{restoreDraftRequest?.content ?? ""}</output>
      <button
        type="button"
        disabled={disabled}
        onClick={() => void onSend("research, then review", undefined, () => {})}
      >
        Send message
      </button>
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
const members = [
  { id: "member-1", name: "Ada" },
] as unknown as MemberWithUser[];

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
const current = createSerialDefinition(
  "Research",
  [step("research", "agent-1")],
  "member-1",
);
const next = createSerialDefinition(
  "Research and review",
  [step("research", "agent-1"), step("review", "agent-2")],
  "member-1",
);

function reply(summary: string, definition: unknown, prose = "Here it is.") {
  return `${prose}\n<graph_draft>${JSON.stringify({ summary, definition })}</graph_draft>`;
}
function aliased(definition: GraphDefinition) {
  return definitionToAliases(definition, directory);
}
function turn(id: string, content: string, role: "user" | "assistant") {
  return { id, role, content } as ChatMessage;
}

const applied: { definition: GraphDefinition; turn: GraphAiTurn }[] = [];
let runtimes: RuntimeDevice[] = [online];

/** The studio's side of the contract: applying a turn is an edit. */
function Harness({ initialMessage }: { initialMessage?: string }) {
  const draft = useGraphDraftStore((state) => state.documents[draftKey]);
  return (
    <QueryClientProvider client={new QueryClient()}>
      <GraphStudioChat
        draftKey={draftKey}
        ai={draft?.ai ?? null}
        definition={draft?.definition ?? current}
        actors={{
          agents,
          runtimes,
          members,
          currentUserId: "user-1",
          loading: false,
        }}
        memberId="member-1"
        disabled={false}
        available
        prefill={null}
        onPrefillApplied={() => undefined}
        initialMessage={initialMessage}
        onApplyTurn={(definition, aiTurn) => {
          applied.push({ definition, turn: aiTurn });
          useGraphDraftStore.getState().edit(draftKey, definition);
        }}
      />
    </QueryClientProvider>
  );
}
const draftDocument = () => useGraphDraftStore.getState().documents[draftKey]!;

beforeEach(() => {
  vi.clearAllMocks();
  window.localStorage.clear();
  useGraphDraftStore.setState({ documents: {} });
  useGraphDraftStore.getState().open(draftKey, current, 0);
  useGraphDraftStore.getState().bindAi(draftKey, ai);
  mocks.messages = [];
  mocks.pending = false;
  mocks.missing = false;
  applied.length = 0;
  runtimes = [online];
});

describe("Graph studio conversation", () => {
  it("merges a reply into the draft and reports what the turn changed", () => {
    mocks.messages = [
      turn(
        "m1",
        encodeGraphBuilderInput("research, then review", current, directory),
        "user",
      ),
      turn("m2", reply("Added a review step", aliased(next)), "assistant"),
    ];
    render(<Harness />);
    expect(draftDocument().definition.name).toBe("Research and review");
    expect(draftDocument().ai?.appliedMessageId).toBe("m2");
    expect(
      draftDocument().definition.nodes.find((node) => node.nodeKey === "review")
        ?.agentId,
    ).toBe("agent-2");
    expect(applied).toHaveLength(1);
    expect(applied[0]!.turn).toMatchObject({
      messageId: "m2",
      added: ["review"],
      summary: "Added a review step",
    });
  });
  it("keeps the merged draft across a reload without merging it again", () => {
    mocks.messages = [
      turn("m2", reply("Added a review step", aliased(next)), "assistant"),
    ];
    const first = render(<Harness />);
    expect(draftDocument().past).toHaveLength(1);
    first.unmount();
    useGraphDraftStore.setState({ documents: {} });
    useGraphDraftStore.getState().open(draftKey, current, 0);
    render(<Harness />);
    expect(draftDocument().definition.name).toBe("Research and review");
    expect(draftDocument().past).toHaveLength(0);
    expect(applied).toHaveLength(1);
  });
  it("leaves a turn that proposes nothing alone", () => {
    mocks.messages = [
      turn("m1", "What should the first step deliver?", "assistant"),
    ];
    render(<Harness />);
    expect(draftDocument().past).toHaveLength(0);
    expect(screen.queryByRole("alert")).toBeNull();
  });
  it("does not judge a reply while it is still being written", () => {
    mocks.pending = true;
    mocks.messages = [
      turn("m1", 'One moment.<graph_draft>{"summary":"x","definit', "assistant"),
    ];
    const view = render(<Harness />);
    expect(screen.queryByRole("alert")).toBeNull();
    expect(draftDocument().ai?.appliedMessageId).toBeNull();
    mocks.pending = false;
    view.rerender(<Harness />);
    expect(screen.getByRole("alert")).toHaveTextContent(
      "The draft in this reply is not valid JSON",
    );
  });
  it("refuses an alias the directory does not offer and names the ones it does", () => {
    const wire = aliased(next) as {
      nodes: { node_key: string; agent_id: string }[];
    };
    wire.nodes.find((node) => node.node_key === "review")!.agent_id = "agent_9";
    mocks.messages = [
      turn("m2", reply("Added a review step", wire), "assistant"),
    ];
    render(<Harness />);
    expect(draftDocument().definition.name).toBe("Research");
    expect(screen.getByRole("alert")).toHaveTextContent("agent_9");
  });
  it("hides the block from the transcript, mid-stream included", () => {
    mocks.messages = [
      turn("m2", 'Here it is.\n<graph_draft>{"definition":{"nodes"', "assistant"),
    ];
    render(<Harness />);
    expect(screen.getByLabelText("Transcript")).toHaveTextContent("Here it is.");
    expect(screen.getByLabelText("Transcript")).not.toHaveTextContent(
      "graph_draft",
    );
  });
  it("holds the conversation while its runtime is offline", () => {
    runtimes = [{ ...online, status: "offline" } as unknown as RuntimeDevice];
    render(<Harness />);
    expect(screen.getByRole("button", { name: "Send message" })).toBeDisabled();
    expect(screen.getByText("Runtime offline")).toBeVisible();
  });
  it("forgets a conversation the server no longer has", () => {
    mocks.missing = true;
    render(<Harness />);
    expect(draftDocument().ai).toBeNull();
  });
  it("starts over but keeps the draft once the server confirms", async () => {
    mocks.messages = [
      turn("m2", reply("Added a review step", aliased(next)), "assistant"),
    ];
    render(<Harness />);
    fireEvent.click(screen.getByRole("button", { name: "New conversation" }));
    fireEvent.click(
      within(screen.getByRole("alertdialog")).getByRole("button", {
        name: "Exit",
      }),
    );
    await waitFor(() => expect(draftDocument().ai).toBeNull());
    expect(draftDocument().definition.name).toBe("Research and review");
  });
});

describe("Graph studio first message", () => {
  beforeEach(() => {
    useGraphDraftStore.getState().clearAi(draftKey);
    useGraphDraftStore.getState().open(draftKey, current, 0);
    mocks.start.mockResolvedValue({
      sessionId: "session-2",
      builderAgentId: "carrier-2",
      runtimeId: "runtime-1",
      model: "claude",
      defaultAgentId: "agent-1",
      approverId: "member-1",
    });
  });
  it("creates the conversation with defaults on the first message, then sends it", async () => {
    render(<Harness />);
    // Nothing is created just by opening the editor.
    expect(mocks.start).not.toHaveBeenCalled();
    expect(screen.getByText("Default executor=agent-1")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Send message" }));
    await waitFor(() =>
      expect(mocks.start).toHaveBeenCalledWith({
        defaultAgentId: "agent-1",
        agentScope: ["agent-1", "agent-2"],
        approverId: "member-1",
      }),
    );
    await waitFor(() =>
      expect(draftDocument().ai?.sessionId).toBe("session-2"),
    );
    await waitFor(() =>
      expect(mocks.send).toHaveBeenCalledWith("research, then review"),
    );
  });
  it("explains why it cannot start and keeps the entry message", () => {
    runtimes = [];
    render(<Harness initialMessage="Summarize the weekly reports" />);
    expect(
      screen.getByText("No runtime is online, so the AI cannot start."),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: "Send message" })).toBeDisabled();
    expect(screen.getByLabelText("Composer")).toHaveTextContent(
      "Summarize the weekly reports",
    );
    expect(mocks.start).not.toHaveBeenCalled();
  });
});
