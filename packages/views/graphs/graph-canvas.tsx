"use client";

import { useMemo, useState, type CSSProperties } from "react";
import {
  Background,
  Controls,
  Handle,
  MarkerType,
  MiniMap,
  Position,
  ReactFlow,
  type Node,
  type NodeProps,
  type Edge,
  useReactFlow,
  Panel,
} from "@xyflow/react";
import {
  Bot,
  CheckCircle2,
  Circle,
  Clock,
  Code2,
  GitBranch,
  GitFork,
  Globe,
  Layers,
  Merge,
  Repeat2,
  ShieldCheck,
} from "lucide-react";
import {
  connectGraphNodes,
  layoutGraph,
  type GraphDefinition,
  type GraphNodeDefinition,
} from "@multica/core/graphs";
import { Button } from "@multica/ui/components/ui/button";
import { useGraphStateLabels } from "./shared";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import "@xyflow/react/dist/style.css";

type FlowNode = Node<
  { node: GraphNodeDefinition; label: string; detail: string; state?: string },
  "graph"
>;
const icons = {
  start: Circle,
  end: CheckCircle2,
  agent: Bot,
  approval: ShieldCheck,
  branch: GitBranch,
  parallel: GitFork,
  join: Merge,
  loop: Repeat2,
  subgraph: Layers,
  delay: Clock,
  http: Globe,
  script: Code2,
};

function GraphFlowNode({ data, selected, isConnectable }: NodeProps<FlowNode>) {
  const stateLabels = useGraphStateLabels();
  const Icon = icons[data.node.type as keyof typeof icons] ?? Circle;
  return (
    <div
      className={cn(
        "w-56 rounded-xl border bg-card px-4 py-3 shadow-sm transition-shadow",
        selected ? "border-primary ring-2 ring-primary/20" : "border-border",
        data.state === "failed" && "border-destructive",
        data.state === "succeeded" && "border-success",
        data.state === "skipped" && "opacity-50",
      )}
    >
      {data.node.type !== "start" && (
        <Handle
          type="target"
          position={Position.Left}
          isConnectable={isConnectable}
          className="!size-3 !border-2 !border-background !bg-muted-foreground"
        />
      )}
      <div className="flex items-center gap-2.5">
        <span className="rounded-lg bg-muted p-2 text-muted-foreground">
          <Icon className="size-4" />
        </span>
        <div className="min-w-0">
          <p className="truncate text-caption text-muted-foreground">
            {data.label}
          </p>
          <p className="truncate text-body font-semibold">
            {data.node.label || data.node.nodeKey}
          </p>
        </div>
      </div>
      {data.detail && (
        <p className="mt-2 line-clamp-2 whitespace-pre-wrap break-words text-caption leading-5 text-muted-foreground">
          {data.detail}
        </p>
      )}
      {data.state && (
        <p className="mt-2 text-caption font-medium">
          {stateLabels[data.state] ?? data.state}
        </p>
      )}
      {data.node.type === "branch" ? (
        <>
          <Handle
            id="true"
            type="source"
            position={Position.Right}
            style={{ top: "35%" }}
            isConnectable={isConnectable}
            className="!size-3 !border-background !bg-success"
          />
          <Handle
            id="false"
            type="source"
            position={Position.Right}
            style={{ top: "75%" }}
            isConnectable={isConnectable}
            className="!size-3 !border-background !bg-warning"
          />
        </>
      ) : (
        data.node.type !== "end" && (
          <Handle
            type="source"
            position={Position.Right}
            isConnectable={isConnectable}
            className="!size-3 !border-2 !border-background !bg-muted-foreground"
          />
        )
      )}
    </div>
  );
}
const nodeTypes = { graph: GraphFlowNode };

function FocusNode({ nodeKey }: { nodeKey?: string }) {
  const { fitView } = useReactFlow();
  const { t } = useT("graphs");
  return nodeKey ? (
    <Panel position="top-left">
      <Button
        variant="outline"
        size="sm"
        onClick={() =>
          void fitView({
            nodes: [{ id: nodeKey }],
            maxZoom: 1.25,
            padding: 0.8,
            duration: 200,
          })
        }
      >
        {t(($) => $.production.focus_node)}
      </Button>
    </Panel>
  ) : null;
}

export function useGraphNodeLabels(): Record<string, string> {
  const { t } = useT("graphs");
  return {
    start: t(($) => $.studio.start),
    end: t(($) => $.studio.end),
    agent: t(($) => $.editor.agent),
    approval: t(($) => $.studio.approval),
    branch: t(($) => $.production.branch),
    parallel: t(($) => $.production.parallel),
    join: t(($) => $.production.join),
    loop: t(($) => $.production.loop),
    subgraph: t(($) => $.production.subgraph),
    delay: t(($) => $.production.delay),
    http: t(($) => $.production.http),
    script: t(($) => $.production.script),
  };
}

export function GraphCanvas({
  definition,
  onChange,
  onSelect,
  selectedKey,
  disabled,
  onError,
  states,
}: {
  definition: GraphDefinition;
  onChange?: (definition: GraphDefinition) => void;
  onSelect?: (key: string) => void;
  selectedKey?: string;
  disabled?: boolean;
  onError?: (error: Error) => void;
  states?: Record<string, string>;
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
  const nodes: FlowNode[] = definition.nodes.map((node) => ({
    id: node.nodeKey,
    ariaLabel: `${labels[node.type] ?? node.type} · ${node.label || node.nodeKey}`,
    type: "graph",
    data: {
      node,
      label: labels[node.type] ?? node.type,
      detail:
        node.instructions?.split(/\r?\n/)[0] ??
        node.http?.url ??
        node.script?.language ??
        "",
      state: states?.[node.nodeKey],
    },
    position:
      dragPositions[node.nodeKey] ??
      definition.layout?.nodes[node.nodeKey] ??
      positions[node.nodeKey]!,
    selected: selectedKey === node.nodeKey,
    deletable: !["start", "end"].includes(node.type),
  }));
  const edges: Edge[] = definition.edges.map((edge, index) => ({
    id: `${index}:${edge.from}:${edge.port ?? ""}:${edge.to}`,
    selected: selectedEdges.includes(
      `${index}:${edge.from}:${edge.port ?? ""}:${edge.to}`,
    ),
    source: edge.from,
    target: edge.to,
    sourceHandle: edge.port,
    label:
      edge.port === "true"
        ? t(($) => $.production.true_path)
        : edge.port === "false"
          ? t(($) => $.production.false_path)
          : undefined,
    type: "smoothstep",
    markerEnd: { type: MarkerType.ArrowClosed },
    animated: states?.[edge.from] === "running",
  }));
  const editable = !disabled && Boolean(onChange);
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
  return (
    <div
      className="h-[560px] min-h-80 w-full overflow-hidden rounded-xl border bg-background lg:h-full"
      aria-label={t(($) => $.studio.flow)}
    >
      <ReactFlow<FlowNode>
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        style={style}
        fitView={!definition.layout?.viewport}
        fitViewOptions={{ minZoom: 0.65, maxZoom: 1, padding: 0.12 }}
        defaultViewport={definition.layout?.viewport}
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
        <FocusNode nodeKey={selectedKey} />
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
    </div>
  );
}
