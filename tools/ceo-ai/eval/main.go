// Command ceo-ai-eval is the answer-quality regression harness for the Mesha
// leadership assistant. It runs a golden question set live through the assistant
// endpoint, scores each answer against an independent Postgres oracle, and emits
// a self-contained HTML report plus a JSON report.
//
// Design goals (see tools/ceo-ai/eval/README.md and docs/ceo-ai/eval.md):
//   - Deterministic scoring: the LLM answer is non-deterministic, but scoring is
//     a pure function of (question, response, oracle). Re-scoring a recorded run
//     always yields the same verdict.
//   - Opt-in only: needs a live assistant (Vertex) + Postgres. It NEVER runs in
//     the default local/PR gate; run-local-ci gates it behind an explicit flag
//     and skips loudly (never a silent pass) when prerequisites are absent.
//   - stdlib-only: the oracle runs through `psql`, so no driver is vendored.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

func main() {
	var (
		goldenDir = flag.String("golden", defaultGolden(), "directory of golden *.json question files")
		validate  = flag.Bool("validate", false, "validate the golden set and exit (no assistant/DB needed)")
		htmlOut   = flag.String("html", "", "path to write the self-contained HTML report")
		jsonOut   = flag.String("json", "", "path to write the JSON report")
		timeoutS  = flag.Int("timeout", 60, "per-request timeout in seconds (assistant and oracle)")
		filter    = flag.String("class", "", "optional: only run questions in this class")
	)
	flag.Parse()

	qs, err := loadGolden(*goldenDir)
	if err != nil {
		fail("golden set: %v", err)
	}
	if *validate {
		fmt.Printf("ceo-ai-eval: golden set OK — %d questions across %d classes\n", len(qs), classCount(qs))
		return
	}

	assistantURL := os.Getenv("MESHA_ASSISTANT_URL")
	dsn := firstEnv("GOATOS_EVAL_DATABASE_URL", "GOATOS_E2E_DATABASE_URL", "DATABASE_URL")
	tenant := os.Getenv("GOATOS_EVAL_TENANT_ID")

	// Loud skip (NOT a pass) when prerequisites are missing, mirroring the
	// e2e-docker-chain posture. Exit 0 so an opt-out CI lane stays green, but the
	// message makes the skip unmistakable. `make ceo-ai-eval` sets STRICT=1 to
	// turn a missing prerequisite into a hard failure.
	missing := prerequisiteGaps(assistantURL, dsn, tenant)
	if len(missing) > 0 {
		msg := "ceo-ai-eval: SKIPPED — missing " + strings.Join(missing, ", ") +
			". This is NOT a pass. Set MESHA_ASSISTANT_URL, GOATOS_EVAL_DATABASE_URL, GOATOS_EVAL_TENANT_ID (and MESHA_EVAL_BEARER) to run live."
		if strictMode() {
			fail("%s", msg)
		}
		fmt.Fprintln(os.Stderr, msg)
		return
	}

	rep := runLive(qs, liveConfig{
		assistantURL: assistantURL,
		bearer:       os.Getenv("MESHA_EVAL_BEARER"),
		dsn:          dsn,
		tenant:       tenant,
		timeout:      time.Duration(*timeoutS) * time.Second,
		classFilter:  *filter,
	})

	if *jsonOut != "" {
		if err := writeJSON(*jsonOut, rep); err != nil {
			fail("write json: %v", err)
		}
		fmt.Printf("ceo-ai-eval: wrote %s\n", *jsonOut)
	}
	if *htmlOut != "" {
		if err := writeHTML(*htmlOut, rep); err != nil {
			fail("write html: %v", err)
		}
		fmt.Printf("ceo-ai-eval: wrote %s\n", *htmlOut)
	}
	printSummary(rep)

	if rep.Metrics.Failed > 0 || rep.Metrics.Errored > 0 {
		os.Exit(1)
	}
}

type liveConfig struct {
	assistantURL string
	bearer       string
	dsn          string
	tenant       string
	timeout      time.Duration
	classFilter  string
}

// minRequestSpacing keeps the harness below the assistant's per-user limiter
// (30 requests / minute = 1 per 2s; orchestrator.go). Firing the golden set
// back-to-back trips the limiter partway through, so every request is spaced by
// at least this interval with headroom.
const minRequestSpacing = 2200 * time.Millisecond

// maxRateLimitRetries bounds the extra waits when the limiter still trips (e.g.
// a shared tenant/user or clock skew); each retry backs off before re-asking.
const maxRateLimitRetries = 3

func runLive(qs []GoldenQuestion, cfg liveConfig) Report {
	ctx := context.Background()
	client := AssistantClient{URL: cfg.assistantURL, Bearer: cfg.bearer, Timeout: cfg.timeout}
	oracle := OracleRunner{DSN: cfg.dsn, TenantID: cfg.tenant, Timeout: cfg.timeout}

	var results []Result
	first := true
	for _, q := range qs {
		if cfg.classFilter != "" && q.Class != cfg.classFilter {
			continue
		}
		if !first {
			time.Sleep(minRequestSpacing)
		}
		first = false

		resp, latency, reqErr := askPaced(ctx, client, q.Question)
		var ores OracleResult
		if reqErr == nil {
			ores = oracle.resolve(ctx, q)
		}
		results = append(results, resultFor(q, resp, ores, latency, reqErr))
	}
	return Report{
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339),
		AssistantURL: cfg.assistantURL,
		TenantID:     cfg.tenant,
		CommitSHA:    commitSHA(),
		Metrics:      computeMetrics(results),
		Results:      results,
	}
}

// askPaced calls the assistant, backing off and retrying when the per-user
// limiter trips (a 200 refusal envelope surfaced as errRateLimited) so a rate
// limit is never scored as a wrong answer. Any other error/response returns
// immediately. Latency is the last attempt's measured wall-clock time.
func askPaced(ctx context.Context, client AssistantClient, question string) (*AssistantResponse, int64, error) {
	var latency int64
	for attempt := 0; ; attempt++ {
		resp, l, err := client.ask(ctx, question)
		latency = l
		if err == errRateLimited && attempt < maxRateLimitRetries {
			time.Sleep(minRequestSpacing * time.Duration(attempt+2))
			continue
		}
		return resp, latency, err
	}
}

func printSummary(rep Report) {
	m := rep.Metrics
	fmt.Println("──────── ceo-ai-eval summary ────────")
	fmt.Printf("  questions:          %d (pass %d, fail %d, errored %d)\n", m.Total, m.Passed, m.Failed, m.Errored)
	fmt.Printf("  pass rate:          %.0f%%\n", m.PassRate*100)
	fmt.Printf("  grounding rate:     %.0f%% (%d/%d)\n", m.GroundingRate*100, m.GroundingPassed, m.GroundingApplicable)
	fmt.Printf("  refusal accuracy:   %.0f%% (%d/%d)\n", m.RefusalAccuracy*100, m.RefusalCorrect, m.RefusalApplicable)
	fmt.Printf("  tool selection:     %.0f%% (%d/%d)\n", m.ToolSelectionAccuracy*100, m.ToolSelPassed, m.ToolSelApplicable)
	fmt.Printf("  latency p50/p90/p95: %d/%d/%d ms\n", m.LatencyP50MS, m.LatencyP90MS, m.LatencyP95MS)
	for _, r := range rep.Results {
		if !r.Passed {
			fmt.Printf("  FAIL %-28s %s\n", r.Question.ID, firstFailure(r))
		}
	}
}

func firstFailure(r Result) string {
	if r.ErrorText != "" {
		return r.ErrorText
	}
	for _, c := range r.Checks {
		if !c.Passed {
			return c.Name + ": " + c.Detail
		}
	}
	return ""
}

func prerequisiteGaps(url, dsn, tenant string) []string {
	var m []string
	if url == "" {
		m = append(m, "MESHA_ASSISTANT_URL")
	}
	if dsn == "" {
		m = append(m, "GOATOS_EVAL_DATABASE_URL")
	}
	if tenant == "" {
		m = append(m, "GOATOS_EVAL_TENANT_ID")
	}
	if _, err := exec.LookPath("psql"); err != nil {
		m = append(m, "psql (libpq client)")
	}
	return m
}

func strictMode() bool {
	switch os.Getenv("CEO_AI_EVAL_STRICT") {
	case "1", "true", "TRUE", "True":
		return true
	}
	return false
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

func defaultGolden() string {
	// Relative to the module root by default so `go run .` and `make` both work.
	if _, err := os.Stat("golden"); err == nil {
		return "golden"
	}
	return "tools/ceo-ai/eval/golden"
}

func classCount(qs []GoldenQuestion) int {
	seen := map[string]bool{}
	for _, q := range qs {
		seen[q.Class] = true
	}
	return len(seen)
}

func commitSHA() string {
	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "ceo-ai-eval: "+format+"\n", a...)
	os.Exit(2)
}
