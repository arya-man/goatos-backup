// Package e2ehrms hosts the HRMS "kernel story" end-to-end test harness: full flows that seed a
// clean-slate roster scenario against an ephemeral Postgres instance per story, drive the real HR
// roster / RBAC coverage engine (workforce positions -> leave/absence -> effective-backup coverage
// -> temporary execution capability -> vaccination-ownership resolution / escalation) through the
// same application services and repositories the rest of the backend uses, and render the outcome
// into a single, dependency-free HTML report for engineering/CEO visibility.
//
// This mirrors backend/tests/e2e (the vaccination kernel-story harness) for a separate HRMS
// section. See harness_test.go for how each story is run, and run.sh / the package doc comment
// there for the exact command.
package e2ehrms

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AssertionResult is one pass/fail check inside a story step.
type AssertionResult struct {
	Description string
	Pass        bool
	Detail      string
}

// StepResult is one narrated step of a story (a setup action, a roster transition, or a read-model
// check), carrying the assertions made during that step.
type StepResult struct {
	Name       string
	Narrative  string
	Assertions []AssertionResult
}

// StoryResult is one full HRMS kernel story (one Go test function), with a plain-English narrative
// and its ordered steps.
type StoryResult struct {
	ID        string
	Title     string
	Narrative string
	Steps     []StepResult
	Pass      bool
}

// Report collects every kernel story recorded in this package (each test runs against its own
// ephemeral Postgres container, but they all render into one report).
type Report struct {
	mu          sync.Mutex
	GeneratedAt time.Time
	Stories     []StoryResult
}

var globalReport = &Report{GeneratedAt: time.Now().UTC()}

// Add appends a finished story's result to the shared report. Safe for concurrent use, though the
// stories currently run sequentially (each owns a Docker container).
func (r *Report) Add(s StoryResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Stories = append(r.Stories, s)
}

func (r *Report) snapshot() reportView {
	r.mu.Lock()
	defer r.mu.Unlock()
	view := reportView{
		GeneratedAt: r.GeneratedAt.Format("2006-01-02 15:04:05 MST"),
		Stories:     append([]StoryResult(nil), r.Stories...),
	}
	for _, s := range view.Stories {
		if s.Pass {
			view.PassCount++
		} else {
			view.FailCount++
		}
		for _, step := range s.Steps {
			for _, a := range step.Assertions {
				view.AssertionCount++
				if !a.Pass {
					view.AssertionFailCount++
				}
			}
		}
	}
	return view
}

// WriteHTML renders every story collected so far into a single, self-contained HTML file at path
// (parent directories are created as needed). The file has no external CSS/JS/CDN references, so it
// opens standalone from disk and is safe to host as-is on GitHub Pages.
func (r *Report) WriteHTML(path string) error {
	view := r.snapshot()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("hrms e2e report: mkdir: %w", err)
	}
	var buf bytes.Buffer
	if err := reportTemplate.Execute(&buf, view); err != nil {
		return fmt.Errorf("hrms e2e report: render: %w", err)
	}
	if err := os.WriteFile(path, stripTrailingWhitespace(buf.Bytes()), 0o644); err != nil {
		return fmt.Errorf("hrms e2e report: write: %w", err)
	}
	return nil
}

// stripTrailingWhitespace trims trailing spaces/tabs from every line of the rendered report. The HTML
// template below indents nested {{range}}/{{if}} blocks for source readability, which otherwise leaves
// whitespace-only lines in the rendered output wherever one of those actions sits alone on its own
// line. A committed report with such lines fails `git diff --check`; stripping trailing whitespace here
// keeps the template readable while keeping the rendered file clean.
func stripTrailingWhitespace(b []byte) []byte {
	lines := bytes.Split(b, []byte("\n"))
	for i, line := range lines {
		lines[i] = bytes.TrimRight(line, " \t")
	}
	return bytes.Join(lines, []byte("\n"))
}

type reportView struct {
	GeneratedAt        string
	Stories            []StoryResult
	PassCount          int
	FailCount          int
	AssertionCount     int
	AssertionFailCount int
}

var reportTemplate = template.Must(template.New("report").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>HRMS Kernel Story Report</title>
<style>
  :root { color-scheme: light dark; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
    max-width: 980px;
    margin: 0 auto;
    padding: 32px 20px 80px;
    line-height: 1.5;
    color: #1a1f24;
    background: #f5f7f9;
  }
  @media (prefers-color-scheme: dark) {
    body { color: #e6e9ec; background: #14181c; }
  }
  h1 { font-size: 1.6rem; margin-bottom: 4px; }
  .subtitle { color: #5b6770; margin-top: 0; }
  @media (prefers-color-scheme: dark) { .subtitle { color: #9aa7b0; } }
  .summary {
    display: flex; gap: 16px; flex-wrap: wrap; margin: 20px 0 32px;
  }
  .summary .tile {
    flex: 1 1 140px;
    border-radius: 10px;
    padding: 14px 16px;
    background: #ffffff;
    border: 1px solid #e1e6ea;
  }
  @media (prefers-color-scheme: dark) {
    .summary .tile { background: #1d2328; border-color: #2b333a; }
  }
  .summary .tile .n { font-size: 1.6rem; font-weight: 700; display: block; }
  .summary .tile.pass .n { color: #1f8a4c; }
  .summary .tile.fail .n { color: #c62828; }
  section.story {
    background: #ffffff;
    border: 1px solid #e1e6ea;
    border-radius: 12px;
    padding: 20px 24px;
    margin-bottom: 28px;
  }
  @media (prefers-color-scheme: dark) {
    section.story { background: #1d2328; border-color: #2b333a; }
  }
  section.story.fail { border-left: 5px solid #c62828; }
  section.story.pass { border-left: 5px solid #1f8a4c; }
  .story h2 { margin: 0 0 6px; font-size: 1.25rem; display: flex; align-items: center; gap: 10px; }
  .badge {
    font-size: 0.7rem;
    font-weight: 700;
    letter-spacing: 0.04em;
    padding: 3px 9px;
    border-radius: 999px;
    color: #fff;
  }
  .badge.pass { background: #1f8a4c; }
  .badge.fail { background: #c62828; }
  .story .narrative { color: #444d54; margin-top: 0; }
  @media (prefers-color-scheme: dark) { .story .narrative { color: #b7c0c7; } }
  .step { margin-top: 18px; padding-top: 14px; border-top: 1px dashed #dfe4e8; }
  @media (prefers-color-scheme: dark) { .step { border-top-color: #2b333a; } }
  .step h3 { margin: 0 0 4px; font-size: 1rem; }
  .step .narrative { margin: 0 0 10px; color: #5b6770; font-size: 0.92rem; }
  @media (prefers-color-scheme: dark) { .step .narrative { color: #9aa7b0; } }
  table { width: 100%; border-collapse: collapse; font-size: 0.88rem; }
  th, td { text-align: left; padding: 6px 8px; vertical-align: top; }
  thead th { color: #5b6770; font-weight: 600; border-bottom: 1px solid #e1e6ea; }
  @media (prefers-color-scheme: dark) {
    thead th { color: #9aa7b0; border-bottom-color: #2b333a; }
  }
  tr.ok td:first-child::before { content: "\2713  "; color: #1f8a4c; font-weight: 700; }
  tr.bad td:first-child::before { content: "\2717  "; color: #c62828; font-weight: 700; }
  tr.bad { background: rgba(198,40,40,0.06); }
  td.detail { color: #5b6770; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 0.82rem; }
  @media (prefers-color-scheme: dark) { td.detail { color: #9aa7b0; } }
  footer { color: #8a949c; font-size: 0.8rem; margin-top: 40px; }
</style>
</head>
<body>
  <h1>HRMS Kernel Story Report</h1>
  <p class="subtitle">Generated {{.GeneratedAt}} &middot; backend/tests/e2e-hrms</p>

  <div class="summary">
    <div class="tile"><span class="n">{{len .Stories}}</span>stories</div>
    <div class="tile pass"><span class="n">{{.PassCount}}</span>stories passed</div>
    <div class="tile fail"><span class="n">{{.FailCount}}</span>stories failed</div>
    <div class="tile"><span class="n">{{.AssertionCount}}</span>assertions checked</div>
    <div class="tile {{if gt .AssertionFailCount 0}}fail{{else}}pass{{end}}"><span class="n">{{.AssertionFailCount}}</span>assertions failed</div>
  </div>

  {{range .Stories}}
  <section class="story {{if .Pass}}pass{{else}}fail{{end}}">
    <h2>{{.Title}} <span class="badge {{if .Pass}}pass{{else}}fail{{end}}">{{if .Pass}}PASS{{else}}FAIL{{end}}</span></h2>
    <p class="narrative">{{.Narrative}}</p>
    {{range .Steps}}
    <div class="step">
      <h3>{{.Name}}</h3>
      {{if .Narrative}}<p class="narrative">{{.Narrative}}</p>{{end}}
      {{if .Assertions}}
      <table>
        <thead><tr><th>Assertion</th><th>Result</th><th>Detail</th></tr></thead>
        <tbody>
        {{range .Assertions}}
          <tr class="{{if .Pass}}ok{{else}}bad{{end}}">
            <td>{{.Description}}</td>
            <td>{{if .Pass}}PASS{{else}}FAIL{{end}}</td>
            <td class="detail">{{.Detail}}</td>
          </tr>
        {{end}}
        </tbody>
      </table>
      {{end}}
    </div>
    {{end}}
  </section>
  {{end}}

  <footer>Generated by backend/tests/e2e-hrms &mdash; run via <code>backend/tests/e2e-hrms/run.sh</code> or <code>go test ./tests/e2e-hrms/... -run TestKernelStor -v</code>.</footer>
</body>
</html>
`))
