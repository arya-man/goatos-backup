package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/permissions"
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

func TestListUploadedProofsPassesFeedSlotQuery(t *testing.T) {
	const parkA = "86000000-0000-4000-8000-000000000701"
	svc := &fakeHTTPProofService{
		proofs: []domain.Artifact{{
			ProofID:     httpTestProof,
			TenantID:    httpTestTenant,
			ProofType:   "video",
			SubjectType: "shed",
			UploadState: "completed",
			MimeType:    "video/mp4",
			Metadata:    map[string]any{"client_task_key": "feed-dist:shed:whole:1:morning:2026-08-13", "field_key": "feed_distribution_video"},
			CreatedAt:   time.Date(2026, 8, 13, 10, 0, 0, 0, time.UTC),
		}},
	}
	mux := http.NewServeMux()
	Register(mux, NewHandler(svc))

	req := httptest.NewRequest(http.MethodGet, "/app/proofs/uploads?scope_type=shed&scope_id=30000000-0000-4000-8000-000000000001&client_task_key=feed-dist:shed:whole:1:morning:2026-08-13&field_key=feed_distribution_video", nil)
	ctx := httpmiddleware.WithTenantID(req.Context(), httpTestTenant)
	ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{{
		Role:      permissions.RoleOperator,
		ScopeType: "park",
		ScopeID:   parkA,
	}})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if svc.listQuery.ClientTaskKey == "" || svc.listQuery.FieldKey != "feed_distribution_video" {
		t.Fatalf("list query = %#v", svc.listQuery)
	}
	if svc.listQuery.AllAuthorizedParks {
		t.Fatalf("park-scoped operator was treated as tenant-wide: %#v", svc.listQuery)
	}
	if len(svc.listQuery.AuthorizedParkIDs) != 1 || svc.listQuery.AuthorizedParkIDs[0] != parkA {
		t.Fatalf("authorized parks = %v, want [%s]", svc.listQuery.AuthorizedParkIDs, parkA)
	}
	var got listUploadedProofsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got.Proofs) != 1 || got.Proofs[0].ProofID != httpTestProof {
		t.Fatalf("proofs = %#v", got.Proofs)
	}
}

// A stored object that has gone missing/unreadable is a KNOWN terminal failure class, not an
// unexpected server fault: the client must be told to stop retrying and render "evidence
// unavailable" instead of hammering the route (the 2026-08-02 incident produced ~5 retries per
// proof because the route answered 500 internal_error).
func TestSignedDownloadMissingObjectIsTerminalGone(t *testing.T) {
	svc := &fakeHTTPProofService{verify: true, openErr: fmt.Errorf("open /media/x: %w", fs.ErrNotExist)}
	mux := http.NewServeMux()
	RegisterSigned(mux, NewHandler(svc))

	req := httptest.NewRequest(http.MethodGet, "/app/proofs/"+httpTestProof+"/download/signed?tenant_id="+httpTestTenant+"&expires=9999999999&sig=ok", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusGone {
		t.Fatalf("status=%d body=%s, want 410", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if got["code"] != "proof_object_missing" {
		t.Fatalf("code=%v, want proof_object_missing", got["code"])
	}
	if got["retryable"] != false {
		t.Fatalf("retryable=%v, want false for a missing stored object", got["retryable"])
	}
	msg, _ := got["message"].(string)
	if !strings.Contains(msg, "no longer") && !strings.Contains(msg, "not retrievable") {
		t.Fatalf("message=%q, want an actionable evidence-unavailable message", msg)
	}
}

// The unsigned sibling route shares respondErr, so it must classify identically; an unreadable
// (permission-denied) object is the same terminal class as a missing one.
func TestUnsignedDownloadMissingObjectIsTerminalGone(t *testing.T) {
	svc := &fakeHTTPProofService{verify: true, openErr: fmt.Errorf("open /media/x: %w", fs.ErrPermission)}
	mux := http.NewServeMux()
	Register(mux, NewHandler(svc))

	req := httptest.NewRequest(http.MethodGet, "/app/proofs/"+httpTestProof+"/download?tenant_id="+httpTestTenant+"&expires=9999999999&sig=ok", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), httpTestTenant))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusGone {
		t.Fatalf("status=%d body=%s, want 410", rec.Code, rec.Body.String())
	}
}

func TestSignedDownloadPresentObjectStillStreams200(t *testing.T) {
	svc := &fakeHTTPProofService{
		verify: true,
		proof:  domain.Artifact{ProofID: httpTestProof, TenantID: httpTestTenant, MimeType: "video/mp4"},
		reader: newReadSeekCloser("proof-video-bytes"),
	}
	mux := http.NewServeMux()
	RegisterSigned(mux, NewHandler(svc))

	req := httptest.NewRequest(http.MethodGet, "/app/proofs/"+httpTestProof+"/download/signed?tenant_id="+httpTestTenant+"&expires=9999999999&sig=ok", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || rec.Body.String() != "proof-video-bytes" {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
}

type fakeHTTPProofService struct {
	verify      bool
	proof       domain.Artifact
	target      domain.UploadTarget
	reader      ports.ReadSeekCloser
	proofs      []domain.Artifact
	listQuery   domain.ListUploadedProofsQuery
	openErr     error
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

func (s *fakeHTTPProofService) ListUploadedProofs(_ context.Context, query domain.ListUploadedProofsQuery) ([]domain.Artifact, error) {
	s.listQuery = query
	return s.proofs, nil
}

func (s *fakeHTTPProofService) DownloadURL(context.Context, string, string) (string, error) {
	return "", nil
}

func (s *fakeHTTPProofService) DeleteUpload(context.Context, string, string, string) error {
	return nil
}

func (s *fakeHTTPProofService) OpenLocalDownload(_ context.Context, tenantID, _ string) (domain.Artifact, ports.ReadSeekCloser, error) {
	s.openTenant = tenantID
	if s.openErr != nil {
		return domain.Artifact{}, nil, s.openErr
	}
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
