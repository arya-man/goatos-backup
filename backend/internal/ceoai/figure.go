package ceoai

// figure.go: how MANY DECIMAL PLACES a figure keeps by the time a leader reads
// it.
//
// scalarString/factValueString fixed the other half of this: a `numeric`
// column used to reach the reader as the pgtype.Numeric struct pgx decoded it
// into, and a cast one reached it carrying the column's full scale. Both now
// render a plain decimal -- but a plain decimal is not yet a figure a person
// reads. The reviewer measured this answer on the branch:
//
//	Average animal weight: Castro 1 30.25, Castro 2 31.4941176470588235,
//	Castro 3 30.1111111111111111, Gandhi 1 32.9625.
//
// 16 decimal places of a goat's weight in kilograms. The arithmetic is right --
// avg() over a `numeric` simply carries every digit the division produces --
// and nothing between the database and the answer ever decided how much of
// that a CEO is owed. Trimming trailing zeros cannot help: none of those
// digits is a zero.
//
// ONE BLANKET RULE WOULD BE WORSE THAN THE BUG. Two decimals is right for a
// weight in kg and wrong for a conversion ratio; four is right for a ratio and
// silly on a rupee amount; rounding a count at all is a lie. So the precision
// is DERIVED, from the two pieces of repo-owned description that travel with
// every fact: the read contract's `unit` column, and the row's own `label`
// (which on the toolbox route IS the source column name, and on the SQL route
// is the phrase the read wrote for the measure). Nothing here is keyed to a
// question, to a view, or to a hardcoded list of measures.
//
// The function only ever REMOVES precision the reader cannot use, never adds
// any: a value already shorter than its measure's allowance is returned
// byte-identical, so `149.4` stays `149.4` and an integer-valued figure never
// gains a decimal point it did not have.

import (
	"regexp"
	"strconv"
	"strings"
)

// plainDecimal matches a figure and nothing else: no exponent, no unit, no
// thousands separator, no leading plus. A label, a scope, an ear tag and a
// status word can never match it, so nothing that is not a figure is rounded.
var plainDecimal = regexp.MustCompile(`^-?\d+\.\d+$`)

// nonFigureToken splits a unit/label into the words precision is judged on.
var nonFigureToken = regexp.MustCompile(`[^a-z0-9]+`)

// Decimal places per measure family. Each is the precision at which the family
// STOPS CARRYING INFORMATION a leader can act on, which is why they differ:
//
//   - a percent is read to a tenth ("94.2% covered"); the digits below that
//     are the denominator's noise.
//   - money is read to the minor unit. Two places is the whole POINT of money,
//     not a sane default imposed on it: a price per kg that divides out to
//     596.71800000000000000000 is 596.72, and any further digit is a fraction
//     of a paisa nobody can pay.
//   - a RATIO is the one family where the small digits are the measurement. A
//     feed conversion of 3.1416 and one of 3.14 are different facts, so ratios
//     keep four places instead of being flattened into the money rule.
//   - a mass/volume/gain figure is read to the precision THE SCALE HAS. Farm
//     scales report to 10 g, so 31.4941176470588235 kg is 31.49 kg.
//   - a duration in days or hours is read to a tenth.
const (
	percentPlaces  = 1
	moneyPlaces    = 2
	ratioPlaces    = 4
	measuredPlaces = 2
	durationPlaces = 1
	// unknownPlaces is what a measure whose family neither the unit nor the
	// label names keeps. It is deliberately the LOOSEST of the families rather
	// than the tightest: an unrecognised measure is likelier to be a ratio-like
	// quantity than money, and three places still makes a 16-decimal figure
	// impossible. Whatever family a figure claims, an absurd one is caught.
	unknownPlaces = 3
)

// ratioWords name a quantity whose small digits ARE the measurement. Checked
// before the mass family so "gain ratio" is a ratio rather than a weight.
var ratioWords = map[string]bool{
	"ratio": true, "ratios": true, "share": true, "index": true,
	"rate": true, "factor": true, "conversion": true, "fcr": true,
	"multiplier": true, "coefficient": true,
}

// moneyWords name an amount of money. "value" is deliberately absent: it is
// the fact contract's own figure column name and rides on every row.
var moneyWords = map[string]bool{
	"inr": true, "usd": true, "eur": true, "rs": true, "rupee": true, "rupees": true,
	"price": true, "prices": true, "cost": true, "costs": true, "revenue": true,
	"amount": true, "spend": true, "payout": true, "margin": true,
	"outstanding": true, "invoice": true, "payment": true, "payments": true,
	"sales": true, "sale": true, "realisation": true, "realization": true,
}

// measuredWords name something a scale or a meter reports.
var measuredWords = map[string]bool{
	"kg": true, "kgs": true, "kilogram": true, "kilograms": true,
	"g": true, "gram": true, "grams": true, "mg": true,
	"tonne": true, "tonnes": true, "ton": true, "tons": true,
	"l": true, "ml": true, "litre": true, "litres": true, "liter": true, "liters": true,
	"weight": true, "weights": true, "wt": true, "adg": true, "gain": true,
	"feed": true, "fed": true, "dose": true, "doses": true, "dosage": true,
	"volume": true, "quantity": true, "qty": true, "intake": true,
}

// durationWords name an elapsed time.
var durationWords = map[string]bool{
	"day": true, "days": true, "hour": true, "hours": true,
	"age": true, "duration": true, "month": true, "months": true,
}

// percentWords name a proportion already multiplied out.
var percentWords = map[string]bool{
	"percent": true, "percentage": true, "percentages": true, "pct": true,
}

// measureDescription returns the text readableFigure should read as the
// measure's own description, given the row's label, its scope and the view the
// statement read.
//
// A KEY IN THE LABEL SLOT DESCRIBES NOTHING, AND THE PRECISION HAS TO COME
// FROM SOMEWHERE THAT DOES. The composer already knows this shape: a
// model-drafted read is free to write
//
//	SELECT park_label AS label, park_label AS scope, avg(weight_kg) AS value
//
// and renderFacts collapses the "Castro 1 in Castro 1" that produces. But the
// figure had already been rendered by then, from a label that names a PARK.
// No park name carries a measure word, so the figure fell to unknownPlaces and
// the live answer to "what is the average weight per park" read
// "Channapatna: 32.948" — a weight in kilograms at a thousandth of a kilogram,
// out of the one file whose job is that this cannot happen.
//
// The source view is repo-owned description, exactly like the read contract's
// `unit`: `weighing_latest_individual_weight` names a weight whatever a model
// captions the row. It is used ONLY when the label is the key — when the two
// slots differ the label IS the measure's phrase and stays the evidence, so
// nothing that reads correctly today changes.
func measureDescription(label, scope, sourceView string) string {
	if strings.TrimSpace(scope) == "" || !strings.EqualFold(strings.TrimSpace(label), strings.TrimSpace(scope)) {
		return label
	}
	if strings.TrimSpace(sourceView) == "" {
		return label
	}
	return sourceView
}

// readableFigure renders one already-stringified database scalar as the figure
// a leader reads: the same number, at the precision its measure carries.
//
// unit is the read contract's `unit` column (often empty) and label is the
// row's own label or source column name. Neither is trusted to be anything in
// particular -- they are read only for the words they happen to contain.
func readableFigure(rendered, unit, label string) string {
	s := strings.TrimSpace(rendered)
	if !plainDecimal.MatchString(s) {
		return rendered
	}
	places := figurePlaces(unit, label)
	if decimalPlaces(s) <= places {
		return rendered
	}
	// THE SAFETY NET IS THE DIGIT COUNT, NOT THE PARSE ERROR. This used to say
	// that an unparseable value meant more integer digits than a float64
	// carries exactly, and leave it alone on that basis. strconv.ParseFloat
	// returns an error only on SYNTAX or RANGE — never on precision loss — so
	// the net was never there: 12345678901234567.891 came back as
	// 12345678901234568, with the INTEGER part changed. Counting the
	// significant digits is the check the comment always described.
	if !roundTripIsExact(s, places) {
		return rendered
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return rendered
	}
	out := trimDecimalZeros(strconv.FormatFloat(f, 'f', places, 64))
	// A FIGURE THAT IS NOT ZERO MUST NOT BE REPORTED AS ZERO. This function's
	// contract is that it only ever removes precision the reader cannot use;
	// for a small magnitude it was removing the figure itself. 0.0412 of a
	// percent rendered as literal `0`, and a reader cannot tell that from a
	// measurement that really is nothing. When the allowance rounds a nonzero
	// value away, the value keeps the precision it needs to stay true — which
	// is the same rule as "already shorter than its allowance is returned
	// unchanged", applied at the other end of the scale.
	if isZeroFigure(out) && !isZeroFigure(s) {
		return rendered
	}
	return out
}

// roundTripIsExact reports that the digits this figure will KEEP survive a
// float64 round trip. Discarding the digits below the allowance is the whole
// job, so only the retained ones matter: the integer part plus `places`. A
// float64 carries 15 decimal digits exactly, so a figure whose retained part
// is wider than that is left at the width the database rendered it — which is
// what the old comment claimed and the old code never did.
func roundTripIsExact(s string, places int) bool {
	const float64DecimalDigits = 15
	return integerDigits(s)+places <= float64DecimalDigits
}

// integerDigits counts the digits before the decimal point.
func integerDigits(s string) int {
	n := 0
	for _, r := range s {
		if r == '.' {
			break
		}
		if r >= '0' && r <= '9' {
			n++
		}
	}
	return n
}

// isZeroFigure reports a rendered decimal that is numerically zero, whatever
// its sign or width ("0", "-0", "0.00").
func isZeroFigure(s string) bool {
	for _, r := range s {
		if r >= '1' && r <= '9' {
			return false
		}
	}
	return true
}

// decimalPlaces counts the digits after the decimal point of a plainDecimal.
func decimalPlaces(s string) int {
	dot := strings.IndexByte(s, '.')
	if dot < 0 {
		return 0
	}
	return len(s) - dot - 1
}

// figurePlaces derives the decimal places this measure keeps, from the words
// its unit and label are written with.
//
// THE UNIT IS ASKED FIRST and the label only when the unit names no family,
// because the unit is the read contract's own statement of what the figure IS
// while the label is a sentence written for a reader. "Average age at sale"
// carries the word `sale`, and reading the label first priced a duration in
// rupees; its unit says `days` and settles it.
//
// Within each side the order is a specificity order: a percent is a percent
// whatever else it is called, and a ratio is judged before the mass family so
// "gain ratio" is not read as a weight.
func figurePlaces(unit, label string) int {
	symbols := unit + label
	if strings.Contains(symbols, "%") {
		return percentPlaces
	}
	if strings.ContainsAny(symbols, "₹$€£") {
		return moneyPlaces
	}
	if places, ok := familyPlaces(figureWords(unit)); ok {
		return places
	}
	// A RATE QUOTED *PER* A UNIT IS A PRICE. `rate` is in both vocabularies —
	// a reject rate is a ratio, a rate per kilo is money — and the ratio
	// family is tested first, so the live answer to "what rate per kilo are we
	// getting" came back as ₹565.8139: one hundredth of a paisa, from a file
	// whose own header says "a price per kg that divides out to
	// 596.71800000000000000000 is 596.72, and any further digit is a fraction
	// of a paisa nobody can pay". The two halves of the file disagreed and the
	// reader got the wrong one. Only this shape disambiguates `rate`, so only
	// this shape is read before the families.
	if isPricePerUnit(label) {
		return moneyPlaces
	}
	if places, ok := familyPlaces(figureWords(label)); ok {
		return places
	}
	return unknownPlaces
}

// ratePerUnit matches the "<rate> per <something>" shape.
var ratePerUnit = regexp.MustCompile(`\b(rate|rates|price|prices|realisation|realization)\s+per\b`)

// isPricePerUnit reports a label that quotes a rate per a unit AND whose
// numerator names no other family. The numerator test is what keeps "gain rate
// per day" a ratio: `gain` is a measured quantity, so the phrase is not a
// price however it is divided.
func isPricePerUnit(label string) bool {
	low := strings.ToLower(label)
	loc := ratePerUnit.FindStringIndex(low)
	if loc == nil {
		return false
	}
	numerator := figureWords(low[:loc[1]])
	delete(numerator, "per")
	if anyWord(numerator, measuredWords) || anyWord(numerator, durationWords) || anyWord(numerator, percentWords) {
		return false
	}
	return true
}

func familyPlaces(words map[string]bool) (int, bool) {
	switch {
	case anyWord(words, percentWords):
		return percentPlaces, true
	case anyWord(words, ratioWords):
		return ratioPlaces, true
	case anyWord(words, moneyWords):
		return moneyPlaces, true
	case anyWord(words, measuredWords):
		return measuredPlaces, true
	case anyWord(words, durationWords):
		return durationPlaces, true
	}
	return 0, false
}

// figureWords splits a unit or a label into lower-case word tokens. WHOLE
// tokens are matched, never substrings: "kg" has to be the unit, not the
// middle of somebody's name.
func figureWords(text string) map[string]bool {
	out := map[string]bool{}
	for _, t := range nonFigureToken.Split(strings.ToLower(text), -1) {
		if t != "" {
			out[t] = true
		}
	}
	return out
}

func anyWord(words, family map[string]bool) bool {
	for w := range words {
		if family[w] {
			return true
		}
	}
	return false
}
