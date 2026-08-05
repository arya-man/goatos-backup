package domain

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Backend-composed display copy for one approval-queue row.
//
// GOLDEN FRONTEND RULE (AGENTS.md): a visible label is backend-owned; clients render it verbatim.
// This used to live in Kotlin -- the Android ViewModel read the echoed payload and built its own
// "12 animal(s) · to shed <uuid>" line -- which broke that rule twice over. It made the phone the
// author of business copy, and because the phone had no name source it printed the raw ids that
// admin-web had already gone to the trouble of resolving. Both surfaces now read ONE line composed
// here, so they cannot drift.
//
// COPY FIREWALL (AGENTS.md): nothing here may emit an id, a key name, or any implementation word.
// A fact with no resolvable name is DROPPED from the line rather than rendered as a code -- an
// approver reading "to shed 0b4e-91c2" learns nothing, and a farm-language line that is one clause
// shorter is strictly better than one carrying a UUID.

// ApprovalNameLookup resolves the ids on an approval payload to names. Both maps are id -> name; a
// missing id simply yields no name, which drops that clause from the composed line.
type ApprovalNameLookup struct {
	Locations map[string]string
	People    map[string]string
}

func (l ApprovalNameLookup) location(id string) string { return strings.TrimSpace(l.Locations[id]) }

// PersonName returns the display name for a user id, or "" when unknown.
func (l ApprovalNameLookup) PersonName(id string) string {
	return strings.TrimSpace(l.People[strings.TrimSpace(id)])
}

// ApprovalSummaryLine composes the one-line description of what a request contains.
//
// The payload is `additionalProperties: true` by contract, so it is treated as DATA to read, never
// as a shape to assert: every clause is optional and a missing key drops out silently. An unknown
// request type yields an empty line rather than a guess -- the type label above it still names the
// work, so a future request type stays visible in the queue even before this function knows it.
func ApprovalSummaryLine(requestType string, summary json.RawMessage, names ApprovalNameLookup) string {
	fields := decodeApprovalSummary(summary)
	if fields == nil {
		return ""
	}

	var parts []string
	add := func(s string) {
		if s = strings.TrimSpace(s); s != "" {
			parts = append(parts, s)
		}
	}

	switch requestType {
	case ApprovalRequestTypeBirth:
		if tag := fields.str("animal_identifier_1"); tag != "" {
			add("Tag " + tag)
		}
		add(fields.str("sex"))
		add(fields.str("breed"))
		if dob := fields.str("dob"); dob != "" {
			add("born " + dob)
		}
	case ApprovalRequestTypeDeath:
		// Deliberately NOT the goat_id: it is a UUID with no name source on this payload, and the
		// row already carries subject_goat_id for anything that needs identity. The approver is
		// deciding on the REASON, which is the fact worth the line.
		add(fields.str("reason"))
		add(fields.str("cause"))
	case ApprovalRequestTypeShifting:
		if n := fields.count("goat_ids"); n > 0 {
			add(strconv.Itoa(n) + " " + pluralAnimals(n))
		}
		from := names.location(fields.str("source_shed_id"))
		to := names.location(fields.str("destination_shed_id"))
		switch {
		case from != "" && to != "":
			add(from + " → " + to)
		case to != "":
			add("to " + to)
		case from != "":
			add("from " + from)
		}
		add(fields.str("category"))
	default:
		return ""
	}

	return strings.Join(parts, " · ")
}

func pluralAnimals(n int) string {
	if n == 1 {
		return "animal"
	}
	return "animals"
}

// approvalSummaryFields is the decoded payload as loose JSON values.
type approvalSummaryFields map[string]json.RawMessage

func decodeApprovalSummary(summary json.RawMessage) approvalSummaryFields {
	if len(summary) == 0 {
		return nil
	}
	var fields approvalSummaryFields
	// A payload that is not an object (or is malformed) yields no line. It is display copy, so a
	// decode failure must degrade the row, never fail the queue read.
	if err := json.Unmarshal(summary, &fields); err != nil {
		return nil
	}
	return fields
}

// str reads a string field. A JSON null, a non-string, or a blank value all read as absent, so the
// literal "null" can never reach the screen.
func (f approvalSummaryFields) str(key string) string {
	raw, ok := f[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

// count reads the length of an array field; anything else counts as zero.
func (f approvalSummaryFields) count(key string) int {
	raw, ok := f[key]
	if !ok {
		return 0
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return 0
	}
	return len(items)
}

// ApprovalSummaryLocationIDs returns the location ids a row's payload references, so the handler
// can resolve a whole page's names in one batched query instead of one lookup per row.
//
// Returned ids may be blank or duplicated across rows; the resolver dedupes.
func ApprovalSummaryLocationIDs(requestType string, summary json.RawMessage) []string {
	if requestType != ApprovalRequestTypeShifting {
		return nil
	}
	fields := decodeApprovalSummary(summary)
	if fields == nil {
		return nil
	}
	return []string{fields.str("source_shed_id"), fields.str("destination_shed_id")}
}
