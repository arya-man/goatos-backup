package app

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
	tasksdomain "github.com/vgoats/goatos/backend/internal/tasks/domain"
)

type fakeReshootEngine struct {
	calls []string
	cap   authored.Evidence
}

func (f *fakeReshootEngine) OpenBirthCaptureReshoot(_ context.Context, tenantID, birthEventID string, capture authored.Evidence, recordingKey, reason string) error {
	f.calls = append(f.calls, tenantID+"|"+birthEventID+"|"+recordingKey+"|"+reason)
	f.cap = capture
	return nil
}

type fakeReshootStore struct {
	fakeCaptureReviewStore
	report   domain.ApprovalRequest
	replaced []string
}

func (f *fakeReshootStore) GetApprovalRequest(context.Context, string, string) (domain.ApprovalRequest, error) {
	return f.report, nil
}

func (f *fakeReshootStore) ReplaceCaptureProof(_ context.Context, tenantID, id string, index int, proof tasksdomain.ProofItem) (domain.ApprovalRequest, error) {
	f.replaced = append(f.replaced, tenantID+"|"+id+"|"+proof.Ref)
	f.report.Capture = tasksdomain.ReplaceCaptureMedia(f.report.Capture, index, proof)
	pending := domain.CaptureReviewPending
	f.report.CaptureReviewStatus = &pending
	return f.report, nil
}

func TestCaptureRejectOpensAReshootStep(t *testing.T) {
	capture := authored.Evidence{Media: []authored.EvidenceMedia{{Ref: "old", Kind: "photo", Label: "Newborns with the mother"}}}
	store := &fakeReshootStore{report: domain.ApprovalRequest{ApprovalRequestID: "req-1", Capture: capture}}
	engine := &fakeReshootEngine{}
	h := NewBirthCaptureVerdictHandler(store, nil).WithReshootEngine(engine)
	payload, _ := json.Marshal(map[string]any{"reason": "Mother's face not visible",
		"source": map[string]string{"module": "counts", "ref_type": domain.VerificationRefTypeBirthCapture, "ref_id": "req-1", "recording_key": "counts-birth-capture:req-1:old"}})
	if err := h.HandleEvent(context.Background(), eventbus.Event{Type: EventVerificationVerdictRework, TenantID: "t", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(engine.calls, []string{"t|req-1|counts-birth-capture:req-1:old|Mother's face not visible"}) || !reflect.DeepEqual(engine.cap, capture) {
		t.Fatalf("engine = %v %+v", engine.calls, engine.cap)
	}
	// Approve opens nothing.
	approve, _ := json.Marshal(map[string]any{"source": map[string]string{"module": "counts", "ref_type": domain.VerificationRefTypeBirthCapture, "ref_id": "req-1"}})
	if err := h.HandleEvent(context.Background(), eventbus.Event{Type: EventVerificationVerdictApproved, TenantID: "t", Payload: approve}); err != nil || len(engine.calls) != 1 {
		t.Fatalf("approve: err=%v calls=%d", err, len(engine.calls))
	}

	// The operator re-shoots: the snapshot swaps that proof and a FRESH report item is queued.
	enq := &fakeBirthCaptureEnqueuer{}
	store.report.Payload = json.RawMessage(`{"park_id":"park-1","shed_id":"shed-1","children":[{"child_ordinal":1}]}`)
	store.report.RaisedByUserID = "op-1"
	listener := NewBirthCaptureReshootService(store, enq)
	if err := listener.OnBirthCaptureReshot(context.Background(), "t", "req-1", 0, tasksdomain.ProofItem{Ref: "new", Kind: "photo"}); err != nil {
		t.Fatal(err)
	}
	if len(store.replaced) != 1 || len(enq.calls) != 1 {
		t.Fatalf("replaced=%v enqueues=%d", store.replaced, len(enq.calls))
	}
	got := enq.calls[0]
	if !reflect.DeepEqual(got.ProofRefs, []string{"new"}) || got.IdempotencyKey != "counts-birth-capture:req-1:new" || got.ParkID != "park-1" || got.SubjectLabel == "" {
		t.Fatalf("re-shot item = %+v", got)
	}
}
