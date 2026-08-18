package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
)

// Feed WASTAGE — service state machine proofs (maintainer decision 2026-08-18). The SQL is proved
// by the pg integration test; these prove the SERVICE rules without Docker: fail-closed wiring,
// mandatory proof, the experiment-pen membership gate on the WRITE, the enqueue firing exactly on
// a fresh pending transition with the pen's trial context, and the worklist deriving pens from the
// frozen experiment sheet with the status overlay.

type recordingWastageEnqueuer struct {
	calls []FeedWastageVerificationEnqueueRequest
	err   error
}

func (e *recordingWastageEnqueuer) EnqueueFeedWastageVerification(_ context.Context, in FeedWastageVerificationEnqueueRequest) error {
	if e.err != nil {
		return e.err
	}
	e.calls = append(e.calls, in)
	return nil
}

type fakeWastageStore struct {
	completeCalls []ports.CompleteWastageParams
	result        ports.CompleteWastageResult
	statuses      []ports.WastageCompletionStatus
}

func (f *fakeWastageStore) CompleteWastage(_ context.Context, p ports.CompleteWastageParams) (ports.CompleteWastageResult, error) {
	f.completeCalls = append(f.completeCalls, p)
	return f.result, nil
}

func (f *fakeWastageStore) ListWastageCompletionStatuses(context.Context, string, string, time.Time) ([]ports.WastageCompletionStatus, error) {
	return f.statuses, nil
}

func (f *fakeWastageStore) ApplyVerifiedWastage(context.Context, ports.ApplyWastageParams) (bool, error) {
	return false, nil
}

func (f *fakeWastageStore) BounceWastageForRework(context.Context, ports.BounceWastageParams) (bool, error) {
	return false, nil
}

func (f *fakeWastageStore) RecordWastageMeasurement(context.Context, ports.RecordWastageMeasurementParams) (ports.RecordWastageMeasurementResult, error) {
	return ports.RecordWastageMeasurementResult{}, nil
}

// newWastageService wires a service whose park runs BOTH clocks, with the EXPERIMENT sheet
// authorable: shed A carries a hand-authored experiment ration (so its pens are experiment pens),
// shed B stays on the per-head grid. "Now" is pinned AFTER the experiment dispatch clock so the
// first wastage read freezes the sheet, mirroring how packing freezes with its direction sheet.
func newWastageService(store *fakeWastageStore, enq FeedWastageVerificationEnqueuer) (*Service, *fakeConfigRepo) {
	svc, config, _ := newTestService()
	config.snapshot.ExperimentByLocation[domain.ExperimentLocationKey(shedA, "")] = []domain.ExperimentCell{
		{FeedItemLabel: "Concentrate", FeedItemKey: "concentrate", AbsoluteKg: "12.000", Category: "Trial A"},
	}
	issueStore := newFakeIssueStore()
	sched := &fakeScheduleReader{clocks: []domain.WorkflowClock{normalClock(), experimentClock()}, parks: []string{testPark}}
	// 18:00 IST on the target date: past the 14:00 experiment direction clock, so the serve path
	// may freeze the day's sheet on first read.
	now := time.Date(2026, 7, 19, 18, 0, 0, 0, targetDate().Location())
	svc = svc.WithIssueStore(issueStore).WithScheduleReader(sched).WithClock(func() time.Time { return now })
	if store != nil {
		svc = svc.WithWastageStore(store)
	}
	if enq != nil {
		svc = svc.WithWastageVerificationEnqueuer(enq)
	}
	return svc, config
}

func wastageInput(shedID string) CompleteWastageInput {
	return CompleteWastageInput{
		TenantID:        testTenant,
		ParkID:          testPark,
		ShedID:          shedID,
		TargetDate:      targetDate(),
		WastageProofRef: "proof-wastage-1",
		CompletedBy:     "op-1",
		IdempotencyKey:  "wastage-key-00000001",
		ActorID:         "op-1",
		ActorType:       "operator",
	}
}

func TestCompleteWastageFailsClosedOnMissingWiringAndProof(t *testing.T) {
	t.Parallel()

	// No store wired: refuse before anything else.
	bare, _, _ := newTestService()
	if _, err := bare.CompleteWastage(context.Background(), wastageInput(shedA)); !errors.Is(err, ports.ErrWastageStoreUnavailable) {
		t.Fatalf("no store err = %v, want ErrWastageStoreUnavailable", err)
	}

	// Store but no enqueue seam: a completion would strand a pending row nobody reviews.
	storeOnly, _, _ := newTestService()
	storeOnly = storeOnly.WithWastageStore(&fakeWastageStore{})
	if _, err := storeOnly.CompleteWastage(context.Background(), wastageInput(shedA)); !errors.Is(err, ErrWastageEnqueuerNotWired) {
		t.Fatalf("no enqueuer err = %v, want ErrWastageEnqueuerNotWired", err)
	}

	store := &fakeWastageStore{}
	enq := &recordingWastageEnqueuer{}
	svc, _ := newWastageService(store, enq)

	missingProof := wastageInput(shedA)
	missingProof.WastageProofRef = ""
	if _, err := svc.CompleteWastage(context.Background(), missingProof); !errors.Is(err, ports.ErrWastageProofRequired) {
		t.Fatalf("missing proof err = %v, want ErrWastageProofRequired", err)
	}

	missingKey := wastageInput(shedA)
	missingKey.IdempotencyKey = ""
	if _, err := svc.CompleteWastage(context.Background(), missingKey); !errors.Is(err, ports.ErrIdempotencyRequired) {
		t.Fatalf("missing idempotency err = %v, want ErrIdempotencyRequired", err)
	}

	if len(store.completeCalls) != 0 || len(enq.calls) != 0 {
		t.Fatalf("rejected requests reached the store (%d) or the queue (%d)", len(store.completeCalls), len(enq.calls))
	}
}

// The WRITE enforces experiment-pen membership from the frozen sheet: a pen the day's experiment
// sheet does not cover — shed B here, which is on the per-head grid — is refused, and a day whose
// experiment sheet cannot exist yet refuses too rather than storing unreachable work.
func TestCompleteWastageRefusesAPenOffTheExperimentSheet(t *testing.T) {
	t.Parallel()
	store := &fakeWastageStore{result: ports.CompleteWastageResult{CompletionID: "c-1", Status: domain.WastageStatusPendingVerification, RowVersion: 1, NewlyPending: true}}
	enq := &recordingWastageEnqueuer{}
	svc, _ := newWastageService(store, enq)

	if _, err := svc.CompleteWastage(context.Background(), wastageInput(shedB)); !errors.Is(err, ports.ErrWastageNotExperimentPen) {
		t.Fatalf("normal-grid pen err = %v, want ErrWastageNotExperimentPen", err)
	}
	if len(store.completeCalls) != 0 {
		t.Fatal("a refused pen must not reach the store")
	}
}

// The happy path: an experiment pen's completion reaches the store with the experiment workflow
// implied by the store (no workflow field to send), and the enqueue fires exactly once on the
// fresh pending transition, carrying the pen's trial group and head count as verifier context.
func TestCompleteWastageEnqueuesWithTrialContextOnFreshPending(t *testing.T) {
	t.Parallel()
	store := &fakeWastageStore{result: ports.CompleteWastageResult{
		CompletionID: "c-1", Status: domain.WastageStatusPendingVerification, RowVersion: 1,
		NewlyPending: true, ShedName: "Shed A",
	}}
	enq := &recordingWastageEnqueuer{}
	svc, _ := newWastageService(store, enq)

	res, err := svc.CompleteWastage(context.Background(), wastageInput(shedA))
	if err != nil {
		t.Fatalf("CompleteWastage: %v", err)
	}
	if !res.NewlyPending {
		t.Fatal("NewlyPending = false, want true")
	}
	if len(enq.calls) != 1 {
		t.Fatalf("enqueue calls = %d, want 1", len(enq.calls))
	}
	call := enq.calls[0]
	if call.CompletionID != "c-1" || call.ShedID != shedA {
		t.Fatalf("enqueue call = %+v, want completion c-1 for shed A", call)
	}
	if call.ExperimentArm != "Trial A" {
		t.Fatalf("ExperimentArm = %q, want the authored trial group — the verifier judges the "+
			"leftover against the trial the pen is on", call.ExperimentArm)
	}
	if call.HeadCountSummary == "" {
		t.Fatal("HeadCountSummary is blank, want the pen's projected head count")
	}
	if call.IdempotencyKey != "feed-wastage-verification:c-1:1" {
		t.Fatalf("enqueue idempotency key = %q, want completion+row_version keyed", call.IdempotencyKey)
	}

	// A replay (NewlyPending false) enqueues nothing more.
	store.result.NewlyPending = false
	replay := wastageInput(shedA)
	replay.IdempotencyKey = "wastage-key-00000002"
	if _, err := svc.CompleteWastage(context.Background(), replay); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(enq.calls) != 1 {
		t.Fatalf("enqueue calls after replay = %d, want still 1", len(enq.calls))
	}
}

// The worklist derives ONE row per experiment PEN from the frozen sheet — normal-grid sheds never
// appear, sessions collapse — and the status overlay + summary buckets come from the store rows.
func TestWastageWorklistListsExperimentPensWithStatusOverlay(t *testing.T) {
	t.Parallel()
	store := &fakeWastageStore{statuses: []ports.WastageCompletionStatus{
		{ShedID: shedA, PartitionLabel: "", Status: domain.WastageStatusRework, ReworkReason: "Retake the video with the scale visible.", WastageKg: ""},
	}}
	svc, _ := newWastageService(store, &recordingWastageEnqueuer{})

	page, err := svc.WastageWorklist(context.Background(), domain.WastageQuery{
		TenantID:   testTenant,
		ParkID:     testPark,
		TargetDate: targetDate(),
	})
	if err != nil {
		t.Fatalf("WastageWorklist: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("items = %d (%+v), want exactly the one experiment pen — shed B is on the grid "+
			"and a pen must appear once, not once per session", len(page.Items), page.Items)
	}
	row := page.Items[0]
	if row.ShedID != shedA || row.Workflow != domain.WorkflowExperiment || row.ExperimentArm != "Trial A" {
		t.Fatalf("row = %+v, want shed A / experiment / Trial A", row)
	}
	// A rework row surfaces as the operator's pending bucket, carrying the reason verbatim.
	if row.LifecycleStatus != domain.SessionStatusPending || row.ReworkReason == "" {
		t.Fatalf("rework row = status %q reason %q, want pending with the backend sentence", row.LifecycleStatus, row.ReworkReason)
	}
	if page.Summary.TotalPens != 1 || page.Summary.PendingPens != 1 || page.Summary.CompletedPens != 0 {
		t.Fatalf("summary = %+v, want 1 total / 1 pending", page.Summary)
	}
}
