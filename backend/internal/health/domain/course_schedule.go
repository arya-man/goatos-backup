package domain

import (
	"sort"
	"strings"

	"github.com/vgoats/goatos/backend/internal/health/diagnosis"
)

// This file is the bridge between a confirmed DIAGNOSIS and the treatment COURSE
// the operator works. It is deliberately pure: no database, no clock, no I/O, so
// the two rules below can be proven without a Postgres round trip.
//
// Maintainer decision 2026-08-14 (F): HOUSING owns the visit cadence, the
// protocol grid owns the content of a visit. Before this, the authored
// day x session grid decided both. The clinical contract is explicit --
// "follow-up by housing, not a 6-hour timer" -- and the two rules disagree
// whenever a card is authored for an afternoon visit that the animal's housing
// does not include.

// Exit types, snapshotted onto a case from the register rule that produced it.
//
//	F           closes when the minimum days have elapsed AND exit criteria are met
//	T           closes on a TEST result (CMT negative twice, FAMACHA 1-2)
//	V           closes when the Director looks; no day count, ever
//	Supportive  daily and ongoing, until the Director clears the animal
const (
	ExitTypeFixed      = "F"
	ExitTypeTest       = "T"
	ExitTypeDirector   = "V"
	ExitTypeSupportive = "Supportive"
)

// ValidExitType reports whether v is one of the four closure models. Anything
// else must be rejected rather than defaulted: guessing a closure model decides
// when an animal stops being treated.
func ValidExitType(v string) bool {
	switch v {
	case ExitTypeFixed, ExitTypeTest, ExitTypeDirector, ExitTypeSupportive:
		return true
	default:
		return false
	}
}

// ClosesOnDayCount reports whether this exit type carries a duration. Only Type F
// does; the other three close on a test, a review, or the Director clearing
// containment, and giving them a day count invents an end date nobody authored.
func ClosesOnDayCount(exitType string) bool { return exitType == ExitTypeFixed }

// SessionsForHousing returns the visits one business day earns under a housing
// directive, in the order they happen.
//
// The two working shifts are 06:00-15:00 and 15:00-24:00. The farm is empty
// 00:00-06:00, so nothing is ever scheduled into that window.
//
//	ICU                      both shifts
//	ICU + quarantine         both shifts
//	ward                     morning only
//	quarantine, not ICU      morning only -- a standing, eating animal is seen once
//	field                    no daily cycle at all
//
// Ward being once a day is a deliberate load decision, not an oversight: putting
// every 104F fever into the evening round as well drowns the shift, and a
// drowned shift stops reading its list.
func SessionsForHousing(acuity, containment string) []string {
	switch {
	case acuity == diagnosis.AcuityICU:
		return []string{SessionMorning, SessionEvening}
	case acuity == diagnosis.AcuityWard:
		return []string{SessionMorning}
	case containment == diagnosis.ContainmentQuarantine:
		return []string{SessionMorning}
	default:
		// field and home: treated in place, no daily cycle.
		return nil
	}
}

// ScheduledSession is one visit on one day, carrying the steps to perform.
type ScheduledSession struct {
	DayNo   int
	Session string
	Steps   []ProtocolStep
}

// ScheduleCourse turns an authored treatment card into the visits it actually needs.
//
// THE CARD DECIDES HOW OFTEN THE ANIMAL IS SEEN (maintainer decision 2026-09-23). A card that
// authors a morning, an afternoon and an evening dose earns THREE visits that day, and the
// operator goes three times. The farm has the operators for it, and the alternative is what this
// replaces: every one of that day's doses piled onto one morning card, so an evening dose was
// handed to someone at 07:00 with nothing saying it was meant for 19:00.
//
// HOUSING IS A FLOOR, NOT A CEILING. An ICU animal is seen morning and evening whatever its card
// authors, because being in ICU is itself a reason to look at it. So a day's visits are the
// housing sessions UNION the sessions its own steps name -- never fewer than the clinical
// minimum, never fewer than the card asks for.
//
// `unscheduled` steps name no time, so they roll into the EARLIEST visit of their day: a medicine
// with no hour on it is given at the first chance, not dropped and not deferred.
//
// A day with no authored step still earns its housing visits, so the animal is still seen. If
// housing asks for no daily cycle at all -- treated in place at home, or out in the field -- then
// a day whose card authors nothing earns no visit, which is the behaviour that shape always had.
//
// days bounds how many days are snapshotted; steps authored beyond it are ignored.
func ScheduleCourse(steps []ProtocolStep, days int, sessions []string) []ScheduledSession {
	if days <= 0 || len(sessions) == 0 {
		// No daily cycle at all -- treated in place at home, or out in the field. The card's
		// steps earn no visit, which is the behaviour that shape has always had and is NOT what
		// the 2026-09-23 decision changed: that one is about a card that authors an evening dose
		// for an animal the farm already visits.
		return nil
	}

	// bucket[day][session] preserves authored order within a visit, which is the order the
	// operator performs them in.
	bucket := make(map[int]map[string][]ProtocolStep, days)
	// authored[day] is the set of sessions that day's own steps name.
	authored := make(map[int]map[string]bool, days)
	for _, step := range steps {
		if step.DayNo < 1 || step.DayNo > days {
			continue
		}
		target := normalizeCourseSession(step.Session)
		if bucket[step.DayNo] == nil {
			bucket[step.DayNo] = map[string][]ProtocolStep{}
			authored[step.DayNo] = map[string]bool{}
		}
		if target != SessionUnscheduled {
			authored[step.DayNo][target] = true
		}
		bucket[step.DayNo][target] = append(bucket[step.DayNo][target], step)
	}

	out := make([]ScheduledSession, 0, days*maxSessionsPerDay)
	for day := 1; day <= days; day++ {
		visits := visitsForDay(sessions, authored[day])
		if len(visits) == 0 {
			continue
		}
		earliest := visits[0]
		for _, session := range visits {
			visit := ScheduledSession{DayNo: day, Session: session}
			if bucket[day] != nil {
				visit.Steps = append(visit.Steps, bucket[day][session]...)
				// A step with no hour on it joins the first visit of its day.
				if session == earliest {
					visit.Steps = append(visit.Steps, bucket[day][SessionUnscheduled]...)
				}
			}
			out = append(out, visit)
		}
	}
	return out
}

// visitsForDay is the housing floor UNION what this day's card authored, in the order the visits
// happen. Sorting by the working day rather than by either input keeps the operator's list in
// clock order however the two were combined.
func visitsForDay(housing []string, authored map[string]bool) []string {
	seen := map[string]bool{}
	for _, s := range housing {
		seen[s] = true
	}
	for s := range authored {
		seen[s] = true
	}
	out := make([]string, 0, len(seen))
	for _, s := range orderedSessions {
		if seen[s] {
			out = append(out, s)
		}
	}
	return out
}

// orderedSessions is the working day in clock order. `unscheduled` is deliberately absent: it is
// not a time of day, it is the absence of one, and it never becomes a visit of its own.
var orderedSessions = []string{SessionMorning, SessionAfternoon, SessionEvening}

const maxSessionsPerDay = 3

func normalizeCourseSession(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case SessionMorning:
		return SessionMorning
	case SessionAfternoon:
		return SessionAfternoon
	case SessionEvening:
		return SessionEvening
	default:
		return SessionUnscheduled
	}
}

// sopRefAliases maps the register's display-form sop_ref onto the protocol
// disease_key it is authored under, for the cases where the two genuinely differ.
//
// These are not typos to be normalised away -- the register and the treatment
// cards were written by different people at different times, and "Bloat" and
// "bloating" are the same illness under two names. Every entry here is a real
// naming divergence; everything else falls through to the generic
// lowercase-and-underscore rule below.
var sopRefAliases = map[string]string{
	"bloat":     "bloating",
	"flystrike": "fly_strike",
	"preg tox":  "pregnancy_toxemia",
	"skin":      "skin_infections",
	"heat":      "heat_stress",
}

// SOPRefToDiseaseKey resolves a register sop_ref to the disease_key a published
// treatment card is stored under.
//
// It returns "" for the sentinel "Field", which is the sop_ref the four field
// actions carry: a field action is treated in place and once, so it has no card
// and must never resolve to one.
//
// A resolved key that has no published card is NOT this function's problem to
// hide -- the caller must fail closed and name the missing card. Nine of the
// register's thirty diagnoses currently point at cards nobody has authored, and
// opening a course with no treatment in it would be worse than refusing.
func SOPRefToDiseaseKey(sopRef string) string {
	key := strings.ToLower(strings.TrimSpace(sopRef))
	if key == "" || key == "field" {
		return ""
	}
	if alias, ok := sopRefAliases[key]; ok {
		return alias
	}
	return strings.ReplaceAll(key, " ", "_")
}

// SortedProblemKeys returns the proposal's problems in a stable order for
// persistence. The engine ranks them by severity for DISPLAY; storage sorts
// alphabetically so a replay writes an identical row.
func SortedProblemKeys(problems []string) []string {
	out := append([]string{}, problems...)
	sort.Strings(out)
	return out
}
