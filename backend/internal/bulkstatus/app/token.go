package app

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	previewTokenV1        = "v1"
	previewTokenMaxAge    = 30 * time.Minute
	previewTokenClockSkew = 2 * time.Minute
	previewTokenNonceLen  = 16
	// DefaultDevSigningKey is the local/test-only fallback used when no shared
	// signing key is configured in a non-shared environment.
	DefaultDevSigningKey = "goatos-bulk-status-preview-dev-v1"
)

// rowsFingerprint is a deterministic, order-independent SHA-256 over the
// normalized (tenant, axis, rows) so preview and commit agree regardless of the
// order the client sends rows in. Millions of rows collapse to one 32-byte hash,
// which is what keeps the signed token O(1) instead of embedding every row.
func rowsFingerprint(tenantID, axis string, rows []EnqueueRow) string {
	normalized := make([]EnqueueRow, len(rows))
	copy(normalized, rows)
	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].GoatID != normalized[j].GoatID {
			return normalized[i].GoatID < normalized[j].GoatID
		}
		if normalized[i].Target != normalized[j].Target {
			return normalized[i].Target < normalized[j].Target
		}
		return normalized[i].Reason < normalized[j].Reason
	})
	h := sha256.New()
	fmt.Fprintf(h, "%s\n%s\n%d\n", strings.TrimSpace(tenantID), strings.TrimSpace(axis), len(normalized))
	for _, row := range normalized {
		// Length-prefixed to avoid delimiter-collision ambiguity across fields.
		fmt.Fprintf(h, "%d:%s|%d:%s|%d:%s\n",
			len(row.GoatID), row.GoatID,
			len(row.Target), row.Target,
			len(row.Reason), row.Reason)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (s *Service) signPreviewToken(tenantID, axis, fingerprint string, count int) (string, error) {
	nonce, err := randomNonce()
	if err != nil {
		return "", err
	}
	// india-date-guard:ignore: owner=ravi issue=GH-india-date scope=token-issuance-timestamp-absolute-instant expiry=2026-12-31
	return signPreviewTokenAt(tenantID, axis, fingerprint, count, s.signingKey, time.Now().UTC(), nonce)
}

func signPreviewTokenAt(tenantID, axis, fingerprint string, count int, signingKey string, issuedAt time.Time, nonce string) (string, error) {
	key := strings.TrimSpace(signingKey)
	if key == "" {
		return "", fmt.Errorf("bulk status preview signing key is required")
	}
	issuedUnix := issuedAt.UTC().Unix()
	payload, err := json.Marshal(struct {
		Version     string `json:"version"`
		TenantID    string `json:"tenant_id"`
		Axis        string `json:"axis"`
		Fingerprint string `json:"fingerprint"`
		Count       int    `json:"count"`
		IssuedAt    int64  `json:"issued_at"`
		Nonce       string `json:"nonce"`
	}{
		Version:     previewTokenV1,
		TenantID:    strings.TrimSpace(tenantID),
		Axis:        strings.TrimSpace(axis),
		Fingerprint: fingerprint,
		Count:       count,
		IssuedAt:    issuedUnix,
		Nonce:       nonce,
	})
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write(payload)
	return fmt.Sprintf("%s:%d:%s:%s", previewTokenV1, issuedUnix, nonce, hex.EncodeToString(mac.Sum(nil))), nil
}

func (s *Service) verifyPreviewToken(tenantID, axis, fingerprint string, count int, token string) error {
	parts := strings.Split(strings.TrimSpace(token), ":")
	if len(parts) != 4 || parts[0] != previewTokenV1 {
		return BadRequest("invalid_preview_token", "commit must include the preview_token returned by preview")
	}
	issuedUnix, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return BadRequest("invalid_preview_token", "commit must include the preview_token returned by preview")
	}
	if decoded, err := hex.DecodeString(parts[2]); err != nil || len(decoded) != previewTokenNonceLen {
		return BadRequest("invalid_preview_token", "commit must include the preview_token returned by preview")
	}
	if decoded, err := hex.DecodeString(parts[3]); err != nil || len(decoded) != sha256.Size {
		return BadRequest("invalid_preview_token", "commit must include the preview_token returned by preview")
	}
	issuedAt := time.Unix(issuedUnix, 0).UTC()
	now := time.Now().UTC()
	if now.Sub(issuedAt) > previewTokenMaxAge || issuedAt.After(now.Add(previewTokenClockSkew)) {
		return BadRequest("invalid_preview_token", "bulk status preview token expired; preview again")
	}
	expected, err := signPreviewTokenAt(tenantID, axis, fingerprint, count, s.signingKey, issuedAt, parts[2])
	if err != nil {
		return Internal("bulk status preview token verification failed")
	}
	if !hmac.Equal([]byte(expected), []byte(token)) {
		return BadRequest("invalid_preview_token", "commit rows do not match the latest preview; preview again")
	}
	return nil
}

func randomNonce() (string, error) {
	var nonce [previewTokenNonceLen]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(nonce[:]), nil
}
