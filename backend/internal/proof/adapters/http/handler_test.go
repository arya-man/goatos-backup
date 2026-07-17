package http

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

type fakeHTTPProofService struct {
	verify      bool
	proof       domain.Artifact
	reader      ports.ReadSeekCloser
	openTenant  string
	storeTenant string
}

func (s *fakeHTTPProofService) CreateUpload(context.Context, domain.CreateUpload) (domain.UploadTarget, error) {
	return domain.UploadTarget{}, nil
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
