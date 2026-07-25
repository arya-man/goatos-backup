// Package local provides a backend-owned local filesystem proof store for development and tests.
package local

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/proof/domain"
	"github.com/vgoats/goatos/backend/internal/proof/ports"
)

var errInvalidObjectKey = errors.New("proof local storage: invalid object key")

type Storage struct {
	baseDir string
	secret  []byte
}

func New(baseDir, secret string) *Storage {
	if strings.TrimSpace(baseDir) == "" {
		baseDir = ".goatos-local-media/proofs"
	}
	if secret == "" {
		secret = "goatos-local-proof-dev"
	}
	return &Storage{baseDir: baseDir, secret: []byte(secret)}
}

var _ ports.Storage = (*Storage)(nil)
var _ ports.SignedURLVerifier = (*Storage)(nil)

func (s *Storage) Provider() string { return "local" }

func (s *Storage) PrepareUpload(_ context.Context, proof domain.Artifact, expires time.Duration) (domain.UploadTarget, error) {
	expiresAt := time.Now().UTC().Add(expires)
	path := "/app/proofs/" + proof.ProofID + "/upload"
	return domain.UploadTarget{
		UploadURL:      signedPath(path, "PUT", proof.TenantID, expiresAt, s.secret),
		Method:         "PUT",
		Headers:        map[string]string{"Content-Type": proof.MimeType},
		ExpiresAt:      expiresAt,
		Proof:          proof,
		UploadProtocol: "simple_put",
	}, nil
}

func (s *Storage) PrepareDownload(_ context.Context, proof domain.Artifact, expires time.Duration) (string, error) {
	expiresAt := time.Now().UTC().Add(expires)
	path := "/app/proofs/" + proof.ProofID + "/download/signed"
	return signedPath(path, "GET", proof.TenantID, expiresAt, s.secret), nil
}

func (s *Storage) Store(_ context.Context, proof domain.Artifact, body io.Reader, mimeType string) (domain.StoredObject, error) {
	path, err := s.localPath(proof.ObjectKey)
	if err != nil {
		return domain.StoredObject{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return domain.StoredObject{}, err
	}
	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return domain.StoredObject{}, err
	}
	hasher := sha256.New()
	size, copyErr := io.Copy(file, io.TeeReader(body, hasher))
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return domain.StoredObject{}, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return domain.StoredObject{}, closeErr
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return domain.StoredObject{}, err
	}
	if strings.TrimSpace(mimeType) == "" {
		mimeType = mime.TypeByExtension(filepath.Ext(path))
	}
	return domain.StoredObject{
		ContentHash: "sha256:" + hex.EncodeToString(hasher.Sum(nil)),
		MimeType:    mimeType,
		SizeBytes:   size,
	}, nil
}

func (s *Storage) FinalizeUpload(_ context.Context, proof domain.Artifact, in domain.CompleteUpload) (domain.StoredObject, error) {
	path, err := s.localPath(proof.ObjectKey)
	if err != nil {
		return domain.StoredObject{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return domain.StoredObject{}, ports.ErrNotFound
		}
		return domain.StoredObject{}, err
	}
	defer file.Close()

	hasher := sha256.New()
	size, err := io.Copy(hasher, file)
	if err != nil {
		return domain.StoredObject{}, err
	}
	contentHash := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	mimeType := strings.TrimSpace(in.MimeType)
	if mimeType == "" {
		mimeType = proof.MimeType
	}
	if mimeType == "" {
		mimeType = mime.TypeByExtension(filepath.Ext(path))
	}
	if in.SizeBytes > 0 && in.SizeBytes != size {
		return domain.StoredObject{}, ports.ErrIntegrityMismatch
	}
	if in.ContentHash != "" && in.ContentHash != contentHash {
		return domain.StoredObject{}, ports.ErrIntegrityMismatch
	}
	return domain.StoredObject{
		ContentHash: contentHash,
		MimeType:    mimeType,
		SizeBytes:   size,
	}, nil
}

func (s *Storage) Open(_ context.Context, proof domain.Artifact) (ports.ReadSeekCloser, error) {
	path, err := s.localPath(proof.ObjectKey)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

func (s *Storage) Verify(method, path, tenantID, expires, signature string, now time.Time) bool {
	expUnix, err := strconv.ParseInt(expires, 10, 64)
	if err != nil || expUnix <= now.Unix() || strings.TrimSpace(tenantID) == "" {
		return false
	}
	expected := sign(method, path, tenantID, expires, s.secret)
	return hmac.Equal([]byte(expected), []byte(signature))
}

func (s *Storage) localPath(objectKey string) (string, error) {
	clean := filepath.Clean(strings.TrimLeft(strings.TrimSpace(objectKey), "/"))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errInvalidObjectKey
	}
	baseAbs, err := filepath.Abs(s.baseDir)
	if err != nil {
		return "", err
	}
	targetAbs := filepath.Join(baseAbs, clean)
	rel, err := filepath.Rel(baseAbs, targetAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", errInvalidObjectKey
	}
	return targetAbs, nil
}

func signedPath(path, method, tenantID string, expiresAt time.Time, secret []byte) string {
	expires := strconv.FormatInt(expiresAt.Unix(), 10)
	q := url.Values{}
	q.Set("tenant_id", tenantID)
	q.Set("expires", expires)
	q.Set("sig", sign(method, path, tenantID, expires, secret))
	return path + "?" + q.Encode()
}

func sign(method, path, tenantID, expires string, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(method))
	_, _ = mac.Write([]byte("\n"))
	_, _ = mac.Write([]byte(path))
	_, _ = mac.Write([]byte("\n"))
	_, _ = mac.Write([]byte(tenantID))
	_, _ = mac.Write([]byte("\n"))
	_, _ = mac.Write([]byte(expires))
	return hex.EncodeToString(mac.Sum(nil))
}
