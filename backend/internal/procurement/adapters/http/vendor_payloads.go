package http

import (
	"strings"

	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

// vendorPayload is the wire shape of one register row.
//
// Field names here, in contracts/openapi/app-api.yaml, and in the generated TypeScript client must
// move together. A rename on one side only is the "contract lie" failure mode: the client compiles,
// renders nothing, and nothing errors.
type vendorPayload struct {
	VendorID     string `json:"vendor_id"`
	RecordType   string `json:"record_type"`
	BusinessName string `json:"business_name"`
	// display_name is composed by the BACKEND so every surface renders a vendor identically.
	// A contact-less vendor is just its business name; with a contact it reads
	// "Bhopal Goat And Agro - Sammer". Composing this client-side is how two screens start
	// disagreeing about what the same vendor is called.
	DisplayName       string  `json:"display_name"`
	ContactPersonName *string `json:"contact_person_name"`
	PhoneNumber       *string `json:"phone_number"`

	Breed             *string `json:"breed"`
	Feed              *string `json:"feed"`
	Status            string  `json:"status"`
	StatusLabel       string  `json:"status_label"`
	FilteredStock     *int    `json:"filtered_stock"`
	PricePerGoat      *string `json:"price_per_goat"`
	ReadyToFiltered   *string `json:"ready_to_filtered"`
	ETAAfterOrderDays *int    `json:"eta_after_order_days"`
	Details           *string `json:"details"`

	State    string  `json:"state"`
	City     *string `json:"city"`
	Location string  `json:"location_display"`

	BankName  *string `json:"bank_name"`
	AccountNo *string `json:"account_no"`
	IFSCCode  *string `json:"ifsc_code"`
	UPIID     *string `json:"upi_id"`
	PANNumber *string `json:"pan_number"`
	// finance_redacted tells the client the payment fields were WITHHELD rather than empty, so it
	// can render "Hidden" instead of a blank that reads as "this vendor has no bank details".
	FinanceRedacted bool `json:"finance_redacted"`

	Comments *string `json:"comments"`

	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	// row_version must be echoed back on update. It is the optimistic fence that stops two editors
	// silently overwriting each other.
	RowVersion int64 `json:"row_version"`
}

type vendorListPayload struct {
	Vendors []vendorPayload `json:"vendors"`
	// total is the whole-filter count, never the page length. With limit and offset echoed back, the
	// client can render "Page 2 of 13" and a working Back control without inventing its own state.
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

type vendorCatalogEntryPayload struct {
	Value string `json:"value"`
	Label string `json:"label"`
	// is_active false means the entry still renders on vendors that carry it but must not be
	// offered for new rows.
	IsActive bool `json:"is_active"`
}

type vendorCatalogPayload struct {
	RecordTypes []vendorCatalogEntryPayload `json:"record_types"`
	Breeds      []vendorCatalogEntryPayload `json:"breeds"`
	States      []vendorCatalogEntryPayload `json:"states"`
	Cities      []vendorCatalogEntryPayload `json:"cities"`
	Statuses    []vendorCatalogEntryPayload `json:"statuses"`
	Feeds       []vendorCatalogEntryPayload `json:"feeds"`
}

// vendorWritePayload is the create/update body.
//
// Every optional field is a plain string rather than a pointer: an update REPLACES the row, so an
// omitted field and a cleared field must mean the same thing. Pointers would introduce a
// patch-vs-replace distinction the register does not have and that clients would get wrong.
type vendorWritePayload struct {
	RecordType        string  `json:"record_type"`
	BusinessName      string  `json:"business_name"`
	ContactPersonName string  `json:"contact_person_name"`
	PhoneNumber       string  `json:"phone_number"`
	Breed             string  `json:"breed"`
	Feed              string  `json:"feed"`
	Status            string  `json:"status"`
	FilteredStock     *int    `json:"filtered_stock"`
	PricePerGoat      *string `json:"price_per_goat"`
	ReadyToFiltered   string  `json:"ready_to_filtered"`
	ETAAfterOrderDays *int    `json:"eta_after_order_days"`
	Details           string  `json:"details"`
	State             string  `json:"state"`
	City              string  `json:"city"`
	BankName          string  `json:"bank_name"`
	AccountNo         string  `json:"account_no"`
	IFSCCode          string  `json:"ifsc_code"`
	UPIID             string  `json:"upi_id"`
	PANNumber         string  `json:"pan_number"`
	Comments          string  `json:"comments"`
	// row_version is required on update and ignored on create.
	RowVersion int64 `json:"row_version"`
}

// vendorStatusPayload is the status-only change body.
type vendorStatusPayload struct {
	Status string `json:"status"`
	// row_version is required: a status flip is still a write that can lose a race.
	RowVersion int64 `json:"row_version"`
}

func (p vendorWritePayload) toDomain() domain.VendorWrite {
	return domain.VendorWrite{
		RecordType: p.RecordType, BusinessName: p.BusinessName,
		ContactPersonName: p.ContactPersonName, PhoneNumber: p.PhoneNumber,
		Breed: p.Breed, Feed: p.Feed, Status: p.Status,
		FilteredStock: p.FilteredStock, PricePerGoat: p.PricePerGoat,
		ReadyToFiltered: p.ReadyToFiltered, ETAAfterOrderDays: p.ETAAfterOrderDays,
		Details: p.Details, State: p.State, City: p.City,
		BankName: p.BankName, AccountNo: p.AccountNo, IFSCCode: p.IFSCCode,
		UPIID: p.UPIID, PANNumber: p.PANNumber, Comments: p.Comments,
	}
}

func toVendorPayload(v domain.Vendor) vendorPayload {
	return vendorPayload{
		VendorID:          v.VendorID,
		RecordType:        v.RecordType,
		BusinessName:      v.BusinessName,
		DisplayName:       vendorDisplayName(v),
		ContactPersonName: v.ContactPersonName,
		PhoneNumber:       v.PhoneNumber,
		Breed:             v.Breed,
		Feed:              v.Feed,
		Status:            v.Status,
		StatusLabel:       vendorStatusLabel(v.Status),
		FilteredStock:     v.FilteredStock,
		PricePerGoat:      v.PricePerGoat,
		ReadyToFiltered:   v.ReadyToFiltered,
		ETAAfterOrderDays: v.ETAAfterOrderDays,
		Details:           v.Details,
		State:             v.State,
		City:              v.City,
		Location:          vendorLocationDisplay(v),
		BankName:          v.BankName,
		AccountNo:         v.AccountNo,
		IFSCCode:          v.IFSCCode,
		UPIID:             v.UPIID,
		PANNumber:         v.PANNumber,
		FinanceRedacted:   v.FinanceRedacted,
		Comments:          v.Comments,
		CreatedAt:         v.CreatedAt,
		UpdatedAt:         v.UpdatedAt,
		RowVersion:        v.RowVersion,
	}
}

// vendorDisplayName composes the one name every surface shows for a vendor.
//
// A missing contact is DROPPED rather than rendered as a dangling separator -- the same
// agree-or-go-bare rule the operational-location helper uses, for the same reason: a label with a
// trailing "- " reads as a bug to the person looking at it.
func vendorDisplayName(v domain.Vendor) string {
	name := strings.TrimSpace(v.BusinessName)
	if v.ContactPersonName == nil {
		return name
	}
	contact := strings.TrimSpace(*v.ContactPersonName)
	if contact == "" || strings.EqualFold(contact, name) {
		// The source data often repeats the business name as the contact ("Irshad" / "Irshad").
		// Rendering "Irshad - Irshad" would be noise.
		return name
	}
	return name + " - " + contact
}

// vendorLocationDisplay composes city and state into the one location string the table shows.
func vendorLocationDisplay(v domain.Vendor) string {
	state := strings.TrimSpace(v.State)
	if v.City == nil {
		return state
	}
	city := strings.TrimSpace(*v.City)
	if city == "" {
		return state
	}
	if state == "" {
		return city
	}
	return city + ", " + state
}

// vendorStatusLabel renders a stored status as operator-facing copy.
//
// Backend-owned by the golden rule: the client renders this verbatim rather than mapping the raw
// token itself, so "In Active" cannot come back as "Inactive" on one screen and "In Active" on
// another.
func vendorStatusLabel(status string) string {
	switch status {
	case domain.VendorStatusActive:
		return "Active"
	case domain.VendorStatusInactive:
		return "Inactive"
	case domain.VendorStatusNegotiating:
		return "Negotiating"
	case domain.VendorStatusBanned:
		return "Banned"
	default:
		return status
	}
}
