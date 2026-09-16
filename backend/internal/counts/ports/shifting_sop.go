package ports

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

// SHIFTING SOP (maintainer decision 2026-09-16, docs/decisions/shifting-sop.md): the raise
// extras, the completion card and the high-priority card a movement runs under are the `shifting`
// section of the PUBLISHED `shifting` SOP version, pinned per movement at raise. counts receives
// them through this seam; the only file naming sop_versions on its behalf is
// backend/internal/shiftingsop/adapters/postgres/rules_source.go.

var (
	// ErrShiftingSOPVersionUnknown: a raise echoed, or a movement is pinned to, a version the farm
	// never published. 409.
	ErrShiftingSOPVersionUnknown = errors.New("counts: shifting sop version unknown")
	// ErrShiftingProofSlotInvalid wraps an authored.ProofError: a compulsory capture missing, a
	// capture outside the card, one capture proving two slots, or a capture of the wrong kind. 422.
	// Returned ONLY by the judge (counts/app/shifting_sop.go).
	ErrShiftingProofSlotInvalid = errors.New("counts: shifting sop proof slot invalid")
	// ErrShiftingAnswerInvalid wraps an authored.AnswerError: a required question unanswered or an
	// answer the card did not offer. 422.
	ErrShiftingAnswerInvalid = errors.New("counts: shifting sop answer invalid")
)

// ProofMeta is the per-proof {label, kind} a verifier item carries beside its media refs
// (verification media_meta, migration 000317): the card's own slot title and the kind the
// register judged the capture to be.
type ProofMeta struct {
	Label string
	Kind  string
}

// ShiftingSOPRulesSource answers "which document does this movement run under".
type ShiftingSOPRulesSource interface {
	// PublishedRules is what a movement RAISED now is pinned to.
	PublishedRules(ctx context.Context, tenantID string) (domain.ShiftingRules, error)
	// RulesVersion is the exact document a movement was pinned to. Version 0 is the seeded
	// document; an unpublished version is ErrShiftingSOPVersionUnknown.
	RulesVersion(ctx context.Context, tenantID string, version int) (domain.ShiftingRules, error)
	// RulesVersions resolves a SET of versions in one call (the pending-execution page reads each
	// pinned version once, never per row). A version that is not published is absent from the map.
	RulesVersions(ctx context.Context, tenantID string, versions []int) (map[int]domain.ShiftingRules, error)
}

// StaticShiftingSOPRules is a ShiftingSOPRulesSource over fixed documents keyed by version -- for
// tests and fakes. Published is the version a raise pins; a version with no entry reads the seed
// for 0 and ErrShiftingSOPVersionUnknown otherwise.
type StaticShiftingSOPRules struct {
	Published domain.ShiftingRules
	ByVersion map[int]domain.ShiftingRules
	Err       error
	// Calls counts RulesVersions invocations, so a test can pin "once per page".
	Calls int
}

func (s *StaticShiftingSOPRules) PublishedRules(context.Context, string) (domain.ShiftingRules, error) {
	if s.Err != nil {
		return domain.ShiftingRules{}, s.Err
	}
	if s.Published.Version == 0 && s.Published.Completion.Proofs == nil {
		return domain.SeededShiftingRules(), nil
	}
	return s.Published, nil
}

func (s *StaticShiftingSOPRules) RulesVersion(_ context.Context, _ string, version int) (domain.ShiftingRules, error) {
	if s.Err != nil {
		return domain.ShiftingRules{}, s.Err
	}
	if r, ok := s.ByVersion[version]; ok {
		return r, nil
	}
	if s.Published.Version == version && version != 0 {
		return s.Published, nil
	}
	if version == 0 {
		return domain.SeededShiftingRules(), nil
	}
	return domain.ShiftingRules{}, ErrShiftingSOPVersionUnknown
}

func (s *StaticShiftingSOPRules) RulesVersions(ctx context.Context, tenantID string, versions []int) (map[int]domain.ShiftingRules, error) {
	s.Calls++
	out := map[int]domain.ShiftingRules{}
	for _, v := range versions {
		r, err := s.RulesVersion(ctx, tenantID, v)
		if errors.Is(err, ErrShiftingSOPVersionUnknown) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out[v] = r
	}
	return out, nil
}

// ExpectedShiftingProofMedia pairs one proof ref with the kind its slot requires and the error to
// return when it does not match, so "which capture was wrong" is answered by name.
type ExpectedShiftingProofMedia struct {
	ProofID           string
	Kind              string
	RequireLiveCamera bool
	OnAbsent          error
}

// ShiftingProofMedia validates and describes the captures a shifting card names. nil (unit tests,
// a DB-less assembly) skips the register check; when wired, every ref must be a completed,
// tenant-owned upload of the slot's kind.
type ShiftingProofMedia interface {
	ValidateShiftingProofMedia(ctx context.Context, tenantID string, expected []ExpectedShiftingProofMedia) error
	// DescribeShiftingProofMedia reports each proof's stored kind (photo / video) for the ids
	// that resolve; an id that does not resolve is absent.
	DescribeShiftingProofMedia(ctx context.Context, tenantID string, proofIDs []string) (map[string]string, error)
}

// ShiftingSOPPin is the narrow read the completion judge needs BEFORE it judges: which version the
// movement is pinned to, its priority, whether it may be completed at all, and -- for a rework --
// what it already stored. Read without a lock; the completion transaction re-checks the gate under
// the row lock.
type ShiftingSOPPin struct {
	Version            *int
	Priority           string
	AuthorizationState string
	EventStatus        string
	VerificationState  string
	// CompletionIdempotencyKey is the key of the completion already recorded, if any: a request
	// carrying the same key is a replay the repository answers, never a fresh judgement.
	CompletionIdempotencyKey string
	// StoredProofs / StoredAnswers are the captures and answers of the completion already recorded
	// (a rework keeps its answers and refuses reuse of the rejected captures).
	StoredProofs  authored.ProofRefs
	StoredAnswers authored.Answers
}

// ShiftingSOPStore is the extra read slice the SOP-driven completion needs from the repository.
// Separate from Repository so pre-existing fakes keep compiling; the postgres Repository
// implements it.
type ShiftingSOPStore interface {
	ShiftingSOPPin(ctx context.Context, tenantID, shiftingEventID string) (ShiftingSOPPin, error)
	// ShiftingEventByIdempotencyKey answers a raise retry BEFORE the raise card is judged: an exact
	// replay (same key, same fingerprint) must return the movement it already recorded even if a
	// version published since added a required question. found=false when the key is unknown; a
	// known key with another fingerprint is ErrIdempotencyConflict. raiseCapture is the approver's
	// snapshot the movement stored (nil when the raise recorded nothing).
	ShiftingEventByIdempotencyKey(ctx context.Context, tenantID, idempotencyKey, requestFingerprint string) (shiftingEventID string, raiseCapture json.RawMessage, found bool, err error)
}
