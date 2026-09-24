package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/growthdirector/domain"
	"github.com/vgoats/goatos/backend/internal/growthdirector/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

// fakeRepo records the park scope the service resolved, so a test can assert
// on the ARGUMENT the repository was called with rather than on a response the
// fake made up. A scope regression is invisible if the fake ignores its input.
// This is the module's service fake: a widened ports.Repository gets its stub
// here, or every app test breaks on the interface.
type fakeRepo struct {
	putAssumptions []domain.AssumptionsUpdate
	gotParkIDs     []string
	gotStart       time.Time
	gotEnd         time.Time
	gotSex         string
	gotOrigin      string
	gotMode        string
	gotSections    string
	parks          []domain.Park
	result         domain.GrowthDirectorWeights
	feedSource     ports.FeedWeightBandSource
	settings       domain.GrowthSettings
}

func (r *fakeRepo) ListParks(context.Context, string) ([]domain.Park, error) {
	return r.parks, nil
}

func (r *fakeRepo) GetFeedWeightBandSource(_ context.Context, _ string, parkIDs []string, start, end time.Time, sex, origin, weighingCategory string, _ []float64) (ports.FeedWeightBandSource, error) {
	r.gotParkIDs = append([]string(nil), parkIDs...)
	r.gotStart, r.gotEnd = start, end
	r.gotSex, r.gotOrigin, r.gotMode = sex, origin, weighingCategory
	return r.feedSource, nil
}

func (r *fakeRepo) GetGrowthDirectorWeights(_ context.Context, _ string, parkIDs []string, start, end time.Time, sex, origin, weighingCategory, sections string) (domain.GrowthDirectorWeights, error) {
	r.gotParkIDs = append([]string(nil), parkIDs...)
	r.gotStart, r.gotEnd = start, end
	r.gotSex, r.gotOrigin, r.gotMode = sex, origin, weighingCategory
	r.gotSections = sections
	out := r.result
	// The real repository builds the park vocabulary from the scope it was
	// handed. Mirroring that keeps this test honest: it proves the caller's
	// scope is what reaches the read, not a list the fake invented.
	for _, park := range r.parks {
		for _, id := range parkIDs {
			if park.ParkID == id {
				out.Parks = append(out.Parks, park)
			}
		}
	}
	return out, nil
}

const (
	gdTenant = "00000000-0000-0000-0000-000000000001"
	gdParkA  = "00000000-0000-4000-8000-000000000201"
	gdParkB  = "00000000-0000-4000-8000-000000000202"
)

func gdActor() domain.Actor {
	return domain.Actor{TenantID: gdTenant, Roles: []string{permissions.RoleGrowthDirector}}
}

func gdContext(grants ...permissions.ActiveGrant) context.Context {
	ctx := httpmiddleware.WithAuthGrants(context.Background(), grants)
	return httpmiddleware.WithTenantID(ctx, gdTenant)
}

// A park-scoped monitor must not be able to read another park by naming its
// id. WeighingMonitor is a CAPABILITY, not a scope, so the flat role gate
// alone lets a park-A director through — the scope resolution is the only
// thing standing between them and park B's herd.
func TestGetGrowthDirectorWeightsRejectsUnauthorizedParkID(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo)
	ctx := gdContext(permissions.ActiveGrant{
		Role: permissions.RoleGrowthDirector, ScopeType: "park", ScopeID: gdParkA,
	})

	if _, err := svc.GetGrowthDirectorWeights(ctx, gdActor(), gdParkB, "", "", "", "", "", ""); err == nil {
		t.Fatal("expected park B to be denied for a park-A scoped monitor, got nil error")
	}
	if repo.gotParkIDs != nil {
		t.Fatalf("repository must not be reached when scope is denied, got parkIDs=%v", repo.gotParkIDs)
	}
}

// Omitting park_id means "every park I may monitor" — never every park that
// exists. A park-A monitor asking for the section gets park A only.
func TestGetGrowthDirectorWeightsOmittedParkIDUsesOnlyAuthorizedParks(t *testing.T) {
	repo := &fakeRepo{parks: []domain.Park{
		{ParkID: gdParkA, Name: "Coimbatore"},
		{ParkID: gdParkB, Name: "Channapatna"},
	}}
	svc := NewService(repo)
	ctx := gdContext(permissions.ActiveGrant{
		Role: permissions.RoleGrowthDirector, ScopeType: "park", ScopeID: gdParkA,
	})

	out, err := svc.GetGrowthDirectorWeights(ctx, gdActor(), "", "", "", "", "", "", "")
	if err != nil {
		t.Fatalf("GetGrowthDirectorWeights: %v", err)
	}
	if len(repo.gotParkIDs) != 1 || repo.gotParkIDs[0] != gdParkA {
		t.Fatalf("expected repository scoped to park A only, got %v", repo.gotParkIDs)
	}
	if got := int(repo.gotEnd.Sub(repo.gotStart).Hours() / 24); got != domain.DefaultPeriodDays {
		t.Fatalf("omitted from/to default window=%d days, want %d", got, domain.DefaultPeriodDays)
	}
	// The park vocabulary is backend-owned AND scoped: offering park B here
	// would advertise a park this caller cannot read.
	if len(out.Parks) != 1 || out.Parks[0].ParkID != gdParkA {
		t.Fatalf("park vocabulary must be limited to authorized parks, got %+v", out.Parks)
	}
}

// A tenant-wide monitor omitting park_id covers every park in the tenant,
// resolved through ListParks rather than through the (empty) grant park list.
func TestGetGrowthDirectorWeightsTenantWideResolvesAllParks(t *testing.T) {
	repo := &fakeRepo{parks: []domain.Park{
		{ParkID: gdParkA, Name: "Coimbatore"},
		{ParkID: gdParkB, Name: "Channapatna"},
	}}
	svc := NewService(repo)
	ctx := gdContext(permissions.ActiveGrant{
		Role: permissions.RoleGrowthDirector, ScopeType: "tenant", ScopeID: gdTenant,
	})

	if _, err := svc.GetGrowthDirectorWeights(ctx, gdActor(), "", "", "", "", "", "", ""); err != nil {
		t.Fatalf("GetGrowthDirectorWeights: %v", err)
	}
	if len(repo.gotParkIDs) != 2 {
		t.Fatalf("tenant-wide monitor must aggregate every park in the tenant, got %v", repo.gotParkIDs)
	}
}

// The window is a BUSINESS-DAY range in Asia/Kolkata, passed down half-open. A
// caller's inclusive last day must become an exclusive midnight boundary the
// day after, or the final day's weighs are silently dropped.
func TestGetGrowthDirectorWeightsPassesHalfOpenBusinessDayWindow(t *testing.T) {
	repo := &fakeRepo{parks: []domain.Park{{ParkID: gdParkA, Name: "Coimbatore"}}}
	svc := NewService(repo)
	ctx := gdContext(permissions.ActiveGrant{
		Role: permissions.RoleGrowthDirector, ScopeType: "tenant", ScopeID: gdTenant,
	})

	if _, err := svc.GetGrowthDirectorWeights(ctx, gdActor(), gdParkA, "2026-07-01", "2026-07-28", "", "", "", ""); err != nil {
		t.Fatalf("GetGrowthDirectorWeights: %v", err)
	}
	if got := repo.gotStart.Format("2006-01-02"); got != "2026-07-01" {
		t.Fatalf("period start: want 2026-07-01, got %s", got)
	}
	// Exclusive boundary is the day AFTER the caller's inclusive last day.
	if got := repo.gotEnd.Format("2006-01-02"); got != "2026-07-29" {
		t.Fatalf("period end must be exclusive midnight after the last day: want 2026-07-29, got %s", got)
	}
}

func TestGetGrowthDirectorWeightsPassesCohortFilters(t *testing.T) {
	repo := &fakeRepo{parks: []domain.Park{{ParkID: gdParkA, Name: "Coimbatore"}}}
	svc := NewService(repo)
	ctx := gdContext(permissions.ActiveGrant{
		Role: permissions.RoleGrowthDirector, ScopeType: "tenant", ScopeID: gdTenant,
	})

	if _, err := svc.GetGrowthDirectorWeights(ctx, gdActor(), gdParkA, "2026-07-01", "2026-07-28", "male", "purchased", "per_shed_partition", "road_to_sale,fair_fight"); err != nil {
		t.Fatalf("GetGrowthDirectorWeights: %v", err)
	}
	if repo.gotSex != "male" || repo.gotOrigin != "purchased" || repo.gotMode != "per_shed_partition" {
		t.Fatalf("filters did not reach repository: sex=%q origin=%q mode=%q", repo.gotSex, repo.gotOrigin, repo.gotMode)
	}
	if repo.gotSections != "road_to_sale,fair_fight" {
		t.Fatalf("sections did not reach repository: %q", repo.gotSections)
	}
}

func TestGetGrowthDirectorWeightsNormalizesAllWeighingCategory(t *testing.T) {
	repo := &fakeRepo{parks: []domain.Park{{ParkID: gdParkA, Name: "Coimbatore"}}}
	svc := NewService(repo)
	ctx := gdContext(permissions.ActiveGrant{
		Role: permissions.RoleGrowthDirector, ScopeType: "tenant", ScopeID: gdTenant,
	})

	if _, err := svc.GetGrowthDirectorWeights(ctx, gdActor(), gdParkA, "", "", "", "", "all", ""); err != nil {
		t.Fatalf("GetGrowthDirectorWeights: %v", err)
	}
	if repo.gotMode != "" {
		t.Fatalf("weighing_category=all must normalize to no filter, got %q", repo.gotMode)
	}
}

func TestGetGrowthDirectorWeightsRejectsMalformedInput(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo)
	ctx := gdContext(permissions.ActiveGrant{
		Role: permissions.RoleGrowthDirector, ScopeType: "tenant", ScopeID: gdTenant,
	})

	for _, tc := range []struct{ name, park, from, to string }{
		{"non-uuid park", "not-a-uuid", "", ""},
		{"non-date from", "", "01/07/2026", ""},
		{"non-date to", "", "", "2026-7-1"},
		{"inverted window", "", "2026-07-28", "2026-07-01"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.GetGrowthDirectorWeights(ctx, gdActor(), tc.park, tc.from, tc.to, "", "", "", ""); err != ports.ErrInvalidArgument {
				t.Fatalf("want ErrInvalidArgument, got %v", err)
			}
		})
	}
}

func TestGetGrowthDirectorWeightsRejectsMalformedWeighingCategory(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo)
	ctx := gdContext(permissions.ActiveGrant{
		Role: permissions.RoleGrowthDirector, ScopeType: "tenant", ScopeID: gdTenant,
	})

	if _, err := svc.GetGrowthDirectorWeights(ctx, gdActor(), gdParkA, "", "", "", "", "magic", ""); err != ports.ErrInvalidArgument {
		t.Fatalf("want ErrInvalidArgument, got %v", err)
	}
	if repo.gotParkIDs != nil {
		t.Fatalf("repository must not be reached for invalid weighing_category, got %v", repo.gotParkIDs)
	}
}

func TestGetGrowthDirectorWeightsRejectsMalformedSections(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo)
	ctx := gdContext(permissions.ActiveGrant{
		Role: permissions.RoleGrowthDirector, ScopeType: "tenant", ScopeID: gdTenant,
	})

	if _, err := svc.GetGrowthDirectorWeights(ctx, gdActor(), gdParkA, "", "", "", "", "", "road_to_sale,magic"); err != ports.ErrInvalidArgument {
		t.Fatalf("want ErrInvalidArgument, got %v", err)
	}
	if repo.gotParkIDs != nil {
		t.Fatalf("repository must not be reached for invalid sections, got %v", repo.gotParkIDs)
	}
}

// A role without WeighingMonitor is refused before any scope work happens.
func TestGetGrowthDirectorWeightsRequiresMonitorCapability(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo)
	ctx := gdContext()
	actor := domain.Actor{TenantID: gdTenant, Roles: []string{permissions.RoleOperator}}

	if _, err := svc.GetGrowthDirectorWeights(ctx, actor, "", "", "", "", "", "", ""); err != ports.ErrForbidden {
		t.Fatalf("want ErrForbidden for a non-monitor role, got %v", err)
	}
}

// Park-scoped grants that carry no monitor capability anywhere: the actor
// passed the flat role gate but owns no park here. An unrestricted read would
// be the escalation this path exists to prevent.
func TestGetGrowthDirectorWeightsNoAuthorizedParksIsNotFound(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo)
	// A grant whose role does not carry WeighingMonitor: the actor claims the
	// role flatly but holds no park with the capability.
	ctx := gdContext(permissions.ActiveGrant{
		Role: permissions.RoleOperator, ScopeType: "park", ScopeID: gdParkA,
	})

	if _, err := svc.GetGrowthDirectorWeights(ctx, gdActor(), "", "", "", "", "", "", ""); err != ports.ErrNotFound {
		t.Fatalf("want ErrNotFound for a monitor with no authorized park, got %v", err)
	}
	if repo.gotParkIDs != nil {
		t.Fatalf("repository must not be reached with an empty scope, got %v", repo.gotParkIDs)
	}
}
func (f *fakeRepo) GetFCR(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time, sex, origin, weighingCategory string) (domain.FCRReport, error) {
	return domain.FCRReport{}, nil
}

func (f *fakeRepo) GetSalePrices(ctx context.Context, tenantID string, asOf time.Time) (domain.SalePrices, error) {
	return domain.SalePrices{Prices: []domain.SalePrice{}}, nil
}

func (f *fakeRepo) GetAssumptions(ctx context.Context, tenantID string, asOf time.Time) (domain.Assumptions, error) {
	return domain.Assumptions{SalePrices: []domain.SalePrice{}, Values: []domain.AssumptionValue{}}, nil
}

func (f *fakeRepo) PutAssumptions(ctx context.Context, tenantID, setBy string, asOf time.Time, update domain.AssumptionsUpdate) (domain.Assumptions, error) {
	f.putAssumptions = append(f.putAssumptions, update)
	return domain.Assumptions{SalePrices: []domain.SalePrice{}, Values: []domain.AssumptionValue{}}, nil
}

// The service owns the business bands: a figure outside them never reaches the repository, and
// the error is the transport's 400 (ports.ErrInvalidArgument) carrying the band in farm words.
func TestPutAssumptionsRejectsOutOfBandFiguresBeforeTheRepository(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo)
	actor := domain.Actor{TenantID: "t", UserID: "u", Roles: []string{"ceo_internal"}}
	bad := []domain.AssumptionsUpdate{
		{SalePrices: []domain.SalePriceUpdate{{Species: "goat", PricePerKgINR: fp(5)}}},
		{SalePrices: []domain.SalePriceUpdate{{Species: "cow", PricePerKgINR: fp(425)}}},
		{SalePrices: []domain.SalePriceUpdate{{Species: "goat"}}},                                                // a species default cannot be blank
		{SalePrices: []domain.SalePriceUpdate{{Species: "goat", ManagementStage: "K3", PricePerKgINR: fp(450)}}}, // stage without sex
		{SalePrices: []domain.SalePriceUpdate{{Species: "goat", ManagementStage: "K3", Sex: "castrated", PricePerKgINR: fp(450)}}},
		{SalePrices: []domain.SalePriceUpdate{{Species: "goat", ManagementStage: "K3", Sex: "male", PricePerKgINR: fp(450)}, {Species: "goat", ManagementStage: "k3", Sex: "Male", PricePerKgINR: fp(460)}}},
		{Values: []domain.ValueUpdate{{Key: "sale_ready_threshold_kg", Value: 3, RowVersion: 1}}},
		{Values: []domain.ValueUpdate{{Key: "load_age_alert_days", Value: 90.5, RowVersion: 1}}},
		{Values: []domain.ValueUpdate{{Key: "load_age_alert_days", Value: 90, RowVersion: 0}}},
		{Values: []domain.ValueUpdate{{Key: "no_such_key", Value: 1, RowVersion: 1}}},
		{},
	}
	for i, update := range bad {
		if _, err := svc.PutAssumptions(context.Background(), actor, update); !errors.Is(err, ports.ErrInvalidArgument) {
			t.Fatalf("case %d: want ErrInvalidArgument, got %v", i, err)
		}
	}
	if len(repo.putAssumptions) != 0 {
		t.Fatalf("repository must not see a rejected update: %+v", repo.putAssumptions)
	}
	good := domain.AssumptionsUpdate{
		SalePrices: []domain.SalePriceUpdate{{Species: "Goat", PricePerKgINR: fp(450)}, {Species: "goat", ManagementStage: "K3", Sex: "male", PricePerKgINR: fp(500)}, {Species: "goat", ManagementStage: "F2", Sex: "female"}},
		Values:     []domain.ValueUpdate{{Key: "sale_ready_threshold_kg", Value: 34.5, RowVersion: 1}, {Key: "load_age_alert_days", Value: 120, RowVersion: 2}},
	}
	if _, err := svc.PutAssumptions(context.Background(), actor, good); err != nil {
		t.Fatalf("valid update: %v", err)
	}
	if len(repo.putAssumptions) != 1 {
		t.Fatalf("valid update must reach the repository once, got %d", len(repo.putAssumptions))
	}
}

func TestPutAssumptionsRejectsInvertedSaleReadyLines(t *testing.T) {
	actor := domain.Actor{TenantID: "t", UserID: "u", Roles: []string{"ceo_internal"}}

	t.Run("both lines in one save", func(t *testing.T) {
		repo := &fakeRepo{}
		svc := NewService(repo)
		update := domain.AssumptionsUpdate{Values: []domain.ValueUpdate{
			{Key: "sale_ready_lower_kg", Value: 40, RowVersion: 1},
			{Key: "sale_ready_threshold_kg", Value: 35, RowVersion: 1},
		}}
		if _, err := svc.PutAssumptions(context.Background(), actor, update); !errors.Is(err, ports.ErrInvalidArgument) {
			t.Fatalf("want ErrInvalidArgument, got %v", err)
		}
		if len(repo.putAssumptions) != 0 {
			t.Fatalf("repository must not see inverted sale lines: %+v", repo.putAssumptions)
		}
	})

	t.Run("partial edit against current other line", func(t *testing.T) {
		repo := &fakeRepo{settings: domain.SettingsFrom([]domain.AssumptionValue{
			{Key: domain.AssumptionSaleReadyLowerKg, Value: 30},
			{Key: domain.AssumptionSaleReadyThresholdKg, Value: 35},
		})}
		svc := NewService(repo)
		update := domain.AssumptionsUpdate{Values: []domain.ValueUpdate{
			{Key: "sale_ready_lower_kg", Value: 36, RowVersion: 1},
		}}
		if _, err := svc.PutAssumptions(context.Background(), actor, update); !errors.Is(err, ports.ErrInvalidArgument) {
			t.Fatalf("want ErrInvalidArgument, got %v", err)
		}
		if len(repo.putAssumptions) != 0 {
			t.Fatalf("repository must not see inverted sale lines: %+v", repo.putAssumptions)
		}
	})
}

func (f *fakeRepo) GrowthSettings(ctx context.Context, tenantID string) (domain.GrowthSettings, error) {
	if f.settings.SaleReadyThresholdKg != 0 || f.settings.SaleReadyLowerKg != 0 || len(f.settings.BandEdgesKg) > 0 {
		return f.settings, nil
	}
	return domain.SettingsFrom(nil), nil
}

func fp(v float64) *float64 { return &v }
