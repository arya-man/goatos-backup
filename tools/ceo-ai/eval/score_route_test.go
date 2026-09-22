package main

import (
	"encoding/json"
	"testing"
)

// The live assistant's citations name the tier "route" (domain.Citation); the
// scorer must read it, or every live answer fails tool-selection with "got []".
func TestTierHitReadsTheBackendRouteField(t *testing.T) {
	var resp AssistantResponse
	if err := json.Unmarshal([]byte(`{"answer":"x","citations":[{"surface":"Mesha operational data","route":"sql","planned_by_model":true}]}`), &resp); err != nil {
		t.Fatal(err)
	}
	if hit, got := tierHit(&resp, []string{"sql"}); !hit {
		t.Fatalf("route=sql citation must satisfy tiers_any_of [sql], got %v", got)
	}
	if hit, _ := tierHit(&resp, []string{"cube"}); hit {
		t.Fatal("route=sql must not satisfy tiers_any_of [cube]")
	}
}
