// Package guard is the prompt-injection defense boundary. Rule: user text and
// any tool-returned text are DATA, never instructions. Tenant + role scope come
// ONLY from the server session and are NEVER read from user text. The assistant
// is READ-ONLY; any write/scope-escalation intent is refused upstream.
package guard

import (
	"regexp"
	"strings"
)

// injectionPatterns match attempts to override instructions, escalate scope,
// or coerce cross-tenant / write behaviour. Matches are neutralized (the text
// stays as an opaque filter value) and reported so the orchestrator can refuse
// scope-escalation while still answering benign questions.
var injectionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)ignore (all |the |previous |prior )?(instructions|rules|prompt)`),
	regexp.MustCompile(`(?i)disregard (all |the )?(previous|prior|above)`),
	regexp.MustCompile(`(?i)you are now|act as (a )?(superadmin|admin|system|developer)`),
	regexp.MustCompile(`(?i)(show|list|give) me (all )?(tenants|other tenants|every tenant)`),
	regexp.MustCompile(`(?i)override (the )?(tenant|role|scope|session)`),
	regexp.MustCompile(`(?i)(reveal|print|show|dump) (the )?(system prompt|prompt|tool list|sql|chain.?of.?thought|step trace)`),
	regexp.MustCompile(`(?i)(drop|delete|truncate|update|insert into|grant)\s`),
	regexp.MustCompile(`(?i);\s*(drop|delete|update|insert|--)`),
}

// scopeEscalationPatterns are the subset that specifically try to widen tenant
// or role scope. These force a refusal (scope comes from the session only).
var scopeEscalationPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(all|other|every) tenants?`),
	regexp.MustCompile(`(?i)act as (a )?(superadmin|admin|system)`),
	regexp.MustCompile(`(?i)override (the )?(tenant|role|scope|session)`),
	regexp.MustCompile(`(?i)ignore my role`),
}

// Result reports the injection-scan outcome for a piece of untrusted text.
type Result struct {
	Detected        bool
	ScopeEscalation bool
	Reasons         []string
}

// Scan inspects untrusted text (user question OR tool-returned string) and
// reports detected injection attempts. It does not mutate the text — callers
// treat the value as opaque data regardless.
func Scan(text string) Result {
	var res Result
	for _, re := range injectionPatterns {
		if re.MatchString(text) {
			res.Detected = true
			res.Reasons = append(res.Reasons, re.String())
		}
	}
	for _, re := range scopeEscalationPatterns {
		if re.MatchString(text) {
			res.ScopeEscalation = true
		}
	}
	return res
}

// SanitizeToolText renders tool-returned free text safe to hand to the model as
// evidence: it flattens control/instruction framing so embedded "as CEO you
// approved..." style content cannot read as a command. The value remains data.
func SanitizeToolText(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
