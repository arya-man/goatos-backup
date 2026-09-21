package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
	"github.com/vgoats/goatos/backend/internal/toxin/domain"
	"github.com/vgoats/goatos/backend/internal/toxin/ports"
)

// ProcedureSource reads the authored toxin procedure from the SOP library (THE TOXIN PROCEDURE IS
// AUTHORED, 2026-09-20): `form_dsl.toxin` of the `procurement.toxin_test` sop_versions row. The
// same document is served to the phone, read by the CEO's review and checked at the write, and a
// version that fails to parse fails CLOSED -- it cannot be published in the first place, because
// sop/app validates it through the same functions.
type ProcedureSource struct{ pool *pgxpool.Pool }

// NewProcedureSource wires the reader over the shared pool.
func NewProcedureSource(pool *pgxpool.Pool) *ProcedureSource { return &ProcedureSource{pool: pool} }

var _ ports.ProcedureSource = (*ProcedureSource)(nil)

const sqlPublishedProcedure = `
SELECT v.version, v.form_dsl
FROM public.sop_versions v
JOIN public.sop_definitions d ON d.tenant_id = v.tenant_id AND d.sop_id = v.sop_id
WHERE v.tenant_id = $1 AND d.code = $2 AND v.status = 'published'
ORDER BY v.version DESC
LIMIT 1`

const sqlProcedureVersion = `
SELECT v.version, v.form_dsl
FROM public.sop_versions v
JOIN public.sop_definitions d ON d.tenant_id = v.tenant_id AND d.sop_id = v.sop_id
WHERE v.tenant_id = $1 AND d.code = $2 AND v.version = $3 AND v.status IN ('published', 'retired')
LIMIT 1`

// PublishedProcedure is what a round opening now would run. A tenant with nothing authored runs
// the seeded document as version 1.
func (s *ProcedureSource) PublishedProcedure(ctx context.Context, tenantID string) (domain.Procedure, error) {
	dsl, version, found, err := s.read(ctx, sqlPublishedProcedure, tenantID, domain.SOPCodeToxinTest)
	if err != nil {
		return domain.Procedure{}, err
	}
	if !found {
		return domain.SeededProcedure(), nil
	}
	return domain.CompileProcedure(dsl, version), nil
}

// ProcedureVersion is the exact procedure a round was opened on -- published or since retired, so
// a round that started on v2 keeps running v2 after v3 is published.
func (s *ProcedureSource) ProcedureVersion(ctx context.Context, tenantID string, version int) (domain.Procedure, error) {
	dsl, got, found, err := s.read(ctx, sqlProcedureVersion, tenantID, domain.SOPCodeToxinTest, version)
	if err != nil {
		return domain.Procedure{}, err
	}
	if !found {
		// Version 1 is ALWAYS the seeded document: a tenant whose rounds opened before anything
		// was authored has no row to read, and refusing them would strand every round in flight
		// at the moment the feature shipped.
		if version <= 1 {
			return domain.SeededProcedure(), nil
		}
		return domain.Procedure{}, ports.ErrProcedureVersionUnknown
	}
	return domain.CompileProcedure(dsl, got), nil
}

func (s *ProcedureSource) read(ctx context.Context, sql string, args ...any) (domain.ToxinDSL, int, bool, error) {
	var version int
	var raw []byte
	bound := sqlbind.MustBind(sql, args...)
	err := s.pool.QueryRow(ctx, bound.SQL(), bound.Args()...).Scan(&version, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ToxinDSL{}, 0, false, nil
	}
	if err != nil {
		return domain.ToxinDSL{}, 0, false, fmt.Errorf("toxin: read procedure: %w", err)
	}
	var formDSL map[string]any
	if err := json.Unmarshal(raw, &formDSL); err != nil {
		return domain.ToxinDSL{}, 0, false, fmt.Errorf("toxin: procedure v%d form_dsl: %w", version, err)
	}
	dsl, err := domain.ParseToxin(formDSL)
	if err != nil {
		return domain.ToxinDSL{}, 0, false, fmt.Errorf("toxin: procedure v%d: %w", version, err)
	}
	if problems := domain.ValidateToxin(dsl); len(problems) > 0 {
		return domain.ToxinDSL{}, 0, false, fmt.Errorf("toxin: procedure v%d: %s", version, problems[0])
	}
	return dsl, version, true, nil
}
