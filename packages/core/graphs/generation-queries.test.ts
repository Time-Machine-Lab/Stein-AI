import { describe, expect, it } from "vitest";
import { graphGenerationActive, graphGenerationMode } from "./controls";
import { graphGenerationJob, graphGenerationOptions, graphKeys } from "./queries";

/**
 * Calls the interval function the way React Query does, with the data it has
 * already put in the cache.
 */
function pollInterval(options: { refetchInterval?: unknown }, status?: string) {
  const interval = options.refetchInterval;
  if (typeof interval !== "function") return interval;
  return (interval as (query: unknown) => unknown)({
    state: { data: status === undefined ? undefined : { status } },
  });
}

describe("graphGenerationJob", () => {
  it("keys the job under the workspace prefix so the graph realtime handler refreshes it", () => {
    const key = graphGenerationJob("ws-1", "gen-1").queryKey;
    expect(key.slice(0, graphKeys.all("ws-1").length)).toEqual(
      graphKeys.all("ws-1"),
    );
    expect(key).not.toEqual(graphGenerationOptions("ws-1").queryKey);
    expect(graphGenerationJob("ws-1", "gen-1").queryKey).not.toEqual(
      graphGenerationJob("ws-1", "gen-2").queryKey,
    );
  });

  it.each([
    ["queued", 5_000],
    ["running", 5_000],
    ["succeeded", false],
    ["failed", false],
    ["cancelled", false],
    [undefined, false],
  ])("polls every %s only while the task is in flight", (status, expected) => {
    expect(
      pollInterval(graphGenerationJob("ws-1", "gen-1"), status as string),
    ).toBe(expected);
  });

  it("stays idle without a workspace or a generation", () => {
    expect(graphGenerationJob("", "gen-1").enabled).toBe(false);
    expect(graphGenerationJob("ws-1", "").enabled).toBe(false);
    expect(graphGenerationJob("ws-1", "gen-1").enabled).toBe(true);
  });
});

describe("graphGenerationActive", () => {
  it.each([
    ["queued", true],
    ["running", true],
    ["succeeded", false],
    ["failed", false],
    ["cancelled", false],
    [undefined, false],
    // A status this build does not recognize must end the poll, not extend it.
    ["paused", false],
  ])("treats %s as active=%s", (status, expected) => {
    expect(graphGenerationActive(status)).toBe(expected);
  });
});

describe("graphGenerationMode", () => {
  it.each([
    [{ enabled: true, runtimeEnabled: true }, "runtime"],
    [{ enabled: false, runtimeEnabled: true }, "runtime"],
    [{ enabled: true, runtimeEnabled: false }, "legacy"],
    [{ enabled: true }, "legacy"],
    [{ enabled: false, runtimeEnabled: false }, "unavailable"],
    [{ enabled: false }, "unavailable"],
    [undefined, "unavailable"],
  ])("resolves %j to %s", (options, expected) => {
    expect(graphGenerationMode(options)).toBe(expected);
  });

  it("does not fall back to the server model on an unknown runtime flag", () => {
    // A server that predates the runtime path sends no `runtime_enabled` at
    // all; the legacy path is the only one that can serve that request.
    expect(graphGenerationMode({ enabled: true })).toBe("legacy");
  });
});
