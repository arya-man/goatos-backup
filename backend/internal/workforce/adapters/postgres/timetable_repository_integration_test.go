package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

const (
	ttTenant = "00000000-0000-4000-8000-0000000000a7"
	// seededTenant owns the CBE/CPT parks the migrated template already carries.
	seededTenant = "00000000-0000-4000-8000-000000000001"
	ttCBE        = "7a000000-0000-4000-8000-000000000201"
	ttCPT        = "7a000000-0000-4000-8000-000000000202"
	ttActor      = "91000000-0000-4000-8000-000000000400"
	ttMissing    = "97000000-0000-4000-8000-0000000004ff"
)

func ttMember(i int) string { return fmt.Sprintf("97000000-0000-4000-8000-%012d", 400+i) }

func ptr(v int) *int { return &v }

// TestTimetableRepositoryWithDockerPostgres proves the HRMS Timetable storage (maintainer request
// 2026-09-30, migration 000457) on the real database: the shift catalog is seeded; a park with
// no stored hours reads every shift unset; hours are per park; a write is fenced on row_version,
// audited in the same transaction, and an exact replay writes nothing; the people read is one
// park's ACTIVE members, keyset-paged, filterable by shift, and the whole-park counts sum to the
// same member set; and a person's shift is set, changed, cleared and refused for an unknown shift.
func TestTimetableRepositoryWithDockerPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	mustExec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v\n%s", err, sql)
		}
	}
	// Migration 000457 seeded the two parks' stated hours IN PLACE: Morning CBE 06:00 / CPT 07:00
	// with no end, General 08:30-18:00, Second 15:00-24:00 at both.
	seeded, err := pool.Query(ctx, `
SELECT l.location_code, t.shift_code, t.start_minute, COALESCE(t.end_minute, -1)
FROM workforce_park_shift_timings t
JOIN locations l ON l.location_id = t.park_id
WHERE t.tenant_id = $1::uuid
ORDER BY 1, 2`, seededTenant)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for seeded.Next() {
		var code, shift string
		var start, end int
		if err := seeded.Scan(&code, &shift, &start, &end); err != nil {
			t.Fatal(err)
		}
		got = append(got, fmt.Sprintf("%s/%s/%d/%d", code, shift, start, end))
	}
	seeded.Close()
	if fmt.Sprint(got) != "[CBE/general/510/1080 CBE/morning/360/-1 CBE/second/900/1440 CPT/general/510/1080 CPT/morning/420/-1 CPT/second/900/1440]" {
		t.Fatalf("seeded park hours = %v", got)
	}

	mustExec(`INSERT INTO tenants (tenant_id, name, status) VALUES ($1, 'Timetable Tenant', 'active') ON CONFLICT (tenant_id) DO NOTHING`, ttTenant)
	mustExec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status) VALUES ($1::uuid, $2::uuid, 'park', 'CPT', 'Channapatna', 'active')`, ttCPT, ttTenant)
	mustExec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status) VALUES ($1::uuid, $2::uuid, 'park', 'CBE', 'Coimbatore', 'active')`, ttCBE, ttTenant)
	// Five active people at CPT, one at CBE, one INACTIVE at CPT who must be counted nowhere.
	for i, m := range []struct{ name, park, status string }{
		{"Amit", ttCPT, "active"}, {"bhavya", ttCPT, "active"}, {"Chetan", ttCPT, "active"},
		{"Darshan", ttCPT, "active"}, {"Esha", ttCPT, "active"}, {"Farid", ttCBE, "active"},
		{"Gone", ttCPT, "inactive"},
	} {
		mustExec(`
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_location_id)
VALUES ($1::uuid, $2::uuid, $3, $3, $4, $5::uuid)`, ttMember(i), ttTenant, m.name, m.status, m.park)
	}
	repo := NewRepository(pool, 5*time.Second)

	parks, err := repo.TimetableParks(ctx, ttTenant)
	if err != nil || len(parks) != 2 || parks[0].Code != "CBE" || parks[1].Code != "CPT" {
		t.Fatalf("parks must come in park-code order (CBE before CPT): %+v %v", parks, err)
	}

	// A park with nothing stored: the catalog's three shifts, in order, all unset.
	timings, err := repo.ParkShiftTimings(ctx, ttTenant, ttCPT)
	if err != nil || len(timings) != 3 || timings[0].ShiftCode != "morning" || timings[1].ShiftCode != "general" || timings[2].ShiftCode != "second" {
		t.Fatalf("catalog = %+v %v", timings, err)
	}
	for _, s := range timings {
		if s.StartMinute != nil || s.EndMinute != nil || s.RowVersion != 0 {
			t.Fatalf("unset shift %s = %+v", s.ShiftCode, s)
		}
	}

	// Per-park hours: CPT's Morning starts at 7 with no end; CBE is untouched.
	morning, err := repo.SetParkShiftTiming(ctx, ttTenant, ttActor, ttCPT, "morning", ptr(420), nil, 0)
	if err != nil || *morning.StartMinute != 420 || morning.EndMinute != nil || morning.RowVersion != 1 {
		t.Fatalf("set CPT morning = %+v %v", morning, err)
	}
	if cbe, _ := repo.ParkShiftTimings(ctx, ttTenant, ttCBE); cbe[0].StartMinute != nil {
		t.Fatalf("CPT's hours leaked into CBE: %+v", cbe[0])
	}
	// Exact replay with the stale version: success, nothing written, no second audit row.
	replay, err := repo.SetParkShiftTiming(ctx, ttTenant, ttActor, ttCPT, "morning", ptr(420), nil, 0)
	if err != nil || replay.RowVersion != 1 {
		t.Fatalf("replay = %+v %v", replay, err)
	}
	// A different value on the stale version is someone else's edit: refused.
	if _, err := repo.SetParkShiftTiming(ctx, ttTenant, ttActor, ttCPT, "morning", ptr(360), nil, 0); !errors.Is(err, ports.ErrTimetableVersionConflict) {
		t.Fatalf("stale write = %v, want version conflict", err)
	}
	// The end set later, on the current version; the Second shift may end at midnight (1440).
	if row, err := repo.SetParkShiftTiming(ctx, ttTenant, ttActor, ttCPT, "morning", ptr(420), ptr(930), 1); err != nil || row.RowVersion != 2 || *row.EndMinute != 930 {
		t.Fatalf("set morning end = %+v %v", row, err)
	}
	if _, err := repo.SetParkShiftTiming(ctx, ttTenant, ttActor, ttCPT, "second", ptr(900), ptr(1440), 0); err != nil {
		t.Fatalf("second shift to midnight: %v", err)
	}
	if _, err := repo.SetParkShiftTiming(ctx, ttTenant, ttActor, ttCPT, "night", ptr(0), nil, 0); !errors.Is(err, ports.ErrUnknownShift) {
		t.Fatalf("unknown shift = %v", err)
	}
	if _, err := repo.SetParkShiftTiming(ctx, ttTenant, ttActor, ttMissing, "morning", ptr(0), nil, 0); !errors.Is(err, ports.ErrUnknownPark) {
		t.Fatalf("unknown park = %v", err)
	}
	var timingAudits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = 'workforce.timetable.shift_timing'`, ttTenant).Scan(&timingAudits); err != nil || timingAudits != 3 {
		t.Fatalf("timing audit rows = %d %v, want 3 (the replay writes none)", timingAudits, err)
	}

	// People: set, replay, change, clear.
	amit := ttMember(0)
	row, err := repo.SetMemberShift(ctx, ttTenant, ttActor, amit, "general", 0)
	if err != nil || row.ShiftCode != "general" || row.RowVersion != 1 || row.ParkID != ttCPT {
		t.Fatalf("set amit = %+v %v", row, err)
	}
	if row, err = repo.SetMemberShift(ctx, ttTenant, ttActor, amit, "general", 0); err != nil || row.RowVersion != 1 {
		t.Fatalf("replay amit = %+v %v", row, err)
	}
	if _, err := repo.SetMemberShift(ctx, ttTenant, ttActor, amit, "second", 0); !errors.Is(err, ports.ErrTimetableVersionConflict) {
		t.Fatalf("stale person write = %v", err)
	}
	if row, err = repo.SetMemberShift(ctx, ttTenant, ttActor, amit, "second", 1); err != nil || row.ShiftCode != "second" || row.RowVersion != 2 {
		t.Fatalf("change amit = %+v %v", row, err)
	}
	if _, err := repo.SetMemberShift(ctx, ttTenant, ttActor, ttMember(1), "general", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetMemberShift(ctx, ttTenant, ttActor, ttMember(2), "general", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetMemberShift(ctx, ttTenant, ttActor, ttMember(3), "night", 0); !errors.Is(err, ports.ErrUnknownShift) {
		t.Fatalf("unknown shift for a person = %v", err)
	}
	if _, err := repo.SetMemberShift(ctx, ttTenant, ttActor, ttMember(6), "general", 0); !errors.Is(err, ports.ErrPersonNotFound) {
		t.Fatalf("inactive person = %v, want not found", err)
	}
	if row, err = repo.SetMemberShift(ctx, ttTenant, ttActor, ttMember(2), "", 1); err != nil || row.ShiftCode != "" || row.RowVersion != 0 {
		t.Fatalf("clear chetan = %+v %v", row, err)
	}

	// Whole-park counts over the SAME member set the list pages: CPT has 5 active people --
	// Amit on second, bhavya on general, three on nothing; the inactive one is nowhere.
	counts, err := repo.ParkShiftHeadcount(ctx, ttTenant, ttCPT)
	if err != nil || counts["second"] != 1 || counts["general"] != 1 || counts[""] != 3 || len(counts) != 3 {
		t.Fatalf("CPT counts = %+v %v", counts, err)
	}

	// Keyset pages of two, case-insensitive name order, never repeating a person.
	var names []string
	cursor := ""
	for page := 0; page < 5; page++ {
		items, next, err := repo.ListTimetablePeople(ctx, ports.ListTimetablePeopleParams{TenantID: ttTenant, ParkID: ttCPT, Cursor: cursor, Limit: 2})
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range items {
			names = append(names, it.DisplayName)
		}
		if next == "" {
			break
		}
		cursor = next
	}
	if fmt.Sprint(names) != "[Amit bhavya Chetan Darshan Esha]" {
		t.Fatalf("paged names = %v", names)
	}
	for filter, want := range map[string]int{"general": 1, "second": 1, "unassigned": 3, "morning": 0} {
		items, _, err := repo.ListTimetablePeople(ctx, ports.ListTimetablePeopleParams{TenantID: ttTenant, ParkID: ttCPT, Shift: filter, Limit: 50})
		if err != nil || len(items) != want || (filter != "unassigned" && counts[filter] != want) {
			t.Fatalf("filter %s = %d rows %v, want %d", filter, len(items), err, want)
		}
	}
	var memberAudits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = 'workforce.timetable.member_shift'`, ttTenant).Scan(&memberAudits); err != nil || memberAudits != 5 {
		t.Fatalf("member audit rows = %d %v, want 5", memberAudits, err)
	}

	// The table's own CHECKs back the service rule: no end without a start, no empty shift.
	if _, err := pool.Exec(ctx, `INSERT INTO workforce_park_shift_timings (tenant_id, park_id, shift_code, end_minute) VALUES ($1, $2, 'general', 600)`, ttTenant, ttCBE); err == nil {
		t.Fatal("an end without a start must be refused by the table")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO workforce_park_shift_timings (tenant_id, park_id, shift_code, start_minute, end_minute) VALUES ($1, $2, 'general', 0, 1440)`, ttTenant, ttCBE); err == nil {
		t.Fatal("a shift ending when it starts must be refused by the table")
	}
}

// ttSeed builds one tenant with two parks and hands back a helper that adds members.
func ttSeed(t *testing.T, ctx context.Context, tenant string) (*Repository, func(sql string, args ...any), func(id int, name, park, status string)) {
	t.Helper()
	pgtest.SkipIfNoDocker(t)
	pool := pgtest.StartPostgres(t, ctx)
	t.Cleanup(pool.Close)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v\n%s", err, sql)
		}
	}
	exec(`INSERT INTO tenants (tenant_id, name, status) VALUES ($1, 'Timetable Tenant', 'active') ON CONFLICT (tenant_id) DO NOTHING`, tenant)
	exec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status) VALUES ($1::uuid, $2::uuid, 'park', 'CPT', 'Channapatna', 'active')`, ttCPT, tenant)
	exec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status) VALUES ($1::uuid, $2::uuid, 'park', 'CBE', 'Coimbatore', 'active')`, ttCBE, tenant)
	member := func(id int, name, park, status string) {
		t.Helper()
		exec(`INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_location_id)
VALUES ($1::uuid, $2::uuid, $3, $3, $4, $5::uuid)`, ttMember(id), tenant, name, status, park)
	}
	return NewRepository(pool, 5*time.Second), exec, member
}

func ttListAll(t *testing.T, ctx context.Context, repo *Repository, tenant, park, shift string, limit int) ([]string, int) {
	t.Helper()
	var names []string
	pages, cursor := 0, ""
	for {
		items, next, err := repo.ListTimetablePeople(ctx, ports.ListTimetablePeopleParams{TenantID: tenant, ParkID: park, Shift: shift, Cursor: cursor, Limit: limit})
		if err != nil {
			t.Fatal(err)
		}
		pages++
		for _, it := range items {
			names = append(names, it.DisplayName)
		}
		if next == "" || pages > 20 {
			return names, pages
		}
		cursor = next
	}
}

// A person joins to a designation, a department and a shift, and the park carries three timing
// rows: none of those sides may multiply the person. Counts and the paged list must both see
// each person exactly once.
func TestTimetableHeadcountOneToManyJoinsCountEachPersonOnce(t *testing.T) {
	ctx := context.Background()
	repo, exec, member := ttSeed(t, ctx, ttTenant)
	for i, name := range []string{"Amit", "Bhavya", "Chetan"} {
		member(i, name, ttCPT, "active")
		exec(`INSERT INTO person_access (tenant_id, workforce_member_id, designation_code) VALUES ($1::uuid, $2::uuid, 'manager_feed')`, ttTenant, ttMember(i))
		exec(`UPDATE workforce_members SET department_id = (SELECT department_id FROM departments WHERE tenant_id = $1::uuid LIMIT 1) WHERE workforce_member_id = $2::uuid`, ttTenant, ttMember(i))
	}
	for _, code := range []string{"morning", "general", "second"} {
		if _, err := repo.SetParkShiftTiming(ctx, ttTenant, ttActor, ttCPT, code, ptr(360), ptr(900), 0); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 3 {
		if _, err := repo.SetMemberShift(ctx, ttTenant, ttActor, ttMember(i), "general", 0); err != nil {
			t.Fatal(err)
		}
	}
	counts, err := repo.ParkShiftHeadcount(ctx, ttTenant, ttCPT)
	if err != nil || counts["general"] != 3 || len(counts) != 1 {
		t.Fatalf("counts = %+v %v, want general=3 only", counts, err)
	}
	if names, _ := ttListAll(t, ctx, repo, ttTenant, ttCPT, "", 50); fmt.Sprint(names) != "[Amit Bhavya Chetan]" {
		t.Fatalf("list = %v", names)
	}
}

// Keyset paging at the exact boundary: four people in pages of two is two pages and no third
// empty one; a page as large as the park is one page with no cursor.
func TestTimetablePeoplePageBoundary(t *testing.T) {
	ctx := context.Background()
	repo, _, member := ttSeed(t, ctx, ttTenant)
	for i, name := range []string{"d", "B", "a", "C"} {
		member(i, name, ttCPT, "active")
	}
	names, pages := ttListAll(t, ctx, repo, ttTenant, ttCPT, "", 2)
	if fmt.Sprint(names) != "[a B C d]" || pages != 2 {
		t.Fatalf("pages of 2 = %v in %d pages, want [a B C d] in 2", names, pages)
	}
	if names, pages = ttListAll(t, ctx, repo, ttTenant, ttCPT, "", 4); len(names) != 4 || pages != 1 {
		t.Fatalf("one full page = %v in %d pages", names, pages)
	}
}

// The park is the scope: another park's people, and the same park id under another tenant, are
// never counted or listed.
func TestTimetableParkScopeKeepsParksAndTenantsApart(t *testing.T) {
	ctx := context.Background()
	repo, exec, member := ttSeed(t, ctx, ttTenant)
	member(0, "Cpt one", ttCPT, "active")
	member(1, "Cpt two", ttCPT, "active")
	member(2, "Cbe one", ttCBE, "active")
	other := "00000000-0000-4000-8000-0000000000a8"
	exec(`INSERT INTO tenants (tenant_id, name, status) VALUES ($1, 'Other', 'active')`, other)
	exec(`INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_location_id)
VALUES ($1::uuid, $2::uuid, 'Stranger', 'Stranger', 'active', $3::uuid)`, ttMember(9), other, ttCPT)
	counts, err := repo.ParkShiftHeadcount(ctx, ttTenant, ttCPT)
	if err != nil || counts[""] != 2 || len(counts) != 1 {
		t.Fatalf("CPT counts = %+v %v, want 2 unassigned", counts, err)
	}
	if names, _ := ttListAll(t, ctx, repo, ttTenant, ttCPT, "", 50); fmt.Sprint(names) != "[Cpt one Cpt two]" {
		t.Fatalf("CPT list = %v", names)
	}
	if names, _ := ttListAll(t, ctx, repo, ttTenant, ttCBE, "", 50); fmt.Sprint(names) != "[Cbe one]" {
		t.Fatalf("CBE list = %v", names)
	}
}

// Every member status the table allows: only ACTIVE people work a shift, so only they are
// counted, listed, or can be put on one.
func TestTimetableStatusMatrixCountsOnlyActivePeople(t *testing.T) {
	ctx := context.Background()
	repo, _, member := ttSeed(t, ctx, ttTenant)
	statuses := []string{"candidate", "active", "inactive", "suspended", "left"}
	for i, status := range statuses {
		member(i, "Person "+status, ttCPT, status)
	}
	counts, err := repo.ParkShiftHeadcount(ctx, ttTenant, ttCPT)
	if err != nil || counts[""] != 1 || len(counts) != 1 {
		t.Fatalf("counts = %+v %v, want exactly the one active person", counts, err)
	}
	if names, _ := ttListAll(t, ctx, repo, ttTenant, ttCPT, "unassigned", 50); fmt.Sprint(names) != "[Person active]" {
		t.Fatalf("list = %v", names)
	}
	for i, status := range statuses {
		_, err := repo.SetMemberShift(ctx, ttTenant, ttActor, ttMember(i), "general", 0)
		if status == "active" && err != nil {
			t.Fatalf("active person refused: %v", err)
		}
		if status != "active" && !errors.Is(err, ports.ErrPersonNotFound) {
			t.Fatalf("%s person = %v, want not found", status, err)
		}
	}
}
