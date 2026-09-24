package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// analytics.app_events is a short hot buffer for the daily rollup (Grafana and
// every reader consume analytics.*_daily summaries). Rows older than
// RetentionDays are ARCHIVED to cold storage and only then deleted:
//
//  1. export one UTC received_at day, keyset-paged, to gzip JSONL (one object
//     per day, content-addressed name, create-only so an object is never
//     overwritten);
//  2. verify: exported row count equals the day's Postgres count, and the
//     stored object's size + MD5 equal the bytes written;
//  3. record the verified object in analytics.app_events_archive;
//  4. delete that day in small batches with pauses and a per-run row cap.
//
// A failed or partial upload never reaches step 3, so nothing is deleted. A
// crash between any two steps resumes: a verified day continues deleting; an
// unverified day re-exports identical bytes to the same name (create returns
// "exists") and is re-verified against the stored object. received_at is the
// ingest clock (DEFAULT now()), so a day older than retention can no longer
// gain rows; a count mismatch at delete time aborts that day anyway.
//
// The rollup reads at most lookback+7 days back (7-day WAU of the oldest
// lookback day); config rejects a shorter retention.

const (
	defaultRetentionDays       = 15
	defaultPruneBatchSize      = 5000
	defaultPruneMaxRows        = 250000
	defaultPruneBatchPause     = 2 * time.Second
	defaultArchiveMaxDays      = 2
	minRetentionBeyondLookback = 7
)

// objectStore is the create-only cold store (GCS in production).
type objectStore interface {
	// Create writes body at name only if no object exists there; it returns
	// errObjectExists when one does.
	Create(ctx context.Context, name string, body []byte) error
	// Stat returns the stored object's size and MD5.
	Stat(ctx context.Context, name string) (objectInfo, error)
}

type objectInfo struct {
	Size int64
	MD5  []byte
}

var errObjectExists = errors.New("archive object already exists")

type archiveResult struct {
	DaysArchived int
	RowsDeleted  int64
	Capped       bool
}

// Keyset page over one received_at day. to_jsonb gives every column with
// jsonb-sorted keys, so re-exporting an unchanged day is byte-identical.
const archivePageSQL = `
SELECT e.received_at, e.event_id, to_jsonb(e)::text
FROM analytics.app_events e
WHERE e.received_at >= $1 AND e.received_at < $2 AND (e.received_at, e.event_id) > ($3, $4::uuid)
ORDER BY e.received_at, e.event_id
LIMIT $5`

const archiveDeleteBatchSQL = `
DELETE FROM analytics.app_events
WHERE event_id IN (
 SELECT event_id FROM analytics.app_events
 WHERE received_at >= $1 AND received_at < $2
 ORDER BY received_at
 LIMIT $3)`

func archiveObjectName(day time.Time, sha string) string {
	return fmt.Sprintf("app_events/dt=%s/part-%s.jsonl.gz", day.Format("2006-01-02"), sha[:16])
}

func archiveAndPruneAppEvents(ctx context.Context, pool *pgxpool.Pool, store objectStore, cfg config, now time.Time, logger *slog.Logger) (archiveResult, error) {
	var res archiveResult
	if cfg.RetentionDays <= 0 {
		return res, nil
	}
	if store == nil {
		logger.InfoContext(ctx, "app_events_archive_not_configured", slog.String("next_step", "set GOATOS_ANALYTICS_ARCHIVE_BUCKET; rows are never deleted without a verified archive"))
		return res, nil
	}
	cutoff := now.UTC().Truncate(24*time.Hour).AddDate(0, 0, -cfg.RetentionDays)
	budget := int64(cfg.PruneMaxRows)
	maxDays := cfg.ArchiveMaxDays
	if maxDays <= 0 {
		maxDays = defaultArchiveMaxDays
	}
	for res.DaysArchived < maxDays {
		var oldest *time.Time
		// scale-guard:ignore: bounded by ArchiveMaxDays (<=31); min(received_at) is one index probe.
		if err := pool.QueryRow(ctx, `SELECT min(received_at) FROM analytics.app_events`).Scan(&oldest); err != nil {
			return res, fmt.Errorf("oldest app_events row: %w", err)
		}
		if oldest == nil || !oldest.UTC().Before(cutoff) {
			break
		}
		day := oldest.UTC().Truncate(24 * time.Hour)
		deleted, done, err := archiveDay(ctx, pool, store, cfg, day, budget, logger)
		res.RowsDeleted += deleted
		budget -= deleted
		if err != nil {
			return res, err
		}
		if !done {
			res.Capped = true
			break
		}
		res.DaysArchived++
	}
	logger.InfoContext(ctx, "app_events_archive_complete", slog.String("cutoff", cutoff.Format("2006-01-02")),
		slog.Int("days", res.DaysArchived), slog.Int64("deleted", res.RowsDeleted), slog.Bool("capped", res.Capped))
	return res, nil
}

// archiveDay returns rows deleted and whether the day is fully removed.
func archiveDay(ctx context.Context, pool *pgxpool.Pool, store objectStore, cfg config, day time.Time, budget int64, logger *slog.Logger) (int64, bool, error) {
	end := day.AddDate(0, 0, 1)
	var archived int64
	// scale-guard:ignore: one ledger lookup per archived day, bounded by ArchiveMaxDays.
	err := pool.QueryRow(ctx, `SELECT row_count FROM analytics.app_events_archive WHERE archive_date=$1 AND verified_at IS NOT NULL`, day.Format("2006-01-02")).Scan(&archived)
	if errors.Is(err, pgx.ErrNoRows) {
		archived, err = exportAndVerifyDay(ctx, pool, store, cfg, day, logger)
	}
	if err != nil {
		return 0, false, err
	}
	var remaining int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM analytics.app_events WHERE received_at >= $1 AND received_at < $2`, day, end).Scan(&remaining); err != nil {
		return 0, false, err
	}
	if remaining > archived {
		// Never delete rows the archive does not hold. Force a re-export.
		_, _ = pool.Exec(ctx, `UPDATE analytics.app_events_archive SET verified_at=NULL WHERE archive_date=$1`, day.Format("2006-01-02"))
		return 0, false, fmt.Errorf("app_events %s has %d rows but archive holds %d; not deleting", day.Format("2006-01-02"), remaining, archived)
	}
	var deleted int64
	for batch := 0; ; batch++ {
		if budget-deleted <= 0 {
			return deleted, false, nil
		}
		if batch > 0 && cfg.PruneBatchPause > 0 {
			select {
			case <-ctx.Done():
				return deleted, false, ctx.Err()
			case <-time.After(cfg.PruneBatchPause):
			}
		}
		limit := min(int64(cfg.PruneBatchSize), budget-deleted)
		// scale-guard:ignore: intentional bounded delete batching (row budget + pause), one set-based DELETE per batch.
		tag, err := pool.Exec(ctx, archiveDeleteBatchSQL, day, end, limit)
		if err != nil {
			return deleted, false, fmt.Errorf("delete archived app_events %s: %w", day.Format("2006-01-02"), err)
		}
		deleted += tag.RowsAffected()
		if tag.RowsAffected() < limit {
			break
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE analytics.app_events_archive SET deleted_at=now() WHERE archive_date=$1`, day.Format("2006-01-02")); err != nil {
		return deleted, false, err
	}
	logger.InfoContext(ctx, "app_events_day_archived", slog.String("day", day.Format("2006-01-02")), slog.Int64("rows", archived), slog.Int64("deleted", deleted))
	return deleted, true, nil
}

func exportAndVerifyDay(ctx context.Context, pool *pgxpool.Pool, store objectStore, cfg config, day time.Time, logger *slog.Logger) (int64, error) {
	end := day.AddDate(0, 0, 1)
	var gz bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&gz, gzip.BestCompression)
	zw.ModTime = time.Time{} // deterministic bytes for identical content
	hash := sha256.New()
	var rows int64
	lastAt, lastID := day.Add(-time.Microsecond), "00000000-0000-0000-0000-000000000000"
	for {
		n, err := exportPage(ctx, pool, day, end, &lastAt, &lastID, cfg.PruneBatchSize, func(line []byte) error {
			hash.Write(line)
			_, err := zw.Write(line)
			return err
		})
		if err != nil {
			return 0, fmt.Errorf("export app_events %s: %w", day.Format("2006-01-02"), err)
		}
		rows += int64(n)
		if n < cfg.PruneBatchSize {
			break
		}
	}
	if err := zw.Close(); err != nil {
		return 0, err
	}
	var count int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM analytics.app_events WHERE received_at >= $1 AND received_at < $2`, day, end).Scan(&count); err != nil {
		return 0, err
	}
	if count != rows {
		return 0, fmt.Errorf("export app_events %s: wrote %d rows but day has %d", day.Format("2006-01-02"), rows, count)
	}
	sha := hex.EncodeToString(hash.Sum(nil))
	name := archiveObjectName(day, sha)
	body := gz.Bytes()
	if err := store.Create(ctx, name, body); err != nil && !errors.Is(err, errObjectExists) {
		return 0, fmt.Errorf("upload %s: %w", name, err)
	}
	info, err := store.Stat(ctx, name)
	if err != nil {
		return 0, fmt.Errorf("stat %s: %w", name, err)
	}
	want := md5.Sum(body)
	if info.Size != int64(len(body)) || !bytes.Equal(info.MD5, want[:]) {
		return 0, fmt.Errorf("archive %s failed verification: size %d/%d md5 mismatch=%v", name, info.Size, len(body), !bytes.Equal(info.MD5, want[:]))
	}
	if _, err := pool.Exec(ctx, `INSERT INTO analytics.app_events_archive(archive_date,object_name,row_count,content_sha256,compressed_bytes,verified_at)
 VALUES($1,$2,$3,$4,$5,now())
 ON CONFLICT(archive_date) DO UPDATE SET object_name=EXCLUDED.object_name,row_count=EXCLUDED.row_count,content_sha256=EXCLUDED.content_sha256,compressed_bytes=EXCLUDED.compressed_bytes,verified_at=EXCLUDED.verified_at,deleted_at=NULL`,
		day.Format("2006-01-02"), name, rows, sha, len(body)); err != nil {
		return 0, err
	}
	logger.InfoContext(ctx, "app_events_day_exported", slog.String("object", name), slog.Int64("rows", rows), slog.Int("compressed_bytes", len(body)))
	return rows, nil
}

func exportPage(ctx context.Context, pool *pgxpool.Pool, day, end time.Time, lastAt *time.Time, lastID *string, limit int, emit func([]byte) error) (int, error) {
	rows, err := pool.Query(ctx, archivePageSQL, day, end, *lastAt, *lastID, limit)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var at time.Time
		var id, doc string
		if err := rows.Scan(&at, &id, &doc); err != nil {
			return n, err
		}
		if err := emit(append([]byte(doc), '\n')); err != nil {
			return n, err
		}
		*lastAt, *lastID = at, id
		n++
	}
	return n, rows.Err()
}
