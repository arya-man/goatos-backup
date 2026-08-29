package app

import (
	"errors"
	"net/http"

	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

var (
	// ErrFeedPurchaseInvalidFarm reports a farm filter that is neither blank/all nor a real farm.
	ErrFeedPurchaseInvalidFarm = errors.New("procurement: unknown feed purchase farm filter")
	// ErrFeedPurchaseOffsetOutOfRange reports a page request past the ledger's bounded depth.
	ErrFeedPurchaseOffsetOutOfRange = errors.New("procurement: feed purchase page out of range")
	// ErrFeedPurchaseIdempotencyKeyRequired reports a record-purchase call with no Idempotency-Key.
	ErrFeedPurchaseIdempotencyKeyRequired = errors.New("procurement: feed purchase idempotency key required")
)

// feedPurchaseFieldLabel turns a validation field name into the label the form shows, so the
// message an operator reads names the box they must fix.
func feedPurchaseFieldLabel(field string) string {
	switch field {
	case "purchase_date":
		return "Purchase date"
	case "farm":
		return "Farm"
	case "feed_item":
		return "Feed"
	case "quantity_kg":
		return "Quantity (kg)"
	case "batch_no":
		return "Batch number"
	case "feed_cost":
		return "Feed cost"
	case "transport_cost":
		return "Transport cost"
	case "loading_cost":
		return "Loading cost"
	case "unloading_cost":
		return "Unloading cost"
	case "total_cost":
		return "Total cost"
	case "payment_released":
		return "Payment released"
	case "payment_status":
		return "Payment status"
	case "vendor":
		return "Vendor"
	case "paid_on":
		return "Paid on"
	case "amount_rupees":
		return "Amount paid"
	case "note":
		return "Note"
	default:
		return "That field"
	}
}

// FeedPurchaseHTTPError maps a feed-purchase error onto the transport error shape.
//
// Every branch returns a message an operator can act on: these surface directly in the entry
// drawer. An unrecognised error falls through to a generic 500 rather than echoing err.Error(),
// whose text can carry table, column and constraint detail that does not belong on a screen.
func FeedPurchaseHTTPError(err error) *Error {
	switch {
	case err == nil:
		return nil

	case errors.Is(err, ports.ErrFeedPurchaseNotFound):
		return NotFound("Feed purchase not found.")

	case errors.Is(err, ports.ErrFeedItemNotInCatalog):
		return BadRequest("feed_item_not_in_catalog",
			"That feed is not in the feed catalog, so it cannot be stocked. Add it in Feed Config first, then record the purchase.")

	case errors.Is(err, ports.ErrFeedPurchaseDuplicateBatch):
		return Conflict("feed_purchase_duplicate_batch",
			"That batch number is already recorded for this feed at this farm. Leave the batch number blank to record this as the next load.")

	case errors.Is(err, ports.ErrIdempotencyConflict):
		return Conflict("idempotency_conflict",
			"This purchase form changed after it was submitted. Reload the page and record it again.")

	case errors.Is(err, ErrFeedPurchaseIdempotencyKeyRequired):
		return BadRequest("missing_idempotency_key", "This purchase could not be recorded safely. Try again.")

	case errors.Is(err, ErrFeedPurchaseInvalidFarm):
		return BadRequest("invalid_farm", "That farm is not recognised.")

	case errors.Is(err, ErrFeedPurchaseOffsetOutOfRange):
		return BadRequest("page_out_of_range", "That page is beyond the purchase ledger. Use the filters to narrow it down.")

	default:
		var v domain.ErrFeedPurchaseValidation
		if errors.As(err, &v) {
			return &Error{
				Code:       "feed_purchase_invalid_" + v.Field,
				Message:    feedPurchaseFieldLabel(v.Field) + " " + v.Reason + ".",
				HTTPStatus: http.StatusBadRequest,
			}
		}
		return Internal("That purchase could not be recorded. Try again.")
	}
}
