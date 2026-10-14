package graph

import "encoding/json"

const (
	LatestSchemaVersion   = 2
	MaxNodes              = 128
	MaxEdges              = 256
	MaxGraphDepth         = 4
	MaxExecutionInstances = 4096
	MaxLoopIterations     = 100
	DefaultMaxConcurrency = 4
	MaxConcurrency        = 32
)

const (
	NodeTypeBranch   NodeType  = "branch"
	NodeTypeParallel NodeType  = "parallel"
	NodeTypeJoin     NodeType  = "join"
	NodeTypeLoop     NodeType  = "loop"
	NodeTypeSubgraph NodeType  = "subgraph"
	NodeTypeDelay    NodeType  = "delay"
	NodeTypeHTTP     NodeType  = "http"
	NodeTypeScript   NodeType  = "script"
	NodeSkipped      NodeState = "skipped"
)

// Conditions are a bounded expression tree; no expression strings are evaluated.
type Condition struct {
	Op         string        `json:"op"`
	Left       *InputBinding `json:"left,omitempty"`
	Right      *InputBinding `json:"right,omitempty"`
	Conditions []Condition   `json:"conditions,omitempty"`
}

type ApprovalConfig struct {
	ApproverIDs []string `json:"approver_ids"`
	Mode        string   `json:"mode"`
	Quorum      int      `json:"quorum,omitempty"`
}

// Loop always executes the body once. Condition true requests another iteration.
// Inputs are Node.Inputs for the first iteration and Next for later iterations.
type LoopConfig struct {
	Body          Definition              `json:"body"`
	MaxIterations int                     `json:"max_iterations"`
	Condition     Condition               `json:"condition"`
	Next          map[string]InputBinding `json:"next"`
}

type SubgraphConfig struct {
	Definition Definition `json:"definition"`
}
type DelayConfig struct {
	Seconds int `json:"seconds"`
}
type HTTPConfig struct {
	Method           string                  `json:"method"`
	URL              string                  `json:"url"`
	Headers          map[string]InputBinding `json:"headers,omitempty"`
	Body             *InputBinding           `json:"body,omitempty"`
	TimeoutSeconds   int                     `json:"timeout_seconds"`
	MaxResponseBytes int                     `json:"max_response_bytes"`
}

// Scripts execute on the node's selected agent runtime, never the coordinator.
type ScriptConfig struct {
	Language       string `json:"language"`
	Code           string `json:"code"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}
type Viewport struct {
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Zoom float64 `json:"zoom"`
}
type Layout struct {
	Nodes    map[string]Position `json:"nodes"`
	Viewport *Viewport           `json:"viewport,omitempty"`
}

// EvaluationContext contains accepted outputs in one execution scope only.
// IterationIndex is one-based and zero outside a loop body.
type EvaluationContext struct {
	RunInput       json.RawMessage
	Outputs        map[string]json.RawMessage
	IterationIndex int
}

type ReadinessPlan struct {
	Ready   []string          `json:"ready"`
	Skipped []string          `json:"skipped"`
	Blocked map[string]string `json:"blocked"`
}
