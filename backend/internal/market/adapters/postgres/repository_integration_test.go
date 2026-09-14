package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/market/domain"
	"github.com/vgoats/goatos/backend/internal/market/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const testTenant = "00000000-0000-4000-8000-000000000001"

// TestMarketSurveyPostgresPaths exercises the survey against a real Postgres, because every
// defect it guards lives in the SQL: the snapshot columns, the set-based upsert whose row count is
// the unknown-question check, and the idempotency reservation inside the same transaction.
func TestMarketSurveyPostgresPaths(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	// The migration seeds six questions for the baseline tenant.
	cfg, err := repo.GetConfig(ctx, testTenant)
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if got := len(cfg.ActiveQuestions()); got != 6 {
		t.Fatalf("seeded questions = %d, want 6 (%+v)", got, cfg.Questions)
	}
	if len(cfg.Cities) != 0 {
		t.Fatalf("cities seeded = %+v, want none", cfg.Cities)
	}
	goatLive := cfg.Questions[0]
	if goatLive.Label != "Goat live price" || goatLive.UnitLabel != "₹/kg" {
		t.Fatalf("first question = %+v", goatLive)
	}

	chennai, err := repo.CreateCity(ctx, testTenant, "", "city-1", domain.CityWrite{Name: "Chennai", Status: domain.StatusActive})
	if err != nil {
		t.Fatalf("create city: %v", err)
	}
	salem, err := repo.CreateCity(ctx, testTenant, "", "city-2", domain.CityWrite{Name: "Salem", Status: domain.StatusActive})
	if err != nil {
		t.Fatalf("create second city: %v", err)
	}
	if salem.SortOrder <= chennai.SortOrder {
		t.Fatalf("second city sorts %d, want after %d", salem.SortOrder, chennai.SortOrder)
	}

	t.Run("a city create replays on its key and refuses a different payload", func(t *testing.T) {
		again, err := repo.CreateCity(ctx, testTenant, "", "city-1", domain.CityWrite{Name: "Chennai", Status: domain.StatusActive})
		if err != nil || again.ID != chennai.ID {
			t.Fatalf("replay = %+v, %v; want the original city", again, err)
		}
		if _, err := repo.CreateCity(ctx, testTenant, "", "city-1", domain.CityWrite{Name: "Madurai", Status: domain.StatusActive}); !errors.Is(err, ports.ErrIdempotencyConflict) {
			t.Fatalf("different payload on a used key = %v, want ErrIdempotencyConflict", err)
		}
		if _, err := repo.CreateCity(ctx, testTenant, "", "city-dup", domain.CityWrite{Name: " chennai ", Status: domain.StatusActive}); !errors.Is(err, ports.ErrDuplicateName) {
			t.Fatalf("duplicate active name = %v, want ErrDuplicateName", err)
		}
	})

	day := "2026-09-14"
	t.Run("a day entry snapshots the question words and upserts on a second sitting", func(t *testing.T) {
		rows, err := repo.RecordDayEntry(ctx, ports.RecordDayEntryParams{
			TenantID: testTenant, ActorID: "", IdempotencyKey: "entry-1",
			Write: domain.DayEntryWrite{CityID: chennai.ID, BusinessDate: day, Answers: []domain.PriceAnswer{
				{QuestionID: goatLive.ID, Price: 620},
			}},
		})
		if err != nil {
			t.Fatalf("record: %v", err)
		}
		if len(rows) != 1 || rows[0].Price != 620 || rows[0].CityName != "Chennai" ||
			rows[0].QuestionLabel != "Goat live price" || rows[0].UnitLabel != "₹/kg" || rows[0].BusinessDate != day {
			t.Fatalf("recorded rows = %+v", rows)
		}
		// Second sitting: correct the first figure and add a second question. Both land, the
		// first row is updated in place (one row per city/question/day).
		sheepLive := cfg.Questions[1]
		rows, err = repo.RecordDayEntry(ctx, ports.RecordDayEntryParams{
			TenantID: testTenant, IdempotencyKey: "entry-2",
			Write: domain.DayEntryWrite{CityID: chennai.ID, BusinessDate: day, Answers: []domain.PriceAnswer{
				{QuestionID: goatLive.ID, Price: 640}, {QuestionID: sheepLive.ID, Price: 580},
			}},
		})
		if err != nil {
			t.Fatalf("second sitting: %v", err)
		}
		if len(rows) != 2 {
			t.Fatalf("rows after second sitting = %d, want 2 (%+v)", len(rows), rows)
		}
		byQ := map[string]float64{}
		for _, r := range rows {
			byQ[r.QuestionID] = r.Price
		}
		if byQ[goatLive.ID] != 640 || byQ[sheepLive.ID] != 580 {
			t.Fatalf("prices = %v", byQ)
		}
		// Exact replay writes nothing and returns the city's day.
		replay, err := repo.RecordDayEntry(ctx, ports.RecordDayEntryParams{
			TenantID: testTenant, IdempotencyKey: "entry-2",
			Write: domain.DayEntryWrite{CityID: chennai.ID, BusinessDate: day, Answers: []domain.PriceAnswer{
				{QuestionID: goatLive.ID, Price: 640}, {QuestionID: sheepLive.ID, Price: 580},
			}},
		})
		if err != nil || len(replay) != 2 {
			t.Fatalf("replay = %d rows, %v", len(replay), err)
		}
	})

	t.Run("a retired question is refused and the transaction writes nothing", func(t *testing.T) {
		offals := cfg.Questions[5]
		if _, err := repo.UpdateQuestion(ctx, testTenant, "", offals.ID, domain.QuestionWrite{Label: offals.Label, UnitLabel: offals.UnitLabel, Status: domain.StatusRetired}); err != nil {
			t.Fatalf("retire question: %v", err)
		}
		_, err := repo.RecordDayEntry(ctx, ports.RecordDayEntryParams{
			TenantID: testTenant, IdempotencyKey: "entry-3",
			Write: domain.DayEntryWrite{CityID: salem.ID, BusinessDate: day, Answers: []domain.PriceAnswer{
				{QuestionID: goatLive.ID, Price: 600}, {QuestionID: offals.ID, Price: 100},
			}},
		})
		if !errors.Is(err, domain.ErrUnknownQuestion) {
			t.Fatalf("retired question in an entry = %v, want ErrUnknownQuestion", err)
		}
		rows, err := repo.ListDayEntries(ctx, testTenant, day)
		if err != nil {
			t.Fatalf("list day: %v", err)
		}
		for _, r := range rows {
			if r.CityID == salem.ID {
				t.Fatalf("Salem got a row despite the refused batch: %+v", r)
			}
		}
	})

	t.Run("editing a question's unit never rewrites recorded history", func(t *testing.T) {
		if _, err := repo.UpdateQuestion(ctx, testTenant, "", goatLive.ID, domain.QuestionWrite{Label: "Goat live price", UnitLabel: "₹/500 g", Status: domain.StatusActive}); err != nil {
			t.Fatalf("re-unit: %v", err)
		}
		rows, err := repo.ListDayEntries(ctx, testTenant, day)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		var found bool
		for _, r := range rows {
			if r.QuestionID == goatLive.ID {
				found = true
				if r.UnitLabel != "₹/kg" {
					t.Fatalf("history unit = %q after the config edit, want the ₹/kg snapshot", r.UnitLabel)
				}
			}
		}
		if !found {
			t.Fatal("goat live row missing")
		}
		// And the next recording carries the NEW unit, so analytics starts a new line.
		next := "2026-09-15"
		rows, err = repo.RecordDayEntry(ctx, ports.RecordDayEntryParams{
			TenantID: testTenant, IdempotencyKey: "entry-4",
			Write: domain.DayEntryWrite{CityID: chennai.ID, BusinessDate: next, Answers: []domain.PriceAnswer{{QuestionID: goatLive.ID, Price: 330}}},
		})
		if err != nil || len(rows) != 1 || rows[0].UnitLabel != "₹/500 g" {
			t.Fatalf("next-day rows = %+v, %v; want the ₹/500 g snapshot", rows, err)
		}
		window, err := repo.ListEntriesBetween(ctx, testTenant, day, next)
		if err != nil {
			t.Fatalf("window: %v", err)
		}
		a := domain.BuildAnalytics(day, next, window)
		var goatLines int
		for _, s := range a.Series {
			if s.QuestionID == goatLive.ID && s.CityID == chennai.ID {
				goatLines++
			}
		}
		if goatLines != 2 || a.Days != 2 {
			t.Fatalf("goat live lines = %d (want 2: one per unit), days = %d (want 2)", goatLines, a.Days)
		}
	})

	t.Run("a retired city refuses an entry and drops off the day cards", func(t *testing.T) {
		if _, err := repo.UpdateCity(ctx, testTenant, "", salem.ID, domain.CityWrite{Name: "Salem", Status: domain.StatusRetired}); err != nil {
			t.Fatalf("retire city: %v", err)
		}
		_, err := repo.RecordDayEntry(ctx, ports.RecordDayEntryParams{
			TenantID: testTenant, IdempotencyKey: "entry-5",
			Write: domain.DayEntryWrite{CityID: salem.ID, BusinessDate: day, Answers: []domain.PriceAnswer{{QuestionID: goatLive.ID, Price: 1}}},
		})
		if !errors.Is(err, ports.ErrCityRetired) {
			t.Fatalf("entry for a retired city = %v, want ErrCityRetired", err)
		}
		cfg, _ := repo.GetConfig(ctx, testTenant)
		entries, _ := repo.ListDayEntries(ctx, testTenant, day)
		cards := domain.BuildDayCards(cfg, entries)
		if len(cards) != 1 || cards[0].City.ID != chennai.ID {
			t.Fatalf("cards = %+v, want Chennai only", cards)
		}
		// Chennai answered 2 of the 5 still-active questions: pending, not done.
		if cards[0].Answered != 2 || cards[0].Total != 5 || cards[0].Status != domain.CardPending {
			t.Fatalf("Chennai card = answered %d / %d, %s", cards[0].Answered, cards[0].Total, cards[0].Status)
		}
	})

	t.Run("reporters are read from the market_reporter grant only", func(t *testing.T) {
		ids, err := repo.ReporterUserIDs(ctx, testTenant)
		if err != nil {
			t.Fatalf("reporters: %v", err)
		}
		if len(ids) != 0 {
			t.Fatalf("reporters on a bare baseline = %v, want none", ids)
		}
	})
}
