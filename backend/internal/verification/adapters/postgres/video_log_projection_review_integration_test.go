package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/verification/ports"
)

// Adversarial regression tests for the VIDEO LOG aggregates (Repository.VideoLogShedSummary and
// Repository.VideoLogShedRows), maintainer decision 2026-08-14.
//
// Each test attacks ONE claim in those queries' projection-review markers, and each is written so
// that removing the guarantee it names makes it fail:
//
//   - OneToMany    -- an item fans out to N media_refs BY DESIGN (that is the proof grain the
//     count wants), but proof_count must count PROOFS and item_count must count
//     ITEMS. A feed distribution item carrying three proofs must read as 3 videos
//     across 1 piece of work, never 3 pieces of work; and the shed's item_count
//     must not multiply by the proof fan-out.
//   - PageBoundary -- the detail read is bounded, and a shed holding more work than the limit must
//     report truncation rather than present a partial day as a whole one.
//   - StatusMatrix -- every non-withdrawn status must appear in the log (a rejected proof ARRIVED
//     just as much as a pending one), while a withdrawn item must inflate nothing.
//   - ParkScope    -- the authorization clamp is separate from the caller's own park filter; a
//     park-scoped caller must never see another park's arrivals.
const (
	videoLogTestTenantID = "20000000-0000-4000-8000-000000000001"
	videoLogTestParkA    = "20000000-0000-4000-8000-0000000000a1"
	videoLogTestParkB    = "20000000-0000-4000-8000-0000000000b1"
	videoLogTestShedA    = "20000000-0000-4000-8000-0000000000a2"
	videoLogTestShedB    = "20000000-0000-4000-8000-0000000000b2"
)

// videoLogDay is a fixed BUSINESS DATE, never an hour offset from now(). AGENTS.md is explicit that
// this time grain is the business day: an hour-anchored fixture passes or fails depending on the
// clock time it runs at, which is exactly the 15-test flake class recorded there.
const videoLogDay = "2026-08-14"

func TestVideoLogOneToManyProofFanOutDoesNotInflateItemCount(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedVideoLogTenant(t, ctx, pool)
	// ONE feed distribution item carrying THREE proofs: weight photo, distribution video, water
	// video -- the real shape of that category.
	item := seedVideoLogItem(t, ctx, pool, "feed", "feed_distribution", "pending", videoLogTestParkA, videoLogTestShedA, "2")
	seedVideoLogProof(t, ctx, pool, item, 1, "photo", "09:30")
	seedVideoLogProof(t, ctx, pool, item, 2, "video", "09:31")
	seedVideoLogProof(t, ctx, pool, item, 3, "video", "09:33")

	repo := NewRepository(pool, 10*time.Second)
	sheds, err := repo.VideoLogShedSummary(ctx, ports.VideoLogParams{TenantID: videoLogTestTenantID, BusinessDate: videoLogDay})
	if err != nil {
		t.Fatalf("VideoLogShedSummary: %v", err)
	}
	if len(sheds) != 1 {
		t.Fatalf("sheds = %d, want 1", len(sheds))
	}
	if sheds[0].ProofCount != 3 {
		t.Fatalf("proof_count = %d, want 3 — the fan-out to media_refs IS the proof grain", sheds[0].ProofCount)
	}
	if sheds[0].ItemCount != 1 {
		t.Fatalf("item_count = %d, want 1 — the proof fan-out must not multiply the work count", sheds[0].ItemCount)
	}
	// The two numbers must range over the SAME set, so item_count can never exceed proof_count.
	if sheds[0].ItemCount > sheds[0].ProofCount {
		t.Fatalf("item_count %d exceeds proof_count %d", sheds[0].ItemCount, sheds[0].ProofCount)
	}

	// The detail level must carry that same item ONCE, with its three proofs attached in the
	// producer's declared order and each keeping its ordinal -- the ordinal is what the service
	// resolves the registry label from, so losing it silently mislabels every proof.
	rows, truncated, err := repo.VideoLogShedRows(ctx, ports.VideoLogParams{
		TenantID: videoLogTestTenantID, BusinessDate: videoLogDay,
		ShedID: videoLogTestShedA + "#2", Limit: 50,
	})
	if err != nil {
		t.Fatalf("VideoLogShedRows: %v", err)
	}
	if truncated {
		t.Fatal("a single item must not report truncation at limit 50")
	}
	if len(rows) != 1 {
		t.Fatalf("detail rows = %d, want 1 item carrying three proofs (never three rows)", len(rows))
	}
	if len(rows[0].Proofs) != 3 {
		t.Fatalf("proofs on the item = %d, want 3", len(rows[0].Proofs))
	}
	for i, want := range []int{1, 2, 3} {
		if rows[0].Proofs[i].Ordinal != want {
			t.Fatalf("proof[%d].ordinal = %d, want %d — proofs must arrive in declared media_refs order", i, rows[0].Proofs[i].Ordinal, want)
		}
	}
	// The weight PHOTO leads the two videos; a screen that called all three "video" would lie.
	if rows[0].Proofs[0].MediaKind != "photo" {
		t.Fatalf("first proof media_kind = %q, want photo", rows[0].Proofs[0].MediaKind)
	}
	if rows[0].Proofs[0].UploadedAt == nil {
		t.Fatal("a completed proof must carry its upload time")
	}
}

// TestVideoLogOneToManyPartitionAliasesCollapseToOnePen is a real-data regression.
//
// One pen is written more than one way: the local tenant's Castro carries BOTH '1' and 'Part 1' on
// the same day, for the SAME pen. The summary grouped on the RAW label, so that pen produced two
// rows -- whose oploc.Key() is identical, because Key() normalizes. The browser caught it as
// duplicate React keys; the deeper defect is that a pen's day was split across two lines, so
// neither line's counts were the pen's real total.
//
// Grouping on the normalized key is the fix, and this pins it: two spellings, ONE row, counts
// summed across both.
func TestVideoLogOneToManyPartitionAliasesCollapseToOnePen(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedVideoLogTenant(t, ctx, pool)
	numeric := seedVideoLogItem(t, ctx, pool, "feed", "feed_packing", "pending", videoLogTestParkA, videoLogTestShedA, "1")
	seedVideoLogProof(t, ctx, pool, numeric, 1, "video", "06:12")
	prefixed := seedVideoLogItem(t, ctx, pool, "feed", "feed_packing", "pending", videoLogTestParkA, videoLogTestShedA, "Part 1")
	seedVideoLogProof(t, ctx, pool, prefixed, 1, "video", "18:40")

	repo := NewRepository(pool, 10*time.Second)
	sheds, err := repo.VideoLogShedSummary(ctx, ports.VideoLogParams{TenantID: videoLogTestTenantID, BusinessDate: videoLogDay})
	if err != nil {
		t.Fatalf("VideoLogShedSummary: %v", err)
	}
	if len(sheds) != 1 {
		labels := make([]string, 0, len(sheds))
		for _, s := range sheds {
			labels = append(labels, s.OperationalLocationDisplay)
		}
		t.Fatalf("sheds = %d %v, want 1 — '1' and 'Part 1' are the SAME pen and must not list twice", len(sheds), labels)
	}
	if sheds[0].ProofCount != 2 || sheds[0].ItemCount != 2 {
		t.Fatalf("proof_count/item_count = %d/%d, want 2/2 — both spellings' work belongs to the one pen",
			sheds[0].ProofCount, sheds[0].ItemCount)
	}
	// The bracket must span BOTH spellings' arrivals, or the pen's day reads as half a day.
	if sheds[0].FirstUploadAt == nil || sheds[0].LastUploadAt == nil {
		t.Fatal("both arrival bounds must be present")
	}
	if sheds[0].FirstUploadAt.Equal(*sheds[0].LastUploadAt) {
		t.Fatal("first and last arrival must span both spellings, not collapse to one row's time")
	}

	// And the detail read must find that pen by its normalized key -- if grouping and filtering
	// disagreed, the summary would offer a pen whose detail page came back empty.
	rows, _, err := repo.VideoLogShedRows(ctx, ports.VideoLogParams{
		TenantID: videoLogTestTenantID, BusinessDate: videoLogDay,
		ShedID: videoLogTestShedA + "#1", Limit: 50,
	})
	if err != nil {
		t.Fatalf("VideoLogShedRows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("detail rows = %d, want 2 — the detail filter must match BOTH spellings of the pen", len(rows))
	}
}

func TestVideoLogPageBoundaryReportsTruncationInsteadOfAPartialDay(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedVideoLogTenant(t, ctx, pool)
	for i := 0; i < 5; i++ {
		item := seedVideoLogItem(t, ctx, pool, "vaccination", "vaccination_proof", "pending", videoLogTestParkA, videoLogTestShedA, "")
		seedVideoLogProof(t, ctx, pool, item, 1, "video", "07:0"+string(rune('0'+i)))
	}

	repo := NewRepository(pool, 10*time.Second)
	params := ports.VideoLogParams{
		TenantID: videoLogTestTenantID, BusinessDate: videoLogDay,
		ShedID: videoLogTestShedA, Limit: 3,
	}
	rows, truncated, err := repo.VideoLogShedRows(ctx, params)
	if err != nil {
		t.Fatalf("VideoLogShedRows: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want the limit of 3", len(rows))
	}
	if !truncated {
		t.Fatal("a shed holding more work than the limit MUST report truncation — a silent cap presents a partial day as a whole one")
	}

	params.Limit = 10
	rows, truncated, err = repo.VideoLogShedRows(ctx, params)
	if err != nil {
		t.Fatalf("VideoLogShedRows: %v", err)
	}
	if len(rows) != 5 || truncated {
		t.Fatalf("rows = %d truncated = %v, want all 5 and no truncation", len(rows), truncated)
	}
}

func TestVideoLogStatusMatrixKeepsDecidedArrivalsAndDropsWithdrawn(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedVideoLogTenant(t, ctx, pool)
	// A rejected and an approved proof ARRIVED; the log is about arrival, not about open work.
	for _, status := range []string{"pending", "approved", "rejected"} {
		item := seedVideoLogItem(t, ctx, pool, "feed", "feed_packing", status, videoLogTestParkA, videoLogTestShedA, "1")
		seedVideoLogProof(t, ctx, pool, item, 1, "video", "06:12")
	}
	// A withdrawn item's source work was superseded; it never really stood.
	withdrawn := seedVideoLogItem(t, ctx, pool, "feed", "feed_packing", "withdrawn", videoLogTestParkA, videoLogTestShedA, "1")
	seedVideoLogProof(t, ctx, pool, withdrawn, 1, "video", "06:20")

	repo := NewRepository(pool, 10*time.Second)
	sheds, err := repo.VideoLogShedSummary(ctx, ports.VideoLogParams{TenantID: videoLogTestTenantID, BusinessDate: videoLogDay})
	if err != nil {
		t.Fatalf("VideoLogShedSummary: %v", err)
	}
	if len(sheds) != 1 {
		t.Fatalf("sheds = %d, want 1", len(sheds))
	}
	if sheds[0].ProofCount != 3 {
		t.Fatalf("proof_count = %d, want 3 (pending + approved + rejected); a withdrawn item must inflate nothing", sheds[0].ProofCount)
	}
}

// TestVideoLogAllShedsExportSpansEveryShedAndNamesItsPark pins the whole-day CSV read.
//
// Two things must hold that do not matter at the single-shed level: rows come back for EVERY shed
// (not just one), and each row names its own park. Shed names repeat across parks, so a file
// carrying only the shed display renders two different sheds identically — the OL-1 collision the
// operational-location convention exists to prevent. The fixture uses the same shed NAME in two
// parks precisely so a regression here cannot pass.
func TestVideoLogAllShedsExportSpansEveryShedAndNamesItsPark(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedVideoLogTenant(t, ctx, pool)
	// Same shed NAME in both parks: seedVideoLogTenant names shed A "Castro"; rename B to match.
	if _, err := pool.Exec(ctx, `UPDATE locations SET name = 'Castro' WHERE location_id = $1::uuid`, videoLogTestShedB); err != nil {
		t.Fatalf("rename shed B: %v", err)
	}
	a := seedVideoLogItem(t, ctx, pool, "feed", "feed_packing", "pending", videoLogTestParkA, videoLogTestShedA, "1")
	seedVideoLogProof(t, ctx, pool, a, 1, "video", "06:12")
	bItem := seedVideoLogItem(t, ctx, pool, "feed", "feed_packing", "pending", videoLogTestParkB, videoLogTestShedB, "1")
	seedVideoLogProof(t, ctx, pool, bItem, 1, "video", "07:30")

	repo := NewRepository(pool, 10*time.Second)
	rows, truncated, err := repo.VideoLogShedRows(ctx, ports.VideoLogParams{
		TenantID: videoLogTestTenantID, BusinessDate: videoLogDay,
		// No ShedID at all: the export asks for the day, not for a shed.
		AllSheds: true, Limit: 500,
	})
	if err != nil {
		t.Fatalf("VideoLogShedRows(AllSheds): %v", err)
	}
	if truncated {
		t.Fatal("two rows must not report truncation at limit 500")
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 — the export must span EVERY shed, not just one", len(rows))
	}
	parks := map[string]string{}
	for _, row := range rows {
		if row.ShedLabel == "" {
			t.Fatal("every export row must carry its own shed label")
		}
		if row.ParkLabel == "" {
			t.Fatalf("every export row must name its park; shed %q did not", row.ShedLabel)
		}
		parks[row.ParkLabel] = row.ShedLabel
	}
	if len(parks) != 2 {
		t.Fatalf("parks on the export rows = %#v, want both parks — without the park these two identically-named sheds are indistinguishable", parks)
	}
}

func TestVideoLogParkScopeClampExcludesAnotherParksArrivals(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedVideoLogTenant(t, ctx, pool)
	a := seedVideoLogItem(t, ctx, pool, "feed", "feed_transport", "pending", videoLogTestParkA, videoLogTestShedA, "")
	seedVideoLogProof(t, ctx, pool, a, 1, "video", "15:04")
	b := seedVideoLogItem(t, ctx, pool, "feed", "feed_transport", "pending", videoLogTestParkB, videoLogTestShedB, "")
	seedVideoLogProof(t, ctx, pool, b, 1, "video", "15:09")

	repo := NewRepository(pool, 10*time.Second)

	// Unclamped: both parks.
	sheds, err := repo.VideoLogShedSummary(ctx, ports.VideoLogParams{TenantID: videoLogTestTenantID, BusinessDate: videoLogDay})
	if err != nil {
		t.Fatalf("VideoLogShedSummary: %v", err)
	}
	if len(sheds) != 2 {
		t.Fatalf("unclamped sheds = %d, want 2", len(sheds))
	}

	// Clamped to park A: park B must be invisible, not merely unfiltered.
	sheds, err = repo.VideoLogShedSummary(ctx, ports.VideoLogParams{
		TenantID: videoLogTestTenantID, BusinessDate: videoLogDay,
		ScopeRestricted: true, ParkIDs: []string{videoLogTestParkA},
	})
	if err != nil {
		t.Fatalf("VideoLogShedSummary: %v", err)
	}
	if len(sheds) != 1 {
		t.Fatalf("clamped sheds = %d, want 1 — the authorization clamp must exclude another park", len(sheds))
	}
	if sheds[0].ParkID != videoLogTestParkA {
		t.Fatalf("clamped shed park = %s, want park A", sheds[0].ParkID)
	}
}

func seedVideoLogTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'video-log-test', 'active')
		ON CONFLICT (tenant_id) DO NOTHING`, videoLogTestTenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	for _, loc := range []struct{ id, name, kind, parent string }{
		{videoLogTestParkA, "Park A", "park", ""},
		{videoLogTestParkB, "Park B", "park", ""},
		{videoLogTestShedA, "Castro", "shed", videoLogTestParkA},
		{videoLogTestShedB, "Godel 1", "shed", videoLogTestParkB},
	} {
		var parent any
		if loc.parent != "" {
			parent = loc.parent
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO locations (location_id, tenant_id, name, location_type, parent_location_id, status)
			VALUES ($1::uuid, $2::uuid, $3, $4, $5::uuid, 'active')
			ON CONFLICT (location_id) DO NOTHING`,
			loc.id, videoLogTestTenantID, loc.name, loc.kind, parent); err != nil {
			t.Fatalf("seed location %s: %v", loc.name, err)
		}
	}
}

// seedVideoLogItem writes one verification_items row anchored to the FIXED business day and returns
// its item_id.
func seedVideoLogItem(t *testing.T, ctx context.Context, pool *pgxpool.Pool, module, category, status, parkID, shedID, partition string) string {
	t.Helper()
	var itemID string
	var reason any
	if status == "rejected" {
		reason = "seeded rejection reason"
	}
	var partitionLabel any
	if partition != "" {
		partitionLabel = partition
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO verification_items (
			tenant_id, vertical, module, category, source_module,
			source_ref_type, source_ref_id, status, verdict_reason,
			park_id, shed_id, partition_label, media_refs,
			captured_at, idempotency_key)
		VALUES ($1::uuid, 'preventive_care', $2, $3, $2,
			$3, gen_random_uuid(), $4, $5,
			$6::uuid, $7::uuid, $8, '[]'::jsonb,
			($9::date::timestamp AT TIME ZONE 'Asia/Kolkata') + interval '8 hours',
			'video-log-test:' || gen_random_uuid()::text)
		RETURNING item_id::text`,
		videoLogTestTenantID, module, category, status, reason,
		parkID, shedID, partitionLabel, videoLogDay).Scan(&itemID); err != nil {
		t.Fatalf("seed item (%s/%s): %v", module, status, err)
	}
	return itemID
}

// seedVideoLogProof inserts a completed proof artifact and APPENDS its id to the item's media_refs,
// mirroring how a producer builds the array. hhmm is the IST wall-clock arrival time on the fixed
// business day.
func seedVideoLogProof(t *testing.T, ctx context.Context, pool *pgxpool.Pool, itemID string, ordinal int, proofType, hhmm string) {
	t.Helper()
	var proofID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO proof_artifacts (
			tenant_id, storage_provider, object_key, mime_type, upload_state,
			scope_type, scope_id, subject_type, proof_type, metadata, uploaded_at)
		VALUES ($1::uuid, 'local', 'video-log-test/' || gen_random_uuid()::text, $2, 'completed',
			'shed', $3::uuid, 'shed', $4, '{}'::jsonb,
			($5::date::timestamp AT TIME ZONE 'Asia/Kolkata') + $6::interval)
		RETURNING proof_id::text`,
		videoLogTestTenantID, "video/mp4", videoLogTestShedA, proofType, videoLogDay, hhmm).Scan(&proofID); err != nil {
		t.Fatalf("seed proof %d: %v", ordinal, err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE verification_items
		SET media_refs = media_refs || to_jsonb($3::text)
		WHERE tenant_id = $1::uuid AND item_id = $2::uuid`,
		videoLogTestTenantID, itemID, proofID); err != nil {
		t.Fatalf("append media_ref: %v", err)
	}
}
