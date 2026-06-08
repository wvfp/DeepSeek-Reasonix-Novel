package tools

import (
	"fmt"
	"strconv"
	"strings"
)

// stringField pulls a string out of a map[string]any input. Numeric
// fields that the LLM occasionally emits as JSON numbers are coerced
// to string so the rest of the pipeline can treat them uniformly.
func stringField(input map[string]any, key string) string {
	v, ok := input[key]
	if !ok || v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return strings.TrimSpace(s)
	case float64:
		return strconv.FormatFloat(s, 'f', -1, 64)
	case int:
		return strconv.Itoa(s)
	case int64:
		return strconv.FormatInt(s, 10)
	case bool:
		return strconv.FormatBool(s)
	default:
		return fmt.Sprint(v)
	}
}

// intField returns the value at key coerced to int. Missing or
// non-numeric → def. The LLM sometimes emits integers as JSON numbers
// (float64) so the type switch covers both.
func intField(input map[string]any, key string, def int) int {
	v, ok := input[key]
	if !ok || v == nil {
		return def
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case string:
		s := strings.TrimSpace(n)
		if s == "" {
			return def
		}
		if x, err := strconv.Atoi(s); err == nil {
			return x
		}
	}
	return def
}

// stringListField returns the value at key as a []string. Accepts
// both []any (the JSON shape) and []string. Missing → nil.
func stringListField(input map[string]any, key string) []string {
	v, ok := input[key]
	if !ok || v == nil {
		return nil
	}
	switch xs := v.(type) {
	case []string:
		out := make([]string, len(xs))
		copy(out, xs)
		return out
	case []any:
		out := make([]string, 0, len(xs))
		for _, e := range xs {
			out = append(out, stringField(map[string]any{"x": e}, "x"))
		}
		return out
	}
	return nil
}

// nestedMapField returns the value at key as a map[string]any. JSON
// unmarshalling produces map[string]any, but tests sometimes pass
// concrete structs; we coerce the common cases.
func nestedMapField(input map[string]any, key string) map[string]any {
	v, ok := input[key]
	if !ok || v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

// requiredString is stringField with a mandatory-value variant.
// Returns ("", err) when the field is absent or empty.
func requiredString(input map[string]any, key string) (string, error) {
	v := stringField(input, key)
	if v == "" {
		return "", fmt.Errorf("input field %q is required", key)
	}
	return v, nil
}
