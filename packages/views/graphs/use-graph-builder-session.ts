"use client";

import { useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  decodeGraphBuilderInput,
  graphBuilderSessionKeys,
  pickGraphBuilderRestore,
  type GraphBuilderSession,
} from "@multica/core/graphs";
import { api, ApiError } from "@multica/core/api";
import {
  chatKeys,
  chatMessagesOptions,
  pendingChatTaskOptions,
} from "@multica/core/chat/queries";
import { upsertChatMessageToCaches } from "@multica/core/chat/message-cache";
import { removeChatMessageFromCaches } from "@multica/core/realtime";
import { useWorkspaceId } from "@multica/core/hooks";
import type { ChatMessage } from "@multica/core/types";
import { useAppForeground } from "../common/use-app-foreground";
import { useChatDraftRestore } from "../chat/components/use-chat-draft-restore";
import { useT } from "../i18n";

const EMPTY_CHAT_MESSAGES: ChatMessage[] = [];

/**
 * Lifecycle of one Graph-design conversation: start it, exchange turns on it,
 * cancel a reply in flight, and destroy it on request.
 *
 * The graph counterpart of the agent studio's `useBuilderSession`, and
 * deliberately the same shape: both drive a real `chat_session` on a hidden
 * carrier agent, so both read messages and pending state through the ordinary
 * chat queries and rely on the global realtime sync to invalidate them — there
 * is no polling here and none is needed.
 *
 * The session id, though, comes from somewhere else. The studio reads it off
 * its route; a Graph draft has no route of its own, so the id is handed in by
 * the caller, which keeps it in the draft document (see `GraphDraftAi`). That
 * is what makes a conversation survive a refresh without a URL of its own.
 */
export function useGraphBuilderSession(options: {
  /** The conversation to operate on; empty string before one is started. */
  sessionId: string;
  /** Encodes composer text into the builder wire format for the current draft. */
  encodeInput: (text: string) => string;
}) {
  const { t } = useT("graphs");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const { sessionId } = options;

  // A conversation is listable once it has a message, and that list is what the
  // setup step offers as a way back after the draft document is gone.
  const invalidateSessionList = () =>
    qc.invalidateQueries({ queryKey: graphBuilderSessionKeys.list(wsId) });

  const [starting, setStarting] = useState(false);
  const [closing, setClosing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [restoreDraft, setRestoreDraft] = useState<{
    id: string;
    content: string;
  } | null>(null);

  // Cancelling a started-but-empty run defers the empty/non-empty judgment the
  // same way the main chat does (#5219): the synchronous cancel answers
  // immediately, and a prompt the daemon had already accepted arrives later as
  // a durable chat_draft_restore row. The two sources are exclusive; whichever
  // exists is handed to the composer, decoded out of the wire format.
  const appForeground = useAppForeground();
  const {
    restoreDraftRequest: durableRestoreRequest,
    handleRestoreDraftApplied: handleDurableRestoreApplied,
  } = useChatDraftRestore(sessionId || null, appForeground);
  const restoreDraftRequest = useMemo(
    () => pickGraphBuilderRestore(restoreDraft, durableRestoreRequest),
    [restoreDraft, durableRestoreRequest],
  );

  const messagesQuery = useQuery(chatMessagesOptions(sessionId));
  const pendingQuery = useQuery(pendingChatTaskOptions(sessionId));
  const messages = messagesQuery.data ?? EMPTY_CHAT_MESSAGES;
  const pending = !!pendingQuery.data?.task_id;
  // The conversation named by the draft document is gone — deleted here, from
  // another tab, or from the recovery list. Read off the transcript fetch
  // rather than "absent from any list", because a conversation is only listed
  // once it has a message: one created a second ago is legitimately missing
  // from the list and must not be mistaken for a dead one.
  const missing =
    messagesQuery.error instanceof ApiError && messagesQuery.error.status === 404;

  /** Starts the conversation. Returns the server's view of it, or null. */
  const start = async (request: {
    defaultAgentId: string;
    agentScope: string[];
    approverId: string;
  }): Promise<GraphBuilderSession | null> => {
    setStarting(true);
    setError(null);
    try {
      const session = await api.createGraphBuilderSession(request);
      if (!session.sessionId) throw new Error();
      return session;
    } catch (err) {
      // The server's own message is the useful one here: every reason this call
      // is refused — an agent the caller cannot invoke, an offline runtime, an
      // unavailable approver — is specific enough to act on, and the generic
      // copy would hide which one it was.
      setError(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.studio.builder.start_failed),
      );
      return null;
    } finally {
      setStarting(false);
    }
  };

  /**
   * Destroys the conversation. Returns false when the server refused, so the
   * caller can stay put and show the error rather than leave a conversation
   * that still exists and will never be listed again.
   */
  const destroy = async (): Promise<boolean> => {
    if (!sessionId) return true;
    setClosing(true);
    setError(null);
    try {
      await api.deleteChatSession(sessionId);
      qc.removeQueries({ queryKey: chatKeys.messages(sessionId) });
      // The send seeds both caches, so both must be dropped here. Leaving the
      // paged one behind would keep a deleted session's transcript sitting
      // fresh forever — it is staleTime: Infinity, so no reader self-corrects.
      qc.removeQueries({ queryKey: chatKeys.messagesPage(sessionId) });
      qc.removeQueries({ queryKey: chatKeys.pendingTask(sessionId) });
      void invalidateSessionList();
      return true;
    } catch (err) {
      setError(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.studio.builder.exit_failed),
      );
      return false;
    } finally {
      setClosing(false);
    }
  };

  /**
   * Retires the conversation once the graph it designed has been saved.
   *
   * Archived, not deleted: the conversation is the record of how the workflow
   * was designed, and an archived session is read-only, drops out of the
   * recovery list and can never run a task. Best-effort by design — the graph
   * is already committed at this point, so a failure here must never turn a
   * successful save into a retryable error.
   */
  const archiveAfterCreate = async () => {
    if (!sessionId) return;
    try {
      await api.setChatSessionArchived(sessionId, true);
      void invalidateSessionList();
    } catch {
      // It stays listed; the user can discard it explicitly.
    }
  };

  /**
   * Sends one turn.
   *
   * `commitInput` is the composer's clear (MUL-5181): it runs the moment the
   * server has accepted the message and the caches render it, not after the
   * reconciling invalidations settle.
   */
  const send = async (
    content: string,
    commitInput?: () => void,
  ): Promise<boolean> => {
    const text = content.trim();
    if (!text || !sessionId || pending) return false;
    setError(null);
    try {
      const encodedContent = options.encodeInput(text);
      const result = await api.sendChatMessage(sessionId, encodedContent);
      const createdAt = new Date().toISOString();
      // Same door as the chat surfaces (MUL-5711): writing only the flat cache
      // would leave the paged cache — which the chat surfaces read for the same
      // session — without the message.
      upsertChatMessageToCaches(
        qc,
        sessionId,
        {
          id: result.message_id,
          chat_session_id: sessionId,
          role: "user",
          content: encodedContent,
          task_id: result.task_id,
          created_at: createdAt,
        },
        { seedIfMissing: true },
      );
      qc.setQueryData(chatKeys.pendingTask(sessionId), {
        task_id: result.task_id,
        status: "queued",
        created_at: createdAt,
      });
      commitInput?.();
      void qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
      void qc.invalidateQueries({ queryKey: chatKeys.messagesPage(sessionId) });
      void qc.invalidateQueries({ queryKey: chatKeys.pendingTask(sessionId) });
      // The first turn is what makes a conversation listable at all.
      void invalidateSessionList();
      return true;
    } catch (err) {
      setError(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.studio.builder.send_failed),
      );
      return false;
    }
  };

  const stop = async () => {
    const taskId = pendingQuery.data?.task_id;
    if (!taskId || !sessionId) return;
    qc.setQueryData(chatKeys.pendingTask(sessionId), {});
    try {
      const result = await api.cancelTaskById(taskId);
      const restored = result.cancelled_chat_message;
      if (restored) {
        // The server deleted this prompt on restore, so drop it from both
        // caches before reconciling — without it the row lingers until the
        // refetch lands, and in the paged cache it would linger for good.
        removeChatMessageFromCaches(
          qc,
          restored.chat_session_id,
          restored.message_id,
        );
        if (restored.restore_to_input)
          setRestoreDraft({
            id: restored.message_id,
            // Still in the wire format: this is the raw chat_message content,
            // so it comes back out the same way the composer put it in.
            content: decodeGraphBuilderInput(restored.content),
          });
      }
      await Promise.all([
        qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) }),
        qc.invalidateQueries({ queryKey: chatKeys.messagesPage(sessionId) }),
        qc.invalidateQueries({ queryKey: chatKeys.pendingTask(sessionId) }),
      ]);
    } catch (err) {
      setError(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.studio.builder.stop_failed),
      );
      // The cancel may still have landed server-side, so re-read the messages
      // too rather than only the pending marker.
      qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
      qc.invalidateQueries({ queryKey: chatKeys.messagesPage(sessionId) });
      qc.invalidateQueries({ queryKey: chatKeys.pendingTask(sessionId) });
    }
  };

  return {
    messages,
    missing,
    messagesLoading: messagesQuery.isLoading,
    pendingTask: pendingQuery.data,
    pending,
    starting,
    closing,
    error,
    restoreDraftRequest,
    handleRestoreDraftApplied: () => {
      if (restoreDraft) {
        setRestoreDraft(null);
        return;
      }
      handleDurableRestoreApplied();
    },
    start,
    destroy,
    archiveAfterCreate,
    send,
    stop,
  };
}
