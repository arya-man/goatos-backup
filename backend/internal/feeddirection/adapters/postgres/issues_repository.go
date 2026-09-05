package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
)

// This file adds the WRITE + lifecycle persistence for issued sheets and the feed_schedule_config
// clock read. It is separate from repository.go (the read-only config/scope reader) but shares the
// same *Repository, which now owns the two new frozen-sheet tables in addition to reading the config.
//
// Every write here is ONE transaction and idempotent. The transaction orchestration lives in this
// adapter because atomicity is a persistence concern; the change-detection and diff logic it drives
// are the pure domain functions (FingerprintRows / DiffCells), so a fake store can be tested without
// re-implementing them.

var (
	_ ports.IssueStore     = (*Repository)(nil)
	_ ports.ScheduleReader = (*Repository)(nil)
)

// ---------------------------------------------------------------------------
// PersistIssue
// ---------------------------------------------------------------------------

// PersistIssue issues, exactly-replays, or re-issues one workflow's sheet atomically.
//
// The natural-key row is locked FOR UPDATE first so concurrent issues of the same day serialize. An
// exact re-issue (same fingerprint) writes nothing and returns the original -- the idempotency
// replay. A re-issue with a DIFFERENT fingerprint replaces the frozen rows in place ONLY while the
// sheet is still 'issued'; once amended or locked it is refused (ErrReissueAfterAmendOrLock), so a
// correction or the transport lock is never silently overwritten.
func (r *Repository) PersistIssue(ctx context.Context, cmd ports.PersistIssueCommand) (ports.IssueResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ports.IssueResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	header, found, err := lockIssue(ctx, tx, cmd.TenantID, cmd.ParkID, cmd.FeedDay, cmd.Workflow)
	if err != nil {
		return ports.IssueResult{}, err
	}

	if !found {
		inserted, insErr := insertIssueHeader(ctx, tx, cmd)
		if insErr != nil {
			var pgErr *pgconn.PgError
			// A concurrent inserter won the race on the live/idempotency unique index. Re-read and fall
			// through to the found branch rather than issuing twice.
			if errors.As(insErr, &pgErr) && pgErr.Code == "23505" {
				header, found, err = lockIssue(ctx, tx, cmd.TenantID, cmd.ParkID, cmd.FeedDay, cmd.Workflow)
				if err != nil {
					return ports.IssueResult{}, err
				}
			} else {
				return ports.IssueResult{}, fmt.Errorf("feeddirection: insert issue: %w", insErr)
			}
		} else {
			if err := insertIssueRows(ctx, tx, cmd.TenantID, inserted.IssueID, cmd.ParkID, cmd.Cells, false, nil); err != nil {
				return ports.IssueResult{}, err
			}
			if err := r.commitAndInvalidateReadCache(ctx, tx); err != nil {
				return ports.IssueResult{}, err
			}
			return ports.IssueResult{Header: inserted, Outcome: ports.IssueOutcomeInserted}, nil
		}
	}

	// Found (either pre-existing or lost the insert race).
	if header.GenerationInputFingerprint == cmd.Fingerprint {
		// Exact replay: no side effects, return the original.
		if err := r.commitAndInvalidateReadCache(ctx, tx); err != nil {
			return ports.IssueResult{}, err
		}
		return ports.IssueResult{Header: header, Outcome: ports.IssueOutcomeReplayed}, nil
	}
	if header.State != domain.IssueStateIssued {
		return ports.IssueResult{}, ports.ErrReissueAfterAmendOrLock
	}

	// Re-issue in place: replace the frozen rows and refresh the fingerprint/issued_at.
	if _, err := tx.Exec(ctx, `DELETE FROM feed_direction_issue_rows WHERE tenant_id = $1::uuid AND feed_direction_issue_id = $2::uuid`,
		cmd.TenantID, header.IssueID); err != nil {
		return ports.IssueResult{}, fmt.Errorf("feeddirection: clear issue rows for re-issue: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE feed_direction_issues
SET generation_input_fingerprint = $3,
    request_fingerprint = $3,
    issued_at = $4,
    generated_by = $5,
    updated_at = now()
WHERE tenant_id = $1::uuid AND feed_direction_issue_id = $2::uuid`,
		cmd.TenantID, header.IssueID, cmd.Fingerprint, cmd.IssuedAt.UTC(), cmd.GeneratedBy); err != nil {
		return ports.IssueResult{}, fmt.Errorf("feeddirection: update issue for re-issue: %w", err)
	}
	if err := insertIssueRows(ctx, tx, cmd.TenantID, header.IssueID, cmd.ParkID, cmd.Cells, false, nil); err != nil {
		return ports.IssueResult{}, err
	}
	if err := r.commitAndInvalidateReadCache(ctx, tx); err != nil {
		return ports.IssueResult{}, err
	}
	header.GenerationInputFingerprint = cmd.Fingerprint
	header.IssuedAt = cmd.IssuedAt
	return ports.IssueResult{Header: header, Outcome: ports.IssueOutcomeReissued}, nil
}

func insertIssueHeader(ctx context.Context, tx pgx.Tx, cmd ports.PersistIssueCommand) (domain.IssueHeader, error) {
	var id string
	err := tx.QueryRow(ctx, `
INSERT INTO feed_direction_issues (
  tenant_id, park_id, feed_day, workflow, state, issued_at,
  generation_input_fingerprint, idempotency_key, request_fingerprint,
  source_contract, source_contract_version, amendment_count, generated_by
) VALUES (
  $1::uuid, $2::uuid, $3::date, $4, 'issued', $5,
  $6, $7, $6,
  $8, $9, 0, $10
)
RETURNING feed_direction_issue_id::text`,
		cmd.TenantID, cmd.ParkID, cmd.FeedDay, cmd.Workflow, cmd.IssuedAt.UTC(),
		cmd.Fingerprint, cmd.IdempotencyKey,
		domain.SourceContract, domain.SourceContractVersion, cmd.GeneratedBy).Scan(&id)
	if err != nil {
		return domain.IssueHeader{}, err
	}
	return domain.IssueHeader{
		IssueID: id, TenantID: cmd.TenantID, ParkID: cmd.ParkID, FeedDay: cmd.FeedDay,
		Workflow: cmd.Workflow, State: domain.IssueStateIssued, IssuedAt: cmd.IssuedAt,
		GenerationInputFingerprint: cmd.Fingerprint, AmendmentCount: 0,
	}, nil
}

// ---------------------------------------------------------------------------
// AmendIssue
// ---------------------------------------------------------------------------

// AmendIssue diffs a fresh recompute against the stored sheet and writes ONLY the changed cells.
//
// It loads the stored cells under the header lock, runs domain.DiffCells, upserts the changed cells
// marked amended, deletes the removed ones, and flips the header to 'amended'. If nothing changed it
// is a no-op that still records the run (amended_at is stamped, amendment_count is not). A locked
// sheet is refused.
func (r *Repository) AmendIssue(ctx context.Context, cmd ports.AmendIssueCommand) (ports.AmendResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ports.AmendResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	header, found, err := lockIssue(ctx, tx, cmd.TenantID, cmd.ParkID, cmd.FeedDay, cmd.Workflow)
	if err != nil {
		return ports.AmendResult{}, err
	}
	if !found {
		return ports.AmendResult{}, ports.ErrIssueNotFound
	}
	if header.State == domain.IssueStateLocked {
		return ports.AmendResult{}, ports.ErrAmendAfterLock
	}

	// Idempotent no-op: an identical recompute changes nothing. Record that the correction ran (stamp
	// amended_at) but do not bump the count or touch any row.
	if header.GenerationInputFingerprint == cmd.Fingerprint {
		if _, err := tx.Exec(ctx, `
UPDATE feed_direction_issues SET amended_at = $3, state = CASE WHEN state = 'issued' THEN 'amended' ELSE state END, updated_at = now()
WHERE tenant_id = $1::uuid AND feed_direction_issue_id = $2::uuid`,
			cmd.TenantID, header.IssueID, cmd.AmendedAt.UTC()); err != nil {
			return ports.AmendResult{}, fmt.Errorf("feeddirection: record no-op amend: %w", err)
		}
		if err := r.commitAndInvalidateReadCache(ctx, tx); err != nil {
			return ports.AmendResult{}, err
		}
		return ports.AmendResult{Header: header, Outcome: ports.AmendOutcomeUnchanged}, nil
	}

	stored, err := loadIssueCells(ctx, tx, cmd.TenantID, header.IssueID)
	if err != nil {
		return ports.AmendResult{}, err
	}
	diff := domain.DiffCells(stored, cmd.Cells)

	if len(diff.Changed) > 0 {
		if err := insertIssueRows(ctx, tx, cmd.TenantID, header.IssueID, cmd.ParkID, diff.Changed, true, &cmd.AmendedAt); err != nil {
			return ports.AmendResult{}, err
		}
	}
	if len(diff.RemovedKeys) > 0 {
		if err := deleteIssueCells(ctx, tx, cmd.TenantID, header.IssueID, diff.RemovedKeys); err != nil {
			return ports.AmendResult{}, err
		}
	}

	if _, err := tx.Exec(ctx, `
UPDATE feed_direction_issues
SET state = 'amended',
    amended_at = $3,
    amendment_count = amendment_count + 1,
    generation_input_fingerprint = $4,
    request_fingerprint = $4,
    updated_at = now()
WHERE tenant_id = $1::uuid AND feed_direction_issue_id = $2::uuid`,
		cmd.TenantID, header.IssueID, cmd.AmendedAt.UTC(), cmd.Fingerprint); err != nil {
		return ports.AmendResult{}, fmt.Errorf("feeddirection: update issue for amend: %w", err)
	}
	if err := r.commitAndInvalidateReadCache(ctx, tx); err != nil {
		return ports.AmendResult{}, err
	}
	header.State = domain.IssueStateAmended
	header.AmendedAt = &cmd.AmendedAt
	header.AmendmentCount++
	header.GenerationInputFingerprint = cmd.Fingerprint
	return ports.AmendResult{
		Header:               header,
		Outcome:              ports.AmendOutcomeAmended,
		AffectedShedIDs:      diff.AffectedShedIDs,
		HeadCountChangedPens: diff.HeadCountChangedPens,
	}, nil
}

// ---------------------------------------------------------------------------
// LockIssue
// ---------------------------------------------------------------------------

// LockIssue transitions a sheet to 'locked'. Idempotent: a second lock is a no-op.
func (r *Repository) LockIssue(ctx context.Context, cmd ports.LockIssueCommand) (ports.LockResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ports.LockResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	header, found, err := lockIssue(ctx, tx, cmd.TenantID, cmd.ParkID, cmd.FeedDay, cmd.Workflow)
	if err != nil {
		return ports.LockResult{}, err
	}
	if !found {
		return ports.LockResult{}, ports.ErrIssueNotFound
	}
	if header.State == domain.IssueStateLocked {
		if err := r.commitAndInvalidateReadCache(ctx, tx); err != nil {
			return ports.LockResult{}, err
		}
		return ports.LockResult{Header: header, Outcome: ports.LockOutcomeAlreadyDone}, nil
	}
	if _, err := tx.Exec(ctx, `
UPDATE feed_direction_issues SET state = 'locked', locked_at = $3, updated_at = now()
WHERE tenant_id = $1::uuid AND feed_direction_issue_id = $2::uuid`,
		cmd.TenantID, header.IssueID, cmd.LockedAt.UTC()); err != nil {
		return ports.LockResult{}, fmt.Errorf("feeddirection: lock issue: %w", err)
	}
	if err := r.commitAndInvalidateReadCache(ctx, tx); err != nil {
		return ports.LockResult{}, err
	}
	header.State = domain.IssueStateLocked
	header.LockedAt = &cmd.LockedAt
	return ports.LockResult{Header: header, Outcome: ports.LockOutcomeLocked}, nil
}

// ---------------------------------------------------------------------------
// Reads used by the serve path and the transactions
// ---------------------------------------------------------------------------

const issueHeaderColumns = `feed_direction_issue_id::text, tenant_id::text, park_id::text, feed_day::text,
       workflow, state, issued_at, amended_at, locked_at, generation_input_fingerprint, amendment_count`

func scanIssueHeader(row pgx.Row) (domain.IssueHeader, error) {
	var h domain.IssueHeader
	var amendedAt, lockedAt *time.Time
	if err := row.Scan(&h.IssueID, &h.TenantID, &h.ParkID, &h.FeedDay, &h.Workflow, &h.State,
		&h.IssuedAt, &amendedAt, &lockedAt, &h.GenerationInputFingerprint, &h.AmendmentCount); err != nil {
		return domain.IssueHeader{}, err
	}
	h.AmendedAt = amendedAt
	h.LockedAt = lockedAt
	return h, nil
}

// lockIssue reads and FOR UPDATE-locks the live issue for a (park, feed_day, workflow).
func lockIssue(ctx context.Context, tx pgx.Tx, tenantID, parkID, feedDay, workflow string) (domain.IssueHeader, bool, error) {
	row := tx.QueryRow(ctx, `
SELECT `+issueHeaderColumns+`
FROM feed_direction_issues
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND feed_day = $3::date AND workflow = $4
  AND state IN ('issued', 'amended', 'locked')
FOR UPDATE`, tenantID, parkID, feedDay, workflow)
	header, err := scanIssueHeader(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.IssueHeader{}, false, nil
	}
	if err != nil {
		return domain.IssueHeader{}, false, fmt.Errorf("feeddirection: lock issue: %w", err)
	}
	return header, true, nil
}

// LoadIssueHeaders returns the live issue headers for a park + feed day, optionally one workflow.
func (r *Repository) LoadIssueHeaders(ctx context.Context, tenantID, parkID, feedDay, workflow string) ([]domain.IssueHeader, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, `
SELECT `+issueHeaderColumns+`
FROM feed_direction_issues
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND feed_day = $3::date
  AND state IN ('issued', 'amended', 'locked')
  AND ($4 = '' OR workflow = $4)
ORDER BY workflow`, tenantID, parkID, feedDay, workflow)
	if err != nil {
		return nil, fmt.Errorf("feeddirection: load issue headers: %w", err)
	}
	defer rows.Close()
	out := make([]domain.IssueHeader, 0, 2)
	for rows.Next() {
		header, err := scanIssueHeader(rows)
		if err != nil {
			return nil, fmt.Errorf("feeddirection: scan issue header: %w", err)
		}
		out = append(out, header)
	}
	return out, rows.Err()
}

const issueCellColumns = `park_id::text, park_label, shed_id::text, shed_label,
       coalesce(partition_label, '') AS partition_label, shed_tag, breed,
       ration_group, experiment_arm, session_no, session_label, head_count, head_count_informational,
       workflow, feed_item_label, feed_item_key, quantity_kg::text, grams_per_head::text,
       shed_factor::text, blocked_reason_code, blocked_reason_detail, session_total_kg::text,
       overdue_pending, row_seq, item_seq, amended`

func scanIssueCell(rows pgx.Rows) (domain.StoredCell, error) {
	var c domain.StoredCell
	if err := rows.Scan(&c.ParkID, &c.ParkLabel, &c.ShedID, &c.ShedLabel, &c.PartitionLabel, &c.ShedTag, &c.Breed,
		&c.RationGroup, &c.ExperimentArm, &c.SessionNo, &c.SessionLabel, &c.HeadCount, &c.HeadCountInformational,
		&c.Workflow, &c.FeedItemLabel, &c.FeedItemKey, &c.QuantityKg, &c.GramsPerHead,
		&c.ShedFactor, &c.BlockedReasonCode, &c.BlockedReasonDetail, &c.SessionTotalKg,
		&c.OverduePending, &c.RowSeq, &c.ItemSeq, &c.Amended); err != nil {
		return domain.StoredCell{}, err
	}
	return c, nil
}

func loadIssueCells(ctx context.Context, tx pgx.Tx, tenantID, issueID string) ([]domain.StoredCell, error) {
	rows, err := tx.Query(ctx, `
SELECT `+issueCellColumns+`
FROM feed_direction_issue_rows
WHERE tenant_id = $1::uuid AND feed_direction_issue_id = $2::uuid
ORDER BY row_seq, item_seq`, tenantID, issueID)
	if err != nil {
		return nil, fmt.Errorf("feeddirection: load issue cells: %w", err)
	}
	defer rows.Close()
	out := make([]domain.StoredCell, 0)
	for rows.Next() {
		cell, err := scanIssueCell(rows)
		if err != nil {
			return nil, fmt.Errorf("feeddirection: scan issue cell: %w", err)
		}
		out = append(out, cell)
	}
	return out, rows.Err()
}

// LoadIssueRows returns the stored cells for a set of issues in ONE set-based read, keyed by issue
// id. It uses `= ANY($2::uuid[])` -- one round trip, not one per issue -- so serving a park-day's two
// workflows is a single query.
func (r *Repository) LoadIssueRows(ctx context.Context, tenantID string, issueIDs []string) (map[string][]domain.StoredCell, error) {
	out := map[string][]domain.StoredCell{}
	if len(issueIDs) == 0 {
		return out, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, `
SELECT feed_direction_issue_id::text, `+issueCellColumns+`
FROM feed_direction_issue_rows
WHERE tenant_id = $1::uuid AND feed_direction_issue_id = ANY($2::uuid[])
ORDER BY feed_direction_issue_id, row_seq, item_seq`, tenantID, issueIDs)
	if err != nil {
		return nil, fmt.Errorf("feeddirection: load issue rows: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var issueID string
		var c domain.StoredCell
		if err := rows.Scan(&issueID, &c.ParkID, &c.ParkLabel, &c.ShedID, &c.ShedLabel, &c.PartitionLabel, &c.ShedTag, &c.Breed,
			&c.RationGroup, &c.ExperimentArm, &c.SessionNo, &c.SessionLabel, &c.HeadCount, &c.HeadCountInformational,
			&c.Workflow, &c.FeedItemLabel, &c.FeedItemKey, &c.QuantityKg, &c.GramsPerHead,
			&c.ShedFactor, &c.BlockedReasonCode, &c.BlockedReasonDetail, &c.SessionTotalKg,
			&c.OverduePending, &c.RowSeq, &c.ItemSeq, &c.Amended); err != nil {
			return nil, fmt.Errorf("feeddirection: scan issue row: %w", err)
		}
		out[issueID] = append(out[issueID], c)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Row writes (set-based, no N+1)
// ---------------------------------------------------------------------------

// insertIssueRows writes a batch of cells in ONE set-based statement via UNNEST -- never a per-cell
// INSERT in a loop, which would be the N+1 the scale rules ban. When upsert is true it is an amend:
// a changed cell replaces its stored value and is marked amended. The nullable numeric columns are
// passed as text arrays and cast, so a NULL quantity (a blocked cell) round-trips as NULL, never 0.
func insertIssueRows(ctx context.Context, tx pgx.Tx, tenantID, issueID, parkID string, cells []domain.StoredCell, amended bool, amendedAt *time.Time) error {
	if len(cells) == 0 {
		return nil
	}
	if err := validateStoredRowSeqs(cells); err != nil {
		return err
	}
	n := len(cells)
	parkLabel := make([]string, n)
	shedID := make([]string, n)
	shedLabel := make([]string, n)
	partitionLabel := make([]string, n)
	shedTag := make([]string, n)
	breed := make([]string, n)
	rationGroup := make([]string, n)
	experimentArm := make([]string, n)
	sessionNo := make([]int32, n)
	sessionLabel := make([]string, n)
	headCount := make([]int64, n)
	hcInfo := make([]bool, n)
	workflow := make([]string, n)
	feedItemLabel := make([]string, n)
	quantity := make([]*string, n)
	grams := make([]*string, n)
	factor := make([]*string, n)
	blockedCode := make([]*string, n)
	blockedDetail := make([]*string, n)
	sessionTotal := make([]string, n)
	overdue := make([]bool, n)
	rowSeq := make([]int32, n)
	itemSeq := make([]int32, n)
	for i, c := range cells {
		parkLabel[i] = c.ParkLabel
		shedID[i] = c.ShedID
		shedLabel[i] = c.ShedLabel
		partitionLabel[i] = c.PartitionLabel
		shedTag[i] = c.ShedTag
		breed[i] = c.Breed
		rationGroup[i] = c.RationGroup
		experimentArm[i] = c.ExperimentArm
		sessionNo[i] = c.SessionNo
		sessionLabel[i] = c.SessionLabel
		headCount[i] = c.HeadCount
		hcInfo[i] = c.HeadCountInformational
		workflow[i] = c.Workflow
		feedItemLabel[i] = c.FeedItemLabel
		quantity[i] = c.QuantityKg
		grams[i] = c.GramsPerHead
		factor[i] = c.ShedFactor
		blockedCode[i] = c.BlockedReasonCode
		blockedDetail[i] = c.BlockedReasonDetail
		sessionTotal[i] = c.SessionTotalKg
		overdue[i] = c.OverduePending
		rowSeq[i] = c.RowSeq
		itemSeq[i] = c.ItemSeq
	}

	var amendedAtUTC *time.Time
	if amendedAt != nil {
		u := amendedAt.UTC()
		amendedAtUTC = &u
	}

	conflict := ""
	if upsert := amended; upsert {
		// An amend upserts changed cells on the natural key. This is the FULL idempotency contract
		// (real columns updated), not the banned idempotency_key-only conflict handler.
		conflict = `
ON CONFLICT (tenant_id, feed_direction_issue_id, shed_id, partition_key, session_no, shed_tag_key, breed_key, feed_item_key)
DO UPDATE SET
  park_label = EXCLUDED.park_label, shed_label = EXCLUDED.shed_label, shed_tag = EXCLUDED.shed_tag,
  breed = EXCLUDED.breed, ration_group = EXCLUDED.ration_group, experiment_arm = EXCLUDED.experiment_arm,
  session_label = EXCLUDED.session_label, head_count = EXCLUDED.head_count,
  head_count_informational = EXCLUDED.head_count_informational, workflow = EXCLUDED.workflow,
  feed_item_label = EXCLUDED.feed_item_label, quantity_kg = EXCLUDED.quantity_kg,
  grams_per_head = EXCLUDED.grams_per_head, shed_factor = EXCLUDED.shed_factor,
  blocked_reason_code = EXCLUDED.blocked_reason_code, blocked_reason_detail = EXCLUDED.blocked_reason_detail,
  session_total_kg = EXCLUDED.session_total_kg, overdue_pending = EXCLUDED.overdue_pending,
  row_seq = EXCLUDED.row_seq, item_seq = EXCLUDED.item_seq,
  amended = true, amended_at = EXCLUDED.amended_at, updated_at = now()`
	}

	_, err := tx.Exec(ctx, `
INSERT INTO feed_direction_issue_rows (
  tenant_id, feed_direction_issue_id, park_id, park_label, shed_id, shed_label, shed_tag, breed,
  ration_group, experiment_arm, session_no, session_label, head_count, head_count_informational,
  workflow, feed_item_label, quantity_kg, grams_per_head, shed_factor, blocked_reason_code,
  blocked_reason_detail, session_total_kg, overdue_pending, row_seq, item_seq, partition_label,
  amended, amended_at
)
SELECT $1::uuid, $2::uuid, $3::uuid, t.park_label, t.shed_id::uuid, t.shed_label, t.shed_tag, t.breed,
  t.ration_group, t.experiment_arm, t.session_no, t.session_label, t.head_count, t.head_count_informational,
  t.workflow, t.feed_item_label, t.quantity_kg::numeric, t.grams_per_head::numeric, t.shed_factor::numeric,
  t.blocked_reason_code, t.blocked_reason_detail, t.session_total_kg::numeric, t.overdue_pending, t.row_seq,
  t.item_seq, nullif(t.partition_label, ''), $27::boolean, $28::timestamptz
FROM unnest(
  $4::text[], $5::text[], $6::text[], $7::text[], $8::text[], $9::text[], $10::text[], $11::int4[],
  $12::text[], $13::int8[], $14::bool[], $15::text[], $16::text[], $17::text[], $18::text[], $19::text[],
  $20::text[], $21::text[], $22::text[], $23::bool[], $24::int4[], $25::int4[], $26::text[]
) AS t(
  park_label, shed_id, shed_label, shed_tag, breed, ration_group, experiment_arm, session_no,
  session_label, head_count, head_count_informational, workflow, feed_item_label, quantity_kg,
  grams_per_head, shed_factor, blocked_reason_code, blocked_reason_detail, session_total_kg,
  overdue_pending, row_seq, item_seq, partition_label
)`+conflict,
		tenantID, issueID, parkID, parkLabel, shedID, shedLabel, shedTag, breed, rationGroup,
		experimentArm, sessionNo, sessionLabel, headCount, hcInfo, workflow, feedItemLabel, quantity,
		grams, factor, blockedCode, blockedDetail, sessionTotal, overdue, rowSeq, itemSeq, partitionLabel,
		amended, amendedAtUTC)
	if err != nil {
		return fmt.Errorf("feeddirection: insert issue rows: %w", err)
	}
	return nil
}

func validateStoredRowSeqs(cells []domain.StoredCell) error {
	bySeq := make(map[int32]domain.StoredRowKey)
	for _, cell := range cells {
		key := cell.RowKey()
		if existing, ok := bySeq[cell.RowSeq]; ok && existing != key {
			return fmt.Errorf(
				"feeddirection: duplicate row_seq %d for distinct rows (%s/%s/%d/%s and %s/%s/%d/%s)",
				cell.RowSeq,
				existing.ShedID, existing.PartitionKey, existing.SessionNo, existing.Workflow,
				key.ShedID, key.PartitionKey, key.SessionNo, key.Workflow,
			)
		}
		bySeq[cell.RowSeq] = key
	}
	return nil
}

// deleteIssueCells removes the cells an amendment dropped, in one set-based DELETE joined to the
// removed keys (never a per-key DELETE loop).
func deleteIssueCells(ctx context.Context, tx pgx.Tx, tenantID, issueID string, keys []domain.CellKey) error {
	if len(keys) == 0 {
		return nil
	}
	n := len(keys)
	shedID := make([]string, n)
	partitionKey := make([]string, n)
	sessionNo := make([]int32, n)
	shedTagKey := make([]string, n)
	breedKey := make([]string, n)
	feedItemKey := make([]string, n)
	for i, k := range keys {
		shedID[i] = k.ShedID
		partitionKey[i] = k.PartitionKey
		sessionNo[i] = k.SessionNo
		shedTagKey[i] = k.ShedTagKey
		breedKey[i] = k.BreedKey
		feedItemKey[i] = k.FeedItemKey
	}
	_, err := tx.Exec(ctx, `
DELETE FROM feed_direction_issue_rows r
USING unnest($3::text[], $4::int4[], $5::text[], $6::text[], $7::text[], $8::text[]) AS k(shed_id, session_no, shed_tag_key, breed_key, feed_item_key, partition_key)
WHERE r.tenant_id = $1::uuid AND r.feed_direction_issue_id = $2::uuid
  AND r.shed_id = k.shed_id::uuid AND r.session_no = k.session_no
  AND r.shed_tag_key = k.shed_tag_key AND r.breed_key = k.breed_key AND r.feed_item_key = k.feed_item_key
  AND r.partition_key = k.partition_key`,
		tenantID, issueID, shedID, sessionNo, shedTagKey, breedKey, feedItemKey, partitionKey)
	if err != nil {
		return fmt.Errorf("feeddirection: delete amended-out issue cells: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// ScheduleReader (feed_schedule_config)
// ---------------------------------------------------------------------------

// ListScheduleClocks returns every configured (workflow, clock) for a park, in force on asOf. The
// times are read as HH:MM:SS local wall-clock strings -- feed_schedule_config stores `time` without
// zone precisely so the recurring business rule is not bound to an offset (migration 000004).
func (r *Repository) ListScheduleClocks(ctx context.Context, tenantID, parkID string, asOf time.Time) ([]domain.WorkflowClock, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	asOfDate := asOf.Format("2006-01-02")
	rows, err := r.pool.Query(ctx, `
SELECT workflow, to_char(direction_time, 'HH24:MI:SS'), to_char(correction_time, 'HH24:MI:SS'),
       to_char(transport_time, 'HH24:MI:SS')
FROM feed_schedule_config
WHERE tenant_id = $1::uuid AND park_id = $2::uuid
  AND valid_from <= $3::date
  AND (valid_to IS NULL OR valid_to > $3::date)
ORDER BY workflow`, tenantID, parkID, asOfDate)
	if err != nil {
		return nil, fmt.Errorf("feeddirection: list schedule clocks: %w", err)
	}
	defer rows.Close()
	out := make([]domain.WorkflowClock, 0, 2)
	for rows.Next() {
		var clock domain.WorkflowClock
		var transport *string
		if err := rows.Scan(&clock.Workflow, &clock.DirectionTime, &clock.CorrectionTime, &transport); err != nil {
			return nil, fmt.Errorf("feeddirection: scan schedule clock: %w", err)
		}
		clock.TransportTime = transport
		out = append(out, clock)
	}
	return out, rows.Err()
}

// ListScheduledParks returns the distinct parks with any schedule clock in force on asOf.
func (r *Repository) ListScheduledParks(ctx context.Context, tenantID string, asOf time.Time) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	asOfDate := asOf.Format("2006-01-02")
	rows, err := r.pool.Query(ctx, `
SELECT DISTINCT park_id::text
FROM feed_schedule_config
WHERE tenant_id = $1::uuid
  AND valid_from <= $2::date
  AND (valid_to IS NULL OR valid_to > $2::date)
ORDER BY park_id::text`, tenantID, asOfDate)
	if err != nil {
		return nil, fmt.Errorf("feeddirection: list scheduled parks: %w", err)
	}
	defer rows.Close()
	out := make([]string, 0)
	for rows.Next() {
		var parkID string
		if err := rows.Scan(&parkID); err != nil {
			return nil, fmt.Errorf("feeddirection: scan scheduled park: %w", err)
		}
		out = append(out, parkID)
	}
	return out, rows.Err()
}
