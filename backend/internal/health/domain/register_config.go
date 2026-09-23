package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/vgoats/goatos/backend/internal/health/diagnosis"
)

// Authoring contracts for the DIAGNOSIS REGISTER tab of Health Config
// (/health/config -> /health-config/registers/*).
//
// Same shape as the treatment protocols beside it, for the same reason: a register is
// EDITED BY VERSION, never in place. Saving builds a draft; publishing promotes it and
// retires the version it replaces. `health_diagnosis_runs.register_version` pins the
// label a run was produced under, so a proposal stays interpretable after a vet edits
// the rules -- and it pins independently of the treatment version on the case, because
// a dosage correction must not re-date a diagnosis and a rule fix must not rewrite a
// course already being administered.

const (
	RegisterWriteKindDraftSave    = "draft_save"
	RegisterWriteKindDraftPublish = "draft_publish"
	RegisterWriteKindDraftDiscard = "draft_discard"
)

// RegisterSummary is one version as the catalog lists it.
type RegisterSummary struct {
	RegisterVersionID string `json:"register_version_id"`
	AnimalClass       string `json:"animal_class"`

	// TypeLabel is the farm's own name for this type, carried so a screen never has to derive one.
	//
	// The four shipped classes have a copy key each; a type the farm AUTHORS has none, so a client
	// deriving a name fell back to the machine key and rendered "kid warmup" and "mothers" on a
	// screen a vet reads -- exactly what the copy firewall bans. The label is on the type row, so
	// the list carries it rather than leaving every client to invent the same fallback.
	TypeLabel string `json:"type_label"`

	Version       int        `json:"version"`
	Status        string     `json:"status"`
	RegisterLabel string     `json:"register_label"`
	QuestionCount int        `json:"question_count"`
	RuleCount     int        `json:"rule_count"`
	PublishedAt   *time.Time `json:"published_at,omitempty"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// RegisterDetail is one version with its whole document.
type RegisterDetail struct {
	RegisterSummary
	Document diagnosis.AuthoredRegister `json:"document"`

	// Problems is what the author must see BEFORE trying to publish: the same verdict
	// publish will apply, including the warnings publish allows. Showing it on the
	// editor rather than only on the publish button is what turns "rejected" into
	// "not finished yet".
	Problems diagnosis.Problems `json:"problems,omitempty"`
}

type SaveRegisterDraftCommand struct {
	TenantID           string
	ActorID            string
	AnimalClass        string
	Document           diagnosis.AuthoredRegister
	IdempotencyKey     string
	RequestFingerprint string
}

type RegisterVersionCommand struct {
	TenantID           string
	ActorID            string
	RegisterVersionID  string
	IdempotencyKey     string
	RequestFingerprint string
}

// RegisterAuthoringResult is what every register write returns, in the ledger's own
// vocabulary so the API, the audit trail and the screen say the same word for the
// same act.
type RegisterAuthoringResult struct {
	Outcome           string `json:"outcome"`
	AnimalClass       string `json:"animal_class"`
	RegisterVersionID string `json:"register_version_id,omitempty"`
	Version           int    `json:"version,omitempty"`
	RetiredVersionID  string `json:"retired_version_id,omitempty"`
	IdempotentReplay  bool   `json:"idempotent_replay"`

	// Warnings are the non-fatal problems the published document still carries: a
	// token declared but not yet read, a non-specific entry no answer can produce.
	// They are RETURNED rather than swallowed because publish allowed them, and an
	// author who is not told what they published cannot fix it later.
	Warnings diagnosis.Problems `json:"warnings,omitempty"`
}

// RegisterContentHash is what makes a re-save of identical content a no-op instead of
// an indistinguishable new revision. It hashes the document as canonical JSON, so two
// saves that differ only in key order or whitespace hash the same.
func RegisterContentHash(doc diagnosis.AuthoredRegister) (string, error) {
	raw, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	var canonical any
	if err := json.Unmarshal(raw, &canonical); err != nil {
		return "", err
	}
	// Marshalling a decoded value sorts object keys, which is the canonicalisation.
	stable, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(stable)
	return hex.EncodeToString(sum[:]), nil
}

// CatalogItem is one row of the shared item registry as the authoring screens see it.
//
// The ID and the NAME are both carried because the step stores both: the id is the link
// that makes the medicine a real registry row, and the name is the label an operator reads
// off the phone mid-treatment. Keeping the name on the step is deliberate -- renaming an
// item in the registry must not silently rewrite the wording of a course a goat is part
// way through.
type CatalogItem struct {
	ItemID   string `json:"item_id"`
	Name     string `json:"name"`
	Category string `json:"category"`
	// CategoryPath is the editable subcategory the farm filed it under ("Medicines >
	// Antibiotics"), so a picker can group by something the farm chose rather than by the
	// fixed kind.
	CategoryPath string `json:"category_path,omitempty"`
}
