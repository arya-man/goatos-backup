// Command import-sales-db loads the one-time "Sales DB" sheet export into Postgres.
//
// The source is the committed fixture fixtures/sales-db-2026-08-17/sales-db.json, exported once
// from the maintainer's Sales DB Google Sheet. It is NOT a sync: after this runs, Postgres is
// canonical and the sheet is not read again. Re-running is safe and idempotent -- every row is
// keyed on (tenant_id, source_row_no), its 1-based position in the exported tab, and updated in
// place rather than duplicated.
//
//	go run ./cmd/import-sales-db \
//	  -database-url "postgres://..." \
//	  -tenant 00000000-0000-4000-8000-000000000001 \
//	  -fixture ../fixtures/sales-db-2026-08-17/sales-db.json
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/sales/domain"
)

// fixtureFile mirrors the export envelope exactly. DisallowUnknownFields rejects a drifted
// fixture, so every key present in the export must be declared here even when it is only
// provenance prose.
type fixtureFile struct {
	ExportedAt string `json:"exported_at"`
	Source     string `json:"source"`
	Note       string `json:"note"`

	Deals            []fixtureDeal      `json:"deals"`
	BuyerLeads       []fixtureBuyerLead `json:"buyer_leads"`
	FPOLeads         []fixtureFPOLead   `json:"fpo_leads"`
	SoldAnimalTags   []fixtureSoldTag   `json:"sold_animal_tags"`
	WeightAudit      []fixtureAuditRow  `json:"weight_audit"`
	MarketBenchmarks []fixtureBenchmark `json:"market_benchmarks"`
}

type fixtureDeal struct {
	SaleDate         string   `json:"sale_date"`
	Farm             string   `json:"farm"`
	SourceSalesID    *int     `json:"source_sales_id"`
	SourcePurchaseID *int     `json:"source_purchase_id"`
	BuyerName        string   `json:"buyer_name"`
	BuyerPlace       *string  `json:"buyer_place"`
	ProductType      string   `json:"product_type"`
	Breed            string   `json:"breed"`
	AnimalCount      *float64 `json:"animal_count"`
	MaleCount        *float64 `json:"male_count"`
	FemaleCount      *float64 `json:"female_count"`
	TotalWeightKg    *float64 `json:"total_weight_kg"`
	AdvanceAmount    *float64 `json:"advance_amount"`
	SalesValue       *float64 `json:"sales_value"`
	Status           string   `json:"status"`
	Feedback         *string  `json:"feedback"`
	Comments         *string  `json:"comments"`
}

type fixtureBuyerLead struct {
	RecordedDate  *string `json:"recorded_date"`
	Farm          *string `json:"farm"`
	SourceSalesID *int    `json:"source_sales_id"`
	BuyerName     string  `json:"buyer_name"`
	BuyerPlace    *string `json:"buyer_place"`
	AnimalType    *string `json:"animal_type"`
	Breed         *string `json:"breed"`
	CallStatus    *string `json:"call_status"`
}

type fixtureFPOLead struct {
	FPOName    string  `json:"fpo_name"`
	Crops      *string `json:"crops"`
	District   *string `json:"district"`
	Taluk      *string `json:"taluk"`
	State      *string `json:"state"`
	CallStatus *string `json:"call_status"`
}

type fixtureSoldTag struct {
	AnimalLabel   string   `json:"animal_label"`
	TagNumber     *string  `json:"tag_number"`
	WeightKg      *float64 `json:"weight_kg"`
	Farm          *string  `json:"farm"`
	SourceSalesID *int     `json:"source_sales_id"`
}

type fixtureAuditRow struct {
	VideoWeightKg float64 `json:"video_weight_kg"`
	BookWeightKg  float64 `json:"book_weight_kg"`
	FarmBorn      bool    `json:"farm_born"`
	TagNumber     *string `json:"tag_number"`
}

type fixtureBenchmark struct {
	Market           string   `json:"market"`
	Category         *string  `json:"category"`
	Breed            string   `json:"breed"`
	Source           *string  `json:"source"`
	ExFarmRate       *string  `json:"ex_farm_rate"`
	TransportRate    *string  `json:"transport_rate"`
	LandingCostPerKg *float64 `json:"landing_cost_per_kg"`
}

func main() {
	var (
		databaseURL = flag.String("database-url", os.Getenv("DATABASE_URL"), "Postgres connection string")
		tenantID    = flag.String("tenant", "", "tenant uuid to import into")
		fixturePath = flag.String("fixture", "fixtures/sales-db-2026-08-17/sales-db.json", "path to the exported fixture")
		dryRun      = flag.Bool("dry-run", false, "parse, validate and report without writing")
	)
	flag.Parse()

	if strings.TrimSpace(*tenantID) == "" {
		log.Fatal("-tenant is required")
	}
	if !*dryRun && strings.TrimSpace(*databaseURL) == "" {
		log.Fatal("-database-url (or DATABASE_URL) is required")
	}

	fixture, err := loadFixture(*fixturePath)
	if err != nil {
		log.Fatalf("load fixture: %v", err)
	}
	if err := validateFixture(fixture); err != nil {
		log.Fatalf("validate fixture: %v", err)
	}
	log.Printf("fixture: %d deals, %d buyer leads, %d fpo leads, %d sold tags, %d weight audits, %d benchmarks",
		len(fixture.Deals), len(fixture.BuyerLeads), len(fixture.FPOLeads),
		len(fixture.SoldAnimalTags), len(fixture.WeightAudit), len(fixture.MarketBenchmarks))

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

	if err := importAll(ctx, pool, *tenantID, fixture); err != nil {
		log.Fatalf("import: %v", err)
	}
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
	if len(f.Deals) == 0 {
		return fixtureFile{}, fmt.Errorf("%s contains no deals", path)
	}
	return f, nil
}

// validateFixture fails loudly on any value the CHECK constraints would reject, naming the row, so
// a broken export never half-imports.
func validateFixture(f fixtureFile) error {
	for i, d := range f.Deals {
		row := i + 1
		if !domain.IsFarm(d.Farm) {
			return fmt.Errorf("deal row %d (%s): unrecognised farm %q", row, d.BuyerName, d.Farm)
		}
		if !domain.IsProductType(d.ProductType) {
			return fmt.Errorf("deal row %d (%s): unrecognised product type %q", row, d.BuyerName, d.ProductType)
		}
		if !domain.IsStatus(d.Status) {
			return fmt.Errorf("deal row %d (%s): unrecognised status %q", row, d.BuyerName, d.Status)
		}
		if _, err := time.Parse("2006-01-02", d.SaleDate); err != nil {
			return fmt.Errorf("deal row %d (%s): bad sale_date %q", row, d.BuyerName, d.SaleDate)
		}
		if strings.TrimSpace(d.BuyerName) == "" {
			return fmt.Errorf("deal row %d: blank buyer_name", row)
		}
		if strings.TrimSpace(d.Breed) == "" {
			return fmt.Errorf("deal row %d (%s): blank breed", row, d.BuyerName)
		}
	}
	for i, l := range f.BuyerLeads {
		if strings.TrimSpace(l.BuyerName) == "" {
			return fmt.Errorf("buyer lead row %d: blank buyer_name", i+1)
		}
	}
	for i, l := range f.FPOLeads {
		if strings.TrimSpace(l.FPOName) == "" {
			return fmt.Errorf("fpo lead row %d: blank fpo_name", i+1)
		}
	}
	for i, t := range f.SoldAnimalTags {
		if strings.TrimSpace(t.AnimalLabel) == "" {
			return fmt.Errorf("sold tag row %d: blank animal_label", i+1)
		}
	}
	for i, b := range f.MarketBenchmarks {
		if strings.TrimSpace(b.Breed) == "" {
			return fmt.Errorf("benchmark row %d: blank breed", i+1)
		}
	}
	return nil
}

// firstNumber extracts the leading number out of a market string like
// 'Chennai -Sheep - 370 Rs Per kg' -> 370. Parsed at IMPORT time so the read path never parses
// prose; a market string with no number stores NULL.
var firstNumberPattern = regexp.MustCompile(`\d+(?:\.\d+)?`)

func firstNumber(raw string) *float64 {
	match := firstNumberPattern.FindString(raw)
	if match == "" {
		return nil
	}
	n, err := strconv.ParseFloat(match, 64)
	if err != nil {
		return nil
	}
	return &n
}

// importAll writes every table in ONE transaction so a partial export never leaves the page
// showing deals without their evidence panels. Every write is SET-BASED (unnest), never a per-row
// Exec in a loop, and every upsert is keyed on the (tenant_id, source_row_no) partial unique index
// so re-running updates in place.
func importAll(ctx context.Context, pool *pgxpool.Pool, tenantID string, f fixtureFile) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := importDeals(ctx, tx, tenantID, f.Deals); err != nil {
		return err
	}
	if err := importBuyerLeads(ctx, tx, tenantID, f.BuyerLeads); err != nil {
		return err
	}
	if err := importFPOLeads(ctx, tx, tenantID, f.FPOLeads); err != nil {
		return err
	}
	if err := importSoldTags(ctx, tx, tenantID, f.SoldAnimalTags); err != nil {
		return err
	}
	if err := importWeightAudit(ctx, tx, tenantID, f.WeightAudit); err != nil {
		return err
	}
	if err := importBenchmarks(ctx, tx, tenantID, f.MarketBenchmarks); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	// Fresh planner statistics: the tables went from empty to a few hundred rows, and a stale
	// empty-table estimate would mislead the planner on the overview rollups.
	if _, err := pool.Exec(ctx, `ANALYZE public.sales_deals, public.sales_buyer_leads, public.sales_fpo_leads, public.sales_sold_animal_tags, public.sales_weight_audit, public.sales_market_benchmarks`); err != nil {
		return fmt.Errorf("analyze: %w", err)
	}

	log.Printf("imported deals=%d buyer_leads=%d fpo_leads=%d sold_animal_tags=%d weight_audit=%d market_benchmarks=%d",
		len(f.Deals), len(f.BuyerLeads), len(f.FPOLeads), len(f.SoldAnimalTags), len(f.WeightAudit), len(f.MarketBenchmarks))
	return nil
}

func rowNos(n int) []int32 {
	out := make([]int32, n)
	for i := range out {
		out[i] = int32(i + 1) // 1-based sheet position: the import natural key
	}
	return out
}

func nullTextPtr(v *string) *string {
	if v == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*v)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func intPtr32(v *int) *int32 {
	if v == nil {
		return nil
	}
	n := int32(*v)
	return &n
}

func importDeals(ctx context.Context, tx pgx.Tx, tenantID string, deals []fixtureDeal) error {
	n := len(deals)
	saleDates := make([]string, 0, n)
	farms := make([]string, 0, n)
	srcSales := make([]*int32, 0, n)
	srcPurchase := make([]*int32, 0, n)
	buyers := make([]string, 0, n)
	places := make([]*string, 0, n)
	products := make([]string, 0, n)
	breeds := make([]string, 0, n)
	animals := make([]*float64, 0, n)
	males := make([]*float64, 0, n)
	females := make([]*float64, 0, n)
	weights := make([]*float64, 0, n)
	advances := make([]*float64, 0, n)
	values := make([]float64, 0, n)
	statuses := make([]string, 0, n)
	feedbacks := make([]*string, 0, n)
	comments := make([]*string, 0, n)
	for _, d := range deals {
		saleDates = append(saleDates, d.SaleDate)
		farms = append(farms, d.Farm)
		srcSales = append(srcSales, intPtr32(d.SourceSalesID))
		srcPurchase = append(srcPurchase, intPtr32(d.SourcePurchaseID))
		buyers = append(buyers, strings.TrimSpace(d.BuyerName))
		places = append(places, nullTextPtr(d.BuyerPlace))
		products = append(products, d.ProductType)
		breeds = append(breeds, strings.TrimSpace(d.Breed))
		animals = append(animals, d.AnimalCount)
		males = append(males, d.MaleCount)
		females = append(females, d.FemaleCount)
		weights = append(weights, d.TotalWeightKg)
		advances = append(advances, d.AdvanceAmount)
		// sales_value is NOT NULL DEFAULT 0 in storage; a sheet row with no value (a failed deal)
		// stores 0 explicitly rather than inventing a number.
		value := 0.0
		if d.SalesValue != nil {
			value = *d.SalesValue
		}
		values = append(values, value)
		statuses = append(statuses, d.Status)
		feedbacks = append(feedbacks, nullTextPtr(d.Feedback))
		comments = append(comments, nullTextPtr(d.Comments))
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO public.sales_deals (
			tenant_id, source_row_no, sale_date, farm, source_sales_id, source_purchase_id,
			buyer_name, buyer_place, product_type, breed,
			animal_count, male_count, female_count, total_weight_kg,
			advance_amount, sales_value, status, feedback, comments
		)
		SELECT $1, row_no, sale_date::date, farm, source_sales_id, source_purchase_id,
		       buyer_name, buyer_place, product_type, breed,
		       animal_count, male_count, female_count, total_weight_kg,
		       advance_amount, sales_value, status, feedback, comments
		FROM unnest(
			$2::int[], $3::text[], $4::text[], $5::int[], $6::int[],
			$7::text[], $8::text[], $9::text[], $10::text[],
			$11::numeric[], $12::numeric[], $13::numeric[], $14::numeric[],
			$15::numeric[], $16::numeric[], $17::text[], $18::text[], $19::text[]
		) AS s(row_no, sale_date, farm, source_sales_id, source_purchase_id,
		       buyer_name, buyer_place, product_type, breed,
		       animal_count, male_count, female_count, total_weight_kg,
		       advance_amount, sales_value, status, feedback, comments)
		ON CONFLICT (tenant_id, source_row_no) WHERE source_row_no IS NOT NULL
		DO UPDATE SET
			sale_date = EXCLUDED.sale_date,
			farm = EXCLUDED.farm,
			source_sales_id = EXCLUDED.source_sales_id,
			source_purchase_id = EXCLUDED.source_purchase_id,
			buyer_name = EXCLUDED.buyer_name,
			buyer_place = EXCLUDED.buyer_place,
			product_type = EXCLUDED.product_type,
			breed = EXCLUDED.breed,
			animal_count = EXCLUDED.animal_count,
			male_count = EXCLUDED.male_count,
			female_count = EXCLUDED.female_count,
			total_weight_kg = EXCLUDED.total_weight_kg,
			advance_amount = EXCLUDED.advance_amount,
			sales_value = EXCLUDED.sales_value,
			status = EXCLUDED.status,
			feedback = EXCLUDED.feedback,
			comments = EXCLUDED.comments,
			updated_at = now()`,
		tenantID, rowNos(n), saleDates, farms, srcSales, srcPurchase,
		buyers, places, products, breeds,
		animals, males, females, weights,
		advances, values, statuses, feedbacks, comments)
	if err != nil {
		return fmt.Errorf("deals upsert: %w", err)
	}
	return nil
}

func importBuyerLeads(ctx context.Context, tx pgx.Tx, tenantID string, leads []fixtureBuyerLead) error {
	n := len(leads)
	dates := make([]*string, 0, n)
	farms := make([]*string, 0, n)
	srcSales := make([]*int32, 0, n)
	buyers := make([]string, 0, n)
	places := make([]*string, 0, n)
	animalTypes := make([]*string, 0, n)
	breeds := make([]*string, 0, n)
	callStatuses := make([]*string, 0, n)
	for _, l := range leads {
		dates = append(dates, nullTextPtr(l.RecordedDate))
		farms = append(farms, nullTextPtr(l.Farm))
		srcSales = append(srcSales, intPtr32(l.SourceSalesID))
		buyers = append(buyers, strings.TrimSpace(l.BuyerName))
		places = append(places, nullTextPtr(l.BuyerPlace))
		animalTypes = append(animalTypes, nullTextPtr(l.AnimalType))
		breeds = append(breeds, nullTextPtr(l.Breed))
		callStatuses = append(callStatuses, nullTextPtr(l.CallStatus))
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO public.sales_buyer_leads (
			tenant_id, source_row_no, recorded_date, farm, source_sales_id,
			buyer_name, buyer_place, animal_type, breed, call_status
		)
		SELECT $1, row_no, recorded_date::date, farm, source_sales_id,
		       buyer_name, buyer_place, animal_type, breed, call_status
		FROM unnest(
			$2::int[], $3::text[], $4::text[], $5::int[],
			$6::text[], $7::text[], $8::text[], $9::text[], $10::text[]
		) AS s(row_no, recorded_date, farm, source_sales_id,
		       buyer_name, buyer_place, animal_type, breed, call_status)
		ON CONFLICT (tenant_id, source_row_no) WHERE source_row_no IS NOT NULL
		DO UPDATE SET
			recorded_date = EXCLUDED.recorded_date,
			farm = EXCLUDED.farm,
			source_sales_id = EXCLUDED.source_sales_id,
			buyer_name = EXCLUDED.buyer_name,
			buyer_place = EXCLUDED.buyer_place,
			animal_type = EXCLUDED.animal_type,
			breed = EXCLUDED.breed,
			call_status = EXCLUDED.call_status,
			updated_at = now()`,
		tenantID, rowNos(n), dates, farms, srcSales,
		buyers, places, animalTypes, breeds, callStatuses)
	if err != nil {
		return fmt.Errorf("buyer leads upsert: %w", err)
	}
	return nil
}

func importFPOLeads(ctx context.Context, tx pgx.Tx, tenantID string, leads []fixtureFPOLead) error {
	n := len(leads)
	names := make([]string, 0, n)
	crops := make([]*string, 0, n)
	districts := make([]*string, 0, n)
	taluks := make([]*string, 0, n)
	states := make([]*string, 0, n)
	callStatuses := make([]*string, 0, n)
	for _, l := range leads {
		names = append(names, strings.TrimSpace(l.FPOName))
		crops = append(crops, nullTextPtr(l.Crops))
		districts = append(districts, nullTextPtr(l.District))
		taluks = append(taluks, nullTextPtr(l.Taluk))
		states = append(states, nullTextPtr(l.State))
		callStatuses = append(callStatuses, nullTextPtr(l.CallStatus))
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO public.sales_fpo_leads (
			tenant_id, source_row_no, fpo_name, crops, district, taluk, state, call_status
		)
		SELECT $1, row_no, fpo_name, crops, district, taluk, state, call_status
		FROM unnest(
			$2::int[], $3::text[], $4::text[], $5::text[], $6::text[], $7::text[], $8::text[]
		) AS s(row_no, fpo_name, crops, district, taluk, state, call_status)
		ON CONFLICT (tenant_id, source_row_no) WHERE source_row_no IS NOT NULL
		DO UPDATE SET
			fpo_name = EXCLUDED.fpo_name,
			crops = EXCLUDED.crops,
			district = EXCLUDED.district,
			taluk = EXCLUDED.taluk,
			state = EXCLUDED.state,
			call_status = EXCLUDED.call_status,
			updated_at = now()`,
		tenantID, rowNos(n), names, crops, districts, taluks, states, callStatuses)
	if err != nil {
		return fmt.Errorf("fpo leads upsert: %w", err)
	}
	return nil
}

func importSoldTags(ctx context.Context, tx pgx.Tx, tenantID string, tags []fixtureSoldTag) error {
	n := len(tags)
	labels := make([]string, 0, n)
	tagNumbers := make([]*string, 0, n)
	weights := make([]*float64, 0, n)
	farms := make([]*string, 0, n)
	srcSales := make([]*int32, 0, n)
	for _, t := range tags {
		labels = append(labels, strings.TrimSpace(t.AnimalLabel))
		tagNumbers = append(tagNumbers, nullTextPtr(t.TagNumber))
		weights = append(weights, t.WeightKg)
		farms = append(farms, nullTextPtr(t.Farm))
		srcSales = append(srcSales, intPtr32(t.SourceSalesID))
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO public.sales_sold_animal_tags (
			tenant_id, source_row_no, animal_label, tag_number, weight_kg, farm, source_sales_id
		)
		SELECT $1, row_no, animal_label, tag_number, weight_kg, farm, source_sales_id
		FROM unnest(
			$2::int[], $3::text[], $4::text[], $5::numeric[], $6::text[], $7::int[]
		) AS s(row_no, animal_label, tag_number, weight_kg, farm, source_sales_id)
		ON CONFLICT (tenant_id, source_row_no) WHERE source_row_no IS NOT NULL
		DO UPDATE SET
			animal_label = EXCLUDED.animal_label,
			tag_number = EXCLUDED.tag_number,
			weight_kg = EXCLUDED.weight_kg,
			farm = EXCLUDED.farm,
			source_sales_id = EXCLUDED.source_sales_id,
			updated_at = now()`,
		tenantID, rowNos(n), labels, tagNumbers, weights, farms, srcSales)
	if err != nil {
		return fmt.Errorf("sold tags upsert: %w", err)
	}
	return nil
}

func importWeightAudit(ctx context.Context, tx pgx.Tx, tenantID string, rows []fixtureAuditRow) error {
	n := len(rows)
	videos := make([]float64, 0, n)
	books := make([]float64, 0, n)
	farmBorn := make([]bool, 0, n)
	tagNumbers := make([]*string, 0, n)
	for _, a := range rows {
		videos = append(videos, a.VideoWeightKg)
		books = append(books, a.BookWeightKg)
		farmBorn = append(farmBorn, a.FarmBorn)
		tagNumbers = append(tagNumbers, nullTextPtr(a.TagNumber))
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO public.sales_weight_audit (
			tenant_id, source_row_no, video_weight_kg, book_weight_kg, farm_born, tag_number
		)
		SELECT $1, row_no, video_weight_kg, book_weight_kg, farm_born, tag_number
		FROM unnest(
			$2::int[], $3::numeric[], $4::numeric[], $5::boolean[], $6::text[]
		) AS s(row_no, video_weight_kg, book_weight_kg, farm_born, tag_number)
		ON CONFLICT (tenant_id, source_row_no) WHERE source_row_no IS NOT NULL
		DO UPDATE SET
			video_weight_kg = EXCLUDED.video_weight_kg,
			book_weight_kg = EXCLUDED.book_weight_kg,
			farm_born = EXCLUDED.farm_born,
			tag_number = EXCLUDED.tag_number,
			updated_at = now()`,
		tenantID, rowNos(n), videos, books, farmBorn, tagNumbers)
	if err != nil {
		return fmt.Errorf("weight audit upsert: %w", err)
	}
	return nil
}

func importBenchmarks(ctx context.Context, tx pgx.Tx, tenantID string, rows []fixtureBenchmark) error {
	n := len(rows)
	markets := make([]*string, 0, n)
	categories := make([]*string, 0, n)
	breeds := make([]string, 0, n)
	sources := make([]*string, 0, n)
	exFarm := make([]*string, 0, n)
	transport := make([]*string, 0, n)
	landing := make([]*float64, 0, n)
	marketPrice := make([]*float64, 0, n)
	for _, b := range rows {
		market := b.Market
		markets = append(markets, nullTextPtr(&market))
		categories = append(categories, nullTextPtr(b.Category))
		breeds = append(breeds, strings.TrimSpace(b.Breed))
		sources = append(sources, nullTextPtr(b.Source))
		exFarm = append(exFarm, nullTextPtr(b.ExFarmRate))
		transport = append(transport, nullTextPtr(b.TransportRate))
		landing = append(landing, b.LandingCostPerKg)
		// The comparable per-kg price lives inside the market prose; parsed here, once, so the
		// read path never parses strings.
		marketPrice = append(marketPrice, firstNumber(b.Market))
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO public.sales_market_benchmarks (
			tenant_id, source_row_no, market, category, breed, source,
			ex_farm_rate, transport_rate, landing_cost_per_kg, market_price_per_kg
		)
		SELECT $1, row_no, market, category, breed, source,
		       ex_farm_rate, transport_rate, landing_cost_per_kg, market_price_per_kg
		FROM unnest(
			$2::int[], $3::text[], $4::text[], $5::text[], $6::text[],
			$7::text[], $8::text[], $9::numeric[], $10::numeric[]
		) AS s(row_no, market, category, breed, source,
		       ex_farm_rate, transport_rate, landing_cost_per_kg, market_price_per_kg)
		ON CONFLICT (tenant_id, source_row_no) WHERE source_row_no IS NOT NULL
		DO UPDATE SET
			market = EXCLUDED.market,
			category = EXCLUDED.category,
			breed = EXCLUDED.breed,
			source = EXCLUDED.source,
			ex_farm_rate = EXCLUDED.ex_farm_rate,
			transport_rate = EXCLUDED.transport_rate,
			landing_cost_per_kg = EXCLUDED.landing_cost_per_kg,
			market_price_per_kg = EXCLUDED.market_price_per_kg,
			updated_at = now()`,
		tenantID, rowNos(n), markets, categories, breeds, sources,
		exFarm, transport, landing, marketPrice)
	if err != nil {
		return fmt.Errorf("benchmarks upsert: %w", err)
	}
	return nil
}
