package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
)

// ---------------------------------------------------------------------------
// Fakes for the completion write path + serve overlay
// ---------------------------------------------------------------------------

type fakeCompletionStore struct {
	completed     []ports.CompletedSession
	listCalls     int
	completeCalls int
	lastParams    ports.CompleteSessionParams
	result        ports.CompleteSessionResult
	err           error
}

func (f *fakeCompletionStore) CompleteSession(_ context.Context, p ports.CompleteSessionParams) (ports.CompleteSessionResult, error) {
	f.completeCalls++
	f.lastParams = p
	if f.err != nil {
		return ports.CompleteSessionResult{}, f.err
	}
	if f.result.CompletionID != "" {
		return f.result, nil
	}
	return ports.CompleteSessionResult{CompletionID: "cmp-1", Applied: true, Status: "completed"}, nil
}

func (f *fakeCompletionStore) ListCompletedSessions(_ context.Context, _, _ string, _ time.Time) ([]ports.CompletedSession, error) {
	f.listCalls++
	return f.completed, nil
}

// fakeDistributionStore is the DISTRIBUTION verification-gated store (the NEW table the DIRECTION
// overlay reads verified sessions from -- separate from fakeCompletionStore, which is packing).
type fakeDistributionStore struct {
	verified    []ports.VerifiedDistribution
	statuses    []ports.SessionCompletionStatus
	listCalls   int
	statusCalls int
}

func (f *fakeDistributionStore) CompleteDistribution(_ context.Context, _ ports.CompleteDistributionParams) (ports.CompleteDistributionResult, error) {
	return ports.CompleteDistributionResult{}, nil
}

func (f *fakeDistributionStore) ListVerifiedDistributions(_ context.Context, _, _ string, _ time.Time) ([]ports.VerifiedDistribution, error) {
	f.listCalls++
	return f.verified, nil
}

func (f *fakeDistributionStore) ListDistributionSessionStatuses(_ context.Context, _, _ string, _ time.Time) ([]ports.SessionCompletionStatus, error) {
	f.statusCalls++
	return f.statuses, nil
}

func (f *fakeDistributionStore) ApplyVerifiedDistribution(_ context.Context, _ ports.ApplyDistributionParams) (bool, error) {
	return false, nil
}

func (f *fakeDistributionStore) BounceDistributionForRework(_ context.Context, _ ports.BounceDistributionParams) (bool, error) {
	return false, nil
}

// fakePackingStore is the PACKING verification-gated store (the NEW table the PACKING overlay reads
// verified sessions from -- separate from fakeCompletionStore, the inert instant path, and from
// fakeDistributionStore). Maintainer decision 2026-07-26 gated packing too.
type fakePackingStore struct {
	verified    []ports.VerifiedPacking
	statuses    []ports.PackingCompletionStatus
	listCalls   int
	statusCalls int
	// reopenCalls records every ReopenPackingForFeedChange the correction issued, so a test can
	// assert BOTH that a normal correction reopens the right pens and that an experiment one is
	// never called at all -- the two halves of the 2026-08-10 rule.
	reopenCalls    []ports.ReopenPackingParams
	reopenedIDs    []string
	reopenCallsErr error
}

func (f *fakePackingStore) CompletePacking(_ context.Context, _ ports.CompletePackingParams) (ports.CompletePackingResult, error) {
	return ports.CompletePackingResult{}, nil
}

func (f *fakePackingStore) ListVerifiedPacking(_ context.Context, _, _ string, _ time.Time) ([]ports.VerifiedPacking, error) {
	f.listCalls++
	return f.verified, nil
}

func (f *fakePackingStore) ListPackingCompletionStatuses(_ context.Context, _, _ string, _ time.Time) ([]ports.PackingCompletionStatus, error) {
	f.statusCalls++
	return f.statuses, nil
}

func (f *fakePackingStore) ApplyVerifiedPacking(_ context.Context, _ ports.ApplyPackingParams) (bool, error) {
	return false, nil
}

func (f *fakePackingStore) BouncePackingForRework(_ context.Context, _ ports.BouncePackingParams) (bool, error) {
	return false, nil
}

func (f *fakePackingStore) ReopenPackingForFeedChange(_ context.Context, p ports.ReopenPackingParams) (ports.ReopenPackingResult, error) {
	f.reopenCalls = append(f.reopenCalls, p)
	if f.reopenCallsErr != nil {
		return ports.ReopenPackingResult{}, f.reopenCallsErr
	}
	return ports.ReopenPackingResult{ReopenedCompletionIDs: f.reopenedIDs}, nil
}

type fakeProofValidator struct {
	calls   int
	lastIDs []string
	// lastExpected records the (ref, kind) pairs the service asked for, so a test can assert the
	// distribution path demands a PHOTO for the weight and VIDEOS for the other two rather than
	// merely that some validation happened.
	lastExpected []ports.ExpectedProofMedia
	err          error
	// kindErr, when set, is returned instead of err for the media-kind path, letting a test drive a
	// wrong-kind rejection without also failing plain presence validation.
	kindErr error
}

func (f *fakeProofValidator) ValidateFeedProofs(_ context.Context, _ string, ids []string) error {
	f.calls++
	f.lastIDs = ids
	return f.err
}

func (f *fakeProofValidator) ValidateLiveCameraVideo(_ context.Context, _, proofID, _ string) error {
	f.calls++
	f.lastIDs = []string{proofID}
	return f.err
}

func (f *fakeProofValidator) ValidateFeedProofMedia(_ context.Context, _ string, expected []ports.ExpectedProofMedia) error {
	f.calls++
	f.lastExpected = expected
	f.lastIDs = make([]string, 0, len(expected))
	for _, exp := range expected {
		f.lastIDs = append(f.lastIDs, exp.ProofID)
	}
	if f.kindErr != nil {
		return f.kindErr
	}
	return f.err
}

func validCompleteInput() CompleteSessionInput {
	return CompleteSessionInput{
		TenantID:       testTenant,
		ParkID:         testPark,
		ShedID:         shedA,
		SessionNo:      1,
		TargetDate:     targetDate(),
		Workflow:       domain.WorkflowNormal,
		IdempotencyKey: "feed-complete-123456",
	}
}

// ---------------------------------------------------------------------------
// Serve overlay: a completed shed-session flips Completed, and the overlay does
// NOT add a config-snapshot read (the read-count invariant is preserved).
// ---------------------------------------------------------------------------

func TestPreviewOverlaysCompletedShedSessions(t *testing.T) {
	t.Parallel()
	// The DIRECTION overlay now reads the DISTRIBUTION verification-gated table (maintainer decision,
	// 2026-07-26): a session is Completed only after a verifier approves, i.e. status='completed' in
	// feed_distribution_completions. The overlay reads ListDistributionSessionStatuses (which also
	// surfaces pending_verification/rework for the status filter), one bounded read per request.
	store := &fakeDistributionStore{}
	service, config, _ := newTestService()
	service.WithDistributionStore(store)

	q := domain.PreviewQuery{Draft: true, TenantID: testTenant, ParkID: testPark, TargetDate: targetDate()}
	page, err := service.Preview(context.Background(), q)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	for _, r := range page.Items {
		if r.Completed {
			t.Fatalf("no completion recorded, but row (%s s%d) reports completed", r.ShedID, r.SessionNo)
		}
		if r.LifecycleStatus != domain.SessionStatusPending {
			t.Fatalf("no completion recorded, but row (%s s%d) status=%q, want pending", r.ShedID, r.SessionNo, r.LifecycleStatus)
		}
	}
	if store.statusCalls != 1 {
		t.Fatalf("ListDistributionSessionStatuses calls = %d, want 1 (one overlay read per request)", store.statusCalls)
	}
	// The overlay is a DEDICATED read: it must not have added a config snapshot read.
	if config.snapshotCalls != 1 {
		t.Fatalf("config snapshot reads = %d, want exactly 1 (overlay must not touch the snapshot)", config.snapshotCalls)
	}

	// Now VERIFY exactly the first row's shed-session (status='completed') and re-serve.
	target := page.Items[0]
	store.statuses = []ports.SessionCompletionStatus{{ShedID: target.ShedID, SessionNo: target.SessionNo, Workflow: target.Workflow, Status: domain.SessionStatusCompleted}}
	page2, err := service.Preview(context.Background(), q)
	if err != nil {
		t.Fatalf("Preview 2: %v", err)
	}
	matched := false
	for _, r := range page2.Items {
		want := r.ShedID == target.ShedID && r.SessionNo == target.SessionNo && r.Workflow == target.Workflow
		if r.Completed != want {
			t.Fatalf("row (%s s%d %s) completed=%v, want %v", r.ShedID, r.SessionNo, r.Workflow, r.Completed, want)
		}
		if want {
			matched = true
		}
	}
	if !matched {
		t.Fatal("the completed shed-session was not present in the re-served page")
	}
}

func TestPackingOverlaysCompletedShedSessions(t *testing.T) {
	t.Parallel()
	// The PACKING overlay reads the PACKING verification-gated table (maintainer decision
	// 2026-07-26): a shed-session is Completed only after a verifier approves, i.e. status='completed' in
	// feed_packing_completions. The overlay reads ListPackingCompletionStatuses (which also surfaces
	// pending_verification/rework for the status filter).
	//
	// The overlay key is the shed-SESSION: shed + pen + session + workflow. This test stamps ONE line
	// and asserts nothing else moves, which is what fails if the key ever loses the pen (the
	// 2026-08-08 defect where one Castro - 1 clip marked Castro - 2 and Castro - 3 too) or the
	// session (the 2026-08-10 grain, where the morning's completion marked the evening packed).
	store := &fakePackingStore{}
	service, _, _ := newTestService()
	service.WithPackingStore(store)

	q := domain.PackingQuery{Draft: true, TenantID: testTenant, ParkID: testPark, TargetDate: targetDate()}
	page, err := service.PackingWorklist(context.Background(), q)
	if err != nil {
		t.Fatalf("PackingWorklist: %v", err)
	}
	if len(page.Items) == 0 {
		t.Fatal("no packing lines to overlay")
	}
	target := page.Items[0]
	store.statuses = []ports.PackingCompletionStatus{{
		ShedID:         target.ShedID,
		PartitionLabel: target.PartitionLabel,
		SessionNo:      target.SessionNo,
		Workflow:       target.Workflow,
		Status:         domain.SessionStatusCompleted,
	}}
	page2, err := service.PackingWorklist(context.Background(), q)
	if err != nil {
		t.Fatalf("PackingWorklist 2: %v", err)
	}
	matched := false
	for _, r := range page2.Items {
		want := r.ShedID == target.ShedID &&
			domain.PartitionMatchKey(r.PartitionLabel) == domain.PartitionMatchKey(target.PartitionLabel) &&
			r.SessionNo == target.SessionNo &&
			r.Workflow == target.Workflow
		if r.Completed != want {
			t.Fatalf("packing line (%s pen %q session %d %s) completed=%v, want %v",
				r.ShedID, r.PartitionLabel, r.SessionNo, r.Workflow, r.Completed, want)
		}
		if want {
			matched = true
		}
	}
	if !matched {
		t.Fatal("the completed shed-session was not present in the re-served page -- the overlay key missed every row")
	}
}

// The status filter narrows the WHOLE scope BEFORE paging (so a filtered page + its summary stay
// consistent) and the three buckets are correct: 'completed' keeps only verifier-approved
// shed-sessions, and 'pending' KEEPS a rework session (rework merges into pending) while EXCLUDING a
// completed one. Runs on the generated-preview path (the non-draft path that actually filters).
func TestPreviewStatusFilterNarrowsScopeBeforePaging(t *testing.T) {
	t.Parallel()
	// now = 2026-07-30 08:00 -> today 07-30 = feedDayTarget: the generated-preview path (no issue).
	now := istInstant(2026, 7, 30, 8)
	svc, _, _, _ := newLifecycleService(now)
	dist := &fakeDistributionStore{}
	svc.WithDistributionStore(dist)

	base := domain.PreviewQuery{TenantID: testTenant, ParkID: testPark, TargetDate: feedDayTarget()}

	// Unfiltered first: every shed-session is pending (no completion rows), so the fixture gives us the
	// real (shed, session, workflow) keys to mark.
	full, err := svc.Preview(context.Background(), base)
	if err != nil {
		t.Fatalf("Preview full: %v", err)
	}
	if len(full.Items) < 2 {
		t.Fatalf("need >= 2 rows to exercise filtering, got %d", len(full.Items))
	}
	for _, r := range full.Items {
		if r.LifecycleStatus != domain.SessionStatusPending || r.Completed {
			t.Fatalf("no completions, row (%s s%d) status=%q completed=%v, want pending/false", r.ShedID, r.SessionNo, r.LifecycleStatus, r.Completed)
		}
	}

	// Mark ONE shed-session completed and a DIFFERENT one rework.
	done := full.Items[0]
	var rework domain.DirectionRow
	for _, r := range full.Items {
		if r.ShedID != done.ShedID || r.SessionNo != done.SessionNo || r.Workflow != done.Workflow {
			rework = r
			break
		}
	}
	if rework.ShedID == "" {
		t.Fatal("fixture has only one shed-session; need a second to test the rework->pending merge")
	}
	dist.statuses = []ports.SessionCompletionStatus{
		{ShedID: done.ShedID, SessionNo: done.SessionNo, Workflow: done.Workflow, Status: domain.SessionStatusCompleted},
		{ShedID: rework.ShedID, SessionNo: rework.SessionNo, Workflow: rework.Workflow, Status: "rework"},
	}

	// status=completed -> ONLY the completed shed-session's grains, all stamped completed.
	comp, err := svc.Preview(context.Background(), withStatus(base, domain.SessionStatusCompleted))
	if err != nil {
		t.Fatalf("Preview completed: %v", err)
	}
	if len(comp.Items) == 0 {
		t.Fatal("completed filter returned no rows")
	}
	for _, r := range comp.Items {
		if r.ShedID != done.ShedID || r.SessionNo != done.SessionNo || r.Workflow != done.Workflow {
			t.Fatalf("completed filter leaked row (%s s%d %s)", r.ShedID, r.SessionNo, r.Workflow)
		}
		if r.LifecycleStatus != domain.SessionStatusCompleted || !r.Completed {
			t.Fatalf("completed row status=%q completed=%v", r.LifecycleStatus, r.Completed)
		}
	}
	// Summary describes the FILTERED scope, not the whole sheet: exactly the one completed shed.
	if comp.Summary.ShedCount != 1 {
		t.Fatalf("completed-filter summary ShedCount=%d, want 1 (summary must track the filtered scope)", comp.Summary.ShedCount)
	}

	// status=pending -> the rework session is present (merged into pending); the completed one is gone.
	pend, err := svc.Preview(context.Background(), withStatus(base, domain.SessionStatusPending))
	if err != nil {
		t.Fatalf("Preview pending: %v", err)
	}
	sawRework, sawDone := false, false
	for _, r := range pend.Items {
		if r.ShedID == done.ShedID && r.SessionNo == done.SessionNo && r.Workflow == done.Workflow {
			sawDone = true
		}
		if r.ShedID == rework.ShedID && r.SessionNo == rework.SessionNo && r.Workflow == rework.Workflow {
			sawRework = true
			if r.LifecycleStatus != domain.SessionStatusPending {
				t.Fatalf("rework row not merged to pending: status=%q", r.LifecycleStatus)
			}
		}
	}
	if sawDone {
		t.Fatal("pending filter leaked the completed shed-session")
	}
	if !sawRework {
		t.Fatal("pending filter dropped the rework shed-session (rework must merge into pending)")
	}
}

func withStatus(q domain.PreviewQuery, status string) domain.PreviewQuery {
	q.Status = status
	return q
}

// ---------------------------------------------------------------------------
// CompleteSession: availability, validation, proof, param threading
// ---------------------------------------------------------------------------

func TestCompleteSessionUnavailableWithoutStore(t *testing.T) {
	t.Parallel()
	service, _, _ := newTestService() // no completion store wired
	_, err := service.CompleteSession(context.Background(), validCompleteInput())
	if !errors.Is(err, ports.ErrCompletionUnavailable) {
		t.Fatalf("err = %v, want ErrCompletionUnavailable", err)
	}
}

func TestCompleteSessionValidation(t *testing.T) {
	t.Parallel()
	store := &fakeCompletionStore{}
	service, _, _ := newTestService()
	service.WithCompletionStore(store)

	cases := []struct {
		name string
		mut  func(*CompleteSessionInput)
		want error
	}{
		{"missing shed", func(i *CompleteSessionInput) { i.ShedID = "" }, ports.ErrShedRequired},
		{"zero session", func(i *CompleteSessionInput) { i.SessionNo = 0 }, ports.ErrInvalidSession},
		{"empty workflow", func(i *CompleteSessionInput) { i.Workflow = "" }, ports.ErrWorkflowRequired},
		{"bad workflow", func(i *CompleteSessionInput) { i.Workflow = "weird" }, ports.ErrInvalidWorkflow},
		{"missing idempotency", func(i *CompleteSessionInput) { i.IdempotencyKey = "" }, ports.ErrIdempotencyRequired},
		{"missing date", func(i *CompleteSessionInput) { i.TargetDate = time.Time{} }, ports.ErrInvalidTargetDate},
	}
	for _, tc := range cases {
		in := validCompleteInput()
		tc.mut(&in)
		if _, err := service.CompleteSession(context.Background(), in); !errors.Is(err, tc.want) {
			t.Fatalf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
	if store.completeCalls != 0 {
		t.Fatalf("invalid inputs reached the store %d times, want 0", store.completeCalls)
	}
}

func TestCompleteSessionRejectsInvalidProof(t *testing.T) {
	t.Parallel()
	store := &fakeCompletionStore{}
	proof := &fakeProofValidator{err: ports.ErrInvalidProof}
	service, _, _ := newTestService()
	service.WithCompletionStore(store).WithProofValidator(proof)

	in := validCompleteInput()
	in.ProofRefs = []domain.ProofRef{{ProofID: "proof-1"}}
	_, err := service.CompleteSession(context.Background(), in)
	if !errors.Is(err, ports.ErrInvalidProof) {
		t.Fatalf("err = %v, want ErrInvalidProof", err)
	}
	if proof.calls != 1 || len(proof.lastIDs) != 1 || proof.lastIDs[0] != "proof-1" {
		t.Fatalf("proof validator calls = %d ids = %v, want 1 call with [proof-1]", proof.calls, proof.lastIDs)
	}
	if store.completeCalls != 0 {
		t.Fatal("an invalid proof reached the store, want the write blocked")
	}
}

func TestCompleteSessionThreadsNormalizedParams(t *testing.T) {
	t.Parallel()
	store := &fakeCompletionStore{}
	proof := &fakeProofValidator{}
	service, _, _ := newTestService()
	service.WithCompletionStore(store).WithProofValidator(proof)

	in := validCompleteInput()
	in.ProofRefs = []domain.ProofRef{{ProofID: "proof-1"}}
	res, err := service.CompleteSession(context.Background(), in)
	if err != nil {
		t.Fatalf("CompleteSession: %v", err)
	}
	if !res.Applied || res.CompletionID == "" {
		t.Fatalf("result = %+v, want applied with a completion id", res)
	}
	if store.completeCalls != 1 {
		t.Fatalf("store completeCalls = %d, want 1", store.completeCalls)
	}
	p := store.lastParams
	if p.ShedID != shedA || p.SessionNo != 1 || p.Workflow != domain.WorkflowNormal {
		t.Fatalf("params = (%s s%d %s), want (shedA,1,normal)", p.ShedID, p.SessionNo, p.Workflow)
	}
	// The date reaches the store normalized to the business-day start.
	if !p.TargetDate.Equal(targetDate()) {
		t.Fatalf("params target date = %v, want %v (business-day start)", p.TargetDate, targetDate())
	}
	if len(p.ProofRefs) != 1 || p.ProofRefs[0].ProofID != "proof-1" {
		t.Fatalf("params proof refs = %+v, want [proof-1]", p.ProofRefs)
	}
	if proof.calls != 1 {
		t.Fatalf("proof validator calls = %d, want 1", proof.calls)
	}
}

func TestCompleteSessionSkipsProofValidationWhenNoRefs(t *testing.T) {
	t.Parallel()
	store := &fakeCompletionStore{}
	proof := &fakeProofValidator{err: ports.ErrInvalidProof} // would fail if called
	service, _, _ := newTestService()
	service.WithCompletionStore(store).WithProofValidator(proof)

	// No proof refs: the optional-video contract means validation is skipped entirely.
	if _, err := service.CompleteSession(context.Background(), validCompleteInput()); err != nil {
		t.Fatalf("CompleteSession with no proof: %v", err)
	}
	if proof.calls != 0 {
		t.Fatalf("proof validator calls = %d, want 0 (no refs to validate)", proof.calls)
	}
	if store.completeCalls != 1 {
		t.Fatalf("store completeCalls = %d, want 1", store.completeCalls)
	}
}

// A reopened pen must be able to SAY it was reopened.
//
// 'rework' has no client bucket of its own -- it normalizes to "pending", the operator's "needs my
// action" state -- so an operator looking at the card cannot tell a pen whose video was thrown away
// from one they never packed. The stored sentence is the only thing that distinguishes them, and it
// must not survive onto any other state: a stale reason on a re-submitted pen would tell a packer to
// redo work they have already redone.
func TestPackingReworkReasonIsCarriedOnlyWhileThePenIsActuallyInRework(t *testing.T) {
	t.Parallel()
	const reason = "Animals moved in or out of this pen, so the feed quantities changed. Pack the new amounts and record a new video."

	store := &fakePackingStore{}
	service, _, _ := newTestService()
	service.WithPackingStore(store)

	q := domain.PackingQuery{Draft: true, TenantID: testTenant, ParkID: testPark, TargetDate: targetDate()}
	seed, err := service.PackingWorklist(context.Background(), q)
	if err != nil {
		t.Fatalf("PackingWorklist: %v", err)
	}
	if len(seed.Items) == 0 {
		t.Fatal("no packing lines to overlay")
	}
	target := seed.Items[0]

	find := func(page domain.PackingPage) domain.PackingRow {
		t.Helper()
		for _, r := range page.Items {
			if r.ShedID == target.ShedID &&
				domain.PartitionMatchKey(r.PartitionLabel) == domain.PartitionMatchKey(target.PartitionLabel) &&
				r.SessionNo == target.SessionNo &&
				r.Workflow == target.Workflow {
				return r
			}
		}
		t.Fatal("the overlaid line was not present in the re-served page -- the overlay key missed every row")
		return domain.PackingRow{}
	}

	// In rework: the sentence reaches the card, and the card still reads as the operator's to act on.
	store.statuses = []ports.PackingCompletionStatus{{
		ShedID: target.ShedID, PartitionLabel: target.PartitionLabel, SessionNo: target.SessionNo,
		Workflow: target.Workflow,
		Status:   domain.PackingStatusRework, ReworkReason: reason,
	}}
	reworked, err := service.PackingWorklist(context.Background(), q)
	if err != nil {
		t.Fatalf("PackingWorklist (rework): %v", err)
	}
	row := find(reworked)
	if row.ReworkReason != reason {
		t.Fatalf("rework reason = %q, want the stored sentence -- without it the card is indistinguishable from an unpacked pen", row.ReworkReason)
	}
	if row.LifecycleStatus != domain.SessionStatusPending {
		t.Fatalf("lifecycle status = %q, want pending: a reopened pen is the operator's to act on again", row.LifecycleStatus)
	}

	// Re-submitted: the row is awaiting a verdict again. The reason must be gone even if a stale one
	// were still stored, or the packer is told to repack what they just repacked.
	store.statuses = []ports.PackingCompletionStatus{{
		ShedID: target.ShedID, PartitionLabel: target.PartitionLabel, SessionNo: target.SessionNo,
		Workflow: target.Workflow,
		Status:   domain.SessionStatusAwaitingVerification, ReworkReason: reason,
	}}
	resubmitted, err := service.PackingWorklist(context.Background(), q)
	if err != nil {
		t.Fatalf("PackingWorklist (resubmitted): %v", err)
	}
	if got := find(resubmitted).ReworkReason; got != "" {
		t.Fatalf("rework reason = %q on a re-submitted pen, want empty", got)
	}

	// A pen nobody has touched has no completion row at all and therefore no reason.
	store.statuses = nil
	untouched, err := service.PackingWorklist(context.Background(), q)
	if err != nil {
		t.Fatalf("PackingWorklist (untouched): %v", err)
	}
	if got := find(untouched).ReworkReason; got != "" {
		t.Fatalf("rework reason = %q on a pen with no completion row, want empty", got)
	}
}
