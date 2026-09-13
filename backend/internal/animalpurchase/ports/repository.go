// Package ports declares what the Animal purchases application needs from persistence and the
// proof store.
package ports

import (
	"context"
	"errors"

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
	Cursor   domain.Cursor
	Limit    int
}

// CreateLoadParams is the load write.
type CreateLoadParams struct {
	TenantID       string
	Write          domain.LoadWrite
	ActorID        string
	IdempotencyKey string
}

// AddCandidateParams is the animal write.
type AddCandidateParams struct {
	TenantID       string
	LoadID         string
	Write          domain.CandidateWrite
	ActorID        string
	IdempotencyKey string
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

// ProofValidator asserts every one of a candidate's captures is a finished in-app-camera
// photo or video upload in the caller's tenant, in ONE read. Fails closed with ErrInvalidVideo.
type ProofValidator interface {
	ValidateCandidateMedia(ctx context.Context, tenantID string, proofRefs []string) error
}

// Media is one signed playback link.
type Media struct {
	URL      string
	MimeType string
}

// MediaResolver signs proofs for playback, MANY AT ONCE (one repository read per page, never one
// per row). A proof it cannot sign is absent from the map; the caller renders "video unavailable"
// for that row, never a missing row.
type MediaResolver interface {
	ResolveMedia(ctx context.Context, tenantID string, proofRefs []string) (map[string]Media, error)
}
