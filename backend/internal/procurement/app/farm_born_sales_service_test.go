package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

type stubFarmBornRepo struct {
	filter        domain.FarmBornFilter
	optionsOrigin string
	facts         []domain.FarmBornAnimalFact
}

func (r *stubFarmBornRepo) FarmBornAnimals(_ context.Context, _ string, f domain.FarmBornFilter) ([]domain.FarmBornAnimalFact, error) {
	r.filter = f
	return r.facts, nil
}

func (r *stubFarmBornRepo) FarmBornOptions(_ context.Context, _ string, origin string) (domain.FarmBornOptions, error) {
	r.optionsOrigin = origin
	return domain.FarmBornOptions{Breeds: []domain.FarmBornOption{{Key: "sirohi", Label: "Sirohi"}}}, nil
}

// TestFarmBornServiceDefaultsToTheLastMonthInIST pins the no-param window: one calendar month
// back from TODAY's IST business date -- at 01:00 IST on the 18th the UTC clock still says the
// 17th, and the window must not.
func TestFarmBornServiceDefaultsToTheLastMonthInIST(t *testing.T) {
	repo := &stubFarmBornRepo{}
	// 2026-09-17T20:30Z is 2026-09-18 02:00 IST.
	svc := NewFarmBornSalesService(repo).WithClock(func() time.Time { return time.Date(2026, 9, 17, 20, 30, 0, 0, time.UTC) })
	out, err := svc.FarmBornSales(context.Background(), "t", FarmBornRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if repo.filter.From != "2026-08-18" || repo.filter.To != "2026-09-18" || repo.filter.Origin != domain.FarmBornOriginBirth {
		t.Fatalf("filter = %+v", repo.filter)
	}
	if out.Summary.From != "2026-08-18" || out.Summary.To != "2026-09-18" {
		t.Fatalf("summary window = %s..%s", out.Summary.From, out.Summary.To)
	}
	// The vocabulary is fetched for the origin reading, not the filtered slice.
	if repo.optionsOrigin != domain.FarmBornOriginBirth || len(out.Options.Breeds) != 1 {
		t.Fatalf("options origin = %q, breeds = %+v", repo.optionsOrigin, out.Options.Breeds)
	}
}

// TestFarmBornServiceForwardsThePenAndNormalisesTheRest pins the request -> filter mapping: the
// pen key splits into shed and partition, sex and species are lower-cased, and a half-open window
// is completed from the default rather than refused.
func TestFarmBornServiceForwardsThePenAndNormalisesTheRest(t *testing.T) {
	repo := &stubFarmBornRepo{}
	svc := NewFarmBornSalesService(repo).WithClock(func() time.Time { return time.Date(2026, 9, 18, 6, 0, 0, 0, time.UTC) })
	_, err := svc.FarmBornSales(context.Background(), "t", FarmBornRequest{
		From: "2026-09-01", Origin: "not_recorded", ParkID: " p1 ", Pen: "s1|Part 3", Species: "Goat", Breed: " Sirohi ", Sex: "MALE", Stage: " F2-Male ",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := domain.FarmBornFilter{From: "2026-09-01", To: "2026-09-18", Origin: "not_recorded", ParkID: "p1", ShedID: "s1", Partition: "Part 3", Species: "goat", Breed: "Sirohi", Sex: "male", Stage: "F2-Male"}
	if repo.filter != want {
		t.Fatalf("filter = %+v, want %+v", repo.filter, want)
	}
}

// TestFarmBornServiceRefusesBadFilters pins every refusal: an origin that is not a served reading
// (purchased is Load wise), an inverted or malformed window, a window past five years, an
// out-of-range page, and a sex or species outside the register's vocabulary.
func TestFarmBornServiceRefusesBadFilters(t *testing.T) {
	svc := NewFarmBornSalesService(&stubFarmBornRepo{}).WithClock(func() time.Time { return time.Date(2026, 9, 18, 6, 0, 0, 0, time.UTC) })
	for name, tc := range map[string]struct {
		req  FarmBornRequest
		want error
		code string
	}{
		"origin":   {FarmBornRequest{Origin: "purchased"}, ErrFarmBornOriginInvalid, "invalid_origin"},
		"inverted": {FarmBornRequest{From: "2026-09-18", To: "2026-09-01"}, ErrFarmBornWindowInvalid, "invalid_window"},
		"garbage":  {FarmBornRequest{From: "yesterday"}, ErrFarmBornWindowInvalid, "invalid_window"},
		"too wide": {FarmBornRequest{From: "2019-01-01", To: "2026-09-18"}, ErrFarmBornWindowTooWide, "window_too_wide"},
		"offset":   {FarmBornRequest{Offset: -1}, ErrFarmBornOffsetInvalid, "invalid_offset"},
		"sex":      {FarmBornRequest{Sex: "both"}, ErrFarmBornSexInvalid, "invalid_sex"},
		"species":  {FarmBornRequest{Species: "cow"}, ErrFarmBornSpeciesInvalid, "invalid_species"},
	} {
		_, err := svc.FarmBornSales(context.Background(), "t", tc.req)
		if !errors.Is(err, tc.want) {
			t.Fatalf("%s: err = %v, want %v", name, err, tc.want)
		}
		if httpErr := FarmBornHTTPError(err); httpErr.Code != tc.code || httpErr.HTTPStatus != 400 {
			t.Fatalf("%s: http = %+v", name, httpErr)
		}
	}
}
