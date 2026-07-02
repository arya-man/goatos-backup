package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

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

// GoatLister is the eligible-goat source (the vaccination repo): chunked for backfill, single for
// the goat.created path.
type GoatLister interface {
	ListEligibleGoatsForGeneration(ctx context.Context, f domain.ImpactFilter, afterGoatID string, limit int32) ([]domain.EligibleGoat, error)
	GetGoatForGeneration(ctx context.Context, tenantID, goatID string) (domain.EligibleGoat, bool, error)
}

// ObligationWriter is the slice of the obligation repo SM-1 generation needs.
type ObligationWriter interface {
	InsertObligation(ctx context.Context, in obldomain.NewObligation) (string, bool, error)
	DeferOpenObligationByIdempotencyKey(ctx context.Context, tenantID, idempotencyKey, reason string, occurredAt time.Time) (obligationID string, applied bool, err error)
	ReopenDeferredObligationByIdempotencyKey(ctx context.Context, tenantID, idempotencyKey string, occurredAt time.Time) (obligationID string, changed bool, err error)
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

type cachedVersionPlan struct {
	rules       []protodomain.Rule
	deferState  []string
	eligibility genEligibility
}

type goatGenerationPlan struct {
	versionID   string
	rules       []protodomain.Rule
	deferState  []string
	goat        domain.EligibleGoat
	opts        generationOptions
	eligibility genEligibility
}

type trustedEvidenceLookup map[string]bool

// GenerationService implements SM-1: expand a published protocol version's rules into per-goat
// obligations over the in-care cohort, idempotently, deferring (visibly) ICU/quarantine/sick goats.
type GenerationService struct {
	proto    ProtocolReader
	goats    GoatLister
	obl      ObligationWriter
	evidence CompletionEvidenceReader
	runs     GenerationRunRecorder
	page     int32
}

// NewGenerationService wires the three repos. The completion-evidence reader is auto-wired when the
// goat repo implements it (the production path); requireEvidenceReader then fails generation loudly if
// it is ever absent, so trusted HF/import evidence is never silently ignored (double-dose risk).
func NewGenerationService(proto ProtocolReader, goats GoatLister, obl ObligationWriter) *GenerationService {
	s := &GenerationService{proto: proto, goats: goats, obl: obl, page: 500}
	if reader, ok := goats.(CompletionEvidenceReader); ok {
		s.evidence = reader
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
	AnimalStage               string        `json:"animal_stage"`
	Stage                     string        `json:"stage"`
	Sex                       string        `json:"sex"`
	Breed                     string        `json:"breed"`
	Lifecycle                 genStringList `json:"lifecycle"`
	Health                    string        `json:"health"`
	Reproductive              string        `json:"reproductive"`
	ExcludeReproductiveStates []string      `json:"exclude_reproductive_states"`
	DeferStates               []string      `json:"defer_states"`
	MinAgeDays                *int32        `json:"min_age_days"`
	MaxAgeDays                *int32        `json:"max_age_days"`
	AgeBand                   string        `json:"age_band"`
}

type genDSL struct {
	Eligibility genEligibility `json:"eligibility"`
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

// normDim maps "all"/"any"/"" to "" (no filter); any other value is an exact-match dimension.
func normDim(v string) string {
	v = strings.TrimSpace(v)
	switch strings.ToLower(v) {
	case "", "all", "any":
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

// GenerateForVersion generates obligations for every applicable rule of a PUBLISHED version across
// the eligible in-care cohort. Idempotent (deterministic key → ON CONFLICT no-op). Returns counts.
func (s *GenerationService) GenerateForVersion(ctx context.Context, tenantID, versionID string, asOf time.Time) (domain.GenerateResult, error) {
	return s.generateForVersion(ctx, tenantID, versionID, asOf, generationOptions{})
}

// GenerateManualCampaignForVersion materializes a deliberate campaign trigger. It is separate from
// normal publish/backfill generation so manual_campaign rules cannot fire accidentally.
func (s *GenerationService) GenerateManualCampaignForVersion(ctx context.Context, tenantID, versionID, campaignID string, asOf time.Time) (domain.GenerateResult, error) {
	return s.generateForVersion(ctx, tenantID, versionID, asOf, generationOptions{
		ManualCampaignID: campaignID,
	})
}

// GenerateEffectiveForAllGoats backfills the current cohort by resolving the effective vaccination
// version per goat/park. This is the safe no-version backfill path; it does not loop every published
// version and therefore cannot double-materialize tenant defaults plus park overrides.
func (s *GenerationService) GenerateEffectiveForAllGoats(ctx context.Context, tenantID string, asOf time.Time) (domain.GenerateResult, error) {
	var res domain.GenerateResult
	if err := s.requireEvidenceReader(); err != nil {
		return res, err
	}
	filter := domain.ImpactFilter{TenantID: tenantID}
	after := ""
	plans := make(map[string]cachedVersionPlan)
	for {
		goats, err := s.goats.ListEligibleGoatsForGeneration(ctx, filter, after, s.page)
		if err != nil {
			return res, err
		}
		if len(goats) == 0 {
			break
		}
		pagePlans := make([]goatGenerationPlan, 0, len(goats))
		for _, g := range goats {
			if !inCare(g.LifecycleStatus) {
				continue
			}
			versionIDs, err := s.proto.ListEffectiveVaccinationVersionsForGoat(ctx, tenantID, g.ParkID, asOf)
			if err != nil {
				return res, err
			}
			for _, versionID := range versionIDs {
				p, ok := plans[versionID]
				if !ok {
					v, err := s.proto.GetVersion(ctx, tenantID, versionID)
					if err != nil {
						return res, err
					}
					dsl, err := parseGenerationDSL(v.RuleDsl)
					if err != nil {
						return res, err
					}
					rules, err := s.proto.ListRules(ctx, tenantID, versionID)
					if err != nil {
						return res, err
					}
					p = cachedVersionPlan{rules: rules, deferState: dsl.Eligibility.DeferStates, eligibility: dsl.Eligibility}
					plans[versionID] = p
				}
				if !goatMatchesEligibility(g, p.eligibility, asOf) {
					continue
				}
				pagePlans = append(pagePlans, goatGenerationPlan{
					versionID:   versionID,
					rules:       p.rules,
					deferState:  p.deferState,
					goat:        g,
					opts:        generationOptions{},
					eligibility: p.eligibility,
				})
			}
		}
		trustedByVersion, err := s.trustedEvidenceForPlans(ctx, tenantID, pagePlans, asOf)
		if err != nil {
			return res, err
		}
		for _, p := range pagePlans {
			if err := s.genOneGoat(ctx, tenantID, p.versionID, p.rules, p.deferState, p.goat, asOf, p.opts, trustedByVersion[p.versionID], &res); err != nil {
				return res, err
			}
		}
		if int32(len(goats)) < s.page {
			break
		}
		after = goats[len(goats)-1].GoatID
	}
	return res, nil
}

// GenerateForVersionWithRun wraps an existing-cohort generation pass in a durable status row. If
// the same idempotency key has already completed, it returns the persisted counts without replaying
// the whole herd scan.
func (s *GenerationService) GenerateForVersionWithRun(ctx context.Context, tenantID, versionID string, asOf time.Time, triggerType, triggerRef string) (domain.GenerationRun, domain.GenerateResult, error) {
	return s.generateForVersionWithRun(ctx, tenantID, versionID, asOf, triggerType, triggerRef, generationOptions{})
}

// GenerateManualCampaignForVersionWithRun wraps manual campaign generation in a durable run row.
func (s *GenerationService) GenerateManualCampaignForVersionWithRun(ctx context.Context, tenantID, versionID, campaignID string, asOf time.Time) (domain.GenerationRun, domain.GenerateResult, error) {
	return s.generateForVersionWithRun(ctx, tenantID, versionID, asOf, "manual_campaign", manualCampaignTriggerRef(campaignID, asOf), generationOptions{
		ManualCampaignID: campaignID,
	})
}

// GenerateManualCampaignForVersionWithHTTPRun wraps manual campaign generation using the caller's
// HTTP Idempotency-Key as the durable command key. Exact retries return the same run/result without
// deriving a fresh as_of timestamp or materializing duplicate manual obligations.
func (s *GenerationService) GenerateManualCampaignForVersionWithHTTPRun(ctx context.Context, tenantID, versionID, campaignID string, asOf time.Time, idempotencyKey, requestHash string) (domain.GenerationRun, domain.GenerateResult, error) {
	return s.generateForVersionWithRun(ctx, tenantID, versionID, asOf, "manual_campaign", manualCampaignTriggerRef(campaignID, asOf), generationOptions{
		ManualCampaignID:  campaignID,
		RunIDempotencyKey: idempotencyKey,
		RunRequestHash:    requestHash,
	})
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
	if err := s.runs.FinishGenerationRun(ctx, tenantID, run.RunID, res, "", lastError, time.Now().UTC()); err != nil && genErr == nil {
		genErr = err
	}
	return run, res, genErr
}

type generationOptions struct {
	ManualCampaignID  string
	RunIDempotencyKey string
	RunRequestHash    string
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
	stage := eligibilityStage(elig)
	filter := domain.ImpactFilter{
		TenantID: tenantID,
		Stage:    normDim(stage),
		Sex:      normDim(elig.Sex),
		Breed:    normDim(elig.Breed),
		Health:   normDim(elig.Health),
	}
	if v.ScopeType == "park" && v.ScopeID != "" {
		park := v.ScopeID
		filter.ParkID = &park
	}
	effectiveCache := make(map[string]map[string]struct{})
	after := ""
	for {
		goats, err := s.goats.ListEligibleGoatsForGeneration(ctx, filter, after, s.page)
		if err != nil {
			return res, err
		}
		if len(goats) == 0 {
			break
		}
		pagePlans := make([]goatGenerationPlan, 0, len(goats))
		for _, g := range goats {
			if !inCare(g.LifecycleStatus) {
				continue
			}
			if !goatMatchesEligibility(g, elig, asOf) {
				continue
			}
			if v.ScopeType == "tenant" {
				effective, err := s.versionEffectiveForGoat(ctx, tenantID, g.ParkID, versionID, asOf, effectiveCache)
				if err != nil {
					return res, err
				}
				if !effective {
					continue
				}
			}
			pagePlans = append(pagePlans, goatGenerationPlan{
				versionID:   versionID,
				rules:       rules,
				deferState:  elig.DeferStates,
				goat:        g,
				opts:        opts,
				eligibility: elig,
			})
		}
		trustedByVersion, err := s.trustedEvidenceForPlans(ctx, tenantID, pagePlans, asOf)
		if err != nil {
			return res, err
		}
		for _, p := range pagePlans {
			if err := s.genOneGoat(ctx, tenantID, p.versionID, p.rules, p.deferState, p.goat, asOf, p.opts, trustedByVersion[p.versionID], &res); err != nil {
				return res, err
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
	return res, nil
}

func (s *GenerationService) versionEffectiveForGoat(ctx context.Context, tenantID, parkID, versionID string, asOf time.Time, cache map[string]map[string]struct{}) (bool, error) {
	key := parkID
	if key == "" {
		key = "<tenant>"
	}
	versions, ok := cache[key]
	if !ok {
		versionIDs, err := s.proto.ListEffectiveVaccinationVersionsForGoat(ctx, tenantID, parkID, asOf)
		if err != nil {
			return false, err
		}
		versions = make(map[string]struct{}, len(versionIDs))
		for _, id := range versionIDs {
			versions[id] = struct{}{}
		}
		cache[key] = versions
	}
	_, ok = versions[versionID]
	return ok, nil
}

func (s *GenerationService) trustedEvidenceForPlans(ctx context.Context, tenantID string, plans []goatGenerationPlan, asOf time.Time) (map[string]trustedEvidenceLookup, error) {
	lookups := make(map[string]trustedEvidenceLookup)
	if len(plans) == 0 {
		return lookups, nil
	}
	candidatesByVersion := make(map[string][]domain.TrustedCompletionCandidate)
	seenByVersion := make(map[string]map[string]bool)
	for _, plan := range plans {
		if _, ok := lookups[plan.versionID]; !ok {
			lookups[plan.versionID] = trustedEvidenceLookup{}
		}
		if _, ok := seenByVersion[plan.versionID]; !ok {
			seenByVersion[plan.versionID] = make(map[string]bool)
		}
		for _, rule := range plan.rules {
			due, ok, skip := dueAt(rule, plan.goat, asOf, plan.opts)
			if skip || !ok {
				continue
			}
			evidenceDue, ok := trustedEvidenceDue(rule, due, asOf)
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
			for key, trusted := range hits {
				if trusted {
					lookups[versionID][key] = true
				}
			}
		}
		return lookups, nil
	}
	for versionID, candidates := range candidatesByVersion {
		for _, candidate := range candidates {
			trusted, err := s.evidence.HasTrustedCompletionEvidence(ctx, tenantID, candidate.GoatID, versionID, candidate.RuleID, candidate.DoseCode, candidate.DueAt, asOf)
			if err != nil {
				return nil, err
			}
			if trusted {
				lookups[versionID][candidate.Key()] = true
			}
		}
	}
	return lookups, nil
}

func (s *GenerationService) hasTrustedCompletionEvidence(ctx context.Context, tenantID, versionID string, rule protodomain.Rule, g domain.EligibleGoat, due, asOf time.Time, trustedLookup trustedEvidenceLookup) (bool, error) {
	candidate := trustedCompletionCandidate(rule, g, due)
	if trustedLookup != nil {
		return trustedLookup[candidate.Key()], nil
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

// genOneGoat applies every applicable rule to one goat: compute due_at, idempotent insert, and a
// canonical deferred state when the goat is in a defer state. Accumulates counts into res.
func (s *GenerationService) genOneGoat(ctx context.Context, tenantID, versionID string, rules []protodomain.Rule, deferStates []string, g domain.EligibleGoat, asOf time.Time, opts generationOptions, trustedLookup trustedEvidenceLookup, res *domain.GenerateResult) error {
	historicalCatchUpMaterialized := false
	for _, rule := range rules {
		baseDue, ok, skip := dueAt(rule, g, asOf, opts)
		if skip {
			res.SkippedNoDueDate++
			if err := s.genMissingDueDateObligation(ctx, tenantID, versionID, rule, g, asOf, res); err != nil {
				return err
			}
			continue
		}
		if !ok {
			continue // after_previous_completion → SM-7, manual_campaign → manual
		}
		evidenceDue, ok := trustedEvidenceDue(rule, baseDue, asOf)
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
		due, catchUpDeferReason, skip := applyMissedDosePolicy(rule, baseDue, asOf)
		if skip {
			continue
		}
		if limitsHistoricalCatchUp(rule, baseDue, due, asOf, opts) {
			if historicalCatchUpMaterialized {
				continue
			}
			historicalCatchUpMaterialized = true
		}
		// Scope to the goat's shed so the SM-4 sweeper batches one vaccination drive per shed (the
		// operational "one shed = one drive" rule), falling back to park then tenant when the goat
		// has no shed/park location. The obligation-shift handler already re-scopes to shed, so
		// generation must stamp shed scope to stay consistent across the goat's lifecycle.
		scopeType, scopeID := generationScope(tenantID, g)
		keyDue := obligationKeyDue(rule, baseDue, due)
		key := obligationKey(tenantID, versionID, rule.RuleID, "goat", g.GoatID, keyDue.UTC().Format(time.RFC3339), strconv.Itoa(int(rule.Sequence)))
		missingKey := previousMissingDueDateKey(tenantID, versionID, rule, g)
		status := "scheduled"
		deferReason := deferredReason(g, deferStates)
		if deferReason == "" {
			deferReason = catchUpDeferReason
		}
		deferred := deferReason != ""
		if deferred {
			status = "deferred"
		}
		obID, applied, err := s.obl.InsertObligation(ctx, obldomain.NewObligation{
			TenantID:          tenantID,
			ProtocolVersionID: versionID,
			RuleID:            rule.RuleID,
			TargetType:        "goat",
			TargetID:          g.GoatID,
			ScopeType:         scopeType,
			ScopeID:           scopeID,
			DueAt:             due,
			Status:            status,
			IdempotencyKey:    key,
			Sequence:          rule.Sequence,
		})
		if err != nil {
			return err
		}
		if !applied {
			if missingKey != "" {
				if _, _, err := s.obl.CancelOpenObligationByIdempotencyKey(ctx, tenantID, missingKey, "missing_due_date_resolved", asOf); err != nil {
					return err
				}
			}
			if deferred {
				_, changed, err := s.obl.DeferOpenObligationByIdempotencyKey(ctx, tenantID, key, deferReason, asOf)
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
				_, changed, err := s.obl.ReopenDeferredObligationByIdempotencyKey(ctx, tenantID, key, asOf)
				if err != nil {
					return err
				}
				if changed {
					res.Reopened++
				}
			}
			continue // replay no-op
		}
		res.Generated++
		if missingKey != "" {
			if _, _, err := s.obl.CancelOpenObligationByIdempotencyKey(ctx, tenantID, missingKey, "missing_due_date_resolved", asOf); err != nil {
				return err
			}
		}

		if deferred {
			// Same payload shape as DeferOpenObligationByIdempotencyKey's 'deferred' event (no drift):
			// {reason, defer_status}. The event key carries occurredAt so repeated defer cycles each
			// record an event (symmetric with the reopen event key).
			payload, _ := json.Marshal(map[string]string{"reason": "defer_state", "defer_status": deferReason})
			if _, _, err := s.obl.RecordStatusEvent(ctx, obldomain.NewStatusEvent{
				TenantID:       tenantID,
				ObligationID:   obID,
				EventType:      "deferred",
				OccurredAt:     asOf,
				Payload:        payload,
				IdempotencyKey: obID + ":deferred:" + asOf.UTC().Format(time.RFC3339Nano),
				Scope:          "obligation.status_event",
				RequestHash:    "defer:" + deferReason,
			}); err != nil {
				return err
			}
			res.Deferred++
		}
	}
	return nil
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
	windowEnd := baseDue.Add(time.Duration(rule.DueWindowDays) * 24 * time.Hour)
	return asOf.After(windowEnd) && materializedDue.Equal(asOf)
}

func (s *GenerationService) genMissingDueDateObligation(ctx context.Context, tenantID, versionID string, rule protodomain.Rule, g domain.EligibleGoat, asOf time.Time, res *domain.GenerateResult) error {
	reason := missingDueDateReason(rule, g)
	if reason == "" {
		reason = "missing_due_date"
	}
	scopeType, scopeID := generationScope(tenantID, g)
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

func previousMissingDueDateKey(tenantID, versionID string, rule protodomain.Rule, g domain.EligibleGoat) string {
	switch rule.TriggerType {
	case "birth_age":
		if g.DOB != nil {
			return missingDueDateKey(tenantID, versionID, rule, g, "missing_dob")
		}
	case "post_arrival":
		if g.EntryDate != nil {
			return missingDueDateKey(tenantID, versionID, rule, g, "missing_entry_date")
		}
	}
	return ""
}

// GenerateForGoat generates obligations for ONE goat across all published vaccination versions it
// is eligible for. Used by the goat.created handler (event-driven SM-1). Idempotent.
func (s *GenerationService) GenerateForGoat(ctx context.Context, tenantID, goatID string, asOf time.Time) (domain.GenerateResult, error) {
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
	for _, versionID := range versionIDs {
		v, err := s.proto.GetVersion(ctx, tenantID, versionID)
		if err != nil {
			return res, err
		}
		dsl, err := parseGenerationDSL(v.RuleDsl)
		if err != nil {
			return res, err
		}
		if !goatMatchesEligibility(g, dsl.Eligibility, asOf) {
			if _, err := s.obl.CancelOpenVaccinationObligationsForGoatVersion(ctx, tenantID, goatID, versionID, "ineligible_after_shift", asOf); err != nil {
				return res, err
			}
			continue
		}
		rules, err := s.proto.ListRules(ctx, tenantID, versionID)
		if err != nil {
			return res, err
		}
		pagePlans = append(pagePlans, goatGenerationPlan{
			versionID:   versionID,
			rules:       rules,
			deferState:  dsl.Eligibility.DeferStates,
			goat:        g,
			opts:        generationOptions{},
			eligibility: dsl.Eligibility,
		})
	}
	trustedByVersion, err := s.trustedEvidenceForPlans(ctx, tenantID, pagePlans, asOf)
	if err != nil {
		return res, err
	}
	for _, p := range pagePlans {
		if err := s.genOneGoat(ctx, tenantID, p.versionID, p.rules, p.deferState, p.goat, asOf, p.opts, trustedByVersion[p.versionID], &res); err != nil {
			return res, err
		}
	}
	return res, nil
}

func inCare(lifecycle string) bool {
	switch lifecycle {
	case "alive", "sick", "under_treatment", "quarantine", "icu":
		return true
	default:
		return false
	}
}

func generationScope(tenantID string, g domain.EligibleGoat) (string, string) {
	scopeType, scopeID := "tenant", tenantID
	if g.ParkID != "" {
		scopeType, scopeID = "park", g.ParkID
	}
	if g.ShedID != "" {
		scopeType, scopeID = "shed", g.ShedID
	}
	return scopeType, scopeID
}

func missingDueDateReason(rule protodomain.Rule, g domain.EligibleGoat) string {
	switch rule.TriggerType {
	case "birth_age":
		if g.DOB == nil {
			return "missing_dob"
		}
	case "post_arrival":
		if g.EntryDate == nil {
			return "missing_entry_date"
		}
	}
	return ""
}

func applyMissedDosePolicy(rule protodomain.Rule, due, asOf time.Time) (time.Time, string, bool) {
	if rule.DueWindowDays <= 0 {
		return due, "", false
	}
	windowEnd := due.Add(time.Duration(rule.DueWindowDays) * 24 * time.Hour)
	if !asOf.After(windowEnd) {
		return due, "", false
	}
	switch strings.ToLower(strings.TrimSpace(rule.CatchUp)) {
	case "", "immediate":
		return asOf, "", false
	case "phc_approval":
		return asOf, "catch_up_phc_approval", false
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

func trustedEvidenceDue(rule protodomain.Rule, due, asOf time.Time) (time.Time, bool) {
	adjusted, _, skip := applyMissedDosePolicy(rule, due, asOf)
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
		next := due
		for !next.Add(time.Duration(rule.DueWindowDays) * 24 * time.Hour).After(asOf) {
			next = next.AddDate(0, 0, int(interval))
		}
		return next, true
	case "yearly":
		next := due
		for !next.Add(time.Duration(rule.DueWindowDays) * 24 * time.Hour).After(asOf) {
			next = next.AddDate(1, 0, 0)
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
		case "sick", "under_treatment", "quarantine", "icu":
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

func deferStateSet(allowed []string) map[string]bool {
	if len(allowed) == 0 {
		return map[string]bool{
			"sick":            true,
			"under_treatment": true,
			"quarantine":      true,
			"icu":             true,
		}
	}
	allowedStates := make(map[string]bool, len(allowed))
	for _, state := range allowed {
		normalized := strings.ToLower(strings.TrimSpace(state))
		if normalized != "" {
			allowedStates[normalized] = true
		}
	}
	return allowedStates
}

func eligibilityStage(e genEligibility) string {
	if e.AnimalStage != "" {
		return e.AnimalStage
	}
	return e.Stage
}

func goatMatchesEligibility(g domain.EligibleGoat, e genEligibility, asOf time.Time) bool {
	if s := normDim(eligibilityStage(e)); s != "" && !sameDim(s, g.Stage) {
		return false
	}
	if s := normDim(e.Sex); s != "" && !sameDim(s, g.Sex) {
		return false
	}
	if s := normDim(e.Breed); s != "" && !sameDim(s, g.Breed) {
		return false
	}
	if !lifecycleMatches(g.LifecycleStatus, e.Lifecycle) {
		return false
	}
	if s := normDim(e.Health); s != "" && !sameDim(s, g.HealthStatus) {
		return false
	}
	if s := normDim(e.Reproductive); s != "" && !sameDim(s, g.ReproductiveStatus) {
		return false
	}
	if s := normDim(e.AgeBand); s != "" && !sameDim(s, g.AgeBand) {
		return false
	}
	for _, excluded := range e.ExcludeReproductiveStates {
		if normDim(excluded) != "" && sameDim(excluded, g.ReproductiveStatus) {
			return false
		}
	}
	if !ageMatches(g, e, asOf) {
		return false
	}
	return true
}

func lifecycleMatches(actual string, allowed genStringList) bool {
	if len(allowed) == 0 {
		return true
	}
	actual = normDim(actual)
	for _, value := range allowed {
		if s := normDim(value); s != "" && sameDim(s, actual) {
			return true
		}
	}
	return false
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
	y, m, d := start.UTC().Date()
	startDate := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	y, m, d = end.UTC().Date()
	endDate := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return int(endDate.Sub(startDate).Hours() / 24)
}

// dueAt computes a rule's due date for a goat. ok=false with skip=false means the trigger is not an
// SM-1 trigger (after_previous_completion → SM-7, manual_campaign). skip=true means an SM-1 trigger
// that cannot be scheduled for this goat (missing dob/entry_date).
func dueAt(rule protodomain.Rule, g domain.EligibleGoat, asOf time.Time, opts generationOptions) (due time.Time, ok bool, skip bool) {
	off := time.Duration(rule.OffsetDays) * 24 * time.Hour
	switch rule.TriggerType {
	case "birth_age":
		if g.DOB == nil {
			return time.Time{}, false, true
		}
		return g.DOB.Add(off), true, false
	case "post_arrival":
		if g.EntryDate == nil {
			return time.Time{}, false, true
		}
		return g.EntryDate.Add(off), true, false
	case "calendar":
		return asOf.Add(off), true, false
	case "manual_campaign":
		if opts.ManualCampaignID == "" {
			return time.Time{}, false, false
		}
		return asOf.Add(off), true, false
	default: // after_previous_completion (SM-7)
		return time.Time{}, false, false
	}
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
		SkippedNoDueDate:           run.SkippedNoDueDate,
		SuppressedByTrustedHistory: run.SuppressedByTrustedHistory,
	}
}
