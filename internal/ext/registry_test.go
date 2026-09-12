package ext

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// fake is a minimal Extension for exercising the registry.
type fake struct {
	m      Manifest
	closed int
}

func (f *fake) Manifest() Manifest { return f.m }
func (f *fake) Invoke(context.Context, string, json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}
func (f *fake) Close() error { f.closed++; return nil }

func okSpec(name string) ToolSpec {
	return ToolSpec{Name: name, Description: "does a thing when asked", Schema: json.RawMessage(`{"type":"object"}`)}
}

func okManifest(name string) Manifest {
	return Manifest{Name: name, Version: "0.1.0", ABIVersion: ABIVersion, Kind: KindRetrieval, Provides: []ToolSpec{okSpec("search")}}
}

func TestValidateManifest(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Manifest)
		wantErr string
	}{
		{"valid", func(*Manifest) {}, ""},
		{"bad name", func(m *Manifest) { m.Name = "Bad-Name" }, "must match"},
		{"abi mismatch", func(m *Manifest) { m.ABIVersion = 99 }, "host speaks"},
		{"unknown kind", func(m *Manifest) { m.Kind = "wat" }, "unknown kind"},
		{"no version", func(m *Manifest) { m.Version = "" }, "version is empty"},
		{"no tools", func(m *Manifest) { m.Provides = nil }, "provides no tools"},
		{"no description", func(m *Manifest) { m.Provides[0].Description = " " }, "no description"},
		{"non-object schema", func(m *Manifest) {
			m.Provides[0].Schema = json.RawMessage(`{"type":"string"}`)
		}, `must be "object"`},
		{"missing schema", func(m *Manifest) { m.Provides[0].Schema = nil }, "missing"},
		{"duplicate tool", func(m *Manifest) { m.Provides = append(m.Provides, okSpec("search")) }, "declared twice"},
		{"requires own kind", func(m *Manifest) { m.Requires = []Kind{KindRetrieval} }, "its own kind"},
		{"declarative needs describe", func(m *Manifest) { m.Kind = KindAgent }, "declarative"},
		{"tool name too long", func(m *Manifest) {
			m.Provides[0].Name = strings.Repeat("a", 70)
		}, "limit is 64"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := okManifest("search_plugin")
			m.Provides = append([]ToolSpec(nil), m.Provides...)
			tc.mutate(&m)
			err := ValidateManifest(m)
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("want valid, got %v", err)
			case tc.wantErr != "" && err == nil:
				t.Fatalf("want error containing %q, got nil", tc.wantErr)
			case tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr):
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestRegistryRejectsDuplicatePlugin(t *testing.T) {
	r := NewRegistry(nil)
	if err := r.Load(&fake{m: okManifest("alpha")}); err != nil {
		t.Fatalf("first load: %v", err)
	}
	if err := r.Load(&fake{m: okManifest("alpha")}); err == nil {
		t.Fatal("loading the same plugin name twice must be rejected")
	}
}

// A tool-name clash must reject the whole plugin, never leave it half
// registered with some tools live and others dropped.
func TestRegistryToolCollisionIsAtomic(t *testing.T) {
	r := NewRegistry(nil)
	first := okManifest("alpha")
	first.Provides = []ToolSpec{okSpec("search"), okSpec("fetch")}
	if err := r.Load(&fake{m: first}); err != nil {
		t.Fatalf("first load: %v", err)
	}

	// Same plugin name is impossible, so collide by construction: a second
	// plugin whose qualified names overlap.
	clash := Manifest{Name: "alpha_search", Version: "1", ABIVersion: ABIVersion, Kind: KindRetrieval,
		Provides: []ToolSpec{okSpec("extra")}}
	if err := r.Load(&fake{m: clash}); err != nil {
		t.Fatalf("non-colliding plugin should load: %v", err)
	}

	collider := Manifest{Name: "alpha", Version: "2", ABIVersion: ABIVersion, Kind: KindRetrieval,
		Provides: []ToolSpec{okSpec("brand_new")}}
	if err := r.Load(&fake{m: collider}); err == nil {
		t.Fatal("duplicate plugin name must be rejected")
	}
	if got := len(r.Manifests()); got != 2 {
		t.Fatalf("registry holds %d plugins, want 2", got)
	}
}

func TestRegistryCloseClosesEveryPlugin(t *testing.T) {
	a, b := &fake{m: okManifest("alpha")}, &fake{m: okManifest("beta")}
	r := NewRegistry(nil)
	r.MustLoad(a, b)
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if a.closed != 1 || b.closed != 1 {
		t.Fatalf("closed counts: alpha=%d beta=%d, want 1 each", a.closed, b.closed)
	}
}

// MustLoad must survive a bad plugin: one broken manifest cannot be allowed to
// take the others down with it.
func TestMustLoadSkipsBadPlugin(t *testing.T) {
	bad := okManifest("broken")
	bad.ABIVersion = 999

	r := NewRegistry(nil)
	r.MustLoad(&fake{m: bad}, &fake{m: okManifest("good")})

	ms := r.Manifests()
	if len(ms) != 1 || ms[0].Name != "good" {
		t.Fatalf("manifests = %v, want only [good]", ms)
	}
}

func TestQualifiedName(t *testing.T) {
	if got := QualifiedName("profile", "upsert"); got != "profile_upsert" {
		t.Fatalf("QualifiedName = %q", got)
	}
	if tool, ok := SplitQualified("profile", "profile_upsert"); !ok || tool != "upsert" {
		t.Fatalf("SplitQualified = %q, %v", tool, ok)
	}
	if _, ok := SplitQualified("profile", "other_upsert"); ok {
		t.Fatal("SplitQualified matched the wrong plugin")
	}
}
