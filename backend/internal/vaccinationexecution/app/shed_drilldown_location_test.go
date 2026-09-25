package app

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

func gandhiPen(partition string) domain.ExecutionProjection {
	return projection("e2f9bd7e-43a6-5000-8c9e-16ff4fd682f7", time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC), 1, func(p *domain.ExecutionProjection) {
		p.ShedName = "Gandhi"
		p.PhysicalShed = "Gandhi"
		p.Partition = partition
	})
}

func drilldownQuery(shedID string, partition *string) domain.ExecutionQuery {
	return domain.ExecutionQuery{
		TenantID:       "tenant",
		ShedID:         &shedID,
		PartitionLabel: partition,
		AsOf:           time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC),
		DueBefore:      time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC),
		Limit:          20,
	}
}

// A drilldown spanning Gandhi 1, 2 and 3 is about the building, not about whichever pen sorted
// first: the header must read "Gandhi", never "Gandhi 1" (the pen board opened "Gandhi 2" onto a
// page titled "Gandhi 1"). Rows all in one pen still name that pen.
func TestShedDrilldownHeaderIsAgreeOrGoBare(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		rows      []domain.ExecutionProjection
		display   string
		partition string
	}{
		{"three pens go bare", []domain.ExecutionProjection{gandhiPen("1"), gandhiPen("3"), gandhiPen("2")}, "Gandhi", ""},
		{"one pen is named", []domain.ExecutionProjection{gandhiPen("2"), gandhiPen("2")}, "Gandhi 2", "2"},
		{"worded pen is named", []domain.ExecutionProjection{func() domain.ExecutionProjection {
			p := gandhiPen("Part 3")
			p.ShedName, p.PhysicalShed = "Godel 1", "Godel 1"
			return p
		}()}, "Godel 1 - Part 3", "Part 3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewService(fakeRepo{rows: tc.rows})
			got, found, err := svc.ShedDrilldown(context.Background(), drilldownQuery(tc.rows[0].ShedID, nil))
			if err != nil || !found {
				t.Fatalf("ShedDrilldown found=%v err=%v", found, err)
			}
			if got.OperationalLocationDisplay != tc.display {
				t.Fatalf("header = %q, want %q", got.OperationalLocationDisplay, tc.display)
			}
			if stringPtrValue(got.PartitionLabel) != tc.partition {
				t.Fatalf("partition = %q, want %q", stringPtrValue(got.PartitionLabel), tc.partition)
			}
			if got.ShedName != tc.rows[0].PhysicalShed {
				t.Fatalf("shed name = %q, want the physical shed %q", got.ShedName, tc.rows[0].PhysicalShed)
			}
		})
	}
}

// Castro 1/2 are pens on the board (from ShedSummary) with no drive work in the execution window.
// Their drilldown used to 404 ("shed vaccination execution was not found"), making every such board
// row a dead end. It now opens with the pen's identity and no drive rows; a pen that is not on the
// board -- an unknown partition, or a park outside the caller's scope -- still reads not-found.
func TestShedDrilldownOpensABoardPenWithNoDriveWork(t *testing.T) {
	t.Parallel()
	const castro = "62241795-628e-58ef-9591-aa384fb0f0f7"
	const park = "00000000-0000-4000-8000-000000003002"
	label := func(v string) *string { return &v }
	repo := fakeRepo{shedRows: []domain.ShedSummaryProjection{
		{ParkID: park, ParkName: "Channapatna", ShedID: castro, ShedName: "Castro", PartitionLabel: label("1"), Animals: 31},
		{ParkID: park, ParkName: "Channapatna", ShedID: castro, ShedName: "Castro", PartitionLabel: label("2"), Animals: 32},
	}}
	svc := NewService(repo)

	got, found, err := svc.ShedDrilldown(context.Background(), drilldownQuery(castro, label("2")))
	if err != nil || !found {
		t.Fatalf("Castro 2 drilldown found=%v err=%v, want an opened pen", found, err)
	}
	if got.OperationalLocationDisplay != "Castro 2" || stringPtrValue(got.PartitionLabel) != "2" || got.ParkName != "Channapatna" {
		t.Fatalf("header = %q / %q / %q, want Castro 2 in Channapatna", got.OperationalLocationDisplay, stringPtrValue(got.PartitionLabel), got.ParkName)
	}
	if got.Rows == nil || len(got.Rows) != 0 || got.Drives == nil || got.AnimalStages == nil {
		t.Fatalf("an empty pen must serialise empty lists, not null: rows=%v drives=%v stages=%v", got.Rows, got.Drives, got.AnimalStages)
	}

	whole, found, err := svc.ShedDrilldown(context.Background(), drilldownQuery(castro, nil))
	if err != nil || !found || whole.OperationalLocationDisplay != "Castro" || whole.PartitionLabel != nil {
		t.Fatalf("building drilldown = %q partition=%v found=%v err=%v, want bare Castro", whole.OperationalLocationDisplay, whole.PartitionLabel, found, err)
	}

	if _, found, _ := svc.ShedDrilldown(context.Background(), drilldownQuery(castro, label("9"))); found {
		t.Fatal("a partition the board does not list must stay not-found")
	}
	scoped := drilldownQuery(castro, label("1"))
	scoped.AuthorizedParkIDs = []string{"00000000-0000-4000-8000-000000003001"}
	if _, found, _ := svc.ShedDrilldown(context.Background(), scoped); found {
		t.Fatal("a pen outside the caller's parks must stay not-found")
	}
}
