package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
)

const probeTenant = "00000000-0000-4000-8000-0000000000aa"

// A fact with a label and NO value is a heading, not a figure. Rendering it
// produced "Total births in last 7 days: ." on the live judge run — a number
// the reader supplies themselves. It is dropped, and a result left with nothing
// reads as an empty read.
func TestBlankValuedFactIsNeverRenderedAsAFigure(t *testing.T) {
	r := domain.ToolResult{
		Route: domain.RouteSQL, Surface: "counts",
		Facts: []domain.Fact{
			{TenantID: probeTenant, Label: "Total births in last 7 days", Value: "  "},
			{TenantID: probeTenant, Label: "Total deaths in last 7 days", Value: "3"},
		},
	}
	body, _, _, err := composer{}.composeFor(domain.Actor{TenantID: probeTenant}, []domain.ToolResult{r})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if strings.Contains(body, "births in last 7 days") {
		t.Errorf("blank-valued fact rendered as a figure: %q", body)
	}
	if !strings.Contains(body, "3") {
		t.Errorf("grounded fact dropped: %q", body)
	}

	onlyBlank := domain.ToolResult{Route: domain.RouteSQL, Surface: "counts",
		Facts: []domain.Fact{{TenantID: probeTenant, Label: "Total births in last 7 days", Value: ""}}}
	body, _, _, err = composer{}.composeFor(domain.Actor{TenantID: probeTenant}, []domain.ToolResult{onlyBlank})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if !strings.Contains(strings.ToLower(body), "no records found") {
		t.Errorf("a result with only blank values must read as an empty read, got %q", body)
	}
}

// A failed read must never render as the wiring token ("sql: could not be
// retrieved."): the reader is told, in words, that the data could not be read.
func TestFailedReadIsNamedInWordsNeverAsARouteToken(t *testing.T) {
	failed := domain.ToolResult{Route: domain.RouteSQL, ToolName: "sql_fallback", Err: errors.New("pq: boom")}
	for name, body := range map[string]string{
		"compose":         mustCompose(t, failed),
		"strictRecompose": (&Assistant{}).strictRecompose([]domain.ToolResult{failed}),
	} {
		if strings.Contains(body, "could not be retrieved") || strings.HasPrefix(strings.TrimSpace(body), "sql:") {
			t.Errorf("%s leaked the route token: %q", name, body)
		}
		if strings.Contains(body, "boom") {
			t.Errorf("%s leaked the internal error: %q", name, body)
		}
		if !strings.Contains(strings.ToLower(body), "couldn't read") {
			t.Errorf("%s does not say the read failed: %q", name, body)
		}
	}
}

func mustCompose(t *testing.T, results ...domain.ToolResult) string {
	t.Helper()
	body, _, _, err := composer{}.composeFor(domain.Actor{TenantID: probeTenant}, results)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	return body
}

// A read that FAILED is not a read that found nothing: when every read failed
// there is no evidence at all, so the answer says so rather than composing a
// "partial" around failure lines with the empty fit issue quietly dropped.
func TestAllReadsFailedIsDistinctFromNothingFound(t *testing.T) {
	failed := []domain.ToolResult{
		{Route: domain.RouteSQL, Err: errors.New("x")},
		{Route: domain.RouteAPI, Err: errors.New("y")},
	}
	if !allReadsFailed(failed) {
		t.Error("every read errored but allReadsFailed said no")
	}
	empty := []domain.ToolResult{{Route: domain.RouteSQL}, {Route: domain.RouteAPI, Err: errors.New("y")}}
	if allReadsFailed(empty) {
		t.Error("a zero-row read is not a failed read")
	}
	if allReadsFailed(nil) {
		t.Error("no reads at all is not all-reads-failed")
	}
}
