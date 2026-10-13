import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { graphKeys } from "./queries";
import { issueKeys } from "../issues/queries";
import type {
  GraphApprovalRequest,
  GraphCommand,
  GraphDefinition,
  GraphGenerationRequest,
  GraphRunAction,
  GraphStartRequest,
  GraphTriggerWrite,
} from "./types";

export function useSaveGraphTrigger(wsId: string, graphId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      request,
      triggerId,
    }: {
      request: GraphTriggerWrite;
      triggerId?: string;
    }) => api.saveGraphTrigger(graphId, request, triggerId),
    onSettled: () =>
      qc.invalidateQueries({ queryKey: graphKeys.triggers(wsId, graphId) }),
  });
}
export function useDeleteGraphTrigger(wsId: string, graphId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      triggerId,
      ...command
    }: GraphCommand & { triggerId: string }) =>
      api.deleteGraphTrigger(triggerId, command),
    onSettled: () =>
      qc.invalidateQueries({ queryKey: graphKeys.triggers(wsId, graphId) }),
  });
}
export function useRotateGraphTriggerSecret() {
  return useMutation({
    mutationFn: ({
      triggerId,
      ...command
    }: GraphCommand & { triggerId: string }) =>
      api.rotateGraphTriggerSecret(triggerId, command),
  });
}

export function useGenerateGraph(wsId: string) {
  return useMutation({
    mutationKey: [...graphKeys.all(wsId), "generate"],
    mutationFn: (request: GraphGenerationRequest) => api.generateGraph(request),
    retry: false,
  });
}

export function useSaveGraph(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (request: {
      definition: GraphDefinition;
      id?: string;
      expectedRevision?: number;
      idempotencyKey: string;
    }) =>
      api.saveGraph(
        request.definition,
        request.id
          ? { id: request.id, expectedRevision: request.expectedRevision ?? 0 }
          : undefined,
        request.idempotencyKey,
      ),
    onSuccess: (graph) =>
      qc.setQueryData(graphKeys.detail(wsId, graph.id), graph),
    onSettled: () => qc.invalidateQueries({ queryKey: graphKeys.all(wsId) }),
  });
}

export function useValidateGraph() {
  return useMutation({ mutationFn: (id: string) => api.validateGraph(id) });
}

export function usePublishGraph(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...command }: GraphCommand & { id: string }) =>
      api.publishGraph(id, command),
    onSettled: () => qc.invalidateQueries({ queryKey: graphKeys.all(wsId) }),
  });
}

export function useStartGraphRun(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (request: GraphStartRequest) => api.startGraphRun(request),
    onSuccess: (run) => {
      qc.setQueryData(graphKeys.run(wsId, run.id), run);
      qc.invalidateQueries({
        queryKey: issueKeys.detail(wsId, run.rootIssueId),
      });
    },
    onSettled: () => qc.invalidateQueries({ queryKey: graphKeys.all(wsId) }),
  });
}

export function useControlGraphRun(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      action,
      ...command
    }: GraphCommand & { id: string; action: GraphRunAction }) =>
      api.controlGraphRun(id, action, command),
    onSuccess: (run) => qc.setQueryData(graphKeys.run(wsId, run.id), run),
    onSettled: () => qc.invalidateQueries({ queryKey: graphKeys.all(wsId) }),
  });
}

export function useApproveGraphRun(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...request }: GraphApprovalRequest & { id: string }) =>
      api.approveGraphRun(id, request),
    onSuccess: (run) => {
      qc.setQueryData(graphKeys.run(wsId, run.id), run);
      qc.invalidateQueries({
        queryKey: issueKeys.detail(wsId, run.rootIssueId),
      });
    },
    onSettled: () => qc.invalidateQueries({ queryKey: graphKeys.all(wsId) }),
  });
}

export function useArchiveGraph(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...command }: GraphCommand & { id: string }) =>
      api.archiveGraph(id, command),
    onSuccess: (graph) =>
      qc.setQueryData(graphKeys.detail(wsId, graph.id), graph),
    onSettled: () => qc.invalidateQueries({ queryKey: graphKeys.all(wsId) }),
  });
}

export function useReplaceGraphApprover(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      ...command
    }: GraphCommand & {
      id: string;
      approverId: string;
      previousApproverId?: string;
    }) => api.replaceGraphApprover(id, command),
    onSuccess: (run) => qc.setQueryData(graphKeys.run(wsId, run.id), run),
    onSettled: () => qc.invalidateQueries({ queryKey: graphKeys.all(wsId) }),
  });
}

export function useDetachGraphIssue(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      issueId,
      idempotencyKey,
    }: {
      issueId: string;
      idempotencyKey: string;
    }) => api.detachGraphIssue(issueId, idempotencyKey),
    onSettled: (_data, _error, { issueId }) =>
      Promise.all([
        qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, issueId) }),
        qc.invalidateQueries({ queryKey: graphKeys.all(wsId) }),
      ]),
  });
}
