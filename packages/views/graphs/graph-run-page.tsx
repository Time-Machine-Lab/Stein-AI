"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft } from "lucide-react";
import { toast } from "sonner";
import {
  canApproveGraphRun, canManageGraphRun, canReplaceGraphApprover, graphNodeTasksOptions, graphRunActions, graphRunOptions,
  nodeCanRetry, useApproveGraphRun, useControlGraphRun, useReplaceGraphApprover,
  type GraphNodeRun, type GraphRun, type GraphRunAction,
} from "@multica/core/graphs";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { memberListOptions } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { NativeSelect } from "@multica/ui/components/ui/native-select";
import { AlertDialog, AlertDialogContent, AlertDialogHeader, AlertDialogTitle, AlertDialogDescription, AlertDialogFooter, AlertDialogCancel, AlertDialogAction } from "@multica/ui/components/ui/alert-dialog";
import { AppLink, useNavigation } from "../navigation";
import { PageHeader } from "../layout/page-header";
import { TranscriptButton } from "../common/task-transcript";
import { useT } from "../i18n";
import { Field, GraphError, GraphState, JsonDetails, useGraphActor, useGraphCommandKey } from "./shared";

function NodeTimeline({ wsId, node, canManage, pending, onAction }: {
  wsId: string; node: GraphNodeRun; canManage: boolean; pending: boolean;
  onAction: (action: GraphRunAction, nodeRunId?: string) => void;
}) {
  const { t } = useT("graphs");
  const paths = useWorkspacePaths();
  const tasks = useQuery(graphNodeTasksOptions(wsId, node.issueId ?? ""));
  return <li className="min-w-0 space-y-4 rounded-xl border p-4">
    <div className="flex flex-wrap items-center gap-2"><h3 className="min-w-0 flex-1 break-all font-medium">{node.nodeKey}</h3><GraphState state={node.state} />{node.issueId && <AppLink href={paths.issueDetail(node.issueId)} className="text-caption underline underline-offset-4">{t(($) => $.run.node_issue)}</AppLink>}</div>
    {node.nodeType === "agent" && <p className="text-caption text-muted-foreground">{t(($) => $.run.attempts, { used: node.attempts.length, max: node.maxAttempts })}</p>}
    {node.blockReason && <p role="status" className="break-words text-body text-destructive">{node.blockReason === "execution_unknown" ? t(($) => $.run.execution_unknown) : node.blockReason}</p>}
    {node.inputHash && <div className="text-caption text-muted-foreground"><span>{t(($) => $.run.input_hash)}</span><code className="ml-2 break-all">{node.inputHash}</code></div>}
    {node.nodeType === "agent" && <JsonDetails label={t(($) => $.run.input)} value={node.input} />}
    {node.attempts.map((attempt) => {
      const task = tasks.data?.find((candidate) => candidate.id === attempt.taskId);
      return <div key={attempt.taskId} className="space-y-2 rounded-lg bg-muted/40 p-3">
        <div className="flex flex-wrap items-center gap-2"><span className="text-caption">{t(($) => $.run.task)}</span><code className="min-w-0 flex-1 break-all text-caption">{attempt.taskId}</code><GraphState state={attempt.state} />{task && <TranscriptButton task={task} agentName={node.nodeKey} isLive={task.status === "running" || task.status === "dispatched"} title={t(($) => $.run.view_transcript)} />}</div>
        <JsonDetails label={t(($) => $.run.manifest)} value={attempt.manifest} />
        {attempt.receipt && <JsonDetails label={t(($) => $.run.receipt)} value={attempt.receipt} />}
      </div>;
    })}
    {node.acceptedSubmissionId && <p className="break-all text-caption text-muted-foreground">{t(($) => $.run.accepted)}{": "}{node.acceptedSubmissionId}</p>}
    {node.submissions.length > 0 && <JsonDetails label={t(($) => $.run.results)} value={node.submissions} open />}
    {canManage && node.state === "blocked" && <div className="flex flex-wrap gap-2">
      {nodeCanRetry(node) && <Button variant="outline" disabled={pending} onClick={() => onAction("retry", node.id)}>{t(($) => $.run.retry)}</Button>}
      {node.canResolveBlock === true && <Button variant="outline" disabled={pending} onClick={() => onAction("resolve-block", node.id)}>{t(($) => $.run.resolve_block)}</Button>}
    </div>}
  </li>;
}

function RunDetail({ wsId, run }: { wsId: string; run: GraphRun }) {
  const { t } = useT("graphs");
  const actor = useGraphActor(wsId);
  const paths = useWorkspacePaths();
  const members = useQuery(memberListOptions(wsId));
  const control = useControlGraphRun(wsId);
  const approval = useApproveGraphRun(wsId);
  const replaceApprover = useReplaceGraphApprover(wsId);
  const [nextApproverId, setNextApproverId] = useState("");
  const [reason, setReason] = useState("");
  const [confirmCancel, setConfirmCancel] = useState(false);
  const commandKey = useGraphCommandKey();
  const pending = control.isPending || approval.isPending || replaceApprover.isPending;
  const actions = graphRunActions(run, actor);
  const actionLabels: Record<GraphRunAction, string> = {
    pause: t(($) => $.run.pause), resume: t(($) => $.run.resume), cancel: t(($) => $.run.cancel),
    retry: t(($) => $.run.retry), "resolve-block": t(($) => $.run.resolve_block), archive: t(($) => $.run.archive),
  };
  function act(action: GraphRunAction, nodeRunId?: string) {
    const request = { id: run.id, action, expectedRevision: run.revision, nodeRunId, reason: reason.trim() || undefined };
    control.mutate({ ...request, idempotencyKey: commandKey(request) }, { onSuccess: () => { setReason(""); toast.success(t(($) => $.run.changed)); } });
  }
  function decide(decision: "approved" | "rejected") {
    if (!run.approval) return;
    const request = { id: run.id, expectedRevision: run.revision, approvalId: run.approval.id, candidateDigest: run.approval.candidateDigest, decision, reason: reason.trim() || undefined };
    approval.mutate({ ...request, idempotencyKey: commandKey(request) }, { onSuccess: () => { setReason(""); toast.success(t(($) => $.run.decision_saved)); } });
  }

  return <div className="mx-auto max-w-5xl space-y-6">
    <GraphError error={control.error ?? approval.error ?? replaceApprover.error} />
    <div className="flex flex-wrap items-center gap-3"><AppLink href={paths.issueDetail(run.rootIssueId)} className="text-body underline underline-offset-4">{t(($) => $.run.parent)}</AppLink><GraphState state={run.state} /><GraphState state={run.controlState} /><span className="text-caption text-muted-foreground">{t(($) => $.page.revision, { revision: run.revision })}</span></div>
    <code className="block break-all text-caption text-muted-foreground">{run.id}</code>
    {run.blockReason && <p role="status" className="text-body text-destructive">{run.blockReason}</p>}
    {run.controlState === "cancel_requested" && <p role="status" className="text-body text-muted-foreground">{t(($) => $.run.cancel_pending)}</p>}
    <div className="flex flex-wrap gap-2">{actions.map((action) => <Button key={action} variant={action === "cancel" ? "destructive" : "outline"} disabled={pending} onClick={() => action === "cancel" ? setConfirmCancel(true) : act(action)}>{actionLabels[action]}</Button>)}</div>
    {!actions.length && !canApproveGraphRun(run, actor) && <p className="text-caption text-muted-foreground">{t(($) => $.run.no_controls)}</p>}
    {(canManageGraphRun(run, actor) || canApproveGraphRun(run, actor)) && <Field label={t(($) => $.run.reason)}>{(id) => <Textarea id={id} value={reason} onChange={(event) => setReason(event.target.value)} placeholder={t(($) => $.run.reason_placeholder)} />}</Field>}
    <JsonDetails label={t(($) => $.run.input)} value={run.input} />
    <section className="space-y-3"><h2 className="text-title font-medium">{t(($) => $.run.nodes)}</h2><ol className="space-y-4">{run.nodes.map((node) => <NodeTimeline key={node.id} wsId={wsId} node={node} canManage={canManageGraphRun(run, actor) && run.controlState === "run" && run.state === "blocked"} pending={pending} onAction={act} />)}</ol></section>
    {run.artifacts.length > 0 && <JsonDetails label={t(($) => $.run.artifacts)} value={run.artifacts} open />}
    {canReplaceGraphApprover(run, actor) && <div className="space-y-3 rounded-xl border p-4"><Field label={t(($) => $.run.replace_approver)}>{(id) => <NativeSelect id={id} className="w-full" value={nextApproverId} onChange={(event) => setNextApproverId(event.target.value)}><option value="">{t(($) => $.editor.choose_approver)}</option>{(members.data ?? []).map((member) => <option key={member.id} value={member.id}>{member.name || member.email}</option>)}</NativeSelect>}</Field><Button variant="outline" disabled={pending || !nextApproverId || nextApproverId === run.approval?.approverId || !reason.trim()} onClick={() => {
      const request = { id: run.id, expectedRevision: run.revision, approverId: nextApproverId, reason: reason.trim() };
      replaceApprover.mutate({ ...request, idempotencyKey: commandKey(request) }, { onSuccess: () => { setReason(""); setNextApproverId(""); toast.success(t(($) => $.run.changed)); } });
    }}>{t(($) => $.run.replace_approver)}</Button></div>}
    {run.approval && <section className="space-y-4 rounded-xl border p-4">
      <div className="flex items-center gap-2"><h2 className="flex-1 text-title font-medium">{t(($) => $.run.approval)}</h2><GraphState state={run.approval.state} /></div>
      <p className="text-body text-muted-foreground">{t(($) => $.run.approval_help)}</p>
      <div className="space-y-1"><p className="text-caption text-muted-foreground">{t(($) => $.run.candidate)}</p><code className="block break-all text-caption">{run.approval.candidateDigest}</code></div>
      <p className="text-body">{t(($) => $.run.approver)}{": "}{members.data?.find((member) => member.id === run.approval?.approverId)?.name ?? run.approval.approverId}</p>
      {canApproveGraphRun(run, actor) ? <div className="flex flex-wrap gap-2"><Button disabled={pending} onClick={() => decide("approved")}>{t(($) => $.run.approve)}</Button><Button variant="outline" disabled={pending || !reason.trim()} onClick={() => decide("rejected")}>{t(($) => $.run.reject)}</Button></div> : run.approval.state === "pending" && <p className="text-caption text-muted-foreground">{t(($) => $.run.approval_waiting)}</p>}
    </section>}
    <JsonDetails label={t(($) => $.run.events)} value={run.events} />
    <AlertDialog open={confirmCancel} onOpenChange={setConfirmCancel}><AlertDialogContent><AlertDialogHeader><AlertDialogTitle>{t(($) => $.run.confirm_cancel)}</AlertDialogTitle><AlertDialogDescription>{t(($) => $.run.cancel_help)}</AlertDialogDescription></AlertDialogHeader><AlertDialogFooter><AlertDialogCancel>{t(($) => $.run.dismiss)}</AlertDialogCancel><AlertDialogAction onClick={() => act("cancel")}>{t(($) => $.run.confirm)}</AlertDialogAction></AlertDialogFooter></AlertDialogContent></AlertDialog>
  </div>;
}

export function GraphRunPage() {
  const { t } = useT("graphs");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const id = navigation.pathname.split("/").filter(Boolean).at(-1) ?? "";
  const run = useQuery(graphRunOptions(wsId, id));
  return <div className="flex min-h-0 flex-1 flex-col"><PageHeader><AppLink href={paths.graphs()} aria-label={t(($) => $.page.back)} className="rounded-md p-1 hover:bg-accent"><ArrowLeft className="size-4" /></AppLink><h1 className="min-w-0 flex-1 truncate text-body font-medium">{t(($) => $.run.title)}</h1><Button variant="ghost" size="sm" onClick={() => void run.refetch()}>{t(($) => $.page.refresh)}</Button></PageHeader>
    <div className="flex-1 overflow-auto p-4"><GraphError error={run.error} />{run.isPending ? <p>{t(($) => $.page.loading)}</p> : run.data ? <RunDetail key={`${wsId}:${run.data.id}`} wsId={wsId} run={run.data} /> : <p>{t(($) => $.page.unavailable)}</p>}</div>
  </div>;
}
