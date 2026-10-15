"use client";

import { useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft } from "lucide-react";
import { toast } from "sonner";
import {
  canApproveGraphRun,
  canManageGraphRun,
  canReplaceGraphApprover,
  graphControlState,
  graphNodeTasksOptions,
  graphRunActions,
  graphRunOptions,
  nodeCanRetry,
  useApproveGraphRun,
  useControlGraphRun,
  useReplaceGraphApprover,
  asSchema,
  graphStartOptions,
  graphPendingApprovals,
  GRAPH_TERMINAL_STATES,
  type GraphNodeRun,
  type GraphRun,
  type GraphRunAction,
  type GraphApproval,
  type GraphNodeDefinition,
  type GraphRepositoryOption,
  type JsonObject,
} from "@multica/core/graphs";
import type { MemberWithUser } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { memberListOptions } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { NativeSelect } from "@multica/ui/components/ui/native-select";
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
import { TranscriptButton } from "../common/task-transcript";
import { useT } from "../i18n";
import {
  Field,
  GraphError,
  GraphState,
  JsonDetails,
  useGraphActor,
  useGraphCommandKey,
} from "./shared";
import { GraphCanvas } from "./graph-canvas";
import { GraphArtifactCards, GraphValueView } from "./graph-result-view";

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

/** Appendix blocks of the rail: labelled, closed, out of the reading path. */
function RailDetails({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}) {
  return (
    <details className="min-w-0 rounded-xl border bg-card">
      <summary className="cursor-pointer px-4 py-3 text-body font-medium">
        {label}
      </summary>
      <div className="min-w-0 space-y-3 px-4 pb-4">{children}</div>
    </details>
  );
}

function NodeDetail({
  wsId,
  node,
  definition,
  canManage,
  pending,
  reason,
  onReasonChange,
  onAction,
}: {
  wsId: string;
  node: GraphNodeRun;
  definition?: GraphNodeDefinition;
  canManage: boolean;
  pending: boolean;
  reason: string;
  onReasonChange: (reason: string) => void;
  onAction: (action: GraphRunAction, nodeRunId?: string) => void;
}) {
  const { t } = useT("graphs");
  const paths = useWorkspacePaths();
  const tasks = useQuery(graphNodeTasksOptions(wsId, node.issueId ?? ""));
  return (
    <section
      id={`graph-node-${node.id}`}
      data-testid="node-detail"
      className="min-w-0 space-y-3 rounded-xl border bg-card p-4"
    >
      <div className="flex flex-wrap items-center gap-2">
        <h2 className="min-w-0 flex-1 break-all text-body font-semibold">
          {node.label || definition?.label || node.nodeKey}
        </h2>
        <GraphState state={node.state} />
      </div>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-caption text-muted-foreground">
        {node.issueId && (
          <AppLink
            href={paths.issueDetail(node.issueId)}
            className="underline underline-offset-4"
          >
            {t(($) => $.run.node_issue)}
          </AppLink>
        )}
        {["agent", "script"].includes(node.nodeType) && (
          <span>
            {t(($) => $.run.attempts, {
              used: node.attempts.length,
              max: node.maxAttempts + node.grantedAttempts,
            })}
          </span>
        )}
        {node.iteration && (
          <span>
            {t(($) => $.production.iteration)} {node.iteration}
          </span>
        )}
        {node.selectedPort && (
          <span className="font-medium text-foreground">
            {node.selectedPort === "true"
              ? t(($) => $.production.true_path)
              : t(($) => $.production.false_path)}
          </span>
        )}
      </div>
      {node.blockReason && (
        <p role="status" className="break-words text-body text-destructive">
          {node.blockReason === "execution_unknown"
            ? t(($) => $.run.execution_unknown)
            : node.blockReason}
        </p>
      )}
      {node.output && (
        <section className="space-y-2">
          <h3 className="text-caption font-medium text-muted-foreground">
            {t(($) => $.production.output)}
          </h3>
          <GraphValueView
            value={node.output}
            schema={definition?.outputSchema}
          />
        </section>
      )}
      {node.submissions.map((submission, index) => {
        const result = asSchema(submission.result);
        return (
          <section
            key={String(submission.id ?? index)}
            className="space-y-3 rounded-lg border p-3"
          >
            <div className="flex items-center gap-2">
              <h3 className="flex-1 text-body font-medium">
                {t(($) => $.production.results)}
              </h3>
              {typeof result.outcome === "string" && (
                <GraphState state={result.outcome} />
              )}
            </div>
            {!node.output && result.output !== undefined && (
              <GraphValueView
                value={result.output}
                schema={definition?.outputSchema}
              />
            )}
            {result.diagnostics !== undefined && (
              <GraphValueView value={result.diagnostics} />
            )}
            {Array.isArray(result.artifacts) && (
              <GraphArtifactCards artifacts={result.artifacts.map(asSchema)} />
            )}
            <JsonDetails
              label={t(($) => $.production.raw_details)}
              value={submission}
            />
          </section>
        );
      })}
      {canManage && node.state === "blocked" && (
        <div className="space-y-3 rounded-lg border p-3">
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
            {nodeCanRetry(node) && (
              <Button
                variant="outline"
                size="sm"
                disabled={pending}
                onClick={() => onAction("retry", node.id)}
              >
                {node.nodeType === "http"
                  ? t(($) => $.production.http_retry)
                  : t(($) => $.run.retry)}
              </Button>
            )}
            {node.nodeType !== "http" && node.canResolveBlock === true && (
              <Button
                variant="outline"
                size="sm"
                disabled={pending}
                onClick={() => onAction("resolve-block", node.id)}
              >
                {t(($) => $.run.resolve_block)}
              </Button>
            )}
          </div>
        </div>
      )}
      {node.attempts.map((attempt) => {
        const task = tasks.data?.find(
          (candidate) => candidate.id === attempt.taskId,
        );
        return (
          <div
            key={attempt.taskId}
            className="space-y-2 rounded-lg bg-muted/40 p-3"
          >
            <div className="flex flex-wrap items-center gap-2">
              <span className="min-w-0 flex-1 text-caption">
                {t(($) => $.run.attempts, {
                  used: attempt.attemptNo,
                  max: node.maxAttempts + node.grantedAttempts,
                })}
              </span>
              <GraphState state={attempt.state} />
              {task && (
                <TranscriptButton
                  task={task}
                  agentName={node.label || definition?.label || node.nodeKey}
                  isLive={
                    task.status === "running" || task.status === "dispatched"
                  }
                  title={t(($) => $.run.view_transcript)}
                />
              )}
            </div>
            <JsonDetails
              label={t(($) => $.production.raw_details)}
              value={{
                task_id: attempt.taskId,
                manifest: attempt.manifest,
                receipt: attempt.receipt,
              }}
            />
          </div>
        );
      })}
      {Object.keys(node.input).length > 0 && (
        <details className="min-w-0 rounded-lg border p-3">
          <summary className="cursor-pointer text-body font-medium">
            {t(($) => $.run.input)}
          </summary>
          <div className="mt-3">
            <GraphValueView value={node.input} />
          </div>
        </details>
      )}
    </section>
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

function RunDetail({ wsId, run }: { wsId: string; run: GraphRun }) {
  const { t } = useT("graphs");
  const actor = useGraphActor(wsId);
  const paths = useWorkspacePaths();
  const members = useQuery(memberListOptions(wsId));
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
  const [replacement, setReplacement] = useState<{
    approval: GraphApproval;
    previousApproverId: string;
  } | null>(null);
  const commandKey = useGraphCommandKey();
  const pending =
    control.isPending || approval.isPending || replaceApprover.isPending;
  const actions = graphRunActions(run, actor);
  const controlState = graphControlState(run);
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
  // A pending gate blocks the run, so it belongs above the flow where nothing
  // can hide it. A decided gate is history and belongs with the rest of it.
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
            selectedKey={selectedNode?.nodeKey}
            states={Object.fromEntries(
              visibleNodes.map((node) => [node.nodeKey, node.state]),
            )}
            details={Object.fromEntries(
              visibleNodes.map((node) => [node.nodeKey, nodeDetail(node)]),
            )}
            onSelect={(key) => {
              const node = visibleNodes.find((item) => item.nodeKey === key);
              if (!node) return;
              setSelectedNodeId(node.id);
              setNarrowPane("details");
            }}
          />
        ) : (
          <section className="space-y-3 rounded-xl border bg-card p-4">
            <h2 className="text-body font-semibold">{t(($) => $.run.nodes)}</h2>
            <ol className="space-y-2">
              {visibleNodes.map((node) => (
                <li key={node.id}>
                  <button
                    type="button"
                    className="flex w-full flex-wrap items-center gap-2 rounded-lg border px-3 py-2 text-left hover:bg-accent"
                    onClick={() => {
                      setSelectedNodeId(node.id);
                      setNarrowPane("details");
                    }}
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

  const railPane = (
    <aside
      data-testid="run-rail"
      aria-label={t(($) => $.run.details)}
      className={`${narrowPane === "details" ? "block" : "hidden xl:block"} min-h-0 min-w-0 space-y-4 xl:overflow-y-auto xl:pe-1`}
    >
      {run.output && (
        <section
          data-testid="run-result"
          className="min-w-0 space-y-3 rounded-xl border bg-card p-4"
        >
          <h2 className="text-body font-semibold">{t(($) => $.run.result)}</h2>
          <GraphValueView
            value={run.output}
            schema={run.definition?.outputSchema}
          />
        </section>
      )}
      {selectedNode ? (
        <NodeDetail
          wsId={wsId}
          node={selectedNode}
          definition={selectedDefinition}
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
      {run.artifacts.length > 0 && (
        <RailDetails label={t(($) => $.run.artifacts)}>
          <GraphArtifactCards
            artifacts={run.artifacts}
            repositories={repositories.data?.repositories}
          />
        </RailDetails>
      )}
      {settledGates.map((gate) => (
        <ApprovalCard
          key={gate.id}
          run={run}
          gate={gate}
          members={members.data ?? []}
          repositories={repositories.data?.repositories ?? []}
          canDecide={false}
          canReplace={false}
          pending={pending}
          reason={reason}
          onReasonChange={setReason}
          onDecide={decide}
          onReplace={() => undefined}
        />
      ))}
      <RailDetails label={t(($) => $.run.input)}>
        <GraphValueView
          value={run.input}
          schema={run.definition?.inputSchema}
        />
      </RailDetails>
      <RailDetails label={t(($) => $.production.events)}>
        <ol className="space-y-3">
          {run.events.map((event, index) => (
            <li key={String(event.id ?? index)} className="border-l-2 pl-4">
              <p className="text-caption text-muted-foreground">
                {typeof event.created_at === "string"
                  ? new Date(event.created_at).toLocaleString()
                  : ""}
              </p>
              <p className="mt-1 text-body font-medium">
                {String(event.type ?? event.event_type ?? event.kind ?? "")}
              </p>
              <GraphValueView
                value={event.payload ?? event.data ?? event.detail}
              />
            </li>
          ))}
        </ol>
      </RailDetails>
      <JsonDetails
        label={t(($) => $.production.raw_details)}
        value={{
          id: run.id,
          revision: run.revision,
          input_hash: run.inputHash,
          version_id: run.versionId,
        }}
      />
    </aside>
  );

  return (
    <div className="mx-auto flex max-w-[1400px] flex-col gap-4">
      <GraphError
        error={control.error ?? approval.error ?? replaceApprover.error}
      />
      <header className="flex flex-wrap items-start gap-x-4 gap-y-3">
        <div className="min-w-0 flex-1 space-y-1.5">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
            <AppLink
              href={paths.issueDetail(run.rootIssueId)}
              className="text-caption underline underline-offset-4"
            >
              {t(($) => $.run.parent)}
            </AppLink>
            <GraphState state={run.state} />
            {controlState && <GraphState state={controlState} />}
            <span className="text-caption text-muted-foreground">
              {t(($) => $.page.revision, { revision: run.revision })}
            </span>
            {run.previousRunId && (
              <AppLink
                href={paths.graphRun(run.previousRunId)}
                className="text-caption underline underline-offset-4"
              >
                {t(($) => $.production.previous_run)}
              </AppLink>
            )}
          </div>
          {run.graphName && (
            <h1 className="text-title font-semibold">{run.graphName}</h1>
          )}
          {run.blockReason && (
            <p role="status" className="text-body text-destructive">
              {run.blockReason}
            </p>
          )}
          {controlState === "cancel_requested" && (
            <p role="status" className="text-body text-muted-foreground">
              {t(($) => $.run.cancel_pending)}
            </p>
          )}
          {!actions.length && !canApproveGraphRun(run, actor) && (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.run.no_controls)}
            </p>
          )}
        </div>
        <div className="flex flex-wrap gap-2">
          {actions.map((action) => (
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
          {run.graphId &&
            GRAPH_TERMINAL_STATES.has(run.state) &&
            canManageGraphRun(run, actor) && (
              <AppLink
                href={`${paths.graphDetail(run.graphId)}?issue=${encodeURIComponent(run.rootIssueId)}&previous_run=${encodeURIComponent(run.id)}`}
                className="inline-flex rounded-md border px-3 py-2 text-body font-medium hover:bg-accent"
              >
                {t(($) => $.production.rework)}
              </AppLink>
            )}
        </div>
      </header>
      {pendingGates.map((gate) => (
        <ApprovalCard
          key={gate.id}
          run={run}
          gate={gate}
          members={members.data ?? []}
          repositories={repositories.data?.repositories ?? []}
          canDecide={
            pendingForActor.has(gate.id) && canApproveGraphRun(run, actor)
          }
          canReplace={canReplaceGraphApprover(run, actor)}
          pending={pending}
          reason={reason}
          onReasonChange={setReason}
          onDecide={decide}
          onReplace={(approvalGate, previousApproverId) =>
            setReplacement({
              approval: approvalGate,
              previousApproverId,
            })
          }
        />
      ))}
      <div className="flex gap-2 xl:hidden">
        <Button
          variant={narrowPane === "flow" ? "default" : "outline"}
          size="sm"
          onClick={() => setNarrowPane("flow")}
        >
          {t(($) => $.studio.flow)}
        </Button>
        <Button
          variant={narrowPane === "details" ? "default" : "outline"}
          size="sm"
          onClick={() => setNarrowPane("details")}
        >
          {t(($) => $.run.details)}
        </Button>
      </div>
      {/* The flow is the map and the rail is the record. Both are bounded so the
          page is one screen whatever the run produced, and each pane scrolls
          itself; letting a run's output set the page height is what turned this
          page into ninety screens of stacked cards. */}
      <div className="grid min-h-0 gap-4 xl:h-[min(760px,78vh)] xl:grid-cols-[minmax(0,1fr)_420px] xl:grid-rows-1">
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
      <div className="flex-1 overflow-auto p-4">
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
