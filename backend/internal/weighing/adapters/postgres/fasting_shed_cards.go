package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	fwrports "github.com/vgoats/goatos/backend/internal/feedwaterremoval/ports"
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
       COALESCE(sp.row_version, 0),
       COALESCE(c.sop_version, 0),
       COALESCE(sp.sop_answers, '{}'::jsonb),
       COALESCE(sp.sop_proofs, '{}'::jsonb)
FROM weighing_fasting_tasks ft
JOIN weighing_campaign_sheds cs
  ON cs.tenant_id = ft.tenant_id AND cs.campaign_id = ft.campaign_id AND cs.status <> 'canceled'
-- The task's pinned SOP version (WEIGHING SOP): weighing_campaigns is keyed on
-- (tenant_id, campaign_id), so this join is exactly 1:1 and never multiplies rows.
JOIN weighing_campaigns c
  ON c.tenant_id = ft.tenant_id AND c.campaign_id = ft.campaign_id
LEFT JOIN weighing_fasting_shed_proofs sp
  ON sp.tenant_id = ft.tenant_id AND sp.fasting_task_id = ft.fasting_task_id
       AND sp.campaign_shed_id = cs.campaign_shed_id
LEFT JOIN locations p
  ON p.tenant_id = ft.tenant_id AND p.location_id = ft.park_id
WHERE ft.tenant_id = $1::uuid
  AND ft.operator_user_id = $2::uuid
  -- The window opens at the evening of the task's PINNED SOP version ($8, jsonb keyed by
  -- version, bound by the service), else the farm default ($7). Bind, never a join: the
  -- rules and the config live outside weighing.
  AND ($3::timestamptz AT TIME ZONE 'Asia/Kolkata') >= ((ft.weigh_business_date - 1) + COALESCE(($8::jsonb ->> COALESCE(c.sop_version, 0)::text)::time, $7::time))
  AND (ft.submitted_at IS NOT NULL OR EXISTS (
        SELECT 1 FROM weighing_campaigns c
        WHERE c.tenant_id = ft.tenant_id AND c.campaign_id = ft.campaign_id
          AND c.status NOT IN ('completed','closed','canceled')
      ))
  AND ($4::date IS NULL OR ft.weigh_business_date < $4::date
       OR (ft.weigh_business_date = $4::date AND cs.campaign_shed_id < $5::uuid))
ORDER BY ft.weigh_business_date DESC, cs.campaign_shed_id DESC
LIMIT $6`

// fastingCardSOPVersionsSQL lists the distinct versions the operator's candidate rounds are
// pinned to: a handful of small integers, bounded by the number of versions ever published.
const fastingCardSOPVersionsSQL = `
SELECT DISTINCT COALESCE(c.sop_version, 0)
FROM weighing_fasting_tasks ft
JOIN weighing_campaigns c
  ON c.tenant_id = ft.tenant_id AND c.campaign_id = ft.campaign_id
WHERE ft.tenant_id = $1::uuid
  AND ft.operator_user_id = $2::uuid
  AND (ft.submitted_at IS NOT NULL OR c.status NOT IN ('completed','closed','canceled'))`

// FastingCardSOPVersions implements ports.FastingStore.
func (r *Repository) FastingCardSOPVersions(ctx context.Context, tenantID, operatorUserID string) ([]int, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	rows, err := r.pool.Query(ctx, fastingCardSOPVersionsSQL, tenantID, operatorUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	versions := []int{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		versions = append(versions, v)
	}
	return versions, rows.Err()
}

// The visibility window's opening time is the removal cutoff of each card's PINNED SOP
// version, else the tenant's CONFIGURED cutoff (maintainer decision 2026-09-07), bound as
// $8 / $7 by the service. Weighing is isolated from every non-weighing table, so the rules
// and the config are binds, never joins here; an unset default is refused rather than
// defaulted, because a literal here would be a second copy of the rule.
func (r *Repository) ListFastingShedCardsForOperator(ctx context.Context, tenantID, operatorUserID string, now time.Time, cutoffs ports.RemovalCutoffs, cursor string, limit int) (domain.FastingShedCardPage, error) {
	if !cutoffs.Default.Valid() {
		return domain.FastingShedCardPage{}, fwrports.ErrCutoffNotConfigured
	}
	byVersion, err := json.Marshal(cutoffs.SQLByVersion())
	if err != nil {
		return domain.FastingShedCardPage{}, err
	}
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
		nullableString(cur.Date), nullableUUIDString(cur.ID), limit+1,
		cutoffs.Default.SQLTime(), string(byVersion))
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
			&card.PlannedWeighDate, &card.WeighBusinessDate, &submittedAt, &card.RowVersion,
			&card.SOPVersion, &card.Answers, &card.ProofRefs); err != nil {
			return domain.FastingShedCardPage{}, err
		}
		if len(card.Answers) == 0 {
			card.Answers = nil
		}
		if len(card.ProofRefs) == 0 {
			card.ProofRefs = nil
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
       COALESCE(sp.feed_proof_ref::text, ''), COALESCE(sp.water_proof_ref::text, ''),
       COALESCE(sp.sop_proofs, '{}'::jsonb)
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
    AND (
      feed_proof_ref = ANY($4::uuid[]) OR water_proof_ref = ANY($4::uuid[])
      OR EXISTS (SELECT 1 FROM jsonb_each_text(COALESCE(sop_proofs, '{}'::jsonb)) e WHERE e.value = ANY($4::text[]))
    )
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
       COALESCE(sp.water_proof_ref::text, ''), sp.status, sp.row_version,
       COALESCE(sp.sop_proofs, '{}'::jsonb)
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
			&shed.FeedProofRef, &shed.WaterProofRef, &shed.Status, &shed.RowVersion, &shed.Proofs,
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
	var priorProofs domain.RemovalProofRefs
	err = tx.QueryRow(ctx, fastingShedTargetSQL, cmd.TenantID, campaignID, cmd.FastingTaskID, cmd.CampaignShedID).
		Scan(&displayName, &partitionLabel, &shedLocationID, &shedStatus, &priorFeed, &priorWater, &priorProofs)
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
	// The captures, in slot order (WEIGHING SOP: the service resolved the slots from the
	// task's pinned rules). A store called directly with only the legacy pair still works.
	refs := cmd.OrderedRefs
	if len(refs) == 0 {
		for _, ref := range []string{cmd.FeedProofRef, cmd.WaterProofRef} {
			if ref != "" {
				refs = append(refs, ref)
			}
		}
	}
	if len(refs) == 0 {
		return domain.FastingShedSubmitResult{}, ports.ErrFastingProofRequired
	}
	// A REWORK re-submit means NEW captures for THIS shed: a rejected clip
	// re-sent — in ANY slot, swapped included — refuses the submit by name.
	if shedStatus == domain.FastingStatusRework {
		prior := map[string]bool{priorFeed: true, priorWater: true}
		for _, ref := range priorProofs {
			prior[ref] = true
		}
		for _, ref := range refs {
			if ref != "" && prior[ref] {
				return domain.FastingShedSubmitResult{}, ports.ErrRejectedProofReuse
			}
		}
	}
	seenRef := map[string]bool{}
	for _, ref := range refs {
		if seenRef[ref] {
			// One capture cannot prove two slots.
			return domain.FastingShedSubmitResult{}, ports.ErrFastingProofInvalid
		}
		seenRef[ref] = true
	}
	// One capture cannot prove two SHEDS of the round either. A ref already
	// recorded on a SIBLING shed of this round refuses this submit.
	var reused bool
	if err := tx.QueryRow(ctx, fastingSiblingRefReuseSQL,
		cmd.TenantID, cmd.FastingTaskID, cmd.CampaignShedID, refs).Scan(&reused); err != nil {
		return domain.FastingShedSubmitResult{}, err
	}
	if reused {
		return domain.FastingShedSubmitResult{}, ports.ErrFastingProofInvalid
	}
	if err := r.validateFastingProofsTx(ctx, tx, cmd.TenantID, expectedProofKinds(cmd)); err != nil {
		return domain.FastingShedSubmitResult{}, err
	}

	shedLabel := oploc.OperationalLocation{ShedName: displayName, PartitionLabel: partitionLabel}.Display()
	var fastingShedID string
	var rowVersion int
	answers := cmd.Answers
	if answers == nil {
		answers = domain.SOPAnswers{}
	}
	answersJSON, err := json.Marshal(answers)
	if err != nil {
		return domain.FastingShedSubmitResult{}, err
	}
	proofs := domain.NormalizeRemovalProofRefs(cmd.Proofs)
	if len(proofs) == 0 {
		// Legacy pair only: stored under the seeded slot keys so every reader sees one shape.
		if cmd.FeedProofRef != "" {
			proofs[domain.RemovalProofFeed] = cmd.FeedProofRef
		}
		if cmd.WaterProofRef != "" {
			proofs[domain.RemovalProofWater] = cmd.WaterProofRef
		}
	}
	proofsJSON, err := json.Marshal(proofs)
	if err != nil {
		return domain.FastingShedSubmitResult{}, err
	}
	if err := tx.QueryRow(ctx, fastingShedUpsertSQL,
		cmd.TenantID, cmd.FastingTaskID, cmd.CampaignShedID, shedLabel,
		nullableUUIDString(cmd.FeedProofRef), nullableUUIDString(cmd.WaterProofRef), answersJSON, proofsJSON).Scan(&fastingShedID, &rowVersion); err != nil {
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
		Answers:             cmd.Answers,
		ProofRefs:           proofs,
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
		Answers:        cmd.Answers,
		Proofs:         proofs,
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
			"sop_answers":            answers,
			"sop_proofs":             proofs,
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

// expectedProofKinds pairs every capture with the kind its slot accepts (video / photo /
// either), for the register check. A legacy pair, or a slot the service did not name, expects a
// video -- the pre-SOP rule.
func expectedProofKinds(cmd domain.SubmitFastingShed) map[string]string {
	out := map[string]string{}
	for key, ref := range cmd.Proofs {
		if strings.TrimSpace(ref) == "" {
			continue
		}
		kind := cmd.SlotKinds[key]
		if kind == "" {
			kind = domain.RemovalProofKindVideo
		}
		out[strings.TrimSpace(ref)] = kind
	}
	for _, ref := range []string{cmd.FeedProofRef, cmd.WaterProofRef} {
		if ref != "" {
			if _, ok := out[ref]; !ok {
				out[ref] = domain.RemovalProofKindVideo
			}
		}
	}
	return out
}
