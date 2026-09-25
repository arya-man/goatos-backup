// Package animalvocab is THE answer to "which species and which sexes does this farm keep", for
// every module that stores an animal's species or sex (the herd register, procurement loads,
// animal purchases, births, sale price assumptions, protocol selectors and the web/phone pickers).
//
// Both lists are authored on Configuration > Items & settings (species_lookup / sex_lookup,
// migration 000346): tenant rows with a lower_snake code, a display name, an order and a status.
// OPEN UP TO NEW SPECIES (maintainer decision 2026-09-25): a species or sex added there is usable
// everywhere at once -- every dropdown lists it and every write accepts it -- while a code no ACTIVE
// row carries is refused. Before this package several modules answered the question with a
// goat/sheep and female/male constant, so a species added on Configuration showed on the
// Configuration screen and was refused by every write.
//
// A new species simply has no vaccination schedule, sale price or feed rule until one is authored
// for it; that is an honest boundary, not a defect. Existing data is goat/sheep and female/male
// and keeps working byte for byte: those four rows are built in and can never be archived.
//
// A TENANT WITH NO LOOKUP ROWS AT ALL (an integration fixture that never ran the seed) reads the
// four built-ins, the same fallback identity's write path has used since 000346, so an
// unconfigured tenant behaves exactly as before rather than refusing every animal.
package animalvocab

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
)

// ActiveSpeciesSQL lists the tenant's active species in picker order, or the two built-ins when
// the tenant has no species rows at all.
//
// Parameters: $1 tenant_id.
const ActiveSpeciesSQL = `
WITH l AS (
  SELECT species_code AS code, name, sort_order, status
  FROM species_lookup
  WHERE tenant_id = $1::uuid
)
SELECT code, name FROM (
  SELECT code, btrim(name) AS name, sort_order FROM l WHERE status = 'active'
  UNION ALL
  SELECT v.code, v.name, v.sort_order
  FROM (VALUES ('goat', 'Goat', 10), ('sheep', 'Sheep', 20)) AS v(code, name, sort_order)
  WHERE NOT EXISTS (SELECT 1 FROM l)
) s
ORDER BY sort_order, lower(name), code
LIMIT 200`

// ActiveSexesSQL lists the tenant's active sexes in picker order, or the two built-ins when the
// tenant has no sex rows at all.
//
// Parameters: $1 tenant_id.
const ActiveSexesSQL = `
WITH l AS (
  SELECT sex_code AS code, name, sort_order, status
  FROM sex_lookup
  WHERE tenant_id = $1::uuid
)
SELECT code, name FROM (
  SELECT code, btrim(name) AS name, sort_order FROM l WHERE status = 'active'
  UNION ALL
  SELECT v.code, v.name, v.sort_order
  FROM (VALUES ('female', 'Female', 10), ('male', 'Male', 20)) AS v(code, name, sort_order)
  WHERE NOT EXISTS (SELECT 1 FROM l)
) s
ORDER BY sort_order, lower(name), code
LIMIT 200`

// Entry is one active species or sex: the code stored on an animal and the name a screen shows.
type Entry struct {
	Code string
	Name string
}

// Querier is the subset of a pgx pool or transaction the readers need.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// ListSpecies returns the tenant's active species.
func ListSpecies(ctx context.Context, q Querier, tenantID string) ([]Entry, error) {
	rows, err := q.Query(ctx, ActiveSpeciesSQL, strings.TrimSpace(tenantID))
	return scan(rows, err, "species")
}

// ListSexes returns the tenant's active sexes.
func ListSexes(ctx context.Context, q Querier, tenantID string) ([]Entry, error) {
	rows, err := q.Query(ctx, ActiveSexesSQL, strings.TrimSpace(tenantID))
	return scan(rows, err, "sexes")
}

func scan(rows pgx.Rows, err error, what string) ([]Entry, error) {
	if err != nil {
		return nil, fmt.Errorf("animalvocab: list %s: %w", what, err)
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.Code, &e.Name); err != nil {
			return nil, fmt.Errorf("animalvocab: scan %s: %w", what, err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("animalvocab: list %s: %w", what, err)
	}
	return out, nil
}

// Vocabulary is a tenant's two lists, read together for a write path that checks both.
type Vocabulary struct {
	Species []Entry
	Sexes   []Entry
}

// Load reads both lists.
func Load(ctx context.Context, q Querier, tenantID string) (Vocabulary, error) {
	species, err := ListSpecies(ctx, q, tenantID)
	if err != nil {
		return Vocabulary{}, err
	}
	sexes, err := ListSexes(ctx, q, tenantID)
	if err != nil {
		return Vocabulary{}, err
	}
	return Vocabulary{Species: species, Sexes: sexes}, nil
}

// Builtins is the vocabulary an unconfigured tenant reads, for callers with no database (unit
// tests, a nil port) that must still behave exactly as before this package.
func Builtins() Vocabulary {
	return Vocabulary{
		Species: []Entry{{Code: "goat", Name: "Goat"}, {Code: "sheep", Name: "Sheep"}},
		Sexes:   []Entry{{Code: "female", Name: "Female"}, {Code: "male", Name: "Male"}},
	}
}

// Has reports whether code is exactly one of the entries' codes, as stored.
func Has(entries []Entry, code string) bool {
	for _, e := range entries {
		if e.Code == code {
			return true
		}
	}
	return false
}

// Resolve matches raw against the entries ignoring case and surrounding space, by code first and
// then by name ("Sheep" or "sheep"), returning the stored code.
func Resolve(entries []Entry, raw string) (code string, ok bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false
	}
	for _, e := range entries {
		if strings.EqualFold(e.Code, trimmed) {
			return e.Code, true
		}
	}
	for _, e := range entries {
		if strings.EqualFold(strings.TrimSpace(e.Name), trimmed) {
			return e.Code, true
		}
	}
	return "", false
}

// Codes returns the entries' codes in list order.
func Codes(entries []Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Code)
	}
	return out
}

// Label is the entry's name for code, or the code made readable when no active entry carries it
// (an archived species still names the animals recorded under it).
func Label(entries []Entry, code string) string {
	for _, e := range entries {
		if e.Code == code && strings.TrimSpace(e.Name) != "" {
			return e.Name
		}
	}
	return strings.ReplaceAll(strings.TrimSpace(code), "_", " ")
}

// codePattern is the shape of a species / sex code (the lookups' own CHECK, migration 000346).
var codePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

// ValidCodeShape reports whether code has the shape a lookup code can have. It is the check a
// caller with no database access makes; membership is still the lookup's answer.
func ValidCodeShape(code string) bool { return codePattern.MatchString(code) }

// ReasonUnknownSpecies and ReasonUnknownSex are the farm-worded refusals every module returns for
// a code no active row carries.
const (
	ReasonUnknownSpecies = "Pick one of the farm's species (Configuration > Items & settings)."
	ReasonUnknownSex     = "Pick one of the farm's genders (Configuration > Items & settings)."
)
