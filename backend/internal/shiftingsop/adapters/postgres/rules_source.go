// Package postgres reads the shifting cards from the SOP library (SHIFTING SOP, maintainer decision
// 2026-09-16): the `shifting` section of a `shifting` sop_versions row, compiled the same way for
// the raise, the completion and the phone.
//
// This is the ONLY place that names sop_versions on counts' behalf; the module receives the rules
// through counts/ports.ShiftingSOPRulesSource as an opaque value -- the feedsop / weighingsop shape.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
)

// RulesSource is the Postgres ShiftingSOPRulesSource. One indexed lookup per uncached version.
//
// A PUBLISHED or RETIRED version's document is immutable (publishing builds a new row; nothing
// edits one in place), so a version resolved once is cached for the life of the process, bounded
// by maxCachedVersions. The published lookup is never cached: a publish must change the very next
// raise.
type RulesSource struct {
	pool         *pgxpool.Pool
	queryTimeout time.Duration

	mu    sync.Mutex
	cache map[cacheKey]domain.ShiftingRules
}

type cacheKey struct {
	tenantID string
	version  int
}

// maxCachedVersions bounds the per-process cache (a tenant publishes a handful of versions a year;
// this is generous and still finite).
const maxCachedVersions = 256

func NewRulesSource(pool *pgxpool.Pool, queryTimeout time.Duration) *RulesSource {
	return &RulesSource{pool: pool, queryTimeout: queryTimeout, cache: map[cacheKey]domain.ShiftingRules{}}
}

var _ ports.ShiftingSOPRulesSource = (*RulesSource)(nil)

const sqlPublishedShiftingSOP = `
SELECT v.version, v.form_dsl
FROM public.sop_versions v
JOIN public.sop_definitions d ON d.tenant_id = v.tenant_id AND d.sop_id = v.sop_id
WHERE v.tenant_id = $1::uuid AND d.code = $2 AND v.status = 'published'
ORDER BY v.version DESC
LIMIT 1`

const sqlShiftingSOPVersions = `
SELECT v.version, v.form_dsl
FROM public.sop_versions v
JOIN public.sop_definitions d ON d.tenant_id = v.tenant_id AND d.sop_id = v.sop_id
WHERE v.tenant_id = $1::uuid AND d.code = $2 AND v.version = ANY($3::int[]) AND v.status IN ('published', 'retired')`

// PublishedRules is what a movement raised now is pinned to. No authored version (a tenant created
// before the migration ran, a test fixture) means the seeded document, version 0; so does a
// published version whose document predates the section.
func (s *RulesSource) PublishedRules(ctx context.Context, tenantID string) (domain.ShiftingRules, error) {
	if s.queryTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.queryTimeout)
		defer cancel()
	}
	var version int
	var raw []byte
	err := s.pool.QueryRow(ctx, sqlPublishedShiftingSOP, tenantID, domain.SOPCodeShifting).Scan(&version, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SeededShiftingRules(), nil
	}
	if err != nil {
		return domain.ShiftingRules{}, fmt.Errorf("shifting sop: read published version: %w", err)
	}
	rules, err := parseRules(version, raw)
	if err != nil {
		return domain.ShiftingRules{}, err
	}
	s.remember(tenantID, rules)
	return rules, nil
}

// RulesVersion is the exact document a movement was pinned to.
func (s *RulesSource) RulesVersion(ctx context.Context, tenantID string, version int) (domain.ShiftingRules, error) {
	if version == 0 {
		return domain.SeededShiftingRules(), nil
	}
	resolved, err := s.RulesVersions(ctx, tenantID, []int{version})
	if err != nil {
		return domain.ShiftingRules{}, err
	}
	rules, ok := resolved[version]
	if !ok {
		return domain.ShiftingRules{}, ports.ErrShiftingSOPVersionUnknown
	}
	return rules, nil
}

// RulesVersions resolves a set of versions in ONE query for the ones not already cached.
func (s *RulesSource) RulesVersions(ctx context.Context, tenantID string, versions []int) (map[int]domain.ShiftingRules, error) {
	out := map[int]domain.ShiftingRules{}
	missing := make([]int, 0, len(versions))
	s.mu.Lock()
	for _, v := range versions {
		if v == 0 {
			out[0] = domain.SeededShiftingRules()
			continue
		}
		if r, ok := s.cache[cacheKey{tenantID, v}]; ok {
			out[v] = r
			continue
		}
		missing = append(missing, v)
	}
	s.mu.Unlock()
	if len(missing) == 0 {
		return out, nil
	}
	if s.queryTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.queryTimeout)
		defer cancel()
	}
	rows, err := s.pool.Query(ctx, sqlShiftingSOPVersions, tenantID, domain.SOPCodeShifting, missing)
	if err != nil {
		return nil, fmt.Errorf("shifting sop: read versions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var version int
		var raw []byte
		if err := rows.Scan(&version, &raw); err != nil {
			return nil, fmt.Errorf("shifting sop: scan version: %w", err)
		}
		rules, err := parseRules(version, raw)
		if err != nil {
			return nil, err
		}
		out[version] = rules
		s.remember(tenantID, rules)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("shifting sop: read versions: %w", err)
	}
	return out, nil
}

func (s *RulesSource) remember(tenantID string, rules domain.ShiftingRules) {
	if rules.Version == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cache) >= maxCachedVersions {
		// Bounded: drop everything rather than grow. A refill costs one indexed read per version.
		s.cache = map[cacheKey]domain.ShiftingRules{}
	}
	s.cache[cacheKey{tenantID, rules.Version}] = rules
}

func parseRules(version int, raw []byte) (domain.ShiftingRules, error) {
	var formDSL map[string]any
	if err := json.Unmarshal(raw, &formDSL); err != nil {
		return domain.ShiftingRules{}, fmt.Errorf("shifting sop: v%d form_dsl: %w", version, err)
	}
	if _, present := formDSL["shifting"]; !present {
		seeded := domain.SeededShiftingRules()
		seeded.Version = version
		return seeded, nil
	}
	dsl, err := domain.ParseShiftingSOP(formDSL)
	if err != nil {
		return domain.ShiftingRules{}, fmt.Errorf("shifting sop: v%d: %w", version, err)
	}
	if problems := domain.ValidateShiftingSOP(dsl); len(problems) > 0 {
		return domain.ShiftingRules{}, fmt.Errorf("shifting sop: v%d invalid: %s", version, problems[0])
	}
	return dsl.Rules(version), nil
}
