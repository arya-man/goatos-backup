package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/obligation/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// These tests drive validateVaccinationWrite through the real repository write paths against
// Postgres: the approved anchor-missing catch-up, canonical purpose resolution (vaccinepurpose),
// generic second-wave gating, carry-over validation and serialized anchor changes.

const fatteningPlanDSL = `{"procurement_policy":{"purpose_plans":{"fattening":{"first_wave":["ET+TT","PPR"],"second_wave_after_days":28,"goat_second_wave":["Goat Pox"],"sheep_second_wave":["Sheep Pox"]}}}}`

type contractRule struct {
	dose, code, name, trigger string
	offset                    int32
	sequence                  int32
}

var contractVaccines = []contractRule{
	{dose: "et_tt_w1", code: "ET_TT", name: "ET+TT", trigger: "manual_campaign", sequence: 1},
	{dose: "ppr_w1", code: "PPR", name: "PPR", trigger: "manual_campaign", sequence: 1},
	{dose: "goat_pox_w2", code: "GOAT_POX", name: "Goat Pox", trigger: "manual_campaign", sequence: 1},
	{dose: "sheep_pox_w2", code: "SHEEP_POX", name: "Sheep Pox", trigger: "manual_campaign", sequence: 1},
	{dose: "fmd_w1", code: "FMD", name: "FMD", trigger: "manual_campaign", sequence: 1},
}

func contractDay(month time.Month, day int) time.Time {
	return time.Date(2026, month, day, 0, 0, 0, 0, biztime.DefaultLocation())
}

// seedContractVersion creates (and optionally publishes) one vaccination version with rule_dsl and
// the given rules; returns version id and dose -> rule id.
func seedContractVersion(t *testing.T, ctx context.Context, pool *pgxpool.Pool, protoID string, version int32, dsl string, rules []contractRule, publish bool) (string, map[string]string) {
	t.Helper()
	proto := protopg.NewRepository(pool, 5*time.Second)
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: version, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(dsl), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create version %d: %v", version, err)
	}
	out := map[string]string{}
	for _, r := range rules {
		id, err := proto.CreateRule(ctx, protodomain.NewRule{
			TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: r.dose, Sequence: r.sequence,
			TriggerType: r.trigger, OffsetDays: r.offset, DueWindowDays: 7, Repeat: "none", CatchUp: "pc_approval",
			EligibilityJSON: []byte(fmt.Sprintf(`{"vaccine":{"code":%q,"name":%q}}`, r.code, r.name)),
			ProofPolicy:     []byte(`{}`),
		})
		if err != nil {
			t.Fatalf("create rule %s: %v", r.dose, err)
		}
		out[r.dose] = id
	}
	if publish {
		if err := proto.PublishVersion(ctx, tenantID, versionID, nil); err != nil {
			t.Fatalf("publish version %d: %v", version, err)
		}
	}
	return versionID, out
}

func seedContractProtocol(t *testing.T, ctx context.Context, pool *pgxpool.Pool, code string) string {
	t.Helper()
	proto := protopg.NewRepository(pool, 5*time.Second)
	id, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: code, Name: code, Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("create definition: %v", err)
	}
	return id
}

// seedContractGoat inserts one animal; purpose "" means no procurement row at all.
func seedContractGoat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID, species string, dob *time.Time, purpose string, arrival *time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, current_location_id, park_id, dob)
VALUES ($1, $2, 'alive', $3, $4, 'female', $5, $5, $6)`, goatID, tenantID, species, meshaParty, cbePark, dob); err != nil {
		t.Fatalf("seed goat %s: %v", goatID, err)
	}
	if purpose == "" {
		return
	}
	if _, err := pool.Exec(ctx, `
WITH load AS (
  INSERT INTO procurement_loads (tenant_id, source_party_id, status, idempotency_key)
  VALUES ($1::uuid, $2::uuid, 'accepted_intake', 'contract-load-' || $3::text)
  RETURNING load_id
)
INSERT INTO procurement_load_goats (
  tenant_id, load_id, goat_id, purpose, selection_state, current_state,
  source_entry_state, ownership_state, health_state, warmup_started_at, intake_accepted_at
)
SELECT $1::uuid, load_id, $3::uuid, $4, 'accepted', 'accepted_herd_intake',
       'accepted', 'mesha_owned', 'passed', $5, $5
FROM load`, tenantID, meshaParty, goatID, purpose, arrival); err != nil {
		t.Fatalf("seed purpose %s for %s: %v", purpose, goatID, err)
	}
}

// seedAcceptedAdministration records an accepted, verified completion of ruleID on a completed row.
func seedAcceptedAdministration(t *testing.T, ctx context.Context, pool *pgxpool.Pool, versionID, ruleID, goatID string, at time.Time) {
	t.Helper()
	key := fmt.Sprintf("contract-history-%s-%s-%d", goatID, ruleID, at.Unix())
	var obligationID string
	if err := pool.QueryRow(ctx, `
INSERT INTO obligation_instances (
  tenant_id, protocol_version_id, rule_id, target_type, target_id,
  scope_type, scope_id, due_at, status, idempotency_key, sequence
) VALUES ($1::uuid, $2::uuid, $3::uuid, 'goat', $4::uuid, 'park', $5::uuid, $6, 'completed', $7, 1)
RETURNING obligation_id::text`, tenantID, versionID, ruleID, goatID, cbePark, at, key).Scan(&obligationID); err != nil {
		t.Fatalf("seed completed obligation: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_completions (tenant_id, obligation_id, goat_id, administered_at, status, verified_at, idempotency_key)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, 'accepted', $4, $5)`, tenantID, obligationID, goatID, at, key); err != nil {
		t.Fatalf("seed accepted completion: %v", err)
	}
}

// seedLegacyRow writes an open row directly, bypassing the validator, as legacy data would exist.
func seedLegacyRow(t *testing.T, ctx context.Context, pool *pgxpool.Pool, versionID, ruleID, goatID, key, identity string, due time.Time) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `
INSERT INTO obligation_instances (
  tenant_id, protocol_version_id, rule_id, target_type, target_id,
  scope_type, scope_id, due_at, status, idempotency_key, sequence, rule_identity_key
) VALUES ($1::uuid, $2::uuid, $3::uuid, 'goat', $4::uuid, 'park', $5::uuid, $6, 'scheduled', $7, 1, NULLIF($8, ''))
RETURNING obligation_id::text`, tenantID, versionID, ruleID, goatID, cbePark, due, key, identity).Scan(&id); err != nil {
		t.Fatalf("seed legacy row %s: %v", key, err)
	}
	return id
}

func contractRow(versionID, ruleID, goatID, key string, due time.Time) domain.NewObligation {
	return domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: goatID, ScopeType: "park", ScopeID: cbePark,
		DueAt: due, Status: "scheduled", IdempotencyKey: key, Sequence: 1,
	}
}

func TestVaccinationAnchorMissingCatchUpIsAuditableAndNarrow(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	_ = seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	protoID := seedContractProtocol(t, ctx, pool, "vaccination.catchup")
	v, rules := seedContractVersion(t, ctx, pool, protoID, 1, `{}`, []contractRule{
		{dose: "et_tt_adult", code: "ET_TT", name: "ET+TT", trigger: "birth_age", offset: 28, sequence: 1},
		{dose: "ppr_adult", code: "PPR", name: "PPR", trigger: "post_arrival", offset: 14, sequence: 1},
	}, true)
	const blank = "10000000-0000-4000-8000-0000000000c1"
	seedContractGoat(t, ctx, pool, blank, "goat", nil, "", nil)
	due := contractDay(time.October, 20)

	ordinary := contractRow(v, rules["et_tt_adult"], blank, "ordinary-missing-dob", due)
	if _, _, err := repo.InsertObligation(ctx, ordinary); !errors.Is(err, ports.ErrBeforeVaccinationAgeFloor) {
		t.Fatalf("ordinary missing-DOB insert error=%v, want fail-closed floor error", err)
	}
	catchUp := contractRow(v, rules["et_tt_adult"], blank, "approved-catch-up", due)
	catchUp.ScheduleBasis = domain.ScheduleBasisAnchorMissingCatchUp
	id, applied, err := repo.InsertObligation(ctx, catchUp)
	if err != nil || !applied {
		t.Fatalf("approved catch-up insert id=%q applied=%v err=%v", id, applied, err)
	}
	var basis string
	if err := pool.QueryRow(ctx, `SELECT schedule_basis FROM obligation_instances WHERE obligation_id=$1::uuid`, id).Scan(&basis); err != nil {
		t.Fatalf("read basis: %v", err)
	}
	if basis != domain.ScheduleBasisAnchorMissingCatchUp {
		t.Fatalf("persisted schedule_basis=%q, want %q", basis, domain.ScheduleBasisAnchorMissingCatchUp)
	}
	// Missing arrival anchor: same narrow exception on post_arrival.
	pprOrdinary := contractRow(v, rules["ppr_adult"], blank, "ordinary-missing-arrival", due)
	if _, _, err := repo.InsertDeferredObligation(ctx, func() domain.NewObligation { o := pprOrdinary; o.Status = "deferred"; return o }(), "warming_hold", due); !errors.Is(err, ports.ErrBeforeVaccinationAgeFloor) {
		t.Fatalf("ordinary missing-arrival deferred insert error=%v, want floor error", err)
	}
	pprCatchUp := contractRow(v, rules["ppr_adult"], blank, "approved-arrival-catch-up", due)
	pprCatchUp.ScheduleBasis = domain.ScheduleBasisAnchorMissingCatchUp
	if _, applied, err := repo.InsertObligation(ctx, pprCatchUp); err != nil || !applied {
		t.Fatalf("approved arrival catch-up applied=%v err=%v", applied, err)
	}
	// Reschedule re-proves the PERSISTED basis under the lock.
	if _, _, err := repo.RescheduleObligationByID(ctx, tenantID, id, "catch-up-move", nil, due.AddDate(0, 0, 3), due.AddDate(0, 0, 3), nil, due); err != nil {
		t.Fatalf("reschedule catch-up row: %v", err)
	}

	// Family now has accepted history: the catch-up exception no longer applies.
	seedAcceptedAdministration(t, ctx, pool, v, rules["et_tt_adult"], blank, contractDay(time.September, 1))
	again := contractRow(v, rules["et_tt_adult"], blank, "catch-up-with-history", due.AddDate(0, 0, 30))
	again.ScheduleBasis = domain.ScheduleBasisAnchorMissingCatchUp
	if _, _, err := repo.InsertObligation(ctx, again); !errors.Is(err, ports.ErrBeforeVaccinationAgeFloor) {
		t.Fatalf("catch-up with accepted family history error=%v, want rejection", err)
	}
	if _, _, err := repo.RescheduleObligationByID(ctx, tenantID, id, "catch-up-move-after-history", nil, due.AddDate(0, 0, 5), due.AddDate(0, 0, 5), nil, due); !errors.Is(err, ports.ErrBeforeVaccinationAgeFloor) {
		t.Fatalf("reschedule of catch-up row after history error=%v, want rejection", err)
	}

	// A catch-up basis on an animal whose anchor EXISTS gets the normal floor.
	const known = "10000000-0000-4000-8000-0000000000c2"
	dob := contractDay(time.September, 16)
	seedContractGoat(t, ctx, pool, known, "goat", &dob, "", nil)
	early := contractRow(v, rules["et_tt_adult"], known, "catch-up-with-dob-early", contractDay(time.September, 24))
	early.ScheduleBasis = domain.ScheduleBasisAnchorMissingCatchUp
	if _, _, err := repo.InsertObligation(ctx, early); !errors.Is(err, ports.ErrBeforeVaccinationAgeFloor) {
		t.Fatalf("catch-up basis with DOB below floor error=%v, want floor error", err)
	}
	onFloor := contractRow(v, rules["et_tt_adult"], known, "catch-up-with-dob-floor", contractDay(time.October, 14))
	onFloor.ScheduleBasis = domain.ScheduleBasisAnchorMissingCatchUp
	if _, applied, err := repo.InsertObligation(ctx, onFloor); err != nil || !applied {
		t.Fatalf("catch-up basis with DOB at floor applied=%v err=%v", applied, err)
	}
}

func TestVaccinationPurposeFailsClosedOnInsertAndReconcile(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	_ = seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	// purpose_plans exists but has no fattening plan: fattening must fail closed, not fall back.
	protoID := seedContractProtocol(t, ctx, pool, "vaccination.purpose")
	v, rules := seedContractVersion(t, ctx, pool, protoID, 1,
		`{"procurement_policy":{"purpose_plans":{"breeding":{"first_wave":["ET+TT","PPR","FMD"]}},"first_wave":["ET+TT","PPR"]}}`,
		contractVaccines, true)
	arrival := contractDay(time.August, 1)
	cases := []struct{ goat, purpose string }{
		{"10000000-0000-4000-8000-0000000000c3", "non_breeding"},
		{"10000000-0000-4000-8000-0000000000c4", "fattening"},
	}
	due := contractDay(time.October, 20)
	for _, c := range cases {
		seedContractGoat(t, ctx, pool, c.goat, "goat", nil, c.purpose, &arrival)
		if _, _, err := repo.InsertObligation(ctx, contractRow(v, rules["et_tt_w1"], c.goat, "insert-"+c.purpose, due)); !errors.Is(err, ports.ErrVaccinationNotApplicable) {
			t.Fatalf("%s insert error=%v, want ErrVaccinationNotApplicable", c.purpose, err)
		}
		identity := "et_tt|et_tt_w1|1|" + c.purpose
		legacy := seedLegacyRow(t, ctx, pool, v, rules["et_tt_w1"], c.goat, "legacy-"+c.purpose, identity, due)
		in := contractRow(v, rules["et_tt_w1"], c.goat, "reconcile-"+c.purpose, due.AddDate(0, 0, 1))
		in.RuleIdentityKey = identity
		if _, _, err := repo.ReconcileOpenObligationForRuleIdentity(ctx, tenantID, in, due); !errors.Is(err, ports.ErrVaccinationNotApplicable) {
			t.Fatalf("%s reconcile error=%v, want ErrVaccinationNotApplicable", c.purpose, err)
		}
		var got time.Time
		if err := pool.QueryRow(ctx, `SELECT due_at FROM obligation_instances WHERE obligation_id=$1::uuid`, legacy).Scan(&got); err != nil || !got.Equal(due) {
			t.Fatalf("%s legacy row moved to %s (err=%v)", c.purpose, got, err)
		}
	}
}

func TestVaccinationAuthoredBreedingPlanGovernsBreedingGoat(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	_ = seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	protoID := seedContractProtocol(t, ctx, pool, "vaccination.breeding")
	v, rules := seedContractVersion(t, ctx, pool, protoID, 1,
		`{"procurement_policy":{"purpose_plans":{"breeding":{"first_wave":["ET+TT"],"second_wave_after_days":21,"goat_second_wave":["PPR"],"sheep_second_wave":["PPR"]}}}}`,
		contractVaccines, true)
	const goat = "10000000-0000-4000-8000-0000000000c5"
	arrival := contractDay(time.August, 1)
	seedContractGoat(t, ctx, pool, goat, "goat", nil, "breeding", &arrival)
	due := contractDay(time.October, 1)
	if _, _, err := repo.InsertObligation(ctx, contractRow(v, rules["fmd_w1"], goat, "breeding-fmd", due)); !errors.Is(err, ports.ErrVaccinationNotApplicable) {
		t.Fatalf("FMD outside authored breeding plan error=%v, want not applicable", err)
	}
	if _, _, err := repo.InsertObligation(ctx, contractRow(v, rules["ppr_w1"], goat, "breeding-ppr-no-first-wave", due)); !errors.Is(err, ports.ErrBeforeVaccinationAgeFloor) {
		t.Fatalf("breeding second wave without first wave error=%v, want floor error", err)
	}
	administered := contractDay(time.September, 20)
	seedAcceptedAdministration(t, ctx, pool, v, rules["et_tt_w1"], goat, administered)
	if _, _, err := repo.InsertObligation(ctx, contractRow(v, rules["ppr_w1"], goat, "breeding-ppr-early", administered.AddDate(0, 0, 20))); !errors.Is(err, ports.ErrBeforeVaccinationAgeFloor) {
		t.Fatalf("breeding second wave before 21-day delay error=%v, want floor error", err)
	}
	if _, applied, err := repo.InsertObligation(ctx, contractRow(v, rules["ppr_w1"], goat, "breeding-ppr-valid", administered.AddDate(0, 0, 21))); err != nil || !applied {
		t.Fatalf("breeding second wave at delay applied=%v err=%v", applied, err)
	}
	if _, applied, err := repo.InsertObligation(ctx, contractRow(v, rules["et_tt_w1"], goat, "breeding-ettt", due)); err != nil || !applied {
		t.Fatalf("breeding first wave applied=%v err=%v", applied, err)
	}
}

func TestVaccinationSecondWaveHistoryChannels(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	_ = seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	protoID := seedContractProtocol(t, ctx, pool, "vaccination.history")
	v, rules := seedContractVersion(t, ctx, pool, protoID, 1, fatteningPlanDSL, contractVaccines, true)
	arrival := contractDay(time.August, 1)

	// Accepted pre-arrival history satisfies the first wave.
	const prearrival = "10000000-0000-4000-8000-0000000000c6"
	seedContractGoat(t, ctx, pool, prearrival, "goat", nil, "fattening", &arrival)
	latest := contractDay(time.July, 20)
	for i, dose := range []string{"et_tt_w1", "ppr_w1"} {
		at := latest.AddDate(0, 0, -5*(1-i))
		if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_prearrival_history_entries (
  tenant_id, goat_id, source_event_id, protocol_version_id, rule_id, vaccine_code, dose_code,
  sequence, administered_at, schedule_path, review_status, reviewed_at, claim, idempotency_key, request_fingerprint
) VALUES ($1::uuid, $2::uuid, $3, $4::uuid, $5::uuid, $6, $3, 1, $7, 'adult_procurement', 'accepted', $7, '{}'::jsonb, $3, $3)`,
			tenantID, prearrival, "prearrival-"+dose, v, rules[dose], map[string]string{"et_tt_w1": "ET_TT", "ppr_w1": "PPR"}[dose], at); err != nil {
			t.Fatalf("seed pre-arrival %s: %v", dose, err)
		}
	}
	if _, _, err := repo.InsertObligation(ctx, contractRow(v, rules["goat_pox_w2"], prearrival, "prearrival-pox-early", latest.AddDate(0, 0, 27))); !errors.Is(err, ports.ErrBeforeVaccinationAgeFloor) {
		t.Fatalf("pox one day before latest pre-arrival + delay error=%v, want floor error", err)
	}
	if _, applied, err := repo.InsertObligation(ctx, contractRow(v, rules["goat_pox_w2"], prearrival, "prearrival-pox-valid", latest.AddDate(0, 0, 28))); err != nil || !applied {
		t.Fatalf("pox at latest pre-arrival + delay applied=%v err=%v", applied, err)
	}

	// Scope-wide anchor events are NOT administrations.
	const anchored = "10000000-0000-4000-8000-0000000000c7"
	seedContractGoat(t, ctx, pool, anchored, "goat", nil, "fattening", &arrival)
	for _, code := range []string{"ET_TT", "PPR"} {
		if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_anchor_events (
  vaccination_anchor_event_id, tenant_id, protocol_version_id, vaccine_code, dose_code, anchor_date,
  scope_type, scope_payload, reason, source_system, idempotency_key
) VALUES (gen_random_uuid(), $1::uuid, $2::uuid, $3, lower($3) || '_w1', $4::date, 'tenant', '{}'::jsonb,
          'contract test', 'contract_test', 'anchor-' || $3)`, tenantID, v, code, latest); err != nil {
			t.Fatalf("seed anchor event %s: %v", code, err)
		}
	}
	if _, _, err := repo.InsertObligation(ctx, contractRow(v, rules["goat_pox_w2"], anchored, "anchor-only-pox", latest.AddDate(0, 0, 60))); !errors.Is(err, ports.ErrBeforeVaccinationAgeFloor) {
		t.Fatalf("pox with anchor-event-only first wave error=%v, want floor error", err)
	}
}

// Second-wave carry-over across a republish, goat and sheep, 1/2/3 open vaccines per animal, with
// zero / partial / complete first-wave history and early vs valid second-wave dates. A valid MANUAL
// second-wave row is rebound in place with every attachment intact; invalid ones stay retired.
func TestVaccinationSecondWaveCarryOverValidatesEveryRow(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	_ = seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	protoID := seedContractProtocol(t, ctx, pool, "vaccination.secondwave")
	v1, r1 := seedContractVersion(t, ctx, pool, protoID, 1, fatteningPlanDSL, contractVaccines, true)
	arrival := contractDay(time.August, 1)
	administered := contractDay(time.September, 1)
	valid := administered.AddDate(0, 0, 28)
	early := administered.AddDate(0, 0, 10)

	type fixture struct {
		goat, species string
		history       []string // first-wave doses administered on `administered`
		rows          map[string]time.Time
	}
	fixtures := []fixture{
		{goat: "10000000-0000-4000-8000-0000000000d0", species: "goat", rows: map[string]time.Time{"goat_pox_w2": valid}},
		{goat: "10000000-0000-4000-8000-0000000000d2", species: "goat", history: []string{"et_tt_w1"}, rows: map[string]time.Time{"ppr_w1": valid, "goat_pox_w2": valid}},
		{goat: "10000000-0000-4000-8000-0000000000d3", species: "goat", history: []string{"et_tt_w1", "ppr_w1"}, rows: map[string]time.Time{"goat_pox_w2": early}},
		{goat: "10000000-0000-4000-8000-0000000000d4", species: "goat", history: []string{"et_tt_w1", "ppr_w1"}, rows: map[string]time.Time{"et_tt_w1": valid.AddDate(0, 6, 0), "ppr_w1": valid.AddDate(1, 0, 0), "goat_pox_w2": valid}},
		{goat: "10000000-0000-4000-8000-0000000000d5", species: "sheep", history: []string{"et_tt_w1", "ppr_w1"}, rows: map[string]time.Time{"sheep_pox_w2": valid, "goat_pox_w2": valid}},
	}
	ids := map[string]string{}
	for _, f := range fixtures {
		seedContractGoat(t, ctx, pool, f.goat, f.species, nil, "fattening", &arrival)
		for _, dose := range f.history {
			seedAcceptedAdministration(t, ctx, pool, v1, r1[dose], f.goat, administered)
		}
		for dose, due := range f.rows {
			ids[f.goat+"/"+dose] = seedLegacyRow(t, ctx, pool, v1, r1[dose], f.goat, "carry-"+f.goat+"-"+dose, "", due)
		}
	}

	// Attach the valid manual Goat Pox row (d4) to a batch, a drive assignment with an operator lane,
	// a pending proof-backed completion and a status event.
	target := ids["10000000-0000-4000-8000-0000000000d4/goat_pox_w2"]
	batchID := seedDriveMembershipBatch(t, ctx, pool, v1, cbePark, valid)
	if _, err := pool.Exec(ctx, `UPDATE obligation_instances SET batch_id=$2::uuid WHERE obligation_id=$1::uuid`, target, batchID); err != nil {
		t.Fatalf("attach batch: %v", err)
	}
	var assignmentID string
	if err := pool.QueryRow(ctx, `
INSERT INTO vaccination_drive_assignments (tenant_id, batch_id, planned_date, park_id, shed_id, physical_shed, partition_label, animal_count, vaccine_rule_ids, total_doses)
VALUES ($1::uuid, $2::uuid, $3::date, $4::uuid, NULL, 'Contract 1', 'whole', 1, ARRAY[$5::uuid], 1)
RETURNING assignment_id::text`, tenantID, batchID, valid, cbePark, r1["goat_pox_w2"]).Scan(&assignmentID); err != nil {
		t.Fatalf("seed assignment: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_drive_assignment_members (tenant_id, assignment_id, obligation_id, goat_id)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid)`, tenantID, assignmentID, target, "10000000-0000-4000-8000-0000000000d4"); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_completions (tenant_id, obligation_id, goat_id, administered_at, status, idempotency_key)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, 'recorded', 'contract-pending-proof')`, tenantID, target, "10000000-0000-4000-8000-0000000000d4", valid); err != nil {
		t.Fatalf("seed pending completion: %v", err)
	}
	var keyBefore string
	if err := pool.QueryRow(ctx, `SELECT idempotency_key FROM obligation_instances WHERE obligation_id=$1::uuid`, target).Scan(&keyBefore); err != nil {
		t.Fatalf("read key: %v", err)
	}

	v2, r2 := seedContractVersion(t, ctx, pool, protoID, 2, fatteningPlanDSL, contractVaccines, false)
	moved, err := repo.CarryOverUnchangedVaccinationObligations(ctx, tenantID,
		[]string{"10000000-0000-4000-8000-0000000000d0", "10000000-0000-4000-8000-0000000000d2", "10000000-0000-4000-8000-0000000000d3", "10000000-0000-4000-8000-0000000000d4", "10000000-0000-4000-8000-0000000000d5"},
		[]string{v2})
	if err != nil {
		t.Fatalf("carry over: %v", err)
	}
	wantRebound := map[string]bool{
		"10000000-0000-4000-8000-0000000000d2/ppr_w1":       true, // first wave: no gate
		"10000000-0000-4000-8000-0000000000d4/et_tt_w1":     true,
		"10000000-0000-4000-8000-0000000000d4/ppr_w1":       true,
		"10000000-0000-4000-8000-0000000000d4/goat_pox_w2":  true, // complete + delay
		"10000000-0000-4000-8000-0000000000d5/sheep_pox_w2": true, // sheep plan
	}
	if moved != len(wantRebound) {
		t.Fatalf("carried over %d rows, want %d", moved, len(wantRebound))
	}
	for key, id := range ids {
		var version, rule string
		if err := pool.QueryRow(ctx, `SELECT protocol_version_id::text, rule_id::text FROM obligation_instances WHERE obligation_id=$1::uuid`, id).Scan(&version, &rule); err != nil {
			t.Fatalf("read %s: %v", key, err)
		}
		if wantRebound[key] && version != v2 {
			t.Fatalf("%s stayed on %s, want rebound to %s", key, version, v2)
		}
		if !wantRebound[key] && version != v1 {
			t.Fatalf("%s moved to %s, want it left on the retired version", key, version)
		}
	}

	var (
		obligationStillMember int
		batchAfter, keyAfter  string
		ruleAfter             string
		pendingOnRow          int
	)
	if err := pool.QueryRow(ctx, `
SELECT oi.batch_id::text, oi.idempotency_key, oi.rule_id::text,
       (SELECT count(*) FROM vaccination_drive_assignment_members m WHERE m.obligation_id = oi.obligation_id AND m.assignment_id = $2::uuid),
       (SELECT count(*) FROM vaccination_completions vc WHERE vc.obligation_id = oi.obligation_id AND vc.idempotency_key = 'contract-pending-proof')
FROM obligation_instances oi WHERE oi.obligation_id = $1::uuid`, target, assignmentID).
		Scan(&batchAfter, &keyAfter, &ruleAfter, &obligationStillMember, &pendingOnRow); err != nil {
		t.Fatalf("read rebound row: %v", err)
	}
	if batchAfter != batchID || keyAfter != keyBefore || ruleAfter != r2["goat_pox_w2"] || obligationStillMember != 1 || pendingOnRow != 1 {
		t.Fatalf("rebound manual second wave lost identity: batch=%s(%s) key=%s(%s) rule=%s member=%d pending=%d",
			batchAfter, batchID, keyAfter, keyBefore, ruleAfter, obligationStillMember, pendingOnRow)
	}

	// The ordinary insert path agrees with carry-over on the early second wave.
	if _, _, err := repo.InsertObligation(ctx, contractRow(v2, r2["goat_pox_w2"], "10000000-0000-4000-8000-0000000000d3", "insert-early-pox", early)); !errors.Is(err, ports.ErrBeforeVaccinationAgeFloor) {
		t.Fatalf("early second-wave insert error=%v, want floor error", err)
	}
}

// Concurrent anchor changes during reconciliation: the committed state must never hold a row below
// the floor proved against the committed anchors. Either the reconcile sees the change (it waits on
// the anchor share lock and retries), or the change is ordered after the reconcile.
func TestVaccinationReconcileSerializesAgainstAnchorChanges(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	_ = seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	protoID := seedContractProtocol(t, ctx, pool, "vaccination.concurrent")
	v, rules := seedContractVersion(t, ctx, pool, protoID, 1, fatteningPlanDSL, []contractRule{
		{dose: "et_tt_kid", code: "ET_TT", name: "ET+TT", trigger: "birth_age", offset: 28, sequence: 1},
		{dose: "ppr_arrival", code: "PPR", name: "PPR", trigger: "post_arrival", offset: 14, sequence: 1},
		{dose: "goat_pox_w2", code: "GOAT_POX", name: "Goat Pox", trigger: "manual_campaign", sequence: 1},
		{dose: "et_tt_w1", code: "ET_TT", name: "ET+TT", trigger: "manual_campaign", sequence: 2},
	}, true)

	type anchorChange struct {
		name, goat, dose string
		dob, arrival     *time.Time
		reconcileTo      time.Time
		change           string
	}
	dob := contractDay(time.September, 1)
	arrival := contractDay(time.September, 1)
	blocking := []anchorChange{
		{name: "dob", goat: "10000000-0000-4000-8000-0000000000f1", dose: "et_tt_kid", dob: &dob,
			reconcileTo: dob.AddDate(0, 0, 30), change: `UPDATE goats SET dob = $2 WHERE goat_id = $1::uuid`},
		{name: "arrival", goat: "10000000-0000-4000-8000-0000000000f2", dose: "ppr_arrival", arrival: &arrival,
			reconcileTo: arrival.AddDate(0, 0, 16), change: `UPDATE procurement_load_goats SET warmup_started_at = $2, intake_accepted_at = $2 WHERE goat_id = $1::uuid`},
	}
	for _, c := range blocking {
		t.Run(c.name, func(t *testing.T) {
			purpose := ""
			if c.arrival != nil {
				purpose = "breeding"
			}
			seedContractGoat(t, ctx, pool, c.goat, "goat", c.dob, purpose, c.arrival)
			identity := "concurrent|" + c.name
			id := seedLegacyRow(t, ctx, pool, v, rules[c.dose], c.goat, "concurrent-"+c.name, identity, c.reconcileTo.AddDate(0, 0, 20))

			// The anchor correction commits a later anchor (+10 days) while the reconcile runs.
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin anchor change: %v", err)
			}
			if _, err := tx.Exec(ctx, c.change, c.goat, contractDay(time.September, 11)); err != nil {
				t.Fatalf("anchor change: %v", err)
			}
			done := make(chan error, 1)
			go func() {
				in := contractRow(v, rules[c.dose], c.goat, "concurrent-reconciled-"+c.name, c.reconcileTo)
				in.RuleIdentityKey = identity
				_, _, err := repo.ReconcileOpenObligationForRuleIdentity(ctx, tenantID, in, c.reconcileTo)
				done <- err
			}()
			select {
			case err := <-done:
				t.Fatalf("reconcile finished (err=%v) while the anchor change was uncommitted; it must wait on the anchor lock", err)
			case <-time.After(500 * time.Millisecond):
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatalf("commit anchor change: %v", err)
			}
			if err := <-done; !errors.Is(err, ports.ErrBeforeVaccinationAgeFloor) && !isSerializationFailure(err) {
				t.Fatalf("reconcile after concurrent %s change error=%v, want floor or serialization rejection", c.name, err)
			}
			var got time.Time
			if err := pool.QueryRow(ctx, `SELECT due_at FROM obligation_instances WHERE obligation_id=$1::uuid`, id).Scan(&got); err != nil {
				t.Fatalf("read row: %v", err)
			}
			if got.Equal(c.reconcileTo) {
				t.Fatalf("committed row moved to %s, below the committed %s floor", got, c.name)
			}
		})
	}

	t.Run("completion", func(t *testing.T) {
		// A first-wave completion is a NEW row, which no lock can pre-empt. Either the reconcile
		// sees it (rejects) or it commits after the reconcile's snapshot and is ordered after it;
		// the next pass then proves against it. Assert both orders.
		const goat = "10000000-0000-4000-8000-0000000000f3"
		seedContractGoat(t, ctx, pool, goat, "goat", nil, "fattening", &arrival)
		seedAcceptedAdministration(t, ctx, pool, v, rules["et_tt_w1"], goat, contractDay(time.September, 1))
		// PPR first wave from pre-existing trusted history keeps the plan complete.
		pprRule := rules["ppr_arrival"]
		seedAcceptedAdministration(t, ctx, pool, v, pprRule, goat, contractDay(time.September, 1))
		due := contractDay(time.September, 29)
		identity := "concurrent|completion"
		id := seedLegacyRow(t, ctx, pool, v, rules["goat_pox_w2"], goat, "concurrent-completion", identity, due.AddDate(0, 0, 10))

		// Order 1: completion commits FIRST (a later first-wave dose on 09-10) -> reconcile rejects.
		seedAcceptedAdministration(t, ctx, pool, v, rules["et_tt_w1"], goat, contractDay(time.September, 10))
		in := contractRow(v, rules["goat_pox_w2"], goat, "concurrent-completion-reconciled", due)
		in.RuleIdentityKey = identity
		if _, _, err := repo.ReconcileOpenObligationForRuleIdentity(ctx, tenantID, in, due); !errors.Is(err, ports.ErrBeforeVaccinationAgeFloor) {
			t.Fatalf("reconcile after committed later completion error=%v, want floor error", err)
		}
		var got time.Time
		if err := pool.QueryRow(ctx, `SELECT due_at FROM obligation_instances WHERE obligation_id=$1::uuid`, id).Scan(&got); err != nil || got.Equal(due) {
			t.Fatalf("row moved below the completion floor: due=%s err=%v", got, err)
		}

	})
}

// A follow-up dose chained from an attested per-animal vaccination anchor ("BT dose 1 on 12/08")
// persists exactly as generation proposes it; the anchor date + gap is its floor.
func TestVaccinationFollowUpChainsFromAnimalSetAnchor(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	_ = seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	protoID := seedContractProtocol(t, ctx, pool, "vaccination.anchorchain")
	v, rules := seedContractVersion(t, ctx, pool, protoID, 1, `{}`, []contractRule{
		{dose: "bt_adult_1", code: "BLUE_TONGUE", name: "Blue Tongue", trigger: "manual_campaign", sequence: 1},
		{dose: "bt_adult_2", code: "BLUE_TONGUE", name: "Blue Tongue", trigger: "after_previous_completion", offset: 28, sequence: 2},
	}, true)
	const goat = "10000000-0000-4000-8000-0000000000b1"
	const other = "10000000-0000-4000-8000-0000000000b2"
	dob := contractDay(time.January, 1).AddDate(-3, 0, 0)
	seedContractGoat(t, ctx, pool, goat, "goat", &dob, "", nil)
	seedContractGoat(t, ctx, pool, other, "goat", &dob, "", nil)
	anchor := contractDay(time.August, 12)
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_anchor_events (
  vaccination_anchor_event_id, tenant_id, protocol_version_id, vaccine_code, dose_code, anchor_date,
  scope_type, scope_payload, reason, source_system, idempotency_key
) VALUES (gen_random_uuid(), $1::uuid, $2::uuid, 'BLUE_TONGUE', 'bt_adult_1', $3::date, 'animal_set',
          jsonb_build_object('animal_ids', jsonb_build_array($4::text)), 'attested BT dose 1', 'contract_test', 'bt-anchor')`,
		tenantID, v, anchor, goat); err != nil {
		t.Fatalf("seed animal-set anchor: %v", err)
	}
	dose2 := func(goatID, key string, due time.Time) domain.NewObligation {
		o := contractRow(v, rules["bt_adult_2"], goatID, key, due)
		o.Sequence = 2
		return o
	}
	if _, _, err := repo.InsertObligation(ctx, dose2(goat, "bt2-early", anchor.AddDate(0, 0, 27))); !errors.Is(err, ports.ErrBeforeVaccinationAgeFloor) {
		t.Fatalf("dose 2 before anchor + gap error=%v, want floor error", err)
	}
	if _, applied, err := repo.InsertObligation(ctx, dose2(goat, "bt2-valid", anchor.AddDate(0, 0, 28))); err != nil || !applied {
		t.Fatalf("dose 2 chained from animal-set anchor applied=%v err=%v", applied, err)
	}
	// The anchor names one animal; it is not history for any other.
	if _, _, err := repo.InsertObligation(ctx, dose2(other, "bt2-other", anchor.AddDate(0, 0, 28))); !errors.Is(err, ports.ErrBeforeVaccinationAgeFloor) {
		t.Fatalf("dose 2 for an animal outside the anchor set error=%v, want missing-previous floor error", err)
	}
}
