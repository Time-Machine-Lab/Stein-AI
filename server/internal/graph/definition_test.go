package graph

import (
	"encoding/json"
	"strings"
	"testing"
)

func testDefinition() Definition {
	return Definition{SchemaVersion: 1, Name: "Implement and review", InputSchema: json.RawMessage(`{"type":"object","properties":{"goal":{"type":"string"}},"required":["goal"],"additionalProperties":false}`), Nodes: []Node{
		{Key: "start", Type: NodeTypeStart},
		{Key: "implement", Type: NodeTypeAgent, AgentID: "agent-1", Instructions: "Implement the goal", Inputs: map[string]InputBinding{"goal": {Source: BindingRunInput, Path: "/goal"}}, OutputSchema: json.RawMessage(`{"type":"object","properties":{"summary":{"type":"string"}},"required":["summary"],"additionalProperties":false}`), ExecutionMode: ExecutionNone, MaxAttempts: 3, TimeoutSeconds: 900},
		{Key: "approve", Type: NodeTypeApproval, ApproverID: "member-1"},
		{Key: "end", Type: NodeTypeEnd},
	}, Edges: []Edge{{From: "start", To: "implement"}, {From: "implement", To: "approve"}, {From: "approve", To: "end"}}}
}

func TestDefinitionRejectsUnsupportedTopologyAndConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Definition)
	}{
		{"cycle", func(d *Definition) { d.Edges[2].To = "start" }},
		{"branch", func(d *Definition) { d.Edges = append(d.Edges, Edge{From: "start", To: "approve"}) }},
		{"duplicate_key", func(d *Definition) { d.Nodes[2].Key = "implement" }},
		{"missing_agent", func(d *Definition) { d.Nodes[1].AgentID = "" }},
		{"invalid_budget", func(d *Definition) { d.Nodes[1].MaxAttempts = 0 }},
		{"negative_budget", func(d *Definition) { d.Nodes[1].MaxAttempts = -1 }},
		{"zero_timeout", func(d *Definition) { d.Nodes[1].TimeoutSeconds = 0 }},
		{"negative_timeout", func(d *Definition) { d.Nodes[1].TimeoutSeconds = -1 }},
		{"unknown_mode", func(d *Definition) { d.Nodes[1].ExecutionMode = "local_directory" }},
		{"approval_not_last", func(d *Definition) {
			d.Edges = []Edge{{From: "start", To: "approve"}, {From: "approve", To: "implement"}, {From: "implement", To: "end"}}
		}},
		{"future_reference", func(d *Definition) {
			d.Nodes[1].Inputs["x"] = InputBinding{Source: BindingNodeOutput, NodeKey: "approve", Path: "/summary"}
		}},
		{"missing_field", func(d *Definition) {
			d.Nodes[1].Inputs["goal"] = InputBinding{Source: BindingRunInput, Path: "/missing"}
		}},
		{"remote_schema", func(d *Definition) {
			d.InputSchema = json.RawMessage(`{"type":"object","$ref":"https://example.test/schema"}`)
		}},
		{"unknown_binding", func(d *Definition) {
			d.Nodes[1].Inputs["goal"] = InputBinding{Source: "javascript", Value: json.RawMessage(`"eval()"`)}
		}},
	}
	if err := ValidateDefinition(testDefinition()); err != nil {
		t.Fatalf("valid definition: %v", err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := testDefinition()
			tt.mutate(&d)
			if err := ValidateDefinition(d); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestAgentExecutionSettingsHaveNoFixedUpperLimit(t *testing.T) {
	for _, version := range []int{SchemaVersion, LatestSchemaVersion} {
		for _, settings := range []struct{ attempts, timeout int }{{16, 21600}, {100, 172800}} {
			d := testDefinition()
			d.SchemaVersion = version
			d.Nodes[1].MaxAttempts = settings.attempts
			d.Nodes[1].TimeoutSeconds = settings.timeout
			raw, err := json.Marshal(d)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := ParseDefinition(raw)
			if err != nil {
				t.Fatalf("V%d rejected attempts=%d, timeout=%d: %v", version, settings.attempts, settings.timeout, err)
			}
			if parsed.Nodes[1].MaxAttempts != settings.attempts || parsed.Nodes[1].TimeoutSeconds != settings.timeout {
				t.Fatal("execution settings changed during parsing")
			}
		}
	}
}

func TestCanonicalDefinitionIgnoresObjectAndNodeOrdering(t *testing.T) {
	d := testDefinition()
	_, a, err := CanonicalDefinition(d)
	if err != nil {
		t.Fatal(err)
	}
	d.Nodes[0], d.Nodes[3] = d.Nodes[3], d.Nodes[0]
	d.Edges[0], d.Edges[2] = d.Edges[2], d.Edges[0]
	d.InputSchema = json.RawMessage(`{ "required":["goal"], "properties":{"goal":{"type":"string"}},"additionalProperties":false,"type":"object"}`)
	_, b, err := CanonicalDefinition(d)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("equivalent definitions have different hashes: %s %s", a, b)
	}
	d.Nodes[1].Instructions = "Different work"
	_, c, err := CanonicalDefinition(d)
	if err != nil {
		t.Fatal(err)
	}
	if b == c {
		t.Fatal("instruction change must change hash")
	}
}

func TestParseDefinitionRejectsUnknownAndDuplicateFields(t *testing.T) {
	raw, err := json.Marshal(testDefinition())
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{strings.Replace(string(raw), `"schema_version":1`, `"schema_version":1,"unexpected":true`, 1), strings.Replace(string(raw), `"schema_version":1`, `"schema_version":1,"schema_version":1`, 1), string(raw) + `{}`} {
		if _, err := ParseDefinition([]byte(v)); err == nil {
			t.Fatalf("accepted malformed definition: %s", v)
		}
	}
}

func TestResolveInputsUsesOnlyExplicitReferencesAndConstants(t *testing.T) {
	n := testDefinition().Nodes[1]
	n.Inputs = map[string]InputBinding{"goal": {Source: BindingRunInput, Path: "/goal"}, "context": {Source: BindingObject, Fields: map[string]InputBinding{"summary": {Source: BindingNodeOutput, NodeKey: "previous", Path: "/items/0/name"}, "literal": {Source: BindingConstant, Value: json.RawMessage(`"${ignored}"`)}}}}
	got, err := ResolveInputs(n, json.RawMessage(`{"goal":"ship","secret":"not selected"}`), map[string]json.RawMessage{"previous": json.RawMessage(`{"items":[{"name":"done"}]}`)})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"context":{"literal":"${ignored}","summary":"done"},"goal":"ship"}` {
		t.Fatalf("unexpected inputs: %s", got)
	}
	if _, err := ResolveInputs(n, json.RawMessage(`{"goal":"ship"}`), nil); err == nil {
		t.Fatal("missing prior output must fail")
	}
}

func TestJSONSchemaRejectsInvalidDataAndUnsupportedSchemas(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"count":{"type":"integer","minimum":1,"maximum":3},"names":{"type":"array","items":{"type":"string","minLength":2},"maxItems":2}},"required":["count"],"additionalProperties":false}`)
	if err := ValidateJSON(schema, json.RawMessage(`{"count":2,"names":["ok"]}`)); err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{`{"count":0}`, `{"count":1.5}`, `{"count":2,"extra":true}`, `{"count":2,"names":["x"]}`, `{}`, `{"count":2,"count":2}`} {
		if err := ValidateJSON(schema, json.RawMessage(data)); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
	for _, schema := range []string{`{"type":"object","$ref":"file:///etc/passwd"}`, `{"type":"object","required":["missing"]}`, `{"type":"array"}`, `{"type":"string","maxLength":-1}`} {
		if err := ValidateJSON(json.RawMessage(schema), json.RawMessage(`{}`)); err == nil {
			t.Fatalf("accepted unsupported schema %s", schema)
		}
	}
}
