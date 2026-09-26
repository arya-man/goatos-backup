// Package postgres reads the feed cards from the SOP library (FEED SOP, maintainer decision
// 2026-09-16): the `feed` section of a feed.direction / feed.packing / feed.transport
// sop_versions row, compiled the same way for the issue, the completion and the phone.
//
// This is the ONLY place that names sop_versions on feeddirection's behalf; the module receives
// the rules through feeddirection/ports.SOPRulesSource as an opaque value -- the weighingsop shape.
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

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
)

// RulesSource is the Postgres SOPRulesSource. One indexed lookup per call.
type RulesSource struct {
	pool         *pgxpool.Pool
	queryTimeout time.Duration
	mu           sync.RWMutex
	versionCache map[feedRulesVersionKey]domain.Rules
}

func NewRulesSource(pool *pgxpool.Pool, queryTimeout time.Duration) *RulesSource {
	return &RulesSource{pool: pool, queryTimeout: queryTimeout, versionCache: make(map[feedRulesVersionKey]domain.Rules)}
}

var _ ports.SOPRulesSource = (*RulesSource)(nil)

const sqlPublishedFeedSOP = `
SELECT v.version, v.form_dsl
FROM public.sop_versions v
JOIN public.sop_definitions d ON d.tenant_id = v.tenant_id AND d.sop_id = v.sop_id
WHERE v.tenant_id = $1::uuid AND d.code = $2 AND v.status = 'published'
ORDER BY v.version DESC
LIMIT 1`

const sqlFeedSOPVersion = `
SELECT v.version, v.form_dsl
FROM public.sop_versions v
JOIN public.sop_definitions d ON d.tenant_id = v.tenant_id AND d.sop_id = v.sop_id
WHERE v.tenant_id = $1::uuid AND d.code = $2 AND v.version = $3 AND v.status IN ('published', 'retired')
LIMIT 1`

// PublishedRules is what a sheet issued now runs under. No authored version (a tenant created
// before the migration ran, a test fixture) means the seeded card, version 0; so does a
// published version whose document predates the section.
func (s *RulesSource) PublishedRules(ctx context.Context, tenantID, stage string) (domain.Rules, error) {
	code, ok := domain.SOPCodeForStage(stage)
	if !ok {
		return domain.Rules{}, fmt.Errorf("feed sop: unknown stage %q", stage)
	}
	rules, found, err := s.read(ctx, stage, sqlPublishedFeedSOP, tenantID, code)
	if err != nil {
		return domain.Rules{}, err
	}
	if !found {
		return domain.SeededRules(stage), nil
	}
	return rules, nil
}

// RulesVersion is the exact card a sheet or task was pinned to.
func (s *RulesSource) RulesVersion(ctx context.Context, tenantID, stage string, version int) (domain.Rules, error) {
	if version == 0 {
		return domain.SeededRules(stage), nil
	}
	code, ok := domain.SOPCodeForStage(stage)
	if !ok {
		return domain.Rules{}, fmt.Errorf("feed sop: unknown stage %q", stage)
	}
	key := feedRulesVersionKey{tenantID: tenantID, stage: stage, version: version}
	s.mu.RLock()
	cached, ok := s.versionCache[key]
	s.mu.RUnlock()
	if ok {
		return cached, nil
	}
	rules, found, err := s.read(ctx, stage, sqlFeedSOPVersion, tenantID, code, version)
	if err != nil {
		return domain.Rules{}, err
	}
	if !found {
		return domain.Rules{}, ports.ErrSOPVersionUnknown
	}
	s.mu.Lock()
	s.versionCache[key] = rules
	s.mu.Unlock()
	return rules, nil
}

type feedRulesVersionKey struct {
	tenantID string
	stage    string
	version  int
}

func (s *RulesSource) read(ctx context.Context, stage, sql string, args ...any) (domain.Rules, bool, error) {
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
		return domain.Rules{}, false, fmt.Errorf("feed sop: read version: %w", err)
	}
	return parseRules(stage, version, raw)
}

func parseRules(stage string, version int, raw []byte) (domain.Rules, bool, error) {
	var formDSL map[string]any
	if err := json.Unmarshal(raw, &formDSL); err != nil {
		return domain.Rules{}, false, fmt.Errorf("feed sop: v%d form_dsl: %w", version, err)
	}
	if _, present := formDSL["feed"]; !present {
		seeded := domain.SeededRules(stage)
		seeded.Version = version
		return seeded, true, nil
	}
	dsl, err := domain.ParseFeedSOP(formDSL)
	if err != nil {
		return domain.Rules{}, false, fmt.Errorf("feed sop: v%d: %w", version, err)
	}
	code, _ := domain.SOPCodeForStage(stage)
	if problems := domain.ValidateFeedSOP(code, dsl); len(problems) > 0 {
		return domain.Rules{}, false, fmt.Errorf("feed sop: v%d invalid: %s", version, problems[0])
	}
	rules, err := dsl.Rules(version, stage)
	if err != nil {
		return domain.Rules{}, false, fmt.Errorf("feed sop: v%d: %w", version, err)
	}
	return rules, true, nil
}
