package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/tasks/domain"
)

// publishedFollowUpSQL: one indexed lookup (sop_versions_one_published_per_sop_idx + sop_definitions
// (tenant_id, code) unique) -- no scan, no fan-out.
const publishedFollowUpSQL = `
SELECT sv.sop_version_id::text, sv.form_dsl
FROM sop_versions sv
JOIN sop_definitions sd ON sd.tenant_id = sv.tenant_id AND sd.sop_id = sv.sop_id
WHERE sv.tenant_id = $1::uuid AND sd.code = $2 AND sv.status = 'published'
LIMIT 1`

// activeTaskTypesSQL reads the (tens of rows) registry by its primary key prefix.
const activeTaskTypesSQL = `
SELECT task_type_key, answer_kind, engine_hook
FROM sop_task_types
WHERE tenant_id = $1::uuid AND status = 'active'`

// SOP-DRIVEN OPEN (maintainer decision 2026-09-13, docs/decisions/sop-driven-herd-operations.md).
//
// compileTemplate resolves the workflow shape for a template key from the database instead of
// from Go: the tenant's PUBLISHED sop_versions row for the SOP code that authors the template
// (domain.TemplateKeyToSOP) plus the tenant's active Task Type Registry. The version id is
// returned so OpenWorkflow can PIN it on the instance -- a later publish changes only workflows
// opened after it.
//
// This is a deliberate cross-module READ of sop_versions / sop_definitions / sop_task_types: the
// SOP module is the canonical store of authored operating procedure and the tasks engine is its
// consumer. It fails CLOSED: no published version, no follow_up section, no track for the key, or
// a document the compiler rejects all refuse to open the workflow with a named error rather than
// stamping steps the maintainer never authored. There is no Go-template fallback any more; the
// seeded v1 documents (migration 000299) guarantee every tenant has one.
func (r *Repository) compileTemplate(ctx context.Context, tenantID, templateKey string, opts domain.CompileOptions) (domain.Template, string, error) {
	sopCode, trackKey, ok := domain.TemplateKeyToSOP(templateKey)
	if !ok {
		return domain.Template{}, "", domain.ErrUnknownTemplate
	}
	var (
		versionID string
		formDSL   []byte
	)
	err := r.pool.QueryRow(ctx, publishedFollowUpSQL, tenantID, sopCode).Scan(&versionID, &formDSL)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Template{}, "", fmt.Errorf("%w: no published %s version for template %q", domain.ErrFollowUpMissing, sopCode, templateKey)
	}
	if err != nil {
		return domain.Template{}, "", err
	}
	var dsl map[string]any
	if err := json.Unmarshal(formDSL, &dsl); err != nil {
		return domain.Template{}, "", fmt.Errorf("%w: %s form_dsl: %v", domain.ErrFollowUpInvalid, sopCode, err)
	}
	followUp, err := domain.ParseFollowUp(dsl)
	if err != nil {
		return domain.Template{}, "", fmt.Errorf("%s: %w", sopCode, err)
	}
	track, ok := followUp.Track(trackKey)
	if !ok {
		return domain.Template{}, "", fmt.Errorf("%w: %s has no track %q", domain.ErrFollowUpTrackMissing, sopCode, trackKey)
	}
	registry, err := r.taskTypeRegistry(ctx, tenantID)
	if err != nil {
		return domain.Template{}, "", err
	}
	if problems := domain.ValidateFollowUp(followUp, registry); len(problems) > 0 {
		return domain.Template{}, "", fmt.Errorf("%w: %s: %v", domain.ErrFollowUpInvalid, sopCode, problems)
	}
	template, err := domain.CompileTrack(track, registry, opts)
	if err != nil {
		return domain.Template{}, "", err
	}
	return template, versionID, nil
}

// taskTypeRegistry reads the tenant's active Task Type Registry rows.
func (r *Repository) taskTypeRegistry(ctx context.Context, tenantID string) (domain.TaskTypeRegistry, error) {
	rows, err := r.pool.Query(ctx, activeTaskTypesSQL, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := domain.TaskTypeRegistry{}
	for rows.Next() {
		var key, answer, hook string
		if err := rows.Scan(&key, &answer, &hook); err != nil {
			return nil, err
		}
		out[key] = domain.FollowUpTaskTy{Key: key, AnswerKind: answer, EngineHook: hook}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: tenant has no active task types (migration 000299 not applied?)", domain.ErrFollowUpInvalid)
	}
	return out, nil
}

func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func mustProofJSON(items []domain.ProofItem) string {
	if items == nil {
		items = []domain.ProofItem{}
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return "[]"
	}
	return string(raw)
}
