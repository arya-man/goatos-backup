package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// Phase A E2E (2026-09-17): the served rule set must be Rules.ServedRules() on EVERY client read.
// Live on the throwaway stack a version authored with one whole-pen slot 1..2 (document mirror
// left at 1..5) served video_max 5 on the planner catalog AND the task read, while the submit
// judged an older phone against the derived window 1..2 -- five videos recorded as the screen
// allowed, then 422 weighing_video_count. The served capture sections were also absent, not
// "filled explicitly" as docs/decisions/weighing-sop.md promises.

type campaignByIDRepo struct {
	fakeRepo
	campaign domain.Campaign
}

func (r *campaignByIDRepo) CampaignByID(context.Context, string, string, ports.CampaignAccess) (domain.Campaign, error) {
	return r.campaign, nil
}

func driftedMirrorRules(version int) ports.StaticRules {
	r := domain.SeededRules()
	r.Version = version
	r.Capture.LumpSum.VideoMin, r.Capture.LumpSum.VideoMax = 1, 5
	r.Capture.LumpSum.Proofs = []domain.CountedProofSlot{{Key: "pen_video", Title: "Weighing video", Kind: "video", Min: 1, Max: 2}}
	return ports.StaticRules{Rules: r}
}

func assertServed(t *testing.T, where string, got domain.Rules) {
	t.Helper()
	if lo, hi := got.Capture.LumpSum.VideoMin, got.Capture.LumpSum.VideoMax; lo != 1 || hi != 2 {
		t.Fatalf("%s: served video window %d..%d, want the derived mirror 1..2 the submit judges", where, lo, hi)
	}
	if len(got.Capture.Individual.Proofs) != 1 || got.Capture.Individual.Proofs[0].Key != domain.IndividualProofAnimalVideo {
		t.Fatalf("%s: per-animal slots not served explicitly: %+v", where, got.Capture.Individual.Proofs)
	}
	if got.Capture.LumpSum.Questions == nil || got.Capture.Individual.Questions == nil {
		t.Fatalf("%s: question lists not served explicitly", where)
	}
}

func TestPlannerCatalogServesTheDerivedCaptureRules(t *testing.T) {
	ceo := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleCEOInternal}}
	service := NewService(&capturingRepo{}).WithSOPRules(driftedMirrorRules(12), pinnedVersion(0))
	catalog, err := service.PlannerCatalog(context.Background(), ceo, "2026-07-29")
	if err != nil {
		t.Fatal(err)
	}
	assertServed(t, "planner catalog", catalog.SOP)
}

func TestTaskReadServesThePinnedDerivedCaptureRules(t *testing.T) {
	ceo := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleCEOInternal}}
	repo := &campaignByIDRepo{campaign: domain.Campaign{CampaignID: "00000000-0000-4000-8000-000000000501", ParkID: testPark, SOPVersion: 12}}
	service := NewService(repo).WithSOPRules(driftedMirrorRules(12), pinnedVersion(12))
	campaign, err := service.GetCampaign(context.Background(), ceo, "00000000-0000-4000-8000-000000000501")
	if err != nil {
		t.Fatal(err)
	}
	if campaign.SOP == nil {
		t.Fatal("task read carries no rules")
	}
	assertServed(t, "task read", *campaign.SOP)
}
