// Package vertex is the Gemini-on-Vertex planner/reviewer adapter. It
// implements ports.AIProvider (Plan) and ports.Reviewer (Critique) by calling
// the Vertex generateContent REST endpoint with ADC credentials.
//
// Hard rules honored here:
//   - Gemini NEVER holds DB creds, executes SQL, or decides permissions. It
//     only classifies + decomposes + picks a tool/metric + extracts params.
//   - CUBE-FIRST: the system prompt instructs the model to prefer a governed
//     Cube metric for any official KPI; the app layer additionally ENFORCES it.
//   - The model output is parsed as data; the app validates every routed tool.
//
// The adapter is transport-only: config comes from env (MESHA_VERTEX_*), auth
// from Application Default Credentials (no secrets in code).
package vertex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
)

// Config is the Vertex connection config (from env; see MESHA_VERTEX_*).
type Config struct {
	Project  string
	Location string
	Model    string
}

// Planner calls Gemini via Vertex to plan leadership questions.
type Planner struct {
	cfg    Config
	tokens oauth2.TokenSource
	http   *http.Client
	// endpoint is overridable in tests to avoid network.
	endpoint func(cfg Config) string
}

// tokenSourceFn allows tests to inject credentials; production uses ADC.
var tokenSourceFn = func(ctx context.Context) (oauth2.TokenSource, error) {
	creds, err := google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, err
	}
	return creds.TokenSource, nil
}

// New builds a Vertex planner using Application Default Credentials.
func New(ctx context.Context, cfg Config) (*Planner, error) {
	if cfg.Project == "" || cfg.Location == "" || cfg.Model == "" {
		return nil, fmt.Errorf("vertex: project, location, and model are required")
	}
	ts, err := tokenSourceFn(ctx)
	if err != nil {
		return nil, fmt.Errorf("vertex: application default credentials: %w", err)
	}
	return &Planner{
		cfg:      cfg,
		tokens:   ts,
		http:     &http.Client{Timeout: 20 * time.Second},
		endpoint: defaultEndpoint,
	}, nil
}

func defaultEndpoint(cfg Config) string {
	return fmt.Sprintf(
		"https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/google/models/%s:generateContent",
		cfg.Location, cfg.Project, cfg.Location, cfg.Model,
	)
}

// PlannedByModel is true: this is the real model planner.
func (*Planner) PlannedByModel() bool { return true }

// Plan asks Gemini to classify + decompose the question and return a strict
// JSON plan. The app layer validates and enforces Cube-first afterward.
func (p *Planner) Plan(ctx context.Context, q domain.Question, mem []domain.ResolvedEntities, catalog []ports.ToolSpec) (domain.Plan, error) {
	prompt := buildPlanPrompt(q, mem, catalog)
	raw, err := p.generate(ctx, systemPlannerInstruction, prompt)
	if err != nil {
		return domain.Plan{}, err
	}
	return parsePlan(raw)
}

// Critique implements ports.Reviewer: judges whether the drafted answer is
// grounded in the supplied facts. Returns grounded=false with a reason on any
// unsupported claim. Never invents new facts.
func (p *Planner) Critique(ctx context.Context, answer string, facts []domain.Fact) (bool, string, error) {
	var sb strings.Builder
	for _, f := range facts {
		// Include the per-fact Scope (park/shed/species/…) in the evidence. Without
		// it a dimensioned answer that names its dimension value ("… (goat): 972")
		// looks unsupported to the critic — the exact cause of by-species/by-park
		// answers degrading to "couldn't verify a figure" while the facts were real.
		if strings.TrimSpace(f.Scope) != "" {
			sb.WriteString(fmt.Sprintf("- %s (%s) = %s\n", f.Label, f.Scope, f.Value))
		} else {
			sb.WriteString(fmt.Sprintf("- %s = %s\n", f.Label, f.Value))
		}
	}
	prompt := fmt.Sprintf(
		"Facts (the ONLY allowed evidence):\n%s\nDrafted answer:\n%q\n\nReturn strict JSON {\"grounded\":bool,\"reason\":string}. Check that every NUMBER and DATA CLAIM (figure, count, rate, percent, named entity, scope) in the answer traces to a fact. Ignore narrative prose, restatements, draft-metric disclaimers, source labels, and framing words. grounded=false ONLY if a number or data claim is missing from or contradicts the facts.",
		sb.String(), answer,
	)
	raw, err := p.generate(ctx, systemReviewerInstruction, prompt)
	if err != nil {
		return true, "", err // reviewer failure must not block; app is authoritative
	}
	var out struct {
		Grounded bool   `json:"grounded"`
		Reason   string `json:"reason"`
	}
	if e := json.Unmarshal([]byte(extractJSON(raw)), &out); e != nil {
		return true, "", nil
	}
	return out.Grounded, out.Reason, nil
}

// --- Vertex REST plumbing ---

type genContentRequest struct {
	SystemInstruction *content        `json:"systemInstruction,omitempty"`
	Contents          []content       `json:"contents"`
	GenerationConfig  genConfig       `json:"generationConfig"`
	SafetySettings    []safetySetting `json:"safetySettings,omitempty"`
}

type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}
type part struct {
	Text string `json:"text"`
}
type genConfig struct {
	Temperature      float64 `json:"temperature"`
	MaxOutputTokens  int     `json:"maxOutputTokens"`
	ResponseMIMEType string  `json:"responseMimeType,omitempty"`
}
type safetySetting struct {
	Category  string `json:"category"`
	Threshold string `json:"threshold"`
}

type genContentResponse struct {
	Candidates []struct {
		Content content `json:"content"`
	} `json:"candidates"`
}

func (p *Planner) generate(ctx context.Context, system, user string) (string, error) {
	reqBody := genContentRequest{
		SystemInstruction: &content{Parts: []part{{Text: system}}},
		Contents:          []content{{Role: "user", Parts: []part{{Text: user}}}},
		GenerationConfig:  genConfig{Temperature: 0.1, MaxOutputTokens: 1024, ResponseMIMEType: "application/json"},
		SafetySettings:    defaultSafetySettings(),
	}
	buf, _ := json.Marshal(reqBody)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint(p.cfg), bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	tok, err := p.tokens.Token()
	if err != nil {
		return "", fmt.Errorf("vertex: token: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.http.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("vertex: status %d: %s", resp.StatusCode, string(body))
	}
	var gr genContentResponse
	if err := json.Unmarshal(body, &gr); err != nil {
		return "", err
	}
	if len(gr.Candidates) == 0 || len(gr.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("vertex: empty candidate")
	}
	return gr.Candidates[0].Content.Parts[0].Text, nil
}

// defaultSafetySettings sets conservative harm thresholds for a leadership tool.
func defaultSafetySettings() []safetySetting {
	const block = "BLOCK_MEDIUM_AND_ABOVE"
	return []safetySetting{
		{"HARM_CATEGORY_HARASSMENT", block},
		{"HARM_CATEGORY_HATE_SPEECH", block},
		{"HARM_CATEGORY_SEXUALLY_EXPLICIT", block},
		{"HARM_CATEGORY_DANGEROUS_CONTENT", block},
	}
}
