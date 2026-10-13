import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import { GRAPH_TERMINAL_STATES } from "./controls";

export const graphKeys = {
  all: (wsId: string) => ["graphs", wsId] as const,
  list: (wsId: string) => [...graphKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) => [...graphKeys.all(wsId), "detail", id] as const,
  runs: (wsId: string) => [...graphKeys.all(wsId), "runs"] as const,
  run: (wsId: string, id: string) => [...graphKeys.runs(wsId), id] as const,
  tasks: (wsId: string, issueId: string) => [...graphKeys.all(wsId), "tasks", issueId] as const,
};

export function graphListOptions(wsId: string) {
  return queryOptions({ queryKey: graphKeys.list(wsId), queryFn: () => api.listGraphs(), enabled: Boolean(wsId) });
}

export function graphDetailOptions(wsId: string, id: string) {
  return queryOptions({ queryKey: graphKeys.detail(wsId, id), queryFn: () => api.getGraph(id), enabled: Boolean(wsId && id), retry: false });
}

export function graphRunsOptions(wsId: string) {
  return queryOptions({ queryKey: graphKeys.runs(wsId), queryFn: () => api.listGraphRuns(), enabled: Boolean(wsId), refetchInterval: 10_000 });
}

export function graphRunOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: graphKeys.run(wsId, id), queryFn: () => api.getGraphRun(id), enabled: Boolean(wsId && id), retry: false,
    refetchInterval: (query) => query.state.data && GRAPH_TERMINAL_STATES.has(query.state.data.state) ? false : 5_000,
  });
}

export function graphNodeTasksOptions(wsId: string, issueId: string) {
  return queryOptions({ queryKey: graphKeys.tasks(wsId, issueId), queryFn: () => api.listTasksByIssue(issueId), enabled: Boolean(wsId && issueId), refetchInterval: 5_000 });
}
