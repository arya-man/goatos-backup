package http

import (
	"encoding/json"

	"github.com/vgoats/goatos/backend/internal/penroutines/domain"
	"github.com/vgoats/goatos/backend/internal/penroutines/ports"
)

// Wire shapes. Every visible string is composed by the domain and rendered verbatim by the
// phone and the web; the client never composes a chip, a reason line or a date of its own.

// stepPayload is the shared step shape (domain.Step): the task as every surface renders it.
type stepPayload = domain.Step

type detailPayload struct {
	Task    stepPayload `json:"task"`
	TraceID string      `json:"trace_id"`
}

type filterPayload struct {
	Key          string `json:"key"`
	Label        string `json:"label"`
	Count        int    `json:"count"`
	Selected     bool   `json:"selected"`
	EmptyMessage string `json:"empty_message"`
}

type pagePayload struct {
	Title      string          `json:"title"`
	Rows       []stepPayload   `json:"rows"`
	NextCursor *string         `json:"next_cursor"`
	Filters    []filterPayload `json:"filters"`
	OpenCount  int             `json:"open_count"`
	TraceID    string          `json:"trace_id"`
}

type presencePayload struct {
	EventType  string                   `json:"event_type"`
	CapturedAt string                   `json:"captured_at"`
	RowVersion int                      `json:"row_version"`
	Location   domain.PresenceLocation  `json:"location"`
	Integrity  domain.PresenceIntegrity `json:"integrity"`
}

type submitPayload struct {
	Answers    map[string]json.RawMessage `json:"answers"`
	ProofRefs  []domain.ProofItem         `json:"proof_refs"`
	RowVersion int                        `json:"row_version"`
	CapturedAt string                     `json:"captured_at"`
	Location   domain.PresenceLocation    `json:"location"`
	Integrity  domain.PresenceIntegrity   `json:"integrity"`
}

const listTitle = "Routines"

func toFilterPayloads(selected string, counts map[string]int) []filterPayload {
	out := make([]filterPayload, 0, len(domain.FilterKeys))
	for _, key := range domain.FilterKeys {
		out = append(out, filterPayload{
			Key:          key,
			Label:        domain.FilterLabel(key),
			Count:        domain.FilterCount(key, counts),
			Selected:     key == selected,
			EmptyMessage: domain.FilterEmptyMessage(key),
		})
	}
	return out
}

// --- admin (authoring) shapes ---

type penPayload struct {
	ShedID         string `json:"shed_id"`
	ShedName       string `json:"shed_name"`
	PartitionLabel string `json:"partition_label"`
	Display        string `json:"operational_location_display"`
}

// rolePayload is one role a routine is for, with its farm label.
type rolePayload struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// rolePersonPayload is one person who currently holds one of the routine's roles in its park
// (a read-only preview; the routine stores roles, never people).
type rolePersonPayload struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
	RoleLabel   string `json:"role_label"`
}

// routineRow is the web table row: the rule, its lines, its roles and who holds them today,
// and the two counts.
type routineRow struct {
	RoutineID      string              `json:"routine_id"`
	ParkID         string              `json:"park_id"`
	ParkName       string              `json:"park_name"`
	Name           string              `json:"name"`
	Instruction    string              `json:"instruction"`
	ScopeKind      string              `json:"scope_kind"`
	OccupiedOnly   bool                `json:"occupied_only"`
	Pens           []penPayload        `json:"pens"`
	CadenceKind    string              `json:"cadence_kind"`
	Weekdays       []int               `json:"weekdays"`
	MonthDays      []int               `json:"month_days"`
	AfterWorkKinds []string            `json:"after_work_kinds"`
	IntervalDays   *int                `json:"interval_days"`
	StartDate      string              `json:"start_date"`
	CadenceLine    string              `json:"cadence_line"`
	DueOffsetDays  int                 `json:"due_offset_days"`
	NotifyTime     string              `json:"notify_time"`
	ReviewKind     string              `json:"review_kind"`
	Status         string              `json:"status"`
	StatusLabel    string              `json:"status_label"`
	CurrentVersion int                 `json:"current_version"`
	Evidence       domain.Evidence     `json:"evidence"`
	EvidenceLine   string              `json:"evidence_line"`
	AssigneeRoles  []rolePayload       `json:"assignee_roles"`
	People         []rolePersonPayload `json:"people"`
	OpenToday      int                 `json:"open_today"`
	Delayed        int                 `json:"delayed"`
	CreatedAt      string              `json:"created_at"`
	UpdatedAt      string              `json:"updated_at"`
	RowVersion     int                 `json:"row_version"`
}

type parkPayload struct {
	ParkID string `json:"park_id"`
	Name   string `json:"name"`
}

type routineListPayload struct {
	Rows    []routineRow  `json:"rows"`
	Parks   []parkPayload `json:"parks"`
	TraceID string        `json:"trace_id"`
}

type routineDetailPayload struct {
	Routine routineRow `json:"routine"`
	TraceID string     `json:"trace_id"`
}

type catalogPenPayload struct {
	ShedID         string `json:"shed_id"`
	ShedName       string `json:"shed_name"`
	PartitionLabel string `json:"partition_label"`
	Display        string `json:"operational_location_display"`
	Occupied       bool   `json:"occupied"`
}

type personPayload struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
}

// catalogRolePayload is one assignable role and who holds it for the requested park.
type catalogRolePayload struct {
	Key    string          `json:"key"`
	Label  string          `json:"label"`
	People []personPayload `json:"people"`
}

type optionPayload struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type catalogDefaults struct {
	NotifyTime    string `json:"notify_time"`
	DueOffsetDays int    `json:"due_offset_days"`
	StartDate     string `json:"start_date"`
	IntervalDays  int    `json:"interval_days"`
}

type catalogPayload struct {
	Pens          []catalogPenPayload  `json:"pens"`
	Roles         []catalogRolePayload `json:"roles"`
	WorkKinds     []optionPayload      `json:"work_kinds"`
	QuestionKinds []optionPayload      `json:"question_kinds"`
	CadenceKinds  []optionPayload      `json:"cadence_kinds"`
	ReviewKinds   []optionPayload      `json:"review_kinds"`
	PresenceKinds []optionPayload      `json:"presence_kinds"`
	ScopeKinds    []optionPayload      `json:"scope_kinds"`
	Defaults      catalogDefaults      `json:"defaults"`
	TraceID       string               `json:"trace_id"`
}

type penWrite struct {
	ShedID         string `json:"shed_id"`
	PartitionLabel string `json:"partition_label"`
}

// routineWrite is the create / update body.
type routineWrite struct {
	ParkID         string          `json:"park_id"`
	Name           string          `json:"name"`
	Instruction    string          `json:"instruction"`
	ScopeKind      string          `json:"scope_kind"`
	OccupiedOnly   *bool           `json:"occupied_only"`
	Pens           []penWrite      `json:"pens"`
	CadenceKind    string          `json:"cadence_kind"`
	Weekdays       []int           `json:"weekdays"`
	MonthDays      []int           `json:"month_days"`
	AfterWorkKinds []string        `json:"after_work_kinds"`
	IntervalDays   *int            `json:"interval_days"`
	StartDate      string          `json:"start_date"`
	DueOffsetDays  *int            `json:"due_offset_days"`
	NotifyTime     string          `json:"notify_time"`
	ReviewKind     string          `json:"review_kind"`
	Evidence       domain.Evidence `json:"evidence"`
	AssigneeRoles  []string        `json:"assignee_roles"`
	RowVersion     int             `json:"row_version"`
}

type statusWrite struct {
	Status     string `json:"status"`
	RowVersion int    `json:"row_version"`
}

// taskRow is a Today-table row: the step plus the names of the people who hold the routine's
// roles for the park today.
type taskRow struct {
	domain.Step
	AssigneeNames []string `json:"assignee_names"`
}

type taskListPayload struct {
	Rows       []taskRow         `json:"rows"`
	NextCursor *string           `json:"next_cursor"`
	Summary    ports.ParkSummary `json:"summary"`
	TraceID    string            `json:"trace_id"`
}

func toRoutineRow(row ports.RoutineListRow) routineRow {
	d := row.Definition
	pens := make([]penPayload, 0, len(d.Pens))
	for _, p := range d.Pens {
		pens = append(pens, penPayload{ShedID: p.ShedID, ShedName: p.ShedName, PartitionLabel: p.Partition, Display: p.Label})
	}
	roles := make([]rolePayload, 0, len(d.AssigneeRoles))
	for _, key := range domain.SortRoles(d.AssigneeRoles) {
		roles = append(roles, rolePayload{Key: key, Label: domain.RoleLabel(key)})
	}
	people := make([]rolePersonPayload, 0, len(d.People))
	for _, p := range d.People {
		people = append(people, rolePersonPayload{UserID: p.UserID, DisplayName: p.DisplayName, RoleLabel: domain.RoleLabel(p.RoleKey)})
	}
	var interval *int
	if d.CadenceKind == domain.CadenceEveryNDays {
		v := d.IntervalDays
		interval = &v
	}
	weekdays, monthDays := d.Weekdays, d.MonthDays
	if weekdays == nil {
		weekdays = []int{}
	}
	if monthDays == nil {
		monthDays = []int{}
	}
	kinds := domain.SortWorkKinds(d.AfterWorkKinds)
	if kinds == nil {
		kinds = []string{}
	}
	ev := d.Evidence
	if ev.Questions == nil {
		ev.Questions = []domain.Question{}
	}
	return routineRow{
		RoutineID:      d.RoutineID,
		ParkID:         d.ParkID,
		ParkName:       d.ParkName,
		Name:           d.Name,
		Instruction:    d.Instruction,
		ScopeKind:      d.ScopeKind,
		OccupiedOnly:   d.OccupiedOnly,
		Pens:           pens,
		CadenceKind:    d.CadenceKind,
		Weekdays:       weekdays,
		MonthDays:      monthDays,
		AfterWorkKinds: kinds,
		IntervalDays:   interval,
		StartDate:      d.StartDate,
		CadenceLine:    domain.CadenceLine(d),
		DueOffsetDays:  d.DueOffsetDays,
		NotifyTime:     d.NotifyTime,
		ReviewKind:     d.ReviewKind,
		Status:         d.Status,
		StatusLabel:    domain.StatusLabel(d.Status),
		CurrentVersion: d.CurrentVersion,
		Evidence:       ev,
		EvidenceLine:   domain.EvidenceLineForScope(d.Evidence, d.ScopeKind),
		AssigneeRoles:  roles,
		People:         people,
		OpenToday:      row.OpenToday,
		Delayed:        row.Delayed,
		CreatedAt:      d.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:      d.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		RowVersion:     d.RowVersion,
	}
}

func (w routineWrite) toDefinition() domain.Definition {
	pens := make([]domain.PenRef, 0, len(w.Pens))
	for _, p := range w.Pens {
		pens = append(pens, domain.PenRef{ShedID: p.ShedID, Partition: p.PartitionLabel})
	}
	occupied := true
	if w.OccupiedOnly != nil {
		occupied = *w.OccupiedOnly
	}
	offset := 0
	if w.DueOffsetDays != nil {
		offset = *w.DueOffsetDays
	} else if w.CadenceKind == domain.CadenceAfterWork {
		offset = 1
	}
	interval := 0
	if w.IntervalDays != nil {
		interval = *w.IntervalDays
	}
	return domain.Definition{
		ParkID:         w.ParkID,
		Name:           w.Name,
		Instruction:    w.Instruction,
		ScopeKind:      w.ScopeKind,
		OccupiedOnly:   occupied,
		Pens:           pens,
		CadenceKind:    w.CadenceKind,
		Weekdays:       w.Weekdays,
		MonthDays:      w.MonthDays,
		AfterWorkKinds: w.AfterWorkKinds,
		IntervalDays:   interval,
		StartDate:      w.StartDate,
		DueOffsetDays:  offset,
		NotifyTime:     w.NotifyTime,
		ReviewKind:     w.ReviewKind,
		Evidence:       w.Evidence,
		AssigneeRoles:  w.AssigneeRoles,
		RowVersion:     w.RowVersion,
	}
}

func options(pairs ...[2]string) []optionPayload {
	out := make([]optionPayload, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, optionPayload{Key: p[0], Label: p[1]})
	}
	return out
}

func workKindOptions() []optionPayload {
	out := make([]optionPayload, 0, len(domain.WorkKinds))
	for _, k := range domain.WorkKinds {
		out = append(out, optionPayload{Key: k, Label: domain.WorkKindLabel(k)})
	}
	return out
}
