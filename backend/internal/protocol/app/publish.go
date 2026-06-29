package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/protocol/domain"
	"github.com/vgoats/goatos/backend/internal/protocol/ports"
)

// ErrNotPublishable is returned when a protocol version fails the source-backed approval gate.
// The wrapped message carries the specific reason for the UI/API.
var ErrNotPublishable = errors.New("protocol: version not publishable")

// ErrUnsupportedRepeatPolicy is returned when direct protocol authoring attempts to store a repeat
// policy that the generator does not execute yet.
var ErrUnsupportedRepeatPolicy = errors.New("protocol: unsupported repeat policy")

// publishableSources are the real source systems whose approved values may be published. Anything
// else (manual_admin, extracted, empty) stays draft / not source-backed.
var publishableSources = map[string]bool{"vaccinations_db": true, "phc": true, "vet": true}

type sourceMeta struct {
	SourceSystem string `json:"source_system"`
	SourceRef    string `json:"source_ref"`
	ReviewStatus string `json:"review_status"`
	ApprovedBy   string `json:"approved_by"`
	ApprovedAt   string `json:"approved_at"`
}

type ruleDSLEnvelope struct {
	Source   sourceMeta    `json:"source"`
	Schedule []scheduleRow `json:"schedule"`
}

type scheduleRow struct {
	DoseCode    string          `json:"dose_code"`
	SOPVersion  string          `json:"sop_version"`
	Repeat      string          `json:"repeat"`
	ProofPolicy json.RawMessage `json:"proof_policy"`
}

// ValidatePublishable enforces the source-backed approval gate on a version's rule_dsl: the nested
// source object must be a real source (vaccinations_db/phc/vet), carry a source_ref, be
// review_status='approved', and name approved_by + approved_at. Pure logic — no DB. Mirrors the
// config-mock gate; this is the backend source of truth. Never publishes manual/extracted/unsourced
// values.
func ValidatePublishable(ruleDSL []byte) error {
	var env ruleDSLEnvelope
	if len(ruleDSL) > 0 {
		if err := json.Unmarshal(ruleDSL, &env); err != nil {
			return fmt.Errorf("%w: invalid rule_dsl: %v", ErrNotPublishable, err)
		}
	}
	s := env.Source
	switch {
	case !publishableSources[strings.TrimSpace(s.SourceSystem)]:
		return fmt.Errorf("%w: source_system must be vaccinations_db/phc/vet (got %q)", ErrNotPublishable, s.SourceSystem)
	case strings.TrimSpace(s.SourceRef) == "":
		return fmt.Errorf("%w: source_ref required", ErrNotPublishable)
	case strings.TrimSpace(s.ReviewStatus) != "approved":
		return fmt.Errorf("%w: review_status must be approved (got %q)", ErrNotPublishable, s.ReviewStatus)
	case strings.TrimSpace(s.ApprovedBy) == "":
		return fmt.Errorf("%w: approved_by required", ErrNotPublishable)
	case strings.TrimSpace(s.ApprovedAt) == "":
		return fmt.Errorf("%w: approved_at required", ErrNotPublishable)
	}
	if _, err := time.Parse(time.RFC3339, strings.TrimSpace(s.ApprovedAt)); err != nil {
		return fmt.Errorf("%w: approved_at must be RFC3339 (got %q)", ErrNotPublishable, s.ApprovedAt)
	}
	return nil
}

func ValidateExecutionContract(v domain.Version) error {
	if strings.TrimSpace(v.SopVersionID) == "" {
		return fmt.Errorf("%w: missing sop_version_id", ErrNotPublishable)
	}
	if !versionProofHasContent(v.ProofPolicy) {
		return fmt.Errorf("%w: missing proof_policy", ErrNotPublishable)
	}
	var env ruleDSLEnvelope
	if len(v.RuleDsl) > 0 {
		if err := json.Unmarshal(v.RuleDsl, &env); err != nil {
			return fmt.Errorf("%w: invalid rule_dsl: %v", ErrNotPublishable, err)
		}
	}
	for idx, row := range env.Schedule {
		if _, err := normalizeRepeatPolicy(row.Repeat); err != nil {
			return fmt.Errorf("%w: schedule[%d] %v", ErrNotPublishable, idx, err)
		}
		if strings.TrimSpace(row.SOPVersion) == "" && strings.TrimSpace(v.SopVersionID) == "" {
			return fmt.Errorf("%w: schedule[%d] missing sop_version", ErrNotPublishable, idx)
		}
		// A row that supplies its own proof_policy must carry real row-level content. A row that
		// omits proof_policy inherits the version-level proof_policy already validated above. The
		// row's own blank/metadata proof does NOT silently fall back to the version proof.
		if len(row.ProofPolicy) > 0 && !rowProofHasContent(row.ProofPolicy) {
			return fmt.Errorf("%w: schedule[%d] missing proof_policy", ErrNotPublishable, idx)
		}
	}
	return nil
}

func normalizeRepeatPolicy(value string) (string, error) {
	repeat := strings.TrimSpace(value)
	if repeat == "" {
		return "none", nil
	}
	switch repeat {
	case "none", "every_n_days", "yearly":
		return repeat, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnsupportedRepeatPolicy, repeat)
	}
}

// recognizedProofKeys are the object keys under which a proof_policy may carry its proof-token
// array. A token is real only when it is a non-blank string inside one of these arrays.
var recognizedProofKeys = map[string]bool{"required_proofs": true, "types": true, "required": true}

// versionProofHasContent validates a VERSION-level proof_policy (protocol_versions.proof_policy).
// It must be a JSON OBJECT carrying at least one non-blank proof token under a recognized array key
// (required_proofs/types/required). It deliberately rejects everything that is not an object-with-
// real-tokens:
//   - {} , {"required_proofs":[]}            — object but zero tokens
//   - ["video"]                              — array shape is not valid at the version level
//   - {"required_proofs":[""]} / ["  "]      — blank tokens are not requirements
//   - {"subject_scope":"batch"}              — metadata, no recognized proof array
//   - {"required":true}                      — recognized key but scalar, not an array of tokens
func versionProofHasContent(raw []byte) bool {
	var v any
	if len(raw) == 0 || json.Unmarshal(raw, &v) != nil {
		return false
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return false
	}
	for key, val := range obj {
		if recognizedProofKeys[key] && arrayHasNonBlankString(val) {
			return true
		}
	}
	return false
}

// rowProofHasContent validates a ROW-level schedule[].proof_policy. A row may be either a bare array
// of non-blank proof tokens (["video"]) or an object carrying a recognized proof-token array (the
// same object shape as the version level). It rejects blank arrays ([""]), metadata-only objects,
// and scalar values (a bare string/bool/number is never a row proof).
func rowProofHasContent(raw []byte) bool {
	var v any
	if len(raw) == 0 || json.Unmarshal(raw, &v) != nil {
		return false
	}
	switch t := v.(type) {
	case []any:
		return arrayHasNonBlankString(t)
	case map[string]any:
		for key, val := range t {
			if recognizedProofKeys[key] && arrayHasNonBlankString(val) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// arrayHasNonBlankString reports whether v is a JSON array holding at least one non-blank string.
func arrayHasNonBlankString(v any) bool {
	arr, ok := v.([]any)
	if !ok {
		return false
	}
	for _, e := range arr {
		if s, ok := e.(string); ok && strings.TrimSpace(s) != "" {
			return true
		}
	}
	return false
}

// PublishVersion publishes a draft version only after the source-backed gate passes. The DB also
// enforces the published-window EXCLUDE non-overlap; capability (CEO/COO protocol.publish.*) is
// enforced at the API/RBAC boundary (later slice).
func (s *Service) PublishVersion(ctx context.Context, tenantID, versionID string, publishedBy *string, idempotencyKey ...string) error {
	v, err := s.repo.GetVersion(ctx, tenantID, versionID)
	if err != nil {
		return err
	}
	if v.Status != "draft" && v.Status != "published" {
		return fmt.Errorf("%w: status=%q", ports.ErrVersionNotDraft, v.Status)
	}
	if v.Status == "draft" {
		if err := ValidatePublishable(v.RuleDsl); err != nil {
			return err
		}
		if err := ValidateExecutionContract(v); err != nil {
			return err
		}
	}
	if err := s.repo.PublishVersion(ctx, tenantID, versionID, publishedBy, idempotencyKey...); err != nil {
		return err
	}
	return nil
}
