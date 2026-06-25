package domain

import (
	"encoding/base64"
	"encoding/json"
	"time"
)

type Query struct {
	TenantID      string
	ActorType     *string
	ActorID       *string
	Action        *string
	ResourceType  *string
	ResourceID    *string
	ScopeType     *string
	ScopeID       *string
	Domain        *string
	Module        *string
	Category      *string
	Result        *string
	Status        *string
	From          *time.Time
	To            *time.Time
	AnomaliesOnly bool
	Limit         int
	Cursor        *Cursor
}

type Cursor struct {
	RecordedAt time.Time `json:"recorded_at"`
	AuditID    string    `json:"audit_id"`
}

type AuditRow struct {
	AuditID      string         `json:"audit_id"`
	RecordedAt   time.Time      `json:"recorded_at"`
	ActorType    string         `json:"actor_type"`
	ActorID      *string        `json:"actor_id,omitempty"`
	Action       string         `json:"action"`
	ResourceType string         `json:"resource_type"`
	ResourceID   *string        `json:"resource_id,omitempty"`
	ScopeType    *string        `json:"scope_type,omitempty"`
	ScopeID      *string        `json:"scope_id,omitempty"`
	Anomaly      bool           `json:"anomaly"`
	Metadata     map[string]any `json:"metadata"`
	TraceID      *string        `json:"trace_id,omitempty"`
}

type ListResponse struct {
	Items      []AuditRow `json:"items"`
	NextCursor *string    `json:"next_cursor,omitempty"`
	TraceID    string     `json:"trace_id"`
}

type SummaryResponse struct {
	Actions              int64     `json:"actions"`
	AwaitingVerification int64     `json:"awaiting_verification"`
	ProofEvents          int64     `json:"proof_events"`
	ProofCoveragePercent int64     `json:"proof_coverage_percent"`
	Rejected             int64     `json:"rejected"`
	Rework               int64     `json:"rework"`
	Anomalies            int64     `json:"anomalies"`
	From                 time.Time `json:"from"`
	To                   time.Time `json:"to"`
	AsOf                 time.Time `json:"as_of"`
	TraceID              string    `json:"trace_id"`
}

func EncodeCursor(c Cursor) (string, error) {
	payload, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func DecodeCursor(raw string) (Cursor, error) {
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return Cursor{}, err
	}
	var c Cursor
	if err := json.Unmarshal(payload, &c); err != nil {
		return Cursor{}, err
	}
	return c, nil
}
