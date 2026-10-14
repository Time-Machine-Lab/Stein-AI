import { create } from "zustand";
import * as SecureStore from "expo-secure-store";
import type { GraphDefinition, GraphGenerationRequest } from "@multica/core/graphs/types";
import { definitionToWire } from "@multica/core/graphs/definition";
import { GraphDefinitionSchema } from "@multica/core/graphs/schemas";

export const graphDraftIdentity = (ws: string, user: string, id = "new") =>
  [ws, user, id].map((value) => Array.from(value).map((c) => c.codePointAt(0)!.toString(16)).join("_")).join(".");
export interface MobileGraphDraft {
  definition: GraphDefinition; revision: number; dirty: boolean; conflict: boolean;
  undo: GraphDefinition[]; redo: GraphDefinition[]; values: Record<string, unknown>;
  proposal?: { definition: GraphDefinition; base: string; summary?: string };
  messages: NonNullable<GraphGenerationRequest["messages"]>; storageError?: string;
}
type Picker = { key: string; path: (string | number)[]; title: string; value: string; options: { value: string; label: string; description?: string; data?: unknown }[] };
interface State {
  documents: Record<string, MobileGraphDraft>; picker?: Picker;
  open(key: string, definition: GraphDefinition, revision: number): void;
  recover(key: string): Promise<void>;
  edit(key: string, definition: GraphDefinition): void;
  saved(key: string, submitted: GraphDefinition, revision: number): void;
  reset(key: string, definition: GraphDefinition, revision: number): void;
  field(key: string, path: (string | number)[], value: unknown): void;
  undo(key: string): void; redo(key: string): void;
  propose(key: string, definition: GraphDefinition, base: GraphDefinition, summary?: string): void;
  apply(key: string): void; discardProposal(key: string): void;
  clear(): void;
}
function setAt(value: unknown, path: (string | number)[], next: unknown): unknown {
  if (!path.length) return next;
  const [key, ...tail] = path;
  if (Array.isArray(value)) return value.map((item, index) => index === key ? setAt(item, tail, next) : item);
  const object = value && typeof value === "object" ? value as Record<string, unknown> : {};
  return { ...object, [key!]: setAt(object[String(key)], tail, next) };
}
// Chunk encrypted drafts so every SecureStore value stays below iOS's small-value
// limit. Switch the manifest last: interruption can never expose a partial draft.
const queues = new Map<string, Promise<void>>();
const timers = new Map<string, ReturnType<typeof setTimeout>>();
function schedule(key: string, draft: MobileGraphDraft, onError: (error: unknown) => void) {
  clearTimeout(timers.get(key));
  if (!draft.dirty) { timers.delete(key); void persist(key).catch(onError); return; }
  timers.set(key, setTimeout(() => { timers.delete(key); void persist(key, draft).catch(onError); }, 350));
}
async function persist(key: string, draft?: MobileGraphDraft) {
  const prior = queues.get(key) ?? Promise.resolve();
  const work = prior.catch(() => {}).then(async () => {
    const head = `graph.${key}`, oldText = await SecureStore.getItemAsync(head);
    const old = oldText ? JSON.parse(oldText) as { generation: string; count: number } : undefined;
    if (draft?.dirty) {
      const text = JSON.stringify({ definition: definitionToWire(draft.definition), revision: draft.revision });
      const generation = `${Date.now()}_${Math.random().toString(36).slice(2)}`, chunks = text.match(/[\s\S]{1,400}/g) ?? [];
      for (let i = 0; i < chunks.length; i++) await SecureStore.setItemAsync(`${head}.${generation}.${i}`, chunks[i]!);
      await SecureStore.setItemAsync(head, JSON.stringify({ generation, count: chunks.length }));
    } else await SecureStore.deleteItemAsync(head);
    if (old && Number.isInteger(old.count) && old.count <= 3000 && /^[a-z0-9_]+$/.test(old.generation))
      for (let i = 0; i < old.count; i++) await SecureStore.deleteItemAsync(`${head}.${old.generation}.${i}`);
  });
  queues.set(key, work);
  try { await work; } finally { if (queues.get(key) === work) queues.delete(key); }
}
export const useGraphDraftStore = create<State>((set, get) => {
  const update = (key: string, transform: (draft: MobileGraphDraft) => MobileGraphDraft, save = true) => {
    const current = get().documents[key]; if (!current) return;
    const next = transform(current); set((s) => ({ documents: { ...s.documents, [key]: next } }));
    if (save) schedule(key, next, (error) => set((s) => ({ documents: s.documents[key] ? { ...s.documents, [key]: { ...s.documents[key]!, storageError: String(error) } } : s.documents })));
  };
  return {
    documents: {},
    open: (key, definition, revision) => {
      const existing = get().documents[key];
      if (existing?.dirty) { if (existing.revision !== revision) update(key, (d) => ({ ...d, conflict: true }), false); return; }
      if (existing?.revision === revision) return;
      set((s) => ({ documents: { ...s.documents, [key]: { definition, revision, dirty: false, conflict: false, undo: [], redo: [], values: {}, messages: [] } } }));
    },
    recover: async (key) => {
      try {
        const text = await SecureStore.getItemAsync(`graph.${key}`); if (!text) return;
        const meta = JSON.parse(text) as { generation: string; count: number };
        if (!Number.isInteger(meta.count) || meta.count < 1 || meta.count > 3000 || !/^[a-z0-9_]+$/.test(meta.generation)) throw new Error("Invalid saved draft");
        const chunks: string[] = [];
        for (let i = 0; i < meta.count; i++) { const part = await SecureStore.getItemAsync(`graph.${key}.${meta.generation}.${i}`); if (part === null) throw new Error("Incomplete saved draft"); chunks.push(part); }
        const raw = JSON.parse(chunks.join("")), definition = GraphDefinitionSchema.parse(raw.definition);
        if (![1,2].includes(definition.schemaVersion) || !Number.isInteger(raw.revision)) throw new Error("Unsupported saved draft");
        update(key, (d) => d.dirty ? d : { ...d, definition, revision: raw.revision, dirty: true, conflict: raw.revision !== d.revision }, false);
      } catch (error) { update(key, (d) => ({ ...d, storageError: String(error) }), false); }
    },
    edit: (key, definition) => update(key, (d) => ({ ...d, definition, dirty: true, undo: [...d.undo, d.definition].slice(-100), redo: [], storageError: undefined })),
    saved: (key, submitted, revision) => update(key, (d) => ({ ...d, revision, conflict: false, dirty: JSON.stringify(d.definition) !== JSON.stringify(submitted) })),
    reset: (key, definition, revision) => update(key, (d) => ({ ...d, definition, revision, dirty: false, conflict: false, undo: [], redo: [] })),
    field: (key, path, value) => update(key, (d) => {
      let target = path, replacement = value;
      if (path[0] === "definition") {
        const parentPath = path.slice(0, -1), parent = parentPath.reduce<unknown>((v, k) => v && typeof v === "object" ? (v as Record<string, unknown>)[String(k)] : undefined, d) as Record<string, unknown> | undefined;
        if (path[path.length - 1] === "op" && typeof value === "string") {
          target = parentPath;
          replacement = ["all", "any", "not"].includes(value) ? { op: value, conditions: (Array.isArray(parent?.conditions) ? parent.conditions : [{ op: "exists", left: { source: "run_input", path: "" } }]).slice(0, value === "not" ? 1 : undefined) } : { op: value, left: parent?.left ?? { source: "run_input", path: "" }, ...(value !== "exists" ? { right: parent?.right ?? { source: "constant", value: true } } : {}) };
        } else if (path[path.length - 1] === "source" && typeof value === "string") {
          target = parentPath;
          replacement = { source: value, ...(value === "constant" ? { value: "" } : value === "object" ? { fields: {} } : value !== "iteration_index" ? { path: "" } : {}), ...(parent?.optional !== undefined ? { optional: parent.optional } : {}), ...(parent?.default !== undefined ? { default: parent.default } : {}) };
        } else if (path[path.length - 1] === "type" && typeof value === "string" && (path.includes("inputSchema") || path.includes("outputSchema"))) {
          target = parentPath; replacement = { type: value, ...(parent?.description ? { description: parent.description } : {}), ...(value === "array" ? { items: { type: "string" } } : {}) };
        }
      }
      const next = setAt(d, target, replacement) as MobileGraphDraft;
      if (path[0] === "values" && path[1] === "input") next.values = { ...next.values, inputTouched: true };
      if (path[0] === "values" && path[1] === "issueId") next.values = { ...next.values, issueTouched: true };
      return path[0] === "definition" ? { ...next, dirty: true, undo: [...d.undo, d.definition].slice(-100), redo: [] } : next;
    }, path[0] === "definition"),
    undo: (key) => update(key, (d) => d.undo.length ? { ...d, definition: d.undo[d.undo.length - 1]!, undo: d.undo.slice(0, -1), redo: [d.definition, ...d.redo], dirty: true } : d),
    redo: (key) => update(key, (d) => d.redo.length ? { ...d, definition: d.redo[0]!, redo: d.redo.slice(1), undo: [...d.undo, d.definition], dirty: true } : d),
    propose: (key, definition, base, summary) => update(key, (d) => ({ ...d, proposal: { definition, base: JSON.stringify(base), summary } }), false),
    apply: (key) => { const d = get().documents[key]; if (!d?.proposal) return; if (JSON.stringify(d.definition) !== d.proposal.base) throw new Error("The draft changed after this proposal. Generate a new proposal to preserve your changes."); get().edit(key, d.proposal.definition); get().discardProposal(key); },
    discardProposal: (key) => update(key, (d) => ({ ...d, proposal: undefined }), false),
    clear: () => { const keys = Object.keys(get().documents); set({ documents: {}, picker: undefined }); for (const key of keys) { clearTimeout(timers.get(key)); timers.delete(key); void persist(key).catch(() => {}); } },
  };
});
