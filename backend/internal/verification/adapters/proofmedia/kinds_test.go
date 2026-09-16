package proofmedia

import (
	"context"
	"fmt"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	proofdomain "github.com/vgoats/goatos/backend/internal/proof/domain"
)

// kindsProofStore answers ONE batched metadata read and records every other call, so a kinds read
// that signed a URL, statted bytes or looped per proof is visible.
type kindsProofStore struct {
	artifacts  map[string]proofdomain.Artifact
	batchCalls [][]string
	signCalls  int
	statCalls  int
	singleRead int
}

func (s *kindsProofStore) DownloadURL(context.Context, string, string) (string, error) {
	s.signCalls++
	return "https://signed.example/x?X-Goog-Signature=abc", nil
}

func (s *kindsProofStore) EnsureObjectAvailable(context.Context, string, string) error {
	s.statCalls++
	return nil
}

func (s *kindsProofStore) ArtifactMetadata(context.Context, string, string) (proofdomain.Artifact, error) {
	s.singleRead++
	return proofdomain.Artifact{}, nil
}

func (s *kindsProofStore) ArtifactMetadataByIDs(_ context.Context, _ string, ids []string) (map[string]proofdomain.Artifact, error) {
	s.batchCalls = append(s.batchCalls, append([]string(nil), ids...))
	out := map[string]proofdomain.Artifact{}
	for _, id := range ids {
		// The real proof register casts to uuid: one bad id fails the WHOLE batch.
		if !uuidutil.IsUUIDString(id) {
			return nil, fmt.Errorf("invalid input syntax for type uuid: %q", id)
		}
		if a, ok := s.artifacts[id]; ok {
			out[id] = a
		}
	}
	return out, nil
}

const (
	kindPhotoID = "11111111-1111-4111-8111-111111111111"
	kindVideoID = "22222222-2222-4222-8222-222222222222"
	kindBlankID = "33333333-3333-4333-8333-333333333333"
	kindAttach  = "44444444-4444-4444-8444-444444444444"
)

func TestProofMediaKindsUsesOneBatchReadAndNoSigning(t *testing.T) {
	store := &kindsProofStore{artifacts: map[string]proofdomain.Artifact{
		kindPhotoID: {ProofID: kindPhotoID, MimeType: "image/jpeg", ProofType: "photo"},
		kindVideoID: {ProofID: kindVideoID, MimeType: "video/mp4", ProofType: "video"},
	}}
	kinds, err := NewResolver(store).ProofMediaKinds(context.Background(), "t", []string{kindPhotoID, kindVideoID, kindPhotoID})
	if err != nil {
		t.Fatal(err)
	}
	if len(store.batchCalls) != 1 || len(store.batchCalls[0]) != 2 {
		t.Fatalf("batch calls = %v, want ONE read of the two distinct ids", store.batchCalls)
	}
	if store.signCalls != 0 || store.statCalls != 0 || store.singleRead != 0 {
		t.Fatalf("kinds read signed=%d statted=%d single=%d, want none", store.signCalls, store.statCalls, store.singleRead)
	}
	if kinds[kindPhotoID] != "image/jpeg" || kinds[kindVideoID] != "video/mp4" {
		t.Fatalf("kinds = %v", kinds)
	}
}

func TestProofMediaKindsSkipsNonUUIDRefsInsteadOfFailingTheBatch(t *testing.T) {
	store := &kindsProofStore{artifacts: map[string]proofdomain.Artifact{
		kindVideoID: {ProofID: kindVideoID, MimeType: "video/mp4", ProofType: "video"},
	}}
	kinds, err := NewResolver(store).ProofMediaKinds(context.Background(), "t", []string{"legacy-slack-file", kindVideoID, " "})
	if err != nil {
		t.Fatalf("a non-uuid ref must be skipped, got %v", err)
	}
	if kinds[kindVideoID] != "video/mp4" {
		t.Fatalf("kinds = %v", kinds)
	}
	if _, ok := kinds["legacy-slack-file"]; ok {
		t.Fatalf("non-uuid ref must not be answered: %v", kinds)
	}
	// Nothing left to ask: no read at all.
	empty := &kindsProofStore{}
	if _, err := NewResolver(empty).ProofMediaKinds(context.Background(), "t", []string{"nope"}); err != nil || len(empty.batchCalls) != 0 {
		t.Fatalf("all-invalid refs: err=%v calls=%v", err, empty.batchCalls)
	}
}

func TestProofMediaKindsPrefersRecordedMimeThenProofType(t *testing.T) {
	store := &kindsProofStore{artifacts: map[string]proofdomain.Artifact{
		// Recorded mime wins over the declared proof type.
		kindPhotoID: {ProofID: kindPhotoID, MimeType: "image/png", ProofType: "video"},
		// No usable mime: the proof type decides.
		kindVideoID: {ProofID: kindVideoID, MimeType: "application/octet-stream", ProofType: "video"},
		kindBlankID: {ProofID: kindBlankID, MimeType: "", ProofType: "photo"},
		// An attachment is neither: blank, so the renderer offers a plain open tile.
		kindAttach: {ProofID: kindAttach, MimeType: "", ProofType: "attachment"},
	}}
	kinds, err := NewResolver(store).ProofMediaKinds(context.Background(), "t", []string{kindPhotoID, kindVideoID, kindBlankID, kindAttach})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{kindPhotoID: "image/png", kindVideoID: "video/mp4", kindBlankID: "image/jpeg", kindAttach: ""}
	for id, w := range want {
		if kinds[id] != w {
			t.Fatalf("kinds[%s] = %q, want %q (all=%v)", id, kinds[id], w, kinds)
		}
	}
	// No batch reader: an empty answer, never an error.
	noBatch, err := NewResolver(richDownloader{}).ProofMediaKinds(context.Background(), "t", []string{kindPhotoID})
	if err != nil || len(noBatch) != 0 {
		t.Fatalf("no batch reader: kinds=%v err=%v", noBatch, err)
	}
}
