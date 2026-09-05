package app

import (
	"errors"
	"net/http"

	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

var (
	// ErrVendorOffsetOutOfRange reports a page request past the register's bounded paging depth.
	ErrVendorOffsetOutOfRange = errors.New("procurement: vendor page out of range")
	// ErrVendorRowVersionRequired reports an update that carried no optimistic-concurrency fence.
	ErrVendorRowVersionRequired = errors.New("procurement: vendor row version required")
	// ErrVendorSideUnknown reports a register side that is neither procurement nor sales.
	//
	// REFUSED rather than widened to "both", because a page that asked for one half of the register
	// and silently received all of it would put buyers on the buying desk's screen -- exactly the
	// mix the split exists to end -- and nothing on the screen would say so.
	ErrVendorSideUnknown = errors.New("procurement: vendor register side unknown")
)

// VendorHTTPError maps a vendor-path error onto the transport error shape.
//
// Every branch returns a message an operator can act on, because these surface directly in the
// admin-web drawer. "conflict" tells someone nothing; "Another user saved changes to this vendor
// while you were editing. Reload to see their version." tells them what to do next.
//
// An unrecognised error deliberately falls through to a 500 with a generic message rather than
// echoing err.Error(): an unexpected failure here is a database or driver fault, and its text can
// carry table, column and constraint detail that does not belong on a screen.
func VendorHTTPError(err error) *Error {
	switch {
	case err == nil:
		return nil

	case errors.Is(err, ports.ErrInvalidVoiceNote):
		return BadRequest("vendor_voice_note_invalid", "That voice note could not be attached. Record it again and save.")

	case errors.Is(err, ports.ErrVendorNotFound):
		// Deliberately the same answer for "does not exist" and "belongs to another tenant", so the
		// register cannot be probed across tenants.
		return NotFound("Vendor not found.")

	case errors.Is(err, ports.ErrVendorDuplicate):
		return Conflict("vendor_duplicate",
			"A vendor with this business name, record type, state and phone number already exists. "+
				"Add the new contact with their own phone number, or edit the existing vendor.")

	case errors.Is(err, ports.ErrVendorStaleWrite):
		return Conflict("vendor_stale_write",
			"Another user saved changes to this vendor while you were editing. Reload to see their version, then reapply your changes.")

	case errors.Is(err, ErrVendorRowVersionRequired):
		return BadRequest("vendor_row_version_required",
			"This vendor must be reloaded before it can be saved.")

	case errors.Is(err, ErrVendorOffsetOutOfRange):
		return BadRequest("page_out_of_range", "That page is beyond the vendor list. Use the filters to narrow it down.")

	case errors.Is(err, ErrVendorSideUnknown):
		return BadRequest("vendor_side_unknown", "That vendor list is not one we keep. Open Vendors from Procurement or from Sales.")

	default:
		// Field-level validation carries its own operator-readable reason.
		var v domain.ErrVendorValidation
		if errors.As(err, &v) {
			return &Error{
				Code:       "vendor_invalid_" + v.Field,
				Message:    vendorFieldLabel(v.Field) + " " + v.Reason + ".",
				HTTPStatus: http.StatusBadRequest,
			}
		}
		return Internal("Could not complete that vendor action.")
	}
}

// vendorFieldLabel renders a storage field name as the label the operator sees on the form, so an
// error reads "Business name required." rather than "business_name required.".
//
// This is error copy attached to a validation failure the backend owns, which is why it lives here
// rather than in the client: the same message must read identically wherever the API is called.
func vendorFieldLabel(field string) string {
	switch field {
	case "business_name":
		return "Business name"
	case "record_type":
		return "Record type"
	case "contact_person_name":
		return "Contact person"
	case "phone_number":
		return "Phone number"
	case "eta_after_order_days":
		return "ETA after order"
	case "filtered_stock":
		return "Filtered stock"
	case "price_per_goat":
		return "Price per goat"
	case "ready_to_filtered":
		return "Ready to filtered"
	case "bank_name":
		return "Bank name"
	case "account_no":
		return "Account number"
	case "ifsc_code":
		return "IFSC code"
	case "upi_id":
		return "UPI ID"
	case "pan_number":
		return "PAN number"
	case "status":
		return "Status"
	case "state":
		return "State"
	case "city":
		return "City"
	case "breed":
		return "Breed"
	case "feed":
		return "Feed"
	case "details":
		return "Details"
	case "comments":
		return "Comments"
	default:
		return field
	}
}
