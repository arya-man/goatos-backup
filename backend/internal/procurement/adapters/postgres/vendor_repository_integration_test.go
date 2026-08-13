package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

func strp(v string) *string { return &v }
func intp(v int) *int       { return &v }

// TestVendorRegisterPostgresPaths exercises the vendor register against a real Postgres.
//
// It is an integration test rather than a unit test on purpose: every defect this file is written
// to catch lives in the SQL, not in Go. The create statement interleaves 22 positional placeholders
// across a nullif()-wrapped column list, which compiles and type-checks whatever order the
// arguments are in -- a swapped pair of same-typed columns (bank_name/account_no, breed/feed) would
// be invisible to a compiler and to any test using a fake repository. So the first subtest writes a
// DISTINCT value into every column and reads each one back by name.
func TestVendorRegisterPostgresPaths(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	price := "1250.50"

	created, err := repo.CreateVendor(ctx, testTenant, domain.VendorWrite{
		RecordType: "Goat Stockist", BusinessName: "Bhopal Goat And Agro", ContactPersonName: "Sammer",
		PhoneNumber: "97550 44183", Breed: "Sojat", Feed: "Multiple", Status: "Active",
		FilteredStock: intp(45), PricePerGoat: &price, ReadyToFiltered: "Yes",
		ETAAfterOrderDays: intp(80), Details: "2 and 4 Teeth Sojat - 35kg avg",
		State: "MP", City: "Bhopal", BankName: "HDFC Bank", AccountNo: "12345678901",
		IFSCCode: "hdfc0002398", UPIID: "sammer@upi", PANNumber: "abcde1234f",
		Comments: "prefers morning calls",
	}.Normalize(), "")
	if err != nil {
		t.Fatalf("create vendor: %v", err)
	}

	t.Run("every column lands in its own field", func(t *testing.T) {
		for name, got := range map[string]string{
			"record_type":   created.RecordType,
			"business_name": created.BusinessName,
			"status":        created.Status,
			"state":         created.State,
		} {
			if got == "" {
				t.Errorf("%s empty", name)
			}
		}
		for name, pair := range map[string][2]string{
			"contact_person_name": {derefOr(created.ContactPersonName), "Sammer"},
			"phone_number":        {derefOr(created.PhoneNumber), "97550 44183"},
			"breed":               {derefOr(created.Breed), "Sojat"},
			"feed":                {derefOr(created.Feed), "Multiple"},
			"ready_to_filtered":   {derefOr(created.ReadyToFiltered), "Yes"},
			"details":             {derefOr(created.Details), "2 and 4 Teeth Sojat - 35kg avg"},
			"city":                {derefOr(created.City), "Bhopal"},
			"bank_name":           {derefOr(created.BankName), "HDFC Bank"},
			"account_no":          {derefOr(created.AccountNo), "12345678901"},
			"upi_id":              {derefOr(created.UPIID), "sammer@upi"},
			"comments":            {derefOr(created.Comments), "prefers morning calls"},
			// IFSC and PAN are upper-cased by Normalize; storing them as typed would make an
			// exact-match lookup fail depending on who typed the row.
			"ifsc_code":  {derefOr(created.IFSCCode), "HDFC0002398"},
			"pan_number": {derefOr(created.PANNumber), "ABCDE1234F"},
		} {
			if pair[0] != pair[1] {
				t.Errorf("column %s mismapped: got %q want %q", name, pair[0], pair[1])
			}
		}
		if created.FilteredStock == nil || *created.FilteredStock != 45 {
			t.Errorf("filtered_stock mismapped: %v", created.FilteredStock)
		}
		if created.ETAAfterOrderDays == nil || *created.ETAAfterOrderDays != 80 {
			t.Errorf("eta_after_order_days mismapped: %v", created.ETAAfterOrderDays)
		}
		if created.PricePerGoat == nil || !strings.HasPrefix(*created.PricePerGoat, "1250.5") {
			t.Errorf("price_per_goat mismapped: %v", created.PricePerGoat)
		}
		if created.Status != domain.VendorStatusActive {
			t.Errorf("sheet casing 'Active' must normalize to %q, got %q", domain.VendorStatusActive, created.Status)
		}
		if created.RowVersion != 1 {
			t.Errorf("row_version starts at 1, got %d", created.RowVersion)
		}
	})

	t.Run("natural key is not defeated by name casing or phone spacing", func(t *testing.T) {
		// The real double-entry in the source sheet ('Ajay Tomar', twice, same number) differed only
		// by an absent contact name, and the sheet spells one number as '97550 44183'. Both spellings
		// must collide with the row above.
		_, err := repo.CreateVendor(ctx, testTenant, domain.VendorWrite{
			RecordType: "Goat Stockist", BusinessName: "  bhopal goat and agro ",
			PhoneNumber: "9755044183", Status: "Active", State: "MP",
		}.Normalize(), "")
		if !errors.Is(err, ports.ErrVendorDuplicate) {
			t.Fatalf("expected ErrVendorDuplicate, got %v", err)
		}
	})

	t.Run("a second contact at the same business is allowed", func(t *testing.T) {
		// Four of the five sheet collisions are legitimate: one business, several people worth
		// calling (Iffco Tokio -> Manoj and Mr Kanna). If this ever starts failing, the import loses
		// eight real contacts.
		if _, err := repo.CreateVendor(ctx, testTenant, domain.VendorWrite{
			RecordType: "Goat Stockist", BusinessName: "Bhopal Goat And Agro",
			ContactPersonName: "Other Person", PhoneNumber: "9000000001",
			Status: "Active", State: "MP",
		}.Normalize(), ""); err != nil {
			t.Fatalf("second contact at the same business must be allowed: %v", err)
		}
	})

	t.Run("finance fields are withheld without the permission", func(t *testing.T) {
		red, err := repo.GetVendor(ctx, testTenant, created.VendorID, false)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		for name, got := range map[string]*string{
			"bank_name": red.BankName, "account_no": red.AccountNo, "ifsc_code": red.IFSCCode,
			"upi_id": red.UPIID, "pan_number": red.PANNumber,
		} {
			if got != nil {
				t.Errorf("%s leaked to a caller without VendorFinanceRead: %q", name, *got)
			}
		}
		if !red.FinanceRedacted {
			t.Error("FinanceRedacted must be set so the UI can say 'hidden' rather than render a blank")
		}
		full, err := repo.GetVendor(ctx, testTenant, created.VendorID, true)
		if err != nil {
			t.Fatalf("get with finance: %v", err)
		}
		if derefOr(full.BankName) != "HDFC Bank" || full.FinanceRedacted {
			t.Error("finance withheld from a caller who does hold VendorFinanceRead")
		}
	})

	t.Run("update is fenced on row_version", func(t *testing.T) {
		base := domain.VendorWrite{
			RecordType: "Goat Stockist", BusinessName: "Bhopal Goat And Agro",
			Status: "Active", State: "MP", PhoneNumber: "97550 44183",
		}.Normalize()

		if _, err := repo.UpdateVendor(ctx, testTenant, created.VendorID, base, 99, "", false); !errors.Is(err, ports.ErrVendorStaleWrite) {
			t.Fatalf("stale version must be refused, got %v", err)
		}

		next := base
		next.Status = "In Active " // the source sheet's exact spelling, which the CHECK rejects raw
		next.City = "Bhopal"
		updated, err := repo.UpdateVendor(ctx, testTenant, created.VendorID, next.Normalize(), created.RowVersion, "", false)
		if err != nil {
			t.Fatalf("update with current version: %v", err)
		}
		if updated.Status != domain.VendorStatusInactive {
			t.Errorf("'In Active ' must normalize to %q, got %q", domain.VendorStatusInactive, updated.Status)
		}
		if updated.RowVersion != created.RowVersion+1 {
			t.Errorf("row_version must advance: %d -> %d", created.RowVersion, updated.RowVersion)
		}
		// The update omitted bank/details, which means CLEARED. They must be NULL, not "" -- two
		// spellings of "no value" would make the natural key's coalesce ambiguous.
		if updated.BankName != nil || updated.Details != nil {
			t.Error("cleared optional fields stored as empty string instead of NULL")
		}
	})

	t.Run("reads are tenant scoped", func(t *testing.T) {
		_, err := repo.GetVendor(ctx, "00000000-0000-4000-8000-000000000999", created.VendorID, true)
		if !errors.Is(err, ports.ErrVendorNotFound) {
			t.Fatalf("another tenant must get ErrVendorNotFound, got %v", err)
		}
	})

	t.Run("search matches infix and treats LIKE metacharacters as literals", func(t *testing.T) {
		page, err := repo.ListVendors(ctx, testTenant, domain.VendorFilter{Search: "bhopal"}, 25, 0, false)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if page.Total != 2 || len(page.Vendors) != 2 {
			t.Fatalf("search 'bhopal' expected 2 rows, got total=%d rows=%d", page.Total, len(page.Vendors))
		}
		if page.Vendors[0].BankName != nil {
			t.Error("list leaked finance to a caller without the permission")
		}
		// An unescaped '%' would match every row; escaped, it matches none of these.
		pct, err := repo.ListVendors(ctx, testTenant, domain.VendorFilter{Search: "%"}, 25, 0, false)
		if err != nil {
			t.Fatalf("list %%: %v", err)
		}
		if pct.Total != 0 {
			t.Errorf("'%%' was treated as a wildcard and matched %d rows", pct.Total)
		}
	})

	t.Run("offset paging advances and the total stays whole-filter", func(t *testing.T) {
		p1, err := repo.ListVendors(ctx, testTenant, domain.VendorFilter{}, 1, 0, false)
		if err != nil {
			t.Fatalf("page 1: %v", err)
		}
		// A summary is a whole-filter aggregate; pagination changes rows only. If this ever equals the
		// page size, the screen reports "1 vendor" over a register of 306 -- and the page COUNT, which
		// is derived from it, would read "Page 1 of 1" on a multi-page list.
		if p1.Total != 2 {
			t.Fatalf("page-1 total must be the whole-filter 2, got %d", p1.Total)
		}
		if len(p1.Vendors) != 1 {
			t.Fatalf("page 1 must hold exactly the requested 1 row, got %d", len(p1.Vendors))
		}

		p2, err := repo.ListVendors(ctx, testTenant, domain.VendorFilter{}, 1, 1, false)
		if err != nil {
			t.Fatalf("page 2: %v", err)
		}
		if p2.Total != 2 {
			t.Errorf("page-2 total must still be the whole-filter 2, got %d", p2.Total)
		}
		if len(p2.Vendors) != 1 || p2.Vendors[0].VendorID == p1.Vendors[0].VendorID {
			t.Error("page 2 repeated page 1's row")
		}
		// Paging BACK must return exactly page 1 -- the whole point of offset here.
		back, err := repo.ListVendors(ctx, testTenant, domain.VendorFilter{}, 1, 0, false)
		if err != nil {
			t.Fatalf("back to page 1: %v", err)
		}
		if len(back.Vendors) != 1 || back.Vendors[0].VendorID != p1.Vendors[0].VendorID {
			t.Error("paging back did not return the same first page")
		}
		// Past the end is an empty page, never an error and never a wrapped-around first page.
		past, err := repo.ListVendors(ctx, testTenant, domain.VendorFilter{}, 1, 500, false)
		if err != nil {
			t.Fatalf("past the end: %v", err)
		}
		if len(past.Vendors) != 0 || past.Total != 2 {
			t.Errorf("past the end must be empty with the total intact: rows=%d total=%d", len(past.Vendors), past.Total)
		}
	})

	t.Run("a caller who cannot read finance cannot erase it", func(t *testing.T) {
		// The drawer hides the payment block from a caller without VendorFinanceRead, so their form
		// submits BLANK bank fields. The update is a replace, so without preserveFinance that blank
		// would delete a bank account the operator was never allowed to see and had no way to know
		// existed. You cannot clear what you cannot read.
		seed, err := repo.CreateVendor(ctx, testTenant, domain.VendorWrite{
			RecordType: "Feed Agent", BusinessName: "Finance Preservation Co", PhoneNumber: "9111122222",
			Status: "Active", State: "MP", BankName: "HDFC Bank", AccountNo: "999888777",
			IFSCCode: "HDFC0009999", UPIID: "fin@upi", PANNumber: "ZZZZZ9999Z",
		}.Normalize(), "")
		if err != nil {
			t.Fatalf("seed: %v", err)
		}

		blankFinance := domain.VendorWrite{
			RecordType: "Feed Agent", BusinessName: "Finance Preservation Co", PhoneNumber: "9111122222",
			Status: "Active", State: "MP", // no bank/account/ifsc/upi/pan at all
		}.Normalize()

		// preserveFinance = true: the caller could not see the payment block.
		kept, err := repo.UpdateVendor(ctx, testTenant, seed.VendorID, blankFinance, seed.RowVersion, "", true)
		if err != nil {
			t.Fatalf("update with preserveFinance: %v", err)
		}
		// Re-read WITH finance, because UpdateVendor's own return is redacted for this caller.
		stored, err := repo.GetVendor(ctx, testTenant, seed.VendorID, true)
		if err != nil {
			t.Fatalf("re-read: %v", err)
		}
		for name, pair := range map[string][2]string{
			"bank_name":  {derefOr(stored.BankName), "HDFC Bank"},
			"account_no": {derefOr(stored.AccountNo), "999888777"},
			"ifsc_code":  {derefOr(stored.IFSCCode), "HDFC0009999"},
			"upi_id":     {derefOr(stored.UPIID), "fin@upi"},
			"pan_number": {derefOr(stored.PANNumber), "ZZZZZ9999Z"},
		} {
			if pair[0] != pair[1] {
				t.Errorf("%s was erased by a caller who could not read it: got %q want %q", name, pair[0], pair[1])
			}
		}
		// The rest of the row must still have been replaced normally.
		if kept.RowVersion != seed.RowVersion+1 {
			t.Errorf("row_version must still advance: %d", kept.RowVersion)
		}

		// preserveFinance = false: a caller who CAN see the block really is clearing it.
		if _, err := repo.UpdateVendor(ctx, testTenant, seed.VendorID, blankFinance, kept.RowVersion, "", false); err != nil {
			t.Fatalf("update without preserveFinance: %v", err)
		}
		cleared, err := repo.GetVendor(ctx, testTenant, seed.VendorID, true)
		if err != nil {
			t.Fatalf("re-read after clear: %v", err)
		}
		if cleared.BankName != nil || cleared.AccountNo != nil {
			t.Error("a caller who CAN read finance must still be able to clear it")
		}
	})

	t.Run("status changes on its own without touching other fields", func(t *testing.T) {
		seed, err := repo.CreateVendor(ctx, testTenant, domain.VendorWrite{
			RecordType: "Feed Agent", BusinessName: "Status Only Co", PhoneNumber: "9222233333",
			Status: "Active", State: "MP", BankName: "SBI", Details: "keep me", City: "Bhopal",
		}.Normalize(), "")
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
		updated, err := repo.UpdateVendorStatus(ctx, testTenant, seed.VendorID, domain.VendorStatusBanned, seed.RowVersion, "")
		if err != nil {
			t.Fatalf("status change: %v", err)
		}
		if updated.Status != domain.VendorStatusBanned || updated.RowVersion != seed.RowVersion+1 {
			t.Fatalf("status=%s rv=%d", updated.Status, updated.RowVersion)
		}
		stored, err := repo.GetVendor(ctx, testTenant, seed.VendorID, true)
		if err != nil {
			t.Fatalf("re-read: %v", err)
		}
		// Every other field must be exactly as seeded -- this is the whole reason the status write is
		// its own method rather than a full replace with one field changed.
		if derefOr(stored.BankName) != "SBI" || derefOr(stored.Details) != "keep me" || derefOr(stored.City) != "Bhopal" {
			t.Errorf("status change altered other fields: bank=%q details=%q city=%q",
				derefOr(stored.BankName), derefOr(stored.Details), derefOr(stored.City))
		}
		if _, err := repo.UpdateVendorStatus(ctx, testTenant, seed.VendorID, domain.VendorStatusActive, 99, ""); !errors.Is(err, ports.ErrVendorStaleWrite) {
			t.Errorf("stale status write must be refused, got %v", err)
		}
	})
}

func derefOr(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
