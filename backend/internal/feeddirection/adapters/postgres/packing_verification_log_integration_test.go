package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// The FEED VERIFICATION panel on /verify (maintainer decisions 2026-09-28): for one feed day, per
// park, pen and session, the planned total, the total the verifier entered on YESTERDAY's packing,
// the total she entered on TODAY's feeding, and fed minus packed. Every row is written through the
// production paths -- PersistIssue, CompletePacking / RecordPackingVerifiedQuantities /
// ApplyVerifiedPacking, CompleteDistribution / RecordDistributionVerifiedFeed /
// ApplyVerifiedDistribution -- never by hand.
//
// Adversarial on the blind-entry boundary at every stage of the chain:
//   - nothing done: the pen-session lists, no figure;
//   - packing APPROVED but feeding not done: the packed total is STILL withheld, because it is the
//     feeding verifier's answer;
//   - feeding submitted, verdict pending: still withheld;
//   - feeding approved: planned, packed, fed and the difference;
//
// plus a second session that never moves (its row stays figureless and out of the day totals), park
// scope on every side, and the day totals ranging over compared rows only.
//
// Name carries the guard's grain dimensions: ONE-TO-MANY (per-feed packing readings and ration cells
// summed to one pen-session), PARK SCOPE, STATUS BUCKETS, and NO PAGE BOUNDARY.
func TestPackingVerificationLogOneToManyParkScopeStatusBucketsNoPageBoundary(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupFeedDirectionDB(t, ctx)

	// Sheet for feed day 2026-07-22, shed A: session 1 concentrate 2.000 + hay 1.000 = 3.000;
	// session 2 the same.
	issuedAt := time.Date(2026, 7, 21, 9, 0, 0, 0, biztime.DefaultLocation())
	conc, hay := "2.000", "1.000"
	cell := func(session int32, label, key string, qty *string, seq int32) domain.StoredCell {
		return domain.StoredCell{
			ParkID: fdPark, ParkLabel: "CBE", ShedID: fdShedA, ShedLabel: "Castro",
			ShedTag: "Non-Pregnant", Breed: "Beetal", RationGroup: "Beetal/Sirohi",
			SessionNo: session, SessionLabel: map[int32]string{1: "Morning", 2: "Evening"}[session],
			HeadCount: 10, Workflow: domain.WorkflowNormal,
			FeedItemLabel: label, FeedItemKey: key, QuantityKg: qty,
			SessionTotalKg: "3.000", RowSeq: session - 1, ItemSeq: seq,
		}
	}
	if _, err := repo.PersistIssue(ctx, ports.PersistIssueCommand{
		TenantID: fdTenant, ParkID: fdPark, FeedDay: "2026-07-22", Workflow: domain.WorkflowNormal,
		IssuedAt: issuedAt, Fingerprint: "fp-feed-check",
		IdempotencyKey: "issue:feed-check:1", GeneratedBy: "test",
		Cells: []domain.StoredCell{
			cell(1, "Concentrate", domain.NormalizeConfigKey("Concentrate"), &conc, 0),
			cell(1, "Hay", domain.NormalizeConfigKey("Hay"), &hay, 1),
			cell(2, "Concentrate", domain.NormalizeConfigKey("Concentrate"), &conc, 0),
			cell(2, "Hay", domain.NormalizeConfigKey("Hay"), &hay, 1),
		},
	}); err != nil {
		t.Fatalf("PersistIssue: %v", err)
	}

	day := businessDay(2026, 7, 22)
	read := func(parkIDs []uuid.UUID) domain.PackingVerificationLog {
		t.Helper()
		got, err := repo.PackingVerificationLog(ctx, fdTenant, parkIDs, day)
		if err != nil {
			t.Fatalf("PackingVerificationLog: %v", err)
		}
		return got
	}
	session := func(log domain.PackingVerificationLog, n int) domain.FeedCheckRow {
		t.Helper()
		for _, r := range log.Rows {
			if r.SessionNo == n {
				return r
			}
		}
		t.Fatalf("no row for session %d in %+v", n, log.Rows)
		return domain.FeedCheckRow{}
	}
	assertNoFigures := func(r domain.FeedCheckRow, stage string) {
		t.Helper()
		if r.PlannedKg != "" || r.PackedKg != "" || r.FedKg != "" || r.DifferenceKg != "" {
			t.Fatalf("%s: figures leaked before the feeding verdict: %+v", stage, r)
		}
	}

	// 1. Nothing done: two pen-sessions listed, no figures.
	got := read(nil)
	if got.FeedDay != "2026-07-22" || got.PackingDay != "2026-07-21" || len(got.Rows) != 2 {
		t.Fatalf("days/rows: %+v", got)
	}
	s1 := session(got, 1)
	if s1.PackingStatus != domain.FeedCheckNotDone || s1.FeedingStatus != domain.FeedCheckNotDone || s1.SessionLabel != "Morning" || s1.ParkLabel != "CPT" {
		t.Fatalf("untouched row: %+v", s1)
	}
	assertNoFigures(s1, "nothing done")

	// 2. Yesterday's packing verified at 2.0 + 0.9 = 2.9 kg; feeding not done -> packed STILL hidden.
	packed, err := repo.CompletePacking(ctx, packingParams())
	if err != nil {
		t.Fatalf("CompletePacking: %v", err)
	}
	if err := repo.RecordPackingVerifiedQuantities(ctx, ports.RecordPackingVerifiedQuantitiesParams{
		TenantID: fdTenant, CompletionID: packed.CompletionID,
		Entries: []ports.PackingVerifiedQuantity{
			{FeedItemKey: domain.NormalizeConfigKey("Concentrate"), FeedItemLabel: "Concentrate", EnteredKg: 2.0},
			{FeedItemKey: domain.NormalizeConfigKey("Hay"), FeedItemLabel: "Hay", EnteredKg: 0.9},
		},
		RecordedBy: fdActor, IdempotencyKey: "rec-pack-1",
	}); err != nil {
		t.Fatalf("RecordPackingVerifiedQuantities: %v", err)
	}
	if _, err := repo.ApplyVerifiedPacking(ctx, ports.ApplyPackingParams{
		TenantID: fdTenant, CompletionID: packed.CompletionID, VerifiedBy: fdActor,
	}); err != nil {
		t.Fatalf("ApplyVerifiedPacking: %v", err)
	}
	s1 = session(read(nil), 1)
	if s1.PackingStatus != domain.FeedCheckVerified || s1.FeedingStatus != domain.FeedCheckNotDone {
		t.Fatalf("after packing approve: %+v", s1)
	}
	assertNoFigures(s1, "packing verified, feeding not done")

	// 3. Today's feeding submitted, verdict pending -> still hidden.
	dist, err := repo.CompleteDistribution(ctx, distributionParams())
	if err != nil {
		t.Fatalf("CompleteDistribution: %v", err)
	}
	s1 = session(read(nil), 1)
	if s1.FeedingStatus != domain.FeedCheckAwaitingVerification {
		t.Fatalf("after feeding submit: %+v", s1)
	}
	assertNoFigures(s1, "feeding awaiting verification")

	// 4. Feeding approved at 2.8 kg -> planned 3.0, packed 2.9, fed 2.8, fed - packed = -0.1.
	if err := repo.RecordDistributionVerifiedFeed(ctx, ports.RecordDistributionVerifiedFeedParams{
		TenantID: fdTenant, CompletionID: dist.CompletionID, EnteredKg: 2.8,
		RecordedBy: fdActor, IdempotencyKey: "rec-fed-1",
	}); err != nil {
		t.Fatalf("RecordDistributionVerifiedFeed: %v", err)
	}
	if _, err := repo.ApplyVerifiedDistribution(ctx, ports.ApplyDistributionParams{
		TenantID: fdTenant, CompletionID: dist.CompletionID, VerifiedBy: fdActor,
	}); err != nil {
		t.Fatalf("ApplyVerifiedDistribution: %v", err)
	}
	got = read(nil)
	s1 = session(got, 1)
	if s1.FeedingStatus != domain.FeedCheckVerified || s1.PlannedKg != "3.000" || s1.PackedKg != "2.900" || s1.FedKg != "2.800" || s1.DifferenceKg != "-0.100" {
		t.Fatalf("both verified: want planned 3.000 packed 2.900 fed 2.800 diff -0.100, got %+v", s1)
	}
	if s1.OperationalLocationDisplay == "" {
		t.Fatalf("pen display missing: %+v", s1)
	}
	assertNoFigures(session(got, 2), "untouched session 2")

	// Day totals range over the COMPARED row only; session 2 counts as a row, not in the kg.
	tot := got.Totals
	if tot.Rows != 2 || tot.Compared != 1 || tot.PlannedKg != "3.000" || tot.PackedKg != "2.900" || tot.FedKg != "2.800" || tot.DifferenceKg != "-0.100" {
		t.Fatalf("day totals: %+v", tot)
	}

	// Park scope binds every side.
	if own := read([]uuid.UUID{uuid.MustParse(fdPark)}); len(own.Rows) != 2 || own.Totals.FedKg != "2.800" {
		t.Fatalf("own-park scope: %+v", own)
	}
	if foreign := read([]uuid.UUID{uuid.New()}); len(foreign.Rows) != 0 {
		t.Fatalf("foreign-park scope: want no rows, got %+v", foreign.Rows)
	}
}
