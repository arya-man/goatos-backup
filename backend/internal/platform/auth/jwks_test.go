package auth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// ── key generation helpers ───────────────────────────────────────────────────

func generateRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	return k
}

func generateECKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey: %v", err)
	}
	return k
}

// ── JWKS serving helpers ─────────────────────────────────────────────────────

type jwkEntry struct {
	Kid string
	Kty string
	Alg string
	// RSA
	N string
	E string
	// EC
	Crv string
	X   string
	Y   string
}

func rsaJWKEntry(kid string, pub *rsa.PublicKey) jwkEntry {
	return jwkEntry{
		Kid: kid,
		Kty: "RSA",
		Alg: "RS256",
		N:   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}
}

func ecJWKEntry(kid string, pub *ecdsa.PublicKey) jwkEntry {
	coordLen := (pub.Curve.Params().BitSize + 7) / 8
	xBytes := make([]byte, coordLen)
	yBytes := make([]byte, coordLen)
	pub.X.FillBytes(xBytes)
	pub.Y.FillBytes(yBytes)
	return jwkEntry{
		Kid: kid,
		Kty: "EC",
		Alg: "ES256",
		Crv: "P-256",
		X:   base64.RawURLEncoding.EncodeToString(xBytes),
		Y:   base64.RawURLEncoding.EncodeToString(yBytes),
	}
}

// jwksServer starts an httptest server serving the supplied entries.
// entries is an atomic pointer so tests can swap the key set mid-test.
func jwksServer(t *testing.T, entriesPtr *atomic.Pointer[[]jwkEntry]) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		entries := *entriesPtr.Load()
		type wireKey struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			Alg string `json:"alg,omitempty"`
			N   string `json:"n,omitempty"`
			E   string `json:"e,omitempty"`
			Crv string `json:"crv,omitempty"`
			X   string `json:"x,omitempty"`
			Y   string `json:"y,omitempty"`
		}
		keys := make([]wireKey, len(entries))
		for i, e := range entries {
			keys[i] = wireKey{
				Kid: e.Kid, Kty: e.Kty, Alg: e.Alg,
				N: e.N, E: e.E, Crv: e.Crv, X: e.X, Y: e.Y,
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// ── JWT minting helpers ───────────────────────────────────────────────────────

func mintRS256Token(t *testing.T, kid string, key *rsa.PrivateKey, payload map[string]any) string {
	t.Helper()
	header := map[string]any{"alg": "RS256", "typ": "JWT", "kid": kid}
	h := encodeJSONSegment(t, header)
	p := encodeJSONSegment(t, payload)
	sigInput := h + "." + p
	digest := sha256.Sum256([]byte(sigInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("rsa.SignPKCS1v15: %v", err)
	}
	return sigInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func mintES256Token(t *testing.T, kid string, key *ecdsa.PrivateKey, payload map[string]any) string {
	t.Helper()
	header := map[string]any{"alg": "ES256", "typ": "JWT", "kid": kid}
	h := encodeJSONSegment(t, header)
	p := encodeJSONSegment(t, payload)
	sigInput := h + "." + p
	digest := sha256.Sum256([]byte(sigInput))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatalf("ecdsa.Sign: %v", err)
	}
	// Encode as fixed 64-byte r||s.
	coordLen := 32
	sig := make([]byte, 2*coordLen)
	r.FillBytes(sig[:coordLen])
	s.FillBytes(sig[coordLen:])
	return sigInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// validPayload returns a minimal valid JWT payload for the test constants.
func validPayload(now time.Time) map[string]any {
	return map[string]any{
		"iss":       testIssuer,
		"aud":       testAudience,
		"sub":       testSubject,
		"tenant_id": testTenant,
		"exp":       now.Add(time.Hour).Unix(),
		"nbf":       now.Add(-time.Minute).Unix(),
	}
}

// newTestJWKSVerifier builds a JWKSVerifier pointed at srv with a fixed clock.
func newTestJWKSVerifier(t *testing.T, srv *httptest.Server, now time.Time, extraOpts ...func(*JWKSConfig)) *JWKSVerifier {
	t.Helper()
	cfg := JWKSConfig{
		JWKSURL:  srv.URL + "/.well-known/jwks.json",
		Issuer:   testIssuer,
		Audience: testAudience,
		Now:      func() time.Time { return now },
		// Use a zero CacheTTL so the cache expires immediately, allowing refresh.
		// Tests that need caching behaviour set their own value via extraOpts.
		CacheTTL:   1 * time.Millisecond,
		HTTPClient: srv.Client(),
	}
	for _, opt := range extraOpts {
		opt(&cfg)
	}
	v, err := NewJWKSVerifier(cfg)
	if err != nil {
		t.Fatalf("NewJWKSVerifier: %v", err)
	}
	return v
}

// ── core acceptance tests ─────────────────────────────────────────────────────

func TestJWKSVerifierAcceptsValidRS256Token(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	key := generateRSAKey(t)
	entries := []jwkEntry{rsaJWKEntry("rsa-kid-1", &key.PublicKey)}
	var ep atomic.Pointer[[]jwkEntry]
	ep.Store(&entries)
	srv := jwksServer(t, &ep)

	v := newTestJWKSVerifier(t, srv, now)
	token := mintRS256Token(t, "rsa-kid-1", key, validPayload(now))
	claims, err := v.Verify(token)
	if err != nil {
		t.Fatalf("Verify RS256 token: %v", err)
	}
	if claims.Subject != testSubject || claims.TenantID != testTenant {
		t.Fatalf("unexpected claims: %#v", claims)
	}
}

func TestJWKSVerifierAcceptsValidES256Token(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	key := generateECKey(t)
	entries := []jwkEntry{ecJWKEntry("ec-kid-1", &key.PublicKey)}
	var ep atomic.Pointer[[]jwkEntry]
	ep.Store(&entries)
	srv := jwksServer(t, &ep)

	v := newTestJWKSVerifier(t, srv, now)
	token := mintES256Token(t, "ec-kid-1", key, validPayload(now))
	claims, err := v.Verify(token)
	if err != nil {
		t.Fatalf("Verify ES256 token: %v", err)
	}
	if claims.Subject != testSubject || claims.TenantID != testTenant {
		t.Fatalf("unexpected claims: %#v", claims)
	}
}

func TestJWKSVerifierAcceptsExternalIDPSubjectWithoutTenantClaim(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	key := generateRSAKey(t)
	entries := []jwkEntry{rsaJWKEntry("rsa-kid-1", &key.PublicKey)}
	var ep atomic.Pointer[[]jwkEntry]
	ep.Store(&entries)
	srv := jwksServer(t, &ep)

	v := newTestJWKSVerifier(t, srv, now)
	externalSubject := "firebase-uid-abc123"
	token := mintRS256Token(t, "rsa-kid-1", key, map[string]any{
		"iss": testIssuer,
		"aud": testAudience,
		"sub": externalSubject,
		"exp": now.Add(time.Hour).Unix(),
	})
	claims, err := v.Verify(token)
	if err != nil {
		t.Fatalf("Verify external subject token: %v", err)
	}
	if claims.Subject != StableSubjectID(testIssuer, externalSubject) {
		t.Fatalf("subject=%s want stable mapped subject", claims.Subject)
	}
	if claims.ExternalSubject != externalSubject {
		t.Fatalf("external_subject=%s want %s", claims.ExternalSubject, externalSubject)
	}
	if claims.TenantID != "" {
		t.Fatalf("tenant=%s want no tenant claim", claims.TenantID)
	}
}

// ── rejection table ───────────────────────────────────────────────────────────

func TestJWKSVerifierRejectsInvalidTokens(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	rsaKey := generateRSAKey(t)
	ecKey := generateECKey(t)
	entries := []jwkEntry{
		rsaJWKEntry("rsa-kid-1", &rsaKey.PublicKey),
		ecJWKEntry("ec-kid-1", &ecKey.PublicKey),
	}
	var ep atomic.Pointer[[]jwkEntry]
	ep.Store(&entries)
	srv := jwksServer(t, &ep)
	v := newTestJWKSVerifier(t, srv, now)

	base := validPayload(now)

	tests := []struct {
		name  string
		token string
	}{
		{
			name:  "malformed",
			token: "not-a-token",
		},
		{
			name:  "alg=none",
			token: noneAlgToken(t, "rsa-kid-1", base),
		},
		{
			name: "HS256 token rejected by jwks verifier",
			token: signTestJWT(t,
				map[string]any{"alg": "HS256", "kid": "rsa-kid-1"},
				base,
				testSecret),
		},
		{
			name:  "wrong issuer",
			token: mintRS256Token(t, "rsa-kid-1", rsaKey, withClaim(base, "iss", "wrong-issuer")),
		},
		{
			name:  "wrong audience",
			token: mintRS256Token(t, "rsa-kid-1", rsaKey, withClaim(base, "aud", "wrong-audience")),
		},
		{
			name:  "expired",
			token: mintRS256Token(t, "rsa-kid-1", rsaKey, withClaim(base, "exp", now.Add(-2*time.Minute).Unix())),
		},
		{
			name:  "nbf in future",
			token: mintRS256Token(t, "rsa-kid-1", rsaKey, withClaim(base, "nbf", now.Add(2*time.Minute).Unix())),
		},
		{
			name:  "unknown kid",
			token: mintRS256Token(t, "unknown-kid-99", rsaKey, base),
		},
		{
			name:  "missing kid in header",
			token: mintRS256TokenNoKid(t, rsaKey, base),
		},
		{
			name:  "missing sub",
			token: mintRS256Token(t, "rsa-kid-1", rsaKey, withoutClaim(base, "sub")),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := v.Verify(tt.token); err == nil {
				t.Fatal("Verify succeeded, want error")
			}
		})
	}
}

// ── clock skew boundary test ──────────────────────────────────────────────────

func TestJWKSVerifierClockSkewBoundary(t *testing.T) {
	rsaKey := generateRSAKey(t)
	entries := []jwkEntry{rsaJWKEntry("rsa-kid-1", &rsaKey.PublicKey)}
	var ep atomic.Pointer[[]jwkEntry]
	ep.Store(&entries)
	srv := jwksServer(t, &ep)

	// Token expired 30s ago, clock skew is 60s → should be accepted.
	now := time.Unix(1_700_000_000, 0).UTC()
	tokenExpired30s := mintRS256Token(t, "rsa-kid-1", rsaKey, map[string]any{
		"iss":       testIssuer,
		"aud":       testAudience,
		"sub":       testSubject,
		"tenant_id": testTenant,
		"exp":       now.Add(-30 * time.Second).Unix(),
		"nbf":       now.Add(-2 * time.Minute).Unix(),
	})

	v := newTestJWKSVerifier(t, srv, now, func(c *JWKSConfig) {
		c.ClockSkew = 60 * time.Second
	})
	if _, err := v.Verify(tokenExpired30s); err != nil {
		t.Fatalf("token within skew window should be accepted: %v", err)
	}

	// Token expired 90s ago, clock skew is 60s → should be rejected.
	tokenExpired90s := mintRS256Token(t, "rsa-kid-1", rsaKey, map[string]any{
		"iss":       testIssuer,
		"aud":       testAudience,
		"sub":       testSubject,
		"tenant_id": testTenant,
		"exp":       now.Add(-90 * time.Second).Unix(),
		"nbf":       now.Add(-3 * time.Minute).Unix(),
	})
	if _, err := v.Verify(tokenExpired90s); err == nil {
		t.Fatal("token outside skew window should be rejected")
	}

	// nbf 30s in the future, clock skew 60s → should be accepted.
	tokenFutureNBF30s := mintRS256Token(t, "rsa-kid-1", rsaKey, map[string]any{
		"iss":       testIssuer,
		"aud":       testAudience,
		"sub":       testSubject,
		"tenant_id": testTenant,
		"exp":       now.Add(time.Hour).Unix(),
		"nbf":       now.Add(30 * time.Second).Unix(),
	})
	if _, err := v.Verify(tokenFutureNBF30s); err != nil {
		t.Fatalf("nbf within skew window should be accepted: %v", err)
	}

	// nbf 90s in the future, clock skew 60s → should be rejected.
	tokenFutureNBF90s := mintRS256Token(t, "rsa-kid-1", rsaKey, map[string]any{
		"iss":       testIssuer,
		"aud":       testAudience,
		"sub":       testSubject,
		"tenant_id": testTenant,
		"exp":       now.Add(time.Hour).Unix(),
		"nbf":       now.Add(90 * time.Second).Unix(),
	})
	if _, err := v.Verify(tokenFutureNBF90s); err == nil {
		t.Fatal("nbf outside skew window should be rejected")
	}
}

// ── JWKS refresh picks up a newly-added kid ───────────────────────────────────

func TestJWKSVerifierRefreshPicksUpNewKid(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	rsaKey1 := generateRSAKey(t)
	rsaKey2 := generateRSAKey(t)

	initial := []jwkEntry{rsaJWKEntry("rsa-kid-1", &rsaKey1.PublicKey)}
	var ep atomic.Pointer[[]jwkEntry]
	ep.Store(&initial)
	srv := jwksServer(t, &ep)

	// Very short cache TTL so the verifier refreshes on every call.
	v := newTestJWKSVerifier(t, srv, now, func(c *JWKSConfig) {
		c.CacheTTL = 1 * time.Millisecond
	})

	// rsa-kid-1 works.
	tok1 := mintRS256Token(t, "rsa-kid-1", rsaKey1, validPayload(now))
	if _, err := v.Verify(tok1); err != nil {
		t.Fatalf("first key should be accepted: %v", err)
	}

	// Add rsa-kid-2 to the JWKS.
	updated := []jwkEntry{
		rsaJWKEntry("rsa-kid-1", &rsaKey1.PublicKey),
		rsaJWKEntry("rsa-kid-2", &rsaKey2.PublicKey),
	}
	ep.Store(&updated)

	// Advance the verifier's clock past the minimum refresh interval so it
	// is willing to re-fetch.
	advancedNow := now.Add(minJWKSRefreshInterval + time.Second)
	v.now = func() time.Time { return advancedNow }

	tok2 := mintRS256Token(t, "rsa-kid-2", rsaKey2, validPayload(advancedNow))
	if _, err := v.Verify(tok2); err != nil {
		t.Fatalf("newly-added key should be accepted after refresh: %v", err)
	}
}

// ── NewJWKSVerifier constructor validation ────────────────────────────────────

func TestNewJWKSVerifierRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  JWKSConfig
	}{
		{
			name: "missing jwks_url",
			cfg:  JWKSConfig{Issuer: testIssuer, Audience: testAudience},
		},
		{
			name: "missing issuer",
			cfg:  JWKSConfig{JWKSURL: "http://example.com/jwks", Audience: testAudience},
		},
		{
			name: "missing audience",
			cfg:  JWKSConfig{JWKSURL: "http://example.com/jwks", Issuer: testIssuer},
		},
		{
			name: "symmetric alg in allow-list",
			cfg: JWKSConfig{
				JWKSURL:     "http://example.com/jwks",
				Issuer:      testIssuer,
				Audience:    testAudience,
				AllowedAlgs: []string{"HS256"},
			},
		},
		{
			name: "none alg in allow-list",
			cfg: JWKSConfig{
				JWKSURL:     "http://example.com/jwks",
				Issuer:      testIssuer,
				Audience:    testAudience,
				AllowedAlgs: []string{"none"},
			},
		},
		{
			name: "negative clock skew",
			cfg: JWKSConfig{
				JWKSURL:   "http://example.com/jwks",
				Issuer:    testIssuer,
				Audience:  testAudience,
				ClockSkew: -time.Second,
			},
		},
		{
			name: "negative max ttl",
			cfg: JWKSConfig{
				JWKSURL:  "http://example.com/jwks",
				Issuer:   testIssuer,
				Audience: testAudience,
				MaxTTL:   -time.Hour,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewJWKSVerifier(tt.cfg); err == nil {
				t.Fatal("NewJWKSVerifier accepted invalid config, want error")
			}
		})
	}
}

// ── audience formats ──────────────────────────────────────────────────────────

func TestJWKSVerifierAcceptsAudienceArray(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	key := generateRSAKey(t)
	entries := []jwkEntry{rsaJWKEntry("kid-1", &key.PublicKey)}
	var ep atomic.Pointer[[]jwkEntry]
	ep.Store(&entries)
	srv := jwksServer(t, &ep)

	v := newTestJWKSVerifier(t, srv, now)
	payload := map[string]any{
		"iss":       testIssuer,
		"aud":       []string{"other-service", testAudience},
		"sub":       testSubject,
		"tenant_id": testTenant,
		"exp":       now.Add(time.Hour).Unix(),
		"nbf":       now.Add(-time.Minute).Unix(),
	}
	token := mintRS256Token(t, "kid-1", key, payload)
	if _, err := v.Verify(token); err != nil {
		t.Fatalf("array aud containing configured audience should be accepted: %v", err)
	}
}

// ── max TTL enforcement ───────────────────────────────────────────────────────

func TestJWKSVerifierEnforcesMaxTTL(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	key := generateRSAKey(t)
	entries := []jwkEntry{rsaJWKEntry("kid-1", &key.PublicKey)}
	var ep atomic.Pointer[[]jwkEntry]
	ep.Store(&entries)
	srv := jwksServer(t, &ep)

	v := newTestJWKSVerifier(t, srv, now, func(c *JWKSConfig) {
		c.MaxTTL = time.Hour
	})

	// Token valid for exactly 1h → accepted.
	ok := mintRS256Token(t, "kid-1", key, map[string]any{
		"iss":       testIssuer,
		"aud":       testAudience,
		"sub":       testSubject,
		"tenant_id": testTenant,
		"exp":       now.Add(time.Hour).Unix(),
		"nbf":       now.Add(-time.Minute).Unix(),
	})
	if _, err := v.Verify(ok); err != nil {
		t.Fatalf("token at max TTL should be accepted: %v", err)
	}

	// Token valid for 25h → rejected.
	tooLong := mintRS256Token(t, "kid-1", key, map[string]any{
		"iss":       testIssuer,
		"aud":       testAudience,
		"sub":       testSubject,
		"tenant_id": testTenant,
		"exp":       now.Add(25 * time.Hour).Unix(),
		"nbf":       now.Add(-time.Minute).Unix(),
	})
	if _, err := v.Verify(tooLong); err == nil {
		t.Fatal("token exceeding max TTL should be rejected")
	}
}

// ── helper: token with alg=none ───────────────────────────────────────────────

func noneAlgToken(t *testing.T, kid string, payload map[string]any) string {
	t.Helper()
	header := map[string]any{"alg": "none", "kid": kid}
	h := encodeJSONSegment(t, header)
	p := encodeJSONSegment(t, payload)
	return h + "." + p + "."
}

// ── helper: RS256 token without a kid field ───────────────────────────────────

func mintRS256TokenNoKid(t *testing.T, key *rsa.PrivateKey, payload map[string]any) string {
	t.Helper()
	header := map[string]any{"alg": "RS256", "typ": "JWT"}
	h := encodeJSONSegment(t, header)
	p := encodeJSONSegment(t, payload)
	sigInput := h + "." + p
	digest := sha256.Sum256([]byte(sigInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("rsa.SignPKCS1v15: %v", err)
	}
	return sigInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}
