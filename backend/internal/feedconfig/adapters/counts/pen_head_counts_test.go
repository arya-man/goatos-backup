package counts

import (
	"context"
	"testing"
	"time"

	fddomain "github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	fdports "github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/feedconfig/ports"
)

type fakeGrains struct {
	calls  []fdports.ProjectedGrainsRequest
	byPark map[string]map[string][]fddomain.ShedGrain
}

func (f *fakeGrains) ProjectedGrainsForSheds(_ context.Context, req fdports.ProjectedGrainsRequest) (map[string][]fddomain.ShedGrain, error) {
	f.calls = append(f.calls, req)
	return f.byPark[req.ParkID], nil
}

// A pen's count is every grain of that pen summed, matched with the sheet's own pen key, so
// "Part 3" on the config row and "part  3" on a grain are one pen. One projection read per park.
func TestPenCountsSumTheSheetsGrainsPerPenWithOneReadPerPark(t *testing.T) {
	grains := &fakeGrains{byPark: map[string]map[string][]fddomain.ShedGrain{
		"cbe": {
			"castro": {
				{PartitionLabel: "part  3", HeadCount: 10},
				{PartitionLabel: "Part 3", HeadCount: 5},
				{PartitionLabel: "Part 4", HeadCount: 8},
			},
			"yashoda": {{PartitionLabel: "", HeadCount: 3}},
		},
		"cpt": {"castro": {{PartitionLabel: "1", HeadCount: 20}}},
	}}
	pens := []ports.PenRef{
		{ParkID: "cbe", ShedID: "castro", PartitionLabel: "Part 3"},
		{ParkID: "cbe", ShedID: "yashoda"},
		{ParkID: "cpt", ShedID: "castro", PartitionLabel: "1"},
		{ParkID: "cbe", ShedID: "castro", PartitionLabel: "Part 9"},
	}
	got, err := New(grains).ProjectedPenHeadCounts(context.Background(), "t", pens, time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	want := []int64{15, 3, 20, 0}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("pen %+v = %d, want %d", pens[i], got[i], want[i])
		}
	}
	if len(grains.calls) != 2 {
		t.Errorf("one projection read per park, got %d", len(grains.calls))
	}
}
