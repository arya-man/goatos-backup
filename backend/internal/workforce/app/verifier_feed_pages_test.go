package app

import (
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// A verifier holding a feed.direction verify duty must be able to REACH every feed evidence queue
// from her nav, not just feed distribution.
//
// Observed on STG 2026-08-09: Feed registers three verification categories (feed_distribution,
// feed_packing, feed_transport) but the verifier's Feed module emitted ONE tab, pinned to
// feed_distribution by verificationCategoryForFeature. Four feed packing proofs and one transport
// proof sat pending with no nav entry that could open them -- the backend gate authorized her for
// all three (her duty maps to the feed_direction navigation module, which is what every one of
// those categories is registered against), so this was purely a missing way in.
//
// The failure is silent by construction: the tab that DOES exist answers 200 with distribution's
// rows, so the queue looks merely empty of packing rather than unreachable. These assertions are on
// the emitted hrefs for that reason -- an "is the tab there" check passed throughout the outage.

// Jyothi's real STG shape: the verifier role at tenant scope. Permissions are derived from the
// role (grantsHavePermission -> permissions.RoleHasPermission), so the role alone is the fixture.
func verifierGrants() []domain.GrantSummary {
	return []domain.GrantSummary{{
		Role:      permissions.RoleVerifier,
		ScopeType: "tenant",
		Status:    "active",
	}}
}

func navHrefByKey(module domain.BootstrapModule, key string) string {
	for _, item := range module.NavItems {
		if item.Key == key {
			return item.Href
		}
	}
	return ""
}

func TestVerifierFeedModuleReachesEveryFeedEvidenceQueue(t *testing.T) {
	module := verificationModuleForFeature("feed.direction", verifierGrants(), "en")

	want := map[string]string{
		"verify_feed_distribution": "feed_distribution",
		"verify_feed_packing":      "feed_packing",
		"verify_feed_transport":    "feed_transport",
	}
	for key, category := range want {
		href := navHrefByKey(module, key)
		if href == "" {
			t.Fatalf("feed verifier nav has no %q tab: a registered evidence category with no nav entry is unreachable on the phone (nav items: %+v)", key, module.NavItems)
		}
		if !strings.Contains(href, "category="+category) {
			t.Errorf("%s href = %q, want it to carry category=%s -- the client scopes the queue by category, so a wrong/absent one silently opens another module's rows", key, href, category)
		}
	}
}

func TestVerifierFeedTabsAreDistinctQueues(t *testing.T) {
	module := verificationModuleForFeature("feed.direction", verifierGrants(), "en")

	seen := map[string]string{}
	for _, item := range module.NavItems {
		if !strings.HasPrefix(item.Key, "verify") {
			continue
		}
		if prior, dup := seen[item.Href]; dup {
			t.Errorf("verify tabs %q and %q both open %q: two tabs onto one queue means a category is unreachable", prior, item.Key, item.Href)
		}
		seen[item.Href] = item.Key
		if strings.TrimSpace(item.Label) == "" {
			t.Errorf("verify tab %q has an empty label; a nameless tab cannot be told from its sibling", item.Key)
		}
	}
	if len(seen) != 3 {
		t.Errorf("feed verifier has %d distinct verify queues, want 3 (distribution, packing, transport)", len(seen))
	}
}

// A single-category module must NOT grow a named tab: the verifier is already standing in that
// module, so naming the page repeats it back at her. This pins the blast radius of the feed fix to
// feed -- vaccination, weighing, counts and health keep exactly the bar they had.
func TestSingleCategoryVerifierModulesKeepOneUnnamedVerifyTab(t *testing.T) {
	for _, feature := range []string{"vaccination", "weighing", "counts", "aas_health"} {
		module := verificationModuleForFeature(feature, verifierGrants(), "en")

		verifyTabs := 0
		for _, item := range module.NavItems {
			if strings.HasPrefix(item.Key, "verify") {
				verifyTabs++
				if item.Key != "verify" {
					t.Errorf("%s verify tab key = %q, want the unnamed \"verify\"", feature, item.Key)
				}
			}
		}
		if verifyTabs != 1 {
			t.Errorf("%s has %d verify tabs, want exactly 1", feature, verifyTabs)
		}
	}
}

// The category a tab emits must be one the producers actually write onto verification_items. A
// value nothing matches is the counts_proof defect: HTTP 200, zero rows, permanently empty.
func TestVerifierFeedCategoriesAreTheRegisteredOnes(t *testing.T) {
	registered := map[string]bool{
		"feed_distribution": true,
		"feed_packing":      true,
		"feed_transport":    true,
	}
	for _, page := range verificationPagesForFeature("feed_direction") {
		if !registered[page.category] {
			t.Errorf("feed verifier tab %q emits category %q, which no feed producer writes", page.key, page.category)
		}
	}
}
