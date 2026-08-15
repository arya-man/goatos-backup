package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/vgoats/goatos/backend/internal/health/diagnosis"
	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/health/ports"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

// DiagnosisRepository persists observation runs and opens the courses a
// confirmation authorises.
//
// It reuses this package's Repository for its pool, timeout and clock so the two
// halves of Health share one connection budget and one notion of now.
type DiagnosisRepository struct{ *Repository }

// NewDiagnosisRepository wraps an existing health Repository.
func NewDiagnosisRepository(base *Repository) *DiagnosisRepository {
	return &DiagnosisRepository{Repository: base}
}

// SubmitObservation resolves the animal, runs the engine, and stores the whole
// proposal in one transaction with its audit and outbox rows.
//
// The animal's facts are read HERE, under a row share lock, rather than accepted
// from the caller: sex and status gate whole diagnoses, so a client able to
// assert them could steer the result far more easily than by mis-ticking a form.
func (r *DiagnosisRepository) SubmitObservation(
	ctx context.Context,
	in domain.SubmitObservationInput,
	evaluate ports.EvaluateFunc,
) (domain.SubmitObservationResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.SubmitObservationResult{}, fmt.Errorf("health: begin observation: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	// Exact idempotency replay: the same key with the same body returns the
	// original result and runs no side effects. A different body under the same
	// key is a conflict, never a silent overwrite.
	if existing, ok, err := r.replayObservation(ctx, tx, in); err != nil {
		return domain.SubmitObservationResult{}, err
	} else if ok {
		if err := tx.Commit(ctx); err != nil {
			return domain.SubmitObservationResult{}, err
		}
		committed = true
		return existing, nil
	}

	facts, err := loadGoatFacts(ctx, tx, in.TenantID, in.GoatID)
	if err != nil {
		return domain.SubmitObservationResult{}, err
	}
	if facts.LifecycleStatus != "alive" {
		return domain.SubmitObservationResult{}, ports.ErrGoatNotAlive
	}
	animal, err := domain.ResolveAnimal(facts)
	if err != nil {
		return domain.SubmitObservationResult{}, err
	}

	// Reconcile needs the animal's OPEN problems as register ids. Without them a
	// follow-up form reads as a fresh diagnosis and every open course is
	// duplicated.
	openProblems, err := loadOpenRegisterRules(ctx, tx, in.TenantID, in.GoatID)
	if err != nil {
		return domain.SubmitObservationResult{}, err
	}
	dctx := in.Context
	dctx.Open = openProblems

	proposal, confirmable := evaluate(animal, in.Findings, dctx)

	formJSON, err := json.Marshal(in.Findings)
	if err != nil {
		return domain.SubmitObservationResult{}, fmt.Errorf("health: encode form: %w", err)
	}
	proposalJSON, err := json.Marshal(proposal)
	if err != nil {
		return domain.SubmitObservationResult{}, fmt.Errorf("health: encode proposal: %w", err)
	}

	var rejectReason *string
	if !proposal.Valid {
		reason := proposal.RejectReason
		rejectReason = &reason
	}

	var runID string
	err = tx.QueryRow(ctx, `
INSERT INTO health_diagnosis_runs
 (tenant_id,goat_id,register_version,observed_by,business_date,form,proposal,valid,reject_reason,
  scope,housing_acuity,housing_containment,housing_low_competition,status,idempotency_key,request_fingerprint)
VALUES ($1::uuid,$2::uuid,$3,$4::uuid,$5::date,$6::jsonb,$7::jsonb,$8,$9,
  $10,$11,$12,$13,'proposed',$14,$15)
RETURNING health_diagnosis_run_id::text`,
		in.TenantID, in.GoatID, proposal.RegisterVersion, in.ActorID,
		in.BusinessDate.Format("2006-01-02"), formJSON, proposalJSON, proposal.Valid, rejectReason,
		proposal.Scope, proposal.Housing.Acuity, proposal.Housing.Containment, proposal.Housing.LowCompetition,
		in.IdempotencyKey, in.RequestFingerprint).Scan(&runID)
	if err != nil {
		return domain.SubmitObservationResult{}, fmt.Errorf("health: insert diagnosis run: %w", err)
	}

	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID: in.TenantID, ActorID: in.ActorID, ActorType: "operator",
		Action: "health.observation.submitted", ResourceType: "health_diagnosis_run", ResourceID: runID,
		ScopeType: "goat", ScopeID: in.GoatID,
		AfterState: map[string]any{
			"register_version": proposal.RegisterVersion,
			"valid":            proposal.Valid,
			"scope":            proposal.Scope,
			"problems":         proposal.Problems,
			"emergencies":      proposal.Emergencies,
		},
		TraceID: in.TraceID,
	}); err != nil {
		return domain.SubmitObservationResult{}, err
	}

	// Emergencies do not wait for a confirmation, so the event carries them: a
	// consumer that pages the Director must not be gated behind a review.
	if err := insertHealthOutboxFor(ctx, tx, "health_diagnosis_run", in.TenantID, in.ActorID, "health.observation.submitted", runID, in.GoatID, in.TraceID,
		map[string]any{
			"health_diagnosis_run_id": runID,
			"goat_id":                 in.GoatID,
			"register_version":        proposal.RegisterVersion,
			"problems":                proposal.Problems,
			"emergencies":             proposal.Emergencies,
			"field_actions":           proposal.FieldActions,
			"housing_acuity":          proposal.Housing.Acuity,
		}); err != nil {
		return domain.SubmitObservationResult{}, err
	}

	// Which of the proposed diagnoses could actually open a course. Resolved
	// here because it needs the published-card lookup, and shown to the Director
	// BEFORE they decide rather than as a failure afterwards.
	confirmable, err = r.annotateSOPAvailability(ctx, tx, in.TenantID, facts.AgeBand, confirmable)
	if err != nil {
		return domain.SubmitObservationResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.SubmitObservationResult{}, fmt.Errorf("health: commit observation: %w", err)
	}
	committed = true

	return domain.SubmitObservationResult{
		DiagnosisRunID: runID,
		Status:         domain.DiagnosisStatusProposed,
		Proposal:       proposal,
		Confirmable:    confirmable,
	}, nil
}

// annotateSOPAvailability marks each proposed diagnosis with whether its
// treatment card exists, naming the gap when it does not.
//
// One batched lookup over the whole set rather than one query per diagnosis: a
// proposal can carry several problems and a per-item read would be an N+1 on a
// hot write path.
func (r *DiagnosisRepository) annotateSOPAvailability(
	ctx context.Context, tx pgx.Tx, tenantID, ageBand string, in []domain.ConfirmableProblem,
) ([]domain.ConfirmableProblem, error) {
	if len(in) == 0 {
		return in, nil
	}
	keys := make([]string, 0, len(in))
	for _, c := range in {
		if c.DiseaseKey != "" {
			keys = append(keys, c.DiseaseKey)
		}
	}
	published := map[string]bool{}
	if len(keys) > 0 {
		rows, err := tx.Query(ctx, `
SELECT disease_key FROM health_protocol_versions
WHERE tenant_id=$1::uuid AND age_band=$2 AND status='published' AND disease_key = ANY($3::text[])`,
			tenantID, ageBand, keys)
		if err != nil {
			return nil, fmt.Errorf("health: check published cards: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var key string
			if err := rows.Scan(&key); err != nil {
				return nil, err
			}
			published[key] = true
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	out := make([]domain.ConfirmableProblem, 0, len(in))
	for _, c := range in {
		c.SOPAvailable = c.DiseaseKey != "" && published[c.DiseaseKey]
		if !c.SOPAvailable {
			c.BlockedReason = fmt.Sprintf("No treatment plan has been set up for %s yet.", c.SOPRef)
		}
		out = append(out, c)
	}
	return out, nil
}

// loadGoatFacts reads everything the engine needs about the animal, including
// the kidding recency that decides periparturient status.
func loadGoatFacts(ctx context.Context, tx pgx.Tx, tenantID, goatID string) (domain.GoatFacts, error) {
	var facts domain.GoatFacts
	var daysSinceKidding *int
	err := tx.QueryRow(ctx, `
SELECT g.species, g.sex, coalesce(g.age_band,''), coalesce(g.management_stage,''), g.lifecycle_status,
       (SELECT (current_date - max(child.dob))::int
          FROM goat_births b
          JOIN goats child ON child.tenant_id = b.tenant_id AND child.goat_id = b.child_goat_id
         WHERE b.tenant_id = g.tenant_id AND b.mother_goat_id = g.goat_id AND child.dob IS NOT NULL)
FROM goats g
WHERE g.tenant_id=$1::uuid AND g.goat_id=$2::uuid
FOR SHARE`, tenantID, goatID).Scan(
		&facts.Species, &facts.Sex, &facts.AgeBand, &facts.ManagementStage, &facts.LifecycleStatus, &daysSinceKidding)
	if errors.Is(err, pgx.ErrNoRows) {
		return facts, ports.ErrNotFound
	}
	if err != nil {
		return facts, fmt.Errorf("health: read goat facts: %w", err)
	}
	facts.DaysSinceKidding = daysSinceKidding
	return facts, nil
}

// loadOpenRegisterRules returns the register ids of this animal's active
// courses, which is what reconcile compares today's form against.
func loadOpenRegisterRules(ctx context.Context, tx pgx.Tx, tenantID, goatID string) ([]string, error) {
	rows, err := tx.Query(ctx, `
SELECT DISTINCT register_rule_id FROM health_cases
WHERE tenant_id=$1::uuid AND goat_id=$2::uuid AND status='active' AND register_rule_id IS NOT NULL
ORDER BY register_rule_id`, tenantID, goatID)
	if err != nil {
		return nil, fmt.Errorf("health: read open problems: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (r *DiagnosisRepository) replayObservation(ctx context.Context, tx pgx.Tx, in domain.SubmitObservationInput) (domain.SubmitObservationResult, bool, error) {
	var runID, fingerprint, status string
	var proposalJSON []byte
	err := tx.QueryRow(ctx, `
SELECT health_diagnosis_run_id::text, request_fingerprint, status, proposal
FROM health_diagnosis_runs WHERE tenant_id=$1::uuid AND idempotency_key=$2`,
		in.TenantID, in.IdempotencyKey).Scan(&runID, &fingerprint, &status, &proposalJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SubmitObservationResult{}, false, nil
	}
	if err != nil {
		return domain.SubmitObservationResult{}, false, fmt.Errorf("health: check observation idempotency: %w", err)
	}
	if fingerprint != in.RequestFingerprint {
		return domain.SubmitObservationResult{}, false, ports.ErrConflict
	}
	var proposal diagnosis.Proposal
	if err := json.Unmarshal(proposalJSON, &proposal); err != nil {
		return domain.SubmitObservationResult{}, false, fmt.Errorf("health: decode stored proposal: %w", err)
	}
	return domain.SubmitObservationResult{
		DiagnosisRunID:   runID,
		Status:           status,
		Proposal:         proposal,
		IdempotentReplay: true,
	}, true, nil
}

// ConfirmDiagnosis records the Director's decision and opens a course for each
// confirmed diagnosis, in ONE transaction.
//
// Partial success is not an option: a run marked confirmed while a case failed
// to open would leave an animal recorded as diagnosed with no treatment
// scheduled, which reads on every screen as work already done.
func (r *DiagnosisRepository) ConfirmDiagnosis(ctx context.Context, in domain.ConfirmDiagnosisInput) (domain.ConfirmDiagnosisResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.ConfirmDiagnosisResult{}, fmt.Errorf("health: begin confirmation: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	var goatID, status, ageBand string
	var confirmationKey, confirmationFingerprint *string
	var proposalJSON []byte
	err = tx.QueryRow(ctx, `
SELECT r.goat_id::text, r.status, r.proposal, coalesce(g.age_band,''),
       r.confirmation_idempotency_key, r.confirmation_fingerprint
FROM health_diagnosis_runs r
JOIN goats g ON g.tenant_id = r.tenant_id AND g.goat_id = r.goat_id
WHERE r.tenant_id=$1::uuid AND r.health_diagnosis_run_id=$2::uuid
FOR UPDATE OF r`, in.TenantID, in.DiagnosisRunID).Scan(&goatID, &status, &proposalJSON, &ageBand, &confirmationKey, &confirmationFingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ConfirmDiagnosisResult{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.ConfirmDiagnosisResult{}, fmt.Errorf("health: read diagnosis run: %w", err)
	}

	if status != domain.DiagnosisStatusProposed {
		// An exact replay reads back the decision; a different one is refused.
		existing, replay, rerr := r.replayConfirmation(ctx, tx, in, status, confirmationKey, confirmationFingerprint)
		if rerr != nil {
			return domain.ConfirmDiagnosisResult{}, rerr
		}
		if replay {
			if err := tx.Commit(ctx); err != nil {
				return domain.ConfirmDiagnosisResult{}, err
			}
			committed = true
			return existing, nil
		}
		return domain.ConfirmDiagnosisResult{}, ports.ErrDiagnosisAlreadyDecided
	}

	var proposal diagnosis.Proposal
	if err := json.Unmarshal(proposalJSON, &proposal); err != nil {
		return domain.ConfirmDiagnosisResult{}, fmt.Errorf("health: decode stored proposal: %w", err)
	}

	confirmable := domain.ConfirmableFromProposal(proposal)

	plan, err := domain.PlanConfirmation(proposal, confirmable, in.ConfirmedProblems)
	if err != nil {
		if errors.Is(err, domain.ErrConfirmNotProposed) {
			return domain.ConfirmDiagnosisResult{}, fmt.Errorf("%w: %v", ports.ErrDiagnosisNotProposed, err)
		}
		return domain.ConfirmDiagnosisResult{}, fmt.Errorf("%w: %v", ports.ErrDiagnosisNotConfirmable, err)
	}

	opened := make([]domain.OpenedCase, 0, len(plan.Confirmed))
	for _, problem := range plan.Confirmed {
		openedCase, err := r.openCourseFromDiagnosis(ctx, tx, in, goatID, ageBand, problem, proposal)
		if err != nil {
			return domain.ConfirmDiagnosisResult{}, err
		}
		opened = append(opened, openedCase)
	}

	if _, err := tx.Exec(ctx, `
UPDATE health_diagnosis_runs
SET status='confirmed', confirmed_by=$3::uuid, confirmed_at=now(),
    confirmation_idempotency_key=$4, confirmation_fingerprint=$5,
    row_version=row_version+1, updated_at=now()
WHERE tenant_id=$1::uuid AND health_diagnosis_run_id=$2::uuid`,
		in.TenantID, in.DiagnosisRunID, in.ActorID, in.IdempotencyKey, in.RequestFingerprint); err != nil {
		return domain.ConfirmDiagnosisResult{}, fmt.Errorf("health: confirm run: %w", err)
	}

	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID: in.TenantID, ActorID: in.ActorID, ActorType: "director",
		Action: "health.diagnosis.confirmed", ResourceType: "health_diagnosis_run", ResourceID: in.DiagnosisRunID,
		ScopeType: "goat", ScopeID: goatID,
		AfterState: map[string]any{"confirmed": confirmedIDs(plan), "declined": plan.Declined, "opened_cases": len(opened)},
		TraceID:    in.TraceID,
	}); err != nil {
		return domain.ConfirmDiagnosisResult{}, err
	}
	if err := insertHealthOutboxFor(ctx, tx, "health_diagnosis_run", in.TenantID, in.ActorID, "health.diagnosis.confirmed", in.DiagnosisRunID, goatID, in.TraceID,
		map[string]any{
			"health_diagnosis_run_id": in.DiagnosisRunID,
			"goat_id":                 goatID,
			"confirmed":               confirmedIDs(plan),
			"declined":                plan.Declined,
		}); err != nil {
		return domain.ConfirmDiagnosisResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.ConfirmDiagnosisResult{}, fmt.Errorf("health: commit confirmation: %w", err)
	}
	committed = true

	return domain.ConfirmDiagnosisResult{
		DiagnosisRunID: in.DiagnosisRunID,
		Status:         domain.DiagnosisStatusConfirmed,
		OpenedCases:    opened,
		Declined:       plan.Declined,
	}, nil
}

// openCourseFromDiagnosis opens one course, failing closed when its treatment
// card has never been published.
func (r *DiagnosisRepository) openCourseFromDiagnosis(
	ctx context.Context, tx pgx.Tx, in domain.ConfirmDiagnosisInput,
	goatID, ageBand string, problem domain.ConfirmableProblem, proposal diagnosis.Proposal,
) (domain.OpenedCase, error) {
	if problem.DiseaseKey == "" {
		return domain.OpenedCase{}, fmt.Errorf("%w: %s has no treatment card", ports.ErrSOPNotAuthored, problem.ID)
	}

	// FAIL CLOSED. Nine of the register's thirty diagnoses currently point at a
	// card nobody has authored. Opening a case with no steps would show the
	// operator an empty plan, which reads as "nothing to do" rather than "nobody
	// has written this down yet".
	card, err := loadPublishedProtocol(ctx, tx, in.TenantID, problem.DiseaseKey, ageBand)
	if err != nil {
		if errors.Is(err, ports.ErrProtocolNotPublished) {
			return domain.OpenedCase{}, fmt.Errorf("%w: %s (%s)", ports.ErrSOPNotAuthored, problem.SOPRef, problem.DiseaseKey)
		}
		return domain.OpenedCase{}, err
	}

	durationDays, horizonDays, err := domain.CourseShapeFor(problem.ExitType, card.duration)
	if err != nil {
		return domain.OpenedCase{}, err
	}

	// Maintainer decision F: housing decides the visits, the card supplies their
	// content.
	sessions := domain.SessionsForHousing(proposal.Housing.Acuity, proposal.Housing.Containment)
	visits := domain.ScheduleCourse(card.steps, horizonDays, sessions)
	start := biztime.BusinessDayStart(r.now())

	var parkID, shedID *string
	if err := tx.QueryRow(ctx, `SELECT park_id::text, shed_id::text FROM goats WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`,
		in.TenantID, goatID).Scan(&parkID, &shedID); err != nil {
		return domain.OpenedCase{}, fmt.Errorf("health: read goat location: %w", err)
	}

	caseKey := in.IdempotencyKey + ":" + problem.ID
	var caseID string
	err = tx.QueryRow(ctx, `
INSERT INTO health_cases
 (tenant_id,goat_id,health_protocol_version_id,disease_key,disease_name,age_band,start_date,
  duration_days,exit_type,status,park_id,shed_id,diagnosed_by,idempotency_key,request_fingerprint,
  health_diagnosis_run_id,register_rule_id)
VALUES ($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7::date,
  $8,$9,'active',nullif($10,'')::uuid,nullif($11,'')::uuid,$12::uuid,$13,$14,
  $15::uuid,$16)
RETURNING health_case_id::text`,
		in.TenantID, goatID, card.id, card.diseaseKey, card.diseaseName, card.ageBand,
		start.Format("2006-01-02"), durationDays, problem.ExitType, valueOrEmpty(parkID), valueOrEmpty(shedID), in.ActorID,
		caseKey, in.RequestFingerprint, in.DiagnosisRunID, problem.ID).Scan(&caseID)
	if err != nil {
		return domain.OpenedCase{}, fmt.Errorf("health: open course for %s: %w", problem.ID, err)
	}

	sessionCount, err := insertScheduledVisits(ctx, tx, in.TenantID, caseID, goatID, visits, r.now())
	if err != nil {
		return domain.OpenedCase{}, err
	}

	return domain.OpenedCase{
		CaseID:       caseID,
		DiseaseKey:   card.diseaseKey,
		ExitType:     problem.ExitType,
		DurationDays: durationDays,
		SessionCount: sessionCount,
	}, nil
}

// insertScheduledVisits writes one session per housing-determined visit, with
// that visit's steps snapshotted immutably onto it.
func insertScheduledVisits(
	ctx context.Context, tx pgx.Tx, tenantID, caseID, goatID string,
	visits []domain.ScheduledSession, now time.Time,
) (int, error) {
	if len(visits) == 0 {
		return 0, nil
	}
	start := biztime.BusinessDayStart(now)
	var batch pgx.Batch

	for _, visit := range visits {
		date := start.AddDate(0, 0, visit.DayNo-1)
		// sessionHour keeps every due time inside a working shift; the farm is
		// empty 00:00-06:00 and nothing may be scheduled there.
		due := time.Date(date.Year(), date.Month(), date.Day(), sessionHour(visit.Session), 0, 0, 0, date.Location())
		status := "scheduled"
		if !due.After(now) {
			status = "due"
		}
		sessionID := uuid.NewString()
		batch.Queue(`
INSERT INTO health_treatment_sessions
 (health_session_id,tenant_id,health_case_id,goat_id,day_no,business_date,session,due_at,status)
VALUES ($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5,$6::date,$7,$8,$9)`,
			sessionID, tenantID, caseID, goatID, visit.DayNo, date.Format("2006-01-02"), visit.Session, due, status)

		for _, step := range visit.Steps {
			stepStatus := "pending"
			if step.RecordType == "critical_action" {
				// Health records a critical action and hands it off; it never
				// performs one.
				stepStatus = "guarded"
			}
			batch.Queue(`
INSERT INTO health_session_steps (
 tenant_id,health_session_id,source_protocol_step_id,seq,record_type,medicine_name,dosage_text,
 dosage_denominator,medicine_route,instruction,critical_action_type,status
) VALUES ($1::uuid,$2::uuid,nullif($3,'')::uuid,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
				tenantID, sessionID, step.StepID, step.Seq, step.RecordType, step.MedicineName, step.DosageText,
				step.DosageDenominator, step.MedicineRoute, step.Instruction, step.CriticalActionType, stepStatus)
		}
	}
	if err := tx.SendBatch(ctx, &batch).Close(); err != nil {
		return 0, fmt.Errorf("health: snapshot treatment course: %w", err)
	}
	return len(visits), nil
}

func (r *DiagnosisRepository) replayConfirmation(
	ctx context.Context, tx pgx.Tx, in domain.ConfirmDiagnosisInput, status string,
	confirmationKey, confirmationFingerprint *string,
) (domain.ConfirmDiagnosisResult, bool, error) {
	if status != domain.DiagnosisStatusConfirmed {
		return domain.ConfirmDiagnosisResult{}, false, nil
	}
	if confirmationKey == nil || *confirmationKey != in.IdempotencyKey {
		return domain.ConfirmDiagnosisResult{}, false, nil
	}
	if confirmationFingerprint == nil || *confirmationFingerprint != in.RequestFingerprint {
		return domain.ConfirmDiagnosisResult{}, false, ports.ErrConflict
	}
	// projection-review: membership=health_cases for this run; group_key=(tenant_id, health_case_id); join_cardinality=sessions counted in a CORRELATED SUBQUERY, never joined, so the 1:N session side cannot duplicate a case row; pagination=none, one run opens a handful of cases; scope=tenant_id and health_diagnosis_run_id
	//
	// The session count is per CASE. Joining health_treatment_sessions instead
	// would multiply each case by its own session count and report a replayed
	// confirmation as having opened several courses where it opened one.
	rows, err := tx.Query(ctx, `
SELECT health_case_id::text, disease_key, exit_type, duration_days,
       (SELECT count(*) FROM health_treatment_sessions s WHERE s.health_case_id = c.health_case_id)
FROM health_cases c
WHERE c.tenant_id=$1::uuid AND c.health_diagnosis_run_id=$2::uuid
ORDER BY c.disease_key`, in.TenantID, in.DiagnosisRunID)
	if err != nil {
		return domain.ConfirmDiagnosisResult{}, false, fmt.Errorf("health: read confirmed cases: %w", err)
	}
	defer rows.Close()

	var opened []domain.OpenedCase
	for rows.Next() {
		var c domain.OpenedCase
		if err := rows.Scan(&c.CaseID, &c.DiseaseKey, &c.ExitType, &c.DurationDays, &c.SessionCount); err != nil {
			return domain.ConfirmDiagnosisResult{}, false, err
		}
		opened = append(opened, c)
	}
	if err := rows.Err(); err != nil {
		return domain.ConfirmDiagnosisResult{}, false, err
	}
	return domain.ConfirmDiagnosisResult{
		DiagnosisRunID:   in.DiagnosisRunID,
		Status:           domain.DiagnosisStatusConfirmed,
		OpenedCases:      opened,
		IdempotentReplay: true,
	}, true, nil
}

// GetDiagnosisRun reads one run back for the Director's queue and the animal's
// diagnosis history.
func (r *DiagnosisRepository) GetDiagnosisRun(ctx context.Context, tenantID, runID string) (domain.DiagnosisRun, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	var out domain.DiagnosisRun
	var proposalJSON []byte
	var businessDate time.Time
	var ageBand string
	// The goat is joined rather than read separately: the display id, the shed and
	// the age band all come off the same row, and the age band was previously a
	// second round trip for the SOP lookup below.
	//
	// A LEFT JOIN, so a run whose animal has since been hard-deleted still reads
	// back. The assessment is a durable medical record; losing it because the
	// animal row went would be worse than showing it without a name.
	err := r.pool.QueryRow(ctx, `
SELECT dr.health_diagnosis_run_id::text, dr.goat_id::text, dr.register_version, dr.observed_by::text,
       dr.observed_at, dr.business_date, dr.proposal, dr.status, dr.confirmed_by::text, dr.confirmed_at,
       COALESCE(g.display_id, ''), COALESCE(g.age_band, '')
FROM health_diagnosis_runs dr
LEFT JOIN goats g ON g.tenant_id = dr.tenant_id AND g.goat_id = dr.goat_id
WHERE dr.tenant_id=$1::uuid AND dr.health_diagnosis_run_id=$2::uuid`, tenantID, runID).Scan(
		&out.DiagnosisRunID, &out.GoatID, &out.RegisterVersion, &out.ObservedBy, &out.ObservedAt,
		&businessDate, &proposalJSON, &out.Status, &out.ConfirmedBy, &out.ConfirmedAt,
		&out.GoatDisplayID, &ageBand)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DiagnosisRun{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.DiagnosisRun{}, fmt.Errorf("health: read diagnosis run: %w", err)
	}
	out.BusinessDate = businessDate.Format("2006-01-02")
	if err := json.Unmarshal(proposalJSON, &out.Proposal); err != nil {
		return domain.DiagnosisRun{}, fmt.Errorf("health: decode stored proposal: %w", err)
	}

	// Only a run still awaiting a decision offers one. A decided run is history:
	// re-deriving its list would show the Director buttons for a choice already
	// made, and any tap on them fails at the write.
	if out.Status == domain.DiagnosisStatusProposed {
		confirmable := domain.ConfirmableFromProposal(out.Proposal)
		if len(confirmable) > 0 {
			tx, err := r.pool.Begin(ctx)
			if err != nil {
				return domain.DiagnosisRun{}, fmt.Errorf("health: begin sop lookup: %w", err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			out.Confirmable, err = r.annotateSOPAvailability(ctx, tx, tenantID, ageBand, confirmable)
			if err != nil {
				return domain.DiagnosisRun{}, err
			}
		}
	}
	return out, nil
}

func confirmedIDs(plan domain.ConfirmationPlan) []string {
	out := make([]string, 0, len(plan.Confirmed))
	for _, c := range plan.Confirmed {
		out = append(out, c.ID)
	}
	return out
}

// ListDiagnosisRuns serves the Director's queue: one keyset page of assessments,
// newest first.
//
// projection-review: membership=health_diagnosis_runs; group_key=(tenant_id, health_diagnosis_run_id); join_cardinality=goats 1:1 on (tenant_id, goat_id), location resolved separately in Go; pagination=keyset on (observed_at, health_diagnosis_run_id) DESC, never OFFSET; scope=tenant_id always, optional goat_id, optional status
//
//	producer  health_diagnosis_runs, unique on (tenant_id, health_diagnosis_run_id)
//	consumer  one row per health_diagnosis_run_id; no GROUP BY, no aggregate
//	joins     goats            1:1 on (tenant_id, goat_id)   -- goat_id is that table's PK half
//	          location batch   resolved SEPARATELY, in Go, keyed by shed id (no join fan-out)
//
// Every join is 1:1, so nothing can duplicate or drop a run. The page is not an
// aggregate and carries no count, so there is no numerator/denominator to align.
//
// KEYSET, never OFFSET: the queue is ordered newest-first and new observations are
// inserted at the HEAD, so an offset page would re-show or skip rows as a manager
// records animals while the Director scrolls.
func (r *DiagnosisRepository) ListDiagnosisRuns(
	ctx context.Context, f domain.DiagnosisQueueFilter,
) (domain.DiagnosisQueuePage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	cursorAt, cursorID, err := decodeCursor(f.Cursor)
	if err != nil {
		return domain.DiagnosisQueuePage{}, ports.ErrConflict
	}

	page := domain.DiagnosisQueuePage{Items: []domain.DiagnosisQueueItem{}}

	// scale-guard:ignore: one tenant + status keyset page capped at limit+1 (<=51), served by the
	// partial health_diagnosis_runs_pending_idx (tenant_id, status, observed_at DESC, id).
	rows, err := r.pool.Query(ctx, `
SELECT dr.health_diagnosis_run_id::text, dr.goat_id::text, COALESCE(g.display_id, ''),
       COALESCE(g.shed_id::text, ''), dr.observed_at, dr.business_date, dr.status,
       dr.proposal->'problems', dr.proposal->'emergencies', dr.proposal->'unexplained'
FROM health_diagnosis_runs dr
JOIN goats g ON g.tenant_id = dr.tenant_id AND g.goat_id = dr.goat_id
WHERE dr.tenant_id = $1::uuid
  AND ($2 = '' OR dr.status = $2)
  AND ($3 = '' OR dr.goat_id = nullif($3,'')::uuid)
  AND ($4::timestamptz IS NULL
       OR (dr.observed_at, dr.health_diagnosis_run_id) < ($4::timestamptz, nullif($5,'')::uuid))
ORDER BY dr.observed_at DESC, dr.health_diagnosis_run_id DESC
LIMIT $6`, f.TenantID, f.Status, f.GoatID, cursorAt, cursorID, f.Limit+1)
	if err != nil {
		return page, fmt.Errorf("health: list diagnosis runs: %w", err)
	}
	defer rows.Close()

	shedIDs := make([]string, 0, f.Limit+1)
	rowShed := make([]string, 0, f.Limit+1)
	for rows.Next() {
		var it domain.DiagnosisQueueItem
		var shedID string
		var businessDate time.Time
		var problemsJSON, emergenciesJSON, unexplainedJSON []byte
		if err := rows.Scan(&it.DiagnosisRunID, &it.GoatID, &it.GoatDisplayID, &shedID,
			&it.ObservedAt, &businessDate, &it.Status,
			&problemsJSON, &emergenciesJSON, &unexplainedJSON); err != nil {
			return page, err
		}
		it.BusinessDate = businessDate.Format("2006-01-02")

		// The three arrays are decoded in GO rather than unpacked in SQL. That is
		// deliberate: `jsonb_array_elements(x)::text` leaves JSON quoting and turns a
		// JSON null into the four-character string "null", which is non-empty and
		// passes every "is there anything here?" check -- and unnesting a jsonb array
		// in the same SELECT fans the row out, which is how a 1:1 page silently
		// becomes N rows per run.
		it.Problems = decodeStringArray(problemsJSON)
		it.EmergencyCount = len(decodeStringArray(emergenciesJSON))
		it.UnexplainedCount = len(decodeStringArray(unexplainedJSON))

		page.Items = append(page.Items, it)
		rowShed = append(rowShed, shedID)
		if shedID != "" {
			shedIDs = append(shedIDs, shedID)
		}
	}
	if err := rows.Err(); err != nil {
		return page, err
	}

	if len(page.Items) > f.Limit {
		last := page.Items[f.Limit-1]
		next := encodeCursor(last.ObservedAt, last.DiagnosisRunID)
		page.NextCursor = &next
		page.Items = page.Items[:f.Limit]
		rowShed = rowShed[:f.Limit]
	}

	if err := r.attachLocations(ctx, f.TenantID, shedIDs, rowShed, page.Items); err != nil {
		return page, err
	}
	return page, nil
}

// attachLocations resolves every row's shed in ONE round trip.
//
// One query for the whole page, not one per row: a per-row lookup is the banned
// N+1 fan-out, and a 20-row queue would become 21 serial reads. The composition
// itself goes through oploc so the partition rules -- human label never the
// matching key, 'whole' filtered, agree-or-go-bare -- are not re-derived here.
//
// A shed that does not resolve leaves the row's location BLANK. Degrading to the
// animal's name alone is correct; rendering a raw uuid or a dangling separator is
// what this whole convention exists to prevent.
func (r *DiagnosisRepository) attachLocations(
	ctx context.Context, tenantID string, shedIDs, rowShed []string, items []domain.DiagnosisQueueItem,
) error {
	if len(shedIDs) == 0 {
		return nil
	}
	rows, err := r.pool.Query(ctx, oploc.ShedScopedLocationBatchSQL, tenantID, shedIDs)
	if err != nil {
		return fmt.Errorf("health: resolve queue locations: %w", err)
	}
	defer rows.Close()
	byShed, err := oploc.ResolveShedLocations(ctx, rows)
	if err != nil {
		return err
	}
	for i := range items {
		loc, ok := byShed[rowShed[i]]
		if !ok {
			continue
		}
		items[i].ShedName = loc.ShedName
		items[i].PartitionLabel = loc.PartitionLabel
		items[i].OperationalLocationDisplay = loc.Display()
	}
	return nil
}

// decodeStringArray reads one jsonb array of strings.
//
// A missing or malformed array reads as EMPTY rather than failing the page. The
// proposal is a stored document and a queue that will not open because one run's
// blob is odd is worse than one row showing no headline.
func decodeStringArray(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}
