package auth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	AlgorithmRS256 = "RS256"
	AlgorithmES256 = "ES256"

	defaultClockSkew        = 60 * time.Second
	defaultJWKSCacheTTL     = 5 * time.Minute
	defaultJWKSFetchTimeout = 10 * time.Second
	minJWKSRefreshInterval  = 30 * time.Second
)

// JWKSConfig holds the configuration for JWKSVerifier.
type JWKSConfig struct {
	// JWKSURL is the URL to fetch the JWKS from. Required.
	JWKSURL string
	// Issuer must match the iss claim exactly. Required.
	Issuer string
	// Audience must appear in the aud claim. Required.
	Audience string
	// AllowedAlgs is the set of accepted asymmetric algorithms. Defaults to
	// {RS256, ES256}. HS* and alg=none are always rejected regardless of this list.
	AllowedAlgs []string
	// ClockSkew is added to exp and subtracted from nbf checks. Defaults to 60s.
	ClockSkew time.Duration
	// MaxTTL is the maximum allowed token lifetime (exp - now). Zero means no ceiling.
	MaxTTL time.Duration
	// CacheTTL controls how long fetched JWKS keys are cached. Defaults to 5m.
	CacheTTL time.Duration
	// HTTPClient is used to fetch JWKS. Defaults to an http.Client with a
	// bounded timeout; inject a test client via httptest.
	HTTPClient *http.Client
	// Now is the clock function. Defaults to time.Now.
	Now func() time.Time
}

// JWKSVerifier verifies RS256 and ES256 bearer tokens against a remote JWKS
// endpoint. It implements the same TokenVerifier interface as HS256Verifier.
type JWKSVerifier struct {
	jwksURL     string
	issuer      string
	audience    string
	allowedAlgs map[string]struct{}
	clockSkew   time.Duration
	maxTTL      time.Duration
	cacheTTL    time.Duration
	client      *http.Client
	now         func() time.Time

	mu             sync.RWMutex
	cachedKeys     map[string]parsedJWK // kid → key
	cacheExpiresAt time.Time
	lastRefresh    time.Time
}

// parsedJWK holds a parsed public key together with its declared algorithm so
// we can cross-check the JWT header alg against the JWK alg/kty.
type parsedJWK struct {
	alg string
	key any // *rsa.PublicKey or *ecdsa.PublicKey
}

// NewJWKSVerifier constructs a JWKSVerifier from cfg. It returns an error if
// any required field is missing or any configured value is out of range.
func NewJWKSVerifier(cfg JWKSConfig) (*JWKSVerifier, error) {
	cfg.JWKSURL = strings.TrimSpace(cfg.JWKSURL)
	cfg.Issuer = strings.TrimSpace(cfg.Issuer)
	cfg.Audience = strings.TrimSpace(cfg.Audience)
	if cfg.JWKSURL == "" {
		return nil, fmt.Errorf("%w: jwks_url is required", ErrInvalidToken)
	}
	if cfg.Issuer == "" || cfg.Audience == "" {
		return nil, fmt.Errorf("%w: missing issuer or audience", ErrInvalidToken)
	}

	allowedAlgs := map[string]struct{}{}
	if len(cfg.AllowedAlgs) == 0 {
		allowedAlgs[AlgorithmRS256] = struct{}{}
		allowedAlgs[AlgorithmES256] = struct{}{}
	} else {
		for _, a := range cfg.AllowedAlgs {
			a = strings.TrimSpace(a)
			if isSymmetricOrNoneAlg(a) {
				return nil, fmt.Errorf("%w: disallowed algorithm in allow-list: %s", ErrInvalidToken, a)
			}
			allowedAlgs[a] = struct{}{}
		}
	}

	clockSkew := cfg.ClockSkew
	if clockSkew == 0 {
		clockSkew = defaultClockSkew
	}
	if clockSkew < 0 {
		return nil, fmt.Errorf("%w: clock_skew must not be negative", ErrInvalidToken)
	}

	if cfg.MaxTTL < 0 {
		return nil, ErrInvalidMaxTTL
	}

	cacheTTL := cfg.CacheTTL
	if cacheTTL == 0 {
		cacheTTL = defaultJWKSCacheTTL
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultJWKSFetchTimeout}
	}

	now := cfg.Now
	if now == nil {
		now = time.Now
	}

	return &JWKSVerifier{
		jwksURL:     cfg.JWKSURL,
		issuer:      cfg.Issuer,
		audience:    cfg.Audience,
		allowedAlgs: allowedAlgs,
		clockSkew:   clockSkew,
		maxTTL:      cfg.MaxTTL,
		cacheTTL:    cacheTTL,
		client:      client,
		now:         now,
		cachedKeys:  map[string]parsedJWK{},
	}, nil
}

// Verify parses and validates a JWT, returning the validated Claims on success.
// It implements the httpmiddleware.TokenVerifier interface.
func (v *JWKSVerifier) Verify(token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return Claims{}, ErrInvalidToken
	}

	// Decode and validate the JOSE header.
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := decodeSegment(parts[0], &header, false); err != nil {
		return Claims{}, ErrInvalidToken
	}

	// Reject symmetric and alg=none unconditionally.
	if isSymmetricOrNoneAlg(header.Alg) {
		return Claims{}, ErrInvalidToken
	}
	// Reject algorithms not in the allow-list.
	if _, ok := v.allowedAlgs[header.Alg]; !ok {
		return Claims{}, ErrInvalidToken
	}
	// kid is required.
	if strings.TrimSpace(header.Kid) == "" {
		return Claims{}, ErrInvalidToken
	}

	// Look up the key; allow one throttled refresh if the kid is unknown.
	key, err := v.resolveKey(header.Kid, header.Alg)
	if err != nil {
		return Claims{}, ErrInvalidToken
	}

	// Verify the signature over header.payload.
	sigInput := parts[0] + "." + parts[1]
	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	if err := verifySignature(header.Alg, key, []byte(sigInput), sigBytes); err != nil {
		return Claims{}, ErrInvalidToken
	}

	// Decode and validate the claims payload.
	var payload rawClaims
	if err := decodeSegment(parts[1], &payload, false); err != nil {
		return Claims{}, ErrInvalidToken
	}
	claims, err := payload.validateWithSkew(v.issuer, v.audience, v.now(), v.maxTTL, v.clockSkew)
	if err != nil {
		return Claims{}, err
	}
	return claims, nil
}

// resolveKey returns the parsed public key for kid, refreshing JWKS if needed.
func (v *JWKSVerifier) resolveKey(kid, alg string) (any, error) {
	// Fast path: key is already cached and cache is fresh.
	v.mu.RLock()
	pk, ok := v.cachedKeys[kid]
	fresh := v.now().Before(v.cacheExpiresAt)
	v.mu.RUnlock()

	if ok && fresh {
		return v.checkKeyAlg(pk, alg)
	}

	// Slow path: attempt one refresh if we haven't refreshed too recently.
	if err := v.refreshIfAllowed(); err != nil {
		return nil, err
	}

	v.mu.RLock()
	pk, ok = v.cachedKeys[kid]
	v.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("jwks: unknown kid %q", kid)
	}
	return v.checkKeyAlg(pk, alg)
}

// checkKeyAlg ensures the JWT header alg is consistent with the JWK's declared alg.
func (v *JWKSVerifier) checkKeyAlg(pk parsedJWK, headerAlg string) (any, error) {
	if pk.alg != "" && pk.alg != headerAlg {
		return nil, fmt.Errorf("jwks: key alg %q does not match header alg %q", pk.alg, headerAlg)
	}
	return pk.key, nil
}

// refreshIfAllowed fetches the JWKS only if the minimum refresh interval has elapsed.
func (v *JWKSVerifier) refreshIfAllowed() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	// Double-check under the write lock: another goroutine may have refreshed already.
	if v.now().Before(v.lastRefresh.Add(minJWKSRefreshInterval)) {
		return nil
	}
	return v.fetchAndCacheLocked()
}

// fetchAndCacheLocked fetches the JWKS and updates the in-memory cache.
// Caller must hold v.mu (write lock).
func (v *JWKSVerifier) fetchAndCacheLocked() error {
	keys, err := v.fetchJWKS()
	if err != nil {
		return fmt.Errorf("jwks: fetch failed: %w", err)
	}
	v.cachedKeys = keys
	v.lastRefresh = v.now()
	v.cacheExpiresAt = v.now().Add(v.cacheTTL)
	return nil
}

// fetchJWKS retrieves and parses the JWKS from the configured URL.
func (v *JWKSVerifier) fetchJWKS() (map[string]parsedJWK, error) {
	resp, err := v.client.Get(v.jwksURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jwks: unexpected HTTP status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB limit
	if err != nil {
		return nil, err
	}

	var jwks struct {
		Keys []rawJWK `json:"keys"`
	}
	if err := json.Unmarshal(body, &jwks); err != nil {
		return nil, fmt.Errorf("jwks: parse error: %w", err)
	}

	parsed := make(map[string]parsedJWK, len(jwks.Keys))
	for _, k := range jwks.Keys {
		pk, err := parseJWK(k)
		if err != nil {
			// Skip keys we cannot parse (unknown kty, missing fields); don't
			// fail the whole fetch.
			continue
		}
		if k.Kid != "" {
			parsed[k.Kid] = pk
		}
	}
	return parsed, nil
}

// rawJWK is the wire representation of a single JSON Web Key.
type rawJWK struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	// RSA fields
	N string `json:"n"`
	E string `json:"e"`
	// EC fields
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

// parseJWK converts a rawJWK into a parsedJWK holding a typed public key.
func parseJWK(k rawJWK) (parsedJWK, error) {
	switch strings.ToUpper(k.Kty) {
	case "RSA":
		pub, err := parseRSAPublicKey(k)
		if err != nil {
			return parsedJWK{}, err
		}
		// Derive alg: use declared alg if present, otherwise infer RS256 for RSA.
		alg := k.Alg
		if alg == "" {
			alg = AlgorithmRS256
		}
		if isSymmetricOrNoneAlg(alg) || !strings.HasPrefix(alg, "RS") {
			return parsedJWK{}, fmt.Errorf("jwks: RSA key with unexpected alg %q", alg)
		}
		return parsedJWK{alg: alg, key: pub}, nil
	case "EC":
		pub, alg, err := parseECPublicKey(k)
		if err != nil {
			return parsedJWK{}, err
		}
		declaredAlg := k.Alg
		if declaredAlg == "" {
			declaredAlg = alg
		}
		if declaredAlg != alg {
			return parsedJWK{}, fmt.Errorf("jwks: EC key alg %q inconsistent with crv %q", declaredAlg, k.Crv)
		}
		return parsedJWK{alg: declaredAlg, key: pub}, nil
	default:
		return parsedJWK{}, fmt.Errorf("jwks: unsupported kty %q", k.Kty)
	}
}

// parseRSAPublicKey builds an *rsa.PublicKey from the base64url-encoded n and e fields.
func parseRSAPublicKey(k rawJWK) (*rsa.PublicKey, error) {
	if k.N == "" || k.E == "" {
		return nil, fmt.Errorf("jwks: RSA key missing n or e")
	}
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("jwks: RSA n decode: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("jwks: RSA e decode: %w", err)
	}
	n := new(big.Int).SetBytes(nBytes)
	e := int(new(big.Int).SetBytes(eBytes).Int64())
	if e <= 0 {
		return nil, fmt.Errorf("jwks: RSA e is not positive")
	}
	pub := &rsa.PublicKey{N: n, E: e}
	if pub.N.BitLen() < 2048 {
		return nil, fmt.Errorf("jwks: RSA key too short (%d bits)", pub.N.BitLen())
	}
	return pub, nil
}

// parseECPublicKey builds an *ecdsa.PublicKey from the JWK. Only P-256 (ES256)
// is supported.
func parseECPublicKey(k rawJWK) (*ecdsa.PublicKey, string, error) {
	if k.Crv != "P-256" {
		return nil, "", fmt.Errorf("jwks: unsupported EC curve %q (only P-256/ES256 supported)", k.Crv)
	}
	if k.X == "" || k.Y == "" {
		return nil, "", fmt.Errorf("jwks: EC key missing x or y")
	}
	xBytes, err := base64.RawURLEncoding.DecodeString(k.X)
	if err != nil {
		return nil, "", fmt.Errorf("jwks: EC x decode: %w", err)
	}
	yBytes, err := base64.RawURLEncoding.DecodeString(k.Y)
	if err != nil {
		return nil, "", fmt.Errorf("jwks: EC y decode: %w", err)
	}
	curve := elliptic.P256()
	x := new(big.Int).SetBytes(xBytes)
	y := new(big.Int).SetBytes(yBytes)
	if !curve.IsOnCurve(x, y) {
		return nil, "", fmt.Errorf("jwks: EC point not on P-256 curve")
	}
	pub := &ecdsa.PublicKey{Curve: curve, X: x, Y: y}
	return pub, AlgorithmES256, nil
}

// verifySignature dispatches to the appropriate signature verification routine
// based on the algorithm.
func verifySignature(alg string, key any, sigInput, sig []byte) error {
	digest := sha256.Sum256(sigInput)
	switch alg {
	case AlgorithmRS256:
		rsaKey, ok := key.(*rsa.PublicKey)
		if !ok {
			return fmt.Errorf("jwks: expected *rsa.PublicKey for RS256")
		}
		return rsa.VerifyPKCS1v15(rsaKey, crypto.SHA256, digest[:], sig)
	case AlgorithmES256:
		ecKey, ok := key.(*ecdsa.PublicKey)
		if !ok {
			return fmt.Errorf("jwks: expected *ecdsa.PublicKey for ES256")
		}
		// P-256 signature is a fixed 64-byte r||s encoding.
		if len(sig) != 64 {
			return fmt.Errorf("jwks: ES256 signature must be 64 bytes, got %d", len(sig))
		}
		r := new(big.Int).SetBytes(sig[:32])
		s := new(big.Int).SetBytes(sig[32:])
		if !ecdsa.Verify(ecKey, digest[:], r, s) {
			return fmt.Errorf("jwks: ES256 signature verification failed")
		}
		return nil
	default:
		return fmt.Errorf("jwks: unsupported algorithm %q", alg)
	}
}

// isSymmetricOrNoneAlg reports whether alg is HS*, alg=none, or blank — all
// of which are unconditionally rejected by JWKSVerifier.
func isSymmetricOrNoneAlg(alg string) bool {
	alg = strings.TrimSpace(alg)
	return alg == "" || strings.EqualFold(alg, "none") || strings.HasPrefix(strings.ToUpper(alg), "HS")
}
