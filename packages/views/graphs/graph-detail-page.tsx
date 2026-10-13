"use client";

import { useQuery } from "@tanstack/react-query";
import { ArrowLeft } from "lucide-react";
import { graphDetailOptions } from "@multica/core/graphs";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { AppLink, useNavigation } from "../navigation";
import { PageHeader } from "../layout/page-header";
import { useT } from "../i18n";
import { GraphEditor } from "./graph-editor";
import { GraphStartForm } from "./graph-start-form";
import { GraphError } from "./shared";

export function GraphDetailPage({ create = false }: { create?: boolean }) {
  const { t } = useT("graphs");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const id = create ? "" : navigation.pathname.split("/").filter(Boolean).at(-1) ?? "";
  const graph = useQuery(graphDetailOptions(wsId, id));
  return <div className="flex min-h-0 flex-1 flex-col">
    <PageHeader><AppLink href={paths.graphs()} aria-label={t(($) => $.page.back)} className="rounded-md p-1 hover:bg-accent"><ArrowLeft className="size-4" /></AppLink><h1 className="min-w-0 flex-1 truncate text-body font-medium">{create ? t(($) => $.page.create) : graph.data?.name ?? t(($) => $.page.title)}</h1></PageHeader>
    <div className="flex-1 overflow-auto p-4">
      <GraphError error={graph.error} />
      {create ? <div className="mx-auto max-w-3xl"><GraphEditor key={`${wsId}:new`} wsId={wsId} /></div> : graph.isPending ? <p>{t(($) => $.page.loading)}</p> : graph.data ?
        <div className="mx-auto grid max-w-6xl items-start gap-8 xl:grid-cols-[minmax(0,2fr)_minmax(260px,1fr)]"><GraphEditor key={`${wsId}:${graph.data.id}`} wsId={wsId} graph={graph.data} /><GraphStartForm key={`${wsId}:${graph.data.id}:start`} wsId={wsId} graph={graph.data} /></div> :
        <div className="space-y-3"><p>{t(($) => $.page.unavailable)}</p><Button variant="outline" onClick={() => void graph.refetch()}>{t(($) => $.page.refresh)}</Button></div>}
    </div>
  </div>;
}
