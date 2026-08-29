// Command import-feed-external-consumption bootstraps and appends the
// feed_external_consumption ledger (migration 000185) from the legacy
// "Feed DB" Google Sheet's Consumption rows for feeds GoatOS does not direct
// through feed sheets -- today exactly UHT Milk, the kids' milk feeding.
//
// Locked decisions (2026-08-22) this command implements:
//   - The sheet is the source of daily consumption for these feeds; GoatOS
//     directed kg never covers them, so stock analytics union this ledger with
//     locked-sheet directed kg for balance/avg-daily/days-left.
//   - CURRENT-CATALOG FEEDS ONLY, same as import-feed-purchases -- but
//     -ensure-catalog-item can first add the named feed to feed_item_catalog
//     (the maintainer-approved act that lets UHT Milk in; it supersedes, for
//     that feed only, the 2026-08-17 skip recorded in import-feed-purchases).
//   - Farm labels resolve to park locations by location_code (CBE/CPT); an
//     unknown farm keeps park_id NULL and the raw label.
//   - Re-runs are idempotent (upsert on the (farm, feed, day) natural key), so
//     the daily sheet routine can append by re-importing a recent window. The
//     CSV must carry ONE row per (farm, feed, day): the sheet records two rows
//     on a batch-changeover day, and those must be summed BEFORE export —
//     a second same-day row here REPLACES the first, it does not add.
//
// CSV columns (header row required):
//
//	date,farm,feed,quantity_kg,batch_no,source_ref
package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func main() {
	csvPath := flag.String("csv", "", "path to the exported Consumption-rows CSV")
	tenantID := flag.String("tenant", "", "tenant uuid")
	dryRun := flag.Bool("dry-run", false, "parse and report without writing")
	ensureCatalog := flag.String("ensure-catalog-item", "", "comma-separated feed labels to add to feed_item_catalog first (maintainer-approved catalog additions, e.g. \"UHT Milk\")")
	flag.Parse()
	if *csvPath == "" || *tenantID == "" {
		log.Fatal("usage: import-feed-external-consumption -csv <file> -tenant <uuid> [-ensure-catalog-item \"UHT Milk\"] [-dry-run] (DATABASE_URL env required)")
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	// Maintainer-approved catalog additions, before the catalog gate below.
	for _, label := range strings.Split(*ensureCatalog, ",") {
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		if *dryRun {
			log.Printf("dry-run: would ensure catalog item %q", label)
			continue
		}
		// Same shape as the feedconfig authoring insert: status active,
		// nutrition NULL (an honest "not measured"), next display_order.
		// scale-guard:ignore: bounded one-off CLI loop over the handful of -ensure-catalog-item labels, not a request path
		tag, err := conn.Exec(ctx, `
INSERT INTO feed_item_catalog (tenant_id, feed_item_label, display_order, status)
SELECT $1::uuid, $2, COALESCE(MAX(display_order), 0) + 1, 'active'
FROM feed_item_catalog WHERE tenant_id = $1::uuid
ON CONFLICT (tenant_id, feed_item_key) DO NOTHING`, *tenantID, label)
		if err != nil {
			log.Fatalf("ensure catalog %q: %v", label, err)
		}
		if tag.RowsAffected() == 1 {
			log.Printf("catalog item added: %q", label)
		} else {
			log.Printf("catalog item already present: %q", label)
		}
	}

	// Park resolution by location_code, tenant-scoped.
	parks := map[string]string{}
	rows, err := conn.Query(ctx, `SELECT upper(location_code), location_id::text FROM locations WHERE tenant_id=$1 AND location_type='park'`, *tenantID)
	if err != nil {
		log.Fatalf("parks: %v", err)
	}
	for rows.Next() {
		var code, id string
		if err := rows.Scan(&code, &id); err != nil {
			log.Fatalf("parks scan: %v", err)
		}
		parks[code] = id
	}
	rows.Close()

	catalog := map[string]bool{}
	rows, err = conn.Query(ctx, `SELECT feed_item_key FROM feed_item_catalog WHERE tenant_id=$1`, *tenantID)
	if err != nil {
		log.Fatalf("catalog: %v", err)
	}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			log.Fatalf("catalog scan: %v", err)
		}
		catalog[key] = true
	}
	rows.Close()
	norm := func(label string) string {
		var out string
		if err := conn.QueryRow(ctx, `SELECT feed_config_norm($1)`, label).Scan(&out); err != nil {
			log.Fatalf("norm: %v", err)
		}
		return out
	}

	f, err := os.Open(*csvPath)
	if err != nil {
		log.Fatalf("open: %v", err)
	}
	defer f.Close()
	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		log.Fatalf("header: %v", err)
	}
	col := map[string]int{}
	for i, name := range header {
		col[strings.TrimSpace(strings.ToLower(name))] = i
	}
	for _, name := range []string{"date", "farm", "feed", "quantity_kg"} {
		if _, ok := col[name]; !ok {
			log.Fatalf("csv missing required column %q", name)
		}
	}
	get := func(rec []string, name string) string {
		i, ok := col[name]
		if !ok || i >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[i])
	}

	imported, updated, skippedCatalog, skippedBad := 0, 0, 0, 0
	skippedFeeds := map[string]int{}
	upsertSQL := `
INSERT INTO feed_external_consumption
  (tenant_id, park_id, farm_label, feed_item_label, feed_day, quantity_kg, batch_no, source_ref)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT (tenant_id, farm_label, feed_item_key, feed_day) DO UPDATE SET
  quantity_kg=EXCLUDED.quantity_kg, batch_no=EXCLUDED.batch_no,
  source_ref=EXCLUDED.source_ref, imported_at=now()`
	type queuedRow struct {
		day  string
		farm string
		feed string
	}
	writeBatch := &pgx.Batch{}
	queued := []queuedRow{}
	for {
		rec, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("read: %v", err)
		}
		feed := get(rec, "feed")
		farm := strings.ToUpper(get(rec, "farm"))
		qtyRaw := get(rec, "quantity_kg")
		dateRaw := get(rec, "date")
		if feed == "" || farm == "" || qtyRaw == "" || dateRaw == "" {
			skippedBad++
			continue
		}
		qty, err := strconv.ParseFloat(strings.ReplaceAll(qtyRaw, ",", ""), 64)
		if err != nil || qty < 0 {
			skippedBad++
			continue
		}
		feedDay, err := time.Parse("2006-01-02", dateRaw)
		if err != nil {
			if feedDay, err = time.Parse("2-Jan-06", dateRaw); err != nil {
				skippedBad++
				continue
			}
		}
		if !catalog[norm(feed)] {
			skippedCatalog++
			skippedFeeds[feed]++
			continue
		}
		var parkID *string
		if id, ok := parks[farm]; ok {
			parkID = &id
		}
		var batchNo *int
		if raw := get(rec, "batch_no"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil {
				batchNo = &n
			}
		}
		sourceRef := get(rec, "source_ref")
		if sourceRef == "" {
			sourceRef = fmt.Sprintf("feed-db-sheet:%s/%s/%s", farm, feed, feedDay.Format("2006-01-02"))
		}
		if *dryRun {
			imported++
			continue
		}
		writeBatch.Queue(upsertSQL, *tenantID, parkID, farm, feed, feedDay.Format("2006-01-02"), qty, batchNo, sourceRef)
		queued = append(queued, queuedRow{day: feedDay.Format("2006-01-02"), farm: farm, feed: feed})
	}
	if writeBatch.Len() > 0 {
		results := conn.SendBatch(ctx, writeBatch)
		defer results.Close()
		for _, item := range queued {
			tag, err := results.Exec() // scale-guard:ignore: drains queued pgx.Batch results; statements were sent in one batch.
			if err != nil {
				log.Fatalf("upsert %s %s/%s: %v", item.day, item.farm, item.feed, err)
			}
			if tag.RowsAffected() == 1 {
				imported++
			} else {
				updated++
			}
		}
	}
	log.Printf("feed external consumption import: imported=%d updated=%d skipped_not_in_catalog=%d skipped_bad_rows=%d dry_run=%v", imported, updated, skippedCatalog, skippedBad, *dryRun)
	for feed, n := range skippedFeeds {
		log.Printf("  skipped (not in catalog): %-32s x%d", feed, n)
	}
}
