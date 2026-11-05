"use client";

import { useDeferredValue, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { GitBranch, Play, Plus, Search } from "lucide-react";
import {
  graphRunSearchOptions,
  graphSearchOptions,
  groupLatestRuns,
  type GraphRecord,
  type GraphRun,
} from "@multica/core/graphs";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@multica/ui/components/ui/tabs";
import { cn } from "@multica/ui/lib/utils";
import { AppLink, useNavigation } from "../navigation";
import { CollectionPageHeader } from "../layout/collection-page";
import { useT, useTimeAgo } from "../i18n";
import { GraphError, useGraphStateLabels } from "./shared";
import { GraphNodeIcon } from "./graph-node-catalog";
import {
  GraphPager,
  GraphRunList,
  GraphRunStateDot,
  RUN_STATE_DOT,
} from "./graph-run-list";

/** Up to five step icons: a graph's shape at a glance. */
function GraphShape({ graph }: { graph: GraphRecord }) {
  const steps = (graph.definition?.nodes ?? []).filter(
    (node) => !["start", "end"].includes(node.type),
  );
  return (
    <span className="flex items-center gap-0.5" aria-hidden="true">
      {steps.slice(0, 5).map((node) => (
        <GraphNodeIcon key={node.nodeKey} type={node.type} className="size-5" />
      ))}
      {steps.length > 5 && (
        <span className="ps-1 text-micro text-muted-foreground">
          +{steps.length - 5}
        </span>
      )}
    </span>
  );
}

/** The last runs as bars, oldest first, coloured by outcome. */
function RecentRuns({ runs }: { runs: GraphRun[] }) {
  const stateLabels = useGraphStateLabels();
  return (
    <span className="flex h-5 items-end gap-0.5">
      {[...runs].reverse().map((run) => (
        <span
          key={run.id}
          title={stateLabels[run.state] ?? run.state}
          className={cn(
            "w-1.5 rounded-sm",
            run.state === "succeeded" ? "h-5" : "h-3",
            RUN_STATE_DOT[run.state] ?? "bg-muted-foreground/40",
          )}
        />
      ))}
    </span>
  );
}

function GraphRow({
  graph,
  runs,
  issueId,
}: {
  graph: GraphRecord;
  runs: GraphRun[];
  issueId: string | null;
}) {
  const { t } = useT("graphs");
  const paths = useWorkspacePaths();
  const timeAgo = useTimeAgo();
  const stateLabels = useGraphStateLabels();
  const issue = issueId ? `issue=${encodeURIComponent(issueId)}` : "";
  const href = `${paths.graphDetail(graph.id)}${issue ? `?${issue}` : ""}`;
  const runHref = `${paths.graphDetail(graph.id)}?${issue || "run=1"}`;
  const latest = runs[0];
  const version = graph.versions?.at(-1)?.version;
  const steps = (graph.definition?.nodes ?? []).filter(
    (node) => !["start", "end"].includes(node.type),
  ).length;
  return (
    <li className="group relative grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-4 gap-y-2 border-b px-4 py-3 last:border-b-0 hover:bg-accent/40 md:grid-cols-[minmax(0,2fr)_minmax(0,1fr)_minmax(0,1.4fr)_5rem_auto]">
      <div className="min-w-0">
        <AppLink
          href={href}
          className="block truncate text-body font-medium after:absolute after:inset-0"
        >
          {graph.name || t(($) => $.page.title)}
        </AppLink>
        <p className="mt-0.5 truncate text-caption text-muted-foreground">
          {t(($) => $.studio.step_count, { count: steps })}
          {graph.archivedAt
            ? ` · ${t(($) => $.page.archived)}`
            : version
              ? ` · ${t(($) => $.studio.ux.published_version, { version })}`
              : ""}
        </p>
      </div>
      <div className="hidden md:block">
        <GraphShape graph={graph} />
      </div>
      <div className="hidden min-w-0 items-center gap-2 text-caption md:flex">
        {latest ? (
          <>
            <GraphRunStateDot state={latest.state} />
            <span className="truncate">
              {stateLabels[latest.state] ?? latest.state}
              {latest.createdAt && (
                <span className="text-muted-foreground">
                  {" · "}
                  {timeAgo(latest.createdAt)}
                </span>
              )}
            </span>
            <span className="ms-auto">
              <RecentRuns runs={runs} />
            </span>
          </>
        ) : (
          <span className="text-muted-foreground">
            {t(($) => $.studio.ux.never_run)}
          </span>
        )}
      </div>
      <span className="hidden text-caption text-muted-foreground md:block">
        {graph.updatedAt ? timeAgo(graph.updatedAt) : ""}
      </span>
      <Button
        size="sm"
        variant="outline"
        className="relative z-10"
        disabled={Boolean(graph.archivedAt)}
        render={<AppLink href={runHref} />}
        nativeButton={false}
      >
        <Play className="size-3.5" />
        {t(($) => $.studio.ux.run_once)}
      </Button>
    </li>
  );
}

export function GraphsPage() {
  const { t } = useT("graphs"),
    wsId = useWorkspaceId(),
    paths = useWorkspacePaths(),
    navigation = useNavigation();
  const [tab, setTab] = useState("graphs");
  const [search, setSearch] = useState("");
  const [cursors, setCursors] = useState<string[]>([]);
  const q = useDeferredValue(search);
  const issueId = navigation.searchParams.get("issue");
  const graphs = useQuery(
    graphSearchOptions(wsId, { q, cursor: cursors.at(-1), limit: 20 }),
  );
  // One read of recent runs, grouped here, instead of a request per row.
  const recent = useQuery(graphRunSearchOptions(wsId, { limit: 100 }));
  const byGraph = useMemo(
    () => groupLatestRuns(recent.data?.items ?? [], 7),
    [recent.data?.items],
  );
  const waiting = recent.data?.stats?.waitingApproval ?? 0;

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
      <Tabs
        value={tab}
        onValueChange={(value) => setTab(String(value))}
        className="min-h-0 flex-1 gap-0"
      >
        <div className="border-b px-5 sm:px-8">
          <TabsList variant="line">
            <TabsTrigger value="graphs">
              {t(($) => $.studio.ux.flows_tab)}
              {graphs.data && (
                <span className="tabular-nums text-muted-foreground">
                  {graphs.data.total}
                </span>
              )}
            </TabsTrigger>
            <TabsTrigger value="runs">
              {t(($) => $.studio.ux.all_runs_tab)}
              {waiting > 0 && (
                <span className="rounded-full bg-warning/15 px-1.5 text-micro font-medium text-warning">
                  {t(($) => $.studio.ux.waiting_count, { count: waiting })}
                </span>
              )}
            </TabsTrigger>
          </TabsList>
        </div>
        <TabsContent value="graphs" className="min-h-0 flex-1 overflow-auto">
          <div className="mx-auto w-full max-w-6xl space-y-4 px-5 py-6 sm:px-8">
            <GraphError error={graphs.error} />
            <div className="relative max-w-sm">
              <Search className="pointer-events-none absolute left-3 top-2.5 size-4 text-muted-foreground" />
              <Input
                className="pl-9"
                aria-label={t(($) => $.production.search)}
                placeholder={t(($) => $.production.search)}
                value={search}
                onChange={(event) => {
                  setSearch(event.target.value);
                  setCursors([]);
                }}
              />
            </div>
            {graphs.isPending ? (
              <p className="text-body text-muted-foreground">
                {t(($) => $.page.loading)}
              </p>
            ) : !graphs.data?.items.length ? (
              <div className="rounded-xl border border-dashed p-10 text-center">
                <p className="text-body text-muted-foreground">
                  {t(($) => $.page.empty_templates)}
                </p>
                <Button
                  className="mt-4"
                  size="sm"
                  onClick={() => navigation.push(paths.graphNew())}
                >
                  <Plus className="size-4" />
                  {t(($) => $.page.new)}
                </Button>
              </div>
            ) : (
              <div className="overflow-hidden rounded-xl border bg-card">
                <div className="hidden grid-cols-[minmax(0,2fr)_minmax(0,1fr)_minmax(0,1.4fr)_5rem_auto] gap-x-4 border-b bg-muted/30 px-4 py-2 text-caption text-muted-foreground md:grid">
                  <span>{t(($) => $.studio.ux.col_name)}</span>
                  <span>{t(($) => $.studio.ux.col_shape)}</span>
                  <span>{t(($) => $.studio.ux.col_last_run)}</span>
                  <span>{t(($) => $.studio.ux.col_updated)}</span>
                  <span className="w-[4.5rem]" />
                </div>
                <ul>
                  {graphs.data.items.map((graph) => (
                    <GraphRow
                      key={graph.id}
                      graph={graph}
                      runs={byGraph.get(graph.id) ?? []}
                      issueId={issueId}
                    />
                  ))}
                </ul>
              </div>
            )}
            <GraphPager
              next={graphs.data?.nextCursor}
              cursors={cursors}
              setCursors={setCursors}
            />
          </div>
        </TabsContent>
        <TabsContent value="runs" className="min-h-0 flex-1 overflow-auto">
          <div className="mx-auto w-full max-w-6xl px-5 py-6 sm:px-8">
            <GraphRunList wsId={wsId} />
          </div>
        </TabsContent>
      </Tabs>
    </div>
  );
}
