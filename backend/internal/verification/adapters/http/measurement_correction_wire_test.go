package http

import (
	"encoding/json"
	"testing"

	"github.com/vgoats/goatos/backend/internal/verification/domain"
)

// The verifier's weight-correction control is rendered by BOTH clients off this one
// wire block. These pin the two ways it can be wrong without any test noticing: the
// block appearing on a category that declares no correctable measurement (a control
// with no write behind it), and the head-count field appearing on the grain whose
// write path refuses it.

// countBearingSpec is a SYNTHETIC spec exercising the count-field mechanics.
// No production category declares a CountLabel any more: weighing's lump-sum
// head count was the only user and was retired on 2026-08-24 (the count is a
// frozen herd-register snapshot taken at submit). The wire mechanism stays for
// a future category, so these tests keep pinning it with a fixture of their own.
func countBearingSpec() *domain.MeasurementCorrectionSpec {
	return &domain.MeasurementCorrectionSpec{
		Title:         "Correct the weight",
		Help:          "Enter the weight you can see in the video. It replaces the weight recorded here.",
		ValueLabel:    "Corrected weight (kg)",
		SubmitLabel:   "Save corrected weight",
		CountLabel:    "Goats on the scale",
		CountRefTypes: []string{"weighing_shed_observation"},
	}
}

func TestCorrectionBlockIsAbsentForACategoryThatDeclaresNone(t *testing.T) {
	// Every category but weighing declares nothing today. A block emitted here would
	// put a correction control on a vaccination or feed proof with no route behind it.
	row := domain.QueueRow{Item: domain.Item{
		Source: domain.SourceRef{Module: "vaccination", RefType: "vaccination_submission", RefID: "abc"},
	}}
	if got := toQueueItemResponse(row, nil).MeasurementCorrection; got != nil {
		t.Fatalf("no declaration must mean no control, got %+v", got)
	}
}

func TestLumpSumCarriesTheHeadCountFieldAndIndividualDoesNot(t *testing.T) {
	// The write path REFUSES a head count on an individual capture
	// (domain.ValidateWeightCorrection -> animal_count_not_applicable). Offering the
	// field there would invite a value the server rejects, so the two must agree.
	lump := toQueueItemResponse(domain.QueueRow{Item: domain.Item{
		Source: domain.SourceRef{Module: "weighing", RefType: "weighing_shed_observation", RefID: "shed-obs-1"},
	}}, countBearingSpec()).MeasurementCorrection
	if lump == nil || lump.CountLabel != "Goats on the scale" {
		t.Fatalf("a lump-sum item must carry the head-count field, got %+v", lump)
	}

	individual := toQueueItemResponse(domain.QueueRow{Item: domain.Item{
		Source: domain.SourceRef{Module: "weighing", RefType: "weighing_observation", RefID: "obs-1"},
	}}, countBearingSpec()).MeasurementCorrection
	if individual == nil {
		t.Fatal("an individual weighing item must still carry the correction control")
	}
	if individual.CountLabel != "" {
		t.Fatalf("an individual item weighs one animal and must carry no head-count field, got %q", individual.CountLabel)
	}
}

func TestCorrectionBlockAddressesTheItemsOwnSourceRecord(t *testing.T) {
	// The client posts the correction against these two values. Composing an address
	// of its own is exactly what the backend-owns-the-contract rule forbids, so they
	// must echo the item's source verbatim.
	block := toQueueItemResponse(domain.QueueRow{Item: domain.Item{
		Source: domain.SourceRef{Module: "weighing", RefType: "weighing_shed_observation", RefID: "shed-obs-7"},
	}}, countBearingSpec()).MeasurementCorrection
	if block.RefType != "weighing_shed_observation" || block.ObservationID != "shed-obs-7" {
		t.Fatalf("the control must address the item's own source record, got %+v", block)
	}
}

func TestCorrectionBlockMarshalsEveryLabelItPromises(t *testing.T) {
	// Clients render these verbatim and compose none of them. A field that serialises
	// empty ships a blank heading or an unlabelled input on the verifier's screen.
	raw, err := json.Marshal(toQueueItemResponse(domain.QueueRow{Item: domain.Item{
		Source: domain.SourceRef{Module: "weighing", RefType: "weighing_shed_observation", RefID: "shed-obs-1"},
	}}, countBearingSpec()).MeasurementCorrection)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"ref_type", "observation_id", "title", "help", "value_label", "submit_label", "count_label"} {
		if value, _ := decoded[key].(string); value == "" {
			t.Fatalf("%s must reach the client with real copy, got %q", key, value)
		}
	}
}
