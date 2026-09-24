package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/platform/readcache"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// TestVaccinationReadCacheHitsInsideABucketAndIsEvictedByACommittedObligationWrite pins both halves
// of the vaccination read-cache contract:
//
//  1. Two reads inside one 30 s bucket are ONE database read. The old key carried as_of in
//     RFC3339Nano, so two reads a nanosecond apart were two keys and the cache never hit.
//  2. A committed write by ANY writer -- here a bare INSERT, standing in for the reconciler, the
//     sweeper or a repair script, none of which know this cache exists -- evicts the entry on
//     this instance through the table trigger's NOTIFY and the readcache listener, so the next
//     read sees the write instead of a pre-write board for the rest of the bucket.
func TestVaccinationReadCacheHitsInsideABucketAndIsEvictedByACommittedObligationWrite(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tenantID := "00000000-0000-4000-8000-0000000000c7"
	parkID := "70000000-0000-4000-8000-0000010000c7"
	shedID := "70000000-0000-4000-8000-0000020000c7"
	protocolID := "70000000-0000-4000-8000-0000060000c0"
	protocolVersionID := "70000000-0000-4000-8000-0000060000c7"
	ruleID := "70000000-0000-4000-8000-0000070000c7"
	partyID := "70000000-0000-4000-8000-00000a0000c7"
	firstGoat := "70000000-0000-4000-8000-0000030000c1"
	secondGoat := "70000000-0000-4000-8000-0000030000c2"
	seedKPIFixtureBase(t, ctx, pool, tenantID, parkID, shedID, protocolID, protocolVersionID, ruleID, partyID, "c7")
	seedKPIGoat(t, ctx, pool, tenantID, shedID, partyID, firstGoat)
	seedKPIGoat(t, ctx, pool, tenantID, shedID, partyID, secondGoat)

	asOf := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	insertObligation := func(obligationID, goatID, key string) {
		execProjectionSQL(t, ctx, pool, "scheduled obligation",
			`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, target_id, target_type, scope_type, scope_id, rule_id, status, due_at, idempotency_key)
			 VALUES ($1, $2, $3, $4, 'goat', 'shed', $5, $6, 'scheduled', $7::timestamptz, $8)`,
			obligationID, tenantID, protocolVersionID, goatID, shedID, ruleID, asOf.Add(3*24*time.Hour), key)
	}
	insertObligation("70000000-0000-4000-8000-0000080000c1", firstGoat, "cache-evict-c1")

	repo := NewRepository(pool, 5*time.Second)
	readcache.NewListener(pool, nil, repo.ReadCache()).Start(ctx)
	waitUntil(t, 5*time.Second, func() bool { return repo.ReadCache().Coherent() })

	read := func(at time.Time) domain.CommandBoardResponse {
		t.Helper()
		resp, err := repo.VaccinationCommandBoard(ctx, domain.CommandBoardQuery{TenantID: tenantID, AsOf: at})
		if err != nil {
			t.Fatalf("VaccinationCommandBoard() error = %v", err)
		}
		return resp
	}
	if got := read(asOf).KPIs.Targets; got != 1 {
		t.Fatalf("targets = %d, want 1", got)
	}
	misses := repo.ReadCache().Stats().Misses
	if got := read(asOf.Add(20 * time.Second)).KPIs.Targets; got != 1 {
		t.Fatalf("targets = %d, want 1", got)
	}
	if after := repo.ReadCache().Stats().Misses; after != misses {
		t.Fatalf("a read 20s later in the same 30s bucket missed the cache (misses %d -> %d)", misses, after)
	}

	insertObligation("70000000-0000-4000-8000-0000080000c2", secondGoat, "cache-evict-c2")
	waitUntil(t, 5*time.Second, func() bool { return read(asOf).KPIs.Targets == 2 })

	// A DOSE RECORD (vaccination_completions) evicts: the next read in the same bucket shows it.
	execProjectionSQL(t, ctx, pool, "recorded dose",
		`INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, goat_id, status, administered_at, idempotency_key)
		 VALUES ('70000000-0000-4000-8000-0000090000c1', $1, '70000000-0000-4000-8000-0000080000c1', $2, 'recorded', $3::timestamptz, 'cache-evict-dose-c1')`,
		tenantID, firstGoat, asOf.Add(-time.Hour))
	waitUntil(t, 5*time.Second, func() bool { return read(asOf).KPIs.AwaitingVerification == 1 })

	// A VERIFICATION DECISION (UPDATE on the completion) evicts.
	execProjectionSQL(t, ctx, pool, "accept dose",
		`UPDATE vaccination_completions SET status = 'accepted', verified_at = $2::timestamptz
		 WHERE tenant_id = $1 AND completion_id = '70000000-0000-4000-8000-0000090000c1'`,
		tenantID, asOf.Add(-time.Minute))
	waitUntil(t, 5*time.Second, func() bool {
		k := read(asOf).KPIs
		return k.DosesVerified == 1 && k.AwaitingVerification == 0
	})

	// AN OBLIGATION CANCEL evicts, and the canceled obligation leaves the targets (maintainer
	// decision 2026-09-24: canceled is not a target).
	execProjectionSQL(t, ctx, pool, "cancel obligation",
		`UPDATE obligation_instances SET status = 'canceled'
		 WHERE tenant_id = $1 AND obligation_id = '70000000-0000-4000-8000-0000080000c2'`, tenantID)
	waitUntil(t, 5*time.Second, func() bool { return read(asOf).KPIs.Targets == 1 })

	// A write in ANOTHER tenant does not evict this tenant's entry.
	before := repo.ReadCache().Stats().Misses
	execProjectionSQL(t, ctx, pool, "other-tenant write",
		`SELECT pg_notify('goatos_read_cache_evict', json_build_object('tenant_id', '00000000-0000-4000-8000-0000000000c8', 'caches', json_build_array('vaccination'))::text)`)
	time.Sleep(200 * time.Millisecond)
	read(asOf)
	if after := repo.ReadCache().Stats().Misses; after != before {
		t.Fatalf("another tenant's eviction dropped this tenant's entry (misses %d -> %d)", before, after)
	}
}

func waitUntil(t *testing.T, limit time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met before the deadline: the committed write never evicted the cached board")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
