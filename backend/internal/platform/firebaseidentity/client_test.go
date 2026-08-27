package firebaseidentity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/oauth2"

	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

func staticToken() oauth2.TokenSource {
	return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "test-token"})
}

type fakeToolkit struct {
	usersByEmail map[string]lookupUser
	signUps      int
	updates      []map[string]any
}

func (f *fakeToolkit) updatedUIDs() []string {
	uids := make([]string, 0, len(f.updates))
	for _, u := range f.updates {
		uids = append(uids, u["localId"].(string))
	}
	return uids
}

func (f *fakeToolkit) passwordUpdates() []string {
	passwords := []string{}
	for _, u := range f.updates {
		if pw, ok := u["password"].(string); ok {
			passwords = append(passwords, pw)
		}
	}
	return passwords
}

func passwordProvider() []struct {
	ProviderID string `json:"providerId"`
} {
	return []struct {
		ProviderID string `json:"providerId"`
	}{{ProviderID: "password"}}
}

func googleOnlyProvider() []struct {
	ProviderID string `json:"providerId"`
} {
	return []struct {
		ProviderID string `json:"providerId"`
	}{{ProviderID: "google.com"}}
}

func (f *fakeToolkit) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("missing bearer: %q", got)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch r.URL.Path {
		case "/v1/projects/goatos-test/accounts:lookup":
			emails, _ := body["email"].([]any)
			users := []lookupUser{}
			if len(emails) == 1 {
				if user, ok := f.usersByEmail[emails[0].(string)]; ok {
					users = append(users, user)
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"users": users})
		case "/v1/projects/goatos-test/accounts":
			f.signUps++
			uid := "new-uid"
			f.usersByEmail[body["email"].(string)] = lookupUser{LocalID: uid}
			_ = json.NewEncoder(w).Encode(map[string]any{"localId": uid})
		case "/v1/projects/goatos-test/accounts:update":
			f.updates = append(f.updates, body)
			_ = json.NewEncoder(w).Encode(map[string]any{"localId": body["localId"]})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
}

func newTestClient(t *testing.T, toolkit *fakeToolkit) *Client {
	server := httptest.NewServer(toolkit.handler(t))
	t.Cleanup(server.Close)
	client, err := New("goatos-test", WithBaseURL(server.URL+"/v1"), WithTokenSource(staticToken()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

func TestEnsureEmailUserCreatesAndVerifies(t *testing.T) {
	toolkit := &fakeToolkit{usersByEmail: map[string]lookupUser{}}
	client := newTestClient(t, toolkit)

	got, err := client.EnsureEmailUser(context.Background(), "amit@mesha.sg", "Amit Kumar", "Amit@2026")
	if err != nil {
		t.Fatalf("EnsureEmailUser: %v", err)
	}
	if got.UID != "new-uid" || got.Existed {
		t.Fatalf("got %+v, want fresh new-uid", got)
	}
	if toolkit.signUps != 1 {
		t.Fatalf("signUps = %d", toolkit.signUps)
	}
	// emailVerified must be forced true — the allowlist rejects unverified emails.
	if uids := toolkit.updatedUIDs(); len(uids) != 1 || uids[0] != "new-uid" {
		t.Fatalf("expected one emailVerified update for new-uid, got %v", toolkit.updates)
	}
	if got.PasswordSet {
		t.Fatalf("a fresh account carries its password by construction; PasswordSet must be false")
	}
}

func TestEnsureEmailUserFindsExistingWithoutSignUp(t *testing.T) {
	toolkit := &fakeToolkit{usersByEmail: map[string]lookupUser{
		"amit@mesha.sg": {LocalID: "existing-uid", EmailVerified: true, ProviderUserInfo: passwordProvider()},
	}}
	client := newTestClient(t, toolkit)

	got, err := client.EnsureEmailUser(context.Background(), "Amit@mesha.sg", "Amit", "ignored")
	if err != nil {
		t.Fatalf("EnsureEmailUser: %v", err)
	}
	if got.UID != "existing-uid" || !got.Existed || got.PasswordSet {
		t.Fatalf("got %+v, want existing-uid/existed with its own password untouched", got)
	}
	if toolkit.signUps != 0 {
		t.Fatalf("an existing account must never be re-created (signUps=%d)", toolkit.signUps)
	}
	if len(toolkit.updates) != 0 {
		t.Fatalf("an already-verified account with its own password needs no update, got %v", toolkit.updates)
	}
}

func TestEnsureEmailUserRepairsUnverifiedExisting(t *testing.T) {
	toolkit := &fakeToolkit{usersByEmail: map[string]lookupUser{
		"amit@mesha.sg": {LocalID: "existing-uid", EmailVerified: false, ProviderUserInfo: passwordProvider()},
	}}
	client := newTestClient(t, toolkit)

	if _, err := client.EnsureEmailUser(context.Background(), "amit@mesha.sg", "Amit", "ignored"); err != nil {
		t.Fatalf("EnsureEmailUser: %v", err)
	}
	if uids := toolkit.updatedUIDs(); len(uids) != 1 || uids[0] != "existing-uid" {
		t.Fatalf("expected emailVerified repair, got %v", toolkit.updates)
	}
	if pws := toolkit.passwordUpdates(); len(pws) != 0 {
		t.Fatalf("an account with its own password must never have it touched, got %v", pws)
	}
}

// The STG 2026-08-27 onboarding defect: the person's Gmail had signed in via
// Google BEFORE Add Person ran, so the account existed with NO password
// provider and the promised convention password never came to exist. The
// existing-account branch must ADD the password there (and only there).
func TestEnsureEmailUserAddsPasswordToSSOOnlyExisting(t *testing.T) {
	toolkit := &fakeToolkit{usersByEmail: map[string]lookupUser{
		"bipin@gmail.com": {LocalID: "sso-uid", EmailVerified: true, ProviderUserInfo: googleOnlyProvider()},
	}}
	client := newTestClient(t, toolkit)

	got, err := client.EnsureEmailUser(context.Background(), "bipin@gmail.com", "Bipin Yadav", "Bipin@2026")
	if err != nil {
		t.Fatalf("EnsureEmailUser: %v", err)
	}
	if got.UID != "sso-uid" || !got.Existed || !got.PasswordSet {
		t.Fatalf("got %+v, want existed sso-uid with PasswordSet", got)
	}
	if toolkit.signUps != 0 {
		t.Fatalf("must not re-create the SSO account (signUps=%d)", toolkit.signUps)
	}
	if pws := toolkit.passwordUpdates(); len(pws) != 1 || pws[0] != "Bipin@2026" {
		t.Fatalf("expected the convention password to be installed, got %v", toolkit.updates)
	}
}

// A password-less EnsureEmailUser call (defensive) must not install an empty
// password on an SSO-only account.
func TestEnsureEmailUserSkipsEmptyPasswordOnSSOOnlyExisting(t *testing.T) {
	toolkit := &fakeToolkit{usersByEmail: map[string]lookupUser{
		"bipin@gmail.com": {LocalID: "sso-uid", EmailVerified: true, ProviderUserInfo: googleOnlyProvider()},
	}}
	client := newTestClient(t, toolkit)

	got, err := client.EnsureEmailUser(context.Background(), "bipin@gmail.com", "Bipin Yadav", "")
	if err != nil {
		t.Fatalf("EnsureEmailUser: %v", err)
	}
	if got.PasswordSet || len(toolkit.updates) != 0 {
		t.Fatalf("empty password must never be installed, got %+v / %v", got, toolkit.updates)
	}
}

func TestEnsureEmailUserWrapsAPIErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"PERMISSION_DENIED"}}`))
	}))
	t.Cleanup(server.Close)
	client, err := New("goatos-test", WithBaseURL(server.URL+"/v1"), WithTokenSource(staticToken()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = client.EnsureEmailUser(context.Background(), "amit@mesha.sg", "Amit", "pw")
	if !errors.Is(err, ports.ErrIdentityUnavailable) {
		t.Fatalf("want ErrIdentityUnavailable, got %v", err)
	}
}

func TestProjectIDFromIssuer(t *testing.T) {
	cases := map[string]string{
		"https://securetoken.google.com/goatos-stg":  "goatos-stg",
		"https://securetoken.google.com/goatos-stg/": "goatos-stg",
		"https://goatos.local/dev":                   "",
		"":                                           "",
		"https://securetoken.google.com/":            "",
	}
	for in, want := range cases {
		if got := ProjectIDFromIssuer(in); got != want {
			t.Fatalf("ProjectIDFromIssuer(%q) = %q, want %q", in, got, want)
		}
	}
}
