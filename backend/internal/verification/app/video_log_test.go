package app

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/verification/domain"
	"github.com/vgoats/goatos/backend/internal/verification/ports"
)

const videoLogTenant = "00000000-0000-4000-8000-000000000001"

// videoLogService builds a service whose clock is pinned to a fixed BUSINESS DAY, never an
// hour-anchored instant. AGENTS.md is explicit that a vaccination-adjacent time grain is the
// business day: an hour-relative fixture passes or fails depending on the time of day it runs.
func videoLogService(t *testing.T, repo *fakeRepo) *Service {
	t.Helper()
	svc := NewService(repo, fakeMedia{})
	svc.now = func() time.Time { return time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC) }
	for _, def := range []domain.CategoryDefinition{
		{
			Vertical: "feed", Module: "feed", Category: "feed_distribution",
			ExpectedMedia:         []string{"photo", "video", "video"},
			MediaLabels:           []string{"Feed weight photo", "Feed distribution video", "Water distribution video"},
			NavigationModule:      "feed_direction",
			NavigationModuleLabel: "Feed",
			PageKey:               "feed-distribution", PageLabel: "Feed distribution",
		},
		{
			Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
			ExpectedMedia:         []string{"video"},
			MediaLabels:           []string{"Vaccination proof video"},
			NavigationModule:      "vaccination",
			NavigationModuleLabel: "Vaccination",
			PageKey:               "vaccination-proof", PageLabel: "Vaccination",
		},
	} {
		if err := svc.RegisterCategory(def); err != nil {
			t.Fatalf("RegisterCategory(%q): %v", def.Category, err)
		}
	}
	return svc
}

// TestVideoLogRejectsAFutureDayAndDefaultsToToday pins the day contract. A future day cannot have
// arrivals, and accepting one would render an empty log that reads as "nothing was filmed" rather
// than "that day has not happened".
func TestVideoLogRejectsAFutureDayAndDefaultsToToday(t *testing.T) {
	repo := newFakeRepo()
	svc := videoLogService(t, repo)

	if _, err := svc.VideoLog(context.Background(), ports.VideoLogParams{
		TenantID: videoLogTenant, BusinessDate: "2026-08-20",
	}); err == nil {
		t.Fatal("a future business_date must be refused, not rendered as an empty day")
	}

	out, err := svc.VideoLog(context.Background(), ports.VideoLogParams{TenantID: videoLogTenant})
	if err != nil {
		t.Fatalf("VideoLog: %v", err)
	}
	if out.BusinessDate != "2026-08-14" {
		t.Fatalf("business_date = %q, want the pinned business day 2026-08-14", out.BusinessDate)
	}
	if repo.videoLogParams.BusinessDate != "2026-08-14" {
		t.Fatalf("repository was asked for %q, want the normalized business day", repo.videoLogParams.BusinessDate)
	}
}

// TestVideoLogFailsClosedForAParkScopedCallerWithNoParks pins that an empty authorized park set
// means NOTHING, never everything. A park clamp that degrades to "no filter" against an empty array
// is the classic way a scoped read silently becomes tenant-wide.
func TestVideoLogFailsClosedForAParkScopedCallerWithNoParks(t *testing.T) {
	repo := newFakeRepo()
	repo.videoLogSheds = []domain.VideoLogShed{{ShedID: "shed-1", ProofCount: 3}}
	svc := videoLogService(t, repo)

	out, err := svc.VideoLog(context.Background(), ports.VideoLogParams{
		TenantID: videoLogTenant, ScopeRestricted: true, ParkIDs: nil,
	})
	if err != nil {
		t.Fatalf("VideoLog: %v", err)
	}
	if len(out.Sheds) != 0 {
		t.Fatalf("a park-scoped caller with no authorized parks must see nothing, got %d shed(s)", len(out.Sheds))
	}
	if repo.videoLogParams.TenantID != "" {
		t.Fatal("the repository must not be queried at all for a caller whose scope authorizes no park")
	}
}

// TestVideoLogResolvesProofLabelsByDeclaredOrdinalNotSliceIndex is the sharp one.
//
// Feed distribution declares [weight photo, distribution video, water video] and the registry's
// MediaLabels are POSITIONAL against that order. When a ref fails to resolve, the proof is absent
// from the row -- so labelling by slice index would shift every later proof up one and rename the
// distribution video as the weight photo. The fixture deliberately omits ordinal 1.
func TestVideoLogResolvesProofLabelsByDeclaredOrdinalNotSliceIndex(t *testing.T) {
	repo := newFakeRepo()
	repo.videoLogSheds = []domain.VideoLogShed{{ShedID: "shed-1", ShedLabel: "Castro", PartitionLabel: "2"}}
	repo.videoLogRows = []domain.VideoLogRow{{
		ItemID: "item-1", Module: "feed", Category: "feed_distribution",
		Proofs: []domain.VideoLogProof{
			// Ordinal 1 (the weight photo) never resolved. These are proofs 2 and 3.
			{ProofID: "p2", Ordinal: 2, MediaKind: "video"},
			{ProofID: "p3", Ordinal: 3, MediaKind: "video"},
		},
	}}
	svc := videoLogService(t, repo)

	out, err := svc.VideoLog(context.Background(), ports.VideoLogParams{
		TenantID: videoLogTenant, ShedID: "shed-1#2",
	})
	if err != nil {
		t.Fatalf("VideoLog: %v", err)
	}
	if len(out.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(out.Rows))
	}
	got := out.Rows[0].Proofs
	if got[0].Label != "Feed distribution video" {
		t.Fatalf("proof at ordinal 2 labelled %q, want \"Feed distribution video\" — labels must follow the DECLARED ordinal, not the slice index", got[0].Label)
	}
	if got[1].Label != "Water distribution video" {
		t.Fatalf("proof at ordinal 3 labelled %q, want \"Water distribution video\"", got[1].Label)
	}
	if out.Rows[0].ModuleLabel != "Feed" || out.Rows[0].CategoryLabel != "Feed distribution" {
		t.Fatalf("module/category labels = %q/%q, want the registry's display copy", out.Rows[0].ModuleLabel, out.Rows[0].CategoryLabel)
	}
	if out.Rows[0].NavModule != "feed_direction" {
		t.Fatalf("nav_module = %q, want feed_direction (the queue's filter key, not the stored module code)", out.Rows[0].NavModule)
	}
}

// TestVideoLogKeepsAnArtifactOwnedLabelOverTheRegistryFallback pins the resolution ORDER. The
// producer's own verification_label can name workflow truth the registry cannot, so it must win.
func TestVideoLogKeepsAnArtifactOwnedLabelOverTheRegistryFallback(t *testing.T) {
	repo := newFakeRepo()
	repo.videoLogRows = []domain.VideoLogRow{{
		ItemID: "item-1", Module: "feed", Category: "feed_distribution",
		Proofs: []domain.VideoLogProof{{ProofID: "p1", Ordinal: 1, Label: "Morning weigh-out", MediaKind: "photo"}},
	}}
	svc := videoLogService(t, repo)

	out, err := svc.VideoLog(context.Background(), ports.VideoLogParams{TenantID: videoLogTenant, ShedID: "shed-1"})
	if err != nil {
		t.Fatalf("VideoLog: %v", err)
	}
	if got := out.Rows[0].Proofs[0].Label; got != "Morning weigh-out" {
		t.Fatalf("proof label = %q, want the artifact's own label to win over the registry fallback", got)
	}
}

// TestVideoLogDropsUnknownModuleCodesFromTheShedSummary pins the copy firewall on the summary line.
// verification_items.module holds a raw config code ("feed"); a module the registry does not know
// must be OMITTED rather than printed raw to a leadership or verifier screen.
func TestVideoLogDropsUnknownModuleCodesFromTheShedSummary(t *testing.T) {
	repo := newFakeRepo()
	repo.videoLogSheds = []domain.VideoLogShed{{
		ShedID:  "shed-1",
		Modules: []string{"feed", "some_unregistered_module", "vaccination"},
	}}
	svc := videoLogService(t, repo)

	out, err := svc.VideoLog(context.Background(), ports.VideoLogParams{TenantID: videoLogTenant})
	if err != nil {
		t.Fatalf("VideoLog: %v", err)
	}
	got := out.Sheds[0].Modules
	if len(got) != 2 || got[0] != "Feed" || got[1] != "Vaccination" {
		t.Fatalf("modules = %#v, want the two known display labels only — a raw code must never reach a screen", got)
	}
}

// TestVideoLogReadsRowsOnlyWhenAShedIsSelected pins the two-level bound. A vaccination drive raises
// one item per animal, so fetching every row for the day summary is exactly the unbounded read this
// design exists to avoid.
func TestVideoLogReadsRowsOnlyWhenAShedIsSelected(t *testing.T) {
	repo := newFakeRepo()
	repo.videoLogSheds = []domain.VideoLogShed{{ShedID: "shed-1"}}
	svc := videoLogService(t, repo)

	out, err := svc.VideoLog(context.Background(), ports.VideoLogParams{TenantID: videoLogTenant})
	if err != nil {
		t.Fatalf("VideoLog: %v", err)
	}
	if len(out.Rows) != 0 {
		t.Fatalf("the day summary must carry no rows, got %d", len(out.Rows))
	}
	if repo.videoLogRowParams.TenantID != "" {
		t.Fatal("the row read must not run when no shed is selected")
	}

	if _, err := svc.VideoLog(context.Background(), ports.VideoLogParams{TenantID: videoLogTenant, ShedID: "shed-1#2"}); err != nil {
		t.Fatalf("VideoLog: %v", err)
	}
	if repo.videoLogRowParams.ShedID != "shed-1#2" {
		t.Fatalf("row read shed = %q, want the composite shed key passed through untouched", repo.videoLogRowParams.ShedID)
	}
	if repo.videoLogRowParams.Limit != defaultVideoLogRowLimit {
		t.Fatalf("row read limit = %d, want the default bound %d", repo.videoLogRowParams.Limit, defaultVideoLogRowLimit)
	}
}

// TestVideoLogClampsAnOversizedLimit pins that a caller cannot ask for an unbounded shed day.
func TestVideoLogClampsAnOversizedLimit(t *testing.T) {
	repo := newFakeRepo()
	svc := videoLogService(t, repo)

	if _, err := svc.VideoLog(context.Background(), ports.VideoLogParams{
		TenantID: videoLogTenant, ShedID: "shed-1", Limit: 100000,
	}); err != nil {
		t.Fatalf("VideoLog: %v", err)
	}
	if repo.videoLogRowParams.Limit != maxVideoLogRowLimit {
		t.Fatalf("limit = %d, want it clamped to %d", repo.videoLogRowParams.Limit, maxVideoLogRowLimit)
	}
}
