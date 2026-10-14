import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";
import { GRAPH_TERMINAL_STATES } from "@multica/core/graphs/controls";
import type { GraphSearchParams } from "@multica/core/graphs/types";

export const graphKeys = {
  all: (wsId: string) => ["graphs", wsId] as const,
  list: (wsId: string) => [...graphKeys.all(wsId), "list"] as const,
  generation: (wsId: string) => [...graphKeys.all(wsId), "generation-options"] as const,
  detail: (wsId: string, id: string) => [...graphKeys.all(wsId), "detail", id] as const,
  runs: (wsId: string) => [...graphKeys.all(wsId), "runs"] as const,
  run: (wsId: string, id: string) => [...graphKeys.runs(wsId), id] as const,
  tasks: (wsId: string, issueId: string) => [...graphKeys.all(wsId), "tasks", issueId] as const,
  search: (wsId: string, params: GraphSearchParams) => [...graphKeys.all(wsId), "search", params] as const,
  runSearch: (wsId: string, params: GraphSearchParams) => [...graphKeys.all(wsId), "run-search", params] as const,
  start: (wsId: string) => [...graphKeys.all(wsId), "start-options"] as const,
  commits: (wsId: string, repositoryId: string, params: { q?: string; ref?: string; cursor?: string }) => [...graphKeys.all(wsId), "commits", repositoryId, params] as const,
  issueSearch: (wsId: string, q: string) => [...graphKeys.all(wsId), "issue-search", q] as const,
  triggers: (wsId: string, graphId: string) => [...graphKeys.all(wsId), "triggers", graphId] as const,
};

export function graphSearchOptions(wsId: string, params: GraphSearchParams = {}) { return queryOptions({ queryKey: graphKeys.search(wsId, params), queryFn: ({ signal }) => api.searchGraphs(params, { signal }), enabled: Boolean(wsId) }); }
export function graphRunSearchOptions(wsId: string, params: GraphSearchParams = {}) { return queryOptions({ queryKey: graphKeys.runSearch(wsId, params), queryFn: ({ signal }) => api.searchGraphRuns(params, { signal }), enabled: Boolean(wsId), refetchInterval: 30_000 }); }
export function graphStartOptions(wsId: string, rootIssueId?: string) { return queryOptions({ queryKey: [...graphKeys.start(wsId), rootIssueId ?? ""], queryFn: ({ signal }) => api.graphStartOptions(rootIssueId, { signal }), enabled: Boolean(wsId) }); }
export function graphCommitsOptions(wsId: string, repositoryId: string, params: { q?: string; ref?: string; cursor?: string } = {}) { return queryOptions({ queryKey: graphKeys.commits(wsId, repositoryId, params), queryFn: ({ signal }) => api.graphRepositoryCommits(repositoryId, params, { signal }), enabled: Boolean(wsId && repositoryId) }); }
export function graphIssueSearchOptions(wsId: string, q: string) { return queryOptions({ queryKey: graphKeys.issueSearch(wsId, q), queryFn: ({ signal }) => api.listIssues({ q, limit: 30 }, { signal }), enabled: Boolean(wsId), staleTime: 30_000 }); }
export function graphTriggersOptions(wsId: string, graphId: string) { return queryOptions({ queryKey: graphKeys.triggers(wsId, graphId), queryFn: ({ signal }) => api.listGraphTriggers(graphId, { signal }), enabled: Boolean(wsId && graphId) }); }
export function graphTriggerEventsOptions(wsId: string, triggerId: string) { return queryOptions({ queryKey: [...graphKeys.all(wsId), "trigger-events", triggerId], queryFn: ({ signal }) => api.listGraphTriggerEvents(triggerId, { signal }), enabled: Boolean(wsId && triggerId), refetchInterval: 30_000 }); }

export function graphGenerationOptions(wsId: string) {
  return queryOptions({ queryKey: graphKeys.generation(wsId), queryFn: ({ signal }) => api.getGraphGenerationOptions({ signal }), enabled: Boolean(wsId), retry: false });
}

export function graphListOptions(wsId: string) {
  return queryOptions({ queryKey: graphKeys.list(wsId), queryFn: ({ signal }) => api.listGraphs({ signal }), enabled: Boolean(wsId) });
}

export function graphDetailOptions(wsId: string, id: string) {
  return queryOptions({ queryKey: graphKeys.detail(wsId, id), queryFn: ({ signal }) => api.getGraph(id, { signal }), enabled: Boolean(wsId && id), retry: false });
}

export function graphRunsOptions(wsId: string) {
  return queryOptions({ queryKey: graphKeys.runs(wsId), queryFn: ({ signal }) => api.listGraphRuns({ signal }), enabled: Boolean(wsId), refetchInterval: 30_000 });
}

export function graphRunOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: graphKeys.run(wsId, id), queryFn: ({ signal }) => api.getGraphRun(id, { signal }), enabled: Boolean(wsId && id), retry: false,
    refetchInterval: (query) => query.state.data && GRAPH_TERMINAL_STATES.has(query.state.data.state) ? false : 15_000,
  });
}

export function graphNodeTasksOptions(wsId: string, issueId: string) {
  return queryOptions({ queryKey: graphKeys.tasks(wsId, issueId), queryFn: ({ signal }) => api.listTasksByIssue(issueId, { signal }), enabled: Boolean(wsId && issueId), refetchInterval: 15_000 });
}
