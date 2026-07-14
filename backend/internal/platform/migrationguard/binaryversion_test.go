package migrationguard

import (
	"os"
	"regexp"
	"sort"
	"strconv"
	"testing"
)

func TestBinaryVersion(t *testing.T) {
	got, err := BinaryVersion()
	if err != nil {
		t.Fatalf("BinaryVersion() error = %v", err)
	}

	versionShape := regexp.MustCompile(`^[0-9]+$`)
	if !versionShape.MatchString(got) {
		t.Fatalf("BinaryVersion() = %q, want a numeric version string", got)
	}

	// Cross-check the embedded result against the real migrations directory
	// on disk. The embed is a build-time snapshot of the same directory, so
	// for a `go test` run against the current checkout they must agree. This
	// is deliberately not pinned to a specific version number, so it does
	// not need updating every time a migration is added.
	entries, err := os.ReadDir("../../../migrations/postgres")
	if err != nil {
		t.Fatalf("read migrations/postgres from disk: %v", err)
	}
	filenameVersion := regexp.MustCompile(`^([0-9]+)_`)
	var versions []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if m := filenameVersion.FindStringSubmatch(entry.Name()); m != nil {
			versions = append(versions, m[1])
		}
	}
	if len(versions) == 0 {
		t.Fatal("no migration files found on disk to cross-check against")
	}
	// Sort numerically, not lexicographically, so that migrations like
	// 999999 and 1000000 are ordered correctly.
	sort.Slice(versions, func(i, j int) bool {
		ni, _ := strconv.ParseInt(versions[i], 10, 64)
		nj, _ := strconv.ParseInt(versions[j], 10, 64)
		return ni < nj
	})
	want := versions[len(versions)-1]
	if got != want {
		t.Fatalf("BinaryVersion() = %q, want %q (highest migration on disk) - embedded migrations/postgres may be stale, rebuild", got, want)
	}
}
