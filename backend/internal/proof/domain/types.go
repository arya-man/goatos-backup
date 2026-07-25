// Package domain holds backend-owned proof/media artifact types.
package domain

import "time"

type Artifact struct {
	ProofID            string
	TenantID           string
	StorageProvider    string
	ObjectKey          string
	ContentHash        string
	MimeType           string
	SizeBytes          int64
	DurationMS         *int64
	UploadState        string
	ScopeType          string
	ScopeID            string
	SubjectType        string
	SubjectID          *string
	ProofType          string
	UploadedBy         *string
	Metadata           map[string]any
	RetentionPolicy    string
	RetentionExpiresAt *time.Time
	UploadExpiresAt    *time.Time
	CreatedAt          time.Time
	UploadedAt         *time.Time
	UpdatedAt          time.Time
	RowVersion         int
}

type CreateUpload struct {
	TenantID    string
	ProofType   string
	MimeType    string
	ScopeType   string
	ScopeID     string
	SubjectType string
	SubjectID   *string
	UploadedBy  *string
	Metadata    map[string]any
	// IdempotencyKey is the stable per-capture key the mobile outbox sends verbatim on every
	// retry (Idempotency-Key header). Empty for callers that don't need replay protection.
	// A replay with the SAME key and the SAME logical request returns the original Artifact
	// (never mints a second object); a same-key/different-request replay is rejected with
	// ports.ErrIdempotencyConflict.
	IdempotencyKey string
}

type CompleteUpload struct {
	TenantID    string
	ProofID     string
	ContentHash string
	MimeType    string
	SizeBytes   int64
	DurationMS  *int64
	Metadata    map[string]any
}

type UploadTarget struct {
	UploadURL      string
	Method         string
	Headers        map[string]string
	ExpiresAt      time.Time
	Proof          Artifact
	DownloadURL    string
	UploadProtocol string
	ChunkSizeBytes int64
}

type StoredObject struct {
	ContentHash string
	MimeType    string
	SizeBytes   int64
}
