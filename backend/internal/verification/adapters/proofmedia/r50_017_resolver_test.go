package proofmedia

import (
	"context"
	"errors"
	"testing"

	proofdomain "github.com/vgoats/goatos/backend/internal/proof/domain"
)

// Mock proof downloader for testing.
type mockDownloader struct {
	responses map[string]string // proofID -> URL
	errors    map[string]error  // proofID -> error
	artifacts map[string]proofdomain.Artifact
}

func (m *mockDownloader) DownloadURL(ctx context.Context, tenantID, proofID string) (string, error) {
	if err, ok := m.errors[proofID]; ok {
		return "", err
	}
	if url, ok := m.responses[proofID]; ok {
		return url, nil
	}
	return "", errors.New("proof not found")
}

func (m *mockDownloader) DownloadArtifact(ctx context.Context, tenantID, proofID string) (proofdomain.Artifact, string, error) {
	if err, ok := m.errors[proofID]; ok {
		return proofdomain.Artifact{}, "", err
	}
	if artifact, ok := m.artifacts[proofID]; ok {
		return artifact, "https://example.com/download/" + proofID, nil
	}
	return proofdomain.Artifact{}, "", errors.New("artifact not found")
}

// R50-017: ResolveMedia reports per-ID failures as empty MediaItems.
// Per-ID failures are reported with empty DownloadURL so the service layer
// can fail-close per item rather than blanking the whole page.
func TestResolveMediaMissingObject(t *testing.T) {
	ctx := context.Background()
	tenantID := "tenant-001"

	mock := &mockDownloader{
		artifacts: map[string]proofdomain.Artifact{
			"proof-1": {MimeType: "image/jpeg"},
			"proof-2": {MimeType: "image/jpeg"},
		},
		errors: make(map[string]error),
	}

	resolver := NewResolver(mock)

	// Resolve with valid proofs — should succeed.
	result, err := resolver.ResolveMedia(ctx, tenantID, []string{"proof-1", "proof-2"})
	if err != nil {
		t.Fatalf("valid proofs resolution: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 media items, got %d", len(result))
	}

	// Resolve with one missing proof — should return per-ID failures as empty MediaItems.
	result, err = resolver.ResolveMedia(ctx, tenantID, []string{"proof-1", "proof-missing"})
	if err != nil {
		t.Fatal("expected no error, per-ID failures are reported as empty MediaItems")
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 media items (one empty), got %d", len(result))
	}
	// proof-1 should have resolved successfully
	if result[0].ProofID != "proof-1" || result[0].DownloadURL == "" {
		t.Fatalf("proof-1 should resolve: %v", result[0])
	}
	// proof-missing should be reported as empty (DownloadURL="")
	if result[1].ProofID != "proof-missing" || result[1].DownloadURL != "" {
		t.Fatalf("proof-missing should be empty: %v", result[1])
	}
}

// R50-017: ResolveMedia reports per-ID signing failures as empty MediaItems.
// When a proof cannot be signed, it's reported as an empty MediaItem.
func TestResolvemediaSigningFailure(t *testing.T) {
	ctx := context.Background()
	tenantID := "tenant-001"

	signingErr := errors.New("signing failed: invalid signature")
	mock := &mockDownloader{
		artifacts: map[string]proofdomain.Artifact{
			"proof-1": {MimeType: "image/jpeg"},
		},
		errors: map[string]error{
			"proof-signing-fail": signingErr,
		},
	}

	resolver := NewResolver(mock)

	// Resolve with mixed success/failure — signing failure is per-ID.
	result, err := resolver.ResolveMedia(ctx, tenantID, []string{"proof-1", "proof-signing-fail"})
	if err != nil {
		t.Fatal("expected no error, per-ID failures are reported as empty MediaItems")
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 media items, got %d", len(result))
	}
	// proof-1 should have signed successfully
	if result[0].DownloadURL == "" {
		t.Fatal("proof-1 should have signed successfully")
	}
	// proof-signing-fail should be reported as empty
	if result[1].DownloadURL != "" {
		t.Fatal("proof-signing-fail should be empty (DownloadURL='')")
	}
}

// R50-017: ResolveMedia reports empty proof IDs as empty MediaItems.
// Empty proof IDs are skipped and reported as empty MediaItems.
func TestResolveMediaEmptyProofID(t *testing.T) {
	ctx := context.Background()
	tenantID := "tenant-001"

	mock := &mockDownloader{
		artifacts: map[string]proofdomain.Artifact{
			"proof-1": {MimeType: "image/jpeg"},
		},
	}

	resolver := NewResolver(mock)

	// Resolve with one valid and one empty proof ID.
	result, err := resolver.ResolveMedia(ctx, tenantID, []string{"proof-1", ""})
	if err != nil {
		t.Fatal("expected no error, empty IDs are reported as empty MediaItems")
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 media items, got %d", len(result))
	}
}

// R50-017: ResolveMedia with artifact downloader (rich interface).
func TestResolveMediaWithArtifactDownloader(t *testing.T) {
	ctx := context.Background()
	tenantID := "tenant-001"

	duration5000 := int64(5000)
	duration0 := int64(0)
	mock := &mockDownloader{
		artifacts: map[string]proofdomain.Artifact{
			"video-proof": {
				MimeType:   "video/mp4",
				DurationMS: &duration5000,
			},
			"photo-proof": {
				MimeType:   "image/jpeg",
				DurationMS: &duration0,
			},
		},
		errors: make(map[string]error),
	}

	resolver := NewResolver(mock)

	// Resolve video proof with artifact downloader.
	result, err := resolver.ResolveMedia(ctx, tenantID, []string{"video-proof"})
	if err != nil {
		t.Fatalf("artifact resolution: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 media item, got %d", len(result))
	}
	if result[0].MimeType != "video/mp4" {
		t.Fatalf("expected video/mp4, got %s", result[0].MimeType)
	}
	if result[0].DurationMS == nil || *result[0].DurationMS != 5000 {
		t.Fatalf("expected duration 5000, got %v", result[0].DurationMS)
	}

	// Artifact missing — reported as empty MediaItem.
	result, err = resolver.ResolveMedia(ctx, tenantID, []string{"missing-artifact"})
	if err != nil {
		t.Fatalf("expected no error, missing artifacts are reported as empty: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 media item, got %d", len(result))
	}
	if result[0].DownloadURL != "" {
		t.Fatal("missing artifact should be empty")
	}
}

// R50-017: ResolveMedia with nil resolver or proof port.
func TestResolveMediaNilResolver(t *testing.T) {
	ctx := context.Background()
	tenantID := "tenant-001"

	var resolver *Resolver
	_, err := resolver.ResolveMedia(ctx, tenantID, []string{"proof-1"})
	if err == nil {
		t.Fatal("expected error for nil resolver, but got none")
	}

	if !contains(err.Error(), "unavailable") {
		t.Fatalf("error should mention unavailable: %v", err)
	}
}

// Helper to check if error message contains a substring.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && (s[0:len(substr)] == substr || s[len(s)-len(substr):] == substr || findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Verify that the returned MediaItem structure is correct.
func TestResolveMediaStructure(t *testing.T) {
	ctx := context.Background()
	tenantID := "tenant-001"

	proofID := "proof-test-structure"
	downloadURL := "https://example.com/download/proof-test-structure"

	mock := &mockDownloader{
		artifacts: map[string]proofdomain.Artifact{
			proofID: {MimeType: "image/jpeg"},
		},
	}

	resolver := NewResolver(mock)

	result, err := resolver.ResolveMedia(ctx, tenantID, []string{proofID})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result))
	}

	item := result[0]
	if item.ProofID != proofID {
		t.Fatalf("ProofID = %s, want %s", item.ProofID, proofID)
	}
	if item.DownloadURL != downloadURL {
		t.Fatalf("DownloadURL = %s, want %s", item.DownloadURL, downloadURL)
	}

	// MimeType should be populated when using artifact downloader
	if item.MimeType != "image/jpeg" {
		t.Fatalf("MimeType = %s, want image/jpeg", item.MimeType)
	}

	// DurationMS should be nil when not set
	if item.DurationMS != nil {
		t.Fatalf("DurationMS should be nil, got %v", item.DurationMS)
	}
}
