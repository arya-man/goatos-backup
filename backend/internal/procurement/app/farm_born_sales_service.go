package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/animalvocab"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

// FarmBornSalesService serves the Sales > Farm born page: the animals the register marks born
// here -- on farm today, sold in a window, and what the sold ones were.
//
// Thin like LoadwiseService: the repository resolves each animal's pen, outcome and deal share,
// the domain groups and pages, and the service only validates the filter and pins the clock.
type FarmBornSalesService struct {
	repo ports.FarmBornSalesRepository
	now  func() time.Time
}

func NewFarmBornSalesService(repo ports.FarmBornSalesRepository) *FarmBornSalesService {
	return &FarmBornSalesService{repo: repo, now: time.Now}
}

// WithClock pins the clock the default window is measured from. Tests only.
func (s *FarmBornSalesService) WithClock(now func() time.Time) *FarmBornSalesService {
	s.now = now
	return s
}

// FarmBornRequest is the page's raw query, validated here.
type FarmBornRequest struct {
	From, To string
	ParkID   string
	// Pen is the `<shed_id>|<partition>` key the page round-trips (domain.FarmBornPenKey).
	Pen     string
	Species string
	Breed   string
	Sex     string
	Stage   string
	Limit   int
	Offset  int
	// Sort / Dir order the WHOLE sold set by one ledger column before it is paged ("sort all
	// rows", 2026-09-25); blank is newest sale first.
	Sort string
	Dir  string
}

var (
	ErrFarmBornWindowInvalid  = errors.New("procurement: farm born window is not a valid date range")
	ErrFarmBornWindowTooWide  = errors.New("procurement: farm born window is wider than the served maximum")
	ErrFarmBornOffsetInvalid  = errors.New("procurement: farm born page offset is out of range")
	ErrFarmBornSexInvalid     = errors.New("procurement: farm born sex filter is not a gender code")
	ErrFarmBornSpeciesInvalid = errors.New("procurement: farm born species filter is not a species code")
)

// FarmBornSales returns the whole page for the request.
//
// The window binds the SOLD side only (maintainer decision 2026-09-18); the on-farm count is
// today's. Absent dates mean the last month ending today, in IST business dates; a half-open
// request (one date without the other) is completed from the default rather than refused, so a
// hand-edited URL still answers.
func (s *FarmBornSalesService) FarmBornSales(ctx context.Context, tenantID string, req FarmBornRequest) (domain.FarmBornSales, error) {
	if req.Offset < 0 || req.Offset > domain.MaxFarmBornOffset {
		return domain.FarmBornSales{}, ErrFarmBornOffsetInvalid
	}
	order, err := domain.ParseTableSort(req.Sort, req.Dir, domain.FarmBornOrderable, domain.FarmBornDefaultSort)
	if err != nil {
		return domain.FarmBornSales{}, err
	}
	// Species and sex are the tenant's Configuration lists (OPEN UP TO NEW SPECIES, 2026-09-25), so
	// a filter is checked for a code's SHAPE only: a configured third species filters like goat, and
	// a code no animal carries simply matches nothing.
	sex := strings.ToLower(strings.TrimSpace(req.Sex))
	if sex != "" && !animalvocab.ValidCodeShape(sex) {
		return domain.FarmBornSales{}, ErrFarmBornSexInvalid
	}
	species := strings.ToLower(strings.TrimSpace(req.Species))
	if species != "" && !animalvocab.ValidCodeShape(species) {
		return domain.FarmBornSales{}, ErrFarmBornSpeciesInvalid
	}

	today := biztime.BusinessDayStart(s.now())
	defaultFrom, defaultTo := domain.DefaultFarmBornWindow(today)
	from, to := strings.TrimSpace(req.From), strings.TrimSpace(req.To)
	if from == "" {
		from = defaultFrom
	}
	if to == "" {
		to = defaultTo
	}
	fromDay, err := time.Parse("2006-01-02", from)
	if err != nil {
		return domain.FarmBornSales{}, ErrFarmBornWindowInvalid
	}
	toDay, err := time.Parse("2006-01-02", to)
	if err != nil {
		return domain.FarmBornSales{}, ErrFarmBornWindowInvalid
	}
	if toDay.Before(fromDay) {
		return domain.FarmBornSales{}, ErrFarmBornWindowInvalid
	}
	if int(toDay.Sub(fromDay).Hours()/24) > domain.MaxFarmBornWindowDays {
		return domain.FarmBornSales{}, ErrFarmBornWindowTooWide
	}

	shedID, partition := domain.SplitFarmBornPenKey(req.Pen)
	filter := domain.FarmBornFilter{
		From:      from,
		To:        to,
		ParkID:    strings.TrimSpace(req.ParkID),
		ShedID:    shedID,
		Partition: partition,
		Species:   species,
		Breed:     strings.TrimSpace(req.Breed),
		Sex:       sex,
		Stage:     strings.TrimSpace(req.Stage),
	}
	facts, err := s.repo.FarmBornAnimals(ctx, tenantID, filter)
	if err != nil {
		return domain.FarmBornSales{}, err
	}
	out := domain.BuildFarmBornSalesSorted(facts, filter, order, req.Limit, req.Offset)
	// The bar's vocabulary is the whole population, not the filtered slice: narrowing to one
	// breed must not make the other breeds vanish from the breed select.
	options, err := s.repo.FarmBornOptions(ctx, tenantID)
	if err != nil {
		return domain.FarmBornSales{}, err
	}
	out.Options = options
	return out, nil
}

// FarmBornHTTPError maps a farm-born error onto the transport error shape.
func FarmBornHTTPError(err error) *Error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrFarmBornWindowInvalid):
		return &Error{Code: "invalid_window", Message: "Pick a from date on or before the to date.", HTTPStatus: http.StatusBadRequest}
	case errors.Is(err, ErrFarmBornWindowTooWide):
		return &Error{Code: "window_too_wide", Message: "That period is too long. Pick up to five years.", HTTPStatus: http.StatusBadRequest}
	case errors.Is(err, ErrFarmBornOffsetInvalid):
		return &Error{Code: "invalid_offset", Message: "That page is out of range.", HTTPStatus: http.StatusBadRequest}
	case errors.Is(err, domain.ErrTableSortInvalid):
		return &Error{Code: "invalid_sort", Message: "That column cannot be sorted.", HTTPStatus: http.StatusBadRequest}
	case errors.Is(err, ErrFarmBornSexInvalid):
		return &Error{Code: "invalid_sex", Message: "Pick one of the farm's genders, or all.", HTTPStatus: http.StatusBadRequest}
	case errors.Is(err, ErrFarmBornSpeciesInvalid):
		return &Error{Code: "invalid_species", Message: "Pick one of the farm's species, or all.", HTTPStatus: http.StatusBadRequest}
	default:
		return Internal("The farm born figures could not be loaded. Try again.")
	}
}
