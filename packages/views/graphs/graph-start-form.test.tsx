import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { GraphDefinition, GraphRecord } from "@multica/core/graphs";
import { ApiError } from "@multica/core/api";
import labels from "../locales/en/graphs.json";
import { GraphStartForm } from "./graph-start-form";

const mocks = vi.hoisted(() => ({
  start: vi.fn(),
  push: vi.fn(),
  commits: vi.fn(),
  agentRuntimeId: "claude",
  agentArchived: false,
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
}));
vi.mock("../navigation", () => ({
  useNavigation: () => ({
    searchParams: new URLSearchParams(),
    push: mocks.push,
  }),
  AppLink: ({ children }: { children: React.ReactNode }) => (
    <span>{children}</span>
  ),
}));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ graphRun: (id: string) => `/runs/${id}` }),
}));
vi.mock("./graph-resource-picker", () => ({
  GraphResourcePicker: ({
    id,
    value,
    onChange,
    options,
    disabled,
  }: {
    id: string;
    value: string;
    onChange: (value: string) => void;
    options: { value: string; label: string }[];
    disabled?: boolean;
  }) => (
    <select
      id={id}
      value={value}
      disabled={disabled}
      onChange={(event) => onChange(event.target.value)}
    >
      <option value="" />
      {options.map((option) => (
        <option key={option.value} value={option.value}>
          {option.label}
        </option>
      ))}
    </select>
  ),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({
    queryKey: ["agents"],
    queryFn: async () => [],
    initialData: [
      {
        id: "agent",
        name: "DeepSeek",
        runtime_id: mocks.agentRuntimeId,
        archived_at: mocks.agentArchived ? "2026-10-17T00:00:00Z" : null,
      },
    ],
  }),
}));
vi.mock("@multica/core/runtimes", async (original) => ({
  ...(await original<object>()),
  runtimeListOptions: () => ({
    queryKey: ["runtimes"],
    queryFn: async () => [],
    initialData: [
      {
        id: "claude",
        name: "Local Claude Code",
        provider: "claude",
        status: "online",
        runtime_mode: "local",
        metadata: {
          capabilities: ["graph-execution-v2", "graph-file-delivery-v1"],
        },
        daemon_id: "machine-1",
        device_info: "WIN-QGQL8EFDVAN · windows-amd64",
      },
      {
        id: "offline",
        name: "Offline Claude",
        provider: "claude",
        status: "offline",
        runtime_mode: "local",
        metadata: {
          capabilities: ["graph-execution-v2", "graph-file-delivery-v1"],
        },
      },
      {
        id: "legacy",
        name: "Legacy Claude",
        provider: "claude",
        status: "online",
        runtime_mode: "local",
        metadata: { capabilities: ["graph-execution-v1"] },
        daemon_id: "machine-2",
        device_info: "DESKTOP-RN3FB79 · windows-amd64",
      },
    ],
  }),
}));
vi.mock("@multica/core/issues/queries", () => ({
  issueDetailOptions: (_: string, id: string) => ({
    queryKey: ["issue", id],
    queryFn: async () => null,
    enabled: false,
  }),
}));
vi.mock("@multica/core/graphs", async (original) => ({
  ...(await original<object>()),
  useStartGraphRun: () => ({ mutateAsync: mocks.start, isPending: false }),
  graphRunOptions: () => ({
    queryKey: ["previous"],
    queryFn: async () => null,
    enabled: false,
  }),
  graphStartOptions: () => ({
    queryKey: ["repositories"],
    queryFn: async () => ({ repositories: [] }),
    initialData: {
      repositories: [
        {
          id: "repo",
          name: "GitLab production",
          provider: "gitlab",
          url: "https://gitlab.example/repo",
        },
      ],
    },
  }),
  graphCommitsOptions: (_: string, id: string) => ({
    queryKey: ["commits", id],
    queryFn: mocks.commits,
    enabled: !!id,
  }),
  graphIssueSearchOptions: () => ({
    queryKey: ["issues"],
    queryFn: async () => ({ issues: [] }),
    initialData: {
      issues: [
        { id: "issue", identifier: "TML-42", title: "Root acceptance" },
        { id: "other", identifier: "TML-43", title: "Another project" },
      ],
    },
  }),
}));
const schema = {
  type: "object",
  properties: {
    requirements: { type: "string", title: "Requirements", minLength: 1 },
  },
  required: ["requirements"],
  additionalProperties: false,
};
const script = {
  nodeKey: "check",
  type: "script",
  agentId: "agent",
  script: { language: "javascript", code: "return input", timeoutSeconds: 10 },
  inputs: {},
  outputSchema: { type: "object" },
};
const definition: GraphDefinition = {
  schemaVersion: 2,
  name: "Nested workflow",
  inputSchema: schema,
  nodes: [
    {
      nodeKey: "loop",
      type: "loop",
      loop: {
        body: {
          schemaVersion: 2,
          name: "Body",
          inputSchema: {},
          nodes: [script],
          edges: [],
        },
        maxIterations: 2,
        condition: { op: "exists", left: { source: "run_input", path: "" } },
        next: {},
      },
    },
  ],
  edges: [],
};
function renderForm(
  d = definition,
  onConfigure?: (request: unknown) => Promise<void>,
  runtimeBindings?: Record<string, string>,
) {
  const graph = {
    id: "graph",
    workspaceId: "ws",
    name: d.name,
    revision: 1,
    definition: d,
    versions: [
      {
        id: "version",
        graphId: "graph",
        version: 1,
        definition: d,
        contentHash: "hash",
      },
    ],
  } as GraphRecord;
  return render(
    <QueryClientProvider
      client={
        new QueryClient({
          defaultOptions: { queries: { staleTime: Infinity, retry: false } },
        })
      }
    >
      <GraphStartForm
        wsId="ws"
        graph={graph}
        onConfigure={onConfigure}
        configuration={runtimeBindings ? { runtimeBindings } : undefined}
      />
    </QueryClientProvider>,
  );
}
beforeEach(() => {
  vi.clearAllMocks();
  mocks.agentRuntimeId = "claude";
  mocks.agentArchived = false;
  mocks.start.mockResolvedValue({ id: "run" });
  mocks.commits.mockResolvedValue({ commits: [] });
});
describe("Graph start execution settings", () => {
  it("binds a nested script to the existing Claude V2 runtime and submits schema values", async () => {
    const configure = vi.fn().mockResolvedValue(undefined);
    renderForm(definition, configure);
    fireEvent.change(screen.getByLabelText("Requirements *"), {
      target: { value: "Verify without modifying main" },
    });
    expect(screen.getAllByText(/Local Claude Code/)[0]).toBeInTheDocument();
    expect(
      screen.queryByRole("option", { name: "Offline Claude" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("option", { name: "Legacy Claude" }),
    ).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: labels.start.submit }));
    await waitFor(() =>
      expect(configure).toHaveBeenCalledWith(
        expect.objectContaining({
          versionId: "version",
          input: { requirements: "Verify without modifying main" },
          runtimeBindings: { "loop/body/check": "claude" },
        }),
      ),
    );
    expect(mocks.start).not.toHaveBeenCalled();
  });
  it("uses the agent binding instead of offering a conflicting runtime override", async () => {
    const configure = vi.fn().mockResolvedValue(undefined);
    renderForm(definition, configure, { "loop/body/check": "legacy" });
    expect(
      screen.queryByRole("combobox", { name: /Runtime for/ }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByText(/WIN-QGQL8EFDVAN · Local Claude Code/),
    ).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Requirements *"), {
      target: { value: "Check" },
    });
    fireEvent.click(screen.getByRole("button", { name: labels.start.submit }));
    await waitFor(() =>
      expect(configure).toHaveBeenCalledWith(
        expect.objectContaining({
          runtimeBindings: { "loop/body/check": "claude" },
        }),
      ),
    );
  });
  it("keeps both computers visible and explains why an online runtime cannot execute Graph", () => {
    mocks.agentRuntimeId = "";
    renderForm();
    const legacy = screen.getByRole("option", {
      name: /Legacy Claude.*Update Multica/,
    });
    expect(legacy).toBeDisabled();
    expect(
      screen
        .getAllByRole("group")
        .some((group) => group.getAttribute("label") === "DESKTOP-RN3FB79"),
    ).toBe(true);
    expect(
      screen
        .getAllByRole("group")
        .some((group) => group.getAttribute("label") === "WIN-QGQL8EFDVAN"),
    ).toBe(true);
    expect(
      screen.getByRole("option", { name: /Offline Claude.*Offline/ }),
    ).toBeDisabled();
  });
  it("names the affected node and agent when its bound runtime needs an update", () => {
    mocks.agentRuntimeId = "legacy";
    renderForm();
    expect(screen.getByRole("alert")).toHaveTextContent(
      /loop\/body\/check.*DeepSeek.*Update Multica/,
    );
    expect(
      screen.getByRole("button", { name: labels.start.submit }),
    ).toBeDisabled();
  });
  it("localizes a server-side binding change with its node and agent", async () => {
    const configure = vi.fn().mockRejectedValue(
      new ApiError("runtime differs from the agent binding", 409, "Conflict", {
        code: "runtime_mismatch",
        node_path: "loop/body/check",
        node_label: "Review design",
        agent_name: "UI reviewer",
      }),
    );
    renderForm(definition, configure);
    fireEvent.change(screen.getByLabelText("Requirements *"), {
      target: { value: "Check" },
    });
    fireEvent.click(screen.getByRole("button", { name: labels.start.submit }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /Review design.*UI reviewer.*Refresh/,
    );
  });
  it.each([
    ["offline", /Offline/],
    ["deleted-runtime", /Runtime unavailable/],
  ])("blocks a bound runtime that is unavailable: %s", (runtimeId, reason) => {
    mocks.agentRuntimeId = runtimeId;
    renderForm();
    expect(screen.getByRole("alert")).toHaveTextContent(reason);
    expect(
      screen.queryByRole("combobox", { name: /Runtime for/ }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: labels.start.submit }),
    ).toBeDisabled();
  });
  it("blocks an archived agent before submitting", () => {
    mocks.agentArchived = true;
    renderForm();
    expect(screen.getByRole("alert")).toHaveTextContent(
      /DeepSeek.*Agent unavailable/,
    );
    expect(
      screen.getByRole("button", { name: labels.start.submit }),
    ).toBeDisabled();
  });
  it("explains that a runtime change requires a new published version", async () => {
    const configure = vi.fn().mockRejectedValue(
      new ApiError("runtime changed", 409, "Conflict", {
        code: "runtime_snapshot_changed",
        node_path: "loop/body/check",
        node_label: "Review design",
        agent_name: "UI reviewer",
      }),
    );
    renderForm(definition, configure);
    fireEvent.change(screen.getByLabelText("Requirements *"), {
      target: { value: "Check" },
    });
    fireEvent.click(screen.getByRole("button", { name: labels.start.submit }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /Review design.*UI reviewer.*Publish a new version/,
    );
  });
  it("rejects invalid input before starting or navigating", async () => {
    renderForm();
    fireEvent.change(screen.getByLabelText(labels.start.issue), {
      target: { value: "issue" },
    });
    fireEvent.click(screen.getByRole("button", { name: labels.start.submit }));
    expect(await screen.findByRole("alert")).toHaveTextContent("requirements");
    expect(mocks.start).not.toHaveBeenCalled();
    expect(mocks.push).not.toHaveBeenCalled();
  });
  it("uses a manual SHA for non-GitHub repositories and clears it when the parent changes", async () => {
    const code = {
      ...definition,
      nodes: [
        {
          nodeKey: "work",
          type: "agent",
          agentId: "agent",
          executionMode: "produce_commit",
          instructions: "test",
        },
      ],
    };
    renderForm(code);
    fireEvent.change(screen.getByLabelText(labels.start.issue), {
      target: { value: "issue" },
    });
    fireEvent.change(screen.getByLabelText(labels.start.repository), {
      target: { value: "repo" },
    });
    const sha = screen.getByLabelText(labels.start.base_sha);
    expect(sha.tagName).toBe("INPUT");
    expect(
      screen.getByText(labels.production.manual_sha_help),
    ).toBeInTheDocument();
    fireEvent.change(sha, { target: { value: "a".repeat(40) } });
    fireEvent.change(screen.getByLabelText("Requirements *"), {
      target: { value: "Check" },
    });
    fireEvent.click(screen.getByRole("button", { name: labels.start.submit }));
    await waitFor(() =>
      expect(mocks.start).toHaveBeenCalledWith(
        expect.objectContaining({
          repository: {
            repositoryId: "repo",
            baseSha: "a".repeat(40),
            commitSha: "a".repeat(40),
          },
        }),
      ),
    );
    expect(mocks.commits).not.toHaveBeenCalled();
    fireEvent.change(screen.getByLabelText(labels.start.issue), {
      target: { value: "other" },
    });
    expect(screen.getByLabelText(labels.start.repository)).toHaveValue("");
  });

  it("does not require a Git repository for file bundle delivery", () => {
    renderForm({
      ...definition,
      nodes: [
        {
          nodeKey: "work",
          type: "agent",
          agentId: "agent",
          executionMode: "produce_files",
          instructions: "Build files",
        },
      ],
    });
    expect(
      screen.queryByLabelText(labels.start.repository),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByLabelText(labels.start.base_sha),
    ).not.toBeInTheDocument();
  });
});
