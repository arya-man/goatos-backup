package app

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/tasks/domain"
)

// TestListWorkflowsAcceptsEveryModuleTheRouteAdmits pins the two vocabularies together. The route
// decides which modules a caller may ask for (hasModulePermission); this service decided a
// narrower list, so a module added to one and not the other answers an EMPTY page with
// `missing_required_field` -- no rows, and nothing on screen to say why.
//
// That is what the 2026-09-20 E2E hit: `sales` was added to the route with the Sales SOP in
// September and never here, so the sale list has always come back empty. The procurement modules
// would have shipped with the same hole.
func TestListWorkflowsAcceptsEveryModuleTheRouteAdmits(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, nil)
	for _, module := range []string{domain.ModuleBirth, domain.ModuleDeath, domain.ModuleGeneral, domain.ModuleSales, domain.ModuleProcurement} {
		if _, err := svc.ListWorkflows(context.Background(), ListWorkflowsInput{TenantID: "tenant", Module: module}); errors.Is(err, domain.ErrMissingRequiredField) {
			t.Fatalf("module %q is admitted by the route but refused here", module)
		}
	}
	// A module the route does not admit is still refused.
	if _, err := svc.ListWorkflows(context.Background(), ListWorkflowsInput{TenantID: "tenant", Module: "nonsense"}); !errors.Is(err, domain.ErrMissingRequiredField) {
		t.Fatalf("an unknown module must be refused, got %v", err)
	}
}
