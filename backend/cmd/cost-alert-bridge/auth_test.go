package main

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/api/idtoken"
	"google.golang.org/api/option"
)

func TestReviewMissingTokenFailsClosed(t *testing.T) {
	s := &server{}
	for _, path := range []string{"/budget-pubsub", "/billing-anomaly-check", "/monitoring-webhook"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", path, nil)
		switch path {
		case "/budget-pubsub":
			s.budgetPubsub(w, r)
		case "/billing-anomaly-check":
			s.billingAnomalyCheck(w, r)
		default:
			s.monitoringWebhook(w, r)
		}
		if w.Code != 401 {
			t.Fatalf("%s unauthenticated status=%d", path, w.Code)
		}
	}
}

func TestBudgetSharedTokenAuth(t *testing.T) {
	s := &server{token: "test-secret"}
	for _, tc := range []struct {
		url, header string
		want        bool
	}{
		{"/budget-pubsub?token=test-secret", "", true},
		{"/budget-pubsub", "Bearer test-secret", true},
		{"/budget-pubsub", "test-secret", false},
		{"/budget-pubsub", "Basic test-secret", false},
		{"/budget-pubsub?token=wrong", "", false},
		{"/budget-pubsub", "", false},
	} {
		r := httptest.NewRequest("POST", tc.url, nil)
		r.Header.Set("Authorization", tc.header)
		if got := s.authorized(r); got != tc.want {
			t.Fatalf("%s header=%q authorized=%v", tc.url, tc.header, got)
		}
	}
}

func TestBudgetGoogleOIDCSignedTokens(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	encode := base64.RawURLEncoding.EncodeToString
	publicKey := map[string]any{"keys": []any{map[string]string{"kty": "RSA", "alg": "RS256", "use": "sig", "kid": "test-key", "n": encode(key.N.Bytes()), "e": encode(big.NewInt(int64(key.E)).Bytes())}}}
	jwks, _ := json.Marshal(publicKey)
	client := &http.Client{Transport: stateRoundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(jwks))), Request: r}, nil
	})}
	validator, err := idtoken.NewValidator(context.Background(), option.WithHTTPClient(client))
	if err != nil {
		t.Fatal(err)
	}
	const audience = "https://cost-alert-bridge.example"
	const email = "cost-alert@example.iam.gserviceaccount.com"
	sign := func(changes map[string]any) string {
		t.Helper()
		claims := map[string]any{"iss": "https://accounts.google.com", "aud": audience, "exp": time.Now().Add(time.Hour).Unix(), "email": email, "email_verified": true}
		for k, v := range changes {
			claims[k] = v
		}
		data, _ := json.Marshal(claims)
		content := encode([]byte(`{"alg":"RS256","kid":"test-key","typ":"JWT"}`)) + "." + encode(data)
		hash := sha256.Sum256([]byte(content))
		signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
		if err != nil {
			t.Fatal(err)
		}
		return content + "." + encode(signature)
	}
	s := &server{oidcAudience: audience, oidcEmails: []string{email}, validateIDToken: validator.Validate, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	for _, tc := range []struct {
		name, path string
		claims     map[string]any
		want       bool
	}{
		{"budget OIDC without shared token", "/budget-pubsub", nil, true},
		{"scheduler OIDC", "/billing-anomaly-check", nil, true},
		{"wrong route", "/monitoring-webhook", nil, false},
		{"wrong audience", "/budget-pubsub", map[string]any{"aud": "wrong"}, false},
		{"expired", "/budget-pubsub", map[string]any{"exp": time.Now().Add(-time.Hour).Unix()}, false},
		{"wrong service account", "/budget-pubsub", map[string]any{"email": "other@example.com"}, false},
		{"missing email", "/budget-pubsub", map[string]any{"email": nil}, false},
		{"unverified email", "/budget-pubsub", map[string]any{"email_verified": false}, false},
		{"wrong issuer", "/budget-pubsub", map[string]any{"iss": "https://attacker.example"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", tc.path, nil)
			r.Header.Set("Authorization", "Bearer "+sign(tc.claims))
			if got := s.authorized(r); got != tc.want {
				t.Fatalf("authorized=%v want=%v", got, tc.want)
			}
		})
	}
	token := sign(nil)
	parts := strings.Split(token, ".")
	signature, _ := base64.RawURLEncoding.DecodeString(parts[2])
	signature[0] ^= 0xff
	parts[2] = encode(signature)
	r := httptest.NewRequest("POST", "/budget-pubsub", nil)
	r.Header.Set("Authorization", "Bearer "+strings.Join(parts, "."))
	if s.authorized(r) {
		t.Fatal("bad signature accepted")
	}
	for _, broken := range []server{{oidcAudience: audience}, {oidcEmails: []string{email}}} {
		r.Header.Set("Authorization", "Bearer "+token)
		if broken.authorized(r) {
			t.Fatal("incomplete OIDC config accepted")
		}
	}
	// Exercise the real Pub/Sub route with signed OIDC and no query/shared token.
	posts := 0
	slack := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { posts++; w.WriteHeader(200) }))
	defer slack.Close()
	s.http = slack.Client()
	s.webhook = slack.URL
	s.now = time.Now
	s.budgetStore = newMemoryBudgetStore()
	push := pubsubPushForTest(repeatedBudget)
	push.Message.PublishAt = time.Now().Format(time.RFC3339Nano)
	data, _ := json.Marshal(push)
	for i := 0; i < 2; i++ {
		r := httptest.NewRequest("POST", "/budget-pubsub", strings.NewReader(string(data)))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		s.budgetPubsub(w, r)
		if w.Code != 204 {
			t.Fatalf("OIDC route status=%d", w.Code)
		}
	}
	if posts != 1 {
		t.Fatalf("OIDC repeat posted %d messages", posts)
	}
}
