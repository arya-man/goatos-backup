package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

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

const maxCompiledRuleDimensionsPerRule = 5000

type protocolRuleDimensionWriter interface {
	ReplaceProtocolRuleDimensions(ctx context.Context, tenantID, versionID string, dimensions []domain.RuleDimension) error
}

type protocolMatrixPublisher interface {
	PublishVersionWithDerivedRules(ctx context.Context, tenantID string, v domain.Version, rules []domain.NewRule, dimensions []domain.RuleDimension, publishedBy *string, capacity *domain.PublishedCapacity, seedOwnedGuardActor string, idempotencyKey ...string) error
}

// capacityAtomicPublisher publishes a non-matrix vaccination version and upserts + parity-checks its
// versioned capacity in the SAME transaction, so a capacity failure rolls the publish back. Repositories
// that do not implement it fall back to the (non-atomic) best-effort post-publish sync.
type capacityAtomicPublisher interface {
	PublishVersionWithCapacity(ctx context.Context, tenantID, versionID string, publishedBy *string, capacity domain.PublishedCapacity, idempotencyKey ...string) error
}

type protocolPublishedMatrixReplayer interface {
	PublishPublishedMatrixReplay(ctx context.Context, tenantID string, v domain.Version, publishedBy *string, idempotencyKey ...string) error
}

// matrixTargetOwnershipReader reads a version's drafted_by so a seed-owned publish can verify its
// target is seed-drafted BEFORE dispatching — closing the already-published replay path where the
// per-transaction retire guard never runs.
type matrixTargetOwnershipReader interface {
	VaccinationMatrixVersionDraftedBy(ctx context.Context, tenantID, versionID string) (string, error)
}

type ruleDSLEnvelope struct {
	Category            string          `json:"category"`
	RulesetFamily       string          `json:"ruleset_family"`
	Vaccine             vaccineMeta     `json:"vaccine"`
	Eligibility         json.RawMessage `json:"eligibility"`
	MissedDosePolicy    json.RawMessage `json:"missed_dose_policy"`
	ProcurementPolicy   json.RawMessage `json:"procurement_policy"`
	CompatibilityPolicy json.RawMessage `json:"compatibility_policy"`
	Capacity            json.RawMessage `json:"capacity"`
	Schedule            []scheduleRow   `json:"schedule"`
	MatrixRows          []matrixRow     `json:"matrix_rows"`
}

// capacityConfigSyncer is the optional repository capability that upserts the operational capacity
// read model (vaccination_capacity_config) from the published version, then returns the stored row for
// a post-publish parity check. The versioned rule_dsl.capacity is the authoring source of truth.
type capacityConfigSyncer interface {
	SyncVaccinationCapacityConfig(ctx context.Context, tenantID string, want domain.PublishedCapacity) (domain.PublishedCapacity, error)
}

const defaultMaxBufferDays = 7

// parseVersionedCapacity reads rule_dsl.capacity. ok is false when the block is absent. ABSENT numeric
// fields fall back to safe defaults (max_per_day 100, max_buffer_days 7 — the business default), but a
// field that is PRESENT and out of range (max_per_day < 1, max_buffer_days < 0) is rejected rather than
// silently rewritten to a default the admin never authored.
func parseVersionedCapacity(raw json.RawMessage) (domain.PublishedCapacity, bool, error) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" || strings.TrimSpace(string(raw)) == "null" {
		return domain.PublishedCapacity{}, false, nil
	}
	var body struct {
		MaxPerDay      *int   `json:"max_per_day"`
		MaxBufferDays  *int   `json:"max_buffer_days"`
		CapacityScope  string `json:"capacity_scope"`
		OverflowPolicy string `json:"overflow_policy"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return domain.PublishedCapacity{}, false, fmt.Errorf("%w: rule_dsl.capacity must be a JSON object: %v", ErrInvalidRuleDSL, err)
	}
	out := domain.PublishedCapacity{
		MaxPerDay:      100,
		MaxBufferDays:  defaultMaxBufferDays,
		CapacityScope:  strings.TrimSpace(body.CapacityScope),
		OverflowPolicy: strings.TrimSpace(body.OverflowPolicy),
	}
	if body.MaxPerDay != nil {
		if *body.MaxPerDay < 1 {
			return domain.PublishedCapacity{}, false, fmt.Errorf("%w: rule_dsl.capacity.max_per_day must be >= 1, got %d", ErrInvalidRuleDSL, *body.MaxPerDay)
		}
		out.MaxPerDay = *body.MaxPerDay
	}
	if body.MaxBufferDays != nil {
		if *body.MaxBufferDays < 0 {
			return domain.PublishedCapacity{}, false, fmt.Errorf("%w: rule_dsl.capacity.max_buffer_days must be >= 0, got %d", ErrInvalidRuleDSL, *body.MaxBufferDays)
		}
		out.MaxBufferDays = *body.MaxBufferDays
	}
	if out.CapacityScope == "" {
		out.CapacityScope = "tenant"
	}
	if out.OverflowPolicy == "" {
		out.OverflowPolicy = "split_within_safe_window_then_mark_needs_review"
	}
	return out, true, nil
}

// syncPublishedCapacityBestEffort is the replay-path variant: a retry of an already-published version
// must NOT re-validate/decode old (possibly invalid) rule_dsl, so an undecodable DSL skips the sync
// (capacity was synced at the first publish) rather than failing the idempotent replay.
func (s *Service) syncPublishedCapacityBestEffort(ctx context.Context, tenantID string, v domain.Version) error {
	env, err := decodeRuleDSLEnvelope(v.RuleDsl)
	if err != nil {
		return nil
	}
	return s.syncPublishedCapacity(ctx, tenantID, v, env)
}

// syncPublishedCapacity upserts the versioned capacity into vaccination_capacity_config after a
// vaccination version publishes, then verifies the stored row matches (preflight parity). vaccination_
// capacity_config is a derived read model here — publish is its only writer.
func (s *Service) syncPublishedCapacity(ctx context.Context, tenantID string, v domain.Version, env ruleDSLEnvelope) error {
	if !isVaccinationVersion(v, env) {
		return nil
	}
	want, ok, err := parseVersionedCapacity(env.Capacity)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	syncer, ok := s.repo.(capacityConfigSyncer)
	if !ok {
		return nil
	}
	got, err := syncer.SyncVaccinationCapacityConfig(ctx, tenantID, want)
	if err != nil {
		return fmt.Errorf("protocol: sync published capacity: %w", err)
	}
	if got != want {
		return fmt.Errorf("%w: capacity parity mismatch after publish (rule_dsl=%+v stored=%+v)", ErrNotPublishable, want, got)
	}
	return nil
}

// versionedCapacityForPublish returns the capacity to sync atomically at first publish, or nil when the
// version is not a vaccination version or carries no rule_dsl.capacity block. A present-but-invalid block
// returns an error so publish is blocked, never silently defaulted.
func versionedCapacityForPublish(v domain.Version, env ruleDSLEnvelope) (*domain.PublishedCapacity, error) {
	if !isVaccinationVersion(v, env) {
		return nil, nil
	}
	want, ok, err := parseVersionedCapacity(env.Capacity)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return &want, nil
}

// mapCapacityParityErr translates the repository's in-transaction parity sentinel into the app-level
// ErrNotPublishable the API and tests expect. Any other error (or nil) passes through unchanged.
func mapCapacityParityErr(err error) error {
	if errors.Is(err, ports.ErrCapacityParityMismatch) {
		return fmt.Errorf("%w: %v", ErrNotPublishable, err)
	}
	return err
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
	SourceDoseCode    string          `json:"source_dose_code"`
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

type matrixRow struct {
	RowID       string          `json:"row_id"`
	Vaccine     json.RawMessage `json:"vaccine"`
	Eligibility json.RawMessage `json:"eligibility"`
	Schedule    []scheduleRow   `json:"schedule"`
}

var (
	ruleDSLTopLevelKeys = map[string]bool{
		"category":             true,
		"ruleset_family":       true,
		"scope":                true,
		"vaccine":              true,
		"eligibility":          true,
		"missed_dose_policy":   true,
		"stock_policy":         true,
		"schedule":             true,
		"matrix_rows":          true,
		"escalation":           true,
		"compatibility_policy": true,
		"procurement_policy":   true,
		"pregnancy_policy":     true,
		"capacity":             true,
		"recovery_policy":      true,
		"drive_policy":         true,
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
		"warmup_no_vaccination_days":       true,
		"kids_normal_schedule_until_weeks": true,
		"adult_prior_vaccination_allowed":  true,
		"first_wave":                       true,
		"second_wave_after_days":           true,
		"goat_second_wave":                 true,
		"sheep_second_wave":                true,
	}
	ruleDSLPregnancyPolicyKeys = map[string]bool{
		"allow_until_pregnancy_month":  true,
		"skip_from_pregnancy_month":    true,
		"skip_through_pregnancy_month": true,
		"post_delivery_catch_up_days":  true,
	}
	// ruleDSLCapacityKeys is the versioned daily-vaccination-capacity block (rule_dsl.capacity). It is
	// authored + published with the rule and synced into vaccination_capacity_config on publish.
	ruleDSLCapacityKeys = map[string]bool{
		"max_per_day":     true,
		"max_buffer_days": true,
		"capacity_scope":  true,
		"overflow_policy": true,
	}
	ruleDSLRecoveryPolicyKeys = map[string]bool{
		"max_nearby_drive_align_days": true,
	}
	ruleDSLMissedDosePolicyKeys = map[string]bool{
		"nearby_drive_align_days":           true,
		"materialize_only_future_open_work": true,
	}
	ruleDSLDrivePolicyKeys = map[string]bool{
		"enabled":                        true,
		"max_goats_per_drive":            true,
		"priority":                       true,
		"combo_align_window_days":        true,
		"max_batching_hold_days":         true,
		"max_batching_hold_count":        true,
		"species_grouping_policy":        true,
		"max_shots_per_animal_per_drive": true,
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
		"source_dose_code":            true,
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
	if raw, ok := root["capacity"]; ok && len(raw) > 0 && string(raw) != "null" {
		if _, err := decodeRuleDSLObject(raw, "rule_dsl.capacity", ruleDSLCapacityKeys); err != nil {
			return err
		}
	}
	if raw, ok := root["recovery_policy"]; ok && len(raw) > 0 && string(raw) != "null" {
		if _, err := decodeRuleDSLObject(raw, "rule_dsl.recovery_policy", ruleDSLRecoveryPolicyKeys); err != nil {
			return err
		}
	}
	if raw, ok := root["missed_dose_policy"]; ok && len(raw) > 0 && string(raw) != "null" {
		if strings.HasPrefix(strings.TrimSpace(string(raw)), "{") {
			if _, err := decodeRuleDSLObject(raw, "rule_dsl.missed_dose_policy", ruleDSLMissedDosePolicyKeys); err != nil {
				return err
			}
		}
	}
	if raw, ok := root["drive_policy"]; ok && len(raw) > 0 && string(raw) != "null" {
		if _, err := decodeRuleDSLObject(raw, "rule_dsl.drive_policy", ruleDSLDrivePolicyKeys); err != nil {
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

// forbiddenPathogenClassValues are vaccine-TYPE (or course) values that must
// never appear in the pathogen_class field. pathogen_class describes the
// organism (bacterial/viral); live/killed/toxoid are immunological types and
// combo is a course/compatibility concept. A type value leaking into
// pathogen_class (BUG1) makes the compatibility engine select the wrong same-day
// / spacing rule, so publish must reject it rather than persist bad metadata.
var forbiddenPathogenClassValues = map[string]struct{}{
	"live":   {},
	"killed": {},
	"toxoid": {},
	"combo":  {},
}

// validVaccineTypes are the only accepted immunological types. Per the V1 Implementation Contract
// (docs/preventive-care-vaccination/vaccination-rules.md) and the Config UI's own vaccine_types
// option group (adminui service.pageOptionGroups), the reviewed taxonomy is live/killed/toxoid PLUS
// "combo" (a reviewed combination row) and "unknown_review_needed" (source explicitly lacks a type;
// fail-closed downstream). R2-07b: these were previously rejected here even though the Config UI
// offered them and the canonical rules require them, so a direct publish with a reviewed value was
// blocked and the UI presented un-publishable options. classifyVaccine() already maps combo/unknown
// to immunoUnknown, which crossVaccineGapDays fails closed on (strictest gap).
// Note: "matrix" is valid only for the wrapper vaccine (vaccination.matrix), not for individual vaccines.
var validVaccineTypes = map[string]struct{}{
	"live":                  {},
	"killed":                {},
	"toxoid":                {},
	"combo":                 {},
	"unknown_review_needed": {},
}

// validPathogenClasses are the only accepted organism classes: bacterial/viral PLUS "mixed"
// (combination vaccine or reviewed combo row) and "unknown_review_needed" (source lacks class; fail
// closed for same-day planning) -- matching the Config UI vaccine_pathogen_classes option group and
// the V1 contract (R2-07b). classifyVaccine treats mixed/unknown as a non-viral/non-bacterial base
// class, which the compatibility engine handles conservatively.
var validPathogenClasses = map[string]struct{}{
	"viral":                 {},
	"bacterial":             {},
	"mixed":                 {},
	"unknown_review_needed": {},
}

// validCourseTypes are the only accepted course types.
var validCourseTypes = map[string]struct{}{
	"single":  {},
	"booster": {},
}

// IsValidVaccineType / IsValidPathogenClass / IsValidCourseType expose the reviewed taxonomy so the
// adminui Config option groups (and their tests) validate against ONE backend source of truth
// instead of a second hardcoded list that can silently drift out of sync (R2-07b).
func IsValidVaccineType(v string) bool {
	_, ok := validVaccineTypes[strings.ToLower(strings.TrimSpace(v))]
	return ok
}

func IsValidPathogenClass(v string) bool {
	_, ok := validPathogenClasses[strings.ToLower(strings.TrimSpace(v))]
	return ok
}

func IsValidCourseType(v string) bool {
	_, ok := validCourseTypes[strings.ToLower(strings.TrimSpace(v))]
	return ok
}

func rejectVaccineTypeValueInPathogenClass(pathogenClass, label string) error {
	v := strings.ToLower(strings.TrimSpace(pathogenClass))
	if v == "" {
		return nil
	}
	if _, bad := forbiddenPathogenClassValues[v]; bad {
		return fmt.Errorf("%w: %s %q is a vaccine-type value; pathogen class must describe the organism (bacterial/viral), not the vaccine type", ErrNotPublishable, label, pathogenClass)
	}
	return nil
}

// validateVaccineType ensures vaccine type is valid (live, killed, toxoid, or matrix for wrapper).
// allowMatrix should be true only for the top-level vaccine (wrapper), not for individual vaccines.
func validateVaccineType(vaccineType, label string, allowMatrix bool) error {
	v := strings.ToLower(strings.TrimSpace(vaccineType))
	if v == "" {
		return fmt.Errorf("%w: %s vaccine type required", ErrNotPublishable, label)
	}
	if v == "matrix" && allowMatrix {
		return nil
	}
	if _, ok := validVaccineTypes[v]; !ok {
		return fmt.Errorf("%w: %s vaccine type %q is invalid; must be live, killed, or toxoid", ErrNotPublishable, label, vaccineType)
	}
	return nil
}

// validatePathogenClass ensures pathogen class is valid (viral or bacterial).
func validatePathogenClass(pathogenClass, label string) error {
	v := strings.ToLower(strings.TrimSpace(pathogenClass))
	if v == "" {
		return fmt.Errorf("%w: %s pathogen class required", ErrNotPublishable, label)
	}
	if _, ok := validPathogenClasses[v]; !ok {
		return fmt.Errorf("%w: %s pathogen class %q is invalid; must be viral or bacterial", ErrNotPublishable, label, pathogenClass)
	}
	return nil
}

// validateCourseType ensures course type is valid (single or booster).
func validateCourseType(courseType, label string) error {
	v := strings.ToLower(strings.TrimSpace(courseType))
	if v == "" {
		return fmt.Errorf("%w: %s course type required", ErrNotPublishable, label)
	}
	if _, ok := validCourseTypes[v]; !ok {
		return fmt.Errorf("%w: %s course type %q is invalid; must be single or booster", ErrNotPublishable, label, courseType)
	}
	return nil
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
	// For the wrapper vaccine, "matrix" is allowed; for individual vaccines it is not.
	// isVaccinationMatrixRuleset() will tell us later if this is the matrix wrapper.
	// For now, validate that if it's not "matrix", it must be one of the valid immunological types.
	if err := validateVaccineType(env.Vaccine.Type, "rule_dsl.vaccine", strings.ToLower(strings.TrimSpace(env.Vaccine.Type)) == "matrix" || env.Vaccine.Code == "vaccination.matrix"); err != nil {
		return err
	}
	if err := rejectVaccineTypeValueInPathogenClass(env.Vaccine.PathogenClass, "rule_dsl.vaccine.pathogen_class"); err != nil {
		return err
	}
	// The wrapper vaccine (type=matrix, code=vaccination.matrix) is exempt from pathogen_class and course_type.
	// Individual vaccines in matrix_rows MUST carry these classifications (validated separately below).
	// Validate pathogen_class only if present (wrapper may omit it)
	if strings.TrimSpace(env.Vaccine.PathogenClass) != "" {
		if err := validatePathogenClass(env.Vaccine.PathogenClass, "rule_dsl.vaccine"); err != nil {
			return err
		}
	}
	// Validate course_type only if present (wrapper may omit it)
	if strings.TrimSpace(env.Vaccine.CourseType) != "" {
		if err := validateCourseType(env.Vaccine.CourseType, "rule_dsl.vaccine"); err != nil {
			return err
		}
	}
	if len(env.Schedule) == 0 {
		return fmt.Errorf("%w: vaccination matrix requires at least one schedule row", ErrNotPublishable)
	}
	if isVaccinationMatrixRuleset(env) {
		if len(env.MatrixRows) == 0 {
			return fmt.Errorf("%w: vaccination matrix requires matrix_rows metadata", ErrNotPublishable)
		}
		for idx, row := range env.Schedule {
			if _, _, ok := findMatrixRowForDose(env.MatrixRows, row); !ok {
				return fmt.Errorf("%w: schedule[%d] missing matrix_rows metadata", ErrNotPublishable, idx)
			}
		}
		if err := validateMatrixScheduleCompleteness(env); err != nil {
			return err
		}
	}
	if len(env.Eligibility) == 0 || strings.TrimSpace(string(env.Eligibility)) == "" || strings.TrimSpace(string(env.Eligibility)) == "null" {
		return fmt.Errorf("%w: vaccination matrix eligibility required", ErrNotPublishable)
	}
	eligibility, err := decodeRuleDSLObject(env.Eligibility, "rule_dsl.eligibility", ruleDSLEligibilityKeys)
	if err != nil {
		return err
	}
	if err := rejectUnknownSexSelector(eligibility, "rule_dsl.eligibility.sex"); err != nil {
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
	if err := rejectPartialClinicalDeferStates(eligibility, "rule_dsl.eligibility.defer_states"); err != nil {
		return err
	}
	if _, ok := eligibility["exclude_reproductive_states"]; !ok {
		return fmt.Errorf("%w: vaccination matrix eligibility.exclude_reproductive_states required", ErrNotPublishable)
	}
	for idx, row := range env.MatrixRows {
		rowEligibility, err := decodeRuleDSLObject(row.Eligibility, fmt.Sprintf("matrix_rows[%d].eligibility", idx), ruleDSLEligibilityKeys)
		if err != nil {
			return err
		}
		if err := rejectUnknownSexSelector(rowEligibility, fmt.Sprintf("matrix_rows[%d].eligibility.sex", idx)); err != nil {
			return err
		}
		if err := rejectPartialClinicalDeferStates(rowEligibility, fmt.Sprintf("matrix_rows[%d].eligibility.defer_states", idx)); err != nil {
			return err
		}
		// R2-07a: a matrix row's vaccine object is REQUIRED. The prior code validated the vaccine
		// fields only inside `if len(row.Vaccine) > 0`, so removing the WHOLE vaccine object (not just
		// its type) bypassed type/pathogen/course validation entirely and still published. Require a
		// non-empty object before decoding so every matrix row must carry a fully-classified vaccine.
		if len(row.Vaccine) == 0 {
			return fmt.Errorf("%w: matrix_rows[%d].vaccine required", ErrNotPublishable, idx)
		}
		var rowVaccine struct {
			Type          string `json:"type"`
			PathogenClass string `json:"pathogen_class"`
			CourseType    string `json:"course_type"`
		}
		if err := json.Unmarshal(row.Vaccine, &rowVaccine); err != nil {
			return fmt.Errorf("%w: matrix_rows[%d].vaccine is not a valid object", ErrNotPublishable, idx)
		}
		if err := rejectVaccineTypeValueInPathogenClass(rowVaccine.PathogenClass, fmt.Sprintf("matrix_rows[%d].vaccine.pathogen_class", idx)); err != nil {
			return err
		}
		// Validate vaccine type. REQUIRED for every matrix-row vaccine (R2-07a): validateVaccineType
		// itself rejects an empty type. Must be live, killed, or toxoid; never "matrix" for an
		// individual vaccine.
		if err := validateVaccineType(rowVaccine.Type, fmt.Sprintf("matrix_rows[%d].vaccine", idx), false); err != nil {
			return err
		}
		// Validate pathogen class (REQUIRED for individual vaccines)
		if err := validatePathogenClass(rowVaccine.PathogenClass, fmt.Sprintf("matrix_rows[%d].vaccine", idx)); err != nil {
			return err
		}
		// Validate course type (REQUIRED for individual vaccines)
		if err := validateCourseType(rowVaccine.CourseType, fmt.Sprintf("matrix_rows[%d].vaccine", idx)); err != nil {
			return err
		}
	}
	for idx, row := range env.Schedule {
		if strings.TrimSpace(row.DoseCode) == "" {
			return fmt.Errorf("%w: schedule[%d] dose_code required for vaccination matrix", ErrNotPublishable, idx)
		}
		if err := validateScheduleTimingNumbers(row, fmt.Sprintf("schedule[%d]", idx)); err != nil {
			return err
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

func validateMatrixScheduleCompleteness(env ruleDSLEnvelope) error {
	topSchedule := map[string]int{}
	topSourceAliases := map[string]int{}
	for idx, row := range env.Schedule {
		doseCode := strings.TrimSpace(row.DoseCode)
		if doseCode == "" {
			continue
		}
		if prev, ok := topSchedule[doseCode]; ok {
			return fmt.Errorf("%w: schedule[%d] duplicates dose_code %q from schedule[%d]", ErrNotPublishable, idx, doseCode, prev)
		}
		topSchedule[doseCode] = idx
		alias := normalizedIdentityKey(row.SourceDoseCode)
		if alias == "" {
			alias = normalizedIdentityKey(doseCode)
		}
		if prev, ok := topSourceAliases[alias]; ok {
			return fmt.Errorf("%w: schedule[%d] duplicates source_dose_code %q from schedule[%d]", ErrNotPublishable, idx, alias, prev)
		}
		topSourceAliases[alias] = idx
	}
	matrixSchedule := map[string]string{}
	matrixRowIDs := map[string]int{}
	matrixSourceAliases := map[string]string{}
	for rowIdx, row := range env.MatrixRows {
		rowID := strings.TrimSpace(row.RowID)
		if rowID == "" {
			return fmt.Errorf("%w: matrix_rows[%d].row_id required", ErrNotPublishable, rowIdx)
		}
		rowKey := normalizedIdentityKey(rowID)
		if prev, ok := matrixRowIDs[rowKey]; ok {
			return fmt.Errorf("%w: matrix_rows[%d].row_id duplicates matrix_rows[%d]", ErrNotPublishable, rowIdx, prev)
		}
		matrixRowIDs[rowKey] = rowIdx
		for cellIdx, cell := range row.Schedule {
			doseCode := strings.TrimSpace(cell.DoseCode)
			if doseCode == "" {
				return fmt.Errorf("%w: matrix_rows[%d].schedule[%d].dose_code required", ErrNotPublishable, rowIdx, cellIdx)
			}
			if err := validateScheduleTimingNumbers(cell, fmt.Sprintf("matrix_rows[%d].schedule[%d]", rowIdx, cellIdx)); err != nil {
				return err
			}
			if owner, ok := matrixSchedule[doseCode]; ok {
				return fmt.Errorf("%w: matrix dose_code %q appears in both %s and matrix_rows[%d].schedule[%d]", ErrNotPublishable, doseCode, owner, rowIdx, cellIdx)
			}
			matrixSchedule[doseCode] = fmt.Sprintf("matrix_rows[%d].schedule[%d]", rowIdx, cellIdx)
			alias := normalizedIdentityKey(cell.SourceDoseCode)
			if alias == "" {
				alias = normalizedIdentityKey(doseCode)
			}
			if owner, ok := matrixSourceAliases[alias]; ok {
				return fmt.Errorf("%w: matrix source_dose_code %q appears in both %s and matrix_rows[%d].schedule[%d]", ErrNotPublishable, alias, owner, rowIdx, cellIdx)
			}
			matrixSourceAliases[alias] = matrixSchedule[doseCode]
			if _, ok := topSchedule[doseCode]; !ok {
				return fmt.Errorf("%w: %s dose_code %q is missing from top-level schedule[]", ErrNotPublishable, matrixSchedule[doseCode], doseCode)
			}
		}
	}
	if len(matrixSchedule) == 0 {
		return fmt.Errorf("%w: vaccination matrix requires at least one matrix row schedule cell", ErrNotPublishable)
	}
	for doseCode, idx := range topSchedule {
		if _, ok := matrixSchedule[doseCode]; !ok {
			return fmt.Errorf("%w: schedule[%d] dose_code %q is not present in matrix_rows[].schedule[]", ErrNotPublishable, idx, doseCode)
		}
	}
	return nil
}

func validateScheduleTimingNumbers(row scheduleRow, label string) error {
	checks := []struct {
		name  string
		value int32
	}{
		{"sequence", row.Sequence},
		{"offset_days", row.OffsetDays},
		{"due_window_days", row.DueWindow},
		{"min_gap_days", row.MinGapDays},
		{"vial_doses", row.VialDoses},
		{"revaccination_interval_days", row.RevaccinationDays},
		{"max_delay_days", row.MaxDelayDays},
	}
	for _, check := range checks {
		if check.value < 0 {
			return fmt.Errorf("%w: %s.%s cannot be negative", ErrNotPublishable, label, check.name)
		}
	}
	return nil
}

func normalizedIdentityKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
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

func rejectUnknownSexSelector(obj map[string]json.RawMessage, label string) error {
	values, err := selectorValues(obj, []string{"sex"}, "", "sex")
	if err != nil {
		return fmt.Errorf("%w: %s %v", ErrNotPublishable, label, err)
	}
	for _, value := range values {
		if value == "unknown" {
			return fmt.Errorf("%w: %s cannot be unknown", ErrNotPublishable, label)
		}
	}
	return nil
}

func rejectUnknownSexInEligibilityJSON(raw []byte) error {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("%w: eligibility_json must be valid JSON: %v", ErrNotPublishable, err)
	}
	if hasUnknownSexSelector(value) {
		return fmt.Errorf("%w: eligibility_json cannot contain sex unknown", ErrNotPublishable)
	}
	return nil
}

func hasUnknownSexSelector(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "sex" && selectorValueHasUnknownSex(child) {
				return true
			}
			if hasUnknownSexSelector(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if hasUnknownSexSelector(child) {
				return true
			}
		}
	}
	return false
}

func selectorValueHasUnknownSex(value any) bool {
	switch typed := value.(type) {
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "unknown")
	case []any:
		for _, child := range typed {
			if selectorValueHasUnknownSex(child) {
				return true
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
	return s.publishVersion(ctx, tenantID, versionID, publishedBy, "", idempotencyKey...)
}

// PublishSeedOwnedVaccinationMatrixVersion publishes a vaccination matrix on behalf of the source
// seed, guarded so its overlap-retire may clear ONLY seed-owned versions (drafted_by = seedGuardActor).
// If an overlapping published matrix authored by anyone else exists (including one authored via Config
// under the same canonical vaccination.matrix protocol, or one published concurrently), the publish
// fails closed with ErrVaccinationMatrixOwnershipConflict instead of retiring user configuration.
func (s *Service) PublishSeedOwnedVaccinationMatrixVersion(ctx context.Context, tenantID, versionID, seedGuardActor string, idempotencyKey ...string) error {
	if strings.TrimSpace(seedGuardActor) == "" {
		return fmt.Errorf("protocol: seed-owned matrix publish requires a seed guard actor id")
	}
	return s.publishVersion(ctx, tenantID, versionID, nil, seedGuardActor, idempotencyKey...)
}

func (s *Service) publishVersion(ctx context.Context, tenantID, versionID string, publishedBy *string, seedGuardActor string, idempotencyKey ...string) error {
	v, err := s.repo.GetVersion(ctx, tenantID, versionID)
	if err != nil {
		return err
	}
	if v.Status != "draft" && v.Status != "published" {
		return fmt.Errorf("%w: status=%q", ports.ErrVersionNotDraft, v.Status)
	}
	// VAX-SEED-R2: a seed-owned publish must only ever target a version the seed itself drafted. Verify
	// this BEFORE any dispatch so the already-published replay path (which never reaches the
	// per-transaction retire guard) cannot accept a user-drafted published target as a "successful seed
	// replay". The target-ownership authority for the draft path stays the atomic under-lock check in
	// PublishVersionWithDerivedRules; this early check additionally covers the published/replay path,
	// where the target is immutable so a pre-dispatch read is sufficient.
	if seedGuardActor != "" {
		reader, ok := s.repo.(matrixTargetOwnershipReader)
		if !ok {
			return fmt.Errorf("protocol: repository cannot verify seed matrix target ownership")
		}
		draftedBy, err := reader.VaccinationMatrixVersionDraftedBy(ctx, tenantID, versionID)
		if err != nil {
			return err
		}
		if draftedBy != seedGuardActor {
			return fmt.Errorf("%w: target version %s is drafted_by %q, not the seed actor", ports.ErrVaccinationMatrixOwnershipConflict, versionID, draftedBy)
		}
	}
	if v.Status == "published" {
		if publishedVersionLooksLikeVaccinationMatrix(v) {
			if replayer, ok := s.repo.(protocolPublishedMatrixReplayer); ok {
				if err := replayer.PublishPublishedMatrixReplay(ctx, tenantID, v, publishedBy, idempotencyKey...); err != nil {
					return err
				}
				return s.syncPublishedCapacityBestEffort(ctx, tenantID, v)
			}
		}
		if err := s.repo.PublishVersion(ctx, tenantID, versionID, publishedBy, idempotencyKey...); err != nil {
			return err
		}
		return s.syncPublishedCapacityBestEffort(ctx, tenantID, v)
	}
	if err := ValidateRuleDSL(v.RuleDsl); err != nil {
		return err
	}
	env, err := decodeRuleDSLEnvelope(v.RuleDsl)
	if err != nil {
		return err
	}
	if isVaccinationMatrixRuleset(env) {
		if err := ValidateExecutionContract(v); err != nil {
			return err
		}
		publisher, ok := s.repo.(protocolMatrixPublisher)
		if !ok {
			return fmt.Errorf("%w: repository cannot atomically publish vaccination matrix", ErrNotPublishable)
		}
		rules, dimensions, err := buildVaccinationMatrixDerivedRows(tenantID, v, env, publishedBy)
		if err != nil {
			return err
		}
		capacity, err := versionedCapacityForPublish(v, env)
		if err != nil {
			return err
		}
		if err := publisher.PublishVersionWithDerivedRules(ctx, tenantID, v, rules, dimensions, publishedBy, capacity, seedGuardActor, idempotencyKey...); err != nil {
			return mapCapacityParityErr(err)
		}
		return nil
	}
	if v.Status == "draft" {
		if err := ValidateExecutionContract(v); err != nil {
			return err
		}
		if err := s.ensureExecutableRuleRows(ctx, tenantID, v, publishedBy); err != nil {
			return err
		}
		if err := s.compileProtocolRuleDimensions(ctx, tenantID, v); err != nil {
			return err
		}
	}
	capacity, err := versionedCapacityForPublish(v, env)
	if err != nil {
		return err
	}
	if capacity != nil {
		if atomic, ok := s.repo.(capacityAtomicPublisher); ok {
			return mapCapacityParityErr(atomic.PublishVersionWithCapacity(ctx, tenantID, versionID, publishedBy, *capacity, idempotencyKey...))
		}
	}
	if err := s.repo.PublishVersion(ctx, tenantID, versionID, publishedBy, idempotencyKey...); err != nil {
		return err
	}
	if capacity != nil {
		// Fallback for a repository without atomic capacity support: best-effort post-publish sync.
		return s.syncPublishedCapacity(ctx, tenantID, v, env)
	}
	return nil
}

func decodeRuleDSLEnvelope(raw []byte) (ruleDSLEnvelope, error) {
	var env ruleDSLEnvelope
	if len(raw) == 0 {
		return env, nil
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return env, fmt.Errorf("%w: invalid rule_dsl: %v", ErrNotPublishable, err)
	}
	return env, nil
}

func buildVaccinationMatrixDerivedRows(tenantID string, v domain.Version, env ruleDSLEnvelope, createdBy *string) ([]domain.NewRule, []domain.RuleDimension, error) {
	rules := make([]domain.NewRule, 0, len(env.Schedule))
	stored := make([]domain.Rule, 0, len(env.Schedule))
	for idx, row := range env.Schedule {
		in, err := scheduleRowRule(tenantID, v, env, row, idx, createdBy)
		if err != nil {
			return nil, nil, err
		}
		in.RuleID = deterministicProtocolRuleID(v.ProtocolVersionID, in.DoseCode)
		rules = append(rules, in)
		stored = append(stored, domain.Rule{
			RuleID:              in.RuleID,
			ProtocolVersionID:   in.ProtocolVersionID,
			ProtocolID:          v.ProtocolID,
			DoseCode:            in.DoseCode,
			Sequence:            in.Sequence,
			TriggerType:         in.TriggerType,
			OffsetDays:          in.OffsetDays,
			DueWindowDays:       in.DueWindowDays,
			MinGapDays:          in.MinGapDays,
			Repeat:              in.Repeat,
			RepeatUntilAfterAge: in.RepeatUntilAfterAge,
			CatchUp:             in.CatchUp,
			EligibilityJSON:     in.EligibilityJSON,
			SopVersionID:        optionalString(in.SopVersionID),
			SortOrder:           in.SortOrder,
		})
	}
	if len(rules) == 0 {
		return nil, nil, fmt.Errorf("%w: vaccination matrix produced no executable protocol_rules rows", ErrNotPublishable)
	}
	dimensions, err := compileVaccinationMatrixDimensions(v, env, stored)
	if err != nil {
		return nil, nil, err
	}
	if len(dimensions) == 0 {
		return nil, nil, fmt.Errorf("%w: vaccination matrix produced no compiled rule dimensions", ErrNotPublishable)
	}
	return rules, dimensions, nil
}

func deterministicProtocolRuleID(versionID, doseCode string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("goatos:protocol-rule:"+strings.TrimSpace(versionID)+":"+strings.TrimSpace(doseCode))).String()
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
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
	existing := make(map[string]domain.Rule, len(rules))
	for _, rule := range rules {
		if doseCode := strings.TrimSpace(rule.DoseCode); doseCode != "" {
			existing[doseCode] = rule
		}
	}
	created := 0
	for idx, row := range env.Schedule {
		in, err := scheduleRowRule(tenantID, v, env, row, idx, createdBy)
		if err != nil {
			return err
		}
		if existingRule, ok := existing[in.DoseCode]; ok {
			if isVaccinationMatrixRuleset(env) {
				if err := validateExistingMatrixRuleMetadata(existingRule, idx); err != nil {
					return err
				}
			}
			continue
		}
		if _, err := s.repo.CreateRule(ctx, in); err != nil {
			return err
		}
		existing[in.DoseCode] = domain.Rule{DoseCode: in.DoseCode, EligibilityJSON: in.EligibilityJSON}
		created++
	}
	if len(rules)+created == 0 {
		return fmt.Errorf("%w: publish requires at least one executable protocol_rules row", ErrNotPublishable)
	}
	return nil
}

func (s *Service) compileProtocolRuleDimensions(ctx context.Context, tenantID string, v domain.Version) error {
	writer, ok := s.repo.(protocolRuleDimensionWriter)
	if !ok {
		return nil
	}
	var env ruleDSLEnvelope
	if len(v.RuleDsl) > 0 {
		if err := json.Unmarshal(v.RuleDsl, &env); err != nil {
			return fmt.Errorf("%w: invalid rule_dsl: %v", ErrNotPublishable, err)
		}
	}
	if !isVaccinationMatrixRuleset(env) {
		return nil
	}
	rules, err := s.repo.ListRules(ctx, tenantID, v.ProtocolVersionID)
	if err != nil {
		return err
	}
	dimensions, err := compileVaccinationMatrixDimensions(v, env, rules)
	if err != nil {
		return err
	}
	if len(dimensions) == 0 {
		return fmt.Errorf("%w: vaccination matrix produced no compiled rule dimensions", ErrNotPublishable)
	}
	return writer.ReplaceProtocolRuleDimensions(ctx, tenantID, v.ProtocolVersionID, dimensions)
}

func compileVaccinationMatrixDimensions(v domain.Version, env ruleDSLEnvelope, rules []domain.Rule) ([]domain.RuleDimension, error) {
	out := make([]domain.RuleDimension, 0, len(rules))
	for _, rule := range rules {
		dims, err := compileVaccinationRuleDimensions(v, env, rule)
		if err != nil {
			return nil, err
		}
		out = append(out, dims...)
	}
	return out, nil
}

func compileVaccinationRuleDimensions(v domain.Version, env ruleDSLEnvelope, rule domain.Rule) ([]domain.RuleDimension, error) {
	// protocol_rule_dimensions is an indexed prefilter, not the final medical decision. Species,
	// stage, sex, breed, and age are used by SQL to avoid scanning every animal; lifecycle, health,
	// and reproductive values stay materialized for audit/explainability while the vaccination app
	// re-checks the full eligibility JSON before generating obligations.
	var payload struct {
		MatrixRowID    string          `json:"matrix_row_id"`
		SourceDoseCode string          `json:"source_dose_code"`
		Eligibility    json.RawMessage `json:"eligibility"`
		Vaccine        json.RawMessage `json:"vaccine"`
	}
	if err := json.Unmarshal(rule.EligibilityJSON, &payload); err != nil {
		return nil, fmt.Errorf("%w: rule %q has invalid matrix eligibility_json: %v", ErrNotPublishable, rule.DoseCode, err)
	}
	matrixRowID := strings.TrimSpace(payload.MatrixRowID)
	if matrixRowID == "" {
		return nil, fmt.Errorf("%w: rule %q missing matrix_row_id for compiled dimensions", ErrNotPublishable, rule.DoseCode)
	}
	eligibility, err := rawObjectMap(payload.Eligibility)
	if err != nil {
		return nil, fmt.Errorf("%w: rule %q invalid eligibility selectors: %v", ErrNotPublishable, rule.DoseCode, err)
	}
	vaccine, err := rawObjectMap(payload.Vaccine)
	if err != nil {
		return nil, fmt.Errorf("%w: rule %q invalid vaccine metadata: %v", ErrNotPublishable, rule.DoseCode, err)
	}
	matrixRow, schedule, ok := findMatrixRowForDose(env.MatrixRows, scheduleRow{DoseCode: rule.DoseCode, SourceDoseCode: payload.SourceDoseCode})
	if !ok {
		return nil, fmt.Errorf("%w: rule %q missing matrix schedule cell for compiled dimensions", ErrNotPublishable, rule.DoseCode)
	}
	scheduleJSON, _ := json.Marshal(schedule)
	if len(scheduleJSON) == 0 || string(scheduleJSON) == "null" {
		scheduleJSON = []byte(`{}`)
	}
	category := strings.TrimSpace(env.Category)
	if category == "" {
		category = v.Category
	}
	species, err := selectorValues(eligibility, []string{"species"}, "all", "species")
	if err != nil {
		return nil, err
	}
	stages, err := selectorValues(eligibility, []string{"animal_stage", "stage"}, "all", "animal_stage")
	if err != nil {
		return nil, err
	}
	sexes, err := selectorValues(eligibility, []string{"sex"}, "all", "sex")
	if err != nil {
		return nil, err
	}
	breeds, err := selectorValues(eligibility, []string{"breed", "breed_class"}, "all", "breed")
	if err != nil {
		return nil, err
	}
	lifecycles, err := selectorValues(eligibility, []string{"lifecycle"}, "alive", "lifecycle")
	if err != nil {
		return nil, err
	}
	healths, err := selectorValues(eligibility, []string{"health"}, "any", "health")
	if err != nil {
		return nil, err
	}
	reproductive, err := selectorValues(eligibility, []string{"reproductive"}, "any", "reproductive")
	if err != nil {
		return nil, err
	}
	minAge, err := selectorInt32(eligibility, "min_age_days")
	if err != nil {
		return nil, fmt.Errorf("%w: rule %q invalid min_age_days: %v", ErrNotPublishable, rule.DoseCode, err)
	}
	maxAge, err := selectorInt32(eligibility, "max_age_days")
	if err != nil {
		return nil, fmt.Errorf("%w: rule %q invalid max_age_days: %v", ErrNotPublishable, rule.DoseCode, err)
	}
	vaccineCode := selectorString(vaccine, "code")
	vaccineType := selectorString(vaccine, "type")
	pathogenClass := selectorString(vaccine, "pathogen_class")
	compatibilityGroup := selectorString(vaccine, "compatibility_group")
	sourceDose := strings.TrimSpace(payload.SourceDoseCode)
	if sourceDose == "" {
		sourceDose = strings.TrimSpace(schedule.SourceDoseCode)
	}
	if sourceDose == "" {
		sourceDose = strings.TrimSpace(rule.DoseCode)
	}

	out := make([]domain.RuleDimension, 0, len(species)*len(stages)*len(sexes)*len(breeds))
	for _, sp := range species {
		for _, stage := range stages {
			for _, sex := range sexes {
				for _, breed := range breeds {
					for _, lifecycle := range lifecycles {
						for _, health := range healths {
							for _, repro := range reproductive {
								if len(out) >= maxCompiledRuleDimensionsPerRule {
									return nil, fmt.Errorf("%w: rule %q expands past %d compiled dimensions", ErrNotPublishable, rule.DoseCode, maxCompiledRuleDimensionsPerRule)
								}
								selectorKey := strings.Join([]string{matrixRow.RowID, rule.RuleID, rule.DoseCode, sp, stage, sex, breed, lifecycle, health, repro}, "|")
								out = append(out, domain.RuleDimension{
									Category:                  category,
									RulesetFamily:             strings.TrimSpace(env.RulesetFamily),
									ProtocolVersionID:         v.ProtocolVersionID,
									RuleID:                    rule.RuleID,
									MatrixRowID:               matrixRowID,
									SelectorKey:               selectorKey,
									DoseCode:                  rule.DoseCode,
									SourceDoseCode:            sourceDose,
									VaccineCode:               vaccineCode,
									VaccineType:               vaccineType,
									PathogenClass:             pathogenClass,
									CompatibilityGroup:        compatibilityGroup,
									Species:                   sp,
									AnimalStage:               stage,
									Sex:                       sex,
									Breed:                     breed,
									Lifecycle:                 lifecycle,
									Health:                    health,
									Reproductive:              repro,
									MinAgeDays:                minAge,
									MaxAgeDays:                maxAge,
									TriggerType:               rule.TriggerType,
									Sequence:                  rule.Sequence,
									OffsetDays:                rule.OffsetDays,
									DueWindowDays:             rule.DueWindowDays,
									MinGapDays:                rule.MinGapDays,
									Repeat:                    rule.Repeat,
									CatchUp:                   rule.CatchUp,
									MaxDelayDays:              schedule.MaxDelayDays,
									RevaccinationIntervalDays: schedule.RevaccinationDays,
									EligibilityJSON:           payload.Eligibility,
									VaccineJSON:               payload.Vaccine,
									ScheduleJSON:              scheduleJSON,
								})
							}
						}
					}
				}
			}
		}
	}
	return out, nil
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
		catchUp = defaultMissedDoseCatchUp(env.MissedDosePolicy)
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
	eligibilityJSON, err := ruleEligibilityJSON(env, row, idx)
	if err != nil {
		return domain.NewRule{}, err
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
		EligibilityJSON:     eligibilityJSON,
		SopVersionID:        sopVersion,
		ProofPolicy:         proof,
		SortOrder:           int32(idx + 1),
		CreatedBy:           createdBy,
		IdempotencyKey:      "protocol-schedule-rule:" + v.ProtocolVersionID + ":" + doseCode,
	}, nil
}

func defaultMissedDoseCatchUp(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" || !strings.HasPrefix(trimmed, "\"") {
		return ""
	}
	var legacy string
	if err := json.Unmarshal(raw, &legacy); err != nil {
		return ""
	}
	return strings.TrimSpace(legacy)
}

func ruleEligibilityJSON(env ruleDSLEnvelope, row scheduleRow, idx int) ([]byte, error) {
	if isVaccinationMatrixRuleset(env) {
		return matrixRuleEligibilityJSON(env, row, idx)
	}
	return plainRuleEligibilityJSON(env.Eligibility), nil
}

func isVaccinationMatrixRuleset(env ruleDSLEnvelope) bool {
	return strings.EqualFold(strings.TrimSpace(env.RulesetFamily), "vaccination.matrix") ||
		strings.EqualFold(strings.TrimSpace(env.Vaccine.Code), "vaccination.matrix") ||
		len(env.MatrixRows) > 0
}

func publishedVersionLooksLikeVaccinationMatrix(v domain.Version) bool {
	if !strings.EqualFold(strings.TrimSpace(v.Category), "vaccination") {
		return false
	}
	env, err := decodeRuleDSLEnvelope(v.RuleDsl)
	if err == nil {
		return isVaccinationMatrixRuleset(env)
	}
	compact := strings.NewReplacer(" ", "", "\n", "", "\r", "", "\t", "").Replace(strings.ToLower(string(v.RuleDsl)))
	return strings.Contains(compact, `"ruleset_family":"vaccination.matrix"`) ||
		strings.Contains(compact, `"code":"vaccination.matrix"`) ||
		strings.Contains(compact, `"matrix_rows":`)
}

func matrixRuleEligibilityJSON(env ruleDSLEnvelope, row scheduleRow, idx int) ([]byte, error) {
	match, matchedCell, ok := findMatrixRowForDose(env.MatrixRows, row)
	if !ok {
		return nil, fmt.Errorf("%w: schedule[%d] matrix row metadata required for dose_code %q", ErrNotPublishable, idx, row.DoseCode)
	}
	rowEligibility := rawObjectOnly(match.Eligibility)
	vaccine := rawObjectOnly(match.Vaccine)
	if len(rowEligibility) == 0 || string(rowEligibility) == "null" || string(rowEligibility) == "{}" {
		return nil, fmt.Errorf("%w: schedule[%d] matrix row eligibility required", ErrNotPublishable, idx)
	}
	if len(vaccine) == 0 || string(vaccine) == "null" || string(vaccine) == "{}" {
		return nil, fmt.Errorf("%w: schedule[%d] matrix row vaccine required", ErrNotPublishable, idx)
	}
	eligibility, err := mergedEligibilityJSON(env.Eligibility, rowEligibility)
	if err != nil {
		return nil, fmt.Errorf("%w: schedule[%d] matrix row eligibility merge failed: %v", ErrNotPublishable, idx, err)
	}
	sourceDose := strings.TrimSpace(matchedCell.SourceDoseCode)
	if sourceDose == "" {
		sourceDose = strings.TrimSpace(row.SourceDoseCode)
	}
	if sourceDose == "" {
		sourceDose = strings.TrimSpace(row.DoseCode)
	}
	payload := struct {
		MatrixRowID    string          `json:"matrix_row_id,omitempty"`
		SourceDoseCode string          `json:"source_dose_code,omitempty"`
		Eligibility    json.RawMessage `json:"eligibility"`
		Vaccine        json.RawMessage `json:"vaccine"`
	}{
		MatrixRowID:    strings.TrimSpace(match.RowID),
		SourceDoseCode: sourceDose,
		Eligibility:    eligibility,
		Vaccine:        vaccine,
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: schedule[%d] matrix metadata marshal failed: %v", ErrNotPublishable, idx, err)
	}
	return out, nil
}

func mergedEligibilityJSON(baseRaw, overrideRaw json.RawMessage) (json.RawMessage, error) {
	base, err := rawObjectMap(baseRaw)
	if err != nil {
		return nil, err
	}
	override, err := rawObjectMap(overrideRaw)
	if err != nil {
		return nil, err
	}
	for key, value := range override {
		if strings.TrimSpace(string(value)) == "" || strings.TrimSpace(string(value)) == "null" {
			continue
		}
		base[key] = value
	}
	if err := canonicalizeEligibilitySelectors(base); err != nil {
		return nil, err
	}
	out, err := json.Marshal(base)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func canonicalizeEligibilitySelectors(obj map[string]json.RawMessage) error {
	for _, field := range []string{"species", "animal_stage", "sex", "breed", "lifecycle", "health", "reproductive"} {
		raw, ok := obj[field]
		if !ok || strings.TrimSpace(string(raw)) == "" || strings.TrimSpace(string(raw)) == "null" {
			continue
		}
		values, err := rawSelectorValues(raw)
		if err != nil {
			return fmt.Errorf("selector %s must be a string or array of strings", field)
		}
		normalized := make([]string, 0, len(values))
		seen := map[string]bool{}
		for _, value := range values {
			v, err := normalizeSelectorValue(field, value)
			if err != nil {
				return err
			}
			if v == "" || seen[v] {
				continue
			}
			seen[v] = true
			normalized = append(normalized, v)
		}
		if len(normalized) == 0 {
			continue
		}
		if raw, err := json.Marshal(normalized); err == nil {
			obj[field] = raw
		}
	}
	return nil
}

func validateExistingMatrixRuleMetadata(rule domain.Rule, idx int) error {
	var payload struct {
		MatrixRowID string          `json:"matrix_row_id"`
		Eligibility json.RawMessage `json:"eligibility"`
		Vaccine     json.RawMessage `json:"vaccine"`
	}
	if err := json.Unmarshal(rule.EligibilityJSON, &payload); err != nil {
		return fmt.Errorf("%w: schedule[%d] existing matrix rule %q has invalid eligibility_json: %v", ErrNotPublishable, idx, rule.DoseCode, err)
	}
	if strings.TrimSpace(payload.MatrixRowID) == "" {
		return fmt.Errorf("%w: schedule[%d] existing matrix rule %q missing matrix_row_id", ErrNotPublishable, idx, rule.DoseCode)
	}
	if len(rawObjectOnly(payload.Eligibility)) == 0 || string(rawObjectOnly(payload.Eligibility)) == "{}" {
		return fmt.Errorf("%w: schedule[%d] existing matrix rule %q missing row eligibility", ErrNotPublishable, idx, rule.DoseCode)
	}
	if len(rawObjectOnly(payload.Vaccine)) == 0 || string(rawObjectOnly(payload.Vaccine)) == "{}" {
		return fmt.Errorf("%w: schedule[%d] existing matrix rule %q missing row vaccine", ErrNotPublishable, idx, rule.DoseCode)
	}
	return nil
}

func findMatrixRowForDose(rows []matrixRow, row scheduleRow) (matrixRow, scheduleRow, bool) {
	doseCode := strings.TrimSpace(row.DoseCode)
	sourceDose := strings.TrimSpace(row.SourceDoseCode)
	for _, matrixRow := range rows {
		for _, cell := range matrixRow.Schedule {
			if strings.TrimSpace(cell.DoseCode) == doseCode {
				return matrixRow, cell, true
			}
			if sourceDose != "" && strings.TrimSpace(cell.SourceDoseCode) == sourceDose {
				return matrixRow, cell, true
			}
		}
	}
	return matrixRow{}, scheduleRow{}, false
}

func rawObjectOnly(raw json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return []byte(`{}`)
	}
	return []byte(trimmed)
}

func rawObjectMap(raw json.RawMessage) (map[string]json.RawMessage, error) {
	raw = rawObjectOnly(raw)
	out := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func selectorValues(obj map[string]json.RawMessage, keys []string, fallback, field string) ([]string, error) {
	for _, key := range keys {
		raw, ok := obj[key]
		if !ok || strings.TrimSpace(string(raw)) == "" || strings.TrimSpace(string(raw)) == "null" {
			continue
		}
		values, err := rawSelectorValues(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: selector %s must be a string or array of strings", ErrNotPublishable, field)
		}
		out := make([]string, 0, len(values))
		seen := map[string]bool{}
		for _, value := range values {
			normalized, err := normalizeSelectorValue(field, value)
			if err != nil {
				return nil, err
			}
			if normalized == "" || seen[normalized] {
				continue
			}
			seen[normalized] = true
			out = append(out, normalized)
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	normalized, err := normalizeSelectorValue(field, fallback)
	if err != nil {
		return nil, err
	}
	return []string{normalized}, nil
}

func rawSelectorValues(raw json.RawMessage) ([]string, error) {
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return []string{single}, nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err == nil {
		return many, nil
	}
	var anyMany []any
	if err := json.Unmarshal(raw, &anyMany); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(anyMany))
	for _, item := range anyMany {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("non-string selector")
		}
		out = append(out, s)
	}
	return out, nil
}

// rejectPartialClinicalDeferStates rejects a defer_states array that is present
// and non-empty but omits any mandatory clinical safety state. An absent or
// empty defer_states maps to the engine's safe full default and is allowed
// (see domain.MandatoryClinicalDeferStates and vaccination.deferStateSet). A
// present, non-empty, partial list is an unsafe authored payload — it would let
// a sick/under-treatment animal's open work be cancelled instead of deferred —
// and must fail publish rather than be silently rewritten (C35-010).
func rejectPartialClinicalDeferStates(obj map[string]json.RawMessage, field string) error {
	raw, ok := obj["defer_states"]
	if !ok {
		return nil
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" || trimmed == "[]" {
		return nil
	}
	values, err := rawSelectorValues(raw)
	if err != nil {
		return fmt.Errorf("%w: %s must be a string or array of strings", ErrNotPublishable, field)
	}
	if len(values) == 0 {
		return nil
	}
	if missing := domain.MissingMandatoryClinicalDeferStates(values); len(missing) > 0 {
		return fmt.Errorf("%w: %s omits mandatory clinical safety states %v; sick/under_treatment/quarantine/icu are safety blocks that must be deferred, not cancelled (leave defer_states empty to use the safe default)", ErrNotPublishable, field, missing)
	}
	return nil
}

func normalizeSelectorValue(field, value string) (string, error) {
	v := strings.TrimSpace(value)
	if v == "" {
		return "", nil
	}
	if v == "*" || (strings.EqualFold(v, "any") && selectorFieldUsesAllWildcard(field)) {
		v = "all"
	}
	switch field {
	case "species", "sex", "health", "reproductive", "lifecycle":
		v = strings.ToLower(v)
	}
	if field == "sex" {
		switch v {
		case "female", "male", "all":
			return v, nil
		case "unknown":
			return "", fmt.Errorf("%w: unknown sex is banned from rule selectors", ErrNotPublishable)
		default:
			return "", fmt.Errorf("%w: unsupported sex selector %q", ErrNotPublishable, value)
		}
	}
	return v, nil
}

func selectorFieldUsesAllWildcard(field string) bool {
	switch field {
	case "species", "animal_stage", "sex", "breed", "lifecycle", "health", "reproductive":
		return true
	default:
		return false
	}
}

func selectorString(obj map[string]json.RawMessage, key string) string {
	raw, ok := obj[key]
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	return ""
}

func selectorInt32(obj map[string]json.RawMessage, key string) (*int32, error) {
	raw, ok := obj[key]
	if !ok || strings.TrimSpace(string(raw)) == "" || strings.TrimSpace(string(raw)) == "null" {
		return nil, nil
	}
	var n int32
	if err := json.Unmarshal(raw, &n); err == nil {
		return &n, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	parsed, err := strconv.ParseInt(strings.TrimSpace(s), 10, 32)
	if err != nil {
		return nil, err
	}
	n = int32(parsed)
	return &n, nil
}

func plainRuleEligibilityJSON(raw json.RawMessage) []byte {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return []byte(`{}`)
	}
	return []byte(trimmed)
}
