package app

import (
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// A verifier's module tab must land on a category some producer ACTUALLY writes.
//
// verificationCategoryForFeature falls back to "<module>_proof" for anything it does not name, and
// that guess has now been wrong four times: counts (counts_proof), feed (one category named while
// three were registered), milk (milk_proof) and health (aas_health_proof). The failure is silent by
// construction -- an unregistered category answers 400 unknown_category, or a registered-but-wrong
// one answers 200 with another page's rows -- so it is only ever found by opening the tab on a
// phone. These assertions pin every landing category against the registry in bootstrap/api.go.
//
// The durable fix is to read the category FROM the registry instead of guessing it here; until then
// this test is the thing that fails when the two drift.

func verifierGrants() []domain.GrantSummary {
	return []domain.GrantSummary{{
		Role:      permissions.RoleVerifier,
		ScopeType: "tenant",
		Status:    "active",
	}}
}

// registeredCategories mirrors bootstrap/api.go's RegisterCategory calls. Update BOTH together.
var registeredCategories = map[string]bool{
	"vaccination_proof": true,
	"weighing_proof":    true,
	"health_adults":     true,
	"health_kids":       true,
	"shifting_move":     true,
	"birth_evidence":    true,
	"death_evidence":    true,
	"milk_preparation":  true,
	"milk_feeding":      true,
	"feed_distribution": true,
	"feed_packing":      true,
	"feed_transport":    true,
}

func TestEveryVerifierModuleLandsOnARegisteredCategory(t *testing.T) {
	for _, feature := range []string{"vaccination", "weighing", "counts", "feed_direction", "milk", "aas_health"} {
		category := verificationCategoryForFeature(feature)
		if !registeredCategories[category] {
			t.Errorf("feature %q lands on category %q, which no producer registers -- the queue answers 400 unknown_category and the tab is dead", feature, category)
		}
	}
}

// Milk review follows the MILK module (maintainer decision 2026-08-09). Its landing page is Milk
// Prep; Milk Feeding sits beside it in the queue's own page filter.
func TestMilkVerifierTabLandsOnMilkPreparation(t *testing.T) {
	if got := verificationCategoryForFeature("milk"); got != "milk_preparation" {
		t.Errorf("milk verify tab category = %q, want milk_preparation (never the invented milk_proof)", got)
	}
}

// ONE Verify tab per module, whatever the module's page count. The bar is module chrome; choosing
// among a module's evidence pages happens in the queue's single-select page filter. Three feed tabs
// were tried and reverted: they all resolved to the same /verify base route, so the shell read every
// one as selected and swallowed the taps.
func TestVerifierModulesEmitExactlyOneVerifyTab(t *testing.T) {
	for _, feature := range []string{"vaccination", "weighing", "counts", "feed_direction", "milk", "aas_health"} {
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
			t.Errorf("%s emitted %d verify tabs, want exactly 1", feature, verifyTabs)
		}
		if strings.TrimSpace(module.Label) == "" {
			t.Errorf("%s module has an empty drawer label", feature)
		}
	}
}

// The tab's href must carry its category explicitly: the client scopes a queue by category, and
// anything it cannot resolve falls through to vaccination -- another module's work, silently.
func TestVerifierTabHrefCarriesItsCategory(t *testing.T) {
	for _, feature := range []string{"feed_direction", "milk", "counts"} {
		module := verificationModuleForFeature(feature, verifierGrants(), "en")
		want := "category=" + verificationCategoryForFeature(feature)
		if !strings.Contains(module.Href, want) {
			t.Errorf("%s module href = %q, want it to carry %q", feature, module.Href, want)
		}
	}
}
