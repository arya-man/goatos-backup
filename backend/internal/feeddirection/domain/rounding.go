package domain

import (
	"fmt"
	"math/big"
	"strings"
)

// ---------------------------------------------------------------------------
// Rounding policy
// ---------------------------------------------------------------------------
//
// Feed quantity arithmetic is EXACT until the single rounding step at the end. Every intermediate
// value is a *big.Rat, never a float64, because the inputs are exact decimals from the database
// (numeric(12,3) grams, numeric(8,4) multipliers, numeric(6,4) split fractions) and binary
// floating point cannot hold them. A float pipeline would make a shed's quantity depend on
// evaluation order and would round 0.1 kg steps into values a packer cannot weigh out.
//
// The legacy sheet rounds UP to the nearest 100 g, then converts to kg. Rounding UP rather than to
// nearest is the safe direction and is preserved deliberately: the error is at most 99 g of extra
// feed per line, whereas rounding down under-feeds. It is also what makes the number packable --
// 100 g is the smallest increment the packing scale is worked to.
//
// WHERE THE ROUNDING HAPPENS IS PART OF THE POLICY, not an implementation detail. It is applied
// PER SESSION, to the number that is actually packed and delivered, and the day's shed total is
// then defined as the SUM OF THE ROUNDED SESSIONS. The alternative -- round the daily total, then
// split it -- produces per-session numbers that are not multiples of 100 g (a rounded daily total
// split two ways lands on 50 g steps), so the packer could not weigh them and the sessions would
// not add back to the printed daily figure. Rounding per session keeps both invariants that
// matter: every delivered number is packable, and the sessions sum EXACTLY to the reported total.

// DefaultRoundingStepGrams is the legacy sheet's rule: round up to the nearest 100 g.
const DefaultRoundingStepGrams int64 = 100

// RoundingPolicy resolves the rounding increment for a feed item.
//
// It is a struct with an explicit per-item override map rather than a bare function because the
// legacy sheet special-cases BAKING SODA, and that special case is NOT reconstructable from the
// authored configuration currently in the database. Rather than invent a second rule and encode a
// guess as a feeding instruction, the general 100 g rule applies to every item today and this map
// is the named, tested seam the baking-soda rule drops into once its exact form is confirmed.
//
// TODO(feed-direction, maintainer decision needed): confirm the baking-soda rounding rule from the
// legacy sheet (candidates seen in the source: no rounding at all, or a 10 g step, because the
// per-head rate is an order of magnitude smaller than the other items and a 100 g step distorts
// it). Once confirmed, add it to PerItemStepGrams keyed by the feed_config_norm key -- no other
// code needs to change, and RoundingPolicyForItem's tests already cover the override path.
type RoundingPolicy struct {
	// DefaultStepGrams applies to every item without an override. Zero means "use
	// DefaultRoundingStepGrams".
	DefaultStepGrams int64
	// PerItemStepGrams overrides the step for specific items, keyed by NORMALIZED feed item key
	// (feed_config_norm form: lowercased, runs of whitespace/underscore/hyphen collapsed to '_').
	// Empty today -- see the TODO above.
	PerItemStepGrams map[string]int64
}

// StandardRoundingPolicy is the policy in force: the legacy 100 g rule for every feed item, with
// no per-item exception wired yet.
func StandardRoundingPolicy() RoundingPolicy {
	return RoundingPolicy{DefaultStepGrams: DefaultRoundingStepGrams}
}

// StepGramsFor returns the rounding increment for one feed item.
//
// A non-positive configured step is treated as "no rounding" (step 1 g) rather than as an error or
// as the default: a zero step would make the modulus undefined, and silently substituting 100 g
// for an author who explicitly asked for none would be the invented-default behaviour AGENTS.md
// bans. Step 1 g is the honest identity.
func (p RoundingPolicy) StepGramsFor(feedItemKey string) int64 {
	if step, ok := p.PerItemStepGrams[NormalizeConfigKey(feedItemKey)]; ok {
		if step <= 0 {
			return 1
		}
		return step
	}
	if p.DefaultStepGrams <= 0 {
		return DefaultRoundingStepGrams
	}
	return p.DefaultStepGrams
}

// RoundUpGrams rounds an exact gram quantity UP to the next whole multiple of step.
//
// Exactly-on-step values are left alone (900 g at a 100 g step stays 900 g, it does not jump to
// 1000). A zero quantity stays zero: an authored zero is a real "feed nothing" instruction, and
// rounding it up to a 100 g minimum would invent feed for milk-fed kids. Negative input is
// impossible from the authored schema (every rate CHECKs >= 0) and is floored at zero rather than
// rounded away from it.
func RoundUpGrams(grams *big.Rat, step int64) int64 {
	if grams == nil || grams.Sign() <= 0 {
		return 0
	}
	if step <= 0 {
		step = 1
	}
	// ceil(grams/step) * step, in exact integer arithmetic: num/(den*step) rounded up.
	num := new(big.Int).Set(grams.Num())
	den := new(big.Int).Mul(grams.Denom(), big.NewInt(step))
	quo, rem := new(big.Int).QuoRem(num, den, new(big.Int))
	if rem.Sign() != 0 {
		quo.Add(quo, big.NewInt(1))
	}
	return quo.Int64() * step
}

// GramsToKgString converts whole grams to a kg decimal string with three decimal places.
//
// Fixed scale, always three decimals, even for whole numbers ("2.000" not "2"). The output is a
// quantity an operator compares across rows and a client sums; ragged scale makes columns
// unreadable and invites a consumer to parse it as a float. Three decimals is exact here because
// grams are whole and 1 g = 0.001 kg.
func GramsToKgString(grams int64) string {
	neg := ""
	if grams < 0 {
		neg = "-"
		grams = -grams
	}
	return fmt.Sprintf("%s%d.%03d", neg, grams/1000, grams%1000)
}

// RoundSessionGramsToKg is the ONE place a computed quantity becomes a packable number: round up
// to the item's step, then convert to kg. Every quantity on a direction or packing row goes
// through this function, so the policy cannot drift between the two surfaces.
func RoundSessionGramsToKg(grams *big.Rat, feedItemKey string, policy RoundingPolicy) (int64, string) {
	rounded := RoundUpGrams(grams, policy.StepGramsFor(feedItemKey))
	return rounded, GramsToKgString(rounded)
}

// ---------------------------------------------------------------------------
// Key normalization
// ---------------------------------------------------------------------------

// NormalizeConfigKey is the Go twin of the database's feed_config_norm(text): trim, casefold, and
// collapse runs of whitespace/underscore/hyphen to a single underscore.
//
// It exists because the generator joins live free-text values (goats.management_stage,
// goats.breed) to authored labels IN MEMORY, after both sides have been read. The database
// function cannot do that join, so this must produce byte-identical output to it -- the live tags
// genuinely differ only cosmetically ('ICU- kid' vs 'ICU-Kid', 'F2- Male' vs 'F2-Male'), and a
// mismatch here does not fail loudly: it produces a BLOCKED row for a shed whose ration is in fact
// perfectly well authored, stalling that shed's feed sheet over a hyphen.
//
// Both sides of every comparison in this package go through this function. It is idempotent, so
// applying it to a value the database already normalized is a no-op rather than a second
// transform.
// It mirrors the SQL definition step for step:
//
//	lower(regexp_replace(btrim(value), '[\s_-]+', '_', 'g'))
//
// Two details of that expression are easy to get wrong and are both load-bearing:
//
//   - btrim(value) with no second argument strips SPACES ONLY, not tabs and not underscores or
//     hyphens. So "  padded  " normalizes to "padded", but "-leading-" normalizes to "_leading_" --
//     the edge separators survive the trim and are then collapsed into underscores by the regex.
//     A Go implementation that helpfully drops leading/trailing separators produces "leading" and
//     silently fails to match the stored key.
//   - the collapse applies to runs at the string EDGES as well as inside it, so a value that is
//     nothing but separators normalizes to a single "_", not to "".
//
// TestGoNormalizerMatchesDatabaseFeedConfigNormExactly compares the two implementations against a
// real database, including these edge shapes. That test caught this exact divergence.
func NormalizeConfigKey(value string) string {
	// btrim default: ASCII space only.
	trimmed := strings.Trim(value, " ")

	var b strings.Builder
	b.Grow(len(trimmed))
	pendingSep := false
	for _, r := range trimmed {
		if isConfigKeySeparator(r) {
			pendingSep = true
			continue
		}
		if pendingSep {
			b.WriteByte('_')
			pendingSep = false
		}
		if r >= 'A' && r <= 'Z' {
			b.WriteRune(r + ('a' - 'A'))
			continue
		}
		b.WriteRune(r)
	}
	// A trailing run collapses to one underscore, exactly as the regex does.
	if pendingSep {
		b.WriteByte('_')
	}
	return b.String()
}

// isConfigKeySeparator matches the SQL character class [\s_-]. \s in Postgres' regex engine is
// [ \t\n\r\f\v].
func isConfigKeySeparator(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '\f', '\v', '_', '-':
		return true
	default:
		return false
	}
}

// ParseDecimal converts an exact decimal string from the database into a *big.Rat.
//
// It returns ok=false rather than a zero value on failure. A rate that will not parse is a data
// problem, and defaulting it to 0 would be the silent-zero starvation path this whole module is
// built to prevent -- the caller turns !ok into a BLOCKED cell.
func ParseDecimal(raw string) (*big.Rat, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, false
	}
	r, ok := new(big.Rat).SetString(trimmed)
	if !ok {
		return nil, false
	}
	return r, true
}
