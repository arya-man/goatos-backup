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

// foreignOrgPatterns name a tenant/organisation OTHER than the caller's. The
// farm's own two parks are routinely called "the other farm", so a bare
// farm/park word is NOT one of these: only an explicit second TENANT/company/
// organisation/account/client counts.
var foreignOrgPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(another|other|different|second|someone else'?s|somebody else'?s|their)\s+(tenant|organisation|organization|company|business|client|account|customer'?s? (?:tenant|account))\b`),
	regexp.MustCompile(`(?i)\b(tenant|organisation|organization|company|account)\s*(id)?\s*[:=]\s*\S+`),
	regexp.MustCompile(`(?i)\bswitch (to|into) (the )?(tenant|organisation|organization|company|account)\b`),
	regexp.MustCompile(`(?i)\bcross[- ]tenant\b`),
}

// uuidPattern matches any RFC-4122-shaped identifier in free text.
var uuidPattern = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)

// ForeignScopeReference reports a question that names a tenant/organisation
// scope other than the caller's own session scope, and why.
//
// Two shapes, both of which must be REFUSED rather than silently answered for
// the caller's own tenant (which reads to the asker as the other tenant's
// number):
//
//  1. an explicit second organisation ("the other tenant", "tenant_id = …");
//  2. ANY identifier (UUID) in the question that is not the caller's own
//     tenant id — leadership names parks, pens and people by label, never by
//     UUID, so a UUID in the text is either a foreign scope or an attempt to
//     steer the read by identifier. The caller's OWN tenant id is allowed
//     (it changes nothing).
func ForeignScopeReference(text, sessionTenantID string) (bool, string) {
	for _, re := range foreignOrgPatterns {
		if re.MatchString(text) {
			return true, "names an organisation other than the caller's session tenant"
		}
	}
	for _, id := range uuidPattern.FindAllString(text, -1) {
		if !strings.EqualFold(strings.TrimSpace(id), strings.TrimSpace(sessionTenantID)) {
			return true, "carries an identifier that is not the caller's session tenant"
		}
	}
	return false, ""
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
