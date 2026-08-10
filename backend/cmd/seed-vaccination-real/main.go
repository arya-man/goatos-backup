// Command seed-vaccination-real imports the real captured herd + vaccination
// spreadsheet snapshot into the local/dev GoatOS schema so the /vaccination
// operations read model renders real cohorts x the vaccination matrix with
// honest up-to-date / due / overdue / scheduled statuses.
//
// Source data (gitignored, read at runtime, never committed):
//   - <source>/goats.json       Sheets API {"values":[[hdr...],[row...]]}
//   - <source>/vaccination.json Sheets API, TWO header rows (vaccine, dose)
//
// It is idempotent (deterministic v5 UUIDs + ON CONFLICT), batched for scale,
// and uses Asia/Kolkata for every business date. It seeds ONLY to the existing
// schema: parks/sheds (creates missing sheds park-scoped), animals, one
// vaccination.matrix protocol definition/version with seven vaccine rows,
// per-goat trusted base-anchor history (accepted completions) and future-only
// obligations. Date cells in the source are imported base schedule anchors from
// Vaccination V2 / DemoDB, not live due dates; for the current schema the
// seeder persists anchors on or before the business date as accepted/completed
// history so recurrence can schedule from them, while the kernel suppresses any
// open work that would land on or before the business date.
// It never mutates the protocol/obligation SCHEMA.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	obligationpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/localtarget"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protocolapp "github.com/vgoats/goatos/backend/internal/protocol/app"
	"github.com/vgoats/goatos/backend/internal/seedrun"
	vaccinationpg "github.com/vgoats/goatos/backend/internal/vaccination/adapters/postgres"
	vaccinationapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	vaccinationdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

const defaultTenantID = "00000000-0000-4000-8000-000000000001"
const matrixProtocolCode = "vaccination.matrix"
const matrixVersionLabel = "V1 Real Vaccination"
const seedKidsNormalScheduleUntilWeeks int32 = 16

// vaccineDef is one vaccine column from the source sheet, mapped to the approved
// GoatOS schedule (docs/preventive-care-vaccination/vaccination-rules.md).
type vaccineDef struct {
	Code      string  // protocol_definitions.code suffix (lowercase dotted-safe)
	Name      string  // human protocol name / vaccine column label
	Disease   string  // vaccine disease target
	Type      string  // immunological type: live | killed | toxoid
	Pathogen  string  // pathogen class: bacterial | viral (NEVER a Type value)
	ItemCode  string  // inventory item code
	DoseML    float64 // dose amount (ml) from the source dose table
	VialDoses int     // doses per vial
}

// vaccines maps the source-sheet vaccine header -> its definition. Keys are the
// exact strings that appear in vaccination.json header row 0.
//
// Type and Pathogen are two INDEPENDENT medical axes and must never be conflated:
//   - Type describes the immunological preparation: live | killed | toxoid.
//   - Pathogen describes the organism class: bacterial | viral.
//
// Same-day compatibility and cross-vaccine spacing rules depend on BOTH axes
// (see internal/vaccination/app/compatibility.go classifyVaccine), so writing a
// Type value into the pathogen_class field selects the wrong medical rule.
// The reviewed source of truth for both axes is the V1 preset table in
// docs/preventive-care-vaccination/vaccination-rules.md.
var vaccines = map[string]vaccineDef{
	// ET+TT is "bacteria killed" per the wiki source (Vaccination Rules.docx):
	// vaccine_type=killed, pathogen_class=bacterial. "Booster" is its course type,
	// not its vaccine type. Do not classify ET+TT as toxoid.
	"ET+TT":       {Code: "et_tt", Name: "ET+TT", Disease: "Enterotoxaemia + Tetanus", Type: "killed", Pathogen: "bacterial", ItemCode: "VAC-ET-TT", DoseML: 2, VialDoses: 100},
	"PPR":         {Code: "ppr", Name: "PPR", Disease: "Peste des Petits Ruminants", Type: "live", Pathogen: "viral", ItemCode: "VAC-PPR", DoseML: 1, VialDoses: 100},
	"Blue tongue": {Code: "blue_tongue", Name: "Blue Tongue", Disease: "Blue Tongue", Type: "killed", Pathogen: "viral", ItemCode: "VAC-BT", DoseML: 2, VialDoses: 100},
	"FMD":         {Code: "fmd", Name: "FMD", Disease: "Foot and Mouth Disease", Type: "killed", Pathogen: "viral", ItemCode: "VAC-FMD", DoseML: 1, VialDoses: 30},
	"HS":          {Code: "hs", Name: "HS", Disease: "Haemorrhagic Septicaemia", Type: "killed", Pathogen: "bacterial", ItemCode: "VAC-HS", DoseML: 2, VialDoses: 100},
	"Goat Pox":    {Code: "goat_pox", Name: "Goat Pox", Disease: "Goat Pox", Type: "live", Pathogen: "viral", ItemCode: "VAC-GP", DoseML: 1, VialDoses: 25},
	"Sheep Pox":   {Code: "sheep_pox", Name: "Sheep Pox", Disease: "Sheep Pox", Type: "live", Pathogen: "viral", ItemCode: "VAC-SP", DoseML: 1, VialDoses: 100},
}

// vaccineOrder gives a stable insert order for the 7 protocols.
var vaccineOrder = []string{"ET+TT", "PPR", "Blue tongue", "FMD", "HS", "Goat Pox", "Sheep Pox"}

// vaccineDrivePriority is the source-backed ordering written into the published
// matrix rule metadata. The obligation planner still has a coarse fallback for
// legacy rules, but the real seed must disambiguate FMD vs HS so shot-cap
// arbitration can overflow deterministically instead of failing on equal priority.
var vaccineDrivePriority = map[string]int{
	"ET+TT":       1,
	"PPR":         2,
	"Goat Pox":    3,
	"Sheep Pox":   3,
	"Blue tongue": 4,
	"FMD":         5,
	"HS":          6,
}

type goatRecord struct {
	RFID           string
	RFID2          string
	OldID          string
	OldIDSuffix    string
	Farm           string
	Shed           string
	ShedTag        string // clinical/reproductive/location signal (Fix Plan A3/A4): ICU, ICU-Kid, Quarantine kids, Pregnant, Non-Pregnant, etc.
	Stage          string
	Age            string
	Breed          string
	Gender         string
	DOB            string
	StageEntryDate string
	PurchaseDate   string
	OriginType     string
	Species        string
	Status         string
	Health         string // source health_status case-log column (Fix Plan A2): Open/Closed/Extended, or a healthy/sick/... value
}

// vaccCell is one goat x vaccine x dose spreadsheet cell.
type vaccCell struct {
	AnimalKey string
	Vaccine   string // source vaccine header
	DoseType  string // "First Dose" | "Booster"
	DoseCode  string // "first" | "booster"
	Sequence  int
	Value     string // date | "Pending" | "NA" | ""
}

type stats struct {
	ParksResolved      int
	ShedsResolved      int
	ShedsCreated       int
	Protocols          int
	Animals            int
	Obligations        int
	Completed          int         // completed history obligations from last-administered cells
	CompletionsHistory int         // vaccination_completions rows (accepted doses)
	PendingSource      int         // "Pending" source signals delegated to kernel generation
	Scheduled          int         // future -> scheduled obligations
	Skipped            int         // NA / blank cells, or a dated cell that failed date parsing
	KernelGenerated    int         // kernel-generated open obligations
	KernelDeferred     int         // kernel-deferred obligations
	KernelSuppressed   int         // kernel-suppressed (already completed) obligations
	Purged             purgeCounts // synthetic fixtures removed
	RetiredSourceDrift int         // active non-source park/shed locations retired during source-owned local/dev reseed
	DobNulled          int         // purchased/imported-origin animals with provably-false DOB, nulled via -null-false-dob
	StagesCorrected    int         // goats whose source stage contradicted age-derived stage, auto-corrected

	// Fix Plan A1 — explicit, non-silent accounting for every dated (non-blank/NA/Pending)
	// source cell. Every dated fact must land in exactly one bucket: Completed, Scheduled,
	// or one of the four below. reconcileDatedFacts asserts the totals add up so nothing is
	// ever dropped without an explicit, reported reason.
	LaterAdministrationsReconciled int // sheet "Booster" cell for a single+repeat vaccine (FMD, HS) reconciled onto the vaccine's one wave as a later recorded administration, NOT a fabricated booster rule. Counted within Completed/Scheduled.
	UnresolvedDatedFacts           int // dated cell whose vaccine/dose could not resolve to any configured rule (expected zero for the current matrix)
	LifecycleExcludedDatedFacts    int // dated cell for a future dose on a goat excluded from new open work by lifecycle (dead/sold/lost/...)
	GoatNotPlacedDatedFacts        int // dated cell for an animal key with no shed placement / not in the herd sheet
	VaccineUnrecognizedDatedFacts  int // dated cell under a vaccine header not present in the seeded matrix
}

const (
	seedFallbackFarm = "SEED_INTAKE"
	seedFallbackShed = "Seed Intake Shed"

	// vaccinationMatrixProofPolicy is the same proof grain enforced by the bound
	// vaccination SOP and Android runner. Current SOP mode is shed-level video:
	// one mandatory shed proof video, up to five total, camera or gallery.
	// Per-goat proof remains supported when the SOP publishes proof_mode=per_goat_video.
	vaccinationMatrixProofPolicy = `{"types":["video"],"required":true,"proof_mode":"shed_level_video","subject_scope":"shed","expected_subjects":["shed"],"minimum_count":1,"maximum_count":5,"maximum_count_per_subject":5,"capture_source":"in_app_camera","allowed_capture_sources":["in_app_camera","gallery_picker"],"verify_capability":"proof.verify","verify_before_apply":true,"retention_policy":"operational_90d"}`
)

type oblIns struct {
	obligationID, versionID, ruleID, goatID string
	scopeType, scopeID                      string
	dueAt                                   time.Time
	windowStart                             *time.Time
	status                                  string
	completedAt                             *time.Time
	sequence                                int
	idem                                    string
}

type cmpIns struct {
	completionID, obligationID, goatID string
	doseML                             float64
	administeredAt                     time.Time
	verifiedAt                         *time.Time
	verifiedBy                         string
	idem                               string
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("seed-vaccination-real", flag.ContinueOnError)
	tenantID := fs.String("tenant-id", getenv("GOATOS_TENANT_ID", defaultTenantID), "tenant id")
	timeout := fs.Duration("timeout", 300*time.Second, "seed timeout")
	sourcePath := fs.String("source", "/Users/ravi/mesha/source-material/vgoats-seed", "path to source data directory")
	purgeFixtures := fs.Bool("purge-fixtures", true, "purge leftover synthetic dev fixtures (trigger-seed protocol family, synthetic G-0000NN goats, junk-named sheds, stale calendar projections) so every surface shows only real herd data")
	allowPartialGeneration := fs.Bool("allow-partial-generation", false, "allow seed to exit successfully when kernel generation isolates per-goat failures")
	allowOwnerlessSeed := fs.Bool("allow-ownerless-seed", false, "dangerous/dev-only: allow vaccination seed when HRMS roster/position prerequisites are absent")
	nullFalseDob := fs.Bool("null-false-dob", false, "when a purchased/imported-origin source animal has a provably-false DOB (DOB after its own entry_date), null the DOB in-memory before insert instead of hard-failing the seed; every nulled row is counted and recorded in a source-dir audit sidecar. Birth-origin animals are never nulled by this flag — a birth-origin DOB-after-entry failure is always a hard error")
	expectNullFalseDob := fs.Int("expect-null-false-dob", -1, "when >= 0, fail the run if the actual -null-false-dob count differs from this expected count (count gate)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		return fmt.Errorf("load Asia/Kolkata: %w", err)
	}

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

	if err := requireOwnerPrerequisites(ctx, pool, *tenantID, *allowOwnerlessSeed); err != nil {
		return err
	}
	goats, err := loadGoats(*sourcePath)
	if err != nil {
		return fmt.Errorf("load goats: %w", err)
	}
	cells, err := loadVaccinationCells(*sourcePath)
	if err != nil {
		return fmt.Errorf("load vaccination cells: %w", err)
	}

	// R50-003: the -expect-null-false-dob count gate is validated INSIDE seed(),
	// before the seed transaction commits, so a mismatch leaves the database
	// untouched (deferred Rollback fires). This post-call check is a redundant
	// defensive backstop only.
	seedResult, err := seed(ctx, pool, pgCfg, *tenantID, loc, goats, cells, *purgeFixtures, *allowPartialGeneration, *sourcePath, *nullFalseDob, *expectNullFalseDob)
	if err != nil {
		return fmt.Errorf("seed: %w", err)
	}
	fmt.Printf("dob_nulled=%d\n", seedResult.DobNulled)
	if *expectNullFalseDob >= 0 && seedResult.DobNulled != *expectNullFalseDob {
		return fmt.Errorf("dob_nulled count gate failed: expected %d, got %d", *expectNullFalseDob, seedResult.DobNulled)
	}
	if err := analyzePostSeedTables(ctx, pool); err != nil {
		return fmt.Errorf("analyze post-seed tables: %w", err)
	}
	fmt.Println("post_seed_analyze completed")

	// The process-integrity screen serves canonical indexed SQL (U7); no post-seed
	// projection recompute. Surviving summaries (eligibility rollup, counts) are
	// recomputed by seed-closeout.
	return nil
}

type ownerPrerequisiteCounts struct {
	ActiveMembers           int
	ActiveAssignedPositions int
}

func requireOwnerPrerequisites(ctx context.Context, pool *pgxpool.Pool, tenantID string, allowOwnerlessSeed bool) error {
	if allowOwnerlessSeed {
		return nil
	}

	var counts ownerPrerequisiteCounts
	if err := pool.QueryRow(ctx, `
SELECT
  (SELECT count(*) FROM workforce_members WHERE tenant_id=$1::uuid AND status='active') AS active_members,
  (SELECT count(*) FROM workforce_positions WHERE tenant_id=$1::uuid AND status='active' AND workforce_member_id IS NOT NULL) AS active_assigned_positions`,
		tenantID).Scan(&counts.ActiveMembers, &counts.ActiveAssignedPositions); err != nil {
		return fmt.Errorf("load owner prerequisites: %w", err)
	}
	return validateOwnerPrerequisites(counts)
}

func validateOwnerPrerequisites(counts ownerPrerequisiteCounts) error {
	missing := make([]string, 0, 2)
	if counts.ActiveMembers == 0 {
		missing = append(missing, "active workforce_members")
	}
	if counts.ActiveAssignedPositions == 0 {
		missing = append(missing, "active assigned workforce_positions")
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("owner preflight failed: missing %s; run `make seed-vaccination-source-full` or run `go run ./cmd/seed-roster-real` before seed-vaccination-real. Refusing to create owner-missing vaccination work", strings.Join(missing, " and "))
}

type seedAnimalStage struct {
	Code      string
	Name      string
	MinAgeDay *int32
	MaxAgeDay *int32
	SortOrder int
	// AgeBand is the kid/adult classification this cohort carries ('kid', 'adult', or "" for the
	// clinical stages, which are deliberately unclassified). It is what a shifting copies onto the
	// animals it moves, so the herd stops counting a goat as a kid the moment it joins an adult
	// cohort. See migration 000109 for the source data behind each assignment -- in particular that
	// F2* is a KID cohort no matter how old the animal gets, which is why this is not derived from
	// MinAgeDay/MaxAgeDay.
	AgeBand string
}

func seedAnimalStageLookup(ctx context.Context, tx pgx.Tx, tenantID string) error {
	stages := []seedAnimalStage{
		{Code: "K0", Name: "Newborn", MinAgeDay: int32Ptr(0), MaxAgeDay: int32Ptr(1), SortOrder: 0, AgeBand: "kid"},
		{Code: "K1", Name: "Milk training", MinAgeDay: int32Ptr(2), MaxAgeDay: int32Ptr(7), SortOrder: 10, AgeBand: "kid"},
		{Code: "K2", Name: "Milk drinking", MinAgeDay: int32Ptr(8), MaxAgeDay: int32Ptr(42), SortOrder: 20, AgeBand: "kid"},
		{Code: "K3", Name: "Weaned kids", MinAgeDay: int32Ptr(43), SortOrder: 30, AgeBand: "kid"},
		{Code: "F2", Name: "Fattening", SortOrder: 40, AgeBand: "kid"},
		{Code: "F2-Male", Name: "Fattening male", SortOrder: 41, AgeBand: "kid"},
		{Code: "F2-Female", Name: "Fattening female", SortOrder: 42, AgeBand: "kid"},
		{Code: "Buck", Name: "Buck", SortOrder: 50, AgeBand: "adult"},
		{Code: "Mother", Name: "Mother", SortOrder: 60, AgeBand: "adult"},
		{Code: "Milking", Name: "Milking", SortOrder: 70, AgeBand: "adult"},
		{Code: "M0", Name: "Mother newborn", SortOrder: 80, AgeBand: "adult"},
		// Warmup is KID by maintainer decision 2026-08-05, deliberately against the source sheet,
		// which labels its one live Warmup animal Adult. See migration 000109 for the override.
		{Code: "Warmup", Name: "Warmup", SortOrder: 90, AgeBand: "kid"},
		{Code: "Pregnant", Name: "Pregnant", SortOrder: 100, AgeBand: "adult"},
		{Code: "Non-Pregnant", Name: "Non-pregnant", SortOrder: 110, AgeBand: "adult"},
		// ICU and Quarantine stay unclassified on purpose: a clinical placement must never
		// reclassify an animal as a kid or an adult.
		{Code: "ICU", Name: "ICU", SortOrder: 120},
		{Code: "Quarantine", Name: "Quarantine", SortOrder: 130},
	}
	for _, stage := range stages {
		if _, err := tx.Exec(ctx, `
INSERT INTO animal_stage_lookup (
  animal_stage_id, tenant_id, stage_code, name, min_age_days, max_age_days,
  sort_order, status, age_band
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5, $6, $7, 'active', NULLIF($8::text, '')
)
ON CONFLICT (tenant_id, stage_code) DO UPDATE
SET name = EXCLUDED.name,
    min_age_days = EXCLUDED.min_age_days,
    max_age_days = EXCLUDED.max_age_days,
    sort_order = EXCLUDED.sort_order,
    status = 'active',
    age_band = EXCLUDED.age_band,
    updated_at = now()`,
			detUUID("animal_stage_lookup", tenantID, stage.Code),
			tenantID,
			stage.Code,
			stage.Name,
			stage.MinAgeDay,
			stage.MaxAgeDay,
			stage.SortOrder,
			stage.AgeBand,
		); err != nil {
			return fmt.Errorf("seed animal stage %s: %w", stage.Code, err)
		}
	}
	return nil
}

func int32Ptr(v int32) *int32 {
	return &v
}

func analyzePostSeedTables(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
ANALYZE
  locations,
  location_operational_attributes,
  workforce_members,
  workforce_positions,
  goats,
  goat_identifiers,
  protocol_definitions,
  protocol_versions,
  protocol_rules,
  obligation_instances,
  obligation_status_events,
  vaccination_completions`)
	return err
}

// ---- source loaders ----

func loadGoats(sourcePath string) ([]goatRecord, error) {
	values, err := readSheet(sourcePath + "/goats.json")
	if err != nil {
		return nil, err
	}
	if len(values) < 2 {
		return nil, fmt.Errorf("goats.json: too few rows")
	}
	col := headerIndex(values[0])
	out := make([]goatRecord, 0, len(values)-1)
	for _, row := range values[1:] {
		if len(row) == 0 {
			continue
		}
		rec := goatRecord{
			RFID:           cell(row, col["rfid"]),
			RFID2:          optionalCell(row, col, "rfid2"),
			OldID:          cell(row, col["old_id"]),
			OldIDSuffix:    cell(row, col["old_id_suffix"]),
			Farm:           cell(row, col["farm"]),
			Shed:           cell(row, col["shed"]),
			ShedTag:        cell(row, col["shed_tag"]),
			Stage:          cell(row, col["stage"]),
			Age:            cell(row, col["age"]),
			Breed:          cell(row, col["breed"]),
			Gender:         cell(row, col["gender"]),
			DOB:            cell(row, col["dob"]),
			StageEntryDate: cell(row, col["stage_entry_date"]),
			PurchaseDate:   cell(row, col["purchase_date"]),
			OriginType:     cell(row, col["origin_type"]),
			Species:        optionalCell(row, col, "species", "Species"),
			Status:         cell(row, col["status"]),
			Health:         cell(row, col["health_status"]),
		}
		if rec.Farm != "" && sourceAnimalIdentifier(rec.RFID, rec.OldID, rec.OldIDSuffix) != "" {
			out = append(out, rec)
		}
	}
	return out, nil
}

// loadVaccinationCells parses the two-header-row vaccination sheet. Row 0 carries
// the vaccine name only in the FIRST column of each vaccine block; booster columns
// have a blank name, so the vaccine name is forward-filled across the block.
func loadVaccinationCells(sourcePath string) ([]vaccCell, error) {
	values, err := readSheet(sourcePath + "/vaccination.json")
	if err != nil {
		return nil, err
	}
	if len(values) < 3 {
		return nil, fmt.Errorf("vaccination.json: too few rows")
	}
	vaccRow, doseRow := values[0], values[1]
	col := headerIndex(vaccRow)

	// Build column -> (vaccine, doseType) with forward-fill of the vaccine name.
	type colDef struct {
		vaccine  string
		doseType string
	}
	colDefs := map[int]colDef{}
	lastVaccine := ""
	for i := 11; i < len(vaccRow); i++ {
		name := stringAt(vaccRow, i)
		if name != "" {
			lastVaccine = name
		}
		dose := stringAt(doseRow, i)
		if lastVaccine == "" || dose == "" {
			continue
		}
		if _, ok := vaccines[lastVaccine]; !ok {
			continue
		}
		colDefs[i] = colDef{vaccine: lastVaccine, doseType: dose}
	}

	rfidCol := col["RFID"]
	oldIDCol := col["Old ID"]
	oldIDSuffixCol := col["Old ID Suffix"]
	var out []vaccCell
	for _, row := range values[2:] {
		if len(row) == 0 {
			continue
		}
		rfid := cell(row, rfidCol)
		animalKey := sourceAnimalIdentifier(rfid, cell(row, oldIDCol), cell(row, oldIDSuffixCol))
		if animalKey == "" {
			continue
		}
		for i, cd := range colDefs {
			doseCodeVal := "first"
			sequenceVal := 1
			if strings.EqualFold(strings.TrimSpace(cd.doseType), "Booster") {
				doseCodeVal = "booster"
				sequenceVal = 2
			}
			out = append(out, vaccCell{
				AnimalKey: animalKey,
				Vaccine:   cd.vaccine,
				DoseType:  cd.doseType,
				DoseCode:  doseCodeVal,
				Sequence:  sequenceVal,
				Value:     cell(row, i),
			})
		}
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
			idx[s] = i
		}
	}
	return idx
}

// ---- seeding ----

// shedKey identifies one physical shed by (farm, shed name). The same shed name
// can exist under BOTH parks as distinct physical sheds, so resolution is keyed
// on the pair.
type shedKey struct{ farm, shed string }

// dobDisposition is one audit entry for a source animal whose provably-false DOB
// (purchased/imported-origin, DOB after its own entry_date — a bulk-fill data error,
// never a genuine birth-origin defect) was nulled in-memory before insert under
// -null-false-dob. Source files are never modified; this is a sidecar record only.
type dobDisposition struct {
	RFID         string `json:"rfid"`
	OriginType   string `json:"origin_type"`
	OriginalDOB  string `json:"original_dob"`
	EntryDate    string `json:"entry_date"`
	EntrySource  string `json:"entry_source"`
	DeltaDays    int    `json:"delta_days"`
	Disposition  string `json:"disposition"`
	RunTimestamp string `json:"run_timestamp"`
}

// stageCorrection tracks one goat whose source stage tag contradicted its age-derived stage.
// The age-derived stage is authoritative; contradictions must be corrected + surfaced for review.
type stageCorrection struct {
	RFID           string `json:"rfid"`
	AgeWeeks       int    `json:"age_weeks"`
	SourceStage    string `json:"source_stage"`
	CorrectedStage string `json:"corrected_stage"`
	RunTimestamp   string `json:"run_timestamp"`
}

// entryDateSourceLabel reports which source column resolved the animal's entry_date,
// mirroring the precedence in buildEntryDateMapping (purchase_date -> stage_entry_date,
// except birth-origin animals which use only their own stage_entry_date).
func entryDateSourceLabel(g goatRecord) string {
	if normalizeOriginType(g.OriginType) == "birth" {
		if parseSourceDate(g.StageEntryDate) != nil {
			return "stage_entry_date"
		}
		return ""
	}
	if parseSourceDate(g.PurchaseDate) != nil {
		return "purchase_date"
	}
	if parseSourceDate(g.StageEntryDate) != nil {
		return "stage_entry_date"
	}
	return ""
}

// resolveGoatDOBViolation checks a source animal's DOB against its resolved entry_date.
// It returns nil, nil when there is no violation (dobTime is nil or on/before entryDate).
// On a violation it either returns a non-nil dobDisposition (purchased/imported-origin,
// nullFalseDob set — caller must null the in-memory DOB and count it) or a hard error
// (no flag, or birth-origin — a birth-origin DOB-after-entry violation is always a
// genuine data defect and is never eligible for -null-false-dob).
func resolveGoatDOBViolation(g goatRecord, animalKey string, dobTime *time.Time, entryDate *time.Time, nullFalseDob bool, now time.Time) (*dobDisposition, error) {
	if dobTime == nil || entryDate == nil || !dobTime.After(*entryDate) {
		return nil, nil
	}
	originType := normalizeOriginType(g.OriginType)
	// R50-004: nulling eligibility is an ALLOWLIST of exactly "procured" or
	// "imported" origin — never a denylist of "not birth". Birth-origin,
	// blank, and any unrecognized origin_type value must hard-fail so
	// unknown-origin source rows can never silently lose their DOB.
	if !nullFalseDob || (originType != "procured" && originType != "imported") {
		return nil, fmt.Errorf("source animal %q has DOB %s after entry_date %s", animalKey, dobTime.Format("2006-01-02"), entryDate.Format("2006-01-02"))
	}
	return &dobDisposition{
		RFID:         animalKey,
		OriginType:   originType,
		OriginalDOB:  dobTime.Format("2006-01-02"),
		EntryDate:    entryDate.Format("2006-01-02"),
		EntrySource:  entryDateSourceLabel(g),
		DeltaDays:    int(dobTime.Sub(*entryDate).Hours() / 24),
		Disposition:  "dob_nulled_false",
		RunTimestamp: now.Format(time.RFC3339),
	}, nil
}

// writeDobDispositionAudit writes the false-DOB null-out sidecar to
// <sourcePath>/seed-dob-disposition-<YYYY-MM-DDTHH-MM-SS>-<seedRunID>.json.
// R50-005: the filename embeds both a seconds-precision timestamp and the
// seed run id so two seeds started in the same second (or a same-day rerun)
// never silently overwrite a prior run's audit sidecar. Source files
// (goats.json, vaccination.json) are never touched — this is an additive
// audit record only, and it is a no-op when there is nothing to record.
func writeDobDispositionAudit(sourcePath string, runDate time.Time, seedRunID string, entries []dobDisposition) error {
	if len(entries) == 0 {
		return nil
	}
	path := filepath.Join(sourcePath, fmt.Sprintf("seed-dob-disposition-%s-%s.json", runDate.Format("2006-01-02T15-04-05"), seedRunID))
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal dob disposition audit: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write dob disposition audit %s: %w", path, err)
	}
	return nil
}

// writeStageCorrectionAudit writes the stage correction sidecar to
// <sourcePath>/seed-stage-correction-<YYYY-MM-DDTHH-MM-SS>-<seedRunID>.json.
// Records every goat whose source stage tag contradicted its age-derived stage.
// Source files are never modified; this is an additive audit record only, and it is a no-op
// when there is nothing to record.
func writeStageCorrectionAudit(sourcePath string, runDate time.Time, seedRunID string, entries []stageCorrection) error {
	if len(entries) == 0 {
		return nil
	}
	path := filepath.Join(sourcePath, fmt.Sprintf("seed-stage-correction-%s-%s.json", runDate.Format("2006-01-02T15-04-05"), seedRunID))
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal stage correction audit: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write stage correction audit %s: %w", path, err)
	}
	return nil
}

type seedGoatUpsertRow struct {
	goatID, animalKey, animalIdentifier1, species, breed, breedID, sex, lifecycle, originType, stage, age, shedID, parkID, dob string
	animalIdentifierAliases                                                                                                    []string
	entryDate, sourceShedName, partitionLabel                                                                                  string
	health                                                                                                                     *string
	reproductiveStatus                                                                                                         *string
}

// checkNoCrossParkMoves enforces the goats-never-change-park invariant (maintainer
// decision 2026-07-19; runtime equivalent: identity/ports.ErrCrossParkMove) for a
// whole seed/reseed batch with ONE set-based query -- never a per-goat lookup, so
// this scales the same way the DOB-null count gate does. A goat with an existing
// non-null park_id whose incoming row targets a different park is collected as an
// offender; if any are found, the whole seed transaction is rejected before the
// upsert batch runs (fail before the batch runs, same discipline as the
// -expect-null-false-dob count gate). Exempt: the goat has no existing row (new
// placement) or its existing park_id IS NULL (terminal exit / not yet placed) --
// either case is an initial placement, not a move.
func checkNoCrossParkMoves(ctx context.Context, tx pgx.Tx, tenantID string, rows []seedGoatUpsertRow) error {
	if len(rows) == 0 {
		return nil
	}
	incomingParkByGoatID := make(map[string]string, len(rows))
	goatIDs := make([]string, 0, len(rows))
	for _, r := range rows {
		incomingParkByGoatID[r.goatID] = r.parkID
		goatIDs = append(goatIDs, r.goatID)
	}
	rs, err := tx.Query(ctx, `
SELECT goat_id::text, park_id::text
FROM goats
WHERE tenant_id = $1 AND goat_id = ANY($2::uuid[]) AND park_id IS NOT NULL`, tenantID, goatIDs)
	if err != nil {
		return fmt.Errorf("cross-park move pre-check: %w", err)
	}
	defer rs.Close()

	var offending []string
	for rs.Next() {
		var goatID, existingParkID string
		if err := rs.Scan(&goatID, &existingParkID); err != nil {
			return fmt.Errorf("cross-park move pre-check scan: %w", err)
		}
		if incomingParkID, ok := incomingParkByGoatID[goatID]; ok && incomingParkID != existingParkID {
			offending = append(offending, fmt.Sprintf("%s(%s->%s)", goatID, existingParkID, incomingParkID))
		}
	}
	if err := rs.Err(); err != nil {
		return fmt.Errorf("cross-park move pre-check rows: %w", err)
	}
	if len(offending) > 0 {
		sort.Strings(offending)
		return fmt.Errorf("seed rejected: %d goat(s) would cross-park move (goats never change park, see identity/ports.ErrCrossParkMove): %s",
			len(offending), strings.Join(offending, ", "))
	}
	return nil
}

func upsertSeedGoats(ctx context.Context, tx pgx.Tx, tenantID string, rows []seedGoatUpsertRow, custodianPartyID string) error {
	if err := checkNoCrossParkMoves(ctx, tx, tenantID, rows); err != nil {
		return err
	}
	if err := batch(ctx, tx, rows, 500, func(b *pgx.Batch, gi seedGoatUpsertRow) {
		b.Queue(`
				INSERT INTO goats (goat_id, tenant_id, species, breed, breed_id, sex, lifecycle_status,
					health_status, origin_type, dob, entry_date, current_location_id, shed_id, park_id, management_stage, age_band, custodian_party_id, reproductive_status, updated_at)
				VALUES (
					$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,
					COALESCE((
						SELECT sp.operational_location_id
						FROM shed_partitions sp
						WHERE sp.tenant_id=$2
						  AND sp.shed_id=$12
						  AND sp.status='active'
						  AND regexp_replace(lower(btrim(sp.partition_label)), '^part[[:space:]]+', '') =
						      regexp_replace(lower(btrim($18)), '^part[[:space:]]+', '')
						LIMIT 1
					), $12),
					$12,$13,$14,$15,$16,$17,now()
				)
				ON CONFLICT (goat_id) DO UPDATE SET species=EXCLUDED.species, breed=EXCLUDED.breed, breed_id=EXCLUDED.breed_id, sex=EXCLUDED.sex,
					lifecycle_status=EXCLUDED.lifecycle_status, health_status=EXCLUDED.health_status,
					origin_type=EXCLUDED.origin_type, dob=EXCLUDED.dob,
					shed_id=EXCLUDED.shed_id, park_id=EXCLUDED.park_id, current_location_id=EXCLUDED.current_location_id,
					management_stage=EXCLUDED.management_stage, age_band=EXCLUDED.age_band, entry_date=EXCLUDED.entry_date,
					custodian_party_id=EXCLUDED.custodian_party_id, reproductive_status=EXCLUDED.reproductive_status, updated_at=now()`,
			gi.goatID, tenantID, gi.species, gi.breed, nullString(gi.breedID), gi.sex, gi.lifecycle, gi.health, nullString(gi.originType),
			nullableDate(gi.dob), nullableDate(gi.entryDate), gi.shedID, gi.parkID, gi.stage, nullString(gi.age), custodianPartyID, gi.reproductiveStatus, gi.partitionLabel)
	}); err != nil {
		return err
	}
	return batch(ctx, tx, rows, 500, func(b *pgx.Batch, gi seedGoatUpsertRow) {
		b.Queue(`
				INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name, updated_at)
				VALUES ($1,$2,$3,$4,$5,now())
				ON CONFLICT (tenant_id, goat_id) DO UPDATE SET
					shed_id=EXCLUDED.shed_id,
					partition_label=EXCLUDED.partition_label,
					source_shed_name=EXCLUDED.source_shed_name,
					updated_at=now()`,
			tenantID, gi.goatID, gi.shedID, gi.partitionLabel, gi.sourceShedName)
	})
}

func upsertSeedGoatIdentifiers(ctx context.Context, tx pgx.Tx, tenantID string, rows []seedGoatUpsertRow) error {
	for _, gi := range rows {
		if gi.animalIdentifier1 == "" {
			continue
		}
		normalizedPrimary := strings.ToLower(gi.animalIdentifier1)
		var existingPrimary string
		if err := tx.QueryRow(ctx, `
			SELECT normalized_value
			FROM goat_identifiers
			WHERE tenant_id = $1
			  AND goat_id = $2
			  AND identifier_type = 'animal_identifier_1'
			  AND is_primary_for_goat
			  AND status = 'active'
			ORDER BY valid_from DESC, created_at DESC
			LIMIT 1`,
			tenantID, gi.goatID).Scan(&existingPrimary); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("read existing primary identifier for goat %s: %w", gi.goatID, err)
		}
		identifierType := "animal_identifier_1"
		isPrimary := true
		if existingPrimary != "" && existingPrimary != normalizedPrimary {
			identifierType = "animal_identifier_2"
			isPrimary = false
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO goat_identifiers (identifier_type, tenant_id, goat_id, identifier_value, normalized_value,
				scope_key, valid_from, normalizer_version, status, is_primary_for_goat)
			VALUES ($1,$2,$3,$4,$5,'global',now()::date,'identifier_normalizer_v1','active',$6)
			ON CONFLICT (tenant_id, normalized_value) DO NOTHING`,
			identifierType, tenantID, gi.goatID, gi.animalIdentifier1, normalizedPrimary, isPrimary); err != nil {
			return fmt.Errorf("insert identifier %s: %w", gi.animalIdentifier1, err)
		}
		for _, alias := range gi.animalIdentifierAliases {
			alias = strings.TrimSpace(alias)
			if alias == "" || strings.EqualFold(alias, gi.animalIdentifier1) {
				continue
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO goat_identifiers (identifier_type, tenant_id, goat_id, identifier_value, normalized_value,
					scope_key, valid_from, normalizer_version, status, is_primary_for_goat)
				VALUES ('animal_identifier_2',$1,$2,$3,$4,'global',now()::date,'identifier_normalizer_v1','active',false)
				ON CONFLICT (tenant_id, normalized_value) DO NOTHING`,
				tenantID, gi.goatID, alias, strings.ToLower(alias)); err != nil {
				return fmt.Errorf("insert identifier %s: %w", alias, err)
			}
		}
	}
	return nil
}

func seed(ctx context.Context, pool *pgxpool.Pool, pgCfg platformpg.Config, tenantID string, loc *time.Location, goats []goatRecord, cells []vaccCell, purgeFixtures bool, allowPartialGeneration bool, sourcePath string, nullFalseDob bool, expectNullFalseDob int) (st stats, retErr error) {
	now := time.Now().In(loc)
	var dobDispositions []dobDisposition
	var stageCorrections []stageCorrection

	// VACC-REV-02: open a persisted seed-run in the `loading` state on the pool (outside the data
	// tx). Any error return below transitions it to `failed`/reset_required via the defer, so a
	// half-seeded or aborted database is never promotable. Only a successful post-generation
	// verification marks it `verified`/ready.
	seedRunID, err := beginSeedRun(ctx, pool, tenantID)
	if err != nil {
		return st, err
	}
	defer func() {
		if retErr != nil {
			failSeedRun(ctx, pool, seedRunID, retErr)
		}
	}()

	// Default custodian party = the Mesha org (goats.custodian_party_id is NOT NULL).
	var custodianPartyID string
	if err := pool.QueryRow(ctx, `SELECT party_id FROM parties WHERE party_type = 'org' AND display_name = 'Mesha' LIMIT 1`).Scan(&custodianPartyID); err != nil {
		return st, fmt.Errorf("resolve custodian party (Mesha org): %w", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return st, fmt.Errorf("begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	// 0. Purge ALL leftover synthetic dev fixtures (data cleanup, not schema/rule change) so every
	//    surface (/calendar, /counts/herd, /vaccination) renders only real herd data. Targets the
	//    trigger-seed protocol family, synthetic G-0000NN goats, junk-named sheds, and stale
	//    calendar projections — never real goats/protocols/sheds/workforce/grants.
	if purgeFixtures {
		pc, err := purgeSyntheticFixtures(ctx, tx, tenantID)
		if err != nil {
			return st, fmt.Errorf("purge synthetic fixtures: %w", err)
		}
		st.Purged = pc
	}

	// 1. Parks: resolve/create every park needed by accepted source animals. While the
	// product validators are still being built, missing placement fields are completed
	// deterministically by seed; vaccination dates/history are never invented.
	parkByFarm := map[string]string{}
	for _, farm := range distinct(goats, func(g goatRecord) string { return seedFarm(g) }) {
		var parkID string
		code := seedLocationCode(farm)
		err := tx.QueryRow(ctx, `SELECT location_id FROM locations WHERE tenant_id=$1 AND location_type='park' AND location_code=$2 LIMIT 1`, tenantID, code).Scan(&parkID)
		if err == pgx.ErrNoRows {
			parkID = detUUID("park", tenantID, farm)
			if _, err := tx.Exec(ctx, `
			INSERT INTO locations (location_id, tenant_id, location_type, location_code, name,
				country, state_region, timezone, status, updated_at)
			VALUES ($1,$2,'park',$3,$4,'IN','Tamil Nadu','Asia/Kolkata','active',now())
			ON CONFLICT (tenant_id, location_code) DO UPDATE SET name=EXCLUDED.name, status='active', updated_at=now()`,
				parkID, tenantID, code, farm); err != nil {
				return st, fmt.Errorf("create park %q: %w", farm, err)
			}
		} else if err != nil {
			return st, fmt.Errorf("resolve park for farm %q: %w", farm, err)
		}
		parkByFarm[farm] = parkID
		st.ParksResolved++
	}
	sourceParkCodes := make([]string, 0, len(parkByFarm))
	for farm := range parkByFarm {
		sourceParkCodes = append(sourceParkCodes, seedLocationCode(farm))
	}
	// 2. Sheds: resolve existing park-scoped shed by (name, park); create missing ones
	//    so EVERY goat maps to its real shed.
	shedByKey := map[shedKey]string{}
	sourceShedKeys := distinctShedKeys(goats)
	for _, k := range sourceShedKeys {
		parkID := parkByFarm[k.farm]
		var shedID string
		err := tx.QueryRow(ctx, `
			SELECT location_id FROM locations
			WHERE tenant_id=$1 AND location_type='shed' AND parent_location_id=$2 AND lower(name)=lower($3)
			LIMIT 1`, tenantID, parkID, k.shed).Scan(&shedID)
		if err == nil {
			shedByKey[k] = shedID
			st.ShedsResolved++
			continue
		}
		if err != pgx.ErrNoRows {
			return st, fmt.Errorf("resolve shed (%s/%s): %w", k.farm, k.shed, err)
		}
		// Create the missing shed park-scoped.
		shedID = detUUID("shed", tenantID, k.farm, k.shed)
		code := shedCode(k.farm, k.shed)
		if _, err := tx.Exec(ctx, `
			INSERT INTO locations (location_id, tenant_id, location_type, location_code, name,
				parent_location_id, country, state_region, timezone, status, updated_at)
			VALUES ($1,$2,'shed',$3,$4,$5,'IN','Tamil Nadu','Asia/Kolkata','active',now())
			ON CONFLICT (tenant_id, location_code) DO UPDATE SET status='active', updated_at=now()`,
			shedID, tenantID, code, k.shed, parkID); err != nil {
			return st, fmt.Errorf("create shed (%s/%s): %w", k.farm, k.shed, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO location_operational_attributes (tenant_id, location_id, usable_for_vaccination, usable_for_sop)
			VALUES ($1,$2,true,true)
			ON CONFLICT (location_id) DO UPDATE SET usable_for_vaccination=true, updated_at=now()`, tenantID, shedID); err != nil {
			return st, fmt.Errorf("create shed attrs (%s/%s): %w", k.farm, k.shed, err)
		}
		shedByKey[k] = shedID
		st.ShedsCreated++
	}
	retired, err := retireActiveNonSourceLocations(ctx, tx, tenantID, sourceParkCodes, sourceShedKeys)
	if err != nil {
		return st, fmt.Errorf("retire non-source locations: %w", err)
	}
	st.RetiredSourceDrift = retired

	// Store entry_date source mapping for later use when seeding goats
	entryDateByAnimalKey := buildEntryDateMapping(goats)

	// 3. Protocol config: one canonical vaccination.matrix protocol version with
	//    seven vaccine rows. The individual vaccines become rules inside the
	//    matrix, not seven top-level protocol definitions.
	versionByVaccine := map[string]string{} // vaccine header -> protocol_version_id
	for _, vaccName := range vaccineOrder {
		def := vaccines[vaccName]
		itemID := detUUID("inventory_item", tenantID, def.Code)
		if _, err := tx.Exec(ctx, `
			INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit, status)
			VALUES ($1,$2,$3,$4,'vaccine','dose','active')
			ON CONFLICT (tenant_id, item_code) DO UPDATE SET name=EXCLUDED.name, status='active', updated_at=now()`,
			itemID, tenantID, def.ItemCode, def.Name+" vaccine"); err != nil {
			return st, fmt.Errorf("inventory item %s: %w", vaccName, err)
		}
	}

	protocolID := detUUID("protocol", tenantID, "vaccination_matrix")
	if _, err := tx.Exec(ctx, `
		INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
		VALUES ($1,$2,$3,'Preventive Care Vaccination Matrix','vaccination','active')
		ON CONFLICT (tenant_id, code) DO UPDATE SET name=EXCLUDED.name, status='active', updated_at=now()`,
		protocolID, tenantID, matrixProtocolCode); err != nil {
		return st, fmt.Errorf("protocol def vaccination matrix: %w", err)
	}
	if err := tx.QueryRow(ctx,
		`SELECT protocol_id FROM protocol_definitions WHERE tenant_id=$1 AND code=$2`,
		tenantID, matrixProtocolCode).Scan(&protocolID); err != nil {
		return st, fmt.Errorf("resolve vaccination matrix protocol id: %w", err)
	}

	// seedActorID is the deterministic, server-only provenance stamp on seed-owned protocol versions
	// (Config authoring stamps drafted_by with the real acting user, never this id). It is what makes
	// a seed-owned matrix distinguishable from a user-authored one under the SAME canonical protocol.
	seedActorID := detUUID("seed-actor", tenantID)

	// Ownership safety (VAX-SEED-01): PublishVersion's overlap-retire clears every overlapping published
	// vaccination matrix at this scope regardless of which protocol OR author created it — including a
	// user-authored version under the same canonical vaccination.matrix protocol. Fail fast (before
	// committing this config transaction) when a non-seed-owned overlapping matrix exists, rather than
	// commit and then have the guarded publish refuse. The authoritative, TOCTOU-safe check is the
	// atomic guard inside the publish transaction below; this is the friendly early exit.
	if foreign, ferr := foreignPublishedVaccinationMatrices(ctx, tx, tenantID, seedActorID); ferr != nil {
		return st, fmt.Errorf("check non-seed-owned vaccination matrices: %w", ferr)
	} else if len(foreign) > 0 {
		return st, fmt.Errorf("seed-vaccination-real: refusing to publish the seed vaccination matrix — %d non-seed-owned published vaccination matrix version(s) overlap at tenant scope (%v); publishing would retire that user-authored configuration. Retire or remove them first, then reseed", len(foreign), foreign)
	}

	ruleDSL, err := vaccinationMatrixRuleDSL()
	if err != nil {
		return st, err
	}
	sopVersionID, err := resolveVaccinationSOPVersion(ctx, tx, tenantID)
	if err != nil {
		return st, err
	}
	versionID, err := reconcileSeedMatrixDraftVersion(ctx, tx, tenantID, protocolID, ruleDSL, sopVersionID, seedActorID)
	if err != nil {
		return st, err
	}

	if err := tx.Commit(ctx); err != nil {
		return st, fmt.Errorf("commit protocol draft: %w", err)
	}
	committed = true

	protocolService := protocolapp.NewService(protocolpg.NewRepository(pool, pgCfg.QueryTimeout))
	if err := protocolService.PublishSeedOwnedVaccinationMatrixVersion(ctx, tenantID, versionID, seedActorID, "seed-vaccination-real:"+versionID); err != nil {
		return st, fmt.Errorf("publish vaccination matrix through protocol service: %w", err)
	}

	tx, err = pool.Begin(ctx)
	if err != nil {
		return st, fmt.Errorf("begin animal seed tx: %w", err)
	}
	committed = false

	// R50-003: lookup writes must be part of the SAME transaction as the imported herd and its
	// count gates below (the DOB-null -expect-null-false-dob gate, ~line 1350). This tx is the
	// one that actually holds the herd/obligation/audit writes and the gate check, so
	// seedAnimalStageLookup runs here -- not in the earlier protocol-draft tx above, which
	// commits independently and would let a later gate failure leave stale lookup rows durably
	// committed while the herd it was meant to travel with rolled back.
	if err := seedAnimalStageLookup(ctx, tx, tenantID); err != nil {
		return st, err
	}

	// Load all protocol rules (birth_age, manual_campaign, revacc for all vaccines)
	// ruleByDoseCode: vaccine_code "_" dose_code → rule_id
	ruleByDoseCode := map[string]string{}
	rows, err := tx.Query(ctx, `
		SELECT dose_code, rule_id
		FROM protocol_rules
		WHERE tenant_id=$1 AND protocol_version_id=$2`,
		tenantID, versionID)
	if err != nil {
		return st, fmt.Errorf("load protocol rules: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var doseCode, ruleID string
		if err := rows.Scan(&doseCode, &ruleID); err != nil {
			return st, fmt.Errorf("scan rule row: %w", err)
		}
		ruleByDoseCode[doseCode] = ruleID
	}
	if err := rows.Err(); err != nil {
		return st, fmt.Errorf("read protocol rules: %w", err)
	}

	for _, vaccName := range vaccineOrder {
		versionByVaccine[vaccName] = versionID
	}
	st.Protocols = 1

	// 4. Animals: import each goat with shed_id + park_id (the read model groups on these).
	breedIDs, err := ensureSeedBreeds(ctx, tx, goats)
	if err != nil {
		return st, err
	}
	goatIDByAnimalKey := map[string]string{}
	goatLifecycleByAnimalKey := map[string]string{}
	goatOriginTypeByAnimalKey := map[string]string{}
	goatDOBByAnimalKey := map[string]*time.Time{}
	goatEntryDateByAnimalKey := map[string]*time.Time{}
	goatStageByAnimalKey := map[string]string{}
	goatShedByAnimalKey := map[string]string{}
	goatParkByAnimalKey := map[string]string{}
	seenAnimalKeys := map[string]int{}
	var goatRows []seedGoatUpsertRow
	for rowIndex, g := range goats {
		placement := seedPlacementKey(g)
		shedID, ok := shedByKey[placement]
		if !ok {
			return st, fmt.Errorf("source animal %q has no deterministic seed shed for placement %s/%s", sourceAnimalIdentifier(g.RFID, g.OldID, g.OldIDSuffix), placement.farm, placement.shed)
		}
		animalKey := sourceAnimalIdentifier(g.RFID, g.OldID, g.OldIDSuffix)
		if animalKey == "" {
			continue
		}
		if firstRow, ok := seenAnimalKeys[animalKey]; ok {
			return st, fmt.Errorf("duplicate source animal identity %q at source rows %d and %d: seed must not merge two goats under one deterministic goat_id", animalKey, firstRow+1, rowIndex+1)
		}
		seenAnimalKeys[animalKey] = rowIndex
		animalIdentifier1, animalIdentifierAliases := identifierSlots(g.RFID, g.RFID2, g.OldID, g.OldIDSuffix)
		goatID := detUUID("goat", tenantID, animalKey)
		parkID := parkByFarm[placement.farm]
		goatIDByAnimalKey[animalKey] = goatID
		goatLifecycleByAnimalKey[animalKey] = normalizeLifecycle(g.Status)
		goatOriginTypeByAnimalKey[animalKey] = normalizeOriginType(g.OriginType)
		goatStageByAnimalKey[animalKey] = normalizeStage(g.Stage, g.Age)
		goatShedByAnimalKey[animalKey] = shedID
		goatParkByAnimalKey[animalKey] = parkID

		// Parse DOB for later use in schedule path resolution
		var dobTime *time.Time
		dobForRow := g.DOB
		if g.DOB != "" {
			d, err := time.ParseInLocation("2006-01-02", g.DOB, loc)
			if err != nil {
				return st, fmt.Errorf("source animal %q has invalid DOB %q: %w", animalKey, g.DOB, err)
			}
			if d.After(now) {
				return st, fmt.Errorf("source animal %q has future DOB %s after seed business date %s", animalKey, d.Format("2006-01-02"), now.Format("2006-01-02"))
			}
			dobTime = &d
			goatDOBByAnimalKey[animalKey] = dobTime

			// AUTO-CORRECT: derive age-based stage and detect contradictions with source stage.
			// The MAINTAINER RULE states: "vaccination STAGE is a pure function of AGE (DOB → age-weeks → kid cutoff).
			// A goat tagged kid (K*) but aged past the kid cutoff is a data contradiction that must never persist.
			// SEED must AUTO-CORRECT the tag to the age-derived stage and HIGHLIGHT it."
			sourceStage := goatStageByAnimalKey[animalKey]
			derivedStage := vaccinationapp.DerivedStageFromDOB(dobTime, now)
			if derivedStage != "" && sourceStage != "" && !stagesMatch(sourceStage, derivedStage) {
				// Stage contradiction: source says sourceStage but age says derivedStage. Correct and track.
				ageWeeks := (now.Sub(*dobTime).Hours() / 24) / 7
				stageCorrections = append(stageCorrections, stageCorrection{
					RFID:           animalKey,
					AgeWeeks:       int(ageWeeks),
					SourceStage:    sourceStage,
					CorrectedStage: derivedStage,
					RunTimestamp:   now.Format(time.RFC3339),
				})
				goatStageByAnimalKey[animalKey] = derivedStage
			}
		}

		// Resolve entry_date from source mapping
		entryDate := entryDateByAnimalKey[animalKey]
		entryDateValue := ""
		if entryDate != nil {
			if entryDate.After(now) {
				return st, fmt.Errorf("source animal %q has future entry_date %s after seed business date %s", animalKey, entryDate.Format("2006-01-02"), now.Format("2006-01-02"))
			}
			disposition, err := resolveGoatDOBViolation(g, animalKey, dobTime, entryDate, nullFalseDob, now)
			if err != nil {
				return st, err
			}
			if disposition != nil {
				dobDispositions = append(dobDispositions, *disposition)
				st.DobNulled++
				dobTime = nil
				dobForRow = ""
				delete(goatDOBByAnimalKey, animalKey)
			}
			goatEntryDateByAnimalKey[animalKey] = entryDate
			entryDateValue = entryDate.Format("2006-01-02")
		}

		species := deriveSeedSpecies(g.Species, g.Breed)
		breed := normalizeBreed(g.Breed)
		_, partitionLabel := normalizeSeedShedPartition(seedShed(g))
		goatRows = append(goatRows, seedGoatUpsertRow{
			goatID:                  goatID,
			animalKey:               animalKey,
			animalIdentifier1:       animalIdentifier1,
			animalIdentifierAliases: animalIdentifierAliases,
			species:                 species,
			breed:                   breed,
			breedID:                 breedIDs[seedBreedKey(species, breed)],
			sex:                     normalizeSex(g.Gender),
			lifecycle:               normalizeLifecycle(g.Status),
			originType:              normalizeOriginType(g.OriginType),
			stage:                   goatStageByAnimalKey[animalKey],
			age:                     g.Age,
			// health precedence: shed_tag reflects where the goat is housed RIGHT NOW
			// (e.g. still in the ICU/quarantine shed), a stronger and more current
			// clinical signal than a closed/extended historical case-log entry, so it
			// wins over the health_status column when both are present (Fix Plan A2/A3).
			health:             resolveGoatHealth(g.Health, g.ShedTag),
			reproductiveStatus: resolveGoatReproductiveStatus(g.ShedTag),
			shedID:             shedID,
			parkID:             parkID,
			dob:                dobForRow,
			entryDate:          entryDateValue,
			sourceShedName:     seedShed(g),
			partitionLabel:     partitionLabel,
		})
	}
	if err := upsertSeedGoats(ctx, tx, tenantID, goatRows, custodianPartyID); err != nil {
		return st, fmt.Errorf("insert goats: %w", err)
	}
	if err := verifyActiveGoatsHaveShedInTx(ctx, tx, tenantID); err != nil {
		return st, err
	}
	if err := verifyActiveGoatsUsePhysicalShedLocationsInTx(ctx, tx, tenantID); err != nil {
		return st, err
	}
	st.Animals = len(goatRows)

	// Identifiers. animal_identifier_1 is the best available real-world animal ID;
	// animal_identifier_2 carries the secondary tag when both RFID and old/source tag exist.
	if err := upsertSeedGoatIdentifiers(ctx, tx, tenantID, goatRows); err != nil {
		return st, err
	}

	// 5. Obligations-first, then completions. Build the two batches, classifying each cell.
	//    Alongside them we build the persisted source-fact lineage ledger (VACC-REV-01/02): one
	//    ledger entry per DATED source cell, keyed by its stable lineage identity, so every dated
	//    fact is accounted for exactly once by lineage and the in-transaction verify can prove each
	//    non-excluded fact reconciles against the row ACTUALLY persisted.
	var obls []oblIns
	var cmps []cmpIns
	var facts []sourceFact
	seenOblIdem := map[string]bool{}
	seenCmpIdem := map[string]bool{}

	parseFactDate := func(v string) *time.Time {
		if t, err := time.ParseInLocation("2006-01-02", v, loc); err == nil {
			d := t
			return &d
		}
		return nil
	}
	addFact := func(c vaccCell, val, disposition, oblID, cmpID, oblIdem, cmpIdem string) {
		facts = append(facts, sourceFact{
			lineageKey:     sourceFactLineageKey(c),
			animalKey:      c.AnimalKey,
			vaccineHeader:  c.Vaccine,
			doseCode:       sourceDoseCode(c),
			sequence:       c.Sequence,
			sourceValue:    val,
			sourceDate:     parseFactDate(val),
			disposition:    disposition,
			obligationID:   oblID,
			completionID:   cmpID,
			obligationIdem: oblIdem,
			completionIdem: cmpIdem,
		})
	}

	vaccMatrixDef := buildCanonicalVaccinationMatrix()

	orderedCells := append([]vaccCell(nil), cells...)
	sort.SliceStable(orderedCells, func(i, j int) bool {
		left, leftErr := time.ParseInLocation("2006-01-02", strings.TrimSpace(orderedCells[i].Value), loc)
		right, rightErr := time.ParseInLocation("2006-01-02", strings.TrimSpace(orderedCells[j].Value), loc)
		if leftErr != nil {
			return false
		}
		if rightErr != nil {
			return true
		}
		return left.Before(right)
	})
	acceptedHistoryByAnimalKey := map[string][]vaccinationdomain.RecentVaccineAdministration{}

	for _, c := range orderedCells {
		// A dated fact is any cell that is neither blank/NA (nothing recorded) nor
		// "Pending" (explicit non-administration evidence owned by the kernel). Fix
		// Plan A1 requires every one of these 3,836 cells to be reconciled — imported
		// or explicitly, non-silently classified — never dropped without a reason.
		val := strings.TrimSpace(c.Value)
		isDatedFact := val != "" && !strings.EqualFold(val, "NA") && !strings.EqualFold(val, "Pending")

		goatID, ok := goatIDByAnimalKey[c.AnimalKey]
		if !ok {
			if isDatedFact {
				st.GoatNotPlacedDatedFacts++
				addFact(c, val, dispositionExcludedGoatNotPlaced, "", "", "", "")
			}
			continue // goat not placed (no shed) or not in herd sheet
		}
		versionID, ok := versionByVaccine[c.Vaccine]
		if !ok {
			if isDatedFact {
				st.VaccineUnrecognizedDatedFacts++
				addFact(c, val, dispositionExcludedVaccineUnknown, "", "", "", "")
			}
			continue
		}
		def := vaccines[c.Vaccine]

		switch {
		case val == "" || strings.EqualFold(val, "NA"):
			st.Skipped++
			continue
		case strings.EqualFold(val, "Pending"):
			// Pending is source evidence that no administration was recorded. The
			// vaccination kernel is the sole owner of the resulting scheduled or
			// missing-anchor deferred obligation. Writing a source placeholder here
			// as well creates two active rows for the same goat/rule with different
			// idempotency keys.
			st.PendingSource++
			continue
		}

		d, err := time.ParseInLocation("2006-01-02", val, loc)
		if err != nil {
			st.Skipped++
			st.UnresolvedDatedFacts++
			addFact(c, val, dispositionUnresolved, "", "", "", "")
			continue
		}
		if dob := goatDOBByAnimalKey[c.AnimalKey]; sourceVaccinationDateBeforeDOB(d, dob) {
			return st, fmt.Errorf("source animal %q has vaccination date %s before DOB %s for %s %s",
				c.AnimalKey, d.Format("2006-01-02"), dob.Format("2006-01-02"), c.Vaccine, c.DoseType)
		}

		// Resolve goat's schedule path (kid vs adult), then map the sheet dose to the
		// rule's dose code. For a single+repeat vaccine (FMD, HS: one wave per path —
		// never a fabricated booster rule), a sheet "Booster" cell resolves to the
		// SAME wave as a later recorded administration (Fix Plan A1) so the fact is
		// retained and available to anchor recurrence, instead of being silently
		// dropped as unmapped.
		// R50-001: classify this administration's schedule path (kid vs adult) AS OF the
		// dose's OWN administration date `d`, not the current seed business date `now`.
		// An animal now 17+ weeks old that received a 15-week dose must still classify
		// under whichever rule family applied to it AT THAT TIME — age-at-now silently
		// reclassifies old kid-course doses onto the adult path once the goat ages past
		// the kid cutoff.
		path := seedSchedulePathForGoat(
			goatOriginTypeByAnimalKey[c.AnimalKey],
			goatDOBByAnimalKey[c.AnimalKey],
			goatStageByAnimalKey[c.AnimalKey],
			goatEntryDateByAnimalKey[c.AnimalKey],
			d,
			seedKidsNormalScheduleUntilWeeks,
			acceptedHistoryByAnimalKey[c.AnimalKey],
		)
		doseCodeForPath, laterAdministration := mapSheetDoseToRuleCode(c.Vaccine, c.DoseCode, path, vaccMatrixDef)
		if doseCodeForPath == "" {
			st.Skipped++
			st.UnresolvedDatedFacts++
			addFact(c, val, dispositionUnresolved, "", "", "", "")
			continue
		}

		ruleID := ruleByDoseCode[doseCodeForPath]
		if ruleID == "" {
			st.Skipped++
			st.UnresolvedDatedFacts++
			addFact(c, val, dispositionUnresolved, "", "", "", "")
			continue
		}

		oblID := detUUID("obligation", tenantID, c.AnimalKey, def.Code, doseCodeForPath)
		oblIdem := "vacc-real-obl:" + c.AnimalKey + ":" + def.Code + ":" + doseCodeForPath
		scopeType, scopeID, err := seedObligationScope(goatShedByAnimalKey[c.AnimalKey])
		if err != nil {
			return st, err
		}

		// Open (scheduled/due) vaccination obligations are blocked by the procurement
		// exclusion guard for goats that are dead/sold/lost/culled/transferred/merged/inactive.
		// Completed history is still allowed for those goats.
		openEligible := !excludedLifecycle(goatLifecycleByAnimalKey[c.AnimalKey])

		administeredAt := sourceVaccinationDateTime(d, loc)
		if sourceVaccinationDateOnOrBeforeBusinessDate(d, now, loc) {
			// Source date cells are imported base anchors. Persist anchors on or
			// before the business date as accepted/completed history so the
			// recurrence engine has a durable start point, but never materialize
			// open work from the anchor itself.
			completedAt := administeredAt
			verifiedBy := detUUID("seed-actor", tenantID)
			sourceDateKey := administeredAt.Format("2006-01-02")
			historyOblIdem := historyObligationIdem(c, def, sourceDateKey)
			historyCmpIdem := historyCompletionIdem(c, def, sourceDateKey)
			historyOblID := historyObligationID(tenantID, c, def, sourceDateKey)
			historyCmpID := historyCompletionID(tenantID, c, def, sourceDateKey)
			// Two distinct source cells that resolve to the SAME committed history row (same
			// animal/vaccine/dose administered on the same day) collapse onto one row. Record the
			// second explicitly as a merge instead of appending a duplicate the DB would silently
			// drop via ON CONFLICT — accounted, never lost.
			if seenCmpIdem[historyCmpIdem] {
				addFact(c, val, dispositionLaterAdministrationMerge, historyOblID, historyCmpID, historyOblIdem, historyCmpIdem)
				st.Completed++
				st.CompletionsHistory++
				if laterAdministration {
					st.LaterAdministrationsReconciled++
				}
				continue
			}
			seenCmpIdem[historyCmpIdem] = true
			seenOblIdem[historyOblIdem] = true
			obls = append(obls, oblIns{
				obligationID: historyOblID,
				versionID:    versionID,
				ruleID:       ruleID,
				goatID:       goatID,
				scopeType:    scopeType,
				scopeID:      scopeID,
				dueAt:        administeredAt,
				status:       "completed",
				completedAt:  &completedAt,
				sequence:     c.Sequence,
				idem:         historyOblIdem,
			})
			cmps = append(cmps, cmpIns{
				completionID:   historyCmpID,
				obligationID:   historyOblID,
				goatID:         goatID,
				doseML:         def.DoseML,
				administeredAt: administeredAt,
				verifiedAt:     &administeredAt,
				verifiedBy:     verifiedBy,
				idem:           historyCmpIdem,
			})
			addFact(c, val, dispositionImportedCompletion, historyOblID, historyCmpID, historyOblIdem, historyCmpIdem)
			acceptedHistoryByAnimalKey[c.AnimalKey] = append(
				acceptedHistoryByAnimalKey[c.AnimalKey],
				vaccinationdomain.RecentVaccineAdministration{
					AdministeredAt:    administeredAt,
					VaccineCode:       def.Code,
					VaccineType:       def.Type,
					PathogenClass:     def.Pathogen,
					DoseCode:          doseCodeForPath,
					Sequence:          int32(c.Sequence),
					ProtocolVersionID: versionID,
				},
			)
			st.Completed++
			st.CompletionsHistory++
			if laterAdministration {
				st.LaterAdministrationsReconciled++
			}
			// NO flat next-revacc obligation; kernel will generate revacc from the completion
		} else {
			if !openEligible {
				st.Skipped++
				st.LifecycleExcludedDatedFacts++
				addFact(c, val, dispositionExcludedLifecycle, "", "", "", "")
				continue
			}
			// A single open dose per goat/rule: two future source cells for the same
			// goat/vaccine/dose collapse onto one scheduled obligation. Record the second as a
			// merge rather than a duplicate open row.
			if seenOblIdem[oblIdem] {
				addFact(c, val, dispositionLaterAdministrationMerge, oblID, "", oblIdem, "")
				st.Scheduled++
				if laterAdministration {
					st.LaterAdministrationsReconciled++
				}
				continue
			}
			seenOblIdem[oblIdem] = true
			// Future dose -> scheduled at the sheet date.
			obls = append(obls, oblIns{
				obligationID: oblID, versionID: versionID, ruleID: ruleID, goatID: goatID,
				scopeType: scopeType, scopeID: scopeID,
				dueAt: administeredAt, status: "scheduled", sequence: c.Sequence, idem: oblIdem,
			})
			addFact(c, val, dispositionScheduledObligation, oblID, "", oblIdem, "")
			st.Scheduled++
			if laterAdministration {
				st.LaterAdministrationsReconciled++
			}
		}
	}
	st.Obligations = len(obls)

	if err := reconcileDatedFacts(cells, st); err != nil {
		return st, err
	}
	// VACC-REV-01: every dated source fact accounted for exactly once by lineage (not summed
	// completion + obligation counts), and unknown dated vaccine headers FAIL the seed [P1].
	if err := reconcileSourceFactLineage(countDatedFacts(cells), facts); err != nil {
		return st, err
	}

	// Insert obligations first (FK target for completions).
	if err := batch(ctx, tx, obls, 500, func(b *pgx.Batch, o oblIns) {
		b.Queue(`
			INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id,
				target_type, target_id, scope_type, scope_id, due_at, window_start, status, completed_at,
				sequence, idempotency_key, created_at, updated_at)
			VALUES ($1,$2,$3,$4,'goat',$5,$6,$7,$8,$9,$10,$11,$12,$13,now(),now())
			ON CONFLICT DO NOTHING`,
			o.obligationID, tenantID, o.versionID, o.ruleID, o.goatID, o.scopeType, o.scopeID, o.dueAt, o.windowStart,
			o.status, o.completedAt, o.sequence, o.idem)
	}); err != nil {
		return st, fmt.Errorf("insert obligations: %w", err)
	}

	// Then completions linked to their obligation.
	if err := batch(ctx, tx, cmps, 500, func(b *pgx.Batch, cm cmpIns) {
		b.Queue(`
			INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, goat_id,
				doses, dose_ml_given, administered_at, verified_at, verified_by, status, idempotency_key, created_at, updated_at)
			VALUES ($1,$2,$3,$4,1,$5,$6,$7,$8,'accepted',$9,now(),now())
			ON CONFLICT DO NOTHING`,
			cm.completionID, tenantID, cm.obligationID, cm.goatID, cm.doseML, cm.administeredAt, cm.verifiedAt, cm.verifiedBy, cm.idem)
	}); err != nil {
		return st, fmt.Errorf("insert completions: %w", err)
	}

	// Persist the source-fact lineage ledger in the SAME transaction.
	if err := insertSourceFactLedger(ctx, tx, tenantID, seedRunID, facts); err != nil {
		return st, fmt.Errorf("insert source-fact ledger: %w", err)
	}

	// VACC-REV-02 [P0]: reconcile the lineage ledger against the rows ACTUALLY persisted in this
	// transaction, BEFORE commit. A silent ON CONFLICT DO NOTHING drop leaves an expected
	// idempotency key absent, which rolls the whole transaction back (deferred Rollback) so a
	// half-imported source is never committed.
	if err := verifyPersistedSourceFactsInTx(ctx, tx, tenantID, facts); err != nil {
		return st, err
	}
	if err := verifyVaccinationObligationsShedScopedInTx(ctx, tx, tenantID); err != nil {
		return st, err
	}

	// R50-003: validate the -expect-null-false-dob count gate BEFORE commit, not
	// after. Dispositions are computed at load time (in-memory, pre-insert); check
	// them here so a mismatch aborts the transaction (deferred Rollback fires,
	// nothing is persisted) instead of failing only after the source data is
	// already durably committed.
	st.DobNulled = len(dobDispositions)
	if expectNullFalseDob >= 0 && st.DobNulled != expectNullFalseDob {
		return st, fmt.Errorf("dob_nulled count gate failed: expected %d, got %d", expectNullFalseDob, st.DobNulled)
	}
	if err := persistDobDispositionProofInTx(ctx, tx, seedRunID, dobDispositions); err != nil {
		return st, err
	}

	if err := tx.Commit(ctx); err != nil {
		return st, fmt.Errorf("commit: %w", err)
	}
	committed = true
	// The database detail written above is canonical and atomic with the imported
	// herd. The sidecars are only convenience exports; an unwritable source folder
	// cannot invalidate or orphan the committed audit proof.
	if err := writeDobDispositionAudit(sourcePath, now, seedRunID, dobDispositions); err != nil {
		fmt.Printf("WARN: %v\n", err)
	}
	if err := writeStageCorrectionAudit(sourcePath, now, seedRunID, stageCorrections); err != nil {
		fmt.Printf("WARN: %v\n", err)
	}

	// Print highlighted stage-correction report to stdout so users see it immediately.
	if len(stageCorrections) > 0 {
		fmt.Println("\n=== HIGHLIGHTED: STAGE AUTO-CORRECTIONS ===")
		fmt.Printf("Seed auto-corrected %d goat(s) with age/stage contradictions:\n", len(stageCorrections))
		for _, sc := range stageCorrections {
			fmt.Printf("  %s: age %dw, source_stage=%q → corrected_stage=%q\n", sc.RFID, sc.AgeWeeks, sc.SourceStage, sc.CorrectedStage)
		}
		fmt.Println("===========================================")
		fmt.Println()
		st.StagesCorrected = len(stageCorrections)
	}

	// Source committed; move the run into the generating state (kernel derivation runs next).
	if err := markSeedRunState(ctx, pool, seedRunID, seedRunStateGenerating, false); err != nil {
		return st, err
	}

	// Run kernel generation IN THE SEED to produce derived obligations. This
	// materializes kid DOB rules and history-anchored revac/booster work. Adult
	// blank-history initial rows are manual campaign rules and must not fire here:
	// adult entry_date is never a vaccination due anchor, and a campaign trigger is
	// required before blank-history adult cohorts become work.
	protocolRepo := protocolpg.NewRepository(pool, pgCfg.QueryTimeout)
	vaccinationRepo := vaccinationpg.NewRepository(pool, pgCfg.QueryTimeout)
	obligationRepo := obligationpg.NewRepository(pool, pgCfg.QueryTimeout)
	gen := vaccinationapp.NewGenerationService(protocolRepo, vaccinationRepo, obligationRepo)

	genRes, err := gen.GenerateEffectiveForAllGoats(ctx, tenantID, now)
	st.KernelGenerated = genRes.Generated
	st.KernelDeferred = genRes.Deferred
	st.KernelSuppressed = genRes.SuppressedByTrustedHistory

	genErr := seedGenerationError(genRes, err, allowPartialGeneration)
	if err != nil && !vaccinationapp.IsGenerationPartialFailure(err) {
		return st, genErr
	}

	printSeedSummary(st, genRes)
	if genErr != nil {
		return st, genErr
	}
	satisfied, err := supersedeActiveOneTimeVaccinationObligationsCoveredByHistory(ctx, pool, tenantID)
	if err != nil {
		return st, err
	}
	if satisfied > 0 {
		fmt.Printf("seed_reconciliation repair: superseded_active_after_history=%d\n", satisfied)
	}
	if err := verifySeedReconciliation(ctx, pool, tenantID, now, st); err != nil {
		return st, err
	}
	if err := verifyActiveGoatsHaveShed(ctx, pool, tenantID); err != nil {
		return st, err
	}
	if err := verifyActiveGoatsUsePhysicalShedLocations(ctx, pool, tenantID); err != nil {
		return st, err
	}
	if err := verifyVaccinationObligationsShedScoped(ctx, pool, tenantID); err != nil {
		return st, err
	}

	// Only a successful post-generation invariant verification marks the run verified/ready.
	if err := markSeedRunState(ctx, pool, seedRunID, seedRunStateVerified, true); err != nil {
		return st, err
	}

	return st, nil
}

func supersedeActiveOneTimeVaccinationObligationsCoveredByHistory(ctx context.Context, pool *pgxpool.Pool, tenantID string) (int64, error) {
	tag, err := pool.Exec(ctx, `
UPDATE obligation_instances current_oi
SET status = 'superseded',
    updated_at = now(),
    row_version = row_version + 1
FROM protocol_rules current_pr
WHERE current_oi.tenant_id = $1::uuid
  AND current_oi.target_type = 'goat'
  AND current_oi.status NOT IN ('completed', 'canceled', 'superseded', 'waived')
  AND current_pr.tenant_id = current_oi.tenant_id
  AND current_pr.rule_id = current_oi.rule_id
  AND COALESCE(NULLIF(current_pr.repeat, ''), 'none') = 'none'
  AND EXISTS (
    SELECT 1
    FROM obligation_instances history_oi
    JOIN vaccination_completions history_vc
      ON history_vc.tenant_id = history_oi.tenant_id
     AND history_vc.obligation_id = history_oi.obligation_id
     AND history_vc.status = 'accepted'
    WHERE history_oi.tenant_id = current_oi.tenant_id
      AND history_oi.target_id = current_oi.target_id
      AND history_oi.rule_id = current_oi.rule_id
      AND history_oi.status = 'completed'
  )`, tenantID)
	if err != nil {
		return 0, fmt.Errorf("seed: supersede active one-time vaccination obligations covered by history: %w", err)
	}
	return tag.RowsAffected(), nil
}

func seedGenerationError(genRes vaccinationdomain.GenerateResult, err error, allowPartialGeneration bool) error {
	if err == nil {
		return nil
	}
	if !vaccinationapp.IsGenerationPartialFailure(err) {
		return fmt.Errorf("generate effective cohort: %w", err)
	}
	if allowPartialGeneration {
		return nil
	}
	return fmt.Errorf("generate effective cohort partial failure: failed_goats=%d generated=%d deferred=%d reopened=%d skipped_no_due_date=%d suppressed_trusted=%d: %w",
		genRes.FailedGoats, genRes.Generated, genRes.Deferred, genRes.Reopened, genRes.SkippedNoDueDate, genRes.SuppressedByTrustedHistory, err)
}

type seedReconciliation struct {
	SourceAcceptedHistory        int64
	AcceptedStatusMismatches     int64
	DuplicateActiveRuleTargets   int64
	ActivePrimaryAfterHistory    int64
	SeededPendingPlaceholders    int64
	RepeatObligationsNotFuture   int64
	SchedulableOpenWorkNotFuture int64
	MissingBreedForeignKeys      int64
	MissingPrimaryIdentifiers    int64
	MissingAnchorNormalWork      int64
	MissingAdultETTTDose2        int64
}

// verifySeedReconciliation is the non-optional seed postflight. A seed command may
// have committed source rows before generation fails, so a failed postflight means
// the environment is unusable and must be reset/reseeded; it must never be handed
// off as partially healthy.
func verifySeedReconciliation(ctx context.Context, pool *pgxpool.Pool, tenantID string, asOf time.Time, st stats) error {
	var got seedReconciliation
	// projection-review: membership=obligation_instances (active, non-terminal) + vaccination_completions (accepted); group_key=(tenant_id, target_id, rule_id) for the duplicate-active grain; join_cardinality=protocol_rules joined 1:1 per (tenant_id, rule_id) so the COUNT/GROUP BY never fans out; pagination=none — a whole-tenant one-shot seed invariant, not a paged user projection; scope=tenant-invariant with an explicit target_type='goat' filter (park/shed/cohort not aggregated here)
	err := pool.QueryRow(ctx, `
WITH active AS (
  SELECT oi.*
  FROM obligation_instances oi
  WHERE oi.tenant_id = $1::uuid
    AND oi.status NOT IN ('completed', 'canceled', 'superseded', 'waived')
), duplicate_active AS (
  SELECT target_id, rule_id
  FROM active
  WHERE target_type = 'goat'
  GROUP BY target_id, rule_id
  HAVING count(*) > 1
)
SELECT
  (SELECT count(*)
   FROM vaccination_completions vc
   WHERE vc.tenant_id = $1::uuid
     AND vc.status = 'accepted'
     AND vc.idempotency_key LIKE 'vacc-real-cmp:%'),
  (SELECT count(*)
   FROM vaccination_completions vc
   JOIN obligation_instances oi
     ON oi.tenant_id = vc.tenant_id AND oi.obligation_id = vc.obligation_id
   WHERE vc.tenant_id = $1::uuid
     AND vc.status = 'accepted'
     AND oi.status <> 'completed'),
  (SELECT count(*) FROM duplicate_active),
  (SELECT count(*)
   FROM active current_oi
   JOIN protocol_rules current_pr
     ON current_pr.tenant_id = current_oi.tenant_id
    AND current_pr.rule_id = current_oi.rule_id
   WHERE current_oi.target_type = 'goat'
     AND COALESCE(NULLIF(current_pr.repeat, ''), 'none') = 'none'
     AND EXISTS (
       SELECT 1
       FROM obligation_instances history_oi
       JOIN vaccination_completions history_vc
         ON history_vc.tenant_id = history_oi.tenant_id
        AND history_vc.obligation_id = history_oi.obligation_id
        AND history_vc.status = 'accepted'
       WHERE history_oi.tenant_id = current_oi.tenant_id
         AND history_oi.target_id = current_oi.target_id
         AND history_oi.rule_id = current_oi.rule_id
         AND history_oi.status = 'completed'
     )),
  (SELECT count(*)
   FROM active
   WHERE idempotency_key LIKE 'vacc-real-obl:%'),
  (SELECT count(*)
   FROM active current_oi
   JOIN protocol_rules current_pr
     ON current_pr.tenant_id = current_oi.tenant_id
    AND current_pr.rule_id = current_oi.rule_id
   WHERE COALESCE(NULLIF(current_pr.repeat, ''), 'none') <> 'none'
     AND (current_oi.due_at AT TIME ZONE 'Asia/Kolkata')::date
         <= ($2::timestamptz AT TIME ZONE 'Asia/Kolkata')::date),
  (SELECT count(*)
   FROM active
   WHERE status <> 'deferred'
     AND NOT EXISTS (
       SELECT 1
       FROM vaccination_completions vc
       WHERE vc.tenant_id = active.tenant_id
         AND vc.obligation_id = active.obligation_id
         AND vc.status IN ('recorded', 'accepted')
     )
     AND (COALESCE(window_end, due_at) AT TIME ZONE 'Asia/Kolkata')::date
         < ($2::timestamptz AT TIME ZONE 'Asia/Kolkata')::date),
  (SELECT count(*)
   FROM goats g
   WHERE g.tenant_id = $1::uuid
     AND g.lifecycle_status <> 'inactive'
     AND g.breed_id IS NULL),
  (SELECT count(*)
   FROM goats g
   WHERE g.tenant_id = $1::uuid
     AND g.lifecycle_status <> 'inactive'
     AND NOT EXISTS (
       SELECT 1 FROM goat_identifiers gi
       WHERE gi.tenant_id = g.tenant_id
         AND gi.goat_id = g.goat_id
         AND gi.identifier_type = 'animal_identifier_1'
         AND gi.status = 'active'
         AND NULLIF(btrim(gi.identifier_value), '') IS NOT NULL
     )),
  (SELECT count(*)
   FROM vaccination_completions dose1_vc
   JOIN obligation_instances dose1_oi
     ON dose1_oi.tenant_id = dose1_vc.tenant_id
    AND dose1_oi.obligation_id = dose1_vc.obligation_id
   JOIN protocol_rules dose1_pr
     ON dose1_pr.tenant_id = dose1_oi.tenant_id
    AND dose1_pr.rule_id = dose1_oi.rule_id
    AND dose1_pr.dose_code = 'et_tt_adult_w1'
   WHERE dose1_vc.tenant_id = $1::uuid
     AND dose1_vc.status = 'accepted'
     AND NOT EXISTS (
       SELECT 1
       FROM obligation_instances dose2_oi
       JOIN protocol_rules dose2_pr
         ON dose2_pr.tenant_id = dose2_oi.tenant_id
        AND dose2_pr.rule_id = dose2_oi.rule_id
        AND dose2_pr.dose_code = 'et_tt_adult_w2'
       WHERE dose2_oi.tenant_id = dose1_oi.tenant_id
         AND dose2_oi.target_type = 'goat'
         AND dose2_oi.target_id = dose1_oi.target_id
         AND dose2_oi.status NOT IN ('canceled', 'superseded', 'waived')
     ))
`, tenantID, asOf).Scan(
		&got.SourceAcceptedHistory,
		&got.AcceptedStatusMismatches,
		&got.DuplicateActiveRuleTargets,
		&got.ActivePrimaryAfterHistory,
		&got.SeededPendingPlaceholders,
		&got.RepeatObligationsNotFuture,
		&got.SchedulableOpenWorkNotFuture,
		&got.MissingBreedForeignKeys,
		&got.MissingPrimaryIdentifiers,
		&got.MissingAdultETTTDose2,
	)
	if err != nil {
		return fmt.Errorf("vaccination seed reconciliation query: %w", err)
	}
	// MissingAnchorNormalWork can no longer be a pure-SQL count. Contract §89 option 4 legitimately
	// routes a blank vaccine family with an unavailable DOB/entry anchor to an adult catch-up at the
	// next compatible drive ("missing identity dates alone are not a clinical defer reason"), so a
	// non-deferred birth_age/post_arrival obligation on a NULL-anchor goat is NOT automatically a
	// defect. §13 still forbids a FABRICATED normal missing-anchor due. The only durable provenance
	// signal distinguishing the two is the obligation's idempotency key: the routed catch-up carries
	// the deterministic AnchorMissingCatchUpKey the generator stamps, a fabricated normal due does
	// not. The key is a sha256 hash (not SQL-LIKE-able), so we load the candidate rows and classify
	// them in Go against the generator's own exported key derivation.
	fabricatedMissingAnchor, err := countFabricatedMissingAnchorWorkPaged(ctx, pool, tenantID)
	if err != nil {
		return fmt.Errorf("vaccination seed reconciliation missing-anchor candidates: %w", err)
	}
	got.MissingAnchorNormalWork = int64(fabricatedMissingAnchor)
	if err := validateSeedReconciliation(got, int64(st.CompletionsHistory)); err != nil {
		return fmt.Errorf("vaccination seed reconciliation failed: %w", err)
	}
	fmt.Printf("seed_reconciliation accepted_history=%d status_mismatches=0 duplicate_active_rule_targets=0 active_primary_after_history=0 seeded_pending_placeholders=0 repeat_not_future=0 schedulable_not_future=0 missing_breed_fks=0 missing_primary_identifiers=0 missing_anchor_normal_work=0 missing_adult_ettt_dose2=0\n", got.SourceAcceptedHistory)
	return nil
}

// missingAnchorCandidate is one active, non-deferred birth_age/post_arrival obligation on a goat
// whose own trigger anchor (DOB for birth_age, herd-entry date for post_arrival) is NULL and which
// has no accepted completion history for that rule. Post-VAX-REV-02 this class contains BOTH the
// contract-legitimate Contract §89 option-4 catch-up (routed to the next compatible drive) AND — if a
// regression ever fabricates one — a normal missing-anchor due that §13 forbids. The idempotency key
// is the durable signal that separates them.
type missingAnchorCandidate struct {
	idempotencyKey    string
	tenantID          string
	protocolVersionID string
	ruleID            string
	goatID            string
	sequence          int32
	triggerType       string
}

// isRoutedCatchUp reports whether this candidate is the deliberate Contract §89 option-4 adult
// catch-up: its persisted idempotency key equals the deterministic key the generator stamps on that
// path (vaccinationapp.AnchorMissingCatchUpKey). Any other key on a NULL-anchor birth_age/post_arrival
// obligation means the row was materialized WITHOUT going through the option-4 routing — a fabricated
// normal missing-anchor due, which stays a reconciliation defect.
func (c missingAnchorCandidate) isRoutedCatchUp() bool {
	return c.idempotencyKey == vaccinationapp.AnchorMissingCatchUpKey(
		c.tenantID, c.protocolVersionID, c.ruleID, c.goatID, c.triggerType, c.sequence)
}

// countFabricatedMissingAnchorWork returns how many missing-anchor candidates are NOT the routed
// §89 option-4 catch-up — the genuine defect the MissingAnchorNormalWork invariant guards. Legitimate
// routed catch-ups are excluded; everything else still fails reconciliation. This never blanket-
// ignores missing-anchor work: a fabricated normal due (any non-catch-up key) is always counted.
func countFabricatedMissingAnchorWork(candidates []missingAnchorCandidate) int {
	n := 0
	for _, c := range candidates {
		if !c.isRoutedCatchUp() {
			n++
		}
	}
	return n
}

// missingAnchorReconcilePageSize bounds each keyset page of the missing-anchor reconciliation scan
// so the postflight check never streams the whole cohort into the process at once (RV-04). At the
// accepted 5k-50k operational envelope (real data ~2.5k animals), a 50k-obligation cohort is ten
// bounded pages at this size. Paging here is defensive hygiene against an unbounded read, not an
// OOM guard for a multi-million-row table that does not exist in this envelope.
const missingAnchorReconcilePageSize = 5000

// countFabricatedMissingAnchorWorkPaged returns how many active, non-deferred birth_age/post_arrival
// obligations on a NULL-anchor goat with no accepted history for that rule are NOT the routed §89
// option-4 catch-up (the genuine defect the MissingAnchorNormalWork invariant guards). The WHERE
// clause mirrors the invariant's former pure-SQL sub-select exactly; classification (routed catch-up
// vs fabricated due) happens in Go against the generator's own key derivation so the two can never
// drift.
//
// RV-04: work is bounded by DATABASE examination, not by matches. Each page keyset-scans exactly
// missingAnchorReconcilePageSize BASE obligation_ids off the obligation_instances primary key
// (step 1), then joins/filters only that bounded id set (step 2) to classify fabricated vs routed
// §89 catch-up in Go against the generator's own key derivation. The cursor advances by the last
// EXAMINED base row, not the last matched candidate, and the loop ends when a base page returns
// fewer than a full page. A sparse or zero-candidate tenant therefore examines at most one page of
// PK-index entries per query instead of scanning every obligation to find a full page of matches --
// the earlier "LIMIT after the selective join/NOT EXISTS" shape could examine the whole table for a
// healthy tenant even at the 5k-50k operational envelope's upper bound. Memory stays O(page): only
// ids and the running count are held, never the cohort.
func countFabricatedMissingAnchorWorkPaged(ctx context.Context, pool *pgxpool.Pool, tenantID string) (int, error) {
	return countFabricatedMissingAnchorWorkPagedWithPageSize(ctx, pool, tenantID, missingAnchorReconcilePageSize)
}

// countFabricatedMissingAnchorWorkPagedWithPageSize is countFabricatedMissingAnchorWorkPaged with
// an overridable base-page size, so a test can force the pagination boundary to fall after just a
// handful of seeded rows (a page size of 2-3) instead of needing to bulk-seed
// missingAnchorReconcilePageSize (5000) real rows to exercise the SAME "first page empty, match on
// a later page" code path (RV-04). Production always calls countFabricatedMissingAnchorWorkPaged,
// which fixes pageSize at missingAnchorReconcilePageSize.
func countFabricatedMissingAnchorWorkPagedWithPageSize(ctx context.Context, pool *pgxpool.Pool, tenantID string, pageSize int32) (int, error) {
	fabricated := 0
	afterID := "00000000-0000-0000-0000-000000000000"
	for {
		// Step 1: bounded keyset page of BASE obligation ids off the PK. Examines exactly one page of
		// index entries regardless of how many (if any) turn out to be missing-anchor candidates.
		baseRows, err := pool.Query(ctx, `
SELECT obligation_id::text
FROM obligation_instances
WHERE tenant_id = $1::uuid
  AND obligation_id > $2::uuid
ORDER BY obligation_id
LIMIT $3`, tenantID, afterID, pageSize)
		if err != nil {
			return 0, err
		}
		ids := make([]string, 0, pageSize)
		for baseRows.Next() {
			var id string
			if err := baseRows.Scan(&id); err != nil {
				baseRows.Close()
				return 0, err
			}
			ids = append(ids, id)
		}
		if err := baseRows.Err(); err != nil {
			baseRows.Close()
			return 0, err
		}
		baseRows.Close()
		if len(ids) == 0 {
			return fabricated, nil
		}

		// Step 2: classify only this bounded id set. The join/filter/NOT-EXISTS runs against at most
		// one page of ids resolved by PK, so it cannot fan out to a full-table scan.
		rows, err := pool.Query(ctx, `
SELECT oi.idempotency_key,
       oi.tenant_id::text,
       oi.protocol_version_id::text,
       oi.rule_id::text,
       oi.target_id::text,
       oi.sequence,
       pr.trigger_type
FROM obligation_instances oi
JOIN protocol_rules pr
  ON pr.tenant_id = oi.tenant_id
 AND pr.rule_id = oi.rule_id
JOIN goats g ON g.tenant_id = oi.tenant_id AND g.goat_id = oi.target_id
WHERE oi.tenant_id = $1::uuid
  AND oi.obligation_id = ANY($2::uuid[])
  AND oi.status NOT IN ('completed', 'canceled', 'superseded', 'waived')
  AND oi.status <> 'deferred'
  AND oi.target_type = 'goat'
  AND (
    (pr.trigger_type = 'birth_age' AND g.dob IS NULL)
    OR (pr.trigger_type = 'post_arrival' AND g.entry_date IS NULL)
  )
  AND NOT EXISTS (
    SELECT 1
    FROM obligation_instances history_oi
    JOIN vaccination_completions vc
      ON vc.tenant_id = history_oi.tenant_id
     AND vc.obligation_id = history_oi.obligation_id
     AND vc.status = 'accepted'
    WHERE history_oi.tenant_id = oi.tenant_id
      AND history_oi.target_id = oi.target_id
      AND history_oi.rule_id = oi.rule_id
  )
  AND NOT EXISTS (
    SELECT 1
    FROM protocol_rule_dimensions curr_dim
    JOIN protocol_rule_dimensions prev_dim
      ON prev_dim.tenant_id = curr_dim.tenant_id
     AND prev_dim.protocol_version_id = curr_dim.protocol_version_id
     AND prev_dim.category = curr_dim.category
     AND prev_dim.vaccine_code = curr_dim.vaccine_code
     AND prev_dim.trigger_type = curr_dim.trigger_type
     AND prev_dim.sequence < curr_dim.sequence
     AND prev_dim.repeat = 'none'
    JOIN obligation_instances history_oi
      ON history_oi.tenant_id = oi.tenant_id
     AND history_oi.target_id = oi.target_id
     AND history_oi.rule_id = prev_dim.rule_id
    JOIN vaccination_completions vc
      ON vc.tenant_id = history_oi.tenant_id
     AND vc.obligation_id = history_oi.obligation_id
     AND vc.status = 'accepted'
    WHERE curr_dim.tenant_id = oi.tenant_id
      AND curr_dim.protocol_version_id = oi.protocol_version_id
      AND curr_dim.rule_id = oi.rule_id
      AND curr_dim.category = 'vaccination'
      AND curr_dim.vaccine_code <> ''
      AND curr_dim.repeat = 'none'
  )`, tenantID, ids)
		if err != nil {
			return 0, err
		}
		for rows.Next() {
			var c missingAnchorCandidate
			if err := rows.Scan(&c.idempotencyKey, &c.tenantID, &c.protocolVersionID, &c.ruleID, &c.goatID, &c.sequence, &c.triggerType); err != nil {
				rows.Close()
				return 0, err
			}
			if !c.isRoutedCatchUp() {
				fabricated++
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return 0, err
		}
		rows.Close()

		if int32(len(ids)) < pageSize {
			return fabricated, nil
		}
		afterID = ids[len(ids)-1]
	}
}

func validateSeedReconciliation(got seedReconciliation, expectedHistory int64) error {
	problems := make([]string, 0, 6)
	if got.SourceAcceptedHistory < expectedHistory {
		problems = append(problems, fmt.Sprintf("accepted source history=%d want_at_least=%d", got.SourceAcceptedHistory, expectedHistory))
	}
	if got.AcceptedStatusMismatches != 0 {
		problems = append(problems, fmt.Sprintf("accepted completion/status mismatches=%d", got.AcceptedStatusMismatches))
	}
	if got.DuplicateActiveRuleTargets != 0 {
		problems = append(problems, fmt.Sprintf("duplicate active goat/rule groups=%d", got.DuplicateActiveRuleTargets))
	}
	if got.ActivePrimaryAfterHistory != 0 {
		problems = append(problems, fmt.Sprintf("active primary obligations already satisfied by accepted history=%d", got.ActivePrimaryAfterHistory))
	}
	if got.SeededPendingPlaceholders != 0 {
		problems = append(problems, fmt.Sprintf("seed-owned Pending placeholders=%d", got.SeededPendingPlaceholders))
	}
	if got.RepeatObligationsNotFuture != 0 {
		problems = append(problems, fmt.Sprintf("repeat obligations not strictly future=%d", got.RepeatObligationsNotFuture))
	}
	if got.SchedulableOpenWorkNotFuture != 0 {
		problems = append(problems, fmt.Sprintf("schedulable open work past latest safe date=%d", got.SchedulableOpenWorkNotFuture))
	}
	if got.MissingBreedForeignKeys != 0 {
		problems = append(problems, fmt.Sprintf("goats missing species-owned breed foreign key=%d", got.MissingBreedForeignKeys))
	}
	if got.MissingPrimaryIdentifiers != 0 {
		problems = append(problems, fmt.Sprintf("goats missing primary animal identifier=%d", got.MissingPrimaryIdentifiers))
	}
	if got.MissingAnchorNormalWork != 0 {
		problems = append(problems, fmt.Sprintf("missing trigger anchor goats with normal active work=%d", got.MissingAnchorNormalWork))
	}
	if got.MissingAdultETTTDose2 != 0 {
		problems = append(problems, fmt.Sprintf("adult ET+TT dose 1 completions missing mandatory dose 2 obligations=%d", got.MissingAdultETTTDose2))
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func seedObligationScope(shedID string) (scopeType, scopeID string, err error) {
	if strings.TrimSpace(shedID) != "" {
		return "shed", shedID, nil
	}
	return "", "", errors.New("seed-vaccination-real: accepted goat vaccination obligation has no shed scope")
}

func printSeedSummary(st stats, genRes vaccinationdomain.GenerateResult) {
	fmt.Printf("seeded real vaccination data with kernel generation:\n"+
		"  parks_resolved=%d sheds_resolved=%d sheds_created=%d protocols=%d animals=%d\n"+
		"  obligations=%d (completed=%d scheduled=%d) completions_history=%d pending_source=%d skipped_cells=%d\n"+
		"  dated_source_facts reconciled=%d (later_administrations=%d unresolved=%d lifecycle_excluded=%d goat_not_placed=%d vaccine_unrecognized=%d)\n"+
		"  purged_fixtures total=%d (obligations=%d batches=%d goats=%d sheds=%d other_child_rows=%d) retired_non_source_locations=%d\n"+
		"  kernel_generation generated=%d deferred=%d reopened=%d failed_goats=%d skipped_no_due_date=%d suppressed_trusted=%d\n",
		st.ParksResolved, st.ShedsResolved, st.ShedsCreated, st.Protocols, st.Animals,
		st.Obligations, st.Completed, st.Scheduled, st.CompletionsHistory, st.PendingSource, st.Skipped,
		st.Completed+st.Scheduled, st.LaterAdministrationsReconciled, st.UnresolvedDatedFacts,
		st.LifecycleExcludedDatedFacts, st.GoatNotPlacedDatedFacts, st.VaccineUnrecognizedDatedFacts,
		st.Purged.total(), st.Purged.Obligations, st.Purged.Batches,
		st.Purged.Goats, st.Purged.Sheds, st.Purged.OtherChildRows, st.RetiredSourceDrift,
		genRes.Generated, genRes.Deferred, genRes.Reopened, genRes.FailedGoats, genRes.SkippedNoDueDate, genRes.SuppressedByTrustedHistory)
}

// purgeCounts breaks down what the fixture purge removed, for the run report.
type purgeCounts struct {
	Obligations    int
	Batches        int
	Goats          int
	Sheds          int
	OtherChildRows int
}

func (p purgeCounts) total() int {
	return p.Obligations + p.Batches + p.Goats + p.Sheds + p.OtherChildRows
}

// purgeSyntheticFixtures removes leftover dev/integration-test synthetic fixtures and stale
// prior seed outputs that leak junk into the /calendar, /counts/herd, /config, and /vaccination
// surfaces, in FK-safe order. It targets ONLY:
//   - the trigger-seed vaccination protocol family (protocol code 'vaccination.matrix%') — its
//     obligations, planned batches, and stale calendar projections;
//   - previous seed-vaccination-real obligations (idempotency key 'vacc-real-obl:%') so a new
//     real-source run replaces the local due/history projection instead of stacking on it;
//   - old per-vaccine "Real herd import" protocol configs, which are retired so Config shows the
//     canonical vaccination.matrix seed only;
//   - synthetic fixture goats (display_id like 'G-0000NN');
//   - junk-named proof/trigger sheds ('Chain Proof%', 'Rework Proof%',
//     'Trusted History%', 'Trigger Shed%') and every goat still located in them;
//   - stale junk calendar_event_projections rows (junk protocol / junk title / junk shed).
//
// It NEVER touches real goats, real sheds (Castro/Godel/Gandhi/...), workforce, or the
// ravi@mesha.sg grant. It is idempotent: on a clean DB every statement is a no-op. All child
// rows are removed before their parents so no FK is violated. This is data cleanup on the
// existing schema — no schema change.
func purgeSyntheticFixtures(ctx context.Context, tx pgx.Tx, tenantID string) (purgeCounts, error) {
	var pc purgeCounts

	// Junk id subqueries (tenant-scoped). Evaluated fresh by each statement; parents are always
	// deleted after their children so these still resolve while children are being removed.
	junkSheds := `(SELECT location_id FROM locations WHERE tenant_id = $1 AND location_type = 'shed'
		AND (name ILIKE '%Chain Proof%' OR name ILIKE '%Rework Proof%' OR name ILIKE '%Trusted History%'
			OR name ILIKE '%Trigger Shed%'))`
	junkGoats := `(SELECT goat_id FROM goats WHERE tenant_id = $1 AND (
		display_id ~ '^G-0000[0-9]{2}$'
		OR shed_id IN ` + junkSheds + `
		OR current_location_id IN ` + junkSheds + `
	))`
	junkObls := `(SELECT oi.obligation_id FROM obligation_instances oi
		JOIN protocol_versions pv ON pv.tenant_id = oi.tenant_id AND pv.protocol_version_id = oi.protocol_version_id
		JOIN protocol_definitions pd ON pd.tenant_id = pv.tenant_id AND pd.protocol_id = pv.protocol_id
		WHERE oi.tenant_id = $1 AND (
			oi.idempotency_key LIKE 'vacc-real-obl:%'
			OR (pd.category = 'vaccination' AND pd.code LIKE 'vaccination.matrix%')
		))`
	junkBatches := `(SELECT ob.batch_id FROM obligation_batches ob
		JOIN protocol_versions pv ON pv.tenant_id = ob.tenant_id AND pv.protocol_version_id = ob.protocol_version_id
		JOIN protocol_definitions pd ON pd.tenant_id = pv.tenant_id AND pd.protocol_id = pv.protocol_id
		WHERE ob.tenant_id = $1 AND pd.category = 'vaccination' AND pd.code LIKE 'vaccination.matrix%')`
	legacySeedVersions := `(SELECT pv.protocol_version_id FROM protocol_versions pv
		JOIN protocol_definitions pd ON pd.tenant_id = pv.tenant_id AND pd.protocol_id = pv.protocol_id
		WHERE pv.tenant_id = $1 AND pd.category = 'vaccination' AND (
			pd.code LIKE 'vaccination.matrix.%'
			OR pd.code IN (
				'vaccination.et_tt', 'vaccination.ppr', 'vaccination.blue_tongue',
				'vaccination.fmd', 'vaccination.hs', 'vaccination.goat_pox',
				'vaccination.sheep_pox', 'vaccination.calendar.matrix',
				'vaccination.calendar.draft_matrix',
				'et_tt', 'ppr', 'blue_tongue', 'fmd', 'hs', 'goat_pox', 'sheep_pox'
			)
		))`
	legacySeedDefinitions := `(SELECT pd.protocol_id FROM protocol_definitions pd
		WHERE pd.tenant_id = $1 AND pd.category = 'vaccination' AND (
			pd.code LIKE 'vaccination.matrix.%'
			OR pd.code IN (
				'vaccination.et_tt', 'vaccination.ppr', 'vaccination.blue_tongue',
				'vaccination.fmd', 'vaccination.hs', 'vaccination.goat_pox',
				'vaccination.sheep_pox', 'vaccination.calendar.matrix',
				'vaccination.calendar.draft_matrix',
				'et_tt', 'ppr', 'blue_tongue', 'fmd', 'hs', 'goat_pox', 'sheep_pox'
			)
		))`

	exec := func(label, sql string) (int, error) {
		tag, err := tx.Exec(ctx, sql, tenantID)
		if err != nil {
			return 0, fmt.Errorf("purge %s: %w", label, err)
		}
		return int(tag.RowsAffected()), nil
	}

	var n int
	var err error

	// Phase 1 — migration 000189 dropped calendar_event_projections; no purge needed (canonical is source).

	// Phase 2 — trigger-seed obligations + their children.
	for _, child := range []string{"vaccination_completions", "obligation_status_events", "obligation_escalations", "feed_direction_completions"} {
		if _, err := exec(child+" (by obligation)", `DELETE FROM `+child+` WHERE tenant_id = $1 AND obligation_id IN `+junkObls); err != nil {
			return pc, err
		}
	}
	n, err = exec("obligation_instances (trigger-seed)", `DELETE FROM obligation_instances WHERE tenant_id = $1 AND obligation_id IN `+junkObls)
	if err != nil {
		return pc, err
	}
	pc.Obligations += n

	// Phase 3 — trigger-seed planned batches + their children (obligations already gone).
	for _, s := range []struct{ label, sql string }{
		{"inventory_stock_movements (batch)", `DELETE FROM inventory_stock_movements WHERE tenant_id = $1 AND batch_id IN ` + junkBatches},
		{"vaccination_completions (batch)", `DELETE FROM vaccination_completions WHERE tenant_id = $1 AND batch_id IN ` + junkBatches},
		{"feed_direction_completions (batch)", `DELETE FROM feed_direction_completions WHERE tenant_id = $1 AND batch_id IN ` + junkBatches},
	} {
		if _, err := exec(s.label, s.sql); err != nil {
			return pc, err
		}
	}
	n, err = exec("obligation_batches (trigger-seed)", `DELETE FROM obligation_batches WHERE tenant_id = $1 AND batch_id IN `+junkBatches)
	if err != nil {
		return pc, err
	}
	pc.Batches += n

	// Phase 4 — synthetic goats: delete every referencing child row, then the goats. Each DELETE is
	// tenant-scoped via junkGoats; tables without a tenant_id column are scoped by goat_id membership.
	goatChildren := []struct{ label, sql string }{
		{"goat_shed_partitions", `DELETE FROM goat_shed_partitions WHERE tenant_id = $1 AND goat_id IN ` + junkGoats},
		{"vaccination_completions (goat)", `DELETE FROM vaccination_completions WHERE tenant_id = $1 AND goat_id IN ` + junkGoats},
		{"obligation_instances (goat target)", `DELETE FROM obligation_instances WHERE tenant_id = $1 AND target_type = 'goat' AND target_id IN ` + junkGoats},
		{"identity_decision_events", `DELETE FROM identity_decision_events WHERE tenant_id = $1 AND event_id IN (SELECT event_id FROM goat_identity_events WHERE tenant_id = $1 AND goat_id IN ` + junkGoats + `)`},
		{"goat_identity_events", `DELETE FROM goat_identity_events WHERE tenant_id = $1 AND goat_id IN ` + junkGoats},
		{"goat_identifiers", `DELETE FROM goat_identifiers WHERE tenant_id = $1 AND goat_id IN ` + junkGoats},
		{"goat_location_history", `DELETE FROM goat_location_history WHERE tenant_id = $1 AND goat_id IN ` + junkGoats},
		{"goat_custody_history", `DELETE FROM goat_custody_history WHERE tenant_id = $1 AND goat_id IN ` + junkGoats},
		{"goat_ownership", `DELETE FROM goat_ownership WHERE tenant_id = $1 AND goat_id IN ` + junkGoats},
		{"goat_merge_links", `DELETE FROM goat_merge_links WHERE tenant_id = $1 AND (merged_goat_id IN ` + junkGoats + ` OR survivor_goat_id IN ` + junkGoats + `)`},
		{"sop_submission_items", `DELETE FROM sop_submission_items WHERE goat_id IN ` + junkGoats},
		{"procurement_pc_handoffs", `DELETE FROM procurement_pc_handoffs WHERE tenant_id = $1 AND goat_id IN ` + junkGoats},
		{"procurement_source_health_checks", `DELETE FROM procurement_source_health_checks WHERE tenant_id = $1 AND goat_id IN ` + junkGoats},
		{"procurement_hf_vaccination_evidence", `DELETE FROM procurement_hf_vaccination_evidence WHERE tenant_id = $1 AND goat_id IN ` + junkGoats},
		{"procurement_load_goats", `DELETE FROM procurement_load_goats WHERE tenant_id = $1 AND goat_id IN ` + junkGoats},
		{"arrival_intake_review_goats", `DELETE FROM arrival_intake_review_goats WHERE tenant_id = $1 AND goat_id IN ` + junkGoats},
		{"source_entry_decisions", `DELETE FROM source_entry_decisions WHERE tenant_id = $1 AND goat_id IN ` + junkGoats},
		{"source_holding_stays", `DELETE FROM source_holding_stays WHERE tenant_id = $1 AND goat_id IN ` + junkGoats},
		{"identity_conflict_goats", `DELETE FROM identity_conflict_goats WHERE tenant_id = $1 AND goat_id IN ` + junkGoats},
		{"identity_correction_requests", `DELETE FROM identity_correction_requests WHERE tenant_id = $1 AND goat_id IN ` + junkGoats},
		{"identity_decision_goats", `DELETE FROM identity_decision_goats WHERE tenant_id = $1 AND goat_id IN ` + junkGoats},
		{"obligation_goat_shift_watermarks", `DELETE FROM obligation_goat_shift_watermarks WHERE goat_id IN ` + junkGoats},
		{"vaccination_generation_runs (cursor)", `UPDATE vaccination_generation_runs SET cursor_goat_id = NULL WHERE tenant_id = $1 AND cursor_goat_id IN ` + junkGoats},
	}
	for _, s := range goatChildren {
		n, err := exec(s.label, s.sql)
		if err != nil {
			return pc, err
		}
		pc.OtherChildRows += n
	}
	// Hard-deleting goats is blocked by the maintainer-locked prevent_goat_hard_delete business-rule
	// trigger (its own message directs: retire to merged/inactive, never DELETE). So the sanctioned
	// removal is a soft-retire: mark the synthetic fixtures 'inactive' and detach them from all
	// locations. This drops them out of active-herd surfaces and frees the junk sheds for deletion,
	// without bypassing a business rule.
	n, err = exec("goats (synthetic soft-retire+detach)", `UPDATE goats
		SET lifecycle_status = 'inactive', shed_id = NULL, park_id = NULL, current_location_id = NULL,
		    cohort_id = NULL, farm_id = NULL, updated_at = now()
		WHERE tenant_id = $1 AND goat_id IN `+junkGoats)
	if err != nil {
		return pc, err
	}
	pc.Goats += n

	// Phase 5 — junk-named sheds: delete referencing rows, then the shed locations.
	shedChildren := []struct{ label, sql string }{
		{"location_operational_attributes", `DELETE FROM location_operational_attributes WHERE tenant_id = $1 AND location_id IN ` + junkSheds},
		{"shed_profiles", `DELETE FROM shed_profiles WHERE location_id IN ` + junkSheds},
		{"location_capacity_records", `DELETE FROM location_capacity_records WHERE location_id IN ` + junkSheds},
		{"location_aliases", `DELETE FROM location_aliases WHERE tenant_id = $1 AND canonical_location_id IN ` + junkSheds},
		{"goat_location_history (shed refs)", `DELETE FROM goat_location_history WHERE tenant_id = $1 AND (to_location_id IN ` + junkSheds + ` OR from_location_id IN ` + junkSheds + `)`},
	}
	for _, s := range shedChildren {
		n, err := exec(s.label, s.sql)
		if err != nil {
			return pc, err
		}
		pc.OtherChildRows += n
	}
	n, err = exec("locations (junk sheds)", `DELETE FROM locations WHERE tenant_id = $1 AND location_type = 'shed'
		AND (name ILIKE '%Chain Proof%' OR name ILIKE '%Rework Proof%' OR name ILIKE '%Trusted History%'
			OR name ILIKE '%Trigger Shed%')`)
	if err != nil {
		return pc, err
	}
	pc.Sheds += n

	// Phase 6 — trigger-seed farm/shed fixtures ("Trigger Gate Farm", "CBE Trigger Shed") from
	// seed-vaccination-trigger, which leak into the herd register. Delete their attrs/profiles then
	// the locations, but ONLY when nothing real references them (no goats, no workforce, no child
	// locations, no inventory) — so workforce and the ravi@mesha.sg grant stay intact.
	triggerLocs := `(SELECT location_id FROM locations WHERE tenant_id = $1 AND location_type IN ('farm', 'shed')
		AND (name ILIKE '%Trigger Gate%' OR name ILIKE '%Trigger Shed%')
		AND NOT EXISTS (SELECT 1 FROM goats g WHERE g.farm_id = locations.location_id OR g.park_id = locations.location_id
			OR g.shed_id = locations.location_id OR g.current_location_id = locations.location_id OR g.cohort_id = locations.location_id)
		AND NOT EXISTS (SELECT 1 FROM workforce_members w WHERE w.primary_location_id = locations.location_id)
		AND NOT EXISTS (SELECT 1 FROM locations c WHERE c.parent_location_id = locations.location_id)
		AND NOT EXISTS (SELECT 1 FROM inventory_stock s WHERE s.location_id = locations.location_id))`
	for _, s := range []struct{ label, sql string }{
		{"location_operational_attributes (trigger)", `DELETE FROM location_operational_attributes WHERE tenant_id = $1 AND location_id IN ` + triggerLocs},
		{"farm_profiles (trigger)", `DELETE FROM farm_profiles WHERE location_id IN ` + triggerLocs},
		{"shed_profiles (trigger)", `DELETE FROM shed_profiles WHERE location_id IN ` + triggerLocs},
		{"location_capacity_records (trigger)", `DELETE FROM location_capacity_records WHERE location_id IN ` + triggerLocs},
	} {
		if _, err := exec(s.label, s.sql); err != nil {
			return pc, err
		}
	}
	n, err = exec("locations (trigger farm/shed)", `DELETE FROM locations WHERE tenant_id = $1 AND location_id IN `+triggerLocs)
	if err != nil {
		return pc, err
	}
	pc.Sheds += n

	// Phase 7 — retire old seed protocol definitions/versions so Config shows the one canonical
	// vaccination.matrix family. Published protocol rows are immutable, but published -> retired is
	// explicitly allowed by the protocol immutability trigger.
	n, err = exec("protocol_versions (legacy seed retire)", `UPDATE protocol_versions
		SET status='retired', retired_at=COALESCE(retired_at, now()), updated_at=now(), row_version=row_version+1
		WHERE tenant_id = $1 AND protocol_version_id IN `+legacySeedVersions+`
		  AND status <> 'retired'`)
	if err != nil {
		return pc, err
	}
	pc.OtherChildRows += n
	n, err = exec("protocol_definitions (legacy seed retire)", `UPDATE protocol_definitions
		SET status='retired', updated_at=now(), row_version=row_version+1
		WHERE tenant_id = $1 AND protocol_id IN `+legacySeedDefinitions+`
		  AND status <> 'retired'`)
	if err != nil {
		return pc, err
	}
	pc.OtherChildRows += n

	return pc, nil
}

// batch queues rows via pgx.Batch in chunks and executes each chunk, surfacing the
// first row error. Triggers still fire per row, but round-trips are pipelined.
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

// ---- helpers ----

func buildEntryDateMapping(goats []goatRecord) map[string]*time.Time {
	// Precedence: purchase_date -> stage_entry_date. DOB is not an entry date.
	//
	// BIRTH-origin exception: purchase_date on a birth-origin (kid) source row belongs to
	// the DAM, not the kid — it is not this animal's own entry into the herd/stage. Using it
	// as the kid's entry date makes a valid pre-registration DOB look like "DOB after entry"
	// (the kid was born before the dam was purchased). For birth-origin animals the entry
	// date is ALWAYS the animal's own stage_entry_date; purchase_date is never consulted.
	out := map[string]*time.Time{}
	for _, g := range goats {
		animalKey := sourceAnimalIdentifier(g.RFID, g.OldID, g.OldIDSuffix)
		if animalKey == "" {
			continue
		}
		if normalizeOriginType(g.OriginType) == "birth" {
			if d := parseSourceDate(g.StageEntryDate); d != nil {
				out[animalKey] = d
			}
			continue
		}
		for _, raw := range []string{g.PurchaseDate, g.StageEntryDate} {
			if d := parseSourceDate(raw); d != nil {
				out[animalKey] = d
				break
			}
		}
	}
	return out
}

func parseSourceDate(raw string) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	for _, layout := range []string{"2006-01-02", "02/01/2006", "1/2/2006"} {
		if d, err := time.Parse(layout, raw); err == nil {
			return &d
		}
	}
	return nil
}

// seedSchedulePathForGoat delegates kid/adult rule-family selection to the live vaccination
// scheduler. Source dates are preserved as history after this decision; a raw sheet cell must
// never be pre-mapped as kid-course history to prove its own rule family.
func seedSchedulePathForGoat(originType string, dob *time.Time, stage string, entryDate *time.Time, asOf time.Time, kidCutoffWeeks int32, history []vaccinationdomain.RecentVaccineAdministration) string {
	path := vaccinationapp.SchedulePathForGoat(vaccinationdomain.EligibleGoat{
		OriginType: originType,
		DOB:        dob,
		Stage:      stage,
		EntryDate:  entryDate,
	}, vaccinationapp.SchedulePathProcurementPolicy{
		KidsNormalScheduleUntilWeeks: kidCutoffWeeks,
	}, asOf, history)
	if path == vaccinationapp.SchedulePathKid {
		return "kid"
	}
	return "adult"
}

// mapSheetDoseToRuleCode maps a sheet dose code (First/Booster) to the actual rule dose_code
// based on goat schedule path (kid vs adult) and vaccine spec.
//
// Fix Plan A1: for a vaccine whose approved matrix defines only ONE wave per path (FMD, HS —
// "single + repeat", never a two-wave birth-age/post-arrival course), a sheet "Booster" cell is
// a LATER recorded administration of that SAME rule, not a distinct booster dose. It resolves
// to the single wave's dose code, and reconciledAsLaterAdministration reports that case so the
// caller can account for the fact explicitly instead of silently dropping it as unmapped. This
// does NOT fabricate a new booster rule — the returned dose code is the vaccine's one existing
// wave, so both the first and the later administration land as separate accepted completions
// under the SAME rule/goat pair (distinguished by due_at), letting generation.go anchor
// recurrence on whichever is latest.
func mapSheetDoseToRuleCode(vaccine string, sheetDoseCode string, path string, vaccMatrixDef map[string]vaccMatrixSpec) (doseCode string, reconciledAsLaterAdministration bool) {
	spec, ok := vaccMatrixDef[vaccine]
	if !ok {
		return "", false
	}
	def, ok := vaccines[vaccine]
	if !ok {
		return "", false
	}

	if path == "kid" {
		switch {
		case sheetDoseCode == "first" && len(spec.BirthAgeWaves) > 0:
			return spec.BirthAgeWaves[0].DoseCode, false
		case sheetDoseCode == "booster" && len(spec.BirthAgeWaves) > 1:
			return spec.BirthAgeWaves[1].DoseCode, false
		case sheetDoseCode == "booster" && len(spec.BirthAgeWaves) == 1:
			return spec.BirthAgeWaves[0].DoseCode, true
		case sheetDoseCode == "first" && len(spec.BirthAgeWaves) > 0:
			return spec.BirthAgeWaves[0].DoseCode, false
		}
		return "", false
	}

	// Adult path: map dated sheet First/Booster cells to the adult campaign dose
	// codes used for accepted history. Blank/pending adult work is generated by
	// campaign/catch-up cohort logic, never by entry_date/post_arrival.
	switch {
	case sheetDoseCode == "first" && len(spec.PostArrivalWaves) > 0:
		return def.Code + "_adult_w1", false
	case sheetDoseCode == "booster" && len(spec.PostArrivalWaves) > 1:
		return def.Code + "_adult_w2", false
	case sheetDoseCode == "booster" && len(spec.PostArrivalWaves) == 1:
		return def.Code + "_adult_w1", true
	case sheetDoseCode == "first" && len(spec.PostArrivalWaves) > 0:
		return def.Code + "_adult_w1", false
	}
	return "", false
}

// countDatedFacts counts source vaccination cells that carry an actual date (i.e. every cell
// that is neither blank/NA nor "Pending"). This is the source-of-truth total that Fix Plan A1's
// reconciliation gate checks against (3,836 in the reviewed source snapshot).
func countDatedFacts(cells []vaccCell) int {
	n := 0
	for _, c := range cells {
		val := strings.TrimSpace(c.Value)
		if val == "" || strings.EqualFold(val, "NA") || strings.EqualFold(val, "Pending") {
			continue
		}
		n++
	}
	return n
}

// reconcileDatedFacts is the Fix Plan A1 hard gate: every dated source cell must land in
// exactly one explicit bucket — imported (Completed/Scheduled) or one of the four documented
// exclusion reasons. If the totals do not add up, some dated fact was dropped without being
// accounted for, and the seed must fail loudly rather than silently lose source history.
func reconcileDatedFacts(cells []vaccCell, st stats) error {
	total := countDatedFacts(cells)
	reconciled := st.Completed + st.Scheduled +
		st.UnresolvedDatedFacts + st.LifecycleExcludedDatedFacts +
		st.GoatNotPlacedDatedFacts + st.VaccineUnrecognizedDatedFacts
	if total != reconciled {
		return fmt.Errorf(
			"vaccination source fact reconciliation gap: dated_source_facts=%d reconciled=%d "+
				"(completed=%d scheduled=%d later_administrations=%d unresolved=%d lifecycle_excluded=%d goat_not_placed=%d vaccine_unrecognized=%d) "+
				"— zero silent drops required",
			total, reconciled, st.Completed, st.Scheduled, st.LaterAdministrationsReconciled,
			st.UnresolvedDatedFacts, st.LifecycleExcludedDatedFacts, st.GoatNotPlacedDatedFacts, st.VaccineUnrecognizedDatedFacts)
	}
	return nil
}

// ---- VACC-REV-01/02: persisted source-fact lineage ledger + seed-run state machine ----

// Seed-run states come from the shared seedrun state machine (verified == READY/promotable;
// failed == RESET_REQUIRED) so the seed and the closeout/promotion gate agree on one vocabulary.
const (
	seedRunStateLoading    = seedrun.StateLoading
	seedRunStateGenerating = seedrun.StateGenerating
	seedRunStateVerified   = seedrun.StateVerified
	seedRunStateFailed     = seedrun.StateFailed
	seedRunCommand         = "seed-vaccination-real"
)

// Source-fact dispositions. Every DATED source cell lands in exactly one of these, exactly once,
// keyed by its lineage identity — never by summing completion counts and obligation counts.
const (
	dispositionImportedCompletion       = "imported_completion"
	dispositionScheduledObligation      = "scheduled_obligation"
	dispositionLaterAdministrationMerge = "later_administration_merge"
	dispositionExcludedGoatNotPlaced    = "excluded_goat_not_placed"
	dispositionExcludedVaccineUnknown   = "excluded_vaccine_unrecognized"
	dispositionExcludedLifecycle        = "excluded_lifecycle"
	dispositionUnresolved               = "unresolved"
)

// sourceFact is one dated source vaccination cell, carrying its stable lineage identity and how it
// was reconciled. It is persisted to vaccination_source_facts and drives the in-transaction
// committed-row verification.
type sourceFact struct {
	lineageKey     string
	animalKey      string
	vaccineHeader  string
	doseCode       string
	sequence       int
	sourceValue    string
	sourceDate     *time.Time
	disposition    string
	obligationID   string
	completionID   string
	obligationIdem string
	completionIdem string
}

// sourceFactLineageKey is the stable per-source-cell identity used for exactly-once accounting. It
// is independent of the insert path (completion vs scheduled vs excluded), so two distinct cells can
// never share a lineage key and one cell can never be counted twice.
func sourceFactLineageKey(c vaccCell) string {
	return strings.Join([]string{
		c.AnimalKey,
		c.Vaccine,
		sourceDoseCode(c),
		strconv.Itoa(c.Sequence),
		strings.TrimSpace(c.Value),
	}, "|")
}

func sourceFactID(tenantID, lineageKey string) string {
	return detUUID("source-fact", tenantID, lineageKey)
}

// beginSeedRun records a new seed run in the `loading` state on the pool (outside the data tx) so a
// data-tx rollback still leaves a durable failure marker.
func beginSeedRun(ctx context.Context, pool *pgxpool.Pool, tenantID string) (string, error) {
	runID := detUUID("seed-run", tenantID, seedRunCommand, time.Now().UTC().Format(time.RFC3339Nano))
	_, err := pool.Exec(ctx, `
		INSERT INTO seed_runs (seed_run_id, tenant_id, command, state)
		VALUES ($1, $2, $3, $4)`,
		runID, tenantID, seedRunCommand, seedRunStateLoading)
	if err != nil {
		return "", fmt.Errorf("begin seed run: %w", err)
	}
	return runID, nil
}

func markSeedRunState(ctx context.Context, pool *pgxpool.Pool, runID, state string, terminal bool) error {
	_, err := pool.Exec(ctx, `
		UPDATE seed_runs
		SET state = $2, updated_at = now(), finished_at = CASE WHEN $3 THEN now() ELSE finished_at END
		WHERE seed_run_id = $1`,
		runID, state, terminal)
	if err != nil {
		return fmt.Errorf("mark seed run %s: %w", state, err)
	}
	return nil
}

func persistDobDispositionProofInTx(ctx context.Context, tx pgx.Tx, runID string, entries []dobDisposition) error {
	// R50-005: encoding/json marshals a nil slice as the JSON literal `null`, not `[]`.
	// dobDispositions in seed() is declared `var dobDispositions []dobDisposition` and stays nil
	// when nothing was nulled, so without this normalization a clean run's audit proof would read
	// `"dob_dispositions": null` instead of an honest empty array -- indistinguishable from "this
	// run never recorded a disposition list at all" to a reader of seed_runs.detail.
	if entries == nil {
		entries = []dobDisposition{}
	}
	proof, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("marshal canonical DOB disposition proof: %w", err)
	}
	tag, err := tx.Exec(ctx, `
UPDATE seed_runs
SET detail = jsonb_set(
      jsonb_set(COALESCE(detail, '{}'::jsonb), '{dob_nulled_count}', to_jsonb($2::int), true),
      '{dob_dispositions}', $3::jsonb, true
    ),
    updated_at = now()
WHERE seed_run_id = $1::uuid`, runID, len(entries), proof)
	if err != nil {
		return fmt.Errorf("persist canonical DOB disposition proof: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("persist canonical DOB disposition proof: seed run %s not found", runID)
	}
	return nil
}

// failSeedRun marks a run RESET_REQUIRED. Best-effort: a failure here must not mask the original
// error, but it is logged so a stuck `loading`/`generating` run is never mistaken for healthy.
func failSeedRun(ctx context.Context, pool *pgxpool.Pool, runID string, cause error) {
	if runID == "" {
		return
	}
	msg := ""
	if cause != nil {
		msg = cause.Error()
	}
	if _, err := pool.Exec(ctx, `
		UPDATE seed_runs
		SET state = $2, error = $3, updated_at = now(), finished_at = now()
		WHERE seed_run_id = $1`,
		runID, seedRunStateFailed, msg); err != nil {
		fmt.Printf("WARN: failed to mark seed run %s as %s: %v\n", runID, seedRunStateFailed, err)
	}
}

// insertSourceFactLedger persists the lineage ledger inside the data transaction.
func insertSourceFactLedger(ctx context.Context, tx pgx.Tx, tenantID, seedRunID string, facts []sourceFact) error {
	runRef := nullString(seedRunID)
	return batch(ctx, tx, facts, 500, func(b *pgx.Batch, f sourceFact) {
		var srcDate *time.Time
		if f.sourceDate != nil {
			d := *f.sourceDate
			srcDate = &d
		}
		b.Queue(`
			INSERT INTO vaccination_source_facts (source_fact_id, tenant_id, seed_run_id, lineage_key,
				animal_key, vaccine_header, dose_code, sequence, source_value, source_date, disposition,
				obligation_idem, completion_idem)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
			ON CONFLICT (source_fact_id) DO NOTHING`,
			sourceFactID(tenantID, f.lineageKey), tenantID, runRef, f.lineageKey,
			f.animalKey, f.vaccineHeader, f.doseCode, f.sequence, f.sourceValue, srcDate, f.disposition,
			nullString(f.obligationIdem), nullString(f.completionIdem))
	})
}

// reconcileSourceFactLineage is the VACC-REV-01 hard gate: every dated source fact is accounted for
// EXACTLY ONCE by its lineage identity. It counts distinct lineage keys (not summed completion +
// obligation rows) and asserts each fact carries a known disposition, and — per Fix Plan A1 [P1] —
// FAILS on any unknown vaccine header that carries a date instead of letting it vanish outside the
// denominator.
func reconcileSourceFactLineage(datedFacts int, facts []sourceFact) error {
	if len(facts) != datedFacts {
		return fmt.Errorf("source-fact lineage gap: dated_source_facts=%d ledger_facts=%d — every dated cell must produce exactly one ledger fact", datedFacts, len(facts))
	}
	seen := make(map[string]struct{}, len(facts))
	unknownHeaders := 0
	for _, f := range facts {
		if _, dup := seen[f.lineageKey]; dup {
			return fmt.Errorf("source-fact lineage collision: lineage_key=%q accounted more than once", f.lineageKey)
		}
		seen[f.lineageKey] = struct{}{}
		if f.disposition == dispositionExcludedVaccineUnknown {
			unknownHeaders++
		}
	}
	if unknownHeaders > 0 {
		return fmt.Errorf("source-fact lineage gate [P1]: %d dated cells carry an unknown vaccine header — a renamed/misspelled source column must FAIL the seed, not vanish outside the denominator", unknownHeaders)
	}
	return nil
}

// verifyPersistedSourceFactsInTx is the VACC-REV-02 [P0] in-transaction check. It runs in the SAME
// transaction as the obligation/completion inserts, BEFORE commit, and compares each non-excluded
// ledger fact to the row ACTUALLY persisted (not the intended in-memory count). A silent
// ON CONFLICT DO NOTHING drop leaves an expected idempotency key absent, which this reports and which
// rolls the whole seed transaction back.
func verifyPersistedSourceFactsInTx(ctx context.Context, tx pgx.Tx, tenantID string, facts []sourceFact) error {
	wantObl := map[string]string{}
	wantCmp := map[string]string{}
	for _, f := range facts {
		switch f.disposition {
		case dispositionImportedCompletion:
			if f.obligationIdem != "" {
				wantObl[f.obligationIdem] = f.obligationID
			}
			if f.completionIdem != "" {
				wantCmp[f.completionIdem] = f.completionID
			}
		case dispositionScheduledObligation:
			if f.obligationIdem != "" {
				wantObl[f.obligationIdem] = f.obligationID
			}
		}
	}

	missingObl, err := missingPersistedRows(ctx, tx, `obligation_instances`, `obligation_id`, tenantID, wantObl)
	if err != nil {
		return fmt.Errorf("verify committed obligations: %w", err)
	}
	missingCmp, err := missingPersistedRows(ctx, tx, `vaccination_completions`, `completion_id`, tenantID, wantCmp)
	if err != nil {
		return fmt.Errorf("verify committed completions: %w", err)
	}
	if len(missingObl) > 0 || len(missingCmp) > 0 {
		return fmt.Errorf("in-transaction source-fact drop detected (rolling back): %d obligation rows and %d completion rows expected by source lineage were not persisted (e.g. obl=%s cmp=%s) — a committed seed must reconcile against PERSISTED rows, not intended counts",
			len(missingObl), len(missingCmp), sample(missingObl), sample(missingCmp))
	}
	return nil
}

func verifyActiveGoatsHaveShedInTx(ctx context.Context, tx pgx.Tx, tenantID string) error {
	var offenders int64
	if err := tx.QueryRow(ctx, activeGoatShedInvariantSQL(), tenantID).Scan(&offenders); err != nil {
		return fmt.Errorf("verify active goat shed placement: %w", err)
	}
	if offenders != 0 {
		return fmt.Errorf("active goat shed invariant failed: %d active animals are missing a real shed/park/current shed placement", offenders)
	}
	return nil
}

func verifyActiveGoatsHaveShed(ctx context.Context, pool *pgxpool.Pool, tenantID string) error {
	var offenders int64
	if err := pool.QueryRow(ctx, activeGoatShedInvariantSQL(), tenantID).Scan(&offenders); err != nil {
		return fmt.Errorf("verify active goat shed placement: %w", err)
	}
	if offenders != 0 {
		return fmt.Errorf("active goat shed invariant failed: %d active animals are missing a real shed/park/current shed placement", offenders)
	}
	return nil
}

func activeGoatShedInvariantSQL() string {
	return `
SELECT count(*)
FROM goats g
LEFT JOIN locations shed
  ON shed.tenant_id = g.tenant_id
 AND shed.location_id = g.shed_id
LEFT JOIN locations current_loc
  ON current_loc.tenant_id = g.tenant_id
 AND current_loc.location_id = g.current_location_id
LEFT JOIN locations park
  ON park.tenant_id = g.tenant_id
 AND park.location_id = g.park_id
WHERE g.tenant_id = $1::uuid
  AND g.lifecycle_status NOT IN ('dead', 'sold', 'lost', 'culled', 'transferred', 'merged', 'inactive')
  AND (
    g.shed_id IS NULL
    OR g.park_id IS NULL
    OR g.current_location_id IS NULL
    OR NOT (
      g.current_location_id = g.shed_id
      OR (
        current_loc.location_type = 'pen'
        AND current_loc.status = 'active'
        AND current_loc.parent_location_id = g.shed_id
      )
    )
    OR shed.location_type <> 'shed'
    OR shed.status <> 'active'
    OR park.location_type <> 'park'
    OR park.status <> 'active'
    OR shed.parent_location_id IS DISTINCT FROM g.park_id
  )`
}

func verifyActiveGoatsUsePhysicalShedLocationsInTx(ctx context.Context, tx pgx.Tx, tenantID string) error {
	var offenders int64
	if err := tx.QueryRow(ctx, activeGoatPhysicalShedInvariantSQL(), tenantID).Scan(&offenders); err != nil {
		return fmt.Errorf("verify physical shed placement: %w", err)
	}
	if offenders != 0 {
		return fmt.Errorf("physical shed invariant failed: %d active animals are placed in partition-named canonical shed locations; normalize Gandhi 1 -> shed Gandhi partition 1 and Godel 1 - Part 3 -> shed Godel 1 partition Part 3", offenders)
	}
	if err := tx.QueryRow(ctx, activeGoatPartitionLineageInvariantSQL(), tenantID).Scan(&offenders); err != nil {
		return fmt.Errorf("verify shed partition lineage: %w", err)
	}
	if offenders != 0 {
		return fmt.Errorf("shed partition invariant failed: %d active animals are missing goat_shed_partitions lineage; planner must receive physical shed plus partition, not infer from location names", offenders)
	}
	return nil
}

func verifyActiveGoatsUsePhysicalShedLocations(ctx context.Context, pool *pgxpool.Pool, tenantID string) error {
	var offenders int64
	if err := pool.QueryRow(ctx, activeGoatPhysicalShedInvariantSQL(), tenantID).Scan(&offenders); err != nil {
		return fmt.Errorf("verify physical shed placement: %w", err)
	}
	if offenders != 0 {
		return fmt.Errorf("physical shed invariant failed: %d active animals are placed in partition-named canonical shed locations; normalize Gandhi 1 -> shed Gandhi partition 1 and Godel 1 - Part 3 -> shed Godel 1 partition Part 3", offenders)
	}
	if err := pool.QueryRow(ctx, activeGoatPartitionLineageInvariantSQL(), tenantID).Scan(&offenders); err != nil {
		return fmt.Errorf("verify shed partition lineage: %w", err)
	}
	if offenders != 0 {
		return fmt.Errorf("shed partition invariant failed: %d active animals are missing goat_shed_partitions lineage; planner must receive physical shed plus partition, not infer from location names", offenders)
	}
	return nil
}

func activeGoatPhysicalShedInvariantSQL() string {
	return `
SELECT count(*)
FROM goats g
JOIN locations shed
  ON shed.tenant_id = g.tenant_id
 AND shed.location_id = g.shed_id
WHERE g.tenant_id = $1::uuid
  AND g.lifecycle_status NOT IN ('dead', 'sold', 'lost', 'culled', 'transferred', 'merged', 'inactive')
  AND g.merged_into_goat_id IS NULL
  AND shed.location_type = 'shed'
  AND shed.name ~* ' - Part [0-9]+$'`
}

func activeGoatPartitionLineageInvariantSQL() string {
	return `
SELECT count(*)
FROM goats g
LEFT JOIN goat_shed_partitions gsp
  ON gsp.tenant_id = g.tenant_id
 AND gsp.goat_id = g.goat_id
 AND gsp.shed_id = g.shed_id
WHERE g.tenant_id = $1::uuid
  AND g.lifecycle_status NOT IN ('dead', 'sold', 'lost', 'culled', 'transferred', 'merged', 'inactive')
  AND g.merged_into_goat_id IS NULL
  AND (
    gsp.goat_id IS NULL
    OR btrim(gsp.partition_label) = ''
    OR btrim(gsp.source_shed_name) = ''
  )`
}

func verifyVaccinationObligationsShedScopedInTx(ctx context.Context, tx pgx.Tx, tenantID string) error {
	var offenders int64
	if err := tx.QueryRow(ctx, vaccinationObligationShedScopeInvariantSQL(), tenantID).Scan(&offenders); err != nil {
		return fmt.Errorf("verify vaccination obligation shed scope: %w", err)
	}
	if offenders != 0 {
		return fmt.Errorf("vaccination obligation shed-scope invariant failed: %d active open goat obligations are not scoped to the goat's shed", offenders)
	}
	return nil
}

func verifyVaccinationObligationsShedScoped(ctx context.Context, pool *pgxpool.Pool, tenantID string) error {
	var offenders int64
	if err := pool.QueryRow(ctx, vaccinationObligationShedScopeInvariantSQL(), tenantID).Scan(&offenders); err != nil {
		return fmt.Errorf("verify vaccination obligation shed scope: %w", err)
	}
	if offenders != 0 {
		return fmt.Errorf("vaccination obligation shed-scope invariant failed: %d active open goat obligations are not scoped to the goat's shed", offenders)
	}
	return nil
}

func vaccinationObligationShedScopeInvariantSQL() string {
	return `
SELECT count(*)
FROM obligation_instances oi
JOIN protocol_versions pv
  ON pv.tenant_id = oi.tenant_id
 AND pv.protocol_version_id = oi.protocol_version_id
JOIN protocol_definitions pd
  ON pd.tenant_id = pv.tenant_id
 AND pd.protocol_id = pv.protocol_id
JOIN goats g
  ON g.tenant_id = oi.tenant_id
 AND g.goat_id = oi.target_id
WHERE oi.tenant_id = $1::uuid
  AND pd.category = 'vaccination'
  AND oi.target_type = 'goat'
  AND oi.status NOT IN ('completed', 'canceled', 'superseded', 'waived')
  AND g.lifecycle_status NOT IN ('dead', 'sold', 'lost', 'culled', 'transferred', 'merged', 'inactive')
  AND (oi.scope_type <> 'shed' OR oi.scope_id IS DISTINCT FROM g.shed_id)`
}

func keysOf(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func sample(keys []string) string {
	if len(keys) == 0 {
		return "-"
	}
	return keys[0]
}

// missingIdempotencyKeys returns the subset of want that is NOT present (committed within the tx) in
// the given table for the tenant. Set-based (= ANY) — no per-key round trip.
func missingPersistedRows(ctx context.Context, tx pgx.Tx, table, idColumn, tenantID string, want map[string]string) ([]string, error) {
	if len(want) == 0 {
		return nil, nil
	}
	type expectedRow struct {
		IdempotencyKey string `json:"idempotency_key"`
		RowID          string `json:"row_id"`
	}
	rowsJSON := make([]expectedRow, 0, len(want))
	for key, id := range want {
		rowsJSON = append(rowsJSON, expectedRow{IdempotencyKey: key, RowID: id})
	}
	payload, err := json.Marshal(rowsJSON)
	if err != nil {
		return nil, err
	}
	// table is a fixed internal literal ('obligation_instances' | 'vaccination_completions'), never
	// user input, so this is not an injection surface.
	q := fmt.Sprintf(`
		SELECT e.idempotency_key
		FROM jsonb_to_recordset($2::jsonb) AS e(idempotency_key text, row_id uuid)
		WHERE NOT EXISTS (
			SELECT 1 FROM %s t
			WHERE t.tenant_id = $1::uuid
			  AND (t.idempotency_key = e.idempotency_key OR t.%s = e.row_id)
		)`, table, idColumn)
	rows, err := tx.Query(ctx, q, tenantID, string(payload))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var missing []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		missing = append(missing, k)
	}
	return missing, rows.Err()
}

func detUUID(kind string, parts ...string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("goatos:seed-vaccination-real:"+kind+":"+strings.Join(parts, ":"))).String()
}

func cell(row []interface{}, idx int) string {
	if idx < 0 || idx >= len(row) || row[idx] == nil {
		return ""
	}
	switch v := row[idx].(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return fmt.Sprintf("%.0f", v)
	case int:
		return fmt.Sprintf("%d", v)
	case bool:
		return fmt.Sprintf("%t", v)
	default:
		return ""
	}
}

func optionalCell(row []interface{}, col map[string]int, names ...string) string {
	for _, name := range names {
		if idx, ok := col[name]; ok {
			return cell(row, idx)
		}
	}
	return ""
}

func stringAt(row []interface{}, idx int) string {
	if idx < 0 || idx >= len(row) || row[idx] == nil {
		return ""
	}
	if s, ok := row[idx].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func oldTagIdentifier(oldID string, suffix string) string {
	oldID = strings.TrimSpace(oldID)
	suffix = strings.TrimSpace(suffix)
	if oldID == "" || strings.EqualFold(oldID, "none") || strings.EqualFold(oldID, "na") {
		return ""
	}
	if suffix == "" || strings.EqualFold(suffix, "none") || strings.EqualFold(suffix, "na") {
		return oldID
	}
	return suffix + "-" + oldID
}

func sourceAnimalIdentifier(rfid string, oldID string, suffix string) string {
	rfid = strings.TrimSpace(rfid)
	if rfid != "" {
		return rfid
	}
	return oldTagIdentifier(oldID, suffix)
}

func identifierSlots(rfid string, rfid2 string, oldID string, suffix string) (string, []string) {
	rfid = strings.TrimSpace(rfid)
	rfid2 = strings.TrimSpace(rfid2)
	oldTag := oldTagIdentifier(oldID, suffix)
	if rfid == "" {
		if rfid2 == "" || strings.EqualFold(rfid2, oldTag) {
			return oldTag, nil
		}
		return oldTag, []string{rfid2}
	}
	aliases := make([]string, 0, 2)
	seen := map[string]struct{}{strings.ToLower(rfid): {}}
	for _, alias := range []string{rfid2, oldTag} {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		normalized := strings.ToLower(alias)
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		aliases = append(aliases, alias)
	}
	return rfid, aliases
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func validateTarget(env, databaseURL string) error {
	if strings.EqualFold(strings.TrimSpace(env), "stg") {
		return localtarget.ValidateStagingCloudSQLDatabaseTarget("seed-vaccination-real", env, databaseURL)
	}
	return localtarget.ValidateLocalDatabaseTarget("seed-vaccination-real", env, databaseURL, "local", "dev", "test")
}

func distinct[T any](items []T, key func(T) string) []string {
	seen := map[string]bool{}
	var out []string
	for _, it := range items {
		k := key(it)
		if k != "" && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

func distinctShedKeys(goats []goatRecord) []shedKey {
	seen := map[shedKey]bool{}
	var out []shedKey
	for _, g := range goats {
		k := seedPlacementKey(g)
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

func seedPlacementKey(g goatRecord) shedKey {
	return shedKey{farm: seedFarm(g), shed: seedPhysicalShed(g)}
}

func seedFarm(g goatRecord) string {
	farm := strings.TrimSpace(g.Farm)
	if farm == "" {
		return seedFallbackFarm
	}
	return farm
}

func seedShed(g goatRecord) string {
	shed := strings.TrimSpace(g.Shed)
	if shed == "" {
		return seedFallbackShed
	}
	return shed
}

func seedPhysicalShed(g goatRecord) string {
	physical, _ := normalizeSeedShedPartition(seedShed(g))
	if strings.TrimSpace(physical) == "" {
		return seedFallbackShed
	}
	return physical
}

func normalizeSeedShedPartition(raw string) (physicalShed, partition string) {
	name := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if name == "" {
		return "", "whole"
	}
	if physical, part, ok := splitSeedPartSuffix(name); ok {
		return physical, part
	}
	parts := strings.Fields(name)
	if len(parts) >= 2 {
		last := parts[len(parts)-1]
		if _, err := strconv.Atoi(last); err == nil {
			physical := strings.TrimSpace(strings.Join(parts[:len(parts)-1], " "))
			if physical != "" {
				return physical, last
			}
		}
	}
	return name, "whole"
}

func splitSeedPartSuffix(name string) (string, string, bool) {
	lower := strings.ToLower(name)
	marker := " - part "
	idx := strings.LastIndex(lower, marker)
	if idx < 0 {
		return "", "", false
	}
	physical := strings.TrimSpace(name[:idx])
	partition := strings.TrimSpace(name[idx+len(marker):])
	if physical == "" || partition == "" {
		return "", "", false
	}
	if _, err := strconv.Atoi(partition); err != nil {
		return "", "", false
	}
	return physical, "Part " + partition, true
}

func seedLocationCode(name string) string {
	code := strings.ToUpper(strings.TrimSpace(name))
	code = strings.Map(func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		default:
			return '_'
		}
	}, code)
	for strings.Contains(code, "__") {
		code = strings.ReplaceAll(code, "__", "_")
	}
	code = strings.Trim(code, "_")
	if code == "" {
		return seedFallbackFarm
	}
	return code
}

func shedCode(farm, shed string) string {
	slug := seedLocationCode(shed)
	if slug == seedFallbackFarm {
		slug = "SHED"
	}
	return seedLocationCode(farm) + "_SHED_" + slug
}

func retireActiveNonSourceLocations(ctx context.Context, tx pgx.Tx, tenantID string, sourceParkCodes []string, sourceSheds []shedKey) (int, error) {
	codes := make([]string, 0, len(sourceParkCodes))
	seen := map[string]struct{}{}
	for _, code := range sourceParkCodes {
		code = strings.ToUpper(strings.TrimSpace(code))
		if code == "" {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		codes = append(codes, code)
	}
	if len(codes) == 0 {
		return 0, nil
	}
	shedPairs := make([]string, 0, len(sourceSheds))
	seenSheds := map[string]struct{}{}
	for _, shed := range sourceSheds {
		parkCode := seedLocationCode(shed.farm)
		shedName := strings.ToLower(strings.TrimSpace(shed.shed))
		if parkCode == "" || shedName == "" {
			continue
		}
		pair := parkCode + "||" + shedName
		if _, ok := seenSheds[pair]; ok {
			continue
		}
		seenSheds[pair] = struct{}{}
		shedPairs = append(shedPairs, pair)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('goatos.approved_location_migration_plan', 'vaccination source-owned local/dev non-source location retire', true)`); err != nil {
		return 0, err
	}
	var retired int
	err := tx.QueryRow(ctx, `
			WITH source_parks AS (
			SELECT location_id
			FROM locations
			WHERE tenant_id = $1::uuid
			  AND location_type = 'park'
				  AND upper(location_code) = ANY($2::text[])
			),
			source_sheds AS (
				SELECT split_part(v, '||', 1) AS park_code,
				       split_part(v, '||', 2) AS shed_name
				FROM unnest($3::text[]) AS source(v)
			),
			stale_parks AS (
			SELECT p.location_id
			FROM locations p
			WHERE p.tenant_id = $1::uuid
			  AND p.location_type = 'park'
			  AND p.status = 'active'
			  AND NOT EXISTS (SELECT 1 FROM source_parks sp WHERE sp.location_id = p.location_id)
			  AND NOT EXISTS (
			    SELECT 1 FROM goats g
			    WHERE g.tenant_id = p.tenant_id
			      AND g.park_id = p.location_id
			      AND g.lifecycle_status IN ('alive','sick','under_treatment','quarantine','icu')
				  )
			),
			stale_source_park_sheds AS (
				SELECT s.location_id
				FROM locations s
				JOIN locations p ON p.location_id = s.parent_location_id AND p.tenant_id = s.tenant_id
				WHERE s.tenant_id = $1::uuid
				  AND s.location_type = 'shed'
				  AND s.status = 'active'
				  AND p.location_type = 'park'
				  AND upper(p.location_code) = ANY($2::text[])
				  AND NOT EXISTS (
				  	SELECT 1 FROM source_sheds src
				  	WHERE src.park_code = upper(p.location_code)
				  	  AND src.shed_name = lower(s.name)
				  )
				  AND NOT EXISTS (
				    SELECT 1 FROM goats g
				    WHERE g.tenant_id = s.tenant_id
				      AND g.shed_id = s.location_id
				      AND g.lifecycle_status IN ('alive','sick','under_treatment','quarantine','icu')
				  )
			),
			retired_sheds AS (
				UPDATE locations s
				SET status = 'inactive', updated_at = now(), row_version = row_version + 1
				WHERE s.tenant_id = $1::uuid
				  AND s.location_type = 'shed'
				  AND s.status = 'active'
				  AND (
				  	s.parent_location_id IN (SELECT location_id FROM stale_parks)
				  	OR s.location_id IN (SELECT location_id FROM stale_source_park_sheds)
				  )
				  AND NOT EXISTS (
			    SELECT 1 FROM goats g
			    WHERE g.tenant_id = s.tenant_id
			      AND g.shed_id = s.location_id
			      AND g.lifecycle_status IN ('alive','sick','under_treatment','quarantine','icu')
			  )
			RETURNING 1
		),
		retired_parks AS (
			UPDATE locations p
			SET status = 'inactive', updated_at = now(), row_version = row_version + 1
			WHERE p.location_id IN (SELECT location_id FROM stale_parks)
			RETURNING 1
			)
			SELECT (SELECT count(*) FROM retired_sheds) + (SELECT count(*) FROM retired_parks)
		`, tenantID, codes, shedPairs).Scan(&retired)
	if err != nil {
		return 0, err
	}
	return retired, nil
}

func normalizeSex(g string) string {
	switch strings.ToLower(strings.TrimSpace(g)) {
	case "female", "f":
		return "female"
	default:
		return "male"
	}
}

func normalizeBreed(b string) string {
	for _, known := range []string{"Beetal", "Sojat", "Boer", "Malai", "Sirohi", "Osmanabadi", "Alpine", "Saanen", "Jamnapari", "Barbari", "Kota"} {
		if strings.EqualFold(strings.TrimSpace(b), known) {
			return known
		}
	}
	if strings.TrimSpace(b) == "" {
		return "Sojat"
	}
	return strings.TrimSpace(b)
}

func seedBreedKey(species, breed string) string {
	return strings.ToLower(strings.TrimSpace(species)) + "\x00" + strings.ToLower(strings.TrimSpace(breed))
}

// ensureSeedBreeds makes the source breed vocabulary a real species-scoped FK
// before goats are inserted. The text column remains for display/backward
// compatibility, but every reviewed source breed must also resolve through the
// canonical breeds table so sheep cannot inherit a goat-only NULL breed_id.
func ensureSeedBreeds(ctx context.Context, tx pgx.Tx, goats []goatRecord) (map[string]string, error) {
	ids := make(map[string]string)
	for _, g := range goats {
		species := deriveSeedSpecies(g.Species, g.Breed)
		breed := normalizeBreed(g.Breed)
		key := seedBreedKey(species, breed)
		if _, ok := ids[key]; ok {
			continue
		}
		var breedID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO breeds (species, canonical_name, status, created_at, updated_at)
			VALUES ($1,$2,'active',now(),now())
			ON CONFLICT (species, canonical_name) DO UPDATE SET status='active', updated_at=now()
			RETURNING breed_id`, species, breed).Scan(&breedID); err != nil {
			return nil, fmt.Errorf("ensure source breed %s/%s: %w", species, breed, err)
		}
		ids[key] = breedID
	}
	return ids, nil
}

func deriveSeedSpecies(speciesHint, breed string) string {
	switch strings.ToLower(strings.TrimSpace(speciesHint)) {
	case "sheep", "ovine", "ewe", "ram", "lamb":
		return "sheep"
	case "goat", "caprine", "doe", "buck", "kid":
		return "goat"
	}
	if strings.Contains(strings.ToLower(strings.TrimSpace(breed)), "sheep") {
		return "sheep"
	}
	return "goat"
}

func normalizeOriginType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "birth", "born", "bred":
		return "birth"
	case "purchase", "procured", "bought":
		return "procured"
	case "import", "imported":
		return "imported"
	default:
		return ""
	}
}

func normalizeLifecycle(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "dead", "died", "death", "mortality":
		return "dead"
	case "sold", "sale":
		return "sold"
	case "culled":
		return "culled"
	case "transferred":
		return "transferred"
	case "lost", "missing":
		return "lost"
	default:
		return "alive"
	}
}

// excludedLifecycle reports whether a lifecycle_status makes a goat ineligible for
// NEW open vaccination obligations (mirrors vw_procurement_vaccination_excluded_goats).
func excludedLifecycle(s string) bool {
	switch s {
	case "dead", "sold", "lost", "culled", "transferred", "merged", "inactive":
		return true
	default:
		return false
	}
}

// normalizeHealth maps the source health signal onto the real health_status domain
// (healthy, sick, under_treatment, recovering, quarantine, icu — goats_health_status_check).
// Fix Plan A2: the sheet's health_status column is a vet CASE-LOG state (Open/Closed/
// Extended), not the clinical status name directly, so those values must not fall through
// to NULL:
//   - "Open"     — a currently active, unresolved case            -> sick
//   - "Extended" — the case ran past its expected close, treatment
//     continues                                                    -> under_treatment
//   - "Closed"   — the case has been resolved                     -> healthy
//   - "Fine"     — explicit healthy source status                  -> healthy
func normalizeHealth(h string) *string {
	switch strings.ToLower(strings.TrimSpace(h)) {
	case "healthy", "normal", "ok", "closed", "fine":
		v := "healthy"
		return &v
	case "sick", "ill", "diseased", "open":
		v := "sick"
		return &v
	case "under_treatment", "under treatment", "treatment", "extended":
		v := "under_treatment"
		return &v
	case "recovering":
		v := "recovering"
		return &v
	case "quarantine":
		v := "quarantine"
		return &v
	case "icu":
		v := "icu"
		return &v
	default:
		return nil
	}
}

// shedTagClinicalSignal maps a source shed_tag to a clinical (health_status) and/or
// reproductive (reproductive_status) signal (Fix Plan A3/A4). shed_tag also carries pure
// growth-cohort/management-stage groupings (K0-K3, F2/F2-Male/F2-Female, Buck, Mother,
// Milking, M0, Warmup) that are location/stage information already captured by the
// stage/age columns — those are intentionally left unmapped here since they are not
// clinical/reproductive signals, only clinical/reproductive/quarantine tags are.
func shedTagClinicalSignal(shedTag string) (health *string, reproductive *string) {
	switch strings.ToLower(strings.TrimSpace(shedTag)) {
	case "icu":
		return nullString("icu"), nil
	case "icu-kid":
		return nullString("icu"), nil
	case "icu-non-pregnant":
		// Compound legacy tag: current placement is BOTH clinical (ICU) and
		// reproductive (non-pregnant); split onto both axes.
		return nullString("icu"), nullString("non_pregnant")
	case "quarantine kids", "quarantine":
		return nullString("quarantine"), nil
	case "pregnant":
		return nil, nullString("pregnant")
	case "non-pregnant":
		return nil, nullString("non_pregnant")
	default:
		return nil, nil
	}
}

// resolveGoatHealth combines the source health_status case-log column with the shed_tag
// current-placement signal (Fix Plan A2 + A3). shed_tag reflects where the goat is housed
// RIGHT NOW (e.g. still in the ICU/quarantine shed), a stronger and more current clinical
// signal than a closed/extended historical case-log entry, so it wins when both are present.
func resolveGoatHealth(sourceHealth string, shedTag string) *string {
	if shedTagHealth, _ := shedTagClinicalSignal(shedTag); shedTagHealth != nil {
		return shedTagHealth
	}
	return normalizeHealth(sourceHealth)
}

// resolveGoatReproductiveStatus imports reproductive_status from the shed_tag signal
// (Fix Plan A4) — the source has no standalone reproductive_status column, only shed_tag
// values like Pregnant/Non-Pregnant/ICU-Non-Pregnant.
func resolveGoatReproductiveStatus(shedTag string) *string {
	_, reproductive := shedTagClinicalSignal(shedTag)
	return reproductive
}

func normalizeStage(stage string, age string) string {
	if s := strings.TrimSpace(stage); s != "" {
		switch strings.ToLower(s) {
		case "unknown", "na", "n/a", "null", "-":
		default:
			return s
		}
	}
	switch strings.ToLower(strings.TrimSpace(age)) {
	case "kid", "kids", "k1", "k2":
		return "Kid"
	default:
		return "Adult"
	}
}

// stagesMatch checks if two stage strings represent the same stage category (kid vs adult).
// Used to detect contradictions between source-provided stage and age-derived stage.
func stagesMatch(sourceStage, derivedStage string) bool {
	// Both must be non-empty for a meaningful comparison
	if sourceStage == "" || derivedStage == "" {
		return true // No contradiction if either is empty
	}
	sourceKid := isKidStage(sourceStage)
	derivedKid := isKidStage(derivedStage)
	return sourceKid == derivedKid
}

// isKidStage reports whether a stage string indicates a kid/young-animal stage.
// Kid stages start with 'K' (K0, K1, K2, K3, etc).
func isKidStage(stage string) bool {
	normalized := strings.ToUpper(strings.TrimSpace(stage))
	return len(normalized) >= 1 && normalized[0] == 'K'
}

func nullString(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

func resolveVaccinationSOPVersion(ctx context.Context, tx pgx.Tx, tenantID string) (string, error) {
	var sopVersionID, formDSL, proofPolicy string
	err := tx.QueryRow(ctx, `
		SELECT sv.sop_version_id::text, sv.form_dsl::text, sv.proof_policy::text
		FROM sop_versions sv
		JOIN sop_definitions sd
		  ON sd.tenant_id = sv.tenant_id
		 AND sd.sop_id = sv.sop_id
		WHERE sv.tenant_id = $1::uuid
		  AND sd.code = 'vaccination.drive'
		  AND sv.status = 'published'
		ORDER BY sv.version DESC, sv.updated_at DESC
		LIMIT 1`, tenantID).Scan(&sopVersionID, &formDSL, &proofPolicy)
	if err != nil {
		return "", fmt.Errorf("resolve published vaccination.drive SOP version: %w", err)
	}
	if err := validateVaccinationSOPContract(formDSL, proofPolicy); err != nil {
		return "", fmt.Errorf("published vaccination.drive SOP %s violates the Android vaccination execution contract: %w", sopVersionID, err)
	}
	return sopVersionID, nil
}

func validateVaccinationSOPContract(formDSLJSON, proofPolicyJSON string) error {
	var form struct {
		Fields []struct {
			Key          string `json:"key"`
			Type         string `json:"type"`
			Required     bool   `json:"required"`
			Repeat       bool   `json:"repeat"`
			ProofSubject string `json:"proof_subject"`
		} `json:"fields"`
		RepeatForEachGoat struct {
			ItemKey     string `json:"item_key"`
			SourceField string `json:"source_field"`
		} `json:"repeat_for_each_goat"`
	}
	if err := json.Unmarshal([]byte(formDSLJSON), &form); err != nil {
		return fmt.Errorf("invalid form_dsl JSON: %w", err)
	}
	var hasGoatScan, hasShedVideo bool
	for _, field := range form.Fields {
		if field.Key == "goat_ids" && field.Type == "goat_scan" && field.Required && field.Repeat {
			hasGoatScan = true
		}
		if field.Key == "shed_video" && field.Type == "video_proof" && field.Required && field.Repeat && field.ProofSubject == "shed" {
			hasShedVideo = true
		}
	}
	if !hasGoatScan || !hasShedVideo {
		return fmt.Errorf("form_dsl must contain required repeat goat_ids/goat_scan and shed_video/video_proof fields")
	}
	if form.RepeatForEachGoat.ItemKey != "goat_id" || form.RepeatForEachGoat.SourceField != "goat_ids" {
		return fmt.Errorf("form_dsl repeat_for_each_goat must bind goat_id to goat_ids")
	}
	var proof struct {
		Types                   []string `json:"types"`
		Required                bool     `json:"required"`
		ProofMode               string   `json:"proof_mode"`
		SubjectScope            string   `json:"subject_scope"`
		ExpectedSubjects        []string `json:"expected_subjects"`
		MinimumCount            int      `json:"minimum_count"`
		MaximumCount            int      `json:"maximum_count"`
		MinimumCountPerSubject  int      `json:"minimum_count_per_subject"`
		MaximumCountPerSubject  int      `json:"maximum_count_per_subject"`
		CaptureSource           string   `json:"capture_source"`
		AllowedCaptureSources   []string `json:"allowed_capture_sources"`
		OneClipSameHandlingGoat bool     `json:"one_clip_covers_same_handling_vaccines"`
		VerifyCapability        string   `json:"verify_capability"`
		VerifyBeforeApply       bool     `json:"verify_before_apply"`
		RetentionPolicy         string   `json:"retention_policy"`
	}
	if err := json.Unmarshal([]byte(proofPolicyJSON), &proof); err != nil {
		return fmt.Errorf("invalid proof_policy JSON: %w", err)
	}
	if len(proof.Types) != 1 || proof.Types[0] != "video" || !proof.Required || proof.ProofMode != "shed_level_video" ||
		proof.SubjectScope != "shed" || len(proof.ExpectedSubjects) != 1 || proof.ExpectedSubjects[0] != "shed" ||
		proof.MinimumCount != 1 || proof.MaximumCount != 5 || proof.MaximumCountPerSubject != 5 ||
		proof.CaptureSource != "in_app_camera" || !stringSliceHas(proof.AllowedCaptureSources, "in_app_camera") ||
		!stringSliceHas(proof.AllowedCaptureSources, "gallery_picker") ||
		proof.VerifyCapability != "proof.verify" || !proof.VerifyBeforeApply ||
		proof.RetentionPolicy != "operational_90d" {
		return fmt.Errorf("proof_policy must require 1..5 shed-level video clips, allow camera/gallery sources, and require verifier approval before apply")
	}
	return nil
}

func stringSliceHas(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func nextProtocolVersion(ctx context.Context, tx pgx.Tx, tenantID, protocolID string) (int, error) {
	var nextVersion int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(version), 0) + 1
		FROM protocol_versions
		WHERE tenant_id=$1
		  AND protocol_id=$2
		  AND scope_type='tenant'
		  AND scope_id IS NULL`,
		tenantID, protocolID).Scan(&nextVersion); err != nil {
		return 0, fmt.Errorf("next vaccination matrix version: %w", err)
	}
	return nextVersion, nil
}

// seedQuerier is the read seam shared by pgx.Tx and *pgxpool.Pool so the ownership-safety guard can
// run inside the seed's config transaction and be exercised directly from tests.
type seedQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// reconcileSeedMatrixDraftVersion resolves the seed's single canonical vaccination-matrix version to
// publish, WITHOUT churning config on replay: it reuses the current non-retired version when the
// authored rule_dsl + SOP already match, refreshes it in place while still a draft, and only mints a
// NEW version when the published matrix genuinely changed (a real correction). It never retires or
// creates versions itself — publishing (retire-overlap) is the caller's next step. Extracted from the
// seed's config transaction so a Postgres regression can drive it twice and assert no version churn.
func reconcileSeedMatrixDraftVersion(ctx context.Context, tx pgx.Tx, tenantID, protocolID, ruleDSL, sopVersionID, seedActorID string) (string, error) {
	// Back-stamp any legacy seed DRAFT (seed label, no provenance yet) with the seed actor before we
	// adopt it, so an adopted draft is never published without its provenance stamp (VAX-SEED-R1).
	// Drafts are mutable; a NULL author under the seed label is only ever the seed's own pre-provenance
	// draft (Config authoring always stamps drafted_by with the acting user). PUBLISHED versions are
	// immutable and cannot be back-stamped — an overlapping published version whose author is not the
	// seed actor (including a NULL author that cannot be positively identified) is treated as an
	// ownership conflict by the publish guard rather than retired.
	if _, err := tx.Exec(ctx, `
		UPDATE protocol_versions SET drafted_by=$1::uuid, updated_at=now()
		WHERE tenant_id=$2::uuid AND protocol_id=$3::uuid AND scope_type='tenant' AND scope_id IS NULL
		  AND version_label=$4 AND status='draft' AND drafted_by IS NULL`,
		seedActorID, tenantID, protocolID, matrixVersionLabel); err != nil {
		return "", fmt.Errorf("stamp legacy seed matrix drafts: %w", err)
	}

	versionID := detUUID("protocol_version", tenantID, "vaccination_matrix", "v1_real")
	var status, existingRuleDSL, existingProofPolicy, existingSOPVersionID string
	var existingVersion int
	// Only ever adopt/refresh versions positively identified as seed-owned (drafted_by = seed actor);
	// after the draft back-stamp above this includes legacy seed drafts. A user-authored version that
	// shares the seed label (drafted_by = a real user) and any unidentifiable NULL row are never
	// adopted here; the publish guard refuses to retire them.
	qerr := tx.QueryRow(ctx, `
		SELECT protocol_version_id, status, version, rule_dsl::text, proof_policy::text, COALESCE(sop_version_id::text, '')
		FROM protocol_versions
		WHERE tenant_id=$1
		  AND protocol_id=$2
		  AND scope_type='tenant'
		  AND scope_id IS NULL
		  AND version_label=$3
		  AND status <> 'retired'
		  AND drafted_by=$4::uuid
		ORDER BY version DESC
		LIMIT 1`,
		tenantID, protocolID, matrixVersionLabel, seedActorID).Scan(&versionID, &status, &existingVersion, &existingRuleDSL, &existingProofPolicy, &existingSOPVersionID)
	matrixChanged := qerr == nil && (!jsonSemanticallyEqual(existingRuleDSL, ruleDSL) ||
		!jsonSemanticallyEqual(existingProofPolicy, vaccinationMatrixProofPolicy) || existingSOPVersionID != sopVersionID)
	if qerr == nil && status == "published" && matrixChanged {
		qerr = pgx.ErrNoRows
	}
	if qerr == pgx.ErrNoRows {
		nextVersion, err := nextProtocolVersion(ctx, tx, tenantID, protocolID)
		if err != nil {
			return "", err
		}
		versionID = detUUID("protocol_version", tenantID, "vaccination_matrix", "v1_real", fmt.Sprintf("%d", nextVersion))
		if _, err := tx.Exec(ctx, `
			INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, scope_id, version,
				version_label, status, effective_from, effective_to, rule_dsl, proof_policy, sop_version_id, drafted_by)
			VALUES ($1,$2,$3,'tenant',NULL,$4,$5,'draft',DATE '2026-01-01',NULL,$6::jsonb,$7::jsonb,$8::uuid,$9::uuid)`,
			versionID, tenantID, protocolID, nextVersion, matrixVersionLabel, ruleDSL, vaccinationMatrixProofPolicy, sopVersionID, seedActorID); err != nil {
			return "", fmt.Errorf("protocol version vaccination matrix: %w", err)
		}
	} else if qerr != nil {
		return "", fmt.Errorf("lookup vaccination matrix version: %w", qerr)
	} else if status == "draft" && matrixChanged {
		if _, err := tx.Exec(ctx, `
			UPDATE protocol_versions
			SET rule_dsl=$1::jsonb,
			    proof_policy=$2::jsonb,
			    sop_version_id=$3::uuid,
			    drafted_by=$6::uuid,
			    updated_at=now(),
			    row_version=row_version+1
			WHERE tenant_id=$4::uuid
			  AND protocol_version_id=$5::uuid
			  AND status='draft'`,
			ruleDSL, vaccinationMatrixProofPolicy, sopVersionID, tenantID, versionID, seedActorID); err != nil {
			return "", fmt.Errorf("refresh vaccination matrix draft: %w", err)
		}
	} else {
		_ = existingVersion
	}
	return versionID, nil
}

// foreignPublishedVaccinationMatrices returns identifiers for any PUBLISHED, tenant-scoped,
// matrix-shaped vaccination version — under ANY protocol, including the seed's own canonical
// vaccination.matrix protocol — whose window overlaps the seed's ([2026-01-01, ∞)) and that is NOT
// seed-owned. Ownership is decided by explicit provenance (drafted_by = seedActorID), NOT by
// protocol_id: Config authoring publishes under the SAME canonical protocol, so a protocol_id check
// misses user versions. Legacy seed rows (drafted_by IS NULL under the seed label) are tolerated here
// because reconcileSeedMatrixDraftVersion claims them just before publish; a genuine other-author row
// (drafted_by set to a non-seed actor) is flagged so the seed refuses rather than overwrite it — as is
// any published overlap whose author cannot be positively identified as the seed (drafted_by NULL),
// since published versions are immutable and cannot be back-stamped. This is the friendly early check;
// the authoritative TOCTOU-safe enforcement is the atomic guard inside the publish transaction
// (assertOnlySeedOwnedMatrixOverlapsTx).
func foreignPublishedVaccinationMatrices(ctx context.Context, q seedQuerier, tenantID, seedActorID string) ([]string, error) {
	rows, err := q.Query(ctx, `
SELECT pv.protocol_version_id::text, pd.code, COALESCE(pv.version_label, '')
FROM protocol_versions pv
JOIN protocol_definitions pd ON pd.tenant_id = pv.tenant_id AND pd.protocol_id = pv.protocol_id
WHERE pv.tenant_id = $1::uuid
  AND pv.status = 'published'
  AND pv.scope_type = 'tenant'
  AND pv.scope_id IS NULL
  AND pd.category = 'vaccination'
  AND daterange(pv.effective_from, pv.effective_to, '[)') && daterange(DATE '2026-01-01', NULL, '[)')
  AND (
    lower(COALESCE(pv.rule_dsl->>'ruleset_family', '')) = 'vaccination.matrix'
    OR lower(COALESCE(pv.rule_dsl->'vaccine'->>'code', '')) = 'vaccination.matrix'
    OR pv.rule_dsl ? 'matrix_rows'
  )
  AND pv.drafted_by IS DISTINCT FROM $2::uuid
ORDER BY pd.code, pv.protocol_version_id`, tenantID, seedActorID)
	if err != nil {
		return nil, fmt.Errorf("query non-seed-owned vaccination matrices: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var versionID, code, label string
		if err := rows.Scan(&versionID, &code, &label); err != nil {
			return nil, fmt.Errorf("scan non-seed-owned vaccination matrix: %w", err)
		}
		out = append(out, fmt.Sprintf("%s (protocol %s, label %q)", versionID, code, label))
	}
	return out, rows.Err()
}

func jsonSemanticallyEqual(a, b string) bool {
	var left, right any
	if json.Unmarshal([]byte(a), &left) != nil || json.Unmarshal([]byte(b), &right) != nil {
		return strings.TrimSpace(a) == strings.TrimSpace(b)
	}
	la, err := json.Marshal(left)
	if err != nil {
		return false
	}
	rb, err := json.Marshal(right)
	if err != nil {
		return false
	}
	return string(la) == string(rb)
}

func vaccinationMatrixRuleDSL() (string, error) {
	// Reject malformed vaccine metadata before building the published matrix so a
	// wrong pathogen_class (e.g. a leaked vaccine-type value) can never reach the
	// compatibility engine as seeded truth (BUG1).
	if err := validateVaccineClassifications(); err != nil {
		return "", fmt.Errorf("vaccination seed matrix: %w", err)
	}
	type scheduleRow struct {
		DoseCode            string  `json:"dose_code"`
		SourceDoseCode      string  `json:"source_dose_code"`
		Sequence            int     `json:"sequence"`
		TriggerType         string  `json:"trigger_type"`
		OffsetDays          int     `json:"offset_days"`
		DueWindowDays       int     `json:"due_window_days"`
		DoseAmount          float64 `json:"dose_amount"`
		DoseUnit            string  `json:"dose_unit"`
		RouteSite           string  `json:"route_site"`
		VialDoses           int     `json:"vial_doses"`
		ScheduleNote        string  `json:"schedule_note,omitempty"`
		MaxDelayDays        int     `json:"max_delay_days"`
		CourseLapsePolicy   string  `json:"course_lapse_policy"`
		MinGapDays          int     `json:"min_gap_days"`
		Repeat              string  `json:"repeat"`
		RepeatUntilAfterAge string  `json:"repeat_until_after_age"`
		CatchUp             string  `json:"catch_up"`
	}

	type vaccineMeta struct {
		Code               string `json:"code"`
		Name               string `json:"name"`
		Type               string `json:"type"`
		Disease            string `json:"disease"`
		CompatibilityGroup string `json:"compatibility_group"`
		PathogenClass      string `json:"pathogen_class"`
		CourseType         string `json:"course_type"`
		Priority           int    `json:"priority"`
	}

	type matrixRow struct {
		RowID       string         `json:"row_id"`
		Vaccine     vaccineMeta    `json:"vaccine"`
		Species     []string       `json:"species"`
		Eligibility map[string]any `json:"eligibility"`
		Schedule    []scheduleRow  `json:"schedule"`
	}

	// Build the canonical vaccination matrix per spec. CPT's ET+TT-only validation seed can publish
	// a packet-scoped subset, while source/history mapping above keeps using the full canonical
	// matrix so dated facts still reconcile against reviewed vaccine names.
	vaccMatrixDef := buildSeedPublicationVaccinationMatrix()

	rows := make([]matrixRow, 0)
	schedule := []scheduleRow{}
	sequenceCounter := 0

	for _, vaccName := range vaccineOrder {
		def := vaccines[vaccName]
		spec := vaccMatrixDef[vaccName]

		rowSchedule := []scheduleRow{}

		// birth_age doses (kid path)
		for _, wave := range spec.BirthAgeWaves {
			sequenceCounter++
			dose := wave.DoseCode
			cell := scheduleRow{
				DoseCode:            dose,
				SourceDoseCode:      dose,
				Sequence:            sequenceCounter,
				TriggerType:         "birth_age",
				OffsetDays:          wave.Days,
				DueWindowDays:       7,
				DoseAmount:          def.DoseML,
				DoseUnit:            "ml",
				RouteSite:           "subcutaneous",
				VialDoses:           def.VialDoses,
				ScheduleNote:        "real vaccination seed source matrix",
				MaxDelayDays:        7,
				CourseLapsePolicy:   "preventive_care_review",
				MinGapDays:          wave.MinGapDays,
				Repeat:              "none",
				RepeatUntilAfterAge: "-",
				CatchUp:             "immediate",
			}
			rowSchedule = append(rowSchedule, cell)
			schedule = append(schedule, cell)
		}

		// Adult course doses. Adult entry_date is never a vaccination due-date
		// anchor; blank-history adult dose 1 joins campaign/catch-up cohorts packed
		// by physical shed/partition, while later course doses (for example ET+TT
		// W2) are anchored to accepted prior-dose history.
		for waveIdx, wave := range spec.PostArrivalWaves {
			sequenceCounter++
			dose := fmt.Sprintf("%s_adult_w%d", def.Code, waveIdx+1)
			triggerType := "manual_campaign"
			if waveIdx > 0 {
				triggerType = "after_previous_completion"
			}
			cell := scheduleRow{
				DoseCode:            dose,
				SourceDoseCode:      dose,
				Sequence:            sequenceCounter,
				TriggerType:         triggerType,
				OffsetDays:          wave.Days,
				DueWindowDays:       7,
				DoseAmount:          def.DoseML,
				DoseUnit:            "ml",
				RouteSite:           "subcutaneous",
				VialDoses:           def.VialDoses,
				ScheduleNote:        "real vaccination seed source matrix",
				MaxDelayDays:        7,
				CourseLapsePolicy:   "preventive_care_review",
				MinGapDays:          wave.MinGapDays,
				Repeat:              "none",
				RepeatUntilAfterAge: "-",
				CatchUp:             "immediate",
			}
			rowSchedule = append(rowSchedule, cell)
			schedule = append(schedule, cell)
		}

		// revacc (after_previous_completion)
		if spec.RevaccinationDays > 0 {
			sequenceCounter++
			dose := def.Code + "_revac"
			cell := scheduleRow{
				DoseCode:       dose,
				SourceDoseCode: dose,
				Sequence:       sequenceCounter,
				TriggerType:    "after_previous_completion",
				OffsetDays:     spec.RevaccinationDays,
				DueWindowDays:  30,
				DoseAmount:     def.DoseML,
				DoseUnit:       "ml",
				RouteSite:      "subcutaneous",
				VialDoses:      def.VialDoses,
				ScheduleNote:   "real vaccination seed source matrix",
				// Repeat rows expose a 30-day due window, so the protocol's
				// publishability invariant requires the maximum allowed delay to
				// cover that full window as well.
				MaxDelayDays:        30,
				CourseLapsePolicy:   "preventive_care_review",
				MinGapDays:          spec.RevaccinationDays,
				Repeat:              "every_n_days",
				RepeatUntilAfterAge: "-",
				// Imported accepted history is a cutover anchor. If one or more old
				// repeat cycles have already elapsed, the kernel advances to the next
				// future cycle instead of turning the imported administration into a
				// synthetic late card at seed time. Primary/booster rows keep their
				// immediate catch-up behavior.
				CatchUp: "next_cycle",
			}
			rowSchedule = append(rowSchedule, cell)
			schedule = append(schedule, cell)
		}

		courseType := "single"
		if len(spec.BirthAgeWaves) > 1 || len(spec.PostArrivalWaves) > 1 {
			courseType = "booster"
		}

		rows = append(rows, matrixRow{
			RowID: "real-seed-" + def.Code,
			Vaccine: vaccineMeta{
				Code:               strings.ToUpper(def.Code),
				Name:               def.Name,
				Type:               def.Type,
				Disease:            def.Disease,
				CompatibilityGroup: strings.ToUpper(def.Code),
				PathogenClass:      def.Pathogen,
				CourseType:         courseType,
				Priority:           vaccineDrivePriority[vaccName],
			},
			Species:     spec.Species,
			Eligibility: vaccinationSeedEligibilityForSpecies(spec.Species),
			Schedule:    rowSchedule,
		})
	}

	payload := map[string]any{
		"category":       "vaccination",
		"ruleset_family": "vaccination.matrix",
		"vaccine": map[string]string{
			"code": "vaccination.matrix",
			"name": "Preventive Care Vaccination Matrix",
			"type": "matrix",
		},
		"eligibility": vaccinationSeedEligibility(),
		"missed_dose_policy": map[string]any{
			"nearby_drive_align_days":           14,
			"materialize_only_future_open_work": true,
		},
		"compatibility_policy": map[string]any{
			"live_to_killed_gap_days":            14,
			"killed_to_killed_gap_days":          14,
			"live_to_live_gap_days":              28,
			"kid_booster_min_gap_days":           21,
			"bacterial_viral_same_day_allowed":   true,
			"live_killed_viral_same_day_allowed": true,
			"max_vaccines_per_combo_session":     2,
		},
		"procurement_policy": map[string]any{
			"warmup_no_vaccination_days":       7,
			"kids_normal_schedule_until_weeks": seedKidsNormalScheduleUntilWeeks,
			"adult_prior_vaccination_allowed":  true,
			"first_wave":                       []string{"ET+TT", "PPR"},
			"second_wave_after_days":           28,
			"goat_second_wave":                 []string{"Goat Pox"},
			"sheep_second_wave":                []string{"Sheep Pox"},
		},
		"pregnancy_policy": map[string]any{
			"allow_until_pregnancy_month":  3,
			"skip_from_pregnancy_month":    4,
			"skip_through_pregnancy_month": 5,
			"post_delivery_catch_up_days":  14,
		},
		"recovery_policy": map[string]any{
			"max_nearby_drive_align_days": 7,
		},
		"drive_policy": map[string]any{
			"enabled":                        true,
			"combo_align_window_days":        7,
			"max_batching_hold_days":         7,
			"max_batching_hold_count":        1,
			"species_grouping_policy":        "kid_mixed",
			"max_shots_per_animal_per_drive": 2,
		},
		"capacity": map[string]any{
			"max_per_day":     200,
			"max_buffer_days": 7,
			"capacity_scope":  "tenant",
			"overflow_policy": "split_within_safe_window_last_safe_may_exceed_cap",
		},
		"schedule":    schedule,
		"matrix_rows": rows,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("build vaccination matrix rule_dsl: %w", err)
	}
	return string(data), nil
}

func buildCanonicalVaccinationMatrix() map[string]vaccMatrixSpec {
	return map[string]vaccMatrixSpec{
		"ET+TT": {
			Species: []string{"goat", "sheep"},
			BirthAgeWaves: []birthAgeWave{
				{DoseCode: "et_tt_kid_4w", Days: 28, MinGapDays: 0},
				{DoseCode: "et_tt_kid_7w", Days: 49, MinGapDays: 21},
			},
			PostArrivalWaves: []postArrivalWave{
				{Days: 7, MinGapDays: 0},
				{Days: 21, MinGapDays: 21},
			},
			RevaccinationDays: 182,
		},
		"PPR": {
			Species: []string{"goat", "sheep"},
			BirthAgeWaves: []birthAgeWave{
				{DoseCode: "ppr_kid_16w", Days: 112, MinGapDays: 0},
			},
			PostArrivalWaves:  []postArrivalWave{{Days: 7}},
			RevaccinationDays: 1095,
		},
		"Goat Pox": {
			Species: []string{"goat"},
			BirthAgeWaves: []birthAgeWave{
				{DoseCode: "goat_pox_kid_20w", Days: 140, MinGapDays: 0},
			},
			PostArrivalWaves:  []postArrivalWave{{Days: 35}},
			RevaccinationDays: 365,
		},
		"FMD": {
			Species: []string{"goat", "sheep"},
			BirthAgeWaves: []birthAgeWave{
				{DoseCode: "fmd_kid_12w", Days: 84, MinGapDays: 0},
			},
			PostArrivalWaves:  []postArrivalWave{{Days: 63}},
			RevaccinationDays: 274,
		},
		"HS": {
			Species: []string{"goat", "sheep"},
			BirthAgeWaves: []birthAgeWave{
				{DoseCode: "hs_kid_12w", Days: 84, MinGapDays: 0},
			},
			PostArrivalWaves:  []postArrivalWave{{Days: 63}},
			RevaccinationDays: 365,
		},
		"Blue tongue": {
			Species: []string{"sheep"},
			BirthAgeWaves: []birthAgeWave{
				{DoseCode: "blue_tongue_kid_16w", Days: 112, MinGapDays: 0},
				{DoseCode: "blue_tongue_kid_20w", Days: 140, MinGapDays: 28},
			},
			PostArrivalWaves:  []postArrivalWave{{Days: 35}},
			RevaccinationDays: 365,
		},
		"Sheep Pox": {
			Species: []string{"sheep"},
			BirthAgeWaves: []birthAgeWave{
				{DoseCode: "sheep_pox_kid_12w", Days: 84, MinGapDays: 0},
			},
			PostArrivalWaves:  []postArrivalWave{{Days: 35}},
			RevaccinationDays: 365,
		},
	}
}

func buildSeedPublicationVaccinationMatrix() map[string]vaccMatrixSpec {
	matrix := buildCanonicalVaccinationMatrix()
	if excluded := excludedSeedPublicationVaccines(); len(excluded) > 0 {
		filtered := make(map[string]vaccMatrixSpec, len(matrix))
		for name, spec := range matrix {
			if excluded[normalizeVaccineNameForExclusion(name)] {
				continue
			}
			filtered[name] = spec
		}
		matrix = filtered
	}
	if os.Getenv("GOATOS_CPT_EXCLUDE_PPR_2026") != "1" {
		return matrix
	}
	filtered := make(map[string]vaccMatrixSpec, len(matrix))
	for name, spec := range matrix {
		if strings.EqualFold(strings.TrimSpace(name), "PPR") {
			continue
		}
		filtered[name] = spec
	}
	return filtered
}

func excludedSeedPublicationVaccines() map[string]bool {
	raw := strings.TrimSpace(os.Getenv("GOATOS_SEED_EXCLUDE_VACCINES"))
	if raw == "" {
		return nil
	}
	excluded := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		key := normalizeVaccineNameForExclusion(part)
		if key != "" {
			excluded[key] = true
		}
	}
	return excluded
}

func normalizeVaccineNameForExclusion(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "_")
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, "+", "_")
	for strings.Contains(value, "__") {
		value = strings.ReplaceAll(value, "__", "_")
	}
	return strings.Trim(value, "_")
}

type vaccMatrixSpec struct {
	Species           []string
	BirthAgeWaves     []birthAgeWave
	PostArrivalWaves  []postArrivalWave
	RevaccinationDays int
}

type birthAgeWave struct {
	DoseCode   string
	Days       int
	MinGapDays int
}

type postArrivalWave struct {
	Days       int
	MinGapDays int
}

func vaccinationSeedEligibility() map[string]any {
	return map[string]any{
		"species":      []string{"goat", "sheep"},
		"animal_stage": []string{"all"},
		"sex":          []string{"female", "male"},
		"breed":        []string{"all"},
		"lifecycle":    []string{"alive"},
		// Legacy V2 rows commonly have no health value. Treat health as an
		// execution-time safety gate so unknown rows remain schedulable while
		// explicit sick/treatment/ICU/quarantine states are visibly deferred.
		"health":                      []string{"any"},
		"reproductive":                []string{"any"},
		"exclude_reproductive_states": []string{"pregnant_late"},
		"defer_states":                []string{"sick", "under_treatment", "recovering", "icu", "quarantine"},
	}
}

func vaccinationSeedEligibilityForSpecies(species []string) map[string]any {
	eligibility := vaccinationSeedEligibility()
	eligibility["species"] = append([]string(nil), species...)
	return eligibility
}

// validVaccineTypes / validPathogenClasses / validCourseTypes are the only accepted values for the
// three independent vaccine classification axes. Pathogen class is deliberately disjoint from
// vaccine type: a "live"/"killed"/"toxoid"/"combo" value in pathogen_class is
// malformed seed metadata (BUG1) that mis-selects same-day compatibility /
// spacing rules. Toxoid stays a valid vaccine TYPE for any future reviewed
// vaccine that needs it; it is simply never a pathogen class.
// Course type (single/booster) is determined from the number of doses in the schedule.
var (
	validVaccineTypes     = map[string]struct{}{"live": {}, "killed": {}, "toxoid": {}}
	validPathogenClasses  = map[string]struct{}{"bacterial": {}, "viral": {}}
	validCourseTypes      = map[string]struct{}{"single": {}, "booster": {}}
	forbiddenPathogenVals = map[string]struct{}{"live": {}, "killed": {}, "toxoid": {}, "combo": {}}
)

// validateVaccineClassifications proves, at seed-build time, that every approved
// vaccine has a known reviewed classification and that the two axes are stored
// separately: Type ∈ {live,killed,toxoid}, Pathogen ∈ {bacterial,viral}, and the
// pathogen field never carries a vaccine-type value. A malformed row fails the
// seed loudly instead of silently emitting a wrong medical class.
func validateVaccineClassifications() error {
	return validateVaccineClassificationsIn(vaccines)
}

func validateVaccineClassificationsIn(defs map[string]vaccineDef) error {
	for name, def := range defs {
		t := strings.ToLower(strings.TrimSpace(def.Type))
		p := strings.ToLower(strings.TrimSpace(def.Pathogen))
		if _, ok := validVaccineTypes[t]; !ok {
			return fmt.Errorf("vaccine %q has invalid immunological type %q (want live|killed|toxoid)", name, def.Type)
		}
		if _, bad := forbiddenPathogenVals[p]; bad {
			return fmt.Errorf("vaccine %q pathogen_class %q is a vaccine-TYPE value; pathogen class must be bacterial|viral (BUG1)", name, def.Pathogen)
		}
		if _, ok := validPathogenClasses[p]; !ok {
			return fmt.Errorf("vaccine %q has unknown pathogen class %q (want bacterial|viral)", name, def.Pathogen)
		}
	}
	return nil
}

func historyObligationID(tenantID string, c vaccCell, def vaccineDef, sourceDateKey string) string {
	return detUUID("obligation-history", tenantID, c.AnimalKey, def.Code, sourceDoseCode(c), sourceDateKey)
}

func historyObligationIdem(c vaccCell, def vaccineDef, sourceDateKey string) string {
	return "vacc-real-obl:history:" + c.AnimalKey + ":" + def.Code + ":" + sourceDoseCode(c) + ":" + sourceDateKey
}

func historyCompletionID(tenantID string, c vaccCell, def vaccineDef, sourceDateKey string) string {
	return detUUID("completion-history", tenantID, c.AnimalKey, def.Code, sourceDoseCode(c), sourceDateKey)
}

func historyCompletionIdem(c vaccCell, def vaccineDef, sourceDateKey string) string {
	return "vacc-real-cmp:" + c.AnimalKey + ":" + def.Code + ":" + sourceDoseCode(c) + ":" + sourceDateKey
}

func sourceDoseCode(c vaccCell) string {
	if code := strings.ToLower(strings.TrimSpace(c.DoseCode)); code != "" {
		return code
	}
	if strings.EqualFold(strings.TrimSpace(c.DoseType), "Booster") {
		return "booster"
	}
	return "first"
}

func sourceVaccinationDateTime(d time.Time, loc *time.Location) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), 9, 0, 0, 0, loc)
}

func sourceVaccinationDateBeforeDOB(d time.Time, dob *time.Time) bool {
	if dob == nil {
		return false
	}
	return startOfDay(d).Before(startOfDay(*dob))
}

func sourceVaccinationDateOnOrBeforeBusinessDate(d time.Time, asOf time.Time, loc *time.Location) bool {
	sourceDay := startOfDay(d.In(loc))
	asOfDay := startOfDay(asOf.In(loc))
	return !sourceDay.After(asOfDay)
}

func nullableDate(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return nil
	}
	return &s
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
