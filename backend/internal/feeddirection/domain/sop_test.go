package domain

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

// The seeded documents ARE the pre-SOP behaviour: the distribution card's photo + two videos
// under the exact field keys the phones have always stamped, one packing video per bag, one
// transport video per shed, one wastage video per experiment pen, no questions. If this test
// moves, deploying the migration changes what the crew is asked -- which is the one thing the
// seed must not do.
func TestSeededFeedSOPIsThePreSOPBehaviour(t *testing.T) {
	t.Parallel()
	want := map[string][]authored.ProofSlot{
		StageDistribution: {
			{Key: SlotFeedWeightPhoto, Kind: authored.KindPhoto, Required: true},
			{Key: SlotFeedVideo, Kind: authored.KindVideo, Required: true},
			{Key: SlotWaterVideo, Kind: authored.KindVideo, Required: true},
		},
		StageWastage:   {{Key: SlotWastageVideo, Kind: authored.KindVideo, Required: true}},
		StagePacking:   {{Key: SlotPackingVideo, Kind: authored.KindVideo, Required: true}},
		StageTransport: {{Key: SlotTransportVideo, Kind: authored.KindVideo, Required: true}},
	}
	for stage, slots := range want {
		r := SeededRules(stage)
		if r.Version != 0 || r.Stage != stage {
			t.Fatalf("%s: version/stage = %d/%q", stage, r.Version, r.Stage)
		}
		if len(r.Proofs) != len(slots) {
			t.Fatalf("%s: %d slots, want %d", stage, len(r.Proofs), len(slots))
		}
		for i, w := range slots {
			g := r.Proofs[i]
			if g.Key != w.Key || g.Kind != w.Kind || !g.Required || g.Title == "" {
				t.Fatalf("%s slot %d = %+v, want key %s kind %s required", stage, i, g, w.Key, w.Kind)
			}
		}
		if len(r.Questions) != 0 {
			t.Fatalf("%s: the seed asks no questions, got %d", stage, len(r.Questions))
		}
	}
}

func TestValidateFeedSOPRequiresTheCodesOwnCardAndRefusesOthers(t *testing.T) {
	t.Parallel()
	pack, err := ParseFeedSOP(map[string]any{"feed": json.RawMessage(SeededFeedSOPJSON(SOPCodeFeedPacking))})
	if err != nil {
		t.Fatal(err)
	}
	if p := ValidateFeedSOP(SOPCodeFeedPacking, pack); len(p) != 0 {
		t.Fatalf("seeded packing invalid: %v", p)
	}
	// The packing document published under the direction code lacks the distribution card and
	// carries one the direction code does not own.
	p := ValidateFeedSOP(SOPCodeFeedDirection, pack)
	if len(p) < 2 {
		t.Fatalf("packing document under feed.direction: %v", p)
	}
	// An empty slot list, a second required slot missing, a bad kind, a duplicate key.
	bad := FeedSOP{SchemaVersion: FeedSOPSchemaVersion, Packing: &StageRules{Proofs: []authored.ProofSlot{
		{Key: "a", Title: "A", Kind: "gif", Required: false},
		{Key: "a", Title: "", Kind: authored.KindPhoto, Required: false},
	}}}
	p = ValidateFeedSOP(SOPCodeFeedPacking, bad)
	if len(p) < 4 {
		t.Fatalf("bad packing: want kind, dup key, blank title and no-compulsory problems, got %v", p)
	}
	// A photo added beside the video, a video replaced by a photo, an extra step: all valid.
	good := FeedSOP{SchemaVersion: FeedSOPSchemaVersion, Distribution: &StageRules{Proofs: []authored.ProofSlot{
		{Key: "scale_photo", Title: "Scale", Kind: authored.KindPhoto, Required: true},
		{Key: "feed_video", Title: "Feed", Kind: authored.KindEither, Required: true},
		{Key: "trough_photo", Title: "Trough after", Kind: authored.KindPhoto, Required: false},
	}, Questions: []authored.Question{{ID: "clean", Kind: authored.QuestionChoice, Title: "Trough clean?", Required: true,
		Options: []authored.Option{{Value: "yes", Label: "Yes"}, {Value: "no", Label: "No"}}}}}}
	if p := ValidateFeedSOP(SOPCodeFeedDirection, good); len(p) != 0 {
		t.Fatalf("good direction invalid: %v", p)
	}
	rules, err := good.Rules(3, StageWastage)
	if err != nil || rules.Version != 3 || len(rules.Proofs) != 1 || rules.Proofs[0].Key != SlotWastageVideo {
		t.Fatalf("a direction document without a wastage block must read the seeded wastage card: %+v %v", rules, err)
	}
}

func TestUnknownFeedSOPKeysNamesThePath(t *testing.T) {
	t.Parallel()
	raw := map[string]any{"feed": map[string]any{
		"schema_version": FeedSOPSchemaVersion,
		"packing":        map[string]any{"proofz": []any{}, "proofs": []any{map[string]any{"key": "x", "titel": "X", "kind": "video"}}},
		"extra":          1,
	}}
	got := UnknownFeedSOPKeys(raw)
	want := []string{"extra", "packing.proofs.0.titel", "packing.proofz"}
	if len(got) != len(want) {
		t.Fatalf("unknown keys = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unknown keys = %v, want %v", got, want)
		}
	}
}

// An older phone sends the fixed fields; they land on the seeded slots and are judged by the
// same card. An explicit slot ref wins over the legacy field for the same key.
func TestLegacyProofRefsMapOntoTheSeededSlots(t *testing.T) {
	t.Parallel()
	refs := LegacyProofRefs(StageDistribution, map[string]string{
		"feed_weight_proof_ref": "p1", "distribution_proof_ref": "v1", "water_proof_ref": " ",
	}, authored.ProofRefs{SlotWaterVideo: "v2", SlotFeedVideo: "vX"})
	if refs[SlotFeedWeightPhoto] != "p1" || refs[SlotFeedVideo] != "vX" || refs[SlotWaterVideo] != "v2" || len(refs) != 3 {
		t.Fatalf("refs = %v", refs)
	}
	ordered, err := SeededRules(StageDistribution).ValidateProofRefs(refs)
	if err != nil || len(ordered) != 3 || ordered[0].Ref != "p1" || ordered[2].Ref != "v2" {
		t.Fatalf("ordered = %+v, %v", ordered, err)
	}
	// A compulsory slot missing is refused by slot key.
	_, err = SeededRules(StageDistribution).ValidateProofRefs(authored.ProofRefs{SlotFeedVideo: "v1"})
	var pe *authored.ProofError
	if !errors.Is(err, authored.ErrProofInvalid) || !errors.As(err, &pe) || pe.SlotKey != SlotFeedWeightPhoto {
		t.Fatalf("missing photo: %v", err)
	}
	fw, f, w, _, _, _ := LegacyFieldsFromRefs(StageDistribution, refs)
	if fw != "p1" || f != "vX" || w != "v2" {
		t.Fatalf("legacy fields = %s %s %s", fw, f, w)
	}
}
