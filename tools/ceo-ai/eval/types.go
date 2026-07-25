package main

// Contract types shared across the harness. Everything the scorer needs is a
// plain value type so scoring stays a pure, deterministic function of
// (question, response, oracle) and can be unit-tested without a network,
// a database, or Vertex.

// GoldenQuestion is one entry in the golden set (tools/ceo-ai/eval/golden/*.json).
type GoldenQuestion struct {
	ID       string  `json:"id"`
	Class    string  `json:"class"`
	Question string  `json:"question"`
	Notes    string  `json:"notes,omitempty"`
	Expect   Expect  `json:"expect"`
	Oracle   *Oracle `json:"oracle,omitempty"`
}

// Expect declares the properties the assistant answer must satisfy. Fields that
// are false/empty are simply not asserted, so a question opts into exactly the
// checks that apply to it.
type Expect struct {
	// Refusal: the assistant must refuse (read-only / out-of-scope / write
	// attempt / role or tenant override). A refusal must NOT contain a fabricated
	// business number.
	Refusal bool `json:"refusal,omitempty"`
	// AggregateFirst: the answer must be an aggregate, not a raw per-animal dump.
	AggregateFirst bool `json:"aggregate_first,omitempty"`
	// SpeciesSplit: the answer must report goat and sheep as distinct numbers.
	SpeciesSplit bool `json:"species_split,omitempty"`
	// IST: the answer resolves "today"/"this week" on the Asia/Kolkata business
	// calendar. Enforced through the grounded-number oracle (computed in
	// Asia/Kolkata); recorded here for reporting.
	IST bool `json:"ist,omitempty"`
	// InjectionSafe: the question embeds an injection attempt (malicious shed
	// name, role-escalation text, DDL in a param). The assistant must either
	// refuse or answer the correctly-scoped question without widening scope.
	InjectionSafe bool `json:"injection_safe,omitempty"`
	// InjectionForbidLeak flips the injection-safe semantics: when true the
	// oracle value is the number that would only appear if the assistant WIDENED
	// scope (e.g. the whole-tenant total for a bogus shed name), so a safe answer
	// must NOT contain it. When false (default) the oracle is the correctly-scoped
	// value the assistant should return.
	InjectionForbidLeak bool `json:"injection_forbid_leak,omitempty"`
	// Grounded: the answer's headline number must equal the oracle result.
	Grounded bool `json:"grounded,omitempty"`
	// TiersAnyOf: at least one citation tier must be in this set
	// (cube|api|toolbox|sql). Drives tool-selection accuracy.
	TiersAnyOf []string `json:"tiers_any_of,omitempty"`
}

// Oracle is an INDEPENDENT ground-truth query over canonical public.* tables
// (never the ceo_ai.* views the assistant may use), so the oracle is a genuine
// second computation of the number, not a mirror of the code under test.
type Oracle struct {
	// Kind: "scalar_int" (one integer) or "rows" (label|value pairs, e.g. the
	// species split). Empty/absent means there is no numeric ground truth
	// (refusal / off-domain questions).
	Kind string `json:"kind"`
	// SQL is a single SELECT. The tenant id is bound as the psql variable
	// :'tenant_id'. It must stay read-only; the harness rejects anything else.
	SQL string `json:"sql"`
}

// AssistantResponse is the leadership answer contract. Per the internal-tracking
// rule the user-facing payload carries only answer/source/mode/request_id/
// citations/conversation_id — never step traces or chain-of-thought.
type AssistantResponse struct {
	Answer         string     `json:"answer"`
	Source         string     `json:"source"`
	Mode           string     `json:"mode"` // answer|refusal|degraded|empty
	RequestID      string     `json:"request_id"`
	ConversationID string     `json:"conversation_id"`
	Citations      []Citation `json:"citations"`
}

// Citation is one provenance chip.
type Citation struct {
	Surface        string `json:"surface"`
	AsOf           string `json:"as_of"`
	Tier           string `json:"tier"` // cube|api|toolbox|sql
	PlannedByModel bool   `json:"planned_by_model"`
}

// OracleResult is the resolved ground truth for one question.
type OracleResult struct {
	Applicable bool             // false when the question has no numeric oracle
	Scalar     int64            // for Kind == "scalar_int"
	Rows       map[string]int64 // for Kind == "rows" (label -> value)
	Err        string           // non-empty if the oracle query failed
}

// Check is a single scored assertion for one question.
type Check struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}

// Result is the fully scored outcome for one golden question.
type Result struct {
	Question  GoldenQuestion     `json:"question"`
	Response  *AssistantResponse `json:"response,omitempty"`
	Oracle    OracleResult       `json:"oracle"`
	Checks    []Check            `json:"checks"`
	LatencyMS int64              `json:"latency_ms"`
	Errored   bool               `json:"errored"`
	ErrorText string             `json:"error_text,omitempty"`
	Passed    bool               `json:"passed"`
}

// Metrics is the deterministic scorecard over all results.
type Metrics struct {
	Total                 int     `json:"total"`
	Passed                int     `json:"passed"`
	Failed                int     `json:"failed"`
	Errored               int     `json:"errored"`
	PassRate              float64 `json:"pass_rate"`
	GroundingApplicable   int     `json:"grounding_applicable"`
	GroundingPassed       int     `json:"grounding_passed"`
	GroundingRate         float64 `json:"grounding_rate"`
	RefusalApplicable     int     `json:"refusal_applicable"`
	RefusalCorrect        int     `json:"refusal_correct"`
	RefusalAccuracy       float64 `json:"refusal_accuracy"`
	ToolSelApplicable     int     `json:"tool_selection_applicable"`
	ToolSelPassed         int     `json:"tool_selection_passed"`
	ToolSelectionAccuracy float64 `json:"tool_selection_accuracy"`
	LatencyP50MS          int64   `json:"latency_p50_ms"`
	LatencyP90MS          int64   `json:"latency_p90_ms"`
	LatencyP95MS          int64   `json:"latency_p95_ms"`
}

// Report is the top-level JSON artifact.
type Report struct {
	GeneratedAt  string   `json:"generated_at"`
	AssistantURL string   `json:"assistant_url"`
	TenantID     string   `json:"tenant_id"`
	CommitSHA    string   `json:"commit_sha"`
	Metrics      Metrics  `json:"metrics"`
	Results      []Result `json:"results"`
}
