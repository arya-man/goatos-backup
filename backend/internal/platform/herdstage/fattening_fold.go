// Package herdstage owns the ONE rule about which stored management_stage values a filter option
// stands for. It lives outside every module because two screens already ask the question -- Counts
// Breakdown and Sales > Farm born -- and a second copy of a vocabulary rule drifts: the day the
// farm retires a tag, one page would keep offering it.
package herdstage

import "strings"

// The FATTENING STAGE FOLD (maintainer decision 2026-09-22).
//
// The farm stores three management_stage values for one cohort: `F2`, `F2-Male` and `F2-Female`.
// On a page that ALREADY carries a gender filter -- Counts Breakdown and Sales > Farm born -- the
// sexed pair
// makes the stage list answer a question the gender filter answers better, and a reader who wants
// fattening males has to know to tick BOTH `F2-Male` and (for animals recorded on the unsexed tag)
// `F2`, or silently read a short count.
//
// So the FILTER shows one option, `F2`, and selecting it matches all three stored values.
//
// THE FOLD IS FILTER-OPTIONS-ONLY, and that boundary is the whole decision. No animal's stored
// stage changes, the detail table still prints the value each row actually carries, and the stage
// chart still draws its three bars -- a reader who wants to know how the cohort splits by sex can
// still see it. Widening this into a label rewrite or a re-tag is a MAINTAINER decision: the two
// sexed tags are read by the sales fattening formula, the stage age-band map (migration 000109
// files all three as `kid`), the feed ration grid and shed profile cohorts.
const FatteningKey = "F2"

// fatteningStageMembers are the stored values FatteningKey stands for, and the ONE list in the
// repository that says so. FatteningKey is itself a member: an animal recorded on the bare tag
// must not drop out of its own option.
var fatteningStageMembers = []string{"F2", "F2-Male", "F2-Female"}

// LowerMembers is the vocabulary a SQL predicate binds, lower-cased to match a
// lower(btrim(...)) comparison. Handing SQL the list keeps this package the only place the
// membership is written down -- a CASE expression spelling the three values out would be a second
// copy, free to drift the day the farm retires a tag.
func LowerMembers() []string {
	out := make([]string, 0, len(fatteningStageMembers))
	for _, member := range fatteningStageMembers {
		out = append(out, strings.ToLower(member))
	}
	return out
}

// IsFattening reports whether a stored stage belongs to the fattening family. It compares
// case-insensitively and ignores surrounding blanks -- the column has no CHECK constraint and the
// importer writes the source sheet cell verbatim -- and matches the WHOLE code, never an "F2"
// prefix, which would also claim a future "F2X" the farm might mean as something else entirely.
//
// Farm born keys its stage options on the lower-cased value while Counts Breakdown keys on the
// stored one, so a rule about which animals are one cohort must not depend on which screen asks.
func IsFattening(stage string) bool {
	trimmed := strings.TrimSpace(stage)
	for _, member := range fatteningStageMembers {
		if strings.EqualFold(trimmed, member) {
			return true
		}
	}
	return false
}

// ExpandFilter turns the keys a client selected into the stored values to match on. Only
// FatteningKey expands; everything else passes through untouched and in order, and an empty
// selection stays empty so "no filter" keeps meaning no filter.
//
// A caller that sends a folded-away value directly (`F2-Male`, from a bookmarked URL or an older
// client) is honoured as itself rather than widened to the whole cohort -- it names exactly one
// stored value, and quietly returning two more animals than were asked for would be worse than
// serving a filter the current list no longer offers.
func ExpandFilter(selected []string) []string {
	if len(selected) == 0 {
		return selected
	}
	out := make([]string, 0, len(selected)+len(fatteningStageMembers))
	seen := make(map[string]struct{}, len(selected)+len(fatteningStageMembers))
	add := func(value string) {
		if _, dup := seen[value]; dup {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for _, key := range selected {
		if strings.EqualFold(strings.TrimSpace(key), FatteningKey) {
			for _, member := range fatteningStageMembers {
				add(member)
			}
			continue
		}
		add(key)
	}
	return out
}

// DisplayLabel is the stage label a reader sees, given a stage code and the name the tenant
// configured for it in animal_stage_lookup.
//
// THE CODE IS STILL THE VALUE (maintainer decision 2026-09-10). Nothing about the stored stage
// changes: the write path stores the code, the filter sends the code, and the census editor's
// value is the code. This decides one thing only -- the words a reader is shown.
//
// ONLY THE FATTENING FAMILY IS RENAMED, and that narrowness is the decision rather than an
// oversight. The lookup carries a name for every code, so rendering names throughout would also
// turn K0/K1/K2/K3 into Newborn / Milk training / Milk drinking / Weaned kids and M0 into Mother
// newborn -- stages the farm reads, says and writes on its own sheets BY THEIR CODE. "F2" is the
// one that says nothing to a reader, and the maintainer asked for that one.
//
// The WORDS are business-managed: they come from animal_stage_lookup.name, so renaming Fattening
// is a data edit and never a deploy. Only the CHOICE of which codes show their name lives here.
// An F2 stage with no configured name falls back to its code rather than to an invented word.
func DisplayLabel(stageCode, configuredName string) string {
	if !IsFattening(stageCode) {
		return stageCode
	}
	name := strings.TrimSpace(configuredName)
	if name == "" {
		return stageCode
	}
	return name
}
