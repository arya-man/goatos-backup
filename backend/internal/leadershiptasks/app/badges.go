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
