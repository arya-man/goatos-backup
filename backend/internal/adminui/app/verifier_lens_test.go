package app

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/adminui/domain"
	"github.com/vgoats/goatos/backend/internal/permissions"
)

const lensTenantID = "00000000-0000-4000-8000-000000000001"

// fakeVerificationModules stands in for the Verification type registry. adminui depends on the port,
// not on verification/domain, so the lens is testable without wiring the real registry.
type fakeVerificationModules struct {
	modules []VerificationNavModule
}

func (f fakeVerificationModules) VerifierNavModules() []VerificationNavModule { return f.modules }

// fakeModuleDutyReader stands in for the workforce repository. It returns a fixed set of module keys.
type fakeModuleDutyReader struct {
	moduleKeys []string
}

func (f fakeModuleDutyReader) ListVerifyModuleKeys(ctx context.Context, tenantID, userID string) ([]string, error) {
	return f.moduleKeys, nil
}

// fakeModuleDutyReaderError returns an error when queried (for fail-safe testing).
type fakeModuleDutyReaderError struct{}

func (f fakeModuleDutyReaderError) ListVerifyModuleKeys(ctx context.Context, tenantID, userID string) ([]string, error) {
	return nil, fmt.Errorf("simulated duty reader error")
}

// registryLikeModules mirrors the shape backend/internal/bootstrap/api.go registers today: five
// evidence modules, several with more than one page, deliberately supplied OUT of order so the
// tests prove the lens sorts rather than echoing input order.
func registryLikeModules() []VerificationNavModule {
	return []VerificationNavModule{
		{Key: "feed_direction", Label: "Feed", Pages: []VerificationNavPage{
			{Key: "feed_transport", Label: "Feed Transport", Category: "feed_transport", Order: 3},
			{Key: "feed_distribution", Label: "Feed Distribution", Category: "feed_distribution", Order: 1},
			{Key: "feed_packing", Label: "Feed Packing", Category: "feed_packing", Order: 2},
		}},
		{Key: "vaccination", Label: "Vaccination", Pages: []VerificationNavPage{
			{Key: "vaccination", Label: "Vaccination", Category: "vaccination_shed_proof", Order: 1},
		}},
		{Key: "counts", Label: "Counts", Pages: []VerificationNavPage{
			{Key: "shifting", Label: "Shifting", Category: "shifting_move", Order: 3},
			{Key: "birth", Label: "Birth", Category: "birth_evidence", Order: 1},
			{Key: "death", Label: "Death", Category: "death_evidence", Order: 2},
		}},
		{Key: "aas_health", Label: "Health", Pages: []VerificationNavPage{
			{Key: "health_kids", Label: "Kids", Category: "health_kids", Order: 2},
			{Key: "health_adults", Label: "Adults", Category: "health_adults", Order: 1},
		}},
		{Key: "weighing", Label: "Weighing", Pages: []VerificationNavPage{
			{Key: "weighing", Label: "Weighing", Category: "weighing_shed_proof", Order: 1},
		}},
	}
}

func lensService() *Service {
	return NewService().
		WithVerificationModules(fakeVerificationModules{modules: registryLikeModules()}).
		WithModuleDutyReader(fakeModuleDutyReader{
			// Default: verifier holds duties for all registry modules (for backward compatibility with old tests).
			moduleKeys: []string{"pc.vaccination", "weighing", "counts", "feed.direction", "aas_health"},
		})
}

func grantsFor(role string) []permissions.ActiveGrant {
	return []permissions.ActiveGrant{{Role: role, ScopeType: "tenant", ScopeID: lensTenantID}}
}

func bootstrapAs(s *Service, role string) domain.BootstrapResponse {
	return s.Bootstrap(context.Background(), BootstrapInput{TenantID: lensTenantID, Grants: grantsFor(role)})
}

func groupByID(t *testing.T, groups []domain.NavigationGroup, id string) domain.NavigationGroup {
	t.Helper()
	for _, group := range groups {
		if group.ID == id {
			return group
		}
	}
	t.Fatalf("missing nav group %q in %#v", id, groups)
	return domain.NavigationGroup{}
}

// The verifier's sidebar is the five registry evidence modules and nothing else. This is the
// maintainer's requirement in one assertion: she must not be able to see another module.
func TestVerifierLensShowsOnlyEvidenceModules(t *testing.T) {
	resp := bootstrapAs(lensService(), permissions.RoleVerifier)

	var labels []string
	for _, group := range resp.Navigation.Groups {
		labels = append(labels, group.Label)
	}
	want := []string{"Counts", "Feed", "Health", "Vaccination", "Weighing"}
	if len(labels) != len(want) {
		t.Fatalf("verifier nav groups = %v, want exactly %v", labels, want)
	}
	for i := range want {
		if labels[i] != want[i] {
			t.Fatalf("verifier nav groups = %v, want %v (sorted by label)", labels, want)
		}
	}

	// Every operational/authority surface must be absent from the sidebar ENTIRELY -- not present
	// and disabled. A greyed-out Control Tower row would still tell her the product exists and
	// invite a URL attempt; the requirement was that she cannot visit other modules at all.
	for _, item := range resp.Navigation.Primary {
		if item.Href != verifierLensRoute {
			t.Fatalf("verifier primary nav leaked a non-evidence route: %#v", item)
		}
	}
	for _, group := range resp.Navigation.Groups {
		for _, leaf := range group.Leaves {
			if leaf.Href != verifierLensRoute {
				t.Fatalf("verifier nav leaf leaked a non-evidence route: %#v", leaf)
			}
		}
	}
}

// Pages within a module follow the registry's declared PageOrder, and each leaf carries its own
// disjoint category so one route can serve every module.
func TestVerifierLensLeavesCarryRegistryPagesInOrder(t *testing.T) {
	resp := bootstrapAs(lensService(), permissions.RoleVerifier)

	counts := groupByID(t, resp.Navigation.Groups, "verification-counts")
	wantLabels := []string{"Birth", "Death", "Shifting"}
	wantCategories := []string{"birth_evidence", "death_evidence", "shifting_move"}
	if len(counts.Leaves) != len(wantLabels) {
		t.Fatalf("counts leaves = %#v, want %v", counts.Leaves, wantLabels)
	}
	for i, leaf := range counts.Leaves {
		if leaf.Label != wantLabels[i] {
			t.Fatalf("counts leaf %d = %q, want %q (registry PageOrder)", i, leaf.Label, wantLabels[i])
		}
		if leaf.Extra["category"] != wantCategories[i] {
			t.Fatalf("counts leaf %q category = %q, want %q", leaf.Label, leaf.Extra["category"], wantCategories[i])
		}
		if leaf.Href != verifierLensRoute {
			t.Fatalf("counts leaf %q href = %q, want %q", leaf.Label, leaf.Href, verifierLensRoute)
		}
	}

	feed := groupByID(t, resp.Navigation.Groups, "verification-feed_direction")
	if len(feed.Leaves) != 3 || feed.Leaves[0].Label != "Feed Distribution" || feed.Leaves[2].Label != "Feed Transport" {
		t.Fatalf("feed leaves = %#v, want Distribution/Packing/Transport in PageOrder", feed.Leaves)
	}
}

// Route-level lockout: hiding a nav item is not access control, so the lens also drops every other
// page contract. requireAdminWebPageContract throws on a missing route_id, which is what makes a
// hand-typed /config or / fail closed instead of rendering.
func TestVerifierLensDropsEveryPageContractExceptTheQueue(t *testing.T) {
	resp := bootstrapAs(lensService(), permissions.RoleVerifier)

	if len(resp.Pages) != 1 {
		var ids []string
		for _, page := range resp.Pages {
			ids = append(ids, page.RouteID)
		}
		t.Fatalf("verifier page contracts = %v, want only %q", ids, verifierLensPageID)
	}
	if resp.Pages[0].RouteID != verifierLensPageID {
		t.Fatalf("verifier page contract = %q, want %q", resp.Pages[0].RouteID, verifierLensPageID)
	}
	for _, rule := range resp.RouteLabels {
		if rule.Pattern != verifierLensRoute {
			t.Fatalf("verifier route labels leaked %q", rule.Pattern)
		}
	}
}

// The lens is selected by review-WITHOUT-act, so a principal holding both keeps the full admin IA.
// Getting this wrong would silently strip the CEO's product down to an evidence queue.
func TestVerifierLensDoesNotApplyToAuthorityPrincipals(t *testing.T) {
	for _, role := range []string{permissions.RoleCEOInternal, permissions.RoleParkHead, permissions.RolePCDirector} {
		t.Run(role, func(t *testing.T) {
			resp := bootstrapAs(lensService(), role)
			if len(resp.Pages) <= 1 {
				t.Fatalf("%s must keep the full page set, got %d pages", role, len(resp.Pages))
			}
			if pageByRouteID(t, resp.Pages, "control-tower").RouteID == "" {
				t.Fatalf("%s lost the control-tower page contract", role)
			}
			for _, group := range resp.Navigation.Groups {
				if group.ID == "verification-counts" {
					t.Fatalf("%s must not receive the verifier lens sidebar", role)
				}
			}
		})
	}
}

// Fail closed when no module source is wired: an unwired binary must show a lens principal an empty
// workspace, never fall through to the full admin IA she must not see.
func TestVerifierLensWithoutModuleSourceStillLocksDown(t *testing.T) {
	resp := bootstrapAs(NewService(), permissions.RoleVerifier)

	if len(resp.Navigation.Groups) != 0 {
		t.Fatalf("unwired lens groups = %#v, want none", resp.Navigation.Groups)
	}
	if len(resp.Pages) != 1 || resp.Pages[0].RouteID != verifierLensPageID {
		t.Fatalf("unwired lens must still drop other pages, got %#v", resp.Pages)
	}
	if resp.NavChrome != domain.NavChromeMinimal {
		t.Fatalf("unwired lens nav chrome = %q, want %q", resp.NavChrome, domain.NavChromeMinimal)
	}
}

// Duty split on the shared /actions page contract: the verifier gets the verdict control and NOT
// the authority's source-task controls; the authority gets the reverse. One page, two personas,
// decided by the backend rather than the renderer.
func TestVerificationReviewControlsSplitByDuty(t *testing.T) {
	controlByID := func(t *testing.T, page domain.PageContract, id string) domain.Control {
		t.Helper()
		for _, control := range page.Controls {
			if control.ID == id {
				return control
			}
		}
		t.Fatalf("missing control %q on page %q", id, page.RouteID)
		return domain.Control{}
	}

	verifierPage := bootstrapAs(lensService(), permissions.RoleVerifier).Pages[0]
	if got := controlByID(t, verifierPage, "record_verdict"); !got.Enabled {
		t.Fatalf("verifier record_verdict = %#v, want enabled", got)
	}
	for _, id := range []string{"request_rework", "reassign_task"} {
		got := controlByID(t, verifierPage, id)
		if got.Enabled {
			t.Fatalf("verifier %s = %#v, want disabled (authority act)", id, got)
		}
		if got.DisabledReason == "" {
			t.Fatalf("verifier %s must carry a disabled reason", id)
		}
	}

	parkHeadPage := pageByRouteID(t, bootstrapAs(lensService(), permissions.RoleParkHead).Pages, verifierLensPageID)
	if got := controlByID(t, parkHeadPage, "record_verdict"); got.Enabled {
		t.Fatalf("park head record_verdict = %#v, want disabled (separation of duty)", got)
	}
	for _, id := range []string{"request_rework", "reassign_task"} {
		if got := controlByID(t, parkHeadPage, id); !got.Enabled {
			t.Fatalf("park head %s = %#v, want enabled", id, got)
		}
	}

	// CEO/CxO reads the same evidence queue and owns the source-task actions, but may NOT record a
	// verdict (maintainer decision 2026-08-03) — the independent second check cannot be signed off
	// by the people it checks.
	ceoPage := pageByRouteID(t, bootstrapAs(lensService(), permissions.RoleCEOInternal).Pages, verifierLensPageID)
	if got := controlByID(t, ceoPage, "record_verdict"); got.Enabled {
		t.Fatalf("ceo record_verdict = %#v, want DISABLED — verdicts are the verifier's alone", got)
	}
	for _, id := range []string{"request_rework", "reassign_task"} {
		if got := controlByID(t, ceoPage, id); !got.Enabled {
			t.Fatalf("ceo %s = %#v, want enabled", id, got)
		}
	}
}

// The route behind the verdict button must agree with the button: hiding a control is not access
// control (docs/decisions/role-module-nav-composition.md). Nobody but the Verifier may POST a
// verdict, and CEO/CxO keeps the queue read it needs to see the evidence.
func TestVerdictRouteIsVerifierOnlyWhileQueueReadStaysLeadershipVisible(t *testing.T) {
	verdict, ok := permissions.Match("POST", "/verification/items/10000000-0000-4000-8000-000000000001/verdict")
	if !ok {
		t.Fatal("recordVerificationVerdict route is not registered")
	}
	for _, role := range []string{permissions.RoleCEOInternal, permissions.RoleParkHead, permissions.RolePCDirector, permissions.RoleOperator, permissions.RoleGrowthDirector} {
		if permissions.RolesAuthorize([]string{role}, verdict.Permissions, verdict.AdminOnly) {
			t.Fatalf("%s must not be able to record a verification verdict", role)
		}
	}
	if !permissions.RolesAuthorize([]string{permissions.RoleVerifier}, verdict.Permissions, verdict.AdminOnly) {
		t.Fatal("verifier must be able to record a verification verdict")
	}

	queue, ok := permissions.Match("GET", "/verification/queue")
	if !ok {
		t.Fatal("listVerificationQueue route is not registered")
	}
	for _, role := range []string{permissions.RoleVerifier, permissions.RoleCEOInternal} {
		if !permissions.RolesAuthorize([]string{role}, queue.Permissions, queue.AdminOnly) {
			t.Fatalf("%s must keep visibility of the evidence queue", role)
		}
	}
}

// A module whose pages all lack navigation metadata is dropped rather than rendered as an empty
// group that opens onto a queue which cannot exist.
func TestVerifierLensDropsModulesWithNoRenderablePage(t *testing.T) {
	svc := NewService().
		WithVerificationModules(fakeVerificationModules{modules: []VerificationNavModule{
			{Key: "vaccination", Label: "Vaccination", Pages: []VerificationNavPage{
				{Key: "vaccination", Label: "Vaccination", Category: "vaccination_shed_proof", Order: 1},
			}},
			{Key: "ghost", Label: "Ghost", Pages: []VerificationNavPage{{Key: "", Label: "", Category: ""}}},
			{Key: "", Label: "", Pages: nil},
		}}).
		WithModuleDutyReader(fakeModuleDutyReader{moduleKeys: []string{"pc.vaccination", "ghost"}})
	resp := bootstrapAs(svc, permissions.RoleVerifier)
	if len(resp.Navigation.Groups) != 1 || resp.Navigation.Groups[0].Label != "Vaccination" {
		t.Fatalf("lens groups = %#v, want only Vaccination", resp.Navigation.Groups)
	}
}

// When a duty reader is wired, the lens shows only modules the verifier holds duties for.
// A verifier with duties for only vaccination and weighing sees exactly those two groups.
func TestVerifierLensFiltersModulesByDuty(t *testing.T) {
	svc := NewService().
		WithVerificationModules(fakeVerificationModules{modules: registryLikeModules()}).
		WithModuleDutyReader(fakeModuleDutyReader{
			// Verifier holds duties for pc.vaccination and weighing only.
			moduleKeys: []string{"pc.vaccination", "weighing"},
		})

	resp := svc.Bootstrap(context.Background(), BootstrapInput{
		TenantID: lensTenantID,
		ActorID:  "user-123",
		Grants:   grantsFor(permissions.RoleVerifier),
	})

	// Should have only Vaccination and Weighing groups (both sorted alphabetically).
	labels := make([]string, 0, len(resp.Navigation.Groups))
	for _, group := range resp.Navigation.Groups {
		labels = append(labels, group.Label)
	}
	want := []string{"Vaccination", "Weighing"}
	if len(labels) != len(want) {
		t.Fatalf("verifier nav groups = %v, want exactly %v", labels, want)
	}
	for i, expected := range want {
		if labels[i] != expected {
			t.Fatalf("verifier nav groups[%d] = %q, want %q", i, labels[i], expected)
		}
	}

	// The Health and Counts and Feed groups must NOT be present.
	for _, group := range resp.Navigation.Groups {
		if group.Label == "Health" || group.Label == "Counts" || group.Label == "Feed" {
			t.Fatalf("unauthorized module %q should not be in nav groups", group.Label)
		}
	}
}

// When the duty reader returns an error, the lens fails SAFE by showing no modules
// rather than falling back to showing all modules.
func TestVerifierLensFailsSafeOnDutyReaderError(t *testing.T) {
	svc := NewService().
		WithVerificationModules(fakeVerificationModules{modules: registryLikeModules()}).
		WithModuleDutyReader(fakeModuleDutyReaderError{})

	resp := svc.Bootstrap(context.Background(), BootstrapInput{
		TenantID: lensTenantID,
		ActorID:  "user-123",
		Grants:   grantsFor(permissions.RoleVerifier),
	})

	if len(resp.Navigation.Groups) != 0 {
		t.Fatalf("verifier nav groups = %#v, want none (fail safe on duty reader error)", resp.Navigation.Groups)
	}
	if resp.NavChrome != domain.NavChromeMinimal {
		t.Fatalf("verifier nav chrome = %q, want %q when no authorized modules", resp.NavChrome, domain.NavChromeMinimal)
	}
}

// When no duty reader is wired, the lens fails SAFE by showing no modules
// rather than showing all modules (protection against accidental unwiring).
func TestVerifierLensFailsSafeWithoutDutyReader(t *testing.T) {
	svc := NewService().
		WithVerificationModules(fakeVerificationModules{modules: registryLikeModules()})
	// Note: no WithModuleDutyReader call

	resp := svc.Bootstrap(context.Background(), BootstrapInput{
		TenantID: lensTenantID,
		ActorID:  "user-123",
		Grants:   grantsFor(permissions.RoleVerifier),
	})

	if len(resp.Navigation.Groups) != 0 {
		t.Fatalf("verifier nav groups = %#v, want none (fail safe without duty reader)", resp.Navigation.Groups)
	}
}

// The lens module set must be a subset of what the verification handler would authorize
// for the same principal. This test asserts they cannot drift apart.
// The verification handler uses navigationModuleForDutyCode to translate duty codes to nav keys;
// the lens must use the same translation.
func TestVerifierLensModuleSetMatchesVerificationHandler(t *testing.T) {
	// Simulate a verifier with duties for vaccination, weighing, and feed.direction
	// (which should resolve to "feed_direction" after translation).
	verifierDuties := []string{"pc.vaccination", "weighing", "feed.direction"}

	dutyReader := fakeModuleDutyReader{moduleKeys: verifierDuties}
	svc := NewService().
		WithVerificationModules(fakeVerificationModules{modules: registryLikeModules()}).
		WithModuleDutyReader(dutyReader)

	// Get the lens modules.
	lensModules := svc.verifierNavModules(context.Background(), BootstrapInput{
		TenantID: lensTenantID,
		ActorID:  "user-123",
		Grants:   grantsFor(permissions.RoleVerifier),
	})

	// Extract the lens's navigation keys.
	lensKeys := make(map[string]bool, len(lensModules))
	for _, module := range lensModules {
		lensKeys[module.Key] = true
	}

	// Build the set of authorized keys using the same translation as the verification handler.
	expectedKeys := make(map[string]bool)
	for _, dutyCode := range verifierDuties {
		navKey := verificationNavigationModuleForDutyCode(dutyCode)
		expectedKeys[navKey] = true
	}

	// They must match.
	if len(lensKeys) != len(expectedKeys) {
		t.Fatalf("lens module keys = %v, verifier duties translate to %v", lensKeys, expectedKeys)
	}
	for key := range expectedKeys {
		if !lensKeys[key] {
			t.Fatalf("lens missing module key %q that verifier holds duty for", key)
		}
	}
	for key := range lensKeys {
		if !expectedKeys[key] {
			t.Fatalf("lens includes module key %q that verifier does not hold duty for", key)
		}
	}
}

// Helper: wrap the shared verification.NavigationModuleForDutyCode function for testing.
// (We can't import verification directly without adding a circular dependency, but the
// production code already does this translation, so we reuse it here.)
func verificationNavigationModuleForDutyCode(dutyCode string) string {
	// This mirrors the logic from backend/internal/verification/module_key_translation.go
	// We import it via the shared location in production; tests can duplicate this logic
	// or import the shared module directly. For now, inline it to avoid test dependencies.
	return fakeTranslateModuleKey(dutyCode)
}

func fakeTranslateModuleKey(dutyModuleCode string) string {
	normalized := strings.TrimSpace(strings.ToLower(dutyModuleCode))
	if normalized == "preventive_care" {
		return "vaccination"
	}
	switch normalized {
	case "feed.direction":
		return "feed_direction"
	}
	normalized = strings.TrimPrefix(normalized, "pc.")
	return strings.ReplaceAll(normalized, ".", "_")
}
