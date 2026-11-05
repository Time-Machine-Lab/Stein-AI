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
  Minus,
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
    /** Who does the work: an agent or the approvers, by name. */
    actor?: string;
    /** One short fact about the step's configuration. */
    meta?: string;
    state?: string;
    mark?: GraphNodeMark;
    issues?: number;
    portLabels?: { yes: string; no: string };
  },
  "graph"
>;
type EdgeTone = "default" | "yes" | "no" | "live" | "idle";
type FlowEdge = Edge<{ edge?: GraphEdge; tone: EdgeTone }, "graph">;

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
  skipped: { tone: "idle", icon: Minus },
  pending: { tone: "idle" },
  ready: { tone: "idle" },
};

const TONE_FRAME = {
  done: "border-success/50",
  live: "border-info ring-3 ring-info/15",
  wait: "border-warning ring-3 ring-warning/20",
  bad: "border-destructive/60 ring-3 ring-destructive/10",
  idle: "border-dashed",
};
const TONE_PILL = {
  done: "bg-success/10 text-success",
  live: "bg-info/10 text-info",
  wait: "bg-warning/15 text-warning",
  bad: "bg-destructive/10 text-destructive",
  idle: "bg-muted text-muted-foreground",
};
const EDGE_STROKE: Record<EdgeTone, string> = {
  default: "var(--muted-foreground)",
  yes: "var(--success)",
  no: "var(--warning)",
  live: "var(--info)",
  idle: "var(--border)",
};

const HANDLE =
  "!size-2.5 !min-h-0 !min-w-0 !rounded-full !border-2 !border-background !bg-muted-foreground transition-transform hover:!scale-150 hover:!bg-primary";

function GraphFlowNode({ data, selected, isConnectable }: NodeProps<FlowNode>) {
  const { t } = useT("graphs");
  const stateLabels = useGraphStateLabels();
  const type = data.node.type;
  const capsule = type === "start" || type === "end";
  const category = graphNodeCategory(type);
  const tone = data.state ? (STATE_TONE[data.state]?.tone ?? "idle") : null;
  const StateIcon = data.state ? STATE_TONE[data.state]?.icon : undefined;
  const frame = selected
    ? "border-primary ring-3 ring-primary/15"
    : data.mark === "added"
      ? "border-success border-dashed ring-3 ring-success/15"
      : data.mark === "changed"
        ? "border-warning ring-3 ring-warning/15"
        : tone
          ? TONE_FRAME[tone]
          : "border-border";
  const markBadge = data.mark && (
    <span
      className={cn(
        "absolute -top-2.5 left-3 rounded-full px-1.5 text-micro font-medium text-white",
        data.mark === "added" ? "bg-success" : "bg-warning",
      )}
    >
      {data.mark === "added"
        ? t(($) => $.studio.ux.mark_added)
        : t(($) => $.studio.ux.mark_changed)}
    </span>
  );
  const issueBadge = Boolean(data.issues) && (
    <span
      role="img"
      aria-label={t(($) => $.studio.ux.node_issues, {
        count: data.issues ?? 0,
      })}
      className="absolute -right-2 -top-2 flex size-5 items-center justify-center rounded-full bg-destructive text-micro font-semibold text-white ring-2 ring-background"
    >
      {data.issues}
    </span>
  );
  const statePill = data.state && tone && (
    <span
      className={cn(
        "inline-flex shrink-0 items-center gap-1 rounded-full px-1.5 py-0.5 text-micro font-medium",
        TONE_PILL[tone],
      )}
    >
      {StateIcon && (
        <StateIcon
          className={cn("size-3", tone === "live" && "animate-spin")}
          aria-hidden="true"
        />
      )}
      {stateLabels[data.state] ?? data.state}
    </span>
  );

  if (capsule)
    return (
      <div
        className={cn(
          "relative flex min-w-40 items-center gap-2 rounded-full border bg-card py-1.5 pl-1.5 pr-4 shadow-xs transition-shadow hover:shadow-md",
          frame,
        )}
      >
        {type === "end" && (
          <Handle
            type="target"
            position={Position.Left}
            isConnectable={isConnectable}
            className={HANDLE}
          />
        )}
        <GraphNodeIcon
          type={type}
          className="size-7 rounded-full [&>svg]:size-3.5"
        />
        <span className="min-w-0">
          <span className="block truncate text-body font-medium leading-tight">
            {data.node.label || data.label}
          </span>
          {data.meta && (
            <span className="block truncate text-micro text-muted-foreground">
              {data.meta}
            </span>
          )}
        </span>
        {statePill && <span className="ms-auto ps-1">{statePill}</span>}
        {markBadge}
        {issueBadge}
        {type === "start" && (
          <Handle
            type="source"
            position={Position.Right}
            isConnectable={isConnectable}
            className={HANDLE}
          />
        )}
      </div>
    );

  return (
    <div
      className={cn(
        "relative w-64 rounded-xl border bg-card shadow-xs transition-[box-shadow,border-color] hover:shadow-md",
        data.state === "skipped" && "bg-muted/40",
        frame,
      )}
    >
      <Handle
        type="target"
        position={Position.Left}
        isConnectable={isConnectable}
        className={HANDLE}
      />
      <div className="flex items-start gap-2.5 p-3">
        <GraphNodeIcon type={type} className="size-8 rounded-lg [&>svg]:size-4" />
        <div className="min-w-0 flex-1">
          <p className="flex min-w-0 items-center gap-1 text-micro">
            <span
              className={cn(
                "shrink-0 font-medium",
                GRAPH_CATEGORY_STYLES[category].text,
              )}
            >
              {data.label}
            </span>
            {data.actor && (
              <span className="min-w-0 truncate text-muted-foreground">
                · {data.actor}
              </span>
            )}
          </p>
          <p className="mt-0.5 line-clamp-2 break-words text-body font-semibold leading-snug">
            {data.node.label || data.node.nodeKey}
          </p>
        </div>
        {statePill}
      </div>
      {data.detail && (
        // Padding lives on the wrapper: on the clamped element itself it
        // would reveal the top of the third line.
        <div className="mx-3 -mt-1 mb-3 rounded-md bg-muted/60 px-2 py-1.5">
          <p className="line-clamp-2 whitespace-pre-wrap break-words text-caption leading-5 text-muted-foreground">
            {data.detail}
          </p>
        </div>
      )}
      {data.meta && (
        <p className="truncate border-t px-3 py-1.5 text-micro text-muted-foreground">
          {data.meta}
        </p>
      )}
      {markBadge}
      {issueBadge}
      {type === "branch" ? (
        <>
          {(
            [
              ["true", "35%", "!bg-success", data.portLabels?.yes, "text-success"],
              ["false", "70%", "!bg-warning", data.portLabels?.no, "text-warning"],
            ] as const
          ).map(([port, top, color, text, textColor]) => (
            <span key={port}>
              <Handle
                id={port}
                type="source"
                position={Position.Right}
                style={{ top }}
                isConnectable={isConnectable}
                className={cn(HANDLE, color)}
              />
              {text && (
                <span
                  className={cn(
                    "pointer-events-none absolute left-full ms-2 -translate-y-1/2 text-micro font-medium",
                    textColor,
                  )}
                  style={{ top }}
                >
                  {text}
                </span>
              )}
            </span>
          ))}
        </>
      ) : (
        <Handle
          type="source"
          position={Position.Right}
          isConnectable={isConnectable}
          className={HANDLE}
        />
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
  data,
  selected,
}: EdgeProps<FlowEdge>) {
  const { t } = useT("graphs");
  const insert = useContext(InsertContext);
  const [hovered, setHovered] = useState(false);
  const tone = data?.tone ?? "default";
  const [path, labelX, labelY] = getSmoothStepPath({
    sourceX,
    sourceY,
    targetX,
    targetY,
    sourcePosition,
    targetPosition,
    borderRadius: 14,
    offset: 20,
  });
  const active = hovered || selected;
  return (
    <>
      <BaseEdge
        id={id}
        path={path}
        markerEnd={markerEnd}
        style={{
          stroke: selected ? "var(--primary)" : EDGE_STROKE[tone],
          strokeWidth: active ? 2 : 1.5,
          strokeDasharray: tone === "idle" ? "5 4" : undefined,
          strokeOpacity: tone === "default" && !active ? 0.55 : 1,
          transition: "stroke-width 120ms, stroke-opacity 120ms",
        }}
      />
      {/* A wide invisible stroke so the connection is easy to hover. */}
      <path
        d={path}
        fill="none"
        stroke="transparent"
        strokeWidth={24}
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
      />
      {insert && data?.edge && (
        <EdgeLabelRenderer>
          <div
            className="nodrag nopan pointer-events-auto absolute"
            style={{
              transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)`,
            }}
            onMouseEnter={() => setHovered(true)}
            onMouseLeave={() => setHovered(false)}
          >
            <GraphAddNodeMenu
              align="center"
              exclude={["join"]}
              onPick={(type) => insert(data.edge!, type)}
              trigger={
                <button
                  type="button"
                  aria-label={t(($) => $.studio.ux.insert_step)}
                  className={cn(
                    "flex items-center justify-center rounded-full border bg-background shadow-sm transition-all",
                    active
                      ? "size-6 border-primary text-primary"
                      : "size-4 text-faint-foreground",
                  )}
                >
                  <Plus className={active ? "size-3.5" : "size-2.5"} />
                </button>
              }
            />
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
  actorNames,
  overview,
  fitOnOpen,
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
  /** Agent and member names by id, so a step can say who does the work. */
  actorNames?: Record<string, string>;
  /**
   * Read a run, not a draft: ignore the viewport whoever last edited the
   * draft left behind and open around the selected step at a readable zoom.
   */
  overview?: boolean;
  /**
   * Fit the whole flow when the canvas opens instead of restoring the saved
   * viewport, which was recorded for whatever pane size the last editor had.
   */
  fitOnOpen?: boolean;
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
  const portLabels = {
    yes: t(($) => $.studio.ux.port_yes),
    no: t(($) => $.studio.ux.port_no),
  };
  const stepActor = (node: GraphNodeDefinition): string | undefined => {
    if (node.agentId) return actorNames?.[node.agentId];
    const approvers =
      node.approval?.approverIds ?? (node.approverId ? [node.approverId] : []);
    const names = approvers
      .map((id) => actorNames?.[id])
      .filter((name): name is string => Boolean(name));
    if (!names.length) return undefined;
    return names.length > 2
      ? `${names.slice(0, 2).join("、")} +${names.length - 2}`
      : names.join("、");
  };
  const stepMeta = (node: GraphNodeDefinition): string | undefined => {
    const count = (value: unknown) =>
      value && typeof value === "object" ? Object.keys(value).length : 0;
    switch (node.type) {
      case "start": {
        const inputs = count(
          (definition.inputSchema as { properties?: unknown })?.properties,
        );
        return inputs
          ? t(($) => $.studio.ux.meta_inputs, { count: inputs })
          : undefined;
      }
      case "end": {
        const outputs = count(definition.outputs);
        return outputs
          ? t(($) => $.studio.ux.meta_outputs, { count: outputs })
          : undefined;
      }
      case "loop":
        return node.loop
          ? t(($) => $.studio.ux.meta_loop, {
              count: node.loop.maxIterations,
            })
          : undefined;
      case "delay":
        return node.delay
          ? t(($) => $.studio.ux.meta_delay, { count: node.delay.seconds })
          : undefined;
      case "http":
        return node.http?.method;
      case "script":
        return node.script?.language;
      case "subgraph":
        return t(($) => $.studio.ux.meta_steps, {
          count: (node.subgraph?.definition.nodes ?? []).filter(
            (item) => !["start", "end"].includes(item.type),
          ).length,
        });
      default:
        return undefined;
    }
  };
  const nodes: FlowNode[] = definition.nodes.map((node) => ({
    id: node.nodeKey,
    ariaLabel: `${labels[node.type] ?? node.type} · ${node.label || node.nodeKey}`,
    type: "graph",
    data: {
      node,
      label: labels[node.type] ?? node.type,
      detail:
        details?.[node.nodeKey] ||
        node.instructions?.split(/\r?\n/).find((line) => line.trim()) ||
        node.http?.url ||
        "",
      actor: stepActor(node),
      meta: stepMeta(node),
      portLabels,
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
    const tone: EdgeTone =
      states?.[edge.from] === "skipped" || states?.[edge.to] === "skipped"
        ? "idle"
        : states?.[edge.from] === "running"
          ? "live"
          : edge.port === "true"
            ? "yes"
            : edge.port === "false"
              ? "no"
              : "default";
    const edgeSelected = selectedEdges.includes(id);
    return {
      id,
      type: "graph",
      data: {
        tone,
        ...(editable && source?.type !== "parallel" ? { edge } : {}),
      },
      selected: edgeSelected,
      source: edge.from,
      target: edge.to,
      sourceHandle: edge.port,
      markerEnd: {
        type: MarkerType.ArrowClosed,
        width: 16,
        height: 16,
        color: edgeSelected ? "var(--primary)" : EDGE_STROKE[tone],
      },
      animated: tone === "live",
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
    "--xy-background-pattern-dots-color": "var(--border)",
  } as CSSProperties;
  // A canvas opens at a zoom where the cards can be read: a long flow fitted
  // whole is a row of unreadable boxes. The editor opens on the first two
  // columns of the automatic layout (340px apart); a run opens around the
  // step it is focused on, with a column either side. The rest is a pan away.
  const leftmost = Math.min(...nodes.map((node) => node.position.x));
  const anchor = overview
    ? nodes.find((node) => node.id === selectedKey)?.position.x
    : undefined;
  const leading = fitOnOpen
    ? nodes.filter((node) => node.position.x <= leftmost + 360)
    : anchor !== undefined
      ? nodes.filter((node) => Math.abs(node.position.x - anchor) <= 340)
      : undefined;
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
          fitView={overview || fitOnOpen || !definition.layout?.viewport}
          fitViewOptions={{
            minZoom: 0.85,
            maxZoom: 1,
            padding: 0.12,
            ...(leading?.length
              ? { nodes: leading.map((node) => ({ id: node.id })) }
              : {}),
          }}
          defaultViewport={
            overview || fitOnOpen ? undefined : definition.layout?.viewport
          }
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
          <Background gap={22} size={1.2} />
          {/* No minimap: beside a conversation or a record pane it is a blank
              box over the flow, and fit-to-view already does its job. */}
          <Controls
            showInteractive={false}
            className="!m-3 overflow-hidden !rounded-lg !border !shadow-sm [&_button]:!border-b-border [&_button]:!size-7"
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
