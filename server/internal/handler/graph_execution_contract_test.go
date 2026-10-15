package handler

import (
	"strings"
	"testing"
)

// graphBuiltinSkill is the node-execution contract: the one text every Graph
// node agent is given, next to its own frozen instructions. It used to say
// "For failed verification submit outcome=needs_changes", and that sentence is
// what killed a run whose author had built an iterate-until-it-passes loop: the
// reviewing agent obeyed it, the engine mapped needs_changes to RunRejected
// (graph/state.go DecideCompletion), and the loop's condition never got to read
// the verdict it was written around.
//
// The rule the agents have to be told is the consequence, not a preference:
// pass continues the graph, needs_changes ends the whole run. A node that
// judges work reports its verdict in the output so the graph can route on it.
// daemon/graph.go carries the same sentence to the same agent, and
// TestGraphPromptStatesOutcomeConsequences pins it there.
func TestGraphExecutionSkillStatesOutcomeConsequences(t *testing.T) {
	t.Parallel()
	skill := graphBuiltinSkill().Content
	required := []struct{ what, needle string }{
		{"pass continues", "outcome=pass accepts your output and continues the graph"},
		{"needs_changes is terminal", "ends the whole run as rejected at once, never returning to an earlier step"},
		{"a verdict belongs in the output", "Put your verdict in the output and submit outcome=pass"},
		{"when needs_changes is right", "a run that must stop so a person can change"},
		{"diagnostics-only submissions stay allowed", "cannot produce an output the schema accepts"},
		{"end-of-attempt report", "After a successful submit or block, publish one concise end-of-attempt report"},
		{"scoped comment before exit", "multica graph node comment --body-file <path>, then exit"},
		{"readable report", "in the task's language"},
		{"evidence-based summary", "work completed, key findings, checks actually performed, and any remaining issues"},
		{"acceptance is not yet confirmed", "Do not claim Graph acceptance, artifact delivery, or human approval"},
		{"comment failure preserves result", "If posting fails, exit without changing or resubmitting the staged result"},
	}
	for _, req := range required {
		if !strings.Contains(skill, req.needle) {
			t.Errorf("node-execution skill dropped %s: no %q", req.what, req.needle)
		}
	}
	// The retired sentence is the defect itself: restoring it in any wording
	// tells every gate node to terminate the run it was meant to iterate.
	if strings.Contains(skill, "For failed verification submit outcome=needs_changes") {
		t.Error("the skill again tells a failed verification to submit needs_changes")
	}
}
