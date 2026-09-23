package postgres

// SOP capture card + reconcile parity -- Postgres proofs (2026-09-16). Opt-in like every DB test.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	identityports "github.com/vgoats/goatos/backend/internal/identity/ports"
	outboxapp "github.com/vgoats/goatos/backend/internal/outbox/app"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
	tasksdomain "github.com/vgoats/goatos/backend/internal/tasks/domain"
)

func sampleCapture() *domain.ApprovalCapture {
	return &domain.ApprovalCapture{
		SOPVersionID: "",
		Proofs:       authored.ProofRefs{"newborns_with_mother": "ref-mother"},
		Answers:      authored.Answers{"delivery_type": json.RawMessage(`"assisted"`)},
		Evidence: authored.Evidence{VersionLabel: "v2",
			Media:       []authored.EvidenceMedia{{Key: "newborns_with_mother", Ref: "ref-mother", Kind: "photo", Label: "Newborns with the mother"}},
			Rows:        []authored.EvidenceRow{{Label: "How was the delivery?", Value: "Assisted", Group: "At report"}},
			MissingNote: "Pen video"},
	}
}

func validateEnvelope(t *testing.T, envelope []byte) {
	t.Helper()
	validator, err := outboxapp.NewEnvelopeValidator(filepath.Join("..", "..", "..", "..", "..", "contracts", "jsonschema", "domain-event-envelope.schema.json"))
	if err != nil {
		t.Fatalf("load contract: %v", err)
	}
	if err := validator.Validate(envelope); err != nil {
		t.Fatalf("envelope violates the domain-event contract: %v", err)
	}
}

func TestBirthSubmissionStoresCaptureAndEmitsBirthReportedPg(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newApprovalRepo(t, pool, identitypg.NewRepository(pool, 10*time.Second))
	motherID := "00000000-0000-4000-8000-00000000b321"
	seedApprovalGoat(t, ctx, pool, motherID, countsShedA)
	dob := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	stage, litter := "kid", 1
	children := []identityports.CreateAdminGoatCommand{{
		TenantID: countsTenant, ActorID: countsOperator, ClientIdempotencyKey: "cap-child-1",
		StoredIdempotencyKey: countsTenant + ":identity.admin.goat_create:cap-child-1",
		IdempotencyScope:     "identity.admin.goat_create", RequestHash: "hash-cap-child-1",
		Identifiers:      []identityports.AdminGoatCreateIdentifier{{IdentifierType: "temporary_tag", IdentifierValue: "CPT-32100", NormalizedValue: "CPT-32100", ScopeKey: "global", IsPrimary: true}},
		CustodianPartyID: countsCustodian, ParkID: countsPark, ShedID: countsShedA,
		Species: "goat", Breed: strPtr("beetal"), Sex: "female", DOB: &dob, OriginType: "birth", EntryDate: dob,
		ManagementStage: &stage, DamID: &motherID, LitterSize: &litter,
	}}
	submission := birthSubmission("birth-capture-pg")
	submission.Payload = json.RawMessage(fmt.Sprintf(`{"species":"goat","park_id":%q,"shed_id":%q}`, countsPark, countsShedA))
	submission.Capture = sampleCapture()
	result, err := repo.CreateBirthApprovalRequest(ctx, submission, children)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if !reflect.DeepEqual(result.Approval.Capture, submission.Capture.Evidence) {
		t.Fatalf("returned capture = %+v", result.Approval.Capture)
	}
	if result.Approval.CaptureReviewStatus == nil || *result.Approval.CaptureReviewStatus != domain.CaptureReviewPending {
		t.Fatalf("a capture with media opens a pending review: %v", result.Approval.CaptureReviewStatus)
	}
	// Replay: no second event.
	if _, err := repo.CreateBirthApprovalRequest(ctx, submission, children); err != nil {
		t.Fatalf("replay: %v", err)
	}
	var (
		count    int
		envelope []byte
	)
	if err := pool.QueryRow(ctx, `SELECT count(*), max(payload::text)::bytea FROM outbox_messages WHERE tenant_id=$1::uuid AND event_type=$2`,
		countsTenant, domain.EventBirthReported).Scan(&count, &envelope); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("counts.birth.reported rows = %d, want 1", count)
	}
	validateEnvelope(t, envelope)
	var decoded struct {
		Payload struct {
			BirthEventID    string            `json:"birth_event_id"`
			ParkID          string            `json:"park_id"`
			ShedID          string            `json:"shed_id"`
			GoatIDs         []string          `json:"goat_ids"`
			CaptureEvidence authored.Evidence `json:"capture_evidence"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(envelope, &decoded); err != nil {
		t.Fatal(err)
	}
	p := decoded.Payload
	if p.BirthEventID != result.Approval.ApprovalRequestID || p.ParkID != countsPark || p.ShedID != countsShedA || len(p.GoatIDs) != 1 || p.GoatIDs[0] != result.Children[0].GoatID {
		t.Fatalf("payload = %+v", p)
	}
	if !reflect.DeepEqual(p.CaptureEvidence, submission.Capture.Evidence) {
		t.Fatalf("event capture = %+v", p.CaptureEvidence)
	}
	// The approver's list carries the snapshot; a stale ref is fenced; the slot verdict rolls up.
	if _, applied, err := repo.SetCaptureSlotReview(ctx, countsTenant, result.Approval.ApprovalRequestID, "newborns_with_mother", "some-old-ref", domain.CaptureReviewApproved, ""); err != nil || applied {
		t.Fatalf("stale ref applied=%v err=%v", applied, err)
	}
	if _, applied, err := repo.SetCaptureSlotReview(ctx, countsTenant, result.Approval.ApprovalRequestID, "newborns_with_mother", "ref-mother", domain.CaptureReviewRework, "Mother's face not visible"); err != nil || !applied {
		t.Fatalf("slot verdict applied=%v err=%v", applied, err)
	}
	page, err := repo.ListApprovalRequests(ctx, domain.ApprovalRequestQuery{TenantID: countsTenant, Status: domain.ApprovalStatusPending, RequestTypes: []string{"birth"}, PageSize: 20})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("list: %v items=%d", err, len(page.Items))
	}
	item := page.Items[0]
	if !reflect.DeepEqual(item.Capture, submission.Capture.Evidence) || item.CaptureReviewStatus == nil || *item.CaptureReviewStatus != "rework" ||
		item.CaptureReviewReason == nil || *item.CaptureReviewReason != "Mother's face not visible" {
		t.Fatalf("list item capture = %+v status=%v reason=%v", item.Capture, item.CaptureReviewStatus, item.CaptureReviewReason)
	}
	// The stored payload never carries the capture (it is replayed through identity).
	var stored []byte
	if err := pool.QueryRow(ctx, `SELECT payload::text::bytea FROM counts_approval_requests WHERE approval_request_id=$1::uuid`, result.Approval.ApprovalRequestID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(stored, &fields)
	if _, ok := fields["sop_capture"]; ok {
		t.Fatal("stored payload carries sop_capture")
	}
}

func TestDeathSubmissionEventCarriesCaptureEvidencePg(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newApprovalRepo(t, pool, &fakeIdentityTx{})
	goatID := "00000000-0000-4000-8000-00000000c321"
	seedApprovalGoat(t, ctx, pool, goatID, countsShedA)
	capture := &domain.ApprovalCapture{Answers: authored.Answers{"found": json.RawMessage(`"trough"`)},
		Evidence: authored.Evidence{VersionLabel: "v4", Rows: []authored.EvidenceRow{{Label: "Found where?", Value: "Trough", Group: "At report"}}}}
	req, _, err := repo.CreateApprovalRequest(ctx, domain.ApprovalRequestSubmission{
		TenantID: countsTenant, RequestType: domain.ApprovalRequestTypeDeath,
		Payload:       json.RawMessage(`{"goat_id":"` + goatID + `","lifecycle_status":"dead","exit_reason":"died"}`),
		SubjectGoatID: &goatID, RaisedByUserID: countsOperator, RaisedAt: time.Now(),
		IdempotencyKey: "death-capture-pg", RequestFingerprint: "death-capture-pg-fp", Capture: capture,
	})
	if err != nil {
		t.Fatal(err)
	}
	if req.CaptureReviewStatus != nil {
		t.Fatal("an answers-only capture has nothing for the verifier and opens no review")
	}
	var envelope []byte
	if err := pool.QueryRow(ctx, `SELECT payload::text::bytea FROM outbox_messages WHERE tenant_id=$1::uuid AND event_type=$2 AND aggregate_id=$3::uuid`,
		countsTenant, domain.EventDeathReported, req.ApprovalRequestID).Scan(&envelope); err != nil {
		t.Fatal(err)
	}
	validateEnvelope(t, envelope)
	var decoded struct {
		Payload struct {
			CaptureEvidence authored.Evidence `json:"capture_evidence"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(envelope, &decoded); err != nil || !reflect.DeepEqual(decoded.Payload.CaptureEvidence, capture.Evidence) {
		t.Fatalf("death event capture = %+v err=%v", decoded.Payload.CaptureEvidence, err)
	}
}

func TestPenReconciliationWorkflowBackedCardCompletesThroughItsWorkflowPg(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := NewRepository(pool, 5*time.Second)
	strayed := "00000000-0000-4000-8000-00000000f321"
	seedPenRecGoatWithTag(t, ctx, pool, strayed, countsShedB, "whole", "1420 3021")
	bucket := "00000000-0000-4000-8000-00000000e321"
	seedPenRecBucket(t, ctx, pool, bucket, countsShedA, "", []string{"1420 3021"})
	if raised := raisePenRec(t, ctx, repo, bucket); raised != 1 {
		t.Fatalf("raised = %d", raised)
	}
	page, err := repo.ListPenReconciliationCards(ctx, domain.PenReconciliationQuery{TenantID: countsTenant, Status: domain.PenReconciliationBucketOpen, PageSize: 20})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("list: %v", err)
	}
	cardID := page.Items[0].CardID
	workflowID := "00000000-0000-4000-8000-00000000aa21"
	if err := repo.SetPenReconciliationWorkflow(ctx, countsTenant, cardID, workflowID, nil); err != nil {
		t.Fatal(err)
	}
	cmd := domain.PenReconciliationCompletionCommand{
		TenantID: countsTenant, CardID: cardID, CompletedByUserID: penRecOperator,
		CompletedAt: time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC),
		ProofRef:    "v-return", ProofRefs: []string{"v-return", "p-tag"}, ProofKinds: map[string]string{"p-tag": "photo"},
		MediaMeta:   []domain.PenReconciliationProofMeta{{Label: "Return", Kind: "video"}, {Label: "Tag", Kind: "photo"}},
		ContextRows: []domain.PenReconciliationContextRow{{Label: "Tag visible?", Value: "Yes", Group: "Tag visible?"}},
		WorkflowID:  "00000000-0000-4000-8000-00000000bb21", IdempotencyKey: "wf-key", RequestFingerprint: "wf-fp",
	}
	// Another workflow's completion is refused.
	if _, _, err := repo.CompletePenReconciliationCard(ctx, cmd); !errors.Is(err, ports.ErrPenReconciliationNotActionable) {
		t.Fatalf("foreign workflow err = %v", err)
	}
	// Its OWN workflow's completion lands (this was refused on every retry before).
	cmd.WorkflowID = workflowID
	result, replay, err := repo.CompletePenReconciliationCard(ctx, cmd)
	if err != nil || replay {
		t.Fatalf("own workflow complete: replay=%v err=%v", replay, err)
	}
	if result.Status != domain.PenReconciliationStatusPendingVerification || !reflect.DeepEqual(result.MediaMeta, cmd.MediaMeta) {
		t.Fatalf("result = %+v", result)
	}
	// Exact replay returns the stored set.
	again, replay, err := repo.CompletePenReconciliationCard(ctx, cmd)
	if err != nil || !replay || !reflect.DeepEqual(again.ProofRefs, []string{"v-return", "p-tag"}) || !reflect.DeepEqual(again.ContextRows, cmd.ContextRows) {
		t.Fatalf("replay = %+v replay=%v err=%v", again, replay, err)
	}
}

func TestPenReconciliationDebtListCarriesTheFullProofSetPg(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := NewRepository(pool, 5*time.Second)
	strayed := "00000000-0000-4000-8000-00000000f322"
	seedPenRecGoatWithTag(t, ctx, pool, strayed, countsShedB, "whole", "1420 3022")
	bucket := "00000000-0000-4000-8000-00000000e322"
	seedPenRecBucket(t, ctx, pool, bucket, countsShedA, "", []string{"1420 3022"})
	raisePenRec(t, ctx, repo, bucket)
	page, _ := repo.ListPenReconciliationCards(ctx, domain.PenReconciliationQuery{TenantID: countsTenant, Status: domain.PenReconciliationBucketOpen, PageSize: 20})
	cardID := page.Items[0].CardID
	meta := []domain.PenReconciliationProofMeta{{Label: "Return", Kind: "video"}, {Label: "Tag", Kind: "photo"}}
	rows := []domain.PenReconciliationContextRow{{Label: "Tag visible?", Value: "Yes", Group: "Tag visible?"}}
	if _, _, err := repo.CompletePenReconciliationCard(ctx, domain.PenReconciliationCompletionCommand{
		TenantID: countsTenant, CardID: cardID, CompletedByUserID: penRecOperator, CompletedAt: time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC),
		ProofRef: "v-return", ProofRefs: []string{"v-return", "p-tag"}, ProofKinds: map[string]string{"p-tag": "photo"},
		MediaMeta: meta, ContextRows: rows, IdempotencyKey: "debt-key", RequestFingerprint: "debt-fp",
	}); err != nil {
		t.Fatal(err)
	}
	debts, err := repo.ListPenReconciliationVerificationEnqueueDebt(ctx, countsTenant, 10)
	if err != nil || len(debts) != 1 {
		t.Fatalf("debts: %v n=%d", err, len(debts))
	}
	d := debts[0]
	if !reflect.DeepEqual(d.ProofRefs, []string{"v-return", "p-tag"}) || !reflect.DeepEqual(d.MediaMeta, meta) || !reflect.DeepEqual(d.ContextRows, rows) {
		t.Fatalf("debt = %+v", d)
	}
}

func TestReplaceCaptureProofSwapsTheRejectedProofPg(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newApprovalRepo(t, pool, &fakeIdentityTx{})
	goatID := "00000000-0000-4000-8000-00000000c323"
	seedApprovalGoat(t, ctx, pool, goatID, countsShedA)
	capture := &domain.ApprovalCapture{Proofs: authored.ProofRefs{"tag_photo": "old"},
		Evidence: authored.Evidence{Media: []authored.EvidenceMedia{{Key: "tag_photo", Ref: "old", Kind: "photo", Label: "Tag"}}}}
	req, _, err := repo.CreateApprovalRequest(ctx, domain.ApprovalRequestSubmission{
		TenantID: countsTenant, RequestType: domain.ApprovalRequestTypeDeath,
		Payload:       json.RawMessage(`{"goat_id":"` + goatID + `","lifecycle_status":"dead","exit_reason":"died"}`),
		SubjectGoatID: &goatID, RaisedByUserID: countsOperator, RaisedAt: time.Now(),
		IdempotencyKey: "replace-capture", RequestFingerprint: "replace-capture-fp", Capture: capture,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, applied, err := repo.SetCaptureSlotReview(ctx, countsTenant, req.ApprovalRequestID, "tag_photo", "old", domain.CaptureReviewRework, "blurry"); err != nil || !applied {
		t.Fatalf("applied=%v err=%v", applied, err)
	}
	for i := 0; i < 2; i++ {
		got, err := repo.ReplaceCaptureProof(ctx, countsTenant, req.ApprovalRequestID, 0, tasksdomain.ProofItem{Ref: "new", Kind: "photo"})
		if err != nil {
			t.Fatal(err)
		}
		if got.Capture.Media[0].Ref != "new" || got.CaptureReviewStatus == nil || *got.CaptureReviewStatus != "pending" || got.CaptureReviewReason != nil {
			t.Fatalf("#%d = %+v status=%v reason=%v", i, got.Capture, got.CaptureReviewStatus, got.CaptureReviewReason)
		}
	}
	var proofs string
	if err := pool.QueryRow(ctx, `SELECT capture_proofs::text FROM counts_approval_requests WHERE approval_request_id=$1::uuid`, req.ApprovalRequestID).Scan(&proofs); err != nil {
		t.Fatal(err)
	}
	if proofs != `{"tag_photo": "new"}` {
		t.Fatalf("capture_proofs = %s", proofs)
	}
	// A late verdict on the superseded proof is fenced out; the re-shoot's pending review stands.
	if got, applied, err := repo.SetCaptureSlotReview(ctx, countsTenant, req.ApprovalRequestID, "tag_photo", "old", domain.CaptureReviewApproved, ""); err != nil || applied || *got.CaptureReviewStatus != "pending" {
		t.Fatalf("stale verdict applied=%v err=%v status=%v", applied, err, got.CaptureReviewStatus)
	}
}

// TestNoCaptureCardSubmitsBehaveAsTodayPg pins the deploy-day rule: with no authored capture
// card, a birth writes NO counts.birth.reported row and a death event carries NO capture_evidence
// key -- the outbox is byte-for-byte what it was before the feature.
func TestNoCaptureCardSubmitsBehaveAsTodayPg(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newApprovalRepo(t, pool, identitypg.NewRepository(pool, 10*time.Second))
	motherID := "00000000-0000-4000-8000-00000000b322"
	seedApprovalGoat(t, ctx, pool, motherID, countsShedA)
	dob := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	stage, litter := "kid", 1
	children := []identityports.CreateAdminGoatCommand{{
		TenantID: countsTenant, ActorID: countsOperator, ClientIdempotencyKey: "plain-child-1",
		StoredIdempotencyKey: countsTenant + ":identity.admin.goat_create:plain-child-1",
		IdempotencyScope:     "identity.admin.goat_create", RequestHash: "hash-plain-child-1",
		Identifiers:      []identityports.AdminGoatCreateIdentifier{{IdentifierType: "temporary_tag", IdentifierValue: "CPT-32200", NormalizedValue: "CPT-32200", ScopeKey: "global", IsPrimary: true}},
		CustodianPartyID: countsCustodian, ParkID: countsPark, ShedID: countsShedA,
		Species: "goat", Breed: strPtr("beetal"), Sex: "female", DOB: &dob, OriginType: "birth", EntryDate: dob,
		ManagementStage: &stage, DamID: &motherID, LitterSize: &litter,
	}}
	if _, err := repo.CreateBirthApprovalRequest(ctx, birthSubmission("plain-birth"), children); err != nil {
		t.Fatal(err)
	}
	var births int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_messages WHERE tenant_id=$1::uuid AND event_type=$2`, countsTenant, domain.EventBirthReported).Scan(&births); err != nil {
		t.Fatal(err)
	}
	if births != 0 {
		t.Fatalf("counts.birth.reported rows = %d, want 0 without a capture", births)
	}
	goatID := "00000000-0000-4000-8000-00000000c322"
	seedApprovalGoat(t, ctx, pool, goatID, countsShedA)
	req, _, err := repo.CreateApprovalRequest(ctx, domain.ApprovalRequestSubmission{
		TenantID: countsTenant, RequestType: domain.ApprovalRequestTypeDeath,
		Payload:       json.RawMessage(`{"goat_id":"` + goatID + `","lifecycle_status":"dead","exit_reason":"died"}`),
		SubjectGoatID: &goatID, RaisedByUserID: countsOperator, RaisedAt: time.Now(),
		IdempotencyKey: "plain-death", RequestFingerprint: "plain-death-fp",
	})
	if err != nil {
		t.Fatal(err)
	}
	var hasKey bool
	if err := pool.QueryRow(ctx, `SELECT payload->'payload' ? 'capture_evidence' FROM outbox_messages WHERE tenant_id=$1::uuid AND event_type=$2 AND aggregate_id=$3::uuid`,
		countsTenant, domain.EventDeathReported, req.ApprovalRequestID).Scan(&hasKey); err != nil {
		t.Fatal(err)
	}
	if hasKey {
		t.Fatal("a death with no capture must not carry capture_evidence in its event payload")
	}
	if req.CaptureReviewStatus != nil || !req.Capture.IsEmpty() {
		t.Fatalf("no capture stored: %+v %v", req.Capture, req.CaptureReviewStatus)
	}
}
