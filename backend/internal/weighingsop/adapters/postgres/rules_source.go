// Package postgres reads the weighing rules from the SOP library (WEIGHING SOP, maintainer
// decision 2026-09-15): the `weighing` section of a `weighing.session` sop_versions row,
// compiled the same way for the planner, the write and the phone.
//
// This is the ONLY place that names sop_versions on weighing's behalf. It deliberately lives
// OUTSIDE backend/internal/weighing: that package is ISOLATED from every non-weighing table
// (AGENTS.md) and receives the rules through weighing/ports.SOPRulesSource as an opaque value --
// the feedwaterremoval shape.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// RulesSource is the Postgres SOPRulesSource. One indexed lookup per call; the callers are
// the plan/edit writes and the per-request reads, each of which asks once.
type RulesSource struct {
	pool         *pgxpool.Pool
	queryTimeout time.Duration
}

func NewRulesSource(pool *pgxpool.Pool, queryTimeout time.Duration) *RulesSource {
	return &RulesSource{pool: pool, queryTimeout: queryTimeout}
}

var _ ports.SOPRulesSource = (*RulesSource)(nil)

const sqlPublishedWeighingSOP = `
SELECT v.version, v.form_dsl
FROM public.sop_versions v
JOIN public.sop_definitions d ON d.tenant_id = v.tenant_id AND d.sop_id = v.sop_id
WHERE v.tenant_id = $1::uuid AND d.code = $2 AND v.status = 'published'
ORDER BY v.version DESC
LIMIT 1`

const sqlWeighingSOPVersion = `
SELECT v.version, v.form_dsl
FROM public.sop_versions v
JOIN public.sop_definitions d ON d.tenant_id = v.tenant_id AND d.sop_id = v.sop_id
WHERE v.tenant_id = $1::uuid AND d.code = $2 AND v.version = $3 AND v.status IN ('published', 'retired')
LIMIT 1`

// PublishedRules is what a task planned now runs under. No authored version (a tenant created
// before the migration ran, a test fixture) means the seeded document, version 0. A published
// version whose document predates the section (never on a migrated tenant, but a hand-edited
// row could) also runs the seeded rules rather than failing the plan: the seeded document IS
// the pre-SOP behaviour.
func (s *RulesSource) PublishedRules(ctx context.Context, tenantID string) (domain.Rules, error) {
	rules, found, err := s.read(ctx, sqlPublishedWeighingSOP, tenantID, domain.SOPCodeWeighingSession)
	if err != nil {
		return domain.Rules{}, err
	}
	if !found {
		return domain.SeededRules(), nil
	}
	return rules, nil
}

// RulesVersion is the exact rule set a task was planned on.
func (s *RulesSource) RulesVersion(ctx context.Context, tenantID string, version int) (domain.Rules, error) {
	if version == 0 {
		return domain.SeededRules(), nil
	}
	rules, found, err := s.read(ctx, sqlWeighingSOPVersion, tenantID, domain.SOPCodeWeighingSession, version)
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
		return domain.Rules{}, false, fmt.Errorf("weighing sop: read version: %w", err)
	}
	return parseRules(version, raw)
}

func parseRules(version int, raw []byte) (domain.Rules, bool, error) {
	var formDSL map[string]any
	if err := json.Unmarshal(raw, &formDSL); err != nil {
		return domain.Rules{}, false, fmt.Errorf("weighing sop: v%d form_dsl: %w", version, err)
	}
	if _, present := formDSL["weighing"]; !present {
		seeded := domain.SeededRules()
		seeded.Version = version
		return seeded, true, nil
	}
	dsl, err := domain.ParseWeighingSOP(formDSL)
	if err != nil {
		return domain.Rules{}, false, fmt.Errorf("weighing sop: v%d: %w", version, err)
	}
	if problems := domain.ValidateWeighingSOP(dsl); len(problems) > 0 {
		return domain.Rules{}, false, fmt.Errorf("weighing sop: v%d invalid: %s", version, problems[0])
	}
	return domain.Rules{Version: version, WeighingSOP: dsl}, true, nil
}

// RulesVersions uses one indexed set read per batch, rather than one round trip per pin.
func (s *RulesSource) RulesVersions(ctx context.Context, tenantID string, versions []int) (map[int]domain.Rules, error) {
	if s.queryTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.queryTimeout)
		defer cancel()
	}
	out := map[int]domain.Rules{}
	if len(versions) == 0 {
		return out, nil
	}
	for _, v := range versions {
		if v == 0 {
			out[0] = domain.SeededRules()
		}
	}
	rows, err := s.pool.Query(ctx, sqlWeighingSOPVersions, tenantID, domain.SOPCodeWeighingSession, versions)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var version int
		var raw []byte
		if err := rows.Scan(&version, &raw); err != nil {
			return nil, err
		}
		rules, _, err := parseRules(version, raw)
		if err != nil {
			return nil, err
		}
		out[version] = rules
	}
	return out, rows.Err()
}

const sqlWeighingSOPVersions = `
SELECT v.version, v.form_dsl
FROM public.sop_versions v
JOIN public.sop_definitions d ON d.tenant_id = v.tenant_id AND d.sop_id = v.sop_id
WHERE v.tenant_id = $1::uuid AND d.code = $2 AND v.version = ANY($3::int[])
  AND v.status IN ('published', 'retired')`
