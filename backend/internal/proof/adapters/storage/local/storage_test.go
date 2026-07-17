package local

import (
	"context"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/proof/domain"
)

const (
	localTestTenant = "00000000-0000-4000-8000-000000000001"
	localTestProof  = "10000000-0000-4000-8000-000000000001"
)

func TestSignedLocalURLsCarryTenantAndUseSignedMediaRoutes(t *testing.T) {
	storage := New(t.TempDir(), "local-proof-secret")
	proof := domain.Artifact{TenantID: localTestTenant, ProofID: localTestProof, MimeType: "video/mp4"}

	target, err := storage.PrepareUpload(context.Background(), proof, 15*time.Minute)
	if err != nil {
		t.Fatalf("PrepareUpload() error = %v", err)
	}
	uploadURL, err := url.Parse(target.UploadURL)
	if err != nil {
		t.Fatalf("parse upload URL: %v", err)
	}
	if uploadURL.Path != "/app/proofs/"+localTestProof+"/upload" {
		t.Fatalf("upload path = %q, want signed media route", uploadURL.Path)
	}
	if uploadURL.Query().Get("tenant_id") != localTestTenant {
		t.Fatalf("upload tenant_id query = %q", uploadURL.Query().Get("tenant_id"))
	}
	if !storage.Verify("PUT", uploadURL.Path, localTestTenant, uploadURL.Query().Get("expires"), uploadURL.Query().Get("sig"), time.Now().UTC()) {
		t.Fatal("signed upload URL did not verify")
	}
	if storage.Verify("PUT", uploadURL.Path, "00000000-0000-4000-8000-000000000099", uploadURL.Query().Get("expires"), uploadURL.Query().Get("sig"), time.Now().UTC()) {
		t.Fatal("signed upload URL verified for a different tenant")
	}

	download, err := storage.PrepareDownload(context.Background(), proof, 15*time.Minute)
	if err != nil {
		t.Fatalf("PrepareDownload() error = %v", err)
	}
	downloadURL, err := url.Parse(download)
	if err != nil {
		t.Fatalf("parse download URL: %v", err)
	}
	if downloadURL.Path != "/app/proofs/"+localTestProof+"/download/signed" {
		t.Fatalf("download path = %q, want signed media route", downloadURL.Path)
	}
	if !storage.Verify("GET", downloadURL.Path, localTestTenant, downloadURL.Query().Get("expires"), downloadURL.Query().Get("sig"), time.Now().UTC()) {
		t.Fatal("signed download URL did not verify")
	}
}

func TestLocalPathStaysInsideBaseDir(t *testing.T) {
	baseDir := t.TempDir()
	storage := New(baseDir, "local-proof-secret")

	path, err := storage.localPath("/tenant/proofs/video.mp4")
	if err != nil {
		t.Fatalf("localPath() error = %v", err)
	}
	rel, err := filepath.Rel(baseDir, path)
	if err != nil {
		t.Fatalf("relative path: %v", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		t.Fatalf("localPath escaped base dir: %q", path)
	}

	if _, err := storage.localPath("../escape.mp4"); err == nil {
		t.Fatal("localPath accepted an escaping object key")
	}
	if _, err := storage.Store(context.Background(), domain.Artifact{ObjectKey: "../../escape.mp4"}, strings.NewReader("proof"), "video/mp4"); err == nil {
		t.Fatal("Store accepted an escaping object key")
	}
}
