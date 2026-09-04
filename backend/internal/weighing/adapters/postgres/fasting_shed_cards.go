package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// PER-SHED CARDS (maintainer correction #2, 2026-09-03): the operator's list
// serves ONE CARD PER SHED — never an umbrella card the sheds hide inside —
// and the operator SUBMITS ONE SHED at a time. The round (weighing_fasting_
// tasks) still owns the operator, the dates and the MIDNIGHT GATE: the
// parent's submitted_at is stamped only when the round's LAST unsubmitted
// shed goes in, and that stamp is the only fact the gate reads.

// fastingShedCardsSQL: one row per live bucket of every visible round. The
// visibility window and terminal-campaign withholding are identical to the
// round list; the keyset is (weigh date DESC, campaign_shed_id DESC).
//
// projection-review: membership=non-canceled weighing_campaign_sheds buckets
// of the operator's visible rounds; group_key=(ft.fasting_task_id,
// cs.campaign_shed_id) — the LEFT JOINed evidence side is UNIQUE on exactly
// that pair (weighing_fasting_shed_proofs_shed_uq), so the join is 1:0..1 and
// never multiplies bucket rows; pagination=keyset on (weigh_business_date
// DESC, campaign_shed_id DESC) with LIMIT lookahead; scope=tenant + assigned
// operator always.
const fastingShedCardsSQL = `
SELECT ft.fasting_task_id::text,
       cs.campaign_shed_id::text,
       COALESCE(sp.fasting_shed_id::text, ''),
       cs.display_name,
       COALESCE(cs.partition_label, ''),
       COALESCE(p.name, ''),
       COALESCE(sp.status, 'open'),
       COALESCE(sp.rework_reason, ''),
       COALESCE(sp.feed_proof_ref::text, ''),
       COALESCE(sp.water_proof_ref::text, ''),
       ft.planned_weigh_date::text,
       ft.weigh_business_date::text,
       ft.submitted_at,
       COALESCE(sp.row_version, 0)
FROM weighing_fasting_tasks ft
JOIN weighing_campaign_sheds cs
  ON cs.tenant_id = ft.tenant_id AND cs.campaign_id = ft.campaign_id AND cs.status <> 'canceled'
LEFT JOIN weighing_fasting_shed_proofs sp
  ON sp.tenant_id = ft.tenant_id AND sp.fasting_task_id = ft.fasting_task_id
       AND sp.campaign_shed_id = cs.campaign_shed_id
LEFT JOIN locations p
  ON p.tenant_id = ft.tenant_id AND p.location_id = ft.park_id
WHERE ft.tenant_id = $1::uuid
  AND ft.operator_user_id = $2::uuid
  AND ($3::timestamptz AT TIME ZONE 'Asia/Kolkata') >= ((ft.weigh_business_date - 1) + TIME '20:00')
  AND (ft.submitted_at IS NOT NULL OR EXISTS (
        SELECT 1 FROM weighing_campaigns c
        WHERE c.tenant_id = ft.tenant_id AND c.campaign_id = ft.campaign_id
          AND c.status NOT IN ('completed','closed','canceled')
      ))
  AND ($4::date IS NULL OR ft.weigh_business_date < $4::date
       OR (ft.weigh_business_date = $4::date AND cs.campaign_shed_id < $5::uuid))
ORDER BY ft.weigh_business_date DESC, cs.campaign_shed_id DESC
LIMIT $6`

func (r *Repository) ListFastingShedCardsForOperator(ctx context.Context, tenantID, operatorUserID string, now time.Time, cursor string, limit int) (domain.FastingShedCardPage, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	cur, err := decodeFastingCursor(cursor)
	if err != nil {
		return domain.FastingShedCardPage{}, ports.ErrInvalidArgument
	}
	rows, err := r.pool.Query(ctx, fastingShedCardsSQL,
		tenantID, operatorUserID, now.UTC(),
		nullableString(cur.Date), nullableUUIDString(cur.ID), limit+1)
	if err != nil {
		return domain.FastingShedCardPage{}, err
	}
	defer rows.Close()
	page := domain.FastingShedCardPage{Items: []domain.FastingShedCard{}}
	for rows.Next() {
		var card domain.FastingShedCard
		var displayName, partitionLabel string
		var submittedAt *time.Time
		if err := rows.Scan(&card.FastingTaskID, &card.CampaignShedID, &card.FastingShedID,
			&displayName, &partitionLabel, &card.ParkName,
			&card.Status, &card.ReworkReason, &card.FeedProofRef, &card.WaterProofRef,
			&card.PlannedWeighDate, &card.WeighBusinessDate, &submittedAt, &card.RowVersion); err != nil {
			return domain.FastingShedCardPage{}, err
		}
		card.ShedLabel = oploc.OperationalLocation{ShedName: displayName, PartitionLabel: partitionLabel}.Display()
		card.SubjectLabel = domain.FastingShedSubjectLabel(card.ShedLabel)
		card.RemovalBusinessDate = domain.RemovalBusinessDate(card.WeighBusinessDate)
		card.SubmittedAt = submittedAt
		page.Items = append(page.Items, card)
	}
	if err := rows.Err(); err != nil {
		return domain.FastingShedCardPage{}, err
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor = encodeFastingCursor(fastingCursor{Date: last.WeighBusinessDate, ID: last.CampaignShedID})
	}
	return page, nil
}

const fastingShedLockSQL = `
SELECT ft.operator_user_id::text, ft.campaign_id::text, ft.submitted_at IS NOT NULL
FROM weighing_fasting_tasks ft
WHERE ft.tenant_id = $1::uuid AND ft.fasting_task_id = $2::uuid
FOR UPDATE`

const fastingShedTargetSQL = `
SELECT cs.display_name, COALESCE(cs.partition_label, ''), cs.location_id::text,
       COALESCE(sp.status, 'open'),
       COALESCE(sp.feed_proof_ref::text, ''), COALESCE(sp.water_proof_ref::text, '')
FROM weighing_campaign_sheds cs
LEFT JOIN weighing_fasting_shed_proofs sp
  ON sp.tenant_id = cs.tenant_id AND sp.campaign_shed_id = cs.campaign_shed_id
       AND sp.fasting_task_id = $3::uuid
WHERE cs.tenant_id = $1::uuid AND cs.campaign_id = $2::uuid
  AND cs.campaign_shed_id = $4::uuid AND cs.status <> 'canceled'`

const fastingSiblingRefReuseSQL = `
SELECT EXISTS (
  SELECT 1 FROM weighing_fasting_shed_proofs
  WHERE tenant_id = $1::uuid AND fasting_task_id = $2::uuid
    AND campaign_shed_id <> $3::uuid
    AND (feed_proof_ref = ANY($4::uuid[]) OR water_proof_ref = ANY($4::uuid[]))
)`

// fastingRoundFullyCoveredSQL: does every live bucket now hold a SUBMITTED
// pair? Runs under the parent row lock, so a concurrent sibling submit
// serializes and exactly one of them stamps the round.
//
// "Submitted" is the ROW'S STATE, not the presence of a clip. Verifier items
// are enqueued per shed the moment each shed lands, so shed A can be submitted
// AND bounced to rework before shed B is ever submitted. A's row still carries
// its (rejected) refs, so a ref-presence check would count it as covered and
// B's submit would stamp the round -- opening the midnight gate while A still
// owes fresh clips. Only a row sitting in pending_verification or completed,
// holding BOTH refs, counts; open and rework rows leave the round unstamped.
const fastingRoundFullyCoveredSQL = `
SELECT NOT EXISTS (
  SELECT 1
  FROM weighing_campaign_sheds cs
  LEFT JOIN weighing_fasting_shed_proofs sp
    ON sp.tenant_id = cs.tenant_id AND sp.campaign_shed_id = cs.campaign_shed_id
         AND sp.fasting_task_id = $3::uuid
  WHERE cs.tenant_id = $1::uuid AND cs.campaign_id = $2::uuid AND cs.status <> 'canceled'
    AND (
      sp.fasting_shed_id IS NULL
      OR sp.status NOT IN ('pending_verification', 'completed')
      OR sp.feed_proof_ref IS NULL
      OR sp.water_proof_ref IS NULL
    )
	)`

const fastingSubmitReplayTaskSQL = `
SELECT ft.campaign_id::text, ft.operator_user_id::text, ft.park_id::text,
       ft.planned_weigh_date::text, ft.weigh_business_date::text,
       COALESCE(p.name, ''), ft.submitted_at
FROM weighing_fasting_tasks ft
LEFT JOIN locations p ON p.tenant_id = ft.tenant_id AND p.location_id = ft.park_id
WHERE ft.tenant_id = $1::uuid AND ft.fasting_task_id = $2::uuid`

const fastingSubmitReplayEvidenceSQL = `
SELECT sp.fasting_shed_id::text, sp.campaign_shed_id::text, sp.shed_label,
       cs.location_id::text, COALESCE(sp.feed_proof_ref::text, ''),
       COALESCE(sp.water_proof_ref::text, ''), sp.status, sp.row_version
FROM weighing_fasting_shed_proofs sp
JOIN weighing_campaign_sheds cs
  ON cs.tenant_id = sp.tenant_id AND cs.campaign_shed_id = sp.campaign_shed_id
WHERE sp.tenant_id = $1::uuid
  AND sp.fasting_task_id = $2::uuid
  AND sp.campaign_shed_id = $3::uuid`

// SubmitFastingShed records ONE shed's pair (maintainer correction #2). The
// last shed's submit stamps the ROUND's submitted_at — the midnight gate's
// only fact — inside the same transaction.
func (r *Repository) SubmitFastingShed(ctx context.Context, cmd domain.SubmitFastingShed) (domain.FastingShedSubmitResult, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.FastingShedSubmitResult{}, err
	}
	defer tx.Rollback(ctx)

	fingerprint := idempotencyFingerprint(cmd)
	if _, _, snapshot, ok, err := r.idempotencyResource(ctx, tx, cmd.TenantID, fastingSubmitAction, cmd.IdempotencyKey, fingerprint); err != nil {
		return domain.FastingShedSubmitResult{}, err
	} else if ok {
		var replay domain.FastingShedCard
		if err := json.Unmarshal(snapshot, &replay); err != nil {
			return domain.FastingShedSubmitResult{}, err
		}
		var campaignID, operatorID, parkID, plannedDate, weighDate, parkName string
		var submittedAt *time.Time
		if err := tx.QueryRow(ctx, fastingSubmitReplayTaskSQL,
			cmd.TenantID, cmd.FastingTaskID).Scan(&campaignID, &operatorID, &parkID, &plannedDate, &weighDate, &parkName, &submittedAt); err != nil {
			return domain.FastingShedSubmitResult{}, err
		}
		var shed domain.FastingShedProof
		err := tx.QueryRow(ctx, fastingSubmitReplayEvidenceSQL,
			cmd.TenantID, cmd.FastingTaskID, cmd.CampaignShedID).Scan(
			&shed.FastingShedID, &shed.CampaignShedID, &shed.ShedLabel, &shed.ShedLocationID,
			&shed.FeedProofRef, &shed.WaterProofRef, &shed.Status, &shed.RowVersion,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.FastingShedSubmitResult{Card: replay, Replayed: true}, nil
		}
		if err != nil {
			return domain.FastingShedSubmitResult{}, err
		}
		task := domain.FastingTask{
			TenantID:            cmd.TenantID,
			FastingTaskID:       cmd.FastingTaskID,
			CampaignID:          campaignID,
			ParkID:              parkID,
			ParkName:            parkName,
			OperatorUserID:      operatorID,
			PlannedWeighDate:    plannedDate,
			WeighBusinessDate:   weighDate,
			RemovalBusinessDate: domain.RemovalBusinessDate(weighDate),
			SubmittedAt:         submittedAt,
		}
		return domain.FastingShedSubmitResult{Card: replay, Evidence: shed, Task: task, Replayed: true}, nil
	}

	var operatorID, campaignID string
	var roundSubmitted bool
	err = tx.QueryRow(ctx, fastingShedLockSQL, cmd.TenantID, cmd.FastingTaskID).Scan(&operatorID, &campaignID, &roundSubmitted)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.FastingShedSubmitResult{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.FastingShedSubmitResult{}, err
	}
	if operatorID != cmd.SubmittedBy {
		return domain.FastingShedSubmitResult{}, ports.ErrFastingNotAssigned
	}

	var displayName, partitionLabel, shedLocationID, shedStatus, priorFeed, priorWater string
	err = tx.QueryRow(ctx, fastingShedTargetSQL, cmd.TenantID, campaignID, cmd.FastingTaskID, cmd.CampaignShedID).
		Scan(&displayName, &partitionLabel, &shedLocationID, &shedStatus, &priorFeed, &priorWater)
	if errors.Is(err, pgx.ErrNoRows) {
		// The shed is not part of this round (or was deselected).
		return domain.FastingShedSubmitResult{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.FastingShedSubmitResult{}, err
	}
	if shedStatus == domain.FastingStatusPendingVerification || shedStatus == domain.FastingStatusCompleted {
		return domain.FastingShedSubmitResult{}, ports.ErrFastingAlreadySubmitted
	}
	// A REWORK re-submit means NEW videos for THIS shed: a rejected clip
	// re-sent — in EITHER slot, swapped included — refuses the submit by name.
	if shedStatus == domain.FastingStatusRework {
		for _, ref := range []string{cmd.FeedProofRef, cmd.WaterProofRef} {
			if ref == priorFeed || ref == priorWater {
				return domain.FastingShedSubmitResult{}, ports.ErrRejectedProofReuse
			}
		}
	}
	if cmd.FeedProofRef == cmd.WaterProofRef {
		return domain.FastingShedSubmitResult{}, ports.ErrFastingProofInvalid
	}
	// One clip cannot prove two removals — not two slots of one shed, and not
	// two SHEDS of the round either. A ref already recorded on a SIBLING shed
	// of this round refuses this submit.
	var reused bool
	if err := tx.QueryRow(ctx, fastingSiblingRefReuseSQL,
		cmd.TenantID, cmd.FastingTaskID, cmd.CampaignShedID,
		[]string{cmd.FeedProofRef, cmd.WaterProofRef}).Scan(&reused); err != nil {
		return domain.FastingShedSubmitResult{}, err
	}
	if reused {
		return domain.FastingShedSubmitResult{}, ports.ErrFastingProofInvalid
	}
	if err := r.validateFastingProofsTx(ctx, tx, cmd.TenantID, []string{cmd.FeedProofRef, cmd.WaterProofRef}); err != nil {
		return domain.FastingShedSubmitResult{}, err
	}

	shedLabel := oploc.OperationalLocation{ShedName: displayName, PartitionLabel: partitionLabel}.Display()
	var fastingShedID string
	var rowVersion int
	if err := tx.QueryRow(ctx, fastingShedUpsertSQL,
		cmd.TenantID, cmd.FastingTaskID, cmd.CampaignShedID, shedLabel,
		cmd.FeedProofRef, cmd.WaterProofRef).Scan(&fastingShedID, &rowVersion); err != nil {
		return domain.FastingShedSubmitResult{}, err
	}

	var roundComplete bool
	if err := tx.QueryRow(ctx, fastingRoundFullyCoveredSQL, cmd.TenantID, campaignID, cmd.FastingTaskID).Scan(&roundComplete); err != nil {
		return domain.FastingShedSubmitResult{}, err
	}
	if roundComplete {
		if _, err := tx.Exec(ctx, fastingSubmitUpdateSQL, cmd.TenantID, cmd.FastingTaskID, cmd.SubmittedBy); err != nil {
			return domain.FastingShedSubmitResult{}, err
		}
	}

	var task domain.FastingTask
	task, err = scanFastingTask(tx.QueryRow(ctx, fastingReadAfterSubmitSQL, cmd.TenantID, cmd.FastingTaskID))
	if err != nil {
		return domain.FastingShedSubmitResult{}, err
	}
	card := domain.FastingShedCard{
		FastingTaskID:       cmd.FastingTaskID,
		CampaignShedID:      cmd.CampaignShedID,
		FastingShedID:       fastingShedID,
		ShedLabel:           shedLabel,
		SubjectLabel:        domain.FastingShedSubjectLabel(shedLabel),
		ParkName:            task.ParkName,
		Status:              domain.FastingStatusPendingVerification,
		FeedProofRef:        cmd.FeedProofRef,
		WaterProofRef:       cmd.WaterProofRef,
		PlannedWeighDate:    task.PlannedWeighDate,
		WeighBusinessDate:   task.WeighBusinessDate,
		RemovalBusinessDate: task.RemovalBusinessDate,
		SubmittedAt:         task.SubmittedAt,
		RowVersion:          rowVersion,
	}
	proofRow := domain.FastingShedProof{
		FastingShedID:  fastingShedID,
		CampaignShedID: cmd.CampaignShedID,
		ShedLabel:      shedLabel,
		ShedLocationID: shedLocationID,
		FeedProofRef:   cmd.FeedProofRef,
		WaterProofRef:  cmd.WaterProofRef,
		Status:         domain.FastingStatusPendingVerification,
		RowVersion:     rowVersion,
	}

	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     cmd.TenantID,
		ActorID:      cmd.SubmittedBy,
		ActorType:    "user",
		Action:       fastingSubmitAction,
		ResourceType: "weighing_fasting_shed",
		ResourceID:   fastingShedID,
		ScopeType:    "weighing.campaign",
		ScopeID:      campaignID,
		AfterState:   card,
		Metadata: map[string]any{
			"campaign_id":            campaignID,
			"campaign_shed_id":       cmd.CampaignShedID,
			"shed_label":             shedLabel,
			"feed_proof_ref":         cmd.FeedProofRef,
			"water_proof_ref":        cmd.WaterProofRef,
			"round_complete":         roundComplete,
			"client_idempotency_key": cmd.IdempotencyKey,
		},
	}); err != nil {
		return domain.FastingShedSubmitResult{}, err
	}
	if err := r.recordIdempotency(ctx, tx, cmd.TenantID, fastingSubmitAction, cmd.IdempotencyKey, fingerprint, "weighing_fasting_shed", fastingShedID, card); err != nil {
		return domain.FastingShedSubmitResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.FastingShedSubmitResult{}, err
	}
	return domain.FastingShedSubmitResult{Card: card, Evidence: proofRow, Task: task}, nil
}
