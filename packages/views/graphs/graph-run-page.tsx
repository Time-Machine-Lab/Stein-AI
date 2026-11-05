"use client";

import { useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  AlertTriangle,
  ArrowLeft,
  MoreHorizontal,
  PauseCircle,
} from "lucide-react";
import { Panel } from "@xyflow/react";
import { toast } from "sonner";
import {
  canApproveGraphRun,
  canManageGraphRun,
  canReplaceGraphApprover,
  graphControlState,
  graphRunActions,
  graphRunOptions,
  nodeCanRetry,
  useApproveGraphRun,
  useControlGraphRun,
  useReplaceGraphApprover,
  asSchema,
  graphStartOptions,
  graphPendingApprovals,
  graphRunProgress,
  GRAPH_TERMINAL_STATES,
  type GraphNodeRun,
  type GraphRun,
  type GraphRunAction,
  type GraphApproval,
  type GraphRepositoryOption,
  type JsonObject,
} from "@multica/core/graphs";
import type { MemberWithUser } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { NativeSelect } from "@multica/ui/components/ui/native-select";
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@multica/ui/components/ui/tabs";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
  AlertDialogAction,
} from "@multica/ui/components/ui/alert-dialog";
import { AppLink, useNavigation } from "../navigation";
import { PageHeader } from "../layout/page-header";
import { useT, useTimeAgo } from "../i18n";
import {
  Field,
  GraphError,
  GraphState,
  JsonDetails,
  useGraphActor,
  useGraphCommandKey,
} from "./shared";
import { GraphCanvas, GraphStateLegend } from "./graph-canvas";
import { GraphArtifactCards, GraphValueView } from "./graph-result-view";
import {
  GraphRunArtifacts,
  GraphRunEvents,
  GraphRunInput,
  GraphRunRaw,
  GraphRunResult,
  GraphStepRecord,
} from "./graph-run-record";

// States worth opening a run page on, most urgent first. A run that is live,
// stuck, or failed is what the reader came for.
const FOCUS_STATES = [
  "running",
  "waiting_approval",
  "blocked",
  "failed",
  "queued",
  "dispatched",
  "started",
  "retry_wait",
];

// Field names an agent reaches for when it means "here is the answer". Preferred
// over whatever key happens to come first, so a node box shows its conclusion
// rather than its opening sentence.
const SUMMARY_KEYS = ["conclusion", "summary", "verdict", "topic", "title"];

function firstText(value: unknown, depth = 0): string {
  if (typeof value === "string") return value;
  if (depth > 4 || !value || typeof value !== "object") return "";
  if (Array.isArray(value)) return "";
  const entries = Object.entries(value as JsonObject);
  for (const key of SUMMARY_KEYS) {
    const match = entries.find(([name]) => name === key);
    if (match) {
      const text = firstText(match[1], depth + 1);
      if (text) return text;
    }
  }
  for (const [, item] of entries) {
    const text = firstText(item, depth + 1);
    if (text) return text;
  }
  return "";
}

/** What a node produced, in one line, for the node box on the flow. */
function nodeDetail(node: GraphNodeRun): string {
  return firstText(node.output).replace(/\s+/g, " ").trim().slice(0, 160);
}

function focusNodeId(nodes: GraphNodeRun[]): string {
  for (const state of FOCUS_STATES) {
    const match = nodes.find((node) => node.state === state);
    if (match) return match.id;
  }
  // Nothing needs attention: open the node that produced the result. Run output
  // comes from the last node that wrote one, not from the synthetic end node,
  // so the reader lands on the answer instead of an empty box.
  return (
    (nodes.filter((node) => node.output).at(-1) ?? nodes.at(-1))?.id ?? ""
  );
}

function ApprovalCard({
  run,
  gate,
  members,
  repositories,
  canDecide,
  canReplace,
  pending,
  reason,
  onReasonChange,
  onDecide,
  onReplace,
}: {
  run: GraphRun;
  gate: GraphApproval;
  members: MemberWithUser[];
  repositories: GraphRepositoryOption[];
  canDecide: boolean;
  canReplace: boolean;
  pending: boolean;
  reason: string;
  onReasonChange: (reason: string) => void;
  onDecide: (gate: GraphApproval, decision: "approved" | "rejected") => void;
  onReplace: (gate: GraphApproval, previousApproverId: string) => void;
}) {
  const { t } = useT("graphs");
  return (
    <section className="space-y-4 rounded-xl border bg-card p-4">
      <div className="flex items-center gap-2">
        <h2 className="flex-1 text-title font-medium">
          {(run.nodes.find((node) => node.id === gate.nodeRunId)?.label ||
            run.nodes.find((node) => node.id === gate.nodeRunId)?.nodeKey) ??
            t(($) => $.run.approval)}
        </h2>
        <GraphState state={gate.state} />
      </div>
      <p className="text-body text-muted-foreground">
        {t(($) => $.run.approval_help)}
      </p>
      {gate.candidate && (
        // While a gate is open the evidence is the thing being judged, so it is
        // already open. Once decided it is a record, and every node's output
        // unfolded would be the longest block on the page.
        <details
          open={gate.state === "pending"}
          className="min-w-0 rounded-lg bg-muted/20"
        >
          <summary className="cursor-pointer px-4 py-3 text-body font-medium">
            {t(($) => $.production.evidence)}
          </summary>
          <div className="min-w-0 space-y-4 px-4 pb-4">
            <GraphValueView
              value={gate.candidate.outputs ?? gate.candidate.inputs}
              schema={{
                properties: Object.fromEntries(
                  run.nodes.map((node) => [
                    node.nodeKey,
                    {
                      title: node.label || node.nodeKey,
                      ...run.definition?.nodes.find(
                        (item) => item.nodeKey === node.nodeKey,
                      )?.outputSchema,
                    },
                  ]),
                ),
              }}
            />
            {Array.isArray(gate.candidate.artifacts) && (
              <GraphArtifactCards
                artifacts={gate.candidate.artifacts.map(asSchema)}
                repositories={repositories}
              />
            )}
          </div>
        </details>
      )}
      <div className="space-y-2">
        {(gate.approverIds?.length ? gate.approverIds : [gate.approverId]).map(
          (memberId) => {
            const vote = gate.decisions?.find(
              (decision) => decision.memberId === memberId,
            );
            return (
              <div key={memberId} className="flex flex-wrap items-center gap-2">
                <span className="min-w-0 flex-1 text-body">
                  {members.find((member) => member.id === memberId)?.name ??
                    memberId}
                </span>
                {vote && <GraphState state={vote.decision} />}
                {canReplace && gate.state === "pending" && (
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => onReplace(gate, memberId)}
                  >
                    {t(($) => $.run.replace_approver)}
                  </Button>
                )}
                {vote?.reason && (
                  <p className="w-full text-caption text-muted-foreground">
                    {vote.reason}
                  </p>
                )}
              </div>
            );
          },
        )}
      </div>
      {canDecide ? (
        <div className="space-y-3">
          <Field label={t(($) => $.run.reason)}>
            {(id) => (
              <Textarea
                id={id}
                value={reason}
                onChange={(event) => onReasonChange(event.target.value)}
                placeholder={t(($) => $.run.reason_placeholder)}
                disabled={pending}
              />
            )}
          </Field>
          <div className="flex flex-wrap gap-2">
            <Button disabled={pending} onClick={() => onDecide(gate, "approved")}>
              {t(($) => $.run.approve)}
            </Button>
            <Button
              variant="outline"
              disabled={pending || !reason.trim()}
              onClick={() => onDecide(gate, "rejected")}
            >
              {t(($) => $.run.reject)}
            </Button>
          </div>
        </div>
      ) : (
        gate.state === "pending" && (
          <p className="text-caption text-muted-foreground">
            {t(($) => $.run.approval_waiting)}
          </p>
        )
      )}
      <JsonDetails
        label={t(($) => $.production.raw_details)}
        value={{
          id: gate.id,
          candidate_digest: gate.candidateDigest,
          policy: gate.mode,
          quorum: gate.quorum,
          candidate: gate.candidate,
        }}
      />
    </section>
  );
}

type RailTab =
  | "step"
  | "result"
  | "artifacts"
  | "approvals"
  | "input"
  | "events"
  | "raw";

/**
 * Something on this run is waiting for a person. One line with the action,
 * and the full gate (evidence, approvers, reason) one click away, so a pending
 * decision never pushes the flow off screen.
 */
function ApprovalBanner({
  title,
  canDecide,
  pending,
  expanded,
  onToggle,
  onApprove,
  children,
}: {
  title: string;
  canDecide: boolean;
  pending: boolean;
  expanded: boolean;
  onToggle: () => void;
  onApprove: () => void;
  children: ReactNode;
}) {
  const { t } = useT("graphs");
  return (
    <section className="rounded-xl border border-warning/40 bg-warning/5">
      <div className="flex flex-wrap items-center gap-3 px-4 py-2.5">
        <PauseCircle className="size-4 shrink-0 text-warning" />
        <p className="min-w-0 flex-1 text-body font-medium">{title}</p>
        <Button size="sm" variant="ghost" onClick={onToggle}>
          {expanded
            ? t(($) => $.studio.ux.hide_details)
            : t(($) => $.studio.ux.view_details)}
        </Button>
        {canDecide && (
          <>
            <Button
              size="sm"
              variant="outline"
              disabled={pending}
              // A rejection needs a reason, so it opens the gate rather than
              // deciding from here.
              onClick={() => {
                if (!expanded) onToggle();
              }}
            >
              {t(($) => $.studio.ux.reject)}
            </Button>
            <Button size="sm" disabled={pending} onClick={onApprove}>
              {t(($) => $.studio.ux.approve)}
            </Button>
          </>
        )}
      </div>
      {expanded && (
        <div className="border-t border-warning/30 p-3">{children}</div>
      )}
    </section>
  );
}

function RunDetail({ wsId, run }: { wsId: string; run: GraphRun }) {
  const { t } = useT("graphs");
  const actor = useGraphActor(wsId);
  const paths = useWorkspacePaths();
  const timeAgo = useTimeAgo();
  const members = useQuery(memberListOptions(wsId));
  const agents = useQuery(agentListOptions(wsId));
  const actorNames = Object.fromEntries([
    ...(agents.data ?? []).map((agent) => [agent.id, agent.name]),
    ...(members.data ?? []).flatMap((member) => [
      [member.id, member.name || member.email],
      [member.user_id, member.name || member.email],
    ]),
  ]) as Record<string, string>;
  const repositories = useQuery(graphStartOptions(wsId, run.rootIssueId));
  const control = useControlGraphRun(wsId);
  const approval = useApproveGraphRun(wsId);
  const replaceApprover = useReplaceGraphApprover(wsId);
  const [nextApproverId, setNextApproverId] = useState("");
  const [reason, setReason] = useState("");
  const [httpRetry, setHttpRetry] = useState<GraphNodeRun | null>(null);
  const [confirmCancel, setConfirmCancel] = useState(false);
  const [selectedScope, setSelectedScope] = useState("");
  const [selectedNodeId, setSelectedNodeId] = useState("");
  const [narrowPane, setNarrowPane] = useState<"flow" | "details">("flow");
  const [railTab, setRailTab] = useState<RailTab>("step");
  const [openGate, setOpenGate] = useState("");
  const [replacement, setReplacement] = useState<{
    approval: GraphApproval;
    previousApproverId: string;
  } | null>(null);
  const commandKey = useGraphCommandKey();
  const pending =
    control.isPending || approval.isPending || replaceApprover.isPending;
  const actions = graphRunActions(run, actor);
  const primaryActions = actions.filter((action) => action !== "archive");
  const controlState = graphControlState(run);
  const progress = graphRunProgress(run);
  const actionLabels: Record<GraphRunAction, string> = {
    pause: t(($) => $.run.pause),
    resume: t(($) => $.run.resume),
    cancel: t(($) => $.run.cancel),
    retry: t(($) => $.run.retry),
    "resolve-block": t(($) => $.run.resolve_block),
    archive: t(($) => $.run.archive),
  };
  function act(action: GraphRunAction, nodeRunId?: string) {
    const request = {
      id: run.id,
      action,
      expectedRevision: run.revision,
      nodeRunId,
      reason: reason.trim() || undefined,
    };
    control.mutate(
      { ...request, idempotencyKey: commandKey(request) },
      {
        onSuccess: () => {
          setReason("");
          setConfirmCancel(false);
          setHttpRetry(null);
          toast.success(t(($) => $.run.changed));
        },
      },
    );
  }
  function decide(gate: GraphApproval, decision: "approved" | "rejected") {
    const request = {
      id: run.id,
      expectedRevision: run.revision,
      approvalId: gate.id,
      candidateDigest: gate.candidateDigest,
      decision,
      reason: reason.trim() || undefined,
    };
    approval.mutate(
      { ...request, idempotencyKey: commandKey(request) },
      {
        onSuccess: () => {
          setReason("");
          setOpenGate("");
          toast.success(t(($) => $.run.decision_saved));
        },
      },
    );
  }

  const gates = run.approvals?.length
    ? run.approvals
    : run.approval
      ? [run.approval]
      : [];
  const pendingGates = gates.filter((gate) => gate.state === "pending");
  const settledGates = gates.filter((gate) => gate.state !== "pending");
  const pendingForActor = new Set(
    graphPendingApprovals(run, actor.memberId).map((gate) => gate.id),
  );
  const scopes = run.scopes ?? [];
  const currentScopeId =
    selectedScope ||
    String(scopes.find((scope) => !scope.parent_node_run_id)?.id ?? "");
  const visibleNodes = currentScopeId
    ? run.nodes.filter((node) => node.scopeId === currentScopeId)
    : run.nodes;
  const selectedScopeData = scopes.find((scope) => scope.id === currentScopeId);
  let visibleDefinition = run.definition;
  const definitionPath =
    typeof selectedScopeData?.definition_path === "string"
      ? selectedScopeData.definition_path.split("/").filter(Boolean)
      : [];
  for (
    let index = 0;
    visibleDefinition && index < definitionPath.length;
    index += 2
  ) {
    const node = visibleDefinition.nodes.find(
      (item) => item.nodeKey === definitionPath[index],
    );
    visibleDefinition = node?.loop?.body ?? node?.subgraph?.definition;
  }
  // Derive the selection instead of storing it: when the scope changes to one
  // that does not contain the selected node, this falls back to the node the
  // run page should be showing anyway.
  const selectedNode =
    visibleNodes.find((node) => node.id === selectedNodeId) ??
    visibleNodes.find((node) => node.id === focusNodeId(visibleNodes));
  const selectedDefinition = selectedNode
    ? visibleDefinition?.nodes.find(
        (item) => item.nodeKey === selectedNode.nodeKey,
      )
    : undefined;
  const blockedNode =
    run.state === "blocked"
      ? run.nodes.find((node) => node.state === "blocked")
      : undefined;
  const nodeName = (id?: string) => {
    const node = run.nodes.find((item) => item.id === id);
    return node?.label || node?.nodeKey || t(($) => $.run.approval);
  };
  const showNode = (node: GraphNodeRun) => {
    if (node.scopeId && node.scopeId !== currentScopeId)
      setSelectedScope(node.scopeId);
    setSelectedNodeId(node.id);
    setRailTab("step");
    setNarrowPane("details");
  };

  const approvalCard = (gate: GraphApproval) => (
    <ApprovalCard
      key={gate.id}
      run={run}
      gate={gate}
      members={members.data ?? []}
      repositories={repositories.data?.repositories ?? []}
      canDecide={
        gate.state === "pending" &&
        pendingForActor.has(gate.id) &&
        canApproveGraphRun(run, actor)
      }
      canReplace={
        gate.state === "pending" && canReplaceGraphApprover(run, actor)
      }
      pending={pending}
      reason={reason}
      onReasonChange={setReason}
      onDecide={decide}
      onReplace={(approvalGate, previousApproverId) =>
        setReplacement({ approval: approvalGate, previousApproverId })
      }
    />
  );

  const flowPane = (
    <div
      data-testid="run-flow"
      className={`${narrowPane === "flow" ? "flex" : "hidden xl:flex"} min-h-0 min-w-0 flex-col gap-3`}
    >
      {scopes.length > 1 && (
        <Field label={t(($) => $.production.scope)}>
          {(id) => (
            <NativeSelect
              id={id}
              value={currentScopeId}
              onChange={(event) => setSelectedScope(event.target.value)}
            >
              {scopes.map((scope) => (
                <option key={String(scope.id)} value={String(scope.id)}>
                  {String(
                    scope.definition_path || t(($) => $.production.root_flow),
                  )}
                  {scope.iteration
                    ? ` · ${t(($) => $.production.iteration)} ${scope.iteration}`
                    : ""}
                </option>
              ))}
            </NativeSelect>
          )}
        </Field>
      )}
      <div className="min-h-0 flex-1">
        {visibleDefinition ? (
          <GraphCanvas
            key={currentScopeId}
            definition={visibleDefinition}
            disabled
            overview
            actorNames={actorNames}
            selectedKey={selectedNode?.nodeKey}
            states={Object.fromEntries(
              visibleNodes.map((node) => [node.nodeKey, node.state]),
            )}
            details={Object.fromEntries(
              visibleNodes.map((node) => [node.nodeKey, nodeDetail(node)]),
            )}
            onSelect={(key) => {
              const node = visibleNodes.find((item) => item.nodeKey === key);
              if (node) showNode(node);
            }}
          >
            <Panel position="bottom-left" className="!mb-3 !ml-14">
              <GraphStateLegend />
            </Panel>
          </GraphCanvas>
        ) : (
          <section className="space-y-3 rounded-xl border bg-card p-4">
            <h2 className="text-body font-semibold">{t(($) => $.run.nodes)}</h2>
            <ol className="space-y-2">
              {visibleNodes.map((node) => (
                <li key={node.id}>
                  <button
                    type="button"
                    className="flex w-full flex-wrap items-center gap-2 rounded-lg border px-3 py-2 text-left hover:bg-accent"
                    onClick={() => showNode(node)}
                  >
                    <span className="min-w-0 flex-1 break-all text-body">
                      {node.label || node.nodeKey}
                    </span>
                    <GraphState state={node.state} />
                  </button>
                </li>
              ))}
            </ol>
          </section>
        )}
      </div>
    </div>
  );

  const tabs: { value: RailTab; label: string; count?: number; show: boolean }[] = [
    { value: "step", label: t(($) => $.studio.ux.tab_step), show: true },
    {
      value: "result",
      label: t(($) => $.studio.ux.tab_output),
      show: Boolean(run.output),
    },
    {
      value: "artifacts",
      label: t(($) => $.studio.ux.tab_artifacts),
      count: run.artifacts.length,
      show: run.artifacts.length > 0,
    },
    {
      value: "approvals",
      label: t(($) => $.studio.ux.rail_approvals),
      count: settledGates.length,
      show: settledGates.length > 0,
    },
    { value: "input", label: t(($) => $.studio.ux.rail_input), show: true },
    {
      value: "events",
      label: t(($) => $.studio.ux.tab_events),
      show: true,
    },
    { value: "raw", label: t(($) => $.studio.ux.rail_raw), show: true },
  ];
  const activeTab = tabs.some((tab) => tab.value === railTab && tab.show)
    ? railTab
    : "step";

  const railPane = (
    <aside
      data-testid="run-rail"
      aria-label={t(($) => $.run.details)}
      className={`${narrowPane === "details" ? "flex" : "hidden xl:flex"} min-h-0 min-w-0 flex-col overflow-hidden rounded-xl border bg-card`}
    >
      <Tabs
        value={activeTab}
        onValueChange={(value) => setRailTab(value as RailTab)}
        className="min-h-0 flex-1 gap-0"
      >
        <div className="shrink-0 overflow-x-auto border-b px-2">
          <TabsList variant="line">
            {tabs
              .filter((tab) => tab.show)
              .map((tab) => (
                <TabsTrigger
                  key={tab.value}
                  value={tab.value}
                  className="shrink-0 gap-1 px-2"
                >
                  {tab.label}
                  {tab.count ? (
                    <span className="rounded-full bg-muted px-1.5 text-micro tabular-nums text-muted-foreground">
                      {tab.count}
                    </span>
                  ) : null}
                </TabsTrigger>
              ))}
          </TabsList>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-5">
          <TabsContent value="step">
            {selectedNode ? (
              <GraphStepRecord
                wsId={wsId}
                node={selectedNode}
                definition={selectedDefinition}
                actorNames={actorNames}
                canManage={
                  !run.archivedAt &&
                  canManageGraphRun(run, actor) &&
                  run.controlState === "run" &&
                  run.state === "blocked"
                }
                pending={pending}
                reason={reason}
                onReasonChange={setReason}
                onAction={(action, nodeId) => {
                  if (selectedNode.nodeType === "http" && action === "retry") {
                    setReason("");
                    setHttpRetry(selectedNode);
                  } else act(action, nodeId);
                }}
              />
            ) : (
              <p className="rounded-xl border border-dashed p-4 text-caption text-muted-foreground">
                {t(($) => $.run.select_node)}
              </p>
            )}
          </TabsContent>
          <TabsContent value="result">
            <GraphRunResult run={run} />
          </TabsContent>
          <TabsContent value="artifacts">
            <GraphRunArtifacts
              run={run}
              repositories={repositories.data?.repositories ?? []}
            />
          </TabsContent>
          <TabsContent value="approvals" className="space-y-3">
            {settledGates.map(approvalCard)}
          </TabsContent>
          <TabsContent value="input">
            <GraphRunInput run={run} />
          </TabsContent>
          <TabsContent value="events">
            <GraphRunEvents run={run} actorNames={actorNames} />
          </TabsContent>
          <TabsContent value="raw">
            <GraphRunRaw run={run} />
          </TabsContent>
        </div>
      </Tabs>
    </aside>
  );

  const canRework = Boolean(
    run.graphId &&
      GRAPH_TERMINAL_STATES.has(run.state) &&
      canManageGraphRun(run, actor),
  );
  const archivable = actions.includes("archive");
  const hasMore =
    archivable || Boolean(run.previousRunId) || Boolean(run.graphId);

  return (
    <div className="mx-auto flex h-full w-full max-w-[1500px] flex-col gap-3">
      <GraphError
        error={control.error ?? approval.error ?? replaceApprover.error}
      />
      <header className="flex flex-wrap items-center gap-x-5 gap-y-3">
        <div className="min-w-0 flex-1 space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="min-w-0 truncate text-title font-semibold">
              {run.graphName || t(($) => $.run.title)}
            </h1>
            <GraphState state={run.state} />
            {controlState && <GraphState state={controlState} />}
          </div>
          <p className="flex flex-wrap items-center gap-x-2 text-caption text-muted-foreground">
            <span>#{run.id.slice(0, 8)}</span>
            {run.createdAt && <span>· {timeAgo(run.createdAt)}</span>}
            <span>·</span>
            <AppLink
              href={paths.issueDetail(run.rootIssueId)}
              className="underline underline-offset-4 hover:text-foreground"
            >
              {t(($) => $.run.parent)}
            </AppLink>
          </p>
        </div>
        {progress.total > 0 && (
          <div className="flex w-56 items-center gap-2">
            <div
              role="progressbar"
              aria-valuemin={0}
              aria-valuemax={progress.total}
              aria-valuenow={progress.done}
              className="h-2 flex-1 overflow-hidden rounded-full bg-muted"
            >
              <div
                className={cn(
                  "h-full rounded-full transition-[width]",
                  progress.failed ? "bg-destructive" : "bg-primary",
                )}
                style={{ width: `${(progress.done / progress.total) * 100}%` }}
              />
            </div>
            <span className="text-caption tabular-nums text-muted-foreground">
              {t(($) => $.studio.ux.progress, {
                done: progress.done,
                total: progress.total,
              })}
            </span>
          </div>
        )}
        <div className="flex flex-wrap items-center gap-2">
          {primaryActions.map((action) => (
            <Button
              key={action}
              variant={action === "cancel" ? "destructive" : "outline"}
              size="sm"
              disabled={pending}
              onClick={() =>
                action === "cancel" ? setConfirmCancel(true) : act(action)
              }
            >
              {actionLabels[action]}
            </Button>
          ))}
          {canRework && run.graphId && (
            <Button
              size="sm"
              render={
                <AppLink
                  href={`${paths.graphDetail(run.graphId)}?issue=${encodeURIComponent(run.rootIssueId)}&previous_run=${encodeURIComponent(run.id)}`}
                />
              }
              nativeButton={false}
            >
              {t(($) => $.production.rework)}
            </Button>
          )}
          {hasMore && (
            <DropdownMenu>
              <DropdownMenuTrigger
                render={<Button variant="ghost" size="icon-sm" />}
                aria-label={t(($) => $.studio.ux.more_actions)}
              >
                <MoreHorizontal className="size-4" />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-auto">
                {run.graphId && (
                  <DropdownMenuItem
                    render={<AppLink href={paths.graphDetail(run.graphId)} />}
                  >
                    {t(($) => $.studio.ux.open_graph)}
                  </DropdownMenuItem>
                )}
                {run.previousRunId && (
                  <DropdownMenuItem
                    render={
                      <AppLink href={paths.graphRun(run.previousRunId)} />
                    }
                  >
                    {t(($) => $.production.previous_run)}
                  </DropdownMenuItem>
                )}
                {archivable && (
                  <DropdownMenuItem
                    variant="destructive"
                    disabled={pending}
                    onClick={() => act("archive")}
                  >
                    {actionLabels.archive}
                  </DropdownMenuItem>
                )}
              </DropdownMenuContent>
            </DropdownMenu>
          )}
        </div>
      </header>
      {controlState === "cancel_requested" && (
        <p role="status" className="text-body text-muted-foreground">
          {t(($) => $.run.cancel_pending)}
        </p>
      )}
      {pendingGates.map((gate) => {
        const mine =
          pendingForActor.has(gate.id) && canApproveGraphRun(run, actor);
        const step = nodeName(gate.nodeRunId);
        return (
          <ApprovalBanner
            key={gate.id}
            title={
              mine
                ? t(($) => $.studio.ux.approval_needed, { step })
                : t(($) => $.studio.ux.approval_waiting, { step })
            }
            canDecide={mine}
            pending={pending}
            expanded={openGate === gate.id}
            onToggle={() => setOpenGate(openGate === gate.id ? "" : gate.id)}
            onApprove={() => decide(gate, "approved")}
          >
            {approvalCard(gate)}
          </ApprovalBanner>
        );
      })}
      {run.state === "blocked" && (
        <section
          role="status"
          className="flex flex-wrap items-center gap-3 rounded-xl border border-destructive/30 bg-destructive/5 px-4 py-2.5"
        >
          <AlertTriangle className="size-4 shrink-0 text-destructive" />
          <p className="min-w-0 flex-1 text-body">
            <span className="font-medium">
              {t(($) => $.studio.ux.blocked_title, {
                step: blockedNode
                  ? blockedNode.label || blockedNode.nodeKey
                  : "",
              })}
            </span>
            {(run.blockReason || blockedNode?.blockReason) && (
              <span className="ms-1 text-muted-foreground">
                {run.blockReason || blockedNode?.blockReason}
              </span>
            )}
          </p>
          {blockedNode && (
            <Button
              size="sm"
              variant="outline"
              onClick={() => showNode(blockedNode)}
            >
              {t(($) => $.studio.ux.open_step)}
            </Button>
          )}
        </section>
      )}
      {!actions.length &&
        !canApproveGraphRun(run, actor) &&
        !pendingGates.length && (
          <p className="text-caption text-muted-foreground">
            {t(($) => $.run.no_controls)}
          </p>
        )}
      <div className="flex gap-2 xl:hidden">
        <Button
          variant={narrowPane === "flow" ? "secondary" : "ghost"}
          size="sm"
          onClick={() => setNarrowPane("flow")}
        >
          {t(($) => $.studio.flow)}
        </Button>
        <Button
          variant={narrowPane === "details" ? "secondary" : "ghost"}
          size="sm"
          onClick={() => setNarrowPane("details")}
        >
          {t(($) => $.run.details)}
        </Button>
      </div>
      {/* The flow is the map and the rail is the record. Both fill the page
          and scroll themselves, so a run's output never sets the page height. */}
      <div className="grid min-h-[480px] flex-1 gap-3 xl:grid-cols-[minmax(0,1fr)_440px] xl:grid-rows-1">
        {flowPane}
        {railPane}
      </div>
      <AlertDialog
        open={Boolean(httpRetry)}
        onOpenChange={(open) => {
          if (!open && !pending) setHttpRetry(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.production.http_retry)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.production.http_retry_help)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <GraphError error={control.error} />
          <Field label={t(($) => $.run.reason)}>
            {(id) => (
              <Textarea
                id={id}
                value={reason}
                onChange={(event) => setReason(event.target.value)}
                disabled={pending}
              />
            )}
          </Field>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={pending}>
              {t(($) => $.production.cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              disabled={
                pending ||
                !reason.trim() ||
                !httpRetry ||
                !nodeCanRetry(httpRetry)
              }
              onClick={() => {
                if (httpRetry) act("retry", httpRetry.id);
              }}
            >
              {t(($) => $.run.confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <AlertDialog
        open={Boolean(replacement)}
        onOpenChange={(open) => {
          if (!open) setReplacement(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.run.replace_approver)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.production.replace_approver_help)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <Field label={t(($) => $.editor.approver)}>
            {(id) => (
              <NativeSelect
                id={id}
                className="w-full"
                value={nextApproverId}
                onChange={(event) => setNextApproverId(event.target.value)}
              >
                <option value="">{t(($) => $.editor.choose_approver)}</option>
                {(members.data ?? [])
                  .filter(
                    (member) =>
                      !(
                        replacement?.approval.approverIds ?? [
                          replacement?.approval.approverId,
                        ]
                      ).includes(member.id),
                  )
                  .map((member) => (
                    <option key={member.id} value={member.id}>
                      {member.name || member.email}
                    </option>
                  ))}
              </NativeSelect>
            )}
          </Field>
          <Field label={t(($) => $.run.reason)}>
            {(id) => (
              <Textarea
                id={id}
                value={reason}
                onChange={(event) => setReason(event.target.value)}
              />
            )}
          </Field>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.run.dismiss)}</AlertDialogCancel>
            <AlertDialogAction
              disabled={pending || !nextApproverId || !reason.trim()}
              onClick={() => {
                if (!replacement) return;
                const request = {
                  id: run.id,
                  expectedRevision: run.revision,
                  nodeRunId: replacement.approval.nodeRunId,
                  previousApproverId: replacement.previousApproverId,
                  approverId: nextApproverId,
                  reason: reason.trim(),
                };
                replaceApprover.mutate(
                  { ...request, idempotencyKey: commandKey(request) },
                  {
                    onSuccess: () => {
                      setReplacement(null);
                      setNextApproverId("");
                      setReason("");
                    },
                  },
                );
              }}
            >
              {t(($) => $.run.confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <AlertDialog open={confirmCancel} onOpenChange={setConfirmCancel}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.run.confirm_cancel)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.run.cancel_help)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <Field label={t(($) => $.run.reason)}>
            {(id) => (
              <Textarea
                id={id}
                value={reason}
                onChange={(event) => setReason(event.target.value)}
                placeholder={t(($) => $.run.reason_placeholder)}
                disabled={pending}
              />
            )}
          </Field>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.run.dismiss)}</AlertDialogCancel>
            <AlertDialogAction disabled={pending} onClick={() => act("cancel")}>
              {t(($) => $.run.confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

export function GraphRunPage() {
  const { t } = useT("graphs");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const id = navigation.pathname.split("/").filter(Boolean).at(-1) ?? "";
  const run = useQuery(graphRunOptions(wsId, id));
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader>
        <AppLink
          href={paths.graphs()}
          aria-label={t(($) => $.page.back)}
          className="rounded-md p-1 hover:bg-accent"
        >
          <ArrowLeft className="size-4" />
        </AppLink>
        <h1 className="min-w-0 flex-1 truncate text-body font-medium">
          {t(($) => $.run.title)}
        </h1>
        <Button variant="ghost" size="sm" onClick={() => void run.refetch()}>
          {t(($) => $.page.refresh)}
        </Button>
      </PageHeader>
      <div className="flex min-h-0 flex-1 flex-col overflow-auto p-4">
        <GraphError error={run.error} />
        {run.isPending ? (
          <p>{t(($) => $.page.loading)}</p>
        ) : run.data ? (
          <RunDetail
            key={`${wsId}:${run.data.id}`}
            wsId={wsId}
            run={run.data}
          />
        ) : (
          <p>{t(($) => $.page.unavailable)}</p>
        )}
      </div>
    </div>
  );
}
