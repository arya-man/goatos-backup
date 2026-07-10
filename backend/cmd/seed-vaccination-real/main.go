package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/localtarget"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

const (
	defaultTenantID = "00000000-0000-4000-8000-000000000001"
)

// Vaccine definitions from vaccination-rules.md
var vaccines = map[string]VaccineDef{
	"ET+TT": {
		Code:     "et_tt",
		Name:     "ET+TT (Enterotoxaemia + Tetanus)",
		Disease:  "Enterotoxaemia + Tetanus",
		Type:     "toxoid",
		ItemCode: "VAC-ET-TT",
		DoseML:   2.0,
		VialDoses: 100,
	},
	"PPR": {
		Code:      "ppr",
		Name:      "PPR (Peste des Petits Ruminants)",
		Disease:   "Peste des Petits Ruminants",
		Type:      "live",
		ItemCode:  "VAC-PPR",
		DoseML:    1.0,
		VialDoses: 100,
	},
	"Blue tongue": {
		Code:      "blue_tongue",
		Name:      "Blue Tongue",
		Disease:   "Blue Tongue",
		Type:      "killed",
		ItemCode:  "VAC-BT",
		DoseML:    2.0,
		VialDoses: 100,
	},
	"Goat Pox": {
		Code:      "goat_pox",
		Name:      "Goat Pox",
		Disease:   "Goat Pox",
		Type:      "live",
		ItemCode:  "VAC-GP",
		DoseML:    1.0,
		VialDoses: 25,
	},
	"Sheep Pox": {
		Code:      "sheep_pox",
		Name:      "Sheep Pox",
		Disease:   "Sheep Pox",
		Type:      "live",
		ItemCode:  "VAC-SP",
		DoseML:    1.0,
		VialDoses: 100,
	},
	"FMD": {
		Code:      "fmd",
		Name:      "FMD (Foot and Mouth Disease)",
		Disease:   "Foot and Mouth Disease",
		Type:      "killed",
		ItemCode:  "VAC-FMD",
		DoseML:    1.0,
		VialDoses: 30,
	},
	"HS": {
		Code:      "hs",
		Name:      "HS (Haemorrhagic Septicaemia)",
		Disease:   "Haemorrhagic Septicaemia",
		Type:      "killed",
		ItemCode:  "VAC-HS",
		DoseML:    2.0,
		VialDoses: 100,
	},
}

type VaccineDef struct {
	Code      string
	Name      string
	Disease   string
	Type      string
	ItemCode  string
	DoseML    float64
	VialDoses int
}

type GoatRecord struct {
	RFID      string
	GoatID    string
	Farm      string
	Shed      string
	Stage     string
	Age       string
	Breed     string
	Gender    string
	DOB       string
	Status    string
	Weight    string
}

type VaccinationRecord struct {
	RFID      string
	Farm      string
	Shed      string
	Vaccines  map[string]map[string]string // vaccine -> (dose type -> date/status)
}

type Stats struct {
	Parks              int
	Sheds              int
	Cohorts            int
	Protocols          int
	Animals            int
	VaccinationHistory int
	Obligations        int
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
	timeout := fs.Duration("timeout", 120*time.Second, "seed timeout")
	sourcePath := fs.String("source", "/Users/ravi/mesha/source-material/vgoats-seed", "path to source data directory")

	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	pgCfg := platformpg.ConfigFromEnv()
	if err := validateTarget(os.Getenv("GOATOS_ENV"), pgCfg.DatabaseURL); err != nil {
		return err
	}

	pool, err := platformpg.Connect(ctx, pgCfg)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer pool.Close()

	// Load source data
	goats, err := loadGoats(ctx, *sourcePath)
	if err != nil {
		return fmt.Errorf("failed to load goats data: %w", err)
	}

	vaccinations, err := loadVaccinations(ctx, *sourcePath)
	if err != nil {
		return fmt.Errorf("failed to load vaccination data: %w", err)
	}

	// Run seeding
	stats, err := seedData(ctx, pool, *tenantID, goats, vaccinations)
	if err != nil {
		return fmt.Errorf("seeding failed: %w", err)
	}

	fmt.Printf("✓ Seeded real vaccination data: parks=%d sheds=%d cohorts=%d protocols=%d animals=%d vaccination_history=%d obligations=%d\n",
		stats.Parks, stats.Sheds, stats.Cohorts, stats.Protocols, stats.Animals, stats.VaccinationHistory, stats.Obligations)
	return nil
}

func loadGoats(ctx context.Context, sourcePath string) ([]GoatRecord, error) {
	data, err := os.ReadFile(sourcePath + "/goats.json")
	if err != nil {
		return nil, err
	}

	var payload struct {
		Values [][]interface{} `json:"values"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}

	if len(payload.Values) < 2 {
		return nil, fmt.Errorf("invalid goats.json: insufficient rows")
	}

	// Find column indices
	headers := payload.Values[0]
	colIdx := make(map[string]int)
	for i, h := range headers {
		if h != nil {
			colIdx[h.(string)] = i
		}
	}

	var records []GoatRecord
	for _, row := range payload.Values[1:] {
		if len(row) == 0 {
			continue
		}

		rec := GoatRecord{
			RFID:   toString(row, colIdx["rfid"]),
			GoatID: toString(row, colIdx["goat_id"]),
			Farm:   toString(row, colIdx["farm"]),
			Shed:   toString(row, colIdx["shed"]),
			Stage:  toString(row, colIdx["stage"]),
			Age:    toString(row, colIdx["age"]),
			Breed:  toString(row, colIdx["breed"]),
			Gender: toString(row, colIdx["gender"]),
			DOB:    toString(row, colIdx["dob"]),
			Status: toString(row, colIdx["status"]),
			Weight: toString(row, colIdx["latest_weight"]),
		}

		if rec.RFID != "" && rec.Farm != "" {
			records = append(records, rec)
		}
	}

	return records, nil
}

func loadVaccinations(ctx context.Context, sourcePath string) ([]VaccinationRecord, error) {
	data, err := os.ReadFile(sourcePath + "/vaccination.json")
	if err != nil {
		return nil, err
	}

	var payload struct {
		Values [][]interface{} `json:"values"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}

	if len(payload.Values) < 3 {
		return nil, fmt.Errorf("invalid vaccination.json: insufficient rows")
	}

	// Parse headers (2 rows: vaccine names + dose types)
	vaccineNames := payload.Values[0]
	doseTypes := payload.Values[1]

	// Find column indices for meta columns
	colIdx := make(map[string]int)
	for i, h := range vaccineNames {
		if h != nil {
			colIdx[h.(string)] = i
		}
	}

	var records []VaccinationRecord
	for _, row := range payload.Values[2:] {
		if len(row) < 5 {
			continue
		}

		rfid := toString(row, colIdx["RFID"])
		farm := toString(row, colIdx["Farm"])
		shed := toString(row, colIdx["Shed"])

		if rfid == "" {
			continue
		}

		rec := VaccinationRecord{
			RFID:     rfid,
			Farm:     farm,
			Shed:     shed,
			Vaccines: make(map[string]map[string]string),
		}

		// Parse vaccine columns
		for i := 11; i < len(vaccineNames) && i < len(row); i++ {
			if vaccineNames[i] == nil || vaccineNames[i].(string) == "" {
				continue
			}

			vaccineName := vaccineNames[i].(string)
			doseType := doseTypes[i].(string)
			value := toString(row, i)

			if _, ok := vaccines[vaccineName]; !ok {
				continue // Skip unknown vaccines
			}

			if rec.Vaccines[vaccineName] == nil {
				rec.Vaccines[vaccineName] = make(map[string]string)
			}
			rec.Vaccines[vaccineName][doseType] = value
		}

		if len(rec.Vaccines) > 0 {
			records = append(records, rec)
		}
	}

	return records, nil
}

func seedData(ctx context.Context, pool *pgxpool.Pool, tenantID string, goats []GoatRecord, vaccinations []VaccinationRecord) (Stats, error) {
	var stats Stats

	// Get the default party (use Mesha organization)
	var defaultPartyID string
	err := pool.QueryRow(ctx, `
		SELECT party_id FROM parties
		WHERE display_name = 'Mesha'
		LIMIT 1
	`).Scan(&defaultPartyID)
	if err != nil {
		return stats, fmt.Errorf("failed to get default party: %w", err)
	}

	// Start transaction
	tx, err := pool.Begin(ctx)
	if err != nil {
		return stats, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Load existing parks
	parkMap := make(map[string]string) // farm code -> park_id
	parks := distinctFarms(goats)

	// Query existing parks by location_code
	for _, farmCode := range parks {
		var parkID string
		err := tx.QueryRow(ctx, `
			SELECT location_id FROM locations
			WHERE tenant_id = $1 AND location_code = $2 AND location_type = 'park'
			LIMIT 1
		`, tenantID, farmCode).Scan(&parkID)

		if err == nil {
			parkMap[farmCode] = parkID
			stats.Parks++
		} else {
			// If park doesn't exist, log and skip
			fmt.Printf("Warning: park %s not found in database\n", farmCode)
		}
	}

	// Load existing sheds
	shedMap := make(map[string]string) // shed name -> shed_id
	sheds := distinctSheds(goats)
	for _, shedName := range sheds {
		// Query for shed by name
		var shedID string
		err := tx.QueryRow(ctx, `
			SELECT location_id FROM locations
			WHERE tenant_id = $1 AND name = $2 AND location_type = 'shed'
			LIMIT 1
		`, tenantID, shedName).Scan(&shedID)

		if err == nil {
			shedMap[shedName] = shedID
			stats.Sheds++
		} else {
			// If shed doesn't exist, log and skip
			fmt.Printf("Warning: shed %s not found in database\n", shedName)
		}
	}

	// Create protocols for each vaccine
	protocolMap := make(map[string]string) // vaccine code -> protocol_version_id
	for vaccName, vaccDef := range vaccines {
		protocolID := vaccineCodeToUUID(vaccDef.Code)
		versionID := vaccineVersionToUUID(vaccDef.Code)

		// Insert protocol definition
		_, err := tx.Exec(ctx, `
			INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
			VALUES ($1, $2, $3, $4, 'vaccination', 'active')
			ON CONFLICT (protocol_id) DO UPDATE SET code = EXCLUDED.code, name = EXCLUDED.name, status = 'active'
		`, protocolID, tenantID, vaccDef.Code, vaccName)
		if err != nil {
			return stats, fmt.Errorf("failed to insert protocol definition for %s: %w", vaccName, err)
		}

		// Insert inventory item
		_, err = tx.Exec(ctx, `
			INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit, status)
			VALUES ($1, $2, $3, $4, 'vaccine', 'dose', 'active')
			ON CONFLICT (item_id) DO UPDATE SET item_code = EXCLUDED.item_code, status = 'active'
		`, vaccineCodeToItemUUID(vaccDef.Code), tenantID, vaccDef.ItemCode, vaccName)
		if err != nil {
			return stats, fmt.Errorf("failed to insert inventory item for %s: %w", vaccName, err)
		}

		// Insert protocol version (tenant scope with NULL scope_id per protocol_versions_scope_shape_check)
		// Insert as draft first, then add rules, then publish
		ruleDSL := fmt.Sprintf(`{
			"vaccine":{"code":"%s","name":"%s","type":"%s","disease":"%s"},
			"schedule":[{"dose_code":"%s_dose","trigger_type":"birth_age","offset_days":28}]
		}`, vaccDef.Code, vaccName, vaccDef.Type, vaccDef.Disease, vaccDef.Code)

		// Check if version already exists
		var existingStatus string
		err = tx.QueryRow(ctx, `
			SELECT status FROM protocol_versions WHERE protocol_version_id = $1
		`, versionID).Scan(&existingStatus)

		if err != nil {
			// New version - insert as draft
			_, err = tx.Exec(ctx, `
				INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, scope_id, version,
					version_label, status, effective_from, effective_to, rule_dsl, proof_policy)
				VALUES ($1, $2, $3, 'tenant', NULL, 1, 'V1 Real Vaccination', 'draft',
					DATE '2026-01-01', DATE '2028-12-31', $4::jsonb, '{"required_proofs":[]}'::jsonb)
			`, versionID, tenantID, protocolID, ruleDSL)
			if err != nil {
				return stats, fmt.Errorf("failed to insert protocol version for %s: %w", vaccName, err)
			}

			// Insert protocol rule
			ruleID := vaccRuleIDToUUID(vaccDef.Code)
			_, err = tx.Exec(ctx, `
				INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type,
					offset_days, due_window_days, min_gap_days, repeat, catch_up, eligibility_json, sort_order)
				VALUES ($1, $2, $3, $4, 1, 'birth_age', 28, 7, 0, 'none', 'immediate',
					'{"stage":"K2","sex":"all","breed":"all","lifecycle":"alive"}'::jsonb, 10)
				ON CONFLICT DO NOTHING
			`, ruleID, tenantID, versionID, vaccDef.Code)
			if err != nil {
				return stats, fmt.Errorf("failed to insert protocol rule for %s: %w", vaccName, err)
			}

			// Now publish
			_, err = tx.Exec(ctx, `
				UPDATE protocol_versions
				SET status = 'published', published_at = now(), updated_at = now()
				WHERE protocol_version_id = $1 AND status = 'draft'
			`, versionID)
			if err != nil {
				return stats, fmt.Errorf("failed to publish protocol version for %s: %w", vaccName, err)
			}
		}

		protocolMap[vaccDef.Code] = versionID
		stats.Protocols++
	}

	// Import animals
	goatMap := make(map[string]string) // rfid -> goat_id
	for _, g := range goats {
		// Skip if shed not found
		shedID, ok := shedMap[g.Shed]
		if !ok {
			continue // Skip goats in sheds that don't exist
		}

		goatID := rfidToUUID(g.RFID)
		goatMap[g.RFID] = goatID

		// Display ID format: G-NNNNNN (6+ digits)
		displayID := fmt.Sprintf("G-%s", g.RFID)
		lifecycle := "alive"
		if strings.ToLower(g.Status) == "dead" {
			lifecycle = "dead"
		}

		gender := normalizeGender(g.Gender)
		breed := normalizeBreed(g.Breed)

		_, err := tx.Exec(ctx, `
			INSERT INTO goats (goat_id, tenant_id, display_id, species, breed, sex,
				lifecycle_status, origin_type, dob, current_location_id, management_stage, custodian_party_id, updated_at)
			VALUES ($1, $2, $3, 'goat', $4, $5, $6, 'procured', $7::date, $8, $9, $10, now())
			ON CONFLICT (goat_id) DO UPDATE SET breed = EXCLUDED.breed, sex = EXCLUDED.sex,
				lifecycle_status = EXCLUDED.lifecycle_status, updated_at = now()
		`, goatID, tenantID, displayID, breed, gender, lifecycle, parseDate(g.DOB), shedID, g.Stage, defaultPartyID)
		if err != nil {
			return stats, fmt.Errorf("failed to insert goat %s: %w", g.RFID, err)
		}

		// Insert animal identifier
		_, err = tx.Exec(ctx, `
			INSERT INTO goat_identifiers (identifier_type, tenant_id, goat_id, identifier_value, normalized_value, scope_key, valid_from, normalizer_version, status)
			VALUES ('animal_identifier_1', $1, $2, $3, $4, 'global', now()::date, 'identifier_normalizer_v1', 'active')
			ON CONFLICT (tenant_id, normalized_value) DO NOTHING
		`, tenantID, goatID, g.RFID, strings.ToLower(g.RFID))
		if err != nil {
			return stats, fmt.Errorf("failed to insert goat identifier %s: %w", g.RFID, err)
		}

		stats.Animals++
	}

	// Process vaccinations
	for _, vacc := range vaccinations {
		goatID, ok := goatMap[vacc.RFID]
		if !ok {
			continue // Skip if goat not found
		}

		for vaccName, doses := range vacc.Vaccines {
			vaccDef, ok := vaccines[vaccName]
			if !ok {
				continue
			}

			versionID := protocolMap[vaccDef.Code]

			// Process doses
			for doseType, dateStr := range doses {
				if dateStr == "" {
					continue
				}

				// Determine if it's history (past) or obligation (future)
				dateVal := parseDate(dateStr)
				if dateVal == nil && dateStr != "Pending" && dateStr != "NA" {
					continue
				}

				status := "completed"
				obligationStatus := "due"

				// Check if date is in future (Pending), past, or NA
				if dateStr == "Pending" {
					obligationStatus = "due"
					// Estimate due date 30 days from today
					futureDate := time.Now().AddDate(0, 0, 30)
					dateVal = &futureDate
				} else if dateStr == "NA" {
					continue // Skip NA entries
				} else if dateVal != nil && dateVal.After(time.Now()) {
					status = "scheduled"
					obligationStatus = "scheduled"
				}

				if dateVal == nil {
					continue
				}

				// Insert vaccination history for past dates
				if status == "completed" {
					completionID := vaccCompletionIDToUUID(vacc.RFID, vaccName, doseType)
					idempotencyKey := fmt.Sprintf("vacc-seed-%s-%s-%s", vacc.RFID, vaccName, doseType)

					_, err := tx.Exec(ctx, `
						INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, goat_id,
							doses, dose_ml_given, administered_at, status, recorded_by, idempotency_key, created_at, updated_at)
						VALUES ($1, $2, $3, $4, 1, $5, $6, $7, $8, $9, now(), now())
						ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
					`, completionID, tenantID, vaccCompletionIDToUUID(vacc.RFID, vaccName, doseType), goatID,
						vaccDef.DoseML, dateVal, "accepted", nil, idempotencyKey)
					if err != nil {
						return stats, fmt.Errorf("failed to insert vaccination completion for %s: %w", vacc.RFID, err)
					}
					stats.VaccinationHistory++
				} else {
					// Insert obligation for future dates
					// rule_id is required - use a deterministic ID based on the vaccine
					ruleID := vaccRuleIDToUUID(vaccDef.Code)
					obligationID := vaccObligationIDToUUID(vacc.RFID, vaccName, doseType)
					idempotencyKey := fmt.Sprintf("obl-seed-%s-%s-%s", vacc.RFID, vaccName, doseType)

					_, err := tx.Exec(ctx, `
						INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id,
							target_type, target_id, scope_type, scope_id, due_at, status, idempotency_key, created_at, updated_at)
						VALUES ($1, $2, $3, $4, 'goat', $5, 'tenant', $2, $6, $7, $8, now(), now())
						ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
					`, obligationID, tenantID, versionID, ruleID,
						goatID, dateVal, obligationStatus, idempotencyKey)
					if err != nil {
						return stats, fmt.Errorf("failed to insert obligation for %s: %w", vacc.RFID, err)
					}
					stats.Obligations++
				}
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return stats, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return stats, nil
}

// Helper functions

func toString(row []interface{}, idx int) string {
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
	default:
		return ""
	}
}

func parseDate(dateStr string) *time.Time {
	if dateStr == "" || dateStr == "Pending" || dateStr == "NA" {
		return nil
	}

	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return nil
	}
	return &t
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func validateTarget(env, databaseURL string) error {
	return localtarget.ValidateLocalDatabaseTarget("seed-vaccination-real", env, databaseURL, "local", "dev", "test")
}

func distinctFarms(goats []GoatRecord) []string {
	seen := make(map[string]bool)
	var result []string
	for _, g := range goats {
		if !seen[g.Farm] {
			seen[g.Farm] = true
			result = append(result, g.Farm)
		}
	}
	return result
}

func distinctSheds(goats []GoatRecord) []string {
	seen := make(map[string]bool)
	var result []string
	for _, g := range goats {
		if !seen[g.Shed] {
			seen[g.Shed] = true
			result = append(result, g.Shed)
		}
	}
	return result
}

func shedToFarm(goats []GoatRecord, shedName string) string {
	for _, g := range goats {
		if g.Shed == shedName {
			return g.Farm
		}
	}
	return ""
}

func normalizeGender(gender string) string {
	switch strings.ToLower(strings.TrimSpace(gender)) {
	case "male", "m":
		return "male"
	case "female", "f":
		return "female"
	default:
		return "male"
	}
}

func normalizeBreed(breed string) string {
	breeds := []string{"Beetal", "Sojat", "Boer", "Malai", "Sirohi", "Osmanabadi", "Alpine", "Saanen"}
	for _, b := range breeds {
		if strings.EqualFold(breed, b) {
			return b
		}
	}
	return "Sojat" // Default breed
}

// UUID generation helpers - deterministic based on input
func farmIDToUUID(farmCode string) string {
	return fmt.Sprintf("00000000-0000-4000-8000-0000%08x", hashString(farmCode)%0xffffffff)
}

func shedNameToUUID(shedName string) string {
	return fmt.Sprintf("00000001-0000-4000-8000-0000%08x", hashString(shedName)%0xffffffff)
}

func rfidToUUID(rfid string) string {
	// Use hash of RFID since RFID can be long
	h1 := hashString(rfid)
	h2 := hashString(rfid + "_2")
	return fmt.Sprintf("00000002-0000-4000-8000-%04x%08x", h1&0xffff, h2)
}

func vaccineCodeToUUID(code string) string {
	return fmt.Sprintf("00000003-0000-4000-8000-0000%08x", hashString(code)%0xffffffff)
}

func vaccineVersionToUUID(code string) string {
	return fmt.Sprintf("00000004-0000-4000-8000-0000%08x", hashString(code+"_v1")%0xffffffff)
}

func vaccRuleIDToUUID(code string) string {
	return fmt.Sprintf("00000008-0000-4000-8000-0000%08x", hashString(code+"_rule")%0xffffffff)
}

func vaccineCodeToItemUUID(code string) string {
	return fmt.Sprintf("00000005-0000-4000-8000-0000%08x", hashString(code+"_item")%0xffffffff)
}

func vaccCompletionIDToUUID(rfid, vaccine, dose string) string {
	return fmt.Sprintf("00000006-0000-4000-8000-0000%08x", hashString(rfid+vaccine+dose)%0xffffffff)
}

func vaccObligationIDToUUID(rfid, vaccine, dose string) string {
	return fmt.Sprintf("00000007-0000-4000-8000-0000%08x", hashString(rfid+vaccine+dose+"_obl")%0xffffffff)
}

func hashString(s string) uint32 {
	h := uint32(0)
	for _, c := range s {
		h = h*31 + uint32(c)
	}
	return h
}
