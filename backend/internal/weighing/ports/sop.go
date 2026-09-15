package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// WEIGHING SOP seam (maintainer decision 2026-09-15, docs/decisions/weighing-sop.md).
//
// Weighing is ISOLATED from every non-weighing table, so it never reads sop_versions itself.
// It holds this interface; the Postgres adapter that names the SOP tables lives OUTSIDE
// backend/internal/weighing (backend/internal/weighingsop) -- the feedwaterremoval shape --
// and hands weighing a compiled, versioned domain.Rules value.

var (
	// ErrSOPVersionUnknown: a task is pinned to a version the farm never published.
	ErrSOPVersionUnknown = errors.New("weighing: sop version unknown")

	// ErrModeNotAllowed refuses a plan that assigns a capture mode the SOP does not offer.
	ErrModeNotAllowed = errors.New("weighing: capture mode not offered by the weighing SOP")

	// ErrRemovalNotOffered refuses a plan that names a removal operator (or asks for the
	// removal) while the SOP has the precondition switched OFF.
	ErrRemovalNotOffered = errors.New("weighing: feed & water removal is switched off by the weighing SOP")

	// ErrWeighDateInPast refuses a plan without the removal precondition whose weigh date
	// is already behind the farm's business date.
	ErrWeighDateInPast = errors.New("weighing: weigh date is in the past")

	// ErrLumpSumVideoCount refuses a lump-sum submit carrying fewer or more pen videos than
	// the task's pinned SOP asks for.
	ErrLumpSumVideoCount = errors.New("weighing: lump-sum video count outside the weighing SOP's window")

	// ErrRemovalChangeLocked refuses switching a task's removal off once a pen of it was
	// submitted: the removal was performed and its evidence is in review or accepted.
	ErrRemovalChangeLocked = errors.New("weighing: feed & water removal cannot be switched off, a pen was already submitted")
)

// SOPRulesSource answers "which rules does weighing run under".
type SOPRulesSource interface {
	// PublishedRules is what a task planned NOW is stamped with and runs under.
	PublishedRules(ctx context.Context, tenantID string) (domain.Rules, error)
	// RulesVersion is the exact rule set a task was planned on. Version 0 is the seeded
	// document; an unpublished version is ErrSOPVersionUnknown.
	RulesVersion(ctx context.Context, tenantID string, version int) (domain.Rules, error)
}

// StaticRules is a SOPRulesSource over one fixed rule set -- for tests and fakes, so a service
// under test still reads its rules through the seam. Err, when set, is returned instead.
type StaticRules struct {
	Rules domain.Rules
	Err   error
}

func (s StaticRules) PublishedRules(context.Context, string) (domain.Rules, error) {
	if s.Err != nil {
		return domain.Rules{}, s.Err
	}
	return s.Rules, nil
}

func (s StaticRules) RulesVersion(_ context.Context, _ string, version int) (domain.Rules, error) {
	if s.Err != nil {
		return domain.Rules{}, s.Err
	}
	if version == 0 {
		return domain.SeededRules(), nil
	}
	if version != s.Rules.Version {
		return domain.Rules{}, ErrSOPVersionUnknown
	}
	return s.Rules, nil
}

// SOPPinReader reads the SOP version a task was planned on (weighing_campaigns.sop_version).
// Separate from Repository so the existing planner/execution fakes keep compiling; the
// postgres Repository implements it and is wired alongside the rules source.
type SOPPinReader interface {
	CampaignSOPVersion(ctx context.Context, tenantID, campaignID string) (int, error)
}
