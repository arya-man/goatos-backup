package proofmedia

import (
	"context"
	"errors"
	"testing"

	proofdomain "github.com/vgoats/goatos/backend/internal/proof/domain"
)

// Mock proof downloader for testing.
type mockDownloader struct {
	responses map[string]string          // proofID -> URL
	errors    map[string]error           // proofID -> error
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

// R50-017: ResolveMedia fails closed on missing objects.
// If any proof ID does not resolve, the entire call fails with an error.
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

	// Attempt to resolve with one missing proof — should fail entirely.
	_, err = resolver.ResolveMedia(ctx, tenantID, []string{"proof-1", "proof-missing"})
	if err == nil {
		t.Fatal("expected error for missing proof, but got none")
	}
	// Should contain "resolve verification proof" and the error message
	if !contains(err.Error(), "resolve verification proof") {
		t.Fatalf("error message should mention proof resolution: %v", err)
	}
}

// R50-017: ResolveMedia fails closed when signing fails.
// If any proof cannot be signed, the entire call fails with an error.
func TestResolvemediaSigningFailure(t *testing.T) {
	ctx := context.Background()
	tenantID := "tenant-001"

	signingErr := errors.New("signing failed: invalid signature")
	mock := &mockDownloader{
		responses: map[string]string{
			"proof-1": "https://example.com/download/proof-1",
		},
		errors: map[string]error{
			"proof-signing-fail": signingErr,
		},
	}

	resolver := NewResolver(mock)

	// Attempt to resolve a proof that fails signing — should fail the entire call.
	_, err := resolver.ResolveMedia(ctx, tenantID, []string{"proof-signing-fail"})
	if err == nil {
		t.Fatal("expected error for signing failure, but got none")
	}

	// Error should wrap the signing error.
	if !contains(err.Error(), "signing failed") {
		t.Fatalf("error should contain signing failure message: %v", err)
	}
}

// R50-017: ResolveMedia fails closed on empty proof ID.
func TestResolveMediaEmptyProofID(t *testing.T) {
	ctx := context.Background()
	tenantID := "tenant-001"

	mock := &mockDownloader{
		artifacts: map[string]proofdomain.Artifact{
			"proof-1": {MimeType: "image/jpeg"},
		},
	}

	resolver := NewResolver(mock)

	// Attempt to resolve with an empty proof ID — should fail.
	_, err := resolver.ResolveMedia(ctx, tenantID, []string{"proof-1", ""})
	if err == nil {
		t.Fatal("expected error for empty proof ID, but got none")
	}

	if !contains(err.Error(), "empty") {
		t.Fatalf("error should mention empty proof ID: %v", err)
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

	// Artifact missing — should fail.
	_, err = resolver.ResolveMedia(ctx, tenantID, []string{"missing-artifact"})
	if err == nil {
		t.Fatal("expected error for missing artifact, but got none")
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
