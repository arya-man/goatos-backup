package http

import (
	"time"

	"github.com/vgoats/goatos/backend/internal/toxin/domain"
	"github.com/vgoats/goatos/backend/internal/toxin/ports"
)

// Step states the clients render verbatim.
const (
	stepStateDone      = "done"
	stepStateAvailable = "available"
	stepStateWaiting   = "waiting"
	stepStateLocked    = "locked"
)

// stepPayload is one procedure step with its live state. All copy is BACKEND-OWNED.
type stepPayload struct {
	StepNo      int    `json:"step_no"`
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	Instruction string `json:"instruction"`
	State       string `json:"state"`
	WaitMinutes int    `json:"wait_minutes,omitempty"`
	// AvailableAt is set while State is waiting: the server instant the step unlocks.
	AvailableAt string `json:"available_at,omitempty"`
	ProofRef    string `json:"proof_ref,omitempty"`
	CompletedBy string `json:"completed_by,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
}

// taskPayload is the wire shape of one toxin test round.
type taskPayload struct {
	TaskID         string  `json:"task_id"`
	FeedPurchaseID string  `json:"feed_purchase_id"`
	RoundNo        int     `json:"round_no"`
	Origin         string  `json:"origin"`
	OriginLine     string  `json:"origin_line,omitempty"`
	FarmLabel      string  `json:"farm_label"`
	FeedItemLabel  string  `json:"feed_item_label"`
	Vendor         string  `json:"vendor"`
	BatchNo        int     `json:"batch_no"`
	PurchaseDate   string  `json:"purchase_date"`
	QuantityKg     float64 `json:"quantity_kg"`
	Status         string  `json:"status"`
	StatusChip     string  `json:"status_chip"`
	// StatusTone is how the chip should READ, not how it should be coloured: the phone maps it
	// to its own palette. Sent because "Overdue" and "Step 2 of 5" are the same field and must
	// not look the same on the card.
	StatusTone string `json:"status_tone"`
	// IsOverdue is the 12-hour start deadline, decided on the SERVER clock. The phone must never
	// derive it: a device with a wrong clock would either hide a late load or redden a fresh one.
	IsOverdue bool `json:"is_overdue"`
	// DueAt is when this round became (or becomes) overdue, RFC3339. Blank when unknown.
	DueAt         string `json:"due_at,omitempty"`
	Outcome       string `json:"outcome,omitempty"`
	OutcomeLabel  string `json:"outcome_label,omitempty"`
	StripPhotoRef string `json:"strip_photo_ref,omitempty"`
	SubmittedBy   string `json:"submitted_by,omitempty"`
	SubmittedAt   string `json:"submitted_at,omitempty"`
	ReviewedAt    string `json:"reviewed_at,omitempty"`
	ReviewReason  string `json:"review_reason,omitempty"`
	CancelReason  string `json:"cancel_reason,omitempty"`
	StepsDone     int    `json:"steps_done"`
	StepsTotal    int    `json:"steps_total"`
	RowVersion    int64  `json:"row_version"`
	CreatedAt     string `json:"created_at"`
	// ContextLine is the backend-composed card subtitle: feed, vendor, load and date in
	// one farm-worded line.
	ContextLine string `json:"context_line"`
	// CanExecute tells the client whether THIS caller may run the test. It is the caller's
	// toxin.execute permission, not a property of the task: a CEO/CXO reading the same row
	// gets false and renders a non-tappable watch-only card, while a named tester gets true
	// and the card opens the step flow. The step routes enforce the same permission
	// independently — this field exists so the phone never offers an action the server will
	// refuse (maintainer decision 2026-08-26).
	CanExecute bool `json:"can_execute"`
}

type taskPagePayload struct {
	Tasks        []taskPayload  `json:"tasks"`
	NextCursor   string         `json:"next_cursor,omitempty"`
	StatusCounts map[string]int `json:"status_counts"`
	// Filters are the list's selectable slices in display order, with BACKEND-OWNED labels.
	// Clients render them verbatim and send back only the KEY — never a status list of their
	// own, so "Pending" means one thing across every surface.
	Filters []taskFilterPayload `json:"filters,omitempty"`
}

// taskFilterPayload is one filter chip. Count is a WHOLE-TENANT aggregate over the filter's
// statuses, never a page-local sum.
type taskFilterPayload struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Count    int    `json:"count"`
	Selected bool   `json:"selected"`
	// EmptyMessage is this slice's own "nothing here" copy, rendered verbatim when it has no
	// rows. Per-slice because one message would be wrong in two of the three.
	EmptyMessage string `json:"empty_message"`
}

// toFilterPayloads composes the chips for the selected key.
func toFilterPayloads(selectedKey string, statusCounts map[string]int) []taskFilterPayload {
	specs := domain.TaskFilters()
	out := make([]taskFilterPayload, 0, len(specs))
	for _, spec := range specs {
		out = append(out, taskFilterPayload{
			Key:          spec.Key,
			Label:        spec.Label,
			Count:        domain.CountForFilter(spec.Key, statusCounts),
			Selected:     spec.Key == selectedKey,
			EmptyMessage: spec.EmptyMessage,
		})
	}
	return out
}

type taskDetailPayload struct {
	taskPayload
	Steps          []stepPayload   `json:"steps"`
	ReadingGuide   []string        `json:"reading_guide"`
	OutcomeOptions []outcomeOption `json:"outcome_options"`
}

type outcomeOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type completeStepPayload struct {
	ProofRef string `json:"proof_ref"`
}

type submitPayload struct {
	Outcome       string `json:"outcome"`
	StripPhotoRef string `json:"strip_photo_ref"`
}

type verdictPayload struct {
	Decision   string `json:"decision"`
	Reason     string `json:"reason,omitempty"`
	RowVersion int64  `json:"row_version"`
}

func toTaskPayload(row ports.TaskRow, now time.Time, canExecute bool) taskPayload {
	t := row.Task
	context := t.FeedItemLabel
	if t.Vendor != "" {
		context += " · " + t.Vendor
	}
	context += " · " + t.FarmLabel + " · " + t.PurchaseDate
	return taskPayload{
		TaskID:         t.TaskID,
		FeedPurchaseID: t.FeedPurchaseID,
		RoundNo:        t.RoundNo,
		Origin:         t.Origin,
		OriginLine:     domain.OriginLine(t.Origin),
		FarmLabel:      t.FarmLabel,
		FeedItemLabel:  t.FeedItemLabel,
		Vendor:         t.Vendor,
		BatchNo:        t.BatchNo,
		PurchaseDate:   t.PurchaseDate,
		QuantityKg:     t.QuantityKg,
		Status:         t.Status,
		StatusChip:     domain.StatusChip(t, row.Completions, now),
		StatusTone:     statusTone(t, row.Completions, now),
		IsOverdue:      taskIsOverdue(t, now),
		DueAt:          taskDueAt(t),
		Outcome:        t.Outcome,
		OutcomeLabel:   domain.OutcomeLabel(t.Outcome),
		StripPhotoRef:  t.StripPhotoRef,
		SubmittedBy:    t.SubmittedBy,
		SubmittedAt:    t.SubmittedAt,
		ReviewedAt:     t.ReviewedAt,
		ReviewReason:   t.ReviewReason,
		CancelReason:   t.CancelReason,
		StepsDone:      len(row.Completions),
		StepsTotal:     len(domain.WorkingSteps()),
		RowVersion:     t.RowVersion,
		CreatedAt:      t.CreatedAt,
		ContextLine:    context,
		CanExecute:     canExecute,
	}
}

// toStepPayloads composes each step's live state against the server clock. The phone
// renders the states verbatim and never derives its own gate logic — its clock is not
// the gate's clock.
func toStepPayloads(row ports.TaskRow, now time.Time, canExecute bool) []stepPayload {
	next := domain.NextStepNo(row.Completions)
	out := make([]stepPayload, 0, len(domain.Steps()))
	for _, spec := range domain.Steps() {
		p := stepPayload{
			StepNo:      spec.No,
			Kind:        spec.Kind,
			Title:       spec.Title,
			Instruction: spec.Instruction,
			WaitMinutes: spec.WaitMinutes,
		}
		if done, ok := completionFor(row.Completions, spec.No); ok {
			p.State = stepStateDone
			p.ProofRef = done.ProofRef
			p.CompletedBy = done.CompletedBy
			p.CompletedAt = done.CompletedAt.UTC().Format(time.RFC3339)
			out = append(out, p)
			continue
		}
		if spec.Kind == domain.StepKindWait {
			// The wait row mirrors the step it gates: done once the settling hour has
			// passed, waiting while it runs, locked before the shake video exists.
			gated, _ := domain.StepSpecFor(5)
			opensAt := domain.GateOpensAt(gated, row.Completions)
			switch {
			case opensAt.IsZero():
				p.State = stepStateLocked
			case now.Before(opensAt):
				p.State = stepStateWaiting
				p.AvailableAt = opensAt.UTC().Format(time.RFC3339)
			default:
				p.State = stepStateDone
			}
			out = append(out, p)
			continue
		}
		// A watcher (no toxin.execute) sees the history but never an actionable step: the
		// next step renders locked rather than available, so no camera is offered.
		if !canExecute || row.Task.Status != domain.StatusInProgress || next == 0 || spec.No != next {
			p.State = stepStateLocked
			out = append(out, p)
			continue
		}
		if opensAt := domain.GateOpensAt(spec, row.Completions); !opensAt.IsZero() && now.Before(opensAt) {
			p.State = stepStateWaiting
			p.AvailableAt = opensAt.UTC().Format(time.RFC3339)
		} else {
			p.State = stepStateAvailable
		}
		out = append(out, p)
	}
	return out
}

func completionFor(completions []domain.StepCompletion, stepNo int) (domain.StepCompletion, bool) {
	for _, c := range completions {
		if c.StepNo == stepNo {
			return c, true
		}
	}
	return domain.StepCompletion{}, false
}

func toTaskDetailPayload(row ports.TaskRow, now time.Time, canExecute bool) taskDetailPayload {
	return taskDetailPayload{
		taskPayload:  toTaskPayload(row, now, canExecute),
		Steps:        toStepPayloads(row, now, canExecute),
		ReadingGuide: domain.ReadingGuide(),
		OutcomeOptions: []outcomeOption{
			{Value: domain.OutcomeNegative, Label: domain.OutcomeLabel(domain.OutcomeNegative)},
			{Value: domain.OutcomePositive, Label: domain.OutcomeLabel(domain.OutcomePositive)},
			{Value: domain.OutcomeInvalid, Label: domain.OutcomeLabel(domain.OutcomeInvalid)},
		},
	}
}

// taskIsOverdue and taskDueAt resolve the 12-hour start clock from the task's own created_at.
// Parsing failure degrades to "not overdue" rather than to a red card on a timestamp we could not
// read — an unreadable clock is not evidence that work is late.
func taskIsOverdue(t domain.Task, now time.Time) bool {
	createdAt, err := time.Parse(time.RFC3339Nano, t.CreatedAt)
	if err != nil {
		return false
	}
	return domain.IsOverdue(t, createdAt, now)
}

func taskDueAt(t domain.Task) string {
	createdAt, err := time.Parse(time.RFC3339Nano, t.CreatedAt)
	if err != nil {
		return ""
	}
	due := domain.DueAt(createdAt)
	if due.IsZero() {
		return ""
	}
	return due.UTC().Format(time.RFC3339)
}

// statusTone tells the card how the chip should read. Overdue is the only DANGER on this screen;
// a finished round is OK; everything else is ordinary progress.
func statusTone(t domain.Task, completions []domain.StepCompletion, now time.Time) string {
	switch {
	case taskIsOverdue(t, now):
		return "danger"
	case t.Status == domain.StatusAccepted:
		return "ok"
	case t.Status == domain.StatusPendingReview:
		return "info"
	case t.Status == domain.StatusCancelled:
		return "muted"
	default:
		return "muted"
	}
}
