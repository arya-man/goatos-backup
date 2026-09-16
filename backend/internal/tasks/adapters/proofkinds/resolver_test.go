package proofkinds

import (
	"context"
	"testing"

	proofdomain "github.com/vgoats/goatos/backend/internal/proof/domain"
)

type fakeProofs struct {
	calls int
	rows  map[string]proofdomain.Artifact
}

func (f *fakeProofs) GetProofsByIDs(_ context.Context, _ string, ids []string) (map[string]proofdomain.Artifact, error) {
	f.calls++
	out := map[string]proofdomain.Artifact{}
	for _, id := range ids {
		if a, ok := f.rows[id]; ok {
			out[id] = a
		}
	}
	return out, nil
}

func TestResolverReadsKindsFromTheRegisterInOneBatch(t *testing.T) {
	proofs := &fakeProofs{rows: map[string]proofdomain.Artifact{
		"v": {TenantID: "t", ProofType: "video"},
		"p": {TenantID: "t", ProofType: "photo"},
		"a": {TenantID: "t", ProofType: "attachment"},
		"x": {TenantID: "other", ProofType: "photo"},
	}}
	got, err := New(proofs).ResolveProofKinds(context.Background(), "t", []string{"v", "p", "a", "x", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if proofs.calls != 1 {
		t.Fatalf("calls = %d, want ONE batched read", proofs.calls)
	}
	if got["v"] != "video" || got["p"] != "photo" {
		t.Fatalf("kinds = %v", got)
	}
	for _, ref := range []string{"a", "x", "missing"} {
		if _, ok := got[ref]; ok {
			t.Fatalf("%q must be absent (unknown kind / other tenant / unknown ref): %v", ref, got)
		}
	}
	if empty, _ := New(proofs).ResolveProofKinds(context.Background(), "t", nil); len(empty) != 0 || proofs.calls != 1 {
		t.Fatal("no refs must not hit the register")
	}
}
