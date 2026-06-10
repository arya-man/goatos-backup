package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	AlgorithmHS256 = "HS256"
	minSecretBytes = 32
)

var (
	ErrInvalidToken = errors.New("invalid token")
	ErrWeakSecret   = errors.New("auth secret must be at least 32 bytes")
)

type Config struct {
	Issuer   string
	Audience string
	Secret   []byte
	Now      func() time.Time
}

type Claims struct {
	Subject   string
	TenantID  string
	Issuer    string
	Audience  string
	Expires   time.Time
	NotBefore time.Time
}

type HS256Verifier struct {
	issuer   string
	audience string
	secret   []byte
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
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &HS256Verifier{
		issuer:   cfg.Issuer,
		audience: cfg.Audience,
		secret:   append([]byte(nil), cfg.Secret...),
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
	claims, err := payload.validate(v.issuer, v.audience, v.now())
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

func (c rawClaims) validate(issuer, audience string, now time.Time) (Claims, error) {
	subject := strings.TrimSpace(c.Subject)
	tenantID := strings.TrimSpace(c.TenantID)
	if !isUUID(subject) || !isUUID(tenantID) {
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
	if !now.Before(exp) {
		return Claims{}, ErrInvalidToken
	}
	nbfUnix, err := parseNumericDate(c.NotBefore)
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	nbf := time.Unix(nbfUnix, 0).UTC()
	if now.Before(nbf) {
		return Claims{}, ErrInvalidToken
	}
	return Claims{
		Subject:   subject,
		TenantID:  tenantID,
		Issuer:    c.Issuer,
		Audience:  aud,
		Expires:   exp,
		NotBefore: nbf,
	}, nil
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
