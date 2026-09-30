package domain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

// TestSeededPCCareSOPIsThePreSOPBehaviour pins the day-one document to the slot table the
// module ran on before the SOP existed: same keys, same titles, same hints, same order, all
// compulsory videos, the 10 s hint on the trimming "while" clip; removal OPTIONAL on deworming
// alone with the two seeded clips; no questions anywhere. If this drifts, deploy changes what
// operators see with no one having published anything.
func TestSeededPCCareSOPIsThePreSOPBehaviour(t *testing.T) {
	rules := SeededRules()
	if rules.Version != 0 {
		t.Fatalf("seed version = %d, want 0", rules.Version)
	}
	for _, category := range SOPCategories {
		legacy := SlotsForCategory(category)
		got := rules.CategorySlots(category)
		if len(got) != len(legacy) {
			t.Fatalf("%s: %d slots, legacy has %d", category, len(got), len(legacy))
		}
		for i := range legacy {
			if got[i].FieldKey != legacy[i].FieldKey || got[i].Label != legacy[i].Label ||
				got[i].Description != legacy[i].Description || got[i].MinDurationHintSeconds != legacy[i].MinDurationHintSeconds {
				t.Fatalf("%s slot %d = %+v, legacy %+v", category, i, got[i], legacy[i])
			}
			if got[i].Kind != authored.KindVideo || !got[i].Required {
				t.Fatalf("%s slot %s must be a compulsory video on the seed", category, got[i].FieldKey)
			}
		}
		if len(rules.CategoryQuestions(category)) != 0 {
			t.Fatalf("%s: the seed asks no questions", category)
		}
	}
	fwr := rules.FeedWaterRemoval
	if fwr.Mode != RemovalModeOptional || len(fwr.AppliesTo) != 1 || fwr.AppliesTo[0] != CategoryDeworming || fwr.CutoffTime != "" {
		t.Fatalf("seed removal = %+v, want optional on deworming alone with the farm evening", fwr)
	}
	legacyRemoval := SlotsForCategory(CategoryFeedWaterRemoval)
	if len(fwr.Proofs) != len(legacyRemoval) {
		t.Fatalf("removal slots = %d, want %d", len(fwr.Proofs), len(legacyRemoval))
	}
	for i, p := range fwr.Proofs {
		if p.Key != legacyRemoval[i].FieldKey || p.Title != legacyRemoval[i].Label || p.Hint != legacyRemoval[i].Description || p.Kind != authored.KindVideo || !p.Required {
			t.Fatalf("removal slot %d = %+v, legacy %+v", i, p, legacyRemoval[i])
		}
	}
	if len(fwr.Questions) != 0 {
		t.Fatal("the seed's removal card asks no questions")
	}
	// The removal is still deworming's alone: anti protozoan's dose does not go in the feed.
	if rules.RemovalAppliesTo(CategoryAntiProtozoan) || !rules.RemovalAppliesTo(CategoryDeworming) {
		t.Fatal("seed: removal applies to deworming and not anti protozoan")
	}
}

// TestMigrationEmbedsTheSeededPCCareSOP pins the seed verbatim inside migration 000386, so the
// document a tenant gets on deploy IS the document the code compiles as version 0.
func TestMigrationEmbedsTheSeededPCCareSOP(t *testing.T) {
	migration, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "postgres", "000386_pc_care_sop.sql"))
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	// 000386 froze the DAY-ONE document; the live seed has since gained the fumigation card
	// (000457, pinned below). The day-one document is kept byte for byte beside it.
	v1, err := os.ReadFile(filepath.Join("sopseed", "pc_care_v1.json"))
	if err != nil {
		t.Fatalf("read v1 seed: %v", err)
	}
	seed := string(v1)
	marker := "$seed$" + seed + "$seed$"
	if n := strings.Count(string(migration), marker); n != 1 {
		t.Fatalf("migration embeds the seed %d times, want exactly 1 (the v1 insert)", n)
	}
	// The embedded JSON is the seed byte for byte AND parses to the compiled rules.
	var fromMigration map[string]any
	if err := json.Unmarshal([]byte(seed), &fromMigration); err != nil {
		t.Fatalf("seed json: %v", err)
	}
	if _, err := ParsePCCareSOP(map[string]any{"pc_care": fromMigration}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	// The day-one document predates fumigation, so it is no longer a complete card set on its
	// own; the LIVE seed (day one + the 000457 card) is what must validate.
	if problems := ValidatePCCareSOP(SeededRules().PCCareSOP); len(problems) > 0 {
		t.Fatalf("live seed invalid: %v", problems)
	}
}

// TestRemovalDecisionFollowsTheMode is the create's rulebook: required ignores a decline and
// applies to a listed category, optional is the planner's word, off refuses an ask.
func TestRemovalDecisionFollowsTheMode(t *testing.T) {
	yes, no := true, false
	base := SeededRules()
	with := func(mode string, applies ...string) Rules {
		r := base
		r.FeedWaterRemoval.Mode = mode
		r.FeedWaterRemoval.AppliesTo = applies
		return r
	}
	cases := []struct {
		name      string
		rules     Rules
		category  string
		requested *bool
		want      bool
		wantErr   error
	}{
		{"optional, not said", with(RemovalModeOptional, CategoryDeworming), CategoryDeworming, nil, false, nil},
		{"optional, asked", with(RemovalModeOptional, CategoryDeworming), CategoryDeworming, &yes, true, nil},
		{"optional, declined", with(RemovalModeOptional, CategoryDeworming), CategoryDeworming, &no, false, nil},
		{"optional, asked on a category it does not apply to", with(RemovalModeOptional, CategoryDeworming), CategoryAntiProtozoan, &yes, false, ErrFeedRemovalNotApplicable},
		{"optional, widened to anti protozoan", with(RemovalModeOptional, CategoryDeworming, CategoryAntiProtozoan), CategoryAntiProtozoan, &yes, true, nil},
		{"required, not said", with(RemovalModeRequired, CategoryDeworming), CategoryDeworming, nil, true, nil},
		{"required ignores a decline", with(RemovalModeRequired, CategoryDeworming), CategoryDeworming, &no, true, nil},
		{"required, category not listed", with(RemovalModeRequired, CategoryDeworming), CategoryHoofTrimming, nil, false, nil},
		{"required, asked on an unlisted category", with(RemovalModeRequired, CategoryDeworming), CategoryHoofTrimming, &yes, false, ErrFeedRemovalNotApplicable},
		{"off, not said", with(RemovalModeOff, CategoryDeworming), CategoryDeworming, nil, false, nil},
		{"off, declined", with(RemovalModeOff, CategoryDeworming), CategoryDeworming, &no, false, nil},
		{"off refuses an ask", with(RemovalModeOff, CategoryDeworming), CategoryDeworming, &yes, false, ErrRemovalNotOffered},
	}
	for _, tc := range cases {
		got, err := tc.rules.RemovalDecision(tc.category, tc.requested)
		if err != tc.wantErr {
			t.Fatalf("%s: err = %v, want %v", tc.name, err, tc.wantErr)
		}
		if got != tc.want {
			t.Fatalf("%s: applies = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestValidatePCCareSOPNamesEveryProblemByPath is the editor's refusal matrix.
func TestValidatePCCareSOPNamesEveryProblemByPath(t *testing.T) {
	broken := func(mutate func(*PCCareSOP)) []string {
		dsl := SeededRules().PCCareSOP
		// Deep-copy the category map so a mutation never leaks into the embedded seed.
		cats := map[string]*CategoryRules{}
		for k, v := range dsl.Categories {
			c := *v
			c.Proofs = append([]CaptureSlot(nil), v.Proofs...)
			c.Questions = append([]authored.Question(nil), v.Questions...)
			cats[k] = &c
		}
		dsl.Categories = cats
		dsl.FeedWaterRemoval.Proofs = append([]authored.ProofSlot(nil), dsl.FeedWaterRemoval.Proofs...)
		dsl.FeedWaterRemoval.AppliesTo = append([]string(nil), dsl.FeedWaterRemoval.AppliesTo...)
		mutate(&dsl)
		return ValidatePCCareSOP(dsl)
	}
	expect := func(name string, problems []string, want string) {
		t.Helper()
		for _, p := range problems {
			if strings.Contains(p, want) {
				return
			}
		}
		t.Fatalf("%s: problems %v do not name %q", name, problems, want)
	}
	if problems := ValidatePCCareSOP(SeededRules().PCCareSOP); len(problems) != 0 {
		t.Fatalf("seed must validate clean: %v", problems)
	}
	expect("schema", broken(func(d *PCCareSOP) { d.SchemaVersion = "x" }), "pc_care.schema_version")
	expect("mode", broken(func(d *PCCareSOP) { d.FeedWaterRemoval.Mode = "sometimes" }), "pc_care.feed_water_removal.mode")
	expect("applies_to unknown", broken(func(d *PCCareSOP) { d.FeedWaterRemoval.AppliesTo = []string{"grooming"} }), "pc_care.feed_water_removal.applies_to.0")
	expect("applies_to empty", broken(func(d *PCCareSOP) { d.FeedWaterRemoval.AppliesTo = nil }), "pc_care.feed_water_removal.applies_to")
	expect("cutoff", broken(func(d *PCCareSOP) { d.FeedWaterRemoval.CutoffTime = "24:00" }), "pc_care.feed_water_removal.cutoff_time")
	expect("removal all optional", broken(func(d *PCCareSOP) {
		for i := range d.FeedWaterRemoval.Proofs {
			d.FeedWaterRemoval.Proofs[i].Required = false
		}
	}), "pc_care.feed_water_removal.proofs")
	expect("category missing", broken(func(d *PCCareSOP) { delete(d.Categories, CategoryTicksRemoval) }), "pc_care.categories.ticks_removal")
	expect("category unknown", broken(func(d *PCCareSOP) { d.Categories["grooming"] = &CategoryRules{} }), "pc_care.categories.grooming")
	expect("category no compulsory capture", broken(func(d *PCCareSOP) { d.Categories[CategoryDeworming].Proofs[0].Required = false }), "pc_care.categories.deworming.proofs")
	expect("bad kind", broken(func(d *PCCareSOP) { d.Categories[CategoryDeworming].Proofs[0].Kind = "audio" }), "pc_care.categories.deworming.proofs.0.kind")
	expect("photo with a duration", broken(func(d *PCCareSOP) {
		d.Categories[CategoryHoofTrimming].Proofs[1].Kind = authored.KindPhoto
	}), "pc_care.categories.hoof_trimming.proofs.1.min_seconds")
	expect("question without choices", broken(func(d *PCCareSOP) {
		d.Categories[CategoryDeworming].Questions = []authored.Question{{ID: "dose_ok", Kind: authored.QuestionChoice, Title: "Dose given?"}}
	}), "pc_care.categories.deworming.questions.0.options")
	// OFF keeps the slots optional-free: an off card need not name a compulsory capture.
	if problems := broken(func(d *PCCareSOP) {
		d.FeedWaterRemoval.Mode = RemovalModeOff
		d.FeedWaterRemoval.Proofs = nil
		d.FeedWaterRemoval.AppliesTo = nil
	}); len(problems) != 0 {
		t.Fatalf("off with no card must validate: %v", problems)
	}
}

// TestUnknownPCCareSOPKeysAreNamedByPath: a misspelt key is refused at save, never dropped.
func TestUnknownPCCareSOPKeysAreNamedByPath(t *testing.T) {
	var raw map[string]any
	if err := json.Unmarshal(SeededPCCareSOPJSON(), &raw); err != nil {
		t.Fatal(err)
	}
	raw["feed_water_removal"].(map[string]any)["cutoff_tme"] = "21:30"
	raw["categories"].(map[string]any)["deworming"].(map[string]any)["proofs"].([]any)[0].(map[string]any)["kinds"] = "video"
	got := UnknownPCCareSOPKeys(map[string]any{"pc_care": raw})
	want := []string{"categories.deworming.proofs.0.kinds", "feed_water_removal.cutoff_tme"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unknown keys = %v, want %v", got, want)
	}
}

// TestCaptureSlotRoundTripsMinSeconds: the trimming hint survives a parse/emit cycle and a
// slot without `required` reads as compulsory (the shared authored rule).
func TestCaptureSlotRoundTripsMinSeconds(t *testing.T) {
	var slot CaptureSlot
	if err := json.Unmarshal([]byte(`{"key":"during_video","title":"While","kind":"video","min_seconds":10}`), &slot); err != nil {
		t.Fatal(err)
	}
	if !slot.Required || slot.MinSeconds != 10 {
		t.Fatalf("slot = %+v, want required with a 10 s hint", slot)
	}
	out, _ := json.Marshal(slot)
	if !strings.Contains(string(out), `"min_seconds":10`) || !strings.Contains(string(out), `"required":true`) {
		t.Fatalf("emitted %s", out)
	}
}

// TestServedRulesFillEveryList: a phone iterates the document without null checks.
func TestServedRulesFillEveryList(t *testing.T) {
	r := Rules{Version: 3, PCCareSOP: PCCareSOP{SchemaVersion: PCCareSOPSchemaVersion, FeedWaterRemoval: RemovalRules{Mode: RemovalModeOff}}}
	served := r.ServedRules()
	if served.Version != 3 || served.FeedWaterRemoval.Proofs == nil || served.FeedWaterRemoval.Questions == nil || served.FeedWaterRemoval.AppliesTo == nil {
		t.Fatalf("served = %+v", served.FeedWaterRemoval)
	}
	for _, c := range SOPCategories {
		if served.Categories[c] == nil || served.Categories[c].Proofs == nil || served.Categories[c].Questions == nil {
			t.Fatalf("%s not filled", c)
		}
	}
	// A category block the document lost reads as the seed so the task stays workable.
	if len(served.Categories[CategoryHoofTrimming].Proofs) != 3 {
		t.Fatalf("hoof trimming fell back to %d slots, want the seeded 3", len(served.Categories[CategoryHoofTrimming].Proofs))
	}
}

// TestMigrationEmbedsTheSeededFumigationCard pins migration 000457: the card it adds in place is
// the seeded fumigation card, and the live seed is exactly the day-one document plus that card.
func TestMigrationEmbedsTheSeededFumigationCard(t *testing.T) {
	migration, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "postgres", "000457_pc_care_fumigation.sql"))
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	parts := strings.Split(string(migration), "$fumigation$")
	if len(parts) != 3 {
		t.Fatalf("migration carries %d $fumigation$ markers, want exactly 2", len(parts)-1)
	}
	var embedded CategoryRules
	if err := json.Unmarshal([]byte(parts[1]), &embedded); err != nil {
		t.Fatalf("embedded card: %v", err)
	}
	seeded := SeededRules().Categories[CategoryFumigation]
	if seeded == nil {
		t.Fatal("the seed carries no fumigation card")
	}
	got, _ := json.Marshal(embedded)
	want, _ := json.Marshal(seeded)
	if string(got) != string(want) {
		t.Fatalf("000457 card drifted from the seed:\n got %s\nwant %s", got, want)
	}

	v1, err := os.ReadFile(filepath.Join("sopseed", "pc_care_v1.json"))
	if err != nil {
		t.Fatalf("read v1 seed: %v", err)
	}
	var day1, live map[string]any
	if err := json.Unmarshal(v1, &day1); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(SeededPCCareSOPJSON(), &live); err != nil {
		t.Fatal(err)
	}
	var card any
	_ = json.Unmarshal([]byte(parts[1]), &card)
	day1["categories"].(map[string]any)[CategoryFumigation] = card
	a, _ := json.Marshal(day1)
	b, _ := json.Marshal(live)
	if string(a) != string(b) {
		t.Fatal("the live seed is not the day-one document plus the 000457 fumigation card")
	}
}

// TestFumigationIsPenWorkWithTwoVideos: the pen's two captures, mixing then spraying, both
// compulsory videos; no animal capture mode; never under the feed & water removal.
func TestFumigationIsPenWorkWithTwoVideos(t *testing.T) {
	if !IsPenProofCategory(CategoryFumigation) || CaptureModeForCategory(CategoryFumigation) != CaptureModeTaskProof {
		t.Fatal("fumigation must be recorded per pen (task_proof)")
	}
	for _, c := range []string{CategoryDeworming, CategoryHoofTrimming, CategoryInventoryVaccine, CategoryFeedWaterRemoval} {
		if IsPenProofCategory(c) {
			t.Fatalf("%s is not a planner pen-proof category", c)
		}
	}
	slots := SeededRules().CategorySlots(CategoryFumigation)
	if len(slots) != 2 || slots[0].FieldKey != SlotMixingVideo || slots[1].FieldKey != SlotSprayingVideo {
		t.Fatalf("slots = %+v, want mixing_video then spraying_video", slots)
	}
	for _, s := range slots {
		if s.Kind != "video" || !s.Required {
			t.Fatalf("slot %s: kind %q required %v, want a compulsory video", s.FieldKey, s.Kind, s.Required)
		}
	}
	if !strings.Contains(SeededRules().Category(CategoryFumigation).Instruction, "5 ml") {
		t.Fatal("the seeded instruction must carry the 5 ml per litre dosage")
	}
	if SeededRules().RemovalAppliesTo(CategoryFumigation) {
		t.Fatal("the seed must not apply the feed & water removal to fumigation")
	}
	dsl := SeededRules().PCCareSOP
	dsl.FeedWaterRemoval.AppliesTo = []string{CategoryDeworming, CategoryFumigation}
	if problems := ValidatePCCareSOP(dsl); len(problems) == 0 {
		t.Fatal("a removal applied to fumigation must be refused")
	}
	if VerificationCategoryFor(CategoryFumigation) != "pc_fumigation" {
		t.Fatal("fumigation needs its own verifier category")
	}
	if !OwesPenVisit(CategoryFumigation, "shed-1") {
		t.Fatal("a fumigated pen owes the next-day visit (2026-09-30)")
	}
}
