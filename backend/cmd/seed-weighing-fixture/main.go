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

const defaultFixturePath = "../fixtures/weighing-seed-2026-07-29/weighing-seed.json"

type fixture struct {
	FixtureID        string                   `json:"fixture_id"`
	TenantID         string                   `json:"tenant_id"`
	ParkID           string                   `json:"park_id"`
	Campaign         campaignFixture          `json:"campaign"`
	Personas         []personaFixture         `json:"personas"`
	SelectedScopes   []scopeFixture           `json:"selected_scopes"`
	WorkGroups       []workGroupFixture       `json:"work_groups"`
	Animals          []animalFixture          `json:"animals"`
	ProofArtifacts   []proofFixture           `json:"proof_artifacts"`
	Observations     []observationFixture     `json:"observations"`
	DuplicateScans   []duplicateScanFixture   `json:"duplicate_scans"`
	ShedObservations []shedObservationFixture `json:"shed_observations"`
	MobileContract   mobileContractFixture    `json:"mobile_contract"`
	ScaleProfile     scaleProfileFixture      `json:"scale_profile"`
	ExpectedProgress expectedProgressFixture  `json:"expected_progress"`
	E2ESteps         []string                 `json:"e2e_steps"`
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

type workGroupFixture struct {
	WorkGroupID           string   `json:"work_group_id"`
	EffectiveBusinessDate string   `json:"effective_business_date"`
	CampaignShedIDs       []string `json:"campaign_shed_ids"`
	Status                string   `json:"status"`
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
	RetryAttempts   int    `json:"retry_attempts"`
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
	OffPageScan           bool    `json:"off_page_scan"`
}

type duplicateScanFixture struct {
	ScanID                         string `json:"scan_id"`
	AnimalID                       string `json:"animal_id"`
	RFID                           string `json:"rfid"`
	OriginalObservationID          string `json:"original_observation_id"`
	IDempotencyKey                 string `json:"idempotency_key"`
	ExpectedResult                 string `json:"expected_result"`
	ProgressDelta                  int    `json:"progress_delta"`
	MustNotCreateSecondObservation bool   `json:"must_not_create_second_observation"`
}

type shedObservationFixture struct {
	ShedObservationID string `json:"shed_observation_id"`
	CampaignShedID    string `json:"campaign_shed_id"`
	WorkGroupID       string `json:"work_group_id"`
	WeighingResult    struct {
		AverageWeightKG float64 `json:"average_weight_kg"`
		TotalWeightKG   float64 `json:"total_weight_kg"`
	} `json:"weighing_result"`
	ProofArtifactID                        string   `json:"proof_artifact_id"`
	ProofArtifactIDs                       []string `json:"proof_artifact_ids"`
	MustNotCreateIndividualWeights         bool     `json:"must_not_create_individual_weights"`
	MustNotUpdateLatestTrustedAnimalWeight bool     `json:"must_not_update_latest_trusted_animal_weight"`
}

type mobileContractFixture struct {
	RoomFirst                            bool     `json:"room_first"`
	OutboxIdempotent                     bool     `json:"outbox_idempotent"`
	ProcessDeathSafe                     bool     `json:"process_death_safe"`
	SignOutWipeTables                    []string `json:"sign_out_wipe_tables"`
	RFIDTerminatorsSwallowedOnlyOnRoutes []string `json:"rfid_terminators_swallowed_only_on_routes"`
	OffPageScanExpected                  struct {
		MustUpdateScanFeedWithoutFetchAll bool `json:"must_update_scan_feed_without_fetch_all"`
	} `json:"off_page_scan_expected"`
}

type scaleProfileFixture struct {
	ExpectedCampaignAnimals int      `json:"expected_campaign_animals"`
	VisiblePageSize         int      `json:"visible_page_size"`
	ForbiddenReadShapes     []string `json:"forbidden_read_shapes"`
}

type expectedProgressFixture struct {
	IndividualExpectedTotal            int `json:"individual_expected_total"`
	IndividualWeighedExpected          int `json:"individual_weighed_expected"`
	IndividualPendingExpected          int `json:"individual_pending_expected"`
	IndividualUnavailableExpected      int `json:"individual_unavailable_expected"`
	IndividualClosedByLeadership       int `json:"individual_closed_by_leadership"`
	PerShedPartitionSelectedTotal      int `json:"per_shed_partition_selected_total"`
	PerShedPartitionCompleted          int `json:"per_shed_partition_completed"`
	PerShedPartitionPending            int `json:"per_shed_partition_pending"`
	PerShedPartitionProofBlocked       int `json:"per_shed_partition_proof_blocked"`
	PerShedPartitionClosedByLeadership int `json:"per_shed_partition_closed_by_leadership"`
	WrongShedExpected                  int `json:"wrong_shed_expected"`
	NotInCampaignScans                 int `json:"not_in_campaign_scans"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "seed-weighing-fixture: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("seed-weighing-fixture", flag.ContinueOnError)
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
	summary := fmt.Sprintf("fixture=%s tenant=%s campaign=%s scopes=%d work_groups=%d animals=%d proofs=%d observations=%d duplicate_scans=%d shed_observations=%d lump_sum_assignments=%d scale_animals=%d",
		fx.FixtureID, fx.TenantID, fx.Campaign.CampaignID, len(fx.SelectedScopes), len(fx.WorkGroups), len(fx.Animals), len(fx.ProofArtifacts), len(fx.Observations), len(fx.DuplicateScans), len(fx.ShedObservations), countLumpSumAssignments(fx), fx.ScaleProfile.ExpectedCampaignAnimals)
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
	clean := resolveFixturePath(path)
	body, err := os.ReadFile(clean)
	if err != nil {
		return fx, fmt.Errorf("read fixture %s: %w", clean, err)
	}
	if err := json.Unmarshal(body, &fx); err != nil {
		return fx, fmt.Errorf("parse fixture %s: %w", clean, err)
	}
	return fx, nil
}

func resolveFixturePath(path string) string {
	clean := filepath.Clean(path)
	if filepath.IsAbs(clean) || fileExists(clean) {
		return clean
	}
	if filepath.Clean(path) == filepath.Clean(defaultFixturePath) {
		for _, candidate := range []string{
			filepath.Join("..", "fixtures", "weighing-seed-2026-07-29", "weighing-seed.json"),
			filepath.Join("fixtures", "weighing-seed-2026-07-29", "weighing-seed.json"),
		} {
			if fileExists(candidate) {
				return candidate
			}
		}
	}
	return clean
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
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
	for _, group := range fx.WorkGroups {
		if group.WorkGroupID == "" {
			return errors.New("work_group_id is required")
		}
		if len(group.CampaignShedIDs) == 0 {
			return fmt.Errorf("work group %s must preserve selected scope membership", group.WorkGroupID)
		}
		for _, scopeID := range group.CampaignShedIDs {
			if _, ok := scopes[scopeID]; !ok {
				return fmt.Errorf("work group %s references unknown campaign_shed_id %s", group.WorkGroupID, scopeID)
			}
		}
	}
	if !hasDelayedWorkGroup(fx) {
		return errors.New("fixture must include delayed roll-forward work group")
	}
	proofs := map[string]bool{}
	retryProofs := 0
	removedProofs := 0
	for _, p := range fx.ProofArtifacts {
		if p.ProofArtifactID == "" {
			return errors.New("proof_artifact_id is required")
		}
		proofs[p.ProofArtifactID] = true
		if p.UploadState == "accepted_after_retry" && p.RetryAttempts > 0 {
			retryProofs++
		}
		if p.UploadState == "uploaded_unsubmitted_removed_before_acceptance" {
			removedProofs++
		}
	}
	if retryProofs == 0 {
		return errors.New("fixture must cover proof upload retry")
	}
	if removedProofs == 0 {
		return errors.New("fixture must cover uploaded-but-unsubmitted proof removal")
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
		if observation.WeightKG <= 0 {
			return fmt.Errorf("observation %s weight must be positive", observation.ObservationID)
		}
	}
	if err := validateDuplicateScans(fx); err != nil {
		return err
	}
	for _, observation := range fx.ShedObservations {
		proofIDs := shedObservationProofIDs(observation)
		if len(proofIDs) < 1 || len(proofIDs) > 5 {
			return fmt.Errorf("shed observation %s must contain 1..5 proof videos", observation.ShedObservationID)
		}
		for _, proofID := range proofIDs {
			if !proofs[proofID] {
				return fmt.Errorf("shed observation %s references unknown proof %s", observation.ShedObservationID, proofID)
			}
		}
		scope := scopes[observation.CampaignShedID]
		if scope.WeighingCategory != "per_shed_partition" {
			return fmt.Errorf("shed observation %s must target per-shed/partition scope", observation.ShedObservationID)
		}
		if shedObservationAverageWeight(observation, scope) <= 0 {
			return fmt.Errorf("shed observation %s average weight must be positive", observation.ShedObservationID)
		}
		if !observation.MustNotCreateIndividualWeights || !observation.MustNotUpdateLatestTrustedAnimalWeight {
			return fmt.Errorf("shed observation %s must assert no individual/latest trusted weight mutation", observation.ShedObservationID)
		}
	}
	if countLumpSumAssignments(fx) == 0 {
		return errors.New("fixture must assign at least one per_shed_partition lump-sum scope to Amit")
	}
	if err := validateExpectedProgress(fx); err != nil {
		return err
	}
	if err := validateMobileAndScaleContract(fx); err != nil {
		return err
	}
	return nil
}

func validateDuplicateScans(fx fixture) error {
	if len(fx.DuplicateScans) == 0 {
		return errors.New("fixture must cover duplicate scan/idempotent replay")
	}
	observations := map[string]observationFixture{}
	for _, observation := range fx.Observations {
		observations[observation.ObservationID] = observation
	}
	for _, duplicate := range fx.DuplicateScans {
		if duplicate.ScanID == "" {
			return errors.New("duplicate scan_id is required")
		}
		original, ok := observations[duplicate.OriginalObservationID]
		if !ok {
			return fmt.Errorf("duplicate scan %s references unknown original observation %s", duplicate.ScanID, duplicate.OriginalObservationID)
		}
		if duplicate.AnimalID != original.AnimalID {
			return fmt.Errorf("duplicate scan %s animal does not match original observation", duplicate.ScanID)
		}
		if duplicate.IDempotencyKey != original.IDempotencyKey {
			return fmt.Errorf("duplicate scan %s must reuse the original idempotency key", duplicate.ScanID)
		}
		if duplicate.ExpectedResult != "idempotent_replay" {
			return fmt.Errorf("duplicate scan %s expected_result must be idempotent_replay", duplicate.ScanID)
		}
		if duplicate.ProgressDelta != 0 {
			return fmt.Errorf("duplicate scan %s must not increment progress", duplicate.ScanID)
		}
		if !duplicate.MustNotCreateSecondObservation {
			return fmt.Errorf("duplicate scan %s must assert no second observation", duplicate.ScanID)
		}
	}
	return nil
}

func hasDelayedWorkGroup(fx fixture) bool {
	for _, group := range fx.WorkGroups {
		if group.Status == "delayed" && group.EffectiveBusinessDate > fx.Campaign.PeriodEndDate {
			return true
		}
	}
	return false
}

func validateExpectedProgress(fx fixture) error {
	p := fx.ExpectedProgress
	if p.IndividualExpectedTotal == 0 && p.PerShedPartitionSelectedTotal == 0 {
		return errors.New("expected_progress is required")
	}
	if p.IndividualExpectedTotal != p.IndividualWeighedExpected+p.IndividualPendingExpected+p.IndividualUnavailableExpected+p.IndividualClosedByLeadership {
		return errors.New("individual progress buckets must be disjoint and complete")
	}
	if p.PerShedPartitionSelectedTotal != p.PerShedPartitionCompleted+p.PerShedPartitionPending+p.PerShedPartitionProofBlocked+p.PerShedPartitionClosedByLeadership {
		return errors.New("per-shed progress buckets must be disjoint and complete")
	}
	wrongShed := 0
	notInCampaign := 0
	offPage := 0
	for _, observation := range fx.Observations {
		switch observation.LocationMatchStatus {
		case "other_shed":
			wrongShed++
			if observation.ExpectedLocationLabel == nil || observation.ActualLocationLabel == nil {
				return fmt.Errorf("wrong-shed observation %s must keep expected and actual shed labels", observation.ObservationID)
			}
		case "not_in_campaign":
			notInCampaign++
		}
		if observation.OffPageScan {
			offPage++
		}
	}
	if p.WrongShedExpected != wrongShed {
		return fmt.Errorf("wrong_shed_expected=%d does not match observations=%d", p.WrongShedExpected, wrongShed)
	}
	if p.NotInCampaignScans != notInCampaign {
		return fmt.Errorf("not_in_campaign_scans=%d does not match observations=%d", p.NotInCampaignScans, notInCampaign)
	}
	if offPage == 0 {
		return errors.New("fixture must cover off-page scans")
	}
	return nil
}

func validateMobileAndScaleContract(fx fixture) error {
	mobile := fx.MobileContract
	if !mobile.RoomFirst || !mobile.OutboxIdempotent || !mobile.ProcessDeathSafe {
		return errors.New("mobile contract must be Room-first, outbox-idempotent, and process-death safe")
	}
	if !contains(mobile.SignOutWipeTables, "weighing_outbox") || !contains(mobile.SignOutWipeTables, "weighing_observations") || !contains(mobile.SignOutWipeTables, "weighing_shed_observations") {
		return errors.New("mobile contract sign-out wipe must include weighing observation and outbox tables")
	}
	if len(mobile.RFIDTerminatorsSwallowedOnlyOnRoutes) != 1 || mobile.RFIDTerminatorsSwallowedOnlyOnRoutes[0] != "WeighingScanRoute" {
		return errors.New("RFID Enter/Tab swallowing must be scoped only to WeighingScanRoute")
	}
	if !mobile.OffPageScanExpected.MustUpdateScanFeedWithoutFetchAll {
		return errors.New("off-page scan contract must update feed without fetching all animals")
	}
	scale := fx.ScaleProfile
	if scale.ExpectedCampaignAnimals < 5000 {
		return errors.New("scale profile must cover 5k+ animals")
	}
	if scale.VisiblePageSize > 20 {
		return errors.New("scale profile visible page size must stay phone-sized")
	}
	for _, forbidden := range []string{"load_all_animals_for_progress", "linear_scan_rfid_lookup", "visible_page_totals_as_campaign_totals", "order_by_limit_1_for_membership"} {
		if !contains(scale.ForbiddenReadShapes, forbidden) {
			return fmt.Errorf("scale profile must forbid %s", forbidden)
		}
	}
	return nil
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
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
		if _, err := tx.Exec(ctx, // scale-guard:ignore: local E2E seed imports a bounded fixture scope list, not a request path
			`
INSERT INTO public.locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
VALUES ($1, $2, $3, $4, $5, $6, 'active')
ON CONFLICT (location_id) DO UPDATE SET location_type = EXCLUDED.location_type, name = EXCLUDED.name, parent_location_id = EXCLUDED.parent_location_id, status = 'active', updated_at = now()`,
			scope.LocationID, fx.TenantID, dbLocationType(scope.LocationType), scopeCode(scope.DisplayName), scope.DisplayName, fx.ParkID); err != nil {
			return fmt.Errorf("upsert location %s: %w", scope.DisplayName, err)
		}
	}
	for label, id := range syntheticLocations(fx) {
		locationIDs[label] = id
		if _, err := tx.Exec(ctx, // scale-guard:ignore: local E2E seed imports bounded synthetic fixture locations
			`
INSERT INTO public.locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
VALUES ($1, $2, $3, $4, $5, $6, 'active')
ON CONFLICT (location_id) DO UPDATE SET name = EXCLUDED.name, parent_location_id = EXCLUDED.parent_location_id, status = 'active', updated_at = now()`,
			id, fx.TenantID, syntheticLocationType(label), scopeCode(label), label, fx.ParkID); err != nil {
			return fmt.Errorf("upsert synthetic location %s: %w", label, err)
		}
	}

	operatorID := personaID(fx.Campaign.OperatorCode)
	creatorID := personaID("ravi_ceo")
	departmentID := "13131313-1313-4131-8131-131313131313"
	if _, err := tx.Exec(ctx, `
INSERT INTO public.departments (department_id, tenant_id, code, label, status)
VALUES ($1::uuid, $2::uuid, 'weighing_ops', 'Weighing Operations', 'active')
ON CONFLICT (department_id) DO UPDATE SET label = EXCLUDED.label, status = 'active', updated_at = now()`, departmentID, fx.TenantID); err != nil {
		return fmt.Errorf("upsert weighing department: %w", err)
	}
	for _, moduleKey := range []string{"vaccination", "weighing"} {
		if _, err := tx.Exec(ctx, // scale-guard:ignore: local E2E seed writes two fixed module grants
			`
INSERT INTO public.department_module_grants (tenant_id, department_id, module_key, status)
VALUES ($1::uuid, $2::uuid, $3, 'active')
ON CONFLICT (tenant_id, department_id, module_key) DO UPDATE SET status = 'active', updated_at = now()`, fx.TenantID, departmentID, moduleKey); err != nil {
			return fmt.Errorf("upsert %s module grant: %w", moduleKey, err)
		}
	}
	for _, persona := range fx.Personas {
		grantRole := dbGrantRole(persona.GrantRole)
		if grantRole == "" {
			continue
		}
		if _, err := tx.Exec(ctx, // scale-guard:ignore: local E2E seed imports bounded selected campaign scopes
			`
INSERT INTO public.user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from, created_by)
SELECT $1::uuid, $2::uuid, $3, 'tenant', $1::uuid, 'active', now(), $4::uuid
WHERE EXISTS (SELECT 1 FROM public.org_role_catalog WHERE role_key=$3)
  AND NOT EXISTS (
    SELECT 1 FROM public.user_scope_grants
    WHERE tenant_id=$1::uuid AND user_id=$2::uuid AND role=$3 AND scope_type='tenant' AND scope_id=$1::uuid AND status='active'
  )`,
			fx.TenantID, personaID(persona.Code), grantRole, creatorID); err != nil {
			return fmt.Errorf("upsert persona grant %s: %w", persona.Code, err)
		}
		var grantExists bool
		if err := tx.QueryRow(ctx, // scale-guard:ignore: local E2E seed verifies each bounded persona grant after insert
			`
SELECT EXISTS (
  SELECT 1 FROM public.user_scope_grants
  WHERE tenant_id=$1::uuid AND user_id=$2::uuid AND role=$3 AND scope_type='tenant' AND scope_id=$1::uuid AND status='active'
)`, fx.TenantID, personaID(persona.Code), grantRole).Scan(&grantExists); err != nil {
			return fmt.Errorf("verify persona grant %s: %w", persona.Code, err)
		}
		if !grantExists {
			return fmt.Errorf("persona grant %s role %s did not match org_role_catalog", persona.Code, grantRole)
		}
		if _, err := tx.Exec(ctx, // scale-guard:ignore: local E2E seed imports bounded fixture personas
			`
INSERT INTO public.workforce_members (workforce_member_id, tenant_id, user_id, display_code, display_name, status, primary_role_hint, primary_location_id, department_id, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, 'active', $6, $7::uuid, $8::uuid, $9::uuid)
ON CONFLICT (workforce_member_id) DO UPDATE SET user_id = EXCLUDED.user_id, display_name = EXCLUDED.display_name, status = 'active', primary_role_hint = EXCLUDED.primary_role_hint, primary_location_id = EXCLUDED.primary_location_id, department_id = EXCLUDED.department_id, updated_at = now()`,
			personaID(persona.Code), fx.TenantID, personaID(persona.Code), persona.Code, persona.DisplayName, primaryRoleHint(grantRole), fx.ParkID, departmentID, creatorID); err != nil {
			return fmt.Errorf("upsert workforce member %s: %w", persona.Code, err)
		}
	}
	for _, animal := range fx.Animals {
		currentLocationID := locationIDs[animal.CurrentLocationLabel]
		if currentLocationID == "" {
			return fmt.Errorf("animal %s current_location_label %q has no location", animal.AnimalID, animal.CurrentLocationLabel)
		}
		lifecycle := lifecycleStatus(animal.CurrentTruth)
		health := healthStatus(animal.CurrentTruth)
		if _, err := tx.Exec(ctx, // scale-guard:ignore: local E2E seed imports bounded fixture animals
			`
INSERT INTO public.goats (goat_id, tenant_id, display_id, species, sex, age_band, lifecycle_status, management_stage, health_status, custodian_party_id, current_location_id, park_id, shed_id, exited_at, exit_reason)
VALUES ($1, $2, $3, 'goat', 'female', 'kid', $4, 'K1', $5, $6, $7, $8, $7, CASE WHEN $4 IN ('sold','transferred') THEN now() ELSE NULL END, CASE WHEN $4 = 'sold' THEN 'sold' WHEN $4 = 'transferred' THEN 'transferred' ELSE NULL END)
ON CONFLICT (goat_id) DO UPDATE SET display_id = EXCLUDED.display_id, lifecycle_status = EXCLUDED.lifecycle_status, health_status = EXCLUDED.health_status, current_location_id = EXCLUDED.current_location_id, park_id = EXCLUDED.park_id, shed_id = EXCLUDED.shed_id, updated_at = now()`,
			animal.AnimalID, fx.TenantID, dbDisplayID(animal), lifecycle, health, custodianID, currentLocationID, fx.ParkID); err != nil {
			return fmt.Errorf("upsert goat %s: %w", animal.DisplayID, err)
		}
		if animal.RFID != "" {
			if _, err := tx.Exec(ctx, // scale-guard:ignore: local E2E seed imports bounded fixture RFID identifiers
				`
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
		if _, err := tx.Exec(ctx, // scale-guard:ignore: local E2E seed imports bounded expected-animal compatibility rows
			`
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
		if _, err := tx.Exec(ctx, // scale-guard:ignore: local E2E seed imports bounded expected-animal compatibility rows
			`
INSERT INTO public.weighing_expected_animals (campaign_id, tenant_id, animal_id, expected_location_id, expected_location_label, campaign_shed_id, status, availability_status, current_location_id, current_location_label, current_lifecycle_status, availability_checked_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, now())
ON CONFLICT (campaign_id, animal_id) DO UPDATE SET status = EXCLUDED.status, availability_status = EXCLUDED.availability_status, current_location_id = EXCLUDED.current_location_id, current_location_label = EXCLUDED.current_location_label, current_lifecycle_status = EXCLUDED.current_lifecycle_status, availability_checked_at = now(), updated_at = now()`,
			fx.Campaign.CampaignID, fx.TenantID, animal.AnimalID, scope.LocationID, scope.DisplayName, *animal.ExpectedCampaignShedID, expectedStatus(animal.ExpectedStatus), availabilityStatus(animal.CurrentTruth), currentLocationID, animal.CurrentLocationLabel, lifecycleStatus(animal.CurrentTruth)); err != nil {
			return fmt.Errorf("upsert expected animal %s: %w", animal.DisplayID, err)
		}
	}
	for _, proof := range fx.ProofArtifacts {
		subjectType, subjectID := proofSubject(proof, fx)
		if _, err := tx.Exec(ctx, // scale-guard:ignore: local E2E seed imports bounded proof fixture rows
			`
INSERT INTO public.proof_artifacts (proof_id, tenant_id, storage_provider, object_key, content_hash, mime_type, size_bytes, duration_ms, upload_state, scope_type, scope_id, subject_type, subject_id, proof_type, uploaded_by, metadata, uploaded_at, idempotency_key, request_fingerprint)
VALUES ($1, $2, 'local', $3, $4, 'video/mp4', 1024, 15000, 'completed', 'task', $5, $6, $7, 'video', $8, $9::jsonb, now(), $10, $11)
ON CONFLICT (proof_id) DO UPDATE SET upload_state = 'completed', subject_type = EXCLUDED.subject_type, subject_id = EXCLUDED.subject_id, metadata = EXCLUDED.metadata, updated_at = now()`,
			proof.ProofArtifactID, fx.TenantID, "weighing-e2e/"+proof.ProofArtifactID+".mp4", "sha256:"+proof.ProofArtifactID, fx.Campaign.CampaignID, subjectType, subjectID, operatorID, proofMetadata(proof), "weighing-e2e:"+proof.ProofArtifactID, proof.ProofArtifactID); err != nil {
			return fmt.Errorf("upsert proof %s: %w", proof.ProofArtifactID, err)
		}
	}
	for _, observation := range fx.Observations {
		expectedID, expectedLabel, actualID, actualLabel, mismatch := observationLocationContext(observation, fx, locationIDs)
		if _, err := tx.Exec(ctx, // scale-guard:ignore: local E2E seed imports bounded observation fixture rows
			`
INSERT INTO public.weighing_observations (observation_id, tenant_id, campaign_id, campaign_shed_id, animal_id, weight_kg, proof_artifact_id, expected_location_id, expected_location_label, actual_location_id, actual_location_label, mismatch_status, recorded_by, idempotency_key)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
ON CONFLICT (tenant_id, idempotency_key) DO UPDATE SET weight_kg = EXCLUDED.weight_kg, proof_artifact_id = EXCLUDED.proof_artifact_id`,
			observation.ObservationID, fx.TenantID, fx.Campaign.CampaignID, observation.CampaignShedID, observation.AnimalID, observation.WeightKG, observation.ProofArtifactID, expectedID, expectedLabel, actualID, actualLabel, mismatch, operatorID, observation.IDempotencyKey); err != nil {
			return fmt.Errorf("upsert observation %s: %w", observation.ObservationID, err)
		}
	}
	// Lump-sum fixture observations document contract coverage, but are not
	// imported: Shed C must remain pending so Amit can execute it on the phone.
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

func dbGrantRole(role string) string {
	switch strings.TrimSpace(role) {
	case "", "viewer":
		return ""
	case "field_operator":
		return "operator"
	default:
		return role
	}
}

func primaryRoleHint(role string) string {
	switch role {
	case "operator", "pc_director":
		return role
	case "ceo_internal":
		return "other"
	default:
		return "operator"
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
	if scope.WeighingCategory == "per_shed_partition" {
		return "pending"
	}
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

func countLumpSumAssignments(fx fixture) int {
	if fx.Campaign.OperatorCode != "amit_operator" {
		return 0
	}
	count := 0
	for _, scope := range fx.SelectedScopes {
		if scope.WeighingCategory == "per_shed_partition" {
			count++
		}
	}
	return count
}

func shedObservationProofIDs(observation shedObservationFixture) []string {
	ids := make([]string, 0, len(observation.ProofArtifactIDs)+1)
	seen := make(map[string]struct{}, len(observation.ProofArtifactIDs)+1)
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	add(observation.ProofArtifactID)
	for _, id := range observation.ProofArtifactIDs {
		add(id)
	}
	return ids
}

func shedObservationAverageWeight(observation shedObservationFixture, scope scopeFixture) float64 {
	if observation.WeighingResult.AverageWeightKG > 0 {
		return observation.WeighingResult.AverageWeightKG
	}
	if observation.WeighingResult.TotalWeightKG > 0 && scope.ExpectedAnimalCount > 0 {
		return observation.WeighingResult.TotalWeightKG / float64(scope.ExpectedAnimalCount)
	}
	return 0
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
