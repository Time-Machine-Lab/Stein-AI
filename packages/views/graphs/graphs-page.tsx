"use client";

import { useQuery } from "@tanstack/react-query";
import { GitBranch, Plus } from "lucide-react";
import { graphListOptions, graphRunsOptions } from "@multica/core/graphs";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { AppLink, useNavigation } from "../navigation";
import { CollectionPageHeader } from "../layout/collection-page";
import { useT } from "../i18n";
import { GraphError, GraphState } from "./shared";

export function GraphsPage() {
  const { t } = useT("graphs");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const graphs = useQuery(graphListOptions(wsId));
  const runs = useQuery(graphRunsOptions(wsId));
  const issueId = navigation.searchParams.get("issue");
  return <div className="flex min-h-0 flex-1 flex-col">
    <CollectionPageHeader icon={GitBranch} title={t(($) => $.page.title)} description={t(($) => $.page.description)} actions={
      <Button size="sm" onClick={() => navigation.push(paths.graphNew())}><Plus className="size-4" />{t(($) => $.page.new)}</Button>
    } />
    <div className="flex-1 space-y-8 overflow-auto p-4">
      <GraphError error={graphs.error ?? runs.error} />
      <section className="space-y-3">
        <h2 className="text-title font-medium">{t(($) => $.page.templates)}</h2>
        {graphs.isPending ? <p>{t(($) => $.page.loading)}</p> : !graphs.data?.length ? <p className="text-body text-muted-foreground">{t(($) => $.page.empty_templates)}</p> :
          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">{graphs.data.map((graph) => <AppLink key={graph.id} href={`${paths.graphDetail(graph.id)}${issueId ? `?issue=${encodeURIComponent(issueId)}` : ""}`} className="min-w-0 space-y-3 rounded-xl border p-4 hover:bg-accent">
            <div className="truncate font-medium">{graph.name}</div>
            <div className="flex flex-wrap gap-2 text-caption text-muted-foreground"><span>{t(($) => $.page.revision, { revision: graph.revision })}</span>{graph.archivedAt && <span>{t(($) => $.page.archived)}</span>}</div>
          </AppLink>)}</div>}
      </section>
      <section className="space-y-3">
        <h2 className="text-title font-medium">{t(($) => $.page.runs)}</h2>
        {runs.isPending ? <p>{t(($) => $.page.loading)}</p> : !runs.data?.length ? <p className="text-body text-muted-foreground">{t(($) => $.page.empty_runs)}</p> :
          <div className="space-y-2">{runs.data.map((run) => <AppLink key={run.id} href={paths.graphRun(run.id)} className="flex flex-wrap items-center gap-3 rounded-lg border p-3 hover:bg-accent">
            <span className="min-w-0 flex-1 truncate font-mono text-caption">{run.id}</span><GraphState state={run.state} /><GraphState state={run.controlState} /><span className="text-caption text-muted-foreground">{run.createdAt ? new Date(run.createdAt).toLocaleString() : ""}</span>
          </AppLink>)}</div>}
      </section>
    </div>
  </div>;
}
