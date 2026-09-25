package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

type noRows struct{}

func (noRows) Next() bool                { return false }
func (noRows) Scan(...any) error         { return nil }
func (noRows) Err() error                { return nil }
func stmt(sql string) sqlbind.BoundQuery { return sqlbind.MustBind(sql) }

// recordingBatch answers every statement with an empty result set, or fails the whole batch.
type recordingBatch struct {
	mu      sync.Mutex
	batches [][]string
	fail    bool
}

func (b *recordingBatch) RunBatch(_ context.Context, stmts []ports.Statement) error {
	b.mu.Lock()
	sqls := make([]string, len(stmts))
	for i, st := range stmts {
		sqls[i] = st.Query.SQL()
	}
	b.batches = append(b.batches, sqls)
	b.mu.Unlock()
	if b.fail {
		return errors.New("batch failed")
	}
	for _, st := range stmts {
		if err := st.Read(noRows{}); err != nil {
			return err
		}
	}
	return nil
}

// batchCountSource is a BatchSource whose statement yields fixed counts; its direct
// CountByState records the deadline it was given and can be made to fail or stall.
type batchCountSource struct {
	*fakeSource
	counts   map[domain.WorkState]int
	fail     bool
	stall    time.Duration
	mu       sync.Mutex
	direct   int
	deadline time.Duration
}

func (s *batchCountSource) CountStatement(_ ports.SourceQuery, out *map[domain.WorkState]int) (ports.Statement, error) {
	return ports.Statement{Query: stmt("SELECT '" + s.kind + "'"), Read: func(ports.ResultRows) error {
		*out = s.counts
		return nil
	}}, nil
}

func (s *batchCountSource) ListStatement(_ ports.SourceQuery, out *[]domain.Row) (ports.Statement, error) {
	return ports.Statement{Query: stmt("SELECT 'list " + s.kind + "'"), Read: func(ports.ResultRows) error { return nil }}, nil
}

func (s *batchCountSource) CountByState(ctx context.Context, _ ports.SourceQuery) (map[domain.WorkState]int, error) {
	s.mu.Lock()
	s.direct++
	if d, ok := ctx.Deadline(); ok {
		s.deadline = time.Until(d)
	}
	s.mu.Unlock()
	if s.stall > 0 {
		select {
		case <-time.After(s.stall):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if s.fail {
		return nil, errors.New("source down")
	}
	return s.counts, nil
}

type primeKey struct{ q string }

// primingSource primes one gate read into the request memo and counts from it.
type primingSource struct {
	*fakeSource
	direct int
}

func (s *primingSource) PrimeStatements(ctx context.Context, q ports.SourceQuery) ([]ports.Statement, error) {
	return []ports.Statement{{Query: stmt("SELECT 'prime'"), Read: func(ports.ResultRows) error {
		ports.SeedRequestRead(ctx, primeKey{q.ParkID}, 3)
		return nil
	}}}, nil
}

func (s *primingSource) CountByState(ctx context.Context, q ports.SourceQuery) (map[domain.WorkState]int, error) {
	n, err := ports.RequestRead(ctx, primeKey{q.ParkID}, func(context.Context) (int, error) {
		s.direct++
		return 1, nil
	})
	return map[domain.WorkState]int{domain.WorkStateDue: n}, err
}

func TestSummaryPrimesGateReadsIntoTheCountBatch(t *testing.T) {
	a := &batchCountSource{fakeSource: mk(domain.ModuleHealth, "a", 0, domain.WorkStateDue, ""), counts: map[domain.WorkState]int{domain.WorkStateDue: 2}}
	b := &batchCountSource{fakeSource: mk(domain.ModulePCCare, "b", 0, domain.WorkStateDue, ""), counts: map[domain.WorkState]int{domain.WorkStateInProgress: 1}}
	p := &primingSource{fakeSource: mk(domain.ModuleVaccination, "v", 0, domain.WorkStateDue, "")}
	batch := &recordingBatch{}
	svc := NewService(a, b, p).WithStatementBatch(batch)
	sum, err := svc.Summary(ports.WithRequestReadMemo(context.Background()), baseQuery())
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.batches) != 1 || len(batch.batches[0]) != 3 {
		t.Fatalf("batches = %v, want ONE batch carrying both counts and the prime", batch.batches)
	}
	if p.direct != 0 || a.direct != 0 || b.direct != 0 {
		t.Fatalf("direct reads prime=%d a=%d b=%d, want none: the batch answered them", p.direct, a.direct, b.direct)
	}
	if sum.ByModule[domain.ModuleVaccination] != 3 || sum.ByModule[domain.ModuleHealth] != 2 || len(sum.Degraded) != 0 {
		t.Fatalf("summary = %+v", sum)
	}
}

// A failed batch falls back to per-source reads: each on its OWN fresh budget, concurrently, so a
// stalled source neither spends a sibling's deadline nor delays it, and a failing source degrades
// alone. The priming source reads for itself.
func TestSummaryBatchFallbackGivesEachSourceItsOwnBudget(t *testing.T) {
	slow := &batchCountSource{fakeSource: mk(domain.ModuleHealth, "a", 0, domain.WorkStateDue, ""), counts: map[domain.WorkState]int{domain.WorkStateDue: 2}, stall: 300 * time.Millisecond}
	down := &batchCountSource{fakeSource: mk(domain.ModulePCCare, "b", 0, domain.WorkStateDue, ""), fail: true}
	late := &batchCountSource{fakeSource: mk(domain.ModuleWeighing, "c", 0, domain.WorkStateDue, ""), counts: map[domain.WorkState]int{domain.WorkStateDue: 1}}
	p := &primingSource{fakeSource: mk(domain.ModuleVaccination, "v", 0, domain.WorkStateDue, "")}
	svc := NewService(slow, down, late, p).WithStatementBatch(&recordingBatch{fail: true})
	start := time.Now()
	sum, err := svc.Summary(ports.WithRequestReadMemo(context.Background()), baseQuery())
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 550*time.Millisecond {
		t.Fatalf("fallback took %v: the per-source reads must run concurrently", elapsed)
	}
	if len(sum.Degraded) != 1 || sum.Degraded[0] != domain.ModulePCCare {
		t.Fatalf("degraded = %v, want only the failing source", sum.Degraded)
	}
	if sum.ByModule[domain.ModuleHealth] != 2 || sum.ByModule[domain.ModuleWeighing] != 1 || sum.ByModule[domain.ModuleVaccination] != 1 {
		t.Fatalf("summary = %+v", sum)
	}
	if p.direct != 1 {
		t.Fatalf("priming source read for itself %d times, want 1 after the failed batch", p.direct)
	}
	for _, s := range []*batchCountSource{slow, down, late} {
		if s.direct != 1 || s.deadline < maxSummarySourceDuration-100*time.Millisecond {
			t.Fatalf("%s: direct=%d deadline=%v, want one read on a fresh %v budget", s.kind, s.direct, s.deadline, maxSummarySourceDuration)
		}
	}
}

// GET /work-board/summary carries no request memo, so a priming source is not primed while
// two batch sources still batch their counts. The count goroutine and Summary must not both
// close the phase-one channel (STG: "panic: close of closed channel" took the API down).
func TestSummaryWithoutRequestMemoClosesPhaseOneOnce(t *testing.T) {
	a := &batchCountSource{fakeSource: mk(domain.ModuleHealth, "a", 0, domain.WorkStateDue, ""), counts: map[domain.WorkState]int{domain.WorkStateDue: 2}}
	b := &batchCountSource{fakeSource: mk(domain.ModulePCCare, "b", 0, domain.WorkStateDue, ""), counts: map[domain.WorkState]int{domain.WorkStateInProgress: 1}}
	p := &primingSource{fakeSource: mk(domain.ModuleVaccination, "v", 0, domain.WorkStateDue, "")}
	svc := NewService(a, b, p).WithStatementBatch(&recordingBatch{})
	sum, err := svc.Summary(context.Background(), baseQuery())
	if err != nil {
		t.Fatal(err)
	}
	if sum.ByModule[domain.ModuleHealth] != 2 || sum.ByModule[domain.ModuleVaccination] != 1 || len(sum.Degraded) != 0 {
		t.Fatalf("summary = %+v", sum)
	}
}
