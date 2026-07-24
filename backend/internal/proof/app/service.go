// Package app coordinates backend-owned proof/media artifact use-cases.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	"github.com/vgoats/goatos/backend/internal/proof/domain"
	"github.com/vgoats/goatos/backend/internal/proof/ports"
	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
)

const defaultSignedURLTTL = 15 * time.Minute

var ErrInvalid = errors.New("proof: invalid input")

type Service struct {
	repo    ports.Repository
	storage ports.Storage
	now     func() time.Time
	ttl     time.Duration
}

func NewService(repo ports.Repository, storage ports.Storage) *Service {
	return &Service{repo: repo, storage: storage, now: time.Now, ttl: defaultSignedURLTTL}
}

func (s *Service) CreateUpload(ctx context.Context, in domain.CreateUpload) (domain.UploadTarget, error) {
	normalizeCreate(&in)
	if err := validateCreate(in); err != nil {
		return domain.UploadTarget{}, err
	}
	proof, err := s.repo.CreateProof(ctx, in, s.storage.Provider())
	if err != nil {
		return domain.UploadTarget{}, err
	}
	target, err := s.storage.PrepareUpload(ctx, proof, s.ttl)
	if err != nil {
		return domain.UploadTarget{}, err
	}
	target.Proof = proof
	if target.ExpiresAt.IsZero() {
		target.ExpiresAt = s.now().Add(s.ttl).UTC()
	}
	if strings.TrimSpace(target.UploadProtocol) == "" {
		target.UploadProtocol = "simple_put"
	}
	return target, nil
}

func (s *Service) CompleteUpload(ctx context.Context, in domain.CompleteUpload) (domain.Artifact, error) {
	normalizeComplete(&in)
	if err := validateComplete(in); err != nil {
		return domain.Artifact{}, err
	}
	proof, err := s.repo.GetProof(ctx, in.TenantID, in.ProofID)
	if err != nil {
		return domain.Artifact{}, err
	}
	if proof.UploadState == "completed" {
		return proof, nil
	}
	stored, err := s.storage.FinalizeUpload(ctx, proof, in)
	if err != nil {
		return domain.Artifact{}, err
	}
	in.ContentHash = stored.ContentHash
	in.MimeType = stored.MimeType
	in.SizeBytes = stored.SizeBytes
	return s.repo.CompleteProof(ctx, in)
}

func (s *Service) StoreUpload(ctx context.Context, tenantID, proofID, mimeType string, body io.Reader) (domain.Artifact, error) {
	if !uuidutil.IsUUIDString(tenantID) || !uuidutil.IsUUIDString(proofID) {
		return domain.Artifact{}, ErrInvalid
	}
	proof, err := s.repo.GetProof(ctx, tenantID, proofID)
	if err != nil {
		return domain.Artifact{}, err
	}
	if proof.UploadState == "completed" {
		return domain.Artifact{}, ErrInvalid
	}
	stored, err := s.storage.Store(ctx, proof, body, strings.TrimSpace(mimeType))
	if err != nil {
		return domain.Artifact{}, err
	}
	proof.ContentHash = stored.ContentHash
	proof.MimeType = stored.MimeType
	proof.SizeBytes = stored.SizeBytes
	return proof, nil
}

func (s *Service) DownloadURL(ctx context.Context, tenantID, proofID string) (string, error) {
	_, url, err := s.DownloadArtifact(ctx, tenantID, proofID)
	return url, err
}

func (s *Service) DownloadArtifact(ctx context.Context, tenantID, proofID string) (domain.Artifact, string, error) {
	if !uuidutil.IsUUIDString(tenantID) || !uuidutil.IsUUIDString(proofID) {
		return domain.Artifact{}, "", ErrInvalid
	}
	proof, err := s.repo.GetProof(ctx, tenantID, proofID)
	if err != nil {
		return domain.Artifact{}, "", err
	}
	url, err := s.storage.PrepareDownload(ctx, proof, s.ttl)
	if err != nil {
		return domain.Artifact{}, "", err
	}
	return proof, url, nil
}

func (s *Service) OpenLocalDownload(ctx context.Context, tenantID, proofID string) (domain.Artifact, ports.ReadSeekCloser, error) {
	if !uuidutil.IsUUIDString(tenantID) || !uuidutil.IsUUIDString(proofID) {
		return domain.Artifact{}, nil, ErrInvalid
	}
	opener, ok := s.storage.(ports.LocalOpener)
	if !ok {
		return domain.Artifact{}, nil, ports.ErrUnsupported
	}
	proof, err := s.repo.GetProof(ctx, tenantID, proofID)
	if err != nil {
		return domain.Artifact{}, nil, err
	}
	reader, err := opener.Open(ctx, proof)
	if err != nil {
		return domain.Artifact{}, nil, err
	}
	return proof, reader, nil
}

func (s *Service) ResolveProofRefs(ctx context.Context, tenantID string, binding sopdomain.ProofBinding, refs []sopdomain.ProofReference) ([]sopdomain.ProofReference, error) {
	if !uuidutil.IsUUIDString(tenantID) || !uuidutil.IsUUIDString(binding.TaskID) {
		return nil, ErrInvalid
	}
	proofIDs := make([]string, 0, len(refs))
	seen := map[string]struct{}{}
	for _, ref := range refs {
		proofID := strings.TrimSpace(ref.ProofID)
		if !uuidutil.IsUUIDString(proofID) {
			return nil, ErrInvalid
		}
		if _, ok := seen[proofID]; ok {
			continue
		}
		seen[proofID] = struct{}{}
		proofIDs = append(proofIDs, proofID)
	}
	if len(proofIDs) == 0 {
		return nil, nil
	}
	proofs, err := s.repo.GetProofsByIDs(ctx, tenantID, proofIDs)
	if err != nil {
		return nil, err
	}
	out := make([]sopdomain.ProofReference, 0, len(proofIDs))
	for _, proofID := range proofIDs {
		proof, ok := proofs[proofID]
		if !ok {
			return nil, ports.ErrNotFound
		}
		if proof.UploadState != "completed" || !proofBoundToTask(proof, binding) || !subjectBoundToTaskScope(proof, binding) {
			return nil, ErrInvalid
		}
		out = append(out, sopdomain.ProofReference{
			ProofID:     proof.ProofID,
			ProofType:   proof.ProofType,
			SubjectType: proof.SubjectType,
			SubjectID:   proof.SubjectID,
			UploadState: proof.UploadState,
			Metadata: map[string]any{
				"storage_provider": proof.StorageProvider,
				"scope_type":       proof.ScopeType,
				"scope_id":         proof.ScopeID,
				"mime_type":        proof.MimeType,
				"size_bytes":       proof.SizeBytes,
				"duration_ms":      proof.DurationMS,
				"content_hash":     proof.ContentHash,
			},
		})
	}
	return out, nil
}

func proofBoundToTask(proof domain.Artifact, binding sopdomain.ProofBinding) bool {
	if proof.ScopeType == "task" && proof.ScopeID == binding.TaskID {
		return true
	}
	// A shed-level proof (shed_level_video) is captured against the task's own scope
	// (e.g. scope_type='shed', scope_id=<the task's shed>). It is bound to the task when
	// its scope matches the task's scope — consistent with ShedCompletionReadiness, which
	// accepts a shed-scoped proof for the same task.
	if binding.ScopeType != "" && proof.ScopeType == binding.ScopeType && proof.ScopeID == binding.ScopeID {
		return true
	}
	return false
}

func subjectBoundToTaskScope(proof domain.Artifact, binding sopdomain.ProofBinding) bool {
	if proof.SubjectID == nil || *proof.SubjectID == "" {
		return true
	}
	switch proof.SubjectType {
	case "task":
		return *proof.SubjectID == binding.TaskID
	case binding.ScopeType:
		return *proof.SubjectID == binding.ScopeID
	default:
		return true
	}
}

func (s *Service) VerifySignedURL(method, path, tenantID, expires, signature string) bool {
	verifier, ok := s.storage.(ports.SignedURLVerifier)
	if !ok {
		return false
	}
	return verifier.Verify(method, path, tenantID, expires, signature, s.now())
}

func normalizeCreate(in *domain.CreateUpload) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.ProofType = strings.TrimSpace(in.ProofType)
	in.MimeType = strings.TrimSpace(in.MimeType)
	in.ScopeType = strings.TrimSpace(in.ScopeType)
	in.ScopeID = strings.TrimSpace(in.ScopeID)
	in.SubjectType = strings.TrimSpace(in.SubjectType)
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	if in.SubjectID != nil {
		v := strings.TrimSpace(*in.SubjectID)
		in.SubjectID = &v
	}
	if in.UploadedBy != nil {
		v := strings.TrimSpace(*in.UploadedBy)
		in.UploadedBy = &v
	}
	if in.Metadata == nil {
		in.Metadata = map[string]any{}
	}
	in.Metadata = sanitizeClientMetadata(in.Metadata)
}

func normalizeComplete(in *domain.CompleteUpload) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.ProofID = strings.TrimSpace(in.ProofID)
	in.ContentHash = strings.TrimSpace(in.ContentHash)
	in.MimeType = strings.TrimSpace(in.MimeType)
	if in.Metadata == nil {
		in.Metadata = map[string]any{}
	}
	in.Metadata = sanitizeClientMetadata(in.Metadata)
}

func sanitizeClientMetadata(metadata map[string]any) map[string]any {
	out := make(map[string]any, len(metadata))
	for key, value := range metadata {
		normalized := strings.ToLower(strings.TrimSpace(key))
		if reservedMetadataKeys[normalized] {
			continue
		}
		out[key] = value
	}
	return out
}

var reservedMetadataKeys = map[string]bool{
	"tenant_id":          true,
	"proof_id":           true,
	"object_key":         true,
	"storage_provider":   true,
	"upload_state":       true,
	"scope_type":         true,
	"scope_id":           true,
	"subject_type":       true,
	"subject_id":         true,
	"task_id":            true,
	"sop_task_id":        true,
	"content_hash":       true,
	"mime_type":          true,
	"size_bytes":         true,
	"duration_ms":        true,
	"gcs_generation":     true,
	"gcs_metageneration": true,
}

func validateCreate(in domain.CreateUpload) error {
	if !uuidutil.IsUUIDString(in.TenantID) || !uuidutil.IsUUIDString(in.ScopeID) {
		return ErrInvalid
	}
	if in.SubjectID != nil && *in.SubjectID != "" && !uuidutil.IsUUIDString(*in.SubjectID) {
		return ErrInvalid
	}
	if in.UploadedBy != nil && *in.UploadedBy != "" && !uuidutil.IsUUIDString(*in.UploadedBy) {
		return ErrInvalid
	}
	if !oneOf(in.ProofType, "photo", "video", "attachment") {
		return ErrInvalid
	}
	if !oneOf(in.ScopeType, "tenant", "farm", "park", "shed", "cohort", "batch", "task", "goat") {
		return ErrInvalid
	}
	if !oneOf(in.SubjectType, "batch", "goat", "shed", "task", "vial_lot", "administration", "other") {
		return ErrInvalid
	}
	if in.ProofType == "video" {
		if in.UploadedBy == nil || *in.UploadedBy == "" {
			return ErrInvalid
		}
		captureSource, _ := in.Metadata["capture_source"].(string)
		if !oneOf(captureSource, "in_app_camera", "gallery_picker") {
			return ErrInvalid
		}
		if captureSource == "gallery_picker" && in.SubjectType != "shed" {
			return ErrInvalid
		}
		start, startOK := metadataNumber(in.Metadata["captured_start_ms"])
		end, endOK := metadataNumber(in.Metadata["captured_end_ms"])
		if !startOK || !endOK || start <= 0 || end < start {
			return ErrInvalid
		}
	}
	if in.SubjectType == "goat" && (in.SubjectID == nil || *in.SubjectID == "") {
		return ErrInvalid
	}
	return nil
}

func validateComplete(in domain.CompleteUpload) error {
	if !uuidutil.IsUUIDString(in.TenantID) || !uuidutil.IsUUIDString(in.ProofID) {
		return ErrInvalid
	}
	if in.SizeBytes < 0 {
		return ErrInvalid
	}
	if in.DurationMS != nil && *in.DurationMS < 0 {
		return ErrInvalid
	}
	return nil
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func metadataNumber(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		number, err := typed.Float64()
		return number, err == nil
	default:
		return 0, false
	}
}
