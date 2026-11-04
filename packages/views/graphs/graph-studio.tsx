"use client";

import { useMemo, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { useDefaultLayout } from "react-resizable-panels";
import { Panel } from "@xyflow/react";
import {
  ChevronRight,
  LayoutGrid,
  Plus,
  Redo2,
  Settings2,
  Sparkles,
  Undo2,
  X,
} from "lucide-react";
import {
  graphChangeMarks,
  graphGenerationMode,
  graphGenerationOptions,
  insertGraphNodeOnEdge,
  layoutGraph,
  nextGraphNodeKey,
  useGraphDraftStore,
  type GraphDefinition,
  type GraphEdge,
} from "@multica/core/graphs";
import { runtimeListOptions } from "@multica/core/runtimes";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
} from "@multica/ui/components/ui/resizable";
import { useIsCompact } from "@multica/ui/hooks/use-mobile";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import { GraphCanvas } from "./graph-canvas";
import {
  GraphAddNodeMenu,
  GraphNodeIcon,
  useGraphNodeLabels,
} from "./graph-node-catalog";
import { GraphNodeInspector } from "./graph-node-inspector";
import {
  GraphFlowSettings,
  type GraphFlowSettingsTab,
} from "./graph-flow-settings";
import {
  GraphStudioChat,
  type GraphAiTurn,
  type GraphComposerPrefill,
} from "./graph-studio-chat";
import {
  getScope,
  newGraphNode,
  replaceScope,
  scopeInLoop,
} from "./graph-definition-helpers";
import type { GraphPublishFlow } from "./use-graph-publish-flow";
import { GraphError } from "./shared";

type Drawer =
  | { kind: "closed" }
  | { kind: "node"; key: string }
  | { kind: "flow"; tab: GraphFlowSettingsTab };

/**
 * The AI-first editor: the conversation on the left, the draft it writes on
 * the right. The canvas is editable at all times — dragging, connecting, adding
 * steps — and a step opens in a drawer over the canvas instead of a separate
 * mode, so talking and hand-editing are one surface.
 */
export function GraphStudio({
  wsId,
  flow,
  archived,
  initialMessage,
}: {
  wsId: string;
  flow: GraphPublishFlow;
  archived?: boolean;
  /** Sent as the first message once (from the new-graph entry page). */
  initialMessage?: string;
}) {
  const { t } = useT("graphs");
  const labels = useGraphNodeLabels();
  const compact = useIsCompact();
  const { defaultLayout, onLayoutChanged } = useDefaultLayout({
    id: "multica_graph_studio_layout",
  });
  const agents = useQuery(agentListOptions(wsId)),
    members = useQuery(memberListOptions(wsId)),
    runtimes = useQuery(runtimeListOptions(wsId)),
    generation = useQuery(graphGenerationOptions(wsId));
  const { definition, draft, draftKey, disabled, actor } = flow;

  const [scopePath, setScopePath] = useState<string[]>([]);
  const [drawer, setDrawer] = useState<Drawer>({ kind: "closed" });
  const [turn, setTurn] = useState<GraphAiTurn | null>(null);
  const [prefill, setPrefill] = useState<GraphComposerPrefill | null>(null);
  const [pane, setPane] = useState<"chat" | "flow">("chat");

  const scope = getScope(definition, scopePath);
  const inLoop = scopeInLoop(definition, scopePath);
  const selectedKey = drawer.kind === "node" ? drawer.key : undefined;
  const selected = scope.nodes.find((node) => node.nodeKey === selectedKey);
  const atRoot = scopePath.length === 0;
  const actors = useMemo(
    () => ({
      agents: (agents.data ?? []).filter((agent) => !agent.archived_at),
      runtimes: runtimes.data ?? [],
      members: members.data ?? [],
      currentUserId: actor.userId || null,
      loading: agents.isPending || runtimes.isPending || members.isPending,
    }),
    [
      agents.data,
      runtimes.data,
      members.data,
      actor.userId,
      agents.isPending,
      runtimes.isPending,
      members.isPending,
    ],
  );

  /** A hand edit: the AI turn's marks no longer describe the draft. */
  const edit = (next: GraphDefinition) => {
    flow.change(next);
    setTurn(null);
  };
  const editScope = (next: GraphDefinition) =>
    edit(replaceScope(definition, scopePath, { ...next, schemaVersion: 2 }));

  const select = (key: string) => {
    const node = scope.nodes.find((item) => item.nodeKey === key);
    if (!node) return;
    if (node.type === "start") setDrawer({ kind: "flow", tab: "input" });
    else if (node.type === "end") setDrawer({ kind: "flow", tab: "output" });
    else setDrawer({ kind: "node", key });
    if (compact) setPane("flow");
  };

  const makeNode = (type: string) => {
    const { key, index } = nextGraphNodeKey(scope, type);
    return {
      key,
      node: {
        ...newGraphNode(type, key),
        label: `${labels[type] ?? type} ${index}`,
      },
    };
  };

  /** Appends a step after the selection, splicing it into a single path. */
  function addNode(type: string) {
    const { key, node } = makeNode(type);
    const after =
      selected ?? scope.nodes.find((item) => item.type === "start");
    const outgoing = scope.edges.filter((edge) => edge.from === after?.nodeKey);
    if (
      after &&
      outgoing.length === 1 &&
      !["branch", "parallel"].includes(after.type) &&
      !["branch", "parallel", "join"].includes(type)
    ) {
      editScope(insertGraphNodeOnEdge(scope, outgoing[0]!, node));
    } else {
      const anchor = scope.layout?.nodes[after?.nodeKey ?? ""];
      editScope({
        ...scope,
        nodes: [...scope.nodes, node],
        layout: {
          ...scope.layout,
          nodes: {
            ...scope.layout?.nodes,
            [key]: { x: (anchor?.x ?? 0) + 300, y: (anchor?.y ?? 0) + 160 },
          },
        },
      });
    }
    setDrawer({ kind: "node", key });
  }

  function insertOnEdge(edge: GraphEdge, type: string) {
    const { key, node } = makeNode(type);
    try {
      editScope(insertGraphNodeOnEdge(scope, edge, node));
      setDrawer({ kind: "node", key });
    } catch (error) {
      flow.setError(error instanceof Error ? error : new Error(String(error)));
    }
  }

  const marks =
    atRoot && turn ? graphChangeMarks(turn.added, turn.changed) : undefined;
  const generationMode = graphGenerationMode(generation.data);

  const toolbar = (
    <Panel position="top-left" className="!m-3">
      <div className="flex flex-wrap items-center gap-1 rounded-lg border bg-background/95 p-1 shadow-sm backdrop-blur">
        {scopePath.length > 0 && (
          <nav className="flex items-center gap-0.5 pe-1 text-caption">
            <Button
              type="button"
              size="xs"
              variant="ghost"
              onClick={() => {
                setScopePath([]);
                setDrawer({ kind: "closed" });
              }}
            >
              {t(($) => $.production.root_flow)}
            </Button>
            {scopePath.map((key, index) => (
              <span key={key} className="flex items-center">
                <ChevronRight className="size-3 text-muted-foreground" />
                <Button
                  type="button"
                  size="xs"
                  variant="ghost"
                  onClick={() => {
                    setScopePath(scopePath.slice(0, index + 1));
                    setDrawer({ kind: "closed" });
                  }}
                >
                  {key}
                </Button>
              </span>
            ))}
          </nav>
        )}
        <GraphAddNodeMenu
          disabled={disabled || scope.nodes.length >= 128}
          onPick={addNode}
          trigger={
            <Button type="button" size="sm" variant="ghost">
              <Plus className="size-3.5" />
              {t(($) => $.studio.ux.add_step)}
            </Button>
          }
        />
        <Button
          type="button"
          size="icon-sm"
          variant="ghost"
          aria-label={t(($) => $.production.undo)}
          disabled={disabled || !draft?.past.length}
          onClick={() => {
            useGraphDraftStore.getState().undo(draftKey);
            setTurn(null);
          }}
        >
          <Undo2 className="size-4" />
        </Button>
        <Button
          type="button"
          size="icon-sm"
          variant="ghost"
          aria-label={t(($) => $.production.redo)}
          disabled={disabled || !draft?.future.length}
          onClick={() => useGraphDraftStore.getState().redo(draftKey)}
        >
          <Redo2 className="size-4" />
        </Button>
        <Button
          type="button"
          size="icon-sm"
          variant="ghost"
          aria-label={t(($) => $.production.layout)}
          disabled={disabled}
          onClick={() => editScope(layoutGraph(scope))}
        >
          <LayoutGrid className="size-4" />
        </Button>
        <Button
          type="button"
          size="sm"
          variant={drawer.kind === "flow" ? "secondary" : "ghost"}
          onClick={() =>
            setDrawer(
              drawer.kind === "flow"
                ? { kind: "closed" }
                : { kind: "flow", tab: "input" },
            )
          }
        >
          <Settings2 className="size-3.5" />
          {t(($) => $.studio.ux.flow_settings)}
        </Button>
      </div>
    </Panel>
  );

  const banner = turn && atRoot && (
    <Panel position="top-center" className="!mt-16 max-w-[min(560px,90%)]">
      <div
        role="status"
        className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-lg border border-primary/30 bg-background/95 px-3 py-2 text-caption shadow-sm backdrop-blur"
      >
        <Sparkles className="size-3.5 shrink-0 text-primary" />
        <span className="min-w-0 flex-1 font-medium">
          {t(($) => $.studio.ux.ai_changed, {
            added: turn.added.length,
            changed: turn.changed.length,
            removed: turn.removed.length,
          })}
          {turn.summary && (
            <span className="ms-1 font-normal text-muted-foreground">
              {turn.summary}
            </span>
          )}
        </span>
        <Button
          type="button"
          size="xs"
          variant="ghost"
          disabled={disabled || !draft?.past.length}
          onClick={() => {
            useGraphDraftStore.getState().undo(draftKey);
            setTurn(null);
          }}
        >
          <Undo2 className="size-3" />
          {t(($) => $.studio.ux.undo_turn)}
        </Button>
        <Button
          type="button"
          size="icon-xs"
          variant="ghost"
          aria-label={t(($) => $.studio.ux.close)}
          onClick={() => setTurn(null)}
        >
          <X className="size-3" />
        </Button>
      </div>
    </Panel>
  );

  const drawerPanel = drawer.kind !== "closed" && (
    <aside
      aria-label={
        drawer.kind === "node"
          ? t(($) => $.studio.step_settings)
          : t(($) => $.studio.ux.flow_settings)
      }
      className="absolute inset-y-2 right-2 z-10 flex w-[min(400px,calc(100%-1rem))] flex-col overflow-hidden rounded-xl border bg-background shadow-lg"
    >
      <header className="flex min-h-12 shrink-0 items-center gap-2.5 border-b px-4">
        {drawer.kind === "node" && selected ? (
          <>
            <GraphNodeIcon type={selected.type} />
            <div className="min-w-0 flex-1">
              <p className="truncate text-body font-semibold">
                {selected.label || selected.nodeKey}
              </p>
              <p className="truncate text-caption text-muted-foreground">
                {labels[selected.type] ?? selected.type}
              </p>
            </div>
          </>
        ) : (
          <p className="min-w-0 flex-1 truncate text-body font-semibold">
            {t(($) => $.studio.ux.flow_settings)}
          </p>
        )}
        <Button
          type="button"
          size="icon-sm"
          variant="ghost"
          aria-label={t(($) => $.studio.ux.close)}
          onClick={() => setDrawer({ kind: "closed" })}
        >
          <X className="size-4" />
        </Button>
      </header>
      <div className="min-h-0 flex-1 overflow-y-auto p-4">
        {drawer.kind === "node" && selected ? (
          <GraphNodeInspector
            key={`${scopePath.join("/")}:${selected.nodeKey}`}
            definition={scope}
            node={selected}
            onChange={editScope}
            onSelect={(key) =>
              setDrawer(
                key === "start"
                  ? { kind: "closed" }
                  : { kind: "node", key },
              )
            }
            onEnter={(key) => {
              setScopePath([...scopePath, key]);
              setDrawer({ kind: "closed" });
            }}
            onError={flow.setError}
            agents={agents.data ?? []}
            members={members.data ?? []}
            disabled={disabled}
            inLoop={inLoop}
          />
        ) : drawer.kind === "flow" ? (
          <GraphFlowSettings
            scope={scope}
            onScopeChange={editScope}
            root={definition}
            onRootChange={edit}
            isRoot={atRoot}
            inLoop={inLoop}
            disabled={disabled}
            tab={drawer.tab}
            onTabChange={(tab) => setDrawer({ kind: "flow", tab })}
            onImported={() => {
              setScopePath([]);
              setDrawer({ kind: "closed" });
            }}
            onError={flow.setError}
            onArchive={archived === undefined ? undefined : flow.archiveGraph}
            canArchive={
              !flow.pending && !draft?.dirty && flow.canEdit && !archived
            }
          />
        ) : null}
      </div>
      {drawer.kind === "node" && selected && generationMode === "builder" && (
        <footer className="shrink-0 border-t p-3">
          <Button
            type="button"
            variant="outline"
            className="w-full justify-start border-primary/30 text-primary"
            disabled={disabled}
            onClick={() => {
              setPrefill({
                id: `step:${selected.nodeKey}:${Date.now()}`,
                content: t(($) => $.studio.ux.ask_ai_prefill, {
                  name: selected.label || selected.nodeKey,
                }),
              });
              if (compact) setPane("chat");
            }}
          >
            <Sparkles className="size-4" />
            {t(($) => $.studio.ux.ask_ai_step)}
          </Button>
        </footer>
      )}
    </aside>
  );

  const canvasPane = (
    <div className="relative h-full min-h-0 bg-muted/20">
      <GraphCanvas
        key={scopePath.join("/")}
        className="h-full rounded-none border-0 lg:h-full"
        definition={scope}
        onChange={editScope}
        onInsertOnEdge={insertOnEdge}
        selectedKey={selectedKey}
        onSelect={select}
        disabled={disabled}
        onError={flow.setError}
        marks={marks}
        issues={atRoot ? flow.issues : undefined}
      >
        {toolbar}
        {banner}
      </GraphCanvas>
      {drawerPanel}
    </div>
  );

  const chatPane = (
    <GraphStudioChat
      draftKey={draftKey}
      ai={draft?.ai ?? null}
      definition={definition}
      actors={actors}
      memberId={actor.memberId ?? null}
      disabled={disabled}
      available={generationMode === "builder"}
      prefill={prefill}
      onPrefillApplied={() => setPrefill(null)}
      initialMessage={initialMessage}
      onApplyTurn={(next, applied) => {
        flow.change(next);
        setTurn(
          applied.added.length || applied.changed.length || applied.removed.length
            ? applied
            : null,
        );
        if (compact) setPane("flow");
      }}
    />
  );

  return (
    <div
      className="flex min-h-0 flex-1 flex-col"
      onKeyDown={(event) => {
        if (
          disabled ||
          !(event.metaKey || event.ctrlKey) ||
          ["INPUT", "TEXTAREA", "SELECT"].includes(
            (event.target as HTMLElement).tagName,
          ) ||
          (event.target as HTMLElement).isContentEditable
        )
          return;
        const key = event.key.toLowerCase();
        if (key === "z" || key === "y") {
          event.preventDefault();
          const store = useGraphDraftStore.getState();
          if (key === "y" || event.shiftKey) store.redo(draftKey);
          else store.undo(draftKey);
          setTurn(null);
        }
      }}
    >
      {compact ? (
        <>
          <div className="flex shrink-0 gap-1 border-b px-3 py-2">
            {(["chat", "flow"] as const).map((value) => (
              <Button
                key={value}
                type="button"
                size="sm"
                variant={pane === value ? "secondary" : "ghost"}
                onClick={() => setPane(value)}
              >
                {value === "chat"
                  ? t(($) => $.studio.ux.tab_chat)
                  : t(($) => $.studio.ux.tab_flow)}
              </Button>
            ))}
          </div>
          <div className="min-h-0 flex-1">
            {pane === "chat" ? chatPane : canvasPane}
          </div>
        </>
      ) : (
        <ResizablePanelGroup
          orientation="horizontal"
          className="min-h-0 flex-1"
          defaultLayout={defaultLayout}
          onLayoutChanged={onLayoutChanged}
        >
          <ResizablePanel
            id="chat"
            defaultSize={420}
            minSize={320}
            groupResizeBehavior="preserve-pixel-size"
          >
            <div className="h-full border-r bg-background">{chatPane}</div>
          </ResizablePanel>
          <ResizableHandle />
          <ResizablePanel id="canvas" minSize="40%">
            {canvasPane}
          </ResizablePanel>
        </ResizablePanelGroup>
      )}
    </div>
  );
}

/** Banners about the draft document itself, above the studio. */
export function GraphDraftNotices({
  flow,
  children,
}: {
  flow: GraphPublishFlow;
  children?: ReactNode;
}) {
  const { t } = useT("graphs");
  const { draft } = flow;
  const notices = [
    Boolean(flow.error) && <GraphError key="error" error={flow.error} />,
    !flow.supported && (
      <p key="unsupported" role="alert" className="text-body text-muted-foreground">
        {t(($) => $.production.unsupported)}
      </p>
    ),
    draft?.recovered && !draft.conflict && !draft.storageConflict && (
      <div key="recovered" className="flex flex-wrap items-center gap-2">
        <p role="status" className="text-caption text-muted-foreground">
          {t(($) => $.production.draft_recovered)}
        </p>
        {draft.dirty && (
          <Button size="xs" variant="ghost" onClick={flow.discard}>
            {t(($) => $.production.discard_local)}
          </Button>
        )}
      </div>
    ),
    draft?.storageError && (
      <p key="storage" role="alert" className="text-caption text-destructive">
        {t(($) => $.production.storage_error)}
      </p>
    ),
    (draft?.conflict || draft?.storageConflict) && (
      <div
        key="conflict"
        role="alert"
        className="flex flex-wrap items-center gap-3 rounded-lg border border-warning/40 bg-warning/5 px-3 py-2"
      >
        <p className="min-w-0 flex-1 text-body">
          {draft.storageConflict
            ? t(($) => $.production.storage_conflict)
            : t(($) => $.production.draft_conflict)}
        </p>
        <Button
          size="sm"
          onClick={() =>
            useGraphDraftStore
              .getState()
              .keepLocal(flow.draftKey, flow.serverRevision)
          }
        >
          {t(($) => $.production.keep_local)}
        </Button>
        <Button size="sm" variant="outline" onClick={flow.discard}>
          {t(($) => $.production.discard_local)}
        </Button>
      </div>
    ),
    children,
  ].filter(Boolean);
  if (!notices.length) return null;
  return <div className={cn("shrink-0 space-y-2 px-5 py-3")}>{notices}</div>;
}
