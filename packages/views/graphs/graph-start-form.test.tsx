import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { GraphDefinition, GraphRecord } from "@multica/core/graphs";
import labels from "../locales/en/graphs.json";
import { GraphStartForm } from "./graph-start-form";

const mocks = vi.hoisted(() => ({
  start: vi.fn(),
  push: vi.fn(),
  commits: vi.fn(),
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
    initialData: [{ id: "agent", name: "DeepSeek", runtime_id: "claude" }],
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
        metadata: { capabilities: ["graph-execution-v2"] },
      },
      {
        id: "offline",
        name: "Offline Claude",
        provider: "claude",
        status: "offline",
        runtime_mode: "local",
        metadata: { capabilities: ["graph-execution-v2"] },
      },
      {
        id: "legacy",
        name: "Legacy Claude",
        provider: "claude",
        status: "online",
        runtime_mode: "local",
        metadata: { capabilities: ["graph-execution-v1"] },
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
      <GraphStartForm wsId="ws" graph={graph} onConfigure={onConfigure} />
    </QueryClientProvider>,
  );
}
beforeEach(() => {
  vi.clearAllMocks();
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
    expect(
      screen.getByRole("option", { name: "Local Claude Code" }),
    ).toBeInTheDocument();
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
});
