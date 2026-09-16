// Package postgres is the Alerts module's storage: its own alert_rule_config rows, and
// READ-ONLY roll-ups of the frozen rows other modules already own (the feed-direction
// sheet, the shifting register, park names). It writes nothing outside alert_rule_config
// and the shared idempotency ledger.
package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/alerts/domain"
	"github.com/vgoats/goatos/backend/internal/alerts/ports"
	platformoutbox "github.com/vgoats/goatos/backend/internal/platform/outbox"
)

// Repository serves every port the alerts service needs from Postgres.
type Repository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

// NewRepository binds the shared pool.
func NewRepository(pool *pgxpool.Pool, timeout time.Duration) *Repository {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &Repository{pool: pool, timeout: timeout}
}

const idempotencyScope = "alerts.rule_config"

// ListRuleConfig reads the stored rows; the catalog defaults are applied by the domain.
func (r *Repository) ListRuleConfig(ctx context.Context, tenantID string) ([]domain.StoredRuleConfig, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	// updated_by is resolved to a NAME here: an id is never rendered to a reader, and a person
	// whose name cannot be resolved is shown as nobody rather than as a uuid. updated_at is a
	// farm-readable Asia/Kolkata label -- business meaning is the Indian calendar, never UTC.
	rows, err := r.pool.Query(ctx, `
SELECT c.rule_key, c.enabled, c.threshold,
       COALESCE(setter.display_name, ''),
       to_char(c.updated_at AT TIME ZONE 'Asia/Kolkata', 'DD Mon YYYY, HH24:MI') || ' IST'
FROM alert_rule_config c
LEFT JOIN LATERAL (
  SELECT wm.display_name
  FROM workforce_members wm
  WHERE wm.tenant_id = c.tenant_id
    AND (wm.workforce_member_id = c.updated_by OR wm.user_id = c.updated_by)
  ORDER BY (wm.workforce_member_id = c.updated_by) DESC, (wm.status = 'active') DESC, wm.updated_at DESC, wm.workforce_member_id
  LIMIT 1
) setter ON true
WHERE c.tenant_id = $1::uuid
ORDER BY c.rule_key`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.StoredRuleConfig{}
	for rows.Next() {
		var row domain.StoredRuleConfig
		var key string
		if err := rows.Scan(&key, &row.Enabled, &row.Threshold, &row.UpdatedBy, &row.UpdatedAt); err != nil {
			return nil, err
		}
		row.Key = domain.RuleKey(key)
		out = append(out, row)
	}
	return out, rows.Err()
}

// UpsertRuleConfig writes one rule's setting under an idempotency reservation: the same
// (rule, enabled, threshold) under the same key is one write, a different payload under a
// known key is refused.
func (r *Repository) UpsertRuleConfig(ctx context.Context, in domain.SetRuleConfig) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	fingerprint := requestFingerprint(string(in.Key), strconv.FormatBool(in.Enabled), strconv.Itoa(in.Threshold))
	reservation, err := reserveIdempotency(ctx, tx, in.TenantID, idempotencyScope, in.IdempotencyKey, fingerprint)
	if err != nil {
		return err
	}
	if !reservation.proceed {
		// Exact replay: the original write stands; nothing runs again.
		return nil
	}
	var actor any
	if strings.TrimSpace(in.ActorID) != "" {
		actor = in.ActorID
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO alert_rule_config (tenant_id, rule_key, enabled, threshold, updated_by, updated_at)
VALUES ($1::uuid, $2, $3, $4, $5::uuid, now())
ON CONFLICT (tenant_id, rule_key)
DO UPDATE SET enabled = EXCLUDED.enabled, threshold = EXCLUDED.threshold, updated_by = EXCLUDED.updated_by, updated_at = now()`,
		in.TenantID, string(in.Key), in.Enabled, in.Threshold, actor); err != nil {
		return err
	}
	resultID := platformoutbox.DeterministicUUID("alert_rule_config:" + in.TenantID + ":" + string(in.Key))
	if err := completeIdempotency(ctx, tx, in.TenantID, idempotencyScope, in.IdempotencyKey, "alert_rule_config", resultID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// PenFeedDay rolls one park's frozen sheet for a feed day up to the PEN grain.
//
// projection-review: membership=the live feed_direction_issues row for (tenant, park,
// feed_day) per workflow, and every feed_direction_issue_rows cell under it (the whole
// generated scope, never a page); group_key=(shed_id, partition_key) -- the pen -- with
// head count taken as the MAX over sessions of the SUM over the pen's ration grains
// (grains partition a pen's animals; sessions feed the same animals), so neither a
// two-grain pen nor a two-session day doubles the count; join_cardinality=issue -> rows is
// 1:N by design and is fully aggregated before the pen row is emitted, nothing else is
// joined; pagination=none, one park-day is the scope.
func (r *Repository) PenFeedDay(ctx context.Context, tenantID, parkID, feedDay string) ([]domain.PenFeedDay, string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, `
WITH issue AS (
  SELECT feed_direction_issue_id, park_id, issued_at
  FROM feed_direction_issues
  WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND feed_day = $3::date
    AND state IN ('issued', 'amended', 'locked')
),
grains AS (
  SELECT r.shed_id, r.partition_key, min(r.partition_label) AS partition_label,
         min(r.shed_label) AS shed_label, min(r.park_label) AS park_label,
         r.session_no, r.shed_tag_key, r.breed_key, r.workflow,
         max(r.head_count) AS head_count,
         sum(COALESCE(r.quantity_kg, 0)) AS kg,
         count(*) FILTER (WHERE r.quantity_kg IS NULL) AS blocked
  FROM feed_direction_issue_rows r
  JOIN issue i ON i.feed_direction_issue_id = r.feed_direction_issue_id
  WHERE r.tenant_id = $1::uuid
  GROUP BY r.shed_id, r.partition_key, r.session_no, r.shed_tag_key, r.breed_key, r.workflow
),
sessions AS (
  SELECT shed_id, partition_key, min(partition_label) AS partition_label, min(shed_label) AS shed_label, min(park_label) AS park_label,
         session_no, sum(head_count) AS head_count, sum(kg) AS kg, sum(blocked) AS blocked
  FROM grains
  GROUP BY shed_id, partition_key, session_no
)
SELECT shed_id::text, COALESCE(min(partition_label), ''), min(shed_label), min(park_label),
       max(head_count)::bigint, sum(kg)::float8, sum(blocked)::bigint,
       (SELECT to_char(min(issued_at) AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') FROM issue)
FROM sessions
GROUP BY shed_id, partition_key
ORDER BY min(shed_label), partition_key`, tenantID, parkID, feedDay)
	if err != nil {
		return nil, "", fmt.Errorf("alerts: pen feed day: %w", err)
	}
	defer rows.Close()
	var out []domain.PenFeedDay
	issuedAt := ""
	for rows.Next() {
		var p domain.PenFeedDay
		var issued *string
		if err := rows.Scan(&p.ShedID, &p.PartitionLabel, &p.ShedName, &p.ParkLabel, &p.HeadCount, &p.QuantityKg, &p.BlockedCells, &issued); err != nil {
			return nil, "", fmt.Errorf("alerts: pen feed day scan: %w", err)
		}
		p.ParkID = parkID
		if issued != nil {
			issuedAt = *issued
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	if out == nil {
		// No sheet issued for that day: nil, so the detector knows there is no baseline.
		return nil, "", nil
	}
	return out, issuedAt, nil
}

// PenMovements lists the shifting events touching a park that were raised, approved or
// applied between two instants (the two sheets' issue times), with the animals each moves.
//
// projection-review: membership=shifting_events for the tenant whose source or destination
// park is the given park, not rejected/canceled, with any lifecycle stamp inside the window;
// group_key=shifting_event_id; join_cardinality=shifting_event_impacts is 1:N per event and
// is SUMMED in a pre-aggregated subquery before the join, so an event with two impact grains
// is one row with one head count; pagination=none, bounded by the park and a one-day window.
func (r *Repository) PenMovements(ctx context.Context, tenantID, parkID, fromInstant, toInstant string) ([]domain.PenMovement, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if strings.TrimSpace(fromInstant) == "" || strings.TrimSpace(toInstant) == "" {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `
SELECT COALESCE(se.source_shed_id::text, ''), COALESCE(se.source_partition_label, ''),
       se.destination_shed_id::text, COALESCE(se.destination_partition_label, ''),
       COALESCE(hc.head_count, 0)::bigint, se.event_status
FROM shifting_events se
LEFT JOIN (
  SELECT shifting_event_id, sum(head_count) AS head_count
  FROM shifting_event_impacts
  WHERE tenant_id = $1::uuid
  GROUP BY shifting_event_id
) hc ON hc.shifting_event_id = se.shifting_event_id
WHERE se.tenant_id = $1::uuid
  AND (se.destination_park_id = $2::uuid OR se.source_park_id = $2::uuid)
  AND se.event_status NOT IN ('rejected', 'canceled')
  AND (
       (se.raised_at     >= $3::timestamptz AND se.raised_at     < $4::timestamptz)
    OR (se.authorized_at >= $3::timestamptz AND se.authorized_at < $4::timestamptz)
    OR (se.applied_at    >= $3::timestamptz AND se.applied_at    < $4::timestamptz)
  )
ORDER BY se.raised_at, se.shifting_event_id`, tenantID, parkID, fromInstant, toInstant)
	if err != nil {
		return nil, fmt.Errorf("alerts: pen movements: %w", err)
	}
	defer rows.Close()
	out := []domain.PenMovement{}
	for rows.Next() {
		var m domain.PenMovement
		if err := rows.Scan(&m.SourceShedID, &m.SourcePartitionLabel, &m.DestinationShedID, &m.DestinationPartitionLabel, &m.HeadCount, &m.Status); err != nil {
			return nil, fmt.Errorf("alerts: pen movements scan: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ParkNames resolves park ids to names.
func (r *Repository) ParkNames(ctx context.Context, tenantID string, parkIDs []string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	out := map[string]string{}
	if len(parkIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `
SELECT location_id::text, name
FROM locations
WHERE tenant_id = $1::uuid AND location_id = ANY($2::uuid[])`, tenantID, parkIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

// ---- idempotency (the shared ledger, same shape as every other module) -------------

type idemReservation struct {
	proceed  bool
	resultID string
}

func requestFingerprint(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:])
}

func scopedIdempotencyKey(tenantID, scope, key string) string {
	return tenantID + ":" + scope + ":" + strings.TrimSpace(key)
}

func reserveIdempotency(ctx context.Context, tx pgx.Tx, tenantID, scope, key, fingerprint string) (idemReservation, error) {
	if strings.TrimSpace(key) == "" {
		return idemReservation{proceed: true}, nil
	}
	scoped := scopedIdempotencyKey(tenantID, scope, key)
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
	var existingHash, resultID string
	if err := tx.QueryRow(ctx, `
SELECT request_hash, COALESCE(result_id::text, '')
FROM idempotency_keys
WHERE idempotency_key = $1`, scoped).Scan(&existingHash, &resultID); err != nil {
		return idemReservation{}, err
	}
	if existingHash != fingerprint {
		return idemReservation{}, ports.ErrIdempotencyConflict
	}
	return idemReservation{resultID: resultID}, nil
}

func completeIdempotency(ctx context.Context, tx pgx.Tx, tenantID, scope, key, resultType, resultID string) error {
	if strings.TrimSpace(key) == "" {
		return nil
	}
	_, err := tx.Exec(ctx, `
UPDATE idempotency_keys
SET status = 'completed', result_type = $2, result_id = $3::uuid, completed_at = now()
WHERE idempotency_key = $1`, scopedIdempotencyKey(tenantID, scope, key), resultType, resultID)
	return err
}
