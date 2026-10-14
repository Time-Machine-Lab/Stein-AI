import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { ApiError } from "@multica/core/api/client";
import type { GraphGenerationMode, GraphGenerationView } from "@multica/core/graphs";
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
  const handlers = { onGenerate: vi.fn(), onCancel: vi.fn(), onManual: vi.fn() };
  const props = {
    prompt: "Research our release",
    onPromptChange: vi.fn(),
    mode: "runtime" as GraphGenerationMode,
    scopeIds: ["agent-1", "agent-2"],
    onScopeChange: vi.fn(),
    defaultAgentId: "agent-1",
    onDefaultAgentChange: vi.fn(),
    approverId: "member-1",
    onApproverChange: vi.fn(),
    agents,
    members,
    loading: false,
    pending: false,
    job: null as GraphGenerationView | null,
    error: undefined as unknown,
    hasDraft: false,
    ...handlers,
    ...overrides,
  };
  render(<GraphAiComposer {...props} />);
  return { ...handlers, props };
}

function jobView(overrides: Record<string, unknown>) {
  return {
    id: "gen-1",
    status: "queued",
    agentId: "agent-1",
    submissions: 0,
    createdAt: "",
    updatedAt: "",
    ...overrides,
  } as GraphGenerationView;
}

const generate = () => screen.getByRole("button", { name: "Generate workflow" });
// A locked composer swaps the label for the in-flight one, so the button is
// still the same affordance — just not the one the idle state advertises.
const busy = () => screen.getByRole("button", { name: "Generating…" });

describe("GraphAiComposer authoring modes", () => {
  it("asks for a scope, a default executor, and an approver on the runtime path", () => {
    composer();
    expect(
      screen.getByRole("combobox", { name: "Participating agents" }),
    ).toHaveTextContent("BuilderReviewer");
    expect(
      screen.getByRole("combobox", { name: "Default executor" }),
    ).toHaveTextContent("Builder");
    expect(
      screen.getByRole("combobox", { name: "Final approver" }),
    ).toHaveTextContent("Ada");
    expect(screen.getByText(/uses its quota/)).toBeVisible();
  });

  it("keeps a single agent picker on the legacy path and hides the runtime copy", () => {
    composer({ mode: "legacy", scopeIds: ["agent-1"] });
    expect(
      screen.getByRole("combobox", { name: "Workflow agent" }),
    ).toHaveTextContent("Builder");
    expect(
      screen.queryByRole("combobox", { name: "Participating agents" }),
    ).toBeNull();
    expect(screen.queryByText(/uses its quota/)).toBeNull();
  });

  it.each([
    ["no scope", { scopeIds: [] }],
    ["no default executor", { defaultAgentId: "" }],
    [
      "a default executor outside the scope",
      { defaultAgentId: "agent-9", scopeIds: ["agent-1"] },
    ],
    ["no approver", { approverId: "" }],
    ["an empty description", { prompt: "" }],
    ["runtime authoring this server cannot serve", { mode: "unavailable" }],
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
  });

  it("still offers manual authoring while runtime authoring is unavailable", () => {
    const { onManual } = composer({ mode: "unavailable" });
    expect(generate()).toBeDisabled();
    expect(
      screen.getByText(/has not been configured on this server/),
    ).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Build manually" }));
    expect(onManual).toHaveBeenCalledOnce();
  });
});

describe("GraphAiComposer task states", () => {
  it("reports a queued task and offers to cancel it", () => {
    const { onCancel } = composer({ job: jobView({ status: "queued" }) });
    expect(screen.getByText(/Waiting for the agent's runtime/)).toBeVisible();
    expect(busy()).toBeDisabled();
    expect(screen.getByLabelText("What should this workflow do?")).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onCancel).toHaveBeenCalledOnce();
  });

  it("reports a running task without repeating the queued copy", () => {
    composer({ job: jobView({ status: "running" }) });
    expect(screen.getByText(/drafting the workflow/)).toBeVisible();
    expect(screen.queryByText(/Waiting for the agent's runtime/)).toBeNull();
  });

  it.each([
    ["generation_timeout", /Generation took too long/],
    ["generation_invalid", /could not produce a valid workflow/],
    ["generation_failed", /AI generation failed/],
    ["capability_missing", /cannot draft workflows yet/],
    ["agent_unavailable", /selected agent is unavailable/],
    ["forbidden", /do not have permission/],
  ])("explains a %s failure from the task", (code, copy) => {
    composer({
      job: jobView({ status: "failed", error: { code, message: "raw" } }),
    });
    expect(screen.getByRole("alert")).toHaveTextContent(copy);
    expect(screen.getByLabelText("What should this workflow do?")).toBeEnabled();
  });

  it("prefers the request error over the task error and adds the retry hint when busy", () => {
    composer({
      error: new ApiError("Busy", 429, "Too Many Requests", {
        code: "generation_busy",
      }),
      job: jobView({
        status: "failed",
        error: { code: "generation_failed", message: "older" },
      }),
    });
    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent(/Another draft is being generated/);
    expect(alert).toHaveTextContent(/Your description is kept/);
  });

  it("falls back to the server's own message for a code this build does not know", () => {
    composer({
      job: jobView({
        status: "failed",
        error: { code: "generation_quota", message: "Out of quota" },
      }),
    });
    expect(screen.getByRole("alert")).toHaveTextContent("Out of quota");
  });

  it("reports a cancelled task neutrally without an alert", () => {
    composer({ job: jobView({ status: "cancelled" }) });
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.getByText(/Generation cancelled/)).toBeVisible();
    expect(generate()).toBeEnabled();
  });

  it("says nothing about failures while everything is quiet", () => {
    composer();
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.queryByText(/Generation cancelled/)).toBeNull();
  });
});
