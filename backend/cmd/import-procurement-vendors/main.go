// Command import-procurement-vendors loads the one-time vendor-register export into Postgres.
//
// The source is the committed fixture fixtures/procurement-vendors-2026-08-12/vendors.json,
// exported once from the "Vendors DB [Procurement]" Google Sheet. It is NOT a sync: after this runs,
// Postgres is canonical and the sheet is not read again. Re-running is safe and idempotent -- a
// vendor is matched on its natural key and updated in place rather than duplicated.
//
//	go run ./cmd/import-procurement-vendors \
//	  -database-url "postgres://..." \
//	  -tenant 00000000-0000-4000-8000-000000000001 \
//	  -fixture ../fixtures/procurement-vendors-2026-08-12/vendors.json
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

type fixtureFile struct {
	Source  map[string]string   `json:"source"`
	Catalog map[string][]string `json:"catalog"`
	Vendors []fixtureVendor     `json:"vendors"`
}

// fixtureVendor mirrors the export exactly.
//
// aadhaar_number and cheque_copy are declared but never imported: both are empty in all 307 rows,
// and the sheet's "Pan number" column turned out to hold a =PROPER(business_name) helper rather
// than any PAN. Declaring them keeps DisallowUnknownFields able to reject a fixture that has
// drifted, without pretending the data exists.
type fixtureVendor struct {
	RecordType        string `json:"record_type"`
	BusinessName      string `json:"business_name"`
	ContactPersonName string `json:"contact_person_name"`
	PhoneNumber       string `json:"phone_number"`
	Breed             string `json:"breed"`
	Status            string `json:"status"`
	Feed              string `json:"feed"`
	FilteredStock     string `json:"filtered_stock"`
	PricePerGoat      string `json:"price_per_goat"`
	ReadyToFiltered   string `json:"ready_to_filtered"`
	ETAAfterOrder     string `json:"eta_after_order"`
	Details           string `json:"details"`
	State             string `json:"state"`
	City              string `json:"city"`
	BankName          string `json:"bank_name"`
	AccountNo         string `json:"account_no"`
	IFSCCode          string `json:"ifsc_code"`
	UPIID             string `json:"upi_id"`
	Comments          string `json:"comments"`
	AadhaarNumber     string `json:"aadhaar_number"`
	PANNumber         string `json:"pan_number"`
	ChequeCopy        string `json:"cheque_copy"`
	SourceRow         int    `json:"source_row"`
}

func main() {
	var (
		databaseURL = flag.String("database-url", os.Getenv("DATABASE_URL"), "Postgres connection string")
		tenantID    = flag.String("tenant", "", "tenant uuid to import into")
		fixturePath = flag.String("fixture", "../fixtures/procurement-vendors-2026-08-12/vendors.json", "path to the exported fixture")
		dryRun      = flag.Bool("dry-run", false, "parse, normalize and report without writing")
	)
	flag.Parse()

	if strings.TrimSpace(*tenantID) == "" {
		log.Fatal("-tenant is required")
	}
	// A dry run parses, normalizes and reports without connecting, so it is usable as a fixture
	// check in CI where no database exists.
	if !*dryRun && strings.TrimSpace(*databaseURL) == "" {
		log.Fatal("-database-url (or DATABASE_URL) is required")
	}

	fixture, err := loadFixture(*fixturePath)
	if err != nil {
		log.Fatalf("load fixture: %v", err)
	}

	rows, collapsed, err := prepare(fixture)
	if err != nil {
		log.Fatalf("prepare: %v", err)
	}
	for _, c := range collapsed {
		log.Printf("collapsed duplicate: %s", c)
	}
	log.Printf("prepared %d vendors from %d source rows (%d collapsed)", len(rows), len(fixture.Vendors), len(collapsed))

	if *dryRun {
		log.Print("dry run: nothing written")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	pool, err := pgxpool.New(ctx, *databaseURL)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	if err := importAll(ctx, pool, *tenantID, fixture, rows); err != nil {
		log.Fatalf("import: %v", err)
	}
	log.Printf("imported %d vendors and %d catalog entries", len(rows), countCatalog(fixture.Catalog))
}

// loadFixture reads the export and REJECTS an unknown field.
//
// encoding/json drops unmatched keys silently, so a fixture whose shape has drifted would import
// partially and still report success -- the exact accept-and-discard failure the repo's fixture
// rule exists to stop.
func loadFixture(path string) (fixtureFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fixtureFile{}, err
	}
	var f fixtureFile
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return fixtureFile{}, fmt.Errorf("decode %s: %w", path, err)
	}
	if len(f.Vendors) == 0 {
		return fixtureFile{}, fmt.Errorf("%s contains no vendors", path)
	}
	return f, nil
}

// skipSentinel reports a cell that means "nothing recorded" rather than a value.
//
// The Slack intake flow treats exactly these as a SKIP (vendorIsSkipReply_ in the Apps Script:
// ”, 'skip', '-', 'na', 'n/a'), and an operator who skipped a question in Slack leaves the sheet
// cell holding that sentinel. Importing it literally puts a vendor on screen whose contact person
// is "-" and whose bank is "-", which reads as data rather than as absence.
//
// Applied ONLY on import, where it is interpreting the sheet's own convention. It is deliberately
// not applied to admin-web input: silently rewriting what a person typed into a form is a different
// and less defensible thing.
func skipSentinel(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "-", "--", "na", "n/a", "nil", "none", "skip":
		return true
	default:
		return false
	}
}

// blankIfSkipped collapses a sentinel to the empty string, which the writer then stores as NULL.
func blankIfSkipped(v string) string {
	if skipSentinel(v) {
		return ""
	}
	return v
}

type preparedVendor struct {
	write     domain.VendorWrite
	sourceRow int
}

// prepare normalizes every source row and collapses true duplicates.
//
// A duplicate is the natural key the database enforces: business name + record type + state +
// PHONE DIGITS. Four of the five collisions in the export are legitimate (one business, several
// contacts on different numbers) and survive; only the genuine double-entry collapses.
//
// When two rows do collapse, the RICHER one wins -- the row with more populated fields. The real
// case is 'Ajay Tomar', entered twice on the same number with the contact name filled in on only
// one of them; keeping the sparser row would discard a fact the farm actually recorded.
func prepare(f fixtureFile) ([]preparedVendor, []string, error) {
	type slot struct {
		prepared preparedVendor
		filled   int
	}
	index := map[string]slot{}
	order := []string{}
	collapsed := []string{}

	for _, v := range f.Vendors {
		status, ok := domain.NormalizeStatus(v.Status)
		if !ok {
			// A status the CHECK constraint would reject. Failing loudly beats importing the row with
			// an invented status, which would misreport whether the farm may buy from this vendor.
			return nil, nil, fmt.Errorf("row %d (%s): unrecognised status %q", v.SourceRow, v.BusinessName, v.Status)
		}
		stock, err := optionalInt(v.FilteredStock)
		if err != nil {
			return nil, nil, fmt.Errorf("row %d: filtered_stock %q: %w", v.SourceRow, v.FilteredStock, err)
		}
		eta, err := optionalInt(v.ETAAfterOrder)
		if err != nil {
			return nil, nil, fmt.Errorf("row %d: eta_after_order %q: %w", v.SourceRow, v.ETAAfterOrder, err)
		}

		write := domain.VendorWrite{
			RecordType: v.RecordType, BusinessName: v.BusinessName,
			// Optional free-text fields run through the sheet's own skip convention first.
			// business_name / record_type / state are NOT sentinel-checked: they are required, so a
			// sentinel there is a broken source row that must fail validation loudly, not vanish.
			ContactPersonName: blankIfSkipped(v.ContactPersonName), PhoneNumber: blankIfSkipped(v.PhoneNumber),
			Breed: blankIfSkipped(v.Breed), Feed: blankIfSkipped(v.Feed), Status: status,
			FilteredStock: stock, PricePerGoat: optionalDecimal(v.PricePerGoat),
			ReadyToFiltered: blankIfSkipped(v.ReadyToFiltered), ETAAfterOrderDays: eta,
			Details: blankIfSkipped(v.Details), State: v.State, City: blankIfSkipped(v.City),
			BankName: blankIfSkipped(v.BankName), AccountNo: blankIfSkipped(v.AccountNo),
			IFSCCode: blankIfSkipped(v.IFSCCode),
			UPIID:    blankIfSkipped(v.UPIID), Comments: blankIfSkipped(v.Comments),
			// PANNumber is deliberately not carried: the sheet column labelled "Pan number" holds a
			// =PROPER(business_name) formula helper, not a PAN.
		}.Normalize()

		if err := write.Validate(); err != nil {
			return nil, nil, fmt.Errorf("row %d (%s): %w", v.SourceRow, v.BusinessName, err)
		}

		key := naturalKey(write)
		candidate := slot{prepared: preparedVendor{write: write, sourceRow: v.SourceRow}, filled: countFilled(write)}
		existing, seen := index[key]
		if !seen {
			index[key] = candidate
			order = append(order, key)
			continue
		}
		keep, drop := existing, candidate
		if candidate.filled > existing.filled {
			keep, drop = candidate, existing
		}
		index[key] = keep
		collapsed = append(collapsed, fmt.Sprintf("%q (%s, %s): kept source row %d, dropped row %d",
			write.BusinessName, write.RecordType, write.State, keep.prepared.sourceRow, drop.prepared.sourceRow))
	}

	out := make([]preparedVendor, 0, len(order))
	for _, key := range order {
		out = append(out, index[key].prepared)
	}
	return out, collapsed, nil
}

// naturalKey mirrors procurement_vendors_natural_uq exactly, including the digits-only phone
// comparison. If these two ever disagree the importer will send the database a row it believes is
// new and be refused.
func naturalKey(w domain.VendorWrite) string {
	return strings.Join([]string{
		strings.ToLower(strings.TrimSpace(w.BusinessName)),
		w.RecordType,
		w.State,
		domain.NormalizePhoneDigits(w.PhoneNumber),
	}, "\x1f")
}

func countFilled(w domain.VendorWrite) int {
	n := 0
	for _, v := range []string{
		w.ContactPersonName, w.PhoneNumber, w.Breed, w.Feed, w.ReadyToFiltered, w.Details,
		w.City, w.BankName, w.AccountNo, w.IFSCCode, w.UPIID, w.PANNumber, w.Comments,
	} {
		if strings.TrimSpace(v) != "" {
			n++
		}
	}
	if w.FilteredStock != nil {
		n++
	}
	if w.ETAAfterOrderDays != nil {
		n++
	}
	if w.PricePerGoat != nil {
		n++
	}
	return n
}

func optionalInt(raw string) (*int, error) {
	trimmed := strings.ReplaceAll(strings.TrimSpace(raw), ",", "")
	if trimmed == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(trimmed)
	if err != nil {
		return nil, fmt.Errorf("not a whole number")
	}
	if n < 0 {
		return nil, fmt.Errorf("negative")
	}
	return &n, nil
}

func optionalDecimal(raw string) *string {
	trimmed := strings.ReplaceAll(strings.TrimSpace(raw), ",", "")
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func countCatalog(catalog map[string][]string) int {
	n := 0
	for _, values := range catalog {
		n += len(values)
	}
	return n
}

// importAll writes the catalog and the vendors in ONE transaction.
//
// They go together because a vendor whose city is not in the catalog cannot be re-selected in its
// own edit form. Importing vendors against a half-written vocabulary would leave rows the UI can
// display but not save.
func importAll(ctx context.Context, pool *pgxpool.Pool, tenantID string, f fixtureFile, rows []preparedVendor) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Both writes are SET-BASED. A per-row Exec inside a range loop is the N+1 shape the repo
	// bans: at 306 vendors it is 306 round trips where one suffices, and the same code is what a
	// future 5,000-row supplier import would run.
	catKinds, catValues, catLabels, catOrders := []string{}, []string{}, []string{}, []int32{}
	kinds := make([]string, 0, len(f.Catalog))
	for kind := range f.Catalog {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		for i, value := range f.Catalog[kind] {
			storedValue, label := value, value
			if kind == domain.CatalogKindStatus {
				// Statuses are stored in their canonical lower-case form so the catalog matches the
				// values on the vendor rows; the sheet's spelling becomes the label.
				normalized, ok := domain.NormalizeStatus(value)
				if !ok {
					return fmt.Errorf("catalog status %q is not a recognised status", value)
				}
				storedValue = normalized
			}
			catKinds = append(catKinds, kind)
			catValues = append(catValues, storedValue)
			catLabels = append(catLabels, strings.TrimSpace(label))
			catOrders = append(catOrders, int32(i))
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO public.procurement_vendor_catalog (tenant_id, kind, value, label, sort_order, is_active)
		SELECT $1, k, v, l, o, true
		FROM unnest($2::text[], $3::text[], $4::text[], $5::int[]) AS s(k, v, l, o)
		ON CONFLICT (tenant_id, kind, value) DO UPDATE
		SET label = EXCLUDED.label, sort_order = EXCLUDED.sort_order, updated_at = now()`,
		tenantID, catKinds, catValues, catLabels, catOrders); err != nil {
		return fmt.Errorf("catalog upsert: %w", err)
	}

	cols := newVendorColumns(len(rows))
	for _, row := range rows {
		cols.append(row)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO public.procurement_vendors (
			tenant_id, record_type, business_name, contact_person_name, phone_number,
			breed, feed, status, filtered_stock, price_per_goat, ready_to_filtered,
			eta_after_order_days, details, state, city,
			bank_name, account_no, ifsc_code, upi_id, comments, source_row
		)
		SELECT $1, record_type, business_name, contact_person_name, phone_number,
		       breed, feed, status, filtered_stock, price_per_goat::numeric, ready_to_filtered,
		       eta_after_order_days, details, state, city,
		       bank_name, account_no, ifsc_code, upi_id, comments, source_row
		FROM unnest(
			$2::text[], $3::text[], $4::text[], $5::text[],
			$6::text[], $7::text[], $8::text[], $9::int[], $10::text[], $11::text[],
			$12::int[], $13::text[], $14::text[], $15::text[],
			$16::text[], $17::text[], $18::text[], $19::text[], $20::text[], $21::int[]
		) AS s(record_type, business_name, contact_person_name, phone_number,
		       breed, feed, status, filtered_stock, price_per_goat, ready_to_filtered,
		       eta_after_order_days, details, state, city,
		       bank_name, account_no, ifsc_code, upi_id, comments, source_row)
		ON CONFLICT (tenant_id, lower(btrim(business_name)), record_type, state,
		             coalesce(regexp_replace(phone_number, '\D', '', 'g'), ''))
		DO UPDATE SET
			contact_person_name = EXCLUDED.contact_person_name,
			breed = EXCLUDED.breed,
			feed = EXCLUDED.feed,
			status = EXCLUDED.status,
			filtered_stock = EXCLUDED.filtered_stock,
			price_per_goat = EXCLUDED.price_per_goat,
			ready_to_filtered = EXCLUDED.ready_to_filtered,
			eta_after_order_days = EXCLUDED.eta_after_order_days,
			details = EXCLUDED.details,
			city = EXCLUDED.city,
			bank_name = EXCLUDED.bank_name,
			account_no = EXCLUDED.account_no,
			ifsc_code = EXCLUDED.ifsc_code,
			upi_id = EXCLUDED.upi_id,
			comments = EXCLUDED.comments,
			source_row = EXCLUDED.source_row,
			updated_at = now(),
			row_version = public.procurement_vendors.row_version + 1`,
		tenantID, cols.recordType, cols.businessName, cols.contact, cols.phone,
		cols.breed, cols.feed, cols.status, cols.stock, cols.price, cols.ready,
		cols.eta, cols.details, cols.state, cols.city,
		cols.bank, cols.account, cols.ifsc, cols.upi, cols.comments, cols.sourceRow,
	); err != nil {
		return fmt.Errorf("vendor upsert: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}
	// Fresh planner statistics: the table went from empty to a few hundred rows, and a stale
	// empty-table estimate makes the planner pick a sequential scan over the trigram index.
	if _, err := pool.Exec(ctx, `ANALYZE public.procurement_vendors, public.procurement_vendor_catalog`); err != nil {
		return fmt.Errorf("analyze: %w", err)
	}
	return nil
}

// vendorColumns transposes prepared rows into one array per column, which is what the set-based
// INSERT ... SELECT unnest(...) needs.
//
// Optional text is []*string so an empty value becomes a real NULL rather than "". Two spellings of
// "no value" would make the natural key's coalesce(phone) ambiguous.
type vendorColumns struct {
	recordType, businessName, status, state []string
	contact, phone, breed, feed, ready      []*string
	details, city, bank, account, ifsc, upi []*string
	comments, price                         []*string
	stock, eta, sourceRow                   []*int32
}

func newVendorColumns(n int) *vendorColumns {
	return &vendorColumns{
		recordType: make([]string, 0, n), businessName: make([]string, 0, n),
		status: make([]string, 0, n), state: make([]string, 0, n),
	}
}

func nullText(v string) *string {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func nullInt(v *int) *int32 {
	if v == nil {
		return nil
	}
	n := int32(*v)
	return &n
}

func (c *vendorColumns) append(row preparedVendor) {
	w := row.write
	c.recordType = append(c.recordType, w.RecordType)
	c.businessName = append(c.businessName, w.BusinessName)
	c.status = append(c.status, w.Status)
	c.state = append(c.state, w.State)
	c.contact = append(c.contact, nullText(w.ContactPersonName))
	c.phone = append(c.phone, nullText(w.PhoneNumber))
	c.breed = append(c.breed, nullText(w.Breed))
	c.feed = append(c.feed, nullText(w.Feed))
	c.ready = append(c.ready, nullText(w.ReadyToFiltered))
	c.details = append(c.details, nullText(w.Details))
	c.city = append(c.city, nullText(w.City))
	c.bank = append(c.bank, nullText(w.BankName))
	c.account = append(c.account, nullText(w.AccountNo))
	c.ifsc = append(c.ifsc, nullText(w.IFSCCode))
	c.upi = append(c.upi, nullText(w.UPIID))
	c.comments = append(c.comments, nullText(w.Comments))
	if w.PricePerGoat == nil {
		c.price = append(c.price, nil)
	} else {
		c.price = append(c.price, nullText(*w.PricePerGoat))
	}
	c.stock = append(c.stock, nullInt(w.FilteredStock))
	c.eta = append(c.eta, nullInt(w.ETAAfterOrderDays))
	sr := int32(row.sourceRow)
	c.sourceRow = append(c.sourceRow, &sr)
}
