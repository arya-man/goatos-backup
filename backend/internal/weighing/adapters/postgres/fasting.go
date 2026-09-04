package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// Weighing FASTING task adapter (maintainer decision 2026-09-03; product rule
// and clocks in domain/fasting.go, table in migration 000253).
//
// ISOLATION: this file reads/writes weighing_fasting_tasks, weighing_campaigns,
// weighing_campaign_sheds, proof_artifacts, weighing_idempotency_records and
// audit_log — weighing-owned tables plus the allowlisted proof/audit plumbing —
// and the ORG table locations for the park's display name. No herd, goat,
// vaccination or other module table on any path.

// fastingSubmitAction / fastingVerdictAction are the AUDIT actions and the
// idempotency-record scopes for the two fasting writes. They are deliberately
// NOT outbox event types: nothing consumes a fasting bus event today, and a
// producer with no consumer is a silent drop. The durable trail is the fasting
// row itself, its audit rows, its idempotency snapshots, and the verification
// item (whose own events drive the verifier notifications).
const (
	fastingSubmitAction  = "weighing.fasting_submitted"
	fastingVerdictAction = "weighing.fasting_verdict_applied"
)

// fastingSelectSQL is the ONE row shape every fasting read scans, so the list,
// the by-id read and the post-submit re-read cannot drift apart. `ft` is the
// fasting alias; the park name resolves through the allowlisted locations org
// table and degrades to ” rather than failing the row.
const fastingSelectSQL = `
SELECT ft.fasting_task_id::text, ft.tenant_id::text, ft.campaign_id::text, ft.park_id::text,
       COALESCE(p.name, ''),
       ft.operator_user_id::text,
       ft.planned_weigh_date::text, ft.weigh_business_date::text,
       ft.status,
       COALESCE(ft.feed_proof_ref::text, ''), COALESCE(ft.water_proof_ref::text, ''),
       COALESCE(ft.submitted_by::text, ''), ft.submitted_at, ft.verified_at,
       COALESCE(ft.rework_reason, ''),
       ft.rolled_forward_count, ft.row_version, ft.created_at, ft.updated_at,
       (
         SELECT COUNT(*)::int
         FROM weighing_campaign_sheds cs
         WHERE cs.tenant_id = ft.tenant_id
           AND cs.campaign_id = ft.campaign_id
           AND cs.status <> 'canceled'
       ) AS shed_count
FROM weighing_fasting_tasks ft
LEFT JOIN locations p
  ON p.tenant_id = ft.tenant_id AND p.location_id = ft.park_id
`

// Package-level SQL for the fasting adapter, hoisted so query-plan tests and
// the scale guard can reach each statement by name.
const fastingInsertSQL = `
INSERT INTO weighing_fasting_tasks (
  tenant_id, campaign_id, park_id, operator_user_id,
  planned_weigh_date, weigh_business_date, idempotency_key, created_by
)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::date, $5::date, $6, $7::uuid)
ON CONFLICT (tenant_id, campaign_id) DO NOTHING`

const fastingSyncOnUpdateSQL = `
UPDATE weighing_fasting_tasks
SET operator_user_id = $3::uuid,
    park_id = $4::uuid,
    planned_weigh_date = $5::date,
    weigh_business_date = $5::date,
    row_version = row_version + 1,
    updated_at = now()
WHERE tenant_id = $1::uuid AND campaign_id = $2::uuid
  AND submitted_at IS NULL`

const fastingExistsSQL = `
SELECT EXISTS (
  SELECT 1 FROM weighing_fasting_tasks
  WHERE tenant_id = $1::uuid AND campaign_id = $2::uuid
)`

const fastingByIDSQL = fastingSelectSQL + `
WHERE ft.tenant_id = $1::uuid
  AND ft.fasting_task_id = $2::uuid
  AND ($3::uuid IS NULL OR ft.operator_user_id = $3::uuid)`

// fastingLiveShedsSQL is the card's REQUIRED coverage: every non-canceled
// bucket of the campaign, with any existing evidence row.
const fastingLiveShedsSQL = `
SELECT cs.campaign_shed_id::text,
       cs.display_name,
       COALESCE(cs.partition_label, ''),
       cs.location_id::text,
       COALESCE(sp.fasting_shed_id::text, ''),
       COALESCE(sp.status, 'open'),
       COALESCE(sp.feed_proof_ref::text, ''),
       COALESCE(sp.water_proof_ref::text, ''),
       COALESCE(sp.rework_reason, ''),
       COALESCE(sp.row_version, 0)
FROM weighing_campaign_sheds cs
LEFT JOIN weighing_fasting_shed_proofs sp
  ON sp.tenant_id = cs.tenant_id AND sp.campaign_shed_id = cs.campaign_shed_id
       AND sp.fasting_task_id = $3::uuid
WHERE cs.tenant_id = $1::uuid AND cs.campaign_id = $2::uuid
  AND cs.status <> 'canceled'
ORDER BY cs.display_name, cs.campaign_shed_id`

// fastingAttachShedsSQL fills a PAGE of cards' shed lists in one batched read.
const fastingAttachShedsSQL = `
SELECT ft.fasting_task_id::text,
       cs.campaign_shed_id::text,
       cs.display_name,
       COALESCE(cs.partition_label, ''),
       cs.location_id::text,
       COALESCE(sp.fasting_shed_id::text, ''),
       COALESCE(sp.status, 'open'),
       COALESCE(sp.feed_proof_ref::text, ''),
       COALESCE(sp.water_proof_ref::text, ''),
       COALESCE(sp.rework_reason, ''),
       COALESCE(sp.row_version, 0)
FROM weighing_fasting_tasks ft
JOIN weighing_campaign_sheds cs
  ON cs.tenant_id = ft.tenant_id AND cs.campaign_id = ft.campaign_id AND cs.status <> 'canceled'
LEFT JOIN weighing_fasting_shed_proofs sp
  ON sp.tenant_id = ft.tenant_id AND sp.fasting_task_id = ft.fasting_task_id
       AND sp.campaign_shed_id = cs.campaign_shed_id
WHERE ft.fasting_task_id = ANY($1::uuid[])
ORDER BY cs.display_name, cs.campaign_shed_id`

const fastingShedUpsertSQL = `
INSERT INTO weighing_fasting_shed_proofs (
  tenant_id, fasting_task_id, campaign_shed_id, shed_label,
  feed_proof_ref, water_proof_ref, status
)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5::uuid, $6::uuid, 'pending_verification')
ON CONFLICT (tenant_id, fasting_task_id, campaign_shed_id) DO UPDATE SET
  shed_label = EXCLUDED.shed_label,
  feed_proof_ref = EXCLUDED.feed_proof_ref,
  water_proof_ref = EXCLUDED.water_proof_ref,
  status = 'pending_verification',
  rework_reason = NULL,
  row_version = weighing_fasting_shed_proofs.row_version + 1,
  updated_at = now()
RETURNING fasting_shed_id::text, row_version`

const fastingSubmitUpdateSQL = `
UPDATE weighing_fasting_tasks
SET submitted_by = $3::uuid,
    submitted_at = COALESCE(submitted_at, now()),
    status = 'pending_verification',
    rework_reason = NULL,
    row_version = row_version + 1,
    updated_at = now()
WHERE tenant_id = $1::uuid AND fasting_task_id = $2::uuid`

const fastingReadAfterSubmitSQL = fastingSelectSQL + `
WHERE ft.tenant_id = $1::uuid AND ft.fasting_task_id = $2::uuid`

const fastingProofValidateSQL = `
SELECT proof_id::text, upload_state, proof_type, COALESCE(mime_type, ''),
       COALESCE(metadata->>'capture_source', '')
FROM proof_artifacts
WHERE tenant_id = $1::uuid AND proof_id = ANY($2::uuid[])`

// fastingShedVerdictSQL lands one verdict on ONE shed's evidence row.
const fastingShedVerdictSQL = `
UPDATE weighing_fasting_shed_proofs
SET status = $3,
    rework_reason = NULLIF($4, ''),
    row_version = row_version + 1,
    updated_at = now()
WHERE tenant_id = $1::uuid AND fasting_shed_id = $2::uuid
  AND status = 'pending_verification'
RETURNING fasting_task_id::text`

// fastingParentRollupSQL recomputes the CARD's status from its shed rows:
// rework if any shed is rework; completed only when every row is completed AND
// every live bucket has a row; otherwise pending_verification. submitted_at is
// never touched — the midnight gate reads submission, not review state.
const fastingParentRollupSQL = `
UPDATE weighing_fasting_tasks ft
SET status = roll.status,
    verified_by = CASE WHEN roll.status = 'completed' THEN NULLIF($3, '')::uuid ELSE ft.verified_by END,
    verified_at = CASE WHEN roll.status = 'completed' THEN now() ELSE ft.verified_at END,
    rework_reason = roll.reason,
    row_version = ft.row_version + 1,
    updated_at = now()
FROM (
  SELECT CASE
           WHEN COUNT(*) FILTER (WHERE sp.status = 'rework') > 0 THEN 'rework'
           WHEN COUNT(*) FILTER (WHERE sp.status IS DISTINCT FROM 'completed') = 0
                AND COUNT(*) > 0 THEN 'completed'
           ELSE 'pending_verification'
         END AS status,
         (ARRAY_REMOVE(ARRAY_AGG(sp.rework_reason ORDER BY sp.updated_at DESC), NULL))[1] AS reason
  FROM weighing_campaign_sheds cs
  LEFT JOIN weighing_fasting_shed_proofs sp
    ON sp.tenant_id = cs.tenant_id AND sp.campaign_shed_id = cs.campaign_shed_id
         AND sp.fasting_task_id = $2::uuid
  WHERE cs.tenant_id = $1::uuid
    AND cs.campaign_id = (SELECT campaign_id FROM weighing_fasting_tasks WHERE tenant_id = $1::uuid AND fasting_task_id = $2::uuid)
    AND cs.status <> 'canceled'
) roll
WHERE ft.tenant_id = $1::uuid AND ft.fasting_task_id = $2::uuid`

const fastingCampaignStartDateSQL = `
SELECT c.start_business_date::text,
       COALESCE(ft.submitted_at IS NOT NULL, false),
       ft.fasting_task_id IS NOT NULL
FROM weighing_campaigns c
LEFT JOIN weighing_fasting_tasks ft
  ON ft.tenant_id = c.tenant_id AND ft.campaign_id = c.campaign_id
WHERE c.tenant_id = $1::uuid AND c.campaign_id = $2::uuid`

type fastingRowScanner interface {
	Scan(dest ...any) error
}

func scanFastingTask(row fastingRowScanner) (domain.FastingTask, error) {
	var task domain.FastingTask
	var submittedAt, verifiedAt *time.Time
	if err := row.Scan(
		&task.FastingTaskID, &task.TenantID, &task.CampaignID, &task.ParkID,
		&task.ParkName,
		&task.OperatorUserID,
		&task.PlannedWeighDate, &task.WeighBusinessDate,
		&task.Status,
		&task.FeedProofRef, &task.WaterProofRef,
		&task.SubmittedBy, &submittedAt, &verifiedAt,
		&task.ReworkReason,
		&task.RolledForwardCount, &task.RowVersion, &task.CreatedAt, &task.UpdatedAt,
		&task.ShedCount,
	); err != nil {
		return domain.FastingTask{}, err
	}
	task.SubmittedAt = submittedAt
	task.VerifiedAt = verifiedAt
	task.RemovalBusinessDate = domain.RemovalBusinessDate(task.WeighBusinessDate)
	task.SubjectLabel = domain.FastingSubjectLabel(task.ParkName, task.ShedCount)
	return task, nil
}

// createFastingTaskTx writes the campaign's fasting row inside the campaign
// create/update transaction. ON CONFLICT DO NOTHING makes an idempotent create
// replay a no-op at this grain (the campaign-level replay short-circuits before
// reaching here anyway).
func (r *Repository) createFastingTaskTx(ctx context.Context, tx pgx.Tx, cmd domain.CreateCampaign, campaignID string) error {
	if strings.TrimSpace(cmd.FastingOperatorUserID) == "" {
		// Internal/legacy callers (tests, backfills) that predate the fasting
		// rule create no fasting row; the midnight gate and the visibility
		// window key on the row's existence, so their campaigns behave exactly
		// as before the feature.
		return nil
	}
	_, err := tx.Exec(ctx, fastingInsertSQL,
		cmd.TenantID, campaignID, cmd.ParkID, cmd.FastingOperatorUserID,
		cmd.StartBusinessDate, cmd.IdempotencyKey+":fasting", cmd.CreatedBy)
	return err
}

// syncFastingTaskOnUpdateTx keeps the fasting row aligned with an EDIT of the
// campaign (new date, new park, new removal operator). The service layer has
// already refused a date move on a submitted fasting task, so the WHERE guard
// here (submitted_at IS NULL) is defence in depth, not the primary gate. A
// legacy campaign with no fasting row gains one when the edit names a removal
// operator — that is how pre-feature tasks are brought under the rule.
func (r *Repository) syncFastingTaskOnUpdateTx(ctx context.Context, tx pgx.Tx, cmd domain.CreateCampaign, campaignID string) error {
	if strings.TrimSpace(cmd.FastingOperatorUserID) == "" {
		return nil
	}
	tag, err := tx.Exec(ctx, fastingSyncOnUpdateSQL,
		cmd.TenantID, campaignID, cmd.FastingOperatorUserID, cmd.ParkID, cmd.StartBusinessDate)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	// No unsubmitted row updated: either a submitted row exists (leave it —
	// the service refused date moves; operator/park edits after submission
	// would rewrite who did work that already happened) or none exists yet.
	var exists bool
	if err := tx.QueryRow(ctx, fastingExistsSQL, cmd.TenantID, campaignID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	return r.createFastingTaskTx(ctx, tx, cmd, campaignID)
}

// fastingCursor is the operator list keyset: (weigh_business_date DESC,
// fasting_task_id DESC).
type fastingCursor struct {
	Date string `json:"d"`
	ID   string `json:"i"`
}

func decodeFastingCursor(raw string) (fastingCursor, error) {
	if strings.TrimSpace(raw) == "" {
		return fastingCursor{}, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return fastingCursor{}, err
	}
	var cur fastingCursor
	if err := json.Unmarshal(decoded, &cur); err != nil {
		return fastingCursor{}, err
	}
	return cur, nil
}

func encodeFastingCursor(cur fastingCursor) string {
	raw, _ := json.Marshal(cur)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// attachFastingSheds fills each card's per-shed evidence list in ONE batched
// read over the page — never a query per card.
func (r *Repository) attachFastingSheds(ctx context.Context, items []domain.FastingTask) error {
	if len(items) == 0 {
		return nil
	}
	taskIDs := make([]string, 0, len(items))
	byTask := make(map[string]int, len(items))
	for i := range items {
		taskIDs = append(taskIDs, items[i].FastingTaskID)
		byTask[items[i].FastingTaskID] = i
	}
	// projection-review: membership=live (non-canceled) weighing_campaign_sheds
	// buckets of the page's campaigns; group_key=(fasting_task_id,
	// campaign_shed_id) — the LEFT JOIN's right side is UNIQUE on exactly that
	// pair (weighing_fasting_shed_proofs_shed_uq), so the join is 1:0..1 and
	// cannot multiply bucket rows; pagination=inherits the caller's page (the
	// task-id array IS the page); scope=tenant + the page's task ids.
	rows, err := r.pool.Query(ctx, fastingAttachShedsSQL, taskIDs)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var taskID string
		var shed domain.FastingShedProof
		var displayName, partitionLabel string
		if err := rows.Scan(&taskID, &shed.CampaignShedID, &displayName, &partitionLabel, &shed.ShedLocationID,
			&shed.FastingShedID, &shed.Status, &shed.FeedProofRef, &shed.WaterProofRef,
			&shed.ReworkReason, &shed.RowVersion); err != nil {
			return err
		}
		shed.ShedLabel = oploc.OperationalLocation{ShedName: displayName, PartitionLabel: partitionLabel}.Display()
		idx, ok := byTask[taskID]
		if !ok {
			continue
		}
		items[idx].Sheds = append(items[idx].Sheds, shed)
	}
	return rows.Err()
}

// FastingTaskByID reads one task. A non-empty operatorUserID is an
// authorization predicate INSIDE the query — the same statement authorizes and
// returns the row, so the two cannot disagree — and a task that exists but
// belongs to someone else answers ErrNotFound, leaking nothing.
func (r *Repository) FastingTaskByID(ctx context.Context, tenantID, fastingTaskID, operatorUserID string) (domain.FastingTask, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	task, err := scanFastingTask(r.pool.QueryRow(ctx, fastingByIDSQL,
		tenantID, fastingTaskID, nullableUUIDString(operatorUserID)))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.FastingTask{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.FastingTask{}, err
	}
	items := []domain.FastingTask{task}
	if err := r.attachFastingSheds(ctx, items); err != nil {
		return domain.FastingTask{}, err
	}
	return items[0], nil
}

// loadFastingLiveSheds reads the campaign's live bucket set with any existing
// evidence rows, under the parent row lock every fasting write already holds.
func (r *Repository) loadFastingLiveSheds(ctx context.Context, tx pgx.Tx, tenantID, campaignID, fastingTaskID string) ([]domain.FastingShedProof, error) {
	rows, err := tx.Query(ctx, fastingLiveShedsSQL, tenantID, campaignID, fastingTaskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFastingShedRows(rows)
}

func scanFastingShedRows(rows pgx.Rows) ([]domain.FastingShedProof, error) {
	out := []domain.FastingShedProof{}
	for rows.Next() {
		var shed domain.FastingShedProof
		var displayName, partitionLabel string
		if err := rows.Scan(&shed.CampaignShedID, &displayName, &partitionLabel, &shed.ShedLocationID,
			&shed.FastingShedID, &shed.Status, &shed.FeedProofRef, &shed.WaterProofRef,
			&shed.ReworkReason, &shed.RowVersion); err != nil {
			return nil, err
		}
		shed.ShedLabel = oploc.OperationalLocation{ShedName: displayName, PartitionLabel: partitionLabel}.Display()
		out = append(out, shed)
	}
	return out, rows.Err()
}

// fastingShedsForTaskTx is the read attached to every served card.
func (r *Repository) fastingShedsForTaskTx(ctx context.Context, tx pgx.Tx, tenantID, campaignID, fastingTaskID string) ([]domain.FastingShedProof, error) {
	return r.loadFastingLiveSheds(ctx, tx, tenantID, campaignID, fastingTaskID)
}

// validateFastingProofsTx asserts the two refs are DISTINCT, tenant-owned,
// COMPLETED uploads, VIDEO artifacts, captured by the in-app camera. One
// set-based read for the pair, mirroring the feed proof validator's contract.
func (r *Repository) validateFastingProofsTx(ctx context.Context, tx pgx.Tx, tenantID string, refs []string) error {
	rows, err := tx.Query(ctx, fastingProofValidateSQL,
		tenantID, refs)
	if err != nil {
		return err
	}
	defer rows.Close()
	valid := map[string]bool{}
	for rows.Next() {
		var proofID, uploadState, proofType, mimeType, captureSource string
		if err := rows.Scan(&proofID, &uploadState, &proofType, &mimeType, &captureSource); err != nil {
			return err
		}
		valid[proofID] = uploadState == "completed" &&
			proofType == "video" &&
			strings.HasPrefix(mimeType, "video/") &&
			captureSource == "in_app_camera"
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, ref := range refs {
		if !valid[ref] {
			return ports.ErrFastingProofInvalid
		}
	}
	return nil
}

// ApplyFastingVerdict applies a verifier approve/rework to the fasting row.
// Keyed on the verdict EVENT id so an at-least-once redelivery applies once. It
// NEVER touches submitted_at — the midnight gate reads submission, and a rework
// must not un-run a weighing that already happened.
func (r *Repository) ApplyFastingVerdict(ctx context.Context, verdict domain.FastingVerdict) error {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	idem := verdict.EventID
	if strings.TrimSpace(idem) == "" {
		idem = fmt.Sprintf("%s:%s:%s", verdict.FastingShedID, verdict.Status, verdict.VerifiedBy)
	}
	fingerprint := idempotencyFingerprint(verdict)
	if _, _, _, ok, err := r.idempotencyResource(ctx, tx, verdict.TenantID, fastingVerdictAction, idem, fingerprint); err != nil {
		return err
	} else if ok {
		return nil
	}

	newStatus := domain.FastingStatusCompleted
	reason := ""
	if verdict.Status == domain.VerificationStatusRework {
		newStatus = domain.FastingStatusRework
		reason = strings.TrimSpace(verdict.Reason)
	} else if verdict.Status != domain.VerificationStatusVerified {
		return ports.ErrInvalidArgument
	}

	var fastingTaskID string
	err = tx.QueryRow(ctx, fastingShedVerdictSQL,
		verdict.TenantID, verdict.FastingShedID, newStatus, reason).Scan(&fastingTaskID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Not pending: already decided (stale redelivery under a new event id)
		// or never submitted. Record the idempotency row and stop — re-erroring
		// would poison the consumer over a verdict with nowhere left to land.
		if err := r.recordIdempotency(ctx, tx, verdict.TenantID, fastingVerdictAction, idem, fingerprint, "weighing_fasting_shed", verdict.FastingShedID, verdict); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if err != nil {
		return err
	}
	// Roll the CARD's status up from its shed rows in the same transaction, so
	// the two grains can never disagree across a crash.
	if _, err := tx.Exec(ctx, fastingParentRollupSQL, verdict.TenantID, fastingTaskID, verdict.VerifiedBy); err != nil {
		return err
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     verdict.TenantID,
		ActorID:      verdict.VerifiedBy,
		ActorType:    "user",
		Action:       fastingVerdictAction,
		ResourceType: "weighing_fasting_shed",
		ResourceID:   verdict.FastingShedID,
		AfterState:   map[string]any{"status": newStatus, "reason": reason, "fasting_task_id": fastingTaskID},
		Metadata: map[string]any{
			"verdict":  verdict.Status,
			"event_id": verdict.EventID,
		},
	}); err != nil {
		return err
	}
	if err := r.recordIdempotency(ctx, tx, verdict.TenantID, fastingVerdictAction, idem, fingerprint, "weighing_fasting_shed", verdict.FastingShedID, verdict); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CampaignStartDate reads the campaign's current weigh date plus the fasting
// facts the edit path's cutoff/lock checks need — one statement, one snapshot.
func (r *Repository) CampaignStartDate(ctx context.Context, tenantID, campaignID string) (string, bool, bool, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	var startDate string
	var fastingSubmitted, hasFasting bool
	err := r.pool.QueryRow(ctx, fastingCampaignStartDateSQL,
		tenantID, campaignID).Scan(&startDate, &fastingSubmitted, &hasFasting)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, false, ports.ErrNotFound
	}
	return startDate, fastingSubmitted, hasFasting, err
}

var _ ports.FastingStore = (*Repository)(nil)

// nullableUUIDString maps "" to nil for a ::uuid bind.
func nullableUUIDString(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}
