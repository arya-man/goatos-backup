package permissions

import "strings"

type Route struct {
	OperationID string
	Method      string
	Pattern     string
	Permissions []string
	AdminOnly   bool
}

var protectedRoutes = []Route{
	{OperationID: "searchGoats", Method: "GET", Pattern: "/goats/search", Permissions: []string{GoatRead}},
	{OperationID: "getGoatPassport", Method: "GET", Pattern: "/goats/{goat_id}", Permissions: []string{GoatRead}},
	{OperationID: "getGoatTimeline", Method: "GET", Pattern: "/goats/{goat_id}/timeline", Permissions: []string{GoatRead}},
	{OperationID: "resolveIdentifier", Method: "GET", Pattern: "/identifiers/{type}/{value}/resolve", Permissions: []string{GoatRead}},
	{OperationID: "createCorrectionRequest", Method: "POST", Pattern: "/identity/correction-requests", Permissions: []string{CorrectionCreate}},
	{OperationID: "listCorrectionRequests", Method: "GET", Pattern: "/identity/correction-requests", Permissions: []string{CorrectionCreate}},

	{OperationID: "createImportRun", Method: "POST", Pattern: "/admin/import-runs", Permissions: []string{ImportRunManage}, AdminOnly: true},
	{OperationID: "getImportRun", Method: "GET", Pattern: "/admin/import-runs/{import_run_id}", Permissions: []string{ImportRunView}},
	{OperationID: "listImportRunRows", Method: "GET", Pattern: "/admin/import-runs/{import_run_id}/rows", Permissions: []string{ImportRunView}},
	{OperationID: "listIdentityConflicts", Method: "GET", Pattern: "/admin/identity/conflicts", Permissions: []string{GoatViewDirtyData}},
	{OperationID: "getIdentityConflict", Method: "GET", Pattern: "/admin/identity/conflicts/{conflict_id}", Permissions: []string{GoatViewDirtyData}},
	{OperationID: "listIdentityCandidates", Method: "GET", Pattern: "/admin/identity/candidates", Permissions: []string{GoatViewDirtyData}},
	{OperationID: "adminListCorrectionRequests", Method: "GET", Pattern: "/admin/identity/correction-requests", Permissions: []string{GoatReviewIdentity}},
	{OperationID: "resolveCorrectionRequest", Method: "POST", Pattern: "/admin/identity/correction-requests/{correction_request_id}/resolve", Permissions: []string{GoatReviewIdentity}},
	{OperationID: "resolveIdentityConflict", Method: "POST", Pattern: "/admin/identity/conflicts/{conflict_id}/resolve", Permissions: []string{GoatReviewIdentity}},
	{OperationID: "approveIdentityCandidate", Method: "POST", Pattern: "/admin/identity/candidates/{candidate_id}/approve", Permissions: []string{GoatReviewIdentity}},
	{OperationID: "rejectIdentityCandidate", Method: "POST", Pattern: "/admin/identity/candidates/{candidate_id}/reject", Permissions: []string{GoatReviewIdentity}},
	{OperationID: "createAdminGoat", Method: "POST", Pattern: "/admin/goats", Permissions: []string{GoatWriteIdentity}},
	{OperationID: "updateAdminGoat", Method: "PATCH", Pattern: "/admin/goats/{goat_id}", Permissions: []string{GoatWriteIdentity}},
	{OperationID: "addGoatIdentifier", Method: "POST", Pattern: "/admin/goats/{goat_id}/identifiers", Permissions: []string{GoatWriteIdentity}},
	{OperationID: "retireGoatIdentifier", Method: "POST", Pattern: "/admin/goats/{goat_id}/identifiers/{identifier_id}/retire", Permissions: []string{GoatWriteIdentity}},

	{OperationID: "getIdentityCounts", Method: "GET", Pattern: "/analytics/identity/counts", Permissions: []string{AnalyticsIdentityRead}},
}

func ProtectedRoutes() []Route {
	out := make([]Route, len(protectedRoutes))
	copy(out, protectedRoutes)
	return out
}

func Match(method, path string) (Route, bool) {
	for _, route := range protectedRoutes {
		if route.Method == method && pathMatches(route.Pattern, path) {
			return route, true
		}
	}
	return Route{}, false
}

func pathMatches(pattern, path string) bool {
	patternParts := splitPath(pattern)
	pathParts := splitPath(path)
	if len(patternParts) != len(pathParts) {
		return false
	}
	for i := range patternParts {
		p := patternParts[i]
		if strings.HasPrefix(p, "{") && strings.HasSuffix(p, "}") {
			if pathParts[i] == "" {
				return false
			}
			continue
		}
		if p != pathParts[i] {
			return false
		}
	}
	return true
}

func splitPath(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}
