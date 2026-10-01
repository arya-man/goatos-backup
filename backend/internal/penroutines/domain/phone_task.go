package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// PHONE-TASK SOP (maintainer instruction 2026-10-01, docs/decisions/simple-task-phone-tabs.md):
// "on creating the SOP it should also create this". A simple task is authored as an SOP on its
// module's SOP page (Preventive Care SOP, Feed SOP, ...). The SOP version's form_dsl carries ONE
// section, `phone_task`, holding everything: the tab (name, icon, filters), which pens, the
// schedule, the questions and captures, the verifier, and who does it in each park. Publishing the
// version writes the tab and one routine per park in the publish transaction; the routine engine
// (materializer, list, submit, verifier) then runs it unchanged.

// PhoneTaskSection is the form_dsl key of the document.
const PhoneTaskSection = "phone_task"

// PhoneTaskDoc is the authored document.
type PhoneTaskDoc struct {
	Tab            PhoneTaskTab    `json:"tab"`
	Instruction    string          `json:"instruction"`
	ScopeKind      string          `json:"scope_kind"`
	OccupiedOnly   *bool           `json:"occupied_only"`
	CadenceKind    string          `json:"cadence_kind"`
	Weekdays       []int           `json:"weekdays"`
	MonthDays      []int           `json:"month_days"`
	AfterWorkKinds []string        `json:"after_work_kinds"`
	IntervalDays   int             `json:"interval_days"`
	StartDate      string          `json:"start_date"`
	DueOffsetDays  *int            `json:"due_offset_days"`
	NotifyTime     string          `json:"notify_time"`
	ReviewKind     string          `json:"review_kind"`
	Evidence       Evidence        `json:"evidence"`
	Parks          []PhoneTaskPark `json:"parks"`
}

// PhoneTaskTab is the tab half of the document.
type PhoneTaskTab struct {
	Icon    string   `json:"icon"`
	Filters []string `json:"filters"`
}

// PhoneTaskPark is who does it, and on which pens, in one park.
type PhoneTaskPark struct {
	ParkID         string             `json:"park_id"`
	AssigneeUserID string             `json:"assignee_user_id"`
	Pens           []PhoneTaskPenWire `json:"pens"`
}

// PhoneTaskPenWire is one ticked pen.
type PhoneTaskPenWire struct {
	ShedID         string `json:"shed_id"`
	PartitionLabel string `json:"partition_label"`
}

// SOPModule maps an SOP code's prefix to the phone module whose bar carries the task.
var sopModulePrefixes = []struct {
	prefix string
	module string
}{
	{"pc_care.", "pc_care"},
	{"feed.", "feed_direction"},
	{"weighing.", "weighing"},
	{"counts.", "counts"},
	{"milk.", "milk"},
	{"procurement.", "vendors"},
	{"sales.", "sales"},
}

// PhoneModuleForSOPCode names the phone module of a module SOP, "" for a code outside them.
func PhoneModuleForSOPCode(code string) string {
	for _, p := range sopModulePrefixes {
		if strings.HasPrefix(code, p.prefix) {
			return p.module
		}
	}
	return ""
}

// PhoneTabKeyForSOPCode is the tab's stable route key: the code with its dots made underscores
// ("pc_care.task_fumigation_wash" -> "pc_care_task_fumigation_wash"), bounded to the key rule.
func PhoneTabKeyForSOPCode(code string) string {
	key := strings.ReplaceAll(code, ".", "_")
	if len(key) > 40 {
		key = strings.TrimRight(key[:40], "_")
	}
	return key
}

// ErrInvalidPhoneTask is every refusal of the document; its message is farm-worded.
var ErrInvalidPhoneTask = errors.New("the task is not valid")

// HasPhoneTask reports whether a form_dsl carries the section.
func HasPhoneTask(formDSL map[string]any) bool {
	_, ok := formDSL[PhoneTaskSection]
	return ok
}

// ParsePhoneTask decodes and validates the document of one SOP version. Unknown keys are refused:
// a field nothing reads is a setting the author thinks they made.
func ParsePhoneTask(sopCode, sopName string, formDSL map[string]any) (PhoneTaskDoc, error) {
	raw, err := json.Marshal(formDSL[PhoneTaskSection])
	if err != nil {
		return PhoneTaskDoc{}, fmt.Errorf("%w: %v", ErrInvalidPhoneTask, err)
	}
	var doc PhoneTaskDoc
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return PhoneTaskDoc{}, fmt.Errorf("%w: %v", ErrInvalidPhoneTask, err)
	}
	if PhoneModuleForSOPCode(sopCode) == "" {
		return PhoneTaskDoc{}, fmt.Errorf("%w: a task with its own phone tab belongs to a module's SOP page", ErrInvalidPhoneTask)
	}
	tab := NormalizeTab(Tab{Label: sopName, ModuleKey: PhoneModuleForSOPCode(sopCode), IconKey: doc.Tab.Icon, Filters: doc.Tab.Filters})
	if err := ValidateTab(tab); err != nil {
		return PhoneTaskDoc{}, fmt.Errorf("%w: %v", ErrInvalidPhoneTask, strings.TrimPrefix(err.Error(), ErrInvalidTab.Error()+": "))
	}
	doc.Tab.Filters = tab.Filters
	doc.Evidence = NormalizeEvidence(doc.Evidence)
	if err := ValidateEvidence(doc.Evidence); err != nil {
		return PhoneTaskDoc{}, err
	}
	if len(doc.Parks) == 0 {
		return PhoneTaskDoc{}, fmt.Errorf("%w: choose at least one park and who does it there", ErrInvalidPhoneTask)
	}
	seen := map[string]bool{}
	for _, park := range doc.Parks {
		if seen[park.ParkID] {
			return PhoneTaskDoc{}, fmt.Errorf("%w: a park is listed twice", ErrInvalidPhoneTask)
		}
		seen[park.ParkID] = true
		if err := ValidateDefinition(doc.DefinitionFor(sopName, park, "2000-01-01")); err != nil {
			return PhoneTaskDoc{}, err
		}
	}
	return doc, nil
}

// DefinitionFor is the routine one park runs under this document. today fills a blank start date.
func (doc PhoneTaskDoc) DefinitionFor(name string, park PhoneTaskPark, today string) Definition {
	pens := make([]PenRef, 0, len(park.Pens))
	for _, p := range park.Pens {
		pens = append(pens, PenRef{ShedID: strings.TrimSpace(p.ShedID), Partition: strings.TrimSpace(p.PartitionLabel)})
	}
	occupied := true
	if doc.OccupiedOnly != nil {
		occupied = *doc.OccupiedOnly
	}
	offset := 0
	if doc.DueOffsetDays != nil {
		offset = *doc.DueOffsetDays
	} else if doc.CadenceKind == CadenceAfterWork {
		offset = 1
	}
	interval := doc.IntervalDays
	if doc.CadenceKind != CadenceEveryNDays {
		interval = 0
	}
	start := strings.TrimSpace(doc.StartDate)
	if start == "" {
		start = today
	}
	notify := strings.TrimSpace(doc.NotifyTime)
	if notify == "" {
		notify = "07:00"
	}
	d := Definition{
		ParkID:         strings.TrimSpace(park.ParkID),
		Name:           strings.TrimSpace(name),
		Instruction:    strings.TrimSpace(doc.Instruction),
		ScopeKind:      doc.ScopeKind,
		OccupiedOnly:   occupied,
		Pens:           pens,
		CadenceKind:    doc.CadenceKind,
		Weekdays:       doc.Weekdays,
		MonthDays:      doc.MonthDays,
		AfterWorkKinds: SortWorkKinds(doc.AfterWorkKinds),
		IntervalDays:   interval,
		StartDate:      start,
		DueOffsetDays:  offset,
		NotifyTime:     notify,
		ReviewKind:     doc.ReviewKind,
		Evidence:       NormalizeEvidence(doc.Evidence),
		AssigneeUserID: strings.TrimSpace(park.AssigneeUserID),
	}
	if d.ScopeKind == ScopeAllPens {
		d.Pens = nil
	}
	return d
}
