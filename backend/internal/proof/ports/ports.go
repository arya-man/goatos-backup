// Package ports declares proof/media storage and persistence boundaries.
package ports

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/vgoats/goatos/backend/internal/proof/domain"
)

var (
	ErrNotFound          = errors.New("proof: not found")
	ErrUnsupported       = errors.New("proof: unsupported storage operation")
	ErrIntegrityMismatch = errors.New("proof: storage object integrity mismatch")
	// ErrObjectMissing is returned when the proof ROW exists but its stored object is missing or
	// unreadable. This is a KNOWN, terminal failure class (evidence unavailable) — distinct from
	// ErrNotFound (no such proof row) and from an unexpected server fault. Callers must NOT retry.
	ErrObjectMissing = errors.New("proof: stored object is missing or unreadable")
	ErrInUse         = errors.New("proof: artifact is already attached")
	ErrForbidden     = errors.New("proof: forbidden")
	// ErrIdempotencyConflict is returned when a CreateProof call reuses an Idempotency-Key
	// already bound to a different logical request (different scope/subject/mime/type) —
	// a same-key exact replay is NOT an error, it returns the original Artifact.
	ErrIdempotencyConflict = errors.New("proof: idempotency key belongs to a different request")
)

type Repository interface {
	CreateProof(ctx context.Context, in domain.CreateUpload, provider string) (domain.Artifact, error)
	GetProof(ctx context.Context, tenantID, proofID string) (domain.Artifact, error)
	GetProofsByIDs(ctx context.Context, tenantID string, proofIDs []string) (map[string]domain.Artifact, error)
	ListUploadedProofs(ctx context.Context, query domain.ListUploadedProofsQuery) ([]domain.Artifact, error)
	CompleteProof(ctx context.Context, in domain.CompleteUpload) (domain.Artifact, error)
	DeleteUnattachedProof(ctx context.Context, tenantID, proofID, actorID string) (domain.Artifact, error)
	ApplyRetention(ctx context.Context, tenantID string, proofIDs []string, policy string, expiresAt *time.Time) (int, error)
	BackfillSubmissionRetention(ctx context.Context, before time.Time, limit int) (int, error)
	PurgeExpired(ctx context.Context, before time.Time, limit int) (int, error)
	PurgeAbandonedUploads(ctx context.Context, before time.Time, limit int) (int, error)
}

type Storage interface {
	Provider() string
	PrepareUpload(ctx context.Context, proof domain.Artifact, expires time.Duration) (domain.UploadTarget, error)
	PrepareDownload(ctx context.Context, proof domain.Artifact, expires time.Duration) (string, error)
	FinalizeUpload(ctx context.Context, proof domain.Artifact, in domain.CompleteUpload) (domain.StoredObject, error)
	Store(ctx context.Context, proof domain.Artifact, body io.Reader, mimeType string) (domain.StoredObject, error)
}

type DeletingStorage interface {
	Delete(ctx context.Context, proof domain.Artifact) error
}

// ObjectStatter answers the single question "are this proof's bytes still there?" without
// downloading them. It exists for ONE caller shape: an irreversible decision about a SINGLE item
// (verification approve). It must never be used to decorate a list/queue read — one stat per proof
// per row is the N+1 that ports.Storage deliberately avoids on hot reads.
//
// A missing or unreadable object is reported as ErrObjectMissing (terminal, non-retryable). A
// transport/permission fault is returned as its own error so callers can tell "the evidence is
// gone" apart from "we could not check right now".
type ObjectStatter interface {
	StatObject(ctx context.Context, proof domain.Artifact) error
}

type SignedURLVerifier interface {
	Verify(method, path, tenantID, expires, signature string, now time.Time) bool
}

type ReadSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
}

type LocalOpener interface {
	Open(ctx context.Context, proof domain.Artifact) (ReadSeekCloser, error)
}
