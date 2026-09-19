package app

import (
	"context"
	"fmt"
	"strings"

	markethttp "github.com/vgoats/goatos/backend/internal/market/adapters/http"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// MARKET SURVEY REPORTERS (maintainer instruction 2026-09-19): the Sales SOP page lists who
// makes the morning market calls -- the people holding the `market_survey` phone module -- and
// lets the survey's owner give or take it. The toggle is a READ-MODIFY-WRITE through
// SavePersonAccess: the same validation, audit row, derived grants and row_version fence the
// People editor has, so there is exactly one way a person's access changes.

// MarketReporterLister reads the people and whether each holds the market module.
type MarketReporterLister interface {
	ListPeopleWithModule(ctx context.Context, tenantID, surface, moduleKey string) ([]MarketReporterRow, error)
}

// MarketReporterRow is one active person with a login.
type MarketReporterRow struct {
	PersonID    string
	DisplayName string
	Title       string
	ParkLabel   string
	Holds       bool
}

// MarketReporterSource is the market handler's seam, built on the access service.
type MarketReporterSource struct {
	access *AccessService
	lister MarketReporterLister
}

// NewMarketReporterSource wires the seam.
func NewMarketReporterSource(access *AccessService, lister MarketReporterLister) *MarketReporterSource {
	return &MarketReporterSource{access: access, lister: lister}
}

var _ markethttp.ReporterSource = (*MarketReporterSource)(nil)

const marketSurveyModule = "market_survey"

// ListMarketReporters implements markethttp.ReporterSource.
func (s *MarketReporterSource) ListMarketReporters(ctx context.Context, tenantID string) ([]markethttp.MarketReporter, error) {
	rows, err := s.lister.ListPeopleWithModule(ctx, tenantID, permissions.SurfaceMobile, marketSurveyModule)
	if err != nil {
		return nil, err
	}
	out := make([]markethttp.MarketReporter, 0, len(rows))
	for _, r := range rows {
		out = append(out, markethttp.MarketReporter{PersonID: r.PersonID, DisplayName: r.DisplayName, Title: r.Title, ParkLabel: r.ParkLabel, Reporter: r.Holds})
	}
	return out, nil
}

// SetMarketReporter implements markethttp.ReporterSource: re-saves the person's whole access with
// the market module's mobile level flipped, everything else exactly as stored.
func (s *MarketReporterSource) SetMarketReporter(ctx context.Context, tenantID, actorID, personID string, enabled bool) error {
	current, err := s.access.GetPersonAccess(ctx, tenantID, personID)
	if err != nil {
		return err
	}
	req := domain.SavePersonAccessRequest{
		DesignationCode: current.DesignationCode,
		ScopeMode:       current.ScopeMode,
		ParkIDs:         current.ParkIDs,
		HomeParkID:      current.HomeParkID,
		PenVisitParkIDs: current.PenVisitParkIDs,
		RowVersion:      current.RowVersion,
	}
	found := false
	for _, m := range current.Modules {
		row := domain.AccessModuleWrite{ModuleKey: m.ModuleKey, Web: m.GrantedWeb, Mobile: m.GrantedMobile, Pages: m.GrantedPagesWeb}
		if m.ModuleKey == marketSurveyModule {
			found = true
			if enabled {
				row.Mobile = []string{permissions.LevelDo}
			} else {
				row.Mobile = []string{}
			}
		}
		req.Modules = append(req.Modules, row)
	}
	if !found {
		return fmt.Errorf("%w: the market survey module is not offered to this person", ErrInvalidAccessRequest)
	}
	// A person with no parks yet cannot be saved by the wholesale write (it demands a scope);
	// the People editor is where that is set. Say so rather than fail on a generic sentence.
	if strings.TrimSpace(req.ScopeMode) != "tenant" && len(req.ParkIDs) == 0 {
		return fmt.Errorf("%w: set this person's park on People first", ErrInvalidAccessRequest)
	}
	_, err = s.access.SavePersonAccess(ctx, tenantID, actorID, personID, req)
	return err
}
