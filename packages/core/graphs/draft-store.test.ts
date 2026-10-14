import { describe, expect, it } from "vitest";
import {
  createGraphDraftStore,
  graphDraftKey,
  type GraphDraftAi,
} from "./draft-store";
import { createSerialDefinition } from "./definition";
import type { StorageAdapter } from "../types/storage";

function memoryStorage(): StorageAdapter {
  const values = new Map<string, string>();
  return {
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => {
      values.set(key, value);
    },
    removeItem: (key) => {
      values.delete(key);
    },
  };
}
const definition = createSerialDefinition("Original", [], "member");
const key = graphDraftKey("workspace", "user", "graph");

describe("Graph draft recovery and history", () => {
  it("recovers unsaved work without leaking across users, workspaces, or graphs", () => {
    const storage = memoryStorage();
    const first = createGraphDraftStore(storage);
    first.getState().open(key, definition, 1);
    first.getState().edit(key, { ...definition, name: "Unsaved" });
    const restored = createGraphDraftStore(storage);
    restored.getState().open(key, definition, 1);
    expect(restored.getState().documents[key]?.definition.name).toBe("Unsaved");
    expect(restored.getState().documents[key]?.recovered).toBe(true);
    for (const other of [
      graphDraftKey("workspace", "other", "graph"),
      graphDraftKey("other", "user", "graph"),
      graphDraftKey("workspace", "user", "other"),
    ]) {
      restored.getState().open(other, definition, 1);
      expect(restored.getState().documents[other]?.definition.name).toBe(
        "Original",
      );
    }
  });
  it("retains conflicting local edits and requires an explicit resolution", () => {
    const store = createGraphDraftStore(memoryStorage());
    store.getState().open(key, definition, 1);
    store.getState().edit(key, { ...definition, name: "Local" });
    store.getState().open(key, { ...definition, name: "Remote" }, 2);
    expect(store.getState().documents[key]).toMatchObject({
      definition: { name: "Local" },
      baseRevision: 1,
      conflict: true,
    });
    store.getState().keepLocal(key, 2);
    expect(store.getState().documents[key]).toMatchObject({
      definition: { name: "Local" },
      baseRevision: 2,
      conflict: false,
      dirty: true,
    });
  });
  it("does not silently overwrite edits from another tab", () => {
    const storage = memoryStorage();
    const a = createGraphDraftStore(storage),
      b = createGraphDraftStore(storage);
    a.getState().open(key, definition, 1);
    b.getState().open(key, definition, 1);
    a.getState().edit(key, { ...definition, name: "Tab A" });
    b.getState().edit(key, { ...definition, name: "Tab B" });
    expect(b.getState().documents[key]?.storageConflict).toBe(true);
    const c = createGraphDraftStore(storage);
    c.getState().open(key, definition, 1);
    expect(c.getState().documents[key]?.definition.name).toBe("Tab A");
  });
  it("undoes and redoes AI proposals as one edit, clears redo after a new edit, and clears persisted work after save", () => {
    const storage = memoryStorage(),
      store = createGraphDraftStore(storage);
    store.getState().open(key, definition, 1);
    store.getState().edit(key, { ...definition, name: "Proposed" });
    store.getState().undo(key);
    expect(store.getState().documents[key]?.definition.name).toBe("Original");
    store.getState().redo(key);
    expect(store.getState().documents[key]?.definition.name).toBe("Proposed");
    store.getState().undo(key);
    store.getState().edit(key, { ...definition, name: "Manual" });
    expect(store.getState().documents[key]?.future).toHaveLength(0);
    store.getState().saved(key, { ...definition, name: "Manual" }, 2);
    expect(storage.getItem(key)).toBeNull();
    expect(store.getState().documents[key]?.dirty).toBe(false);
  });
  it("keeps edits in memory and exposes storage failure instead of claiming recovery is available", () => {
    const store = createGraphDraftStore({
      getItem: () => null,
      removeItem: () => {},
      setItem: () => {
        throw new Error("quota");
      },
    });
    store.getState().open(key, definition, 1);
    store.getState().edit(key, { ...definition, name: "Do not lose this" });
    expect(store.getState().documents[key]?.definition.name).toBe(
      "Do not lose this",
    );
    expect(store.getState().documents[key]?.storageError).toBe(true);
  });
  it("preserves edits made after the saved snapshot was sent", () => {
    const store = createGraphDraftStore(memoryStorage());
    store.getState().open(key, definition, 1);
    store.getState().edit(key, { ...definition, name: "Submitted" });
    const submitted = store.getState().documents[key]!.definition;
    store.getState().edit(key, { ...definition, name: "More recent" });
    store.getState().saved(key, submitted, 2);
    expect(store.getState().documents[key]).toMatchObject({
      definition: { name: "More recent" },
      dirty: true,
      baseRevision: 2,
    });
  });
  it("ignores malformed nested nodes in persisted data", () => {
    const storage = memoryStorage();
    storage.setItem(
      key,
      JSON.stringify({
        format: 1,
        token: "bad",
        definition: { ...definition, nodes: [null] },
        baseRevision: 1,
      }),
    );
    const store = createGraphDraftStore(storage);
    store.getState().open(key, definition, 1);
    expect(store.getState().documents[key]).toMatchObject({
      definition,
      recovered: false,
    });
  });
  it("does not delete another tab's draft when an in-flight save completes", () => {
    const storage = memoryStorage(),
      first = createGraphDraftStore(storage);
    first.getState().open(key, definition, 1);
    first.getState().edit(key, { ...definition, name: "Submitted" });
    const submitted = first.getState().documents[key]!.definition;
    const second = createGraphDraftStore(storage);
    second.getState().open(key, definition, 1);
    second.getState().edit(key, { ...definition, name: "Other tab" });
    first.getState().saved(key, submitted, 2);
    const recovered = createGraphDraftStore(storage);
    recovered.getState().open(key, submitted, 2);
    expect(recovered.getState().documents[key]).toMatchObject({
      definition: { name: "Other tab" },
      dirty: true,
      conflict: true,
    });
    expect(first.getState().documents[key]?.dirty).toBe(false);
  });
  it("clears only one workspace across reloads, then removes persisted and in-memory drafts on logout", () => {
    const storage = memoryStorage(),
      first = createGraphDraftStore(storage);
    const other = graphDraftKey("other-workspace", "user", "graph");
    first.getState().open(key, definition, 1, "acme");
    first.getState().edit(key, { ...definition, name: "Acme draft" });
    first.getState().open(other, definition, 1, "other");
    first.getState().edit(other, { ...definition, name: "Other draft" });
    const restored = createGraphDraftStore(storage);
    restored.getState().clearWorkspace("acme");
    expect(storage.getItem(key)).toBeNull();
    restored.getState().open(other, definition, 1, "other");
    expect(restored.getState().documents[other]?.definition.name).toBe(
      "Other draft",
    );
    restored.getState().clearAll();
    expect(restored.getState().documents).toEqual({});
    expect(storage.getItem(other)).toBeNull();
  });
});

const ai: GraphDraftAi = {
  sessionId: "session",
  agentId: "carrier",
  executorId: "agent-1",
  scopeIds: ["agent-1", "agent-2"],
  approverId: "member-1",
  runtimeId: "runtime-1",
  appliedMessageId: null,
};

/** The row the store wrote for `key`, or null when nothing was persisted. */
function stored(storage: StorageAdapter, draftKey: string) {
  const raw = storage.getItem(draftKey);
  return raw ? (JSON.parse(raw) as { token: string; ai?: GraphDraftAi }) : null;
}

describe("Graph draft conversation binding", () => {
  it("returns to the same conversation after a reload, with the reply it merged", () => {
    const storage = memoryStorage(),
      first = createGraphDraftStore(storage);
    first.getState().open(key, definition, 1);
    first.getState().bindAi(key, ai);
    first.getState().edit(key, { ...definition, name: "Drafted" });
    first.getState().markAiApplied(key, "message-2");
    const restored = createGraphDraftStore(storage);
    restored.getState().open(key, definition, 1);
    expect(restored.getState().documents[key]).toMatchObject({
      definition: { name: "Drafted" },
      recovered: true,
      ai: { ...ai, appliedMessageId: "message-2" },
    });
  });
  it("persists a binding before it has drafted anything", () => {
    // A refresh in the seconds between starting a conversation and its first
    // reply has to find it again: the alternative is a second carrier agent
    // beside an orphaned one.
    const storage = memoryStorage(),
      store = createGraphDraftStore(storage);
    store.getState().open(key, definition, 1);
    store.getState().bindAi(key, ai);
    expect(stored(storage, key)?.ai).toEqual(ai);
    const restored = createGraphDraftStore(storage);
    restored.getState().open(key, definition, 1);
    expect(restored.getState().documents[key]?.ai).toEqual(ai);
  });
  it("records a merged reply once, on a draft that is already being persisted", () => {
    const storage = memoryStorage(),
      store = createGraphDraftStore(storage);
    store.getState().open(key, definition, 1);
    store.getState().bindAi(key, ai);
    store.getState().edit(key, { ...definition, name: "Drafted" });
    const before = store.getState().documents[key]!.token;
    store.getState().markAiApplied(key, "message-1");
    const after = store.getState().documents[key]!.token;
    expect(after).not.toBe(before);
    expect(stored(storage, key)?.ai?.appliedMessageId).toBe("message-1");
    // Re-recording the same reply is what every re-render would do; it must not
    // burn a storage write and a new token each time.
    store.getState().markAiApplied(key, "message-1");
    expect(store.getState().documents[key]!.token).toBe(after);
  });
  it("drops a binding written by another build rather than half-reading it", () => {
    const storage = memoryStorage();
    storage.setItem(
      key,
      JSON.stringify({
        format: 1,
        token: "t",
        definition,
        baseRevision: 1,
        ai: { ...ai, appliedMessageId: undefined },
      }),
    );
    const store = createGraphDraftStore(storage);
    store.getState().open(key, definition, 1);
    expect(store.getState().documents[key]?.ai).toBeNull();
  });
  it("forgets the conversation without forgetting the draft, and vice versa", () => {
    const storage = memoryStorage(),
      store = createGraphDraftStore(storage);
    store.getState().open(key, definition, 1);
    store.getState().bindAi(key, ai);
    store.getState().clearAi(key);
    expect(store.getState().documents[key]?.ai).toBeNull();
    // Nothing was edited, so there is no draft worth recovering — reopening the
    // graph must not announce one.
    expect(stored(storage, key)).toBeNull();
    store.getState().bindAi(key, ai);
    store.getState().edit(key, { ...definition, name: "Drafted" });
    store.getState().clearAi(key);
    expect(store.getState().documents[key]).toMatchObject({
      ai: null,
      dirty: true,
      definition: { name: "Drafted" },
    });
    const restored = createGraphDraftStore(storage);
    restored.getState().open(key, definition, 1);
    expect(restored.getState().documents[key]).toMatchObject({
      definition: { name: "Drafted" },
      ai: null,
    });
  });
});
