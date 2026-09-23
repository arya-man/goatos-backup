package ceoai

import "testing"

// TestASmallFigureIsNeverRoundedToZero. figure.go's contract is that it "only
// ever REMOVES precision the reader cannot use"; for a small magnitude it was
// removing the figure. Measured on the branch: 0.00456 INR "Cost per gram" ->
// "0", and 0.0412 % "Mortality percent" -> "0". A reader cannot tell that from
// a measurement that really is nothing.
func TestASmallFigureIsNeverRoundedToZero(t *testing.T) {
	for _, tc := range []struct{ in, unit, label string }{
		{"0.00456", "INR", "Cost per gram"},
		{"0.0412", "%", "Mortality percent"},
		{"-0.00001", "kg", "Average weight"},
		{"0.0004", "", "Something nobody named"},
	} {
		got := readableFigure(tc.in, tc.unit, tc.label)
		if isZeroFigure(got) {
			t.Errorf("readableFigure(%q, %q, %q) = %q — a nonzero quantity reported as zero",
				tc.in, tc.unit, tc.label, got)
		}
	}
	// A value that really is zero is still rendered as zero.
	if got := readableFigure("0.000", "kg", "Average weight"); !isZeroFigure(got) {
		t.Errorf("a genuine zero must still render as zero, got %q", got)
	}
}

// TestAPricePerUnitIsMoneyNotARatio. Live: "what rate per kilo are we getting"
// -> "565.8139". `rate` is in ratioWords and ratio is tested before money, so
// a rupee price rendered to one hundredth of a paisa — contradicting this
// file's own header, which names exactly this case.
func TestAPricePerUnitIsMoneyNotARatio(t *testing.T) {
	for _, tc := range []struct{ in, unit, label, want string }{
		{"565.81390000", "", "Average rate per kilo", "565.81"},
		{"596.71833333333333", "", "Realised price per kg", "596.72"},
		{"565.81390000", "", "Rate per head", "565.81"},
		// Unchanged: a dimensionless ratio keeps its four places, and a rate
		// whose NUMERATOR names another family is not a price.
		{"12.3456789012345678", "", "Reject rate", "12.3457"},
		{"12.3456789012345678", "", "Feed conversion ratio", "12.3457"},
		{"12.3456789012345678", "", "Gain rate per day", "12.3457"},
		{"12.3456789012345678", "%", "Coverage rate per park", "12.3"},
	} {
		if got := readableFigure(tc.in, tc.unit, tc.label); got != tc.want {
			t.Errorf("readableFigure(%q, %q, %q) = %q, want %q", tc.in, tc.unit, tc.label, got, tc.want)
		}
	}
}

// TestAFigureWiderThanFloat64IsLeftAlone. figure.go claimed a ParseFloat
// safety net that does not exist: ParseFloat errors only on syntax or range,
// never on precision loss, so 12345678901234567.891 came back as
// 12345678901234568 — the INTEGER part changed. The net is now the digit
// count the comment always described.
func TestAFigureWiderThanFloat64IsLeftAlone(t *testing.T) {
	const wide = "12345678901234567.891"
	if got := readableFigure(wide, "INR", "Total sales amount"); got != wide {
		t.Errorf("readableFigure(%q) = %q — a figure too wide for float64 was rounded, changing its integer part", wide, got)
	}
	// Everything a farm can actually produce still rounds.
	if got := readableFigure("31.4941176470588235", "kg", "Average animal weight"); got != "31.49" {
		t.Errorf("an ordinary figure stopped being trimmed: %q", got)
	}
}
