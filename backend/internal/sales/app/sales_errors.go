package app

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/vgoats/goatos/backend/internal/sales/domain"
	"github.com/vgoats/goatos/backend/internal/sales/ports"
)

// Error is the transport error shape the sales handlers write. Same contract as procurement's:
// code + operator-readable message + status.
type Error struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *Error) Error() string {
	return e.Code + ": " + e.Message
}

func BadRequest(code, message string) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: http.StatusBadRequest}
}

func NotFound(message string) *Error {
	return &Error{Code: "not_found_or_not_allowed", Message: message, HTTPStatus: http.StatusNotFound}
}

func Conflict(code, message string) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: http.StatusConflict}
}

func Internal(message string) *Error {
	return &Error{Code: "internal_error", Message: message, HTTPStatus: http.StatusInternalServerError}
}

var (
	// ErrSalesInvalidFarm reports a farm filter that is neither a farm nor "all".
	ErrSalesInvalidFarm = errors.New("sales: unknown farm filter")
	// ErrSalesOffsetOutOfRange reports a page request past the ledger's bounded paging depth.
	ErrSalesOffsetOutOfRange = errors.New("sales: deal page out of range")
	// ErrSalesIdempotencyKeyRequired reports a record-sale request without an Idempotency-Key.
	ErrSalesIdempotencyKeyRequired = errors.New("sales: idempotency key required")
)

// SalesHTTPError maps a sales-path error onto the transport error shape.
//
// Every branch returns a message an operator can act on. An unrecognised error deliberately falls
// through to a 500 with a generic message rather than echoing err.Error(): an unexpected failure
// here is a database or driver fault, and its text can carry table/constraint detail that does not
// belong on a screen.
func SalesHTTPError(err error) *Error {
	switch {
	case err == nil:
		return nil

	case errors.Is(err, ports.ErrDealNotFound):
		return NotFound("Sale not found.")

	case errors.Is(err, ports.ErrDealPaymentNotFound):
		return NotFound("Payment not found.")

	case errors.Is(err, ports.ErrLeadNotFound):
		return NotFound("Lead not found.")

	case errors.Is(err, ports.ErrIdempotencyConflict):
		return Conflict("idempotency_conflict",
			"This request was already submitted with different details. Review the recorded sale before trying again.")

	case errors.Is(err, ErrSalesInvalidFarm):
		return BadRequest("invalid_farm", "Choose CBE, CPT, or all farms.")

	case errors.Is(err, ErrSalesOffsetOutOfRange):
		return BadRequest("page_out_of_range", "That page is beyond the sales ledger. Use the farm filter to narrow it down.")

	case errors.Is(err, ErrSalesIdempotencyKeyRequired):
		return BadRequest("missing_idempotency_key", "This sale could not be recorded safely. Try again.")

	case errors.Is(err, ports.ErrProductHasSales):
		return Conflict("product_has_sales",
			"This item has sales recorded against it, so it cannot be deleted. Untick 'In use' instead: it disappears from every dropdown and its sales stay readable.")

	case errors.Is(err, ports.ErrProductNotFound):
		return NotFound("Item not found.")

	case errors.Is(err, ports.ErrProductNameTaken):
		return Conflict("product_name_taken",
			"Another item already has that name. A sale records the name it was sold under, so two items cannot share one.")

	case errors.Is(err, ports.ErrProductNotSellable):
		// A CONFLICT, not a validation failure: the body was right when the form opened and the
		// farm changed its mind in between, so telling the desk to reload is more useful than a
		// field error implying they typed something wrong.
		return Conflict("product_not_sellable",
			"That product is no longer one this farm sells. Reload the page to see the current list, then record the sale again.")

	case errors.Is(err, ErrNothingSellable):
		return BadRequest("nothing_sellable",
			"This farm has nothing set up to sell yet. Add what it sells under Configuration, Items and settings, then record the sale.")

	case errors.Is(err, ports.ErrValuationVersionConflict):
		return Conflict("valuation_version_conflict", "The valuation was changed by someone else. Reload the page to see the current figures, then try again.")

	case errors.Is(err, domain.ErrValuationInvalid):
		return BadRequest("valuation_invalid", strings.TrimPrefix(err.Error(), domain.ErrValuationInvalid.Error()+": "))

	default:
		// The short-feed-sale CONFIRMATION (maintainer decision 2026-09-23). 422 rather than 400
		// because nothing about the body is malformed -- it is a complete, valid sale that the
		// store's ledger disagrees with, and the desk may well be right. The client keys on the
		// CODE and re-sends the same sale with stock_shortfall_acknowledged, never on this
		// sentence, which is farm copy and may be reworded.
		var short domain.ErrFeedStockShort
		if errors.As(err, &short) {
			return &Error{
				Code:       "feed_stock_confirmation_required",
				Message:    feedShortfallMessage(short),
				HTTPStatus: http.StatusUnprocessableEntity,
			}
		}
		var p domain.ErrProductValidation
		if errors.As(err, &p) {
			return &Error{
				Code:       "sellable_product_invalid_" + p.Field,
				Message:    productFieldLabel(p.Field) + " " + p.Reason + ".",
				HTTPStatus: http.StatusBadRequest,
			}
		}
		var v domain.ErrDealValidation
		if errors.As(err, &v) {
			return &Error{
				Code:       "sales_invalid_" + v.Field,
				Message:    salesFieldLabel(v.Field) + " " + v.Reason + ".",
				HTTPStatus: http.StatusBadRequest,
			}
		}
		var f domain.ErrFieldValidation
		if errors.As(err, &f) {
			return &Error{
				Code:       "sales_invalid_" + f.Field,
				Message:    salesFieldLabel(f.Field) + " " + f.Reason + ".",
				HTTPStatus: http.StatusBadRequest,
			}
		}
		return Internal("Could not complete that sales action.")
	}
}

// feedShortfallMessage is the sentence the desk reads when a sale takes more feed than the store's
// ledger holds. It names the farm, the feed and BOTH figures, because the question being asked is
// whether the ledger is behind -- and nobody can answer that without seeing what the ledger says.
func feedShortfallMessage(short domain.ErrFeedStockShort) string {
	parts := make([]string, 0, len(short.Shortfalls))
	for _, s := range short.Shortfalls {
		parts = append(parts, fmt.Sprintf("%s %s has %s kg in the store and this sale takes %s kg",
			s.FarmLabel, s.FeedItem, trimKg(s.BalanceKg), trimKg(s.RequestedKg)))
	}
	return strings.Join(parts, "; ") + ". If a load has reached the farm and is not recorded yet, confirm and record the sale anyway."
}

// trimKg renders kilograms without trailing zeros, so 2000 reads as "2000" and 12.5 as "12.5".
func trimKg(v float64) string {
	out := strconv.FormatFloat(v, 'f', 3, 64)
	out = strings.TrimRight(out, "0")
	return strings.TrimSuffix(out, ".")
}

// productFieldLabel renders an item-editor field name as the label on screen.
func productFieldLabel(field string) string {
	switch field {
	case "name":
		return "Item name"
	case "kind":
		return "What it is"
	case "unit":
		return "Sold by"
	case "status":
		return "Status"
	case "sort_order":
		return "Order"
	}
	return field
}

// salesFieldLabel renders a storage field name as the label the operator sees on the record-sale
// drawer, so an error reads "Buyer name required." rather than "buyer_name required.".
func salesFieldLabel(field string) string {
	switch field {
	case "sale_date":
		return "Sale date"
	case "farm":
		return "Farm"
	case "product_type":
		return "Product"
	case "breed":
		return "Breed"
	case "buyer_name":
		return "Buyer name"
	case "buyer_place":
		return "Buyer place"
	case "animal_count":
		return "Animal count"
	case "male_count":
		return "Male count"
	case "female_count":
		return "Female count"
	case "total_weight_kg":
		return "Total weight"
	case "sales_value":
		return "Sale value"
	case "advance_amount":
		return "Advance amount"
	case "comments":
		return "Comments"
	case "recorded_date":
		return "Recorded date"
	case "call_status":
		return "Call status"
	case "animal_type":
		return "Animal type"
	case "fpo_name":
		return "Farmer group name"
	case "crops":
		return "Crops"
	case "district":
		return "District"
	case "taluk":
		return "Taluk"
	case "state":
		return "State"
	case "market":
		return "Market"
	case "category":
		return "Animal"
	case "source":
		return "Quoted by"
	case "ex_farm_rate":
		return "Ex-farm rate"
	case "transport_rate":
		return "Transport rate"
	case "landing_cost_per_kg":
		return "Landed cost per kg"
	case "market_price_per_kg":
		return "Market price per kg"
	case "rows":
		return "Tag list"
	case "animal_label":
		return "Animal label"
	case "weight_kg":
		return "Weight"
	case "tag_number":
		return "Tag number"
	case "book_weight_kg":
		return "Book weight"
	case "video_weight_kg":
		return "Video weight"
	default:
		return field
	}
}
