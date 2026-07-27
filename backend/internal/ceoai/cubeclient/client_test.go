package cubeclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(t *testing.T, url string) *Client {
	t.Helper()
	c, err := New(Config{BaseURL: url, APISecret: "test-secret", MaxRetries: 2})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestNewRequiresConfig(t *testing.T) {
	if _, err := New(Config{}); err != ErrNotConfigured {
		t.Fatalf("want ErrNotConfigured, got %v", err)
	}
	if _, err := New(Config{BaseURL: "http://x"}); err != ErrNotConfigured {
		t.Fatalf("want ErrNotConfigured (no secret), got %v", err)
	}
}

func TestLoadRequiresTenant(t *testing.T) {
	c := newTestClient(t, "http://127.0.0.1:0")
	if _, err := c.Load(context.Background(), "  ", Query{Measures: []string{"kpi_animals.active_animal_count"}}); err != ErrMissingTenant {
		t.Fatalf("want ErrMissingTenant, got %v", err)
	}
}

func TestLoadSignsTenantAndReturnsRows(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"kpi_vaccination.vaccination_overdue":"108","kpi_vaccination.park_label":"Coimbatore"}]}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	res, err := c.Load(context.Background(), "tenant-xyz", Query{
		Measures:   []string{"kpi_vaccination.vaccination_overdue"},
		Dimensions: []string{"kpi_vaccination.park_label"},
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(res.Rows) != 1 || res.Rows[0]["kpi_vaccination.vaccination_overdue"] != "108" {
		t.Fatalf("unexpected rows: %+v", res.Rows)
	}

	// The tenant must be signed into the JWT payload, not present as a query field.
	tenant := decodeJWTClaim(t, gotAuth, "tenant_id")
	if tenant != "tenant-xyz" {
		t.Fatalf("tenant not signed into token: %q", tenant)
	}
	securityContext := decodeJWTSecurityContext(t, gotAuth)
	if securityContext["tenant_id"] != "tenant-xyz" {
		t.Fatalf("tenant not signed into Cube securityContext: %v", securityContext)
	}
	q, _ := gotBody["query"].(map[string]any)
	if _, leaked := q["filters"]; leaked {
		t.Fatalf("query must not carry a tenant filter; got filters: %v", q["filters"])
	}

	// Hard row-limit ceiling must be applied.
	if lim, _ := q["limit"].(float64); int(lim) != defaultRowLimit {
		t.Fatalf("want limit %d, got %v", defaultRowLimit, q["limit"])
	}
}

func TestLoadRetriesOnContinueWaitThenSucceeds(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if atomic.AddInt32(&calls, 1) == 1 {
			_, _ = w.Write([]byte(`{"error":"Continue wait"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"kpi_animals.active_animal_count":"1308"}]}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	res, err := c.Load(context.Background(), "t", Query{Measures: []string{"kpi_animals.active_animal_count"}})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if res.Rows[0]["kpi_animals.active_animal_count"] != "1308" {
		t.Fatalf("unexpected: %+v", res.Rows)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("want 2 calls (1 wait + 1 ok), got %d", calls)
	}
}

func TestLoadUnauthorizedIsTerminal(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"forbidden"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	_, err := c.Load(context.Background(), "t", Query{Measures: []string{"kpi_animals.active_animal_count"}})
	if err != ErrUnauthorized {
		t.Fatalf("want ErrUnauthorized, got %v", err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("unauthorized must not retry; got %d calls", calls)
	}
}

func TestLoadServerErrorRetriesThenAPIError(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	_, err := c.Load(context.Background(), "t", Query{Measures: []string{"kpi_animals.active_animal_count"}})
	var apiErr *APIError
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("want APIError containing boom, got %v", err)
	}
	if !as(err, &apiErr) || apiErr.StatusCode != 500 {
		t.Fatalf("want *APIError 500, got %v", err)
	}
	// 1 initial + 2 retries = 3 attempts.
	if atomic.LoadInt32(&calls) != 3 {
		t.Fatalf("want 3 attempts, got %d", calls)
	}
}

func TestLoadRespectsContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := c.Load(ctx, "t", Query{Measures: []string{"kpi_animals.active_animal_count"}}); err == nil {
		t.Fatal("want error on cancelled context")
	}
}

// --- helpers ---------------------------------------------------------------

func as(err error, target **APIError) bool {
	for err != nil {
		if e, ok := err.(*APIError); ok {
			*target = e
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func decodeJWTClaim(t *testing.T, token, claim string) string {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("not a JWT: %q", token)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(payload, &m); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	s, _ := m[claim].(string)
	return s
}

func decodeJWTSecurityContext(t *testing.T, token string) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("not a JWT: %q", token)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(payload, &m); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	sc, _ := m["securityContext"].(map[string]any)
	return sc
}
