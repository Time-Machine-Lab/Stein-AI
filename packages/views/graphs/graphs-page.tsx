"use client";

import { useDeferredValue, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowRight, GitBranch, Plus, Search } from "lucide-react";
import {
  graphControlState,
  graphSearchOptions,
  graphRunSearchOptions,
} from "@multica/core/graphs";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { NativeSelect } from "@multica/ui/components/ui/native-select";
import { AppLink, useNavigation } from "../navigation";
import { CollectionPageHeader } from "../layout/collection-page";
import { useT } from "../i18n";
import { GraphError, GraphState, useGraphStateLabels } from "./shared";

function Pager({
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
export function GraphsPage() {
  const { t } = useT("graphs"),
    wsId = useWorkspaceId(),
    paths = useWorkspacePaths(),
    navigation = useNavigation();
  const [search, setSearch] = useState(""),
    [state, setState] = useState("");
  const [graphCursors, setGraphCursors] = useState<string[]>([]),
    [runCursors, setRunCursors] = useState<string[]>([]);
  const q = useDeferredValue(search),
    stateLabels = useGraphStateLabels();
  const graphs = useQuery(
    graphSearchOptions(wsId, { q, cursor: graphCursors.at(-1), limit: 12 }),
  );
  const runs = useQuery(
    graphRunSearchOptions(wsId, {
      q,
      state,
      cursor: runCursors.at(-1),
      limit: 20,
    }),
  );
  const stats = runs.data?.stats,
    issueId = navigation.searchParams.get("issue");
  const metrics = stats
    ? [
        [t(($) => $.production.total), stats.total.toLocaleString()],
        [t(($) => $.production.active), stats.active.toLocaleString()],
        [t(($) => $.production.blocked), stats.blocked.toLocaleString()],
        [t(($) => $.production.failed), stats.failed.toLocaleString()],
        [
          t(($) => $.production.duration),
          `${(stats.totalDurationMs / 60000).toLocaleString(undefined, { maximumFractionDigits: 1 })} min`,
        ],
        [
          t(($) => $.production.cost),
          new Intl.NumberFormat(undefined, {
            style: "currency",
            currency: "USD",
            maximumFractionDigits: 3,
          }).format(stats.totalCostUsd),
        ],
        [t(($) => $.production.tokens), stats.totalTokens.toLocaleString()],
      ]
    : [];
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <CollectionPageHeader
        icon={GitBranch}
        title={t(($) => $.page.title)}
        description={t(($) => $.page.description)}
        actions={
          <Button size="sm" onClick={() => navigation.push(paths.graphNew())}>
            <Plus className="size-4" />
            {t(($) => $.page.new)}
          </Button>
        }
      />
      <div className="flex-1 space-y-8 overflow-auto px-5 py-6 sm:px-8">
        <GraphError error={graphs.error ?? runs.error} />
        <div className="flex flex-wrap gap-3">
          <div className="relative min-w-48 flex-1">
            <Search className="pointer-events-none absolute left-3 top-2.5 size-4 text-muted-foreground" />
            <Input
              className="pl-9"
              aria-label={t(($) => $.production.search)}
              placeholder={t(($) => $.production.search)}
              value={search}
              onChange={(event) => {
                setSearch(event.target.value);
                setGraphCursors([]);
                setRunCursors([]);
              }}
            />
          </div>
          <NativeSelect
            aria-label={t(($) => $.production.all_states)}
            value={state}
            onChange={(event) => {
              setState(event.target.value);
              setRunCursors([]);
            }}
          >
            <option value="">{t(($) => $.production.all_states)}</option>
            {[
              "active",
              "waiting_approval",
              "blocked",
              "succeeded",
              "failed",
              "rejected",
              "cancelled",
            ].map((value) => (
              <option key={value} value={value}>
                {stateLabels[value] ?? value}
              </option>
            ))}
          </NativeSelect>
        </div>
        {metrics.length > 0 && (
          <dl className="grid grid-cols-2 gap-3 sm:grid-cols-4 xl:grid-cols-7">
            {metrics.map(([label, value]) => (
              <div key={label} className="rounded-xl border bg-card p-4">
                <dt className="truncate text-caption text-muted-foreground">
                  {label}
                </dt>
                <dd className="mt-2 truncate text-title font-semibold tabular-nums">
                  {value}
                </dd>
              </div>
            ))}
          </dl>
        )}
        {Boolean(stats?.unpricedTasks) && (
          <p className="text-caption text-muted-foreground">
            {t(($) => $.production.unpriced_tasks, {
              count: stats!.unpricedTasks,
            })}
          </p>
        )}
        <section className="space-y-4">
          <div className="flex items-center justify-between">
            <h2 className="text-title font-semibold">
              {t(($) => $.page.templates)}
            </h2>
            <span className="text-caption text-muted-foreground">
              {graphs.data?.total ?? 0}
            </span>
          </div>
          {graphs.isPending ? (
            <p>{t(($) => $.page.loading)}</p>
          ) : !graphs.data?.items.length ? (
            <div className="rounded-xl border border-dashed p-8 text-center text-body text-muted-foreground">
              {t(($) => $.page.empty_templates)}
            </div>
          ) : (
            <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
              {graphs.data.items.map((graph) => (
                <AppLink
                  key={graph.id}
                  href={`${paths.graphDetail(graph.id)}${issueId ? `?issue=${encodeURIComponent(issueId)}` : ""}`}
                  className="group min-w-0 space-y-4 rounded-xl border bg-card p-5 transition-colors hover:border-primary/30 hover:bg-accent/30"
                >
                  <div className="flex items-start gap-3">
                    <span className="rounded-lg bg-primary/10 p-2 text-primary">
                      <GitBranch className="size-4" />
                    </span>
                    <h3 className="min-w-0 flex-1 truncate text-body font-semibold">
                      {graph.name}
                    </h3>
                    <ArrowRight className="size-4 shrink-0 text-muted-foreground" />
                  </div>
                  <div className="flex flex-wrap gap-2 text-caption text-muted-foreground">
                    <span>
                      {t(($) => $.page.revision, { revision: graph.revision })}
                    </span>
                    {graph.archivedAt && (
                      <span>{t(($) => $.page.archived)}</span>
                    )}
                    <span className="ml-auto">
                      {graph.updatedAt
                        ? new Date(graph.updatedAt).toLocaleDateString()
                        : ""}
                    </span>
                  </div>
                </AppLink>
              ))}
            </div>
          )}
          <Pager
            next={graphs.data?.nextCursor}
            cursors={graphCursors}
            setCursors={setGraphCursors}
          />
        </section>
        <section className="space-y-4">
          <div className="flex items-center justify-between">
            <h2 className="text-title font-semibold">
              {t(($) => $.page.runs)}
            </h2>
            <span className="text-caption text-muted-foreground">
              {runs.data?.total ?? 0}
            </span>
          </div>
          {runs.isPending ? (
            <p>{t(($) => $.page.loading)}</p>
          ) : !runs.data?.items.length ? (
            <p className="text-body text-muted-foreground">
              {t(($) => $.page.empty_runs)}
            </p>
          ) : (
            <div className="overflow-hidden rounded-xl border">
              {runs.data.items.map((run) => {
                const controlState = graphControlState(run);
                return (
                  <AppLink
                    key={run.id}
                    href={paths.graphRun(run.id)}
                    className="flex flex-wrap items-center gap-3 border-b p-4 last:border-b-0 hover:bg-accent/40"
                  >
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-body font-medium">
                        {run.graphName ||
                          graphs.data?.items.find(
                            (graph) => graph.id === run.graphId,
                          )?.name ||
                          run.id.slice(0, 8)}
                      </p>
                      <p className="mt-1 text-caption text-muted-foreground">
                        {run.createdAt
                          ? new Date(run.createdAt).toLocaleString()
                          : ""}
                      </p>
                    </div>
                    <GraphState state={run.state} />
                    {controlState && <GraphState state={controlState} />}
                    <ArrowRight className="size-4 text-muted-foreground" />
                  </AppLink>
                );
              })}
            </div>
          )}
          <Pager
            next={runs.data?.nextCursor}
            cursors={runCursors}
            setCursors={setRunCursors}
          />
        </section>
      </div>
    </div>
  );
}
