package domain

import "testing"

// TestRejectedRowIsNeverQuietlyOK pins the one rule every source inherits: a rejected row is
// amber, carries attention, and holds a pending unit, whatever the source left blank.
func TestRejectedRowIsNeverQuietlyOK(t *testing.T) {
	r := Row{Module: ModuleFeed, SourceType: "feed_transport_task", SourceID: "x", WorkState: WorkStateRejected}.Finalize()
	if r.Severity != SeverityWatch || r.Counts.NeedsAttention != 1 || r.Counts.Pending != 1 || r.Lane != LaneInProgress {
		t.Fatalf("rejected row finalized wrong: %+v", r)
	}
	// A source that already said more keeps it.
	r = Row{Module: ModuleWeighing, SourceType: "weighing_work_item", SourceID: "y", WorkState: WorkStateRejected, Severity: SeverityAtRisk, Counts: Counts{Done: 5, NeedsAttention: 2}}.Finalize()
	if r.Severity != SeverityAtRisk || r.Counts.NeedsAttention != 2 || r.Counts.Pending != 0 || r.Counts.Done != 5 {
		t.Fatalf("a fuller answer must stand: %+v", r)
	}
	// Any other state is untouched.
	r = Row{Module: ModuleFeed, SourceType: "t", SourceID: "z", WorkState: WorkStateDue}.Finalize()
	if r.Severity != SeverityOK || r.Counts != (Counts{}) {
		t.Fatalf("a due row must not be touched: %+v", r)
	}
}
