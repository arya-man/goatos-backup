// Command seed-vaccination-real imports the real captured herd + vaccination
// spreadsheet snapshot into the local/dev GoatOS schema so the /vaccination
// operations read model renders real cohorts x the 7 vaccine protocols with
// honest up-to-date / due / overdue / scheduled statuses.
//
// Source data (gitignored, read at runtime, never committed):
//   - <source>/goats.json       Sheets API {"values":[[hdr...],[row...]]}
//   - <source>/vaccination.json Sheets API, TWO header rows (vaccine, dose)
//
// It is idempotent (deterministic v5 UUIDs + ON CONFLICT), batched for scale,
// and uses Asia/Kolkata for every business date. It seeds ONLY to the existing
// schema: parks/sheds (creates missing sheds park-scoped), animals, the 7
// vaccine protocol definitions/versions/rules, per-goat vaccination history
// (accepted completions) and obligations (scheduled/due). It never mutates the
// protocol/obligation SCHEMA or business rules.
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
)

const defaultTenantID = "00000000-0000-4000-8000-000000000001"

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
	RFID   string
	Farm   string
	Shed   string
	Stage  string
	Age    string
	Breed  string
	Gender string
	DOB    string
	Status string
	Health string
}

// vaccCell is one goat x vaccine x dose spreadsheet cell.
type vaccCell struct {
	RFID     string
	Vaccine  string // source vaccine header
	DoseType string // "First Dose" | "Booster"
	DoseCode string // "first" | "booster"
	Sequence int
	Value    string // date | "Pending" | "NA" | ""
}

type stats struct {
	ParksResolved      int
	ShedsResolved      int
	ShedsCreated       int
	Protocols          int
	Animals            int
	Obligations        int
	Completed          int // completed obligations (past history)
	CompletionsHistory int // vaccination_completions rows (accepted doses)
	Due                int // "Pending" -> open/due obligations
	Scheduled          int // future -> scheduled obligations
	Skipped            int // NA / blank cells
	PurgedTriggerSeed  int // dev trigger-seed vaccination obligations removed
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
	purgeTriggerSeed := fs.Bool("purge-trigger-seed", true, "purge dev trigger-seed vaccination obligations (protocol code vaccination.matrix*) so the /vaccination matrix shows only real data")
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
	if err := localtarget.ValidateLocalDatabaseTarget("seed-vaccination-real", os.Getenv("GOATOS_ENV"), pgCfg.DatabaseURL, "local", "dev", "test"); err != nil {
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

	st, err := seed(ctx, pool, *tenantID, loc, goats, cells, *purgeTriggerSeed)
	if err != nil {
		return fmt.Errorf("seed: %w", err)
	}

	fmt.Printf("seeded real vaccination data:\n"+
		"  parks_resolved=%d sheds_resolved=%d sheds_created=%d protocols=%d animals=%d\n"+
		"  obligations=%d (completed=%d due=%d scheduled=%d) completions_history=%d skipped_cells=%d\n"+
		"  purged_trigger_seed_obligations=%d\n",
		st.ParksResolved, st.ShedsResolved, st.ShedsCreated, st.Protocols, st.Animals,
		st.Obligations, st.Completed, st.Due, st.Scheduled, st.CompletionsHistory, st.Skipped,
		st.PurgedTriggerSeed)
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
			RFID:   cell(row, col["rfid"]),
			Farm:   cell(row, col["farm"]),
			Shed:   cell(row, col["shed"]),
			Stage:  cell(row, col["stage"]),
			Age:    cell(row, col["age"]),
			Breed:  cell(row, col["breed"]),
			Gender: cell(row, col["gender"]),
			DOB:    cell(row, col["dob"]),
			Status: cell(row, col["status"]),
			Health: cell(row, col["health_status"]),
		}
		if rec.RFID != "" && rec.Farm != "" {
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
	var out []vaccCell
	for _, row := range values[2:] {
		if len(row) == 0 {
			continue
		}
		rfid := cell(row, rfidCol)
		if rfid == "" {
			continue
		}
		for i, cd := range colDefs {
			out = append(out, vaccCell{
				RFID:     rfid,
				Vaccine:  cd.vaccine,
				DoseType: cd.doseType,
				DoseCode: doseCode(cd.doseType),
				Sequence: doseSequence(cd.doseType),
				Value:    cell(row, i),
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

func seed(ctx context.Context, pool *pgxpool.Pool, tenantID string, loc *time.Location, goats []goatRecord, cells []vaccCell, purgeTriggerSeed bool) (stats, error) {
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
	defer tx.Rollback(ctx)

	// 0. Purge dev trigger-seed vaccination fixtures (data cleanup, not schema/rule change) so the
	//    /vaccination operations matrix renders only real herd data. Targets ONLY the trigger-seed
	//    protocol family (code vaccination.matrix*) against synthetic fixture goats.
	if purgeTriggerSeed {
		n, err := purgeTriggerSeedVaccination(ctx, tx, tenantID)
		if err != nil {
			return st, fmt.Errorf("purge trigger-seed fixtures: %w", err)
		}
		st.PurgedTriggerSeed = n
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

	// 3. Protocols: 7 vaccine protocols, each draft -> per-dose rules -> published.
	versionByVaccine := map[string]string{}  // vaccine header -> protocol_version_id
	ruleByVaccineDose := map[string]string{} // vaccine|doseCode -> rule_id
	for _, vaccName := range vaccineOrder {
		def := vaccines[vaccName]
		protocolID := detUUID("protocol", tenantID, def.Code)
		versionID := detUUID("protocol_version", tenantID, def.Code)
		itemID := detUUID("inventory_item", tenantID, def.Code)

		if _, err := tx.Exec(ctx, `
			INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
			VALUES ($1,$2,$3,$4,'vaccination','active')
			ON CONFLICT (protocol_id) DO UPDATE SET name=EXCLUDED.name, status='active', updated_at=now()`,
			protocolID, tenantID, "vaccination."+def.Code, def.Name); err != nil {
			return st, fmt.Errorf("protocol def %s: %w", vaccName, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit, status)
			VALUES ($1,$2,$3,$4,'vaccine','dose','active')
			ON CONFLICT (tenant_id, item_code) DO UPDATE SET name=EXCLUDED.name, status='active', updated_at=now()`,
			itemID, tenantID, def.ItemCode, def.Name+" vaccine"); err != nil {
			return st, fmt.Errorf("inventory item %s: %w", vaccName, err)
		}

		// Only (re)build config while the version is new; published config is immutable.
		var status string
		qerr := tx.QueryRow(ctx, `SELECT status FROM protocol_versions WHERE protocol_version_id=$1`, versionID).Scan(&status)
		if qerr == pgx.ErrNoRows {
			ruleDSL := fmt.Sprintf(`{"vaccine":{"code":%q,"name":%q,"type":%q,"disease":%q,"dose_ml":%g,"vial_doses":%d},"reference":"docs/preventive-care-vaccination/vaccination-rules.md"}`,
				def.Code, def.Name, def.Type, def.Disease, def.DoseML, def.VialDoses)
			if _, err := tx.Exec(ctx, `
				INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, scope_id, version,
					version_label, status, effective_from, effective_to, rule_dsl, proof_policy)
				VALUES ($1,$2,$3,'tenant',NULL,1,'Real herd import','draft',DATE '2024-01-01',NULL,$4::jsonb,'{"required_proofs":["shed","vial_lot","administration"]}'::jsonb)`,
				versionID, tenantID, protocolID, ruleDSL); err != nil {
				return st, fmt.Errorf("protocol version %s: %w", vaccName, err)
			}
			for _, dt := range doseTypesFor(vaccName) {
				ruleID := detUUID("protocol_rule", tenantID, def.Code, doseCode(dt))
				if _, err := tx.Exec(ctx, `
					INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type,
						offset_days, due_window_days, min_gap_days, repeat, catch_up, eligibility_json, sort_order)
					VALUES ($1,$2,$3,$4,$5,'birth_age',28,7,0,'none','immediate',
						'{"stage":"all","sex":"all","breed":"all","lifecycle":"alive"}'::jsonb,$6)
					ON CONFLICT (rule_id) DO NOTHING`,
					ruleID, tenantID, versionID, def.Code+"_"+doseCode(dt), doseSequence(dt), doseSequence(dt)*10); err != nil {
					return st, fmt.Errorf("protocol rule %s/%s: %w", vaccName, dt, err)
				}
			}
			if _, err := tx.Exec(ctx, `
				UPDATE protocol_versions SET status='published', published_at=now(), updated_at=now()
				WHERE protocol_version_id=$1 AND status='draft'`, versionID); err != nil {
				return st, fmt.Errorf("publish version %s: %w", vaccName, err)
			}
		} else if qerr != nil {
			return st, fmt.Errorf("lookup version %s: %w", vaccName, qerr)
		}

		versionByVaccine[vaccName] = versionID
		for _, dt := range doseTypesFor(vaccName) {
			ruleByVaccineDose[vaccName+"|"+doseCode(dt)] = detUUID("protocol_rule", tenantID, def.Code, doseCode(dt))
		}
		st.Protocols++
	}

	// 4. Animals: import each goat with shed_id + park_id (the read model groups on these).
	goatIDByRFID := map[string]string{}
	goatLifecycleByRFID := map[string]string{}
	type goatIns struct {
		goatID, displayID, breed, sex, lifecycle, stage, age, shedID, parkID, dob string
		health                                                                    *string
	}
	var goatRows []goatIns
	for _, g := range goats {
		shedID, ok := shedByKey[shedKey{g.Farm, g.Shed}]
		if !ok {
			// No shed (blank) -> cannot place in a cohort; skip animal placement.
			continue
		}
		goatID := detUUID("goat", tenantID, g.RFID)
		goatIDByRFID[g.RFID] = goatID
		goatLifecycleByRFID[g.RFID] = normalizeLifecycle(g.Status)
		goatRows = append(goatRows, goatIns{
			goatID:    goatID,
			displayID: "G-" + g.RFID,
			breed:     normalizeBreed(g.Breed),
			sex:       normalizeSex(g.Gender),
			lifecycle: normalizeLifecycle(g.Status),
			stage:     g.Stage,
			age:       g.Age,
			health:    normalizeHealth(g.Health),
			shedID:    shedID,
			parkID:    parkByFarm[g.Farm],
			dob:       g.DOB,
		})
	}
	if err := batch(ctx, tx, goatRows, 500, func(b *pgx.Batch, gi goatIns) {
		b.Queue(`
			INSERT INTO goats (goat_id, tenant_id, display_id, species, breed, sex, lifecycle_status,
				health_status, origin_type, dob, current_location_id, shed_id, park_id, management_stage, age_band, custodian_party_id, updated_at)
			VALUES ($1,$2,$3,'goat',$4,$5,$6,$7,'procured',$8,$9,$9,$10,$11,$12,$13,now())
			ON CONFLICT (goat_id) DO UPDATE SET breed=EXCLUDED.breed, sex=EXCLUDED.sex,
				lifecycle_status=EXCLUDED.lifecycle_status, health_status=EXCLUDED.health_status,
				shed_id=EXCLUDED.shed_id, park_id=EXCLUDED.park_id, current_location_id=EXCLUDED.current_location_id,
				management_stage=EXCLUDED.management_stage, age_band=EXCLUDED.age_band, updated_at=now()`,
			gi.goatID, tenantID, gi.displayID, gi.breed, gi.sex, gi.lifecycle, gi.health,
			nullableDate(gi.dob), gi.shedID, gi.parkID, gi.stage, nullString(gi.age), custodianPartyID)
	}); err != nil {
		return st, fmt.Errorf("insert goats: %w", err)
	}
	st.Animals = len(goatRows)

	// Identifiers (RFID as animal_identifier_1). Best-effort, dedup on value.
	for _, gi := range goatRows {
		rfid := strings.TrimPrefix(gi.displayID, "G-")
		if _, err := tx.Exec(ctx, `
			INSERT INTO goat_identifiers (identifier_type, tenant_id, goat_id, identifier_value, normalized_value,
				scope_key, valid_from, normalizer_version, status)
			VALUES ('animal_identifier_1',$1,$2,$3,$4,'global',now()::date,'identifier_normalizer_v1','active')
			ON CONFLICT (tenant_id, normalized_value) DO NOTHING`,
			tenantID, gi.goatID, rfid, strings.ToLower(rfid)); err != nil {
			return st, fmt.Errorf("insert identifier %s: %w", rfid, err)
		}
	}

	// 5. Obligations-first, then completions. Build the two batches, classifying each cell.
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
	var obls []oblIns
	var cmps []cmpIns

	for _, c := range cells {
		goatID, ok := goatIDByRFID[c.RFID]
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
		oblID := detUUID("obligation", tenantID, c.RFID, def.Code, c.DoseCode)
		oblIdem := "vacc-real-obl:" + c.RFID + ":" + def.Code + ":" + c.DoseCode

		// Open (scheduled/due) vaccination obligations are blocked by the procurement
		// exclusion guard for goats that are dead/sold/lost/culled/transferred/merged/inactive.
		// Completed history is still allowed for those goats.
		openEligible := !excludedLifecycle(goatLifecycleByRFID[c.RFID])

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
			dueAt := time.Date(d.Year(), d.Month(), d.Day(), 9, 0, 0, 0, loc)
			if dueAt.Before(now) {
				// Past dose -> completed history (allowed even for excluded goats).
				completedAt := dueAt
				obls = append(obls, oblIns{
					obligationID: oblID, versionID: versionID, ruleID: ruleID, goatID: goatID,
					dueAt: dueAt, status: "completed", completedAt: &completedAt, sequence: c.Sequence, idem: oblIdem,
				})
				cmps = append(cmps, cmpIns{
					completionID:   detUUID("completion", tenantID, c.RFID, def.Code, c.DoseCode),
					obligationID:   oblID,
					goatID:         goatID,
					doseML:         def.DoseML,
					administeredAt: dueAt,
					idem:           "vacc-real-cmp:" + c.RFID + ":" + def.Code + ":" + c.DoseCode,
				})
				st.Completed++
				st.CompletionsHistory++
			} else {
				if !openEligible {
					st.Skipped++
					continue
				}
				// Future dose -> scheduled.
				obls = append(obls, oblIns{
					obligationID: oblID, versionID: versionID, ruleID: ruleID, goatID: goatID,
					dueAt: dueAt, status: "scheduled", sequence: c.Sequence, idem: oblIdem,
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
	return st, nil
}

// purgeTriggerSeedVaccination removes the dev trigger-seed vaccination obligations (and their
// completions / status events / escalations) so the operations matrix shows only real protocols.
// It targets ONLY protocol definitions whose code starts with 'vaccination.matrix' (the
// seed-vaccination-trigger family). It never deletes protocol config, goats, workforce, or grants.
func purgeTriggerSeedVaccination(ctx context.Context, tx pgx.Tx, tenantID string) (int, error) {
	const junkIDs = `
		SELECT oi.obligation_id
		FROM obligation_instances oi
		JOIN protocol_versions pv ON pv.tenant_id = oi.tenant_id AND pv.protocol_version_id = oi.protocol_version_id
		JOIN protocol_definitions pd ON pd.tenant_id = pv.tenant_id AND pd.protocol_id = pv.protocol_id
		WHERE oi.tenant_id = $1 AND pd.category = 'vaccination' AND pd.code LIKE 'vaccination.matrix%'`
	for _, child := range []string{"vaccination_completions", "obligation_status_events", "obligation_escalations", "feed_direction_completions"} {
		if _, err := tx.Exec(ctx, `DELETE FROM `+child+` WHERE tenant_id = $1 AND obligation_id IN (`+junkIDs+`)`, tenantID); err != nil {
			return 0, fmt.Errorf("delete %s: %w", child, err)
		}
	}
	tag, err := tx.Exec(ctx, `DELETE FROM obligation_instances WHERE tenant_id = $1 AND obligation_id IN (`+junkIDs+`)`, tenantID)
	if err != nil {
		return 0, fmt.Errorf("delete obligation_instances: %w", err)
	}
	return int(tag.RowsAffected()), nil
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

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
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

func nullString(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
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
