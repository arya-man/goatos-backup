// Package domain defines durable notification delivery state.
package domain

import (
	"encoding/json"
	"time"
)

const (
	StatusQueued     = "queued"
	StatusSending    = "sending"
	StatusSent       = "sent"
	StatusFailed     = "failed"
	StatusSuppressed = "suppressed"
	StatusRead       = "read"
)

type Request struct {
	NotificationRequestID string
	TenantID              string
	CalendarEventID       string
	TargetType            string
	TargetID              string
	NotificationType      string
	Channel               string
	RecipientRef          string
	Title                 string
	Body                  string
	Status                string
	TraceID               string
	Context               json.RawMessage
	DeliveryAttempts      int
	LeaseToken            string
	RequestedAt           time.Time
}

type DispatchResult struct {
	ReclaimedStaleCount int
	ClaimedCount        int
	SentCount           int
	FailedCount         int
}
