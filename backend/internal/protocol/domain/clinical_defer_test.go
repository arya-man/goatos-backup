package domain

import (
	"reflect"
	"testing"
)

func TestMissingMandatoryClinicalDeferStates(t *testing.T) {
	cases := []struct {
		name    string
		present []string
		want    []string
	}{
		{name: "full set has nothing missing", present: []string{"sick", "under_treatment", "recovering", "quarantine", "icu"}, want: []string{}},
		{name: "case-insensitive full set", present: []string{"Sick", "UNDER_TREATMENT", "Recovering", "Quarantine", "ICU"}, want: []string{}},
		{name: "superset has nothing missing", present: []string{"sick", "under_treatment", "recovering", "quarantine", "icu", "post_breeding_hold"}, want: []string{}},
		{name: "icu+quarantine partial drops sick, under_treatment, and recovering", present: []string{"icu", "quarantine"}, want: []string{"sick", "under_treatment", "recovering"}},
		{name: "drops only under_treatment", present: []string{"sick", "recovering", "quarantine", "icu"}, want: []string{"under_treatment"}},
		{name: "single state drops four", present: []string{"sick"}, want: []string{"under_treatment", "recovering", "quarantine", "icu"}},
		{name: "empty drops all five", present: nil, want: []string{"sick", "under_treatment", "recovering", "quarantine", "icu"}},
		{name: "blank tokens ignored", present: []string{" ", ""}, want: []string{"sick", "under_treatment", "recovering", "quarantine", "icu"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MissingMandatoryClinicalDeferStates(tc.present)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("MissingMandatoryClinicalDeferStates(%v) = %v, want %v", tc.present, got, tc.want)
			}
		})
	}
}

func TestEffectiveClinicalDeferStatesAlwaysCoversMandatory(t *testing.T) {
	cases := [][]string{
		nil,
		{},
		{"icu"},
		{"icu", "quarantine"},
		{"sick", "quarantine", "ICU"},
		{"post_breeding_hold"},
	}
	for _, present := range cases {
		got := EffectiveClinicalDeferStates(present)
		have := make(map[string]bool, len(got))
		for _, s := range got {
			have[s] = true
		}
		for _, mandatory := range MandatoryClinicalDeferStates {
			if !have[mandatory] {
				t.Fatalf("EffectiveClinicalDeferStates(%v) = %v, missing mandatory clinical state %q", present, got, mandatory)
			}
		}
	}
}

func TestEffectiveClinicalDeferStatesPreservesAuthoredExtras(t *testing.T) {
	got := EffectiveClinicalDeferStates([]string{"icu", "post_breeding_hold", "ICU"})
	want := []string{"sick", "under_treatment", "recovering", "quarantine", "icu", "post_breeding_hold"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EffectiveClinicalDeferStates extras = %v, want %v", got, want)
	}
}
