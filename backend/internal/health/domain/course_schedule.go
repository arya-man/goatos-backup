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

// ScheduleCourse maps authored protocol steps onto housing-driven visits.
//
// A step authored for a session the housing directive INCLUDES is performed at
// that visit. A step authored for any other session -- an afternoon step on a
// ward animal seen only in the morning, or an `unscheduled` step -- rolls into
// the EARLIEST visit of its day.
//
// Rolling forward rather than dropping is the safe direction: a dropped step is
// a medicine silently not given, while a step moved earlier in the same day is
// still given on the day it was authored for. Rolling into the earliest visit
// rather than the nearest one keeps a day's work front-loaded, which is what an
// operator walking a shift list needs.
//
// days bounds how many days of steps are snapshotted. Steps authored beyond it
// are ignored, and a day inside it with no authored step still yields its visits
// so the animal is seen.
func ScheduleCourse(steps []ProtocolStep, days int, sessions []string) []ScheduledSession {
	if days <= 0 || len(sessions) == 0 {
		return nil
	}
	allowed := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		allowed[s] = true
	}
	earliest := sessions[0]

	// bucket[day][session] preserves authored order within a visit, which is the
	// order the operator performs them in.
	bucket := make(map[int]map[string][]ProtocolStep, days)
	for _, step := range steps {
		if step.DayNo < 1 || step.DayNo > days {
			continue
		}
		target := normalizeCourseSession(step.Session)
		if !allowed[target] {
			target = earliest
		}
		if bucket[step.DayNo] == nil {
			bucket[step.DayNo] = map[string][]ProtocolStep{}
		}
		bucket[step.DayNo][target] = append(bucket[step.DayNo][target], step)
	}

	out := make([]ScheduledSession, 0, days*len(sessions))
	for day := 1; day <= days; day++ {
		for _, session := range sessions {
			visit := ScheduledSession{DayNo: day, Session: session}
			if bucket[day] != nil {
				visit.Steps = bucket[day][session]
			}
			out = append(out, visit)
		}
	}
	return out
}

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
