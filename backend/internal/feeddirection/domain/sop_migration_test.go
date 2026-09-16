package domain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMigrationEmbedsTheSeededFeedSOP pins migration 000327 to the embedded seeds: the section
// it adds in place to each tenant's published feed.* version is the same bytes the code compiles
// for a tenant with no authored version, so day one on STG is the current behaviour exactly.
func TestMigrationEmbedsTheSeededFeedSOP(t *testing.T) {
	path := filepath.Join("..", "..", "..", "migrations", "postgres", "000327_feed_sop_cards.sql")
	sql, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{SOPCodeFeedDirection, SOPCodeFeedPacking, SOPCodeFeedTransport} {
		want := "$seed$" + strings.TrimSpace(string(SeededFeedSOPJSON(code))) + "$seed$"
		if got := strings.Count(string(sql), want); got != 1 {
			t.Fatalf("migration embeds the seeded %s document verbatim %d time(s), want 1", code, got)
		}
	}
}
