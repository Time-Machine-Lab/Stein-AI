import type { JsonObject, GraphRepositoryOption } from "./types";

/** Only allow browser links to HTTP(S) resources, never executable/protocol URLs. */
export function graphArtifactLink(
  artifact: JsonObject,
  repositories: GraphRepositoryOption[],
): string | undefined {
  const explicit = typeof artifact.url === "string" ? artifact.url : undefined;
  const repository = repositories.find(
    (item) => item.id === artifact.repository_id,
  );
  const base =
    repository?.url ??
    (typeof artifact.repository_id === "string" ? artifact.repository_id : "");
  try {
    const url = new URL(explicit ?? base);
    if (
      !["http:", "https:"].includes(url.protocol) ||
      url.username ||
      url.password
    )
      return undefined;
    if (explicit) return url.toString();
    if (
      typeof artifact.commit_sha !== "string" ||
      !/^(?:[0-9a-f]{40}|[0-9a-f]{64})$/i.test(artifact.commit_sha)
    )
      return undefined;
    return `${url.origin}${url.pathname.replace(/\.git$/, "").replace(/\/$/, "")}${repository?.provider === "gitlab" ? "/-/commit/" : "/commit/"}${artifact.commit_sha}`;
  } catch {
    return undefined;
  }
}
