"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, MessageSquare, PenLine } from "lucide-react";
import { api } from "@multica/core/api";
import {
  decodeGraphBuilderInput,
  graphBuilderSessionKeys,
  graphBuilderSessionsOptions,
  stripGraphDraft,
  useGraphDraftStore,
  type GraphBuilderSessionSummary,
} from "@multica/core/graphs";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import type { Agent, MemberWithUser, RuntimeDevice } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { useT, useTimeAgo } from "../i18n";
import { AppLink } from "../navigation";
import {
  graphAgentReady,
  graphBuilderCandidates,
  graphScopeWithExecutor,
} from "./graph-ai-actors";
import { GraphActorMultiPicker, GraphActorPicker } from "./graph-actor-picker";
import { Field } from "./shared";
import { useGraphBuilderSession } from "./use-graph-builder-session";

/**
 * The step before a design conversation exists: who runs it, who it may assign
 * work to, and who approves.
 *
 * These three cannot be deferred. The first is frozen onto the hidden carrier
 * agent when the session is created, and all three become the alias directory
 * every later turn is written against — `agent_default` has to mean the same
 * teammate from the first reply to the last, so the selection is made once and
 * stored in the draft document rather than re-derived per turn.
 *
 * The counterpart of the agent studio's setup step, and the same shape: a form,
 * a start button, and a list of unfinished conversations so this screen is not a
 * dead end when the draft document is gone.
 */
export function GraphAiSetupPanel({
  draftKey,
  agents,
  runtimes,
  members,
  currentUserId,
  memberId,
  disabled,
  onManual,
}: {
  /** The draft document the conversation is bound to once it starts. */
  draftKey: string;
  agents: Agent[];
  runtimes: RuntimeDevice[];
  members: MemberWithUser[];
  /** For runtime usability — the user, not the membership row. */
  currentUserId: string | null;
  /** The caller's own membership row, the default approver. */
  memberId: string | null;
  /** The editor cannot author right now (archived, unsupported, read-only). */
  disabled?: boolean;
  /** Leaves AI mode for the editor, for someone who would rather build it. */
  onManual: () => void;
}) {
  const { t } = useT("graphs");
  const paths = useWorkspacePaths();
  const candidates = useMemo(() => graphBuilderCandidates(agents), [agents]);

  const [executorId, setExecutorId] = useState(""),
    [scopeIds, setScopeIds] = useState<string[]>([]),
    [approverId, setApproverId] = useState(""),
    [seeded, setSeeded] = useState(false);

  // One seeding pass, once the catalogue has arrived: the whole scope, and the
  // first executor that could actually run a conversation. Deliberately not a
  // reactive default — a teammate hired later must not move a choice the user
  // has already made, and an empty scope only means the list is still loading.
  useEffect(() => {
    if (seeded || !candidates.length) return;
    setSeeded(true);
    setScopeIds(candidates.map((agent) => agent.id));
    setExecutorId(
      candidates.find((agent) =>
        graphAgentReady(agent, runtimes, currentUserId),
      )?.id ?? "",
    );
  }, [candidates, currentUserId, runtimes, seeded]);

  useEffect(() => {
    if (approverId || !memberId) return;
    if (members.some((member) => member.id === memberId)) setApproverId(memberId);
  }, [approverId, memberId, members]);

  // Nothing is sent from this screen; the encoder only becomes reachable once a
  // conversation exists, and that is the workspace's instance of the hook.
  const builder = useGraphBuilderSession({
    sessionId: "",
    encodeInput: (text) => text,
  });

  const executor = candidates.find((agent) => agent.id === executorId) ?? null;
  // The conversation itself runs on the executor's runtime, so an executor that
  // cannot execute is refused here rather than by the server a moment later.
  const executorReady = executor
    ? graphAgentReady(executor, runtimes, currentUserId)
    : false;
  const executorMissingModel = !!executor && !executor.model.trim();
  const hasOnlineRuntime = runtimes.some(
    (runtime) => runtime.status === "online",
  );

  const startConversation = async () => {
    if (!executor || !executorReady || !approverId) return;
    const session = await builder.start({
      defaultAgentId: executor.id,
      agentScope: graphScopeWithExecutor(executor.id, scopeIds),
      approverId,
    });
    if (!session) return;
    const executorIdToUse = session.defaultAgentId || executor.id;
    useGraphDraftStore.getState().bindAi(draftKey, {
      sessionId: session.sessionId,
      agentId: session.builderAgentId,
      executorId: executorIdToUse,
      scopeIds: graphScopeWithExecutor(executorIdToUse, scopeIds),
      approverId: session.approverId || approverId,
      runtimeId: session.runtimeId,
      appliedMessageId: null,
    });
  };

  const agentOptions = candidates.map((agent) => ({
    value: agent.id,
    label: agent.name,
    avatarUrl: agent.avatar_url,
  }));

  return (
    <main className="flex min-h-0 flex-1 items-center justify-center overflow-y-auto px-5 py-10">
      <div className="w-full max-w-xl">
        <GraphUnfinishedSessions />
        <div className="rounded-xl border bg-card p-6 shadow-sm">
          <span className="flex size-11 items-center justify-center rounded-lg bg-primary/10 text-primary">
            <MessageSquare className="size-5" />
          </span>
          <h2 className="mt-5 text-title-lg font-semibold">
            {t(($) => $.studio.builder.setup_title)}
          </h2>
          <p className="mt-2 text-body leading-6 text-muted-foreground">
            {t(($) => $.studio.builder.setup_description)}
          </p>

          {candidates.length ? (
            <div className="mt-6 space-y-4">
              <Field label={t(($) => $.studio.default_agent)}>
                {(id) => (
                  <GraphActorPicker
                    id={id}
                    label={t(($) => $.studio.default_agent)}
                    value={executorId}
                    onChange={setExecutorId}
                    disabled={disabled || builder.starting}
                    isAgent
                    placeholder={t(($) => $.studio.choose_default_agent)}
                    options={agentOptions}
                  />
                )}
              </Field>
              {executor && !executorReady && (
                <p className="text-caption leading-5 text-destructive">
                  {executorMissingModel
                    ? t(($) => $.studio.builder.executor_no_model)
                    : t(($) => $.studio.builder.executor_offline)}{" "}
                  <AppLink
                    href={paths.agentDetail(executor.id)}
                    className="underline underline-offset-4"
                  >
                    {t(($) => $.studio.builder.open_agent)}
                  </AppLink>
                </p>
              )}
              <Field label={t(($) => $.studio.scope)}>
                {(id) => (
                  <GraphActorMultiPicker
                    id={id}
                    label={t(($) => $.studio.scope)}
                    values={scopeIds}
                    onChange={setScopeIds}
                    disabled={disabled || builder.starting}
                    isAgent
                    placeholder={t(($) => $.studio.choose_agents)}
                    options={agentOptions}
                  />
                )}
              </Field>
              <p className="text-caption leading-5 text-muted-foreground">
                {t(($) => $.studio.scope_hint)}
              </p>
              <Field label={t(($) => $.editor.approver)}>
                {(id) => (
                  <GraphActorPicker
                    id={id}
                    label={t(($) => $.editor.approver)}
                    value={approverId}
                    onChange={setApproverId}
                    disabled={disabled || builder.starting}
                    placeholder={t(($) => $.editor.choose_approver)}
                    options={members.map((member) => ({
                      value: member.id,
                      label: member.name || member.email,
                      avatarUrl: member.avatar_url,
                    }))}
                  />
                )}
              </Field>
            </div>
          ) : (
            <p className="mt-6 text-body leading-6 text-muted-foreground">
              {t(($) => $.studio.no_agents)}
            </p>
          )}

          {builder.error && (
            <div role="alert" className="mt-4 text-body text-destructive">
              {builder.error}
            </div>
          )}

          <div className="mt-6 flex flex-wrap items-center justify-between gap-3">
            <Button
              type="button"
              variant="ghost"
              className="-ml-2 text-muted-foreground"
              onClick={onManual}
            >
              <PenLine className="size-4" />
              {t(($) => $.studio.manual)}
            </Button>
            {!candidates.length ? null : !hasOnlineRuntime ? (
              <Button
                render={<AppLink href={paths.runtimes()} />}
                nativeButton={false}
              >
                {t(($) => $.studio.builder.connect_runtime)}
              </Button>
            ) : (
              <Button
                type="button"
                onClick={() => void startConversation()}
                disabled={
                  disabled ||
                  builder.starting ||
                  !executorReady ||
                  scopeIds.length === 0 ||
                  !approverId
                }
              >
                {builder.starting && (
                  <Loader2 className="size-4 animate-spin" />
                )}
                {t(($) => $.studio.builder.start)}
              </Button>
            )}
          </div>
        </div>
      </div>
    </main>
  );
}

/**
 * The design conversations this user left behind, on the one screen that would
 * otherwise silently start another one.
 *
 * Discarding is the only action, and that is the honest one: the conversation
 * is real and still running, but what it was designing lived in a draft
 * document the client no longer has, and the alias directory cannot be
 * reconstructed from the session — resuming would hand the model a directory
 * that does not match the one its own history was written against. What is left
 * is the handle that deletes a conversation together with its hidden carrier.
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
    <div className="mb-5 rounded-xl border bg-muted/40 p-4">
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

/**
 * The list carries the last message still in the wire format — the user side is
 * a JSON envelope holding the whole draft, the assistant side ends in a
 * `<graph_draft>` block — so both sides go through the same decoding the
 * conversation itself uses. Shown raw, every row is a wall of JSON.
 */
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
