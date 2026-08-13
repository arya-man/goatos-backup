package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"google.golang.org/api/googleapi"
)

type fakeProofObjectManager struct {
	stats     map[string]proofObjectMeta
	statErrs  map[string]error
	uploads   map[string]proofObjectMeta
	uploadErr error
	uploaded  []string
}

func (f *fakeProofObjectManager) stat(_ context.Context, _ string, objectKey string) (proofObjectMeta, error) {
	if err := f.statErrs[objectKey]; err != nil {
		return proofObjectMeta{}, err
	}
	meta, ok := f.stats[objectKey]
	if !ok {
		return proofObjectMeta{}, &googleapi.Error{Code: 404, Message: "object not found"}
	}
	return meta, nil
}

func (f *fakeProofObjectManager) upload(_ context.Context, _ string, proof preparedProof) (proofObjectMeta, error) {
	if f.uploadErr != nil {
		return proofObjectMeta{}, f.uploadErr
	}
	f.uploaded = append(f.uploaded, proof.ObjectKey)
	meta, ok := f.uploads[proof.ObjectKey]
	if !ok {
		return proofObjectMeta{}, errors.New("upload fixture missing")
	}
	return meta, nil
}

func TestSyncPreparedProofObjectsUploadsMissingObjectBeforeDBWrite(t *testing.T) {
	ctx := context.Background()
	proofs := map[string]preparedProof{
		"F1": {ObjectKey: "tenant/legacy/slack/feed/a.jpg", Size: 101, MD5: "local-md5"},
	}
	manager := &fakeProofObjectManager{
		uploads: map[string]proofObjectMeta{
			"tenant/legacy/slack/feed/a.jpg": {size: 101, md5: "local-md5"},
		},
	}

	if err := syncPreparedProofObjectsWith(ctx, manager, "goatos-stg-media", proofs); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(manager.uploaded) != 1 || manager.uploaded[0] != "tenant/legacy/slack/feed/a.jpg" {
		t.Fatalf("uploaded = %v, want missing proof object uploaded before completed proof row", manager.uploaded)
	}
}

func TestSyncPreparedProofObjectsRejectsSameSizeWrongObject(t *testing.T) {
	ctx := context.Background()
	proofs := map[string]preparedProof{
		"F1": {ObjectKey: "tenant/legacy/slack/feed/a.jpg", Size: 101, MD5: "local-md5"},
	}

	err := syncPreparedProofObjectsWith(ctx, &fakeProofObjectManager{
		stats: map[string]proofObjectMeta{
			"tenant/legacy/slack/feed/a.jpg": {size: 101, md5: "different-md5"},
		},
	}, "goatos-stg-media", proofs)
	if err == nil || !strings.Contains(err.Error(), "md5Hash mismatch") {
		t.Fatalf("err = %v, want same-size wrong object rejected before DB rows are written", err)
	}
}

func TestSyncPreparedProofObjectsRejectsWrongSize(t *testing.T) {
	ctx := context.Background()
	proofs := map[string]preparedProof{
		"F1": {ObjectKey: "tenant/legacy/slack/feed/a.jpg", Size: 101, MD5: "local-md5"},
	}

	err := syncPreparedProofObjectsWith(ctx, &fakeProofObjectManager{
		stats: map[string]proofObjectMeta{
			"tenant/legacy/slack/feed/a.jpg": {size: 100, md5: "local-md5"},
		},
	}, "goatos-stg-media", proofs)
	if err == nil || !strings.Contains(err.Error(), "size=100, want 101") {
		t.Fatalf("err = %v, want size mismatch before completed proof rows are written", err)
	}
}

func TestSyncPreparedProofObjectsAcceptsMatchingExistingObject(t *testing.T) {
	ctx := context.Background()
	proofs := map[string]preparedProof{
		"F1": {ObjectKey: "tenant/legacy/slack/feed/a.jpg", Size: 101, MD5: "local-md5"},
	}
	manager := &fakeProofObjectManager{
		stats: map[string]proofObjectMeta{
			"tenant/legacy/slack/feed/a.jpg": {size: 101, md5: "local-md5"},
		},
	}

	if err := syncPreparedProofObjectsWith(ctx, manager, "goatos-stg-media", proofs); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(manager.uploaded) != 0 {
		t.Fatalf("uploaded = %v, want matching pre-uploaded object reused", manager.uploaded)
	}
}
