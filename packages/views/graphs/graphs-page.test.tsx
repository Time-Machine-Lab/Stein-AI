import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import labels from "../locales/en/graphs.json";
import { GraphsPage } from "./graphs-page";

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
  useNavigation: () => ({
    pathname: "/acme/graphs",
    searchParams: new URLSearchParams(),
    push: vi.fn(),
  }),
  AppLink: ({ href, children, ...props }: React.ComponentProps<"a">) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
}));
vi.mock("../layout/collection-page", () => ({
  CollectionPageHeader: ({ title }: { title: string }) => <h1>{title}</h1>,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    graphNew: () => "/acme/graphs/new",
    graphDetail: (id: string) => `/acme/graphs/${id}`,
    graphRun: (id: string) => `/acme/graphs/runs/${id}`,
  }),
}));
const step = (nodeKey: string, type: string) => ({ nodeKey, type });
vi.mock("@multica/core/graphs", async (original) => ({
  ...(await original<object>()),
  graphSearchOptions: () => ({
    queryKey: ["graphs"],
    queryFn: async () => ({}),
    initialData: {
      total: 2,
      items: [
        {
          id: "g1",
          name: "Release",
          revision: 1,
          updatedAt: "2026-01-01T00:00:00Z",
          versions: [{ version: 3 }],
          definition: {
            nodes: [step("start", "start"), step("a", "agent"), step("end", "end")],
          },
        },
        {
          id: "g2",
          name: "Weekly report",
          revision: 1,
          updatedAt: "2026-01-01T00:00:00Z",
          definition: { nodes: [step("start", "start"), step("end", "end")] },
        },
      ],
    },
  }),
  graphRunSearchOptions: () => ({
    queryKey: ["runs"],
    queryFn: async () => ({}),
    initialData: {
      total: 2,
      stats: { waitingApproval: 1 },
      items: [
        {
          id: "r1",
          graphId: "g1",
          state: "succeeded",
          createdAt: "2026-01-01T00:00:00Z",
        },
        {
          id: "r2",
          graphId: "g1",
          state: "waiting_approval",
          createdAt: "2026-01-02T00:00:00Z",
        },
      ],
    },
  }),
}));
vi.mock("./graph-run-list", async (original) => ({
  ...(await original<object>()),
  GraphRunList: () => <p>All runs list</p>,
}));

function page() {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <GraphsPage />
    </QueryClientProvider>,
  );
}

describe("Graphs list", () => {
  it("shows each flow's latest run without opening it", () => {
    page();
    const rows = screen.getAllByRole("listitem");
    const release = rows.find((row) => row.textContent?.includes("Release"))!;
    expect(release).toHaveTextContent(labels.states.waiting_approval);
    expect(release).toHaveTextContent("Published v3");
    expect(release).toHaveTextContent("1 steps");
    expect(
      within(release).getByRole("button", { name: labels.studio.ux.run_once }),
    ).toHaveAttribute("href", "/acme/graphs/g1?run=1");
    const weekly = rows.find((row) =>
      row.textContent?.includes("Weekly report"),
    )!;
    expect(weekly).toHaveTextContent(labels.studio.ux.never_run);
  });
  it("counts runs waiting for approval on the runs tab", () => {
    page();
    const tab = screen.getByRole("tab", {
      name: new RegExp(labels.studio.ux.all_runs_tab),
    });
    expect(tab).toHaveTextContent("1 waiting");
    fireEvent.click(tab);
    expect(screen.getByText("All runs list")).toBeVisible();
  });
});
