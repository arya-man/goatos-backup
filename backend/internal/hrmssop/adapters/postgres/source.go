// Package postgres reads the HRMS SOP (the published version, or the one a record was pinned to).
// It is the only HRMS file that names the sop_* tables; the workforce module holds the result.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/hrmssop/domain"
)

// ErrVersionUnknown: a record names a version that does not exist (or was never published).
var ErrVersionUnknown = errors.New("HRMS SOP version unknown")

type Source struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewSource(pool *pgxpool.Pool, timeout time.Duration) *Source {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Source{pool: pool, timeout: timeout}
}

const sqlPublished = `
SELECT v.version, v.form_dsl
FROM public.sop_versions v
JOIN public.sop_definitions d ON d.tenant_id = v.tenant_id AND d.sop_id = v.sop_id
WHERE v.tenant_id = $1::uuid AND d.code = $2 AND v.status = 'published'
ORDER BY v.version DESC
LIMIT 1`

const sqlVersion = `
SELECT v.version, v.form_dsl
FROM public.sop_versions v
JOIN public.sop_definitions d ON d.tenant_id = v.tenant_id AND d.sop_id = v.sop_id
WHERE v.tenant_id = $1::uuid AND d.code = $2 AND v.version = $3 AND v.status IN ('published', 'retired')
LIMIT 1`

// Published is the version in force; with none published it is the seed at version 0.
func (s *Source) Published(ctx context.Context, tenantID string) (domain.Rules, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	rules, found, err := s.read(ctx, s.pool.QueryRow(ctx, sqlPublished, tenantID, domain.SOPCode))
	if err != nil || found {
		return rules, err
	}
	return domain.Rules{Version: 0, Document: domain.Seed()}, nil
}

// Version is the pinned version a record was made on.
func (s *Source) Version(ctx context.Context, tenantID string, version int) (domain.Rules, error) {
	if version == 0 {
		return domain.Rules{Version: 0, Document: domain.Seed()}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	rules, found, err := s.read(ctx, s.pool.QueryRow(ctx, sqlVersion, tenantID, domain.SOPCode, version))
	if err != nil {
		return domain.Rules{}, err
	}
	if !found {
		return domain.Rules{}, ErrVersionUnknown
	}
	return rules, nil
}

func (s *Source) read(_ context.Context, row pgx.Row) (domain.Rules, bool, error) {
	var version int
	var raw []byte
	if err := row.Scan(&version, &raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Rules{}, false, nil
		}
		return domain.Rules{}, false, err
	}
	var formDSL map[string]any
	if err := json.Unmarshal(raw, &formDSL); err != nil {
		return domain.Rules{}, false, fmt.Errorf("HRMS SOP version %d: %w", version, err)
	}
	section, ok := formDSL[domain.Section]
	if !ok {
		// A version without the section runs the seed's rules under its own number.
		return domain.Rules{Version: version, Document: domain.Seed()}, true, nil
	}
	doc, problems := domain.Parse(section)
	if len(problems) > 0 {
		// Save-time validation should make this impossible; refuse rather than guess.
		return domain.Rules{}, false, fmt.Errorf("HRMS SOP version %d is not valid: %s", version, problems[0])
	}
	return domain.Rules{Version: version, Document: doc}, true, nil
}
