package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Rule lineage: how a rule is recognised across protocol versions.
//
// protocol_rules.rule_id is a fresh UUID per version, because publishing rewrites every rule
// row. Two derived values answer the questions rule_id cannot:
//
//	identity_key        WHICH rule is this, in business terms -- stable across versions
//	content_fingerprint WHAT does it say -- changes if and only if what an animal owes can change
//
// Carry-over (see docs/preventive-care-vaccination/additive-publish.md) rebinds an animal's open
// obligations to the new version only when BOTH match, so a publish that adds a sixth vaccine
// leaves the other five exactly where they were.

// RuleIdentityKey names a rule by business identity: which vaccine, which dose, which position
// in the course. Case and surrounding space are not identity, so both are normalised away.
//
// The separator is a pipe character rather than a null byte: vaccine/dose codes follow the pattern
// [a-z0-9_] and never contain |, so collision is not a concern. We use | instead of \x00 because
// PostgreSQL text fields cannot store null bytes (UTF-8 encoding violation).
func RuleIdentityKey(vaccineCode, doseCode string, sequence int32) string {
	return strings.Join([]string{
		strings.ToLower(strings.TrimSpace(vaccineCode)),
		strings.ToLower(strings.TrimSpace(doseCode)),
		strconv.FormatInt(int64(sequence), 10),
	}, "|")
}

// RuleContentFingerprint hashes every field that can change WHAT an animal owes or WHEN.
//
// Deliberately excluded: sort_order and created_at. Reordering the vaccine list in the editor,
// or republishing an unchanged plan, must not reschedule anything.
//
// Returns "" when the rule carries JSON that cannot be canonicalised. A blank fingerprint never
// matches, so the rule takes the cancel-and-regenerate path -- the behaviour that shipped before
// carry-over existed. Failing safe means falling back to that, never to an unverified carry-over.
func RuleContentFingerprint(in NewRule) string {
	eligibility, err := canonicalJSON(in.EligibilityJSON)
	if err != nil {
		return ""
	}
	proof, err := canonicalJSON(in.ProofPolicy)
	if err != nil {
		return ""
	}
	sop := ""
	if in.SopVersionID != nil {
		sop = strings.TrimSpace(*in.SopVersionID)
	}
	withdrawal := ""
	if in.WithdrawalDays != nil {
		withdrawal = strconv.FormatInt(int64(*in.WithdrawalDays), 10)
	}

	// Field name is hashed alongside its value so that moving a value between two fields of the
	// same type changes the fingerprint.
	parts := []string{
		"trigger_type=" + strings.ToLower(strings.TrimSpace(in.TriggerType)),
		"offset_days=" + strconv.FormatInt(int64(in.OffsetDays), 10),
		"due_window_days=" + strconv.FormatInt(int64(in.DueWindowDays), 10),
		"min_gap_days=" + strconv.FormatInt(int64(in.MinGapDays), 10),
		"repeat=" + strings.ToLower(strings.TrimSpace(in.Repeat)),
		"repeat_until_after_age=" + strings.ToLower(strings.TrimSpace(in.RepeatUntilAfterAge)),
		"catch_up=" + strings.ToLower(strings.TrimSpace(in.CatchUp)),
		"sop_version_id=" + sop,
		"withdrawal_days=" + withdrawal,
		"eligibility_json=" + eligibility,
		"proof_policy=" + proof,
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:])
}

// canonicalJSON re-renders a JSON document so that two semantically identical documents produce
// identical bytes. encoding/json marshals map keys in sorted order, so a decode/encode round trip
// through interface{} normalises both key order and insignificant whitespace.
//
// An absent document and an explicit null are the same absence and canonicalise to "null", so a
// publisher that starts writing "{}" where it previously omitted a field does not, by that alone,
// look like a rule change.
func canonicalJSON(raw []byte) (string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return "null", nil
	}
	var doc any
	if err := json.Unmarshal([]byte(trimmed), &doc); err != nil {
		return "", fmt.Errorf("protocol: rule fingerprint: %w", err)
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("protocol: rule fingerprint: %w", err)
	}
	return string(out), nil
}

// VaccineCodeForRule reads the vaccine a rule belongs to out of its eligibility payload.
//
// The publisher folds the matrix row's vaccine block into eligibility_json under "vaccine", and
// generation reads the vaccine back from exactly that field. Deriving the identity key from the
// same place keeps the publisher and the scheduler from disagreeing about which vaccine a rule is
// for -- a disagreement that would silently carry over the wrong rule.
func VaccineCodeForRule(eligibilityJSON []byte) string {
	trimmed := strings.TrimSpace(string(eligibilityJSON))
	if trimmed == "" || trimmed == "null" {
		return ""
	}
	var meta struct {
		Vaccine struct {
			Code string `json:"code"`
		} `json:"vaccine"`
	}
	if err := json.Unmarshal([]byte(trimmed), &meta); err != nil {
		return ""
	}
	return strings.TrimSpace(meta.Vaccine.Code)
}
