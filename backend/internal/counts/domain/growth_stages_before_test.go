package domain

import (
	"reflect"
	"testing"
)

// KID STAGE SHIFT TASKS read the ladder backwards: which stages still owe a move to reach a target.
func TestGrowthStagesBeforeWalksTheSameLadderTheRaiseObeys(t *testing.T) {
	cases := map[string][]string{
		"K1":       {"K0"},
		"K2":       {"K0", "K1"},
		"k3":       {"K0", "K1", "K2"},
		"F2-Male":  {"F2", "K0", "K1", "K2", "K3"},
		"K0":       nil,
		"ICU-Kid":  nil,
		"":         nil,
		"Pregnant": {"F2", "F2-Female", "K0", "K1", "K2", "K3", "Non-Pregnant"},
	}
	for target, want := range cases {
		if got := GrowthStagesBefore(target); !reflect.DeepEqual(got, want) {
			t.Errorf("GrowthStagesBefore(%q) = %v, want %v", target, got, want)
		}
	}
}
