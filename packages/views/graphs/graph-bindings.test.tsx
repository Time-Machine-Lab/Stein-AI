import { useState } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { GraphInputBinding } from "@multica/core/graphs";
import labels from "../locales/en/graphs.json";
import { GraphBindingField } from "./graph-bindings";
vi.mock("../i18n", () => ({
  useT: () => ({
    t: (selector: (value: typeof labels) => string) => selector(labels),
  }),
}));
function Binding() {
  const [value, setValue] = useState<GraphInputBinding>({
    source: "constant",
    value: { retries: 2, enabled: true, tags: ["review"] },
  });
  return (
    <>
      <GraphBindingField
        definition={{
          schemaVersion: 2,
          name: "Test",
          inputSchema: {},
          nodes: [],
          edges: [],
        }}
        value={value}
        onChange={setValue}
      />
      <output>{JSON.stringify(value.value)}</output>
    </>
  );
}
describe("visual Graph constants", () => {
  it("edits nested constant values while preserving numbers, booleans, and arrays", () => {
    render(<Binding />);
    fireEvent.change(screen.getByRole("spinbutton"), {
      target: { value: "3" },
    });
    expect(JSON.parse(screen.getByRole("status").textContent!)).toEqual({
      retries: 3,
      enabled: true,
      tags: ["review"],
    });
  });
});
