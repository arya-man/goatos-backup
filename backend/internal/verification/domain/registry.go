package domain

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// CategoryDefinition is one entry in the verification type registry — the RT-registry analog from
// the Slack Workflow Engine (verification-module-design.md §2.3). Registering an entry is all a new
// vertical/module needs to appear in the verifier queue, admin-web screen, and mobile section; no
// verification code changes per module.
type CategoryDefinition struct {
	Vertical      string
	Module        string
	Category      string
	ExpectedMedia []string // e.g. ["video"]; informational — enforced by producer, not verification.
	SLAHours      int
}

// Registry is a concurrency-safe, in-memory category registry populated at composition time.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]CategoryDefinition
}

func NewRegistry() *Registry {
	return &Registry{entries: map[string]CategoryDefinition{}}
}

// Register adds (or replaces) one category entry. Category is the registry key: two modules must
// not share a category string.
func (r *Registry) Register(def CategoryDefinition) error {
	def.Vertical = strings.TrimSpace(def.Vertical)
	def.Module = strings.TrimSpace(def.Module)
	def.Category = strings.TrimSpace(def.Category)
	if def.Vertical == "" || def.Module == "" || def.Category == "" {
		return fmt.Errorf("%w: vertical, module, and category are required", ErrInvalid)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[def.Category] = def
	return nil
}

// Get returns the registered definition for a category, if any.
func (r *Registry) Get(category string) (CategoryDefinition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.entries[strings.TrimSpace(category)]
	return def, ok
}

// List returns all registered categories, sorted by category for deterministic output.
func (r *Registry) List() []CategoryDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]CategoryDefinition, 0, len(r.entries))
	for _, def := range r.entries {
		out = append(out, def)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Category < out[j].Category })
	return out
}
