package domain

import (
	"fmt"
	"strings"
	"time"
)

const (
	MilkFeedingVerificationNotSubmitted = "not_submitted"
	MilkFeedingVerificationPending      = "pending_verification"
	MilkFeedingVerificationCompleted    = "completed"
	MilkFeedingVerificationRework       = "rework"

	MilkFeedingStepCleanBottles     = "clean_bottles"
	MilkFeedingStepMixingAndFilling = "mixing_and_filling"

	VerificationVerticalMilkFeeding = "counts"
	VerificationModuleMilkFeeding   = "milk_feeding"
	VerificationCategoryMilkFeeding = "milk_feeding"
	VerificationRefTypeMilkFeeding  = "milk_feeding_completion"
)

type MilkFeedingSessionDefinition struct {
	SessionNo int    `json:"session_no"`
	DueTime   string `json:"due_time"`
}

func MilkFeedingSessions() []MilkFeedingSessionDefinition {
	return []MilkFeedingSessionDefinition{{1, "08:00"}, {2, "12:00"}, {3, "16:00"}, {4, "21:00"}}
}

func MilkFeedingSessionDueTime(sessionNo int) (string, bool) {
	for _, session := range MilkFeedingSessions() {
		if session.SessionNo == sessionNo {
			return session.DueTime, true
		}
	}
	return "", false
}

func MilkFeedingIsAvailable(feedingDate, dueTime string, now time.Time) bool {
	india, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		return false
	}
	availableAt, err := time.ParseInLocation("2006-01-02 15:04", strings.TrimSpace(feedingDate)+" "+strings.TrimSpace(dueTime), india)
	if err != nil {
		return false
	}
	return MilkFeedingAvailableAt(availableAt, now)
}

func MilkFeedingAvailableAt(availableAt, now time.Time) bool { return !now.Before(availableAt) }

type MilkFeedingWatchlistKid struct {
	GoatID         string `json:"goat_id"`
	ConsecutiveYes int    `json:"consecutive_yes"`
	Remarks        string `json:"remarks,omitempty"`
	AddedDate      string `json:"added_date"`
	AddedSession   int    `json:"added_session"`
}

type MilkFeedingWatchlistAnswer struct {
	GoatID    string `json:"goat_id"`
	DrankMilk bool   `json:"drank_milk"`
}

type MilkFeedingNewRefusal struct {
	GoatID  string `json:"goat_id"`
	Remarks string `json:"remarks,omitempty"`
}

type MilkFeedingAnswers struct {
	WatchlistAnswers     []MilkFeedingWatchlistAnswer `json:"watchlist_answers"`
	TotalKidsFed         int                          `json:"total_kids_fed"`
	Attempt1NotDrinking  int                          `json:"attempt_1_not_drinking"`
	Attempt2NotDrinking  int                          `json:"attempt_2_not_drinking"`
	NewRefusals          []MilkFeedingNewRefusal      `json:"new_refusals"`
	UdderMilkNotDrinking int                          `json:"udder_milk_not_drinking"`
	ORSNotDrinking       int                          `json:"ors_not_drinking"`
}

func (a MilkFeedingAnswers) Validate(watchlist []MilkFeedingWatchlistKid) error {
	if a.TotalKidsFed < 0 || a.Attempt1NotDrinking < 0 || a.Attempt2NotDrinking < 0 || a.UdderMilkNotDrinking < 0 || a.ORSNotDrinking < 0 {
		return fmt.Errorf("milk feeding counts cannot be negative")
	}
	if a.Attempt1NotDrinking > a.TotalKidsFed {
		return fmt.Errorf("attempt 1 refusals cannot exceed total kids fed")
	}
	if a.Attempt2NotDrinking > a.Attempt1NotDrinking {
		return fmt.Errorf("attempt 2 refusals cannot exceed attempt 1 refusals")
	}
	if a.UdderMilkNotDrinking > a.Attempt2NotDrinking {
		return fmt.Errorf("udder-milk refusals cannot exceed attempt 2 refusals")
	}
	if a.ORSNotDrinking > a.UdderMilkNotDrinking {
		return fmt.Errorf("ORS refusals cannot exceed udder-milk refusals")
	}

	wanted := make(map[string]struct{}, len(watchlist))
	for _, kid := range watchlist {
		wanted[strings.TrimSpace(kid.GoatID)] = struct{}{}
	}
	seenAnswers := make(map[string]struct{}, len(a.WatchlistAnswers))
	watchlistRefusals := 0
	for _, answer := range a.WatchlistAnswers {
		id := strings.TrimSpace(answer.GoatID)
		if _, ok := wanted[id]; !ok {
			return fmt.Errorf("goat %q is not on this shed's watchlist", id)
		}
		if _, duplicate := seenAnswers[id]; duplicate {
			return fmt.Errorf("duplicate watchlist answer for goat %q", id)
		}
		seenAnswers[id] = struct{}{}
		if !answer.DrankMilk {
			watchlistRefusals++
		}
	}
	if len(seenAnswers) != len(wanted) {
		return fmt.Errorf("every watchlist kid requires an answer")
	}

	newRefusalCount := a.Attempt2NotDrinking - watchlistRefusals
	if newRefusalCount < 0 {
		newRefusalCount = 0
	}
	if len(a.NewRefusals) != newRefusalCount {
		return fmt.Errorf("%d new refusal goat IDs are required", newRefusalCount)
	}
	seenNew := make(map[string]struct{}, len(a.NewRefusals))
	for _, refusal := range a.NewRefusals {
		id := strings.TrimSpace(refusal.GoatID)
		if id == "" {
			return fmt.Errorf("new refusal goat ID is required")
		}
		if _, alreadyTracked := wanted[id]; alreadyTracked {
			return fmt.Errorf("goat %q is already on the watchlist", id)
		}
		if _, duplicate := seenNew[id]; duplicate {
			return fmt.Errorf("duplicate new refusal goat %q", id)
		}
		seenNew[id] = struct{}{}
	}
	return nil
}

func ApplyMilkFeedingWatchlistAnswers(current []MilkFeedingWatchlistKid, answers []MilkFeedingWatchlistAnswer, newRefusals []MilkFeedingNewRefusal, date string, sessionNo int) []MilkFeedingWatchlistKid {
	answerByGoat := make(map[string]bool, len(answers))
	for _, answer := range answers {
		answerByGoat[strings.TrimSpace(answer.GoatID)] = answer.DrankMilk
	}
	next := make([]MilkFeedingWatchlistKid, 0, len(current)+len(newRefusals))
	for _, kid := range current {
		copy := kid
		if drank, ok := answerByGoat[strings.TrimSpace(kid.GoatID)]; ok {
			if drank {
				copy.ConsecutiveYes++
			} else {
				copy.ConsecutiveYes = 0
			}
		}
		if copy.ConsecutiveYes < 2 {
			next = append(next, copy)
		}
	}
	for _, refusal := range newRefusals {
		next = append(next, MilkFeedingWatchlistKid{GoatID: strings.TrimSpace(refusal.GoatID), Remarks: strings.TrimSpace(refusal.Remarks), AddedDate: date, AddedSession: sessionNo})
	}
	return next
}

type MilkFeedingProofs struct {
	CleanBottlesProofRef     string `json:"clean_bottles_proof_ref"`
	MixingAndFillingProofRef string `json:"mixing_and_filling_proof_ref"`
}

func (p MilkFeedingProofs) OrderedStepProofs() []MilkPreparationStepProof {
	return []MilkPreparationStepProof{
		{StepCode: MilkFeedingStepCleanBottles, ProofRef: strings.TrimSpace(p.CleanBottlesProofRef)},
		{StepCode: MilkFeedingStepMixingAndFilling, ProofRef: strings.TrimSpace(p.MixingAndFillingProofRef)},
	}
}

func (p MilkFeedingProofs) Validate() error {
	steps := p.OrderedStepProofs()
	if steps[0].ProofRef == "" || steps[1].ProofRef == "" {
		return fmt.Errorf("clean-bottles and mixing/filling videos are required")
	}
	if steps[0].ProofRef == steps[1].ProofRef {
		return fmt.Errorf("one video cannot prove both milk feeding steps")
	}
	return nil
}

type MilkFeedingTask struct {
	TaskID             string                    `json:"task_id"`
	ParkID             string                    `json:"park_id"`
	ParkLabel          string                    `json:"park_label"`
	FeedingDate        string                    `json:"feeding_date"`
	SessionNo          int                       `json:"session_no"`
	DueTime            string                    `json:"due_time"`
	Available          bool                      `json:"available"`
	AvailableAt        time.Time                 `json:"available_at"`
	BlockedReason      string                    `json:"blocked_reason,omitempty"`
	HeadCount          int                       `json:"head_count"`
	VerificationStatus string                    `json:"verification_status"`
	CompletionID       string                    `json:"completion_id,omitempty"`
	AttemptNo          int32                     `json:"attempt_no,omitempty"`
	ReworkReason       string                    `json:"rework_reason,omitempty"`
	Watchlist          []MilkFeedingWatchlistKid `json:"watchlist"`
}

func (t MilkFeedingTask) GrainKey() string {
	return fmt.Sprintf("%s:%s:%d", strings.TrimSpace(t.ParkID), t.FeedingDate, t.SessionNo)
}

type MilkFeedingPage struct {
	FeedingDate string            `json:"feeding_date"`
	GeneratedAt time.Time         `json:"generated_at"`
	Items       []MilkFeedingTask `json:"items"`
	Limit       int32             `json:"limit"`
	Offset      int32             `json:"offset"`
	HasMore     bool              `json:"has_more"`
}

type MilkFeedingQuery struct {
	TenantID    string
	ParkID      *string
	FeedingDate time.Time
	SessionNo   *int
	Limit       int32
	Offset      int32
}

type MilkFeedingSubmission struct {
	TenantID       string
	TaskID         string
	ParkID         string
	FeedingDate    time.Time
	SessionNo      int
	Answers        MilkFeedingAnswers
	Proofs         MilkFeedingProofs
	SubmittedBy    string
	SubmittedAt    time.Time
	IdempotencyKey string
	TraceID        string
}

type MilkFeedingSubmissionResult struct {
	CompletionID string `json:"completion_id"`
	Status       string `json:"status"`
	AttemptNo    int32  `json:"attempt_no"`
	RowVersion   int32  `json:"row_version"`
	NeedsEnqueue bool   `json:"-"`
}

type MilkFeedingMaterializeRequest struct {
	TenantID    string
	FeedingDate time.Time
}
type MilkFeedingMaterializeResult struct {
	FeedingDate string
	Inserted    int64
}

type MilkFeedingVerdictCommand struct {
	TenantID     string
	CompletionID string
	VerifiedBy   string
	Reason       string
	TraceID      string
	OccurredAt   time.Time
}
