import { create } from "zustand";
import { defaultStorage } from "../platform/storage";
import type { StorageAdapter } from "../types/storage";
import type { GraphDefinition } from "../graphs/types";
import { GraphDefinitionSchema } from "../graphs/schemas";
import { definitionToWire } from "../graphs/definition";
import { registerDraftCleanup } from "../drafts/cleanup-registry";
import { getCurrentSlug } from "../platform/workspace-storage";

/**
 * The AI conversation that is drafting this document, if one is.
 *
 * It lives in the draft document rather than in the URL because
 * `packages/views` may not import a router, and because it belongs to the same
 * identity the draft already has: one `(user, workspace, graph | "new")` triple.
 * A refresh therefore returns to the same conversation, mid-thought.
 *
 * `appliedMessageId` is the assistant reply already merged into `definition`.
 * It is what makes reopening a conversation idempotent: without it, every mount
 * would re-merge the last reply, and every re-render would look like a new
 * change.
 */
export interface GraphDraftAi {
  /** The chat session carrying the conversation. */
  sessionId: string;
  /** The hidden carrier agent backing that session; deleted with it. */
  agentId: string;
  /** The executor whose runtime and model the carrier clones. */
  executorId: string;
  /**
   * The agents the draft may assign work to, in alias order: the first is
   * `agent_default`. Persisted rather than re-derived, because aliases are
   * positional — a directory that changes between turns changes what an alias
   * means, and a reply written against the old one would merge into the wrong
   * teammate.
   */
  scopeIds: string[];
  /** The member a new approval node falls back to (`approver_default`). */
  approverId: string;
  /** Where the conversation runs, for the runtime indicator. */
  runtimeId: string;
  appliedMessageId: string | null;
}
export interface GraphDraft {
  definition: GraphDefinition;
  workspaceSlug?: string;
  baseRevision: number;
  dirty: boolean;
  recovered: boolean;
  conflict: boolean;
  storageConflict: boolean;
  storageError: boolean;
  token: string | null;
  past: GraphDefinition[];
  future: GraphDefinition[];
  ai: GraphDraftAi | null;
}
interface GraphDraftState {
  documents: Record<string, GraphDraft>;
  open: (
    key: string,
    definition: GraphDefinition,
    revision: number,
    workspaceSlug?: string,
  ) => void;
  edit: (key: string, definition: GraphDefinition) => void;
  undo: (key: string) => void;
  redo: (key: string) => void;
  keepLocal: (key: string, revision: number) => void;
  saved: (key: string, definition: GraphDefinition, revision: number) => void;
  discard: (key: string, definition: GraphDefinition, revision: number) => void;
  /** Binds an AI conversation to the document, replacing any previous one. */
  bindAi: (key: string, ai: GraphDraftAi) => void;
  /** Records the assistant reply whose draft is now in `definition`. */
  markAiApplied: (key: string, messageId: string) => void;
  /** Unbinds the conversation without touching the definition. */
  clearAi: (key: string) => void;
  clearWorkspace: (slug: string, adapter?: StorageAdapter) => void;
  clearAll: () => void;
}
const clone = <T>(value: T): T => JSON.parse(JSON.stringify(value)) as T;
export function graphDraftKey(
  workspaceId: string,
  userId: string,
  graphId = "new",
) {
  return `multica-graph-draft:${encodeURIComponent(userId)}:${encodeURIComponent(workspaceId)}:${encodeURIComponent(graphId)}`;
}
const fresh = (
  definition: GraphDefinition,
  baseRevision: number,
): GraphDraft => ({
  definition: clone(definition),
  baseRevision,
  dirty: false,
  recovered: false,
  conflict: false,
  storageConflict: false,
  storageError: false,
  token: null,
  past: [],
  future: [],
  ai: null,
});

/**
 * Validates one stored AI binding, field by field, the way `read` validates the
 * definition: a payload written by a newer build — or truncated mid-write — must
 * yield no conversation at all rather than a half-bound one, because a
 * conversation missing its directory would silently rewrite actor aliases.
 */
function readAi(value: unknown): GraphDraftAi | null {
  if (!value || typeof value !== "object") return null;
  const ai = value as Record<string, unknown>;
  const text = (key: string): string | null =>
    typeof ai[key] === "string" ? (ai[key] as string) : null;
  const sessionId = text("sessionId"),
    agentId = text("agentId"),
    executorId = text("executorId"),
    approverId = text("approverId"),
    runtimeId = text("runtimeId");
  if (!sessionId || !agentId || !executorId || !approverId || !runtimeId)
    return null;
  if (
    !Array.isArray(ai.scopeIds) ||
    ai.scopeIds.some((id) => typeof id !== "string") ||
    (ai.appliedMessageId !== null && typeof ai.appliedMessageId !== "string")
  )
    return null;
  return {
    sessionId,
    agentId,
    executorId,
    approverId,
    runtimeId,
    scopeIds: ai.scopeIds as string[],
    appliedMessageId: (ai.appliedMessageId as string | null) ?? null,
  };
}
const indexKey = "multica-graph-drafts:index:v1";
function readIndex(storage: StorageAdapter): { key: string; slug: string }[] {
  try {
    const value: unknown = JSON.parse(storage.getItem(indexKey) ?? "[]");
    return Array.isArray(value)
      ? value.filter(
          (item): item is { key: string; slug: string } =>
            item &&
            typeof item.key === "string" &&
            /^multica-graph-draft:[^:]+:[^:]+:[^:]+$/.test(item.key) &&
            typeof item.slug === "string",
        )
      : [];
  } catch {
    return [];
  }
}
function removeIndexedDrafts(storage: StorageAdapter, slug?: string) {
  const index = readIndex(storage),
    remaining = index.filter(
      (item) => slug !== undefined && item.slug !== slug,
    );
  for (const item of index)
    if (slug === undefined || item.slug === slug) storage.removeItem(item.key);
  if (remaining.length) storage.setItem(indexKey, JSON.stringify(remaining));
  else storage.removeItem(indexKey);
}

/** Draft persistence is deliberately independent of Query's authoritative server state. */
export function createGraphDraftStore(storage: StorageAdapter) {
  function read(
    key: string,
  ): {
    token: string;
    definition: GraphDefinition;
    baseRevision: number;
    ai: GraphDraftAi | null;
  } | null {
    try {
      const raw = storage.getItem(key);
      if (!raw) return null;
      const value: unknown = JSON.parse(raw);
      if (
        !value ||
        typeof value !== "object" ||
        !("format" in value) ||
        value.format !== 1 ||
        !("token" in value) ||
        typeof value.token !== "string" ||
        !("definition" in value) ||
        !("baseRevision" in value) ||
        typeof value.baseRevision !== "number"
      )
        return null;
      const d = value.definition;
      if (
        !d ||
        typeof d !== "object" ||
        !("schemaVersion" in d) ||
        ![1, 2].includes(Number(d.schemaVersion)) ||
        !("nodes" in d) ||
        !Array.isArray(d.nodes) ||
        !("edges" in d) ||
        !Array.isArray(d.edges) ||
        !("name" in d) ||
        typeof d.name !== "string"
      )
        return null;
      const definition = GraphDefinitionSchema.parse(
        definitionToWire(d as GraphDefinition),
      );
      return {
        token: value.token,
        definition,
        baseRevision: value.baseRevision,
        ai: readAi("ai" in value ? value.ai : null),
      };
    } catch {
      return null;
    }
  }
  function persist(
    key: string,
    document: GraphDraft,
    force = false,
  ): GraphDraft {
    try {
      const latest = read(key);
      if (!force && latest?.token && latest.token !== document.token)
        return { ...document, storageConflict: true };
      const token = crypto.randomUUID();
      const index = readIndex(storage).filter((item) => item.key !== key);
      storage.setItem(
        indexKey,
        JSON.stringify([...index, { key, slug: document.workspaceSlug ?? "" }]),
      );
      storage.setItem(
        key,
        JSON.stringify({
          format: 1,
          token,
          definition: document.definition,
          baseRevision: document.baseRevision,
          // Omitted rather than written as null when there is no conversation,
          // so a document from this build stays readable by the previous one.
          ...(document.ai ? { ai: document.ai } : {}),
        }),
      );
      return {
        ...document,
        token,
        storageError: false,
        storageConflict: false,
      };
    } catch {
      return { ...document, storageError: true };
    }
  }
  return create<GraphDraftState>((set, get) => {
    const update = (key: string, document: GraphDraft) =>
      set((state) => ({ documents: { ...state.documents, [key]: document } }));
    // Persists only what already has a stored copy to keep in step. A clean
    // document has none (the two ways one can exist — `saved` and `discard` —
    // both remove it), and writing one would make the next `open` report a
    // recovered draft nobody edited.
    const sync = (key: string, document: GraphDraft) =>
      update(key, document.dirty ? persist(key, document) : document);
    const discard = (
      key: string,
      definition: GraphDefinition,
      revision: number,
    ) => {
      let failed = false;
      try {
        storage.removeItem(key);
      } catch {
        failed = true;
      }
      update(key, {
        ...fresh(definition, revision),
        workspaceSlug: get().documents[key]?.workspaceSlug,
        storageError: failed,
      });
    };
    return {
      documents: {},
      open: (
        key,
        definition,
        revision,
        workspaceSlug = getCurrentSlug() ?? "",
      ) => {
        const current = get().documents[key];
        if (current) {
          if (current.baseRevision === revision) return;
          update(
            key,
            current.dirty
              ? { ...current, conflict: true }
              : { ...fresh(definition, revision), workspaceSlug },
          );
          return;
        }
        const recovered = read(key);
        update(key, {
          ...(recovered
            ? {
                ...fresh(recovered.definition, recovered.baseRevision),
                token: recovered.token,
                dirty: true,
                recovered: true,
                conflict: recovered.baseRevision !== revision,
                // The conversation survives a reload with the definition: the
                // whole point of binding it to the document is that a refresh
                // returns to the same conversation rather than a blank one.
                ai: recovered.ai,
              }
            : fresh(definition, revision)),
          workspaceSlug,
        });
      },
      edit: (key, definition) => {
        const current = get().documents[key];
        if (
          !current ||
          JSON.stringify(current.definition) === JSON.stringify(definition)
        )
          return;
        update(
          key,
          persist(key, {
            ...current,
            definition: clone(definition),
            dirty: true,
            past: [...current.past, current.definition].slice(-100),
            future: [],
          }),
        );
      },
      undo: (key) => {
        const current = get().documents[key],
          previous = current?.past.at(-1);
        if (!current || !previous) return;
        update(
          key,
          persist(key, {
            ...current,
            definition: previous,
            dirty: true,
            past: current.past.slice(0, -1),
            future: [current.definition, ...current.future],
          }),
        );
      },
      redo: (key) => {
        const current = get().documents[key],
          next = current?.future[0];
        if (!current || !next) return;
        update(
          key,
          persist(key, {
            ...current,
            definition: next,
            dirty: true,
            past: [...current.past, current.definition],
            future: current.future.slice(1),
          }),
        );
      },
      bindAi: (key, ai) => {
        const current = get().documents[key];
        // Written even while the definition is untouched: a refresh before the
        // first reply must return to the same conversation rather than start a
        // second one beside an orphaned carrier agent.
        if (current) update(key, persist(key, { ...current, ai }));
      },
      markAiApplied: (key, messageId) => {
        const current = get().documents[key];
        // Guarded rather than assumed: a reply that changed nothing is still
        // applied, and re-recording the same id would burn a storage write and
        // a new token for nothing.
        if (current?.ai && current.ai.appliedMessageId !== messageId)
          sync(key, {
            ...current,
            ai: { ...current.ai, appliedMessageId: messageId },
          });
      },
      clearAi: (key) => {
        const current = get().documents[key];
        if (!current?.ai) return;
        // The definition is deliberately left alone: ending the conversation
        // does not undo the work it produced. A document with no local changes
        // has nothing worth keeping on disk, so it is dropped the way `discard`
        // drops one — the alternative is reopening the graph to a "draft
        // recovered" line for a draft that was never edited.
        const next = { ...current, ai: null };
        if (current.dirty) {
          sync(key, next);
          return;
        }
        let failed = false;
        try {
          storage.removeItem(key);
        } catch {
          failed = true;
        }
        update(key, { ...next, token: null, storageError: failed });
      },
      keepLocal: (key, revision) => {
        const current = get().documents[key];
        if (current)
          update(
            key,
            persist(
              key,
              {
                ...current,
                baseRevision: revision,
                conflict: false,
                dirty: true,
              },
              true,
            ),
          );
      },
      saved: (key, definition, revision) => {
        const current = get().documents[key];
        if (
          current &&
          JSON.stringify(current.definition) !== JSON.stringify(definition)
        ) {
          update(
            key,
            persist(key, {
              ...current,
              baseRevision: revision,
              conflict: false,
              dirty: true,
            }),
          );
        } else {
          // A completed request only owns the persisted snapshot it submitted.
          const latest = read(key);
          if (latest && latest.token !== current?.token)
            update(key, {
              ...fresh(definition, revision),
              workspaceSlug: current?.workspaceSlug,
            });
          else discard(key, definition, revision);
        }
      },
      discard,
      clearWorkspace: (slug, adapter = storage) => {
        removeIndexedDrafts(adapter, slug);
        set((state) => ({
          documents: Object.fromEntries(
            Object.entries(state.documents).filter(
              ([, document]) => document.workspaceSlug !== slug,
            ),
          ),
        }));
      },
      clearAll: () => {
        try {
          removeIndexedDrafts(storage);
        } finally {
          set({ documents: {} });
        }
      },
    };
  });
}

export const useGraphDraftStore = createGraphDraftStore(defaultStorage);
registerDraftCleanup({
  storageKey: indexKey,
  workspaceScoped: false,
  clearWorkspace: (adapter, slug) =>
    useGraphDraftStore.getState().clearWorkspace(slug, adapter),
  resetInMemory: () => useGraphDraftStore.getState().clearAll(),
});
