import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { GraphRun } from "@multica/core/graphs";
import labels from "../locales/en/graphs.json";
import { GraphRunArtifacts, GraphRunEvents } from "./graph-run-record";

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
}));
vi.mock("../navigation", () => ({
  AppLink: ({ href, children, ...props }: React.ComponentProps<"a">) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
}));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    attachmentPreview: (id: string) => `/acme/attachments/${id}/preview`,
    issueDetail: (id: string) => `/acme/issues/${id}`,
  }),
}));
vi.mock("@multica/core/api", () => ({
  api: { getAttachmentBlob: vi.fn(() => new Promise(() => {})) },
}));
vi.mock("../common/task-transcript", () => ({ TranscriptButton: () => null }));
vi.mock("../editor/readonly-content", () => ({
  ReadonlyContent: ({ content }: { content: string }) => <span>{content}</span>,
}));

const research = {
  id: "node-1",
  nodeKey: "research",
  label: "Research",
  nodeType: "agent",
  state: "succeeded",
  scopeId: "root",
  input: {},
  inputHash: "",
  maxAttempts: 2,
  grantedAttempts: 0,
  canRetry: false,
  canResolveBlock: false,
  attempts: [
    { taskId: "task-1", attemptNo: 1, generation: 0, state: "succeeded", manifest: {} },
  ],
  submissions: [],
};
const run = {
  id: "run-1",
  nodes: [research],
  scopes: [{ id: "root" }],
  events: [
    { id: "e1", type: "scope.started", message: "root", created_at: "2026-01-01T00:00:00Z" },
    { id: "e2", type: "run.started", actor_id: "user-1", created_at: "2026-01-01T00:00:00Z" },
    { id: "e3", type: "attempt.created", message: "task-1", created_at: "2026-01-01T00:00:01Z" },
    { id: "e4", type: "approval.vote", message: "member-1:approved", created_at: "2026-01-01T00:01:00Z" },
    { id: "e5", type: "run.pause", actor_id: "user-1", message: "Waiting on design", created_at: "2026-01-01T00:02:00Z" },
  ],
  artifacts: [
    { id: "a1", type: "attachment_ref", media_type: "image/png", size: 2048, producer_task_id: "task-1" },
    { id: "a2", type: "attachment_ref", media_type: "application/zip", size: 4096, producer_task_id: "task-1" },
  ],
} as unknown as GraphRun;

function wrap(node: React.ReactNode) {
  return render(<QueryClientProvider client={new QueryClient()}>{node}</QueryClientProvider>);
}

describe("Run events", () => {
  it("tells the history in sentences with the step and the person", () => {
    wrap(
      <GraphRunEvents
        run={run}
        actorNames={{ "user-1": "Ada", "member-1": "Grace" }}
      />,
    );
    expect(screen.getByText("Run started")).toBeVisible();
    expect(screen.getByText("Started Research")).toBeVisible();
    expect(screen.getByText("Grace approved")).toBeVisible();
    expect(screen.getByText("Ada paused the run")).toBeVisible();
    expect(screen.getByText("Waiting on design")).toBeVisible();
    // The root scope opening is the run starting, not a separate event.
    expect(screen.queryByText(labels.studio.ux.ev_scope_started)).toBeNull();
  });
});

describe("Run artifacts", () => {
  it("separates images from files and names the step that made them", () => {
    wrap(<GraphRunArtifacts run={run} repositories={[]} />);
    expect(screen.getByText("Images · 1")).toBeVisible();
    expect(screen.getByText("Files and commits · 1")).toBeVisible();
    expect(screen.getAllByText(/Research/).length).toBe(2);
    expect(screen.getByText("PNG · 2 KB")).toBeVisible();
  });
});
