package wiring

import (
	"context"
	"testing"
	"time"

	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	feedports "github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	weighingdomain "github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// fakeFeedStore satisfies eventwiring.FeedCompletionStore (distribution + packing + transport). Only
// the verdict-applied methods record; the rest exist to satisfy the port.
type fakeFeedStore struct {
	distributionApplied []string
	packingApplied      []string
	transportApplied    []string
	distributionBounced []string
}

func (f *fakeFeedStore) CompleteDistribution(context.Context, feedports.CompleteDistributionParams) (feedports.CompleteDistributionResult, error) {
	return feedports.CompleteDistributionResult{}, nil
}

func (f *fakeFeedStore) ListVerifiedDistributions(context.Context, string, string, time.Time) ([]feedports.VerifiedDistribution, error) {
	return nil, nil
}

func (f *fakeFeedStore) ListDistributionSessionStatuses(context.Context, string, string, time.Time) ([]feedports.SessionCompletionStatus, error) {
	return nil, nil
}

func (f *fakeFeedStore) ApplyVerifiedDistribution(_ context.Context, p feedports.ApplyDistributionParams) (bool, error) {
	f.distributionApplied = append(f.distributionApplied, p.CompletionID)
	return true, nil
}

func (f *fakeFeedStore) BounceDistributionForRework(_ context.Context, p feedports.BounceDistributionParams) (bool, error) {
	f.distributionBounced = append(f.distributionBounced, p.CompletionID)
	return true, nil
}

func (f *fakeFeedStore) CompletePacking(context.Context, feedports.CompletePackingParams) (feedports.CompletePackingResult, error) {
	return feedports.CompletePackingResult{}, nil
}

func (f *fakeFeedStore) ListVerifiedPacking(context.Context, string, string, time.Time) ([]feedports.VerifiedPacking, error) {
	return nil, nil
}

func (f *fakeFeedStore) ListPackingCompletionStatuses(context.Context, string, string, time.Time) ([]feedports.PackingCompletionStatus, error) {
	return nil, nil
}

func (f *fakeFeedStore) ApplyVerifiedPacking(_ context.Context, p feedports.ApplyPackingParams) (bool, error) {
	f.packingApplied = append(f.packingApplied, p.CompletionID)
	return true, nil
}

func (f *fakeFeedStore) BouncePackingForRework(context.Context, feedports.BouncePackingParams) (bool, error) {
	return true, nil
}

func (f *fakeFeedStore) MaterializeTransportTasks(context.Context, feedports.MaterializeTransportParams) (feedports.MaterializeTransportResult, error) {
	return feedports.MaterializeTransportResult{}, nil
}

func (f *fakeFeedStore) GetTransportTask(context.Context, string, string) (feedports.FeedTransportTask, error) {
	return feedports.FeedTransportTask{}, nil
}

func (f *fakeFeedStore) ListTransportTasks(context.Context, feedports.ListTransportTasksParams) (feedports.FeedTransportTaskPage, error) {
	return feedports.FeedTransportTaskPage{}, nil
}

func (f *fakeFeedStore) SubmitTransportAttempt(context.Context, feedports.SubmitTransportParams) (feedports.SubmitTransportResult, error) {
	return feedports.SubmitTransportResult{}, nil
}

func (f *fakeFeedStore) ApplyVerifiedTransport(_ context.Context, p feedports.ApplyTransportParams) (bool, error) {
	f.transportApplied = append(f.transportApplied, p.AttemptID)
	return true, nil
}

func (f *fakeFeedStore) BounceTransportForRework(context.Context, feedports.BounceTransportParams) (bool, error) {
	return true, nil
}

// fakeShiftingRepo satisfies eventwiring.ShiftingVerificationRepo.
type fakeShiftingRepo struct{ applied []string }

func (f *fakeShiftingRepo) ApplyVerifiedShiftingEvent(_ context.Context, in countsdomain.ShiftingVerifiedApplyCommand) (countsdomain.ShiftingExecutionResult, bool, error) {
	f.applied = append(f.applied, in.ShiftingEventID)
	return countsdomain.ShiftingExecutionResult{}, true, nil
}

func (f *fakeShiftingRepo) BounceShiftingEventForRework(context.Context, countsdomain.ShiftingReworkCommand) error {
	return nil
}

// fakeWeighingStore satisfies eventwiring.WeighingVerdictStore.
type fakeWeighingStore struct{ applied []string }

func (f *fakeWeighingStore) ApplyVerificationVerdict(_ context.Context, v weighingdomain.VerificationVerdict) (weighingdomain.VerificationVerdictResult, error) {
	f.applied = append(f.applied, v.ObservationID)
	return weighingdomain.VerificationVerdictResult{}, nil
}

// TestVerdictReachesEveryApplierThroughThisBuilder is the root-cause proof for the defect this
// builder carried: it hand-listed consumers and registered only the weighing verdict applier, so a
// verifier approve/rework routed through a bus built HERE was a silent no-op for shifting, feed
// distribution, feed packing and feed transport.
//
// The assertion is on the real production dispatch path — the bus this builder registers onto —
// not on a re-declared handler list: publishing each module's verdict must actually execute that
// module's applier store.
func TestVerdictReachesEveryApplierThroughThisBuilder(t *testing.T) {
	feed := &fakeFeedStore{}
	shifting := &fakeShiftingRepo{}
	weighing := &fakeWeighingStore{}
	weighingAck := &fakeWeighingAcker{}

	bus := buildDomainBusOn(eventbus.NewInProcessBus(), nil, time.Second, nil, verificationStores{
		feed:        feed,
		shifting:    shifting,
		weighing:    weighing,
		weighingAck: weighingAck,
	})

	const tenant = "00000000-0000-4000-8000-000000000001"
	publish := func(t *testing.T, module, refType, refID string) {
		t.Helper()
		payload := `{"verified_by":"verifier-1","source":{"module":"` + module + `","ref_type":"` + refType + `","ref_id":"` + refID + `"}}`
		if err := bus.Publish(context.Background(), eventbus.Event{
			ID:       "20000000-0000-4000-8000-000000000001",
			Type:     "verification.verdict.approved",
			TenantID: tenant,
			Key:      refID,
			Payload:  []byte(payload),
		}); err != nil {
			t.Fatalf("publish %s/%s: %v", module, refType, err)
		}
	}

	// Each module is published and asserted in turn, so a missing applier fails on ITS OWN
	// assertion rather than being masked by a later module's handler.
	publish(t, "feed", "feed_distribution_completion", "fd-1")
	if len(feed.distributionApplied) != 1 || feed.distributionApplied[0] != "fd-1" {
		t.Fatalf("feed-distribution applier never ran: %v — an approved distribution verdict on this bus is a silent no-op", feed.distributionApplied)
	}
	publish(t, "feed", "feed_packing_completion", "fp-1")
	if len(feed.packingApplied) != 1 || feed.packingApplied[0] != "fp-1" {
		t.Fatalf("feed-packing applier never ran: %v", feed.packingApplied)
	}
	publish(t, "feed", "feed_transport_attempt", "ft-1")
	if len(feed.transportApplied) != 1 || feed.transportApplied[0] != "ft-1" {
		t.Fatalf("feed-transport applier never ran: %v", feed.transportApplied)
	}
	publish(t, "counts", "shifting_event", "sh-1")
	if len(shifting.applied) != 1 || shifting.applied[0] != "sh-1" {
		t.Fatalf("shifting applier never ran: %v", shifting.applied)
	}
	publish(t, "weighing", "weighing_observation", "wg-1")
	// The apply-RECEIPT must travel with the apply. Without it the verdict lands on the
	// observation but the verifier's item stays reading as "decided, not yet in effect"
	// forever -- the same silent-drop shape as an unregistered applier, one hop later.
	if len(weighingAck.acked) != 1 || weighingAck.acked[0] != "wg-1" {
		t.Fatalf("weighing apply-receipt never sent: %v -- an applied verdict would read as still-being-applied forever", weighingAck.acked)
	}
	if len(weighing.applied) != 1 || weighing.applied[0] != "wg-1" {
		t.Fatalf("weighing applier never ran: %v", weighing.applied)
	}

	// A rework verdict must reach the same appliers, not only approve.
	if err := bus.Publish(context.Background(), eventbus.Event{
		ID:       "20000000-0000-4000-8000-000000000002",
		Type:     "verification.verdict.rework",
		TenantID: tenant,
		Payload:  []byte(`{"reason":"re-shoot","source":{"module":"feed","ref_type":"feed_distribution_completion","ref_id":"fd-2"}}`),
	}); err != nil {
		t.Fatalf("publish rework: %v", err)
	}
	if len(feed.distributionBounced) != 1 || feed.distributionBounced[0] != "fd-2" {
		t.Fatalf("feed-distribution rework applier never ran: %v", feed.distributionBounced)
	}
}

// fakeWeighingAcker captures the apply-receipt weighing sends verification after a
// verdict has landed on the observation.
type fakeWeighingAcker struct {
	acked []string
}

func (f *fakeWeighingAcker) AckWeighingVerificationApplied(_ context.Context, _, _ string, observationIDs []string) error {
	f.acked = append(f.acked, observationIDs...)
	return nil
}
