package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
)

// ---- fakes -------------------------------------------------------------------

type fakeRow struct {
	id       string
	goatID   string
	axis     string
	target   string
	reason   string
	expected *int
	state    string // pending|claimed|applied|skipped|error|retry
	retry    int
	eventID  string
	failure  string
}

type fakeRepo struct {
	mu            sync.Mutex
	rows          map[string]*fakeRow
	order         []string
	reproStatuses map[string]bool
	nextID        int
	enqueued      *EnqueueJobParams
	enqueueErr    error
	jobState      string
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{rows: map[string]*fakeRow{}, reproStatuses: map[string]bool{}, jobState: JobStatePending}
}

func (f *fakeRepo) seed(goatID, axis, target string, expected *int) string {
	f.nextID++
	id := fmt.Sprintf("row-%d", f.nextID)
	f.rows[id] = &fakeRow{id: id, goatID: goatID, axis: axis, target: target, expected: expected, state: "pending"}
	f.order = append(f.order, id)
	return id
}

func (f *fakeRepo) ReproductiveStatusExists(_ context.Context, code string) (bool, error) {
	return f.reproStatuses[code], nil
}

func (f *fakeRepo) EnqueueJob(_ context.Context, params EnqueueJobParams) (EnqueueJobResult, error) {
	if f.enqueueErr != nil {
		return EnqueueJobResult{}, f.enqueueErr
	}
	f.enqueued = &params
	return EnqueueJobResult{JobID: "job-1", TotalRows: len(params.Rows), State: JobStatePending}, nil
}

func (f *fakeRepo) ClaimRows(_ context.Context, _, _ string, limit, _ int) ([]ClaimedRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]ClaimedRow, 0, limit)
	for _, id := range f.order {
		if len(out) >= limit {
			break
		}
		row := f.rows[id]
		if row.state == "pending" || row.state == "retry" {
			row.state = "claimed"
			out = append(out, ClaimedRow{
				RowID: row.id, GoatID: row.goatID, Axis: row.axis, Target: row.target,
				Reason: row.reason, ExpectedRowVersion: row.expected, RetryCount: row.retry,
			})
		}
	}
	return out, nil
}

func (f *fakeRepo) MarkRowApplied(_ context.Context, _, rowID, eventID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows[rowID].state = "applied"
	f.rows[rowID].eventID = eventID
	return nil
}

func (f *fakeRepo) MarkRowSkipped(_ context.Context, _, rowID, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows[rowID].state = "skipped"
	f.rows[rowID].failure = reason
	return nil
}

func (f *fakeRepo) MarkRowError(_ context.Context, _, rowID, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows[rowID].state = "error"
	f.rows[rowID].failure = reason
	return nil
}

func (f *fakeRepo) MarkRowRetry(_ context.Context, _, rowID, reason string, maxRetries int) (RowOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	row := f.rows[rowID]
	row.retry++
	row.failure = reason
	if row.retry >= maxRetries {
		row.state = "error"
		return OutcomeError, nil
	}
	row.state = "retry"
	return OutcomeRetry, nil
}

func (f *fakeRepo) RefreshJobCounts(_ context.Context, _, _ string) (JobCounts, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var c JobCounts
	for _, id := range f.order {
		c.Total++
		switch f.rows[id].state {
		case "applied":
			c.Applied++
		case "skipped":
			c.Skipped++
		case "error":
			c.Failed++
		default:
			c.Remaining++
		}
	}
	if c.Remaining > 0 {
		c.State = JobStateRunning
	} else {
		c.State = JobStateCompleted
	}
	f.jobState = c.State
	return c, nil
}

func (f *fakeRepo) ListJobIDsWithClaimableRows(_ context.Context, _ string, _ int) ([]string, error) {
	return []string{"job-1"}, nil
}

type fakeReader struct {
	states map[string]GoatState
}

func (r fakeReader) ReadGoatStates(_ context.Context, _ string, goatIDs []string) (map[string]GoatState, error) {
	out := map[string]GoatState{}
	for _, id := range goatIDs {
		if st, ok := r.states[id]; ok {
			out[id] = st
		}
	}
	return out, nil
}

type fakeApplier struct {
	fn func(ApplyRowRequest) (ApplyRowResult, error)
}

func (a fakeApplier) ApplyBulkStatusRow(_ context.Context, req ApplyRowRequest) (ApplyRowResult, error) {
	return a.fn(req)
}

const (
	goatA = "10000000-0000-4000-8000-000000000001"
	goatB = "10000000-0000-4000-8000-000000000002"
	goatC = "10000000-0000-4000-8000-000000000003"
)

func ptr(i int) *int { return &i }

// ---- token tests -------------------------------------------------------------

func TestPreviewTokenRoundTripAndTamper(t *testing.T) {
	svc := NewService(newFakeRepo(), fakeReader{}).WithSigningKey("k")
	fp := rowsFingerprint("t1", AxisHealth, []EnqueueRow{{GoatID: goatA, Target: "sick"}})
	token, err := svc.signPreviewToken("t1", AxisHealth, fp, 1)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if err := svc.verifyPreviewToken("t1", AxisHealth, fp, 1, token); err != nil {
		t.Fatalf("verify valid token: %v", err)
	}
	if err := svc.verifyPreviewToken("t1", AxisHealth, fp, 2, token); err == nil {
		t.Fatal("expected count mismatch to fail verification")
	}
	if err := svc.verifyPreviewToken("t2", AxisHealth, fp, 1, token); err == nil {
		t.Fatal("expected tenant mismatch to fail verification")
	}
	other := NewService(newFakeRepo(), fakeReader{}).WithSigningKey("other-key")
	if err := other.verifyPreviewToken("t1", AxisHealth, fp, 1, token); err == nil {
		t.Fatal("expected wrong signing key to fail verification")
	}
}

func TestFingerprintOrderIndependent(t *testing.T) {
	a := rowsFingerprint("t1", AxisHealth, []EnqueueRow{{GoatID: goatA, Target: "sick"}, {GoatID: goatB, Target: "healthy"}})
	b := rowsFingerprint("t1", AxisHealth, []EnqueueRow{{GoatID: goatB, Target: "healthy"}, {GoatID: goatA, Target: "sick"}})
	if a != b {
		t.Fatal("fingerprint must be order-independent")
	}
	c := rowsFingerprint("t1", AxisHealth, []EnqueueRow{{GoatID: goatA, Target: "healthy"}, {GoatID: goatB, Target: "healthy"}})
	if a == c {
		t.Fatal("fingerprint must change when a target changes")
	}
}

// ---- preview tests -----------------------------------------------------------

func previewBody(t *testing.T, axis string, rows []PreviewRow) []byte {
	t.Helper()
	raw, err := json.Marshal(PreviewRequest{Axis: axis, Rows: rows})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestPreviewDecisions(t *testing.T) {
	repo := newFakeRepo()
	repo.reproStatuses["pregnant"] = true
	reader := fakeReader{states: map[string]GoatState{
		goatA: {GoatID: goatA, Exists: true, LifecycleStatus: "active", ReproductiveStatus: "open", RowVersion: 3},
		goatB: {GoatID: goatB, Exists: true, LifecycleStatus: "active", ReproductiveStatus: "pregnant", RowVersion: 1},
		goatC: {GoatID: goatC, Exists: true, LifecycleStatus: "sold", RowVersion: 9},
	}}
	svc := NewService(repo, reader).WithSigningKey("k")

	rows := []PreviewRow{
		{GoatID: goatA, Target: "pregnant"},                                  // apply
		{GoatID: goatB, Target: "pregnant"},                                  // noop (already pregnant)
		{GoatID: goatC, Target: "pregnant"},                                  // not_found (exited)
		{GoatID: "10000000-0000-4000-8000-000000000004", Target: "pregnant"}, // not_found (missing)
		{GoatID: "10000000-0000-4000-8000-000000000005", Target: "made-up"},  // requires_review (bad target)
	}
	resp, err := svc.Preview(context.Background(), PreviewInput{TenantID: "t1", RawBody: previewBody(t, AxisReproductive, rows)})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if resp.PreviewToken == "" {
		t.Fatal("expected a preview token")
	}
	got := map[string]string{}
	for _, r := range resp.Rows {
		got[r.GoatID+"|"+r.Target] = r.Decision
	}
	assertDecision(t, got, goatA+"|pregnant", DecisionApply)
	assertDecision(t, got, goatB+"|pregnant", DecisionNoop)
	assertDecision(t, got, goatC+"|pregnant", DecisionNotFound)
	if resp.Summary.Apply != 1 || resp.Summary.Noop != 1 || resp.Summary.NotFound != 2 || resp.Summary.RequiresReview != 1 {
		t.Fatalf("unexpected summary: %+v", resp.Summary)
	}
}

func TestPreviewHealthGuardrailBlocked(t *testing.T) {
	reader := fakeReader{states: map[string]GoatState{
		goatA: {GoatID: goatA, Exists: true, LifecycleStatus: "active", HealthStatus: "healthy", RowVersion: 1},
	}}
	svc := NewService(newFakeRepo(), reader).WithSigningKey("k")
	resp, err := svc.Preview(context.Background(), PreviewInput{TenantID: "t1", RawBody: previewBody(t, AxisHealth, []PreviewRow{{GoatID: goatA, Target: "icu"}})})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if resp.Rows[0].Decision != DecisionBlocked {
		t.Fatalf("expected icu to be blocked (guardrail), got %s", resp.Rows[0].Decision)
	}
}

func TestPreviewRejectsBadAxisAndDuplicates(t *testing.T) {
	svc := NewService(newFakeRepo(), fakeReader{}).WithSigningKey("k")
	if _, err := svc.Preview(context.Background(), PreviewInput{TenantID: "t1", RawBody: previewBody(t, "weight", nil)}); err == nil {
		t.Fatal("expected invalid axis error")
	}
	dup := []PreviewRow{{GoatID: goatA, Target: "sick"}, {GoatID: goatA, Target: "healthy"}}
	if _, err := svc.Preview(context.Background(), PreviewInput{TenantID: "t1", RawBody: previewBody(t, AxisHealth, dup)}); err == nil {
		t.Fatal("expected duplicate goat_id error")
	}
}

// ---- commit tests ------------------------------------------------------------

func TestCommitVerifiesTokenAndEnqueues(t *testing.T) {
	repo := newFakeRepo()
	repo.reproStatuses["pregnant"] = true
	reader := fakeReader{states: map[string]GoatState{
		goatA: {GoatID: goatA, Exists: true, LifecycleStatus: "active", ReproductiveStatus: "open", RowVersion: 3},
	}}
	svc := NewService(repo, reader).WithSigningKey("k")

	rows := []PreviewRow{{GoatID: goatA, Target: "pregnant"}}
	preview, err := svc.Preview(context.Background(), PreviewInput{TenantID: "t1", RawBody: previewBody(t, AxisReproductive, rows)})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}

	commitRaw, _ := json.Marshal(CommitRequest{Axis: AxisReproductive, Rows: rows, PreviewToken: preview.PreviewToken})
	resp, err := svc.Commit(context.Background(), CommitInput{TenantID: "t1", ActorID: goatA, IdempotencyKey: "idem-1", RawBody: commitRaw})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if resp.JobID != "job-1" || resp.TotalRows != 1 {
		t.Fatalf("unexpected commit response: %+v", resp)
	}
	if repo.enqueued == nil || repo.enqueued.Fingerprint == "" || repo.enqueued.IdempotencyKey != "idem-1" {
		t.Fatalf("expected enqueue with fingerprint + idempotency key, got %+v", repo.enqueued)
	}

	// Tampered rows must fail token verification.
	tampered, _ := json.Marshal(CommitRequest{Axis: AxisReproductive, Rows: []PreviewRow{{GoatID: goatB, Target: "pregnant"}}, PreviewToken: preview.PreviewToken})
	if _, err := svc.Commit(context.Background(), CommitInput{TenantID: "t1", ActorID: goatA, IdempotencyKey: "idem-2", RawBody: tampered}); err == nil {
		t.Fatal("expected tampered commit rows to fail token verification")
	}
}

func TestCommitRequiresIdempotencyKey(t *testing.T) {
	svc := NewService(newFakeRepo(), fakeReader{}).WithSigningKey("k")
	raw, _ := json.Marshal(CommitRequest{Axis: AxisHealth, Rows: []PreviewRow{{GoatID: goatA, Target: "sick"}}, PreviewToken: "v1:1:aa:bb"})
	_, err := svc.Commit(context.Background(), CommitInput{TenantID: "t1", ActorID: goatA, RawBody: raw})
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "missing_idempotency_key" {
		t.Fatalf("expected missing_idempotency_key, got %v", err)
	}
}

// ---- worker tests ------------------------------------------------------------

func drainSettings() WorkerSettings {
	return WorkerSettings{Concurrency: 2, RowsPerSecond: 0, Burst: 1, BatchSize: 10, MaxRetries: 2, MaxIterations: 50}
}

func TestWorkerAppliesRows(t *testing.T) {
	repo := newFakeRepo()
	repo.seed(goatA, AxisReproductive, "pregnant", ptr(1))
	repo.seed(goatB, AxisReproductive, "pregnant", ptr(2))
	applier := fakeApplier{fn: func(req ApplyRowRequest) (ApplyRowResult, error) {
		return ApplyRowResult{Outcome: OutcomeApplied, EventID: "evt-" + req.GoatID}, nil
	}}
	w := NewWorkerService(repo, applier, drainSettings(), nil)
	drain, err := w.RunUntilDrained(context.Background(), "t1", "job-1")
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if drain.Applied != 2 || drain.FinalCounts.State != JobStateCompleted || drain.FinalCounts.Remaining != 0 {
		t.Fatalf("unexpected drain: %+v", drain)
	}
	for _, id := range repo.order {
		if repo.rows[id].state != "applied" || repo.rows[id].eventID == "" {
			t.Fatalf("row %s not applied with event id: %+v", id, repo.rows[id])
		}
	}
}

func TestWorkerSkipsNoClobberAndMissingExpected(t *testing.T) {
	repo := newFakeRepo()
	repo.seed(goatA, AxisReproductive, "pregnant", ptr(1)) // applier reports no-clobber skip
	repo.seed(goatB, AxisReproductive, "pregnant", nil)    // no expected_row_version -> skipped without applier
	applier := fakeApplier{fn: func(req ApplyRowRequest) (ApplyRowResult, error) {
		return ApplyRowResult{Outcome: OutcomeSkipped, Reason: "row_version_mismatch_or_noop"}, nil
	}}
	w := NewWorkerService(repo, applier, drainSettings(), nil)
	drain, err := w.RunUntilDrained(context.Background(), "t1", "job-1")
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if drain.Skipped != 2 || drain.FinalCounts.Skipped != 2 {
		t.Fatalf("expected 2 skipped, got %+v", drain)
	}
}

func TestWorkerRetriesThenParksAsError(t *testing.T) {
	repo := newFakeRepo()
	repo.seed(goatA, AxisHealth, "sick", ptr(1))
	var attempts int
	var mu sync.Mutex
	applier := fakeApplier{fn: func(req ApplyRowRequest) (ApplyRowResult, error) {
		mu.Lock()
		attempts++
		mu.Unlock()
		return ApplyRowResult{}, errors.New("connection reset") // transient transport error
	}}
	w := NewWorkerService(repo, applier, drainSettings(), nil)
	drain, err := w.RunUntilDrained(context.Background(), "t1", "job-1")
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	// MaxRetries=2: attempt1 -> retry, attempt2 -> error.
	if repo.rows["row-1"].state != "error" {
		t.Fatalf("expected row to be parked as error, got %s", repo.rows["row-1"].state)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 apply attempts before parking, got %d", attempts)
	}
	if drain.FinalCounts.Failed != 1 {
		t.Fatalf("expected failed count 1, got %+v", drain.FinalCounts)
	}
}

func TestWorkerPermanentErrorOutcome(t *testing.T) {
	repo := newFakeRepo()
	repo.seed(goatA, AxisExit, "dead", ptr(1))
	applier := fakeApplier{fn: func(req ApplyRowRequest) (ApplyRowResult, error) {
		return ApplyRowResult{Outcome: OutcomeError, Reason: "critical_death_transition_requires_guardrail"}, nil
	}}
	w := NewWorkerService(repo, applier, drainSettings(), nil)
	if _, err := w.RunUntilDrained(context.Background(), "t1", "job-1"); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if repo.rows["row-1"].state != "error" || repo.rows["row-1"].failure == "" {
		t.Fatalf("expected permanent error row, got %+v", repo.rows["row-1"])
	}
}

func assertDecision(t *testing.T, got map[string]string, key, want string) {
	t.Helper()
	if got[key] != want {
		t.Fatalf("decision for %s: got %q want %q", key, got[key], want)
	}
}
