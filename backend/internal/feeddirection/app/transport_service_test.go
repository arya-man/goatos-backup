package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
)

type transportServiceStore struct {
	ports.TransportStore
	task   ports.FeedTransportTask
	result ports.SubmitTransportResult
}

func (s *transportServiceStore) GetTransportTask(context.Context, string, string) (ports.FeedTransportTask, error) {
	return s.task, nil
}
func (s *transportServiceStore) SubmitTransportAttempt(context.Context, ports.SubmitTransportParams) (ports.SubmitTransportResult, error) {
	return s.result, nil
}

type transportProofValidator struct{ shedID string }

func (*transportProofValidator) ValidateFeedProofs(context.Context, string, []string) error {
	return nil
}
func (v *transportProofValidator) ValidateLiveCameraVideo(_ context.Context, _, _, shedID string) error {
	v.shedID = shedID
	return nil
}

type transportEnqueueSpy struct{ calls int }

func (s *transportEnqueueSpy) EnqueueFeedTransportVerification(context.Context, FeedTransportVerificationEnqueueRequest) error {
	s.calls++
	return nil
}

func TestSubmitTransportReplayRepairsMissingVerificationQueueItem(t *testing.T) {
	store := &transportServiceStore{
		task: ports.FeedTransportTask{TaskID: "task-1", ParkID: "park-1", ShedID: "shed-1"},
		// NewlyPending=false models an exact idempotent replay after the first queue call failed.
		result: ports.SubmitTransportResult{AttemptID: "attempt-1", ParkID: "park-1", ShedID: "shed-1", Status: "verification_due", AttemptNo: 1},
	}
	proofs := &transportProofValidator{}
	enqueuer := &transportEnqueueSpy{}
	service := NewService(nil, nil).WithTransportStore(store).WithProofValidator(proofs).WithTransportVerificationEnqueuer(enqueuer)

	_, err := service.SubmitTransport(context.Background(), SubmitTransportInput{
		TenantID: "tenant-1", TaskID: "task-1", ProofRef: "proof-1", OperatorID: "operator-1",
		IdempotencyKey: "transport-retry-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if enqueuer.calls != 1 {
		t.Fatalf("enqueue calls=%d want 1", enqueuer.calls)
	}
	if proofs.shedID != "shed-1" {
		t.Fatalf("validated shed=%q want shed-1", proofs.shedID)
	}
}
