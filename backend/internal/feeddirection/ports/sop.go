package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
)

// FEED SOP (maintainer decision 2026-09-16): the cards the crew runs -- what to capture and
// answer at distribution, wastage, packing and transport -- are the `feed` section of the
// PUBLISHED feed.* SOP versions. feeddirection receives them through this seam; the only file
// naming sop_versions on its behalf is backend/internal/feedsop/adapters/postgres/rules_source.go.

var (
	// ErrSOPVersionUnknown: a sheet or task is pinned to a version the farm never published.
	ErrSOPVersionUnknown = errors.New("feeddirection: sop version unknown")
	// ErrSheetNotIssued: a completion names a park/feed day/workflow for which no sheet was ever
	// issued. There is no pen-session to complete and no pinned card to judge against; a write
	// here would invent work the crew was never directed to do (a packing for a day with no
	// sheet, a distribution for a pen nobody was asked to feed).
	ErrSheetNotIssued = errors.New("feeddirection: no feed sheet has been issued for this day")
	// ErrFeedDayNotReached: a distribution or wastage completion for a feed day AFTER today's
	// business date. The sheet exists from the day before (it is issued at 07:00 for tomorrow),
	// but the feeding it proves has not happened yet; the phone never offers it (today only) and
	// the write path refuses it too, so a wrong device clock cannot record tomorrow's feeding.
	ErrFeedDayNotReached = errors.New("feeddirection: this feed day has not started yet")
	// ErrSOPProofSlotInvalid wraps an authored.ProofError: a compulsory capture missing, a capture
	// outside the card, one capture proving two slots, or a capture of the wrong kind. 422.
	ErrSOPProofSlotInvalid = errors.New("feeddirection: sop proof slot invalid")
	// ErrSOPAnswerInvalid wraps an authored.AnswerError: a required question unanswered or an
	// answer the card did not offer. 422.
	ErrSOPAnswerInvalid = errors.New("feeddirection: sop answer invalid")
)

// ProofMeta is the per-proof {label, kind} a verifier item carries beside its media refs (the
// verification module's media_meta, migration 000317): the card's own slot title, and the kind
// the register judged the capture to be.
type ProofMeta struct {
	Label string
	Kind  string
}

// SOPRulesSource answers "which card does this stage run under".
type SOPRulesSource interface {
	// PublishedRules is what a sheet issued NOW (or a transport task materialized now) is
	// stamped with and runs under.
	PublishedRules(ctx context.Context, tenantID, stage string) (domain.Rules, error)
	// RulesVersion is the exact card a sheet/task was pinned to. Version 0 is the seeded
	// document; an unpublished version is ErrSOPVersionUnknown.
	RulesVersion(ctx context.Context, tenantID, stage string, version int) (domain.Rules, error)
}

// StaticSOPRules is a SOPRulesSource over fixed rule sets keyed by stage -- for tests and fakes.
// A stage with no entry reads the seeded card, so an unwired service behaves exactly as before.
type StaticSOPRules struct {
	ByStage map[string]domain.Rules
	Err     error
}

func (s StaticSOPRules) PublishedRules(_ context.Context, _ string, stage string) (domain.Rules, error) {
	if s.Err != nil {
		return domain.Rules{}, s.Err
	}
	if r, ok := s.ByStage[stage]; ok {
		return r, nil
	}
	return domain.SeededRules(stage), nil
}

func (s StaticSOPRules) RulesVersion(_ context.Context, _ string, stage string, version int) (domain.Rules, error) {
	if s.Err != nil {
		return domain.Rules{}, s.Err
	}
	if r, ok := s.ByStage[stage]; ok && r.Version == version {
		return r, nil
	}
	if version == 0 {
		return domain.SeededRules(stage), nil
	}
	return domain.Rules{}, ErrSOPVersionUnknown
}
