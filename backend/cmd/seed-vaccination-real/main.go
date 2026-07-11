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
// per-goat vaccination history (accepted completions) and next-cycle obligations
// (scheduled/due). Date cells in the source are last-administered dates, not
// due dates; the seeder derives the next open work from those history rows.
// It never mutates the protocol/obligation SCHEMA.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/localtarget"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	processpg "github.com/vgoats/goatos/backend/internal/processintegrity/adapters/postgres"
	processdomain "github.com/vgoats/goatos/backend/internal/processintegrity/domain"
	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protocolapp "github.com/vgoats/goatos/backend/internal/protocol/app"
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
	RFID        string
	OldID       string
	OldIDSuffix string
	Farm        string
	Shed        string
	Stage       string
	Age         string
	Breed       string
	Gender      string
	DOB         string
	Status      string
	Health      string
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
	Due                int         // "Pending" -> open/due obligations
	Scheduled          int         // future -> scheduled obligations
	Skipped            int         // NA / blank cells
	Purged             purgeCounts // synthetic fixtures removed
}

type oblIns struct {
	obligationID, versionID, ruleID, goatID string
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

	st, err := seed(ctx, pool, *tenantID, loc, goats, cells, *purgeFixtures)
	if err != nil {
		return fmt.Errorf("seed: %w", err)
	}
	processProjection, err := processpg.NewRepository(pool, pgCfg.QueryTimeout).RecomputeProjection(ctx, processdomain.ProjectionRecomputeRequest{
		TenantID: *tenantID,
	})
	if err != nil {
		return fmt.Errorf("recompute process integrity projection after vaccination seed: %w", err)
	}

	fmt.Printf("seeded real vaccination data:\n"+
		"  parks_resolved=%d sheds_resolved=%d sheds_created=%d protocols=%d animals=%d\n"+
		"  obligations=%d (completed=%d due=%d scheduled=%d) completions_history=%d skipped_cells=%d\n"+
		"  purged_fixtures total=%d (calendar_projections=%d obligations=%d batches=%d goats=%d sheds=%d other_child_rows=%d)\n"+
		"  process_integrity_projection rows=%d version=%d as_of=%s\n",
		st.ParksResolved, st.ShedsResolved, st.ShedsCreated, st.Protocols, st.Animals,
		st.Obligations, st.Completed, st.Due, st.Scheduled, st.CompletionsHistory, st.Skipped,
		st.Purged.total(), st.Purged.CalendarProjections, st.Purged.Obligations, st.Purged.Batches,
		st.Purged.Goats, st.Purged.Sheds, st.Purged.OtherChildRows,
		processProjection.Rows, processProjection.ProjectionVersion, processProjection.AsOf.Format(time.RFC3339))
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
			RFID:        cell(row, col["rfid"]),
			OldID:       cell(row, col["old_id"]),
			OldIDSuffix: cell(row, col["old_id_suffix"]),
			Farm:        cell(row, col["farm"]),
			Shed:        cell(row, col["shed"]),
			Stage:       cell(row, col["stage"]),
			Age:         cell(row, col["age"]),
			Breed:       cell(row, col["breed"]),
			Gender:      cell(row, col["gender"]),
			DOB:         cell(row, col["dob"]),
			Status:      cell(row, col["status"]),
			Health:      cell(row, col["health_status"]),
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
			out = append(out, vaccCell{
				AnimalKey: animalKey,
				Vaccine:   cd.vaccine,
				DoseType:  cd.doseType,
				DoseCode:  doseCode(cd.doseType),
				Sequence:  doseSequence(cd.doseType),
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

func seed(ctx context.Context, pool *pgxpool.Pool, tenantID string, loc *time.Location, goats []goatRecord, cells []vaccCell, purgeFixtures bool) (stats, error) {
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

	// 3. Protocol config: one canonical vaccination.matrix protocol version with
	//    seven vaccine rows. The individual vaccines become rules inside the
	//    matrix, not seven top-level protocol definitions.
	versionByVaccine := map[string]string{}  // vaccine header -> protocol_version_id
	ruleByVaccineDose := map[string]string{} // vaccine|doseCode -> rule_id
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

	protocolService := protocolapp.NewService(protocolpg.NewRepository(pool, 0))
	if err := protocolService.PublishVersion(ctx, tenantID, versionID, nil, "seed-vaccination-real:"+versionID); err != nil {
		return st, fmt.Errorf("publish vaccination matrix through protocol service: %w", err)
	}

	tx, err = pool.Begin(ctx)
	if err != nil {
		return st, fmt.Errorf("begin animal seed tx: %w", err)
	}
	committed = false

	for _, vaccName := range vaccineOrder {
		def := vaccines[vaccName]
		versionByVaccine[vaccName] = versionID
		for _, dt := range doseTypesFor(vaccName) {
			var ruleID string
			if err := tx.QueryRow(ctx, `
				SELECT rule_id
				FROM protocol_rules
				WHERE tenant_id=$1 AND protocol_version_id=$2 AND dose_code=$3`,
				tenantID, versionID, matrixDoseCode(def, dt)).Scan(&ruleID); err != nil {
				return st, fmt.Errorf("resolve protocol rule %s/%s: %w", vaccName, dt, err)
			}
			ruleByVaccineDose[vaccName+"|"+doseCode(dt)] = ruleID
		}
	}
	st.Protocols = 1

	// 4. Animals: import each goat with shed_id + park_id (the read model groups on these).
	goatIDByAnimalKey := map[string]string{}
	goatLifecycleByAnimalKey := map[string]string{}
	type goatIns struct {
		goatID, animalKey, animalIdentifier1, animalIdentifier2, breed, sex, lifecycle, stage, age, shedID, parkID, dob string
		health                                                                                                          *string
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
		goatIDByAnimalKey[animalKey] = goatID
		goatLifecycleByAnimalKey[animalKey] = normalizeLifecycle(g.Status)
		goatRows = append(goatRows, goatIns{
			goatID:            goatID,
			animalKey:         animalKey,
			animalIdentifier1: animalIdentifier1,
			animalIdentifier2: animalIdentifier2,
			breed:             normalizeBreed(g.Breed),
			sex:               normalizeSex(g.Gender),
			lifecycle:         normalizeLifecycle(g.Status),
			stage:             normalizeStage(g.Stage, g.Age),
			age:               g.Age,
			health:            normalizeHealth(g.Health),
			shedID:            shedID,
			parkID:            parkByFarm[g.Farm],
			dob:               g.DOB,
		})
	}
	if err := batch(ctx, tx, goatRows, 500, func(b *pgx.Batch, gi goatIns) {
		b.Queue(`
			INSERT INTO goats (goat_id, tenant_id, species, breed, sex, lifecycle_status,
				health_status, origin_type, dob, current_location_id, shed_id, park_id, management_stage, age_band, custodian_party_id, updated_at)
			VALUES ($1,$2,'goat',$3,$4,$5,$6,'procured',$7,$8,$8,$9,$10,$11,$12,now())
			ON CONFLICT (goat_id) DO UPDATE SET breed=EXCLUDED.breed, sex=EXCLUDED.sex,
				lifecycle_status=EXCLUDED.lifecycle_status, health_status=EXCLUDED.health_status,
				shed_id=EXCLUDED.shed_id, park_id=EXCLUDED.park_id, current_location_id=EXCLUDED.current_location_id,
				management_stage=EXCLUDED.management_stage, age_band=EXCLUDED.age_band, updated_at=now()`,
			gi.goatID, tenantID, gi.breed, gi.sex, gi.lifecycle, gi.health,
			nullableDate(gi.dob), gi.shedID, gi.parkID, gi.stage, nullString(gi.age), custodianPartyID)
	}); err != nil {
		return st, fmt.Errorf("insert goats: %w", err)
	}
	st.Animals = len(goatRows)

	// Identifiers. animal_identifier_1 is the best available real-world animal ID;
	// animal_identifier_2 carries the secondary tag when both RFID and old/source tag exist.
	for _, gi := range goatRows {
		if _, err := tx.Exec(ctx, `
			INSERT INTO goat_identifiers (identifier_type, tenant_id, goat_id, identifier_value, normalized_value,
				scope_key, valid_from, normalizer_version, status)
			VALUES ('animal_identifier_1',$1,$2,$3,$4,'global',now()::date,'identifier_normalizer_v1','active')
			ON CONFLICT (tenant_id, normalized_value) DO NOTHING`,
			tenantID, gi.goatID, gi.animalIdentifier1, strings.ToLower(gi.animalIdentifier1)); err != nil {
			return st, fmt.Errorf("insert identifier %s: %w", gi.animalIdentifier1, err)
		}
		if gi.animalIdentifier2 == "" || strings.EqualFold(gi.animalIdentifier2, gi.animalIdentifier1) {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO goat_identifiers (identifier_type, tenant_id, goat_id, identifier_value, normalized_value,
				scope_key, valid_from, normalizer_version, status)
			VALUES ('animal_identifier_2',$1,$2,$3,$4,'global',now()::date,'identifier_normalizer_v1','active')
			ON CONFLICT (tenant_id, normalized_value) DO NOTHING`,
			tenantID, gi.goatID, gi.animalIdentifier2, strings.ToLower(gi.animalIdentifier2)); err != nil {
			return st, fmt.Errorf("insert identifier %s: %w", gi.animalIdentifier2, err)
		}
	}

	// 5. Obligations-first, then completions. Build the two batches, classifying each cell.
	var obls []oblIns
	var cmps []cmpIns

	for _, c := range cells {
		goatID, ok := goatIDByAnimalKey[c.AnimalKey]
		if !ok {
			continue // goat not placed (no shed) or not in herd sheet
		}
		versionID, ok := versionByVaccine[c.Vaccine]
		if !ok {
			continue
		}
		ruleID := ruleByVaccineDose[c.Vaccine+"|"+c.DoseCode]
		if ruleID == "" {
			continue
		}
		def := vaccines[c.Vaccine]
		oblID := detUUID("obligation", tenantID, c.AnimalKey, def.Code, c.DoseCode)
		oblIdem := "vacc-real-obl:" + c.AnimalKey + ":" + def.Code + ":" + c.DoseCode

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
			if !openEligible {
				st.Skipped++
				continue
			}
			// Open work due now.
			ws := startOfDay(now).AddDate(0, 0, -1)
			due := startOfDay(now).AddDate(0, 0, 7)
			obls = append(obls, oblIns{
				obligationID: oblID, versionID: versionID, ruleID: ruleID, goatID: goatID,
				dueAt: due, windowStart: &ws, status: "due", sequence: c.Sequence, idem: oblIdem,
			})
			st.Due++
		default:
			d, err := time.ParseInLocation("2006-01-02", val, loc)
			if err != nil {
				st.Skipped++
				continue
			}
			administeredAt := sourceVaccinationDateTime(d, loc)
			if !administeredAt.After(now) {
				// Source date cells are last-administered history, not due dates. Keep
				// accepted history for audit/proof, then derive the next open cycle.
				completedAt := administeredAt
				historyOblID := detUUID("obligation-history", tenantID, c.AnimalKey, def.Code, c.DoseCode, administeredAt.Format("2006-01-02"))
				obls = append(obls, oblIns{
					obligationID: historyOblID,
					versionID:    versionID,
					ruleID:       ruleID,
					goatID:       goatID,
					dueAt:        administeredAt,
					status:       "completed",
					completedAt:  &completedAt,
					sequence:     c.Sequence,
					idem:         "vacc-real-obl:history:" + c.AnimalKey + ":" + def.Code + ":" + c.DoseCode,
				})
				cmps = append(cmps, cmpIns{
					completionID:   detUUID("completion", tenantID, c.AnimalKey, def.Code, c.DoseCode),
					obligationID:   historyOblID,
					goatID:         goatID,
					doseML:         def.DoseML,
					administeredAt: administeredAt,
					idem:           "vacc-real-cmp:" + c.AnimalKey + ":" + def.Code + ":" + c.DoseCode,
				})
				st.Completed++
				st.CompletionsHistory++
				if !openEligible {
					continue
				}
				nextDue := nextDueAfterLastVaccination(administeredAt, c.Vaccine, now)
				obls = append(obls, oblIns{
					obligationID: detUUID("obligation-next", tenantID, c.AnimalKey, def.Code, c.DoseCode, nextDue.Format("2006-01-02")),
					versionID:    versionID, ruleID: ruleID, goatID: goatID,
					dueAt:    nextDue,
					status:   "scheduled",
					sequence: c.Sequence,
					idem:     "vacc-real-obl:next:" + c.AnimalKey + ":" + def.Code + ":" + c.DoseCode + ":" + nextDue.Format("2006-01-02"),
				})
				st.Scheduled++
			} else {
				if !openEligible {
					st.Skipped++
					continue
				}
				// Future dose -> scheduled.
				obls = append(obls, oblIns{
					obligationID: oblID, versionID: versionID, ruleID: ruleID, goatID: goatID,
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
			VALUES ($1,$2,$3,$4,'goat',$5,'tenant',$2,$6,$7,$8,$9,$10,$11,now(),now())
			ON CONFLICT (tenant_id, idempotency_key) DO NOTHING`,
			o.obligationID, tenantID, o.versionID, o.ruleID, o.goatID, o.dueAt, o.windowStart,
			o.status, o.completedAt, o.sequence, o.idem)
	}); err != nil {
		return st, fmt.Errorf("insert obligations: %w", err)
	}

	// Then completions linked to their obligation.
	if err := batch(ctx, tx, cmps, 500, func(b *pgx.Batch, cm cmpIns) {
		b.Queue(`
			INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, goat_id,
				doses, dose_ml_given, administered_at, status, idempotency_key, created_at, updated_at)
			VALUES ($1,$2,$3,$4,1,$5,$6,'accepted',$7,now(),now())
			ON CONFLICT (tenant_id, idempotency_key) DO NOTHING`,
			cm.completionID, tenantID, cm.obligationID, cm.goatID, cm.doseML, cm.administeredAt, cm.idem)
	}); err != nil {
		return st, fmt.Errorf("insert completions: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return st, fmt.Errorf("commit: %w", err)
	}
	committed = true
	return st, nil
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
//   - junk-named sheds ('Chain Proof%', 'Rework Proof%', 'Trusted History%');
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
	junkGoats := `(SELECT goat_id FROM goats WHERE tenant_id = $1 AND display_id ~ '^G-0000[0-9]{2}$')`
	junkSheds := `(SELECT location_id FROM locations WHERE tenant_id = $1 AND location_type = 'shed'
		AND (name ILIKE '%Chain Proof%' OR name ILIKE '%Rework Proof%' OR name ILIKE '%Trusted History%'))`
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
		AND (name ILIKE '%Chain Proof%' OR name ILIKE '%Rework Proof%' OR name ILIKE '%Trusted History%')`)
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
		DoseCode                  string  `json:"dose_code"`
		SourceDoseCode            string  `json:"source_dose_code"`
		Sequence                  int     `json:"sequence"`
		TriggerType               string  `json:"trigger_type"`
		OffsetDays                int     `json:"offset_days"`
		DueWindowDays             int     `json:"due_window_days"`
		DoseAmount                float64 `json:"dose_amount"`
		DoseUnit                  string  `json:"dose_unit"`
		VialDoses                 int     `json:"vial_doses"`
		RevaccinationIntervalDays int     `json:"revaccination_interval_days"`
		ScheduleNote              string  `json:"schedule_note,omitempty"`
		RouteSite                 string  `json:"route_site"`
		MaxDelayDays              int     `json:"max_delay_days"`
		CourseLapsePolicy         string  `json:"course_lapse_policy"`
		MinGapDays                int     `json:"min_gap_days"`
		Repeat                    string  `json:"repeat"`
		RepeatUntilAfterAge       string  `json:"repeat_until_after_age"`
		CatchUp                   string  `json:"catch_up"`
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
		Eligibility map[string]any `json:"eligibility"`
		Schedule    []scheduleRow  `json:"schedule"`
	}
	rows := make([]matrixRow, 0, len(vaccineOrder))
	schedule := []scheduleRow{}
	for _, vaccName := range vaccineOrder {
		def := vaccines[vaccName]
		rowSchedule := []scheduleRow{}
		for _, dt := range doseTypesFor(vaccName) {
			dose := matrixDoseCode(def, dt)
			cell := scheduleRow{
				DoseCode:                  dose,
				SourceDoseCode:            dose,
				Sequence:                  vaccineSortOrder(vaccName) + doseSequence(dt),
				TriggerType:               "birth_age",
				OffsetDays:                offsetDays(dt),
				DueWindowDays:             7,
				DoseAmount:                def.DoseML,
				DoseUnit:                  "ml",
				VialDoses:                 def.VialDoses,
				RevaccinationIntervalDays: revaccinationIntervalDays(vaccName),
				ScheduleNote:              "real vaccination seed source matrix",
				RouteSite:                 "subcutaneous",
				MaxDelayDays:              7,
				CourseLapsePolicy:         "preventive_care_review",
				MinGapDays:                minGapDays(dt),
				Repeat:                    "none",
				RepeatUntilAfterAge:       "-",
				CatchUp:                   "immediate",
			}
			rowSchedule = append(rowSchedule, cell)
			schedule = append(schedule, cell)
		}
		courseType := "single"
		if len(rowSchedule) > 1 {
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
			Eligibility: vaccinationSeedEligibility(),
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
		"eligibility":        vaccinationSeedEligibility(),
		"missed_dose_policy": "immediate",
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

func vaccinationSeedEligibility() map[string]any {
	return map[string]any{
		"species":                     []string{"goat", "sheep"},
		"animal_stage":                []string{"all"},
		"sex":                         []string{"female", "male"},
		"breed":                       []string{"all"},
		"lifecycle":                   []string{"alive"},
		"health":                      []string{"healthy"},
		"reproductive":                []string{"any"},
		"exclude_reproductive_states": []string{"pregnant_late"},
		"defer_states":                []string{"icu", "quarantine"},
	}
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

func revaccinationIntervalDays(vaccName string) int {
	switch vaccName {
	case "FMD":
		return 274
	case "PPR":
		return 1095
	case "ET+TT":
		return 182
	default:
		return 365
	}
}

func sourceVaccinationDateTime(d time.Time, loc *time.Location) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), 9, 0, 0, 0, loc)
}

func nextDueAfterLastVaccination(lastAdministeredAt time.Time, vaccName string, asOf time.Time) time.Time {
	intervalDays := revaccinationIntervalDays(vaccName)
	nextDue := lastAdministeredAt.AddDate(0, 0, intervalDays)
	for !nextDue.After(asOf) {
		nextDue = nextDue.AddDate(0, 0, intervalDays)
	}
	return nextDue
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

func matrixDoseCode(def vaccineDef, doseType string) string {
	return def.Code + "_" + doseCode(doseType)
}

func doseCode(doseType string) string {
	if strings.EqualFold(strings.TrimSpace(doseType), "Booster") {
		return "booster"
	}
	return "first"
}

func doseSequence(doseType string) int {
	if strings.EqualFold(strings.TrimSpace(doseType), "Booster") {
		return 2
	}
	return 1
}

func offsetDays(doseType string) int {
	if strings.EqualFold(strings.TrimSpace(doseType), "Booster") {
		return 56
	}
	return 28
}

func minGapDays(doseType string) int {
	if strings.EqualFold(strings.TrimSpace(doseType), "Booster") {
		return 21
	}
	return 0
}

func vaccineSortOrder(vaccName string) int {
	for i, name := range vaccineOrder {
		if name == vaccName {
			return (i + 1) * 10
		}
	}
	return 999
}

// doseTypesFor lists the dose columns present for a vaccine in the source sheet.
func doseTypesFor(vaccName string) []string {
	switch vaccName {
	case "ET+TT", "Blue tongue", "FMD":
		return []string{"First Dose", "Booster"}
	default:
		return []string{"First Dose"}
	}
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
