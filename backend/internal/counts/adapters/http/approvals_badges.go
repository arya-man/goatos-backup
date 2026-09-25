package http

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

// The phone's Approvals badge (2026-09-25). The bootstrap's module badge chain carried no source
// for the "approvals" module, so its badge was always 0 while requests waited. The badge is the
// number of PENDING requests THIS caller may decide: the same decidable types
// (permissions.DecidableApprovalRequestTypes) and the same park scope the queue applies, read from
// the SAME authenticated grants on the request context the list handler reads -- so the badge and
// the queue cannot disagree about who may decide what. One bounded COUNT, whole queue, never a page.

const (
	// ApprovalsModuleKey is the registry key of the Approvals module (workforce bootstrap_copy.go).
	ApprovalsModuleKey = "approvals"
	// ApprovalsNavHref is the module's one bar item.
	ApprovalsNavHref = "/counts/approvals"
)

// pendingApprovalCounter is counts/app.ApprovalService.CountPending.
type pendingApprovalCounter interface {
	CountPending(ctx context.Context, tenantID string, decidableTypes, callerParkIDs []string) (int, error)
}

// moduleBadgeSource / navItemBadgeSource mirror workforce/app.ModuleBadgeSource and
// NavItemBadgeSource (declared here so counts does not import workforce).
type moduleBadgeSource interface {
	ModuleBadgeCounts(ctx context.Context, tenantID, userID string, moduleKeys []string) (map[string]int, error)
}

type navItemBadgeSource interface {
	NavItemBadgeCounts(ctx context.Context, tenantID, userID string, hrefs []string) (map[string]int, error)
}

// ApprovalsBadges chains the Approvals count over an inner badge source.
type ApprovalsBadges struct {
	inner    moduleBadgeSource
	approval pendingApprovalCounter
}

// NewApprovalsBadges composes the Approvals badge over inner (which may be nil).
func NewApprovalsBadges(inner moduleBadgeSource, approvals pendingApprovalCounter) *ApprovalsBadges {
	return &ApprovalsBadges{inner: inner, approval: approvals}
}

func (b *ApprovalsBadges) pending(ctx context.Context, tenantID string) (int, error) {
	if b.approval == nil {
		return 0, nil
	}
	decidable := permissions.DecidableApprovalRequestTypes(callerRolesFromContext(ctx))
	if len(decidable) == 0 {
		return 0, nil
	}
	return b.approval.CountPending(ctx, tenantID, decidable, callerParkScopeFromContext(ctx))
}

// ModuleBadgeCounts implements workforce/app.ModuleBadgeSource.
func (b *ApprovalsBadges) ModuleBadgeCounts(ctx context.Context, tenantID, userID string, moduleKeys []string) (map[string]int, error) {
	out := map[string]int{}
	if b.inner != nil {
		counts, err := b.inner.ModuleBadgeCounts(ctx, tenantID, userID, moduleKeys)
		if err != nil {
			return nil, err
		}
		for k, v := range counts {
			out[k] = v
		}
	}
	if !containsString(moduleKeys, ApprovalsModuleKey) {
		return out, nil
	}
	n, err := b.pending(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if n > 0 {
		out[ApprovalsModuleKey] = n
	}
	return out, nil
}

// NavItemBadgeCounts implements workforce/app.NavItemBadgeSource: the Approvals bar item carries
// the same number; every other href is the inner source's.
func (b *ApprovalsBadges) NavItemBadgeCounts(ctx context.Context, tenantID, userID string, hrefs []string) (map[string]int, error) {
	out := map[string]int{}
	if inner, ok := b.inner.(navItemBadgeSource); ok && inner != nil {
		counts, err := inner.NavItemBadgeCounts(ctx, tenantID, userID, hrefs)
		if err != nil {
			return nil, err
		}
		for k, v := range counts {
			out[k] = v
		}
	}
	if !containsString(hrefs, ApprovalsNavHref) {
		return out, nil
	}
	n, err := b.pending(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if n > 0 {
		out[ApprovalsNavHref] = n
	}
	return out, nil
}

func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
