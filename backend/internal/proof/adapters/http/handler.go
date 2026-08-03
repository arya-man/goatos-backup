// Package http exposes proof/media artifact endpoints.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/proof/app"
	"github.com/vgoats/goatos/backend/internal/proof/domain"
	"github.com/vgoats/goatos/backend/internal/proof/ports"
)

const maxLocalUploadBytes int64 = 2 << 30

type Service interface {
	CreateUpload(ctx context.Context, in domain.CreateUpload) (domain.UploadTarget, error)
	CompleteUpload(ctx context.Context, in domain.CompleteUpload) (domain.Artifact, error)
	StoreUpload(ctx context.Context, tenantID, proofID, mimeType string, body io.Reader) (domain.Artifact, error)
	DownloadURL(ctx context.Context, tenantID, proofID string) (string, error)
	OpenLocalDownload(ctx context.Context, tenantID, proofID string) (domain.Artifact, ports.ReadSeekCloser, error)
	DeleteUpload(ctx context.Context, tenantID, proofID, actorID string) error
	VerifySignedURL(method, path, tenantID, expires, signature string) bool
}

type Handler struct {
	service Service
	log     *slog.Logger
}

func NewHandler(service Service, log ...*slog.Logger) *Handler {
	l := slog.Default()
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	}
	return &Handler{service: service, log: l}
}

func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("POST /app/proofs/uploads", h.CreateUpload)
	mux.HandleFunc("PUT /app/proofs/{proof_id}/upload", h.UploadLocal)
	mux.HandleFunc("POST /app/proofs/{proof_id}/complete", h.CompleteUpload)
	mux.HandleFunc("GET /app/proofs/{proof_id}/download", h.Download)
	mux.HandleFunc("DELETE /app/proofs/{proof_id}", h.DeleteUpload)
}

func RegisterSigned(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("PUT /app/proofs/{proof_id}/upload", h.UploadLocalSigned)
	mux.HandleFunc("GET /app/proofs/{proof_id}/download/signed", h.DownloadSigned)
}

type createUploadRequest struct {
	ProofType   string         `json:"proof_type"`
	MimeType    string         `json:"mime_type"`
	ScopeType   string         `json:"scope_type"`
	ScopeID     string         `json:"scope_id"`
	SubjectType string         `json:"subject_type"`
	SubjectID   *string        `json:"subject_id"`
	Metadata    map[string]any `json:"metadata"`
}

type completeUploadRequest struct {
	ContentHash string         `json:"content_hash"`
	MimeType    string         `json:"mime_type"`
	SizeBytes   int64          `json:"size_bytes"`
	DurationMS  *int64         `json:"duration_ms"`
	Metadata    map[string]any `json:"metadata"`
}

type proofResponse struct {
	ProofID         string         `json:"proof_id"`
	StorageProvider string         `json:"storage_provider"`
	ProofType       string         `json:"proof_type"`
	SubjectType     string         `json:"subject_type"`
	SubjectID       *string        `json:"subject_id"`
	UploadState     string         `json:"upload_state"`
	MimeType        string         `json:"mime_type"`
	SizeBytes       int64          `json:"size_bytes"`
	DurationMS      *int64         `json:"duration_ms,omitempty"`
	ContentHash     string         `json:"content_hash"`
	Metadata        map[string]any `json:"metadata"`
	CreatedAt       time.Time      `json:"created_at"`
	UploadedAt      *time.Time     `json:"uploaded_at,omitempty"`
}

type createUploadResponse struct {
	Proof          proofResponse     `json:"proof"`
	UploadURL      string            `json:"upload_url"`
	UploadMethod   string            `json:"upload_method"`
	Headers        map[string]string `json:"headers"`
	ExpiresAt      time.Time         `json:"expires_at"`
	UploadProtocol string            `json:"upload_protocol"`
	ChunkSizeBytes int64             `json:"chunk_size_bytes,omitempty"`
}

type downloadURLResponse struct {
	DownloadURL string `json:"download_url"`
}

type errorEnvelope struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	TraceID   string `json:"trace_id"`
	Retryable bool   `json:"retryable"`
}

func (h *Handler) CreateUpload(w http.ResponseWriter, r *http.Request) {
	var req createUploadRequest
	if !h.decode(w, r, &req) {
		return
	}
	target, err := h.service.CreateUpload(r.Context(), domain.CreateUpload{
		TenantID:       tenantID(r),
		ProofType:      req.ProofType,
		MimeType:       req.MimeType,
		ScopeType:      req.ScopeType,
		ScopeID:        req.ScopeID,
		SubjectType:    req.SubjectType,
		SubjectID:      req.SubjectID,
		UploadedBy:     actorPtr(r),
		Metadata:       req.Metadata,
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
	})
	if err != nil {
		h.respondErr(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusCreated, createUploadResponse{
		Proof:          toProofResponse(target.Proof),
		UploadURL:      target.UploadURL,
		UploadMethod:   target.Method,
		Headers:        target.Headers,
		ExpiresAt:      target.ExpiresAt,
		UploadProtocol: target.UploadProtocol,
		ChunkSizeBytes: target.ChunkSizeBytes,
	})
}

func (h *Handler) UploadLocal(w http.ResponseWriter, r *http.Request) {
	if !h.verifySignedURL(r, tenantID(r)) {
		httpresponse.WriteError(w, r, h.log, http.StatusForbidden,
			errorEnvelope{Code: "invalid_upload_url", Message: "upload URL is invalid or expired", TraceID: traceID(r)}, nil)
		return
	}
	defer r.Body.Close()
	proof, err := h.service.StoreUpload(r.Context(), tenantID(r), r.PathValue("proof_id"), r.Header.Get("Content-Type"), http.MaxBytesReader(w, r.Body, maxLocalUploadBytes))
	if err != nil {
		h.respondErr(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, map[string]proofResponse{"proof": toProofResponse(proof)})
}

func (h *Handler) UploadLocalSigned(w http.ResponseWriter, r *http.Request) {
	tenantID := signedTenantID(r)
	if !h.verifySignedURL(r, tenantID) {
		httpresponse.WriteError(w, r, h.log, http.StatusForbidden,
			errorEnvelope{Code: "invalid_upload_url", Message: "upload URL is invalid or expired", TraceID: traceID(r)}, nil)
		return
	}
	defer r.Body.Close()
	proof, err := h.service.StoreUpload(r.Context(), tenantID, r.PathValue("proof_id"), r.Header.Get("Content-Type"), http.MaxBytesReader(w, r.Body, maxLocalUploadBytes))
	if err != nil {
		h.respondErr(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, map[string]proofResponse{"proof": toProofResponse(proof)})
}

func (h *Handler) CompleteUpload(w http.ResponseWriter, r *http.Request) {
	var req completeUploadRequest
	if !h.decode(w, r, &req) {
		return
	}
	proof, err := h.service.CompleteUpload(r.Context(), domain.CompleteUpload{
		TenantID:    tenantID(r),
		ProofID:     r.PathValue("proof_id"),
		ContentHash: req.ContentHash,
		MimeType:    req.MimeType,
		SizeBytes:   req.SizeBytes,
		DurationMS:  req.DurationMS,
		Metadata:    req.Metadata,
	})
	if err != nil {
		h.respondErr(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, map[string]proofResponse{"proof": toProofResponse(proof)})
}

func (h *Handler) Download(w http.ResponseWriter, r *http.Request) {
	if h.verifySignedURL(r, tenantID(r)) {
		proof, reader, err := h.service.OpenLocalDownload(r.Context(), tenantID(r), r.PathValue("proof_id"))
		if err == nil {
			defer reader.Close()
			http.ServeContent(w, r, proof.ProofID, proof.UpdatedAt, reader)
			return
		}
		if !errors.Is(err, ports.ErrUnsupported) {
			h.respondErr(w, r, err)
			return
		}
	}
	url, err := h.service.DownloadURL(r.Context(), tenantID(r), r.PathValue("proof_id"))
	if err != nil {
		h.respondErr(w, r, err)
		return
	}
	if strings.HasPrefix(url, "http") {
		http.Redirect(w, r, url, http.StatusTemporaryRedirect)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, downloadURLResponse{DownloadURL: url})
}

func (h *Handler) DownloadSigned(w http.ResponseWriter, r *http.Request) {
	tenantID := signedTenantID(r)
	if !h.verifySignedURL(r, tenantID) {
		httpresponse.WriteError(w, r, h.log, http.StatusForbidden,
			errorEnvelope{Code: "invalid_download_url", Message: "download URL is invalid or expired", TraceID: traceID(r)}, nil)
		return
	}
	proof, reader, err := h.service.OpenLocalDownload(r.Context(), tenantID, r.PathValue("proof_id"))
	if err != nil {
		h.respondErr(w, r, err)
		return
	}
	defer reader.Close()
	http.ServeContent(w, r, proof.ProofID, proof.UpdatedAt, reader)
}

func (h *Handler) DeleteUpload(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteUpload(r.Context(), tenantID(r), r.PathValue("proof_id"), actorID(r)); err != nil {
		h.respondErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) verifySignedURL(r *http.Request, tenantID string) bool {
	q := r.URL.Query()
	return h.service.VerifySignedURL(r.Method, r.URL.Path, tenantID, q.Get("expires"), q.Get("sig"))
}

func (h *Handler) decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		h.badRequest(w, r, "invalid_body", "request body could not be read")
		return false
	}
	if len(body) == 0 {
		h.badRequest(w, r, "empty_body", "request body is required")
		return false
	}
	if err := json.Unmarshal(body, dst); err != nil {
		h.badRequest(w, r, "invalid_json", "request body is not valid JSON")
		return false
	}
	return true
}

func (h *Handler) respondErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, app.ErrInvalid):
		h.badRequest(w, r, "invalid_proof", "proof request is invalid")
	case errors.Is(err, ports.ErrNotFound):
		httpresponse.WriteError(w, r, h.log, http.StatusNotFound,
			errorEnvelope{Code: "not_found", Message: "proof was not found", TraceID: traceID(r)}, nil)
	// The proof row exists but its bytes are gone/unreadable: a KNOWN terminal condition, not a
	// server fault. 410 Gone + retryable=false tells the client to stop retrying and render
	// "evidence unavailable" (a 500 caused a ~5x retry storm per proof on 2026-08-02).
	case errors.Is(err, ports.ErrObjectMissing), errors.Is(err, fs.ErrNotExist), errors.Is(err, fs.ErrPermission):
		httpresponse.WriteError(w, r, h.log, http.StatusGone,
			errorEnvelope{Code: "proof_object_missing", Message: "proof media is no longer retrievable from storage", TraceID: traceID(r)}, nil)
	case errors.Is(err, ports.ErrUnsupported):
		httpresponse.WriteError(w, r, h.log, http.StatusConflict,
			errorEnvelope{Code: "unsupported_storage_operation", Message: "storage provider does not support this operation", TraceID: traceID(r)}, err)
	case errors.Is(err, ports.ErrInUse):
		httpresponse.WriteError(w, r, h.log, http.StatusConflict,
			errorEnvelope{Code: "proof_in_use", Message: "proof is already attached to a submitted record", TraceID: traceID(r)}, nil)
	case errors.Is(err, ports.ErrIntegrityMismatch):
		httpresponse.WriteError(w, r, h.log, http.StatusConflict,
			errorEnvelope{Code: "proof_integrity_mismatch", Message: "proof upload does not match storage object", TraceID: traceID(r)}, nil)
	case errors.Is(err, ports.ErrIdempotencyConflict):
		httpresponse.WriteError(w, r, h.log, http.StatusConflict,
			errorEnvelope{Code: "idempotency_key_conflict", Message: "idempotency key already belongs to a different proof upload request", TraceID: traceID(r)}, nil)
	default:
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
			errorEnvelope{Code: "internal_error", Message: "internal server error", TraceID: traceID(r), Retryable: true}, err)
	}
}

func (h *Handler) badRequest(w http.ResponseWriter, r *http.Request, code, message string) {
	httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
		errorEnvelope{Code: code, Message: message, TraceID: traceID(r)}, nil)
}

func toProofResponse(p domain.Artifact) proofResponse {
	return proofResponse{
		ProofID:         p.ProofID,
		StorageProvider: p.StorageProvider,
		ProofType:       p.ProofType,
		SubjectType:     p.SubjectType,
		SubjectID:       p.SubjectID,
		UploadState:     p.UploadState,
		MimeType:        p.MimeType,
		SizeBytes:       p.SizeBytes,
		DurationMS:      p.DurationMS,
		ContentHash:     p.ContentHash,
		Metadata:        p.Metadata,
		CreatedAt:       p.CreatedAt,
		UploadedAt:      p.UploadedAt,
	}
}

func tenantID(r *http.Request) string { return httpmiddleware.TenantIDFromContext(r.Context()) }

func signedTenantID(r *http.Request) string { return strings.TrimSpace(r.URL.Query().Get("tenant_id")) }

func actorID(r *http.Request) string { return httpmiddleware.ActorIDFromContext(r.Context()) }

func actorPtr(r *http.Request) *string {
	if a := httpmiddleware.ActorIDFromContext(r.Context()); a != "" {
		return &a
	}
	return nil
}

func traceID(r *http.Request) string {
	if t := httpmiddleware.TraceIDFromContext(r.Context()); t != "" {
		return t
	}
	return "missing-trace"
}
