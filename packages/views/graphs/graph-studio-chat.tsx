"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { MessageSquarePlus } from "lucide-react";
import { api } from "@multica/core/api";
import {
  decodeGraphBuilderInput,
  encodeGraphBuilderInput,
  graphBuilderSessionKeys,
  graphBuilderSessionsOptions,
  graphDefinitionChanges,
  hasGraphDraftBlock,
  mergeGraphDraft,
  parseGraphDraft,
  stripGraphDraft,
  useGraphDraftStore,
  type GraphBuilderSessionSummary,
  type GraphDefinition,
  type GraphDraftAi,
  type GraphDraftMergeFailure,
} from "@multica/core/graphs";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import type { Agent, MemberWithUser, RuntimeDevice } from "@multica/core/types";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { ChatInput } from "../chat/components/chat-input";
import {
  ChatMessageList,
  ChatMessageSkeleton,
} from "../chat/components/chat-message-list";
import { AppLink } from "../navigation";
import { useT, useTimeAgo } from "../i18n";
import {
  graphAgentReady,
  graphBuilderCandidates,
  graphBuilderDirectory,
  graphScopeWithExecutor,
} from "./graph-ai-actors";
import { GraphActorPicker } from "./graph-actor-picker";
import { useGraphBuilderSession } from "./use-graph-builder-session";

/** One AI turn that changed the draft, as the canvas marks it. */
export interface GraphAiTurn {
  messageId: string;
  added: string[];
  changed: string[];
  removed: string[];
  summary: string;
}

/** Text to drop into the composer, e.g. "About step X: ". */
export interface GraphComposerPrefill {
  id: string;
  content: string;
}

interface MergeError {
  messageId: string;
  reason: GraphDraftMergeFailure;
  alias?: string;
  availableAliases?: string[];
  detail?: string;
}

interface Actors {
  agents: Agent[];
  runtimes: RuntimeDevice[];
  members: MemberWithUser[];
  currentUserId: string | null;
  /** Any of the lists above is still loading; nothing is judged until it is not. */
  loading: boolean;
}

/**
 * The conversation column of the studio. Before a conversation exists it is a
 * composer with sensible defaults; the conversation (and its hidden carrier
 * agent) is created by the first message, never just by opening the page.
 */
export function GraphStudioChat({
  draftKey,
  ai,
  definition,
  actors,
  memberId,
  disabled,
  available,
  prefill,
  onPrefillApplied,
  initialMessage,
  onApplyTurn,
}: {
  draftKey: string;
  ai: GraphDraftAi | null;
  definition: GraphDefinition;
  actors: Actors;
  memberId: string | null;
  disabled: boolean;
  /** False when this server has no chat-based authoring. */
  available: boolean;
  prefill: GraphComposerPrefill | null;
  onPrefillApplied: () => void;
  /** Sent as the first message once, e.g. from the new-graph entry page. */
  initialMessage?: string;
  onApplyTurn: (definition: GraphDefinition, turn: GraphAiTurn) => void;
}) {
  const { t } = useT("graphs");
  // A message typed before the conversation existed, sent by the conversation
  // once it mounts. Kept here so it survives the starter -> conversation swap.
  const [queued, setQueued] = useState<string | null>(null);
  if (!available)
    return (
      <div className="flex h-full items-center justify-center p-6 text-center">
        <p className="max-w-sm text-body leading-6 text-muted-foreground">
          {t(($) => $.studio.ux.ai_unavailable)}
        </p>
      </div>
    );
  return ai ? (
    <GraphConversation
      key={ai.sessionId}
      draftKey={draftKey}
      ai={ai}
      definition={definition}
      actors={actors}
      disabled={disabled}
      prefill={prefill}
      onPrefillApplied={onPrefillApplied}
      queued={queued}
      onQueuedSent={() => setQueued(null)}
      onApplyTurn={onApplyTurn}
    />
  ) : (
    <GraphConversationStarter
      draftKey={draftKey}
      actors={actors}
      memberId={memberId}
      disabled={disabled}
      prefill={prefill}
      onPrefillApplied={onPrefillApplied}
      initialMessage={initialMessage}
      onStarted={setQueued}
    />
  );
}

/** The executor and approver a new conversation would use. */
function useDefaultActors(actors: Actors, memberId: string | null) {
  const candidates = useMemo(
    () => graphBuilderCandidates(actors.agents),
    [actors.agents],
  );
  const firstReady =
    candidates.find((agent) =>
      graphAgentReady(agent, actors.runtimes, actors.currentUserId),
    )?.id ?? "";
  const defaultApprover =
    memberId && actors.members.some((member) => member.id === memberId)
      ? memberId
      : (actors.members[0]?.id ?? "");
  return { candidates, firstReady, defaultApprover };
}

function ActorPickers({
  candidates,
  members,
  executorId,
  approverId,
  onExecutor,
  onApprover,
  disabled,
}: {
  candidates: Agent[];
  members: MemberWithUser[];
  executorId: string;
  approverId: string;
  onExecutor: (id: string) => void;
  onApprover: (id: string) => void;
  disabled: boolean;
}) {
  const { t } = useT("graphs");
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-1.5">
      <div className="w-40 min-w-0">
        <GraphActorPicker
          id="graph-studio-executor"
          label={t(($) => $.studio.default_agent)}
          value={executorId}
          onChange={onExecutor}
          disabled={disabled}
          isAgent
          placeholder={t(($) => $.studio.choose_default_agent)}
          options={candidates.map((agent) => ({
            value: agent.id,
            label: agent.name,
            avatarUrl: agent.avatar_url,
          }))}
        />
      </div>
      <div className="w-36 min-w-0">
        <GraphActorPicker
          id="graph-studio-approver"
          label={t(($) => $.editor.approver)}
          value={approverId}
          onChange={onApprover}
          disabled={disabled}
          placeholder={t(($) => $.editor.choose_approver)}
          options={members.map((member) => ({
            value: member.id,
            label: member.name || member.email,
            avatarUrl: member.avatar_url,
          }))}
        />
      </div>
    </div>
  );
}

function GraphConversationStarter({
  draftKey,
  actors,
  memberId,
  disabled,
  prefill,
  onPrefillApplied,
  initialMessage,
  onStarted,
}: {
  draftKey: string;
  actors: Actors;
  memberId: string | null;
  disabled: boolean;
  prefill: GraphComposerPrefill | null;
  onPrefillApplied: () => void;
  initialMessage?: string;
  onStarted: (message: string) => void;
}) {
  const { t } = useT("graphs");
  const paths = useWorkspacePaths();
  const { candidates, firstReady, defaultApprover } = useDefaultActors(
    actors,
    memberId,
  );
  const [executorId, setExecutorId] = useState("");
  const [approverId, setApproverId] = useState("");
  const executorChoice = executorId || firstReady;
  const approverChoice = approverId || defaultApprover;
  const builder = useGraphBuilderSession({
    sessionId: "",
    encodeInput: (text) => text,
  });
  const executor = candidates.find((agent) => agent.id === executorChoice);
  const executorReady = executor
    ? graphAgentReady(executor, actors.runtimes, actors.currentUserId)
    : false;
  const hasOnlineRuntime = actors.runtimes.some(
    (runtime) => runtime.status === "online",
  );
  const blocker = actors.loading
    ? "loading"
    : !candidates.length
    ? "no_agents"
    : !hasOnlineRuntime
      ? "no_runtime"
      : !executorReady
        ? "executor"
        : !approverChoice
          ? "approver"
          : null;

  const start = async (text: string): Promise<boolean> => {
    if (blocker || disabled || !executor || !text.trim()) return false;
    const scope = graphScopeWithExecutor(
      executor.id,
      candidates.map((agent) => agent.id),
    );
    const session = await builder.start({
      defaultAgentId: executor.id,
      agentScope: scope,
      approverId: approverChoice,
    });
    if (!session) return false;
    const executorToUse = session.defaultAgentId || executor.id;
    onStarted(text.trim());
    useGraphDraftStore.getState().bindAi(draftKey, {
      sessionId: session.sessionId,
      agentId: session.builderAgentId,
      executorId: executorToUse,
      scopeIds: graphScopeWithExecutor(executorToUse, scope),
      approverId: session.approverId || approverChoice,
      runtimeId: session.runtimeId,
      appliedMessageId: null,
    });
    return true;
  };

  // The entry page hands over its first message; send it once actors load.
  // When something blocks the conversation, the text goes back into the
  // composer instead of being lost.
  const sentInitial = useRef(false);
  const [initialRestore, setInitialRestore] =
    useState<GraphComposerPrefill | null>(null);
  useEffect(() => {
    if (!initialMessage || sentInitial.current || blocker === "loading")
      return;
    sentInitial.current = true;
    if (blocker) {
      setInitialRestore({ id: "graph-entry", content: initialMessage });
      return;
    }
    void start(initialMessage).then((ok) => {
      if (!ok) setInitialRestore({ id: "graph-entry", content: initialMessage });
    });
    // `start` closes over the current choices, which is what should be sent.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initialMessage, blocker]);

  const examples = [
    t(($) => $.studio.example_research),
    t(($) => $.studio.example_release),
    t(($) => $.studio.example_content),
  ];
  const blockerText = {
    no_agents: t(($) => $.studio.no_agents),
    no_runtime: t(($) => $.studio.ux.no_runtime),
    executor: executor?.model.trim()
      ? t(($) => $.studio.builder.executor_offline)
      : t(($) => $.studio.builder.executor_no_model),
    approver: t(($) => $.studio.missing_approver),
  };

  return (
    <section className="flex h-full min-h-0 flex-col">
      <div className="min-h-0 flex-1 overflow-y-auto px-5 py-8">
        <div className="mx-auto w-full max-w-xl">
          <GraphUnfinishedSessions />
          <h3 className="text-balance text-title font-semibold">
            {t(($) => $.studio.builder.empty_title)}
          </h3>
          <p className="mt-2 text-pretty text-body leading-6 text-muted-foreground">
            {t(($) => $.studio.builder.empty_description)}
          </p>
          <div className="mt-5 flex flex-wrap gap-2">
            {examples.map((example) => (
              <button
                key={example}
                type="button"
                disabled={Boolean(blocker) || builder.starting}
                onClick={() => void start(example)}
                className="rounded-full border bg-background px-3 py-1.5 text-left text-caption transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
              >
                {example}
              </button>
            ))}
          </div>
          {blocker && blocker !== "loading" && (
            <div
              role="status"
              className="mt-6 rounded-lg border border-warning/40 bg-warning/5 px-3 py-2.5 text-caption leading-5"
            >
              <p>{blockerText[blocker]}</p>
              <p className="mt-1 flex flex-wrap gap-3">
                {blocker === "no_runtime" && (
                  <AppLink
                    href={paths.runtimes()}
                    className="underline underline-offset-4"
                  >
                    {t(($) => $.studio.builder.connect_runtime)}
                  </AppLink>
                )}
                {blocker === "executor" && executor && (
                  <AppLink
                    href={paths.agentDetail(executor.id)}
                    className="underline underline-offset-4"
                  >
                    {t(($) => $.studio.builder.open_agent)}
                  </AppLink>
                )}
                <span className="text-muted-foreground">
                  {t(($) => $.studio.ux.manual_hint)}
                </span>
              </p>
            </div>
          )}
        </div>
      </div>
      {builder.error && (
        <div
          role="alert"
          className="mx-5 mb-3 rounded-md bg-destructive/5 px-3 py-2 text-body text-destructive"
        >
          {builder.error}
        </div>
      )}
      <ChatInput
        onSend={async (content, _attachments, commitInput) => {
          const ok = await start(content);
          if (ok) commitInput();
          return ok;
        }}
        isRunning={builder.starting}
        disabled={disabled || Boolean(blocker) || builder.starting}
        agentName={t(($) => $.studio.builder.chat_title)}
        draftKeyOverride={`graph-builder:new:${draftKey}`}
        editorKeyOverride={`graph-builder:new:${draftKey}`}
        restoreDraftRequest={prefill ?? initialRestore}
        onRestoreDraftApplied={
          prefill ? onPrefillApplied : () => setInitialRestore(null)
        }
        leftAdornment={
          <ActorPickers
            candidates={candidates}
            members={actors.members}
            executorId={executorChoice}
            approverId={approverChoice}
            onExecutor={setExecutorId}
            onApprover={setApproverId}
            disabled={disabled || builder.starting}
          />
        }
      />
    </section>
  );
}

/**
 * One design conversation. A reply's draft is merged into the draft store the
 * moment the turn is over, so the canvas beside it is the draft itself, and
 * every turn is one undo step.
 *
 * Mounted with `key={sessionId}`, so every piece of state here belongs to
 * exactly one conversation and a remount is the only reset that cannot forget a
 * field.
 */
function GraphConversation({
  draftKey,
  ai,
  definition,
  actors,
  disabled,
  prefill,
  onPrefillApplied,
  queued,
  onQueuedSent,
  onApplyTurn,
}: {
  draftKey: string;
  ai: GraphDraftAi;
  definition: GraphDefinition;
  actors: Actors;
  disabled: boolean;
  prefill: GraphComposerPrefill | null;
  onPrefillApplied: () => void;
  queued: string | null;
  onQueuedSent: () => void;
  onApplyTurn: (definition: GraphDefinition, turn: GraphAiTurn) => void;
}) {
  const { t } = useT("graphs");
  const [confirmingExit, setConfirmingExit] = useState(false);
  const [mergeError, setMergeError] = useState<MergeError | null>(null);
  const { agents, runtimes, members, currentUserId } = actors;

  const directory = useMemo(
    () => graphBuilderDirectory(ai, agents, runtimes, currentUserId),
    [ai, agents, runtimes, currentUserId],
  );
  const runtime = runtimes.find((device) => device.id === ai.runtimeId) ?? null;
  const runtimeOnline = runtime?.status === "online";

  // Read through refs so the encoder and the apply callback stay current
  // without making the session hook or the merge effect depend on every render.
  const encodeRef = useRef({ definition, directory });
  encodeRef.current = { definition, directory };
  const applyRef = useRef(onApplyTurn);
  applyRef.current = onApplyTurn;

  const builder = useGraphBuilderSession({
    sessionId: ai.sessionId,
    encodeInput: (text) =>
      encodeGraphBuilderInput(
        text,
        encodeRef.current.definition,
        encodeRef.current.directory,
      ),
  });
  const { messages, pending } = builder;

  // The message typed before this conversation existed.
  const sentQueued = useRef(false);
  useEffect(() => {
    if (!queued || sentQueued.current) return;
    sentQueued.current = true;
    void builder.send(queued).finally(onQueuedSent);
  }, [builder, queued, onQueuedSent]);

  // The latest assistant reply, read through its scalar identity and content so
  // a realtime update that mutates the cached array in place still re-runs this.
  const latest = useMemo(() => {
    const reversed = [...messages].reverse();
    return reversed.find((message) => message.role === "assistant");
  }, [messages]);
  const latestId = latest?.id;
  const latestContent = latest?.content;
  const appliedMessageId = ai.appliedMessageId;

  useEffect(() => {
    // A reply that is still being written is not a malformed one: an unclosed
    // block is exactly what half a stream looks like, so no verdict is reached
    // until the turn is over.
    if (pending) return;
    if (!latestId || !latestContent) return;
    if (latestId === appliedMessageId) return;
    const store = useGraphDraftStore.getState();
    const payload = parseGraphDraft(latestContent);
    if (!payload) {
      // A turn that proposed nothing is a legitimate answer to a question; a
      // turn that opened a block and produced nothing usable is not.
      if (!hasGraphDraftBlock(latestContent)) return;
      store.markAiApplied(draftKey, latestId);
      setMergeError({ messageId: latestId, reason: "unparsable" });
      return;
    }
    const before = encodeRef.current.definition;
    const merged = mergeGraphDraft(before, payload, encodeRef.current.directory);
    // Recorded whether or not the merge succeeded: a rejected reply has been
    // judged, and retrying it on every render would never change the answer.
    store.markAiApplied(draftKey, latestId);
    if (merged.ok) {
      const changes = graphDefinitionChanges(before, merged.definition);
      applyRef.current(merged.definition, {
        messageId: latestId,
        added: changes.added,
        changed: changes.changed,
        removed: changes.removed,
        summary: merged.summary ?? "",
      });
      setMergeError(null);
      return;
    }
    setMergeError({
      messageId: latestId,
      reason: merged.reason,
      ...(merged.alias ? { alias: merged.alias } : {}),
      ...(merged.availableAliases
        ? { availableAliases: merged.availableAliases }
        : {}),
      ...(merged.detail ? { detail: merged.detail } : {}),
    });
  }, [appliedMessageId, draftKey, latestContent, latestId, pending]);

  useEffect(() => {
    if (builder.missing) useGraphDraftStore.getState().clearAi(draftKey);
  }, [builder.missing, draftKey]);

  const displayMessages = useMemo(
    () =>
      messages.map((message) => ({
        ...message,
        content:
          message.role === "user"
            ? decodeGraphBuilderInput(message.content)
            : stripGraphDraft(message.content),
      })),
    [messages],
  );

  const exit = async () => {
    if (!(await builder.destroy())) return;
    setConfirmingExit(false);
    useGraphDraftStore.getState().clearAi(draftKey);
  };

  const mergeText = mergeError
    ? {
        unparsable: t(($) => $.studio.builder.merge_unparsable),
        invalid: t(($) => $.studio.builder.merge_invalid),
        too_large: t(($) => $.studio.builder.merge_too_large),
        unknown_alias: t(($) => $.studio.builder.merge_unknown_alias, {
          alias: mergeError.alias ?? "",
          available: (mergeError.availableAliases ?? []).join(", "),
        }),
      }[mergeError.reason]
    : "";

  const candidates = useMemo(() => graphBuilderCandidates(agents), [agents]);

  return (
    <section className="flex h-full min-h-0 flex-col">
      <header className="flex min-h-11 shrink-0 items-center justify-between gap-3 border-b px-4 py-2">
        <div className="flex min-w-0 items-center gap-1.5 text-caption text-muted-foreground">
          <span
            className={cn(
              "size-2 rounded-full",
              runtimeOnline ? "bg-success" : "bg-muted-foreground/40",
            )}
            aria-hidden="true"
          />
          {runtimeOnline
            ? t(($) => $.studio.builder.runtime_online)
            : t(($) => $.studio.builder.runtime_offline)}
        </div>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          disabled={pending}
          onClick={() => setConfirmingExit(true)}
        >
          <MessageSquarePlus className="size-3.5" />
          {t(($) => $.studio.ux.new_conversation)}
        </Button>
      </header>

      {builder.messagesLoading ? (
        <ChatMessageSkeleton />
      ) : (
        <ChatMessageList
          messages={displayMessages}
          pendingTask={builder.pendingTask}
          availability={runtimeOnline ? "online" : "offline"}
          // Applies to the live stream as well as history, which is what
          // keeps half-written JSON off the screen mid-reply.
          transformContent={stripGraphDraft}
        />
      )}

      {mergeError && (
        <div
          role="alert"
          className="mx-4 mb-3 rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-2 text-caption leading-5 text-destructive"
        >
          <p className="break-words">{mergeText}</p>
          {mergeError.detail && (
            <p className="mt-1 break-words font-mono text-micro">
              {mergeError.detail}
            </p>
          )}
        </div>
      )}
      {builder.error ? (
        <div
          role="alert"
          aria-live="polite"
          className="mx-4 mb-3 rounded-md bg-destructive/5 px-3 py-2 text-body text-destructive"
        >
          {builder.error}
        </div>
      ) : null}

      <ChatInput
        onSend={(content, _attachmentIds, commitInput) =>
          builder.send(content, commitInput)
        }
        onStop={() => void builder.stop()}
        isRunning={pending}
        disabled={!runtimeOnline || disabled}
        agentName={t(($) => $.studio.builder.chat_title)}
        draftKeyOverride={`graph-builder:${ai.sessionId}`}
        editorKeyOverride={`graph-builder:${ai.sessionId}`}
        restoreDraftRequest={prefill ?? builder.restoreDraftRequest}
        onRestoreDraftApplied={
          prefill ? onPrefillApplied : builder.handleRestoreDraftApplied
        }
        leftAdornment={
          <ActorPickers
            candidates={candidates}
            members={members}
            executorId={ai.executorId}
            approverId={ai.approverId}
            // Aliases are positional; changing who reads as `agent_default`
            // mid-turn would let the reply in flight land on someone else.
            disabled={pending || disabled}
            onExecutor={(executorId) =>
              useGraphDraftStore.getState().updateAi(draftKey, {
                executorId,
                scopeIds: graphScopeWithExecutor(executorId, [
                  ...ai.scopeIds,
                  ...candidates.map((agent) => agent.id),
                ]).filter((id, index, all) => all.indexOf(id) === index),
              })
            }
            onApprover={(approverId) =>
              useGraphDraftStore.getState().updateAi(draftKey, { approverId })
            }
          />
        }
      />

      <AlertDialog
        open={confirmingExit}
        onOpenChange={(open) => {
          if (!open && !builder.closing) setConfirmingExit(false);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.studio.builder.exit_title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.studio.builder.exit_confirm)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={builder.closing}>
              {t(($) => $.studio.builder.exit_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={(event) => {
                // Keep the dialog up while the delete is in flight; it closes
                // from `exit` only once the server has accepted.
                event.preventDefault();
                void exit();
              }}
              disabled={builder.closing}
            >
              {t(($) => $.studio.builder.exit)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  );
}

/**
 * The design conversations this user left behind. Discarding is the only
 * action: what they were designing lived in a draft document the client no
 * longer has, and the alias directory cannot be reconstructed from the session.
 */
function GraphUnfinishedSessions() {
  const { t } = useT("graphs");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const sessions = useQuery(graphBuilderSessionsOptions(wsId));
  const [discarding, setDiscarding] = useState("");
  const rows = sessions.data ?? [];
  if (!rows.length) return null;

  const discard = async (sessionId: string) => {
    setDiscarding(sessionId);
    try {
      await api.deleteChatSession(sessionId);
    } catch {
      // Left in the list, which is the truth: it still exists.
    } finally {
      setDiscarding("");
      void qc.invalidateQueries({ queryKey: graphBuilderSessionKeys.list(wsId) });
    }
  };

  return (
    <div className="mb-6 rounded-xl border bg-muted/40 p-4">
      <p className="text-body font-medium">
        {t(($) => $.studio.builder.recovery_title)}
      </p>
      <p className="mt-1 text-caption leading-5 text-muted-foreground">
        {t(($) => $.studio.builder.recovery_hint)}
      </p>
      <ul className="mt-3 space-y-2">
        {rows.map((session) => (
          <li
            key={session.sessionId}
            className="flex items-center gap-3 rounded-lg border bg-background px-3 py-2"
          >
            <span className="min-w-0 flex-1">
              <span className="block truncate text-caption">
                {sessionPreview(session)}
              </span>
              {session.lastMessageAt && (
                <SessionTimestamp at={session.lastMessageAt} />
              )}
            </span>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              disabled={discarding === session.sessionId}
              onClick={() => void discard(session.sessionId)}
            >
              {t(($) => $.studio.builder.recovery_discard)}
            </Button>
          </li>
        ))}
      </ul>
    </div>
  );
}

function SessionTimestamp({ at }: { at: string }) {
  const timeAgo = useTimeAgo();
  return (
    <span className="mt-0.5 block text-micro text-muted-foreground">
      {timeAgo(at)}
    </span>
  );
}

function sessionPreview(
  session: Pick<
    GraphBuilderSessionSummary,
    "lastMessageContent" | "lastMessageRole"
  >,
): string {
  const content = session.lastMessageContent;
  if (!content) return "";
  return session.lastMessageRole === "user"
    ? decodeGraphBuilderInput(content)
    : stripGraphDraft(content);
}
