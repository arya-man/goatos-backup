package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

const (
	testTenant = "00000000-0000-4000-8000-000000000001"
	testActor  = "00000000-0000-4000-8000-000000000101"
	testPark   = "00000000-0000-4000-8000-000000000201"
	testOp     = "00000000-0000-4000-8000-000000000301"
	testShed   = "00000000-0000-4000-8000-000000000401"
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
func (f *fakeRepo) RecordAnimalObservation(context.Context, domain.RecordAnimalObservation) (domain.Observation, error) {
	f.animalWrites++
	return domain.Observation{}, nil
}
func (f *fakeRepo) RecordShedObservation(context.Context, domain.RecordShedObservation) (domain.Observation, error) {
	f.shedWrites++
	return domain.Observation{}, nil
}
func (f fakeRepo) RefreshAvailability(context.Context, string, string) error { return nil }
