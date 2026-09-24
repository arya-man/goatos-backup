package postgres

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/obligation/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
	"github.com/vgoats/goatos/backend/internal/platform/vaccinationanchor"
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

// vaccinationFloorQueryer is what the validator needs from a transaction.
type vaccinationFloorQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// ---------------------------------------------------------------------------------------------
// Procurement arrival row selection.
//
// OWNED SEPARATELY (arrival-anchor fix): this is the ONE place the validator chooses a goat's
// procurement_load_goats row and derives the post_arrival anchor and purpose from it. Both
// fragments assume the goat is aliased `g` and expose the chosen row as `proc`. Change the row
// choice here only; every contract read (single-row and batch) uses these two constants.
// ---------------------------------------------------------------------------------------------

// vaccinationArrivalColumnsSQL selects (arrival anchor, purpose) from `proc` / `g`.
const vaccinationArrivalColumnsSQL = `
       COALESCE(proc.warmup_started_at, proc.intake_accepted_at, g.entry_date::timestamp AT TIME ZONE 'Asia/Kolkata'),
       COALESCE(proc.purpose, '')`

// vaccinationArrivalProcurementLateralSQL joins the procurement row that governs the animal.
const vaccinationArrivalProcurementLateralSQL = `
-- Same row selection and entry anchor as generation (GetGoatForGeneration + warmingEntryAt):
-- warmup start, else accepted intake, else the goat's herd entry date. created_at is NOT an
-- anchor -- a load row that never reached warmup/intake proves no arrival. Row choice is
-- canonical: accepted herd intake first, then warmup, then latest, so a newer pending or
-- rejected re-procurement row can never outrank the intake that governs the animal.
LEFT JOIN LATERAL (
  SELECT plg.warmup_started_at, plg.intake_accepted_at, plg.purpose
  FROM procurement_load_goats plg
  WHERE plg.tenant_id = g.tenant_id AND plg.goat_id = g.goat_id
  ORDER BY (plg.intake_accepted_at IS NOT NULL OR plg.current_state = 'accepted_herd_intake' OR plg.selection_state = 'accepted_herd_intake') DESC, (plg.warmup_started_at IS NOT NULL) DESC, COALESCE(plg.warmup_started_at, plg.intake_accepted_at, plg.created_at) DESC, plg.load_goat_id DESC
  LIMIT 1
  FOR SHARE
) proc ON true`

// ---------------------------------------------------------------------------------------------

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

// vaccinationWriteContract is the rule + animal contract one write is judged against. nil means
// the rule is not a vaccination rule (or the goat is gone).
type vaccinationWriteContract struct {
	TriggerType string
	OffsetDays  int32
	MinGapDays  int32
	Repeat      string
	VaccineCode string
	VaccineName string
	DOB         *time.Time
	ArrivalAt   *time.Time
	Purpose     string
	Species     string
	PolicyJSON  []byte
}

// vaccinationGoatHistory is the animal's accepted/trusted administration history and its
// chaining anchor events, each as latest time per canonical (vaccinepurpose.Normalize) family.
type vaccinationGoatHistory struct {
	Administered map[string]time.Time
	Anchored     map[string]time.Time
}

func (h vaccinationGoatHistory) latestAdministered(vaccine string) *time.Time {
	return latestIn(h.Administered, vaccine)
}

func (h vaccinationGoatHistory) latestAnchored(vaccine string) *time.Time {
	return latestIn(h.Anchored, vaccine)
}

func latestIn(m map[string]time.Time, vaccine string) *time.Time {
	normalized := vaccinepurpose.Normalize(vaccine)
	if normalized == "" {
		return nil
	}
	if t, ok := m[normalized]; ok {
		return &t
	}
	return nil
}

// vaccinationWriteInputs is everything decideVaccinationWrite needs for one write. Loading it is
// the only I/O; the decision is pure.
type vaccinationWriteInputs struct {
	Contract *vaccinationWriteContract
	History  vaccinationGoatHistory
}

// validateVaccinationWrite is the single persistence guard for vaccination timing and purpose.
// It is validateVaccinationWrites for one write: the load and the decision are the same code.
//
// Callers run it inside the same transaction as the write, after locking the row being written.
func validateVaccinationWrite(ctx context.Context, q vaccinationFloorQueryer, w vaccinationWrite) error {
	decisions, err := validateVaccinationWrites(ctx, q, []vaccinationWrite{w})
	if err != nil {
		return err
	}
	return decisions[0]
}

// validateVaccinationWrites proves a batch of writes with a FIXED number of queries (at most four,
// independent of the batch size) and then runs decideVaccinationWrite per write. The returned
// slice holds one decision per write (nil = allowed); the error is a plumbing failure only.
//
// All writes must belong to one tenant. The anchors of every vaccination goat in the batch are
// share-locked, in goat_id order, for the life of the transaction: a concurrent DOB or arrival
// correction either commits first (and this serializable attempt retries against it) or waits
// until this write commits. It can never slip between the proof and the write.
func validateVaccinationWrites(ctx context.Context, q vaccinationFloorQueryer, writes []vaccinationWrite) ([]error, error) {
	inputs, err := loadVaccinationWriteInputs(ctx, q, writes)
	if err != nil {
		return nil, err
	}
	out := make([]error, len(writes))
	for i, w := range writes {
		out[i] = decideVaccinationWrite(w, inputs[i])
	}
	return out, nil
}

type ruleGoatKey [32]byte

func makeRuleGoatKey(rule, goat pgtype.UUID) ruleGoatKey {
	var k ruleGoatKey
	copy(k[:16], rule.Bytes[:])
	copy(k[16:], goat.Bytes[:])
	return k
}

// loadVaccinationWriteInputs issues, for the whole batch:
//  1. one ordered FOR SHARE lock of every goat whose write names a vaccination rule;
//  2. one contract read over unnest(rule, goat) pairs;
//  3. one administration-history read grouped by (goat, family);
//  4. one anchor-event read grouped by (goat, family).
//
// Reads 3-4 are skipped when no write resolved to a vaccination contract, so a non-vaccination
// write costs one round trip.
func loadVaccinationWriteInputs(ctx context.Context, q vaccinationFloorQueryer, writes []vaccinationWrite) ([]vaccinationWriteInputs, error) {
	out := make([]vaccinationWriteInputs, len(writes))
	var (
		tenant      pgtype.UUID
		rules       []pgtype.UUID
		goats       []pgtype.UUID
		seenPair    = map[ruleGoatKey]bool{}
		needsLookup = false
	)
	for _, w := range writes {
		// A write the pure decision rejects (or passes) without data needs no read.
		if w.TargetType != "goat" {
			continue
		}
		if _, err := normalizeScheduleBasis(w.ScheduleBasis); err != nil {
			continue
		}
		if !needsLookup {
			tenant, needsLookup = w.Tenant, true
		} else if tenant != w.Tenant {
			return nil, fmt.Errorf("obligation: vaccination write batch spans tenants")
		}
		k := makeRuleGoatKey(w.Rule, w.Target)
		if seenPair[k] {
			continue
		}
		seenPair[k] = true
		rules = append(rules, w.Rule)
		goats = append(goats, w.Target)
	}
	if !needsLookup {
		return out, nil
	}

	// 1. Lock order is normalized to goat_id: every validator lock (single row, carry-over chunk,
	// drive override) takes goats in the same order, so two of them can never deadlock on goats.
	// The ORDER BY sits below the row-lock step, so locks are taken in that order.
	lockRows, err := q.Query(ctx, vaccinationWriteGoatLockSQL, tenant, rules, goats)
	if err != nil {
		return nil, fmt.Errorf("obligation: lock vaccination write goats: %w", err)
	}
	var lockedGoats []pgtype.UUID
	for lockRows.Next() {
		var id pgtype.UUID
		if err := lockRows.Scan(&id); err != nil {
			lockRows.Close()
			return nil, fmt.Errorf("obligation: scan locked vaccination goat: %w", err)
		}
		lockedGoats = append(lockedGoats, id)
	}
	lockRows.Close()
	if err := lockRows.Err(); err != nil {
		return nil, fmt.Errorf("obligation: lock vaccination write goats: %w", err)
	}
	if len(lockedGoats) == 0 {
		return out, nil
	}

	// 2. Contracts for every (rule, goat) pair.
	contractRows, err := q.Query(ctx, vaccinationWriteContractSQL, tenant, rules, goats)
	if err != nil {
		return nil, fmt.Errorf("obligation: read vaccination write contract: %w", err)
	}
	contracts := map[ruleGoatKey]*vaccinationWriteContract{}
	for contractRows.Next() {
		var (
			rule, goat pgtype.UUID
			c          vaccinationWriteContract
		)
		if err := contractRows.Scan(&rule, &goat, &c.TriggerType, &c.OffsetDays, &c.MinGapDays, &c.Repeat, &c.VaccineCode, &c.VaccineName,
			&c.DOB, &c.ArrivalAt, &c.Purpose, &c.Species, &c.PolicyJSON); err != nil {
			contractRows.Close()
			return nil, fmt.Errorf("obligation: read vaccination write contract: %w", err)
		}
		contracts[makeRuleGoatKey(rule, goat)] = &c
	}
	contractRows.Close()
	if err := contractRows.Err(); err != nil {
		return nil, fmt.Errorf("obligation: read vaccination write contract: %w", err)
	}

	// 3-4. History for every goat with a contract.
	histories := map[[16]byte]*vaccinationGoatHistory{}
	var historyGoats []pgtype.UUID
	for _, w := range writes {
		if w.TargetType != "goat" || contracts[makeRuleGoatKey(w.Rule, w.Target)] == nil {
			continue
		}
		if _, ok := histories[w.Target.Bytes]; ok {
			continue
		}
		histories[w.Target.Bytes] = &vaccinationGoatHistory{Administered: map[string]time.Time{}, Anchored: map[string]time.Time{}}
		historyGoats = append(historyGoats, w.Target)
	}
	if len(historyGoats) > 0 {
		if err := scanGoatFamilyLatest(ctx, q, vaccineAdministrationHistorySQL, tenant, historyGoats, histories, false); err != nil {
			return nil, fmt.Errorf("obligation: read vaccination administration history: %w", err)
		}
		if err := scanGoatFamilyLatest(ctx, q, vaccineAnchorEventSQL, tenant, historyGoats, histories, true); err != nil {
			return nil, fmt.Errorf("obligation: read vaccination anchor events: %w", err)
		}
	}

	for i, w := range writes {
		if w.TargetType != "goat" {
			continue
		}
		c := contracts[makeRuleGoatKey(w.Rule, w.Target)]
		if c == nil {
			continue
		}
		out[i] = vaccinationWriteInputs{Contract: c, History: *histories[w.Target.Bytes]}
	}
	return out, nil
}

// vaccinationWriteGoatLockSQL share-locks, in goat_id order, every goat whose write names a
// vaccination rule ($1 tenant, $2 rule ids, $3 goat ids; pairwise).
const vaccinationWriteGoatLockSQL = `
SELECT g.goat_id
FROM goats g
WHERE g.tenant_id = $1
  AND g.goat_id IN (
    SELECT req.goat_id
    FROM unnest($2::uuid[], $3::uuid[]) AS req(rule_id, goat_id)
    JOIN protocol_rules pr ON pr.tenant_id = $1 AND pr.rule_id = req.rule_id
    JOIN protocol_versions pv ON pv.tenant_id = pr.tenant_id AND pv.protocol_version_id = pr.protocol_version_id
    JOIN protocol_definitions pd ON pd.tenant_id = pv.tenant_id AND pd.protocol_id = pv.protocol_id
    WHERE pd.category = 'vaccination'
  )
ORDER BY g.goat_id
FOR SHARE OF g`

// vaccinationWriteContractSQL reads the rule + animal contract for every (rule, goat) pair
// ($1 tenant, $2 rule ids, $3 goat ids; pairwise).
const vaccinationWriteContractSQL = `
SELECT req.rule_id, req.goat_id,
       pr.trigger_type, pr.offset_days, pr.min_gap_days, pr.repeat,
       COALESCE(NULLIF(pr.eligibility_json->'vaccine'->>'code', ''), NULLIF(pv.rule_dsl->'vaccine'->>'code', ''), ''),
       COALESCE(NULLIF(pr.eligibility_json->'vaccine'->>'name', ''), NULLIF(pv.rule_dsl->'vaccine'->>'name', ''), ''),
       g.dob,` + vaccinationArrivalColumnsSQL + `,
       COALESCE(NULLIF(g.species, ''), 'goat'),
       COALESCE(pv.rule_dsl->'procurement_policy', 'null'::jsonb)
FROM unnest($2::uuid[], $3::uuid[]) AS req(rule_id, goat_id)
JOIN protocol_rules pr
  ON pr.tenant_id = $1 AND pr.rule_id = req.rule_id
JOIN protocol_versions pv
  ON pv.tenant_id = pr.tenant_id AND pv.protocol_version_id = pr.protocol_version_id
JOIN protocol_definitions pd
  ON pd.tenant_id = pv.tenant_id AND pd.protocol_id = pv.protocol_id
JOIN goats g
  ON g.tenant_id = pr.tenant_id AND g.goat_id = req.goat_id` + vaccinationArrivalProcurementLateralSQL + `
WHERE pd.category = 'vaccination'`

func scanGoatFamilyLatest(ctx context.Context, q vaccinationFloorQueryer, sql string, tenant pgtype.UUID, goats []pgtype.UUID, into map[[16]byte]*vaccinationGoatHistory, anchors bool) error {
	bound, err := sqlbind.Bind(sql, tenant, goats)
	if err != nil {
		return err
	}
	rows, err := q.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			goat   pgtype.UUID
			family string
			latest *time.Time
		)
		if err := rows.Scan(&goat, &family, &latest); err != nil {
			return err
		}
		h := into[goat.Bytes]
		if h == nil || latest == nil || family == "" {
			continue
		}
		if anchors {
			h.Anchored[family] = *latest
		} else {
			h.Administered[family] = *latest
		}
	}
	return rows.Err()
}

// decideVaccinationWrite is the pure clinical decision for one write. It is shared exactly by the
// single-row and batch paths.
//
//   - Purpose applicability comes only from vaccinepurpose.Resolve over the rule version's
//     procurement_policy. non_breeding and unconfigured purposes fail closed.
//   - birth_age / post_arrival need DOB / the accepted-intake anchor. A missing anchor is allowed
//     ONLY for the approved anchor_missing_catch_up basis, and only while the vaccine family has no
//     accepted or trusted administration. A catch-up row whose anchor exists gets the normal floor.
//   - after_previous_completion needs the accepted/trusted previous administration.
//   - A plan-governed second-wave vaccine (any trigger, any purpose) needs every first-wave vaccine
//     administered and due_at >= business day of the latest of them + the plan delay.
func decideVaccinationWrite(w vaccinationWrite, in vaccinationWriteInputs) error {
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
	c := in.Contract
	if c == nil {
		// Not a vaccination rule (or the goat is gone): nothing clinical to prove, but a
		// catch-up exception can only ever be granted to vaccination work.
		if basis != domain.ScheduleBasisAnchored {
			return fmt.Errorf("%w: schedule basis %s requires a vaccination rule", ports.ErrBeforeVaccinationAgeFloor, basis)
		}
		return nil
	}
	vaccine := c.VaccineName
	if strings.TrimSpace(vaccine) == "" {
		vaccine = c.VaccineCode
	}
	family := c.VaccineCode
	if strings.TrimSpace(family) == "" {
		family = c.VaccineName
	}

	policy, err := vaccinepurpose.DecodePolicy(c.PolicyJSON)
	if err != nil {
		return fmt.Errorf("obligation: decode vaccination procurement policy: %w", err)
	}
	decision := vaccinepurpose.Resolve(policy, c.Purpose, c.Species, vaccine)
	if !decision.Applicable {
		return fmt.Errorf("%w: %s", ports.ErrVaccinationNotApplicable, decision.Reason)
	}

	var floor time.Time
	switch strings.ToLower(strings.TrimSpace(c.TriggerType)) {
	case "birth_age", "post_arrival":
		anchor, anchorName := c.DOB, "birth date"
		if strings.EqualFold(strings.TrimSpace(c.TriggerType), "post_arrival") {
			anchor, anchorName = c.ArrivalAt, "arrival"
		}
		if anchor != nil {
			floor = biztime.BusinessDayStart(*anchor).AddDate(0, 0, int(c.OffsetDays))
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
		if administered := laterOf(in.History.latestAdministered(family), in.History.latestAnchored(family)); administered != nil {
			return fmt.Errorf("%w: catch-up basis is invalid for %s with administration/anchor history on %s",
				ports.ErrBeforeVaccinationAgeFloor, family, biztime.BusinessDate(*administered))
		}
	case "after_previous_completion":
		// The PREVIOUS dose may also be an attested vaccination anchor, exactly as generation
		// chains follow-ups from anchor_admins. Anchors never count as first-wave evidence.
		administered := laterOf(in.History.latestAdministered(family), in.History.latestAnchored(family))
		if administered == nil {
			return fmt.Errorf("%w: previous completion anchor is missing", ports.ErrBeforeVaccinationAgeFloor)
		}
		anchor := biztime.BusinessDayStart(*administered)
		if strings.EqualFold(strings.TrimSpace(c.Repeat), "yearly") {
			floor = anchor.AddDate(1, 0, 0)
		} else {
			gap := c.OffsetDays
			if c.MinGapDays > gap {
				gap = c.MinGapDays
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
			administered := in.History.latestAdministered(required)
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
// read uses ($1 tenant, $2 goat ids). It returns (goat_id, canonical family, latest administered_at)
// with the family canonicalized like vaccinepurpose.Normalize. Its channels and filters mirror
// generation's RecentVaccineAdministrationsForGoats exactly, so persistence is never looser (or
// stricter) than the schedule that proposed the date:
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
  SELECT vc.goat_id, vc.administered_at,
         COALESCE(NULLIF(pr.eligibility_json->'vaccine'->>'code',''), NULLIF(pv.rule_dsl->'vaccine'->>'code',''), '') AS vaccine_code
  FROM vaccination_completions vc
  JOIN obligation_instances oi ON oi.tenant_id = vc.tenant_id AND oi.obligation_id = vc.obligation_id
  JOIN protocol_versions pv ON pv.tenant_id = oi.tenant_id AND pv.protocol_version_id = oi.protocol_version_id
  LEFT JOIN protocol_rules pr ON pr.tenant_id = oi.tenant_id AND pr.protocol_version_id = oi.protocol_version_id AND pr.rule_id = oi.rule_id
  WHERE vc.tenant_id = $1 AND vc.goat_id = ANY($2::uuid[])
    AND vc.status = 'accepted' AND vc.verified_at IS NOT NULL
  UNION ALL
  SELECT ev.goat_id, ev.administered_at,
         COALESCE(NULLIF(pr.eligibility_json->'vaccine'->>'code',''), NULLIF(ev.metadata->>'vaccine_code',''), NULLIF(ev.vaccine_name,''), NULLIF(pv.rule_dsl->'vaccine'->>'code',''), '')
  FROM procurement_hf_vaccination_evidence ev
  JOIN procurement_load_goats plg ON plg.tenant_id = ev.tenant_id AND plg.load_id = ev.load_id AND plg.goat_id = ev.goat_id
  JOIN proof_artifacts proof ON proof.tenant_id = ev.tenant_id AND proof.proof_id = ev.proof_ref_id AND proof.upload_state = 'completed'
  JOIN protocol_versions pv ON pv.tenant_id = ev.tenant_id AND pv.protocol_version_id = ev.protocol_version_id
  LEFT JOIN protocol_rules pr ON pr.tenant_id = ev.tenant_id AND pr.protocol_version_id = ev.protocol_version_id AND pr.rule_id = ev.rule_id
  WHERE ev.tenant_id = $1 AND ev.goat_id = ANY($2::uuid[])
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
  SELECT ph.goat_id, ph.administered_at,
         COALESCE(NULLIF(pr.eligibility_json->'vaccine'->>'code',''), NULLIF(ph.vaccine_code,''), NULLIF(pv.rule_dsl->'vaccine'->>'code',''), '')
  FROM vaccination_prearrival_history_entries ph
  JOIN protocol_versions pv ON pv.tenant_id = ph.tenant_id AND pv.protocol_version_id = ph.protocol_version_id
  LEFT JOIN protocol_rules pr ON pr.tenant_id = ph.tenant_id AND pr.protocol_version_id = ph.protocol_version_id AND pr.rule_id = ph.rule_id
  WHERE ph.tenant_id = $1 AND ph.goat_id = ANY($2::uuid[])
    AND ph.review_status = 'accepted' AND ph.reviewed_at IS NOT NULL
)
SELECT goat_id,
       lower(replace(replace(replace(replace(vaccine_code,' ',''),'_',''),'+',''),'-','')) AS family,
       max(administered_at)
FROM administrations
GROUP BY 1, 2`

// vaccineAnchorEventSQL is generation's anchor_admins, byte-for-byte (the shared
// vaccinationanchor definition), grouped to (goat_id, canonical family, latest anchor) for
// $1 tenant and $2 goat ids. A write is judged as of the business day it is made on: anchors
// strictly before today (Asia/Kolkata) count, resolved through the protocol version published and
// effective today for the goat's tenant/park scope -- exactly what the generation pass that
// proposed the date read.
var vaccineAnchorEventSQL = `
WITH anchor_admins AS (` + vaccinationanchor.AnchorAdministrationsSQL("$1", "$2::uuid[]", "(now() AT TIME ZONE 'Asia/Kolkata')::date") + `
)
SELECT goat_id::uuid,
       lower(replace(replace(replace(replace(vaccine_code,' ',''),'_',''),'+',''),'-','')) AS family,
       max(administered_at)
FROM anchor_admins
GROUP BY 1, 2`

func laterOf(a, b *time.Time) *time.Time {
	if a == nil {
		return b
	}
	if b != nil && b.After(*a) {
		return b
	}
	return a
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
		if i+1 < vaccinationWriteMaxSerializationRetries {
			// Jittered backoff so two writers that just collided do not retry in lockstep.
			backoff := time.Duration(10*(i+1))*time.Millisecond + time.Duration(rand.Int64N(int64(20*time.Millisecond)))
			select {
			case <-ctx.Done():
				return zero, errors.Join(ctx.Err(), lastErr)
			case <-time.After(backoff):
			}
		}
	}
	return zero, fmt.Errorf("obligation: vaccination write lost %d serialization retries: %w", vaccinationWriteMaxSerializationRetries, lastErr)
}
