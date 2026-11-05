import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import labels from "../locales/en/graphs.json";
import { GraphRunList } from "./graph-run-list";

const calls = vi.hoisted(() => [] as Record<string, unknown>[]);

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
  useTimeAgo: () => () => "2m ago",
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
    graphRun: (id: string) => `/acme/graphs/runs/${id}`,
  }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({
    queryKey: ["members"],
    queryFn: async () => [],
    initialData: [
      { id: "member-1", user_id: "user-1", name: "Ada", email: "a@x.io" },
    ],
  }),
}));
const now = new Date().toISOString();
vi.mock("@multica/core/graphs", async (original) => ({
  ...(await original<object>()),
  graphRunSearchOptions: (_wsId: string, params: Record<string, unknown>) => {
    calls.push(params);
    return {
      queryKey: ["runs", params],
      queryFn: async () => ({}),
      initialData: {
        total: 2,
        stats: {
          total: 2,
          active: 0,
          waitingApproval: 0,
          blocked: 0,
          succeeded: 1,
          failed: 1,
          rejected: 0,
          cancelled: 0,
          totalDurationMs: 90_000,
          totalCostUsd: 0.5,
          totalTokens: 1200,
        },
        items: [
          {
            id: "run-aaaaaaaa1",
            graphName: "Release",
            state: "succeeded",
            initiatorId: "user-1",
            rootTitle: "Ship v2",
            createdAt: now,
            updatedAt: now,
            finishedAt: new Date(Date.parse(now) + 65_000).toISOString(),
          },
          {
            id: "run-bbbbbbbb2",
            graphName: "Release",
            state: "failed",
            initiatorId: "",
            blockReason: "Runtime offline",
            createdAt: now,
            updatedAt: now,
          },
        ],
      },
    };
  },
}));

function list(props: Partial<React.ComponentProps<typeof GraphRunList>> = {}) {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <GraphRunList wsId="ws-1" {...props} />
    </QueryClientProvider>,
  );
}

describe("Graph run list", () => {
  it("shows outcome, issue, initiator and duration on each row", () => {
    list();
    expect(screen.getByText(labels.studio.ux.today)).toBeVisible();
    const [ok, failed] = screen.getAllByRole("link");
    expect(ok).toHaveAttribute("href", "/acme/graphs/runs/run-aaaaaaaa1");
    expect(ok).toHaveTextContent("For: Ship v2");
    expect(ok).toHaveTextContent("Ada");
    expect(ok).toHaveTextContent("1m 5s");
    expect(within(failed!).getByText("Runtime offline")).toBeVisible();
    expect(failed).toHaveTextContent(labels.studio.ux.started_by_trigger);
  });
  it("summarises the set and hides empty state filters", () => {
    list();
    expect(screen.getByText("50%")).toBeVisible();
    expect(
      screen.queryByRole("button", { name: /Awaiting approval/ }),
    ).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /Failed/ }));
    expect(calls.some((params) => params.state === "failed")).toBe(true);
  });
});
