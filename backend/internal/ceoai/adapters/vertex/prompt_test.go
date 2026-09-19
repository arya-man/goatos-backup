package vertex

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
	"github.com/vgoats/goatos/backend/internal/ceoai/reporting"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

func TestParsePlanFencedJSON(t *testing.T) {
	raw := "```json\n{\"refusal\":\"\",\"sub_questions\":[{\"id\":\"0\",\"text\":\"t\",\"intent_class\":\"total_animal_census\",\"route\":\"cube\",\"tool\":\"active_animals\",\"params\":{\"park_label\":\"Castro 1\"}}]}\n```"
	pl, err := parsePlan(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(pl.SubQuestions) != 1 {
		t.Fatalf("expected 1 sub-question, got %d", len(pl.SubQuestions))
	}
	s := pl.SubQuestions[0]
	if s.ToolName != "active_animals" || s.Route != domain.RouteCube {
		t.Fatalf("bad parse: %+v", s)
	}
	if s.Params["park_label"] != "Castro 1" {
		t.Fatalf("param not parsed: %+v", s.Params)
	}
}

func TestParsePlanRefusal(t *testing.T) {
	pl, err := parsePlan(`{"refusal":"read only","sub_questions":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	if pl.Refusal == "" {
		t.Fatal("expected refusal preserved")
	}
}

func TestExtractJSONStripsProse(t *testing.T) {
	if got := extractJSON("Here you go: {\"a\":1} thanks"); got != `{"a":1}` {
		t.Fatalf("extractJSON=%q", got)
	}
}

// --- plan v3 D1.1: card block, golden snapshot, usageMetadata, repair ---

var updateGolden = flag.Bool("update", false, "rewrite testdata/prompt.golden from the current prompt")

func goldenQuestion() domain.Question {
	return domain.Question{
		Actor: domain.Actor{TenantID: "11111111-1111-1111-1111-111111111111", Role: "ceo_internal"},
		Text:  "How many deaths last month by park?",
		AsOf:  time.Date(2026, 7, 22, 0, 0, 0, 0, biztime.DefaultLocation()),
	}
}

func goldenCatalog() []ports.ToolSpec {
	return []ports.ToolSpec{
		{Name: "active_animals", Route: domain.RouteCube, Description: "Active animals", Params: []string{"park_label", "species"}},
		{Name: "vaccination_overdue", Route: domain.RouteCube, Description: "Vaccination overdue", Params: []string{"shed_label"}},
	}
}

// TestPlanPromptGolden pins the rendered planner prompt (system instruction +
// user prompt with the schema card block) so a card, rule or wording change is
// a visible diff. Regenerate deliberately with:
//
//	go test ./internal/ceoai/adapters/vertex -run TestPlanPromptGolden -update
func TestPlanPromptGolden(t *testing.T) {
	got := "=== SYSTEM ===\n" + systemPlannerInstruction + "\n=== USER ===\n" + buildPlanPrompt(goldenQuestion(), nil, goldenCatalog()) + "\n"
	path := filepath.Join("testdata", "prompt.golden")
	if *updateGolden {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if string(want) != got {
		t.Fatalf("planner prompt drifted from testdata/prompt.golden; review the diff and re-run with -update if intended.\n--- got ---\n%s", got)
	}
}

func TestPlanPromptCarriesEveryCardAndWindow(t *testing.T) {
	p := buildPlanPrompt(goldenQuestion(), nil, goldenCatalog())
	for _, c := range reporting.Cards() {
		if !strings.Contains(p, "ceo_ai."+c.Name+":") {
			t.Errorf("prompt is missing the card for ceo_ai.%s", c.Name)
		}
	}
	// The old single-view hint is gone; the card block replaces it.
	if strings.Contains(p, "Useful columns: tenant_id, park_id") {
		t.Fatal("legacy single-view hint still present")
	}
	// Tenant literal and LIMIT rule are unchanged.
	if !strings.Contains(p, `WHERE tenant_id = "11111111-1111-1111-1111-111111111111"`) || !strings.Contains(p, "LIMIT <= 100") {
		t.Fatal("tenant literal / LIMIT rule missing from the SQL fallback block")
	}
	// The resolved window for "last month" as of 2026-07-22 is June 2026,
	// rendered half-open so the model binds exactly what the guard requires.
	if !strings.Contains(p, "from '2026-06-01' to_exclusive '2026-07-01' (last month)") {
		t.Fatalf("window hint missing or wrong:\n%s", p)
	}
	// No window: no hint line.
	q := goldenQuestion()
	q.Text = "how many goats do we have"
	if strings.Contains(buildPlanPrompt(q, nil, goldenCatalog()), "Resolved window") {
		t.Fatal("window hint must be absent when the question has no period")
	}
}

func TestGenerateParsesUsageMetadataAndMaxOutputTokens(t *testing.T) {
	var gotReq genContentRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotReq)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"{\"refusal\":\"\",\"sub_questions\":[]}"}]}}],"usageMetadata":{"promptTokenCount":1234,"candidatesTokenCount":56,"totalTokenCount":1290}}`))
	}))
	defer srv.Close()
	p := &Planner{
		cfg:      Config{Project: "p", Location: "l", Model: "m"},
		tokens:   oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "t"}),
		http:     srv.Client(),
		endpoint: func(Config) string { return srv.URL },
	}
	_, usage, err := p.PlanWithUsage(context.Background(), goldenQuestion(), nil, goldenCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if usage.PromptTokens != 1234 || usage.OutputTokens != 56 {
		t.Fatalf("usage not parsed: %+v", usage)
	}
	if gotReq.GenerationConfig.MaxOutputTokens != 2048 {
		t.Fatalf("maxOutputTokens = %d, want 2048", gotReq.GenerationConfig.MaxOutputTokens)
	}

	// Absent usageMetadata -> zero usage (caller falls back to len/4).
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"{\"sql\":\"SELECT 1\"}"}]}}]}`))
	}))
	defer srv2.Close()
	p.endpoint = func(Config) string { return srv2.URL }
	p.http = srv2.Client()
	sql, usage2, err := p.RepairSQL(context.Background(), goldenQuestion(), "SELECT x", "banned keyword", "card", "")
	if err != nil || sql != "SELECT 1" {
		t.Fatalf("repair: sql=%q err=%v", sql, err)
	}
	if usage2 != (Usage{}) {
		t.Fatalf("expected zero usage without usageMetadata, got %+v", usage2)
	}
}

func TestParseRepair(t *testing.T) {
	if _, err := parseRepair(`{"sql":""}`); err == nil {
		t.Fatal("empty sql must error")
	}
	if _, err := parseRepair(`not json`); err == nil {
		t.Fatal("garbage must error")
	}
	sql, err := parseRepair("```json\n{\"sql\":\"SELECT 1 FROM ceo_ai.x WHERE tenant_id = 'a' LIMIT 1\"}\n```")
	if err != nil || !strings.HasPrefix(sql, "SELECT 1") {
		t.Fatalf("fenced repair: %q %v", sql, err)
	}
	rp := buildRepairPrompt("tenant-x", "SELECT bad", "sqlguard: banned keyword", "- ceo_ai.v: card", "v.d >= '2026-08-01' AND v.d < '2026-09-01'")
	for _, want := range []string{"SELECT bad", "banned keyword", "ceo_ai.v: card", `"tenant-x"`, "Required period binding"} {
		if !strings.Contains(rp, want) {
			t.Fatalf("repair prompt missing %q:\n%s", want, rp)
		}
	}
}
