package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

// Pen-tag ADOPTION for the typed shifting rewrite (maintainer decisions 2026-08-20,
// docs/features/shifting/shifting-rewrite-tag-rules.md): a spacing / delivery / flushing movement
// into an EMPTY pen tags that pen with the arriving group's tag -- "pen tags follow occupancy".
//
// The write itself is the same one the Counts Breakdown Stage editor performs
// (applyConfiguredCohort in shed_stage.go): shed_partitions.animal_stage_id for a pen,
// shed_profiles.animal_stage_id for a shed with no pens. What is DIFFERENT here is the guard: the
// raise promised the park head an EMPTY (or already-matching) destination, approval and completion
// can be hours apart, and this runs under the shifting row lock at apply -- so it re-validates that
// promise and FAILS CLOSED when the pen changed underneath the approval, rolling the whole apply
// back rather than silently creating the mixed pen the raise-time check exists to prevent.

// ConfigureAdoptedShedCohortInTx validates and writes the destination pen's adopted tag inside the
// caller's transaction. It is exposed through the counts IdentityTxWriter seam; counts never
// writes shed_partitions/shed_profiles itself.
func (r *Repository) ConfigureAdoptedShedCohortInTx(ctx context.Context, tx pgx.Tx, cmd ports.ConfigureAdoptedShedCohortCommand) error {
	stage := strings.TrimSpace(cmd.Stage)
	if strings.TrimSpace(cmd.TenantID) == "" || strings.TrimSpace(cmd.ShedID) == "" || stage == "" {
		return ports.ErrInvalidReference
	}

	// Canonicalize against the live vocabulary, clinical refused: an ADOPTED tag is a pen
	// configuration a movement writes, and no movement type adopts a clinical state (health stamps
	// the ANIMAL's state; it never re-tags the pen). resolveDestinationTag is reused, not copied,
	// so the vocabulary and clinical rules stay single-sourced.
	resolution, err := r.resolveDestinationTag(ctx, tx, ports.RelocateGoatsCommand{
		TenantID:       cmd.TenantID,
		DestinationTag: stage,
	})
	if err != nil {
		return err
	}
	if resolution.stage == "" {
		return ports.ErrInvalidReference
	}

	// Re-validate the raise-time promise under the apply lock: the pen must still be either
	// UNCONFIGURED or already configured with this same tag, and must hold no live animal whose
	// stage disagrees with the tag being adopted. Anything else means the pen changed between
	// approval and completion; failing rolls the entire apply back.
	partition := ""
	if cmd.PartitionLabel != nil {
		partition = strings.TrimSpace(*cmd.PartitionLabel)
	}
	var currentTag string
	var occupantsDisagree int
	if oploc.IsPartitioned(partition) {
		err = tx.QueryRow(ctx, `
SELECT COALESCE(a.stage_code, ''),
       (SELECT COUNT(*)
        FROM goats g
        JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
        WHERE g.tenant_id = sp.tenant_id AND g.shed_id = sp.shed_id
          AND g.lifecycle_status = 'alive' AND g.exited_at IS NULL
          AND regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '') = sp.normalized_label
          AND lower(btrim(COALESCE(g.management_stage, ''))) <> lower($4))
FROM shed_partitions sp
LEFT JOIN animal_stage_lookup a
       ON a.tenant_id = sp.tenant_id AND a.animal_stage_id = sp.animal_stage_id AND a.status = 'active'
WHERE sp.tenant_id = $1::uuid AND sp.shed_id = $2::uuid AND sp.status = 'active'
  AND sp.normalized_label = $3`,
			cmd.TenantID, cmd.ShedID, oploc.NormalizePartition(partition), resolution.stage).
			Scan(&currentTag, &occupantsDisagree)
	} else {
		err = tx.QueryRow(ctx, `
SELECT COALESCE(a.stage_code, ''),
       (SELECT COUNT(*)
        FROM goats g
        WHERE g.tenant_id = shed.tenant_id AND g.shed_id = shed.location_id
          AND g.lifecycle_status = 'alive' AND g.exited_at IS NULL
          AND lower(btrim(COALESCE(g.management_stage, ''))) <> lower($3))
FROM locations shed
LEFT JOIN shed_profiles p ON p.tenant_id = shed.tenant_id AND p.location_id = shed.location_id
LEFT JOIN animal_stage_lookup a
       ON a.tenant_id = shed.tenant_id AND a.animal_stage_id = p.animal_stage_id AND a.status = 'active'
WHERE shed.tenant_id = $1::uuid AND shed.location_id = $2::uuid
  AND shed.location_type = 'shed' AND shed.status = 'active'`,
			cmd.TenantID, cmd.ShedID, resolution.stage).
			Scan(&currentTag, &occupantsDisagree)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		// The pen left the active catalog between approval and apply; the partition re-validation
		// in the apply path fails this case too, but adoption must not depend on ordering.
		return ports.ErrDestinationPenChanged
	}
	if err != nil {
		return fmt.Errorf("identity: adopt shed cohort: validate destination pen: %w", err)
	}
	if currentTag != "" && !strings.EqualFold(currentTag, resolution.stage) {
		return ports.ErrDestinationPenChanged
	}
	if occupantsDisagree > 0 {
		return ports.ErrDestinationPenChanged
	}

	if err := r.writeConfiguredCohort(ctx, tx, cmd.TenantID, cmd.ShedID, partition, resolution.stage); err != nil {
		return err
	}
	return nil
}
