package safety

import (
	"context"
	"testing"
	"time"
)

func TestSemaphoreBoundsConcurrency(t *testing.T) {
	s, _ := NewSemaphore(SemaphoreConfig{MaxConcurrent: 2, AcquireTimeout: 0})
	ctx := context.Background()

	r1, err := s.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := s.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.Available() != 0 {
		t.Fatalf("expected 0 slots free, got %d", s.Available())
	}
	// Third acquire with fail-fast (no timeout) sheds load.
	if _, err := s.Acquire(ctx); err != ErrOverloaded {
		t.Fatalf("expected ErrOverloaded, got %v", err)
	}
	r1()
	// Now a slot is free.
	r3, err := s.Acquire(ctx)
	if err != nil {
		t.Fatalf("should acquire after release, got %v", err)
	}
	r2()
	r3()
	if s.Available() != 2 {
		t.Fatalf("all slots should be returned, got %d", s.Available())
	}
}

func TestSemaphoreWaitsThenSheds(t *testing.T) {
	s, _ := NewSemaphore(SemaphoreConfig{MaxConcurrent: 1, AcquireTimeout: 30 * time.Millisecond})
	ctx := context.Background()
	r1, err := s.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := s.Acquire(ctx); err != ErrOverloaded {
		t.Fatalf("expected shed after wait, got %v", err)
	}
	if time.Since(start) < 25*time.Millisecond {
		t.Fatal("should have waited approximately the acquire timeout")
	}
	r1()
}

func TestSemaphoreReleaseIdempotent(t *testing.T) {
	s, _ := NewSemaphore(SemaphoreConfig{MaxConcurrent: 1})
	r, err := s.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r()
	r() // double release must not over-fill the pool
	if s.Available() != 1 {
		t.Fatalf("double release must not exceed capacity, got %d", s.Available())
	}
}

func TestSemaphoreHonorsContextCancel(t *testing.T) {
	s, _ := NewSemaphore(SemaphoreConfig{MaxConcurrent: 1, AcquireTimeout: time.Second})
	r1, _ := s.Acquire(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Acquire(ctx); err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	r1()
}
