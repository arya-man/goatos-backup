package main

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"strings"
)

// writeJSON emits the machine-readable report. Deterministic given the results.
func writeJSON(path string, rep Report) error {
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// writeHTML emits a self-contained report following the repo E2E report visual
// contract: title/subtitle, summary tiles, pass/fail/pending badges, and a
// per-question section explaining what was tested, the oracle, and the checks.
func writeHTML(path string, rep Report) error {
	var b strings.Builder
	m := rep.Metrics
	b.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8">`)
	b.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">`)
	b.WriteString(`<title>Mesha CEO Assistant — Answer-Quality Eval</title>`)
	b.WriteString("<style>" + reportCSS + "</style></head><body>")

	b.WriteString(`<header><h1>Mesha CEO Assistant — Answer-Quality Eval</h1>`)
	b.WriteString(`<p class="sub">Golden-question regression: every answer is scored against an independent Postgres oracle over canonical tables. Numbers must match ground truth; refusals must not fabricate; routing tier and aggregate-first shape are asserted.</p>`)
	b.WriteString(`<p class="meta">Generated ` + esc(rep.GeneratedAt) + ` · commit ` + esc(short(rep.CommitSHA)) + ` · assistant ` + esc(rep.AssistantURL) + ` · tenant ` + esc(short(rep.TenantID)) + `</p></header>`)

	overall := "pass"
	if m.Failed > 0 || m.Errored > 0 {
		overall = "fail"
	}
	b.WriteString(`<section class="tiles">`)
	tile(&b, "Overall", strings.ToUpper(overall), overall)
	tile(&b, "Pass rate", pct(m.PassRate)+fmt.Sprintf(" (%d/%d)", m.Passed, m.Total), tone(m.Failed == 0 && m.Errored == 0))
	tile(&b, "Grounding rate", pct(m.GroundingRate)+fmt.Sprintf(" (%d/%d)", m.GroundingPassed, m.GroundingApplicable), tone(m.GroundingPassed == m.GroundingApplicable))
	tile(&b, "Refusal accuracy", pct(m.RefusalAccuracy)+fmt.Sprintf(" (%d/%d)", m.RefusalCorrect, m.RefusalApplicable), tone(m.RefusalCorrect == m.RefusalApplicable))
	tile(&b, "Tool selection", pct(m.ToolSelectionAccuracy)+fmt.Sprintf(" (%d/%d)", m.ToolSelPassed, m.ToolSelApplicable), tone(m.ToolSelPassed == m.ToolSelApplicable))
	tile(&b, "Latency p50/p90/p95", fmt.Sprintf("%d / %d / %d ms", m.LatencyP50MS, m.LatencyP90MS, m.LatencyP95MS), "neutral")
	if m.Errored > 0 {
		tile(&b, "Errored", fmt.Sprintf("%d", m.Errored), "fail")
	}
	b.WriteString(`</section>`)

	b.WriteString(`<section class="results"><h2>Questions</h2>`)
	for _, r := range rep.Results {
		writeResultCard(&b, r)
	}
	b.WriteString(`</section>`)
	b.WriteString(`<footer><p>Read-only leadership eval. Step traces and chain-of-thought are internal and are never shown in the leadership chat answer.</p></footer>`)
	b.WriteString("</body></html>")
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func writeResultCard(b *strings.Builder, r Result) {
	badge := "pass"
	label := "PASS"
	switch {
	case r.Errored:
		badge, label = "err", "ERROR"
	case !r.Passed:
		badge, label = "fail", "FAIL"
	}
	q := r.Question
	fmt.Fprintf(b, `<article class="card %s"><div class="chead"><span class="badge %s">%s</span>`, badge, badge, label)
	fmt.Fprintf(b, `<code class="qid">%s</code><span class="cls">%s</span><span class="lat">%d ms</span></div>`,
		esc(q.ID), esc(q.Class), r.LatencyMS)
	fmt.Fprintf(b, `<p class="q">%s</p>`, esc(q.Question))
	if q.Notes != "" {
		fmt.Fprintf(b, `<p class="note">%s</p>`, esc(q.Notes))
	}
	fmt.Fprintf(b, `<p class="exp">Asserts: %s</p>`, esc(expectSummary(q.Expect)))
	if q.Oracle != nil {
		fmt.Fprintf(b, `<details><summary>Oracle (%s)</summary><pre>%s</pre>`, esc(q.Oracle.Kind), esc(strings.TrimSpace(q.Oracle.SQL)))
		if r.Oracle.Applicable {
			switch {
			case r.Oracle.Err != "":
				fmt.Fprintf(b, `<p class="oerr">oracle error: %s</p>`, esc(r.Oracle.Err))
			case q.Oracle.Kind == oracleRows:
				fmt.Fprintf(b, `<p class="oval">ground truth: %s</p>`, esc(fmt.Sprintf("%v", r.Oracle.Rows)))
			default:
				fmt.Fprintf(b, `<p class="oval">ground truth: %d</p>`, r.Oracle.Scalar)
			}
		}
		b.WriteString(`</details>`)
	}
	if r.Response != nil {
		fmt.Fprintf(b, `<details><summary>Answer (mode=%s, source=%s)</summary><pre>%s</pre></details>`,
			esc(r.Response.Mode), esc(r.Response.Source), esc(r.Response.Answer))
	}
	if r.ErrorText != "" {
		fmt.Fprintf(b, `<p class="oerr">%s</p>`, esc(r.ErrorText))
	}
	b.WriteString(`<ul class="checks">`)
	for _, c := range r.Checks {
		cls, mark := "ok", "✓"
		if !c.Passed {
			cls, mark = "no", "✗"
		}
		fmt.Fprintf(b, `<li class="%s"><span class="cm">%s</span> <b>%s</b> — %s</li>`, cls, mark, esc(c.Name), esc(c.Detail))
	}
	b.WriteString(`</ul></article>`)
}

func expectSummary(e Expect) string {
	var parts []string
	if e.Grounded {
		parts = append(parts, "grounded-number")
	}
	if e.SpeciesSplit {
		parts = append(parts, "species-split")
	}
	if e.Refusal {
		parts = append(parts, "refusal")
	}
	if e.AggregateFirst {
		parts = append(parts, "aggregate-first")
	}
	if e.InjectionSafe {
		if e.InjectionForbidLeak {
			parts = append(parts, "injection-safe(forbid-leak)")
		} else {
			parts = append(parts, "injection-safe(scoped)")
		}
	}
	if e.IST {
		parts = append(parts, "IST")
	}
	if len(e.TiersAnyOf) > 0 {
		parts = append(parts, "tier="+strings.Join(e.TiersAnyOf, "|"))
	}
	if len(parts) == 0 {
		return "(none)"
	}
	return strings.Join(parts, ", ")
}

func tile(b *strings.Builder, label, value, tone string) {
	fmt.Fprintf(b, `<div class="tile %s"><div class="tl">%s</div><div class="tv">%s</div></div>`, tone, esc(label), esc(value))
}

func tone(ok bool) string {
	if ok {
		return "pass"
	}
	return "fail"
}

func pct(f float64) string { return fmt.Sprintf("%.0f%%", f*100) }
func esc(s string) string  { return html.EscapeString(s) }
func short(s string) string {
	if s == "" {
		return "—"
	}
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

const reportCSS = `
:root{color-scheme:light dark}
*{box-sizing:border-box}
body{margin:0;font:15px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;background:#0f1720;color:#e6edf3}
header{padding:28px 24px 8px}
h1{margin:0 0 4px;font-size:22px}
.sub{margin:0 0 6px;max-width:80ch;color:#9fb0c0}
.meta{margin:0;color:#7d8fa1;font-size:12px}
.tiles{display:flex;flex-wrap:wrap;gap:12px;padding:16px 24px}
.tile{flex:1 1 160px;min-width:150px;background:#16212e;border:1px solid #24313f;border-radius:10px;padding:12px 14px}
.tile .tl{font-size:12px;color:#9fb0c0;text-transform:uppercase;letter-spacing:.04em}
.tile .tv{font-size:19px;font-weight:600;margin-top:4px}
.tile.pass .tv{color:#3fb950}.tile.fail .tv{color:#f85149}.tile.neutral .tv{color:#e6edf3}
.results{padding:8px 24px 24px}
h2{font-size:16px;margin:12px 0}
.card{background:#131c26;border:1px solid #24313f;border-left-width:4px;border-radius:8px;padding:12px 14px;margin:10px 0}
.card.pass{border-left-color:#3fb950}.card.fail{border-left-color:#f85149}.card.err{border-left-color:#d29922}
.chead{display:flex;align-items:center;gap:10px;flex-wrap:wrap}
.badge{font-size:11px;font-weight:700;padding:2px 8px;border-radius:999px}
.badge.pass{background:#12331d;color:#3fb950}.badge.fail{background:#3a1514;color:#f85149}.badge.err{background:#3a2c0f;color:#d29922}
.qid{color:#79c0ff;font-size:12px}.cls{color:#9fb0c0;font-size:12px}.lat{margin-left:auto;color:#7d8fa1;font-size:12px}
.q{font-weight:600;margin:8px 0 4px}.note{color:#9fb0c0;font-size:13px;margin:0 0 4px}
.exp{color:#a5d6ff;font-size:12px;margin:2px 0}
details{margin:6px 0}summary{cursor:pointer;color:#9fb0c0;font-size:13px}
pre{background:#0b1119;border:1px solid #24313f;border-radius:6px;padding:8px;overflow-x:auto;font-size:12px;white-space:pre-wrap;word-break:break-word}
.oval{color:#3fb950;font-size:12px}.oerr{color:#f85149;font-size:12px}
.checks{list-style:none;padding:0;margin:8px 0 0}
.checks li{font-size:12px;padding:2px 0}
.checks li.ok .cm{color:#3fb950}.checks li.no{color:#f85149}.checks li.no .cm{color:#f85149}
footer{padding:16px 24px;color:#7d8fa1;font-size:12px;border-top:1px solid #24313f}
`
