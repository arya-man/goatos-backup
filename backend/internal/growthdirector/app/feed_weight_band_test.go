package app

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/growthdirector/domain"
	"github.com/vgoats/goatos/backend/internal/growthdirector/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
)

func feedSourceFixture() ports.FeedWeightBandSource {
	items := []ports.FeedRollupItem{
		{Label: "Dry Masoor Bhusa", GramsPerHead: 400},
		{Label: "Mesha Kids Concentrate", GramsPerHead: 250.4},
	}
	exitedAt := time.Date(2026, 9, 1, 6, 0, 0, 0, time.UTC)
	weighedAt := time.Date(2026, 8, 20, 6, 0, 0, 0, time.UTC)
	return ports.FeedWeightBandSource{
		FeedDay: "2026-09-10", PositiveRows: 10, CollapsedItems: 6,
		IndividualAnimalsWeighed: 9, LumpSumAnimalsWeighed: 40,
		Rollups: []ports.FeedRollup{
			// A pen-average pen: one evidence row; the register says every resident is male.
			{ParkID: gdParkA, ParkName: "Coimbatore", Pen: "Castro 1", ShedTag: "F2-Male", RationGroup: "Fattening", Breed: "Beetal x Sojat", Workflow: "normal", KgPerDay: 12.5, Items: items,
				Evidence: []ports.FeedWeightEvidence{{Source: domain.FeedBandSourcePenAverage, Band: "25_30", Animals: 40, AverageWeightKg: 27.2, MaleCount: 38, AnimalsAll: 40, AverageWeightKgAll: 27.2, MaleCountAll: 38}}},
			// A per-animal pen with two kid rollups that would read identically: two bands
			// each, the 15-20 band mixed with two sold animals beside it, the under-15 band
			// all female.
			{ParkID: gdParkA, ParkName: "Coimbatore", Pen: "Godel 1 - Part 2", ShedTag: "K3", RationGroup: "Kid", Breed: "Sojat", Workflow: "normal", KgPerDay: 3, Items: items[1:],
				Evidence: []ports.FeedWeightEvidence{{Source: domain.FeedBandSourcePerAnimal, Band: "15_20", Animals: 3, AverageWeightKg: 17, FemaleCount: 2, MaleCount: 1, AnimalsAll: 5, AverageWeightKgAll: 17.4, FemaleCountAll: 3, MaleCountAll: 2, ExitedAnimals: 2, ExitedSold: 1}, {Source: domain.FeedBandSourcePerAnimal, Band: "under_15", Animals: 2, AverageWeightKg: 12, FemaleCount: 2, AnimalsAll: 2, AverageWeightKgAll: 12, FemaleCountAll: 2}}},
			{ParkID: gdParkA, ParkName: "Coimbatore", Pen: "Godel 1 - Part 2", ShedTag: "ICU-Kid", RationGroup: "Kid", Breed: "Sojat", Workflow: "normal", KgPerDay: 1, Items: items[1:],
				Evidence: []ports.FeedWeightEvidence{{Source: domain.FeedBandSourcePerAnimal, Band: "15_20", Animals: 3, AverageWeightKg: 17, FemaleCount: 2, MaleCount: 1, AnimalsAll: 5, AverageWeightKgAll: 17.4, FemaleCountAll: 3, MaleCountAll: 2, ExitedAnimals: 2, ExitedSold: 1}, {Source: domain.FeedBandSourcePerAnimal, Band: "under_15", Animals: 2, AverageWeightKg: 12, FemaleCount: 2, AnimalsAll: 2, AverageWeightKgAll: 12, FemaleCountAll: 2}}},
			// A fed pen nobody has weighed in the period: excluded, counted.
			{ParkID: gdParkA, ParkName: "Coimbatore", Pen: "Sumathi 1 - Part 4", ShedTag: "F2-Female", RationGroup: "Fattening", Breed: "Sojat", Workflow: "experiment", ExperimentArm: "Arm A", KgPerDay: 9, Items: items},
		},
		Exited: []ports.FeedExitedAnimal{
			{GoatID: "g1", ParkID: gdParkA, Tag: "TAG-SOLD", Pen: "Godel 1 - Part 2", Sex: "female", ExitReason: "sold", LifecycleStatus: "sold", ExitedAt: exitedAt, LastWeighedAt: &weighedAt, LastWeightKg: 18.5},
			{GoatID: "g2", ParkID: gdParkA, Tag: "TAG-DEAD", ExitReason: "", LifecycleStatus: "dead", ExitedAt: exitedAt},
			// An exit that is neither sold nor died: the register's inactive record with a blank
			// reason (13 of the 120 exits on the OCI clone, 2026-09-18). Bucket "other".
			{GoatID: "g3", ParkID: gdParkA, Tag: "TAG-INACTIVE", ExitReason: "", LifecycleStatus: "inactive", ExitedAt: exitedAt},
		},
	}
}

func TestBuildFeedWeightBandReconcilesAndOrders(t *testing.T) {
	got := BuildFeedWeightBand(feedSourceFixture())
	rec := got.Reconciliation
	if rec.FeedDay != "2026-09-10" || rec.PositiveRows != 10 || rec.CollapsedItems != 6 {
		t.Fatalf("sheet stages not carried: %+v", rec)
	}
	if rec.Rollups != 4 || rec.MatchedRollups != 3 || rec.ExcludedRollups != 1 || rec.OutputRows != 5 {
		t.Fatalf("reconciliation wrong: %+v", rec)
	}
	// The General-tab figures and the exit count ride on the reconciliation verbatim.
	if rec.IndividualAnimalsWeighed != 9 || rec.LumpSumAnimalsWeighed != 40 || rec.ExitedAnimals != 3 {
		t.Fatalf("weighing-side totals wrong: %+v", rec)
	}
	if len(got.Rows) != 5 {
		t.Fatalf("want 5 rows, got %d", len(got.Rows))
	}
	first := got.Rows[0]
	if first.WeightSource != domain.FeedBandSourcePenAverage || first.Pen != "Castro 1" {
		t.Fatalf("pen-average rows must come first: %+v", first)
	}
	if first.Group != "Fattening" || first.Gender != "Male" || first.Breed != "Beetal cross Sojat" {
		t.Fatalf("display derivation wrong: %+v", first)
	}
	if first.FeedGiven != "Bhusa 400g/head + Kids Concentrate 250g/head" {
		t.Fatalf("feed given wrong: %q", first.FeedGiven)
	}
	// Per-animal rows: band ascending inside the pen, and the two kid rollups carry
	// their shed tag so they do not read as one row repeated.
	if got.Rows[1].Band != "under_15" || got.Rows[2].Band != "under_15" || got.Rows[3].Band != "15_20" {
		t.Fatalf("bands not ascending: %+v", got.Rows[1:])
	}
	groups := map[string]bool{}
	genders := map[string]string{}
	exited := map[string]int{}
	for _, row := range got.Rows[1:] {
		groups[row.Group] = true
		genders[row.Band] = row.Gender
		exited[row.Band] = row.ExitedAnimals
	}
	if !groups["Kid (K3)"] || !groups["Kid (ICU-Kid)"] {
		t.Fatalf("identical kid rollups not disambiguated: %v", groups)
	}
	// Gender comes from the animals: the 15-20 kid band holds two females and a male.
	if genders["15_20"] != "Mixed 2F·1M" || genders["under_15"] != "Female" {
		t.Fatalf("gender must be read off the register counts, got %v", genders)
	}
	// The sold animals ride on the band row as a note, outside the head count, split sold / died.
	if exited["15_20"] != 2 || exited["under_15"] != 0 {
		t.Fatalf("exited note wrong: %v", exited)
	}
	for _, row := range got.Rows[1:] {
		// Fixture: 2 exited, 1 sold, 0 died -> the second is OTHER, never invented as died.
		if row.Band == "15_20" && (row.ExitedSold != 1 || row.ExitedDied != 0 || row.ExitedOther != 1) {
			t.Fatalf("sold/died/other split wrong: %+v", row)
		}
		if row.ExitedSold+row.ExitedDied+row.ExitedOther != row.ExitedAnimals {
			t.Fatalf("exit buckets must sum to exited_animals: %+v", row)
		}
	}
	// The period totals bucket the same way and sum to the exit count.
	if rec.ExitedSold != 1 || rec.ExitedDied != 1 || rec.ExitedOther != 1 || rec.ExitedSold+rec.ExitedDied+rec.ExitedOther != rec.ExitedAnimals {
		t.Fatalf("reconciliation exit buckets: want 1/1/1 of 3, got %+v", rec)
	}
	// Weighed / not weighed split: only TAG-SOLD carries a weigh in the period.
	if rec.ExitedWeighed != 1 || rec.ExitedNotWeighed != 2 || rec.ExitedWeighed+rec.ExitedNotWeighed != rec.ExitedAnimals {
		t.Fatalf("reconciliation weighed split: want 1 weighed + 2 not weighed = 3, got %+v", rec)
	}
	// The unweighed pen is listed under Not shown with its feed, and nowhere else.
	if len(got.Unmatched) != 1 || got.Unmatched[0].Pen != "Sumathi 1 - Part 4" || got.Unmatched[0].FeedType != "experiment" || got.Unmatched[0].Group != "Fattening" || got.Unmatched[0].FeedGiven != "Bhusa 400g/head + Kids Concentrate 250g/head" {
		t.Fatalf("not-shown list wrong: %+v", got.Unmatched)
	}
	// The exit list: dates as business dates, the weighed one banded, the unweighed one bare.
	if len(got.Exited) != 3 {
		t.Fatalf("want 3 exits, got %+v", got.Exited)
	}
	sold, dead, other := got.Exited[0], got.Exited[1], got.Exited[2]
	if !sold.WeighedInPeriod || dead.WeighedInPeriod || other.WeighedInPeriod {
		t.Fatalf("weighed_in_period must follow the last weigh: %+v", got.Exited)
	}
	if sold.Bucket != domain.FeedExitSold || dead.Bucket != domain.FeedExitDied || other.Bucket != domain.FeedExitOther || other.LifecycleStatus != "inactive" {
		t.Fatalf("exit buckets: want sold/died/other with the stored text kept, got %+v", got.Exited)
	}
	if sold.Tag != "TAG-SOLD" || sold.ExitedAt != "2026-09-01" || sold.LastWeighedAt != "2026-08-20" || sold.LastBand != "15_20" || sold.LastWeightKg == nil || *sold.LastWeightKg != 18.5 {
		t.Fatalf("sold exit wrong: %+v", sold)
	}
	// Gender from the register, and the pen's feed today from that pen's first rollup.
	if sold.Gender != "Female" || sold.ParkName != "Coimbatore" || sold.FeedType != "normal" || sold.FeedGiven != "Kids Concentrate 250g/head" {
		t.Fatalf("sold exit context wrong: %+v", sold)
	}
	if dead.Tag != "TAG-DEAD" || dead.LifecycleStatus != "dead" || dead.LastWeighedAt != "" || dead.LastBand != "" || dead.LastWeightKg != nil {
		t.Fatalf("unweighed dead exit must carry no weigh: %+v", dead)
	}
}

func TestBuildFeedWeightBandCarriesBothHeadCountVariants(t *testing.T) {
	src := feedSourceFixture()
	// A band whose only weighed animals have left: on-farm 0, all 2 -- the row is served, the
	// rollup counts as matched only under "all".
	src.Rollups = append(src.Rollups, ports.FeedRollup{ParkID: gdParkA, ParkName: "Coimbatore", Pen: "Yashoda 9", ShedTag: "F2-Male", RationGroup: "Fattening", Breed: "Sojat", Workflow: "normal", KgPerDay: 4,
		Items:    []ports.FeedRollupItem{{Label: "Mesha Bhusa", GramsPerHead: 300}},
		Evidence: []ports.FeedWeightEvidence{{Source: domain.FeedBandSourcePerAnimal, Band: "25_30", Animals: 0, AverageWeightKg: 0, AnimalsAll: 2, AverageWeightKgAll: 27, MaleCountAll: 2, ExitedAnimals: 2, ExitedSold: 2}}})
	got := BuildFeedWeightBand(src)
	rec := got.Reconciliation
	if rec.Rollups != 5 || rec.MatchedRollups != 3 || rec.ExcludedRollups != 2 || rec.MatchedRollupsAll != 4 || rec.ExcludedRollupsAll != 1 || rec.OutputRows != 6 {
		t.Fatalf("two-variant reconciliation wrong: %+v", rec)
	}
	if len(got.Unmatched) != 1 || got.Unmatched[0].Pen != "Sumathi 1 - Part 4" {
		t.Fatalf("only the never-weighed pen is Not shown on the wire: %+v", got.Unmatched)
	}
	var gone *domain.FeedWeightBandRow
	for i := range got.Rows {
		if got.Rows[i].Pen == "Yashoda 9" {
			gone = &got.Rows[i]
		}
	}
	if gone == nil || gone.WeightAnimals != 0 || gone.WeightAnimalsAll != 2 || gone.AverageWeightKgAll != 27 || gone.GenderAll != "Male" || gone.Gender != "" || gone.ExitedSold != 2 {
		t.Fatalf("exited-only band row must carry the all-variant: %+v", gone)
	}
}

func TestShedTagGroupAndGenderDisplay(t *testing.T) {
	cases := []struct{ tag, group string }{
		{"F2-Male", "Fattening"},
		{"F2-Female", "Fattening"},
		{"K3", "Kid"},
		{"ICU-Kid", "Kid"},
		{"Buck", "Buck"},
		{"Mother", "Mother"},
		{"Non-Pregnant", "Non-Pregnant"},
		{"Warmup", "Warmup"},
		{"F2-Male + K3", "Fattening + Kid"},
		{"F2-Male + F2-Female", "Fattening"},
		{"K3 + K1", "Kid"},
		{"Something New", "Something New"},
	}
	for _, c := range cases {
		if group := domain.ShedTagGroup(c.tag); group != c.group {
			t.Errorf("%q: want group %q got %q", c.tag, c.group, group)
		}
	}
	genders := []struct {
		f, m int
		want string
	}{{0, 0, ""}, {3, 0, "Female"}, {0, 7, "Male"}, {12, 10, "Mixed 12F·10M"}}
	for _, g := range genders {
		if got := domain.GenderDisplay(g.f, g.m); got != g.want {
			t.Errorf("GenderDisplay(%d,%d): want %q got %q", g.f, g.m, g.want, got)
		}
	}
}

func TestGetFeedWeightBandGateWindowAndFilters(t *testing.T) {
	repo := &fakeRepo{parks: []domain.Park{{ParkID: gdParkA, Name: "A"}, {ParkID: gdParkB, Name: "B"}}, feedSource: feedSourceFixture()}
	svc := NewService(repo)
	tenantWide := gdContext(permissions.ActiveGrant{Role: permissions.RoleGrowthDirector, ScopeType: "tenant", ScopeID: gdTenant})
	if _, err := svc.GetFeedWeightBand(context.Background(), domain.Actor{TenantID: gdTenant, Roles: []string{permissions.RoleOperator}}, "", "", "", "", "", ""); err != ports.ErrForbidden {
		t.Fatalf("non-monitor must be forbidden, got %v", err)
	}
	if _, err := svc.GetFeedWeightBand(tenantWide, gdActor(), "", "2026-13-01", "", "", "", ""); err != ports.ErrInvalidArgument {
		t.Fatalf("bad date must be rejected, got %v", err)
	}
	if _, err := svc.GetFeedWeightBand(tenantWide, gdActor(), "", "", "", "unknown", "", ""); err != ports.ErrInvalidArgument {
		t.Fatalf("bad sex must be rejected, got %v", err)
	}
	if _, err := svc.GetFeedWeightBand(tenantWide, gdActor(), "", "", "", "", "imported", ""); err != ports.ErrInvalidArgument {
		t.Fatalf("bad origin must be rejected, got %v", err)
	}
	if _, err := svc.GetFeedWeightBand(tenantWide, gdActor(), "", "", "", "", "", "by_hand"); err != ports.ErrInvalidArgument {
		t.Fatalf("bad weighing category must be rejected, got %v", err)
	}
	got, err := svc.GetFeedWeightBand(tenantWide, gdActor(), "", "2026-09-01", "2026-09-10", "male", "purchased", "all")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.gotParkIDs) != 2 {
		t.Fatalf("tenant-wide read must cover every park, got %v", repo.gotParkIDs)
	}
	if repo.gotStart.Format("2006-01-02") != "2026-09-01" || repo.gotEnd.Format("2006-01-02") != "2026-09-11" {
		t.Fatalf("window must be inclusive from, exclusive day after to: %v .. %v", repo.gotStart, repo.gotEnd)
	}
	// The filters reach the repository as the weighing resolvers expect them: "all" is blank.
	if repo.gotSex != "male" || repo.gotOrigin != "purchased" || repo.gotMode != "" {
		t.Fatalf("filters must reach the repository normalised, got sex=%q origin=%q mode=%q", repo.gotSex, repo.gotOrigin, repo.gotMode)
	}
	if got.Reconciliation.OutputRows != 5 {
		t.Fatalf("service must build rows from the source: %+v", got.Reconciliation)
	}
}

func TestFeedExitBucketFoldsRegisterWordsToThreeBuckets(t *testing.T) {
	cases := []struct{ ls, er, want string }{
		{"sold", "sold", domain.FeedExitSold},
		{"", "sold", domain.FeedExitSold},
		{"dead", "died", domain.FeedExitDied},
		{"dead", "", domain.FeedExitDied},
		{"", "died", domain.FeedExitDied},
		{"inactive", "", domain.FeedExitOther},
		{"", "transferred", domain.FeedExitOther},
		{"", "culled", domain.FeedExitOther},
		{"", "", domain.FeedExitOther},
	}
	for _, c := range cases {
		if got := domain.FeedExitBucket(c.ls, c.er); got != c.want {
			t.Fatalf("FeedExitBucket(%q, %q) = %q, want %q", c.ls, c.er, got, c.want)
		}
	}
}
