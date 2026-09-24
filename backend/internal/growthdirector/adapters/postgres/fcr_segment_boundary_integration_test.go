package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/growthdirector/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// The segment is a PAGE of feed days [earlier round, later round): start inclusive, end exclusive.
// Priced before the segment join (the 2026-09-24 burst fix), a feed row exactly ON a round date must
// still land in exactly one segment and carry the load bought that same day:
//
//   - a load bought ON the segment start (Jul 8, batch 2, ₹40) already prices Jul 8 -- same day
//     counts (purchase_date <= feed_day) -- and, being the latest load on or before every later
//     day, prices Jul 9..14 too; batch 1 (₹20, Jul 1) prices nothing. Had the same-day load been
//     missed, Jul 8 would read ₹20 and the total ₹2,600.
//   - a feed row ON the segment end (Jul 15, the later round's own day) belongs to the NEXT segment,
//     so it adds nothing here: feed stays 70 kg, not 80, and head-days stay 175.
//
// Lump cost: 70 kg x ₹40 = ₹2,800.
func TestFCRSegmentFeedAtSegmentPageBoundary(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedFCRFixture(t, ctx, pool)
	execGD(t, ctx, pool, `
INSERT INTO feed_purchases (tenant_id, park_id, farm_label, feed_item_label, batch_no, purchase_date, quantity_kg, total_cost, per_kg_cost, depletes_from)
VALUES ($1::uuid, $2::uuid, 'CBE', 'Maize Crush', 2, '2026-07-08', 1000, 40000, 40, '2026-07-08')`, gdTenant, gdPark)
	endIssue := fcrIssueBase + "71"
	execGD(t, ctx, pool, `
INSERT INTO feed_direction_issues (feed_direction_issue_id, tenant_id, park_id, feed_day, workflow, state, issued_at, generation_input_fingerprint, idempotency_key, request_fingerprint, source_contract, source_contract_version, generated_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, '2026-07-15', 'normal', 'issued', now(), 'fp:2026-07-15', 'idem:fcr:2026-07-15', 'fp:2026-07-15', 'growthdirector-test', '1', 'test')`,
		endIssue, gdTenant, gdPark)
	execGD(t, ctx, pool, `
INSERT INTO feed_direction_issue_rows (tenant_id, feed_direction_issue_id, park_id, park_label, shed_id, shed_label, partition_label, shed_tag, breed, session_no, head_count, head_count_informational, workflow, feed_item_label, quantity_kg, session_total_kg, overdue_pending, row_seq, item_seq)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'CBE', $4::uuid, 'Lump 1', NULL, '', '', 1, 25, false, 'normal', 'Maize Crush', 10.0, 10.0, false, 0, 1)`,
		gdTenant, endIssue, gdPark, gdShedLump)

	repo := NewRepository(pool, 30*time.Second)
	got, err := repo.GetFCR(ctx, gdTenant, []string{gdPark},
		time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC), time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC), "", "", "")
	if err != nil {
		t.Fatalf("GetFCR: %v", err)
	}
	var lump *domain.FCRPen
	for i := range got.Pens {
		if got.Pens[i].OperationalLocationDisplay == "Coimbatore · Lump 1" {
			lump = &got.Pens[i]
		}
	}
	if lump == nil {
		t.Fatalf("no lump pen row: %+v", got.Pens)
	}
	fcrNear(t, "segment feed excludes the end-day row", lump.FeedKg, 70)
	fcrNear(t, "start-day row priced at the same-day load", lump.FeedCostINR, 2800)
	fcrNear(t, "head-days exclude the end day", lump.HeadDays, 175)
}
