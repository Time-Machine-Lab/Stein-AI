"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight } from "lucide-react";
import {
  graphControlState,
  graphRunProgress,
  graphRunSearchOptions,
  type GraphRun,
} from "@multica/core/graphs";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { AppLink } from "../navigation";
import { useT, useTimeAgo } from "../i18n";
import { GraphError, GraphState, useGraphStateLabels } from "./shared";

/** Filters offered as chips, most actionable first. */
const STATE_FILTERS = [
  "",
  "waiting_approval",
  "blocked",
  "failed",
  "active",
  "succeeded",
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

function RunRow({ run, showGraph }: { run: GraphRun; showGraph: boolean }) {
  const { t } = useT("graphs");
  const paths = useWorkspacePaths();
  const timeAgo = useTimeAgo();
  const controlState = graphControlState(run);
  const progress = run.nodes?.length ? graphRunProgress(run) : null;
  return (
    <AppLink
      href={paths.graphRun(run.id)}
      className="flex flex-wrap items-center gap-x-4 gap-y-2 border-b px-4 py-3 last:border-b-0 hover:bg-accent/40"
    >
      <GraphRunStateDot state={run.state} />
      <div className="min-w-0 flex-1">
        <p className="truncate text-body font-medium">
          {showGraph
            ? run.graphName || run.id.slice(0, 8)
            : t(($) => $.studio.ux.run_label, { id: run.id.slice(0, 8) })}
        </p>
        <p className="mt-0.5 text-caption text-muted-foreground">
          {run.createdAt ? timeAgo(run.createdAt) : ""}
        </p>
      </div>
      {progress && progress.total > 0 && (
        <div className="hidden w-32 items-center gap-2 sm:flex">
          <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-muted">
            <div
              className={cn(
                "h-full rounded-full",
                progress.failed ? "bg-destructive" : "bg-primary",
              )}
              style={{ width: `${(progress.done / progress.total) * 100}%` }}
            />
          </div>
          <span className="text-caption tabular-nums text-muted-foreground">
            {progress.done}/{progress.total}
          </span>
        </div>
      )}
      <GraphState state={run.state} />
      {controlState && <GraphState state={controlState} />}
      <ChevronRight className="size-4 text-muted-foreground" />
    </AppLink>
  );
}

/**
 * Runs, filtered by state, optionally for one graph. The state chips replace
 * the old row of statistic cards: the numbers people act on are filters.
 */
export function GraphRunList({
  wsId,
  graphId,
  query = "",
}: {
  wsId: string;
  graphId?: string;
  query?: string;
}) {
  const { t } = useT("graphs");
  const stateLabels = useGraphStateLabels();
  const [state, setState] = useState("");
  const [cursors, setCursors] = useState<string[]>([]);
  const runs = useQuery(
    graphRunSearchOptions(wsId, {
      q: query,
      state,
      graphId,
      cursor: cursors.at(-1),
      limit: 20,
    }),
  );
  const stats = runs.data?.stats;
  const counts: Record<string, number | undefined> = stats
    ? {
        "": stats.total,
        waiting_approval: stats.waitingApproval,
        blocked: stats.blocked,
        failed: stats.failed,
        active: stats.active,
        succeeded: stats.succeeded,
        cancelled: stats.cancelled,
      }
    : {};
  return (
    <div className="space-y-4">
      <GraphError error={runs.error} />
      <div className="flex flex-wrap gap-2" role="group">
        {STATE_FILTERS.map((value) => (
          <button
            key={value || "all"}
            type="button"
            aria-pressed={state === value}
            onClick={() => {
              setState(value);
              setCursors([]);
            }}
            className={cn(
              "flex items-center gap-1.5 rounded-full border px-3 py-1 text-caption transition-colors hover:bg-accent",
              state === value &&
                "border-primary bg-primary/5 font-medium text-foreground",
            )}
          >
            {value && <GraphRunStateDot state={value} />}
            {value ? (stateLabels[value] ?? value) : t(($) => $.studio.ux.all)}
            {counts[value] !== undefined && state === "" && (
              <span className="tabular-nums text-muted-foreground">
                {counts[value]?.toLocaleString()}
              </span>
            )}
          </button>
        ))}
      </div>
      {stats && (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.studio.ux.run_totals, {
            duration: (stats.totalDurationMs / 60000).toLocaleString(undefined, {
              maximumFractionDigits: 1,
            }),
            cost: new Intl.NumberFormat(undefined, {
              style: "currency",
              currency: "USD",
              maximumFractionDigits: 3,
            }).format(stats.totalCostUsd),
            tokens: stats.totalTokens.toLocaleString(),
          })}
          {Boolean(stats.unpricedTasks) &&
            ` · ${t(($) => $.production.unpriced_tasks, {
              count: stats.unpricedTasks ?? 0,
            })}`}
        </p>
      )}
      {runs.isPending ? (
        <p className="text-body text-muted-foreground">
          {t(($) => $.page.loading)}
        </p>
      ) : !runs.data?.items.length ? (
        <div className="rounded-xl border border-dashed p-8 text-center text-body text-muted-foreground">
          {t(($) => $.page.empty_runs)}
        </div>
      ) : (
        <div className="overflow-hidden rounded-xl border bg-card">
          {runs.data.items.map((run) => (
            <RunRow key={run.id} run={run} showGraph={!graphId} />
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
