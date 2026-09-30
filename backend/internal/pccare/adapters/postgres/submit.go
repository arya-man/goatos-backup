package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	platformoutbox "github.com/vgoats/goatos/backend/internal/platform/outbox"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

const (
	pcCarePendingVerificationEventType     = "pc_care.task.pending_verification"
	pcCarePendingVerificationSchemaVersion = "1.0.0"
	pcCarePendingVerificationSchemaRef     = "contracts/jsonschema/domain-event-envelope.schema.json"
	pcCarePendingVerificationTopic         = "pc_care.events"
	pcCarePendingVerificationAggregateType = "pc_care_task"
)

// SubmitTask flips the WHOLE task open|rework -> pending_verification once every scanned
// animal carries its full slot set, in one transaction: readiness check, task flip
// (row_version bump), animal submitted_at stamps, audit, idempotency. It composes the labeled
// media set the ONE verification item carries. It completes NOTHING — the task is completed
// only when a verifier approves (ApplyVerifiedTask).
func (r *Repository) SubmitTask(ctx context.Context, p ports.SubmitTaskParams) (ports.SubmitTaskResult, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ports.SubmitTaskResult{}, fmt.Errorf("pccare: begin submit tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	var (
		category, parkID, shedID, partitionLabel, vaccineLabel, status, workState, plannedDate string
		// gatesRoundID is non-empty ONLY on a round-grain feed & water removal card. It is what
		// tells this submit to read the per-pen evidence table instead of the task-proof slots.
		gatesRoundID string
		rowVersion   int32
	)
	err = tx.QueryRow(ctx, `
SELECT category, park_id::text, coalesce(shed_id::text, ''), coalesce(partition_label, ''),
       coalesce(vaccine_label, ''), status, work_state,
       planned_business_date::text, coalesce(gates_round_id::text, ''), row_version
FROM pc_care_tasks
WHERE tenant_id = $1::uuid AND task_id = $2::uuid
FOR UPDATE`, p.TenantID, p.TaskID).Scan(
		&category, &parkID, &shedID, &partitionLabel, &vaccineLabel, &status, &workState, &plannedDate, &gatesRoundID, &rowVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.SubmitTaskResult{}, ports.ErrNotFound
	}
	if err != nil {
		return ports.SubmitTaskResult{}, fmt.Errorf("pccare: lock task for submit: %w", err)
	}

	// The shed's display NAME for the verifier's subject label — carried on EVERY return path
	// (the feed packing declared-but-never-populated lesson). A per-vaccine stock task has no
	// shed; its verifier subject is the vaccine label instead.
	var shedLocation oploc.OperationalLocation
	if shedID != "" {
		boundShed := sqlbind.MustBind(oploc.ShedScopedLocationSQL, p.TenantID, shedID)
		shedLocation, err = oploc.ResolveShedLocation(ctx, tx.QueryRow(ctx, boundShed.SQL(), boundShed.Args()...))
		if err != nil {
			return ports.SubmitTaskResult{}, fmt.Errorf("pccare: resolve submit shed location: %w", err)
		}
	}

	base := ports.SubmitTaskResult{
		TaskID: p.TaskID, Category: category, ParkID: parkID, ShedID: shedID,
		ShedName: shedLocation.ShedName, PartitionLabel: partitionLabel,
		VaccineLabel:        vaccineLabel,
		PlannedBusinessDate: plannedDate,
	}

	fingerprint := requestFingerprint(p.TaskID)
	reservation, err := reserveIdempotency(ctx, tx, p.TenantID, pcCareSubmitIdemScope, p.IdempotencyKey, fingerprint)
	if err != nil {
		return ports.SubmitTaskResult{}, err
	}
	if !reservation.proceed {
		// Exact replay: echo the current state, enqueue nothing.
		if err := tx.Commit(ctx); err != nil {
			return ports.SubmitTaskResult{}, fmt.Errorf("pccare: commit idempotent submit replay: %w", err)
		}
		committed = true
		result := base
		result.Status = status
		result.RowVersion = rowVersion
		return result, nil
	}

	switch status {
	case domain.StatusPendingVerification, domain.StatusCompleted:
		// A different-key re-send of an already-submitted task: the work IS submitted, so this
		// is an idempotent no-op (feed packing same-proof precedent), never an error and never
		// a second enqueue.
		if err := completeIdempotency(ctx, tx, p.TenantID, pcCareSubmitIdemScope, p.IdempotencyKey, pcCareTaskResourceType, p.TaskID); err != nil {
			return ports.SubmitTaskResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return ports.SubmitTaskResult{}, fmt.Errorf("pccare: commit already-submitted no-op: %w", err)
		}
		committed = true
		result := base
		result.Status = status
		result.RowVersion = rowVersion
		return result, nil
	case domain.StatusOpen, domain.StatusRework:
		// The submitting states.
	default:
		return ports.SubmitTaskResult{}, fmt.Errorf("pccare: unexpected task status %q", status)
	}
	if workState != domain.WorkStateScheduled && workState != domain.WorkStateDelayed {
		return ports.SubmitTaskResult{}, domain.ErrTaskNotOpen
	}

	var animalCount int
	var mediaRefs []ports.LabeledRef
	var removalPens []ports.RemovalPenRef
	switch {
	case gatesRoundID != "":
		// A ROUND-grain feed & water removal (maintainer decision 2026-09-05): the evidence is
		// one feed video + one water video PER PEN, and EVERY pen must carry both. A card
		// submitted with one pen unfilmed would tell the midnight gate that pen's animals were
		// fasted when they were not.
		var err error
		removalPens, mediaRefs, err = removalPenSubmitRefs(ctx, tx, p.TenantID, p.TaskID, removalSlotsOrSeeded(p.RemovalSlots))
		if err != nil {
			return ports.SubmitTaskResult{}, err
		}
	case domain.CaptureModeForCategory(category) == domain.CaptureModeTaskProof:
		var err error
		if category == domain.CategoryInventoryVaccine {
			var requirementCount int
			if err := tx.QueryRow(ctx, `
SELECT count(*)::int
FROM pc_care_task_inventory_requirements
WHERE tenant_id = $1::uuid AND task_id = $2::uuid AND required_doses > 0`,
				p.TenantID, p.TaskID).Scan(&requirementCount); err != nil {
				return ports.SubmitTaskResult{}, fmt.Errorf("pccare: inventory requirement count: %w", err)
			}
			if requirementCount == 0 {
				return ports.SubmitTaskResult{}, domain.ErrProofIncomplete
			}
		}
		// Every declared slot must carry its proof — for feed_water_removal, BOTH the feed
		// removal video and the water removal video.
		mediaRefs, animalCount, err = r.taskProofMediaRefs(ctx, tx, p.TenantID, p.TaskID, category, taskProofSlotsOrSeeded(category, p.RemovalSlots))
		if err != nil {
			return ports.SubmitTaskResult{}, err
		}
	default:
		// Readiness: every scanned animal must carry every COMPULSORY slot of the task's pinned
		// card -- the keys snapshotted on the task row at create (PC CARE SOP, 2026-09-22). ONE
		// bounded set-based count per submit (a write, not a list).
		// Resolve what would prove this task BEFORE asking the database whether it is proven.
		// `sop_proofs ?& '{}'` is vacuously TRUE, so an unresolvable card reaching the predicate
		// would count every animal complete; refusing here is the difference between a task that
		// cannot be submitted and one that submits with no evidence at all. A task created since
		// this rule exists can never land here -- the create refuses first -- so this covers the
		// rows that predate it.
		requiredKeys, err := requiredKeysOrSeeded(category, p.RequiredSlotKeys)
		if err != nil {
			return ports.SubmitTaskResult{}, err
		}
		var missingCount int
		if err := tx.QueryRow(ctx, `
SELECT count(*)::int,
       count(*) FILTER (WHERE NOT (an.sop_proofs ?& coalesce(nullif(t.required_slot_keys, '{}'::text[]), $3::text[])))::int
FROM pc_care_task_animals an
JOIN pc_care_tasks t ON t.tenant_id = an.tenant_id AND t.task_id = an.task_id
WHERE an.tenant_id = $1::uuid AND an.task_id = $2::uuid`,
			p.TenantID, p.TaskID, requiredKeys,
		).Scan(&animalCount, &missingCount); err != nil {
			return ports.SubmitTaskResult{}, fmt.Errorf("pccare: submit readiness count: %w", err)
		}
		if animalCount == 0 {
			return ports.SubmitTaskResult{}, domain.ErrNoAnimals
		}
		if missingCount > 0 {
			return ports.SubmitTaskResult{}, domain.ErrProofIncomplete
		}
	}

	if err := tx.QueryRow(ctx, `
UPDATE pc_care_tasks
SET status = 'pending_verification',
    submitted_by = nullif($3::text, '')::uuid,
    submitted_at = now(),
    rework_reason = NULL,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND task_id = $2::uuid AND status IN ('open', 'rework')
RETURNING row_version`, p.TenantID, p.TaskID, p.SubmittedBy).Scan(&rowVersion); err != nil {
		return ports.SubmitTaskResult{}, fmt.Errorf("pccare: flip task pending: %w", err)
	}

	if _, err := tx.Exec(ctx, `
UPDATE pc_care_task_animals
SET submitted_at = now(), updated_at = now()
WHERE tenant_id = $1::uuid AND task_id = $2::uuid`, p.TenantID, p.TaskID); err != nil {
		return ports.SubmitTaskResult{}, fmt.Errorf("pccare: stamp animal submits: %w", err)
	}

	if gatesRoundID == "" && domain.CaptureModeForCategory(category) != domain.CaptureModeTaskProof {
		var err error
		mediaRefs, err = r.composeSubmitMediaRefs(ctx, tx, p.TenantID, p.TaskID, captureSlotsOrSeeded(category, p.Slots))
		if err != nil {
			return ports.SubmitTaskResult{}, err
		}
	}
	// The answers given at submit are stored on the task beside the pin they were judged by.
	answersJSON, err := json.Marshal(p.Answers)
	if err != nil {
		return ports.SubmitTaskResult{}, fmt.Errorf("pccare: encode sop answers: %w", err)
	}
	if p.Answers == nil {
		answersJSON = []byte("{}")
	}
	if _, err := tx.Exec(ctx, `
UPDATE pc_care_tasks SET sop_answers = $3::jsonb WHERE tenant_id = $1::uuid AND task_id = $2::uuid`,
		p.TenantID, p.TaskID, answersJSON); err != nil {
		return ports.SubmitTaskResult{}, fmt.Errorf("pccare: store sop answers: %w", err)
	}

	actorType := strings.TrimSpace(p.ActorType)
	if actorType == "" {
		actorType = "operator"
	}
	// A per-vaccine stock task has no shed; its audit scope is the park.
	submitScopeType, submitScopeID := "shed", shedID
	if shedID == "" {
		submitScopeType, submitScopeID = "park", parkID
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     p.TenantID,
		ActorID:      p.ActorID,
		ActorType:    actorType,
		Action:       pcCarePendingAction,
		ResourceType: pcCareTaskResourceType,
		ResourceID:   p.TaskID,
		ScopeType:    submitScopeType,
		ScopeID:      submitScopeID,
		AfterState: map[string]any{
			"category":              category,
			"park_id":               parkID,
			"shed_id":               shedID,
			"partition_label":       partitionLabel,
			"vaccine_label":         vaccineLabel,
			"planned_business_date": plannedDate,
			"status":                domain.StatusPendingVerification,
			"animal_count":          animalCount,
		},
		Metadata: map[string]any{"source": "pc-care-submit"},
		TraceID:  p.TraceID,
	}); err != nil {
		return ports.SubmitTaskResult{}, fmt.Errorf("pccare: write submit audit: %w", err)
	}

	if err := completeIdempotency(ctx, tx, p.TenantID, pcCareSubmitIdemScope, p.IdempotencyKey, pcCareTaskResourceType, p.TaskID); err != nil {
		return ports.SubmitTaskResult{}, fmt.Errorf("pccare: complete submit idempotency: %w", err)
	}
	if err := insertPendingVerificationOutbox(ctx, tx, pendingVerificationOutbox{
		TenantID:            p.TenantID,
		TaskID:              p.TaskID,
		Category:            category,
		ParkID:              parkID,
		ShedID:              shedID,
		ShedName:            shedLocation.ShedName,
		PartitionLabel:      partitionLabel,
		VaccineLabel:        vaccineLabel,
		PlannedBusinessDate: plannedDate,
		MediaRefs:           mediaRefs,
		AnimalCount:         int32(animalCount),
		RemovalPens:         removalPens,
		AnswerRows:          p.AnswerRows,
		OperatorID:          p.SubmittedBy,
		RowVersion:          rowVersion,
		OccurredAt:          p.Now,
		TraceID:             p.TraceID,
	}); err != nil {
		return ports.SubmitTaskResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ports.SubmitTaskResult{}, fmt.Errorf("pccare: commit submit: %w", err)
	}
	committed = true

	result := base
	result.Status = domain.StatusPendingVerification
	result.RowVersion = rowVersion
	result.NewlyPending = true
	result.MediaRefs = mediaRefs
	result.AnimalCount = int32(animalCount)
	return result, nil
}

// removalSlotsOrSeeded / captureSlotsOrSeeded / requiredKeysOrSeeded are the submit's FAIL-CLOSED
// rule, the twin of the create's slotKeysOrSeeded: a caller that hands the store no card runs the
// SEEDED one (version 0), never a card that asks for nothing. Without this a direct store submit
// would pass readiness vacuously and reach the verifier with no media at all.
func removalSlotsOrSeeded(slots []authored.ProofSlot) []authored.ProofSlot {
	if len(slots) > 0 {
		return slots
	}
	return domain.SeededRules().RemovalProofs()
}

// taskProofSlotsOrSeeded is the task-level card a task_proof submit is judged by: the service's
// pinned slots when it passed them, otherwise the SEEDED card of that task's kind -- the pen card
// for fumigation, the removal card otherwise -- never a card that demands nothing.
func taskProofSlotsOrSeeded(category string, slots []authored.ProofSlot) []authored.ProofSlot {
	if len(slots) > 0 {
		return slots
	}
	if domain.IsPenProofCategory(category) {
		return domain.SeededRules().Category(category).ProofSlots()
	}
	return removalSlotsOrSeeded(nil)
}

func seededRemovalSlots() []domain.Slot {
	out := []domain.Slot{}
	for _, p := range domain.SeededRules().RemovalProofs() {
		out = append(out, domain.Slot{FieldKey: p.Key, Label: p.Title, Description: p.Hint, Kind: p.Kind, Required: p.Required})
	}
	return out
}

func captureSlotsOrSeeded(category string, slots []domain.Slot) []domain.Slot {
	if len(slots) > 0 {
		return slots
	}
	return domain.SeededRules().CategorySlots(category)
}

func requiredKeysOrSeeded(category string, keys []string) ([]string, error) {
	return slotKeysOrSeeded(category, keys, true)
}

type pendingVerificationOutbox struct {
	TenantID            string
	TaskID              string
	Category            string
	ParkID              string
	ShedID              string
	ShedName            string
	PartitionLabel      string
	VaccineLabel        string
	PlannedBusinessDate string
	MediaRefs           []ports.LabeledRef
	AnimalCount         int32
	// RemovalPens is set only on a round-grain removal submit; the consumer fans it out into
	// ONE verifier item per pen.
	RemovalPens []ports.RemovalPenRef
	// AnswerRows are the operators' answers in farm words, for the verifier's context rows.
	AnswerRows []authored.AnswerRow
	OperatorID string
	RowVersion int32
	OccurredAt time.Time
	TraceID    string
}

func insertPendingVerificationOutbox(ctx context.Context, tx pgx.Tx, o pendingVerificationOutbox) error {
	idempotencyKey := "pc-care-verification:" + o.TaskID + ":" + strconv.Itoa(int(o.RowVersion))
	eventID := platformoutbox.DeterministicUUID(pcCarePendingVerificationEventType + ":" + o.TenantID + ":" + idempotencyKey)
	media := make([]map[string]string, 0, len(o.MediaRefs))
	for _, ref := range o.MediaRefs {
		if strings.TrimSpace(ref.ProofRef) == "" {
			continue
		}
		media = append(media, map[string]string{"proof_ref": ref.ProofRef, "label": ref.Label, "kind": ref.Kind})
	}
	// One entry per pen on a round-grain removal, so the consumer can mint ONE verifier item
	// per pen. Absent on every other submit, which keeps those payloads byte-identical to
	// what they were before rounds existed.
	answerRows := make([]map[string]string, 0, len(o.AnswerRows))
	for _, row := range o.AnswerRows {
		answerRows = append(answerRows, map[string]string{"label": row.Title, "value": row.Value})
	}
	removalPens := make([]map[string]any, 0, len(o.RemovalPens))
	for _, pen := range o.RemovalPens {
		penProofs := make([]map[string]string, 0, len(pen.Proofs))
		for _, proof := range pen.Proofs {
			penProofs = append(penProofs, map[string]string{"proof_ref": proof.ProofRef, "label": proof.Label, "kind": proof.Kind})
		}
		removalPens = append(removalPens, map[string]any{
			"proofs":          penProofs,
			"removal_pen_id":  pen.RemovalPenID,
			"gated_task_id":   pen.GatedTaskID,
			"pen_label":       pen.PenLabel,
			"feed_proof_ref":  pen.FeedProofRef,
			"water_proof_ref": pen.WaterProofRef,
			"row_version":     pen.RowVersion,
		})
	}
	payload := map[string]any{
		"task_id":               o.TaskID,
		"category":              o.Category,
		"park_id":               o.ParkID,
		"shed_id":               o.ShedID,
		"shed_name":             o.ShedName,
		"partition_label":       o.PartitionLabel,
		"vaccine_label":         o.VaccineLabel,
		"planned_business_date": o.PlannedBusinessDate,
		"media_refs":            media,
		"animal_count":          o.AnimalCount,
		"operator_id":           o.OperatorID,
		"row_version":           o.RowVersion,
	}
	if len(removalPens) > 0 {
		payload["removal_pens"] = removalPens
	}
	// The operators' answers to the pinned card's questions, in farm words, so the verifier
	// reads them beside the captures they explain. Absent on a card that asks none, which keeps
	// the payload byte-identical to what it was before the SOP existed.
	if len(answerRows) > 0 {
		payload["context_rows"] = answerRows
	}
	envelope := pcCareEventEnvelope{
		EventID:        eventID,
		EventType:      pcCarePendingVerificationEventType,
		SchemaVersion:  pcCarePendingVerificationSchemaVersion,
		SchemaRef:      pcCarePendingVerificationSchemaRef,
		AggregateType:  pcCarePendingVerificationAggregateType,
		AggregateID:    o.TaskID,
		IdempotencyKey: idempotencyKey,
		TenantID:       o.TenantID,
		ParkID:         o.ParkID,
		ShedID:         o.ShedID,
		ActorID:        o.OperatorID,
		OccurredAt:     o.OccurredAt,
		Payload:        payload,
		TraceID:        o.TraceID,
	}.build()
	envelopeJSON, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("pccare: marshal pending verification envelope: %w", err)
	}
	headersJSON, err := json.Marshal(map[string]any{"content_type": "application/json"})
	if err != nil {
		return fmt.Errorf("pccare: marshal pending verification headers: %w", err)
	}
	_, err = tx.Exec(ctx, `
INSERT INTO outbox_messages (
  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, trace_id, status, next_attempt_at
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5, $6::uuid,
  $7, $8::jsonb, $9::jsonb, $10, $11, 'pending', now()
)
ON CONFLICT DO NOTHING`,
		o.TenantID, eventID, pcCarePendingVerificationEventType, pcCarePendingVerificationSchemaVersion,
		pcCarePendingVerificationAggregateType, o.TaskID, pcCarePendingVerificationTopic,
		envelopeJSON, headersJSON, idempotencyKey, o.TraceID)
	if err != nil {
		return fmt.Errorf("pccare: insert pending verification outbox: %w", err)
	}
	return nil
}

// composeSubmitMediaRefs builds the verification item's labeled media set: every animal's
// captures in scan order then the PINNED card's slot order, each labeled "<tag> · <slot title>"
// with the kind the register judged it, so the verifier can tell which animal and which step
// every capture proves. One bounded read of one task's rows.
func (r *Repository) composeSubmitMediaRefs(ctx context.Context, tx pgx.Tx, tenantID, taskID string, slots []domain.Slot) ([]ports.LabeledRef, error) {
	rows, err := tx.Query(ctx, `
SELECT scanned_identifier, sop_proofs, sop_proof_meta
FROM pc_care_task_animals
WHERE tenant_id = $1::uuid AND task_id = $2::uuid
ORDER BY scanned_at, animal_row_id`, tenantID, taskID)
	if err != nil {
		return nil, fmt.Errorf("pccare: read submit media refs: %w", err)
	}
	defer rows.Close()

	out := make([]ports.LabeledRef, 0, 64)
	for rows.Next() {
		var tag string
		var proofsJSON, metaJSON []byte
		if err := rows.Scan(&tag, &proofsJSON, &metaJSON); err != nil {
			return nil, fmt.Errorf("pccare: scan submit media row: %w", err)
		}
		var byKey map[string]string
		if err := json.Unmarshal(proofsJSON, &byKey); err != nil {
			return nil, fmt.Errorf("pccare: decode submit media proofs: %w", err)
		}
		var meta map[string]struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(metaJSON, &meta); err != nil {
			return nil, fmt.Errorf("pccare: decode submit media meta: %w", err)
		}
		for _, slot := range slots {
			ref := byKey[slot.FieldKey]
			if ref == "" {
				continue
			}
			kind := meta[slot.FieldKey].Kind
			if kind == "" && slot.Kind != authored.KindEither {
				kind = slot.Kind
			}
			out = append(out, ports.LabeledRef{ProofRef: ref, Label: tag + " · " + slot.Label, Kind: kind})
		}
	}
	return out, rows.Err()
}
