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
	// ErrIdempotencyConflict is returned when a CreateProof call reuses an Idempotency-Key
	// already bound to a different logical request (different scope/subject/mime/type) —
	// a same-key exact replay is NOT an error, it returns the original Artifact.
	ErrIdempotencyConflict = errors.New("proof: idempotency key belongs to a different request")
)

type Repository interface {
	CreateProof(ctx context.Context, in domain.CreateUpload, provider string) (domain.Artifact, error)
	GetProof(ctx context.Context, tenantID, proofID string) (domain.Artifact, error)
	GetProofsByIDs(ctx context.Context, tenantID string, proofIDs []string) (map[string]domain.Artifact, error)
	CompleteProof(ctx context.Context, in domain.CompleteUpload) (domain.Artifact, error)
	ApplyRetention(ctx context.Context, tenantID string, proofIDs []string, policy string, expiresAt *time.Time) (int, error)
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
