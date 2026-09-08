// Package postgres owns notification_alert_audiences (migration 000283) and reads the
// designation catalog the matrix offers as columns.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/notificationaudience/ports"
)

// Repository is the pgx-backed ports.AudienceRepository.
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

var _ ports.AudienceRepository = (*Repository)(nil)

// Every statement is a package-level named const so the scale guard and a future query-plan test
// can reach it. All of them are primary-key or tenant-key lookups on a table bounded by the alert
// catalog (a few dozen rows per tenant), never by herd or roster data.
const listAudiencesSQL = `
SELECT alert_key, designation_codes, row_version
FROM notification_alert_audiences
WHERE tenant_id = $1::uuid
ORDER BY alert_key
LIMIT 500`

const loadAudienceSQL = `
SELECT alert_key, designation_codes, row_version
FROM notification_alert_audiences
WHERE tenant_id = $1::uuid AND alert_key = $2`

// unknownDesignationsSQL returns, from the submitted codes, those that are not ACTIVE catalog
// rows. Evaluated inside the write transaction so a code retired between screen load and save is
// refused rather than stored.
const unknownDesignationsSQL = `
SELECT coalesce(array_agg(code ORDER BY code), '{}')
FROM unnest($1::text[]) AS code
WHERE NOT EXISTS (
  SELECT 1 FROM designation_catalog d
  WHERE d.designation_code = code AND d.status = 'active'
)`

// insertAudienceSQL is the first customisation: the PK conflict is the version fence (a
// concurrent first save yields no row through ON CONFLICT DO NOTHING + RETURNING).
const insertAudienceSQL = `
INSERT INTO notification_alert_audiences (tenant_id, alert_key, designation_codes, updated_by)
VALUES ($1::uuid, $2, $3::text[], nullif($4, '')::uuid)
ON CONFLICT (tenant_id, alert_key) DO NOTHING
RETURNING alert_key, designation_codes, row_version`

const updateAudienceSQL = `
UPDATE notification_alert_audiences
SET designation_codes = $3::text[],
    updated_at = now(),
    updated_by = nullif($4, '')::uuid,
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND alert_key = $2 AND row_version = $5
RETURNING alert_key, designation_codes, row_version`

const deleteAudienceSQL = `
DELETE FROM notification_alert_audiences
WHERE tenant_id = $1::uuid AND alert_key = $2 AND row_version = $3`

const insertAudienceAuditSQL = `
INSERT INTO audit_log (tenant_id, actor_id, actor_type, action, resource_type, resource_id, after_state, metadata)
VALUES ($1::uuid, nullif($2, '')::uuid, 'user', $3, 'notification_alert', NULL, $4::jsonb, $5::jsonb)`

const listDesignationsSQL = `
SELECT designation_code, label, coalesce(grade, '')
FROM designation_catalog
WHERE status = 'active'
ORDER BY sort_order, designation_code
LIMIT 200`

// ListAudiences reads every override for the tenant in ONE round trip: the table is bounded
// by the alert catalog (a few dozen rows per tenant at most), never by data.
func (r *Repository) ListAudiences(ctx context.Context, tenantID string) (map[string]ports.Audience, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, listAudiencesSQL, tenantID)
	if err != nil {
		return nil, fmt.Errorf("notification audiences: list: %w", err)
	}
	defer rows.Close()
	out := map[string]ports.Audience{}
	for rows.Next() {
		var a ports.Audience
		if err := rows.Scan(&a.AlertKey, &a.DesignationCodes, &a.RowVersion); err != nil {
			return nil, fmt.Errorf("notification audiences: scan: %w", err)
		}
		if a.DesignationCodes == nil {
			a.DesignationCodes = []string{}
		}
		out[a.AlertKey] = a
	}
	return out, rows.Err()
}

// LoadAudience reads one alert's override by primary key.
func (r *Repository) LoadAudience(ctx context.Context, tenantID, alertKey string) (ports.Audience, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var a ports.Audience
	err := r.pool.QueryRow(ctx, loadAudienceSQL, tenantID, alertKey).
		Scan(&a.AlertKey, &a.DesignationCodes, &a.RowVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.Audience{}, false, nil
	}
	if err != nil {
		return ports.Audience{}, false, fmt.Errorf("notification audiences: load %s: %w", alertKey, err)
	}
	if a.DesignationCodes == nil {
		a.DesignationCodes = []string{}
	}
	return a, true, nil
}

// ReplaceAudience writes the whole audience for one alert in ONE transaction: the designation
// codes are validated against the ACTIVE catalog inside it (an unknown or retired code is
// refused, never dropped), the row is version-fenced, and the audit entry lands with it.
//
// Fence: a not-yet-customised alert saves with expected version 0 and must INSERT; a
// customised one must UPDATE the exact version the screen loaded. Either way a mismatch is
// ports.ErrVersionConflict, never a silent overwrite of another admin's decision.
func (r *Repository) ReplaceAudience(ctx context.Context, cmd ports.ReplaceAudienceCommand) (ports.Audience, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ports.Audience{}, fmt.Errorf("notification audiences: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	codes := cmd.DesignationCodes
	if codes == nil {
		codes = []string{}
	}
	var unknown []string
	if err := tx.QueryRow(ctx, unknownDesignationsSQL, codes).Scan(&unknown); err != nil {
		return ports.Audience{}, fmt.Errorf("notification audiences: validate codes: %w", err)
	}
	if len(unknown) > 0 {
		return ports.Audience{}, fmt.Errorf("%w: %v", ports.ErrUnknownDesignation, unknown)
	}

	var stored ports.Audience
	if cmd.ExpectedRowVersion == 0 {
		// First customisation: INSERT, and refuse if someone customised it meanwhile (the PK
		// conflict is the fence). ON CONFLICT DO NOTHING + RETURNING yields no row on conflict.
		err = tx.QueryRow(ctx, insertAudienceSQL, cmd.TenantID, cmd.AlertKey, codes, cmd.ActorID).
			Scan(&stored.AlertKey, &stored.DesignationCodes, &stored.RowVersion)
	} else {
		err = tx.QueryRow(ctx, updateAudienceSQL, cmd.TenantID, cmd.AlertKey, codes, cmd.ActorID, cmd.ExpectedRowVersion).
			Scan(&stored.AlertKey, &stored.DesignationCodes, &stored.RowVersion)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.Audience{}, ports.ErrVersionConflict
	}
	if err != nil {
		return ports.Audience{}, fmt.Errorf("notification audiences: replace %s: %w", cmd.AlertKey, err)
	}
	if stored.DesignationCodes == nil {
		stored.DesignationCodes = []string{}
	}

	after, err := json.Marshal(map[string]any{"alert_key": stored.AlertKey, "designation_codes": stored.DesignationCodes})
	if err != nil {
		return ports.Audience{}, err
	}
	metadata, err := json.Marshal(map[string]any{"designation_count": len(stored.DesignationCodes), "row_version": stored.RowVersion})
	if err != nil {
		return ports.Audience{}, err
	}
	if _, err := tx.Exec(ctx, insertAudienceAuditSQL, cmd.TenantID, cmd.ActorID, "notification_audience.replaced", after, metadata); err != nil {
		return ports.Audience{}, fmt.Errorf("notification audiences: audit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ports.Audience{}, fmt.Errorf("notification audiences: commit: %w", err)
	}
	return stored, nil
}

// ResetAudience deletes the override. Deleting a row that does not exist is a no-op: "back to
// default" is already true, so a double click is not an error.
func (r *Repository) ResetAudience(ctx context.Context, tenantID, actorID, alertKey string, expectedRowVersion int) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if expectedRowVersion == 0 {
		_, customised, err := r.LoadAudience(ctx, tenantID, alertKey)
		if err != nil {
			return err
		}
		if customised {
			return ports.ErrVersionConflict
		}
		return nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("notification audiences: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, deleteAudienceSQL, tenantID, alertKey, expectedRowVersion)
	if err != nil {
		return fmt.Errorf("notification audiences: reset %s: %w", alertKey, err)
	}
	if tag.RowsAffected() == 0 {
		return ports.ErrVersionConflict
	}
	after, err := json.Marshal(map[string]any{"alert_key": alertKey, "use_defaults": true})
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, insertAudienceAuditSQL, tenantID, actorID, "notification_audience.reset", after, "{}"); err != nil {
		return fmt.Errorf("notification audiences: audit: %w", err)
	}
	return tx.Commit(ctx)
}

// ListDesignations reads the active designation catalog in display order.
func (r *Repository) ListDesignations(ctx context.Context) ([]ports.Designation, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, listDesignationsSQL)
	if err != nil {
		return nil, fmt.Errorf("notification audiences: designations: %w", err)
	}
	defer rows.Close()
	var out []ports.Designation
	for rows.Next() {
		var d ports.Designation
		if err := rows.Scan(&d.Code, &d.Label, &d.Grade); err != nil {
			return nil, fmt.Errorf("notification audiences: scan designation: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
