package cubeclient

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"time"
)

// signSecurityContext mints a short-lived HS256 JWT whose payload carries the
// tenant-scoped Cube security context. Cube verifies the signature with the
// shared CUBEJS_API_SECRET and exposes the payload as securityContext to
// queryRewrite. Stdlib-only (no external JWT dependency).
//
// The tenant id here always originates from the server-side session — never
// from user text. Extra claims (role, scope) may be added later; Cube's
// queryRewrite is the enforcement point.
func signSecurityContext(secret, tenantID string, ttl time.Duration) (string, error) {
	now := time.Now()
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	payload := map[string]any{
		"tenant_id": tenantID,
		"iat":       now.Unix(),
		"exp":       now.Add(ttl).Unix(),
	}
	hb, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	pb, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding
	signingInput := enc.EncodeToString(hb) + "." + enc.EncodeToString(pb)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	sig := enc.EncodeToString(mac.Sum(nil))
	return signingInput + "." + sig, nil
}
