import { describe, expect, it, vi } from "vitest";
import { graphArtifactLink } from "./graph-result-view";
vi.mock("../editor/readonly-content", () => ({ ReadonlyContent: () => null }));
const sha = "a".repeat(40);
describe("Graph artifact links", () => {
  it("builds provider-specific immutable commit links", () => {
    expect(
      graphArtifactLink({ repository_id: "repo", commit_sha: sha }, [
        {
          id: "repo",
          name: "Repo",
          provider: "gitlab",
          url: "https://gitlab.example/team/repo.git",
        },
      ]),
    ).toBe(`https://gitlab.example/team/repo/-/commit/${sha}`);
  });
  it("rejects executable URLs, credentials, and malformed commits", () => {
    for (const url of [
      "javascript:alert(1)",
      "data:text/html,x",
      "https://token:secret@example.com",
    ]) {
      expect(graphArtifactLink({ url }, [])).toBeUndefined();
    }
    expect(
      graphArtifactLink(
        {
          repository_id: "https://github.com/team/repo",
          commit_sha: "a".repeat(41),
        },
        [],
      ),
    ).toBeUndefined();
  });
});
