package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/vgoats/goatos/backend/internal/identity/domain"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

func CanonicalRequestHashWithSubject(tenantID, command, route, subjectID string, body []byte) (string, error) {
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return "", err
	}
	canonical := struct {
		TenantID  string `json:"tenant_id"`
		Command   string `json:"command"`
		Route     string `json:"route"`
		SubjectID string `json:"subject_id,omitempty"`
		Body      any    `json:"body"`
	}{
		TenantID:  tenantID,
		Command:   command,
		Route:     route,
		SubjectID: subjectID,
		Body:      decoded,
	}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func validateEvidenceRefs(refs []domain.EvidenceRef, requireNonEmpty bool) error {
	if requireNonEmpty && len(refs) == 0 {
		return BadRequest("missing_evidence_refs", "evidence_refs must contain at least one item")
	}
	for i := range refs {
		ref := &refs[i]
		ref.EvidenceType = strings.TrimSpace(ref.EvidenceType)
		ref.EvidenceID = strings.TrimSpace(ref.EvidenceID)
		if !allowedEvidenceTypes[ref.EvidenceType] {
			return BadRequest("invalid_evidence_ref", "evidence_type is not supported")
		}
		if ref.EvidenceID == "" || len(ref.EvidenceID) > 200 {
			return BadRequest("invalid_evidence_ref", "evidence_id must be between 1 and 200 characters")
		}
		if err := trimAndValidateOptionalString("source_system", ref.SourceSystem, 120, true); err != nil {
			return err
		}
		if err := trimAndValidateOptionalString("description", ref.Description, 500, false); err != nil {
			return err
		}
	}
	return nil
}

func validateOptionalUUID(field string, value *string) error {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" || !uuidPattern.MatchString(trimmed) {
		return BadRequest("invalid_"+field, field+" must be a valid UUID")
	}
	*value = trimmed
	return nil
}

func trimAndValidateOptionalString(field string, value *string, max int, requireNonEmpty bool) error {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if requireNonEmpty && trimmed == "" {
		return BadRequest("invalid_"+field, field+" cannot be empty")
	}
	if len(trimmed) > max {
		return BadRequest("invalid_"+field, fmt.Sprintf("%s must be %d characters or fewer", field, max))
	}
	*value = trimmed
	return nil
}

var allowedIdentifierTypes = map[string]bool{
	"animal_identifier_1": true,
	"animal_identifier_2": true,
}

var allowedEvidenceTypes = map[string]bool{
	"source_record":          true,
	"identifier":             true,
	"goat":                   true,
	"event":                  true,
	"media":                  true,
	"decision":               true,
	"location":               true,
	"actor":                  true,
	"protocol":               true,
	"sop":                    true,
	"vaccination_completion": true,
	"vaccination_obligation": true,
}
