package app

import (
	"context"
	"regexp"
	"strings"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
)

var numberRe = regexp.MustCompile(`\d[\d,]*\.?\d*`)

// truncationMarkerRe matches renderFacts' row-cap marker.
var truncationMarkerRe = regexp.MustCompile(`… \d+ more not shown`)

// reviewer runs the runtime review pass before returning: groundedness (every
// number in the answer traces to a tool fact), scope/safety (no raw dump, no
// cross-tenant, no write, no chain-of-thought in the body), and completeness
// (all sub-questions answered). On failure the orchestrator self-corrects once
// or downgrades honestly — it NEVER emits an unverified number.
type reviewer struct {
	critic ports.Reviewer // optional Gemini critic, gated by MESHA_AI_REVIEW
}

// review returns the verdict for a drafted answer against its evidence, with
// every result's numbers pooled. Callers that know which result each rendered
// block came from use reviewSections instead — see its comment for why the
// pooled form is the weaker check.
func (rv reviewer) review(ctx context.Context, body string, results []domain.ToolResult, expectedSubs int) domain.ReviewVerdict {
	return rv.reviewSections(ctx, body, []answerSection{{text: body}}, results, expectedSubs)
}

// reviewSections grounds each rendered block against the ONE result it was
// rendered from, and only falls back to the pooled set for a section with no
// result attached (a lead sentence, a fit note, a degraded re-render).
//
// Pooling every result's numbers was the hole: a step that read nothing could
// carry a label ("Delayed tasks by category") while the figure beside it came
// from another step entirely, and the answer still passed review because the
// number existed SOMEWHERE in the evidence. A number must be grounded by the
// evidence it is presented as coming from.
func (rv reviewer) reviewSections(ctx context.Context, body string, sections []answerSection, results []domain.ToolResult, expectedSubs int) domain.ReviewVerdict {
	verdict := domain.ReviewVerdict{Grounded: true, ScopeSafe: true, Complete: true}

	pooled := groundedNumbers(results...)
	for _, sec := range sections {
		want := pooled
		if len(sec.result.Facts) > 0 || sec.result.Route != "" {
			want = groundedNumbers(sec.result)
		}
		for _, n := range numberRe.FindAllString(truncationMarkerRe.ReplaceAllString(sec.text, ""), -1) {
			nn := normalizeNum(n)
			if len(nn) <= 1 {
				continue // ignore trivial single digits (list bullets etc.)
			}
			if !want[nn] {
				verdict.Grounded = false
				verdict.FailReasons = append(verdict.FailReasons,
					"ungrounded number: "+n+" (not in the evidence this line reports)")
			}
		}
	}

	low := strings.ToLower(body)
	for _, banned := range []string{"chain of thought", "step trace", "system prompt", "select ", "tenant_id ="} {
		if strings.Contains(low, banned) {
			verdict.ScopeSafe = false
			verdict.FailReasons = append(verdict.FailReasons, "leaked internal token: "+banned)
		}
	}

	// Completeness counts the results that ANSWERED A SUB-QUESTION. A synthetic
	// result the orchestrator appended to annotate the answer (the "Window: …"
	// line) answers none of them, and counting it let a 2-sub plan with one
	// empty read report Complete -- which also skipped the failed-read retry
	// the orchestrator runs on an incomplete verdict.
	nonEmpty := 0
	for _, r := range results {
		if r.Synthetic {
			continue
		}
		if r.Err == nil && (len(r.Facts) > 0 || strings.TrimSpace(r.Summary) != "") {
			nonEmpty++
		}
	}
	if expectedSubs > 0 && nonEmpty < expectedSubs {
		verdict.Complete = false
		verdict.FailReasons = append(verdict.FailReasons, "incomplete: some sub-questions unanswered")
	}

	if rv.critic != nil {
		var facts []domain.Fact
		for _, r := range results {
			facts = append(facts, r.Facts...)
		}
		if ok, reason, err := rv.critic.Critique(ctx, body, facts); err == nil && !ok {
			verdict.Grounded = false
			verdict.FailReasons = append(verdict.FailReasons, "critic: "+reason)
		}
	}
	return verdict
}

// groundedNumbers is the set of numbers the given results can ground.
func groundedNumbers(results ...domain.ToolResult) map[string]bool {
	grounded := map[string]bool{}
	for _, r := range results {
		for _, f := range r.Facts {
			for _, n := range numberRe.FindAllString(f.Value, -1) {
				grounded[normalizeNum(n)] = true
			}
			for _, n := range numberRe.FindAllString(f.Label, -1) {
				grounded[normalizeNum(n)] = true
			}
			// Fact.Scope is a real dimension value copied verbatim from a tool row
			// (e.g. the operator display_name "Fixture Staff 012", a park label, a
			// filter value). It is the SAME grounded evidence as Value/Label, and the
			// composer prints it (as the per-row scope), so any digits inside it
			// (operator codes, numbered labels) must ground the answer — otherwise a
			// legitimately grounded per-operator breakdown is falsely flagged as an
			// ungrounded number and downgraded to the generic "couldn't verify" reply.
			for _, n := range numberRe.FindAllString(f.Scope, -1) {
				grounded[normalizeNum(n)] = true
			}
		}
		// NOTE: r.Summary is tool/model prose, NOT hard evidence — its numbers do
		// NOT ground the answer. Only Fact values/labels are trusted. This is what
		// lets the review pass catch a hallucinated figure smuggled into a summary.
	}
	return grounded
}

func normalizeNum(s string) string {
	// Strip grouping commas and any trailing sentence period. The number regex
	// greedily captures a trailing "." (e.g. a figure that ends a sentence:
	// "… 1200."), which would otherwise fail to match the same figure stored
	// without the period in the grounding set. A trailing dot is never part of a
	// number, so trimming it keeps genuine decimals ("12.5") intact while making
	// grounded prose match.
	return strings.TrimRight(strings.ReplaceAll(s, ",", ""), ".")
}
