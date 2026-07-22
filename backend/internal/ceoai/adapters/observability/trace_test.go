package observability

import (
	"context"
	"strings"
	"testing"
)

func TestRedactSecrets_ScrubsCredentialShapes(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"bearer", "Authorization: Bearer abc.def.ghijklmnop"},
		{"jwt", "token eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9payload"},
		{"apikey_kv", "api_key=sk-supersecretkey1234567890"},
		{"google", "key AIzaSyA1234567890abcdefghijklmnopqrstuv"},
		{"password_kv", "password: hunter2secret"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RedactSecrets(tc.in)
			if !strings.Contains(got, redactedToken) {
				t.Fatalf("expected redaction in %q, got %q", tc.in, got)
			}
		})
	}
}

func TestRedactSecrets_KeepsGoatTags(t *testing.T) {
	// Goat RFID/tags are NOT PII and must survive for debugging.
	in := "shed=Castro 1 rfid=982000123456789 old_tag=GT-4471"
	got := RedactSecrets(in)
	if got != in {
		t.Fatalf("goat tags must not be redacted: %q -> %q", in, got)
	}
}

func TestTraceRecord_SanitizeScrubsAllFields(t *testing.T) {
	rec := TraceRecord{
		TenantID:         "t1",
		RequestID:        "r1",
		QuestionRedacted: "why overdue? token=sk-abcdefghijklmnop1234",
		RejectionReason:  "password: leaked123",
		Steps: []StepTrace{
			{SubQuestion: "auth eyJabcdefghijklmnop.more", Params: "api_key=sk-1234567890abcdef", Err: "Bearer zzzzzzzzzzzzzzzz"},
		},
	}
	san := rec.Sanitize()
	if HasSecretLeak(san) {
		t.Fatalf("sanitized record still leaks a secret: %+v", san)
	}
}

func TestMemoryTraceStore_RecordSanitizesAndScopes(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryTraceStore(4)

	if err := store.RecordTrace(ctx, TraceRecord{
		TenantID:         "tenant-a",
		RequestID:        "req-1",
		QuestionRedacted: "hello token=sk-abcdefghijklmnop1234",
	}); err != nil {
		t.Fatalf("record: %v", err)
	}

	// Same request id, different tenant must not be visible cross-tenant.
	got, err := store.GetTrace(ctx, "tenant-a", "req-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if HasSecretLeak(got) {
		t.Fatalf("stored trace leaks secret: %+v", got)
	}
	if _, err := store.GetTrace(ctx, "tenant-b", "req-1"); err != ErrTraceNotFound {
		t.Fatalf("cross-tenant read must be not-found, got %v", err)
	}
	if _, err := store.GetTrace(ctx, "tenant-a", "missing"); err != ErrTraceNotFound {
		t.Fatalf("missing read must be not-found, got %v", err)
	}
}

func TestMemoryTraceStore_EvictsOldest(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryTraceStore(2)
	for _, id := range []string{"a", "b", "c"} {
		if err := store.RecordTrace(ctx, TraceRecord{TenantID: "t", RequestID: id}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.GetTrace(ctx, "t", "a"); err != ErrTraceNotFound {
		t.Fatalf("oldest trace 'a' should have been evicted, got %v", err)
	}
	if _, err := store.GetTrace(ctx, "t", "c"); err != nil {
		t.Fatalf("newest trace 'c' should be present, got %v", err)
	}
}
