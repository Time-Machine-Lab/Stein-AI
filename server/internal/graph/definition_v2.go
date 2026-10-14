package graph

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"
)

func hasV2Config(n Node) bool {
	return n.Condition != nil || n.Approval != nil || n.Loop != nil || n.Subgraph != nil || n.Delay != nil || n.HTTP != nil || n.Script != nil
}

// TopologicalNodes returns a stable order independent of editor array ordering.
func TopologicalNodes(d Definition) ([]Node, error) {
	if d.SchemaVersion == LatestSchemaVersion {
		return topologicalNodes(d)
	}
	return OrderedNodes(d)
}

func topologicalNodes(d Definition) ([]Node, error) {
	if len(d.Nodes) < 2 || len(d.Nodes) > MaxNodes {
		return nil, invalid("nodes", "requires 2 to 128 nodes per scope")
	}
	if len(d.Edges) > MaxEdges {
		return nil, invalid("edges", "exceeds 256 edges per scope")
	}
	nodes := map[string]Node{}
	incoming, outgoing := map[string][]Edge{}, map[string][]Edge{}
	start, end := "", ""
	for _, n := range d.Nodes {
		if len(n.Label) > 200 {
			return nil, invalid("nodes."+n.Key+".label", "must contain at most 200 bytes")
		}
		if !nodeKeyPattern.MatchString(n.Key) {
			return nil, invalid("nodes."+n.Key+".node_key", "must be a stable identifier of 1 to 64 characters")
		}
		if _, ok := nodes[n.Key]; ok {
			return nil, invalid("nodes."+n.Key, "duplicate node key")
		}
		nodes[n.Key] = n
		switch n.Type {
		case NodeTypeStart:
			if start != "" {
				return nil, invalid("nodes", "requires exactly one start")
			}
			start = n.Key
		case NodeTypeEnd:
			if end != "" {
				return nil, invalid("nodes", "requires exactly one end")
			}
			end = n.Key
		case NodeTypeAgent, NodeTypeApproval, NodeTypeBranch, NodeTypeParallel, NodeTypeJoin, NodeTypeLoop, NodeTypeSubgraph, NodeTypeDelay, NodeTypeHTTP, NodeTypeScript:
		default:
			return nil, invalid("nodes."+n.Key+".type", "unsupported node type")
		}
	}
	if start == "" || end == "" {
		return nil, invalid("nodes", "requires exactly one start and end")
	}
	seen := map[string]bool{}
	for _, e := range d.Edges {
		source, fromOK := nodes[e.From]
		_, toOK := nodes[e.To]
		if !fromOK || !toOK {
			return nil, invalid("edges", "source or destination does not exist")
		}
		key := e.From + "/" + e.To + "/" + e.Port
		if seen[key] {
			return nil, invalid("edges", "duplicate edge")
		}
		seen[key] = true
		if source.Type == NodeTypeBranch {
			if e.Port != "true" && e.Port != "false" {
				return nil, invalid("edges", "branch edges require true or false port")
			}
		} else if e.Port != "" {
			return nil, invalid("edges", "only branch edges may name a port")
		}
		incoming[e.To] = append(incoming[e.To], e)
		outgoing[e.From] = append(outgoing[e.From], e)
	}
	for _, n := range d.Nodes {
		in, out := len(incoming[n.Key]), len(outgoing[n.Key])
		switch n.Type {
		case NodeTypeStart:
			if in != 0 || out != 1 {
				return nil, invalid("nodes."+n.Key, "start requires zero incoming and one outgoing edge")
			}
		case NodeTypeEnd:
			if in != 1 || out != 0 {
				return nil, invalid("nodes."+n.Key, "end requires one incoming and zero outgoing edges")
			}
		case NodeTypeParallel:
			if in != 1 || out < 2 {
				return nil, invalid("nodes."+n.Key, "parallel requires one incoming and at least two outgoing edges")
			}
		case NodeTypeBranch:
			if in != 1 || out != 2 || outgoing[n.Key][0].Port == outgoing[n.Key][1].Port {
				return nil, invalid("nodes."+n.Key, "branch requires one incoming and one edge for each true/false port")
			}
		case NodeTypeJoin:
			if in < 2 || out != 1 {
				return nil, invalid("nodes."+n.Key, "join requires at least two incoming and one outgoing edge")
			}
		default:
			if in != 1 || out != 1 {
				return nil, invalid("nodes."+n.Key, "node requires one incoming and one outgoing edge")
			}
		}
	}
	degree := map[string]int{}
	queue := []string{start}
	result := make([]Node, 0, len(nodes))
	for key := range nodes {
		degree[key] = len(incoming[key])
	}
	for len(queue) > 0 {
		sort.Strings(queue)
		key := queue[0]
		queue = queue[1:]
		result = append(result, nodes[key])
		for _, e := range outgoing[key] {
			degree[e.To]--
			if degree[e.To] == 0 {
				queue = append(queue, e.To)
			}
		}
	}
	if len(result) != len(nodes) {
		return nil, invalid("edges", "every node must be reachable from start without cycles")
	}
	return result, nil
}

func validateDefinitionV2(d Definition, depth int, inLoop bool) (int, error) {
	if depth > MaxGraphDepth {
		return 0, invalid("nodes", "child graph nesting exceeds 4")
	}
	if d.SchemaVersion != LatestSchemaVersion {
		return 0, invalid("schema_version", "embedded definitions require version 2")
	}
	if strings.TrimSpace(d.Name) == "" || len(d.Name) > 200 {
		return 0, invalid("name", "must contain 1 to 200 bytes")
	}
	if d.MaxConcurrency < 0 || d.MaxConcurrency > MaxConcurrency {
		return 0, invalid("max_concurrency", "must be between 1 and 32 when specified")
	}
	inputSchema, err := parseSchema(d.InputSchema, true)
	if err != nil {
		return 0, invalid("input_schema", err.Error())
	}
	ordered, err := topologicalNodes(d)
	if err != nil {
		return 0, err
	}
	refs, err := referenceScopes(d, ordered)
	if err != nil {
		return 0, err
	}
	count := len(ordered)
	for _, n := range ordered {
		path := "nodes." + n.Key
		if err := validateV2Node(n); err != nil {
			return 0, invalid(path, err.Error())
		}
		scope := bindingValidation{input: inputSchema, prior: refs[n.Key], inLoop: inLoop}
		if err := validateBindingsV2(n.Inputs, scope); err != nil {
			return 0, invalid(path+".inputs", err.Error())
		}
		if n.Condition != nil {
			if err := validateCondition(*n.Condition, scope); err != nil {
				return 0, invalid(path+".condition", err.Error())
			}
		}
		if n.HTTP != nil {
			for name, binding := range n.HTTP.Headers {
				if err := validateBindingV2(binding, scope, 0); err != nil {
					return 0, invalid(path+".http.headers."+name, err.Error())
				}
			}
			if n.HTTP.Body != nil {
				if err := validateBindingV2(*n.HTTP.Body, scope, 0); err != nil {
					return 0, invalid(path+".http.body", err.Error())
				}
			}
		}
		var child *Definition
		multiplier := 1
		if n.Loop != nil {
			child = &n.Loop.Body
			multiplier = n.Loop.MaxIterations
		}
		if n.Subgraph != nil {
			child = &n.Subgraph.Definition
		}
		if child != nil {
			childCount, err := validateDefinitionV2(*child, depth+1, inLoop || n.Loop != nil)
			if err != nil {
				return 0, invalid(path+".body", err.Error())
			}
			if len(child.OutputSchema) == 0 {
				return 0, invalid(path+".body.output_schema", "child graphs require an output schema")
			}
			if err := sameSchema(n.OutputSchema, child.OutputSchema); err != nil {
				return 0, invalid(path+".output_schema", err.Error())
			}
			if err := validateInputShape(n.Inputs, child.InputSchema); err != nil {
				return 0, invalid(path+".inputs", err.Error())
			}
			if err := validateBindingTargets(n.Inputs, child.InputSchema, scope); err != nil {
				return 0, invalid(path+".inputs", err.Error())
			}
			count += multiplier * childCount
			if count > MaxExecutionInstances {
				return 0, invalid("nodes", "expanded execution instance budget exceeds 4096")
			}
			if n.Loop != nil {
				childNodes, _ := topologicalNodes(*child)
				childRefs, err := referenceScopes(*child, childNodes)
				if err != nil {
					return 0, err
				}
				childInput, _ := parseSchema(child.InputSchema, true)
				childScope := bindingValidation{input: childInput, prior: childRefs[childNodes[len(childNodes)-1].Key], inLoop: true}
				if err := validateCondition(n.Loop.Condition, childScope); err != nil {
					return 0, invalid(path+".loop.condition", err.Error())
				}
				if err := validateBindingsV2(n.Loop.Next, childScope); err != nil {
					return 0, invalid(path+".loop.next", err.Error())
				}
				if err := validateInputShape(n.Loop.Next, child.InputSchema); err != nil {
					return 0, invalid(path+".loop.next", err.Error())
				}
				if err := validateBindingTargets(n.Loop.Next, child.InputSchema, childScope); err != nil {
					return 0, invalid(path+".loop.next", err.Error())
				}
			}
		}
	}
	if len(d.OutputSchema) > 0 {
		if _, err := parseSchema(d.OutputSchema, true); err != nil {
			return 0, invalid("output_schema", err.Error())
		}
		if err := validateInputShape(d.Outputs, d.OutputSchema); err != nil {
			return 0, invalid("outputs", err.Error())
		}
		if err := validateBindingsV2(d.Outputs, bindingValidation{input: inputSchema, prior: refs[ordered[len(ordered)-1].Key], inLoop: inLoop}); err != nil {
			return 0, invalid("outputs", err.Error())
		}
		if err := validateBindingTargets(d.Outputs, d.OutputSchema, bindingValidation{input: inputSchema, prior: refs[ordered[len(ordered)-1].Key], inLoop: inLoop}); err != nil {
			return 0, invalid("outputs", err.Error())
		}
	} else if len(d.Outputs) > 0 {
		return 0, invalid("outputs", "requires output_schema")
	}
	if err := validateLayout(d.Layout, ordered); err != nil {
		return 0, err
	}
	if depth == 0 {
		raw, err := json.Marshal(d)
		if err != nil || len(raw) > MaxDefinitionBytes {
			return 0, invalid("definition", "invalid JSON or exceeds size limit")
		}
		if _, err := decodeJSONWithDepth(raw, MaxDefinitionBytes, 64); err != nil {
			return 0, err
		}
	}
	return count, nil
}

func validateV2Node(n Node) error {
	expected := map[NodeType]bool{NodeTypeBranch: n.Condition != nil, NodeTypeApproval: n.Approval != nil || n.ApproverID != "", NodeTypeLoop: n.Loop != nil, NodeTypeSubgraph: n.Subgraph != nil, NodeTypeDelay: n.Delay != nil, NodeTypeHTTP: n.HTTP != nil, NodeTypeScript: n.Script != nil}
	for kind, present := range expected {
		if present && n.Type != kind {
			return fmt.Errorf("%s configuration is only valid on %s nodes", kind, kind)
		}
	}
	if required, ok := expected[n.Type]; ok && !required {
		return fmt.Errorf("%s configuration is required", n.Type)
	}
	if n.Type == NodeTypeAgent {
		copy := n
		copy.Condition = nil
		copy.Approval = nil
		copy.Loop = nil
		copy.Subgraph = nil
		copy.Delay = nil
		copy.HTTP = nil
		copy.Script = nil
		return validateNode(copy)
	}
	if n.Type != NodeTypeScript && (n.Instructions != "" || n.ExecutionMode != "" || n.MaxAttempts != 0 || n.TimeoutSeconds != 0) {
		return fmt.Errorf("execution settings are only valid on agent or script nodes")
	}
	if n.AgentID != "" && n.Type != NodeTypeScript {
		return fmt.Errorf("agent_id is only valid on agent or script nodes")
	}
	outputRequired := n.Type == NodeTypeHTTP || n.Type == NodeTypeScript || n.Type == NodeTypeLoop || n.Type == NodeTypeSubgraph
	if outputRequired {
		if _, err := parseSchema(n.OutputSchema, true); err != nil {
			return fmt.Errorf("output_schema: %w", err)
		}
	} else if len(n.OutputSchema) > 0 {
		return fmt.Errorf("output_schema is not valid on this node type")
	}
	if len(n.Inputs) > 0 && n.Type != NodeTypeLoop && n.Type != NodeTypeSubgraph && n.Type != NodeTypeScript && n.Type != NodeTypeApproval {
		return fmt.Errorf("inputs are not valid on this node type")
	}
	if n.Approval != nil {
		if n.ApproverID != "" {
			return fmt.Errorf("approver_id and approval cannot be combined")
		}
		if len(n.Approval.ApproverIDs) < 1 || len(n.Approval.ApproverIDs) > 32 {
			return fmt.Errorf("approval requires 1 to 32 approvers")
		}
		seen := map[string]bool{}
		for _, id := range n.Approval.ApproverIDs {
			if strings.TrimSpace(id) == "" || len(id) > 128 || seen[id] {
				return fmt.Errorf("approval members must be nonempty and unique")
			}
			seen[id] = true
		}
		switch n.Approval.Mode {
		case "all", "any":
			if n.Approval.Quorum != 0 {
				return fmt.Errorf("quorum is only valid in quorum mode")
			}
		case "quorum":
			if n.Approval.Quorum < 1 || n.Approval.Quorum > len(n.Approval.ApproverIDs) {
				return fmt.Errorf("quorum must be between 1 and the number of approvers")
			}
		default:
			return fmt.Errorf("unsupported approval mode")
		}
	}
	if n.Type == NodeTypeApproval && n.Approval == nil && strings.TrimSpace(n.ApproverID) == "" {
		return fmt.Errorf("approver_id is required")
	}
	if n.Loop != nil && (n.Loop.MaxIterations < 1 || n.Loop.MaxIterations > MaxLoopIterations) {
		return fmt.Errorf("max_iterations must be between 1 and 100")
	}
	if n.Delay != nil && (n.Delay.Seconds < 1 || n.Delay.Seconds > 86400) {
		return fmt.Errorf("delay seconds must be between 1 and 86400")
	}
	if n.Script != nil {
		if n.Instructions != "" || n.ExecutionMode != ExecutionNone || n.MaxAttempts < 1 || n.MaxAttempts > 3 || n.TimeoutSeconds != n.Script.TimeoutSeconds {
			return fmt.Errorf("script requires execution_mode none, 1 to 3 attempts and matching timeout_seconds")
		}
		if strings.TrimSpace(n.AgentID) == "" {
			return fmt.Errorf("script agent_id is required")
		}
		if n.Script.Language != "javascript" && n.Script.Language != "python" {
			return fmt.Errorf("unsupported script language")
		}
		if strings.TrimSpace(n.Script.Code) == "" || len(n.Script.Code) > 64<<10 {
			return fmt.Errorf("script code must contain 1 to 65536 bytes")
		}
		if n.Script.TimeoutSeconds < 1 || n.Script.TimeoutSeconds > 3600 {
			return fmt.Errorf("script timeout must be between 1 and 3600")
		}
	}
	if n.HTTP != nil {
		h := n.HTTP
		switch h.Method {
		case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD":
		default:
			return fmt.Errorf("unsupported HTTP method")
		}
		u, err := url.Parse(h.URL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.Fragment != "" || len(h.URL) > 8192 {
			return fmt.Errorf("HTTP URL must be absolute http/https without credentials or fragment")
		}
		if h.TimeoutSeconds < 1 || h.TimeoutSeconds > 300 {
			return fmt.Errorf("HTTP timeout must be between 1 and 300")
		}
		if h.MaxResponseBytes < 1 || h.MaxResponseBytes > MaxJSONBytes {
			return fmt.Errorf("HTTP max_response_bytes must be between 1 and 262144")
		}
		if len(h.Headers) > 32 {
			return fmt.Errorf("HTTP headers exceeds 32")
		}
		seen := map[string]bool{}
		for key := range h.Headers {
			normalized := strings.ToLower(key)
			if !validHeaderName(key) || seen[normalized] {
				return fmt.Errorf("invalid or duplicate HTTP header name")
			}
			seen[normalized] = true
		}
		if (h.Method == "GET" || h.Method == "HEAD") && h.Body != nil {
			return fmt.Errorf("GET and HEAD cannot have a request body")
		}
	}
	return nil
}

func validHeaderName(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", c)) {
			return false
		}
	}
	switch strings.ToLower(s) {
	case "host", "connection", "content-length", "transfer-encoding", "trailer", "upgrade", "proxy-authorization", "proxy-connection", "te", "idempotency-key":
		return false
	}
	return true
}

func sameSchema(a, b json.RawMessage) error {
	_, ah, err := CanonicalJSON(a)
	if err != nil {
		return err
	}
	_, bh, err := CanonicalJSON(b)
	if err != nil {
		return err
	}
	if ah != bh {
		return fmt.Errorf("must match child graph output_schema")
	}
	return nil
}
func validateInputShape(bindings map[string]InputBinding, raw json.RawMessage) error {
	s, err := parseSchema(raw, true)
	if err != nil {
		return err
	}
	required, _ := s["required"].([]any)
	for _, field := range required {
		if _, ok := bindings[field.(string)]; !ok {
			return fmt.Errorf("required field %q has no binding", field)
		}
	}
	if s["additionalProperties"] == false {
		props, _ := s["properties"].(map[string]any)
		for field := range bindings {
			if _, ok := props[field]; !ok {
				return fmt.Errorf("binding field %q is not declared", field)
			}
		}
	}
	return nil
}
func validateLayout(layout *Layout, nodes []Node) error {
	if layout == nil {
		return nil
	}
	keys := map[string]bool{}
	for _, n := range nodes {
		keys[n.Key] = true
	}
	valid := func(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && math.Abs(v) <= 1e6 }
	for key, p := range layout.Nodes {
		if !keys[key] || !valid(p.X) || !valid(p.Y) {
			return invalid("layout.nodes", "requires existing node keys and finite positions within +/-1000000")
		}
	}
	if p := layout.Viewport; p != nil {
		if !valid(p.X) || !valid(p.Y) || !valid(p.Zoom) || p.Zoom < 0.1 || p.Zoom > 4 {
			return invalid("layout.viewport", "invalid position or zoom outside 0.1 to 4")
		}
	}
	return nil
}
func canonicalizeV2(d *Definition) {
	d.Nodes, _ = topologicalNodes(*d)
	sort.Slice(d.Edges, func(i, j int) bool {
		a, b := d.Edges[i], d.Edges[j]
		if a.From != b.From {
			return a.From < b.From
		}
		if a.Port != b.Port {
			return a.Port < b.Port
		}
		return a.To < b.To
	})
	for i := range d.Nodes {
		if d.Nodes[i].Loop != nil {
			copy := *d.Nodes[i].Loop
			canonicalizeV2(&copy.Body)
			d.Nodes[i].Loop = &copy
		}
		if d.Nodes[i].Subgraph != nil {
			copy := *d.Nodes[i].Subgraph
			canonicalizeV2(&copy.Definition)
			d.Nodes[i].Subgraph = &copy
		}
	}
}
