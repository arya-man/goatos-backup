// Package postgres reads the herd-operations CAPTURE CARDS from the SOP library: the
// `capture_card` section of a counts.birth / counts.death sop_versions row. This is the ONLY
// place that names sop_versions on counts' behalf; counts receives the card through
// counts/app.CaptureCardSource as a typed value -- the feedsop / weighingsop shape.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	uuidutil "github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

// CaptureCardSource is the Postgres counts/app.CaptureCardSource. One indexed lookup per call
// (sop_definitions (tenant_id, code) unique + sop_versions_one_published_per_sop_idx).
type CaptureCardSource struct {
	pool         *pgxpool.Pool
	queryTimeout time.Duration
}

// NewCaptureCardSource constructs the source.
func NewCaptureCardSource(pool *pgxpool.Pool, queryTimeout time.Duration) *CaptureCardSource {
	return &CaptureCardSource{pool: pool, queryTimeout: queryTimeout}
}

var _ countsapp.CaptureCardSource = (*CaptureCardSource)(nil)

const sqlPublishedCaptureCard = `
SELECT v.sop_version_id::text, v.version_label, v.form_dsl
FROM public.sop_versions v
JOIN public.sop_definitions d ON d.tenant_id = v.tenant_id AND d.sop_id = v.sop_id
WHERE v.tenant_id = $1::uuid AND d.code = $2 AND v.status = 'published'
ORDER BY v.version DESC
LIMIT 1`

const sqlCaptureCardVersion = `
SELECT v.sop_version_id::text, v.version_label, v.form_dsl
FROM public.sop_versions v
JOIN public.sop_definitions d ON d.tenant_id = v.tenant_id AND d.sop_id = v.sop_id
WHERE v.tenant_id = $1::uuid AND d.code = $2 AND v.sop_version_id = $3::uuid
  AND v.status IN ('published', 'retired')
LIMIT 1`

// PublishedCaptureCard is the card a report raised NOW is judged by. No published version (a
// tenant created before the SOP was seeded, a fixture) is the empty card, pinned to nothing.
func (s *CaptureCardSource) PublishedCaptureCard(ctx context.Context, tenantID, sopCode string) (countsapp.CaptureCardVersion, error) {
	out, found, err := s.read(ctx, sqlPublishedCaptureCard, tenantID, sopCode)
	if err != nil {
		return countsapp.CaptureCardVersion{}, err
	}
	if !found {
		return countsapp.CaptureCardVersion{Card: countsdomain.CaptureCard{SchemaVersion: countsdomain.CaptureSchemaVersion}}, nil
	}
	return out, nil
}

// CaptureCardVersion is the exact card a NEW app echoed as its pin: published or retired.
func (s *CaptureCardSource) CaptureCardVersion(ctx context.Context, tenantID, sopCode, versionID string) (countsapp.CaptureCardVersion, error) {
	if !uuidutil.IsUUIDString(versionID) {
		// Names no version: refresh the card (409), never a 500 from the uuid cast.
		return countsapp.CaptureCardVersion{}, countsapp.ErrCaptureSOPVersionUnknown
	}
	out, found, err := s.read(ctx, sqlCaptureCardVersion, tenantID, sopCode, versionID)
	if err != nil {
		return countsapp.CaptureCardVersion{}, err
	}
	if !found {
		return countsapp.CaptureCardVersion{}, countsapp.ErrCaptureSOPVersionUnknown
	}
	return out, nil
}

func (s *CaptureCardSource) read(ctx context.Context, sql string, args ...any) (countsapp.CaptureCardVersion, bool, error) {
	if s.queryTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.queryTimeout)
		defer cancel()
	}
	var (
		out countsapp.CaptureCardVersion
		raw []byte
	)
	err := s.pool.QueryRow(ctx, sql, args...).Scan(&out.VersionID, &out.VersionLabel, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return countsapp.CaptureCardVersion{}, false, nil
	}
	if err != nil {
		return countsapp.CaptureCardVersion{}, false, fmt.Errorf("counts sop: read capture card: %w", err)
	}
	var formDSL map[string]any
	if err := json.Unmarshal(raw, &formDSL); err != nil {
		return countsapp.CaptureCardVersion{}, false, fmt.Errorf("counts sop: %s form_dsl: %w", out.VersionLabel, err)
	}
	card, err := countsdomain.ParseCaptureCard(formDSL)
	if err != nil {
		return countsapp.CaptureCardVersion{}, false, fmt.Errorf("counts sop: %s: %w", out.VersionLabel, err)
	}
	if problems := countsdomain.ValidateCaptureCard(card); len(problems) > 0 {
		return countsapp.CaptureCardVersion{}, false, fmt.Errorf("counts sop: %s invalid: %s", out.VersionLabel, problems[0])
	}
	out.Card = card
	return out, true, nil
}
