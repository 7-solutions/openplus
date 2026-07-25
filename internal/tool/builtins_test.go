package tool

import (
	"encoding/json"
	"testing"
)

// TestBuiltinsRegisterAndValidate proves every builtin satisfies the Tool port
// and exposes a valid object JSON Schema. This is the contract the agent loop's
// tool.Registry relies on (T-020).
func TestBuiltinsRegisterAndValidate(t *testing.T) {
	builtins := []Tool{
		Read{},
		Write{},
		Edit{},
		Bash{},
		Glob{Root: "."},
		Grep{Root: "."},
	}
	r := NewRegistry(builtins...)

	if len(r.All()) != len(builtins) {
		t.Fatalf("registry has %d tools, want %d", len(r.All()), len(builtins))
	}

	for _, b := range builtins {
		name := b.Name()
		if name == "" {
			t.Fatal("builtin with empty name")
		}
		if b.Description() == "" {
			t.Errorf("tool %q: empty description", name)
		}
		var schema map[string]any
		if err := json.Unmarshal(b.Schema(), &schema); err != nil {
			t.Errorf("tool %q: invalid schema JSON: %v", name, err)
			continue
		}
		if schema["type"] != "object" {
			t.Errorf("tool %q: schema type = %v, want object", name, schema["type"])
		}
		if got, ok := r.Get(name); !ok || got != b {
			t.Errorf("registry missing %q", name)
		}
	}
}
