package ext

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// nameRe constrains plugin and tool names so that the qualified form
// "<plugin>_<tool>" is always a legal LLM function name.
var nameRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// maxToolNameLen is the function-name limit imposed by the model APIs.
const maxToolNameLen = 64

// QualifiedName is how a plugin's tool is exposed to the LLM.
func QualifiedName(plugin, tool string) string { return plugin + "_" + tool }

// SplitQualified reverses QualifiedName against a known plugin name.
func SplitQualified(plugin, qualified string) (string, bool) {
	prefix := plugin + "_"
	if !strings.HasPrefix(qualified, prefix) {
		return "", false
	}
	return strings.TrimPrefix(qualified, prefix), true
}

// entry is a loaded plugin plus its validated manifest snapshot.
type entry struct {
	ext      Extension
	manifest Manifest
}

// Registry holds every loaded plugin and is the only thing the rest of the
// system talks to. It is safe for concurrent use.
//
// A plugin that fails validation is rejected and logged; it never takes the
// host down with it. A broken plugin on stage must not kill the demo.
type Registry struct {
	mu      sync.RWMutex
	entries []entry
	taken   map[string]string // qualified tool name -> owning plugin
	log     *slog.Logger
}

// NewRegistry returns an empty Registry. A nil logger discards rejections.
func NewRegistry(log *slog.Logger) *Registry {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Registry{taken: map[string]string{}, log: log}
}

// Load validates a plugin and adds it. On failure the plugin is not added and
// the error explains why; callers normally log and carry on.
func (r *Registry) Load(e Extension) error {
	m := e.Manifest()
	if err := ValidateManifest(m); err != nil {
		return fmt.Errorf("plugin %q rejected: %w", m.Name, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, existing := range r.entries {
		if existing.manifest.Name == m.Name {
			return fmt.Errorf("plugin %q rejected: already loaded", m.Name)
		}
	}
	// Reserve every tool name up front so a collision rejects the whole plugin
	// rather than leaving it half-registered.
	for _, ts := range m.Provides {
		q := QualifiedName(m.Name, ts.Name)
		if owner, clash := r.taken[q]; clash {
			return fmt.Errorf("plugin %q rejected: tool %q already provided by %q", m.Name, q, owner)
		}
	}
	for _, ts := range m.Provides {
		r.taken[QualifiedName(m.Name, ts.Name)] = m.Name
	}
	r.entries = append(r.entries, entry{ext: e, manifest: m})
	r.log.Info("plugin loaded", "name", m.Name, "version", m.Version, "kind", string(m.Kind), "tools", len(m.Provides))
	return nil
}

// MustLoad loads plugins, logging rejections instead of returning them. It is
// the normal wiring path: one bad plugin must not stop the others.
func (r *Registry) MustLoad(exts ...Extension) {
	for _, e := range exts {
		if err := r.Load(e); err != nil {
			r.log.Error("plugin not loaded", "err", err)
		}
	}
}

// Manifests returns a snapshot of every loaded manifest, sorted by name.
func (r *Registry) Manifests() []Manifest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Manifest, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, e.manifest)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ByKind returns the loaded plugins of a given Kind, in load order. The host
// uses it to resolve Manifest.Requires; plugins never call each other.
func (r *Registry) ByKind(k Kind) []Extension {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Extension
	for _, e := range r.entries {
		if e.manifest.Kind == k {
			out = append(out, e.ext)
		}
	}
	return out
}

// Lookup finds a plugin by name.
func (r *Registry) Lookup(name string) (Extension, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, e := range r.entries {
		if e.manifest.Name == name {
			return e.ext, true
		}
	}
	return nil, false
}

// Describe calls the DescribeTool of a declarative plugin and decodes the
// result into v.
func Describe(ctx context.Context, e Extension, v any) error {
	m := e.Manifest()
	if !m.Kind.Declarative() {
		return fmt.Errorf("plugin %q has kind %q, which is not declarative", m.Name, m.Kind)
	}
	raw, err := e.Invoke(ctx, DescribeTool, json.RawMessage("{}"))
	if err != nil {
		return fmt.Errorf("plugin %q describe: %w", m.Name, err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("plugin %q describe returned unusable JSON: %w", m.Name, err)
	}
	return nil
}

// Close closes every loaded plugin, returning the first error seen while still
// attempting to close the rest.
func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var first error
	for _, e := range r.entries {
		if err := e.ext.Close(); err != nil && first == nil {
			first = fmt.Errorf("close %q: %w", e.manifest.Name, err)
		}
	}
	r.entries = nil
	r.taken = map[string]string{}
	return first
}

// ValidateManifest applies every rule a plugin must satisfy to be loadable.
// It is exported so plugin authors can assert against it in their own tests;
// exttest.Conformance runs it for you.
func ValidateManifest(m Manifest) error {
	if !nameRe.MatchString(m.Name) {
		return fmt.Errorf("name %q must match %s", m.Name, nameRe)
	}
	if m.ABIVersion != ABIVersion {
		return fmt.Errorf("abi_version %d, host speaks %d", m.ABIVersion, ABIVersion)
	}
	if !m.Kind.Valid() {
		return fmt.Errorf("unknown kind %q", m.Kind)
	}
	if m.Version == "" {
		return fmt.Errorf("version is empty")
	}
	if len(m.Provides) == 0 {
		return fmt.Errorf("provides no tools")
	}
	if m.Kind.Declarative() {
		if len(m.Provides) != 1 || m.Provides[0].Name != DescribeTool {
			return fmt.Errorf("kind %q is declarative: it must provide exactly one tool named %q", m.Kind, DescribeTool)
		}
	}
	for _, k := range m.Requires {
		if !k.Valid() {
			return fmt.Errorf("requires unknown kind %q", k)
		}
		if k == m.Kind {
			return fmt.Errorf("requires its own kind %q", k)
		}
	}
	seen := map[string]bool{}
	for _, ts := range m.Provides {
		if err := validateToolSpec(m.Name, ts); err != nil {
			return err
		}
		if seen[ts.Name] {
			return fmt.Errorf("tool %q declared twice", ts.Name)
		}
		seen[ts.Name] = true
	}
	return nil
}

func validateToolSpec(plugin string, ts ToolSpec) error {
	if !nameRe.MatchString(ts.Name) {
		return fmt.Errorf("tool name %q must match %s", ts.Name, nameRe)
	}
	if q := QualifiedName(plugin, ts.Name); len(q) > maxToolNameLen {
		return fmt.Errorf("tool name %q is %d chars, limit is %d", q, len(q), maxToolNameLen)
	}
	if strings.TrimSpace(ts.Description) == "" {
		return fmt.Errorf("tool %q has no description; the model reads it to decide when to call the tool", ts.Name)
	}
	if err := validateObjectSchema(ts.Schema); err != nil {
		return fmt.Errorf("tool %q schema: %w", ts.Name, err)
	}
	return nil
}

// validateObjectSchema enforces the one schema rule that matters: the argument
// schema must be an object, because that is the only shape a function
// declaration can carry.
func validateObjectSchema(raw json.RawMessage) error {
	if len(raw) == 0 {
		return fmt.Errorf("missing; use {\"type\":\"object\",\"properties\":{}} for a no-argument tool")
	}
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return fmt.Errorf("not valid JSON: %w", err)
	}
	if probe.Type != "object" {
		return fmt.Errorf("top-level type is %q, must be \"object\"", probe.Type)
	}
	return nil
}
