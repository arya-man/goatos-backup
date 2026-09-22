package app

import "testing"

// The model judge is a whole Vertex round trip on the critical path of a
// leader's question. It earns that cost only when its verdict can still change
// what happens next -- which it cannot once the deterministic checks have
// already found a misfit, because the one re-plan is triggered either way.
func TestTheJudgeIsNotAskedWhenTheOutcomeCannotChange(t *testing.T) {
	shaped := RequestedShape{Dimensions: []string{"park"}}

	if !judgeWorthAsking(shaped, nil, false) {
		t.Error("a shaped question whose reads looked fine is exactly the case the judge exists for")
	}
	if judgeWorthAsking(shaped, []FitIssue{{Kind: "dimension"}}, false) {
		t.Error("the judge was asked although a re-plan is already triggered")
	}
	if judgeWorthAsking(shaped, nil, true) {
		t.Error("the streaming path opted out of the model critic and was judged anyway")
	}
}

// A question naming no breakdown, no unit and no period asserts no shape the
// evidence could fail to have, so there is nothing to hold the facts against.
func TestAQuestionAssertingNoShapeIsNotJudged(t *testing.T) {
	if judgeWorthAsking(RequestedShape{}, nil, false) {
		t.Error("a plain lookup paid for a judge call with nothing to judge")
	}
	for _, req := range []RequestedShape{
		{Dimensions: []string{"pen"}},
		{Units: []string{"kg"}},
		{Window: mustWindow(t)},
	} {
		if !judgeWorthAsking(req, nil, false) {
			t.Errorf("a question asserting %+v was left unjudged", req)
		}
	}
}

func mustWindow(t *testing.T) Window {
	t.Helper()
	w, ok := ResolveWindow("last month", trNow, nil)
	if !ok {
		t.Fatal("last month did not resolve to a period")
	}
	return w
}
