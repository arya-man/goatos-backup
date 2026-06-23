// Package domain holds the vaccination module domain types (dose-administered records).
package domain

import "time"

// NewCompletion is the input to record one administered dose against an obligation.
type NewCompletion struct {
	TenantID                 string
	ObligationID             string
	BatchID                  *string
	GoatID                   string
	SopSubmissionItemID      *string
	VaccineInventoryLotID    *string
	Doses                    *int32
	DoseMlGiven              string
	RouteSite                string
	AdverseReaction          bool
	AdverseReactionProblemID *string
	ColdChainVerified        bool
	AdministeredAt           time.Time
	Status                   string // defaults to "recorded"
	WithdrawalUntilDate      *time.Time
	RecordedBy               *string
	IdempotencyKey           string
}

// CompletionHistoryItem is one row of a goat's vaccination history.
type CompletionHistoryItem struct {
	CompletionID        string
	ObligationID        string
	BatchID             string
	AdministeredAt      time.Time
	Status              string
	Doses               int32
	RouteSite           string
	AdverseReaction     bool
	WithdrawalUntilDate *time.Time
}

// LastAccepted is the most recent accepted administration for a goat (next-due / SM-7 basis).
type LastAccepted struct {
	CompletionID   string
	ObligationID   string
	AdministeredAt time.Time
}
