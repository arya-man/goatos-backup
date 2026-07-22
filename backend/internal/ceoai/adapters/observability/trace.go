package observability

import (
	"regexp"
	"strings"
	"time"
)

// StepTrace is one internal execution step in the assistant pipeline. It is
// INTERNAL: it is persisted for the admin-only debug surface and NEVER returned
// in a leadership chat answer (committed Internal Tracking rule). Goat RFID/
// tags are not PII and may appear; human-actor identity and secrets must not.
type StepTrace struct {
	SubQuestion string `json:"sub_question"`
	Route       string `json:"route"`     // cube|mesha_api|mcp_toolbox|sql_fallback|none
	ToolName    string `json:"tool_name"` // resolved tool / metric / view
	Params      string `json:"params"`    // redacted, JSON-ish param summary (no tenant, no secret)
	RowCount    int    `json:"row_count"`
	DurationMS  int64  `json:"duration_ms"`
	Verdict     string `json:"verdict,omitempty"` // review verdict for this step, if any
	Err         string `json:"err,omitempty"`
}

// TraceRecord is the full internal audit/trace of one assistant request, keyed
// by request_id and scoped to a tenant. It is what the admin debug endpoint
// returns to the ceo_internal/superadmin cohort ONLY.
type TraceRecord struct {
	RequestID        string      `json:"request_id"`
	TenantID         string      `json:"-"` // scope key; never serialized to the client
	ActorRole        string      `json:"actor_role"`
	ConversationID   string      `json:"conversation_id,omitempty"`
	QuestionRedacted string      `json:"question_redacted"`
	RouteTier        string      `json:"route_tier"`
	ToolCalled       string      `json:"tool_called"`
	SourceViews      []string    `json:"source_views"`
	RowCount         int         `json:"row_count"`
	LatencyMS        int         `json:"latency_ms"`
	Status           string      `json:"status"`
	RejectionReason  string      `json:"rejection_reason,omitempty"`
	ReviewVerdict    string      `json:"review_verdict,omitempty"`
	ModelVersion     string      `json:"model_version,omitempty"`
	PromptVersion    string      `json:"prompt_version,omitempty"`
	Steps            []StepTrace `json:"steps"`
	CreatedAt        time.Time   `json:"created_at"`
}

// secretPattern matches obvious credential-shaped tokens so a step trace can
// never persist a leaked secret even if an upstream layer accidentally placed
// one in a param summary. This is defense-in-depth: params should already be
// scrubbed at the source. Goat RFID/tag values are deliberately NOT matched —
// they are not PII (repo rule) and are useful for debugging.
var secretPattern = regexp.MustCompile(
	`(?i)(bearer\s+[a-z0-9._\-]+` +
		`|eyJ[a-zA-Z0-9._\-]{10,}` + // JWT
		`|sk-[a-zA-Z0-9]{16,}` + // API-key shaped
		`|AIza[a-zA-Z0-9_\-]{20,}` + // Google API key
		`|-----BEGIN[^-]+PRIVATE ` + `KEY-----` + // split literal so the repo secret-scan guard does not self-match this redaction pattern
		`|(?:password|passwd|secret|api[_-]?key|token|authorization)\s*[=:]\s*\S+)`,
)

const redactedToken = "[REDACTED]"

// RedactSecrets scrubs credential-shaped substrings from any string bound for
// the internal trace store. Called on question text and every param summary
// before persistence so the admin debug surface cannot leak secrets.
func RedactSecrets(s string) string {
	if s == "" {
		return s
	}
	return secretPattern.ReplaceAllString(s, redactedToken)
}

// Sanitize returns a copy of the record with every free-text field passed
// through RedactSecrets. The store calls this on write so redaction is
// enforced at the persistence boundary, not left to callers.
func (t TraceRecord) Sanitize() TraceRecord {
	out := t
	out.QuestionRedacted = RedactSecrets(t.QuestionRedacted)
	out.RejectionReason = RedactSecrets(t.RejectionReason)
	steps := make([]StepTrace, len(t.Steps))
	for i, st := range t.Steps {
		st.Params = RedactSecrets(st.Params)
		st.SubQuestion = RedactSecrets(st.SubQuestion)
		st.Err = RedactSecrets(st.Err)
		steps[i] = st
	}
	out.Steps = steps
	return out
}

// HasSecretLeak reports whether any serialized field still matches the secret
// pattern. Used by tests and as a last-line assertion; a true here is a bug.
func HasSecretLeak(t TraceRecord) bool {
	fields := []string{t.QuestionRedacted, t.RejectionReason}
	for _, st := range t.Steps {
		fields = append(fields, st.Params, st.SubQuestion, st.Err)
	}
	return secretPattern.MatchString(strings.Join(fields, "\n"))
}
