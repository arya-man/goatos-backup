package app

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
)

func strptr(s string) *string { return &s }

// The service is a thin read-model passthrough, so the contract worth pinning is that it hands
// every filter to the repository unchanged. A dropped filter here would silently widen the
// census population rather than fail, which is exactly the kind of bug that reads as "the
// numbers look a bit high" instead of as an error.
func TestGetBreakdownPassesEveryFilterThrough(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewHerdRegisterService(repo)

	want := domain.CountsBreakdownQuery{
		TenantID:        "tenant-1",
		LifecycleStatus: strptr("alive"),
		ParkID:          strptr("park-1"),
		ShedID:          strptr("shed-1"),
		ManagementStage: strptr("K1"),
		Breed:           strptr("Beetal"),
		Sex:             strptr("female"),
		Limit:           25,
		Offset:          50,
	}

	if _, err := svc.GetBreakdown(context.Background(), want); err != nil {
		t.Fatalf("GetBreakdown: %v", err)
	}

	got := repo.breakdownQuery
	if got.TenantID != want.TenantID {
		t.Errorf("tenant: got %q want %q", got.TenantID, want.TenantID)
	}
	for name, pair := range map[string][2]*string{
		"lifecycle_status": {got.LifecycleStatus, want.LifecycleStatus},
		"park_id":          {got.ParkID, want.ParkID},
		"shed_id":          {got.ShedID, want.ShedID},
		"management_stage": {got.ManagementStage, want.ManagementStage},
		"breed":            {got.Breed, want.Breed},
		"sex":              {got.Sex, want.Sex},
	} {
		gotVal, wantVal := pair[0], pair[1]
		if gotVal == nil || wantVal == nil || *gotVal != *wantVal {
			t.Errorf("%s: got %v want %v", name, gotVal, wantVal)
		}
	}
	if got.Limit != want.Limit || got.Offset != want.Offset {
		t.Errorf("paging: got limit=%d offset=%d want limit=%d offset=%d", got.Limit, got.Offset, want.Limit, want.Offset)
	}
}

// A repository failure must surface as an error so the page can render its honest
// "breakdown unavailable" state. Swallowing it would render an empty table that reads to an
// operator as "this tenant has no animals".
func TestGetBreakdownSurfacesRepositoryError(t *testing.T) {
	sentinel := errors.New("boom")
	repo := &fakeRepo{breakdownErr: sentinel}
	svc := NewHerdRegisterService(repo)

	if _, err := svc.GetBreakdown(context.Background(), domain.CountsBreakdownQuery{TenantID: "t"}); !errors.Is(err, sentinel) {
		t.Fatalf("got %v, want %v", err, sentinel)
	}
}
