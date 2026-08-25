package postgres

import (
	"strconv"
	"testing"

	"github.com/vgoats/goatos/backend/internal/economics/domain"
)

// The query LIMITs are string consts (the queries themselves are consts); this
// pins them to the domain caps so the two cannot drift apart silently.
func TestRowCapLiteralsMatchDomain(t *testing.T) {
	if maxAnimalRowsSQL != strconv.Itoa(domain.MaxAnimalRows) {
		t.Fatalf("maxAnimalRowsSQL %q != domain.MaxAnimalRows %d", maxAnimalRowsSQL, domain.MaxAnimalRows)
	}
	if maxSoldRowsSQL != strconv.Itoa(domain.MaxSoldRows) {
		t.Fatalf("maxSoldRowsSQL %q != domain.MaxSoldRows %d", maxSoldRowsSQL, domain.MaxSoldRows)
	}
}
