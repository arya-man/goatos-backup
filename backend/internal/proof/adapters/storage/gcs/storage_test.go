package gcs

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/url"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/proof/domain"
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
