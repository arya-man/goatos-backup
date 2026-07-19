// Command seed-roster-real imports the maintainer's reviewed roster mapping +
// Jun-26 attendance sheet into the Goat OS HRMS roster/coverage tables so the
// real website shows real people instead of demo names.
//
// Source data (gitignored, read at runtime, NEVER committed):
//   - <source>/attendance-jun-26.json   Sheets API Jun-26 tab (live source)
//     columns: Name, Basic Salary, Incentive, Type, Designation Type, Designation,
//     Location, DOJ, <day columns 1..30>. Designation Type values (CXO / Director /
//     Manager / Assistant Manager) are read directly and map to hr_designation_grade
//     (cxo | director | manager | assistant_manager), lowercased+underscored.
//   - <source>/roster-name-mapping.jun26-review.csv   maintainer-reviewed authoritative
//     roster-to-person join. Columns: center, timetable_position,
//     timetable_name, jun26_candidate, designation_type, designation, location,
//     confidence, notes. This CSV is the SOURCE OF TRUTH for timetable-slot assignments --
//     replaces fuzzy matching. The jun26_candidate column names the person in the Jun-26
//     sheet who holds each roster position. UNRESOLVED confidence slot leaves
//     grade/member-link null and logs it. MANUAL_SEED confidence creates a
//     seed-only member from the CSV row when the person is intentionally not
//     present in Jun-26 yet.
//   - <source>/timetable-goats-team-v1.json   Staff-Timetable Goats-Team-v1 tab,
//     used for recurring week-off days by fixed operational position.
//   - <source>/attendance-jun-26.json (optional)   per-calendar-day attendance grid
//     for derived leave windows; day columns "1".."30" (June has 30 days). Cell value
//     "0" = explicit marked absence, "1" = present, "" = no-data (not counted as absence).
//
// PII rule: staff display names/emails are PII. This file NEVER contains a
// literal person name/email -- names flow through only as runtime values read
// from the gitignored sources above. Every deterministic UUID is keyed on
// non-PII identifiers (sequence number, dates, position codes) rather than on
// the name string itself. Log lines print only counts, never names.
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
	"encoding/csv"
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

// positionDef is the fixed shape of one Operational Position row.
type positionDef struct {
	code         string
	tier         string // assistant | manager | head
	isBackupSlot bool
	backupGroup  string // "" if no backup for this position
	deptGuess    string // best-effort departments.code guess; "" = no guess
}

// positionDefs is keyed by the roster position name from the CSV.
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
	"Packaging AM2":           {code: "packaging_am2", tier: "assistant", backupGroup: ""},
	"Backup AM1":              {code: "backup_am1", tier: "assistant", isBackupSlot: true, backupGroup: "am1_backup"},
	"Backup AM2":              {code: "backup_am2", tier: "assistant", isBackupSlot: true, backupGroup: "am2_backup"},
	"Health/Kidding Manager1": {code: "health_kidding_manager_1", tier: "manager", backupGroup: "manager_backup", deptGuess: "health"},
	"Health/Kidding Manager2": {code: "health_kidding_manager_2", tier: "manager", backupGroup: "manager_backup", deptGuess: "health"},
	"Breeding Manager":        {code: "breeding_manager", tier: "manager", backupGroup: "manager_backup", deptGuess: "breeding"},
	"Preventive Care Manager": {code: "preventive_care_manager", tier: "manager", backupGroup: "manager_backup", deptGuess: "preventive_care"},
	"Backup Manager":          {code: "backup_manager", tier: "manager", isBackupSlot: true, backupGroup: "manager_backup"},
	"Feeding Manager":         {code: "feeding_manager", tier: "manager", backupGroup: "manager_backup", deptGuess: "feed"},
	"Goats Head":              {code: "goats_head", tier: "head", backupGroup: "manager_backup"},
	"Park Head":               {code: "park_head", tier: "head", backupGroup: ""},
	"Health Trainer AM1":      {code: "health_trainer_am1", tier: "assistant", backupGroup: ""},
	"Backup Trainer AM2":      {code: "backup_trainer_am2", tier: "assistant", backupGroup: ""},
	"Trainer AM3":             {code: "trainer_am3", tier: "assistant", backupGroup: ""},
	"Farming AM":              {code: "farming_am", tier: "assistant", backupGroup: ""},
}

// ---- normalized shapes ----

type memberSource string

const (
	sourceJun26      memberSource = "jun26"
	sourceManualSeed memberSource = "manual_seed"
)

// memberRec is one normalized staff member keyed by June-26 sheet name.
type memberRec struct {
	seqNo           int    // position in Jun-26 sheet (1-based), used for deterministic UUID
	name            string // Jun-26 sheet name; NEVER logged
	source          memberSource
	sourceKey       string // deterministic non-PII import key
	designationType string // Designation Type from Jun-26: CXO | Director | Manager | Assistant Manager
	designation     string // Designation from Jun-26
	location        string // Location from Jun-26: Bangalore | CBE | CPT
	grade           string // hr_designation_grade (cxo | director | manager | assistant_manager), lowercased+underscored from designationType
	bestTier        string // highest position tier ("" if none)
	bestCode        string // position_code that produced bestTier
}

type rosterAssignment struct {
	center         string // CBE | CPT
	position       positionDef
	jun26Name      string // person's Jun-26 name; "" if UNRESOLVED
	isResolved     bool   // false for UNRESOLVED slots
	unresolved     bool   // true if confidence was UNRESOLVED
	designation    string // Jun-26 Designation (for diagnostics/notes only)
	weekOffWeekday string
}

type leaveWindow struct {
	seqNo     int       // memberRec.seqNo
	startDate time.Time // inclusive, Asia/Kolkata midnight
	endDate   time.Time // inclusive, Asia/Kolkata midnight
}

type stats struct {
	Jun26Rows              int
	MappingRows            int
	AssignmentsFilled      int
	AssignmentsUnresolved  int
	PositionSlotsDefined   int
	PositionSlotsFilled    int
	PositionSlotsVacant    int
	BackupGroupsByCenter   map[string]map[string]int
	MembersWithGradeMapped int
	AttendanceRowsTotal    int
	AttendanceMatchedNames int
	AttendanceUnmatched    int
	LeaveWindowsEmitted    int
	LeaveDaysTotal         int
	ManualMembersCreated   int
	MembersInserted        int
	PositionsInserted      int
	LeavesInserted         int
	DepartmentMatches      int
	ModuleGrantsInserted   int
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
	attendanceYear := fs.Int("attendance-year", 2026, "calendar year of attendance sheet")
	attendanceMonth := fs.Int("attendance-month", 6, "calendar month (1-12) of attendance sheet")
	if err := fs.Parse(args); err != nil {
		return err
	}

	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		return fmt.Errorf("load Asia/Kolkata: %w", err)
	}

	// ---- Step 1: normalize (safe to run with no DB at all) ----
	jun26Members, err := loadJun26Keyed(*sourcePath)
	if err != nil {
		return fmt.Errorf("load jun-26 sheet: %w", err)
	}

	rosterMappings, err := loadRosterMappingCSV(*sourcePath)
	if err != nil {
		return fmt.Errorf("load roster mapping CSV: %w", err)
	}

	weekOffs, err := loadRosterWeekOffs(*sourcePath)
	if err != nil {
		return fmt.Errorf("load roster week-offs: %w", err)
	}

	grid, err := loadAttendanceGrid(*sourcePath)
	if err != nil {
		return fmt.Errorf("load attendance grid: %w", err)
	}

	members, assignments, st := normalizeAssignments(jun26Members, rosterMappings, weekOffs)
	leaves, st2 := normalizeLeaveWindows(grid, members, *attendanceYear, *attendanceMonth, loc)
	st.AttendanceRowsTotal = st2.AttendanceRowsTotal
	st.AttendanceMatchedNames = st2.AttendanceMatchedNames
	st.AttendanceUnmatched = st2.AttendanceUnmatched
	st.LeaveWindowsEmitted = st2.LeaveWindowsEmitted
	st.LeaveDaysTotal = st2.LeaveDaysTotal

	fmt.Printf("normalized real roster:\n"+
		"  jun26_rows=%d mapping_rows=%d assignments_filled=%d unresolved=%d\n"+
		"  members_with_grade=%d manual_seed_members=%d backup_groups_by_center=%v\n"+
		"  attendance_rows=%d matched_members=%d unmatched_names=%d leave_windows=%d leave_days=%d\n",
		st.Jun26Rows, st.MappingRows, st.AssignmentsFilled, st.AssignmentsUnresolved,
		st.MembersWithGradeMapped, st.ManualMembersCreated, st.BackupGroupsByCenter,
		st.AttendanceRowsTotal, st.AttendanceMatchedNames, st.AttendanceUnmatched, st.LeaveWindowsEmitted, st.LeaveDaysTotal)

	// Log unresolved slots if any
	if st.AssignmentsUnresolved > 0 {
		fmt.Printf("WARNING: %d unresolved assignment(s) -- slots seeded with position but null member/grade\n", st.AssignmentsUnresolved)
	}

	// ---- Step 2/3: import (requires the HRMS migration to be live) ----
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	pgCfg := platformpg.ConfigFromEnv()
	if err := validateTarget(os.Getenv("GOATOS_ENV"), pgCfg.DatabaseURL); err != nil {
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

	ist, err := importRoster(ctx, pool, *tenantID, members, assignments, leaves)
	if err != nil {
		return fmt.Errorf("import: %w", err)
	}
	st.MembersInserted = ist.MembersInserted
	st.PositionsInserted = ist.PositionsInserted
	st.LeavesInserted = ist.LeavesInserted
	st.DepartmentMatches = ist.DepartmentMatches
	st.ModuleGrantsInserted = ist.ModuleGrantsInserted

	fmt.Printf("seeded real roster:\n"+
		"  members_inserted=%d positions_inserted=%d leaves_inserted=%d department_matches=%d module_grants_inserted=%d\n",
		st.MembersInserted, st.PositionsInserted, st.LeavesInserted, st.DepartmentMatches, st.ModuleGrantsInserted)
	return nil
}

// ---- schema readiness guard ----

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

	// mig 000002. The roster import now writes department_module_grants in the same
	// transaction, so a database behind that migration must be reported as not-ready here
	// rather than failing mid-import (or, worse, committing members with no module grants
	// and yielding an empty bottom bar).
	var moduleGrantsTable *string
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.department_module_grants')::text`).Scan(&moduleGrantsTable); err != nil {
		return false, "", fmt.Errorf("check department_module_grants: %w", err)
	}
	if moduleGrantsTable == nil {
		missing = append(missing, "department_module_grants table")
	}

	if len(missing) > 0 {
		return false, strings.Join(missing, ", "), nil
	}
	return true, "", nil
}

// ---- source loaders ----

// loadJun26Keyed loads the Jun-26 attendance sheet and keys by name.
func loadJun26Keyed(sourcePath string) (map[string]*memberRec, error) {
	values, err := readSheet(sourcePath + "/attendance-jun-26.json")
	if err != nil {
		return nil, err
	}
	if len(values) < 2 {
		return nil, fmt.Errorf("attendance-jun-26.json: too few rows")
	}

	col := headerIndex(values[0])
	nameCol := col["Name"]
	desigTypeCol := col["Designation Type"]
	desigCol := col["Designation"]
	locCol := col["Location"]

	out := make(map[string]*memberRec)
	for seqNo, row := range values[1:] {
		name := cell(row, nameCol)
		if name == "" {
			continue
		}
		desigType := cell(row, desigTypeCol)
		desig := cell(row, desigCol)
		location := cell(row, locCol)

		grade := mapDesignationTypeToGrade(desigType)

		m := &memberRec{
			seqNo:           seqNo + 2, // 1-based, row 1 is header
			name:            name,
			source:          sourceJun26,
			sourceKey:       strconv.Itoa(seqNo + 2),
			designationType: desigType,
			designation:     desig,
			location:        location,
			grade:           grade,
		}
		out[name] = m
	}
	return out, nil
}

// mapDesignationTypeToGrade maps "CXO" / "Director" / "Manager" / "Assistant Manager" to
// cxo | director | manager | assistant_manager.
func mapDesignationTypeToGrade(desigType string) string {
	lower := strings.ToLower(desigType)
	switch {
	case strings.Contains(lower, "cxo"):
		return "cxo"
	case strings.Contains(lower, "director"):
		return "director"
	case strings.Contains(lower, "manager") && strings.Contains(lower, "assistant"):
		return "assistant_manager"
	case strings.Contains(lower, "manager"):
		return "manager"
	default:
		return ""
	}
}

// rosterMappingRow represents one row from the reviewed CSV.
type rosterMappingRow struct {
	rowNo           int
	center          string
	position        string // timetable_position
	jun26Name       string // jun26_candidate
	confidence      string // HIGH | REVIEW | UNRESOLVED | MANUAL_SEED
	designationType string
	designation     string // for notes/member seeding
	location        string
}

// loadRosterMappingCSV loads the authoritative roster-to-person join.
func loadRosterMappingCSV(sourcePath string) ([]rosterMappingRow, error) {
	f, err := os.Open(sourcePath + "/roster-name-mapping.jun26-review.csv")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read CSV: %w", err)
	}

	if len(rows) < 2 {
		return nil, fmt.Errorf("roster mapping CSV: too few rows")
	}

	// Header: center, timetable_position, timetable_name, jun26_candidate, designation_type, designation, location, confidence, notes
	var out []rosterMappingRow
	for i, row := range rows[1:] {
		if len(row) < 8 {
			continue
		}
		center := strings.TrimSpace(row[0])
		position := strings.TrimSpace(row[1])
		jun26Name := strings.TrimSpace(row[3])
		desigType := strings.TrimSpace(row[4])
		desig := strings.TrimSpace(row[5])
		location := strings.TrimSpace(row[6])
		confidence := strings.TrimSpace(row[7])

		if center == "" || position == "" {
			continue
		}

		out = append(out, rosterMappingRow{
			rowNo:           i + 2,
			center:          center,
			position:        position,
			jun26Name:       jun26Name,
			confidence:      confidence,
			designationType: desigType,
			designation:     desig,
			location:        location,
		})
	}
	return out, nil
}

type attendanceRow struct {
	name string
	days map[int]string
}

func loadRosterWeekOffs(sourcePath string) (map[string]string, error) {
	values, err := readSheet(sourcePath + "/timetable-goats-team-v1.json")
	if err != nil {
		return map[string]string{}, nil
	}
	if len(values) < 2 {
		return map[string]string{}, nil
	}

	out := make(map[string]string)
	for _, row := range values[1:] {
		position := normalizeLabel(cell(row, 0))
		if position == "" {
			continue
		}
		if weekday := normalizeWeekday(cell(row, 4)); weekday != "" {
			out[position] = weekday
		}
	}
	return out, nil
}

// loadAttendanceGrid loads attendance grid keyed by June name.
// If no supported attendance grid exists, returns empty grid (optional).
func loadAttendanceGrid(sourcePath string) ([]attendanceRow, error) {
	var values [][]interface{}
	var err error
	for _, name := range []string{"/attendance-jun-26.json", "/attendance-june.json"} {
		values, err = readSheet(sourcePath + name)
		if err == nil {
			break
		}
	}
	if err != nil {
		return []attendanceRow{}, nil
	}

	if len(values) < 2 {
		return []attendanceRow{}, nil
	}

	col := headerIndex(values[0])
	nameCol := col["Name"]
	dayCol := make(map[int]int, 30)
	for d := 1; d <= 30; d++ {
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
		out = append(out, attendanceRow{name: name, days: days})
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

// normalizeLabel collapses whitespace in position names for matching.
func normalizeLabel(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func normalizeWeekday(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday":
		return strings.ToLower(strings.TrimSpace(s))
	default:
		return ""
	}
}

// normalizeAssignments builds the roster assignments from the maintainer's reviewed CSV
// and Jun-26 member data, handling UNRESOLVED slots.
func normalizeAssignments(jun26Members map[string]*memberRec, csvMappings []rosterMappingRow, weekOffs map[string]string) (map[string]*memberRec, []rosterAssignment, stats) {
	var st stats
	st.Jun26Rows = len(jun26Members)
	st.MappingRows = len(csvMappings)
	st.BackupGroupsByCenter = map[string]map[string]int{}

	// Build result members (filtered to those assigned)
	members := make(map[string]*memberRec)
	var assignments []rosterAssignment

	for _, mapping := range csvMappings {
		// Normalize position name to handle spacing variations
		posName := normalizeLabel(mapping.position)
		def, ok := positionDefs[posName]
		if !ok {
			continue // unmapped position (shouldn't happen with reviewed CSV)
		}
		weekOffWeekday := weekOffs[posName]

		st.PositionSlotsDefined++

		// Resolve the member
		var member *memberRec
		var resolved bool

		if mapping.confidence == "UNRESOLVED" || mapping.jun26Name == "" {
			st.AssignmentsUnresolved++
			resolved = false
		} else {
			if m, ok := jun26Members[mapping.jun26Name]; ok {
				member = m
				resolved = true
			} else if isManualSeed(mapping.confidence) {
				location := mapping.location
				if location == "" {
					location = mapping.center
				}
				member = &memberRec{
					seqNo:           9000 + mapping.rowNo,
					name:            mapping.jun26Name,
					source:          sourceManualSeed,
					sourceKey:       fmt.Sprintf("%s:%s", mapping.center, def.code),
					designationType: mapping.designationType,
					designation:     mapping.designation,
					location:        location,
					grade:           mapDesignationTypeToGrade(mapping.designationType),
				}
				resolved = true
			}
			if resolved {
				st.AssignmentsFilled++
				if existing, exists := members[mapping.jun26Name]; exists {
					member = existing
				} else {
					members[mapping.jun26Name] = member
					if member.grade != "" {
						st.MembersWithGradeMapped++
					}
					if member.source == sourceManualSeed {
						st.ManualMembersCreated++
					}
				}
			}
			if !resolved {
				// CSV references a name not in Jun-26 (shouldn't happen with maintained CSV)
				st.AssignmentsUnresolved++
			}
		}

		if !resolved {
			// Unresolved: create position without member link
			assignments = append(assignments, rosterAssignment{
				center:         mapping.center,
				position:       def,
				jun26Name:      "",
				isResolved:     false,
				unresolved:     true,
				designation:    mapping.designation,
				weekOffWeekday: weekOffWeekday,
			})
		} else {
			// Resolved: normal assignment
			assignments = append(assignments, rosterAssignment{
				center:         mapping.center,
				position:       def,
				jun26Name:      mapping.jun26Name,
				isResolved:     true,
				unresolved:     false,
				designation:    mapping.designation,
				weekOffWeekday: weekOffWeekday,
			})

			// Update member's best tier/code
			if tierRank(def.tier) > tierRank(member.bestTier) {
				member.bestTier = def.tier
				member.bestCode = def.code
			}

			// Track backup group
			if def.backupGroup != "" {
				if st.BackupGroupsByCenter[mapping.center] == nil {
					st.BackupGroupsByCenter[mapping.center] = map[string]int{}
				}
				st.BackupGroupsByCenter[mapping.center][def.backupGroup]++
			}
		}
	}

	st.PositionSlotsFilled = len(assignments) - st.AssignmentsUnresolved
	st.PositionSlotsVacant = 0 // CSV has no vacant slots

	return members, assignments, st
}

func isManualSeed(confidence string) bool {
	return strings.EqualFold(strings.TrimSpace(confidence), "MANUAL_SEED")
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

// normalizeLeaveWindows derives leave windows from attendance grid keyed by Jun-26 name.
func normalizeLeaveWindows(grid []attendanceRow, members map[string]*memberRec, year, month int, loc *time.Location) ([]leaveWindow, stats) {
	var st stats
	st.AttendanceRowsTotal = len(grid)
	daysInMonth := time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, loc).Day()

	var out []leaveWindow
	for _, row := range grid {
		m, ok := members[row.name]
		if !ok {
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
				seqNo:     m.seqNo,
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
	MembersInserted      int
	PositionsInserted    int
	LeavesInserted       int
	DepartmentMatches    int
	ModuleGrantsInserted int
}

// ---- department -> module grants (seed coupling for mig 000002) ----

// defaultDepartmentModules is the default department -> module_key grant set applied by
// this importer. It exists because of the seed-coupling rule in
// docs/runbooks/initial-seed-migration-coupling.md: /app/bootstrap composes navigation from
// department_module_grants (see docs/decisions/role-module-nav-composition.md), so a roster
// import that attaches members to departments but grants no modules produces a technically
// correct HRMS and a completely empty bottom bar on the phone.
//
// These are SEED defaults for local/dev, not business authority. Departments are canonical
// (baseline migration data); who may run which module is an operational decision that
// belongs in the admin Config surface once module administration exists. Keep this map
// minimal and justified rather than granting everything to everyone:
//
//   - preventive_care -> vaccination: the PC seats run the vaccination drives. This is the
//     grant that keeps the existing vaccination operator working after mig 000002 removed
//     the hardcoded grantedModules = ["vaccination"].
//   - preventive_care, health -> counts: Counts carries birth/death capture, and the
//     maintainer-approved rule is that field operators record birth and death from the app
//     (docs/runbooks/android-dev-device.md). Health/Kidding seats are the ones present at a
//     kidding or a death, and PC seats are in the sheds daily.
//   - feed -> feed_direction and breeding -> breeding are registry-declared "soon" modules.
//     Granting them now is harmless (a soon module contributes no nav and does not count
//     toward the drawer threshold) and means those departments light up automatically when
//     the module ships instead of needing a seed change then.
//
// Departments with no operational module today (procurement, growth, infrastructure, milk,
// sales) are deliberately absent. A department key that does not exist in this tenant is
// skipped by the INSERT ... SELECT below, never an error.
var defaultDepartmentModules = map[string][]string{
	"preventive_care": {"vaccination", "counts"},
	"health":          {"counts"},
	"feed":            {"feed_direction"},
	"breeding":        {"breeding"},
}

func importRoster(ctx context.Context, pool *pgxpool.Pool, tenantID string, members map[string]*memberRec, assignments []rosterAssignment, leaves []leaveWindow) (importStats, error) {
	var ist importStats

	tx, err := pool.Begin(ctx)
	if err != nil {
		return ist, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Resolve center locations
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

	// Insert members
	type memberIns struct {
		id, displayCode, name, roleHint, status string
		grade                                   *string
		locationID                              *string
		deptID                                  *string
	}
	var memberRows []memberIns
	memberID := map[string]string{}

	// Sort for stable insert order
	keys := make([]string, 0, len(members))
	for k := range members {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		m := members[key]
		source := m.source
		if source == "" {
			source = sourceJun26
		}
		sourceKey := m.sourceKey
		if sourceKey == "" {
			sourceKey = strconv.Itoa(m.seqNo)
		}
		id := detUUID("workforce_member", string(source), tenantID, sourceKey)
		displayCode := fmt.Sprintf("HRMS-JUN26-%03d", m.seqNo)
		if source == sourceManualSeed {
			displayCode = fmt.Sprintf("HRMS-MANUAL-%03d", m.seqNo)
		}
		memberID[key] = id

		var grade *string
		if m.grade != "" {
			grade = &m.grade
		}

		status := "active"

		var locationID *string
		if center := m.location; center == "CBE" || center == "CPT" {
			l := centerLocationID[center]
			locationID = &l
		}

		roleHint := deriveRoleHint(m, grade)

		// Deterministic department guess: use position's deptGuess for best tier
		var deptGuess string
		for _, a := range assignments {
			if a.jun26Name == key && a.position.code == m.bestCode && a.position.deptGuess != "" {
				deptGuess = a.position.deptGuess
				break
			}
		}

		deptID, err := resolveDept(deptGuess)
		if err != nil {
			return ist, fmt.Errorf("resolve department: %w", err)
		}

		// display_name = the real person name (the map key IS the Jun-26 name),
		// read at runtime from gitignored source — never hardcoded/committed.
		memberRows = append(memberRows, memberIns{
			id: id, displayCode: displayCode, name: key, roleHint: roleHint, status: status,
			grade: grade, locationID: locationID, deptID: deptID,
		})
	}

	if err := batch(ctx, tx, memberRows, 200, func(b *pgx.Batch, m memberIns) {
		b.Queue(`
			INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status,
				primary_role_hint, primary_location_id, department_id, hr_designation_grade, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,now())
			ON CONFLICT (workforce_member_id) DO UPDATE SET
				display_code = EXCLUDED.display_code,
				display_name = EXCLUDED.display_name,
				status = EXCLUDED.status,
				primary_role_hint = EXCLUDED.primary_role_hint,
				primary_location_id = EXCLUDED.primary_location_id,
				department_id = EXCLUDED.department_id,
				hr_designation_grade = EXCLUDED.hr_designation_grade,
				updated_at = now()`,
			m.id, tenantID, m.displayCode, m.name, m.status, m.roleHint, m.locationID, m.deptID, m.grade)
	}); err != nil {
		return ist, fmt.Errorf("insert members: %w", err)
	}
	ist.MembersInserted = len(memberRows)

	// Insert positions (skip unresolved; they remain vacant)
	var positionRows []struct {
		posID          string
		mID            string
		center         string
		def            positionDef
		weekOffWeekday string
	}

	for _, a := range assignments {
		if !a.isResolved {
			// Skip unresolved positions; they remain vacant in the database
			continue
		}

		posID := detUUID("workforce_position", tenantID, a.center, a.position.code)
		mID := memberID[a.jun26Name]

		positionRows = append(positionRows, struct {
			posID          string
			mID            string
			center         string
			def            positionDef
			weekOffWeekday string
		}{posID: posID, mID: mID, center: a.center, def: a.position, weekOffWeekday: a.weekOffWeekday})
	}

	if err := batch(ctx, tx, positionRows, 200, func(b *pgx.Batch, p struct {
		posID          string
		mID            string
		center         string
		def            positionDef
		weekOffWeekday string
	}) {
		var backupGroup *string
		if p.def.backupGroup != "" {
			bg := p.def.backupGroup
			backupGroup = &bg
		}
		var weekOff *string
		if p.weekOffWeekday != "" {
			w := p.weekOffWeekday
			weekOff = &w
		}

		b.Queue(`
			INSERT INTO workforce_positions (position_id, tenant_id, workforce_member_id, scope_type, scope_id,
				position_code, position_tier, is_backup_slot, backup_group_code, week_off_weekday, status, valid_from, updated_at)
			VALUES ($1,$2,$3,'center',$4,$5,$6,$7,$8,$9,'active',now(),now())
			ON CONFLICT (position_id) DO UPDATE SET
				workforce_member_id = EXCLUDED.workforce_member_id,
				scope_type = EXCLUDED.scope_type,
				scope_id = EXCLUDED.scope_id,
				position_code = EXCLUDED.position_code,
				position_tier = EXCLUDED.position_tier,
				is_backup_slot = EXCLUDED.is_backup_slot,
				backup_group_code = EXCLUDED.backup_group_code,
				week_off_weekday = EXCLUDED.week_off_weekday,
				status = EXCLUDED.status,
				valid_to = NULL,
				updated_at = now()`,
			p.posID, tenantID, p.mID, centerLocationID[p.center], p.def.code, p.def.tier, p.def.isBackupSlot, backupGroup, weekOff)
	}); err != nil {
		return ist, fmt.Errorf("insert positions: %w", err)
	}
	ist.PositionsInserted = len(positionRows)

	// Build seqNo -> (name, center) map for leave lookup
	seqNoToMember := make(map[int]struct{ name, center string })
	for k, m := range members {
		seqNoToMember[m.seqNo] = struct{ name, center string }{k, m.location}
	}

	// Insert leaves with coverage resolution (P2)
	if err := batch(ctx, tx, leaves, 200, func(b *pgx.Batch, l leaveWindow) {
		memberInfo, ok := seqNoToMember[l.seqNo]
		if !ok {
			return // Member not found (shouldn't happen)
		}

		absID := detUUID("workforce_absence", tenantID, strconv.Itoa(l.seqNo), l.startDate.Format("2006-01-02"))
		mID := memberID[memberInfo.name]

		scopeType := "tenant"
		scopeID := tenantID
		if memberInfo.center == "CBE" || memberInfo.center == "CPT" {
			scopeType = "center"
			scopeID = centerLocationID[memberInfo.center]
		}

		startsAt := l.startDate
		endsAt := l.endDate.AddDate(0, 0, 1)

		// Resolve effective backup for the position holder (P2)
		var replacementID *string
		positionRow := tx.QueryRow(ctx, `
			SELECT p.position_id FROM workforce_positions p
			WHERE tenant_id=$1 AND workforce_member_id=$2 AND scope_type=$3 AND scope_id=$4 AND status='active'
			LIMIT 1`, tenantID, mID, scopeType, scopeID)
		var positionID string
		if err := positionRow.Scan(&positionID); err == nil {
			// Position found; resolve backup for this position's backup_group
			backupRow := tx.QueryRow(ctx, `
				SELECT p.backup_group_code FROM workforce_positions p
				WHERE position_id=$1 LIMIT 1`, positionID)
			var backupGroup *string
			_ = backupRow.Scan(&backupGroup)
			if backupGroup != nil && *backupGroup != "" {
				// Find the backup holder in this group
				backupHolderRow := tx.QueryRow(ctx, `
					SELECT p.workforce_member_id FROM workforce_positions p
					WHERE tenant_id=$1 AND backup_group_code=$2 AND scope_type=$3 AND scope_id=$4
					AND is_backup_slot=true AND status='active' AND p.workforce_member_id != $5
					LIMIT 1`, tenantID, *backupGroup, scopeType, scopeID, mID)
				var backupMemberID string
				if err := backupHolderRow.Scan(&backupMemberID); err == nil {
					replacementID = &backupMemberID
				}
			}
		}
		// Query with ON CONFLICT will handle idempotency; batch sets replacement_member_id on first insert
		b.Queue(`
			INSERT INTO workforce_absences (absence_id, tenant_id, workforce_member_id, scope_type, scope_id,
				starts_at, ends_at, reason_code, status, replacement_member_id, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,'attendance_sheet_absence','approved',$8,now())
			ON CONFLICT (absence_id) DO UPDATE SET replacement_member_id = COALESCE(excluded.replacement_member_id, workforce_absences.replacement_member_id)`,
			absID, tenantID, mID, scopeType, scopeID, startsAt, endsAt, replacementID)
	}); err != nil {
		return ist, fmt.Errorf("insert leave windows: %w", err)
	}
	ist.LeavesInserted = len(leaves)

	// Same transaction as the member/department attachment above: a roster that commits
	// with departments but without module grants is a half-seeded state whose only symptom
	// is an empty bottom bar at sign-in.
	granted, err := grantDefaultDepartmentModules(ctx, tx, tenantID)
	if err != nil {
		return ist, fmt.Errorf("grant department modules: %w", err)
	}
	ist.ModuleGrantsInserted = granted

	if err := tx.Commit(ctx); err != nil {
		return ist, fmt.Errorf("commit: %w", err)
	}
	return ist, nil
}

// grantDefaultDepartmentModules upserts defaultDepartmentModules into
// department_module_grants for the tenant's existing departments, returning the number of
// rows newly inserted or reactivated.
//
// Idempotent (ON CONFLICT on the table's (tenant_id, department_id, module_key) unique
// constraint) and set-based: two flattened arrays joined against departments in ONE
// statement, not a query per department. A department code absent from this tenant simply
// does not join, so the seed never invents a department. An existing 'active' row is left
// untouched so the count reports real change rather than the whole map every run.
func grantDefaultDepartmentModules(ctx context.Context, tx pgx.Tx, tenantID string) (int, error) {
	// Flatten to (code, module_key) pairs, sorted for a deterministic statement.
	codes := make([]string, 0, len(defaultDepartmentModules))
	for code := range defaultDepartmentModules {
		codes = append(codes, code)
	}
	sort.Strings(codes)

	pairCodes := make([]string, 0, len(codes)*2)
	pairModules := make([]string, 0, len(codes)*2)
	for _, code := range codes {
		modules := append([]string(nil), defaultDepartmentModules[code]...)
		sort.Strings(modules)
		for _, module := range modules {
			pairCodes = append(pairCodes, code)
			pairModules = append(pairModules, module)
		}
	}
	if len(pairCodes) == 0 {
		return 0, nil
	}

	tag, err := tx.Exec(ctx, `
INSERT INTO department_module_grants (tenant_id, department_id, module_key, status)
SELECT d.tenant_id, d.department_id, g.module_key, 'active'
FROM unnest($2::text[], $3::text[]) AS g(department_code, module_key)
JOIN departments d
  ON d.tenant_id = $1::uuid
 AND d.code = g.department_code
 AND d.status = 'active'
ON CONFLICT (tenant_id, department_id, module_key) DO UPDATE
SET status = 'active', updated_at = now()
WHERE department_module_grants.status <> 'active'`, tenantID, pairCodes, pairModules)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

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

func validateTarget(env, databaseURL string) error {
	if strings.EqualFold(strings.TrimSpace(env), "stg") {
		return localtarget.ValidateStagingCloudSQLDatabaseTarget("seed-roster-real", env, databaseURL)
	}
	return localtarget.ValidateLocalDatabaseTarget("seed-roster-real", env, databaseURL, "local", "dev", "test")
}
