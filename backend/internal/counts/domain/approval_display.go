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

// ApprovalNameLookup resolves the ids on an approval payload to names. All maps are id -> name; a
// missing id simply yields no name, which drops that clause from the composed line.
type ApprovalNameLookup struct {
	Locations map[string]string
	People    map[string]string
	// AnimalLocations is subject_goat_id -> that animal's CURRENT operational location, already
	// composed to "<park>, <shed display>" (park+bare shed, or park+"<shed> <partition>" /
	// "<shed> - Part N" when the animal sits in a real partition -- see internal/platform/oploc).
	// Resolved at READ time from the animal's live goats/goat_shed_partitions row: a death is
	// terminal and ExitGoat never touches goats.shed_id, so the animal keeps the shed/partition it
	// died in and no location needs to be snapshotted onto the death payload at raise time.
	//
	// That is a VERIFIED claim, not an assumption -- if a dead animal could be moved, this line
	// would name a place it was never in. Exactly two paths write goats.shed_id:
	//   1. identity/adapters/postgres/goat_relocate.go -- the shifting relocate. Its callers run
	//      counts/app.validateShiftableGoatFacts, which REJECTS any goat with exited_at set or
	//      lifecycle_status != 'alive', so a dead animal cannot be relocated.
	//   2. procurement/adapters/postgres/repository.go -- sets lifecycle_status='alive' in the same
	//      statement, i.e. intake, never a post-mortem move.
	// The guarantee is enforced in the application layer, not by a database constraint, so a raw
	// SQL backfill could still violate it. If a third writer of goats.shed_id ever appears, it must
	// either exclude dead animals or this resolution must move to a raise-time snapshot.
	AnimalLocations map[string]string
}

func (l ApprovalNameLookup) location(id string) string { return strings.TrimSpace(l.Locations[id]) }

// AnimalLocation returns the composed operational-location display for a goat id, or "" when the
// animal has no resolvable park/shed (matches the drop-not-guess rule every other clause follows).
// Exported so the HTTP handler can also expose it as its own field (SubjectAnimalLocation)
// alongside the composed summary line -- see approval_handler.go.
func (l ApprovalNameLookup) AnimalLocation(id string) string {
	return strings.TrimSpace(l.AnimalLocations[strings.TrimSpace(id)])
}

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
//
// subjectGoatID is the row's ApprovalRequest.SubjectGoatID (blank for birth, which has no single
// subject animal). It is not part of the JSON summary payload -- see ApprovalSummaryGoatIDs.
func ApprovalSummaryLine(requestType string, summary json.RawMessage, subjectGoatID string, names ApprovalNameLookup) string {
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
		// Deliberately NOT the goat_id itself: it is a UUID with no name source. The approver is
		// deciding on the REASON, which is the leading fact -- but they were reporting a real gap
		// without a location too: "Death ke kuda current sheds sariga levu" (shed/park is missing
		// for death rows just like it used to be missing everywhere else). Give it the same
		// treatment shifting already gets, resolved from the animal's current location.
		add(fields.str("reason"))
		add(fields.str("cause"))
		add(names.AnimalLocation(subjectGoatID))
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

// ApprovalSummaryGoatIDs returns the goat id a row's location clause needs, so the handler can
// resolve a whole page's animal locations in one batched query instead of one lookup per row --
// same shape as ApprovalSummaryLocationIDs, keyed off the row's SubjectGoatID rather than the JSON
// payload (a death payload never carries the goat id itself, per ApprovalSummaryLine above).
//
// Returned ids may be blank; the resolver dedupes and drops blanks.
func ApprovalSummaryGoatIDs(requestType string, subjectGoatID string) []string {
	if requestType != ApprovalRequestTypeDeath {
		return nil
	}
	if strings.TrimSpace(subjectGoatID) == "" {
		return nil
	}
	return []string{subjectGoatID}
}
