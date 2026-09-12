// Package exttest validates a plugin against the ABI.
//
// Plugin authors: add one test and you are done.
//
//	func TestConformance(t *testing.T) {
//		exttest.Conformance(t, myplugin.New(deps))
//	}
//
// It checks everything the host will check at load time, plus the failure
// modes that only show up in front of an audience: garbage arguments, unknown
// tool names, double Close.
package exttest

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sirius/cogdebt/internal/ext"
)

// Case is a real call to exercise, supplied by the plugin author.
type Case struct {
	// Tool is the unqualified tool name, as declared in the manifest.
	Tool string
	// Args is the arguments object.
	Args any
	// WantFault, if set, requires the call to fail with this Fault.Code.
	WantFault string
	// Check inspects a successful result. Optional.
	Check func(t *testing.T, result json.RawMessage)
}

// Conformance runs every ABI check that needs no plugin-specific knowledge.
// It does not invoke tools that could have side effects; pass Cases to
// Calls for that.
func Conformance(t *testing.T, e ext.Extension) {
	t.Helper()
	ctx := t.Context()
	m := e.Manifest()

	t.Run("manifest_valid", func(t *testing.T) {
		if err := ext.ValidateManifest(m); err != nil {
			t.Fatalf("manifest would be rejected by the host: %v", err)
		}
	})

	t.Run("manifest_stable", func(t *testing.T) {
		// The host may call Manifest on every turn to rebuild the tool list,
		// so it has to be pure and cheap.
		a, _ := json.Marshal(e.Manifest())
		b, _ := json.Marshal(e.Manifest())
		if string(a) != string(b) {
			t.Fatalf("Manifest() is not stable across calls:\nfirst:  %s\nsecond: %s", a, b)
		}
	})

	t.Run("schemas_convert", func(t *testing.T) {
		for _, spec := range m.Provides {
			schema, err := ext.SchemaFromJSON(spec.Schema)
			if err != nil {
				t.Errorf("tool %q: schema does not convert: %v", spec.Name, err)
				continue
			}
			if schema.Type != "OBJECT" {
				t.Errorf("tool %q: converted schema type is %q, want OBJECT", spec.Name, schema.Type)
			}
		}
	})

	t.Run("unknown_tool_faults", func(t *testing.T) {
		_, err := e.Invoke(ctx, "definitely_not_a_tool", json.RawMessage(`{}`))
		if err == nil {
			t.Fatal("Invoke with an unknown tool name returned no error; it must return a Fault")
		}
		assertFault(t, err)
	})

	t.Run("rejects_missing_required", func(t *testing.T) {
		// Syntactically valid JSON is the host's guarantee, so that is not
		// tested here. What a plugin owns is the schema: a call missing a
		// required field must fault before the tool does any work.
		for _, spec := range m.Provides {
			required := requiredFields(spec.Schema)
			if len(required) == 0 {
				continue // a no-argument tool has nothing to reject
			}
			_, err := e.Invoke(ctx, spec.Name, json.RawMessage(`{}`))
			if err == nil {
				t.Errorf("tool %q accepted {} despite requiring %v; it must return a Fault", spec.Name, required)
				continue
			}
			assertFault(t, err)
		}
	})

	t.Run("wrong_types_fault_cleanly", func(t *testing.T) {
		// Wrong types must surface as a Fault, never as a raw decoder error:
		// the model only gets to correct itself if the message is written for
		// it. ext.Args does this conversion.
		for _, spec := range m.Provides {
			args := wrongTypedArgs(spec.Schema)
			if len(args) == 0 {
				continue
			}
			raw, _ := json.Marshal(args)
			out, err := e.Invoke(ctx, spec.Name, raw)
			if err != nil {
				assertFault(t, err)
				continue
			}
			if !json.Valid(out) {
				t.Errorf("tool %q returned invalid JSON for mistyped arguments", spec.Name)
			}
		}
	})

	t.Run("close_is_idempotent", func(t *testing.T) {
		if err := e.Close(); err != nil {
			t.Fatalf("first Close: %v", err)
		}
		if err := e.Close(); err != nil {
			t.Fatalf("second Close must be safe, got: %v", err)
		}
	})
}

// Calls exercises real invocations. Run it before Conformance, since
// Conformance closes the plugin.
func Calls(t *testing.T, e ext.Extension, cases ...Case) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.Tool, func(t *testing.T) {
			args, err := json.Marshal(c.Args)
			if err != nil {
				t.Fatalf("encode args: %v", err)
			}
			out, err := e.Invoke(t.Context(), c.Tool, args)

			if c.WantFault != "" {
				if err == nil {
					t.Fatalf("want fault %q, got success: %s", c.WantFault, out)
				}
				f := assertFault(t, err)
				if f.Code != c.WantFault {
					t.Fatalf("want fault %q, got %q (%s)", c.WantFault, f.Code, f.Message)
				}
				return
			}
			if err != nil {
				t.Fatalf("Invoke: %v", err)
			}
			if !json.Valid(out) {
				t.Fatalf("result is not valid JSON: %q", out)
			}
			if c.Check != nil {
				c.Check(t, out)
			}
		})
	}
}

// requiredFields lists the schema's required property names.
func requiredFields(schema json.RawMessage) []string {
	var s struct {
		Required []string `json:"required"`
	}
	_ = json.Unmarshal(schema, &s)
	return s.Required
}

// wrongTypedArgs builds an arguments object where every declared property
// carries a deliberately wrong type.
func wrongTypedArgs(schema json.RawMessage) map[string]any {
	var s struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(schema, &s); err != nil {
		return nil
	}
	out := make(map[string]any, len(s.Properties))
	for name, prop := range s.Properties {
		switch prop.Type {
		case "string":
			out[name] = 12345
		case "number", "integer":
			out[name] = "not-a-number"
		case "array":
			out[name] = "not-an-array"
		case "boolean":
			out[name] = "not-a-boolean"
		case "object":
			out[name] = "not-an-object"
		}
	}
	return out
}

// assertFault requires err to be a *ext.Fault carrying a message the model can
// act on.
func assertFault(t *testing.T, err error) *ext.Fault {
	t.Helper()
	f, ok := err.(*ext.Fault)
	if !ok {
		t.Fatalf("error %v (%T) is not a *ext.Fault; expected failures must cross the ABI as data", err, err)
	}
	if f.Code == "" {
		t.Fatal("Fault.Code is empty")
	}
	if f.Message == "" {
		t.Fatal("Fault.Message is empty; it is handed to the model so it can correct itself")
	}
	return f
}

// Describe decodes a declarative plugin's spec, failing the test if it does not
// answer usefully.
func Describe[T any](t *testing.T, e ext.Extension) T {
	t.Helper()
	var v T
	if err := ext.Describe(context.Background(), e, &v); err != nil {
		t.Fatalf("describe: %v", err)
	}
	return v
}
