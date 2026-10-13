package graph

import (
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"unicode/utf8"
)

var schemaKeywords = map[string]bool{"type": true, "properties": true, "required": true, "additionalProperties": true, "items": true, "enum": true, "minimum": true, "maximum": true, "minLength": true, "maxLength": true, "minItems": true, "maxItems": true, "title": true, "description": true}

func parseSchema(raw json.RawMessage, objectRoot bool) (map[string]any, error) {
	v, err := decodeJSON(raw, MaxSchemaBytes)
	if err != nil {
		return nil, err
	}
	schema, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("schema must be an object")
	}
	if err := validateSchema(schema, 0); err != nil {
		return nil, err
	}
	if objectRoot && schema["type"] != "object" {
		return nil, fmt.Errorf("root schema type must be object")
	}
	return schema, nil
}

func validateSchema(s map[string]any, depth int) error {
	if depth > MaxDepth {
		return fmt.Errorf("schema nesting exceeds limit")
	}
	for key := range s {
		if !schemaKeywords[key] {
			return fmt.Errorf("unsupported schema keyword %q", key)
		}
	}
	kind, ok := s["type"].(string)
	if !ok {
		return fmt.Errorf("schema type is required")
	}
	switch kind {
	case "object", "array", "string", "integer", "number", "boolean", "null":
	default:
		return fmt.Errorf("unsupported schema type %q", kind)
	}
	for _, key := range []string{"title", "description"} {
		if v, ok := s[key]; ok {
			if _, ok := v.(string); !ok {
				return fmt.Errorf("%s must be a string", key)
			}
		}
	}
	for _, key := range []string{"properties", "required", "additionalProperties"} {
		if _, ok := s[key]; ok && kind != "object" {
			return fmt.Errorf("%s is only valid for objects", key)
		}
	}
	for _, key := range []string{"items", "minItems", "maxItems"} {
		if _, ok := s[key]; ok && kind != "array" {
			return fmt.Errorf("%s is only valid for arrays", key)
		}
	}
	for _, key := range []string{"minLength", "maxLength"} {
		if _, ok := s[key]; ok && kind != "string" {
			return fmt.Errorf("%s is only valid for strings", key)
		}
	}
	for _, key := range []string{"minimum", "maximum"} {
		if v, ok := s[key]; ok {
			if kind != "integer" && kind != "number" {
				return fmt.Errorf("%s is only valid for numbers", key)
			}
			if _, ok := v.(json.Number); !ok {
				return fmt.Errorf("%s must be a number", key)
			}
		}
	}
	for _, key := range []string{"minLength", "maxLength", "minItems", "maxItems"} {
		if v, ok := s[key]; ok {
			if _, err := nonnegativeInt(v); err != nil {
				return fmt.Errorf("%s must be a bounded nonnegative integer", key)
			}
		}
	}
	for _, pair := range [][2]string{{"minimum", "maximum"}, {"minLength", "maxLength"}, {"minItems", "maxItems"}} {
		lo, lok := s[pair[0]]
		hi, hok := s[pair[1]]
		if lok && hok && compareNumbers(lo.(json.Number), hi.(json.Number)) > 0 {
			return fmt.Errorf("%s exceeds %s", pair[0], pair[1])
		}
	}
	if kind == "object" {
		props := map[string]any{}
		if v, ok := s["properties"]; ok {
			var valid bool
			props, valid = v.(map[string]any)
			if !valid || len(props) > 128 {
				return fmt.Errorf("properties must be an object with at most 128 fields")
			}
		}
		for key, value := range props {
			child, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("property %q must be a schema object", key)
			}
			if err := validateSchema(child, depth+1); err != nil {
				return fmt.Errorf("property %q: %w", key, err)
			}
		}
		if v, ok := s["additionalProperties"]; ok {
			if _, ok := v.(bool); !ok {
				return fmt.Errorf("additionalProperties must be a boolean")
			}
		}
		if v, ok := s["required"]; ok {
			required, ok := v.([]any)
			if !ok {
				return fmt.Errorf("required must be an array")
			}
			seen := map[string]bool{}
			for _, field := range required {
				name, ok := field.(string)
				if !ok || seen[name] {
					return fmt.Errorf("required must contain unique property names")
				}
				if _, ok := props[name]; !ok {
					return fmt.Errorf("required field %q is not declared", name)
				}
				seen[name] = true
			}
		}
	}
	if kind == "array" {
		items, ok := s["items"].(map[string]any)
		if !ok {
			return fmt.Errorf("arrays require one items schema")
		}
		if err := validateSchema(items, depth+1); err != nil {
			return err
		}
	}
	if v, ok := s["enum"]; ok {
		values, ok := v.([]any)
		if !ok || len(values) == 0 || len(values) > 128 {
			return fmt.Errorf("enum requires 1 to 128 values")
		}
		withoutEnum := map[string]any{}
		for k, v := range s {
			if k != "enum" {
				withoutEnum[k] = v
			}
		}
		for i, value := range values {
			if err := validateValue(withoutEnum, value, "enum", depth+1); err != nil {
				return err
			}
			for j := 0; j < i; j++ {
				if reflect.DeepEqual(value, values[j]) {
					return fmt.Errorf("enum values must be unique")
				}
			}
		}
	}
	return nil
}

func nonnegativeInt(value any) (int, error) {
	n, ok := value.(json.Number)
	if !ok {
		return 0, fmt.Errorf("not a number")
	}
	r, ok := new(big.Rat).SetString(string(n))
	if !ok || !r.IsInt() || r.Sign() < 0 || !r.Num().IsInt64() {
		return 0, fmt.Errorf("not a bounded nonnegative integer")
	}
	v := r.Num().Int64()
	if v > MaxJSONBytes {
		return 0, fmt.Errorf("exceeds value size limit")
	}
	return int(v), nil
}

func compareNumbers(a, b json.Number) int {
	ar, _ := new(big.Rat).SetString(string(a))
	br, _ := new(big.Rat).SetString(string(b))
	return ar.Cmp(br)
}

// ValidateJSON supports only the explicitly bounded local schema subset.
func ValidateJSON(schema, data json.RawMessage) error {
	s, err := parseSchema(schema, false)
	if err != nil {
		return invalid("schema", err.Error())
	}
	v, err := decodeJSON(data, MaxJSONBytes)
	if err != nil {
		return err
	}
	return validateValue(s, v, "$", 0)
}

func validateValue(s map[string]any, value any, path string, depth int) error {
	if depth > MaxDepth {
		return invalid(path, "value nesting exceeds limit")
	}
	if choices, ok := s["enum"].([]any); ok {
		found := false
		for _, choice := range choices {
			if reflect.DeepEqual(choice, value) {
				found = true
				break
			}
		}
		if !found {
			return invalid(path, "value is not in enum")
		}
	}
	switch s["type"] {
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return invalid(path, "must be an object")
		}
		props, _ := s["properties"].(map[string]any)
		required, _ := s["required"].([]any)
		for _, field := range required {
			if _, ok := object[field.(string)]; !ok {
				return invalid(path+"."+field.(string), "is required")
			}
		}
		for key, v := range object {
			schema, declared := props[key]
			if !declared {
				if s["additionalProperties"] == false {
					return invalid(path+"."+key, "unexpected property")
				}
				continue
			}
			if err := validateValue(schema.(map[string]any), v, path+"."+key, depth+1); err != nil {
				return err
			}
		}
	case "array":
		array, ok := value.([]any)
		if !ok {
			return invalid(path, "must be an array")
		}
		if err := validateLength(s, len(array), "minItems", "maxItems", path); err != nil {
			return err
		}
		for i, v := range array {
			if err := validateValue(s["items"].(map[string]any), v, fmt.Sprintf("%s[%d]", path, i), depth+1); err != nil {
				return err
			}
		}
	case "string":
		v, ok := value.(string)
		if !ok {
			return invalid(path, "must be a string")
		}
		return validateLength(s, utf8.RuneCountInString(v), "minLength", "maxLength", path)
	case "number", "integer":
		n, ok := value.(json.Number)
		if !ok {
			return invalid(path, "must be a number")
		}
		r, _ := new(big.Rat).SetString(string(n))
		if s["type"] == "integer" && !r.IsInt() {
			return invalid(path, "must be an integer")
		}
		if min, ok := s["minimum"].(json.Number); ok && compareNumbers(n, min) < 0 {
			return invalid(path, "below minimum")
		}
		if max, ok := s["maximum"].(json.Number); ok && compareNumbers(n, max) > 0 {
			return invalid(path, "above maximum")
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return invalid(path, "must be a boolean")
		}
	case "null":
		if value != nil {
			return invalid(path, "must be null")
		}
	default:
		return invalid(path, "unsupported schema type")
	}
	return nil
}

func validateLength(s map[string]any, length int, minKey, maxKey, path string) error {
	if v, ok := s[minKey]; ok {
		min, err := nonnegativeInt(v)
		if err != nil {
			return err
		}
		if length < min {
			return invalid(path, "below "+minKey)
		}
	}
	if v, ok := s[maxKey]; ok {
		max, err := nonnegativeInt(v)
		if err != nil {
			return err
		}
		if length > max {
			return invalid(path, "above "+maxKey)
		}
	}
	return nil
}

func schemaAtPointer(schema map[string]any, path string) (map[string]any, error) {
	parts, err := pointerParts(path)
	if err != nil {
		return nil, err
	}
	for _, part := range parts {
		switch schema["type"] {
		case "object":
			props, _ := schema["properties"].(map[string]any)
			child, ok := props[part].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("field %q is not declared in source schema", part)
			}
			schema = child
		case "array":
			if _, err := arrayIndex(part); err != nil {
				return nil, err
			}
			schema = schema["items"].(map[string]any)
		default:
			return nil, fmt.Errorf("cannot traverse scalar schema")
		}
	}
	return schema, nil
}
