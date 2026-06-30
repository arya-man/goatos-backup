package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	feedapp "github.com/vgoats/goatos/backend/internal/feed/app"
	feeddomain "github.com/vgoats/goatos/backend/internal/feed/domain"
	oblpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

const (
	feedTenant                = "00000000-0000-4000-8000-000000000001"
	feedShed                  = "55000000-0000-4000-8000-0000000000f1" // a shed location seeded by the test
	feedCountsPark            = "55000000-0000-4000-8000-000000000101"
	feedCountsSourceShed      = "55000000-0000-4000-8000-000000000102"
	feedCountsDestinationShed = "55000000-0000-4000-8000-000000000103"
)

// TestFeedDirectionCompletionFlow drives the feed SM-5 verify+complete path: accept completes a shed
// direction's obligation; reject leaves it open; double-submit is a no-op.
func TestFeedDirectionCompletionFlow(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	if _, err := pool.Exec(ctx,
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
		 VALUES ($1, $2, 'shed', 'SHED-F1', 'Feed test shed', 'active')`, feedShed, feedTenant); err != nil {
		t.Fatalf("seed shed: %v", err)
	}

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	feed := NewRepository(pool, 5*time.Second)

	// Feed protocol (category='feed') for the obligation FKs.
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: feedTenant, Code: "feed.direction.demo", Name: "Feed", Category: "feed_direction", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: feedTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleID, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: feedTenant, ProtocolVersionID: versionID, DoseCode: "ration", Sequence: 1,
		TriggerType: "calendar", Repeat: "every_n_days", CatchUp: "next_cycle",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}

	mkObl := func(key string, due time.Time) string {
		id, applied, err := obl.InsertObligation(ctx, obldomain.NewObligation{
			TenantID: feedTenant, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "shed", TargetID: feedShed, ScopeType: "shed", ScopeID: feedShed,
			DueAt: due, Status: "scheduled", IdempotencyKey: key, Sequence: 1,
		})
		if err != nil || !applied {
			t.Fatalf("obligation %s: applied=%v err=%v", key, applied, err)
		}
		return id
	}
	obA := mkObl("feed-a", time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC))
	obB := mkObl("feed-b", time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC))

	completion := feedapp.NewCompletionService(feedapp.NewService(feed), obl)
	head := int32(40)
	mk := func(ob, key string) feeddomain.NewDirection {
		return feeddomain.NewDirection{
			TenantID: feedTenant, ObligationID: ob, ShedID: feedShed,
			QuantityFed: "12.5", QuantityUnit: "kg", HeadCount: &head,
			FedAt: time.Date(2026, 6, 23, 7, 0, 0, 0, time.UTC), IdempotencyKey: key,
		}
	}

	// Accept obA: direction accepted + obligation completed.
	ar, err := completion.Accept(ctx, feedapp.AcceptInput{Direction: mk(obA, "rec-a")})
	if err != nil || !ar.Applied || !ar.Completed {
		t.Fatalf("accept obA: %+v err=%v", ar, err)
	}
	if got := scanText(t, ctx, pool, `SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, feedTenant, obA); got != "completed" {
		t.Fatalf("obA status: want completed, got %s", got)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM feed_direction_completions WHERE tenant_id=$1 AND obligation_id=$2 AND status='accepted'`, feedTenant, obA); got != 1 {
		t.Fatalf("want 1 accepted feed direction for obA, got %d", got)
	}

	// Double-submit obA: no-op.
	if ar2, err := completion.Accept(ctx, feedapp.AcceptInput{Direction: mk(obA, "rec-a")}); err != nil || ar2.Applied {
		t.Fatalf("double-submit should be a no-op: %+v err=%v", ar2, err)
	}

	// Reject obB: obligation stays open, direction rejected.
	rr, err := completion.Reject(ctx, feedapp.RejectInput{Direction: mk(obB, "rec-b"), Reason: "blurry feed video"})
	if err != nil || !rr.Applied {
		t.Fatalf("reject obB: %+v err=%v", rr, err)
	}
	if got := scanText(t, ctx, pool, `SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, feedTenant, obB); got == "completed" {
		t.Fatalf("obB must not be completed after reject")
	}
	if got := scanText(t, ctx, pool, `SELECT status FROM feed_direction_completions WHERE tenant_id=$1 AND obligation_id=$2`, feedTenant, obB); got != "rejected" {
		t.Fatalf("obB direction: want rejected, got %s", got)
	}

	// Verification queue is empty (one accepted, one rejected — none left recorded).
	queue, err := feed.ListRecordedDirections(ctx, feedTenant, 100)
	if err != nil || len(queue) != 0 {
		t.Fatalf("verification queue: want empty, got %d err=%v", len(queue), err)
	}
	// Shed history shows both.
	hist, err := feed.ListDirectionsByShed(ctx, feedTenant, feedShed, 100)
	if err != nil || len(hist) != 2 {
		t.Fatalf("shed history: want 2, got %d err=%v", len(hist), err)
	}
}

func TestReadinessConsumesSeededCountsProjectionAndBlocksPregnantDestinationShortage(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedFeedCountsScope(t, ctx, pool)

	countsRepo := countspg.NewRepository(pool, 5*time.Second)
	countsService := countsapp.NewService(countsRepo)
	feedService := feedapp.NewService(NewRepository(pool, 5*time.Second)).
		WithCountsReadiness(countsRepo).
		WithCountsProjectionExceptionLister(countsRepo)

	if _, replay, err := countsRepo.RecordBaseCountAnchor(ctx, feedReadinessBaseAnchor(feedCountsSourceShed, "feed-readiness-source-anchor", "feed-readiness-source-fp", 20)); err != nil || replay {
		t.Fatalf("record source anchor replay=%v err=%v", replay, err)
	}
	if _, replay, err := countsRepo.RecordBaseCountAnchor(ctx, feedReadinessBaseAnchor(feedCountsDestinationShed, "feed-readiness-dest-anchor", "feed-readiness-dest-fp", 5)); err != nil || replay {
		t.Fatalf("record destination anchor replay=%v err=%v", replay, err)
	}
	shiftID, replay, err := countsRepo.RecordShiftingEvent(ctx, feedReadinessPregnantShift())
	if err != nil || replay || shiftID == "" {
		t.Fatalf("record pregnant shift id=%q replay=%v err=%v", shiftID, replay, err)
	}

	outboxPayload := feedOutboxPayloadForAggregate(t, ctx, pool, countsdomain.EventShiftingEventRecorded, shiftID)
	event, err := eventbus.EventFromEnvelope(outboxPayload, eventbus.Event{})
	if err != nil {
		t.Fatalf("decode shifting outbox event: %v", err)
	}
	bus := eventbus.NewInProcessBus()
	countsapp.NewProjectionInputHandler(countsService, countsapp.WithProjectionInputHandlerGeneratedBy("feed-readiness-integration-test")).Register(bus)
	if err := bus.Publish(ctx, event); err != nil {
		t.Fatalf("publish shifting event to projection handler: %v", err)
	}

	readiness, err := feedService.Readiness(ctx, feedTenant)
	if err != nil {
		t.Fatalf("feed readiness: %v", err)
	}
	if readiness.Status != feeddomain.ReadinessBlocked || readiness.GenerationAllowed {
		t.Fatalf("feed readiness=%+v, want blocked/no-generation", readiness)
	}
	g2 := readinessGate(t, readiness.Gates, "G2")
	if g2.Status != feeddomain.ReadinessBlocked || g2.AllowsGenerate ||
		g2.BlockerReason != "Counts/Shifting has open projection exceptions; Feed generation remains blocked." {
		t.Fatalf("G2=%+v, want open-exception blocker", g2)
	}
	if csg := feedSubgate(t, readiness.CountsShiftingSubgates, "CSG4"); csg.Status != feeddomain.ReadinessReady {
		t.Fatalf("CSG4=%+v, want ready after dual-horizon recompute", csg)
	}
	if csg := feedSubgate(t, readiness.CountsShiftingSubgates, "CSG9"); csg.Status != feeddomain.ReadinessReady {
		t.Fatalf("CSG9=%+v, want ready projection API evidence", csg)
	}
	if csg := feedSubgate(t, readiness.CountsShiftingSubgates, "CSG10"); csg.Status != feeddomain.ReadinessPending {
		t.Fatalf("CSG10=%+v, want pending observability/source/E2E evidence", csg)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*)
FROM count_projection_snapshots
WHERE tenant_id=$1::uuid
  AND park_id=$2::uuid
  AND generated_by='feed-readiness-integration-test'`, feedTenant, feedCountsPark); got != 2 {
		t.Fatalf("event-driven projection snapshots=%d, want count_as_of and feed_target_date", got)
	}

	exceptions, err := feedService.ListCountsProjectionExceptions(ctx, feeddomain.CountsProjectionExceptionQuery{
		TenantID: feedTenant, Status: "open", ExceptionType: stringPtr("destination_shortage"),
		Severity: stringPtr("critical"), WorkState: stringPtr("owner_missing"), Limit: 10,
	})
	if err != nil {
		t.Fatalf("list feed counts projection exceptions: %v", err)
	}
	if len(exceptions.Items) != 1 {
		t.Fatalf("exceptions=%+v, want one destination_shortage", exceptions)
	}
	item := exceptions.Items[0]
	if item.ShedID == nil || *item.ShedID != feedCountsDestinationShed ||
		item.StageTag == nil || *item.StageTag != "pregnant" ||
		item.BlockerReason == "" {
		t.Fatalf("exception item=%+v, want pregnant destination shortage", item)
	}
}

func seedFeedCountsScope(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, 'park', 'CPT-FEED-READINESS', 'CPT Feed Readiness', 'active')
ON CONFLICT (location_id) DO NOTHING`, feedTenant, feedCountsPark); err != nil {
		t.Fatalf("seed counts park: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
VALUES
  ($3::uuid, $1::uuid, 'shed', 'CPT-FEED-SOURCE', 'CPT Feed Source Shed', $2::uuid, 'active'),
  ($4::uuid, $1::uuid, 'shed', 'CPT-FEED-PREGNANT-DEST', 'CPT Feed Pregnant Destination Shed', $2::uuid, 'active')
ON CONFLICT (location_id) DO NOTHING`,
		feedTenant, feedCountsPark, feedCountsSourceShed, feedCountsDestinationShed); err != nil {
		t.Fatalf("seed counts sheds: %v", err)
	}
}

func feedReadinessBaseAnchor(shedID, key, fingerprint string, count int32) countsdomain.BaseCountAnchor {
	return countsdomain.BaseCountAnchor{
		TenantID: feedTenant, ParkID: feedCountsPark, ShedID: shedID,
		BreedKey: "beetal", BreedLabel: "Beetal", CountedAt: time.Date(2026, 6, 30, 6, 0, 0, 0, time.UTC),
		HeadCount: count, SourceSystem: "physical_base_count", SourceRef: "feed-readiness-base-count:" + shedID,
		SourceHash: "feed-readiness-base-hash:" + shedID, DiscrepancyState: "not_checked",
		IdempotencyKey: key, RequestFingerprint: fingerprint,
	}
}

func feedReadinessPregnantShift() countsdomain.ShiftingEvent {
	stage := "pregnant"
	return countsdomain.ShiftingEvent{
		TenantID: feedTenant, LogicalShiftingEventKey: "feed-readiness-pregnant-shift",
		Priority: "high", Category: "pregnancy", SourceParkID: stringPtr(feedCountsPark),
		SourceShedID: stringPtr(feedCountsSourceShed), DestinationParkID: feedCountsPark,
		DestinationShedID: feedCountsDestinationShed,
		RaisedAt:          time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC), EffectiveAt: time.Date(2026, 6, 30, 13, 0, 0, 0, time.UTC),
		AuthorizationState: "authorized", VerificationState: "verified", EventStatus: "authorized",
		SourceSystem: "feed_shiftings_docx", SourceRef: "feed-readiness-shift-report",
		PayloadHash: "feed-readiness-shift-payload", IdempotencyKey: "feed-readiness-shift-idem",
		RequestFingerprint: "feed-readiness-shift-fp",
		Impacts: []countsdomain.ShiftingEventImpact{{
			GrainKey: "beetal:pregnant", BreedKey: "beetal", BreedLabel: "Beetal", StageTag: &stage,
			HeadCount: 3, PregnantCount: 3, RiskFlagsJSON: []byte(`{"pregnant":true}`),
			RationContextResolutionState: "blocked", BlockerReason: stringPtr("destination shed ration context unresolved"),
		}},
	}
}

func readinessGate(t *testing.T, gates []feeddomain.ReadinessGate, id string) feeddomain.ReadinessGate {
	t.Helper()
	for _, gate := range gates {
		if gate.ID == id {
			return gate
		}
	}
	t.Fatalf("missing readiness gate %s in %+v", id, gates)
	return feeddomain.ReadinessGate{}
}

func feedSubgate(t *testing.T, subgates []feeddomain.CountsShiftingSubgate, id string) feeddomain.CountsShiftingSubgate {
	t.Helper()
	for _, subgate := range subgates {
		if subgate.ID == id {
			return subgate
		}
	}
	t.Fatalf("missing feed subgate %s in %+v", id, subgates)
	return feeddomain.CountsShiftingSubgate{}
}

func stringPtr(s string) *string { return &s }

func feedOutboxPayloadForAggregate(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventType, aggregateID string) []byte {
	t.Helper()
	var payload []byte
	if err := pool.QueryRow(ctx, `
SELECT payload
FROM outbox_messages
WHERE tenant_id=$1::uuid
  AND event_type=$2
  AND aggregate_id=$3::uuid`, feedTenant, eventType, aggregateID).Scan(&payload); err != nil {
		t.Fatalf("load outbox payload: %v", err)
	}
	return payload
}

func scanText(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) string {
	t.Helper()
	var s string
	if err := pool.QueryRow(ctx, sql, args...).Scan(&s); err != nil {
		t.Fatalf("scan %q: %v", sql, err)
	}
	return s
}

func countRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", sql, err)
	}
	return n
}
