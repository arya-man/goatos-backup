package postgres

import (
	"strconv"
	"testing"

	"github.com/vgoats/goatos/backend/internal/economics/domain"
)

// The sold query's LIMIT is a string const (the query itself is a const); this
// pins it to the domain cap so the two cannot drift apart silently. The animal
// table's cap is applied in Go against domain.MaxAnimalRows directly.
func TestRowCapLiteralsMatchDomain(t *testing.T) {
	if maxSoldRowsSQL != strconv.Itoa(domain.MaxSoldRows) {
		t.Fatalf("maxSoldRowsSQL %q != domain.MaxSoldRows %d", maxSoldRowsSQL, domain.MaxSoldRows)
	}
}
