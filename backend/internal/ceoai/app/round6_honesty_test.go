package app

import (
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
)

// round6_honesty_test.go pins the round-5 review's two blockers and the two
// renderer defects the behavioural review found beside them. Every one of them
// is a sentence the SYSTEM writes, with no model in the loop to blame.

func salesFacts(scope string, labelSuffix string) []domain.Fact {
	return []domain.Fact{
		{TenantID: "t1", Label: "Sold animals" + labelSuffix, Value: "16", Scope: scope},
		{TenantID: "t1", Label: "Sold goats" + labelSuffix, Value: "16", Scope: scope},
		{TenantID: "t1", Label: "Sold sheep" + labelSuffix, Value: "0", Scope: scope},
		{TenantID: "t1", Label: "Sales revenue" + labelSuffix, Value: "291600", Scope: scope},
	}
}

// TestTheSalesSentenceNamesTheMonthItRead is blocker B1. Live, twice: "how
// much revenue did we make in august" answered "This month, 16 animals were
// sold … Sales revenue was 291600." 291,600 is August's and correct;
// September was 1,669,049, so a leader booked this month's revenue 5.7x low.
func TestTheSalesSentenceNamesTheMonthItRead(t *testing.T) {
	body, _, _ := composer{}.compose([]domain.ToolResult{{
		Surface: "Mesha read API · Sales overview",
		Facts:   salesFacts("2026-08", " in 2026-08"),
	}})
	if !strings.Contains(body, "For August 2026") {
		t.Errorf("the sentence must name the month it read, got %q", body)
	}
	if strings.Contains(strings.ToLower(body), "this month") {
		t.Errorf("a figure that is not this month's must never be called this month's: %q", body)
	}
}

// TestASalesLabelSayingThisMonthCannotMakeItThisMonth is the mechanism, pinned
// separately from the copy. The period came from nothing but whether a fact
// LABEL contained the substring "this month", and the reader that produced
// those labels stamped the phrase on every month it bound. A label is prose;
// the bound scope is evidence.
func TestASalesLabelSayingThisMonthCannotMakeItThisMonth(t *testing.T) {
	body, _, _ := composer{}.compose([]domain.ToolResult{{
		Surface: "Mesha read API · Sales overview",
		// The exact shape that shipped: every label says "this month", and the
		// month actually read is August.
		Facts: salesFacts("2026-08", " this month"),
	}})
	if strings.Contains(strings.ToLower(body), "this month") {
		t.Errorf("the label's words overrode the window that was read: %q", body)
	}
	if !strings.Contains(body, "For August 2026") {
		t.Errorf("want the bound month named, got %q", body)
	}
}

// TestASalesReadWithNoMonthStillSaysSo: the all-time branch must not gain a
// period it never bound.
func TestASalesReadWithNoMonthStillSaysSo(t *testing.T) {
	body, _, _ := composer{}.compose([]domain.ToolResult{{
		Surface: "Mesha read API · Sales overview",
		Facts:   salesFacts("", ""),
	}})
	if !strings.Contains(body, "Across all recorded sales") {
		t.Errorf("an unbound sales read must say it is all-time, got %q", body)
	}
}

// TestTheRefusalNeverAssertsTheRecordsExist is blocker B2. Live: "what is the
// staff attrition rate" -> "I couldn't build a read for that question right
// now. The underlying records exist, so please rephrase it and I'll try
// again." The farm records no attrition, and this sentence has no model in it.
func TestTheRefusalNeverAssertsTheRecordsExist(t *testing.T) {
	low := strings.ToLower(refusalCouldNotBuildARead)
	for _, forbidden := range []string{
		"records exist", "the data exists", "we do have", "the underlying records",
	} {
		if strings.Contains(low, forbidden) {
			t.Errorf("the refusal asserts a fact about the farm's data it cannot know: %q", refusalCouldNotBuildARead)
		}
	}
	if !strings.Contains(low, "may not have a source") {
		t.Errorf("the refusal must say what it could not do, honestly: %q", refusalCouldNotBuildARead)
	}
}

// TestARowIsNeverRenderedAsBeingInItself is the behavioural review's chart
// key/label desync: 12 lines across 86 live answers read "Castro 1 in Castro
// 1", "Boer in Boer". A model-drafted read may put the same column in the
// label and the scope slot, and a category is never "in" itself.
func TestARowIsNeverRenderedAsBeingInItself(t *testing.T) {
	body, _, _ := composer{}.compose([]domain.ToolResult{{
		Surface: "Mesha operational data",
		Facts: []domain.Fact{
			{TenantID: "t1", Label: "Castro 1", Value: "30.25", Scope: "Castro 1"},
			{TenantID: "t1", Label: "castro 2", Value: "31.49", Scope: "Castro 2"},
		},
	}})
	if strings.Contains(body, "Castro 1 in Castro 1") {
		t.Errorf("the series key landed in the label slot: %q", body)
	}
	if strings.Contains(strings.ToLower(body), "castro 2 in castro 2") {
		t.Errorf("the duplicate must be caught whatever its case: %q", body)
	}
	for _, want := range []string{"Castro 1", "30.25", "Castro 2", "31.49"} {
		if !strings.Contains(body, want) {
			t.Errorf("collapsing the duplicate must not lose data: %q missing from %q", want, body)
		}
	}
	// A genuine label-in-scope row still reads as one.
	body, _, _ = composer{}.compose([]domain.ToolResult{{
		Facts: []domain.Fact{
			{TenantID: "t1", Label: "Average weight kg", Value: "30.25", Scope: "Castro 1"},
			{TenantID: "t1", Label: "Average weight kg", Value: "31.49", Scope: "Castro 2"},
		},
	}})
	if !strings.Contains(body, "Castro 1") || !strings.Contains(body, "30.25") {
		t.Errorf("a real measure-in-scope row must still render: %q", body)
	}
}

// TestAChartSeriesIsNeverNamedAfterItsOwnCategory: the series NAME was the
// FIRST row's label whatever the others said, so a read that labels each row
// with its own category named the chart's series after one of its bars.
func TestAChartSeriesIsNeverNamedAfterItsOwnCategory(t *testing.T) {
	actor := domain.Actor{TenantID: "t1"}
	results := []domain.ToolResult{{
		Surface: "Mesha operational data",
		Facts: []domain.Fact{
			{TenantID: "t1", Label: "Castro 1", Value: "30.25", Scope: "Castro 1"},
			{TenantID: "t1", Label: "Castro 2", Value: "31.49", Scope: "Castro 2"},
			{TenantID: "t1", Label: "Castro 3", Value: "30.11", Scope: "Castro 3"},
		},
	}}
	chart, err := buildChart(actor, "average weight by shed", results)
	if err != nil {
		t.Fatalf("buildChart: %v", err)
	}
	if chart == nil {
		t.Fatal("a three-point dimensioned read must chart")
	}
	for _, x := range chart.X {
		if strings.EqualFold(chart.Series[0].Name, x) {
			t.Errorf("series named %q after its own category %q; x axis is %v", chart.Series[0].Name, x, chart.X)
		}
	}
	// A real shared measure label still names the series.
	results[0].Facts = []domain.Fact{
		{TenantID: "t1", Label: "Average weight kg", Value: "30.25", Scope: "Castro 1"},
		{TenantID: "t1", Label: "Average weight kg", Value: "31.49", Scope: "Castro 2"},
	}
	chart, err = buildChart(actor, "average weight by shed", results)
	if err != nil || chart == nil {
		t.Fatalf("buildChart: %v, chart=%v", err, chart)
	}
	if chart.Series[0].Name != "Average weight kg" {
		t.Errorf("a measure shared by every row must name the series, got %q", chart.Series[0].Name)
	}
}

// TestASelectListLiteralIsNotAFilter closes the entitysubstitution bypass the
// round-5 probe confirmed: a model-authored
//
//	SELECT 'MG-100001' AS label, avg(latest_weight_kg) AS value FROM …
//
// satisfied both discriminators — the tag is in Params["sql"] and the fact it
// produces echoes it — and shipped the herd average wearing one animal's name.
func TestASelectListLiteralIsNotAFilter(t *testing.T) {
	bypass := "SELECT 'MG-100001' AS label, avg(latest_weight_kg) AS value FROM ceo_ai.weighing_latest_individual_weight LIMIT 5"
	subs := []domain.SubQuestion{{Params: map[string]any{"sql": bypass}}}
	results := []domain.ToolResult{{
		SourceView: "weighing_latest_individual_weight",
		Facts:      []domain.Fact{{TenantID: "t1", Label: "MG-100001", Value: "29.67"}},
	}}
	got, tag, view := namedEntitySubstitution("what does MG-100001 weigh now", subs, results)
	if !got {
		t.Error("a literal in the SELECT list is a caption the model wrote, not evidence the read is about the animal")
	}
	if tag != "MG-100001" || view != "weighing_latest_individual_weight" {
		t.Errorf("refusal must name the tag and the view, got %q / %q", tag, view)
	}

	// The real filtered read must still be accepted, including after the
	// identity fold rewrites it.
	for _, sql := range []string{
		"SELECT 'MG-100001' AS label, weight_kg AS value FROM ceo_ai.weighing_latest_individual_weight WHERE animal_key = 'MG-100001'",
		"SELECT label, value FROM ceo_ai.weighing_latest_individual_weight WHERE upper(trim(animal_key)) = upper(trim('MG-100001'))",
	} {
		subs = []domain.SubQuestion{{Params: map[string]any{"sql": sql}}}
		if got, _, _ := namedEntitySubstitution("what does MG-100001 weigh now", subs, results); got {
			t.Errorf("a read that filters on the tag answers about the animal: %q", sql)
		}
	}

	// A read API takes its animal as an ordinary param and has no statement;
	// that path is unchanged.
	subs = []domain.SubQuestion{{Params: map[string]any{"animal_tag": "MG-100001"}}}
	if got, _, _ := namedEntitySubstitution("what does MG-100001 weigh now", subs, results); got {
		t.Error("a read API's bound animal param must still count as selecting the animal")
	}
}

// TestADocumentIdentifierIsNotAnEarTag: the regex's own comment claimed the
// digit floor kept non-animals out; it keeps operational LOCATIONS out and
// nothing else, so INV-1001 matched and the reader was told a read "reports a
// figure for the whole scope it covers, not for one animal".
func TestADocumentIdentifierIsNotAnEarTag(t *testing.T) {
	for _, q := range []string{
		"what is the total on INV-1001",
		"what did we pay on PO-20241",
		"which tasks belong to SOP-1234",
		"are we compliant with ISO-9001",
	} {
		if tags := earTagsIn(q); len(tags) != 0 {
			t.Errorf("%q: paperwork read as an ear tag: %v", q, tags)
		}
	}
	for _, q := range []string{
		"what does MG-100001 weigh now",
		"compare MG-100001 and MG-100002",
		"show me AB-12345",
	} {
		if tags := earTagsIn(q); len(tags) == 0 {
			t.Errorf("%q: a real ear tag stopped being recognised", q)
		}
	}
}
