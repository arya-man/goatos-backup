package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/proof/domain"
	"github.com/vgoats/goatos/backend/internal/proof/ports"
)

const (
	httpTestTenant = "00000000-0000-4000-8000-000000000001"
	httpTestProof  = "10000000-0000-4000-8000-000000000001"
)

func TestSignedDownloadRouteStreamsWithoutBearerAuth(t *testing.T) {
	svc := &fakeHTTPProofService{
		verify: true,
		proof: domain.Artifact{
			ProofID:     httpTestProof,
			TenantID:    httpTestTenant,
			MimeType:    "video/mp4",
			UploadState: "completed",
			UpdatedAt:   time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC),
		},
		reader: newReadSeekCloser("proof-video-bytes"),
	}
	mux := http.NewServeMux()
	RegisterSigned(mux, NewHandler(svc))

	req := httptest.NewRequest(http.MethodGet, "/app/proofs/"+httpTestProof+"/download/signed?tenant_id="+httpTestTenant+"&expires=9999999999&sig=ok", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != "proof-video-bytes" {
		t.Fatalf("body=%q", got)
	}
	if svc.openTenant != httpTestTenant {
		t.Fatalf("OpenLocalDownload tenant=%q, want signed tenant", svc.openTenant)
	}
}

func TestSignedUploadRouteStoresWithoutBearerAuth(t *testing.T) {
	svc := &fakeHTTPProofService{verify: true}
	mux := http.NewServeMux()
	RegisterSigned(mux, NewHandler(svc))

	req := httptest.NewRequest(http.MethodPut, "/app/proofs/"+httpTestProof+"/upload?tenant_id="+httpTestTenant+"&expires=9999999999&sig=ok", strings.NewReader("proof-video-bytes"))
	req.Header.Set("Content-Type", "video/mp4")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if svc.storeTenant != httpTestTenant {
		t.Fatalf("StoreUpload tenant=%q, want signed tenant", svc.storeTenant)
	}
}

func TestCreateUploadResponseAdvertisesResumableProtocol(t *testing.T) {
	svc := &fakeHTTPProofService{
		target: domain.UploadTarget{
			Proof: domain.Artifact{
				ProofID:         httpTestProof,
				TenantID:        httpTestTenant,
				StorageProvider: "gcs",
				ProofType:       "video",
				SubjectType:     "shed",
				UploadState:     "pending",
				MimeType:        "video/mp4",
				Metadata:        map[string]any{},
				CreatedAt:       time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC),
			},
			UploadURL:      "https://storage.googleapis.com/goatos-proof-test/tenant/proofs/proof-1",
			Method:         "POST",
			Headers:        map[string]string{"x-goog-resumable": "start", "Content-Length": "0"},
			ExpiresAt:      time.Date(2026, 7, 24, 10, 15, 0, 0, time.UTC),
			UploadProtocol: "gcs_resumable_v1",
			ChunkSizeBytes: 8 * 1024 * 1024,
		},
	}
	mux := http.NewServeMux()
	Register(mux, NewHandler(svc))

	body := `{"proof_type":"video","mime_type":"video/mp4","scope_type":"shed","scope_id":"30000000-0000-4000-8000-000000000001","subject_type":"shed","metadata":{"capture_source":"in_app_camera","captured_start_ms":1,"captured_end_ms":2}}`
	req := httptest.NewRequest(http.MethodPost, "/app/proofs/uploads", strings.NewReader(body))
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), httpTestTenant))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got createUploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.UploadMethod != "POST" || got.UploadProtocol != "gcs_resumable_v1" {
		t.Fatalf("upload target method/protocol = %q/%q", got.UploadMethod, got.UploadProtocol)
	}
	if got.ChunkSizeBytes != 8*1024*1024 {
		t.Fatalf("chunk_size_bytes = %d, want 8MiB", got.ChunkSizeBytes)
	}
	if got.Headers["x-goog-resumable"] != "start" || got.Headers["Content-Length"] != "0" {
		t.Fatalf("headers = %#v, want resumable initiation headers", got.Headers)
	}
}

type fakeHTTPProofService struct {
	verify      bool
	proof       domain.Artifact
	target      domain.UploadTarget
	reader      ports.ReadSeekCloser
	openTenant  string
	storeTenant string
}

func (s *fakeHTTPProofService) CreateUpload(context.Context, domain.CreateUpload) (domain.UploadTarget, error) {
	return s.target, nil
}

func (s *fakeHTTPProofService) CompleteUpload(context.Context, domain.CompleteUpload) (domain.Artifact, error) {
	return domain.Artifact{}, nil
}

func (s *fakeHTTPProofService) StoreUpload(_ context.Context, tenantID, _ string, _ string, _ io.Reader) (domain.Artifact, error) {
	s.storeTenant = tenantID
	return domain.Artifact{
		ProofID:     httpTestProof,
		TenantID:    tenantID,
		MimeType:    "video/mp4",
		UploadState: "completed",
		UpdatedAt:   time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC),
	}, nil
}

func (s *fakeHTTPProofService) DownloadURL(context.Context, string, string) (string, error) {
	return "", nil
}

func (s *fakeHTTPProofService) DeleteUpload(context.Context, string, string) error {
	return nil
}

func (s *fakeHTTPProofService) OpenLocalDownload(_ context.Context, tenantID, _ string) (domain.Artifact, ports.ReadSeekCloser, error) {
	s.openTenant = tenantID
	return s.proof, s.reader, nil
}

func (s *fakeHTTPProofService) VerifySignedURL(_, _, tenantID, _, _ string) bool {
	return s.verify && tenantID == httpTestTenant
}

type readSeekCloser struct {
	*strings.Reader
}

func newReadSeekCloser(value string) *readSeekCloser {
	return &readSeekCloser{Reader: strings.NewReader(value)}
}

func (r *readSeekCloser) Close() error { return nil }
