"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  AlertCircle,
  AlertTriangle,
  ArrowLeft,
  Check,
  Loader2,
  Play,
} from "lucide-react";
import {
  graphDetailOptions,
  type GraphRecord,
} from "@multica/core/graphs";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Dialog,
  DialogContent,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
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
import { GraphNewEntry } from "./graph-new-entry";
import { GraphRunList } from "./graph-run-list";
import { GraphStartForm } from "./graph-start-form";
import { GraphTriggers } from "./graph-triggers";
import { GraphError } from "./shared";
import {
  useGraphPublishFlow,
  type GraphPublishFlow,
} from "./use-graph-publish-flow";

export function GraphDetailPage({ create = false }: { create?: boolean }) {
  const { t } = useT("graphs");
  const wsId = useWorkspaceId();
  const navigation = useNavigation();
  const id = create
    ? ""
    : (navigation.pathname.split("/").filter(Boolean).at(-1) ?? "");
  const graph = useQuery(graphDetailOptions(wsId, id));

  if (create) return <GraphWorkspace key={`${wsId}:new`} wsId={wsId} />;
  if (graph.isPending)
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        <GraphPageHeader title={t(($) => $.page.title)} />
        <div
          aria-label={t(($) => $.page.loading)}
          className="grid flex-1 gap-0 lg:grid-cols-[420px_1fr]"
        >
          <Skeleton className="m-4 h-64" />
          <Skeleton className="m-4" />
        </div>
      </div>
    );
  if (!graph.data)
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        <GraphPageHeader title={t(($) => $.page.title)} />
        <GraphError error={graph.error} />
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
      </div>
    );
  return (
    <GraphWorkspace
      key={`${wsId}:${graph.data.id}`}
      wsId={wsId}
      graph={graph.data}
    />
  );
}

function GraphPageHeader({
  title,
  children,
}: {
  title?: string;
  children?: React.ReactNode;
}) {
  const { t } = useT("graphs");
  const paths = useWorkspacePaths();
  return (
    <PageHeader className="h-14 gap-3 px-4">
      <AppLink
        href={paths.graphs()}
        aria-label={t(($) => $.page.back)}
        className="shrink-0 rounded-md p-1.5 text-muted-foreground hover:bg-accent hover:text-foreground"
      >
        <ArrowLeft className="size-4" />
      </AppLink>
      {title !== undefined && (
        <h1 className="min-w-0 flex-1 truncate text-body font-medium">
          {title}
        </h1>
      )}
      {children}
    </PageHeader>
  );
}

/** Draft state, problem count and the save/publish action for the header. */
function DraftActions({
  flow,
  graph,
  onShowIssues,
}: {
  flow: GraphPublishFlow;
  graph?: GraphRecord;
  onShowIssues: () => void;
}) {
  const { t } = useT("graphs");
  const { draft } = flow;
  const problems = flow.diagnostics.length;
  return (
    <div className="flex shrink-0 items-center gap-2">
      {problems > 0 && (
        <Button
          type="button"
          size="sm"
          variant="ghost"
          className="text-destructive"
          onClick={onShowIssues}
        >
          <AlertTriangle className="size-3.5" />
          {t(($) => $.studio.ux.problems, { count: problems })}
        </Button>
      )}
      <span className="hidden items-center gap-1 text-caption text-muted-foreground md:flex">
        {!draft?.dirty && <Check className="size-3.5" />}
        {draft?.dirty
          ? draft.storageError || draft.storageConflict
            ? t(($) => $.studio.unsaved)
            : t(($) => $.production.local_saved)
          : t(($) => $.studio.saved)}
      </span>
      {graph ? (
        <Button
          type="button"
          size="sm"
          disabled={
            flow.pending ||
            flow.blocked ||
            !flow.supported ||
            !flow.admin ||
            Boolean(graph.archivedAt)
          }
          title={!flow.admin ? t(($) => $.studio.ux.publish_admin) : undefined}
          onClick={() => void flow.publishAll()}
        >
          {flow.publishing && <Loader2 className="size-3.5 animate-spin" />}
          {t(($) => $.editor.publish)}
        </Button>
      ) : (
        <Button
          type="button"
          size="sm"
          disabled={flow.disabled || flow.blocked}
          onClick={() => void flow.saveDraft()}
        >
          {flow.saving && <Loader2 className="size-3.5 animate-spin" />}
          {t(($) => $.editor.save)}
        </Button>
      )}
    </div>
  );
}

function GraphWorkspace({
  wsId,
  graph,
}: {
  wsId: string;
  graph?: GraphRecord;
}) {
  const { t } = useT("graphs");
  const navigation = useNavigation();
  const flow = useGraphPublishFlow(wsId, graph);
  const [tab, setTab] = useState("editor");
  const params = navigation.searchParams;
  const [runOpen, setRunOpen] = useState(
    Boolean(
      graph &&
        (params.get("issue") || params.get("previous_run") || params.get("run")),
    ),
  );
  // The new-graph entry: shown until the user describes, copies or starts
  // blank. A recovered draft skips it — there is already something to edit.
  const [entered, setEntered] = useState(false);
  const [firstMessage, setFirstMessage] = useState<string>();
  const showEntry =
    !graph && !entered && !flow.draft?.dirty && !flow.draft?.ai;

  const nameInput = (
    <Input
      aria-label={t(($) => $.editor.name)}
      className="h-8 min-w-0 max-w-sm flex-1 border-transparent bg-transparent px-2 text-body font-medium shadow-none hover:border-border focus-visible:border-border"
      value={flow.definition.name}
      disabled={flow.disabled}
      maxLength={200}
      placeholder={t(($) => $.editor.name_placeholder)}
      onChange={(event) =>
        flow.change({ ...flow.definition, name: event.target.value })
      }
    />
  );

  if (!graph)
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        <GraphPageHeader
          title={showEntry ? t(($) => $.page.create) : undefined}
        >
          {!showEntry && (
            <>
              {nameInput}
              <span className="flex-1" />
              <DraftActions flow={flow} onShowIssues={() => undefined} />
            </>
          )}
        </GraphPageHeader>
        {showEntry ? (
          <GraphNewEntry
            wsId={wsId}
            onDescribe={(message) => {
              setFirstMessage(message);
              setEntered(true);
            }}
            onCopy={(definition) => {
              flow.change(definition);
              setEntered(true);
            }}
            onBlank={() => setEntered(true)}
          />
        ) : (
          <GraphEditor wsId={wsId} flow={flow} initialMessage={firstMessage} />
        )}
      </div>
    );

  return (
    <Tabs
      value={tab}
      onValueChange={(value) => setTab(String(value))}
      className="min-h-0 flex-1 gap-0"
    >
      <GraphPageHeader>
        {nameInput}
        <TabsList className="mx-auto hidden md:flex">
          <TabsTrigger value="editor" className="px-3">
            {t(($) => $.studio.edit_tab)}
          </TabsTrigger>
          <TabsTrigger value="runs" className="px-3">
            {t(($) => $.studio.ux.runs_tab)}
          </TabsTrigger>
          <TabsTrigger value="triggers" className="px-3">
            {t(($) => $.triggers.title)}
          </TabsTrigger>
        </TabsList>
        <Button
          type="button"
          size="sm"
          variant="outline"
          disabled={Boolean(graph.archivedAt)}
          onClick={() => setRunOpen(true)}
        >
          <Play className="size-3.5" />
          {t(($) => $.studio.ux.run_once)}
        </Button>
        <DraftActions
          flow={flow}
          graph={graph}
          onShowIssues={() => setTab("editor")}
        />
      </GraphPageHeader>
      <TabsList className="mx-4 mt-2 md:hidden">
        <TabsTrigger value="editor">{t(($) => $.studio.edit_tab)}</TabsTrigger>
        <TabsTrigger value="runs">{t(($) => $.studio.ux.runs_tab)}</TabsTrigger>
        <TabsTrigger value="triggers">{t(($) => $.triggers.title)}</TabsTrigger>
      </TabsList>
      <TabsContent
        value="editor"
        keepMounted
        className={tab === "editor" ? "flex min-h-0 flex-1 flex-col" : "hidden"}
      >
        <GraphEditor wsId={wsId} graph={graph} flow={flow} />
      </TabsContent>
      <TabsContent
        value="runs"
        className={tab === "runs" ? "min-h-0 flex-1 overflow-y-auto" : "hidden"}
      >
        <div className="mx-auto w-full max-w-4xl px-5 py-6">
          <GraphRunList
            wsId={wsId}
            graphId={graph.id}
            onRun={graph.archivedAt ? undefined : () => setRunOpen(true)}
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
          key={`${wsId}:${graph.id}:triggers`}
          wsId={wsId}
          graph={graph}
        />
      </TabsContent>
      <Dialog open={runOpen} onOpenChange={setRunOpen}>
        <DialogContent className="max-h-[90vh] overflow-y-auto p-0 sm:max-w-2xl">
          <DialogTitle className="sr-only">
            {t(($) => $.start.title)}
          </DialogTitle>
          <GraphStartForm
            key={`${wsId}:${graph.id}:start`}
            wsId={wsId}
            graph={graph}
          />
        </DialogContent>
      </Dialog>
    </Tabs>
  );
}
