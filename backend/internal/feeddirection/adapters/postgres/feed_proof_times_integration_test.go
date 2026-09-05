package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// The 17:30 feed-proof-times read, against the REAL schema -- which is the only place the two
// defects this query can have are visible:
//
//  1. GRAIN. feed_direction_issue_rows is one row per feed ITEM, so a pen fed Concentrate + Hay +
//     Milk has THREE rows for one pen-session. Without the collapse, that pen is listed three times
//     and its single video reads as three. The fixture below gives every pen-session three items
//     precisely so a lost GROUP BY cannot pass.
//  2. PEN JOIN. The sheet and the completion match on partition_key, the identical generated column
//     on both tables. A pen fed under "Part 3" while the sheet says "part 3" must still match; a
//     regression here files fed pens as missing, which is the report saying the opposite of the
//     truth.
//
// Gated by pgtest.SkipIfNoDocker + GOATOS_RUN_POSTGRES_TESTS, the same harness this package's other
// integration tests use.

const (
	fptFeedDay   = "2026-09-05"
	fptIssueID   = "fd100000-0000-4000-8000-0000000091a1"
	fptWeightRef = "fd100000-0000-4000-8000-0000000092a1"
	fptFeedRef   = "fd100000-0000-4000-8000-0000000092a2"
	fptWaterRef  = "fd100000-0000-4000-8000-0000000092a3"
)

func fptExec(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("seed: %v\nsql: %s", err, sql)
	}
}

// seedProofTimesSheet writes a live sheet with TWO pen-sessions on one shed:
// "Yashoda" session 1 (fed, all three captures) and session 2 (planned, nothing captured).
// Each pen-session carries THREE feed-item cells -- the fan-out the grain collapse must survive.
func seedProofTimesSheet(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	fptExec(t, ctx, pool, `
INSERT INTO feed_direction_issues (feed_direction_issue_id, tenant_id, park_id, feed_day, workflow,
  state, issued_at, generation_input_fingerprint, request_fingerprint, idempotency_key, generated_by,
  source_contract, source_contract_version)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::date, 'normal', 'issued', now(), 'fp', 'rfp',
  'idem-proof-times', 'test', 'feed.direction.sheet', '1')`,
		fptIssueID, fdiTenant, fdiPark, fptFeedDay)

	for _, session := range []int{1, 2} {
		for item, label := range []string{"Concentrate", "Hay", "Milk"} {
			fptExec(t, ctx, pool, `
INSERT INTO feed_direction_issue_rows (tenant_id, feed_direction_issue_id, park_id, park_label,
  shed_id, shed_label, partition_label, shed_tag, breed, ration_group, session_no, session_label,
  head_count, head_count_informational, workflow, feed_item_label, quantity_kg, session_total_kg,
  overdue_pending, row_seq, item_seq, amended)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'CBE', $4::uuid, 'Yashoda', NULL, 'Non-Pregnant', 'Beetal',
  'Beetal/Sirohi', $5, $6, 10, false, 'normal', $7, 1.0, 3.0, false, 0, $8, false)`,
				fdiTenant, fptIssueID, fdiPark, fdiShedA, session, sessionLabelFor(session), label, item)
		}
	}
}

func sessionLabelFor(session int) string {
	if session == 1 {
		return "Morning"
	}
	return "Evening"
}

func seedProofArtifact(t *testing.T, ctx context.Context, pool *pgxpool.Pool, proofID, state, uploadedAt string) {
	t.Helper()
	var uploaded any
	if uploadedAt != "" {
		uploaded = uploadedAt
	}
	fptExec(t, ctx, pool, `
INSERT INTO proof_artifacts (proof_id, tenant_id, storage_provider, object_key, mime_type,
  upload_state, scope_type, scope_id, subject_type, subject_id, proof_type, uploaded_at)
VALUES ($1::uuid, $2::uuid, 'gcs', 'proofs/' || $1, 'video/mp4', $3, 'shed', $4::uuid, 'shed',
  $4::uuid, 'video', $5::timestamptz)`,
		proofID, fdiTenant, state, fdiShedA, uploaded)
}

func TestFeedProofTimesReadsOneRowPerPenSessionAndTheUploadInstants(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)
	seedProofTimesSheet(t, ctx, pool)

	// Two captures landed; the water video is still uploading, which must read as MISSING -- an
	// artifact that has not finished landing is not something a verifier can watch.
	seedProofArtifact(t, ctx, pool, fptWeightRef, "completed", "2026-09-05T09:12:00+05:30")
	seedProofArtifact(t, ctx, pool, fptFeedRef, "completed", "2026-09-05T09:31:00+05:30")
	seedProofArtifact(t, ctx, pool, fptWaterRef, "uploading", "")

	fptExec(t, ctx, pool, `
INSERT INTO feed_distribution_completions (tenant_id, park_id, shed_id, partition_label, session_no,
  target_date, workflow, status, feed_weight_proof_ref, distribution_proof_ref, water_proof_ref,
  idempotency_key)
VALUES ($1::uuid, $2::uuid, $3::uuid, NULL, 1, $4::date, 'normal', 'pending_verification',
  $5, $6, $7, 'idem-fpt-1')`,
		fdiTenant, fdiPark, fdiShedA, fptFeedDay, fptWeightRef, fptFeedRef, fptWaterRef)

	feedDay, err := time.ParseInLocation("2006-01-02", fptFeedDay, biztime.DefaultLocation())
	if err != nil {
		t.Fatalf("parse feed day: %v", err)
	}
	reports, err := repo.FeedProofTimesByPark(ctx, fdiTenant, feedDay)
	if err != nil {
		t.Fatalf("FeedProofTimesByPark: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("reports = %d, want one park", len(reports))
	}
	report := reports[0]

	// THE GRAIN ASSERTION: three feed items per pen-session, two sessions -- six sheet rows, and
	// exactly TWO report rows. A missing collapse yields six.
	if len(report.Rows) != 2 {
		t.Fatalf("rows = %d, want 2 (one per pen-session, not one per feed item)", len(report.Rows))
	}

	bySession := map[int]int{}
	for i, row := range report.Rows {
		bySession[row.SessionNo] = i
		if got := row.PenDisplay(); got != "Yashoda" {
			t.Errorf("pen display = %q, want the undivided shed's own name", got)
		}
	}
	fed := report.Rows[bySession[1]]
	if got := fed.Weight.UploadedAt.In(biztime.DefaultLocation()).Format("15:04"); got != "09:12" {
		t.Errorf("weight upload time = %s, want 09:12", got)
	}
	if got := fed.Distribution.UploadedAt.In(biztime.DefaultLocation()).Format("15:04"); got != "09:31" {
		t.Errorf("distribution upload time = %s, want 09:31", got)
	}
	if fed.Water.Recorded() {
		t.Error("an upload still in flight must read as missing, not as done")
	}
	if fed.Status != "pending_verification" {
		t.Errorf("status = %q, want the completion's raw status", fed.Status)
	}

	// The session nobody submitted must still APPEAR, with nothing recorded. A report that goes
	// quiet exactly where work was skipped is the opposite of what 17:30 is for.
	unfed := report.Rows[bySession[2]]
	if unfed.Status != "" || unfed.Weight.Recorded() || unfed.Distribution.Recorded() || unfed.Water.Recorded() {
		t.Errorf("an unsubmitted pen-session must carry no captures, got %+v", unfed)
	}
	if report.MissingCount() != 2 {
		t.Errorf("missing = %d, want both pen-sessions short of a capture", report.MissingCount())
	}
}

// The sheet and the completion are matched on partition_key, which normalizes case and whitespace on
// both sides. A pen fed under a differently-cased label is the SAME pen and must not read as missing.
func TestFeedProofTimesMatchesThePenAcrossLabelCasing(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)

	fptExec(t, ctx, pool, `
INSERT INTO feed_direction_issues (feed_direction_issue_id, tenant_id, park_id, feed_day, workflow,
  state, issued_at, generation_input_fingerprint, request_fingerprint, idempotency_key, generated_by,
  source_contract, source_contract_version)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::date, 'normal', 'issued', now(), 'fp', 'rfp',
  'idem-proof-times-pen', 'test', 'feed.direction.sheet', '1')`,
		fptIssueID, fdiTenant, fdiPark, fptFeedDay)
	fptExec(t, ctx, pool, `
INSERT INTO feed_direction_issue_rows (tenant_id, feed_direction_issue_id, park_id, park_label,
  shed_id, shed_label, partition_label, shed_tag, breed, ration_group, session_no, session_label,
  head_count, head_count_informational, workflow, feed_item_label, quantity_kg, session_total_kg,
  overdue_pending, row_seq, item_seq, amended)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'CBE', $4::uuid, 'Godel 1', 'Part 3', 'Non-Pregnant', 'Beetal',
  'Beetal/Sirohi', 1, 'Morning', 10, false, 'normal', 'Concentrate', 1.0, 1.0, false, 0, 0, false)`,
		fdiTenant, fptIssueID, fdiPark, fdiShedA)

	seedProofArtifact(t, ctx, pool, fptWeightRef, "completed", "2026-09-05T09:05:00+05:30")
	seedProofArtifact(t, ctx, pool, fptFeedRef, "completed", "2026-09-05T09:20:00+05:30")
	seedProofArtifact(t, ctx, pool, fptWaterRef, "completed", "2026-09-05T09:35:00+05:30")

	// The completion stores the label differently cased -- the same pen.
	fptExec(t, ctx, pool, `
INSERT INTO feed_distribution_completions (tenant_id, park_id, shed_id, partition_label, session_no,
  target_date, workflow, status, feed_weight_proof_ref, distribution_proof_ref, water_proof_ref,
  idempotency_key)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'part 3', 1, $4::date, 'normal', 'completed', $5, $6, $7,
  'idem-fpt-pen')`,
		fdiTenant, fdiPark, fdiShedA, fptFeedDay, fptWeightRef, fptFeedRef, fptWaterRef)

	feedDay, _ := time.ParseInLocation("2006-01-02", fptFeedDay, biztime.DefaultLocation())
	reports, err := repo.FeedProofTimesByPark(ctx, fdiTenant, feedDay)
	if err != nil {
		t.Fatalf("FeedProofTimesByPark: %v", err)
	}
	if len(reports) != 1 || len(reports[0].Rows) != 1 {
		t.Fatalf("want one park with one pen-session, got %+v", reports)
	}
	row := reports[0].Rows[0]
	if got := row.PenDisplay(); got != "Godel 1 - Part 3" {
		t.Errorf("pen display = %q, want the sheet's own worded label", got)
	}
	if !row.Complete() {
		t.Errorf("a fed pen must not read as missing because its label was cased differently: %+v", row)
	}
	if reports[0].MissingCount() != 0 {
		t.Errorf("missing = %d, want 0", reports[0].MissingCount())
	}
}
