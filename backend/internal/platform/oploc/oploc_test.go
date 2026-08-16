package oploc

import "testing"

func TestNormalizePartitionCollapsesEveryNonPartitionedEncoding(t *testing.T) {
	// The live data uses three different encodings for "no partition": SQL NULL
	// (read out as ""), empty string, and the literal 'whole' sentinel. All three
	// must mean the same thing or non-partitioned sheds fragment into ghost rows.
	for _, raw := range []string{"", "   ", "whole", "WHOLE", " Whole "} {
		if got := NormalizePartition(raw); got != WholeSentinel {
			t.Fatalf("NormalizePartition(%q) = %q, want %q", raw, got, WholeSentinel)
		}
		if IsPartitioned(raw) {
			t.Fatalf("IsPartitioned(%q) = true, want false", raw)
		}
	}
}

func TestNormalizePartitionBridgesBothLabelConventions(t *testing.T) {
	// Live data carries both conventions: bare numerics (Castro 1/2/3, Yashoda
	// 1..10) and 'Part N' (Godel 1 - Part 3, Mandela 1 - Part 10). They must
	// compare equal, matching the operator-execution SQL normalizer.
	cases := []struct{ a, b string }{
		{"3", "Part 3"},
		{"Part 3", "part 3"},
		{"10", "Part  10"},
		{"Part 1", "1"},
	}
	for _, c := range cases {
		if !SamePartition(c.a, c.b) {
			t.Fatalf("SamePartition(%q, %q) = false, want true", c.a, c.b)
		}
	}
	if SamePartition("1", "2") {
		t.Fatal("SamePartition(1, 2) = true, want false")
	}
	if SamePartition("Part 1", "Part 2") {
		t.Fatal("SamePartition(Part 1, Part 2) = true, want false")
	}
}

func TestDisplayNeverShowsWholeToAUser(t *testing.T) {
	// "Never show fake 'Yashoda whole' to CEO/operator."
	for _, raw := range []string{"", "whole", "WHOLE", "  "} {
		loc := OperationalLocation{ShedName: "Yashoda", PartitionLabel: raw}
		if got := loc.Display(); got != "Yashoda" {
			t.Fatalf("Display() with partition %q = %q, want %q", raw, got, "Yashoda")
		}
	}
}

func TestDisplayPreservesEachShedsOwnConvention(t *testing.T) {
	cases := []struct {
		shed, partition, want string
	}{
		// Bare numerals: space separator (matches the physical shed name "Castro 1" painted on the building).
		{"Castro", "1", "Castro 1"},
		{"Castro", "2", "Castro 2"},
		{"Gandhi", "3", "Gandhi 3"},
		{"Ho Chi Minh", "1", "Ho Chi Minh 1"},
		// Worded labels ("Part N"): space-dash-space separator for visual boundary (since many shed names end in digits).
		{"Godel 1", "Part 3", "Godel 1 - Part 3"},
		{"Mandela 1", "Part 10", "Mandela 1 - Part 10"},
		{"Godel 1", "Part 1", "Godel 1 - Part 1"},
		// Non-partitioned sheds: bare shed name only, no partition rendering.
		{"Yashoda", "", "Yashoda"},
		// Digit-terminated shed names with bare numeric partitions (75% of live STG data):
		// space separator remains unambiguous because "Part" prefix exists for the rare
		// worded case, so the reader knows "Godel 1 1" means partition 1 (not a shed
		// named "Godel 1 1", which would use "Part" prefix).
		{"Godel 1", "1", "Godel 1 1"},
		{"Godel 1", "10", "Godel 1 10"},
		{"Sumathi 2", "7", "Sumathi 2 7"},
		{"Old Yashoda", "5", "Old Yashoda 5"},
	}
	for _, c := range cases {
		loc := OperationalLocation{ShedName: c.shed, PartitionLabel: c.partition}
		if got := loc.Display(); got != c.want {
			t.Fatalf("Display(%q, %q) = %q, want %q", c.shed, c.partition, got, c.want)
		}
	}
}

func TestKeyGroupsByShedIDNotShedName(t *testing.T) {
	// Shed names repeat across parks: there are two "Castro" rows with different
	// location_ids. Keying on the name would merge CPT and CBE animals.
	cpt := OperationalLocation{ShedID: "shed-cpt", ShedName: "Castro", PartitionLabel: "1"}
	cbe := OperationalLocation{ShedID: "shed-cbe", ShedName: "Castro", PartitionLabel: "1"}
	if cpt.Key() == cbe.Key() {
		t.Fatal("two parks' Castro partition 1 collapsed to one key; counts would merge parks")
	}

	// Same shed, both label conventions, must be one key -- not two.
	a := OperationalLocation{ShedID: "shed-godel", PartitionLabel: "Part 3"}
	b := OperationalLocation{ShedID: "shed-godel", PartitionLabel: "3"}
	if a.Key() != b.Key() {
		t.Fatalf("Key mismatch across label conventions: %q vs %q", a.Key(), b.Key())
	}

	// Non-partitioned rows key on the shed alone, consistently across encodings.
	null := OperationalLocation{ShedID: "shed-yashoda", PartitionLabel: ""}
	whole := OperationalLocation{ShedID: "shed-yashoda", PartitionLabel: "whole"}
	if null.Key() != whole.Key() {
		t.Fatalf("non-partitioned encodings split: %q vs %q", null.Key(), whole.Key())
	}
}

func TestIsBarNumericPartitionDetectsPureNumerals(t *testing.T) {
	// Bare numerals use space separator in Display().
	cases := []struct {
		label  string
		wantOK bool
	}{
		// Bare numerals: should be true.
		{"1", true},
		{"42", true},
		{"123", true},
		{"0", true},
		{"  5  ", true}, // whitespace is trimmed before check
		// Worded labels: should be false.
		{"Part 1", false},
		{"Part 3", false},
		{"Parts 1-3", false},
		{"भाग 2", false}, // Devanagari "part"
		{"whole", false},
		{"", false},
		{"  ", false},
		// Edge cases that are not bare numerals.
		{"A", false},
		{"1A", false},
		{"Part", false},
	}
	for _, c := range cases {
		if got := isBarNumericPartition(c.label); got != c.wantOK {
			t.Errorf("isBarNumericPartition(%q) = %v, want %v", c.label, got, c.wantOK)
		}
	}
}
