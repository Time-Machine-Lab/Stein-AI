import { describe, expect, it } from "vitest";
import {
  schemaDefaults,
  validateGraphInput,
  schemaFields,
} from "./input-schema";
describe("Graph schema input forms", () => {
  const schema = {
    type: "object",
    required: ["name", "count"],
    properties: {
      name: { type: "string", minLength: 2 },
      count: { type: "integer", minimum: 1, default: 2 },
      enabled: { type: "boolean", default: false },
      nested: {
        type: "object",
        properties: {
          mode: { type: "string", enum: ["one", "two"], default: "one" },
        },
      },
    },
  };
  it("uses explicit defaults including false and nested objects without inventing required input", () => {
    expect(schemaDefaults(schema)).toEqual({
      count: 2,
      enabled: false,
      nested: { mode: "one" },
    });
  });
  it("reports paths for missing or mistyped values, bounds, enums and array members", () => {
    expect(
      validateGraphInput(schema, {
        name: "a",
        count: 0,
        enabled: "false",
        nested: { mode: "three" },
      }).map((item) => item.path),
    ).toEqual(["name", "count", "enabled", "nested.mode"]);
    expect(
      validateGraphInput(schema, {
        name: "okay",
        count: 2,
        enabled: false,
        nested: { mode: "one" },
      }),
    ).toEqual([]);
  });
  it("offers escaped JSON pointer paths from nested output schemas", () => {
    expect(
      schemaFields({
        type: "object",
        properties: {
          "a/b": { type: "object", properties: { "~c": { type: "number" } } },
        },
      }).map((field) => field.path),
    ).toEqual(["", "/a~1b", "/a~1b/~0c"]);
  });
});
