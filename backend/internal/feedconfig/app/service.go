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
	"regexp"
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
	absoluteKgScale = 3 // feed_experiment_config.absolute_kg  numeric(12,3) -- legacy rows only
	// feed_experiment_config.grams_per_head numeric(12,3). Same scale as the ration grid's rate,
	// because it is the same kind of number: grams per animal per day.
	experimentGramsScale = 3
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
func (s *Service) ListRationRates(ctx context.Context, tenantID string, f RationRateFilter) (domain.RationRatePage, error) {
	if strings.TrimSpace(tenantID) == "" {
		return domain.RationRatePage{}, ErrMissingTenant
	}
	if strings.TrimSpace(f.ParkID) == "" {
		return domain.RationRatePage{}, ErrMissingPark
	}
	page, err := resolvePage(f.Limit, f.Offset)
	if err != nil {
		return domain.RationRatePage{}, err
	}
	compare, err := resolveGramsComparison(f.GramsOp, f.GramsValue)
	if err != nil {
		return domain.RationRatePage{}, err
	}
	return s.repo.ListRationRates(ctx, domain.RationRateQuery{
		TenantID:     tenantID,
		ParkID:       strings.TrimSpace(f.ParkID),
		RationGroup:  strings.TrimSpace(f.RationGroup),
		Breed:        strings.TrimSpace(f.Breed),
		ShedTag:      strings.TrimSpace(f.ShedTag),
		FeedItems:    cleanStrings(f.FeedItems),
		GramsCompare: compare,
		Page:         page,
	})
}

// RationRateFilter is the read's narrowing input.
//
// A STRUCT rather than more positional parameters. The grid now filters on park, ration group,
// breed, shed tag, a SET of feed items and a comparison against the rate itself; as a parameter
// list that is nine same-typed arguments in a row, where transposing two of them compiles cleanly
// and silently filters the grid by the wrong column.
type RationRateFilter struct {
	ParkID      string
	RationGroup string
	// Breed resolves through the breed -> ration-group map; see domain.RationRateQuery.Breed for
	// why it is a different filter from RationGroup rather than an alias for it.
	Breed     string
	ShedTag   string
	FeedItems []string
	// GramsOp and GramsValue are the two halves of ONE filter and are validated as a pair: either
	// both are present or neither is. Accepting one alone would mean inventing the other, and both
	// inventions answer a question nobody asked -- a default operator silently reinterprets the
	// value, and a default value silently reinterprets the operator.
	GramsOp    string
	GramsValue string
	Limit      *int32
	Offset     *int32
}

// ErrInvalidFilter is returned for a filter the caller expressed wrongly -- an unknown comparison
// operator, a non-numeric comparison value, or half a comparison. It maps to 400, never to an empty
// page: silently returning no rows for a malformed filter reads on screen as "no rates are
// configured", which on this screen means "these animals are blocked" and is a different fact.
var ErrInvalidFilter = errors.New("feedconfig: invalid filter")

// gramsValuePattern is the exact-decimal shape numeric(12,3) accepts from this filter: an optional
// sign, digits, and at most three decimal places.
//
// Validated as TEXT and passed on as text. Parsing to float64 to check it would reintroduce exactly
// the round-tripping this module keeps decimal strings to avoid, and would let 1e309 through as
// +Inf. The database does the actual comparison in numeric.
var gramsValuePattern = regexp.MustCompile(`^-?\d{1,9}(\.\d{1,3})?$`)

func resolveGramsComparison(op, value string) (*domain.GramsComparison, error) {
	op = strings.TrimSpace(op)
	value = strings.TrimSpace(value)
	if op == "" && value == "" {
		return nil, nil
	}
	if op == "" || value == "" {
		return nil, fmt.Errorf("%w: grams comparison needs both an operator and a value", ErrInvalidFilter)
	}
	parsed, ok := domain.ParseGramsOp(op)
	if !ok {
		return nil, fmt.Errorf("%w: unknown grams comparison operator %q", ErrInvalidFilter, op)
	}
	if !gramsValuePattern.MatchString(value) {
		return nil, fmt.Errorf("%w: grams comparison value %q is not an exact decimal", ErrInvalidFilter, value)
	}
	return &domain.GramsComparison{Op: parsed, Value: value}, nil
}

// cleanStrings trims and drops blanks, and returns nil for an all-blank set.
//
// nil vs empty matters downstream: the SQL reads a NULL array as "no filter" and a present array as
// "match one of these", so a set of nothing but blanks must collapse to no filter rather than to an
// array that matches nothing.
func cleanStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, value := range in {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
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
		TenantID: tenantID, ParkID: strings.TrimSpace(parkID), AsOfDate: s.businessDate(), Page: page,
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
// ListPens returns the park's operational locations for the experiment enroller: every active shed,
// and every pen of a subdivided shed, flagged with whether it already carries experiment config.
//
// park_id is REQUIRED for the same reason it is on every other read here -- the ration grid, the
// session split and the dispatch clock are all park-scoped, and a tenant-wide location list would be
// an unbounded read with no screen behind it.
func (s *Service) ListPens(ctx context.Context, tenantID, parkID string, limit, offset *int32) (domain.PenPage, error) {
	if strings.TrimSpace(tenantID) == "" {
		return domain.PenPage{}, ErrMissingTenant
	}
	// park_id is OPTIONAL here, matching ListExperimentConfig. Omitted means every pen in the
	// tenant, which is what the enroller needs when the top bar reads company-wide: the experiment
	// table is tenant-wide in that mode, so a park-locked candidate list would offer nothing for the
	// other park's rows. Requiring it made the company-wide enroller receive ZERO pens and render
	// "every pen already has quantities" over a park with 37 free ones.
	page, err := resolvePage(limit, offset)
	if err != nil {
		return domain.PenPage{}, err
	}
	return s.repo.ListPens(ctx, domain.PenQuery{
		TenantID: tenantID,
		ParkID:   strings.TrimSpace(parkID),
		Page:     page,
	})
}

func (s *Service) ListExperimentConfig(ctx context.Context, tenantID string, f ExperimentConfigFilter) (domain.ExperimentConfigPage, error) {
	parkID, shedID, status := f.ParkID, f.ShedID, f.Status
	limit, offset := f.Limit, f.Offset
	if strings.TrimSpace(tenantID) == "" {
		return domain.ExperimentConfigPage{}, ErrMissingTenant
	}
	// park_id is OPTIONAL on THIS read alone. Every other read here is park-owned, but an experiment
	// cell carries its own park, so an absent park means "the whole tenant's authored experiments" --
	// which is what a company-wide scope must be able to show. Without this the screen silently
	// rendered one park's pens and called it everything.
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
	compare, err := resolveGramsComparison(f.KgOp, f.KgValue)
	if err != nil {
		return domain.ExperimentConfigPage{}, err
	}
	return s.repo.ListExperimentConfig(ctx, domain.ExperimentConfigQuery{
		TenantID: tenantID,
		ParkID:   strings.TrimSpace(parkID),
		ShedID:   strings.TrimSpace(shedID),
		// Not validated against the shed's catalog here. This is a READ: a partition that matches
		// nothing simply returns no pens, which is the honest answer, and rejecting it would need a
		// per-request catalog lookup on a filter the operator picked from a list we supplied.
		PartitionLabel:     strings.TrimSpace(f.PartitionLabel),
		Status:             normalizedStatus,
		FeedItems:          cleanStrings(f.FeedItems),
		ExperimentCategory: strings.TrimSpace(f.ExperimentCategory),
		GramsCompare:       compare,
		Page:               page,
	})
}

// ExperimentConfigFilter is the experiment section's narrowing input.
//
// A struct for the same reason RationRateFilter is one, and it deliberately mirrors that type: the
// two sections of Feed Config filter on the same shapes (a set of feed items, a comparison against
// the authored quantity), so an author who learns one has learned the other.
//
// Both quantities are now a per-head RATE IN GRAMS (maintainer decision 2026-09-01): an experiment
// cell is authored per animal, exactly as a ration-grid cell is, and the two sections differ in
// where the rate comes FROM rather than in what it means. The wire names keep their `kg_` prefix
// only so an in-flight bookmark or client does not break; the number they compare is grams per
// animal, and a legacy pen-total cell is claimed by neither side of the comparison (see
// domain.ExperimentConfigQuery.GramsCompare).
type ExperimentConfigFilter struct {
	ParkID string
	ShedID string
	// PartitionLabel narrows to ONE PEN of the selected shed. Blank means every pen of it.
	PartitionLabel     string
	Status             string
	FeedItems          []string
	ExperimentCategory string
	// KgOp and KgValue are validated as a pair by the same rule as the grid's grams comparison:
	// both or neither, and an unrecognised operator is rejected rather than dropped.
	KgOp    string
	KgValue string
	Limit   *int32
	Offset  *int32
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

// SetSessionTemplateItemInput declares a feed on one session's recipe, or withdraws it.
//
// Declared is a plain bool rather than a status string because there are exactly two states and the
// author is choosing between them. There is no quantity here on purpose: grams live in the ration
// grid, keyed by ration group and shed tag, because one slot feeds every group at a different rate.
type SetSessionTemplateItemInput struct {
	TenantID      string
	ActorRef      string
	ParkID        string
	SessionNo     *int32
	FeedItemLabel string
	Declared      *bool

	IdempotencyKey     string
	RequestFingerprint string
}

// SetSessionTemplateItem validates the slot write and hands it to the repository.
//
// SessionNo and Declared are pointers so ABSENT is distinguishable from a zero value. Defaulting
// either would be dangerous in opposite directions: a missing session_no would silently target
// session 0 (which no park runs), and a missing `declared` would have to guess between adding a feed
// and taking one away.
func (s *Service) SetSessionTemplateItem(ctx context.Context, in SetSessionTemplateItemInput) (domain.WriteResult, error) {
	identity, err := s.writeIdentity(in.TenantID, in.ActorRef, in.IdempotencyKey, in.RequestFingerprint)
	if err != nil {
		return domain.WriteResult{}, err
	}
	parkID, err := domain.RequireNonBlank("park_id", in.ParkID)
	if err != nil {
		return domain.WriteResult{}, err
	}
	item, err := domain.RequireNonBlank("feed_item", in.FeedItemLabel)
	if err != nil {
		return domain.WriteResult{}, err
	}
	if in.SessionNo == nil {
		return domain.WriteResult{}, &domain.FieldError{Field: "session_no", Reason: domain.ErrMissingField}
	}
	// Mirrors feed_session_template_items_session_no_check. Rejected here rather than left to the
	// constraint so the author gets a field error instead of a database violation.
	if *in.SessionNo < 1 {
		return domain.WriteResult{}, &domain.FieldError{Field: "session_no", Reason: domain.ErrValueOutOfRange}
	}
	if in.Declared == nil {
		return domain.WriteResult{}, &domain.FieldError{Field: "declared", Reason: domain.ErrMissingField}
	}
	return s.repo.SetSessionTemplateItem(ctx, domain.SetSessionTemplateItemCommand{
		WriteIdentity: identity,
		ParkID:        parkID,
		SessionNo:     *in.SessionNo,
		FeedItemLabel: item,
		Declared:      *in.Declared,
	})
}

// CreateFeedItemInput adds one entry to the tenant's feed-item catalog.
//
// NO park_id, on purpose: feed_item_catalog is keyed (tenant, feed_item_key), so the vocabulary is
// shared by every park. This is the only write in the module that is not park-scoped.
//
// The three nutritional attributes are *string for a DIFFERENT reason than GramsPerHead's pointer
// above, and the difference matters. There, nil is rejected because absence of a rate is a blocking
// state that must never be filled in. Here, nil is ACCEPTED and stored as NULL, because the column
// is genuinely nullable and the consequence is bounded: a missing energy value blocks a rollup, not
// a feeding decision. What the pointer buys is the same distinction either way -- an unmeasured
// attribute stays distinguishable from a measured 0, and neither is invented from the other.
//
// A PRESENT but out-of-range attribute is still rejected with a field error, never clamped into the
// column's CHECK range.
type CreateFeedItemInput struct {
	TenantID        string
	ActorRef        string
	FeedItemLabel   string
	EnergyKcalPerKg *string
	DryMatterFactor *string
	WastageFactor   *string
	DisplayOrder    *int32

	IdempotencyKey     string
	RequestFingerprint string
}

// CreateFeedItem validates and adds one catalog entry.
//
// WHAT THIS WRITE DOES NOT DO, stated because the screen it serves sits next to the ration grid:
// it authors no rate, no shed factor and no experiment quantity. The new label becomes SELECTABLE
// on those surfaces immediately, and every combination using it stays unconfigured -- and therefore
// blocking -- until someone authors it. Adding a convenience "seed a 0 rate for the new item" step
// here would author "feed none of it" for every group and tag in the tenant, which is the exact
// blank-is-not-zero collapse this module exists to prevent.
func (s *Service) CreateFeedItem(ctx context.Context, in CreateFeedItemInput) (domain.WriteResult, error) {
	identity, err := s.writeIdentity(in.TenantID, in.ActorRef, in.IdempotencyKey, in.RequestFingerprint)
	if err != nil {
		return domain.WriteResult{}, err
	}
	label, err := domain.RequireNonBlank("feed_item", in.FeedItemLabel)
	if err != nil {
		return domain.WriteResult{}, err
	}
	// Each optional attribute is validated ONLY when present. An absent one stays nil and is stored
	// as NULL -- "not measured" -- rather than being normalized into a 0 that claims it was.
	energy, err := normalizeOptional(in.EnergyKcalPerKg, func(raw string) (string, error) {
		return domain.NormalizeEnergyKcalPerKg("energy_kcal_per_kg", raw)
	})
	if err != nil {
		return domain.WriteResult{}, err
	}
	dryMatter, err := normalizeOptional(in.DryMatterFactor, func(raw string) (string, error) {
		return domain.NormalizeDryMatterFactor("dry_matter_factor", raw)
	})
	if err != nil {
		return domain.WriteResult{}, err
	}
	wastage, err := normalizeOptional(in.WastageFactor, func(raw string) (string, error) {
		return domain.NormalizeWastageFactor("wastage_factor", raw)
	})
	if err != nil {
		return domain.WriteResult{}, err
	}
	displayOrder, err := domain.ValidateDisplayOrder("display_order", in.DisplayOrder)
	if err != nil {
		return domain.WriteResult{}, err
	}
	return s.repo.CreateFeedItem(ctx, domain.CreateFeedItemCommand{
		WriteIdentity:   identity,
		FeedItemLabel:   label,
		EnergyKcalPerKg: energy,
		DryMatterFactor: dryMatter,
		WastageFactor:   wastage,
		DisplayOrder:    displayOrder,
	})
}

// SetFeedItemStatusInput retires one feed item, or restores a retired one.
//
// The one authored field is the status. Nothing else about the item is editable here on purpose:
// this is the "remove it" action, and letting it also rewrite an item's energy or wastage would put
// an in-place edit of measured attributes behind a control that says Retire.
type SetFeedItemStatusInput struct {
	TenantID   string
	ActorRef   string
	FeedItemID string
	Status     string

	IdempotencyKey     string
	RequestFingerprint string
}

// SetFeedItemStatus takes a feed item out of every future feed sheet, or puts it back.
//
// Not a display toggle -- generation reads the catalog `WHERE status = 'active'` -- so this is
// validated as strictly as any other authored write: an unrecognised status is rejected rather than
// coerced, because one value keeps the item in every sheet and the other removes it from all of them.
func (s *Service) SetFeedItemStatus(ctx context.Context, in SetFeedItemStatusInput) (domain.WriteResult, error) {
	identity, err := s.writeIdentity(in.TenantID, in.ActorRef, in.IdempotencyKey, in.RequestFingerprint)
	if err != nil {
		return domain.WriteResult{}, err
	}
	feedItemID, err := domain.RequireNonBlank("feed_item_id", in.FeedItemID)
	if err != nil {
		return domain.WriteResult{}, err
	}
	status, err := domain.ValidateFeedItemStatus("status", in.Status)
	if err != nil {
		return domain.WriteResult{}, err
	}
	return s.repo.SetFeedItemStatus(ctx, domain.SetFeedItemStatusCommand{
		WriteIdentity: identity,
		FeedItemID:    feedItemID,
		Status:        status,
	})
}

// normalizeOptional runs a validator over a value only when the caller SENT one.
//
// The nil passthrough is the whole point: it keeps "the author did not fill this in" out of the
// validators entirely, so no validator can accidentally turn an absent attribute into a canonical
// "0.000". A present-but-blank string is NOT treated as absent -- it reaches the validator, which
// rejects it as a missing field, because a client that sent the key meant to send a value.
func normalizeOptional(raw *string, normalize func(string) (string, error)) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	normalized, err := normalize(*raw)
	if err != nil {
		return nil, err
	}
	return &normalized, nil
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

// UpsertExperimentConfigInput authors one experiment pen's GRAMS PER ANIMAL of one feed item.
//
// GramsPerHead is a *string for exactly the same absent-vs-zero reason as the ration grid's, and the
// stakes are the mirror image. On the grid a missing rate BLOCKS the shed loudly; here a missing row
// is silent — the pen simply stops being an experiment pen and gets fed off the ration grid instead,
// on a sheet that looks complete. So:
//
//	nil    -- ABSENT. Rejected. It is not "feed nothing" and it is not "leave it as it was".
//	"0"    -- the author entered zero. Accepted: an arm that deliberately gets none of an item.
//	"-5"   -- present but out of range. Rejected with a field error, never clamped.
//
// THE FIGURE IS PER ANIMAL and the generator multiplies it by the pen's LIVE head count. Nothing
// here asks for a head count: the pen's population is a fact the herd register answers live, on the
// sheet and on the config screen alike, so there is no figure for an author to restate and no stale
// copy of one to go wrong (maintainer instruction 2026-09-01, "use live only, forget recorded").
type UpsertExperimentConfigInput struct {
	TenantID string
	ActorRef string
	ParkID   string
	ShedID   string
	// PartitionLabel names which PEN of the shed this cell belongs to. Empty is legitimate (an
	// undivided shed) and is NOT rejected -- but on a partitioned shed an empty label authors the
	// shed-wide 'whole' row rather than a pen, so the client must send the pen it rendered.
	PartitionLabel     string
	FeedItemLabel      string
	GramsPerHead       *string
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
	if in.GramsPerHead == nil {
		return domain.WriteResult{}, &domain.FieldError{Field: "grams_per_head", Reason: domain.ErrMissingField}
	}
	// allowZero=true: 0 g is a legal authored quantity (an arm that deliberately gets none of an
	// item). Negative is not, and is rejected rather than clamped.
	grams, err := domain.NormalizeDecimal("grams_per_head", *in.GramsPerHead, experimentGramsScale, true)
	if err != nil {
		return domain.WriteResult{}, err
	}
	return s.repo.UpsertExperimentConfig(ctx, domain.UpsertExperimentConfigCommand{
		WriteIdentity:      identity,
		ParkID:             parkID,
		ShedID:             shedID,
		PartitionLabel:     strings.TrimSpace(in.PartitionLabel),
		FeedItemLabel:      item,
		GramsPerHead:       grams,
		ExperimentCategory: category,
	})
}

// ExperimentBatchCellInput is one authored feed item inside a batch enrolment.
//
// GramsPerHead is a *string for the same absent-vs-zero reason as the single-cell write. A cell the
// author left BLANK must not be in this slice at all; a cell that is here and carries nil is an
// error, not an instruction to feed nothing.
type ExperimentBatchCellInput struct {
	FeedItemLabel string
	GramsPerHead  *string
}

// UpsertExperimentConfigBatchInput enrolls every feed item of ONE unconfigured pen atomically.
type UpsertExperimentConfigBatchInput struct {
	TenantID           string
	ActorRef           string
	ParkID             string
	ShedID             string
	PartitionLabel     string
	ExperimentCategory string
	Cells              []ExperimentBatchCellInput

	IdempotencyKey     string
	RequestFingerprint string
}

// UpsertExperimentConfigBatch validates and enrolls a whole pen's authored quantities atomically.
//
// Every cell is validated BEFORE the transaction opens. A batch that would reject its fourth cell
// must not have written its first three: the point of this endpoint is that a pen is never left
// half-authored, and validating inside the loop that writes would make the guarantee depend on
// rollback rather than on never having started.
func (s *Service) UpsertExperimentConfigBatch(ctx context.Context, in UpsertExperimentConfigBatchInput) (domain.WriteResult, error) {
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
	category, err := domain.RequireNonBlank("experiment_category", in.ExperimentCategory)
	if err != nil {
		return domain.WriteResult{}, err
	}
	// An empty batch is a caller mistake, not a no-op: it would enrol a pen onto the experiment
	// workflow with nothing authored, which the planner reads as "fed nothing".
	if len(in.Cells) == 0 {
		return domain.WriteResult{}, &domain.FieldError{Field: "items", Reason: domain.ErrMissingField}
	}
	// Duplicate feed items are rejected rather than de-duplicated. Two cells naming the same item
	// carry two different authored quantities; inside one INSERT they race and the survivor is
	// arbitrary, so silently keeping one would store a number the author did not choose. Compared on
	// the NORMALIZED key, because that is what the unique index collapses them on.
	seen := make(map[string]struct{}, len(in.Cells))
	cells := make([]domain.ExperimentBatchCell, 0, len(in.Cells))
	for _, cell := range in.Cells {
		item, err := domain.RequireNonBlank("feed_item", cell.FeedItemLabel)
		if err != nil {
			return domain.WriteResult{}, err
		}
		key := domain.NormalizeFeedItemKey(item)
		if _, dup := seen[key]; dup {
			return domain.WriteResult{}, &domain.FieldError{Field: "items", Reason: domain.ErrDuplicateFeedItem}
		}
		seen[key] = struct{}{}
		if cell.GramsPerHead == nil {
			return domain.WriteResult{}, &domain.FieldError{Field: "grams_per_head", Reason: domain.ErrMissingField}
		}
		grams, err := domain.NormalizeDecimal("grams_per_head", *cell.GramsPerHead, experimentGramsScale, true)
		if err != nil {
			return domain.WriteResult{}, err
		}
		cells = append(cells, domain.ExperimentBatchCell{FeedItemLabel: item, GramsPerHead: grams})
	}

	return s.repo.UpsertExperimentConfigBatch(ctx, domain.UpsertExperimentConfigBatchCommand{
		WriteIdentity:      identity,
		ParkID:             parkID,
		ShedID:             shedID,
		PartitionLabel:     strings.TrimSpace(in.PartitionLabel),
		ExperimentCategory: category,
		Cells:              cells,
	})
}

// SetExperimentShedStatusInput switches ONE PEN between the experiment workflow and the normal
// per-head ration grid.
//
// Status is REQUIRED and validated, never defaulted: the two values are the two workflows, and
// picking one for a caller who did not say would change what a shed's animals are fed.
type SetExperimentShedStatusInput struct {
	TenantID string
	ActorRef string
	ParkID   string
	ShedID   string
	// PartitionLabel names the PEN being switched. Required for a subdivided shed and blank for an
	// undivided one; the adapter validates it against the shed's own catalog. Without it this
	// switch retired every pen of the shed while the UI captioned it with one pen's name.
	PartitionLabel string
	Status         string

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
		WriteIdentity:  identity,
		ParkID:         parkID,
		ShedID:         shedID,
		PartitionLabel: strings.TrimSpace(in.PartitionLabel),
		Status:         status,
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
