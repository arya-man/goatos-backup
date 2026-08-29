package domain

import (
	"fmt"
	"strings"
	"time"
)

const (
	MilkPreparationVerificationNotSubmitted = "not_submitted"
	MilkPreparationVerificationPending      = "pending_verification"
	MilkPreparationVerificationCompleted    = "completed"
	MilkPreparationVerificationRework       = "rework"

	MilkPreparationStepGoatMilkQuantity   = "goat_milk_quantity"
	MilkPreparationStepBoilingTemperature = "boiling_temperature"
	MilkPreparationStepCooledTemperature  = "cooled_temperature"
	MilkPreparationStepUHTMilkQuantity    = "uht_milk_quantity"
	MilkPreparationStepCitricAcidMixing   = "citric_acid_mixing"

	VerificationVerticalMilkPreparation = "counts"
	VerificationModuleMilkPreparation   = "milk_preparation"
	VerificationCategoryMilkPreparation = "milk_preparation"
	VerificationRefTypeMilkPreparation  = "milk_preparation_completion"
)

// MilkPreparationProofs carries one independent live-camera video for every applicable
// preparation step. Goat-milk quantity/boil/cool are conditional; UHT quantity and citric-acid
// mixing are always required.
type MilkPreparationProofs struct {
	GoatMilkQuantityProofRef   string `json:"goat_milk_quantity_proof_ref,omitempty"`
	BoilingTemperatureProofRef string `json:"boiling_temperature_proof_ref,omitempty"`
	CooledTemperatureProofRef  string `json:"cooled_temperature_proof_ref,omitempty"`
	UHTMilkQuantityProofRef    string `json:"uht_milk_quantity_proof_ref"`
	CitricAcidMixingProofRef   string `json:"citric_acid_mixing_proof_ref"`
}

// MilkPreparationAnswers replaces the legacy Goat Milking/UHT/citric-acid sheet questions.
// Morning and evening collection may legitimately be zero; preparation quantities must be
// positive for every applicable proof-backed step.
type MilkPreparationAnswers struct {
	MorningMilkCollectedLitres float64 `json:"morning_milk_collected_litres"`
	EveningMilkCollectedLitres float64 `json:"evening_milk_collected_litres"`
	GoatMilkQuantityLitres     float64 `json:"goat_milk_quantity_litres,omitempty"`
	BoilingTemperatureC        float64 `json:"boiling_temperature_c,omitempty"`
	CooledTemperatureC         float64 `json:"cooled_temperature_c,omitempty"`
	UHTMilkQuantityLitres      float64 `json:"uht_milk_quantity_litres"`
	CitricAcidGrams            float64 `json:"citric_acid_grams"`
}

func (a MilkPreparationAnswers) Validate(goatMilkUsed bool) error {
	if a.MorningMilkCollectedLitres < 0 || a.EveningMilkCollectedLitres < 0 {
		return fmt.Errorf("milk collected cannot be negative")
	}
	if a.UHTMilkQuantityLitres <= 0 || a.CitricAcidGrams <= 0 {
		return fmt.Errorf("UHT milk quantity and citric acid grams must be positive")
	}
	if goatMilkUsed {
		if a.GoatMilkQuantityLitres <= 0 || a.BoilingTemperatureC <= 0 || a.CooledTemperatureC <= 0 {
			return fmt.Errorf("goat milk quantity, boiling temperature, and cooled temperature must be positive")
		}
	} else if a.GoatMilkQuantityLitres != 0 || a.BoilingTemperatureC != 0 || a.CooledTemperatureC != 0 {
		return fmt.Errorf("goat-milk answers are not applicable when goat_milk_used is false")
	}
	return nil
}

type MilkPreparationStepProof struct {
	StepCode string `json:"step_code"`
	ProofRef string `json:"proof_ref"`
}

func (p MilkPreparationProofs) OrderedStepProofs(goatMilkUsed bool) []MilkPreparationStepProof {
	steps := make([]MilkPreparationStepProof, 0, 5)
	if goatMilkUsed {
		steps = append(steps,
			MilkPreparationStepProof{StepCode: MilkPreparationStepGoatMilkQuantity, ProofRef: strings.TrimSpace(p.GoatMilkQuantityProofRef)},
			MilkPreparationStepProof{StepCode: MilkPreparationStepBoilingTemperature, ProofRef: strings.TrimSpace(p.BoilingTemperatureProofRef)},
			MilkPreparationStepProof{StepCode: MilkPreparationStepCooledTemperature, ProofRef: strings.TrimSpace(p.CooledTemperatureProofRef)},
		)
	}
	return append(steps,
		MilkPreparationStepProof{StepCode: MilkPreparationStepUHTMilkQuantity, ProofRef: strings.TrimSpace(p.UHTMilkQuantityProofRef)},
		MilkPreparationStepProof{StepCode: MilkPreparationStepCitricAcidMixing, ProofRef: strings.TrimSpace(p.CitricAcidMixingProofRef)},
	)
}

func (p MilkPreparationProofs) OrderedRefs(goatMilkUsed bool) []string {
	steps := p.OrderedStepProofs(goatMilkUsed)
	refs := make([]string, 0, len(steps))
	for _, step := range steps {
		refs = append(refs, step.ProofRef)
	}
	return refs
}

func (p MilkPreparationProofs) Validate(goatMilkUsed bool) error {
	if !goatMilkUsed && (strings.TrimSpace(p.GoatMilkQuantityProofRef) != "" ||
		strings.TrimSpace(p.BoilingTemperatureProofRef) != "" ||
		strings.TrimSpace(p.CooledTemperatureProofRef) != "") {
		return fmt.Errorf("goat-milk step videos are not applicable when goat_milk_used is false")
	}
	seen := make(map[string]string, 5)
	for _, step := range p.OrderedStepProofs(goatMilkUsed) {
		if step.ProofRef == "" {
			return fmt.Errorf("video proof is required for step %s", step.StepCode)
		}
		if prior, exists := seen[step.ProofRef]; exists {
			return fmt.Errorf("one video cannot prove both %s and %s", prior, step.StepCode)
		}
		seen[step.ProofRef] = step.StepCode
	}
	return nil
}

type MilkPreparationSubmission struct {
	TenantID        string
	ParkID          string
	PreparationDate time.Time
	FeedingDate     time.Time
	GoatMilkUsed    bool
	Answers         MilkPreparationAnswers
	Proofs          MilkPreparationProofs
	SubmittedBy     string
	SubmittedAt     time.Time
	IdempotencyKey  string
	TraceID         string
}

type MilkPreparationSubmissionResult struct {
	CompletionID string `json:"completion_id"`
	Status       string `json:"status"`
	AttemptNo    int32  `json:"attempt_no"`
	RowVersion   int32  `json:"row_version"`
	NeedsEnqueue bool   `json:"-"`
}

// MilkPreparationUHTConsumption is the verified UHT-milk fact a COMPLETED
// preparation carries: how many litres of UHT the farm opened preparing that
// day's milk. It exists so the feed stock ledger can consume the operator's
// own verified answer instead of the legacy sheet copy (maintainer decision
// 2026-08-22: purchases arrive later via Procurement; consumption comes from
// the app). The day the store depletes is the PREPARATION date — that is when
// the packets are physically opened; feeding happens the next day.
type MilkPreparationUHTConsumption struct {
	TenantID     string
	ParkID       string
	CompletionID string
	// PreparationDate is the ISO business date the milk was prepared (and the
	// UHT consumed).
	PreparationDate string
	AttemptNo       int32
	// UHTMilkQuantityLitres is the accepted attempt's answer; always > 0 on a
	// completed preparation because submission validation rejects otherwise.
	UHTMilkQuantityLitres float64
}

type MilkPreparationVerdictCommand struct {
	TenantID     string
	CompletionID string
	VerifiedBy   string
	Reason       string
	TraceID      string
	OccurredAt   time.Time
}
