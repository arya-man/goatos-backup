package domain

import "testing"

func TestAssignmentDriveEventIDRoundTripAndIgnoresDate(t *testing.T) {
	const assignmentID = "86000000-0000-4000-8000-00000000ee01"

	eventID := FormatAssignmentDriveEventID(assignmentID)
	if eventID != "vaccinationdrive:assignment:"+assignmentID {
		t.Fatalf("event id=%q", eventID)
	}
	parsed, err := ParseDriveEventID(eventID)
	if err != nil {
		t.Fatalf("ParseDriveEventID: %v", err)
	}
	if parsed.AssignmentID != assignmentID || parsed.BatchID != "" || parsed.DueDay != "" {
		t.Fatalf("parsed=%#v", parsed)
	}
}

func TestAssignmentDriveEventIDRejectsInvalidAssignment(t *testing.T) {
	if _, err := ParseDriveEventID("vaccinationdrive:assignment:not-a-uuid"); err == nil {
		t.Fatal("expected invalid assignment event id")
	}
}
