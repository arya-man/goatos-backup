// Package postgres persists Animal purchases: loads, candidates, decisions, idempotency, audit
// and the decision's outbox event -- each write in ONE transaction.
package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/animalpurchase/domain"
	"github.com/vgoats/goatos/backend/internal/animalpurchase/ports"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

const (
	idemScopeLoad      = "animal_purchase.load"
	idemScopeCandidate = "animal_purchase.candidate"
	idemScopeDecision  = "animal_purchase.decision"

	uniqueViolation = "23505"
)

// Repository is the Postgres adapter.
type Repository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewRepository(pool *pgxpool.Pool, timeout time.Duration) *Repository {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Repository{pool: pool, timeout: timeout}
}

var _ ports.Repository = (*Repository)(nil)

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// ---- SQL, package-level so the query-plan tooling and the scale guard can reach it ----

const (
	sqlReserveIdempotency = `
INSERT INTO idempotency_keys (idempotency_key, tenant_id, scope, request_hash, status)
VALUES ($1, $2::uuid, $3, $4, 'started')
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING idempotency_key`

	sqlReadIdempotency = `
SELECT request_hash, COALESCE(result_id::text, '') FROM idempotency_keys WHERE idempotency_key = $1`

	sqlCompleteIdempotency = `
UPDATE idempotency_keys
SET status = 'completed', result_type = $2, result_id = $3::uuid, completed_at = now()
WHERE idempotency_key = $1`

	loadColumns = `
  l.load_id::text, l.tenant_id::text, l.load_ref, l.vendor_id::text, l.vendor_name,
  COALESCE(l.park_id::text, ''), l.farm_label, l.expected_count, l.notes, l.status,
  COALESCE(l.recorded_by::text, ''), l.created_at, l.updated_at, l.row_version,
  c.total, c.pending, c.accepted, c.rejected, l.questionnaire_version, l.sop_answers,
  COALESCE((SELECT max(wm.display_name) FROM public.workforce_members wm
            WHERE wm.tenant_id = l.tenant_id AND wm.user_id = l.recorded_by AND wm.status = 'active'), '')`

	// loadCountsJoin pre-aggregates the many side (candidates) to ONE row per load before the
	// join, so the load page is 1:1 with loads and the counts are whole-load, never page sums.
	//
	// projection-review: membership=animal_purchase_loads (the served window itself, PK load_id) with the many side animal_purchase_candidates (PK candidate_id, unique (tenant_id, load_id, seq_no)); group_key=(tenant_id, load_id) on both sides so the lateral groups candidates by the load own key and attaches exactly one row per load; join_cardinality=lateral 1:1 by construction (one aggregate row, ON true) so no load repeats and the four counts are FILTER clauses over the SAME candidate set, numerator (pending/accepted/rejected) and denominator (total) ranging over identical keys; pagination=keyset (created_at DESC, load_id DESC) LIMIT limit+1 over loads only, counts computed per load and independent of the page, and the review read takes its whole-filter counts BEFORE its cursor predicate; scope=tenant_id on every branch, status buckets pending/accepted/rejected disjoint by CHECK with total their union
	loadCountsJoin = `
LEFT JOIN LATERAL (
  SELECT count(*)::int AS total,
         count(*) FILTER (WHERE c.decision = 'pending')::int AS pending,
         count(*) FILTER (WHERE c.decision = 'accepted')::int AS accepted,
         count(*) FILTER (WHERE c.decision = 'rejected')::int AS rejected
  FROM public.animal_purchase_candidates c
  WHERE c.tenant_id = l.tenant_id AND c.load_id = l.load_id
) c ON true`

	sqlGetLoad = `SELECT ` + loadColumns + `
FROM public.animal_purchase_loads l ` + loadCountsJoin + `
WHERE l.tenant_id = $1::uuid AND l.load_id = $2::uuid`

	sqlListLoadsFirst = `SELECT ` + loadColumns + `
FROM public.animal_purchase_loads l ` + loadCountsJoin + `
WHERE l.tenant_id = $1::uuid
ORDER BY l.created_at DESC, l.load_id DESC
LIMIT $2`

	sqlListLoadsAfter = `SELECT ` + loadColumns + `
FROM public.animal_purchase_loads l ` + loadCountsJoin + `
WHERE l.tenant_id = $1::uuid AND (l.created_at, l.load_id) < ($3::timestamptz, $4::uuid)
ORDER BY l.created_at DESC, l.load_id DESC
LIMIT $2`

	sqlVendorName = `SELECT business_name FROM public.procurement_vendors
WHERE tenant_id = $1::uuid AND vendor_id = $2::uuid AND status <> 'banned'`

	sqlInsertLoad = `
INSERT INTO public.animal_purchase_loads (
  tenant_id, load_ref, vendor_id, vendor_name, park_id, farm_label, expected_count, notes,
  recorded_by, idempotency_key, questionnaire_version, sop_answers
) VALUES (
  $1::uuid, $2, $3::uuid, $4,
  (SELECT l.location_id FROM public.locations l
    WHERE l.tenant_id = $1::uuid AND l.location_type = 'park' AND upper(l.location_code) = $5 LIMIT 1),
  $5, $6, $7, nullif($8, '')::uuid, $9, $10, $11
) RETURNING load_id::text`

	sqlTouchLoad = `UPDATE public.animal_purchase_loads SET updated_at = now(), row_version = row_version + 1
WHERE tenant_id = $1::uuid AND load_id = $2::uuid`

	sqlLockLoad = `SELECT status FROM public.animal_purchase_loads
WHERE tenant_id = $1::uuid AND load_id = $2::uuid FOR UPDATE`

	candidateColumns = `
  c.candidate_id::text, c.tenant_id::text, c.load_id::text, l.load_ref, c.seq_no,
  c.species, c.sex, c.breed, c.age_months, c.weight_kg::float8, COALESCE(c.condition, ''), c.temp_tag, c.notes,
  COALESCE(c.video_proof_ref, ''), c.decision, COALESCE(c.decided_by::text, ''), c.decided_by_name, c.decided_at,
  c.decision_note, COALESCE(c.recorded_by::text, ''), c.created_at, c.updated_at, c.row_version,
  c.questionnaire_version, c.sop_answers, COALESCE(c.field_verdict, ''), c.height_cm::float8, c.rectal_temp_c::float8`

	candidateFrom = `
FROM public.animal_purchase_candidates c
JOIN public.animal_purchase_loads l ON l.tenant_id = c.tenant_id AND l.load_id = c.load_id`

	sqlGetCandidate = `SELECT ` + candidateColumns + candidateFrom + `
WHERE c.tenant_id = $1::uuid AND c.candidate_id = $2::uuid`

	sqlInsertCandidate = `
INSERT INTO public.animal_purchase_candidates (
  tenant_id, load_id, seq_no, species, sex, breed, weight_kg, temp_tag, notes,
  questionnaire_version, sop_answers, field_verdict, height_cm, rectal_temp_c,
  recorded_by, idempotency_key
) VALUES (
  $1::uuid, $2::uuid,
  (SELECT COALESCE(max(seq_no), 0) + 1 FROM public.animal_purchase_candidates WHERE tenant_id = $1::uuid AND load_id = $2::uuid),
  $3, $4, $5, $6, $7, $8,
  $9, $10::jsonb, nullif($11, ''), $12, $13,
  nullif($14, '')::uuid, $15
) RETURNING candidate_id::text, seq_no`

	sqlInsertCandidateMedia = `
INSERT INTO public.animal_purchase_candidate_media (tenant_id, candidate_id, slot, position, proof_ref)
SELECT $1::uuid, $2::uuid, s.slot, s.position, s.proof_ref
FROM unnest($3::text[], $4::int[], $5::text[]) AS s(slot, position, proof_ref)`

	// Media for a PAGE of candidates in one read (the N+1 the read paths must not do).
	sqlCandidateMedia = `
SELECT candidate_id::text, slot, proof_ref
FROM public.animal_purchase_candidate_media
WHERE tenant_id = $1::uuid AND candidate_id = ANY($2::uuid[])
ORDER BY candidate_id, slot, position`

	sqlListCandidates = `SELECT ` + candidateColumns + candidateFrom + `
WHERE c.tenant_id = $1::uuid AND c.load_id = $2::uuid AND c.seq_no > $3
ORDER BY c.seq_no
LIMIT $4`

	// Review reads: the filter predicate is composed from a fixed set of clauses (load, decision)
	// and the keyset cursor; every variant is a prefix of this shape.
	sqlReviewCountsBase = `SELECT count(*)::int,
  count(*) FILTER (WHERE c.decision = 'pending')::int,
  count(*) FILTER (WHERE c.decision = 'accepted')::int,
  count(*) FILTER (WHERE c.decision = 'rejected')::int
FROM public.animal_purchase_candidates c WHERE c.tenant_id = $1::uuid`

	sqlReviewPageBase = `SELECT ` + candidateColumns + candidateFrom + `
WHERE c.tenant_id = $1::uuid`

	sqlReviewPageOrder = `
ORDER BY c.created_at, c.candidate_id
LIMIT `

	sqlCandidateGuard = `SELECT decision, row_version, load_id::text FROM public.animal_purchase_candidates
WHERE tenant_id = $1::uuid AND candidate_id = $2::uuid FOR UPDATE`

	sqlActorName = `SELECT display_name FROM public.workforce_members
WHERE tenant_id = $1::uuid AND user_id = $2::uuid AND status = 'active' ORDER BY updated_at DESC LIMIT 1`

	sqlDecide = `UPDATE public.animal_purchase_candidates
SET decision = $3, decided_by = nullif($4, '')::uuid, decided_by_name = $5, decided_at = now(),
    decision_note = $6, updated_at = now(), row_version = row_version + 1
WHERE tenant_id = $1::uuid AND candidate_id = $2::uuid AND decision = 'pending' AND row_version = $7`

	// projection-review: membership=goats alive rows with a breed (PK goat_id); group_key=breed alone, one output row per distinct breed spelling; join_cardinality=no join at all, a single-table GROUP BY so each goat contributes to exactly one breed count; pagination=LIMIT 30 most common breeds, a bounded suggestion list and not a paged read; scope=tenant_id, and the count is only an ordering key never shown on a screen
	sqlBreedSuggestions = `SELECT breed FROM (
  SELECT g.breed, count(*) AS n FROM public.goats g
  WHERE g.tenant_id = $1::uuid AND COALESCE(g.breed, '') <> '' AND g.lifecycle_status = 'alive'
  GROUP BY g.breed) b ORDER BY n DESC, breed LIMIT 30`

	sqlInsertOutbox = `
INSERT INTO outbox_messages (
  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, status
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5, $6::uuid,
  $7, $8::jsonb, $9::jsonb, $10, 'pending'
)`
)

// ---- idempotency (the procurement shape, scoped by tenant + operation) ----

func idemScopedKey(tenantID, scope, key string) string {
	return tenantID + ":" + scope + ":" + strings.TrimSpace(key)
}

func fingerprint(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:])
}

type reservation struct {
	proceed  bool
	resultID string
}

func reserveIdempotency(ctx context.Context, tx pgx.Tx, tenantID, scope, key, hash string) (reservation, error) {
	scoped := idemScopedKey(tenantID, scope, key)
	var claimed string
	err := tx.QueryRow(ctx, sqlReserveIdempotency, scoped, tenantID, scope, hash).Scan(&claimed)
	if err == nil {
		return reservation{proceed: true}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return reservation{}, err
	}
	var existingHash, resultID string
	if err := tx.QueryRow(ctx, sqlReadIdempotency, scoped).Scan(&existingHash, &resultID); err != nil {
		return reservation{}, err
	}
	if existingHash != hash {
		return reservation{}, ports.ErrIdempotencyConflict
	}
	return reservation{proceed: false, resultID: resultID}, nil
}

func completeIdempotency(ctx context.Context, tx pgx.Tx, tenantID, scope, key, resultType, resultID string) error {
	_, err := tx.Exec(ctx, sqlCompleteIdempotency, idemScopedKey(tenantID, scope, key), resultType, resultID)
	return err
}

func isUnique(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == constraint
}

// ---- loads ----

func scanLoad(row pgx.Row) (domain.Load, error) {
	var l domain.Load
	var answers []byte
	err := row.Scan(&l.LoadID, &l.TenantID, &l.LoadRef, &l.VendorID, &l.VendorName,
		&l.ParkID, &l.FarmLabel, &l.ExpectedCount, &l.Notes, &l.Status,
		&l.RecordedBy, &l.CreatedAt, &l.UpdatedAt, &l.RowVersion,
		&l.Counts.Total, &l.Counts.Pending, &l.Counts.Accepted, &l.Counts.Rejected,
		&l.QuestionnaireVersion, &answers, &l.RecordedByName)
	if err == nil && len(answers) > 0 {
		_ = json.Unmarshal(answers, &l.Answers)
	}
	return l, err
}

func (r *Repository) getLoad(ctx context.Context, q rowQuerier, tenantID, loadID string) (domain.Load, error) {
	l, err := scanLoad(q.QueryRow(ctx, sqlGetLoad, tenantID, loadID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Load{}, ports.ErrLoadNotFound
	}
	if err != nil {
		return domain.Load{}, fmt.Errorf("animal purchase: get load: %w", err)
	}
	return l, nil
}

func (r *Repository) GetLoad(ctx context.Context, tenantID, loadID string) (domain.Load, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if !isUUID(loadID) {
		return domain.Load{}, ports.ErrLoadNotFound
	}
	return r.getLoad(ctx, r.pool, tenantID, loadID)
}

func (r *Repository) ListLoads(ctx context.Context, tenantID string, cursor domain.Cursor, limit int) (ports.LoadPage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var rows pgx.Rows
	var err error
	if cursor.ID != "" {
		rows, err = r.pool.Query(ctx, sqlListLoadsAfter, tenantID, limit+1, cursor.CreatedAt, cursor.ID)
	} else {
		rows, err = r.pool.Query(ctx, sqlListLoadsFirst, tenantID, limit+1)
	}
	if err != nil {
		return ports.LoadPage{}, fmt.Errorf("animal purchase: list loads: %w", err)
	}
	defer rows.Close()
	var out []domain.Load
	for rows.Next() {
		l, err := scanLoad(rows)
		if err != nil {
			return ports.LoadPage{}, fmt.Errorf("animal purchase: scan load: %w", err)
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return ports.LoadPage{}, err
	}
	page := ports.LoadPage{Loads: out}
	if len(out) > limit {
		page.Loads = out[:limit]
		last := page.Loads[limit-1]
		page.NextCursor = domain.EncodeCursor(domain.Cursor{Kind: domain.CursorKindLoad, CreatedAt: last.CreatedAt.UTC().Format(time.RFC3339Nano), ID: last.LoadID})
	}
	return page, nil
}

func (r *Repository) CreateLoad(ctx context.Context, p ports.CreateLoadParams) (domain.Load, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Load{}, fmt.Errorf("animal purchase: begin create load: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	w := p.Write
	hash := fingerprint("load", w.LoadRef, w.VendorID, w.FarmLabel, strconv.Itoa(w.ExpectedCount), w.Notes)
	res, err := reserveIdempotency(ctx, tx, p.TenantID, idemScopeLoad, p.IdempotencyKey, hash)
	if err != nil {
		return domain.Load{}, err
	}
	if !res.proceed {
		if err := tx.Commit(ctx); err != nil {
			return domain.Load{}, err
		}
		return r.GetLoad(ctx, p.TenantID, res.resultID)
	}

	// The vendor must be a real register row in this tenant; its name is frozen on the load.
	if !isUUID(w.VendorID) {
		return domain.Load{}, ports.ErrVendorNotFound
	}
	var vendorName string
	err = tx.QueryRow(ctx, sqlVendorName, p.TenantID, w.VendorID).Scan(&vendorName)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Load{}, ports.ErrVendorNotFound
	}
	if err != nil {
		return domain.Load{}, fmt.Errorf("animal purchase: vendor lookup: %w", err)
	}

	var loadID string
	loadAnswers, err := json.Marshal(w.Answers)
	if err != nil {
		return domain.Load{}, fmt.Errorf("animal purchase: encode load answers: %w", err)
	}
	err = tx.QueryRow(ctx, sqlInsertLoad,
		p.TenantID, w.LoadRef, w.VendorID, vendorName, w.FarmLabel, w.ExpectedCount, w.Notes, p.ActorID, p.IdempotencyKey,
		p.QuestionnaireVersion, loadAnswers).Scan(&loadID)
	if isUnique(err, "animal_purchase_loads_ref_uq") {
		return domain.Load{}, ports.ErrLoadRefTaken
	}
	if err != nil {
		return domain.Load{}, fmt.Errorf("animal purchase: insert load: %w", err)
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID: p.TenantID, ActorID: p.ActorID, ActorType: "human",
		Action: "procurement.animal_purchase.load_recorded", ResourceType: "animal_purchase_load", ResourceID: loadID,
		Metadata: map[string]any{
			"domain": "procurement", "module": "animal_purchases", "load_ref": w.LoadRef,
			"vendor_id": w.VendorID, "vendor_name": vendorName, "farm": w.FarmLabel, "expected_count": w.ExpectedCount,
		},
	}); err != nil {
		return domain.Load{}, fmt.Errorf("animal purchase: audit load: %w", err)
	}
	if err := completeIdempotency(ctx, tx, p.TenantID, idemScopeLoad, p.IdempotencyKey, "animal_purchase_load", loadID); err != nil {
		return domain.Load{}, err
	}
	load, err := r.getLoad(ctx, tx, p.TenantID, loadID)
	if err != nil {
		return domain.Load{}, err
	}
	// PROCUREMENT IS SOP-DRIVEN END TO END (2026-09-20): the load's intake workflow is opened by
	// the tasks engine from this event, INSIDE the same transaction as the row -- a load that
	// exists always has its steps, and a load that rolled back never announced itself.
	if err := emitLoadRecorded(ctx, tx, p.TenantID, p.ActorID, p.IdempotencyKey, load); err != nil {
		return domain.Load{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Load{}, fmt.Errorf("animal purchase: commit load: %w", err)
	}
	return load, nil
}

// ---- candidates ----

func scanCandidate(row pgx.Row) (domain.Candidate, error) {
	var c domain.Candidate
	var answers []byte
	err := row.Scan(&c.CandidateID, &c.TenantID, &c.LoadID, &c.LoadRef, &c.SeqNo,
		&c.Species, &c.Sex, &c.Breed, &c.AgeMonths, &c.WeightKg, &c.Condition, &c.TempTag, &c.Notes,
		&c.VideoProofRef, &c.Decision, &c.DecidedBy, &c.DecidedByName, &c.DecidedAt,
		&c.DecisionNote, &c.RecordedBy, &c.CreatedAt, &c.UpdatedAt, &c.RowVersion,
		&c.QuestionnaireVersion, &answers, &c.FieldVerdict, &c.HeightCm, &c.RectalTempC)
	if err != nil {
		return c, err
	}
	c.Answers = domain.Answers{}
	if len(answers) > 0 {
		_ = json.Unmarshal(answers, &c.Answers)
	}
	c.Media = domain.MediaRefs{}
	return c, nil
}

func (r *Repository) getCandidate(ctx context.Context, q rowQuerier, tenantID, candidateID string) (domain.Candidate, error) {
	c, err := scanCandidate(q.QueryRow(ctx, sqlGetCandidate, tenantID, candidateID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Candidate{}, ports.ErrCandidateNotFound
	}
	if err != nil {
		return domain.Candidate{}, fmt.Errorf("animal purchase: get candidate: %w", err)
	}
	rows := []domain.Candidate{c}
	if err := attachMedia(ctx, q, tenantID, rows); err != nil {
		return domain.Candidate{}, err
	}
	return rows[0], nil
}

type rowsQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// attachMedia fills every candidate's Media from ONE read over the page's ids.
func attachMedia(ctx context.Context, q any, tenantID string, candidates []domain.Candidate) error {
	if len(candidates) == 0 {
		return nil
	}
	querier, ok := q.(rowsQuerier)
	if !ok {
		return nil
	}
	ids := make([]string, 0, len(candidates))
	index := make(map[string]int, len(candidates))
	for i := range candidates {
		ids = append(ids, candidates[i].CandidateID)
		index[candidates[i].CandidateID] = i
		if candidates[i].Media == nil {
			candidates[i].Media = domain.MediaRefs{}
		}
	}
	rows, err := querier.Query(ctx, sqlCandidateMedia, tenantID, ids)
	if err != nil {
		return fmt.Errorf("animal purchase: candidate media: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, slot, ref string
		if err := rows.Scan(&id, &slot, &ref); err != nil {
			return fmt.Errorf("animal purchase: scan media: %w", err)
		}
		if i, ok := index[id]; ok {
			candidates[i].Media[slot] = append(candidates[i].Media[slot], ref)
		}
	}
	return rows.Err()
}

func (r *Repository) GetCandidate(ctx context.Context, tenantID, candidateID string) (domain.Candidate, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if !isUUID(candidateID) {
		return domain.Candidate{}, ports.ErrCandidateNotFound
	}
	return r.getCandidate(ctx, r.pool, tenantID, candidateID)
}

func (r *Repository) AddCandidate(ctx context.Context, p ports.AddCandidateParams) (domain.Candidate, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if !isUUID(p.LoadID) {
		return domain.Candidate{}, ports.ErrLoadNotFound
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Candidate{}, fmt.Errorf("animal purchase: begin add candidate: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	w := p.Write
	answersJSON, err := json.Marshal(w.Answers)
	if err != nil {
		return domain.Candidate{}, fmt.Errorf("animal purchase: answers: %w", err)
	}
	mediaJSON, _ := json.Marshal(w.Media)
	hash := fingerprint("candidate", p.LoadID, string(answersJSON), string(mediaJSON))
	res, err := reserveIdempotency(ctx, tx, p.TenantID, idemScopeCandidate, p.IdempotencyKey, hash)
	if err != nil {
		return domain.Candidate{}, err
	}
	if !res.proceed {
		if err := tx.Commit(ctx); err != nil {
			return domain.Candidate{}, err
		}
		return r.GetCandidate(ctx, p.TenantID, res.resultID)
	}

	// The load's row lock serialises seq_no assignment: two phones adding at once cannot both
	// claim max+1.
	var status string
	err = tx.QueryRow(ctx, sqlLockLoad, p.TenantID, p.LoadID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Candidate{}, ports.ErrLoadNotFound
	}
	if err != nil {
		return domain.Candidate{}, fmt.Errorf("animal purchase: lock load: %w", err)
	}
	if status != domain.LoadStatusOpen {
		return domain.Candidate{}, ports.ErrLoadClosed
	}

	var candidateID string
	var seqNo int
	err = tx.QueryRow(ctx, sqlInsertCandidate,
		p.TenantID, p.LoadID, w.Species, w.Sex, w.Breed, w.WeightKg, w.TempTag, w.Notes,
		p.QuestionnaireVersion, answersJSON, w.FieldVerdict, w.HeightCm, w.RectalTempC,
		p.ActorID, p.IdempotencyKey).Scan(&candidateID, &seqNo)
	if err != nil {
		return domain.Candidate{}, fmt.Errorf("animal purchase: insert candidate: %w", err)
	}
	var slots []string
	var positions []int
	var refs []string
	for _, q := range w.Catalog.MediaSlots() {
		for i, ref := range w.Media[q.Slot] {
			slots = append(slots, q.Slot)
			positions = append(positions, i)
			refs = append(refs, ref)
		}
	}
	if len(refs) > 0 {
		if _, err := tx.Exec(ctx, sqlInsertCandidateMedia, p.TenantID, candidateID, slots, positions, refs); err != nil {
			if isUnique(err, "animal_purchase_candidate_media_proof_uq") {
				return domain.Candidate{}, ports.ErrMediaAlreadyUsed
			}
			return domain.Candidate{}, fmt.Errorf("animal purchase: insert candidate media: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, sqlTouchLoad, p.TenantID, p.LoadID); err != nil {
		return domain.Candidate{}, fmt.Errorf("animal purchase: touch load: %w", err)
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID: p.TenantID, ActorID: p.ActorID, ActorType: "human",
		Action: "procurement.animal_purchase.candidate_recorded", ResourceType: "animal_purchase_candidate", ResourceID: candidateID,
		Metadata: map[string]any{
			"domain": "procurement", "module": "animal_purchases", "load_id": p.LoadID, "seq_no": seqNo,
			"species": w.Species, "sex": w.Sex, "breed": w.Breed, "field_verdict": w.FieldVerdict,
			"questionnaire_version": p.QuestionnaireVersion, "media_count": len(refs),
		},
	}); err != nil {
		return domain.Candidate{}, fmt.Errorf("animal purchase: audit candidate: %w", err)
	}
	if err := completeIdempotency(ctx, tx, p.TenantID, idemScopeCandidate, p.IdempotencyKey, "animal_purchase_candidate", candidateID); err != nil {
		return domain.Candidate{}, err
	}
	c, err := r.getCandidate(ctx, tx, p.TenantID, candidateID)
	if err != nil {
		return domain.Candidate{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Candidate{}, fmt.Errorf("animal purchase: commit candidate: %w", err)
	}
	return c, nil
}

func (r *Repository) ListCandidates(ctx context.Context, tenantID, loadID string, cursor domain.Cursor, limit int) (ports.CandidatePage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if !isUUID(loadID) {
		return ports.CandidatePage{}, ports.ErrLoadNotFound
	}
	load, err := r.getLoad(ctx, r.pool, tenantID, loadID)
	if err != nil {
		return ports.CandidatePage{}, err
	}
	rows, err := r.pool.Query(ctx, sqlListCandidates, tenantID, loadID, cursor.Seq, limit+1)
	if err != nil {
		return ports.CandidatePage{}, fmt.Errorf("animal purchase: list candidates: %w", err)
	}
	defer rows.Close()
	page, err := collectCandidates(rows, limit, func(last domain.Candidate) string {
		return domain.EncodeCursor(domain.Cursor{Kind: domain.CursorKindCandidate, Seq: last.SeqNo})
	})
	if err != nil {
		return ports.CandidatePage{}, err
	}
	rows.Close()
	if err := attachMedia(ctx, r.pool, tenantID, page.Candidates); err != nil {
		return ports.CandidatePage{}, err
	}
	page.Counts = load.Counts
	return page, nil
}

func (r *Repository) ListReview(ctx context.Context, tenantID string, q ports.ReviewQuery) (ports.CandidatePage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	// Filter predicates and their args, shared by the counts read and the page read.
	filterArgs := []any{tenantID}
	where := ``
	if q.LoadID != "" {
		if !isUUID(q.LoadID) {
			return ports.CandidatePage{}, ports.ErrLoadNotFound
		}
		filterArgs = append(filterArgs, q.LoadID)
		where += fmt.Sprintf(` AND c.load_id = $%d::uuid`, len(filterArgs))
	}
	// The recorded-on window keeps created_at bare (SARGable on the (tenant, created_at) index);
	// the service already turned the IST dates into instants.
	if !q.From.IsZero() {
		filterArgs = append(filterArgs, q.From)
		where += fmt.Sprintf(` AND c.created_at >= $%d::timestamptz`, len(filterArgs))
	}
	if !q.To.IsZero() {
		filterArgs = append(filterArgs, q.To)
		where += fmt.Sprintf(` AND c.created_at < $%d::timestamptz`, len(filterArgs))
	}
	// Whole-filter counts BEFORE the decision chip and the cursor predicate: the chips show the
	// same numbers whichever chip is picked and whichever page is open, in ONE read (the handler
	// used to re-run the whole list for them).
	var counts domain.DecisionCounts
	if err := r.pool.QueryRow(ctx, sqlReviewCountsBase+where, filterArgs...).
		Scan(&counts.Total, &counts.Pending, &counts.Accepted, &counts.Rejected); err != nil {
		return ports.CandidatePage{}, fmt.Errorf("animal purchase: review counts: %w", err)
	}
	if q.Decision != "" {
		filterArgs = append(filterArgs, q.Decision)
		where += fmt.Sprintf(` AND c.decision = $%d`, len(filterArgs))
	}
	pageArgs := append([]any{}, filterArgs...)
	cursorWhere := ``
	if q.Cursor.ID != "" {
		pageArgs = append(pageArgs, q.Cursor.CreatedAt, q.Cursor.ID)
		cursorWhere = fmt.Sprintf(` AND (c.created_at, c.candidate_id) > ($%d::timestamptz, $%d::uuid)`, len(pageArgs)-1, len(pageArgs))
	}
	pageArgs = append(pageArgs, q.Limit+1)
	rows, err := r.pool.Query(ctx, sqlReviewPageBase+where+cursorWhere+sqlReviewPageOrder+"$"+strconv.Itoa(len(pageArgs)), pageArgs...)
	if err != nil {
		return ports.CandidatePage{}, fmt.Errorf("animal purchase: list review: %w", err)
	}
	defer rows.Close()
	page, err := collectCandidates(rows, q.Limit, func(last domain.Candidate) string {
		return domain.EncodeCursor(domain.Cursor{Kind: domain.CursorKindReview, CreatedAt: last.CreatedAt.UTC().Format(time.RFC3339Nano), ID: last.CandidateID})
	})
	if err != nil {
		return ports.CandidatePage{}, err
	}
	rows.Close()
	if err := attachMedia(ctx, r.pool, tenantID, page.Candidates); err != nil {
		return ports.CandidatePage{}, err
	}
	page.Counts = counts
	return page, nil
}

func collectCandidates(rows pgx.Rows, limit int, next func(domain.Candidate) string) (ports.CandidatePage, error) {
	var out []domain.Candidate
	for rows.Next() {
		c, err := scanCandidate(rows)
		if err != nil {
			return ports.CandidatePage{}, fmt.Errorf("animal purchase: scan candidate: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return ports.CandidatePage{}, err
	}
	page := ports.CandidatePage{Candidates: out}
	if len(out) > limit {
		page.Candidates = out[:limit]
		page.NextCursor = next(page.Candidates[limit-1])
	}
	return page, nil
}

// Decide records the CEO's accept / reject: version-fenced, once only, audited, and announced on
// the outbox inside the same transaction so the phone's push cannot outrun the commit.
func (r *Repository) Decide(ctx context.Context, p ports.DecideParams) (domain.Candidate, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if !isUUID(p.CandidateID) {
		return domain.Candidate{}, ports.ErrCandidateNotFound
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Candidate{}, fmt.Errorf("animal purchase: begin decide: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	hash := fingerprint("decision", p.CandidateID, p.Write.Decision, p.Write.Note, strconv.Itoa(p.Write.RowVersion))
	res, err := reserveIdempotency(ctx, tx, p.TenantID, idemScopeDecision, p.IdempotencyKey, hash)
	if err != nil {
		return domain.Candidate{}, err
	}
	if !res.proceed {
		if err := tx.Commit(ctx); err != nil {
			return domain.Candidate{}, err
		}
		return r.GetCandidate(ctx, p.TenantID, p.CandidateID)
	}

	// One lean, row-locked read for the fence: the full row (with its captures) is read once,
	// AFTER the update, for the audit and the event.
	var before struct {
		Decision   string
		RowVersion int
		LoadID     string
	}
	if err := tx.QueryRow(ctx, sqlCandidateGuard, p.TenantID, p.CandidateID).Scan(&before.Decision, &before.RowVersion, &before.LoadID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Candidate{}, ports.ErrCandidateNotFound
		}
		return domain.Candidate{}, fmt.Errorf("animal purchase: read candidate: %w", err)
	}
	if before.Decision != domain.DecisionPending {
		return domain.Candidate{}, ports.ErrAlreadyDecided
	}
	if before.RowVersion != p.Write.RowVersion {
		return domain.Candidate{}, ports.ErrRowVersionMismatch
	}
	actorName := strings.TrimSpace(p.ActorName)
	if actorName == "" && isUUID(p.ActorID) {
		_ = tx.QueryRow(ctx, sqlActorName, p.TenantID, p.ActorID).Scan(&actorName)
	}
	tag, err := tx.Exec(ctx, sqlDecide,
		p.TenantID, p.CandidateID, p.Write.Decision, p.ActorID, actorName, p.Write.Note, p.Write.RowVersion)
	if err != nil {
		return domain.Candidate{}, fmt.Errorf("animal purchase: update decision: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return domain.Candidate{}, ports.ErrRowVersionMismatch
	}
	if _, err := tx.Exec(ctx, sqlTouchLoad, p.TenantID, before.LoadID); err != nil {
		return domain.Candidate{}, fmt.Errorf("animal purchase: touch load: %w", err)
	}
	after, err := r.getCandidate(ctx, tx, p.TenantID, p.CandidateID)
	if err != nil {
		return domain.Candidate{}, err
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID: p.TenantID, ActorID: p.ActorID, ActorType: "human",
		Action: "procurement.animal_purchase.decided", ResourceType: "animal_purchase_candidate", ResourceID: p.CandidateID,
		BeforeState: map[string]any{"decision": before.Decision, "row_version": before.RowVersion},
		AfterState:  map[string]any{"decision": after.Decision, "row_version": after.RowVersion, "decided_by_name": actorName},
		Metadata: map[string]any{
			"domain": "procurement", "module": "animal_purchases", "load_id": after.LoadID, "load_ref": after.LoadRef,
			"seq_no": after.SeqNo, "decision": after.Decision, "note": p.Write.Note,
		},
	}); err != nil {
		return domain.Candidate{}, fmt.Errorf("animal purchase: audit decision: %w", err)
	}
	load, err := r.getLoad(ctx, tx, p.TenantID, after.LoadID)
	if err != nil {
		return domain.Candidate{}, err
	}
	if err := emitDecided(ctx, tx, p.TenantID, p.ActorID, p.IdempotencyKey, after, load); err != nil {
		return domain.Candidate{}, err
	}
	if err := completeIdempotency(ctx, tx, p.TenantID, idemScopeDecision, p.IdempotencyKey, "animal_purchase_candidate", p.CandidateID); err != nil {
		return domain.Candidate{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Candidate{}, fmt.Errorf("animal purchase: commit decision: %w", err)
	}
	return after, nil
}

// BreedSuggestions is the herd's own breed vocabulary, most common first, bounded.
func (r *Repository) BreedSuggestions(ctx context.Context, tenantID string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, sqlBreedSuggestions, tenantID)
	if err != nil {
		return nil, fmt.Errorf("animal purchase: breed suggestions: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var b string
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ---- outbox ----

const (
	DecidedEventType     = "procurement.animal_purchase.decided"
	decidedSchemaVersion = "1.0.0"
	decidedSchemaRef     = "contracts/jsonschema/domain-event-envelope.schema.json"
	decidedTopic         = "procurement.events"
	aggregateType        = "animal_purchase_candidate"
)

// LoadRecordedEventType announces a purchase load the moment it is opened. Its ONE consumer today
// is the tasks engine, which opens the load's intake workflow from the published
// `procurement.animal_purchase_intake` SOP (PROCUREMENT IS SOP-DRIVEN END TO END, 2026-09-20).
const (
	LoadRecordedEventType = "procurement.animal_purchase.load_recorded"
	loadAggregateType     = "animal_purchase_load"
)

// LoadRecordedPayload is the event body the workflow opener reads. The park is carried because
// the workflow is a PARK's work: the load was already resolved to one when the row was inserted,
// and re-deriving it in the consumer would be a second answer to a question already answered.
type LoadRecordedPayload struct {
	LoadID        string `json:"load_id"`
	LoadRef       string `json:"load_ref"`
	ParkID        string `json:"park_id"`
	FarmLabel     string `json:"farm_label"`
	VendorID      string `json:"vendor_id"`
	VendorName    string `json:"vendor_name"`
	ExpectedCount int    `json:"expected_count"`
	RecordedBy    string `json:"recorded_by"`
	OccurredAt    string `json:"occurred_at"`
}

// DecidedPayload is the event body the notification bridge reads.
type DecidedPayload struct {
	CandidateID   string `json:"candidate_id"`
	LoadID        string `json:"load_id"`
	LoadRef       string `json:"load_ref"`
	SeqNo         int    `json:"seq_no"`
	Species       string `json:"species"`
	Sex           string `json:"sex"`
	Breed         string `json:"breed"`
	FarmLabel     string `json:"farm_label"`
	VendorName    string `json:"vendor_name"`
	Decision      string `json:"decision"`
	DecidedByName string `json:"decided_by_name"`
	DecisionNote  string `json:"decision_note"`
	RecordedBy    string `json:"recorded_by"`
	OccurredAt    string `json:"occurred_at"`
	Pending       int    `json:"load_pending"`
	Accepted      int    `json:"load_accepted"`
	Rejected      int    `json:"load_rejected"`
}

// outboxWriter is the slice of a transaction the emitter needs, so a test can capture the
// envelope without a database.
type outboxWriter interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

// emitLoadRecorded writes the load-recorded envelope inside the insert transaction.
func emitLoadRecorded(ctx context.Context, tx outboxWriter, tenantID, actorID, idempotencyKey string, load domain.Load) error {
	eventID := uuid.NewString()
	now := time.Now().UTC()
	payload := LoadRecordedPayload{
		LoadID: load.LoadID, LoadRef: load.LoadRef, ParkID: load.ParkID, FarmLabel: load.FarmLabel,
		VendorID: load.VendorID, VendorName: load.VendorName, ExpectedCount: load.ExpectedCount,
		RecordedBy: load.RecordedBy, OccurredAt: now.Format(time.RFC3339Nano),
	}
	var actor any
	if strings.TrimSpace(actorID) != "" {
		actor = actorID
	}
	envelope, err := json.Marshal(map[string]any{
		"event_id":       eventID,
		"event_type":     LoadRecordedEventType,
		"schema_version": decidedSchemaVersion,
		"schema_ref":     decidedSchemaRef,
		"occurred_at":    now.Format("2006-01-02T15:04:05.000000Z"),
		"recorded_at":    now.Format("2006-01-02T15:04:05.000000Z"),
		"aggregate_type": loadAggregateType,
		"aggregate_id":   load.LoadID,
		"producer": map[string]any{
			"module":  "animal_purchases",
			"service": "goatos-api",
			"version": nil,
		},
		"idempotency_key": idempotencyKey,
		"actor": map[string]any{
			"actor_type": "human",
			"actor_id":   actor,
			"actor_ref":  nil,
		},
		"subject_type":     loadAggregateType,
		"subject_id":       load.LoadID,
		"visibility_scope": map[string]any{"tenant_id": tenantID},
		"evidence_refs": []map[string]string{{
			"evidence_type": "source_record",
			"evidence_id":   "animal_purchase_load:" + load.LoadID,
		}},
		"payload":  payload,
		"trace_id": "animal-purchase-load:" + load.LoadID,
	})
	if err != nil {
		return err
	}
	headers, err := json.Marshal(map[string]any{
		"actor_id":      actorID,
		"farm":          load.FarmLabel,
		"business_date": biztime.BusinessDate(now),
	})
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, sqlInsertOutbox, tenantID, eventID, LoadRecordedEventType, decidedSchemaVersion, loadAggregateType, load.LoadID,
		decidedTopic, envelope, headers, idempotencyKey); err != nil {
		return fmt.Errorf("animal purchase: outbox load recorded: %w", err)
	}
	return nil
}

func emitDecided(ctx context.Context, tx outboxWriter, tenantID, actorID, idempotencyKey string, c domain.Candidate, load domain.Load) error {
	eventID := uuid.NewString()
	now := time.Now().UTC()
	payload := DecidedPayload{
		CandidateID: c.CandidateID, LoadID: c.LoadID, LoadRef: c.LoadRef, SeqNo: c.SeqNo,
		Species: c.Species, Sex: c.Sex, Breed: c.Breed, FarmLabel: load.FarmLabel, VendorName: load.VendorName,
		Decision: c.Decision, DecidedByName: c.DecidedByName, DecisionNote: c.DecisionNote, RecordedBy: c.RecordedBy,
		OccurredAt: now.Format(time.RFC3339Nano),
		Pending:    load.Counts.Pending, Accepted: load.Counts.Accepted, Rejected: load.Counts.Rejected,
	}
	var actor any
	if strings.TrimSpace(actorID) != "" {
		actor = actorID
	}
	envelope, err := json.Marshal(map[string]any{
		"event_id":       eventID,
		"event_type":     DecidedEventType,
		"schema_version": decidedSchemaVersion,
		"schema_ref":     decidedSchemaRef,
		"occurred_at":    now.Format("2006-01-02T15:04:05.000000Z"),
		"recorded_at":    now.Format("2006-01-02T15:04:05.000000Z"),
		"aggregate_type": aggregateType,
		"aggregate_id":   c.CandidateID,
		"producer": map[string]any{
			"module":  "animal_purchases",
			"service": "goatos-api",
			"version": nil,
		},
		"idempotency_key": idempotencyKey,
		"actor": map[string]any{
			"actor_type": "human",
			"actor_id":   actor,
			"actor_ref":  nil,
		},
		"subject_type":     aggregateType,
		"subject_id":       c.CandidateID,
		"visibility_scope": map[string]any{"tenant_id": tenantID},
		"evidence_refs": []map[string]string{{
			"evidence_type": "source_record",
			"evidence_id":   "animal_purchase_candidate:" + c.CandidateID,
		}},
		"payload":  payload,
		"trace_id": "animal-purchase-decision:" + c.CandidateID,
	})
	if err != nil {
		return err
	}
	headers, err := json.Marshal(map[string]any{
		"actor_id":      actorID,
		"farm":          load.FarmLabel,
		"business_date": biztime.BusinessDate(now),
	})
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, sqlInsertOutbox, tenantID, eventID, DecidedEventType, decidedSchemaVersion, aggregateType, c.CandidateID,
		decidedTopic, envelope, headers, idempotencyKey); err != nil {
		return fmt.Errorf("animal purchase: outbox decided: %w", err)
	}
	return nil
}

// ---- helpers ----

func isUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, r := range value {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
				return false
			}
		}
	}
	return true
}
