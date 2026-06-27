package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/protocol/domain"
)

const testTenantID = "00000000-0000-4000-8000-000000000001"

func TestPublishVersionWritesDurableOutboxEvent(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	protocolID, err := repo.CreateDefinition(ctx, domain.NewDefinition{
		TenantID: testTenantID,
		Code:     "vaccination.publish.outbox",
		Name:     "Vaccination Publish Outbox",
		Category: "vaccination",
		Status:   "draft",
	})
	if err != nil {
		t.Fatalf("create definition: %v", err)
	}
	versionID, err := repo.CreateVersion(ctx, domain.NewVersion{
		TenantID:      testTenantID,
		ProtocolID:    protocolID,
		ScopeType:     "tenant",
		Version:       1,
		Status:        "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{}`),
		ProofPolicy:   []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create version: %v", err)
	}
	if err := repo.PublishVersion(ctx, testTenantID, versionID, nil); err != nil {
		t.Fatalf("publish version: %v", err)
	}

	var status, eventType, aggregateType, aggregateID, idempotencyKey string
	var payload []byte
	if err := pool.QueryRow(ctx, `
SELECT pv.status, om.event_type, om.aggregate_type, om.aggregate_id::text, om.idempotency_key, om.payload
FROM protocol_versions pv
JOIN outbox_messages om
  ON om.tenant_id = pv.tenant_id
 AND om.aggregate_id = pv.protocol_version_id
WHERE pv.tenant_id = $1
  AND pv.protocol_version_id = $2::uuid`, testTenantID, versionID).Scan(
		&status, &eventType, &aggregateType, &aggregateID, &idempotencyKey, &payload,
	); err != nil {
		t.Fatalf("read publish outbox: %v", err)
	}
	if status != "published" || eventType != "protocol.version.published" || aggregateType != "protocol_version" || aggregateID != versionID {
		t.Fatalf("status/event = %s/%s/%s/%s, want published protocol.version.published protocol_version %s", status, eventType, aggregateType, aggregateID, versionID)
	}
	if idempotencyKey != "protocol:version:published:"+versionID {
		t.Fatalf("idempotency key = %q", idempotencyKey)
	}
	var envelope struct {
		EventType       string `json:"event_type"`
		AggregateType   string `json:"aggregate_type"`
		AggregateID     string `json:"aggregate_id"`
		SubjectType     string `json:"subject_type"`
		VisibilityScope struct {
			TenantID string `json:"tenant_id"`
		} `json:"visibility_scope"`
		Payload struct {
			ProtocolVersionID string `json:"protocol_version_id"`
			Category          string `json:"category"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("decode outbox envelope: %v", err)
	}
	if envelope.EventType != "protocol.version.published" ||
		envelope.AggregateType != "protocol_version" ||
		envelope.AggregateID != versionID ||
		envelope.SubjectType != "protocol_version" ||
		envelope.VisibilityScope.TenantID != testTenantID ||
		envelope.Payload.ProtocolVersionID != versionID ||
		envelope.Payload.Category != "vaccination" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}
