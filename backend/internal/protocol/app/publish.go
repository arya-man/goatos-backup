package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// ErrNotPublishable is returned when a protocol version fails the source-backed approval gate.
// The wrapped message carries the specific reason for the UI/API.
var ErrNotPublishable = errors.New("protocol: version not publishable")

// publishableSources are the real source systems whose approved values may be published. Anything
// else (manual_admin, extracted, empty) stays draft / not source-backed.
var publishableSources = map[string]bool{"vaccinations_db": true, "phc": true, "vet": true}

type sourceMeta struct {
	SourceSystem string `json:"source_system"`
	SourceRef    string `json:"source_ref"`
	ReviewStatus string `json:"review_status"`
	ApprovedBy   string `json:"approved_by"`
}

type ruleDSLEnvelope struct {
	Source   sourceMeta    `json:"source"`
	Schedule []scheduleRow `json:"schedule"`
}

type scheduleRow struct {
	DoseCode    string          `json:"dose_code"`
	SOPVersion  string          `json:"sop_version"`
	ProofPolicy json.RawMessage `json:"proof_policy"`
}

// ValidatePublishable enforces the source-backed approval gate on a version's rule_dsl: the nested
// source object must be a real source (vaccinations_db/phc/vet), carry a source_ref, be
// review_status='approved', and name an approved_by. Pure logic — no DB. Mirrors the config-mock
// gate; this is the backend source of truth. Never publishes manual/extracted/unsourced values.
func ValidatePublishable(ruleDSL []byte) error {
	var env ruleDSLEnvelope
	if len(ruleDSL) > 0 {
		if err := json.Unmarshal(ruleDSL, &env); err != nil {
			return fmt.Errorf("%w: invalid rule_dsl: %v", ErrNotPublishable, err)
		}
	}
	s := env.Source
	switch {
	case !publishableSources[s.SourceSystem]:
		return fmt.Errorf("%w: source_system must be vaccinations_db/phc/vet (got %q)", ErrNotPublishable, s.SourceSystem)
	case s.SourceRef == "":
		return fmt.Errorf("%w: source_ref required", ErrNotPublishable)
	case s.ReviewStatus != "approved":
		return fmt.Errorf("%w: review_status must be approved (got %q)", ErrNotPublishable, s.ReviewStatus)
	case s.ApprovedBy == "":
		return fmt.Errorf("%w: approved_by required", ErrNotPublishable)
	}
	return nil
}

func ValidateExecutionContract(v domain.Version) error {
	if strings.TrimSpace(v.SopVersionID) == "" {
		return fmt.Errorf("%w: missing sop_version_id", ErrNotPublishable)
	}
	if !nonEmptyJSONObject(v.ProofPolicy) {
		return fmt.Errorf("%w: missing proof_policy", ErrNotPublishable)
	}
	var env ruleDSLEnvelope
	if len(v.RuleDsl) > 0 {
		if err := json.Unmarshal(v.RuleDsl, &env); err != nil {
			return fmt.Errorf("%w: invalid rule_dsl: %v", ErrNotPublishable, err)
		}
	}
	for idx, row := range env.Schedule {
		if strings.TrimSpace(row.SOPVersion) == "" && strings.TrimSpace(v.SopVersionID) == "" {
			return fmt.Errorf("%w: schedule[%d] missing sop_version", ErrNotPublishable, idx)
		}
		if len(row.ProofPolicy) == 0 && !nonEmptyJSONObject(v.ProofPolicy) {
			return fmt.Errorf("%w: schedule[%d] missing proof_policy", ErrNotPublishable, idx)
		}
	}
	return nil
}

func nonEmptyJSONObject(raw []byte) bool {
	var m map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &m) != nil {
		return false
	}
	return len(m) > 0
}

// PublishVersion publishes a draft version only after the source-backed gate passes. The DB also
// enforces the published-window EXCLUDE non-overlap; capability (CEO/COO protocol.publish.*) is
// enforced at the API/RBAC boundary (later slice).
func (s *Service) PublishVersion(ctx context.Context, tenantID, versionID string, publishedBy *string) error {
	v, err := s.repo.GetVersion(ctx, tenantID, versionID)
	if err != nil {
		return err
	}
	if err := ValidatePublishable(v.RuleDsl); err != nil {
		return err
	}
	if err := ValidateExecutionContract(v); err != nil {
		return err
	}
	return s.repo.PublishVersion(ctx, tenantID, versionID, publishedBy)
}
