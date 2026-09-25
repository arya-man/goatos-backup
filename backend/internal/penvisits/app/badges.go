package app

import "context"

// ModuleKey is the drawer/registry key of the module this tab lives in (bootstrap_copy.go):
// pen visits are the "For me" tab of the Tasks module (maintainer decision 2026-09-14, restoring
// the 2026-09-07 shape and retiring the 2026-09-12 step-on-the-parent-card fold), so their
// badge rides that module's key. The tab is the generic "work the system owes this person"
// list: pen visits are its first card type; a future module adds a card type here, not a tab.
const ModuleKey = "leadership_tasks"

// Bar-item hrefs the two counts belong to (bootstrap_copy.go's contributions). The module
// badge is their sum; each tab shows its own.
const (
	ForMeHref      = "/pen-visits"
	RaisedByMeHref = "/leadership-tasks"
)

// BadgeSource is the shape workforce/app.ModuleBadgeSource asks for; the leadership tasks
// service implements it for the same module key.
type BadgeSource interface {
	ModuleBadgeCounts(ctx context.Context, tenantID, userID string, moduleKeys []string) (map[string]int, error)
}

// NavItemBadgeSource is the per-tab half (workforce/app.NavItemBadgeSource); the leadership
// tasks service answers it for its own tab.
type NavItemBadgeSource interface {
	NavItemBadgeCounts(ctx context.Context, tenantID, userID string, hrefs []string) (map[string]int, error)
}

// ModuleBadges answers the Tasks module badge for BOTH halves of the module: the CXO's unseen
// assigned asks (inner) plus the park head's pen visits still owed. One person carries one of
// the two today, so the sum never double-counts; it is a sum rather than a max so a future
// person holding both reads the whole desk.
type ModuleBadges struct {
	inner  BadgeSource
	visits *Service
}

// NewModuleBadges composes the module badge over the leadership tasks source and this service.
func NewModuleBadges(inner BadgeSource, visits *Service) *ModuleBadges {
	return &ModuleBadges{inner: inner, visits: visits}
}

// ModuleBadgeCounts implements workforce/app.ModuleBadgeSource.
//
// The pen-visit count is read concurrently with the inner source (independent reads); the inner
// source's error still takes precedence, as it did when they ran in turn.
func (b *ModuleBadges) ModuleBadgeCounts(ctx context.Context, tenantID, userID string, moduleKeys []string) (map[string]int, error) {
	own := b.startOpenCount(ctx, tenantID, userID, containsKey(moduleKeys, ModuleKey))
	out := map[string]int{}
	if b.inner != nil {
		counts, err := b.inner.ModuleBadgeCounts(ctx, tenantID, userID, moduleKeys)
		if err != nil {
			own.wait()
			return nil, err
		}
		for k, v := range counts {
			out[k] = v
		}
	}
	if !own.wanted {
		return out, nil
	}
	n, err := own.wait()
	if err != nil {
		return nil, err
	}
	if n > 0 {
		out[ModuleKey] += n
	}
	return out, nil
}

// NavItemBadgeCounts implements workforce/app.NavItemBadgeSource: the "For me" tab carries the
// pens still owed; the "Raised by me" tab is delegated to the leadership tasks source.
func (b *ModuleBadges) NavItemBadgeCounts(ctx context.Context, tenantID, userID string, hrefs []string) (map[string]int, error) {
	own := b.startOpenCount(ctx, tenantID, userID, containsKey(hrefs, ForMeHref))
	out := map[string]int{}
	if inner, ok := b.inner.(NavItemBadgeSource); ok && inner != nil {
		counts, err := inner.NavItemBadgeCounts(ctx, tenantID, userID, hrefs)
		if err != nil {
			own.wait()
			return nil, err
		}
		for k, v := range counts {
			out[k] = v
		}
	}
	if !own.wanted {
		return out, nil
	}
	n, err := own.wait()
	if err != nil {
		return nil, err
	}
	if n > 0 {
		out[ForMeHref] = n
	}
	return out, nil
}

func containsKey(keys []string, want string) bool {
	for _, k := range keys {
		if k == want {
			return true
		}
	}
	return false
}

// pendingCount is an OpenCount read started in the background.
type pendingCount struct {
	wanted bool
	done   chan struct{}
	n      int
	err    error
}

func (p *pendingCount) wait() (int, error) {
	if !p.wanted {
		return 0, nil
	}
	<-p.done
	return p.n, p.err
}

func (b *ModuleBadges) startOpenCount(ctx context.Context, tenantID, userID string, wanted bool) *pendingCount {
	p := &pendingCount{wanted: wanted && b.visits != nil}
	if !p.wanted {
		return p
	}
	p.done = make(chan struct{})
	go func() {
		defer close(p.done)
		p.n, p.err = b.visits.OpenCount(ctx, tenantID, userID)
	}()
	return p
}
