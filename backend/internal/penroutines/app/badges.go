package app

import "context"

// ModuleKey is the drawer/registry key of the Routines module (bootstrap_copy.go), and
// ListHref its one bar item. Both counts are the same number: the checks this person still
// has to do.
const (
	ModuleKey = "pen_routines"
	ListHref  = "/pen-routines"
)

// ModuleBadges answers the Routines module badge and its list-item badge for one person.
type ModuleBadges struct {
	routines *Service
}

// NewModuleBadges composes the badge over the service.
func NewModuleBadges(routines *Service) *ModuleBadges {
	return &ModuleBadges{routines: routines}
}

// ModuleBadgeCounts implements workforce/app.ModuleBadgeSource.
func (b *ModuleBadges) ModuleBadgeCounts(ctx context.Context, tenantID, userID string, moduleKeys []string) (map[string]int, error) {
	out := map[string]int{}
	if b == nil || b.routines == nil || !contains(moduleKeys, ModuleKey) {
		return out, nil
	}
	n, err := b.routines.OpenCount(ctx, tenantID, userID)
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
	out := map[string]int{}
	if b == nil || b.routines == nil || !contains(hrefs, ListHref) {
		return out, nil
	}
	n, err := b.routines.OpenCount(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	if n > 0 {
		out[ListHref] = n
	}
	return out, nil
}

func contains(keys []string, want string) bool {
	for _, k := range keys {
		if k == want {
			return true
		}
	}
	return false
}
