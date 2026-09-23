package app

import (
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
	"github.com/vgoats/goatos/backend/internal/ceoai/reporting"
)

// TestTheLeaderFoldReachesTheMeasureGateToo is the live gap round 6 left: the
// coverage gate had been taught the farm's words and the measure gate had not,
// so "who owes us money" nominated sales_buyer_summary correctly, the planner
// read it, and the answer was refused one step later with "I don't have a
// source for owes us money" — while outstanding_rupees on that same card held
// 325,930.
func TestTheLeaderFoldReachesTheMeasureGateToo(t *testing.T) {
	cards := reporting.Cards()
	catalog := []ports.ToolSpec{{Name: "sales_overview", Description: "Sales overview read"}}

	for _, term := range []string{"owes", "money"} {
		if !termModelled(term, cards, catalog) {
			t.Errorf("the catalogue carries the column a leader means by %q, so the measure gate must not call it unmodelled", term)
		}
	}
	if unmodelled, terms := measureUnmodelled("who owes us money", cards, catalog); unmodelled {
		t.Errorf("an answerable money question was refused on %v", terms)
	}
}

// TestTheLeaderFoldStillRefusesWhatTheCatalogueNamesNothingFor is the other
// direction, and it is the one that matters: folding a leader's word must not
// make a subject the farm does not record look modelled. `cash` and `owe` both
// fold; `runway`, `rent` and `attrition` are named by nothing and still carry
// their questions to a refusal.
func TestTheLeaderFoldStillRefusesWhatTheCatalogueNamesNothingFor(t *testing.T) {
	cards := reporting.Cards()
	catalog := []ports.ToolSpec{{Name: "sales_overview", Description: "Sales overview read"}}

	for _, term := range []string{"runway", "rent", "attrition", "insurance", "chickens"} {
		if termModelled(term, cards, catalog) {
			t.Errorf("%q is named by nothing in the catalogue; the fold must not reach it", term)
		}
	}
}

// TestTheFoldIsOnlyEverARetry pins the shape that makes the fold safe: the
// literal term is tested first and the fold can only ADD a match. leaderFold
// returns "" for a word that is already the catalogue's own, so no caller can
// accidentally replace a literal match with a folded one.
func TestTheFoldIsOnlyEverARetry(t *testing.T) {
	for _, own := range []string{"revenue", "outstanding", "weight", "park"} {
		if got := leaderFold(own); got != "" {
			t.Errorf("leaderFold(%q) = %q; a word the catalogue already names must not be folded", own, got)
		}
	}
	for _, leaders := range []struct{ word, want string }{
		{"money", "revenue"},
		// wordStem trims the gerund, and the card's own `outstanding_rupees`
		// folds onto that same stem — which is the point: both sides run
		// through coverageStem, so they meet wherever they meet.
		{"owes", "outstand"},
	} {
		if got := leaderFold(leaders.word); got != leaders.want {
			t.Errorf("leaderFold(%q) = %q, want %q", leaders.word, got, leaders.want)
		}
	}
}

// TestAKeyCanNeverNameTheSeriesItIsABarOf closes what round 6's comment
// claimed and its code did not do: a label that IS the row's own scope "names
// nothing and is discarded here". When every row agrees on that label — one
// category read twice — the common-label test cannot see it, and the series was
// still named after its own bar.
func TestAKeyCanNeverNameTheSeriesItIsABarOf(t *testing.T) {
	// Pinned at seriesFromFacts, which is where the naming decision is made.
	// buildChart happens to reject this particular shape on its x axis today,
	// so a test written at the chart would pass for a reason that has nothing
	// to do with the defect and would stop covering it the day the x-axis rule
	// moves.
	series, labels := seriesFromFacts(domain.ToolResult{
		Surface: "Mesha operational data",
		Facts: []domain.Fact{
			{TenantID: "t1", Label: "Boer", Value: "30.25", Scope: "Boer"},
			{TenantID: "t1", Label: "Boer", Value: "31.49", Scope: "Boer"},
		},
	})
	if series == nil {
		t.Fatal("two numeric scoped facts must make a series")
	}
	for _, x := range labels {
		if strings.EqualFold(series.name, x) {
			t.Errorf("series named %q after its own category %q; x axis is %v", series.name, x, labels)
		}
	}
	if series.name != "Mesha operational data" {
		t.Errorf("with no measure name to use, the series falls back to the surface; got %q", series.name)
	}

	// A measure every row really shares still names the series.
	series, _ = seriesFromFacts(domain.ToolResult{
		Surface: "Mesha operational data",
		Facts: []domain.Fact{
			{TenantID: "t1", Label: "Average weight kg", Value: "30.25", Scope: "Castro 1"},
			{TenantID: "t1", Label: "Average weight kg", Value: "31.49", Scope: "Castro 2"},
		},
	})
	if series == nil || series.name != "Average weight kg" {
		t.Errorf("a shared measure must still name the series; got %v", series)
	}
}

// TestACaveatAboutAThingReadsAsASentence: every fit detail used to be dropped
// into one adverbial slot, which reads correctly for "by breed" and collapses
// for a noun. Measured live on the corrected workforce answer: "I could not
// produce it the backup you asked about (the read that ran does not report it)
// as asked."
func TestACaveatAboutAThingReadsAsASentence(t *testing.T) {
	note := fitNote([]FitIssue{{Kind: "measure", Detail: "the backup you asked about"}})
	if strings.Contains(note, "produce it the backup") {
		t.Errorf("a noun detail was rendered in the adverbial slot: %q", note)
	}
	if !strings.Contains(note, "I could not reach the backup you asked about") {
		t.Errorf("a noun detail must get its own clause: %q", note)
	}

	// An adverbial detail still reads the way it always did.
	note = fitNote([]FitIssue{{Kind: "dimension", Detail: "by breed"}})
	if !strings.Contains(note, "I could not produce it by breed as asked") {
		t.Errorf("an adverbial detail must keep its slot: %q", note)
	}

	// Both at once keep their own clauses rather than being run together.
	note = fitNote([]FitIssue{
		{Kind: "dimension", Detail: "by breed"},
		{Kind: "window", Detail: "the period you asked about"},
	})
	if !strings.Contains(note, "I could not produce it by breed as asked") ||
		!strings.Contains(note, "I could not reach the period you asked about") {
		t.Errorf("mixed details must each read as a sentence: %q", note)
	}
}
