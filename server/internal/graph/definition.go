// Package graph defines bounded Graph contracts independently of storage and execution.
package graph

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

const (
	SchemaVersion      = 1
	MaxAgentNodes      = 16
	MaxDefinitionBytes = 256 << 10
	MaxJSONBytes       = 256 << 10
	MaxSchemaBytes     = 64 << 10
	MaxDepth           = 16
)

type NodeType string

const (
	NodeTypeStart    NodeType = "start"
	NodeTypeAgent    NodeType = "agent"
	NodeTypeApproval NodeType = "approval"
	NodeTypeEnd      NodeType = "end"
)

type ExecutionMode string

const (
	ExecutionNone          ExecutionMode = "none"
	ExecutionProduceCommit ExecutionMode = "produce_commit"
	ExecutionVerifyCommit  ExecutionMode = "verify_commit"
)

type BindingSource string

const (
	BindingRunInput       BindingSource = "run_input"
	BindingNodeOutput     BindingSource = "node_output"
	BindingConstant       BindingSource = "constant"
	BindingObject         BindingSource = "object"
	BindingIterationIndex BindingSource = "iteration_index"
)

type Definition struct {
	SchemaVersion  int                     `json:"schema_version"`
	Name           string                  `json:"name"`
	Nodes          []Node                  `json:"nodes"`
	Edges          []Edge                  `json:"edges"`
	InputSchema    json.RawMessage         `json:"input_schema"`
	OutputSchema   json.RawMessage         `json:"output_schema,omitempty"`
	Outputs        map[string]InputBinding `json:"outputs,omitempty"`
	MaxConcurrency int                     `json:"max_concurrency,omitempty"`
	Layout         *Layout                 `json:"layout,omitempty"`
}

type Node struct {
	Key            string                  `json:"node_key"`
	Label          string                  `json:"label,omitempty"`
	Type           NodeType                `json:"type"`
	AgentID        string                  `json:"agent_id,omitempty"`
	Instructions   string                  `json:"instructions,omitempty"`
	Inputs         map[string]InputBinding `json:"inputs,omitempty"`
	OutputSchema   json.RawMessage         `json:"output_schema,omitempty"`
	ExecutionMode  ExecutionMode           `json:"execution_mode,omitempty"`
	MaxAttempts    int                     `json:"max_attempts,omitempty"`
	TimeoutSeconds int                     `json:"timeout_seconds,omitempty"`
	ApproverID     string                  `json:"approver_id,omitempty"`
	Condition      *Condition              `json:"condition,omitempty"`
	Approval       *ApprovalConfig         `json:"approval,omitempty"`
	Loop           *LoopConfig             `json:"loop,omitempty"`
	Subgraph       *SubgraphConfig         `json:"subgraph,omitempty"`
	Delay          *DelayConfig            `json:"delay,omitempty"`
	HTTP           *HTTPConfig             `json:"http,omitempty"`
	Script         *ScriptConfig           `json:"script,omitempty"`
}

type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Port string `json:"port,omitempty"`
}

type InputBinding struct {
	Source   BindingSource           `json:"source"`
	NodeKey  string                  `json:"node_key,omitempty"`
	Path     string                  `json:"path,omitempty"`
	Value    json.RawMessage         `json:"value,omitempty"`
	Fields   map[string]InputBinding `json:"fields,omitempty"`
	Default  json.RawMessage         `json:"default,omitempty"`
	Optional bool                    `json:"optional,omitempty"`
}

// ValidationError keeps a stable field path for HTTP and editor diagnostics.
type ValidationError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

func (e *ValidationError) Error() string { return e.Path + ": " + e.Message }
func invalid(path, message string) error { return &ValidationError{Path: path, Message: message} }

var nodeKeyPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,63}$`)

func ParseDefinition(data []byte) (Definition, error) {
	var d Definition
	if _, err := decodeJSONWithDepth(data, MaxDefinitionBytes, 64); err != nil {
		return d, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&d); err != nil {
		return d, invalid("definition", err.Error())
	}
	return d, ValidateDefinition(d)
}

func ValidateDefinition(d Definition) error {
	if d.SchemaVersion == LatestSchemaVersion {
		_, err := validateDefinitionV2(d, 0, false)
		return err
	}
	if d.SchemaVersion != SchemaVersion {
		return invalid("schema_version", "only versions 1 and 2 are supported")
	}
	if len(d.OutputSchema) > 0 || len(d.Outputs) > 0 || d.MaxConcurrency != 0 || d.Layout != nil {
		return invalid("definition", "version 2 configuration is not valid in version 1")
	}
	for _, n := range d.Nodes {
		if len(n.Label) > 200 {
			return invalid("nodes."+n.Key+".label", "must contain at most 200 bytes")
		}
		if hasV2Config(n) {
			return invalid("nodes."+n.Key, "version 2 configuration is not valid in version 1")
		}
	}
	for _, e := range d.Edges {
		if e.Port != "" {
			return invalid("edges", "ports require version 2")
		}
	}
	if strings.TrimSpace(d.Name) == "" || len(d.Name) > 200 {
		return invalid("name", "must contain 1 to 200 bytes")
	}
	inputSchema, err := parseSchema(d.InputSchema, true)
	if err != nil {
		return invalid("input_schema", err.Error())
	}
	ordered, err := orderedNodes(d)
	if err != nil {
		return err
	}
	prior := map[string]map[string]any{}
	for _, n := range ordered {
		if err := validateNode(n); err != nil {
			return err
		}
		if n.Type != NodeTypeAgent {
			continue
		}
		if len(n.Inputs) > 128 {
			return invalid("nodes."+n.Key+".inputs", "at most 128 bindings are allowed")
		}
		for field, binding := range n.Inputs {
			if field == "" || len(field) > 128 {
				return invalid("nodes."+n.Key+".inputs", "invalid field name")
			}
			if err := validateBinding(binding, inputSchema, prior, 0); err != nil {
				return invalid("nodes."+n.Key+".inputs."+field, err.Error())
			}
		}
		schema, err := parseSchema(n.OutputSchema, true)
		if err != nil {
			return invalid("nodes."+n.Key+".output_schema", err.Error())
		}
		prior[n.Key] = schema
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return invalid("definition", err.Error())
	}
	if len(raw) > MaxDefinitionBytes {
		return invalid("definition", "exceeds size limit")
	}
	return nil
}

func validateNode(n Node) error {
	path := "nodes." + n.Key
	if !nodeKeyPattern.MatchString(n.Key) {
		return invalid(path+".node_key", "must be a stable identifier of 1 to 64 characters")
	}
	if n.Type == NodeTypeAgent {
		if strings.TrimSpace(n.AgentID) == "" {
			return invalid(path+".agent_id", "is required")
		}
		if strings.TrimSpace(n.Instructions) == "" || len(n.Instructions) > 64<<10 {
			return invalid(path+".instructions", "must contain 1 to 65536 bytes")
		}
		if n.MaxAttempts < 1 || n.MaxAttempts > 3 {
			return invalid(path+".max_attempts", "must be between 1 and 3")
		}
		if n.TimeoutSeconds < 1 || n.TimeoutSeconds > 86400 {
			return invalid(path+".timeout_seconds", "must be between 1 and 86400")
		}
		if n.ExecutionMode != ExecutionNone && n.ExecutionMode != ExecutionProduceCommit && n.ExecutionMode != ExecutionVerifyCommit {
			return invalid(path+".execution_mode", "unsupported execution mode")
		}
		if n.ApproverID != "" {
			return invalid(path+".approver_id", "is only valid on approval nodes")
		}
		if _, err := parseSchema(n.OutputSchema, true); err != nil {
			return invalid(path+".output_schema", err.Error())
		}
		return nil
	}
	if n.AgentID != "" || n.Instructions != "" || len(n.Inputs) > 0 || len(n.OutputSchema) > 0 || n.ExecutionMode != "" || n.MaxAttempts != 0 || n.TimeoutSeconds != 0 {
		return invalid(path, "agent configuration is only valid on agent nodes")
	}
	if n.Type == NodeTypeApproval {
		if strings.TrimSpace(n.ApproverID) == "" {
			return invalid(path+".approver_id", "is required")
		}
	} else if n.ApproverID != "" {
		return invalid(path+".approver_id", "is only valid on approval nodes")
	}
	return nil
}

// OrderedNodes returns the single validated path, regardless of JSON array order.
func OrderedNodes(d Definition) ([]Node, error) {
	if err := ValidateDefinition(d); err != nil {
		return nil, err
	}
	return orderedNodes(d)
}

func orderedNodes(d Definition) ([]Node, error) {
	if d.SchemaVersion == LatestSchemaVersion {
		return topologicalNodes(d)
	}
	if len(d.Nodes) < 4 || len(d.Nodes) > MaxAgentNodes+3 {
		return nil, invalid("nodes", "requires start, 1 to 16 agents, approval, and end")
	}
	if len(d.Edges) != len(d.Nodes)-1 {
		return nil, invalid("edges", "must form one serial path")
	}
	nodes := make(map[string]Node, len(d.Nodes))
	counts := map[NodeType]int{}
	start := ""
	for _, n := range d.Nodes {
		if _, ok := nodes[n.Key]; ok {
			return nil, invalid("nodes."+n.Key, "duplicate node key")
		}
		if n.Type != NodeTypeStart && n.Type != NodeTypeAgent && n.Type != NodeTypeApproval && n.Type != NodeTypeEnd {
			return nil, invalid("nodes."+n.Key+".type", "unsupported node type")
		}
		nodes[n.Key] = n
		counts[n.Type]++
		if n.Type == NodeTypeStart {
			start = n.Key
		}
	}
	if counts[NodeTypeStart] != 1 || counts[NodeTypeEnd] != 1 || counts[NodeTypeApproval] != 1 || counts[NodeTypeAgent] < 1 {
		return nil, invalid("nodes", "requires exactly one start, approval, and end")
	}
	next := map[string]string{}
	incoming := map[string]int{}
	for _, e := range d.Edges {
		if _, ok := nodes[e.From]; !ok {
			return nil, invalid("edges", "source does not exist")
		}
		if _, ok := nodes[e.To]; !ok {
			return nil, invalid("edges", "destination does not exist")
		}
		if _, ok := next[e.From]; ok {
			return nil, invalid("edges", "branches are not supported")
		}
		next[e.From] = e.To
		incoming[e.To]++
		if incoming[e.To] > 1 {
			return nil, invalid("edges", "joins are not supported")
		}
	}
	if incoming[start] != 0 {
		return nil, invalid("edges", "start cannot have an incoming edge")
	}
	ordered := make([]Node, 0, len(nodes))
	seen := map[string]bool{}
	for key := start; key != ""; key = next[key] {
		if seen[key] {
			return nil, invalid("edges", "cycles are not supported")
		}
		seen[key] = true
		ordered = append(ordered, nodes[key])
	}
	if len(ordered) != len(nodes) {
		return nil, invalid("edges", "every node must be reachable from start")
	}
	if ordered[len(ordered)-1].Type != NodeTypeEnd || ordered[len(ordered)-2].Type != NodeTypeApproval {
		return nil, invalid("nodes", "approval must immediately precede end")
	}
	for _, n := range ordered[1 : len(ordered)-2] {
		if n.Type != NodeTypeAgent {
			return nil, invalid("nodes", "only agent nodes may precede approval")
		}
	}
	return ordered, nil
}

func CanonicalDefinition(d Definition) ([]byte, string, error) {
	if err := ValidateDefinition(d); err != nil {
		return nil, "", err
	}
	nodes, err := orderedNodes(d)
	if err != nil {
		return nil, "", err
	}
	d.Nodes = nodes
	if d.SchemaVersion == LatestSchemaVersion {
		canonicalizeV2(&d)
		raw, err := json.Marshal(d)
		if err != nil {
			return nil, "", err
		}
		value, err := decodeJSONWithDepth(raw, MaxDefinitionBytes, 64)
		if err != nil {
			return nil, "", err
		}
		raw, err = json.Marshal(value)
		if err != nil {
			return nil, "", err
		}
		sum := sha256.Sum256(raw)
		return raw, hex.EncodeToString(sum[:]), nil
	}
	d.Edges = make([]Edge, 0, len(nodes)-1)
	for i := 1; i < len(nodes); i++ {
		d.Edges = append(d.Edges, Edge{From: nodes[i-1].Key, To: nodes[i].Key})
	}
	return canonicalValue(d)
}

func validateBinding(b InputBinding, input map[string]any, prior map[string]map[string]any, depth int) error {
	if b.Optional || len(b.Default) > 0 {
		return fmt.Errorf("optional and default bindings require version 2")
	}
	if depth > MaxDepth {
		return fmt.Errorf("input binding nesting exceeds %d", MaxDepth)
	}
	switch b.Source {
	case BindingRunInput, BindingNodeOutput:
		if len(b.Value) > 0 || len(b.Fields) > 0 {
			return fmt.Errorf("reference binding cannot also contain value or fields")
		}
		schema := input
		if b.Source == BindingNodeOutput {
			var ok bool
			schema, ok = prior[b.NodeKey]
			if !ok {
				return fmt.Errorf("node_output must refer to an earlier agent node")
			}
		} else if b.NodeKey != "" {
			return fmt.Errorf("run_input cannot name a node")
		}
		_, err := schemaAtPointer(schema, b.Path)
		return err
	case BindingConstant:
		if b.NodeKey != "" || b.Path != "" || len(b.Fields) > 0 {
			return fmt.Errorf("constant cannot contain reference or object fields")
		}
		_, err := decodeJSON(b.Value, MaxJSONBytes)
		return err
	case BindingObject:
		if b.NodeKey != "" || b.Path != "" || len(b.Value) > 0 {
			return fmt.Errorf("object cannot contain reference or constant value")
		}
		if len(b.Fields) > 128 {
			return fmt.Errorf("object exceeds 128 fields")
		}
		for _, field := range b.Fields {
			if err := validateBinding(field, input, prior, depth+1); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported input source %q", b.Source)
	}
}
