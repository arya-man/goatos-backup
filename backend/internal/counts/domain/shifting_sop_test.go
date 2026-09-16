package domain

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

// The seeded document IS the pre-SOP behaviour: one shifting video on every completion, the two
// feed clips on a high-priority one, nothing at raise, no questions. If this test moves, deploying
// the migration changes what the operator is asked -- the one thing the seed must not do.
func TestSeededShiftingSOPIsThePreSOPBehaviour(t *testing.T) {
	t.Parallel()
	rules := SeededShiftingRules()
	if rules.Version != 0 {
		t.Fatalf("seed version = %d, want 0", rules.Version)
	}
	low := rules.CompletionSlots("low")
	if len(low) != 1 || low[0].Key != SlotShiftingVideo || low[0].Kind != authored.KindVideo || !low[0].Required {
		t.Fatalf("low completion slots = %+v", low)
	}
	high := rules.CompletionSlots("high")
	wantHigh := []string{SlotShiftingVideo, SlotShiftingPackingVideo, SlotShiftingFeedingVideo}
	if len(high) != 3 {
		t.Fatalf("high completion slots = %+v", high)
	}
	for i, k := range wantHigh {
		if high[i].Key != k || high[i].Kind != authored.KindVideo || !high[i].Required {
			t.Fatalf("high slot %d = %+v, want %s video required", i, high[i], k)
		}
	}
	if len(rules.CompletionQuestions("high")) != 0 || len(rules.CompletionQuestions("low")) != 0 {
		t.Fatal("the seed asks no completion questions")
	}
	raise := rules.RaiseCard()
	if len(raise.Proofs) != 0 || len(raise.Questions) != 0 || raise.Instruction != "" {
		t.Fatalf("the seed asks nothing at raise, got %+v", raise)
	}
}

// TestMigrationEmbedsTheSeededShiftingSOP pins migration 000324 to the embedded seed: the
// section it adds in place to each tenant's published `shifting` version is the same bytes the
// code compiles for a tenant with no authored version.
func TestMigrationEmbedsTheSeededShiftingSOP(t *testing.T) {
	path := filepath.Join("..", "..", "..", "migrations", "postgres", "000324_shifting_sop.sql")
	sql, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "$seed$" + strings.TrimSpace(string(SeededShiftingSOPJSON())) + "$seed$"
	if got := strings.Count(string(sql), want); got != 1 {
		t.Fatalf("migration embeds the seeded shifting document verbatim %d time(s), want 1", got)
	}
}

func parseShiftingSeed(t *testing.T) ShiftingSOP {
	t.Helper()
	dsl, err := ParseShiftingSOP(map[string]any{"shifting": json.RawMessage(SeededShiftingSOPJSON())})
	if err != nil {
		t.Fatal(err)
	}
	return dsl
}

func TestValidateShiftingSOPRefusesByPath(t *testing.T) {
	t.Parallel()
	if p := ValidateShiftingSOP(parseShiftingSeed(t)); len(p) != 0 {
		t.Fatalf("seed invalid: %v", p)
	}
	has := func(problems []string, prefix string) bool {
		for _, p := range problems {
			if strings.HasPrefix(p, prefix) {
				return true
			}
		}
		return false
	}
	// Wrong schema version.
	d := parseShiftingSeed(t)
	d.SchemaVersion = "x"
	if p := ValidateShiftingSOP(d); !has(p, "shifting.schema_version") {
		t.Fatalf("schema_version not refused: %v", p)
	}
	// Completion with no compulsory slot: the move must be proven by something the verifier sees.
	d = parseShiftingSeed(t)
	d.Completion.Proofs[0].Required = false
	if p := ValidateShiftingSOP(d); !has(p, "shifting.completion.proofs") {
		t.Fatalf("optional-only completion not refused: %v", p)
	}
	// Completion with no slot at all.
	d = parseShiftingSeed(t)
	d.Completion.Proofs = nil
	if p := ValidateShiftingSOP(d); !has(p, "shifting.completion.proofs") {
		t.Fatalf("empty completion not refused: %v", p)
	}
	// High-priority section authored away entirely: the feed fingerprint still gates a high move,
	// so the section must keep at least one compulsory capture.
	d = parseShiftingSeed(t)
	d.HighPriority.Proofs = nil
	if p := ValidateShiftingSOP(d); !has(p, "shifting.high_priority.proofs") {
		t.Fatalf("empty high-priority not refused: %v", p)
	}
	// Raise may be questions-only, and may be empty.
	d = parseShiftingSeed(t)
	d.Raise.Questions = []authored.Question{{ID: "reason", Kind: authored.QuestionText, Title: "Why", Required: true}}
	if p := ValidateShiftingSOP(d); len(p) != 0 {
		t.Fatalf("questions-only raise refused: %v", p)
	}
	// Slot keys unique ACROSS sections: a raise slot named like the completion video.
	d = parseShiftingSeed(t)
	d.Raise.Proofs = []authored.ProofSlot{{Key: SlotShiftingVideo, Title: "Dup", Kind: authored.KindPhoto, Required: false}}
	if p := ValidateShiftingSOP(d); !has(p, "shifting.completion.proofs.0.key") {
		t.Fatalf("cross-section duplicate key not refused: %v", p)
	}
	// Question ids unique across sections.
	d = parseShiftingSeed(t)
	d.Raise.Questions = []authored.Question{{ID: "q1", Kind: authored.QuestionText, Title: "A"}}
	d.Completion.Questions = []authored.Question{{ID: "q1", Kind: authored.QuestionText, Title: "B"}}
	if p := ValidateShiftingSOP(d); !has(p, "shifting.completion.questions.0.id") {
		t.Fatalf("cross-section duplicate question id not refused: %v", p)
	}
	// Unknown keys named by path.
	unknown := UnknownShiftingSOPKeys(map[string]any{"shifting": map[string]any{
		"schema_version": ShiftingSOPSchemaVersion,
		"completion":     map[string]any{"proofz": []any{}},
		"extra":          1,
	}})
	if len(unknown) != 2 || unknown[0] != "completion.proofz" || unknown[1] != "extra" {
		t.Fatalf("unknown keys = %v", unknown)
	}
}

func TestLegacyShiftingProofRefsMapOntoSeededSlots(t *testing.T) {
	t.Parallel()
	legacy := map[string]string{"proof_ref": "v1", "feed_packing_proof_ref": "p1", "feed_given_proof_ref": "f1"}
	low := LegacyShiftingProofRefs("low", legacy, nil)
	if len(low) != 1 || low[SlotShiftingVideo] != "v1" {
		t.Fatalf("low = %v: the feed refs are not part of a low movement", low)
	}
	high := LegacyShiftingProofRefs("high", legacy, nil)
	if len(high) != 3 || high[SlotShiftingVideo] != "v1" || high[SlotShiftingPackingVideo] != "p1" || high[SlotShiftingFeedingVideo] != "f1" {
		t.Fatalf("high = %v", high)
	}
	// Explicit slot refs win over the legacy field for the same key; blanks contribute nothing.
	mixed := LegacyShiftingProofRefs("high", map[string]string{"proof_ref": " ", "feed_packing_proof_ref": "p1"}, authored.ProofRefs{SlotShiftingPackingVideo: "p9", "other": " "})
	if len(mixed) != 1 || mixed[SlotShiftingPackingVideo] != "p9" {
		t.Fatalf("mixed = %v", mixed)
	}
}

func TestCompletionSlotsIncludeHighSectionOnlyForHighPriority(t *testing.T) {
	t.Parallel()
	d := parseShiftingSeed(t)
	d.Completion.Questions = []authored.Question{{ID: "cq", Kind: authored.QuestionText, Title: "C"}}
	d.HighPriority.Questions = []authored.Question{{ID: "hq", Kind: authored.QuestionText, Title: "H"}}
	rules := d.Rules(7)
	if rules.Version != 7 {
		t.Fatalf("version = %d", rules.Version)
	}
	if got := rules.CompletionQuestions("low"); len(got) != 1 || got[0].ID != "cq" {
		t.Fatalf("low questions = %+v", got)
	}
	if got := rules.CompletionQuestions("high"); len(got) != 2 || got[0].ID != "cq" || got[1].ID != "hq" {
		t.Fatalf("high questions = %+v", got)
	}
	if got := rules.CompletionSlots("low"); len(got) != 1 {
		t.Fatalf("low slots = %+v", got)
	}
	if got := rules.CompletionSlots("HIGH"); len(got) != 3 {
		t.Fatalf("high slots (case-insensitive) = %+v", got)
	}
}

// The legacy columns mirror the SEEDED slot; when the farm renamed that slot away, proof_ref (which
// must never be NULL on a completed move) carries the first judged capture instead.
func TestProofRefMirrorsFirstCaptureWhenSeededSlotRenamed(t *testing.T) {
	t.Parallel()
	seeded := authored.ProofRefs{SlotShiftingVideo: "v1", SlotShiftingPackingVideo: "p1", SlotShiftingFeedingVideo: "f1"}
	proof, packing, feeding := LegacyColumnsFromRefs(seeded, []string{"v1", "p1", "f1"})
	if proof != "v1" || packing != "p1" || feeding != "f1" {
		t.Fatalf("seeded mirrors = %q %q %q", proof, packing, feeding)
	}
	renamed := authored.ProofRefs{"walk_video": "w1", "arrival_photo": "a1"}
	proof, packing, feeding = LegacyColumnsFromRefs(renamed, []string{"w1", "a1"})
	if proof != "w1" || packing != "" || feeding != "" {
		t.Fatalf("renamed mirrors = %q %q %q, want first capture and blank feed columns", proof, packing, feeding)
	}
}

// A seeded submission (no answers, no raise proofs) keys its verification item exactly as before
// this change, so a retry from an older phone collapses onto the item it already created.
func TestVerificationKeyKeepsLegacyShapeForSeededSubmission(t *testing.T) {
	t.Parallel()
	refs := []string{"v1", "p1", "f1"}
	legacy := "counts-shifting-verification:ev1:v1:p1:f1"
	if got := ShiftingVerificationKey("ev1", refs, nil, nil); got != legacy {
		t.Fatalf("seeded key = %q, want %q", got, legacy)
	}
	if got := ShiftingVerificationKey("ev1", refs, authored.Answers{}, []string{}); got != legacy {
		t.Fatalf("empty answers/raise key = %q, want legacy %q", got, legacy)
	}
	withAnswers := ShiftingVerificationKey("ev1", refs, authored.Answers{"q": json.RawMessage(`"a"`)}, nil)
	withRaise := ShiftingVerificationKey("ev1", refs, nil, []string{"r1"})
	if withAnswers == legacy || withRaise == legacy || withAnswers == withRaise {
		t.Fatalf("answers/raise must change the key: %q %q", withAnswers, withRaise)
	}
	if again := ShiftingVerificationKey("ev1", refs, authored.Answers{"q": json.RawMessage(`"a"`)}, nil); again != withAnswers {
		t.Fatal("the key is not deterministic")
	}
}

func TestRaiseCardMayBeQuestionsOnly(t *testing.T) {
	t.Parallel()
	d := parseShiftingSeed(t)
	d.Raise.Questions = []authored.Question{{ID: "reason", Kind: authored.QuestionChoice, Title: "Why", Required: true, Options: []authored.Option{{Value: "sick", Label: "Sick"}}}}
	card := d.Rules(3).RaiseCard()
	if card.Version != 3 || card.Stage != SectionRaise || len(card.Proofs) != 0 || len(card.Questions) != 1 {
		t.Fatalf("raise card = %+v", card)
	}
	if _, err := card.ValidateProofRefs(nil); err != nil {
		t.Fatalf("questions-only card refuses no proofs: %v", err)
	}
	if err := card.ValidateAnswers(nil); !errors.Is(err, authored.ErrAnswerInvalid) {
		t.Fatalf("required question unanswered not refused: %v", err)
	}
}

// The verdict consumer needs to know WHICH evidence a rework verdict judged, so a stale verdict
// (a relay replay of the old item's rework after the operator already re-shot) cannot bounce the
// fresh submission. The item's recording key is the producer's own ShiftingVerificationKey; its
// completion refs are recoverable from it, with or without the answers digest.
func TestShiftingVerificationKeyRefsRoundTripsTheKey(t *testing.T) {
	a, b := "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"
	seeded := ShiftingVerificationKey("ev1", []string{a}, nil, nil)
	withDigest := ShiftingVerificationKey("ev1", []string{a, b}, authored.Answers{"calm": json.RawMessage(`"yes"`)}, nil)
	withRaise := ShiftingVerificationKey("ev1", []string{b}, nil, []string{a})
	cases := []struct {
		key  string
		want []string
	}{
		{seeded, []string{a}},
		{withDigest, []string{a, b}},
		{withRaise, []string{b}},
		{"counts-shifting-verification:ev1:legacy-ref", []string{"legacy-ref"}},
		{"counts-shifting-verification:other-event:" + a, nil},
		{"feed-packing:ev1:" + a, nil},
		{"", nil},
	}
	for _, c := range cases {
		got := ShiftingVerificationKeyRefs(c.key, "ev1")
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Fatalf("refs(%q) = %v, want %v", c.key, got, c.want)
		}
	}
}
