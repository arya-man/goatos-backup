package app

import "context"

// ModuleKey is the drawer/registry key of this module (bootstrap_copy.go).
const ModuleKey = "leadership_tasks"

// ModuleBadgeCounts implements workforce/app.ModuleBadgeSource: the CXO's unseen assigned
// tasks, under this module's key only. Asked for other keys it answers nothing for them.
func (s *Service) ModuleBadgeCounts(ctx context.Context, tenantID, userID string, moduleKeys []string) (map[string]int, error) {
	wanted := false
	for _, k := range moduleKeys {
		if k == ModuleKey {
			wanted = true
			break
		}
	}
	if !wanted {
		return map[string]int{}, nil
	}
	n, err := s.repo.UnseenCount(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	return map[string]int{ModuleKey: n}, nil
}

// TabHref is the bar item the unseen count belongs to (bootstrap_copy.go's contribution).
const TabHref = "/leadership-tasks"

// NavItemBadgeCounts implements workforce/app.NavItemBadgeSource: the CXO's unseen assigned
// tasks, on this module's own tab only.
func (s *Service) NavItemBadgeCounts(ctx context.Context, tenantID, userID string, hrefs []string) (map[string]int, error) {
	wanted := false
	for _, h := range hrefs {
		if h == TabHref {
			wanted = true
			break
		}
	}
	if !wanted {
		return map[string]int{}, nil
	}
	n, err := s.repo.UnseenCount(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return map[string]int{}, nil
	}
	return map[string]int{TabHref: n}, nil
}
