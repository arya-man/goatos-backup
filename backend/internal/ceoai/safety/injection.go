package safety

import (
	"regexp"
	"strings"
)

// InjectionScanner detects prompt-injection / instruction-override attempts in
// untrusted text. Two text surfaces are untrusted and MUST flow through here:
//
//  1. The leadership user's question.
//  2. Any text returned from a tool/DB (a shed/park/vaccine/operator NAME, a
//     load/exception title, an audit note). Data fetched from the database can
//     itself carry an injection payload.
//
// The scanner NEVER mutates tenant/role/tool-allowlist. Those come from the
// server session (Identity) and this package deliberately offers no path to
// change them from text. The scanner's job is to (a) flag override attempts so
// the orchestrator can refuse when appropriate, and (b) produce a segregated,
// clearly-delimited representation so downstream prompt assembly keeps user text
// as DATA, never as instructions.
type InjectionScanner struct {
	patterns []*regexp.Regexp
}

// injectionPatterns is the built-in adversarial corpus. Case-insensitive.
// Grouped by attack family; kept explicit (not one mega-regex) for auditability.
var injectionPatterns = []string{
	// Instruction override / ignore-previous.
	`(?i)ignore\s+(all\s+|the\s+|any\s+)?(previous|prior|earlier|above|preceding)\s+(instruction|instructions|prompt|prompts|message|messages|context|directions?)`,
	`(?i)disregard\s+(all\s+|the\s+|any\s+|your\s+)?(previous|prior|earlier|above|system|prior\s+)?(instruction|instructions|prompt|rules?|guardrails?)`,
	`(?i)forget\s+(everything|all|your|the)\s+(above|previous|prior|instructions?|rules?)`,
	`(?i)override\s+(the\s+|your\s+|all\s+)?(system|previous|prior|safety|security)\s+(prompt|instruction|instructions|settings?|rules?)`,
	`(?i)do\s+not\s+(follow|obey|adhere\s+to)\s+(the\s+|your\s+|any\s+)?(previous|prior|system|above)\s+(instruction|instructions|rules?)`,

	// Role / persona reassignment.
	`(?i)you\s+are\s+now\s+(a|an|the|in|no\s+longer)`,
	`(?i)(act|behave|respond|roleplay|pretend)\s+as\s+(if\s+you\s+are\s+|though\s+you\s+are\s+|a\s+|an\s+|the\s+)?(super\s?admin|admin|root|developer|dan|jailbroken|unrestricted)`,
	`(?i)pretend\s+(to\s+be|you\s+are|that\s+you)`,
	`(?i)from\s+now\s+on\s+you\s+(are|will|must|should)`,
	`(?i)switch\s+to\s+(developer|debug|god|admin|unrestricted|jailbreak)\s+mode`,
	`(?i)enable\s+(developer|debug|dan|jailbreak|unrestricted)\s+mode`,

	// Fake system / turn spoofing.
	`(?i)^\s*(system|assistant|developer)\s*[:>]`,
	`(?i)\bsystem\s*:\s*(you|ignore|the\s+assistant|new\s+instruction)`,
	`(?i)\[\s*(system|admin|developer)\s*\]`,
	`(?i)<\s*(system|/?s|im_start|im_end)\s*>`,
	`(?i)###\s*(system|instruction|new\s+instruction)`,
	`(?i)\bBEGIN\s+SYSTEM\s+PROMPT\b`,

	// Scope / tenant / role escalation via text.
	`(?i)(show|give|list|access|switch\s+to|use)\s+(me\s+)?(all\s+)?(other\s+)?tenants?`,
	`(?i)(ignore|bypass|change|override|drop|remove)\s+(the\s+|my\s+|any\s+)?(tenant|role|permission|rbac|scope|acl)\s*(filter|scope|check|restriction|boundary)?`,
	`(?i)i\s+am\s+(a\s+|the\s+|now\s+)?(super\s?admin|admin|ceo|root|god|owner)\b.*\b(so|therefore|override|show|give|grant)`,
	`(?i)cross[-\s]?tenant`,
	`(?i)(as|since)\s+(the\s+)?(ceo|admin|superadmin)\b.*\b(you\s+)?(approved|authorize|grant|override|delete)`,

	// Exfiltration of internals / secrets / prompt.
	`(?i)(print|reveal|show|repeat|output|display|dump|leak|expose)\s+(me\s+)?(your\s+|the\s+|all\s+)?(system\s+prompt|prompt|instructions|tool\s+list|tools|chain[-\s]?of[-\s]?thought|reasoning|step\s+trace|hidden\s+(rules?|prompt))`,
	`(?i)(what|repeat)\s+(are|were|is)\s+your\s+(system\s+)?(instructions|prompt|rules|guidelines)`,
	`(?i)(reveal|show|print|give|dump|exfiltrate|leak)\s+(me\s+)?(the\s+|your\s+|all\s+)?(env|environment)\s*(vars?|variables?)?`,
	`(?i)(reveal|show|print|give|dump)\s+(me\s+)?(the\s+|your\s+|any\s+)?(api[_\s-]?key|apikey|token|password|passwd|secret|credential|service[-\s]?account|db\s+password|connection\s+string)`,

	// Side-effect / write coercion smuggled as instruction.
	`(?i)\b(delete|drop|truncate|update|insert|grant|revoke)\s+(table|from|into|all|the)\b`,
	`(?i)(send|email|post|forward|upload|exfiltrate|leak)\b[^.]{0,40}?\bto\b[^.]{0,25}(https?://|www\.|@)`,
}

// NewInjectionScanner builds the scanner with the built-in corpus plus any extra
// patterns. Invalid extra patterns are skipped rather than panicking.
func NewInjectionScanner(extra ...string) *InjectionScanner {
	all := make([]*regexp.Regexp, 0, len(injectionPatterns)+len(extra))
	for _, p := range injectionPatterns {
		all = append(all, regexp.MustCompile(p))
	}
	for _, p := range extra {
		if re, err := regexp.Compile(p); err == nil {
			all = append(all, re)
		}
	}
	return &InjectionScanner{patterns: all}
}

// Scan reports whether text contains a prompt-injection / override attempt and,
// if so, returns the stable slug of the first matching family for logging.
func (s *InjectionScanner) Scan(text string) (matched bool, reason string) {
	if s == nil {
		return false, ""
	}
	// Normalize common obfuscations before matching: collapse whitespace and
	// strip zero-width characters used to split trigger words.
	norm := normalizeForScan(text)
	for _, re := range s.patterns {
		if re.MatchString(norm) {
			return true, "prompt_injection:" + reFamily(re.String())
		}
	}
	return false, ""
}

// ScreenQuestion evaluates the leadership user's question. An override attempt in
// the question is refused (the assistant will not act on smuggled instructions),
// with a scoped, non-leaky user message.
func (s *InjectionScanner) ScreenQuestion(question string) Verdict {
	if matched, reason := s.Scan(question); matched {
		return Verdict{
			Decision:    DecisionRefuse,
			Reason:      reason,
			UserMessage: "I can only answer read-only questions about Mesha operations within your access scope. I can't change roles, tenants, or system behavior, or reveal internal system details.",
		}
	}
	return allow()
}

// SanitizeToolText prepares text that came from a tool/DB result for safe
// inclusion in a model prompt. It does NOT reject (a shed may legitimately be
// named oddly); instead it neutralizes the text as opaque data: strips control
// characters, collapses turn/role markers, and wraps it so the planner treats it
// as a value to filter on, never as an instruction. The second return reports
// whether an injection signature was present, so the caller can log it.
func (s *InjectionScanner) SanitizeToolText(field, value string) (safe string, flagged bool) {
	flagged, _ = s.Scan(value)
	cleaned := stripControl(value)
	// Drop bracket delimiters so DB text cannot break out of the data wrapper.
	cleaned = strings.NewReplacer("[", "(", "]", ")").Replace(cleaned)
	// Defang fake turn/role markers (including inline "system:") inside DB text.
	cleaned = fakeTurnMarker.ReplaceAllString(cleaned, "_")
	// Present as an explicitly delimited data value, never free text.
	return "[DATA " + sanitizeFieldName(field) + "=" + quoteData(cleaned) + "]", flagged
}

// EnforceScope is the hard guarantee that scope is server-controlled. Given the
// session Identity and an arbitrary requested-scope string parsed from user
// text, it ALWAYS returns the session identity's scope, discarding any
// user-supplied tenant/role. It returns flagged=true when the user text tried to
// assert a different tenant/role, so the attempt is auditable.
func EnforceScope(session Identity, requestedTenantFromText, requestedRoleFromText string) (scoped Identity, flagged bool) {
	rt := strings.TrimSpace(requestedTenantFromText)
	rr := strings.TrimSpace(requestedRoleFromText)
	if rt != "" && !strings.EqualFold(rt, session.TenantID) {
		flagged = true
	}
	if rr != "" && !strings.EqualFold(rr, session.Role) {
		flagged = true
	}
	// Session identity wins, unconditionally.
	return session, flagged
}

// --- helpers ---

var (
	zeroWidth      = regexp.MustCompile(`[\x{200B}-\x{200D}\x{FEFF}\x{2060}]`)
	whitespaceRun  = regexp.MustCompile(`\s+`)
	fakeTurnMarker = regexp.MustCompile(`(?i)<\s*/?\s*(system|im_start|im_end|assistant|user)\s*>|\b(system|assistant|developer)\s*:`)
	fieldNameSafe  = regexp.MustCompile(`[^a-zA-Z0-9_.-]`)
)

func normalizeForScan(text string) string {
	t := zeroWidth.ReplaceAllString(text, "")
	t = whitespaceRun.ReplaceAllString(t, " ")
	return t
}

func stripControl(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		// Keep printable + normal spaces/newlines-as-space; drop other controls.
		if r == '\n' || r == '\t' || r == '\r' {
			b.WriteByte(' ')
			continue
		}
		if r < 0x20 || r == 0x7f {
			continue
		}
		b.WriteRune(r)
	}
	return zeroWidth.ReplaceAllString(b.String(), "")
}

func sanitizeFieldName(field string) string {
	f := fieldNameSafe.ReplaceAllString(field, "")
	if f == "" {
		return "value"
	}
	return f
}

func quoteData(s string) string {
	// Escape the delimiter so embedded ] cannot break out of the data wrapper.
	s = strings.ReplaceAll(s, `]`, `\]`)
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

// reFamily derives a short, stable family slug from a pattern for metrics. It is
// intentionally coarse (keyword-based) so slugs stay low-cardinality.
func reFamily(pat string) string {
	switch {
	case strings.Contains(pat, "tenant") || strings.Contains(pat, "cross"):
		return "scope_escalation"
	case strings.Contains(pat, "role") || strings.Contains(pat, "admin") || strings.Contains(pat, "ceo"):
		return "role_escalation"
	case strings.Contains(pat, "system prompt") || strings.Contains(pat, "chain") || strings.Contains(pat, "tool"):
		return "exfiltrate_internals"
	case strings.Contains(pat, "api") || strings.Contains(pat, "secret") || strings.Contains(pat, "password") || strings.Contains(pat, "env"):
		return "exfiltrate_secrets"
	case strings.Contains(pat, "delete") || strings.Contains(pat, "drop") || strings.Contains(pat, "send") || strings.Contains(pat, "email"):
		return "write_coercion"
	case strings.Contains(pat, "you are now") || strings.Contains(pat, "pretend") || strings.Contains(pat, "roleplay") || strings.Contains(pat, "mode"):
		return "persona_reassign"
	default:
		return "instruction_override"
	}
}
