package pgtemplate

import (
	"reflect"
	"testing"
	"time"
)

func TestHashIsContentKeyed(t *testing.T) {
	base := []Migration{{"000001_a.sql", []byte("-- +goose Up\nCREATE TABLE a();")}, {"000002_b.sql", []byte("x")}}
	h := Hash(base)
	if len(h) != 16 || h != Hash(base) {
		t.Fatalf("hash not stable 16-hex: %q", h)
	}
	edited := []Migration{base[0], {"000002_b.sql", []byte("y")}}
	renamed := []Migration{base[0], {"000003_b.sql", []byte("x")}}
	added := append(append([]Migration{}, base...), Migration{"000003_c.sql", nil})
	// Boundary shift: moving a byte between name and content must change the hash.
	shifted := []Migration{{"000001_a.sql", []byte("-- +goose Up\nCREATE TABLE a();")}, {"000002_b.sqlx", []byte("")}}
	for name, m := range map[string][]Migration{"edited": edited, "renamed": renamed, "added": added, "shifted": shifted} {
		if Hash(m) == h {
			t.Errorf("%s migrations produced the same hash", name)
		}
	}
	if !templateRE.MatchString(TemplateName(h)) {
		t.Fatalf("template name %q does not match its own pattern", TemplateName(h))
	}
}

func TestNamesFitPostgresLimitAndPatterns(t *testing.T) {
	now := time.Unix(1790000000, 0)
	for _, n := range []string{TemplateName("0123456789abcdef"), buildName("0123456789abcdef", now), CloneName(now, "99999_0123456789ab_leak123")} {
		if len(n) > 63 {
			t.Errorf("%q exceeds 63 bytes", n)
		}
		if !IsPgtestDatabase(n) {
			t.Errorf("%q not recognised as a pgtest database", n)
		}
	}
	if m := markerRE.FindStringSubmatch(marker("0123456789abcdef", now)); m == nil || m[1] != "0123456789abcdef" {
		t.Fatalf("marker does not round-trip: %q", marker("0123456789abcdef", now))
	}
}

// The cleanup filter must NEVER select a database outside the three pgtest patterns, however old or
// however similar its name is -- above all the stg clone `goatos` on the OCI server.
func TestCleanupNeverMatchesForeignDatabases(t *testing.T) {
	foreign := []string{
		"goatos", "postgres", "template0", "template1", "goatos_stg", "goatos_prod",
		"goatos_tmpl_123_abc", "goatos_test_123_abc_1", "goatos_sqlc_plans_123",
		"goatos_pgtest_template_", "goatos_pgtest_template_0123456789ABCDEF", "goatos_pgtest_template_0123456789abcdef0",
		"goatos_pgtest_clone_", "goatos_pgtest_clone_123_x", "xgoatos_pgtest_clone_1000000000_a",
		"goatos_pgtest_clone_1000000000_a;drop", "goatos_pgtest_build_0123456789abcdef",
		"GOATOS_PGTEST_CLONE_1000000000_A", "goatos_pgtest_clone_1000000000_a\n",
	}
	var dbs []Candidate
	for _, n := range foreign {
		if IsPgtestDatabase(n) {
			t.Errorf("%q wrongly recognised as a pgtest database", n)
		}
		dbs = append(dbs, Candidate{Name: n, Comment: "goatos-pgtest-template hash=0123456789abcdef built=1000000000"})
	}
	if got := SelectStale(dbs, "ffffffffffffffff", time.Unix(1900000000, 0)); len(got) != 0 {
		t.Fatalf("cleanup selected foreign databases: %v", got)
	}
}

func TestSelectStale(t *testing.T) {
	now := time.Unix(1790000000, 0)
	cur := "aaaaaaaaaaaaaaaa"
	tmpl := func(hash string, age time.Duration) Candidate {
		return Candidate{Name: TemplateName(hash), Comment: marker(hash, now.Add(-age))}
	}
	dbs := []Candidate{
		{Name: "goatos"},
		tmpl(cur, 30*24*time.Hour),                // current: always kept, however old
		tmpl("bbbbbbbbbbbbbbbb", 10*24*time.Hour), // other, older than 3 days: dropped
		tmpl("cccccccccccccccc", 20*24*time.Hour), // other, older: dropped
		tmpl("dddddddddddddddd", 1*time.Hour),     // most recent other ("previous"): kept
		{Name: TemplateName("eeeeeeeeeeeeeeee")},  // unmarked other template: dropped
		{Name: CloneName(now.Add(-7*time.Hour), "old_1")},
		{Name: CloneName(now.Add(-1*time.Hour), "fresh_1")},
		{Name: buildName("ffffffffffffffff", now.Add(-7*time.Hour))},
		{Name: buildName(cur, now.Add(-time.Minute))},
	}
	got := SelectStale(dbs, cur, now)
	want := []string{
		CloneName(now.Add(-7*time.Hour), "old_1"),
		buildName("ffffffffffffffff", now.Add(-7*time.Hour)),
		TemplateName("bbbbbbbbbbbbbbbb"),
		TemplateName("cccccccccccccccc"),
		TemplateName("eeeeeeeeeeeeeeee"),
	}
	sortStrings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SelectStale:\n got %v\nwant %v", got, want)
	}
}

func TestDSNForSwapsOnlyTheDatabase(t *testing.T) {
	cases := map[string]string{
		"postgres://u:p@127.0.0.1:15432/goatos?sslmode=disable": "postgres://u:p@127.0.0.1:15432/x?sslmode=disable",
		"postgresql://u@h/goatos":                               "postgresql://u@h/x",
		"postgres://u@h:1":                                      "postgres://u@h:1/x",
		"host=h dbname=goatos":                                  "host=h dbname=goatos dbname=x",
	}
	for in, want := range cases {
		if got := DSNFor(in, "x"); got != want {
			t.Errorf("DSNFor(%q) = %q, want %q", in, got, want)
		}
	}
}

func sortStrings(s []string) {
	for i := range s {
		for j := i + 1; j < len(s); j++ {
			if s[j] < s[i] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}
