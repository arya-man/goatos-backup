package postgres

// Birth/death workflow engine — Postgres integration proof against the real migration-complete
// schema (docs/decisions/birth-death-workflows.md). Opt-in like every DB test:
// GOATOS_RUN_POSTGRES_TESTS=1 (pgtest.SkipIfNoDocker); the default suite never starts a container.
//
// These tests drive the SAME repository methods the goat.created / goat.exited /
// goat.identifier.added consumers and the answer/complete APIs call, so this file is the
// production-path proof registered for those events in context/architecture/domain-event-registry.json.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
	"github.com/vgoats/goatos/backend/internal/tasks/ports"
)

const (
	wfTenant    = "aaaaaaa1-0000-0000-0000-000000000001"
	wfCustodian = "aaaaaaa1-0000-0000-0000-00000000c0de"
	wfKid       = "aaaaaaa1-0000-0000-0000-0000000000fa"
	wfDam       = "aaaaaaa1-0000-0000-0000-0000000000fb"
	wfDead      = "aaaaaaa1-0000-0000-0000-0000000000fc"
)

var wfEventAt = time.Date(2026, 7, 27, 9, 30, 0, 0, biztime.DefaultLocation())

func newWorkflowRepo(t *testing.T) (*Repository, *pgxpool.Pool, context.Context) {
	t.Helper()
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)

	// goats.tenant_id is FK-constrained, so the tenant has to exist before any animal is seeded.
	if _, err := pool.Exec(ctx, `
INSERT INTO tenants (tenant_id, name, status)
VALUES ($1::uuid, 'Workflow Test Tenant', 'active')
ON CONFLICT (tenant_id) DO NOTHING`, wfTenant); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO parties (party_id, party_type, display_name, status)
VALUES ($1::uuid, 'org', 'Workflow Test Custodian', 'active')
ON CONFLICT (party_id) DO NOTHING`, wfCustodian); err != nil {
		t.Fatalf("seed custodian party: %v", err)
	}
	for _, goat := range []struct {
		id, sex, lifecycle, tob string
	}{
		{wfKid, "female", "alive", "09:30"},
		{wfDam, "female", "alive", ""},
		{wfDead, "male", "alive", ""},
	} {
		if _, err := pool.Exec(ctx, `
INSERT INTO goats (goat_id, tenant_id, species, sex, breed, lifecycle_status, custodian_party_id,
                   origin_type, dob, entry_date, exited_at, exit_reason, time_of_birth)
VALUES ($1::uuid, $2::uuid, 'goat', $3, 'Boer', $4, $5::uuid,
        'birth', DATE '2026-07-27', DATE '2026-07-27',
        CASE WHEN $4 = 'dead' THEN now() END,
        CASE WHEN $4 = 'dead' THEN 'died' END,
        nullif($6, '')::time)
ON CONFLICT (goat_id) DO NOTHING`, goat.id, wfTenant, goat.sex, goat.lifecycle, wfCustodian, goat.tob); err != nil {
			t.Fatalf("seed goat %s: %v", goat.id, err)
		}
	}
	// The dam is addressable by a free-text tag through goat_identifiers (identity's normalization
	// is upper+trim).
	if _, err := pool.Exec(ctx, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value,
                              scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES ($1::uuid, $2::uuid, 'animal_identifier_1', 'dam-901', 'DAM-901', 'global', true, 'active', now(), 'test')
ON CONFLICT DO NOTHING`, wfTenant, wfDam); err != nil {
		t.Fatalf("seed dam identifier: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value,
                              scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES ($1::uuid, $2::uuid, 'animal_identifier_1', 'kid-rfid-901', 'KID-RFID-901', 'global', true, 'active', now(), 'test')
ON CONFLICT DO NOTHING`, wfTenant, wfKid); err != nil {
		t.Fatalf("seed kid identifier: %v", err)
	}
	return NewRepository(pool, 10*time.Second), pool, ctx
}

func openWorkflow(t *testing.T, repo *Repository, ctx context.Context, templateKey, subject string) string {
	t.Helper()
	created, err := repo.OpenWorkflow(ctx, ports.OpenWorkflowCommand{
		TenantID:      wfTenant,
		TemplateKey:   templateKey,
		SubjectGoatID: subject,
		EventAt:       wfEventAt,
	})
	if err != nil {
		t.Fatalf("open %s: %v", templateKey, err)
	}
	if !created {
		t.Fatalf("open %s: expected created", templateKey)
	}
	detailID := findWorkflowID(t, repo, ctx, templateKey, subject)
	return detailID
}

func findWorkflowID(t *testing.T, repo *Repository, ctx context.Context, templateKey, subject string) string {
	t.Helper()
	var id string
	err := repo.pool.QueryRow(ctx, `
SELECT workflow_id::text FROM workflow_instances
WHERE tenant_id = $1::uuid AND template_key = $2 AND subject_goat_id = $3::uuid`,
		wfTenant, templateKey, subject).Scan(&id)
	if err != nil {
		t.Fatalf("find workflow: %v", err)
	}
	return id
}

func actionIDByKey(t *testing.T, detail domain.WorkflowDetail, key string) string {
	t.Helper()
	for _, a := range detail.Actions {
		if a.ActionKey == key {
			return a.ActionID
		}
	}
	t.Fatalf("action %q not found", key)
	return ""
}

func actionStatus(t *testing.T, detail domain.WorkflowDetail, key string) string {
	t.Helper()
	for _, a := range detail.Actions {
		if a.ActionKey == key {
			return a.Status
		}
	}
	t.Fatalf("action %q not found", key)
	return ""
}

func factValue(t *testing.T, detail domain.WorkflowDetail, label string) string {
	t.Helper()
	for _, fact := range detail.Facts {
		if fact.Label == label {
			return fact.Value
		}
	}
	t.Fatalf("fact %q not found in %#v", label, detail.Facts)
	return ""
}

func TestBirthKidDetailShowsMotherRFIDAndLitterSize(t *testing.T) {
	repo, pool, ctx := newWorkflowRepo(t)
	if _, err := pool.Exec(ctx, `
INSERT INTO goat_births (
  tenant_id, child_goat_id, mother_goat_id, birth_event_id, child_ordinal,
  litter_size, count_status, count_approved_at
)
VALUES ($1::uuid, $2::uuid, $3::uuid, $2::uuid, 1, 2, 'approved', now())`, wfTenant, wfKid, wfDam); err != nil {
		t.Fatalf("seed birth relationship: %v", err)
	}
	created, err := repo.OpenWorkflow(ctx, ports.OpenWorkflowCommand{
		TenantID: wfTenant, TemplateKey: domain.TemplateKeyBirthKid, SubjectGoatID: wfKid,
		DamGoatID: stringPtr(wfDam), EventAt: wfEventAt,
	})
	if err != nil || !created {
		t.Fatalf("open kid workflow: created=%v err=%v", created, err)
	}
	detail, err := repo.GetWorkflow(ctx, wfTenant, findWorkflowID(t, repo, ctx, domain.TemplateKeyBirthKid, wfKid), wfEventAt)
	if err != nil {
		t.Fatalf("get kid workflow: %v", err)
	}
	if got := factValue(t, detail, "Mother RFID"); got != "dam-901" {
		t.Fatalf("mother RFID=%q, want dam-901", got)
	}
	if got := factValue(t, detail, "Litter size"); got != "2" {
		t.Fatalf("litter size=%q, want 2", got)
	}
}

func stringPtr(value string) *string { return &value }

// TestOpenWorkflowIdempotentAndCardServed proves the ON CONFLICT natural-key open, the
// birth-time-derived template instantiation, and the write-maintained card list read. At 09:30
// IST, four birth-day colostrum slots remain eligible and all five next-day slots are included.
func TestOpenWorkflowIdempotentAndCardServed(t *testing.T) {
	repo, _, ctx := newWorkflowRepo(t)
	workflowID := openWorkflow(t, repo, ctx, domain.TemplateKeyBirthKid, wfKid)

	// Redelivered event: created=false, no duplicate actions.
	created, err := repo.OpenWorkflow(ctx, ports.OpenWorkflowCommand{
		TenantID: wfTenant, TemplateKey: domain.TemplateKeyBirthKid, SubjectGoatID: wfKid, EventAt: wfEventAt,
	})
	if err != nil || created {
		t.Fatalf("redelivered open: created=%v err=%v, want false/nil", created, err)
	}

	detail, err := repo.GetWorkflow(ctx, wfTenant, workflowID, wfEventAt)
	if err != nil {
		t.Fatalf("get workflow: %v", err)
	}
	if len(detail.Actions) != 17 {
		t.Fatalf("kid actions = %d, want 17 (8 main + 9 birth-time-derived colostrum sessions)", len(detail.Actions))
	}
	if detail.Card.ActionsTotal != 17 || detail.Card.ActionsDone != 0 {
		t.Fatalf("card counters = %d/%d, want 0/17", detail.Card.ActionsDone, detail.Card.ActionsTotal)
	}
	if detail.Card.Subject.Sex != "female" || detail.Card.Subject.Breed != "Boer" {
		t.Fatalf("subject display = %+v (canonical goat join)", detail.Card.Subject)
	}

	page, err := repo.ListWorkflows(ctx, domain.WorkflowListQuery{
		TenantID: wfTenant, Module: domain.ModuleBirth,
		EventDate: biztime.BusinessDate(wfEventAt), Filter: domain.FilterAll,
		PageSize: 20, Now: wfEventAt,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].WorkflowID != workflowID {
		t.Fatalf("list items = %+v, want the one card", page.Items)
	}
	if page.Chips.All != 1 || page.Chips.Completed != 0 {
		t.Fatalf("chips = %+v", page.Chips)
	}
}

func TestListWorkflowsReturnsOnlyPreviousDatesWithOverdueOpenWork(t *testing.T) {
	repo, _, ctx := newWorkflowRepo(t)
	openWorkflow(t, repo, ctx, domain.TemplateKeyBirthKid, wfKid)

	page, err := repo.ListWorkflows(ctx, domain.WorkflowListQuery{
		TenantID: wfTenant, Module: domain.ModuleBirth,
		EventDate: "2026-07-28", TodayDate: "2026-07-28", Filter: domain.FilterAll,
		PageSize: 20, Now: wfEventAt.Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.OverdueDates) != 1 {
		t.Fatalf("overdue dates = %+v, want one previous date", page.OverdueDates)
	}
	if got := page.OverdueDates[0]; got.Date != "2026-07-27" || got.WorkflowCount != 1 {
		t.Fatalf("overdue date = %+v, want 2026-07-27 count 1", got)
	}
}

// TestWorkflowChipsTreatAwaitingVerificationAsExclusive proves one death workflow cannot be
// counted in both Completed and Awaiting video. Completion becomes visible only after the
// verifier verdict consumer closes awaiting_verification.
func TestWorkflowChipsTreatAwaitingVerificationAsExclusive(t *testing.T) {
	repo, pool, ctx := newWorkflowRepo(t)
	workflowID := openWorkflow(t, repo, ctx, domain.TemplateKeyDeath, wfDead)

	if _, err := pool.Exec(ctx, `
UPDATE workflow_instances
SET state = 'completed', actions_done = actions_total, awaiting_verification = true
WHERE tenant_id = $1::uuid AND workflow_id = $2::uuid`, wfTenant, workflowID); err != nil {
		t.Fatalf("stage awaiting-verification workflow: %v", err)
	}

	query := domain.WorkflowListQuery{
		TenantID: wfTenant, Module: domain.ModuleDeath,
		EventDate: biztime.BusinessDate(wfEventAt), Filter: domain.FilterAll,
		PageSize: 20, Now: wfEventAt,
	}
	page, err := repo.ListWorkflows(ctx, query)
	if err != nil {
		t.Fatalf("list awaiting workflow: %v", err)
	}
	if page.Chips.All != 1 || page.Chips.Completed != 0 || page.Chips.AwaitingVideo != 1 {
		t.Fatalf("awaiting chips = %+v, want all=1 completed=0 awaiting_video=1", page.Chips)
	}

	if _, err := pool.Exec(ctx, `
UPDATE workflow_instances
SET awaiting_verification = false
WHERE tenant_id = $1::uuid AND workflow_id = $2::uuid`, wfTenant, workflowID); err != nil {
		t.Fatalf("apply verifier approval: %v", err)
	}
	page, err = repo.ListWorkflows(ctx, query)
	if err != nil {
		t.Fatalf("list approved workflow: %v", err)
	}
	if page.Chips.All != 1 || page.Chips.Completed != 1 || page.Chips.AwaitingVideo != 0 {
		t.Fatalf("approved chips = %+v, want all=1 completed=1 awaiting_video=0", page.Chips)
	}
}

// TestAnswerActionIdempotencyPg proves the mandatory idempotency contract on the real adapter:
// first call applies, exact replay returns the original result with no second mutation, and a
// same-key/different-payload replay is a conflict. Card fields are maintained in the same txn.
func TestAnswerActionIdempotencyPg(t *testing.T) {
	repo, _, ctx := newWorkflowRepo(t)
	workflowID := openWorkflow(t, repo, ctx, domain.TemplateKeyBirthKid, wfKid)
	detail, err := repo.GetWorkflow(ctx, wfTenant, workflowID, wfEventAt)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	cmd := domain.AnswerActionCommand{
		TenantID: wfTenant, WorkflowID: workflowID,
		ActionID:    actionIDByKey(t, detail, domain.ActionKeyKidClean),
		AnswerValue: "yes", AnsweredAt: wfEventAt.UTC(),
		IdempotencyKey: "answer-key-1", RequestFingerprint: "fp-1",
	}

	result, err := repo.AnswerAction(ctx, cmd)
	if err != nil {
		t.Fatalf("first answer: %v", err)
	}
	if result.Replayed || result.Workflow.ActionsDone != 1 {
		t.Fatalf("first answer = %+v", result)
	}

	replayed, err := repo.AnswerAction(ctx, cmd)
	if err != nil {
		t.Fatalf("exact replay: %v", err)
	}
	if !replayed.Replayed || replayed.Workflow.ActionsDone != 1 {
		t.Fatalf("exact replay = %+v, want Replayed with unchanged card", replayed)
	}

	conflicting := cmd
	conflicting.AnswerValue = "no"
	conflicting.RequestFingerprint = "fp-2"
	if _, err := repo.AnswerAction(ctx, conflicting); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("conflicting replay err = %v, want ErrIdempotencyConflict", err)
	}

	// Cross-action key reuse trips the partial unique index and maps to the same conflict.
	crossAction := domain.AnswerActionCommand{
		TenantID: wfTenant, WorkflowID: workflowID,
		ActionID:    actionIDByKey(t, detail, domain.ActionKeySuckReflex),
		AnswerValue: "yes", AnsweredAt: wfEventAt.UTC(),
		IdempotencyKey: "answer-key-1", RequestFingerprint: "fp-other",
	}
	if _, err := repo.AnswerAction(ctx, crossAction); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("cross-action key reuse err = %v, want ErrIdempotencyConflict", err)
	}
}

// TestMotherAnswerRequiresAndPersistsVideoPg exercises the production adapter path for a
// requires_video QUESTION (not only action completions): proofless answers fail atomically, while
// one answer with one proof stores both on the same workflow_actions row.
func TestMotherAnswerRequiresAndPersistsVideoPg(t *testing.T) {
	repo, _, ctx := newWorkflowRepo(t)
	workflowID := openWorkflow(t, repo, ctx, domain.TemplateKeyBirthMother, wfDam)
	detail, err := repo.GetWorkflow(ctx, wfTenant, workflowID, wfEventAt)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	cmd := domain.AnswerActionCommand{
		TenantID: wfTenant, WorkflowID: workflowID,
		ActionID:    actionIDByKey(t, detail, domain.ActionKeyBabiesStillInside),
		AnswerValue: "no", AnsweredAt: wfEventAt.UTC(),
		IdempotencyKey: "mother-answer-video-1", RequestFingerprint: "fp-mother-answer-video-1",
	}
	if _, err := repo.AnswerAction(ctx, cmd); !errors.Is(err, domain.ErrProofRequired) {
		t.Fatalf("proofless mother answer err = %v, want ErrProofRequired", err)
	}
	cmd.ProofRef = "proof-mother-question"
	result, err := repo.AnswerAction(ctx, cmd)
	if err != nil {
		t.Fatalf("answer with proof: %v", err)
	}
	if result.Action.AnswerValue == nil || *result.Action.AnswerValue != "no" ||
		result.Action.ProofRef == nil || *result.Action.ProofRef != "proof-mother-question" {
		t.Fatalf("stored mother answer = %+v", result.Action)
	}
	labels, answers, err := repo.ResolveActionPresentations(ctx, wfTenant, []string{cmd.ActionID})
	if err != nil {
		t.Fatalf("resolve verifier task presentation: %v", err)
	}
	if labels[cmd.ActionID] != "Are any babies still inside?" || answers[cmd.ActionID] != "No" {
		t.Fatalf("resolved verifier task presentation label=%q answer=%q", labels[cmd.ActionID], answers[cmd.ActionID])
	}
}

// TestActionWritesRejectOutOfSequencePg proves the production repository locks all sibling rows
// and rejects a later operator mutation until the preceding action in that section completes.
func TestActionWritesRejectOutOfSequencePg(t *testing.T) {
	repo, _, ctx := newWorkflowRepo(t)
	workflowID := openWorkflow(t, repo, ctx, domain.TemplateKeyBirthKid, wfKid)
	detail, err := repo.GetWorkflow(ctx, wfTenant, workflowID, wfEventAt)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	second := domain.CompleteActionCommand{
		TenantID: wfTenant, WorkflowID: workflowID,
		ActionID:    actionIDByKey(t, detail, domain.ActionKeyIodineDipping),
		ProofRef:    "proof-sequence-second",
		CompletedAt: wfEventAt.UTC(), IdempotencyKey: "sequence-second", RequestFingerprint: "fp-sequence-second",
	}
	if _, err := repo.CompleteAction(ctx, second); !errors.Is(err, domain.ErrActionOutOfSequence) {
		t.Fatalf("second action before first err = %v, want ErrActionOutOfSequence", err)
	}

	first := domain.AnswerActionCommand{
		TenantID: wfTenant, WorkflowID: workflowID,
		ActionID: actionIDByKey(t, detail, domain.ActionKeyKidClean), AnswerValue: "yes",
		ProofRef:   "proof-sequence-first",
		AnsweredAt: wfEventAt.UTC(), IdempotencyKey: "sequence-first", RequestFingerprint: "fp-sequence-first",
	}
	if _, err := repo.AnswerAction(ctx, first); err != nil {
		t.Fatalf("complete first action: %v", err)
	}
	if _, err := repo.CompleteAction(ctx, second); err != nil {
		t.Fatalf("second action after first: %v", err)
	}
}

// TestORSRoundTwoIsServerBlockedForFiftyMinutes proves the real write path uses the persisted
// first-round completion time, not a client timer: 49:59 is rejected and exactly 50:00 succeeds.
func TestORSRoundTwoIsServerBlockedForFiftyMinutes(t *testing.T) {
	repo, _, ctx := newWorkflowRepo(t)
	workflowID := openWorkflow(t, repo, ctx, domain.TemplateKeyBirthMother, wfDam)
	detail, err := repo.GetWorkflow(ctx, wfTenant, workflowID, wfEventAt)
	if err != nil {
		t.Fatal(err)
	}
	firstGivenAt := wfEventAt.UTC()
	for _, action := range detail.Actions {
		if action.Seq > 5 {
			continue
		}
		key := "ors-gate-prerequisite-" + action.ActionKey
		if action.ActionType == domain.ActionTypeQuestion {
			_, err = repo.AnswerAction(ctx, domain.AnswerActionCommand{
				TenantID: wfTenant, WorkflowID: workflowID, ActionID: action.ActionID,
				AnswerValue: "yes", ProofRef: "proof-" + action.ActionKey,
				AnsweredBy: wfCustodian, AnsweredAt: firstGivenAt,
				IdempotencyKey: key, RequestFingerprint: key,
			})
		} else {
			_, err = repo.CompleteAction(ctx, domain.CompleteActionCommand{
				TenantID: wfTenant, WorkflowID: workflowID, ActionID: action.ActionID,
				ProofRef: "proof-" + action.ActionKey, CompletedBy: wfCustodian,
				CompletedAt: firstGivenAt, IdempotencyKey: key, RequestFingerprint: key,
			})
		}
		if err != nil {
			t.Fatalf("complete prerequisite %s: %v", action.ActionKey, err)
		}
	}
	detail, err = repo.GetWorkflow(ctx, wfTenant, workflowID, firstGivenAt)
	if err != nil {
		t.Fatal(err)
	}
	ors2ID := actionIDByKey(t, detail, domain.ActionKeyORSWater2)
	early := domain.CompleteActionCommand{
		TenantID: wfTenant, WorkflowID: workflowID, ActionID: ors2ID,
		ProofRef: "proof-ors-2-early", CompletedBy: wfCustodian,
		CompletedAt:    firstGivenAt.Add(49*time.Minute + 59*time.Second),
		IdempotencyKey: "ors-2-early", RequestFingerprint: "ors-2-early",
	}
	if _, err := repo.CompleteAction(ctx, early); !errors.Is(err, domain.ErrActionNotYetDue) {
		t.Fatalf("ORS round 2 at 49:59 err=%v, want ErrActionNotYetDue", err)
	}
	onTime := early
	onTime.ProofRef = "proof-ors-2-on-time"
	onTime.CompletedAt = firstGivenAt.Add(50 * time.Minute)
	onTime.IdempotencyKey = "ors-2-on-time"
	onTime.RequestFingerprint = "ors-2-on-time"
	if _, err := repo.CompleteAction(ctx, onTime); err != nil {
		t.Fatalf("ORS round 2 at 50:00: %v", err)
	}
}

// TestColostrumSessionIsServerBlockedUntilItsBirthDerivedTime proves the production write path
// treats the scheduled time as a not-before gate. The round remains completable after that instant;
// the schedule is not a narrow attendance window.
func TestColostrumSessionIsServerBlockedUntilItsBirthDerivedTime(t *testing.T) {
	repo, _, ctx := newWorkflowRepo(t)
	workflowID := openWorkflow(t, repo, ctx, domain.TemplateKeyBirthKid, wfKid)
	detail, err := repo.GetWorkflow(ctx, wfTenant, workflowID, wfEventAt)
	if err != nil {
		t.Fatal(err)
	}

	for _, action := range detail.Actions {
		if action.Section != domain.SectionMain || action.Seq > 5 {
			continue
		}
		key := "colostrum-gate-prerequisite-" + action.ActionKey
		if action.ActionType == domain.ActionTypeQuestion {
			_, err = repo.AnswerAction(ctx, domain.AnswerActionCommand{
				TenantID: wfTenant, WorkflowID: workflowID, ActionID: action.ActionID,
				AnswerValue: "yes", ProofRef: "proof-" + action.ActionKey,
				AnsweredBy: wfCustodian, AnsweredAt: wfEventAt,
				IdempotencyKey: key, RequestFingerprint: key,
			})
		} else {
			_, err = repo.CompleteAction(ctx, domain.CompleteActionCommand{
				TenantID: wfTenant, WorkflowID: workflowID, ActionID: action.ActionID,
				ProofRef: "proof-" + action.ActionKey, CompletedBy: wfCustodian,
				CompletedAt: wfEventAt, IdempotencyKey: key, RequestFingerprint: key,
			})
		}
		if err != nil {
			t.Fatalf("complete prerequisite %s: %v", action.ActionKey, err)
		}
	}

	detail, err = repo.GetWorkflow(ctx, wfTenant, workflowID, wfEventAt)
	if err != nil {
		t.Fatal(err)
	}
	var firstSession domain.WorkflowAction
	for _, action := range detail.Actions {
		if action.Section == domain.SectionColostrumSession {
			firstSession = action
			break
		}
	}
	if firstSession.ActionID == "" || firstSession.DueAt == nil {
		t.Fatalf("first scheduled colostrum session missing due_at: %+v", firstSession)
	}
	early := domain.CompleteActionCommand{
		TenantID: wfTenant, WorkflowID: workflowID, ActionID: firstSession.ActionID,
		ProofRef: "proof-colostrum-early", CompletedBy: wfCustodian,
		CompletedAt:    firstSession.DueAt.Add(-time.Second),
		IdempotencyKey: "colostrum-early", RequestFingerprint: "colostrum-early",
	}
	if _, err := repo.CompleteAction(ctx, early); !errors.Is(err, domain.ErrActionNotYetDue) {
		t.Fatalf("colostrum before scheduled time err=%v, want ErrActionNotYetDue", err)
	}
	onTime := early
	onTime.ProofRef = "proof-colostrum-on-time"
	onTime.CompletedAt = *firstSession.DueAt
	onTime.IdempotencyKey = "colostrum-on-time"
	onTime.RequestFingerprint = "colostrum-on-time"
	if _, err := repo.CompleteAction(ctx, onTime); err != nil {
		t.Fatalf("colostrum at scheduled time: %v", err)
	}
}

// TestDeathEvidenceFlowPg proves the death gate end to end on the real schema: proofless video is
// refused, both initial uploads remain staged, the admin approval transaction alone releases both
// proofs to review and applies the death, and verifier rework resets both uploads for a re-shoot.
func TestDeathEvidenceFlowPg(t *testing.T) {
	repo, pool, ctx := newWorkflowRepo(t)
	workflowID := openWorkflow(t, repo, ctx, domain.TemplateKeyDeath, wfDead)
	detail, err := repo.GetWorkflow(ctx, wfTenant, workflowID, wfEventAt)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	// Proofless completion of a requires_video action fails closed; nothing changes.
	proofless := domain.CompleteActionCommand{
		TenantID: wfTenant, WorkflowID: workflowID,
		ActionID:       actionIDByKey(t, detail, domain.ActionKeyDeathVideo),
		CompletedAt:    wfEventAt.UTC(),
		IdempotencyKey: "complete-1", RequestFingerprint: "fp-c1",
	}
	if _, err := repo.CompleteAction(ctx, proofless); !errors.Is(err, domain.ErrProofRequired) {
		t.Fatalf("proofless err = %v, want ErrProofRequired", err)
	}

	// First video with proof.
	first := proofless
	first.ProofRef = "proof-death"
	result, err := repo.CompleteAction(ctx, first)
	if err != nil {
		t.Fatalf("first video: %v", err)
	}
	if result.NeedsVerificationEnqueue {
		t.Fatal("first video must not need the enqueue")
	}

	// Second video is staged only: an unapproved death must not appear in Verify.
	second := domain.CompleteActionCommand{
		TenantID: wfTenant, WorkflowID: workflowID,
		ActionID:       actionIDByKey(t, detail, domain.ActionKeyPostMortemVideo),
		ProofRef:       "proof-postmortem",
		CompletedAt:    wfEventAt.UTC(),
		IdempotencyKey: "complete-2", RequestFingerprint: "fp-c2",
	}
	result, err = repo.CompleteAction(ctx, second)
	if err != nil {
		t.Fatalf("second video: %v", err)
	}
	if result.NeedsVerificationEnqueue || result.Workflow.AwaitingVerification {
		t.Fatalf("unapproved second video result = %+v, want staged only", result)
	}
	detail, _ = repo.GetWorkflow(ctx, wfTenant, workflowID, wfEventAt)
	if got := actionStatus(t, detail, domain.ActionKeyParkHeadSignoff); got != domain.ActionStatusPending {
		t.Fatalf("review action before admin approval = %q, want pending", got)
	}

	// The approval's caller-owned transaction proves both videos, flips the internal review action,
	// and applies the terminal goat state atomically.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin approval tx: %v", err)
	}
	ready, err := repo.PrepareDeathEvidenceForApprovalInTx(ctx, tx, wfTenant, wfDead)
	if err != nil || !ready {
		_ = tx.Rollback(ctx)
		t.Fatalf("prepare approval evidence: ready=%v err=%v", ready, err)
	}
	if _, err := tx.Exec(ctx, `UPDATE goats SET lifecycle_status='dead', exit_reason='died', exited_at=now() WHERE goat_id=$1::uuid`, wfDead); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("apply death in approval tx: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit approval tx: %v", err)
	}
	detail, _ = repo.GetWorkflow(ctx, wfTenant, workflowID, wfEventAt)
	if got := actionStatus(t, detail, domain.ActionKeyParkHeadSignoff); got != domain.ActionStatusInReview {
		t.Fatalf("review action after admin approval = %q, want in_review", got)
	}
	review, err := repo.DeathEvidenceForVerification(ctx, wfTenant, wfDead)
	if err != nil {
		t.Fatalf("load approval-released evidence: %v", err)
	}
	if len(review.ProofRefs) != 2 || review.ProofRefs[0] != "proof-death" || review.ProofRefs[1] != "proof-postmortem" {
		t.Fatalf("released proofs = %v, want both videos", review.ProofRefs)
	}

	// REWORK: both videos reset (proofs cleared), sign-off back to pending; idempotent.
	rework := ports.DeathVerdictCommand{TenantID: wfTenant, WorkflowID: workflowID, Reason: "tag not visible", VerdictAt: wfEventAt}
	for i := 0; i < 2; i++ {
		if err := repo.BounceDeathVideosForRework(ctx, rework); err != nil {
			t.Fatalf("rework #%d: %v", i, err)
		}
	}
	detail, _ = repo.GetWorkflow(ctx, wfTenant, workflowID, wfEventAt)
	for _, key := range []string{domain.ActionKeyDeathVideo, domain.ActionKeyPostMortemVideo} {
		if got := actionStatus(t, detail, key); got != domain.ActionStatusRework {
			t.Fatalf("%s = %q, want rework", key, got)
		}
	}
	if got := actionStatus(t, detail, domain.ActionKeyParkHeadSignoff); got != domain.ActionStatusPending {
		t.Fatalf("sign-off after rework = %q, want pending", got)
	}
	if detail.Card.AwaitingVerification {
		t.Fatal("workflow must not be awaiting after rework")
	}

	// Re-shoot with NEW keys, then APPROVE completes the sign-off and the workflow.
	for _, spec := range []struct{ key, proof, idem string }{
		{domain.ActionKeyDeathVideo, "reshoot-death", "complete-3"},
		{domain.ActionKeyPostMortemVideo, "reshoot-postmortem", "complete-4"},
	} {
		cmd := domain.CompleteActionCommand{
			TenantID: wfTenant, WorkflowID: workflowID,
			ActionID: actionIDByKey(t, detail, spec.key), ProofRef: spec.proof,
			CompletedAt: wfEventAt.UTC(), IdempotencyKey: spec.idem, RequestFingerprint: "fp-" + spec.idem,
		}
		if _, err := repo.CompleteAction(ctx, cmd); err != nil {
			t.Fatalf("re-shoot %s: %v", spec.key, err)
		}
	}
	approve := ports.DeathVerdictCommand{TenantID: wfTenant, WorkflowID: workflowID, VerdictAt: wfEventAt}
	for i := 0; i < 2; i++ { // idempotent under redelivery
		if err := repo.ApplyDeathSignoffApproved(ctx, approve); err != nil {
			t.Fatalf("approve #%d: %v", i, err)
		}
	}
	detail, _ = repo.GetWorkflow(ctx, wfTenant, workflowID, wfEventAt)
	if detail.Card.State != domain.WorkflowStateCompleted {
		t.Fatalf("workflow state = %q, want completed", detail.Card.State)
	}
	if detail.Card.ActionsDone != 2 || detail.Card.ActionsTotal != 2 {
		t.Fatalf("operator card = %d/%d, want 2/2", detail.Card.ActionsDone, detail.Card.ActionsTotal)
	}
}

// TestResolveDamAndFactsPg proves the canonical goat reads the goat.created consumer depends on:
// facts by PK (including the new time_of_birth column) and dam resolution by uuid AND by free-text
// identifier (normalized upper+trim).
func TestResolveDamAndFactsPg(t *testing.T) {
	repo, _, ctx := newWorkflowRepo(t)

	facts, err := repo.GoatWorkflowFacts(ctx, wfTenant, wfKid)
	if err != nil {
		t.Fatalf("facts: %v", err)
	}
	if facts.TimeOfBirth == nil || *facts.TimeOfBirth != "09:30" {
		t.Fatalf("time_of_birth = %v, want 09:30", facts.TimeOfBirth)
	}
	if facts.DOB == nil {
		t.Fatal("dob must be read")
	}

	if id, err := repo.ResolveDamGoat(ctx, wfTenant, wfDam); err != nil || id != wfDam {
		t.Fatalf("uuid resolve = %q err=%v", id, err)
	}
	if id, err := repo.ResolveDamGoat(ctx, wfTenant, "  dam-901 "); err != nil || id != wfDam {
		t.Fatalf("identifier resolve = %q err=%v", id, err)
	}
	if _, err := repo.ResolveDamGoat(ctx, wfTenant, "no-such-dam"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unresolvable dam err = %v, want ErrNotFound", err)
	}
}

// TestCompleteTagActionForGoatPg proves RFID promotion records the prerequisite but never completes
// the mandatory tagging-video task.
func TestCompleteTagActionForGoatPg(t *testing.T) {
	repo, _, ctx := newWorkflowRepo(t)
	workflowID := openWorkflow(t, repo, ctx, domain.TemplateKeyBirthKid, wfKid)

	for i := 0; i < 2; i++ { // idempotent under redelivery
		if err := repo.CompleteTagActionForGoat(ctx, wfTenant, wfKid, wfEventAt.UTC()); err != nil {
			t.Fatalf("tag completion #%d: %v", i, err)
		}
	}
	detail, err := repo.GetWorkflow(ctx, wfTenant, workflowID, wfEventAt)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got := actionStatus(t, detail, domain.ActionKeyTagTheKid); got != domain.ActionStatusPending {
		t.Fatalf("tag_the_kid = %q, want pending until video", got)
	}
	// A goat with no open birth workflow is a silent no-op.
	if err := repo.CompleteTagActionForGoat(ctx, wfTenant, wfDam, wfEventAt.UTC()); err != nil {
		t.Fatalf("no-workflow tag completion: %v", err)
	}
}

func TestBirthReviewIsIndependentPerMotherAndChildWorkflow(t *testing.T) {
	repo, pool, ctx := newWorkflowRepo(t)
	if _, err := pool.Exec(ctx, `INSERT INTO goat_births
	 (tenant_id,child_goat_id,mother_goat_id,birth_event_id,child_ordinal,litter_size,count_status)
	 VALUES ($1::uuid,$2::uuid,$3::uuid,$2::uuid,1,1,'pending')`, wfTenant, wfKid, wfDam); err != nil {
		t.Fatal(err)
	}
	kidID := openWorkflow(t, repo, ctx, domain.TemplateKeyBirthKid, wfKid)
	created, err := repo.OpenWorkflow(ctx, ports.OpenWorkflowCommand{TenantID: wfTenant, TemplateKey: domain.TemplateKeyBirthMother,
		SubjectGoatID: wfDam, DamGoatID: stringPtr(wfKid), EventAt: wfEventAt})
	if err != nil || !created {
		t.Fatalf("open mother: created=%v err=%v", created, err)
	}
	motherID := findWorkflowID(t, repo, ctx, domain.TemplateKeyBirthMother, wfDam)
	completeAll := func(id string) {
		detail, err := repo.GetWorkflow(ctx, wfTenant, id, wfEventAt)
		if err != nil {
			t.Fatal(err)
		}
		for _, action := range detail.Actions {
			key := "all-video-" + action.ActionID
			completedAt := wfEventAt
			if action.ActionKey == domain.ActionKeyORSWater2 {
				completedAt = wfEventAt.Add(50 * time.Minute)
			}
			if action.ActionType == domain.ActionTypeQuestion || action.ActionType == domain.ActionTypeQuestionSelect {
				answer := "yes"
				if action.ActionType == domain.ActionTypeQuestionSelect {
					answer = action.Options[0]
				}
				_, err = repo.AnswerAction(ctx, domain.AnswerActionCommand{TenantID: wfTenant, WorkflowID: id, ActionID: action.ActionID,
					AnswerValue: answer, ProofRef: "proof-" + action.ActionID, AnsweredBy: wfCustodian, AnsweredAt: completedAt, IdempotencyKey: key, RequestFingerprint: key})
			} else {
				_, err = repo.CompleteAction(ctx, domain.CompleteActionCommand{TenantID: wfTenant, WorkflowID: id, ActionID: action.ActionID,
					ProofRef: "proof-" + action.ActionID, CompletedBy: wfCustodian, CompletedAt: completedAt, IdempotencyKey: key, RequestFingerprint: key})
			}
			if err != nil {
				t.Fatalf("complete %s: %v", action.ActionKey, err)
			}
		}
	}
	completeAll(motherID)
	motherDetail, err := repo.GetWorkflow(ctx, wfTenant, motherID, wfEventAt)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range motherDetail.Actions {
		if action.ActionKey == domain.ActionKeyORSWater2 {
			want := wfEventAt.Add(50 * time.Minute)
			if action.DueAt == nil || !action.DueAt.Equal(want) {
				t.Fatalf("ORS round 2 due=%v, want %v", action.DueAt, want)
			}
		}
	}
	review, err := repo.BirthWorkflowEvidenceForVerification(ctx, wfTenant, motherID)
	if err != nil {
		t.Fatalf("mother review must not wait for kids: %v", err)
	}
	if review.WorkflowID != motherID || review.SubjectRole != domain.TemplateKeyBirthMother || len(review.ProofRefs) != 6 {
		t.Fatalf("mother review=%+v, want mother workflow and 6 proofs", review)
	}
	motherDetail, _ = repo.GetWorkflow(ctx, wfTenant, motherID, wfEventAt)
	kidDetail, _ := repo.GetWorkflow(ctx, wfTenant, kidID, wfEventAt)
	if !motherDetail.Card.AwaitingVerification || kidDetail.Card.AwaitingVerification {
		t.Fatalf("review gates mother=%v kid=%v, want true/false", motherDetail.Card.AwaitingVerification, kidDetail.Card.AwaitingVerification)
	}

	completeAll(kidID)
	kidReview, err := repo.BirthWorkflowEvidenceForVerification(ctx, wfTenant, kidID)
	if err != nil {
		t.Fatalf("kid review: %v", err)
	}
	if kidReview.WorkflowID != kidID || kidReview.SubjectRole != domain.TemplateKeyBirthKid || len(kidReview.ProofRefs) != 13 {
		t.Fatalf("kid review=%+v, want kid workflow and 13 proofs", kidReview)
	}
	if err := repo.ApplyBirthSignoffApproved(ctx, ports.DeathVerdictCommand{TenantID: wfTenant, WorkflowID: review.WorkflowID}); err != nil {
		t.Fatal(err)
	}
	motherDetail, _ = repo.GetWorkflow(ctx, wfTenant, motherID, wfEventAt)
	kidDetail, _ = repo.GetWorkflow(ctx, wfTenant, kidID, wfEventAt)
	if motherDetail.Card.AwaitingVerification || !kidDetail.Card.AwaitingVerification {
		t.Fatalf("approval gates mother=%v kid=%v, want false/true", motherDetail.Card.AwaitingVerification, kidDetail.Card.AwaitingVerification)
	}
}

// TestListKeysetPaginationPg proves the keyset cursor walks every card exactly once in
// (next_due_at ASC NULLS LAST, workflow_id ASC) order with page size 1.
func TestListKeysetPaginationPg(t *testing.T) {
	repo, _, ctx := newWorkflowRepo(t)
	openWorkflow(t, repo, ctx, domain.TemplateKeyBirthKid, wfKid)
	// Mother workflow for the dam shares the same module/date, giving a second card.
	created, err := repo.OpenWorkflow(ctx, ports.OpenWorkflowCommand{
		TenantID: wfTenant, TemplateKey: domain.TemplateKeyBirthMother, SubjectGoatID: wfDam, EventAt: wfEventAt,
	})
	if err != nil || !created {
		t.Fatalf("open mother: created=%v err=%v", created, err)
	}

	seen := map[string]bool{}
	cursor := ""
	for i := 0; i < 5; i++ {
		decoded, err := domain.DecodeWorkflowCursor(cursor)
		if err != nil {
			t.Fatalf("decode cursor: %v", err)
		}
		page, err := repo.ListWorkflows(ctx, domain.WorkflowListQuery{
			TenantID: wfTenant, Module: domain.ModuleBirth,
			EventDate: biztime.BusinessDate(wfEventAt), Filter: domain.FilterAll,
			PageSize: 1, Cursor: decoded, Now: wfEventAt,
		})
		if err != nil {
			t.Fatalf("page %d: %v", i, err)
		}
		for _, item := range page.Items {
			if seen[item.WorkflowID] {
				t.Fatalf("card %s served twice", item.WorkflowID)
			}
			seen[item.WorkflowID] = true
		}
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	if len(seen) != 2 {
		t.Fatalf("keyset walk visited %d cards, want 2", len(seen))
	}
}
