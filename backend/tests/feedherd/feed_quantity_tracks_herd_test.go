// Package feedherd proves the herd -> feed chain end to end: does a BIRTH, a DEATH, and an
// authorized-but-unexecuted SHIFTING actually change the kilograms on the next issued feed sheet?
//
// The question this answers is operational, not theoretical. The feed sheet is GENERATED on D-1 by
// backend/cmd/feed-direction-issue; if the generator's head counts did not track the live herd, a
// shed would keep being fed for animals that died or left, and newborn kids would be fed nothing.
//
// It is wired exactly as cmd/feed-direction-issue wires production:
//
//	feeddirectionapp.NewService(feeddirectionpg.Repository, feeddirectioncounts.NewReader(countsapp.Service))
//
// so the number under assertion travels the real path: canonical goats/shifting_events -> the
// counts feed projection (ProjectedShedCountsForFeed) -> the feeddirection generator -> the frozen
// feed_direction_issue_rows an operator actually packs from.
//
// Per the repository E2E rule, the fixture inserts only INPUT facts (tenant, locations, goats,
// authored feed configuration). Every head count and every kilogram under assertion is produced by
// the production services, and the three herd mutations are driven through their production write
// paths (identity CreateAdminGoat / CriticalDeathExit, counts RecordShiftingEvent + approval
// DecideApprovalRequest), never by seeding the derived state.
package feedherd

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	feeddirectioncounts "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/counts"
	feeddirectionpg "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/postgres"
	feeddirectionapp "github.com/vgoats/goatos/backend/internal/feeddirection/app"
	feeddomain "github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	identityapp "github.com/vgoats/goatos/backend/internal/identity/app"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const (
	// Baseline tenant + custodian party, already present from the migrations.
	fhTenant = "00000000-0000-4000-8000-000000000001"
	fhParty  = "00000000-0000-4000-8000-000000001001"

	// A park of this test's own, so the baseline sheds cannot leak grains into the park scope the
	// generator walks.
	fhPark  = "ae000000-0000-4000-8000-000000003001"
	fhShedA = "ae000000-0000-4000-8000-000000004001"
	fhShedB = "ae000000-0000-4000-8000-000000004002"

	fhOperator = "00000000-0000-4000-8000-000000009002"
	fhApprover = "00000000-0000-4000-8000-000000009001"

	// One grain only, so a kilogram change can be attributed to a head-count change and nothing
	// else. K1 is both the animals' management_stage and (therefore) the shed tag the ration rate
	// is authored against.
	//
	// K1 is authored applies_to='kid', and the generator branches the ration course on THAT, not on
	// goats.age_band (strategy.go STEP 2): a kid tag of any breed resolves to the single 'Kid'
	// group and never consults the breed map. So the rate below is authored against 'Kid', and the
	// breed->group row exists only to show it is deliberately not the path in play here.
	fhBreed     = "Beetal"
	fhStage     = "K1"
	fhRationGrp = "Kid"

	// 200 g/head/day of Concentrate, one session at the full split, shed factor 1.0. Chosen so the
	// expected kilograms are exact decimals and a wrong head count cannot round into looking right.
	fhGramsPerHead = 200

	fhBaselineShedA = 20
	fhBaselineShedB = 10
	fhBirths        = 3
	fhDeaths        = 2
	fhShifted       = 5
)

// The two dispatch days. Feed for day D is produced on D-1, so an issue run on asOfDay1 writes the
// sheet for feedDay1. Every instant is an India business instant; nothing here is built in UTC.
// The dates sit in the recent past on purpose: identity's birth validation derives a kid/adult
// stage from dob against the REAL wall clock (deriveAgeBasedStage uses time.Now()), so a
// future-dated birth would not be a newborn to it.
var (
	asOfDay1 = time.Date(2026, 7, 20, 7, 0, 0, 0, biztime.DefaultLocation())
	asOfDay2 = time.Date(2026, 7, 21, 7, 0, 0, 0, biztime.DefaultLocation())
	feedDay1 = "2026-07-21"
	feedDay2 = "2026-07-22"
	birthDay = "2026-07-20"
)

// TestFeedQuantitiesTrackBirthsDeathsAndAuthorizedShiftings is the whole story.
//
//	day 1  issue  -> shed A 20 head / 4.000 kg, shed B 10 head / 2.000 kg
//	       3 births into shed A   (identity CreateAdminGoat, origin_type=birth)
//	       2 deaths in shed A     (identity CriticalDeathExit -- the guardrail path)
//	       5 animals A -> B moved (counts RecordShiftingEvent + park-head approval; NOT executed)
//	day 2  issue  -> shed A 16 head / 3.200 kg, shed B 15 head / 3.000 kg
//
// Shed B's +5 is the load-bearing part: the animals have NOT physically moved and their canonical
// goats.shed_id still says shed A. They count toward B's feed the moment the park head authorizes
// the movement (maintainer decision 2026-07-27, zero lead time), because the feed sheet has to be
// packed for where the animals WILL be standing.
func TestFeedQuantitiesTrackBirthsDeathsAndAuthorizedShiftings(t *testing.T) {
	ctx := context.Background()
	pgtest.SkipIfNoDocker(t)
	pool := pgtest.StartPostgres(t, ctx)

	seedScope(t, ctx, pool)
	seedFeedConfig(t, ctx, pool)
	shedAGoats := seedHerd(t, ctx, pool, fhShedA, 1, fhBaselineShedA)
	seedHerd(t, ctx, pool, fhShedB, 100, fhBaselineShedB)

	const timeout = 15 * time.Second
	countsRepo := countspg.NewRepository(pool, timeout).
		WithIdentityTxWriter(identitypg.NewRepository(pool, timeout))
	countsSvc := countsapp.NewService(countsRepo)
	identitySvc := identityapp.NewService(identitypg.NewRepository(pool, timeout))
	feedRepo := feeddirectionpg.NewRepository(pool, timeout)

	// The exact wiring of backend/cmd/feed-direction-issue.
	newFeedService := func(asOf time.Time) *feeddirectionapp.Service {
		return feeddirectionapp.NewService(feedRepo, feeddirectioncounts.NewReader(countsSvc)).
			WithIssueStore(feedRepo).
			WithScheduleReader(feedRepo).
			WithGeneratedBy("feed-herd-e2e").
			WithClock(func() time.Time { return asOf })
	}

	// ---------------------------------------------------------------- day 1
	issue(t, ctx, newFeedService(asOfDay1), asOfDay1)
	base := readIssuedSheet(t, ctx, pool, feedDay1)
	assertCell(t, "day1 shed A", base[fhShedA], fhBaselineShedA, kgFor(fhBaselineShedA))
	assertCell(t, "day1 shed B", base[fhShedB], fhBaselineShedB, kgFor(fhBaselineShedB))

	// -------------------------------------------------- herd mutations (real write paths)
	// The dam is one of shed A's own animals, so the births are attributed to a real canonical
	// mother the way identity requires.
	recordBirths(t, ctx, identitySvc, fhBirths, shedAGoats[len(shedAGoats)-1])
	recordDeaths(t, ctx, identitySvc, pool, shedAGoats[:fhDeaths])
	authorizeShifting(t, ctx, countsRepo, shedAGoats[fhDeaths:fhDeaths+fhShifted])

	// ---------------------------------------------------------------- day 2
	issue(t, ctx, newFeedService(asOfDay2), asOfDay2)
	after := readIssuedSheet(t, ctx, pool, feedDay2)

	wantA := fhBaselineShedA + fhBirths - fhDeaths - fhShifted // 16
	wantB := fhBaselineShedB + fhShifted                       // 15
	assertCell(t, "day2 shed A", after[fhShedA], wantA, kgFor(wantA))
	assertCell(t, "day2 shed B", after[fhShedB], wantB, kgFor(wantB))

	// The movement must NOT have relocated anything yet: it is authorized, not executed. If the
	// animals had already moved, shed B's +5 would be a live-herd fact and would prove nothing about
	// the pending-movement projection.
	var stillInA int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM goats
WHERE tenant_id = $1::uuid AND shed_id = $2::uuid AND lifecycle_status = 'alive'`,
		fhTenant, fhShedA).Scan(&stillInA); err != nil {
		t.Fatalf("count live animals in shed A: %v", err)
	}
	if want := fhBaselineShedA + fhBirths - fhDeaths; stillInA != want {
		t.Fatalf("canonical goats in shed A = %d, want %d -- the authorized movement must not have "+
			"relocated anyone yet, or shed B's projected +%d proves nothing", stillInA, want, fhShifted)
	}

	t.Logf("shed A: %d head / %s kg  ->  %d head / %s kg", fhBaselineShedA, kgFor(fhBaselineShedA), wantA, kgFor(wantA))
	t.Logf("shed B: %d head / %s kg  ->  %d head / %s kg", fhBaselineShedB, kgFor(fhBaselineShedB), wantB, kgFor(wantB))
}

// kgFor is the expected pack weight, computed independently of the generator: head x g/head / 1000,
// rendered at the column's 3-decimal scale.
func kgFor(head int) string {
	return fmt.Sprintf("%.3f", float64(head*fhGramsPerHead)/1000.0)
}

// ---------------------------------------------------------------------------
// Production calls
// ---------------------------------------------------------------------------

func issue(t *testing.T, ctx context.Context, svc *feeddirectionapp.Service, asOf time.Time) {
	t.Helper()
	report, err := svc.IssueDirection(ctx, feeddirectionapp.IssueRequest{
		TenantID: fhTenant, ParkID: fhPark, Workflow: feeddomain.WorkflowNormal, AsOf: asOf,
	})
	if err != nil {
		t.Fatalf("IssueDirection at %s: %v", asOf.Format(time.RFC3339), err)
	}
	if report.Outcome == "" {
		t.Fatalf("IssueDirection at %s returned no outcome", asOf.Format(time.RFC3339))
	}
}

// recordBirths creates newborns through identity's guarded admin-goat create with origin_type
// pinned to "birth" -- the same command the operator birth route (/app/counts/birth-events) calls.
func recordBirths(t *testing.T, ctx context.Context, identity *identityapp.Service, n int, damGoatID string) {
	t.Helper()
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("feedherd-birth-%d", i)
		body, err := json.Marshal(map[string]any{
			"animal_identifier_1": fmt.Sprintf("FHB%012d", i),
			"species":             "goat",
			"breed":               fhBreed,
			"sex":                 "female",
			"park_id":             fhPark,
			"shed_id":             fhShedA,
			"management_stage":    fhStage,
			"dob":                 birthDay,
			"origin_type":         "birth",
			"entry_date":          birthDay,
			"dam_id":              damGoatID,
			"litter_size":         n,
			"evidence_refs": []map[string]string{
				{"evidence_type": "source_record", "evidence_id": key},
			},
		})
		if err != nil {
			t.Fatalf("marshal birth %d: %v", i, err)
		}
		if _, err := identity.CreateAdminGoat(ctx, identityapp.CreateAdminGoatInput{
			TenantID: fhTenant, ActorID: fhOperator, IdempotencyKey: key,
			TraceID: "trace-" + key, RawBody: body,
		}); err != nil {
			t.Fatalf("record birth %d through identity CreateAdminGoat: %v", i, err)
		}
	}
}

// recordDeaths exits animals through the CRITICAL-DEATH GUARDRAIL command, not the ordinary exit
// primitive and not a bulk status write -- the only sanctioned death path
// (docs/features/critical-animal-action-guardrails.md).
func recordDeaths(t *testing.T, ctx context.Context, identity *identityapp.Service, pool *pgxpool.Pool, goatIDs []string) {
	t.Helper()
	for i, goatID := range goatIDs {
		key := fmt.Sprintf("feedherd-death-%d", i)
		body, err := json.Marshal(map[string]any{
			"lifecycle_status": "dead",
			"exit_reason":      "died",
			"reason":           "feed/herd chain E2E: death through the critical-death guardrail",
			"occurred_at":      asOfDay1.Add(4 * time.Hour),
			"evidence_refs": []map[string]string{
				{"evidence_type": "source_record", "evidence_id": key},
			},
			"row_version": goatRowVersion(t, ctx, pool, goatID),
		})
		if err != nil {
			t.Fatalf("marshal death %d: %v", i, err)
		}
		if _, err := identity.CriticalDeathExit(ctx, identityapp.ExitGoatInput{
			TenantID: fhTenant, ActorID: fhOperator, IdempotencyKey: key,
			TraceID: "trace-" + key, GoatID: goatID, RawBody: body,
		}); err != nil {
			t.Fatalf("record death %d (%s) through CriticalDeathExit: %v", i, goatID, err)
		}
	}
}

// authorizeShifting raises a movement and has the park head APPROVE it. Approval is authorization
// only: it does not relocate the animals (maintainer decision 2026-07-19). That is exactly the
// state the feed projection must already account for.
func authorizeShifting(t *testing.T, ctx context.Context, repo *countspg.Repository, goatIDs []string) {
	t.Helper()
	const key = "feedherd-shift-1"
	authorizedAt := asOfDay2.Add(-1 * time.Hour)

	stage := fhStage
	sex := "female"
	eventID, _, err := repo.RecordShiftingEvent(ctx, countsdomain.ShiftingEvent{
		TenantID:                fhTenant,
		LogicalShiftingEventKey: key,
		Priority:                "low",
		Category:                "growth",
		SourceParkID:            strPtr(fhPark),
		SourceShedID:            strPtr(fhShedA),
		DestinationParkID:       fhPark,
		DestinationShedID:       fhShedB,
		RaisedAt:                authorizedAt,
		EffectiveAt:             authorizedAt,
		AuthorizationState:      "pending",
		VerificationState:       "unverified",
		EventStatus:             "pending",
		SourceSystem:            "goatos_canonical",
		SourceRef:               "e2e:" + key,
		PayloadHash:             "hash-" + key,
		IdempotencyKey:          "idem-" + key,
		RequestFingerprint:      "fp-" + key,
		Impacts: []countsdomain.ShiftingEventImpact{{
			GrainKey:      fhShedB + ":beetal:k1",
			BreedKey:      "beetal",
			BreedLabel:    fhBreed,
			StageTag:      &stage,
			Sex:           &sex,
			HeadCount:     int32(len(goatIDs)),
			RiskFlagsJSON: []byte("{}"),
		}},
	})
	if err != nil {
		t.Fatalf("RecordShiftingEvent: %v", err)
	}

	payload, err := json.Marshal(map[string]any{
		"shifting_event_id":   eventID,
		"destination_park_id": fhPark,
		"destination_shed_id": fhShedB,
		"goat_ids":            goatIDs,
	})
	if err != nil {
		t.Fatalf("marshal shifting approval payload: %v", err)
	}
	req, _, err := repo.CreateApprovalRequest(ctx, countsdomain.ApprovalRequestSubmission{
		TenantID:           fhTenant,
		RequestType:        countsdomain.ApprovalRequestTypeShifting,
		Payload:            payload,
		ShiftingEventID:    &eventID,
		RaisedByUserID:     fhOperator,
		RaisedAt:           authorizedAt,
		IdempotencyKey:     "submit-" + key,
		RequestFingerprint: "submit-fp-" + key,
	})
	if err != nil {
		t.Fatalf("CreateApprovalRequest: %v", err)
	}

	if _, _, err := repo.DecideApprovalRequest(ctx, countsdomain.ApprovalDecision{
		TenantID:           fhTenant,
		ApprovalRequestID:  req.ApprovalRequestID,
		Status:             countsdomain.ApprovalStatusApproved,
		DecidedByUserID:    fhApprover,
		DecidedAt:          authorizedAt,
		IdempotencyKey:     "decide-" + key,
		RequestFingerprint: "decide-fp-" + key,
		Effect: &countsdomain.ApprovalEffect{Shifting: &countsdomain.ShiftingApprovalEffect{
			ShiftingEventID:   eventID,
			DestinationParkID: fhPark,
			DestinationShedID: fhShedB,
			GoatIDs:           goatIDs,
		}},
	}); err != nil {
		t.Fatalf("DecideApprovalRequest (park head authorization): %v", err)
	}
}

// ---------------------------------------------------------------------------
// Readback
// ---------------------------------------------------------------------------

type sheetCell struct {
	found     bool
	headCount int
	quantity  string
	blocked   string
}

// readIssuedSheet reads the FROZEN rows an operator packs from -- the generator's real output,
// not a recomputation.
func readIssuedSheet(t *testing.T, ctx context.Context, pool *pgxpool.Pool, feedDay string) map[string]sheetCell {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT r.shed_id::text, r.head_count, COALESCE(r.quantity_kg::text, ''),
       COALESCE(r.blocked_reason_code, '') || CASE WHEN r.blocked_reason_detail IS NULL THEN '' ELSE ': ' || r.blocked_reason_detail END
FROM feed_direction_issue_rows r
JOIN feed_direction_issues i ON i.feed_direction_issue_id = r.feed_direction_issue_id
WHERE i.tenant_id = $1::uuid
  AND i.park_id = $2::uuid
  AND i.feed_day = $3::date
  AND i.workflow = 'normal'
  AND i.state IN ('issued', 'amended', 'locked')`,
		fhTenant, fhPark, feedDay)
	if err != nil {
		t.Fatalf("read issued sheet for %s: %v", feedDay, err)
	}
	defer rows.Close()

	out := map[string]sheetCell{}
	for rows.Next() {
		var shedID, quantity, blocked string
		var head int
		if err := rows.Scan(&shedID, &head, &quantity, &blocked); err != nil {
			t.Fatalf("scan issued row: %v", err)
		}
		out[shedID] = sheetCell{found: true, headCount: head, quantity: quantity, blocked: blocked}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate issued rows: %v", err)
	}
	if len(out) == 0 {
		t.Fatalf("no issued feed rows at all for %s -- the sheet was never generated", feedDay)
	}
	return out
}

func assertCell(t *testing.T, label string, got sheetCell, wantHead int, wantKg string) {
	t.Helper()
	if !got.found {
		t.Fatalf("%s: no feed row was issued for this shed", label)
	}
	if got.blocked != "" {
		t.Fatalf("%s: cell is BLOCKED (%s) -- the ration could not be resolved, so this proves nothing about quantities", label, got.blocked)
	}
	if got.headCount != wantHead {
		t.Fatalf("%s: head_count = %d, want %d", label, got.headCount, wantHead)
	}
	if got.quantity != wantKg {
		t.Fatalf("%s: quantity_kg = %s, want %s (%d head x %d g)", label, got.quantity, wantKg, wantHead, fhGramsPerHead)
	}
}

func goatRowVersion(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID string) int64 {
	t.Helper()
	var v int64
	if err := pool.QueryRow(ctx, `SELECT row_version FROM goats WHERE goat_id = $1::uuid`, goatID).Scan(&v); err != nil {
		t.Fatalf("read row_version for %s: %v", goatID, err)
	}
	return v
}

// ---------------------------------------------------------------------------
// Input facts
// ---------------------------------------------------------------------------

func seedScope(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	exec := execer(t, ctx, pool)
	exec(`INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Mesha Test', 'active')
ON CONFLICT (tenant_id) DO NOTHING`, fhTenant)
	exec(`INSERT INTO parties (party_id, party_type, display_name, status)
VALUES ($1::uuid, 'org', 'Feed Herd E2E Custodian', 'active')
ON CONFLICT (party_id) DO NOTHING`, fhParty)
	exec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, 'park', 'FHP', 'Feed Herd Park', 'active')
ON CONFLICT (location_id) DO NOTHING`, fhTenant, fhPark)
	exec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status, parent_location_id, display_order)
VALUES ($3::uuid, $1::uuid, 'shed', 'FH-A', 'Feed Herd Shed A', 'active', $2::uuid, 1),
       ($4::uuid, $1::uuid, 'shed', 'FH-B', 'Feed Herd Shed B', 'active', $2::uuid, 2)
ON CONFLICT (location_id) DO NOTHING`, fhTenant, fhPark, fhShedA, fhShedB)
	// identity rejects a management_stage that is not an ACTIVE animal stage, so the birth path
	// needs the stage in the vocabulary, not just on the seeded rows.
	exec(`INSERT INTO animal_stage_lookup (tenant_id, stage_code, name, sort_order, status)
VALUES ($1::uuid, $2, $2, 1, 'active')
ON CONFLICT DO NOTHING`, fhTenant, fhStage)
}

// seedFeedConfig authors the smallest complete ration that resolves: one tag, one group, one item,
// one session at the full split, one rate, and the dispatch clock the lifecycle reads.
func seedFeedConfig(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	exec := execer(t, ctx, pool)
	exec(`INSERT INTO feed_shed_tags (tenant_id, shed_tag_label, applies_to, display_order, status)
VALUES ($1::uuid, $2, 'kid', 1, 'active')`, fhTenant, fhStage)
	exec(`INSERT INTO feed_ration_groups (tenant_id, breed_label, ration_group_label)
VALUES ($1::uuid, $2, 'Beetal/Sirohi')`, fhTenant, fhBreed)
	exec(`INSERT INTO feed_item_catalog (tenant_id, feed_item_label, display_order, status)
VALUES ($1::uuid, 'Concentrate', 1, 'active')`, fhTenant)
	exec(`INSERT INTO feed_session_templates (tenant_id, park_id, session_no, session_label, split_fraction, display_order, status)
VALUES ($1::uuid, $2::uuid, 1, 'Morning', 1.0, 1, 'active')`, fhTenant, fhPark)
	// valid_from is explicit here for the same reason as the dispatch clock: it defaults to
	// CURRENT_DATE, and the session's item list must already be in force on the pinned feed day.
	exec(`INSERT INTO feed_session_template_items (tenant_id, park_id, session_no, slot_no, feed_item_label, status, valid_from)
VALUES ($1::uuid, $2::uuid, 1, 1, 'Concentrate', 'active', DATE '2026-01-01')`, fhTenant, fhPark)
	exec(`INSERT INTO feed_ration_rates (tenant_id, park_id, ration_group_label, shed_tag_label, feed_item_label, grams_per_head, valid_from)
VALUES ($1::uuid, $2::uuid, $3, $4, 'Concentrate', $5, DATE '2026-01-01')`,
		fhTenant, fhPark, fhRationGrp, fhStage, fhGramsPerHead)
	// valid_from is explicit: it defaults to CURRENT_DATE, and this scenario runs on a pinned
	// business date in the past, so an implicit default would leave the clock not yet in force.
	exec(`INSERT INTO feed_schedule_config (tenant_id, park_id, workflow, direction_time, correction_time, transport_time, valid_from)
VALUES ($1::uuid, $2::uuid, 'normal', '07:00', '14:00', '15:45', DATE '2026-01-01')`, fhTenant, fhPark)
}

// seedHerd inserts live animals as INPUT facts and returns their ids in insertion order.
func seedHerd(t *testing.T, ctx context.Context, pool *pgxpool.Pool, shedID string, offset, n int) []string {
	t.Helper()
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		seq := offset + i
		goatID := fmt.Sprintf("ae000000-0000-4000-8000-0000000%05d", 80000+seq)
		if _, err := pool.Exec(ctx, `
INSERT INTO goats (
  goat_id, tenant_id, display_id, species, breed, sex, lifecycle_status,
  age_band, custodian_party_id, park_id, shed_id, management_stage
) VALUES (
  $1::uuid, $2::uuid, $3, 'goat', $4, 'female', 'alive', 'kid',
  $5::uuid, $6::uuid, $7::uuid, $8
)`, goatID, fhTenant, fmt.Sprintf("G-8%05d", seq), fhBreed, fhParty, fhPark, shedID, fhStage); err != nil {
			t.Fatalf("seed goat %s: %v", goatID, err)
		}
		ids = append(ids, goatID)
	}
	return ids
}

func execer(t *testing.T, ctx context.Context, pool *pgxpool.Pool) func(string, ...any) {
	return func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v\nsql: %s", err, sql)
		}
	}
}

func strPtr(s string) *string { return &s }
