package app

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

const (
	testTenant   = "00000000-0000-4000-8000-000000000001"
	testActor    = "00000000-0000-4000-8000-000000000101"
	testPark     = "00000000-0000-4000-8000-000000000201"
	testOp       = "00000000-0000-4000-8000-000000000301"
	testShed     = "00000000-0000-4000-8000-000000000401"
	secondShed   = "00000000-0000-4000-8000-000000000402"
	perShedScope = "00000000-0000-4000-8000-000000000403"
	animalOne    = "00000000-0000-4000-8000-000000000601"
	animalTwo    = "00000000-0000-4000-8000-000000000602"
	proofOne     = "00000000-0000-4000-8000-000000000701"
	proofTwo     = "00000000-0000-4000-8000-000000000702"
	proofThree   = "00000000-0000-4000-8000-000000000703"
	proofShed    = "00000000-0000-4000-8000-000000000704"
)

func TestWeighingRBACSeparatesPlanMonitorExecute(t *testing.T) {
	service := NewService(&fakeRepo{})
	ceo := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleCEOInternal}}
	director := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RolePCDirector}}
	operator := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleOperator}}
	cmd := validCreate()

	if _, err := service.CreateCampaign(context.Background(), ceo, cmd); err != nil {
		t.Fatalf("CEO create errored: %v", err)
	}
	if _, err := service.CreateCampaign(context.Background(), director, cmd); err == nil {
		t.Fatal("director created weighing campaign; want forbidden")
	}
	if _, err := service.ListCampaigns(context.Background(), director); err != nil {
		t.Fatalf("director monitor errored: %v", err)
	}
	if _, err := service.ListCampaigns(context.Background(), operator); err != nil {
		t.Fatalf("operator execution list errored: %v", err)
	}
	if _, err := service.ListScopeRoster(context.Background(), operator, "00000000-0000-4000-8000-000000000501", "00000000-0000-4000-8000-000000000801", 250); err != nil {
		t.Fatalf("operator roster read errored: %v", err)
	}
	if _, err := service.ListScopeRoster(context.Background(), director, "00000000-0000-4000-8000-000000000501", "00000000-0000-4000-8000-000000000801", 250); err == nil {
		t.Fatal("director read execution roster; want forbidden")
	}
	if _, err := service.RecordAnimalObservation(context.Background(), operator, domain.RecordAnimalObservation{
		CampaignID: "00000000-0000-4000-8000-000000000501", AnimalID: "00000000-0000-4000-8000-000000000601", WeightKg: 12.3, ProofArtifactID: "00000000-0000-4000-8000-000000000701", ActualLocationID: testShed, IdempotencyKey: "scan-1",
	}); err != nil {
		t.Fatalf("operator execute errored: %v", err)
	}
	if _, err := service.RecordAnimalObservation(context.Background(), director, domain.RecordAnimalObservation{
		CampaignID: "00000000-0000-4000-8000-000000000501", AnimalID: "00000000-0000-4000-8000-000000000601", WeightKg: 12.3, ProofArtifactID: "00000000-0000-4000-8000-000000000701", ActualLocationID: testShed, IdempotencyKey: "scan-2",
	}); err == nil {
		t.Fatal("director executed weighing observation; want forbidden")
	}
}

func TestPerShedCategoryRoutesToShedObservationOnly(t *testing.T) {
	repo := &fakeRepo{}
	service := NewService(repo)
	operator := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleOperator}}
	if _, err := service.RecordShedObservation(context.Background(), operator, domain.RecordShedObservation{
		CampaignID: "00000000-0000-4000-8000-000000000501", CampaignShedID: "00000000-0000-4000-8000-000000000801", WeightKg: 450, ProofArtifactID: "00000000-0000-4000-8000-000000000701", IdempotencyKey: "shed-1",
	}); err != nil {
		t.Fatalf("shed observation errored: %v", err)
	}
	if repo.animalWrites != 0 {
		t.Fatalf("per-shed observation wrote %d animal rows; want 0", repo.animalWrites)
	}
	if repo.shedWrites != 1 {
		t.Fatalf("shed writes = %d, want 1", repo.shedWrites)
	}
}

func TestWeighingSeedScenarioDrivesEndToEndServiceContract(t *testing.T) {
	repo := newScenarioRepo()
	service := NewService(repo)
	ctx := context.Background()
	ceo := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleCEOInternal}}
	director := domain.Actor{TenantID: testTenant, UserID: "00000000-0000-4000-8000-000000000102", Roles: []string{permissions.RolePCDirector}}
	operator := domain.Actor{TenantID: testTenant, UserID: testOp, Roles: []string{permissions.RoleOperator}}

	campaign, err := service.CreateCampaign(ctx, ceo, domain.CreateCampaign{
		ParkID:            testPark,
		PeriodStartDate:   "2026-07-27",
		PeriodEndDate:     "2026-08-02",
		StartBusinessDate: "2026-07-29",
		PlannedCapPerDay:  100,
		OperatorUserID:    testOp,
		IdempotencyKey:    "weighing-seed:create",
		Sheds: []domain.CreateCampaignShed{
			{LocationID: testShed, LocationType: "shed", DisplayName: "Kid Shed A", WeighingCategory: domain.CategoryIndividualAnimal},
			{LocationID: secondShed, LocationType: "shed", DisplayName: "Kid Shed B", WeighingCategory: domain.CategoryIndividualAnimal},
			{LocationID: perShedScope, LocationType: "shed", DisplayName: "Kid Shed C", WeighingCategory: domain.CategoryPerShedPartition},
		},
	})
	if err != nil {
		t.Fatalf("create campaign: %v", err)
	}
	if campaign.Progress.IndividualExpectedCount != 2 || campaign.Progress.PerScopeExpectedCount != 1 {
		t.Fatalf("category-aware progress after create = %+v, want 2 individual + 1 per-scope", campaign.Progress)
	}

	if _, err := service.PublishCampaign(ctx, ceo, campaign.CampaignID, "weighing-seed:publish"); err != nil {
		t.Fatalf("publish campaign: %v", err)
	}
	if _, err := service.PublishCampaign(ctx, director, campaign.CampaignID, "weighing-seed:publish-director"); !errors.Is(err, ports.ErrForbidden) {
		t.Fatalf("director publish err = %v, want forbidden", err)
	}
	roster, err := service.ListScopeRoster(ctx, operator, campaign.CampaignID, repo.shedByLocation[testShed].CampaignShedID, 250)
	if err != nil {
		t.Fatalf("operator roster read: %v", err)
	}
	if len(roster) != 1 || roster[0].AnimalID != animalOne || roster[0].PrimaryIdentifier != "RFID-ONE" {
		t.Fatalf("roster = %+v, want animal one with RFID", roster)
	}

	first, err := service.RecordAnimalObservation(ctx, operator, domain.RecordAnimalObservation{
		CampaignID: campaign.CampaignID, AnimalID: animalOne, WeightKg: 10.2, ProofArtifactID: proofOne, IdempotencyKey: "weighing-seed:animal-1",
	})
	if err != nil {
		t.Fatalf("record first animal: %v", err)
	}
	if first.CampaignShedID != repo.shedByLocation[testShed].CampaignShedID || first.ExpectedLocationID != testShed || first.ActualLocationID != testShed {
		t.Fatalf("first animal context = %+v, want expected current shed", first)
	}
	replay, err := service.RecordAnimalObservation(ctx, operator, domain.RecordAnimalObservation{
		CampaignID: campaign.CampaignID, AnimalID: animalOne, WeightKg: 10.2, ProofArtifactID: proofOne, IdempotencyKey: "weighing-seed:animal-1",
	})
	if err != nil {
		t.Fatalf("replay animal observation: %v", err)
	}
	if replay.ObservationID != first.ObservationID || repo.animalWrites != 1 {
		t.Fatalf("idempotent replay = %+v writes=%d, want original observation and one write", replay, repo.animalWrites)
	}

	wrongShed, err := service.RecordAnimalObservation(ctx, operator, domain.RecordAnimalObservation{
		CampaignID: campaign.CampaignID, AnimalID: animalTwo, WeightKg: 11.4, ProofArtifactID: proofTwo, IdempotencyKey: "weighing-seed:wrong-shed",
	})
	if err != nil {
		t.Fatalf("record wrong-shed animal: %v", err)
	}
	if wrongShed.ExpectedLocationID != secondShed || wrongShed.ActualLocationID != testShed || wrongShed.ActualLocationLabel != "Kid Shed A" {
		t.Fatalf("wrong-shed context = %+v, want expected shed B and actual shed A", wrongShed)
	}

	shedObs, err := service.RecordShedObservation(ctx, operator, domain.RecordShedObservation{
		CampaignID: campaign.CampaignID, CampaignShedID: repo.shedByLocation[perShedScope].CampaignShedID, WeightKg: 452.5, ProofArtifactID: proofShed, IdempotencyKey: "weighing-seed:shed-c",
	})
	if err != nil {
		t.Fatalf("record per-shed observation: %v", err)
	}
	if shedObs.AnimalID != "" || repo.latestAnimalWeightWrites != 0 {
		t.Fatalf("per-shed observation touched animal truth: obs=%+v latestWrites=%d", shedObs, repo.latestAnimalWeightWrites)
	}
	if _, err := service.RecordShedObservation(ctx, operator, domain.RecordShedObservation{
		CampaignID: campaign.CampaignID, CampaignShedID: repo.shedByLocation[testShed].CampaignShedID, WeightKg: 220, ProofArtifactID: proofShed, IdempotencyKey: "weighing-seed:bad-shed-category",
	}); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("individual shed accepted per-shed observation err = %v, want not found", err)
	}

	if _, err := service.RecordAnimalObservation(ctx, director, domain.RecordAnimalObservation{
		CampaignID: campaign.CampaignID, AnimalID: animalOne, WeightKg: 10.8, ProofArtifactID: proofThree, IdempotencyKey: "weighing-seed:director-execute",
	}); !errors.Is(err, ports.ErrForbidden) {
		t.Fatalf("director execute err = %v, want forbidden", err)
	}
}

func validCreate() domain.CreateCampaign {
	return domain.CreateCampaign{
		ParkID: testPark, PeriodStartDate: "2026-07-27", PeriodEndDate: "2026-08-02", StartBusinessDate: "2026-07-29", PlannedCapPerDay: 100, OperatorUserID: testOp, IdempotencyKey: "create-1",
		Sheds: []domain.CreateCampaignShed{{LocationID: testShed, LocationType: "shed", DisplayName: "Kid Shed", WeighingCategory: domain.CategoryIndividualAnimal}},
	}
}

type fakeRepo struct {
	animalWrites int
	shedWrites   int
}

func (f fakeRepo) CreateCampaign(context.Context, domain.CreateCampaign) (domain.Campaign, error) {
	return domain.Campaign{CampaignID: "00000000-0000-4000-8000-000000000501"}, nil
}
func (f fakeRepo) PublishCampaign(context.Context, string, string, string, string) (domain.Campaign, error) {
	return domain.Campaign{}, nil
}
func (f fakeRepo) ListCampaigns(context.Context, string) ([]domain.Campaign, error) { return nil, nil }
func (f fakeRepo) ListScopeRoster(context.Context, string, string, string, int) ([]domain.ExpectedAnimal, error) {
	return []domain.ExpectedAnimal{{AnimalID: animalOne, PrimaryIdentifier: "RFID-ONE"}}, nil
}
func (f *fakeRepo) RecordAnimalObservation(context.Context, domain.RecordAnimalObservation) (domain.Observation, error) {
	f.animalWrites++
	return domain.Observation{}, nil
}
func (f *fakeRepo) RecordShedObservation(context.Context, domain.RecordShedObservation) (domain.Observation, error) {
	f.shedWrites++
	return domain.Observation{}, nil
}
func (f fakeRepo) RefreshAvailability(context.Context, string, string) error { return nil }

type scenarioRepo struct {
	campaign                 domain.Campaign
	shedByLocation           map[string]domain.CampaignShed
	expectedByAnimal         map[string]domain.ExpectedAnimal
	currentLocation          map[string]string
	currentLocationName      map[string]string
	animalByIdem             map[string]domain.Observation
	shedByIdem               map[string]domain.Observation
	animalWrites             int
	shedWrites               int
	latestAnimalWeightWrites int
}

func newScenarioRepo() *scenarioRepo {
	return &scenarioRepo{
		shedByLocation:   map[string]domain.CampaignShed{},
		expectedByAnimal: map[string]domain.ExpectedAnimal{},
		currentLocation: map[string]string{
			animalOne: testShed,
			animalTwo: testShed,
		},
		currentLocationName: map[string]string{
			testShed:     "Kid Shed A",
			secondShed:   "Kid Shed B",
			perShedScope: "Kid Shed C",
		},
		animalByIdem: map[string]domain.Observation{},
		shedByIdem:   map[string]domain.Observation{},
	}
}

func (r *scenarioRepo) CreateCampaign(_ context.Context, cmd domain.CreateCampaign) (domain.Campaign, error) {
	r.campaign = domain.Campaign{
		CampaignID:        "00000000-0000-4000-8000-000000000501",
		TenantID:          cmd.TenantID,
		ParkID:            cmd.ParkID,
		PeriodStartDate:   cmd.PeriodStartDate,
		PeriodEndDate:     cmd.PeriodEndDate,
		StartBusinessDate: cmd.StartBusinessDate,
		Status:            domain.StatusDraft,
		PlannedCapPerDay:  cmd.PlannedCapPerDay,
		OperatorUserID:    cmd.OperatorUserID,
		CreatedBy:         cmd.CreatedBy,
	}
	for i, shed := range cmd.Sheds {
		campaignShed := domain.CampaignShed{
			CampaignShedID: []string{
				"00000000-0000-4000-8000-000000000801",
				"00000000-0000-4000-8000-000000000802",
				"00000000-0000-4000-8000-000000000803",
			}[i],
			CampaignID:       r.campaign.CampaignID,
			LocationID:       shed.LocationID,
			LocationType:     shed.LocationType,
			DisplayName:      shed.DisplayName,
			WeighingCategory: shed.WeighingCategory,
			Status:           "pending",
		}
		campaignShed.ExpectedAnimalCount = 1
		r.shedByLocation[shed.LocationID] = campaignShed
		r.campaign.Sheds = append(r.campaign.Sheds, campaignShed)
	}
	r.expectedByAnimal[animalOne] = domain.ExpectedAnimal{CampaignID: r.campaign.CampaignID, AnimalID: animalOne, ExpectedLocationID: testShed, ExpectedLocationLabel: "Kid Shed A", Status: "pending"}
	r.expectedByAnimal[animalTwo] = domain.ExpectedAnimal{CampaignID: r.campaign.CampaignID, AnimalID: animalTwo, ExpectedLocationID: secondShed, ExpectedLocationLabel: "Kid Shed B", Status: "pending"}
	r.campaign.Progress = scenarioProgress(r.campaign.Sheds, r.expectedByAnimal)
	return r.campaign, nil
}

func (r *scenarioRepo) PublishCampaign(_ context.Context, tenantID, campaignID, _ string, _ string) (domain.Campaign, error) {
	if tenantID != r.campaign.TenantID || campaignID != r.campaign.CampaignID {
		return domain.Campaign{}, ports.ErrNotFound
	}
	r.campaign.Status = domain.StatusPublished
	return r.campaign, nil
}

func (r *scenarioRepo) ListCampaigns(context.Context, string) ([]domain.Campaign, error) {
	return []domain.Campaign{r.campaign}, nil
}

func (r *scenarioRepo) ListScopeRoster(_ context.Context, tenantID, campaignID, campaignShedID string, limit int) ([]domain.ExpectedAnimal, error) {
	if tenantID != r.campaign.TenantID || campaignID != r.campaign.CampaignID {
		return nil, ports.ErrNotFound
	}
	out := []domain.ExpectedAnimal{}
	for _, animal := range r.expectedByAnimal {
		shed := r.shedByLocation[animal.ExpectedLocationID]
		if shed.CampaignShedID != campaignShedID {
			continue
		}
		switch animal.AnimalID {
		case animalOne:
			animal.DisplayAnimalID = "KID-A-001"
			animal.PrimaryIdentifier = "RFID-ONE"
		case animalTwo:
			animal.DisplayAnimalID = "KID-B-001"
			animal.PrimaryIdentifier = "RFID-TWO"
		}
		animal.CampaignShedID = campaignShedID
		animal.Seq = int64(len(out) + 1)
		out = append(out, animal)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	if len(out) == 0 {
		return nil, ports.ErrNotFound
	}
	return out, nil
}

func (r *scenarioRepo) RecordAnimalObservation(_ context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
	if obs, ok := r.animalByIdem[cmd.IdempotencyKey]; ok {
		return obs, nil
	}
	expected, ok := r.expectedByAnimal[cmd.AnimalID]
	if !ok {
		return domain.Observation{}, ports.ErrNotFound
	}
	expectedShed := r.shedByLocation[expected.ExpectedLocationID]
	actualLocation := r.currentLocation[cmd.AnimalID]
	obs := domain.Observation{
		ObservationID:       fmt.Sprintf("00000000-0000-4000-8000-00000000090%d", len(r.animalByIdem)+1),
		CampaignID:          cmd.CampaignID,
		CampaignShedID:      expectedShed.CampaignShedID,
		AnimalID:            cmd.AnimalID,
		WeightKg:            cmd.WeightKg,
		ProofArtifactID:     cmd.ProofArtifactID,
		ExpectedLocationID:  expected.ExpectedLocationID,
		ActualLocationID:    actualLocation,
		ActualLocationLabel: r.currentLocationName[actualLocation],
	}
	r.animalByIdem[cmd.IdempotencyKey] = obs
	r.animalWrites++
	expected.Status = "weighed"
	if actualLocation == expected.ExpectedLocationID {
		expected.AvailabilityStatus = domain.AvailabilityExpectedShed
	} else {
		expected.AvailabilityStatus = domain.AvailabilityMovedOtherShed
	}
	expected.CurrentLocationID = actualLocation
	expected.CurrentLocationLabel = r.currentLocationName[actualLocation]
	r.expectedByAnimal[cmd.AnimalID] = expected
	r.campaign.Progress = scenarioProgress(r.campaign.Sheds, r.expectedByAnimal)
	return obs, nil
}

func (r *scenarioRepo) RecordShedObservation(_ context.Context, cmd domain.RecordShedObservation) (domain.Observation, error) {
	if obs, ok := r.shedByIdem[cmd.IdempotencyKey]; ok {
		return obs, nil
	}
	var campaignShed domain.CampaignShed
	var found bool
	for _, shed := range r.shedByLocation {
		if shed.CampaignShedID == cmd.CampaignShedID {
			campaignShed = shed
			found = true
			break
		}
	}
	if !found || campaignShed.WeighingCategory != domain.CategoryPerShedPartition {
		return domain.Observation{}, ports.ErrNotFound
	}
	campaignShed.Status = "completed"
	r.shedByLocation[campaignShed.LocationID] = campaignShed
	for i := range r.campaign.Sheds {
		if r.campaign.Sheds[i].CampaignShedID == campaignShed.CampaignShedID {
			r.campaign.Sheds[i] = campaignShed
		}
	}
	obs := domain.Observation{
		ObservationID:   "00000000-0000-4000-8000-000000000951",
		CampaignID:      cmd.CampaignID,
		CampaignShedID:  cmd.CampaignShedID,
		WeightKg:        cmd.WeightKg,
		ProofArtifactID: cmd.ProofArtifactID,
	}
	r.shedByIdem[cmd.IdempotencyKey] = obs
	r.shedWrites++
	r.campaign.Progress = scenarioProgress(r.campaign.Sheds, r.expectedByAnimal)
	return obs, nil
}

func (r *scenarioRepo) RefreshAvailability(context.Context, string, string) error { return nil }

func scenarioProgress(sheds []domain.CampaignShed, animals map[string]domain.ExpectedAnimal) domain.Progress {
	progress := domain.Progress{}
	for _, shed := range sheds {
		switch shed.WeighingCategory {
		case domain.CategoryIndividualAnimal:
			progress.IndividualExpectedCount += shed.ExpectedAnimalCount
		case domain.CategoryPerShedPartition:
			progress.PerScopeExpectedCount++
			if shed.Status == "completed" {
				progress.PerScopeCompletedCount++
			}
		}
	}
	for _, animal := range animals {
		if animal.Status == "weighed" {
			progress.IndividualCompletedCount++
		}
		if animal.AvailabilityStatus == domain.AvailabilityMovedOtherShed {
			progress.WrongShedCount++
		}
	}
	progress.RemainingCount = (progress.IndividualExpectedCount - progress.IndividualCompletedCount) + (progress.PerScopeExpectedCount - progress.PerScopeCompletedCount)
	return progress
}
