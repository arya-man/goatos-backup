// Command print-capability-catalog prints the per-person access catalog as JSON.
//
// One module per row: which surfaces it exists on, what each capability level GRANTS, and
// which admin-web screens it owns. The live persona sweep
// (tools/dev/audit-person-access.py) reads this to work out what a person's ticks should
// let them do, so its expectations come from the catalog itself rather than a second copy
// of the rules that would drift.
package main

import (
	"encoding/json"
	"os"
	"sort"
	"strings"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

type payload struct {
	Modules []module `json:"modules"`
	// WriteRoutes are the mutating routes gated on EXACTLY ONE permission and carrying no
	// path parameters -- the ones a test can call blind to prove a capability level really
	// limits what someone may do. Emitted from the route table itself so the expectations
	// can never be a hand-copied permission string; guessing one is what made a first run
	// report six false failures on procurement.vendor.write.
	WriteRoutes []writeRoute `json:"write_routes"`
}

type writeRoute struct {
	Method     string `json:"method"`
	Path       string `json:"path"`
	Permission string `json:"permission"`
}

type module struct {
	Key      string              `json:"key"`
	Label    string              `json:"label"`
	Surfaces []string            `json:"surfaces"`
	Levels   map[string][]string `json:"levels"`
	Pages    []page              `json:"pages"`
}

type page struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Href        string   `json:"href"`
	Permissions []string `json:"permissions"`
}

func main() {
	out := make([]module, 0, 24)
	for _, m := range permissions.ModuleCapabilities() {
		row := module{Key: m.Key, Label: m.Label, Surfaces: m.Surfaces, Levels: map[string][]string{}}
		for level, perms := range m.Levels {
			if perms == nil {
				perms = []string{}
			}
			row.Levels[level] = perms
		}
		for _, p := range permissions.PagesForModule(m.Key) {
			perms := p.Permissions
			if perms == nil {
				perms = []string{}
			}
			row.Pages = append(row.Pages, page{Key: p.Key, Label: p.Label, Href: p.Href, Permissions: perms})
		}
		out = append(out, row)
	}
	routes := make([]writeRoute, 0, 32)
	seen := map[string]struct{}{}
	for _, r := range permissions.ProtectedRoutes() {
		switch r.Method {
		case "POST", "PUT", "PATCH", "DELETE":
		default:
			continue
		}
		if len(r.Permissions) != 1 || len(r.AnyPermissions) > 0 || r.AdminOnly {
			continue
		}
		if strings.Contains(r.Pattern, "{") {
			continue
		}
		perm := r.Permissions[0]
		if _, dup := seen[perm]; dup {
			continue
		}
		seen[perm] = struct{}{}
		routes = append(routes, writeRoute{Method: r.Method, Path: r.Pattern, Permission: perm})
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].Permission < routes[j].Permission })

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload{Modules: out, WriteRoutes: routes}); err != nil {
		os.Exit(1)
	}
}
