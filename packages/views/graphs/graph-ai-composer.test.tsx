import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { ApiError } from "@multica/core/api/client";
import type { GraphGenerationMode } from "@multica/core/graphs";
import labels from "../locales/en/graphs.json";
import issueLabels from "../locales/en/issues.json";
import { GraphAiComposer } from "./graph-ai-composer";

vi.mock("../i18n", () => ({
  useT: (namespace: string) => ({
    t: (
      selector: (value: unknown) => string,
      options?: Record<string, unknown>,
    ) => {
      let text = selector(namespace === "issues" ? issueLabels : labels);
      for (const [key, value] of Object.entries(options ?? {}))
        text = text.replace(`{{${key}}}`, String(value));
      return text;
    },
  }),
}));

const agents = [
  {
    id: "agent-1",
    name: "Builder",
  },
  {
    id: "agent-2",
    name: "Reviewer",
  },
] as never;
const members = [{ id: "member-1", name: "Ada", email: "ada@example.com" }] as never;

function composer(overrides: Record<string, unknown> = {}) {
  const handlers = { onGenerate: vi.fn(), onManual: vi.fn(), onPromptChange: vi.fn() };
  const props = {
    prompt: "Research our release",
    defaultAgentId: "agent-1",
    onDefaultAgentChange: vi.fn(),
    approverId: "member-1",
    onApproverChange: vi.fn(),
    mode: "legacy" as GraphGenerationMode,
    agents,
    members,
    loading: false,
    pending: false,
    error: undefined as unknown,
    hasDraft: false,
    ...handlers,
    ...overrides,
  };
  render(<GraphAiComposer {...props} />);
  return { ...handlers, props };
}

const generate = () => screen.getByRole("button", { name: "Generate workflow" });
// A locked composer swaps the label for the in-flight one, so the button is
// still the same affordance — just not the one the idle state advertises.
const busy = () => screen.getByRole("button", { name: "Generating…" });

describe("GraphAiComposer one-shot form", () => {
  it("asks for one agent and one approver", () => {
    composer();
    expect(
      screen.getByRole("combobox", { name: "Workflow agent" }),
    ).toHaveTextContent("Builder");
    expect(
      screen.getByRole("combobox", { name: "Final approver" }),
    ).toHaveTextContent("Ada");
  });

  it("no longer asks for a participating scope or a runtime quota", () => {
    // Both belonged to the runtime pipeline: it needed a scope to hand the
    // model, and it spent a submission budget that this path never had.
    composer();
    expect(
      screen.queryByRole("combobox", { name: "Participating agents" }),
    ).toBeNull();
    expect(screen.queryByText(/uses its quota/)).toBeNull();
    expect(screen.queryByText(/may use only these agents/)).toBeNull();
  });

  it.each([
    ["no agent", { defaultAgentId: "" }],
    ["no approver", { approverId: "" }],
    ["an empty description", { prompt: "" }],
    ["AI authoring this server cannot serve", { mode: "unavailable" }],
    ["the editor withholding permission", { disabled: true }],
    ["generation options still loading", { loading: true }],
  ])("withholds Generate with %s", (_label, overrides) => {
    composer(overrides);
    expect(generate()).toBeDisabled();
  });

  it("swaps Generate for an in-flight label while a request is pending", () => {
    composer({ pending: true });
    expect(busy()).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Generate workflow" })).toBeNull();
    expect(screen.getByLabelText("What should this workflow do?")).toBeDisabled();
  });

  it("fills the description from an example instead of sending it", () => {
    const { onPromptChange, onGenerate } = composer({ prompt: "" });
    fireEvent.click(
      screen.getByRole("button", {
        name: "Research a topic, summarize findings, then request my approval.",
      }),
    );
    expect(onPromptChange).toHaveBeenCalledWith(
      "Research a topic, summarize findings, then request my approval.",
    );
    expect(onGenerate).not.toHaveBeenCalled();
  });

  it("still offers manual authoring while AI authoring is unavailable", () => {
    const { onManual } = composer({ mode: "unavailable" });
    expect(generate()).toBeDisabled();
    expect(
      screen.getByText(/has not been configured on this server/),
    ).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Build manually" }));
    expect(onManual).toHaveBeenCalledOnce();
  });
});

describe("GraphAiComposer failure reporting", () => {
  it.each([
    ["generation_timeout", /Generation took too long/],
    ["generation_invalid", /could not produce a valid workflow/],
    ["generation_failed", /AI generation failed/],
    ["capability_missing", /cannot draft workflows yet/],
    ["agent_unavailable", /selected agent is unavailable/],
    ["forbidden", /do not have permission/],
  ])("explains a %s rejection", (code, copy) => {
    composer({ error: new ApiError("raw", 400, "Bad Request", { code }) });
    expect(screen.getByRole("alert")).toHaveTextContent(copy);
    expect(screen.getByLabelText("What should this workflow do?")).toBeEnabled();
  });

  it("adds the retry hint when the server is busy", () => {
    composer({
      error: new ApiError("Busy", 429, "Too Many Requests", {
        code: "generation_busy",
      }),
    });
    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent(/Another draft is being generated/);
    expect(alert).toHaveTextContent(/Your description is kept/);
  });

  it("falls back to the server's own message for a code this build does not know", () => {
    composer({
      error: new ApiError("Out of quota", 400, "Bad Request", {
        code: "generation_quota",
      }),
    });
    expect(screen.getByRole("alert")).toHaveTextContent("Out of quota");
  });

  it("says nothing about failures while everything is quiet", () => {
    composer();
    expect(screen.queryByRole("alert")).toBeNull();
  });
});
