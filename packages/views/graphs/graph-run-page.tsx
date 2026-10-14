"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft } from "lucide-react";
import { toast } from "sonner";
import {
  canApproveGraphRun,
  canManageGraphRun,
  canReplaceGraphApprover,
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
} from "@multica/core/graphs";
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

function NodeTimeline({
  wsId,
  node,
  definition,
  canManage,
  pending,
  onAction,
}: {
  wsId: string;
  node: GraphNodeRun;
  definition?: GraphNodeDefinition;
  canManage: boolean;
  pending: boolean;
  onAction: (action: GraphRunAction, nodeRunId?: string) => void;
}) {
  const { t } = useT("graphs");
  const paths = useWorkspacePaths();
  const tasks = useQuery(graphNodeTasksOptions(wsId, node.issueId ?? ""));
  return (
    <li
      id={`graph-node-${node.id}`}
      className="min-w-0 space-y-4 rounded-xl border p-4"
    >
      <div className="flex flex-wrap items-center gap-2">
        <h3 className="min-w-0 flex-1 break-all font-medium">
          {node.label || definition?.label || node.nodeKey}
        </h3>
        <GraphState state={node.state} />
        {node.issueId && (
          <AppLink
            href={paths.issueDetail(node.issueId)}
            className="text-caption underline underline-offset-4"
          >
            {t(($) => $.run.node_issue)}
          </AppLink>
        )}
      </div>
      {["agent", "script"].includes(node.nodeType) && (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.run.attempts, {
            used: node.attempts.length,
            max: node.maxAttempts,
          })}
        </p>
      )}
      {node.blockReason && (
        <p role="status" className="break-words text-body text-destructive">
          {node.blockReason === "execution_unknown"
            ? t(($) => $.run.execution_unknown)
            : node.blockReason}
        </p>
      )}
      {node.iteration && (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.production.iteration)} {node.iteration}
        </p>
      )}
      {node.selectedPort && (
        <p className="text-caption font-medium">
          {node.selectedPort === "true"
            ? t(($) => $.production.true_path)
            : t(($) => $.production.false_path)}
        </p>
      )}
      {Object.keys(node.input).length > 0 && (
        <details className="rounded-lg border p-3">
          <summary className="cursor-pointer text-body font-medium">
            {t(($) => $.run.input)}
          </summary>
          <div className="mt-3">
            <GraphValueView value={node.input} />
          </div>
        </details>
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
                  max: node.maxAttempts,
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
      {node.output && (
        <section className="space-y-3">
          <h4 className="text-body font-medium">
            {t(($) => $.production.output)}
          </h4>
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
              <h4 className="flex-1 text-body font-medium">
                {t(($) => $.production.results)}
              </h4>
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
        <div className="flex flex-wrap gap-2">
          {nodeCanRetry(node) && (
            <Button
              variant="outline"
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
              disabled={pending}
              onClick={() => onAction("resolve-block", node.id)}
            >
              {t(($) => $.run.resolve_block)}
            </Button>
          )}
        </div>
      )}
    </li>
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
  const [replacement, setReplacement] = useState<{
    approval: GraphApproval;
    previousApproverId: string;
  } | null>(null);
  const commandKey = useGraphCommandKey();
  const pending =
    control.isPending || approval.isPending || replaceApprover.isPending;
  const actions = graphRunActions(run, actor);
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
  return (
    <div className="mx-auto max-w-6xl space-y-6">
      <GraphError
        error={control.error ?? approval.error ?? replaceApprover.error}
      />
      <div className="flex flex-wrap items-center gap-3">
        <AppLink
          href={paths.issueDetail(run.rootIssueId)}
          className="text-body underline underline-offset-4"
        >
          {t(($) => $.run.parent)}
        </AppLink>
        <GraphState state={run.state} />
        <GraphState state={run.controlState} />
        <span className="text-caption text-muted-foreground">
          {t(($) => $.page.revision, { revision: run.revision })}
        </span>
      </div>
      {run.graphName && (
        <h1 className="text-title-lg font-semibold">{run.graphName}</h1>
      )}
      {run.previousRunId && (
        <AppLink
          href={paths.graphRun(run.previousRunId)}
          className="text-caption underline underline-offset-4"
        >
          {t(($) => $.production.previous_run)}
        </AppLink>
      )}
      {run.blockReason && (
        <p role="status" className="text-body text-destructive">
          {run.blockReason}
        </p>
      )}
      {run.controlState === "cancel_requested" && (
        <p role="status" className="text-body text-muted-foreground">
          {t(($) => $.run.cancel_pending)}
        </p>
      )}
      <div className="flex flex-wrap gap-2">
        {actions.map((action) => (
          <Button
            key={action}
            variant={action === "cancel" ? "destructive" : "outline"}
            disabled={pending}
            onClick={() =>
              action === "cancel" ? setConfirmCancel(true) : act(action)
            }
          >
            {actionLabels[action]}
          </Button>
        ))}
      </div>
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
      {!actions.length && !canApproveGraphRun(run, actor) && (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.run.no_controls)}
        </p>
      )}
      {(canManageGraphRun(run, actor) || canApproveGraphRun(run, actor)) && (
        <Field label={t(($) => $.run.reason)}>
          {(id) => (
            <Textarea
              id={id}
              value={reason}
              onChange={(event) => setReason(event.target.value)}
              placeholder={t(($) => $.run.reason_placeholder)}
            />
          )}
        </Field>
      )}
      <details className="rounded-xl border p-4">
        <summary className="cursor-pointer text-body font-medium">
          {t(($) => $.run.input)}
        </summary>
        <div className="mt-3">
          <GraphValueView
            value={run.input}
            schema={run.definition?.inputSchema}
          />
        </div>
      </details>
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
      {visibleDefinition && (
        <details className="rounded-xl border p-4">
          <summary className="cursor-pointer text-body font-medium">
            {t(($) => $.studio.flow)}
          </summary>
          <div className="mt-3">
            <GraphCanvas
              key={currentScopeId}
              definition={visibleDefinition}
              disabled
              states={Object.fromEntries(
                visibleNodes.map((node) => [node.nodeKey, node.state]),
              )}
              onSelect={(key) =>
                document
                  .getElementById(
                    `graph-node-${visibleNodes.find((node) => node.nodeKey === key)?.id}`,
                  )
                  ?.scrollIntoView({ behavior: "smooth", block: "center" })
              }
            />
          </div>
        </details>
      )}
      <section className="space-y-3">
        <h2 className="text-title font-medium">{t(($) => $.run.nodes)}</h2>
        <ol className="space-y-4">
          {visibleNodes.map((node) => (
            <NodeTimeline
              key={node.id}
              wsId={wsId}
              node={node}
              definition={visibleDefinition?.nodes.find(
                (item) => item.nodeKey === node.nodeKey,
              )}
              canManage={
                !run.archivedAt &&
                canManageGraphRun(run, actor) &&
                run.controlState === "run" &&
                run.state === "blocked"
              }
              pending={pending}
              onAction={(action, nodeId) => {
                if (node.nodeType === "http" && action === "retry") {
                  setReason("");
                  setHttpRetry(node);
                } else act(action, nodeId);
              }}
            />
          ))}
        </ol>
      </section>
      {run.artifacts.length > 0 && (
        <section className="space-y-3">
          <h2 className="text-title font-medium">
            {t(($) => $.run.artifacts)}
          </h2>
          <GraphArtifactCards
            artifacts={run.artifacts}
            repositories={repositories.data?.repositories}
          />
        </section>
      )}
      {run.output && (
        <section className="space-y-3 rounded-xl border p-4">
          <h2 className="text-title font-medium">
            {t(($) => $.production.output)}
          </h2>
          <GraphValueView
            value={run.output}
            schema={run.definition?.outputSchema}
          />
        </section>
      )}
      {gates.map((gate) => (
        <section key={gate.id} className="space-y-4 rounded-xl border p-4">
          <div className="flex items-center gap-2">
            <h2 className="flex-1 text-title font-medium">
              {(run.nodes.find((node) => node.id === gate.nodeRunId)?.label ||
                run.nodes.find((node) => node.id === gate.nodeRunId)
                  ?.nodeKey) ??
                t(($) => $.run.approval)}
            </h2>
            <GraphState state={gate.state} />
          </div>
          <p className="text-body text-muted-foreground">
            {t(($) => $.run.approval_help)}
          </p>
          {gate.candidate && (
            <div className="space-y-4 rounded-lg bg-muted/20 p-4">
              <h3 className="text-body font-medium">
                {t(($) => $.production.evidence)}
              </h3>
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
                  repositories={repositories.data?.repositories}
                />
              )}
            </div>
          )}
          <div className="space-y-2">
            {(gate.approverIds?.length
              ? gate.approverIds
              : [gate.approverId]
            ).map((memberId) => {
              const vote = gate.decisions?.find(
                (decision) => decision.memberId === memberId,
              );
              return (
                <div
                  key={memberId}
                  className="flex flex-wrap items-center gap-2"
                >
                  <span className="min-w-0 flex-1 text-body">
                    {members.data?.find((member) => member.id === memberId)
                      ?.name ?? memberId}
                  </span>
                  {vote && <GraphState state={vote.decision} />}
                  {canReplaceGraphApprover(run, actor) &&
                    gate.state === "pending" && (
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() =>
                          setReplacement({
                            approval: gate,
                            previousApproverId: memberId,
                          })
                        }
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
            })}
          </div>
          {pendingForActor.has(gate.id) && canApproveGraphRun(run, actor) ? (
            <div className="flex flex-wrap gap-2">
              <Button
                disabled={pending}
                onClick={() => decide(gate, "approved")}
              >
                {t(($) => $.run.approve)}
              </Button>
              <Button
                variant="outline"
                disabled={pending || !reason.trim()}
                onClick={() => decide(gate, "rejected")}
              >
                {t(($) => $.run.reject)}
              </Button>
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
      ))}
      <details className="rounded-xl border p-4">
        <summary className="cursor-pointer text-body font-medium">
          {t(($) => $.production.events)}
        </summary>
        <ol className="mt-4 space-y-3">
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
      </details>
      <JsonDetails
        label={t(($) => $.production.raw_details)}
        value={{
          id: run.id,
          revision: run.revision,
          input_hash: run.inputHash,
          version_id: run.versionId,
        }}
      />
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
