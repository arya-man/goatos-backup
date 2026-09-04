package parkscope

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGrantScopeHasOneWriter pins the maintainer decision of 2026-09-04: no code outside
// this package (and the two recorded exceptions below) may insert a user_scope_grants row
// with a scope of its own. A new INSERT elsewhere is exactly how the three park records
// drifted apart before -- it compiled, it passed review, and it gave a Channapatna operator
// Coimbatore work.
//
// Recorded exceptions:
//   - workforce/adapters/postgres/repository.go: the shed/cohort/custodian branch of the
//     operators grant API, which is not park membership.
//   - permissions/adapters/postgres/email_grants.go: the login-time claim of a pending
//     email grant, which is immediately followed by ReconcileUser.
//   - cmd/seed-*: seeders, which call WritePersonScope / ReconcileUser after their insert
//     (seed-stg-login-grants) or are throwaway QA fixtures.
func TestGrantScopeHasOneWriter(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	allowed := map[string]bool{
		"internal/parkscope/parkscope.go":                        true,
		"internal/workforce/adapters/postgres/repository.go":     true,
		"internal/permissions/adapters/postgres/email_grants.go": true,
		"cmd/seed-stg-login-grants/main.go":                      true,
		"cmd/seed-dev-grant/main.go":                             true,
		"cmd/seed-vaccination-per-goat-qa/main.go":               true,
	}
	var offenders []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !strings.Contains(strings.ToLower(string(body)), "insert into user_scope_grants") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if !allowed[rel] {
			offenders = append(offenders, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) > 0 {
		t.Fatalf("user_scope_grants is written outside internal/parkscope: %v\nA grant's park scope is derived from the person's People-screen ticks (WritePersonScope / SyncGrantScope); add the ROLE through them instead of inserting a row with a scope of its own.", offenders)
	}
}

func TestDesiredScopesShapes(t *testing.T) {
	types, ids, err := DesiredScopes("t1", "tenant", []string{"p1"})
	if err != nil || len(types) != 1 || types[0] != "tenant" || ids[0] != "t1" {
		t.Fatalf("tenant mode = (%v,%v,%v), want one tenant pair on the tenant id", types, ids, err)
	}
	types, ids, err = DesiredScopes("t1", "parks", []string{"p1", " p2 ", "p1", ""})
	if err != nil || len(types) != 2 || ids[0] != "p1" || ids[1] != "p2" {
		t.Fatalf("parks mode = (%v,%v,%v), want two deduplicated park pairs", types, ids, err)
	}
	if _, _, err := DesiredScopes("t1", "parks", nil); err == nil {
		t.Fatal("parks mode with no parks must refuse rather than derive nothing")
	}
	if _, _, err := DesiredScopes("t1", "company", []string{"p1"}); err == nil {
		t.Fatal("an unknown scope mode must refuse")
	}
}
