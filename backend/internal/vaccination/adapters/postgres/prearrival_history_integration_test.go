package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	oblpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	"github.com/vgoats/goatos/backend/internal/vaccination/ports"
)

// BUG-017 regression suite.
//
// Procurement captures a supplier-attested pre-arrival vaccination card into the `goat.created`
// payload key `trusted_vaccination_history`
// (backend/internal/procurement/adapters/postgres/goat_created_outbox.go). Before this change the
// vaccination `goat.created` consumer discarded `e.Payload` entirely, so a procured adult that had
// genuinely received ET+TT dose 1 + dose 2 from the supplier was scheduled from scratch and
// re-injected, and its 182-day repeat was withheld waiting for a dose 2 the animal already had.
//
// These tests drive the REAL production path: the in-process event bus -> the registered
// vaccapp.GoatCreatedHandler -> GenerationService -> the real Postgres repositories.

const (
	bug017ETTTRuleDSL = `{"vaccine":{"code":"ET_TT","name":"ET+TT","type":"killed","pathogen_class":"bacterial"},` +
		`"eligibility":{"defer_states":["sick","under_treatment","quarantine","icu"]},` +
		`"source":{"source_system":"pc","source_ref":"PC 6","review_status":"approved","approved_by":"Reviewer"}}`
)

type bug017Protocol struct {
	versionID   string
	w1RuleID    string
	w2RuleID    string
	revacRuleID string
	kidRuleID   string
}

// seedBUG017ETTTProtocol publishes the confirmed ET+TT course: a two-dose adult primary course
// (dose 2 = dose 1 + 21 days) followed by the 182-day repeat, plus one kid-course row so a
// kid-dose claim on an adult animal can be exercised as a protocol-impossible claim.
func seedBUG017ETTTProtocol(t *testing.T, ctx context.Context, proto *protopg.Repository, code string) bug017Protocol {
	t.Helper()
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: code, Name: "BUG017 " + code, Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(bug017ETTTRuleDSL), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	out := bug017Protocol{versionID: versionID}
	if out.w1RuleID, err = proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "et_tt_adult_w1", Sequence: 1,
		TriggerType: "post_arrival", OffsetDays: 7, Repeat: "none", CatchUp: "immediate",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	}); err != nil {
		t.Fatalf("w1 rule: %v", err)
	}
	if out.w2RuleID, err = proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "et_tt_adult_w2", Sequence: 2,
		TriggerType: "post_arrival", OffsetDays: 28, MinGapDays: 21, Repeat: "none", CatchUp: "immediate",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	}); err != nil {
		t.Fatalf("w2 rule: %v", err)
	}
	if out.revacRuleID, err = proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "et_tt_revac", Sequence: 3,
		TriggerType: "after_previous_completion", OffsetDays: 182, MinGapDays: 182,
		Repeat: "every_n_days", CatchUp: "next_cycle",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	}); err != nil {
		t.Fatalf("revac rule: %v", err)
	}
	if out.kidRuleID, err = proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "et_tt_kid_w4", Sequence: 4,
		TriggerType: "birth_age", OffsetDays: 28, Repeat: "none", CatchUp: "immediate",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	}); err != nil {
		t.Fatalf("kid rule: %v", err)
	}
	if err := proto.PublishVersion(ctx, impTenant, versionID, nil); err != nil {
		t.Fatalf("publish: %v", err)
	}
	return out
}

func bug017Payload(t *testing.T, goatID string, entryDate time.Time, claims []map[string]any) []byte {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"goat_id":                     goatID,
		"origin_type":                 "procured",
		"entry_date":                  entryDate.Format("2006-01-02"),
		"trusted_vaccination_history": claims,
		"generation_status":           "queued",
	})
	if err != nil {
		t.Fatalf("marshal goat.created payload: %v", err)
	}
	return payload
}

func bug017Claim(vaccineCode, doseCode string, sequence int, administeredAt time.Time) map[string]any {
	return map[string]any{
		"vaccine_code":    vaccineCode,
		"dose_code":       doseCode,
		"sequence":        sequence,
		"administered_at": administeredAt.Format(time.RFC3339),
	}
}

type bug017Obligation struct {
	ruleID string
	dueAt  time.Time
	status string
}

func bug017Obligations(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID string) []bug017Obligation {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT rule_id::text, due_at, status
FROM obligation_instances
WHERE tenant_id = $1 AND target_id = $2
ORDER BY due_at`, impTenant, goatID)
	if err != nil {
		t.Fatalf("read obligations: %v", err)
	}
	defer rows.Close()
	var out []bug017Obligation
	for rows.Next() {
		var o bug017Obligation
		if err := rows.Scan(&o.ruleID, &o.dueAt, &o.status); err != nil {
			t.Fatalf("scan obligation: %v", err)
		}
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("obligation rows: %v", err)
	}
	return out
}

func bug017PublishGoatCreated(t *testing.T, ctx context.Context, bus eventbus.Bus, eventID, goatID string, payload []byte, asOf time.Time) {
	t.Helper()
	if err := bus.Publish(ctx, eventbus.Event{
		ID: eventID, Type: vaccapp.EventGoatCreated, TenantID: impTenant, Key: goatID,
		Payload: payload, OccurredAt: asOf,
	}); err != nil {
		t.Fatalf("publish goat.created: %v", err)
	}
}

// TestBUG017ProcuredTrustedHistoryAnchorsAdultETTTRepeat is the headline case: a genuine
// supplier-attested adult ET+TT course (dose 1 + dose 2) must NOT be re-issued, and the 182-day
// repeat must anchor on the accepted dose 2 date.
func TestBUG017ProcuredTrustedHistoryAnchorsAdultETTTRepeat(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)
	p := seedBUG017ETTTProtocol(t, ctx, proto, "vaccination.bug017.course")

	goatID := "30000000-0000-4000-8000-00000000e001"
	entryDate := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	seedGenAdultProcuredGoat(t, ctx, pool, goatID, "alive", entryDate)

	dose1At := time.Date(2026, 6, 20, 8, 0, 0, 0, time.UTC)
	dose2At := time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC)

	bus := eventbus.NewInProcessBus()
	vaccapp.NewGoatCreatedHandler(vaccapp.NewGenerationService(proto, vacc, obl)).Register(bus)
	bug017PublishGoatCreated(t, ctx, bus, "evt-bug017-course", goatID,
		bug017Payload(t, goatID, entryDate, []map[string]any{
			bug017Claim("ET_TT", "et_tt_adult_w1", 1, dose1At),
			bug017Claim("ET_TT", "et_tt_adult_w2", 2, dose2At),
		}), asOf)

	got := bug017Obligations(t, ctx, pool, goatID)
	for _, o := range got {
		if o.ruleID == p.w1RuleID || o.ruleID == p.w2RuleID {
			t.Fatalf("regenerated already-given adult ET+TT dose rule=%s due=%s: accepted pre-arrival history must suppress it (all=%#v)",
				o.ruleID, o.dueAt, got)
		}
	}
	wantRepeat := biztime.BusinessDayStart(dose2At).AddDate(0, 0, 182)
	found := false
	for _, o := range got {
		if o.ruleID != p.revacRuleID {
			continue
		}
		found = true
		if !o.dueAt.Equal(wantRepeat) {
			t.Fatalf("ET+TT 182-day repeat due=%s, want %s (anchored on accepted dose 2)", o.dueAt, wantRepeat)
		}
	}
	if !found {
		t.Fatalf("no ET+TT 182-day repeat generated; want one due %s anchored on accepted dose 2 (all=%#v)", wantRepeat, got)
	}
}

// TestBUG017DoseOneOnlyYieldsDoseTwoNotRepeat locks the confirmed ET+TT course rule: an accepted
// dose 1 alone must create the dose 2 obligation at dose 1 + 21 days and must never jump to the
// 182-day repeat.
func TestBUG017DoseOneOnlyYieldsDoseTwoNotRepeat(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)
	p := seedBUG017ETTTProtocol(t, ctx, proto, "vaccination.bug017.dose1only")

	goatID := "30000000-0000-4000-8000-00000000e002"
	entryDate := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	seedGenAdultProcuredGoat(t, ctx, pool, goatID, "alive", entryDate)

	dose1At := time.Date(2026, 7, 10, 8, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC)

	bus := eventbus.NewInProcessBus()
	vaccapp.NewGoatCreatedHandler(vaccapp.NewGenerationService(proto, vacc, obl)).Register(bus)
	bug017PublishGoatCreated(t, ctx, bus, "evt-bug017-dose1", goatID,
		bug017Payload(t, goatID, entryDate, []map[string]any{
			bug017Claim("ET_TT", "et_tt_adult_w1", 1, dose1At),
		}), asOf)

	got := bug017Obligations(t, ctx, pool, goatID)
	wantDose2 := biztime.BusinessDayStart(dose1At).AddDate(0, 0, 21)
	sawDose2 := false
	for _, o := range got {
		switch o.ruleID {
		case p.w1RuleID:
			t.Fatalf("regenerated already-given adult ET+TT dose 1 due=%s (all=%#v)", o.dueAt, got)
		case p.revacRuleID:
			t.Fatalf("182-day repeat generated due=%s from dose 1 alone; the repeat may only start after dose 2 (all=%#v)", o.dueAt, got)
		case p.w2RuleID:
			sawDose2 = true
			if !o.dueAt.Equal(wantDose2) {
				t.Fatalf("adult ET+TT dose 2 due=%s, want %s (dose 1 + 21 days)", o.dueAt, wantDose2)
			}
		}
	}
	if !sawDose2 {
		t.Fatalf("no adult ET+TT dose 2 obligation generated; want one due %s (all=%#v)", wantDose2, got)
	}
}

// TestBUG017ExactReplayAddsNoDuplicateHistory proves the idempotency contract: redelivering the
// same goat.created event returns the original persisted entries with no new side effects.
func TestBUG017ExactReplayAddsNoDuplicateHistory(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)
	seedBUG017ETTTProtocol(t, ctx, proto, "vaccination.bug017.replay")

	goatID := "30000000-0000-4000-8000-00000000e003"
	entryDate := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	seedGenAdultProcuredGoat(t, ctx, pool, goatID, "alive", entryDate)

	dose1At := time.Date(2026, 6, 20, 8, 0, 0, 0, time.UTC)
	dose2At := time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC)
	payload := bug017Payload(t, goatID, entryDate, []map[string]any{
		bug017Claim("ET_TT", "et_tt_adult_w1", 1, dose1At),
		bug017Claim("ET_TT", "et_tt_adult_w2", 2, dose2At),
	})

	bus := eventbus.NewInProcessBus()
	vaccapp.NewGoatCreatedHandler(vaccapp.NewGenerationService(proto, vacc, obl)).Register(bus)
	bug017PublishGoatCreated(t, ctx, bus, "evt-bug017-replay", goatID, payload, asOf)
	firstEntries := countRowsVacc(t, ctx, pool,
		`SELECT count(*) FROM vaccination_prearrival_history_entries WHERE tenant_id=$1 AND goat_id=$2`, impTenant, goatID)
	firstObligations := bug017Obligations(t, ctx, pool, goatID)
	if firstEntries != 2 {
		t.Fatalf("persisted pre-arrival history entries = %d, want 2", firstEntries)
	}

	bug017PublishGoatCreated(t, ctx, bus, "evt-bug017-replay", goatID, payload, asOf)
	replayEntries := countRowsVacc(t, ctx, pool,
		`SELECT count(*) FROM vaccination_prearrival_history_entries WHERE tenant_id=$1 AND goat_id=$2`, impTenant, goatID)
	if replayEntries != firstEntries {
		t.Fatalf("exact replay changed persisted history entries: %d -> %d", firstEntries, replayEntries)
	}
	replayObligations := bug017Obligations(t, ctx, pool, goatID)
	if len(replayObligations) != len(firstObligations) {
		t.Fatalf("exact replay changed obligations: %d -> %d (%#v)", len(firstObligations), len(replayObligations), replayObligations)
	}
	for i := range firstObligations {
		if firstObligations[i] != replayObligations[i] {
			t.Fatalf("exact replay mutated obligation %d: %#v -> %#v", i, firstObligations[i], replayObligations[i])
		}
	}
}

// TestBUG017ImpossibleClaimIsRejectedRecordedAndDoesNotSuppress proves the durable rejected-entry
// sink: a kid-course dose claimed for an animal independently classified as adult is
// protocol-impossible. It must be persisted with review_status='rejected' plus a reason (never
// silently dropped) and it must NOT suppress the adult course the animal genuinely still needs.
func TestBUG017ImpossibleClaimIsRejectedRecordedAndDoesNotSuppress(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)
	p := seedBUG017ETTTProtocol(t, ctx, proto, "vaccination.bug017.rejected")

	goatID := "30000000-0000-4000-8000-00000000e004"
	entryDate := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	seedGenAdultProcuredGoat(t, ctx, pool, goatID, "alive", entryDate)

	asOf := time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC)
	bus := eventbus.NewInProcessBus()
	vaccapp.NewGoatCreatedHandler(vaccapp.NewGenerationService(proto, vacc, obl)).Register(bus)
	bug017PublishGoatCreated(t, ctx, bus, "evt-bug017-rejected", goatID,
		bug017Payload(t, goatID, entryDate, []map[string]any{
			// Kid-course dose claimed for a goat whose DOB/entry/stage classify it as adult.
			bug017Claim("ET_TT", "et_tt_kid_w4", 1, time.Date(2026, 6, 20, 8, 0, 0, 0, time.UTC)),
			// Administered in the future: impossible as pre-arrival history.
			bug017Claim("ET_TT", "et_tt_adult_w1", 1, time.Date(2026, 8, 30, 8, 0, 0, 0, time.UTC)),
		}), asOf)

	if got := countRowsVacc(t, ctx, pool, `
SELECT count(*) FROM vaccination_prearrival_history_entries
WHERE tenant_id=$1 AND goat_id=$2 AND review_status='rejected'
  AND nullif(btrim(rejection_reason), '') IS NOT NULL`, impTenant, goatID); got != 2 {
		t.Fatalf("durably recorded rejected pre-arrival claims = %d, want 2 (rejected claims must be surfaced, never dropped)", got)
	}
	if got := countRowsVacc(t, ctx, pool, `
SELECT count(*) FROM vaccination_prearrival_history_entries
WHERE tenant_id=$1 AND goat_id=$2 AND review_status='accepted'`, impTenant, goatID); got != 0 {
		t.Fatalf("accepted pre-arrival entries = %d, want 0; an impossible supplier claim must never become accepted history", got)
	}

	got := bug017Obligations(t, ctx, pool, goatID)
	sawW1, sawW2 := false, false
	for _, o := range got {
		switch o.ruleID {
		case p.w1RuleID:
			sawW1 = true
		case p.w2RuleID:
			sawW2 = true
		case p.kidRuleID:
			t.Fatalf("kid-course obligation generated for an adult-classified goat (all=%#v)", got)
		}
	}
	if !sawW1 || !sawW2 {
		t.Fatalf("rejected claims suppressed genuinely-needed adult ET+TT work: w1=%v w2=%v (all=%#v)", sawW1, sawW2, got)
	}
}

// TestBUG017SameKeyDifferentPayloadIsRejected completes the idempotency contract: a redelivery of
// the SAME goat.created event carrying a DIFFERENT claim (a silently corrected supplier date) must
// be rejected, not allowed to overwrite an already-reviewed medical claim.
func TestBUG017SameKeyDifferentPayloadIsRejected(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)
	seedBUG017ETTTProtocol(t, ctx, proto, "vaccination.bug017.conflict")

	goatID := "30000000-0000-4000-8000-00000000e005"
	entryDate := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	seedGenAdultProcuredGoat(t, ctx, pool, goatID, "alive", entryDate)
	asOf := time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC)

	bus := eventbus.NewInProcessBus()
	vaccapp.NewGoatCreatedHandler(vaccapp.NewGenerationService(proto, vacc, obl)).Register(bus)
	bug017PublishGoatCreated(t, ctx, bus, "evt-bug017-conflict", goatID,
		bug017Payload(t, goatID, entryDate, []map[string]any{
			bug017Claim("ET_TT", "et_tt_adult_w1", 1, time.Date(2026, 6, 20, 8, 0, 0, 0, time.UTC)),
		}), asOf)

	err := bus.Publish(ctx, eventbus.Event{
		ID: "evt-bug017-conflict", Type: vaccapp.EventGoatCreated, TenantID: impTenant, Key: goatID,
		Payload: bug017Payload(t, goatID, entryDate, []map[string]any{
			bug017Claim("ET_TT", "et_tt_adult_w1", 1, time.Date(2026, 6, 25, 8, 0, 0, 0, time.UTC)),
		}),
		OccurredAt: asOf,
	})
	if err == nil {
		t.Fatalf("same-key/different-payload replay was accepted; it must be rejected")
	}
	if !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("same-key/different-payload replay error = %v, want %v", err, ports.ErrIdempotencyConflict)
	}
	if got := countRowsVacc(t, ctx, pool, `
SELECT count(*) FROM vaccination_prearrival_history_entries
WHERE tenant_id=$1 AND goat_id=$2 AND administered_at = TIMESTAMPTZ '2026-06-20 08:00:00+00'`,
		impTenant, goatID); got != 1 {
		t.Fatalf("original reviewed claim rows = %d, want 1 (the conflicting replay must not overwrite it)", got)
	}
}
