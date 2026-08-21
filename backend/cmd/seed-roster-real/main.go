// Command seed-roster-real imports the maintainer's reviewed roster mapping +
// Jun-26 attendance sheet into the Goat OS HRMS roster/coverage tables so the
// real website shows real people instead of demo names.
//
// Source data is read from the committed, synthetic, PII-safe fixture by default.
// A maintainer may point -source at a private reviewed export, which must never be committed:
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
	"errors"
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

// verifierPositionCode must match backend/cmd/seed-position-duties.VerifierPositionCode:
// this seeder creates the seat, that one attaches the per-module 'verify' duties to it.
const verifierPositionCode = "video_verifier"

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
	vaccinationCap *int
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
	sourcePath := fs.String("source", "../fixtures/vaccination-hrms-source-full", "path to source data directory")
	attendanceYear := fs.Int("attendance-year", 2026, "calendar year of attendance sheet")
	attendanceMonth := fs.Int("attendance-month", 6, "calendar month (1-12) of attendance sheet")
	strict := fs.Bool("strict", false, "fail before connecting or writing when any roster slot is unresolved/unknown/duplicated or required PC/Backup/Park Head coverage is absent")
	validateOnly := fs.Bool("validate-only", false, "normalize and validate the source without connecting to Postgres or writing rows")
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

	operatorRoster, err := loadOperatorRoster(*sourcePath)
	if err != nil {
		return fmt.Errorf("load operator roster: %w", err)
	}

	members, assignments, st := normalizeAssignments(jun26Members, rosterMappings, weekOffs)
	contractCenters, err := applyOperatorRosterOverlay(operatorRoster, assignments)
	if err != nil {
		return err
	}
	if operatorRoster != nil {
		fmt.Printf("operator-roster overlay applied: park=%s operators=%d\n",
			operatorRoster.SourceScope.ParkCode, len(operatorRoster.Operators))
	}
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
	if *strict {
		if err := validateStrictRoster(rosterMappings, assignments, st, contractCenters); err != nil {
			return err
		}
	}
	if *validateOnly {
		fmt.Println("strict roster source validation complete; no database connection or writes")
		return nil
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
		if *strict {
			return fmt.Errorf("strict roster seed requires the HRMS schema (missing: %s); no database writes were made", missing)
		}
		fmt.Printf("importer built, waiting on HRMS migration to land (missing: %s) -- normalizer counts above are the full dry-run result; no database writes were made.\n", missing)
		return nil
	}

	ist, err := importRoster(ctx, pool, *tenantID, members, assignments, leaves, operatorRoster)
	if err != nil {
		return fmt.Errorf("import: %w", err)
	}
	st.MembersInserted = ist.MembersInserted
	st.PositionsInserted = ist.PositionsInserted
	st.LeavesInserted = ist.LeavesInserted
	st.DepartmentMatches = ist.DepartmentMatches
	st.ModuleGrantsInserted = ist.ModuleGrantsInserted

	fmt.Printf("seeded real roster:\n"+
		"  members_inserted=%d positions_inserted=%d leaves_inserted=%d department_matches=%d module_grants_inserted=%d\n"+
		"  contract_directors_seeded=%d leadership_grants_seeded=%d\n",
		st.MembersInserted, st.PositionsInserted, st.LeavesInserted, st.DepartmentMatches, st.ModuleGrantsInserted,
		ist.DirectorsInserted, ist.LeadershipGrants)
	return nil
}

func validateStrictRoster(mappings []rosterMappingRow, assignments []rosterAssignment, st stats, contractCenters map[string]bool) error {
	problems := make([]string, 0)
	if st.AssignmentsUnresolved != 0 {
		problems = append(problems, fmt.Sprintf("unresolved assignments=%d", st.AssignmentsUnresolved))
	}
	if st.PositionSlotsDefined != st.MappingRows {
		problems = append(problems, fmt.Sprintf("unknown/ignored position labels=%d", st.MappingRows-st.PositionSlotsDefined))
	}
	seen := make(map[string]struct{}, len(assignments))
	required := make(map[string]map[string]bool)
	for _, mapping := range mappings {
		center := strings.TrimSpace(mapping.center)
		if center == "" {
			continue
		}
		// A center whose field capacity is declared by the operator-roster
		// contract seats equal vaccination operators, not the generic
		// PC-manager/backup/park-head trio, so the trio requirement is waived.
		if contractCenters[center] {
			continue
		}
		if _, ok := required[center]; !ok {
			required[center] = map[string]bool{"preventive_care_manager": false, "backup_manager": false, "park_head": false}
		}
	}
	for _, assignment := range assignments {
		key := assignment.center + "\x00" + assignment.position.code
		if _, exists := seen[key]; exists {
			problems = append(problems, "duplicate center/position="+assignment.center+"/"+assignment.position.code)
		}
		seen[key] = struct{}{}
		if byPosition, ok := required[assignment.center]; ok {
			if _, tracked := byPosition[assignment.position.code]; tracked && assignment.isResolved {
				byPosition[assignment.position.code] = true
			}
		}
	}
	for center, byPosition := range required {
		for position, present := range byPosition {
			if !present {
				problems = append(problems, "missing resolved "+center+"/"+position)
			}
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("strict roster preflight failed before database connection: %s", strings.Join(problems, "; "))
	}
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

// ---- operator-roster contract overlay (CPT operator-drive rehearsal packet) ----
//
// The jun-26 timetable model expresses field staff as shared operational seats
// (Preventive Care Manager, Backup Manager, Park Head). A vaccination
// operator-drive rehearsal source instead ships an authoritative operator
// roster contract (`cpt-operator-roster.json`) that declares the field
// operators as EQUAL vaccination operators with per-person position codes and
// contract-owned week-offs. When that contract is present in the source dir it
// is the source of truth for that center's field capacity: it recasts the
// center's resolved seats into per-person `vaccination_operator_<name>`
// positions (manager tier, not a backup slot) and supplies the week-off. This
// removes the park-head/backup labelling the runbook forbids for these seeds
// without disturbing the members, animals, vaccination history, or the generic
// jun-26 model used by every other center/source.
type operatorRosterOperator struct {
	Code                 string   `json:"code"`
	DisplayName          string   `json:"display_name"`
	EmailHint            string   `json:"email_hint"`
	Role                 string   `json:"role"`
	Tier                 string   `json:"tier"`
	CanExecuteVaccinaton bool     `json:"can_execute_vaccination"`
	ParkScope            []string `json:"park_scope"`
	WeekOff              string   `json:"week_off"`
	AnimalCapPerDay      *int     `json:"animal_cap_per_day"`
	ShiftLabel           string   `json:"shift_label"`        // am | pm | rover (scheduler-consumed operator assignment)
	ShiftStartMinute     *int     `json:"shift_start_minute"` // minutes-of-day, 0-1439
	ShiftEndMinute       *int     `json:"shift_end_minute"`   // minutes-of-day, 0-1439
}

// operatorRosterDirector is a monitoring-only person declared by the contract. A director is
// deliberately NOT an operator: he is seeded as a workforce member with NO workforce_positions
// row, no vaccination_daily_animal_cap, and no vaccination_operator_shift_config row, so he adds
// zero field execution capacity. That property is now ENFORCED here rather than holding by
// accident because nothing consumed the block (BUG-024).
type operatorRosterDirector struct {
	Code                 string   `json:"code"`
	DisplayName          string   `json:"display_name"`
	Role                 string   `json:"role"`
	CanExecuteVaccinaton bool     `json:"can_execute_vaccination"`
	ParkScope            []string `json:"park_scope"`
	TimetableRequired    bool     `json:"timetable_required"`
	Notes                []string `json:"notes"`
}

// operatorRosterLeadership is the CEO/CXO tenant-scoped full-access block. Seeded through the
// same auth_pending_email_grants path as backend/cmd/seed-dev-email-grants, so a CPT-only
// reseed produces the leadership grants its own validation doc requires.
type operatorRosterLeadership struct {
	GrantRole     string   `json:"grant_role"`
	ScopeType     string   `json:"scope_type"`
	WorkforceHint string   `json:"workforce_hint"`
	Emails        []string `json:"emails"`
}

// operatorRosterVerifier is a tenant-scoped verifier login declared by the CPT
// seed packet. Verifiers review proof and never add vaccination execution
// capacity, so the roster seeder only seeds the approved-email grant path.
type operatorRosterVerifier struct {
	Code                    string   `json:"code"`
	DisplayName             string   `json:"display_name"`
	Email                   string   `json:"email"`
	Role                    string   `json:"role"`
	IdentityProvider        string   `json:"identity_provider"`
	ParkScope               []string `json:"park_scope"`
	CanExecuteVaccinaton    bool     `json:"can_execute_vaccination"`
	AddsVaccinationCapacity bool     `json:"adds_vaccination_capacity"`
	Notes                   []string `json:"notes"`
}

// operatorRosterAndroidLogin is the post-DB-seed mobile provisioning contract for
// field executors. The roster seeder validates it so the block cannot be silently
// ignored, but it deliberately does not create Firebase users: cloud identity
// provisioning happens after DB seed through the Auth/Firebase runbook.
type operatorRosterAndroidLogin struct {
	RequiredAfterDatabaseSeed           bool     `json:"required_after_database_seed"`
	IdentityProvider                    string   `json:"identity_provider"`
	SourceEmailField                    string   `json:"source_email_field"`
	UniqueEmailPerOperator              bool     `json:"unique_email_per_operator"`
	UniqueTemporaryPasswordPerOperator  bool     `json:"unique_temporary_password_per_operator"`
	SharedPasswordForbidden             bool     `json:"shared_password_forbidden"`
	PlaintextPasswordsInGitForbidden    bool     `json:"plaintext_passwords_in_git_forbidden"`
	MustSendOrRecordIndividualResetFlow bool     `json:"must_send_or_record_individual_reset_flow"`
	AndroidLoginSmokeRequired           bool     `json:"android_login_smoke_required"`
	Notes                               []string `json:"notes"`
}

type operatorRosterContract struct {
	Schema           string `json:"schema"`
	BusinessDate     string `json:"business_date"`
	OperatorCapacity struct {
		Unit                  string `json:"unit"`
		DefaultAnimalsPerDay  *int   `json:"default_animals_per_day"`
		DoseCountIsNotCapaity bool   `json:"dose_count_is_not_capacity"`
		SeedCatchupOverrides  []struct {
			Date         string `json:"date"`
			OperatorCode string `json:"operator_code"`
			MaxAnimals   int    `json:"max_animals"`
			Reason       string `json:"reason"`
		} `json:"seed_catchup_overrides"`
	} `json:"operator_capacity"`
	SourceScope struct {
		Tenant           string   `json:"tenant"`
		ParkCode         string   `json:"park_code"`
		ParkName         string   `json:"park_name"`
		CentersAllowed   []string `json:"centers_allowed"`
		CentersForbidden []string `json:"centers_forbidden"`
		Notes            []string `json:"notes"`
	} `json:"source_scope"`
	Operators            []operatorRosterOperator    `json:"operators"`
	OperatorAndroidLogin *operatorRosterAndroidLogin `json:"operator_android_login"`
	// DefaultOperatorAssignment is the CEO-set default operator + N-active-operators for this park
	// (vaccination_operator_assignment_config). Optional -- absent means the seed does not author an
	// assignment config row (never a silent default).
	DefaultOperatorAssignment *struct {
		ActiveOperatorsPerDay         int      `json:"active_operators_per_day"`
		DefaultOperatorCode           string   `json:"default_operator_code"`
		FallbackOperatorCode          string   `json:"fallback_operator_code"`
		FallbackWhen                  string   `json:"fallback_when"`
		SecondaryFallbackOperatorCode string   `json:"secondary_fallback_operator_code"`
		SecondaryFallbackWhen         string   `json:"secondary_fallback_when"`
		DriveGrain                    string   `json:"drive_grain"`
		ShiftLabelSemantics           string   `json:"shift_label_semantics"`
		Notes                         []string `json:"notes"`
	} `json:"default_operator_assignment"`
	Directors             []operatorRosterDirector  `json:"directors"`
	Verifiers             []operatorRosterVerifier  `json:"verifiers"`
	LeadershipFullAccess  *operatorRosterLeadership `json:"leadership_full_access"`
	WeeklyCapacityExample []struct {
		Weekday                    string   `json:"weekday"`
		Date                       string   `json:"date"`
		AvailableOperators         []string `json:"available_operators"`
		OperatorOff                []string `json:"operator_off"`
		TotalCapacityAnimals       int      `json:"total_capacity_animals"`
		DriveAssignedOperatorCount int      `json:"drive_assigned_operator_count"`
		DriveCapacityAnimals       int      `json:"drive_capacity_animals"`
	} `json:"weekly_capacity_examples"`
}

// loadOperatorRoster returns the parsed operator-roster contract when the source
// dir contains `cpt-operator-roster.json`, or (nil, nil) when it is absent (the
// normal jun-26 source has no such file).
func loadOperatorRoster(sourcePath string) (*operatorRosterContract, error) {
	raw, err := os.ReadFile(sourcePath + "/cpt-operator-roster.json")
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read operator roster: %w", err)
	}
	var contract operatorRosterContract
	// BUG-024 root cause: encoding/json silently drops keys with no matching struct field, so
	// the contract's `directors` and `leadership_full_access` blocks were parsed, discarded,
	// and never seeded -- with no error and no warning. DisallowUnknownFields turns any block
	// this command does not consume into a hard, named failure instead of a silent drop.
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&contract); err != nil {
		return nil, fmt.Errorf("parse operator roster (every declared block must be consumed by this seeder; add the field or remove the block): %w", err)
	}
	if strings.TrimSpace(contract.SourceScope.ParkCode) == "" {
		return nil, fmt.Errorf("operator roster missing source_scope.park_code")
	}
	if len(contract.Operators) == 0 {
		return nil, fmt.Errorf("operator roster has no operators")
	}
	operatorCodes := make(map[string]bool, len(contract.Operators))
	operatorKeys := make(map[string]bool, len(contract.Operators))
	operatorEmails := make(map[string]string, len(contract.Operators))
	for _, op := range contract.Operators {
		operatorCodes[op.Code] = true
		operatorKeys[operatorNameKey(op.DisplayName)] = true
		email := strings.ToLower(strings.TrimSpace(op.EmailHint))
		if email == "" || !strings.Contains(email, "@") {
			return nil, fmt.Errorf("operator roster operator %s requires email_hint for Android login provisioning", op.Code)
		}
		if previous := operatorEmails[email]; previous != "" {
			return nil, fmt.Errorf("operator roster operators %s and %s share email_hint %q; Android operator logins must be unique", previous, op.Code, email)
		}
		operatorEmails[email] = op.Code
	}
	if contract.OperatorAndroidLogin == nil {
		return nil, fmt.Errorf("operator roster operator_android_login contract is required for Android login provisioning")
	}
	if err := validateOperatorAndroidLoginContract(contract.OperatorAndroidLogin); err != nil {
		return nil, err
	}
	for _, director := range contract.Directors {
		if strings.TrimSpace(director.Code) == "" || strings.TrimSpace(director.DisplayName) == "" {
			return nil, fmt.Errorf("operator roster director requires code and display_name")
		}
		// The director-has-no-execution-capacity rule is an invariant, not an accident.
		if director.CanExecuteVaccinaton {
			return nil, fmt.Errorf("operator roster director %s declares can_execute_vaccination=true; a director adds no field capacity unless seeded as an explicit operator", director.Code)
		}
		if operatorCodes[director.Code] || operatorKeys[operatorNameKey(director.DisplayName)] {
			return nil, fmt.Errorf("operator roster director %s is also declared as a vaccination operator", director.Code)
		}
	}
	for _, verifier := range contract.Verifiers {
		if strings.TrimSpace(verifier.Code) == "" || strings.TrimSpace(verifier.DisplayName) == "" {
			return nil, fmt.Errorf("operator roster verifier requires code and display_name")
		}
		email := strings.ToLower(strings.TrimSpace(verifier.Email))
		if email == "" || !strings.Contains(email, "@") {
			return nil, fmt.Errorf("operator roster verifier %s requires email", verifier.Code)
		}
		if strings.TrimSpace(verifier.Role) != "verifier" {
			return nil, fmt.Errorf("operator roster verifier %s role must be verifier", verifier.Code)
		}
		if strings.TrimSpace(verifier.IdentityProvider) != "firebase_email_password" {
			return nil, fmt.Errorf("operator roster verifier %s identity_provider must be firebase_email_password", verifier.Code)
		}
		if verifier.CanExecuteVaccinaton || verifier.AddsVaccinationCapacity {
			return nil, fmt.Errorf("operator roster verifier %s must not execute vaccination or add capacity", verifier.Code)
		}
		if operatorCodes[verifier.Code] || operatorKeys[operatorNameKey(verifier.DisplayName)] {
			return nil, fmt.Errorf("operator roster verifier %s is also declared as a vaccination operator", verifier.Code)
		}
	}
	if lead := contract.LeadershipFullAccess; lead != nil {
		if len(lead.Emails) == 0 {
			return nil, fmt.Errorf("operator roster leadership_full_access declares no emails")
		}
		if strings.TrimSpace(lead.GrantRole) == "" {
			return nil, fmt.Errorf("operator roster leadership_full_access requires grant_role")
		}
		if scope := strings.TrimSpace(lead.ScopeType); scope != "" && scope != "tenant" {
			return nil, fmt.Errorf("operator roster leadership_full_access scope_type %q is unsupported; only tenant-scoped leadership grants are seeded", scope)
		}
		for _, email := range lead.Emails {
			if !strings.Contains(email, "@") {
				return nil, fmt.Errorf("operator roster leadership_full_access email %q is not an email address", email)
			}
		}
	}
	return &contract, nil
}

func validateOperatorAndroidLoginContract(login *operatorRosterAndroidLogin) error {
	if !login.RequiredAfterDatabaseSeed {
		return fmt.Errorf("operator roster operator_android_login.required_after_database_seed must be true")
	}
	if strings.TrimSpace(login.IdentityProvider) != "firebase_email_password" {
		return fmt.Errorf("operator roster operator_android_login.identity_provider must be firebase_email_password")
	}
	if strings.TrimSpace(login.SourceEmailField) != "operators[].email_hint" {
		return fmt.Errorf("operator roster operator_android_login.source_email_field must be operators[].email_hint")
	}
	if !login.UniqueEmailPerOperator {
		return fmt.Errorf("operator roster operator_android_login.unique_email_per_operator must be true")
	}
	if !login.UniqueTemporaryPasswordPerOperator {
		return fmt.Errorf("operator roster operator_android_login.unique_temporary_password_per_operator must be true")
	}
	if !login.SharedPasswordForbidden {
		return fmt.Errorf("operator roster operator_android_login.shared_password_forbidden must be true")
	}
	if !login.PlaintextPasswordsInGitForbidden {
		return fmt.Errorf("operator roster operator_android_login.plaintext_passwords_in_git_forbidden must be true")
	}
	if !login.MustSendOrRecordIndividualResetFlow {
		return fmt.Errorf("operator roster operator_android_login.must_send_or_record_individual_reset_flow must be true")
	}
	if !login.AndroidLoginSmokeRequired {
		return fmt.Errorf("operator roster operator_android_login.android_login_smoke_required must be true")
	}
	return nil
}

// operatorNameKey is the deterministic join key between a contract operator and
// a resolved roster seat: the lowercased first token of the person's name,
// which equals the `vaccination_operator_<name>` code suffix.
func operatorNameKey(name string) string {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(name)))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// applyOperatorRosterOverlay recasts the contract park's resolved seats into
// per-person vaccination-operator positions. It returns the set of centers the
// contract covers (so strict validation can waive the generic PC/Backup/Park
// Head requirement for them) and errors if any declared operator could not be
// matched to a resolved seat (a broken source, never a silent drop).
func applyOperatorRosterOverlay(contract *operatorRosterContract, assignments []rosterAssignment) (map[string]bool, error) {
	if contract == nil {
		return map[string]bool{}, nil
	}
	park := strings.TrimSpace(contract.SourceScope.ParkCode)
	byKey := make(map[string]operatorRosterOperator, len(contract.Operators))
	for _, op := range contract.Operators {
		byKey[operatorNameKey(op.DisplayName)] = op
	}
	matched := make(map[string]bool, len(byKey))
	for i := range assignments {
		a := &assignments[i]
		if a.center != park || !a.isResolved {
			continue
		}
		op, ok := byKey[operatorNameKey(a.jun26Name)]
		if !ok {
			continue
		}
		a.position = positionDef{
			code:         op.Code,
			tier:         op.Tier,
			isBackupSlot: false,
			backupGroup:  "",
			deptGuess:    "preventive_care",
		}
		if wk := normalizeWeekday(op.WeekOff); wk != "" {
			a.weekOffWeekday = wk
		}
		cap := contract.OperatorCapacity.DefaultAnimalsPerDay
		if op.AnimalCapPerDay != nil {
			cap = op.AnimalCapPerDay
		}
		if cap != nil && *cap > 0 {
			v := *cap
			a.vaccinationCap = &v
		}
		matched[operatorNameKey(a.jun26Name)] = true
	}
	var missing []string
	for key, op := range byKey {
		if !matched[key] {
			missing = append(missing, op.Code)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("operator roster contract lists operators with no resolved %s seat: %s", park, strings.Join(missing, ", "))
	}
	return map[string]bool{park: true}, nil
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
	DirectorsInserted    int
	LeadershipGrants     int
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
//   - health -> aas_health + counts + feed_direction + vaccination: maintainer decision
//     2026-07-30. Health/Kidding operators own the Adult/Kids Health worklists and also perform
//     Counts, Feed, and Vaccination work. This is a department grant, not a blanket expansion of
//     every operator. aas_health is the backend module key whose display label is "Health".
//   - preventive_care -> counts: Counts carries birth/death capture, and the maintainer-approved
//     rule is that field operators record birth and death from the app
//     (docs/runbooks/android-dev-device.md). PC seats are in the sheds daily.
//   - preventive_care -> feed_direction: maintainer decision 2026-07-22 — field operators now
//     see the Feed vertical on the phone (they dispatch and pack what the direction says). The
//     matching protocol.read / feed_packing.read grants were added to RoleOperator in the same
//     change; this module grant is what makes the Feed module actually render for a PC operator.
//   - feed -> feed_direction and breeding -> breeding light those departments up too. breeding is
//     a registry-declared "soon" module (harmless to grant early); feed_direction is available.
//
// Departments with no operational module today (procurement, growth, infrastructure, milk,
// sales) are deliberately absent. A department key that does not exist in this tenant is
// skipped by the INSERT ... SELECT below, never an error.
//
//   - milk accompanied counts everywhere it was granted: Milk Preparation and Milk Feeding moved
//     out of the Counts module into their own drawer module (maintainer decision 2026-07-31)
//     while keeping their /counts/... routes and CountsWrite authority, so a department holding
//     counts also held milk or those two daily pages silently vanished from its bottom bar.
//     Migration 000064 applied the same rule to already-seeded databases.
//
//     That coupling is NO LONGER automatic. Maintainer decision 2026-08-05 removes BOTH milk and
//     aas_health from preventive_care, so a PC seat's bottom bar is Vaccination + Counts + Feed
//     only. Losing Milk Prep / Milk Feeding from the PC bar is the POINT of that decision, not the
//     defect the old rule guarded against — health keeps both modules and is unchanged. The pages
//     themselves are untouched: /counts/milk-preparation and /counts/milk-feeding still exist and
//     still require CountsWrite; only the PC nav entry is gone. Because the coupling is now a
//     per-department decision rather than an invariant, TestDefaultDepartmentModulesMatchDecisions
//     pins this map exactly instead of asserting counts-implies-milk. Migration
//     000110_preventive_care_drop_milk_aas_health.sql applies the same removal to already-seeded
//     databases (aas_health reached preventive_care via migration 000098, which copied every
//     vaccination grant, so it is not in this seed map to begin with).
var defaultDepartmentModules = map[string][]string{
	// pc_care (maintainer decision 2026-08-21): deworming / ticks removal / hoof trimming /
	// hair trimming live beside vaccination in the Preventive Care department. Migration
	// 000180_pc_care_module_grants.sql applies the same grant to already-seeded databases.
	"preventive_care": {"vaccination", "counts", "feed_direction", "pc_care"},
	"health":          {"aas_health", "counts", "milk", "feed_direction", "vaccination"},
	"feed":            {"feed_direction"},
	"breeding":        {"breeding"},
}

func importRoster(ctx context.Context, pool *pgxpool.Pool, tenantID string, members map[string]*memberRec, assignments []rosterAssignment, leaves []leaveWindow, operatorRoster *operatorRosterContract) (importStats, error) {
	var ist importStats

	tx, err := pool.Begin(ctx)
	if err != nil {
		return ist, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Resolve only the center locations actually present in this source bundle.
	// CPT operator-drive packets are intentionally CPT-only; requiring CBE here
	// makes a clean local DB reseed fail before vaccination import can create the
	// source-owned park locations.
	centerLocationID := map[string]string{}
	requiredCenters := map[string]bool{}
	for _, m := range members {
		center := strings.TrimSpace(m.location)
		if center == "CBE" || center == "CPT" {
			requiredCenters[center] = true
		}
	}
	for _, assignment := range assignments {
		center := strings.TrimSpace(assignment.center)
		if center == "CBE" || center == "CPT" {
			requiredCenters[center] = true
		}
	}
	for center := range requiredCenters {
		var locationID string
		if err := tx.QueryRow(ctx, `SELECT location_id FROM locations WHERE tenant_id=$1 AND location_type='park' AND location_code=$2 LIMIT 1`, tenantID, center).Scan(&locationID); err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				return ist, fmt.Errorf("resolve center location %s: %w", center, err)
			}
			locationID = detUUID("location", tenantID, "park", center)
			name := center
			if center == "CPT" {
				name = "Channapatna"
			} else if center == "CBE" {
				name = "Coimbatore"
			}
			if _, err := tx.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($1::uuid, $2::uuid, 'park', $3, $4, 'active')
ON CONFLICT (location_id) DO UPDATE
SET location_code = EXCLUDED.location_code,
    name = EXCLUDED.name,
    status = EXCLUDED.status,
    updated_at = now()`,
				locationID, tenantID, center, name); err != nil {
				return ist, fmt.Errorf("upsert center location %s: %w", center, err)
			}
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

		// A member is a vaccination-executing operator if they hold any active
		// vaccination_operator_* position (the operator-roster overlay stamps that
		// code onto their resolved seat). Computed from the post-overlay assignments,
		// not from bestCode/bestTier, which were frozen from the original June seat.
		var execVaccOperator bool
		for _, a := range assignments {
			if a.jun26Name == key && strings.HasPrefix(a.position.code, "vaccination_operator_") {
				execVaccOperator = true
				break
			}
		}

		roleHint := deriveRoleHint(m, grade, execVaccOperator)

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
		vaccinationCap *int
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
			vaccinationCap *int
		}{posID: posID, mID: mID, center: a.center, def: a.position, weekOffWeekday: a.weekOffWeekday, vaccinationCap: a.vaccinationCap})
	}

	if err := batch(ctx, tx, positionRows, 200, func(b *pgx.Batch, p struct {
		posID          string
		mID            string
		center         string
		def            positionDef
		weekOffWeekday string
		vaccinationCap *int
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
				position_code, position_tier, is_backup_slot, backup_group_code, week_off_weekday, vaccination_daily_animal_cap, status, valid_from, updated_at)
			VALUES ($1,$2,$3,'center',$4,$5,$6,$7,$8,$9,$10,'active',now(),now())
			ON CONFLICT (position_id) DO UPDATE SET
				workforce_member_id = EXCLUDED.workforce_member_id,
				scope_type = EXCLUDED.scope_type,
				scope_id = EXCLUDED.scope_id,
				position_code = EXCLUDED.position_code,
				position_tier = EXCLUDED.position_tier,
				is_backup_slot = EXCLUDED.is_backup_slot,
				backup_group_code = EXCLUDED.backup_group_code,
				week_off_weekday = EXCLUDED.week_off_weekday,
				vaccination_daily_animal_cap = EXCLUDED.vaccination_daily_animal_cap,
				status = EXCLUDED.status,
				valid_to = NULL,
				updated_at = now()`,
			p.posID, tenantID, p.mID, centerLocationID[p.center], p.def.code, p.def.tier, p.def.isBackupSlot, backupGroup, weekOff, p.vaccinationCap)
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

	// Scheduler-consumed operator shift + N-active-operators-per-day default assignment config.
	// Same transaction as the roster import (seed-migration coupling): a roster commit without its
	// shift/default config is a half-seeded state that renders an empty operator-assignment Config
	// screen and makes the scheduler ignore the intended default/N-operator rule.
	if err := seedOperatorAssignmentConfig(ctx, tx, tenantID, centerLocationID, operatorRoster, assignments, memberID); err != nil {
		return ist, fmt.Errorf("seed operator assignment config: %w", err)
	}

	// BUG-024: the contract's directors / leadership_full_access blocks used to be parsed and
	// thrown away. Same transaction as the roster import: a roster that commits its operators
	// without the monitoring director and the CEO/CXO grants is exactly the half-seeded HRMS
	// state LOCAL_DB_RESEED_VALIDATION.md forbids.
	directors, grants, err := seedContractPeopleAndEmailGrants(ctx, tx, tenantID, centerLocationID, operatorRoster, resolveDept, assignments)
	if err != nil {
		return ist, fmt.Errorf("seed contract directors/leadership: %w", err)
	}
	ist.DirectorsInserted = directors
	ist.LeadershipGrants = grants

	if err := tx.Commit(ctx); err != nil {
		return ist, fmt.Errorf("commit: %w", err)
	}
	return ist, nil
}

// seedContractPeopleAndEmailGrants seeds the operator-roster contract blocks that the seeder
// previously dropped on the floor (BUG-024):
//
//   - `directors[]`  -> a workforce_members row per director with hr_designation_grade=director,
//     scoped to the contract park, and deliberately NO workforce_positions row: no operational
//     position means no vaccination_daily_animal_cap and no shift config, so the director carries
//     zero field execution capacity. If a director somehow resolved to a roster seat, that is a
//     broken source and a hard error, not a silently seeded operator.
//   - `leadership_full_access` -> tenant-scoped auth_pending_email_grants rows, the same table and
//     shape backend/cmd/seed-dev-email-grants writes, so a CPT-only reseed no longer depends on
//     GOATOS_DEV_DASHBOARD_ADMIN_EMAILS being set by hand.
//   - `verifiers[]` -> tenant-scoped auth_pending_email_grants rows for proof
//     reviewers, PLUS a workforce_members row and one `video_verifier`
//     workforce_positions seat per park in scope. The seat is required for
//     notification routing (position_module_duties 'verify' rows join
//     workforce_positions), NOT for execution: verifiers still get no
//     vaccination_daily_animal_cap and no vaccination capacity.
//
// No-ops for sources without the contract. Returns (directorsSeeded, leadershipGrantsSeeded).
func seedContractPeopleAndEmailGrants(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	centerLocationID map[string]string,
	contract *operatorRosterContract,
	resolveDept func(string) (*string, error),
	assignments []rosterAssignment,
) (int, int, error) {
	if contract == nil {
		grants, err := seedVerifierSeatsFromExistingGrants(ctx, tx, tenantID, centerLocationID)
		return 0, grants, err
	}
	park := strings.TrimSpace(contract.SourceScope.ParkCode)

	seatKeys := map[string]bool{}
	for _, a := range assignments {
		if a.isResolved {
			seatKeys[operatorNameKey(a.jun26Name)] = true
		}
	}

	directorsSeeded := 0
	for _, director := range contract.Directors {
		if seatKeys[operatorNameKey(director.DisplayName)] {
			return 0, 0, fmt.Errorf("director %s resolved to an operational roster seat; a monitoring director must hold no operational position", director.Code)
		}
		deptID, err := resolveDept("preventive_care")
		if err != nil {
			return 0, 0, fmt.Errorf("resolve director department: %w", err)
		}
		var locationID *string
		if id, ok := centerLocationID[park]; ok && id != "" {
			locationID = &id
		}
		grade := "director"
		id := detUUID("workforce_member", "operator_roster_director", tenantID, director.Code)
		if _, err := tx.Exec(ctx, `
			INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status,
				primary_role_hint, primary_location_id, department_id, hr_designation_grade, updated_at)
			VALUES ($1,$2,$3,$4,'active','pc_director',$5,$6,$7,now())
			ON CONFLICT (workforce_member_id) DO UPDATE SET
				display_code = EXCLUDED.display_code,
				display_name = EXCLUDED.display_name,
				status = EXCLUDED.status,
				primary_role_hint = EXCLUDED.primary_role_hint,
				primary_location_id = EXCLUDED.primary_location_id,
				department_id = EXCLUDED.department_id,
				hr_designation_grade = EXCLUDED.hr_designation_grade,
				updated_at = now()`,
			id, tenantID, strings.ToUpper(strings.ReplaceAll(director.Code, "_", "-")), director.DisplayName,
			locationID, deptID, grade); err != nil {
			return 0, 0, fmt.Errorf("insert director %s: %w", director.Code, err)
		}
		// Fail closed if a previous/parallel path gave this director an operational position:
		// the "director has zero execution capacity" rule must be asserted, not assumed.
		var positions int
		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM workforce_positions WHERE tenant_id=$1 AND workforce_member_id=$2 AND status='active'`,
			tenantID, id).Scan(&positions); err != nil {
			return 0, 0, fmt.Errorf("verify director %s has no operational position: %w", director.Code, err)
		}
		if positions != 0 {
			return 0, 0, fmt.Errorf("director %s holds %d active workforce position(s); a monitoring director must have none", director.Code, positions)
		}
		directorsSeeded++
	}

	grantsSeeded := 0
	if lead := contract.LeadershipFullAccess; lead != nil {
		emails := append([]string(nil), lead.Emails...)
		sort.Strings(emails)
		for _, raw := range emails {
			email := strings.ToLower(strings.TrimSpace(raw))
			if email == "" {
				continue
			}
			if _, err := tx.Exec(ctx, `
INSERT INTO auth_pending_email_grants (
  tenant_id, email, normalized_email, role, scope_type, scope_id, status, valid_from, source
) VALUES ($1, $2, $2, $3, 'tenant', $1, 'active', now(), 'cpt_operator_roster_contract')
ON CONFLICT (tenant_id, normalized_email, role, scope_type, scope_id)
  WHERE status = 'active' AND valid_to IS NULL
DO UPDATE SET email = EXCLUDED.email, source = EXCLUDED.source, updated_at = now()`,
				tenantID, email, lead.GrantRole); err != nil {
				return 0, 0, fmt.Errorf("upsert leadership grant: %w", err)
			}
			grantsSeeded++
		}
	}
	for _, verifier := range contract.Verifiers {
		email := strings.ToLower(strings.TrimSpace(verifier.Email))
		if email == "" {
			continue
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO auth_pending_email_grants (
  tenant_id, email, normalized_email, role, scope_type, scope_id, status, valid_from, source
) VALUES ($1, $2, $2, 'verifier', 'tenant', $1, 'active', now(), 'cpt_operator_roster_contract')
ON CONFLICT (tenant_id, normalized_email, role, scope_type, scope_id)
  WHERE status = 'active' AND valid_to IS NULL
DO UPDATE SET email = EXCLUDED.email, source = EXCLUDED.source, updated_at = now()`,
			tenantID, email); err != nil {
			return 0, 0, fmt.Errorf("upsert verifier grant: %w", err)
		}
		grantsSeeded++

		// The verifier ALSO needs a workforce seat, and this is not cosmetic HR data.
		// verification.item.pending routes to whoever holds a 'verify' duty in
		// position_module_duties, and that table joins workforce_positions -- so a verifier
		// with an email grant but no seat is a verifier no push can ever reach. That was the
		// live state: zero verify duty rows, every verifier notification resolving to zero
		// devices, for vaccination and weighing alike.
		//
		// The verifier stays TENANT-level in meaning (one person reviews every park's proof),
		// but ResolveModuleDutyRecipients looks up verify duty holders at CENTER scope using
		// the item's park id, so the SAME member holds the seat at every park in scope. It
		// carries no vaccination_daily_animal_cap and its position_code matches no vaccination
		// prefix in seed-position-duties, so it adds no execution capacity -- the contract
		// invariant validated above (can_execute_vaccination / adds_vaccination_capacity false)
		// is preserved.
		verifierMemberID := detUUID("workforce_member", "operator_roster_verifier", tenantID, verifier.Code)
		if _, err := tx.Exec(ctx, `
			INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status,
				primary_role_hint, updated_at)
			VALUES ($1,$2,$3,$4,'active','verifier',now())
			ON CONFLICT (workforce_member_id) DO UPDATE SET
				display_code = EXCLUDED.display_code,
				display_name = EXCLUDED.display_name,
				status = EXCLUDED.status,
				primary_role_hint = EXCLUDED.primary_role_hint,
				updated_at = now()`,
			verifierMemberID, tenantID, strings.ToUpper(strings.ReplaceAll(verifier.Code, "_", "-")), verifier.DisplayName); err != nil {
			return 0, 0, fmt.Errorf("insert verifier member %s: %w", verifier.Code, err)
		}

		parks := verifier.ParkScope
		if len(parks) == 0 {
			// No declared scope means tenant-wide, which for seat purposes is every seeded park.
			for park := range centerLocationID {
				parks = append(parks, park)
			}
		}
		sort.Strings(parks)
		for _, parkCode := range parks {
			locationID, ok := centerLocationID[strings.TrimSpace(parkCode)]
			if !ok || locationID == "" {
				return 0, 0, fmt.Errorf("verifier %s is scoped to park %q which has no resolved location id", verifier.Code, parkCode)
			}
			posID := detUUID("workforce_position", tenantID, parkCode, verifierPositionCode)
			if _, err := tx.Exec(ctx, `
				INSERT INTO workforce_positions (position_id, tenant_id, workforce_member_id, scope_type, scope_id,
					position_code, position_tier, is_backup_slot, status, valid_from, updated_at)
				VALUES ($1,$2,$3,'center',$4,$5,'manager',false,'active',now(),now())
				ON CONFLICT (position_id) DO UPDATE SET
					workforce_member_id = EXCLUDED.workforce_member_id,
					scope_type = EXCLUDED.scope_type,
					scope_id = EXCLUDED.scope_id,
					position_code = EXCLUDED.position_code,
					position_tier = EXCLUDED.position_tier,
					status = EXCLUDED.status,
					valid_to = NULL,
					updated_at = now()`,
				posID, tenantID, verifierMemberID, locationID, verifierPositionCode); err != nil {
				return 0, 0, fmt.Errorf("insert verifier seat %s@%s: %w", verifier.Code, parkCode, err)
			}
		}
	}
	return directorsSeeded, grantsSeeded, nil
}

func seedVerifierSeatsFromExistingGrants(ctx context.Context, tx pgx.Tx, tenantID string, centerLocationID map[string]string) (int, error) {
	rows, err := tx.Query(ctx, `
SELECT normalized_email
FROM auth_pending_email_grants
WHERE tenant_id = $1::uuid
  AND role = 'verifier'
  AND scope_type = 'tenant'
  AND status = 'active'
  AND (valid_to IS NULL OR valid_to > now())
ORDER BY normalized_email`, tenantID)
	if err != nil {
		return 0, fmt.Errorf("load existing verifier grants: %w", err)
	}
	defer rows.Close()

	type verifierGrant struct {
		code        string
		displayName string
		email       string
	}
	var verifiers []verifierGrant
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return 0, fmt.Errorf("scan verifier grant: %w", err)
		}
		local := strings.Split(email, "@")[0]
		code := "verifier_" + verifierCodeSlug(local)
		if code == "verifier_" {
			code = "verifier_seed"
		}
		verifiers = append(verifiers, verifierGrant{
			code:        code,
			displayName: email,
			email:       email,
		})
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("read verifier grants: %w", err)
	}
	if len(verifiers) == 0 {
		return 0, nil
	}

	inserted := 0
	parks := make([]string, 0, len(centerLocationID))
	for park := range centerLocationID {
		parks = append(parks, park)
	}
	sort.Strings(parks)
	for _, verifier := range verifiers {
		memberID := detUUID("workforce_member", "existing_verifier_grant", tenantID, verifier.email)
		if _, err := tx.Exec(ctx, `
			INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status,
				primary_role_hint, updated_at)
			VALUES ($1,$2,$3,$4,'active','verifier',now())
			ON CONFLICT (workforce_member_id) DO UPDATE SET
				display_code = EXCLUDED.display_code,
				display_name = EXCLUDED.display_name,
				status = EXCLUDED.status,
				primary_role_hint = EXCLUDED.primary_role_hint,
				updated_at = now()`,
			memberID, tenantID, strings.ToUpper(strings.ReplaceAll(verifier.code, "_", "-")), verifier.displayName); err != nil {
			return inserted, fmt.Errorf("insert existing verifier member %s: %w", verifier.code, err)
		}
		for _, parkCode := range parks {
			locationID := centerLocationID[parkCode]
			if locationID == "" {
				continue
			}
			posID := detUUID("workforce_position", tenantID, parkCode, verifierPositionCode)
			if _, err := tx.Exec(ctx, `
				INSERT INTO workforce_positions (position_id, tenant_id, workforce_member_id, scope_type, scope_id,
					position_code, position_tier, is_backup_slot, status, valid_from, updated_at)
				VALUES ($1,$2,$3,'center',$4,$5,'manager',false,'active',now(),now())
				ON CONFLICT (position_id) DO UPDATE SET
					workforce_member_id = EXCLUDED.workforce_member_id,
					scope_type = EXCLUDED.scope_type,
					scope_id = EXCLUDED.scope_id,
					position_code = EXCLUDED.position_code,
					position_tier = EXCLUDED.position_tier,
					status = EXCLUDED.status,
					valid_to = NULL,
					updated_at = now()`,
				posID, tenantID, memberID, locationID, verifierPositionCode); err != nil {
				return inserted, fmt.Errorf("insert existing verifier seat %s@%s: %w", verifier.code, parkCode, err)
			}
			inserted++
		}
	}
	return inserted, nil
}

func verifierCodeSlug(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(raw)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

// seedOperatorAssignmentConfig upserts vaccination_operator_shift_config for every operator in the
// contract that declares a shift, plus vaccination_operator_assignment_config when the contract
// authors a default_operator_assignment. No-ops (contract == nil) for sources without the CPT operator
// roster overlay. Never invents a default operator -- an authored N with no resolvable
// default_operator_code is a hard error, not a silent skip.
func seedOperatorAssignmentConfig(ctx context.Context, tx pgx.Tx, tenantID string, centerLocationID map[string]string, contract *operatorRosterContract, assignments []rosterAssignment, memberID map[string]string) error {
	if contract == nil {
		return nil
	}
	park := strings.TrimSpace(contract.SourceScope.ParkCode)
	parkID, ok := centerLocationID[park]
	if !ok || parkID == "" {
		return fmt.Errorf("operator assignment config: park %s has no resolved location id", park)
	}

	// Resolve operatorNameKey -> workforce_member_id the same way applyOperatorRosterOverlay matched
	// contract operators to resolved roster seats (a.jun26Name is the memberID map's key space).
	memberIDByOperatorKey := map[string]string{}
	for _, a := range assignments {
		if a.center != park || !a.isResolved {
			continue
		}
		if mID, ok := memberID[a.jun26Name]; ok && mID != "" {
			memberIDByOperatorKey[operatorNameKey(a.jun26Name)] = mID
		}
	}

	operatorIDByCode := map[string]string{}
	for _, op := range contract.Operators {
		if op.ShiftLabel == "" || op.ShiftStartMinute == nil || op.ShiftEndMinute == nil {
			continue // this operator's contract row does not author a shift; skip (validate-or-reject at the row level, not the whole seed)
		}
		mID, ok := memberIDByOperatorKey[operatorNameKey(op.DisplayName)]
		if !ok || mID == "" {
			return fmt.Errorf("operator assignment config: operator %s (%s) has no resolved workforce member id", op.Code, op.DisplayName)
		}
		operatorIDByCode[op.Code] = mID
		weekOff := normalizeWeekday(op.WeekOff)
		var weekOffArg any
		if weekOff != "" {
			weekOffArg = weekOff
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO vaccination_operator_shift_config
  (tenant_id, operator_id, park_id, shift_label, shift_start_minute, shift_end_minute, week_off_weekday, updated_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7, now())
ON CONFLICT (tenant_id, operator_id, park_id) DO UPDATE SET
  shift_label = EXCLUDED.shift_label,
  shift_start_minute = EXCLUDED.shift_start_minute,
  shift_end_minute = EXCLUDED.shift_end_minute,
  week_off_weekday = EXCLUDED.week_off_weekday,
  updated_at = now();`,
			tenantID, mID, parkID, op.ShiftLabel, *op.ShiftStartMinute, *op.ShiftEndMinute, weekOffArg); err != nil {
			return fmt.Errorf("upsert shift config for %s: %w", op.Code, err)
		}
	}

	for _, override := range contract.OperatorCapacity.SeedCatchupOverrides {
		operatorID, ok := operatorIDByCode[override.OperatorCode]
		if !ok || operatorID == "" {
			return fmt.Errorf("operator capacity override: operator_code %q does not resolve to a seeded shift operator", override.OperatorCode)
		}
		if _, err := time.Parse("2006-01-02", strings.TrimSpace(override.Date)); err != nil {
			return fmt.Errorf("operator capacity override for %s has invalid date %q: %w", override.OperatorCode, override.Date, err)
		}
		if override.MaxAnimals < 1 {
			return fmt.Errorf("operator capacity override for %s on %s has max_animals=%d", override.OperatorCode, override.Date, override.MaxAnimals)
		}
		if strings.TrimSpace(override.Reason) == "" {
			return fmt.Errorf("operator capacity override for %s on %s requires reason", override.OperatorCode, override.Date)
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO vaccination_operator_capacity_overrides
  (tenant_id, park_id, operator_id, capacity_date, max_animals, reason, updated_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::date, $5, $6, now())
ON CONFLICT (tenant_id, park_id, operator_id, capacity_date) DO UPDATE SET
  max_animals = EXCLUDED.max_animals,
  reason = EXCLUDED.reason,
  updated_at = now();`,
			tenantID, parkID, operatorID, override.Date, override.MaxAnimals, strings.TrimSpace(override.Reason)); err != nil {
			return fmt.Errorf("upsert capacity override for %s on %s: %w", override.OperatorCode, override.Date, err)
		}
	}

	if contract.DefaultOperatorAssignment == nil {
		return nil
	}
	defaultID, ok := operatorIDByCode[contract.DefaultOperatorAssignment.DefaultOperatorCode]
	if !ok || defaultID == "" {
		return fmt.Errorf("operator assignment config: default_operator_code %q does not resolve to a seeded shift operator",
			contract.DefaultOperatorAssignment.DefaultOperatorCode)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO vaccination_operator_assignment_config
  (tenant_id, park_id, active_operators_per_day, default_operator_id, row_version, updated_at)
VALUES ($1::uuid, $2::uuid, $3, $4::uuid, 1, now())
ON CONFLICT (tenant_id, park_id) DO UPDATE SET
  active_operators_per_day = EXCLUDED.active_operators_per_day,
  default_operator_id = EXCLUDED.default_operator_id,
  row_version = vaccination_operator_assignment_config.row_version + 1,
  updated_at = now();`,
		tenantID, parkID, contract.DefaultOperatorAssignment.ActiveOperatorsPerDay, defaultID); err != nil {
		return fmt.Errorf("upsert operator assignment config: %w", err)
	}
	return nil
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
	// NOTE (P1-NAV): only departments with a genuine operational module are granted one. A department
	// with no module in defaultDepartmentModules (e.g. procurement, growth, infra, milk, sales)
	// intentionally receives NO grant and its members compose an EMPTY bottom bar -- that is correct,
	// not a bug: nav is earned by holding a module, and inventing a phantom "baseline" module for a
	// department that does no operational work would fabricate access. The blank-nav CRASH (a missing
	// department_module_grants table) is fixed by migration 000008, not by over-granting here.
	return int(tag.RowsAffected()), nil
}

func deriveRoleHint(m *memberRec, grade *string, execVaccOperator bool) string {
	// A vaccination-executing operator is always an "operator" for app/gate purposes,
	// regardless of the HR capacity-tier (manager) used for roster/capacity or any
	// higher-ranked June seat (e.g. park_head) the same person also holds. The mobile
	// scan gate keys on primary_role_hint == "operator"; a field executor labelled
	// supervisor/park_head would be silently locked out of scanning.
	if execVaccOperator {
		return "operator"
	}
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
