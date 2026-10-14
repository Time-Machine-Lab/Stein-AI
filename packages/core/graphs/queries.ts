import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import { GRAPH_TERMINAL_STATES, graphGenerationActive } from "./controls";
import type { GraphSearchParams } from "./types";

export const graphKeys = {
  all: (wsId: string) => ["graphs", wsId] as const,
  list: (wsId: string) => [...graphKeys.all(wsId), "list"] as const,
  generation: (wsId: string) =>
    [...graphKeys.all(wsId), "generation-options"] as const,
  generationJob: (wsId: string, id: string) =>
    [...graphKeys.all(wsId), "generation", id] as const,
  detail: (wsId: string, id: string) =>
    [...graphKeys.all(wsId), "detail", id] as const,
  runs: (wsId: string) => [...graphKeys.all(wsId), "runs"] as const,
  run: (wsId: string, id: string) => [...graphKeys.runs(wsId), id] as const,
  tasks: (wsId: string, issueId: string) =>
    [...graphKeys.all(wsId), "tasks", issueId] as const,
  search: (wsId: string, params: GraphSearchParams) =>
    [...graphKeys.all(wsId), "search", params] as const,
  runSearch: (wsId: string, params: GraphSearchParams) =>
    [...graphKeys.all(wsId), "run-search", params] as const,
  start: (wsId: string) => [...graphKeys.all(wsId), "start-options"] as const,
  commits: (
    wsId: string,
    repositoryId: string,
    params: { q?: string; ref?: string; cursor?: string },
  ) => [...graphKeys.all(wsId), "commits", repositoryId, params] as const,
  issueSearch: (wsId: string, q: string) =>
    [...graphKeys.all(wsId), "issue-search", q] as const,
  triggers: (wsId: string, graphId: string) =>
    [...graphKeys.all(wsId), "triggers", graphId] as const,
};

export function graphSearchOptions(
  wsId: string,
  params: GraphSearchParams = {},
) {
  return queryOptions({
    queryKey: graphKeys.search(wsId, params),
    queryFn: () => api.searchGraphs(params),
    enabled: Boolean(wsId),
  });
}
export function graphRunSearchOptions(
  wsId: string,
  params: GraphSearchParams = {},
) {
  return queryOptions({
    queryKey: graphKeys.runSearch(wsId, params),
    queryFn: () => api.searchGraphRuns(params),
    enabled: Boolean(wsId),
    refetchInterval: 15_000,
  });
}
export function graphStartOptions(wsId: string, rootIssueId?: string) {
  return queryOptions({
    queryKey: [...graphKeys.start(wsId), rootIssueId ?? ""],
    queryFn: () => api.graphStartOptions(rootIssueId),
    enabled: Boolean(wsId),
  });
}
export function graphCommitsOptions(
  wsId: string,
  repositoryId: string,
  params: { q?: string; ref?: string; cursor?: string } = {},
) {
  return queryOptions({
    queryKey: graphKeys.commits(wsId, repositoryId, params),
    queryFn: () => api.graphRepositoryCommits(repositoryId, params),
    enabled: Boolean(wsId && repositoryId),
  });
}
export function graphIssueSearchOptions(wsId: string, q: string) {
  return queryOptions({
    queryKey: graphKeys.issueSearch(wsId, q),
    queryFn: ({ signal }) =>
      api.searchIssues({ q, limit: 30, include_closed: false, signal }),
    // The search endpoint rejects an empty q, so wait for the user to type.
    enabled: Boolean(wsId && q.trim()),
    staleTime: 30_000,
  });
}
export function graphTriggersOptions(wsId: string, graphId: string) {
  return queryOptions({
    queryKey: graphKeys.triggers(wsId, graphId),
    queryFn: () => api.listGraphTriggers(graphId),
    enabled: Boolean(wsId && graphId),
  });
}
export function graphTriggerEventsOptions(wsId: string, triggerId: string) {
  return queryOptions({
    queryKey: [...graphKeys.all(wsId), "trigger-events", triggerId],
    queryFn: () => api.listGraphTriggerEvents(triggerId),
    enabled: Boolean(wsId && triggerId),
    refetchInterval: 15_000,
  });
}

export function graphGenerationOptions(wsId: string) {
  return queryOptions({
    queryKey: graphKeys.generation(wsId),
    queryFn: () => api.getGraphGenerationOptions(),
    enabled: Boolean(wsId),
    retry: false,
  });
}

export function graphGenerationJob(wsId: string, id: string) {
  return queryOptions({
    queryKey: graphKeys.generationJob(wsId, id),
    queryFn: () => api.getGraphGeneration(id),
    enabled: Boolean(wsId && id),
    retry: false,
    // WebSocket events fire on state changes; this poll is the safety net for
    // a missed event and stops as soon as the task reaches a terminal state.
    refetchInterval: (query) =>
      graphGenerationActive(query.state.data?.status) ? 5_000 : false,
  });
}

export function graphListOptions(wsId: string) {
  return queryOptions({
    queryKey: graphKeys.list(wsId),
    queryFn: () => api.listGraphs(),
    enabled: Boolean(wsId),
  });
}

export function graphDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: graphKeys.detail(wsId, id),
    queryFn: () => api.getGraph(id),
    enabled: Boolean(wsId && id),
    retry: false,
  });
}

export function graphRunsOptions(wsId: string) {
  return queryOptions({
    queryKey: graphKeys.runs(wsId),
    queryFn: () => api.listGraphRuns(),
    enabled: Boolean(wsId),
    refetchInterval: 10_000,
  });
}

export function graphRunOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: graphKeys.run(wsId, id),
    queryFn: () => api.getGraphRun(id),
    enabled: Boolean(wsId && id),
    retry: false,
    refetchInterval: (query) =>
      query.state.data && GRAPH_TERMINAL_STATES.has(query.state.data.state)
        ? false
        : 5_000,
  });
}

export function graphNodeTasksOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: graphKeys.tasks(wsId, issueId),
    queryFn: () => api.listTasksByIssue(issueId),
    enabled: Boolean(wsId && issueId),
    refetchInterval: 5_000,
  });
}
