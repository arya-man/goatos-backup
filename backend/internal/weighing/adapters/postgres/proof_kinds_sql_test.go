package postgres

import (
	"os"
	"strings"
	"testing"
)

// The {ref: kind} subquery runs once per observation row. `proof_id = X OR proof_id::text IN
// (jsonb_each_text ...)` cannot use the proof_artifacts primary key, so every row seq-scanned the
// register (OCI clone, all 2,852 observations: 57.6 s; busiest shed's 23 rows: 442 ms). The
// ANY(uuid[]) form is a PK lookup (138 ms; 1.0 ms) with an md5-identical result. Pin it for every
// read path that renders proof kinds, including the case-sensitive UUID filter that keeps
// non-canonical slot values out exactly as the old lowercase text compare did.
func TestProofKindsSubqueryUsesPrimaryKeyLookup(t *testing.T) {
	const canonical = `e.value ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'`
	if !strings.Contains(animalProofKindsSQL, "pa.proof_id = ANY(array_append(") || !strings.Contains(animalProofKindsSQL, canonical) {
		t.Fatalf("animalProofKindsSQL must be an ANY(uuid[]) PK lookup over canonical UUIDs:\n%s", animalProofKindsSQL)
	}
	repo, err := os.ReadFile("repository.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(repo), "jsonb_each_text(weighing_observations.sop_proofs) e\n                         WHERE "+canonical) {
		t.Fatal("repository.go's weighing_observations proof kinds must use the same ANY(uuid[]) PK lookup")
	}
	for _, file := range []string{"repository.go", "capture_sql.go", "leadership_sheds_page.go"} {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(src), "proof_id::text IN (SELECT") {
			t.Fatalf("%s still has the unindexable proof_id::text IN (...) form", file)
		}
		if strings.Contains(string(src), "e.value ~* '^[0-9a-f]{8}") {
			t.Fatalf("%s matches slot refs case-insensitively; the old text compare was case-sensitive", file)
		}
	}
}
