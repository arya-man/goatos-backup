// Package app coordinates the authored feed-configuration read/write use-cases.
//
// The service is where paging is bounded and where authored values are VALIDATED OR REJECTED. It
// holds no SQL and no HTTP: the repository owns persistence and effective dating, the handler owns
// wire shape, and this layer owns the rules that must hold no matter which of them calls.
package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/feedconfig/domain"
	"github.com/vgoats/goatos/backend/internal/feedconfig/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

var (
	ErrMissingTenant = errors.New("feedconfig: missing tenant context")
	ErrMissingPark   = errors.New("feedconfig: park_id is required")
	ErrMissingActor  = errors.New("feedconfig: actor is required")
	// ErrInvalidPaging is returned for a PRESENT but out-of-range limit/offset. It is never silently
	// clamped: a caller who asked for 5000 rows and got 100 without being told has been given a
	// truncated grid it believes is complete.
	ErrInvalidPaging = errors.New("feedconfig: invalid paging")
	// ErrMissingIdempotencyKey is returned when a write arrives without a client key. Writes here are
	// retried by browsers and flaky networks; without a key the retry authors a second row.
	ErrMissingIdempotencyKey = errors.New("feedconfig: idempotency key is required")
)

const (
	// Paging bounds. These are AUTHORED CONFIG tables (1442 rates, 31 tags, 10 items, 7 groups), so
	// the ceiling is set by what a screen can usefully render, not by what the table could hold.
	defaultLimit = int32(50)
	maxLimit     = int32(200)
	// maxOffset bounds the offset walk. A caller paging past this is not reading a screen, and an
	// unbounded, growable offset is exactly the pagination anti-pattern the scale rules ban.
	maxOffset = int32(5000)

	// Column scales, mirroring migration 000003. Authored values are rejected rather than rounded to
	// fit, so these must stay in step with the schema.
	gramsScale      = 3 // feed_ration_rates.grams_per_head    numeric(12,3)
	multiplierScale = 4 // feed_shed_factors.multiplier        numeric(8,4)
	absoluteKgScale = 3 // feed_experiment_config.absolute_kg  numeric(12,3)
)

// Clock lets tests pin the business date. Production passes nil and gets the real clock.
type Clock func() time.Time

type Service struct {
	repo ports.Repository
	now  Clock
}

func NewService(repo ports.Repository) *Service {
	return &Service{repo: repo, now: time.Now}
}

// WithClock pins the service's business clock. Test-only seam: an effective-dated write's outcome
// depends on whether the open row was authored TODAY, so a test that cannot fix "today" cannot
// distinguish a supersede from a same-day correction.
func (s *Service) WithClock(now Clock) *Service {
	if now != nil {
		s.now = now
	}
	return s
}

// businessDate is the Asia/Kolkata business date an edit takes effect on.
//
// Derived from the India business calendar, never from SQL now() and never from the client. A
// late-evening IST write would otherwise be dated to the previous UTC day, which for an
// effective-dated config row means the edit claims to have been in force yesterday.
func (s *Service) businessDate() string {
	return biztime.BusinessDate(s.now())
}

// ---------------------------------------------------------------------------
// Reads
// ---------------------------------------------------------------------------

// resolvePage validates a caller's paging window. Absent values take the declared default; a
// present-but-out-of-range value is an error, never a coerced value.
func resolvePage(limit, offset *int32) (domain.Page, error) {
	page := domain.Page{Limit: defaultLimit, Offset: 0}
	if limit != nil {
		if *limit < 1 || *limit > maxLimit {
			return domain.Page{}, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidPaging, maxLimit)
		}
		page.Limit = *limit
	}
	if offset != nil {
		if *offset < 0 || *offset > maxOffset {
			return domain.Page{}, fmt.Errorf("%w: offset must be between 0 and %d", ErrInvalidPaging, maxOffset)
		}
		page.Offset = *offset
	}
	return page, nil
}

// ListRationRates serves one page of the editable grid for a park.
//
// park_id is REQUIRED, not optional-with-a-tenant-wide-fallback. Rates are park-scoped because CBE
// and CPT genuinely differ on two rows (see migration 000003); a tenant-wide listing would
// interleave two parks' rates under identical group/tag/item labels and give the author no way to
// tell which park a row belongs to.
func (s *Service) ListRationRates(ctx context.Context, tenantID, parkID, rationGroup, shedTag, feedItem string, limit, offset *int32) (domain.RationRatePage, error) {
	if strings.TrimSpace(tenantID) == "" {
		return domain.RationRatePage{}, ErrMissingTenant
	}
	if strings.TrimSpace(parkID) == "" {
		return domain.RationRatePage{}, ErrMissingPark
	}
	page, err := resolvePage(limit, offset)
	if err != nil {
		return domain.RationRatePage{}, err
	}
	return s.repo.ListRationRates(ctx, domain.RationRateQuery{
		TenantID:    tenantID,
		ParkID:      strings.TrimSpace(parkID),
		RationGroup: strings.TrimSpace(rationGroup),
		ShedTag:     strings.TrimSpace(shedTag),
		FeedItem:    strings.TrimSpace(feedItem),
		Page:        page,
	})
}

// ListRationGroups serves the breed -> ration-group map. Tenant-scoped, not park-scoped: the merge
// it carries (Beetal + Sirohi share one group) is a property of the breeds, not of a park.
func (s *Service) ListRationGroups(ctx context.Context, tenantID string, limit, offset *int32) (domain.RationGroupPage, error) {
	if strings.TrimSpace(tenantID) == "" {
		return domain.RationGroupPage{}, ErrMissingTenant
	}
	page, err := resolvePage(limit, offset)
	if err != nil {
		return domain.RationGroupPage{}, err
	}
	return s.repo.ListRationGroups(ctx, tenantID, page)
}

// ListShedTags serves the authored tag vocabulary, optionally narrowed to the kid or adult course.
func (s *Service) ListShedTags(ctx context.Context, tenantID, appliesTo string, limit, offset *int32) (domain.ShedTagPage, error) {
	if strings.TrimSpace(tenantID) == "" {
		return domain.ShedTagPage{}, ErrMissingTenant
	}
	// A present-but-unrecognised applies_to is rejected rather than ignored: silently dropping the
	// filter would return the FULL tag list to a caller who asked for kid tags only.
	normalized, err := domain.ValidateAppliesTo("applies_to", appliesTo)
	if err != nil {
		return domain.ShedTagPage{}, err
	}
	page, err := resolvePage(limit, offset)
	if err != nil {
		return domain.ShedTagPage{}, err
	}
	return s.repo.ListShedTags(ctx, domain.ShedTagQuery{TenantID: tenantID, AppliesTo: normalized, Page: page})
}

// ListFeedItems serves the feed-item catalog.
func (s *Service) ListFeedItems(ctx context.Context, tenantID string, limit, offset *int32) (domain.FeedItemPage, error) {
	if strings.TrimSpace(tenantID) == "" {
		return domain.FeedItemPage{}, ErrMissingTenant
	}
	page, err := resolvePage(limit, offset)
	if err != nil {
		return domain.FeedItemPage{}, err
	}
	return s.repo.ListFeedItems(ctx, tenantID, page)
}

// ListSessionTemplates serves a park's feeding-session split.
func (s *Service) ListSessionTemplates(ctx context.Context, tenantID, parkID string, limit, offset *int32) (domain.SessionTemplatePage, error) {
	if strings.TrimSpace(tenantID) == "" {
		return domain.SessionTemplatePage{}, ErrMissingTenant
	}
	if strings.TrimSpace(parkID) == "" {
		return domain.SessionTemplatePage{}, ErrMissingPark
	}
	page, err := resolvePage(limit, offset)
	if err != nil {
		return domain.SessionTemplatePage{}, err
	}
	return s.repo.ListSessionTemplates(ctx, domain.SessionTemplateQuery{
		TenantID: tenantID, ParkID: strings.TrimSpace(parkID), Page: page,
	})
}

// ListScheduleConfig serves a park's dispatch clocks, optionally narrowed to one workflow.
func (s *Service) ListScheduleConfig(ctx context.Context, tenantID, parkID, workflow string, limit, offset *int32) (domain.ScheduleConfigPage, error) {
	if strings.TrimSpace(tenantID) == "" {
		return domain.ScheduleConfigPage{}, ErrMissingTenant
	}
	if strings.TrimSpace(parkID) == "" {
		return domain.ScheduleConfigPage{}, ErrMissingPark
	}
	// Same reasoning as applies_to above: an unrecognised workflow filter must not quietly widen the
	// result to both workflows, which run on deliberately different clocks.
	normalizedWorkflow := ""
	if strings.TrimSpace(workflow) != "" {
		var err error
		normalizedWorkflow, err = domain.ValidateWorkflow("workflow", workflow)
		if err != nil {
			return domain.ScheduleConfigPage{}, err
		}
	}
	page, err := resolvePage(limit, offset)
	if err != nil {
		return domain.ScheduleConfigPage{}, err
	}
	return s.repo.ListScheduleConfig(ctx, domain.ScheduleConfigQuery{
		TenantID: tenantID, ParkID: strings.TrimSpace(parkID), Workflow: normalizedWorkflow, Page: page,
	})
}

// ListShedFactors serves a park's per-shed multipliers.
//
// A shed with NO factor row is not an error and is not a zero: the read path treats absence as 1.0,
// which is safe because it cannot zero out a ration. This listing therefore returns only AUTHORED
// factors and never synthesizes a 1.0 row for every shed -- doing so would make an unauthored shed
// indistinguishable from one someone deliberately set to 1.0.
func (s *Service) ListShedFactors(ctx context.Context, tenantID, parkID, shedID, feedItem string, limit, offset *int32) (domain.ShedFactorPage, error) {
	if strings.TrimSpace(tenantID) == "" {
		return domain.ShedFactorPage{}, ErrMissingTenant
	}
	if strings.TrimSpace(parkID) == "" {
		return domain.ShedFactorPage{}, ErrMissingPark
	}
	page, err := resolvePage(limit, offset)
	if err != nil {
		return domain.ShedFactorPage{}, err
	}
	return s.repo.ListShedFactors(ctx, domain.ShedFactorQuery{
		TenantID: tenantID,
		ParkID:   strings.TrimSpace(parkID),
		ShedID:   strings.TrimSpace(shedID),
		FeedItem: strings.TrimSpace(feedItem),
		Page:     page,
	})
}

// ListExperimentConfig serves a park's hand-authored EXPERIMENT sheds.
//
// Status is optional and defaults to BOTH, which is deliberate. A withdrawn shed's rows are retired
// rather than deleted, so its authored quantities survive; hiding them by default would force an
// operator restoring a shed to re-key every figure from the workbook, and would also make a shed
// that someone withdrew by mistake invisible on the screen that owns that decision.
func (s *Service) ListExperimentConfig(ctx context.Context, tenantID, parkID, shedID, status string, limit, offset *int32) (domain.ExperimentConfigPage, error) {
	if strings.TrimSpace(tenantID) == "" {
		return domain.ExperimentConfigPage{}, ErrMissingTenant
	}
	if strings.TrimSpace(parkID) == "" {
		return domain.ExperimentConfigPage{}, ErrMissingPark
	}
	// Same reasoning as applies_to and workflow above: an unrecognised status filter must not quietly
	// widen the result. Here it matters more than usual, because the two statuses mean two DIFFERENT
	// WORKFLOWS, and a screen that showed retired rows while claiming to show active ones would
	// misreport which sheds are on absolute kg.
	normalizedStatus, err := domain.ValidateExperimentStatus("status", status, false)
	if err != nil {
		return domain.ExperimentConfigPage{}, err
	}
	page, err := resolvePage(limit, offset)
	if err != nil {
		return domain.ExperimentConfigPage{}, err
	}
	return s.repo.ListExperimentConfig(ctx, domain.ExperimentConfigQuery{
		TenantID: tenantID,
		ParkID:   strings.TrimSpace(parkID),
		ShedID:   strings.TrimSpace(shedID),
		Status:   normalizedStatus,
		Page:     page,
	})
}

// ---------------------------------------------------------------------------
// Writes
// ---------------------------------------------------------------------------

// UpsertRationRateInput is the validated-at-the-edge shape of a grid edit.
//
// GramsPerHead is a *string, and that pointer is load-bearing. It distinguishes:
//
//	nil    -- the field was ABSENT. Rejected. Absence of a rate means "not configured", a BLOCKING
//	          state; a write path that filled it with 0 would author "feed nothing" instead.
//	"0"    -- the author entered zero. Accepted. Milk-fed K0/K1 kids really are fed 0 g of solids.
//	"-5"   -- present but out of range. Rejected with a field error, never clamped to 0.
//
// This is the same distinction the frontend must preserve: a cleared input is not an explicit 0.
type UpsertRationRateInput struct {
	TenantID         string
	ActorRef         string
	ParkID           string
	RationGroupLabel string
	ShedTagLabel     string
	FeedItemLabel    string
	GramsPerHead     *string

	IdempotencyKey     string
	RequestFingerprint string
}

func (s *Service) UpsertRationRate(ctx context.Context, in UpsertRationRateInput) (domain.WriteResult, error) {
	identity, err := s.writeIdentity(in.TenantID, in.ActorRef, in.IdempotencyKey, in.RequestFingerprint)
	if err != nil {
		return domain.WriteResult{}, err
	}
	parkID, err := domain.RequireNonBlank("park_id", in.ParkID)
	if err != nil {
		return domain.WriteResult{}, err
	}
	group, err := domain.RequireNonBlank("ration_group", in.RationGroupLabel)
	if err != nil {
		return domain.WriteResult{}, err
	}
	tag, err := domain.RequireNonBlank("shed_tag", in.ShedTagLabel)
	if err != nil {
		return domain.WriteResult{}, err
	}
	item, err := domain.RequireNonBlank("feed_item", in.FeedItemLabel)
	if err != nil {
		return domain.WriteResult{}, err
	}
	if in.GramsPerHead == nil {
		// Absent, not zero. See the type comment: this is the single most consequential validation in
		// the module.
		return domain.WriteResult{}, &domain.FieldError{Field: "grams_per_head", Reason: domain.ErrMissingField}
	}
	// allowZero=true: 0 is a legal authored rate. Negative is not, and is rejected rather than
	// clamped.
	grams, err := domain.NormalizeDecimal("grams_per_head", *in.GramsPerHead, gramsScale, true)
	if err != nil {
		return domain.WriteResult{}, err
	}
	return s.repo.UpsertRationRate(ctx, domain.UpsertRationRateCommand{
		WriteIdentity:    identity,
		ParkID:           parkID,
		RationGroupLabel: group,
		ShedTagLabel:     tag,
		FeedItemLabel:    item,
		GramsPerHead:     grams,
	})
}

// UpsertShedFactorInput authors one shed multiplier.
//
// Multiplier is a *string for the same absent-vs-zero reason as GramsPerHead, but its range rule
// differs: 0 IS accepted here because the migration allows an explicitly authored 0 (a shed that is
// deliberately fed nothing of an item), while a missing row reads as the safe 1.0. Negative is
// rejected.
type UpsertShedFactorInput struct {
	TenantID      string
	ActorRef      string
	ParkID        string
	ShedID        string
	FeedItemLabel string
	Multiplier    *string

	IdempotencyKey     string
	RequestFingerprint string
}

func (s *Service) UpsertShedFactor(ctx context.Context, in UpsertShedFactorInput) (domain.WriteResult, error) {
	identity, err := s.writeIdentity(in.TenantID, in.ActorRef, in.IdempotencyKey, in.RequestFingerprint)
	if err != nil {
		return domain.WriteResult{}, err
	}
	parkID, err := domain.RequireNonBlank("park_id", in.ParkID)
	if err != nil {
		return domain.WriteResult{}, err
	}
	shedID, err := domain.RequireNonBlank("shed_id", in.ShedID)
	if err != nil {
		return domain.WriteResult{}, err
	}
	item, err := domain.RequireNonBlank("feed_item", in.FeedItemLabel)
	if err != nil {
		return domain.WriteResult{}, err
	}
	if in.Multiplier == nil {
		return domain.WriteResult{}, &domain.FieldError{Field: "multiplier", Reason: domain.ErrMissingField}
	}
	multiplier, err := domain.NormalizeDecimal("multiplier", *in.Multiplier, multiplierScale, true)
	if err != nil {
		return domain.WriteResult{}, err
	}
	return s.repo.UpsertShedFactor(ctx, domain.UpsertShedFactorCommand{
		WriteIdentity: identity,
		ParkID:        parkID,
		ShedID:        shedID,
		FeedItemLabel: item,
		Multiplier:    multiplier,
	})
}

// UpsertScheduleConfigInput authors one park/workflow dispatch clock.
//
// TransportTime is a **string so three states stay distinguishable: absent (leave whatever policy
// the caller implies -- here, NULL), explicitly null (no declared cutoff), and a value. A single
// *string would collapse "not sent" into "cleared".
type UpsertScheduleConfigInput struct {
	TenantID       string
	ActorRef       string
	ParkID         string
	Workflow       string
	DirectionTime  string
	CorrectionTime string
	TransportTime  *string
	// TransportTimeProvided distinguishes an omitted transport_time from one explicitly sent as null.
	// Both currently store NULL, but they are different author intents and the flag keeps the
	// distinction available rather than baking in today's coincidence.
	TransportTimeProvided bool

	IdempotencyKey     string
	RequestFingerprint string
}

func (s *Service) UpsertScheduleConfig(ctx context.Context, in UpsertScheduleConfigInput) (domain.WriteResult, error) {
	identity, err := s.writeIdentity(in.TenantID, in.ActorRef, in.IdempotencyKey, in.RequestFingerprint)
	if err != nil {
		return domain.WriteResult{}, err
	}
	parkID, err := domain.RequireNonBlank("park_id", in.ParkID)
	if err != nil {
		return domain.WriteResult{}, err
	}
	workflow, err := domain.ValidateWorkflow("workflow", in.Workflow)
	if err != nil {
		return domain.WriteResult{}, err
	}
	direction, err := domain.NormalizeLocalTime("direction_time", in.DirectionTime)
	if err != nil {
		return domain.WriteResult{}, err
	}
	correction, err := domain.NormalizeLocalTime("correction_time", in.CorrectionTime)
	if err != nil {
		return domain.WriteResult{}, err
	}
	var transport *string
	if in.TransportTime != nil {
		// A PRESENT but malformed transport time fails the write. It is not dropped to NULL: NULL means
		// "no declared cutoff", so silently converting a typo into NULL would tell the dispatcher the
		// park has no transport deadline at all.
		normalized, terr := domain.NormalizeLocalTime("transport_time", *in.TransportTime)
		if terr != nil {
			return domain.WriteResult{}, terr
		}
		transport = &normalized
	}
	// Ordering is checked here as well as by the schema so the author gets a field-level message
	// naming which time is wrong, rather than a constraint violation.
	if err := domain.ValidateScheduleOrder(direction, correction, transport); err != nil {
		return domain.WriteResult{}, err
	}
	return s.repo.UpsertScheduleConfig(ctx, domain.UpsertScheduleConfigCommand{
		WriteIdentity:  identity,
		ParkID:         parkID,
		Workflow:       workflow,
		DirectionTime:  direction,
		CorrectionTime: correction,
		TransportTime:  transport,
	})
}

// UpsertExperimentConfigInput authors one experiment shed's ABSOLUTE kg of one feed item.
//
// AbsoluteKg is a *string for exactly the same absent-vs-zero reason as GramsPerHead, and the stakes
// are the mirror image. On the ration grid a missing rate BLOCKS the shed loudly; here a missing row
// is silent — the shed simply stops being an experiment shed and gets fed off the per-head grid at
// roughly twice the authored quantity, on a sheet that looks complete. So:
//
//	nil    -- ABSENT. Rejected. It is not "feed nothing" and it is not "leave it as it was".
//	"0"    -- the author entered zero. Accepted: an arm that deliberately gets none of an item.
//	"-5"   -- present but out of range. Rejected with a field error, never clamped.
//
// HeadCount is INFORMATIONAL and is never multiplied into AbsoluteKg (see domain.ExperimentConfig).
// It is a pointer so "not recorded" stays distinct from an authored 0, which would state the shed is
// empty.
type UpsertExperimentConfigInput struct {
	TenantID           string
	ActorRef           string
	ParkID             string
	ShedID             string
	FeedItemLabel      string
	AbsoluteKg         *string
	HeadCount          *int32
	ExperimentCategory string

	IdempotencyKey     string
	RequestFingerprint string
}

func (s *Service) UpsertExperimentConfig(ctx context.Context, in UpsertExperimentConfigInput) (domain.WriteResult, error) {
	identity, err := s.writeIdentity(in.TenantID, in.ActorRef, in.IdempotencyKey, in.RequestFingerprint)
	if err != nil {
		return domain.WriteResult{}, err
	}
	parkID, err := domain.RequireNonBlank("park_id", in.ParkID)
	if err != nil {
		return domain.WriteResult{}, err
	}
	shedID, err := domain.RequireNonBlank("shed_id", in.ShedID)
	if err != nil {
		return domain.WriteResult{}, err
	}
	item, err := domain.RequireNonBlank("feed_item", in.FeedItemLabel)
	if err != nil {
		return domain.WriteResult{}, err
	}
	// The arm is required rather than defaulted because it is what the direction sheet prints in the
	// shed-tag column for an experiment shed — the operator's only cue that these numbers are
	// hand-entered rather than computed. A blank there reads as an ordinary untagged row.
	category, err := domain.RequireNonBlank("experiment_category", in.ExperimentCategory)
	if err != nil {
		return domain.WriteResult{}, err
	}
	if in.AbsoluteKg == nil {
		return domain.WriteResult{}, &domain.FieldError{Field: "absolute_kg", Reason: domain.ErrMissingField}
	}
	// allowZero=true: 0 kg is a legal authored quantity. Negative is not, and is rejected rather than
	// clamped.
	kg, err := domain.NormalizeDecimal("absolute_kg", *in.AbsoluteKg, absoluteKgScale, true)
	if err != nil {
		return domain.WriteResult{}, err
	}
	headCount, err := domain.ValidateHeadCount("head_count", in.HeadCount)
	if err != nil {
		return domain.WriteResult{}, err
	}
	return s.repo.UpsertExperimentConfig(ctx, domain.UpsertExperimentConfigCommand{
		WriteIdentity:      identity,
		ParkID:             parkID,
		ShedID:             shedID,
		FeedItemLabel:      item,
		AbsoluteKg:         kg,
		HeadCount:          headCount,
		ExperimentCategory: category,
	})
}

// SetExperimentShedStatusInput switches a WHOLE SHED between the experiment workflow and the normal
// per-head ration grid.
//
// Status is REQUIRED and validated, never defaulted: the two values are the two workflows, and
// picking one for a caller who did not say would change what a shed's animals are fed.
type SetExperimentShedStatusInput struct {
	TenantID string
	ActorRef string
	ParkID   string
	ShedID   string
	Status   string

	IdempotencyKey     string
	RequestFingerprint string
}

func (s *Service) SetExperimentShedStatus(ctx context.Context, in SetExperimentShedStatusInput) (domain.WriteResult, error) {
	identity, err := s.writeIdentity(in.TenantID, in.ActorRef, in.IdempotencyKey, in.RequestFingerprint)
	if err != nil {
		return domain.WriteResult{}, err
	}
	parkID, err := domain.RequireNonBlank("park_id", in.ParkID)
	if err != nil {
		return domain.WriteResult{}, err
	}
	shedID, err := domain.RequireNonBlank("shed_id", in.ShedID)
	if err != nil {
		return domain.WriteResult{}, err
	}
	status, err := domain.ValidateExperimentStatus("status", in.Status, true)
	if err != nil {
		return domain.WriteResult{}, err
	}
	return s.repo.SetExperimentShedStatus(ctx, domain.SetExperimentShedStatusCommand{
		WriteIdentity: identity,
		ParkID:        parkID,
		ShedID:        shedID,
		Status:        status,
	})
}

// writeIdentity assembles the idempotency envelope shared by every authored write and enforces the
// three things none of them may ship without: a tenant, an actor for the audit trail, and a client
// idempotency key plus fingerprint.
func (s *Service) writeIdentity(tenantID, actorRef, key, fingerprint string) (domain.WriteIdentity, error) {
	if strings.TrimSpace(tenantID) == "" {
		return domain.WriteIdentity{}, ErrMissingTenant
	}
	if strings.TrimSpace(actorRef) == "" {
		return domain.WriteIdentity{}, ErrMissingActor
	}
	if strings.TrimSpace(key) == "" || strings.TrimSpace(fingerprint) == "" {
		return domain.WriteIdentity{}, ErrMissingIdempotencyKey
	}
	return domain.WriteIdentity{
		TenantID:           tenantID,
		ActorRef:           strings.TrimSpace(actorRef),
		EffectiveFrom:      s.businessDate(),
		IdempotencyKey:     strings.TrimSpace(key),
		RequestFingerprint: strings.TrimSpace(fingerprint),
	}, nil
}
