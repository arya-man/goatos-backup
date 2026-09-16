package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
	tasksdomain "github.com/vgoats/goatos/backend/internal/tasks/domain"
)

// CAPTURE CARD SERVICE (maintainer decision 4, 2026-09-16). Serves the card the phone renders
// beside the Add birth / Add death form (GET /app/counts/capture-cards/{kind}) and judges a
// submission against it: pin the echoed version, validate slots and answers, resolve each
// capture's kind from the proof register, and compose the snapshot the approver and the
// verifier read. Counts never names sop tables -- the card arrives through CaptureCardSource
// (countssop/adapters/postgres).

var (
	// ErrCaptureSOPVersionUnknown is a NEW app echoing a sop_version_id that is neither published
	// nor retired for this card (HTTP 409 capture_sop_version_unknown): the phone refreshes its
	// card and resubmits.
	ErrCaptureSOPVersionUnknown = errors.New("counts: capture sop version is unknown")
	// ErrCaptureKindUnknown is a capture kind the form does not offer.
	ErrCaptureKindUnknown = errors.New("counts: capture card kind is unknown")
)

// Capture card kinds (the {kind} path segment).
const (
	CaptureKindBirth = "birth"
	CaptureKindDeath = "death"
)

// CaptureCardVersion is one authored card with the version it came from ("" = never authored,
// the empty card).
type CaptureCardVersion struct {
	VersionID    string
	VersionLabel string
	Card         domain.CaptureCard
}

// CaptureCardSource reads the card from the SOP library (implemented outside counts).
type CaptureCardSource interface {
	PublishedCaptureCard(ctx context.Context, tenantID, sopCode string) (CaptureCardVersion, error)
	CaptureCardVersion(ctx context.Context, tenantID, sopCode, versionID string) (CaptureCardVersion, error)
}

// CaptureProofKinds reads each capture's kind (video / photo) from the proof register; a ref the
// register does not hold is absent from the map.
type CaptureProofKinds interface {
	ProofKinds(ctx context.Context, tenantID string, refs []string) (map[string]string, error)
}

// CaptureCardService is the app service.
type CaptureCardService struct {
	source CaptureCardSource
	kinds  CaptureProofKinds
}

// NewCaptureCardService constructs the service. kinds may be nil (a typed slot then keeps its
// own kind; an `either` slot stays blank).
func NewCaptureCardService(source CaptureCardSource, kinds CaptureProofKinds) *CaptureCardService {
	return &CaptureCardService{source: source, kinds: kinds}
}

// CaptureCardView is the served card.
type CaptureCardView struct {
	Kind         string
	SOPCode      string
	VersionID    string
	VersionLabel string
	Card         domain.CaptureCard
}

func captureSOPCode(kind string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case CaptureKindBirth:
		return tasksdomain.SOPCodeBirth, nil
	case CaptureKindDeath:
		return tasksdomain.SOPCodeDeath, nil
	}
	return "", ErrCaptureKindUnknown
}

// Card serves the published card for a kind.
func (s *CaptureCardService) Card(ctx context.Context, tenantID, kind string) (CaptureCardView, error) {
	code, err := captureSOPCode(kind)
	if err != nil {
		return CaptureCardView{}, err
	}
	if strings.TrimSpace(tenantID) == "" {
		return CaptureCardView{}, ErrMissingRequiredField
	}
	version, err := s.source.PublishedCaptureCard(ctx, tenantID, code)
	if err != nil {
		return CaptureCardView{}, err
	}
	return CaptureCardView{Kind: strings.ToLower(strings.TrimSpace(kind)), SOPCode: code, VersionID: version.VersionID, VersionLabel: version.VersionLabel, Card: version.Card}, nil
}

// Judge applies the OLDER-APP rule (maintainer decision 7). sub == nil (the request carried no
// sop_capture) is judged against the PUBLISHED card: accepted, nothing mapped, every compulsory
// item noted as "Not captured (older app)". A present submission pins its echoed version (absent
// id = published) and is judged strictly: 422 naming the slot / question, and each capture's
// kind must be one the slot accepts as the register recorded it.
func (s *CaptureCardService) Judge(ctx context.Context, tenantID, kind string, sub *domain.CaptureSubmission) (*domain.ApprovalCapture, error) {
	code, err := captureSOPCode(kind)
	if err != nil {
		return nil, err
	}
	var version CaptureCardVersion
	if sub != nil && strings.TrimSpace(sub.SOPVersionID) != "" {
		version, err = s.source.CaptureCardVersion(ctx, tenantID, code, strings.TrimSpace(sub.SOPVersionID))
	} else {
		version, err = s.source.PublishedCaptureCard(ctx, tenantID, code)
	}
	if err != nil {
		return nil, err
	}
	judged, err := domain.JudgeCapture(version.Card, sub)
	if err != nil {
		return nil, err
	}
	kinds := map[string]string{}
	if len(judged.Ordered) > 0 && s.kinds != nil {
		refs := make([]string, 0, len(judged.Ordered))
		for _, p := range judged.Ordered {
			refs = append(refs, p.Ref)
		}
		kinds, err = s.kinds.ProofKinds(ctx, tenantID, refs)
		if err != nil {
			return nil, err
		}
		for _, p := range judged.Ordered {
			kind, known := kinds[p.Ref]
			if !known {
				return nil, fmt.Errorf("%w: %w", authored.ErrProofInvalid, &authored.ProofError{SlotKey: p.Slot.Key, Message: "This capture has not finished uploading: " + p.Slot.Title})
			}
			if !p.Slot.Accepts(kind) {
				return nil, fmt.Errorf("%w: %w", authored.ErrProofInvalid, &authored.ProofError{SlotKey: p.Slot.Key, Message: "Record a " + p.Slot.Kind + " for: " + p.Slot.Title})
			}
		}
	}
	evidence := domain.ComposeCaptureEvidence(version.VersionLabel, version.Card, judged.Proofs, kinds, judged.Answers, judged.Missing)
	if version.Card.IsEmpty() && evidence.IsEmpty() {
		return nil, nil
	}
	return &domain.ApprovalCapture{SOPVersionID: version.VersionID, Proofs: judged.Proofs, Answers: judged.Answers, Evidence: evidence}, nil
}
