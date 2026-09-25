package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// The Full Schedule drew "Gandhi 84" and "Yashoda 2" (for 2 animals in Yashoda 3) because the drive
// assignment rows carried no composed pen name and the page composed its own. The contract has always
// declared partition_label + operational_location_display on this row; the service now fills them
// through oploc, and a whole pen carries no partition and no "whole".
func TestDriveAssignmentRowsCarryThePenIdentity(t *testing.T) {
	t.Parallel()
	svc := NewService(fakeRepo{driveRows: []domain.DriveAssignmentRow{
		{PhysicalShed: "Gandhi", PartitionLabel: "3", Animals: 42},
		{PhysicalShed: "Godel 1", PartitionLabel: "Part 2", Animals: 30},
		{PhysicalShed: "Yashoda", PartitionLabel: "3", Animals: 2},
		{PhysicalShed: "Ho Chi Minh 1", PartitionLabel: "whole", Animals: 17},
	}})
	resp, err := svc.DriveAssignments(context.Background(), domain.DriveAssignmentQuery{TenantID: "tenant"})
	if err != nil {
		t.Fatalf("DriveAssignments: %v", err)
	}
	want := []struct{ display, partition string }{
		{"Gandhi 3", "3"}, {"Godel 1 - Part 2", "Part 2"}, {"Yashoda 3", "3"}, {"Ho Chi Minh 1", ""},
	}
	body, _ := json.Marshal(resp)
	for i, w := range want {
		row := resp.Rows[i]
		if row.OperationalLocationDisplay != w.display {
			t.Fatalf("row %d display = %q, want %q", i, row.OperationalLocationDisplay, w.display)
		}
		if stringPtrValue(row.PartitionLabelRaw) != w.partition {
			t.Fatalf("row %d partition_label = %q, want %q", i, stringPtrValue(row.PartitionLabelRaw), w.partition)
		}
		if !strings.Contains(string(body), `"operational_location_display":"`+w.display+`"`) {
			t.Fatalf("wire body lacks operational_location_display %q: %s", w.display, body)
		}
	}
	if strings.Contains(string(body), `"partition_label":"whole"`) {
		t.Fatalf("a whole pen must never carry partition_label \"whole\": %s", body)
	}
}
