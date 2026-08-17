package domain

import (
	"math"
	"sort"
	"strings"
	"time"
)

// Milk Preparation is the current-day planning read for the milk-fed growth cohorts. The
// preparation page intentionally excludes K0 colostrum and clinical/ICU feeding: neither has an
// approved per-head milk-volume rule in the supplied workflow source, so treating either as zero
// would publish an unsafe instruction.
const (
	MilkPreparationStatusReady   = "ready"
	MilkPreparationStatusBlocked = "blocked"

	milkPreparationSessionCount = 4
	milkCitricAcidGramsPerLitre = 5.5
)

// MilkPreparationQuery scopes the current live-herd plan. AsOf is resolved in the India business
// calendar and is returned to clients as preparation_date; it does not reconstruct historical
// census state.
type MilkPreparationQuery struct {
	TenantID string
	ParkID   *string
	Limit    int32
	Offset   int32
	AsOf     time.Time
}

// MilkPreparationCohortCount is the exact producer grain read from canonical goats:
// physical shed x management stage x partition (OperationalLocation-complete).
type MilkPreparationCohortCount struct {
	ParkID    string
	ParkLabel string
	ShedID    string
	ShedLabel string
	// PartitionLabel is the raw stored label ('1', 'Part 3'), or '' for a non-partitioned shed.
	// Never the literal sentinel "whole".
	PartitionLabel  string
	ManagementStage string
	HeadCount       int
}

// MilkPreparationSession is one feeding-session quantity for a cohort row. RequiredML stays an
// integer so totals never accumulate binary floating-point rounding drift.
type MilkPreparationSession struct {
	SessionNo  int   `json:"session_no"`
	Active     bool  `json:"active"`
	PerHeadML  int   `json:"per_head_ml"`
	RequiredML int64 `json:"required_ml"`
}

// MilkPreparationRow is one physical shed x milk cohort x partition planning row.
type MilkPreparationRow struct {
	ParkID    string `json:"park_id"`
	ParkLabel string `json:"park_label"`
	ShedID    string `json:"shed_id"`
	ShedLabel string `json:"shed_label"`
	// PartitionLabel is the raw stored partition label, or "" for a non-partitioned shed. Never
	// the literal sentinel "whole".
	PartitionLabel string `json:"partition_label,omitempty"`
	// OperationalLocationDisplay is the user-facing ground location: the bare shed name when
	// PartitionLabel is empty ("Yashoda"), otherwise the shed joined to its partition following
	// that shed's own convention ("Castro 2", "Godel 1 - Part 3"). See oploc.OperationalLocation.
	OperationalLocationDisplay string                   `json:"operational_location_display,omitempty"`
	ManagementStage            string                   `json:"management_stage"`
	HeadCount                  int                      `json:"head_count"`
	Sessions                   []MilkPreparationSession `json:"sessions"`
	DailyRequiredML            int64                    `json:"daily_required_ml"`
	Status                     string                   `json:"status"`
	BlockedReason              string                   `json:"blocked_reason,omitempty"`
	VerificationStatus         string                   `json:"verification_status"`
	CompletionID               string                   `json:"completion_id,omitempty"`
	AttemptNo                  int32                    `json:"attempt_no,omitempty"`
	ReworkReason               string                   `json:"rework_reason,omitempty"`
}

// MilkPreparationFarmTask is the only actionable operator grain: one farm on one preparation
// day. Shed breakdown is deliberately absent from the operator contract.
type MilkPreparationFarmTask struct {
	ParkID                  string                         `json:"park_id"`
	ParkLabel               string                         `json:"park_label"`
	CohortCount             int                            `json:"cohort_count"`
	HeadCount               int                            `json:"head_count"`
	TotalRequiredML         int64                          `json:"total_required_ml"`
	MilkDirection           []MilkPreparationDirectionLine `json:"milk_direction"`
	CitricAcidGramsPerLitre float64                        `json:"citric_acid_grams_per_litre"`
	CitricAcidGrams         float64                        `json:"citric_acid_grams"`
	VerificationStatus      string                         `json:"verification_status"`
	CompletionID            string                         `json:"completion_id,omitempty"`
	AttemptNo               int32                          `json:"attempt_no,omitempty"`
	ReworkReason            string                         `json:"rework_reason,omitempty"`
}

// MilkPreparationDirectionLine is one operator-visible line of the legacy Milk Direction
// calculation. It is calculated from canonical live-herd counts and never changes the farm-day
// task grain.
type MilkPreparationDirectionLine struct {
	ManagementStage string `json:"management_stage"`
	HeadCount       int    `json:"head_count"`
	PerHeadML       int    `json:"per_head_ml"`
	SessionCount    int    `json:"session_count"`
	RequiredML      int64  `json:"required_ml"`
}

// MilkPreparationSummary is whole-scope and invariant to page size/offset.
type MilkPreparationSummary struct {
	Scope                        string  `json:"scope"`
	ShedCount                    int     `json:"shed_count"`
	CohortCount                  int     `json:"cohort_count"`
	HeadCount                    int     `json:"head_count"`
	TotalRequiredML              int64   `json:"total_required_ml"`
	CitricAcidGrams              float64 `json:"citric_acid_grams"`
	BlockedRowCount              int     `json:"blocked_row_count"`
	ParkCount                    int     `json:"park_count"`
	NotSubmittedFarmCount        int     `json:"not_submitted_farm_count"`
	PendingVerificationFarmCount int     `json:"pending_verification_farm_count"`
	CompletedFarmCount           int     `json:"completed_farm_count"`
	ReworkFarmCount              int     `json:"rework_farm_count"`
}

// MilkPreparationPage is the backend-owned admin-web contract. Items are one bounded page;
// Summary always covers the complete park scope.
type MilkPreparationPage struct {
	PreparationDate string                    `json:"preparation_date"`
	FeedingDate     string                    `json:"feeding_date"`
	GeneratedAt     time.Time                 `json:"generated_at"`
	Items           []MilkPreparationRow      `json:"items"`
	FarmTasks       []MilkPreparationFarmTask `json:"farm_tasks"`
	Summary         MilkPreparationSummary    `json:"summary"`
	Limit           int32                     `json:"limit"`
	Offset          int32                     `json:"offset"`
	HasMore         bool                      `json:"has_more"`
}

type milkPreparationRule struct {
	PerHeadML      int
	ActiveSessions map[int]bool
}

// MilkK3WindowDays is how many feed days a K3 animal draws milk for after entering the cohort.
//
// Maintainer rule 2026-08-15, and the source ladder agrees: K3 is the WEANING window, days 78-84 of
// a kid's life -- seven days, not a standing cohort. It is counted inclusively from the entry day,
// so an animal that enters on the 1st is prepared for through the 7th and stops on the 8th.
//
// It lives here, next to the volume matrix, because it is the same kind of fact: the authored
// business shape of the K3 cohort. The SQL window in the counts adapter is built from this constant
// rather than repeating a literal, so the two cannot drift.
const MilkK3WindowDays = 7

func milkPreparationRuleForStage(stage string) (milkPreparationRule, bool) {
	switch strings.ToUpper(strings.TrimSpace(stage)) {
	case "K1":
		return milkPreparationRule{PerHeadML: 200, ActiveSessions: map[int]bool{1: true, 2: true, 3: true, 4: true}}, true
	case "K2":
		return milkPreparationRule{PerHeadML: 300, ActiveSessions: map[int]bool{1: true, 2: true, 3: true, 4: true}}, true
	case "K3":
		return milkPreparationRule{PerHeadML: 200, ActiveSessions: map[int]bool{1: true, 4: true}}, true
	default:
		return milkPreparationRule{}, false
	}
}

// BuildMilkPreparationRow applies the approved volume matrix to one canonical cohort count.
func BuildMilkPreparationRow(count MilkPreparationCohortCount) (MilkPreparationRow, bool) {
	rule, ok := milkPreparationRuleForStage(count.ManagementStage)
	if !ok {
		return MilkPreparationRow{}, false
	}
	row := MilkPreparationRow{
		ParkID:             count.ParkID,
		ParkLabel:          count.ParkLabel,
		ShedID:             count.ShedID,
		ShedLabel:          count.ShedLabel,
		PartitionLabel:     count.PartitionLabel,
		ManagementStage:    strings.ToUpper(strings.TrimSpace(count.ManagementStage)),
		HeadCount:          count.HeadCount,
		Sessions:           make([]MilkPreparationSession, 0, milkPreparationSessionCount),
		Status:             MilkPreparationStatusReady,
		VerificationStatus: MilkPreparationVerificationNotSubmitted,
	}
	if strings.TrimSpace(count.ShedID) == "" {
		row.Status = MilkPreparationStatusBlocked
		row.BlockedReason = "missing_shed"
	}
	for sessionNo := 1; sessionNo <= milkPreparationSessionCount; sessionNo++ {
		active := rule.ActiveSessions[sessionNo]
		required := int64(0)
		if active {
			required = int64(count.HeadCount) * int64(rule.PerHeadML)
			row.DailyRequiredML += required
		}
		row.Sessions = append(row.Sessions, MilkPreparationSession{
			SessionNo: sessionNo, Active: active, PerHeadML: rule.PerHeadML, RequiredML: required,
		})
	}
	return row, true
}

// MilkPreparationDailyMLPerHead exposes the source-backed daily requirement for whole-scope
// aggregates while keeping the session matrix in one domain rule.
func MilkPreparationDailyMLPerHead(stage string) (int64, bool) {
	rule, ok := milkPreparationRuleForStage(stage)
	if !ok {
		return 0, false
	}
	active := 0
	for sessionNo := 1; sessionNo <= milkPreparationSessionCount; sessionNo++ {
		if rule.ActiveSessions[sessionNo] {
			active++
		}
	}
	return int64(active * rule.PerHeadML), true
}

// BuildMilkPreparationSummary rolls up the complete filtered row set. Citric acid is 5.5 g/L;
// rounding to one decimal matches the source instruction's precision without hiding millilitres.
func BuildMilkPreparationSummary(rows []MilkPreparationRow) MilkPreparationSummary {
	result := MilkPreparationSummary{Scope: "filtered", CohortCount: len(rows)}
	sheds := make(map[string]struct{})
	for _, row := range rows {
		if row.ShedID != "" {
			sheds[row.ShedID] = struct{}{}
		}
		result.HeadCount += row.HeadCount
		result.TotalRequiredML += row.DailyRequiredML
		if row.Status == MilkPreparationStatusBlocked {
			result.BlockedRowCount++
		}
	}
	result.ShedCount = len(sheds)
	result.CitricAcidGrams = MilkPreparationCitricAcidGrams(result.TotalRequiredML)
	return result
}

// MilkPreparationCitricAcidGrams converts an exact millilitre total using the approved 5.5 g/L
// preparation ratio and rounds only at the display-contract boundary.
func MilkPreparationCitricAcidGrams(totalRequiredML int64) float64 {
	grams := (float64(totalRequiredML) / 1000) * milkCitricAcidGramsPerLitre
	return math.Round(grams*10) / 10
}

// BuildMilkPreparationDirectionBreakdown reproduces the legacy formula at farm scope:
// head count × configured millilitres per session × active session count.
func BuildMilkPreparationDirectionBreakdown(rows []MilkPreparationRow) []MilkPreparationDirectionLine {
	byStage := make(map[string]MilkPreparationDirectionLine, 3)
	for _, row := range rows {
		rule, ok := milkPreparationRuleForStage(row.ManagementStage)
		if !ok {
			continue
		}
		stage := strings.ToUpper(strings.TrimSpace(row.ManagementStage))
		line := byStage[stage]
		line.ManagementStage = stage
		line.HeadCount += row.HeadCount
		line.PerHeadML = rule.PerHeadML
		line.SessionCount = len(rule.ActiveSessions)
		line.RequiredML += row.DailyRequiredML
		byStage[stage] = line
	}
	result := make([]MilkPreparationDirectionLine, 0, len(byStage))
	for _, line := range byStage {
		result = append(result, line)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ManagementStage < result[j].ManagementStage })
	return result
}
