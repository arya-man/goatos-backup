package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/obligation/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/vaccinepurpose"
)

// vaccinationWrite is one vaccination obligation date about to be persisted: inserted, reopened,
// rescheduled, moved by a drive-date override, reconciled, or rebound across a publish. Every one
// of those paths validates through validateVaccinationWrite so the clinical contract cannot differ
// between them.
type vaccinationWrite struct {
	Tenant     pgtype.UUID
	Rule       pgtype.UUID
	TargetType string
	Target     pgtype.UUID
	DueAt      time.Time
	// ScheduleBasis is the incoming (insert/reconcile) or persisted (reopen/reschedule/drive
	// override/carry-over) obligation_instances.schedule_basis. Empty means anchored.
	ScheduleBasis string
}

type vaccinationFloorQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// normalizeScheduleBasis maps the empty value to anchored and rejects anything the table CHECK
// would reject, so a typo fails before it reaches the database.
func normalizeScheduleBasis(basis string) (string, error) {
	switch strings.TrimSpace(basis) {
	case "", domain.ScheduleBasisAnchored:
		return domain.ScheduleBasisAnchored, nil
	case domain.ScheduleBasisAnchorMissingCatchUp:
		return domain.ScheduleBasisAnchorMissingCatchUp, nil
	default:
		return "", fmt.Errorf("obligation: unknown schedule basis %q", basis)
	}
}

// validateVaccinationWrite is the single persistence guard for vaccination timing and purpose.
//
//   - Purpose applicability comes only from vaccinepurpose.Resolve over the rule version's
//     procurement_policy. non_breeding and unconfigured purposes fail closed.
//   - birth_age / post_arrival need DOB / the accepted-intake anchor. A missing anchor is allowed
//     ONLY for the approved anchor_missing_catch_up basis, and only while the vaccine family has no
//     accepted or trusted administration. A catch-up row whose anchor exists gets the normal floor.
//   - after_previous_completion needs the accepted/trusted previous administration.
//   - A plan-governed second-wave vaccine (any trigger, any purpose) needs every first-wave vaccine
//     administered and due_at >= business day of the latest of them + the plan delay.
//
// Callers run it inside the same transaction as the write, after locking the row being written.
func validateVaccinationWrite(ctx context.Context, q vaccinationFloorQueryer, w vaccinationWrite) error {
	basis, err := normalizeScheduleBasis(w.ScheduleBasis)
	if err != nil {
		return err
	}
	if w.TargetType != "goat" {
		if basis != domain.ScheduleBasisAnchored {
			return fmt.Errorf("%w: schedule basis %s requires a goat target", ports.ErrBeforeVaccinationAgeFloor, basis)
		}
		return nil
	}
	var (
		triggerType, repeat, vaccineCode, vaccineName string
		purpose, species                              string
		offsetDays, minGapDays                        int32
		dob, warmupAt                                 *time.Time
		policyJSON                                    []byte
	)
	err = q.QueryRow(ctx, `
SELECT pr.trigger_type, pr.offset_days, pr.min_gap_days, pr.repeat,
       COALESCE(NULLIF(pr.eligibility_json->'vaccine'->>'code', ''), NULLIF(pv.rule_dsl->'vaccine'->>'code', ''), ''),
       COALESCE(NULLIF(pr.eligibility_json->'vaccine'->>'name', ''), NULLIF(pv.rule_dsl->'vaccine'->>'name', ''), ''),
       g.dob,
       COALESCE(proc.warmup_started_at, proc.intake_accepted_at, g.entry_date::timestamp AT TIME ZONE 'Asia/Kolkata'),
       COALESCE(proc.purpose, ''), COALESCE(NULLIF(g.species, ''), 'goat'),
       COALESCE(pv.rule_dsl->'procurement_policy', 'null'::jsonb)
FROM protocol_rules pr
JOIN protocol_versions pv
  ON pv.tenant_id = pr.tenant_id AND pv.protocol_version_id = pr.protocol_version_id
JOIN protocol_definitions pd
  ON pd.tenant_id = pv.tenant_id AND pd.protocol_id = pv.protocol_id
JOIN goats g
  ON g.tenant_id = pr.tenant_id AND g.goat_id = $3
-- Same row selection and entry anchor as generation (GetGoatForGeneration + warmingEntryAt):
-- warmup start, else accepted intake, else the goat's herd entry date. created_at is NOT an
-- anchor -- a load row that never reached warmup/intake proves no arrival.
LEFT JOIN LATERAL (
  SELECT plg.warmup_started_at, plg.intake_accepted_at, plg.purpose
  FROM procurement_load_goats plg
  WHERE plg.tenant_id = g.tenant_id AND plg.goat_id = g.goat_id
  ORDER BY COALESCE(plg.warmup_started_at, plg.intake_accepted_at, plg.created_at) DESC NULLS LAST
  LIMIT 1
  FOR SHARE
) proc ON true
WHERE pr.tenant_id = $1
  AND pr.rule_id = $2
  AND pd.category = 'vaccination'
-- The anchors are share-locked for the life of the write transaction: a concurrent DOB or
-- arrival correction either commits first (and this serializable attempt retries against it) or
-- waits until this write commits and is ordered after it. It can never slip between the proof
-- and the write.
FOR SHARE OF g`, w.Tenant, w.Rule, w.Target).
		Scan(&triggerType, &offsetDays, &minGapDays, &repeat, &vaccineCode, &vaccineName, &dob, &warmupAt, &purpose, &species, &policyJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		// Not a vaccination rule (or the goat is gone): nothing clinical to prove, but a
		// catch-up exception can only ever be granted to vaccination work.
		if basis != domain.ScheduleBasisAnchored {
			return fmt.Errorf("%w: schedule basis %s requires a vaccination rule", ports.ErrBeforeVaccinationAgeFloor, basis)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("obligation: read vaccination write contract: %w", err)
	}
	vaccine := vaccineName
	if strings.TrimSpace(vaccine) == "" {
		vaccine = vaccineCode
	}
	family := vaccineCode
	if strings.TrimSpace(family) == "" {
		family = vaccineName
	}

	policy, err := vaccinepurpose.DecodePolicy(policyJSON)
	if err != nil {
		return fmt.Errorf("obligation: decode vaccination procurement policy: %w", err)
	}
	decision := vaccinepurpose.Resolve(policy, purpose, species, vaccine)
	if !decision.Applicable {
		return fmt.Errorf("%w: %s", ports.ErrVaccinationNotApplicable, decision.Reason)
	}

	var floor time.Time
	switch strings.ToLower(strings.TrimSpace(triggerType)) {
	case "birth_age", "post_arrival":
		anchor, anchorName := dob, "birth date"
		if strings.EqualFold(strings.TrimSpace(triggerType), "post_arrival") {
			anchor, anchorName = warmupAt, "arrival"
		}
		if anchor != nil {
			floor = biztime.BusinessDayStart(*anchor).AddDate(0, 0, int(offsetDays))
			break
		}
		if basis != domain.ScheduleBasisAnchorMissingCatchUp {
			return fmt.Errorf("%w: %s anchor is missing", ports.ErrBeforeVaccinationAgeFloor, anchorName)
		}
		// Approved adult catch-up: the anchor is genuinely unknown, so there is no age floor to
		// prove -- but the exception exists only for a BLANK vaccine family. Any accepted or
		// trusted administration means the animal is on the history-backed path instead.
		// "Blank" is generation's hasVaccineAdministrationHistory definition, which also counts
		// vaccination anchor events: an anchored family is on the anchor path, never catch-up.
		administered, err := latestVaccineAdministration(ctx, q, w.Tenant, w.Target, family)
		if err != nil {
			return err
		}
		anchored, err := latestVaccineAnchorEvent(ctx, q, w.Tenant, w.Target, family)
		if err != nil {
			return err
		}
		if administered = laterOf(administered, anchored); administered != nil {
			return fmt.Errorf("%w: catch-up basis is invalid for %s with administration/anchor history on %s",
				ports.ErrBeforeVaccinationAgeFloor, family, biztime.BusinessDate(*administered))
		}
	case "after_previous_completion":
		administered, err := latestVaccineAdministration(ctx, q, w.Tenant, w.Target, family)
		if err != nil {
			return err
		}
		// The PREVIOUS dose may also be an attested vaccination anchor, exactly as generation
		// chains follow-ups from anchor_admins. Anchors never count as first-wave evidence.
		anchored, err := latestVaccineAnchorEvent(ctx, q, w.Tenant, w.Target, family)
		if err != nil {
			return err
		}
		administered = laterOf(administered, anchored)
		if administered == nil {
			return fmt.Errorf("%w: previous completion anchor is missing", ports.ErrBeforeVaccinationAgeFloor)
		}
		anchor := biztime.BusinessDayStart(*administered)
		if strings.EqualFold(strings.TrimSpace(repeat), "yearly") {
			floor = anchor.AddDate(1, 0, 0)
		} else {
			gap := offsetDays
			if minGapDays > gap {
				gap = minGapDays
			}
			if gap > 0 {
				floor = anchor.AddDate(0, 0, int(gap))
			}
		}
	}
	if !floor.IsZero() && w.DueAt.Before(floor) {
		return fmt.Errorf("%w: requested=%s floor=%s", ports.ErrBeforeVaccinationAgeFloor, biztime.BusinessDate(w.DueAt), biztime.BusinessDate(floor))
	}

	if decision.PlanGoverned && decision.SecondWave {
		var latest time.Time
		for _, required := range decision.Plan.FirstWave {
			administered, err := latestVaccineAdministration(ctx, q, w.Tenant, w.Target, required)
			if err != nil {
				return err
			}
			if administered == nil {
				return fmt.Errorf("%w: first-wave vaccine %s is incomplete", ports.ErrBeforeVaccinationAgeFloor, required)
			}
			if administered.After(latest) {
				latest = *administered
			}
		}
		secondWaveFloor := biztime.BusinessDayStart(latest).AddDate(0, 0, int(decision.Plan.Delay()))
		if w.DueAt.Before(secondWaveFloor) {
			return fmt.Errorf("%w: requested=%s second_wave_floor=%s", ports.ErrBeforeVaccinationAgeFloor, biztime.BusinessDate(w.DueAt), biztime.BusinessDate(secondWaveFloor))
		}
	}
	return nil
}

// isVaccinationContractViolation reports a clinical rejection (as opposed to plumbing failure).
func isVaccinationContractViolation(err error) bool {
	return errors.Is(err, ports.ErrBeforeVaccinationAgeFloor) || errors.Is(err, ports.ErrVaccinationNotApplicable)
}

// vaccineAdministrationHistorySQL is the ONE administration-history fragment every validator
// read uses ($1 tenant, $2 goat, $3 vaccine canonicalized like vaccinepurpose.Normalize). Its
// channels and filters mirror generation's RecentVaccineAdministrationsForGoats exactly, so
// persistence is never looser (or stricter) than the schedule that proposed the date:
//
//   - accepted + verified vaccination completions;
//   - trusted, reviewed, proof-backed procurement evidence administered INSIDE a 28-35 day
//     holding-farm warm-up window of the same load;
//   - reviewed, accepted pre-arrival history.
//
// vaccination_anchor_events are deliberately NOT administrations: a scope-wide anchor date says
// when a drive starts, not that this animal received a dose.
const vaccineAdministrationHistorySQL = `
WITH administrations AS (
  SELECT vc.administered_at,
         COALESCE(NULLIF(pr.eligibility_json->'vaccine'->>'code',''), NULLIF(pv.rule_dsl->'vaccine'->>'code',''), '') AS vaccine_code
  FROM vaccination_completions vc
  JOIN obligation_instances oi ON oi.tenant_id = vc.tenant_id AND oi.obligation_id = vc.obligation_id
  JOIN protocol_versions pv ON pv.tenant_id = oi.tenant_id AND pv.protocol_version_id = oi.protocol_version_id
  LEFT JOIN protocol_rules pr ON pr.tenant_id = oi.tenant_id AND pr.protocol_version_id = oi.protocol_version_id AND pr.rule_id = oi.rule_id
  WHERE vc.tenant_id = $1 AND vc.goat_id = $2
    AND vc.status = 'accepted' AND vc.verified_at IS NOT NULL
  UNION ALL
  SELECT ev.administered_at,
         COALESCE(NULLIF(pr.eligibility_json->'vaccine'->>'code',''), NULLIF(ev.metadata->>'vaccine_code',''), NULLIF(ev.vaccine_name,''), NULLIF(pv.rule_dsl->'vaccine'->>'code',''), '')
  FROM procurement_hf_vaccination_evidence ev
  JOIN procurement_load_goats plg ON plg.tenant_id = ev.tenant_id AND plg.load_id = ev.load_id AND plg.goat_id = ev.goat_id
  JOIN proof_artifacts proof ON proof.tenant_id = ev.tenant_id AND proof.proof_id = ev.proof_ref_id AND proof.upload_state = 'completed'
  JOIN protocol_versions pv ON pv.tenant_id = ev.tenant_id AND pv.protocol_version_id = ev.protocol_version_id
  LEFT JOIN protocol_rules pr ON pr.tenant_id = ev.tenant_id AND pr.protocol_version_id = ev.protocol_version_id AND pr.rule_id = ev.rule_id
  WHERE ev.tenant_id = $1 AND ev.goat_id = $2
    AND ev.review_status = 'trusted' AND ev.reviewed_at IS NOT NULL
    AND ev.proof_ref_id IS NOT NULL
    AND plg.holding_location_id IS NOT NULL
    AND plg.warmup_started_at IS NOT NULL
    AND COALESCE(
      plg.warmup_days,
      floor(extract(epoch FROM (COALESCE(plg.warmup_ended_at, ev.administered_at) - plg.warmup_started_at)) / 86400)::int
    ) BETWEEN 28 AND 35
    AND ev.administered_at >= plg.warmup_started_at
    AND (plg.warmup_ended_at IS NULL OR ev.administered_at <= plg.warmup_ended_at)
  UNION ALL
  SELECT ph.administered_at,
         COALESCE(NULLIF(pr.eligibility_json->'vaccine'->>'code',''), NULLIF(ph.vaccine_code,''), NULLIF(pv.rule_dsl->'vaccine'->>'code',''), '')
  FROM vaccination_prearrival_history_entries ph
  JOIN protocol_versions pv ON pv.tenant_id = ph.tenant_id AND pv.protocol_version_id = ph.protocol_version_id
  LEFT JOIN protocol_rules pr ON pr.tenant_id = ph.tenant_id AND pr.protocol_version_id = ph.protocol_version_id AND pr.rule_id = ph.rule_id
  WHERE ph.tenant_id = $1 AND ph.goat_id = $2
    AND ph.review_status = 'accepted' AND ph.reviewed_at IS NOT NULL
)
SELECT max(administered_at)
FROM administrations
WHERE lower(replace(replace(replace(replace(vaccine_code,' ',''),'_',''),'+',''),'-','')) = $3`

// vaccineAnchorEventSQL mirrors generation's anchor_admins for ONE goat and vaccine family
// ($1 tenant, $2 goat, $3 canonical vaccine): live, chaining, non-publish anchor events whose scope
// covers the animal (tenant / animal_set / park / shed / partition), dated before the business
// day, honouring enforce_age_eligibility for birth-age anchor rules.
const vaccineAnchorEventSQL = `
SELECT max(vae.anchor_date::timestamp AT TIME ZONE 'Asia/Kolkata')
FROM goats g
LEFT JOIN goat_shed_partitions gsp
  ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id AND gsp.shed_id = g.shed_id
JOIN vaccination_anchor_events vae
  ON vae.tenant_id = g.tenant_id
 AND vae.canceled_at IS NULL
 AND vae.chain_future_from_anchor
 AND vae.protocol_version_id IS NOT NULL
 AND vae.dose_code IS NOT NULL
 AND vae.source_system <> 'vaccination_plan_publish'
 AND vae.anchor_date < (now() AT TIME ZONE 'Asia/Kolkata')::date
 AND (
   vae.scope_type = 'tenant'
   OR (vae.scope_type = 'animal_set' AND vae.scope_payload ? 'animal_ids' AND (vae.scope_payload -> 'animal_ids') ? g.goat_id::text)
   OR (vae.scope_type = 'park' AND COALESCE(vae.scope_payload ->> 'park_id', '') = g.park_id::text)
   OR (vae.scope_type = 'shed' AND COALESCE(vae.scope_payload ->> 'shed_id', '') = g.shed_id::text)
   OR (vae.scope_type = 'partition' AND COALESCE(vae.scope_payload ->> 'shed_id', '') = g.shed_id::text AND COALESCE(vae.scope_payload ->> 'partition_label', '') = COALESCE(gsp.partition_label, 'whole'))
 )
LEFT JOIN protocol_rules anchor_pr
  ON anchor_pr.tenant_id = vae.tenant_id
 AND anchor_pr.protocol_version_id = vae.protocol_version_id
 AND lower(btrim(anchor_pr.dose_code)) = lower(btrim(vae.dose_code))
WHERE g.tenant_id = $1 AND g.goat_id = $2
  AND lower(replace(replace(replace(replace(vae.vaccine_code,' ',''),'_',''),'+',''),'-','')) = $3
  AND (
    NOT vae.enforce_age_eligibility
    OR anchor_pr.trigger_type IS DISTINCT FROM 'birth_age'
    OR (g.dob IS NOT NULL AND g.dob + anchor_pr.offset_days <= vae.anchor_date)
  )`

// latestVaccineAnchorEvent is the latest anchor event date covering the animal for one family.
func latestVaccineAnchorEvent(ctx context.Context, q vaccinationFloorQueryer, tenant, target pgtype.UUID, vaccine string) (*time.Time, error) {
	normalized := vaccinepurpose.Normalize(vaccine)
	if normalized == "" {
		return nil, nil
	}
	var anchoredAt *time.Time
	if err := q.QueryRow(ctx, vaccineAnchorEventSQL, tenant, target, normalized).Scan(&anchoredAt); err != nil {
		return nil, fmt.Errorf("obligation: read vaccination anchor events: %w", err)
	}
	return anchoredAt, nil
}

func laterOf(a, b *time.Time) *time.Time {
	if a == nil {
		return b
	}
	if b != nil && b.After(*a) {
		return b
	}
	return a
}

// latestVaccineAdministration is the latest administration of one vaccine family through the
// shared history fragment above.
func latestVaccineAdministration(ctx context.Context, q vaccinationFloorQueryer, tenant, target pgtype.UUID, vaccine string) (*time.Time, error) {
	normalized := vaccinepurpose.Normalize(vaccine)
	if normalized == "" {
		return nil, nil
	}
	var administeredAt *time.Time
	if err := q.QueryRow(ctx, vaccineAdministrationHistorySQL, tenant, target, normalized).Scan(&administeredAt); err != nil {
		return nil, fmt.Errorf("obligation: read vaccination administration history: %w", err)
	}
	return administeredAt, nil
}

// vaccinationWriteMaxSerializationRetries bounds the retry of a SERIALIZABLE vaccination write
// that lost a race (SQLSTATE 40001). The retry re-reads the anchors and re-validates from scratch,
// so it can only commit a state the validator accepts against the winner's committed data.
const vaccinationWriteMaxSerializationRetries = 3

// isSerializationFailure reports a transaction PostgreSQL aborted for concurrency alone:
// serialization_failure (40001) or deadlock_detected (40P01). Both are safe to retry from scratch.
func isSerializationFailure(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == "40001" || pgErr.Code == "40P01")
}

// withSerializableVaccinationTx runs attempt in a fresh SERIALIZABLE transaction, retrying a
// serialization failure a bounded number of times. attempt owns Commit; the deferred Rollback is a
// no-op after a successful commit.
func withSerializableVaccinationTx[T any](ctx context.Context, begin func(context.Context, pgx.TxOptions) (pgx.Tx, error), attempt func(pgx.Tx) (T, error)) (T, error) {
	var zero T
	var lastErr error
	for i := 0; i < vaccinationWriteMaxSerializationRetries; i++ {
		tx, err := begin(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
		if err != nil {
			return zero, fmt.Errorf("obligation: begin serializable vaccination write: %w", err)
		}
		out, err := attempt(tx)
		_ = tx.Rollback(ctx)
		if err == nil {
			return out, nil
		}
		if !isSerializationFailure(err) {
			return zero, err
		}
		lastErr = err
	}
	return zero, fmt.Errorf("obligation: vaccination write lost %d serialization retries: %w", vaccinationWriteMaxSerializationRetries, lastErr)
}
