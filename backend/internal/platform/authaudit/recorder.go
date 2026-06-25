package authaudit

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/audit"
)

type Event struct {
	TenantID     string
	ActorID      string
	ActorType    string
	Action       string
	ResourceType string
	ScopeType    string
	ScopeID      string
	Metadata     map[string]any
	TraceID      string
}

type PostgresRecorder struct {
	recorder *audit.PostgresRecorder
}

func NewPostgresRecorder(pool *pgxpool.Pool, timeout time.Duration) *PostgresRecorder {
	return &PostgresRecorder{recorder: audit.NewPostgresRecorder(pool, timeout)}
}

func (r *PostgresRecorder) Record(ctx context.Context, event Event) error {
	metadata := metadataWithAuthDefaults(event.Metadata, event.Action)
	return r.recorder.Record(ctx, audit.Event{
		TenantID:     event.TenantID,
		ActorID:      event.ActorID,
		ActorType:    event.ActorType,
		Action:       event.Action,
		ResourceType: event.ResourceType,
		ScopeType:    event.ScopeType,
		ScopeID:      event.ScopeID,
		Metadata:     metadata,
		TraceID:      event.TraceID,
	})
}

func metadataWithAuthDefaults(input map[string]any, action string) map[string]any {
	metadata := make(map[string]any, len(input)+5)
	for key, value := range input {
		metadata[key] = value
	}
	setDefault(metadata, "domain", "admin")
	setDefault(metadata, "module", "auth_session")
	setDefault(metadata, "category", "auth")
	result := "success"
	if action == ActionFailedSignIn {
		result = "failed"
	}
	setDefault(metadata, "result", result)
	setDefault(metadata, "status", result)
	return metadata
}

func setDefault(metadata map[string]any, key string, value any) {
	if _, ok := metadata[key]; !ok {
		metadata[key] = value
	}
}
