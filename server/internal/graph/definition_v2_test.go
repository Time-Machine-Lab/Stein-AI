package graph

import (
	"encoding/json"
	"strings"
	"testing"
)

const parallelDefinitionJSON = `{"schema_version":2,"name":"Parallel review","input_schema":{"type":"object"},"nodes":[{"node_key":"start","type":"start"},{"node_key":"fork","type":"parallel"},{"node_key":"left","type":"delay","delay":{"seconds":1}},{"node_key":"right","type":"delay","delay":{"seconds":1}},{"node_key":"join","type":"join"},{"node_key":"end","type":"end"}],"edges":[{"from":"start","to":"fork"},{"from":"fork","to":"left"},{"from":"fork","to":"right"},{"from":"left","to":"join"},{"from":"right","to":"join"},{"from":"join","to":"end"}]}`

func TestV2HTTPRejectsReservedTransportAndInvocationHeaders(t *testing.T) {
	for _, header := range []string{"Host", "Connection", "Content-Length", "Transfer-Encoding", "Trailer", "Upgrade", "Proxy-Authorization", "Proxy-Connection", "TE", "Idempotency-Key"} {
		t.Run(header, func(t *testing.T) {
			d, err := ParseDefinition([]byte(parallelDefinitionJSON))
			if err != nil {
				t.Fatal(err)
			}
			for i := range d.Nodes {
				if d.Nodes[i].Key == "left" {
					d.Nodes[i] = Node{Key: "left", Type: NodeTypeHTTP, OutputSchema: json.RawMessage(`{"type":"object"}`), HTTP: &HTTPConfig{Method: "GET", URL: "https://example.org", TimeoutSeconds: 30, MaxResponseBytes: 1024, Headers: map[string]InputBinding{strings.ToLower(header): {Source: BindingConstant, Value: json.RawMessage(`"value"`)}}}}
				}
			}
			if err := ValidateDefinition(d); err == nil {
				t.Fatal("reserved HTTP header was publishable")
			}
		})
	}
}

func TestV2AcceptsParallelTopologyAndPreservesV1(t *testing.T) {
	d, err := ParseDefinition([]byte(parallelDefinitionJSON))
	if err != nil {
		t.Fatalf("valid V2 parallel graph rejected: %v", err)
	}
	if d.SchemaVersion != 2 {
		t.Fatalf("wrong schema version: %d", d.SchemaVersion)
	}
	if err := ValidateDefinition(testDefinition()); err != nil {
		t.Fatalf("V1 changed: %v", err)
	}
}

func TestV2RejectsUnboundedOrAmbiguousTopology(t *testing.T) {
	for name, raw := range map[string]string{
		"cycle":                strings.Replace(parallelDefinitionJSON, `{"from":"join","to":"end"}`, `{"from":"join","to":"fork"}`, 1),
		"unknown port":         strings.Replace(parallelDefinitionJSON, `{"from":"fork","to":"left"}`, `{"from":"fork","to":"left","port":"true"}`, 1),
		"zero delay":           strings.Replace(parallelDefinitionJSON, `"seconds":1`, `"seconds":0`, 1),
		"too much concurrency": strings.Replace(parallelDefinitionJSON, `"schema_version":2`, `"schema_version":2,"max_concurrency":33`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseDefinition([]byte(raw)); err == nil {
				t.Fatal("invalid graph accepted")
			}
		})
	}
}

func TestV1RejectsV2Configuration(t *testing.T) {
	raw, _ := json.Marshal(testDefinition())
	raw = []byte(strings.Replace(string(raw), `"schema_version":1`, `"schema_version":1,"max_concurrency":4`, 1))
	if _, err := ParseDefinition(raw); err == nil {
		t.Fatal("V1 accepted V2 configuration")
	}
}

func emptyChild() Definition {
	return Definition{SchemaVersion: 2, Name: "Child", InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`), OutputSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`), Nodes: []Node{{Key: "start", Type: NodeTypeStart}, {Key: "end", Type: NodeTypeEnd}}, Edges: []Edge{{From: "start", To: "end"}}}
}
func withMiddle(node Node) Definition {
	d := emptyChild()
	d.Nodes = []Node{{Key: "start", Type: NodeTypeStart}, node, {Key: "end", Type: NodeTypeEnd}}
	d.Edges = []Edge{{From: "start", To: node.Key}, {From: node.Key, To: "end"}}
	return d
}
func loopNode(child Definition, iterations int) Node {
	return Node{Key: "repeat", Type: NodeTypeLoop, OutputSchema: child.OutputSchema, Loop: &LoopConfig{Body: child, MaxIterations: iterations, Condition: Condition{Op: "eq", Left: literal(`1`), Right: literal(`2`)}, Next: map[string]InputBinding{}}}
}

func TestV2NestedLoopBudgetAndCanonicalWalkPaths(t *testing.T) {
	d := emptyChild()
	for range MaxGraphDepth {
		d = withMiddle(loopNode(d, 1))
	}
	raw, _ := json.Marshal(d)
	parsed, err := ParseDefinition(raw)
	if err != nil {
		t.Fatalf("valid depth-four loop rejected: %v", err)
	}
	_, hash, err := CanonicalDefinition(parsed)
	if err != nil || hash == "" {
		t.Fatalf("nested canonicalization failed: %v", err)
	}
	paths := []string{}
	if err := WalkNodes(parsed, func(path string, n Node) error {
		if n.Type == NodeTypeStart {
			paths = append(paths, path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 5 || paths[4] != "repeat/body/repeat/body/repeat/body/repeat/body/start" {
		t.Fatalf("wrong immutable definition paths: %v", paths)
	}
	d = withMiddle(loopNode(d, 1))
	if err := ValidateDefinition(d); err == nil {
		t.Fatal("fifth nested scope accepted")
	}
	d = withMiddle(loopNode(withMiddle(loopNode(emptyChild(), 100)), 100))
	if err := ValidateDefinition(d); err == nil {
		t.Fatal("expanded execution budget over 4096 accepted")
	}
}

func TestV2TypedDefaultsAndChildInputs(t *testing.T) {
	d := testDefinition()
	d.SchemaVersion = 2
	d.Nodes[1].Inputs["goal"] = InputBinding{Source: BindingRunInput, Path: "/goal", Default: json.RawMessage(`42`)}
	if err := ValidateDefinition(d); err == nil {
		t.Fatal("numeric default for string source accepted")
	}
	child := emptyChild()
	child.InputSchema = json.RawMessage(`{"type":"object","properties":{"number":{"type":"integer"}},"required":["number"],"additionalProperties":false}`)
	n := loopNode(child, 2)
	n.Inputs = map[string]InputBinding{"number": *literal(`"wrong"`)}
	n.Loop.Next = map[string]InputBinding{"number": *literal(`1`)}
	if err := ValidateDefinition(withMiddle(n)); err == nil {
		t.Fatal("wrong child input type accepted")
	}
}

func TestV2ConditionalReferencesRequireExplicitFallback(t *testing.T) {
	d := testDefinition()
	d.SchemaVersion = 2
	d.Nodes = []Node{{Key: "start", Type: NodeTypeStart}, {Key: "choose", Type: NodeTypeBranch, Condition: &Condition{Op: "eq", Left: literal(`1`), Right: literal(`1`)}}, d.Nodes[1], {Key: "other", Type: NodeTypeDelay, Delay: &DelayConfig{Seconds: 1}}, {Key: "join", Type: NodeTypeJoin}, d.Nodes[2], d.Nodes[3]}
	d.Edges = []Edge{{From: "start", To: "choose"}, {From: "choose", To: "implement", Port: "true"}, {From: "choose", To: "other", Port: "false"}, {From: "implement", To: "join"}, {From: "other", To: "join"}, {From: "join", To: "approve"}, {From: "approve", To: "end"}}
	d.Nodes[5].Inputs = map[string]InputBinding{"result": {Source: BindingNodeOutput, NodeKey: "implement", Path: "/summary"}}
	if err := ValidateDefinition(d); err == nil {
		t.Fatal("unguarded conditional output accepted")
	}
	b := d.Nodes[5].Inputs["result"]
	b.Default = json.RawMessage(`"skipped"`)
	d.Nodes[5].Inputs["result"] = b
	if err := ValidateDefinition(d); err != nil {
		t.Fatalf("explicit fallback rejected: %v", err)
	}
	d.Nodes[1].Type = NodeTypeParallel
	d.Nodes[1].Condition = nil
	d.Edges[1].Port = ""
	d.Edges[2].Port = ""
	b.Default = nil
	d.Nodes[5].Inputs["result"] = b
	if err := ValidateDefinition(d); err != nil {
		t.Fatalf("guaranteed parallel ancestor wrongly rejected: %v", err)
	}
}

func TestV2ConfigurationBounds(t *testing.T) {
	for name, node := range map[string]Node{
		"approval duplicate": {Key: "a", Type: NodeTypeApproval, Approval: &ApprovalConfig{ApproverIDs: []string{"same", "same"}, Mode: "all"}},
		"approval quorum":    {Key: "a", Type: NodeTypeApproval, Approval: &ApprovalConfig{ApproverIDs: []string{"member"}, Mode: "quorum", Quorum: 2}},
		"zero loop":          loopNode(emptyChild(), 0),
		"http credentials":   {Key: "request", Type: NodeTypeHTTP, OutputSchema: json.RawMessage(`{"type":"object"}`), HTTP: &HTTPConfig{Method: "GET", URL: "https://secret:password@example.org", TimeoutSeconds: 1, MaxResponseBytes: 1}},
		"script timeout":     {Key: "script", Type: NodeTypeScript, AgentID: "agent", ExecutionMode: ExecutionNone, MaxAttempts: 1, TimeoutSeconds: 10, OutputSchema: json.RawMessage(`{"type":"object"}`), Script: &ScriptConfig{Language: "javascript", Code: "return {};", TimeoutSeconds: 1}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateDefinition(withMiddle(node)); err == nil {
				t.Fatal("invalid node accepted")
			}
		})
	}
	for _, mode := range []string{"any", "all", "quorum"} {
		n := Node{Key: "approve", Type: NodeTypeApproval, Approval: &ApprovalConfig{ApproverIDs: []string{"one", "two"}, Mode: mode}}
		if mode == "quorum" {
			n.Approval.Quorum = 1
		}
		if err := ValidateDefinition(withMiddle(n)); err != nil {
			t.Fatalf("valid %s approval: %v", mode, err)
		}
	}
}
