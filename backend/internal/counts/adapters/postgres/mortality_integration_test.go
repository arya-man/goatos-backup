package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Adversarial coverage for the Mortality aggregate. Every test targets a way this read can
// report a WRONG NUMBER rather than fail loudly: a death counted differently from Herd
// Analytics, a cause landing in two bases, a band boundary the SQL draws one day off the Go
// rule the labels come from, or a rate whose denominator is not the bucket's own live head count.

// insertMortalityGoat seeds one animal with every attribute the mortality read slices on.
// exitedOn empty = still alive.
func insertMortalityGoat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id, breed, sex, stage, ageBand, dob, origin, exitReason, exitedOn string) {
	t.Helper()
	var reason, exitedAt *string
	lifecycle := "alive"
	if exitReason != "" {
		reason = &exitReason
		exitedAt = &exitedOn
		switch exitReason {
		case "died":
			lifecycle = "dead"
		default:
			lifecycle = exitReason
		}
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO goats (
  goat_id, tenant_id, display_id, species, breed, sex, lifecycle_status, age_band, dob, origin_type,
  custodian_party_id, park_id, shed_id, management_stage, exit_reason, exited_at
) VALUES (
  $1::uuid, $2::uuid, $3, 'goat', $4, $5, $6, $7, $8::date, $9,
  '00000000-0000-4000-8000-000000001001'::uuid, $10::uuid, $11::uuid, $12, $13,
  CASE WHEN $14::text IS NULL THEN NULL ELSE ($14::date + time '10:00') AT TIME ZONE 'Asia/Kolkata' END
)`, id, countsTenant, "G-"+id[len(id)-6:], breed, sex, lifecycle, ageBand, dob, origin, countsPark, countsShedA, stage, reason, exitedAt); err != nil {
		t.Fatalf("seed goat %s: %v", id, err)
	}
}

func mortalityGoatID(n int) string { return fmt.Sprintf("40000000-0000-4000-8000-0000000%05d", n) }

func istDate(t time.Time) string { return t.In(biztime.DefaultLocation()).Format("2006-01-02") }

func TestMortalitySyntheticLoadLabelsUseBusinessBuckets(t *testing.T) {
	if got := loadBucketLabel("farm_born", ""); got != "Farm born" {
		t.Fatalf("farm_born label=%q, want Farm born", got)
	}
	if got := loadBucketLabel("no_load", ""); got != "Farm born" {
		t.Fatalf("no_load label=%q, want Farm born; missing procurement_load_goats membership must not surface a scary third bucket", got)
	}
	if got := loadBucketLabel("11111111-1111-4111-8111-111111111111", "126"); got != "Load 126" {
		t.Fatalf("real load label=%q, want Load 126", got)
	}
}

// THE PARITY LOCK. Mortality and Herd Analytics count a death off the same row with the same
// predicate; the Deaths tile must be the same number on both screens for the same window and
// scope. Cross-surface disagreement about a business number is a maintainer question, so the
// two reads are pinned together here rather than each trusted alone.
func TestMortalityMultipleDimensionsMatchHerdAnalyticsAndPartitionAnimals(t *testing.T) {
	ctx := context.Background()
	repo, pool := newHerdAnalyticsRepo(t, ctx)
	from, to := monthsBack(2), thisMonthDay(1)
	died := thisMonthDay(1)

	// Two deaths by exit_reason, one by the lifecycle fallback (NULL reason, status dead), one
	// sale that must NOT count, and two live animals.
	insertMortalityGoat(t, ctx, pool, mortalityGoatID(1), "Beetal", "female", "K1", "kid", istDayMinus(died, 5), "birth", "died", died)
	insertMortalityGoat(t, ctx, pool, mortalityGoatID(2), "Sirohi", "male", "F2-Male", "adult", istDayMinus(died, 500), "procured", "died", died)
	insertExitedGoat(t, ctx, pool, mortalityGoatID(3), "G-900003", "dead", "", died)
	insertMortalityGoat(t, ctx, pool, mortalityGoatID(4), "Beetal", "male", "F2-Male", "adult", istDayMinus(died, 400), "procured", "sold", died)
	insertMortalityGoat(t, ctx, pool, mortalityGoatID(5), "Beetal", "female", "Non-Pregnant", "adult", istDayMinus(died, 700), "procured", "", "")
	insertMortalityGoat(t, ctx, pool, mortalityGoatID(6), "Sirohi", "female", "K2", "kid", istDayMinus(died, 40), "birth", "", "")

	herd, err := repo.GetHerdAnalytics(ctx, domain.HerdAnalyticsQuery{TenantID: countsTenant, FromDate: from, ToDate: to})
	if err != nil {
		t.Fatalf("herd analytics: %v", err)
	}
	mort, err := repo.GetMortality(ctx, domain.MortalityQuery{TenantID: countsTenant, FromDate: from, ToDate: to})
	if err != nil {
		t.Fatalf("mortality: %v", err)
	}
	if mort.Totals.Deaths != 3 || herd.Totals.Deaths != mort.Totals.Deaths {
		t.Fatalf("deaths: mortality=%d herd analytics=%d, want 3 on both", mort.Totals.Deaths, herd.Totals.Deaths)
	}
	// Animals = the 2 LIVE animals, the same head count Counts Breakdown reports; the dead and
	// the sold are not in the denominator (maintainer decision 2026-09-18).
	if mort.Totals.Animals != 2 {
		t.Fatalf("animals=%d want 2", mort.Totals.Animals)
	}
	if mort.Totals.RatePct == nil || *mort.Totals.RatePct != 150.0 {
		t.Fatalf("rate=%v want 150.0 (3 deaths against 2 live animals)", mort.Totals.RatePct)
	}
	if mort.Totals.KidDeaths+mort.Totals.AdultDeaths != mort.Totals.Deaths {
		t.Fatalf("kids %d + adults %d != deaths %d", mort.Totals.KidDeaths, mort.Totals.AdultDeaths, mort.Totals.Deaths)
	}
	// Every RATE series partitions both deaths and the live head count exactly.
	for name, series := range map[string][]domain.MortalityBucket{"stage": mort.Stage, "breed": mort.Breed, "sex": mort.Sex, "kid_adult": mort.KidAdult, "park": mort.Park, "load": mort.Load, "vendor": mort.Vendor} {
		var deaths, animals int64
		for _, b := range series {
			deaths += b.Deaths
			animals += b.Animals
		}
		if deaths != mort.Totals.Deaths || animals != mort.Totals.Animals {
			t.Fatalf("%s series sums deaths=%d animals=%d, want %d/%d: %+v", name, deaths, animals, mort.Totals.Deaths, mort.Totals.Animals, series)
		}
	}
	// The breed rate divides by THAT breed's own live animals, never the whole herd: Beetal has
	// 2 deaths (one by reason, the fallback row is Beetal too) against 1 live animal; Sirohi has
	// 1 death against 1 live animal.
	want := map[string][3]float64{"Beetal": {2, 1, 200.0}, "Sirohi": {1, 1, 100.0}}
	for _, b := range mort.Breed {
		w, ok := want[b.Key]
		if !ok {
			t.Fatalf("unexpected breed bucket %+v", b)
		}
		if float64(b.Deaths) != w[0] || float64(b.Animals) != w[1] || b.RatePct == nil || *b.RatePct != w[2] {
			t.Fatalf("%s bucket %+v, want %v", b.Key, b, w)
		}
	}
	// A pen bucket carries the backend-composed operational location, never an id, and only
	// pens that saw a death are listed.
	if len(mort.Pen) != 1 || mort.Pen[0].Label != "CPT Shed 1" || mort.Pen[0].Deaths != 3 {
		t.Fatalf("pen series %+v, want one CPT Shed 1 row with 3 deaths", mort.Pen)
	}
	// The month spine covers the window and the kid/adult split rides each month.
	if len(mort.Months) != 3 {
		t.Fatalf("months=%d want 3 (a month-boundary window two months back)", len(mort.Months))
	}
	var monthDeaths int64
	for _, m := range mort.Months {
		monthDeaths += m.Deaths
		if m.Kids+m.Adults != m.Deaths {
			t.Fatalf("month %s kids %d + adults %d != deaths %d", m.Month, m.Kids, m.Adults, m.Deaths)
		}
	}
	if monthDeaths != mort.Totals.Deaths {
		t.Fatalf("months sum %d != deaths %d", monthDeaths, mort.Totals.Deaths)
	}
}

// THE BAND LOCK. The SQL draws the age band, days-since and season boundaries itself (so the
// grouping happens in the database) while the LABELS come from the Go rules in the domain. A
// boundary one day off between the two would put a death in one band on the chart and name
// it another in the recent list. Seed one death exactly ON each boundary and prove the SQL's
// bucket is the Go rule's bucket.
func TestMortalityBandKeysMatchDomain(t *testing.T) {
	ctx := context.Background()
	repo, pool := newHerdAnalyticsRepo(t, ctx)
	// A fixed death day in the monsoon so the season is known, inside a fixed window.
	died := "2026-07-15"
	from, to := "2026-07-01", "2026-07-31"

	ages := []int64{0, 7, 8, 30, 31, 90, 91, 180, 181, 365, 366}
	for i, age := range ages {
		insertMortalityGoat(t, ctx, pool, mortalityGoatID(100+i), "Beetal", "female", "K1", "kid", istDayMinus(died, int(age)), "birth", "died", died)
	}
	// One death with no date of birth at all.
	if _, err := pool.Exec(ctx, `UPDATE goats SET dob = NULL WHERE goat_id = $1::uuid`, mortalityGoatID(100)); err != nil {
		t.Fatalf("clear dob: %v", err)
	}

	mort, err := repo.GetMortality(ctx, domain.MortalityQuery{TenantID: countsTenant, FromDate: from, ToDate: to})
	if err != nil {
		t.Fatalf("mortality: %v", err)
	}
	want := map[string]int64{"unknown": 1}
	for _, age := range ages[1:] {
		a := age
		want[domain.MortalityAgeBandKey(&a)]++
	}
	got := map[string]int64{}
	for _, b := range mort.AgeAtDeath {
		got[b.Key] = b.Deaths
		if b.Label != domain.MortalityAgeBandLabel(b.Key) {
			t.Fatalf("band %q label %q is not the domain label", b.Key, b.Label)
		}
	}
	for key, n := range want {
		if got[key] != n {
			t.Fatalf("band %s: sql=%d go=%d (all: %v)", key, got[key], n, got)
		}
	}
	// Every death in July lands in monsoon and nowhere else; the other seasons are explicit zeros.
	seasons := map[string]int64{}
	for _, b := range mort.Season {
		seasons[b.Key] = b.Deaths
	}
	if seasons["monsoon"] != int64(len(ages)) || seasons["summer"] != 0 || seasons["winter"] != 0 || seasons["post_monsoon"] != 0 || len(mort.Season) != 4 {
		t.Fatalf("season series %+v, want %d in monsoon and explicit zeros elsewhere", mort.Season, len(ages))
	}
	if mort.Totals.FirstWeekDeaths != 1 {
		// Only the age-7 death: the age-0 animal had its dob cleared and is "unknown".
		t.Fatalf("first_week_deaths=%d want 1", mort.Totals.FirstWeekDeaths)
	}
	// The recent list names the same band the chart counted the animal under.
	for _, d := range mort.Deaths {
		if d.AgeBandKey != domain.MortalityAgeBandKey(d.AgeDays) || d.AgeBandLabel != domain.MortalityAgeBandLabel(d.AgeBandKey) {
			t.Fatalf("recent row %s band %q/%q disagrees with age %v", d.DisplayID, d.AgeBandKey, d.AgeBandLabel, d.AgeDays)
		}
		if d.Season != domain.MortalitySeasonLabel("monsoon") {
			t.Fatalf("recent row %s season %q want monsoon", d.DisplayID, d.Season)
		}
	}
}

// THE CAUSE LOCK. Three bases -- recorded, inferred, none -- must partition deaths exactly,
// with recorded winning over inferred, and a recorded cause must reach the app layer as its
// raw key with an EMPTY label so Health's vocabulary names it (the adapter must not).
func TestMortalityCauseBasesPartitionDeaths(t *testing.T) {
	ctx := context.Background()
	repo, pool := newHerdAnalyticsRepo(t, ctx)
	died := "2026-07-15"
	from, to := "2026-07-01", "2026-07-31"

	recorded := mortalityGoatID(200)
	both := mortalityGoatID(201)
	inferred := mortalityGoatID(202)
	coMorbid := mortalityGoatID(206)
	none := mortalityGoatID(203)
	priorCase := mortalityGoatID(204)
	afterCase := mortalityGoatID(205)
	for _, id := range []string{recorded, both, inferred, coMorbid, none, priorCase, afterCase} {
		insertMortalityGoat(t, ctx, pool, id, "Beetal", "female", "F2-Female", "adult", "2025-01-01", "procured", "died", died)
	}
	for _, id := range []string{recorded, both} {
		if _, err := pool.Exec(ctx, `
INSERT INTO health_death_causes (tenant_id, goat_id, cause_key, cause_kind) VALUES ($1::uuid, $2::uuid, 'MASTITIS', 'register_rule')`, countsTenant, id); err != nil {
			t.Fatalf("seed cause: %v", err)
		}
	}
	// A dead-closed case needs a published protocol version to hang off; one minimal card.
	var versionID string
	if err := pool.QueryRow(ctx, `
INSERT INTO health_protocol_versions (tenant_id, disease_key, display_name, age_band, version, status, content_hash, published_at)
VALUES ($1::uuid, 'supportive', 'Supportive care', 'adult', 1, 'published', 'mortality-test', now())
RETURNING health_protocol_version_id::text`, countsTenant).Scan(&versionID); err != nil {
		t.Fatalf("seed protocol version: %v", err)
	}
	for i, id := range []string{both, inferred} {
		if _, err := pool.Exec(ctx, `
	INSERT INTO health_cases (tenant_id, goat_id, health_protocol_version_id, disease_key, disease_name,
	                          age_band, start_date, duration_days, status, park_id, shed_id,
                          idempotency_key, request_fingerprint)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'supportive', 'Pneumonia', 'adult', '2026-07-10', 5, 'closed_dead',
        $4::uuid, $5::uuid, $6, $6)`, countsTenant, id, versionID, countsPark, countsShedA, fmt.Sprintf("mortality-case-%d", i)); err != nil {
			t.Fatalf("seed inferred case: %v", err)
		}
	}
	for i, disease := range []string{"Pneumonia", "Mastitis"} {
		if _, err := pool.Exec(ctx, `
	INSERT INTO health_cases (tenant_id, goat_id, health_protocol_version_id, disease_key, disease_name,
	                          age_band, start_date, duration_days, status, park_id, shed_id,
	                          idempotency_key, request_fingerprint)
	VALUES ($1::uuid, $2::uuid, $3::uuid, 'supportive', $4, 'adult', '2026-07-10', 5, 'closed_dead',
	        $5::uuid, $6::uuid, $7, $7)`, countsTenant, coMorbid, versionID, disease, countsPark, countsShedA, fmt.Sprintf("mortality-comorbid-case-%d", i)); err != nil {
			t.Fatalf("seed co-morbid inferred case: %v", err)
		}
	}
	for i, seed := range []struct {
		goatID   string
		start    string
		closedAt string
	}{
		{priorCase, "2026-06-01", "2026-06-10"},
		{afterCase, "2026-07-20", "2026-07-25"},
	} {
		if _, err := pool.Exec(ctx, `
INSERT INTO health_cases (tenant_id, goat_id, health_protocol_version_id, disease_key, disease_name,
                          age_band, start_date, duration_days, status, closed_at, park_id, shed_id,
                          idempotency_key, request_fingerprint)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'supportive', 'Pneumonia', 'adult', $4::date, 5, 'closed_dead',
        ($5::date + time '18:00') AT TIME ZONE 'Asia/Kolkata',
        $6::uuid, $7::uuid, $8, $8)`, countsTenant, seed.goatID, versionID, seed.start, seed.closedAt, countsPark, countsShedA, fmt.Sprintf("mortality-non-overlap-case-%d", i)); err != nil {
			t.Fatalf("seed non-overlap case: %v", err)
		}
	}

	mort, err := repo.GetMortality(ctx, domain.MortalityQuery{TenantID: countsTenant, FromDate: from, ToDate: to})
	if err != nil {
		t.Fatalf("mortality: %v", err)
	}
	if mort.Totals.Deaths != 7 {
		t.Fatalf("deaths=%d want 7", mort.Totals.Deaths)
	}
	if mort.Totals.CauseRecorded != 2 || mort.Totals.CauseInferred != 2 || mort.Totals.CauseNone != 3 {
		t.Fatalf("bases recorded=%d inferred=%d none=%d, want 2/2/3", mort.Totals.CauseRecorded, mort.Totals.CauseInferred, mort.Totals.CauseNone)
	}
	var sum int64
	seenCoMorbid := false
	for _, b := range mort.Cause {
		sum += b.Deaths
		switch b.Basis {
		case domain.MortalityCauseRecorded:
			if b.Key != "MASTITIS" || b.Label != "" {
				t.Fatalf("recorded bucket must carry the raw key and an empty label for Health to name: %+v", b)
			}
		case domain.MortalityCauseInferred:
			switch b.Label {
			case "Pneumonia":
			case "Mastitis · Pneumonia":
				seenCoMorbid = true
			default:
				t.Fatalf("inferred bucket must carry the case's own disease name: %+v", b)
			}
		case domain.MortalityCauseNone:
			if b.Key != "" {
				t.Fatalf("none bucket must have an empty key: %+v", b)
			}
		default:
			t.Fatalf("unknown basis %+v", b)
		}
	}
	if sum != mort.Totals.Deaths {
		t.Fatalf("cause buckets sum %d != deaths %d", sum, mort.Totals.Deaths)
	}
	if !seenCoMorbid {
		t.Fatalf("co-morbid inferred cause did not use the stable sorted label: %+v", mort.Cause)
	}
	// The breed x cause cross tab is the same partition one level down.
	var cross int64
	for _, c := range mort.BreedByCause {
		cross += c.Deaths
	}
	if cross != mort.Totals.Deaths {
		t.Fatalf("breed_by_cause sums %d != deaths %d", cross, mort.Totals.Deaths)
	}
}

func istDayMinus(day string, n int) string {
	parsed, err := time.ParseInLocation("2006-01-02", day, biztime.DefaultLocation())
	if err != nil {
		panic(err)
	}
	return istDate(parsed.AddDate(0, 0, -n))
}

// THE STATUS MATRIX. Every exit_reason the schema permits, plus the NULL-reason lifecycle
// fallback, must land in exactly one of two places: a death (died / dead) or a non-death exit
// that is NEITHER a death NOR in the head count. A sale counted as a death inflates the rate;
// a sold animal left in the denominator deflates it; neither fails loudly.
func TestMortalityStatusMatrixOnlyDiedCountsAsADeath(t *testing.T) {
	ctx := context.Background()
	repo, pool := newHerdAnalyticsRepo(t, ctx)
	died := "2026-07-15"
	from, to := "2026-07-01", "2026-07-31"

	matrix := []struct{ lifecycle, reason string }{
		{"dead", "died"}, {"dead", ""},
		{"sold", "sold"}, {"sold", ""},
		{"culled", "culled"}, {"culled", ""},
		{"transferred", "transferred"}, {"transferred", ""},
		{"lost", "lost"}, {"lost", ""},
	}
	for i, m := range matrix {
		insertExitedGoat(t, ctx, pool, mortalityGoatID(300+i), fmt.Sprintf("G-93%04d", i), m.lifecycle, m.reason, died)
	}
	// Two animals still on the farm, one of them in ICU. The denominator is the same default
	// live census Counts Breakdown reports, so the clinical state is reachable elsewhere but not
	// counted here.
	insertMortalityGoat(t, ctx, pool, mortalityGoatID(320), "Beetal", "female", "F2-Female", "adult", "2025-01-01", "procured", "", "")
	insertMortalityGoat(t, ctx, pool, mortalityGoatID(321), "Beetal", "female", "ICU", "adult", "2025-01-01", "procured", "", "")
	if _, err := pool.Exec(ctx, `UPDATE goats SET lifecycle_status = 'icu' WHERE goat_id = $1::uuid`, mortalityGoatID(321)); err != nil {
		t.Fatalf("icu: %v", err)
	}

	mort, err := repo.GetMortality(ctx, domain.MortalityQuery{TenantID: countsTenant, FromDate: from, ToDate: to})
	if err != nil {
		t.Fatalf("mortality: %v", err)
	}
	if mort.Totals.Deaths != 2 {
		t.Fatalf("deaths=%d want 2 (died by reason, dead by fallback)", mort.Totals.Deaths)
	}
	if mort.Totals.Animals != 1 {
		t.Fatalf("animals=%d want 1 (strict Counts Breakdown live head count; ICU excluded)", mort.Totals.Animals)
	}
	if len(mort.Deaths) != 2 {
		t.Fatalf("recent list %d rows, want the 2 deaths only", len(mort.Deaths))
	}
	// StatusMatrix, vendor side: the vendor series reads its membership off the same rows, so
	// the eight non-death exits are in neither its deaths nor its head count.
	var vendorDeaths, vendorAnimals int64
	for _, b := range mort.Vendor {
		vendorDeaths += b.Deaths
		vendorAnimals += b.Animals
	}
	if vendorDeaths != 2 || vendorAnimals != 1 {
		t.Fatalf("vendor series sums deaths=%d animals=%d, want 2/1 -- a sold, culled, transferred or lost animal is leaking into a vendor's rate", vendorDeaths, vendorAnimals)
	}
}

// PARK SCOPE. A park filter must narrow EVERY series -- totals, rates, months, bands, causes
// and the recent list -- not just the tile. One farm's leadership shown the estate under their
// own farm's label is the defect.
func TestMortalityParkScopeNarrowsEverySeries(t *testing.T) {
	ctx := context.Background()
	repo, pool := newHerdAnalyticsRepo(t, ctx)
	died := "2026-07-15"
	from, to := "2026-07-01", "2026-07-31"
	otherPark := "50000000-0000-4000-8000-000000000001"
	otherShed := "50000000-0000-4000-8000-000000000002"
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, 'park', 'QA2', 'QA2', 'active')
ON CONFLICT (location_id) DO NOTHING`, countsTenant, otherPark); err != nil {
		t.Fatalf("seed park: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
VALUES ($3::uuid, $1::uuid, 'shed', 'QA2-S1', 'QA2 Shed 1', $2::uuid, 'active')
ON CONFLICT (location_id) DO NOTHING`, countsTenant, otherPark, otherShed); err != nil {
		t.Fatalf("seed shed: %v", err)
	}
	// Two deaths and one live animal in CPT; one death and one live animal in QA2.
	insertMortalityGoat(t, ctx, pool, mortalityGoatID(400), "Beetal", "female", "K1", "kid", "2026-07-01", "birth", "died", died)
	insertMortalityGoat(t, ctx, pool, mortalityGoatID(401), "Beetal", "male", "F2-Male", "adult", "2025-01-01", "procured", "died", died)
	insertMortalityGoat(t, ctx, pool, mortalityGoatID(402), "Beetal", "female", "F2-Female", "adult", "2025-01-01", "procured", "", "")
	for i, reason := range []string{"died", ""} {
		insertMortalityGoat(t, ctx, pool, mortalityGoatID(410+i), "Sirohi", "female", "K2", "kid", "2026-06-01", "birth", reason, died)
	}
	if _, err := pool.Exec(ctx, `UPDATE goats SET park_id = $2::uuid, shed_id = $3::uuid WHERE goat_id IN ($4::uuid, $5::uuid) AND tenant_id = $1::uuid`,
		countsTenant, otherPark, otherShed, mortalityGoatID(410), mortalityGoatID(411)); err != nil {
		t.Fatalf("move to QA2: %v", err)
	}

	all, err := repo.GetMortality(ctx, domain.MortalityQuery{TenantID: countsTenant, FromDate: from, ToDate: to})
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	cpt := countsPark
	scoped, err := repo.GetMortality(ctx, domain.MortalityQuery{TenantID: countsTenant, ParkID: &cpt, FromDate: from, ToDate: to})
	if err != nil {
		t.Fatalf("scoped: %v", err)
	}
	if all.Totals.Deaths != 3 || all.Totals.Animals != 2 || len(all.Park) != 2 {
		t.Fatalf("unscoped totals %+v parks %+v, want 3 deaths / 2 live over 2 parks", all.Totals, all.Park)
	}
	if scoped.Totals.Deaths != 2 || scoped.Totals.Animals != 1 || len(scoped.Park) != 1 {
		t.Fatalf("CPT totals %+v parks %+v, want 2 deaths / 1 live over 1 park", scoped.Totals, scoped.Park)
	}
	sum := func(bs []domain.MortalityBucket) (d int64) {
		for _, b := range bs {
			d += b.Deaths
		}
		return d
	}
	// ParkScope covers the vendor series too: a vendor sells to both farms, so an unnarrowed
	// vendor row would show one farm's leadership the estate's deaths under their own filter.
	for name, series := range map[string][]domain.MortalityBucket{"breed": scoped.Breed, "age": scoped.AgeAtDeath, "season": scoped.Season, "cause": scoped.Cause, "pen": scoped.Pen, "vaccine": scoped.DaysSinceVaccine, "vendor": scoped.Vendor} {
		if sum(series) != 2 {
			t.Fatalf("%s series sums %d under the CPT scope, want 2: %+v", name, sum(series), series)
		}
	}
	if len(scoped.Deaths) != 2 || scoped.Months[0].Deaths != 2 {
		t.Fatalf("recent list %d rows, July %d deaths under CPT scope, want 2 and 2", len(scoped.Deaths), scoped.Months[0].Deaths)
	}
	// The scoped park's own label, whatever the baseline named it (the shared test park id is
	// seeded by the baseline as CBE/Coimbatore, so the seed's CPT code is a no-op).
	scopedLabel := scoped.Park[0].Label
	for _, d := range scoped.Deaths {
		if d.Park != scopedLabel {
			t.Fatalf("recent row %s is in %q under the %s scope", d.DisplayID, d.Park, scopedLabel)
		}
	}
	// The Sirohi kid in QA2 must not appear in a CPT breed bucket at all.
	for _, b := range scoped.Breed {
		if b.Key == "Sirohi" {
			t.Fatalf("QA2's Sirohi leaked into the CPT scope: %+v", b)
		}
	}
}

// PAGE BOUNDARY. The deaths list serves ONE PAGE; every figure above it is a whole-window count
// and must NOT move with the page. Seed one more death than a default page holds.
func TestMortalityRecentListPageBoundaryLeavesTotalsUntouched(t *testing.T) {
	ctx := context.Background()
	repo, pool := newHerdAnalyticsRepo(t, ctx)
	died := "2026-07-15"
	from, to := "2026-07-01", "2026-07-31"
	n := domain.MortalityRecentLimit + 1
	for i := 0; i < n; i++ {
		insertMortalityGoat(t, ctx, pool, mortalityGoatID(500+i), "Beetal", "female", "K1", "kid", "2026-07-01", "birth", "died", istDayMinus(died, i%10))
	}
	mort, err := repo.GetMortality(ctx, domain.MortalityQuery{TenantID: countsTenant, FromDate: from, ToDate: to})
	if err != nil {
		t.Fatalf("mortality: %v", err)
	}
	if len(mort.Deaths) != domain.MortalityRecentLimit || mort.RecentLimit != domain.MortalityRecentLimit || mort.RecentOffset != 0 {
		t.Fatalf("page %d rows (limit %d, offset %d), want a full first page", len(mort.Deaths), mort.RecentLimit, mort.RecentOffset)
	}
	if mort.Totals.Deaths != int64(n) {
		t.Fatalf("deaths=%d want %d: the total is the PAGER's total and must not follow one page", mort.Totals.Deaths, n)
	}
	// PageBoundary, vendor side: the vendor series and its cross tab are whole-window rollups
	// and must not follow the list cap either.
	var vendor, vendorCause int64
	for _, b := range mort.Vendor {
		vendor += b.Deaths
	}
	for _, c := range mort.VendorByCause {
		vendorCause += c.Deaths
	}
	if vendor != int64(n) || vendorCause != int64(n) {
		t.Fatalf("vendor=%d vendor x cause=%d, want %d each: a whole-window rollup must not follow one page of the list", vendor, vendorCause, n)
	}
	var age, season, cause, months int64
	for _, b := range mort.AgeAtDeath {
		age += b.Deaths
	}
	for _, b := range mort.Season {
		season += b.Deaths
	}
	for _, b := range mort.Cause {
		cause += b.Deaths
	}
	for _, m := range mort.Months {
		months += m.Deaths
	}
	if age != int64(n) || season != int64(n) || cause != int64(n) || months != int64(n) {
		t.Fatalf("count series age=%d season=%d cause=%d months=%d, want %d each", age, season, cause, months, n)
	}
	// Most recent first, so the first row is the death on the window's latest day.
	if mort.Deaths[0].DiedOn != died {
		t.Fatalf("first recent row died %s, want %s (most recent first)", mort.Deaths[0].DiedOn, died)
	}
}

// THE LIST PAGES THROUGH THE WHOLE WINDOW, and the second page continues exactly where the first
// stopped. This is what the card's pager walks: a reader shown a rate for a vendor or a load has
// to be able to read the animals behind it, and the old hard cap stopped at the first page.
//
// The two properties that make a page boundary trustworthy are asserted together: no animal is
// REPEATED across the pages and none is SKIPPED, which holds because the list orders by
// (died_on DESC, goat_id) -- total over the window's deaths -- rather than by the date alone.
// Several of these animals share a death date on purpose, since a partial order is exactly what
// lets a row sit on both pages or on neither.
func TestMortalityDeathsListPagesThroughTheWholeWindow(t *testing.T) {
	ctx := context.Background()
	repo, pool := newHerdAnalyticsRepo(t, ctx)
	from, to := "2026-07-01", "2026-07-31"
	const n = 23
	for i := 0; i < n; i++ {
		// i%5 deliberately puts several animals on the same died_on.
		insertMortalityGoat(t, ctx, pool, mortalityGoatID(700+i), "Beetal", "female", "K1", "kid", "2026-07-01", "birth", "died", istDayMinus("2026-07-20", i%5))
	}

	const size = 10
	seen := map[string]int{}
	pages := 0
	for offset := 0; offset < n; offset += size {
		page, err := repo.GetMortality(ctx, domain.MortalityQuery{TenantID: countsTenant, FromDate: from, ToDate: to, RecentLimit: size, RecentOffset: offset})
		if err != nil {
			t.Fatalf("mortality page at offset %d: %v", offset, err)
		}
		if page.RecentLimit != size || page.RecentOffset != offset {
			t.Fatalf("page reports limit %d offset %d, want %d/%d -- the pager renders what the server served", page.RecentLimit, page.RecentOffset, size, offset)
		}
		want := size
		if remaining := n - offset; remaining < size {
			want = remaining
		}
		if len(page.Deaths) != want {
			t.Fatalf("page at offset %d holds %d rows, want %d", offset, len(page.Deaths), want)
		}
		if page.Totals.Deaths != int64(n) {
			t.Fatalf("page at offset %d reports totals.deaths=%d, want %d: the pager's total is a whole-window figure", offset, page.Totals.Deaths, n)
		}
		for _, d := range page.Deaths {
			seen[d.GoatID]++
		}
		pages++
	}
	if pages != 3 {
		t.Fatalf("walked %d pages, want 3 for %d deaths at %d a page", pages, n, size)
	}
	if len(seen) != n {
		t.Fatalf("the pages covered %d distinct animals, want %d: a boundary skipped or repeated a row", len(seen), n)
	}
	for id, times := range seen {
		if times != 1 {
			t.Fatalf("animal %s appeared on %d pages, want exactly 1", id, times)
		}
	}
}

// A NONSENSE PAGER PARAMETER MUST NOT TAKE THE SCREEN DOWN. Every figure on this payload except
// the list itself is whole-window, so an unknown page size or a negative offset resolves to the
// first page at the default size -- it is never an error, and never served verbatim.
func TestMortalityResolvesAnUnusablePageRequestToTheFirstPage(t *testing.T) {
	ctx := context.Background()
	repo, pool := newHerdAnalyticsRepo(t, ctx)
	from, to := "2026-07-01", "2026-07-31"
	for i := 0; i < 4; i++ {
		insertMortalityGoat(t, ctx, pool, mortalityGoatID(760+i), "Beetal", "female", "K1", "kid", "2026-07-01", "birth", "died", "2026-07-11")
	}
	for _, bad := range []struct {
		name          string
		limit, offset int
	}{
		{"size outside the offered vocabulary", 7, 0},
		{"negative offset", 10, -5},
		{"offset past the clamp", 10, domain.MortalityRecentMaxOffset + 1},
	} {
		t.Run(bad.name, func(t *testing.T) {
			mort, err := repo.GetMortality(ctx, domain.MortalityQuery{TenantID: countsTenant, FromDate: from, ToDate: to, RecentLimit: bad.limit, RecentOffset: bad.offset})
			if err != nil {
				t.Fatalf("mortality: %v", err)
			}
			if mort.RecentOffset != 0 {
				t.Fatalf("offset resolved to %d, want the first page", mort.RecentOffset)
			}
			if bad.limit == 7 && mort.RecentLimit != domain.MortalityRecentLimit {
				t.Fatalf("size resolved to %d, want the default %d", mort.RecentLimit, domain.MortalityRecentLimit)
			}
			if len(mort.Deaths) != 4 {
				t.Fatalf("list holds %d rows, want all 4 deaths on the first page", len(mort.Deaths))
			}
		})
	}
}

// THE DENOMINATOR IS TODAY'S HEAD COUNT (maintainer decision 2026-09-18, superseding the
// at-risk population an earlier review fix had widened to interval overlap). "Animals" is
// how many animals are in that section NOW -- the same figure Counts Breakdown reports -- so
// a goat sold after the window, a goat that died inside it and a goat that entered after the
// window all sit exactly where the head count puts them: out, out, and in.
func TestMortalityAnimalsIsTodaysHeadCountNotAnAtRiskPopulation(t *testing.T) {
	ctx := context.Background()
	repo, pool := newHerdAnalyticsRepo(t, ctx)
	from, to := "2026-07-01", "2026-07-31"

	insertMortalityGoat(t, ctx, pool, mortalityGoatID(600), "Beetal", "female", "K1", "kid", "2026-07-01", "birth", "died", "2026-07-15")
	insertMortalityGoat(t, ctx, pool, mortalityGoatID(601), "Beetal", "female", "K1", "kid", "2026-06-01", "birth", "", "")
	insertMortalityGoat(t, ctx, pool, mortalityGoatID(602), "Beetal", "female", "K1", "kid", "2026-06-01", "procured", "sold", "2026-08-15")
	// Born after the window: not exposed in July, but IN the herd today, so in the head count.
	insertMortalityGoat(t, ctx, pool, mortalityGoatID(603), "Beetal", "female", "K1", "kid", "2026-08-20", "birth", "", "")

	mort, err := repo.GetMortality(ctx, domain.MortalityQuery{TenantID: countsTenant, FromDate: from, ToDate: to})
	if err != nil {
		t.Fatalf("mortality: %v", err)
	}
	if mort.Totals.Deaths != 1 {
		t.Fatalf("deaths=%d want 1", mort.Totals.Deaths)
	}
	if mort.Totals.Animals != 2 {
		t.Fatalf("animals=%d want 2 (the two live animals; the dead and the sold are out)", mort.Totals.Animals)
	}
	if mort.Totals.RatePct == nil || *mort.Totals.RatePct != 50.0 {
		t.Fatalf("rate=%v want 50.0", mort.Totals.RatePct)
	}
	for _, b := range mort.Stage {
		if b.Key == "K1" && (b.Animals != 2 || b.Deaths != 1) {
			t.Fatalf("K1 bucket %+v, want 1 death / 2 animals", b)
		}
	}
}

// THE VENDOR ROLL-UP LOCK, and the cardinality axis of this change: a vendor stands in a
// ONE-TO-MANY relation to its loads, which is exactly the shape that fans a bucket out or
// collapses it to one member. The vendor series answers a question the load series cannot: one
// bad load is bad luck, the same vendor twice is a supply problem. So a vendor's bucket must
// gather EVERY load that vendor sent -- deaths and the live head count alike -- and must not
// read as one load. The denominator is that vendor's own live animals, the page's locked
// rule. Farm-born animals have no vendor and keep their own bucket; a purchased animal whose
// load names no party lands in 'no_vendor' rather than being folded into a real vendor.
//
// Mutation-tested when written: grouping the vendor branch by the load (the tempting reuse of
// the load_key already in `pop`) splits Sardar's six animals into two rows of three and turns
// this red, as does dropping the parties label lookup, which leaves a bare uuid on screen.
func TestMortalityVendorMultipleDimensionsRollUpEveryLoadThatVendorSent(t *testing.T) {
	ctx := context.Background()
	repo, pool := newHerdAnalyticsRepo(t, ctx)
	died := "2026-07-15"
	from, to := "2026-07-01", "2026-07-31"

	var sardar, kumar string
	if err := pool.QueryRow(ctx, `
INSERT INTO parties (party_type, display_name, status) VALUES ('org', 'Sardar Traders', 'active')
RETURNING party_id::text`).Scan(&sardar); err != nil {
		t.Fatalf("seed vendor: %v", err)
	}
	if err := pool.QueryRow(ctx, `
INSERT INTO parties (party_type, display_name, status) VALUES ('org', 'Kumar Livestock', 'active')
RETURNING party_id::text`).Scan(&kumar); err != nil {
		t.Fatalf("seed vendor: %v", err)
	}
	newLoad := func(vendor, key string) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, `
INSERT INTO procurement_loads (tenant_id, source_party_id, purchase_date, status, idempotency_key, context)
VALUES ($1::uuid, $2::uuid, '2026-06-01', 'accepted_intake', $3::text, jsonb_build_object('load_ref', $3::text))
RETURNING load_id::text`, countsTenant, vendor, key).Scan(&id); err != nil {
			t.Fatalf("seed load %s: %v", key, err)
		}
		return id
	}
	// Sardar sent TWO loads; Kumar one. An orphan load with no vendor row is impossible
	// (source_party_id is NOT NULL), so 'no_vendor' is proved by a purchased animal that is on
	// no load at all.
	var reddy string
	if err := pool.QueryRow(ctx, `
INSERT INTO parties (party_type, display_name, status) VALUES ('org', 'Reddy Farms', 'active')
RETURNING party_id::text`).Scan(&reddy); err != nil {
		t.Fatalf("seed vendor: %v", err)
	}
	sardarA, sardarB, kumarLoad := newLoad(sardar, "L-1"), newLoad(sardar, "L-2"), newLoad(kumar, "L-3")
	reddyLoad := newLoad(reddy, "L-4")
	attach := func(load, goatID string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
INSERT INTO procurement_load_goats (tenant_id, load_id, goat_id, selection_state, current_state, intake_accepted_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'accepted_herd_intake', 'accepted_herd_intake', now())`, countsTenant, load, goatID); err != nil {
			t.Fatalf("attach goat to load: %v", err)
		}
	}
	// Sardar: 2 deaths and 4 live, spread across his two loads so neither load alone is the answer.
	sardarGoats := map[string][2]string{ // goat id -> {load, exit reason}
		mortalityGoatID(700): {sardarA, "died"},
		mortalityGoatID(701): {sardarB, "died"},
		mortalityGoatID(702): {sardarA, ""},
		mortalityGoatID(703): {sardarA, ""},
		mortalityGoatID(704): {sardarB, ""},
		mortalityGoatID(705): {sardarB, ""},
	}
	for id, spec := range sardarGoats {
		exitedOn := ""
		if spec[1] != "" {
			exitedOn = died
		}
		insertMortalityGoat(t, ctx, pool, id, "Beetal", "female", "F2-Female", "adult", "2025-01-01", "procured", spec[1], exitedOn)
		attach(spec[0], id)
	}
	// Kumar: 1 death against 1 live animal.
	insertMortalityGoat(t, ctx, pool, mortalityGoatID(710), "Sirohi", "male", "F2-Male", "adult", "2025-01-01", "procured", "died", died)
	attach(kumarLoad, mortalityGoatID(710))
	insertMortalityGoat(t, ctx, pool, mortalityGoatID(711), "Sirohi", "male", "F2-Male", "adult", "2025-01-01", "procured", "", "")
	attach(kumarLoad, mortalityGoatID(711))
	// Reddy has lost NOTHING. The load and pen series would drop him; the vendor board must
	// keep him, because a supplier list showing only the vendors whose animals died cannot be
	// compared -- the reader cannot tell a good vendor from one who is simply absent.
	for _, id := range []string{mortalityGoatID(715), mortalityGoatID(716)} {
		insertMortalityGoat(t, ctx, pool, id, "Sirohi", "female", "F2-Female", "adult", "2025-01-01", "procured", "", "")
		attach(reddyLoad, id)
	}
	// Farm born: a death and a live animal, neither of which belongs to any vendor.
	insertMortalityGoat(t, ctx, pool, mortalityGoatID(720), "Malai", "female", "K1", "kid", "2026-07-01", "birth", "died", died)
	insertMortalityGoat(t, ctx, pool, mortalityGoatID(721), "Malai", "female", "K1", "kid", "2026-07-01", "birth", "", "")
	// Purchased but on no load at all.
	insertMortalityGoat(t, ctx, pool, mortalityGoatID(730), "Beetal", "male", "F2-Male", "adult", "2025-01-01", "procured", "died", died)

	// One recorded cause, so the cross tab has a named column beside the unrecorded ones.
	if _, err := pool.Exec(ctx, `
INSERT INTO health_death_causes (tenant_id, goat_id, cause_key, cause_kind) VALUES ($1::uuid, $2::uuid, 'MASTITIS', 'register_rule')`,
		countsTenant, mortalityGoatID(700)); err != nil {
		t.Fatalf("seed cause: %v", err)
	}

	mort, err := repo.GetMortality(ctx, domain.MortalityQuery{TenantID: countsTenant, FromDate: from, ToDate: to})
	if err != nil {
		t.Fatalf("mortality: %v", err)
	}

	byLabel := map[string]domain.MortalityBucket{}
	for _, b := range mort.Vendor {
		if _, clash := byLabel[b.Label]; clash {
			t.Fatalf("vendor label %q appears twice, so one vendor reads as two rows: %+v", b.Label, mort.Vendor)
		}
		byLabel[b.Label] = b
	}
	// Sardar's TWO loads are ONE vendor row: 2 deaths against his 4 live animals.
	want := map[string][3]float64{
		"Sardar Traders":     {2, 4, 50.0},
		"Kumar Livestock":    {1, 1, 100.0},
		"Farm born":          {1, 1, 100.0},
		"No vendor recorded": {1, 0, -1},
		"Reddy Farms":        {0, 2, 0.0},
	}
	for label, w := range want {
		got, ok := byLabel[label]
		if !ok {
			t.Fatalf("vendor series missing %q: %+v", label, mort.Vendor)
		}
		if float64(got.Deaths) != w[0] || float64(got.Animals) != w[1] {
			t.Fatalf("%s bucket %+v, want deaths=%v animals=%v", label, got, w[0], w[1])
		}
		switch {
		case w[2] < 0 && got.RatePct != nil:
			// No live animal in the section: a rate would be invented, so there must be none.
			t.Fatalf("%s carries rate %v with %d live animals, want no rate", label, *got.RatePct, got.Animals)
		case w[2] >= 0 && (got.RatePct == nil || *got.RatePct != w[2]):
			t.Fatalf("%s rate %v, want %v", label, got.RatePct, w[2])
		}
	}
	// The series partitions the whole read exactly: every death and every live animal is in
	// exactly one vendor bucket, the same property every other RATE series holds.
	var deaths, animals int64
	for _, b := range mort.Vendor {
		deaths += b.Deaths
		animals += b.Animals
	}
	if deaths != mort.Totals.Deaths || animals != mort.Totals.Animals {
		t.Fatalf("vendor series sums deaths=%d animals=%d, want %d/%d", deaths, animals, mort.Totals.Deaths, mort.Totals.Animals)
	}
	// The vendor bucket is the vendor's loads added up, never one of them: Sardar's two load
	// rows must each be smaller than his vendor row.
	for _, b := range mort.Load {
		if b.Label == "Load L-1" || b.Label == "Load L-2" {
			if b.Deaths >= byLabel["Sardar Traders"].Deaths {
				t.Fatalf("load %s has %d deaths, the same as the whole vendor -- the vendor row is one load, not the roll-up", b.Label, b.Deaths)
			}
		}
	}
	// Vendor x cause: every cell totals back to the vendor's deaths, and the cause column is
	// labelled farm copy resolved by the app layer, never left as a bare key here.
	crossDeaths := map[string]int64{}
	for _, c := range mort.VendorByCause {
		crossDeaths[c.RowLabel] += c.Deaths
	}
	if crossDeaths["Sardar Traders"] != 2 || crossDeaths["Kumar Livestock"] != 1 || crossDeaths["Farm born"] != 1 {
		t.Fatalf("vendor x cause rows %+v, want Sardar 2, Kumar 1, Farm born 1", crossDeaths)
	}
	// The cross tab is deaths only, so a vendor who has lost nothing has no row in it -- the
	// series above is where he is compared.
	if _, present := crossDeaths["Reddy Farms"]; present {
		t.Fatalf("vendor x cause carries a row for a vendor with no deaths: %+v", crossDeaths)
	}
	var crossTotal int64
	for _, c := range mort.VendorByCause {
		crossTotal += c.Deaths
	}
	if crossTotal != mort.Totals.Deaths {
		t.Fatalf("vendor x cause totals %d != deaths %d", crossTotal, mort.Totals.Deaths)
	}
}
