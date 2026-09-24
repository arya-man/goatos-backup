package postgres

import (
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/processintegrity/domain"
)

// /workflows/{row_id} used to rebuild EVERY grain of the tenant's whole history (IncludeCompleted)
// and only then keep the one matching row_id (590 ms on the clone, 1.5 s with JIT). A batched
// row_id names its shed, protocol version and earliest business date, so GetRow pushes those into
// the existing $3/$9/$4 filters. Each is either a grouping key of the row or a lower bound its MIN
// date already satisfies, so the row's own members are unchanged.
func TestNarrowQueryToBatchedRowID(t *testing.T) {
	const (
		shed = "5a7678f6-0b2c-5bd3-b466-b0364cae7a21"
		pv   = "11111111-2222-4333-8444-555555555555"
	)
	rowID := "batch:aaaaaaaa-0000-4000-8000-000000000001:rule:bbbbbbbb-0000-4000-8000-000000000001" +
		":protocol_version:" + pv + ":shed:" + shed + ":partition:part 10:date:2026-08-15"
	early := time.Date(2026, 1, 1, 0, 0, 0, 0, biztime.DefaultLocation())

	got := narrowQueryToRowID(domain.Query{DueAfter: &early}, rowID)
	if got.ShedID == nil || *got.ShedID != shed {
		t.Fatalf("shed not pushed down: %v", got.ShedID)
	}
	if got.ProtocolVersionID == nil || *got.ProtocolVersionID != pv {
		t.Fatalf("protocol version not pushed down: %v", got.ProtocolVersionID)
	}
	want := time.Date(2026, 8, 15, 0, 0, 0, 0, biztime.DefaultLocation())
	if got.DueAfter == nil || !got.DueAfter.Equal(want) {
		t.Fatalf("DueAfter = %v, want %v", got.DueAfter, want)
	}

	// Never widen a caller's narrower scope.
	late := time.Date(2026, 9, 1, 0, 0, 0, 0, biztime.DefaultLocation())
	other := "99999999-0000-4000-8000-000000000009"
	got = narrowQueryToRowID(domain.Query{DueAfter: &late, ShedID: &other}, rowID)
	if !got.DueAfter.Equal(late) || *got.ShedID != other || got.ProtocolVersionID == nil {
		t.Fatalf("narrowing widened the caller's scope: %+v", got)
	}

	// Rows whose id carries no location/date are left alone.
	for _, id := range []string{"obligation:abc", "feed_projection_exception:x", "batch:garbage"} {
		got = narrowQueryToRowID(domain.Query{}, id)
		if got.ShedID != nil || got.ProtocolVersionID != nil || got.DueAfter != nil {
			t.Fatalf("%s should not be narrowed: %+v", id, got)
		}
	}
}
