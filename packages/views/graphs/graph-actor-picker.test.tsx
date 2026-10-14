import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { GraphActorMultiPicker } from "./graph-actor-picker";

const options = [
  { value: "agent-1", label: "Builder" },
  { value: "agent-2", label: "Reviewer" },
];

/** Controlled, the way the editor uses it: the parent owns the selection. */
function picker(initial: string[], onChange = vi.fn()) {
  function Harness() {
    const [values, setValues] = useState(initial);
    return (
      <GraphActorMultiPicker
        id="scope"
        label="Participating agents"
        values={values}
        onChange={(next) => {
          onChange(next);
          setValues(next);
        }}
        options={options}
        placeholder="Choose agents"
        isAgent
      />
    );
  }
  render(<Harness />);
  return onChange;
}

const trigger = () =>
  screen.getByRole("combobox", { name: "Participating agents" });
const row = (name: string) => screen.getByRole("button", { name });

describe("GraphActorMultiPicker", () => {
  it("adds each selected agent without closing the list", () => {
    const onChange = picker([]);
    fireEvent.click(trigger());
    fireEvent.click(row("Builder"));
    expect(onChange).toHaveBeenLastCalledWith(["agent-1"]);
    // The second pick has to be reachable in the same visit.
    fireEvent.click(row("Reviewer"));
    expect(onChange).toHaveBeenLastCalledWith(["agent-1", "agent-2"]);
  });

  it("removes an agent that is already in the scope", () => {
    const onChange = picker(["agent-1", "agent-2"]);
    fireEvent.click(trigger());
    fireEvent.click(row("Builder"));
    expect(onChange).toHaveBeenLastCalledWith(["agent-2"]);
  });

  it("shows every selected agent as a chip in the caller's order", () => {
    picker(["agent-2", "agent-1"]);
    expect(trigger()).toHaveTextContent("ReviewerBuilder");
  });

  it("ignores a selected agent that is no longer offered", () => {
    picker(["agent-1", "agent-9"]);
    expect(trigger()).toHaveTextContent("Builder");
    expect(trigger()).not.toHaveTextContent("agent-9");
  });

  it("falls back to the placeholder while nothing is selected", () => {
    picker([]);
    expect(trigger()).toHaveTextContent("Choose agents");
    fireEvent.click(trigger());
    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "nobody" },
    });
    expect(screen.queryByRole("button", { name: "Builder" })).toBeNull();
  });
});
