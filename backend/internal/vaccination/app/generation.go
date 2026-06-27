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
	AnimalStage               string   `json:"animal_stage"`
	Stage                     string   `json:"stage"`
	Sex                       string   `json:"sex"`
	Breed                     string   `json:"breed"`
	Health                    string   `json:"health"`
	Reproductive              string   `json:"reproductive"`
	ExcludeReproductiveStates []string `json:"exclude_reproductive_states"`
	DeferStates               []string `json:"defer_states"`
}

type genDSL struct {
	Eligibility genEligibility `json:"eligibility"`
}

// normDim maps "all"/"any"/"" to "" (no filter); any other value is an exact-match dimension.
func normDim(v string) string {
	switch v {
	case "", "all", "any":
		return ""
	default:
		return v
	}
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

	var dsl genDSL
	if len(v.RuleDsl) > 0 {
		_ = json.Unmarshal(v.RuleDsl, &dsl)
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
	after := ""
	for {
		goats, err := s.goats.ListEligibleGoatsForGeneration(ctx, filter, after, s.page)
		if err != nil {
			return res, err
		}
		if len(goats) == 0 {
			break
		}
		for _, g := range goats {
			if !goatMatchesEligibility(g, elig) {
				continue
			}
			if err := s.genOneGoat(ctx, tenantID, versionID, rules, elig.DeferStates, g, asOf, opts, &res); err != nil {
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

// genOneGoat applies every applicable rule to one goat: compute due_at, idempotent insert, and a
// canonical deferred state when the goat is in a defer state. Accumulates counts into res.
func (s *GenerationService) genOneGoat(ctx context.Context, tenantID, versionID string, rules []protodomain.Rule, deferStates []string, g domain.EligibleGoat, asOf time.Time, opts generationOptions, res *domain.GenerateResult) error {
	for _, rule := range rules {
		due, ok, skip := dueAt(rule, g, asOf, opts)
		if skip {
			res.SkippedNoDueDate++
			continue
		}
		if !ok {
			continue // after_previous_completion → SM-7, manual_campaign → manual
		}
		// evidence is guaranteed non-nil here: both entrypoints call requireEvidenceReader first.
		trusted, err := s.evidence.HasTrustedCompletionEvidence(ctx, tenantID, g.GoatID, versionID, rule.RuleID, rule.DoseCode, due, asOf)
		if err != nil {
			return err
		}
		if trusted {
			res.SuppressedByTrustedHistory++
			continue
		}
		// Scope to the goat's shed so the SM-4 sweeper batches one vaccination drive per shed (the
		// operational "one shed = one drive" rule), falling back to park then tenant when the goat
		// has no shed/park location. The obligation-shift handler already re-scopes to shed, so
		// generation must stamp shed scope to stay consistent across the goat's lifecycle.
		scopeType, scopeID := "tenant", tenantID
		if g.ParkID != "" {
			scopeType, scopeID = "park", g.ParkID
		}
		if g.ShedID != "" {
			scopeType, scopeID = "shed", g.ShedID
		}
		key := obligationKey(tenantID, versionID, rule.RuleID, "goat", g.GoatID, due.UTC().Format(time.RFC3339), strconv.Itoa(int(rule.Sequence)))
		status := "scheduled"
		deferReason := deferredReason(g, deferStates)
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
				IdempotencyKey: obID + ":deferred:" + asOf.UTC().Format(time.RFC3339),
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
	for _, versionID := range versionIDs {
		v, err := s.proto.GetVersion(ctx, tenantID, versionID)
		if err != nil {
			return res, err
		}
		var dsl genDSL
		if len(v.RuleDsl) > 0 {
			_ = json.Unmarshal(v.RuleDsl, &dsl)
		}
		if !goatMatchesEligibility(g, dsl.Eligibility) {
			continue
		}
		rules, err := s.proto.ListRules(ctx, tenantID, versionID)
		if err != nil {
			return res, err
		}
		if err := s.genOneGoat(ctx, tenantID, versionID, rules, dsl.Eligibility.DeferStates, g, asOf, generationOptions{}, &res); err != nil {
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

func deferredReason(g domain.EligibleGoat, allowed []string) string {
	if len(allowed) == 0 {
		return ""
	}
	allowedStates := make(map[string]bool, len(allowed))
	for _, state := range allowed {
		normalized := strings.ToLower(strings.TrimSpace(state))
		if normalized != "" {
			allowedStates[normalized] = true
		}
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

func eligibilityStage(e genEligibility) string {
	if e.AnimalStage != "" {
		return e.AnimalStage
	}
	return e.Stage
}

func goatMatchesEligibility(g domain.EligibleGoat, e genEligibility) bool {
	if s := normDim(eligibilityStage(e)); s != "" && s != g.Stage {
		return false
	}
	if s := normDim(e.Sex); s != "" && s != g.Sex {
		return false
	}
	if s := normDim(e.Breed); s != "" && s != g.Breed {
		return false
	}
	if s := normDim(e.Health); s != "" && s != g.HealthStatus {
		return false
	}
	if s := normDim(e.Reproductive); s != "" && s != g.ReproductiveStatus {
		return false
	}
	for _, excluded := range e.ExcludeReproductiveStates {
		if normDim(excluded) != "" && normDim(excluded) == g.ReproductiveStatus {
			return false
		}
	}
	return true
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
		SkippedNoDueDate:           run.SkippedNoDueDate,
		SuppressedByTrustedHistory: run.SuppressedByTrustedHistory,
	}
}
