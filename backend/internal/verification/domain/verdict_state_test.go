package domain

import (
	"testing"
	"time"
)

// VerdictState is the difference between "a verifier decided" and "the farm's
// records changed". Those are two different moments because verdicts are applied
// asynchronously, and collapsing them is what let an approved-but-unapplied item
// vanish out of the pending queue and read as finished work (W-18).
//
// Every case below asserts the VALUE, so a future refactor that quietly makes
// "applying" unreachable fails here rather than in production silence.
func TestVerdictState(t *testing.T) {
	applied := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		item Item
		want string
	}{
		{
			name: "nobody has decided yet",
			item: Item{Status: StatusPending, ApplierAckExpected: true},
			want: VerdictStateAwaitingReview,
		},
		{
			// THE bug. The verifier tapped approve, the item left the pending
			// queue, and the producing module has not been told yet.
			name: "approved, producer has not acked",
			item: Item{Status: StatusApproved, ApplierAckExpected: true},
			want: VerdictStateApplying,
		},
		{
			name: "rejected, producer has not acked",
			item: Item{Status: StatusRejected, ApplierAckExpected: true},
			want: VerdictStateApplying,
		},
		{
			name: "approved and acked by the producer",
			item: Item{Status: StatusApproved, ApplierAckExpected: true, AppliedAt: &applied},
			want: VerdictStateSettled,
		},
		{
			// A producer that has not wired an ack must not have all of its
			// decided items park in "applying" forever. Opt-in, not opt-out.
			name: "decided by a producer that does not participate in the ack protocol",
			item: Item{Status: StatusApproved, ApplierAckExpected: false},
			want: VerdictStateSettled,
		},
		{
			// A withdrawal carries no verdict at all, so there is no outcome for
			// any applier to apply and nothing to wait on.
			name: "withdrawn, never decided",
			item: Item{Status: StatusWithdrawn, ApplierAckExpected: true},
			want: VerdictStateSettled,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.item.VerdictState(); got != tc.want {
				t.Fatalf("VerdictState()=%q, want %q", got, tc.want)
			}
		})
	}
}
