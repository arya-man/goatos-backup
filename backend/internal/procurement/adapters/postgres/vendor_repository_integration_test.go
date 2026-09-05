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

// TestVendorOptionsPicklistIsActiveOnlyAndNameOrdered exercises the picklist behind every "who is
// this sale for" dropdown against a real Postgres.
//
// Integration rather than unit for the same reason as the file above: everything worth breaking
// here lives in the SQL. The active-only predicate is a MEDICAL-grade correctness rule for
// commerce -- offering a banned counterparty as the buyer of a new sale is the defect -- and a
// fake repository would happily return whatever the test handed it.
func TestVendorOptionsPicklistIsActiveOnlyAndNameOrdered(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)

	// Lower-case "zebu" is seeded FIRST and must still sort LAST: the ordering is lower(name), so a
	// plain byte order would put every capitalised name after it and scramble the picker.
	seed := []struct {
		name   string
		status string
		city   string
	}{
		{"zebu Agro", domain.VendorStatusActive, "Hosur"},
		{"Anantapur Sheep Traders", domain.VendorStatusActive, "Anantapur"},
		{"Madur Livestock", domain.VendorStatusActive, ""},
		{"Retired Traders", domain.VendorStatusInactive, "Salem"},
		{"Never Again Agro", domain.VendorStatusBanned, "Erode"},
		{"Still Talking Agro", domain.VendorStatusNegotiating, "Mysore"},
	}
	for _, v := range seed {
		if _, err := repo.CreateVendor(ctx, testTenant, domain.VendorWrite{
			RecordType: "Sheep Agent", BusinessName: v.name, Status: v.status,
			State: "TN", City: v.city,
			// Payment instruments on every row, so the assertion below that the picklist carries
			// none of them is testing a real exclusion rather than an empty column.
			BankName: "HDFC Bank", AccountNo: "12345678901", UPIID: "someone@upi",
		}.Normalize(), ""); err != nil {
			t.Fatalf("seed vendor %q: %v", v.name, err)
		}
	}

	options, err := repo.ListVendorOptions(ctx, testTenant)
	if err != nil {
		t.Fatalf("list vendor options: %v", err)
	}

	got := make([]string, 0, len(options.Vendors))
	for _, v := range options.Vendors {
		got = append(got, v.BusinessName)
	}
	want := []string{"Anantapur Sheep Traders", "Madur Livestock", "zebu Agro"}
	if len(got) != len(want) {
		t.Fatalf("picklist = %v, want exactly the ACTIVE vendors %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("picklist = %v, want case-insensitive name order %v", got, want)
		}
	}
	if options.Truncated {
		t.Fatal("a register of three vendors must not report itself truncated")
	}

	byName := map[string]domain.VendorOption{}
	for _, v := range options.Vendors {
		byName[v.BusinessName] = v
	}
	// A vendor with no city keeps an EMPTY place rather than a NULL the picker would have to guess
	// at -- the label composition appends a location only when there is one.
	if madur := byName["Madur Livestock"]; madur.City != "" || madur.State != "TN" {
		t.Fatalf("missing city must read as empty, not null: %+v", madur)
	}
	if anantapur := byName["Anantapur Sheep Traders"]; anantapur.VendorID == "" || anantapur.RecordType != "Sheep Agent" {
		t.Fatalf("picklist row must carry its id and record type: %+v", anantapur)
	}
}

// TestVendorCatalogOffersBuyerRecordTypes pins the 2026-08-27 maintainer decision that added the
// buyer-side vendor categories.
//
// The register's 35 record types were imported from a legacy sheet describing only the SUPPLY side
// -- everyone the farm buys FROM. Since 000193 made every buyer a vendor row, there was no honest
// way to classify who the farm SELLS TO.
//
// The migration is asserted on a FRESH database on purpose. An earlier draft scoped the insert to
// tenants that already held a record_type vocabulary, which reads sensibly and is silently wrong:
// on a fresh database the vendor import has not run, so that vocabulary does not exist, the insert
// matches nothing, and because the importer only upserts the fixture's own 35 values these five
// would then never appear at all. This test fails on that draft.
func TestVendorCatalogOffersBuyerRecordTypes(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)

	entries, err := repo.ListVendorCatalog(ctx, testTenant, true)
	if err != nil {
		t.Fatalf("ListVendorCatalog: %v", err)
	}
	got := map[string]domain.VendorCatalogEntry{}
	for _, e := range entries {
		if e.Kind == domain.CatalogKindRecordType {
			got[e.Value] = e
		}
	}
	for _, want := range []string{"Agent", "Butcher", "Company", "Farmer", "Slaughter House"} {
		e, ok := got[want]
		if !ok {
			t.Errorf("record_type %q is not offered; the buyer categories must survive a fresh migrate with no vendor import", want)
			continue
		}
		// Label mirrors the value: these are typed by hand, not imported, so there is no separate
		// sheet spelling for the label to carry.
		if e.Label != want {
			t.Errorf("record_type %q label = %q, want %q", want, e.Label, want)
		}
		// A shared 100 keeps the five together AFTER the imported set, and survives the importer
		// renumbering the fixture's values to their 0..34 indexes on every run.
		if e.SortOrder != 100 {
			t.Errorf("record_type %q sort_order = %d, want 100", want, e.SortOrder)
		}
	}
	// Plural spellings must NOT be offered alongside the singular ones -- every other entry in the
	// vocabulary is singular and a dropdown holding both reads as two different categories.
	for _, banned := range []string{"Agents", "Butchers", "Farmers", "Companies"} {
		if _, ok := got[banned]; ok {
			t.Errorf("record_type %q is offered; the vocabulary is singular throughout", banned)
		}
	}
}

// TestTheTwoRegisterSidesPartitionTheWholeRegister pins the load-bearing property of the
// 2026-09-05 split: Procurement > Vendors and Sales > Vendors are COMPLEMENTARY. Every vendor
// appears on exactly one of them -- never both, and, crucially, never NEITHER.
//
// It is an integration test because the whole rule lives in SQL. The failure mode it guards against
// is invisible to a compiler and to any fake repository: an INNER-style predicate that requires a
// catalog row to exist at all. domain.Validate deliberately does not check record_type against the
// catalog (the vocabulary is business-managed and grows without a deploy), so an uncatalogued type
// is a REAL state -- and under such a predicate that vendor vanishes from both pages and is
// unfindable. Mutation-tested by dropping the NOT from the procurement branch: the uncatalogued
// vendor disappears and this test goes red on two assertions.
//
// (The predicate is spelled NOT EXISTS rather than NOT IN. That is a robustness preference, NOT a
// live bug guard: the subquery projects `value`, which is NOT NULL in the catalog's primary key,
// so the NOT IN null-swallowing trap cannot fire here -- confirmed by mutation, the IN/NOT IN form
// keeps this test green.)
//
// The third vendor below carries a record type that is in NO catalog row, which is what makes the
// "neither" case a real assertion rather than a hypothetical.
func TestTheTwoRegisterSidesPartitionTheWholeRegister(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)

	newVendor := func(recordType, name, phone string) {
		t.Helper()
		if _, err := repo.CreateVendor(ctx, testTenant, domain.VendorWrite{
			RecordType: recordType, BusinessName: name, ContactPersonName: "Contact",
			PhoneNumber: phone, Status: "Active", State: "KA", City: "Ballari",
		}.Normalize(), ""); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	newVendor("Sheep Agent", "Anantapur Sheep Supply", "9000000001") // catalogued, procurement side
	newVendor("Butcher", "Ballari Meat House", "9000000002")         // catalogued, sales side (000217 + 000256)
	newVendor("Kite Maker", "Nobody Catalogued This", "9000000003")  // in NO catalog row at all

	list := func(side string) map[string]bool {
		t.Helper()
		page, err := repo.ListVendors(ctx, testTenant, domain.VendorFilter{Side: side}, 100, 0, false)
		if err != nil {
			t.Fatalf("ListVendors(side=%q): %v", side, err)
		}
		// The page and the whole-filter total must range over the identical predicate set; they are
		// built from one function precisely so they cannot diverge, and this asserts it.
		if page.Total != len(page.Vendors) {
			t.Fatalf("side=%q total = %d but page holds %d rows; the count and the list disagree", side, page.Total, len(page.Vendors))
		}
		got := map[string]bool{}
		for _, v := range page.Vendors {
			got[v.BusinessName] = true
		}
		return got
	}

	whole := list("")
	procurement := list(domain.VendorSideProcurement)
	sales := list(domain.VendorSideSales)

	if !sales["Ballari Meat House"] || len(sales) != 1 {
		t.Errorf("sales side = %v, want exactly the butcher", sales)
	}
	if !procurement["Anantapur Sheep Supply"] {
		t.Error("the sheep agent is missing from the procurement side")
	}
	// The uncatalogued type falls to PROCUREMENT -- the status quo before the split, and the only
	// answer that keeps it reachable at all.
	if !procurement["Nobody Catalogued This"] {
		t.Error("an uncatalogued record type vanished from the procurement side; it must fail toward the status quo, not off both pages")
	}
	if procurement["Ballari Meat House"] {
		t.Error("the butcher is listed on the buying desk's register")
	}

	// The partition property, stated directly: neither side is empty by accident, the two are
	// disjoint, and together they are the whole register.
	if len(whole) != len(procurement)+len(sales) {
		t.Fatalf("whole register = %d rows, sides = %d + %d; the two sides must partition it exactly", len(whole), len(procurement), len(sales))
	}
	for name := range whole {
		if procurement[name] == sales[name] {
			t.Errorf("vendor %q is on both sides or on neither", name)
		}
	}
}

// TestCatalogNarrowsOnlyTheRecordTypes pins that a side narrows the RECORD TYPES and leaves every
// other vocabulary whole. A butcher and a feed stockist sit in the same states and are reached in
// the same towns, so narrowing states or cities per side would hide real values from one register's
// filters for no gain -- and the register's own city facet is derived from the vendors that exist,
// which has no side at all.
func TestCatalogNarrowsOnlyTheRecordTypes(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	entries, err := repo.ListVendorCatalog(ctx, testTenant, true)
	if err != nil {
		t.Fatalf("ListVendorCatalog: %v", err)
	}

	sides := map[string]string{}
	otherKindSides := map[string]int{}
	for _, e := range entries {
		if e.Kind == domain.CatalogKindRecordType {
			sides[e.Value] = e.RegisterSide
			continue
		}
		otherKindSides[e.RegisterSide]++
	}

	for _, value := range []string{"Agent", "Butcher", "Company", "Farmer", "Slaughter House"} {
		if sides[value] != domain.VendorSideSales {
			t.Errorf("record_type %q register_side = %q, want %q", value, sides[value], domain.VendorSideSales)
		}
	}
	// A representative supply-side type, and the migration's default: anything not named above stays
	// on the buying desk, so a record type added later by import lands where a sheet of suppliers
	// means it to land.
	for _, value := range []string{"Sheep Agent", "Transport Agent", "Vet Doctor", "Welder"} {
		if got, ok := sides[value]; ok && got != domain.VendorSideProcurement {
			t.Errorf("record_type %q register_side = %q, want %q", value, got, domain.VendorSideProcurement)
		}
	}
	// Every non-record_type entry carries the inert storage default. Readers ignore the field for
	// those kinds; this asserts nothing has started writing a side onto a vocabulary that has none.
	if n := otherKindSides[domain.VendorSideSales]; n != 0 {
		t.Errorf("%d non-record_type catalog entries carry the sales side; only record types have a side", n)
	}
}
