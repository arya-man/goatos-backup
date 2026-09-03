package domain

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// VERIFIER WEIGHT CORRECTION (maintainer decision 2026-08-17; head-count editing
// RETIRED by maintainer decision 2026-08-24).
//
// The verifier watches the proof video and can fix the number the operator typed.
// She is the only role that may: this rides on permissions.VerificationVerdict,
// the same verifier-exclusive capability that owns approve/reject, so the person
// who judges the evidence is the person who fixes what the evidence shows.
//
// THE CORRECTED VALUE REPLACES THE RECORDED ONE. On an individual capture it
// replaces that one animal's weight and nothing else; on a lump-sum capture it
// replaces the shed total and the stored average is recomputed against the
// RECORDED head count. The head count itself is NOT correctable by anyone: since
// 2026-08-24 it is snapshotted from the herd register at submit time and frozen
// on the row forever, so a correction that names a count is refused rather than
// silently ignored.
//
// It is deliberately NOT coupled to her verdict. She may correct before deciding,
// or after — including on an item she already approved — until the bucket closes.
// A closed bucket is finished work with a recorded outcome, and reopening that
// outcome is what ReopenScope is for.
//
// Free-flow is untouched: a correction changes a number and records who changed
// it. Nothing here resolves a scanned tag to an animal, reads a roster, or gates
// a scan.

// WeightCorrectionCommand is one verifier correction of one observation.
//
// Grain: ONE observation, named by the id the verification item carries in
// Source.RefID. RefType picks the grain the same way the verdict consumer does —
// VerificationRefTypeAnimal is an individual capture, VerificationRefTypeShed is
// a lump-sum shed capture — so the caller never has to guess which table holds it.
type WeightCorrectionCommand struct {
	TenantID      string
	ObservationID string
	// RefType is VerificationRefTypeAnimal or VerificationRefTypeShed.
	RefType string
	// WeightKg is the corrected weight: one animal's weight for an individual
	// capture, the whole shed total for a lump-sum one.
	WeightKg float64
	// AnimalCount is REFUSED whenever it is non-zero, on BOTH grains (maintainer
	// decision 2026-08-24). The lump-sum head count is snapshotted from the herd
	// register at submit and is immutable; an individual observation weighs
	// exactly one animal. Zero means "no count sent", the only accepted value.
	// The field stays on the wire so an installed APK that still sends a count
	// gets an honest refusal instead of a silently dropped edit.
	AnimalCount int
	// Reason is the verifier's own words. Optional: the video is the evidence and
	// the corrected number speaks for itself, so requiring prose would only teach
	// her to type a filler character.
	Reason string
	// CorrectedBy is the verifier's user id, recorded on the row.
	CorrectedBy    string
	IdempotencyKey string
}

// WeightCorrectionResult is the readback of a correction, and is persisted as the
// idempotency result snapshot so an exact replay returns this same value without
// rewriting anything.
//
// It carries BOTH numbers on purpose. The client that just sent the correction
// needs the new value to render, and the audit trail needs the old one to be
// legible without a second read.
type WeightCorrectionResult struct {
	ObservationID string `json:"observation_id"`
	RefType       string `json:"ref_type"`
	CampaignID    string `json:"campaign_id"`
	// CampaignShedID is the bucket this observation belongs to. It is what a client
	// refreshes after a correction lands.
	CampaignShedID string `json:"campaign_shed_id"`
	// WeightKg / AnimalCount are the values AFTER the correction.
	WeightKg    float64 `json:"weight_kg"`
	AnimalCount int     `json:"animal_count,omitempty"`
	// AverageWeightKg is recomputed from the corrected total and count. Lump-sum only.
	AverageWeightKg float64 `json:"average_weight_kg,omitempty"`
	// PreviousWeightKg / PreviousAnimalCount are what the row held immediately
	// before this correction — the operator's values on a first correction, the
	// prior verifier's on a later one.
	PreviousWeightKg    float64 `json:"previous_weight_kg"`
	PreviousAnimalCount int     `json:"previous_animal_count,omitempty"`
	// OperatorWeightKg / OperatorAnimalCount are what the OPERATOR originally
	// recorded, preserved across every later correction. On a first correction
	// they equal the Previous* pair; on a second they still hold the operator's
	// numbers while Previous* holds the first verifier's.
	OperatorWeightKg    float64 `json:"operator_weight_kg"`
	OperatorAnimalCount int     `json:"operator_animal_count,omitempty"`
	Reason              string  `json:"reason,omitempty"`
	CorrectedBy         string  `json:"corrected_by"`
	// SubjectLabel is the REcomposed verifier-facing sentence for this observation,
	// carrying the corrected weight. The caller pushes it back onto the verification
	// item so the queue stops advertising the number that was just replaced.
	SubjectLabel string    `json:"subject_label"`
	CorrectedAt  time.Time `json:"corrected_at"`
}

// MaxCorrectableWeightKg / MinCorrectableWeightKg bound a corrected weight.
//
// The floor is the database's own CHECK (weight_kg > 0) expressed at the grain the
// scale actually reads: a weight rounds to three decimals, so anything under a
// gram is a typo or an empty field, never a goat.
//
// The ceiling is a TYPO GUARD, not a biological claim. It is generous enough that a
// whole-shed total cannot hit it in normal work, and exists so a slipped keypress
// (12 kg typed as 120000) is refused at the door rather than stored and then
// dragged through every average and ADG series that reads this row. A real total
// that legitimately exceeds it is a maintainer conversation, not a client retry.
const (
	MinCorrectableWeightKg = 0.001
	MaxCorrectableWeightKg = 100000
)

// ValidateWeightCorrection is the ONE place the correction rules live, so the HTTP
// adapter, the service and the store cannot disagree about what a valid correction
// is. It returns a stable, client-renderable code for each refusal.
//
// Codes are returned rather than sentences because the copy belongs to the backend
// contract layer, not to a domain validator, and because a client keying on
// "weight_out_of_range" is stable in a way a rewordable sentence is not.
func ValidateWeightCorrection(cmd WeightCorrectionCommand) (WeightCorrectionCommand, string) {
	cmd.TenantID = strings.TrimSpace(cmd.TenantID)
	cmd.ObservationID = strings.TrimSpace(cmd.ObservationID)
	cmd.RefType = strings.TrimSpace(cmd.RefType)
	cmd.Reason = strings.TrimSpace(cmd.Reason)
	cmd.CorrectedBy = strings.TrimSpace(cmd.CorrectedBy)
	cmd.IdempotencyKey = strings.TrimSpace(cmd.IdempotencyKey)

	switch {
	case cmd.TenantID == "":
		return cmd, "missing_tenant"
	case cmd.ObservationID == "":
		return cmd, "missing_observation"
	case cmd.CorrectedBy == "":
		return cmd, "missing_verifier"
	case cmd.IdempotencyKey == "":
		return cmd, "missing_idempotency_key"
	case cmd.RefType != VerificationRefTypeAnimal && cmd.RefType != VerificationRefTypeShed:
		return cmd, "invalid_ref_type"
	}

	// NaN and ±Inf survive JSON decoding into a float64 through some clients and
	// would pass a naive range check (every comparison against NaN is false), so
	// they are refused explicitly before the range test rather than after it.
	if math.IsNaN(cmd.WeightKg) || math.IsInf(cmd.WeightKg, 0) {
		return cmd, "weight_out_of_range"
	}
	// Round to the column's own scale FIRST, so a value that only fails the floor
	// because of its fourth decimal is judged on the number that would be stored.
	cmd.WeightKg = roundKg(cmd.WeightKg)
	if cmd.WeightKg < MinCorrectableWeightKg || cmd.WeightKg > MaxCorrectableWeightKg {
		return cmd, "weight_out_of_range"
	}

	// A head count is refused on BOTH grains (maintainer decision 2026-08-24): an
	// individual capture weighs one animal, and a lump-sum count is snapshotted
	// from the herd register at submit and frozen. Accepting it silently would
	// report success for an edit that did nothing.
	if cmd.AnimalCount != 0 {
		return cmd, "animal_count_not_applicable"
	}
	return cmd, ""
}

// roundKg rounds to the three decimals both weight columns store, so the value
// this package validates is byte-for-byte the value Postgres keeps.
func roundKg(kg float64) float64 {
	return math.Round(kg*1000) / 1000
}

// RecomputeAverageWeightKg derives the lump-sum average from a corrected total and
// head count.
//
// It exists because average_weight_kg is a STORED column with its own
// (average_weight_kg > 0) CHECK: leaving it at the pre-correction value would make
// a shed's own average disagree with its own total, which is exactly the kind of
// silent two-numbers-for-one-fact defect the cross-surface parity rule bans.
//
// A zero or negative count returns 0 and the caller must not write it; the store
// keeps the recorded count in that case, so the situation is unreachable through
// the validated command path.
func RecomputeAverageWeightKg(totalKg float64, animalCount int) float64 {
	if animalCount <= 0 {
		return 0
	}
	return roundKg(totalKg / float64(animalCount))
}

// CorrectedSubjectLabel recomposes the sentence the verifier reads for a corrected
// observation, so the queue row and the drawer header stop advertising the weight
// that was just replaced.
//
// It mirrors individualSubjectLabel / lumpSumSubjectLabel in weighing/app, which
// compose the same sentence at enqueue. The composition lives here so the correct
// path and the capture path cannot drift into two different sentences for the same
// fact; the app-side helpers delegate to it.
func CorrectedSubjectLabel(refType, shedDisplay, scannedIdentifier string, weightKg float64, animalCount int) string {
	shed := strings.TrimSpace(shedDisplay)
	if refType == VerificationRefTypeShed {
		head := shed
		if head == "" {
			head = "Whole pen"
		}
		label := head + " · " + FormatWeightKg(weightKg)
		if animalCount > 0 {
			label += " · " + strconv.Itoa(animalCount) + " goats"
		}
		return label
	}

	parts := make([]string, 0, 3)
	if shed != "" {
		parts = append(parts, shed)
	}
	if tag := strings.TrimSpace(scannedIdentifier); tag != "" {
		parts = append(parts, "Tag "+tag)
	}
	parts = append(parts, FormatWeightKg(weightKg))
	return strings.Join(parts, " · ")
}

// FormatWeightKg always carries the unit — a bare number on a verification screen
// is the exact ambiguity the subject label exists to remove.
func FormatWeightKg(weightKg float64) string {
	return strconv.FormatFloat(weightKg, 'f', 1, 64) + " kg"
}
