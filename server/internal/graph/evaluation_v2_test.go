package graph

import (
	"encoding/json"
	"strings"
	"testing"
)

func literal(value string) *InputBinding {
	return &InputBinding{Source: BindingConstant, Value: json.RawMessage(value)}
}

func TestV2ConditionExactNumbersAndNoCoercion(t *testing.T) {
	ctx := EvaluationContext{RunInput: json.RawMessage(`{}`)}
	for _, tt := range []struct {
		op, left, right string
		want            bool
	}{
		{"eq", "9007199254740993", "9007199254740992", false},
		{"gt", "9007199254740993", "9007199254740992", true},
		{"eq", "1e2", "100.0", true},
		{"eq", `{"a":1,"b":[true]}`, `{"b":[true],"a":1.0}`, true},
		{"lt", `"aa"`, `"ab"`, true},
	} {
		t.Run(tt.op+tt.left, func(t *testing.T) {
			got, err := EvaluateCondition(Condition{Op: tt.op, Left: literal(tt.left), Right: literal(tt.right)}, ctx)
			if err != nil || got != tt.want {
				t.Fatalf("got %v, %v; want %v", got, err, tt.want)
			}
		})
	}
	if _, err := EvaluateCondition(Condition{Op: "gt", Left: literal(`"2"`), Right: literal(`1`)}, ctx); err == nil {
		t.Fatal("string coerced to number")
	}
}

func TestV2ScopedBindingsDistinguishMissingNullAndCorruption(t *testing.T) {
	ctx := EvaluationContext{RunInput: json.RawMessage(`{"present":null}`), Outputs: map[string]json.RawMessage{"broken": json.RawMessage(`{"x":`)}, IterationIndex: 3}
	bindings := map[string]InputBinding{"iteration": {Source: BindingIterationIndex}, "fallback": {Source: BindingNodeOutput, NodeKey: "skipped", Path: "/value", Default: json.RawMessage(`42`)}, "optional": {Source: BindingNodeOutput, NodeKey: "skipped", Optional: true}, "present": {Source: BindingRunInput, Path: "/present", Default: json.RawMessage(`42`)}}
	got, err := ResolveScopedInputs(bindings, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"fallback":42,"iteration":3,"optional":null,"present":null}` {
		t.Fatalf("wrong scoped input: %s", got)
	}
	for _, path := range []string{"/present", "/missing"} {
		exists, err := EvaluateCondition(Condition{Op: "exists", Left: &InputBinding{Source: BindingRunInput, Path: path}}, ctx)
		if err != nil || exists != (path == "/present") {
			t.Fatalf("exists %s: %v %v", path, exists, err)
		}
	}
	if _, err := ResolveBinding(InputBinding{Source: BindingNodeOutput, NodeKey: "broken", Optional: true}, ctx); err == nil {
		t.Fatal("corrupt JSON treated as optional missing value")
	}
	if _, err := ResolveBinding(InputBinding{Source: BindingRunInput, Path: "/bad~escape", Optional: true}, ctx); err == nil {
		t.Fatal("malformed pointer treated as missing")
	}
	if _, err := ResolveBinding(InputBinding{Source: BindingIterationIndex}, EvaluationContext{RunInput: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("iteration index leaked outside loop")
	}
}

func TestV2ConditionBoundsAndShortCircuit(t *testing.T) {
	ctx := EvaluationContext{RunInput: json.RawMessage(`{}`)}
	falseCondition := Condition{Op: "eq", Left: literal(`1`), Right: literal(`2`)}
	missing := Condition{Op: "eq", Left: &InputBinding{Source: BindingNodeOutput, NodeKey: "missing"}, Right: literal(`1`)}
	got, err := EvaluateCondition(Condition{Op: "all", Conditions: []Condition{falseCondition, missing}}, ctx)
	if err != nil || got {
		t.Fatalf("short circuit failed: %v %v", got, err)
	}
	c := falseCondition
	for range MaxDepth + 1 {
		c = Condition{Op: "not", Conditions: []Condition{c}}
	}
	if _, err := EvaluateCondition(c, ctx); err == nil {
		t.Fatal("unbounded condition depth accepted")
	}
	if _, err := EvaluateCondition(Condition{Op: "eval", Left: literal(`"process.exit()"`)}, ctx); err == nil {
		t.Fatal("untrusted expression operator accepted")
	}
	if _, err := ResolveBinding(InputBinding{Source: BindingConstant, Value: json.RawMessage(`"` + strings.Repeat("x", MaxJSONBytes) + `"`)}, ctx); err == nil {
		t.Fatal("unbounded constant accepted")
	}
}

func TestV2ParallelReadinessWaitsForEverySelectedPath(t *testing.T) {
	d, err := ParseDefinition([]byte(parallelDefinitionJSON))
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]NodeState{"start": NodeSucceeded, "fork": NodeSucceeded}
	p, err := PlanReady(d, states, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(p.Ready, ",") != "left,right" {
		t.Fatalf("parallel work not ready: %+v", p)
	}
	states["left"] = NodeSucceeded
	states["right"] = NodeActive
	p, err = PlanReady(d, states, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Ready) != 0 {
		t.Fatalf("join ran before active sibling: %+v", p)
	}
	states["right"] = NodeSucceeded
	p, err = PlanReady(d, states, nil)
	if err != nil || strings.Join(p.Ready, ",") != "join" {
		t.Fatalf("join never ready: %+v %v", p, err)
	}
	states["right"] = NodeFailed
	p, err = PlanReady(d, states, nil)
	if err != nil || p.Blocked["join"] == "" {
		t.Fatalf("failed predecessor hidden: %+v %v", p, err)
	}
}

func TestV2BranchReadinessPropagatesSkipsAndUsesPersistedPort(t *testing.T) {
	d, err := ParseDefinition([]byte(strings.Replace(strings.Replace(parallelDefinitionJSON, `"type":"parallel"`, `"type":"branch","condition":{"op":"eq","left":{"source":"constant","value":1},"right":{"source":"constant","value":1}}`, 1), `{"from":"fork","to":"left"},{"from":"fork","to":"right"}`, `{"from":"fork","to":"left","port":"true"},{"from":"fork","to":"right","port":"false"}`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]NodeState{"start": NodeSucceeded, "fork": NodeSucceeded, "left": NodeSucceeded}
	p, err := PlanReady(d, states, map[string]string{"fork": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(p.Ready, ",") != "join" || strings.Join(p.Skipped, ",") != "right" {
		t.Fatalf("branch readiness %+v", p)
	}
	p, err = PlanReady(d, states, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Ready) != 0 || len(p.Blocked) == 0 {
		t.Fatalf("missing durable decision silently re-evaluated: %+v", p)
	}
	if _, err := PlanReady(d, states, map[string]string{"fork": "unknown"}); err == nil {
		t.Fatal("unknown selected port accepted")
	}
}
