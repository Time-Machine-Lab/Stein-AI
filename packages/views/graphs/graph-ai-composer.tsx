"use client";

import {
  ArrowRight,
  Bot,
  CheckCheck,
  GitBranch,
  Loader2,
  PenLine,
  Search,
  Sparkles,
} from "lucide-react";
import type { Agent, MemberWithUser } from "@multica/core/types";
import type { GraphGenerationMode } from "@multica/core/graphs";
import { errorCode } from "@multica/core/api/client";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../i18n";
import { Field } from "./shared";
import { GraphActorPicker } from "./graph-actor-picker";

/**
 * The one-shot form: describe the workflow, hand it to one agent, get one
 * proposal back.
 *
 * This is the path a deployment without a builder still uses — the server
 * generates from its own LLM client and the result arrives as a proposal to
 * apply or discard. Where a builder exists the editor never renders this, and
 * the conversation replaces both the form and the apply step.
 */
export function GraphAiComposer({
  prompt,
  onPromptChange,
  mode,
  defaultAgentId,
  onDefaultAgentChange,
  approverId,
  onApproverChange,
  agents,
  members,
  loading,
  disabled,
  pending,
  error,
  hasDraft,
  onGenerate,
  onManual,
}: {
  prompt: string;
  onPromptChange: (value: string) => void;
  mode: GraphGenerationMode;
  defaultAgentId: string;
  onDefaultAgentChange: (value: string) => void;
  approverId: string;
  onApproverChange: (value: string) => void;
  agents: Agent[];
  members: MemberWithUser[];
  /** The generation-options query is still in flight. */
  loading: boolean;
  /** The editor cannot start authoring right now. */
  disabled?: boolean;
  /** The request is in flight. */
  pending: boolean;
  /** The last rejected request, from before a task existed. */
  error: unknown;
  hasDraft: boolean;
  onGenerate: () => void;
  onManual: () => void;
}) {
  const { t } = useT("graphs");
  const examples = [
    t(($) => $.studio.example_research),
    t(($) => $.studio.example_release),
    t(($) => $.studio.example_content),
  ];
  const agentOptions = agents.map((agent) => ({
    value: agent.id,
    label: agent.name,
    avatarUrl: agent.avatar_url,
  }));
  const locked = pending;
  // The request is the only source of failure on this path: one synchronous
  // call, so there is no task whose status could also report one.
  const requestCode = errorCode(error);
  const failureCode = requestCode;
  const failureText =
    error
      ? (() => {
          switch (failureCode) {
            case "generation_timeout":
              return t(($) => $.studio.error_timeout);
            case "generation_invalid":
              return t(($) => $.studio.error_invalid);
            case "generation_busy":
              return t(($) => $.studio.error_busy);
            case "generation_unavailable":
              return t(($) => $.studio.unavailable);
            case "capability_missing":
              return t(($) => $.studio.error_capability);
            case "agent_unavailable":
              return t(($) => $.studio.error_agent);
            case "forbidden":
              return t(($) => $.studio.error_forbidden);
            case "generation_failed":
              return t(($) => $.studio.error_failed);
            default:
              // A newer backend can report a code this build does not know.
              // Show what the server said rather than inventing copy for it.
              return (
                (error instanceof Error ? error.message : "") ||
                t(($) => $.studio.error_failed)
              );
          }
        })()
      : "";
  return (
    <div className="mx-auto flex w-full max-w-6xl flex-1 flex-col justify-center px-5 py-8 sm:px-8 lg:py-12">
      <div className="mb-8 max-w-2xl">
        <div className="mb-4 inline-flex items-center gap-2 rounded-full border bg-card px-3 py-1 text-caption font-medium text-muted-foreground">
          <Sparkles className="size-3.5 text-primary" />
          {t(($) => $.studio.eyebrow)}
        </div>
        <h2 className="text-balance text-display-sm font-semibold tracking-tight sm:text-display">
          {t(($) => $.studio.title)}
        </h2>
        <p className="mt-3 max-w-xl text-pretty text-body leading-6 text-muted-foreground">
          {t(($) => $.studio.description)}
        </p>
      </div>
      <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,1.6fr)_minmax(260px,1fr)] lg:gap-10">
        <section className="min-w-0">
          <div className="overflow-hidden rounded-2xl border bg-card shadow-sm focus-within:border-primary/40">
            <div className="p-5 sm:p-6">
              <Field label={t(($) => $.studio.prompt)}>
                {(id) => (
                  <Textarea
                    id={id}
                    value={prompt}
                    onChange={(event) => onPromptChange(event.target.value)}
                    placeholder={t(($) => $.studio.placeholder)}
                    maxLength={12000}
                    disabled={locked}
                    className="min-h-40 resize-y border-0 bg-transparent px-0 text-body leading-7 shadow-none focus-visible:ring-0 dark:bg-transparent"
                  />
                )}
              </Field>
              <div className="mt-4 grid gap-4 sm:grid-cols-2">
                <Field label={t(($) => $.studio.agent)}>
                  {(id) => (
                    <GraphActorPicker
                      id={id}
                      label={t(($) => $.studio.agent)}
                      value={defaultAgentId}
                      onChange={onDefaultAgentChange}
                      disabled={locked}
                      isAgent
                      placeholder={t(($) => $.editor.choose_agent)}
                      options={agentOptions}
                    />
                  )}
                </Field>
                <Field label={t(($) => $.editor.approver)}>
                  {(id) => (
                    <GraphActorPicker
                      id={id}
                      label={t(($) => $.editor.approver)}
                      value={approverId}
                      onChange={onApproverChange}
                      disabled={locked}
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
              <p className="mt-3 text-caption leading-5 text-muted-foreground">
                {!agents.length
                  ? t(($) => $.studio.no_agents)
                  : t(($) => $.studio.agent_hint)}
              </p>
            </div>
            <div className="flex flex-wrap items-center justify-between gap-3 border-t bg-muted/20 px-5 py-4 sm:px-6">
              <p className="max-w-64 text-caption leading-5 text-muted-foreground">
                {t(($) => $.studio.review_hint)}
              </p>
              <Button
                type="button"
                disabled={
                  mode === "unavailable" ||
                  loading ||
                  locked ||
                  disabled ||
                  !prompt.trim() ||
                  !defaultAgentId ||
                  !approverId
                }
                onClick={onGenerate}
              >
                {locked ? (
                  <Loader2 className="size-4 animate-spin" />
                ) : (
                  <Sparkles className="size-4" />
                )}
                {locked
                  ? t(($) => $.studio.generating)
                  : t(($) => $.studio.generate)}
              </Button>
            </div>
          </div>
          {failureText && (
            <div
              role="alert"
              className="mt-3 rounded-lg border border-destructive/30 bg-destructive/5 px-4 py-3 text-caption leading-5 text-destructive"
            >
              <p className="break-words">{failureText}</p>
              {failureCode === "generation_busy" && (
                <p className="mt-1 break-words">
                  {t(($) => $.studio.retry_hint)}
                </p>
              )}
            </div>
          )}
          {!loading && mode === "unavailable" && !disabled && (
            <p className="mt-3 rounded-lg border border-warning/25 bg-warning/5 px-4 py-3 text-caption leading-5 text-muted-foreground">
              {t(($) => $.studio.unavailable)}
            </p>
          )}
          {hasDraft && (
            <p className="mt-3 text-caption leading-5 text-muted-foreground">
              {t(($) => $.studio.replace_hint)}
            </p>
          )}
          <div className="mt-5">
            <p className="mb-2 text-caption text-muted-foreground">
              {t(($) => $.studio.try_example)}
            </p>
            <div className="flex flex-wrap gap-2">
              {examples.map((example) => (
                <button
                  key={example}
                  type="button"
                  disabled={locked}
                  onClick={() => onPromptChange(example)}
                  className="rounded-lg border px-3 py-2 text-left text-caption leading-5 transition-colors hover:border-primary/30 hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
                >
                  {example}
                </button>
              ))}
            </div>
          </div>
          <Button
            type="button"
            variant="ghost"
            className="mt-5 -ml-2 text-muted-foreground"
            onClick={onManual}
          >
            <PenLine className="size-4" />
            {t(($) => $.studio.manual)}
            <ArrowRight className="size-3.5" />
          </Button>
        </section>
        <aside className="rounded-2xl bg-muted/30 p-5 sm:p-6">
          <div className="mb-5 flex items-center gap-2 text-caption font-medium text-muted-foreground">
            <GitBranch className="size-4" />
            {t(($) => $.studio.preview_title)}
          </div>
          <div className="space-y-0">
            {[
              {
                icon: Search,
                title: t(($) => $.studio.preview_plan),
                detail: t(($) => $.studio.preview_plan_hint),
              },
              {
                icon: Bot,
                title: t(($) => $.studio.preview_work),
                detail: t(($) => $.studio.preview_work_hint),
              },
              {
                icon: CheckCheck,
                title: t(($) => $.studio.preview_review),
                detail: t(($) => $.studio.preview_review_hint),
              },
            ].map(({ icon: Icon, title, detail }, index) => (
              <div key={title}>
                {index > 0 && (
                  <div className="ml-5 h-5 border-l border-dashed border-border" />
                )}
                <div className="flex items-start gap-3 rounded-xl border bg-background p-3.5">
                  <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted">
                    <Icon className="size-4 text-muted-foreground" />
                  </span>
                  <div className="min-w-0">
                    <p className="text-body font-medium">{title}</p>
                    <p className="mt-1 text-caption leading-5 text-muted-foreground">
                      {detail}
                    </p>
                  </div>
                </div>
              </div>
            ))}
          </div>
          <p className="mt-5 text-caption leading-5 text-muted-foreground">
            {t(($) => $.studio.preview_hint)}
          </p>
        </aside>
      </div>
    </div>
  );
}
