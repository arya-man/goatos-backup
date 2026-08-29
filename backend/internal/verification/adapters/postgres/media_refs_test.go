package postgres

import (
	"reflect"
	"testing"
)

func TestDecodeMediaRefsAcceptsStringArray(t *testing.T) {
	got, err := decodeMediaRefs([]byte(`["proof-a","proof-b"]`))
	if err != nil {
		t.Fatalf("decodeMediaRefs: %v", err)
	}
	want := []string{"proof-a", "proof-b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("media refs = %#v, want %#v", got, want)
	}
}

func TestDecodeMediaRefsAcceptsObjectArrayFromLegacyRows(t *testing.T) {
	got, err := decodeMediaRefs([]byte(`[
		{"proof_id":"proof-a","label":"front"},
		{"proofRef":"proof-b","kind":"video"},
		{"artifactId":"proof-c"}
	]`))
	if err != nil {
		t.Fatalf("decodeMediaRefs: %v", err)
	}
	want := []string{"proof-a", "proof-b", "proof-c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("media refs = %#v, want %#v", got, want)
	}
}

func TestDecodeMediaRefsIgnoresObjectWithoutProofID(t *testing.T) {
	got, err := decodeMediaRefs([]byte(`[{"label":"front"},"proof-b"]`))
	if err != nil {
		t.Fatalf("decodeMediaRefs: %v", err)
	}
	want := []string{"proof-b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("media refs = %#v, want %#v", got, want)
	}
}
