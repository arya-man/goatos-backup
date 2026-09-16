package app

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
	tasksdomain "github.com/vgoats/goatos/backend/internal/tasks/domain"
)

// Birth is verified PER RECORDING (maintainer correction 2026-09-16, PR #283's principle): the Add
// birth form's proofs are ONE verifier item PER FORM PROOF SLOT; a verdict addresses that slot's
// current proof only; a reject re-shoots only that slot. Death is unchanged (a bundle).

type fakeBirthCaptureEnqueuer struct {
	calls []BirthCaptureVerificationEnqueueRequest
	items map[string]int
	err   error
}

func (f *fakeBirthCaptureEnqueuer) EnqueueBirthCaptureVerification(_ context.Context, in BirthCaptureVerificationEnqueueRequest) error {
	if f.err != nil {
		return f.err
	}
	f.calls = append(f.calls, in)
	if f.items == nil {
		f.items = map[string]int{}
	}
	f.items[in.TenantID+"|"+in.IdempotencyKey]++ // ON CONFLICT DO NOTHING: one item per key
	return nil
}

// fakeSlotReviewStore models the approval row: the snapshot plus one review per slot, fenced on
// the slot's CURRENT proof ref exactly like the Postgres write.
type fakeSlotReviewStore struct {
	report   domain.ApprovalRequest
	reviews  map[string]domain.CaptureSlotReview
	replaced []string
}

func (f *fakeSlotReviewStore) SetCaptureSlotReview(_ context.Context, _, _, slotKey, ref, status, reason string) (domain.ApprovalRequest, bool, error) {
	current := ""
	for _, m := range f.report.Capture.Media {
		if m.Key == slotKey {
			current = m.Ref
		}
	}
	if current == "" || current != ref {
		return f.report, false, nil
	}
	f.reviews[slotKey] = domain.CaptureSlotReview{Ref: ref, Status: status, Reason: reason}
	return f.report, true, nil
}

func (f *fakeSlotReviewStore) ReplaceCaptureProof(_ context.Context, tenantID, id string, index int, proof tasksdomain.ProofItem) (domain.ApprovalRequest, error) {
	f.replaced = append(f.replaced, tenantID+"|"+id+"|"+proof.Ref)
	f.report.Capture = tasksdomain.ReplaceCaptureMedia(f.report.Capture, index, proof)
	key := f.report.Capture.Media[index].Key
	f.reviews[key] = domain.CaptureSlotReview{Ref: proof.Ref, Status: domain.CaptureReviewPending}
	return f.report, nil
}

type fakeReshootEngine struct {
	calls   []string
	indexes [][]int
}

func (f *fakeReshootEngine) OpenBirthCaptureReshoot(_ context.Context, tenantID, birthEventID string, capture authored.Evidence, indexes []int, recordingKey, reason string) error {
	f.calls = append(f.calls, tenantID+"|"+birthEventID+"|"+recordingKey+"|"+reason)
	f.indexes = append(f.indexes, indexes)
	return nil
}

func twoSlotCapture() authored.Evidence {
	return authored.Evidence{VersionLabel: "v2",
		Media: []authored.EvidenceMedia{
			{Key: "newborns_with_mother", Ref: "ref-mother", Kind: "photo", Label: "Newborns with the mother"},
			{Key: "pen_clip", Ref: "ref-pen", Kind: "video", Label: "Pen clip"},
		},
		Rows:        []authored.EvidenceRow{{Label: "How was the delivery?", Value: "Assisted", Group: "At report"}},
		MissingNote: "Kid weight"}
}

func birthReportedEvent(t *testing.T, capture authored.Evidence) eventbus.Event {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"approval_request_id": "req-1", "birth_event_id": "req-1", "park_id": "park-1", "shed_id": "shed-1",
		"goat_ids": []string{"kid-1", "kid-2"}, "raised_by_user_id": "op-1", "capture_evidence": capture,
	})
	if err != nil {
		t.Fatal(err)
	}
	return eventbus.Event{Type: domain.EventBirthReported, TenantID: "t", Key: "req-1", Payload: payload, OccurredAt: time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC)}
}

func birthCaptureVerdict(t *testing.T, eventType, recordingKey, reason string) eventbus.Event {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"verified_by": "ver-1", "reason": reason,
		"source": map[string]string{"module": "counts", "ref_type": domain.VerificationRefTypeBirthCapture, "ref_id": "req-1", "recording_key": recordingKey}})
	if err != nil {
		t.Fatal(err)
	}
	return eventbus.Event{Type: eventType, TenantID: "t", Payload: payload}
}

func TestBirthCaptureEnqueuesOneItemPerFormProofSlot(t *testing.T) {
	enq := &fakeBirthCaptureEnqueuer{}
	h := NewBirthReportedVerificationHandler(enq, nil)
	ev := birthReportedEvent(t, twoSlotCapture())
	for i := 0; i < 2; i++ { // redelivered event: still one item per slot
		if err := h.HandleEvent(context.Background(), ev); err != nil {
			t.Fatalf("handle #%d: %v", i, err)
		}
	}
	if len(enq.items) != 2 {
		t.Fatalf("items = %d, want one per form proof slot", len(enq.items))
	}
	first, second := enq.calls[0], enq.calls[1]
	if first.IdempotencyKey != "counts-birth-capture:req-1:newborns_with_mother:ref-mother" || second.IdempotencyKey != "counts-birth-capture:req-1:pen_clip:ref-pen" {
		t.Fatalf("keys = %q %q", first.IdempotencyKey, second.IdempotencyKey)
	}
	if !reflect.DeepEqual(first.ProofRefs, []string{"ref-mother"}) || !reflect.DeepEqual(second.ProofRefs, []string{"ref-pen"}) {
		t.Fatalf("each item carries only its slot's proof: %v %v", first.ProofRefs, second.ProofRefs)
	}
	if !reflect.DeepEqual(first.MediaMeta, []CaptureMediaMeta{{Label: "Newborns with the mother", Kind: "photo"}}) {
		t.Fatalf("meta = %+v", first.MediaMeta)
	}
	if first.SubjectLabel != "Birth report · Newborns with the mother" || second.SubjectLabel != "Birth report · Pen clip" {
		t.Fatalf("subjects = %q %q", first.SubjectLabel, second.SubjectLabel)
	}
	wantRows := []CaptureContextRow{
		{Label: "How was the delivery?", Value: "Assisted", Group: "At report"},
		{Label: authored.MissingNoteOlderApp, Value: "Kid weight", Group: "At report"},
	}
	if !reflect.DeepEqual(first.ContextRows, wantRows) || !reflect.DeepEqual(second.ContextRows, wantRows) {
		t.Fatalf("every item carries the form's answers as context: %+v", first.ContextRows)
	}
	if first.BirthEventID != "req-1" || first.SlotKey != "newborns_with_mother" || first.ParkID != "park-1" || first.OperatorID != "op-1" {
		t.Fatalf("request = %+v", first)
	}
	other := ev
	other.Type = domain.EventDeathReported
	if err := h.HandleEvent(context.Background(), other); err != nil || len(enq.calls) != 4 {
		t.Fatalf("other event: err=%v calls=%d", err, len(enq.calls))
	}
}

func TestBirthCaptureQuestionsOnlyFormEnqueuesNothing(t *testing.T) {
	enq := &fakeBirthCaptureEnqueuer{}
	c := twoSlotCapture()
	c.Media = nil
	if err := NewBirthReportedVerificationHandler(enq, nil).HandleEvent(context.Background(), birthReportedEvent(t, c)); err != nil {
		t.Fatal(err)
	}
	if len(enq.calls) != 0 {
		t.Fatal("a report with no media must not create a verifier item")
	}
}

func TestBirthCaptureRejectReshootsOnlyThatSlot(t *testing.T) {
	store := &fakeSlotReviewStore{report: domain.ApprovalRequest{ApprovalRequestID: "req-1", RaisedByUserID: "op-1",
		Payload: json.RawMessage(`{"park_id":"park-1","shed_id":"shed-1"}`), Capture: twoSlotCapture()}, reviews: map[string]domain.CaptureSlotReview{}}
	engine := &fakeReshootEngine{}
	h := NewBirthCaptureVerdictHandler(store, nil).WithReshootEngine(engine)
	if err := h.HandleEvent(context.Background(), birthCaptureVerdict(t, EventVerificationVerdictApproved, "counts-birth-capture:req-1:newborns_with_mother:ref-mother", "")); err != nil {
		t.Fatal(err)
	}
	if err := h.HandleEvent(context.Background(), birthCaptureVerdict(t, EventVerificationVerdictRework, "counts-birth-capture:req-1:pen_clip:ref-pen", "Pen not visible")); err != nil {
		t.Fatal(err)
	}
	if store.reviews["newborns_with_mother"].Status != domain.CaptureReviewApproved || store.reviews["pen_clip"].Status != domain.CaptureReviewRework ||
		store.reviews["pen_clip"].Reason != "Pen not visible" {
		t.Fatalf("reviews = %+v", store.reviews)
	}
	if len(engine.calls) != 1 || !reflect.DeepEqual(engine.indexes[0], []int{1}) || engine.calls[0] != "t|req-1|counts-birth-capture:req-1:pen_clip:ref-pen|Pen not visible" {
		t.Fatalf("re-shoot = %v %v, want ONLY the rejected slot", engine.calls, engine.indexes)
	}
	// The operator re-shoots that slot: only that slot's item is queued, under a fresh key.
	enq := &fakeBirthCaptureEnqueuer{}
	listener := NewBirthCaptureReshootService(store, enq)
	if err := listener.OnBirthCaptureReshot(context.Background(), "t", "req-1", 1, tasksdomain.ProofItem{Ref: "ref-pen-2", Kind: "video"}); err != nil {
		t.Fatal(err)
	}
	if len(enq.calls) != 1 || enq.calls[0].IdempotencyKey != "counts-birth-capture:req-1:pen_clip:ref-pen-2" || !reflect.DeepEqual(enq.calls[0].ProofRefs, []string{"ref-pen-2"}) {
		t.Fatalf("re-shot item = %+v", enq.calls)
	}
	if store.reviews["newborns_with_mother"].Status != domain.CaptureReviewApproved {
		t.Fatal("the approved slot is untouched by another slot's re-shoot")
	}
}

func TestBirthCaptureStaleVerdictOnSupersededRefIsIgnored(t *testing.T) {
	c := twoSlotCapture()
	c.Media[1].Ref = "ref-pen-2" // already re-shot
	store := &fakeSlotReviewStore{report: domain.ApprovalRequest{ApprovalRequestID: "req-1", Capture: c},
		reviews: map[string]domain.CaptureSlotReview{"pen_clip": {Ref: "ref-pen-2", Status: domain.CaptureReviewPending}}}
	engine := &fakeReshootEngine{}
	h := NewBirthCaptureVerdictHandler(store, nil).WithReshootEngine(engine)
	// A delayed verdict on the REJECTED old recording must not touch the re-shoot.
	if err := h.HandleEvent(context.Background(), birthCaptureVerdict(t, EventVerificationVerdictRework, "counts-birth-capture:req-1:pen_clip:ref-pen", "old")); err != nil {
		t.Fatal(err)
	}
	if store.reviews["pen_clip"].Status != domain.CaptureReviewPending || len(engine.calls) != 0 {
		t.Fatalf("stale verdict changed state: %+v calls=%v", store.reviews, engine.calls)
	}
	// A verdict whose key cannot be read (no slot) is ignored, never applied to the whole report.
	if err := h.HandleEvent(context.Background(), birthCaptureVerdict(t, EventVerificationVerdictApproved, "counts-birth-capture:req-1:ref-x", "")); err != nil {
		t.Fatal(err)
	}
	if len(store.reviews) != 1 {
		t.Fatalf("unreadable key applied: %+v", store.reviews)
	}
}

func TestCaptureReviewRollup(t *testing.T) {
	media := twoSlotCapture().Media
	cases := []struct {
		reviews map[string]domain.CaptureSlotReview
		want    string
	}{
		{map[string]domain.CaptureSlotReview{}, domain.CaptureReviewPending},
		{map[string]domain.CaptureSlotReview{"newborns_with_mother": {Status: "approved"}}, domain.CaptureReviewPending},
		{map[string]domain.CaptureSlotReview{"newborns_with_mother": {Status: "approved"}, "pen_clip": {Status: "approved"}}, domain.CaptureReviewApproved},
		{map[string]domain.CaptureSlotReview{"newborns_with_mother": {Status: "approved"}, "pen_clip": {Status: "rework"}}, domain.CaptureReviewRework},
	}
	for i, tc := range cases {
		if got := domain.CaptureReviewRollup(media, tc.reviews); got != tc.want {
			t.Fatalf("case %d: rollup = %q, want %q", i, got, tc.want)
		}
	}
	if got := domain.CaptureReviewRollup(nil, nil); got != "" {
		t.Fatalf("no media = %q, want no review", got)
	}
}
