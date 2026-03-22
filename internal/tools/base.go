package tools

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Tool defines the interface for agent tools
type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]any // JSON Schema
	Execute(ctx context.Context, params map[string]any) (any, error)
}

// BaseTool provides common functionality for all tools
type BaseTool struct{}

// CastParams applies safe schema-driven type casting before execution
func (b *BaseTool) CastParams(params map[string]any, schema map[string]any) map[string]any {
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

	result := make(map[string]any)
	for k, v := range params {
		if propSchema, ok := props[k].(map[string]any); ok {
			result[k] = b.castValue(v, propSchema)
		} else {
			result[k] = v
		}
	}
	return result
}

func (b *BaseTool) castValue(val any, schema map[string]any) any {
	if val == nil {
		return nil
	}

	targetType := b.resolveType(schema["type"])

	switch targetType {
	case "integer":
		if iv, ok := val.(int); ok {
			return iv
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
				result := make([]any, len(av))
				for i, item := range av {
					result[i] = b.castValue(item, itemSchema)
				}
				return result
			}
			return av
		}
	case "object":
		if ov, ok := val.(map[string]any); ok {
			return b.CastParams(ov, schema)
		}
	}
	return val
}

func (b *BaseTool) resolveType(t any) string {
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

// ValidateParams validates tool parameters against JSON schema
func (b *BaseTool) ValidateParams(params map[string]any, schema map[string]any) []string {
	if schema == nil {
		return nil
	}

	schemaType, _ := schema["type"].(string)
	if schemaType == "" {
		return nil
	}

	// Handle nullable
	if tl, ok := schema["type"].([]any); ok {
		hasNull := false
		nonNull := ""
		for _, t := range tl {
			if ts, ok := t.(string); ok {
				if ts == "null" {
					hasNull = true
				} else {
					nonNull = ts
				}
			}
		}
		if hasNull && nonNull != "" {
			if params == nil {
				return nil
			}
		}
	}

	if schemaType != "object" {
		return nil
	}

	return b.validateObject(params, schema, "")
}

func (b *BaseTool) validateObject(params map[string]any, schema map[string]any, path string) []string {
	var errors []string

	if params == nil {
		return errors
	}

	props, _ := schema["properties"].(map[string]any)
	required, _ := schema["required"].([]any)

	// Check required fields
	for _, r := range required {
		if rn, ok := r.(string); ok {
			if _, exists := params[rn]; !exists {
				fieldPath := rn
				if path != "" {
					fieldPath = path + "." + rn
				}
				errors = append(errors, fmt.Sprintf("missing required field: %s", fieldPath))
			}
		}
	}

	// Validate each param
	for k, v := range params {
		if propSchema, ok := props[k].(map[string]any); ok {
			fieldPath := k
			if path != "" {
				fieldPath = path + "." + k
			}
			errors = append(errors, b.validateValue(v, propSchema, fieldPath)...)
		}
	}

	return errors
}

func (b *BaseTool) validateValue(val any, schema map[string]any, path string) []string {
	var errors []string

	targetType := b.resolveType(schema["type"])
	nullable := schema["nullable"] == true

	// Handle null/nullable
	if val == nil {
		if nullable {
			return errors
		}
		errors = append(errors, fmt.Sprintf("%s cannot be null", path))
		return errors
	}

	// Type-specific validation
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
			// OK
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
			errors = append(errors, fmt.Sprintf("%s must be an array", path))
			return errors
		}
		if itemsSchema, ok := schema["items"].(map[string]any); ok {
			for i, item := range av {
				itemPath := fmt.Sprintf("%s[%d]", path, i)
				errors = append(errors, b.validateValue(item, itemsSchema, itemPath)...)
			}
		}
	case "object":
		ov, ok := val.(map[string]any)
		if !ok {
			errors = append(errors, fmt.Sprintf("%s must be an object", path))
			return errors
		}
		errors = append(errors, b.validateObject(ov, schema, path)...)
	}

	// Enum validation
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

	// Range validations
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

	// String length validations
	if targetType == "string" {
		if sv, ok := val.(string); ok {
			if minLen, ok := schema["minLength"].(int); ok && len(sv) < minLen {
				errors = append(errors, fmt.Sprintf("%s must be at least %d characters", path, minLen))
			}
			if maxLen, ok := schema["maxLength"].(int); ok && len(sv) > maxLen {
				errors = append(errors, fmt.Sprintf("%s must be at most %d characters", path, maxLen))
			}
			// Pattern validation
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

// ToolRegistry manages tool registration and execution
type ToolRegistry interface {
	Register(tool Tool)
	Unregister(name string)
	Get(name string) Tool
	Has(name string) bool
	GetDefinitions() []map[string]any
	Execute(ctx context.Context, name string, params map[string]any) (any, error)
}

type toolRegistry struct {
	tools map[string]Tool
	mu    sync.RWMutex
}

// NewRegistry creates a new tool registry
func NewRegistry() ToolRegistry {
	return &toolRegistry{
		tools: make(map[string]Tool),
	}
}

func (r *toolRegistry) Register(tool Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[tool.Name()] = tool
}

func (r *toolRegistry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tools, name)
}

func (r *toolRegistry) Get(name string) Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tools[name]
}

func (r *toolRegistry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.tools[name]
	return ok
}

func (r *toolRegistry) GetDefinitions() []map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]map[string]any, 0, len(r.tools))
	for _, tool := range r.tools {
		defs = append(defs, map[string]any{
			"name":        tool.Name(),
			"description": tool.Description(),
			"parameters":  tool.Parameters(),
		})
	}
	return defs
}

func (r *toolRegistry) Execute(ctx context.Context, name string, params map[string]any) (any, error) {
	r.mu.Lock()
	tool, ok := r.tools[name]
	if !ok {
		r.mu.Unlock()
		return nil, fmt.Errorf("tool not found: %s", name)
	}
	// Keep lock held during execution to prevent tool from being unregistered
	r.mu.Unlock()

	// Apply type casting
	params = (&BaseTool{}).CastParams(params, tool.Parameters())

	// Validate params
	if errs := (&BaseTool{}).ValidateParams(params, tool.Parameters()); len(errs) > 0 {
		return nil, fmt.Errorf("validation failed: %s", strings.Join(errs, "; "))
	}

	result, err := tool.Execute(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}

	return result, nil
}