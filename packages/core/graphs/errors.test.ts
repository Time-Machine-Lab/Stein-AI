import { describe, expect, it } from "vitest";
import { ApiError } from "../api/client";
import { graphFailureContext } from "./errors";

describe("Graph error context", () => {
  it("preserves node and agent context from the server", () => {
    const body = {
      code: "runtime_mismatch",
      node_path: "loop/body/check",
      node_label: "Review",
      agent_name: "Reviewer",
    };
    expect(
      graphFailureContext(new ApiError("changed", 409, "Conflict", body)),
    ).toEqual(body);
  });
  it("handles older and malformed error responses without breaking the UI", () => {
    expect(graphFailureContext(new Error("offline"))).toBeUndefined();
    expect(
      graphFailureContext(
        new ApiError("changed", 409, "Conflict", { code: "runtime_mismatch" }),
      ),
    ).toEqual({ code: "runtime_mismatch" });
    expect(
      graphFailureContext(
        new ApiError("changed", 409, "Conflict", {
          code: "runtime_mismatch",
          node_label: [],
        }),
      ),
    ).toEqual({ code: "runtime_mismatch" });
    expect(
      graphFailureContext(new ApiError("bad", 500, "Error", { code: [] })),
    ).toBeUndefined();
  });
});
