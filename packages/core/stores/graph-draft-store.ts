import { create } from "zustand";
import { defaultStorage } from "../platform/storage";
import type { StorageAdapter } from "../types/storage";
import type { GraphDefinition } from "../graphs/types";
import { GraphDefinitionSchema } from "../graphs/schemas";
import { definitionToWire } from "../graphs/definition";
import { registerDraftCleanup } from "../drafts/cleanup-registry";
import { getCurrentSlug } from "../platform/workspace-storage";

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
});
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
