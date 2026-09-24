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
// rule the labels come from, or a rate whose denominator is not every animal that was in the
// bucket during the window (maintainer decision 2026-09-24).

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
	// Three kinds of animal, three names: an animal on no load that was not born here is
	// procured with no load record, never a second "Farm born" row (maintainer, 2026-09-24).
	if got := loadBucketLabel("no_load", ""); got != "Procured, no load" {
		t.Fatalf("no_load label=%q, want Procured, no load", got)
	}
	if loadBucketLabel("no_load", "") == loadBucketLabel("farm_born", "") {
		t.Fatalf("the no_load and farm_born groups share the label %q, so the load table shows it twice", loadBucketLabel("farm_born", ""))
	}
	// The vendor table names the same animals the same way.
	if got := vendorBucketLabel("no_vendor", ""); got != loadBucketLabel("no_load", "") {
		t.Fatalf("no_vendor label=%q, want the load table's %q for the same animals", got, loadBucketLabel("no_load", ""))
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
	// Animals = every animal that was there in the window: the 3 dead, the 1 sold inside it and
	// the 2 still alive (maintainer decision 2026-09-24).
	if mort.Totals.Animals != 6 {
		t.Fatalf("animals=%d want 6", mort.Totals.Animals)
	}
	if mort.Totals.RatePct == nil || *mort.Totals.RatePct != 50.0 {
		t.Fatalf("rate=%v want 50.0 (3 deaths of the 6 animals that were there)", mort.Totals.RatePct)
	}
	if mort.Totals.KidDeaths+mort.Totals.AdultDeaths != mort.Totals.Deaths {
		t.Fatalf("kids %d + adults %d != deaths %d", mort.Totals.KidDeaths, mort.Totals.AdultDeaths, mort.Totals.Deaths)
	}
	// With no stage change or move recorded, every RATE series partitions both deaths and the
	// animals exactly. (Once animals move, stage / kid-adult / pen stop adding up by design: an
	// animal counts in every bucket it passed through.)
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
	// The breed rate divides by THAT breed's own animals, never the whole herd: Beetal has 2
	// deaths (one by reason, the fallback row is Beetal too) of 4 animals (the two dead, the
	// sold one, the live one); Sirohi has 1 death of 2.
	want := map[string][3]float64{"Beetal": {2, 4, 50.0}, "Sirohi": {1, 2, 50.0}}
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
	// Its label carries the park code in front, from the park series of the same response.
	parkCode := ""
	for _, park := range mort.Park {
		if park.Key == countsPark {
			parkCode = park.Label
		}
	}
	if len(mort.Pen) != 1 || parkCode == "" || mort.Pen[0].Label != parkCode+" · CPT Shed 1" || mort.Pen[0].Deaths != 3 {
		t.Fatalf("pen series %+v, want one %q row with 3 deaths", mort.Pen, parkCode+" · CPT Shed 1")
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
	// Two animals still on the farm, one of them in ICU. An ICU animal is still on the farm, so
	// it is one of the animals that was there.
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
	// Every exit in the window was there until it left, so all ten are animals; only two are deaths.
	if mort.Totals.Animals != 12 {
		t.Fatalf("animals=%d want 12 (ten exits inside the window + two on the farm, ICU included)", mort.Totals.Animals)
	}
	if len(mort.Deaths) != 2 {
		t.Fatalf("recent list %d rows, want the 2 deaths only", len(mort.Deaths))
	}
	// StatusMatrix, vendor side: the vendor series reads its membership off the same rows, so
	// the eight non-death exits are among its animals but never among its deaths.
	var vendorDeaths, vendorAnimals int64
	for _, b := range mort.Vendor {
		vendorDeaths += b.Deaths
		vendorAnimals += b.Animals
	}
	if vendorDeaths != 2 || vendorAnimals != 12 {
		t.Fatalf("vendor series sums deaths=%d animals=%d, want 2/12 -- a sold, culled, transferred or lost animal is being counted as a death, or dropped from the animals", vendorDeaths, vendorAnimals)
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
	if all.Totals.Deaths != 3 || all.Totals.Animals != 5 || len(all.Park) != 2 {
		t.Fatalf("unscoped totals %+v parks %+v, want 3 deaths of 5 animals over 2 parks", all.Totals, all.Park)
	}
	if scoped.Totals.Deaths != 2 || scoped.Totals.Animals != 3 || len(scoped.Park) != 1 {
		t.Fatalf("CPT totals %+v parks %+v, want 2 deaths of 3 animals over 1 park", scoped.Totals, scoped.Park)
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

// THE DENOMINATOR IS EVERY ANIMAL THAT WAS THERE (maintainer decision 2026-09-24, superseding
// the 2026-09-18 "today's head count"). A goat that died inside the window, one still alive and
// one sold AFTER the window were all in the herd in July, so all three are July's animals. A
// goat born after the window was not there in July and is not one of them, even though it is
// in the herd today.
func TestMortalityAnimalsAreEveryAnimalThatWasThere(t *testing.T) {
	ctx := context.Background()
	repo, pool := newHerdAnalyticsRepo(t, ctx)
	from, to := "2026-07-01", "2026-07-31"

	insertMortalityGoat(t, ctx, pool, mortalityGoatID(600), "Beetal", "female", "K1", "kid", "2026-07-01", "birth", "died", "2026-07-15")
	insertMortalityGoat(t, ctx, pool, mortalityGoatID(601), "Beetal", "female", "K1", "kid", "2026-06-01", "birth", "", "")
	insertMortalityGoat(t, ctx, pool, mortalityGoatID(602), "Beetal", "female", "K1", "kid", "2026-06-01", "procured", "sold", "2026-08-15")
	insertMortalityGoat(t, ctx, pool, mortalityGoatID(603), "Beetal", "female", "K1", "kid", "2026-08-20", "birth", "", "")
	// Died BEFORE the window: history, not one of July's animals.
	insertMortalityGoat(t, ctx, pool, mortalityGoatID(604), "Beetal", "female", "K1", "kid", "2026-05-01", "birth", "died", "2026-06-20")

	mort, err := repo.GetMortality(ctx, domain.MortalityQuery{TenantID: countsTenant, FromDate: from, ToDate: to})
	if err != nil {
		t.Fatalf("mortality: %v", err)
	}
	if mort.Totals.Deaths != 1 {
		t.Fatalf("deaths=%d want 1", mort.Totals.Deaths)
	}
	if mort.Totals.Animals != 3 {
		t.Fatalf("animals=%d want 3 (the dead one, the live one and the one sold after July)", mort.Totals.Animals)
	}
	if mort.Totals.RatePct == nil || *mort.Totals.RatePct != 33.3 {
		t.Fatalf("rate=%v want 33.3", mort.Totals.RatePct)
	}
	for _, b := range mort.Stage {
		if b.Key == "K1" && (b.Animals != 3 || b.Deaths != 1) {
			t.Fatalf("K1 bucket %+v, want 1 death of 3 animals", b)
		}
	}
}

// recordStageChange writes the goat.stage_changed event every production stage write emits
// and moves the animal's row to the new stage, exactly as the identity writers do.
func recordStageChange(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID, on, from, to string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO goat_identity_events (tenant_id, goat_id, event_type, event_version, occurred_at, payload, idempotency_key)
VALUES ($1::uuid, $2::uuid, 'goat.stage_changed', 1, ($3::date + time '09:00') AT TIME ZONE 'Asia/Kolkata',
        jsonb_build_object('goat_id', $2::text, 'previous_management_stage', $4::text, 'management_stage', $5::text),
        'test-stage:' || $2::text || ':' || $3::text)`, countsTenant, goatID, on, from, to); err != nil {
		t.Fatalf("stage change %s: %v", goatID, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE goats SET management_stage = $3 WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`,
		countsTenant, goatID, to); err != nil {
		t.Fatalf("stage row %s: %v", goatID, err)
	}
}

// recordPenMove writes the goat_location_history row every production move writes and moves
// the animal's row to the new shed.
func recordPenMove(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID, on, fromShed, toShed string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO goat_location_history (tenant_id, goat_id, from_location_id, to_location_id, reason, occurred_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'test move', ($5::date + time '09:00') AT TIME ZONE 'Asia/Kolkata')`,
		countsTenant, goatID, fromShed, toShed, on); err != nil {
		t.Fatalf("pen move %s: %v", goatID, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE goats SET shed_id = $3::uuid, current_location_id = $3::uuid WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`,
		countsTenant, goatID, toShed); err != nil {
		t.Fatalf("pen row %s: %v", goatID, err)
	}
}

// assertDeathsNeverExceedAnimals: every death is one of its bucket's animals, so no bucket in
// any rate series can read more deaths than animals, and every series' deaths add up to the
// total (animals need not, once animals move).
func assertDeathsNeverExceedAnimals(t *testing.T, mort domain.Mortality) {
	t.Helper()
	for name, series := range map[string][]domain.MortalityBucket{
		"stage": mort.Stage, "kid_adult": mort.KidAdult, "pen": mort.Pen, "breed": mort.Breed, "sex": mort.Sex,
		"species": mort.Species, "park": mort.Park, "load": mort.Load, "vendor": mort.Vendor,
	} {
		var deaths int64
		for _, b := range series {
			deaths += b.Deaths
			if b.Deaths > b.Animals {
				t.Fatalf("%s bucket %q has %d deaths of %d animals -- a death is missing from its own bucket's animals", name, b.Label, b.Deaths, b.Animals)
			}
		}
		if deaths != mort.Totals.Deaths {
			t.Fatalf("%s series deaths sum %d, want %d", name, deaths, mort.Totals.Deaths)
		}
	}
}

func stageBucket(mort domain.Mortality, key string) (domain.MortalityBucket, bool) {
	for _, b := range mort.Stage {
		if b.Key == key {
			return b, true
		}
	}
	return domain.MortalityBucket{}, false
}

func penBucket(mort domain.Mortality, suffix string) (domain.MortalityBucket, bool) {
	for _, b := range mort.Pen {
		if len(b.Label) >= len(suffix) && b.Label[len(b.Label)-len(suffix):] == suffix {
			return b, true
		}
	}
	return domain.MortalityBucket{}, false
}

// SHIFTING ANIMALS OUT DOES NOT MOVE THE RATE -- the defect the 2026-09-24 decision fixes. F2
// has 30 animals; one dies; two days later 15 shift to Fattening. Under the old rule F2 read
// 1 / 15 = 6.7%; it must read 1 of the 30 that were there = 3.3%. And a CLOSED window stays
// closed: shifting 10 more out in August does not rewrite July.
func TestMortalityShiftingOutDoesNotMoveTheRate(t *testing.T) {
	ctx := context.Background()
	repo, pool := newHerdAnalyticsRepo(t, ctx)
	from, to := "2026-07-01", "2026-07-31"

	insertMortalityGoat(t, ctx, pool, mortalityGoatID(800), "Beetal", "female", "F2-Female", "adult", "2025-01-01", "procured", "died", "2026-07-05")
	for i := 1; i < 30; i++ {
		insertMortalityGoat(t, ctx, pool, mortalityGoatID(800+i), "Beetal", "female", "F2-Female", "adult", "2025-01-01", "procured", "", "")
	}
	for i := 1; i <= 15; i++ {
		recordStageChange(t, ctx, pool, mortalityGoatID(800+i), "2026-07-07", "F2-Female", "Fattening")
	}

	read := func() domain.Mortality {
		t.Helper()
		mort, err := repo.GetMortality(ctx, domain.MortalityQuery{TenantID: countsTenant, FromDate: from, ToDate: to})
		if err != nil {
			t.Fatalf("mortality: %v", err)
		}
		return mort
	}
	check := func(mort domain.Mortality, when string) {
		t.Helper()
		f2, ok := stageBucket(mort, "F2-Female")
		if !ok || f2.Deaths != 1 || f2.Animals != 30 || f2.RatePct == nil || *f2.RatePct != 3.3 {
			t.Fatalf("%s: F2-Female %+v, want 1 death of 30 = 3.3%%", when, f2)
		}
		fat, ok := stageBucket(mort, "Fattening")
		if !ok || fat.Deaths != 0 || fat.Animals != 15 {
			t.Fatalf("%s: Fattening %+v, want 0 deaths of 15", when, fat)
		}
		if mort.Totals.Deaths != 1 || mort.Totals.Animals != 30 {
			t.Fatalf("%s: totals %+v, want 1 death of 30", when, mort.Totals)
		}
		assertDeathsNeverExceedAnimals(t, mort)
	}
	check(read(), "after the shift")

	// August: 10 more leave F2. July is over, so July's numbers must not move.
	for i := 16; i <= 25; i++ {
		recordStageChange(t, ctx, pool, mortalityGoatID(800+i), "2026-08-10", "F2-Female", "Fattening")
	}
	check(read(), "after a later shift")
}

// THE ONLY ANIMAL IN A PEN DIES -- the second defect. One animal is moved from CPT Shed 1 into
// CPT Shed 2 (the ICU pen) and put on the ICU stage, and dies there. Under the old rule both
// read 1 / 0 and showed no rate at all; both must read 1 of 1 = 100%. The shed it came from
// still counts it: another death in CPT Shed 1 is 1 of the 10 that were there, the moved
// animal included.
func TestMortalityTheOnlyAnimalInAPenDyingReadsOneOfOne(t *testing.T) {
	ctx := context.Background()
	repo, pool := newHerdAnalyticsRepo(t, ctx)
	from, to := "2026-07-01", "2026-07-31"

	insertMortalityGoat(t, ctx, pool, mortalityGoatID(900), "Beetal", "female", "F2-Female", "adult", "2025-01-01", "procured", "died", "2026-07-10")
	recordPenMove(t, ctx, pool, mortalityGoatID(900), "2026-07-03", countsShedA, countsShedB)
	recordStageChange(t, ctx, pool, mortalityGoatID(900), "2026-07-03", "F2-Female", "ICU")
	insertMortalityGoat(t, ctx, pool, mortalityGoatID(901), "Beetal", "female", "F2-Female", "adult", "2025-01-01", "procured", "died", "2026-07-20")
	for i := 2; i < 10; i++ {
		insertMortalityGoat(t, ctx, pool, mortalityGoatID(900+i), "Beetal", "female", "F2-Female", "adult", "2025-01-01", "procured", "", "")
	}

	mort, err := repo.GetMortality(ctx, domain.MortalityQuery{TenantID: countsTenant, FromDate: from, ToDate: to})
	if err != nil {
		t.Fatalf("mortality: %v", err)
	}
	icu, ok := stageBucket(mort, "ICU")
	if !ok || icu.Deaths != 1 || icu.Animals != 1 || icu.RatePct == nil || *icu.RatePct != 100.0 {
		t.Fatalf("ICU stage %+v, want 1 death of 1 = 100%%", icu)
	}
	icuPen, ok := penBucket(mort, "CPT Shed 2")
	if !ok || icuPen.Deaths != 1 || icuPen.Animals != 1 || icuPen.RatePct == nil || *icuPen.RatePct != 100.0 {
		t.Fatalf("ICU pen %+v in %+v, want 1 death of 1 = 100%%", icuPen, mort.Pen)
	}
	home, ok := penBucket(mort, "CPT Shed 1")
	if !ok || home.Deaths != 1 || home.Animals != 10 {
		t.Fatalf("CPT Shed 1 %+v, want 1 death of the 10 that were there", home)
	}
	f2, ok := stageBucket(mort, "F2-Female")
	if !ok || f2.Deaths != 1 || f2.Animals != 10 {
		t.Fatalf("F2-Female %+v, want 1 death of 10 (the ICU animal was F2 until it moved)", f2)
	}
	assertDeathsNeverExceedAnimals(t, mort)
}

// SELLING ANIMALS DOES NOT MOVE THE RATE -- the third defect. 10 animals, 1 dies, then 8 are
// sold. Under the old rule that read 1 / 1 = 100%; it is 1 of the 10 that were there.
func TestMortalitySellingAnimalsDoesNotMoveTheRate(t *testing.T) {
	ctx := context.Background()
	repo, pool := newHerdAnalyticsRepo(t, ctx)
	from, to := "2026-07-01", "2026-07-31"

	insertMortalityGoat(t, ctx, pool, mortalityGoatID(950), "Beetal", "male", "Fattening", "adult", "2025-01-01", "procured", "died", "2026-07-05")
	for i := 1; i < 10; i++ {
		exit, on := "", ""
		if i <= 8 {
			exit, on = "sold", "2026-07-20"
		}
		insertMortalityGoat(t, ctx, pool, mortalityGoatID(950+i), "Beetal", "male", "Fattening", "adult", "2025-01-01", "procured", exit, on)
	}

	mort, err := repo.GetMortality(ctx, domain.MortalityQuery{TenantID: countsTenant, FromDate: from, ToDate: to})
	if err != nil {
		t.Fatalf("mortality: %v", err)
	}
	fat, ok := stageBucket(mort, "Fattening")
	if !ok || fat.Deaths != 1 || fat.Animals != 10 || fat.RatePct == nil || *fat.RatePct != 10.0 {
		t.Fatalf("Fattening %+v, want 1 death of 10 = 10%%", fat)
	}
	assertDeathsNeverExceedAnimals(t, mort)
}

// A kid that grew into an adult stage inside the window counts in BOTH halves -- it was at risk
// as a kid and as an adult -- while its death lands only where it died. Deaths still never
// exceed animals in any bucket.
func TestMortalityDeathsNeverExceedTheAnimalsThatWereThere(t *testing.T) {
	ctx := context.Background()
	repo, pool := newHerdAnalyticsRepo(t, ctx)
	from, to := "2026-07-01", "2026-07-31"

	insertMortalityGoat(t, ctx, pool, mortalityGoatID(970), "Beetal", "male", "F2-Male", "adult", "2025-12-01", "birth", "died", "2026-07-20")
	recordStageChange(t, ctx, pool, mortalityGoatID(970), "2026-07-10", "K3", "F2-Male")
	insertMortalityGoat(t, ctx, pool, mortalityGoatID(971), "Beetal", "male", "K3", "kid", "2026-01-01", "birth", "", "")

	mort, err := repo.GetMortality(ctx, domain.MortalityQuery{TenantID: countsTenant, FromDate: from, ToDate: to})
	if err != nil {
		t.Fatalf("mortality: %v", err)
	}
	if mort.Totals.KidAnimals != 2 || mort.Totals.KidDeaths != 0 {
		t.Fatalf("kids %d deaths of %d, want 0 of 2 (the grown animal was a kid first)", mort.Totals.KidDeaths, mort.Totals.KidAnimals)
	}
	if mort.Totals.AdultAnimals != 1 || mort.Totals.AdultDeaths != 1 {
		t.Fatalf("adults %d deaths of %d, want 1 of 1", mort.Totals.AdultDeaths, mort.Totals.AdultAnimals)
	}
	k3, ok := stageBucket(mort, "K3")
	if !ok || k3.Animals != 2 || k3.Deaths != 0 {
		t.Fatalf("K3 %+v, want 0 deaths of 2", k3)
	}
	assertDeathsNeverExceedAnimals(t, mort)
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
	// Sardar's TWO loads are ONE vendor row: 2 deaths of the 6 animals he sent.
	want := map[string][3]float64{
		"Sardar Traders":    {2, 6, 33.3},
		"Kumar Livestock":   {1, 2, 50.0},
		"Farm born":         {1, 2, 50.0},
		"Procured, no load": {1, 1, 100.0},
		"Reddy Farms":       {0, 2, 0.0},
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

// A pen names its farm, the way Breakdown does ("CPT · Castro 1"): both farms have a "Castro 1",
// and with All parks on screen the two rows were indistinguishable. And a load recorded without a
// load number is named by its purchase date in DD/MM/YYYY, on the load table and the deaths list
// alike -- never "01 Jun 2026".
func TestMortalityPensNameTheirParkAndLoadDatesReadDDMMYYYY(t *testing.T) {
	ctx := context.Background()
	repo, pool := newHerdAnalyticsRepo(t, ctx)
	seedSameNamedShedsInTwoParks(t, ctx, pool)
	from, to := monthsBack(2), thisMonthDay(1)
	died := thisMonthDay(1)

	var vendor, unnumbered string
	if err := pool.QueryRow(ctx, `
INSERT INTO parties (party_type, display_name, status) VALUES ('org', 'Date vendor', 'active')
RETURNING party_id::text`).Scan(&vendor); err != nil {
		t.Fatalf("seed vendor: %v", err)
	}
	if err := pool.QueryRow(ctx, `
INSERT INTO procurement_loads (tenant_id, source_party_id, purchase_date, status, idempotency_key, context)
VALUES ($1::uuid, $2::uuid, '2026-06-01', 'accepted_intake', 'unnumbered-load', '{}'::jsonb)
RETURNING load_id::text`, countsTenant, vendor).Scan(&unnumbered); err != nil {
		t.Fatalf("seed load: %v", err)
	}
	dead := func(n int, park, shed string) string {
		t.Helper()
		id := mortalityGoatID(n)
		if _, err := pool.Exec(ctx, `
INSERT INTO goats (
  goat_id, tenant_id, display_id, species, breed, sex, lifecycle_status, age_band, dob, origin_type,
  custodian_party_id, park_id, shed_id, management_stage, exit_reason, exited_at
) VALUES (
  $1::uuid, $2::uuid, $3, 'goat', 'Beetal', 'male', 'dead', 'adult', '2025-01-01'::date, 'procured',
  '00000000-0000-4000-8000-000000001001'::uuid, $4::uuid, $5::uuid, 'F2-Male', 'died',
  ($6::date + time '10:00') AT TIME ZONE 'Asia/Kolkata'
)`, id, countsTenant, fmt.Sprintf("G-88%04d", n), park, shed, died); err != nil {
			t.Fatalf("seed goat %d: %v", n, err)
		}
		return id
	}
	dead(1, countsPark, countsShedCastroOne)
	onLoad := dead(2, countsParkTwo, countsShedCastroTwo)
	if _, err := pool.Exec(ctx, `
INSERT INTO procurement_load_goats (tenant_id, load_id, goat_id, selection_state, current_state, intake_accepted_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'accepted_herd_intake', 'accepted_herd_intake', now())`, countsTenant, unnumbered, onLoad); err != nil {
		t.Fatalf("attach goat to load: %v", err)
	}

	mort, err := repo.GetMortality(ctx, domain.MortalityQuery{TenantID: countsTenant, FromDate: from, ToDate: to})
	if err != nil {
		t.Fatalf("mortality: %v", err)
	}
	codes := map[string]string{}
	for _, park := range mort.Park {
		codes[park.Key] = park.Label
	}
	one, two := codes[countsPark], codes[countsParkTwo]
	if one == "" || two == "" || one == two {
		t.Fatalf("park series did not resolve two park codes: %+v", mort.Park)
	}
	pens := map[string]bool{}
	for _, pen := range mort.Pen {
		pens[pen.Label] = true
	}
	for _, want := range []string{one + " · Castro 1", two + " · Castro 1"} {
		if !pens[want] {
			t.Errorf("pen rows = %v, want %q: the same-named pens of two farms must each name their farm", pens, want)
		}
	}

	const wantLoad = "Load 01/06/2026"
	var loadSeen bool
	for _, load := range mort.Load {
		if load.Key == unnumbered {
			loadSeen = load.Label == wantLoad
			if !loadSeen {
				t.Errorf("unnumbered load label = %q, want %q", load.Label, wantLoad)
			}
		}
	}
	if !loadSeen {
		t.Errorf("load series %+v has no row for the unnumbered load", mort.Load)
	}
	var listed bool
	for _, d := range mort.Deaths {
		if d.GoatID == onLoad {
			listed = true
			if d.LoadRef != wantLoad {
				t.Errorf("deaths list load = %q, want %q", d.LoadRef, wantLoad)
			}
		}
	}
	if !listed {
		t.Errorf("deaths list %+v is missing the animal on the unnumbered load", mort.Deaths)
	}
}

// ONE-TO-MANY: an animal carries MANY history spans (K3 -> ICU -> K3, and two pen moves out and
// back), and the spans are joined per animal. It must still be ONE of K3's animals and ONE of
// its pen's, never one per span -- the fan-out a span join invites.
func TestMortalityOneToManyHistorySpansCountAnAnimalOnce(t *testing.T) {
	ctx := context.Background()
	repo, pool := newHerdAnalyticsRepo(t, ctx)
	from, to := "2026-07-01", "2026-07-31"

	id := mortalityGoatID(980)
	insertMortalityGoat(t, ctx, pool, id, "Beetal", "female", "K3", "kid", "2026-03-01", "birth", "died", "2026-07-25")
	recordStageChange(t, ctx, pool, id, "2026-07-05", "K3", "ICU")
	recordStageChange(t, ctx, pool, id, "2026-07-12", "ICU", "K3")
	recordPenMove(t, ctx, pool, id, "2026-07-05", countsShedA, countsShedB)
	recordPenMove(t, ctx, pool, id, "2026-07-12", countsShedB, countsShedA)

	mort, err := repo.GetMortality(ctx, domain.MortalityQuery{TenantID: countsTenant, FromDate: from, ToDate: to})
	if err != nil {
		t.Fatalf("mortality: %v", err)
	}
	if k3, ok := stageBucket(mort, "K3"); !ok || k3.Animals != 1 || k3.Deaths != 1 {
		t.Fatalf("K3 %+v, want 1 death of 1 (two K3 spans are one animal)", k3)
	}
	if icu, ok := stageBucket(mort, "ICU"); !ok || icu.Animals != 1 || icu.Deaths != 0 {
		t.Fatalf("ICU %+v, want 0 deaths of 1", icu)
	}
	if pen, ok := penBucket(mort, "CPT Shed 1"); !ok || pen.Animals != 1 || pen.Deaths != 1 {
		t.Fatalf("CPT Shed 1 %+v in %+v, want 1 death of 1 (two spans are one animal)", pen, mort.Pen)
	}
	if mort.Totals.Animals != 1 || mort.Totals.KidAnimals != 1 {
		t.Fatalf("totals %+v, want one animal, one kid", mort.Totals)
	}
	assertDeathsNeverExceedAnimals(t, mort)
}

// PAGE BOUNDARY under history: paging the deaths list must not move any animals figure, now
// that those figures come from history spans rather than the animal rows the list reads.
func TestMortalityPageBoundaryLeavesHistoryAnimalsUntouched(t *testing.T) {
	ctx := context.Background()
	repo, pool := newHerdAnalyticsRepo(t, ctx)
	from, to := "2026-07-01", "2026-07-31"
	for i := 0; i < 12; i++ {
		id := mortalityGoatID(1000 + i)
		insertMortalityGoat(t, ctx, pool, id, "Beetal", "female", "F2-Female", "adult", "2025-01-01", "procured", "died", fmt.Sprintf("2026-07-%02d", 10+i))
		recordStageChange(t, ctx, pool, id, "2026-07-02", "K3", "F2-Female")
	}
	first, err := repo.GetMortality(ctx, domain.MortalityQuery{TenantID: countsTenant, FromDate: from, ToDate: to, RecentLimit: 10})
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	second, err := repo.GetMortality(ctx, domain.MortalityQuery{TenantID: countsTenant, FromDate: from, ToDate: to, RecentLimit: 10, RecentOffset: 10})
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if len(first.Deaths) != 10 || len(second.Deaths) != 2 {
		t.Fatalf("pages hold %d and %d deaths, want 10 and 2", len(first.Deaths), len(second.Deaths))
	}
	k3a, _ := stageBucket(first, "K3")
	k3b, _ := stageBucket(second, "K3")
	if k3a.Animals != 12 || k3b.Animals != 12 || first.Totals.KidAnimals != second.Totals.KidAnimals {
		t.Fatalf("K3 animals %d / %d, kids %d / %d across pages, want 12 on both and equal kids", k3a.Animals, k3b.Animals, first.Totals.KidAnimals, second.Totals.KidAnimals)
	}
}

// PARK SCOPE under history: a park filter narrows the history spans too. An animal in the
// other park, with its own stage history, must add nothing to this park's stage or pen animals.
func TestMortalityParkScopeNarrowsHistorySpans(t *testing.T) {
	ctx := context.Background()
	repo, pool := newHerdAnalyticsRepo(t, ctx)
	seedSameNamedShedsInTwoParks(t, ctx, pool)
	from, to := "2026-07-01", "2026-07-31"

	here, there := mortalityGoatID(1100), mortalityGoatID(1101)
	insertMortalityGoat(t, ctx, pool, here, "Beetal", "female", "F2-Female", "adult", "2025-01-01", "procured", "died", "2026-07-20")
	recordStageChange(t, ctx, pool, here, "2026-07-05", "K3", "F2-Female")
	insertMortalityGoat(t, ctx, pool, there, "Beetal", "female", "F2-Female", "adult", "2025-01-01", "procured", "", "")
	if _, err := pool.Exec(ctx, `UPDATE goats SET park_id = $2::uuid, shed_id = $3::uuid WHERE tenant_id = $1::uuid AND goat_id = $4::uuid`,
		countsTenant, countsParkTwo, countsShedCastroTwo, there); err != nil {
		t.Fatalf("move to the other park: %v", err)
	}
	recordStageChange(t, ctx, pool, there, "2026-07-05", "K3", "F2-Female")

	cpt := countsPark
	scoped, err := repo.GetMortality(ctx, domain.MortalityQuery{TenantID: countsTenant, ParkID: &cpt, FromDate: from, ToDate: to})
	if err != nil {
		t.Fatalf("scoped: %v", err)
	}
	for _, key := range []string{"K3", "F2-Female"} {
		if b, ok := stageBucket(scoped, key); !ok || b.Animals != 1 {
			t.Fatalf("%s under the park scope %+v, want 1 animal (the other park's animal leaked in)", key, b)
		}
	}
	if scoped.Totals.Animals != 1 || len(scoped.Pen) != 1 || scoped.Pen[0].Animals != 1 {
		t.Fatalf("scoped totals %+v pens %+v, want 1 animal in 1 pen", scoped.Totals, scoped.Pen)
	}
}

// STATUS MATRIX under the new rule: every exit inside the window was on the farm until it left,
// so it is one of the animals whatever the reason; only a death is a death. And an animal that
// is sick, under treatment, in quarantine or in ICU is still on the farm and is one of the
// animals -- the old live-only divisor left all four out.
func TestMortalityStatusMatrixEveryStatusOnTheFarmIsAnAnimal(t *testing.T) {
	ctx := context.Background()
	repo, pool := newHerdAnalyticsRepo(t, ctx)
	from, to := "2026-07-01", "2026-07-31"

	onFarm := []string{"alive", "sick", "under_treatment", "quarantine", "icu"}
	for i, status := range onFarm {
		id := mortalityGoatID(1200 + i)
		insertMortalityGoat(t, ctx, pool, id, "Beetal", "female", "F2-Female", "adult", "2025-01-01", "procured", "", "")
		if _, err := pool.Exec(ctx, `UPDATE goats SET lifecycle_status = $2 WHERE goat_id = $1::uuid`, id, status); err != nil {
			t.Fatalf("status %s: %v", status, err)
		}
	}
	// Exits BEFORE the window: history, not July's animals.
	for i, reason := range []string{"sold", "culled", "died"} {
		insertMortalityGoat(t, ctx, pool, mortalityGoatID(1210+i), "Beetal", "female", "F2-Female", "adult", "2025-01-01", "procured", reason, "2026-06-15")
	}
	// Exits INSIDE the window: all July's animals, one of them a death.
	for i, reason := range []string{"sold", "culled", "transferred", "lost", "died"} {
		insertMortalityGoat(t, ctx, pool, mortalityGoatID(1220+i), "Beetal", "female", "F2-Female", "adult", "2025-01-01", "procured", reason, "2026-07-15")
	}

	mort, err := repo.GetMortality(ctx, domain.MortalityQuery{TenantID: countsTenant, FromDate: from, ToDate: to})
	if err != nil {
		t.Fatalf("mortality: %v", err)
	}
	if mort.Totals.Deaths != 1 || mort.Totals.Animals != 10 {
		t.Fatalf("totals %+v, want 1 death of 10 (five on the farm in any status, five exits inside July)", mort.Totals)
	}
	if f2, ok := stageBucket(mort, "F2-Female"); !ok || f2.Animals != 10 || f2.Deaths != 1 {
		t.Fatalf("F2-Female %+v, want 1 death of 10", f2)
	}
}
