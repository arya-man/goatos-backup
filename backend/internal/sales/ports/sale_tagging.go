package ports

import (
	"context"
	"fmt"
)

// SaleTaggingReader answers the ONE herd fact the sales ledger needs before a deal is marked
// failed (maintainer decision 2026-09-25): how many animals are still tagged to it.
//
// It is a port the sales module declares rather than a join its repository performs, because the
// sales lock (migration 000173) keeps sales off the herd schema: the mapping lives on the herd side
// (goat_sale_allocations, 000177) and is read through sales/adapters/identitybridge -- the mirror
// of identity/adapters/salesbridge, which reads the ledger for the tagging gate.
type SaleTaggingReader interface {
	// TaggedAnimalCount is the number of animals live-tagged to the deal. It reads COMMITTED
	// state, so the caller asks while holding the deal's row lock: a tagging confirm locks the
	// same row before it writes, so the two can never both win.
	TaggedAnimalCount(ctx context.Context, tenantID, dealID string) (int, error)
}

// ErrDealHasTaggedAnimals refuses marking a deal failed while animals are tagged to it. Those
// animals were exited from the herd as sold by the tagging confirm; putting them back is a
// reversal nothing in the herd supports yet (vaccination work cancelled at exit, counts, feed),
// so the status change is refused rather than leaving a failed sale whose animals are gone.
type ErrDealHasTaggedAnimals struct {
	Count int
}

func (e ErrDealHasTaggedAnimals) Error() string {
	return fmt.Sprintf("sales: %d animals are tagged to this deal", e.Count)
}
