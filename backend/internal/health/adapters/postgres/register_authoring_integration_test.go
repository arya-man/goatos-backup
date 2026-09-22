package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	healthapp "github.com/vgoats/goatos/backend/internal/health/app"
	"github.com/vgoats/goatos/backend/internal/health/diagnosis"
	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/health/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// The authored diagnosis register against a real database.
//
// These run through the SERVICE, not the repository alone, because the service is what
// the handler calls -- and because the one check that matters most here, a rule's
// treats against the protocol catalog, lives in the publish TRANSACTION and cannot be
// reached any other way.

func registerService(pool *pgxpool.Pool) *healthapp.RegisterConfigService {
	return healthapp.NewRegisterConfigService(NewRepository(pool, 10*time.Second))
}

func seedRegisters(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := NewRepository(pool, 10*time.Second).
		SeedPublishedRegisters(ctx, healthTenant, healthActor); err != nil {
		t.Fatalf("seed registers: %v", err)
	}
}

func versionCmd(id, key string) domain.RegisterVersionCommand {
	return domain.RegisterVersionCommand{
		TenantID: healthTenant, ActorID: healthActor, RegisterVersionID: id,
		IdempotencyKey: key, RequestFingerprint: "fp-" + key,
	}
}

// The whole authored lifecycle on the production path: seed -> open a draft -> edit ->
// publish -> confirm the swap IN THE DATABASE rather than in the return value.
func TestRegisterAuthoringLifecycle(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	seedRegisters(t, ctx, pool)

	svc := registerService(pool)

	live, err := svc.RegisterFor(ctx, healthTenant, diagnosis.ClassAdult)
	if err != nil {
		t.Fatal(err)
	}
	if len(live.Questions) == 0 || len(live.Rules) == 0 {
		t.Fatal("the seeded register is empty")
	}

	draft, err := svc.GetDraftForEdit(ctx, versionCmd("", "open-1"), diagnosis.ClassAdult)
	if err != nil {
		t.Fatal(err)
	}
	if draft.Status != "draft" {
		t.Fatalf("status = %q", draft.Status)
	}
	// An edit begins from what is LIVE, not from a blank form.
	if len(draft.Document.Questions) != len(live.Questions) {
		t.Fatalf("the draft is not a copy of the published register: %d vs %d",
			len(draft.Document.Questions), len(live.Questions))
	}

	// Add a symptom and the rule that reads it -- the act this whole feature exists
	// for, and the one that used to be a four-file change across two languages.
	doc := draft.Document
	doc.Questions = append(doc.Questions, diagnosis.Question{
		ID: "ear_droop", Kind: diagnosis.QuestionChoice, Title: "Drooping ear",
		Options: []diagnosis.Option{
			{Value: "no", Label: "No"},
			{Value: "yes", Label: "Yes", Emits: []string{"ear:droop"}},
		},
	})
	doc.Rules = append(doc.Rules, diagnosis.Rule{
		ID: "EAR_INFECTION", Kind: diagnosis.KindProblem,
		Pathognomonic: []diagnosis.Clause{{Findings: []string{"ear:droop"}}},
		SeverityBase:  2, ExitType: "F", Containment: "home",
	})

	if _, err := svc.SaveDraft(ctx, domain.SaveRegisterDraftCommand{
		TenantID: healthTenant, ActorID: healthActor, AnimalClass: diagnosis.ClassAdult,
		Document: doc, IdempotencyKey: "save-1", RequestFingerprint: "fp-save-1",
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	res, err := svc.PublishDraft(ctx, versionCmd(draft.RegisterVersionID, "publish-1"))
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if res.Outcome != "published" || res.RetiredVersionID == "" {
		t.Fatalf("publish did not swap the live register: %+v", res)
	}

	// ONE live register per class. Two would make the engine's choice a sort order.
	var published int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM health_diagnosis_register_versions
WHERE tenant_id=$1::uuid AND animal_class=$2 AND status='published'`,
		healthTenant, diagnosis.ClassAdult).Scan(&published); err != nil {
		t.Fatal(err)
	}
	if published != 1 {
		t.Fatalf("published registers for adult = %d, want exactly 1", published)
	}

	// The new symptom now diagnoses, which is the end-to-end point.
	after, err := svc.RegisterFor(ctx, healthTenant, diagnosis.ClassAdult)
	if err != nil {
		t.Fatal(err)
	}
	ev := after.Evidence(diagnosis.Animal{Class: diagnosis.ClassAdult, Sex: "F"},
		diagnosis.Answers{"ear_droop": {Values: []string{"yes"}}}, nil)
	if !ev["ear:droop"] {
		t.Fatal("a symptom authored on the screen did not reach the evidence set")
	}
}

// A SAVE whose content matches the draft writes no new version. Without this, an editor
// that saves as the author types produces a version history a reviewer cannot read --
// and the history is how a rule change is audited.
func TestAnIdenticalRegisterSaveIsNotANewVersion(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	seedRegisters(t, ctx, pool)
	svc := registerService(pool)

	draft, err := svc.GetDraftForEdit(ctx, versionCmd("", "open-1"), diagnosis.ClassAdult)
	if err != nil {
		t.Fatal(err)
	}
	res, err := svc.SaveDraft(ctx, domain.SaveRegisterDraftCommand{
		TenantID: healthTenant, ActorID: healthActor, AnimalClass: diagnosis.ClassAdult,
		Document: draft.Document, IdempotencyKey: "save-same", RequestFingerprint: "fp-same",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != "unchanged" {
		t.Fatalf("outcome = %q, want unchanged", res.Outcome)
	}
}

// THE CHECK THAT NEEDS A DATABASE, and the correction that running it forced.
//
// Both cases REPORT and neither refuses. The first version made an unmatched treats key
// fatal, on the reasoning that it could only be a typo -- and the seeded register itself
// then failed to publish, because EIGHT of its own rules name diseases with no row in
// the farm's catalog. Nothing mechanical separates a typo from a disease nobody has
// authored yet: both produce no match. So the author is told the consequence -- the
// diagnosis fires and opens no course -- and decides.
func TestAnUnwritableTreatsWarnsRatherThanRefusingThePublish(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	seedRegisters(t, ctx, pool)
	svc := registerService(pool)

	// A disease that exists in the catalog but has no published protocol in either band.
	if _, err := authoringService(pool).CreateDisease(ctx, createDiseaseCmd("Ear infection", "ear")); err != nil {
		t.Fatal(err)
	}

	edit := func(t *testing.T, treats, key string) (domain.RegisterAuthoringResult, error) {
		t.Helper()
		draft, err := svc.GetDraftForEdit(ctx, versionCmd("", "open-"+key), diagnosis.ClassAdult)
		if err != nil {
			t.Fatal(err)
		}
		doc := draft.Document
		doc.Questions = append(doc.Questions, diagnosis.Question{
			ID: "ear_droop", Kind: diagnosis.QuestionChoice, Title: "Drooping ear",
			Options: []diagnosis.Option{
				{Value: "no", Label: "No"},
				{Value: "yes", Label: "Yes", Emits: []string{"ear:droop"}},
			},
		})
		doc.Rules = append(doc.Rules, diagnosis.Rule{
			ID: "EAR_INFECTION", Kind: diagnosis.KindProblem, Treats: treats,
			Pathognomonic: []diagnosis.Clause{{Findings: []string{"ear:droop"}}},
			SeverityBase:  2, ExitType: "F", Containment: "home",
		})
		if _, err := svc.SaveDraft(ctx, domain.SaveRegisterDraftCommand{
			TenantID: healthTenant, ActorID: healthActor, AnimalClass: diagnosis.ClassAdult,
			Document: doc, IdempotencyKey: "save-" + key, RequestFingerprint: "fp-save-" + key,
		}); err != nil {
			t.Fatal(err)
		}
		return svc.PublishDraft(ctx, versionCmd(draft.RegisterVersionID, "publish-"+key))
	}

	for _, c := range []struct{ treats, key, why string }{
		{"not_a_disease_at_all", "nocatalog", "a disease nobody has authored"},
		{"ear_infection", "unwritten", "a disease whose course nobody has written"},
	} {
		res, err := edit(t, c.treats, c.key)
		if err != nil {
			t.Fatalf("%s must publish with a warning, not a refusal: %v", c.why, err)
		}
		var warned bool
		for _, w := range res.Warnings {
			if !w.Fatal {
				warned = true
			}
		}
		if !warned {
			t.Fatalf("%s must be reported to the author", c.why)
		}
	}
}

// Two authors, one register. Without the one-draft index the second publish retires the
// first author's version seconds after it went live, with no error and with a live rule
// table nobody reviewed.
func TestOnlyOneDraftPerRegisterIsOpenAtATime(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	seedRegisters(t, ctx, pool)
	svc := registerService(pool)

	first, err := svc.GetDraftForEdit(ctx, versionCmd("", "open-a"), diagnosis.ClassAdult)
	if err != nil {
		t.Fatal(err)
	}
	// A second open returns the SAME draft rather than a second one -- which is what
	// lets the same author close the tab and come back.
	second, err := svc.GetDraftForEdit(ctx, versionCmd("", "open-b"), diagnosis.ClassAdult)
	if err != nil {
		t.Fatal(err)
	}
	if first.RegisterVersionID != second.RegisterVersionID {
		t.Fatalf("two drafts were opened: %s and %s", first.RegisterVersionID, second.RegisterVersionID)
	}

	var drafts int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM health_diagnosis_register_versions
WHERE tenant_id=$1::uuid AND animal_class=$2 AND status='draft'`,
		healthTenant, diagnosis.ClassAdult).Scan(&drafts); err != nil {
		t.Fatal(err)
	}
	if drafts != 1 {
		t.Fatalf("open drafts = %d, want 1", drafts)
	}
}

// Re-seeding a farm that has edited its rules must not retire the vet's work and
// replace it with the shipped table.
func TestReSeedingLeavesAnEditedRegisterAlone(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	seedRegisters(t, ctx, pool)

	repo := NewRepository(pool, 10*time.Second)
	before, err := repo.PublishedRegister(ctx, healthTenant, diagnosis.ClassAdult)
	if err != nil {
		t.Fatal(err)
	}

	results, err := repo.SeedPublishedRegisters(ctx, healthTenant, healthActor)
	if err != nil {
		t.Fatal(err)
	}
	for _, res := range results {
		if res.Outcome != "unchanged" {
			t.Errorf("%s: re-seeding wrote a new register (%s)", res.AnimalClass, res.Outcome)
		}
	}

	after, err := repo.PublishedRegister(ctx, healthTenant, diagnosis.ClassAdult)
	if err != nil {
		t.Fatal(err)
	}
	if before.RegisterVersionID != after.RegisterVersionID {
		t.Fatal("re-seeding replaced the live register")
	}
}

// A published register is immutable and a retired one is history; editing either would
// rewrite what animals were actually diagnosed against.
func TestOnlyADraftCanBePublishedOrDiscarded(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	seedRegisters(t, ctx, pool)
	svc := registerService(pool)

	live, err := NewRepository(pool, 10*time.Second).PublishedRegister(ctx, healthTenant, diagnosis.ClassAdult)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PublishDraft(ctx, versionCmd(live.RegisterVersionID, "p")); !errors.Is(err, ports.ErrRegisterNotADraft) {
		t.Fatalf("publishing a live register: want ErrRegisterNotADraft, got %v", err)
	}
	if _, err := svc.DiscardDraft(ctx, versionCmd(live.RegisterVersionID, "d")); !errors.Is(err, ports.ErrRegisterNotADraft) {
		t.Fatalf("discarding a live register: want ErrRegisterNotADraft, got %v", err)
	}
}
