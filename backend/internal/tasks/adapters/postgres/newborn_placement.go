package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	identityports "github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	protocoldomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
)

// The Record shed fallback: placing a kid whose park had no kid pen when it was born.
//
// See domain.ActionKeyRecordShed. This file owns the two halves that need Postgres: deciding at
// workflow-open time whether the step is needed at all, and applying the operator's answer as a
// real placement inside the action write's own transaction.

// IdentityTxWriter is the transaction-scoped seam this module uses to place a kid.
//
// Declared HERE, in the consuming package, so identity never has to know Tasks exists --
// the same shape counts/adapters/postgres.IdentityTxWriter uses for the shifting approval.
// *identitypostgres.Repository satisfies it; bootstrap wires the real one in.
//
// Why a transaction-scoped seam rather than identity's app service: the app service methods open
// and commit their own transaction. Calling one from an action write would give two independent
// transactions, and a crash between them would leave the step recorded as completed with the kid
// still in the wrong pen -- or the kid moved with the step still owed. Threading the caller's
// transaction is what makes the answer and its effect one atomic unit.
//
// Every relocation rule still lives in identity: the same cross-park guard, identity events,
// location history, partition upsert and goat.location.changed outbox row as the pool-owned call.
// Tasks never writes `goats`.
type IdentityTxWriter interface {
	RelocateGoatsToShedInTx(ctx context.Context, tx pgx.Tx, cmd identityports.RelocateGoatsCommand) (identityports.RelocateGoatsResult, error)
}

// WithIdentityTxWriter injects the placement seam. A repository without it can still open and run
// birth workflows; completing a Record shed step returns a clear error rather than silently
// recording the answer and leaving the kid where it was.
func (r *Repository) WithIdentityTxWriter(w IdentityTxWriter) *Repository {
	r.identityTx = w
	return r
}

// newbornPenTagQuery resolves the CONFIGURED tag of the operational location a goat currently sits
// in, using the same precedence the destinations catalog uses: the pen's own authored tag when the
// animal is in a real pen, the shed's profile when the shed has no pens.
//
// It is deliberately the pen's tag and never its residents'. A pen kept ready for kids is normally
// EMPTY, so a resident-derived answer would fail to recognise exactly the pens this asks about --
// and it would flip as animals move.
//
// partition matching uses the SAME normalization the catalog query uses (lowercase, trimmed, a
// leading "part " stripped), against shed_partitions.normalized_label -- the internal MATCHING KEY.
// partition_label is the human label and is never a join key.
//
// Index: goats primary key (tenant_id, goat_id) resolves the animal; goat_shed_partitions is keyed
// (tenant_id, goat_id); shed_partitions_tenant_shed_idx covers (tenant_id, shed_id, status). One
// indexed round trip per workflow open, never per pen.
const newbornPenTagQuery = `
SELECT COALESCE(
    CASE WHEN pen.shed_id IS NOT NULL THEN pen_stage.stage_code ELSE shed_stage.stage_code END,
    ''
  )
FROM goats g
LEFT JOIN goat_shed_partitions gsp
       ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
LEFT JOIN shed_partitions pen
       ON pen.tenant_id = g.tenant_id
      AND pen.shed_id = g.shed_id
      AND pen.status = 'active'
      AND pen.normalized_label = regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '')
LEFT JOIN animal_stage_lookup pen_stage
       ON pen_stage.tenant_id = pen.tenant_id
      AND pen_stage.animal_stage_id = pen.animal_stage_id
      AND pen_stage.status = 'active'
LEFT JOIN shed_profiles profile
       ON profile.tenant_id = g.tenant_id AND profile.location_id = g.shed_id
LEFT JOIN animal_stage_lookup shed_stage
       ON shed_stage.tenant_id = profile.tenant_id
      AND shed_stage.animal_stage_id = profile.animal_stage_id
      AND shed_stage.status = 'active'
WHERE g.tenant_id = $1::uuid AND g.goat_id = $2::uuid`

// needsShedPlacement reports whether a newborn's care workflow must carry the Record shed step.
//
// It is derived from GROUND TRUTH -- where the kid actually is right now -- rather than from
// whether the park happened to have a kid pen at raise time. That makes it self-healing in both
// directions: a kid already placed in a kid pen never gets the step, and once the fallback tags a
// pen for kids, the park's NEXT birth places automatically and opens without it.
//
// It FAILS OPEN (returns false) when the goat row cannot be read. Adding a placement step to a
// workflow on the strength of a failed lookup would ask an operator to re-file a kid that is
// probably already correct; the birth write's own kid-pen check is the gate that matters.
func (r *Repository) needsShedPlacement(ctx context.Context, tenantID, goatID string) (bool, error) {
	var stage string
	err := r.pool.QueryRow(ctx, newbornPenTagQuery, tenantID, goatID).Scan(&stage)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !protocoldomain.IsNewbornPen(stage), nil
}

// recordedPen is the operational location an operator named on the Record shed step.
type recordedPen struct {
	ParkID         string
	ShedID         string
	ShedName       string
	PartitionLabel *string
}

// resolveRecordedPen validates the answer against the LIVE location catalog inside the caller's
// transaction and returns the pen it names.
//
// The answer is "<shed_id>|<partition_label>", the same key the operator's pen picker is built on.
// It is validated against real rows rather than trusted: a stale phone can offer a pen that has
// since been retired, and filing a kid into a location that no longer exists would be worse than
// refusing the step.
func (r *Repository) resolveRecordedPen(ctx context.Context, tx pgx.Tx, tenantID, answer string) (recordedPen, error) {
	shedID, partitionLabel, err := domain.ParseRecordedPenAnswer(answer)
	if err != nil {
		return recordedPen{}, err
	}

	var out recordedPen
	var parkID, shedName *string
	scanErr := tx.QueryRow(ctx, `
SELECT shed.parent_location_id::text, shed.name
FROM locations shed
WHERE shed.tenant_id = $1::uuid
  AND shed.location_id = $2::uuid
  AND shed.location_type = 'shed'
  AND shed.status = 'active'
  AND shed.retired_at IS NULL`, tenantID, shedID).Scan(&parkID, &shedName)
	if errors.Is(scanErr, pgx.ErrNoRows) {
		return recordedPen{}, domain.ErrInvalidAnswer
	}
	if scanErr != nil {
		return recordedPen{}, scanErr
	}
	if parkID == nil || shedName == nil {
		return recordedPen{}, domain.ErrInvalidAnswer
	}
	out.ParkID, out.ShedID, out.ShedName = *parkID, shedID, *shedName

	if partitionLabel == "" {
		// A bare shed is only a valid answer when the shed genuinely has no pens. A partitioned
		// shed named without its pen is ambiguous about where the kid physically is, and guessing
		// would file it in whichever pen happened to sort first.
		var penCount int
		if err := tx.QueryRow(ctx, `
SELECT count(*) FROM shed_partitions
WHERE tenant_id = $1::uuid AND shed_id = $2::uuid AND status = 'active'`, tenantID, shedID).Scan(&penCount); err != nil {
			return recordedPen{}, err
		}
		if penCount > 0 {
			return recordedPen{}, domain.ErrInvalidAnswer
		}
		return out, nil
	}

	// A named pen must be a REAL active pen of that shed. Matched on normalized_label, the internal
	// matching key; the human partition_label is what comes back for display and for the write.
	var storedLabel string
	labelErr := tx.QueryRow(ctx, `
SELECT partition_label FROM shed_partitions
WHERE tenant_id = $1::uuid AND shed_id = $2::uuid AND status = 'active'
  AND normalized_label = $3`, tenantID, shedID, oploc.NormalizePartition(partitionLabel)).Scan(&storedLabel)
	if errors.Is(labelErr, pgx.ErrNoRows) {
		return recordedPen{}, domain.ErrInvalidAnswer
	}
	if labelErr != nil {
		return recordedPen{}, labelErr
	}
	out.PartitionLabel = &storedLabel
	return out, nil
}

// applyRecordedNewbornPen places the kid and tags the pen for kids, inside the caller's transaction.
//
// ONE TRANSACTION, THREE EFFECTS. The action row's completion, the relocation (identity event,
// location history, goat_shed_partitions upsert and the goat.location.changed outbox row) and the
// pen's kid tag all commit together or none of them do. A failure anywhere rolls the step back to
// pending, so the operator is asked again rather than being told the kid was filed when it was not.
//
// IDEMPOTENT BY CONSTRUCTION. The action write itself is the idempotency gate: ApplyAnswer returns
// an exact replay before this is reached, and a completed action refuses a second write outright,
// so this never runs twice for the same step. The relocation additionally carries its own
// per-goat outbox idempotency prefix, so even a retry that did reach it could not enqueue a second
// location event for the same animal, and the pen tag is written with ON CONFLICT so re-tagging an
// already-tagged pen is a no-op rather than a duplicate row.
func (r *Repository) applyRecordedNewbornPen(
	ctx context.Context, tx pgx.Tx, w *domain.WorkflowInstance, action domain.WorkflowAction, cmd domain.AnswerActionCommand,
) error {
	if r.identityTx == nil {
		return fmt.Errorf("tasks: record shed: identity write seam is not configured")
	}
	pen, err := r.resolveRecordedPen(ctx, tx, cmd.TenantID, strings.TrimSpace(cmd.AnswerValue))
	if err != nil {
		return err
	}

	// DestinationTag is left EMPTY on purpose: the kid is already K0 (the birth handler pins it),
	// and this step records where the animal IS rather than reclassifying it. Passing the pen's tag
	// would make a placement correction into a cohort change, which is a different decision.
	moved, err := r.identityTx.RelocateGoatsToShedInTx(ctx, tx, identityports.RelocateGoatsCommand{
		TenantID:                  cmd.TenantID,
		ActorID:                   cmd.AnsweredBy,
		TraceID:                   action.ActionID,
		GoatIDs:                   []string{w.SubjectGoatID},
		ToParkID:                  pen.ParkID,
		ToShedID:                  pen.ShedID,
		DestinationPartitionLabel: pen.PartitionLabel,
		DestinationShedName:       pen.ShedName,
		Reason:                    goatLocationHistoryReasonNewbornPenRecorded,
		OccurredAt:                cmd.AnsweredAt,
		OutboxIdempotencyPrefix:   "tasks-record-shed:" + action.ActionID,
	})
	if err != nil {
		return err
	}
	// FAIL CLOSED on a shortfall, exactly as the shifting approval does. A kid that did not move --
	// because it exited, or was merged -- must not leave the step recorded as done.
	if len(moved.MovedGoatIDs) != 1 {
		return fmt.Errorf("tasks: record shed: placed %d animals, want 1", len(moved.MovedGoatIDs))
	}
	return r.tagPenForNewborns(ctx, tx, cmd.TenantID, pen)
}

// tagPenForNewborns writes the kid tag onto the recorded pen so the park's NEXT birth places
// automatically (maintainer decision 2026-08-20: the retag is immediate and needs no approval).
//
// A pen that ALREADY carries a tag is left alone. Overwriting one would let a placement action
// silently re-purpose a pen somebody deliberately configured for another cohort -- the operator was
// recording where a kid is, not redesigning the park.
func (r *Repository) tagPenForNewborns(ctx context.Context, tx pgx.Tx, tenantID string, pen recordedPen) error {
	if pen.PartitionLabel == nil {
		// A shed with no pens carries its cohort on shed_profiles instead.
		_, err := tx.Exec(ctx, `
INSERT INTO shed_profiles (tenant_id, location_id, animal_stage_id, created_at, updated_at)
SELECT $1::uuid, $2::uuid, lookup.animal_stage_id, now(), now()
FROM animal_stage_lookup lookup
WHERE lookup.tenant_id = $1::uuid AND lookup.status = 'active' AND lookup.stage_code = $3
ON CONFLICT (tenant_id, location_id) DO NOTHING`, tenantID, pen.ShedID, protocoldomain.NewbornPenStage)
		return err
	}
	_, err := tx.Exec(ctx, `
UPDATE shed_partitions
SET animal_stage_id = lookup.animal_stage_id, updated_at = now()
FROM animal_stage_lookup lookup
WHERE shed_partitions.tenant_id = $1::uuid
  AND shed_partitions.shed_id = $2::uuid
  AND shed_partitions.status = 'active'
  AND shed_partitions.normalized_label = $3
  AND shed_partitions.animal_stage_id IS NULL
  AND lookup.tenant_id = $1::uuid
  AND lookup.status = 'active'
  AND lookup.stage_code = $4`,
		tenantID, pen.ShedID, oploc.NormalizePartition(*pen.PartitionLabel), protocoldomain.NewbornPenStage)
	return err
}

// goatLocationHistoryReasonNewbornPenRecorded distinguishes a Record shed placement from a shifting
// approval on the animal's own location history, so the timeline says WHY the kid moved.
const goatLocationHistoryReasonNewbornPenRecorded = "newborn_pen_recorded"
