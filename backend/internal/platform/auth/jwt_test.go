package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const (
	testSecret   = "0123456789abcdef0123456789abcdef"
	testIssuer   = "goatos-test"
	testAudience = "goatos-api"
	testSubject  = "90000000-0000-4000-8000-000000000001"
	testTenant   = "00000000-0000-4000-8000-000000000001"
)

func TestHS256VerifierAcceptsValidTokenAndIgnoresRoleClaim(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	verifier := newTestVerifier(t, now, testSecret)
	token := signTestJWT(t, map[string]any{"alg": "HS256", "typ": "JWT"}, map[string]any{
		"iss":       testIssuer,
		"aud":       testAudience,
		"sub":       testSubject,
		"tenant_id": testTenant,
		"exp":       now.Add(time.Hour).Unix(),
		"nbf":       now.Add(-time.Minute).Unix(),
		"role":      "admin",
	}, testSecret)

	claims, err := verifier.Verify(token)
	if err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
	if claims.Subject != testSubject || claims.TenantID != testTenant {
		t.Fatalf("claims = %#v", claims)
	}
}

func TestHS256VerifierRejectsInvalidTokens(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	verifier := newTestVerifier(t, now, testSecret)
	validPayload := map[string]any{
		"iss":       testIssuer,
		"aud":       testAudience,
		"sub":       testSubject,
		"tenant_id": testTenant,
		"exp":       now.Add(time.Hour).Unix(),
		"nbf":       now.Add(-time.Minute).Unix(),
	}

	tests := []struct {
		name    string
		header  map[string]any
		payload map[string]any
		secret  string
		token   string
	}{
		{name: "malformed", token: "not-a-token"},
		{name: "alg none", header: map[string]any{"alg": "none"}, payload: validPayload, secret: testSecret},
		{name: "wrong alg", header: map[string]any{"alg": "HS512"}, payload: validPayload, secret: testSecret},
		{name: "wrong secret", header: map[string]any{"alg": "HS256"}, payload: validPayload, secret: "abcdef0123456789abcdef0123456789"},
		{name: "wrong issuer", header: map[string]any{"alg": "HS256"}, payload: withClaim(validPayload, "iss", "wrong"), secret: testSecret},
		{name: "wrong audience", header: map[string]any{"alg": "HS256"}, payload: withClaim(validPayload, "aud", "wrong"), secret: testSecret},
		{name: "expired", header: map[string]any{"alg": "HS256"}, payload: withClaim(validPayload, "exp", now.Add(-time.Second).Unix()), secret: testSecret},
		{name: "future nbf", header: map[string]any{"alg": "HS256"}, payload: withClaim(validPayload, "nbf", now.Add(time.Hour).Unix()), secret: testSecret},
		{name: "missing sub", header: map[string]any{"alg": "HS256"}, payload: withoutClaim(validPayload, "sub"), secret: testSecret},
		{name: "missing tenant", header: map[string]any{"alg": "HS256"}, payload: withoutClaim(validPayload, "tenant_id"), secret: testSecret},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := tt.token
			if token == "" {
				token = signTestJWT(t, tt.header, tt.payload, tt.secret)
			}
			if _, err := verifier.Verify(token); err == nil {
				t.Fatal("Verify succeeded, want error")
			}
		})
	}
}

func TestNewHS256VerifierRejectsWeakSecret(t *testing.T) {
	if _, err := NewHS256Verifier(Config{Issuer: testIssuer, Audience: testAudience, Secret: []byte("short")}); err == nil {
		t.Fatal("expected weak secret error")
	}
}

func newTestVerifier(t *testing.T, now time.Time, secret string) *HS256Verifier {
	t.Helper()
	verifier, err := NewHS256Verifier(Config{
		Issuer:   testIssuer,
		Audience: testAudience,
		Secret:   []byte(secret),
		Now:      func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewHS256Verifier: %v", err)
	}
	return verifier
}

func signTestJWT(t *testing.T, header map[string]any, payload map[string]any, secret string) string {
	t.Helper()
	headerSegment := encodeJSONSegment(t, header)
	payloadSegment := encodeJSONSegment(t, payload)
	data := headerSegment + "." + payloadSegment
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(data))
	return data + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func encodeJSONSegment(t *testing.T, payload map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func withClaim(base map[string]any, key string, value any) map[string]any {
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	out[key] = value
	return out
}

func withoutClaim(base map[string]any, key string) map[string]any {
	out := map[string]any{}
	for k, v := range base {
		if !strings.EqualFold(k, key) {
			out[k] = v
		}
	}
	return out
}
