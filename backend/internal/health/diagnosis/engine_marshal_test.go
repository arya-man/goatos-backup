package diagnosis

import (
	"encoding/json"
	"strings"
	"testing"
)

// A REJECTED proposal must still serialise every list as an array.
//
// This is the shape that broke the phone: a kid form missing `landing` is rejected, so
// every list on the proposal is nil. Go marshals nil to `null`, app-api.yaml declares
// these as `type: array`, and the client types them as non-optional lists -- so the
// response was undecodable, the sync engine classed the decode failure as retryable, and
// the observation screen sat on "Recorded. The assessment will appear once this syncs."
// forever for a form the engine had already judged.
//
// Asserted on the JSON rather than on the struct, because the struct is fine either way:
// the defect lives entirely in serialisation.
func TestRejectedProposalSerialisesListsAsArraysNotNull(t *testing.T) {
	// The zero value IS the rejected shape: nothing is populated.
	raw, err := json.Marshal(Proposal{Valid: false, RejectReason: "landing_required", Scope: ClassKidMilk})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), ":null") {
		t.Fatalf("proposal emitted a null where the contract declares an array or object:\n%s", raw)
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"emergencies", "problems", "covered", "rechecks", "field_actions", "unexplained",
		"ongoing", "new", "propose_close", "propose_extend", "hd_flags", "hints",
	} {
		v, ok := decoded[key]
		if !ok {
			t.Errorf("%s is missing; the contract declares it an array", key)
			continue
		}
		if _, isList := v.([]any); !isList {
			t.Errorf("%s = %#v, want a JSON array", key, v)
		}
	}
	for _, key := range []string{"tiers", "sop", "course_type"} {
		if _, isObj := decoded[key].(map[string]any); !isObj {
			t.Errorf("%s = %#v, want a JSON object", key, decoded[key])
		}
	}
}

// An ACCEPTED proposal must keep its real values -- the normalisation must not blank them.
func TestAcceptedProposalKeepsItsValuesThroughMarshalling(t *testing.T) {
	raw, err := json.Marshal(Proposal{
		Valid: true, Scope: ClassAdult, RegisterVersion: "adult-1",
		Problems: []string{"FEVER"}, Tiers: map[string]Tier{"FEVER": TierProbable},
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Problems []string          `json:"problems"`
		Tiers    map[string]string `json:"tiers"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Problems) != 1 || decoded.Problems[0] != "FEVER" {
		t.Errorf("problems = %v, want [FEVER]", decoded.Problems)
	}
	if decoded.Tiers["FEVER"] == "" {
		t.Errorf("tiers lost its value: %v", decoded.Tiers)
	}
}
