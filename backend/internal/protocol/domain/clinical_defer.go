package domain

import "strings"

// MandatoryClinicalDeferStates are the clinical safety blocks that a published
// vaccination protocol can never opt out of.
//
// Per docs/preventive-care-vaccination/vaccination-rules.md, an animal that is
// sick, under treatment, recovering, in quarantine, or in ICU is subject to
// "predefined postponement rules ... safety blocks, not optional planner
// choices." Its open vaccination work must be DEFERRED (held until the animal
// is healthy and reopened when the animal recovers), never cancelled or silently
// excluded.
//
// Authored rule_dsl.eligibility.defer_states may only ADD states to this set; it
// may never remove one of these. Both the publish-time validator
// (protocol/app) and the obligation generator (vaccination/app) resolve the
// effective clinical-defer set through the helpers below so the two layers can
// never diverge.
var MandatoryClinicalDeferStates = []string{"sick", "under_treatment", "recovering", "quarantine", "icu"}

// ExitLifecycleStates are terminal lifecycle states. An animal in any of these
// has permanently left the herd (or been merged) and is never eligible for, nor
// deferrable to, vaccination work — even if a stale clinical health signal is
// still present on the row. Distinct from the clinical hold states, which are
// "alive but temporarily blocked".
var ExitLifecycleStates = []string{"dead", "sold", "culled", "transferred", "lost", "merged", "inactive"}

// NormalizeDeferState lowercases and trims a single defer-state token. An empty
// or whitespace-only token normalizes to "".
func NormalizeDeferState(state string) string {
	return strings.ToLower(strings.TrimSpace(state))
}

// IsExitLifecycleState reports whether a lifecycle_status is a terminal exit
// state (dead/sold/culled/transferred/lost/merged/inactive). Case-insensitive.
func IsExitLifecycleState(status string) bool {
	n := NormalizeDeferState(status)
	if n == "" {
		return false
	}
	for _, s := range ExitLifecycleStates {
		if s == n {
			return true
		}
	}
	return false
}

// IsClinicalDeferState reports whether a health_status or lifecycle_status token
// is one of the mandatory clinical safety-hold states. Case-insensitive.
func IsClinicalDeferState(status string) bool {
	n := NormalizeDeferState(status)
	if n == "" {
		return false
	}
	for _, s := range MandatoryClinicalDeferStates {
		if s == n {
			return true
		}
	}
	return false
}

// NormalizeClinicalStageKey normalizes a free-text management_stage for comparison against the
// canonical clinical vocabulary: lowercased, trimmed, and inner whitespace collapsed to single
// underscores, so "Under Treatment", "under treatment", and "under_treatment" all match.
//
// STRICTER THAN NormalizeDeferState ON PURPOSE. That one only lowercases and trims, so it reports
// "Under Treatment" as NOT clinical -- fine for the health_status ENUM tokens it was written for,
// wrong for an operator-facing management_stage that may carry a space. A management_stage must use
// this one.
func NormalizeClinicalStageKey(stage string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(stage))), "_")
}

// IsClinicalManagementStage reports whether a management_stage names one of the mandatory clinical
// safety-hold states (sick, under_treatment, recovering, quarantine, icu).
//
// This is the ONE implementation of that question for management_stage values; identity's relocation
// guard and the counts shifting resolver both call it, so a movement can never disagree with itself
// about whether a destination tag is clinical.
//
// It matches the STATE only. 'ICU-Kid' and 'Quarantine kids' are PEN names, not states -- their keys
// are "icu-kid" and "quarantine_kids", neither of which is in the set -- so they stay writable, which
// is exactly what migration 000167 decided. A movement may say which pen an animal is in; it may
// never say she is sick.
func IsClinicalManagementStage(stage string) bool {
	key := NormalizeClinicalStageKey(stage)
	if key == "" {
		return false
	}
	for _, clinical := range MandatoryClinicalDeferStates {
		if key == NormalizeClinicalStageKey(clinical) {
			return true
		}
	}
	return false
}

// MissingMandatoryClinicalDeferStates returns the mandatory clinical safety
// states that are absent from present, in canonical order. An empty result
// means present already covers every mandatory clinical block.
//
// Callers distinguish "author supplied nothing" (an absent defer_states key,
// where the engine safe-default applies) from "author supplied a partial list"
// (a present-but-incomplete list, which is an unsafe authored payload and must
// be rejected at publish time).
func MissingMandatoryClinicalDeferStates(present []string) []string {
	have := make(map[string]bool, len(present))
	for _, state := range present {
		if n := NormalizeDeferState(state); n != "" {
			have[n] = true
		}
	}
	missing := make([]string, 0, len(MandatoryClinicalDeferStates))
	for _, state := range MandatoryClinicalDeferStates {
		if !have[state] {
			missing = append(missing, state)
		}
	}
	return missing
}

// EffectiveClinicalDeferStates returns the union of present and every mandatory
// clinical safety state, normalized and de-duplicated, in a stable order
// (mandatory states first in canonical order, then any additional authored
// states in input order). It guarantees the runtime always treats the mandatory
// clinical states as deferred even for a protocol version that was published
// before the publish-time guard existed.
func EffectiveClinicalDeferStates(present []string) []string {
	seen := make(map[string]bool, len(present)+len(MandatoryClinicalDeferStates))
	out := make([]string, 0, len(present)+len(MandatoryClinicalDeferStates))
	for _, state := range MandatoryClinicalDeferStates {
		if !seen[state] {
			seen[state] = true
			out = append(out, state)
		}
	}
	for _, state := range present {
		if n := NormalizeDeferState(state); n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

// IsClinicalStage reports whether a stage code names one of the mandatory clinical states.
//
// It exists so a CALLER offering the stage vocabulary as a picker can drop the clinical tags
// (ICU, Quarantine) rather than offering them and letting the write reject them. The comparison is
// normalized the same way the identity module's destination-tag check normalizes it -- lowercased
// with separators stripped -- so "Under Treatment", "under_treatment" and "under-treatment" all
// resolve to the same state.
//
// Reused, never re-hardcoded: the clinical-defer safety rule exists because a second copy of this
// set is a second place to forget one.
func IsClinicalStage(stage string) bool {
	key := clinicalKey(stage)
	if key == "" {
		return false
	}
	for _, clinical := range MandatoryClinicalDeferStates {
		if key == clinicalKey(clinical) {
			return true
		}
	}
	return false
}

func clinicalKey(stage string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(stage)) {
		if r >= 'a' && r <= 'z' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
