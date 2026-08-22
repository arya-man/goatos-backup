// Package firebaseidentity is the Identity Toolkit v1 adapter behind the
// workforce IdentityProvider port. It creates/looks up Firebase Auth
// email/password users server-side with OAuth2 application-default credentials
// (the same credential pattern the FCM push gateway uses), so the in-app
// "Add Person" flow can mint a working login without any console or CLI step.
//
// Idempotency: EnsureEmailUser is lookup-by-email first, create only when
// absent. A retry after a partial failure (user created, DB tx failed) finds
// the existing account and returns the same UID.
//
// emailVerified is forced true on both paths: the auth middleware's email
// allowlist (authallow.EmailSet.Allows) rejects unverified emails, so an
// unverified account can never log in.
package firebaseidentity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

const (
	defaultBaseURL = "https://identitytoolkit.googleapis.com/v1"
	scopeIdentity  = "https://www.googleapis.com/auth/identitytoolkit"
	requestTimeout = 15 * time.Second
)

// SecureTokenIssuerPrefix is the Firebase issuer shape the project id can be
// derived from when no explicit project id is configured.
const secureTokenIssuerPrefix = "https://securetoken.google.com/"

// ProjectIDFromIssuer derives the Firebase project id from a
// securetoken.google.com issuer, or "" when the issuer is not Firebase-shaped
// (e.g. the local HS256 dev issuer).
func ProjectIDFromIssuer(issuer string) string {
	issuer = strings.TrimSpace(issuer)
	if !strings.HasPrefix(issuer, secureTokenIssuerPrefix) {
		return ""
	}
	project := strings.TrimSuffix(strings.TrimPrefix(issuer, secureTokenIssuerPrefix), "/")
	if project == "" || strings.Contains(project, "/") {
		return ""
	}
	return project
}

type Client struct {
	projectID  string
	baseURL    string
	httpClient *http.Client

	mu          sync.Mutex
	tokenSource oauth2.TokenSource
}

// Option customizes the client (tests inject a fake HTTP server + static token).
type Option func(*Client)

func WithBaseURL(url string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(url, "/") }
}

func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.httpClient = h }
}

func WithTokenSource(ts oauth2.TokenSource) Option {
	return func(c *Client) { c.tokenSource = ts }
}

// New returns a client for the given Firebase project. projectID must be
// non-empty; callers derive it from config or ProjectIDFromIssuer and skip
// constructing the adapter entirely when neither yields one (local dev).
func New(projectID string, opts ...Option) (*Client, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, fmt.Errorf("firebaseidentity: project id is required")
	}
	c := &Client{
		projectID:  projectID,
		baseURL:    defaultBaseURL,
		httpClient: &http.Client{Timeout: requestTimeout},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

var _ ports.IdentityProvider = (*Client)(nil)

// EnsureEmailUser implements ports.IdentityProvider.
func (c *Client) EnsureEmailUser(ctx context.Context, email, displayName, password string) (ports.EnsuredUser, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return ports.EnsuredUser{}, fmt.Errorf("firebaseidentity: email is required")
	}

	existing, err := c.lookupByEmail(ctx, email)
	if err != nil {
		return ports.EnsuredUser{}, err
	}
	if existing != nil {
		// Existing account: never reset its password, only repair the
		// email-verified flag if a manual creation left it false.
		if !existing.EmailVerified {
			if err := c.setEmailVerified(ctx, existing.LocalID); err != nil {
				return ports.EnsuredUser{}, err
			}
		}
		return ports.EnsuredUser{UID: existing.LocalID, Existed: true}, nil
	}

	uid, err := c.signUp(ctx, email, displayName, password)
	if err != nil {
		return ports.EnsuredUser{}, err
	}
	if err := c.setEmailVerified(ctx, uid); err != nil {
		return ports.EnsuredUser{}, err
	}
	return ports.EnsuredUser{UID: uid, Existed: false}, nil
}

type lookupUser struct {
	LocalID       string `json:"localId"`
	EmailVerified bool   `json:"emailVerified"`
}

func (c *Client) lookupByEmail(ctx context.Context, email string) (*lookupUser, error) {
	var resp struct {
		Users []lookupUser `json:"users"`
	}
	err := c.post(ctx, "/accounts:lookup", map[string]any{"email": []string{email}}, &resp)
	if err != nil {
		return nil, err
	}
	if len(resp.Users) == 0 {
		return nil, nil
	}
	return &resp.Users[0], nil
}

func (c *Client) signUp(ctx context.Context, email, displayName, password string) (string, error) {
	var resp struct {
		LocalID string `json:"localId"`
	}
	err := c.post(ctx, "/accounts", map[string]any{
		"email":       email,
		"password":    password,
		"displayName": displayName,
	}, &resp)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(resp.LocalID) == "" {
		return "", fmt.Errorf("%w: sign-up returned no user id", ports.ErrIdentityUnavailable)
	}
	return resp.LocalID, nil
}

func (c *Client) setEmailVerified(ctx context.Context, uid string) error {
	var resp struct {
		LocalID string `json:"localId"`
	}
	return c.post(ctx, "/accounts:update", map[string]any{
		"localId":       uid,
		"emailVerified": true,
	}, &resp)
}

// post issues one authenticated project-scoped Identity Toolkit call. Every
// failure wraps ports.ErrIdentityUnavailable so the service can present one
// stable "account service unavailable" outcome without leaking transport
// detail into user-facing copy.
func (c *Client) post(ctx context.Context, path string, body, out any) error {
	bearer, err := c.bearer(ctx)
	if err != nil {
		return fmt.Errorf("%w: %v", ports.ErrIdentityUnavailable, err)
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("%w: encode request: %v", ports.ErrIdentityUnavailable, err)
	}
	url := fmt.Sprintf("%s/projects/%s%s", c.baseURL, c.projectID, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("%w: build request: %v", ports.ErrIdentityUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ports.ErrIdentityUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("%w: read response: %v", ports.ErrIdentityUnavailable, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: identity toolkit %s returned %d: %s",
			ports.ErrIdentityUnavailable, path, resp.StatusCode, identityErrorMessage(raw))
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("%w: decode response: %v", ports.ErrIdentityUnavailable, err)
		}
	}
	return nil
}

// identityErrorMessage extracts the API's error message without echoing the
// full body (which can be large) into logs.
func identityErrorMessage(raw []byte) string {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil && envelope.Error.Message != "" {
		return envelope.Error.Message
	}
	if len(raw) > 200 {
		raw = raw[:200]
	}
	return string(raw)
}

func (c *Client) bearer(ctx context.Context) (string, error) {
	c.mu.Lock()
	source := c.tokenSource
	if source == nil {
		// Credential discovery talks to the metadata server / ADC over the
		// network; a failure here is transient and retryable.
		var err error
		source, err = google.DefaultTokenSource(ctx, scopeIdentity)
		if err != nil {
			c.mu.Unlock()
			return "", fmt.Errorf("resolve identity credentials: %w", err)
		}
		c.tokenSource = source
	}
	c.mu.Unlock()
	token, err := source.Token()
	if err != nil {
		return "", fmt.Errorf("fetch identity access token: %w", err)
	}
	if token == nil || strings.TrimSpace(token.AccessToken) == "" {
		return "", fmt.Errorf("identity credentials returned an empty access token")
	}
	return token.AccessToken, nil
}
