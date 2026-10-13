package graph

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

func decodeJSON(data []byte, limit int) (any, error) {
	if len(data) == 0 || len(data) > limit {
		return nil, invalid("json", "empty value or size limit exceeded")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	v, err := decodeValue(d, 0)
	if err != nil {
		return nil, invalid("json", err.Error())
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, invalid("json", "unexpected trailing content")
	}
	return v, nil
}

func decodeValue(d *json.Decoder, depth int) (any, error) {
	if depth > MaxDepth {
		return nil, fmt.Errorf("nesting exceeds %d", MaxDepth)
	}
	t, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch v := t.(type) {
	case json.Delim:
		switch v {
		case '{':
			m := map[string]any{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return nil, err
				}
				k, ok := key.(string)
				if !ok {
					return nil, fmt.Errorf("invalid object key")
				}
				if _, ok := m[k]; ok {
					return nil, fmt.Errorf("duplicate object field %q", k)
				}
				value, err := decodeValue(d, depth+1)
				if err != nil {
					return nil, err
				}
				m[k] = value
			}
			if _, err := d.Token(); err != nil {
				return nil, err
			}
			return m, nil
		case '[':
			a := []any{}
			for d.More() {
				value, err := decodeValue(d, depth+1)
				if err != nil {
					return nil, err
				}
				a = append(a, value)
			}
			if _, err := d.Token(); err != nil {
				return nil, err
			}
			return a, nil
		default:
			return nil, fmt.Errorf("unexpected delimiter")
		}
	case json.Number:
		return normalizeNumber(string(v))
	default:
		return v, nil
	}
}

// normalizeNumber preserves exact decimal values without float64 rounding.
func normalizeNumber(s string) (json.Number, error) {
	negative := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	exponent := 0
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		v, err := strconv.Atoi(s[i+1:])
		if err != nil || v < -1024 || v > 1024 {
			return "", fmt.Errorf("number exponent exceeds supported range")
		}
		exponent = v
		s = s[:i]
	}
	point := len(s)
	if i := strings.IndexByte(s, '.'); i >= 0 {
		point = i
		s = s[:i] + s[i+1:]
	}
	point += exponent
	for len(s) > 0 && s[0] == '0' {
		s = s[1:]
		point--
	}
	if s == "" {
		return json.Number("0"), nil
	}
	s = strings.TrimRight(s, "0")
	if len(s) > 2048 {
		return "", fmt.Errorf("number exceeds supported precision")
	}
	switch {
	case point <= 0:
		s = "0." + strings.Repeat("0", -point) + s
	case point >= len(s):
		s += strings.Repeat("0", point-len(s))
	default:
		s = s[:point] + "." + s[point:]
	}
	if negative {
		s = "-" + s
	}
	return json.Number(s), nil
}

// CanonicalJSON normalizes object order, whitespace, and exact decimal spelling.
func CanonicalJSON(data json.RawMessage) ([]byte, string, error) {
	v, err := decodeJSON(data, MaxJSONBytes)
	if err != nil {
		return nil, "", err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, "", err
	}
	if len(b) > MaxJSONBytes {
		return nil, "", invalid("json", "canonical value exceeds size limit")
	}
	h := sha256.Sum256(b)
	return b, hex.EncodeToString(h[:]), nil
}

func canonicalValue(value any) ([]byte, string, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return nil, "", err
	}
	return CanonicalJSON(b)
}

func pointerParts(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("path must be a JSON Pointer")
	}
	parts := strings.Split(path[1:], "/")
	if len(parts) > MaxDepth {
		return nil, fmt.Errorf("JSON Pointer exceeds depth limit")
	}
	for i, p := range parts {
		var b strings.Builder
		for j := 0; j < len(p); j++ {
			if p[j] != '~' {
				b.WriteByte(p[j])
				continue
			}
			j++
			if j >= len(p) || (p[j] != '0' && p[j] != '1') {
				return nil, fmt.Errorf("invalid JSON Pointer escape")
			}
			if p[j] == '0' {
				b.WriteByte('~')
			} else {
				b.WriteByte('/')
			}
		}
		parts[i] = b.String()
	}
	return parts, nil
}

func arrayIndex(segment string) (int, error) {
	if segment == "" || (len(segment) > 1 && segment[0] == '0') {
		return 0, fmt.Errorf("invalid array index")
	}
	for _, r := range segment {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("invalid array index")
		}
	}
	index, err := strconv.Atoi(segment)
	if err != nil {
		return 0, fmt.Errorf("invalid array index")
	}
	return index, nil
}

func valueAtPointer(value any, path string) (any, error) {
	parts, err := pointerParts(path)
	if err != nil {
		return nil, err
	}
	for _, p := range parts {
		switch v := value.(type) {
		case map[string]any:
			var ok bool
			value, ok = v[p]
			if !ok {
				return nil, fmt.Errorf("field %q is missing", p)
			}
		case []any:
			i, err := arrayIndex(p)
			if err != nil || i >= len(v) {
				return nil, fmt.Errorf("array index %q is unavailable", p)
			}
			value = v[i]
		default:
			return nil, fmt.Errorf("cannot traverse scalar value")
		}
	}
	return value, nil
}

func ResolveInputs(node Node, runInput json.RawMessage, outputs map[string]json.RawMessage) (json.RawMessage, error) {
	input, err := decodeJSON(runInput, MaxJSONBytes)
	if err != nil {
		return nil, err
	}
	resolved := make(map[string]any, len(node.Inputs))
	for field, b := range node.Inputs {
		value, err := resolveBinding(b, input, outputs, 0)
		if err != nil {
			return nil, invalid("inputs."+field, err.Error())
		}
		resolved[field] = value
	}
	raw, _, err := canonicalValue(resolved)
	return raw, err
}

func resolveBinding(b InputBinding, input any, outputs map[string]json.RawMessage, depth int) (any, error) {
	if depth > MaxDepth {
		return nil, fmt.Errorf("input binding depth exceeded")
	}
	switch b.Source {
	case BindingRunInput:
		return valueAtPointer(input, b.Path)
	case BindingNodeOutput:
		raw, ok := outputs[b.NodeKey]
		if !ok {
			return nil, fmt.Errorf("accepted output for %q is missing", b.NodeKey)
		}
		value, err := decodeJSON(raw, MaxJSONBytes)
		if err != nil {
			return nil, err
		}
		return valueAtPointer(value, b.Path)
	case BindingConstant:
		return decodeJSON(b.Value, MaxJSONBytes)
	case BindingObject:
		result := map[string]any{}
		if len(b.Fields) > 128 {
			return nil, fmt.Errorf("object exceeds 128 fields")
		}
		for k, v := range b.Fields {
			value, err := resolveBinding(v, input, outputs, depth+1)
			if err != nil {
				return nil, err
			}
			result[k] = value
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unsupported input source")
	}
}
