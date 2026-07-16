package postgres

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	oblpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	vaccdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

type replayInsertBarrier struct {
	mu                  sync.Mutex
	pprArrivals         int
	bothAtPPR           chan struct{}
	goatPoxInserted     chan struct{}
	goatPoxInsertedOnce sync.Once
}

type replayBarrierObligationWriter struct {
	*oblpg.Repository
	barrier *replayInsertBarrier
}

func (w replayBarrierObligationWriter) InsertObligation(ctx context.Context, in obldomain.NewObligation) (string, bool, error) {
	if in.RuleID == "" {
		return w.Repository.InsertObligation(ctx, in)
	}
	if in.Sequence == 1 {
		w.barrier.mu.Lock()
		w.barrier.pprArrivals++
		if w.barrier.pprArrivals == 2 {
			close(w.barrier.bothAtPPR)
		}
		w.barrier.mu.Unlock()
		select {
		case <-w.barrier.bothAtPPR:
		case <-ctx.Done():
			return "", false, ctx.Err()
		}
	}
	id, applied, err := w.Repository.InsertObligation(ctx, in)
	if err != nil {
		return id, applied, err
	}
	if in.Sequence == 1 && applied {
		// Hold the PPR winner until the conflict loser has reconciled PPR and inserted Goat Pox.
		// This deterministically exercises the window where a stale preflight snapshot used to miss
		// the just-committed PPR row.
		select {
		case <-w.barrier.goatPoxInserted:
		case <-ctx.Done():
			return "", false, ctx.Err()
		}
	}
	if in.Sequence == 2 {
		w.barrier.goatPoxInsertedOnce.Do(func() { close(w.barrier.goatPoxInserted) })
	}
	return id, applied, nil
}

func seedGenGoat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id, lifecycle string) {
	t.Helper()
	seedGenGoatWithStage(t, ctx, pool, id, lifecycle, "K1")
}

func seedGenGoatWithStage(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id, lifecycle, stage string) {
	t.Helper()
	_, err := pool.Exec(ctx,
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex,
			   current_location_id, park_id, management_stage, dob)
			 VALUES ($1, $2, $3, 'goat', $4, 'female', $5, $5, $6, DATE '2026-05-01')`,
		id, impTenant, lifecycle, impParty, impCbe, stage)
	if err != nil {
		t.Fatalf("seed gen goat %s: %v", id, err)
	}
}

// TestConcurrentGenerationReplayUsesCommittedObligationForSpacing is the R2-01 PostgreSQL race
// guard. Both generators finish preflight before either inserts PPR. The PPR winner is then held
// until the conflict loser has reconciled the committed row and inserted Goat Pox. Spacing must use
// that authoritative PPR row, so the loser cannot materialize Goat Pox on the co-due date.
func TestConcurrentGenerationReplayUsesCommittedObligationForSpacing(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.replay.race", Name: "Replay race", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{"eligibility":{"animal_stage":"K1"},"compatibility_policy":{"live_to_live_gap_days":28},"source":{"source_system":"pc","source_ref":"R2-01","review_status":"approved","approved_by":"Reviewer"}}`),
		ProofPolicy:   []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	for _, rule := range []protodomain.NewRule{
		{TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "ppr_primary", Sequence: 1,
			TriggerType: "birth_age", OffsetDays: 30, DueWindowDays: 7, Repeat: "none", CatchUp: "pc_approval",
			EligibilityJSON: []byte(`{"vaccine":{"code":"PPR","name":"PPR","type":"live","pathogen_class":"viral","compatibility_group":"PPR","course_type":"single"}}`), ProofPolicy: []byte(`{}`)},
		{TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "goatpox_primary", Sequence: 2,
			TriggerType: "birth_age", OffsetDays: 30, DueWindowDays: 7, Repeat: "none", CatchUp: "pc_approval",
			EligibilityJSON: []byte(`{"vaccine":{"code":"GOAT_POX","name":"Goat Pox","type":"live","pathogen_class":"viral","compatibility_group":"POX","course_type":"single"}}`), ProofPolicy: []byte(`{}`)},
	} {
		if _, err := proto.CreateRule(ctx, rule); err != nil {
			t.Fatalf("rule %s: %v", rule.DoseCode, err)
		}
	}
	if err := proto.PublishVersion(ctx, impTenant, versionID, nil); err != nil {
		t.Fatalf("publish: %v", err)
	}

	const goatID = "30000000-0000-4000-8000-0000000000b7"
	seedGenGoat(t, ctx, pool, goatID, "alive")
	if _, err := pool.Exec(ctx, `UPDATE goats SET dob=DATE '2026-07-01' WHERE tenant_id=$1 AND goat_id=$2`, impTenant, goatID); err != nil {
		t.Fatalf("set DOB: %v", err)
	}

	barrier := &replayInsertBarrier{bothAtPPR: make(chan struct{}), goatPoxInserted: make(chan struct{})}
	writer := replayBarrierObligationWriter{Repository: obl, barrier: barrier}
	gens := []*vaccapp.GenerationService{
		vaccapp.NewGenerationService(proto, vacc, writer),
		vaccapp.NewGenerationService(proto, vacc, writer),
	}
	errs := make(chan error, len(gens))
	for _, gen := range gens {
		go func(g *vaccapp.GenerationService) {
			_, err := g.GenerateForGoat(ctx, impTenant, goatID, time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC))
			errs <- err
		}(gen)
	}
	for range gens {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent generation: %v", err)
		}
	}

	var dueAt time.Time
	if err := pool.QueryRow(ctx, `
		SELECT due_at
		FROM obligation_instances
		WHERE tenant_id=$1 AND protocol_version_id=$2 AND target_id=$3 AND sequence=2`,
		impTenant, versionID, goatID).Scan(&dueAt); err != nil {
		t.Fatalf("load Goat Pox due date: %v", err)
	}
	india, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatalf("load India timezone: %v", err)
	}
	if got, want := dueAt.In(india).Format("2006-01-02"), "2026-08-28"; got != want {
		t.Fatalf("Goat Pox due = %s, want %s (committed PPR due + 28 days)", got, want)
	}
}

func seedGenAdultProcuredGoat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id, lifecycle string, entryDate time.Time) {
	t.Helper()
	seedGenGoatWithStage(t, ctx, pool, id, lifecycle, "adult")
	if _, err := pool.Exec(ctx, `
UPDATE goats
SET entry_date = $3::date,
    dob = DATE '2025-01-01',
    origin_type = 'procured',
    shed_id = $4::uuid,
    current_location_id = $4::uuid
WHERE tenant_id = $1 AND goat_id = $2`, impTenant, id, entryDate, impCbe); err != nil {
		t.Fatalf("set adult procured goat %s: %v", id, err)
	}
}

func TestListEligibleGoatsForGenerationUsesCompiledRuleDimensions(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.compiled", Name: "Compiled Matrix", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{"ruleset_family":"vaccination.matrix"}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleID, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", OffsetDays: 28, Repeat: "none", CatchUp: "immediate",
		EligibilityJSON: []byte(`{"matrix_row_id":"k1-goat-female","eligibility":{"species":["goat"],"animal_stage":["K1"],"sex":["female"],"min_age_days":20,"max_age_days":40},"vaccine":{"code":"ET_TT","type":"killed"}}`),
		ProofPolicy:     []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}
	if err := proto.ReplaceProtocolRuleDimensions(ctx, impTenant, versionID, []protodomain.RuleDimension{{
		Category:        "vaccination",
		RulesetFamily:   "vaccination.matrix",
		RuleID:          ruleID,
		MatrixRowID:     "k1-goat-female",
		SelectorKey:     "k1-goat-female|primary|goat|K1|female|all|alive|any|any",
		DoseCode:        "primary",
		VaccineCode:     "ET_TT",
		VaccineType:     "killed",
		Species:         "goat",
		AnimalStage:     "K1",
		Sex:             "female",
		Breed:           "all",
		Lifecycle:       "alive",
		Health:          "any",
		Reproductive:    "any",
		MinAgeDays:      ptrInt32(20),
		MaxAgeDays:      ptrInt32(40),
		TriggerType:     "birth_age",
		Sequence:        1,
		OffsetDays:      28,
		DueWindowDays:   7,
		Repeat:          "none",
		CatchUp:         "immediate",
		EligibilityJSON: []byte(`{"species":["goat"],"animal_stage":["K1"],"sex":["female"],"min_age_days":20,"max_age_days":40}`),
		VaccineJSON:     []byte(`{"code":"ET_TT","type":"killed"}`),
		ScheduleJSON:    []byte(`{"dose_code":"primary"}`),
	}}); err != nil {
		t.Fatalf("compiled dimensions: %v", err)
	}

	asOf := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	type animal struct {
		id      string
		species string
		sex     string
		stage   string
		dob     any
	}
	for _, a := range []animal{
		{"32000000-0000-4000-8000-000000000101", "goat", "female", "K1", asOf.AddDate(0, 0, -30)},
		{"32000000-0000-4000-8000-000000000102", "goat", "female", "K1", asOf.AddDate(0, 0, -10)},
		{"32000000-0000-4000-8000-000000000103", "goat", "male", "K1", asOf.AddDate(0, 0, -30)},
		{"32000000-0000-4000-8000-000000000104", "sheep", "female", "K1", asOf.AddDate(0, 0, -30)},
		{"32000000-0000-4000-8000-000000000105", "goat", "female", "adult", asOf.AddDate(0, 0, -30)},
		{"32000000-0000-4000-8000-000000000106", "goat", "female", "K1", nil},
	} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex,
			   current_location_id, park_id, management_stage, dob)
			 VALUES ($1, $2, 'alive', $3, $4, $5, $6, $6, $7, $8::date)`,
			a.id, impTenant, a.species, impParty, a.sex, impCbe, a.stage, a.dob); err != nil {
			t.Fatalf("seed compiled goat %s: %v", a.id, err)
		}
	}

	repo := NewRepository(pool, 5*time.Second)
	rows, err := repo.ListEligibleGoatsForGeneration(ctx, vaccdomain.ImpactFilter{
		TenantID:          impTenant,
		ProtocolVersionID: versionID,
		AsOf:              asOf,
	}, "", 50)
	if err != nil {
		t.Fatalf("list eligible compiled: %v", err)
	}
	if len(rows) != 1 || rows[0].GoatID != "32000000-0000-4000-8000-000000000101" {
		t.Fatalf("rows=%#v, want only goat/female/K1 in compiled age window", rows)
	}
}

func ptrInt32(v int32) *int32 { return &v }

func TestListEligibleGoatsForGenerationUsesISTForCompiledAge(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.compiled.tz", Name: "Compiled Matrix TZ", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{"ruleset_family":"vaccination.matrix"}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleID, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", OffsetDays: 1, Repeat: "none", CatchUp: "immediate",
		EligibilityJSON: []byte(`{"matrix_row_id":"k1-goat-female-tz","eligibility":{"species":["goat"],"animal_stage":["K1"],"sex":["female"],"min_age_days":1},"vaccine":{"code":"ET_TT","type":"killed"}}`),
		ProofPolicy:     []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}
	if err := proto.ReplaceProtocolRuleDimensions(ctx, impTenant, versionID, []protodomain.RuleDimension{{
		Category:        "vaccination",
		RulesetFamily:   "vaccination.matrix",
		RuleID:          ruleID,
		MatrixRowID:     "k1-goat-female-tz",
		SelectorKey:     "k1-goat-female-tz|primary|goat|K1|female|all|alive|any|any",
		DoseCode:        "primary",
		VaccineCode:     "ET_TT",
		VaccineType:     "killed",
		Species:         "goat",
		AnimalStage:     "K1",
		Sex:             "female",
		Breed:           "all",
		Lifecycle:       "alive",
		Health:          "any",
		Reproductive:    "any",
		MinAgeDays:      ptrInt32(1),
		TriggerType:     "birth_age",
		Sequence:        1,
		OffsetDays:      1,
		DueWindowDays:   7,
		Repeat:          "none",
		CatchUp:         "immediate",
		EligibilityJSON: []byte(`{"species":["goat"],"animal_stage":["K1"],"sex":["female"],"min_age_days":1}`),
		VaccineJSON:     []byte(`{"code":"ET_TT","type":"killed"}`),
		ScheduleJSON:    []byte(`{"dose_code":"primary"}`),
	}}); err != nil {
		t.Fatalf("compiled dimensions: %v", err)
	}

	goatID := "32000000-0000-4000-8000-000000000201"
	if _, err := pool.Exec(ctx,
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex,
		   current_location_id, park_id, management_stage, dob)
		 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $4, 'K1', DATE '2026-06-28')`,
		goatID, impTenant, impParty, impCbe); err != nil {
		t.Fatalf("seed timezone goat: %v", err)
	}

	asOf := time.Date(2026, 6, 28, 20, 0, 0, 0, time.UTC)
	repo := NewRepository(pool, 5*time.Second)

	if _, err := pool.Exec(ctx, `UPDATE locations SET timezone = 'UTC' WHERE tenant_id = $1 AND location_id = $2`, impTenant, impCbe); err != nil {
		t.Fatalf("set UTC location timezone: %v", err)
	}
	rows, err := repo.ListEligibleGoatsForGeneration(ctx, vaccdomain.ImpactFilter{
		TenantID:          impTenant,
		ProtocolVersionID: versionID,
		AsOf:              asOf,
	}, "", 50)
	if err != nil {
		t.Fatalf("list eligible UTC boundary: %v", err)
	}
	if len(rows) != 1 || rows[0].GoatID != goatID {
		t.Fatalf("UTC rows=%#v, want goat eligible on the IST first birthday", rows)
	}

	if _, err := pool.Exec(ctx, `UPDATE locations SET timezone = 'Asia/Kolkata' WHERE tenant_id = $1 AND location_id = $2`, impTenant, impCbe); err != nil {
		t.Fatalf("set IST location timezone: %v", err)
	}
	rows, err = repo.ListEligibleGoatsForGeneration(ctx, vaccdomain.ImpactFilter{
		TenantID:          impTenant,
		ProtocolVersionID: versionID,
		AsOf:              asOf,
	}, "", 50)
	if err != nil {
		t.Fatalf("list eligible IST boundary: %v", err)
	}
	if len(rows) != 1 || rows[0].GoatID != goatID {
		t.Fatalf("IST rows=%#v, want goat eligible on the IST first birthday", rows)
	}
}

func TestSM1GenerationIdempotentAndDeferVisible(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	// Published vaccination version: K1 eligibility, defer ICU/quarantine/sick, one birth_age rule.
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.gen", Name: "Gen", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	ruleDSL := []byte(`{"eligibility":{"animal_stage":"K1","defer_states":["ICU","quarantine","sick"]},` +
		`"source":{"source_system":"pc","source_ref":"PC §6","review_status":"approved","approved_by":"Reviewer"}}`)
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: ruleDSL, ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if _, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", OffsetDays: 21, Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	}); err != nil {
		t.Fatalf("rule: %v", err)
	}
	if err := proto.PublishVersion(ctx, impTenant, versionID, nil); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// 2 alive + 1 quarantine (defer) K1 goats, plus one non-K1 goat that Config-authored
	// animal_stage eligibility must exclude.
	seedGenGoat(t, ctx, pool, "30000000-0000-4000-8000-0000000000a1", "alive")
	seedGenGoat(t, ctx, pool, "30000000-0000-4000-8000-0000000000a2", "alive")
	seedGenGoat(t, ctx, pool, "30000000-0000-4000-8000-0000000000d1", "quarantine")
	seedGenGoatWithStage(t, ctx, pool, "30000000-0000-4000-8000-0000000000e1", "alive", "adult")

	gen := vaccapp.NewGenerationService(proto, vacc, obl)
	asOf := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)

	res, err := gen.GenerateForVersion(ctx, impTenant, versionID, asOf)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if res.Generated != 3 {
		t.Fatalf("generated: want 3, got %d", res.Generated)
	}
	if res.Deferred != 1 {
		t.Fatalf("deferred: want 1 (quarantine goat), got %d", res.Deferred)
	}

	// Idempotent re-run: no new obligations.
	res2, err := gen.GenerateForVersion(ctx, impTenant, versionID, asOf)
	if err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	if res2.Generated != 0 {
		t.Fatalf("re-gen should be a no-op, generated %d", res2.Generated)
	}

	if got := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND protocol_version_id=$2`, impTenant, versionID); got != 3 {
		t.Fatalf("expected 3 obligations, got %d", got)
	}
	// Defer is canonical: the quarantine goat's obligation is held in deferred status and also has
	// a deferred ledger event for audit/as-of views.
	if got := countRowsVacc(t, ctx, pool,
		`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='deferred'`,
		impTenant, "30000000-0000-4000-8000-0000000000d1"); got != 1 {
		t.Fatalf("expected 1 deferred obligation for quarantine goat, got %d", got)
	}
	if got := countRowsVacc(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events e
		 JOIN obligation_instances o ON o.tenant_id=e.tenant_id AND o.obligation_id=e.obligation_id
		 WHERE e.tenant_id=$1 AND e.event_type='deferred' AND o.target_id=$2`,
		impTenant, "30000000-0000-4000-8000-0000000000d1"); got != 1 {
		t.Fatalf("expected 1 deferred event for quarantine goat, got %d", got)
	}
}

func TestStartGenerationRunFailedReplayPreservesStartedAt(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.manual.retry", Name: "Manual retry", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}

	firstAt := time.Date(2026, 6, 27, 8, 0, 0, 0, time.UTC)
	secondAt := firstAt.Add(5 * time.Minute)
	run, started, err := vacc.StartGenerationRun(ctx, vaccdomain.GenerationRunInput{
		TenantID: impTenant, ProtocolVersionID: versionID, TriggerType: "manual_campaign", TriggerRef: "catchup@first",
		StartedAt: firstAt, IdempotencyKey: "manual-campaign-retry-key", RequestHash: "request-hash",
	})
	if err != nil || !started {
		t.Fatalf("start generation run started=%v err=%v run=%#v", started, err, run)
	}
	if err := vacc.FinishGenerationRun(ctx, impTenant, run.RunID, vaccdomain.GenerateResult{Generated: 1, Reopened: 2}, "", "forced partial failure", firstAt.Add(time.Minute)); err != nil {
		t.Fatalf("finish failed run: %v", err)
	}
	var reopenedCount int
	if err := pool.QueryRow(ctx,
		`SELECT reopened_count FROM vaccination_generation_runs WHERE tenant_id = $1::uuid AND run_id = $2::uuid`,
		impTenant, run.RunID).Scan(&reopenedCount); err != nil {
		t.Fatalf("read reopened count: %v", err)
	}
	if reopenedCount != 2 {
		t.Fatalf("persisted reopened count: want 2, got %d", reopenedCount)
	}

	retry, restarted, err := vacc.StartGenerationRun(ctx, vaccdomain.GenerationRunInput{
		TenantID: impTenant, ProtocolVersionID: versionID, TriggerType: "manual_campaign", TriggerRef: "catchup@second",
		StartedAt: secondAt, IdempotencyKey: "manual-campaign-retry-key", RequestHash: "request-hash",
	})
	if err != nil || !restarted {
		t.Fatalf("restart generation run restarted=%v err=%v run=%#v", restarted, err, retry)
	}
	if !retry.StartedAt.Equal(firstAt) || retry.Status != "running" || retry.CompletedAt != nil || retry.Generated != 0 || retry.Reopened != 0 || retry.RequestHash != "request-hash" {
		t.Fatalf("retry run = %#v, want original started_at, reset counts, running", retry)
	}
}

func TestStartGenerationRunStaleRunningReplayRestarts(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.manual.stale_retry", Name: "Manual stale retry", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}

	firstAt := time.Date(2026, 6, 27, 8, 0, 0, 0, time.UTC)
	run, started, err := vacc.StartGenerationRun(ctx, vaccdomain.GenerationRunInput{
		TenantID: impTenant, ProtocolVersionID: versionID, TriggerType: "manual_campaign", TriggerRef: "stale@first",
		StartedAt: firstAt, IdempotencyKey: "manual-campaign-stale-key", RequestHash: "request-hash",
	})
	if err != nil || !started {
		t.Fatalf("start generation run started=%v err=%v run=%#v", started, err, run)
	}

	// Model a DEAD run: no heartbeat for >15 REAL minutes. Staleness is wall-clock now(), so updated_at
	// must be aged relative to now(), NOT to the business asOf (the bug this guards: an asOf-based
	// window can never elapse on replay).
	if _, err := pool.Exec(ctx, `
UPDATE vaccination_generation_runs SET updated_at = now() - interval '16 minutes'
WHERE tenant_id = $1::uuid AND idempotency_key = 'manual-campaign-stale-key'`, impTenant); err != nil {
		t.Fatalf("age dead generation run: %v", err)
	}

	// Publish REDELIVERY: the same event carries the same OccurredAt, so asOf == the stuck run's
	// started_at. A wall-clock staleness window still reclaims the dead run; an asOf-based window
	// (started_at < asOf-15m) never could, wedging the cohort forever.
	retry, restarted, err := vacc.StartGenerationRun(ctx, vaccdomain.GenerationRunInput{
		TenantID: impTenant, ProtocolVersionID: versionID, TriggerType: "manual_campaign", TriggerRef: "stale@first",
		StartedAt: firstAt, IdempotencyKey: "manual-campaign-stale-key", RequestHash: "request-hash",
	})
	if err != nil || !restarted {
		t.Fatalf("restart stale generation run restarted=%v err=%v run=%#v", restarted, err, retry)
	}
	if retry.Status != "running" || retry.RequestHash != "request-hash" {
		t.Fatalf("stale retry run = %#v, want restarted running row", retry)
	}
}

// TestStartGenerationRunHeartbeatPreventsReclaim proves a long but LIVE run (recent wall-clock
// heartbeat) is not reclaimed/duplicated by the stale-run detector — even when the reclaiming caller
// supplies a FUTURE business asOf (staleness is measured against now(), not asOf).
func TestStartGenerationRunHeartbeatPreventsReclaim(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.manual.heartbeat", Name: "Manual heartbeat", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}

	firstAt := time.Date(2026, 6, 27, 8, 0, 0, 0, time.UTC)
	run, started, err := vacc.StartGenerationRun(ctx, vaccdomain.GenerationRunInput{
		TenantID: impTenant, ProtocolVersionID: versionID, TriggerType: "manual_campaign", TriggerRef: "live@first",
		StartedAt: firstAt, IdempotencyKey: "manual-campaign-live-key", RequestHash: "request-hash",
	})
	if err != nil || !started {
		t.Fatalf("start generation run started=%v err=%v run=%#v", started, err, run)
	}

	// Recent heartbeat: updated_at 5 REAL minutes ago (well inside the 15m wall-clock window).
	if _, err := pool.Exec(ctx, `
UPDATE vaccination_generation_runs SET updated_at = now() - interval '5 minutes'
WHERE tenant_id = $1::uuid AND idempotency_key = 'manual-campaign-live-key'`, impTenant); err != nil {
		t.Fatalf("heartbeat generation run: %v", err)
	}

	// FUTURE business asOf must NOT steal the live run: an asOf-based window (asOf-15m) would exceed
	// now() and wrongly reclaim, but staleness is wall-clock so the recent heartbeat wins.
	futureAsOf := firstAt.Add(72 * time.Hour)
	retry, restarted, err := vacc.StartGenerationRun(ctx, vaccdomain.GenerationRunInput{
		TenantID: impTenant, ProtocolVersionID: versionID, TriggerType: "manual_campaign", TriggerRef: "live@retry",
		StartedAt: futureAsOf, IdempotencyKey: "manual-campaign-live-key", RequestHash: "request-hash",
	})
	if err != nil {
		t.Fatalf("retry live run: %v", err)
	}
	if restarted {
		t.Fatalf("live (recently heartbeated) run must NOT be reclaimed even with a future asOf; got restarted=true run=%#v", retry)
	}
	if retry.RunID != run.RunID || retry.Status != "running" {
		t.Fatalf("retry should return the original live run, got %#v", retry)
	}
}

func TestGoatCreatedHandlerGeneratesViaBus(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.bus", Name: "Bus", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	ruleDSL := []byte(`{"eligibility":{"animal_stage":"K1","defer_states":["ICU","quarantine","sick"]},` +
		`"source":{"source_system":"pc","source_ref":"PC §6","review_status":"approved","approved_by":"Reviewer"}}`)
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: ruleDSL, ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if _, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", OffsetDays: 21, Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	}); err != nil {
		t.Fatalf("rule: %v", err)
	}
	if err := proto.PublishVersion(ctx, impTenant, versionID, nil); err != nil {
		t.Fatalf("publish: %v", err)
	}

	goatID := "30000000-0000-4000-8000-0000000000b9"
	seedGenGoat(t, ctx, pool, goatID, "alive")

	bus := eventbus.NewInProcessBus()
	vaccapp.NewGoatCreatedHandler(vaccapp.NewGenerationService(proto, vacc, obl)).Register(bus)

	if err := bus.Publish(ctx, eventbus.Event{
		Type: vaccapp.EventGoatCreated, TenantID: impTenant, Key: goatID,
		OccurredAt: time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("publish goat.created: %v", err)
	}

	if got := countRowsVacc(t, ctx, pool,
		`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND protocol_version_id=$3`,
		impTenant, goatID, versionID); got != 1 {
		t.Fatalf("expected 1 obligation generated via bus, got %d", got)
	}
}

func TestGoatRecheckDefersExistingScheduledObligation(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.recheck.defer", Name: "Recheck Defer", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	ruleDSL := []byte(`{"eligibility":{"animal_stage":"K1","defer_states":["sick","quarantine","ICU"]},` +
		`"source":{"source_system":"pc","source_ref":"PC §6","review_status":"approved","approved_by":"Reviewer"}}`)
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: ruleDSL, ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if _, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", OffsetDays: 21, Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	}); err != nil {
		t.Fatalf("rule: %v", err)
	}
	if err := proto.PublishVersion(ctx, impTenant, versionID, nil); err != nil {
		t.Fatalf("publish: %v", err)
	}

	goatID := "30000000-0000-4000-8000-0000000000f1"
	seedGenGoat(t, ctx, pool, goatID, "alive")
	gen := vaccapp.NewGenerationService(proto, vacc, obl)
	asOf := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)
	if res, err := gen.GenerateForGoat(ctx, impTenant, goatID, asOf); err != nil || res.Generated != 1 {
		t.Fatalf("initial generate result=%#v err=%v", res, err)
	}
	obligationID := scanText(t, ctx, pool,
		`SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND protocol_version_id=$3`,
		impTenant, goatID, versionID)
	batchID, attached, err := obl.CreateBatchWithObligations(ctx, obldomain.NewBatch{
		TenantID: impTenant, ProtocolVersionID: versionID, ScopeType: "park", ScopeID: impCbe,
		Status: "planned", EstimatedTargets: 1,
	}, []string{obligationID})
	if err != nil {
		t.Fatalf("attach planned batch: %v", err)
	}
	if attached != 1 {
		t.Fatalf("attached=%d, want 1", attached)
	}

	if _, err := pool.Exec(ctx, `UPDATE goats SET health_status='sick' WHERE tenant_id=$1 AND goat_id=$2`, impTenant, goatID); err != nil {
		t.Fatalf("mark goat sick: %v", err)
	}
	bus := eventbus.NewInProcessBus()
	vaccapp.NewGoatRecheckHandler(gen).Register(bus)
	if err := bus.Publish(ctx, eventbus.Event{
		Type: vaccapp.EventGoatLocationChanged, TenantID: impTenant, Key: goatID,
		OccurredAt: asOf.Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("publish goat recheck: %v", err)
	}

	if got := scanText(t, ctx, pool, `SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, impTenant, obligationID); got != "deferred" {
		t.Fatalf("status=%q, want deferred", got)
	}
	if got := countRowsVacc(t, ctx, pool,
		`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2 AND batch_id IS NULL`,
		impTenant, obligationID); got != 1 {
		t.Fatalf("expected deferred obligation detached from planned batch, got %d", got)
	}
	if got := scanText(t, ctx, pool, `SELECT estimated_targets::text FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2`, impTenant, batchID); got != "0" {
		t.Fatalf("planned batch estimated_targets=%s, want 0 after defer detach", got)
	}
	if got := countRowsVacc(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='deferred'`,
		impTenant, obligationID); got != 1 {
		t.Fatalf("deferred events=%d, want 1", got)
	}
}

func TestGoatCreatedTrustedHFEvidenceSuppressesMatchingObligation(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.hf.suppress", Name: "HF Suppress", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	ruleDSL := []byte(`{"eligibility":{},"source":{"source_system":"pc","source_ref":"PC §6","review_status":"approved","approved_by":"Reviewer"}}`)
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: ruleDSL, ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleID, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "post_arrival", OffsetDays: 7, Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}
	if err := proto.PublishVersion(ctx, impTenant, versionID, nil); err != nil {
		t.Fatalf("publish: %v", err)
	}

	trustedGoat := "30000000-0000-4000-8000-0000000000c1"
	importedGoat := "30000000-0000-4000-8000-0000000000c2"
	futureTrustedGoat := "30000000-0000-4000-8000-0000000000c3"
	entryDate := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	seedGenAdultProcuredGoat(t, ctx, pool, trustedGoat, "alive", entryDate)
	seedGenAdultProcuredGoat(t, ctx, pool, importedGoat, "alive", entryDate)
	seedGenAdultProcuredGoat(t, ctx, pool, futureTrustedGoat, "alive", entryDate)
	pastAdministered := time.Date(2026, 6, 8, 8, 0, 0, 0, time.UTC)
	pastReviewed := time.Date(2026, 6, 9, 8, 0, 0, 0, time.UTC)
	futureAdministered := time.Date(2026, 6, 12, 8, 0, 0, 0, time.UTC)
	futureReviewed := time.Date(2026, 6, 13, 8, 0, 0, 0, time.UTC)
	seedGenerationProcurementEvidence(t, ctx, pool, "30000000-0000-4000-8000-00000000d001", trustedGoat, versionID, ruleID, "trusted", pastAdministered, &pastReviewed)
	seedGenerationProcurementEvidence(t, ctx, pool, "30000000-0000-4000-8000-00000000d002", importedGoat, versionID, ruleID, "imported", pastAdministered, nil)
	seedGenerationProcurementEvidence(t, ctx, pool, "30000000-0000-4000-8000-00000000d003", futureTrustedGoat, versionID, ruleID, "trusted", futureAdministered, &futureReviewed)

	gen := vaccapp.NewGenerationService(proto, vacc, obl)
	asOf := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)
	trustedResult, err := gen.GenerateForGoat(ctx, impTenant, trustedGoat, asOf)
	if err != nil {
		t.Fatalf("generate trusted goat: %v", err)
	}
	if trustedResult.Generated != 0 || trustedResult.SuppressedByTrustedHistory != 1 {
		t.Fatalf("trusted result = %#v, want generated 0 suppressed 1", trustedResult)
	}
	importedResult, err := gen.GenerateForGoat(ctx, impTenant, importedGoat, asOf)
	if err != nil {
		t.Fatalf("generate imported goat: %v", err)
	}
	if importedResult.Generated != 1 || importedResult.SuppressedByTrustedHistory != 0 {
		t.Fatalf("imported result = %#v, want generated 1 suppressed 0", importedResult)
	}
	futureTrustedResult, err := gen.GenerateForGoat(ctx, impTenant, futureTrustedGoat, asOf)
	if err != nil {
		t.Fatalf("generate future trusted goat: %v", err)
	}
	if futureTrustedResult.Generated != 1 || futureTrustedResult.SuppressedByTrustedHistory != 0 {
		t.Fatalf("future trusted result = %#v, want generated 1 suppressed 0", futureTrustedResult)
	}
	if got := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, impTenant, trustedGoat); got != 0 {
		t.Fatalf("trusted HF evidence goat obligations = %d, want 0", got)
	}
	if got := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, impTenant, importedGoat); got != 1 {
		t.Fatalf("imported-only HF evidence goat obligations = %d, want 1", got)
	}
	if got := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, impTenant, futureTrustedGoat); got != 1 {
		t.Fatalf("future trusted HF evidence goat obligations = %d, want 1", got)
	}
}

func TestGoatCreatedTrustedHFEvidenceSuppressesAcrossProtocolVersions(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.hf.versioned", Name: "HF Versioned", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	v1End := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	v1ID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), EffectiveTo: &v1End,
		RuleDsl: []byte(`{"eligibility":{}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("v1: %v", err)
	}
	v1RuleID, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: v1ID, DoseCode: "primary", Sequence: 1,
		TriggerType: "post_arrival", OffsetDays: 7, Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("v1 rule: %v", err)
	}
	if err := proto.PublishVersion(ctx, impTenant, v1ID, nil); err != nil {
		t.Fatalf("publish v1: %v", err)
	}

	v2ID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 2, Status: "draft",
		EffectiveFrom: v1End, RuleDsl: []byte(`{"eligibility":{}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("v2: %v", err)
	}
	if _, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: v2ID, DoseCode: "primary", Sequence: 1,
		TriggerType: "post_arrival", OffsetDays: 7, Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	}); err != nil {
		t.Fatalf("v2 rule: %v", err)
	}
	if err := proto.PublishVersion(ctx, impTenant, v2ID, nil); err != nil {
		t.Fatalf("publish v2: %v", err)
	}

	goatID := "30000000-0000-4000-8000-0000000000c4"
	entryDate := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	seedGenAdultProcuredGoat(t, ctx, pool, goatID, "alive", entryDate)
	reviewedAt := time.Date(2026, 6, 9, 8, 0, 0, 0, time.UTC)
	seedGenerationProcurementEvidence(t, ctx, pool, "30000000-0000-4000-8000-00000000d004", goatID, v1ID, v1RuleID, "trusted", time.Date(2026, 6, 8, 8, 0, 0, 0, time.UTC), &reviewedAt)

	gen := vaccapp.NewGenerationService(proto, vacc, obl)
	asOf := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	result, err := gen.GenerateForGoat(ctx, impTenant, goatID, asOf)
	if err != nil {
		t.Fatalf("generate goat: %v", err)
	}
	if result.Generated != 0 || result.SuppressedByTrustedHistory != 1 {
		t.Fatalf("result = %#v, want generated 0 suppressed 1", result)
	}
	if got := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, impTenant, goatID); got != 0 {
		t.Fatalf("cross-version trusted HF evidence obligations = %d, want 0", got)
	}
}

func seedGenerationProcurementEvidence(t *testing.T, ctx context.Context, pool *pgxpool.Pool, loadID, goatID, versionID, ruleID, reviewStatus string, administeredAt time.Time, reviewedAt *time.Time) {
	t.Helper()
	holdingStart := administeredAt.Add(-10 * 24 * time.Hour)
	holdingEnd := holdingStart.Add(30 * 24 * time.Hour)
	if _, err := pool.Exec(ctx, `
	INSERT INTO procurement_loads (load_id, tenant_id, source_party_id, expected_count, status, idempotency_key)
	VALUES ($1, $2, $3, 1, 'source_warmup', $4)
ON CONFLICT (tenant_id, load_id) DO NOTHING`, loadID, impTenant, impParty, "gen-hf-load:"+loadID); err != nil {
		t.Fatalf("seed procurement load: %v", err)
	}
	if _, err := pool.Exec(ctx, `
	INSERT INTO procurement_load_goats (
	  tenant_id, load_id, goat_id, purpose, current_state, selection_state,
	  source_entry_state, ownership_state, health_state, warmup_started_at,
	  warmup_ended_at, warmup_days, holding_location_id
	) VALUES (
	  $1, $2, $3, 'breeding', 'accepted_herd_intake', 'accepted_herd_intake',
	  'accepted', 'mesha_owned', 'passed', $4::timestamptz, $5::timestamptz, 30, $6::uuid
	)
	ON CONFLICT (tenant_id, load_id, goat_id) DO NOTHING`, impTenant, loadID, goatID, holdingStart, holdingEnd, impCbe); err != nil {
		t.Fatalf("seed procurement load goat: %v", err)
	}
	if _, err := pool.Exec(ctx, `
	INSERT INTO proof_artifacts (
	  proof_id, tenant_id, storage_provider, object_key, upload_state,
	  scope_type, scope_id, subject_type, proof_type, content_hash, mime_type, size_bytes
	) VALUES (
	  $1::uuid, $2, 'local', 'gen-hf-proof-' || $1::text, 'completed',
	  'tenant', $2, 'other', 'video', 'hash-' || $1::text, 'video/mp4', 100
	)
	ON CONFLICT (proof_id) DO NOTHING`, loadID, impTenant); err != nil {
		t.Fatalf("seed HF proof: %v", err)
	}
	if _, err := pool.Exec(ctx, `
	INSERT INTO procurement_hf_vaccination_evidence (
	  tenant_id, load_id, goat_id, protocol_version_id, rule_id, dose_code,
	  administered_at, vaccine_name, lot_number, proof_ref_id, source_ref, review_status,
	  reviewed_at, idempotency_key
	) VALUES (
	  $1, $2, $3, $4, $5, 'primary',
	  $6::timestamptz, 'HF vaccine', 'HF-LOT', $10::uuid, 'procurement_holding_park',
	  $7, $8::timestamptz,
	  $9
	)`, impTenant, loadID, goatID, versionID, ruleID, administeredAt, reviewStatus, reviewedAt, "gen-hf-evidence:"+goatID, loadID); err != nil {
		t.Fatalf("seed HF evidence: %v", err)
	}
}

func countRowsVacc(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestRecoverableDeferredCounterIgnoresMissedRows(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.recoverable_counter", Name: "Recoverable Counter", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{"ruleset_family":"vaccination.matrix"}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleID, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", OffsetDays: 28, Repeat: "none", CatchUp: "immediate",
		EligibilityJSON: []byte(`{"species":["goat"],"animal_stage":["K1"]}`),
		ProofPolicy:     []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}

	deferredGoat := "33000000-0000-4000-8000-000000000101"
	missedGoat := "33000000-0000-4000-8000-000000000102"
	seedGenGoat(t, ctx, pool, deferredGoat, "alive")
	seedGenGoat(t, ctx, pool, missedGoat, "alive")
	dueAt := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	for _, row := range []struct {
		goatID string
		status string
		key    string
	}{
		{goatID: deferredGoat, status: "deferred", key: "recoverable-counter:deferred"},
		{goatID: missedGoat, status: "missed", key: "recoverable-counter:missed"},
	} {
		if _, applied, err := obl.InsertObligation(ctx, obldomain.NewObligation{
			TenantID: impTenant, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: row.goatID, ScopeType: "tenant", ScopeID: impTenant,
			DueAt: dueAt, Status: row.status, IdempotencyKey: row.key, Sequence: 1,
		}); err != nil || !applied {
			t.Fatalf("seed %s obligation: applied=%v err=%v", row.status, applied, err)
		}
	}

	olderThan := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	total, err := vacc.CountRecoverableDeferredVaccinationObligations(ctx, impTenant, olderThan)
	if err != nil {
		t.Fatalf("count recoverable deferred: %v", err)
	}
	if total != 1 {
		t.Fatalf("recoverable deferred count: want 1 deferred row, got %d", total)
	}
	goatIDs, err := vacc.ListRecoverableDeferredVaccinationGoatIDs(ctx, impTenant, olderThan, 10)
	if err != nil {
		t.Fatalf("list recoverable deferred goats: %v", err)
	}
	if len(goatIDs) != 1 || goatIDs[0] != deferredGoat {
		t.Fatalf("recoverable deferred goats=%v, want only %s", goatIDs, deferredGoat)
	}
}

// TestGoatCreatedAcceptedCompletionSuppressesMatchingObligation proves that an ACCEPTED+VERIFIED
// Goat OS administration for the same protocol+dose suppresses re-generation of that dose, while a
// merely RECORDED or accepted-without-verified_at completion does NOT — so version changes / catch-up
// cannot double-issue a dose the goat already received, without over-suppressing untrusted work.
func TestGoatCreatedAcceptedCompletionSuppressesMatchingObligation(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.goatos.suppress", Name: "GoatOS Suppress", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	ruleDSL := []byte(`{"eligibility":{}}`)
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: ruleDSL, ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleID, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "post_arrival", OffsetDays: 7, Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}
	if err := proto.PublishVersion(ctx, impTenant, versionID, nil); err != nil {
		t.Fatalf("publish: %v", err)
	}

	acceptedGoat := "30000000-0000-4000-8000-0000000000e1"
	recordedGoat := "30000000-0000-4000-8000-0000000000e2"
	unverifiedGoat := "30000000-0000-4000-8000-0000000000e3"
	entryDate := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	for _, g := range []string{acceptedGoat, recordedGoat, unverifiedGoat} {
		seedGenAdultProcuredGoat(t, ctx, pool, g, "alive", entryDate)
	}

	administered := time.Date(2026, 6, 12, 8, 0, 0, 0, time.UTC)
	seedGoatOSCompletion(t, ctx, pool, obl, versionID, ruleID, acceptedGoat, "accepted", administered)
	seedGoatOSCompletion(t, ctx, pool, obl, versionID, ruleID, recordedGoat, "recorded", administered)
	seedGoatOSCompletion(t, ctx, pool, obl, versionID, ruleID, unverifiedGoat, "accepted_unverified", administered)

	gen := vaccapp.NewGenerationService(proto, vacc, obl)
	asOf := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)

	acceptedRes, err := gen.GenerateForGoat(ctx, impTenant, acceptedGoat, asOf)
	if err != nil {
		t.Fatalf("generate accepted goat: %v", err)
	}
	if acceptedRes.Generated != 0 || acceptedRes.SuppressedByTrustedHistory != 1 {
		t.Fatalf("accepted result = %#v, want generated 0 suppressed 1", acceptedRes)
	}

	recordedRes, err := gen.GenerateForGoat(ctx, impTenant, recordedGoat, asOf)
	if err != nil {
		t.Fatalf("generate recorded goat: %v", err)
	}
	if recordedRes.Generated != 1 || recordedRes.SuppressedByTrustedHistory != 0 {
		t.Fatalf("recorded result = %#v, want generated 1 suppressed 0 (only accepted history suppresses)", recordedRes)
	}

	unverifiedRes, err := gen.GenerateForGoat(ctx, impTenant, unverifiedGoat, asOf)
	if err != nil {
		t.Fatalf("generate unverified goat: %v", err)
	}
	if unverifiedRes.Generated != 1 || unverifiedRes.SuppressedByTrustedHistory != 0 {
		t.Fatalf("unverified result = %#v, want generated 1 suppressed 0 (accepted requires verified_at)", unverifiedRes)
	}
}

func TestRecentVaccineAdministrationsRespectsVerificationAsOf(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.goatos.history_asof", Name: "GoatOS History As-Of", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{"eligibility":{}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleID, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "post_arrival", OffsetDays: 7, Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}
	if err := proto.PublishVersion(ctx, impTenant, versionID, nil); err != nil {
		t.Fatalf("publish: %v", err)
	}

	goatID := "30000000-0000-4000-8000-0000000000f1"
	seedGenAdultProcuredGoat(t, ctx, pool, goatID, "alive", time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC))
	administered := time.Date(2026, 6, 12, 8, 0, 0, 0, time.UTC)
	futureVerifiedAt := time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC)
	seedGoatOSCompletionWithVerifiedAt(t, ctx, pool, obl, versionID, ruleID, goatID, "accepted", administered, &futureVerifiedAt)

	beforeReview := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	early, err := vacc.RecentVaccineAdministrationsForGoats(ctx, impTenant, []string{goatID}, beforeReview)
	if err != nil {
		t.Fatalf("recent before verification: %v", err)
	}
	if got := len(early[goatID]); got != 0 {
		t.Fatalf("recent before verification rows=%d, want 0 so generation cannot use future-verified history", got)
	}

	afterReview := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	late, err := vacc.RecentVaccineAdministrationsForGoats(ctx, impTenant, []string{goatID}, afterReview)
	if err != nil {
		t.Fatalf("recent after verification: %v", err)
	}
	if got := len(late[goatID]); got != 1 {
		t.Fatalf("recent after verification rows=%d, want 1", got)
	}
}

func TestGoatCreatedAcceptedCompletionDoesNotSuppressDifferentCalendarCycle(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.goatos.sequence", Name: "GoatOS Sequence",
		Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	v1End := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	v1, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), EffectiveTo: &v1End,
		RuleDsl: []byte(`{"eligibility":{}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create v1: %v", err)
	}
	v1Rule, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: v1, DoseCode: "primary", Sequence: 1,
		TriggerType: "post_arrival", OffsetDays: 7, Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create v1 rule: %v", err)
	}
	if err := proto.PublishVersion(ctx, impTenant, v1, nil); err != nil {
		t.Fatalf("publish v1: %v", err)
	}
	v2, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 2, Status: "draft",
		EffectiveFrom: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{"eligibility":{}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create v2: %v", err)
	}
	if _, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: v2, DoseCode: "primary", Sequence: 2,
		TriggerType: "post_arrival", OffsetDays: 30, Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	}); err != nil {
		t.Fatalf("create v2 rule: %v", err)
	}
	if err := proto.PublishVersion(ctx, impTenant, v2, nil); err != nil {
		t.Fatalf("publish v2: %v", err)
	}

	goatID := "30000000-0000-4000-8000-0000000000e4"
	entryDate := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	seedGenAdultProcuredGoat(t, ctx, pool, goatID, "alive", entryDate)
	seedGoatOSCompletion(t, ctx, pool, obl, v1, v1Rule, goatID, "accepted", time.Date(2026, 6, 17, 8, 0, 0, 0, time.UTC))

	res, err := vaccapp.NewGenerationService(proto, vacc, obl).GenerateForGoat(ctx, impTenant, goatID, time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if res.Generated != 1 || res.SuppressedByTrustedHistory != 0 {
		t.Fatalf("result = %#v, want sequence-2 obligation generated and not suppressed by sequence-1 history", res)
	}
	if got := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND protocol_version_id=$3 AND "sequence"=2`, impTenant, goatID, v2); got != 1 {
		t.Fatalf("sequence-2 obligations = %d, want 1", got)
	}
}

func TestTrustedCompletionEvidenceUsesISTForRepeatCycle(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.goatos.repeat_tz", Name: "GoatOS Repeat TZ",
		Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{"eligibility":{}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleID, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "post_arrival", OffsetDays: 7, Repeat: "yearly", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}

	goatID := "30000000-0000-4000-8000-0000000000e5"
	seedGenAdultProcuredGoat(t, ctx, pool, goatID, "alive", time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC))
	existingDue := time.Date(2026, 6, 29, 0, 30, 0, 0, time.UTC)
	obID, applied, err := obl.InsertObligation(ctx, obldomain.NewObligation{
		TenantID: impTenant, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: goatID, ScopeType: "tenant", ScopeID: impTenant,
		DueAt: existingDue, Status: "completed", IdempotencyKey: "prior-repeat-tz:" + goatID, Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("seed repeat obligation: applied=%v err=%v", applied, err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_completions (tenant_id, obligation_id, goat_id, doses, administered_at, status, verified_at, idempotency_key)
VALUES ($1::uuid, $2::uuid, $3::uuid, 1, $4::timestamptz, 'accepted', $5::timestamptz, $6)`,
		impTenant, obID, goatID, existingDue, existingDue.Add(30*time.Minute), "compl-repeat-tz:"+goatID); err != nil {
		t.Fatalf("seed repeat completion: %v", err)
	}

	candidate := vaccdomain.TrustedCompletionCandidate{
		GoatID:   goatID,
		RuleID:   ruleID,
		DoseCode: "primary",
		DueAt:    time.Date(2026, 6, 28, 20, 0, 0, 0, time.UTC),
		Repeat:   "yearly",
	}
	generationAt := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)

	if _, err := pool.Exec(ctx, `UPDATE locations SET timezone = 'UTC' WHERE tenant_id = $1 AND location_id = $2`, impTenant, impCbe); err != nil {
		t.Fatalf("set UTC location timezone: %v", err)
	}
	hits, err := vacc.HasTrustedCompletionEvidenceBatch(ctx, impTenant, versionID, []vaccdomain.TrustedCompletionCandidate{candidate}, generationAt)
	if err != nil {
		t.Fatalf("trusted evidence UTC: %v", err)
	}
	if !hits[candidate.Key()] {
		t.Fatalf("UTC location should still match repeat cycles that share the IST business date")
	}

	if _, err := pool.Exec(ctx, `UPDATE locations SET timezone = 'Asia/Kolkata' WHERE tenant_id = $1 AND location_id = $2`, impTenant, impCbe); err != nil {
		t.Fatalf("set IST location timezone: %v", err)
	}
	hits, err = vacc.HasTrustedCompletionEvidenceBatch(ctx, impTenant, versionID, []vaccdomain.TrustedCompletionCandidate{candidate}, generationAt)
	if err != nil {
		t.Fatalf("trusted evidence IST: %v", err)
	}
	if !hits[candidate.Key()] {
		t.Fatalf("IST location should match repeat cycles that share the IST business date")
	}
}

// TestGenerateScopesObligationToShedLocation proves a goat assigned to a real shed location gets a
// shed-scoped obligation, so the SM-4 sweeper batches one vaccination drive per shed.
func TestGenerateScopesObligationToShedLocation(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.shed.scope", Name: "Shed Scope", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{"eligibility":{}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if _, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "post_arrival", OffsetDays: 7, Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	}); err != nil {
		t.Fatalf("rule: %v", err)
	}
	if err := proto.PublishVersion(ctx, impTenant, versionID, nil); err != nil {
		t.Fatalf("publish: %v", err)
	}

	shedID := "00000000-0000-4000-8000-00000000f001"
	if _, err := pool.Exec(ctx,
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
		 VALUES ($1, $2, 'shed', 'SHEDT1', 'Test Shed 1', 'active')`, shedID, impTenant); err != nil {
		t.Fatalf("shed location: %v", err)
	}
	goatID := "30000000-0000-4000-8000-0000000000f1"
	entryDate := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	seedGenAdultProcuredGoat(t, ctx, pool, goatID, "alive", entryDate)
	if _, err := pool.Exec(ctx,
		`UPDATE goats SET shed_id=$3::uuid, current_location_id=$3::uuid
		 WHERE tenant_id=$1 AND goat_id=$2`, impTenant, goatID, shedID); err != nil {
		t.Fatalf("set shed: %v", err)
	}

	gen := vaccapp.NewGenerationService(proto, vacc, obl)
	res, err := gen.GenerateForGoat(ctx, impTenant, goatID, time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if res.Generated != 1 {
		t.Fatalf("want 1 generated, got %#v", res)
	}

	var scopeType, scopeID string
	if err := pool.QueryRow(ctx,
		`SELECT scope_type, scope_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`,
		impTenant, goatID).Scan(&scopeType, &scopeID); err != nil {
		t.Fatalf("scan scope: %v", err)
	}
	if scopeType != "shed" || scopeID != shedID {
		t.Fatalf("obligation scope = %s/%s, want shed/%s (one drive per shed)", scopeType, scopeID, shedID)
	}
}

// seedGoatOSCompletion creates a prior obligation (tenant-scoped, scope irrelevant to suppression)
// and a vaccination_completions row for it in the given status, simulating a goat's Goat OS
// administration history.
func seedGoatOSCompletion(t *testing.T, ctx context.Context, pool *pgxpool.Pool, obl *oblpg.Repository, versionID, ruleID, goatID, status string, administered time.Time) {
	t.Helper()
	var verifiedAt *time.Time
	if status == "accepted" {
		acceptedAt := administered.Add(2 * time.Hour)
		verifiedAt = &acceptedAt
	}
	seedGoatOSCompletionWithVerifiedAt(t, ctx, pool, obl, versionID, ruleID, goatID, status, administered, verifiedAt)
}

func seedGoatOSCompletionWithVerifiedAt(t *testing.T, ctx context.Context, pool *pgxpool.Pool, obl *oblpg.Repository, versionID, ruleID, goatID, status string, administered time.Time, verifiedAt *time.Time) {
	t.Helper()
	dbStatus := status
	var verifiedAtParam any
	if verifiedAt != nil {
		verifiedAtParam = *verifiedAt
	}
	if status == "accepted_unverified" {
		dbStatus = "accepted"
		verifiedAtParam = nil
	}
	obID, applied, err := obl.InsertObligation(ctx, obldomain.NewObligation{
		TenantID: impTenant, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: goatID, ScopeType: "tenant", ScopeID: impTenant,
		DueAt: administered.Add(48 * time.Hour), Status: "completed", IdempotencyKey: "prior:" + goatID, Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("seed prior obligation: applied=%v err=%v", applied, err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_completions (tenant_id, obligation_id, goat_id, doses, administered_at, status, verified_at, idempotency_key)
VALUES ($1::uuid, $2::uuid, $3::uuid, 1, $4::timestamptz, $5, $6::timestamptz, $7)`,
		impTenant, obID, goatID, administered, dbStatus, verifiedAtParam, "compl:"+goatID); err != nil {
		t.Fatalf("seed completion: %v", err)
	}
}

// TestGenerationRecordsStaleKidStageReviewItem is the VACC-REV-10 guard: a goat past the 20-week kid
// cutoff that still carries a K1/K2 tag creates exactly ONE identifiable, goat-scoped review item
// (reason + observed stage/age), and a replayed generation pass creates no duplicate.
func TestGenerationRecordsStaleKidStageReviewItem(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.stale.review", Name: "StaleReview", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	ruleDSL := []byte(`{"eligibility":{"animal_stage":"K1"},"source":{"source_system":"pc","source_ref":"PC §6","review_status":"approved","approved_by":"Reviewer"}}`)
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), RuleDsl: ruleDSL, ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if _, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", OffsetDays: 21, Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	}); err != nil {
		t.Fatalf("rule: %v", err)
	}
	if err := proto.PublishVersion(ctx, impTenant, versionID, nil); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// K1-tagged goat born 2026-05-01, evaluated 2026-11-01 (~26 weeks) — well past the 20-week cutoff.
	const staleGoat = "30000000-0000-4000-8000-0000000000f7"
	seedGenGoatWithStage(t, ctx, pool, staleGoat, "alive", "K1")
	gen := vaccapp.NewGenerationService(proto, vacc, obl)
	asOf := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	if _, err := gen.GenerateForVersion(ctx, impTenant, versionID, asOf); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if got := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM vaccination_stage_review_items WHERE tenant_id=$1 AND goat_id=$2`, impTenant, staleGoat); got != 1 {
		t.Fatalf("stale K-stage goat review items = %d, want exactly 1", got)
	}
	var reason, stage string
	var ageWeeks int
	if err := pool.QueryRow(ctx, `SELECT reason, observed_stage, observed_age_weeks FROM vaccination_stage_review_items WHERE tenant_id=$1 AND goat_id=$2`, impTenant, staleGoat).Scan(&reason, &stage, &ageWeeks); err != nil {
		t.Fatalf("read review item: %v", err)
	}
	if reason != "kid_stage_past_age_cutoff" || stage != "K1" || ageWeeks < 21 {
		t.Fatalf("review item reason=%q stage=%q ageWeeks=%d, want kid_stage_past_age_cutoff/K1/>20w", reason, stage, ageWeeks)
	}

	// Replayed generation must not duplicate the review item.
	if _, err := gen.GenerateForVersion(ctx, impTenant, versionID, asOf); err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	if got := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM vaccination_stage_review_items WHERE tenant_id=$1 AND goat_id=$2`, impTenant, staleGoat); got != 1 {
		t.Fatalf("after replay review items = %d, want still exactly 1 (idempotent)", got)
	}
}

// TestStageReviewItemLifecycle is the VACC-REV-10 lifecycle guard: create, replay-dedupe (open),
// operator listing, resolution, idempotent re-resolve, and a NEW open occurrence after resolution.
func TestStageReviewItemLifecycle(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	const goatID = "40000000-0000-4000-8000-0000000000c1"
	const key = "vacc-stage-review:t:g:K1"
	resolver := "40000000-0000-4000-8000-0000000000ff"

	// create + replay dedupe (open).
	for i := 0; i < 2; i++ {
		if err := repo.RecordStageReviewItem(ctx, impTenant, goatID, "kid_stage_past_age_cutoff", "K1", 26, 20, key); err != nil {
			t.Fatalf("record #%d: %v", i, err)
		}
	}
	openPage, err := repo.ListOpenStageReviewItems(ctx, impTenant, nil, 50)
	if err != nil {
		t.Fatalf("list open: %v", err)
	}
	if len(openPage.Items) != 1 {
		t.Fatalf("open items = %d, want 1 (replay deduped)", len(openPage.Items))
	}
	it := openPage.Items[0]
	if it.GoatID != goatID || it.Reason != "kid_stage_past_age_cutoff" || it.ObservedStage != "K1" || it.ObservedAgeWeeks != 26 || it.Status != "open" {
		t.Fatalf("unexpected review item: %#v", it)
	}

	// resolve, then list shows none open; re-resolve is an idempotent no-op.
	resolved, err := repo.ResolveStageReviewItem(ctx, impTenant, it.ReviewItemID, resolver, "tag corrected to adult", "corrected", time.Now())
	if err != nil || !resolved {
		t.Fatalf("resolve: resolved=%v err=%v", resolved, err)
	}
	openAfterResolve, _ := repo.ListOpenStageReviewItems(ctx, impTenant, nil, 50)
	if len(openAfterResolve.Items) != 0 {
		t.Fatalf("open after resolve = %d, want 0", len(openAfterResolve.Items))
	}
	if again, err := repo.ResolveStageReviewItem(ctx, impTenant, it.ReviewItemID, resolver, "", "corrected", time.Now()); err != nil || again {
		t.Fatalf("re-resolve should be a no-op: again=%v err=%v", again, err)
	}

	// recurrence AFTER resolution opens a NEW actionable occurrence (not swallowed by the key).
	if err := repo.RecordStageReviewItem(ctx, impTenant, goatID, "kid_stage_past_age_cutoff", "K1", 40, 20, key); err != nil {
		t.Fatalf("recurrence record: %v", err)
	}
	openAfterPage, err := repo.ListOpenStageReviewItems(ctx, impTenant, nil, 50)
	if err != nil {
		t.Fatalf("list after recurrence: %v", err)
	}
	if len(openAfterPage.Items) != 1 || openAfterPage.Items[0].ReviewItemID == it.ReviewItemID {
		t.Fatalf("recurrence: want 1 NEW open item distinct from the resolved one, got %#v", openAfterPage.Items)
	}
	total := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM vaccination_stage_review_items WHERE tenant_id=$1 AND goat_id=$2`, impTenant, goatID)
	if total != 2 {
		t.Fatalf("total rows = %d, want 2 (one resolved + one new open)", total)
	}
}

// TestStageReviewItemOpenUniquePerGoat is the VACC-REV-10 defect + rollout-compat guard: the bridge
// writer keeps exactly ONE open review item per goat, updated in place, REGARDLESS of the idempotency
// key. It records the goat with two DIFFERENT keys (as a stage-suffixed old-binary writer would emit)
// and asserts a single open row — proving the writer depends on the (tenant, goat) identity via its
// advisory lock + update-else-insert, not on any ON CONFLICT target / unique index. That is what makes
// it migrate-first-safe across releases that shipped different conflict targets.
func TestStageReviewItemOpenUniquePerGoat(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	const goatID = "40000000-0000-4000-8000-0000000000d2"

	// Two DIFFERENT keys, as a stage-suffixed old-binary writer emitted for K1 vs K2.
	if err := repo.RecordStageReviewItem(ctx, impTenant, goatID, "kid_stage_past_age_cutoff", "K1", 22, 20, "vacc-stage-review:"+impTenant+":"+goatID+":K1"); err != nil {
		t.Fatalf("record K1: %v", err)
	}
	if err := repo.RecordStageReviewItem(ctx, impTenant, goatID, "kid_stage_past_age_cutoff", "K2", 30, 20, "vacc-stage-review:"+impTenant+":"+goatID+":K2"); err != nil {
		t.Fatalf("record K2: %v", err)
	}

	openPage, err := repo.ListOpenStageReviewItems(ctx, impTenant, nil, 50)
	if err != nil {
		t.Fatalf("list open: %v", err)
	}
	if len(openPage.Items) != 1 {
		t.Fatalf("open items = %d, want 1 (one open item per goat regardless of stage)", len(openPage.Items))
	}
	it := openPage.Items[0]
	if it.ObservedStage != "K2" || it.ObservedAgeWeeks != 30 {
		t.Fatalf("open item stage=%q age=%d, want K2/30 (latest observed, updated in place)", it.ObservedStage, it.ObservedAgeWeeks)
	}
	total := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM vaccination_stage_review_items WHERE tenant_id=$1 AND goat_id=$2`, impTenant, goatID)
	if total != 1 {
		t.Fatalf("total rows = %d, want 1 (single open item, no stage-keyed duplicate)", total)
	}
}

// TestStageReviewOpenUniqueRejectsDuplicate is the VACC-REV-10 round-5 migrate-first + DB-level guard:
// migration 000210 KEEPS the (tenant, idempotency_key) open index AND adds a (tenant, goat) open index,
// so a still-live predecessor binary's `ON CONFLICT (tenant_id, idempotency_key)` writes keep working
// for the FIRST row, while only an actual SECOND stage-keyed open row for the same goat fails closed.
// Proven here: (1) predecessor FIRST insert (fresh goat, key K1) succeeds — the idem index is present;
// (2) predecessor SAME-key replay succeeds (DO UPDATE, still one row); (3) predecessor DIFFERENT-stage
// key for the same goat is rejected by the (tenant, goat) unique index (fail closed, no dup);
// (4) exactly one open row survives and the bridge writer updates it in place.
func TestStageReviewOpenUniqueRejectsDuplicate(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	const goatID = "40000000-0000-4000-8000-0000000000d3"

	// Exact SQL the old stage-keyed predecessor shipped (ON CONFLICT on the idempotency-key index).
	oldWriterSQL := `
		INSERT INTO vaccination_stage_review_items
			(review_item_id, tenant_id, goat_id, reason, observed_stage, observed_age_weeks, idempotency_key)
		VALUES (gen_random_uuid(), $1::uuid, $2::uuid, 'kid_stage_past_age_cutoff', $3, $4, $5)
		ON CONFLICT (tenant_id, idempotency_key) WHERE status = 'open' DO UPDATE
		SET observed_stage = EXCLUDED.observed_stage, observed_age_weeks = EXCLUDED.observed_age_weeks`

	// (1) predecessor FIRST insert (fresh goat) SUCCEEDS — the idempotency-key index is retained.
	if _, err := pool.Exec(ctx, oldWriterSQL, impTenant, goatID, "K1", 22, "vacc-stage-review:"+impTenant+":"+goatID+":K1"); err != nil {
		t.Fatalf("predecessor first insert should succeed (idem index kept): %v", err)
	}
	// (2) predecessor SAME-key replay SUCCEEDS (DO UPDATE, still one open row).
	if _, err := pool.Exec(ctx, oldWriterSQL, impTenant, goatID, "K1", 23, "vacc-stage-review:"+impTenant+":"+goatID+":K1"); err != nil {
		t.Fatalf("predecessor same-key replay should succeed: %v", err)
	}
	if open := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM vaccination_stage_review_items WHERE tenant_id=$1 AND goat_id=$2 AND status='open'`, impTenant, goatID); open != 1 {
		t.Fatalf("open rows after predecessor first+replay = %d, want 1", open)
	}

	// (3) predecessor DIFFERENT-stage key for the SAME goat is rejected by the (tenant, goat) index.
	if _, err := pool.Exec(ctx, oldWriterSQL, impTenant, goatID, "K2", 30, "vacc-stage-review:"+impTenant+":"+goatID+":K2"); err == nil {
		t.Fatalf("predecessor different-stage-key duplicate should violate the (tenant, goat) open-unique index, got no error")
	}

	// (4) exactly one open row survives; the bridge writer updates it in place on its next pass.
	if open := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM vaccination_stage_review_items WHERE tenant_id=$1 AND goat_id=$2 AND status='open'`, impTenant, goatID); open != 1 {
		t.Fatalf("open rows = %d, want 1 (duplicate failed closed)", open)
	}
	if err := repo.RecordStageReviewItem(ctx, impTenant, goatID, "kid_stage_past_age_cutoff", "K2", 31, 20, "vacc-stage-review:"+impTenant+":"+goatID); err != nil {
		t.Fatalf("bridge writer K2: %v", err)
	}
	it := func() (stage string, age int) {
		row := pool.QueryRow(ctx, `SELECT observed_stage, observed_age_weeks FROM vaccination_stage_review_items WHERE tenant_id=$1::uuid AND goat_id=$2::uuid AND status='open'`, impTenant, goatID)
		if err := row.Scan(&stage, &age); err != nil {
			t.Fatalf("load open row: %v", err)
		}
		return
	}
	if stage, age := it(); stage != "K2" || age != 31 {
		t.Fatalf("open row stage=%q age=%d, want K2/31 (updated in place)", stage, age)
	}
	if total := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM vaccination_stage_review_items WHERE tenant_id=$1 AND goat_id=$2`, impTenant, goatID); total != 1 {
		t.Fatalf("total rows = %d, want 1 (no duplicate ever created)", total)
	}
}

// TestStageReviewMigration211RepairsPreviouslyApplied210 exercises the real upgrade path, not a
// fresh database. The originally released 000210 dropped the predecessor writer's idempotency-key
// conflict target. Recreate that deployed state, execute the committed 000211 Up SQL, and prove the
// predecessor can write/replay while the per-goat index still rejects a stage-keyed duplicate.
func TestStageReviewMigration211RepairsPreviouslyApplied210(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	// Simulate an environment that already ran the originally shipped 000210.
	if _, err := pool.Exec(ctx, `DROP INDEX IF EXISTS vaccination_stage_review_items_open_idem_unique`); err != nil {
		t.Fatalf("simulate original 000210: %v", err)
	}
	var absent bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('vaccination_stage_review_items_open_idem_unique') IS NULL`).Scan(&absent); err != nil {
		t.Fatalf("check simulated index state: %v", err)
	}
	if !absent {
		t.Fatal("precondition: idempotency index should be absent after simulated original 000210")
	}

	migrationPath := filepath.Join("..", "..", "..", "..", "migrations", "postgres", "000211_restore_vaccination_stage_review_idem_index.sql")
	raw, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("read 000211 migration: %v", err)
	}
	sections := strings.SplitN(string(raw), "-- +goose Down", 2)
	if len(sections) != 2 {
		t.Fatal("000211 migration is missing its goose Down marker")
	}
	upSQL := strings.TrimPrefix(sections[0], "-- +goose Up")
	if _, err := pool.Exec(ctx, upSQL); err != nil {
		t.Fatalf("apply 000211 Up: %v", err)
	}

	var idemPresent, goatPresent bool
	if err := pool.QueryRow(ctx, `
		SELECT to_regclass('vaccination_stage_review_items_open_idem_unique') IS NOT NULL,
		       to_regclass('vaccination_stage_review_items_open_goat_unique') IS NOT NULL`).Scan(&idemPresent, &goatPresent); err != nil {
		t.Fatalf("check repaired indexes: %v", err)
	}
	if !idemPresent || !goatPresent {
		t.Fatalf("repaired indexes: idem=%v goat=%v, want both true", idemPresent, goatPresent)
	}

	const goatID = "40000000-0000-4000-8000-0000000000d4"
	oldWriterSQL := `
		INSERT INTO vaccination_stage_review_items
			(review_item_id, tenant_id, goat_id, reason, observed_stage, observed_age_weeks, idempotency_key)
		VALUES (gen_random_uuid(), $1::uuid, $2::uuid, 'kid_stage_past_age_cutoff', $3, $4, $5)
		ON CONFLICT (tenant_id, idempotency_key) WHERE status = 'open' DO UPDATE
		SET observed_stage = EXCLUDED.observed_stage, observed_age_weeks = EXCLUDED.observed_age_weeks`
	keyK1 := "vacc-stage-review:" + impTenant + ":" + goatID + ":K1"
	if _, err := pool.Exec(ctx, oldWriterSQL, impTenant, goatID, "K1", 22, keyK1); err != nil {
		t.Fatalf("predecessor first insert after 000211: %v", err)
	}
	if _, err := pool.Exec(ctx, oldWriterSQL, impTenant, goatID, "K1", 23, keyK1); err != nil {
		t.Fatalf("predecessor replay after 000211: %v", err)
	}
	keyK2 := "vacc-stage-review:" + impTenant + ":" + goatID + ":K2"
	if _, err := pool.Exec(ctx, oldWriterSQL, impTenant, goatID, "K2", 30, keyK2); err == nil {
		t.Fatal("predecessor different-stage duplicate should fail against per-goat index")
	}
	if got := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM vaccination_stage_review_items WHERE tenant_id=$1 AND goat_id=$2 AND status='open'`, impTenant, goatID); got != 1 {
		t.Fatalf("open rows after repaired upgrade path = %d, want 1", got)
	}
}

// TestResolveStageReviewItemCorrectedAtomicReverify is the VACC-REV-10 guard for the atomic, fail-closed
// 'corrected' re-check: a still-stale goat is NOT resolved, a goat whose stage is advanced IS resolved,
// and a review item whose goat no longer exists fails closed (not resolved). The re-check runs against
// the goat's live state in one locked statement.
func TestResolveStageReviewItemCorrectedAtomicReverify(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	vacc := NewRepository(pool, 5*time.Second)

	const goatID = "30000000-0000-4000-8000-0000000000f8"
	const actor = "40000000-0000-4000-8000-0000000000ff"
	seedGenGoatWithStage(t, ctx, pool, goatID, "alive", "K1")
	// The re-check uses now(); make the goat ~30 weeks old so it is genuinely past the 20-week cutoff.
	if _, err := pool.Exec(ctx, `UPDATE goats SET dob = (now() AT TIME ZONE 'Asia/Kolkata')::date - 210 WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`, impTenant, goatID); err != nil {
		t.Fatalf("age goat: %v", err)
	}

	// Persist cutoff 20 on the item (the policy that raised it); the re-check reads this, not a re-derived one.
	if err := vacc.RecordStageReviewItem(ctx, impTenant, goatID, "kid_stage_past_age_cutoff", "K1", 30, 20, "vacc-stage-review:"+impTenant+":"+goatID); err != nil {
		t.Fatalf("record: %v", err)
	}
	var reviewID string
	if err := pool.QueryRow(ctx, `SELECT review_item_id::text FROM vaccination_stage_review_items WHERE tenant_id=$1::uuid AND goat_id=$2::uuid AND status='open'`, impTenant, goatID).Scan(&reviewID); err != nil {
		t.Fatalf("load review id: %v", err)
	}

	// 1) still stale (K1 + past the item's stored cutoff) -> not resolved, but was open.
	resolved, wasOpen, err := vacc.ResolveStageReviewItemCorrected(ctx, impTenant, reviewID, actor, "claims fixed", time.Now())
	if err != nil {
		t.Fatalf("resolve #1: %v", err)
	}
	if resolved || !wasOpen {
		t.Fatalf("still-stale: resolved=%v wasOpen=%v, want false/true", resolved, wasOpen)
	}

	// 2) advance the stage off K, then corrected -> resolved.
	if _, err := pool.Exec(ctx, `UPDATE goats SET management_stage='adult' WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`, impTenant, goatID); err != nil {
		t.Fatalf("advance stage: %v", err)
	}
	resolved, wasOpen, err = vacc.ResolveStageReviewItemCorrected(ctx, impTenant, reviewID, actor, "advanced to adult", time.Now())
	if err != nil {
		t.Fatalf("resolve #2: %v", err)
	}
	if !resolved {
		t.Fatalf("after-fix: resolved=%v, want true", resolved)
	}

	// 3) fail closed when the goat is missing: an item for a non-existent goat is NOT resolved.
	const ghost = "30000000-0000-4000-8000-0000000000f9"
	if err := vacc.RecordStageReviewItem(ctx, impTenant, ghost, "kid_stage_past_age_cutoff", "K1", 30, 20, "vacc-stage-review:"+impTenant+":"+ghost); err != nil {
		t.Fatalf("record ghost: %v", err)
	}
	var ghostID string
	if err := pool.QueryRow(ctx, `SELECT review_item_id::text FROM vaccination_stage_review_items WHERE tenant_id=$1::uuid AND goat_id=$2::uuid AND status='open'`, impTenant, ghost).Scan(&ghostID); err != nil {
		t.Fatalf("load ghost review id: %v", err)
	}
	resolved, wasOpen, err = vacc.ResolveStageReviewItemCorrected(ctx, impTenant, ghostID, actor, "x", time.Now())
	if err != nil {
		t.Fatalf("resolve #3: %v", err)
	}
	if resolved || !wasOpen {
		t.Fatalf("missing-goat: resolved=%v wasOpen=%v, want false/true (fail closed)", resolved, wasOpen)
	}
}

// TestResolveStageReviewItemCorrectedUsesStoredCutoff proves the 'corrected' re-check evaluates against
// the cutoff PERSISTED on each item (age_cutoff_weeks), not a value re-derived across published versions
// (the MIN-across-versions defect). Two goats identical in age and stage (both K1, ~22 weeks old) differ
// ONLY in the cutoff stored on their review item: one at 30 weeks (age is under it -> no longer stale ->
// resolves) and one at 20 weeks (age is over it -> still stale -> blocked). Opposite outcomes from the
// same goat state = the stored cutoff is the deciding input.
func TestResolveStageReviewItemCorrectedUsesStoredCutoff(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	vacc := NewRepository(pool, 5*time.Second)

	const actor = "40000000-0000-4000-8000-0000000000ff"
	// Both goats: alive, K1, ~22 weeks old (154 days) — past a 20w cutoff but under a 30w cutoff.
	seed := func(goatID string, storedCutoff int) string {
		seedGenGoatWithStage(t, ctx, pool, goatID, "alive", "K1")
		if _, err := pool.Exec(ctx, `UPDATE goats SET dob = (now() AT TIME ZONE 'Asia/Kolkata')::date - 154 WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`, impTenant, goatID); err != nil {
			t.Fatalf("age goat %s: %v", goatID, err)
		}
		if err := vacc.RecordStageReviewItem(ctx, impTenant, goatID, "kid_stage_past_age_cutoff", "K1", 22, storedCutoff, "vacc-stage-review:"+impTenant+":"+goatID); err != nil {
			t.Fatalf("record %s: %v", goatID, err)
		}
		var id string
		if err := pool.QueryRow(ctx, `SELECT review_item_id::text FROM vaccination_stage_review_items WHERE tenant_id=$1::uuid AND goat_id=$2::uuid AND status='open'`, impTenant, goatID).Scan(&id); err != nil {
			t.Fatalf("load review id %s: %v", goatID, err)
		}
		return id
	}

	// Higher stored cutoff (30w): the 22w goat is UNDER it -> no longer stale -> corrected resolves,
	// with NO stage change. A MIN-derived 20w cutoff would (wrongly) block this.
	highID := seed("30000000-0000-4000-8000-0000000000fa", 30)
	resolved, wasOpen, err := vacc.ResolveStageReviewItemCorrected(ctx, impTenant, highID, actor, "under stored cutoff", time.Now())
	if err != nil {
		t.Fatalf("resolve high-cutoff: %v", err)
	}
	if !resolved || !wasOpen {
		t.Fatalf("high-cutoff (30w vs 22w age): resolved=%v wasOpen=%v, want true/true (stored cutoff honored)", resolved, wasOpen)
	}

	// Lower stored cutoff (20w): identical goat state, but 22w is OVER it -> still stale -> blocked.
	lowID := seed("30000000-0000-4000-8000-0000000000fb", 20)
	resolved, wasOpen, err = vacc.ResolveStageReviewItemCorrected(ctx, impTenant, lowID, actor, "over stored cutoff", time.Now())
	if err != nil {
		t.Fatalf("resolve low-cutoff: %v", err)
	}
	if resolved || !wasOpen {
		t.Fatalf("low-cutoff (20w vs 22w age): resolved=%v wasOpen=%v, want false/true (still stale)", resolved, wasOpen)
	}
}

// TestResolveStageReviewItemCorrectedNullCutoffFailsClosed proves a legacy / predecessor-written row
// (age_cutoff_weeks IS NULL — no persisted policy provenance) can NOT be resolved as 'corrected' by
// defaulting to an invented cutoff. A 15-week K1 goat raised under an unknown cutoff would (wrongly)
// look non-stale against a fabricated 20-week fallback and close as corrected without any real fix.
// With NULL treated as unknown/fail-closed, the item stays open (409) until generation re-records the
// exact cutoff or an operator resolves it as an explicit exception.
func TestResolveStageReviewItemCorrectedNullCutoffFailsClosed(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	vacc := NewRepository(pool, 5*time.Second)

	const goatID = "30000000-0000-4000-8000-0000000000fc"
	const actor = "40000000-0000-4000-8000-0000000000ff"
	seedGenGoatWithStage(t, ctx, pool, goatID, "alive", "K1")
	// ~15 weeks old (105 days): past a short cutoff (e.g. 12w) but under the 20w fallback the buggy
	// COALESCE would invent.
	if _, err := pool.Exec(ctx, `UPDATE goats SET dob = (now() AT TIME ZONE 'Asia/Kolkata')::date - 105 WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`, impTenant, goatID); err != nil {
		t.Fatalf("age goat: %v", err)
	}
	// Legacy/predecessor row: raw insert with age_cutoff_weeks left NULL.
	if _, err := pool.Exec(ctx, `
		INSERT INTO vaccination_stage_review_items
			(review_item_id, tenant_id, goat_id, reason, observed_stage, observed_age_weeks, idempotency_key)
		VALUES (gen_random_uuid(), $1::uuid, $2::uuid, 'kid_stage_past_age_cutoff', 'K1', 15, $3)`,
		impTenant, goatID, "vacc-stage-review:"+impTenant+":"+goatID); err != nil {
		t.Fatalf("seed legacy null-cutoff row: %v", err)
	}
	var reviewID string
	if err := pool.QueryRow(ctx, `SELECT review_item_id::text FROM vaccination_stage_review_items WHERE tenant_id=$1::uuid AND goat_id=$2::uuid AND status='open'`, impTenant, goatID).Scan(&reviewID); err != nil {
		t.Fatalf("load review id: %v", err)
	}

	// Unchanged goat data + NULL cutoff -> must NOT resolve as corrected; stays open (409).
	resolved, wasOpen, err := vacc.ResolveStageReviewItemCorrected(ctx, impTenant, reviewID, actor, "claims fixed", time.Now())
	if err != nil {
		t.Fatalf("resolve null-cutoff: %v", err)
	}
	if resolved || !wasOpen {
		t.Fatalf("null-cutoff: resolved=%v wasOpen=%v, want false/true (fail closed, no invented cutoff)", resolved, wasOpen)
	}

	// Once generation re-records the exact cutoff (12w here) via the bridge writer, the SAME 15w goat is
	// genuinely past it, so it STILL does not resolve as corrected — now on real provenance, not a default.
	if err := vacc.RecordStageReviewItem(ctx, impTenant, goatID, "kid_stage_past_age_cutoff", "K1", 15, 12, "vacc-stage-review:"+impTenant+":"+goatID); err != nil {
		t.Fatalf("re-record with real cutoff: %v", err)
	}
	resolved, wasOpen, err = vacc.ResolveStageReviewItemCorrected(ctx, impTenant, reviewID, actor, "claims fixed", time.Now())
	if err != nil {
		t.Fatalf("resolve after re-record: %v", err)
	}
	if resolved || !wasOpen {
		t.Fatalf("after re-record (15w vs 12w cutoff): resolved=%v wasOpen=%v, want false/true (still stale on real cutoff)", resolved, wasOpen)
	}
}
