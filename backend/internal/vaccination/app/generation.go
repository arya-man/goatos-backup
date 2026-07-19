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
	FindNearestPlannedBatchDate(ctx context.Context, tenantID, versionID, ruleID, vaccineCode, shedID, parkID string, from, to time.Time) (*time.Time, error)
	CancelOpenObligationByIdempotencyKey(ctx context.Context, tenantID, idempotencyKey, reason string, occurredAt time.Time) (obligationID string, changed bool, err error)
	CancelOpenVaccinationObligationsForGoatExceptVersions(ctx context.Context, tenantID, goatID string, effectiveVersionIDs []string, reason string, occurredAt time.Time) (int, error)
	CancelOpenVaccinationObligationsForGoatVersion(ctx context.Context, tenantID, goatID, protocolVersionID, reason string, occurredAt time.Time) (int, error)
	RecordStatusEvent(ctx context.Context, ev obldomain.NewStatusEvent) (string, bool, error)
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
func cancelReasonMintsSuccessor(reason string) bool {
	switch strings.TrimSpace(reason) {
	case "ineligible_after_shift":
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
	if recorder, ok := goats.(GenerationRunRecorder); ok {
		s.runs = recorder
	}
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
	filter := domain.ImpactFilter{TenantID: tenantID}
	after := ""
	plans := make(map[string]cachedVersionPlan)
	effectiveVersionsByPark := make(map[string][]string)
	failedGoats := make(map[string]struct{})
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
		vaccineHistoryByGoat, err := s.recentVaccineAdminsForPlans(ctx, tenantID, pagePlans, asOf)
		if err != nil {
			return res, err
		}
		trustedByVersion, err := s.trustedEvidenceForPlans(ctx, tenantID, pagePlans, asOf, vaccineHistoryByGoat)
		if err != nil {
			return res, err
		}
		for _, p := range pagePlans {
			if err := s.genOneGoat(ctx, tenantID, p.versionID, p.rules, p.deferState, p.eligibility, p.goat, asOf, p.opts, p.policies, p.vaccineProfile, vaccineHistoryByGoat[p.goat.GoatID], trustedByVersion[p.versionID], &res); err != nil {
				if shouldAbortGeneration(err) {
					return res, err
				}
				recordFailedGenerationGoat(&res, failedGoats, p.goat.GoatID)
				continue
			}
		}
		if int32(len(goats)) < s.page {
			break
		}
		after = goats[len(goats)-1].GoatID
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
	if err := s.runs.FinishGenerationRun(ctx, tenantID, run.RunID, res, "", lastError, time.Now().UTC()); err != nil && genErr == nil {
		genErr = err
	}
	return run, res, genErr
}

type generationOptions struct {
	ManualCampaignID  string
	RunIDempotencyKey string
	RunRequestHash    string
	// healthRecoveryAlign enables sick/ICU/quarantine recovery replanning: align to a nearby planned
	// drive within recovery_policy.max_nearby_drive_align_days (default 7), else micro-drive now.
	healthRecoveryAlign bool
	// heartbeat, when set, is invoked once per cohort page so a long run keeps its generation-run row
	// fresh and is not reclaimed mid-flight. Best-effort: errors are intentionally swallowed by the
	// caller closure so a transient heartbeat failure never aborts a multi-minute generation pass.
	heartbeat func(ctx context.Context)
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
		if v.ScopeType == "tenant" {
			if err := s.fillEffectiveVersionsForGoats(ctx, tenantID, activeGoats, asOf, effectiveVersionsByPark); err != nil {
				return res, err
			}
		}
		effectiveVersionSets := make(map[string]map[string]struct{})
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
		vaccineHistoryByGoat, err := s.recentVaccineAdminsForPlans(ctx, tenantID, pagePlans, asOf)
		if err != nil {
			return res, err
		}
		trustedByVersion, err := s.trustedEvidenceForPlans(ctx, tenantID, pagePlans, asOf, vaccineHistoryByGoat)
		if err != nil {
			return res, err
		}
		for _, p := range pagePlans {
			if err := s.genOneGoat(ctx, tenantID, p.versionID, p.rules, p.deferState, p.eligibility, p.goat, asOf, p.opts, p.policies, p.vaccineProfile, vaccineHistoryByGoat[p.goat.GoatID], trustedByVersion[p.versionID], &res); err != nil {
				if shouldAbortGeneration(err) {
					return res, err
				}
				recordFailedGenerationGoat(&res, failedGoats, p.goat.GoatID)
				continue
			}
		}
		if opts.heartbeat != nil {
			opts.heartbeat(ctx)
		}
		if int32(len(goats)) < s.page {
			break
		}
		after = goats[len(goats)-1].GoatID
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
			due, ok, skip := dueAt(rule, plan.goat, asOf, plan.opts, plan.policies)
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
			hits, err := batch.HasTrustedCompletionEvidenceBatch(ctx, tenantID, versionID, candidates, asOf)
			if err != nil {
				return nil, err
			}
			lookup := lookups[versionID]
			for _, candidate := range candidates {
				key := candidate.Key()
				lookup.record(key, hits[key])
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
		// History outranks DOB/arrival, per DOSE: once THIS rule's own dose has been administered, it
		// must not be regenerated — even AFTER a DOB/entry-date correction makes dueAt resolvable — so
		// a later identity correction can never replace, duplicate, or replay that already-given dose.
		// Match the SPECIFIC dose (not any dose of the vaccine): a still-required later dose of the
		// same course (ET+TT week-7 booster, adult wave two) has no matching administration yet and
		// must still be scheduled from its own anchor (VACC-REV-06).
		if isPrimaryAnchorRule(rule) && hasSameDoseAdministration(rule, ruleVaccine, vaccineHistory) {
			res.SuppressedByTrustedHistory++
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
		baseDue, ok, skip := time.Time{}, false, false
		if courseDue, found, err := primaryCourseContinuationDueFromHistory(rule, ruleVaccine, rules, versionEligibility, vaccineProf, path, vaccineHistory); err != nil {
			return err
		} else if found {
			baseDue = courseDue
			ok = true
		} else {
			baseDue, ok, skip = dueAt(rule, g, asOf, opts, policies)
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
				if err := s.genMissingDueDateObligation(ctx, tenantID, versionID, rule, g, asOf, res); err != nil {
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
				}
			}
		}
		if !ok {
			continue // after_previous_completion → SM-7, manual_campaign → manual
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
		if skipNonFutureOpenWork(due, asOf, policies.MissedDose) {
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
		key := obligationKey(tenantID, versionID, rule.RuleID, "goat", g.GoatID, keyDue.UTC().Format(time.RFC3339), strconv.Itoa(int(rule.Sequence)))
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
			Sequence:          rule.Sequence,
		}
		var applied bool
		if deferred {
			_, applied, err = s.obl.InsertDeferredObligation(ctx, newObligation, deferReason, asOf)
		} else {
			_, applied, err = s.obl.InsertObligation(ctx, newObligation)
		}
		if err != nil {
			return err
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
			if missingKey != "" {
				if _, _, err := s.obl.CancelOpenObligationByIdempotencyKey(ctx, tenantID, missingKey, "missing_due_date_resolved", asOf); err != nil {
					return err
				}
			}
			if deferred {
				finalRef, changed, err = s.obl.DeferOpenObligationForGeneration(ctx, tenantID, key, deferReason, asOf)
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
				if opts.healthRecoveryAlign {
					reschedule, err = s.recoveryRescheduleForRule(ctx, tenantID, versionID, rule, ruleVaccine, g, asOf, policies.Recovery, policies.Compatibility, vaccineHistory)
					if err != nil {
						return err
					}
				}
				finalRef, changed, err = s.obl.ReopenDeferredObligationForGeneration(ctx, tenantID, key, asOf, reschedule)
				if err != nil {
					return err
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
	for attempt := 1; ; attempt++ {
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
		var ref obldomain.ObligationRef
		var changed bool
		if deferred {
			ref, changed, err = s.obl.DeferOpenObligationForGeneration(ctx, tenantID, successor.IdempotencyKey, deferReason, asOf)
		} else {
			ref, changed, err = s.obl.ReopenDeferredObligationForGeneration(ctx, tenantID, successor.IdempotencyKey, asOf, nil)
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
	}
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

func (s *GenerationService) genMissingDueDateObligation(ctx context.Context, tenantID, versionID string, rule protodomain.Rule, g domain.EligibleGoat, asOf time.Time, res *domain.GenerateResult) error {
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
		TargetType:        "goat",
		TargetID:          g.GoatID,
		ScopeType:         scopeType,
		ScopeID:           scopeID,
		DueAt:             asOf,
		Status:            "deferred",
		IdempotencyKey:    key,
		Sequence:          rule.Sequence,
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
	vaccineHistoryByGoat, err := s.recentVaccineAdminsForPlans(ctx, tenantID, pagePlans, asOf)
	if err != nil {
		return res, err
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

func skipNonFutureOpenWork(due, asOf time.Time, policy genMissedDosePolicy) bool {
	if !policy.MaterializeOnlyFutureOpenWork {
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

func dueAt(rule protodomain.Rule, g domain.EligibleGoat, asOf time.Time, opts generationOptions, policies genVersionPolicies) (due time.Time, ok bool, skip bool) {
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
			return time.Time{}, false, false
		}
		return businessDayStart(asOf).AddDate(0, 0, int(rule.OffsetDays)), true, false
	default: // after_previous_completion (SM-7)
		return time.Time{}, false, false
	}
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

// primaryCourseContinuationDueFromHistory handles course rows that are authored as DOB/arrival
// anchored primaries (for example ET+TT adult dose 1/dose 2 or kid 4w/7w) but whose first dose was
// imported as real history. In that case the next primary-course dose must continue from the actual
// administered date plus the configured row-to-row gap, not from a stale/missing DOB or entry date.
func primaryCourseContinuationDueFromHistory(rule protodomain.Rule, ruleVaccine vaccineProfile, rules []protodomain.Rule, fallbackEligibility genEligibility, fallbackVaccine vaccineProfile, path string, history []domain.RecentVaccineAdministration) (time.Time, bool, error) {
	if !isPrimaryAnchorRule(rule) {
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
		if !isPrimaryAnchorRule(candidate) || !ruleMatchesSchedulePath(candidate, path) {
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
		if !isPrimaryAnchorRule(candidate) || !ruleMatchesSchedulePath(candidate, path) {
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
		if !isPrimaryAnchorRule(candidate) || !ruleMatchesSchedulePath(candidate, path) {
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
