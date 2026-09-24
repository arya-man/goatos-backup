package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/md5"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/observability"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

func TestRetentionMustCoverRollupReadWindow(t *testing.T) {
	for _, tc := range []struct {
		args []string
		ok   bool
	}{
		{[]string{"-tenant-id=t"}, true}, // default 15 days, lookback 1
		{[]string{"-tenant-id=t", "-archive-bucket=gs://x"}, false},
		{[]string{"-tenant-id=t", "-archive-bucket=goatos-stg-analytics-archive"}, true},
		{[]string{"-tenant-id=t", "-lookback-days=3", "-retention-days=10"}, true},
		{[]string{"-tenant-id=t", "-lookback-days=3", "-retention-days=9"}, false},
		{[]string{"-tenant-id=t", "-lookback-days=7", "-retention-days=14"}, true},
		{[]string{"-tenant-id=t", "-retention-days=0"}, true},
		{[]string{"-tenant-id=t", "-retention-days=-1"}, false},
		{[]string{"-tenant-id=t", "-prune-batch-size=0"}, false},
		{[]string{"-tenant-id=t", "-work-mem=1GB"}, false},
		{[]string{"-tenant-id=t", "-chunk-pause=-1s"}, false},
	} {
		cfg, err := parseConfig(tc.args)
		if (err == nil) != tc.ok {
			t.Fatalf("%v: err=%v want ok=%v", tc.args, err, tc.ok)
		}
		if tc.ok && len(tc.args) == 1 && (cfg.RetentionDays != 15 || cfg.workMem() != "32MB" || cfg.ChunkPause != 5*time.Second) {
			t.Fatalf("defaults: %+v", cfg)
		}
	}
}

type fakeStore struct {
	mu       sync.Mutex
	objects  map[string][]byte
	creates  int
	failNext error
	truncate bool // simulate a partial/corrupt object landing in the store
}

func (f *fakeStore) Create(_ context.Context, name string, body []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creates++
	if err := f.failNext; err != nil {
		f.failNext = nil
		return err
	}
	if _, ok := f.objects[name]; ok {
		return errObjectExists
	}
	stored := append([]byte(nil), body...)
	if f.truncate {
		stored = stored[:len(stored)/2]
		f.truncate = false
	}
	f.objects[name] = stored
	return nil
}

func (f *fakeStore) Stat(_ context.Context, name string) (objectInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.objects[name]
	if !ok {
		return objectInfo{}, errors.New("not found")
	}
	sum := md5.Sum(b)
	return objectInfo{Size: int64(len(b)), MD5: sum[:]}, nil
}

func archivedRows(t *testing.T, body []byte) []map[string]any {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	sc := bufio.NewScanner(zr)
	for sc.Scan() {
		var row map[string]any
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		out = append(out, row)
	}
	return out
}

// export -> verify -> delete ordering; a failed or partial upload never
// deletes; a crash mid-delete resumes without re-uploading; rows inside the
// hot window (and clock-skewed recent rows) are never touched.
func TestArchiveVerifiesBeforeDeleteAndResumesPostgres(t *testing.T) {
	pool := pgtest.StartPostgres(t, context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	now := time.Date(2026, 9, 24, 21, 45, 0, 0, time.UTC)
	oldDay := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	add := func(received time.Time, name string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO analytics.app_events(tenant_id,event_name,properties,client_event_time,received_at) VALUES($1,$2,'{"proof_id":"p"}',$3,$3)`, testTenantID, name, received); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 7; i++ {
		add(oldDay.Add(time.Duration(i)*time.Hour), "route_entered")
	}
	add(oldDay.AddDate(0, 0, 1).Add(time.Hour), "next_day")
	add(now.AddDate(0, 0, -2), "hot")
	count := func(q string) int64 {
		t.Helper()
		var n int64
		if err := pool.QueryRow(ctx, q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	logger := observability.New(observability.Config{Service: "analytics-rollup-test"})
	store := &fakeStore{objects: map[string][]byte{}}
	cfg := config{RetentionDays: 15, ArchiveMaxDays: 2, PruneBatchSize: 3, PruneMaxRows: 100}

	// 1. Upload fails: nothing recorded, nothing deleted, error surfaced.
	store.failNext = errors.New("503 backend error")
	if _, err := archiveAndPruneAppEvents(ctx, pool, store, cfg, now, logger); err == nil {
		t.Fatal("failed upload reported success")
	}
	if got := count(`SELECT count(*) FROM analytics.app_events`); got != 9 {
		t.Fatalf("failed upload deleted rows: %d left", got)
	}
	// 2. Partial/corrupt object: verification fails, nothing deleted.
	store.truncate = true
	if _, err := archiveAndPruneAppEvents(ctx, pool, store, cfg, now, logger); err == nil {
		t.Fatal("partial upload passed verification")
	}
	if got := count(`SELECT count(*) FROM analytics.app_events`); got != 9 || count(`SELECT count(*) FROM analytics.app_events_archive`) != 0 {
		t.Fatalf("partial upload deleted rows or recorded archive: %d left", got)
	}
	store.objects = map[string][]byte{}

	// 3. Crash mid-delete: per-run cap stops after 4 rows of the 7-row day.
	capped := cfg
	capped.PruneMaxRows = 4
	res, err := archiveAndPruneAppEvents(ctx, pool, store, capped, now, logger)
	if err != nil || !res.Capped || res.RowsDeleted != 4 {
		t.Fatalf("capped run %+v err=%v", res, err)
	}
	if count(`SELECT count(*) FROM analytics.app_events_archive WHERE verified_at IS NOT NULL AND deleted_at IS NULL AND row_count=7`) != 1 {
		t.Fatal("verified archive not recorded before delete")
	}
	var body []byte
	for name, b := range store.objects {
		if len(name) < 30 || name[:26] != "app_events/dt=2026-09-01/p" {
			t.Fatalf("object name %q", name)
		}
		body = b
	}
	if rows := archivedRows(t, body); len(rows) != 7 || rows[0]["event_name"] != "route_entered" || rows[0]["properties"].(map[string]any)["proof_id"] != "p" {
		t.Fatalf("archived rows=%d first=%v", len(rows), rows[0])
	}
	creates := store.creates

	// 4. Resume: finishes day 1 without re-uploading, then archives day 2.
	res, err = archiveAndPruneAppEvents(ctx, pool, store, cfg, now, logger)
	if err != nil || res.DaysArchived != 2 || res.RowsDeleted != 4 {
		t.Fatalf("resume %+v err=%v", res, err)
	}
	if store.creates != creates+1 || len(store.objects) != 2 {
		t.Fatalf("resume re-uploaded a verified day: creates=%d objects=%d", store.creates, len(store.objects))
	}
	if got := count(`SELECT count(*) FROM analytics.app_events`); got != 1 {
		t.Fatalf("left=%d; want only the hot row", got)
	}
	if count(`SELECT count(*) FROM analytics.app_events_archive WHERE deleted_at IS NOT NULL`) != 2 {
		t.Fatal("archive ledger not closed out")
	}

	// 5. Crash after upload but before the ledger row: re-export is byte-identical,
	// create reports exists, stat verifies, delete proceeds.
	add(oldDay.AddDate(0, 0, 2), "crash_day")
	name := ""
	pre := &fakeStore{objects: map[string][]byte{}}
	if _, err := exportAndVerifyDay(ctx, pool, pre, cfg, oldDay.AddDate(0, 0, 2), logger); err != nil {
		t.Fatal(err)
	}
	for n, b := range pre.objects {
		name = n
		store.objects[n] = b
	}
	if _, err := pool.Exec(ctx, `DELETE FROM analytics.app_events_archive WHERE archive_date='2026-09-03'`); err != nil {
		t.Fatal(err)
	}
	if res, err = archiveAndPruneAppEvents(ctx, pool, store, cfg, now, logger); err != nil || res.DaysArchived != 1 {
		t.Fatalf("post-crash resume %+v err=%v (object %s)", res, err, name)
	}
	if count(`SELECT count(*) FROM analytics.app_events`) != 1 {
		t.Fatal("post-crash day not deleted")
	}

	// 6. No bucket configured: never deletes.
	add(oldDay.AddDate(0, 0, 4), "no_bucket")
	if res, err := archiveAndPruneAppEvents(ctx, pool, nil, cfg, now, logger); err != nil || res.RowsDeleted != 0 {
		t.Fatalf("deleted without an archive store: %+v %v", res, err)
	}
}
