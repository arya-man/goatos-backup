package app

import "context"

// ModuleKey is the drawer/registry key of the Routines module (bootstrap_copy.go), and
// ListHref its one bar item. Both counts are the same number: the checks this person still
// has to do.
const (
	ModuleKey = "pen_routines"
	ListHref  = "/pen-routines"
)

// BadgeSource is the shape workforce/app.ModuleBadgeSource asks for; the badge chain below
// wraps whichever source was registered before this module (the pen-visit / Tasks one).
type BadgeSource interface {
	ModuleBadgeCounts(ctx context.Context, tenantID, userID string, moduleKeys []string) (map[string]int, error)
}

// NavItemBadgeSource is the per-tab half (workforce/app.NavItemBadgeSource).
type NavItemBadgeSource interface {
	NavItemBadgeCounts(ctx context.Context, tenantID, userID string, hrefs []string) (map[string]int, error)
}

// ModuleBadges answers the Routines module badge and its list-item badge for one person,
// delegating every other key to the inner source so ONE badge source serves the workforce
// service.
type ModuleBadges struct {
	inner    BadgeSource
	routines *Service
}

// NewModuleBadges composes the badge over the inner source and this service.
func NewModuleBadges(inner BadgeSource, routines *Service) *ModuleBadges {
	return &ModuleBadges{inner: inner, routines: routines}
}

// ModuleBadgeCounts implements workforce/app.ModuleBadgeSource.
//
// This module's own count is read concurrently with the inner chain (they are independent
// reads); the inner source's error still takes precedence, as it did when they ran in turn.
func (b *ModuleBadges) ModuleBadgeCounts(ctx context.Context, tenantID, userID string, moduleKeys []string) (map[string]int, error) {
	own := b.startOpenCount(ctx, tenantID, userID, contains(moduleKeys, ModuleKey))
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
		out[ModuleKey] = n
	}
	return out, nil
}

// NavItemBadgeCounts implements workforce/app.NavItemBadgeSource.
func (b *ModuleBadges) NavItemBadgeCounts(ctx context.Context, tenantID, userID string, hrefs []string) (map[string]int, error) {
	own := b.startOpenCount(ctx, tenantID, userID, contains(hrefs, ListHref))
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
		out[ListHref] = n
	}
	return out, nil
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
	p := &pendingCount{wanted: wanted && b.routines != nil}
	if !p.wanted {
		return p
	}
	p.done = make(chan struct{})
	go func() {
		defer close(p.done)
		p.n, p.err = b.routines.OpenCount(ctx, tenantID, userID)
	}()
	return p
}

func contains(keys []string, want string) bool {
	for _, k := range keys {
		if k == want {
			return true
		}
	}
	return false
}
