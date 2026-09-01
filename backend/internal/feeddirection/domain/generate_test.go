package domain

import (
	"testing"
)

// These tests are the executable statement of the resolution rule. They run WITHOUT Docker,
// because generation is a pure function of two inputs and none of its business rules -- least of
// all the blocked-vs-zero rule -- should need a database to prove.

const (
	testParkID = "00000000-0000-4000-8000-000000003001"
	testShedID = "00000000-0000-4000-8000-000000004001"
)

// testConfig mirrors the shape of the live authored grid: the real tag vocabulary split by
// applies_to, the real Beetal+Sirohi merge, and a small item catalog.
func testConfig() ConfigSnapshot {
	cfg := ConfigSnapshot{
		ParkID:    testParkID,
		ParkLabel: "CPT",
		ShedTagsByKey: map[string]ShedTag{
			"non_pregnant": {Label: "Non-Pregnant", AppliesTo: AppliesToAdult},
			// 'Mother' is an ADULT tag. Five live animals carry it while their stored age_band says
			// 'kid'; the tag wins. See TestKidAdultBranchComesFromAppliesToNotAgeBand.
			"mother": {Label: "Mother", AppliesTo: AppliesToAdult},
			// 'ICU-Kid' is a KID tag. One live animal carries it while its stored age_band says
			// 'adult'; the tag wins here too.
			"icu_kid": {Label: "ICU-Kid", AppliesTo: AppliesToKid},
			"f2_male": {Label: "F2-Male", AppliesTo: AppliesToKid},
			"k0":      {Label: "K0", AppliesTo: AppliesToKid},
		},
		RationGroupByBreedKey: map[string]string{
			"anantapur_sheep": "Anantapur Sheep",
			// The real merge: two distinct live breeds, one ration group.
			"beetal": "Beetal/Sirohi",
			"sirohi": "Beetal/Sirohi",
		},
		// The CATALOG -- the tenant's whole feed vocabulary. "Dry Maize" is in it and is deliberately
		// NOT declared as a slot below: it is one of the substitution roughages, and its presence
		// here is what makes TestUndeclaredCatalogItemIsAbsentNotBlocked meaningful.
		FeedItems: []FeedItem{
			{Label: "Concentrate", Key: "concentrate"},
			{Label: "Hybrid", Key: "hybrid"},
			{Label: "Dry Maize", Key: "dry_maize"},
		},
		RatesByKey:       map[string]RationRate{},
		ShedFactorsByKey: map[string]string{},
		// The RECIPE -- what each session is actually made of, in packing-slot order. Both sessions
		// declare the same slots, mirroring the live CBE/CPT template.
		Sessions: []SessionTemplate{
			{SessionNo: 1, Label: "Morning", SplitFraction: "0.5000", Items: testSlots()},
			{SessionNo: 2, Label: "Evening", SplitFraction: "0.5000", Items: testSlots()},
		},
		ExperimentByLocation: map[string][]ExperimentCell{},
	}
	return cfg
}

// testSlots is the declared feed-slot list both test sessions carry, in packing order. Returned
// fresh each call so a test that edits one session's slots cannot mutate the other's.
func testSlots() []FeedItem {
	return []FeedItem{
		{Label: "Concentrate", Key: "concentrate"},
		{Label: "Hybrid", Key: "hybrid"},
	}
}

// withSlots replaces the declared slots on EVERY session -- the "what does this session consist of"
// half of the config, as distinct from withRate's "how much of it".
func withSlots(cfg ConfigSnapshot, items ...FeedItem) ConfigSnapshot {
	sessions := make([]SessionTemplate, 0, len(cfg.Sessions))
	for _, session := range cfg.Sessions {
		session.Items = append([]FeedItem(nil), items...)
		sessions = append(sessions, session)
	}
	cfg.Sessions = sessions
	return cfg
}

func withRate(cfg ConfigSnapshot, group, tag, item, grams string) ConfigSnapshot {
	cfg.RatesByKey[RateKey(group, tag, item)] = RationRate{GramsPerHead: grams}
	return cfg
}

func shed(grains ...ShedGrain) ShedInput {
	return ShedInput{ShedID: testShedID, ShedLabel: "Shed 1", Grains: grains}
}

func generate(cfg ConfigSnapshot, in ShedInput, sessionNo int32) []DirectionRow {
	return GenerateDirection(GenerateInput{
		Config:    cfg,
		Sheds:     []ShedInput{in},
		SessionNo: sessionNo,
		Rounding:  StandardRoundingPolicy(),
		Planners:  NewPlannerSet(),
	})
}

func findRow(t *testing.T, rows []DirectionRow, tag string, sessionNo int32) DirectionRow {
	t.Helper()
	for _, row := range rows {
		if row.ShedTag == tag && row.SessionNo == sessionNo {
			return row
		}
	}
	t.Fatalf("no row for tag %q session %d in %d rows", tag, sessionNo, len(rows))
	return DirectionRow{}
}

func itemOf(t *testing.T, row DirectionRow, label string) ItemQuantity {
	t.Helper()
	for _, item := range row.Items {
		if item.FeedItem == label {
			return item
		}
	}
	t.Fatalf("row %q has no item %q", row.ShedTag, label)
	return ItemQuantity{}
}

// ---------------------------------------------------------------------------
// The kid/adult branch
// ---------------------------------------------------------------------------

// The maintainer decision this whole module turns on: the kid/adult branch comes from
// feed_shed_tags.applies_to, NOT from goats.age_band.
//
// The two disagree on 6 live animals. The generator's input type has NO age_band field at all,
// which is the structural half of the guarantee; these cases are the behavioural half. Both
// contradictions are reproduced exactly as they appear in live data.
func TestKidAdultBranchComesFromAppliesToNotAgeBand(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		// stage/breed are the live free-text values. ageBandWouldSay documents the CONTRADICTING
		// stored value so the case is self-explaining; it is deliberately not an input, because the
		// generator must never be able to consult it.
		stage           string
		breed           string
		ageBandWouldSay string
		wantTag         string
		wantGroup       string
	}{
		{
			name:            "5 live animals: Mother tag with a kid age_band resolves ADULT via the tag",
			stage:           "Mother",
			breed:           "Anantapur Sheep",
			ageBandWouldSay: "kid",
			wantTag:         "Mother",
			// The adult branch consults the breed map. Had age_band won, this would have been "Kid".
			wantGroup: "Anantapur Sheep",
		},
		{
			name:            "1 live animal: ICU-Kid tag with an adult age_band resolves KID via the tag",
			stage:           "ICU- kid",
			breed:           "Anantapur Sheep",
			ageBandWouldSay: "adult",
			wantTag:         "ICU-Kid",
			// Kids of EVERY breed collapse to the single 'Kid' group and never touch the breed map.
			wantGroup: KidRationGroupLabel,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := testConfig()
			cfg = withRate(cfg, "Anantapur Sheep", "Mother", "Concentrate", "300.000")
			cfg = withRate(cfg, "Anantapur Sheep", "Mother", "Hybrid", "1000.000")
			cfg = withRate(cfg, KidRationGroupLabel, "ICU-Kid", "Concentrate", "50.000")
			cfg = withRate(cfg, KidRationGroupLabel, "ICU-Kid", "Hybrid", "200.000")

			rows := generate(cfg, shed(ShedGrain{ManagementStage: tc.stage, Breed: tc.breed, HeadCount: 10}), 1)
			if len(rows) != 1 {
				t.Fatalf("rows = %d, want 1", len(rows))
			}
			row := rows[0]
			if row.ShedTag != tc.wantTag {
				t.Fatalf("shed_tag = %q, want %q (the AUTHORED label, not the raw source text)", row.ShedTag, tc.wantTag)
			}
			if row.RationGroup != tc.wantGroup {
				t.Fatalf("ration_group = %q, want %q; stored age_band says %q and must be ignored",
					row.RationGroup, tc.wantGroup, tc.ageBandWouldSay)
			}
			if row.Blocked {
				t.Fatalf("row blocked; both contradictions must RESOLVE, not stall the shed")
			}
		})
	}
}

// Beetal and Sirohi are two live breeds sharing one ration group. Both must resolve, and both must
// stay visible as their own breed -- an operator needs to know which animals are in the shed.
func TestBeetalAndSirohiBothResolveToTheMergedRationGroup(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg = withRate(cfg, "Beetal/Sirohi", "Non-Pregnant", "Concentrate", "200.000")
	cfg = withRate(cfg, "Beetal/Sirohi", "Non-Pregnant", "Hybrid", "1000.000")

	rows := generate(cfg, shed(
		ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Beetal", HeadCount: 10},
		ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Sirohi", HeadCount: 4},
	), 1)

	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 (one per breed, both resolving to the merged group)", len(rows))
	}
	byBreed := map[string]DirectionRow{}
	for _, row := range rows {
		byBreed[row.Breed] = row
	}
	for _, breed := range []string{"Beetal", "Sirohi"} {
		row, ok := byBreed[breed]
		if !ok {
			t.Fatalf("no row for breed %q", breed)
		}
		if row.RationGroup != "Beetal/Sirohi" {
			t.Fatalf("breed %q resolved to group %q, want \"Beetal/Sirohi\"", breed, row.RationGroup)
		}
		if row.Blocked {
			t.Fatalf("breed %q blocked; the merge must resolve", breed)
		}
	}
	// 10 head x 200 g x 0.5 = 1000 g; 4 head x 200 g x 0.5 = 400 g. Both exact multiples of 100 g.
	if got := *itemOf(t, byBreed["Beetal"], "Concentrate").QuantityKg; got != "1.000" {
		t.Fatalf("Beetal concentrate = %q, want \"1.000\"", got)
	}
	if got := *itemOf(t, byBreed["Sirohi"], "Concentrate").QuantityKg; got != "0.400" {
		t.Fatalf("Sirohi concentrate = %q, want \"0.400\"", got)
	}
}

// The live tag vocabulary carries cosmetic separator/case variants of the SAME tag. A missed match
// here is not a loud failure: it blocks a shed whose ration is perfectly well authored.
func TestLabelVariantsMatchAfterNormalization(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		stage     string
		wantTag   string
		wantGroup string
	}{
		{name: "ICU- kid matches the authored ICU-Kid", stage: "ICU- kid", wantTag: "ICU-Kid", wantGroup: KidRationGroupLabel},
		{name: "ICU-Kid matches itself", stage: "ICU-Kid", wantTag: "ICU-Kid", wantGroup: KidRationGroupLabel},
		{name: "F2- Male matches the authored F2-Male", stage: "F2- Male", wantTag: "F2-Male", wantGroup: KidRationGroupLabel},
		{name: "F2-Male matches itself", stage: "F2-Male", wantTag: "F2-Male", wantGroup: KidRationGroupLabel},
		{name: "case and padding are folded", stage: "  non-PREGNANT ", wantTag: "Non-Pregnant", wantGroup: "Anantapur Sheep"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := testConfig()
			for _, tag := range []string{"ICU-Kid", "F2-Male"} {
				cfg = withRate(cfg, KidRationGroupLabel, tag, "Concentrate", "50.000")
				cfg = withRate(cfg, KidRationGroupLabel, tag, "Hybrid", "100.000")
			}
			cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Concentrate", "200.000")
			cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Hybrid", "800.000")

			rows := generate(cfg, shed(ShedGrain{ManagementStage: tc.stage, Breed: "Anantapur Sheep", HeadCount: 8}), 1)
			if len(rows) != 1 {
				t.Fatalf("rows = %d, want 1", len(rows))
			}
			if rows[0].ShedTag != tc.wantTag {
				t.Fatalf("shed_tag = %q, want %q", rows[0].ShedTag, tc.wantTag)
			}
			if rows[0].RationGroup != tc.wantGroup {
				t.Fatalf("ration_group = %q, want %q", rows[0].RationGroup, tc.wantGroup)
			}
			if rows[0].Blocked {
				t.Fatalf("row blocked; a cosmetic spelling difference must not stall a shed")
			}
		})
	}
}

// Two raw stage spellings of the same tag inside one shed must COLLAPSE into a single instruction,
// not produce two half-sized rows an operator has to re-add.
func TestRawStageVariantsCollapseIntoOneRow(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg = withRate(cfg, KidRationGroupLabel, "ICU-Kid", "Concentrate", "100.000")
	cfg = withRate(cfg, KidRationGroupLabel, "ICU-Kid", "Hybrid", "100.000")

	rows := generate(cfg, shed(
		ShedGrain{ManagementStage: "ICU- kid", Breed: "Beetal", HeadCount: 42},
		ShedGrain{ManagementStage: "ICU-Kid", Breed: "Beetal", HeadCount: 27},
	), 1)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1 collapsed row", len(rows))
	}
	if rows[0].HeadCount != 69 {
		t.Fatalf("head_count = %d, want 69 (42 + 27 summed across spelling variants)", rows[0].HeadCount)
	}
}

// Sex is part of the counts grain but NOT of the ration key, so it must be summed away.
func TestSexIsSummedAwayBecauseItIsNotPartOfTheRationKey(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Concentrate", "200.000")
	cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Hybrid", "500.000")

	// The counts projection delivers these as two grains differing only by sex.
	rows := generate(cfg, shed(
		ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Anantapur Sheep", HeadCount: 12},
		ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Anantapur Sheep", HeadCount: 8},
	), 1)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].HeadCount != 20 {
		t.Fatalf("head_count = %d, want 20", rows[0].HeadCount)
	}
}

// ---------------------------------------------------------------------------
// Blocked vs authored zero -- the safety-critical distinction
// ---------------------------------------------------------------------------

// A missing rate must produce a BLOCKED cell carrying a reason, and it must carry NO NUMBER. A
// silent 0 is a shed that goes unfed while the sheet looks complete.
func TestMissingRateBlocksAndCarriesNoQuantity(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	// Concentrate is authored; Hybrid is deliberately NOT.
	cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Concentrate", "200.000")

	rows := generate(cfg, shed(ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Anantapur Sheep", HeadCount: 10}), 1)
	row := rows[0]

	blocked := itemOf(t, row, "Hybrid")
	if blocked.Status != QuantityBlocked {
		t.Fatalf("status = %q, want %q", blocked.Status, QuantityBlocked)
	}
	// THE CORE ASSERTION: no number at all. Not "0", not "0.000" -- nil.
	if blocked.QuantityKg != nil {
		t.Fatalf("blocked item carried quantity %q; an unauthored ration must have NO number", *blocked.QuantityKg)
	}
	if blocked.BlockedReason == nil {
		t.Fatal("blocked item has no reason; a gap an operator cannot locate is a gap they cannot close")
	}
	if blocked.BlockedReason.Code != BlockReasonNoRationRate {
		t.Fatalf("reason code = %q, want %q", blocked.BlockedReason.Code, BlockReasonNoRationRate)
	}
	if blocked.BlockedReason.Detail == "" {
		t.Fatal("blocked reason has no human-readable detail")
	}
	if !row.Blocked {
		t.Fatal("row.Blocked is false; the row total is partial and must say so")
	}
	// The resolved sibling is unaffected: one gap does not poison the rest of the row.
	resolved := itemOf(t, row, "Concentrate")
	if resolved.Status != QuantityResolved || resolved.QuantityKg == nil {
		t.Fatalf("sibling item did not resolve: %+v", resolved)
	}
	// The total sums only the resolved item: 10 x 200 x 0.5 = 1000 g.
	if row.SessionTotalKg != "1.000" {
		t.Fatalf("session_total_kg = %q, want \"1.000\" (resolved items only)", row.SessionTotalKg)
	}
}

// An AUTHORED zero is the opposite state: a real "feed nothing" instruction for milk-fed kids. It
// must be a fully numeric, resolved row.
func TestAuthoredZeroIsANormalResolvedRowWithZeroQuantity(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	// K0 kids are on milk: 0 g of every solid item is the correct authored value.
	cfg = withRate(cfg, KidRationGroupLabel, "K0", "Concentrate", "0.000")
	cfg = withRate(cfg, KidRationGroupLabel, "K0", "Hybrid", "0.000")

	rows := generate(cfg, shed(ShedGrain{ManagementStage: "K0", Breed: "Malai", HeadCount: 15}), 1)
	row := rows[0]

	if row.Blocked {
		t.Fatal("row blocked; an authored zero is a valid instruction, not a configuration gap")
	}
	for _, label := range []string{"Concentrate", "Hybrid"} {
		item := itemOf(t, row, label)
		if item.Status != QuantityResolved {
			t.Fatalf("%s status = %q, want %q", label, item.Status, QuantityResolved)
		}
		if item.QuantityKg == nil {
			t.Fatalf("%s has no quantity; an authored zero IS a number", label)
		}
		if *item.QuantityKg != "0.000" {
			t.Fatalf("%s quantity = %q, want \"0.000\"", label, *item.QuantityKg)
		}
		if item.BlockedReason != nil {
			t.Fatalf("%s carries a blocked reason despite resolving", label)
		}
	}
	// Note the kid branch worked without the breed map: 'Malai' has no entry in testConfig's
	// RationGroupByBreedKey, and a kid must not need one.
	if row.RationGroup != KidRationGroupLabel {
		t.Fatalf("ration_group = %q, want %q", row.RationGroup, KidRationGroupLabel)
	}
}

// An authored zero and a missing rate must be DISTINGUISHABLE in the output type. This is the test
// that would fail if the two were ever collapsed.
func TestAuthoredZeroAndMissingRateAreDistinguishableInTheOutputType(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg = withRate(cfg, KidRationGroupLabel, "K0", "Concentrate", "0.000")
	// Hybrid is left unauthored.

	rows := generate(cfg, shed(ShedGrain{ManagementStage: "K0", Breed: "Malai", HeadCount: 15}), 1)
	row := rows[0]

	authoredZero := itemOf(t, row, "Concentrate")
	missing := itemOf(t, row, "Hybrid")

	if authoredZero.Status == missing.Status {
		t.Fatalf("authored zero and missing rate share status %q; they are opposite instructions", authoredZero.Status)
	}
	if (authoredZero.QuantityKg == nil) == (missing.QuantityKg == nil) {
		t.Fatal("authored zero and missing rate agree on quantity presence; the pointer IS the safety contract")
	}
	if *authoredZero.QuantityKg != "0.000" {
		t.Fatalf("authored zero quantity = %q, want \"0.000\"", *authoredZero.QuantityKg)
	}
	if missing.QuantityKg != nil {
		t.Fatal("missing rate produced a number")
	}
}

func TestUnknownShedTagBlocksTheWholeRowRatherThanGuessingACourse(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Concentrate", "200.000")

	rows := generate(cfg, shed(ShedGrain{ManagementStage: "Totally Unknown Stage", Breed: "Anantapur Sheep", HeadCount: 5}), 1)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if !row.Blocked {
		t.Fatal("row not blocked; an unresolvable tag must never fall back to either course")
	}
	// The column shape a blocked row keeps is the DECLARED one, matching what a resolved row would
	// have printed. It is not the whole catalog: "Dry Maize" is catalogued but is not a slot of this
	// park, so listing it as a blocked cell would invent a gap nobody has to close.
	if len(row.Items) != len(cfg.PlannedFeedItems()) {
		t.Fatalf("blocked row has %d items, want %d -- a blocked row keeps the declared column shape",
			len(row.Items), len(cfg.PlannedFeedItems()))
	}
	for _, item := range row.Items {
		if item.Status != QuantityBlocked || item.QuantityKg != nil {
			t.Fatalf("item %q not fully blocked: %+v", item.FeedItem, item)
		}
		if item.BlockedReason.Code != BlockReasonUnknownShedTag {
			t.Fatalf("reason code = %q, want %q", item.BlockedReason.Code, BlockReasonUnknownShedTag)
		}
	}
}

func TestUnknownAdultBreedBlocksButKidsNeverConsultTheBreedMap(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg = withRate(cfg, KidRationGroupLabel, "F2-Male", "Concentrate", "80.000")
	cfg = withRate(cfg, KidRationGroupLabel, "F2-Male", "Hybrid", "150.000")

	// An ADULT tag with an unmapped breed blocks: the group cannot be resolved.
	adult := generate(cfg, shed(ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Unmapped Breed", HeadCount: 5}), 1)
	if !adult[0].Blocked {
		t.Fatal("adult row with an unmapped breed did not block")
	}
	if adult[0].Items[0].BlockedReason.Code != BlockReasonUnknownRationGroup {
		t.Fatalf("reason = %q, want %q", adult[0].Items[0].BlockedReason.Code, BlockReasonUnknownRationGroup)
	}

	// The SAME unmapped breed on a KID tag resolves, because kids bypass the breed map entirely.
	kid := generate(cfg, shed(ShedGrain{ManagementStage: "F2-Male", Breed: "Unmapped Breed", HeadCount: 5}), 1)
	if kid[0].Blocked {
		t.Fatal("kid row blocked on an unmapped breed; kids must never consult the breed map")
	}
	if kid[0].RationGroup != KidRationGroupLabel {
		t.Fatalf("kid ration_group = %q, want %q", kid[0].RationGroup, KidRationGroupLabel)
	}
}

// A park with no authored session split must BLOCK, not collapse to one implicit whole-day session
// that the packer has no batch for.
func TestNoSessionTemplateBlocksRatherThanInventingASession(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.Sessions = nil
	cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Concentrate", "200.000")
	cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Hybrid", "500.000")

	rows := generate(cfg, shed(ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Anantapur Sheep", HeadCount: 10}), 0)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1 blocked row (never an empty result, which reads as an empty shed)", len(rows))
	}
	if !rows[0].Blocked {
		t.Fatal("row not blocked")
	}
	for _, item := range rows[0].Items {
		if item.QuantityKg != nil {
			t.Fatalf("item %q carried a quantity with no session to deliver it in", item.FeedItem)
		}
		if item.BlockedReason.Code != BlockReasonNoSessionTemplate {
			t.Fatalf("reason = %q, want %q", item.BlockedReason.Code, BlockReasonNoSessionTemplate)
		}
	}
}

// ---------------------------------------------------------------------------
// Session split and rounding
// ---------------------------------------------------------------------------

// The invariant that makes the printed sheet trustworthy: the sessions add back EXACTLY to the
// day's shed total. That holds because rounding is applied per session and the daily total is
// defined as the sum of the rounded sessions -- see the policy note in rounding.go.
func TestSessionSplitSumsExactlyToTheShedDailyTotal(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		sessions []SessionTemplate
		head     int64
		grams    string
		// wantPerSession is the expected kg for each session, in order.
		wantPerSession []string
		wantDailyKg    string
	}{
		{
			name: "two even sessions",
			sessions: []SessionTemplate{
				{SessionNo: 1, Label: "Morning", SplitFraction: "0.5000"},
				{SessionNo: 2, Label: "Evening", SplitFraction: "0.5000"},
			},
			head: 10, grams: "200.000",
			wantPerSession: []string{"1.000", "1.000"},
			wantDailyKg:    "2.000",
		},
		{
			name: "three uneven sessions, each independently rounded up to a packable 100 g",
			sessions: []SessionTemplate{
				{SessionNo: 1, Label: "Morning", SplitFraction: "0.5000"},
				{SessionNo: 2, Label: "Noon", SplitFraction: "0.2500"},
				{SessionNo: 3, Label: "Evening", SplitFraction: "0.2500"},
			},
			// 7 head x 155 g = 1085 g/day. Splits: 542.5 -> 600, 271.25 -> 300, 271.25 -> 300.
			head: 7, grams: "155.000",
			wantPerSession: []string{"0.600", "0.300", "0.300"},
			// The day's total is the SUM OF THE ROUNDED SESSIONS (1200 g), not the rounded raw daily
			// figure (1085 -> 1100). Those differ, and the sessions must add to what is printed.
			wantDailyKg: "1.200",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := testConfig()
			cfg.Sessions = tc.sessions
			cfg.FeedItems = []FeedItem{{Label: "Concentrate", Key: "concentrate"}}
			cfg = withSlots(cfg, cfg.FeedItems...)
			cfg = withSlots(cfg, cfg.FeedItems...)
			cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Concentrate", tc.grams)

			rows := generate(cfg, shed(ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Anantapur Sheep", HeadCount: tc.head}), 0)
			if len(rows) != len(tc.sessions) {
				t.Fatalf("rows = %d, want %d (one per session)", len(rows), len(tc.sessions))
			}

			var totalGrams int64
			for i, row := range rows {
				got := *itemOf(t, row, "Concentrate").QuantityKg
				if got != tc.wantPerSession[i] {
					t.Fatalf("session %d quantity = %q, want %q", row.SessionNo, got, tc.wantPerSession[i])
				}
				if row.SessionTotalKg != tc.wantPerSession[i] {
					t.Fatalf("session %d total = %q, want %q", row.SessionNo, row.SessionTotalKg, tc.wantPerSession[i])
				}
				grams, ok := kgStringToGrams(got)
				if !ok {
					t.Fatalf("session %d quantity %q did not parse back to grams", row.SessionNo, got)
				}
				totalGrams += grams
			}
			if got := GramsToKgString(totalGrams); got != tc.wantDailyKg {
				t.Fatalf("sessions summed to %q, want the shed daily total %q", got, tc.wantDailyKg)
			}
		})
	}
}

// Narrowing to one session must not rescale it. Asking for the morning batch returns the morning
// batch, not the whole day relabelled.
func TestSessionFilterNarrowsWithoutRescaling(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.FeedItems = []FeedItem{{Label: "Concentrate", Key: "concentrate"}}
	cfg = withSlots(cfg, cfg.FeedItems...)
	cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Concentrate", "200.000")
	in := shed(ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Anantapur Sheep", HeadCount: 10})

	all := generate(cfg, in, 0)
	if len(all) != 2 {
		t.Fatalf("unfiltered rows = %d, want 2", len(all))
	}
	only := generate(cfg, in, 2)
	if len(only) != 1 {
		t.Fatalf("filtered rows = %d, want 1", len(only))
	}
	if only[0].SessionNo != 2 {
		t.Fatalf("session_no = %d, want 2", only[0].SessionNo)
	}
	// Still the half-day quantity: 10 x 200 x 0.5 = 1000 g.
	if got := *itemOf(t, only[0], "Concentrate").QuantityKg; got != "1.000" {
		t.Fatalf("filtered session quantity = %q, want \"1.000\" -- narrowing must not rescale", got)
	}
}

func TestShedFactorScalesAndAMissingFactorReadsAsOne(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.FeedItems = []FeedItem{
		{Label: "Concentrate", Key: "concentrate"},
		{Label: "Hybrid", Key: "hybrid"},
	}
	cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Concentrate", "200.000")
	cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Hybrid", "200.000")
	// A factor for concentrate only; hybrid has none and must read as 1.0.
	cfg.ShedFactorsByKey[ShedFactorKey(testShedID, "Concentrate")] = "1.5000"

	rows := generate(cfg, shed(ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Anantapur Sheep", HeadCount: 10}), 1)
	row := rows[0]

	// 10 x 200 x 1.5 x 0.5 = 1500 g.
	if got := *itemOf(t, row, "Concentrate").QuantityKg; got != "1.500" {
		t.Fatalf("factored quantity = %q, want \"1.500\"", got)
	}
	// 10 x 200 x 1.0 x 0.5 = 1000 g.
	if got := *itemOf(t, row, "Hybrid").QuantityKg; got != "1.000" {
		t.Fatalf("unfactored quantity = %q, want \"1.000\" (a missing factor reads as 1.0)", got)
	}
	if got := *itemOf(t, row, "Hybrid").ShedFactor; got != "1.0000" {
		t.Fatalf("reported factor = %q, want \"1.0000\"", got)
	}
}

// TestCorruptShedFactorBlocksRatherThanSilentlyDefaultingToOne is the P2-FACTOR regression.
//
// A MISSING shed factor safely defaults to 1.0 (proven above). A PRESENT-but-unparseable factor
// row is a different case: something was authored that this code cannot read. Before the fix,
// normalItem's `if parsed, ok := ParseDecimal(raw); ok { ... }` silently kept the outer defaults
// (factor=1.0) on the !ok branch instead of erroring, so a corrupt row was indistinguishable from
// "nobody configured a factor" -- exactly the asymmetry the rate branch a few lines above already
// guards against for feed_ration_rates. This must block instead.
func TestCorruptShedFactorBlocksRatherThanSilentlyDefaultingToOne(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.FeedItems = []FeedItem{{Label: "Concentrate", Key: "concentrate"}}
	cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Concentrate", "200.000")
	// A row EXISTS for this cell, but its stored value is not a decimal.
	cfg.ShedFactorsByKey[ShedFactorKey(testShedID, "Concentrate")] = "not-a-number"

	rows := generate(cfg, shed(ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Anantapur Sheep", HeadCount: 10}), 1)
	row := rows[0]

	item := itemOf(t, row, "Concentrate")
	if item.BlockedReason == nil {
		t.Fatalf("corrupt shed factor was not blocked: got a resolved quantity %v (silently defaulted to 1.0)", item.QuantityKg)
	}
	if item.BlockedReason.Code != BlockReasonInvalidShedFactor {
		t.Fatalf("blocked code = %q, want %q", item.BlockedReason.Code, BlockReasonInvalidShedFactor)
	}
	if item.QuantityKg != nil {
		t.Fatalf("blocked item carries a quantity %v, want nil", item.QuantityKg)
	}
}

func TestZeroHeadCountProducesZeroNotBlocked(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Concentrate", "200.000")
	cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Hybrid", "200.000")

	rows := generate(cfg, shed(ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Anantapur Sheep", HeadCount: 0}), 1)
	row := rows[0]
	if row.Blocked {
		t.Fatal("an empty grain is not a configuration gap and must not block")
	}
	// Nothing to feed because there is nobody there -- a resolved zero, distinct from an unauthored
	// one.
	if got := *itemOf(t, row, "Concentrate").QuantityKg; got != "0.000" {
		t.Fatalf("quantity = %q, want \"0.000\"", got)
	}
}

// ---------------------------------------------------------------------------
// Experiment strategy
// ---------------------------------------------------------------------------

// The single most important property of the experiment workflow: absolute_kg is ALREADY a shed
// total. Multiplying it by head count would overfeed the shed by a factor of its population.
func TestExperimentStrategyUsesAbsoluteKgAndIgnoresHeadCount(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.ExperimentByLocation[ExperimentLocationKey(testShedID, "")] = []ExperimentCell{
		{FeedItemLabel: "Concentrate", FeedItemKey: "concentrate", Basis: ExperimentBasisAbsoluteKg, AbsoluteKg: "12.000", Category: "Trial A"},
	}
	// A ration rate for the same grain exists and must be IGNORED: this shed is not on the grid.
	cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Concentrate", "200.000")

	// Two very different head counts must produce the SAME quantity.
	for _, head := range []int64{7, 700} {
		rows := generate(cfg, shed(ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Anantapur Sheep", HeadCount: head}), 1)
		if len(rows) != 1 {
			t.Fatalf("head=%d rows = %d, want 1 row for the whole shed", head, len(rows))
		}
		row := rows[0]
		if row.Workflow != WorkflowExperiment {
			t.Fatalf("workflow = %q, want %q", row.Workflow, WorkflowExperiment)
		}
		if !row.HeadCountInformational {
			t.Fatal("head_count_informational is false; nothing downstream may scale by this head count")
		}
		if row.HeadCount != head {
			t.Fatalf("head_count = %d, want %d carried through for display", row.HeadCount, head)
		}
		// 12 kg/day x 0.5 = 6 kg. Independent of head count.
		if got := *itemOf(t, row, "Concentrate").QuantityKg; got != "6.000" {
			t.Fatalf("head=%d quantity = %q, want \"6.000\" -- absolute kg must not scale with head count", head, got)
		}
		// The grid's rate must not have leaked in.
		if row.RationGroup != "" {
			t.Fatalf("experiment row carried ration_group %q; it is not resolved from the grid", row.RationGroup)
		}
	}
}

// THE CURRENT AUTHORING BASIS: grams per animal, scaled by the pen's LIVE projected head count
// (maintainer decision 2026-09-01). This is the exact inverse of the legacy property above, which is
// why the two tests sit side by side: the same stored number means two different feedings, and the
// basis on the cell is the only thing that tells them apart.
func TestExperimentStrategyMultipliesGramsPerHeadByTheLiveHeadCount(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.ExperimentByLocation[ExperimentLocationKey(testShedID, "")] = []ExperimentCell{
		{FeedItemLabel: "Concentrate", FeedItemKey: "concentrate", Basis: ExperimentBasisGramsPerHead, GramsPerHead: "350.000", Category: "Trial A"},
	}
	// A ration rate for the same grain exists and must still be IGNORED: the pen is hand-authored,
	// it is simply hand-authored as a RATE now.
	cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Concentrate", "200.000")
	// A shed factor for this pen exists and must NOT apply: an experiment quantity is grams x head
	// count and nothing else (maintainer decision, same day).
	cfg.ShedFactorsByKey[ShedFactorKey(testShedID, "Concentrate")] = "2.0000"

	// 350 g x head x 0.5 (the session's share of the day), then the shared pipeline's packable
	// rounding -- LINEAR in head count, unlike the legacy basis, and unaffected by the factor above.
	for _, tc := range []struct {
		head int64
		want string
	}{
		{head: 7, want: "1.300"}, // 1.225 kg rounded up to a packable figure
		{head: 700, want: "122.500"},
		{head: 0, want: "0.000"},
	} {
		rows := generate(cfg, shed(ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Anantapur Sheep", HeadCount: tc.head}), 1)
		if len(rows) != 1 {
			t.Fatalf("head=%d rows = %d, want 1 row for the whole pen", tc.head, len(rows))
		}
		row := rows[0]
		if row.Workflow != WorkflowExperiment {
			t.Fatalf("workflow = %q, want %q", row.Workflow, WorkflowExperiment)
		}
		// The count DID drive the quantity, so the row must not claim otherwise -- anything
		// downstream reading this flag as "informational" would be reading a lie.
		if row.HeadCountInformational {
			t.Fatal("head_count_informational is true on a per-animal cell; the count is the multiplier here")
		}
		item := itemOf(t, row, "Concentrate")
		if item.BlockedReason != nil {
			t.Fatalf("head=%d blocked = %+v, want a resolved quantity", tc.head, item.BlockedReason)
		}
		if got := *item.QuantityKg; got != tc.want {
			t.Fatalf("head=%d quantity = %q, want %q -- grams per animal must scale with the live count", tc.head, got, tc.want)
		}
		if item.GramsPerHead == nil || *item.GramsPerHead != "350.000" {
			t.Fatalf("head=%d grams_per_head = %v, want the authored rate reported alongside the quantity", tc.head, item.GramsPerHead)
		}
		if item.ShedFactor != nil {
			t.Fatalf("head=%d shed_factor = %q; an experiment quantity applies no factor", tc.head, *item.ShedFactor)
		}
	}
}

// A PEN MID-RE-AUTHORING HOLDS BOTH BASES, and each cell must be read on its own terms.
//
// This is the state every existing experiment pen passes through: one item re-entered in grams while
// the rest still carry the pen totals they were authored with. Reading the pen on one basis -- either
// one -- is off by the pen's whole population for half its items.
func TestExperimentPenMixingBothBasesReadsEachCellOnItsOwnBasis(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.ExperimentByLocation[ExperimentLocationKey(testShedID, "")] = []ExperimentCell{
		{FeedItemLabel: "Concentrate", FeedItemKey: "concentrate", Basis: ExperimentBasisGramsPerHead, GramsPerHead: "350.000", Category: "Trial A"},
		{FeedItemLabel: "Dry Masoor Bhusa", FeedItemKey: "dry_masoor_bhusa", Basis: ExperimentBasisAbsoluteKg, AbsoluteKg: "12.000", Category: "Trial A"},
	}

	rows := generate(cfg, shed(ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Anantapur Sheep", HeadCount: 63}), 1)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	row := rows[0]
	// 350 g x 63 x 0.5 = 11.025 kg, rounded up to a packable 11.1.
	if got := *itemOf(t, row, "Concentrate").QuantityKg; got != "11.100" {
		t.Fatalf("per-animal cell = %q, want \"11.100\"", got)
	}
	// 12 kg x 0.5 = 6 kg, head count untouched.
	if got := *itemOf(t, row, "Dry Masoor Bhusa").QuantityKg; got != "6.000" {
		t.Fatalf("legacy pen-total cell = %q, want \"6.000\" -- it must not be scaled by head count", got)
	}
	// ONE cell using the count is enough: the row can no longer say the count is informational.
	if row.HeadCountInformational {
		t.Fatal("head_count_informational is true on a pen whose concentrate is authored per animal")
	}
}

// AN UNREADABLE BASIS BLOCKS. The two readings differ by the pen's entire population, so there is no
// safe default to fall back to -- a guess is a feeding error either way.
func TestExperimentCellWithUnknownBasisBlocksRatherThanGuessing(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.ExperimentByLocation[ExperimentLocationKey(testShedID, "")] = []ExperimentCell{
		{FeedItemLabel: "Concentrate", FeedItemKey: "concentrate", Basis: "kilograms_maybe", AbsoluteKg: "12.000", GramsPerHead: "350.000", Category: "Trial A"},
	}

	rows := generate(cfg, shed(ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Anantapur Sheep", HeadCount: 63}), 1)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	item := itemOf(t, rows[0], "Concentrate")
	if item.BlockedReason == nil {
		t.Fatal("an unrecognized quantity basis resolved to a number; it must block")
	}
	if item.QuantityKg != nil {
		t.Fatalf("blocked cell still carried quantity %q", *item.QuantityKg)
	}
}

// An experiment shed is still a shed FULL OF ANIMALS, and the operator has to be told which ones.
//
// The shipped defect: an experiment row reported breed "" and put the trial ARM in the SHED TAG
// column. Live Castro 1 returned `shed_tag:"Sheep M NEW", breed:""` for a shed holding 63 Anantapur
// Sheep tagged F2-Male — the two facts that say what is standing in the shed were the two facts
// missing, and the one column that was populated meant something different than it does on every
// other row of the same table.
func TestExperimentRowCarriesTheLiveBreedAndShedTagOfTheAnimalsInTheShed(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.ExperimentByLocation[ExperimentLocationKey(testShedID, "")] = []ExperimentCell{
		{FeedItemLabel: "Concentrate", FeedItemKey: "concentrate", Basis: ExperimentBasisAbsoluteKg, AbsoluteKg: "39.000", Category: "Sheep M NEW"},
	}

	rows := generate(cfg, shed(ShedGrain{
		ManagementStage: "F2-Male",
		Breed:           "Anantapur Sheep",
		HeadCount:       63,
	}), 1)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	row := rows[0]

	if row.Breed != "Anantapur Sheep" {
		t.Errorf("breed = %q, want %q — an operator cannot see what is in the shed from a blank breed", row.Breed, "Anantapur Sheep")
	}
	if row.ShedTag != "F2-Male" {
		t.Errorf("shed_tag = %q, want the animals' authored tag %q", row.ShedTag, "F2-Male")
	}
	if row.ExperimentArm != "Sheep M NEW" {
		t.Errorf("experiment_arm = %q, want %q", row.ExperimentArm, "Sheep M NEW")
	}
	// The regression itself: the arm must NEVER be in the tag column. Two meanings in one column is
	// how a column stops being trustworthy on rows that were always correct.
	if row.ShedTag == row.ExperimentArm {
		t.Errorf("shed_tag == experiment_arm (%q) — the arm is overloading the tag column again", row.ShedTag)
	}
	// The ration group stays empty: an absolute kg never consults the breed -> group map.
	if row.RationGroup != "" {
		t.Errorf("ration_group = %q, want empty — no group is consulted on the experiment path", row.RationGroup)
	}
}

// The raw live stage is reported through the AUTHORED tag vocabulary, exactly as the normal path
// does it, so a cosmetic source variant does not print two spellings of one tag.
//
// It does NOT block when the stage is unknown, and that asymmetry with the normal path is the
// point: the normal path blocks because it cannot pick a ration course without the tag, while an
// experiment quantity is hand-entered and needs no course at all. Blanking the column would be
// strictly less information than the raw text.
func TestExperimentShedTagNormalizesButNeverBlocksOnAnUnknownStage(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		stage   string
		wantTag string
	}{
		{"cosmetic variant resolves to the authored label", "icu- kid", "ICU-Kid"},
		{"unknown stage falls back to the raw live text", "Nursery Pen 4", "Nursery Pen 4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := testConfig()
			cfg.ExperimentByLocation[ExperimentLocationKey(testShedID, "")] = []ExperimentCell{
				{FeedItemLabel: "Concentrate", FeedItemKey: "concentrate", Basis: ExperimentBasisAbsoluteKg, AbsoluteKg: "10.000", Category: "Trial A"},
			}
			rows := generate(cfg, shed(ShedGrain{ManagementStage: tc.stage, Breed: "Beetal", HeadCount: 4}), 1)
			if len(rows) != 1 {
				t.Fatalf("rows = %d, want 1", len(rows))
			}
			if rows[0].ShedTag != tc.wantTag {
				t.Errorf("shed_tag = %q, want %q", rows[0].ShedTag, tc.wantTag)
			}
			if rows[0].Blocked {
				t.Error("row blocked — an experiment quantity is hand-authored and does not depend on the tag resolving")
			}
			if got := *itemOf(t, rows[0], "Concentrate").QuantityKg; got != "5.000" {
				t.Errorf("quantity = %q, want \"5.000\" — the tag must not affect an absolute kg", got)
			}
		})
	}
}

// A MULTI-BREED experiment shed names every breed it holds. Yashoda 3 (Beetal + Sojat) and
// Yashoda 4 (Beetal + Osmanabadi) are both live today, so collapsing to one breed would not merely
// lose detail — it would print a specific wrong answer, and print the SAME wrong answer for two
// sheds that hold different animals.
//
// The same rule covers multiple management stages. No live experiment shed has two today, but the
// code must not assume that: the assumption is not enforced anywhere upstream, so a second stage
// arriving tomorrow would silently drop one.
func TestExperimentRowNamesEveryBreedAndStageInAMixedShed(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.ExperimentByLocation[ExperimentLocationKey(testShedID, "")] = []ExperimentCell{
		{FeedItemLabel: "Concentrate", FeedItemKey: "concentrate", Basis: ExperimentBasisAbsoluteKg, AbsoluteKg: "20.000", Category: "Sheep M NEW"},
	}

	rows := generate(cfg, shed(
		// Sojat leads on head count and must therefore read first; Beetal is the minority breed.
		ShedGrain{ManagementStage: "F2-Male", Breed: "Beetal", HeadCount: 8},
		ShedGrain{ManagementStage: "K0", Breed: "Sojat", HeadCount: 25},
	), 1)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1 row for the whole experiment shed", len(rows))
	}
	row := rows[0]

	if row.Breed != "Sojat + Beetal" {
		t.Errorf("breed = %q, want %q — both breeds named, dominant first", row.Breed, "Sojat + Beetal")
	}
	if row.ShedTag != "K0 + F2-Male" {
		t.Errorf("shed_tag = %q, want %q — multiple stages are listed, not collapsed to one", row.ShedTag, "K0 + F2-Male")
	}
	// Head count still sums the whole shed, and the arm is unaffected by the mix.
	if row.HeadCount != 33 {
		t.Errorf("head_count = %d, want 33", row.HeadCount)
	}
	if row.ExperimentArm != "Sheep M NEW" {
		t.Errorf("experiment_arm = %q, want %q", row.ExperimentArm, "Sheep M NEW")
	}
}

// REGRESSION GUARD: the descriptive columns are descriptive ONLY. Adding breed/tag reporting to the
// experiment path must not have moved a single gram — the whole point of the experiment workflow is
// that its quantity is a hand-authored shed absolute, unrelated to which animals are in the shed.
//
// Pinned to the live figure the maintainer verified: Castro 1, 39.000 kg per session.
func TestExperimentQuantityIsUnaffectedByBreedAndTagReporting(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.ExperimentByLocation[ExperimentLocationKey(testShedID, "")] = []ExperimentCell{
		{FeedItemLabel: "Concentrate", FeedItemKey: "concentrate", Basis: ExperimentBasisAbsoluteKg, AbsoluteKg: "78.000", Category: "Sheep M NEW"},
	}

	// Three sheds that differ in every descriptive way and in head count, and must not differ by one
	// gram: single breed, mixed breed, and no grains at all.
	for _, tc := range []struct {
		name   string
		grains []ShedGrain
	}{
		{"single breed", []ShedGrain{{ManagementStage: "F2-Male", Breed: "Anantapur Sheep", HeadCount: 63}}},
		{"mixed breed", []ShedGrain{
			{ManagementStage: "F2-Male", Breed: "Beetal", HeadCount: 8},
			{ManagementStage: "K0", Breed: "Sojat", HeadCount: 400},
		}},
		{"no projected animals", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rows := generate(cfg, shed(tc.grains...), 1)
			if len(rows) != 1 {
				t.Fatalf("rows = %d, want 1", len(rows))
			}
			// 78 kg/day x 0.5 = 39 kg for the session.
			if got := *itemOf(t, rows[0], "Concentrate").QuantityKg; got != "39.000" {
				t.Fatalf("session quantity = %q, want \"39.000\"", got)
			}
			if got := rows[0].SessionTotalKg; got != "39.000" {
				t.Fatalf("session_total_kg = %q, want \"39.000\"", got)
			}
		})
	}
}

// The packing worklist carries the arm too, so a packer knows which trial a bag belongs to without
// cross-referencing the direction sheet. It must be the ARM, never the shed tag.
func TestPackingLineCarriesTheExperimentArm(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.ExperimentByLocation[ExperimentLocationKey(testShedID, "")] = []ExperimentCell{
		{FeedItemLabel: "Concentrate", FeedItemKey: "concentrate", Basis: ExperimentBasisAbsoluteKg, AbsoluteKg: "78.000", Category: "Sheep M NEW"},
	}

	rows := generate(cfg, shed(ShedGrain{ManagementStage: "F2-Male", Breed: "Anantapur Sheep", HeadCount: 63}), 1)
	lines := BuildPackingRows(rows, cfg.FeedItems)
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
	if lines[0].ExperimentArm != "Sheep M NEW" {
		t.Errorf("packing experiment_arm = %q, want %q", lines[0].ExperimentArm, "Sheep M NEW")
	}
	if lines[0].TotalKg != "39.000" {
		t.Errorf("packing total_kg = %q, want \"39.000\" — the rollup must match the direction sheet exactly", lines[0].TotalKg)
	}
}

// A NORMAL row never carries an arm. The field is not a general-purpose label, and a normal shed
// that acquired one would put a trial name on an animal that is not in a trial.
func TestNormalRowsCarryNoExperimentArm(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Concentrate", "200.000")
	cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Hybrid", "500.000")

	rows := generate(cfg, shed(ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Anantapur Sheep", HeadCount: 10}), 1)
	if len(rows) == 0 {
		t.Fatal("no rows generated")
	}
	for _, row := range rows {
		if row.ExperimentArm != "" {
			t.Errorf("normal row carries experiment_arm %q, want empty", row.ExperimentArm)
		}
	}
	for _, line := range BuildPackingRows(rows, cfg.FeedItems) {
		if line.ExperimentArm != "" {
			t.Errorf("normal packing line carries experiment_arm %q, want empty", line.ExperimentArm)
		}
	}
}

// The experiment planner OWNS a shed that has authored rows, so the normal grid path must not run
// for it at all -- even when the grid would have resolved.
func TestExperimentPlannerTakesPrecedenceOverTheGrid(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Concentrate", "200.000")
	cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Hybrid", "500.000")

	in := shed(ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Anantapur Sheep", HeadCount: 10})

	normal := NewPlannerSet().PlannerFor(in, cfg)
	if normal.Workflow() != WorkflowNormal {
		t.Fatalf("without experiment rows the planner is %q, want %q", normal.Workflow(), WorkflowNormal)
	}

	cfg.ExperimentByLocation[ExperimentLocationKey(testShedID, "")] = []ExperimentCell{
		{FeedItemLabel: "Concentrate", FeedItemKey: "concentrate", Basis: ExperimentBasisAbsoluteKg, AbsoluteKg: "9.000", Category: "Trial B"},
	}
	experiment := NewPlannerSet().PlannerFor(in, cfg)
	if experiment.Workflow() != WorkflowExperiment {
		t.Fatalf("with experiment rows the planner is %q, want %q", experiment.Workflow(), WorkflowExperiment)
	}

	rows := generate(cfg, in, 1)
	// Only the authored item. A catalog item absent from the experiment sheet is NOT a gap: the
	// operator hand-entered exactly what this trial uses.
	if len(rows[0].Items) != 1 {
		t.Fatalf("experiment row has %d items, want only the 1 authored cell", len(rows[0].Items))
	}
	if rows[0].Blocked {
		t.Fatal("experiment row blocked; an unlisted catalog item is out of scope, not missing config")
	}
}

// The planner set must be TOTAL: no shed can fall through selection and silently produce no rows,
// which on a feed sheet is indistinguishable from an empty shed.
func TestPlannerSetIsTotal(t *testing.T) {
	t.Parallel()
	planner := NewPlannerSet().PlannerFor(ShedInput{ShedID: "unknown-shed"}, ConfigSnapshot{})
	if planner == nil {
		t.Fatal("planner set returned nil; selection must be total")
	}
	if planner.Workflow() != WorkflowNormal {
		t.Fatalf("fallback workflow = %q, want %q", planner.Workflow(), WorkflowNormal)
	}
}

// ---------------------------------------------------------------------------
// Summary and packing rollup
// ---------------------------------------------------------------------------

func TestSummaryExcludesBlockedCellsFromTotalsAndCountsThemInstead(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Concentrate", "200.000")
	// Hybrid unauthored.

	rows := generate(cfg, shed(ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Anantapur Sheep", HeadCount: 10}), 0)
	summary := SummarizeScope(rows, cfg.FeedItems)

	if summary.Scope != SummaryScopeFiltered {
		t.Fatalf("scope = %q, want %q -- the rollup must declare that it covers the whole filtered set, not the page",
			summary.Scope, SummaryScopeFiltered)
	}
	if summary.ShedCount != 1 {
		t.Fatalf("shed_count = %d, want 1", summary.ShedCount)
	}
	// Two sessions x one blocked item.
	if summary.BlockedCount != 2 {
		t.Fatalf("blocked_count = %d, want 2", summary.BlockedCount)
	}
	if summary.BlockedShedCount != 1 {
		t.Fatalf("blocked_shed_count = %d, want 1", summary.BlockedShedCount)
	}

	byItem := map[string]FeedItemTotal{}
	for _, total := range summary.TotalKgByFeedItem {
		byItem[total.FeedItem] = total
	}
	// 2 sessions x 1000 g = 2 kg.
	if got := byItem["Concentrate"].QuantityKg; got != "2.000" {
		t.Fatalf("concentrate total = %q, want \"2.000\"", got)
	}
	// The blocked item's column total is 0 -- but its BlockedCells says why, so the zero is never
	// read as a complete figure.
	hybrid := byItem["Hybrid"]
	if hybrid.BlockedCells != 2 {
		t.Fatalf("hybrid blocked_cells = %d, want 2", hybrid.BlockedCells)
	}
	// Column order follows the authored catalog so a client's columns are stable across pages.
	if summary.TotalKgByFeedItem[0].FeedItem != "Concentrate" {
		t.Fatalf("first column = %q, want the first catalog item", summary.TotalKgByFeedItem[0].FeedItem)
	}
}

func TestPackingRowsSumGrainsPerShedAndPropagateBlocked(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.FeedItems = []FeedItem{{Label: "Concentrate", Key: "concentrate"}}
	cfg = withSlots(cfg, cfg.FeedItems...)
	cfg = withRate(cfg, "Beetal/Sirohi", "Non-Pregnant", "Concentrate", "200.000")
	cfg = withRate(cfg, KidRationGroupLabel, "F2-Male", "Concentrate", "100.000")

	rows := generate(cfg, shed(
		ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Beetal", HeadCount: 10},
		ShedGrain{ManagementStage: "F2-Male", Breed: "Beetal", HeadCount: 6},
	), 1)
	packing := BuildPackingRows(rows, cfg.FeedItems)

	if len(packing) != 1 {
		t.Fatalf("packing rows = %d, want 1 (one bag per item per shed per session, not one per grain)", len(packing))
	}
	line := packing[0]
	if line.Status != PackingStatusReady {
		t.Fatalf("status = %q, want %q", line.Status, PackingStatusReady)
	}
	if line.HeadCount != 16 {
		t.Fatalf("head_count = %d, want 16 summed across the shed's grains", line.HeadCount)
	}
	// 10 x 200 x 0.5 = 1000 g, plus 6 x 100 x 0.5 = 300 g -> 1.3 kg. Summed from the ALREADY-ROUNDED
	// session quantities, so the worklist matches the direction sheet exactly.
	if line.TotalKg != "1.300" {
		t.Fatalf("total_kg = %q, want \"1.300\"", line.TotalKg)
	}

	// Now break one grain's ration: the shed's line must go blocked rather than being packed from
	// the resolved remainder.
	broken := testConfig()
	broken.FeedItems = cfg.FeedItems
	broken = withSlots(broken, broken.FeedItems...)
	broken = withRate(broken, "Beetal/Sirohi", "Non-Pregnant", "Concentrate", "200.000")
	brokenRows := generate(broken, shed(
		ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Beetal", HeadCount: 10},
		ShedGrain{ManagementStage: "F2-Male", Breed: "Beetal", HeadCount: 6},
	), 1)
	brokenPacking := BuildPackingRows(brokenRows, broken.FeedItems)
	if brokenPacking[0].Status != PackingStatusBlocked {
		t.Fatalf("status = %q, want %q -- a partially resolved line must not look packable",
			brokenPacking[0].Status, PackingStatusBlocked)
	}
	if len(brokenPacking[0].BlockedReasons) == 0 {
		t.Fatal("blocked packing line carries no reasons")
	}
	if brokenPacking[0].Items[0].QuantityKg != nil {
		t.Fatal("blocked packing item carried a quantity")
	}
}

// An empty shed is NOT a configuration gap. Conflating the two sends an operator hunting for a
// missing rate that does not exist.
func TestEmptyShedIsDistinctFromBlocked(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.FeedItems = []FeedItem{{Label: "Concentrate", Key: "concentrate"}}
	cfg = withSlots(cfg, cfg.FeedItems...)
	cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Concentrate", "200.000")

	rows := generate(cfg, shed(ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Anantapur Sheep", HeadCount: 0}), 1)
	packing := BuildPackingRows(rows, cfg.FeedItems)
	if packing[0].Status != PackingStatusEmpty {
		t.Fatalf("status = %q, want %q", packing[0].Status, PackingStatusEmpty)
	}
	if len(packing[0].BlockedReasons) != 0 {
		t.Fatal("an empty shed reported blocked reasons")
	}
}

// A shed with no grains at all produces no rows, which is correct -- but the packing surface must
// still be able to say the shed is empty, which it does from the shed page rather than from here.
func TestShedWithNoGrainsProducesNoRows(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	rows := generate(cfg, shed(), 0)
	if len(rows) != 0 {
		t.Fatalf("rows = %d, want 0", len(rows))
	}
}

func TestOverduePendingPropagatesFromTheCountsProjection(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.FeedItems = []FeedItem{{Label: "Concentrate", Key: "concentrate"}}
	cfg = withSlots(cfg, cfg.FeedItems...)
	cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Concentrate", "200.000")

	rows := generate(cfg, shed(
		ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Anantapur Sheep", HeadCount: 5},
		ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Anantapur Sheep", HeadCount: 5, OverduePending: true},
	), 1)
	if !rows[0].OverduePending {
		t.Fatal("overdue_pending did not propagate; the operator must see that a movement this plan assumes has not happened")
	}
}

// ---------------------------------------------------------------------------
// The session RECIPE: which items a row is allowed to name
// ---------------------------------------------------------------------------
//
// Migration 000005's first half. feed_item_catalog is the tenant's feed VOCABULARY; the park's
// session slots are its RECIPE. Before the slots existed, the generator had no recipe to consult
// and walked the catalog, asking every shed for the four roughages that are substitution
// alternatives to one another -- on one live CBE run, 619 kg of Hybrid nobody packs plus 112
// blocked cells of noise.
//
// The pair of tests below is the whole fix, stated as its two halves. They must BOTH hold: taken
// alone the first would be satisfied by never blocking, and the second by never emitting anything.

// HALF ONE -- NARROWING WHICH ITEMS ARE ASKED FOR DOES NOT SOFTEN WHAT HAPPENS WHEN ONE IS MISSING.
//
// This is the safety regression this fix could most plausibly have introduced, so it is asserted
// directly. A slot the park DECLARED still has to resolve. If its (park, group, tag, item) cell has
// no authored rate, the cell blocks and carries no number, exactly as it did before slots existed.
// Silently dropping a declared-but-unrated slot would be strictly worse than the old noise: the
// sheet would look complete while a feed the park actually serves went unpacked.
func TestDeclaredSlotWithNoRateStillBlocks(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	// Both slots are declared. Only one is rated.
	cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Concentrate", "200.000")

	rows := generate(cfg, shed(ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Anantapur Sheep", HeadCount: 10}), 1)
	row := findRow(t, rows, "Non-Pregnant", 1)

	if !row.Blocked {
		t.Fatal("row not blocked; a declared slot with no authored rate must still block the row")
	}

	// The rated slot resolves normally -- blocking is per cell, not per row.
	resolved := itemOf(t, row, "Concentrate")
	if resolved.Status != QuantityResolved || resolved.QuantityKg == nil {
		t.Fatalf("declared+rated slot did not resolve: %+v", resolved)
	}

	// The declared-but-unrated slot blocks, with the gap's exact coordinate and NO number.
	blocked := itemOf(t, row, "Hybrid")
	if blocked.Status != QuantityBlocked {
		t.Fatalf("declared slot with no rate has status %q, want %q", blocked.Status, QuantityBlocked)
	}
	if blocked.QuantityKg != nil {
		t.Fatalf("blocked slot carries a quantity %q; a blocked cell must have no number at all", *blocked.QuantityKg)
	}
	if blocked.BlockedReason == nil || blocked.BlockedReason.Code != BlockReasonNoRationRate {
		t.Fatalf("blocked slot reason = %+v, want code %q", blocked.BlockedReason, BlockReasonNoRationRate)
	}

	// And the gap is COUNTED as a gap rather than being absorbed into the total.
	summary := SummarizeScope(rows, cfg.PlannedFeedItems())
	if summary.BlockedCount != 1 {
		t.Fatalf("summary blocked_count = %d, want 1", summary.BlockedCount)
	}
}

// HALF TWO -- AN UNDECLARED CATALOG ITEM IS SIMPLY NOT ASKED FOR.
//
// "This park does not feed Dry Maize" and "this park feeds Dry Maize but nobody said how much" are
// different states and must not render identically. Before the slots existed they did, which is
// what buried the real gaps under 112 cells of noise.
//
// The distinction is asserted three ways, because absence is easy to get right in one place and
// wrong in another: the item must not appear as a row cell, must not be counted as a blocked gap,
// and must not appear on the packing worklist as a bag to fill.
func TestUndeclaredCatalogItemIsAbsentNotBlocked(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	// "Dry Maize" is in the catalog and is deliberately NOT a declared slot. It is given a perfectly
	// good rate to make the point sharper: the reason it does not appear is that the park never
	// declared it, not that its ration is missing.
	cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Concentrate", "200.000")
	cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Hybrid", "500.000")
	cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Dry Maize", "900.000")

	rows := generate(cfg, shed(ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Anantapur Sheep", HeadCount: 10}), 1)
	row := findRow(t, rows, "Non-Pregnant", 1)

	if row.Blocked {
		t.Fatal("row blocked; every DECLARED slot is rated, so there is no gap to report")
	}
	for _, item := range row.Items {
		if item.FeedItem == "Dry Maize" {
			t.Fatalf("undeclared catalog item appeared on the row as %+v; it must be absent, "+
				"neither quantified (feed nobody packs) nor blocked (a gap nobody has to close)", item)
		}
	}
	if len(row.Items) != 2 {
		t.Fatalf("row has %d items, want exactly the 2 declared slots", len(row.Items))
	}

	// Not a gap in the rollup...
	summary := SummarizeScope(rows, cfg.PlannedFeedItems())
	if summary.BlockedCount != 0 {
		t.Fatalf("summary blocked_count = %d, want 0 -- an undeclared item is not an authored gap", summary.BlockedCount)
	}
	for _, total := range summary.TotalKgByFeedItem {
		if total.FeedItem == "Dry Maize" {
			t.Fatalf("undeclared item reached the summary totals: %+v", total)
		}
	}

	// ...and not a bag on the packing worklist.
	for _, line := range BuildPackingRows(rows, cfg.PlannedFeedItems()) {
		for _, item := range line.Items {
			if item.FeedItem == "Dry Maize" {
				t.Fatalf("undeclared item reached the packing worklist: %+v", item)
			}
		}
	}
}

// Slots are emitted in slot_no order, which is PACKING order rather than catalog order. The sheet
// an operator reads and the sequence they fill bags in are the same list, so the order the park
// authored has to survive generation.
func TestDeclaredSlotsAreEmittedInSlotOrderNotCatalogOrder(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	// Declared in the REVERSE of the catalog order (catalog: Concentrate, Hybrid, Dry Maize).
	cfg = withSlots(cfg,
		FeedItem{Label: "Hybrid", Key: "hybrid"},
		FeedItem{Label: "Concentrate", Key: "concentrate"},
	)
	cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Concentrate", "200.000")
	cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Hybrid", "500.000")

	rows := generate(cfg, shed(ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Anantapur Sheep", HeadCount: 10}), 1)
	row := findRow(t, rows, "Non-Pregnant", 1)

	got := []string{}
	for _, item := range row.Items {
		got = append(got, item.FeedItem)
	}
	if len(got) != 2 || got[0] != "Hybrid" || got[1] != "Concentrate" {
		t.Fatalf("row items = %v, want the declared slot order [Hybrid Concentrate]", got)
	}
}

// Two sessions may consist of different feeds. The park's morning slots must not leak into the
// evening row -- the whole reason the slots hang off the session rather than off the park.
func TestSessionsNarrowToTheirOwnDeclaredSlots(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.Sessions = []SessionTemplate{
		{SessionNo: 1, Label: "Morning", SplitFraction: "0.5000",
			Items: []FeedItem{{Label: "Concentrate", Key: "concentrate"}}},
		{SessionNo: 2, Label: "Evening", SplitFraction: "0.5000",
			Items: []FeedItem{{Label: "Hybrid", Key: "hybrid"}}},
	}
	cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Concentrate", "200.000")
	cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Hybrid", "500.000")

	rows := generate(cfg, shed(ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Anantapur Sheep", HeadCount: 10}), 0)

	morning := findRow(t, rows, "Non-Pregnant", 1)
	if len(morning.Items) != 1 || morning.Items[0].FeedItem != "Concentrate" {
		t.Fatalf("morning items = %+v, want only Concentrate", morning.Items)
	}
	evening := findRow(t, rows, "Non-Pregnant", 2)
	if len(evening.Items) != 1 || evening.Items[0].FeedItem != "Hybrid" {
		t.Fatalf("evening items = %+v, want only Hybrid", evening.Items)
	}
	if morning.Blocked || evening.Blocked {
		t.Fatal("a session that declares its own slots and has rates for them must not block")
	}
}

// A session that declares NO slots is an incomplete template, not permission to feed everything.
// It blocks -- the same asymmetry as a missing rate: absence never widens what gets fed.
func TestSessionThatDeclaresNoItemsBlocksRatherThanFallingBackToTheCatalog(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg = withSlots(cfg) // every session declares nothing
	cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Concentrate", "200.000")

	rows := generate(cfg, shed(ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Anantapur Sheep", HeadCount: 10}), 1)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if !row.Blocked {
		t.Fatal("row not blocked; a session with no declared items has no authored packing list")
	}
	if row.SessionTotalKg != "0.000" {
		t.Fatalf("session_total_kg = %q, want \"0.000\" -- nothing resolved", row.SessionTotalKg)
	}
	for _, item := range row.Items {
		if item.Status != QuantityBlocked || item.QuantityKg != nil {
			t.Fatalf("item %q not fully blocked: %+v", item.FeedItem, item)
		}
		if item.BlockedReason.Code != BlockReasonNoSessionTemplate {
			t.Fatalf("reason code = %q, want %q", item.BlockedReason.Code, BlockReasonNoSessionTemplate)
		}
	}
}

// The experiment workflow does NOT consult the park's session slots. Its hand-entered cells are
// already the complete and authoritative list of what the shed is fed, so filtering them through
// the normal-workflow recipe would delete every experiment feed the ordinary sessions happen not to
// declare -- which in the live experiment template is most of them.
func TestExperimentShedIgnoresTheSessionSlotRecipe(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	// The park's sessions declare Concentrate and Hybrid; the experiment shed is fed neither.
	cfg.ExperimentByLocation[ExperimentLocationKey(testShedID, "")] = []ExperimentCell{
		{FeedItemLabel: "RGS Concentrate", FeedItemKey: "rgs_concentrate", Basis: ExperimentBasisAbsoluteKg, AbsoluteKg: "12.000", Category: "Trial A"},
	}

	rows := generate(cfg, shed(ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Anantapur Sheep", HeadCount: 10}), 1)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.Workflow != WorkflowExperiment {
		t.Fatalf("workflow = %q, want %q", row.Workflow, WorkflowExperiment)
	}
	if len(row.Items) != 1 || row.Items[0].FeedItem != "RGS Concentrate" {
		t.Fatalf("experiment items = %+v, want only the hand-authored RGS Concentrate", row.Items)
	}
	if row.Blocked {
		t.Fatal("experiment row blocked; its authored cells are complete and need no session slots")
	}
}

// TestBuildPackingRowsPanicsOnBlockedItemWithNilReason is the P3-BLOCK regression.
//
// Before the fix, a blocked ItemQuantity (Status == QuantityBlocked or QuantityKg == nil) with a
// nil BlockedReason silently substituted an empty BlockedReason{} -- a packing row that LOOKS
// blocked but carries no code/detail an operator or API consumer can act on, hiding whatever
// upstream bug failed to set a real reason. Every legitimate blocking call site in this package
// always sets a reason, so a nil one reaching BuildPackingRows is a programmer error and must fail
// loudly rather than ship a hollow BlockedReason.
func TestBuildPackingRowsPanicsOnBlockedItemWithNilReason(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	// No ration rate authored for Concentrate, so the grain resolves BLOCKED with a real reason.
	rows := generate(cfg, shed(ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Anantapur Sheep", HeadCount: 10}), 1)
	if len(rows) == 0 {
		t.Fatal("no rows generated to corrupt")
	}
	item := itemOf(t, rows[0], "Concentrate")
	if item.BlockedReason == nil {
		t.Fatal("fixture item is not actually blocked with a reason; test setup is wrong")
	}

	// Simulate the invariant violation: the same shape a bug elsewhere in the pipeline could
	// produce -- Status == blocked but no reason attached.
	for i := range rows[0].Items {
		if rows[0].Items[i].FeedItem == "Concentrate" {
			rows[0].Items[i].BlockedReason = nil
		}
	}

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("BuildPackingRows did not panic on a blocked item with a nil BlockedReason")
		}
	}()
	BuildPackingRows(rows, cfg.FeedItems)
	t.Fatal("unreachable: panic expected before this point")
}

// TestMixedShedRunsExperimentAndGridPartitionsSideBySide is the case that forced experiment config
// to become partition-aware (maintainer decision 2026-08-07).
//
// CBE's Godel 2 holds EIGHT partitions and only Parts 3, 4 and 5 are authored experiments; Parts 1
// and 2 are ordinary per-head-grid sheds. Until this change ExperimentByShedID was keyed by shed
// alone, so "is Godel 2 an experiment?" had no correct answer: yes overfed the 141 grid animals off
// an absolute shed total, no overfed the experiment partitions off the grid (measured on the real
// data as CBE concentrate 398.8 kg against the workbook's 182.0 kg).
//
// Both halves must come out of ONE shed in ONE pass: the experiment partitions on their authored
// absolute kg, the grid partitions on grams-per-head, and the shed's total equal to their sum.
func TestMixedShedRunsExperimentAndGridPartitionsSideBySide(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	// Parts 3 and 4 are authored experiments. Part 1 is not, and must stay on the grid.
	cfg.ExperimentByLocation[ExperimentLocationKey(testShedID, "Part 3")] = []ExperimentCell{
		{FeedItemLabel: "Concentrate", FeedItemKey: "concentrate", Basis: ExperimentBasisAbsoluteKg, AbsoluteKg: "12.000", Category: "Trial A"},
	}
	cfg.ExperimentByLocation[ExperimentLocationKey(testShedID, "Part 4")] = []ExperimentCell{
		{FeedItemLabel: "Concentrate", FeedItemKey: "concentrate", Basis: ExperimentBasisAbsoluteKg, AbsoluteKg: "8.000", Category: "Trial B"},
	}
	cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Concentrate", "1000.000")

	grains := []ShedGrain{
		{ManagementStage: "Non-Pregnant", Breed: "Anantapur Sheep", HeadCount: 46, PartitionLabel: "Part 3"},
		{ManagementStage: "Non-Pregnant", Breed: "Anantapur Sheep", HeadCount: 44, PartitionLabel: "Part 4"},
		{ManagementStage: "Non-Pregnant", Breed: "Anantapur Sheep", HeadCount: 56, PartitionLabel: "Part 1"},
	}

	var rows []DirectionRow
	for _, part := range SplitGrainsByPartition(grains) {
		in := ShedInput{ShedID: testShedID, ShedLabel: "Godel 2", PartitionLabel: part.PartitionLabel, Grains: part.Grains}
		rows = append(rows, generate(cfg, in, 1)...)
	}

	byPartition := map[string]DirectionRow{}
	for _, row := range rows {
		if row.ShedID != testShedID {
			t.Fatalf("row escaped its shed: shed_id=%q", row.ShedID)
		}
		byPartition[row.PartitionLabel] = row
	}
	for _, want := range []string{"Part 1", "Part 3", "Part 4"} {
		if _, ok := byPartition[want]; !ok {
			t.Fatalf("no row for %q; partitions present: %v", want, byPartition)
		}
	}

	// The two authored partitions are experiments, on ABSOLUTE kg, independent of head count.
	for _, tc := range []struct{ partition, wantKg, wantArm string }{
		{"Part 3", "6.000", "Trial A"}, // 12 kg x 0.5 session split
		{"Part 4", "4.000", "Trial B"}, // 8 kg x 0.5
	} {
		row := byPartition[tc.partition]
		if row.Workflow != WorkflowExperiment {
			t.Fatalf("%s workflow = %q, want %q", tc.partition, row.Workflow, WorkflowExperiment)
		}
		if got := *itemOf(t, row, "Concentrate").QuantityKg; got != tc.wantKg {
			t.Fatalf("%s quantity = %q, want %q (absolute kg, never scaled by head)", tc.partition, got, tc.wantKg)
		}
		if !row.HeadCountInformational {
			t.Fatalf("%s head count is not marked informational", tc.partition)
		}
	}

	// Part 1 was never authored, so it must stay on the grid: 56 head x 1000 g x 0.5 = 28.000 kg.
	// If the experiment leaked across the whole shed this row would carry an absolute total instead.
	grid := byPartition["Part 1"]
	if grid.Workflow != WorkflowNormal {
		t.Fatalf("Part 1 workflow = %q, want %q -- an unauthored partition must stay on the ration grid",
			grid.Workflow, WorkflowNormal)
	}
	if got := *itemOf(t, grid, "Concentrate").QuantityKg; got != "28.000" {
		t.Fatalf("Part 1 quantity = %q, want \"28.000\" (56 head x 1000 g x 0.5)", got)
	}
	if grid.HeadCountInformational {
		t.Fatal("Part 1 head count marked informational; a grid row's head count is what scales it")
	}
}

// A shed with NO partitions must behave exactly as it did before this change: one input, one row,
// and an experiment authored with no partition still applies to it.
func TestNonPartitionedShedIsUnchangedByThePartitionSplit(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.ExperimentByLocation[ExperimentLocationKey(testShedID, "")] = []ExperimentCell{
		{FeedItemLabel: "Concentrate", FeedItemKey: "concentrate", Basis: ExperimentBasisAbsoluteKg, AbsoluteKg: "10.000", Category: "Trial A"},
	}
	grains := []ShedGrain{{ManagementStage: "Non-Pregnant", Breed: "Anantapur Sheep", HeadCount: 30}}

	parts := SplitGrainsByPartition(grains)
	if len(parts) != 1 || parts[0].PartitionLabel != "" {
		t.Fatalf("split produced %d entries (%+v), want exactly one unpartitioned entry", len(parts), parts)
	}
	in := ShedInput{ShedID: testShedID, ShedLabel: "Shed 1", PartitionLabel: parts[0].PartitionLabel, Grains: parts[0].Grains}
	rows := generate(cfg, in, 1)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].PartitionLabel != "" {
		t.Fatalf("partition_label = %q, want empty for a shed with no partitions", rows[0].PartitionLabel)
	}
	if got := *itemOf(t, rows[0], "Concentrate").QuantityKg; got != "5.000" {
		t.Fatalf("quantity = %q, want \"5.000\"", got)
	}
}

// 'whole' is a MATCHING key, never copy, and a label variant must not split one partition in two.
func TestPartitionMatchKeyNormalizesLabelVariantsAndBlank(t *testing.T) {
	t.Parallel()
	for _, blank := range []string{"", "   "} {
		if got := PartitionMatchKey(blank); got != "whole" {
			t.Fatalf("PartitionMatchKey(%q) = %q, want \"whole\"", blank, got)
		}
	}
	if a, b := PartitionMatchKey("Part 3"), PartitionMatchKey("part  3"); a != b {
		t.Fatalf("label variants split a partition: %q vs %q", a, b)
	}
	// Two partitions of one shed must never collapse onto each other.
	if PartitionMatchKey("Part 3") == PartitionMatchKey("Part 4") {
		t.Fatal("Part 3 and Part 4 share a match key")
	}
}

// TestPackingLinesAreOnePerPartitionNotPerShed is the regression for what a packer actually holds.
//
// Reported from the phone (2026-08-08): Feed Packing showed "Castro - 1" and nothing for Castro 2,
// and only "Gandhi - 1" for a shed with three pens. The line key was (shedID, sessionNo), so every
// partition of a shed merged into ONE bag whose quantities were the SUM of all pens, stamped with
// whichever partition's row arrived first. The missing pens do not read as an error -- they read as
// sheds that need no feed -- and the surviving bag is over-weight.
//
// One bag per operational location, per session. This is the packing twin of the direction grain,
// and it matters more: direction is a document, a bag is a physical thing somebody carries.
func TestPackingLinesAreOnePerPartitionNotPerShed(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Concentrate", "1000.000")

	// Same shed, same session, same grain -- two different pens with different head counts.
	var rows []DirectionRow
	for _, part := range []struct {
		label string
		head  int64
	}{{"1", 10}, {"2", 4}} {
		in := ShedInput{
			ShedID:         testShedID,
			ShedLabel:      "Castro",
			PartitionLabel: part.label,
			Grains: []ShedGrain{{
				ManagementStage: "Non-Pregnant", Breed: "Anantapur Sheep",
				HeadCount: part.head, PartitionLabel: part.label,
			}},
		}
		rows = append(rows, generate(cfg, in, 1)...)
	}

	lines := BuildPackingRows(rows, cfg.FeedItems)

	byPartition := map[string]PackingRow{}
	for _, l := range lines {
		if _, clash := byPartition[l.PartitionLabel]; clash {
			t.Fatalf("two bags for partition %q in one session", l.PartitionLabel)
		}
		byPartition[l.PartitionLabel] = l
	}
	if len(lines) != 2 {
		t.Fatalf("packing lines = %d, want 2 (one bag per pen); got %v", len(lines), byPartition)
	}
	for _, want := range []string{"1", "2"} {
		if _, ok := byPartition[want]; !ok {
			t.Fatalf("no bag for Castro %s -- the pen vanished from the worklist; got %v", want, byPartition)
		}
	}

	// And the quantities must be the PEN's, not the shed's sum. 10 head x 1000 g x 0.5 = 5.000 kg;
	// 4 head x 1000 g x 0.5 = 2.000 kg. A merged bag would read 7.000 on one line and lose the other.
	for _, tc := range []struct{ partition, wantKg string }{{"1", "5.000"}, {"2", "2.000"}} {
		got := packingItemKg(t, byPartition[tc.partition], "Concentrate")
		if got != tc.wantKg {
			t.Errorf("Castro %s pack quantity = %q, want %q (this pen only, never the shed total)",
				tc.partition, got, tc.wantKg)
		}
	}
}

// ONE BAG PER PEN PER SESSION (maintainer decision 2026-08-11, REVERTING the 2026-08-10 pen-day
// bag).
//
// A pen's morning and evening shares are two separate bags: each is weighed out on its own and
// filmed on its own, so each is its own line. Three things are pinned, and each is a way the pen-day
// merge got it wrong:
//
//  1. TWO lines for a pen that has both sessions, not one.
//  2. Each line carries its OWN session's quantities and its own total -- not the day's, which would
//     show a packer twice what belongs in the bag in front of them.
//  3. Head count is the PEN's and repeats on both lines. It is the same animals morning and evening
//     and it is a DENOMINATOR, so summing it across a pen's lines reports double the population.
func TestPackingBagIsOnePerPenPerSession(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.FeedItems = []FeedItem{{Label: "Concentrate", Key: "concentrate"}}
	cfg = withSlots(cfg, cfg.FeedItems...)
	cfg = withRate(cfg, "Beetal/Sirohi", "Non-Pregnant", "Concentrate", "200.000")

	in := shed(ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Beetal", HeadCount: 10})
	// Both sessions of the same pen, exactly as the serve path loads them.
	rows := append(generate(cfg, in, 1), generate(cfg, in, 2)...)

	lines := BuildPackingRows(rows, cfg.FeedItems)
	if len(lines) != 2 {
		t.Fatalf("packing lines = %d, want 2 -- the pen must appear ONCE PER SESSION, not once for the day", len(lines))
	}

	bySession := map[int32]PackingRow{}
	for _, l := range lines {
		if _, clash := bySession[l.SessionNo]; clash {
			t.Fatalf("two bags for session %d", l.SessionNo)
		}
		bySession[l.SessionNo] = l
	}

	for _, tc := range []struct {
		sessionNo int32
		label     string
	}{{1, "Morning"}, {2, "Evening"}} {
		line, ok := bySession[tc.sessionNo]
		if !ok {
			t.Fatalf("no bag for session %d -- one of the pen's two bags vanished", tc.sessionNo)
		}
		// The authored label is what the packer reads; a positional "Session 1" says nothing about
		// which share of the day the bag is for.
		if line.SessionLabel != tc.label {
			t.Errorf("session %d label = %q, want %q", tc.sessionNo, line.SessionLabel, tc.label)
		}
		// 10 head x 200 g x 0.5 = 1000 g per session. The DAY figure (2.000) must never appear on a
		// line: it is twice what goes in this bag.
		if got := packingItemKg(t, line, "Concentrate"); got != "1.000" {
			t.Errorf("session %d Concentrate = %q, want \"1.000\"", tc.sessionNo, got)
		}
		if line.TotalKg != "1.000" {
			t.Errorf("session %d total = %q, want \"1.000\"", tc.sessionNo, line.TotalKg)
		}
		if line.HeadCount != 10 {
			t.Errorf("session %d head_count = %d, want 10 -- the pen's animals, the same on both lines",
				tc.sessionNo, line.HeadCount)
		}
	}
}

// Splitting by session must NOT be read as licence to merge pens, and merging pens must never be
// read as licence to merge sessions. Castro 1 and Castro 2 are different animals on different
// rations; 000137 exists because one Castro - 1 clip was closing out all three pens. Two pens x two
// sessions = FOUR bags, each with its own quantity.
func TestPackingLinesKeepPartitionsAndSessionsApart(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.FeedItems = []FeedItem{{Label: "Concentrate", Key: "concentrate"}}
	cfg = withSlots(cfg, cfg.FeedItems...)
	cfg = withRate(cfg, "Anantapur Sheep", "Non-Pregnant", "Concentrate", "1000.000")

	var rows []DirectionRow
	for _, pen := range []struct {
		label string
		head  int64
	}{{"1", 10}, {"2", 4}} {
		in := ShedInput{
			ShedID:         testShedID,
			ShedLabel:      "Castro",
			PartitionLabel: pen.label,
			Grains: []ShedGrain{{
				ManagementStage: "Non-Pregnant", Breed: "Anantapur Sheep",
				HeadCount: pen.head, PartitionLabel: pen.label,
			}},
		}
		rows = append(rows, generate(cfg, in, 1)...)
		rows = append(rows, generate(cfg, in, 2)...)
	}

	lines := BuildPackingRows(rows, cfg.FeedItems)
	if len(lines) != 4 {
		t.Fatalf("packing lines = %d, want 4 -- two pens x two sessions, with neither dimension collapsed", len(lines))
	}

	type penSession struct {
		pen       string
		sessionNo int32
	}
	byKey := map[penSession]PackingRow{}
	for _, l := range lines {
		key := penSession{l.PartitionLabel, l.SessionNo}
		if _, clash := byKey[key]; clash {
			t.Fatalf("two bags for pen %q session %d", key.pen, key.sessionNo)
		}
		byKey[key] = l
	}

	// 10 head x 1000 g x 0.5 = 5.000 kg per session; 4 head -> 2.000 kg per session. A bag that had
	// swallowed the other pen would read 7.000 and the second pen would have vanished entirely; one
	// that had swallowed the other session would read double.
	for _, tc := range []struct {
		pen        string
		perSession string
	}{{"1", "5.000"}, {"2", "2.000"}} {
		for _, sessionNo := range []int32{1, 2} {
			line, ok := byKey[penSession{tc.pen, sessionNo}]
			if !ok {
				t.Fatalf("no bag for Castro %s session %d -- it vanished from the worklist", tc.pen, sessionNo)
			}
			if got := packingItemKg(t, line, "Concentrate"); got != tc.perSession {
				t.Errorf("Castro %s session %d = %q, want %q (this pen and this session only)",
					tc.pen, sessionNo, got, tc.perSession)
			}
			if line.TotalKg != tc.perSession {
				t.Errorf("Castro %s session %d total = %q, want %q", tc.pen, sessionNo, line.TotalKg, tc.perSession)
			}
			// Bare numeric partitions join with a space, not a dash (oploc.go, 2026-08-14).
			if line.OperationalLocationDisplay != "Castro "+tc.pen {
				t.Errorf("Castro %s display = %q, want %q -- shed and pen must always render together",
					tc.pen, line.OperationalLocationDisplay, "Castro "+tc.pen)
			}
		}
	}
}

// A pen can be fine in the morning and short in the evening when the two sessions draw on different
// feed items. Each session is its OWN line, so the morning line stays packable and only the evening
// line reads blocked -- the packer is told which bag is short rather than being handed one flag over
// the whole day.
func TestOneBlockedSessionDoesNotBlockThePensOtherSession(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.FeedItems = []FeedItem{{Label: "Concentrate", Key: "concentrate"}, {Label: "Hybrid", Key: "hybrid"}}
	cfg = withRate(cfg, "Beetal/Sirohi", "Non-Pregnant", "Concentrate", "200.000")
	// Morning pours Concentrate (rate authored); evening pours Hybrid (rate MISSING -> blocked).
	cfg.Sessions = []SessionTemplate{
		{SessionNo: 1, Label: "Morning", SplitFraction: "0.5000", Items: []FeedItem{{Label: "Concentrate", Key: "concentrate"}}},
		{SessionNo: 2, Label: "Evening", SplitFraction: "0.5000", Items: []FeedItem{{Label: "Hybrid", Key: "hybrid"}}},
	}

	in := shed(ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Beetal", HeadCount: 10})
	rows := append(generate(cfg, in, 1), generate(cfg, in, 2)...)

	lines := BuildPackingRows(rows, cfg.FeedItems)
	if len(lines) != 2 {
		t.Fatalf("packing lines = %d, want 2", len(lines))
	}

	bySession := map[int32]PackingRow{}
	for _, l := range lines {
		bySession[l.SessionNo] = l
	}

	morning := bySession[1]
	if morning.Status != PackingStatusReady {
		t.Errorf("morning status = %q, want %q -- a resolved bag must stay packable when its sibling is short",
			morning.Status, PackingStatusReady)
	}
	if len(morning.BlockedReasons) != 0 {
		t.Errorf("morning carries %d blocked reasons, want 0", len(morning.BlockedReasons))
	}

	evening := bySession[2]
	if evening.Status != PackingStatusBlocked {
		t.Fatalf("evening status = %q, want %q", evening.Status, PackingStatusBlocked)
	}
	if len(evening.BlockedReasons) == 0 {
		t.Error("the blocked bag carries no reasons of its own")
	}
	// A blocked cell carries no quantity -- it is a gap, never a packable zero.
	if evening.Items[0].QuantityKg != nil {
		t.Error("blocked packing item carried a quantity")
	}
}

// packingItemKg reads one feed item's quantity off a packing line.
func packingItemKg(t *testing.T, line PackingRow, item string) string {
	t.Helper()
	for _, it := range line.Items {
		if it.FeedItem == item {
			if it.QuantityKg == nil {
				t.Fatalf("%s is blocked on the %q bag", item, line.PartitionLabel)
			}
			return *it.QuantityKg
		}
	}
	t.Fatalf("no %s on the %q bag", item, line.PartitionLabel)
	return ""
}
