package ports

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestRequestReadConcurrentDedupAndNewRequest(t *testing.T) {
	var calls atomic.Int32
	read := func(context.Context) (int, error) { return int(calls.Add(1)), nil }
	ctx := WithRequestReadMemo(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, err := RequestRead(ctx, "scope", read)
			if err != nil || value != 1 {
				t.Errorf("value=%d err=%v", value, err)
			}
		}()
	}
	wg.Wait()
	next, err := RequestRead(WithRequestReadMemo(ctx), "scope", read)
	if err != nil || next != 2 || calls.Load() != 2 {
		t.Fatalf("next request reused previous read: %d %d %v", next, calls.Load(), err)
	}
	_, _ = RequestRead(context.Background(), "scope", read)
	_, _ = RequestRead(context.Background(), "scope", read)
	if calls.Load() != 4 {
		t.Fatal("unscoped calls must not share")
	}
}
func TestRequestReadErrorsRetryAndTypesStaySeparate(t *testing.T) {
	ctx := WithRequestReadMemo(context.Background())
	failure := errors.New("transient")
	_, err := RequestRead(ctx, "same", func(context.Context) (int, error) { return 0, failure })
	if !errors.Is(err, failure) {
		t.Fatal(err)
	}
	value, err := RequestRead(ctx, "same", func(context.Context) (int, error) { return 7, nil })
	if value != 7 || err != nil {
		t.Fatalf("retry %d %v", value, err)
	}
	text, err := RequestRead(ctx, "same", func(context.Context) (string, error) { return "typed", nil })
	if text != "typed" || err != nil {
		t.Fatal("type collision")
	}
	empty, err := RequestRead(ctx, "nil", func(context.Context) (any, error) { return nil, nil })
	if empty != nil || err != nil {
		t.Fatal("nil interface")
	}
	empty, err = RequestRead(ctx, "nil", func(context.Context) (any, error) { t.Fatal("duplicate nil read"); return nil, nil })
	if empty != nil || err != nil {
		t.Fatal("cached nil interface")
	}
}
func TestRequestReadCanceledWaiterDoesNotCancelLeader(t *testing.T) {
	ctx := WithRequestReadMemo(context.Background())
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_, _ = RequestRead(ctx, "key", func(context.Context) (int, error) { close(started); <-release; return 9, nil })
	}()
	<-started
	waiter, cancel := context.WithCancel(ctx)
	cancel()
	_, err := RequestRead(waiter, "key", func(context.Context) (int, error) { t.Error("waiter duplicated read"); return 0, nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(release)
	<-finished
	value, err := RequestRead(ctx, "key", func(context.Context) (int, error) { t.Fatal("leader result lost"); return 0, nil })
	if value != 9 || err != nil {
		t.Fatal(value, err)
	}
}
