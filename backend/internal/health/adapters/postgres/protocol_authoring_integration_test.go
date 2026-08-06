package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	healthapp "github.com/vgoats/goatos/backend/internal/health/app"
	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/health/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// The /health-config/* authoring path against a real database.
//
// These run through the SERVICE (healthapp.ConfigService), not the repository alone, because the
// service is what the HTTP handler calls: normalization, validation and step ordering all sit there
// and a repository-only test would prove a path production never takes.

func authoringService(pool *pgxpool.Pool) *healthapp.ConfigService {
	return healthapp.NewConfigService(NewRepository(pool, 10*time.Second))
}

func createDiseaseCmd(name, key string) domain.CreateDiseaseCommand {
	return domain.CreateDiseaseCommand{
		TenantID: healthTenant, ActorID: healthActor,
		DisplayName: name, DurationDays: 2,
		IdempotencyKey: "create-" + key, RequestFingerprint: "fp-create-" + key,
	}
}

func completeCourse(name string) domain.AuthoredProtocol {
	return domain.AuthoredProtocol{
		DisplayName:  name,
		DurationDays: 2,
		Steps: []domain.AuthoredStep{
			{DayNo: 1, Session: domain.SessionMorning, RecordType: domain.RecordTypeMedication,
				MedicineName: "Meloxicam Paracetamol", DosageText: "5", DosageDenominator: "ml", MedicineRoute: "IM"},
			{DayNo: 2, Session: domain.SessionMorning, RecordType: domain.RecordTypeAction,
				Instruction: "Check the animal is eating."},
		},
	}
}

// The whole authored lifecycle on the production path: create -> save -> publish -> re-edit ->
// publish again, with the version swap and the retirement asserted in the DATABASE, not in the
// return value.
func TestHealthConfigAuthoringLifecycle(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	svc := authoringService(pool)

	// ---- create -------------------------------------------------------------------------------
	created, err := svc.CreateDisease(ctx, createDiseaseCmd("Foot rot", "foot-rot"))
	if err != nil {
		t.Fatalf("create disease: %v", err)
	}
	if created.Outcome != domain.OutcomeCreated || created.DiseaseKey != "foot_rot" {
		t.Fatalf("create=%+v, want outcome created and a derived key foot_rot", created)
	}
	// BOTH bands, because the phone picks the protocol from the goat's own age band.
	if len(created.DraftVersionIDs) != 2 ||
		created.DraftVersionIDs[domain.AgeBandAdult] == "" ||
		created.DraftVersionIDs[domain.AgeBandKid] == "" {
		t.Fatalf("create must open a draft for both age bands, got %+v", created.DraftVersionIDs)
	}
	assertHealthCount(t, ctx, pool, "drafts after create",
		`SELECT count(*)::int FROM health_protocol_versions WHERE tenant_id=$1::uuid AND disease_key='foot_rot' AND status='draft'`,
		2, healthTenant)
	assertHealthCount(t, ctx, pool, "published after create",
		`SELECT count(*)::int FROM health_protocol_versions WHERE tenant_id=$1::uuid AND disease_key='foot_rot' AND status='published'`,
		0, healthTenant)

	adultDraft := created.DraftVersionIDs[domain.AgeBandAdult]

	// ---- save ---------------------------------------------------------------------------------
	saved, err := svc.SaveDraft(ctx, domain.SaveDraftCommand{
		TenantID: healthTenant, ActorID: healthActor,
		DiseaseKey: "foot_rot", AgeBand: domain.AgeBandAdult,
		Protocol:       completeCourse("Foot rot"),
		IdempotencyKey: "save-1", RequestFingerprint: "fp-save-1",
	})
	if err != nil {
		t.Fatalf("save draft: %v", err)
	}
	if saved.Outcome != domain.OutcomeSaved {
		t.Fatalf("save outcome=%q, want saved", saved.Outcome)
	}
	// Seq is server-assigned 1..N in working order; the client sent none.
	var seqs []int
	rows, err := pool.Query(ctx, `SELECT seq FROM health_protocol_steps WHERE health_protocol_version_id=$1::uuid ORDER BY seq`, adultDraft)
	if err != nil {
		t.Fatalf("read draft steps: %v", err)
	}
	for rows.Next() {
		var seq int
		if err := rows.Scan(&seq); err != nil {
			t.Fatalf("scan seq: %v", err)
		}
		seqs = append(seqs, seq)
	}
	rows.Close()
	if len(seqs) != 2 || seqs[0] != 1 || seqs[1] != 2 {
		t.Fatalf("expected server-assigned seq 1,2 got %v", seqs)
	}

	// ---- publish ------------------------------------------------------------------------------
	published, err := svc.PublishDraft(ctx, domain.ProtocolVersionCommand{
		TenantID: healthTenant, ActorID: healthActor, ProtocolVersionID: adultDraft,
		IdempotencyKey: "publish-1", RequestFingerprint: "fp-publish-1",
	})
	if err != nil {
		t.Fatalf("publish draft: %v", err)
	}
	if published.Outcome != domain.OutcomePublished {
		t.Fatalf("publish outcome=%q, want published", published.Outcome)
	}
	// The FIRST publish of a disease retires nothing.
	if published.RetiredVersionID != "" {
		t.Fatalf("first publish must retire nothing, retired %q", published.RetiredVersionID)
	}
	assertHealthCount(t, ctx, pool, "published adult after first publish",
		`SELECT count(*)::int FROM health_protocol_versions WHERE tenant_id=$1::uuid AND disease_key='foot_rot' AND age_band='adult' AND status='published'`,
		1, healthTenant)

	// ---- re-edit and publish again --------------------------------------------------------------
	editDraft, err := svc.GetDraftForEdit(ctx, domain.ProtocolVersionCommand{
		TenantID: healthTenant, ActorID: healthActor,
	}, "foot_rot", domain.AgeBandAdult)
	if err != nil {
		t.Fatalf("open draft for edit: %v", err)
	}
	if editDraft.Status != domain.ProtocolStatusDraft {
		t.Fatalf("expected a draft, got status %q", editDraft.Status)
	}
	// Copy-on-edit: the draft carries the published version's steps so the author starts from what
	// is live rather than from a blank page.
	if len(editDraft.Steps) != 2 {
		t.Fatalf("draft must copy the published steps, got %d", len(editDraft.Steps))
	}
	// The LIVE version is untouched while the draft exists.
	assertHealthCount(t, ctx, pool, "published still live during edit",
		`SELECT count(*)::int FROM health_protocol_versions WHERE tenant_id=$1::uuid AND health_protocol_version_id=$2::uuid AND status='published'`,
		1, healthTenant, adultDraft)

	changed := completeCourse("Foot rot")
	changed.Steps[0].DosageText = "3" // the dosage change this whole feature exists for
	if _, err := svc.SaveDraft(ctx, domain.SaveDraftCommand{
		TenantID: healthTenant, ActorID: healthActor,
		DiseaseKey: "foot_rot", AgeBand: domain.AgeBandAdult,
		Protocol:       changed,
		IdempotencyKey: "save-2", RequestFingerprint: "fp-save-2",
	}); err != nil {
		t.Fatalf("save second draft: %v", err)
	}
	second, err := svc.PublishDraft(ctx, domain.ProtocolVersionCommand{
		TenantID: healthTenant, ActorID: healthActor, ProtocolVersionID: editDraft.ProtocolVersionID,
		IdempotencyKey: "publish-2", RequestFingerprint: "fp-publish-2",
	})
	if err != nil {
		t.Fatalf("publish second draft: %v", err)
	}
	if second.RetiredVersionID != adultDraft {
		t.Fatalf("second publish must retire the previous version %q, retired %q", adultDraft, second.RetiredVersionID)
	}
	// Exactly one published version survives, and the old one is retired rather than deleted.
	assertHealthCount(t, ctx, pool, "exactly one published adult version",
		`SELECT count(*)::int FROM health_protocol_versions WHERE tenant_id=$1::uuid AND disease_key='foot_rot' AND age_band='adult' AND status='published'`,
		1, healthTenant)
	assertHealthCount(t, ctx, pool, "the previous version is retired, not deleted",
		`SELECT count(*)::int FROM health_protocol_versions WHERE tenant_id=$1::uuid AND health_protocol_version_id=$2::uuid AND status='retired'`,
		1, healthTenant, adultDraft)
	// The retired version keeps its steps, so "what dose did this goat get" stays answerable.
	assertHealthCount(t, ctx, pool, "retired version keeps its steps",
		`SELECT count(*)::int FROM health_protocol_steps WHERE health_protocol_version_id=$1::uuid`,
		2, adultDraft)

	// The live protocol now carries the NEW dosage.
	var liveDose string
	if err := pool.QueryRow(ctx, `
SELECT s.dosage_text FROM health_protocol_steps s
JOIN health_protocol_versions v ON v.health_protocol_version_id=s.health_protocol_version_id
WHERE v.tenant_id=$1::uuid AND v.disease_key='foot_rot' AND v.age_band='adult' AND v.status='published' AND s.seq=1`,
		healthTenant).Scan(&liveDose); err != nil {
		t.Fatalf("read live dosage: %v", err)
	}
	if liveDose != "3" {
		t.Fatalf("live dosage=%q, want the published edit 3", liveDose)
	}
}

// THE SAFETY PROPERTY. A goat diagnosed under v1 keeps being treated on v1's dosages after v2 is
// published. This is the reason protocols are versioned rather than edited in place, and it is
// asserted on the real diagnosis path (OpenCase), not on the authoring path alone.
func TestPublishingDoesNotChangeAnAnimalMidTreatment(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)
	svc := authoringService(pool)

	created, err := svc.CreateDisease(ctx, createDiseaseCmd("Fever", "fever"))
	if err != nil {
		t.Fatalf("create disease: %v", err)
	}
	adultDraft := created.DraftVersionIDs[domain.AgeBandAdult]
	if _, err := svc.SaveDraft(ctx, domain.SaveDraftCommand{
		TenantID: healthTenant, ActorID: healthActor,
		DiseaseKey: "fever", AgeBand: domain.AgeBandAdult, Protocol: completeCourse("Fever"),
		IdempotencyKey: "save-v1", RequestFingerprint: "fp-save-v1",
	}); err != nil {
		t.Fatalf("save v1: %v", err)
	}
	if _, err := svc.PublishDraft(ctx, domain.ProtocolVersionCommand{
		TenantID: healthTenant, ActorID: healthActor, ProtocolVersionID: adultDraft,
		IdempotencyKey: "publish-v1", RequestFingerprint: "fp-publish-v1",
	}); err != nil {
		t.Fatalf("publish v1: %v", err)
	}

	// Diagnose a goat under v1, through the real OpenCase path.
	loc, _ := time.LoadLocation("Asia/Kolkata")
	opened, err := repo.OpenCase(ctx, domain.OpenCaseInput{
		TenantID: healthTenant, ActorID: healthActor, GoatID: healthGoat,
		DiseaseKey: "fever", AgeBand: domain.AgeBandAdult, StartDate: time.Now().In(loc),
		IdempotencyKey: "open-fever-v1", RequestFingerprint: "fp-open-v1",
	})
	if err != nil {
		t.Fatalf("open case under v1: %v", err)
	}

	// Now change the dosage and publish v2.
	editDraft, err := svc.GetDraftForEdit(ctx, domain.ProtocolVersionCommand{
		TenantID: healthTenant, ActorID: healthActor,
	}, "fever", domain.AgeBandAdult)
	if err != nil {
		t.Fatalf("open draft: %v", err)
	}
	changed := completeCourse("Fever")
	changed.Steps[0].DosageText = "99"
	if _, err := svc.SaveDraft(ctx, domain.SaveDraftCommand{
		TenantID: healthTenant, ActorID: healthActor,
		DiseaseKey: "fever", AgeBand: domain.AgeBandAdult, Protocol: changed,
		IdempotencyKey: "save-v2", RequestFingerprint: "fp-save-v2",
	}); err != nil {
		t.Fatalf("save v2: %v", err)
	}
	if _, err := svc.PublishDraft(ctx, domain.ProtocolVersionCommand{
		TenantID: healthTenant, ActorID: healthActor, ProtocolVersionID: editDraft.ProtocolVersionID,
		IdempotencyKey: "publish-v2", RequestFingerprint: "fp-publish-v2",
	}); err != nil {
		t.Fatalf("publish v2: %v", err)
	}

	// The open case still points at v1 ...
	var pinned string
	if err := pool.QueryRow(ctx, `SELECT health_protocol_version_id::text FROM health_cases
WHERE tenant_id=$1::uuid AND health_case_id=$2::uuid`, healthTenant, opened.CaseID).Scan(&pinned); err != nil {
		t.Fatalf("read pinned version: %v", err)
	}
	if pinned != adultDraft {
		t.Fatalf("the case must stay pinned to v1 %q, got %q", adultDraft, pinned)
	}
	// ... and the steps the operator will actually administer still say 5, not 99.
	var caseDose string
	if err := pool.QueryRow(ctx, `
SELECT ss.dosage_text FROM health_session_steps ss
JOIN health_treatment_sessions hs ON hs.health_session_id=ss.health_session_id
WHERE hs.health_case_id=$1::uuid AND ss.record_type='medication'
ORDER BY hs.day_no, ss.seq LIMIT 1`, opened.CaseID).Scan(&caseDose); err != nil {
		t.Fatalf("read the case's own step copy: %v", err)
	}
	if caseDose != "5" {
		t.Fatalf("an animal mid-treatment must keep v1's dosage 5, got %q", caseDose)
	}
}

// Identical content is not a new version. Without this, every "Save" click churns out a version
// differing from its predecessor in nothing but its id, and the history stops being readable.
func TestSavingIdenticalContentIsUnchangedAndWritesNothing(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	svc := authoringService(pool)

	if _, err := svc.CreateDisease(ctx, createDiseaseCmd("Anemia", "anemia")); err != nil {
		t.Fatalf("create: %v", err)
	}
	save := func(key string) domain.AuthoringResult {
		res, err := svc.SaveDraft(ctx, domain.SaveDraftCommand{
			TenantID: healthTenant, ActorID: healthActor,
			DiseaseKey: "anemia", AgeBand: domain.AgeBandAdult, Protocol: completeCourse("Anemia"),
			IdempotencyKey: key, RequestFingerprint: "fp-" + key,
		})
		if err != nil {
			t.Fatalf("save %s: %v", key, err)
		}
		return res
	}
	if first := save("s1"); first.Outcome != domain.OutcomeSaved {
		t.Fatalf("first save outcome=%q, want saved", first.Outcome)
	}
	// A DIFFERENT idempotency key with the SAME content: a genuinely new request, but nothing to do.
	if second := save("s2"); second.Outcome != domain.OutcomeUnchanged {
		t.Fatalf("re-saving identical content must be unchanged, got %q", second.Outcome)
	}
	assertHealthCount(t, ctx, pool, "still exactly one draft",
		`SELECT count(*)::int FROM health_protocol_versions WHERE tenant_id=$1::uuid AND disease_key='anemia' AND age_band='adult' AND status='draft'`,
		1, healthTenant)
}

// The idempotency contract, all three branches, on the real write path.
func TestAuthoringIdempotency(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	svc := authoringService(pool)

	cmd := createDiseaseCmd("Mastitis", "mastitis")
	first, err := svc.CreateDisease(ctx, cmd)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if first.IdempotentReplay {
		t.Fatal("the first call is not a replay")
	}

	// EXACT replay: same key, same fingerprint -> the original result, no second disease.
	replay, err := svc.CreateDisease(ctx, cmd)
	if err != nil {
		t.Fatalf("replay create: %v", err)
	}
	if !replay.IdempotentReplay || replay.Outcome != domain.OutcomeCreated {
		t.Fatalf("replay=%+v, want an idempotent replay of the create", replay)
	}
	if len(replay.DraftVersionIDs) != 2 {
		t.Fatalf("a replayed create must still name both drafts, got %+v", replay.DraftVersionIDs)
	}
	assertHealthCount(t, ctx, pool, "replay created no extra versions",
		`SELECT count(*)::int FROM health_protocol_versions WHERE tenant_id=$1::uuid AND disease_key='mastitis'`,
		2, healthTenant)

	// SAME key, DIFFERENT payload: two different edits claiming one identity. Guessing which the
	// author meant would author a protocol nobody asked for.
	conflicting := cmd
	conflicting.RequestFingerprint = "a-different-payload"
	if _, err := svc.CreateDisease(ctx, conflicting); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("same key + different payload must be ErrConflict, got %v", err)
	}
}

// One draft per protocol. Without it, two authors each get a draft, both publish, and the second
// silently retires the first author's just-published version seconds after it went live.
func TestOnlyOneDraftPerProtocol(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	svc := authoringService(pool)

	if _, err := svc.CreateDisease(ctx, createDiseaseCmd("Jaundice", "jaundice")); err != nil {
		t.Fatalf("create: %v", err)
	}
	open := func() (domain.ProtocolDetail, error) {
		return svc.GetDraftForEdit(ctx, domain.ProtocolVersionCommand{
			TenantID: healthTenant, ActorID: healthActor,
		}, "jaundice", domain.AgeBandAdult)
	}
	a, err := open()
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	b, err := open()
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	// The second author is handed the SAME draft, not a second one.
	if a.ProtocolVersionID != b.ProtocolVersionID {
		t.Fatalf("expected one shared draft, got %q and %q", a.ProtocolVersionID, b.ProtocolVersionID)
	}
	assertHealthCount(t, ctx, pool, "exactly one draft",
		`SELECT count(*)::int FROM health_protocol_versions WHERE tenant_id=$1::uuid AND disease_key='jaundice' AND age_band='adult' AND status='draft'`,
		1, healthTenant)
}

// A published version is immutable because goats are being treated from it, so neither publish nor
// discard may address one.
func TestPublishedVersionsCannotBePublishedAgainOrDiscarded(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	svc := authoringService(pool)

	created, err := svc.CreateDisease(ctx, createDiseaseCmd("Pinkeye", "pinkeye"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	adultDraft := created.DraftVersionIDs[domain.AgeBandAdult]
	if _, err := svc.SaveDraft(ctx, domain.SaveDraftCommand{
		TenantID: healthTenant, ActorID: healthActor,
		DiseaseKey: "pinkeye", AgeBand: domain.AgeBandAdult, Protocol: completeCourse("Pinkeye"),
		IdempotencyKey: "save", RequestFingerprint: "fp-save",
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := svc.PublishDraft(ctx, domain.ProtocolVersionCommand{
		TenantID: healthTenant, ActorID: healthActor, ProtocolVersionID: adultDraft,
		IdempotencyKey: "pub", RequestFingerprint: "fp-pub",
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if _, err := svc.PublishDraft(ctx, domain.ProtocolVersionCommand{
		TenantID: healthTenant, ActorID: healthActor, ProtocolVersionID: adultDraft,
		IdempotencyKey: "pub-again", RequestFingerprint: "fp-pub-again",
	}); !errors.Is(err, ports.ErrNotADraft) {
		t.Fatalf("republishing a published version must be ErrNotADraft, got %v", err)
	}
	if _, err := svc.DiscardDraft(ctx, domain.ProtocolVersionCommand{
		TenantID: healthTenant, ActorID: healthActor, ProtocolVersionID: adultDraft,
		IdempotencyKey: "discard", RequestFingerprint: "fp-discard",
	}); !errors.Is(err, ports.ErrNotADraft) {
		t.Fatalf("discarding a published version must be ErrNotADraft, got %v", err)
	}
	assertHealthCount(t, ctx, pool, "the published version survives both attempts",
		`SELECT count(*)::int FROM health_protocol_versions WHERE tenant_id=$1::uuid AND health_protocol_version_id=$2::uuid AND status='published'`,
		1, healthTenant, adultDraft)
}

// Publish applies the strict rulebook against what is STORED, not against what the last save was
// told. A draft can be saved incomplete on purpose; this is the gate.
func TestPublishRejectsAnIncompleteStoredDraft(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	svc := authoringService(pool)

	created, err := svc.CreateDisease(ctx, createDiseaseCmd("Acidosis", "acidosis"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	adultDraft := created.DraftVersionIDs[domain.AgeBandAdult]

	// An empty draft saves fine ...
	if _, err := svc.SaveDraft(ctx, domain.SaveDraftCommand{
		TenantID: healthTenant, ActorID: healthActor,
		DiseaseKey: "acidosis", AgeBand: domain.AgeBandAdult,
		Protocol:       domain.AuthoredProtocol{DisplayName: "Acidosis", DurationDays: 2},
		IdempotencyKey: "save-empty", RequestFingerprint: "fp-save-empty",
	}); err != nil {
		t.Fatalf("an empty draft must save: %v", err)
	}
	// ... and does not publish.
	_, err = svc.PublishDraft(ctx, domain.ProtocolVersionCommand{
		TenantID: healthTenant, ActorID: healthActor, ProtocolVersionID: adultDraft,
		IdempotencyKey: "pub-empty", RequestFingerprint: "fp-pub-empty",
	})
	var ve *domain.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("publishing an empty draft must be a validation error, got %v", err)
	}
	assertHealthCount(t, ctx, pool, "nothing was published",
		`SELECT count(*)::int FROM health_protocol_versions WHERE tenant_id=$1::uuid AND disease_key='acidosis' AND status='published'`,
		0, healthTenant)
}

// Reordering steps is a whole-list replacement. The per-version UNIQUE (seq) constraint makes an
// in-place renumber collide with itself, so this proves the replacement path commits cleanly.
func TestSavingAReorderRewritesTheWholeStepList(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	svc := authoringService(pool)

	if _, err := svc.CreateDisease(ctx, createDiseaseCmd("Bloating", "bloating")); err != nil {
		t.Fatalf("create: %v", err)
	}
	first := domain.AuthoredProtocol{
		DisplayName: "Bloating", DurationDays: 1,
		Steps: []domain.AuthoredStep{
			{DayNo: 1, Session: domain.SessionMorning, RecordType: domain.RecordTypeMedication,
				MedicineName: "Bloatosil", DosageText: "5", DosageDenominator: "ml", MedicineRoute: "Oral"},
			{DayNo: 1, Session: domain.SessionEvening, RecordType: domain.RecordTypeAction, Instruction: "Walk the animal."},
		},
	}
	if _, err := svc.SaveDraft(ctx, domain.SaveDraftCommand{
		TenantID: healthTenant, ActorID: healthActor,
		DiseaseKey: "bloating", AgeBand: domain.AgeBandAdult, Protocol: first,
		IdempotencyKey: "r1", RequestFingerprint: "fp-r1",
	}); err != nil {
		t.Fatalf("save first: %v", err)
	}

	// Swap them. Because SortAuthoredSteps orders by day then working session, moving the action to
	// the morning and the medicine to the evening genuinely inverts the stored order.
	swapped := domain.AuthoredProtocol{
		DisplayName: "Bloating", DurationDays: 1,
		Steps: []domain.AuthoredStep{
			{DayNo: 1, Session: domain.SessionEvening, RecordType: domain.RecordTypeMedication,
				MedicineName: "Bloatosil", DosageText: "5", DosageDenominator: "ml", MedicineRoute: "Oral"},
			{DayNo: 1, Session: domain.SessionMorning, RecordType: domain.RecordTypeAction, Instruction: "Walk the animal."},
		},
	}
	if _, err := svc.SaveDraft(ctx, domain.SaveDraftCommand{
		TenantID: healthTenant, ActorID: healthActor,
		DiseaseKey: "bloating", AgeBand: domain.AgeBandAdult, Protocol: swapped,
		IdempotencyKey: "r2", RequestFingerprint: "fp-r2",
	}); err != nil {
		t.Fatalf("save reorder: %v", err)
	}

	var firstType string
	if err := pool.QueryRow(ctx, `
SELECT s.record_type FROM health_protocol_steps s
JOIN health_protocol_versions v ON v.health_protocol_version_id=s.health_protocol_version_id
WHERE v.tenant_id=$1::uuid AND v.disease_key='bloating' AND v.age_band='adult' AND v.status='draft'
ORDER BY s.seq LIMIT 1`, healthTenant).Scan(&firstType); err != nil {
		t.Fatalf("read reordered steps: %v", err)
	}
	if firstType != domain.RecordTypeAction {
		t.Fatalf("after the reorder the morning ACTION must be seq 1, got %q", firstType)
	}
	assertHealthCount(t, ctx, pool, "no orphan steps left behind",
		`SELECT count(*)::int FROM health_protocol_steps s
JOIN health_protocol_versions v ON v.health_protocol_version_id=s.health_protocol_version_id
WHERE v.tenant_id=$1::uuid AND v.disease_key='bloating'`,
		2, healthTenant)
}

// A disease key is the join identity of every case ever opened under it, so reusing one would merge
// two clinical histories.
func TestCreatingADuplicateDiseaseIsRejected(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	svc := authoringService(pool)

	if _, err := svc.CreateDisease(ctx, createDiseaseCmd("Wounds", "wounds")); err != nil {
		t.Fatalf("create: %v", err)
	}
	dup := createDiseaseCmd("Wounds", "wounds-again")
	if _, err := svc.CreateDisease(ctx, dup); !errors.Is(err, ports.ErrDiseaseExists) {
		t.Fatalf("a duplicate disease must be ErrDiseaseExists, got %v", err)
	}
}

// THE IMPORTER LOCK. Once the app has authored a protocol, the sheet import would retire every
// published version and republish the sheet's set — silently discarding the authored edit, with no
// constraint violation to notice.
func TestSheetImportFailsClosedOnceTheAppHasAuthored(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)
	svc := authoringService(pool)

	sheet := []domain.SourceProtocol{{
		DiseaseKey: "diarrhea", DisplayName: "Diarrhea", AgeBand: domain.AgeBandAdult,
		DurationDays: 1,
		Steps: []domain.ProtocolStep{{
			DayNo: 1, Session: domain.SessionMorning, Seq: 1, RecordType: domain.RecordTypeAction,
			Instruction: strPtr("Give electrolyte."),
		}},
	}}

	// BEFORE any app authoring, the import works — this is the bootstrap.
	if err := repo.ReplacePublishedProtocols(ctx, healthTenant, healthActor, "google-sheet:x", "hash-1", sheet); err != nil {
		t.Fatalf("the bootstrap import must succeed: %v", err)
	}

	// Now author something in the app.
	if _, err := svc.CreateDisease(ctx, createDiseaseCmd("Milk fever", "milk-fever")); err != nil {
		t.Fatalf("create: %v", err)
	}

	// The same import now fails closed rather than discarding that draft.
	err := repo.ReplacePublishedProtocols(ctx, healthTenant, healthActor, "google-sheet:x", "hash-2", sheet)
	if !errors.Is(err, ports.ErrImportAfterAuthoring) {
		t.Fatalf("import after authoring must fail closed, got %v", err)
	}
	if !strings.Contains(err.Error(), "app-authored") {
		t.Fatalf("the error must say why, got %q", err.Error())
	}

	// And the break-glass form still works, for a maintainer re-bootstrap.
	if err := repo.ReplacePublishedProtocolsOverwritingAuthored(ctx, healthTenant, healthActor, "google-sheet:x", "hash-3", sheet); err != nil {
		t.Fatalf("the break-glass import must succeed: %v", err)
	}
}

// The catalog read: one row per disease per age band, carrying the live version and the open draft
// side by side, with the counts belonging to their own version rather than to a joined row.
func TestProtocolCatalogPairsLiveAndDraftWithoutFanout(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	svc := authoringService(pool)

	created, err := svc.CreateDisease(ctx, createDiseaseCmd("Fracture", "fracture"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	adultDraft := created.DraftVersionIDs[domain.AgeBandAdult]
	if _, err := svc.SaveDraft(ctx, domain.SaveDraftCommand{
		TenantID: healthTenant, ActorID: healthActor,
		DiseaseKey: "fracture", AgeBand: domain.AgeBandAdult, Protocol: completeCourse("Fracture"),
		IdempotencyKey: "cat-save", RequestFingerprint: "fp-cat-save",
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := svc.PublishDraft(ctx, domain.ProtocolVersionCommand{
		TenantID: healthTenant, ActorID: healthActor, ProtocolVersionID: adultDraft,
		IdempotencyKey: "cat-pub", RequestFingerprint: "fp-cat-pub",
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	// Re-open a draft so the adult row carries BOTH a live version and an open draft.
	if _, err := svc.GetDraftForEdit(ctx, domain.ProtocolVersionCommand{
		TenantID: healthTenant, ActorID: healthActor,
	}, "fracture", domain.AgeBandAdult); err != nil {
		t.Fatalf("reopen draft: %v", err)
	}

	page, err := svc.ListProtocolCatalog(ctx, domain.ProtocolCatalogQuery{TenantID: healthTenant, Limit: 20})
	if err != nil {
		t.Fatalf("list catalog: %v", err)
	}
	var adult, kid *domain.ProtocolCatalogItem
	for i := range page.Items {
		if page.Items[i].DiseaseKey != "fracture" {
			continue
		}
		if page.Items[i].AgeBand == domain.AgeBandAdult {
			adult = &page.Items[i]
		} else {
			kid = &page.Items[i]
		}
	}
	if adult == nil || kid == nil {
		t.Fatalf("expected one row per age band, got %+v", page.Items)
	}
	// ONE row for the adult band even though it has two versions with two step sets — the join is
	// 1:1 on (disease, age band) and the step counts are pre-aggregated per version.
	if adult.PublishedVersionID == "" || !adult.HasDraft {
		t.Fatalf("the adult row must carry the live version AND the open draft, got %+v", adult)
	}
	if adult.StepCount != 2 || adult.MedicationCount != 1 {
		t.Fatalf("published counts must belong to the published version, got step=%d med=%d", adult.StepCount, adult.MedicationCount)
	}
	if adult.DraftStepCount != 2 {
		t.Fatalf("draft counts must belong to the draft version, got %d", adult.DraftStepCount)
	}
	// The kid band was never edited: drafted, never published.
	if kid.PublishedVersionID != "" || !kid.HasDraft {
		t.Fatalf("the kid row must show no live version and an open draft, got %+v", kid)
	}

	// The age-band filter narrows without changing the pairing.
	adultsOnly, err := svc.ListProtocolCatalog(ctx, domain.ProtocolCatalogQuery{
		TenantID: healthTenant, AgeBand: domain.AgeBandAdult, Limit: 20,
	})
	if err != nil {
		t.Fatalf("list adults: %v", err)
	}
	for _, item := range adultsOnly.Items {
		if item.AgeBand != domain.AgeBandAdult {
			t.Fatalf("the age-band filter leaked a %q row", item.AgeBand)
		}
	}
}

// PublishDraft is ONE transaction spanning FOUR side effects: retire the live version, publish the
// draft, write the audit row, and emit the domain event. AGENTS.md requires that a state transition
// and the sync it owns commit together or not at all — a half-applied publish would be the worst
// possible state here, because "draft promoted but old version not retired" leaves TWO live
// protocols for one disease and the next diagnosis picks arbitrarily.
//
// The failure is injected at the LAST side effect (the outbox emit) with a real trigger, so
// everything before it has already succeeded inside the transaction. If any of it survived the
// rollback, this test fails.
//
// WHAT THIS TEST ACTUALLY GATES, stated precisely because the obvious reading is wrong. It was
// mutation-tested by deleting PublishDraft's error check on insertProtocolOutbox (`_ = ...`), and
// it stayed GREEN. That is not a hole: a trigger RAISE aborts the whole Postgres transaction, so
// the ledger insert and the commit that follow fail regardless of what the Go code does with the
// error. The atomicity here is enforced by the database, and swallowing the Go error cannot defeat
// it — which is a stronger guarantee than a Go-level check, not a weaker one.
//
// So this test proves the OUTCOME (a failed sync leaves nothing behind, and the same idempotency
// key is still usable afterwards), not the error-propagation line. A regression that DID defeat it
// — moving the outbox emit outside the transaction, or committing before it — would fail here,
// which is the shape of the defect the rule exists to prevent.
func TestPublishDraftRollsBackEverythingWhenTheOutboxSyncFails(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	svc := authoringService(pool)

	// v1 published, v2 drafted — so a successful publish would have BOTH a retire and a promote.
	created, err := svc.CreateDisease(ctx, createDiseaseCmd("Mastitis", "rollback"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	v1 := created.DraftVersionIDs[domain.AgeBandAdult]
	if _, err := svc.SaveDraft(ctx, domain.SaveDraftCommand{
		TenantID: healthTenant, ActorID: healthActor,
		DiseaseKey: "mastitis", AgeBand: domain.AgeBandAdult, Protocol: completeCourse("Mastitis"),
		IdempotencyKey: "rb-save-1", RequestFingerprint: "fp-rb-save-1",
	}); err != nil {
		t.Fatalf("save v1: %v", err)
	}
	if _, err := svc.PublishDraft(ctx, domain.ProtocolVersionCommand{
		TenantID: healthTenant, ActorID: healthActor, ProtocolVersionID: v1,
		IdempotencyKey: "rb-pub-1", RequestFingerprint: "fp-rb-pub-1",
	}); err != nil {
		t.Fatalf("publish v1: %v", err)
	}
	v2, err := svc.GetDraftForEdit(ctx, domain.ProtocolVersionCommand{
		TenantID: healthTenant, ActorID: healthActor,
	}, "mastitis", domain.AgeBandAdult)
	if err != nil {
		t.Fatalf("open v2: %v", err)
	}

	auditBefore := countRows(t, ctx, pool,
		`SELECT count(*)::int FROM audit_log WHERE tenant_id=$1::uuid AND action='health.protocol.published'`, healthTenant)
	ledgerBefore := countRows(t, ctx, pool,
		`SELECT count(*)::int FROM health_config_write_log WHERE tenant_id=$1::uuid`, healthTenant)

	// Break the LAST side effect of the publish transaction.
	if _, err := pool.Exec(ctx, `
CREATE OR REPLACE FUNCTION health_publish_outbox_boom() RETURNS trigger AS $$
BEGIN RAISE EXCEPTION 'injected outbox failure'; END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER health_publish_outbox_boom_trg BEFORE INSERT ON outbox_messages
FOR EACH ROW WHEN (NEW.event_type = 'health.protocol.published')
EXECUTE FUNCTION health_publish_outbox_boom();`); err != nil {
		t.Fatalf("install failure trigger: %v", err)
	}

	_, err = svc.PublishDraft(ctx, domain.ProtocolVersionCommand{
		TenantID: healthTenant, ActorID: healthActor, ProtocolVersionID: v2.ProtocolVersionID,
		IdempotencyKey: "rb-pub-2", RequestFingerprint: "fp-rb-pub-2",
	})
	if err == nil {
		t.Fatal("the publish must fail when its outbox emit fails")
	}
	if _, dropErr := pool.Exec(ctx, `DROP TRIGGER health_publish_outbox_boom_trg ON outbox_messages`); dropErr != nil {
		t.Fatalf("remove failure trigger: %v", dropErr)
	}

	// NOTHING may have survived: not the promote, not the retire, not the audit row, not the ledger
	// entry that would make a retry look like an already-applied replay.
	assertHealthCount(t, ctx, pool, "the draft was NOT promoted",
		`SELECT count(*)::int FROM health_protocol_versions WHERE tenant_id=$1::uuid AND health_protocol_version_id=$2::uuid AND status='draft'`,
		1, healthTenant, v2.ProtocolVersionID)
	assertHealthCount(t, ctx, pool, "the live version was NOT retired",
		`SELECT count(*)::int FROM health_protocol_versions WHERE tenant_id=$1::uuid AND health_protocol_version_id=$2::uuid AND status='published'`,
		1, healthTenant, v1)
	assertHealthCount(t, ctx, pool, "there is still exactly ONE live version for this disease",
		`SELECT count(*)::int FROM health_protocol_versions WHERE tenant_id=$1::uuid AND disease_key='mastitis' AND age_band='adult' AND status='published'`,
		1, healthTenant)
	assertHealthCount(t, ctx, pool, "no audit row was left behind",
		`SELECT count(*)::int FROM audit_log WHERE tenant_id=$1::uuid AND action='health.protocol.published'`,
		auditBefore, healthTenant)
	assertHealthCount(t, ctx, pool, "no ledger entry was left behind",
		`SELECT count(*)::int FROM health_config_write_log WHERE tenant_id=$1::uuid`,
		ledgerBefore, healthTenant)

	// And the SAME key now succeeds, because the failed attempt consumed nothing. A ledger entry
	// that had survived would make this replay the phantom publish instead of performing it.
	republished, err := svc.PublishDraft(ctx, domain.ProtocolVersionCommand{
		TenantID: healthTenant, ActorID: healthActor, ProtocolVersionID: v2.ProtocolVersionID,
		IdempotencyKey: "rb-pub-2", RequestFingerprint: "fp-rb-pub-2",
	})
	if err != nil {
		t.Fatalf("retry after the rolled-back publish must succeed: %v", err)
	}
	if republished.IdempotentReplay {
		t.Fatal("the retry must PERFORM the publish, not replay a write that never committed")
	}
	if republished.RetiredVersionID != v1 {
		t.Fatalf("the retry must retire v1, retired %q", republished.RetiredVersionID)
	}
}

func countRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, query, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// ADVERSARIAL: the one-to-many fan-out the catalog's pre-aggregation exists to prevent.
//
// A protocol has MANY steps and a disease has MANY versions. If step_counts were joined raw instead
// of pre-aggregated to one row per version, a 28-step protocol would return 28 catalog rows and
// every count on them would be a count of joined rows rather than of steps. This seeds a protocol
// with many steps of MULTIPLE DIMENSIONS (three record types across three sessions and two days)
// plus a second version of the same disease, and asserts the catalog still returns exactly one row
// per (disease, age band) with counts that belong to their own version.
func TestProtocolCatalogOneToManyStepsAndMultipleDimensionsDoNotFanOutRows(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	svc := authoringService(pool)

	created, err := svc.CreateDisease(ctx, createDiseaseCmd("Dog Bite", "dog-bite"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	adultDraft := created.DraftVersionIDs[domain.AgeBandAdult]

	// 12 steps: 3 record types x 2 sessions x 2 days. Every dimension the row reports is exercised.
	wide := domain.AuthoredProtocol{DisplayName: "Dog Bite", DurationDays: 2}
	for day := 1; day <= 2; day++ {
		for _, session := range []string{domain.SessionMorning, domain.SessionEvening} {
			wide.Steps = append(wide.Steps,
				domain.AuthoredStep{DayNo: day, Session: session, RecordType: domain.RecordTypeMedication,
					MedicineName: "OTC 200mg", DosageText: "5", DosageDenominator: "ml", MedicineRoute: "IM"},
				domain.AuthoredStep{DayNo: day, Session: session, RecordType: domain.RecordTypeAction,
					Instruction: "Clean the wound."},
				domain.AuthoredStep{DayNo: day, Session: session, RecordType: domain.RecordTypeCriticalAction,
					Instruction: "If worsening, shift to quarantine.", CriticalActionType: domain.CriticalActionQuarantineOrMove},
			)
		}
	}
	if _, err := svc.SaveDraft(ctx, domain.SaveDraftCommand{
		TenantID: healthTenant, ActorID: healthActor,
		DiseaseKey: "dog_bite", AgeBand: domain.AgeBandAdult, Protocol: wide,
		IdempotencyKey: "fanout-save", RequestFingerprint: "fp-fanout-save",
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := svc.PublishDraft(ctx, domain.ProtocolVersionCommand{
		TenantID: healthTenant, ActorID: healthActor, ProtocolVersionID: adultDraft,
		IdempotencyKey: "fanout-pub", RequestFingerprint: "fp-fanout-pub",
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	// A SECOND version of the same disease, so the disease also has many VERSIONS.
	second, err := svc.GetDraftForEdit(ctx, domain.ProtocolVersionCommand{
		TenantID: healthTenant, ActorID: healthActor,
	}, "dog_bite", domain.AgeBandAdult)
	if err != nil {
		t.Fatalf("open second draft: %v", err)
	}

	page, err := svc.ListProtocolCatalog(ctx, domain.ProtocolCatalogQuery{
		TenantID: healthTenant, AgeBand: domain.AgeBandAdult, Search: "Dog Bite", Limit: 50,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("12 steps x 2 versions must still be ONE catalog row, got %d", len(page.Items))
	}
	row := page.Items[0]
	if row.StepCount != 12 || row.MedicationCount != 4 || row.CriticalActionCount != 4 {
		t.Fatalf("published counts must be per-version counts of STEPS, got step=%d med=%d crit=%d",
			row.StepCount, row.MedicationCount, row.CriticalActionCount)
	}
	if !row.HasDraft || row.DraftVersionID != second.ProtocolVersionID || row.DraftStepCount != 12 {
		t.Fatalf("the draft side must report its OWN version and counts, got %+v", row)
	}
}

// ADVERSARIAL: page boundaries. The catalog keysets on (display_name, disease_key, age_band); all
// three are needed because display_name repeats across the two age bands of one disease. A cursor
// on name alone would skip or repeat a band at every page edge.
func TestProtocolCatalogPaginationWalksEveryRowAcrossPageBoundariesWithoutSkipOrRepeat(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	svc := authoringService(pool)

	// 7 diseases x 2 bands = 14 rows, walked 3 at a time so boundaries land mid-disease.
	names := []string{"Abscesses", "Acidosis", "Anemia", "Arthritis", "Bloating", "Fever", "Wounds"}
	for i, name := range names {
		if _, err := svc.CreateDisease(ctx, createDiseaseCmd(name, fmt.Sprintf("page-%d", i))); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}

	seen := map[string]int{}
	cursor := ""
	for page := 0; page < 20; page++ {
		got, err := svc.ListProtocolCatalog(ctx, domain.ProtocolCatalogQuery{
			TenantID: healthTenant, Cursor: cursor, Limit: 3,
		})
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		for _, item := range got.Items {
			seen[item.DiseaseKey+":"+item.AgeBand]++
		}
		if got.NextCursor == nil {
			break
		}
		if len(got.Items) != 3 {
			t.Fatalf("a page reporting more must be full, got %d rows", len(got.Items))
		}
		cursor = *got.NextCursor
	}
	if len(seen) != len(names)*2 {
		t.Fatalf("the walk must visit every (disease, age band) exactly once: saw %d distinct of %d",
			len(seen), len(names)*2)
	}
	for key, count := range seen {
		if count != 1 {
			t.Fatalf("%s appeared %d times across pages -- a keyset must never repeat a row", key, count)
		}
	}
}

// ADVERSARIAL: every status bucket. A version is draft, published or retired, and the catalog must
// place each in the right column of the matrix -- published facts on the published side, draft facts
// on the draft side, and a RETIRED version on neither (it is history, not a live course).
func TestProtocolCatalogStatusMatrixPlacesEveryStatusInItsOwnBucket(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	svc := authoringService(pool)

	created, err := svc.CreateDisease(ctx, createDiseaseCmd("Pinkeye", "status-matrix"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	adultV1 := created.DraftVersionIDs[domain.AgeBandAdult]

	// v1: 2 steps, published then later retired.
	if _, err := svc.SaveDraft(ctx, domain.SaveDraftCommand{
		TenantID: healthTenant, ActorID: healthActor,
		DiseaseKey: "pinkeye", AgeBand: domain.AgeBandAdult, Protocol: completeCourse("Pinkeye"),
		IdempotencyKey: "sm-1", RequestFingerprint: "fp-sm-1",
	}); err != nil {
		t.Fatalf("save v1: %v", err)
	}
	if _, err := svc.PublishDraft(ctx, domain.ProtocolVersionCommand{
		TenantID: healthTenant, ActorID: healthActor, ProtocolVersionID: adultV1,
		IdempotencyKey: "sm-pub-1", RequestFingerprint: "fp-sm-pub-1",
	}); err != nil {
		t.Fatalf("publish v1: %v", err)
	}
	// v2: 3 steps, published -> v1 becomes RETIRED.
	v2, err := svc.GetDraftForEdit(ctx, domain.ProtocolVersionCommand{
		TenantID: healthTenant, ActorID: healthActor,
	}, "pinkeye", domain.AgeBandAdult)
	if err != nil {
		t.Fatalf("open v2: %v", err)
	}
	threeStep := completeCourse("Pinkeye")
	threeStep.Steps = append(threeStep.Steps, domain.AuthoredStep{
		DayNo: 2, Session: domain.SessionEvening, RecordType: domain.RecordTypeAction, Instruction: "Recheck the eye.",
	})
	if _, err := svc.SaveDraft(ctx, domain.SaveDraftCommand{
		TenantID: healthTenant, ActorID: healthActor,
		DiseaseKey: "pinkeye", AgeBand: domain.AgeBandAdult, Protocol: threeStep,
		IdempotencyKey: "sm-2", RequestFingerprint: "fp-sm-2",
	}); err != nil {
		t.Fatalf("save v2: %v", err)
	}
	if _, err := svc.PublishDraft(ctx, domain.ProtocolVersionCommand{
		TenantID: healthTenant, ActorID: healthActor, ProtocolVersionID: v2.ProtocolVersionID,
		IdempotencyKey: "sm-pub-2", RequestFingerprint: "fp-sm-pub-2",
	}); err != nil {
		t.Fatalf("publish v2: %v", err)
	}
	// v3: an OPEN draft, so all three statuses exist at once for this disease/band.
	v3, err := svc.GetDraftForEdit(ctx, domain.ProtocolVersionCommand{
		TenantID: healthTenant, ActorID: healthActor,
	}, "pinkeye", domain.AgeBandAdult)
	if err != nil {
		t.Fatalf("open v3: %v", err)
	}

	assertHealthCount(t, ctx, pool, "all three statuses coexist",
		`SELECT count(DISTINCT status)::int FROM health_protocol_versions
WHERE tenant_id=$1::uuid AND disease_key='pinkeye' AND age_band='adult'`, 3, healthTenant)

	page, err := svc.ListProtocolCatalog(ctx, domain.ProtocolCatalogQuery{
		TenantID: healthTenant, AgeBand: domain.AgeBandAdult, Search: "Pinkeye", Limit: 20,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("three versions of one disease/band must be ONE row, got %d", len(page.Items))
	}
	row := page.Items[0]
	if row.PublishedVersionID != v2.ProtocolVersionID || row.StepCount != 3 {
		t.Fatalf("the published column must carry v2's facts, got id=%q steps=%d", row.PublishedVersionID, row.StepCount)
	}
	if row.DraftVersionID != v3.ProtocolVersionID {
		t.Fatalf("the draft column must carry v3, got %q", row.DraftVersionID)
	}
	// The RETIRED v1 must appear in NEITHER live column -- it is history.
	if row.PublishedVersionID == adultV1 || row.DraftVersionID == adultV1 {
		t.Fatalf("the retired version must not occupy a live column, got %+v", row)
	}
	// It is still readable through the detail history, which is where history belongs.
	detail, err := svc.GetProtocolDetail(ctx, healthTenant, v2.ProtocolVersionID)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	var sawRetired bool
	for _, version := range detail.History {
		if version.ProtocolVersionID == adultV1 && version.Status == domain.ProtocolStatusRetired {
			sawRetired = true
		}
	}
	if !sawRetired {
		t.Fatalf("the retired version must remain readable in the history, got %+v", detail.History)
	}

	// The draft_only filter is the same matrix read from the other side.
	draftsOnly, err := svc.ListProtocolCatalog(ctx, domain.ProtocolCatalogQuery{
		TenantID: healthTenant, DraftOnly: true, Limit: 50,
	})
	if err != nil {
		t.Fatalf("list drafts: %v", err)
	}
	for _, item := range draftsOnly.Items {
		if !item.HasDraft {
			t.Fatalf("draft_only returned a row with no draft: %+v", item)
		}
	}
}

// A draft is deleted whole: its steps go with it (ON DELETE CASCADE), and the live version is
// untouched.
func TestDiscardRemovesTheDraftAndItsStepsOnly(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	svc := authoringService(pool)

	created, err := svc.CreateDisease(ctx, createDiseaseCmd("Lumps", "lumps"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	adultDraft := created.DraftVersionIDs[domain.AgeBandAdult]
	if _, err := svc.SaveDraft(ctx, domain.SaveDraftCommand{
		TenantID: healthTenant, ActorID: healthActor,
		DiseaseKey: "lumps", AgeBand: domain.AgeBandAdult, Protocol: completeCourse("Lumps"),
		IdempotencyKey: "d-save", RequestFingerprint: "fp-d-save",
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := svc.DiscardDraft(ctx, domain.ProtocolVersionCommand{
		TenantID: healthTenant, ActorID: healthActor, ProtocolVersionID: adultDraft,
		IdempotencyKey: "d-discard", RequestFingerprint: "fp-d-discard",
	}); err != nil {
		t.Fatalf("discard: %v", err)
	}
	assertHealthCount(t, ctx, pool, "the draft is gone",
		`SELECT count(*)::int FROM health_protocol_versions WHERE tenant_id=$1::uuid AND health_protocol_version_id=$2::uuid`,
		0, healthTenant, adultDraft)
	assertHealthCount(t, ctx, pool, "its steps went with it",
		`SELECT count(*)::int FROM health_protocol_steps WHERE health_protocol_version_id=$1::uuid`,
		0, adultDraft)
	// The kid band's draft is untouched.
	assertHealthCount(t, ctx, pool, "the other band is untouched",
		`SELECT count(*)::int FROM health_protocol_versions WHERE tenant_id=$1::uuid AND disease_key='lumps' AND age_band='kid' AND status='draft'`,
		1, healthTenant)
}

// A rename is a DISEASE-level fact: both bands are the same illness, and letting them drift would
// show an operator one name on an adult and another on a kid.
func TestRenamingADraftRenamesBothBands(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	svc := authoringService(pool)

	if _, err := svc.CreateDisease(ctx, createDiseaseCmd("Foot rot", "rename")); err != nil {
		t.Fatalf("create: %v", err)
	}
	renamed := completeCourse("Footrot")
	if _, err := svc.SaveDraft(ctx, domain.SaveDraftCommand{
		TenantID: healthTenant, ActorID: healthActor,
		DiseaseKey: "foot_rot", AgeBand: domain.AgeBandAdult, Protocol: renamed,
		IdempotencyKey: "rn", RequestFingerprint: "fp-rn",
	}); err != nil {
		t.Fatalf("save rename: %v", err)
	}
	assertHealthCount(t, ctx, pool, "both bands carry the new name",
		`SELECT count(*)::int FROM health_protocol_versions WHERE tenant_id=$1::uuid AND disease_key='foot_rot' AND display_name='Footrot'`,
		2, healthTenant)
	// The KEY is unchanged, so every case ever opened under it still joins.
	assertHealthCount(t, ctx, pool, "the disease key is unchanged",
		`SELECT count(*)::int FROM health_protocol_versions WHERE tenant_id=$1::uuid AND disease_key='foot_rot'`,
		2, healthTenant)
}

func strPtr(s string) *string { return &s }
