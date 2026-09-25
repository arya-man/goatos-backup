package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

// The headline requirement: NO id ever reaches an approver's screen. Every case below either
// renders a name or drops the clause -- it never falls back to the raw value.
func TestApprovalSummaryLineNeverRendersAnID(t *testing.T) {
	const shedID = "0b4e91c2-4d18-4a2b-9f31-2c7d5e8a1b40"
	const parkID = "7f3a91c2-4d18-4a2b-9f31-2c7d5e8a1b41"

	const goatID = "5c1e91c2-4d18-4a2b-9f31-2c7d5e8a1b42"
	const farmID = "9d2e91c2-4d18-4a2b-9f31-2c7d5e8a1b43"

	cases := []struct {
		name          string
		requestType   string
		summary       string
		subjectGoatID string
		names         ApprovalNameLookup
		want          string
	}{
		{
			name:        "shifting with both shed names resolved",
			requestType: ApprovalRequestTypeShifting,
			summary: `{"goat_ids":["a","b","c"],"source_shed_id":"` + parkID +
				`","destination_shed_id":"` + shedID + `","category":"growth"}`,
			names: ApprovalNameLookup{Locations: map[string]string{
				parkID: "Gandhi 1",
				shedID: "Gandhi 2",
			}},
			want: "3 animals · Gandhi 1 → Gandhi 2 · Growth move",
		},
		{
			// 2026-09-25: the raw category ("normal") reached the line, and two parks both have a
			// "Castro 1" -- the farm name now leads the pens.
			name:        "shifting names the farm and a human category label",
			requestType: ApprovalRequestTypeShifting,
			summary: `{"goat_ids":["a","b"],"destination_park_id":"` + farmID + `","source_shed_id":"` + parkID +
				`","source_partition_label":"2","destination_shed_id":"` + shedID + `","destination_partition_label":"3","category":"normal"}`,
			names: ApprovalNameLookup{Locations: map[string]string{
				farmID: "Coimbatore",
				parkID: "Castro",
				shedID: "Castro",
			}},
			want: "2 animals · Coimbatore · Castro 2 → Castro 3 · Normal move",
		},
		{
			name:        "an unknown shifting category is dropped, never rendered raw",
			requestType: ApprovalRequestTypeShifting,
			summary:     `{"goat_ids":["a"],"category":"some_future_type"}`,
			want:        "1 animal",
		},
		{
			// The approver must know WHICH animal died: its real RFID tag leads the line.
			name:          "death leads with the animal's tag",
			requestType:   ApprovalRequestTypeDeath,
			summary:       `{"reason":"Found dead in shed"}`,
			subjectGoatID: goatID,
			names: ApprovalNameLookup{
				AnimalLocations: map[string]string{goatID: "CBE, Castro 2"},
				AnimalTags:      map[string]string{goatID: "982000123456789"},
			},
			want: "Tag 982000123456789 · Found dead in shed · CBE, Castro 2",
		},
		{
			// The regression this whole change exists for. With no name available the clause is
			// DROPPED; the old Kotlin composer printed "to shed 0b4e91c2-...".
			name:        "shifting with an unresolvable shed drops the clause, never prints the id",
			requestType: ApprovalRequestTypeShifting,
			summary:     `{"goat_ids":["a"],"destination_shed_id":"` + shedID + `","category":"spacing"}`,
			names:       ApprovalNameLookup{Locations: map[string]string{}},
			want:        "1 animal · Spacing move",
		},
		{
			name:        "shifting names only the destination when the source is absent",
			requestType: ApprovalRequestTypeShifting,
			summary:     `{"goat_ids":["a","b"],"destination_shed_id":"` + shedID + `"}`,
			names:       ApprovalNameLookup{Locations: map[string]string{shedID: "Yashoda 3"}},
			want:        "2 animals · to Yashoda 3",
		},
		{
			name:        "birth",
			requestType: ApprovalRequestTypeBirth,
			summary:     `{"animal_identifier_1":"981098102345678","sex":"Female","breed":"Osmanabadi","dob":"5 Aug 2026"}`,
			want:        "Tag 981098102345678 · Female · Osmanabadi · born 5 Aug 2026",
		},
		{
			// A birth payload stores dob as an ISO date; the approver reads the farm's dd/mm/yyyy.
			name:        "birth renders an ISO date of birth the farm way",
			requestType: ApprovalRequestTypeBirth,
			summary:     `{"sex":"Male","dob":"2026-09-17"}`,
			want:        "Male · born 17/09/2026",
		},
		{
			// A death payload's goat_id is a UUID with no name source, so it is deliberately not
			// in the line at all -- the approver is deciding on the reason.
			name:        "death renders the reason and never the goat id",
			requestType: ApprovalRequestTypeDeath,
			summary:     `{"goat_id":"` + shedID + `","reason":"Found dead in shed"}`,
			want:        "Found dead in shed",
		},
		{
			// The gap this change closes: a death row must carry the animal's location, resolved
			// to names, exactly like shifting already does.
			name:          "death names the animal's park and shed",
			requestType:   ApprovalRequestTypeDeath,
			summary:       `{"reason":"Found dead in shed"}`,
			subjectGoatID: goatID,
			names: ApprovalNameLookup{AnimalLocations: map[string]string{
				goatID: "CBE, Castro 2",
			}},
			want: "Found dead in shed · CBE, Castro 2",
		},
		{
			// A non-partitioned shed renders bare -- never "Yashoda whole" -- and this function
			// trusts whatever the resolver already composed, so the case is exercised end to end
			// through the postgres adapter's own tests; here it just proves the clause passes
			// through untouched.
			name:          "death names a non-partitioned shed bare",
			requestType:   ApprovalRequestTypeDeath,
			summary:       `{"reason":"Old age"}`,
			subjectGoatID: goatID,
			names: ApprovalNameLookup{AnimalLocations: map[string]string{
				goatID: "CPT, Yashoda",
			}},
			want: "Old age · CPT, Yashoda",
		},
		{
			// The animal's location cannot be resolved (no roster row, or the goat id is blank) --
			// the clause drops rather than rendering the raw id or an empty fragment.
			name:          "death drops the location clause when it cannot be resolved",
			requestType:   ApprovalRequestTypeDeath,
			summary:       `{"reason":"Found dead in shed"}`,
			subjectGoatID: goatID,
			names:         ApprovalNameLookup{AnimalLocations: map[string]string{}},
			want:          "Found dead in shed",
		},
		{
			name:        "a JSON null string field is absent, never the literal null",
			requestType: ApprovalRequestTypeBirth,
			summary:     `{"animal_identifier_1":"777","sex":null,"breed":"Osmanabadi"}`,
			want:        "Tag 777 · Osmanabadi",
		},
		{
			name:        "a payload with nothing nameable yields an empty line",
			requestType: ApprovalRequestTypeShifting,
			summary:     `{}`,
			want:        "",
		},
		{
			// Display copy must degrade, never explode: a malformed or non-object payload yields
			// no line and the row still renders behind its type label.
			name:        "a malformed payload yields an empty line",
			requestType: ApprovalRequestTypeBirth,
			summary:     `"not an object"`,
			want:        "",
		},
		{
			// A future request type stays VISIBLE in the queue (its type label still renders);
			// this function simply declines to guess at its payload.
			name:        "an unknown request type yields an empty line rather than a guess",
			requestType: "quarantine",
			summary:     `{"animal_identifier_1":"777"}`,
			want:        "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ApprovalSummaryLine(tc.requestType, json.RawMessage(tc.summary), tc.subjectGoatID, tc.names)
			if got != tc.want {
				t.Fatalf("summary line = %q, want %q", got, tc.want)
			}
			// Belt and braces on the headline rule: whatever the branch, no uuid escapes.
			for _, id := range []string{shedID, parkID, goatID} {
				if strings.Contains(got, id) {
					t.Fatalf("summary line %q leaked the id %q to an approver's screen", got, id)
				}
			}
		})
	}
}

// The handler harvests the subject goat id per row and resolves the whole page's animal locations
// in one batched call -- same shape as ApprovalSummaryLocationIDs above, and for the same reason:
// a per-row lookup on a 20-row page is the banned N+1 fan-out.
func TestApprovalSummaryGoatIDs(t *testing.T) {
	const goatID = "5c1e91c2-4d18-4a2b-9f31-2c7d5e8a1b42"

	if got := ApprovalSummaryGoatIDs(ApprovalRequestTypeDeath, goatID); len(got) != 1 || got[0] != goatID {
		t.Fatalf("death goat ids = %v, want [%s]", got, goatID)
	}
	if got := ApprovalSummaryGoatIDs(ApprovalRequestTypeDeath, "  "); len(got) != 0 {
		t.Fatalf("blank subject goat id contributed %v; want none", got)
	}
	// Birth and shifting name no single subject animal, so they must contribute nothing to the
	// batch even when a goat id is supplied.
	for _, requestType := range []string{ApprovalRequestTypeBirth, ApprovalRequestTypeShifting} {
		if got := ApprovalSummaryGoatIDs(requestType, goatID); len(got) != 0 {
			t.Fatalf("%s contributed goat ids %v; want none", requestType, got)
		}
	}
}

// The handler harvests ids per row and resolves the page in one batched call, so this must report
// every location a row references -- and nothing for the types that reference none.
func TestApprovalSummaryLocationIDs(t *testing.T) {
	got := ApprovalSummaryLocationIDs(ApprovalRequestTypeShifting,
		json.RawMessage(`{"source_shed_id":"src","destination_shed_id":"dst","destination_park_id":"park"}`))
	if len(got) != 4 || got[0] != "src" || got[1] != "dst" || got[2] != "park" || got[3] != "" {
		t.Fatalf("shifting location ids = %v, want [src dst park \"\"] (the park names the farm; blanks are deduped by the resolver)", got)
	}

	// Birth and death name no shed, so they must contribute nothing to the batch.
	for _, requestType := range []string{ApprovalRequestTypeBirth, ApprovalRequestTypeDeath} {
		if got := ApprovalSummaryLocationIDs(requestType, json.RawMessage(`{"reason":"x"}`)); len(got) != 0 {
			t.Fatalf("%s contributed location ids %v; want none", requestType, got)
		}
	}
}

func TestApprovalNameLookupPersonName(t *testing.T) {
	names := ApprovalNameLookup{People: map[string]string{"u-1": "Sagar Mahoor", "u-2": "   "}}
	if got := names.PersonName("u-1"); got != "Sagar Mahoor" {
		t.Fatalf("PersonName = %q, want %q", got, "Sagar Mahoor")
	}
	// A blank stored name is treated as absent so the caller omits the line instead of rendering
	// "Raised by" followed by nothing.
	if got := names.PersonName("u-2"); got != "" {
		t.Fatalf("blank display name should read as absent, got %q", got)
	}
	if got := names.PersonName("nobody"); got != "" {
		t.Fatalf("unknown user should read as absent, got %q", got)
	}
}
