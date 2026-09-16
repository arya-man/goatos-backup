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
)

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

type fakeCaptureReviewStore struct {
	calls []string
}

func (f *fakeCaptureReviewStore) SetCaptureReviewStatus(_ context.Context, tenantID, id, status, reason string) error {
	f.calls = append(f.calls, tenantID+"|"+id+"|"+status+"|"+reason)
	return nil
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

func TestBirthCaptureVerificationEnqueuesOneItemPerLitter(t *testing.T) {
	enq := &fakeBirthCaptureEnqueuer{}
	h := NewBirthReportedVerificationHandler(enq, nil)
	capture := authored.Evidence{VersionLabel: "v2",
		Media:       []authored.EvidenceMedia{{Ref: "ref-mother", Kind: "photo", Label: "Newborns with the mother"}, {Ref: "ref-pen", Kind: "video", Label: "Pen video"}},
		Rows:        []authored.EvidenceRow{{Label: "How was the delivery?", Value: "Assisted", Group: "At report"}},
		MissingNote: "Kid weight"}
	ev := birthReportedEvent(t, capture)
	for i := 0; i < 2; i++ { // redelivered event: ONE item
		if err := h.HandleEvent(context.Background(), ev); err != nil {
			t.Fatalf("handle #%d: %v", i, err)
		}
	}
	if len(enq.items) != 1 || len(enq.calls) != 2 {
		t.Fatalf("items=%d calls=%d, want one item per litter", len(enq.items), len(enq.calls))
	}
	got := enq.calls[0]
	if got.TenantID != "t" || got.BirthEventID != "req-1" || got.ParkID != "park-1" || got.ShedID != "shed-1" || got.OperatorID != "op-1" {
		t.Fatalf("request = %+v", got)
	}
	if !reflect.DeepEqual(got.ProofRefs, []string{"ref-mother", "ref-pen"}) {
		t.Fatalf("refs = %v", got.ProofRefs)
	}
	wantMeta := []CaptureMediaMeta{{Label: "Newborns with the mother", Kind: "photo"}, {Label: "Pen video", Kind: "video"}}
	if !reflect.DeepEqual(got.MediaMeta, wantMeta) {
		t.Fatalf("meta = %+v", got.MediaMeta)
	}
	wantRows := []CaptureContextRow{
		{Label: "How was the delivery?", Value: "Assisted", Group: "At report"},
		{Label: authored.MissingNoteOlderApp, Value: "Kid weight", Group: "At report"},
	}
	if !reflect.DeepEqual(got.ContextRows, wantRows) {
		t.Fatalf("rows = %+v", got.ContextRows)
	}
	if got.IdempotencyKey != "counts-birth-capture:req-1:ref-mother:ref-pen" {
		t.Fatalf("key = %q", got.IdempotencyKey)
	}
	if got.SubjectLabel != "Birth report · 2 kids · 2026-09-16" {
		t.Fatalf("subject = %q", got.SubjectLabel)
	}
	// Zero media: answers reach the approver only; NO empty verifier item (decision 6).
	enq2 := &fakeBirthCaptureEnqueuer{}
	if err := NewBirthReportedVerificationHandler(enq2, nil).HandleEvent(context.Background(), birthReportedEvent(t, authored.Evidence{Rows: capture.Rows})); err != nil {
		t.Fatal(err)
	}
	if len(enq2.calls) != 0 {
		t.Fatal("a report with no media must not create a verifier item")
	}
	// Other event types pass through.
	other := ev
	other.Type = domain.EventDeathReported
	if err := h.HandleEvent(context.Background(), other); err != nil || len(enq.calls) != 2 {
		t.Fatalf("other event: err=%v calls=%d", err, len(enq.calls))
	}
}

func TestBirthCaptureVerdictStampsTheApprovalRow(t *testing.T) {
	store := &fakeCaptureReviewStore{}
	h := NewBirthCaptureVerdictHandler(store, nil)
	verdict := func(eventType, refType, reason string) eventbus.Event {
		payload, _ := json.Marshal(map[string]any{"verified_by": "ver-1", "reason": reason,
			"source": map[string]string{"module": "counts", "ref_type": refType, "ref_id": "req-1"}})
		return eventbus.Event{Type: eventType, TenantID: "t", Payload: payload}
	}
	if err := h.HandleEvent(context.Background(), verdict(EventVerificationVerdictApproved, domain.VerificationRefTypeBirthCapture, "")); err != nil {
		t.Fatal(err)
	}
	if err := h.HandleEvent(context.Background(), verdict(EventVerificationVerdictRework, domain.VerificationRefTypeBirthCapture, "Mother's face not visible")); err != nil {
		t.Fatal(err)
	}
	// Other counts ref types (a birth STEP, a death bundle, shifting) pass through untouched.
	for _, refType := range []string{"workflow_birth_action", "workflow_death_signoff", "shifting_event"} {
		if err := h.HandleEvent(context.Background(), verdict(EventVerificationVerdictRework, refType, "x")); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"t|req-1|approved|", "t|req-1|rework|Mother's face not visible"}
	if !reflect.DeepEqual(store.calls, want) {
		t.Fatalf("store calls = %v, want %v", store.calls, want)
	}
}
