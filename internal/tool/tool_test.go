package tool

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestCastParamsPrimitives(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"n": map[string]any{"type": "integer"},
			"f": map[string]any{"type": "number"},
			"s": map[string]any{"type": "string"},
			"b": map[string]any{"type": "boolean"},
		},
	}
	in := map[string]any{
		"n": "42",
		"f": "3.14",
		"s": 7,
		"b": "yes",
	}
	out := CastParams(in, schema)
	if out["n"] != 42 {
		t.Errorf("n = %v", out["n"])
	}
	if out["f"] != 3.14 {
		t.Errorf("f = %v", out["f"])
	}
	if out["s"] != "7" {
		t.Errorf("s = %v", out["s"])
	}
	if out["b"] != true {
		t.Errorf("b = %v", out["b"])
	}
}

func TestCastParamsNestedArray(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"xs": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "integer"},
			},
		},
	}
	in := map[string]any{"xs": []any{"1", "2", "3"}}
	out := CastParams(in, schema)
	xs := out["xs"].([]any)
	for i, want := range []int{1, 2, 3} {
		if xs[i] != want {
			t.Errorf("xs[%d] = %v; want %d", i, xs[i], want)
		}
	}
}

func TestValidateParamsRequired(t *testing.T) {
	schema := map[string]any{
		"type":     "object",
		"required": []any{"a", "b"},
		"properties": map[string]any{
			"a": map[string]any{"type": "string"},
			"b": map[string]any{"type": "integer"},
		},
	}
	errs := ValidateParams(map[string]any{"a": "ok"}, schema)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %v", errs)
	}
}

func TestValidateParamsEnum(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"mode": map[string]any{"type": "string", "enum": []any{"a", "b"}},
		},
	}
	if errs := ValidateParams(map[string]any{"mode": "c"}, schema); len(errs) != 1 {
		t.Fatalf("expected enum violation, got %v", errs)
	}
	if errs := ValidateParams(map[string]any{"mode": "a"}, schema); len(errs) != 0 {
		t.Fatalf("expected no error, got %v", errs)
	}
}

func TestValidateParamsRange(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"n": map[string]any{"type": "integer", "minimum": 0, "maximum": 10},
		},
	}
	if errs := ValidateParams(map[string]any{"n": -1}, schema); len(errs) != 1 {
		t.Fatalf("expected minimum violation")
	}
	if errs := ValidateParams(map[string]any{"n": 11}, schema); len(errs) != 1 {
		t.Fatalf("expected maximum violation")
	}
	if errs := ValidateParams(map[string]any{"n": 5}, schema); len(errs) != 0 {
		t.Fatalf("expected valid, got %v", errs)
	}
}

// --- registry ---

type fakeLegacy struct {
	name string
	fn   func(ctx context.Context, p map[string]any) (any, error)
}

func (f *fakeLegacy) Name() string                 { return f.name }
func (f *fakeLegacy) Description() string          { return "fake " + f.name }
func (f *fakeLegacy) Parameters() map[string]any   { return map[string]any{"type": "object"} }
func (f *fakeLegacy) Execute(ctx context.Context, p map[string]any) (any, error) {
	return f.fn(ctx, p)
}

func TestRegistryConcurrency(t *testing.T) {
	r := NewRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := "t" + string(rune('a'+i%26))
			r.Register(FromLegacy(&fakeLegacy{
				name: name,
				fn:   func(ctx context.Context, p map[string]any) (any, error) { return "ok", nil },
			}, ""))
		}(i)
	}
	wg.Wait()
	if len(r.All()) == 0 {
		t.Fatalf("no tools registered")
	}
}

func TestRegistryExecuteValidatesAndCasts(t *testing.T) {
	schema := map[string]any{
		"type":     "object",
		"required": []any{"n"},
		"properties": map[string]any{
			"n": map[string]any{"type": "integer"},
		},
	}
	received := map[string]any{}
	r := NewRegistry()
	r.Register(&syntheticTool{
		name:   "sum",
		params: schema,
		exec: func(args map[string]any) (*Result, error) {
			received = args
			return &Result{}, nil
		},
	})

	// String "42" must be cast to int 42 before reaching the tool.
	if _, err := r.Execute(context.Background(), "sum", "1", map[string]any{"n": "42"}, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if received["n"] != 42 {
		t.Fatalf("n not cast: %v (%T)", received["n"], received["n"])
	}

	// Missing required field.
	if _, err := r.Execute(context.Background(), "sum", "2", map[string]any{}, nil); err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestRegistryExecuteMissingTool(t *testing.T) {
	r := NewRegistry()
	_, err := r.Execute(context.Background(), "nope", "x", nil, nil)
	if err == nil {
		t.Fatalf("expected missing-tool error")
	}
}

func TestLegacyAdapterPassesErrors(t *testing.T) {
	adapter := FromLegacy(&fakeLegacy{
		name: "fail",
		fn: func(ctx context.Context, p map[string]any) (any, error) {
			return nil, errors.New("boom")
		},
	}, "")
	_, err := adapter.Execute(context.Background(), "id", nil, nil)
	if err == nil || err.Error() != "boom" {
		t.Fatalf("unexpected err: %v", err)
	}
}

// syntheticTool is a minimal AgentTool used by registry tests without the
// adapter layer.
type syntheticTool struct {
	name   string
	params map[string]any
	exec   func(args map[string]any) (*Result, error)
}

func (s *syntheticTool) Name() string                 { return s.name }
func (s *syntheticTool) Label() string                { return s.name }
func (s *syntheticTool) Description() string          { return s.name }
func (s *syntheticTool) Parameters() map[string]any   { return s.params }
func (s *syntheticTool) ExecutionMode() ExecutionMode { return ExecutionDefault }
func (s *syntheticTool) PrepareArguments(r map[string]any) (map[string]any, error) {
	return r, nil
}
func (s *syntheticTool) Execute(ctx context.Context, id string, args map[string]any, u UpdateFn) (*Result, error) {
	return s.exec(args)
}
