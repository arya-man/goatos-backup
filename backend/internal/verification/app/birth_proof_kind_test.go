package app

import (
	"context"
	"testing"
	"time"

	tasksbridge "github.com/vgoats/goatos/backend/internal/tasks/adapters/verificationbridge"
	tasksapp "github.com/vgoats/goatos/backend/internal/tasks/app"
	tasksdomain "github.com/vgoats/goatos/backend/internal/tasks/domain"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
	"github.com/vgoats/goatos/backend/internal/verificationcatalog"
)

func TestBirthProofKindsReachVerifierPlayer(t *testing.T) {
	const photo = "33333333-3333-4333-8333-333333333333"
	const video = "44444444-4444-4444-8444-444444444444"
	for _, tc := range []struct {
		name        string
		proofs      []tasksdomain.ProofItem
		refs, mimes []string
		labels      []string
	}{
		{"photo", []tasksdomain.ProofItem{{Ref: photo, Kind: tasksdomain.ProofKindPhoto}}, []string{photo}, []string{"image/jpeg"}, []string{"Authored step"}},
		// A video and a photo of ONE step share the step title, so the verifier reads them apart
		// by kind (domain.ComposeMediaLabels) rather than as "1 of 2 / 2 of 2" of one series.
		{"mixed capture order", []tasksdomain.ProofItem{{Ref: photo, Kind: tasksdomain.ProofKindPhoto}, {Ref: video, Kind: tasksdomain.ProofKindVideo}}, []string{video, photo}, []string{"video/mp4", "image/jpeg"}, []string{"Authored step · video", "Authored step · photo"}},
		{"legacy video", nil, []string{video}, []string{"video/mp4"}, []string{"Authored step"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepo()
			svc := NewService(repo, nil)
			if err := svc.RegisterCategory(verificationcatalog.BirthEvidence); err != nil {
				t.Fatal(err)
			}
			err := tasksbridge.New(svc).EnqueueBirthStepVerification(context.Background(), tasksapp.BirthStepVerificationEnqueueRequest{
				TenantID: testTenant, WorkflowID: "11111111-1111-4111-8111-111111111111", ActionID: "22222222-2222-4222-8222-222222222222",
				Proofs: tc.proofs, ProofRefs: tc.refs, ProofLabel: "Authored step", SubjectLabel: "Authored step", CapturedAt: time.Now(), IdempotencyKey: "birth-step-proof",
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(repo.items) != 1 {
				t.Fatalf("created %d items", len(repo.items))
			}
			for _, item := range repo.items {
				rows := svc.resolveMedia(context.Background(), testTenant, []domain.Item{item})
				if len(rows[0].Media) != len(tc.refs) {
					t.Fatalf("media=%+v", rows[0].Media)
				}
				for i, media := range rows[0].Media {
					if media.MimeType != tc.mimes[i] || media.ProofID != tc.refs[i] || media.Label != tc.labels[i] {
						t.Fatalf("proof %d reached wrong player/label: %+v", i, media)
					}
				}
			}
		})
	}
}
