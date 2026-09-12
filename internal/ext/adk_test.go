package ext

import (
	"encoding/json"
	"testing"

	"google.golang.org/genai"
)

// TestSchemaFromJSONNormalizesTypes guards the trap that makes a schema look
// correct and behave wrong: JSON Schema spells types lowercase, genai spells
// them uppercase. Without normalization the schema marshals happily and the
// model API quietly misreads it.
func TestSchemaFromJSONNormalizesTypes(t *testing.T) {
	raw := json.RawMessage(`{
		"type": "object",
		"properties": {
			"skills": {"type": "array", "items": {"type": "string"}},
			"depth":  {"type": "integer"},
			"nested": {"type": "object", "properties": {"flag": {"type": "boolean"}}}
		},
		"required": ["skills"]
	}`)

	got, err := SchemaFromJSON(raw)
	if err != nil {
		t.Fatalf("SchemaFromJSON: %v", err)
	}

	checks := []struct {
		what string
		got  genai.Type
		want genai.Type
	}{
		{"root", got.Type, genai.TypeObject},
		{"skills", got.Properties["skills"].Type, genai.TypeArray},
		{"skills.items", got.Properties["skills"].Items.Type, genai.TypeString},
		{"depth", got.Properties["depth"].Type, genai.TypeInteger},
		{"nested", got.Properties["nested"].Type, genai.TypeObject},
		{"nested.flag", got.Properties["nested"].Properties["flag"].Type, genai.TypeBoolean},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s type = %q, want %q", c.what, c.got, c.want)
		}
	}
	if len(got.Required) != 1 || got.Required[0] != "skills" {
		t.Errorf("required = %v, want [skills]", got.Required)
	}
}

func TestSchemaFromJSONEmptyIsObject(t *testing.T) {
	got, err := SchemaFromJSON(nil)
	if err != nil {
		t.Fatalf("SchemaFromJSON(nil): %v", err)
	}
	if got.Type != genai.TypeObject {
		t.Errorf("type = %q, want OBJECT", got.Type)
	}
}
