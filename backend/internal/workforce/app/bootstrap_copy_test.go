package app

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/localization"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

func moduleKeySet(modules []domain.BootstrapModule) map[string]string {
	out := make(map[string]string, len(modules))
	for _, m := range modules {
		out[m.Key] = m.Status
	}
	return out
}

// TestLeadershipDrawerCompositionPerRole locks the role -> drawer matrix
// (maintainer decision 2026-07-24, docs/decisions/role-module-nav-composition.md):
//   - CEO/CXO: Vaccination (leadership home) + Counts + Feed(soon) + Breeding(soon),
//     never the operator vaccination-drives module or Verification. Expanded drawer.
//   - PC Director / Park Head: Vaccination home ONLY (preventive-care specialty).
//     No Counts, no Feed/Breeding soon rows. Minimal chrome (single module).
//   - Verifier: Verification only. Operator: department modules, never leadership.
func TestLeadershipDrawerCompositionPerRole(t *testing.T) {
	const en = localization.DefaultTag

	t.Run("ceo sees vaccination+counts+soon, no operator-vaccination, no verification", func(t *testing.T) {
		grants := []domain.GrantSummary{grantWithRole(permissions.RoleCEOInternal)}
		modules := modulesFor(grants, nil, en)
		keys := moduleKeySet(modules)

		if _, ok := keys["leadership"]; !ok {
			t.Fatalf("CEO must have the leadership (Vaccination) module; got %v", keys)
		}
		if keys["counts"] != moduleStatusAvailable {
			t.Fatalf("CEO must see Counts as available; got %v", keys)
		}
		if keys["feed_direction"] != moduleStatusSoon || keys["breeding"] != moduleStatusSoon {
			t.Fatalf("CEO must see Feed/Breeding as soon; got %v", keys)
		}
		if _, ok := keys["vaccination"]; ok {
			t.Fatalf("CEO must NOT see the operator vaccination-drives module; got %v", keys)
		}
		if _, ok := keys["verification"]; ok {
			t.Fatalf("CEO must NOT see Verification (verifier-only); got %v", keys)
		}
		// The leadership drawer row is branded "Vaccination" and lands on Calendar.
		for _, m := range modules {
			if m.Key == "leadership" {
				if m.Label != "Vaccination" {
					t.Fatalf("leadership row label = %q, want Vaccination", m.Label)
				}
				if m.Href != "/calendar" {
					t.Fatalf("leadership row lands on %q, want /calendar", m.Href)
				}
				var hasOverview, hasCalendar bool
				for _, it := range m.NavItems {
					if it.Key == "overview" && it.Href == "/leadership" {
						hasOverview = true
					}
					if it.Key == "calendar" {
						hasCalendar = true
					}
				}
				if !hasOverview || !hasCalendar {
					t.Fatalf("leadership bar must keep Overview + Calendar tabs; got %+v", m.NavItems)
				}
			}
		}
		if got := navChromeFor(grants, modules); got != domain.NavChromeExpanded {
			t.Fatalf("CEO chrome = %q, want expanded", got)
		}
	})

	for _, role := range []string{permissions.RolePCDirector, permissions.RoleParkHead} {
		t.Run("pc-leader "+role+" sees vaccination only", func(t *testing.T) {
			grants := []domain.GrantSummary{grantWithRole(role)}
			// Even with a department vaccination+counts grant, a preventive-care leader's
			// drawer is the vaccination home only.
			modules := modulesFor(grants, []string{"vaccination", "counts"}, en)
			keys := moduleKeySet(modules)

			if _, ok := keys["leadership"]; !ok {
				t.Fatalf("%s must have the leadership (Vaccination) module; got %v", role, keys)
			}
			for _, banned := range []string{"counts", "feed_direction", "breeding", "vaccination", "verification"} {
				if _, ok := keys[banned]; ok {
					t.Fatalf("%s must NOT see %q; got %v", role, banned, keys)
				}
			}
			if got := navChromeFor(grants, modules); got != domain.NavChromeMinimal {
				t.Fatalf("%s chrome = %q, want minimal (single module)", role, got)
			}
		})
	}

	t.Run("verifier sees verification only", func(t *testing.T) {
		grants := []domain.GrantSummary{grantWithRole(permissions.RoleVerifier)}
		keys := moduleKeySet(modulesFor(grants, []string{"vaccination"}, en))
		if _, ok := keys["verification"]; !ok {
			t.Fatalf("verifier must see Verification; got %v", keys)
		}
		if _, ok := keys["leadership"]; ok {
			t.Fatalf("verifier must NOT see leadership; got %v", keys)
		}
	})

	t.Run("operator keeps department modules, never leadership", func(t *testing.T) {
		grants := []domain.GrantSummary{grantWithRole(permissions.RoleOperator)}
		keys := moduleKeySet(modulesFor(grants, []string{"vaccination", "counts"}, en))
		if _, ok := keys["vaccination"]; !ok {
			t.Fatalf("operator must keep the vaccination module; got %v", keys)
		}
		if _, ok := keys["leadership"]; ok {
			t.Fatalf("operator must NOT get the leadership module; got %v", keys)
		}
		if _, ok := keys["verification"]; ok {
			t.Fatalf("operator must NOT get verification; got %v", keys)
		}
	})
}

func TestBootstrapCopyCatalogCoversSupportedLocales(t *testing.T) {
	english := bootstrapLabels[localization.DefaultTag]
	if len(english) == 0 {
		t.Fatal("English bootstrap copy catalog is empty")
	}
	for _, tag := range localization.SupportedTags() {
		labels := bootstrapLabels[tag]
		if len(labels) == 0 {
			t.Fatalf("missing bootstrap copy catalog for locale %q", tag)
		}
		for key := range english {
			if labels[key] == "" {
				t.Fatalf("missing bootstrap copy key %q for locale %q", key, tag)
			}
		}
	}
}
