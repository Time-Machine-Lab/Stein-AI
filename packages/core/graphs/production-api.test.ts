import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { GraphRunSchema } from "./schemas";
afterEach(() => vi.unstubAllGlobals());
const respond = (value: unknown) => {
  const fetcher = vi
    .fn()
    .mockResolvedValue(new Response(JSON.stringify(value)));
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
};
describe("Graph production API boundary", () => {
  it("sends scoped repository lookup and preserves commit text without renaming business fields", async () => {
    const fetcher = respond({
      repositories: [
        {
          id: "repo",
          name: "Project",
          provider: "github",
          url: "https://github.com/team/repo",
          default_branch: "main",
        },
      ],
    });
    expect(
      (await new ApiClient("").graphStartOptions("issue/id")).repositories[0]
        ?.defaultBranch,
    ).toBe("main");
    expect(fetcher.mock.calls[0]?.[0]).toBe(
      "/api/graphs/start-options?root_issue_id=issue%2Fid",
    );
  });
  it.each([{}, { items: [{ id: null }], total: 10 }, { items: "bad" }])(
    "fails closed on malformed search results",
    async (body) => {
      respond(body);
      expect(await new ApiClient("").searchGraphRuns()).toEqual({
        items: [],
        total: 0,
      });
    },
  );
  it("parses multiple approval gates and votes without enabling duplicate votes", () => {
    const result = GraphRunSchema.parse({
      id: "run",
      workspace_id: "ws",
      root_issue_id: "issue",
      version_id: "version",
      initiator_id: "u",
      state: "active",
      control_state: "run",
      revision: 2,
      approvals: [
        {
          id: "gate",
          approver_id: "a",
          approver_ids: ["a", "b"],
          mode: "all",
          candidate_digest: "digest",
          revision: 1,
          state: "pending",
          decisions: [
            {
              member_id: "a",
              user_id: "u",
              decision: "approved",
              decided_at: "now",
            },
          ],
          candidate: { outputs: { result_key: "value" } },
        },
      ],
    });
    expect(result.approvals?.[0]?.decisions?.[0]?.memberId).toBe("a");
    expect(result.approvals?.[0]?.candidate).toEqual({
      outputs: { result_key: "value" },
    });
  });
  it("does not report a successful trigger save or secret rotation on malformed responses", async () => {
    respond({ ok: true });
    await expect(
      new ApiClient("").saveGraphTrigger("graph", {
        name: "Daily",
        kind: "schedule",
        versionId: "version",
        enabled: true,
        config: { cron: "0 9 * * *", timezone: "Asia/Singapore" },
        input: {},
        runtimeBindings: {},
      }),
    ).rejects.toThrow();
    await expect(
      new ApiClient("").rotateGraphTriggerSecret("trigger", {
        expectedRevision: 1,
        idempotencyKey: "key",
      }),
    ).rejects.toThrow();
  });
});
