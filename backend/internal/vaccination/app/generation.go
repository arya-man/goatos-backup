// seed-fixture-guard:ignore: this generator change reconciles and supersedes EXISTING obligations,
// uses already-seeded procurement purpose data, and reads no new seed source. It adds no seed input,
// changes no fixture column, and moves no canonical seed schema, so the fixture manifest, the
// source-CSV validators and the seed-source date contract have nothing to record. The migrations it
// ships beside are additive and declare their own no-seed-impact reason.
package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	oblports "github.com/vgoats/goatos/backend/internal/obligation/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

// ProtocolReader is the slice of the protocol repo SM-1 generation needs.
type ProtocolReader interface {
	GetVersion(ctx context.Context, tenantID, versionID string) (protodomain.Version, error)
	ListRules(ctx context.Context, tenantID, versionID string) ([]protodomain.Rule, error)
	// ListEffectiveVaccinationVersionsForGoat returns one published version id per protocol — the
	// version effective as of asOf for the goat's park scope. Per-goat generation never issues doses
	// from a superseded version.
	ListEffectiveVaccinationVersionsForGoat(ctx context.Context, tenantID, parkID string, asOf time.Time) ([]string, error)
}

// ProtocolVersionBatchReader is implemented by the production protocol repo so cohort generation
// can load rulebooks once per page of version ids rather than once per goat.
type ProtocolVersionBatchReader interface {
	GetVersionsByIDs(ctx context.Context, tenantID string, versionIDs []string) (map[string]protodomain.Version, error)
	ListRulesForVersions(ctx context.Context, tenantID string, versionIDs []string) (map[string][]protodomain.Rule, error)
}

// EffectiveVaccinationVersionBatchReader resolves effective protocol versions for every park in a
// page at once. It returns entries keyed by the original park id; the empty key is tenant scope.
type EffectiveVaccinationVersionBatchReader interface {
	ListEffectiveVaccinationVersionsForParks(ctx context.Context, tenantID string, parkIDs []string, asOf time.Time) (map[string][]string, error)
}

// GoatLister is the eligible-goat source (the vaccination repo): chunked for backfill, single for
// the goat.created path.
type GoatLister interface {
	ListEligibleGoatsForGeneration(ctx context.Context, f domain.ImpactFilter, afterGoatID string, limit int32) ([]domain.EligibleGoat, error)
	GetGoatForGeneration(ctx context.Context, tenantID, goatID string) (domain.EligibleGoat, bool, error)
}

var errGenerationPartialFailures = errors.New("vaccination: generation completed with failed goats")

// IsGenerationPartialFailure reports whether generation finished the bounded scan but isolated at
// least one deterministic per-goat failure. Callers should surface the counters before failing the
// job so operators can distinguish this from a hard infrastructure abort.
func IsGenerationPartialFailure(err error) bool {
	return errors.Is(err, errGenerationPartialFailures)
}

// IsGenerationAbortError reports whether a generation error should abort the whole run rather than
// be counted as a per-goat poison record. Context cancellation and retryable database/connection
// failures are run-level faults: swallowing them as failed goats hides infrastructure incidents and
// prevents the scheduler from retrying the scan cleanly.
func IsGenerationAbortError(err error) bool {
	return shouldAbortGeneration(err)
}

// ObligationWriter is the slice of the obligation repo SM-1 generation needs.
type ObligationWriter interface {
	InsertObligation(ctx context.Context, in obldomain.NewObligation) (string, bool, error)
	InsertDeferredObligation(ctx context.Context, in obldomain.NewObligation, reason string, occurredAt time.Time) (string, bool, error)
	// Replay reconciliation returns the row's final persisted status and due date from the same
	// transaction. Cross-vaccine spacing must never depend on a stale page snapshot or a proposed
	// recovery date that the database did not apply.
	DeferOpenObligationForGeneration(ctx context.Context, tenantID, idempotencyKey, reason string, occurredAt time.Time) (obldomain.ObligationRef, bool, error)
	ReopenDeferredObligationForGeneration(ctx context.Context, tenantID, idempotencyKey string, occurredAt time.Time, reschedule *obldomain.RecoveryReschedule) (obldomain.ObligationRef, bool, error)
	RealignOpenObligationForGeneration(ctx context.Context, tenantID, idempotencyKey string, dueAt time.Time, windowEnd *time.Time, occurredAt time.Time) (obldomain.ObligationRef, bool, error)
	FindNearestPlannedBatchDate(ctx context.Context, tenantID, versionID, ruleID, vaccineCode, shedID, parkID string, from, to time.Time) (*time.Time, error)
	CancelOpenObligationByIdempotencyKey(ctx context.Context, tenantID, idempotencyKey, reason string, occurredAt time.Time) (obligationID string, changed bool, err error)
	CancelOpenVaccinationObligationsForExitedGoats(ctx context.Context, tenantID, reason string, occurredAt time.Time) (int, error)
	CancelOpenVaccinationObligationsForGoatExceptVersions(ctx context.Context, tenantID, goatID string, effectiveVersionIDs []string, reason string, occurredAt time.Time) (int, error)
	// Bounded pre-filter for plan replacement: which of these animals still hold open work
	// under a version that is no longer effective for them. Usually none, for one indexed read.
	GoatsWithVaccinationObligationsOutsideVersions(ctx context.Context, tenantID string, goatIDs, effectiveVersionIDs []string) ([]string, error)
	CarryOverUnchangedVaccinationObligations(ctx context.Context, tenantID string, goatIDs, effectiveVersionIDs []string) (int, error)
	ReconcileOpenObligationForRuleIdentity(ctx context.Context, tenantID string, in obldomain.NewObligation, occurredAt time.Time) (obldomain.ObligationRef, bool, error)
	// Finds an open row by the CAUSE it descends from, for the case where an insert was
	// refused because another writer already created this cycle under a different key.
	OpenObligationForRepeatCycle(ctx context.Context, tenantID, protocolVersionID, ruleID, targetType, targetID string, sequence int32, sourceRef string) (obldomain.ObligationRef, bool, error)
	CancelOpenVaccinationObligationsForGoatVersion(ctx context.Context, tenantID, goatID, protocolVersionID, reason string, occurredAt time.Time) (int, error)
	RecordStatusEvent(ctx context.Context, ev obldomain.NewStatusEvent) (string, bool, error)
	// NextSuccessorSuffix computes the next free numeric successor suffix for a base idempotency key
	// in one bounded query (R50-011), avoiding O(N) probe round trips on large collision histories.
	NextSuccessorSuffix(ctx context.Context, tenantID, baseKey string) (int, error)
}

// ManualVaccineAnchorReader is implemented by the production obligation store.
// seed-fixture-guard:ignore: this is generation suppression for runtime manual anchors; it changes no seed source, fixture column, SOP DSL, or HRMS import contract.
// A manual-campaign anchor is the starting point for that animal's vaccine family:
// once it exists, DOB/arrival/calendar base rules for the same vaccine must not
// recreate earlier work. Repeat rules still run from accepted history after the
// anchored dose is verified.
type ManualVaccineAnchorReader interface {
	ManualVaccineAnchorsForGoat(ctx context.Context, tenantID, goatID string, vaccineCodes []string) (map[string]obldomain.ObligationRef, error)
}

// GenerationRunRecorder persists operator-visible generation status. It is optional for unit
// tests, but production wires it so publish-triggered generation is not only a log line.
type GenerationRunRecorder interface {
	StartGenerationRun(ctx context.Context, in domain.GenerationRunInput) (domain.GenerationRun, bool, error)
	FinishGenerationRun(ctx context.Context, tenantID, runID string, result domain.GenerateResult, cursorGoatID, lastError string, completedAt time.Time) error
	// HeartbeatGenerationRun keeps a long (1M-goat) run from being reclaimed mid-flight by the
	// stale-run detector. Called per page; best-effort (errors are non-fatal to the run).
	HeartbeatGenerationRun(ctx context.Context, tenantID, runID string) error
}

// CompletionEvidenceReader checks reviewed imported/HF completion evidence before SM-1 materializes
// a matching post-arrival obligation. Only trusted evidence may suppress due work.
type CompletionEvidenceReader interface {
	HasTrustedCompletionEvidence(ctx context.Context, tenantID, goatID, protocolVersionID, ruleID, doseCode string, dueAt, generationAt time.Time) (bool, error)
}

// BatchCompletionEvidenceReader lets a generation page resolve trusted-history suppression in one
// database round-trip per protocol version instead of goat×rule probes.
type BatchCompletionEvidenceReader interface {
	HasTrustedCompletionEvidenceBatch(ctx context.Context, tenantID, protocolVersionID string, candidates []domain.TrustedCompletionCandidate, generationAt time.Time) (map[string]bool, error)
}

// CrossVaccineGapReader resolves the latest accepted dose per goat for cross-vaccine gap floors.
type CrossVaccineGapReader interface {
	LastRecentVaccineAdministrationsForGoats(ctx context.Context, tenantID string, goatIDs []string, before time.Time) (map[string]domain.RecentVaccineAdministration, error)
}

// CrossVaccineGapHistoryReader resolves every recent accepted/trusted dose per goat for
// cross-vaccine gap floors. Live-live protection must look across the full recent history, not
// only whichever killed/live dose happened latest.
type CrossVaccineGapHistoryReader interface {
	RecentVaccineAdministrationsForGoats(ctx context.Context, tenantID string, goatIDs []string, before time.Time) (map[string][]domain.RecentVaccineAdministration, error)
}

type cachedVersionPlan struct {
	rules          []protodomain.Rule
	deferState     []string
	eligibility    genEligibility
	policies       genVersionPolicies
	vaccineProfile vaccineProfile
}

type goatGenerationPlan struct {
	versionID      string
	rules          []protodomain.Rule
	deferState     []string
	goat           domain.EligibleGoat
	opts           generationOptions
	eligibility    genEligibility
	policies       genVersionPolicies
	vaccineProfile vaccineProfile
}

type trustedEvidenceLookup struct {
	checked map[string]bool
	trusted map[string]bool
}

func newTrustedEvidenceLookup() trustedEvidenceLookup {
	return trustedEvidenceLookup{
		checked: make(map[string]bool),
		trusted: make(map[string]bool),
	}
}

func (l trustedEvidenceLookup) record(key string, trusted bool) {
	if l.checked == nil {
		return
	}
	l.checked[key] = true
	if trusted {
		l.trusted[key] = true
	}
}

func (l trustedEvidenceLookup) get(key string) (bool, bool) {
	if l.checked == nil {
		return false, false
	}
	if !l.checked[key] {
		return false, false
	}
	return l.trusted[key], true
}

// pendingVaccine tracks a vaccine being generated in the current genOneGoat pass,
// used to enforce cross-vaccine spacing among co-due pending obligations (BUG #2).
type pendingVaccine struct {
	code  string
	class vaccineImmunoClass
	due   time.Time
}

func actionableObligationForSpacing(ref obldomain.ObligationRef) bool {
	if ref.DueAt.IsZero() {
		return false
	}
	switch strings.TrimSpace(ref.Status) {
	case "scheduled", "due", "missed", "deferred":
		return true
	default:
		return false
	}
}

// cancelReasonMintsSuccessor classifies a persisted cancellation reason (from the most recent
// 'canceled' obligation_status_events payload, wired through ObligationRef.Reason) into
// successor-eligible vs terminal. FAIL CLOSED: empty or unrecognized reasons never mint a
// successor — a legacy row with no recorded reason gives no evidence the goat is returning.
//
// The vocabulary below is the FULL set of reasons real producers write today (grep of every
// CancelOpen* call site plus adapter defaults):
//
//	"ineligible_after_shift"                    generation.go shift-away cancel (park A -> B).
//	                                            THE returning-goat case: the goat later
//	                                            requalifies (B -> A) and must get fresh work. MINTS.
//	"ineligible_after_exit"                     obligation/app/cancel.go goat.exited handler.
//	                                            Goat left the herd; if it ever returns, re-entry
//	                                            generation creates fresh work. NO successor.
//	"vaccine_history_outranks_anchor"           generation history reconciliation — the shot is
//	                                            already covered by real history. NO successor.
//	"vaccine_history_now_available"             generation history reconciliation — catch-up row
//	                                            replaced by history-anchored schedule. NO successor.
//	"missing_due_date_resolved"                 generation — placeholder row replaced by a real
//	                                            dated obligation. NO successor.
//	"version_no_longer_effective_after_recheck" generation recheck — version superseded for this
//	                                            goat's scope. NO successor.
//	"version_no_longer_effective"               adapter default for the except-versions cancel.
//	                                            Same semantics as above. NO successor.
//	"ineligible_after_recheck"                  adapter default for the per-version cancel when a
//	                                            caller passes no reason. Ambiguous → fail closed.
//	"protocol_version_replaced"                 generation tenant-wide scan — the version that
//	                                            produced this work is no longer effective for the
//	                                            animal. MINTS, for the same reason the shift-away
//	                                            cancel does: the animal can come back into that
//	                                            version's scope (it moves parks and returns, or a
//	                                            park override lapses and the tenant default is
//	                                            effective again). A fixed-due dose then recomputes
//	                                            to the SAME idempotency key, meets its own canceled
//	                                            row, and without a successor the animal silently
//	                                            never receives that vaccination. Repeat cycles
//	                                            escape that trap on their own -- their identity is
//	                                            the cause, not the key -- but birth-age and course
//	                                            doses do not.
func cancelReasonMintsSuccessor(reason string) bool {
	switch strings.TrimSpace(reason) {
	case "ineligible_after_shift", "protocol_version_replaced":
		return true
	default:
		return false
	}
}

// applyCrossVaccineGapFloorFromPending floors a vaccine's due date against incompatible
// pending vaccines already generated in this pass. Cross-vaccine spacing is a positive medical
// assertion between two IDENTIFIED, DIFFERENT vaccine products, so it applies ONLY when both the
// prior pending vaccine and next carry a non-empty vaccine code AND those codes differ:
//   - Same code = two sequential doses of ONE course (e.g., ET+TT week-0 then week-7). These must
//     never be pushed apart by the cross-vaccine gap — that would wrongly delay a legitimate
//     same-course dose. This mirrors applyCrossVaccineGapFloor's own same-code guard.
//   - Empty/unknown code on either side = we cannot establish the pair are distinct products, so
//     we do not fabricate a gap between them. (Unknown-class fail-closed still applies against
//     real administered history via applyCrossVaccineGapFloorFromHistory; upstream config/publish
//     validation rejects vaccines without a valid classification before they can be scheduled.)
//
// Callers append pending in rule order, so results are deterministic across replayed generation.
func applyCrossVaccineGapFloorFromPending(due time.Time, next vaccineProfile, pending []pendingVaccine, policy genCompatibilityPolicy) time.Time {
	out := due
	for _, p := range pending {
		if p.code == "" || next.Code == "" || strings.EqualFold(p.code, next.Code) {
			continue
		}
		gap := crossVaccineGapDays(p.class, next.Class, policy)
		if gap <= 0 {
			continue
		}
		floor := businessDayStart(p.due).AddDate(0, 0, int(gap))
		if out.Before(floor) {
			out = floor
		}
	}
	return out
}

// GenerationService implements SM-1: expand a published protocol version's rules into per-goat
// obligations over the in-care cohort, idempotently, deferring (visibly) recovering/ICU/quarantine/sick goats.
type GenerationService struct {
	proto           ProtocolReader
	goats           GoatLister
	obl             ObligationWriter
	evidence        CompletionEvidenceReader
	crossVaccineGap CrossVaccineGapReader
	crossHistory    CrossVaccineGapHistoryReader
	preArrival      PreArrivalHistoryWriter
	runs            GenerationRunRecorder
	page            int32
}

// NewGenerationService wires the three repos. The completion-evidence reader is auto-wired when the
// goat repo implements it (the production path); requireEvidenceReader then fails generation loudly if
// it is ever absent, so trusted HF/import evidence is never silently ignored (double-dose risk).
func NewGenerationService(proto ProtocolReader, goats GoatLister, obl ObligationWriter) *GenerationService {
	s := &GenerationService{proto: proto, goats: goats, obl: obl, page: 500}
	if reader, ok := goats.(CompletionEvidenceReader); ok {
		s.evidence = reader
	}
	if gapReader, ok := goats.(CrossVaccineGapReader); ok {
		s.crossVaccineGap = gapReader
	}
	if historyReader, ok := goats.(CrossVaccineGapHistoryReader); ok {
		s.crossHistory = historyReader
	}
	if writer, ok := goats.(PreArrivalHistoryWriter); ok {
		s.preArrival = writer
	}
	if recorder, ok := goats.(GenerationRunRecorder); ok {
		s.runs = recorder
	}
	return s
}

// WithPreArrivalHistoryWriter overrides the auto-wired pre-arrival accepted-history writer
// (BUG-017). Production discovers it off the vaccination repository; this exists for composition
// roots that inject a different adapter.
func (s *GenerationService) WithPreArrivalHistoryWriter(w PreArrivalHistoryWriter) *GenerationService {
	s.preArrival = w
	return s
}

// WithGenerationRunRecorder overrides the optional durable generation-run recorder.
func (s *GenerationService) WithGenerationRunRecorder(rec GenerationRunRecorder) *GenerationService {
	s.runs = rec
	return s
}

// requireEvidenceReader guards generation: without a completion-evidence reader, SM-1 cannot check
// trusted HF/import evidence and could re-issue a dose a goat already received. Refuse rather than
// silently skip the suppression check.
func (s *GenerationService) requireEvidenceReader() error {
	if s.evidence == nil {
		return fmt.Errorf("vaccination: generation requires a completion-evidence reader to honor trusted HF/import suppression; refusing to generate")
	}
	return nil
}

type genEligibility struct {
	AnimalStage               genStringList `json:"animal_stage"`
	Stage                     genStringList `json:"stage"`
	Species                   genStringList `json:"species"`
	Sex                       genStringList `json:"sex"`
	Breed                     genStringList `json:"breed"`
	Lifecycle                 genStringList `json:"lifecycle"`
	Health                    genStringList `json:"health"`
	Reproductive              genStringList `json:"reproductive"`
	ProcurementPurpose        genStringList `json:"procurement_purpose"`
	ExcludeReproductiveStates []string      `json:"exclude_reproductive_states"`
	DeferStates               []string      `json:"defer_states"`
	MinAgeDays                *int32        `json:"min_age_days"`
	MaxAgeDays                *int32        `json:"max_age_days"`
	AgeBand                   genStringList `json:"age_band"`
}

type genDSL struct {
	Vaccine             genVaccineMeta         `json:"vaccine"`
	Eligibility         genEligibility         `json:"eligibility"`
	CompatibilityPolicy genCompatibilityPolicy `json:"compatibility_policy"`
	ProcurementPolicy   genProcurementPolicy   `json:"procurement_policy"`
	PregnancyPolicy     genPregnancyPolicy     `json:"pregnancy_policy"`
	RecoveryPolicy      genRecoveryPolicy      `json:"recovery_policy"`
	MissedDosePolicy    genMissedDosePolicy    `json:"missed_dose_policy"`
}

type genRuleMetadata struct {
	Eligibility json.RawMessage `json:"eligibility"`
	Vaccine     genVaccineMeta  `json:"vaccine"`
}

func versionPoliciesFromDSL(dsl genDSL) genVersionPolicies {
	return genVersionPolicies{
		Procurement:   dsl.ProcurementPolicy,
		Pregnancy:     dsl.PregnancyPolicy,
		Compatibility: dsl.CompatibilityPolicy,
		Recovery:      dsl.RecoveryPolicy,
		MissedDose:    dsl.MissedDosePolicy,
	}
}

type genStringList []string

func (l *genStringList) UnmarshalJSON(raw []byte) error {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		*l = nil
		return nil
	}
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		*l = []string{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err != nil {
		return err
	}
	*l = many
	return nil
}

// normDim maps "all"/"any"/"*"/"" to "" (no filter); any other value is an exact-match dimension.
func normDim(v string) string {
	v = strings.TrimSpace(v)
	switch strings.ToLower(v) {
	case "", "all", "any", "*":
		return ""
	default:
		return v
	}
}

func parseGenerationDSL(raw []byte) (genDSL, error) {
	var dsl genDSL
	if len(raw) == 0 {
		return dsl, nil
	}
	if err := json.Unmarshal(raw, &dsl); err != nil {
		return dsl, fmt.Errorf("vaccination: invalid published rule_dsl for generation: %w", err)
	}
	return dsl, nil
}

func ruleGenerationContext(rule protodomain.Rule, fallbackEligibility genEligibility, fallbackVaccine vaccineProfile) (genEligibility, vaccineProfile, error) {
	elig := fallbackEligibility
	vaccine := fallbackVaccine
	raw := strings.TrimSpace(string(rule.EligibilityJSON))
	if raw == "" || raw == "null" || raw == "{}" {
		return elig, vaccine, nil
	}

	var meta genRuleMetadata
	if err := json.Unmarshal(rule.EligibilityJSON, &meta); err != nil {
		return elig, vaccine, fmt.Errorf("vaccination: invalid rule eligibility_json for rule %s: %w", rule.RuleID, err)
	}
	if len(meta.Eligibility) > 0 && strings.TrimSpace(string(meta.Eligibility)) != "null" {
		var rowEligibility genEligibility
		if err := json.Unmarshal(meta.Eligibility, &rowEligibility); err != nil {
			return elig, vaccine, fmt.Errorf("vaccination: invalid matrix row eligibility for rule %s: %w", rule.RuleID, err)
		}
		elig = mergeEligibility(elig, rowEligibility)
	} else {
		var rowEligibility genEligibility
		if err := json.Unmarshal(rule.EligibilityJSON, &rowEligibility); err != nil {
			return elig, vaccine, fmt.Errorf("vaccination: invalid direct eligibility_json for rule %s: %w", rule.RuleID, err)
		}
		elig = mergeEligibility(elig, rowEligibility)
	}
	vaccine = mergeVaccineProfile(vaccine, meta.Vaccine)
	return elig, vaccine, nil
}

func mergeEligibility(base, override genEligibility) genEligibility {
	out := base
	if len(override.AnimalStage) > 0 {
		out.AnimalStage = override.AnimalStage
	}
	if len(override.Stage) > 0 {
		out.Stage = override.Stage
	}
	if len(override.Species) > 0 {
		out.Species = override.Species
	}
	if len(override.Sex) > 0 {
		out.Sex = override.Sex
	}
	if len(override.Breed) > 0 {
		out.Breed = override.Breed
	}
	if len(override.Lifecycle) > 0 {
		out.Lifecycle = override.Lifecycle
	}
	if len(override.Health) > 0 {
		out.Health = override.Health
	}
	if len(override.Reproductive) > 0 {
		out.Reproductive = override.Reproductive
	}
	if len(override.ProcurementPurpose) > 0 {
		out.ProcurementPurpose = override.ProcurementPurpose
	}
	if len(override.ExcludeReproductiveStates) > 0 {
		out.ExcludeReproductiveStates = override.ExcludeReproductiveStates
	}
	if len(override.DeferStates) > 0 {
		out.DeferStates = override.DeferStates
	}
	if override.MinAgeDays != nil {
		out.MinAgeDays = override.MinAgeDays
	}
	if override.MaxAgeDays != nil {
		out.MaxAgeDays = override.MaxAgeDays
	}
	if len(override.AgeBand) > 0 {
		out.AgeBand = override.AgeBand
	}
	return out
}

func mergeVaccineProfile(base vaccineProfile, meta genVaccineMeta) vaccineProfile {
	out := base
	if strings.TrimSpace(meta.Code) != "" {
		out.Code = strings.TrimSpace(meta.Code)
	}
	if strings.TrimSpace(meta.Name) != "" {
		out.Name = strings.TrimSpace(meta.Name)
	}
	if strings.TrimSpace(meta.Type) != "" {
		out.Type = strings.TrimSpace(meta.Type)
	}
	if strings.TrimSpace(meta.PathogenClass) != "" {
		out.PathogenClass = strings.TrimSpace(meta.PathogenClass)
	}
	if strings.TrimSpace(meta.CompatibilityGroup) != "" {
		out.CompatibilityGroup = strings.TrimSpace(meta.CompatibilityGroup)
	}
	out.Class = classifyVaccine(out.Type, out.PathogenClass)
	return out
}

// GenerateForVersion generates obligations for every applicable rule of a PUBLISHED version across
// the eligible in-care cohort. Idempotent (deterministic key → ON CONFLICT no-op). Returns counts.
func (s *GenerationService) GenerateForVersion(ctx context.Context, tenantID, versionID string, asOf time.Time) (domain.GenerateResult, error) {
	return s.generateForVersion(ctx, tenantID, versionID, asOf, generationOptions{})
}

// GenerateManualCampaignForVersion materializes a deliberate campaign trigger. It is separate from
// normal publish/backfill generation so manual_campaign rules cannot fire accidentally.
func (s *GenerationService) GenerateManualCampaignForVersion(ctx context.Context, tenantID, versionID, campaignID string, asOf time.Time) (domain.GenerateResult, error) {
	if manualCampaignAsOfInFuture(asOf) {
		return domain.GenerateResult{}, domain.ErrFutureManualCampaign
	}
	return s.generateForVersion(ctx, tenantID, versionID, asOf, generationOptions{
		ManualCampaignID: campaignID,
	})
}

// GenerateEffectiveForAllGoats backfills the current cohort by resolving the effective vaccination
// version per goat/park. This is the safe no-version backfill path; it does not loop every published
// version and therefore cannot double-materialize tenant defaults plus park overrides.
//
// Contract §89 (Authoritative Per-Vaccine Anchor Order) governs every caller of this path, including
// the reviewed source-import seed (backend/cmd/seed-vaccination-real): anchoring is PER VACCINE
// FAMILY, never goat-wide. A family with its own accepted history is course continuation (its next
// dose flows through its own after_previous_completion rule, and the primary/catch-up rule for that
// SAME dose is suppressed — see hasSameDoseAdministration/hasVaccineAdministrationHistory in
// genOneGoat). A blank family — no accepted history of that specific vaccine — independently falls
// through DOB → entry → adult catch-up at the next compatible drive, exactly as it would for a
// zero-history goat; a recorded dose in an unrelated vaccine family (e.g. ET+TT) is never treated as
// proof that this family was also administered (VAX-REV-02). There is no goat-wide "already enrolled"
// checkpoint and no source-import-scoped variant of this function — every goat, imported or
// pre-existing, is generated by the same per-vaccine rules (VAX-REV-03).
func (s *GenerationService) GenerateEffectiveForAllGoats(ctx context.Context, tenantID string, asOf time.Time) (domain.GenerateResult, error) {
	return s.generateEffectiveForAllGoats(ctx, tenantID, asOf, generationOptions{healthRecoveryAlign: true})
}

func (s *GenerationService) generateEffectiveForAllGoats(ctx context.Context, tenantID string, asOf time.Time, baseOpts generationOptions) (domain.GenerateResult, error) {
	var res domain.GenerateResult
	if err := s.requireEvidenceReader(); err != nil {
		return res, err
	}
	if _, err := s.obl.CancelOpenVaccinationObligationsForExitedGoats(ctx, tenantID, "ineligible_after_exit", asOf); err != nil {
		return res, err
	}
	filter := domain.ImpactFilter{TenantID: tenantID}
	after := ""
	plans := make(map[string]cachedVersionPlan)
	effectiveVersionsByPark := make(map[string][]string)
	failedGoats := make(map[string]struct{})
	allPlans := make([]goatGenerationPlan, 0, s.page)
	for {
		goats, err := s.goats.ListEligibleGoatsForGeneration(ctx, filter, after, s.page)
		if err != nil {
			return res, err
		}
		if len(goats) == 0 {
			break
		}
		activeGoats := activeGenerationGoats(goats)
		if err := s.fillEffectiveVersionsForGoats(ctx, tenantID, activeGoats, asOf, effectiveVersionsByPark); err != nil {
			return res, err
		}
		if err := s.loadVersionPlans(ctx, tenantID, missingVersionPlansForGoats(activeGoats, effectiveVersionsByPark, plans), plans); err != nil {
			return res, err
		}

		// Plan replacement: publishing a new matrix retires the old version in the same
		// transaction, but its already-generated obligations stay open and keep appearing on
		// operators' lists beside the replacement plan's own work. The per-animal path
		// superseded them; this scan -- the one that actually runs after a publish -- did not.
		//
		// Work already in progress is deliberately left alone: the cancel path covers
		// scheduled, due and deferred only, so an animal being worked right now is never
		// pulled out from under the operator by a publish.
		if err := s.supersedeRetiredPlanWork(ctx, tenantID, activeGoats, effectiveVersionsByPark, asOf); err != nil {
			return res, err
		}
		pagePlans := make([]goatGenerationPlan, 0, len(activeGoats))
		for _, g := range activeGoats {
			for _, versionID := range effectiveVersionsByPark[generationParkCacheKey(g.ParkID)] {
				p := plans[versionID]
				if !goatMatchesEligibility(g, p.eligibility, p.policies.Pregnancy, asOf) {
					continue
				}
				pagePlans = append(pagePlans, goatGenerationPlan{
					versionID:      versionID,
					rules:          p.rules,
					deferState:     p.deferState,
					goat:           g,
					opts:           baseOpts,
					eligibility:    p.eligibility,
					policies:       p.policies,
					vaccineProfile: p.vaccineProfile,
				})
			}
		}
		allPlans = append(allPlans, pagePlans...)
		if int32(len(goats)) < s.page {
			break
		}
		after = goats[len(goats)-1].GoatID
	}
	vaccineHistoryByGoat, err := s.recentVaccineAdminsForPlans(ctx, tenantID, allPlans, businessDayEnd(asOf))
	if err != nil {
		return res, err
	}
	runOpts := baseOpts
	runOpts.campaignDueByGoat, runOpts.cohortAlignedCampaignByGoat, err = campaignDueOverrides(allPlans, asOf, vaccineHistoryByGoat)
	if err != nil {
		return res, err
	}
	for i := range allPlans {
		allPlans[i].opts = runOpts
	}
	trustedByVersion, err := s.trustedEvidenceForPlans(ctx, tenantID, allPlans, asOf, vaccineHistoryByGoat)
	if err != nil {
		return res, err
	}
	for i, p := range allPlans {
		p.opts = runOpts
		if err := s.genOneGoat(ctx, tenantID, p.versionID, p.rules, p.deferState, p.eligibility, p.goat, asOf, p.opts, p.policies, p.vaccineProfile, vaccineHistoryByGoat[p.goat.GoatID], trustedByVersion[p.versionID], &res); err != nil {
			if shouldAbortGeneration(err) {
				return res, err
			}
			recordFailedGenerationGoat(&res, failedGoats, p.goat.GoatID)
			continue
		}
		if runOpts.heartbeat != nil && (i+1)%100 == 0 {
			runOpts.heartbeat(ctx)
		}
	}
	if res.FailedGoats > 0 {
		return res, errGenerationPartialFailures
	}
	return res, nil
}

func activeGenerationGoats(goats []domain.EligibleGoat) []domain.EligibleGoat {
	if len(goats) == 0 {
		return nil
	}
	out := make([]domain.EligibleGoat, 0, len(goats))
	for _, goat := range goats {
		if inCare(goat.LifecycleStatus) {
			out = append(out, goat)
		}
	}
	return out
}

func generationParkCacheKey(parkID string) string {
	if parkID == "" {
		return "<tenant>"
	}
	return parkID
}

func (s *GenerationService) fillEffectiveVersionsForGoats(ctx context.Context, tenantID string, goats []domain.EligibleGoat, asOf time.Time, cache map[string][]string) error {
	missing := make([]string, 0)
	seen := make(map[string]bool)
	for _, goat := range goats {
		key := generationParkCacheKey(goat.ParkID)
		if _, ok := cache[key]; ok || seen[key] {
			continue
		}
		seen[key] = true
		missing = append(missing, goat.ParkID)
	}
	if len(missing) == 0 {
		return nil
	}
	if reader, ok := s.proto.(EffectiveVaccinationVersionBatchReader); ok {
		byPark, err := reader.ListEffectiveVaccinationVersionsForParks(ctx, tenantID, missing, asOf)
		if err != nil {
			return err
		}
		for _, parkID := range missing {
			cache[generationParkCacheKey(parkID)] = append([]string(nil), byPark[parkID]...)
		}
		return nil
	}
	for _, parkID := range missing {
		// scale-guard:ignore: test-double fallback; production protocol repo implements page-level effective-version lookup.
		versionIDs, err := s.proto.ListEffectiveVaccinationVersionsForGoat(ctx, tenantID, parkID, asOf)
		if err != nil {
			return err
		}
		cache[generationParkCacheKey(parkID)] = append([]string(nil), versionIDs...)
	}
	return nil
}

func missingVersionPlansForGoats(goats []domain.EligibleGoat, effectiveVersionsByPark map[string][]string, plans map[string]cachedVersionPlan) []string {
	seen := make(map[string]bool)
	out := make([]string, 0)
	for _, goat := range goats {
		for _, versionID := range effectiveVersionsByPark[generationParkCacheKey(goat.ParkID)] {
			if _, ok := plans[versionID]; ok || seen[versionID] {
				continue
			}
			seen[versionID] = true
			out = append(out, versionID)
		}
	}
	return out
}

func missingVersionPlans(versionIDs []string, plans map[string]cachedVersionPlan) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(versionIDs))
	for _, versionID := range versionIDs {
		if versionID == "" {
			continue
		}
		if _, ok := plans[versionID]; ok || seen[versionID] {
			continue
		}
		seen[versionID] = true
		out = append(out, versionID)
	}
	return out
}

func (s *GenerationService) loadVersionPlans(ctx context.Context, tenantID string, versionIDs []string, plans map[string]cachedVersionPlan) error {
	missing := missingVersionPlans(versionIDs, plans)
	if len(missing) == 0 {
		return nil
	}
	if reader, ok := s.proto.(ProtocolVersionBatchReader); ok {
		versions, err := reader.GetVersionsByIDs(ctx, tenantID, missing)
		if err != nil {
			return err
		}
		rulesByVersion, err := reader.ListRulesForVersions(ctx, tenantID, missing)
		if err != nil {
			return err
		}
		for _, versionID := range missing {
			v, ok := versions[versionID]
			if !ok {
				return fmt.Errorf("vaccination: protocol version %s not found", versionID)
			}
			if err := cacheVersionPlan(versionID, v, rulesByVersion[versionID], plans); err != nil {
				return err
			}
		}
		return nil
	}
	for _, versionID := range missing {
		// scale-guard:ignore: test-double fallback; production protocol repo implements page-level version lookup.
		v, err := s.proto.GetVersion(ctx, tenantID, versionID)
		if err != nil {
			return err
		}
		// scale-guard:ignore: test-double fallback; production protocol repo implements page-level rule lookup.
		rules, err := s.proto.ListRules(ctx, tenantID, versionID)
		if err != nil {
			return err
		}
		if err := cacheVersionPlan(versionID, v, rules, plans); err != nil {
			return err
		}
	}
	return nil
}

func cacheVersionPlan(versionID string, v protodomain.Version, rules []protodomain.Rule, plans map[string]cachedVersionPlan) error {
	dsl, err := parseGenerationDSL(v.RuleDsl)
	if err != nil {
		return err
	}
	plans[versionID] = cachedVersionPlan{
		rules:          append([]protodomain.Rule(nil), rules...),
		deferState:     dsl.Eligibility.DeferStates,
		eligibility:    dsl.Eligibility,
		policies:       versionPoliciesFromDSL(dsl),
		vaccineProfile: vaccineProfileFromDSL(dsl),
	}
	return nil
}

func versionIDSet(versionIDs []string) map[string]struct{} {
	out := make(map[string]struct{}, len(versionIDs))
	for _, versionID := range versionIDs {
		out[versionID] = struct{}{}
	}
	return out
}

// GenerateRecoveryRepairForGoat runs a single-goat recovery repair using the same nearby-drive
// alignment semantics as goat.health/location recovery events. It is used by bounded repair jobs so
// recovered deferred goats do not depend on a full-herd scan reaching their page.
func (s *GenerationService) GenerateRecoveryRepairForGoat(ctx context.Context, tenantID, goatID string, asOf time.Time) (domain.GenerateResult, error) {
	return s.generateForGoat(ctx, tenantID, goatID, asOf, generationOptions{healthRecoveryAlign: true})
}

// GenerateForVersionWithRun wraps an existing-cohort generation pass in a durable status row. If
// the same idempotency key has already completed, it returns the persisted counts without replaying
// the whole herd scan.
func (s *GenerationService) GenerateForVersionWithRun(ctx context.Context, tenantID, versionID string, asOf time.Time, triggerType, triggerRef string) (domain.GenerationRun, domain.GenerateResult, error) {
	return s.generateForVersionWithRun(ctx, tenantID, versionID, asOf, triggerType, triggerRef, generationOptions{})
}

// GenerateManualCampaignForVersionWithHTTPRun wraps manual campaign generation using the caller's
// HTTP Idempotency-Key as the durable command key. Exact retries return the same run/result without
// deriving a fresh as_of timestamp or materializing duplicate manual obligations.
func (s *GenerationService) GenerateManualCampaignForVersionWithHTTPRun(ctx context.Context, tenantID, versionID, campaignID string, asOf time.Time, idempotencyKey, requestHash string) (domain.GenerationRun, domain.GenerateResult, error) {
	if manualCampaignAsOfInFuture(asOf) {
		return domain.GenerationRun{}, domain.GenerateResult{}, domain.ErrFutureManualCampaign
	}
	return s.generateForVersionWithRun(ctx, tenantID, versionID, asOf, "manual_campaign", manualCampaignTriggerRef(campaignID, asOf), generationOptions{
		ManualCampaignID:  campaignID,
		RunIDempotencyKey: idempotencyKey,
		RunRequestHash:    requestHash,
	})
}

func manualCampaignAsOfInFuture(asOf time.Time) bool {
	return !asOf.IsZero() && asOf.After(time.Now())
}

func (s *GenerationService) generateForVersionWithRun(ctx context.Context, tenantID, versionID string, asOf time.Time, triggerType, triggerRef string, opts generationOptions) (domain.GenerationRun, domain.GenerateResult, error) {
	if s.runs == nil {
		res, err := s.generateForVersion(ctx, tenantID, versionID, asOf, opts)
		return domain.GenerationRun{}, res, err
	}
	key := generationRunKey(tenantID, versionID, triggerType, triggerRef)
	if opts.RunIDempotencyKey != "" {
		key = opts.RunIDempotencyKey
	}
	run, started, err := s.runs.StartGenerationRun(ctx, domain.GenerationRunInput{
		TenantID:          tenantID,
		ProtocolVersionID: versionID,
		TriggerType:       triggerType,
		TriggerRef:        triggerRef,
		StartedAt:         asOf,
		IdempotencyKey:    key,
		RequestHash:       opts.RunRequestHash,
	})
	if err != nil {
		return run, domain.GenerateResult{}, err
	}
	if !started {
		if run.Status == "completed" {
			return run, generationResultFromRun(run), nil
		}
		return run, generationResultFromRun(run), fmt.Errorf("vaccination: generation run %s is already %s", run.RunID, run.Status)
	}
	effectiveAsOf := asOf
	if !run.StartedAt.IsZero() {
		effectiveAsOf = run.StartedAt.UTC()
	}
	runID := run.RunID
	opts.heartbeat = func(ctx context.Context) {
		_ = s.runs.HeartbeatGenerationRun(ctx, tenantID, runID)
	}
	res, genErr := s.generateForVersion(ctx, tenantID, versionID, effectiveAsOf, opts)
	lastError := ""
	if genErr != nil {
		lastError = genErr.Error()
	}
	// india-date-guard:ignore: owner=ravi issue=GH-india-date scope=generation-run-finish-absolute-instant-storage expiry=2026-12-31
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer finishCancel()
	if err := s.runs.FinishGenerationRun(finishCtx, tenantID, run.RunID, res, "", lastError, time.Now().In(biztime.DefaultLocation())); err != nil && genErr == nil {
		genErr = err
	}
	return run, res, genErr
}

type generationOptions struct {
	ManualCampaignID            string
	RunIDempotencyKey           string
	RunRequestHash              string
	campaignDueByGoat           map[string]time.Time
	cohortAlignedCampaignByGoat map[string]bool
	// healthRecoveryAlign enables sick/ICU/quarantine recovery replanning: align to a nearby planned
	// drive within recovery_policy.max_nearby_drive_align_days (default 7), else micro-drive now.
	healthRecoveryAlign bool
	// heartbeat, when set, is invoked once per cohort page so a long run keeps its generation-run row
	// fresh and is not reclaimed mid-flight. Best-effort: errors are intentionally swallowed by the
	// caller closure so a transient heartbeat failure never aborts a multi-minute generation pass.
	heartbeat func(ctx context.Context)
}

func campaignDueGoatKey(versionID, ruleID, goatID string) string {
	return strings.TrimSpace(versionID) + "\x00" + strings.TrimSpace(ruleID) + "\x00" + strings.TrimSpace(goatID)
}

func adultCampaignCohortKey(versionID, parkID, vaccineCode string) string {
	return strings.TrimSpace(versionID) + "\x00" + strings.TrimSpace(parkID) + "\x00" + strings.ToLower(strings.TrimSpace(vaccineCode))
}

func adultCampaignStart(asOf time.Time) time.Time {
	return businessDayStart(asOf).AddDate(0, 0, 1)
}

func businessDayEnd(asOf time.Time) time.Time {
	return businessDayStart(asOf).AddDate(0, 0, 1).Add(-time.Nanosecond)
}

func isAdultCampaignRule(rule protodomain.Rule) bool {
	doseCode := strings.ToLower(strings.TrimSpace(rule.DoseCode))
	return strings.Contains(doseCode, "_adult_") || strings.HasSuffix(doseCode, "_adult")
}

func isRecurringCampaignRule(rule protodomain.Rule) bool {
	switch strings.ToLower(strings.TrimSpace(rule.Repeat)) {
	case "every_n_days", "yearly":
		return true
	default:
		return false
	}
}

func hasPhysicalCampaignPlacement(g domain.EligibleGoat) bool {
	// The physical shed is the indivisible scheduling boundary. Partition metadata is optional:
	// a goat placed in a real park/shed must not disappear from the normal adult campaign merely
	// because the source did not divide that shed into named partitions.
	return strings.TrimSpace(g.ParkID) != "" && strings.TrimSpace(g.ShedID) != ""
}

func hasPhysicalCampaignPartition(g domain.EligibleGoat) bool {
	return hasPhysicalCampaignPlacement(g) && strings.TrimSpace(g.PartitionLabel) != ""
}

func campaignDueOverrides(plans []goatGenerationPlan, asOf time.Time, vaccineHistoryByGoat map[string][]domain.RecentVaccineAdministration) (map[string]time.Time, map[string]bool, error) {
	type cohortWindow struct {
		readyAt     time.Time
		safeThrough time.Time
	}
	type candidate struct {
		goatKey     string
		cohortKey   string
		overrideDue time.Time
	}
	type cohortMember struct {
		goatKey   string
		cohortKey string
	}
	campaignStart := adultCampaignStart(asOf)
	cohortWindows := make(map[string][]cohortWindow)
	var cohortMembers []cohortMember
	var candidates []candidate
	for _, plan := range plans {
		history := vaccineHistoryByGoat[plan.goat.GoatID]
		path := schedulePathForGoat(plan.goat, plan.policies.Procurement, asOf, history)
		for _, rule := range plan.rules {
			ruleEligibility, ruleVaccine, err := ruleGenerationContext(rule, plan.eligibility, plan.vaccineProfile)
			if err != nil {
				return nil, nil, err
			}
			if !goatMatchesEligibility(plan.goat, ruleEligibility, plan.policies.Pregnancy, asOf) {
				continue
			}
			if !ruleMatchesSchedulePath(rule, path) {
				continue
			}
			rowDeferStates := ruleEligibility.DeferStates
			if len(rowDeferStates) == 0 {
				rowDeferStates = plan.deferState
			}
			if deferredReason(plan.goat, rowDeferStates) != "" || policyDeferReason(plan.goat, plan.policies, asOf) != "" {
				continue
			}

			// Adults without accepted history belong to the next compatible normal drive for
			// this vaccine and park. Derive that drive from the intersection of the existing
			// repeat cohort's readiness/safety windows; verifier/director workflow time is not
			// involved because the history reader exposes the medical administered_at instant.
			if path == schedulePathAdultProcurement && hasPhysicalCampaignPlacement(plan.goat) &&
				strings.EqualFold(strings.TrimSpace(rule.TriggerType), "after_previous_completion") &&
				isRecurringCampaignRule(rule) {
				wait, err := repeatMustWaitForPrimaryCourse(rule, ruleVaccine, plan.rules, plan.eligibility, plan.vaccineProfile, path, history)
				if err != nil {
					return nil, nil, err
				}
				if !wait {
					if due, found := dueAfterPreviousCompletion(rule, ruleVaccine, history); found {
						safeThrough := due
						if rule.DueWindowDays > 0 {
							safeThrough = businessDayStart(due).AddDate(0, 0, int(rule.DueWindowDays))
						}
						if due.Before(campaignStart) {
							due = campaignStart
						}
						cohortKey := adultCampaignCohortKey(plan.versionID, plan.goat.ParkID, ruleVaccine.Code)
						cohortWindows[cohortKey] = append(cohortWindows[cohortKey], cohortWindow{readyAt: due, safeThrough: safeThrough})
						cohortMembers = append(cohortMembers, cohortMember{
							goatKey:   campaignDueGoatKey(plan.versionID, rule.RuleID, plan.goat.GoatID),
							cohortKey: cohortKey,
						})
					}
				}
			}

			triggerType := strings.TrimSpace(rule.TriggerType)
			if !strings.EqualFold(triggerType, "post_arrival") && !strings.EqualFold(triggerType, "manual_campaign") {
				continue
			}
			if !isAdultCampaignRule(rule) {
				continue
			}
			if strings.EqualFold(triggerType, "manual_campaign") {
				if !hasPhysicalCampaignPlacement(plan.goat) {
					continue
				}
			} else if !hasPhysicalCampaignPartition(plan.goat) {
				continue
			}
			if hasVaccineAdministrationHistory(ruleVaccine, history) {
				continue
			}
			procDue, procOK := procurementPurposePrimaryDue(plan.goat, rule, ruleVaccine, plan.policies.Procurement)
			if !procOK {
				continue
			}
			candidates = append(candidates, candidate{
				goatKey:     campaignDueGoatKey(plan.versionID, rule.RuleID, plan.goat.GoatID),
				cohortKey:   adultCampaignCohortKey(plan.versionID, plan.goat.ParkID, ruleVaccine.Code),
				overrideDue: procDue,
			})
		}
	}
	if len(candidates) == 0 && len(cohortMembers) == 0 {
		return nil, nil, nil
	}
	cohortDates := make(map[string]time.Time, len(cohortWindows))
	for cohortKey, windows := range cohortWindows {
		if len(windows) == 0 {
			continue
		}
		latestReady := windows[0].readyAt
		earliestSafe := windows[0].safeThrough
		for _, window := range windows[1:] {
			if window.readyAt.After(latestReady) {
				latestReady = window.readyAt
			}
			if window.safeThrough.Before(earliestSafe) {
				earliestSafe = window.safeThrough
			}
		}
		if !latestReady.After(earliestSafe) {
			cohortDates[cohortKey] = latestReady
		}
	}
	out := make(map[string]time.Time, len(candidates)+len(cohortMembers))
	aligned := make(map[string]bool, len(candidates)+len(cohortMembers))
	for _, member := range cohortMembers {
		if cohortDate, found := cohortDates[member.cohortKey]; found {
			out[member.goatKey] = cohortDate
			aligned[member.goatKey] = true
		}
	}
	for _, c := range candidates {
		campaignDate := campaignStart
		if !c.overrideDue.IsZero() {
			campaignDate = c.overrideDue
		}
		// Missing-history adults follow the established normal drive for this
		// vaccine and park. They do not form an earlier offset-based micro-drive.
		if c.overrideDue.IsZero() {
			if cohortDate, found := cohortDates[c.cohortKey]; found {
				campaignDate = cohortDate
				aligned[c.goatKey] = true
			}
		} else if cohortDate, found := cohortDates[c.cohortKey]; found && cohortDate.After(campaignDate) {
			campaignDate = cohortDate
			aligned[c.goatKey] = true
		}
		out[c.goatKey] = campaignDate
	}
	return out, aligned, nil
}

func procurementPurposePrimaryDue(g domain.EligibleGoat, rule protodomain.Rule, vaccine vaccineProfile, policy genProcurementPolicy) (time.Time, bool) {
	purpose := strings.ToLower(strings.TrimSpace(g.ProcurementPurpose))
	if purpose == "" || purpose == "unspecified" {
		return time.Time{}, true
	}
	plan, ok := policy.PurposePlans[purpose]
	if !ok || purpose == "non_breeding" {
		return time.Time{}, true
	}
	vaccineName := procurementVaccineName(vaccine)
	base := warmingEntryAt(g)
	if base == nil {
		return time.Time{}, false
	}
	if containsProcurementVaccine(plan.FirstWave, vaccineName) {
		return adjustPostArrivalDue(g, protodomain.Rule{TriggerType: "post_arrival", OffsetDays: 0, DueWindowDays: rule.DueWindowDays}, policy), true
	}
	secondWave := plan.GoatSecondWave
	if strings.EqualFold(strings.TrimSpace(g.Species), "sheep") {
		secondWave = plan.SheepSecondWave
	}
	if containsProcurementVaccine(secondWave, vaccineName) {
		days := int32(28)
		if plan.SecondWaveAfterDays != nil {
			days = *plan.SecondWaveAfterDays
		}
		return adjustPostArrivalDue(g, protodomain.Rule{TriggerType: "post_arrival", OffsetDays: days, DueWindowDays: rule.DueWindowDays}, policy), true
	}
	return time.Time{}, false
}

func procurementVaccineName(v vaccineProfile) string {
	name := strings.TrimSpace(v.Name)
	if name != "" {
		return name
	}
	return strings.TrimSpace(v.Code)
}

func containsProcurementVaccine(values genStringList, vaccine string) bool {
	needle := normalizeProcurementVaccine(vaccine)
	for _, value := range values {
		if normalizeProcurementVaccine(value) == needle {
			return true
		}
	}
	return false
}

func normalizeProcurementVaccine(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer(" ", "", "_", "", "+", "", "-", "")
	return replacer.Replace(value)
}

func (s *GenerationService) generateForVersion(ctx context.Context, tenantID, versionID string, asOf time.Time, opts generationOptions) (domain.GenerateResult, error) {
	var res domain.GenerateResult
	if err := s.requireEvidenceReader(); err != nil {
		return res, err
	}

	v, err := s.proto.GetVersion(ctx, tenantID, versionID)
	if err != nil {
		return res, err
	}
	if v.Status != "published" {
		return res, fmt.Errorf("vaccination: generate requires a published version, got status %q", v.Status)
	}
	rules, err := s.proto.ListRules(ctx, tenantID, versionID)
	if err != nil {
		return res, err
	}

	dsl, err := parseGenerationDSL(v.RuleDsl)
	if err != nil {
		return res, err
	}
	elig := dsl.Eligibility
	policies := versionPoliciesFromDSL(dsl)
	vaccineProf := vaccineProfileFromDSL(dsl)
	stage := eligibilityStage(elig)
	filter := domain.ImpactFilter{
		TenantID:          tenantID,
		ProtocolVersionID: versionID,
		AsOf:              asOf,
		Stage:             normSingleDim(stage),
		Sex:               normSingleDim(elig.Sex),
		Breed:             normSingleDim(elig.Breed),
	}
	if v.ScopeType == "park" && v.ScopeID != "" {
		park := v.ScopeID
		filter.ParkID = &park
	}
	effectiveVersionsByPark := make(map[string][]string)
	failedGoats := make(map[string]struct{})
	allPlans := make([]goatGenerationPlan, 0, s.page)
	after := ""
	for {
		goats, err := s.goats.ListEligibleGoatsForGeneration(ctx, filter, after, s.page)
		if err != nil {
			return res, err
		}
		if len(goats) == 0 {
			break
		}
		activeGoats := activeGenerationGoats(goats)
		// Which versions are effective for each animal's park, resolved the same way whatever
		// this run's own scope is.
		//
		// This used to run only for tenant-scoped versions, so a park-scoped run left the map
		// empty, the supersede below saw no effective version for the park and skipped, and
		// the previous plan's open work sat beside the new plan's for exactly the animals a
		// park override exists to move.
		//
		// Resolved rather than assumed: a park can hold an override for one vaccine and take
		// the tenant default for the rest, so seeding a single-entry map with this run's own
		// version would retire every one of those tenant-default obligations as "no longer
		// effective".
		if err := s.fillEffectiveVersionsForGoats(ctx, tenantID, activeGoats, asOf, effectiveVersionsByPark); err != nil {
			return res, err
		}
		effectiveVersionSets := make(map[string]map[string]struct{})
		// Plan replacement: publishing a new matrix retires the old version in the same
		// transaction, but its already-generated obligations stay open and keep appearing on
		// operators' lists beside the replacement plan's own work. The per-animal path
		// superseded them; this scan -- the one that actually runs after a publish -- did not.
		//
		// Work already in progress is deliberately left alone: the cancel path covers
		// scheduled, due and deferred only, so an animal being worked right now is never
		// pulled out from under the operator by a publish.
		if err := s.supersedeRetiredPlanWork(ctx, tenantID, activeGoats, effectiveVersionsByPark, asOf); err != nil {
			return res, err
		}
		pagePlans := make([]goatGenerationPlan, 0, len(activeGoats))
		for _, g := range activeGoats {
			if !goatMatchesEligibility(g, elig, policies.Pregnancy, asOf) {
				continue
			}
			if v.ScopeType == "tenant" {
				key := generationParkCacheKey(g.ParkID)
				set := effectiveVersionSets[key]
				if set == nil {
					set = versionIDSet(effectiveVersionsByPark[key])
					effectiveVersionSets[key] = set
				}
				if _, ok := set[versionID]; !ok {
					continue
				}
			}
			pagePlans = append(pagePlans, goatGenerationPlan{
				versionID:      versionID,
				rules:          rules,
				deferState:     elig.DeferStates,
				goat:           g,
				opts:           opts,
				eligibility:    elig,
				policies:       policies,
				vaccineProfile: vaccineProf,
			})
		}
		allPlans = append(allPlans, pagePlans...)
		if opts.heartbeat != nil {
			opts.heartbeat(ctx)
		}
		if int32(len(goats)) < s.page {
			break
		}
		after = goats[len(goats)-1].GoatID
	}
	vaccineHistoryByGoat, err := s.recentVaccineAdminsForPlans(ctx, tenantID, allPlans, businessDayEnd(asOf))
	if err != nil {
		return res, err
	}
	runOpts := opts
	runOpts.campaignDueByGoat, runOpts.cohortAlignedCampaignByGoat, err = campaignDueOverrides(allPlans, asOf, vaccineHistoryByGoat)
	if err != nil {
		return res, err
	}
	for i := range allPlans {
		allPlans[i].opts = runOpts
	}
	trustedByVersion, err := s.trustedEvidenceForPlans(ctx, tenantID, allPlans, asOf, vaccineHistoryByGoat)
	if err != nil {
		return res, err
	}
	for i, p := range allPlans {
		p.opts = runOpts
		if err := s.genOneGoat(ctx, tenantID, p.versionID, p.rules, p.deferState, p.eligibility, p.goat, asOf, p.opts, p.policies, p.vaccineProfile, vaccineHistoryByGoat[p.goat.GoatID], trustedByVersion[p.versionID], &res); err != nil {
			if shouldAbortGeneration(err) {
				return res, err
			}
			recordFailedGenerationGoat(&res, failedGoats, p.goat.GoatID)
			continue
		}
		if runOpts.heartbeat != nil && (i+1)%100 == 0 {
			runOpts.heartbeat(ctx)
		}
	}
	if res.FailedGoats > 0 {
		return res, errGenerationPartialFailures
	}
	return res, nil
}

func shouldAbortGeneration(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return isRetryableGenerationInfraError(err)
}

func isRetryableGenerationInfraError(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "40001", // serialization_failure
			"40P01", // deadlock_detected
			"55P03", // lock_not_available
			"57014", // query_canceled / statement timeout
			"57P01", // admin_shutdown
			"57P02", // crash_shutdown
			"57P03", // cannot_connect_now
			"53300", // too_many_connections
			"53400", // configuration_limit_exceeded
			"08000",
			"08001",
			"08003",
			"08004",
			"08006",
			"08007":
			return true
		default:
			return false
		}
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range []string{
		"failed to acquire connection",
		"timeout acquiring connection",
		"pool closed",
		"connection reset",
		"connection refused",
		"connection is closed",
		"server closed the connection",
		"broken pipe",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

func recordFailedGenerationGoat(res *domain.GenerateResult, seen map[string]struct{}, goatID string) {
	key := strings.TrimSpace(goatID)
	if key == "" {
		res.FailedGoats++
		return
	}
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	res.FailedGoats++
}

func (s *GenerationService) trustedEvidenceForPlans(ctx context.Context, tenantID string, plans []goatGenerationPlan, asOf time.Time, vaccineHistoryByGoat map[string][]domain.RecentVaccineAdministration) (map[string]trustedEvidenceLookup, error) {
	lookups := make(map[string]trustedEvidenceLookup)
	if len(plans) == 0 {
		return lookups, nil
	}
	candidatesByVersion := make(map[string][]domain.TrustedCompletionCandidate)
	seenByVersion := make(map[string]map[string]bool)
	for _, plan := range plans {
		if _, ok := lookups[plan.versionID]; !ok {
			lookups[plan.versionID] = newTrustedEvidenceLookup()
		}
		if _, ok := seenByVersion[plan.versionID]; !ok {
			seenByVersion[plan.versionID] = make(map[string]bool)
		}
		for _, rule := range plan.rules {
			ruleEligibility, _, err := ruleGenerationContext(rule, plan.eligibility, plan.vaccineProfile)
			if err != nil {
				return nil, err
			}
			if !goatMatchesEligibility(plan.goat, ruleEligibility, plan.policies.Pregnancy, asOf) {
				continue
			}
			if !ruleMatchesSchedulePath(rule, schedulePathForGoat(plan.goat, plan.policies.Procurement, asOf, vaccineHistoryByGoat[plan.goat.GoatID])) {
				continue
			}
			due, ok, skip := dueAt(plan.versionID, rule, plan.goat, asOf, plan.opts, plan.policies)
			if skip {
				// A missing DOB/entry date cannot erase accepted history. Non-repeat
				// evidence matching does not use the candidate due date, so include a
				// stable as-of candidate in the page-level batch before the missing-date
				// branch decides whether a blocker is needed.
				if strings.EqualFold(strings.TrimSpace(rule.Repeat), "") || strings.EqualFold(strings.TrimSpace(rule.Repeat), "none") {
					due = asOf
					ok = true
				} else {
					continue
				}
			}
			if !ok {
				continue
			}
			if override, found := plan.opts.campaignDueByGoat[campaignDueGoatKey(plan.versionID, rule.RuleID, plan.goat.GoatID)]; found {
				due = override
			}
			evidenceDue, ok := trustedEvidenceDue(rule, due, asOf, nil, plan.policies.MissedDose)
			if !ok {
				continue
			}
			candidate := trustedCompletionCandidate(rule, plan.goat, evidenceDue)
			key := candidate.Key()
			if seenByVersion[plan.versionID][key] {
				continue
			}
			seenByVersion[plan.versionID][key] = true
			candidatesByVersion[plan.versionID] = append(candidatesByVersion[plan.versionID], candidate)
		}
	}
	if batch, ok := s.evidence.(BatchCompletionEvidenceReader); ok {
		for versionID, candidates := range candidatesByVersion {
			lookup := lookups[versionID]
			for _, chunk := range trustedCompletionCandidateChunks(candidates, 200) {
				hits, err := batch.HasTrustedCompletionEvidenceBatch(ctx, tenantID, versionID, chunk, asOf)
				if err != nil {
					return nil, err
				}
				for _, candidate := range chunk {
					key := candidate.Key()
					lookup.record(key, hits[key])
				}
			}
		}
		return lookups, nil
	}
	for versionID, candidates := range candidatesByVersion {
		lookup := lookups[versionID]
		for _, candidate := range candidates {
			trusted, err := s.evidence.HasTrustedCompletionEvidence(ctx, tenantID, candidate.GoatID, versionID, candidate.RuleID, candidate.DoseCode, candidate.DueAt, asOf)
			if err != nil {
				return nil, err
			}
			lookup.record(candidate.Key(), trusted)
		}
	}
	return lookups, nil
}

func trustedCompletionCandidateChunks(candidates []domain.TrustedCompletionCandidate, size int) [][]domain.TrustedCompletionCandidate {
	if len(candidates) == 0 {
		return nil
	}
	if size <= 0 || size >= len(candidates) {
		return [][]domain.TrustedCompletionCandidate{candidates}
	}
	chunks := make([][]domain.TrustedCompletionCandidate, 0, (len(candidates)+size-1)/size)
	for start := 0; start < len(candidates); start += size {
		end := start + size
		if end > len(candidates) {
			end = len(candidates)
		}
		chunks = append(chunks, candidates[start:end])
	}
	return chunks
}

func (s *GenerationService) hasTrustedCompletionEvidence(ctx context.Context, tenantID, versionID string, rule protodomain.Rule, g domain.EligibleGoat, due, asOf time.Time, trustedLookup trustedEvidenceLookup) (bool, error) {
	candidate := trustedCompletionCandidate(rule, g, due)
	if trusted, checked := trustedLookup.get(candidate.Key()); checked {
		return trusted, nil
	}
	return s.evidence.HasTrustedCompletionEvidence(ctx, tenantID, g.GoatID, versionID, rule.RuleID, rule.DoseCode, due, asOf)
}

func trustedCompletionCandidate(rule protodomain.Rule, g domain.EligibleGoat, due time.Time) domain.TrustedCompletionCandidate {
	return domain.TrustedCompletionCandidate{
		GoatID:   g.GoatID,
		RuleID:   rule.RuleID,
		DoseCode: rule.DoseCode,
		DueAt:    due,
		Repeat:   rule.Repeat,
	}
}

func (s *GenerationService) recentVaccineAdminsForPlans(ctx context.Context, tenantID string, plans []goatGenerationPlan, before time.Time) (map[string][]domain.RecentVaccineAdministration, error) {
	out := make(map[string][]domain.RecentVaccineAdministration)
	if len(plans) == 0 || (s.crossHistory == nil && s.crossVaccineGap == nil) {
		return out, nil
	}
	goatIDs := make([]string, 0, len(plans))
	seen := make(map[string]bool, len(plans))
	for _, plan := range plans {
		if seen[plan.goat.GoatID] {
			continue
		}
		seen[plan.goat.GoatID] = true
		goatIDs = append(goatIDs, plan.goat.GoatID)
	}
	if s.crossHistory != nil {
		return s.crossHistory.RecentVaccineAdministrationsForGoats(ctx, tenantID, goatIDs, before)
	}
	latest, err := s.crossVaccineGap.LastRecentVaccineAdministrationsForGoats(ctx, tenantID, goatIDs, before)
	if err != nil {
		return nil, err
	}
	for goatID, admin := range latest {
		if !admin.AdministeredAt.IsZero() {
			out[goatID] = []domain.RecentVaccineAdministration{admin}
		}
	}
	return out, nil
}

// genOneGoat applies every applicable rule to one goat: compute due_at, idempotent insert, and a
// canonical deferred state when the goat is in a defer state. Accumulates counts into res.
func (s *GenerationService) genOneGoat(ctx context.Context, tenantID, versionID string, rules []protodomain.Rule, deferStates []string, versionEligibility genEligibility, g domain.EligibleGoat, asOf time.Time, opts generationOptions, policies genVersionPolicies, vaccineProf vaccineProfile, vaccineHistory []domain.RecentVaccineAdministration, trustedLookup trustedEvidenceLookup, res *domain.GenerateResult) error {
	historicalCatchUpMaterialized := false
	path := schedulePathForGoat(g, policies.Procurement, asOf, vaccineHistory)
	manualAnchors, err := s.manualVaccineAnchorsForSeedRules(ctx, tenantID, g.GoatID, rules, versionEligibility, vaccineProf)
	if err != nil {
		return err
	}
	// BUG #2: track pending obligations generated in this pass to enforce cross-vaccine spacing among co-due vaccines.
	// Sorted by rule sequence to ensure deterministic spacing order across replays.
	var pending []pendingVaccine
	for _, rule := range rules {
		ruleEligibility, ruleVaccine, err := ruleGenerationContext(rule, versionEligibility, vaccineProf)
		if err != nil {
			return err
		}
		if !goatMatchesEligibility(g, ruleEligibility, policies.Pregnancy, asOf) {
			continue
		}
		if !ruleMatchesSchedulePath(rule, path) {
			continue
		}
		procPurposeDue, procPurposeOK := procurementPurposePrimaryDue(g, rule, ruleVaccine, policies.Procurement)
		if path == schedulePathAdultProcurement && isAdultCampaignRule(rule) &&
			(strings.EqualFold(strings.TrimSpace(rule.TriggerType), "post_arrival") ||
				strings.EqualFold(strings.TrimSpace(rule.TriggerType), "manual_campaign")) &&
			!procPurposeOK {
			continue
		}
		// History outranks DOB/arrival, per DOSE: once THIS rule's own dose has been administered, it
		// must not be regenerated — even AFTER a DOB/entry-date correction makes dueAt resolvable — so
		// a later identity correction can never replace, duplicate, or replay that already-given dose.
		// Match the SPECIFIC dose (not any dose of the vaccine): a still-required later dose of the
		// same course (ET+TT week-7 booster, adult wave two) has no matching administration yet and
		// must still be scheduled from its own anchor (VACC-REV-06).
		if isPrimaryCourseRule(rule) && hasSameDoseAdministration(rule, ruleVaccine, vaccineHistory) {
			res.SuppressedByTrustedHistory++
			// A normal adult blank-history campaign uses a stable key so a later drive-date
			// recalculation cannot duplicate it. If accepted medical history arrives on a later
			// verification pass, that same stable row must be retired before the repeat course is
			// generated; verifier/director delay never makes the already-administered primary due.
			if stableKey := stableAdultCampaignObligationKey(tenantID, versionID, rule, g); stableKey != "" {
				if _, _, err := s.obl.CancelOpenObligationByIdempotencyKey(ctx, tenantID, stableKey, "vaccine_history_outranks_adult_campaign", asOf); err != nil {
					return err
				}
			}
			// Supersede any stale no-anchor catch-up placeholder for THIS dose from an earlier pass so
			// history + a later DOB fix never leave a duplicate of the already-given dose behind.
			if staleKey := missingDueDateKey(tenantID, versionID, rule, g, anchorMissingReason(rule.TriggerType)); staleKey != "" {
				if _, _, err := s.obl.CancelOpenObligationByIdempotencyKey(ctx, tenantID, staleKey, "vaccine_history_outranks_anchor", asOf); err != nil {
					return err
				}
			}
			continue
		}
		anchorCatchUpKey := ""
		// Set only on the history-driven repeat path below, and only for repeat rules.
		var historyAnchor *obldomain.RepeatCycleSource
		baseDue, ok, skip := time.Time{}, false, false
		if courseDue, found, err := primaryCourseContinuationDueFromHistory(rule, ruleVaccine, rules, versionEligibility, vaccineProf, path, vaccineHistory); err != nil {
			return err
		} else if found {
			baseDue = courseDue
			ok = true
		} else {
			baseDue, ok, skip = dueAt(versionID, rule, g, asOf, opts, policies)
		}
		if ok && !procPurposeDue.IsZero() {
			baseDue = procPurposeDue
		}
		if skip {
			trusted, err := s.hasTrustedCompletionEvidence(ctx, tenantID, versionID, rule, g, asOf, asOf, trustedLookup)
			if err != nil {
				return err
			}
			if trusted {
				res.SuppressedByTrustedHistory++
				continue
			}
			// B2 (anchor fallback): a missing DOB/entry-date anchor must never
			// produce a missing_due_date deferral when the goat already carries an
			// accepted administration of this rule's own vaccine. Per-vaccine
			// continuation (B1) already owns this vaccine's next due date via its
			// own after_previous_completion/revac rule elsewhere in this same rule
			// set, computed straight from vaccineHistory — never by reverse-
			// engineering a DOB. Suppress this specific primary/wave rule instance
			// instead of flagging a fabricated blocker.
			// R2-03: For rules on the adult procurement path, respect the AdultPriorVaccinationAllowed flag:
			// when false in an active procurement policy, adult prior vaccination history must NOT suppress.
			// Default (when policy not active) is to allow adult prior vaccination for backward compat.
			if hasVaccineAdministrationHistory(ruleVaccine, vaccineHistory) {
				shouldSuppressByHistory := true
				if path == schedulePathAdultProcurement && policies.Procurement.active() &&
					!policies.Procurement.adultPriorAllowed() {
					// Adult prior vaccination is disabled: schedule the full course
					// as if this vaccine has no prior history.
					shouldSuppressByHistory = false
				}
				if shouldSuppressByHistory {
					res.SuppressedByTrustedHistory++
					// B7 (dynamic recompute): imported vaccination history triggers
					// recomputation even when DOB/entry-date is STILL unknown. A B3
					// no-anchor catch-up placeholder created on an earlier pass (before
					// this vaccine's history existed) is now obsolete — supersede it here
					// too, not only when the anchor itself later resolves (the
					// missingKey/previousMissingDueDateKey path below only fires once
					// DOB/entry-date becomes known, which is a DIFFERENT recompute
					// trigger from "history just got imported").
					if staleKey := missingDueDateKey(tenantID, versionID, rule, g, anchorMissingReason(rule.TriggerType)); staleKey != "" {
						if _, _, err := s.obl.CancelOpenObligationByIdempotencyKey(ctx, tenantID, staleKey, "vaccine_history_now_available", asOf); err != nil {
							return err
						}
					}
					continue
				}
			}
			switch rule.TriggerType {
			case "birth_age", "post_arrival":
				// Contract §89: anchoring is per VACCINE FAMILY, never goat-wide. This branch is only
				// reached when hasVaccineAdministrationHistory (above) already found this rule's own
				// vaccine family blank — a dose recorded in an UNRELATED family (ET+TT, PPR, ...) is not
				// proof this family was ever administered, so it must not suppress this family's catch-up
				// (VAX-REV-02). Fall through to B3 exactly as a zero-history goat would.
				// B3 (never-received vaccine + no anchor): route to the adult
				// catch-up/primary path at the next compatible drive instead of
				// deferring merely because DOB/entry-date is unknown. due=asOf lets
				// the normal missed-dose, nearby-drive, and cross-vaccine-gap
				// machinery below place it correctly — subject to the same
				// ≤2-per-visit / live-spacing / health-pregnancy gates as any other
				// obligation — never a fabricated kid_12w/kid_16w deferral.
				//
				// The materialized obligation uses the SAME stable (date-independent)
				// idempotency key the legacy missing_due_date placeholder used, so a
				// repeated generation pass (asOf advancing daily while the anchor is
				// still unknown) stays idempotent (no duplicate catch-up obligation
				// piles up), and — B7 — the existing missingKey cancellation below
				// automatically supersedes this placeholder the moment DOB/entry-date
				// is backfilled and dueAt starts succeeding normally for this rule.
				baseDue = businessDayStart(asOf)
				ok = true
				anchorCatchUpKey = missingDueDateKey(tenantID, versionID, rule, g, anchorMissingReason(rule.TriggerType))
			default:
				res.SkippedNoDueDate++
				if err := s.genMissingDueDateObligation(ctx, tenantID, versionID, rule, ruleVaccine.Code, g, asOf, res); err != nil {
					return err
				}
				continue
			}
		}
		if !ok {
			// R2-03: For rules on the adult procurement path, respect the AdultPriorVaccinationAllowed flag:
			// when false in an active procurement policy, do not use adult prior vaccination history.
			// Default (when policy not active) is to allow adult prior vaccination for backward compat.
			allowHistoryDue := true
			if path == schedulePathAdultProcurement && policies.Procurement.active() &&
				!policies.Procurement.adultPriorAllowed() {
				allowHistoryDue = false
			}
			if allowHistoryDue {
				wait, err := repeatMustWaitForPrimaryCourse(rule, ruleVaccine, rules, versionEligibility, vaccineProf, path, vaccineHistory)
				if err != nil {
					return err
				}
				if wait {
					continue
				}
				if historyDue, found := dueAfterPreviousCompletion(rule, ruleVaccine, vaccineHistory); found {
					baseDue = historyDue
					ok = true
					// This due date was derived from a specific past administration, and it
					// moves whenever a newer administration of the same vaccine lands. Record
					// the administration itself as the cycle's cause so the moved date updates
					// the existing row instead of minting a second one beside it.
					if latest, hasLatest := latestVaccineAdministration(rule, strings.TrimSpace(ruleVaccine.Code), vaccineHistory); hasLatest && isRepeatRule(&rule) {
						historyAnchor = historyRepeatCycle(latest)
					}
				}
			}
		}
		if !ok {
			continue // after_previous_completion → SM-7, manual_campaign → manual
		}
		if manualAnchorSuppressesSeedRule(rule) {
			if _, found := manualAnchors[vaccineAnchorLookupKey(ruleVaccine.Code)]; found {
				res.SuppressedByTrustedHistory++
				continue
			}
		}
		supersededCampaignKey := ""
		campaignKey := campaignDueGoatKey(versionID, rule.RuleID, g.GoatID)
		cohortCampaignRealignment := false
		if override, found := opts.campaignDueByGoat[campaignKey]; found {
			cohortCampaignRealignment = opts.cohortAlignedCampaignByGoat[campaignKey] && isStableAdultCampaignObligationKey(rule, g)
			if !baseDue.Equal(override) && !isStableAdultCampaignObligationKey(rule, g) {
				legacyBaseDue := baseDue
				if isAdultCampaignRule(rule) && hasPhysicalCampaignPartition(g) && hasVaccineAdministrationHistory(ruleVaccine, vaccineHistory) {
					campaignStart := adultCampaignStart(asOf)
					if legacyBaseDue.Before(campaignStart) {
						legacyBaseDue = campaignStart
					}
				}
				supersededCampaignKey = obligationKey(
					tenantID,
					versionID,
					rule.RuleID,
					"goat",
					g.GoatID,
					obligationKeyDue(rule, legacyBaseDue, legacyBaseDue).UTC().Format(time.RFC3339),
					strconv.Itoa(int(rule.Sequence)),
				)
			}
			baseDue = override
		}
		if isAdultCampaignRule(rule) && hasPhysicalCampaignPartition(g) && hasVaccineAdministrationHistory(ruleVaccine, vaccineHistory) {
			campaignStart := adultCampaignStart(asOf)
			if baseDue.Before(campaignStart) {
				baseDue = campaignStart
			}
		}
		nearbyMissedDrive, err := s.nearbyMissedDoseDriveDate(ctx, tenantID, versionID, rule, ruleVaccine, g, baseDue, asOf, policies.MissedDose)
		if err != nil {
			return err
		}
		evidenceDue, ok := trustedEvidenceDue(rule, baseDue, asOf, nearbyMissedDrive, policies.MissedDose)
		if !ok {
			continue
		}
		trusted, err := s.hasTrustedCompletionEvidence(ctx, tenantID, versionID, rule, g, evidenceDue, asOf, trustedLookup)
		if err != nil {
			return err
		}
		if trusted {
			res.SuppressedByTrustedHistory++
			continue
		}
		due, catchUpDeferReason, skip := applyMissedDosePolicy(rule, baseDue, asOf, nearbyMissedDrive, policies.MissedDose)
		if skip {
			continue
		}
		if s.crossHistory != nil || s.crossVaccineGap != nil {
			due = applyCrossVaccineGapFloorFromHistory(due, vaccineHistory, ruleVaccine, policies.Compatibility)
		}
		// BUG #2: also floor against incompatible pending vaccines in this pass. This ensures that
		// two co-due live vaccines (e.g., PPR and Goat Pox both due today) are spaced LiveToLiveGapDays
		// apart, not left same-day because neither is in the other's history yet.
		due = applyCrossVaccineGapFloorFromPending(due, ruleVaccine, pending, policies.Compatibility)
		if skipNonFutureOpenWork(rule, due, asOf, policies.MissedDose) {
			continue
		}
		if limitsHistoricalCatchUp(rule, baseDue, due, asOf, opts) {
			// Contract §89: an anchored historical catch-up (its due window has already elapsed) for a
			// blank family (this rule's own vaccine has no accepted history — hasSameDoseAdministration
			// and hasVaccineAdministrationHistory above already suppress a family WITH its own history)
			// is treated exactly like a zero-history goat's catch-up: a dose recorded in an unrelated
			// vaccine family is not proof this family was ever administered, so it is not suppressed here
			// (VAX-REV-02). Still throttled to one materialized historical catch-up per goat per pass.
			if historicalCatchUpMaterialized {
				continue
			}
			historicalCatchUpMaterialized = true
		}
		// Scope goat vaccination obligations to the animal's real shed. A park drive is an execution
		// batch made by the sweeper; it is not a valid fallback scope for animal due work.
		scopeType, scopeID, err := generationScope(tenantID, g)
		if err != nil {
			return err
		}
		keyDue := obligationKeyDue(rule, baseDue, due)
		keyDueToken := keyDue.UTC().Format(time.RFC3339)
		if isStableAdultCampaignObligationKey(rule, g) {
			keyDueToken = "adult_campaign"
		}
		key := obligationKey(tenantID, versionID, rule.RuleID, "goat", g.GoatID, keyDueToken, strconv.Itoa(int(rule.Sequence)))
		if anchorCatchUpKey != "" {
			key = anchorCatchUpKey
		}
		missingKey := previousMissingDueDateKey(tenantID, versionID, rule, g)
		status := "scheduled"
		rowDeferStates := ruleEligibility.DeferStates
		if len(rowDeferStates) == 0 {
			rowDeferStates = deferStates
		}
		deferReason := deferredReason(g, rowDeferStates)
		if deferReason == "" {
			deferReason = policyDeferReason(g, policies, asOf)
		}
		if deferReason == "" {
			deferReason = catchUpDeferReason
		}
		deferred := deferReason != ""
		if deferred {
			status = "deferred"
		}
		newObligation := obldomain.NewObligation{
			TenantID:          tenantID,
			ProtocolVersionID: versionID,
			RuleID:            rule.RuleID,
			TargetType:        "goat",
			TargetID:          g.GoatID,
			ScopeType:         scopeType,
			ScopeID:           scopeID,
			DueAt:             due,
			WindowEnd:         obligationWindowEnd(rule, due),
			Status:            status,
			IdempotencyKey:    key,
			// The rule's business identity, from the same helper the publisher writes to
			// protocol_rule_lineage. This is what makes the animal's existing work findable
			// across a publish: the version and rule UUIDs change, the identity does not.
			RuleIdentityKey: protodomain.RuleIdentityKey(ruleVaccine.Code, rule.DoseCode, rule.Sequence),
			Sequence:        rule.Sequence,
			RepeatCycle:     historyAnchor,
		}
		if historyAnchor != nil {
			finalDue := due
			historyAnchor.DueAt = &finalDue
		}
		// Reconcile BEFORE inserting. The animal may already owe this rule under an older
		// version, and the date it owes it can move even when the rule's content did not --
		// because the due date is computed from the ANIMAL's history too. Inserting in that case
		// books the same dose twice, which is worse than the churn carry-over removes. So the
		// existing row is moved to the new date and version, keeping its obligation_id and
		// everything attached to it, and no second row is written.
		var applied bool
		reconciled, found, err := s.obl.ReconcileOpenObligationForRuleIdentity(ctx, tenantID, newObligation, asOf)
		if err != nil {
			// An animal already holding two unlabelled open obligations for this rule is a data
			// problem a generation pass must not paper over: picking one cancels a scheduled
			// vaccination on a guess, inserting books a third. It fails for THIS animal, carrying
			// the obligation ids a human needs, and the rest of the run continues.
			if errors.Is(err, oblports.ErrAmbiguousOpenWork) {
				res.AmbiguousOpenWork++
			}
			return err
		}
		if found {
			// Same work, still open, now pointing at the current version. Nothing was generated,
			// so it does not count as new; the persisted state is already what generation wanted.
			res.Reconciled++
			if reconciled.DateBlocked {
				res.ReconcileDateBlocked++
			}
		} else if deferred {
			_, applied, err = s.obl.InsertDeferredObligation(ctx, newObligation, deferReason, asOf)
		} else {
			_, applied, err = s.obl.InsertObligation(ctx, newObligation)
		}
		if err != nil {
			return err
		}
		if supersededCampaignKey != "" && supersededCampaignKey != key {
			if _, _, err := s.obl.CancelOpenObligationByIdempotencyKey(ctx, tenantID, supersededCampaignKey, "adult_campaign_date_realigned", asOf); err != nil {
				return err
			}
		}
		if !applied {
			// R2-01: reconcile this obligation's persisted state FIRST (missing-key cancel, then
			// defer or recovery-reopen), and only THEN append it to `pending` for co-due cross-vaccine
			// spacing -- using the FINAL persisted due_at and skipping TERMINAL obligations. The prior
			// code appended before reconciling and regardless of status, so a waived/superseded/
			// completed obligation (no remaining shot) still delayed an unrelated co-due vaccine, and a
			// recovery-reschedule that ran afterwards left the stale date in `pending`.
			var finalRef obldomain.ObligationRef
			var changed bool
			var reschedule *obldomain.RecoveryReschedule
			// Which row this pass is actually reconciling.
			//
			// Everything below is keyed, and the key contains the due date. A repeat cycle
			// whose due date moved is suppressed by its CAUSE, so the surviving row sits under
			// the PREVIOUS key and every call here would miss it: the duplicate is gone, but
			// so is the move -- the row keeps a stale date, is never deferred for a sick
			// animal, never reopened for a recovered one, and contributes nothing to
			// cross-vaccine spacing. Reconciling the row we actually suppressed against fixes
			// that; it also means the new due date reaches the surviving row, which is the
			// only way a moved date surfaces at all now that a second row is impossible.
			reconcileKey := key
			// A reconciled obligation may hold a DIFFERENT key than the one just computed: when
			// its date could not move, the row keeps whatever address it could take. Addressing it
			// by the wished-for key would look up nothing and fail the animal, so follow-ups use
			// the key the row actually holds.
			if reconciledKey := strings.TrimSpace(reconciled.IdempotencyKey); found && reconciledKey != "" {
				reconcileKey = reconciledKey
			}
			if historyAnchor.Valid() {
				survivor, found, err := s.obl.OpenObligationForRepeatCycle(
					ctx, tenantID, versionID, rule.RuleID, "goat", g.GoatID, rule.Sequence, historyAnchor.SourceRef,
				)
				if err != nil {
					return err
				}
				if found && strings.TrimSpace(survivor.IdempotencyKey) != "" && survivor.IdempotencyKey != key {
					reconcileKey = survivor.IdempotencyKey
					if !deferred && !survivor.DueAt.Equal(due) {
						// A taken date is not a failure. The duplicate guard spans every status,
						// so the recomputed date can already hold a dose that was given, or a
						// canceled row -- and the survivor simply stays where it is rather than
						// poisoning this animal for every later pass as well.
						_, _, err := s.obl.RealignOpenObligationForGeneration(ctx, tenantID, reconcileKey, due, newObligation.WindowEnd, asOf)
						if err != nil && !errors.Is(err, oblports.ErrDueDateTaken) {
							return err
						}
					}
				}
			}
			if missingKey != "" && missingKey != key && missingKey != reconcileKey {
				if _, _, err := s.obl.CancelOpenObligationByIdempotencyKey(ctx, tenantID, missingKey, "missing_due_date_resolved", asOf); err != nil {
					return err
				}
			}
			if cohortCampaignRealignment && !deferred {
				// Keyed on `key`, not the repeat-cycle reconcile key: campaign rows never carry
				// repeat metadata, so the two are the same value here, and the campaign contract
				// guard reads this call literally.
				if _, _, err := s.obl.RealignOpenObligationForGeneration(ctx, tenantID, key, due, newObligation.WindowEnd, asOf); err != nil {
					return err
				}
			}
			if deferred {
				finalRef, changed, err = s.obl.DeferOpenObligationForGeneration(ctx, tenantID, reconcileKey, deferReason, asOf)
				if err != nil {
					return err
				}
				if changed {
					res.Deferred++
				}
			} else {
				// Goat is no longer in a defer state: if a held (deferred) obligation exists for this
				// key, reopen it so the recovered goat's due work becomes schedulable again. A no-op
				// when the row is already schedulable/terminal (safe recovery-recheck replay).
				if found && actionableObligationForSpacing(reconciled) &&
					!strings.EqualFold(strings.TrimSpace(reconciled.Status), "deferred") {
					finalRef = reconciled
				} else {
					if opts.healthRecoveryAlign {
						reschedule, err = s.recoveryRescheduleForRule(ctx, tenantID, versionID, rule, ruleVaccine, g, asOf, policies.Recovery, policies.Compatibility, vaccineHistory)
						if err != nil {
							return err
						}
					}
					finalRef, changed, err = s.obl.ReopenDeferredObligationForGeneration(ctx, tenantID, reconcileKey, asOf, reschedule)
					if err != nil {
						return err
					}
				}
				if changed {
					res.Reopened++
				}
			}
			// Only the state returned by the reconciliation transaction is authoritative. In
			// particular, `reschedule` is merely a proposal and must not space another vaccine when a
			// terminal obligation made reopening a no-op.
			if actionableObligationForSpacing(finalRef) {
				pending = append(pending, pendingVaccine{code: ruleVaccine.Code, class: ruleVaccine.Class, due: finalRef.DueAt})
			} else if strings.EqualFold(strings.TrimSpace(finalRef.Status), "canceled") &&
				cancelReasonMintsSuccessor(finalRef.Reason) {
				// Returning-goat successor: the goat requalified for this version after its prior
				// row was canceled by a shift-away. Minted with the goat's CURRENT clinical status
				// (deferred=true → deferred successor, LIFE-001), preserving the canceled predecessor.
				// Terminal/history cancellations never reach here (see cancelReasonMintsSuccessor).
				successorRef, successorChanged, successorApplied, err := s.insertSuccessorForCanceledGenerationReplay(ctx, tenantID, key, newObligation, asOf, deferred, deferReason)
				if err != nil {
					return err
				}
				if successorApplied {
					res.Generated++
					if deferred {
						res.Deferred++
					}
				}
				if successorChanged {
					res.Reopened++
				}
				if actionableObligationForSpacing(successorRef) {
					pending = append(pending, pendingVaccine{code: ruleVaccine.Code, class: ruleVaccine.Class, due: successorRef.DueAt})
				}
			}
			continue // replay no-op
		}
		res.Generated++
		// BUG #2: track this newly generated obligation so subsequent vaccines in this pass respect cross-vaccine spacing.
		pending = append(pending, pendingVaccine{
			code:  ruleVaccine.Code,
			class: ruleVaccine.Class,
			due:   due,
		})
		if missingKey != "" {
			if _, _, err := s.obl.CancelOpenObligationByIdempotencyKey(ctx, tenantID, missingKey, "missing_due_date_resolved", asOf); err != nil {
				return err
			}
		}

		if deferred {
			res.Deferred++
		}
	}
	return nil
}

func (s *GenerationService) insertSuccessorForCanceledGenerationReplay(ctx context.Context, tenantID, baseKey string, base obldomain.NewObligation, asOf time.Time, deferred bool, deferReason string) (obldomain.ObligationRef, bool, bool, error) {
	// LIFE-001 fix: successor status must reflect the newly calculated clinical status.
	// A requalified sick/ICU goat must get a fresh DEFERRED successor, not forced to scheduled.
	successorStatus := "scheduled"
	if deferred {
		successorStatus = "deferred"
	}
	base.Status = successorStatus
	if deferred && deferReason == "" {
		deferReason = "defer_state"
	}
	// R50-011: Find the next free successor suffix in one bounded query (max 2000 attempts).
	// Never O(N) round trips for large collision histories.
	nextSuffix, err := s.obl.NextSuccessorSuffix(ctx, tenantID, baseKey)
	if err != nil {
		return obldomain.ObligationRef{}, false, false, fmt.Errorf("get next successor suffix: %w", err)
	}

	for attempt := nextSuffix; attempt < nextSuffix+100; attempt++ {
		successor := base
		successor.IdempotencyKey = fmt.Sprintf("%s:successor:%02d", baseKey, attempt)
		var obID string
		var applied bool
		var err error
		if deferred {
			obID, applied, err = s.obl.InsertDeferredObligation(ctx, successor, deferReason, asOf)
		} else {
			obID, applied, err = s.obl.InsertObligation(ctx, successor)
		}
		if err != nil {
			return obldomain.ObligationRef{}, false, false, err
		}
		if applied {
			return obldomain.ObligationRef{ObligationID: obID, Status: successor.Status, DueAt: successor.DueAt, Reason: ""}, false, true, nil
		}
		// Replay: reconcile the existing successor row with the goat's CURRENT clinical status.
		// A still-held goat must keep (or re-enter) deferred — reopening here would wrongly
		// schedule work for a sick/ICU goat.
		//
		// WHICH row, though. The insert is refused for two different reasons and only one of
		// them is a key collision: a repeat cycle is refused by its CAUSE, so the row already
		// holding it can sit under an entirely different key. Reconciling the key we just
		// computed then reconciles nothing, and the held animal's existing repeat dose stays
		// SCHEDULED -- work an operator sees as due for an animal that is sick.
		//
		// Defensive, honestly labelled: the caller resolves the same way before it ever gets
		// here, so no test input reaches this branch today. It is kept because the two paths
		// must not disagree about what a refusal means -- the last time they did, that
		// disagreement was the bug.
		reconcileKey := successor.IdempotencyKey
		if base.RepeatCycle.Valid() {
			survivor, found, lookupErr := s.obl.OpenObligationForRepeatCycle(
				ctx, tenantID, base.ProtocolVersionID, base.RuleID, base.TargetType, base.TargetID,
				base.Sequence, base.RepeatCycle.SourceRef,
			)
			if lookupErr != nil {
				return obldomain.ObligationRef{}, false, false, lookupErr
			}
			if found && strings.TrimSpace(survivor.IdempotencyKey) != "" {
				reconcileKey = survivor.IdempotencyKey
			}
		}
		var ref obldomain.ObligationRef
		var changed bool
		if deferred {
			ref, changed, err = s.obl.DeferOpenObligationForGeneration(ctx, tenantID, reconcileKey, deferReason, asOf)
		} else {
			ref, changed, err = s.obl.ReopenDeferredObligationForGeneration(ctx, tenantID, reconcileKey, asOf, nil)
		}
		if err != nil {
			return obldomain.ObligationRef{}, false, false, err
		}
		if actionableObligationForSpacing(ref) {
			return ref, changed, false, nil
		}
		if !strings.EqualFold(strings.TrimSpace(ref.Status), "canceled") && strings.TrimSpace(ref.Status) != "" {
			return ref, changed, false, nil
		}
		// Refused, and no row under this key: for a repeat cycle that means another writer
		// already holds this cause -- a booster-minted successor, or a rescheduled missed
		// dose. Every remaining suffix would be refused by the same guard, so trying 99 more
		// only burns round trips and then fails the whole generation pass with a suffix
		// error that names the wrong problem. The cycle exists; return it.
		if strings.TrimSpace(ref.Status) == "" && base.RepeatCycle.Valid() {
			existing, found, err := s.obl.OpenObligationForRepeatCycle(
				ctx, tenantID, base.ProtocolVersionID, base.RuleID, base.TargetType, base.TargetID,
				base.Sequence, base.RepeatCycle.SourceRef,
			)
			if err != nil {
				return obldomain.ObligationRef{}, false, false, err
			}
			if found {
				return existing, false, false, nil
			}
		}
	}
	return obldomain.ObligationRef{}, false, false, fmt.Errorf("exhausted successor suffix attempts (max 100 from computed next=%d)", nextSuffix)
}

func limitsHistoricalCatchUp(rule protodomain.Rule, baseDue, materializedDue, asOf time.Time, opts generationOptions) bool {
	if opts.ManualCampaignID != "" || rule.TriggerType == "manual_campaign" {
		return false
	}
	if rule.DueWindowDays <= 0 {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(rule.CatchUp), "next_cycle") {
		return false
	}
	windowEnd := businessDayStart(baseDue).AddDate(0, 0, int(rule.DueWindowDays))
	return asOf.After(windowEnd) && materializedDue.Equal(asOf)
}

func (s *GenerationService) genMissingDueDateObligation(ctx context.Context, tenantID, versionID string, rule protodomain.Rule, vaccineCode string, g domain.EligibleGoat, asOf time.Time, res *domain.GenerateResult) error {
	reason := missingDueDateReason(rule, g)
	if reason == "" {
		reason = "missing_due_date"
	}
	scopeType, scopeID, err := generationScope(tenantID, g)
	if err != nil {
		return err
	}
	key := missingDueDateKey(tenantID, versionID, rule, g, reason)
	obID, applied, err := s.obl.InsertObligation(ctx, obldomain.NewObligation{
		TenantID:          tenantID,
		ProtocolVersionID: versionID,
		RuleID:            rule.RuleID,
		// Same identity the scheduled path writes. A placeholder row for a missing anchor is still
		// this rule's work for this animal, and a row without an identity sits outside the
		// one-open-obligation-per-identity index -- which is where duplicates come from.
		RuleIdentityKey: protodomain.RuleIdentityKey(vaccineCode, rule.DoseCode, rule.Sequence),
		TargetType:      "goat",
		TargetID:        g.GoatID,
		ScopeType:       scopeType,
		ScopeID:         scopeID,
		DueAt:           asOf,
		Status:          "deferred",
		IdempotencyKey:  key,
		Sequence:        rule.Sequence,
	})
	if err != nil {
		return err
	}
	if !applied {
		return nil
	}
	res.Generated++
	res.Deferred++
	payload, _ := json.Marshal(map[string]string{
		"reason":       "missing_due_date",
		"missing":      reason,
		"trigger_type": rule.TriggerType,
	})
	if _, _, err := s.obl.RecordStatusEvent(ctx, obldomain.NewStatusEvent{
		TenantID:       tenantID,
		ObligationID:   obID,
		EventType:      "deferred",
		OccurredAt:     asOf,
		Payload:        payload,
		IdempotencyKey: obID + ":deferred:missing_due_date:" + asOf.UTC().Format(time.RFC3339Nano),
		Scope:          "obligation.status_event",
		RequestHash:    "missing_due_date:" + reason,
	}); err != nil {
		return err
	}
	return nil
}

func missingDueDateKey(tenantID, versionID string, rule protodomain.Rule, g domain.EligibleGoat, reason string) string {
	if reason == "" {
		reason = "missing_due_date"
	}
	return obligationKey(tenantID, versionID, rule.RuleID, "goat", g.GoatID, "missing_due_date", reason, strconv.Itoa(int(rule.Sequence)))
}

// AnchorMissingCatchUpKey returns the durable idempotency key genOneGoat stamps on a Contract §89
// option-4 adult catch-up obligation — the one materialized for a blank vaccine family whose
// birth_age/post_arrival anchor (DOB / herd-entry date) is unavailable (genOneGoat's B3 branch sets
// anchorCatchUpKey = missingDueDateKey(..., anchorMissingReason(triggerType))). It exists so seed
// reconciliation can tell a contract-legitimate routed catch-up (§89 option 4 — "missing identity
// dates alone are not a clinical defer reason", schedule at the next compatible drive) apart from a
// fabricated NORMAL missing-anchor due (the defect §13 forbids). This exported facade keeps the key
// derivation single-sourced with the generator, so the reconciliation classifier can never drift from
// the exact bytes genOneGoat hashed. Inputs must be the same canonical (lowercase-uuid) string forms
// the generator used; feed the raw uuid::text columns straight from obligation_instances.
func AnchorMissingCatchUpKey(tenantID, versionID, ruleID, goatID, triggerType string, sequence int32) string {
	return obligationKey(tenantID, versionID, ruleID, "goat", goatID, "missing_due_date", anchorMissingReason(triggerType), strconv.Itoa(int(sequence)))
}

func previousMissingDueDateKey(tenantID, versionID string, rule protodomain.Rule, g domain.EligibleGoat) string {
	switch rule.TriggerType {
	case "birth_age":
		if g.DOB != nil {
			return missingDueDateKey(tenantID, versionID, rule, g, "missing_dob")
		}
	case "post_arrival":
		if warmingEntryAt(g) != nil {
			return missingDueDateKey(tenantID, versionID, rule, g, "missing_entry_date")
		}
	}
	return ""
}

// GenerateForGoat generates obligations for ONE goat across all published vaccination versions it
// is eligible for. Used by the goat.created handler (event-driven SM-1). Idempotent.
func (s *GenerationService) GenerateForGoat(ctx context.Context, tenantID, goatID string, asOf time.Time) (domain.GenerateResult, error) {
	return s.generateForGoat(ctx, tenantID, goatID, asOf, generationOptions{})
}

func (s *GenerationService) generateForGoat(ctx context.Context, tenantID, goatID string, asOf time.Time, opts generationOptions) (domain.GenerateResult, error) {
	var res domain.GenerateResult
	if err := s.requireEvidenceReader(); err != nil {
		return res, err
	}
	g, found, err := s.goats.GetGoatForGeneration(ctx, tenantID, goatID)
	if err != nil {
		return res, err
	}
	if !found || !inCare(g.LifecycleStatus) {
		return res, nil // exited/unknown goats get no obligations
	}
	versionIDs, err := s.proto.ListEffectiveVaccinationVersionsForGoat(ctx, tenantID, g.ParkID, asOf)
	if err != nil {
		return res, err
	}
	if _, err := s.obl.CancelOpenVaccinationObligationsForGoatExceptVersions(ctx, tenantID, goatID, versionIDs, "version_no_longer_effective_after_recheck", asOf); err != nil {
		return res, err
	}
	pagePlans := make([]goatGenerationPlan, 0, len(versionIDs))
	plans := make(map[string]cachedVersionPlan, len(versionIDs))
	if err := s.loadVersionPlans(ctx, tenantID, versionIDs, plans); err != nil {
		return res, err
	}
	for _, versionID := range versionIDs {
		p := plans[versionID]
		if !goatMatchesEligibility(g, p.eligibility, p.policies.Pregnancy, asOf) {
			if _, err := s.obl.CancelOpenVaccinationObligationsForGoatVersion(ctx, tenantID, goatID, versionID, "ineligible_after_shift", asOf); err != nil {
				return res, err
			}
			continue
		}
		pagePlans = append(pagePlans, goatGenerationPlan{
			versionID:      versionID,
			rules:          p.rules,
			deferState:     p.deferState,
			goat:           g,
			opts:           opts,
			eligibility:    p.eligibility,
			policies:       p.policies,
			vaccineProfile: p.vaccineProfile,
		})
	}
	vaccineHistoryByGoat, err := s.recentVaccineAdminsForPlans(ctx, tenantID, pagePlans, businessDayEnd(asOf))
	if err != nil {
		return res, err
	}
	runOpts := opts
	runOpts.campaignDueByGoat, runOpts.cohortAlignedCampaignByGoat, err = campaignDueOverrides(pagePlans, asOf, vaccineHistoryByGoat)
	if err != nil {
		return res, err
	}
	for i := range pagePlans {
		pagePlans[i].opts = runOpts
	}
	trustedByVersion, err := s.trustedEvidenceForPlans(ctx, tenantID, pagePlans, asOf, vaccineHistoryByGoat)
	if err != nil {
		return res, err
	}
	for _, p := range pagePlans {
		if err := s.genOneGoat(ctx, tenantID, p.versionID, p.rules, p.deferState, p.eligibility, p.goat, asOf, p.opts, p.policies, p.vaccineProfile, vaccineHistoryByGoat[p.goat.GoatID], trustedByVersion[p.versionID], &res); err != nil {
			return res, err
		}
	}
	return res, nil
}

func (s *GenerationService) recoveryRescheduleForRule(ctx context.Context, tenantID, versionID string, rule protodomain.Rule, ruleVaccine vaccineProfile, g domain.EligibleGoat, asOf time.Time, recovery genRecoveryPolicy, compatibility genCompatibilityPolicy, vaccineHistory []domain.RecentVaccineAdministration) (*obldomain.RecoveryReschedule, error) {
	from := businessDayStart(asOf)
	to := from.AddDate(0, 0, int(recovery.alignDays()))
	nearby, err := s.obl.FindNearestPlannedBatchDate(ctx, tenantID, versionID, rule.RuleID, ruleVaccine.Code, g.ShedID, g.ParkID, from, to)
	if err != nil {
		return nil, err
	}
	due, reason := recoveryRescheduleDue(asOf, recovery, nearby)
	due = applyCrossVaccineGapFloorFromHistory(due, vaccineHistory, ruleVaccine, compatibility)
	windowStart, windowEnd := recoveryDueWindows(due, rule.DueWindowDays)
	return &obldomain.RecoveryReschedule{
		DueAt:       due,
		WindowStart: windowStart,
		WindowEnd:   windowEnd,
		AlignReason: reason,
	}, nil
}

func (s *GenerationService) nearbyMissedDoseDriveDate(ctx context.Context, tenantID, versionID string, rule protodomain.Rule, ruleVaccine vaccineProfile, g domain.EligibleGoat, due, asOf time.Time, policy genMissedDosePolicy) (*time.Time, error) {
	if rule.DueWindowDays <= 0 {
		return nil, nil
	}
	windowEnd := businessDayStart(due).AddDate(0, 0, int(rule.DueWindowDays))
	if !asOf.After(windowEnd) {
		return nil, nil
	}
	switch strings.ToLower(strings.TrimSpace(rule.CatchUp)) {
	case "", "immediate":
	default:
		return nil, nil
	}
	from := businessDayStart(asOf)
	to := from.AddDate(0, 0, int(policy.alignDays()))
	return s.obl.FindNearestPlannedBatchDate(ctx, tenantID, versionID, rule.RuleID, ruleVaccine.Code, g.ShedID, g.ParkID, from, to)
}

func inCare(lifecycle string) bool {
	switch lifecycle {
	case "alive", "sick", "under_treatment", "quarantine", "icu":
		return true
	default:
		return false
	}
}

func generationScope(_ string, g domain.EligibleGoat) (string, string, error) {
	if strings.TrimSpace(g.ParkID) == "" || strings.TrimSpace(g.ShedID) == "" {
		return "", "", fmt.Errorf("vaccination: active goat %s missing required park/shed placement; seed/import must assign every in-care animal to a real shed before vaccination generation", g.GoatID)
	}
	return "shed", g.ShedID, nil
}

func missingDueDateReason(rule protodomain.Rule, g domain.EligibleGoat) string {
	switch rule.TriggerType {
	case "birth_age":
		if g.DOB == nil {
			return "missing_dob"
		}
	case "post_arrival":
		if warmingEntryAt(g) == nil {
			return "missing_entry_date"
		}
	}
	return ""
}

// anchorMissingReason maps a rule's trigger type to the SAME stable reason string
// previousMissingDueDateKey uses to compute the cancellation key once the anchor
// resolves (B3/B7): the B3 adult-catch-up placeholder and the legacy
// missing_due_date placeholder intentionally share one key scheme so either one is
// superseded automatically the moment DOB/entry-date is known.
func anchorMissingReason(triggerType string) string {
	switch triggerType {
	case "birth_age":
		return "missing_dob"
	case "post_arrival":
		return "missing_entry_date"
	default:
		return ""
	}
}

func applyMissedDosePolicy(rule protodomain.Rule, due, asOf time.Time, nearbyDriveDate *time.Time, policy genMissedDosePolicy) (time.Time, string, bool) {
	if rule.DueWindowDays <= 0 {
		return due, "", false
	}
	windowEnd := businessDayStart(due).AddDate(0, 0, int(rule.DueWindowDays))
	if !asOf.After(windowEnd) {
		return due, "", false
	}
	switch strings.ToLower(strings.TrimSpace(rule.CatchUp)) {
	case "", "immediate":
		if nearbyDriveDate != nil {
			nearby := businessDayStart(*nearbyDriveDate)
			from := businessDayStart(asOf)
			to := from.AddDate(0, 0, int(policy.alignDays()))
			if !nearby.Before(from) && !nearby.After(to) {
				return nearby, "", false
			}
		}
		return asOf, "", false
	case "pc_approval":
		return asOf, "catch_up_pc_approval", false
	case "defer":
		return asOf, "missed_dose_deferred", false
	case "next_cycle":
		next, ok := nextRepeatCycle(rule, due, asOf)
		if !ok {
			return time.Time{}, "", true
		}
		return next, "", false
	default:
		return asOf, "catch_up_" + strings.ToLower(strings.TrimSpace(rule.CatchUp)), false
	}
}

func skipNonFutureOpenWork(rule protodomain.Rule, due, asOf time.Time, policy genMissedDosePolicy) bool {
	if !policy.MaterializeOnlyFutureOpenWork {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(rule.Repeat), "none") || strings.TrimSpace(rule.Repeat) == "" {
		return false
	}
	return !businessDayStart(due).After(businessDayStart(asOf))
}

func trustedEvidenceDue(rule protodomain.Rule, due, asOf time.Time, nearbyDriveDate *time.Time, policy genMissedDosePolicy) (time.Time, bool) {
	adjusted, _, skip := applyMissedDosePolicy(rule, due, asOf, nearbyDriveDate, policy)
	if skip {
		return time.Time{}, false
	}
	if strings.EqualFold(strings.TrimSpace(rule.CatchUp), "next_cycle") && !adjusted.Equal(due) {
		return adjusted, true
	}
	return due, true
}

func obligationKeyDue(rule protodomain.Rule, baseDue, materializedDue time.Time) time.Time {
	return baseDue
}

func isStableAdultCampaignObligationKey(rule protodomain.Rule, g domain.EligibleGoat) bool {
	repeat := strings.TrimSpace(rule.Repeat)
	if repeat != "" && !strings.EqualFold(repeat, "none") {
		return false
	}
	return isAdultCampaignRule(rule) && hasPhysicalCampaignPlacement(g)
}

func stableAdultCampaignObligationKey(tenantID, versionID string, rule protodomain.Rule, g domain.EligibleGoat) string {
	if !isStableAdultCampaignObligationKey(rule, g) {
		return ""
	}
	return obligationKey(
		tenantID,
		versionID,
		rule.RuleID,
		"goat",
		g.GoatID,
		"adult_campaign",
		strconv.Itoa(int(rule.Sequence)),
	)
}

func nextRepeatCycle(rule protodomain.Rule, due, asOf time.Time) (time.Time, bool) {
	switch strings.ToLower(strings.TrimSpace(rule.Repeat)) {
	case "every_n_days":
		interval := rule.MinGapDays
		if interval <= 0 {
			return time.Time{}, false
		}
		next := businessDayStart(due)
		for !next.After(asOf) {
			next = businessDayStart(next).AddDate(0, 0, int(interval))
		}
		return next, true
	case "yearly":
		next := businessDayStart(due)
		for !next.After(asOf) {
			next = businessDayStart(next).AddDate(1, 0, 0)
		}
		return next, true
	default:
		return time.Time{}, false
	}
}

func deferredReason(g domain.EligibleGoat, allowed []string) string {
	allowedStates := deferStateSet(allowed)
	if len(allowedStates) == 0 {
		return ""
	}
	for _, value := range []string{g.HealthStatus, g.LifecycleStatus} {
		normalized := strings.ToLower(strings.TrimSpace(value))
		switch normalized {
		case "sick", "under_treatment", "recovering", "quarantine", "icu":
			if allowedStates[normalized] {
				return normalized
			}
		}
	}
	if g.LocationIsICU && allowedStates["icu"] {
		return "location_icu"
	}
	if g.LocationIsQuarantine && allowedStates["quarantine"] {
		return "location_quarantine"
	}
	return ""
}

// deferStateSet resolves the effective clinical-defer set for a rule. Authored
// defer_states may only ADD states; the mandatory clinical safety blocks (sick,
// under_treatment, recovering, quarantine, icu) are always deferred regardless of what the
// authored list contains. This keeps the runtime medically safe even for a
// protocol version that was published before the publish-time guard existed:
// a sick/under-treatment/recovering animal under a partial or empty defer list
// is held until healthy, never cancelled/excluded. See protodomain.EffectiveClinicalDeferStates
// and docs/preventive-care-vaccination/vaccination-rules.md (C35-010).
func deferStateSet(allowed []string) map[string]bool {
	effective := protodomain.EffectiveClinicalDeferStates(allowed)
	allowedStates := make(map[string]bool, len(effective))
	for _, state := range effective {
		allowedStates[state] = true
	}
	return allowedStates
}

func eligibilityStage(e genEligibility) genStringList {
	if len(e.AnimalStage) > 0 {
		return e.AnimalStage
	}
	return e.Stage
}

func goatMatchesEligibility(g domain.EligibleGoat, e genEligibility, preg genPregnancyPolicy, asOf time.Time) bool {
	if !selectorMatches(g.Species, e.Species) {
		return false
	}
	if !selectorMatches(g.Stage, eligibilityStage(e)) {
		return false
	}
	if !selectorMatches(g.Sex, e.Sex) {
		return false
	}
	if !selectorMatches(g.Breed, e.Breed) {
		return false
	}
	// A terminal exit state (dead/sold/culled/transferred/lost/merged/inactive) is
	// never eligible and never deferrable, even if a stale clinical health signal
	// is present on the row.
	if protodomain.IsExitLifecycleState(g.LifecycleStatus) {
		return false
	}
	// Clinical defer is a SAFETY HOLD, not an exclusion. A goat in a clinical
	// state — represented via health_status (sick/under_treatment/recovering/
	// quarantine/icu), lifecycle_status (sick/under_treatment/quarantine/icu),
	// or via an ICU/quarantine location — is
	// INCLUDED as deferred, bypassing the normal lifecycle=alive and
	// health=healthy selectors so its open work is held for recovery rather than
	// cancelled/excluded. Without this, a canonical `lifecycle=alive` rule rejects
	// an in-care goat carried as `lifecycle_status=sick` before the defer logic
	// ever runs — the wrong medical action (C35-010).
	if deferredReason(g, e.DeferStates) == "" {
		if !lifecycleMatches(g.LifecycleStatus, e.Lifecycle) {
			return false
		}
		if !selectorMatches(g.HealthStatus, e.Health) {
			return false
		}
	}
	if !selectorMatches(g.ReproductiveStatus, e.Reproductive) {
		return false
	}
	if !selectorMatches(g.ProcurementPurpose, e.ProcurementPurpose) {
		return false
	}
	if !selectorMatches(g.AgeBand, e.AgeBand) {
		return false
	}
	for _, excluded := range e.ExcludeReproductiveStates {
		if reproductiveStateExcluded(g, []string{excluded}, preg) {
			return false
		}
	}
	if !ageMatches(g, e, asOf) {
		return false
	}
	return true
}

func lifecycleMatches(actual string, allowed genStringList) bool {
	return selectorMatches(actual, allowed)
}

func selectorMatches(actual string, allowed genStringList) bool {
	if len(allowed) == 0 {
		return true
	}
	hasConcrete := false
	for _, value := range allowed {
		s := normDim(value)
		if s == "" {
			return true
		}
		hasConcrete = true
		if sameDim(s, actual) {
			return true
		}
	}
	return !hasConcrete
}

func normSingleDim(values genStringList) string {
	out := ""
	for _, value := range values {
		s := normDim(value)
		if s == "" {
			return ""
		}
		if out != "" && !sameDim(out, s) {
			return ""
		}
		out = s
	}
	return out
}

func sameDim(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func ageMatches(g domain.EligibleGoat, e genEligibility, asOf time.Time) bool {
	if e.MinAgeDays == nil && e.MaxAgeDays == nil {
		return true
	}
	if g.DOB == nil {
		return false
	}
	ageDays := wholeDaysBetween(*g.DOB, asOf)
	if e.MinAgeDays != nil && ageDays < int(*e.MinAgeDays) {
		return false
	}
	if e.MaxAgeDays != nil && ageDays > int(*e.MaxAgeDays) {
		return false
	}
	return true
}

func wholeDaysBetween(start, end time.Time) int {
	startDate := businessDayStart(start)
	endDate := businessDayStart(end)
	return int(endDate.Sub(startDate).Hours() / 24)
}

// dueAt computes a rule's due date for a goat. ok=false with skip=false means the trigger is not an
// SM-1 trigger (after_previous_completion → SM-7, manual_campaign). skip=true means an SM-1 trigger
// that cannot be scheduled for this goat (missing dob/entry_date).
func obligationWindowEnd(rule protodomain.Rule, due time.Time) *time.Time {
	if rule.DueWindowDays <= 0 {
		return nil
	}
	end := businessDayStart(due).AddDate(0, 0, int(rule.DueWindowDays))
	return &end
}

func dueAt(versionID string, rule protodomain.Rule, g domain.EligibleGoat, asOf time.Time, opts generationOptions, policies genVersionPolicies) (due time.Time, ok bool, skip bool) {
	switch rule.TriggerType {
	case "birth_age":
		if g.DOB == nil {
			return time.Time{}, false, true
		}
		return businessDayStart(*g.DOB).AddDate(0, 0, int(rule.OffsetDays)), true, false
	case "post_arrival":
		if warmingEntryAt(g) == nil {
			return time.Time{}, false, true
		}
		return adjustPostArrivalDue(g, rule, policies.Procurement), true, false
	case "calendar":
		return businessDayStart(asOf).AddDate(0, 0, int(rule.OffsetDays)), true, false
	case "manual_campaign":
		if opts.ManualCampaignID == "" {
			// Adult blank-history rows are normal schedule work. campaignDueOverrides has already
			// admitted only eligible adults with a real physical-shed placement and no accepted
			// same-vaccine history, so materialize them during ordinary generation. The explicit
			// manual-campaign command remains available for non-adult/manual-only rule rows.
			if campaignDue, found := opts.campaignDueByGoat[campaignDueGoatKey(versionID, rule.RuleID, g.GoatID)]; found {
				return campaignDue, true, false
			}
			return time.Time{}, false, false
		}
		return businessDayStart(asOf).AddDate(0, 0, int(rule.OffsetDays)), true, false
	default: // after_previous_completion (SM-7)
		return time.Time{}, false, false
	}
}

func (s *GenerationService) manualVaccineAnchorsForSeedRules(ctx context.Context, tenantID, goatID string, rules []protodomain.Rule, eligibility genEligibility, vaccineProf vaccineProfile) (map[string]obldomain.ObligationRef, error) {
	reader, ok := s.obl.(ManualVaccineAnchorReader)
	if !ok {
		return nil, nil
	}
	codes := make([]string, 0, len(rules))
	seen := map[string]bool{}
	for _, rule := range rules {
		if !manualAnchorSuppressesSeedRule(rule) {
			continue
		}
		_, ruleVaccine, err := ruleGenerationContext(rule, eligibility, vaccineProf)
		if err != nil {
			return nil, err
		}
		key := vaccineAnchorLookupKey(ruleVaccine.Code)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		codes = append(codes, ruleVaccine.Code)
	}
	if len(codes) == 0 {
		return nil, nil
	}
	anchors, err := reader.ManualVaccineAnchorsForGoat(ctx, tenantID, goatID, codes)
	if err != nil {
		return nil, err
	}
	if len(anchors) == 0 {
		return nil, nil
	}
	normalized := make(map[string]obldomain.ObligationRef, len(anchors))
	for code, ref := range anchors {
		normalized[vaccineAnchorLookupKey(code)] = ref
	}
	return normalized, nil
}

func manualAnchorSuppressesSeedRule(rule protodomain.Rule) bool {
	switch strings.TrimSpace(rule.TriggerType) {
	case "birth_age", "post_arrival", "calendar", "manual_campaign":
		return true
	default:
		return false
	}
}

func vaccineAnchorLookupKey(code string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "+", "_"))
}

// dueAfterPreviousCompletion resolves an after_previous_completion (SM-1 revac)
// rule's due date from the goat's OWN latest accepted administration of the SAME
// vaccine — B1 (per-vaccine continuation): each vaccine progresses independently
// from its own latest completion, e.g. FMD's next due = latest FMD administration +
// 9 months, never first-dose + interval. This intentionally does NOT require the
// completion to have come from any specific prior rule/sequence (birth_age primary,
// post_arrival primary, or an earlier revac cycle all count identically) — anchoring
// to rule-sequence adjacency was the root cause of anchoring FMD recurrence to the
// wrong (earliest reachable) administration whenever a vaccine's primary dose came
// from a different wave than the sequence-immediately-prior rule.
func dueAfterPreviousCompletion(rule protodomain.Rule, ruleVaccine vaccineProfile, history []domain.RecentVaccineAdministration) (time.Time, bool) {
	if !strings.EqualFold(strings.TrimSpace(rule.TriggerType), "after_previous_completion") {
		return time.Time{}, false
	}
	targetVaccine := strings.TrimSpace(ruleVaccine.Code)
	if targetVaccine == "" {
		return time.Time{}, false
	}
	latest, found := latestVaccineCompletion(rule, targetVaccine, history)
	if !found {
		return time.Time{}, false
	}
	// A genuine repeat/revac rule (Repeat="every_n_days"/"yearly") always anchors to
	// its own configured interval from the goat's latest administration of this
	// vaccine (B1) — this is what makes FMD's "single + repeat" course correctly
	// compute latest-FMD-administration + 9 months regardless of whether the latest
	// dose came from the birth_age primary, the post_arrival primary, or an earlier
	// revac cycle.
	if due, ok := repeatDueAfterCompletion(rule, latest); ok {
		return due, true
	}
	// A non-repeating after_previous_completion dose (an immediate next-in-chain
	// booster with no Repeat configured) uses its fixed offset/min-gap interval,
	// still anchored to the latest matching administration.
	gap := afterPreviousGapDays(rule)
	if gap <= 0 {
		return time.Time{}, false
	}
	return businessDayStart(latest).AddDate(0, 0, int(gap)), true
}

// primaryCourseContinuationDueFromHistory handles primary-course rows whose earlier course dose was
// imported as real history. In that case the next primary-course dose must continue from the actual
// administered date plus the configured row-to-row gap, not from a stale/missing DOB or entry date.
func primaryCourseContinuationDueFromHistory(rule protodomain.Rule, ruleVaccine vaccineProfile, rules []protodomain.Rule, fallbackEligibility genEligibility, fallbackVaccine vaccineProfile, path string, history []domain.RecentVaccineAdministration) (time.Time, bool, error) {
	if !isPrimaryCourseRule(rule) {
		return time.Time{}, false, nil
	}
	prev, prevVaccine, found, err := previousPrimaryCourseRule(rule, ruleVaccine, rules, fallbackEligibility, fallbackVaccine, path)
	if err != nil || !found {
		return time.Time{}, false, err
	}
	adminAt, administered := latestSameDoseAdministration(prev, prevVaccine, history)
	if !administered {
		return time.Time{}, false, nil
	}
	gap := rule.OffsetDays - prev.OffsetDays
	if rule.MinGapDays > gap {
		gap = rule.MinGapDays
	}
	if gap <= 0 {
		return time.Time{}, false, nil
	}
	return businessDayStart(adminAt).AddDate(0, 0, int(gap)), true, nil
}

// repeatMustWaitForPrimaryCourse prevents an accepted first dose from being treated as a completed
// course. Repeat/revac rows can anchor from history only after the latest accepted primary-course
// dose has no later primary-course dose outstanding for this goat's schedule path.
func repeatMustWaitForPrimaryCourse(rule protodomain.Rule, ruleVaccine vaccineProfile, rules []protodomain.Rule, fallbackEligibility genEligibility, fallbackVaccine vaccineProfile, path string, history []domain.RecentVaccineAdministration) (bool, error) {
	if !strings.EqualFold(strings.TrimSpace(rule.TriggerType), "after_previous_completion") {
		return false, nil
	}
	latest, found := latestVaccineAdministration(rule, strings.TrimSpace(ruleVaccine.Code), history)
	if !found {
		return false, nil
	}
	currentPrimary, currentVaccine, matched, err := primaryRuleForAdministration(ruleVaccine, latest, rules, fallbackEligibility, fallbackVaccine, path)
	if err != nil || !matched {
		return false, err
	}
	for _, candidate := range rules {
		if !isPrimaryCourseRule(candidate) || !ruleMatchesSchedulePath(candidate, path) {
			continue
		}
		candidateEligibility, candidateVaccine, err := ruleGenerationContext(candidate, fallbackEligibility, fallbackVaccine)
		if err != nil {
			return false, err
		}
		_ = candidateEligibility
		if !strings.EqualFold(strings.TrimSpace(candidateVaccine.Code), strings.TrimSpace(currentVaccine.Code)) {
			continue
		}
		if !primaryRuleAfter(candidate, currentPrimary) {
			continue
		}
		if hasSameDoseAdministration(candidate, candidateVaccine, history) {
			continue
		}
		return true, nil
	}
	return false, nil
}

func previousPrimaryCourseRule(rule protodomain.Rule, ruleVaccine vaccineProfile, rules []protodomain.Rule, fallbackEligibility genEligibility, fallbackVaccine vaccineProfile, path string) (protodomain.Rule, vaccineProfile, bool, error) {
	var best protodomain.Rule
	var bestVaccine vaccineProfile
	found := false
	for _, candidate := range rules {
		if !isPrimaryCourseRule(candidate) || !ruleMatchesSchedulePath(candidate, path) {
			continue
		}
		_, candidateVaccine, err := ruleGenerationContext(candidate, fallbackEligibility, fallbackVaccine)
		if err != nil {
			return protodomain.Rule{}, vaccineProfile{}, false, err
		}
		if !strings.EqualFold(strings.TrimSpace(candidateVaccine.Code), strings.TrimSpace(ruleVaccine.Code)) {
			continue
		}
		if !primaryRuleBefore(candidate, rule) {
			continue
		}
		if !found || primaryRuleAfter(candidate, best) {
			best = candidate
			bestVaccine = candidateVaccine
			found = true
		}
	}
	return best, bestVaccine, found, nil
}

func primaryRuleForAdministration(ruleVaccine vaccineProfile, admin domain.RecentVaccineAdministration, rules []protodomain.Rule, fallbackEligibility genEligibility, fallbackVaccine vaccineProfile, path string) (protodomain.Rule, vaccineProfile, bool, error) {
	for _, candidate := range rules {
		if !isPrimaryCourseRule(candidate) || !ruleMatchesSchedulePath(candidate, path) {
			continue
		}
		_, candidateVaccine, err := ruleGenerationContext(candidate, fallbackEligibility, fallbackVaccine)
		if err != nil {
			return protodomain.Rule{}, vaccineProfile{}, false, err
		}
		if !strings.EqualFold(strings.TrimSpace(candidateVaccine.Code), strings.TrimSpace(ruleVaccine.Code)) {
			continue
		}
		if sameDoseAdministration(candidate, candidateVaccine, admin) {
			return candidate, candidateVaccine, true, nil
		}
	}
	return protodomain.Rule{}, vaccineProfile{}, false, nil
}

func primaryRuleBefore(a, b protodomain.Rule) bool {
	if a.OffsetDays != b.OffsetDays {
		return a.OffsetDays < b.OffsetDays
	}
	return a.Sequence < b.Sequence
}

func primaryRuleAfter(a, b protodomain.Rule) bool {
	if a.OffsetDays != b.OffsetDays {
		return a.OffsetDays > b.OffsetDays
	}
	return a.Sequence > b.Sequence
}

func latestSameDoseAdministration(rule protodomain.Rule, vaccine vaccineProfile, history []domain.RecentVaccineAdministration) (time.Time, bool) {
	var latest time.Time
	found := false
	for _, admin := range history {
		if !sameDoseAdministration(rule, vaccine, admin) {
			continue
		}
		if !found || admin.AdministeredAt.After(latest) {
			latest = admin.AdministeredAt
			found = true
		}
	}
	return latest, found
}

func sameDoseAdministration(rule protodomain.Rule, vaccine vaccineProfile, admin domain.RecentVaccineAdministration) bool {
	if admin.AdministeredAt.IsZero() {
		return false
	}
	if !sameProtocolLineage(rule, admin) {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(admin.VaccineCode), strings.TrimSpace(vaccine.Code)) {
		return false
	}
	ruleDose := strings.ToLower(strings.TrimSpace(rule.DoseCode))
	adminDose := strings.ToLower(strings.TrimSpace(admin.DoseCode))
	if ruleDose != "" && adminDose != "" {
		return ruleDose == adminDose
	}
	return rule.Sequence != 0 && admin.Sequence == rule.Sequence
}

// latestVaccineCompletion returns the MOST RECENT accepted administration of the
// rule's own vaccine, regardless of which rule OR PROTOCOL/VERSION produced it
// (RV-03). Continuation anchors on the vaccine's real-world latest dose: a goat
// whose latest FMD was administered under an older protocol still has its next FMD
// scheduled from that administration + the CURRENT protocol's interval (screenshot
// example: latest FMD 1 Jan under old protocol, current interval 9 months → next
// FMD 1 Oct). Requiring same-protocol lineage here left an animal with cross-protocol
// same-vaccine history and a missing DOB/entry anchor with NOTHING scheduled: the
// primary is suppressed by hasVaccineAdministrationHistory (which matches by vaccine
// code across any protocol) while continuation was rejected for lineage — the two
// signals must use ONE consistent lineage rule (RV-03), and per the product decision
// that rule is "latest administration of this vaccine, any protocol, anchors the next
// dose". Dose-level anti-duplication stays protocol-scoped in hasSameDoseAdministration
// (a different protocol's "dose 1" is not our dose), so no earlier dose is recreated.
//
// Selecting max(administered_at) explicitly — rather than trusting the first entry in
// whatever order a caller's history slice happens to be in — keeps this correct even
// when a goat has multiple real historical administrations of the same vaccine (the
// common case for the years-long imported field history).
func latestVaccineCompletion(rule protodomain.Rule, targetVaccine string, history []domain.RecentVaccineAdministration) (time.Time, bool) {
	admin, found := latestVaccineAdministration(rule, targetVaccine, history)
	return admin.AdministeredAt, found
}

func latestVaccineAdministration(rule protodomain.Rule, targetVaccine string, history []domain.RecentVaccineAdministration) (domain.RecentVaccineAdministration, bool) {
	var latestAdmin domain.RecentVaccineAdministration
	var latestAt time.Time
	found := false
	for _, admin := range history {
		if admin.AdministeredAt.IsZero() {
			continue
		}
		adminVaccine := strings.TrimSpace(admin.VaccineCode)
		if adminVaccine == "" || !strings.EqualFold(targetVaccine, adminVaccine) {
			continue
		}
		if !found || admin.AdministeredAt.After(latestAt) {
			latestAt = admin.AdministeredAt
			latestAdmin = admin
			found = true
		}
	}
	return latestAdmin, found
}

// hasVaccineAdministrationHistory reports whether the goat has any accepted
// administration of the rule's own vaccine, regardless of which rule/path produced
// it. Used for B2 (never defer for a missing DOB/entry-date anchor when a
// vaccination anchor already exists for this vaccine).
func hasVaccineAdministrationHistory(vaccine vaccineProfile, history []domain.RecentVaccineAdministration) bool {
	code := strings.TrimSpace(vaccine.Code)
	if code == "" {
		return false
	}
	for _, admin := range history {
		if admin.AdministeredAt.IsZero() {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(admin.VaccineCode), code) {
			return true
		}
	}
	return false
}

// hasSameDoseAdministration reports whether THIS rule's specific dose (its dose_code, or its
// sequence when dose codes are absent) of THIS vaccine has already been administered from the SAME
// protocol lineage. It is the correct anchor-suppression signal: a completed dose must not be
// regenerated after a DOB/entry correction, but a LATER still-required dose of the same course
// (ET+TT week-7 booster, adult wave two) — which has no matching administration yet — must still be
// scheduled. Using any-dose vaccine history here would wrongly suppress those required boosters
// (VACC-REV-06). Cross-protocol administrations (different version/protocol) MUST NOT suppress work
// even if dose code or sequence matches (a different protocol's "dose 1" is not the same as ours).
func hasSameDoseAdministration(rule protodomain.Rule, vaccine vaccineProfile, history []domain.RecentVaccineAdministration) bool {
	code := strings.TrimSpace(vaccine.Code)
	if code == "" {
		return false
	}
	ruleDose := strings.ToLower(strings.TrimSpace(rule.DoseCode))
	for _, admin := range history {
		if admin.AdministeredAt.IsZero() {
			continue
		}
		// VACC-REV-06: protocol lineage must match BEFORE comparing dose code or sequence. A dose
		// from a different protocol (version) is not the same dose, regardless of matching
		// dose_code/sequence labels.
		if !sameProtocolLineage(rule, admin) {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(admin.VaccineCode), code) {
			continue
		}
		adminDose := strings.ToLower(strings.TrimSpace(admin.DoseCode))
		// When BOTH sides name a dose code, dose-code identity is authoritative: an exact match is the
		// same dose; a mismatch is a DIFFERENT dose of the same vaccine, so do NOT fall back to a
		// sequence match (two distinct doses can reuse sequence 1 and would wrongly suppress each
		// other — VACC-REV-06).
		if ruleDose != "" && adminDose != "" {
			if ruleDose == adminDose {
				return true
			}
			continue
		}
		// Sequence is only a fallback when dose-code identity is unavailable on one or both sides.
		if rule.Sequence != 0 && admin.Sequence == rule.Sequence {
			return true
		}
	}
	return false
}

func sameProtocolLineage(rule protodomain.Rule, admin domain.RecentVaccineAdministration) bool {
	ruleProtocolID := strings.TrimSpace(rule.ProtocolID)
	adminProtocolID := strings.TrimSpace(admin.ProtocolID)
	if ruleProtocolID != "" && adminProtocolID != "" {
		return ruleProtocolID == adminProtocolID
	}
	ruleVersionID := strings.TrimSpace(rule.ProtocolVersionID)
	adminVersionID := strings.TrimSpace(admin.ProtocolVersionID)
	if ruleVersionID != "" && adminVersionID != "" {
		return ruleVersionID == adminVersionID
	}
	return true
}

func afterPreviousGapDays(rule protodomain.Rule) int32 {
	gap := rule.OffsetDays
	if rule.MinGapDays > gap {
		gap = rule.MinGapDays
	}
	return gap
}

func obligationKey(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		_, _ = h.Write([]byte(p))
		_, _ = h.Write([]byte("|"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func generationRunKey(tenantID, versionID, triggerType, triggerRef string) string {
	if triggerType == "" {
		triggerType = "manual"
	}
	return obligationKey("vaccination_generation_run", tenantID, versionID, triggerType, triggerRef)
}

func manualCampaignTriggerRef(campaignID string, asOf time.Time) string {
	return strings.TrimSpace(campaignID) + "@" + asOf.UTC().Format(time.RFC3339Nano)
}

func generationResultFromRun(run domain.GenerationRun) domain.GenerateResult {
	return domain.GenerateResult{
		Generated:                  run.Generated,
		Deferred:                   run.Deferred,
		Reopened:                   run.Reopened,
		FailedGoats:                run.FailedGoats,
		SkippedNoDueDate:           run.SkippedNoDueDate,
		SuppressedByTrustedHistory: run.SuppressedByTrustedHistory,
	}
}

// historyRepeatCycle names a past administration as the cause of the repeat cycle it
// drives. The reference is the administration's own immutable coordinates -- which
// vaccine, given when, as which dose -- and never the derived due date, because the due
// date is precisely what moves when a newer administration lands.
func historyRepeatCycle(admin domain.RecentVaccineAdministration) *obldomain.RepeatCycleSource {
	ref := obldomain.RepeatCycleRef(admin.VaccineCode, admin.AdministeredAt, admin.Sequence)
	if ref == "" {
		return nil
	}
	at := admin.AdministeredAt
	return &obldomain.RepeatCycleSource{
		Source:    obldomain.RepeatCycleSourceTrustedHistory,
		SourceRef: ref,
		AnchorAt:  &at,
	}
}

// supersedeRetiredPlanWork cancels open vaccination obligations belonging to a version that is
// no longer effective for the animal, one park at a time.
func (s *GenerationService) supersedeRetiredPlanWork(
	ctx context.Context,
	tenantID string,
	goats []domain.EligibleGoat,
	effectiveVersionsByPark map[string][]string,
	asOf time.Time,
) error {
	byPark := make(map[string][]string, len(effectiveVersionsByPark))
	for _, g := range goats {
		key := generationParkCacheKey(g.ParkID)
		byPark[key] = append(byPark[key], g.GoatID)
	}
	for parkKey, goatIDs := range byPark {
		versionIDs := effectiveVersionsByPark[parkKey]
		if len(versionIDs) == 0 {
			// No effective plan for this park. Cancelling everything here would be
			// indistinguishable from a misconfigured lookup, so leave the work alone.
			continue
		}
		// seed-fixture-guard:ignore: owner=ravi issue=vaccination-additive-publish reason=carry-over-rebinds-existing-obligations-to-the-republished-version-and-authors-no-seed-source-config-or-SOP-contract expiry=2026-11-30
		//
		// Carry over BEFORE superseding. Work whose rule is unchanged -- same vaccine, same dose,
		// same content -- is rebound to the new version in place, keeping its obligation id and
		// its due date. Only what is left after this is genuinely stale, so publishing a plan
		// that adds one vaccine no longer cancels and re-mints the vaccines nobody touched.
		//
		// Order matters beyond tidiness: rebinding after generation would collide with
		// obligation_instances_dup_guard, because generation would already hold that key.
		if _, err := s.obl.CarryOverUnchangedVaccinationObligations(ctx, tenantID, goatIDs, versionIDs); err != nil {
			return err
		}
		stale, err := s.obl.GoatsWithVaccinationObligationsOutsideVersions(ctx, tenantID, goatIDs, versionIDs)
		if err != nil {
			return err
		}
		for _, goatID := range stale {
			if _, err := s.obl.CancelOpenVaccinationObligationsForGoatExceptVersions(
				ctx, tenantID, goatID, versionIDs, "protocol_version_replaced", asOf,
			); err != nil {
				return err
			}
		}
	}
	return nil
}
