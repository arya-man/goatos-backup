package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	hrmsdomain "github.com/vgoats/goatos/backend/internal/hrmssop/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// HRMS violations and enquiries (maintainer decisions 2026-09-30). The route table decides who
// may call (violations read/write for HR and the CEO; enquiry fill for park heads on the phone);
// this service validates against the HRMS SOP -- published for a new violation, PINNED for an
// enquiry -- and composes every sentence the pages show.

// HRMSRules reads the HRMS SOP (internal/hrmssop/adapters/postgres.Source).
type HRMSRules interface {
	Published(ctx context.Context, tenantID string) (hrmsdomain.Rules, error)
	Version(ctx context.Context, tenantID string, version int) (hrmsdomain.Rules, error)
}

// DisciplineCaller is who is asking. All is HR / the CEO (every park); otherwise the caller is a
// park head and sees only the parks they head.
type DisciplineCaller struct {
	All    bool
	UserID string
}

type DisciplineService struct {
	repo  ports.DisciplineRepository
	parks ports.TimetableRepository
	rules HRMSRules
	now   func() time.Time
	log   *slog.Logger
}

func NewDisciplineService(repo ports.DisciplineRepository, parks ports.TimetableRepository, rules HRMSRules, log *slog.Logger) *DisciplineService {
	if log == nil {
		log = slog.Default()
	}
	return &DisciplineService{repo: repo, parks: parks, rules: rules, now: time.Now, log: log}
}

const (
	violationPageSize   = 25
	violationTotalsSize = 200
	maxNoteRunes        = 2000
	maxReasonRunes      = 500
	maxAnswerRunes      = 2000
	maxPenalties        = 50
)

var disciplineCopy = map[string]string{
	"status.recorded":         "Recorded",
	"status.withdrawn":        "Withdrawn",
	"source.manual":           "Recorded by hand",
	"source.enquiry":          "From an enquiry",
	"enquiry.open":            "Open",
	"enquiry.overdue":         "Overdue",
	"enquiry.submitted":       "Submitted",
	"penalty.none":            "Nobody penalised",
	"penalty.one":             "1 person penalised",
	"penalty.many":            "%d people penalised",
	"error.type":              "Choose a violation from the published list.",
	"error.person":            "That person was not found, or is no longer active.",
	"error.person_park":       "You can record a violation only for someone whose home park is yours.",
	"error.fine":              "The fine must be between ₹0 and ₹1,00,00,000.",
	"error.date":              "Choose the date it happened; it cannot be in the future.",
	"error.note":              "The note is too long (2000 letters at most).",
	"error.key":               "This request is missing its retry key.",
	"error.reason":            "Say why it is withdrawn (up to 500 letters).",
	"error.withdraw_conflict": "Someone else changed this violation just now. The latest is shown.",
	"error.month":             "That month is not valid.",
	"error.park":              "That park is not one of the farm's active parks.",
	"error.enquiry":           "That enquiry was not found.",
	"error.enquiry_submitted": "This enquiry was already submitted.",
	"error.enquiry_conflict":  "Someone else changed this enquiry just now. Reload and try again.",
	"error.answer_required":   "Answer: %s",
	"error.answer_unknown":    "An answer was given to a question this enquiry does not ask.",
	"error.answer_long":       "An answer is too long (2000 letters at most).",
	"error.answer_kind":       "Answer %s with yes or no.",
	"error.penalty_person":    "Each person penalised must work at this park and be active.",
	"error.penalty_twice":     "The same person is penalised twice for the same violation.",
	"error.penalty_many":      "At most 50 people can be penalised in one enquiry.",
	"error.idempotency":       "This retry key was already used for a different violation.",
	"error.filter":            "That filter is not valid.",
	"error.no_enquiry":        "No enquiry is authored for this event.",
}

// ---------- violations ----------

// Violations reads People / HRMS > Violations for one park (or all) and one month.
func (s *DisciplineService) Violations(ctx context.Context, tenantID string, caller DisciplineCaller, parkID, month, status, cursor string, limit int, traceID string) (*domain.ViolationsPage, error) {
	parks, parkScope, err := s.scope(ctx, tenantID, caller, parkID)
	if err != nil {
		return nil, err
	}
	from, to, monthKey, err := monthWindow(month, s.now())
	if err != nil {
		return nil, err
	}
	if status != "" && status != domain.ViolationRecorded && status != domain.ViolationWithdrawn {
		return nil, &Error{Code: "invalid_filter", Message: disciplineCopy["error.filter"], HTTPStatus: 400}
	}
	if limit <= 0 || limit > 100 {
		limit = violationPageSize
	}
	f := ports.ViolationFilter{TenantID: tenantID, ParkIDs: parkScope, From: from, To: to, Status: status}
	rows, next, err := s.repo.ListViolations(ctx, f, cursor, limit)
	if errors.Is(err, ports.ErrInvalidFilter) {
		return nil, &Error{Code: "invalid_cursor", Message: disciplineCopy["error.filter"], HTTPStatus: 400}
	}
	if err != nil {
		return nil, err
	}
	// The totals are what is OWED: under "all" they count recorded violations only -- a withdrawn
	// one stays in the list (greyed, with its reason) but no longer fines anybody. Filtering to
	// "withdrawn" totals the withdrawn ones.
	totalsFilter := f
	if totalsFilter.Status == "" {
		totalsFilter.Status = domain.ViolationRecorded
	}
	sum, err := s.repo.ViolationSummary(ctx, totalsFilter)
	if err != nil {
		return nil, err
	}
	totals, err := s.repo.ViolationTotalsByPerson(ctx, totalsFilter, violationTotalsSize)
	if err != nil {
		return nil, err
	}
	people, err := s.repo.PersonOptions(ctx, tenantID, parkScope)
	if err != nil {
		return nil, err
	}
	rules, err := s.rules.Published(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := &domain.ViolationsPage{
		Parks: parks, ParkID: parkID, Months: monthOptions(s.now()), Month: monthKey, Status: status,
		Summary:  domain.ViolationSummary{Count: sum.Count, FineRupees: sum.FineRupees, FineLabel: domain.RupeesLabel(sum.FineRupees), People: sum.People},
		ByPerson: []domain.ViolationPersonTotal{}, Items: []domain.Violation{}, NextCursor: next,
		Types: typeOptions(rules.Document), People: personOptions(people), SOPVersion: rules.Version, TraceID: traceID,
	}
	for _, t := range totals {
		out.ByPerson = append(out.ByPerson, domain.ViolationPersonTotal{
			PersonID: t.PersonID, PersonName: t.PersonName, ParkLabel: t.ParkLabel, Count: t.Count,
			Designation: designationLabel(t.DesignationLabel, t.RoleHint, t.DesignationGrade, clockCopyFor("en")),
			FineRupees:  t.FineRupees, FineLabel: domain.RupeesLabel(t.FineRupees),
		})
	}
	for _, r := range rows {
		out.Items = append(out.Items, composeViolation(r))
	}
	return out, nil
}

// RecordViolation records one violation by hand, against the PUBLISHED HRMS SOP.
func (s *DisciplineService) RecordViolation(ctx context.Context, tenantID string, caller DisciplineCaller, actorID string, req domain.RecordViolationRequest, traceID string) (*domain.ViolationResponse, error) {
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		return nil, &Error{Code: "idempotency_key_required", Message: disciplineCopy["error.key"], HTTPStatus: 400}
	}
	if !caller.All {
		// A park head records only against a person whose home park they head (2026-09-30).
		home, err := s.repo.MemberHomePark(ctx, tenantID, strings.TrimSpace(req.PersonID))
		if err != nil {
			return nil, mapDisciplineError(err)
		}
		heads, err := s.repo.ParkHeadParks(ctx, tenantID, caller.UserID)
		if err != nil {
			return nil, err
		}
		ok := false
		for _, p := range heads {
			ok = ok || (home != "" && p == home)
		}
		if !ok {
			return nil, &Error{Code: "person_not_in_your_park", Message: disciplineCopy["error.person_park"], HTTPStatus: 422}
		}
	}
	rules, err := s.rules.Published(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	occurred, err := s.pastDate(req.OccurredOn)
	if err != nil {
		return nil, err
	}
	v, err := buildViolation(rules, req.PersonID, req.TypeKey, req.FineRupees, req.Note, occurred)
	if err != nil {
		return nil, err
	}
	v.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	v.Fingerprint = requestFingerprint(v.PersonID, v.TypeKey, strconv.Itoa(v.FineRupees), v.OccurredOn.Format("2006-01-02"), v.Note)
	row, err := s.repo.RecordViolation(ctx, tenantID, actorID, v)
	if err != nil {
		return nil, mapDisciplineError(err)
	}
	return &domain.ViolationResponse{Violation: composeViolation(row), TraceID: traceID}, nil
}

// WithdrawViolation withdraws a mistaken violation, keeping it on record.
func (s *DisciplineService) WithdrawViolation(ctx context.Context, tenantID, actorID, violationID string, req domain.WithdrawViolationRequest, traceID string) (*domain.ViolationResponse, error) {
	reason := strings.TrimSpace(req.Reason)
	if reason == "" || utf8.RuneCountInString(reason) > maxReasonRunes {
		return nil, &Error{Code: "reason_required", Message: disciplineCopy["error.reason"], HTTPStatus: 422}
	}
	row, err := s.repo.WithdrawViolation(ctx, tenantID, actorID, strings.TrimSpace(violationID), reason, req.RowVersion)
	if err != nil {
		return nil, mapDisciplineError(err)
	}
	return &domain.ViolationResponse{Violation: composeViolation(row), TraceID: traceID}, nil
}

// ---------- enquiries ----------

// Enquiries lists enquiries for the caller's parks.
func (s *DisciplineService) Enquiries(ctx context.Context, tenantID string, caller DisciplineCaller, parkID, status, cursor string, limit int, traceID string) (*domain.EnquiryPage, error) {
	parks, parkScope, err := s.scope(ctx, tenantID, caller, parkID)
	if err != nil {
		return nil, err
	}
	switch status {
	case "", "open", "overdue", "submitted":
	default:
		return nil, &Error{Code: "invalid_filter", Message: disciplineCopy["error.filter"], HTTPStatus: 400}
	}
	now := s.now()
	f := ports.EnquiryFilter{TenantID: tenantID, ParkIDs: parkScope, Status: status, Now: now, Cursor: cursor, Limit: limit}
	rows, next, err := s.repo.ListEnquiries(ctx, f)
	if errors.Is(err, ports.ErrInvalidFilter) {
		return nil, &Error{Code: "invalid_cursor", Message: disciplineCopy["error.filter"], HTTPStatus: 400}
	}
	if err != nil {
		return nil, err
	}
	open, overdue, submitted, err := s.repo.EnquirySummary(ctx, f)
	if err != nil {
		return nil, err
	}
	out := &domain.EnquiryPage{Parks: parks, ParkID: parkID, Status: status, Items: []domain.Enquiry{}, NextCursor: next, TraceID: traceID,
		Summary: domain.EnquirySummary{Open: open, Overdue: overdue, Submitted: submitted}}
	titles := map[int]hrmsdomain.Document{}
	for _, r := range rows {
		doc, ok := titles[r.SOPVersion]
		if !ok {
			rules, err := s.rules.Version(ctx, tenantID, r.SOPVersion)
			if err != nil {
				return nil, err
			}
			doc = rules.Document
			titles[r.SOPVersion] = doc
		}
		out.Items = append(out.Items, composeEnquiry(r, doc, now))
	}
	return out, nil
}

// Enquiry is one enquiry with everything its report needs, read on its PINNED SOP version.
func (s *DisciplineService) Enquiry(ctx context.Context, tenantID string, caller DisciplineCaller, enquiryID, traceID string) (*domain.EnquiryDetail, error) {
	row, rules, err := s.visibleEnquiry(ctx, tenantID, caller, enquiryID)
	if err != nil {
		return nil, err
	}
	return s.composeDetail(ctx, tenantID, row, rules, traceID)
}

// SubmitEnquiry records the report and the violations it names, in one transaction.
func (s *DisciplineService) SubmitEnquiry(ctx context.Context, tenantID string, caller DisciplineCaller, actorID, enquiryID string, req domain.SubmitEnquiryRequest, traceID string) (*domain.EnquiryDetail, error) {
	row, rules, err := s.visibleEnquiry(ctx, tenantID, caller, enquiryID)
	if err != nil {
		return nil, err
	}
	if row.Status != domain.EnquiryOpen {
		return nil, Conflict("enquiry_submitted", disciplineCopy["error.enquiry_submitted"])
	}
	enquiry, _ := rules.Document.Enquiry(row.TriggerKey)
	answers, err := validateAnswers(enquiry.Questions, req.Answers)
	if err != nil {
		return nil, err
	}
	if len(req.Penalties) > maxPenalties {
		return nil, &Error{Code: "too_many_penalties", Message: disciplineCopy["error.penalty_many"], HTTPStatus: 422}
	}
	people, err := s.repo.PersonOptions(ctx, tenantID, []string{row.ParkID})
	if err != nil {
		return nil, err
	}
	atPark := map[string]bool{}
	for _, p := range people {
		atPark[p.PersonID] = true
	}
	occurred := biztime.BusinessDayStart(row.OccurredAt)
	seen := map[string]bool{}
	penalties := make([]ports.NewViolation, 0, len(req.Penalties))
	for _, p := range req.Penalties {
		if !atPark[strings.TrimSpace(p.PersonID)] {
			return nil, &Error{Code: "penalty_person", Message: disciplineCopy["error.penalty_person"], HTTPStatus: 422}
		}
		key := strings.TrimSpace(p.PersonID) + "|" + strings.TrimSpace(p.TypeKey)
		if seen[key] {
			return nil, &Error{Code: "penalty_twice", Message: disciplineCopy["error.penalty_twice"], HTTPStatus: 422}
		}
		seen[key] = true
		v, err := buildViolation(rules, p.PersonID, p.TypeKey, p.FineRupees, p.Note, occurred)
		if err != nil {
			return nil, err
		}
		penalties = append(penalties, v)
	}
	updated, err := s.repo.SubmitEnquiry(ctx, ports.SubmitEnquiryCommand{
		TenantID: tenantID, EnquiryID: row.EnquiryID, ActorUserID: actorID, Answers: answers,
		Penalties: penalties, RowVersion: req.RowVersion,
	})
	if err != nil {
		return nil, mapDisciplineError(err)
	}
	return s.composeDetail(ctx, tenantID, updated, rules, traceID)
}

// OpenDeathEnquiry opens the enquiry an approved death owes, idempotently on the animal. When the
// published HRMS SOP authors no enquiry for deaths, none opens (HR removed it on purpose).
func (s *DisciplineService) OpenDeathEnquiry(ctx context.Context, tenantID, goatID, parkID, shedID string, occurredAt time.Time) error {
	rules, err := s.rules.Published(ctx, tenantID)
	if err != nil {
		return err
	}
	enquiry, ok := rules.Document.Enquiry(hrmsdomain.TriggerAnimalDeath)
	if !ok {
		s.log.InfoContext(ctx, "hrms_enquiry_not_authored", slog.String("tenant_id", tenantID), slog.String("trigger", hrmsdomain.TriggerAnimalDeath))
		return nil
	}
	if parkID == "" {
		s.log.WarnContext(ctx, "hrms_enquiry_death_without_park", slog.String("tenant_id", tenantID), slog.String("goat_id", goatID))
		return nil
	}
	if occurredAt.IsZero() {
		occurredAt = s.now()
	}
	created, err := s.repo.OpenEnquiry(ctx, ports.OpenEnquiryCommand{
		TenantID: tenantID, TriggerKey: hrmsdomain.TriggerAnimalDeath, SubjectType: "goat", SubjectID: goatID,
		ParkID: parkID, ShedID: shedID, OccurredAt: occurredAt, OpenedAt: s.now(), DueHours: enquiry.DueHours, SOPVersion: rules.Version,
	})
	if err == nil && created {
		s.log.InfoContext(ctx, "hrms_enquiry_opened", slog.String("tenant_id", tenantID), slog.String("goat_id", goatID), slog.String("park_id", parkID))
	}
	return err
}

// ---------- helpers ----------

func (s *DisciplineService) scope(ctx context.Context, tenantID string, caller DisciplineCaller, parkID string) ([]domain.TimetablePark, []string, error) {
	parkRows, err := s.parks.TimetableParks(ctx, tenantID)
	if err != nil {
		return nil, nil, err
	}
	allowed := map[string]bool{}
	if !caller.All {
		heads, err := s.repo.ParkHeadParks(ctx, tenantID, caller.UserID)
		if err != nil {
			return nil, nil, err
		}
		for _, p := range heads {
			allowed[p] = true
		}
	}
	parks := []domain.TimetablePark{}
	known := false
	for _, p := range parkRows {
		if !caller.All && !allowed[p.ParkID] {
			continue
		}
		parks = append(parks, domain.TimetablePark{ParkID: p.ParkID, Label: p.Name})
		if p.ParkID == parkID {
			known = true
		}
	}
	parkID = strings.TrimSpace(parkID)
	if parkID != "" && parkID != "all" {
		if !known {
			return nil, nil, &Error{Code: "unknown_park", Message: disciplineCopy["error.park"], HTTPStatus: 422}
		}
		return parks, []string{parkID}, nil
	}
	if caller.All {
		return parks, nil, nil
	}
	scoped := make([]string, 0, len(parks))
	for _, p := range parks {
		scoped = append(scoped, p.ParkID)
	}
	return parks, scoped, nil
}

func (s *DisciplineService) visibleEnquiry(ctx context.Context, tenantID string, caller DisciplineCaller, enquiryID string) (ports.EnquiryRow, hrmsdomain.Rules, error) {
	row, err := s.repo.GetEnquiry(ctx, tenantID, strings.TrimSpace(enquiryID))
	if errors.Is(err, ports.ErrNotFound) {
		return ports.EnquiryRow{}, hrmsdomain.Rules{}, NotFound(disciplineCopy["error.enquiry"])
	}
	if err != nil {
		return ports.EnquiryRow{}, hrmsdomain.Rules{}, err
	}
	if !caller.All {
		heads, err := s.repo.ParkHeadParks(ctx, tenantID, caller.UserID)
		if err != nil {
			return ports.EnquiryRow{}, hrmsdomain.Rules{}, err
		}
		ok := false
		for _, p := range heads {
			ok = ok || p == row.ParkID
		}
		if !ok {
			// Another park's enquiry answers not-found: its existence is not the caller's business.
			return ports.EnquiryRow{}, hrmsdomain.Rules{}, NotFound(disciplineCopy["error.enquiry"])
		}
	}
	rules, err := s.rules.Version(ctx, tenantID, row.SOPVersion)
	if err != nil {
		return ports.EnquiryRow{}, hrmsdomain.Rules{}, err
	}
	return row, rules, nil
}

func (s *DisciplineService) composeDetail(ctx context.Context, tenantID string, row ports.EnquiryRow, rules hrmsdomain.Rules, traceID string) (*domain.EnquiryDetail, error) {
	violations, err := s.repo.ViolationsForEnquiry(ctx, tenantID, row.EnquiryID)
	if err != nil {
		return nil, err
	}
	people, err := s.repo.PersonOptions(ctx, tenantID, []string{row.ParkID})
	if err != nil {
		return nil, err
	}
	enquiry, _ := rules.Document.Enquiry(row.TriggerKey)
	out := &domain.EnquiryDetail{
		Enquiry: composeEnquiry(row, rules.Document, s.now()), Questions: []domain.EnquiryQuestion{}, Answers: row.Answers,
		Violations: []domain.Violation{}, Types: typeOptions(rules.Document), People: personOptions(people),
		CanSubmit: row.Status == domain.EnquiryOpen, SOPVersion: row.SOPVersion, TraceID: traceID,
	}
	for _, q := range enquiry.Questions {
		out.Questions = append(out.Questions, domain.EnquiryQuestion{ID: q.ID, Kind: q.Kind, Title: q.Title, Required: q.Required})
	}
	for _, v := range violations {
		out.Violations = append(out.Violations, composeViolation(v))
	}
	return out, nil
}

func buildViolation(rules hrmsdomain.Rules, personID, typeKey string, fine *int, note string, occurred time.Time) (ports.NewViolation, error) {
	t, ok := rules.Document.ViolationType(strings.TrimSpace(typeKey))
	if !ok || !t.Active {
		return ports.NewViolation{}, &Error{Code: "unknown_violation_type", Message: disciplineCopy["error.type"], HTTPStatus: 422}
	}
	amount := t.DefaultFine
	if fine != nil {
		amount = *fine
	}
	if amount < 0 || amount > hrmsdomain.MaxFineRupees {
		return ports.NewViolation{}, &Error{Code: "invalid_fine", Message: disciplineCopy["error.fine"], HTTPStatus: 422}
	}
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > maxNoteRunes {
		return ports.NewViolation{}, &Error{Code: "note_too_long", Message: disciplineCopy["error.note"], HTTPStatus: 422}
	}
	if strings.TrimSpace(personID) == "" {
		return ports.NewViolation{}, &Error{Code: "person_required", Message: disciplineCopy["error.person"], HTTPStatus: 422}
	}
	return ports.NewViolation{PersonID: strings.TrimSpace(personID), TypeKey: t.Key, TypeLabel: t.Title, FineRupees: amount,
		OccurredOn: occurred, Note: note, SOPVersion: rules.Version}, nil
}

// validateAnswers checks a report against the pinned questions: every required one answered, no
// answer to a question it does not ask, text bounded, yes/no a boolean.
func validateAnswers(questions []hrmsdomain.Question, raw map[string]any) (map[string]any, error) {
	asked := map[string]hrmsdomain.Question{}
	for _, q := range questions {
		asked[q.ID] = q
	}
	out := map[string]any{}
	for id, v := range raw {
		q, ok := asked[id]
		if !ok {
			return nil, &Error{Code: "unknown_answer", Message: disciplineCopy["error.answer_unknown"], HTTPStatus: 422}
		}
		switch q.Kind {
		case hrmsdomain.QuestionYesNo:
			b, ok := v.(bool)
			if !ok {
				return nil, &Error{Code: "invalid_answer", Message: fmt.Sprintf(disciplineCopy["error.answer_kind"], q.Title), HTTPStatus: 422}
			}
			out[id] = b
		default:
			text, _ := v.(string)
			text = strings.TrimSpace(text)
			if utf8.RuneCountInString(text) > maxAnswerRunes {
				return nil, &Error{Code: "answer_too_long", Message: disciplineCopy["error.answer_long"], HTTPStatus: 422}
			}
			if text != "" {
				out[id] = text
			}
		}
	}
	for _, q := range questions {
		if _, ok := out[q.ID]; q.Required && !ok {
			return nil, &Error{Code: "answer_required", Message: fmt.Sprintf(disciplineCopy["error.answer_required"], q.Title), HTTPStatus: 422}
		}
	}
	return out, nil
}

func (s *DisciplineService) pastDate(raw string) (time.Time, error) {
	d, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(raw), biztime.DefaultLocation())
	if err != nil || biztime.BusinessDate(d) > biztime.BusinessDate(s.now()) {
		return time.Time{}, &Error{Code: "invalid_date", Message: disciplineCopy["error.date"], HTTPStatus: 422}
	}
	return d, nil
}

// monthWindow turns "YYYY-MM" (default: this IST month) into [first day, first of next month).
func monthWindow(month string, now time.Time) (time.Time, time.Time, string, error) {
	loc := biztime.DefaultLocation()
	var first time.Time
	if strings.TrimSpace(month) == "" {
		n := now.In(loc)
		first = time.Date(n.Year(), n.Month(), 1, 0, 0, 0, 0, loc)
	} else {
		t, err := time.ParseInLocation("2006-01", strings.TrimSpace(month), loc)
		if err != nil {
			return time.Time{}, time.Time{}, "", &Error{Code: "invalid_month", Message: disciplineCopy["error.month"], HTTPStatus: 400}
		}
		first = t
	}
	return first, first.AddDate(0, 1, 0), first.Format("2006-01"), nil
}

// monthOptions are this month and the eleven before it, newest first.
func monthOptions(now time.Time) []domain.MonthOption {
	loc := biztime.DefaultLocation()
	n := now.In(loc)
	first := time.Date(n.Year(), n.Month(), 1, 0, 0, 0, 0, loc)
	out := make([]domain.MonthOption, 0, 12)
	for i := 0; i < 12; i++ {
		m := first.AddDate(0, -i, 0)
		out = append(out, domain.MonthOption{Key: m.Format("2006-01"), Label: m.Format("Jan 2006")})
	}
	return out
}

func typeOptions(doc hrmsdomain.Document) []domain.ViolationTypeOption {
	out := []domain.ViolationTypeOption{}
	for _, t := range doc.ActiveTypes() {
		out = append(out, domain.ViolationTypeOption{Key: t.Key, Title: t.Title, DefaultFine: t.DefaultFine, DefaultFineLabel: domain.RupeesLabel(t.DefaultFine)})
	}
	return out
}

func personOptions(rows []ports.PersonOptionRow) []domain.PersonOption {
	out := make([]domain.PersonOption, 0, len(rows))
	for _, p := range rows {
		out = append(out, domain.PersonOption{PersonID: p.PersonID, Name: p.Name, ParkID: p.ParkID, ParkLabel: p.ParkLabel,
			Designation: designationLabel(p.DesignationLabel, p.RoleHint, p.DesignationGrade, clockCopyFor("en"))})
	}
	return out
}

func farmTimestamp(t time.Time) string {
	return biztime.FarmDate(t) + " " + t.In(biztime.DefaultLocation()).Format("15:04")
}

func composeViolation(r ports.ViolationRow) domain.Violation {
	return domain.Violation{
		ViolationID: r.ViolationID, PersonID: r.PersonID, PersonName: r.PersonName, ParkID: r.ParkID, ParkLabel: r.ParkLabel,
		Designation: designationLabel(r.DesignationLabel, r.RoleHint, r.DesignationGrade, clockCopyFor("en")),
		TypeKey:     r.TypeKey, TypeLabel: r.TypeLabel, FineRupees: r.FineRupees, FineLabel: domain.RupeesLabel(r.FineRupees),
		OccurredOn: r.OccurredOn.Format("2006-01-02"), OccurredOnLabel: biztime.FarmDateFromBusinessDate(r.OccurredOn.Format("2006-01-02")),
		Note: r.Note, Source: r.Source, SourceLabel: disciplineCopy["source."+r.Source], EnquiryID: r.EnquiryID,
		RecordedByName: r.RecordedByName, RecordedAtLabel: farmTimestamp(r.RecordedAt),
		Status: r.Status, StatusLabel: disciplineCopy["status."+r.Status], WithdrawReason: r.WithdrawReason,
		SOPVersion: r.SOPVersion, RowVersion: r.RowVersion,
	}
}

func composeEnquiry(r ports.EnquiryRow, doc hrmsdomain.Document, now time.Time) domain.Enquiry {
	title := "Enquiry"
	if e, ok := doc.Enquiry(r.TriggerKey); ok {
		title = e.Title
	}
	out := domain.Enquiry{
		EnquiryID: r.EnquiryID, TriggerKey: r.TriggerKey, Title: title, SubjectLabel: r.SubjectLabel,
		ParkID: r.ParkID, ParkLabel: r.ParkLabel, OccurredAtLabel: biztime.FarmDate(r.OccurredAt),
		OpenedAtLabel: farmTimestamp(r.OpenedAt), DueAt: r.DueAt.UTC().Format(time.RFC3339), DueAtLabel: farmTimestamp(r.DueAt),
		Status: r.Status, SubmittedByName: r.SubmittedByName, PenaltyCount: r.PenaltyCount, RowVersion: r.RowVersion,
	}
	switch {
	case r.Status == domain.EnquirySubmitted:
		out.StatusLabel = disciplineCopy["enquiry.submitted"]
		switch r.PenaltyCount {
		case 0:
			out.PenaltyLabel = disciplineCopy["penalty.none"]
		case 1:
			out.PenaltyLabel = disciplineCopy["penalty.one"]
		default:
			out.PenaltyLabel = fmt.Sprintf(disciplineCopy["penalty.many"], r.PenaltyCount)
		}
	case now.After(r.DueAt):
		out.StatusLabel, out.Overdue = disciplineCopy["enquiry.overdue"], true
	default:
		out.StatusLabel = disciplineCopy["enquiry.open"]
	}
	if r.SubmittedAt != nil {
		out.SubmittedAtLabel = farmTimestamp(*r.SubmittedAt)
	}
	return out
}

func mapDisciplineError(err error) error {
	switch {
	case errors.Is(err, ports.ErrPersonNotFound):
		return &Error{Code: "person_not_found", Message: disciplineCopy["error.person"], HTTPStatus: 422}
	case errors.Is(err, ports.ErrViolationVersionConflict):
		return Conflict("violation_version_conflict", disciplineCopy["error.withdraw_conflict"])
	case errors.Is(err, ports.ErrEnquiryNotOpen):
		return Conflict("enquiry_submitted", disciplineCopy["error.enquiry_submitted"])
	case errors.Is(err, ports.ErrEnquiryVersionConflict):
		return Conflict("enquiry_version_conflict", disciplineCopy["error.enquiry_conflict"])
	case errors.Is(err, ports.ErrIdempotencyConflict):
		return Conflict("idempotency_conflict", disciplineCopy["error.idempotency"])
	case errors.Is(err, ports.ErrNotFound):
		return NotFound(disciplineCopy["error.enquiry"])
	}
	return err
}

func requestFingerprint(parts ...string) string {
	raw, _ := json.Marshal(parts)
	return string(raw)
}

// ---------- the death trigger ----------

// EventGoatExited is identity's exit event; an approved death carries exit_reason=died.
const EventGoatExited = "goat.exited"

type goatExitedForEnquiry struct {
	GoatID        string `json:"goat_id"`
	ExitReason    string `json:"exit_reason"`
	CurrentParkID string `json:"current_park_id"`
	CurrentShedID string `json:"current_shed_id"`
}

// DeathEnquiryHandler opens the death enquiry when a death is approved (maintainer decision
// 2026-09-30: every death, the park head decides whether anyone is penalised). Sold / culled /
// transferred exits open nothing. Replays open nothing twice (natural key on the animal).
type DeathEnquiryHandler struct {
	svc *DisciplineService
}

func NewDeathEnquiryHandler(svc *DisciplineService) *DeathEnquiryHandler {
	return &DeathEnquiryHandler{svc: svc}
}

var _ eventbus.Handler = (*DeathEnquiryHandler)(nil)

func (h *DeathEnquiryHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(EventGoatExited, h)
}

func (h *DeathEnquiryHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	var p goatExitedForEnquiry
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return eventbus.PermanentError(err)
		}
	}
	if p.ExitReason != "died" {
		return nil
	}
	goatID := strings.TrimSpace(p.GoatID)
	if goatID == "" {
		goatID = strings.TrimSpace(e.Key)
	}
	if goatID == "" || strings.TrimSpace(e.TenantID) == "" {
		return nil
	}
	return h.svc.OpenDeathEnquiry(ctx, e.TenantID, goatID, strings.TrimSpace(p.CurrentParkID), strings.TrimSpace(p.CurrentShedID), e.OccurredAt)
}
