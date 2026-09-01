package app

import (
	"context"
	"github.com/vgoats/goatos/backend/internal/health/domain"
	"testing"
	"time"
)

const (
	testTenant = "10000000-0000-4000-8000-000000000001"
	testActor  = "20000000-0000-4000-8000-000000000001"
	testGoat   = "30000000-0000-4000-8000-000000000001"
)

type fakeRepo struct{ list domain.ListFilter }

func (*fakeRepo) OpenCase(context.Context, domain.OpenCaseInput) (domain.OpenCaseResult, error) {
	return domain.OpenCaseResult{DurationDays: domain.DefaultDurationDays}, nil
}
func (f *fakeRepo) ListWorkItems(_ context.Context, in domain.ListFilter) (domain.WorkItemPage, error) {
	f.list = in
	return domain.WorkItemPage{Items: []domain.WorkItem{}}, nil
}
func (*fakeRepo) GetWorkItem(context.Context, string, string) (domain.WorkItemDetail, error) {
	return domain.WorkItemDetail{}, nil
}
func (*fakeRepo) CompleteWorkItem(context.Context, domain.CompleteInput) (domain.CompleteResult, error) {
	return domain.CompleteResult{}, nil
}
func (*fakeRepo) CloseCase(context.Context, domain.CloseCaseInput) (domain.CloseCaseResult, error) {
	return domain.CloseCaseResult{}, nil
}
func (*fakeRepo) HoldForDeathReview(context.Context, string, string) error       { return nil }
func (*fakeRepo) ResumeAfterDeathRejected(context.Context, string, string) error { return nil }
func (*fakeRepo) CloseForApprovedDeath(context.Context, string, string) error    { return nil }
func TestListWorkItemsCapsPageAtTwenty(t *testing.T) {
	repo := &fakeRepo{}
	_, err := NewService(repo).ListWorkItems(context.Background(), domain.ListFilter{TenantID: testTenant, AgeBand: domain.AgeBandAdult, Date: "2026-07-30", Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	if repo.list.Limit != domain.MaxPageSize {
		t.Fatalf("limit=%d", repo.list.Limit)
	}
}
func TestOpenCaseRequiresStableIdempotencyKey(t *testing.T) {
	_, err := NewService(&fakeRepo{}).OpenCase(context.Background(), domain.OpenCaseInput{TenantID: testTenant, ActorID: testActor, GoatID: testGoat, DiseaseKey: "fever", AgeBand: domain.AgeBandAdult, StartDate: time.Now()})
	if err != ErrInvalidInput {
		t.Fatalf("err=%v want invalid input", err)
	}
}

func TestListWorkItemsMapsPublicHeldStatusAndRejectsInvalidScope(t *testing.T) {
	repo := &fakeRepo{}
	service := NewService(repo)
	_, err := service.ListWorkItems(context.Background(), domain.ListFilter{
		TenantID: testTenant, AgeBand: domain.AgeBandKid, Date: "2026-07-30", Status: "held",
	})
	if err != nil || repo.list.Status != "held_death_review" {
		t.Fatalf("held mapping=%q err=%v", repo.list.Status, err)
	}
	_, err = service.ListWorkItems(context.Background(), domain.ListFilter{
		TenantID: testTenant, AgeBand: domain.AgeBandKid, Date: "2026-07-30", ParkID: "not-a-uuid",
	})
	if err != ErrInvalidInput {
		t.Fatalf("invalid park err=%v, want ErrInvalidInput", err)
	}
}
