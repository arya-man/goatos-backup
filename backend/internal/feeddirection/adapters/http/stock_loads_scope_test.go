package http

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

// STOCK LOADS ADMITS A FEED-DIRECTION READER (dddd18b93).
//
// The route table declares the door:
//
//	{OperationID: "getFeedAnalyticsStockLoads", ... AnyPermissions: {FeedAnalyticsStockRead, FeedDirectionRead}}
//
// and the handler resolves the caller's park scope a SECOND time, from its own permission list.
// Those two lists had drifted: the handler asked for FeedAnalyticsStockRead alone, so a principal
// the route let through resolved NO authorized parks inside the handler. dddd18b93 aligned them.
//
// WHY THE TEST THAT SHIPPED WITH IT DOES NOT BITE, which is the point of this file. The existing
// TestGetStockLoadsAcceptsFeedDirectionReader grants RoleFeedDirector -- a role holding BOTH
// permissions. Removing FeedDirectionRead from the handler changes nothing for that caller, so the
// test passes with the fix undone (verified: one of the disproved rows in the revert receipts). A
// fixture whose principal holds both halves of an OR can never show which half is load-bearing.
// The test has to be run by someone who holds ONLY the added one.
func TestStockLoadsAdmitsAPrincipalHoldingOnlyFeedDirectionRead(t *testing.T) {
	const (
		tenantID = "00000000-0000-4000-8000-000000000001"
		actorID  = "40000000-0000-4000-8000-000000000001"
		parkA    = "20000000-0000-4000-8000-000000000001"
	)
	role := roleHoldingOnlyFeedDirectionRead(t)

	service := &transportScopeSpyService{}
	req := httptest.NewRequest(http.MethodGet, "/feed-analytics/stock-loads?park_id="+parkA, nil)
	ctx := httpmiddleware.WithActorID(httpmiddleware.WithTenantID(req.Context(), tenantID), actorID)
	ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{{Role: role, ScopeType: "park", ScopeID: parkA}})
	recorder := httptest.NewRecorder()

	NewHandler(service, slog.Default()).GetStockLoads(recorder, req.WithContext(ctx))

	if recorder.Code != http.StatusOK {
		t.Fatalf("%s holds FeedDirectionRead and the route table admits it, but the handler answered %d: %s",
			role, recorder.Code, recorder.Body.String())
	}
	if service.stockLoadsCalls != 1 {
		t.Fatalf("stock loads calls=%d, want 1", service.stockLoadsCalls)
	}
	// The park scope must be resolved through the SAME permission, not left empty. An empty
	// authorized list is the quiet form of this defect: a 200 carrying no park, so the page renders
	// blank rather than refusing, and nobody reads a blank page as a permission problem.
	if got := service.stockLoadsInput.AuthorizedParkIDs; len(got) != 1 || got[0] != parkA {
		t.Fatalf("authorized parks=%v, want [%s] resolved through FeedDirectionRead", got, parkA)
	}
}

// The other side: widening the gate must not have opened it to everyone. A principal holding
// NEITHER permission still resolves no park and is refused.
func TestStockLoadsStillRefusesAPrincipalHoldingNeitherPermission(t *testing.T) {
	const (
		tenantID = "00000000-0000-4000-8000-000000000001"
		actorID  = "40000000-0000-4000-8000-000000000001"
		parkA    = "20000000-0000-4000-8000-000000000001"
	)
	role := permissions.RoleVerifier
	if permissions.RoleHasPermission(role, permissions.FeedDirectionRead) || permissions.RoleHasPermission(role, permissions.FeedAnalyticsStockRead) {
		// Pick another outsider rather than skip: a skip here would silently stop asserting that
		// the gate is a gate at all.
		role = permissions.RoleGrowthDirector
	}
	if permissions.RoleHasPermission(role, permissions.FeedDirectionRead) || permissions.RoleHasPermission(role, permissions.FeedAnalyticsStockRead) {
		t.Fatalf("no outsider role left to probe with: %s now holds a feed read permission", role)
	}

	service := &transportScopeSpyService{}
	req := httptest.NewRequest(http.MethodGet, "/feed-analytics/stock-loads?park_id="+parkA, nil)
	ctx := httpmiddleware.WithActorID(httpmiddleware.WithTenantID(req.Context(), tenantID), actorID)
	ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{{Role: role, ScopeType: "park", ScopeID: parkA}})
	recorder := httptest.NewRecorder()

	NewHandler(service, slog.Default()).GetStockLoads(recorder, req.WithContext(ctx))

	if recorder.Code == http.StatusOK && service.stockLoadsCalls > 0 {
		t.Fatalf("%s holds neither feed read permission but reached stock loads (status %d)", role, recorder.Code)
	}
}

// roleHoldingOnlyFeedDirectionRead names a role carrying FeedDirectionRead and NOT
// FeedAnalyticsStockRead, which is what makes the OR in the route table observable. It FAILS
// rather than skips when no such role is left: a skip would leave this file green while asserting
// nothing, which is the same silence the disproved test above was already producing.
func roleHoldingOnlyFeedDirectionRead(t *testing.T) string {
	t.Helper()
	for _, role := range []string{"director_feed", "head_feed", "manager_feed"} {
		if permissions.RoleHasPermission(role, permissions.FeedDirectionRead) &&
			!permissions.RoleHasPermission(role, permissions.FeedAnalyticsStockRead) {
			return role
		}
	}
	t.Fatal("no role holds FeedDirectionRead without FeedAnalyticsStockRead any more; " +
		"pick a new probe role here rather than deleting the check — the OR in the route table " +
		"is unobservable without one")
	return ""
}
