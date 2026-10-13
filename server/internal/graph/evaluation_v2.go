package graph

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// ResolveBinding never reads outputs outside the supplied scope. A default
// handles absence only; malformed JSON and malformed pointers remain errors.
func ResolveBinding(b InputBinding, ctx EvaluationContext) (json.RawMessage, error) {
	if err := validateEvaluationBinding(b, 0); err != nil {
		return nil, err
	}
	value, _, err := resolveScopedBinding(b, ctx, 0, false)
	if err != nil {
		return nil, err
	}
	raw, _, err := canonicalValue(value)
	return raw, err
}

func ResolveScopedInputs(bindings map[string]InputBinding, ctx EvaluationContext) (json.RawMessage, error) {
	if len(bindings) > 128 {
		return nil, invalid("inputs", "at most 128 fields are allowed")
	}
	result := map[string]any{}
	for field, b := range bindings {
		if field == "" || len(field) > 128 {
			return nil, invalid("inputs", "invalid field name")
		}
		if err := validateEvaluationBinding(b, 0); err != nil {
			return nil, invalid("inputs."+field, err.Error())
		}
		value, _, err := resolveScopedBinding(b, ctx, 0, false)
		if err != nil {
			return nil, invalid("inputs."+field, err.Error())
		}
		result[field] = value
	}
	raw, _, err := canonicalValue(result)
	return raw, err
}

func validateEvaluationBinding(b InputBinding, depth int) error {
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
		if len(b.Value) > 0 || len(b.Fields) > 0 || (b.Source == BindingRunInput && b.NodeKey != "") || (b.Source == BindingNodeOutput && !nodeKeyPattern.MatchString(b.NodeKey)) {
			return fmt.Errorf("invalid reference binding")
		}
		_, err := pointerParts(b.Path)
		return err
	case BindingIterationIndex:
		if b.NodeKey != "" || b.Path != "" || len(b.Value) > 0 || len(b.Fields) > 0 || b.Optional || len(b.Default) > 0 {
			return fmt.Errorf("invalid iteration_index binding")
		}
		return nil
	case BindingConstant:
		if b.NodeKey != "" || b.Path != "" || len(b.Fields) > 0 || b.Optional || len(b.Default) > 0 {
			return fmt.Errorf("invalid constant binding")
		}
		_, err := decodeJSON(b.Value, MaxJSONBytes)
		return err
	case BindingObject:
		if b.NodeKey != "" || b.Path != "" || len(b.Value) > 0 || b.Optional || len(b.Default) > 0 || len(b.Fields) > 128 {
			return fmt.Errorf("invalid object binding")
		}
		for key, field := range b.Fields {
			if key == "" || len(key) > 128 {
				return fmt.Errorf("invalid object field name")
			}
			if err := validateEvaluationBinding(field, depth+1); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported binding source %q", b.Source)
	}
}

func resolveScopedBinding(b InputBinding, ctx EvaluationContext, depth int, presenceOnly bool) (any, bool, error) {
	if depth > MaxDepth {
		return nil, false, fmt.Errorf("binding nesting exceeds limit")
	}
	missing := func() (any, bool, error) {
		if presenceOnly {
			return nil, false, nil
		}
		if len(b.Default) > 0 {
			v, err := decodeJSON(b.Default, MaxJSONBytes)
			return v, false, err
		}
		if b.Optional {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("referenced input or accepted node output is missing")
	}
	switch b.Source {
	case BindingRunInput, BindingNodeOutput:
		raw := ctx.RunInput
		if b.Source == BindingNodeOutput {
			var ok bool
			raw, ok = ctx.Outputs[b.NodeKey]
			if !ok {
				return missing()
			}
		}
		value, err := decodeJSON(raw, MaxJSONBytes)
		if err != nil {
			return nil, false, err
		}
		value, found, err := optionalPointer(value, b.Path)
		if err != nil {
			return nil, false, err
		}
		if !found {
			return missing()
		}
		return value, true, nil
	case BindingIterationIndex:
		if ctx.IterationIndex < 1 || ctx.IterationIndex > MaxLoopIterations {
			return nil, false, fmt.Errorf("iteration_index is unavailable outside a valid loop iteration")
		}
		return json.Number(fmt.Sprintf("%d", ctx.IterationIndex)), true, nil
	case BindingConstant:
		value, err := decodeJSON(b.Value, MaxJSONBytes)
		return value, true, err
	case BindingObject:
		result := map[string]any{}
		for key, field := range b.Fields {
			value, _, err := resolveScopedBinding(field, ctx, depth+1, false)
			if err != nil {
				return nil, false, err
			}
			result[key] = value
		}
		return result, true, nil
	default:
		return nil, false, fmt.Errorf("unsupported binding source")
	}
}

func optionalPointer(value any, path string) (any, bool, error) {
	parts, err := pointerParts(path)
	if err != nil {
		return nil, false, err
	}
	for _, p := range parts {
		switch v := value.(type) {
		case map[string]any:
			var ok bool
			value, ok = v[p]
			if !ok {
				return nil, false, nil
			}
		case []any:
			index, err := arrayIndex(p)
			if err != nil {
				return nil, false, err
			}
			if index >= len(v) {
				return nil, false, nil
			}
			value = v[index]
		default:
			return nil, false, nil
		}
	}
	return value, true, nil
}

func EvaluateCondition(c Condition, ctx EvaluationContext) (bool, error) {
	count := 0
	if err := validateEvaluationCondition(c, 0, &count); err != nil {
		return false, err
	}
	return evaluateCondition(c, ctx)
}

func validateEvaluationCondition(c Condition, depth int, count *int) error {
	*count++
	if depth > MaxDepth || *count > 128 {
		return fmt.Errorf("condition exceeds depth or 128 predicates")
	}
	switch c.Op {
	case "all", "any", "not":
		if c.Left != nil || c.Right != nil || len(c.Conditions) < 1 || len(c.Conditions) > 32 || (c.Op == "not" && len(c.Conditions) != 1) {
			return fmt.Errorf("invalid logical operands")
		}
		for _, child := range c.Conditions {
			if err := validateEvaluationCondition(child, depth+1, count); err != nil {
				return err
			}
		}
	case "exists":
		if c.Left == nil || c.Right != nil || len(c.Conditions) > 0 || (c.Left.Source != BindingRunInput && c.Left.Source != BindingNodeOutput) {
			return fmt.Errorf("exists requires one reference")
		}
		return validateEvaluationBinding(*c.Left, 0)
	case "eq", "ne", "gt", "gte", "lt", "lte":
		if c.Left == nil || c.Right == nil || len(c.Conditions) > 0 {
			return fmt.Errorf("comparison requires two operands")
		}
		if err := validateEvaluationBinding(*c.Left, 0); err != nil {
			return err
		}
		return validateEvaluationBinding(*c.Right, 0)
	default:
		return fmt.Errorf("unsupported condition operator %q", c.Op)
	}
	return nil
}

func evaluateCondition(c Condition, ctx EvaluationContext) (bool, error) {
	switch c.Op {
	case "all", "any":
		for _, child := range c.Conditions {
			value, err := evaluateCondition(child, ctx)
			if err != nil {
				return false, err
			}
			if c.Op == "all" && !value {
				return false, nil
			}
			if c.Op == "any" && value {
				return true, nil
			}
		}
		return c.Op == "all", nil
	case "not":
		value, err := evaluateCondition(c.Conditions[0], ctx)
		return !value, err
	case "exists":
		_, found, err := resolveScopedBinding(*c.Left, ctx, 0, true)
		return found, err
	default:
		left, _, err := resolveScopedBinding(*c.Left, ctx, 0, false)
		if err != nil {
			return false, err
		}
		right, _, err := resolveScopedBinding(*c.Right, ctx, 0, false)
		if err != nil {
			return false, err
		}
		if c.Op == "eq" || c.Op == "ne" {
			equal := reflect.DeepEqual(left, right)
			if c.Op == "ne" {
				equal = !equal
			}
			return equal, nil
		}
		comparison := 0
		switch l := left.(type) {
		case json.Number:
			r, ok := right.(json.Number)
			if !ok {
				return false, fmt.Errorf("ordering operands must share the same type")
			}
			comparison = compareNumbers(l, r)
		case string:
			r, ok := right.(string)
			if !ok {
				return false, fmt.Errorf("ordering operands must share the same type")
			}
			comparison = strings.Compare(l, r)
		default:
			return false, fmt.Errorf("ordering supports only numbers and strings")
		}
		switch c.Op {
		case "gt":
			return comparison > 0, nil
		case "gte":
			return comparison >= 0, nil
		case "lt":
			return comparison < 0, nil
		case "lte":
			return comparison <= 0, nil
		default:
			return false, fmt.Errorf("unsupported operator")
		}
	}
}
