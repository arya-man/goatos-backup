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
	StatusDiscarded  = "discarded"
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
	OutboxID       string          `json:"outbox_id"`
	TenantID       string          `json:"tenant_id"`
	EventID        string          `json:"event_id"`
	EventType      string          `json:"event_type"`
	SchemaVersion  string          `json:"schema_version"`
	AggregateType  string          `json:"aggregate_type"`
	AggregateID    string          `json:"aggregate_id"`
	Topic          string          `json:"topic"`
	Status         string          `json:"status"`
	AttemptCount   int             `json:"attempt_count"`
	ReplayCount    int             `json:"replay_count"`
	LastError      string          `json:"last_error"`
	IdempotencyKey string          `json:"idempotency_key"`
	TraceID        *string         `json:"trace_id,omitempty"`
	Headers        json.RawMessage `json:"headers"`
	Payload        json.RawMessage `json:"payload"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type RunResult struct {
	BatchesProcessed    int
	ReclaimedStaleCount int
	ClaimedCount        int
	PublishedCount      int
	RetryScheduledCount int
	FailedCount         int
	DeadLetterCount     int
}

type Health struct {
	Status          string     `json:"status"`
	PendingCount    int64      `json:"pending_count"`
	PublishingCount int64      `json:"publishing_count"`
	FailedCount     int64      `json:"failed_count"`
	DeadLetterCount int64      `json:"dead_letter_count"`
	OldestPendingAt *time.Time `json:"oldest_pending_at,omitempty"`
	OldestFailureAt *time.Time `json:"oldest_failure_at,omitempty"`
	LastPublishedAt *time.Time `json:"last_published_at,omitempty"`
}
