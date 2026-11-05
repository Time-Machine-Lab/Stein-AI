"use client";

import { useEffect, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  AlertTriangle,
  Archive,
  Ban,
  Check,
  CheckCircle2,
  ChevronDown,
  Clock,
  ExternalLink,
  FileText,
  Flag,
  GitCommitHorizontal,
  ImageOff,
  Layers,
  Pause,
  Play,
  RotateCcw,
  ShieldCheck,
  Unlink,
  Upload,
  Wrench,
  X,
  type LucideIcon,
} from "lucide-react";
import {
  asSchema,
  graphArtifactProducer,
  graphEventNode,
  graphNodeTasksOptions,
  nodeCanRetry,
  type GraphNodeDefinition,
  type GraphNodeRun,
  type GraphRepositoryOption,
  type GraphRun,
  type GraphRunAction,
  type JsonObject,
} from "@multica/core/graphs";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import { TranscriptButton } from "../common/task-transcript";
import { api } from "@multica/core/api";
import { AppLink } from "../navigation";
import { useT } from "../i18n";
import { Field, JsonDetails } from "./shared";
import { GraphNodeIcon, useGraphNodeLabels } from "./graph-node-catalog";
import {
  CopyValueButton,
  GraphArtifactCards,
  GraphValueView,
  graphArtifactLink,
} from "./graph-result-view";
import { GraphRunStatePill } from "./graph-run-list";

function useDuration() {
  const { t } = useT("graphs");
  return (from?: string, to?: string) => {
    const start = Date.parse(from ?? "");
    if (Number.isNaN(start)) return "";
    const end = to ? Date.parse(to) : Date.now();
    const seconds = Math.max(0, Math.round((end - start) / 1000));
    if (seconds < 60) return t(($) => $.studio.ux.dur_seconds, { s: seconds });
    const minutes = Math.floor(seconds / 60);
    if (minutes < 60)
      return t(($) => $.studio.ux.dur_minutes, { m: minutes, s: seconds % 60 });
    return t(($) => $.studio.ux.dur_hours, {
      h: Math.floor(minutes / 60),
      m: minutes % 60,
    });
  };
}

/** A titled block of the record pane: heading, optional action, content. */
function RecordSection({
  title,
  action,
  children,
}: {
  title: string;
  action?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="min-w-0 space-y-3">
      <div className="flex items-center gap-2">
        <h3 className="min-w-0 flex-1 text-caption font-medium text-muted-foreground">
          {title}
        </h3>
        {action}
      </div>
      {children}
    </section>
  );
}

function EmptyNote({ children }: { children: ReactNode }) {
  return (
    <p className="rounded-lg border border-dashed px-4 py-8 text-center text-caption text-muted-foreground">
      {children}
    </p>
  );
}

/** Collapsed by default: what the reader opens when they need it. */
function Fold({ label, children }: { label: string; children: ReactNode }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="min-w-0">
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen((current) => !current)}
        className="flex w-full items-center gap-1.5 text-caption font-medium text-muted-foreground hover:text-foreground"
      >
        <ChevronDown
          className={cn("size-3.5 transition-transform", !open && "-rotate-90")}
        />
        {label}
      </button>
      {open && <div className="mt-3 min-w-0">{children}</div>}
    </div>
  );
}

/**
 * One step of a run: who did it and how it went in a header, then what it
 * produced, then the attempts and its input for whoever needs to dig.
 */
export function GraphStepRecord({
  wsId,
  node,
  definition,
  actorNames,
  canManage,
  pending,
  reason,
  onReasonChange,
  onAction,
}: {
  wsId: string;
  node: GraphNodeRun;
  definition?: GraphNodeDefinition;
  actorNames: Record<string, string>;
  canManage: boolean;
  pending: boolean;
  reason: string;
  onReasonChange: (reason: string) => void;
  onAction: (action: GraphRunAction, nodeRunId?: string) => void;
}) {
  const { t } = useT("graphs");
  const paths = useWorkspacePaths();
  const labels = useGraphNodeLabels();
  const duration = useDuration();
  const tasks = useQuery(graphNodeTasksOptions(wsId, node.issueId ?? ""));
  const name = node.label || definition?.label || node.nodeKey;
  const actor = definition?.agentId
    ? actorNames[definition.agentId]
    : undefined;
  const budget = node.maxAttempts + node.grantedAttempts;
  const working = ["agent", "script"].includes(node.nodeType);
  const latest = node.attempts.at(-1);
  const latestTask = latest
    ? tasks.data?.find((task) => task.id === latest.taskId)
    : undefined;
  const elapsed = node.startedAt
    ? duration(node.startedAt, node.finishedAt)
    : "";
  const fallbackOutput =
    !node.output &&
    node.submissions
      .map((submission) => asSchema(asSchema(submission.result).output))
      .find((output) => Object.keys(output).length);

  return (
    <article
      id={`graph-node-${node.id}`}
      data-testid="node-detail"
      className="min-w-0 space-y-6"
    >
      <header className="space-y-3">
        <div className="flex items-start gap-3">
          <GraphNodeIcon
            type={node.nodeType}
            className="size-9 rounded-lg [&>svg]:size-4"
          />
          <div className="min-w-0 flex-1">
            <h2 className="break-words text-title font-semibold leading-snug">
              {name}
            </h2>
            <p className="mt-0.5 truncate text-caption text-muted-foreground">
              {labels[node.nodeType] ?? node.nodeType}
              {actor && ` · ${actor}`}
            </p>
          </div>
          <GraphRunStatePill state={node.state} />
        </div>
        <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-caption text-muted-foreground">
          {working && (
            <span className="inline-flex items-center gap-1">
              <RotateCcw className="size-3.5" />
              {t(($) => $.run.attempts, { used: node.attempts.length, max: budget })}
            </span>
          )}
          {elapsed && (
            <span className="inline-flex items-center gap-1">
              <Clock className="size-3.5" />
              {elapsed}
            </span>
          )}
          {node.iteration ? (
            <span>
              {t(($) => $.production.iteration)} {node.iteration}
            </span>
          ) : null}
          {node.selectedPort && (
            <span className="rounded bg-muted px-1.5 py-0.5 font-medium text-foreground">
              {node.selectedPort === "true"
                ? t(($) => $.studio.ux.port_yes)
                : t(($) => $.studio.ux.port_no)}
            </span>
          )}
        </div>
        {(node.issueId || latestTask) && (
          <div className="flex flex-wrap gap-2">
            {latestTask && (
              <TranscriptButton
                task={latestTask}
                agentName={name}
                isLive={
                  latestTask.status === "running" ||
                  latestTask.status === "dispatched"
                }
                title={t(($) => $.run.view_transcript)}
              />
            )}
            {node.issueId && (
              <Button
                size="sm"
                variant="outline"
                render={<AppLink href={paths.issueDetail(node.issueId)} />}
                nativeButton={false}
              >
                <ExternalLink className="size-3.5" />
                {t(($) => $.run.node_issue)}
              </Button>
            )}
          </div>
        )}
      </header>

      {node.blockReason && (
        <div
          role="status"
          className="flex gap-2 rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-2.5 text-body"
        >
          <AlertTriangle className="mt-0.5 size-4 shrink-0 text-destructive" />
          <p className="min-w-0 break-words">
            {node.blockReason === "execution_unknown"
              ? t(($) => $.run.execution_unknown)
              : node.blockReason}
          </p>
        </div>
      )}

      {canManage && node.state === "blocked" && (
        <div className="space-y-3 rounded-lg border p-3">
          <Field label={t(($) => $.run.reason)}>
            {(id) => (
              <Textarea
                id={id}
                value={reason}
                onChange={(event) => onReasonChange(event.target.value)}
                placeholder={t(($) => $.run.reason_placeholder)}
                disabled={pending}
              />
            )}
          </Field>
          <div className="flex flex-wrap gap-2">
            {nodeCanRetry(node) && (
              <Button
                size="sm"
                disabled={pending}
                onClick={() => onAction("retry", node.id)}
              >
                {node.nodeType === "http"
                  ? t(($) => $.production.http_retry)
                  : t(($) => $.run.retry)}
              </Button>
            )}
            {node.nodeType !== "http" && node.canResolveBlock === true && (
              <Button
                variant="outline"
                size="sm"
                disabled={pending}
                onClick={() => onAction("resolve-block", node.id)}
              >
                {t(($) => $.run.resolve_block)}
              </Button>
            )}
          </div>
        </div>
      )}

      {node.output || fallbackOutput ? (
        <RecordSection
          title={t(($) => $.production.output)}
          action={<CopyValueButton value={node.output ?? fallbackOutput} />}
        >
          <GraphValueView
            value={node.output ?? fallbackOutput}
            schema={definition?.outputSchema}
          />
        </RecordSection>
      ) : (
        !node.blockReason && (
          <EmptyNote>{t(($) => $.studio.ux.no_output)}</EmptyNote>
        )
      )}

      {node.attempts.length > 0 && (
        <RecordSection title={t(($) => $.studio.ux.attempt_history)}>
          <ol className="divide-y rounded-lg border">
            {node.attempts.map((attempt) => {
              const task = tasks.data?.find(
                (candidate) => candidate.id === attempt.taskId,
              );
              return (
                <li
                  key={attempt.taskId}
                  className="flex flex-wrap items-center gap-3 px-3 py-2"
                >
                  <span className="text-caption font-medium tabular-nums">
                    #{attempt.attemptNo}
                  </span>
                  <GraphRunStatePill state={attempt.state} />
                  <span className="min-w-0 flex-1" />
                  {task && (
                    <TranscriptButton
                      task={task}
                      agentName={name}
                      isLive={
                        task.status === "running" ||
                        task.status === "dispatched"
                      }
                      title={t(($) => $.run.view_transcript)}
                    />
                  )}
                </li>
              );
            })}
          </ol>
        </RecordSection>
      )}

      {Object.keys(node.input).length > 0 && (
        <Fold label={t(($) => $.studio.ux.step_input)}>
          <GraphValueView value={node.input} />
        </Fold>
      )}
      <Fold label={t(($) => $.production.raw_details)}>
        <pre className="max-h-80 overflow-auto whitespace-pre-wrap break-all rounded-lg bg-muted/40 p-3 font-mono text-micro text-muted-foreground">
          {JSON.stringify(
            {
              node_run_id: node.id,
              input_hash: node.inputHash,
              attempts: node.attempts,
              submissions: node.submissions,
            },
            null,
            2,
          )}
        </pre>
      </Fold>
    </article>
  );
}

export function GraphRunResult({ run }: { run: GraphRun }) {
  const { t } = useT("graphs");
  if (!run.output) return <EmptyNote>{t(($) => $.studio.ux.no_result)}</EmptyNote>;
  return (
    <RecordSection
      title={t(($) => $.run.result)}
      action={<CopyValueButton value={run.output} />}
    >
      <div data-testid="run-result">
        <GraphValueView value={run.output} schema={run.definition?.outputSchema} />
      </div>
    </RecordSection>
  );
}

export function GraphRunInput({ run }: { run: GraphRun }) {
  const { t } = useT("graphs");
  if (!Object.keys(run.input ?? {}).length)
    return <EmptyNote>{t(($) => $.studio.ux.no_input)}</EmptyNote>;
  return (
    <RecordSection
      title={t(($) => $.studio.ux.input_given)}
      action={<CopyValueButton value={run.input} />}
    >
      <GraphValueView value={run.input} schema={run.definition?.inputSchema} />
    </RecordSection>
  );
}

const formatSize = (bytes: unknown) =>
  typeof bytes !== "number"
    ? ""
    : bytes < 1024 * 1024
      ? `${(bytes / 1024).toLocaleString(undefined, { maximumFractionDigits: 1 })} KB`
      : `${(bytes / 1024 / 1024).toLocaleString(undefined, { maximumFractionDigits: 1 })} MB`;

/**
 * A loadable URL for an attachment image. The download endpoint is
 * auth-gated by the API token, which a native <img> cannot send, so the
 * bytes come through the authenticated client and render from an object URL.
 */
function useAttachmentImageURL(id: string): string {
  const blob = useQuery({
    queryKey: ["graph-artifact-blob", id],
    queryFn: () => api.getAttachmentBlob(id),
    enabled: Boolean(id),
    staleTime: Infinity,
    gcTime: 5 * 60 * 1000,
  });
  const [url, setUrl] = useState("");
  useEffect(() => {
    if (!blob.data) return;
    const next = URL.createObjectURL(blob.data);
    setUrl(next);
    return () => URL.revokeObjectURL(next);
  }, [blob.data]);
  return url;
}

function ArtifactThumb({
  artifact,
  producer,
}: {
  artifact: JsonObject;
  producer?: string;
}) {
  const paths = useWorkspacePaths();
  const id = String(artifact.id ?? "");
  const src = useAttachmentImageURL(id);
  const [broken, setBroken] = useState(false);
  const name =
    typeof artifact.name === "string" ? artifact.name : String(artifact.media_type ?? "");
  return (
    <AppLink
      href={paths.attachmentPreview(id)}
      className="group block min-w-0 overflow-hidden rounded-lg border bg-card transition-shadow hover:shadow-md focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
    >
      <span className="flex aspect-[4/3] items-center justify-center overflow-hidden bg-muted">
        {broken ? (
          <ImageOff className="size-5 text-muted-foreground" />
        ) : !src ? (
          <span className="size-full animate-pulse bg-muted" />
        ) : (
          <img
            src={src}
            alt={name}
            loading="lazy"
            onError={() => setBroken(true)}
            className="size-full object-cover object-top transition-transform group-hover:scale-[1.02]"
          />
        )}
      </span>
      <span className="block px-2.5 py-2">
        <span className="block truncate text-caption font-medium">
          {producer ?? name}
        </span>
        <span className="block truncate text-micro text-muted-foreground">
          {[String(artifact.media_type ?? "").replace("image/", "").toUpperCase(), formatSize(artifact.size)]
            .filter(Boolean)
            .join(" · ")}
        </span>
      </span>
    </AppLink>
  );
}

/**
 * Images as a gallery, files and commits as rows, each saying which step
 * made it. The raw JSON is in the Raw tab, not under every card.
 */
export function GraphRunArtifacts({
  run,
  repositories,
}: {
  run: GraphRun;
  repositories: GraphRepositoryOption[];
}) {
  const { t } = useT("graphs");
  const paths = useWorkspacePaths();
  const producerName = (artifact: JsonObject) => {
    const node = graphArtifactProducer(run, artifact.producer_task_id);
    return node ? node.label || node.nodeKey : undefined;
  };
  const images = run.artifacts.filter(
    (artifact) =>
      artifact.type === "attachment_ref" &&
      typeof artifact.id === "string" &&
      String(artifact.media_type ?? "").startsWith("image/"),
  );
  const files = run.artifacts.filter(
    (artifact) => artifact.type === "attachment_ref" && !images.includes(artifact),
  );
  const commits = run.artifacts.filter((artifact) => artifact.type === "git_commit");
  const other = run.artifacts.filter(
    (artifact) =>
      !images.includes(artifact) &&
      !files.includes(artifact) &&
      !commits.includes(artifact),
  );
  if (!run.artifacts.length)
    return <EmptyNote>{t(($) => $.studio.ux.no_artifacts)}</EmptyNote>;
  return (
    <div className="min-w-0 space-y-6">
      {images.length > 0 && (
        <RecordSection
          title={t(($) => $.studio.ux.artifact_images, { count: images.length })}
        >
          <div className="grid grid-cols-2 gap-3">
            {images.map((artifact) => (
              <ArtifactThumb
                key={String(artifact.id)}
                artifact={artifact}
                producer={producerName(artifact)}
              />
            ))}
          </div>
        </RecordSection>
      )}
      {(files.length > 0 || commits.length > 0) && (
        <RecordSection
          title={t(($) => $.studio.ux.artifact_files, {
            count: files.length + commits.length,
          })}
        >
          <ul className="divide-y rounded-lg border">
            {[...commits, ...files].map((artifact, index) => {
              const commit = artifact.type === "git_commit";
              const Icon = commit ? GitCommitHorizontal : FileText;
              const href = commit
                ? graphArtifactLink(artifact, repositories)
                : typeof artifact.id === "string"
                  ? paths.attachmentPreview(artifact.id)
                  : undefined;
              const title = commit
                ? String(artifact.commit_sha ?? "").slice(0, 12)
                : typeof artifact.name === "string"
                  ? artifact.name
                  : String(artifact.media_type ?? artifact.type ?? "");
              const sub = [
                commit
                  ? (repositories.find((repo) => repo.id === artifact.repository_id)
                      ?.name ?? String(artifact.managed_ref ?? ""))
                  : formatSize(artifact.size),
                producerName(artifact),
              ]
                .filter(Boolean)
                .join(" · ");
              const body = (
                <>
                  <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
                    <Icon className="size-4" />
                  </span>
                  <span className="min-w-0 flex-1">
                    <span
                      className={cn(
                        "block truncate text-body",
                        commit && "font-mono",
                      )}
                    >
                      {title}
                    </span>
                    {sub && (
                      <span className="block truncate text-caption text-muted-foreground">
                        {sub}
                      </span>
                    )}
                  </span>
                  {href && <ExternalLink className="size-3.5 shrink-0 text-muted-foreground" />}
                </>
              );
              const row = "flex items-center gap-3 px-3 py-2.5";
              return (
                <li key={String(artifact.id ?? index)}>
                  {!href ? (
                    <div className={row}>{body}</div>
                  ) : commit ? (
                    <a
                      href={href}
                      target="_blank"
                      rel="noopener noreferrer"
                      className={cn(row, "hover:bg-accent/50")}
                    >
                      {body}
                    </a>
                  ) : (
                    <AppLink href={href} className={cn(row, "hover:bg-accent/50")}>
                      {body}
                    </AppLink>
                  )}
                </li>
              );
            })}
          </ul>
        </RecordSection>
      )}
      {other.length > 0 && (
        <GraphArtifactCards artifacts={other} repositories={repositories} />
      )}
    </div>
  );
}

type EventStyle = { icon: LucideIcon; tone: string };
const EVENT_STYLE: Record<string, EventStyle> = {
  "run.started": { icon: Flag, tone: "bg-primary/10 text-primary" },
  "scope.started": { icon: Layers, tone: "bg-muted text-muted-foreground" },
  "attempt.created": { icon: Play, tone: "bg-info/10 text-info" },
  "node.submitted": { icon: Upload, tone: "bg-muted text-muted-foreground" },
  "node.accepted": { icon: CheckCircle2, tone: "bg-success/10 text-success" },
  "approval.requested": { icon: ShieldCheck, tone: "bg-warning/15 text-warning" },
  "run.succeeded": { icon: Check, tone: "bg-success/10 text-success" },
  "run.failed": { icon: X, tone: "bg-destructive/10 text-destructive" },
  "run.rejected": { icon: X, tone: "bg-destructive/10 text-destructive" },
  "run.cancelled": { icon: Ban, tone: "bg-muted text-muted-foreground" },
  "run.cancel": { icon: Ban, tone: "bg-muted text-muted-foreground" },
  "run.stopping": { icon: Ban, tone: "bg-muted text-muted-foreground" },
  "run.pause": { icon: Pause, tone: "bg-warning/15 text-warning" },
  "run.resume": { icon: Play, tone: "bg-info/10 text-info" },
  "run.retry": { icon: RotateCcw, tone: "bg-info/10 text-info" },
  "run.resolve-block": { icon: Wrench, tone: "bg-info/10 text-info" },
  "run.archive": { icon: Archive, tone: "bg-muted text-muted-foreground" },
  "issue.detached": { icon: Unlink, tone: "bg-muted text-muted-foreground" },
};
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/**
 * The run's history as sentences: what happened, to which step, by whom.
 * Event codes and reference ids stay in the Raw tab.
 */
export function GraphRunEvents({
  run,
  actorNames,
}: {
  run: GraphRun;
  actorNames: Record<string, string>;
}) {
  const { t } = useT("graphs");
  const rootScope = String(
    run.scopes?.find((scope) => !scope.parent_node_run_id)?.id ?? "",
  );
  const describe = (event: JsonObject) => {
    const type = String(event.type ?? event.event_type ?? event.kind ?? "");
    const message = typeof event.message === "string" ? event.message : "";
    const node = graphEventNode(run, message);
    const step = node ? node.label || node.nodeKey : "";
    const actorId = typeof event.actor_id === "string" ? event.actor_id : "";
    const actor = actorNames[actorId] ?? "";
    const someone = actor || t(($) => $.studio.ux.ev_someone);
    let note = !node && message && !UUID.test(message) ? message : "";
    let title: string;
    switch (type) {
      case "run.started":
        title = t(($) => $.studio.ux.ev_run_started);
        break;
      case "scope.started":
        title = t(($) => $.studio.ux.ev_scope_started);
        note = "";
        break;
      case "attempt.created":
        title = t(($) => $.studio.ux.ev_attempt, { step });
        break;
      case "node.submitted":
        title = t(($) => $.studio.ux.ev_submitted, { step });
        break;
      case "node.accepted":
        title = t(($) => $.studio.ux.ev_accepted, { step });
        break;
      case "approval.requested":
        title = t(($) => $.studio.ux.ev_approval_requested, { step });
        break;
      case "approval.vote": {
        const [memberId, decision] = message.split(":");
        const voter = actorNames[memberId ?? ""] || someone;
        title =
          decision === "rejected"
            ? t(($) => $.studio.ux.ev_vote_rejected, { actor: voter })
            : t(($) => $.studio.ux.ev_vote_approved, { actor: voter });
        note = "";
        break;
      }
      case "run.succeeded":
        title = t(($) => $.studio.ux.ev_succeeded);
        break;
      case "run.failed":
        title = t(($) => $.studio.ux.ev_failed);
        break;
      case "run.rejected":
        title = t(($) => $.studio.ux.ev_rejected);
        break;
      case "run.cancelled":
        title = t(($) => $.studio.ux.ev_cancelled);
        break;
      case "run.stopping":
        title = t(($) => $.studio.ux.ev_stopping);
        break;
      case "run.pause":
        title = t(($) => $.studio.ux.ev_paused, { actor: someone });
        break;
      case "run.resume":
        title = t(($) => $.studio.ux.ev_resumed, { actor: someone });
        break;
      case "run.cancel":
        title = t(($) => $.studio.ux.ev_cancel, { actor: someone });
        break;
      case "run.retry":
        title = t(($) => $.studio.ux.ev_retry, { actor: someone, step });
        break;
      case "run.resolve-block":
        title = t(($) => $.studio.ux.ev_resolved, { actor: someone });
        break;
      case "run.archive":
        title = t(($) => $.studio.ux.ev_archived, { actor: someone });
        break;
      case "issue.detached":
        title = t(($) => $.studio.ux.ev_detached, { actor: someone });
        break;
      default:
        title = type || t(($) => $.production.events);
    }
    return { type, title, note, node };
  };

  // The root scope opening is the run starting; only nested ones are news.
  const events = run.events.filter(
    (event) =>
      !(event.type === "scope.started" && event.message === rootScope) &&
      !(event.type === "scope.started" && run.events.indexOf(event) === 0),
  );
  if (!events.length)
    return <EmptyNote>{t(($) => $.studio.ux.no_events)}</EmptyNote>;

  let lastDay = "";
  return (
    <ol className="relative min-w-0">
      {events.map((event, index) => {
        const { type, title, note } = describe(event);
        const style = EVENT_STYLE[type] ?? {
          icon: Clock,
          tone: "bg-muted text-muted-foreground",
        };
        const Icon = style.icon;
        const at =
          typeof event.created_at === "string" ? new Date(event.created_at) : null;
        const day = at?.toLocaleDateString() ?? "";
        const showDay = day && day !== lastDay;
        lastDay = day || lastDay;
        const last = index === events.length - 1;
        return (
          <li key={String(event.id ?? index)} className="min-w-0">
            {showDay && (
              <p className="mb-2 mt-1 text-micro font-medium text-muted-foreground first:mt-0">
                {at?.toLocaleDateString(undefined, {
                  year: "numeric",
                  month: "long",
                  day: "numeric",
                })}
              </p>
            )}
            <div className="relative flex gap-3 pb-4">
              {!last && (
                <span
                  aria-hidden="true"
                  className="absolute left-3 top-7 h-[calc(100%-1.75rem)] w-px bg-border"
                />
              )}
              <span
                className={cn(
                  "relative flex size-6 shrink-0 items-center justify-center rounded-full",
                  style.tone,
                )}
              >
                <Icon className="size-3.5" />
              </span>
              <div className="min-w-0 flex-1 pt-0.5">
                <div className="flex items-baseline gap-2">
                  <p className="min-w-0 flex-1 break-words text-body">{title}</p>
                  <time
                    className="shrink-0 text-micro tabular-nums text-muted-foreground"
                    dateTime={at?.toISOString()}
                    title={at?.toLocaleString()}
                  >
                    {at?.toLocaleTimeString(undefined, {
                      hour: "2-digit",
                      minute: "2-digit",
                      second: "2-digit",
                    })}
                  </time>
                </div>
                {note && (
                  <p className="mt-1 line-clamp-3 break-words rounded-md bg-muted/50 px-2 py-1 text-caption text-muted-foreground">
                    {note}
                  </p>
                )}
              </div>
            </div>
          </li>
        );
      })}
    </ol>
  );
}

function RawRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center gap-3 px-3 py-2">
      <dt className="w-24 shrink-0 text-caption text-muted-foreground">{label}</dt>
      <dd className="min-w-0 flex-1 truncate font-mono text-caption">{value || "—"}</dd>
      {value && <CopyValueButton value={value} />}
    </div>
  );
}

export function GraphRunRaw({ run }: { run: GraphRun }) {
  const { t } = useT("graphs");
  return (
    <div className="min-w-0 space-y-6">
      <RecordSection title={t(($) => $.studio.ux.identifiers)}>
        <dl className="divide-y rounded-lg border">
          <RawRow label={t(($) => $.studio.ux.raw_run)} value={run.id} />
          <RawRow label={t(($) => $.studio.ux.raw_version)} value={run.versionId} />
          <RawRow label={t(($) => $.studio.ux.raw_issue)} value={run.rootIssueId} />
          <RawRow label={t(($) => $.studio.ux.raw_revision)} value={String(run.revision)} />
          <RawRow label={t(($) => $.studio.ux.raw_hash)} value={run.inputHash} />
        </dl>
      </RecordSection>
      <RecordSection title={t(($) => $.studio.ux.raw_json)}>
        <div className="space-y-2">
          <JsonDetails label={t(($) => $.run.input)} value={run.input} />
          {run.output && (
            <JsonDetails label={t(($) => $.run.result)} value={run.output} />
          )}
          {run.artifacts.length > 0 && (
            <JsonDetails label={t(($) => $.run.artifacts)} value={run.artifacts} />
          )}
          <JsonDetails label={t(($) => $.production.events)} value={run.events} />
        </div>
      </RecordSection>
    </div>
  );
}
