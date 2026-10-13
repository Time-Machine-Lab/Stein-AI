"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertCircle, ArrowLeft, GitBranch, Play } from "lucide-react";
import { graphDetailOptions } from "@multica/core/graphs";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@multica/ui/components/ui/tabs";
import { AppLink, useNavigation } from "../navigation";
import { PageHeader } from "../layout/page-header";
import { CollectionPageState } from "../layout/collection-page";
import { useT } from "../i18n";
import { GraphEditor } from "./graph-editor";
import { GraphStartForm } from "./graph-start-form";
import { GraphTriggers } from "./graph-triggers";
import { GraphError } from "./shared";

export function GraphDetailPage({ create = false }: { create?: boolean }) {
  const { t } = useT("graphs");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const id = create
    ? ""
    : (navigation.pathname.split("/").filter(Boolean).at(-1) ?? "");
  const graph = useQuery(graphDetailOptions(wsId, id));
  const [tab, setTab] = useState(
    navigation.searchParams.get("issue") ? "run" : "editor",
  );
  return (
    <Tabs
      value={tab}
      onValueChange={(value) => setTab(String(value))}
      className="min-h-0 flex-1 gap-0"
    >
      <PageHeader className="h-14 gap-3 px-5">
        <AppLink
          href={paths.graphs()}
          aria-label={t(($) => $.page.back)}
          className="shrink-0 rounded-md p-1.5 text-muted-foreground hover:bg-accent hover:text-foreground"
        >
          <ArrowLeft className="size-4" />
        </AppLink>
        <GitBranch className="hidden size-4 shrink-0 text-muted-foreground sm:block" />
        <h1 className="min-w-0 flex-1 truncate text-body font-medium">
          {create
            ? t(($) => $.page.create)
            : (graph.data?.name ?? t(($) => $.page.title))}
        </h1>
        {!create && graph.data && (
          <TabsList>
            <TabsTrigger value="editor" className="px-3">
              {t(($) => $.studio.edit_tab)}
            </TabsTrigger>
            <TabsTrigger value="run" className="px-3">
              <Play className="size-3.5" />
              {t(($) => $.studio.run_tab)}
            </TabsTrigger>
            <TabsTrigger value="triggers" className="px-3">
              {t(($) => $.triggers.title)}
            </TabsTrigger>
          </TabsList>
        )}
      </PageHeader>
      <div className="flex min-h-0 flex-1 flex-col">
        <GraphError error={graph.error} />
        {create ? (
          <GraphEditor key={`${wsId}:new`} wsId={wsId} />
        ) : graph.isPending ? (
          <div
            aria-label={t(($) => $.page.loading)}
            className="mx-auto w-full max-w-6xl space-y-6 p-8"
          >
            <Skeleton className="h-10 w-1/2" />
            <div className="grid gap-6 sm:grid-cols-2">
              <Skeleton className="h-80" />
              <Skeleton className="h-80" />
            </div>
          </div>
        ) : graph.data ? (
          <>
            <TabsContent
              value="editor"
              keepMounted
              className={
                tab === "editor" ? "flex min-h-0 flex-1 flex-col" : "hidden"
              }
            >
              <GraphEditor
                key={`${wsId}:${graph.data.id}`}
                wsId={wsId}
                graph={graph.data}
              />
            </TabsContent>
            <TabsContent
              value="run"
              keepMounted
              className={
                tab === "run" ? "min-h-0 flex-1 overflow-y-auto" : "hidden"
              }
            >
              <div className="mx-auto w-full max-w-2xl px-5 py-8">
                <GraphStartForm
                  key={`${wsId}:${graph.data.id}:start`}
                  wsId={wsId}
                  graph={graph.data}
                />
              </div>
            </TabsContent>
            <TabsContent
              value="triggers"
              keepMounted
              className={
                tab === "triggers" ? "min-h-0 flex-1 overflow-y-auto" : "hidden"
              }
            >
              <GraphTriggers
                key={`${wsId}:${graph.data.id}:triggers`}
                wsId={wsId}
                graph={graph.data}
              />
            </TabsContent>
          </>
        ) : (
          <CollectionPageState
            icon={AlertCircle}
            title={t(($) => $.page.unavailable)}
            tone="warning"
            actions={
              <Button variant="outline" onClick={() => void graph.refetch()}>
                {t(($) => $.page.refresh)}
              </Button>
            }
          />
        )}
      </div>
    </Tabs>
  );
}
