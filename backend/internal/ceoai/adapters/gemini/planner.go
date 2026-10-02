// Package gemini is the Gemini planner/reviewer adapter. It implements
// ports.AIProvider (Plan) and ports.Reviewer (Critique) by calling the Gemini
// Developer API (generativelanguage.googleapis.com) generateContent endpoint
// with an AI Studio API key (prepaid credits). It never calls Vertex AI.
//
// Hard rules honored here:
//   - Gemini NEVER holds DB creds, executes SQL, or decides permissions. It
//     only classifies + decomposes + picks a tool/metric + extracts params.
//   - CUBE-FIRST: the system prompt instructs the model to prefer a governed
//     Cube metric for any official KPI; the app layer additionally ENFORCES it.
//   - The model output is parsed as data; the app validates every routed tool.
//
// The adapter is transport-only: config comes from env (MESHA_GEMINI_*); the
// key is mounted from Secret Manager, never in code.
package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/ceoai/app"
	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
)

// Config is the Gemini Developer API config (from env; see MESHA_GEMINI_*).
type Config struct {
	APIKey string
	Model  string
}

// Planner calls Gemini via the Developer API to plan leadership questions.
type Planner struct {
	cfg  Config
	http *http.Client
	// endpoint is overridable in tests to avoid network.
	endpoint func(cfg Config) string
}

// New builds a Gemini planner. The API key is required: there is no
// credential fallback (and never a Vertex fallback).
func New(_ context.Context, cfg Config) (*Planner, error) {
	if strings.TrimSpace(cfg.APIKey) == "" || cfg.Model == "" {
		return nil, fmt.Errorf("gemini: api key and model are required")
	}
	return &Planner{
		cfg:      cfg,
		http:     &http.Client{Timeout: 20 * time.Second},
		endpoint: defaultEndpoint,
	}, nil
}

// APIHost is the Gemini Developer API host (AI Studio key, prepaid billing).
const APIHost = "generativelanguage.googleapis.com"

func defaultEndpoint(cfg Config) string {
	return fmt.Sprintf("https://%s/v1beta/models/%s:generateContent", APIHost, url.PathEscape(cfg.Model))
}

// PlannedByModel is true: this is the real model planner.
func (*Planner) PlannedByModel() bool { return true }

// Plan asks Gemini to classify + decompose the question and return a strict
// JSON plan. The app layer validates and enforces Cube-first afterward.
func (p *Planner) Plan(ctx context.Context, q domain.Question, mem []domain.ResolvedEntities, catalog []ports.ToolSpec) (domain.Plan, error) {
	plan, _, err := p.PlanWithUsage(ctx, q, mem, catalog)
	return plan, err
}

// Usage is the token accounting Gemini reports for one generateContent call
// (usageMetadata.promptTokenCount / candidatesTokenCount). Zero values mean
// the response carried no usageMetadata; callers fall back to a len/4
// estimate in that case (plan v3 D1.1).
type Usage = app.TokenUsage

// PlanWithUsage is Plan plus the real token usage of the planner call. The
// orchestrator type-asserts for this so the budget records what Gemini billed
// instead of a character estimate.
func (p *Planner) PlanWithUsage(ctx context.Context, q domain.Question, mem []domain.ResolvedEntities, catalog []ports.ToolSpec) (domain.Plan, Usage, error) {
	prompt := buildPlanPrompt(q, mem, catalog)
	raw, usage, err := p.generate(ctx, systemPlannerInstruction, prompt)
	if err != nil {
		return domain.Plan{}, usage, err
	}
	plan, err := parsePlan(raw)
	return plan, usage, err
}

// RepairSQL is the one-shot repair call (plan v3 D1.3): given the rejected
// draft, the guard/Postgres reason and the schema card of the referenced
// view, ask the model for exactly one corrected draft. The caller re-validates
// the result; this never executes anything. windowText, when non-empty, is
// the literal period binding the guard will require.
func (p *Planner) RepairSQL(ctx context.Context, q domain.Question, failedSQL, reason, cardText, windowText string) (string, Usage, error) {
	prompt := buildRepairPrompt(q.Actor.TenantID, failedSQL, reason, cardText, windowText)
	raw, usage, err := p.generate(ctx, systemRepairInstruction, prompt)
	if err != nil {
		return "", usage, err
	}
	sql, err := parseRepair(raw)
	return sql, usage, err
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
	raw, _, err := p.generate(ctx, systemReviewerInstruction, prompt)
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

// --- Gemini REST plumbing ---

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
	// UsageMetadata is Gemini's billed token accounting for the call.
	UsageMetadata *usageMetadata `json:"usageMetadata,omitempty"`
}

type usageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

// maxOutputTokens bounds one planner/reviewer/repair response. Raised from
// 1024 to 2048 (plan v3 D1.1): a multi-sub-question plan that quotes a full
// SQL draft per sub-question was being truncated mid-JSON at 1024.
const maxOutputTokens = 2048

// parseUsage maps the response's usageMetadata to Usage (zero when absent).
func parseUsage(u *usageMetadata) Usage {
	if u == nil {
		return Usage{}
	}
	return Usage{PromptTokens: u.PromptTokenCount, OutputTokens: u.CandidatesTokenCount}
}

func (p *Planner) generate(ctx context.Context, system, user string) (string, Usage, error) {
	reqBody := genContentRequest{
		SystemInstruction: &content{Parts: []part{{Text: system}}},
		Contents:          []content{{Role: "user", Parts: []part{{Text: user}}}},
		GenerationConfig:  genConfig{Temperature: 0.1, MaxOutputTokens: maxOutputTokens, ResponseMIMEType: "application/json"},
		SafetySettings:    defaultSafetySettings(),
	}
	buf, _ := json.Marshal(reqBody)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint(p.cfg), bytes.NewReader(buf))
	if err != nil {
		return "", Usage{}, err
	}
	// Key in a header (never the URL), so it does not show up in logged request URLs.
	httpReq.Header.Set("x-goog-api-key", p.cfg.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.http.Do(httpReq)
	if err != nil {
		return "", Usage{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", Usage{}, fmt.Errorf("gemini: status %d: %s", resp.StatusCode, clipBody(body, 500))
	}
	var gr genContentResponse
	if err := json.Unmarshal(body, &gr); err != nil {
		return "", Usage{}, err
	}
	usage := parseUsage(gr.UsageMetadata)
	if len(gr.Candidates) == 0 || len(gr.Candidates[0].Content.Parts) == 0 {
		return "", usage, fmt.Errorf("gemini: empty candidate")
	}
	return gr.Candidates[0].Content.Parts[0].Text, usage, nil
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

// clipBody bounds an error body quoted into an error/log (Gemini errors can be long JSON).
func clipBody(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
