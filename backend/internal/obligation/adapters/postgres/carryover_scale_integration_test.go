package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/obligation/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgconv"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	vaccpg "github.com/vgoats/goatos/backend/internal/vaccination/adapters/postgres"
)

// ---------------------------------------------------------------------------------------------
// Carry-over at scale: ~500 goats x 3 vaccines through the chunked, set-based path.
// ---------------------------------------------------------------------------------------------

const (
	scaleGoats        = 500
	scaleInvalidGoats = 50
	scaleTemplateGoat = "10000000-0000-4000-8000-00000000c5a1"
)

type scaleRule struct {
	code, doseCode string
	offset, minGap int32
}

func scaleRules() []scaleRule {
	return []scaleRule{
		{code: "ET_TT", doseCode: "et_tt_primary", offset: 28, minGap: 180},
		{code: "PPR", doseCode: "ppr_primary", offset: 112, minGap: 1095},
		{code: "GOAT_POX", doseCode: "goat_pox_primary", offset: 140, minGap: 365},
	}
}

func createScaleVersion(t *testing.T, ctx context.Context, proto *protopg.Repository, protoID string, version int32) (string, map[string]string) {
	t.Helper()
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: version, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create version %d: %v", version, err)
	}
	rules := map[string]string{}
	for _, r := range scaleRules() {
		ruleID, err := proto.CreateRule(ctx, protodomain.NewRule{
			TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: r.doseCode, Sequence: 1,
			TriggerType: "birth_age", OffsetDays: r.offset, DueWindowDays: 7, MinGapDays: r.minGap,
			Repeat: "yearly", CatchUp: "immediate",
			EligibilityJSON: []byte(fmt.Sprintf(`{"vaccine":{"code":%q},"eligibility":{"animal_stage":"adult"}}`, r.code)),
			ProofPolicy:     []byte(`{"mode":"per_goat"}`),
		})
		if err != nil {
			t.Fatalf("create rule %s: %v", r.doseCode, err)
		}
		rules[r.doseCode] = ruleID
	}
	return versionID, rules
}

func scaleGoatID(i int) string {
	return fmt.Sprintf("5ca1e000-0000-4000-8000-%012d", i)
}

// cloneObligationsToGoats copies each template obligation onto every goat in one INSERT ... SELECT,
// so the fixture does not spend 1,500 validated round trips building itself.
func cloneObligationsToGoats(t *testing.T, ctx context.Context, pool *pgxpool.Pool, templateIDs, goatIDs []string) {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT column_name FROM information_schema.columns
WHERE table_schema = current_schema() AND table_name = 'obligation_instances' AND is_generated = 'NEVER'
ORDER BY ordinal_position`)
	if err != nil {
		t.Fatalf("read obligation columns: %v", err)
	}
	var cols, exprs []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		cols = append(cols, c)
		switch c {
		case "obligation_id":
			exprs = append(exprs, "gen_random_uuid()")
		case "target_id":
			exprs = append(exprs, "g.goat_id")
		case "idempotency_key":
			exprs = append(exprs, "t.idempotency_key || '-' || g.goat_id::text")
		case "created_at", "updated_at":
			exprs = append(exprs, "now()")
		default:
			exprs = append(exprs, "t."+c)
		}
	}
	rows.Close()
	if _, err := pool.Exec(ctx, `
INSERT INTO obligation_instances (`+strings.Join(cols, ", ")+`)
SELECT `+strings.Join(exprs, ", ")+`
FROM obligation_instances t
CROSS JOIN unnest($2::uuid[]) AS g(goat_id)
WHERE t.tenant_id = $1::uuid AND t.obligation_id = ANY($3::uuid[])`, tenantID, goatIDs, templateIDs); err != nil {
		t.Fatalf("clone obligations: %v", err)
	}
}

type scaleRow struct {
	id, version, key, goat string
	due                    time.Time
}

func scaleRowsFor(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatIDs []string) map[string]scaleRow {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT obligation_id::text, protocol_version_id::text, idempotency_key, target_id::text, due_at
FROM obligation_instances
WHERE tenant_id = $1::uuid AND target_id = ANY($2::uuid[]) AND status <> 'canceled'`, tenantID, goatIDs)
	if err != nil {
		t.Fatalf("read scale rows: %v", err)
	}
	defer rows.Close()
	out := map[string]scaleRow{}
	for rows.Next() {
		var r scaleRow
		if err := rows.Scan(&r.id, &r.version, &r.key, &r.goat, &r.due); err != nil {
			t.Fatalf("scan scale row: %v", err)
		}
		out[r.id] = r
	}
	return out
}

func TestCarryOverScaleFiveHundredGoatsThreeVaccinesIsSetBasedAndCorrect(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 30*time.Second)
	proto := protopg.NewRepository(pool, 5*time.Second)
	seedCapacityGoatInPark(t, ctx, pool, scaleTemplateGoat, cbePark)
	goatIDs := make([]string, scaleGoats)
	for i := range goatIDs {
		goatIDs[i] = scaleGoatID(i)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, current_location_id, park_id, dob)
SELECT g, $1, 'alive', 'goat', $2, 'female', $3, $3, DATE '2020-01-01'
FROM unnest($4::uuid[]) AS g
ON CONFLICT (goat_id) DO NOTHING`, tenantID, meshaParty, cbePark, append([]string{scaleTemplateGoat}, goatIDs...)); err != nil {
		t.Fatalf("seed goats: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE goats SET dob = DATE '2020-01-01' WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`, tenantID, scaleTemplateGoat); err != nil {
		t.Fatalf("template DOB: %v", err)
	}

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.carryover.scale", Name: "CarryOverScale", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	v1, v1Rules := createScaleVersion(t, ctx, proto, protoID, 1)
	if err := proto.PublishVersion(ctx, tenantID, v1, nil); err != nil {
		t.Fatalf("publish v1: %v", err)
	}
	due := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	var templates []string
	for i, r := range scaleRules() {
		templates = append(templates, insertObligationForRule(t, ctx, repo, v1, v1Rules[r.doseCode], scaleTemplateGoat, "scale-"+r.doseCode, due.AddDate(0, 0, i)))
	}
	cloneObligationsToGoats(t, ctx, pool, templates, goatIDs)
	// The first 50 goats turn out to be born on 2026-09-01: every row is below its age floor.
	invalid := map[string]bool{}
	for _, id := range goatIDs[:scaleInvalidGoats] {
		invalid[id] = true
	}
	if _, err := pool.Exec(ctx, `UPDATE goats SET dob = DATE '2026-09-01' WHERE tenant_id=$1::uuid AND goat_id = ANY($2::uuid[])`, tenantID, goatIDs[:scaleInvalidGoats]); err != nil {
		t.Fatalf("under-age DOB: %v", err)
	}
	before := scaleRowsFor(t, ctx, pool, goatIDs)
	if len(before) != scaleGoats*len(scaleRules()) {
		t.Fatalf("seeded %d rows, want %d", len(before), scaleGoats*len(scaleRules()))
	}

	v2, _ := createScaleVersion(t, ctx, proto, protoID, 2)

	started := time.Now()
	moved, stats, err := repo.carryOverUnchangedVaccinationObligations(ctx, tenantID, goatIDs, []string{v2})
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("carry over: %v", err)
	}
	t.Logf("carry-over: goats=%d rows=%d moved=%d chunks=%d attempts=%d queries=%d candidates=%d duration=%s",
		scaleGoats, len(before), moved, stats.Chunks, stats.Attempts, stats.Queries, stats.Candidates, elapsed)

	wantMoved := (scaleGoats - scaleInvalidGoats) * len(scaleRules())
	if moved != wantMoved {
		t.Fatalf("moved %d rows, want %d", moved, wantMoved)
	}
	wantChunks := (scaleGoats + carryOverGoatChunkSize - 1) / carryOverGoatChunkSize
	if stats.Chunks != wantChunks {
		t.Fatalf("chunks=%d, want %d", stats.Chunks, wantChunks)
	}
	// Fixed statements per chunk attempt, independent of the candidate count: goat lock, and per
	// pass one candidate SELECT, at most four validator reads and a savepoint'd UPDATE.
	const maxQueriesPerAttempt = 1 + 2*(1+4+1)
	if stats.Queries > stats.Attempts*maxQueriesPerAttempt {
		t.Fatalf("queries=%d over %d attempts, want <= %d per attempt (per-row validation regressed)", stats.Queries, stats.Attempts, maxQueriesPerAttempt)
	}
	if elapsed > 20*time.Second {
		t.Fatalf("carry-over of %d rows took %s, want under 20s", len(before), elapsed)
	}

	after := scaleRowsFor(t, ctx, pool, goatIDs)
	if len(after) != len(before) {
		t.Fatalf("row count changed: %d -> %d", len(before), len(after))
	}
	for id, b := range before {
		a, ok := after[id]
		if !ok {
			t.Fatalf("obligation %s vanished", id)
		}
		if !a.due.Equal(b.due) || a.key != b.key {
			t.Fatalf("obligation %s changed identity: due %s->%s key %s->%s", id, b.due, a.due, b.key, a.key)
		}
		wantVersion := v2
		if invalid[b.goat] {
			wantVersion = v1
		}
		if a.version != wantVersion {
			t.Fatalf("obligation %s (goat %s invalid=%v) on version %s, want %s", id, b.goat, invalid[b.goat], a.version, wantVersion)
		}
	}

	// Idempotent: a second call finds nothing left that is valid to move.
	again, _, err := repo.carryOverUnchangedVaccinationObligations(ctx, tenantID, goatIDs, []string{v2})
	if err != nil || again != 0 {
		t.Fatalf("second carry-over moved=%d err=%v, want 0", again, err)
	}
}

// ---------------------------------------------------------------------------------------------
// Anchor semantics: persistence reads generation's anchor_admins exactly.
// ---------------------------------------------------------------------------------------------

const anchorParityGoat = "10000000-0000-4000-8000-00000000a9c1"

type anchorProtocol struct {
	version string
	dose1   string
	dose2   string
}

func createAnchorProtocol(t *testing.T, ctx context.Context, pool *pgxpool.Pool, proto *protopg.Repository, code, status string) anchorProtocol {
	t.Helper()
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{TenantID: tenantID, Code: code, Name: code, Category: "vaccination", Status: "draft"})
	if err != nil {
		t.Fatalf("definition %s: %v", code, err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{"vaccine":{"code":"BT","type":"killed","pathogen_class":"viral"}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version %s: %v", code, err)
	}
	dose1, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "bt_dose1", Sequence: 1, TriggerType: "birth_age", OffsetDays: 90,
		Repeat: "none", CatchUp: "immediate",
		EligibilityJSON: []byte(`{"vaccine":{"code":"BT","type":"killed","pathogen_class":"viral"}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("dose1 %s: %v", code, err)
	}
	dose2, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "bt_dose2", Sequence: 2, TriggerType: "after_previous_completion", OffsetDays: 21, MinGapDays: 21,
		Repeat: "none", CatchUp: "immediate",
		EligibilityJSON: []byte(`{"vaccine":{"code":"BT","type":"killed","pathogen_class":"viral"}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("dose2 %s: %v", code, err)
	}
	if status != "draft" {
		if _, err := pool.Exec(ctx, `UPDATE protocol_versions SET status=$3 WHERE tenant_id=$1::uuid AND protocol_version_id=$2::uuid`, tenantID, versionID, status); err != nil {
			t.Fatalf("set %s status %s: %v", code, status, err)
		}
	}
	return anchorProtocol{version: versionID, dose1: dose1, dose2: dose2}
}

func insertAnchorEvent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id, versionID string, anchor time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_anchor_events (
  vaccination_anchor_event_id, tenant_id, protocol_version_id, vaccine_code, dose_code, anchor_date,
  scope_type, scope_payload, reason, source_system, idempotency_key
) VALUES ($1::uuid, $2, $3, 'BT', 'bt_dose1', $4::date,
  'animal_set', jsonb_build_object('animal_ids', jsonb_build_array($5::text)), 'attested BT dose 1', 'test', $6)`,
		id, tenantID, versionID, anchor.Format("2006-01-02"), anchorParityGoat, "anchor-parity-"+id); err != nil {
		t.Fatalf("insert anchor %s: %v", id, err)
	}
}

// persistenceAnchorAndDecision runs the SAME load + decide the write path runs, in one tx.
func persistenceAnchorAndDecision(t *testing.T, ctx context.Context, pool *pgxpool.Pool, rule string, due time.Time) (*time.Time, error) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	tenant, _ := pgconv.UUID(tenantID)
	ruleID, _ := pgconv.UUID(rule)
	goat, _ := pgconv.UUID(anchorParityGoat)
	w := vaccinationWrite{Tenant: tenant, Rule: ruleID, TargetType: "goat", Target: goat, DueAt: due}
	inputs, err := loadVaccinationWriteInputs(ctx, tx, []vaccinationWrite{w})
	if err != nil {
		t.Fatalf("load inputs: %v", err)
	}
	if inputs[0].Contract == nil {
		t.Fatalf("no vaccination contract loaded for rule %s", rule)
	}
	return inputs[0].History.latestAnchored("BT"), decideVaccinationWrite(w, inputs[0])
}

func generationAnchor(t *testing.T, ctx context.Context, pool *pgxpool.Pool) *time.Time {
	t.Helper()
	vacc := vaccpg.NewRepository(pool, 10*time.Second)
	history, err := vacc.RecentVaccineAdministrationsForGoats(ctx, tenantID, []string{anchorParityGoat}, biztime.BusinessDayStart(time.Now()).AddDate(0, 0, 1).Add(-time.Nanosecond))
	if err != nil {
		t.Fatalf("generation history: %v", err)
	}
	var latest *time.Time
	for _, admin := range history[anchorParityGoat] {
		if admin.Source != "anchor_event" {
			continue
		}
		at := admin.AdministeredAt
		if latest == nil || at.After(*latest) {
			latest = &at
		}
	}
	return latest
}

func TestVaccinationAnchorSemanticsMatchGenerationForPersistence(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	if _, err := pool.Exec(ctx,
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, current_location_id, park_id, shed_id, dob)
		 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $4, $4, DATE '2025-01-01')`,
		anchorParityGoat, tenantID, meshaParty, cbePark); err != nil {
		t.Fatalf("seed goat: %v", err)
	}
	proto := protopg.NewRepository(pool, 5*time.Second)
	published := createAnchorProtocol(t, ctx, pool, proto, "vaccination.anchor.parity.published", "published")
	unpublished := createAnchorProtocol(t, ctx, pool, proto, "vaccination.anchor.parity.draft", "draft")
	retired := createAnchorProtocol(t, ctx, pool, proto, "vaccination.anchor.parity.retired", "retired")

	anchorDay := biztime.BusinessDayStart(time.Now()).AddDate(0, 0, -30)

	// 1. Anchors on an unpublished and a retired version only: ignored by BOTH sides.
	insertAnchorEvent(t, ctx, pool, "40000000-0000-4000-8000-00000000a9d1", unpublished.version, anchorDay)
	insertAnchorEvent(t, ctx, pool, "40000000-0000-4000-8000-00000000a9d2", retired.version, anchorDay)
	if got := generationAnchor(t, ctx, pool); got != nil {
		t.Fatalf("generation chained from an unpublished/retired anchor on %s", got)
	}
	anchored, err := persistenceAnchorAndDecision(t, ctx, pool, published.dose2, anchorDay.AddDate(0, 0, 60))
	if anchored != nil {
		t.Fatalf("persistence counted an unpublished/retired anchor on %s", anchored)
	}
	if !errors.Is(err, ports.ErrBeforeVaccinationAgeFloor) {
		t.Fatalf("dose 2 with only unpublished/retired anchors: err=%v, want previous-completion anchor missing", err)
	}

	// 2. An anchor on the published, effective version: both sides chain from the same day, and
	// the revac floor (anchor + 21) is the same date on both sides.
	insertAnchorEvent(t, ctx, pool, "40000000-0000-4000-8000-00000000a9d3", published.version, anchorDay)
	gen := generationAnchor(t, ctx, pool)
	if gen == nil {
		t.Fatalf("generation did not chain from the published anchor")
	}
	floor := biztime.BusinessDayStart(*gen).AddDate(0, 0, 21)
	anchored, err = persistenceAnchorAndDecision(t, ctx, pool, published.dose2, floor)
	if anchored == nil {
		t.Fatalf("persistence did not see the published anchor")
	}
	if biztime.BusinessDate(*anchored) != biztime.BusinessDate(*gen) {
		t.Fatalf("anchor day differs: generation %s persistence %s", biztime.BusinessDate(*gen), biztime.BusinessDate(*anchored))
	}
	if err != nil {
		t.Fatalf("dose 2 at generation's revac floor %s rejected by persistence: %v", biztime.BusinessDate(floor), err)
	}
	if _, err := persistenceAnchorAndDecision(t, ctx, pool, published.dose2, floor.AddDate(0, 0, -1)); !errors.Is(err, ports.ErrBeforeVaccinationAgeFloor) {
		t.Fatalf("dose 2 one day before generation's revac floor: err=%v, want rejection", err)
	}

	// 3. Through the real write path as well.
	repo := NewRepository(pool, 10*time.Second)
	if _, _, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: published.version, RuleID: published.dose2, TargetType: "goat", TargetID: anchorParityGoat,
		ScopeType: "park", ScopeID: cbePark, DueAt: floor, Status: "scheduled", IdempotencyKey: "anchor-parity-dose2", Sequence: 2,
	}); err != nil {
		t.Fatalf("insert dose 2 at the shared revac floor: %v", err)
	}
}

// ---------------------------------------------------------------------------------------------
// P2-21: in_progress work the new rule rejects is kept on its drive's version and surfaced.
// ---------------------------------------------------------------------------------------------

func TestCarryOverKeepsRejectedInProgressWorkBoundAndSurfacesIt(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 30*time.Second)
	proto := protopg.NewRepository(pool, 5*time.Second)
	const (
		underAge = "10000000-0000-4000-8000-00000000b0a1"
		adult    = "10000000-0000-4000-8000-00000000b0a2"
	)
	for _, g := range []string{underAge, adult} {
		seedCapacityGoatInPark(t, ctx, pool, g, cbePark)
	}
	if _, err := pool.Exec(ctx, `UPDATE goats SET dob = DATE '2020-01-01' WHERE tenant_id=$1::uuid AND goat_id = ANY($2::uuid[])`, tenantID, []string{underAge, adult}); err != nil {
		t.Fatalf("DOB: %v", err)
	}
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.carryover.inprogress", Name: "CarryOverInProgress", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	v1, v1Rules := createScaleVersion(t, ctx, proto, protoID, 1)
	if err := proto.PublishVersion(ctx, tenantID, v1, nil); err != nil {
		t.Fatalf("publish v1: %v", err)
	}
	due := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	startedUnderAge := insertObligationForRule(t, ctx, repo, v1, v1Rules["et_tt_primary"], underAge, "inprogress-under-ettt", due)
	scheduledUnderAge := insertObligationForRule(t, ctx, repo, v1, v1Rules["ppr_primary"], underAge, "inprogress-under-ppr", due)
	startedAdult := insertObligationForRule(t, ctx, repo, v1, v1Rules["et_tt_primary"], adult, "inprogress-adult-ettt", due)
	if _, err := pool.Exec(ctx, `UPDATE obligation_instances SET status='in_progress' WHERE tenant_id=$1::uuid AND obligation_id = ANY($2::uuid[])`,
		tenantID, []string{startedUnderAge, startedAdult}); err != nil {
		t.Fatalf("start drive rows: %v", err)
	}
	// A DOB correction after the drive started puts the first goat under every age floor.
	if _, err := pool.Exec(ctx, `UPDATE goats SET dob = DATE '2026-09-01' WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`, tenantID, underAge); err != nil {
		t.Fatalf("DOB correction: %v", err)
	}
	v2, _ := createScaleVersion(t, ctx, proto, protoID, 2)

	moved, stats, err := repo.carryOverUnchangedVaccinationObligations(ctx, tenantID, []string{underAge, adult}, []string{v2})
	if err != nil {
		t.Fatalf("carry over: %v", err)
	}
	if moved != 1 {
		t.Fatalf("moved=%d, want only the valid in-progress adult row", moved)
	}
	if len(stats.StrandedInProgress) != 1 || stats.StrandedInProgress[0] != startedUnderAge {
		t.Fatalf("stranded in-progress=%v, want exactly [%s] surfaced (the scheduled row is not live work)", stats.StrandedInProgress, startedUnderAge)
	}
	var version, status string
	for id, want := range map[string][2]string{
		startedUnderAge:   {v1, "in_progress"},
		scheduledUnderAge: {v1, "scheduled"},
		startedAdult:      {v2, "in_progress"},
	} {
		if err := pool.QueryRow(ctx, `SELECT protocol_version_id::text, status FROM obligation_instances WHERE tenant_id=$1::uuid AND obligation_id=$2::uuid`, tenantID, id).Scan(&version, &status); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		if version != want[0] || status != want[1] {
			t.Fatalf("obligation %s: version=%s status=%s, want %s/%s", id, version, status, want[0], want[1])
		}
	}
}
