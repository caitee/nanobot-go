package tool

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// CastParams applies safe schema-driven type casting before execution.
// Ported verbatim from internal/tools/base.go to preserve behavior.
func CastParams(params map[string]any, schema map[string]any) map[string]any {
	if schema == nil {
		return params
	}
	schemaType, _ := schema["type"].(string)
	if schemaType != "object" {
		return params
	}
	props, _ := schema["properties"].(map[string]any)
	if props == nil {
		return params
	}
	result := make(map[string]any, len(params))
	for k, v := range params {
		if propSchema, ok := props[k].(map[string]any); ok {
			result[k] = castValue(v, propSchema)
		} else {
			result[k] = v
		}
	}
	return result
}

func castValue(val any, schema map[string]any) any {
	if val == nil {
		return nil
	}
	targetType := resolveType(schema["type"])
	switch targetType {
	case "integer":
		if iv, ok := val.(int); ok {
			return iv
		}
		if fv, ok := val.(float64); ok {
			return int(fv)
		}
		if sv, ok := val.(string); ok {
			if iv, err := strconv.Atoi(sv); err == nil {
				return iv
			}
		}
	case "number":
		if nv, ok := val.(float64); ok {
			return nv
		}
		if iv, ok := val.(int); ok {
			return float64(iv)
		}
		if sv, ok := val.(string); ok {
			if nf, err := strconv.ParseFloat(sv, 64); err == nil {
				return nf
			}
		}
	case "string":
		if sv, ok := val.(string); ok {
			return sv
		}
		return fmt.Sprintf("%v", val)
	case "boolean":
		if bv, ok := val.(bool); ok {
			return bv
		}
		if sv, ok := val.(string); ok {
			sl := strings.ToLower(sv)
			if sl == "true" || sl == "1" || sl == "yes" {
				return true
			}
			if sl == "false" || sl == "0" || sl == "no" {
				return false
			}
		}
	case "array":
		if av, ok := val.([]any); ok {
			itemSchema, _ := schema["items"].(map[string]any)
			if itemSchema != nil {
				res := make([]any, len(av))
				for i, item := range av {
					res[i] = castValue(item, itemSchema)
				}
				return res
			}
			return av
		}
	case "object":
		if ov, ok := val.(map[string]any); ok {
			return CastParams(ov, schema)
		}
	}
	return val
}

func resolveType(t any) string {
	if t == nil {
		return "string"
	}
	if ts, ok := t.(string); ok {
		return ts
	}
	if tl, ok := t.([]any); ok {
		for _, item := range tl {
			if s, ok := item.(string); ok && s != "null" {
				return s
			}
		}
	}
	return "string"
}

// ValidateParams returns a flat list of validation errors. An empty slice
// means "valid".
func ValidateParams(params map[string]any, schema map[string]any) []string {
	if schema == nil {
		return nil
	}
	schemaType, _ := schema["type"].(string)
	if schemaType != "object" {
		return nil
	}
	return validateObject(params, schema, "")
}

func validateObject(params map[string]any, schema map[string]any, path string) []string {
	var errors []string
	if params == nil {
		return errors
	}
	props, _ := schema["properties"].(map[string]any)
	required, _ := schema["required"].([]any)

	for _, r := range required {
		if rn, ok := r.(string); ok {
			if _, exists := params[rn]; !exists {
				fp := rn
				if path != "" {
					fp = path + "." + rn
				}
				errors = append(errors, fmt.Sprintf("missing required field: %s", fp))
			}
		}
	}

	for k, v := range params {
		if propSchema, ok := props[k].(map[string]any); ok {
			fp := k
			if path != "" {
				fp = path + "." + k
			}
			errors = append(errors, validateValue(v, propSchema, fp)...)
		}
	}
	return errors
}

func validateValue(val any, schema map[string]any, path string) []string {
	var errors []string
	targetType := resolveType(schema["type"])
	nullable := schema["nullable"] == true

	if val == nil {
		if nullable {
			return errors
		}
		return append(errors, fmt.Sprintf("%s cannot be null", path))
	}

	switch targetType {
	case "integer":
		if _, ok := val.(int); !ok {
			if sv, ok := val.(string); ok {
				if _, err := strconv.Atoi(sv); err != nil {
					errors = append(errors, fmt.Sprintf("%s must be an integer", path))
				}
			} else {
				errors = append(errors, fmt.Sprintf("%s must be an integer", path))
			}
		}
	case "number":
		switch val.(type) {
		case int, float64:
		default:
			errors = append(errors, fmt.Sprintf("%s must be a number", path))
		}
	case "boolean":
		if _, ok := val.(bool); !ok {
			errors = append(errors, fmt.Sprintf("%s must be a boolean", path))
		}
	case "string":
		if _, ok := val.(string); !ok {
			errors = append(errors, fmt.Sprintf("%s must be a string", path))
		}
	case "array":
		av, ok := val.([]any)
		if !ok {
			return append(errors, fmt.Sprintf("%s must be an array", path))
		}
		if itemsSchema, ok := schema["items"].(map[string]any); ok {
			for i, item := range av {
				ip := fmt.Sprintf("%s[%d]", path, i)
				errors = append(errors, validateValue(item, itemsSchema, ip)...)
			}
		}
	case "object":
		ov, ok := val.(map[string]any)
		if !ok {
			return append(errors, fmt.Sprintf("%s must be an object", path))
		}
		errors = append(errors, validateObject(ov, schema, path)...)
	}

	if enum, ok := schema["enum"].([]any); ok {
		found := false
		for _, e := range enum {
			if fmt.Sprintf("%v", e) == fmt.Sprintf("%v", val) {
				found = true
				break
			}
		}
		if !found {
			errors = append(errors, fmt.Sprintf("%s must be one of %v", path, enum))
		}
	}

	if targetType == "integer" || targetType == "number" {
		if v, ok := toFloat64(val); ok {
			if min, ok := schema["minimum"]; ok {
				if m, ok := toFloat64(min); ok && v < m {
					errors = append(errors, fmt.Sprintf("%s must be >= %v", path, m))
				}
			}
			if max, ok := schema["maximum"]; ok {
				if m, ok := toFloat64(max); ok && v > m {
					errors = append(errors, fmt.Sprintf("%s must be <= %v", path, m))
				}
			}
		}
	}

	if targetType == "string" {
		if sv, ok := val.(string); ok {
			if minLen, ok := schema["minLength"].(int); ok && len(sv) < minLen {
				errors = append(errors, fmt.Sprintf("%s must be at least %d characters", path, minLen))
			}
			if maxLen, ok := schema["maxLength"].(int); ok && len(sv) > maxLen {
				errors = append(errors, fmt.Sprintf("%s must be at most %d characters", path, maxLen))
			}
			if pattern, ok := schema["pattern"].(string); ok {
				if re, err := regexp.Compile(pattern); err == nil && !re.MatchString(sv) {
					errors = append(errors, fmt.Sprintf("%s does not match pattern %s", path, pattern))
				}
			}
		}
	}

	return errors
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case float64:
		return n, true
	case string:
		if f, err := strconv.ParseFloat(n, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}
