"use client";

import { useDeferredValue, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  AlertTriangle,
  Ban,
  Check,
  ChevronRight,
  CircleDashed,
  Loader2,
  Pause,
  Play,
  Search,
  X,
  type LucideIcon,
} from "lucide-react";
import {
  graphControlState,
  graphRunDurationMs,
  graphRunSearchOptions,
  type GraphRun,
} from "@multica/core/graphs";
import { useWorkspacePaths } from "@multica/core/paths";
import { memberListOptions } from "@multica/core/workspace/queries";
import type { MemberWithUser } from "@multica/core/types";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { AppLink } from "../navigation";
import { useT, useTimeAgo } from "../i18n";
import { GraphError, useGraphStateLabels } from "./shared";

/** Filters offered as chips, most actionable first. */
const STATE_FILTERS = [
  "",
  "waiting_approval",
  "blocked",
  "active",
  "failed",
  "succeeded",
  "rejected",
  "cancelled",
] as const;

export const RUN_STATE_DOT: Record<string, string> = {
  succeeded: "bg-success",
  active: "bg-info",
  running: "bg-info",
  waiting_approval: "bg-warning",
  blocked: "bg-warning",
  failed: "bg-destructive",
  rejected: "bg-destructive",
  cancelled: "bg-muted-foreground/40",
};

/** A run state as a coloured pill with an icon, readable without colour. */
const RUN_STATE_PILL: Record<string, { className: string; icon: LucideIcon }> = {
  succeeded: { className: "bg-success/10 text-success", icon: Check },
  active: { className: "bg-info/10 text-info", icon: Loader2 },
  waiting_approval: { className: "bg-warning/15 text-warning", icon: Pause },
  blocked: { className: "bg-warning/15 text-warning", icon: AlertTriangle },
  failed: { className: "bg-destructive/10 text-destructive", icon: X },
  rejected: { className: "bg-destructive/10 text-destructive", icon: Ban },
  cancelled: { className: "bg-muted text-muted-foreground", icon: Ban },
};

export function GraphRunStateDot({
  state,
  className,
}: {
  state: string;
  className?: string;
}) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        "inline-block size-2 shrink-0 rounded-full",
        RUN_STATE_DOT[state] ?? "bg-muted-foreground/40",
        className,
      )}
    />
  );
}

export function GraphRunStatePill({ state }: { state: string }) {
  const labels = useGraphStateLabels();
  const pill = RUN_STATE_PILL[state] ?? {
    className: "bg-muted text-muted-foreground",
    icon: CircleDashed,
  };
  const Icon = pill.icon;
  return (
    <span
      className={cn(
        "inline-flex max-w-full items-center gap-1 rounded-full px-2 py-0.5 text-caption font-medium",
        pill.className,
      )}
    >
      <Icon
        className={cn("size-3 shrink-0", state === "active" && "animate-spin")}
        aria-hidden="true"
      />
      <span className="truncate">{labels[state] ?? state}</span>
    </span>
  );
}

export function GraphPager({
  next,
  cursors,
  setCursors,
}: {
  next?: string;
  cursors: string[];
  setCursors: (value: string[]) => void;
}) {
  const { t } = useT("graphs");
  if (!next && !cursors.length) return null;
  return (
    <div className="flex justify-end gap-2">
      <Button
        variant="outline"
        size="sm"
        disabled={!cursors.length}
        onClick={() => setCursors(cursors.slice(0, -1))}
      >
        {t(($) => $.production.previous)}
      </Button>
      <Button
        variant="outline"
        size="sm"
        disabled={!next}
        onClick={() => {
          if (next) setCursors([...cursors, next]);
        }}
      >
        {t(($) => $.production.next)}
      </Button>
    </div>
  );
}

function useDurationFormat() {
  const { t } = useT("graphs");
  return (ms: number | null) => {
    if (ms === null) return "—";
    const seconds = Math.round(ms / 1000);
    if (seconds < 60) return t(($) => $.studio.ux.dur_seconds, { s: seconds });
    const minutes = Math.floor(seconds / 60);
    if (minutes < 60)
      return t(($) => $.studio.ux.dur_minutes, {
        m: minutes,
        s: seconds % 60,
      });
    return t(($) => $.studio.ux.dur_hours, {
      h: Math.floor(minutes / 60),
      m: minutes % 60,
    });
  };
}

/** "Today", "Yesterday" or a date, for the group a run is listed under. */
function useDayLabel() {
  const { t } = useT("graphs");
  return (iso: string) => {
    const date = new Date(iso);
    if (Number.isNaN(date.getTime())) return "";
    const day = new Date(date.getFullYear(), date.getMonth(), date.getDate());
    const now = new Date();
    const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
    const diff = Math.round((today.getTime() - day.getTime()) / 86_400_000);
    if (diff === 0) return t(($) => $.studio.ux.today);
    if (diff === 1) return t(($) => $.studio.ux.yesterday);
    return date.toLocaleDateString(undefined, {
      month: "long",
      day: "numeric",
      ...(date.getFullYear() !== now.getFullYear() ? { year: "numeric" } : {}),
    });
  };
}

const ROW_GRID =
  "grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-4 gap-y-1 md:grid-cols-[7.5rem_minmax(0,1fr)_9rem_6.5rem_6rem_1rem]";

function RunRow({
  run,
  showGraph,
  initiator,
}: {
  run: GraphRun;
  showGraph: boolean;
  initiator?: MemberWithUser;
}) {
  const { t } = useT("graphs");
  const paths = useWorkspacePaths();
  const timeAgo = useTimeAgo();
  const duration = useDurationFormat();
  const stateLabels = useGraphStateLabels();
  const controlState = graphControlState(run);
  const started = run.createdAt ? new Date(run.createdAt) : null;
  const live = !["succeeded", "failed", "rejected", "cancelled"].includes(
    run.state,
  );
  return (
    <li>
      <AppLink
        href={paths.graphRun(run.id)}
        className={cn(
          ROW_GRID,
          "group px-4 py-3 transition-colors hover:bg-accent/50 focus-visible:bg-accent/50 focus-visible:outline-none",
        )}
      >
        <span className="order-2 justify-self-end md:order-none md:justify-self-start">
          <GraphRunStatePill state={run.state} />
        </span>
        <span className="order-1 min-w-0 md:order-none">
          <span className="flex min-w-0 items-center gap-2">
            <span className="truncate text-body font-medium">
              {showGraph
                ? run.graphName || t(($) => $.run.title)
                : t(($) => $.studio.ux.run_label, { id: run.id.slice(0, 8) })}
            </span>
            {showGraph && (
              <span className="shrink-0 font-mono text-micro text-muted-foreground">
                #{run.id.slice(0, 8)}
              </span>
            )}
            {controlState && (
              <span className="shrink-0 rounded bg-muted px-1.5 text-micro text-muted-foreground">
                {stateLabels[controlState] ?? controlState}
              </span>
            )}
          </span>
          {run.blockReason && ["blocked", "failed"].includes(run.state) ? (
            <span className="mt-0.5 block truncate text-caption text-destructive">
              {run.blockReason}
            </span>
          ) : run.rootTitle ? (
            <span className="mt-0.5 block truncate text-caption text-muted-foreground">
              {t(($) => $.studio.ux.for_issue, { title: run.rootTitle })}
            </span>
          ) : null}
        </span>
        <span className="hidden min-w-0 items-center gap-2 md:flex">
          {initiator ? (
            <>
              <ActorAvatar
                name={initiator.name || initiator.email}
                initials={Array.from(initiator.name || initiator.email)
                  .slice(0, 2)
                  .join("")}
                avatarUrl={initiator.avatar_url}
                size="sm"
              />
              <span className="truncate text-caption">
                {initiator.name || initiator.email}
              </span>
            </>
          ) : (
            <span className="text-caption text-muted-foreground">
              {run.initiatorId ? "—" : t(($) => $.studio.ux.started_by_trigger)}
            </span>
          )}
        </span>
        <span
          className="hidden text-caption tabular-nums text-muted-foreground md:block"
          title={started?.toLocaleString()}
        >
          {started
            ? started.toLocaleTimeString(undefined, {
                hour: "2-digit",
                minute: "2-digit",
              })
            : ""}
          <span className="block text-micro">
            {run.createdAt ? timeAgo(run.createdAt) : ""}
          </span>
        </span>
        <span
          className={cn(
            "hidden text-caption tabular-nums md:block",
            live ? "text-info" : "text-muted-foreground",
          )}
        >
          {duration(graphRunDurationMs(run))}
        </span>
        <ChevronRight className="hidden size-4 text-muted-foreground transition-transform group-hover:translate-x-0.5 md:block" />
      </AppLink>
    </li>
  );
}

function Stat({
  label,
  value,
  hint,
}: {
  label: string;
  value: string;
  hint?: string;
}) {
  return (
    <div className="min-w-0 px-4 py-3">
      <dt className="truncate text-caption text-muted-foreground">{label}</dt>
      <dd className="mt-1 flex min-w-0 items-baseline gap-2">
        <span className="truncate text-title font-semibold tabular-nums">
          {value}
        </span>
        {hint && (
          <span className="truncate text-caption tabular-nums text-muted-foreground">
            {hint}
          </span>
        )}
      </dd>
    </div>
  );
}

/**
 * Runs as a table you can scan: outcome, what ran and for which issue, who
 * started it, when, and how long it took, grouped by day. State chips filter
 * and count; a short summary line replaces the old wall of statistic cards.
 */
export function GraphRunList({
  wsId,
  graphId,
  onRun,
}: {
  wsId: string;
  graphId?: string;
  /** Offered from the empty state when the list belongs to one graph. */
  onRun?: () => void;
}) {
  const { t } = useT("graphs");
  const stateLabels = useGraphStateLabels();
  const dayLabel = useDayLabel();
  const duration = useDurationFormat();
  const [state, setState] = useState("");
  const [search, setSearch] = useState("");
  const [cursors, setCursors] = useState<string[]>([]);
  const query = useDeferredValue(search);
  const members = useQuery(memberListOptions(wsId));
  const runs = useQuery(
    graphRunSearchOptions(wsId, {
      q: query,
      state,
      graphId,
      cursor: cursors.at(-1),
      limit: 30,
    }),
  );
  // The counts belong to the unfiltered set, so they come from a request that
  // ignores the state filter; otherwise every other chip would read zero.
  const totals = useQuery(
    graphRunSearchOptions(wsId, { q: query, graphId, limit: 1 }),
  );
  const stats = totals.data?.stats;
  const counts: Record<string, number> = stats
    ? {
        "": stats.total,
        waiting_approval: stats.waitingApproval,
        blocked: stats.blocked,
        active: stats.active,
        failed: stats.failed,
        succeeded: stats.succeeded,
        rejected: stats.rejected,
        cancelled: stats.cancelled,
      }
    : {};
  const byUser = useMemo(() => {
    const map = new Map<string, MemberWithUser>();
    for (const member of members.data ?? []) {
      map.set(member.user_id, member);
      map.set(member.id, member);
    }
    return map;
  }, [members.data]);
  const groups = useMemo(() => {
    const result: { label: string; runs: GraphRun[] }[] = [];
    for (const run of runs.data?.items ?? []) {
      const label = dayLabel(run.createdAt);
      const last = result.at(-1);
      if (last?.label === label) last.runs.push(run);
      else result.push({ label, runs: [run] });
    }
    return result;
    // dayLabel is a fresh closure each render but depends only on the locale.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [runs.data?.items]);
  const finished = stats
    ? stats.succeeded + stats.failed + stats.rejected + stats.cancelled
    : 0;

  return (
    <div className="space-y-5">
      <GraphError error={runs.error} />
      {stats && stats.total > 0 && (
        <dl className="grid grid-cols-2 divide-x divide-y rounded-xl border bg-card sm:grid-cols-4 sm:divide-y-0">
          <Stat
            label={t(($) => $.studio.ux.stat_runs)}
            value={stats.total.toLocaleString()}
          />
          <Stat
            label={t(($) => $.studio.ux.stat_success)}
            value={
              finished
                ? `${Math.round((stats.succeeded / finished) * 100)}%`
                : "—"
            }
          />
          <Stat
            label={t(($) => $.studio.ux.stat_time)}
            value={duration(stats.totalDurationMs)}
          />
          <Stat
            label={t(($) => $.studio.ux.stat_cost)}
            value={new Intl.NumberFormat(undefined, {
              notation: "compact",
            }).format(stats.totalTokens)}
            hint={new Intl.NumberFormat(undefined, {
              style: "currency",
              currency: "USD",
              maximumFractionDigits: 2,
            }).format(stats.totalCostUsd)}
          />
        </dl>
      )}
      <div className="flex flex-wrap items-center gap-3">
        <div className="relative w-full sm:w-64">
          <Search className="pointer-events-none absolute left-3 top-2.5 size-4 text-muted-foreground" />
          <Input
            className="pl-9"
            aria-label={t(($) => $.studio.ux.search_runs)}
            placeholder={t(($) => $.studio.ux.search_runs)}
            value={search}
            onChange={(event) => {
              setSearch(event.target.value);
              setCursors([]);
            }}
          />
        </div>
        <div
          className="flex flex-wrap gap-1.5"
          role="group"
          aria-label={t(($) => $.production.all_states)}
        >
          {STATE_FILTERS.filter(
            (value) => !value || value === state || (counts[value] ?? 0) > 0,
          ).map((value) => (
            <button
              key={value || "all"}
              type="button"
              aria-pressed={state === value}
              onClick={() => {
                setState(value);
                setCursors([]);
              }}
              className={cn(
                "flex h-8 items-center gap-1.5 rounded-lg border px-2.5 text-caption transition-colors",
                state === value
                  ? "border-foreground bg-foreground font-medium text-background"
                  : "bg-background hover:bg-accent",
              )}
            >
              {value && <GraphRunStateDot state={value} />}
              {value ? (stateLabels[value] ?? value) : t(($) => $.studio.ux.all)}
              {counts[value] !== undefined && (
                <span className="tabular-nums">
                  {counts[value].toLocaleString()}
                </span>
              )}
            </button>
          ))}
        </div>
      </div>
      {runs.isPending ? (
        <div className="space-y-2 rounded-xl border bg-card p-4">
          {[0, 1, 2, 3].map((index) => (
            <Skeleton key={index} className="h-10" />
          ))}
        </div>
      ) : !runs.data?.items.length ? (
        <div className="flex flex-col items-center rounded-xl border border-dashed px-6 py-14 text-center">
          <span className="flex size-10 items-center justify-center rounded-full bg-muted text-muted-foreground">
            <Play className="size-4" />
          </span>
          <p className="mt-3 text-body font-medium">
            {query || state
              ? t(($) => $.studio.ux.no_matching_runs)
              : t(($) => $.page.empty_runs)}
          </p>
          {!query && !state && onRun && (
            <>
              <p className="mt-1 text-caption text-muted-foreground">
                {t(($) => $.studio.ux.empty_runs_hint)}
              </p>
              <Button className="mt-4" size="sm" onClick={onRun}>
                <Play className="size-3.5" />
                {t(($) => $.studio.ux.run_once)}
              </Button>
            </>
          )}
        </div>
      ) : (
        <div className="overflow-hidden rounded-xl border bg-card">
          <div
            className={cn(
              ROW_GRID,
              "hidden border-b bg-muted/40 px-4 py-2 text-caption text-muted-foreground md:grid",
            )}
          >
            <span>{t(($) => $.studio.ux.col_state)}</span>
            <span>{t(($) => $.studio.ux.col_run)}</span>
            <span>{t(($) => $.studio.ux.col_by)}</span>
            <span>{t(($) => $.studio.ux.col_started)}</span>
            <span>{t(($) => $.studio.ux.col_duration)}</span>
            <span />
          </div>
          {groups.map((group) => (
            <section key={group.label}>
              <h3 className="sticky top-0 z-[1] border-b bg-card/95 px-4 py-1.5 text-micro font-medium text-muted-foreground backdrop-blur">
                {group.label}
              </h3>
              <ul className="divide-y">
                {group.runs.map((run) => (
                  <RunRow
                    key={run.id}
                    run={run}
                    showGraph={!graphId}
                    initiator={byUser.get(run.initiatorId)}
                  />
                ))}
              </ul>
            </section>
          ))}
        </div>
      )}
      <GraphPager
        next={runs.data?.nextCursor}
        cursors={cursors}
        setCursors={setCursors}
      />
    </div>
  );
}
