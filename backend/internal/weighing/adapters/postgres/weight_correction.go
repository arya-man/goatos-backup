package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// VERIFIER WEIGHT CORRECTION -- the write side.
//
// One transaction does all of it: overwrite the live weight, preserve the
// operator's original values the FIRST time only, recompute the lump-sum average,
// write the audit row, record the idempotency snapshot, and enqueue the domain
// event. There is deliberately no post-commit step, because a correction that
// changed the number but lost its audit row would be the one kind of failure this
// feature cannot tolerate: a weight nobody can explain.
// NO DOMAIN EVENT IS PUBLISHED, deliberately.
//
// A producer with no consumer is a silent drop (AGENTS.md: "Register BOTH ends, every
// time"), and nothing consumes a weight correction: the corrected value is written to
// the live column every read model already reads, and the verification item's stale
// label is restated SYNCHRONOUSLY through verification's own relabel seam rather than
// asynchronously off a bus. The durable trail is the audit row below, which carries
// both the before and after state.
//
// If a future decision makes a correction NOTIFY somebody -- leadership told a weight
// was changed, say -- that is the moment to add the event, together with the consumer
// that reads it. Adding the producer now, against no consumer, would read to the next
// author as already wired.
const idempotencyEventWeightCorrected = "weighing.observation_weight_corrected"

// correctionScope is the pre-correction snapshot read under the row lock. Every
// value the result and the audit trail need is taken from it, so nothing is
// re-read after the UPDATE and the "before" state cannot drift from what was
// actually replaced.
type correctionScope struct {
	CampaignID     string
	CampaignShedID string
	ShedDisplay    string
	// ScannedIdentifier is the raw tag on an individual capture, "" for lump-sum.
	// It is only used to recompose the label; nothing resolves it to an animal.
	ScannedIdentifier string
	WeightKg          float64
	AnimalCount       int
	// OperatorWeightKg / OperatorAnimalCount are NULL until the first correction.
	// Their presence is exactly "this row has been corrected before".
	OperatorWeightKg    *float64
	OperatorAnimalCount *int
	BucketStatus        string
	Withdrawn           bool
}

// CorrectObservationWeight replaces the weight a verifier judged to be wrong.
//
// The corrected value REPLACES the recorded one in place: weight_kg is the live
// truth every read model already reads, and shadowing it behind an override column
// would mean every one of those reads had to learn about corrections. What is kept
// instead is the OPERATOR'S ORIGINAL, written once and never overwritten, so a
// second correction cannot erase what the operator actually entered.
func (r *Repository) CorrectObservationWeight(ctx context.Context, cmd domain.WeightCorrectionCommand) (domain.WeightCorrectionResult, error) {
	cmd, code := domain.ValidateWeightCorrection(cmd)
	if code != "" {
		return domain.WeightCorrectionResult{}, ports.ErrInvalidArgument
	}

	ctx, cancel := r.timeout(ctx)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.WeightCorrectionResult{}, err
	}
	defer tx.Rollback(ctx)

	// Every value that CHANGES the outcome is in the fingerprint, so an exact
	// client retry replays for free while the same key carrying a different weight
	// is refused as the contradiction it is rather than answered from cache with a
	// number it never wrote.
	fingerprint := idempotencyFingerprint(map[string]any{
		"observation_id": cmd.ObservationID,
		"ref_type":       cmd.RefType,
		"weight_kg":      cmd.WeightKg,
		"animal_count":   cmd.AnimalCount,
		"reason":         cmd.Reason,
		"corrected_by":   cmd.CorrectedBy,
	})
	if result, ok, err := r.weightCorrectionByIdempotency(ctx, tx, cmd, fingerprint); err != nil || ok {
		if err != nil {
			return domain.WeightCorrectionResult{}, err
		}
		return result, tx.Commit(ctx)
	}

	scope, err := r.lockCorrectionScope(ctx, tx, cmd)
	if err != nil {
		return domain.WeightCorrectionResult{}, err
	}
	// A closed bucket is settled work. Correcting a weight inside it would move a
	// number that has already been reported as final, so the answer is to REOPEN
	// the bucket (a monitor authority) and correct it then -- never to write
	// through the closure.
	if scope.BucketStatus == domain.StatusClosed || scope.BucketStatus == "canceled" {
		return domain.WeightCorrectionResult{}, ports.ErrCorrectionAfterClose
	}
	// A withdrawn lump-sum submission is a superseded record that no longer
	// represents the bucket's work; its verification item was retired with it.
	// Correcting it would edit a row nothing reads and report success for a change
	// no screen will ever show.
	if scope.Withdrawn {
		return domain.WeightCorrectionResult{}, ports.ErrImmutable
	}

	animalCount := scope.AnimalCount
	if cmd.RefType == domain.VerificationRefTypeShed && cmd.AnimalCount > 0 {
		animalCount = cmd.AnimalCount
	}

	correctedAt, err := r.applyWeightCorrection(ctx, tx, cmd, scope, animalCount)
	if err != nil {
		return domain.WeightCorrectionResult{}, err
	}

	// The operator's original survives every later correction: it is written on the
	// first one and left alone afterwards, so this reads the stored value when one
	// exists and the pre-correction value only when this IS the first correction.
	operatorWeight := scope.WeightKg
	if scope.OperatorWeightKg != nil {
		operatorWeight = *scope.OperatorWeightKg
	}
	operatorCount := scope.AnimalCount
	if scope.OperatorAnimalCount != nil {
		operatorCount = *scope.OperatorAnimalCount
	}

	result := domain.WeightCorrectionResult{
		ObservationID:    cmd.ObservationID,
		RefType:          cmd.RefType,
		CampaignID:       scope.CampaignID,
		CampaignShedID:   scope.CampaignShedID,
		WeightKg:         cmd.WeightKg,
		PreviousWeightKg: scope.WeightKg,
		OperatorWeightKg: operatorWeight,
		Reason:           cmd.Reason,
		CorrectedBy:      cmd.CorrectedBy,
		SubjectLabel:     domain.CorrectedSubjectLabel(cmd.RefType, scope.ShedDisplay, scope.ScannedIdentifier, cmd.WeightKg, animalCount),
		// India business time: every Goat OS business meaning derives from
		// Asia/Kolkata, never UTC (AGENTS.md). Recorded into the idempotency
		// snapshot below so a replay returns this same instant verbatim.
		CorrectedAt: correctedAt.In(biztime.DefaultLocation()),
	}
	if cmd.RefType == domain.VerificationRefTypeShed {
		result.AnimalCount = animalCount
		result.PreviousAnimalCount = scope.AnimalCount
		result.OperatorAnimalCount = operatorCount
		result.AverageWeightKg = domain.RecomputeAverageWeightKg(cmd.WeightKg, animalCount)
	}

	if err := r.auditWeightCorrection(ctx, tx, cmd, scope, result); err != nil {
		return domain.WeightCorrectionResult{}, err
	}
	if err := r.recordIdempotency(
		ctx, tx, cmd.TenantID, idempotencyEventWeightCorrected, cmd.IdempotencyKey, fingerprint,
		cmd.RefType, cmd.ObservationID, result,
	); err != nil {
		return domain.WeightCorrectionResult{}, err
	}
	return result, tx.Commit(ctx)
}

func (r *Repository) weightCorrectionByIdempotency(
	ctx context.Context,
	tx pgx.Tx,
	cmd domain.WeightCorrectionCommand,
	fingerprint string,
) (domain.WeightCorrectionResult, bool, error) {
	_, resourceType, snapshot, ok, err := r.idempotencyResource(
		ctx, tx, cmd.TenantID, idempotencyEventWeightCorrected, cmd.IdempotencyKey, fingerprint,
	)
	if err != nil || !ok {
		return domain.WeightCorrectionResult{}, ok, err
	}
	// The same key naming the other grain is a different request, not a replay.
	if resourceType != cmd.RefType {
		return domain.WeightCorrectionResult{}, true, ports.ErrIdempotencyConflict
	}
	var result domain.WeightCorrectionResult
	if len(snapshot) > 0 {
		if err := json.Unmarshal(snapshot, &result); err != nil {
			return domain.WeightCorrectionResult{}, true, err
		}
	}
	return result, true, nil
}

// lockCorrectionScope takes the observation's row lock and reads everything the
// correction needs in ONE indexed primary-key lookup, so the pre-correction
// snapshot and the UPDATE cannot straddle a concurrent verdict or re-capture.
func (r *Repository) lockCorrectionScope(ctx context.Context, tx pgx.Tx, cmd domain.WeightCorrectionCommand) (correctionScope, error) {
	var scope correctionScope
	var query string
	switch cmd.RefType {
	case domain.VerificationRefTypeAnimal:
		query = `
SELECT observation.campaign_id::text,
  COALESCE(observation.campaign_shed_id::text, ''),
  COALESCE(cs.display_name, ''),
  COALESCE(observation.scanned_identifier, ''),
  observation.weight_kg,
  0,
  observation.operator_weight_kg,
  NULL::integer,
  COALESCE(cs.status, ''),
  false
FROM weighing_observations observation
LEFT JOIN weighing_campaign_sheds cs
  ON cs.tenant_id=observation.tenant_id
 AND cs.campaign_shed_id=observation.campaign_shed_id
WHERE observation.tenant_id=$1::uuid
  AND observation.observation_id=$2::uuid
FOR UPDATE OF observation`
	case domain.VerificationRefTypeShed:
		query = `
SELECT observation.campaign_id::text,
  observation.campaign_shed_id::text,
  COALESCE(cs.display_name, ''),
  '',
  observation.weight_kg,
  observation.animal_count,
  observation.operator_weight_kg,
  observation.operator_animal_count,
  cs.status,
  (observation.withdrawn_at IS NOT NULL)
FROM weighing_shed_observations observation
JOIN weighing_campaign_sheds cs
  ON cs.tenant_id=observation.tenant_id
 AND cs.campaign_shed_id=observation.campaign_shed_id
WHERE observation.tenant_id=$1::uuid
  AND observation.shed_observation_id=$2::uuid
FOR UPDATE OF observation`
	default:
		return correctionScope{}, ports.ErrInvalidArgument
	}
	if err := tx.QueryRow(ctx, query, cmd.TenantID, cmd.ObservationID).Scan(
		&scope.CampaignID,
		&scope.CampaignShedID,
		&scope.ShedDisplay,
		&scope.ScannedIdentifier,
		&scope.WeightKg,
		&scope.AnimalCount,
		&scope.OperatorWeightKg,
		&scope.OperatorAnimalCount,
		&scope.BucketStatus,
		&scope.Withdrawn,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return correctionScope{}, ports.ErrNotFound
		}
		return correctionScope{}, err
	}
	return scope, nil
}

// applyWeightCorrection writes the corrected values.
//
// operator_weight_kg is written with COALESCE(operator_weight_kg, weight_kg) —
// evaluated against the row's PRE-UPDATE values, so the first correction captures
// the operator's number and every later one leaves it exactly where it is. That is
// the whole preservation rule, in one expression, with no read-modify-write race.
func (r *Repository) applyWeightCorrection(
	ctx context.Context,
	tx pgx.Tx,
	cmd domain.WeightCorrectionCommand,
	scope correctionScope,
	animalCount int,
) (time.Time, error) {
	var correctedAt time.Time
	var query string
	var args []any
	switch cmd.RefType {
	case domain.VerificationRefTypeAnimal:
		query = `
UPDATE weighing_observations
SET weight_kg=$3::numeric,
  operator_weight_kg=COALESCE(operator_weight_kg, weight_kg),
  weight_corrected_by=$4::uuid,
  weight_corrected_at=now(),
  weight_correction_reason=NULLIF($5, '')
WHERE tenant_id=$1::uuid
  AND observation_id=$2::uuid
RETURNING weight_corrected_at`
		args = []any{cmd.TenantID, cmd.ObservationID, cmd.WeightKg, cmd.CorrectedBy, cmd.Reason}
	case domain.VerificationRefTypeShed:
		// average_weight_kg is a STORED derived column with its own (> 0) CHECK, so it
		// is recomputed in the SAME statement. Leaving it stale would make the shed's
		// average disagree with the shed's own total -- two numbers for one fact.
		query = `
UPDATE weighing_shed_observations
SET weight_kg=$3::numeric,
  animal_count=$4::integer,
  average_weight_kg=$5::numeric,
  operator_weight_kg=COALESCE(operator_weight_kg, weight_kg),
  operator_animal_count=COALESCE(operator_animal_count, animal_count),
  weight_corrected_by=$6::uuid,
  weight_corrected_at=now(),
  weight_correction_reason=NULLIF($7, '')
WHERE tenant_id=$1::uuid
  AND shed_observation_id=$2::uuid
RETURNING weight_corrected_at`
		args = []any{
			cmd.TenantID, cmd.ObservationID, cmd.WeightKg, animalCount,
			domain.RecomputeAverageWeightKg(cmd.WeightKg, animalCount),
			cmd.CorrectedBy, cmd.Reason,
		}
	default:
		return time.Time{}, ports.ErrInvalidArgument
	}
	if err := tx.QueryRow(ctx, query, args...).Scan(&correctedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// The row was locked a moment ago, so this can only mean it was deleted
			// underneath us. Not-found is the honest answer.
			return time.Time{}, ports.ErrNotFound
		}
		return time.Time{}, err
	}
	_ = scope
	return correctedAt, nil
}

func (r *Repository) auditWeightCorrection(
	ctx context.Context,
	tx pgx.Tx,
	cmd domain.WeightCorrectionCommand,
	scope correctionScope,
	result domain.WeightCorrectionResult,
) error {
	return audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     cmd.TenantID,
		ActorID:      cmd.CorrectedBy,
		ActorType:    "user",
		Action:       "weighing.observation_weight_corrected",
		ResourceType: cmd.RefType,
		ResourceID:   cmd.ObservationID,
		ScopeType:    "weighing.campaign",
		ScopeID:      scope.CampaignID,
		// Both states are recorded because "what did this weight used to be" is the
		// first question anyone asks of a corrected number, and answering it from the
		// audit row alone means never having to reconstruct it from two tables.
		BeforeState: map[string]any{
			"weight_kg":    scope.WeightKg,
			"animal_count": scope.AnimalCount,
		},
		AfterState: result,
		Metadata: map[string]any{
			"campaign_id":            scope.CampaignID,
			"campaign_shed_id":       scope.CampaignShedID,
			"ref_type":               cmd.RefType,
			"reason":                 cmd.Reason,
			"corrected_by":           cmd.CorrectedBy,
			"operator_weight_kg":     result.OperatorWeightKg,
			"client_idempotency_key": cmd.IdempotencyKey,
		},
	})
}
