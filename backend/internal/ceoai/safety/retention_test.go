package safety

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakePurger records calls and deletes from a synthetic set of expiries.
type fakePurger struct {
	remaining int
	calls     int
	err       error
	lastAsOf  time.Time
	lastLimit int
}

func (p *fakePurger) PurgeExpired(_ context.Context, asOf time.Time, limit int) (int, error) {
	p.calls++
	p.lastAsOf = asOf
	p.lastLimit = limit
	if p.err != nil {
		return 0, p.err
	}
	n := p.remaining
	if n > limit {
		n = limit
	}
	p.remaining -= n
	return n, nil
}

func TestRetentionRunOnceBatches(t *testing.T) {
	p := &fakePurger{remaining: 1200}
	c := NewRetentionCleaner(p, RetentionConfig{BatchLimit: 500, MaxBatchesPerTick: 20}, fixedClock(time.Now()), nil)
	total, err := c.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if total != 1200 {
		t.Fatalf("expected 1200 purged, got %d", total)
	}
	// 500 + 500 + 200 => 3 batches (last short-batch stops the loop).
	if p.calls != 3 {
		t.Fatalf("expected 3 batches, got %d", p.calls)
	}
	if p.lastLimit != 500 {
		t.Fatalf("batch limit not propagated, got %d", p.lastLimit)
	}
}

func TestRetentionRunOnceRespectsMaxBatches(t *testing.T) {
	p := &fakePurger{remaining: 100000}
	c := NewRetentionCleaner(p, RetentionConfig{BatchLimit: 100, MaxBatchesPerTick: 3}, fixedClock(time.Now()), nil)
	total, err := c.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if p.calls != 3 || total != 300 {
		t.Fatalf("max batches per tick not honored: calls=%d total=%d", p.calls, total)
	}
}

func TestRetentionPropagatesError(t *testing.T) {
	p := &fakePurger{remaining: 10, err: errors.New("db down")}
	c := NewRetentionCleaner(p, DefaultRetentionConfig(), fixedClock(time.Now()), nil)
	if _, err := c.RunOnce(context.Background()); err == nil {
		t.Fatal("purge error must not be swallowed")
	}
}

func TestRetentionRunStopsOnContext(t *testing.T) {
	p := &fakePurger{remaining: 100000}
	c := NewRetentionCleaner(p, RetentionConfig{Interval: time.Millisecond, BatchLimit: 1, MaxBatchesPerTick: 1000}, fixedClock(time.Now()), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// Run returns promptly because ctx is already cancelled.
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not stop on cancelled context")
	}
}

func TestRetentionExpiry(t *testing.T) {
	created := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	exp := RetentionExpiry(created, 30)
	if !exp.Equal(created.AddDate(0, 0, 30)) {
		t.Fatalf("unexpected expiry %v", exp)
	}
	if !RetentionExpiry(created, 0).IsZero() {
		t.Fatal("zero window should yield zero time")
	}
}
