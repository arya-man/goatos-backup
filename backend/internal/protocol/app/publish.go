package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/vgoats/goatos/backend/internal/protocol/domain"
	"github.com/vgoats/goatos/backend/internal/protocol/ports"
)

// ErrNotPublishable is returned when a protocol version cannot safely generate executable work.
// The wrapped message carries the specific reason for the UI/API.
var ErrNotPublishable = errors.New("protocol: version not publishable")

// ErrInvalidRuleDSL is returned when authored rule_dsl is not valid JSON or carries keys outside the
// versioned protocol-rule DSL schema.
var ErrInvalidRuleDSL = errors.New("protocol: invalid rule_dsl")

// ErrUnsupportedRepeatPolicy is returned when direct protocol authoring attempts to store a repeat
// policy that the generator cannot execute safely.
var ErrUnsupportedRepeatPolicy = errors.New("protocol: unsupported repeat policy")

var executableTriggerTypes = map[string]bool{
	"birth_age":                 true,
	"post_arrival":              true,
	"calendar":                  true,
	"manual_campaign":           true,
	"after_previous_completion": true,
}

type ruleDSLEnvelope struct {
	Category            string          `json:"category"`
	Vaccine             vaccineMeta     `json:"vaccine"`
	Eligibility         json.RawMessage `json:"eligibility"`
	MissedDosePolicy    string          `json:"missed_dose_policy"`
	ProcurementPolicy   json.RawMessage `json:"procurement_policy"`
	CompatibilityPolicy json.RawMessage `json:"compatibility_policy"`
	Schedule            []scheduleRow   `json:"schedule"`
}

type vaccineMeta struct {
	Code               string `json:"code"`
	Name               string `json:"name"`
	Type               string `json:"type"`
	InventoryItemID    string `json:"inventory_item_id"`
	Manufacturer       string `json:"manufacturer"`
	Disease            string `json:"disease"`
	CompatibilityGroup string `json:"compatibility_group"`
	PathogenClass      string `json:"pathogen_class"`
	CourseType         string `json:"course_type"`
}

type scheduleRow struct {
	DoseCode          string          `json:"dose_code"`
	Sequence          int32           `json:"sequence"`
	TriggerType       string          `json:"trigger_type"`
	OffsetDays        int32           `json:"offset_days"`
	DueWindow         int32           `json:"due_window_days"`
	DoseAmount        float64         `json:"dose_amount"`
	DoseUnit          string          `json:"dose_unit"`
	VialDoses         int32           `json:"vial_doses"`
	RevaccinationDays int32           `json:"revaccination_interval_days"`
	RouteSite         string          `json:"route_site"`
	MaxDelayDays      int32           `json:"max_delay_days"`
	CourseLapsePolicy string          `json:"course_lapse_policy"`
	SOPVersion        string          `json:"sop_version"`
	SOPLabel          string          `json:"sop_label"`
	MinGapDays        int32           `json:"min_gap_days"`
	Repeat            string          `json:"repeat"`
	RepeatUntil       string          `json:"repeat_until_after_age"`
	CatchUp           string          `json:"catch_up"`
	ProofPolicy       json.RawMessage `json:"proof_policy"`
}

var (
	ruleDSLTopLevelKeys = map[string]bool{
		"category":             true,
		"scope":                true,
		"vaccine":              true,
		"eligibility":          true,
		"missed_dose_policy":   true,
		"stock_policy":         true,
		"schedule":             true,
		"escalation":           true,
		"compatibility_policy": true,
		"procurement_policy":   true,
		"pregnancy_policy":     true,
		"recovery_policy":      true,
		"parameter_template":   true,
		"ration":               true,
		"session_timing":       true,
		"inventory_policy":     true,
		"validation_policy":    true,
	}
	ruleDSLScopeKeys = map[string]bool{
		"type": true,
		"id":   true,
	}
	ruleDSLEligibilityKeys = map[string]bool{
		"animal_stage":                true,
		"animal_stage_source":         true,
		"stage":                       true,
		"species":                     true,
		"sex":                         true,
		"breed":                       true,
		"breed_class":                 true,
		"lifecycle":                   true,
		"health":                      true,
		"reproductive":                true,
		"exclude_reproductive_states": true,
		"defer_states":                true,
		"min_age_days":                true,
		"max_age_days":                true,
		"age_band":                    true,
		"mother_vaccinated_branch":    true,
	}
	ruleDSLVaccineKeys = map[string]bool{
		"code":                true,
		"name":                true,
		"type":                true,
		"inventory_item_id":   true,
		"manufacturer":        true,
		"disease":             true,
		"compatibility_group": true,
		"pathogen_class":      true,
		"course_type":         true,
	}
	ruleDSLCompatibilityPolicyKeys = map[string]bool{
		"live_to_killed_gap_days":            true,
		"killed_to_killed_gap_days":          true,
		"live_to_live_gap_days":              true,
		"kid_booster_min_gap_days":           true,
		"bacterial_viral_same_day_allowed":   true,
		"live_killed_viral_same_day_allowed": true,
		"max_vaccines_per_combo_session":     true,
	}
	ruleDSLProcurementPolicyKeys = map[string]bool{
		"warmup_no_vaccination_days":            true,
		"kids_normal_schedule_until_weeks":      true,
		"adult_prior_vaccination_allowed":       true,
		"assume_mother_vaccinated_when_unknown": true,
		"first_wave":                            true,
		"second_wave_after_days":                true,
		"goat_second_wave":                      true,
		"sheep_second_wave":                     true,
	}
	ruleDSLPregnancyPolicyKeys = map[string]bool{
		"allow_until_pregnancy_month":  true,
		"skip_from_pregnancy_month":    true,
		"skip_through_pregnancy_month": true,
		"post_delivery_catch_up_days":  true,
	}
	ruleDSLRecoveryPolicyKeys = map[string]bool{
		"max_nearby_drive_align_days": true,
	}
	ruleDSLSourceKeys = map[string]bool{
		"source_system": true,
		"source_ref":    true,
		"imported_at":   true,
		"reviewed_by":   true,
		"review_status": true,
		"approved_by":   true,
		"approved_at":   true,
	}
	ruleDSLScheduleKeys = map[string]bool{
		"dose_code":                   true,
		"sequence":                    true,
		"trigger_type":                true,
		"offset_days":                 true,
		"due_window_days":             true,
		"dose_amount":                 true,
		"dose_unit":                   true,
		"vial_doses":                  true,
		"revaccination_interval_days": true,
		"schedule_note":               true,
		"route_site":                  true,
		"max_delay_days":              true,
		"course_lapse_policy":         true,
		"min_gap_days":                true,
		"repeat":                      true,
		"repeat_until_after_age":      true,
		"catch_up":                    true,
		"sop_version":                 true,
		"sop_label":                   true,
		"proof_policy":                true,
	}
	ruleDSLStockPolicyKeys = map[string]bool{
		"vaccine_lot_requirement": true,
		"item_id":                 true,
		"vaccine_item_id":         true,
		"pick":                    true,
		"reject_expired_lot":      true,
		"cold_chain_required":     true,
	}
	ruleDSLRationKeys = map[string]bool{
		"mode":               true,
		"feed_item":          true,
		"quantity":           true,
		"unit":               true,
		"quantity_semantics": true,
	}
	ruleDSLSessionTimingKeys = map[string]bool{
		"session_order":          true,
		"session_time":           true,
		"split_weight":           true,
		"packing_proof_policy":   true,
		"execution_proof_policy": true,
	}
	ruleDSLInventoryPolicyKeys = map[string]bool{
		"mode":    true,
		"reserve": true,
		"consume": true,
		"release": true,
	}
	ruleDSLParameterTemplateKeys = map[string]bool{
		"source_tables":      true,
		"parameter_families": true,
		"dimension_keys":     true,
		"ratio_policy":       true,
	}
	ruleDSLValidationPolicyKeys = map[string]bool{
		"checks":              true,
		"calculation_outputs": true,
		"fail_closed":         true,
		"preview_required":    true,
	}
)

// ValidateRuleDSL enforces the committed protocol-rule DSL schema shape. It rejects unknown keys
// at every stable authored layer so typo'd immutable config cannot be silently stored.
func ValidateRuleDSL(ruleDSL []byte) error {
	if len(ruleDSL) == 0 {
		return nil
	}
	root, err := decodeRuleDSLObject(ruleDSL, "rule_dsl", ruleDSLTopLevelKeys)
	if err != nil {
		return err
	}
	if raw, ok := root["scope"]; ok && len(raw) > 0 && string(raw) != "null" {
		if _, err := decodeRuleDSLObject(raw, "rule_dsl.scope", ruleDSLScopeKeys); err != nil {
			return err
		}
	}
	if raw, ok := root["eligibility"]; ok && len(raw) > 0 && string(raw) != "null" {
		if _, err := decodeRuleDSLObject(raw, "rule_dsl.eligibility", ruleDSLEligibilityKeys); err != nil {
			return err
		}
	}
	if raw, ok := root["vaccine"]; ok && len(raw) > 0 && string(raw) != "null" {
		if _, err := decodeRuleDSLObject(raw, "rule_dsl.vaccine", ruleDSLVaccineKeys); err != nil {
			return err
		}
	}
	if raw, ok := root["stock_policy"]; ok && len(raw) > 0 && string(raw) != "null" {
		if _, err := decodeRuleDSLObject(raw, "rule_dsl.stock_policy", ruleDSLStockPolicyKeys); err != nil {
			return err
		}
	}
	if raw, ok := root["compatibility_policy"]; ok && len(raw) > 0 && string(raw) != "null" {
		if _, err := decodeRuleDSLObject(raw, "rule_dsl.compatibility_policy", ruleDSLCompatibilityPolicyKeys); err != nil {
			return err
		}
	}
	if raw, ok := root["procurement_policy"]; ok && len(raw) > 0 && string(raw) != "null" {
		if _, err := decodeRuleDSLObject(raw, "rule_dsl.procurement_policy", ruleDSLProcurementPolicyKeys); err != nil {
			return err
		}
	}
	if raw, ok := root["pregnancy_policy"]; ok && len(raw) > 0 && string(raw) != "null" {
		if _, err := decodeRuleDSLObject(raw, "rule_dsl.pregnancy_policy", ruleDSLPregnancyPolicyKeys); err != nil {
			return err
		}
	}
	if raw, ok := root["recovery_policy"]; ok && len(raw) > 0 && string(raw) != "null" {
		if _, err := decodeRuleDSLObject(raw, "rule_dsl.recovery_policy", ruleDSLRecoveryPolicyKeys); err != nil {
			return err
		}
	}
	if raw, ok := root["parameter_template"]; ok && len(raw) > 0 && string(raw) != "null" {
		if _, err := decodeRuleDSLObject(raw, "rule_dsl.parameter_template", ruleDSLParameterTemplateKeys); err != nil {
			return err
		}
	}
	if raw, ok := root["ration"]; ok && len(raw) > 0 && string(raw) != "null" {
		if _, err := decodeRuleDSLObject(raw, "rule_dsl.ration", ruleDSLRationKeys); err != nil {
			return err
		}
	}
	if raw, ok := root["inventory_policy"]; ok && len(raw) > 0 && string(raw) != "null" {
		if _, err := decodeRuleDSLObject(raw, "rule_dsl.inventory_policy", ruleDSLInventoryPolicyKeys); err != nil {
			return err
		}
	}
	if raw, ok := root["validation_policy"]; ok && len(raw) > 0 && string(raw) != "null" {
		if _, err := decodeRuleDSLObject(raw, "rule_dsl.validation_policy", ruleDSLValidationPolicyKeys); err != nil {
			return err
		}
	}
	if raw, ok := root["schedule"]; ok && len(raw) > 0 && string(raw) != "null" {
		if err := validateRuleDSLArray(raw, "rule_dsl.schedule", ruleDSLScheduleKeys); err != nil {
			return err
		}
	}
	if raw, ok := root["session_timing"]; ok && len(raw) > 0 && string(raw) != "null" {
		if err := validateRuleDSLArray(raw, "rule_dsl.session_timing", ruleDSLSessionTimingKeys); err != nil {
			return err
		}
	}
	return nil
}

func decodeRuleDSLObject(raw []byte, path string, allowed map[string]bool) (map[string]json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("%w: %s must be a JSON object: %v", ErrInvalidRuleDSL, path, err)
	}
	if obj == nil {
		return nil, fmt.Errorf("%w: %s must be a JSON object", ErrInvalidRuleDSL, path)
	}
	for key := range obj {
		if !allowed[key] {
			return nil, fmt.Errorf("%w: unknown key %s.%s", ErrInvalidRuleDSL, path, key)
		}
	}
	return obj, nil
}

func validateRuleDSLArray(raw []byte, path string, allowed map[string]bool) error {
	var rows []json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil {
		return fmt.Errorf("%w: %s must be an array: %v", ErrInvalidRuleDSL, path, err)
	}
	for idx, row := range rows {
		if _, err := decodeRuleDSLObject(row, fmt.Sprintf("%s[%d]", path, idx), allowed); err != nil {
			return err
		}
	}
	return nil
}

// ValidatePublishable is retained for old callers/tests that used this helper as a publish precheck.
// The former source/review gate is retired for Protocol Engine Phase 0: publishability is now driven
// by RBAC at the API boundary plus ValidateRuleDSL and ValidateExecutionContract. This helper only
// reports malformed JSON as not-publishable; empty or source-less DSL is allowed to proceed to the
// executable-contract checks.
func ValidatePublishable(ruleDSL []byte) error {
	if strings.TrimSpace(string(ruleDSL)) == "" {
		return nil
	}
	var env any
	if err := json.Unmarshal(ruleDSL, &env); err != nil {
		return fmt.Errorf("%w: invalid rule_dsl: %v", ErrNotPublishable, err)
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
	if isVaccinationVersion(v, env) {
		if err := validateVaccinationMatrix(env); err != nil {
			return err
		}
	}
	for idx, row := range env.Schedule {
		triggerType := strings.TrimSpace(row.TriggerType)
		if triggerType == "" {
			return fmt.Errorf("%w: schedule[%d] missing trigger_type", ErrNotPublishable, idx)
		}
		if !executableTriggerTypes[triggerType] {
			return fmt.Errorf("%w: schedule[%d] unsupported trigger_type %q", ErrNotPublishable, idx, triggerType)
		}
		if _, err := normalizeRepeatPolicy(row.Repeat, row.MinGapDays); err != nil {
			return fmt.Errorf("%w: schedule[%d] %w", ErrNotPublishable, idx, err)
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

func isVaccinationVersion(v domain.Version, env ruleDSLEnvelope) bool {
	return strings.TrimSpace(v.Category) == "vaccination" || strings.TrimSpace(env.Category) == "vaccination"
}

func validateVaccinationMatrix(env ruleDSLEnvelope) error {
	if strings.TrimSpace(env.Vaccine.Code) == "" {
		return fmt.Errorf("%w: vaccine.code required for vaccination matrix", ErrNotPublishable)
	}
	if strings.TrimSpace(env.Vaccine.Name) == "" {
		return fmt.Errorf("%w: vaccine.name required for vaccination matrix", ErrNotPublishable)
	}
	if strings.TrimSpace(env.Vaccine.Type) == "" {
		return fmt.Errorf("%w: vaccine.type required for vaccination matrix", ErrNotPublishable)
	}
	if len(env.Schedule) == 0 {
		return fmt.Errorf("%w: vaccination matrix requires at least one schedule row", ErrNotPublishable)
	}
	if len(env.Eligibility) == 0 || strings.TrimSpace(string(env.Eligibility)) == "" || strings.TrimSpace(string(env.Eligibility)) == "null" {
		return fmt.Errorf("%w: vaccination matrix eligibility required", ErrNotPublishable)
	}
	eligibility, err := decodeRuleDSLObject(env.Eligibility, "rule_dsl.eligibility", ruleDSLEligibilityKeys)
	if err != nil {
		return err
	}
	if !hasAnyNonBlank(eligibility, "animal_stage", "stage", "age_band") && !hasAnyNumber(eligibility, "min_age_days", "max_age_days") {
		return fmt.Errorf("%w: vaccination matrix eligibility must include age/stage targeting", ErrNotPublishable)
	}
	for _, key := range []string{"sex", "breed", "lifecycle", "health", "reproductive"} {
		if !hasAnyNonBlank(eligibility, key) {
			return fmt.Errorf("%w: vaccination matrix eligibility.%s required", ErrNotPublishable, key)
		}
	}
	if _, ok := eligibility["defer_states"]; !ok {
		return fmt.Errorf("%w: vaccination matrix eligibility.defer_states required", ErrNotPublishable)
	}
	if _, ok := eligibility["exclude_reproductive_states"]; !ok {
		return fmt.Errorf("%w: vaccination matrix eligibility.exclude_reproductive_states required", ErrNotPublishable)
	}
	for idx, row := range env.Schedule {
		if strings.TrimSpace(row.DoseCode) == "" {
			return fmt.Errorf("%w: schedule[%d] dose_code required for vaccination matrix", ErrNotPublishable, idx)
		}
		if row.DoseAmount <= 0 {
			return fmt.Errorf("%w: schedule[%d] dose_amount required for vaccination matrix", ErrNotPublishable, idx)
		}
		if strings.TrimSpace(row.DoseUnit) == "" {
			return fmt.Errorf("%w: schedule[%d] dose_unit required for vaccination matrix", ErrNotPublishable, idx)
		}
		if strings.TrimSpace(row.RouteSite) == "" {
			return fmt.Errorf("%w: schedule[%d] route_site required for vaccination matrix", ErrNotPublishable, idx)
		}
		if row.MaxDelayDays <= 0 {
			return fmt.Errorf("%w: schedule[%d] max_delay_days required for vaccination matrix", ErrNotPublishable, idx)
		}
		if row.MaxDelayDays < row.DueWindow {
			return fmt.Errorf("%w: schedule[%d] max_delay_days must cover due_window_days", ErrNotPublishable, idx)
		}
		if strings.TrimSpace(row.CourseLapsePolicy) == "" {
			return fmt.Errorf("%w: schedule[%d] course_lapse_policy required for vaccination matrix", ErrNotPublishable, idx)
		}
	}
	if err := validateVaccinationComboLimits(env); err != nil {
		return err
	}
	return nil
}

const maxVaccinesPerComboSession = 2

func validateVaccinationComboLimits(env ruleDSLEnvelope) error {
	if len(env.CompatibilityPolicy) > 0 && string(env.CompatibilityPolicy) != "null" {
		compat, err := decodeRuleDSLObject(env.CompatibilityPolicy, "rule_dsl.compatibility_policy", ruleDSLCompatibilityPolicyKeys)
		if err != nil {
			return err
		}
		if raw, ok := compat["max_vaccines_per_combo_session"]; ok {
			var max int32
			if err := json.Unmarshal(raw, &max); err != nil {
				return fmt.Errorf("%w: compatibility_policy.max_vaccines_per_combo_session must be a number", ErrNotPublishable)
			}
			if max > maxVaccinesPerComboSession {
				return fmt.Errorf("%w: compatibility_policy.max_vaccines_per_combo_session cannot exceed %d", ErrNotPublishable, maxVaccinesPerComboSession)
			}
		}
	}
	if len(env.ProcurementPolicy) == 0 || string(env.ProcurementPolicy) == "null" {
		return nil
	}
	proc, err := decodeRuleDSLObject(env.ProcurementPolicy, "rule_dsl.procurement_policy", ruleDSLProcurementPolicyKeys)
	if err != nil {
		return err
	}
	for _, key := range []string{"first_wave", "goat_second_wave", "sheep_second_wave"} {
		raw, ok := proc[key]
		if !ok {
			continue
		}
		var wave []string
		if err := json.Unmarshal(raw, &wave); err != nil {
			return fmt.Errorf("%w: procurement_policy.%s must be a string array", ErrNotPublishable, key)
		}
		if countNonBlankStrings(wave) > maxVaccinesPerComboSession {
			return fmt.Errorf("%w: procurement_policy.%s allows at most %d vaccines per combo visit", ErrNotPublishable, key, maxVaccinesPerComboSession)
		}
	}
	return nil
}

func countNonBlankStrings(values []string) int {
	n := 0
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			n++
		}
	}
	return n
}

func hasAnyNonBlank(obj map[string]json.RawMessage, keys ...string) bool {
	for _, key := range keys {
		raw, ok := obj[key]
		if !ok {
			continue
		}
		var s string
		if json.Unmarshal(raw, &s) == nil && strings.TrimSpace(s) != "" {
			return true
		}
		var arr []string
		if json.Unmarshal(raw, &arr) == nil {
			for _, v := range arr {
				if strings.TrimSpace(v) != "" {
					return true
				}
			}
		}
	}
	return false
}

func hasAnyNumber(obj map[string]json.RawMessage, keys ...string) bool {
	for _, key := range keys {
		raw, ok := obj[key]
		if !ok {
			continue
		}
		var n float64
		if json.Unmarshal(raw, &n) == nil {
			return true
		}
	}
	return false
}

func normalizeRepeatPolicy(value string, minGapDays int32) (string, error) {
	repeat := strings.TrimSpace(value)
	if repeat == "" {
		return "none", nil
	}
	switch repeat {
	case "none":
		return repeat, nil
	case "yearly":
		return repeat, nil
	case "every_n_days":
		if minGapDays <= 0 {
			return "", fmt.Errorf("%w: every_n_days requires min_gap_days", ErrUnsupportedRepeatPolicy)
		}
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

// PublishVersion publishes a draft version after schema and executable-contract checks pass. The DB
// also enforces the published-window EXCLUDE non-overlap; category capability
// (CEO/COO protocol.publish.*) is enforced at the API/RBAC boundary.
func (s *Service) PublishVersion(ctx context.Context, tenantID, versionID string, publishedBy *string, idempotencyKey ...string) error {
	v, err := s.repo.GetVersion(ctx, tenantID, versionID)
	if err != nil {
		return err
	}
	if v.Status != "draft" && v.Status != "published" {
		return fmt.Errorf("%w: status=%q", ports.ErrVersionNotDraft, v.Status)
	}
	if err := ValidateRuleDSL(v.RuleDsl); err != nil {
		return err
	}
	if v.Status == "draft" {
		if err := ValidateExecutionContract(v); err != nil {
			return err
		}
		if err := s.ensureExecutableRuleRows(ctx, tenantID, v, publishedBy); err != nil {
			return err
		}
	}
	if err := s.repo.PublishVersion(ctx, tenantID, versionID, publishedBy, idempotencyKey...); err != nil {
		return err
	}
	return nil
}

func (s *Service) ensureExecutableRuleRows(ctx context.Context, tenantID string, v domain.Version, createdBy *string) error {
	rules, err := s.repo.ListRules(ctx, tenantID, v.ProtocolVersionID)
	if err != nil {
		return err
	}
	var env ruleDSLEnvelope
	if len(v.RuleDsl) > 0 {
		if err := json.Unmarshal(v.RuleDsl, &env); err != nil {
			return fmt.Errorf("%w: invalid rule_dsl: %v", ErrNotPublishable, err)
		}
	}
	existing := make(map[string]bool, len(rules))
	for _, rule := range rules {
		if doseCode := strings.TrimSpace(rule.DoseCode); doseCode != "" {
			existing[doseCode] = true
		}
	}
	created := 0
	for idx, row := range env.Schedule {
		in, err := scheduleRowRule(tenantID, v, env, row, idx, createdBy)
		if err != nil {
			return err
		}
		if existing[in.DoseCode] {
			continue
		}
		if _, err := s.repo.CreateRule(ctx, in); err != nil {
			return err
		}
		existing[in.DoseCode] = true
		created++
	}
	if len(rules)+created == 0 {
		return fmt.Errorf("%w: publish requires at least one executable protocol_rules row", ErrNotPublishable)
	}
	return nil
}

func scheduleRowRule(tenantID string, v domain.Version, env ruleDSLEnvelope, row scheduleRow, idx int, createdBy *string) (domain.NewRule, error) {
	doseCode := strings.TrimSpace(row.DoseCode)
	if doseCode == "" {
		return domain.NewRule{}, fmt.Errorf("%w: schedule[%d] missing dose_code", ErrNotPublishable, idx)
	}
	triggerType := strings.TrimSpace(row.TriggerType)
	if triggerType == "" {
		return domain.NewRule{}, fmt.Errorf("%w: schedule[%d] missing trigger_type", ErrNotPublishable, idx)
	}
	repeat, err := normalizeRepeatPolicy(row.Repeat, row.MinGapDays)
	if err != nil {
		return domain.NewRule{}, fmt.Errorf("%w: schedule[%d] %w", ErrNotPublishable, idx, err)
	}
	catchUp := strings.TrimSpace(row.CatchUp)
	if catchUp == "" {
		catchUp = strings.TrimSpace(env.MissedDosePolicy)
	}
	if catchUp == "" {
		catchUp = "immediate"
	}
	sequence := row.Sequence
	if sequence <= 0 {
		sequence = int32(idx + 1)
	}
	proof := row.ProofPolicy
	if len(proof) == 0 {
		proof = v.ProofPolicy
	}
	var sopVersion *string
	if rowSOP := strings.TrimSpace(row.SOPVersion); rowSOP != "" {
		sopVersion = &rowSOP
	}
	return domain.NewRule{
		TenantID:            tenantID,
		ProtocolVersionID:   v.ProtocolVersionID,
		DoseCode:            doseCode,
		Sequence:            sequence,
		TriggerType:         triggerType,
		OffsetDays:          row.OffsetDays,
		DueWindowDays:       row.DueWindow,
		MinGapDays:          row.MinGapDays,
		Repeat:              repeat,
		RepeatUntilAfterAge: strings.TrimSpace(row.RepeatUntil),
		CatchUp:             catchUp,
		EligibilityJSON:     ruleEligibilityJSON(env.Eligibility),
		SopVersionID:        sopVersion,
		ProofPolicy:         proof,
		SortOrder:           int32(idx + 1),
		CreatedBy:           createdBy,
		IdempotencyKey:      "protocol-schedule-rule:" + v.ProtocolVersionID + ":" + doseCode,
	}, nil
}

func ruleEligibilityJSON(raw json.RawMessage) []byte {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return []byte(`{}`)
	}
	return []byte(trimmed)
}
