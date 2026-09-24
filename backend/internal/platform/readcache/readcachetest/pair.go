// Package readcachetest is the Postgres-gated read-your-writes harness for writers of the tables
// the shared analytics read cache depends on: two cache instances (the writer's process and a
// sibling API process), each with its own LISTEN connection, exactly as bootstrap wires them.
package readcachetest

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/readcache"
)

// Limit is the read-your-writes bound for every writer: a sibling instance (and the writer itself
// when the caller owns the transaction) must serve the new value within one second.
const Limit = time.Second

// Pair is two API instances' caches. Writer is the one to inject into the repository under test.
type Pair struct {
	Writer  *readcache.Cache
	Sibling *readcache.Cache
	gen     atomic.Int64
}

// NewPair starts both caches and their listeners and waits until both are coherent.
func NewPair(t *testing.T, ctx context.Context, pool *pgxpool.Pool) *Pair {
	t.Helper()
	p := &Pair{
		Writer:  readcache.New(readcache.DefaultOptions("ryw-writer")),
		Sibling: readcache.New(readcache.DefaultOptions("ryw-sibling")),
	}
	lctx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	for _, c := range []*readcache.Cache{p.Writer, p.Sibling} {
		readcache.NewListener(pool, nil, c).Start(lctx)
	}
	deadline := time.Now().Add(10 * time.Second)
	for !p.Writer.Coherent() || !p.Sibling.Coherent() {
		if time.Now().After(deadline) {
			t.Fatal("readcachetest: eviction listeners never connected")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return p
}

// Read stands in for a cached analytics read scoped to tenant+parks: it returns the value the
// cache serves, and a fresh load returns the current generation.
func (p *Pair) read(ctx context.Context, c *readcache.Cache, key readcache.Key) int64 {
	v, _ := readcache.Load(ctx, c, key, func(context.Context) (int64, error) { return p.gen.Load(), nil })
	return v
}

// Check primes a cached read of tenant+parks on both instances, runs write, and fails unless the
// writing instance's next read is fresh (at once when localFirst, else within Limit) and the
// sibling's within Limit. A write that must not evict (a rolled-back tx) is not what this checks.
func (p *Pair) Check(t *testing.T, ctx context.Context, name, tenantID string, parks []string, localFirst bool, write func(t *testing.T)) {
	t.Helper()
	key := readcache.Key{Tenant: tenantID, Parks: parks, Params: "readcachetest:" + name}
	before := p.gen.Add(1)
	for _, c := range []*readcache.Cache{p.Writer, p.Sibling} {
		if got := p.read(ctx, c, key); got != before {
			t.Fatalf("%s: prime read = %d, want %d", name, got, before)
		}
	}
	after := p.gen.Add(1) // the "database" now holds the post-write value
	// Cached reads keep serving the pre-write value until something evicts them.
	if got := p.read(ctx, p.Writer, key); got != before {
		t.Fatalf("%s: harness broken: cache did not hold the primed value", name)
	}
	write(t)
	if localFirst {
		if got := p.read(ctx, p.Writer, key); got != after {
			t.Fatalf("%s: the WRITING instance's next read is stale (served generation %d, want %d)", name, got, after)
		}
	} else {
		wait(t, name+" (writer instance, caller-owned tx)", func() bool { return p.read(ctx, p.Writer, key) == after })
	}
	took := wait(t, name+" (sibling instance)", func() bool { return p.read(ctx, p.Sibling, key) == after })
	t.Logf("%s: sibling fresh after %v", name, took.Round(time.Millisecond))
}

func wait(t *testing.T, what string, cond func() bool) time.Duration {
	t.Helper()
	start := time.Now()
	for !cond() {
		if time.Since(start) > Limit {
			t.Fatal(fmt.Sprintf("%s: still stale after %v", what, Limit))
		}
		time.Sleep(5 * time.Millisecond)
	}
	return time.Since(start)
}
