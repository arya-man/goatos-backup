package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/verification/domain"
	"github.com/vgoats/goatos/backend/internal/verification/ports"
)

const samplingTenant = "11111111-1111-4111-8111-111111111111"

func samplingService(t *testing.T, repo *fakeRepo, now time.Time) *Service {
	t.Helper()
	svc := NewService(repo, fakeMedia{})
	svc.now = func() time.Time { return now }
	for _, def := range []domain.CategoryDefinition{
		{
			Vertical: "feed", Module: "feed", Category: "feed_distribution",
			NavigationModule: "feed_direction", NavigationModuleLabel: "Feed",
			PageKey: "feed_distribution", PageLabel: "Feed Distribution", PageOrder: 1,
		},
		{
			// The locked shape: the verifier RECORDS the packed quantities off the video, so the
			// approve must carry them and an unwatched video would complete a pen-day with no
			// quantity at all.
			Vertical: "feed", Module: "feed", Category: "feed_packing",
			NavigationModule: "feed_direction", NavigationModuleLabel: "Feed",
			PageKey: "feed_packing", PageLabel: "Feed Packing", PageOrder: 2,
			MeasurementCorrection: &domain.MeasurementCorrectionSpec{
				Title: "Record the packed quantities", Help: "Enter the packed weight you can see.",
				ValueLabel: "Packed quantity (kg)", SubmitLabel: "Save packed quantities",
				RequiredForApprove: true, PerItemFields: true,
			},
		},
		{
			// Weighing declares a measurement but does NOT require it: the operator already
			// recorded a weight, so a blank field means "his weight is right". Samplable.
			Vertical: "weighing", Module: "weighing", Category: "weighing_proof",
			NavigationModule: "weighing", NavigationModuleLabel: "Weighing",
			PageKey: "weighing", PageLabel: "Weighing", PageOrder: 1,
			MeasurementCorrection: &domain.MeasurementCorrectionSpec{
				Title: "Correct the weight", Help: "Enter the weight you can see. It replaces the recorded one.",
				ValueLabel: "Corrected weight (kg)", SubmitLabel: "Save corrected weight",
			},
		},
	} {
		if err := svc.RegisterCategory(def); err != nil {
			t.Fatalf("register %s: %v", def.Category, err)
		}
	}
	return svc
}

// TestSamplingBucketMatchesTheGeneratedColumn pins the Go draw to the SQL generated column in
// migration 000214. The two MUST agree: the database decides which items a verifier sees and which
// the closeout settles, and a Go helper that drew differently would make every test that used it
// prove something the running system does not do.
//
// The expectations were produced by the database itself:
//
//	select mod(('x' || substr(md5(v), 1, 6))::bit(24)::int, 100)
func TestSamplingBucketMatchesTheGeneratedColumn(t *testing.T) {
	for _, tc := range []struct {
		itemID string
		want   int
	}{
		{"11111111-1111-4111-8111-111111111111", 97},
		{"22222222-2222-4222-8222-222222222222", 42},
		{"9f7a1c3e-0b2d-4e6f-8a1b-5c9d0e2f4a6b", 4},
		{"00000000-0000-4000-9000-000000000001", 63},
	} {
		if got := domain.SamplingBucket(tc.itemID); got != tc.want {
			t.Fatalf("SamplingBucket(%s) = %d, want %d (the value Postgres computes)", tc.itemID, got, tc.want)
		}
	}
	// Postgres renders uuid::text lower-case, so an upper-case id from a client must draw the same
	// bucket as its own row rather than a second, contradictory one.
	if upper, lower := domain.SamplingBucket("9F7A1C3E-0B2D-4E6F-8A1B-5C9D0E2F4A6B"), domain.SamplingBucket("9f7a1c3e-0b2d-4e6f-8a1b-5c9d0e2f4a6b"); upper != lower {
		t.Fatalf("case changed the draw: %d vs %d", upper, lower)
	}
}

// TestRaisingTheShareOnlyEverAddsVideos is the property that makes "takes effect the same day"
// safe to offer: the maintainer was told that raising 40% to 60% pulls MORE of today's videos into
// the verifier's queue and never retracts one she is already holding. Strict `<` is what delivers
// that; `<=` or a re-drawn bucket would not.
func TestRaisingTheShareOnlyEverAddsVideos(t *testing.T) {
	for bucket := 0; bucket < domain.SamplingBucketCount; bucket++ {
		for percent := 0; percent < 100; percent++ {
			if domain.InSample(bucket, percent) && !domain.InSample(bucket, percent+1) {
				t.Fatalf("bucket %d left the sample when the share rose from %d to %d", bucket, percent, percent+1)
			}
		}
	}
	if domain.InSample(0, 0) {
		t.Fatal("0% drew a video; nothing may be selected at zero")
	}
	if !domain.InSample(99, 100) {
		t.Fatal("100% left a video out; every video must be selected at full")
	}
}

// TestHerShareFullyReviewedReadsOneHundredPercent is the maintainer's own sentence: at 40% on feed
// packing, reviewing all 40% is 100% of her work.
func TestHerShareFullyReviewedReadsOneHundredPercent(t *testing.T) {
	if got := (domain.SamplingDayStats{Captured: 100, Selected: 40, Reviewed: 40}).ProgressPercent(); got != 100 {
		t.Fatalf("40 of 40 reviewed = %d%%, want 100%%", got)
	}
	if got := (domain.SamplingDayStats{Captured: 100, Selected: 40, Reviewed: 20}).ProgressPercent(); got != 50 {
		t.Fatalf("20 of 40 reviewed = %d%%, want 50%%", got)
	}
	// A day that drew nothing is COMPLETE, not zero: she owes nothing, and 0% would read as a
	// verifier who has fallen behind on work that does not exist.
	if got := (domain.SamplingDayStats{Captured: 12, Selected: 0}).ProgressPercent(); got != 100 {
		t.Fatalf("nothing drawn = %d%%, want 100%%", got)
	}
}

// TestACategoryWhoseApproveCarriesTheNumberCannotBeSampled is the safety rule behind the whole
// feature. Feed packing's operator submits a video AND NO NUMBER: the verifier reads the packed
// quantities off the clip, and the producer's applier refuses an approve carrying none. Waiving one
// of those videos would either complete a pen-day with no quantity recorded or strand the item
// mid-apply, so the share is refused rather than accepted-and-ignored.
func TestACategoryWhoseApproveCarriesTheNumberCannotBeSampled(t *testing.T) {
	repo := newFakeRepo()
	svc := samplingService(t, repo, time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC))

	_, err := svc.SetSamplingPolicy(context.Background(), domain.SetSamplingPolicy{
		TenantID: samplingTenant, Category: "feed_packing", Percent: 40,
	})
	if err == nil {
		t.Fatal("feed packing accepted a share; the verifier records its quantities, so every video must be watched")
	}
	if len(repo.samplingUpserts) != 0 {
		t.Fatalf("a refused share still wrote %d policy rows", len(repo.samplingUpserts))
	}
	// And it is refused with a REASON the CEO can read, not a bare rejection.
	var badRequest *Error
	if !errors.As(err, &badRequest) || badRequest.Code != "sampling_not_available" {
		t.Fatalf("refusal = %v, want sampling_not_available", err)
	}
	if badRequest.Message != domain.SamplingLockedReason {
		t.Fatalf("refusal message = %q, want the backend-owned reason", badRequest.Message)
	}

	// The same category is still LISTED, at 100 and locked, so the CEO sees why rather than
	// wondering where the module went.
	repo.samplingPolicies = []ports.SamplingPolicyRow{{Category: "feed_packing", Percent: 10, EffectiveBusinessDate: "2026-08-01"}}
	overview, err := svc.SamplingOverview(context.Background(), samplingTenant, "")
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	row := findSamplingRow(t, overview, "feed_packing")
	if row.Waivable {
		t.Fatal("feed packing reported as samplable")
	}
	// A STALE stored row must not be advertised: the queue never applies it, so reporting 10% would
	// tell the CEO a share is in force that is not.
	if row.SamplePercent != 100 {
		t.Fatalf("locked row advertised %d%%, want 100%% -- the queue applies no share here", row.SamplePercent)
	}
	if row.LockedReason == "" {
		t.Fatal("locked row carried no reason; a disabled control with no reason is the defect this prevents")
	}
	// Weighing declares a measurement too, but does not REQUIRE it, so it stays samplable.
	if !findSamplingRow(t, overview, "weighing_proof").Waivable {
		t.Fatal("weighing was locked; a blank correction means the operator's weight stands, so it is a spot check")
	}
}

// TestTheShareTakesEffectTodayAndLeavesEarlierDaysAlone pins both halves of the effective-dating
// rule: the change lands on TODAY (so the verifier's queue changes now), and the date comes from
// the SERVER -- a caller that could name its own date could rewrite a day she has already worked.
func TestTheShareTakesEffectTodayAndLeavesEarlierDaysAlone(t *testing.T) {
	repo := newFakeRepo()
	// 2026-08-26 18:40 UTC is already 2026-08-27 in Asia/Kolkata; the business day, not the UTC
	// day, is what a farm day means here.
	svc := samplingService(t, repo, time.Date(2026, 8, 26, 18, 40, 0, 0, time.UTC))

	if _, err := svc.SetSamplingPolicy(context.Background(), domain.SetSamplingPolicy{
		TenantID: samplingTenant, Category: "feed_distribution", Percent: 40,
		EffectiveBusinessDate: "2026-01-01", // a client trying to name its own day
	}); err != nil {
		t.Fatalf("set share: %v", err)
	}
	if len(repo.samplingUpserts) != 1 {
		t.Fatalf("wrote %d policy rows, want 1", len(repo.samplingUpserts))
	}
	if got := repo.samplingUpserts[0].EffectiveBusinessDate; got != "2026-08-27" {
		t.Fatalf("effective date = %q, want the server's business day 2026-08-27 (never the client's)", got)
	}
}

// TestAShareOutsideTheRangeIsRefusedNotClamped: present-but-invalid is a refusal, never a silent
// default (the authored-config rule). An author who typed 140 must be told, not quietly given 100.
func TestAShareOutsideTheRangeIsRefusedNotClamped(t *testing.T) {
	repo := newFakeRepo()
	svc := samplingService(t, repo, time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC))
	for _, percent := range []int{-1, 101, 140} {
		if _, err := svc.SetSamplingPolicy(context.Background(), domain.SetSamplingPolicy{
			TenantID: samplingTenant, Category: "feed_distribution", Percent: percent,
		}); err == nil {
			t.Fatalf("%d%% was accepted", percent)
		}
	}
	if len(repo.samplingUpserts) != 0 {
		t.Fatalf("an out-of-range share still wrote %d rows", len(repo.samplingUpserts))
	}
	// 0 and 100 are both REAL settings and must go through: 0 is "review none of this today".
	for _, percent := range []int{0, 100} {
		if _, err := svc.SetSamplingPolicy(context.Background(), domain.SetSamplingPolicy{
			TenantID: samplingTenant, Category: "feed_distribution", Percent: percent,
		}); err != nil {
			t.Fatalf("%d%% was refused: %v", percent, err)
		}
	}
}

// TestTheOverviewSpeaksFarmLanguageAndNeverALoneCategoryToken: every visible word comes from the
// registry's own display copy. The category token travels for the write and must never be the only
// thing naming a row -- the copy firewall bans config vocabulary from visible UI.
func TestTheOverviewSpeaksFarmLanguageAndNeverALoneCategoryToken(t *testing.T) {
	repo := newFakeRepo()
	repo.samplingStats = map[string]domain.SamplingDayStats{
		"feed_distribution": {Captured: 50, Selected: 20, Reviewed: 20, AutoAccepted: 30},
	}
	svc := samplingService(t, repo, time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC))
	overview, err := svc.SamplingOverview(context.Background(), samplingTenant, "")
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if overview.BusinessDate != "2026-08-26" {
		t.Fatalf("business date = %q, want today", overview.BusinessDate)
	}
	row := findSamplingRow(t, overview, "feed_distribution")
	if row.ModuleLabel != "Feed" || row.PageLabel != "Feed Distribution" {
		t.Fatalf("labels = %q/%q, want the registry's own copy", row.ModuleLabel, row.PageLabel)
	}
	if row.Stats.Reviewed != 20 || row.Stats.Captured != 50 {
		t.Fatalf("day stats did not reach the row: %+v", row.Stats)
	}
	if row.Stats.ProgressPercent() != 100 {
		t.Fatalf("20 of 20 drawn reviewed = %d%%, want 100%%", row.Stats.ProgressPercent())
	}
	// A category the CEO has never set runs at the default and says so with no phantom setter.
	if row.SamplePercent != domain.DefaultSamplePercent || row.EffectiveFrom != "" || row.SetByName != "" {
		t.Fatalf("unset category reported %+v, want the default with no standing row", row)
	}
}

// TestTheClosetOutSettlesOnlyClosedDaysAndOnlyWaivableCategories pins the two narrowings that keep
// the closeout safe: it never touches TODAY (the share is still editable, and a video waived now
// could not be recruited back by a raise this afternoon), and it never offers a category whose
// approve must carry a number.
func TestTheClosetOutSettlesOnlyClosedDaysAndOnlyWaivableCategories(t *testing.T) {
	repo := newFakeRepo()
	repo.settled = 7
	svc := samplingService(t, repo, time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC))

	settled, err := svc.SettleUnsampledItems(context.Background(), samplingTenant, 0)
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	if settled != 7 {
		t.Fatalf("settled = %d, want 7", settled)
	}
	// 2026-08-26 10:00 UTC is 15:30 IST, so the business day started at 2026-08-26 00:00 IST and
	// nothing captured today may be settled.
	wantBefore := time.Date(2026, 8, 26, 0, 0, 0, 0, repo.settleParams.Before.Location())
	if !repo.settleParams.Before.Equal(wantBefore) {
		t.Fatalf("cutoff = %s, want the business-day start %s", repo.settleParams.Before, wantBefore)
	}
	got := map[string]bool{}
	for _, category := range repo.settleParams.WaivableCategories {
		got[category] = true
	}
	if got["feed_packing"] {
		t.Fatal("feed packing was offered to the closeout; its approve must carry the packed quantities")
	}
	if !got["feed_distribution"] || !got["weighing_proof"] {
		t.Fatalf("closeout allowlist missing a samplable category: %v", repo.settleParams.WaivableCategories)
	}
	if repo.settleParams.Limit <= 0 {
		t.Fatalf("closeout ran unbounded (limit %d)", repo.settleParams.Limit)
	}
}

func findSamplingRow(t *testing.T, overview domain.SamplingOverview, category string) domain.SamplingCategory {
	t.Helper()
	for _, row := range overview.Categories {
		if row.Category == category {
			return row
		}
	}
	t.Fatalf("category %s missing from the overview", category)
	return domain.SamplingCategory{}
}
