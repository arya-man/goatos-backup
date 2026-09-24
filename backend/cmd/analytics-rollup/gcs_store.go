package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"

	"golang.org/x/oauth2/google"
)

var archiveBucketName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,61}[a-z0-9]$`)

// gcsStore is a create-only GCS JSON API client (no storage SDK dependency).
// Needs storage.objects.create and storage.objects.get on the bucket only;
// ifGenerationMatch=0 makes an existing object a 412, never an overwrite.
type gcsStore struct {
	bucket string
	client *http.Client
	base   string
}

func newGCSStore(ctx context.Context, bucket string) (*gcsStore, error) {
	if !archiveBucketName.MatchString(bucket) {
		return nil, fmt.Errorf("invalid archive bucket %q", bucket)
	}
	client, err := google.DefaultClient(ctx, "https://www.googleapis.com/auth/devstorage.read_write")
	if err != nil {
		return nil, err
	}
	return &gcsStore{bucket: bucket, client: client, base: "https://storage.googleapis.com"}, nil
}

func (s *gcsStore) Create(ctx context.Context, name string, body []byte) error {
	u := fmt.Sprintf("%s/upload/storage/v1/b/%s/o?uploadType=media&ifGenerationMatch=0&name=%s", s.base, url.PathEscape(s.bucket), url.QueryEscape(name))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/gzip")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	switch {
	case resp.StatusCode == http.StatusPreconditionFailed:
		return errObjectExists
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return fmt.Errorf("gcs create HTTP %d: %s", resp.StatusCode, msg)
	}
	return nil
}

func (s *gcsStore) Stat(ctx context.Context, name string) (objectInfo, error) {
	u := fmt.Sprintf("%s/storage/v1/b/%s/o/%s?fields=size,md5Hash", s.base, url.PathEscape(s.bucket), url.PathEscape(name))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return objectInfo{}, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return objectInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return objectInfo{}, fmt.Errorf("gcs stat HTTP %d: %s", resp.StatusCode, msg)
	}
	var meta struct {
		Size    string `json:"size"`
		MD5Hash string `json:"md5Hash"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&meta); err != nil {
		return objectInfo{}, err
	}
	size, err := strconv.ParseInt(meta.Size, 10, 64)
	if err != nil {
		return objectInfo{}, err
	}
	sum, err := base64.StdEncoding.DecodeString(meta.MD5Hash)
	if err != nil {
		return objectInfo{}, err
	}
	return objectInfo{Size: size, MD5: sum}, nil
}
