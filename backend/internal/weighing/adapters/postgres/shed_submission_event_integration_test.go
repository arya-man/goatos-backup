package postgres

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	outboxapp "github.com/vgoats/goatos/backend/internal/outbox/app"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

func TestCompletedWeighingShedEnqueuesSubmissionEventInSameTransaction(t *testing.T) {
	tests := []struct {
		name           string
		campaignShedID string
		record         func(context.Context, *Repository) error
	}{
		{
			name:           "individual",
			campaignShedID: repoAnimalScope,
			record: func(ctx context.Context, repo *Repository) error {
				_, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
					TenantID:         repoTenant,
					CampaignID:       repoCampaign,
					CampaignShedID:   repoAnimalScope,
					WeightKg:         12.4,
					ProofArtifactID:  repoExpectedShedProof,
					ActualLocationID: repoExpectedShed,
					IdempotencyKey:   "animal:submission-event",
					RecordedBy:       repoOperator,
				})
				return err
			},
		},
		{
			name:           "lump_sum",
			campaignShedID: repoShedScope,
			record: func(ctx context.Context, repo *Repository) error {
				_, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
					TenantID:         repoTenant,
					CampaignID:       repoCampaign,
					CampaignShedID:   repoShedScope,
					AverageWeightKg:  13.4,
					ProofArtifactIDs: []string{repoShedProof},
					IdempotencyKey:   "shed:submission-event",
					RecordedBy:       repoOperator,
				})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pgtest.SkipIfNoDocker(t)
			ctx := context.Background()
			pool := pgtest.StartPostgres(t, ctx)
			defer pool.Close()
			seedWeighingObservationFixture(t, ctx, pool)
			repo := NewRepository(pool, 5*time.Second)

			if err := tt.record(ctx, repo); err != nil {
				t.Fatalf("record completed shed submission: %v", err)
			}

			var count int
			var campaignShedID, tenantID string
			var payload []byte
			if err := pool.QueryRow(ctx, `
SELECT count(*)::int,
       COALESCE(max(payload->'payload'->>'campaign_shed_id'), ''),
       COALESCE(max(payload->'payload'->>'tenant_id'), ''),
       max(payload)::text
FROM outbox_messages
WHERE tenant_id=$1::uuid
  AND event_type=$2
  AND aggregate_id=$3::uuid
  AND payload->'payload'->>'campaign_shed_id'=$4`,
				repoTenant, eventWeighingShedSubmissionCompleted, repoCampaign, tt.campaignShedID).
				Scan(&count, &campaignShedID, &tenantID, &payload); err != nil {
				t.Fatalf("read shed completion outbox event: %v", err)
			}
			if count != 1 || campaignShedID != tt.campaignShedID || tenantID != repoTenant {
				t.Fatalf("completion outbox = count %d tenant %q scope %q, want one event for tenant %q scope %q",
					count, tenantID, campaignShedID, repoTenant, tt.campaignShedID)
			}
			validator, err := outboxapp.NewEnvelopeValidator(filepath.Join("..", "..", "..", "..", "..", "contracts", "jsonschema", "domain-event-envelope.schema.json"))
			if err != nil {
				t.Fatalf("load domain envelope validator: %v", err)
			}
			if err := validator.Validate(payload); err != nil {
				t.Fatalf("weighing completion outbox payload is not a valid domain-event envelope: %v\npayload: %s", err, payload)
			}
		})
	}
}
