"use client";

import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import {
  graphDraftKey,
  useArchiveGraph,
  useGraphDraftStore,
  usePublishGraph,
  useSaveGraph,
  useValidateGraph,
  type GraphDefinition,
  type GraphDiagnostic,
  type GraphRecord,
} from "@multica/core/graphs";
import { api } from "@multica/core/api";
import { useWorkspacePaths, useWorkspaceSlug } from "@multica/core/paths";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";
import { useGraphActor, useGraphCommandKey } from "./shared";
import { blankDefinition } from "./graph-definition-helpers";

/**
 * Everything about one graph's draft that lives above the editor: the draft
 * document, who may change it, and the single "publish" action that saves,
 * validates and publishes in one go.
 *
 * Owned by the page rather than the editor so the page header can carry the
 * publish button and the problem count while the editor fills the body.
 */
export function useGraphPublishFlow(wsId: string, graph?: GraphRecord) {
  const { t } = useT("graphs");
  const workspaceSlug = useWorkspaceSlug();
  const actor = useGraphActor(wsId);
  const navigation = useNavigation();
  const paths = useWorkspacePaths();
  const commandKey = useGraphCommandKey();
  const save = useSaveGraph(wsId),
    validate = useValidateGraph(),
    publish = usePublishGraph(wsId),
    archive = useArchiveGraph(wsId);
  const initial = useMemo(
    () => graph?.definition ?? blankDefinition(),
    [graph?.definition],
  );
  const draftKey = graphDraftKey(wsId, actor.userId, graph?.id);
  const draft = useGraphDraftStore((state) => state.documents[draftKey]);
  const definition = draft?.definition ?? initial;
  const [localError, setLocalError] = useState<Error | null>(null);
  const [publishing, setPublishing] = useState(false);

  const admin = actor.role === "admin" || actor.role === "owner";
  const canEdit =
    Boolean(actor.userId) &&
    (!graph || admin || graph.createdBy === actor.userId);
  const supported = [1, 2].includes(definition.schemaVersion);
  const pending =
    publishing ||
    save.isPending ||
    validate.isPending ||
    publish.isPending ||
    archive.isPending;
  const disabled =
    pending || !canEdit || !supported || Boolean(graph?.archivedAt);
  const blocked = Boolean(draft?.conflict || draft?.storageConflict);

  useEffect(() => {
    if (actor.userId)
      useGraphDraftStore
        .getState()
        .open(
          draftKey,
          initial,
          graph?.revision ?? 0,
          workspaceSlug ?? undefined,
        );
  }, [actor.userId, draftKey, initial, graph?.revision, workspaceSlug]);

  useEffect(() => {
    if (!draft?.dirty) return;
    const warn = (event: BeforeUnloadEvent) => {
      if (draft.storageError || draft.storageConflict) event.preventDefault();
    };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [draft?.dirty, draft?.storageError, draft?.storageConflict]);

  const change = (next: GraphDefinition) => {
    useGraphDraftStore.getState().edit(draftKey, next);
    validate.reset();
    publish.reset();
    setLocalError(null);
  };

  const fail = (error: unknown) =>
    setLocalError(error instanceof Error ? error : new Error(String(error)));

  /** Saves the draft. Returns the saved revision, or null when it did not. */
  async function saveDraft(): Promise<number | null> {
    if (!canEdit || !supported || graph?.archivedAt || blocked) return null;
    // Read before `saved`, which clears the draft document this comes from.
    // The conversation that designed a new graph is retired with it: the draft
    // key follows the graph to `:<id>` from here on, so a session left running
    // would be one nothing points at.
    const designedBy = !graph ? (draft?.ai?.sessionId ?? null) : null;
    try {
      const request = {
        definition,
        id: graph?.id,
        expectedRevision: draft?.baseRevision ?? graph?.revision ?? 0,
      };
      const result = await save.mutateAsync({
        ...request,
        idempotencyKey: commandKey(request),
      });
      useGraphDraftStore
        .getState()
        .saved(draftKey, definition, result.revision);
      if (designedBy) {
        try {
          await api.setChatSessionArchived(designedBy, true);
        } catch {
          // A failed archive leaves a listed conversation the user can
          // discard by hand; it must not turn the save into an error.
        }
      }
      validate.reset();
      if (!graph) {
        toast.success(t(($) => $.page.saved));
        navigation.replace(paths.graphDetail(result.id));
      }
      return result.revision;
    } catch (error) {
      fail(error);
      return null;
    }
  }

  /**
   * Save (when there is anything to save), validate, publish. Stops at the
   * first step that does not pass; a failed validation leaves its diagnostics
   * on `issues` so the canvas can point at the steps they belong to.
   */
  async function publishAll(): Promise<boolean> {
    if (!graph || !admin || graph.archivedAt || blocked || pending)
      return false;
    setPublishing(true);
    setLocalError(null);
    try {
      let revision = draft?.baseRevision ?? graph.revision;
      if (draft?.dirty) {
        const saved = await saveDraft();
        if (saved === null) return false;
        revision = saved;
      }
      const result = await validate.mutateAsync(graph.id);
      if (result.valid !== true) {
        toast.error(
          t(($) => $.studio.ux.publish_blocked, {
            count: result.diagnostics.length,
          }),
        );
        return false;
      }
      const request = { id: graph.id, expectedRevision: revision };
      await publish.mutateAsync({
        ...request,
        idempotencyKey: commandKey(request),
      });
      toast.success(t(($) => $.page.published));
      return true;
    } catch (error) {
      fail(error);
      return false;
    } finally {
      setPublishing(false);
    }
  }

  function archiveGraph() {
    if (!graph) return;
    const request = {
      id: graph.id,
      expectedRevision: draft?.baseRevision ?? graph.revision,
    };
    archive.mutate({
      ...request,
      idempotencyKey: commandKey({ ...request, action: "archive" }),
    });
  }

  const diagnostics: GraphDiagnostic[] =
    validate.data && validate.data.valid !== true
      ? validate.data.diagnostics
      : [];
  const issues: Record<string, number> = {};
  for (const diagnostic of diagnostics)
    if (diagnostic.nodeKey)
      issues[diagnostic.nodeKey] = (issues[diagnostic.nodeKey] ?? 0) + 1;

  return {
    actor,
    draftKey,
    draft,
    initial,
    /** The revision the server holds, for "keep my local copy". */
    serverRevision: graph?.revision ?? 0,
    definition,
    change,
    canEdit,
    admin,
    supported,
    disabled,
    pending,
    blocked,
    saving: save.isPending,
    publishing,
    saveDraft,
    publishAll,
    archiveGraph,
    diagnostics,
    issues,
    error:
      localError ??
      save.error ??
      validate.error ??
      publish.error ??
      archive.error,
    setError: setLocalError,
    discard: () =>
      useGraphDraftStore
        .getState()
        .discard(draftKey, initial, graph?.revision ?? 0),
  };
}

export type GraphPublishFlow = ReturnType<typeof useGraphPublishFlow>;
