package safety

import (
	"context"
	"regexp"
	"strings"
)

// HarmCategory mirrors the Vertex/Gemini safety harm categories the assistant
// configures. Kept as a local enum so this package has no hard dependency on the
// Vertex SDK; the vertex adapter maps these to genai.HarmCategory values.
type HarmCategory string

const (
	HarmHateSpeech       HarmCategory = "HARM_CATEGORY_HATE_SPEECH"
	HarmDangerousContent HarmCategory = "HARM_CATEGORY_DANGEROUS_CONTENT"
	HarmHarassment       HarmCategory = "HARM_CATEGORY_HARASSMENT"
	HarmSexualContent    HarmCategory = "HARM_CATEGORY_SEXUALLY_EXPLICIT"
)

// HarmBlockThreshold mirrors the Vertex block thresholds.
type HarmBlockThreshold string

const (
	BlockLowAndAbove    HarmBlockThreshold = "BLOCK_LOW_AND_ABOVE"
	BlockMediumAndAbove HarmBlockThreshold = "BLOCK_MEDIUM_AND_ABOVE"
	BlockOnlyHigh       HarmBlockThreshold = "BLOCK_ONLY_HIGH"
	BlockNone           HarmBlockThreshold = "BLOCK_NONE"
)

// VertexSafetySetting is one harm-category threshold pair.
type VertexSafetySetting struct {
	Category  HarmCategory
	Threshold HarmBlockThreshold
}

// DefaultVertexSafetySettings returns the harm-category thresholds the leadership
// assistant sends to Gemini on every generateContent call. A leadership
// operations tool has no legitimate need for any harmful category, so all
// categories block at medium-and-above (Gemini's strictest broadly-safe tier).
func DefaultVertexSafetySettings() []VertexSafetySetting {
	return []VertexSafetySetting{
		{HarmHateSpeech, BlockMediumAndAbove},
		{HarmDangerousContent, BlockMediumAndAbove},
		{HarmHarassment, BlockMediumAndAbove},
		{HarmSexualContent, BlockMediumAndAbove},
	}
}

// ModerationStage distinguishes inbound (user question) from outbound (composed
// answer) screening, which have slightly different policies.
type ModerationStage int

const (
	StageInput ModerationStage = iota
	StageOutput
)

// Moderator screens text for abuse, unsafe content, and off-domain use. It is a
// port: the default HeuristicModerator runs with no external dependency for
// local/CI, and a Vertex-backed implementation can be substituted in production.
type Moderator interface {
	// Moderate returns an allow verdict, or a DecisionRefuse verdict with a
	// safe, scoped user message when the text must be blocked.
	Moderate(ctx context.Context, stage ModerationStage, text string) Verdict
}

// HeuristicModerator is a dependency-free moderator used for local dev, tests,
// and as the always-on pre/post filter that runs even when a model-based
// moderator is also configured (defense in depth).
type HeuristicModerator struct {
	unsafe   []*regexp.Regexp
	offTopic *offTopicClassifier
}

// NewHeuristicModerator constructs the default moderator.
func NewHeuristicModerator() *HeuristicModerator {
	pats := []string{
		// Overt abuse/harassment aimed at people.
		`(?i)\b(kill|murder|assault|hurt|harm)\s+(him|her|them|the\s+\w+|my\s+\w+|that\s+\w+)\b`,
		`(?i)\b(make|build|synthesize|assemble)\s+(an?\s+)?(bomb|explosive|weapon|poison|nerve\s+agent)\b`,
		`(?i)\bhow\s+to\s+(make|build|create)\s+(a\s+)?(bomb|explosive|meth|poison)\b`,
		`(?i)\b(self[-\s]?harm|suicide\s+method|how\s+to\s+kill\s+myself)\b`,
	}
	res := make([]*regexp.Regexp, 0, len(pats))
	for _, p := range pats {
		res = append(res, regexp.MustCompile(p))
	}
	return &HeuristicModerator{unsafe: res, offTopic: newOffTopicClassifier()}
}

// Moderate implements Moderator.
func (m *HeuristicModerator) Moderate(_ context.Context, stage ModerationStage, text string) Verdict {
	if m == nil {
		return allow()
	}
	for _, re := range m.unsafe {
		if re.MatchString(text) {
			return Verdict{
				Decision:    DecisionRefuse,
				Reason:      "moderation:unsafe_content",
				UserMessage: "I can't help with that. I'm the Mesha operations assistant and can only answer read-only questions about your farm operations.",
			}
		}
	}
	// Off-domain screening applies to the inbound question. We keep it as a
	// soft signal on output (an answer legitimately contains operations terms).
	if stage == StageInput && m.offTopic.isOffTopic(text) {
		return Verdict{
			Decision:    DecisionRefuse,
			Reason:      "moderation:off_domain",
			UserMessage: "I can only answer questions about Mesha farm operations — herd counts, vaccination, feed, procurement, workforce, and related operational topics.",
		}
	}
	return allow()
}

// offTopicClassifier is a tiny keyword heuristic: a question is treated as
// off-domain only when it contains a clear off-topic signal AND no operational
// vocabulary. This is deliberately conservative to avoid refusing legitimate
// short questions ("how many now?").
type offTopicClassifier struct {
	domain  *regexp.Regexp
	offTopc *regexp.Regexp
}

func newOffTopicClassifier() *offTopicClassifier {
	domain := regexp.MustCompile(`(?i)\b(animal|animals|goat|goats|sheep|herd|shed|sheds|park|parks|vaccinat\w*|dose|doses|feed|feeding|ration|procure\w*|intake|load|loads|count|counts|census|headcount|overdue|due|adherence|compliance|mortality|birth|births|death|deaths|stock|inventory|operator|operators|roster|coverage|backup|manager|breed|kid|kids|adult|obligation|verification|proof|capacity|movement|shift\w*|castro|channapatna|gandhi|mesha)\b`)
	off := regexp.MustCompile(`(?i)\b(weather|stock\s+market|bitcoin|crypto|movie|song|lyrics|recipe|joke|poem|football|cricket\s+score|election|president|celebrity|dating|horoscope|write\s+(me\s+)?(a\s+)?(story|essay|code)|translate\s+this\s+(poem|song))\b`)
	return &offTopicClassifier{domain: domain, offTopc: off}
}

func (c *offTopicClassifier) isOffTopic(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	if c.domain.MatchString(t) {
		return false // has operational vocabulary -> in-domain
	}
	return c.offTopc.MatchString(t)
}

// ChainModerators runs several moderators in order and returns the first
// non-allow verdict. Used to layer the always-on heuristic filter before/after a
// model-based moderator.
func ChainModerators(mods ...Moderator) Moderator {
	return moderatorChain(mods)
}

type moderatorChain []Moderator

func (c moderatorChain) Moderate(ctx context.Context, stage ModerationStage, text string) Verdict {
	for _, m := range c {
		if m == nil {
			continue
		}
		if v := m.Moderate(ctx, stage, text); !v.Allowed() {
			return v
		}
	}
	return allow()
}
