// Package postgres stores the market survey (migration 000304).
package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/market/domain"
	"github.com/vgoats/goatos/backend/internal/market/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
)

// Repository is the pgx adapter.
type Repository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

// NewRepository wires the adapter over the shared pool.
func NewRepository(pool *pgxpool.Pool, timeout time.Duration) *Repository {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Repository{pool: pool, timeout: timeout}
}

var _ ports.Repository = (*Repository)(nil)

const (
	cityColumns     = `id::text, name, sort_order, status, created_at, updated_at`
	questionColumns = `id::text, label, unit_label, sort_order, status, created_at, updated_at`
	entryColumns    = `e.id::text, e.city_id::text, e.question_id::text, e.business_date::text, e.price::float8,
		e.city_name, e.question_label, e.unit_label, COALESCE(e.recorded_by::text, ''), e.recorded_at`
	// entryFrom joins the config ONLY to order rows the way the screen lists cities and
	// questions; every rendered word still comes from the entry's own snapshot columns.
	entryFrom = `FROM market_price_entries e
JOIN market_cities c ON c.id = e.city_id
JOIN market_questions q ON q.id = e.question_id`
	entryOrder = `c.sort_order, lower(c.name), q.sort_order, lower(q.label)`
)

// GetConfig reads every city and question in shown order: active first, then by sort order and
// name. Bounded by the config caps, never herd-sized.
func (r *Repository) GetConfig(ctx context.Context, tenantID string) (domain.Config, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var cfg domain.Config
	rows, err := r.pool.Query(ctx, `
SELECT `+cityColumns+`
FROM market_cities
WHERE tenant_id = $1::uuid
ORDER BY (status = 'active') DESC, sort_order, lower(name), created_at
LIMIT $2`, tenantID, domain.MaxCities*2)
	if err != nil {
		return cfg, fmt.Errorf("market: list cities: %w", err)
	}
	cfg.Cities, err = scanCities(rows)
	if err != nil {
		return cfg, err
	}
	rows, err = r.pool.Query(ctx, `
SELECT `+questionColumns+`
FROM market_questions
WHERE tenant_id = $1::uuid
ORDER BY (status = 'active') DESC, sort_order, lower(label), created_at
LIMIT $2`, tenantID, domain.MaxQuestion*2)
	if err != nil {
		return cfg, fmt.Errorf("market: list questions: %w", err)
	}
	cfg.Questions, err = scanQuestions(rows)
	return cfg, err
}

// CreateCity appends a city after the current last one. Idempotent on the key: an exact replay
// returns the city the first call created.
func (r *Repository) CreateCity(ctx context.Context, tenantID, actorID, idempotencyKey string, write domain.CityWrite) (domain.City, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var out domain.City
	err := r.inTx(ctx, func(tx pgx.Tx) error {
		res, err := reserveIdempotency(ctx, tx, tenantID, "market_city_create", idempotencyKey,
			requestFingerprint(write.Name, write.Status))
		if err != nil {
			return err
		}
		if !res.proceed {
			return tx.QueryRow(ctx, `SELECT `+cityColumns+` FROM market_cities WHERE tenant_id = $1::uuid AND id = $2::uuid`,
				tenantID, res.resultID).Scan(&out.ID, &out.Name, &out.SortOrder, &out.Status, &out.CreatedAt, &out.UpdatedAt)
		}
		var active int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM market_cities WHERE tenant_id = $1::uuid AND status = 'active'`, tenantID).Scan(&active); err != nil {
			return err
		}
		if active >= domain.MaxCities {
			return ports.ErrTooMany
		}
		err = tx.QueryRow(ctx, `
INSERT INTO market_cities (tenant_id, name, sort_order, status, created_by)
VALUES ($1::uuid, $2, COALESCE((SELECT max(sort_order) FROM market_cities WHERE tenant_id = $1::uuid), 0) + 10, $3, nullif($4, '')::uuid)
RETURNING `+cityColumns, tenantID, write.Name, write.Status, actorID).
			Scan(&out.ID, &out.Name, &out.SortOrder, &out.Status, &out.CreatedAt, &out.UpdatedAt)
		if err != nil {
			return mapUnique(err)
		}
		if err := r.audit(ctx, tx, tenantID, actorID, "market.city.create", "market_city", out.ID, map[string]any{
			"name": out.Name, "idempotency_key": idempotencyKey,
		}); err != nil {
			return err
		}
		return completeIdempotency(ctx, tx, tenantID, "market_city_create", idempotencyKey, "market_city", out.ID)
	})
	return out, err
}

// UpdateCity renames, retires or reactivates a city. Naturally idempotent: the same write twice
// leaves the same row, so no key is reserved.
func (r *Repository) UpdateCity(ctx context.Context, tenantID, actorID, cityID string, write domain.CityWrite) (domain.City, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var out domain.City
	err := r.inTx(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
UPDATE market_cities
SET name = $3, status = $4, updated_at = now()
WHERE tenant_id = $1::uuid AND id = $2::uuid
RETURNING `+cityColumns, tenantID, cityID, write.Name, write.Status).
			Scan(&out.ID, &out.Name, &out.SortOrder, &out.Status, &out.CreatedAt, &out.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ports.ErrNotFound
		}
		if err != nil {
			return mapUnique(err)
		}
		return r.audit(ctx, tx, tenantID, actorID, "market.city.update", "market_city", out.ID, map[string]any{
			"name": out.Name, "status": out.Status,
		})
	})
	return out, err
}

// CreateQuestion appends a question after the current last one. Idempotent on the key.
func (r *Repository) CreateQuestion(ctx context.Context, tenantID, actorID, idempotencyKey string, write domain.QuestionWrite) (domain.Question, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var out domain.Question
	err := r.inTx(ctx, func(tx pgx.Tx) error {
		res, err := reserveIdempotency(ctx, tx, tenantID, "market_question_create", idempotencyKey,
			requestFingerprint(write.Label, write.UnitLabel, write.Status))
		if err != nil {
			return err
		}
		if !res.proceed {
			return tx.QueryRow(ctx, `SELECT `+questionColumns+` FROM market_questions WHERE tenant_id = $1::uuid AND id = $2::uuid`,
				tenantID, res.resultID).Scan(&out.ID, &out.Label, &out.UnitLabel, &out.SortOrder, &out.Status, &out.CreatedAt, &out.UpdatedAt)
		}
		var active int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM market_questions WHERE tenant_id = $1::uuid AND status = 'active'`, tenantID).Scan(&active); err != nil {
			return err
		}
		if active >= domain.MaxQuestion {
			return ports.ErrTooMany
		}
		err = tx.QueryRow(ctx, `
INSERT INTO market_questions (tenant_id, label, unit_label, sort_order, status, created_by)
VALUES ($1::uuid, $2, $3, COALESCE((SELECT max(sort_order) FROM market_questions WHERE tenant_id = $1::uuid), 0) + 10, $4, nullif($5, '')::uuid)
RETURNING `+questionColumns, tenantID, write.Label, write.UnitLabel, write.Status, actorID).
			Scan(&out.ID, &out.Label, &out.UnitLabel, &out.SortOrder, &out.Status, &out.CreatedAt, &out.UpdatedAt)
		if err != nil {
			return mapUnique(err)
		}
		if err := r.audit(ctx, tx, tenantID, actorID, "market.question.create", "market_question", out.ID, map[string]any{
			"label": out.Label, "unit_label": out.UnitLabel, "idempotency_key": idempotencyKey,
		}); err != nil {
			return err
		}
		return completeIdempotency(ctx, tx, tenantID, "market_question_create", idempotencyKey, "market_question", out.ID)
	})
	return out, err
}

// UpdateQuestion relabels, re-units, retires or reactivates a question. Entries already recorded
// keep the words they were recorded against (their own snapshot columns), so this never rewrites
// history. Naturally idempotent.
func (r *Repository) UpdateQuestion(ctx context.Context, tenantID, actorID, questionID string, write domain.QuestionWrite) (domain.Question, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var out domain.Question
	err := r.inTx(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
UPDATE market_questions
SET label = $3, unit_label = $4, status = $5, updated_at = now()
WHERE tenant_id = $1::uuid AND id = $2::uuid
RETURNING `+questionColumns, tenantID, questionID, write.Label, write.UnitLabel, write.Status).
			Scan(&out.ID, &out.Label, &out.UnitLabel, &out.SortOrder, &out.Status, &out.CreatedAt, &out.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ports.ErrNotFound
		}
		if err != nil {
			return mapUnique(err)
		}
		return r.audit(ctx, tx, tenantID, actorID, "market.question.update", "market_question", out.ID, map[string]any{
			"label": out.Label, "unit_label": out.UnitLabel, "status": out.Status,
		})
	})
	return out, err
}

// ListDayEntries reads one business day's entries. Indexed on (tenant, business_date).
func (r *Repository) ListDayEntries(ctx context.Context, tenantID, businessDate string) ([]domain.Entry, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, `
SELECT `+entryColumns+`
`+entryFrom+`
WHERE e.tenant_id = $1::uuid AND e.business_date = $2::date
ORDER BY `+entryOrder+`
LIMIT $3`, tenantID, businessDate, domain.MaxCities*domain.MaxQuestion)
	if err != nil {
		return nil, fmt.Errorf("market: list day entries: %w", err)
	}
	return scanEntries(rows)
}

// ListEntriesBetween reads the analytics window oldest first. Bounded by the window x the config
// caps; the service bounds the window itself.
func (r *Repository) ListEntriesBetween(ctx context.Context, tenantID, from, to string) ([]domain.Entry, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, `
SELECT `+entryColumns+`
`+entryFrom+`
WHERE e.tenant_id = $1::uuid AND e.business_date >= $2::date AND e.business_date <= $3::date
ORDER BY e.business_date, `+entryOrder+`
LIMIT $4`, tenantID, from, to, 400*domain.MaxCities*domain.MaxQuestion)
	if err != nil {
		return nil, fmt.Errorf("market: list entries between: %w", err)
	}
	return scanEntries(rows)
}

// RecordDayEntry upserts a city's answers for one day inside ONE transaction: the city must be
// active, every question must be active, and each row snapshots the live city name and the
// question's label and unit as they are at this moment. Idempotent on the key; an exact replay
// re-reads the day's rows for the city and returns them without writing.
func (r *Repository) RecordDayEntry(ctx context.Context, p ports.RecordDayEntryParams) ([]domain.Entry, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var out []domain.Entry
	err := r.inTx(ctx, func(tx pgx.Tx) error {
		parts := []string{p.Write.CityID, p.Write.BusinessDate}
		for _, a := range p.Write.Answers {
			parts = append(parts, a.QuestionID, fmt.Sprintf("%.2f", a.Price))
		}
		res, err := reserveIdempotency(ctx, tx, p.TenantID, "market_day_entry", p.IdempotencyKey, requestFingerprint(parts...))
		if err != nil {
			return err
		}
		if !res.proceed {
			out, err = r.cityDayEntries(ctx, tx, p.TenantID, p.Write.CityID, p.Write.BusinessDate)
			return err
		}
		var cityName, cityStatus string
		err = tx.QueryRow(ctx, `SELECT name, status FROM market_cities WHERE tenant_id = $1::uuid AND id = $2::uuid FOR UPDATE`,
			p.TenantID, p.Write.CityID).Scan(&cityName, &cityStatus)
		if errors.Is(err, pgx.ErrNoRows) {
			return ports.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("market: lock city: %w", err)
		}
		if cityStatus != domain.StatusActive {
			return ports.ErrCityRetired
		}
		// One set-based write: every answer's question is resolved and snapshotted in the same
		// statement, and a question that is missing or retired makes the row count fall short,
		// which is how the unknown-question refusal is detected without a per-answer query.
		ids := make([]string, 0, len(p.Write.Answers))
		prices := make([]float64, 0, len(p.Write.Answers))
		for _, a := range p.Write.Answers {
			ids = append(ids, a.QuestionID)
			prices = append(prices, a.Price)
		}
		tag, err := tx.Exec(ctx, `
INSERT INTO market_price_entries
    (tenant_id, city_id, question_id, business_date, price, city_name, question_label, unit_label, recorded_by, recorded_at, updated_at)
SELECT $1::uuid, $2::uuid, q.id, $3::date, a.price, $4, q.label, q.unit_label, nullif($7, '')::uuid, now(), now()
FROM unnest($5::uuid[], $6::float8[]) AS a (question_id, price)
JOIN market_questions q ON q.tenant_id = $1::uuid AND q.id = a.question_id AND q.status = 'active'
ON CONFLICT (tenant_id, city_id, question_id, business_date) DO UPDATE
SET price = EXCLUDED.price,
    city_name = EXCLUDED.city_name,
    question_label = EXCLUDED.question_label,
    unit_label = EXCLUDED.unit_label,
    recorded_by = EXCLUDED.recorded_by,
    updated_at = now()`,
			p.TenantID, p.Write.CityID, p.Write.BusinessDate, cityName, ids, prices, p.ActorID)
		if err != nil {
			return fmt.Errorf("market: upsert entries: %w", err)
		}
		if int(tag.RowsAffected()) != len(ids) {
			return domain.ErrUnknownQuestion
		}
		if err := r.audit(ctx, tx, p.TenantID, p.ActorID, "market.survey.record", "market_city", p.Write.CityID, map[string]any{
			"business_date": p.Write.BusinessDate, "city_name": cityName, "answers": len(ids),
			"idempotency_key": p.IdempotencyKey,
		}); err != nil {
			return err
		}
		if err := completeIdempotency(ctx, tx, p.TenantID, "market_day_entry", p.IdempotencyKey, "market_city", p.Write.CityID); err != nil {
			return err
		}
		out, err = r.cityDayEntries(ctx, tx, p.TenantID, p.Write.CityID, p.Write.BusinessDate)
		return err
	})
	return out, err
}

func (r *Repository) cityDayEntries(ctx context.Context, tx pgx.Tx, tenantID, cityID, businessDate string) ([]domain.Entry, error) {
	rows, err := tx.Query(ctx, `
SELECT `+entryColumns+`
`+entryFrom+`
WHERE e.tenant_id = $1::uuid AND e.city_id = $2::uuid AND e.business_date = $3::date
ORDER BY `+entryOrder+`
LIMIT $4`, tenantID, cityID, businessDate, domain.MaxQuestion*2)
	if err != nil {
		return nil, fmt.Errorf("market: read city day: %w", err)
	}
	return scanEntries(rows)
}

// ReporterUserIDs lists active market_reporter grant holders. Per person: the grant row, never a
// job. Bounded -- a tenant names a handful of reporters.
func (r *Repository) ReporterUserIDs(ctx context.Context, tenantID string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, `
SELECT DISTINCT user_id::text
FROM user_scope_grants
WHERE tenant_id = $1::uuid
  AND role = $2
  AND status = 'active'
  AND (valid_to IS NULL OR valid_to > now())
LIMIT 100`, tenantID, permissions.RoleMarketReporter)
	if err != nil {
		return nil, fmt.Errorf("market: reporters: %w", err)
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

func (r *Repository) audit(ctx context.Context, tx pgx.Tx, tenantID, actorID, action, resourceType, resourceID string, meta map[string]any) error {
	meta["domain"] = "sales"
	meta["module"] = "market_survey"
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID: tenantID, ActorID: actorID, ActorType: "human",
		Action: action, ResourceType: resourceType, ResourceID: resourceID, Metadata: meta,
	}); err != nil {
		return fmt.Errorf("market: audit %s: %w", action, err)
	}
	return nil
}

func (r *Repository) inTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("market: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func mapUnique(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ports.ErrDuplicateName
	}
	return err
}

func scanCities(rows pgx.Rows) ([]domain.City, error) {
	defer rows.Close()
	out := []domain.City{}
	for rows.Next() {
		var c domain.City
		if err := rows.Scan(&c.ID, &c.Name, &c.SortOrder, &c.Status, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func scanQuestions(rows pgx.Rows) ([]domain.Question, error) {
	defer rows.Close()
	out := []domain.Question{}
	for rows.Next() {
		var q domain.Question
		if err := rows.Scan(&q.ID, &q.Label, &q.UnitLabel, &q.SortOrder, &q.Status, &q.CreatedAt, &q.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func scanEntries(rows pgx.Rows) ([]domain.Entry, error) {
	defer rows.Close()
	out := []domain.Entry{}
	for rows.Next() {
		var e domain.Entry
		if err := rows.Scan(&e.ID, &e.CityID, &e.QuestionID, &e.BusinessDate, &e.Price,
			&e.CityName, &e.QuestionLabel, &e.UnitLabel, &e.RecordedBy, &e.RecordedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// --- idempotency (the sales/procurement guard, same contract) ---------------------------------

type idemReservation struct {
	proceed    bool
	resultType string
	resultID   string
}

func idemScopedKey(tenantID, scope, key string) string {
	return tenantID + ":" + scope + ":" + strings.TrimSpace(key)
}

func requestFingerprint(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:])
}

func reserveIdempotency(ctx context.Context, tx pgx.Tx, tenantID, scope, key, fingerprint string) (idemReservation, error) {
	if strings.TrimSpace(key) == "" {
		return idemReservation{}, ports.ErrIdempotencyKeyRequired
	}
	scoped := idemScopedKey(tenantID, scope, key)
	var claimed string
	err := tx.QueryRow(ctx, `
INSERT INTO idempotency_keys (idempotency_key, tenant_id, scope, request_hash, status)
VALUES ($1, $2::uuid, $3, $4, 'started')
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING idempotency_key`, scoped, tenantID, scope, fingerprint).Scan(&claimed)
	if err == nil {
		return idemReservation{proceed: true}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return idemReservation{}, err
	}
	var existingHash, status, resultType, resultID string
	if err := tx.QueryRow(ctx, `
SELECT request_hash, status, COALESCE(result_type, ''), COALESCE(result_id::text, '')
FROM idempotency_keys
WHERE idempotency_key = $1`, scoped).Scan(&existingHash, &status, &resultType, &resultID); err != nil {
		return idemReservation{}, err
	}
	if existingHash != fingerprint {
		return idemReservation{}, ports.ErrIdempotencyConflict
	}
	return idemReservation{proceed: false, resultType: resultType, resultID: resultID}, nil
}

func completeIdempotency(ctx context.Context, tx pgx.Tx, tenantID, scope, key, resultType, resultID string) error {
	scoped := idemScopedKey(tenantID, scope, key)
	_, err := tx.Exec(ctx, `
UPDATE idempotency_keys
SET status = 'completed', result_type = $2, result_id = nullif($3::text, '')::uuid, completed_at = now()
WHERE idempotency_key = $1`, scoped, resultType, resultID)
	return err
}
