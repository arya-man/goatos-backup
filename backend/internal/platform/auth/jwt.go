package auth

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	AlgorithmHS256     = "HS256"
	DefaultMaxTokenTTL = 24 * time.Hour
	minSecretBytes     = 32
)

var (
	ErrInvalidToken  = errors.New("invalid token")
	ErrInvalidMaxTTL = errors.New("auth max token ttl must be positive")
	ErrWeakSecret    = errors.New("auth secret must be at least 32 bytes")
)

type Config struct {
	Issuer   string
	Audience string
	Secret   []byte
	MaxTTL   time.Duration
	Now      func() time.Time
}

type Claims struct {
	Subject         string
	ExternalSubject string
	TenantID        string
	Issuer          string
	Audience        string
	Expires         time.Time
	NotBefore       time.Time
}

func MintHS256Token(cfg Config, subject, tenantID string, ttl time.Duration) (string, error) {
	verifier, err := NewHS256Verifier(cfg)
	if err != nil {
		return "", err
	}
	subject = strings.TrimSpace(subject)
	tenantID = strings.TrimSpace(tenantID)
	if !isUUID(subject) || !isUUID(tenantID) {
		return "", ErrInvalidToken
	}
	if ttl <= 0 || ttl > verifier.maxTTL {
		return "", ErrInvalidMaxTTL
	}
	now := verifier.now().UTC()
	header := map[string]any{
		"alg": AlgorithmHS256,
		"typ": "JWT",
	}
	payload := map[string]any{
		"iss":       verifier.issuer,
		"aud":       verifier.audience,
		"sub":       subject,
		"tenant_id": tenantID,
		"exp":       now.Add(ttl).Unix(),
		"nbf":       now.Add(-1 * time.Minute).Unix(),
	}
	headerSegment, err := encodeSegment(header)
	if err != nil {
		return "", err
	}
	payloadSegment, err := encodeSegment(payload)
	if err != nil {
		return "", err
	}
	signed := headerSegment + "." + payloadSegment
	return signed + "." + base64.RawURLEncoding.EncodeToString(signHS256([]byte(signed), verifier.secret)), nil
}

type HS256Verifier struct {
	issuer   string
	audience string
	secret   []byte
	maxTTL   time.Duration
	now      func() time.Time
}

func NewHS256Verifier(cfg Config) (*HS256Verifier, error) {
	if len(cfg.Secret) < minSecretBytes {
		return nil, ErrWeakSecret
	}
	cfg.Issuer = strings.TrimSpace(cfg.Issuer)
	cfg.Audience = strings.TrimSpace(cfg.Audience)
	if cfg.Issuer == "" || cfg.Audience == "" {
		return nil, fmt.Errorf("%w: missing issuer or audience", ErrInvalidToken)
	}
	maxTTL := cfg.MaxTTL
	if maxTTL == 0 {
		maxTTL = DefaultMaxTokenTTL
	}
	if maxTTL < 0 {
		return nil, ErrInvalidMaxTTL
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &HS256Verifier{
		issuer:   cfg.Issuer,
		audience: cfg.Audience,
		secret:   append([]byte(nil), cfg.Secret...),
		maxTTL:   maxTTL,
		now:      now,
	}, nil
}

func (v *HS256Verifier) Verify(token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return Claims{}, ErrInvalidToken
	}

	var header struct {
		Alg string `json:"alg"`
		Typ string `json:"typ,omitempty"`
	}
	if err := decodeSegment(parts[0], &header, true); err != nil {
		return Claims{}, ErrInvalidToken
	}
	if header.Alg != AlgorithmHS256 {
		return Claims{}, ErrInvalidToken
	}

	signed := parts[0] + "." + parts[1]
	expected := signHS256([]byte(signed), v.secret)
	got, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(got, expected) {
		return Claims{}, ErrInvalidToken
	}

	var payload rawClaims
	if err := decodeSegment(parts[1], &payload, false); err != nil {
		return Claims{}, ErrInvalidToken
	}
	claims, err := payload.validate(v.issuer, v.audience, v.now(), v.maxTTL)
	if err != nil {
		return Claims{}, err
	}
	return claims, nil
}

type rawClaims struct {
	Subject   string          `json:"sub"`
	TenantID  string          `json:"tenant_id"`
	Issuer    string          `json:"iss"`
	Audience  json.RawMessage `json:"aud"`
	Expires   json.RawMessage `json:"exp"`
	NotBefore json.RawMessage `json:"nbf"`
}

func (c rawClaims) validate(issuer, audience string, now time.Time, maxTTL time.Duration) (Claims, error) {
	return c.validateWithSkew(issuer, audience, now, maxTTL, 0, true, true, false)
}

// validateWithSkew validates the raw claims applying an optional clockSkew
// tolerance to exp and nbf. Both HS256Verifier and JWKSVerifier call this so
// the validation rules cannot diverge.
func (c rawClaims) validateWithSkew(issuer, audience string, now time.Time, maxTTL, clockSkew time.Duration, requireNotBefore, requireTenantID, mapExternalSubject bool) (Claims, error) {
	subject := strings.TrimSpace(c.Subject)
	tenantID := strings.TrimSpace(c.TenantID)
	if subject == "" {
		return Claims{}, ErrInvalidToken
	}
	actorID := subject
	if !isUUID(actorID) {
		if !mapExternalSubject {
			return Claims{}, ErrInvalidToken
		}
		actorID = StableSubjectID(issuer, subject)
	}
	if tenantID == "" {
		if requireTenantID {
			return Claims{}, ErrInvalidToken
		}
	} else if !isUUID(tenantID) {
		return Claims{}, ErrInvalidToken
	}
	if c.Issuer != issuer {
		return Claims{}, ErrInvalidToken
	}
	aud, err := parseAudience(c.Audience, audience)
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	expUnix, err := parseNumericDate(c.Expires)
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	exp := time.Unix(expUnix, 0).UTC()
	// exp must be in the future, allowing for clockSkew tolerance.
	if !now.Before(exp.Add(clockSkew)) {
		return Claims{}, ErrInvalidToken
	}
	// exp must not exceed now + maxTTL (no skew applied to the ceiling).
	if maxTTL > 0 && exp.After(now.Add(maxTTL)) {
		return Claims{}, ErrInvalidToken
	}
	var nbf time.Time
	if len(c.NotBefore) == 0 {
		if requireNotBefore {
			return Claims{}, ErrInvalidToken
		}
	} else {
		nbfUnix, err := parseNumericDate(c.NotBefore)
		if err != nil {
			return Claims{}, ErrInvalidToken
		}
		nbf = time.Unix(nbfUnix, 0).UTC()
		// nbf must not be in the future, allowing for clockSkew tolerance.
		if now.Add(clockSkew).Before(nbf) {
			return Claims{}, ErrInvalidToken
		}
	}
	return Claims{
		Subject:         actorID,
		ExternalSubject: subject,
		TenantID:        tenantID,
		Issuer:          c.Issuer,
		Audience:        aud,
		Expires:         exp,
		NotBefore:       nbf,
	}, nil
}

// StableSubjectID maps non-UUID IdP subjects, such as Firebase Auth UIDs, to a
// deterministic internal UUID for existing actor/grant columns.
func StableSubjectID(issuer, subject string) string {
	subject = strings.TrimSpace(subject)
	if isUUID(subject) {
		return strings.ToLower(subject)
	}
	namespace := [16]byte{0x89, 0x47, 0xf8, 0xfa, 0x12, 0xb2, 0x48, 0xa5, 0xa9, 0xdb, 0x93, 0x64, 0x42, 0x5d, 0x0f, 0x72}
	h := sha1.New()
	_, _ = h.Write(namespace[:])
	_, _ = h.Write([]byte(strings.TrimSpace(issuer)))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(subject))
	sum := h.Sum(nil)
	id := append([]byte(nil), sum[:16]...)
	id[6] = (id[6] & 0x0f) | 0x50
	id[8] = (id[8] & 0x3f) | 0x80
	return formatUUID(id)
}

func formatUUID(id []byte) string {
	const hex = "0123456789abcdef"
	out := make([]byte, 36)
	j := 0
	for i, b := range id {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			out[j] = '-'
			j++
		}
		out[j] = hex[b>>4]
		out[j+1] = hex[b&0x0f]
		j += 2
	}
	return string(out)
}

func decodeSegment(segment string, dst any, disallowUnknown bool) error {
	raw, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	if disallowUnknown {
		dec.DisallowUnknownFields()
	}
	return dec.Decode(dst)
}

func encodeSegment(src any) (string, error) {
	raw, err := json.Marshal(src)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func parseAudience(raw json.RawMessage, expected string) (string, error) {
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		if single == expected {
			return single, nil
		}
		return "", ErrInvalidToken
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err != nil {
		return "", err
	}
	for _, aud := range list {
		if aud == expected {
			return aud, nil
		}
	}
	return "", ErrInvalidToken
}

func parseNumericDate(raw json.RawMessage) (int64, error) {
	var n int64
	if err := json.Unmarshal(raw, &n); err == nil {
		return n, nil
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err != nil {
		return 0, err
	}
	return int64(f), nil
}

func signHS256(data, secret []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(data)
	return mac.Sum(nil)
}

func isUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, r := range value {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
				return false
			}
		}
	}
	return true
}
