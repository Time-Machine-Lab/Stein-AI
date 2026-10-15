import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import labels from "../locales/en/graphs.json";
import { GraphValueView } from "./graph-result-view";

vi.mock("../i18n", () => ({
  useT: () => ({
    // Interpolates like the real `t`, so a count in a button label is asserted
    // as the reader sees it rather than as `{{count}}`.
    t: (
      selector: (value: typeof labels) => string,
      vars?: Record<string, unknown>,
    ) =>
      Object.entries(vars ?? {}).reduce(
        (text, [key, value]) => text.replaceAll(`{{${key}}}`, String(value)),
        selector(labels),
      ),
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
  useWorkspacePaths: () => ({ attachmentPreview: (id: string) => `/a/${id}` }),
}));
// The canonical renderer is Tiptap; these tests are about structure and length,
// so a plain span carries the text faithfully and keeps the run cheap.
vi.mock("../editor/readonly-content", () => ({
  ReadonlyContent: ({ content }: { content: string }) => <span>{content}</span>,
}));

describe("GraphValueView", () => {
  it("reads a report headline-first instead of in the order the agent emitted keys", () => {
    const { container } = render(
      <GraphValueView
        value={{
          sources: ["https://example.com/a"],
          open_gaps: ["one gap"],
          conclusion: "Ship it",
        }}
      />,
    );
    const terms = [...container.querySelectorAll("dt")].map(
      (node) => node.textContent,
    );
    expect(terms).toEqual(["conclusion", "sources", "open gaps"]);
  });

  it("folds a long collection instead of rendering every entry", () => {
    render(
      <GraphValueView
        value={{ sources: Array.from({ length: 57 }, (_, i) => `https://e.com/${i}`) }}
      />,
    );
    expect(screen.getByText("https://e.com/0")).toBeVisible();
    expect(screen.queryByText("https://e.com/9")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /Show all 57/ }));
    expect(screen.getByText("https://e.com/56")).toBeVisible();
  });

  it("renders a uniform list of findings as one table, not one card each", () => {
    const { container } = render(
      <GraphValueView
        value={{
          findings: [
            { claim: "A", evidence: "e1" },
            { claim: "B", evidence: "e2" },
            { claim: "C", evidence: "e3" },
          ],
        }}
      />,
    );
    expect(container.querySelectorAll("table")).toHaveLength(1);
    expect(container.querySelectorAll("thead th")).toHaveLength(2);
    expect(container.querySelectorAll("tbody tr")).toHaveLength(3);
  });

  it("links only URLs a browser may safely follow", () => {
    const { container } = render(
      <GraphValueView
        value={{ a: "https://example.com/x", b: "javascript:alert(1)" }}
      />,
    );
    const links = [...container.querySelectorAll("a")];
    expect(links.map((link) => link.getAttribute("href"))).toEqual([
      "https://example.com/x",
    ]);
    expect(container.textContent).toContain("javascript:alert(1)");
  });

  it("does not lose a key that first appears in a folded-away row", () => {
    const items = Array.from({ length: 20 }, (_, i) => ({
      claim: `c${i}`,
      ...(i === 19 ? { late_key: "found" } : {}),
    }));
    const { container } = render(<GraphValueView value={{ findings: items }} />);
    const columns = [...container.querySelectorAll("thead th")].map(
      (node) => node.textContent,
    );
    expect(columns).toContain("late key");
  });

  it("empties render as a placeholder rather than an empty frame", () => {
    const { container } = render(<GraphValueView value={{ a: [], b: {} }} />);
    expect(container.querySelectorAll("table")).toHaveLength(0);
    expect(container.textContent).toContain("—");
  });
});
