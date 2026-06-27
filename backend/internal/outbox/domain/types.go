package domain

import (
	"encoding/json"
	"time"
)

const (
	StatusPending    = "pending"
	StatusPublishing = "publishing"
	StatusPublished  = "published"
	StatusFailed     = "failed"
	StatusDeadLetter = "dead_letter"
)

type Message struct {
	OutboxID       string
	TenantID       string
	EventID        string
	EventType      string
	SchemaVersion  string
	AggregateType  string
	AggregateID    string
	Topic          string
	Headers        json.RawMessage
	Payload        json.RawMessage
	IdempotencyKey string
	TraceID        *string
	AttemptCount   int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type DeadLetterMessage struct {
	OutboxID      string
	TenantID      string
	EventID       string
	EventType     string
	AggregateType string
	AggregateID   string
	Topic         string
	Status        string
	AttemptCount  int
	LastError     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type RunResult struct {
	ReclaimedStaleCount int
	ClaimedCount        int
	PublishedCount      int
	RetryScheduledCount int
	FailedCount         int
	DeadLetterCount     int
}
