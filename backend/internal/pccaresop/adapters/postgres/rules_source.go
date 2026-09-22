// Package postgres reads the PC Care rules from the SOP library (PC CARE SOP, maintainer
// decision 2026-09-22): the `pc_care` section of a pc_care.tasks sop_versions row, compiled the
// same way for the planner, the task read, the submit and the verifier item.
//
// This is the ONLY place that names sop_versions on pccare's behalf; the module receives the
// rules through pccare/ports.SOPRulesSource as an opaque value -- the weighingsop shape.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
)

// RulesSource is the Postgres SOPRulesSource. One indexed lookup per call.
type RulesSource struct {
	pool         *pgxpool.Pool
	queryTimeout time.Duration
}

func NewRulesSource(pool *pgxpool.Pool, queryTimeout time.Duration) *RulesSource {
	return &RulesSource{pool: pool, queryTimeout: queryTimeout}
}

var _ ports.SOPRulesSource = (*RulesSource)(nil)

const sqlPublishedPCCareSOP = `
SELECT v.version, v.form_dsl
FROM public.sop_versions v
JOIN public.sop_definitions d ON d.tenant_id = v.tenant_id AND d.sop_id = v.sop_id
WHERE v.tenant_id = $1::uuid AND d.code = $2 AND v.status = 'published'
ORDER BY v.version DESC
LIMIT 1`

const sqlPCCareSOPVersion = `
SELECT v.version, v.form_dsl
FROM public.sop_versions v
JOIN public.sop_definitions d ON d.tenant_id = v.tenant_id AND d.sop_id = v.sop_id
WHERE v.tenant_id = $1::uuid AND d.code = $2 AND v.version = $3 AND v.status IN ('published', 'retired')
LIMIT 1`

// PublishedRules is what a task planned now runs under. No authored version (a tenant created
// before the migration ran, a test fixture) means the seeded rules, version 0.
func (s *RulesSource) PublishedRules(ctx context.Context, tenantID string) (domain.Rules, error) {
	rules, found, err := s.read(ctx, sqlPublishedPCCareSOP, tenantID, domain.SOPCodePCCare)
	if err != nil {
		return domain.Rules{}, err
	}
	if !found {
		return domain.SeededRules(), nil
	}
	return rules, nil
}

// RulesVersion is the exact rule set a task was pinned to.
func (s *RulesSource) RulesVersion(ctx context.Context, tenantID string, version int) (domain.Rules, error) {
	if version == 0 {
		return domain.SeededRules(), nil
	}
	rules, found, err := s.read(ctx, sqlPCCareSOPVersion, tenantID, domain.SOPCodePCCare, version)
	if err != nil {
		return domain.Rules{}, err
	}
	if !found {
		return domain.Rules{}, ports.ErrSOPVersionUnknown
	}
	return rules, nil
}

func (s *RulesSource) read(ctx context.Context, sql string, args ...any) (domain.Rules, bool, error) {
	if s.queryTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.queryTimeout)
		defer cancel()
	}
	var version int
	var raw []byte
	err := s.pool.QueryRow(ctx, sql, args...).Scan(&version, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Rules{}, false, nil
	}
	if err != nil {
		return domain.Rules{}, false, fmt.Errorf("pc care sop: read version: %w", err)
	}
	return parseRules(version, raw)
}

func parseRules(version int, raw []byte) (domain.Rules, bool, error) {
	var formDSL map[string]any
	if err := json.Unmarshal(raw, &formDSL); err != nil {
		return domain.Rules{}, false, fmt.Errorf("pc care sop: v%d form_dsl: %w", version, err)
	}
	if _, present := formDSL["pc_care"]; !present {
		// A version published without the section (none exists today, but a stored document
		// must never fail to load): the seeded rules under that version number.
		seeded := domain.SeededRules()
		seeded.Version = version
		return seeded, true, nil
	}
	dsl, err := domain.ParsePCCareSOP(formDSL)
	if err != nil {
		return domain.Rules{}, false, fmt.Errorf("pc care sop: v%d: %w", version, err)
	}
	if problems := domain.ValidatePCCareSOP(dsl); len(problems) > 0 {
		return domain.Rules{}, false, fmt.Errorf("pc care sop: v%d invalid: %s", version, problems[0])
	}
	return domain.Rules{Version: version, PCCareSOP: dsl}, true, nil
}
