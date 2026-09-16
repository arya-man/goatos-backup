// Package app composes the Alerts page: the effective rule config, and the day's alerts
// for one park derived from other modules' frozen rows through the enabled detectors.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/vgoats/goatos/backend/internal/alerts/domain"
	"github.com/vgoats/goatos/backend/internal/alerts/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Service is the one entry point the transport calls.
type Service struct {
	config     ports.ConfigStore
	sheets     ports.FeedSheetReader
	movements  ports.MovementReader
	stock      ports.LowStockReader
	parks      ports.ParkNameReader
	eventRules ports.EventRuleStore
	events     ports.EventReader
	log        *slog.Logger
	now        func() time.Time
}

// NewService wires the readers. Any reader may be nil; its rule then reports "could not
// load" rather than blanking the page.
func NewService(config ports.ConfigStore, sheets ports.FeedSheetReader, movements ports.MovementReader, stock ports.LowStockReader, parks ports.ParkNameReader, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{config: config, sheets: sheets, movements: movements, stock: stock, parks: parks, log: log, now: time.Now}
}

// WithEvents wires the user-defined event alerts: the rule store and the per-kind readers.
func (s *Service) WithEvents(rules ports.EventRuleStore, reader ports.EventReader) *Service {
	s.eventRules, s.events = rules, reader
	return s
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

// EventRules lists the tenant's composed alerts. An unwired store yields none.
func (s *Service) EventRules(ctx context.Context, tenantID string) ([]domain.EventRule, error) {
	if s.eventRules == nil {
		return []domain.EventRule{}, nil
	}
	rules, err := s.eventRules.ListEventRules(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("alerts: list event rules: %w", err)
	}
	return rules, nil
}

// SetEventRule validates and creates or updates one composed alert.
func (s *Service) SetEventRule(ctx context.Context, in domain.SetEventRule) (domain.EventRule, error) {
	if err := in.Validate(); err != nil {
		return domain.EventRule{}, err
	}
	if s.eventRules == nil {
		return domain.EventRule{}, fmt.Errorf("alerts: event rules not wired")
	}
	return s.eventRules.UpsertEventRule(ctx, in)
}

// DeleteEventRule removes one composed alert.
func (s *Service) DeleteEventRule(ctx context.Context, tenantID, ruleID string) error {
	if s.eventRules == nil {
		return fmt.Errorf("alerts: event rules not wired")
	}
	return s.eventRules.DeleteEventRule(ctx, tenantID, ruleID)
}

// Page is one park's alerts for one business day.
type Page struct {
	Rows []domain.Alert
	// Degraded names the rules whose read failed on this request; the rest still serve, so
	// the client shows those as "couldn't load" rather than an empty page that reads as
	// "all clear".
	Degraded []domain.RuleKey
	// RulesRun contains only completed checks; degraded and skipped rules are separate. A composed event
	// rule runs as "event:<id>".
	RulesRun []domain.RuleKey
	// Skipped names enabled rules that did not run for this date and why, worded for the page.
	Skipped []SkippedRule
}

// SkippedRule is one enabled rule the read left out, with the farm-worded reason.
type SkippedRule struct {
	Key    domain.RuleKey `json:"key"`
	Label  string         `json:"label"`
	Reason string         `json:"reason"`
}

// List runs every enabled detector for one park and day. The feed-stock rule is farm-grain
// and read once per tenant; it is filtered to the park so "all parks" composed by the client
// from per-park reads never counts a feed twice.
//
// The rules are independent reads over different tables, so they run CONCURRENTLY under a
// small bounded pool (listConcurrency): a page of six rules is one round trip deep, not six.
// Results are gathered back into rule order so two reads of the same day list the same way.
func (s *Service) List(ctx context.Context, tenantID, parkID, businessDate string) (Page, error) {
	if businessDate == "" {
		businessDate = biztime.BusinessDate(s.now())
	}
	// The three inputs the rules need -- the switches, the composed rules, the park's name --
	// are independent reads and run together; over a tunnel each is a round trip.
	var (
		cfgs       []domain.RuleConfig
		eventRules []domain.EventRule
		parkName   string
		cfgErr     error
		rulesErr   error
		setup      sync.WaitGroup
	)
	setup.Add(3)
	go func() { defer setup.Done(); cfgs, cfgErr = s.Config(ctx, tenantID) }()
	go func() { defer setup.Done(); eventRules, rulesErr = s.EventRules(ctx, tenantID) }()
	go func() {
		defer setup.Done()
		// One park name for every row on the page, whatever table the rule read it from: the
		// feed sheet freezes the park's short code while the stock ledger carries the farm
		// label, and a page whose rows disagree about what the park is called reads as two.
		if s.parks != nil {
			if names, err := s.parks.ParkNames(ctx, tenantID, []string{parkID}); err == nil {
				parkName = names[parkID]
			}
		}
	}()
	setup.Wait()
	if cfgErr != nil {
		return Page{}, cfgErr
	}
	if rulesErr != nil {
		return Page{}, rulesErr
	}
	page := Page{Rows: []domain.Alert{}}
	today := biztime.BusinessDate(s.now())

	type job struct {
		key domain.RuleKey
		run func(context.Context) ([]domain.Alert, error)
	}
	jobs := []job{}
	for _, cfg := range cfgs {
		if !cfg.Enabled {
			continue
		}
		if cfg.TodayOnly && businessDate != today {
			// A live figure cannot be read as-of a past day; skipping it keeps yesterday's
			// page from changing when a purchase lands today. Not listed in RulesRun: the page
			// says "rules checked", and this one was not -- it is named under Skipped instead.
			page.Skipped = append(page.Skipped, SkippedRule{Key: cfg.Key, Label: cfg.Label, Reason: "Checked for today only; this date is not re-read."})
			continue
		}
		cfg := cfg
		jobs = append(jobs, job{key: cfg.Key, run: func(ctx context.Context) ([]domain.Alert, error) {
			return s.run(ctx, cfg, tenantID, parkID, businessDate)
		}})
	}
	type sharedEvents struct {
		once sync.Once
		page domain.EventPage
		err  error
	}
	eventReads := map[domain.EventKind]*sharedEvents{}
	for _, rule := range eventRules {
		if !rule.Enabled {
			continue
		}
		rule := rule
		read := eventReads[rule.Kind]
		if read == nil {
			read = &sharedEvents{}
			eventReads[rule.Kind] = read
		}
		key := domain.RuleKey("event:" + rule.ID)
		jobs = append(jobs, job{key: key, run: func(ctx context.Context) ([]domain.Alert, error) {
			if s.events == nil {
				return nil, fmt.Errorf("alerts: event reader not wired")
			}
			read.once.Do(func() { read.page, read.err = s.events.Events(ctx, tenantID, parkID, rule.Kind, businessDate) })
			if read.err != nil {
				return nil, read.err
			}
			return domain.DetectEvents(businessDate, parkName, rule, read.page), nil
		}})
	}

	type outcome struct {
		rows []domain.Alert
		err  error
	}
	results := make([]outcome, len(jobs))
	sem := make(chan struct{}, listConcurrency)
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, j job) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				if r := recover(); r != nil {
					results[i] = outcome{err: fmt.Errorf("alerts: rule %s panicked: %v", j.key, r)}
					s.log.ErrorContext(ctx, "alerts_rule_panic", "rule", j.key, "panic", fmt.Sprint(r))
				}
			}()
			rows, err := j.run(ctx)
			results[i] = outcome{rows: rows, err: err}
		}(i, j)
	}
	wg.Wait()
	for i, j := range jobs {
		if errors.Is(results[i].err, errFeedSheetsMissing) {
			rule, _ := domain.RuleByKey(j.key)
			page.Skipped = append(page.Skipped, SkippedRule{Key: j.key, Label: rule.Label, Reason: "Not checked: both this date’s and the previous day’s issued feed sheets are required."})
			continue
		}
		if results[i].err != nil {
			s.log.WarnContext(ctx, "alerts_rule_read_failed", "rule", j.key, "park_id", parkID, "business_date", businessDate, "error", results[i].err)
			page.Degraded = append(page.Degraded, j.key)
			continue
		}
		page.RulesRun = append(page.RulesRun, j.key)
		rows := results[i].rows
		for k := range rows {
			if parkName != "" {
				rows[k].ParkLabel = parkName
			}
		}
		page.Rows = append(page.Rows, rows...)
	}
	domain.SortAlerts(page.Rows)
	return page, nil
}

var errFeedSheetsMissing = errors.New("feed comparison needs both issued sheets")

// listConcurrency bounds the rule reads in flight for one page read: enough to collapse the
// round trips, small enough that a burst of page loads cannot swamp the shared pool.
const listConcurrency = 4

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
	day, err := time.ParseInLocation("2006-01-02", businessDate, biztime.DefaultLocation())
	if err != nil {
		return nil, fmt.Errorf("alerts: invalid business date %q: %w", businessDate, err)
	}
	// Business DAYS, never hours: the comparison is yesterday's sheet against today's. The two
	// sheet reads are independent and run together.
	yesterdayDate := day.AddDate(0, 0, -1).Format("2006-01-02")
	var (
		today, yesterday             []domain.PenFeedDay
		todayIssued, yesterdayIssued string
		todayErr, yesterdayErr       error
		wg                           sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		today, todayIssued, todayErr = s.sheets.PenFeedDay(ctx, tenantID, parkID, businessDate)
	}()
	go func() {
		defer wg.Done()
		yesterday, yesterdayIssued, yesterdayErr = s.sheets.PenFeedDay(ctx, tenantID, parkID, yesterdayDate)
	}()
	wg.Wait()
	if todayErr != nil {
		return nil, todayErr
	}
	if yesterdayErr != nil {
		return nil, yesterdayErr
	}
	if yesterday == nil || today == nil {
		// Missing evidence is a skipped comparison, never a successful check.
		return nil, errFeedSheetsMissing
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
