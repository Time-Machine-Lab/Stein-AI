"use client";

import {
  createContext,
  useContext,
  useMemo,
  useState,
  type CSSProperties,
  type ReactNode,
} from "react";
import {
  Background,
  BaseEdge,
  Controls,
  EdgeLabelRenderer,
  Handle,
  MarkerType,
  MiniMap,
  Position,
  ReactFlow,
  getSmoothStepPath,
  type Edge,
  type EdgeProps,
  type Node,
  type NodeProps,
} from "@xyflow/react";
import {
  AlertCircle,
  Check,
  Loader2,
  Pause,
  Plus,
  X,
  type LucideIcon,
} from "lucide-react";
import {
  canInsertGraphNode,
  connectGraphNodes,
  layoutGraph,
  type GraphDefinition,
  type GraphEdge,
  type GraphNodeDefinition,
} from "@multica/core/graphs";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import { useGraphStateLabels } from "./shared";
import {
  GRAPH_CATEGORY_STYLES,
  GraphAddNodeMenu,
  GraphNodeIcon,
  graphNodeCategory,
  useGraphNodeLabels,
} from "./graph-node-catalog";
import "@xyflow/react/dist/style.css";


export type GraphNodeMark = "added" | "changed";

type FlowNode = Node<
  {
    node: GraphNodeDefinition;
    label: string;
    detail: string;
    state?: string;
    mark?: GraphNodeMark;
    issues?: number;
  },
  "graph"
>;
type FlowEdge = Edge<{ edge: GraphEdge }, "graph">;

/** Run states grouped by how they should read on a node. */
const STATE_TONE: Record<
  string,
  { tone: "done" | "live" | "wait" | "bad" | "idle"; icon?: LucideIcon }
> = {
  succeeded: { tone: "done", icon: Check },
  completed: { tone: "done", icon: Check },
  approved: { tone: "done", icon: Check },
  running: { tone: "live", icon: Loader2 },
  started: { tone: "live", icon: Loader2 },
  dispatched: { tone: "live", icon: Loader2 },
  queued: { tone: "live", icon: Loader2 },
  waiting_approval: { tone: "wait", icon: Pause },
  blocked: { tone: "wait", icon: AlertCircle },
  retry_wait: { tone: "wait", icon: Pause },
  failed: { tone: "bad", icon: X },
  rejected: { tone: "bad", icon: X },
  cancelled: { tone: "bad", icon: X },
  skipped: { tone: "idle" },
  pending: { tone: "idle" },
  ready: { tone: "idle" },
};

const TONE_CLASS = {
  done: "border-success/60 bg-success/5",
  live: "border-info ring-2 ring-info/20",
  wait: "border-warning ring-2 ring-warning/25",
  bad: "border-destructive/70 bg-destructive/5",
  idle: "",
};
const TONE_TEXT = {
  done: "text-success",
  live: "text-info",
  wait: "text-warning",
  bad: "text-destructive",
  idle: "text-muted-foreground",
};

function GraphFlowNode({ data, selected, isConnectable }: NodeProps<FlowNode>) {
  const { t } = useT("graphs");
  const stateLabels = useGraphStateLabels();
  const type = data.node.type;
  const capsule = type === "start" || type === "end";
  const category = graphNodeCategory(type);
  const tone = data.state ? (STATE_TONE[data.state]?.tone ?? "idle") : null;
  const StateIcon = data.state ? STATE_TONE[data.state]?.icon : undefined;
  return (
    <div
      className={cn(
        "relative border bg-card shadow-sm transition-shadow",
        capsule
          ? "flex min-w-36 items-center gap-2 rounded-full px-4 py-2"
          : "w-60 overflow-hidden rounded-xl py-2.5 pl-4 pr-3",
        selected && "border-primary ring-2 ring-primary/20",
        !selected && tone && TONE_CLASS[tone],
        data.state === "skipped" && "opacity-50",
        !selected &&
          data.mark === "added" &&
          "border-2 border-dashed border-success",
        !selected && data.mark === "changed" && "border-2 border-warning",
      )}
    >
      {!capsule && (
        <span
          aria-hidden="true"
          className={cn(
            "absolute inset-y-0 left-0 w-1",
            GRAPH_CATEGORY_STYLES[category].bar,
          )}
        />
      )}
      {type !== "start" && (
        <Handle
          type="target"
          position={Position.Left}
          isConnectable={isConnectable}
          className="!size-2.5 !border-2 !border-background !bg-muted-foreground"
        />
      )}
      {capsule ? (
        <>
          <GraphNodeIcon type={type} className="size-5" />
          <span className="min-w-0 truncate text-body font-medium">
            {data.node.label || data.label}
          </span>
          {data.detail && (
            <span className="truncate text-caption text-muted-foreground">
              {data.detail}
            </span>
          )}
        </>
      ) : (
        <>
          <div className="flex items-center gap-2.5">
            <GraphNodeIcon type={type} />
            <div className="min-w-0 flex-1">
              <p className="truncate text-caption text-muted-foreground">
                {data.label}
              </p>
              <p className="truncate text-body font-semibold">
                {data.node.label || data.node.nodeKey}
              </p>
            </div>
          </div>
          {data.detail && (
            <p className="mt-1.5 line-clamp-2 whitespace-pre-wrap break-words text-caption leading-5 text-muted-foreground">
              {data.detail}
            </p>
          )}
        </>
      )}
      {data.state && tone && !capsule && (
        <p
          className={cn(
            "mt-1.5 flex items-center gap-1 text-caption font-medium",
            TONE_TEXT[tone],
          )}
        >
          {StateIcon && (
            <StateIcon
              className={cn("size-3.5", tone === "live" && "animate-spin")}
              aria-hidden="true"
            />
          )}
          {stateLabels[data.state] ?? data.state}
        </p>
      )}
      {data.mark && (
        <span
          className={cn(
            "absolute -top-2.5 right-3 rounded-full border bg-background px-1.5 text-micro font-medium",
            data.mark === "added"
              ? "border-success text-success"
              : "border-warning text-warning",
          )}
        >
          {data.mark === "added"
            ? t(($) => $.studio.ux.mark_added)
            : t(($) => $.studio.ux.mark_changed)}
        </span>
      )}
      {Boolean(data.issues) && (
        <span
          role="img"
          aria-label={t(($) => $.studio.ux.node_issues, {
            count: data.issues ?? 0,
          })}
          className="absolute -right-2 -top-2 flex size-5 items-center justify-center rounded-full bg-destructive text-micro font-semibold text-white"
        >
          !
        </span>
      )}
      {type === "branch" ? (
        <>
          <Handle
            id="true"
            type="source"
            position={Position.Right}
            style={{ top: "35%" }}
            isConnectable={isConnectable}
            className="!size-2.5 !border-background !bg-success"
          />
          <Handle
            id="false"
            type="source"
            position={Position.Right}
            style={{ top: "75%" }}
            isConnectable={isConnectable}
            className="!size-2.5 !border-background !bg-warning"
          />
        </>
      ) : (
        type !== "end" && (
          <Handle
            type="source"
            position={Position.Right}
            isConnectable={isConnectable}
            className="!size-2.5 !border-2 !border-background !bg-muted-foreground"
          />
        )
      )}
    </div>
  );
}

/** Lets the custom edge reach the insert callback without rebuilding edges. */
const InsertContext = createContext<
  ((edge: GraphEdge, type: string) => void) | null
>(null);

function GraphFlowEdge({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  markerEnd,
  style,
  label,
  data,
  selected,
}: EdgeProps<FlowEdge>) {
  const { t } = useT("graphs");
  const insert = useContext(InsertContext);
  const [path, labelX, labelY] = getSmoothStepPath({
    sourceX,
    sourceY,
    targetX,
    targetY,
    sourcePosition,
    targetPosition,
  });
  return (
    <>
      <BaseEdge id={id} path={path} markerEnd={markerEnd} style={style} />
      {(label || (insert && data)) && (
        <EdgeLabelRenderer>
          <div
            className="nodrag nopan pointer-events-auto absolute flex items-center gap-1"
            style={{
              transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)`,
            }}
          >
            {label && (
              <span className="rounded bg-background px-1 text-micro text-muted-foreground">
                {label}
              </span>
            )}
            {insert && data && (
              <GraphAddNodeMenu
                align="center"
                exclude={["join"]}
                onPick={(type) => insert(data.edge, type)}
                trigger={
                  <button
                    type="button"
                    aria-label={t(($) => $.studio.ux.insert_step)}
                    className={cn(
                      "flex size-5 items-center justify-center rounded-full border bg-background text-muted-foreground shadow-sm transition-opacity hover:border-primary hover:text-primary focus-visible:opacity-100",
                      selected ? "opacity-100" : "opacity-60 hover:opacity-100",
                    )}
                  >
                    <Plus className="size-3" />
                  </button>
                }
              />
            )}
          </div>
        </EdgeLabelRenderer>
      )}
    </>
  );
}

const nodeTypes = { graph: GraphFlowNode };
const edgeTypes = { graph: GraphFlowEdge };

export function GraphCanvas({
  definition,
  onChange,
  onSelect,
  onInsertOnEdge,
  selectedKey,
  disabled,
  onError,
  states,
  details,
  marks,
  issues,
  overview,
  className,
  children,
}: {
  definition: GraphDefinition;
  onChange?: (definition: GraphDefinition) => void;
  onSelect?: (key: string) => void;
  /** Shows a "+" on every connection; picking a step type calls this. */
  onInsertOnEdge?: (edge: GraphEdge, type: string) => void;
  selectedKey?: string;
  disabled?: boolean;
  onError?: (error: Error) => void;
  states?: Record<string, string>;
  /** Per-node line shown under the label. The run page passes what a node produced. */
  details?: Record<string, string>;
  /** Steps the last AI turn added or changed. */
  marks?: Record<string, GraphNodeMark>;
  /** Validation problems per node key. */
  issues?: Record<string, number>;
  /**
   * Read the graph as a whole: ignore the viewport whoever last edited the
   * draft left behind, and let the fit zoom out far enough to include every
   * node. A run is read as a shape, so half of it hidden past the pane edge is
   * the worst outcome; an editor is read node by node, so it stays legible.
   */
  overview?: boolean;
  className?: string;
  /** Extra `<Panel>`s, rendered inside the flow. */
  children?: ReactNode;
}) {
  const labels = useGraphNodeLabels();
  const { t } = useT("graphs");
  const [dragPositions, setDragPositions] = useState<
    Record<string, { x: number; y: number }>
  >({});
  const [selectedEdges, setSelectedEdges] = useState<string[]>([]);
  const positions = useMemo(
    () => layoutGraph(definition).layout!.nodes,
    [definition],
  );
  const editable = !disabled && Boolean(onChange);
  const nodes: FlowNode[] = definition.nodes.map((node) => ({
    id: node.nodeKey,
    ariaLabel: `${labels[node.type] ?? node.type} · ${node.label || node.nodeKey}`,
    type: "graph",
    data: {
      node,
      label: labels[node.type] ?? node.type,
      detail:
        details?.[node.nodeKey] ||
        node.instructions?.split(/\r?\n/)[0] ||
        node.http?.url ||
        node.script?.language ||
        "",
      state: states?.[node.nodeKey],
      mark: marks?.[node.nodeKey],
      issues: issues?.[node.nodeKey],
    },
    position:
      dragPositions[node.nodeKey] ??
      definition.layout?.nodes[node.nodeKey] ??
      positions[node.nodeKey]!,
    selected: selectedKey === node.nodeKey,
    deletable: !["start", "end"].includes(node.type),
  }));
  const edges: FlowEdge[] = definition.edges.map((edge, index) => {
    const id = `${index}:${edge.from}:${edge.port ?? ""}:${edge.to}`;
    const source = definition.nodes.find((node) => node.nodeKey === edge.from);
    return {
      id,
      type: "graph",
      data:
        editable && source?.type !== "parallel" ? { edge } : undefined,
      selected: selectedEdges.includes(id),
      source: edge.from,
      target: edge.to,
      sourceHandle: edge.port,
      label:
        edge.port === "true"
          ? t(($) => $.production.true_path)
          : edge.port === "false"
            ? t(($) => $.production.false_path)
            : undefined,
      markerEnd: { type: MarkerType.ArrowClosed },
      animated: states?.[edge.from] === "running",
    };
  });
  const style = {
    "--xy-background-color": "var(--background)",
    "--xy-node-color": "var(--foreground)",
    "--xy-edge-stroke": "var(--muted-foreground)",
    "--xy-edge-stroke-selected": "var(--primary)",
    "--xy-controls-button-background-color": "var(--card)",
    "--xy-controls-button-color": "var(--foreground)",
    "--xy-controls-button-border-color": "var(--border)",
    "--xy-minimap-background-color": "var(--card)",
    "--xy-background-pattern-dots-color": "var(--border)",
  } as CSSProperties;
  const insert =
    editable && onInsertOnEdge
      ? (edge: GraphEdge, type: string) => {
          if (canInsertGraphNode(type)) onInsertOnEdge(edge, type);
        }
      : null;
  return (
    <div
      className={cn(
        "h-[560px] min-h-80 w-full overflow-hidden rounded-xl border bg-background lg:h-full",
        className,
      )}
      aria-label={t(($) => $.studio.flow)}
    >
      <InsertContext.Provider value={insert}>
        <ReactFlow<FlowNode, FlowEdge>
          nodes={nodes}
          edges={edges}
          nodeTypes={nodeTypes}
          edgeTypes={edgeTypes}
          style={style}
          fitView={overview || !definition.layout?.viewport}
          fitViewOptions={{
            minZoom: overview ? 0.3 : 0.65,
            maxZoom: 1,
            padding: 0.12,
          }}
          defaultViewport={overview ? undefined : definition.layout?.viewport}
          minZoom={0.1}
          maxZoom={4}
          nodesDraggable={editable}
          nodesConnectable={editable}
          edgesReconnectable={editable}
          elementsSelectable
          deleteKeyCode={editable ? ["Backspace", "Delete"] : null}
          onNodeClick={(_, node) => onSelect?.(node.id)}
          onEdgesChange={(changes) =>
            setSelectedEdges((current) => {
              const next = new Set(current);
              for (const change of changes)
                if (change.type === "select") {
                  if (change.selected) next.add(change.id);
                  else next.delete(change.id);
                }
              return [...next];
            })
          }
          onNodesChange={(changes) => {
            for (const change of changes)
              if (change.type === "select" && change.selected)
                onSelect?.(change.id);
            if (!editable) return;
            const positionsChanged = changes.filter(
              (change) => change.type === "position" && change.position,
            );
            if (positionsChanged.length) {
              const nextPositions = {
                ...positions,
                ...definition.layout?.nodes,
                ...dragPositions,
              };
              for (const change of positionsChanged)
                if (change.type === "position" && change.position)
                  nextPositions[change.id] = change.position;
              if (
                positionsChanged.some(
                  (change) => change.type === "position" && change.dragging,
                )
              )
                setDragPositions(nextPositions);
              else {
                setDragPositions({});
                onChange?.({
                  ...definition,
                  layout: { ...definition.layout, nodes: nextPositions },
                });
              }
            }
          }}
          onDelete={({ nodes: removedNodes, edges: removedEdges }) => {
            if (!editable) return;
            const removed = new Set(removedNodes.map((node) => node.id));
            const edgeIds = new Set(removedEdges.map((edge) => edge.id));
            onChange?.({
              ...definition,
              nodes: definition.nodes.filter(
                (node) => !removed.has(node.nodeKey),
              ),
              edges: definition.edges.filter(
                (edge, index) =>
                  !removed.has(edge.from) &&
                  !removed.has(edge.to) &&
                  !edgeIds.has(edges[index]!.id),
              ),
              ...(definition.layout
                ? {
                    layout: {
                      ...definition.layout,
                      nodes: Object.fromEntries(
                        Object.entries(definition.layout.nodes).filter(
                          ([key]) => !removed.has(key),
                        ),
                      ),
                    },
                  }
                : {}),
            });
          }}
          onConnect={(connection) => {
            if (!editable) return;
            try {
              onChange?.(
                connectGraphNodes(definition, {
                  from: connection.source,
                  to: connection.target,
                  ...(connection.sourceHandle
                    ? { port: connection.sourceHandle }
                    : {}),
                }),
              );
            } catch (error) {
              onError?.(
                error instanceof Error ? error : new Error(String(error)),
              );
            }
          }}
          onReconnect={(oldEdge, connection) => {
            if (!editable) return;
            try {
              const withoutOld = {
                ...definition,
                edges: definition.edges.filter(
                  (_, index) => edges[index]!.id !== oldEdge.id,
                ),
              };
              onChange?.(
                connectGraphNodes(withoutOld, {
                  from: connection.source,
                  to: connection.target,
                  ...(connection.sourceHandle
                    ? { port: connection.sourceHandle }
                    : {}),
                }),
              );
            } catch (error) {
              onError?.(
                error instanceof Error ? error : new Error(String(error)),
              );
            }
          }}
          onMoveEnd={(event, viewport) => {
            if (
              editable &&
              event &&
              JSON.stringify(viewport) !==
                JSON.stringify(definition.layout?.viewport)
            )
              onChange?.({
                ...definition,
                layout: {
                  nodes: { ...positions, ...definition.layout?.nodes },
                  viewport,
                },
              });
          }}
        >
          {children}
          <Background gap={20} size={1} />
          <Controls showInteractive={false} />
          <MiniMap
            pannable
            zoomable
            nodeColor="var(--muted)"
            maskColor="var(--background)"
            className="!hidden sm:!block"
          />
        </ReactFlow>
      </InsertContext.Provider>
    </div>
  );
}

/** Colour key for a run's flow: what each node tone means. */
export function GraphStateLegend() {
  const { t } = useT("graphs");
  const items: [string, string][] = [
    ["bg-success", t(($) => $.studio.ux.legend_done)],
    ["bg-info", t(($) => $.studio.ux.legend_running)],
    ["bg-warning", t(($) => $.studio.ux.legend_waiting)],
    ["bg-destructive", t(($) => $.studio.ux.legend_failed)],
    ["bg-muted-foreground/40", t(($) => $.studio.ux.legend_idle)],
  ];
  return (
    <ul className="flex flex-wrap items-center gap-3 rounded-full border bg-background/90 px-3 py-1 text-micro text-muted-foreground shadow-sm backdrop-blur">
      {items.map(([dot, label]) => (
        <li key={label} className="flex items-center gap-1.5">
          <span className={cn("size-2 rounded-full", dot)} aria-hidden="true" />
          {label}
        </li>
      ))}
    </ul>
  );
}
