package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

func intPtrValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

const milkFeedingResourceType = "milk_feeding_task"

func (r *Repository) MaterializeMilkFeedingTasks(ctx context.Context, in domain.MilkFeedingMaterializeRequest) (domain.MilkFeedingMaterializeResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	day := biztime.BusinessDayStart(in.FeedingDate)
	if in.FeedingDate.IsZero() {
		day = biztime.BusinessDayStart(time.Now())
	}
	command, err := r.pool.Exec(ctx, `
WITH eligible_farms AS (
  SELECT g.tenant_id, g.park_id, count(*)::integer AS head_count
  FROM goats g
  JOIN locations p ON p.tenant_id = g.tenant_id AND p.location_id = g.park_id
    AND p.location_type = 'park' AND p.status = 'active'
  WHERE g.tenant_id = $1::uuid AND g.merged_into_goat_id IS NULL AND g.lifecycle_status = 'alive'
    AND upper(regexp_replace(trim(coalesce(g.management_stage, '')), '[^A-Za-z0-9]+', '', 'g'))
      IN ('K1', 'K2', 'K3', 'ICUKID', 'QUARANTINEMILKKID')
  GROUP BY g.tenant_id, g.park_id
), sessions(session_no, due_time) AS (
  VALUES (1, time '08:00'), (2, time '12:00'), (3, time '16:00'), (4, time '21:00')
)
INSERT INTO milk_feeding_tasks (tenant_id, park_id, shed_id, feeding_date, session_no, due_at, head_count)
SELECT e.tenant_id, e.park_id, NULL, $2::date, x.session_no,
       ($2::date + x.due_time) AT TIME ZONE 'Asia/Kolkata', e.head_count
FROM eligible_farms e CROSS JOIN sessions x
ON CONFLICT DO NOTHING`, in.TenantID, day.Format("2006-01-02"))
	if err != nil {
		return domain.MilkFeedingMaterializeResult{}, fmt.Errorf("counts: materialize milk feeding tasks: %w", err)
	}
	return domain.MilkFeedingMaterializeResult{FeedingDate: day.Format("2006-01-02"), Inserted: command.RowsAffected()}, nil
}

// projection-review: producer unique columns=(tenant_id,park_id,feeding_date,session_no);
// consumer match columns are identical. locations are 1:1 labels and watchlist is pre-aggregated
// to one JSON value per task farm, so neither join multiplies the task grain.
func (r *Repository) ListMilkFeedingTasks(ctx context.Context, in domain.MilkFeedingQuery) (domain.MilkFeedingPage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	day := biztime.BusinessDayStart(in.FeedingDate)
	if in.FeedingDate.IsZero() {
		day = biztime.BusinessDayStart(time.Now())
	}
	limit := in.Limit
	if limit <= 0 || limit > 20 {
		limit = 20
	}
	offset := in.Offset
	if offset < 0 || offset > 5000 {
		offset = 0
	}
	// Hard-bounded result set, not a growable offset. The predicate pins ONE feeding_date and
	// farm-grain rows (shed_id IS NULL), and milk_feeding_tasks_farm_session_uidx is UNIQUE on
	// (tenant_id, park_id, feeding_date, session_no) with session_no CHECK-constrained to 1..4, so
	// the whole filtered set is at most parks*4 rows (8 today). The offset can never walk a deep
	// tail; it is clamped to 5000 above purely as an input guard.
	rows, err := r.pool.Query(ctx, `-- scale-guard:ignore: bounded to parks*4 rows (one feeding_date, farm grain, session_no 1..4); offset cannot reach a deep tail
WITH watchlists AS MATERIALIZED (
  SELECT w.tenant_id, w.park_id,
         jsonb_agg(jsonb_build_object('goat_id', w.goat_id::text, 'consecutive_yes', w.consecutive_yes,
           'remarks', coalesce(w.remarks,''), 'added_date', w.added_date::text, 'added_session', w.added_session)
           ORDER BY w.added_date, w.added_session, w.goat_id)::text AS payload
  FROM milk_feeding_farm_watchlist w WHERE w.tenant_id = $1::uuid GROUP BY w.tenant_id, w.park_id
)
SELECT t.task_id::text, t.park_id::text, coalesce(nullif(p.location_code,''),p.name,''),
       t.feeding_date::text,
       t.session_no, to_char(t.due_at AT TIME ZONE 'Asia/Kolkata','HH24:MI'),
       now() >= t.due_at, t.due_at,
       CASE WHEN now() < t.due_at THEN 'Available at ' || to_char(t.due_at AT TIME ZONE 'Asia/Kolkata','HH24:MI') ELSE '' END,
       t.head_count,
       t.status, t.current_attempt_no, coalesce(t.rework_reason,''), coalesce(w.payload,'[]')
FROM milk_feeding_tasks t
JOIN locations p ON p.tenant_id=t.tenant_id AND p.location_id=t.park_id
LEFT JOIN watchlists w ON w.tenant_id=t.tenant_id AND w.park_id=t.park_id
WHERE t.tenant_id=$1::uuid AND t.feeding_date=$2::date
  AND t.status <> 'retired' AND t.shed_id IS NULL
  AND ($3='' OR t.park_id=nullif($3,'')::uuid)
  AND ($4=0 OR t.session_no=$4)
ORDER BY p.location_code,p.name,t.session_no,t.task_id LIMIT $5 OFFSET $6`,
		in.TenantID, day.Format("2006-01-02"), ptrValue(in.ParkID), intPtrValue(in.SessionNo), limit+1, offset)
	if err != nil {
		return domain.MilkFeedingPage{}, fmt.Errorf("counts: list milk feeding tasks: %w", err)
	}
	defer rows.Close()
	items := make([]domain.MilkFeedingTask, 0, limit+1)
	for rows.Next() {
		var item domain.MilkFeedingTask
		var watchlistJSON string
		if err := rows.Scan(&item.TaskID, &item.ParkID, &item.ParkLabel, &item.FeedingDate, &item.SessionNo, &item.DueTime, &item.Available, &item.AvailableAt, &item.BlockedReason, &item.HeadCount, &item.VerificationStatus, &item.AttemptNo, &item.ReworkReason, &watchlistJSON); err != nil {
			return domain.MilkFeedingPage{}, fmt.Errorf("counts: scan milk feeding task: %w", err)
		}
		item.CompletionID = item.TaskID
		if err := json.Unmarshal([]byte(watchlistJSON), &item.Watchlist); err != nil {
			return domain.MilkFeedingPage{}, fmt.Errorf("counts: decode milk feeding watchlist: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return domain.MilkFeedingPage{}, fmt.Errorf("counts: iterate milk feeding tasks: %w", err)
	}
	hasMore := len(items) > int(limit)
	if hasMore {
		items = items[:limit]
	}
	generatedAt := time.Now().UTC() // india-date-guard:ignore: owner=goatos issue=GH-india-date scope=response-absolute-instant expiry=2026-12-31
	return domain.MilkFeedingPage{FeedingDate: day.Format("2006-01-02"), GeneratedAt: generatedAt, Items: items, Limit: limit, Offset: offset, HasMore: hasMore}, nil
}

func (r *Repository) SubmitMilkFeeding(ctx context.Context, in domain.MilkFeedingSubmission) (domain.MilkFeedingSubmissionResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	proofMap := map[string]string{domain.MilkFeedingStepCleanBottles: in.Proofs.CleanBottlesProofRef, domain.MilkFeedingStepMixingAndFilling: in.Proofs.MixingAndFillingProofRef}
	proofJSON, _ := json.Marshal(proofMap)
	answerJSON, err := json.Marshal(in.Answers)
	if err != nil {
		return domain.MilkFeedingSubmissionResult{}, err
	}
	fingerprint := milkFeedingFingerprint(in, answerJSON, proofJSON)
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.MilkFeedingSubmissionResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var replay domain.MilkFeedingSubmissionResult
	var storedFingerprint string
	err = tx.QueryRow(ctx, `SELECT a.request_fingerprint,t.task_id::text,t.status,a.attempt_no,t.row_version FROM milk_feeding_attempts a JOIN milk_feeding_tasks t ON t.tenant_id=a.tenant_id AND t.task_id=a.task_id WHERE a.tenant_id=$1::uuid AND a.idempotency_key=$2`, in.TenantID, in.IdempotencyKey).Scan(&storedFingerprint, &replay.CompletionID, &replay.Status, &replay.AttemptNo, &replay.RowVersion)
	if err == nil {
		if storedFingerprint != fingerprint {
			return domain.MilkFeedingSubmissionResult{}, ports.ErrIdempotencyConflict
		}
		replay.NeedsEnqueue = replay.Status == domain.MilkFeedingVerificationPending
		if err := tx.Commit(ctx); err != nil {
			return domain.MilkFeedingSubmissionResult{}, err
		}
		return replay, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.MilkFeedingSubmissionResult{}, err
	}
	var status, parkID, feedingDate string
	var dueAt time.Time
	var sessionNo int
	var currentAttempt, rowVersion int32
	err = tx.QueryRow(ctx, `SELECT status,park_id::text,feeding_date::text,session_no,due_at,current_attempt_no,row_version FROM milk_feeding_tasks WHERE tenant_id=$1::uuid AND task_id=$2::uuid AND status <> 'retired' AND shed_id IS NULL FOR UPDATE`, in.TenantID, in.TaskID).Scan(&status, &parkID, &feedingDate, &sessionNo, &dueAt, &currentAttempt, &rowVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.MilkFeedingSubmissionResult{}, ports.ErrMilkFeedingNotFound
	}
	if err != nil {
		return domain.MilkFeedingSubmissionResult{}, err
	}
	if parkID != in.ParkID || feedingDate != in.FeedingDate.Format("2006-01-02") || sessionNo != in.SessionNo {
		return domain.MilkFeedingSubmissionResult{}, ports.ErrMilkFeedingNotFound
	}
	if !domain.MilkFeedingAvailableAt(dueAt, in.SubmittedAt) {
		return domain.MilkFeedingSubmissionResult{}, ports.ErrMilkFeedingNotYetAvailable
	}
	if status == domain.MilkFeedingVerificationPending {
		return domain.MilkFeedingSubmissionResult{}, ports.ErrMilkFeedingPending
	}
	if status == domain.MilkFeedingVerificationCompleted {
		return domain.MilkFeedingSubmissionResult{}, ports.ErrMilkFeedingCompleted
	}
	watchlist, err := loadMilkFeedingWatchlist(ctx, tx, in.TenantID, in.ParkID)
	if err != nil {
		return domain.MilkFeedingSubmissionResult{}, err
	}
	if err := in.Answers.Validate(watchlist); err != nil {
		return domain.MilkFeedingSubmissionResult{}, err
	}
	newIDs := make([]string, 0, len(in.Answers.NewRefusals))
	for _, refusal := range in.Answers.NewRefusals {
		newIDs = append(newIDs, strings.TrimSpace(refusal.GoatID))
	}
	if len(newIDs) > 0 {
		var matched int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM goats WHERE tenant_id=$1::uuid AND park_id=$2::uuid AND lifecycle_status='alive' AND merged_into_goat_id IS NULL AND goat_id=ANY($3::uuid[])`, in.TenantID, in.ParkID, newIDs).Scan(&matched); err != nil {
			return domain.MilkFeedingSubmissionResult{}, err
		}
		if matched != len(newIDs) {
			return domain.MilkFeedingSubmissionResult{}, fmt.Errorf("counts: every new refusal goat must be alive in the task farm")
		}
	}
	attempt := currentAttempt + 1
	if err := tx.QueryRow(ctx, `UPDATE milk_feeding_tasks SET status='pending_verification',current_attempt_no=$3,assigned_operator_id=coalesce(assigned_operator_id,$4::uuid),submitted_by=$4::uuid,submitted_at=$5,verified_by=NULL,verified_at=NULL,rework_reason=NULL,row_version=row_version+1,updated_at=now() WHERE tenant_id=$1::uuid AND task_id=$2::uuid RETURNING row_version`, in.TenantID, in.TaskID, attempt, in.SubmittedBy, in.SubmittedAt.UTC()).Scan(&rowVersion); err != nil {
		return domain.MilkFeedingSubmissionResult{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO milk_feeding_attempts(tenant_id,task_id,attempt_no,answers,proof_refs,submitted_by,submitted_at,idempotency_key,request_fingerprint) VALUES($1::uuid,$2::uuid,$3,$4::jsonb,$5::jsonb,$6::uuid,$7,$8,$9)`, in.TenantID, in.TaskID, attempt, answerJSON, proofJSON, in.SubmittedBy, in.SubmittedAt.UTC(), in.IdempotencyKey, fingerprint); err != nil {
		return domain.MilkFeedingSubmissionResult{}, err
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{TenantID: in.TenantID, ActorID: in.SubmittedBy, ActorType: "operator", Action: "milk.feeding.pending_verification", ResourceType: milkFeedingResourceType, ResourceID: in.TaskID, ScopeType: "park", ScopeID: in.ParkID, AfterState: map[string]any{"feeding_date": feedingDate, "session_no": sessionNo, "attempt_no": attempt, "status": domain.MilkFeedingVerificationPending}, Metadata: map[string]any{"proof_steps": proofMap}, TraceID: in.TraceID}); err != nil {
		return domain.MilkFeedingSubmissionResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.MilkFeedingSubmissionResult{}, err
	}
	return domain.MilkFeedingSubmissionResult{CompletionID: in.TaskID, Status: domain.MilkFeedingVerificationPending, AttemptNo: attempt, RowVersion: rowVersion, NeedsEnqueue: true}, nil
}

func loadMilkFeedingWatchlist(ctx context.Context, tx pgx.Tx, tenantID, parkID string) ([]domain.MilkFeedingWatchlistKid, error) {
	rows, err := tx.Query(ctx, `SELECT goat_id::text,consecutive_yes,coalesce(remarks,''),added_date::text,added_session FROM milk_feeding_farm_watchlist WHERE tenant_id=$1::uuid AND park_id=$2::uuid ORDER BY added_date,added_session,goat_id FOR UPDATE`, tenantID, parkID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.MilkFeedingWatchlistKid{}
	for rows.Next() {
		var k domain.MilkFeedingWatchlistKid
		if err := rows.Scan(&k.GoatID, &k.ConsecutiveYes, &k.Remarks, &k.AddedDate, &k.AddedSession); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func milkFeedingFingerprint(in domain.MilkFeedingSubmission, answers, proofs []byte) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{in.TaskID, in.ParkID, in.FeedingDate.Format("2006-01-02"), fmt.Sprint(in.SessionNo), string(answers), string(proofs)}, "\x00")))
	return hex.EncodeToString(sum[:])
}

func (r *Repository) ApplyVerifiedMilkFeeding(ctx context.Context, in domain.MilkFeedingVerdictCommand) (bool, error) {
	return r.transitionMilkFeedingVerdict(ctx, in, domain.MilkFeedingVerificationCompleted)
}
func (r *Repository) BounceMilkFeedingForRework(ctx context.Context, in domain.MilkFeedingVerdictCommand) (bool, error) {
	return r.transitionMilkFeedingVerdict(ctx, in, domain.MilkFeedingVerificationRework)
}

func (r *Repository) transitionMilkFeedingVerdict(ctx context.Context, in domain.MilkFeedingVerdictCommand, target string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var parkID, feedingDate string
	var sessionNo int
	var attemptNo int32
	var answersJSON []byte
	err = tx.QueryRow(ctx, `UPDATE milk_feeding_tasks SET status=$3,verified_by=CASE WHEN $3='completed' THEN nullif($4,'')::uuid ELSE NULL END,verified_at=CASE WHEN $3='completed' THEN $5 ELSE NULL END,rework_reason=CASE WHEN $3='rework' THEN nullif($6,'') ELSE NULL END,row_version=row_version+1,updated_at=now() WHERE tenant_id=$1::uuid AND task_id=$2::uuid AND status='pending_verification' AND shed_id IS NULL RETURNING park_id::text,feeding_date::text,session_no,current_attempt_no`, in.TenantID, in.CompletionID, target, in.VerifiedBy, in.OccurredAt.UTC(), strings.TrimSpace(in.Reason)).Scan(&parkID, &feedingDate, &sessionNo, &attemptNo)
	if errors.Is(err, pgx.ErrNoRows) {
		var exists bool
		if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM milk_feeding_tasks WHERE tenant_id=$1::uuid AND task_id=$2::uuid)`, in.TenantID, in.CompletionID).Scan(&exists); e != nil {
			return false, e
		}
		if !exists {
			return false, ports.ErrMilkFeedingNotFound
		}
		if e := tx.Commit(ctx); e != nil {
			return false, e
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if target == domain.MilkFeedingVerificationCompleted {
		if err := tx.QueryRow(ctx, `SELECT answers FROM milk_feeding_attempts WHERE tenant_id=$1::uuid AND task_id=$2::uuid AND attempt_no=$3`, in.TenantID, in.CompletionID, attemptNo).Scan(&answersJSON); err != nil {
			return false, err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM milk_feeding_farm_watchlist w USING jsonb_to_recordset(($3::jsonb)->'watchlist_answers') AS x(goat_id text,drank_milk boolean) WHERE w.tenant_id=$1::uuid AND w.park_id=$2::uuid AND w.goat_id=nullif(x.goat_id,'')::uuid AND x.drank_milk AND w.consecutive_yes=1`, in.TenantID, parkID, answersJSON); err != nil {
			return false, err
		}
		if _, err := tx.Exec(ctx, `UPDATE milk_feeding_farm_watchlist w SET consecutive_yes=CASE WHEN x.drank_milk THEN 1 ELSE 0 END,updated_at=now() FROM jsonb_to_recordset(($3::jsonb)->'watchlist_answers') AS x(goat_id text,drank_milk boolean) WHERE w.tenant_id=$1::uuid AND w.park_id=$2::uuid AND w.goat_id=nullif(x.goat_id,'')::uuid`, in.TenantID, parkID, answersJSON); err != nil {
			return false, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO milk_feeding_farm_watchlist(tenant_id,park_id,goat_id,remarks,added_date,added_session) SELECT $1::uuid,$2::uuid,nullif(x.goat_id,'')::uuid,nullif(x.remarks,''),$4::date,$5 FROM jsonb_to_recordset(($3::jsonb)->'new_refusals') AS x(goat_id text,remarks text) ON CONFLICT(tenant_id,park_id,goat_id) DO NOTHING`, in.TenantID, parkID, answersJSON, feedingDate, sessionNo); err != nil {
			return false, err
		}
	}
	action := "milk.feeding.completed"
	if target == domain.MilkFeedingVerificationRework {
		action = "milk.feeding.rework"
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{TenantID: in.TenantID, ActorID: in.VerifiedBy, ActorType: "verifier", Action: action, ResourceType: milkFeedingResourceType, ResourceID: in.CompletionID, ScopeType: "park", ScopeID: parkID, AfterState: map[string]any{"feeding_date": feedingDate, "session_no": sessionNo, "attempt_no": attemptNo, "status": target, "reason": strings.TrimSpace(in.Reason)}, Metadata: map[string]any{"park_id": parkID}, TraceID: in.TraceID}); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}
