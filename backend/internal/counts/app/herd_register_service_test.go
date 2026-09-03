package app

import (
	"context"
	"errors"
	"reflect"
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
		TenantID:         "tenant-1",
		LifecycleStatus:  strptr("alive"),
		ParkIDs:          []string{"park-1", "park-2"},
		Pens:             []domain.CountsBreakdownPen{{ShedID: "shed-1"}, {ShedID: "shed-2", PartitionLabel: "Part 3"}},
		ManagementStages: []string{"K1", "K2"},
		Breeds:           []string{"Beetal"},
		Sexes:            []string{"female"},
		Limit:            25,
		Offset:           50,
	}

	if _, err := svc.GetBreakdown(context.Background(), want); err != nil {
		t.Fatalf("GetBreakdown: %v", err)
	}

	got := repo.breakdownQuery
	if got.TenantID != want.TenantID {
		t.Errorf("tenant: got %q want %q", got.TenantID, want.TenantID)
	}
	if got.LifecycleStatus == nil || want.LifecycleStatus == nil || *got.LifecycleStatus != *want.LifecycleStatus {
		t.Errorf("lifecycle_status: got %v want %v", got.LifecycleStatus, want.LifecycleStatus)
	}
	for name, pair := range map[string][2][]string{
		"park_ids":          {got.ParkIDs, want.ParkIDs},
		"management_stages": {got.ManagementStages, want.ManagementStages},
		"breeds":            {got.Breeds, want.Breeds},
		"sexes":             {got.Sexes, want.Sexes},
	} {
		if !reflect.DeepEqual(pair[0], pair[1]) {
			t.Errorf("%s: got %v want %v", name, pair[0], pair[1])
		}
	}
	if !reflect.DeepEqual(got.Pens, want.Pens) {
		t.Errorf("pens: got %v want %v", got.Pens, want.Pens)
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
