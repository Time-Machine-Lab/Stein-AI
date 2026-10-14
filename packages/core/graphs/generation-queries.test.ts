import { describe, expect, it } from "vitest";
import { graphGenerationMode } from "./controls";
import { graphGenerationOptions, graphKeys } from "./queries";
import type { GraphGenerationOptions } from "./types";

/**
 * A server older than the chat-based builder never sends `builder_enabled`, so
 * the mode must resolve off `enabled` alone. The client type requires the field
 * because the parser always fills it, hence the assertion here.
 */
function withoutBuilder(sent: Pick<GraphGenerationOptions, "enabled">) {
  return sent as GraphGenerationOptions;
}

describe("graphGenerationMode", () => {
  it.each([
    [{ enabled: true, builderEnabled: true }, "builder"],
    [{ enabled: false, builderEnabled: true }, "builder"],
    [{ enabled: true, builderEnabled: false }, "legacy"],
    [{ enabled: false, builderEnabled: false }, "unavailable"],
    [undefined, "unavailable"],
  ])("resolves %j to %s", (options, expected) => {
    expect(graphGenerationMode(options)).toBe(expected);
  });

  it("lands an old server that predates the builder on the legacy path", () => {
    expect(graphGenerationMode(withoutBuilder({ enabled: true }))).toBe(
      "legacy",
    );
  });

  it("stays unavailable when a server advertises neither path", () => {
    // Both signals absent means neither entry point may be offered; calling the
    // legacy endpoint is the only alternative and it is not configured.
    expect(graphGenerationMode(withoutBuilder({ enabled: false }))).toBe(
      "unavailable",
    );
    expect(graphGenerationMode(undefined)).toBe("unavailable");
  });
});

describe("graphGenerationOptions", () => {
  it("keys the options under the workspace prefix and stays idle without one", () => {
    const key = graphGenerationOptions("ws-1").queryKey;
    expect(key.slice(0, graphKeys.all("ws-1").length)).toEqual(
      graphKeys.all("ws-1"),
    );
    expect(graphGenerationOptions("a").queryKey).not.toEqual(
      graphGenerationOptions("b").queryKey,
    );
    expect(graphGenerationOptions("").enabled).toBe(false);
    expect(graphGenerationOptions("ws-1").enabled).toBe(true);
  });
});
