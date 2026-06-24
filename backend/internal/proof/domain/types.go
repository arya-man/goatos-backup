// Package domain holds backend-owned proof/media artifact types.
package domain

import "time"

type Artifact struct {
	ProofID         string
	TenantID        string
	StorageProvider string
	ObjectKey       string
	ContentHash     string
	MimeType        string
	SizeBytes       int64
	DurationMS      *int64
	UploadState     string
	ScopeType       string
	ScopeID         string
	SubjectType     string
	SubjectID       *string
	ProofType       string
	UploadedBy      *string
	Metadata        map[string]any
	CreatedAt       time.Time
	UploadedAt      *time.Time
	UpdatedAt       time.Time
	RowVersion      int
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
	UploadURL   string
	Method      string
	Headers     map[string]string
	ExpiresAt   time.Time
	Proof       Artifact
	DownloadURL string
}

type StoredObject struct {
	ContentHash string
	MimeType    string
	SizeBytes   int64
}
