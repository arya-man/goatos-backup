package app

import (
	"context"
	"sync/atomic"

	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
)

// F1: a fresh (stream) read must never be answered by a compute that started BEFORE the NOTIFY
// it is reacting to: it waits for that flight, then gets a post-notify compute.
func TestLiveCacheFreshReadAfterNotifyDuringComputeGetsPostNotifyData(t *testing.T) {
	c := newLiveCohortCache[int]()
	release := make(chan struct{})
	var calls atomic.Int32
	compute := func(context.Context) (int, error) {
		n := int(calls.Add(1))
		if n == 1 {
			<-release // the pre-notify compute is slow
		}
		return n, nil
	}
	ctx := context.Background()
	key := liveCohortKey("t1")
	first := make(chan int, 1)
	go func() {
		v, _ := c.get(ctx, key, true, compute)
		first <- v
	}()
	for calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	c.invalidate("t1") // NOTIFY lands while compute #1 is running
	second := make(chan int, 1)
	go func() {
		v, _ := c.get(ctx, key, true, compute)
		second <- v
	}()
	time.Sleep(20 * time.Millisecond)
	close(release)
	if v := <-first; v != 1 {
		t.Fatalf("pre-notify caller got %d, want 1", v)
	}
	select {
	case v := <-second:
		if v != 2 {
			t.Fatalf("post-notify fresh caller got %d (pre-notify data), want 2", v)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("post-notify fresh caller never returned")
	}
}

// A mapping bind NOTIFYs; the very next ordinary page load (not a stream read) must reflect it,
// not a stale-while-revalidate copy of the pre-bind snapshot.
func TestPageLoadAfterMappingBindReflectsIt(t *testing.T) {
	repo := manyMappedTags(4)
	svc := NewService(repo)
	actor := domain.Actor{TenantID: "tenant-1", UserID: "user-1"}
	ctx := context.Background()
	resp, err := svc.ListLive(ctx, actor, nil, nil, nil, nil, nil, nil, nil, nil, "", 10, domain.LiveSort{})
	if err != nil || resp.Summary.TagsSeen != 4 {
		t.Fatalf("first read summary = %d err %v", resp.Summary.TagsSeen, err)
	}
	// "Bind": a fifth tag is now mapped; the write path NOTIFYs.
	extra := manyMappedTags(5)
	repo.livePages, repo.resolvedTags, repo.goats = extra.livePages, extra.resolvedTags, extra.goats
	svc.InvalidateLive("tenant-1")
	resp, err = svc.ListLive(ctx, actor, nil, nil, nil, nil, nil, nil, nil, nil, "", 10, domain.LiveSort{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Summary.TagsSeen != 5 {
		t.Fatalf("page load after bind summary = %d, want 5 (served the pre-bind snapshot)", resp.Summary.TagsSeen)
	}
}
