package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

type verificationCompletionFake struct {
	applyErr             error
	applyCalls           int
	submissionApplyCalls int
	outcome              string
	actor                string
	goatIDs              []string
}

func (f *verificationCompletionFake) AcceptExisting(context.Context, AcceptExistingInput) (AcceptResult, error) {
	return AcceptResult{}, nil
}

func (f *verificationCompletionFake) RejectExisting(context.Context, string, string, string, *string) (RejectResult, error) {
	return RejectResult{}, nil
}

func (f *verificationCompletionFake) ApplyGoatVerification(_ context.Context, _, _, _, outcome, _ string, actorID *string) error {
	f.applyCalls++
	f.outcome = outcome
	if actorID != nil {
		f.actor = *actorID
	}
	return f.applyErr
}

func (f *verificationCompletionFake) ApplySubmissionVerification(_ context.Context, _, _, outcome, _ string, actorID *string) ([]string, error) {
	f.submissionApplyCalls++
	f.outcome = outcome
	if actorID != nil {
		f.actor = *actorID
	}
	if f.goatIDs == nil {
		f.goatIDs = []string{
			"73000000-0000-4000-8000-000000000004",
			"73000000-0000-4000-8000-000000000005",
		}
	}
	return f.goatIDs, f.applyErr
}

type verificationClosureFake struct {
	calls        int
	submissionID string
	goatID       string
	actorID      string
}

func (f *verificationClosureFake) AcceptSubmissionItemVerification(_ context.Context, _, submissionID, goatID, actorID string) error {
	f.calls++
	f.submissionID = submissionID
	f.goatID = goatID
	f.actorID = actorID
	return nil
}

func TestGenericSubmissionVerificationCloseProjectsEverySOPGoatAfterVaccinationAcceptance(t *testing.T) {
	completion := &verificationCompletionFake{}
	closure := &verificationClosureFake{}
	handler := NewVerificationHandler(completion).WithClosureProjector(closure)
	payload := genericSubmissionVerificationPayload(t)

	if err := handler.HandleEvent(context.Background(), eventbus.Event{
		Type:     EventGenericVerificationClosed,
		TenantID: "00000000-0000-4000-8000-000000000001",
		Payload:  payload,
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if completion.submissionApplyCalls != 1 || completion.outcome != "closed" || completion.actor != "73000000-0000-4000-8000-000000000012" {
		t.Fatalf("completion call = %#v", completion)
	}
	if closure.calls != 2 || closure.submissionID != "73000000-0000-4000-8000-000000000009" || closure.actorID != completion.actor {
		t.Fatalf("closure call = %#v", closure)
	}
}

func TestGenericVerificationCloseProjectsSOPOnlyAfterVaccinationAcceptance(t *testing.T) {
	completion := &verificationCompletionFake{}
	closure := &verificationClosureFake{}
	handler := NewVerificationHandler(completion).WithClosureProjector(closure)
	payload := genericVerificationPayload(t)

	if err := handler.HandleEvent(context.Background(), eventbus.Event{
		Type:     EventGenericVerificationClosed,
		TenantID: "00000000-0000-4000-8000-000000000001",
		Payload:  payload,
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if completion.applyCalls != 1 || completion.outcome != "closed" || completion.actor != "73000000-0000-4000-8000-000000000012" {
		t.Fatalf("completion call = %#v", completion)
	}
	if closure.calls != 1 || closure.submissionID != "73000000-0000-4000-8000-000000000009" || closure.goatID != "73000000-0000-4000-8000-000000000004" || closure.actorID != completion.actor {
		t.Fatalf("closure call = %#v", closure)
	}

	completion.applyErr = errors.New("stock consume failed")
	if err := handler.HandleEvent(context.Background(), eventbus.Event{
		Type:     EventGenericVerificationClosed,
		TenantID: "00000000-0000-4000-8000-000000000001",
		Payload:  payload,
	}); err == nil {
		t.Fatal("HandleEvent error = nil, want canonical vaccination failure")
	}
	if closure.calls != 1 {
		t.Fatalf("SOP projected despite vaccination failure: calls=%d", closure.calls)
	}
}

func genericVerificationPayload(t *testing.T) []byte {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"closed_by": "73000000-0000-4000-8000-000000000012",
		"source": map[string]any{
			"module":        "vaccination",
			"submission_id": "73000000-0000-4000-8000-000000000009",
			"ref_type":      "vaccination_goat",
			"ref_id":        "73000000-0000-4000-8000-000000000004",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func genericSubmissionVerificationPayload(t *testing.T) []byte {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"closed_by": "73000000-0000-4000-8000-000000000012",
		"source": map[string]any{
			"module":        "vaccination",
			"submission_id": "73000000-0000-4000-8000-000000000009",
			"ref_type":      "sop_submission",
			"ref_id":        "73000000-0000-4000-8000-000000000009",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return payload
}
