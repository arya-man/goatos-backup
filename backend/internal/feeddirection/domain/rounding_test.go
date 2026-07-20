package domain

import (
	"math/big"
	"testing"
)

// Rounding is the last step before a number becomes a physical bag of feed, so it gets its own
// tests rather than being asserted incidentally through the generator. Every case below is a
// quantity a packer would have to weigh out.

func TestRoundUpGramsRoundsUpToTheStepAndLeavesExactMultiplesAlone(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		grams string
		step  int64
		want  int64
	}{
		{name: "exact multiple is untouched", grams: "900", step: 100, want: 900},
		{name: "one gram over rounds up a whole step", grams: "901", step: 100, want: 1000},
		{name: "just under the step rounds up", grams: "99", step: 100, want: 100},
		{name: "fractional grams round up", grams: "1250.5", step: 100, want: 1300},
		{name: "exactly on a fractional boundary rounds up", grams: "1200.001", step: 100, want: 1300},
		// An authored zero is a real "feed nothing" instruction (milk-fed K0/K1 kids). Rounding it up
		// to a 100 g minimum would invent feed the author never specified.
		{name: "authored zero stays zero and is not raised to one step", grams: "0", step: 100, want: 0},
		{name: "a step of one is the identity for whole grams", grams: "137", step: 1, want: 137},
		{name: "a non-positive step degrades to one gram, never to a default", grams: "137.4", step: 0, want: 138},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			grams, ok := ParseDecimal(tc.grams)
			if !ok {
				t.Fatalf("ParseDecimal(%q) failed", tc.grams)
			}
			if got := RoundUpGrams(grams, tc.step); got != tc.want {
				t.Fatalf("RoundUpGrams(%s, %d) = %d, want %d", tc.grams, tc.step, got, tc.want)
			}
		})
	}
}

func TestRoundUpGramsTreatsNilAndNegativeAsZero(t *testing.T) {
	t.Parallel()
	if got := RoundUpGrams(nil, 100); got != 0 {
		t.Fatalf("nil quantity = %d, want 0", got)
	}
	if got := RoundUpGrams(new(big.Rat).SetInt64(-500), 100); got != 0 {
		t.Fatalf("negative quantity = %d, want 0 (floored, never rounded away from zero)", got)
	}
}

func TestGramsToKgStringAlwaysUsesThreeDecimals(t *testing.T) {
	t.Parallel()
	cases := []struct {
		grams int64
		want  string
	}{
		{grams: 0, want: "0.000"},
		{grams: 100, want: "0.100"},
		{grams: 1000, want: "1.000"},
		{grams: 12300, want: "12.300"},
		{grams: 1234567, want: "1234.567"},
	}
	for _, tc := range cases {
		if got := GramsToKgString(tc.grams); got != tc.want {
			t.Fatalf("GramsToKgString(%d) = %q, want %q", tc.grams, got, tc.want)
		}
	}
}

// The rounding policy is a named, overridable seam rather than scattered arithmetic. These tests
// cover BOTH paths -- the standard 100 g rule in force today and the per-item override the
// baking-soda rule will drop into -- so wiring that rule later needs no new test scaffolding.
func TestRoundingPolicyStepResolution(t *testing.T) {
	t.Parallel()

	standard := StandardRoundingPolicy()
	if got := standard.StepGramsFor("concentrate"); got != DefaultRoundingStepGrams {
		t.Fatalf("standard policy step = %d, want %d", got, DefaultRoundingStepGrams)
	}
	// No per-item exception is wired today: every item, baking soda included, takes the 100 g rule.
	if got := standard.StepGramsFor("Baking Soda"); got != DefaultRoundingStepGrams {
		t.Fatalf("baking soda step = %d, want the general %d rule until the exact rule is confirmed",
			got, DefaultRoundingStepGrams)
	}

	overridden := RoundingPolicy{
		DefaultStepGrams: 100,
		PerItemStepGrams: map[string]int64{"baking_soda": 10},
	}
	// The override is looked up by NORMALIZED key, so a raw label with different spacing/case still
	// finds it -- the same normalization rule the ration grid joins on.
	if got := overridden.StepGramsFor("Baking Soda"); got != 10 {
		t.Fatalf("overridden baking soda step = %d, want 10", got)
	}
	if got := overridden.StepGramsFor(" Baking -_ soda "); got != 10 {
		t.Fatalf("override must resolve through key normalization, got %d", got)
	}
	if got := overridden.StepGramsFor("concentrate"); got != 100 {
		t.Fatalf("unoverridden item = %d, want the default 100", got)
	}
	// An explicitly non-positive override means "no rounding", not "fall back to 100" -- silently
	// substituting a business default the author never entered is banned.
	zeroed := RoundingPolicy{DefaultStepGrams: 100, PerItemStepGrams: map[string]int64{"x": 0}}
	if got := zeroed.StepGramsFor("x"); got != 1 {
		t.Fatalf("explicit zero override = %d, want 1 (identity), never the default", got)
	}
}

func TestRoundSessionGramsToKgAppliesPolicyThenConverts(t *testing.T) {
	t.Parallel()
	policy := StandardRoundingPolicy()
	grams, _ := ParseDecimal("1250.5")
	rounded, kg := RoundSessionGramsToKg(grams, "concentrate", policy)
	if rounded != 1300 {
		t.Fatalf("rounded = %d, want 1300", rounded)
	}
	if kg != "1.300" {
		t.Fatalf("kg = %q, want \"1.300\"", kg)
	}
}

// NormalizeConfigKey must be byte-identical to the database's feed_config_norm(), because the
// generator joins live free-text values to authored labels in memory. A mismatch does not fail
// loudly -- it blocks a shed whose ration is perfectly well authored.
func TestNormalizeConfigKeyMatchesFeedConfigNormSemantics(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{in: "ICU-Kid", want: "icu_kid"},
		// The real live variant: a space after the hyphen. 42 animals carry this spelling and 27
		// carry the other; both must land on one key.
		{in: "ICU- kid", want: "icu_kid"},
		{in: "F2-Male", want: "f2_male"},
		{in: "F2- Male", want: "f2_male"},
		{in: "Milking Warmup", want: "milking_warmup"},
		{in: "  Non-Pregnant  ", want: "non_pregnant"},
		{in: "Beetal/Sirohi", want: "beetal/sirohi"},
		{in: "Quarantine kids", want: "quarantine_kids"},
		{in: "A___B---C   D", want: "a_b_c_d"},
		{in: "", want: ""},
		{in: "   ", want: ""},
		// btrim() strips SPACES ONLY, so edge hyphens/underscores survive the trim and are then
		// collapsed into underscores -- they are NOT dropped. Getting this wrong is a silent
		// lookup miss, and it is exactly the divergence the live-database parity test caught.
		{in: "-leading-and-trailing-", want: "_leading_and_trailing_"},
		{in: "_x_", want: "_x_"},
		{in: "trailing-", want: "trailing_"},
		// A value that is nothing but separators collapses to a single underscore, not to "".
		{in: "---", want: "_"},
		// A tab survives btrim (which strips spaces only) and is then collapsed by the regex.
		{in: "a\tb", want: "a_b"},
	}
	for _, tc := range cases {
		if got := NormalizeConfigKey(tc.in); got != tc.want {
			t.Fatalf("NormalizeConfigKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeConfigKeyIsIdempotent(t *testing.T) {
	t.Parallel()
	// The stored *_key columns are already normalized by the database. Applying the Go twin to them
	// must be a no-op rather than a second transform, or every lookup against a stored key would
	// miss.
	for _, in := range []string{"icu_kid", "f2_male", "beetal/sirohi", "milking_warmup"} {
		if got := NormalizeConfigKey(in); got != in {
			t.Fatalf("NormalizeConfigKey(%q) = %q, want it unchanged", in, got)
		}
	}
}

func TestParseDecimalRejectsRatherThanDefaultingToZero(t *testing.T) {
	t.Parallel()
	// This is the silent-zero guard at its lowest level: an unparseable rate must NOT come back as
	// a usable zero, because the caller would then feed the shed nothing while looking correct.
	for _, bad := range []string{"", "   ", "abc", "1.2.3", "12g"} {
		if _, ok := ParseDecimal(bad); ok {
			t.Fatalf("ParseDecimal(%q) reported ok; a bad rate must fail so the caller blocks", bad)
		}
	}
	for _, good := range []string{"0", "0.000", "250.500", "1234.567"} {
		if _, ok := ParseDecimal(good); !ok {
			t.Fatalf("ParseDecimal(%q) failed; an authored value must parse", good)
		}
	}
}
