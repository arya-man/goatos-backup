// Command seed-shed-capacity loads the configured HOLDING CAPACITY of every operational location
// from the committed Sheds DB fixture.
//
// It is the capacity twin of seed-shed-profiles, which derives a shed's COHORT from resident
// animals. Capacity cannot be derived that way -- how many animals a pen was built to hold is a
// fact about the building, not about who is standing in it today -- so it comes from the farm's
// own Sheds DB sheet, captured into fixtures/shed-capacity-<date>/shed-capacity.json.
//
// GRAIN. Capacity is written at the grain the farm records it: a PEN's capacity goes to
// shed_partitions.capacity (migration 000160), and a shed with NO pens at all (Q1, Q2, Q3,
// Ho Chi Minh) keeps its capacity on shed_profiles.capacity. Those are the only two cases, and the
// fixture states which one each row is by whether partition_label is empty.
//
// The fixture is the AUDITABLE record of how the sheet was mapped: each row carries the sheet rows
// summed into it (`source_rows`), and rows the pen catalog could not accept are listed in
// `source.unmapped_source_rows` and PRINTED on every run rather than dropped in silence. The farm's
// sheet is a superset of the catalog in two places today -- CBE Godel 1 lists ten pens where the
// catalog has eight, and CPT has no Ho Chi Minh -- and that is a data question for the maintainer,
// not something this command should paper over by inventing pens.
//
// RESOLUTION is by (park location_code, shed name) against active, non-retired shed locations, with
// legacy partition-alias rows excluded through the shared oploc.PartitionAliasExclusionSQL -- the
// farm's pens exist a second time in `locations` as rows literally named "Castro 1", and matching
// one of those would write a pen's capacity onto a row no screen reads. A name that resolves to
// zero or to more than one shed FAILS the seed rather than guessing; so does a pen the catalog does
// not have, and so does an unknown fixture key.
//
// Idempotent (writes keyed by the target row's own primary key, row_version bumped only when the
// capacity actually changes), tenant-scoped, and gated to local/dev/test/stg like the sibling
// seeds. It writes exactly one column of each of two tables.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/localtarget"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

const (
	defaultTenantID = "00000000-0000-4000-8000-000000000001"
	defaultFixture  = "fixtures/shed-capacity-2026-08-14/shed-capacity.json"
)

type fixtureFile struct {
	Source fixtureSource `json:"source"`
	Sheds  []fixtureRow  `json:"sheds"`
}

// fixtureSource is provenance. It is declared so DisallowUnknownFields does not reject the block,
// and so a reader can tell which capture of the sheet a row came from and what that capture could
// not place.
type fixtureSource struct {
	SpreadsheetID string `json:"spreadsheet_id"`
	Tab           string `json:"tab"`
	CapturedOn    string `json:"captured_on"`
	GrainNote     string `json:"grain_note"`
	// UnmappedSourceRows are sheet rows no operational location could accept. Printed on every
	// run: a capacity the farm recorded and the catalog cannot hold is a data question, not noise.
	UnmappedSourceRows []string `json:"unmapped_source_rows"`
}

type fixtureRow struct {
	ParkCode string `json:"park_code"`
	ShedName string `json:"shed_name"`
	// PartitionLabel is the pen's HUMAN label ("Part 3", "2"), empty for a shed with no pens.
	// Never normalized_label -- that is a matching key and never travels in a fixture or a screen.
	PartitionLabel string `json:"partition_label"`
	Capacity       int    `json:"capacity"`
	// SourceRows names the sheet rows summed into Capacity. Audit trail, not input.
	SourceRows []string `json:"source_rows"`
}

// resolveShedsSQL resolves EVERY (park code, shed name) pair in the fixture in one round trip.
//
// Set-based on purpose: a per-shed query in a loop is the N+1 shape the repo bans, and the guard
// makes no exception for a seed. It returns every match rather than one per pair, so an ambiguous
// name is visible to the caller and FAILS the seed -- picking one of two sheds would silently
// write a capacity onto the wrong building.
var resolveShedsSQL = `
SELECT want.park_code, want.shed_name, l.location_id::text
FROM unnest($2::text[], $3::text[]) AS want(park_code, shed_name)
JOIN locations park
  ON park.tenant_id = $1::uuid
 AND park.location_type = 'park'
 AND park.status = 'active'
 AND park.location_code = want.park_code
JOIN locations l
  ON l.tenant_id = park.tenant_id
 AND l.parent_location_id = park.location_id
 AND l.location_type = 'shed'
 AND l.status = 'active'
 AND l.retired_at IS NULL
 AND BTRIM(l.name) = BTRIM(want.shed_name)
 AND ` + oploc.PartitionAliasExclusionSQL("l")

// upsertShedCapacitySQL writes the capacity of a shed that has NO pens, in one statement.
//
// row_version bumps only on an actual change and the WHERE clause skips an unchanged row entirely,
// so re-running the seed reports zero applied rather than churning versions that would read to a
// later reviewer as repeated reconfiguration.
const upsertShedCapacitySQL = `
INSERT INTO shed_profiles (location_id, tenant_id, capacity, row_version)
SELECT incoming.location_id::uuid, $1::uuid, incoming.capacity, 1
FROM unnest($2::text[], $3::int[]) AS incoming(location_id, capacity)
ON CONFLICT (location_id) DO UPDATE
  SET capacity    = EXCLUDED.capacity,
      row_version = shed_profiles.row_version
                  + CASE WHEN shed_profiles.capacity IS DISTINCT FROM EXCLUDED.capacity THEN 1 ELSE 0 END,
      updated_at  = now()
WHERE shed_profiles.capacity IS DISTINCT FROM EXCLUDED.capacity`

// updatePenCapacitySQL writes each pen's capacity onto its EXISTING catalog row.
//
// UPDATE, never INSERT: shed_partitions is the catalog of pens that physically exist, and this
// command's job is to record a capacity for a pen the farm already has -- not to create pens from
// a spreadsheet. A fixture row naming a pen the catalog lacks is rejected by assertPensExist, which
// is how CBE Godel 1's Part 9 and Part 10 stay visible instead of being conjured.
//
// Matching is on partition_label, the human label, because that is what the fixture carries; the
// BTRIM keeps a stray space in either source from silently missing.
const updatePenCapacitySQL = `
UPDATE shed_partitions sp
SET capacity   = incoming.capacity,
    updated_at = now()
FROM unnest($2::text[], $3::text[], $4::int[]) AS incoming(shed_id, partition_label, capacity)
WHERE sp.tenant_id = $1::uuid
  AND sp.shed_id = incoming.shed_id::uuid
  AND BTRIM(sp.partition_label) = BTRIM(incoming.partition_label)
  AND sp.status = 'active'
  AND sp.capacity IS DISTINCT FROM incoming.capacity`

// countPenMatchesSQL reports how many catalog rows each fixture pen matches, so a pen the catalog
// does not have fails the seed instead of silently updating nothing.
//
// projection-review: membership=the fixture's own (shed_id, partition_label) pairs, unnested as the
// LEFT side so a pair matching NOTHING still returns a row with count 0 -- an INNER join would drop
// exactly the pens this check exists to catch; group_key=(incoming.shed_id,
// incoming.partition_label), the same pair the caller looks the result up by, so a count can never
// be attributed to a different pen; join_cardinality=1:{0,1} because shed_partitions is unique on
// (tenant_id, shed_id, normalized_label) and the predicate pins tenant, shed and label, so a count
// above 1 means duplicate catalog rows and is reported rather than silently accepted;
// pagination=none, one bounded pass over the fixture's ~105 pens at seed time; scope=tenant_id on
// shed_partitions plus status='active', matching the write's own predicate exactly.
const countPenMatchesSQL = `
SELECT incoming.shed_id, incoming.partition_label, count(sp.shed_id)
FROM unnest($2::text[], $3::text[]) AS incoming(shed_id, partition_label)
LEFT JOIN shed_partitions sp
  ON sp.tenant_id = $1::uuid
 AND sp.shed_id = incoming.shed_id::uuid
 AND BTRIM(sp.partition_label) = BTRIM(incoming.partition_label)
 AND sp.status = 'active'
GROUP BY incoming.shed_id, incoming.partition_label`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("seed-shed-capacity", flag.ContinueOnError)
	tenantID := fs.String("tenant-id", getenv("GOATOS_TENANT_ID", defaultTenantID), "tenant id")
	fixturePath := fs.String("fixture", getenv("GOATOS_SHED_CAPACITY_FIXTURE", defaultFixture), "capacity fixture path")
	timeout := fs.Duration("timeout", 120*time.Second, "seed timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}

	fixture, err := loadFixture(*fixturePath)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	pgCfg := platformpg.ConfigFromEnv()
	if err := validateTarget(os.Getenv("GOATOS_ENV"), pgCfg.DatabaseURL); err != nil {
		return err
	}
	pool, err := platformpg.Connect(ctx, pgCfg)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer pool.Close()

	// DEDUPED before the query: a shed now contributes one fixture row per pen, so sending the
	// pairs verbatim would hand the resolver ten identical (CBE, Godel 1) pairs and make a single
	// shed look like ten sheds sharing a name -- tripping the ambiguity check on correct data.
	parkCodes, shedNames := []string{}, []string{}
	seenPair := map[string]bool{}
	for _, row := range fixture.Sheds {
		if row.Capacity < 0 {
			return fmt.Errorf("%s/%s: capacity %d is negative", row.ParkCode, operationalLabel(row), row.Capacity)
		}
		key := row.ParkCode + "\x00" + row.ShedName
		if seenPair[key] {
			continue
		}
		seenPair[key] = true
		parkCodes = append(parkCodes, row.ParkCode)
		shedNames = append(shedNames, row.ShedName)
	}

	shedIDByPair, err := resolveSheds(ctx, pool, *tenantID, parkCodes, shedNames)
	if err != nil {
		return err
	}
	locationIDs := make([]string, len(fixture.Sheds))
	for i, row := range fixture.Sheds {
		locationIDs[i] = shedIDByPair[row.ParkCode+"\x00"+row.ShedName]
	}

	// Split by grain: a pen's capacity belongs to the pen catalog, a pen-less shed's to its
	// profile. The two are never both written for one location.
	shedIDs, shedCaps := []string{}, []int32{}
	penShedIDs, penLabels, penCaps := []string{}, []string{}, []int32{}
	for i, row := range fixture.Sheds {
		if row.PartitionLabel == "" {
			shedIDs = append(shedIDs, locationIDs[i])
			shedCaps = append(shedCaps, int32(row.Capacity))
			continue
		}
		penShedIDs = append(penShedIDs, locationIDs[i])
		penLabels = append(penLabels, row.PartitionLabel)
		penCaps = append(penCaps, int32(row.Capacity))
	}

	if err := assertPensExist(ctx, pool, *tenantID, fixture, locationIDs, penShedIDs, penLabels); err != nil {
		return err
	}

	var appliedSheds, appliedPens int64
	if len(shedIDs) > 0 {
		tag, execErr := pool.Exec(ctx, upsertShedCapacitySQL, *tenantID, shedIDs, shedCaps)
		if execErr != nil {
			return fmt.Errorf("upsert shed capacity: %w", execErr)
		}
		appliedSheds = tag.RowsAffected()
	}
	if len(penShedIDs) > 0 {
		tag, execErr := pool.Exec(ctx, updatePenCapacitySQL, *tenantID, penShedIDs, penLabels, penCaps)
		if execErr != nil {
			return fmt.Errorf("update pen capacity: %w", execErr)
		}
		appliedPens = tag.RowsAffected()
	}

	// Coverage is reported over REAL operational locations only. The unpartitioned-shed denominator
	// applies the same alias exclusion the resolver does -- without it the legacy "Castro 1" rows
	// (which have no pens of their own) count as pen-less sheds and the line reads 4/109 for a farm
	// with four such sheds.
	//
	// projection-review: membership=active pens (shed_partitions, status='active') for the pen
	// counters, and active non-retired shed locations with NO active pen and minus legacy partition
	// aliases for the shed counters -- the same two populations this command writes to, so the
	// report cannot claim coverage of rows it never targets; group_key=none, these are four scalar
	// counts over disjoint populations and nothing is grouped or joined across them (the pen
	// subqueries touch only shed_partitions; the shed counts scan locations LEFT JOINed to
	// shed_profiles); join_cardinality=shed_profiles is 1:{0,1} on its location_id PRIMARY KEY, so
	// the LEFT JOIN cannot fan a shed into two rows and count(*) stays a shed count; pagination=none,
	// this is a one-shot post-seed report over a bounded configuration catalog, not a request path
	// or a paged read; scope=tenant_id on every subquery and on the outer scan. Numerator and
	// denominator range over the SAME key set in both pairs -- pens over pens, pen-less sheds over
	// pen-less sheds -- so neither ratio can exceed 1.
	var pensTotal, pensWithCapacity, shedsNoPens, shedsNoPensWithCapacity int
	coverageSQL := `
SELECT (SELECT count(*) FROM shed_partitions WHERE tenant_id=$1::uuid AND status='active'),
       (SELECT count(*) FROM shed_partitions WHERE tenant_id=$1::uuid AND status='active' AND capacity IS NOT NULL),
       count(*),
       count(*) FILTER (WHERE p.capacity IS NOT NULL)
FROM locations l
LEFT JOIN shed_profiles p ON p.tenant_id = l.tenant_id AND p.location_id = l.location_id
WHERE l.tenant_id = $1::uuid AND l.location_type = 'shed' AND l.status = 'active' AND l.retired_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM shed_partitions sp WHERE sp.tenant_id=l.tenant_id AND sp.shed_id=l.location_id AND sp.status='active')
  AND ` + oploc.PartitionAliasExclusionSQL("l")
	if err := pool.QueryRow(ctx, coverageSQL, *tenantID).Scan(&pensTotal, &pensWithCapacity, &shedsNoPens, &shedsNoPensWithCapacity); err != nil {
		return fmt.Errorf("count coverage: %w", err)
	}

	fmt.Printf("seed-shed-capacity: applied_pens=%d applied_sheds=%d | pens_with_capacity=%d/%d unpartitioned_sheds_with_capacity=%d/%d source=%s@%s\n",
		appliedPens, appliedSheds, pensWithCapacity, pensTotal, shedsNoPensWithCapacity, shedsNoPens,
		fixture.Source.Tab, fixture.Source.CapturedOn)
	for _, unmapped := range fixture.Source.UnmappedSourceRows {
		fmt.Printf("seed-shed-capacity: UNMAPPED sheet row (no operational location to hold it): %s\n", unmapped)
	}
	return nil
}

// operationalLabel renders a fixture row the way the farm reads it, for error messages.
func operationalLabel(row fixtureRow) string {
	return oploc.OperationalLocation{ShedName: row.ShedName, PartitionLabel: row.PartitionLabel}.Display()
}

// resolveSheds maps each DISTINCT fixture (park code, shed name) pair to its shed location_id.
//
// It fails on a pair that matches no active shed and on a pair that matches more than one: writing
// a capacity onto the wrong building is worse than not writing it, and a name the farm has renamed
// must be noticed rather than skipped. It returns a MAP rather than a positional slice on purpose
// -- the caller has many fixture rows per shed, and a positional result would have to be expanded
// back out by index, which is exactly the parallel-array shift this repo keeps getting bitten by.
func resolveSheds(ctx context.Context, pool *pgxpool.Pool, tenantID string, parkCodes, shedNames []string) (map[string]string, error) {
	rows, err := pool.Query(ctx, resolveShedsSQL, tenantID, parkCodes, shedNames)
	if err != nil {
		return nil, fmt.Errorf("resolve sheds: %w", err)
	}
	defer rows.Close()

	matches := map[string][]string{}
	for rows.Next() {
		var parkCode, shedName, locationID string
		if err := rows.Scan(&parkCode, &shedName, &locationID); err != nil {
			return nil, fmt.Errorf("resolve sheds scan: %w", err)
		}
		key := parkCode + "\x00" + shedName
		matches[key] = append(matches[key], locationID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("resolve sheds: %w", err)
	}

	out := make(map[string]string, len(parkCodes))
	for i := range parkCodes {
		key := parkCodes[i] + "\x00" + shedNames[i]
		found := matches[key]
		switch len(found) {
		case 1:
			out[key] = found[0]
		case 0:
			return nil, fmt.Errorf("%s/%s: no active shed with that name in that park", parkCodes[i], shedNames[i])
		default:
			return nil, fmt.Errorf("%s/%s: %d active sheds share that name; capacity cannot be attributed", parkCodes[i], shedNames[i], len(found))
		}
	}
	return out, nil
}

// assertPensExist fails the seed when a fixture pen has no catalog row.
//
// Without this the UPDATE would simply match nothing and the run would report success while that
// pen's capacity silently went nowhere -- the accept-and-discard failure the repo's fixture rule
// exists to stop, one layer below the JSON decoder.
func assertPensExist(ctx context.Context, pool *pgxpool.Pool, tenantID string, fixture fixtureFile, locationIDs, penShedIDs, penLabels []string) error {
	if len(penShedIDs) == 0 {
		return nil
	}
	rows, err := pool.Query(ctx, countPenMatchesSQL, tenantID, penShedIDs, penLabels)
	if err != nil {
		return fmt.Errorf("verify pens: %w", err)
	}
	defer rows.Close()

	counts := map[string]int64{}
	for rows.Next() {
		var shedID, label string
		var count int64
		if err := rows.Scan(&shedID, &label, &count); err != nil {
			return fmt.Errorf("verify pens scan: %w", err)
		}
		counts[shedID+"\x00"+label] = count
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("verify pens: %w", err)
	}

	// Reported against the FIXTURE row, not the shed id, so the error names something a person can
	// find in the sheet.
	for i, row := range fixture.Sheds {
		if row.PartitionLabel == "" {
			continue
		}
		if counts[locationIDs[i]+"\x00"+row.PartitionLabel] == 0 {
			return fmt.Errorf("%s/%s: the pen catalog has no active pen by that label; capacity cannot be recorded",
				row.ParkCode, operationalLabel(row))
		}
	}
	return nil
}

// fixtureCandidates returns the path as given, then the same path one directory up.
//
// The default is repo-root-relative, but seed-closeout runs commands from backend/. Trying the
// parent is a two-line courtesy that turns a confusing "no such file" into a working default;
// anything beyond one level would be guessing at the caller's tree.
func fixtureCandidates(path string) []string {
	if filepath.IsAbs(path) {
		return []string{path}
	}
	return []string{path, filepath.Join("..", path)}
}

// loadFixture reads the capture and REJECTS an unknown field.
//
// encoding/json drops unmatched keys silently, so a fixture whose shape has drifted would seed
// partially and still report success -- the accept-and-discard failure the repo's fixture rule
// exists to stop.
func loadFixture(path string) (fixtureFile, error) {
	var raw []byte
	var err error
	for _, candidate := range fixtureCandidates(path) {
		raw, err = os.ReadFile(candidate)
		if err == nil {
			break
		}
	}
	if err != nil {
		return fixtureFile{}, err
	}
	var f fixtureFile
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return fixtureFile{}, fmt.Errorf("decode %s: %w", path, err)
	}
	if len(f.Sheds) == 0 {
		return fixtureFile{}, fmt.Errorf("%s contains no sheds", path)
	}
	for _, row := range f.Sheds {
		if row.ParkCode == "" || row.ShedName == "" {
			return fixtureFile{}, fmt.Errorf("%s: a row is missing park_code or shed_name", path)
		}
	}
	return f, nil
}

func validateTarget(env, databaseURL string) error {
	if env == "stg" {
		return localtarget.ValidateStagingCloudSQLDatabaseTarget("seed-shed-capacity", env, databaseURL)
	}
	return localtarget.ValidateLocalDatabaseTarget("seed-shed-capacity", env, databaseURL, "local", "dev", "test")
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
