package graph

import (
	"encoding/json"
	"fmt"
)

type sourceSchema struct {
	schema     map[string]any
	guaranteed bool
}
type bindingValidation struct {
	input  map[string]any
	prior  map[string]sourceSchema
	inLoop bool
}

func validateBindingsV2(bindings map[string]InputBinding, scope bindingValidation) error {
	if len(bindings) > 128 {
		return fmt.Errorf("at most 128 bindings are allowed")
	}
	for field, b := range bindings {
		if field == "" || len(field) > 128 {
			return fmt.Errorf("invalid field name")
		}
		if err := validateBindingV2(b, scope, 0); err != nil {
			return fmt.Errorf("%s: %w", field, err)
		}
	}
	return nil
}

func validateBindingV2(b InputBinding, scope bindingValidation, depth int) error {
	if depth > MaxDepth {
		return fmt.Errorf("binding nesting exceeds limit")
	}
	if len(b.Default) > 0 {
		if _, err := decodeJSON(b.Default, MaxJSONBytes); err != nil {
			return err
		}
	}
	switch b.Source {
	case BindingRunInput, BindingNodeOutput:
		if len(b.Value) > 0 || len(b.Fields) > 0 {
			return fmt.Errorf("reference cannot contain value or fields")
		}
		s := scope.input
		if b.Source == BindingNodeOutput {
			source, ok := scope.prior[b.NodeKey]
			if !ok {
				return fmt.Errorf("node_output must refer to an output-producing dependency ancestor in this scope")
			}
			if !source.guaranteed && !b.Optional && len(b.Default) == 0 {
				return fmt.Errorf("conditional ancestor output requires optional or default")
			}
			s = source.schema
		} else if b.NodeKey != "" {
			return fmt.Errorf("run_input cannot name a node")
		}
		selected, err := schemaAtPointer(s, b.Path)
		if err != nil {
			return err
		}
		if len(b.Default) > 0 {
			value, err := decodeJSON(b.Default, MaxJSONBytes)
			if err != nil {
				return err
			}
			if err := validateValue(selected, value, "default", 0); err != nil {
				return err
			}
		}
		return nil
	case BindingIterationIndex:
		if !scope.inLoop {
			return fmt.Errorf("iteration_index is only available in loop scopes")
		}
		if b.NodeKey != "" || b.Path != "" || len(b.Value) > 0 || len(b.Fields) > 0 || b.Optional || len(b.Default) > 0 {
			return fmt.Errorf("iteration_index cannot contain other binding fields")
		}
		return nil
	case BindingConstant:
		if b.NodeKey != "" || b.Path != "" || len(b.Fields) > 0 || b.Optional || len(b.Default) > 0 {
			return fmt.Errorf("constant cannot contain reference or object fields")
		}
		_, err := decodeJSON(b.Value, MaxJSONBytes)
		return err
	case BindingObject:
		if b.NodeKey != "" || b.Path != "" || len(b.Value) > 0 || b.Optional || len(b.Default) > 0 {
			return fmt.Errorf("object cannot contain reference or constant fields")
		}
		if len(b.Fields) > 128 {
			return fmt.Errorf("object exceeds 128 fields")
		}
		for key, v := range b.Fields {
			if key == "" || len(key) > 128 {
				return fmt.Errorf("invalid object field name")
			}
			if err := validateBindingV2(v, scope, depth+1); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported binding source %q", b.Source)
	}
}

func validateBindingTargets(bindings map[string]InputBinding, raw json.RawMessage, scope bindingValidation) error {
	s, err := parseSchema(raw, true)
	if err != nil {
		return err
	}
	properties, _ := s["properties"].(map[string]any)
	for field, b := range bindings {
		target, ok := properties[field].(map[string]any)
		if !ok {
			continue
		}
		if err := validateBindingTarget(b, target, scope, 0); err != nil {
			return fmt.Errorf("%s: %w", field, err)
		}
	}
	return nil
}

func validateBindingTarget(b InputBinding, target map[string]any, scope bindingValidation, depth int) error {
	if depth > MaxDepth {
		return fmt.Errorf("binding nesting exceeds limit")
	}
	if b.Source == BindingConstant {
		value, err := decodeJSON(b.Value, MaxJSONBytes)
		if err != nil {
			return err
		}
		return validateValue(target, value, "value", 0)
	}
	kind, err := bindingType(b, scope)
	if err != nil {
		return err
	}
	expected := target["type"].(string)
	if kind != expected && !(kind == "integer" && expected == "number") {
		return fmt.Errorf("binding type %s does not match target type %s", kind, expected)
	}
	if b.Source == BindingObject {
		props, _ := target["properties"].(map[string]any)
		required, _ := target["required"].([]any)
		for _, field := range required {
			if _, ok := b.Fields[field.(string)]; !ok {
				return fmt.Errorf("required object field %q has no binding", field)
			}
		}
		for field, value := range b.Fields {
			child, ok := props[field].(map[string]any)
			if !ok {
				if target["additionalProperties"] == false {
					return fmt.Errorf("unexpected object field %q", field)
				}
				continue
			}
			if err := validateBindingTarget(value, child, scope, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

func bindingType(b InputBinding, scope bindingValidation) (string, error) {
	switch b.Source {
	case BindingConstant:
		v, err := decodeJSON(b.Value, MaxJSONBytes)
		if err != nil {
			return "", err
		}
		return valueType(v), nil
	case BindingObject:
		return "object", nil
	case BindingIterationIndex:
		return "integer", nil
	case BindingRunInput:
		s, err := schemaAtPointer(scope.input, b.Path)
		if err != nil {
			return "", err
		}
		return s["type"].(string), nil
	case BindingNodeOutput:
		s, err := schemaAtPointer(scope.prior[b.NodeKey].schema, b.Path)
		if err != nil {
			return "", err
		}
		return s["type"].(string), nil
	default:
		return "", fmt.Errorf("unknown binding type")
	}
}

func valueType(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case json.Number:
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return "unknown"
	}
}

// referenceScopes proves that referenced outputs are ancestors and, unless an
// explicit fallback exists, execute whenever the consuming node executes.
func referenceScopes(d Definition, ordered []Node) (map[string]map[string]sourceSchema, error) {
	incoming := map[string][]Edge{}
	for _, e := range d.Edges {
		incoming[e.To] = append(incoming[e.To], e)
	}
	ancestors := map[string]map[string]bool{}
	activations := map[string]int{}
	schemas := map[string]map[string]any{}
	nodes := map[string]Node{}
	bdd := newDecisionDiagram()
	result := map[string]map[string]sourceSchema{}
	for index, n := range ordered {
		nodes[n.Key] = n
		ancestors[n.Key] = map[string]bool{}
		activation := 0
		if n.Type == NodeTypeStart {
			activation = 1
		}
		for _, e := range incoming[n.Key] {
			ancestors[n.Key][e.From] = true
			for key := range ancestors[e.From] {
				ancestors[n.Key][key] = true
			}
			edgeActivation := activations[e.From]
			if nodes[e.From].Type == NodeTypeBranch {
				variable := bdd.variable(e.From)
				if e.Port == "false" {
					variable = bdd.not(variable)
				}
				edgeActivation = bdd.and(edgeActivation, variable)
			}
			activation = bdd.or(activation, edgeActivation)
		}
		activations[n.Key] = activation
		refs := map[string]sourceSchema{}
		for _, prior := range ordered[:index] {
			if ancestors[n.Key][prior.Key] && schemas[prior.Key] != nil {
				refs[prior.Key] = sourceSchema{schema: schemas[prior.Key], guaranteed: bdd.and(activation, bdd.not(activations[prior.Key])) == 0}
			}
		}
		if bdd.err != nil {
			return nil, invalid("edges", bdd.err.Error())
		}
		result[n.Key] = refs
		if len(n.OutputSchema) > 0 {
			schema, err := parseSchema(n.OutputSchema, true)
			if err != nil {
				return nil, invalid("nodes."+n.Key+".output_schema", err.Error())
			}
			schemas[n.Key] = schema
		}
	}
	return result, nil
}

// The reduced ordered decision diagram prevents branch-path enumeration from
// becoming exponential. Its explicit node budget bounds adversarial topology.
type decisionNode struct{ variable, low, high int }
type decisionDiagram struct {
	nodes     []decisionNode
	unique    map[decisionNode]int
	variables map[string]int
	memo      map[[3]int]int
	negated   map[int]int
	err       error
}

func newDecisionDiagram() *decisionDiagram {
	return &decisionDiagram{nodes: []decisionNode{{variable: 1 << 30}, {variable: 1 << 30}}, unique: map[decisionNode]int{}, variables: map[string]int{}, memo: map[[3]int]int{}, negated: map[int]int{0: 1, 1: 0}}
}
func (b *decisionDiagram) make(variable, low, high int) int {
	if low == high {
		return low
	}
	n := decisionNode{variable, low, high}
	if id, ok := b.unique[n]; ok {
		return id
	}
	if len(b.nodes) >= 8192 {
		b.err = fmt.Errorf("branch condition complexity exceeds 8192 states")
		return 0
	}
	id := len(b.nodes)
	b.nodes = append(b.nodes, n)
	b.unique[n] = id
	return id
}
func (b *decisionDiagram) variable(key string) int {
	v, ok := b.variables[key]
	if !ok {
		v = len(b.variables)
		b.variables[key] = v
	}
	return b.make(v, 0, 1)
}
func (b *decisionDiagram) not(id int) int {
	if result, ok := b.negated[id]; ok {
		return result
	}
	n := b.nodes[id]
	result := b.make(n.variable, b.not(n.low), b.not(n.high))
	b.negated[id] = result
	return result
}
func (b *decisionDiagram) and(a, c int) int { return b.apply(0, a, c) }
func (b *decisionDiagram) or(a, c int) int  { return b.apply(1, a, c) }
func (b *decisionDiagram) apply(op, a, c int) int {
	if b.err != nil {
		return 0
	}
	if len(b.memo) > 65536 {
		b.err = fmt.Errorf("branch analysis exceeds operation budget")
		return 0
	}
	if a > c {
		a, c = c, a
	}
	if op == 0 {
		if a == 0 {
			return 0
		}
		if a == 1 {
			return c
		}
	} else {
		if c == 1 || a == 1 {
			return 1
		}
		if a == 0 {
			return c
		}
	}
	if a == c {
		return a
	}
	key := [3]int{op, a, c}
	if result, ok := b.memo[key]; ok {
		return result
	}
	an, cn := b.nodes[a], b.nodes[c]
	v := min(an.variable, cn.variable)
	al, ah, cl, ch := a, a, c, c
	if an.variable == v {
		al, ah = an.low, an.high
	}
	if cn.variable == v {
		cl, ch = cn.low, cn.high
	}
	result := b.make(v, b.apply(op, al, cl), b.apply(op, ah, ch))
	b.memo[key] = result
	return result
}

func validateCondition(c Condition, scope bindingValidation) error {
	count := 0
	return validateConditionAt(c, scope, 0, &count)
}
func validateConditionAt(c Condition, scope bindingValidation, depth int, count *int) error {
	*count++
	if depth > MaxDepth || *count > 128 {
		return fmt.Errorf("condition exceeds depth or 128 predicates")
	}
	switch c.Op {
	case "all", "any", "not":
		if c.Left != nil || c.Right != nil || len(c.Conditions) == 0 || len(c.Conditions) > 32 || (c.Op == "not" && len(c.Conditions) != 1) {
			return fmt.Errorf("invalid logical condition operands")
		}
		for _, child := range c.Conditions {
			if err := validateConditionAt(child, scope, depth+1, count); err != nil {
				return err
			}
		}
		return nil
	case "exists":
		if c.Left == nil || c.Right != nil || len(c.Conditions) > 0 || (c.Left.Source != BindingRunInput && c.Left.Source != BindingNodeOutput) {
			return fmt.Errorf("exists requires one reference operand")
		}
		copy := *c.Left
		copy.Optional = true
		return validateBindingV2(copy, scope, 0)
	case "eq", "ne", "gt", "gte", "lt", "lte":
		if c.Left == nil || c.Right == nil || len(c.Conditions) > 0 {
			return fmt.Errorf("comparison requires left and right operands")
		}
		if err := validateBindingV2(*c.Left, scope, 0); err != nil {
			return err
		}
		if err := validateBindingV2(*c.Right, scope, 0); err != nil {
			return err
		}
		left, err := bindingType(*c.Left, scope)
		if err != nil {
			return err
		}
		right, err := bindingType(*c.Right, scope)
		if err != nil {
			return err
		}
		numeric := func(s string) bool { return s == "number" || s == "integer" }
		compatible := left == right || numeric(left) && numeric(right)
		if c.Op == "eq" || c.Op == "ne" {
			if !compatible && !(left == "null" && c.Right.Optional) && !(right == "null" && c.Left.Optional) {
				return fmt.Errorf("comparison operands must have compatible types")
			}
			return nil
		}
		if !compatible || !(numeric(left) || left == "string") {
			return fmt.Errorf("ordering requires both numeric or both string operands")
		}
		return nil
	default:
		return fmt.Errorf("unsupported condition operator %q", c.Op)
	}
}
