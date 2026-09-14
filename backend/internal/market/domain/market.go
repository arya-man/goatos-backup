// Package domain holds the market survey's types and rules (maintainer decision 2026-09-14).
//
// Every morning the procurement director phones a handful of markets and asks what goat and sheep
// fetch there. WHAT is asked -- the cities, and the questions with the unit each price is quoted
// in -- is configuration a sales desk edits on Sales Config; the phone renders one card per city
// per business day and the reporter types the answers in. An entry snapshots the question it
// answered so a later config edit never rewrites history.
package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Statuses of a city or question. Retired rows are kept, never deleted: their entries stay
// readable and reactivating one resumes the series.
const (
	StatusActive  = "active"
	StatusRetired = "retired"
)

// Card statuses for the phone's day view. A city is done for the day when EVERY active question
// has a price recorded; anything less is pending, because a half-answered card is a call still to
// be finished, not a call made.
const (
	CardPending = "pending"
	CardDone    = "done"
)

// Field limits. A city name or question label is a few words; a unit is "₹/kg" or "₹/500 g".
const (
	MaxNameLen  = 80
	MaxUnitLen  = 24
	MaxPrice    = 10_000_000
	MaxCities   = 50
	MaxQuestion = 50
)

// ErrFieldValidation names the field and reason of a refused write, in farm words.
type ErrFieldValidation struct {
	Field  string
	Reason string
}

func (e ErrFieldValidation) Error() string { return e.Field + ": " + e.Reason }

var (
	// ErrNothingToRecord is a day entry naming no question at all.
	ErrNothingToRecord = errors.New("market: nothing to record")
	// ErrUnknownQuestion is a day entry naming a question the config does not carry (retired,
	// or never existed).
	ErrUnknownQuestion = errors.New("market: unknown question")
)

// City is one market phoned each morning.
type City struct {
	ID        string
	Name      string
	SortOrder int
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Question is one thing asked in every city, with the unit its price is quoted in.
type Question struct {
	ID        string
	Label     string
	UnitLabel string
	SortOrder int
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// DefaultCallTime is the local IST time the calls open when a tenant has no configured row.
const DefaultCallTime = "08:00"

// Config is the whole authored survey: every city and question, active and retired, in shown
// order, plus the ONE local time the day's calls open. Retired rows ride along so the config
// screen can reactivate them.
type Config struct {
	Cities    []City
	Questions []Question
	// CallTime is "HH:MM" in Asia/Kolkata: when the cards appear on the phone and the reminder
	// goes out (maintainer decision 2026-09-14). Blank means DefaultCallTime.
	CallTime string
}

// EffectiveCallTime is the configured call time, or the default when none is stored.
func (c Config) EffectiveCallTime() string {
	if strings.TrimSpace(c.CallTime) == "" {
		return DefaultCallTime
	}
	return c.CallTime
}

// ParseCallTime validates an "HH:MM" local time.
func ParseCallTime(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	t, err := time.Parse("15:04", raw)
	if err != nil {
		return "", ErrFieldValidation{Field: "call_time", Reason: "must be a time like 08:00"}
	}
	return t.Format("15:04"), nil
}

// CallsOpenAt is the instant the calls open on a business date, in the farm's own zone.
func CallsOpenAt(businessDate, callTime string) (time.Time, error) {
	d, err := time.Parse("2006-01-02", businessDate)
	if err != nil {
		return time.Time{}, err
	}
	t, err := time.Parse("15:04", callTime)
	if err != nil {
		return time.Time{}, err
	}
	loc := biztime.DefaultLocation()
	return time.Date(d.Year(), d.Month(), d.Day(), t.Hour(), t.Minute(), 0, 0, loc), nil
}

// CallsOpen reports whether the day's calls have opened by now: a PAST business day is always
// open (a call the reporter forgot to type in is still real), today's opens at the call time,
// and a future day never is.
func CallsOpen(businessDate, callTime string, now time.Time) bool {
	today := biztime.BusinessDate(now)
	if businessDate < today {
		return true
	}
	if businessDate > today {
		return false
	}
	openAt, err := CallsOpenAt(businessDate, callTime)
	if err != nil {
		return true
	}
	return !now.Before(openAt)
}

// ActiveCities filters the config to the cities phoned today.
func (c Config) ActiveCities() []City {
	out := make([]City, 0, len(c.Cities))
	for _, city := range c.Cities {
		if city.Status == StatusActive {
			out = append(out, city)
		}
	}
	return out
}

// ActiveQuestions filters the config to the questions asked today.
func (c Config) ActiveQuestions() []Question {
	out := make([]Question, 0, len(c.Questions))
	for _, q := range c.Questions {
		if q.Status == StatusActive {
			out = append(out, q)
		}
	}
	return out
}

// CityWrite creates or edits a city.
type CityWrite struct {
	Name   string
	Status string
}

// Normalize collapses whitespace and defaults a blank status to active.
func (w CityWrite) Normalize() CityWrite {
	out := CityWrite{Name: collapseSpace(w.Name), Status: strings.ToLower(strings.TrimSpace(w.Status))}
	if out.Status == "" {
		out.Status = StatusActive
	}
	return out
}

// Validate refuses a blank or over-long name and an unknown status.
func (w CityWrite) Validate() error {
	if w.Name == "" {
		return ErrFieldValidation{Field: "name", Reason: "required"}
	}
	if len(w.Name) > MaxNameLen {
		return ErrFieldValidation{Field: "name", Reason: "too long"}
	}
	if w.Status != StatusActive && w.Status != StatusRetired {
		return ErrFieldValidation{Field: "status", Reason: "must be active or retired"}
	}
	return nil
}

// QuestionWrite creates or edits a question.
type QuestionWrite struct {
	Label     string
	UnitLabel string
	Status    string
}

// Normalize collapses whitespace and defaults a blank status to active.
func (w QuestionWrite) Normalize() QuestionWrite {
	out := QuestionWrite{
		Label:     collapseSpace(w.Label),
		UnitLabel: collapseSpace(w.UnitLabel),
		Status:    strings.ToLower(strings.TrimSpace(w.Status)),
	}
	if out.Status == "" {
		out.Status = StatusActive
	}
	return out
}

// Validate refuses a blank label or unit and an unknown status.
func (w QuestionWrite) Validate() error {
	if w.Label == "" {
		return ErrFieldValidation{Field: "label", Reason: "required"}
	}
	if len(w.Label) > MaxNameLen {
		return ErrFieldValidation{Field: "label", Reason: "too long"}
	}
	if w.UnitLabel == "" {
		return ErrFieldValidation{Field: "unit_label", Reason: "required"}
	}
	if len(w.UnitLabel) > MaxUnitLen {
		return ErrFieldValidation{Field: "unit_label", Reason: "too long"}
	}
	if w.Status != StatusActive && w.Status != StatusRetired {
		return ErrFieldValidation{Field: "status", Reason: "must be active or retired"}
	}
	return nil
}

// PriceAnswer is one question's price as typed on the phone.
type PriceAnswer struct {
	QuestionID string
	Price      float64
}

// DayEntryWrite records a city's answers for one business day. Only the questions named are
// written; a question left blank on the phone is simply not in the list and keeps whatever the
// day already holds, so a reporter can answer a card in two sittings.
type DayEntryWrite struct {
	CityID       string
	BusinessDate string
	Answers      []PriceAnswer
}

// Validate refuses an empty entry, a malformed date, a duplicate question, or an impossible
// price. Whether each question EXISTS is the service's check against the live config.
func (w DayEntryWrite) Validate() error {
	if strings.TrimSpace(w.CityID) == "" {
		return ErrFieldValidation{Field: "city_id", Reason: "required"}
	}
	if _, err := time.Parse("2006-01-02", w.BusinessDate); err != nil {
		return ErrFieldValidation{Field: "business_date", Reason: "must be a date"}
	}
	if len(w.Answers) == 0 {
		return ErrNothingToRecord
	}
	seen := make(map[string]struct{}, len(w.Answers))
	for _, a := range w.Answers {
		id := strings.TrimSpace(a.QuestionID)
		if id == "" {
			return ErrFieldValidation{Field: "question_id", Reason: "required"}
		}
		if _, dup := seen[id]; dup {
			return ErrFieldValidation{Field: "question_id", Reason: "repeated"}
		}
		seen[id] = struct{}{}
		if a.Price < 0 {
			return ErrFieldValidation{Field: "price", Reason: "must not be negative"}
		}
		if a.Price > MaxPrice {
			return ErrFieldValidation{Field: "price", Reason: "too large"}
		}
	}
	return nil
}

// Entry is one recorded price: the snapshot words plus the figure.
type Entry struct {
	ID            string
	CityID        string
	QuestionID    string
	BusinessDate  string
	Price         float64
	CityName      string
	QuestionLabel string
	UnitLabel     string
	RecordedBy    string
	RecordedAt    time.Time
}

// CardQuestion is one question on a day card with its recorded price, if any.
type CardQuestion struct {
	Question Question
	Price    *float64
}

// DayCard is one city's card for a business day.
type DayCard struct {
	City      City
	Status    string
	Answered  int
	Total     int
	Questions []CardQuestion
}

// BuildDayCards composes the phone's day view from the live config and the day's entries: one
// card per ACTIVE city, every ACTIVE question on it, and the recorded price where one exists.
// A retired question's old entry does not appear on the card -- it is not asked any more -- but
// stays in the analytics.
func BuildDayCards(cfg Config, entries []Entry) []DayCard {
	questions := cfg.ActiveQuestions()
	priced := make(map[string]map[string]float64) // city -> question -> price
	for _, e := range entries {
		byQ, ok := priced[e.CityID]
		if !ok {
			byQ = map[string]float64{}
			priced[e.CityID] = byQ
		}
		byQ[e.QuestionID] = e.Price
	}
	cities := cfg.ActiveCities()
	cards := make([]DayCard, 0, len(cities))
	for _, city := range cities {
		card := DayCard{City: city, Total: len(questions), Questions: make([]CardQuestion, 0, len(questions))}
		for _, q := range questions {
			cq := CardQuestion{Question: q}
			if p, ok := priced[city.ID][q.ID]; ok {
				v := p
				cq.Price = &v
				card.Answered++
			}
			card.Questions = append(card.Questions, cq)
		}
		card.Status = CardPending
		if card.Total > 0 && card.Answered == card.Total {
			card.Status = CardDone
		}
		cards = append(cards, card)
	}
	return cards
}

// DefaultBusinessDate is today's IST business day, the day a blank ?date= means.
func DefaultBusinessDate(now time.Time) string {
	return biztime.BusinessDate(now)
}

// SeriesPoint is one day's price on one (city, question) line.
type SeriesPoint struct {
	BusinessDate string
	Price        float64
}

// Series is one line on the analytics chart: a question in a city, in the words the entries
// carried. The KEY includes the unit snapshot so a unit change starts a NEW line rather than
// rescaling the old one.
type Series struct {
	CityID        string
	CityName      string
	QuestionID    string
	QuestionLabel string
	UnitLabel     string
	Points        []SeriesPoint
}

// LatestCell is the most recent price for one (city, question) inside the window.
type LatestCell struct {
	CityID        string
	CityName      string
	QuestionID    string
	QuestionLabel string
	UnitLabel     string
	BusinessDate  string
	Price         float64
	// PreviousPrice is the price recorded on the day before the latest one, when the window
	// holds one; the page renders the change beside the figure.
	PreviousPrice *float64
}

// Analytics is the whole-window read the Market analytics page renders.
type Analytics struct {
	From   string
	To     string
	Latest []LatestCell
	Series []Series
	// Days is the number of distinct business days with at least one entry in the window.
	Days int
}

// BuildAnalytics groups a window's entries (oldest first) into per-(city, question, unit) series
// and a latest-per-cell table. Pure and deterministic; the repository supplies the rows.
func BuildAnalytics(from, to string, entries []Entry) Analytics {
	out := Analytics{From: from, To: to}
	type key struct{ city, question, unit string }
	index := map[key]int{}
	days := map[string]struct{}{}
	for _, e := range entries {
		days[e.BusinessDate] = struct{}{}
		k := key{e.CityID, e.QuestionID, e.UnitLabel}
		i, ok := index[k]
		if !ok {
			i = len(out.Series)
			index[k] = i
			out.Series = append(out.Series, Series{
				CityID: e.CityID, CityName: e.CityName,
				QuestionID: e.QuestionID, QuestionLabel: e.QuestionLabel, UnitLabel: e.UnitLabel,
			})
		}
		s := &out.Series[i]
		// The newest snapshot names the line: a renamed city or relabelled question shows its
		// current words, while the unit is part of the key and never changes within a line.
		s.CityName, s.QuestionLabel = e.CityName, e.QuestionLabel
		s.Points = append(s.Points, SeriesPoint{BusinessDate: e.BusinessDate, Price: e.Price})
	}
	out.Days = len(days)
	// Latest per (city, question) regardless of unit: the table shows what the market said most
	// recently, in whatever unit it was said in.
	type cellKey struct{ city, question string }
	latest := map[cellKey]int{}
	for _, e := range entries {
		k := cellKey{e.CityID, e.QuestionID}
		if i, ok := latest[k]; ok {
			prev := out.Latest[i]
			if e.BusinessDate >= prev.BusinessDate {
				p := prev.Price
				out.Latest[i] = LatestCell{
					CityID: e.CityID, CityName: e.CityName, QuestionID: e.QuestionID,
					QuestionLabel: e.QuestionLabel, UnitLabel: e.UnitLabel,
					BusinessDate: e.BusinessDate, Price: e.Price, PreviousPrice: &p,
				}
			}
			continue
		}
		latest[k] = len(out.Latest)
		out.Latest = append(out.Latest, LatestCell{
			CityID: e.CityID, CityName: e.CityName, QuestionID: e.QuestionID,
			QuestionLabel: e.QuestionLabel, UnitLabel: e.UnitLabel,
			BusinessDate: e.BusinessDate, Price: e.Price,
		})
	}
	return out
}

// FormatPrice renders a price for a notification body: whole rupees when whole, else two places.
func FormatPrice(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("₹%d", int64(v))
	}
	return fmt.Sprintf("₹%.2f", v)
}

func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
