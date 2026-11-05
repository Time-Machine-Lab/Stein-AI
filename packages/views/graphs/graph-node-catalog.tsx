"use client";

import type { ReactElement } from "react";
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
  type LucideIcon,
} from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";

export type GraphNodeCategory = "agent" | "human" | "logic" | "tool" | "edge";

export const GRAPH_NODE_ICONS: Record<string, LucideIcon> = {
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

export function graphNodeCategory(type: string): GraphNodeCategory {
  switch (type) {
    case "agent":
      return "agent";
    case "approval":
      return "human";
    case "branch":
    case "parallel":
    case "join":
    case "loop":
    case "subgraph":
      return "logic";
    case "delay":
    case "http":
    case "script":
      return "tool";
    default:
      return "edge";
  }
}

/**
 * Category colour classes. Spelled out in full so Tailwind can see every class;
 * the colours themselves are the `--graph-*` tokens in `tokens.css`.
 */
export const GRAPH_CATEGORY_STYLES: Record<
  GraphNodeCategory,
  { bar: string; icon: string; dot: string; text: string }
> = {
  agent: {
    bar: "bg-graph-agent",
    icon: "bg-graph-agent/12 text-graph-agent ring-1 ring-inset ring-graph-agent/20",
    dot: "bg-graph-agent",
    text: "text-graph-agent",
  },
  human: {
    bar: "bg-graph-human",
    icon: "bg-graph-human/15 text-graph-human ring-1 ring-inset ring-graph-human/25",
    dot: "bg-graph-human",
    text: "text-graph-human",
  },
  logic: {
    bar: "bg-graph-logic",
    icon: "bg-graph-logic/12 text-graph-logic ring-1 ring-inset ring-graph-logic/20",
    dot: "bg-graph-logic",
    text: "text-graph-logic",
  },
  tool: {
    bar: "bg-graph-tool",
    icon: "bg-graph-tool/12 text-graph-tool ring-1 ring-inset ring-graph-tool/20",
    dot: "bg-graph-tool",
    text: "text-graph-tool",
  },
  edge: {
    bar: "bg-muted-foreground/40",
    icon: "bg-muted text-muted-foreground ring-1 ring-inset ring-border",
    dot: "bg-muted-foreground/40",
    text: "text-muted-foreground",
  },
};

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

const GROUPS: { key: "people" | "logic" | "tools"; types: string[] }[] = [
  { key: "people", types: ["agent", "approval"] },
  { key: "logic", types: ["branch", "parallel", "join", "loop", "subgraph"] },
  { key: "tools", types: ["delay", "http", "script"] },
];

export function GraphNodeIcon({
  type,
  className,
}: {
  type: string;
  className?: string;
}) {
  const Icon = GRAPH_NODE_ICONS[type] ?? Circle;
  return (
    <span
      className={cn(
        "flex size-7 shrink-0 items-center justify-center rounded-md",
        GRAPH_CATEGORY_STYLES[graphNodeCategory(type)].icon,
        className,
      )}
      aria-hidden="true"
    >
      <Icon className="size-3.5" />
    </span>
  );
}

/**
 * The one way to add a step: a grouped menu of step types. Used from the canvas
 * toolbar (append after the selection) and from the "+" on a connection
 * (insert on it), so both places offer the same vocabulary.
 */
export function GraphAddNodeMenu({
  trigger,
  onPick,
  exclude = [],
  disabled,
  align = "start",
}: {
  trigger: ReactElement;
  onPick: (type: string) => void;
  exclude?: string[];
  disabled?: boolean;
  align?: "start" | "center" | "end";
}) {
  const { t } = useT("graphs");
  const labels = useGraphNodeLabels();
  const groupLabels = {
    people: t(($) => $.studio.ux.group_people),
    logic: t(($) => $.studio.ux.group_logic),
    tools: t(($) => $.studio.ux.group_tools),
  };
  return (
    <DropdownMenu>
      <DropdownMenuTrigger render={trigger} disabled={disabled} />
      <DropdownMenuContent align={align} className="w-52">
        {GROUPS.map((group) => {
          const types = group.types.filter((type) => !exclude.includes(type));
          if (!types.length) return null;
          return (
            <DropdownMenuGroup key={group.key}>
              <DropdownMenuLabel>{groupLabels[group.key]}</DropdownMenuLabel>
              {types.map((type) => (
                <DropdownMenuItem key={type} onClick={() => onPick(type)}>
                  <GraphNodeIcon type={type} className="size-5" />
                  {labels[type] ?? type}
                </DropdownMenuItem>
              ))}
            </DropdownMenuGroup>
          );
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
