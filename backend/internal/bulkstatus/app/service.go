package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// defaultMaxRows bounds a single synchronous preview/commit request body. The
// worker's throttle — not this cap — is what makes million-row jobs safe; this
// only bounds one HTTP round trip. Very large sets should be split across
// commits (each enqueues an independent, resumable job).
const defaultMaxRows = 20000

// bulk targets excluded from the mass path because the single-goat transition
// requires the critical-action guardrail; preview marks them "blocked".
var criticalHealthTargets = map[string]bool{"quarantine": true, "icu": true}

// health vocabulary accepted on the mass path (guardrail states excluded above).
var bulkHealthTargets = map[string]bool{
	"healthy":         true,
	"sick":            true,
	"under_treatment": true,
	"recovering":      true,
}

// exit lifecycle targets accepted on the mass path. "dead" is intentionally
// absent: EVERY death exit must go through the critical-action guardrail path
// (the dead+died pairing on /admin/goats/{goat_id}/critical-death-exit), which
// carries the obligation-cancel effects a mass update would skip.
//
// This is a per-write guardrail, NOT an admin-only restriction. Death recording
// is a field write — a maintainer-approved operator records a death from the
// mobile Counts module — so the rule is "one animal, one guarded transition",
// not "only an admin may kill a row". Excluding "dead" here keeps that true no
// matter who is calling: the mass path can never become a back door around the
// guardrail. Preview marks a "dead" target blocked; see criticalHealthTargets
// above for the same treatment of quarantine/ICU.
var bulkExitTargets = map[string]bool{
	"sold":        true,
	"culled":      true,
	"transferred": true,
	"lost":        true,
}

func exitedLifecycle(status string) bool {
	switch status {
	case "dead", "sold", "culled", "transferred", "lost", "merged", "inactive":
		return true
	default:
		return false
	}
}

// Service implements preview + commit. It never applies transitions itself; the
// worker does, through the identity module.
type Service struct {
	repo       BulkStatusRepository
	goats      GoatStateReader
	signingKey string
	maxRows    int
}

func NewService(repo BulkStatusRepository, goats GoatStateReader) *Service {
	return &Service{repo: repo, goats: goats, signingKey: DefaultDevSigningKey, maxRows: defaultMaxRows}
}

func (s *Service) WithSigningKey(key string) *Service {
	if trimmed := strings.TrimSpace(key); trimmed != "" {
		s.signingKey = trimmed
	}
	return s
}

func (s *Service) WithMaxRows(n int) *Service {
	if n > 0 {
		s.maxRows = n
	}
	return s
}

// Preview validates the axis + rows, reads live goat state, and returns a
// per-row decision plus a signed token. It does NOT mutate anything.
func (s *Service) Preview(ctx context.Context, input PreviewInput) (*PreviewResponse, error) {
	if err := requireTenant(input.TenantID); err != nil {
		return nil, err
	}
	var body PreviewRequest
	if err := decodeStrict(input.RawBody, &body, "BulkStatusPreviewRequest"); err != nil {
		return nil, err
	}
	axis, err := normalizeAxis(body.Axis)
	if err != nil {
		return nil, err
	}
	rows, err := s.normalizeRows(body.Rows)
	if err != nil {
		return nil, err
	}

	goatIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		goatIDs = append(goatIDs, row.GoatID)
	}
	states, err := s.goats.ReadGoatStates(ctx, input.TenantID, goatIDs)
	if err != nil {
		return nil, mapRepoErr(err)
	}

	// Classify each DISTINCT target once. Reproductive validation hits the DB, so
	// per-row classification would be an N+1 (up to 20k identical EXISTS at the
	// request cap); the distinct-target set is tiny.
	type targetClass struct{ ok, blocked bool }
	targetClasses := make(map[string]targetClass, 4)
	for _, row := range rows {
		if _, seen := targetClasses[row.Target]; seen {
			continue
		}
		ok, blocked, tErr := s.classifyTarget(ctx, axis, row.Target)
		if tErr != nil {
			return nil, mapRepoErr(tErr)
		}
		targetClasses[row.Target] = targetClass{ok: ok, blocked: blocked}
	}

	resp := &PreviewResponse{
		Axis:    axis,
		Rows:    make([]PreviewRowResult, 0, len(rows)),
		TraceID: input.TraceID,
	}
	for _, row := range rows {
		result := PreviewRowResult{GoatID: row.GoatID, Target: row.Target}
		tc := targetClasses[row.Target]
		switch {
		case !tc.ok:
			result.Decision = DecisionRequiresReview
			result.Message = fmt.Sprintf("unsupported %s target %q", axis, row.Target)
		case tc.blocked:
			result.Decision = DecisionBlocked
			result.Message = "target requires the critical-action guardrail path and cannot run as a bulk update"
		default:
			result = decideAgainstState(axis, row, states[row.GoatID])
		}
		s.tallyDecision(&resp.Summary, result.Decision)
		resp.Rows = append(resp.Rows, result)
	}
	resp.Summary.Total = len(resp.Rows)

	fingerprint := rowsFingerprint(input.TenantID, axis, toEnqueueRows(rows))
	token, err := s.signPreviewToken(input.TenantID, axis, fingerprint, len(rows))
	if err != nil {
		return nil, fmt.Errorf("bulk status preview token generation failed: %w", err)
	}
	resp.PreviewToken = token
	return resp, nil
}

// Commit verifies the token then enqueues a durable job + one row per goat. It
// must not apply 1M transitions inline — enqueue is a set-based insert only.
func (s *Service) Commit(ctx context.Context, input CommitInput) (*CommitResponse, error) {
	if err := requireTenant(input.TenantID); err != nil {
		return nil, err
	}
	actorID := strings.TrimSpace(input.ActorID)
	if !uuidPattern.MatchString(actorID) {
		return nil, Unauthorized("missing_actor_scope", "actor scope is required")
	}
	idempotencyKey := strings.TrimSpace(input.IdempotencyKey)
	if idempotencyKey == "" {
		return nil, BadRequest("missing_idempotency_key", "Idempotency-Key header is required")
	}
	var body CommitRequest
	if err := decodeStrict(input.RawBody, &body, "BulkStatusCommitRequest"); err != nil {
		return nil, err
	}
	axis, err := normalizeAxis(body.Axis)
	if err != nil {
		return nil, err
	}
	rows, err := s.normalizeRows(body.Rows)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(body.PreviewToken) == "" {
		return nil, BadRequest("missing_preview_token", "preview_token is required")
	}
	enqueueRows := toEnqueueRows(rows)
	fingerprint := rowsFingerprint(input.TenantID, axis, enqueueRows)
	if err := s.verifyPreviewToken(input.TenantID, axis, fingerprint, len(rows), body.PreviewToken); err != nil {
		return nil, err
	}
	result, err := s.repo.EnqueueJob(ctx, EnqueueJobParams{
		TenantID:       input.TenantID,
		ActorID:        actorID,
		Axis:           axis,
		IdempotencyKey: idempotencyKey,
		Fingerprint:    fingerprint,
		Rows:           enqueueRows,
	})
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &CommitResponse{
		JobID:     result.JobID,
		Axis:      axis,
		TotalRows: result.TotalRows,
		State:     result.State,
		Replayed:  result.Replayed,
		TraceID:   input.TraceID,
	}, nil
}

// classifyTarget reports whether a target is a supported value for the axis and
// whether it is a guardrail-blocked value. This is a boundary check for a clean
// preview; the identity transition remains the authoritative enforcement point.
func (s *Service) classifyTarget(ctx context.Context, axis, target string) (ok bool, blocked bool, err error) {
	switch axis {
	case AxisReproductive:
		exists, err := s.repo.ReproductiveStatusExists(ctx, target)
		if err != nil {
			return false, false, err
		}
		return exists, false, nil
	case AxisHealth:
		if criticalHealthTargets[target] {
			return true, true, nil
		}
		return bulkHealthTargets[target], false, nil
	case AxisExit:
		if target == "dead" {
			return true, true, nil
		}
		return bulkExitTargets[target], false, nil
	default:
		return false, false, nil
	}
}

func (s *Service) tallyDecision(summary *PreviewSummary, decision string) {
	switch decision {
	case DecisionApply:
		summary.Apply++
	case DecisionNoop:
		summary.Noop++
	case DecisionNotFound:
		summary.NotFound++
	case DecisionBlocked:
		summary.Blocked++
	default:
		summary.RequiresReview++
	}
}

func decideAgainstState(axis string, row EnqueueRow, state GoatState) PreviewRowResult {
	result := PreviewRowResult{GoatID: row.GoatID, Target: row.Target}
	if !state.Exists {
		result.Decision = DecisionNotFound
		result.Message = "goat not found in tenant scope"
		return result
	}
	rowVersion := state.RowVersion
	result.RowVersion = &rowVersion
	if state.Merged {
		result.Decision = DecisionNotFound
		result.Message = "goat identity was merged into a survivor"
		return result
	}
	if exitedLifecycle(state.LifecycleStatus) {
		result.CurrentState = state.LifecycleStatus
		result.Decision = DecisionNotFound
		result.Message = "goat has already exited the active herd"
		return result
	}
	switch axis {
	case AxisReproductive:
		result.CurrentState = state.ReproductiveStatus
	case AxisHealth:
		result.CurrentState = state.HealthStatus
	case AxisExit:
		result.CurrentState = state.LifecycleStatus
	}
	if result.CurrentState == row.Target {
		result.Decision = DecisionNoop
		result.Message = "goat is already at the requested status"
		return result
	}
	result.Decision = DecisionApply
	return result
}

func (s *Service) normalizeRows(in []PreviewRow) ([]EnqueueRow, error) {
	if len(in) == 0 {
		return nil, BadRequest("missing_rows", "rows must contain at least one item")
	}
	if len(in) > s.maxRows {
		return nil, BadRequest("bulk_too_large", fmt.Sprintf("a single request supports at most %d rows", s.maxRows))
	}
	out := make([]EnqueueRow, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for i := range in {
		goatID := strings.TrimSpace(in[i].GoatID)
		if !uuidPattern.MatchString(goatID) {
			return nil, BadRequest("invalid_goat_id", fmt.Sprintf("row %d: goat_id must be a valid UUID", i+1))
		}
		if _, dup := seen[goatID]; dup {
			return nil, BadRequest("duplicate_goat_id", fmt.Sprintf("row %d: goat_id %s appears more than once", i+1, goatID))
		}
		seen[goatID] = struct{}{}
		target := strings.TrimSpace(in[i].Target)
		if target == "" || len(target) > 80 {
			return nil, BadRequest("invalid_target", fmt.Sprintf("row %d: target must be between 1 and 80 characters", i+1))
		}
		if strings.ContainsAny(target, "\n\r\t") {
			return nil, BadRequest("invalid_target", fmt.Sprintf("row %d: target must be a single line value", i+1))
		}
		reason := strings.TrimSpace(in[i].Reason)
		if len(reason) > 500 {
			return nil, BadRequest("invalid_reason", fmt.Sprintf("row %d: reason must be 500 characters or fewer", i+1))
		}
		out = append(out, EnqueueRow{GoatID: goatID, Target: target, Reason: reason})
	}
	return out, nil
}

func toEnqueueRows(rows []EnqueueRow) []EnqueueRow {
	// Rows are already normalized EnqueueRow values; returned as-is so preview
	// and commit fingerprint the identical slice.
	return rows
}

func normalizeAxis(raw string) (string, error) {
	axis := strings.TrimSpace(strings.ToLower(raw))
	switch axis {
	case AxisReproductive, AxisHealth, AxisExit:
		return axis, nil
	default:
		return "", BadRequest("invalid_axis", "axis must be reproductive, health, or exit")
	}
}

func requireTenant(tenantID string) error {
	if strings.TrimSpace(tenantID) == "" {
		return Unauthorized("missing_tenant_scope", "tenant scope is required")
	}
	return nil
}

func decodeStrict(raw []byte, dest any, schemaName string) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return BadRequest("invalid_json", "request body is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		return BadRequest("invalid_json", "request body must match "+schemaName)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return BadRequest("invalid_json", "request body must contain a single JSON object")
	}
	return nil
}
