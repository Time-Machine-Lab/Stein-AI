import { useEffect, useState } from "react";
import { Alert, Linking, View } from "react-native";
import { router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import type {
  GraphRun,
  GraphNodeRun,
  GraphRunAction,
  GraphApproval,
  GraphRepositoryOption,
  JsonObject,
} from "@multica/core/graphs/types";
import { asSchema } from "@multica/core/graphs/input-schema";
import {
  canApproveGraphRun,
  canManageGraphRun,
  canReplaceGraphApprover,
  graphPendingApprovals,
  graphRunActions,
  nodeCanRetry,
  GRAPH_TERMINAL_STATES,
} from "@multica/core/graphs/controls";
import { graphArtifactLink } from "@multica/core/graphs/artifacts";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { api } from "@/data/api";
import {
  graphRunOptions,
  graphStartOptions,
  graphNodeTasksOptions,
} from "@/data/queries/graphs";
import {
  useControlGraphRun,
  useApproveGraphRun,
  useReplaceGraphApprover,
} from "@/data/mutations/graphs";
import {
  graphDraftIdentity,
  useGraphDraftStore,
} from "@/data/stores/graph-draft-store";
import {
  GraphScreen,
  GraphSection,
  GraphDisclosure,
  GraphError,
  GraphValue,
  GraphText,
  GraphPick,
  GraphStatus,
  useGraphContext,
  useGraphCommandKey,
} from "./shared";
import { emptyGraph } from "./editor-screen";
export function GraphRunScreen({ id }: { id: string }) {
  const { wsId } = useGraphContext(),
    query = useQuery(graphRunOptions(wsId, id));
  return (
    <GraphScreen loading={query.isLoading}>
      <GraphError error={query.error} retry={() => void query.refetch()} />
      <Button
        variant="outline"
        disabled={query.isFetching}
        onPress={() => void query.refetch()}
      >
        <Text>Refresh</Text>
      </Button>
      {query.data && <RunDetail run={query.data} />}
    </GraphScreen>
  );
}
function Artifact({
  artifact,
  run,
  repositories,
}: {
  artifact: JsonObject;
  run: GraphRun;
  repositories: GraphRepositoryOption[];
}) {
  const { wsId } = useGraphContext();
  const issueId =
    run.nodes.find((n) => n.attempts.some((a) => a.taskId === artifact.task_id))
      ?.issueId ?? run.rootIssueId;
  const attachments = useQuery({
    queryKey: ["graphs", wsId, "attachments", issueId],
    queryFn: ({ signal }) => api.listAttachments(issueId, { signal }),
    enabled: artifact.type === "attachment_ref",
  });
  const attachment = attachments.data?.find(
    (a) => a.id === artifact.attachment_id || a.id === artifact.id,
  );
  const href =
    graphArtifactLink(artifact, repositories) ??
    (attachment
      ? graphArtifactLink({ url: attachment.download_url }, [])
      : undefined);
  return (
    <View className="gap-2 rounded-lg border border-border p-3">
      <Text className="font-medium">
        {String(
          artifact.name ??
            (artifact.type === "git_commit"
              ? String(artifact.commit_sha ?? "").slice(0, 12)
              : "Attachment"),
        )}
      </Text>
      {href && (
        <Button
          variant="outline"
          onPress={() =>
            void Linking.openURL(href).catch(() =>
              Alert.alert("Unable to open artifact"),
            )
          }
        >
          <Text>Open artifact</Text>
        </Button>
      )}
      <GraphError
        error={attachments.error}
        retry={() => void attachments.refetch()}
      />
      <GraphDisclosure title="Artifact details">
        <GraphValue value={artifact} />
      </GraphDisclosure>
    </View>
  );
}
function NodeDetail({
  node,
  run,
  canManage,
  pending,
  onAction,
}: {
  node: GraphNodeRun;
  run: GraphRun;
  canManage: boolean;
  pending: boolean;
  onAction: (action: GraphRunAction, node: GraphNodeRun) => void;
}) {
  const { wsId, slug } = useGraphContext(),
    tasks = useQuery(graphNodeTasksOptions(wsId, node.issueId ?? ""));
  let definition = run.definition;
  const scope = run.scopes?.find((s) => s.id === node.scopeId);
  const path =
    typeof scope?.definition_path === "string"
      ? scope.definition_path.split("/")
      : [];
  for (let i = 0; definition && i < path.length; i += 2) {
    const parent = definition.nodes.find((n) => n.nodeKey === path[i]);
    definition = parent?.loop?.body ?? parent?.subgraph?.definition;
  }
  const step = definition?.nodes.find((n) => n.nodeKey === node.nodeKey);
  return (
    <GraphSection title={node.label || step?.label || node.nodeKey}>
      <GraphStatus state={node.state} />
      {node.iteration !== undefined && (
        <Text className="text-xs text-muted-foreground">
          Iteration {node.iteration}
        </Text>
      )}
      {node.selectedPort && (
        <Text>
          {node.selectedPort === "true"
            ? "Condition matched"
            : "Condition did not match"}
        </Text>
      )}
      {node.blockReason && (
        <Text className="text-destructive">
          {node.blockReason === "execution_unknown"
            ? "The previous execution outcome is unknown. Verify it before recovery."
            : node.blockReason === "effect_outcome_unknown"
              ? "The remote request may already have completed. Check the remote service before retrying."
              : node.blockReason.replaceAll("_", " ")}
        </Text>
      )}
      {node.issueId && (
        <Button
          variant="outline"
          onPress={() => router.push(`/${slug}/issue/${node.issueId}`)}
        >
          <Text>Open step task</Text>
        </Button>
      )}
      {node.attempts.length > 0 && (
        <>
          <Text>
            {node.attempts.length} / {node.maxAttempts} attempts
          </Text>
          {node.issueId && (
            <Button
              variant="outline"
              onPress={() => router.push(`/${slug}/issue/${node.issueId}/runs`)}
            >
              <Text>Execution logs and attempts</Text>
            </Button>
          )}
          {tasks.data?.map((task) => (
            <GraphStatus key={task.id} state={task.status} />
          ))}
          <GraphError error={tasks.error} retry={() => void tasks.refetch()} />
        </>
      )}
      {node.output && (
        <GraphValue value={node.output} schema={step?.outputSchema} />
      )}
      <GraphDisclosure title="Step input">
        <GraphValue value={node.input} />
      </GraphDisclosure>
      {node.submissions.map((submission, index) => (
        <GraphDisclosure key={index} title={`Submission ${index + 1}`}>
          <GraphValue
            value={asSchema(submission.result).output ?? submission}
            schema={step?.outputSchema}
          />
        </GraphDisclosure>
      ))}
      {canManage && node.state === "blocked" && (
        <>
          {nodeCanRetry(node) && (
            <Button
              variant="outline"
              disabled={pending}
              onPress={() => onAction("retry", node)}
            >
              <Text>
                {node.nodeType === "http"
                  ? "Review and retry request"
                  : "Retry step"}
              </Text>
            </Button>
          )}
          {node.nodeType !== "http" && node.canResolveBlock && (
            <Button
              variant="outline"
              disabled={pending}
              onPress={() => onAction("resolve-block", node)}
            >
              <Text>Resolve block</Text>
            </Button>
          )}
        </>
      )}
    </GraphSection>
  );
}
function RunDetail({ run }: { run: GraphRun }) {
  const { wsId, slug, userId, actor, members } = useGraphContext(),
    control = useControlGraphRun(wsId),
    approval = useApproveGraphRun(wsId),
    replace = useReplaceGraphApprover(wsId),
    repositories = useQuery(graphStartOptions(wsId, run.rootIssueId));
  const [reason, setReason] = useState(""),
    [error, setError] = useState<unknown>(null),
    commandKey = useGraphCommandKey();
  const key = graphDraftIdentity(wsId, userId, `control-${run.id}`),
    doc = useGraphDraftStore((s) => s.documents[key]);
  useEffect(() => {
    useGraphDraftStore.getState().open(key, emptyGraph(), 0);
  }, [key]);
  const pending = control.isPending || approval.isPending || replace.isPending,
    actions = graphRunActions(run, actor),
    canManage =
      canManageGraphRun(run, actor) &&
      run.controlState === "run" &&
      run.state === "blocked" &&
      !run.archivedAt;
  const execute = async (fn: () => Promise<unknown>) => {
    setError(null);
    try {
      await fn();
      setReason("");
    } catch (e) {
      setError(e);
    }
  };
  const act = (action: GraphRunAction, node?: GraphNodeRun) => {
    const request = {
      id: run.id,
      action,
      expectedRevision: run.revision,
      nodeRunId: node?.id,
      reason: reason.trim() || undefined,
    };
    const submit = () =>
      void execute(() =>
        control.mutateAsync({
          ...request,
          idempotencyKey: commandKey(request),
        }),
      );
    if (node?.nodeType === "http") {
      if (!reason.trim()) {
        setError(
          new Error(
            "Record what you checked in the remote service before retrying this request.",
          ),
        );
        return;
      }
      Alert.alert(
        "Retry this HTTP request?",
        "The remote side effect may already have occurred. Confirm its state first. Retrying reuses the same idempotency key; a service that ignores that key may repeat the operation.",
        [
          { text: "Cancel", style: "cancel" },
          { text: "Retry request", onPress: submit },
        ],
      );
    } else if (action === "cancel")
      Alert.alert(
        "Cancel this run?",
        "Cancellation waits for running work to acknowledge; completed artifacts remain available.",
        [
          { text: "Keep running", style: "cancel" },
          { text: "Cancel run", style: "destructive", onPress: submit },
        ],
      );
    else submit();
  };
  const decide = (gate: GraphApproval, decision: "approved" | "rejected") => {
    const request = {
      id: run.id,
      expectedRevision: run.revision,
      approvalId: gate.id,
      candidateDigest: gate.candidateDigest,
      decision,
      reason: reason.trim() || undefined,
    };
    void execute(() =>
      approval.mutateAsync({ ...request, idempotencyKey: commandKey(request) }),
    );
  };
  const gates = run.approvals?.length
      ? run.approvals
      : run.approval
        ? [run.approval]
        : [],
    mine = new Set(graphPendingApprovals(run, actor.memberId).map((g) => g.id));
  const actionLabels: Record<GraphRunAction, string> = {
    pause: "Pause",
    resume: "Resume",
    cancel: "Cancel run",
    archive: "Archive run",
    retry: "Retry",
    "resolve-block": "Resolve block",
  };
  return (
    <>
      <Text className="text-xl font-semibold">
        {run.graphName ?? "Workflow run"}
      </Text>
      <View className="flex-row flex-wrap gap-2">
        <GraphStatus state={run.state} />
        <GraphStatus state={run.controlState} />
        <Text className="text-xs text-muted-foreground">
          Revision {run.revision}
        </Text>
      </View>
      <GraphError
        error={error ?? control.error ?? approval.error ?? replace.error}
      />
      <Button
        variant="outline"
        onPress={() => router.push(`/${slug}/issue/${run.rootIssueId}`)}
      >
        <Text>Parent task</Text>
      </Button>
      {run.previousRunId && (
        <Button
          variant="outline"
          onPress={() => router.push(`/${slug}/graph/run/${run.previousRunId}`)}
        >
          <Text>Previous run</Text>
        </Button>
      )}
      {actions.map((action) => (
        <Button
          key={action}
          variant={action === "cancel" ? "destructive" : "outline"}
          disabled={pending}
          onPress={() => act(action)}
        >
          <Text>{actionLabels[action]}</Text>
        </Button>
      ))}
      {canManageGraphRun(run, actor) &&
        GRAPH_TERMINAL_STATES.has(run.state) &&
        run.graphId && (
          <Button
            onPress={() =>
              router.push({
                pathname: `/${slug}/graph/${run.graphId}/start`,
                params: { previous_run: run.id, issue: run.rootIssueId },
              })
            }
          >
            <Text>Rework in a new run</Text>
          </Button>
        )}
      {(canManageGraphRun(run, actor) || canApproveGraphRun(run, actor)) && (
        <GraphText
          label="Review or recovery reason"
          value={reason}
          onChange={setReason}
          multiline
          disabled={pending}
        />
      )}
      <GraphDisclosure title="Frozen run input">
        <GraphValue value={run.input} schema={run.definition?.inputSchema} />
      </GraphDisclosure>
      {run.nodes.map((node) => (
        <NodeDetail
          key={node.id}
          node={node}
          run={run}
          canManage={canManage}
          pending={pending}
          onAction={act}
        />
      ))}
      {run.output && (
        <GraphSection title="Workflow result">
          <GraphValue
            value={run.output}
            schema={run.definition?.outputSchema}
          />
        </GraphSection>
      )}
      {run.artifacts.length > 0 && (
        <GraphSection title="Artifacts">
          {run.artifacts.map((artifact, i) => (
            <Artifact
              key={i}
              artifact={artifact}
              run={run}
              repositories={repositories.data?.repositories ?? []}
            />
          ))}
        </GraphSection>
      )}
      {gates.map((gate) => (
        <GraphSection
          key={gate.id}
          title={
            run.nodes.find((n) => n.id === gate.nodeRunId)?.label ||
            "Review candidate"
          }
        >
          <GraphStatus state={gate.state} />
          <Text className="text-sm text-muted-foreground">
            {gate.mode === "all"
              ? "All reviewers must approve."
              : gate.mode === "quorum"
                ? `${gate.quorum} approvals required.`
                : "Any reviewer may approve."}{" "}
            This decision applies to the frozen candidate below.
          </Text>
          {gate.candidate && (
            <GraphValue
              value={gate.candidate.outputs ?? gate.candidate.inputs}
              schema={{
                properties: Object.fromEntries(
                  run.nodes.map((n) => [
                    n.nodeKey,
                    { title: n.label || n.nodeKey },
                  ]),
                ),
              }}
            />
          )}
          {Array.isArray(gate.candidate?.artifacts) &&
            gate.candidate.artifacts.map((artifact, i) => (
              <Artifact
                key={i}
                artifact={asSchema(artifact)}
                run={run}
                repositories={repositories.data?.repositories ?? []}
              />
            ))}
          {(gate.approverIds?.length
            ? gate.approverIds
            : [gate.approverId]
          ).map((memberId) => {
            const vote = gate.decisions?.find((v) => v.memberId === memberId),
              field = `${gate.id}.${memberId}`,
              next = String(doc?.values[field] ?? "");
            return (
              <View className="gap-2" key={memberId}>
                <Text>
                  {members.data?.find((m) => m.id === memberId)?.name ??
                    memberId}
                </Text>
                {vote && (
                  <>
                    <GraphStatus state={vote.decision} />
                    {vote.reason && <Text>{vote.reason}</Text>}
                  </>
                )}
                {doc &&
                  gate.state === "pending" &&
                  canReplaceGraphApprover(run, actor) && (
                    <>
                      <GraphPick
                        documentKey={key}
                        path={["values", field]}
                        label="Replacement reviewer"
                        value={next}
                        options={(members.data ?? [])
                          .filter(
                            (m) =>
                              !(gate.approverIds ?? [gate.approverId]).includes(
                                m.id,
                              ),
                          )
                          .map((m) => ({
                            value: m.id,
                            label: m.name || m.email,
                          }))}
                        disabled={pending}
                      />
                      <Button
                        variant="outline"
                        disabled={pending || !next || !reason.trim()}
                        onPress={() =>
                          Alert.alert(
                            "Replace reviewer?",
                            "This creates a new approval and resets all votes for this gate. Every reviewer must vote again.",
                            [
                              { text: "Cancel", style: "cancel" },
                              {
                                text: "Replace reviewer",
                                onPress: () => {
                                  const request = {
                                    id: run.id,
                                    expectedRevision: run.revision,
                                    nodeRunId: gate.nodeRunId,
                                    previousApproverId: memberId,
                                    approverId: next,
                                    reason: reason.trim(),
                                  };
                                  void execute(() =>
                                    replace.mutateAsync({
                                      ...request,
                                      idempotencyKey: commandKey(request),
                                    }),
                                  );
                                },
                              },
                            ],
                          )
                        }
                      >
                        <Text>Replace reviewer</Text>
                      </Button>
                    </>
                  )}
              </View>
            );
          })}
          {mine.has(gate.id) && canApproveGraphRun(run, actor) && (
            <>
              <Button
                disabled={pending}
                onPress={() => decide(gate, "approved")}
              >
                <Text>Approve candidate</Text>
              </Button>
              <Button
                variant="outline"
                disabled={pending || !reason.trim()}
                onPress={() => decide(gate, "rejected")}
              >
                <Text>Reject candidate</Text>
              </Button>
            </>
          )}
          <GraphDisclosure title="Candidate verification">
            <GraphValue
              value={{
                candidate_digest: gate.candidateDigest,
                revision: gate.revision,
              }}
            />
          </GraphDisclosure>
        </GraphSection>
      ))}
      <GraphDisclosure title="Run events">
        <GraphValue value={run.events} />
      </GraphDisclosure>
    </>
  );
}
