// Command import-feed-purchases bootstraps the feed_purchases ledger from the
// legacy "Feed DB" Google Sheet's Purchase rows, exported as CSV.
//
// Locked decisions (2026-08-17) this command implements:
//   - ONE-TIME BOOTSTRAP: the sheet is the source of history; ongoing entry
//     arrives later through the Procurement vertical. Re-runs are idempotent
//     (upsert on the (farm, feed, batch) natural key).
//   - CURRENT-CATALOG FEEDS ONLY: a sheet feed label that does not resolve in
//     feed_item_catalog (via feed_config_norm) is SKIPPED and counted, never
//     invented into the catalog.
//   - Farm labels resolve to park locations by location_code (CBE/CPT); an
//     unknown farm keeps park_id NULL and the raw label.
//
// CSV columns (header row required, matching the sheet's DB tab):
//
//	date,farm,feed,purchase_qty_kg,feed_cost,transport_cost,loading_cost,unloading_cost,total_cost,per_kg_cost,batch_no,vendor,payment_released,payment_status
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

// legacyFeedAliases maps the sheet's farm-legacy feed names onto the GoatOS
// catalog labels where both name the SAME physical feed. This is a rename map,
// never an invention: a sheet feed with no catalog identity (UHT Milk, Corn
// Silage, Green Guinea Grass, ...) still SKIPS per the current-catalog-only
// decision. Keys are feed_config_norm outputs of the sheet labels.
var legacyFeedAliases = map[string]string{
	"masoor_dhal_bhusa":            "Dry Masoor Bhusa",
	"green_hybrid":                 "Hybrid",
	"green_hedge_lucerne":          "Hedge Lucerne",
	"green_cofs":                   "COFS",
	"dry_maze_forage":              "Dry Maize",
	"mesha_kids_concentrate_sheep": "Mesha Kids Sheep Concentrate",
	"mesha_kids_concentrate_goat":  "Mesha Kids Goat Concentrate",
}

func consumedOrZero(v *float64) float64 {
	if v == nil || *v < 0 {
		return 0
	}
	return *v
}

func main() {
	csvPath := flag.String("csv", "", "path to the exported Purchase-rows CSV")
	tenantID := flag.String("tenant", "", "tenant uuid")
	dryRun := flag.Bool("dry-run", false, "parse and report without writing")
	depletesFrom := flag.String("depletes-from", "", "first feed day GoatOS directed kg depletes this ledger (YYYY-MM-DD); the sheet covers consumption before it")
	flag.Parse()
	if *csvPath == "" || *tenantID == "" || *depletesFrom == "" {
		log.Fatal("usage: import-feed-purchases -csv <file> -tenant <uuid> -depletes-from <YYYY-MM-DD> [-dry-run] (DATABASE_URL env required)")
	}
	if _, err := time.Parse("2006-01-02", *depletesFrom); err != nil {
		log.Fatalf("bad -depletes-from: %v", err)
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

	// Current catalog keys (any status: a retired-but-cataloged feed still
	// reports its historical expenses; only feeds the catalog never carried skip).
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
	var norm = func(label string) string {
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
	need := []string{"date", "farm", "feed", "purchase_qty_kg", "batch_no"}
	for _, name := range need {
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
	money := func(raw string) *float64 {
		raw = strings.NewReplacer("₹", "", ",", "", " ", "").Replace(raw)
		if raw == "" {
			return nil
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil
		}
		return &v
	}

	imported, updated, skippedCatalog, skippedBad := 0, 0, 0, 0
	skippedFeeds := map[string]int{}
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
		qty := money(get(rec, "purchase_qty_kg"))
		batchRaw := get(rec, "batch_no")
		dateRaw := get(rec, "date")
		if feed == "" || farm == "" || qty == nil || *qty <= 0 || batchRaw == "" || dateRaw == "" {
			skippedBad++
			continue
		}
		batch, err := strconv.Atoi(batchRaw)
		if err != nil {
			skippedBad++
			continue
		}
		// The sheet writes 24-Jan-25 style dates.
		purchaseDate, err := time.Parse("2-Jan-06", dateRaw)
		if err != nil {
			if purchaseDate, err = time.Parse("2006-01-02", dateRaw); err != nil {
				skippedBad++
				continue
			}
		}
		key := norm(feed)
		if alias, ok := legacyFeedAliases[key]; ok {
			feed = alias
			key = norm(alias)
		}
		if !catalog[key] {
			skippedCatalog++
			skippedFeeds[feed]++
			continue
		}
		var parkID *string
		if id, ok := parks[farm]; ok {
			parkID = &id
		}
		if *dryRun {
			imported++
			continue
		}
		tag, err := conn.Exec(ctx, `
INSERT INTO feed_purchases
  (tenant_id, park_id, farm_label, feed_item_label, batch_no, purchase_date, quantity_kg,
   consumed_at_import_kg, depletes_from,
   feed_cost, transport_cost, loading_cost, unloading_cost, total_cost, per_kg_cost,
   vendor, payment_released, payment_status, source_ref)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
ON CONFLICT (tenant_id, farm_label, feed_item_key, batch_no) DO UPDATE SET
  quantity_kg=EXCLUDED.quantity_kg, feed_cost=EXCLUDED.feed_cost,
  transport_cost=EXCLUDED.transport_cost, loading_cost=EXCLUDED.loading_cost,
  unloading_cost=EXCLUDED.unloading_cost, total_cost=EXCLUDED.total_cost,
  per_kg_cost=EXCLUDED.per_kg_cost, vendor=EXCLUDED.vendor,
  consumed_at_import_kg=EXCLUDED.consumed_at_import_kg, depletes_from=EXCLUDED.depletes_from,
  payment_released=EXCLUDED.payment_released, payment_status=EXCLUDED.payment_status,
  source_ref=EXCLUDED.source_ref, imported_at=now()`,
			*tenantID, parkID, farm, feed, batch, purchaseDate.Format("2006-01-02"), *qty,
			consumedOrZero(money(get(rec, "consumed_at_import_kg"))), *depletesFrom,
			money(get(rec, "feed_cost")), money(get(rec, "transport_cost")), money(get(rec, "loading_cost")),
			money(get(rec, "unloading_cost")), money(get(rec, "total_cost")), money(get(rec, "per_kg_cost")),
			get(rec, "vendor"), money(get(rec, "payment_released")), get(rec, "payment_status"),
			fmt.Sprintf("feed-db-sheet:batch=%d", batch))
		if err != nil {
			log.Fatalf("upsert batch %d %s/%s: %v", batch, farm, feed, err)
		}
		if tag.RowsAffected() == 1 {
			imported++
		} else {
			updated++
		}
	}
	log.Printf("feed purchases import: imported=%d updated=%d skipped_not_in_catalog=%d skipped_bad_rows=%d dry_run=%v", imported, updated, skippedCatalog, skippedBad, *dryRun)
	for feed, n := range skippedFeeds {
		log.Printf("  skipped (not in catalog): %-32s x%d", feed, n)
	}
}
