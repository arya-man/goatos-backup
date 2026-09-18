package domain

import (
	"sort"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

// FARM BORN SALES (maintainer request 2026-09-18): the animals the farm did NOT buy on a load --
// what is on the farm now, what sold in a window, which breed / sex / stage / pen the sold ones
// came from, and what they brought in. The counterpart of Load wise, which reconciles every
// PURCHASED load: the two pages partition the herd, so an animal is on exactly one of them.
//
// RECORDED CROSS-MODULE REPORTING READ, the load-wise shape (docs/decisions/sales-loadwise.md).
// Procurement owns procurement_load_goats -- the only table that says which animal came off which
// load, and therefore the only table that can say which did NOT -- and joins OUT to goats (breed,
// sex, stage, pen, terminal outcome) and to goat_sale_allocations + sales_deals (the sale date and
// the revenue a sold animal's deal brought in). Read-only over those tables and reporting grain
// only: nothing here gates a sale, an exit or a pipeline step, and the sales module's own lock
// (migration 000173: sales reads nothing from herd/procurement) is untouched because the
// dependency points the other way.
//
// WHICH ANIMALS (maintainer instruction 2026-09-19): every animal NOT on an accepted purchase
// load -- the exact complement of the load-wise membership -- whatever the register's origin
// field says. That field is under-filled (264 of the 2026 kids and 272 older adults carry none),
// so a reading keyed on it undercounted the farm's own animals; the load table is the one fact
// that is complete, and "not bought on a load" is what the farm means by its own stock.
//
// THE WINDOW BINDS THE SOLD SIDE ONLY (maintainer decision 2026-09-18). "How many do I have" is
// answered live, today, whatever the window; "how many did I sell, of what, for how much" is
// answered for the sales whose date falls in the window. The default window is the last month.

// FarmBornFilter is the whole page's filter: the sold window plus the herd dimensions. Every
// figure on the page ranges over the same filter, because a page whose cards disagree about
// which animals they counted has no true number on it (the Weights page rule).
type FarmBornFilter struct {
	// From/To bound the SOLD side by sale date, inclusive, YYYY-MM-DD business dates.
	From string
	To   string
	// ParkID narrows to one park; "" is every park.
	ParkID string
	// ShedID + Partition narrow to one pen. Partition is the human label ("Part 3", "1") and may
	// be blank for an undivided shed; it is matched through oploc.NormalizePartition on both sides.
	ShedID    string
	Partition string
	Species   string
	Breed     string
	Sex       string
	Stage     string
}

// Animal outcome buckets on this page.
const (
	FarmBornOnFarm = "on_farm"
	FarmBornSold   = "sold"
)

// FarmBornAnimalFact is one animal after the repository has resolved its pen, its outcome and --
// for a sold one -- its sale date and deal share. The input grain of BuildFarmBornSales.
type FarmBornAnimalFact struct {
	GoatID    string
	DisplayID string
	// Tag is the animal's RFID / tag (goat_identifiers), the identifier the farm knows it by.
	Tag     string
	Species string
	Breed   string
	Sex     string
	Stage   string

	ParkID         string
	ParkName       string
	ShedID         string
	ShedName       string
	PartitionLabel string

	// Bucket is FarmBornOnFarm or FarmBornSold. A sold fact is only present when its sale date
	// falls inside the filter window.
	Bucket string
	// SaleDate is the deal's sale date, or the exit date when the animal was exited as sold
	// without being tagged to a deal.
	SaleDate string
	// SaleValue is the animal's share of its deal's value (value / animals tagged), nil when the
	// sale carries no deal -- the animal counts as sold but brought in nothing this page can see.
	SaleValue *float64
	BuyerName string
	DealID    string
}

// PenDisplay is the animal's pen as the farm names it.
func (f FarmBornAnimalFact) PenDisplay() string {
	return oploc.OperationalLocation{ShedID: f.ShedID, ShedName: f.ShedName, PartitionLabel: f.PartitionLabel}.Display()
}

// PenKey is the pen's stable identity: shed uuid plus the normalized partition key.
func (f FarmBornAnimalFact) PenKey() string {
	return FarmBornPenKey(f.ShedID, f.PartitionLabel)
}

// FarmBornPenKey composes the pen filter value the page round-trips: `<shed_id>|<partition>`.
// The human partition label rides in the key so a pen option and the fact it groups agree
// without a second lookup; matching normalizes both sides.
func FarmBornPenKey(shedID, partition string) string {
	shedID = strings.TrimSpace(shedID)
	if shedID == "" {
		return ""
	}
	if !oploc.IsPartitioned(partition) {
		return shedID
	}
	return shedID + "|" + strings.TrimSpace(partition)
}

// SplitFarmBornPenKey parses a pen filter value back into its shed id and partition label.
func SplitFarmBornPenKey(key string) (shedID, partition string) {
	key = strings.TrimSpace(key)
	if i := strings.IndexByte(key, '|'); i >= 0 {
		return strings.TrimSpace(key[:i]), strings.TrimSpace(key[i+1:])
	}
	return key, ""
}

// FarmBornBucket is one row of a breakdown: a breed, a sex, a stage or a pen, with the animals
// on farm now and the animals sold in the window, and what those sales brought in.
type FarmBornBucket struct {
	Key   string
	Label string
	// Detail carries the park for a pen bucket, so CBE Castro and CPT Castro stay apart on the
	// All-parks view.
	Detail string
	// ParkID is set for a pen bucket.
	ParkID string
	OnFarm int
	Sold   int
	// SoldPriced is how many of Sold carry a deal share; Revenue sums those shares.
	SoldPriced int
	Revenue    float64
}

// FarmBornSummary is the whole-filter headline.
type FarmBornSummary struct {
	// OnFarm is the live count today, whatever the window.
	OnFarm int
	// Sold, SoldPriced and Revenue are the window's sales.
	Sold       int
	SoldPriced int
	Revenue    float64
	// AvgPrice is Revenue over SoldPriced; 0 when nothing priced.
	AvgPrice float64
	From     string
	To       string
}

// FarmBornSoldRow is one sold animal on the ledger at the foot of the page.
type FarmBornSoldRow struct {
	GoatID     string
	DisplayID  string
	Tag        string
	Species    string
	Breed      string
	Sex        string
	Stage      string
	ParkName   string
	PenDisplay string
	SaleDate   string
	SaleValue  *float64
	BuyerName  string
	DealID     string
}

// FarmBornOption is one filter choice the backend serves, built from the population itself so
// the bar never offers a breed or a pen that no animal on the page has.
type FarmBornOption struct {
	Key   string
	Label string
	// ParkID scopes a pen option to its park so the pen list follows the park select.
	ParkID string
}

// FarmBornOptions is the filter bar's vocabulary.
type FarmBornOptions struct {
	Parks   []FarmBornOption
	Pens    []FarmBornOption
	Species []FarmBornOption
	Breeds  []FarmBornOption
	Sexes   []FarmBornOption
	Stages  []FarmBornOption
}

// FarmBornSales is the whole page in one read.
type FarmBornSales struct {
	Summary FarmBornSummary
	ByBreed []FarmBornBucket
	BySex   []FarmBornBucket
	ByStage []FarmBornBucket
	ByPen   []FarmBornBucket
	// Sold is one page of the sold ledger; TotalSold is the whole-filter count.
	Sold      []FarmBornSoldRow
	TotalSold int
	Limit     int
	Offset    int
	Options   FarmBornOptions
}

// Sold ledger page bounds: a bounded desktop table.
const (
	DefaultFarmBornPageSize = 25
	MaxFarmBornPageSize     = 100
	MaxFarmBornOffset       = 10000
	// MaxFarmBornWindowDays bounds the sold window so one request cannot ask for the farm's
	// whole history of sales at animal grain.
	MaxFarmBornWindowDays = 5 * 366
)

// ClampFarmBornPageSize resolves a requested page size to a supported one.
func ClampFarmBornPageSize(requested int) int {
	switch {
	case requested <= 0:
		return DefaultFarmBornPageSize
	case requested > MaxFarmBornPageSize:
		return MaxFarmBornPageSize
	default:
		return requested
	}
}

// DefaultFarmBornWindow is the no-param window: the last month, ending today (IST business
// dates). One calendar month back rather than 30 days, because that is what "last one month"
// means at the farm.
func DefaultFarmBornWindow(today time.Time) (from, to string) {
	day := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	return day.AddDate(0, -1, 0).Format("2006-01-02"), day.Format("2006-01-02")
}

// Not-recorded labels, backend-owned like every other label on the page. They are keys the
// renderer never composes: a blank breed is a fact about the register, and it is reported as
// one rather than as an empty cell.
const (
	FarmBornUnknownBreed = "Breed not recorded"
	FarmBornUnknownStage = "Stage not recorded"
	FarmBornUnknownPen   = "Pen not recorded"
	FarmBornUnknownPark  = "Park not recorded"
)

// FarmBornSexLabel is the label for a register sex value.
func FarmBornSexLabel(sex string) string {
	switch strings.ToLower(strings.TrimSpace(sex)) {
	case "male":
		return "Male"
	case "female":
		return "Female"
	default:
		return "Sex not recorded"
	}
}

// FarmBornSpeciesLabel is the label for a register species value.
func FarmBornSpeciesLabel(species string) string {
	switch strings.ToLower(strings.TrimSpace(species)) {
	case "goat":
		return "Goat"
	case "sheep":
		return "Sheep"
	default:
		return "Species not recorded"
	}
}

// BuildFarmBornSales groups the filtered facts into the page: the headline, the four breakdowns,
// and the requested page of the sold ledger. Filtering is the repository's; this ranges over
// exactly the facts it was handed.
//
// projection-review: membership=FarmBornAnimalFact rows, one per ANIMAL (the repository's own
// predicate: tenant, not on a load, the herd dimensions; on-farm animals live
// today, sold animals with a sale date inside the window), each carrying ONE bucket;
// group_key=the bucket key (breed / sex / stage / pen key) on both the breakdown rows and the
// summary, which range over the identical fact slice, so every breakdown's OnFarm sums to
// Summary.OnFarm and every breakdown's Sold to Summary.Sold by construction;
// join_cardinality=none here (no join; the pen, the deal share and the buyer ride on each fact 1:1
// from the repository's resolution); pagination=Offset/Limit slice the SORTED sold rows only, the
// summary and breakdowns are computed before the slice; the ratio AvgPrice has numerator (the
// sum of deal shares) and denominator (the count of facts carrying a share) ranging over the same
// key set, and a zero denominator yields 0 rather than a division.
func BuildFarmBornSales(facts []FarmBornAnimalFact, filter FarmBornFilter, limit, offset int) FarmBornSales {
	summary := FarmBornSummary{From: filter.From, To: filter.To}

	breed := map[string]*FarmBornBucket{}
	sex := map[string]*FarmBornBucket{}
	stage := map[string]*FarmBornBucket{}
	pen := map[string]*FarmBornBucket{}
	sold := make([]FarmBornSoldRow, 0, 64)

	add := func(m map[string]*FarmBornBucket, key, label, detail, parkID string, f FarmBornAnimalFact) {
		b := m[key]
		if b == nil {
			b = &FarmBornBucket{Key: key, Label: label, Detail: detail, ParkID: parkID}
			m[key] = b
		} else if label < b.Label {
			// Two spellings that fold to one key ("Malai" / "malai") are one bucket; the
			// byte-smallest spelling -- the capitalised one -- names it, whichever came first.
			b.Label = label
		}
		switch f.Bucket {
		case FarmBornOnFarm:
			b.OnFarm++
		case FarmBornSold:
			b.Sold++
			if f.SaleValue != nil {
				b.SoldPriced++
				b.Revenue += *f.SaleValue
			}
		}
	}

	for _, f := range facts {
		breedLabel := strings.TrimSpace(f.Breed)
		if breedLabel == "" {
			breedLabel = FarmBornUnknownBreed
		}
		breedKey := strings.ToLower(strings.TrimSpace(f.Species)) + ":" + strings.ToLower(breedLabel)
		add(breed, breedKey, breedLabel, FarmBornSpeciesLabel(f.Species), "", f)

		sexKey := strings.ToLower(strings.TrimSpace(f.Sex))
		add(sex, sexKey, FarmBornSexLabel(f.Sex), "", "", f)

		stageLabel := strings.TrimSpace(f.Stage)
		if stageLabel == "" {
			stageLabel = FarmBornUnknownStage
		}
		add(stage, strings.ToLower(stageLabel), stageLabel, "", "", f)

		penLabel := f.PenDisplay()
		if penLabel == "" {
			penLabel = FarmBornUnknownPen
		}
		parkName := strings.TrimSpace(f.ParkName)
		if parkName == "" {
			parkName = FarmBornUnknownPark
		}
		add(pen, f.PenKey(), penLabel, parkName, f.ParkID, f)

		switch f.Bucket {
		case FarmBornOnFarm:
			summary.OnFarm++
		case FarmBornSold:
			summary.Sold++
			if f.SaleValue != nil {
				summary.SoldPriced++
				summary.Revenue += *f.SaleValue
			}
			sold = append(sold, FarmBornSoldRow{
				GoatID:     f.GoatID,
				DisplayID:  f.DisplayID,
				Tag:        f.Tag,
				Species:    f.Species,
				Breed:      f.Breed,
				Sex:        f.Sex,
				Stage:      f.Stage,
				ParkName:   f.ParkName,
				PenDisplay: f.PenDisplay(),
				SaleDate:   f.SaleDate,
				SaleValue:  f.SaleValue,
				BuyerName:  f.BuyerName,
				DealID:     f.DealID,
			})
		}
	}
	if summary.SoldPriced > 0 {
		summary.AvgPrice = summary.Revenue / float64(summary.SoldPriced)
	}

	// Newest sale first; tag then goat id make the order total.
	sort.Slice(sold, func(i, j int) bool {
		if sold[i].SaleDate != sold[j].SaleDate {
			return sold[i].SaleDate > sold[j].SaleDate
		}
		if sold[i].Tag != sold[j].Tag {
			return sold[i].Tag < sold[j].Tag
		}
		return sold[i].GoatID < sold[j].GoatID
	})

	limit = ClampFarmBornPageSize(limit)
	if offset < 0 {
		offset = 0
	}
	page := []FarmBornSoldRow{}
	if offset < len(sold) {
		end := offset + limit
		if end > len(sold) {
			end = len(sold)
		}
		page = sold[offset:end]
	}

	return FarmBornSales{
		Summary:   summary,
		ByBreed:   sortedBuckets(breed),
		BySex:     sortedBuckets(sex),
		ByStage:   sortedBuckets(stage),
		ByPen:     sortedBuckets(pen),
		Sold:      page,
		TotalSold: len(sold),
		Limit:     limit,
		Offset:    offset,
	}
}

// sortedBuckets orders a breakdown most-sold first, then most on farm, then by label so the order
// is total and a reader scanning for "what sold" reads the answer at the top.
func sortedBuckets(m map[string]*FarmBornBucket) []FarmBornBucket {
	out := make([]FarmBornBucket, 0, len(m))
	for _, b := range m {
		out = append(out, *b)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Sold != out[j].Sold {
			return out[i].Sold > out[j].Sold
		}
		if out[i].OnFarm != out[j].OnFarm {
			return out[i].OnFarm > out[j].OnFarm
		}
		if out[i].Detail != out[j].Detail {
			return out[i].Detail < out[j].Detail
		}
		if out[i].Label != out[j].Label {
			return out[i].Label < out[j].Label
		}
		return out[i].Key < out[j].Key
	})
	return out
}
