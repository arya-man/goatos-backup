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
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	obligationpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/localtarget"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	processpg "github.com/vgoats/goatos/backend/internal/processintegrity/adapters/postgres"
	processdomain "github.com/vgoats/goatos/backend/internal/processintegrity/domain"
	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protocolapp "github.com/vgoats/goatos/backend/internal/protocol/app"
	vaccinationpg "github.com/vgoats/goatos/backend/internal/vaccination/adapters/postgres"
	vaccinationapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	vaccinationdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
	vaccexecpg "github.com/vgoats/goatos/backend/internal/vaccinationexecution/adapters/postgres"
	vaccexecdomain "github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

const defaultTenantID = "00000000-0000-4000-8000-000000000001"
const matrixProtocolCode = "vaccination.matrix"
const matrixVersionLabel = "V1 Real Vaccination"

// vaccineDef is one vaccine column from the source sheet, mapped to the approved
// GoatOS schedule (docs/preventive-care-vaccination/vaccination-rules.md).
type vaccineDef struct {
	Code      string  // protocol_definitions.code suffix (lowercase dotted-safe)
	Name      string  // human protocol name / vaccine column label
	Disease   string  // vaccine disease target
	Type      string  // live | killed | toxoid
	ItemCode  string  // inventory item code
	DoseML    float64 // dose amount (ml) from the source dose table
	VialDoses int     // doses per vial
}

// vaccines maps the source-sheet vaccine header -> its definition. Keys are the
// exact strings that appear in vaccination.json header row 0.
var vaccines = map[string]vaccineDef{
	"ET+TT":       {Code: "et_tt", Name: "ET+TT", Disease: "Enterotoxaemia + Tetanus", Type: "toxoid", ItemCode: "VAC-ET-TT", DoseML: 2, VialDoses: 100},
	"PPR":         {Code: "ppr", Name: "PPR", Disease: "Peste des Petits Ruminants", Type: "live", ItemCode: "VAC-PPR", DoseML: 1, VialDoses: 100},
	"Blue tongue": {Code: "blue_tongue", Name: "Blue Tongue", Disease: "Blue Tongue", Type: "killed", ItemCode: "VAC-BT", DoseML: 2, VialDoses: 100},
	"FMD":         {Code: "fmd", Name: "FMD", Disease: "Foot and Mouth Disease", Type: "killed", ItemCode: "VAC-FMD", DoseML: 1, VialDoses: 30},
	"HS":          {Code: "hs", Name: "HS", Disease: "Haemorrhagic Septicaemia", Type: "killed", ItemCode: "VAC-HS", DoseML: 2, VialDoses: 100},
	"Goat Pox":    {Code: "goat_pox", Name: "Goat Pox", Disease: "Goat Pox", Type: "live", ItemCode: "VAC-GP", DoseML: 1, VialDoses: 25},
	"Sheep Pox":   {Code: "sheep_pox", Name: "Sheep Pox", Disease: "Sheep Pox", Type: "live", ItemCode: "VAC-SP", DoseML: 1, VialDoses: 100},
}

// vaccineOrder gives a stable insert order for the 7 protocols.
var vaccineOrder = []string{"ET+TT", "PPR", "Blue tongue", "FMD", "HS", "Goat Pox", "Sheep Pox"}

type goatRecord struct {
	RFID           string
	OldID          string
	OldIDSuffix    string
	Farm           string
	Shed           string
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
	Health         string
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
	Skipped            int         // NA / blank cells
	KernelGenerated    int         // kernel-generated open obligations
	KernelDeferred     int         // kernel-deferred obligations
	KernelSuppressed   int         // kernel-suppressed (already completed) obligations
	Purged             purgeCounts // synthetic fixtures removed
}

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

	goats, err := loadGoats(*sourcePath)
	if err != nil {
		return fmt.Errorf("load goats: %w", err)
	}
	cells, err := loadVaccinationCells(*sourcePath)
	if err != nil {
		return fmt.Errorf("load vaccination cells: %w", err)
	}

	if _, err := seed(ctx, pool, pgCfg, *tenantID, loc, goats, cells, *purgeFixtures, *allowPartialGeneration); err != nil {
		return fmt.Errorf("seed: %w", err)
	}

	processProjection, err := processpg.NewRepository(pool, pgCfg.QueryTimeout).RecomputeProjection(ctx, processdomain.ProjectionRecomputeRequest{
		TenantID: *tenantID,
	})
	if err != nil {
		return fmt.Errorf("recompute process integrity projection after vaccination seed: %w", err)
	}

	fmt.Printf("process_integrity_projection rows=%d version=%d as_of=%s\n",
		processProjection.Rows, processProjection.ProjectionVersion, processProjection.AsOf.Format(time.RFC3339))

	// Deploy-seed population (C35-002, vaccinationexecution half): every
	// vaccination read path serves versioned projections now. A real-data seed is
	// not complete until these read models are warm from the canonical seed rows.
	vaccinationExecutionRepo := vaccexecpg.NewRepository(pool, pgCfg.QueryTimeout)
	shedProjection, err := vaccinationExecutionRepo.RecomputeShedProjection(ctx, vaccexecdomain.ShedProjectionRecomputeRequest{
		TenantID: *tenantID,
	})
	if err != nil {
		return fmt.Errorf("recompute vaccination shed projection after vaccination seed: %w", err)
	}

	fmt.Printf("vaccination_shed_projection rows=%d version=%d as_of=%s\n",
		shedProjection.Rows, shedProjection.ProjectionVersion, shedProjection.AsOf.Format(time.RFC3339))

	executionProjection, err := vaccinationExecutionRepo.RecomputeExecutionProjection(ctx, vaccexecdomain.ExecutionProjectionRecomputeRequest{
		TenantID: *tenantID,
	})
	if err != nil {
		return fmt.Errorf("recompute vaccination execution projection after vaccination seed: %w", err)
	}

	fmt.Printf("vaccination_execution_projection rows=%d version=%d as_of=%s\n",
		executionProjection.Rows, executionProjection.ProjectionVersion, executionProjection.AsOf.Format(time.RFC3339))

	operationsProjection, err := vaccinationExecutionRepo.RecomputeOperationsProjection(ctx, vaccexecdomain.OperationsProjectionRecomputeRequest{
		TenantID: *tenantID,
	})
	if err != nil {
		return fmt.Errorf("recompute vaccination operations projection after vaccination seed: %w", err)
	}

	fmt.Printf("vaccination_operations_projection rows=%d version=%d as_of=%s\n",
		operationsProjection.Rows, operationsProjection.ProjectionVersion, operationsProjection.AsOf.Format(time.RFC3339))
	return nil
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
			OldID:          cell(row, col["old_id"]),
			OldIDSuffix:    cell(row, col["old_id_suffix"]),
			Farm:           cell(row, col["farm"]),
			Shed:           cell(row, col["shed"]),
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

func seed(ctx context.Context, pool *pgxpool.Pool, pgCfg platformpg.Config, tenantID string, loc *time.Location, goats []goatRecord, cells []vaccCell, purgeFixtures bool, allowPartialGeneration bool) (stats, error) {
	var st stats
	now := time.Now().In(loc)

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

	// 1. Parks: resolve existing CBE/CPT park rows by location_code.
	parkByFarm := map[string]string{}
	for _, farm := range distinct(goats, func(g goatRecord) string { return g.Farm }) {
		var parkID string
		if err := tx.QueryRow(ctx, `SELECT location_id FROM locations WHERE tenant_id=$1 AND location_type='park' AND location_code=$2 LIMIT 1`, tenantID, farm).Scan(&parkID); err != nil {
			return st, fmt.Errorf("resolve park for farm %q: %w", farm, err)
		}
		parkByFarm[farm] = parkID
		st.ParksResolved++
	}

	// 2. Sheds: resolve existing park-scoped shed by (name, park); create missing ones
	//    so EVERY goat maps to its real shed.
	shedByKey := map[shedKey]string{}
	for _, k := range distinctShedKeys(goats) {
		if k.shed == "" {
			continue
		}
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

	ruleDSL, err := vaccinationMatrixRuleDSL()
	if err != nil {
		return st, err
	}
	sopVersionID, err := resolveVaccinationSOPVersion(ctx, tx, tenantID)
	if err != nil {
		return st, err
	}
	versionID := detUUID("protocol_version", tenantID, "vaccination_matrix", "v1_real")
	var status, existingRuleDSL, existingSOPVersionID string
	var existingVersion int
	qerr := tx.QueryRow(ctx, `
		SELECT protocol_version_id, status, version, rule_dsl::text, COALESCE(sop_version_id::text, '')
		FROM protocol_versions
		WHERE tenant_id=$1
		  AND protocol_id=$2
		  AND scope_type='tenant'
		  AND scope_id IS NULL
		  AND version_label=$3
		  AND status <> 'retired'
		ORDER BY version DESC
		LIMIT 1`,
		tenantID, protocolID, matrixVersionLabel).Scan(&versionID, &status, &existingVersion, &existingRuleDSL, &existingSOPVersionID)
	if qerr == nil && status == "published" && (!jsonSemanticallyEqual(existingRuleDSL, ruleDSL) || existingSOPVersionID != sopVersionID) {
		qerr = pgx.ErrNoRows
	}
	if qerr == pgx.ErrNoRows {
		nextVersion, err := nextProtocolVersion(ctx, tx, tenantID, protocolID)
		if err != nil {
			return st, err
		}
		versionID = detUUID("protocol_version", tenantID, "vaccination_matrix", "v1_real", fmt.Sprintf("%d", nextVersion))
		if _, err := tx.Exec(ctx, `
			INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, scope_id, version,
				version_label, status, effective_from, effective_to, rule_dsl, proof_policy, sop_version_id)
			VALUES ($1,$2,$3,'tenant',NULL,$4,$5,'draft',DATE '2026-01-01',NULL,$6::jsonb,'{"required_proofs":["shed","vial_lot","administration"]}'::jsonb,$7::uuid)`,
			versionID, tenantID, protocolID, nextVersion, matrixVersionLabel, ruleDSL, sopVersionID); err != nil {
			return st, fmt.Errorf("protocol version vaccination matrix: %w", err)
		}
		status = "draft"
	} else if qerr != nil {
		return st, fmt.Errorf("lookup vaccination matrix version: %w", qerr)
	} else if status == "draft" && (!jsonSemanticallyEqual(existingRuleDSL, ruleDSL) || existingSOPVersionID != sopVersionID) {
		if _, err := tx.Exec(ctx, `
			UPDATE protocol_versions
			SET rule_dsl=$1::jsonb,
			    proof_policy='{"required_proofs":["shed","vial_lot","administration"]}'::jsonb,
			    sop_version_id=$2::uuid,
			    updated_at=now(),
			    row_version=row_version+1
			WHERE tenant_id=$3::uuid
			  AND protocol_version_id=$4::uuid
			  AND status='draft'`,
			ruleDSL, sopVersionID, tenantID, versionID); err != nil {
			return st, fmt.Errorf("refresh vaccination matrix draft: %w", err)
		}
	} else {
		_ = existingVersion
	}

	if err := tx.Commit(ctx); err != nil {
		return st, fmt.Errorf("commit protocol draft: %w", err)
	}
	committed = true

	protocolService := protocolapp.NewService(protocolpg.NewRepository(pool, pgCfg.QueryTimeout))
	if err := protocolService.PublishVersion(ctx, tenantID, versionID, nil, "seed-vaccination-real:"+versionID); err != nil {
		return st, fmt.Errorf("publish vaccination matrix through protocol service: %w", err)
	}

	tx, err = pool.Begin(ctx)
	if err != nil {
		return st, fmt.Errorf("begin animal seed tx: %w", err)
	}
	committed = false

	// Load all protocol rules (birth_age, post_arrival, revacc for all vaccines)
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
	type goatIns struct {
		goatID, animalKey, animalIdentifier1, animalIdentifier2, species, breed, breedID, sex, lifecycle, originType, stage, age, shedID, parkID, dob string
		entryDate                                                                                                                                     string
		health                                                                                                                                        *string
	}
	var goatRows []goatIns
	for _, g := range goats {
		shedID, ok := shedByKey[shedKey{g.Farm, g.Shed}]
		if !ok {
			// No shed (blank) -> cannot place in a cohort; skip animal placement.
			continue
		}
		animalKey := sourceAnimalIdentifier(g.RFID, g.OldID, g.OldIDSuffix)
		if animalKey == "" {
			continue
		}
		animalIdentifier1, animalIdentifier2 := identifierSlots(g.RFID, g.OldID, g.OldIDSuffix)
		goatID := detUUID("goat", tenantID, animalKey)
		parkID := parkByFarm[g.Farm]
		goatIDByAnimalKey[animalKey] = goatID
		goatLifecycleByAnimalKey[animalKey] = normalizeLifecycle(g.Status)
		goatOriginTypeByAnimalKey[animalKey] = normalizeOriginType(g.OriginType)
		goatStageByAnimalKey[animalKey] = normalizeStage(g.Stage, g.Age)
		goatShedByAnimalKey[animalKey] = shedID
		goatParkByAnimalKey[animalKey] = parkID

		// Parse DOB for later use in schedule path resolution
		var dobTime *time.Time
		if g.DOB != "" {
			if d, err := time.ParseInLocation("2006-01-02", g.DOB, loc); err == nil {
				dobTime = &d
				goatDOBByAnimalKey[animalKey] = dobTime
			}
		}

		// Resolve entry_date from source mapping
		entryDate := entryDateByAnimalKey[animalKey]
		entryDateValue := ""
		if entryDate != nil {
			goatEntryDateByAnimalKey[animalKey] = entryDate
			entryDateValue = entryDate.Format("2006-01-02")
		}

		species := deriveSeedSpecies(g.Species, g.Breed)
		breed := normalizeBreed(g.Breed)
		goatRows = append(goatRows, goatIns{
			goatID:            goatID,
			animalKey:         animalKey,
			animalIdentifier1: animalIdentifier1,
			animalIdentifier2: animalIdentifier2,
			species:           species,
			breed:             breed,
			breedID:           breedIDs[seedBreedKey(species, breed)],
			sex:               normalizeSex(g.Gender),
			lifecycle:         normalizeLifecycle(g.Status),
			originType:        normalizeOriginType(g.OriginType),
			stage:             normalizeStage(g.Stage, g.Age),
			age:               g.Age,
			health:            normalizeHealth(g.Health),
			shedID:            shedID,
			parkID:            parkID,
			dob:               g.DOB,
			entryDate:         entryDateValue,
		})
	}
	if err := batch(ctx, tx, goatRows, 500, func(b *pgx.Batch, gi goatIns) {
		b.Queue(`
				INSERT INTO goats (goat_id, tenant_id, species, breed, breed_id, sex, lifecycle_status,
					health_status, origin_type, dob, entry_date, current_location_id, shed_id, park_id, management_stage, age_band, custodian_party_id, updated_at)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$12,$13,$14,$15,$16,now())
				ON CONFLICT (goat_id) DO UPDATE SET species=EXCLUDED.species, breed=EXCLUDED.breed, breed_id=EXCLUDED.breed_id, sex=EXCLUDED.sex,
					lifecycle_status=EXCLUDED.lifecycle_status, health_status=EXCLUDED.health_status,
					shed_id=EXCLUDED.shed_id, park_id=EXCLUDED.park_id, current_location_id=EXCLUDED.current_location_id,
					management_stage=EXCLUDED.management_stage, age_band=EXCLUDED.age_band, entry_date=EXCLUDED.entry_date, updated_at=now()`,
			gi.goatID, tenantID, gi.species, gi.breed, nullString(gi.breedID), gi.sex, gi.lifecycle, gi.health, nullString(gi.originType),
			nullableDate(gi.dob), nullableDate(gi.entryDate), gi.shedID, gi.parkID, gi.stage, nullString(gi.age), custodianPartyID)
	}); err != nil {
		return st, fmt.Errorf("insert goats: %w", err)
	}
	st.Animals = len(goatRows)

	// Identifiers. animal_identifier_1 is the best available real-world animal ID;
	// animal_identifier_2 carries the secondary tag when both RFID and old/source tag exist.
	for _, gi := range goatRows {
		if _, err := tx.Exec(ctx, `
			INSERT INTO goat_identifiers (identifier_type, tenant_id, goat_id, identifier_value, normalized_value,
				scope_key, valid_from, normalizer_version, status, is_primary_for_goat)
			VALUES ('animal_identifier_1',$1,$2,$3,$4,'global',now()::date,'identifier_normalizer_v1','active',true)
			ON CONFLICT (tenant_id, normalized_value) DO NOTHING`,
			tenantID, gi.goatID, gi.animalIdentifier1, strings.ToLower(gi.animalIdentifier1)); err != nil {
			return st, fmt.Errorf("insert identifier %s: %w", gi.animalIdentifier1, err)
		}
		if gi.animalIdentifier2 == "" || strings.EqualFold(gi.animalIdentifier2, gi.animalIdentifier1) {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO goat_identifiers (identifier_type, tenant_id, goat_id, identifier_value, normalized_value,
				scope_key, valid_from, normalizer_version, status, is_primary_for_goat)
			VALUES ('animal_identifier_2',$1,$2,$3,$4,'global',now()::date,'identifier_normalizer_v1','active',false)
			ON CONFLICT (tenant_id, normalized_value) DO NOTHING`,
			tenantID, gi.goatID, gi.animalIdentifier2, strings.ToLower(gi.animalIdentifier2)); err != nil {
			return st, fmt.Errorf("insert identifier %s: %w", gi.animalIdentifier2, err)
		}
	}

	// 5. Obligations-first, then completions. Build the two batches, classifying each cell.
	var obls []oblIns
	var cmps []cmpIns

	vaccMatrixDef := buildCanonicalVaccinationMatrix()

	for _, c := range cells {
		goatID, ok := goatIDByAnimalKey[c.AnimalKey]
		if !ok {
			continue // goat not placed (no shed) or not in herd sheet
		}
		versionID, ok := versionByVaccine[c.Vaccine]
		if !ok {
			continue
		}
		def := vaccines[c.Vaccine]

		// Resolve goat's schedule path (kid vs adult)
		path := schedulePathForGoat(
			goatOriginTypeByAnimalKey[c.AnimalKey],
			goatDOBByAnimalKey[c.AnimalKey],
			goatStageByAnimalKey[c.AnimalKey],
			goatEntryDateByAnimalKey[c.AnimalKey],
			now,
		)

		// Map sheet dose to actual rule based on schedule path
		var doseCodeForPath string
		spec := vaccMatrixDef[c.Vaccine]

		if path == "kid" {
			// Map sheet First/Booster to birth_age waves
			if c.DoseCode == "first" && len(spec.BirthAgeWaves) > 0 {
				doseCodeForPath = spec.BirthAgeWaves[0].DoseCode
			} else if c.DoseCode == "booster" && len(spec.BirthAgeWaves) > 1 {
				doseCodeForPath = spec.BirthAgeWaves[1].DoseCode
			} else if c.DoseCode == "first" {
				doseCodeForPath = spec.BirthAgeWaves[0].DoseCode
			} else {
				st.Skipped++
				continue
			}
		} else {
			// Adult path: map sheet First/Booster to post_arrival waves
			if c.DoseCode == "first" && len(spec.PostArrivalWaves) > 0 {
				doseCodeForPath = def.Code + "_adult_w1"
			} else if c.DoseCode == "booster" && len(spec.PostArrivalWaves) > 1 {
				doseCodeForPath = def.Code + "_adult_w2"
			} else if c.DoseCode == "first" {
				doseCodeForPath = def.Code + "_adult_w1"
			} else {
				st.Skipped++
				continue
			}
		}

		ruleID := ruleByDoseCode[doseCodeForPath]
		if ruleID == "" {
			st.Skipped++
			continue
		}

		oblID := detUUID("obligation", tenantID, c.AnimalKey, def.Code, doseCodeForPath)
		oblIdem := "vacc-real-obl:" + c.AnimalKey + ":" + def.Code + ":" + doseCodeForPath
		scopeType, scopeID := seedObligationScope(tenantID, goatParkByAnimalKey[c.AnimalKey], goatShedByAnimalKey[c.AnimalKey])

		// Open (scheduled/due) vaccination obligations are blocked by the procurement
		// exclusion guard for goats that are dead/sold/lost/culled/transferred/merged/inactive.
		// Completed history is still allowed for those goats.
		openEligible := !excludedLifecycle(goatLifecycleByAnimalKey[c.AnimalKey])

		val := strings.TrimSpace(c.Value)
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
		default:
			d, err := time.ParseInLocation("2006-01-02", val, loc)
			if err != nil {
				st.Skipped++
				continue
			}
			administeredAt := sourceVaccinationDateTime(d, loc)
			if sourceVaccinationDateOnOrBeforeBusinessDate(d, now, loc) {
				// Source date cells are imported base anchors. Persist anchors on or
				// before the business date as accepted/completed history so the
				// recurrence engine has a durable start point, but never materialize
				// open work from the anchor itself.
				completedAt := administeredAt
				verifiedBy := detUUID("seed-actor", tenantID)
				sourceDateKey := administeredAt.Format("2006-01-02")
				historyOblID := historyObligationID(tenantID, c, def, sourceDateKey)
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
					idem:         historyObligationIdem(c, def, sourceDateKey),
				})
				cmps = append(cmps, cmpIns{
					completionID:   historyCompletionID(tenantID, c, def, sourceDateKey),
					obligationID:   historyOblID,
					goatID:         goatID,
					doseML:         def.DoseML,
					administeredAt: administeredAt,
					verifiedAt:     &administeredAt,
					verifiedBy:     verifiedBy,
					idem:           historyCompletionIdem(c, def, sourceDateKey),
				})
				st.Completed++
				st.CompletionsHistory++
				// NO flat next-revacc obligation; kernel will generate revacc from the completion
			} else {
				if !openEligible {
					st.Skipped++
					continue
				}
				// Future dose -> scheduled at the sheet date.
				obls = append(obls, oblIns{
					obligationID: oblID, versionID: versionID, ruleID: ruleID, goatID: goatID,
					scopeType: scopeType, scopeID: scopeID,
					dueAt: administeredAt, status: "scheduled", sequence: c.Sequence, idem: oblIdem,
				})
				st.Scheduled++
			}
		}
	}
	st.Obligations = len(obls)

	// Insert obligations first (FK target for completions).
	if err := batch(ctx, tx, obls, 500, func(b *pgx.Batch, o oblIns) {
		b.Queue(`
			INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id,
				target_type, target_id, scope_type, scope_id, due_at, window_start, status, completed_at,
				sequence, idempotency_key, created_at, updated_at)
			VALUES ($1,$2,$3,$4,'goat',$5,$6,$7,$8,$9,$10,$11,$12,$13,now(),now())
			ON CONFLICT (tenant_id, idempotency_key) DO NOTHING`,
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
			ON CONFLICT (tenant_id, idempotency_key) DO NOTHING`,
			cm.completionID, tenantID, cm.obligationID, cm.goatID, cm.doseML, cm.administeredAt, cm.verifiedAt, cm.verifiedBy, cm.idem)
	}); err != nil {
		return st, fmt.Errorf("insert completions: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return st, fmt.Errorf("commit: %w", err)
	}
	committed = true

	// Run kernel generation IN THE SEED to produce derived obligations
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
	if err := verifySeedReconciliation(ctx, pool, tenantID, now, st); err != nil {
		return st, err
	}

	return st, nil
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
}

// verifySeedReconciliation is the non-optional seed postflight. A seed command may
// have committed source rows before generation fails, so a failed postflight means
// the environment is unusable and must be reset/reseeded; it must never be handed
// off as partially healthy.
func verifySeedReconciliation(ctx context.Context, pool *pgxpool.Pool, tenantID string, asOf time.Time, st stats) error {
	var got seedReconciliation
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
     AND (due_at AT TIME ZONE 'Asia/Kolkata')::date
         <= ($2::timestamptz AT TIME ZONE 'Asia/Kolkata')::date),
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
   FROM active oi
   JOIN protocol_rules pr
     ON pr.tenant_id = oi.tenant_id
    AND pr.rule_id = oi.rule_id
   JOIN goats g ON g.tenant_id = oi.tenant_id AND g.goat_id = oi.target_id
   WHERE oi.target_type = 'goat'
     AND (
       (pr.trigger_type = 'birth_age' AND g.dob IS NULL)
       OR (pr.trigger_type = 'post_arrival' AND g.entry_date IS NULL)
     )
     AND oi.status <> 'deferred'
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
		&got.MissingAnchorNormalWork,
	)
	if err != nil {
		return fmt.Errorf("vaccination seed reconciliation query: %w", err)
	}
	if err := validateSeedReconciliation(got, int64(st.CompletionsHistory)); err != nil {
		return fmt.Errorf("vaccination seed reconciliation failed: %w", err)
	}
	fmt.Printf("seed_reconciliation accepted_history=%d status_mismatches=0 duplicate_active_rule_targets=0 active_primary_after_history=0 seeded_pending_placeholders=0 repeat_not_future=0 schedulable_not_future=0 missing_breed_fks=0 missing_primary_identifiers=0 missing_anchor_normal_work=0\n", got.SourceAcceptedHistory)
	return nil
}

func validateSeedReconciliation(got seedReconciliation, expectedHistory int64) error {
	problems := make([]string, 0, 6)
	if got.SourceAcceptedHistory != expectedHistory {
		problems = append(problems, fmt.Sprintf("accepted source history=%d want=%d", got.SourceAcceptedHistory, expectedHistory))
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
		problems = append(problems, fmt.Sprintf("schedulable open work not strictly future=%d", got.SchedulableOpenWorkNotFuture))
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
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func seedObligationScope(tenantID, parkID, shedID string) (scopeType, scopeID string) {
	if strings.TrimSpace(shedID) != "" {
		return "shed", shedID
	}
	if strings.TrimSpace(parkID) != "" {
		return "park", parkID
	}
	return "tenant", tenantID
}

func printSeedSummary(st stats, genRes vaccinationdomain.GenerateResult) {
	fmt.Printf("seeded real vaccination data with kernel generation:\n"+
		"  parks_resolved=%d sheds_resolved=%d sheds_created=%d protocols=%d animals=%d\n"+
		"  obligations=%d (completed=%d scheduled=%d) completions_history=%d pending_source=%d skipped_cells=%d\n"+
		"  purged_fixtures total=%d (calendar_projections=%d obligations=%d batches=%d goats=%d sheds=%d other_child_rows=%d)\n"+
		"  kernel_generation generated=%d deferred=%d reopened=%d failed_goats=%d skipped_no_due_date=%d suppressed_trusted=%d\n",
		st.ParksResolved, st.ShedsResolved, st.ShedsCreated, st.Protocols, st.Animals,
		st.Obligations, st.Completed, st.Scheduled, st.CompletionsHistory, st.PendingSource, st.Skipped,
		st.Purged.total(), st.Purged.CalendarProjections, st.Purged.Obligations, st.Purged.Batches,
		st.Purged.Goats, st.Purged.Sheds, st.Purged.OtherChildRows,
		genRes.Generated, genRes.Deferred, genRes.Reopened, genRes.FailedGoats, genRes.SkippedNoDueDate, genRes.SuppressedByTrustedHistory)
}

// purgeCounts breaks down what the fixture purge removed, for the run report.
type purgeCounts struct {
	CalendarProjections int
	Obligations         int
	Batches             int
	Goats               int
	Sheds               int
	OtherChildRows      int
}

func (p purgeCounts) total() int {
	return p.CalendarProjections + p.Obligations + p.Batches + p.Goats + p.Sheds + p.OtherChildRows
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
	junkVersions := `(SELECT pv.protocol_version_id FROM protocol_versions pv
		JOIN protocol_definitions pd ON pd.tenant_id = pv.tenant_id AND pd.protocol_id = pv.protocol_id
		WHERE pv.tenant_id = $1 AND pd.category = 'vaccination' AND pd.code LIKE 'vaccination.matrix%')`
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

	// Phase 1 — stale junk calendar_event_projections (leaf; references locations/protocol/batch).
	n, err := exec("calendar_event_projections", `DELETE FROM calendar_event_projections
		WHERE tenant_id = $1 AND (
			protocol_version_id IN `+junkVersions+`
			OR shed_id IN `+junkSheds+`
			OR park_id IN `+junkSheds+`
			OR title ILIKE '%Chain Proof%' OR title ILIKE '%Rework Proof%'
			OR title ILIKE '%Trusted History%' OR title ILIKE '%Trigger Seed%'
			OR title ILIKE '%Calendar Shed%')`)
	if err != nil {
		return pc, err
	}
	pc.CalendarProjections += n

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
		{"calendar_event_projections (shed)", `DELETE FROM calendar_event_projections WHERE tenant_id = $1 AND (shed_id IN ` + junkSheds + ` OR park_id IN ` + junkSheds + `)`},
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
	out := map[string]*time.Time{}
	for _, g := range goats {
		animalKey := sourceAnimalIdentifier(g.RFID, g.OldID, g.OldIDSuffix)
		if animalKey == "" {
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

// schedulePathForGoat replicates the kernel's schedulePathForGoat logic
func schedulePathForGoat(originType string, dob *time.Time, stage string, entryDate *time.Time, asOf time.Time) string {
	origin := strings.ToLower(strings.TrimSpace(originType))
	if origin == "birth" {
		return "kid"
	}
	kidWeeks := 16
	// Check age: age <= 16w (112d) → kid
	if dob != nil {
		ageDays := int(asOf.Sub(*dob).Hours() / 24)
		ageWeeks := ageDays / 7
		if ageWeeks <= kidWeeks {
			return "kid"
		}
	}
	// Check management_stage starts "K" → kid
	if isKidManagementStage(stage) {
		return "kid"
	}
	// Check if has entry_date (procured/imported) → adult
	if entryDate != nil {
		return "adult"
	}
	// Check origin type
	if origin == "procured" || origin == "imported" {
		return "adult"
	}
	return "kid"
}

func isKidManagementStage(stage string) bool {
	stage = strings.ToUpper(strings.TrimSpace(stage))
	if len(stage) >= 1 && strings.HasPrefix(stage, "K") {
		return true
	}
	return false
}

// resolveGoatPath wraps schedulePathForGoat for spec naming consistency
func resolveGoatPath(originType string, dob *time.Time, stage string, entryDate *time.Time, asOf time.Time) string {
	return schedulePathForGoat(originType, dob, stage, entryDate, asOf)
}

// mapSheetDoseToRuleCode maps a sheet dose code (First/Booster) to the actual rule dose_code
// based on goat schedule path (kid vs adult) and vaccine spec.
func mapSheetDoseToRuleCode(vaccine string, sheetDoseCode string, path string, vaccMatrixDef map[string]vaccMatrixSpec) string {
	spec, ok := vaccMatrixDef[vaccine]
	if !ok {
		return ""
	}
	def, ok := vaccines[vaccine]
	if !ok {
		return ""
	}

	if path == "kid" {
		// Map sheet First/Booster to birth_age waves
		if sheetDoseCode == "first" && len(spec.BirthAgeWaves) > 0 {
			return spec.BirthAgeWaves[0].DoseCode
		} else if sheetDoseCode == "booster" && len(spec.BirthAgeWaves) > 1 {
			return spec.BirthAgeWaves[1].DoseCode
		} else if sheetDoseCode == "first" {
			return spec.BirthAgeWaves[0].DoseCode
		}
	} else {
		// Adult path: map sheet First/Booster to post_arrival waves
		if sheetDoseCode == "first" && len(spec.PostArrivalWaves) > 0 {
			return def.Code + "_adult_w1"
		} else if sheetDoseCode == "booster" && len(spec.PostArrivalWaves) > 1 {
			return def.Code + "_adult_w2"
		} else if sheetDoseCode == "first" {
			return def.Code + "_adult_w1"
		}
	}
	return ""
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

func identifierSlots(rfid string, oldID string, suffix string) (string, string) {
	rfid = strings.TrimSpace(rfid)
	oldTag := oldTagIdentifier(oldID, suffix)
	if rfid == "" {
		return oldTag, ""
	}
	if oldTag == "" || strings.EqualFold(oldTag, rfid) {
		return rfid, ""
	}
	return rfid, oldTag
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
		k := shedKey{g.Farm, g.Shed}
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

func shedCode(farm, shed string) string {
	slug := strings.ToUpper(shed)
	slug = strings.Map(func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		default:
			return '_'
		}
	}, slug)
	for strings.Contains(slug, "__") {
		slug = strings.ReplaceAll(slug, "__", "_")
	}
	slug = strings.Trim(slug, "_")
	return farm + "_SHED_" + slug
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

func normalizeHealth(h string) *string {
	switch strings.ToLower(strings.TrimSpace(h)) {
	case "healthy", "normal", "ok":
		v := "healthy"
		return &v
	case "sick", "ill", "diseased":
		v := "sick"
		return &v
	case "under_treatment", "under treatment", "treatment":
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

func nullString(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

func resolveVaccinationSOPVersion(ctx context.Context, tx pgx.Tx, tenantID string) (string, error) {
	var sopVersionID string
	err := tx.QueryRow(ctx, `
		SELECT sv.sop_version_id::text
		FROM sop_versions sv
		JOIN sop_definitions sd
		  ON sd.tenant_id = sv.tenant_id
		 AND sd.sop_id = sv.sop_id
		WHERE sv.tenant_id = $1::uuid
		  AND sd.code = 'vaccination.drive'
		  AND sv.status = 'published'
		ORDER BY sv.version DESC, sv.updated_at DESC
		LIMIT 1`, tenantID).Scan(&sopVersionID)
	if err != nil {
		return "", fmt.Errorf("resolve published vaccination.drive SOP version: %w", err)
	}
	return sopVersionID, nil
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
	type scheduleRow struct {
		DoseCode            string  `json:"dose_code"`
		SourceDoseCode      string  `json:"source_dose_code"`
		Sequence            int     `json:"sequence"`
		TriggerType         string  `json:"trigger_type"`
		OffsetDays          int     `json:"offset_days"`
		DueWindowDays       int     `json:"due_window_days"`
		DoseAmount          float64 `json:"dose_amount"`
		DoseUnit            string  `json:"dose_unit"`
		VialDoses           int     `json:"vial_doses"`
		ScheduleNote        string  `json:"schedule_note,omitempty"`
		RouteSite           string  `json:"route_site"`
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
	}

	type matrixRow struct {
		RowID       string         `json:"row_id"`
		Vaccine     vaccineMeta    `json:"vaccine"`
		Species     []string       `json:"species"`
		Eligibility map[string]any `json:"eligibility"`
		Schedule    []scheduleRow  `json:"schedule"`
	}

	// Build the canonical vaccination matrix per spec
	vaccMatrixDef := buildCanonicalVaccinationMatrix()

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
				VialDoses:           def.VialDoses,
				ScheduleNote:        "real vaccination seed source matrix",
				RouteSite:           "subcutaneous",
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

		// post_arrival doses (adult procurement path)
		for waveIdx, wave := range spec.PostArrivalWaves {
			sequenceCounter++
			dose := fmt.Sprintf("%s_adult_w%d", def.Code, waveIdx+1)
			cell := scheduleRow{
				DoseCode:            dose,
				SourceDoseCode:      dose,
				Sequence:            sequenceCounter,
				TriggerType:         "post_arrival",
				OffsetDays:          wave,
				DueWindowDays:       7,
				DoseAmount:          def.DoseML,
				DoseUnit:            "ml",
				VialDoses:           def.VialDoses,
				ScheduleNote:        "real vaccination seed source matrix",
				RouteSite:           "subcutaneous",
				MaxDelayDays:        7,
				CourseLapsePolicy:   "preventive_care_review",
				MinGapDays:          0,
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
				VialDoses:      def.VialDoses,
				ScheduleNote:   "real vaccination seed source matrix",
				RouteSite:      "subcutaneous",
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
		if len(spec.BirthAgeWaves) > 1 {
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
				PathogenClass:      pathogenClass(def.Type),
				CourseType:         courseType,
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
			"kids_normal_schedule_until_weeks": 16,
			"adult_prior_vaccination_allowed":  true,
			"first_wave":                       []string{"ET+TT", "PPR"},
			"second_wave_after_days":           28,
			"goat_second_wave":                 []string{"Goat Pox", "ET+TT booster"},
			"sheep_second_wave":                []string{"ET+TT booster", "Sheep Pox"},
		},
		"capacity": map[string]any{
			"max_per_day":     100,
			"max_buffer_days": 7,
			"capacity_scope":  "tenant",
			"overflow_policy": "split_within_safe_window_then_mark_needs_review",
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
			PostArrivalWaves:  []int{7, 35},
			RevaccinationDays: 182,
		},
		"PPR": {
			Species: []string{"goat", "sheep"},
			BirthAgeWaves: []birthAgeWave{
				{DoseCode: "ppr_kid_16w", Days: 112, MinGapDays: 0},
			},
			PostArrivalWaves:  []int{7},
			RevaccinationDays: 1095,
		},
		"Goat Pox": {
			Species: []string{"goat"},
			BirthAgeWaves: []birthAgeWave{
				{DoseCode: "goat_pox_kid_20w", Days: 140, MinGapDays: 0},
			},
			PostArrivalWaves:  []int{35},
			RevaccinationDays: 365,
		},
		"FMD": {
			Species: []string{"goat", "sheep"},
			BirthAgeWaves: []birthAgeWave{
				{DoseCode: "fmd_kid_12w", Days: 84, MinGapDays: 0},
			},
			PostArrivalWaves:  []int{63},
			RevaccinationDays: 274,
		},
		"HS": {
			Species: []string{"goat", "sheep"},
			BirthAgeWaves: []birthAgeWave{
				{DoseCode: "hs_kid_12w", Days: 84, MinGapDays: 0},
			},
			PostArrivalWaves:  []int{63},
			RevaccinationDays: 365,
		},
		"Blue tongue": {
			Species: []string{"sheep"},
			BirthAgeWaves: []birthAgeWave{
				{DoseCode: "blue_tongue_kid_16w", Days: 112, MinGapDays: 0},
				{DoseCode: "blue_tongue_kid_20w", Days: 140, MinGapDays: 28},
			},
			PostArrivalWaves:  []int{35},
			RevaccinationDays: 365,
		},
		"Sheep Pox": {
			Species: []string{"sheep"},
			BirthAgeWaves: []birthAgeWave{
				{DoseCode: "sheep_pox_kid_12w", Days: 84, MinGapDays: 0},
			},
			PostArrivalWaves:  []int{35},
			RevaccinationDays: 365,
		},
	}
}

type vaccMatrixSpec struct {
	Species           []string
	BirthAgeWaves     []birthAgeWave
	PostArrivalWaves  []int
	RevaccinationDays int
}

type birthAgeWave struct {
	DoseCode   string
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
		"defer_states":                []string{"sick", "under_treatment", "icu", "quarantine"},
	}
}

func vaccinationSeedEligibilityForSpecies(species []string) map[string]any {
	eligibility := vaccinationSeedEligibility()
	eligibility["species"] = append([]string(nil), species...)
	return eligibility
}

func pathogenClass(vaccineType string) string {
	switch strings.ToLower(strings.TrimSpace(vaccineType)) {
	case "live":
		return "live"
	case "killed":
		return "killed"
	case "toxoid":
		return "bacterial"
	default:
		return "unknown_review_needed"
	}
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
