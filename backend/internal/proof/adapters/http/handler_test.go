package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
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

func TestCreateUploadLogsBoundedProofCorrelationMetadata(t *testing.T) {
	var logs bytes.Buffer
	svc := &fakeHTTPProofService{target: domain.UploadTarget{Proof: domain.Artifact{
		ProofID: httpTestProof, ProofType: "video", MimeType: "video/mp4", UploadState: "pending",
	}}}
	mux := http.NewServeMux()
	Register(mux, NewHandler(svc, slog.New(slog.NewJSONHandler(&logs, nil))))
	longException := strings.Repeat("x", proofLogValueLimit+20)
	body := fmt.Sprintf(`{"proof_type":"video","mime_type":"video/mp4","scope_type":"task","scope_id":"scope-1","subject_type":"goat","metadata":{"local_proof_id":"local-1","outbox_item_id":"outbox-1","client_task_key":"task-1","field_key":"vaccination_goat_proof","obligation_id":"obligation-1","attempt":2,"proof_stage":"register","exception_class":%q,"local_file_available":true,"upload_original":false,"client_trace_id":"mobile-trace-1","ignored_secret":"must-not-log"}}`, longException)
	req := httptest.NewRequest(http.MethodPost, "/app/proofs/uploads", strings.NewReader(body))
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), httpTestTenant))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	line := logs.String()
	for _, want := range []string{
		`"msg":"proof_upload_created"`, `"local_proof_id":"local-1"`,
		`"outbox_item_id":"outbox-1"`, `"client_task_key":"task-1"`,
		`"field_key":"vaccination_goat_proof"`, `"obligation_id":"obligation-1"`,
		`"attempt":"2"`, `"proof_stage":"register"`, `"local_file_available":"true"`,
		`"upload_original":"false"`, `"client_trace_id":"mobile-trace-1"`,
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("log missing %s in %s", want, line)
		}
	}
	if strings.Contains(line, "ignored_secret") || strings.Contains(line, "must-not-log") {
		t.Fatalf("log leaked arbitrary metadata: %s", line)
	}
	if strings.Contains(line, longException) {
		t.Fatalf("log did not bound exception metadata: %s", line)
	}
}

func TestCompleteUploadLogsObligationCycleAndServerProofCorrelation(t *testing.T) {
	var logs bytes.Buffer
	svc := &fakeHTTPProofService{proof: domain.Artifact{
		ProofID: httpTestProof, MimeType: "video/mp4", SizeBytes: 42, UploadState: "completed",
		Metadata: map[string]any{
			"server_proof_id": httpTestProof,
			"upload_original": true,
			"obligation_cycles": []any{map[string]any{
				"obligation_id": "obligation-1", "obligation_row_version": float64(7),
			}},
		},
	}}
	mux := http.NewServeMux()
	Register(mux, NewHandler(svc, slog.New(slog.NewJSONHandler(&logs, nil))))
	req := httptest.NewRequest(http.MethodPost, "/app/proofs/"+httpTestProof+"/complete", strings.NewReader(`{"content_hash":"sha256:x","mime_type":"video/mp4","size_bytes":42,"metadata":{}}`))
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), httpTestTenant))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	line := logs.String()
	for _, want := range []string{
		`"msg":"proof_upload_completed"`, `"server_proof_id":"` + httpTestProof + `"`,
		`"upload_original":"true"`, `"obligation_cycle_count":1`,
		`"obligation_id":"obligation-1"`, `"obligation_row_version":"7"`,
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("log missing %s in %s", want, line)
		}
	}
}

func TestDownloadRedirectLogsAttributionAndPrivateCache(t *testing.T) {
	uploadedBy := "90000000-0000-4000-8000-000000000001"
	subjectID := "30000000-0000-4000-8000-000000000001"
	var logs bytes.Buffer
	svc := &fakeHTTPProofService{
		proof: domain.Artifact{
			ProofID:         httpTestProof,
			TenantID:        httpTestTenant,
			StorageProvider: "gcs",
			ObjectKey:       "goatos-stg-media/tenant/proofs/proof-1.mp4",
			ContentHash:     "sha256:proof",
			MimeType:        "video/mp4",
			SizeBytes:       42,
			ScopeType:       "pc_care_task",
			ScopeID:         "20000000-0000-4000-8000-000000000001",
			SubjectType:     "goat",
			SubjectID:       &subjectID,
			ProofType:       "pc_hoof_trimming",
			UploadedBy:      &uploadedBy,
			UpdatedAt:       time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC),
		},
		downloadURL: "https://storage.googleapis.com/goatos-stg-media/signed-proof",
	}
	mux := http.NewServeMux()
	Register(mux, NewHandler(svc, slog.New(slog.NewJSONHandler(&logs, nil))))

	req := httptest.NewRequest(http.MethodGet, "/app/proofs/"+httpTestProof+"/download", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), httpTestTenant))
	req.Header.Set("Accept", "text/html")
	req.Header.Set("User-Agent", "okhttp/5.1.0")
	req.Header.Set("X-Forwarded-For", "157.51.58.239, 10.0.0.1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != svc.downloadURL {
		t.Fatalf("Location=%q, want signed URL", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "private, max-age=300" {
		t.Fatalf("Cache-Control=%q", got)
	}
	if got := rec.Header().Get("Vary"); got != "Authorization, Accept" {
		t.Fatalf("Vary=%q", got)
	}
	logLine := logs.String()
	for _, want := range []string{
		`"event":"proof_download_redirect"`,
		`"proof_id":"` + httpTestProof + `"`,
		`"remote_ip":"157.51.58.239"`,
		`"object_key":"goatos-stg-media/tenant/proofs/proof-1.mp4"`,
		`"proof_type":"pc_hoof_trimming"`,
	} {
		if !strings.Contains(logLine, want) {
			t.Fatalf("log missing %s in %s", want, logLine)
		}
	}
}

func TestDownloadJSONClientGetsURLWithoutRedirectingToGCS(t *testing.T) {
	uploadedBy := "90000000-0000-4000-8000-000000000001"
	var logs bytes.Buffer
	svc := &fakeHTTPProofService{
		proof: domain.Artifact{
			ProofID:         httpTestProof,
			TenantID:        httpTestTenant,
			StorageProvider: "gcs",
			ObjectKey:       "goatos-stg-media/tenant/proofs/proof-1.mp4",
			ContentHash:     "sha256:proof",
			MimeType:        "video/mp4",
			SizeBytes:       42,
			ScopeType:       "pc_care_task",
			ScopeID:         "20000000-0000-4000-8000-000000000001",
			SubjectType:     "goat",
			ProofType:       "pc_hoof_trimming",
			UploadedBy:      &uploadedBy,
			UpdatedAt:       time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC),
		},
		downloadURL: "https://storage.googleapis.com/goatos-stg-media/signed-proof",
	}
	mux := http.NewServeMux()
	Register(mux, NewHandler(svc, slog.New(slog.NewJSONHandler(&logs, nil))))

	req := httptest.NewRequest(http.MethodGet, "/app/proofs/"+httpTestProof+"/download", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), httpTestTenant))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "okhttp/5.1.0")
	req.Header.Set("X-Forwarded-For", "157.51.58.239, 10.0.0.1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "" {
		t.Fatalf("Location=%q, want no redirect", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "private, max-age=300" {
		t.Fatalf("Cache-Control=%q", got)
	}
	if got := rec.Header().Get("Vary"); got != "Authorization, Accept" {
		t.Fatalf("Vary=%q", got)
	}
	var body downloadURLResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode JSON response: %v", err)
	}
	if body.DownloadURL != svc.downloadURL {
		t.Fatalf("download_url=%q, want signed URL", body.DownloadURL)
	}
	logLine := logs.String()
	if strings.Contains(logLine, `"event":"proof_download_redirect"`) {
		t.Fatalf("unexpected redirect event in %s", logLine)
	}
	for _, want := range []string{
		`"event":"proof_download_url_issued"`,
		`"proof_id":"` + httpTestProof + `"`,
		`"remote_ip":"157.51.58.239"`,
		`"object_key":"goatos-stg-media/tenant/proofs/proof-1.mp4"`,
		`"proof_type":"pc_hoof_trimming"`,
	} {
		if !strings.Contains(logLine, want) {
			t.Fatalf("log missing %s in %s", want, logLine)
		}
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
	if got.Proofs[0].DownloadURL != "" {
		t.Fatalf("download_url = %q, want omitted by default on list reads", got.Proofs[0].DownloadURL)
	}
	if svc.downloadURLCalls != 0 {
		t.Fatalf("DownloadURL calls = %d, want 0 for default list read", svc.downloadURLCalls)
	}
}

func TestListUploadedProofsOptInReturnsBackendDownloadPathWithoutSigning(t *testing.T) {
	svc := &fakeHTTPProofService{
		downloadURL: "https://storage.example/proof",
		proofs: []domain.Artifact{{
			ProofID:     httpTestProof,
			TenantID:    httpTestTenant,
			ProofType:   "video",
			SubjectType: "shed",
			UploadState: "completed",
			MimeType:    "video/mp4",
			CreatedAt:   time.Date(2026, 8, 13, 10, 0, 0, 0, time.UTC),
		}},
	}
	mux := http.NewServeMux()
	Register(mux, NewHandler(svc))

	req := httptest.NewRequest(http.MethodGet, "/app/proofs/uploads?include_download_urls=true", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), httpTestTenant))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got listUploadedProofsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	want := "/app/proofs/" + httpTestProof + "/download"
	if len(got.Proofs) != 1 || got.Proofs[0].DownloadURL != want {
		t.Fatalf("proofs = %#v, want backend download_url %q", got.Proofs, want)
	}
	if svc.downloadURLCalls != 0 {
		t.Fatalf("DownloadURL calls = %d, want 0 for list opt-in path", svc.downloadURLCalls)
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
	verify           bool
	proof            domain.Artifact
	downloadURL      string
	downloadURLCalls int
	target           domain.UploadTarget
	reader           ports.ReadSeekCloser
	proofs           []domain.Artifact
	listQuery        domain.ListUploadedProofsQuery
	openErr          error
	openTenant       string
	storeTenant      string
}

func (s *fakeHTTPProofService) CreateUpload(context.Context, domain.CreateUpload) (domain.UploadTarget, error) {
	return s.target, nil
}

func (s *fakeHTTPProofService) CompleteUpload(context.Context, domain.CompleteUpload) (domain.Artifact, error) {
	return s.proof, nil
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
	s.downloadURLCalls++
	return s.downloadURL, nil
}

func (s *fakeHTTPProofService) DownloadArtifact(context.Context, string, string) (domain.Artifact, string, error) {
	return s.proof, s.downloadURL, nil
}

func (s *fakeHTTPProofService) DownloadArtifactForActor(context.Context, string, string, string) (domain.Artifact, string, error) {
	return s.proof, s.downloadURL, nil
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
