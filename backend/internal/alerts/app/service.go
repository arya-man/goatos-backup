// Package app composes the Alerts page: the effective rule config, and the day's alerts
// for one park derived from other modules' frozen rows through the enabled detectors.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/vgoats/goatos/backend/internal/alerts/domain"
	"github.com/vgoats/goatos/backend/internal/alerts/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Service is the one entry point the transport calls.
type Service struct {
	config    ports.ConfigStore
	sheets    ports.FeedSheetReader
	movements ports.MovementReader
	stock     ports.LowStockReader
	parks     ports.ParkNameReader
	log       *slog.Logger
	now       func() time.Time
}

// NewService wires the readers. Any reader may be nil; its rule then reports "could not
// load" rather than blanking the page.
func NewService(config ports.ConfigStore, sheets ports.FeedSheetReader, movements ports.MovementReader, stock ports.LowStockReader, parks ports.ParkNameReader, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{config: config, sheets: sheets, movements: movements, stock: stock, parks: parks, log: log, now: time.Now}
}

// WithClock pins the clock for tests.
func (s *Service) WithClock(now func() time.Time) *Service {
	if now != nil {
		s.now = now
	}
	return s
}

// Config returns every catalog rule at its effective setting.
func (s *Service) Config(ctx context.Context, tenantID string) ([]domain.RuleConfig, error) {
	stored, err := s.config.ListRuleConfig(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("alerts: list config: %w", err)
	}
	return domain.EffectiveConfig(stored), nil
}

// SetConfig validates and writes one rule's setting, then returns the effective row.
func (s *Service) SetConfig(ctx context.Context, in domain.SetRuleConfig) (domain.RuleConfig, error) {
	if err := in.Validate(); err != nil {
		return domain.RuleConfig{}, err
	}
	if err := s.config.UpsertRuleConfig(ctx, in); err != nil {
		return domain.RuleConfig{}, err
	}
	cfgs, err := s.Config(ctx, in.TenantID)
	if err != nil {
		return domain.RuleConfig{}, err
	}
	for _, c := range cfgs {
		if c.Key == in.Key {
			return c, nil
		}
	}
	return domain.RuleConfig{}, domain.ErrUnknownRule
}

// Page is one park's alerts for one business day.
type Page struct {
	Rows []domain.Alert
	// Degraded names the rules whose read failed on this request; the rest still serve, so
	// the client shows those as "couldn't load" rather than an empty page that reads as
	// "all clear".
	Degraded []domain.RuleKey
	// RulesRun is every enabled rule, so the page can say what it checked.
	RulesRun []domain.RuleKey
}

// List runs every enabled detector for one park and day. The feed-stock rule is farm-grain
// and read once per tenant; it is filtered to the park so "all parks" composed by the client
// from per-park reads never counts a feed twice.
func (s *Service) List(ctx context.Context, tenantID, parkID, businessDate string) (Page, error) {
	if businessDate == "" {
		businessDate = biztime.BusinessDate(s.now())
	}
	cfgs, err := s.Config(ctx, tenantID)
	if err != nil {
		return Page{}, err
	}
	page := Page{Rows: []domain.Alert{}}
	// One park name for every row on the page, whatever table the rule read it from: the feed
	// sheet freezes the park's short code while the stock ledger carries the farm label, and a
	// page whose rows disagree about what the park is called reads as two parks.
	parkName := ""
	if s.parks != nil {
		if names, err := s.parks.ParkNames(ctx, tenantID, []string{parkID}); err == nil {
			parkName = names[parkID]
		}
	}
	for _, cfg := range cfgs {
		if !cfg.Enabled {
			continue
		}
		page.RulesRun = append(page.RulesRun, cfg.Key)
		rows, err := s.run(ctx, cfg, tenantID, parkID, businessDate)
		if err != nil {
			s.log.WarnContext(ctx, "alerts_rule_read_failed", "rule", cfg.Key, "park_id", parkID, "business_date", businessDate, "error", err)
			page.Degraded = append(page.Degraded, cfg.Key)
			continue
		}
		for i := range rows {
			if parkName != "" {
				rows[i].ParkLabel = parkName
			}
		}
		page.Rows = append(page.Rows, rows...)
	}
	domain.SortAlerts(page.Rows)
	return page, nil
}

func (s *Service) run(ctx context.Context, cfg domain.RuleConfig, tenantID, parkID, businessDate string) ([]domain.Alert, error) {
	switch cfg.Key {
	case domain.RulePenFeedQuantityChange:
		return s.penFeedChanges(ctx, tenantID, parkID, businessDate, cfg.Threshold)
	case domain.RuleFeedLowStock:
		return s.lowStock(ctx, tenantID, parkID, businessDate, cfg.Threshold)
	default:
		// A catalog rule with no detector is a programming error, not a silent pass.
		return nil, fmt.Errorf("alerts: rule %q has no detector", cfg.Key)
	}
}

func (s *Service) penFeedChanges(ctx context.Context, tenantID, parkID, businessDate string, minChange int) ([]domain.Alert, error) {
	if s.sheets == nil || s.movements == nil {
		return nil, fmt.Errorf("alerts: feed sheet reader not wired")
	}
	today, todayIssued, err := s.sheets.PenFeedDay(ctx, tenantID, parkID, businessDate)
	if err != nil {
		return nil, err
	}
	day, err := time.ParseInLocation("2006-01-02", businessDate, biztime.DefaultLocation())
	if err != nil {
		return nil, fmt.Errorf("alerts: invalid business date %q: %w", businessDate, err)
	}
	// Business DAYS, never hours: the comparison is yesterday's sheet against today's.
	yesterdayDate := day.AddDate(0, 0, -1).Format("2006-01-02")
	yesterday, yesterdayIssued, err := s.sheets.PenFeedDay(ctx, tenantID, parkID, yesterdayDate)
	if err != nil {
		return nil, err
	}
	if yesterday == nil || today == nil {
		// One of the two sheets was never issued: nothing to compare.
		return nil, nil
	}
	moves, err := s.movements.PenMovements(ctx, tenantID, parkID, yesterdayIssued, todayIssued)
	if err != nil {
		return nil, err
	}
	return domain.DetectPenFeedChanges(domain.PenFeedChangeInput{
		BusinessDate:  businessDate,
		Today:         today,
		Yesterday:     yesterday,
		Movements:     moves,
		MinHeadChange: int64(minChange),
	}), nil
}

func (s *Service) lowStock(ctx context.Context, tenantID, parkID, businessDate string, withinDays int) ([]domain.Alert, error) {
	if s.stock == nil {
		return nil, fmt.Errorf("alerts: stock reader not wired")
	}
	feeds, err := s.stock.LowStock(ctx, tenantID, withinDays)
	if err != nil {
		return nil, err
	}
	inPark := make([]domain.LowStockFeed, 0, len(feeds))
	ids := []string{}
	for _, f := range feeds {
		if f.ParkID != parkID {
			continue
		}
		inPark = append(inPark, f)
		ids = append(ids, f.ParkID)
	}
	names := map[string]string{}
	if s.parks != nil && len(ids) > 0 {
		if resolved, err := s.parks.ParkNames(ctx, tenantID, ids); err == nil {
			names = resolved
		}
	}
	return domain.DetectLowStock(businessDate, names, inPark), nil
}
