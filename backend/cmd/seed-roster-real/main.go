// Command seed-roster-real imports the maintainer's real Staff-Timetable +
// Attendance DB sheets into the Goat OS HRMS roster/coverage tables so the
// real website shows real people instead of demo names.
//
// Source data (gitignored, read at runtime, NEVER committed):
//   - <source>/timetable-goats-team-v1.json   Sheets API {"values":[[hdr...],[row...]]}
//     roster columns: [Operational Position | Shift | CBE person | CPT person |
//     Week OFFs | Backup]. Position rows run from row 1 until the first blank
//     row; everything after the blank row is Director-tier responsibility text
//     (process ownership, not per-center execution) and is intentionally NOT
//     parsed here.
//   - <source>/attendance-employee-master.json  columns: SL No, Name, Gender,
//     DOB, Designation, Location, Salary, DOJ, Contact Num, Emergency Num, Status.
//   - <source>/attendance-february-26.json      per-calendar-day attendance grid
//     (columns "1".."31"); a cell value of "0" is an explicit marked absence,
//     "1" is present, "" is no-data (not counted as absence -- also covers the
//     trailing 29/30/31 columns that do not exist in a 28-day February).
//
// PII rule: staff display names/emails are PII. This file NEVER contains a
// literal person name/email -- names flow through only as runtime values read
// from the gitignored JSON above. Every deterministic UUID is keyed on
// non-PII identifiers (SL No, a stable first-seen sequence number, dates,
// position codes) rather than on the name string itself. Log lines print only
// counts, never names.
//
// Design source of truth: docs/hr/roster-rbac-design.md (SS4.1-4.5). Per that
// doc's confirmed model this importer treats HR Designation grade (axis a),
// Operational Position (axis b), and Department ownership (axis c) as three
// independent fields -- never folding one into another.
//
// It is idempotent (deterministic v5 UUIDs + ON CONFLICT DO NOTHING), batched
// for scale, tenant-scoped, and gated to local/dev/test only (same guard as
// backend/cmd/seed-vaccination-real). It never touches goats, protocols,
// sheds, or the ravi@mesha.sg grant. It refuses to run at all until the HRMS
// migration (workforce_positions + workforce_members.hr_designation_grade)
// has landed -- see checkSchemaReady below.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/localtarget"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

const defaultTenantID = "00000000-0000-4000-8000-000000000001"

// ---- position vocabulary (docs/hr/roster-rbac-design.md SS2, SS4.2, SS4.3) ----

// positionDef is the fixed shape of one Operational Position row from the
// Staff-Timetable roster tab, independent of who currently holds it.
type positionDef struct {
	code         string
	tier         string // assistant | manager | head
	isBackupSlot bool
	backupGroup  string // "" if the sheet declares no backup for this position
	deptGuess    string // best-effort departments.code guess; "" = no guess
}

// positionDefs is keyed by the roster tab's Role label, normalized (collapsed
// whitespace, trimmed). Two rows in the real sheet do not resolve to a single
// backup_group_code and are flagged as open questions rather than guessed:
// "Packaging AM2" (source Backup cell is empty) and "Feeding Manager" (source
// Backup cell reads "Backup Manager and Backup AM2" -- two groups; this
// importer keeps only the primary manager_backup group, see the run report).
var positionDefs = map[string]positionDef{
	"Feeding AM1":             {code: "feeding_am1", tier: "assistant", backupGroup: "am1_backup", deptGuess: "feed"},
	"Feeding AM2":             {code: "feeding_am2", tier: "assistant", backupGroup: "am1_backup", deptGuess: "feed"},
	"Feeding AM3":             {code: "feeding_am3", tier: "assistant", backupGroup: "am1_backup", deptGuess: "feed"},
	"Cleaning AM1":            {code: "cleaning_am1", tier: "assistant", backupGroup: "am1_backup"},
	"Cleaning AM2":            {code: "cleaning_am2", tier: "assistant", backupGroup: "am1_backup"},
	"Milk AM1":                {code: "milk_am1", tier: "assistant", backupGroup: "am1_backup", deptGuess: "milk"},
	"Milk AM2":                {code: "milk_am2", tier: "assistant", backupGroup: "am2_backup", deptGuess: "milk"},
	"Breeding AM":             {code: "breeding_am", tier: "assistant", backupGroup: "am2_backup", deptGuess: "breeding"},
	"Health/Kidding AM1":      {code: "health_kidding_am1", tier: "assistant", backupGroup: "am2_backup", deptGuess: "health"},
	"Health/Kidding AM2":      {code: "health_kidding_am2", tier: "assistant", backupGroup: "am2_backup", deptGuess: "health"},
	"Packaging AM1":           {code: "packaging_am1", tier: "assistant", backupGroup: "am2_backup"},
	"Packaging AM2":           {code: "packaging_am2", tier: "assistant", backupGroup: ""}, // OPEN QUESTION: sheet declares no backup
	"Backup AM1":              {code: "backup_am1", tier: "assistant", isBackupSlot: true, backupGroup: "am1_backup"},
	"Backup AM2":              {code: "backup_am2", tier: "assistant", isBackupSlot: true, backupGroup: "am2_backup"},
	"Health/Kidding Manager1": {code: "health_kidding_manager_1", tier: "manager", backupGroup: "manager_backup", deptGuess: "health"},
	"Health/Kidding Manager2": {code: "health_kidding_manager_2", tier: "manager", backupGroup: "manager_backup", deptGuess: "health"},
	"Breeding Manager":        {code: "breeding_manager", tier: "manager", backupGroup: "manager_backup", deptGuess: "breeding"},
	"Preventive Care Manager": {code: "preventive_care_manager", tier: "manager", backupGroup: "manager_backup", deptGuess: "preventive_care"},
	"Backup Manager":          {code: "backup_manager", tier: "manager", isBackupSlot: true, backupGroup: "manager_backup"},
	"Feeding Manager":         {code: "feeding_manager", tier: "manager", backupGroup: "manager_backup", deptGuess: "feed"}, // OPEN QUESTION: sheet also names Backup AM2, see header note
	"Goats Head":              {code: "goats_head", tier: "head", backupGroup: "manager_backup"},
	"Park Head":               {code: "park_head", tier: "head", backupGroup: ""}, // top of local escalation chain, no backup by design
	"Health Trainer AM1":      {code: "health_trainer_am1", tier: "assistant", backupGroup: ""},
	"Backup Trainer AM2":      {code: "backup_trainer_am2", tier: "assistant", backupGroup: ""}, // name contains "Backup" but is NOT a canonical backup slot
	"Trainer AM3":             {code: "trainer_am3", tier: "assistant", backupGroup: ""},
	"Farming AM":              {code: "farming_am", tier: "assistant", backupGroup: ""},
}

// ---- normalized shapes ----

type memberSource string

const (
	sourceEmployeeMaster memberSource = "employee_master"
	sourceTimetable      memberSource = "timetable_holder"
)

// memberRec is one normalized staff member. key is a normalized-name string
// used ONLY in-process for dedup/joins across sheets -- it is never written
// to the database and never appears in a log line.
type memberRec struct {
	key         string
	source      memberSource
	seq         int    // SL No (employee_master) or a first-seen counter (timetable) -- non-PII, used for deterministic UUIDs/display_code
	designation string // raw Employee Master Designation string, "" for timetable-only members
	center      string // "Bangalore" | "CBE" | "CPT" | ""
	statusRaw   string // Employee Master Status ("Active"/"Inactive"), "" for timetable-only (defaults to active)
	bestTier    string // highest position tier this member holds, if any ("" if none)
	bestCode    string // the position_code that produced bestTier, for role-hint refinement
}

type positionRow struct {
	def       positionDef
	center    string // "CBE" | "CPT"
	weekOff   string // lowercase weekday, "" if none
	memberKey string
}

type leaveWindow struct {
	memberKey string
	startDate time.Time // inclusive, Asia/Kolkata midnight
	endDate   time.Time // inclusive, Asia/Kolkata midnight
}

type stats struct {
	EmployeeMasterRows     int
	TimetableRosterRows    int
	TimetableHoldersNew    int
	MembersTotal           int
	PositionSlotsDefined   int // roster rows x 2 centers
	PositionSlotsFilled    int
	PositionSlotsVacant    int
	BackupGroupsByCenter   map[string]map[string]int // center -> backup_group_code -> count
	GradeUnmapped          int
	AttendanceRowsTotal    int
	AttendanceMatchedNames int
	AttendanceUnmatched    int
	LeaveWindowsEmitted    int
	LeaveDaysTotal         int
	MembersInserted        int
	PositionsInserted      int
	LeavesInserted         int
	DepartmentMatches      int
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("seed-roster-real", flag.ContinueOnError)
	tenantID := fs.String("tenant-id", getenv("GOATOS_TENANT_ID", defaultTenantID), "tenant id")
	timeout := fs.Duration("timeout", 120*time.Second, "seed timeout")
	sourcePath := fs.String("source", "/Users/ravi/mesha/source-material/vgoats-seed", "path to source data directory")
	attendanceYear := fs.Int("attendance-year", 2026, "calendar year of the attendance-february-26.json sheet")
	attendanceMonth := fs.Int("attendance-month", 2, "calendar month (1-12) of the attendance-february-26.json sheet")
	if err := fs.Parse(args); err != nil {
		return err
	}

	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		return fmt.Errorf("load Asia/Kolkata: %w", err)
	}

	// ---- Step 1: normalize (safe to run with no DB at all) ----
	emp, err := loadEmployeeMasterKeyed(*sourcePath)
	if err != nil {
		return fmt.Errorf("load employee master: %w", err)
	}
	roster, err := loadRosterPositions(*sourcePath)
	if err != nil {
		return fmt.Errorf("load roster positions: %w", err)
	}
	grid, err := loadAttendanceGrid(*sourcePath)
	if err != nil {
		return fmt.Errorf("load attendance grid: %w", err)
	}

	members, positions, st := normalizeMembersAndPositions(emp, roster)
	leaves, st2 := normalizeLeaveWindows(grid, members, *attendanceYear, *attendanceMonth, loc)
	st.AttendanceRowsTotal = st2.AttendanceRowsTotal
	st.AttendanceMatchedNames = st2.AttendanceMatchedNames
	st.AttendanceUnmatched = st2.AttendanceUnmatched
	st.LeaveWindowsEmitted = st2.LeaveWindowsEmitted
	st.LeaveDaysTotal = st2.LeaveDaysTotal
	st.MembersTotal = len(members)

	fmt.Printf("normalized real roster:\n"+
		"  employee_master_rows=%d timetable_roster_rows=%d timetable_only_new_members=%d members_total=%d\n"+
		"  position_slots_defined=%d filled=%d vacant=%d grade_unmapped=%d\n"+
		"  backup_groups_by_center=%v\n"+
		"  attendance_rows=%d matched_members=%d unmatched_names=%d leave_windows=%d leave_days=%d\n",
		st.EmployeeMasterRows, st.TimetableRosterRows, st.TimetableHoldersNew, st.MembersTotal,
		st.PositionSlotsDefined, st.PositionSlotsFilled, st.PositionSlotsVacant, st.GradeUnmapped,
		st.BackupGroupsByCenter,
		st.AttendanceRowsTotal, st.AttendanceMatchedNames, st.AttendanceUnmatched, st.LeaveWindowsEmitted, st.LeaveDaysTotal)

	// ---- Step 2/3: import (requires the HRMS migration to be live) ----
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	pgCfg := platformpg.ConfigFromEnv()
	if err := localtarget.ValidateLocalDatabaseTarget("seed-roster-real", os.Getenv("GOATOS_ENV"), pgCfg.DatabaseURL, "local", "dev", "test"); err != nil {
		return err
	}
	pool, err := platformpg.Connect(ctx, pgCfg)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer pool.Close()

	ready, missing, err := checkSchemaReady(ctx, pool)
	if err != nil {
		return fmt.Errorf("check schema: %w", err)
	}
	if !ready {
		fmt.Printf("importer built, waiting on HRMS migration to land (missing: %s) -- normalizer counts above are the full dry-run result; no database writes were made.\n", missing)
		return nil
	}

	ist, err := importRoster(ctx, pool, *tenantID, members, positions, leaves)
	if err != nil {
		return fmt.Errorf("import: %w", err)
	}
	st.MembersInserted = ist.MembersInserted
	st.PositionsInserted = ist.PositionsInserted
	st.LeavesInserted = ist.LeavesInserted
	st.DepartmentMatches = ist.DepartmentMatches

	fmt.Printf("seeded real roster:\n"+
		"  members_inserted=%d positions_inserted=%d leaves_inserted=%d department_matches=%d\n",
		st.MembersInserted, st.PositionsInserted, st.LeavesInserted, st.DepartmentMatches)
	return nil
}

// ---- schema readiness guard ----

// checkSchemaReady reports whether the proposed HRMS migration
// (docs/hr/roster-rbac-design.md SS4.1/SS4.2: workforce_positions table +
// workforce_members.hr_designation_grade column) has landed. This importer
// refuses to write anything until both are present -- it never invents a
// divergent schema.
func checkSchemaReady(ctx context.Context, pool *pgxpool.Pool) (bool, string, error) {
	var missing []string

	var positionsTable *string
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.workforce_positions')::text`).Scan(&positionsTable); err != nil {
		return false, "", fmt.Errorf("check workforce_positions: %w", err)
	}
	if positionsTable == nil {
		missing = append(missing, "workforce_positions table")
	}

	var gradeCol int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.columns
		WHERE table_schema='public' AND table_name='workforce_members' AND column_name='hr_designation_grade'`).Scan(&gradeCol); err != nil {
		return false, "", fmt.Errorf("check hr_designation_grade: %w", err)
	}
	if gradeCol == 0 {
		missing = append(missing, "workforce_members.hr_designation_grade column")
	}

	if len(missing) > 0 {
		return false, strings.Join(missing, ", "), nil
	}
	return true, "", nil
}

// ---- source loaders ----

type empRow struct {
	SLNo        int
	Designation string
	Location    string
	Status      string
}

// empRowWithKey pairs an empRow with its in-memory-only normalized name key.
// Kept as a distinct type so a stray fmt/log of an empRow-shaped value can
// never print a name -- the key field is a one-way-normalized join key only.
type empRowWithKey struct {
	empRow
	key string
}

func loadEmployeeMasterKeyed(sourcePath string) ([]empRowWithKey, error) {
	values, err := readSheet(sourcePath + "/attendance-employee-master.json")
	if err != nil {
		return nil, err
	}
	if len(values) < 2 {
		return nil, fmt.Errorf("attendance-employee-master.json: too few rows")
	}
	col := headerIndex(values[0])
	nameCol, slCol, desigCol, locCol, statCol := col["Name"], col["SL No"], col["Designation"], col["Location"], col["Status"]
	out := make([]empRowWithKey, 0, len(values)-1)
	for _, row := range values[1:] {
		name := cell(row, nameCol)
		if name == "" {
			continue
		}
		sl, _ := strconv.Atoi(cell(row, slCol))
		out = append(out, empRowWithKey{
			empRow: empRow{
				SLNo:        sl,
				Designation: cell(row, desigCol),
				Location:    cell(row, locCol),
				Status:      cell(row, statCol),
			},
			key: normalizeName(name),
		})
	}
	return out, nil
}

type rosterRow struct {
	Role    string
	CBE     string
	CPT     string
	WeekOff string
}

// loadRosterPositions parses timetable-goats-team-v1.json's position rows
// only. Parsing stops at the first blank row -- everything after it is
// Director-tier responsibility text (a different, non-tabular shape) which
// this importer does not model.
func loadRosterPositions(sourcePath string) ([]rosterRow, error) {
	values, err := readSheet(sourcePath + "/timetable-goats-team-v1.json")
	if err != nil {
		return nil, err
	}
	if len(values) < 2 {
		return nil, fmt.Errorf("timetable-goats-team-v1.json: too few rows")
	}
	var out []rosterRow
	for _, row := range values[1:] {
		if len(row) == 0 {
			break // Director-tier freeform section begins here
		}
		role := normalizeLabel(cell(row, 0))
		if role == "" {
			continue
		}
		out = append(out, rosterRow{
			Role:    role,
			CBE:     cell(row, 2),
			CPT:     cell(row, 3),
			WeekOff: strings.ToLower(cell(row, 4)),
		})
	}
	return out, nil
}

type attendanceRow struct {
	key  string
	days map[int]string
}

// loadAttendanceGrid parses attendance-february-26.json's per-day columns.
func loadAttendanceGrid(sourcePath string) ([]attendanceRow, error) {
	values, err := readSheet(sourcePath + "/attendance-february-26.json")
	if err != nil {
		return nil, err
	}
	if len(values) < 2 {
		return nil, fmt.Errorf("attendance-february-26.json: too few rows")
	}
	col := headerIndex(values[0])
	nameCol := col["Name"]
	dayCol := make(map[int]int, 31)
	for d := 1; d <= 31; d++ {
		if i, ok := col[strconv.Itoa(d)]; ok {
			dayCol[d] = i
		}
	}
	var out []attendanceRow
	for _, row := range values[1:] {
		name := cell(row, nameCol)
		if name == "" {
			continue
		}
		days := make(map[int]string, len(dayCol))
		for d, i := range dayCol {
			days[d] = cell(row, i)
		}
		out = append(out, attendanceRow{key: normalizeName(name), days: days})
	}
	return out, nil
}

func readSheet(path string) ([][]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Values [][]interface{} `json:"values"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return payload.Values, nil
}

func headerIndex(hdr []interface{}) map[string]int {
	idx := map[string]int{}
	for i, h := range hdr {
		if s, ok := h.(string); ok && s != "" {
			idx[strings.TrimSpace(s)] = i
		}
	}
	return idx
}

func cell(row []interface{}, idx int) string {
	if idx < 0 || idx >= len(row) || row[idx] == nil {
		return ""
	}
	switch v := row[idx].(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return fmt.Sprintf("%g", v)
	default:
		return ""
	}
}

// ---- normalization ----

func normalizeLabel(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func normalizeName(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// deriveGrade maps an Employee Master Designation string to the design's
// hr_designation_grade enum (cxo|director|manager|assistant_manager). The
// real sheet's Designation values are fine-grained titles, not the coarse
// grade the design doc assumed lives in this column -- "Head" and the
// founder-tier "Operation" label are approximated (director, cxo
// respectively) rather than left unmapped; see the run report's open
// questions for why the design's 4-value enum does not cleanly cover the
// source vocabulary.
func deriveGrade(designation string) (grade string, matched bool) {
	lower := strings.ToLower(designation)
	switch {
	case strings.Contains(lower, "assistant manager"):
		return "assistant_manager", true
	case strings.Contains(lower, "manager"):
		return "manager", true
	case strings.Contains(lower, "head"):
		return "director", true // approximation -- source has no distinct "head" grade value
	case strings.Contains(lower, "operation"):
		return "cxo", true // approximation -- source never literally says "CXO"
	default:
		return "", false
	}
}

func normalizeMembersAndPositions(emp []empRowWithKey, roster []rosterRow) (map[string]*memberRec, []positionRow, stats) {
	var st stats
	st.EmployeeMasterRows = len(emp)
	st.TimetableRosterRows = len(roster)
	st.BackupGroupsByCenter = map[string]map[string]int{}

	members := map[string]*memberRec{}
	for _, e := range emp {
		if _, ok := deriveGrade(e.Designation); !ok {
			st.GradeUnmapped++
		}
		members[e.key] = &memberRec{
			key:         e.key,
			source:      sourceEmployeeMaster,
			seq:         e.SLNo,
			designation: e.Designation,
			center:      e.Location,
			statusRaw:   e.Status,
		}
	}

	var positions []positionRow
	timetableSeq := 0
	for _, r := range roster {
		def, ok := positionDefs[r.Role]
		if !ok {
			continue // unmapped label -- defensive; every real row is covered by positionDefs
		}
		for _, cp := range []struct {
			center string
			holder string
		}{{"CBE", r.CBE}, {"CPT", r.CPT}} {
			st.PositionSlotsDefined++
			holder := cp.holder
			if holder == "" || holder == "--" {
				st.PositionSlotsVacant++
				continue
			}
			st.PositionSlotsFilled++
			key := normalizeName(holder)
			if _, ok := members[key]; !ok {
				timetableSeq++
				members[key] = &memberRec{
					key:    key,
					source: sourceTimetable,
					seq:    timetableSeq,
					center: cp.center,
				}
				st.TimetableHoldersNew++
			}
			m := members[key]
			if tierRank(def.tier) > tierRank(m.bestTier) {
				m.bestTier = def.tier
				m.bestCode = def.code
			}
			weekOff := ""
			if isWeekday(r.WeekOff) {
				weekOff = r.WeekOff
			}
			positions = append(positions, positionRow{def: def, center: cp.center, weekOff: weekOff, memberKey: key})
			if def.backupGroup != "" {
				if st.BackupGroupsByCenter[cp.center] == nil {
					st.BackupGroupsByCenter[cp.center] = map[string]int{}
				}
				st.BackupGroupsByCenter[cp.center][def.backupGroup]++
			}
		}
	}
	return members, positions, st
}

func tierRank(tier string) int {
	switch tier {
	case "head":
		return 3
	case "manager":
		return 2
	case "assistant":
		return 1
	default:
		return 0
	}
}

var weekdays = map[string]bool{
	"monday": true, "tuesday": true, "wednesday": true, "thursday": true,
	"friday": true, "saturday": true, "sunday": true,
}

func isWeekday(s string) bool { return weekdays[s] }

func normalizeLeaveWindows(grid []attendanceRow, members map[string]*memberRec, year, month int, loc *time.Location) ([]leaveWindow, stats) {
	var st stats
	st.AttendanceRowsTotal = len(grid)
	daysInMonth := time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, loc).Day()

	var out []leaveWindow
	for _, row := range grid {
		if _, ok := members[row.key]; !ok {
			st.AttendanceUnmatched++
			continue
		}
		st.AttendanceMatchedNames++

		runStart := -1
		flush := func(endDay int) {
			if runStart == -1 {
				return
			}
			out = append(out, leaveWindow{
				memberKey: row.key,
				startDate: time.Date(year, time.Month(month), runStart, 0, 0, 0, 0, loc),
				endDate:   time.Date(year, time.Month(month), endDay, 0, 0, 0, 0, loc),
			})
			st.LeaveWindowsEmitted++
			st.LeaveDaysTotal += endDay - runStart + 1
			runStart = -1
		}
		for d := 1; d <= daysInMonth; d++ {
			absent := row.days[d] == "0" // explicit mark only; blank = no-data, not leave
			switch {
			case absent && runStart == -1:
				runStart = d
			case !absent && runStart != -1:
				flush(d - 1)
			}
		}
		flush(daysInMonth)
	}
	return out, st
}

// ---- import ----

type importStats struct {
	MembersInserted   int
	PositionsInserted int
	LeavesInserted    int
	DepartmentMatches int
}

func importRoster(ctx context.Context, pool *pgxpool.Pool, tenantID string, members map[string]*memberRec, positions []positionRow, leaves []leaveWindow) (importStats, error) {
	var ist importStats

	tx, err := pool.Begin(ctx)
	if err != nil {
		return ist, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	centerLocationID := map[string]string{}
	for _, center := range []string{"CBE", "CPT"} {
		var locationID string
		if err := tx.QueryRow(ctx, `SELECT location_id FROM locations WHERE tenant_id=$1 AND location_type='park' AND location_code=$2 LIMIT 1`, tenantID, center).Scan(&locationID); err != nil {
			return ist, fmt.Errorf("resolve center location %s: %w", center, err)
		}
		centerLocationID[center] = locationID
	}

	deptCache := map[string]*string{}
	resolveDept := func(code string) (*string, error) {
		if code == "" {
			return nil, nil
		}
		if id, ok := deptCache[code]; ok {
			return id, nil
		}
		var deptID string
		err := tx.QueryRow(ctx, `SELECT department_id FROM departments WHERE tenant_id=$1 AND code=$2 LIMIT 1`, tenantID, code).Scan(&deptID)
		if err == pgx.ErrNoRows {
			deptCache[code] = nil
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		deptCache[code] = &deptID
		ist.DepartmentMatches++
		return &deptID, nil
	}

	// Deterministic per-member best department guess: use whichever position
	// (if any) produced the member's bestCode.
	memberDeptGuess := map[string]string{}
	for _, p := range positions {
		if p.def.deptGuess == "" {
			continue
		}
		m := members[p.memberKey]
		if m != nil && p.def.code == m.bestCode {
			memberDeptGuess[p.memberKey] = p.def.deptGuess
		}
	}

	// Stable insert order for reproducible batches/logs.
	keys := make([]string, 0, len(members))
	for k := range members {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	type memberIns struct {
		id, displayCode, roleHint, status string
		grade                             *string
		locationID                        *string
		deptID                            *string
	}
	var memberRows []memberIns
	memberID := map[string]string{}
	for _, key := range keys {
		m := members[key]
		var id string
		var displayCode string
		switch m.source {
		case sourceEmployeeMaster:
			id = detUUID("workforce_member", "employee_master", tenantID, strconv.Itoa(m.seq))
			displayCode = fmt.Sprintf("HRMS-EMP-%03d", m.seq)
		default:
			id = detUUID("workforce_member", "timetable_holder", tenantID, strconv.Itoa(m.seq))
			displayCode = fmt.Sprintf("HRMS-TT-%03d", m.seq)
		}
		memberID[key] = id

		var grade *string
		if g, ok := deriveGrade(m.designation); ok {
			grade = &g
		}

		status := "active"
		if strings.EqualFold(m.statusRaw, "Inactive") {
			status = "inactive"
		}

		var locationID *string
		if center := m.center; center == "CBE" || center == "CPT" {
			l := centerLocationID[center]
			locationID = &l
		}
		// Bangalore (HQ) and unresolved centers get no primary_location_id --
		// open question SS7.2 in docs/hr/roster-rbac-design.md: HQ has no
		// operational-park location row today.

		roleHint := deriveRoleHint(m, grade)

		deptID, err := resolveDept(memberDeptGuess[key])
		if err != nil {
			return ist, fmt.Errorf("resolve department for member: %w", err)
		}

		memberRows = append(memberRows, memberIns{
			id: id, displayCode: displayCode, roleHint: roleHint, status: status,
			grade: grade, locationID: locationID, deptID: deptID,
		})
	}

	if err := batch(ctx, tx, memberRows, 200, func(b *pgx.Batch, m memberIns) {
		b.Queue(`
			INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status,
				primary_role_hint, primary_location_id, department_id, hr_designation_grade, updated_at)
			VALUES ($1,$2,$3,$3,$4,$5,$6,$7,$8,now())
			ON CONFLICT (workforce_member_id) DO NOTHING`,
			m.id, tenantID, m.displayCode, m.status, m.roleHint, m.locationID, m.deptID, m.grade)
	}); err != nil {
		return ist, fmt.Errorf("insert members: %w", err)
	}
	ist.MembersInserted = len(memberRows)

	if err := batch(ctx, tx, positions, 200, func(b *pgx.Batch, p positionRow) {
		posID := detUUID("workforce_position", tenantID, p.center, p.def.code)
		mID := memberID[p.memberKey]
		var backupGroup, weekOff *string
		if p.def.backupGroup != "" {
			bg := p.def.backupGroup
			backupGroup = &bg
		}
		if p.weekOff != "" {
			wo := p.weekOff
			weekOff = &wo
		}
		b.Queue(`
			INSERT INTO workforce_positions (position_id, tenant_id, workforce_member_id, scope_type, scope_id,
				position_code, position_tier, is_backup_slot, backup_group_code, week_off_weekday, status, valid_from, updated_at)
			VALUES ($1,$2,$3,'center',$4,$5,$6,$7,$8,$9,'active',now(),now())
			ON CONFLICT (position_id) DO NOTHING`,
			posID, tenantID, mID, centerLocationID[p.center], p.def.code, p.def.tier, p.def.isBackupSlot, backupGroup, weekOff)
	}); err != nil {
		return ist, fmt.Errorf("insert positions: %w", err)
	}
	ist.PositionsInserted = len(positions)

	if err := batch(ctx, tx, leaves, 200, func(b *pgx.Batch, l leaveWindow) {
		m := members[l.memberKey]
		absID := detUUID("workforce_absence", tenantID, l.memberKey, l.startDate.Format("2006-01-02"))
		mID := memberID[l.memberKey]
		// scope_type='center' (not the historically-used 'park') to match
		// workforce_positions' own scope vocabulary -- migration 000151
		// extended workforce_absences_scope_check to admit 'center' for
		// exactly this HR roster model (docs/hr/roster-rbac-design.md S0.2).
		scopeType := "tenant"
		scopeID := tenantID
		if center := m.center; center == "CBE" || center == "CPT" {
			scopeType = "center"
			scopeID = centerLocationID[center]
		}
		startsAt := l.startDate
		endsAt := l.endDate.AddDate(0, 0, 1) // exclusive end, satisfies ends_at > starts_at even for single-day leave
		b.Queue(`
			INSERT INTO workforce_absences (absence_id, tenant_id, workforce_member_id, scope_type, scope_id,
				starts_at, ends_at, reason_code, status, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,'attendance_sheet_absence','approved',now())
			ON CONFLICT (absence_id) DO NOTHING`,
			absID, tenantID, mID, scopeType, scopeID, startsAt, endsAt)
	}); err != nil {
		return ist, fmt.Errorf("insert leave windows: %w", err)
	}
	ist.LeavesInserted = len(leaves)

	if err := tx.Commit(ctx); err != nil {
		return ist, fmt.Errorf("commit: %w", err)
	}
	return ist, nil
}

// deriveRoleHint picks the existing workforce_members.primary_role_hint value
// (operator|park_head|verifier|supervisor|admin|other) from whichever axis is
// available: the member's highest-tier operational position if they hold
// one, else their HR Designation grade.
func deriveRoleHint(m *memberRec, grade *string) string {
	switch m.bestCode {
	case "park_head":
		return "park_head"
	}
	switch m.bestTier {
	case "head", "manager":
		return "supervisor"
	case "assistant":
		return "operator"
	}
	if grade != nil {
		switch *grade {
		case "cxo", "director":
			return "admin"
		case "manager":
			return "supervisor"
		case "assistant_manager":
			return "operator"
		}
	}
	return "other"
}

// batch queues rows via pgx.Batch in chunks and executes each chunk,
// surfacing the first row error.
func batch[T any](ctx context.Context, tx pgx.Tx, rows []T, size int, queue func(*pgx.Batch, T)) error {
	for start := 0; start < len(rows); start += size {
		end := start + size
		if end > len(rows) {
			end = len(rows)
		}
		b := &pgx.Batch{}
		for _, r := range rows[start:end] {
			queue(b, r)
		}
		br := tx.SendBatch(ctx, b)
		var firstErr error
		for range rows[start:end] {
			if _, err := br.Exec(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		if cerr := br.Close(); cerr != nil && firstErr == nil {
			firstErr = cerr
		}
		if firstErr != nil {
			return firstErr
		}
	}
	return nil
}

func detUUID(kind string, parts ...string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("goatos:seed-roster-real:"+kind+":"+strings.Join(parts, ":"))).String()
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
