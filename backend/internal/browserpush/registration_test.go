package browserpush

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeRepo struct {
	upserted      []RegisterRequest
	upsertTenant  string
	upsertMember  string
	upsertNow     time.Time
	created       bool
	upsertErr     error
	unsubscribed  bool
	unsubCalls    int
	unsubInstall  string
	staleToken    string
	staleReason   string
	staleCount    int
	resolveResult []Recipient
}

func (f *fakeRepo) Upsert(_ context.Context, tenantID, memberOrUserID string, in RegisterRequest, now time.Time) (Registration, bool, error) {
	f.upserted = append(f.upserted, in)
	f.upsertTenant = tenantID
	f.upsertMember = memberOrUserID
	f.upsertNow = now
	if f.upsertErr != nil {
		return Registration{}, false, f.upsertErr
	}
	return Registration{
		BrowserRegistrationID: "reg-1",
		WorkforceMemberID:     memberOrUserID,
		Provider:              ProviderWebFCM,
		BrowserInstallID:      in.BrowserInstallID,
		BrowserLabel:          in.BrowserLabel,
		Status:                StatusActive,
		RowVersion:            1,
	}, f.created, nil
}

func (f *fakeRepo) MarkUnsubscribed(_ context.Context, _, _, browserInstallID string, _ time.Time) (bool, error) {
	f.unsubCalls++
	f.unsubInstall = browserInstallID
	return f.unsubscribed, nil
}

func (f *fakeRepo) ListForMember(_ context.Context, _, _ string) ([]Registration, error) {
	return nil, nil
}

func (f *fakeRepo) ResolveMemberRecipients(_ context.Context, _, _ string) ([]Recipient, error) {
	return f.resolveResult, nil
}

func (f *fakeRepo) MarkTokenStale(_ context.Context, _, token, reason string, _ time.Time) (int, error) {
	f.staleToken = token
	f.staleReason = reason
	return f.staleCount, nil
}

func newService(repo *fakeRepo) *Service {
	fixed := time.Date(2026, 9, 18, 11, 40, 0, 0, time.UTC)
	return NewService(repo).WithClock(func() time.Time { return fixed })
}

// The BODY never names the person. A browser that could register a push address against somebody
// else's name would be able to read that person's notifications.
func TestIdentityComesFromTheSessionAndNeverFromTheBody(t *testing.T) {
	repo := &fakeRepo{}
	service := newService(repo)
	_, err := service.Register(context.Background(), RegisterCommand{
		TenantID: "tenant-1",
		ActorID:  "user-1",
		Body:     RegisterRequest{BrowserInstallID: "web-1", Token: "token-1"},
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if repo.upsertTenant != "tenant-1" || repo.upsertMember != "user-1" {
		t.Fatalf("want the session identity, got tenant=%q member=%q", repo.upsertTenant, repo.upsertMember)
	}
}

func TestAnUnauthenticatedCallerIsRefused(t *testing.T) {
	service := newService(&fakeRepo{})
	for _, cmd := range []RegisterCommand{
		{TenantID: "", ActorID: "user-1", Body: RegisterRequest{BrowserInstallID: "web-1", Token: "t"}},
		{TenantID: "tenant-1", ActorID: "", Body: RegisterRequest{BrowserInstallID: "web-1", Token: "t"}},
	} {
		if _, err := service.Register(context.Background(), cmd); err == nil {
			t.Fatalf("want a refusal for %+v", cmd)
		}
	}
}

func TestAMissingInstallIdOrTokenIsRefused(t *testing.T) {
	service := newService(&fakeRepo{})
	cases := map[string]RegisterRequest{
		"no install id": {BrowserInstallID: "  ", Token: "token-1"},
		"no token":      {BrowserInstallID: "web-1", Token: "   "},
	}
	for name, body := range cases {
		if _, err := service.Register(context.Background(), RegisterCommand{TenantID: "t", ActorID: "u", Body: body}); err == nil {
			t.Fatalf("%s: want a refusal", name)
		}
	}
}

// A push token is a bearer credential. Control characters in one are never legitimate and are
// exactly what a log-injection attempt looks like, so it is REFUSED rather than silently stripped
// -- stripping would store a token that can never resolve and report success.
func TestATokenWithControlCharactersIsRefusedRatherThanCleaned(t *testing.T) {
	repo := &fakeRepo{}
	service := newService(repo)
	_, err := service.Register(context.Background(), RegisterCommand{
		TenantID: "t",
		ActorID:  "u",
		Body:     RegisterRequest{BrowserInstallID: "web-1", Token: "token\nInjected: yes"},
	})
	if err == nil {
		t.Fatal("want a refusal")
	}
	if len(repo.upserted) != 0 {
		t.Fatalf("nothing may be stored, got %+v", repo.upserted)
	}
}

func TestAnOversizedFieldIsRefusedAndALongLabelIsTruncated(t *testing.T) {
	repo := &fakeRepo{}
	service := newService(repo)

	if _, err := service.Register(context.Background(), RegisterCommand{
		TenantID: "t", ActorID: "u",
		Body: RegisterRequest{BrowserInstallID: strings.Repeat("x", maxBrowserInstallIDLen+1), Token: "token-1"},
	}); err == nil {
		t.Fatal("want an oversized install id refused")
	}
	if _, err := service.Register(context.Background(), RegisterCommand{
		TenantID: "t", ActorID: "u",
		Body: RegisterRequest{BrowserInstallID: "web-1", Token: strings.Repeat("x", maxTokenLen+1)},
	}); err == nil {
		t.Fatal("want an oversized token refused")
	}

	// The label and the user agent are DISPLAY ONLY, so an over-long one is trimmed rather than
	// refused: failing a registration over a cosmetic field would cost the person their
	// notifications for nothing.
	if _, err := service.Register(context.Background(), RegisterCommand{
		TenantID: "t", ActorID: "u",
		Body: RegisterRequest{
			BrowserInstallID: "web-1",
			Token:            "token-1",
			BrowserLabel:     strings.Repeat("l", maxBrowserLabelLen+50),
			UserAgent:        strings.Repeat("a", maxUserAgentLen+50),
		},
	}); err != nil {
		t.Fatalf("want a long label accepted and trimmed, got %v", err)
	}
	last := repo.upserted[len(repo.upserted)-1]
	if len(last.BrowserLabel) != maxBrowserLabelLen {
		t.Fatalf("want the label trimmed to %d, got %d", maxBrowserLabelLen, len(last.BrowserLabel))
	}
	if len(last.UserAgent) != maxUserAgentLen {
		t.Fatalf("want the user agent trimmed to %d, got %d", maxUserAgentLen, len(last.UserAgent))
	}
}

// Unregister is idempotent by design: the client calls it on a permission revocation it may
// already have reported, and on every load where permission reads 'denied'.
func TestUnregisterIsIdempotentAndNotAnError(t *testing.T) {
	repo := &fakeRepo{unsubscribed: false}
	service := newService(repo)
	result, err := service.Unregister(context.Background(), UnregisterCommand{
		TenantID: "t", ActorID: "u", Body: UnregisterRequest{BrowserInstallID: "web-1"},
	})
	if err != nil {
		t.Fatalf("unregister: %v", err)
	}
	if result.Removed {
		t.Fatal("nothing was active, so removed must be false")
	}
	if repo.unsubInstall != "web-1" {
		t.Fatalf("want the browser named, got %q", repo.unsubInstall)
	}
}

func TestUnregisterWithoutABrowserIsRefused(t *testing.T) {
	repo := &fakeRepo{}
	service := newService(repo)
	if _, err := service.Unregister(context.Background(), UnregisterCommand{
		TenantID: "t", ActorID: "u", Body: UnregisterRequest{BrowserInstallID: "   "},
	}); err == nil {
		t.Fatal("want a refusal: unregistering 'some browser' is not a thing")
	}
	if repo.unsubCalls != 0 {
		t.Fatalf("nothing may be written, got %d calls", repo.unsubCalls)
	}
}

// THE PRUNE. Without it the table only ever grows dead rows and every future push to that person
// burns the full retry schedule against an address that can never resolve.
func TestPruneRetiresTheTokenAndCarriesTheProvidersReason(t *testing.T) {
	repo := &fakeRepo{staleCount: 2}
	service := newService(repo)
	retired, err := service.PruneToken(context.Background(), "tenant-1", "dead-token", "UNREGISTERED")
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if retired != 2 {
		t.Fatalf("want 2 retired, got %d", retired)
	}
	if repo.staleToken != "dead-token" {
		t.Fatalf("want the token addressed, got %q", repo.staleToken)
	}
	if repo.staleReason != "UNREGISTERED" {
		t.Fatalf("want the provider's own reason recorded, got %q", repo.staleReason)
	}
}

// A blank reason still records SOMETHING: 'stale' with no reason leaves a support question with
// no answer, and the table's own CHECK requires the pairing.
func TestPruneWithNoReasonStillRecordsOne(t *testing.T) {
	repo := &fakeRepo{staleCount: 1}
	service := newService(repo)
	if _, err := service.PruneToken(context.Background(), "tenant-1", "dead-token", "   "); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if strings.TrimSpace(repo.staleReason) == "" {
		t.Fatal("want a default reason recorded")
	}
}

// A prune with nothing to address is a no-op, not a tenant-wide UPDATE. A blank token reaching the
// repository would match every row whose token is blank.
func TestPruneWithNoTokenTouchesNothing(t *testing.T) {
	repo := &fakeRepo{staleCount: 99}
	service := newService(repo)
	for _, args := range [][2]string{{"tenant-1", "  "}, {"", "token"}} {
		retired, err := service.PruneToken(context.Background(), args[0], args[1], "UNREGISTERED")
		if err != nil {
			t.Fatalf("prune: %v", err)
		}
		if retired != 0 {
			t.Fatalf("want a no-op, got %d retired", retired)
		}
	}
	if repo.staleToken != "" {
		t.Fatalf("the repository must not be called, got token %q", repo.staleToken)
	}
}

func TestResolveBrowserRecipientsSkipsAnEmptyLookup(t *testing.T) {
	repo := &fakeRepo{resolveResult: []Recipient{{WorkforceMemberID: "m", BrowserRegistrationID: "r", Token: "t"}}}
	service := newService(repo)
	got, err := service.ResolveBrowserRecipients(context.Background(), "", "member-1")
	if err != nil || got != nil {
		t.Fatalf("want a nil answer for a blank tenant, got %+v (%v)", got, err)
	}
	got, err = service.ResolveBrowserRecipients(context.Background(), "tenant-1", "member-1")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want the recipient, got %+v", got)
	}
}

func TestARepositoryFailureSurfaces(t *testing.T) {
	repo := &fakeRepo{upsertErr: errors.New("boom")}
	service := newService(repo)
	if _, err := service.Register(context.Background(), RegisterCommand{
		TenantID: "t", ActorID: "u", Body: RegisterRequest{BrowserInstallID: "web-1", Token: "token-1"},
	}); err == nil {
		t.Fatal("want the repository error to surface")
	}
}
