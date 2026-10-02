package domain

import "testing"

// PR #294 O8: "Pens to pack 6" sat above 25 pen lines because the summary counted PHYSICAL
// sheds -- Gandhi 1/2/3, Godel 1 - Part 1..4 and Old Yashoda 1..5 each collapsed to one. A pen is
// shed + partition (docs/decisions/partition-is-operational-shed.md), so both summaries count
// operational locations; the paging stays on the physical shed so a shed's pens never split.
func TestSummariesCountPensNotPhysicalSheds(t *testing.T) {
	kg := "1.0"
	item := []ItemQuantity{{FeedItem: "Dry Masoor Bhusa", QuantityKg: &kg}}
	blocked := []ItemQuantity{{FeedItem: "Dry Masoor Bhusa", Status: QuantityBlocked}}
	rows := []DirectionRow{
		{ShedID: "gandhi", PartitionLabel: "1", Items: item},
		{ShedID: "gandhi", PartitionLabel: "2", Items: item},
		{ShedID: "gandhi", PartitionLabel: "3", Items: blocked},
		{ShedID: "yashoda", PartitionLabel: "", Items: item},
		// the same pen twice (a second grain / session) is still one pen; "Part 1" and "part 1 " match
		{ShedID: "godel", PartitionLabel: "Part 1", Items: item},
		{ShedID: "godel", PartitionLabel: "part 1 ", Items: item},
	}
	if got := SummarizeScope(rows, nil); got.ShedCount != 5 || got.BlockedShedCount != 1 {
		t.Fatalf("SummarizeScope pens = %d blocked = %d, want 5 / 1", got.ShedCount, got.BlockedShedCount)
	}
	lines := []PackingRow{
		{ShedID: "gandhi", PartitionLabel: "1", SessionNo: 1, Items: item},
		{ShedID: "gandhi", PartitionLabel: "1", SessionNo: 2, Items: item},
		{ShedID: "gandhi", PartitionLabel: "2", SessionNo: 1, Items: blocked},
		{ShedID: "yashoda", SessionNo: 1, Items: item},
	}
	if got := SummarizePacking(lines, nil); got.ShedCount != 3 || got.BlockedShedCount != 1 {
		t.Fatalf("SummarizePacking pens = %d blocked = %d, want 3 / 1", got.ShedCount, got.BlockedShedCount)
	}
}
