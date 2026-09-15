// Package ports declares what the Animal purchases application needs from persistence and the
// proof store.
package ports

import (
	"context"
	"errors"
	"time"

	"github.com/vgoats/goatos/backend/internal/animalpurchase/domain"
)

var (
	ErrLoadNotFound        = errors.New("animal purchase: load not found")
	ErrCandidateNotFound   = errors.New("animal purchase: candidate not found")
	ErrLoadRefTaken        = errors.New("animal purchase: load number already used")
	ErrVendorNotFound      = errors.New("animal purchase: vendor not found")
	ErrLoadClosed          = errors.New("animal purchase: load is closed")
	ErrAlreadyDecided      = errors.New("animal purchase: candidate already decided")
	ErrRowVersionMismatch  = errors.New("animal purchase: candidate changed since it was read")
	ErrIdempotencyConflict = errors.New("animal purchase: idempotency key reused with a different payload")
	ErrInvalidVideo        = errors.New("animal purchase: video proof is not a finished in-app-camera upload")
	// ErrMediaAlreadyUsed: a capture already belongs to another animal (one photo is one animal's evidence).
	ErrMediaAlreadyUsed = errors.New("animal purchase: capture already attached to another animal")
)

// LoadPage is one keyset page of loads.
type LoadPage struct {
	Loads      []domain.Load
	NextCursor string
}

// CandidatePage is one keyset page of animals (a load's list, or the review queue).
type CandidatePage struct {
	Candidates []domain.Candidate
	NextCursor string
	// Counts are WHOLE-FILTER decision counts (the load's, or the review queue's), never page sums.
	Counts domain.DecisionCounts
}

// ReviewQuery is the CEO's queue filter.
type ReviewQuery struct {
	LoadID   string
	Decision string // "", pending, accepted, rejected
	// Recorded-on window as absolute instants derived from IST business dates by the service:
	// From is the start of the first day (inclusive), To the start of the day AFTER the last
	// (exclusive), so the predicate stays on the bare created_at column. Zero means open.
	From, To time.Time
	Cursor   domain.Cursor
	Limit    int
}

// CreateLoadParams is the load write.
type CreateLoadParams struct {
	TenantID string
	// QuestionnaireVersion is the SOP version whose load form the phone rendered; 0 = published.
	QuestionnaireVersion int
	Write                domain.LoadWrite
	ActorID              string
	IdempotencyKey       string
}

// AddCandidateParams is the animal write.
type AddCandidateParams struct {
	TenantID string
	LoadID   string
	// QuestionnaireVersion is the SOP version the phone rendered; 0 means "the published one".
	QuestionnaireVersion int
	Write                domain.CandidateWrite
	ActorID              string
	IdempotencyKey       string
}

// DecideParams is the CEO's decision write.
type DecideParams struct {
	TenantID       string
	CandidateID    string
	Write          domain.DecisionWrite
	ActorID        string
	ActorName      string
	IdempotencyKey string
}

// Repository is the persistence seam.
type Repository interface {
	CreateLoad(ctx context.Context, p CreateLoadParams) (domain.Load, error)
	ListLoads(ctx context.Context, tenantID string, cursor domain.Cursor, limit int) (LoadPage, error)
	GetLoad(ctx context.Context, tenantID, loadID string) (domain.Load, error)
	AddCandidate(ctx context.Context, p AddCandidateParams) (domain.Candidate, error)
	ListCandidates(ctx context.Context, tenantID, loadID string, cursor domain.Cursor, limit int) (CandidatePage, error)
	ListReview(ctx context.Context, tenantID string, q ReviewQuery) (CandidatePage, error)
	GetCandidate(ctx context.Context, tenantID, candidateID string) (domain.Candidate, error)
	Decide(ctx context.Context, p DecideParams) (domain.Candidate, error)
	// BreedSuggestions are the breeds the herd already carries, so the form offers the farm's
	// own spelling first; the breed field stays free text.
	BreedSuggestions(ctx context.Context, tenantID string) ([]string, error)
}

// CatalogSource serves the inspection questionnaire by SOP version (PROCUREMENT SOP, maintainer
// decision 2026-09-14): Published is what a phone opening the form today renders; Version is the
// exact document a recorded animal was answered on (published or since retired), used to
// validate its write and label its review. A tenant with no authored version gets the seeded
// catalog. ErrCatalogVersionUnknown when the asked-for version never existed.
var ErrCatalogVersionUnknown = errors.New("animal purchase: inspection sop version unknown")

type CatalogSource interface {
	PublishedCatalog(ctx context.Context, tenantID string) (domain.Catalog, error)
	CatalogVersion(ctx context.Context, tenantID string, version int) (domain.Catalog, error)
}

// ProofValidator asserts every one of a candidate's captures is a finished in-app-camera
// photo or video upload in the caller's tenant, in ONE read. Fails closed with ErrInvalidVideo.
type ProofValidator interface {
	ValidateCandidateMedia(ctx context.Context, tenantID string, proofRefs []string) error
	// ValidateCandidateMediaKinds is the SOP-aware check: every proof ref must be a finished
	// in-app-camera upload AND of a kind its slot accepts (a photo in a video-only slot is
	// refused server-side, not only by the phone's buttons). refsByKind maps each ref to the
	// kinds allowed ("photo" / "video").
	ValidateCandidateMediaKinds(ctx context.Context, tenantID string, allowedByRef map[string][]string) error
}

// ErrMediaKindNotAccepted: a capture is not the kind its SOP slot asks for.
var ErrMediaKindNotAccepted = errors.New("animal purchase: capture kind not accepted by its slot")

// Media is one proof download route with stored metadata.
type Media struct {
	URL          string
	ThumbnailURL string
	MimeType     string
}

// MediaResolver resolves proof metadata for MANY refs at once without signing or reading backing
// objects. A proof it cannot resolve is absent from the map; the caller renders media unavailable
// for that row, never a missing row.
type MediaResolver interface {
	ResolveMedia(ctx context.Context, tenantID string, proofRefs []string) (map[string]Media, error)
}
