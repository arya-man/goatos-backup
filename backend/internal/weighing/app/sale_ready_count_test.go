package app

import (
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// personActor is a principal whose permissions came from their People / HRMS ticks.
func personActor(perms ...string) domain.Actor {
	return domain.Actor{TenantID: swTenant, Roles: []string{permissions.RoleFeedDirector}, Permissions: perms, PermissionsResolved: true}
}

// TestFarmValueReadersGetTheOver35CountAndNothingElse is bug 8 of the People / HRMS fixes
// (2026-10-02): the Sales > Farm value "Over 35 kg" card read the whole shed-weights report, so it
// needed Weighing, and was gated on a ROLE carrying it. Maintainer: "he should see that value
// whether he has weighing page access or not". The count is now open to SalesRead -- and only the
// count: the full report stays refused.
func TestFarmValueReadersGetTheOver35CountAndNothingElse(t *testing.T) {
	repo := &shedWeightsRepo{parks: []domain.WeighingPark{{ParkID: swParkA, Name: "A"}, {ParkID: swParkB, Name: "B"}}}
	repo.shedWeights.Summary.AtOrAbove35Kg = 42
	svc := NewService(repo)
	// A Feed Director ROLE (no sales, no weighing) with Sales ticked for park A only.
	ctx := httpmiddleware.WithPersonParkScope(swContext(permissions.ActiveGrant{Role: permissions.RoleFeedDirector, ScopeType: "tenant", ScopeID: swTenant}),
		httpmiddleware.PersonParkScope{ParkIDs: []string{swParkA}})
	reader := personActor(permissions.SalesRead)

	got, err := svc.GetSaleReadyCount(ctx, reader, "", "", "", "0", 0, 0)
	if err != nil {
		t.Fatalf("a Farm value reader was refused the count: %v", err)
	}
	if got.AtOrAbove35Kg != 42 {
		t.Fatalf("count = %d, want 42", got.AtOrAbove35Kg)
	}
	if len(repo.gotScopeParkIDs) != 1 || repo.gotScopeParkIDs[0] != swParkA {
		t.Fatalf("count read over %v, want only the reader's own park %s", repo.gotScopeParkIDs, swParkA)
	}
	if _, err := svc.GetSaleReadyCount(ctx, reader, swParkB, "", "", "0", 0, 0); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("a park outside the reader's scope: err = %v, want not found", err)
	}
	if _, err := svc.GetShedWeights(ctx, reader, "", "", "", "", "", "", "", 0, 0); !errors.Is(err, ports.ErrForbidden) {
		t.Fatalf("the full Weights report must stay closed to a sales-only reader: err = %v", err)
	}
	if _, err := svc.GetSaleReadyCount(ctx, personActor(), "", "", "", "0", 0, 0); !errors.Is(err, ports.ErrForbidden) {
		t.Fatalf("someone with neither Sales nor Weighing: err = %v, want forbidden", err)
	}
}

// TestWeighingTickedOnPeopleOpensTheWeightsReport: the park scope behind every Weights read came
// from ROLE grants only, so a person ticked for Weighing on People / HRMS whose role carries no
// weighing.monitor resolved to no parks and got "not found". The person's own scope decides now.
func TestWeighingTickedOnPeopleOpensTheWeightsReport(t *testing.T) {
	repo := &shedWeightsRepo{parks: []domain.WeighingPark{{ParkID: swParkA, Name: "A"}, {ParkID: swParkB, Name: "B"}}}
	svc := NewService(repo)
	ctx := httpmiddleware.WithPersonParkScope(swContext(permissions.ActiveGrant{Role: permissions.RoleFeedDirector, ScopeType: "tenant", ScopeID: swTenant}),
		httpmiddleware.PersonParkScope{TenantWide: true})
	if _, err := svc.GetShedWeights(ctx, personActor(permissions.WeighingMonitor), "", "", "", "", "", "", "", 0, 0); err != nil {
		t.Fatalf("a person ticked for Weighing was refused the Weights report: %v", err)
	}
	if len(repo.gotScopeParkIDs) != 2 {
		t.Fatalf("scope = %v, want both parks", repo.gotScopeParkIDs)
	}
}
