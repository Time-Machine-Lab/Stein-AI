import { describe, expect, it, vi } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import { graphDetailOptions, graphRunOptions, graphSearchOptions, graphCommitsOptions, graphIssueSearchOptions, graphKeys } from "./graphs";
import { api } from "@/data/api";
vi.mock("@/data/api", () => ({ api: { getGraph: vi.fn().mockResolvedValue(null), getGraphRun: vi.fn().mockResolvedValue(null), searchGraphs: vi.fn().mockResolvedValue({ items: [], total: 0 }), graphRepositoryCommits: vi.fn().mockResolvedValue({ commits: [] }), listIssues: vi.fn().mockResolvedValue({ issues: [] }) } }));
describe("mobile Graph queries", () => {
  it("isolates workspaces and never seeds detail caches with listing metadata", () => {
    const qc = new QueryClient(); qc.setQueryData(graphKeys.search("a", {}), { items: [{ id: "g" }], total: 1 });
    expect(qc.getQueryData(graphKeys.detail("a", "g"))).toBeUndefined();
    expect(graphKeys.run("a", "r")).not.toEqual(graphKeys.run("b", "r"));
  });
  it("forwards cancellation to Graph reads including commit and task lookup", async () => {
    const qc = new QueryClient();
    await qc.fetchQuery(graphDetailOptions("w", "g")); await qc.fetchQuery(graphRunOptions("w", "r")); await qc.fetchQuery(graphSearchOptions("w")); await qc.fetchQuery(graphCommitsOptions("w", "repo", { q: "main" })); await qc.fetchQuery(graphIssueSearchOptions("w", "release"));
    expect(api.getGraph).toHaveBeenCalledWith("g", { signal: expect.any(AbortSignal) });
    expect(api.getGraphRun).toHaveBeenCalledWith("r", { signal: expect.any(AbortSignal) });
    expect(api.searchGraphs).toHaveBeenCalledWith({}, { signal: expect.any(AbortSignal) });
    expect(api.graphRepositoryCommits).toHaveBeenCalledWith("repo", { q: "main" }, { signal: expect.any(AbortSignal) });
    expect(api.listIssues).toHaveBeenCalledWith({ q: "release", limit: 30 }, { signal: expect.any(AbortSignal) });
  });
});
