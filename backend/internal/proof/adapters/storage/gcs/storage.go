// Package gcs prepares V4 signed URLs for direct GCS proof media uploads/downloads.
package gcs

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/proof/domain"
	"github.com/vgoats/goatos/backend/internal/proof/ports"
)

const resumableChunkSizeBytes int64 = 8 * 1024 * 1024

type Storage struct {
	bucket      string
	clientEmail string
	privateKey  *rsa.PrivateKey
	client      *http.Client
	now         func() time.Time
}

func New(bucket, clientEmail, privateKeyPEM string) (*Storage, error) {
	key, err := parsePrivateKey(privateKeyPEM)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(bucket) == "" || strings.TrimSpace(clientEmail) == "" {
		return nil, errors.New("gcs proof storage requires bucket and client email")
	}
	return &Storage{
		bucket:      bucket,
		clientEmail: clientEmail,
		privateKey:  key,
		client:      &http.Client{Timeout: 10 * time.Second},
		now:         time.Now,
	}, nil
}

var _ ports.Storage = (*Storage)(nil)

func (s *Storage) Provider() string { return "gcs" }

func (s *Storage) PrepareUpload(_ context.Context, proof domain.Artifact, expires time.Duration) (domain.UploadTarget, error) {
	expiresAt := s.now().UTC().Add(expires)
	if proof.ProofType == "video" {
		headers := map[string]string{
			"Content-Length":             "0",
			"Content-Type":               contentTypeOrDefault(proof.MimeType),
			"x-goog-if-generation-match": "0",
			"x-goog-resumable":           "start",
		}
		signed, err := s.signedURL("POST", proof.ObjectKey, expiresAt, headers, nil)
		if err != nil {
			return domain.UploadTarget{}, err
		}
		return domain.UploadTarget{
			UploadURL:      signed,
			Method:         "POST",
			Headers:        headers,
			ExpiresAt:      expiresAt,
			Proof:          proof,
			UploadProtocol: "gcs_resumable_v1",
			ChunkSizeBytes: resumableChunkSizeBytes,
		}, nil
	}
	headers := map[string]string{"x-goog-if-generation-match": "0"}
	signed, err := s.signedURL("PUT", proof.ObjectKey, expiresAt, headers, nil)
	if err != nil {
		return domain.UploadTarget{}, err
	}
	return domain.UploadTarget{
		UploadURL:      signed,
		Method:         "PUT",
		Headers:        headers,
		ExpiresAt:      expiresAt,
		Proof:          proof,
		UploadProtocol: "simple_put",
	}, nil
}

func (s *Storage) PrepareDownload(_ context.Context, proof domain.Artifact, expires time.Duration) (string, error) {
	query := map[string]string{}
	if generation := generationFromProof(proof); generation != "" {
		query["generation"] = generation
	}
	return s.signedURL("GET", proof.ObjectKey, s.now().UTC().Add(expires), nil, query)
}

func (s *Storage) FinalizeUpload(ctx context.Context, proof domain.Artifact, in domain.CompleteUpload) (domain.StoredObject, error) {
	signed, err := s.signedURL("HEAD", proof.ObjectKey, s.now().UTC().Add(2*time.Minute), nil, nil)
	if err != nil {
		return domain.StoredObject{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, signed, nil)
	if err != nil {
		return domain.StoredObject{}, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return domain.StoredObject{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return domain.StoredObject{}, ports.ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return domain.StoredObject{}, errors.New("gcs proof object stat failed")
	}
	size := resp.ContentLength
	if in.SizeBytes > 0 && size >= 0 && in.SizeBytes != size {
		return domain.StoredObject{}, ports.ErrIntegrityMismatch
	}
	if size < 0 {
		size = 0
	}
	mimeType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if mimeType == "" {
		mimeType = strings.TrimSpace(in.MimeType)
	}
	if mimeType == "" {
		mimeType = proof.MimeType
	}
	contentHash := "gcs-object:" + strings.Trim(strings.TrimSpace(resp.Header.Get("ETag")), `"`)
	if generation := strings.TrimSpace(resp.Header.Get("X-Goog-Generation")); generation != "" {
		contentHash = "gcs-generation:" + generation
	}
	if contentHash == "gcs-object:" {
		contentHash = "gcs-object:verified"
	}
	return domain.StoredObject{
		ContentHash: contentHash,
		MimeType:    mimeType,
		SizeBytes:   size,
	}, nil
}

func (s *Storage) Store(context.Context, domain.Artifact, io.Reader, string) (domain.StoredObject, error) {
	return domain.StoredObject{}, ports.ErrUnsupported
}

func (s *Storage) Delete(ctx context.Context, proof domain.Artifact) error {
	signed, err := s.signedURL("DELETE", proof.ObjectKey, s.now().UTC().Add(2*time.Minute), nil, nil)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, signed, nil)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New("gcs proof object delete failed")
	}
	return nil
}

func (s *Storage) signedURL(method, objectKey string, expiresAt time.Time, headers map[string]string, extraQuery map[string]string) (string, error) {
	now := s.now().UTC()
	if expiresAt.Before(now) {
		expiresAt = now.Add(15 * time.Minute)
	}
	expires := int64(expiresAt.Sub(now).Seconds())
	if expires < 1 {
		expires = 1
	}
	date := now.Format("20060102")
	timestamp := now.Format("20060102T150405Z")
	scope := date + "/auto/storage/goog4_request"
	credential := s.clientEmail + "/" + scope
	escapedPath := "/" + url.PathEscape(s.bucket) + "/" + escapeObjectKey(objectKey)

	values := url.Values{}
	for key, value := range extraQuery {
		values.Set(key, value)
	}
	values.Set("X-Goog-Algorithm", "GOOG4-RSA-SHA256")
	values.Set("X-Goog-Credential", credential)
	values.Set("X-Goog-Date", timestamp)
	values.Set("X-Goog-Expires", strconvFormat(expires))
	signedHeaders, canonicalHeaderBlock := buildCanonicalHeaders(headers)
	values.Set("X-Goog-SignedHeaders", signedHeaders)
	canonicalQuery := values.Encode()

	canonicalRequest := method + "\n" +
		escapedPath + "\n" +
		canonicalQuery + "\n" +
		canonicalHeaderBlock + "\n" +
		signedHeaders + "\n" +
		"UNSIGNED-PAYLOAD"
	requestHash := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := "GOOG4-RSA-SHA256\n" +
		timestamp + "\n" +
		scope + "\n" +
		hex.EncodeToString(requestHash[:])
	signHash := sha256.Sum256([]byte(stringToSign))
	signature, err := rsa.SignPKCS1v15(rand.Reader, s.privateKey, crypto.SHA256, signHash[:])
	if err != nil {
		return "", err
	}
	return "https://storage.googleapis.com" + escapedPath + "?" + canonicalQuery + "&X-Goog-Signature=" + hex.EncodeToString(signature), nil
}

func buildCanonicalHeaders(headers map[string]string) (string, string) {
	normalized := map[string]string{"host": "storage.googleapis.com"}
	for key, value := range headers {
		name := strings.ToLower(strings.TrimSpace(key))
		if name == "" || name == "host" {
			continue
		}
		normalized[name] = strings.Join(strings.Fields(value), " ")
	}
	names := make([]string, 0, len(normalized))
	for name := range normalized {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		b.WriteString(name)
		b.WriteByte(':')
		b.WriteString(normalized[name])
		b.WriteByte('\n')
	}
	return strings.Join(names, ";"), b.String()
}

func generationFromProof(proof domain.Artifact) string {
	if !strings.HasPrefix(proof.ContentHash, "gcs-generation:") {
		return ""
	}
	return strings.TrimPrefix(proof.ContentHash, "gcs-generation:")
}

func escapeObjectKey(objectKey string) string {
	parts := strings.Split(strings.TrimPrefix(objectKey, "/"), "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func contentTypeOrDefault(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "application/octet-stream"
	}
	return value
}

func parsePrivateKey(raw string) (*rsa.PrivateKey, error) {
	raw = strings.ReplaceAll(raw, `\n`, "\n")
	block, _ := pem.Decode([]byte(raw))
	if block == nil {
		return nil, errors.New("invalid GCS private key PEM")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("GCS private key must be RSA")
	}
	return key, nil
}

func strconvFormat(v int64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	n := v
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
