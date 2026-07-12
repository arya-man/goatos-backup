package app

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// fakeOwnership resolves manager/backup keyed by shed id; an absent shed => nil owners (seed gap).
type fakeOwnership struct {
	managers map[string]*domain.ShedOwner
	backups  map[string]*domain.ShedOwner
	err      error
}

func (f fakeOwnership) ShedOwnership(_ context.Context, _, shedID, _ string, _ time.Time) (*domain.ShedOwner, *domain.ShedOwner, error) {
	if f.err != nil {
		return nil, nil, f.err
	}
	return f.managers[shedID], f.backups[shedID], nil
}

func (f fakeOwnership) ShedOwnerships(_ context.Context, _ string, sheds []domain.ShedOwnershipScope, _ time.Time) (map[string]domain.ShedOwnership, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make(map[string]domain.ShedOwnership, len(sheds))
	for _, shed := range sheds {
		out[shed.ShedID] = domain.ShedOwnership{Manager: f.managers[shed.ShedID], Backup: f.backups[shed.ShedID]}
	}
	return out, nil
}

func TestPlanSessionsSplitsAndClassifies(t *testing.T) {
	cfg := domain.CapacityConfig{MaxPerDay: 100, MaxBufferDays: 3} // allowed window = 4 days
	start := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name        string
		cells       int
		wantN       int
		wantStatus  domain.CapacityStatus
		wantPerDay  []int
		wantLastCap domain.CapacityStatus // capacity of the final planned day
	}{
		{"empty", 0, 0, domain.CapacityWithinCap, nil, ""},
		{"fits one day", 53, 1, domain.CapacityWithinCap, []int{53}, domain.CapacityWithinCap},
		{"exact one day", 100, 1, domain.CapacityWithinCap, []int{100}, domain.CapacityWithinCap},
		{"split three even", 300, 3, domain.CapacityOverCap, []int{100, 100, 100}, domain.CapacityWithinCap},
		{"split with remainder", 250, 3, domain.CapacityOverCap, []int{100, 100, 50}, domain.CapacityWithinCap},
		{"breach beyond window", 500, 5, domain.CapacityBreach, []int{100, 100, 100, 100, 100}, domain.CapacityBreach},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n, status, planned, err := PlanSessions(c.cells, cfg, start)
			if err != nil {
				t.Fatalf("PlanSessions: %v", err)
			}
			if n != c.wantN {
				t.Errorf("sessions = %d, want %d", n, c.wantN)
			}
			if status != c.wantStatus {
				t.Errorf("capacity = %q, want %q", status, c.wantStatus)
			}
			if len(planned) != len(c.wantPerDay) {
				t.Fatalf("planned days = %d, want %d", len(planned), len(c.wantPerDay))
			}
			total := 0
			for i, ps := range planned {
				if ps.Vaccinations != c.wantPerDay[i] {
					t.Errorf("day %d vaccinations = %d, want %d", i, ps.Vaccinations, c.wantPerDay[i])
				}
				if ps.DailyLimit != 100 {
					t.Errorf("day %d dailyLimit = %d, want 100", i, ps.DailyLimit)
				}
				total += ps.Vaccinations
			}
			if len(c.wantPerDay) > 0 {
				if total != c.cells {
					t.Errorf("planned total = %d, want cells %d", total, c.cells)
				}
				if planned[len(planned)-1].Capacity != c.wantLastCap {
					t.Errorf("last day capacity = %q, want %q", planned[len(planned)-1].Capacity, c.wantLastCap)
				}
			}
		})
	}
}

func TestPlanSessionsDatesAreConsecutive(t *testing.T) {
	cfg := domain.CapacityConfig{MaxPerDay: 100, MaxBufferDays: 3}
	start := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)
	_, _, planned, err := PlanSessions(300, cfg, start)
	if err != nil {
		t.Fatalf("PlanSessions: %v", err)
	}
	wantDates := []string{"2026-07-18", "2026-07-19", "2026-07-20"}
	for i, d := range wantDates {
		if planned[i].Date != d {
			t.Errorf("day %d date = %q, want %q", i, planned[i].Date, d)
		}
	}
}

func TestShedSummaryPassesThroughPlannerAndOwners(t *testing.T) {
	// Projection carries the SQL-computed Sessions/Capacity/Status (so filters page correctly); the
	// service maps them straight through, computes Done = Animals - Due, and attaches Manager/Backup.
	proj := []domain.ShedSummaryProjection{
		{ParkID: "p1", ParkName: "CBE", ShedID: "s1", ShedName: "Castro 1", Animals: 54, DueAnimals: 53, OpenCells: 53, Sessions: 1, Capacity: domain.CapacityWithinCap, Status: domain.ShedStatusDue, TotalCount: 2},
		{ParkID: "p1", ParkName: "CBE", ShedID: "s2", ShedName: "Godell 1", Animals: 300, DueAnimals: 300, OpenCells: 300, Sessions: 3, Capacity: domain.CapacityOverCap, Status: domain.ShedStatusSplit, TotalCount: 2},
	}
	own := fakeOwnership{
		managers: map[string]*domain.ShedOwner{
			"s1": {WorkforceMemberID: "m-jaya", DisplayName: "Jayamangal Kumar"},
			// s2: no manager -> seed gap
		},
		backups: map[string]*domain.ShedOwner{
			"s1": {WorkforceMemberID: "m-darshan", DisplayName: "Darshan"},
		},
	}
	svc := NewService(fakeRepo{shedRows: proj}, own)

	resp, err := svc.ShedSummary(context.Background(), domain.ShedSummaryQuery{TenantID: "t1", Limit: 25})
	if err != nil {
		t.Fatalf("ShedSummary: %v", err)
	}
	if len(resp.Rows) != 2 || resp.Page.Total != 2 {
		t.Fatalf("rows=%d total=%d", len(resp.Rows), resp.Page.Total)
	}
	r0 := resp.Rows[0]
	if r0.Animals != 54 || r0.Due != 53 || r0.Done != 1 {
		t.Errorf("Castro counts %d/%d/%d", r0.Animals, r0.Due, r0.Done)
	}
	if r0.Due+r0.Done != r0.Animals {
		t.Errorf("Due+Done != Animals")
	}
	if r0.Sessions != 1 || r0.Capacity != domain.CapacityWithinCap || r0.Status != domain.ShedStatusDue {
		t.Errorf("Castro planner passthrough: sessions=%d cap=%q status=%q", r0.Sessions, r0.Capacity, r0.Status)
	}
	if r0.Manager == nil || r0.Manager.DisplayName != "Jayamangal Kumar" || r0.Backup == nil || r0.Backup.DisplayName != "Darshan" {
		t.Errorf("Castro owners: %+v / %+v", r0.Manager, r0.Backup)
	}
	r1 := resp.Rows[1]
	if r1.Sessions != 3 || r1.Capacity != domain.CapacityOverCap || r1.Status != domain.ShedStatusSplit {
		t.Errorf("Godell planner passthrough: sessions=%d cap=%q status=%q", r1.Sessions, r1.Capacity, r1.Status)
	}
	if r1.Manager != nil || r1.Backup != nil {
		t.Errorf("Godell should be a manager/backup seed gap (nil), got %+v/%+v", r1.Manager, r1.Backup)
	}
}

func TestShedDetailBuildsPlannedSessionsAndHeader(t *testing.T) {
	next := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)
	proj := []domain.ShedSummaryProjection{
		{ParkID: "p1", ParkName: "CBE", ShedID: "s1", ShedName: "Godell 1", Animals: 300, DueAnimals: 300, OpenCells: 250, Sessions: 3, Capacity: domain.CapacityOverCap, Status: domain.ShedStatusSplit, NextDue: &next, TotalCount: 1},
	}
	ops := []domain.OperationsRow{
		{ParkID: "p1", ShedID: "s1", ProtocolID: "fmd", ProtocolName: "FMD", Stage: "kid", Animals: 150, DueCount: 150, TotalCount: 150},
		{ParkID: "p1", ShedID: "s1", ProtocolID: "hs", ProtocolName: "HS", Stage: "kid", Animals: 100, DueCount: 100, TotalCount: 100},
	}
	svc := NewService(fakeRepo{shedRows: proj, opsRows: ops}) // default cap config 100/3

	detail, found, err := svc.ShedDetail(context.Background(), "s1", domain.OperationsQuery{TenantID: "t1"})
	if err != nil || !found {
		t.Fatalf("ShedDetail found=%v err=%v", found, err)
	}
	if detail.Animals != 300 || detail.Due != 300 || detail.Done != 0 {
		t.Errorf("header %d/%d/%d", detail.Animals, detail.Due, detail.Done)
	}
	if detail.Sessions != 3 || detail.Capacity != domain.CapacityOverCap || detail.Status != domain.ShedStatusSplit {
		t.Errorf("header planner: sessions=%d cap=%q status=%q", detail.Sessions, detail.Capacity, detail.Status)
	}
	// Planned sessions re-planned from 250 open cells at cap 100 -> 100/100/50 across 3 days from next_due.
	if len(detail.PlannedSessions) != 3 {
		t.Fatalf("planned sessions = %d, want 3", len(detail.PlannedSessions))
	}
	wantVax := []int{100, 100, 50}
	for i, ps := range detail.PlannedSessions {
		if ps.Vaccinations != wantVax[i] || ps.DailyLimit != 100 || ps.Capacity != domain.CapacityWithinCap {
			t.Errorf("planned[%d] = %+v", i, ps)
		}
	}
	if detail.PlannedSessions[0].Date != "2026-07-18" {
		t.Errorf("first session date = %q", detail.PlannedSessions[0].Date)
	}
	if len(detail.Vaccines) != 2 {
		t.Errorf("vaccines = %d, want 2 (FMD, HS)", len(detail.Vaccines))
	}
}

func TestShedDetailNotFoundWhenNoAliveAnimals(t *testing.T) {
	svc := NewService(fakeRepo{}) // no shed rows
	_, found, err := svc.ShedDetail(context.Background(), "s-missing", domain.OperationsQuery{TenantID: "t1"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if found {
		t.Errorf("expected not found for a shed with no alive animals")
	}
}

func TestShedAnimalsKeysetCursor(t *testing.T) {
	rows := make([]domain.ShedAnimalRow, 3)
	for i := range rows {
		rows[i] = domain.ShedAnimalRow{GoatID: string(rune('a' + i)), DisplayID: "G", Status: "done"}
	}
	svc := NewService(fakeRepo{shedAnimals: rows})

	page, err := svc.ShedAnimals(context.Background(), domain.ShedAnimalQuery{TenantID: "t1", ShedID: "s1", Limit: 3})
	if err != nil {
		t.Fatalf("ShedAnimals: %v", err)
	}
	if page.NextCursor == nil || *page.NextCursor != "c" {
		t.Errorf("want next cursor 'c', got %v", page.NextCursor)
	}

	page2, err := svc.ShedAnimals(context.Background(), domain.ShedAnimalQuery{TenantID: "t1", ShedID: "s1", Limit: 10})
	if err != nil {
		t.Fatalf("ShedAnimals: %v", err)
	}
	if page2.NextCursor != nil {
		t.Errorf("want nil cursor when exhausted, got %v", *page2.NextCursor)
	}
}
