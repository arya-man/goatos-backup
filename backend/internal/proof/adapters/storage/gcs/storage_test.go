package gcs

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/proof/domain"
	"github.com/vgoats/goatos/backend/internal/proof/ports"
)

func TestSignedUploadUsesGenerationMatchPrecondition(t *testing.T) {
	storage := newTestStorage(t)
	storage.now = func() time.Time { return time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC) }

	target, err := storage.PrepareUpload(context.Background(), domain.Artifact{
		ProofID:   "10000000-0000-4000-8000-000000000001",
		ObjectKey: "tenant/proofs/proof-1",
	}, 15*time.Minute)
	if err != nil {
		t.Fatalf("PrepareUpload() error = %v", err)
	}
	if got := target.Headers["x-goog-if-generation-match"]; got != "0" {
		t.Fatalf("generation precondition header = %q, want 0", got)
	}
	u, err := url.Parse(target.UploadURL)
	if err != nil {
		t.Fatalf("parse signed URL: %v", err)
	}
	if got := u.Query().Get("X-Goog-SignedHeaders"); got != "host;x-goog-if-generation-match" {
		t.Fatalf("signed headers = %q", got)
	}
}

func TestVideoUploadUsesResumableSessionInitiation(t *testing.T) {
	storage := newTestStorage(t)
	storage.now = func() time.Time { return time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC) }

	target, err := storage.PrepareUpload(context.Background(), domain.Artifact{
		ProofID:   "10000000-0000-4000-8000-000000000001",
		ObjectKey: "tenant/proofs/proof-1",
		ProofType: "video",
		MimeType:  "video/mp4",
	}, 15*time.Minute)
	if err != nil {
		t.Fatalf("PrepareUpload() error = %v", err)
	}
	if target.Method != "POST" {
		t.Fatalf("method = %q, want POST", target.Method)
	}
	if target.UploadProtocol != "gcs_resumable_v1" {
		t.Fatalf("upload protocol = %q, want gcs_resumable_v1", target.UploadProtocol)
	}
	if target.ChunkSizeBytes != 8*1024*1024 {
		t.Fatalf("chunk size = %d, want 8MiB", target.ChunkSizeBytes)
	}
	if got := target.Headers["x-goog-resumable"]; got != "start" {
		t.Fatalf("x-goog-resumable = %q, want start", got)
	}
	if got := target.Headers["x-goog-if-generation-match"]; got != "0" {
		t.Fatalf("generation precondition header = %q, want 0", got)
	}
	if got := target.Headers["Content-Type"]; got != "video/mp4" {
		t.Fatalf("content-type header = %q, want video/mp4", got)
	}
	if got := target.Headers["Content-Length"]; got != "0" {
		t.Fatalf("content-length header = %q, want 0", got)
	}
	u, err := url.Parse(target.UploadURL)
	if err != nil {
		t.Fatalf("parse signed URL: %v", err)
	}
	if got := u.Query().Get("X-Goog-SignedHeaders"); got != "content-length;content-type;host;x-goog-if-generation-match;x-goog-resumable" {
		t.Fatalf("signed headers = %q", got)
	}
}

func TestSignedDownloadPinsGeneration(t *testing.T) {
	storage := newTestStorage(t)
	storage.now = func() time.Time { return time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC) }

	signed, err := storage.PrepareDownload(context.Background(), domain.Artifact{
		ObjectKey:   "tenant/proofs/proof-1",
		ContentHash: "gcs-generation:1719230400000000",
	}, 15*time.Minute)
	if err != nil {
		t.Fatalf("PrepareDownload() error = %v", err)
	}
	u, err := url.Parse(signed)
	if err != nil {
		t.Fatalf("parse signed URL: %v", err)
	}
	if got := u.Query().Get("generation"); got != "1719230400000000" {
		t.Fatalf("generation query = %q", got)
	}
}

func newTestStorage(t *testing.T) *Storage {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	storage, err := New("goatos-proof-test", "proof-signer@example.iam.gserviceaccount.com", string(pemBytes))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return storage
}

// stubRoundTripper answers the signed HEAD without touching the network.
type stubRoundTripper struct {
	status int
	method string
}

func (s *stubRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	s.method = req.Method
	return &http.Response{
		StatusCode: s.status,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

// StatObject must issue ONE signed HEAD (never a GET of the bytes) and map a 404/410 to the
// terminal ErrObjectMissing class used by the verdict-time evidence gate.
func TestStatObjectMapsMissingObjectToTerminalClass(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		wantErr error
	}{
		{"present", http.StatusOK, nil},
		{"not found", http.StatusNotFound, ports.ErrObjectMissing},
		{"gone", http.StatusGone, ports.ErrObjectMissing},
	} {
		t.Run(tc.name, func(t *testing.T) {
			storage := newTestStorage(t)
			rt := &stubRoundTripper{status: tc.status}
			storage.client = &http.Client{Transport: rt}

			err := storage.StatObject(context.Background(), domain.Artifact{ObjectKey: "tenant/2026/08/02/proof.mp4"})
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("StatObject = %v, want nil", err)
				}
			} else if !errors.Is(err, tc.wantErr) {
				t.Fatalf("StatObject = %v, want %v", err, tc.wantErr)
			}
			if rt.method != http.MethodHead {
				t.Fatalf("method = %q, want HEAD (never download the bytes to check existence)", rt.method)
			}
		})
	}
}

var _ ports.ObjectStatter = (*Storage)(nil)
