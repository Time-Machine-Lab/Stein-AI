import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createSerialDefinition, exportGraphDefinition, type GraphRecord } from "@multica/core/graphs";
import labels from "../locales/en/graphs.json";
import { GraphEditor } from "./graph-editor";

const mocks = vi.hoisted(() => ({ save: vi.fn(), replace: vi.fn(), actor: { userId: "user-1", role: "admin", memberId: "member-1" } }));

vi.mock("../i18n", () => ({ useT: () => ({ t: (selector: (value: typeof labels) => string, options?: Record<string, unknown>) => {
  let text = selector(labels);
  for (const [key, value] of Object.entries(options ?? {})) text = text.replace(`{{${key}}}`, String(value));
  return text;
} }) }));
vi.mock("../navigation", () => ({ useNavigation: () => ({ replace: mocks.replace }) }));
vi.mock("@multica/core/paths", () => ({ useWorkspacePaths: () => ({ graphDetail: (id: string) => `/acme/graphs/${id}` }) }));
vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"], queryFn: async () => [{ id: "agent-1", name: "Builder" }], initialData: [{ id: "agent-1", name: "Builder" }] }),
  memberListOptions: () => ({ queryKey: ["members"], queryFn: async () => [{ id: "member-1", name: "Ada" }], initialData: [{ id: "member-1", name: "Ada" }] }),
}));
vi.mock("./shared", async (original) => ({ ...await original<object>(), useGraphActor: () => mocks.actor }));
vi.mock("@multica/core/graphs", async (original) => ({
  ...await original<object>(),
  useSaveGraph: () => ({ mutateAsync: mocks.save, isPending: false }),
  useValidateGraph: () => ({ mutate: vi.fn(), reset: vi.fn(), isPending: false }),
  usePublishGraph: () => ({ mutate: vi.fn(), reset: vi.fn(), isPending: false }),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn() } }));

function editor(graph?: GraphRecord) {
  return render(<QueryClientProvider client={new QueryClient()}><GraphEditor wsId="ws-1" graph={graph} /></QueryClientProvider>);
}

beforeEach(() => { vi.clearAllMocks(); mocks.actor.role = "admin"; mocks.save.mockResolvedValue({ id: "graph-1", revision: 1 }); });

describe("Graph step editor", () => {
  it("creates a serial definition from the step form and waits before navigating", async () => {
    editor();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Release" } });
    fireEvent.change(screen.getByLabelText("Agent"), { target: { value: "agent-1" } });
    fireEvent.change(screen.getByLabelText("Instructions"), { target: { value: "Build and verify" } });
    fireEvent.change(screen.getByLabelText("Final approver"), { target: { value: "member-1" } });
    fireEvent.click(screen.getByRole("button", { name: "Save draft" }));
    await waitFor(() => expect(mocks.save).toHaveBeenCalledOnce());
    expect(mocks.save.mock.calls[0]![0].definition.nodes.map((node: { type: string }) => node.type)).toEqual(["start", "agent", "approval", "end"]);
    expect(mocks.save.mock.calls[0]![0].definition.nodes[1].agentId).toBe("agent-1");
    await waitFor(() => expect(mocks.replace).toHaveBeenCalledWith("/acme/graphs/graph-1"));
  });

  it("rejects malformed binding JSON without saving or navigating", async () => {
    editor();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Release" } });
    fireEvent.change(screen.getByLabelText("Agent"), { target: { value: "agent-1" } });
    fireEvent.change(screen.getByLabelText("Instructions"), { target: { value: "Build" } });
    fireEvent.change(screen.getByLabelText("Final approver"), { target: { value: "member-1" } });
    fireEvent.change(screen.getByLabelText("Input bindings (JSON)"), { target: { value: "[" } });
    fireEvent.click(screen.getByRole("button", { name: "Save draft" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("valid JSON object");
    expect(mocks.save).not.toHaveBeenCalled();
    expect(mocks.replace).not.toHaveBeenCalled();
  });

  it("keeps unfamiliar definition versions read-only", () => {
    const definition = createSerialDefinition("Future", [], "member-1");
    editor({ id: "graph-1", workspaceId: "ws-1", name: "Future", revision: 1, definition: { ...definition, schemaVersion: 2 }, createdAt: "", updatedAt: "", versions: [] });
    expect(screen.getByLabelText("Name")).toBeDisabled();
    expect(screen.getByRole("button", { name: "Save draft" })).toBeDisabled();
    expect(screen.getByRole("alert")).toHaveTextContent("cannot edit");
  });

  it("preserves unsaved form edits when a pasted definition cannot be imported", () => {
    editor();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "My unsaved work" } });
    fireEvent.change(screen.getByLabelText("Instructions"), { target: { value: "Keep this instruction" } });
    fireEvent.change(screen.getByLabelText("Definition JSON"), { target: { value: '{"schema_version":1}' } });
    fireEvent.click(screen.getByRole("button", { name: "Import JSON", hidden: true }));
    expect(screen.getByLabelText("Name")).toHaveValue("My unsaved work");
    expect(screen.getByLabelText("Instructions")).toHaveValue("Keep this instruction");
    expect(screen.getByRole("alert")).toHaveTextContent("current edits were preserved");
    expect(mocks.save).not.toHaveBeenCalled();
  });

  it("imports an entire definition locally and preserves custom control keys when saving", async () => {
    editor();
    const definition = createSerialDefinition("Imported workflow", [{ nodeKey: "build", type: "agent", agentId: "agent-1", instructions: "Imported instructions", executionMode: "none", maxAttempts: 2, timeoutSeconds: 60, inputs: {}, outputSchema: { type: "object" } }], "member-1", { type: "object" }, { start: "begin", approval: "review", end: "finish" });
    fireEvent.change(screen.getByLabelText("Definition JSON"), { target: { value: exportGraphDefinition(definition) } });
    fireEvent.click(screen.getByRole("button", { name: "Import JSON", hidden: true }));
    expect(screen.getByLabelText("Name")).toHaveValue("Imported workflow");
    expect(screen.getByLabelText("Instructions")).toHaveValue("Imported instructions");
    expect(mocks.save).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Save draft" }));
    await waitFor(() => expect(mocks.save).toHaveBeenCalledOnce());
    expect(mocks.save.mock.calls[0]![0].definition.nodes.map((node: { nodeKey: string }) => node.nodeKey)).toEqual(["begin", "build", "review", "finish"]);
  });
});
