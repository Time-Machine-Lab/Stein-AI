import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import labels from "../locales/en/graphs.json";
import { GraphDetailPage } from "./graph-detail-page";

vi.mock("../i18n", () => ({
  useT: () => ({
    t: (selector: (value: typeof labels) => string) => selector(labels),
  }),
}));
vi.mock("../navigation", () => ({
  useNavigation: () => ({
    pathname: "/acme/graphs/graph-1",
    searchParams: new URLSearchParams(),
  }),
  AppLink: ({ href, children, ...props }: React.ComponentProps<"a">) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ graphs: () => "/acme/graphs" }),
}));
vi.mock("@multica/core/graphs", () => ({
  graphDetailOptions: () => ({
    queryKey: ["graph"],
    queryFn: async () => ({ id: "graph-1", name: "Release" }),
    initialData: { id: "graph-1", name: "Release" },
  }),
}));
vi.mock("./graph-editor", () => ({
  GraphEditor: () => (
    <input aria-label="Draft name" defaultValue="Original draft" />
  ),
}));
vi.mock("./graph-start-form", () => ({
  GraphStartForm: () => <p>Run settings</p>,
}));
vi.mock("./graph-triggers", () => ({
  GraphTriggers: () => <p>Trigger settings</p>,
}));

describe("Graph studio page", () => {
  it("separates run settings while preserving unsaved edits across tabs", () => {
    render(
      <QueryClientProvider client={new QueryClient()}>
        <GraphDetailPage />
      </QueryClientProvider>,
    );
    fireEvent.change(screen.getByLabelText("Draft name"), {
      target: { value: "Unsaved changes" },
    });
    fireEvent.click(screen.getByRole("tab", { name: "Start run" }));
    expect(screen.getByText("Run settings")).toBeVisible();
    expect(screen.getByLabelText("Draft name")).not.toBeVisible();
    fireEvent.click(screen.getByRole("tab", { name: "Workflow" }));
    expect(screen.getByLabelText("Draft name")).toHaveValue("Unsaved changes");
    expect(screen.getByLabelText("Draft name")).toBeVisible();
  });
});
