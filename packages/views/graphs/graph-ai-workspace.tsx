"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useDefaultLayout } from "react-resizable-panels";
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
import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
} from "@multica/ui/components/ui/resizable";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import {
  decodeGraphBuilderInput,
  encodeGraphBuilderInput,
  hasGraphDraftBlock,
  mergeGraphDraft,
  parseGraphDraft,
  stripGraphDraft,
  useGraphDraftStore,
  type GraphDefinition,
  type GraphDraftAi,
  type GraphDraftMergeFailure,
} from "@multica/core/graphs";
import type { Agent, RuntimeDevice } from "@multica/core/types";
import { ChatInput } from "../chat/components/chat-input";
import {
  ChatMessageList,
  ChatMessageSkeleton,
} from "../chat/components/chat-message-list";
import { useT } from "../i18n";
import { graphBuilderDirectory } from "./graph-ai-actors";
import { GraphCanvas } from "./graph-canvas";
import { useGraphBuilderSession } from "./use-graph-builder-session";

interface MergeError {
  messageId: string;
  reason: GraphDraftMergeFailure;
  alias?: string;
  availableAliases?: string[];
  detail?: string;
}

/**
 * One design conversation: the chat on the left, the draft it is writing on the
 * right.
 *
 * The draft is merged into the graph draft store the moment a reply lands
 * (C3), so the canvas is not a preview of a proposal to accept or reject — it is
 * the draft, and saving it is the editor's existing job. That is the whole
 * difference from the one-shot pipeline this replaced: saying what to change is
 * the edit, and every turn is one undo step.
 *
 * Mounted with `key={sessionId}`, so every piece of state here belongs to
 * exactly one conversation and a remount is the only reset that cannot forget a
 * field.
 */
export function GraphAiWorkspace({
  draftKey,
  ai,
  definition,
  agents,
  runtimes,
  currentUserId,
  canUndo,
  onApplyDefinition,
  onOpenEditor,
  onMissing,
}: {
  draftKey: string;
  ai: GraphDraftAi;
  definition: GraphDefinition;
  agents: Agent[];
  runtimes: RuntimeDevice[];
  currentUserId: string | null;
  canUndo: boolean;
  /** Applies a merged definition, with the editor's validation resets. */
  onApplyDefinition: (definition: GraphDefinition) => void;
  onOpenEditor: () => void;
  /** The conversation no longer exists; the caller unbinds and starts over. */
  onMissing: () => void;
}) {
  const { t } = useT("graphs");
  const { defaultLayout, onLayoutChanged } = useDefaultLayout({
    id: "multica_graph_builder_layout",
  });
  const [confirmingExit, setConfirmingExit] = useState(false);
  const [summary, setSummary] = useState("");
  const [mergeError, setMergeError] = useState<MergeError | null>(null);

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
  const applyRef = useRef(onApplyDefinition);
  applyRef.current = onApplyDefinition;

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
    // until the turn is over. Without this, every streamed frame would be
    // judged — and marked applied — as unparsable JSON.
    if (pending) return;
    if (!latestId || !latestContent) return;
    if (latestId === appliedMessageId) return;
    const store = useGraphDraftStore.getState();
    const payload = parseGraphDraft(latestContent);
    if (!payload) {
      // A turn that proposed nothing is a legitimate answer to a question; a
      // turn that opened a block and produced nothing usable is not, and it is
      // recorded so the failure is reported once instead of on every frame.
      if (!hasGraphDraftBlock(latestContent)) return;
      store.markAiApplied(draftKey, latestId);
      setMergeError({ messageId: latestId, reason: "unparsable" });
      return;
    }
    const merged = mergeGraphDraft(
      encodeRef.current.definition,
      payload,
      encodeRef.current.directory,
    );
    // Recorded whether or not the merge succeeded: a rejected reply has been
    // judged, and retrying it on every render would never change the answer.
    store.markAiApplied(draftKey, latestId);
    if (merged.ok) {
      applyRef.current(merged.definition);
      setSummary(merged.summary ?? "");
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
    if (builder.missing) onMissing();
  }, [builder.missing, onMissing]);

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

  const examples = [
    t(($) => $.studio.example_research),
    t(($) => $.studio.example_release),
    t(($) => $.studio.example_content),
  ];

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

  return (
    <>
      {/* The group lives here, not on the route, so its two panels are its only
          children. A group whose children alternate between one and two panels
          cannot keep a single persisted layout straight. */}
      <ResizablePanelGroup
        orientation="horizontal"
        className="min-h-0 flex-1"
        defaultLayout={defaultLayout}
        onLayoutChanged={onLayoutChanged}
      >
        <ResizablePanel id="design" minSize="30%">
          <section className="flex h-full min-h-0 flex-col bg-background @container">
            <header className="flex min-h-14 shrink-0 items-center justify-between gap-4 border-b px-5 py-2.5">
              <div className="min-w-0">
                <h2 className="truncate text-body font-semibold">
                  {t(($) => $.studio.builder.chat_title)}
                </h2>
                <p className="truncate text-caption text-muted-foreground">
                  {t(($) => $.studio.builder.chat_hint)}
                </p>
              </div>
              <div className="flex shrink-0 items-center gap-1.5 text-caption text-muted-foreground">
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
            </header>

            {builder.messagesLoading ? (
              <ChatMessageSkeleton />
            ) : messages.length > 0 || pending ? (
              <ChatMessageList
                messages={displayMessages}
                pendingTask={builder.pendingTask}
                availability={runtimeOnline ? "online" : "offline"}
                // Applies to the live stream as well as history, which is what
                // keeps half-written JSON off the screen mid-reply.
                transformContent={stripGraphDraft}
              />
            ) : (
              <div className="flex min-h-0 flex-1 items-center justify-center overflow-y-auto px-5 py-8">
                <div className="w-full max-w-xl text-center">
                  <h3 className="text-balance text-title font-semibold">
                    {t(($) => $.studio.builder.empty_title)}
                  </h3>
                  <p className="mx-auto mt-2 max-w-md text-pretty text-body leading-6 text-muted-foreground">
                    {t(($) => $.studio.builder.empty_description)}
                  </p>
                  <div className="mt-5 flex flex-wrap justify-center gap-2">
                    {examples.map((example) => (
                      <button
                        key={example}
                        type="button"
                        onClick={() => void builder.send(example)}
                        className="rounded-full border bg-background px-3 py-1.5 text-caption transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                      >
                        {example}
                      </button>
                    ))}
                  </div>
                </div>
              </div>
            )}

            {builder.error ? (
              <div
                role="alert"
                aria-live="polite"
                className="mx-5 mb-3 rounded-md bg-destructive/5 px-3 py-2 text-body text-destructive"
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
              disabled={!runtimeOnline}
              agentName={t(($) => $.studio.builder.chat_title)}
              draftKeyOverride={`graph-builder:${ai.sessionId}`}
              editorKeyOverride={`graph-builder:${ai.sessionId}`}
              restoreDraftRequest={builder.restoreDraftRequest}
              onRestoreDraftApplied={builder.handleRestoreDraftApplied}
            />
          </section>
        </ResizablePanel>

        <ResizableHandle />

        <ResizablePanel
          id="draft"
          defaultSize={480}
          minSize={360}
          groupResizeBehavior="preserve-pixel-size"
        >
          <div className="flex h-full min-h-0 flex-col border-l bg-muted/10">
            <header className="flex min-h-14 shrink-0 items-center justify-between gap-4 border-b px-5 py-2.5">
              <div className="min-w-0">
                <h2 className="truncate text-body font-semibold">
                  {t(($) => $.studio.builder.canvas_title)}
                </h2>
                <p className="truncate text-caption text-muted-foreground">
                  {t(($) => $.studio.builder.canvas_hint)}
                </p>
              </div>
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={!canUndo || pending}
                onClick={() => useGraphDraftStore.getState().undo(draftKey)}
              >
                {t(($) => $.studio.builder.undo)}
              </Button>
            </header>
            <div className="min-h-0 flex-1 overflow-y-auto p-4">
              {summary && (
                <p className="mb-3 rounded-lg border bg-background px-3 py-2 text-caption leading-5">
                  {summary}
                </p>
              )}
              {mergeError && (
                <div
                  role="alert"
                  className="mb-3 rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-2 text-caption leading-5 text-destructive"
                >
                  <p className="break-words">{mergeText}</p>
                  {mergeError.detail && (
                    <p className="mt-1 break-words font-mono text-micro">
                      {mergeError.detail}
                    </p>
                  )}
                </div>
              )}
              <div className="h-[min(560px,70vh)] min-h-72">
                <GraphCanvas definition={definition} disabled />
              </div>
            </div>
            <footer className="flex flex-wrap items-center justify-between gap-3 border-t px-5 py-3">
              <p className="text-caption text-muted-foreground">
                {t(($) => $.studio.builder.footer_hint)}
              </p>
              <div className="flex flex-wrap items-center gap-2">
                <Button
                  type="button"
                  variant="outline"
                  disabled={pending}
                  onClick={() => setConfirmingExit(true)}
                >
                  {t(($) => $.studio.builder.exit)}
                </Button>
                <Button type="button" onClick={onOpenEditor}>
                  {t(($) => $.studio.builder.open_editor)}
                </Button>
              </div>
            </footer>
          </div>
        </ResizablePanel>
      </ResizablePanelGroup>

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
    </>
  );
}
