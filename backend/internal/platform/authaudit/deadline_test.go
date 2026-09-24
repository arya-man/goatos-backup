package authaudit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/permissions"
	platformauth "github.com/vgoats/goatos/backend/internal/platform/auth"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

type deadlineRecorder struct {
	err      error
	deadline time.Time
	seen     bool
}

func (r *deadlineRecorder) Record(ctx context.Context, _ Event) error {
	r.deadline, r.seen = ctx.Deadline()
	return r.err
}

type deadlineClaimer struct {
	err      error
	deadline time.Time
	seen     bool
}

func (c *deadlineClaimer) ClaimPendingEmailGrant(ctx context.Context, _ permissions.PendingEmailGrantClaim) (permissions.PendingEmailGrantResult, error) {
	c.deadline, c.seen = ctx.Deadline()
	return permissions.PendingEmailGrantResult{}, c.err
}

type ctxDynamicEmails struct{ block bool }

func (d ctxDynamicEmails) EmailAllowed(ctx context.Context, _, _ string) bool {
	if d.block {
		<-ctx.Done()
	}
	return false
}

func deadlineTestClaims() platformauth.Claims {
	verified := true
	return platformauth.Claims{Subject: testActorID, Email: "ravi@mesha.sg", EmailVerified: &verified, Expires: time.Unix(1_800_000_000, 0).UTC()}
}

func serveSignIn(t *testing.T, h *Handler) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/auth/session-events", strings.NewReader(`{"event_type":"auth.sign_in","source":"admin-web"}`))
	req.Header.Set("Authorization", "Bearer t")
	req.Header.Set(httpmiddleware.TenantContextHeader, testTenantID)
	rec := httptest.NewRecorder()
	RequestWrapped(h).ServeHTTP(rec, req)
	return rec
}

// One request deadline covers allowlist + claim + insert, not a fresh budget each.
func TestSessionEventSharesOneRequestDeadline(t *testing.T) {
	rec := &deadlineRecorder{}
	claimer := &deadlineClaimer{}
	h := NewHandler(staticVerifier{claims: deadlineTestClaims()}, rec, nil,
		WithPendingEmailGrantClaimer(claimer), WithRequestDeadline(2*time.Second))
	if got := serveSignIn(t, h); got.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", got.Code, got.Body.String())
	}
	if !claimer.seen || !rec.seen {
		t.Fatalf("claim/record saw no request deadline (claim=%v record=%v)", claimer.seen, rec.seen)
	}
	if !claimer.deadline.Equal(rec.deadline) {
		t.Fatalf("claim deadline %s != record deadline %s", claimer.deadline, rec.deadline)
	}
}

func TestPoolTimeoutIs503WithRetryAfter(t *testing.T) {
	cases := map[string]*Handler{
		"record timeout":  NewHandler(staticVerifier{claims: deadlineTestClaims()}, &deadlineRecorder{err: context.DeadlineExceeded}, nil),
		"record canceled": NewHandler(staticVerifier{claims: deadlineTestClaims()}, &deadlineRecorder{err: context.Canceled}, nil),
		"claim timeout": NewHandler(staticVerifier{claims: deadlineTestClaims()}, &deadlineRecorder{}, nil,
			WithPendingEmailGrantClaimer(&deadlineClaimer{err: context.DeadlineExceeded})),
		"allowlist timed out": NewHandler(staticVerifier{claims: deadlineTestClaims()}, &deadlineRecorder{}, nil,
			WithAllowedEmails([]string{"someone-else@mesha.sg"}),
			WithDynamicAllowedEmails(ctxDynamicEmails{block: true}),
			WithRequestDeadline(50*time.Millisecond)),
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			got := serveSignIn(t, h)
			if got.Code != http.StatusServiceUnavailable || got.Header().Get("Retry-After") == "" {
				t.Fatalf("status=%d retry-after=%q body=%s", got.Code, got.Header().Get("Retry-After"), got.Body.String())
			}
		})
	}
}
