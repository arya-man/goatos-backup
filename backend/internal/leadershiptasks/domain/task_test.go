package domain

import (
	"errors"
	"testing"
	"time"
)

const (
	raiser   = "33333333-3333-4333-8333-333333333333"
	assignee = "44444444-4444-4444-8444-444444444444"
	stranger = "55555555-5555-4555-8555-555555555555"
)

func sample(status string) Task {
	return Task{
		TaskID:         "22222222-2222-4222-8222-222222222222",
		TaskNo:         12,
		Title:          "Approve the vendor contract",
		Status:         status,
		RaisedByUserID: raiser,
		RaisedByName:   "Hemant",
		AssigneeUserID: assignee,
		AssigneeName:   "Ravi",
		RaisedAt:       time.Date(2026, 9, 4, 5, 30, 0, 0, time.UTC),
	}
}

var (
	director = Actor{UserID: raiser, CanRaise: true}
	cxo      = Actor{UserID: assignee, CanAct: true}
	nobody   = Actor{UserID: stranger, CanRaise: true, CanAct: true}
)

// The status ladder from each side: the CXO walks it, the director only cancels, a
// stranger (even one holding both permissions) gets nothing. Cancelled is terminal for
// everyone; done can be reopened by the CXO only.
func TestStatusOptionsAndTransitionsAgree(t *testing.T) {
	cases := []struct {
		status string
		actor  Actor
		want   []string
	}{
		{StatusOpen, cxo, []string{StatusInProgress, StatusDone}},
		{StatusInProgress, cxo, []string{StatusDone}},
		{StatusDone, cxo, []string{StatusInProgress}},
		{StatusCancelled, cxo, nil},
		{StatusOpen, director, []string{StatusCancelled}},
		{StatusInProgress, director, []string{StatusCancelled}},
		{StatusDone, director, nil},
		{StatusCancelled, director, nil},
		{StatusOpen, nobody, nil},
	}
	for _, c := range cases {
		task := sample(c.status)
		got := StatusOptionsFor(task, c.actor)
		if len(got) != len(c.want) {
			t.Fatalf("%s/%s: options %+v, want %v", c.status, c.actor.UserID, got, c.want)
		}
		for i, opt := range got {
			if opt.Key != c.want[i] || opt.Label == "" {
				t.Fatalf("%s/%s: option %d = %+v, want %s with a label", c.status, c.actor.UserID, i, opt, c.want[i])
			}
			if err := CheckTransition(task, c.actor, opt.Key); err != nil {
				t.Fatalf("%s -> %s by %s must be allowed, got %v", c.status, opt.Key, c.actor.UserID, err)
			}
		}
		// Every status NOT offered must be refused: the options ARE the rule.
		for _, s := range []string{StatusOpen, StatusInProgress, StatusDone, StatusCancelled} {
			offered := false
			for _, w := range c.want {
				offered = offered || w == s
			}
			if offered {
				continue
			}
			if err := CheckTransition(task, c.actor, s); err == nil {
				t.Fatalf("%s -> %s by %s must be refused", c.status, s, c.actor.UserID)
			}
		}
	}
	if err := CheckTransition(sample(StatusOpen), cxo, "bogus"); !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("unknown status: %v", err)
	}
	if err := CheckTransition(sample(StatusDone), director, StatusCancelled); !errors.Is(err, ErrTaskClosed) {
		t.Fatalf("cancelling a done task must read as closed, got %v", err)
	}
	if err := CheckTransition(sample(StatusOpen), nobody, StatusCancelled); !errors.Is(err, ErrNotRaiser) {
		t.Fatalf("a stranger cancelling must read as not-raiser, got %v", err)
	}
	if err := CheckTransition(sample(StatusOpen), nobody, StatusDone); !errors.Is(err, ErrNotAssignee) {
		t.Fatalf("a stranger completing must read as not-assignee, got %v", err)
	}
}

func TestCapabilitiesFollowPartyAndStatus(t *testing.T) {
	open := sample(StatusOpen)
	if !open.CanEdit(director) || !open.CanCancel(director) || open.CanChangeStatus(director) {
		t.Fatal("the raiser edits and cancels an open task and never moves its status")
	}
	if open.CanEdit(cxo) || open.CanCancel(cxo) || !open.CanChangeStatus(cxo) {
		t.Fatal("the assignee moves status and never edits or cancels")
	}
	done := sample(StatusDone)
	if done.CanEdit(director) || done.CanCancel(director) {
		t.Fatal("a done task is history for the raiser")
	}
	if !done.CanChangeStatus(cxo) {
		t.Fatal("the assignee may reopen a done task")
	}
	cancelled := sample(StatusCancelled)
	if cancelled.CanChangeStatus(cxo) || cancelled.CanEdit(director) {
		t.Fatal("a cancelled task is terminal for everyone")
	}
	// The permission half matters too: a raiser whose grant lost raise cannot edit.
	if open.CanEdit(Actor{UserID: raiser}) {
		t.Fatal("editing needs the raise permission, not only authorship")
	}
}

func TestBriefValidation(t *testing.T) {
	long := make([]rune, MaxTitleRunes+1)
	for i := range long {
		long[i] = 'x'
	}
	if err := ValidateBrief("", "", nil); !errors.Is(err, ErrTitleRequired) {
		t.Fatalf("blank title: %v", err)
	}
	if err := ValidateBrief(string(long), "", nil); !errors.Is(err, ErrTitleTooLong) {
		t.Fatalf("long title: %v", err)
	}
	refs := make([]AttachmentRef, MaxAttachments+1)
	for i := range refs {
		refs[i] = AttachmentRef{ProofID: "p" + string(rune('a'+i)), Kind: AttachmentFile}
	}
	if err := ValidateBrief("ok", "", refs); !errors.Is(err, ErrTooManyAttachments) {
		t.Fatalf("too many: %v", err)
	}
	if err := ValidateBrief("ok", "", []AttachmentRef{{ProofID: "p1", Kind: "zip"}}); !errors.Is(err, ErrInvalidAttachmentKind) {
		t.Fatalf("bad kind: %v", err)
	}
	if err := ValidateBrief("ok", "", []AttachmentRef{{ProofID: "p1", Kind: AttachmentAudio}, {ProofID: "p1", Kind: AttachmentAudio}}); !errors.Is(err, ErrDuplicateAttachment) {
		t.Fatalf("duplicate: %v", err)
	}
	if err := ValidateBrief("ok", "", []AttachmentRef{{ProofID: "", Kind: AttachmentAudio}}); !errors.Is(err, ErrAttachmentProofRequired) {
		t.Fatalf("missing proof: %v", err)
	}
	if err := ValidateBrief("  ok  ", "brief", []AttachmentRef{{ProofID: "p1", Kind: AttachmentPhoto, FileName: "a.jpg"}}); err != nil {
		t.Fatalf("valid brief: %v", err)
	}
}

// Copy is composed from the viewer's side, in IST, and never shows an id.
func TestCopyIsFarmWordedFromTheViewersSide(t *testing.T) {
	task := sample(StatusInProgress)
	if got := MetaLine(task, cxo); got != "Raised by Hemant · 04/09/2026" {
		t.Fatalf("assignee meta line = %q", got)
	}
	if got := MetaLine(task, director); got != "For Ravi · 04/09/2026" {
		t.Fatalf("raiser meta line = %q", got)
	}
	if got := NumberLabel(task.TaskNo); got != "#12" {
		t.Fatalf("number label = %q", got)
	}
	if StatusChip(StatusInProgress) != "Doing" || StatusChip(StatusCancelled) != "Cancelled" {
		t.Fatal("status chips must be the farm words")
	}
	unnamed := task
	unnamed.RaisedByName = ""
	if got := MetaLine(unnamed, cxo); got != "Raised by a director · 04/09/2026" {
		t.Fatalf("a name that does not resolve must degrade to a role word, never an id: %q", got)
	}
}

func TestFiltersHideCancelledAndCountWholeList(t *testing.T) {
	if FilterKeyOrDefault("bogus") != FilterAll || FilterKeyOrDefault(" done ") != FilterDone {
		t.Fatal("filter key normalization")
	}
	all := StatusesForFilter(FilterAll)
	for _, s := range all {
		if s == StatusCancelled {
			t.Fatal("All must hide cancelled tasks")
		}
	}
	counts := map[string]int{StatusOpen: 2, StatusInProgress: 1, StatusDone: 4, StatusCancelled: 9}
	if FilterCount(FilterAll, counts) != 7 || FilterCount(FilterDone, counts) != 4 {
		t.Fatal("filter counts")
	}
	if FilterEmptyMessage(FilterAll, true) == FilterEmptyMessage(FilterAll, false) {
		t.Fatal("the empty line differs between a director (invited to raise) and a CXO (desk clear)")
	}
}
