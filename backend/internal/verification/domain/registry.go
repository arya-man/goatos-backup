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
	// MediaLabels is the backend-owned header shown above each proof, positionally parallel to
	// ExpectedMedia — index 0 names the first proof, index 1 the second, and so on. It is what tells
	// a verifier WHICH video she is watching (the feed-distribution video vs the water proof), so it
	// belongs to the registry with the rest of this category's display copy, not to a renderer.
	//
	// Optional: a category that leaves it empty still gets a header, derived from ExpectedMedia by
	// MediaLabelFor. A per-proof label resolved from workflow task truth always wins over both.
	MediaLabels           []string
	SLAHours              int
	NavigationModule      string // backend drawer module key, e.g. counts or feed_direction
	NavigationModuleLabel string // backend-owned display copy for the queue header
	PageKey               string // stable page/tab key inside NavigationModule
	PageLabel             string // backend-owned top-tab label
	PageOrder             int
}

// MediaLabelFor returns the backend-owned header for the proof at position i (0-based) out of
// mediaCount proofs on one item.
//
// Every proof gets a header — a verifier watching an unlabelled video has to guess what she is
// checking it against. Resolution order:
//
//  1. the category's declared MediaLabels[i] — the real semantic name ("Water distribution proof");
//  2. otherwise the declared ExpectedMedia[i] kind, humanised ("video" -> "Video"), numbered when
//     the same kind appears more than once so five milk-prep videos are not five rows of "Video";
//  3. otherwise a plain numbered "Proof", for a category that declared neither.
//
// A per-proof label resolved from workflow task truth is richer than any of these and is applied by
// the caller BEFORE this fallback, never overwritten by it.
func (d CategoryDefinition) MediaLabelFor(i, mediaCount int) string {
	if i < 0 {
		return ""
	}
	if i < len(d.MediaLabels) {
		if label := strings.TrimSpace(d.MediaLabels[i]); label != "" {
			return label
		}
	}
	if i < len(d.ExpectedMedia) {
		if kind := strings.TrimSpace(d.ExpectedMedia[i]); kind != "" {
			return numberedLabel(humanizeMediaKind(kind), i, d.countExpectedKind(kind))
		}
	}
	return numberedLabel("Proof", i, mediaCount)
}

// countExpectedKind reports how many times a media kind is expected on this category, which decides
// whether its label needs an ordinal.
func (d CategoryDefinition) countExpectedKind(kind string) int {
	n := 0
	for _, candidate := range d.ExpectedMedia {
		if strings.EqualFold(strings.TrimSpace(candidate), kind) {
			n++
		}
	}
	return n
}

// numberedLabel appends a 1-based ordinal only when the label would otherwise repeat.
func numberedLabel(label string, i, total int) string {
	if total <= 1 {
		return label
	}
	return fmt.Sprintf("%s %d", label, i+1)
}

// humanizeMediaKind turns a registry media kind into operator-facing words. The raw tokens
// ("photo_or_video") are config identifiers and must never reach a screen.
func humanizeMediaKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "video":
		return "Video"
	case "photo":
		return "Photo"
	case "photo_or_video", "video_or_photo":
		return "Photo or video"
	case "":
		return "Proof"
	default:
		replaced := strings.ReplaceAll(strings.ToLower(kind), "_", " ")
		return strings.ToUpper(replaced[:1]) + replaced[1:]
	}
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
	def.NavigationModule = strings.TrimSpace(def.NavigationModule)
	def.NavigationModuleLabel = strings.TrimSpace(def.NavigationModuleLabel)
	def.PageKey = strings.TrimSpace(def.PageKey)
	def.PageLabel = strings.TrimSpace(def.PageLabel)
	if def.Vertical == "" || def.Module == "" || def.Category == "" {
		return fmt.Errorf("%w: vertical, module, and category are required", ErrInvalid)
	}
	pageFields := []string{def.NavigationModule, def.NavigationModuleLabel, def.PageKey, def.PageLabel}
	pageFieldCount := 0
	for _, value := range pageFields {
		if value != "" {
			pageFieldCount++
		}
	}
	if pageFieldCount != 0 && pageFieldCount != len(pageFields) {
		return fmt.Errorf("%w: verification navigation metadata must include module key, module label, page key, and page label", ErrInvalid)
	}
	// MediaLabels is POSITIONAL against ExpectedMedia, so a longer list would silently name proofs
	// the producer never sends. Reject that here rather than let it surface as a mislabelled video.
	for i := range def.MediaLabels {
		def.MediaLabels[i] = strings.TrimSpace(def.MediaLabels[i])
	}
	if len(def.MediaLabels) > 0 && len(def.MediaLabels) != len(def.ExpectedMedia) {
		return fmt.Errorf("%w: verification media labels must be positionally parallel to expected media (%d labels vs %d expected)", ErrInvalid, len(def.MediaLabels), len(def.ExpectedMedia))
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
