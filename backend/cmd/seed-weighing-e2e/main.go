package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

const defaultFixturePath = "../../fixtures/weighing-e2e-2026-07-29/weighing-seed.json"

type fixture struct {
	FixtureID        string                   `json:"fixture_id"`
	TenantID         string                   `json:"tenant_id"`
	ParkID           string                   `json:"park_id"`
	Campaign         campaignFixture          `json:"campaign"`
	Personas         []personaFixture         `json:"personas"`
	SelectedScopes   []scopeFixture           `json:"selected_scopes"`
	Animals          []animalFixture          `json:"animals"`
	ProofArtifacts   []proofFixture           `json:"proof_artifacts"`
	Observations     []observationFixture     `json:"observations"`
	ShedObservations []shedObservationFixture `json:"shed_observations"`
}

type campaignFixture struct {
	CampaignID        string `json:"campaign_id"`
	PeriodStartDate   string `json:"period_start_date"`
	PeriodEndDate     string `json:"period_end_date"`
	StartBusinessDate string `json:"start_business_date"`
	CadenceType       string `json:"cadence_type"`
	PlannedCapPerDay  int    `json:"planned_cap_per_day"`
	Status            string `json:"status"`
	OperatorCode      string `json:"operator_code"`
}

type personaFixture struct {
	Code        string `json:"code"`
	DisplayName string `json:"display_name"`
	GrantRole   string `json:"grant_role"`
}

type scopeFixture struct {
	CampaignShedID      string `json:"campaign_shed_id"`
	LocationID          string `json:"location_id"`
	DisplayName         string `json:"display_name"`
	LocationType        string `json:"location_type"`
	WeighingCategory    string `json:"weighing_category"`
	ExpectedAnimalCount int    `json:"expected_animal_count"`
}

type animalFixture struct {
	AnimalID               string  `json:"animal_id"`
	RFID                   string  `json:"rfid"`
	DisplayID              string  `json:"display_id"`
	ExpectedCampaignShedID *string `json:"expected_campaign_shed_id"`
	ExpectedLocationLabel  *string `json:"expected_location_label"`
	CurrentLocationLabel   string  `json:"current_location_label"`
	CurrentTruth           string  `json:"current_truth"`
	ExpectedStatus         string  `json:"expected_status"`
}

type proofFixture struct {
	ProofArtifactID string `json:"proof_artifact_id"`
	SubjectScope    string `json:"subject_scope"`
	ProofMode       string `json:"proof_mode"`
	UploadState     string `json:"upload_state"`
}

type observationFixture struct {
	ObservationID         string  `json:"observation_id"`
	AnimalID              string  `json:"animal_id"`
	WorkGroupID           string  `json:"work_group_id"`
	CampaignShedID        *string `json:"campaign_shed_id"`
	WeightKG              float64 `json:"weight_kg"`
	LocationMatchStatus   string  `json:"location_match_status"`
	ExpectedLocationLabel *string `json:"expected_location_label"`
	ActualLocationLabel   *string `json:"actual_location_label"`
	ProofArtifactID       string  `json:"proof_artifact_id"`
	IDempotencyKey        string  `json:"idempotency_key"`
}

type shedObservationFixture struct {
	ShedObservationID string `json:"shed_observation_id"`
	CampaignShedID    string `json:"campaign_shed_id"`
	WorkGroupID       string `json:"work_group_id"`
	WeighingResult    struct {
		TotalWeightKG float64 `json:"total_weight_kg"`
	} `json:"weighing_result"`
	ProofArtifactID string `json:"proof_artifact_id"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "seed-weighing-e2e: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("seed-weighing-e2e", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fixturePath := fs.String("fixture", defaultFixturePath, "path to weighing E2E fixture JSON")
	dryRun := fs.Bool("dry-run", false, "validate and summarize without writing to DATABASE_URL")
	timeout := fs.Duration("timeout", 30*time.Second, "database import timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}

	fx, err := loadFixture(*fixturePath)
	if err != nil {
		return err
	}
	if err := validateFixture(fx); err != nil {
		return err
	}
	summary := fmt.Sprintf("fixture=%s tenant=%s campaign=%s scopes=%d animals=%d proofs=%d observations=%d shed_observations=%d",
		fx.FixtureID, fx.TenantID, fx.Campaign.CampaignID, len(fx.SelectedScopes), len(fx.Animals), len(fx.ProofArtifacts), len(fx.Observations), len(fx.ShedObservations))
	if *dryRun {
		fmt.Fprintf(stdout, "weighing E2E seed dry-run ok: %s\n", summary)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	pool, err := platformpg.Connect(ctx, platformpg.ConfigFromEnv())
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := importFixture(ctx, pool, fx); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "weighing E2E seed imported: %s\n", summary)
	return nil
}

func loadFixture(path string) (fixture, error) {
	var fx fixture
	clean := filepath.Clean(path)
	body, err := os.ReadFile(clean)
	if err != nil {
		return fx, fmt.Errorf("read fixture %s: %w", clean, err)
	}
	if err := json.Unmarshal(body, &fx); err != nil {
		return fx, fmt.Errorf("parse fixture %s: %w", clean, err)
	}
	return fx, nil
}

func validateFixture(fx fixture) error {
	switch {
	case fx.TenantID == "":
		return errors.New("tenant_id is required")
	case fx.ParkID == "":
		return errors.New("park_id is required")
	case fx.Campaign.CampaignID == "":
		return errors.New("campaign.campaign_id is required")
	case fx.Campaign.OperatorCode == "":
		return errors.New("campaign.operator_code is required")
	case len(fx.SelectedScopes) == 0:
		return errors.New("selected_scopes is required")
	case len(fx.Animals) == 0:
		return errors.New("animals is required")
	}
	personas := map[string]bool{}
	for _, p := range fx.Personas {
		if p.Code == "" {
			return errors.New("persona.code is required")
		}
		personas[p.Code] = true
	}
	if !personas[fx.Campaign.OperatorCode] {
		return fmt.Errorf("operator persona %q is not present", fx.Campaign.OperatorCode)
	}
	scopes := map[string]scopeFixture{}
	for _, s := range fx.SelectedScopes {
		if s.CampaignShedID == "" || s.LocationID == "" || s.DisplayName == "" {
			return errors.New("selected scope ids and display_name are required")
		}
		if dbLocationType(s.LocationType) == "" {
			return fmt.Errorf("unsupported scope location_type %q", s.LocationType)
		}
		scopes[s.CampaignShedID] = s
	}
	proofs := map[string]bool{}
	for _, p := range fx.ProofArtifacts {
		if p.ProofArtifactID == "" {
			return errors.New("proof_artifact_id is required")
		}
		proofs[p.ProofArtifactID] = true
	}
	for _, animal := range fx.Animals {
		if animal.AnimalID == "" || animal.DisplayID == "" {
			return errors.New("animal_id and display_id are required")
		}
		if animal.ExpectedCampaignShedID != nil {
			if _, ok := scopes[*animal.ExpectedCampaignShedID]; !ok {
				return fmt.Errorf("animal %s references unknown campaign_shed_id %s", animal.AnimalID, *animal.ExpectedCampaignShedID)
			}
		}
	}
	for _, observation := range fx.Observations {
		if !proofs[observation.ProofArtifactID] {
			return fmt.Errorf("observation %s references unknown proof %s", observation.ObservationID, observation.ProofArtifactID)
		}
	}
	for _, observation := range fx.ShedObservations {
		if !proofs[observation.ProofArtifactID] {
			return fmt.Errorf("shed observation %s references unknown proof %s", observation.ShedObservationID, observation.ProofArtifactID)
		}
	}
	return nil
}

func importFixture(ctx context.Context, pool *pgxpool.Pool, fx fixture) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `INSERT INTO public.tenants (tenant_id, name, status) VALUES ($1, $2, 'active') ON CONFLICT (tenant_id) DO UPDATE SET name = EXCLUDED.name, status = 'active', updated_at = now()`, fx.TenantID, "Weighing E2E Tenant"); err != nil {
		return fmt.Errorf("upsert tenant: %w", err)
	}
	custodianID := "12121212-1212-4121-8121-121212121212"
	if _, err := tx.Exec(ctx, `INSERT INTO public.parties (party_id, party_type, display_name, status) VALUES ($1, 'org', 'Weighing E2E Custodian', 'active') ON CONFLICT (party_id) DO UPDATE SET display_name = EXCLUDED.display_name, status = 'active', updated_at = now()`, custodianID); err != nil {
		return fmt.Errorf("upsert custodian party: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO public.locations (location_id, tenant_id, location_type, location_code, name, status) VALUES ($1, $2, 'park', 'WG-PARK', 'Weighing E2E Park', 'active') ON CONFLICT (location_id) DO UPDATE SET name = EXCLUDED.name, status = 'active', updated_at = now()`, fx.ParkID, fx.TenantID); err != nil {
		return fmt.Errorf("upsert park: %w", err)
	}
	locationIDs := map[string]string{}
	for _, scope := range fx.SelectedScopes {
		locationIDs[scope.DisplayName] = scope.LocationID
		if _, err := tx.Exec(ctx, `
INSERT INTO public.locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
VALUES ($1, $2, $3, $4, $5, $6, 'active')
ON CONFLICT (location_id) DO UPDATE SET location_type = EXCLUDED.location_type, name = EXCLUDED.name, parent_location_id = EXCLUDED.parent_location_id, status = 'active', updated_at = now()`,
			scope.LocationID, fx.TenantID, dbLocationType(scope.LocationType), scopeCode(scope.DisplayName), scope.DisplayName, fx.ParkID); err != nil {
			return fmt.Errorf("upsert location %s: %w", scope.DisplayName, err)
		}
	}
	for label, id := range syntheticLocations(fx) {
		locationIDs[label] = id
		if _, err := tx.Exec(ctx, `
INSERT INTO public.locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
VALUES ($1, $2, $3, $4, $5, $6, 'active')
ON CONFLICT (location_id) DO UPDATE SET name = EXCLUDED.name, parent_location_id = EXCLUDED.parent_location_id, status = 'active', updated_at = now()`,
			id, fx.TenantID, syntheticLocationType(label), scopeCode(label), label, fx.ParkID); err != nil {
			return fmt.Errorf("upsert synthetic location %s: %w", label, err)
		}
	}

	operatorID := personaID(fx.Campaign.OperatorCode)
	creatorID := personaID("ravi_ceo")
	for _, animal := range fx.Animals {
		currentLocationID := locationIDs[animal.CurrentLocationLabel]
		if currentLocationID == "" {
			return fmt.Errorf("animal %s current_location_label %q has no location", animal.AnimalID, animal.CurrentLocationLabel)
		}
		lifecycle := lifecycleStatus(animal.CurrentTruth)
		health := healthStatus(animal.CurrentTruth)
		if _, err := tx.Exec(ctx, `
INSERT INTO public.goats (goat_id, tenant_id, display_id, species, sex, age_band, lifecycle_status, management_stage, health_status, custodian_party_id, current_location_id, park_id, shed_id, exited_at, exit_reason)
VALUES ($1, $2, $3, 'goat', 'female', 'kid', $4, 'K1', $5, $6, $7, $8, $7, CASE WHEN $4 IN ('sold','transferred') THEN now() ELSE NULL END, CASE WHEN $4 = 'sold' THEN 'sold' WHEN $4 = 'transferred' THEN 'transferred' ELSE NULL END)
ON CONFLICT (goat_id) DO UPDATE SET display_id = EXCLUDED.display_id, lifecycle_status = EXCLUDED.lifecycle_status, health_status = EXCLUDED.health_status, current_location_id = EXCLUDED.current_location_id, park_id = EXCLUDED.park_id, shed_id = EXCLUDED.shed_id, updated_at = now()`,
			animal.AnimalID, fx.TenantID, dbDisplayID(animal), lifecycle, health, custodianID, currentLocationID, fx.ParkID); err != nil {
			return fmt.Errorf("upsert goat %s: %w", animal.DisplayID, err)
		}
		if animal.RFID != "" {
			if _, err := tx.Exec(ctx, `
INSERT INTO public.goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, source_system, source_record_id, normalizer_version, confidence)
VALUES ($1, $2, 'animal_identifier_1', $3, $4, 'global', true, 'active', now(), 'weighing_e2e_fixture', $5, 'weighing-e2e-v1', 1)
ON CONFLICT (tenant_id, normalized_value) DO UPDATE SET goat_id = EXCLUDED.goat_id, identifier_value = EXCLUDED.identifier_value, status = 'active', is_primary_for_goat = true, updated_at = now()`,
				fx.TenantID, animal.AnimalID, animal.RFID, normalizeIdentifier(animal.RFID), animal.AnimalID); err != nil {
				return fmt.Errorf("upsert goat identifier %s: %w", animal.RFID, err)
			}
		}
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO public.weighing_campaigns (campaign_id, tenant_id, park_id, period_type, period_start_date, period_end_date, cadence_type, start_business_date, status, planned_cap_per_day, operator_user_id, published_at, created_by)
VALUES ($1, $2, $3, 'week', $4, $5, $6, $7, $8, $9, $10, CASE WHEN $8 IN ('published','in_progress','delayed','completed') THEN now() ELSE NULL END, $11)
ON CONFLICT (campaign_id) DO UPDATE SET status = EXCLUDED.status, planned_cap_per_day = EXCLUDED.planned_cap_per_day, operator_user_id = EXCLUDED.operator_user_id, updated_at = now()`,
		fx.Campaign.CampaignID, fx.TenantID, fx.ParkID, fx.Campaign.PeriodStartDate, fx.Campaign.PeriodEndDate, fx.Campaign.CadenceType, fx.Campaign.StartBusinessDate, fx.Campaign.Status, fx.Campaign.PlannedCapPerDay, operatorID, creatorID); err != nil {
		return fmt.Errorf("upsert campaign: %w", err)
	}
	for _, scope := range fx.SelectedScopes {
		if _, err := tx.Exec(ctx, `
INSERT INTO public.weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, expected_animal_count, weighing_category, status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (campaign_shed_id) DO UPDATE SET display_name = EXCLUDED.display_name, expected_animal_count = EXCLUDED.expected_animal_count, weighing_category = EXCLUDED.weighing_category, status = EXCLUDED.status, updated_at = now()`,
			scope.CampaignShedID, fx.Campaign.CampaignID, fx.TenantID, scope.LocationID, dbLocationType(scope.LocationType), scope.DisplayName, scope.ExpectedAnimalCount, scope.WeighingCategory, scopeStatus(scope, fx)); err != nil {
			return fmt.Errorf("upsert campaign scope %s: %w", scope.DisplayName, err)
		}
	}
	for _, animal := range fx.Animals {
		if animal.ExpectedCampaignShedID == nil {
			continue
		}
		scope := scopeByID(fx, *animal.ExpectedCampaignShedID)
		if scope == nil {
			return fmt.Errorf("missing scope %s", *animal.ExpectedCampaignShedID)
		}
		currentLocationID := locationIDs[animal.CurrentLocationLabel]
		if _, err := tx.Exec(ctx, `
INSERT INTO public.weighing_expected_animals (campaign_id, tenant_id, animal_id, expected_location_id, expected_location_label, campaign_shed_id, status, availability_status, current_location_id, current_location_label, current_lifecycle_status, availability_checked_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, now())
ON CONFLICT (campaign_id, animal_id) DO UPDATE SET status = EXCLUDED.status, availability_status = EXCLUDED.availability_status, current_location_id = EXCLUDED.current_location_id, current_location_label = EXCLUDED.current_location_label, current_lifecycle_status = EXCLUDED.current_lifecycle_status, availability_checked_at = now(), updated_at = now()`,
			fx.Campaign.CampaignID, fx.TenantID, animal.AnimalID, scope.LocationID, scope.DisplayName, *animal.ExpectedCampaignShedID, expectedStatus(animal.ExpectedStatus), availabilityStatus(animal.CurrentTruth), currentLocationID, animal.CurrentLocationLabel, lifecycleStatus(animal.CurrentTruth)); err != nil {
			return fmt.Errorf("upsert expected animal %s: %w", animal.DisplayID, err)
		}
	}
	for _, proof := range fx.ProofArtifacts {
		subjectType, subjectID := proofSubject(proof, fx)
		if _, err := tx.Exec(ctx, `
INSERT INTO public.proof_artifacts (proof_id, tenant_id, storage_provider, object_key, content_hash, mime_type, size_bytes, duration_ms, upload_state, scope_type, scope_id, subject_type, subject_id, proof_type, uploaded_by, metadata, uploaded_at, idempotency_key, request_fingerprint)
VALUES ($1, $2, 'local', $3, $4, 'video/mp4', 1024, 15000, 'completed', 'task', $5, $6, $7, 'video', $8, $9::jsonb, now(), $10, $11)
ON CONFLICT (proof_id) DO UPDATE SET upload_state = 'completed', subject_type = EXCLUDED.subject_type, subject_id = EXCLUDED.subject_id, metadata = EXCLUDED.metadata, updated_at = now()`,
			proof.ProofArtifactID, fx.TenantID, "weighing-e2e/"+proof.ProofArtifactID+".mp4", "sha256:"+proof.ProofArtifactID, fx.Campaign.CampaignID, subjectType, subjectID, operatorID, proofMetadata(proof), "weighing-e2e:"+proof.ProofArtifactID, proof.ProofArtifactID); err != nil {
			return fmt.Errorf("upsert proof %s: %w", proof.ProofArtifactID, err)
		}
	}
	for _, observation := range fx.Observations {
		expectedID, expectedLabel, actualID, actualLabel, mismatch := observationLocationContext(observation, fx, locationIDs)
		if _, err := tx.Exec(ctx, `
INSERT INTO public.weighing_observations (observation_id, tenant_id, campaign_id, campaign_shed_id, animal_id, weight_kg, proof_artifact_id, expected_location_id, expected_location_label, actual_location_id, actual_location_label, mismatch_status, recorded_by, idempotency_key)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
ON CONFLICT (tenant_id, idempotency_key) DO UPDATE SET weight_kg = EXCLUDED.weight_kg, proof_artifact_id = EXCLUDED.proof_artifact_id`,
			observation.ObservationID, fx.TenantID, fx.Campaign.CampaignID, observation.CampaignShedID, observation.AnimalID, observation.WeightKG, observation.ProofArtifactID, expectedID, expectedLabel, actualID, actualLabel, mismatch, operatorID, observation.IDempotencyKey); err != nil {
			return fmt.Errorf("upsert observation %s: %w", observation.ObservationID, err)
		}
	}
	for _, observation := range fx.ShedObservations {
		if _, err := tx.Exec(ctx, `
INSERT INTO public.weighing_shed_observations (shed_observation_id, tenant_id, campaign_id, campaign_shed_id, weight_kg, proof_artifact_id, recorded_by, idempotency_key)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (tenant_id, idempotency_key) DO UPDATE SET weight_kg = EXCLUDED.weight_kg, proof_artifact_id = EXCLUDED.proof_artifact_id`,
			observation.ShedObservationID, fx.TenantID, fx.Campaign.CampaignID, observation.CampaignShedID, observation.WeighingResult.TotalWeightKG, observation.ProofArtifactID, operatorID, "weighing-e2e:"+observation.ShedObservationID); err != nil {
			return fmt.Errorf("upsert shed observation %s: %w", observation.ShedObservationID, err)
		}
	}
	return tx.Commit(ctx)
}

func dbLocationType(locationType string) string {
	switch strings.ToLower(locationType) {
	case "shed", "cohort", "pen":
		return strings.ToLower(locationType)
	case "partition":
		return "pen"
	default:
		return ""
	}
}

func syntheticLocations(fx fixture) map[string]string {
	locations := map[string]string{}
	for _, animal := range fx.Animals {
		if animal.CurrentLocationLabel == "" {
			continue
		}
		known := false
		for _, scope := range fx.SelectedScopes {
			if scope.DisplayName == animal.CurrentLocationLabel {
				known = true
				break
			}
		}
		if !known {
			locations[animal.CurrentLocationLabel] = syntheticLocationID(animal.CurrentLocationLabel)
		}
	}
	return locations
}

func syntheticLocationID(label string) string {
	switch label {
	case "ICU":
		return "55555555-00f1-4555-8555-5555555555f1"
	case "Sold/Transferred":
		return "55555555-00f2-4555-8555-5555555555f2"
	case "Adult Shed Z":
		return "55555555-00f3-4555-8555-5555555555f3"
	default:
		return "55555555-00ff-4555-8555-5555555555ff"
	}
}

func syntheticLocationType(label string) string {
	if label == "ICU" {
		return "cohort"
	}
	return "shed"
}

func scopeCode(label string) string {
	code := strings.ToUpper(strings.ReplaceAll(label, " ", "-"))
	code = strings.ReplaceAll(code, "/", "")
	return strings.Trim(code, "-")
}

func dbDisplayID(animal animalFixture) string {
	suffix := animal.AnimalID
	if len(suffix) >= 6 {
		suffix = suffix[len(suffix)-6:]
	}
	return "G-" + suffix
}

func normalizeIdentifier(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func personaID(code string) string {
	switch code {
	case "ravi_ceo":
		return "10101010-1010-4101-8101-101010101010"
	case "dinakar_director":
		return "20202020-2020-4202-8202-202020202020"
	case "amit_operator":
		return "30303030-3030-4303-8303-303030303030"
	default:
		return "40404040-4040-4404-8404-404040404040"
	}
}

func lifecycleStatus(truth string) string {
	switch truth {
	case "sold_transferred":
		return "sold"
	default:
		return "alive"
	}
}

func healthStatus(truth string) string {
	switch truth {
	case "icu":
		return "icu"
	default:
		return "healthy"
	}
}

func expectedStatus(status string) string {
	switch status {
	case "weighed", "unavailable", "pending":
		return status
	default:
		return "pending"
	}
}

func availabilityStatus(truth string) string {
	switch truth {
	case "active_expected_shed":
		return "expected_shed"
	case "moved_other_shed":
		return "moved_other_shed"
	case "icu":
		return "icu"
	case "sold_transferred":
		return "sold_transferred"
	default:
		return "unknown_review"
	}
}

func scopeStatus(scope scopeFixture, fx fixture) string {
	for _, shedObservation := range fx.ShedObservations {
		if shedObservation.CampaignShedID == scope.CampaignShedID {
			return "completed"
		}
	}
	accepted := 0
	for _, animal := range fx.Animals {
		if animal.ExpectedCampaignShedID != nil && *animal.ExpectedCampaignShedID == scope.CampaignShedID && animal.ExpectedStatus == "weighed" {
			accepted++
		}
	}
	if accepted == 0 {
		return "pending"
	}
	if accepted >= scope.ExpectedAnimalCount && scope.ExpectedAnimalCount > 0 {
		return "completed"
	}
	return "in_progress"
}

func scopeByID(fx fixture, id string) *scopeFixture {
	for i := range fx.SelectedScopes {
		if fx.SelectedScopes[i].CampaignShedID == id {
			return &fx.SelectedScopes[i]
		}
	}
	return nil
}

func proofSubject(proof proofFixture, fx fixture) (string, any) {
	for _, observation := range fx.Observations {
		if observation.ProofArtifactID == proof.ProofArtifactID {
			return "goat", observation.AnimalID
		}
	}
	for _, observation := range fx.ShedObservations {
		if observation.ProofArtifactID == proof.ProofArtifactID {
			scope := scopeByID(fx, observation.CampaignShedID)
			if scope != nil {
				return "shed", scope.LocationID
			}
		}
	}
	return "task", fx.Campaign.CampaignID
}

func proofMetadata(proof proofFixture) string {
	body, _ := json.Marshal(map[string]string{
		"feature":       "weighing",
		"subject_scope": proof.SubjectScope,
		"proof_mode":    proof.ProofMode,
		"fixture_state": proof.UploadState,
	})
	return string(body)
}

func observationLocationContext(observation observationFixture, fx fixture, locationIDs map[string]string) (any, any, any, any, string) {
	animal := animalByID(fx, observation.AnimalID)
	if animal == nil {
		return nil, nil, nil, nil, "extra_scan"
	}
	expectedLabel := deref(animal.ExpectedLocationLabel)
	if observation.ExpectedLocationLabel != nil {
		expectedLabel = *observation.ExpectedLocationLabel
	}
	actualLabel := animal.CurrentLocationLabel
	if observation.ActualLocationLabel != nil {
		actualLabel = *observation.ActualLocationLabel
	}
	mismatch := "expected_shed"
	switch observation.LocationMatchStatus {
	case "other_shed":
		mismatch = "wrong_shed"
	case "not_in_campaign":
		mismatch = "extra_scan"
	}
	return nullableID(locationIDs[expectedLabel]), nullableText(expectedLabel), nullableID(locationIDs[actualLabel]), nullableText(actualLabel), mismatch
}

func animalByID(fx fixture, id string) *animalFixture {
	for i := range fx.Animals {
		if fx.Animals[i].AnimalID == id {
			return &fx.Animals[i]
		}
	}
	return nil
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func nullableID(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}
