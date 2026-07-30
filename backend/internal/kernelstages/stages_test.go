package kernelstages

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/worker"
)

// Compile-time proof that every stage implements the supervisor's StageRunner
// contract, so the kernel worker can register them.
var (
	_ worker.StageRunner = (*OutboxRelayStage)(nil)
	_ worker.StageRunner = (*DomainConsumerStage)(nil)
	_ worker.StageRunner = (*VaccinationGenerationStage)(nil)
	_ worker.StageRunner = (*ObligationSweeperStage)(nil)
	_ worker.StageRunner = (*NotificationDispatcherStage)(nil)
	_ worker.StageRunner = (*InventoryBatchReconcilerStage)(nil)
	_ worker.StageRunner = (*SopSubmissionFanoutRetryStage)(nil)
	_ worker.StageRunner = (*SopReviewFanoutRetryStage)(nil)
	_ worker.StageRunner = (*ProcessedEventSweeperStage)(nil)
	_ worker.StageRunner = (*IdempotencyKeySweeperStage)(nil)
	_ worker.StageRunner = (*FeedTransportStage)(nil)
	_ worker.StageRunner = (*FeedDirectionLifecycleStage)(nil)
	_ worker.StageRunner = (*MilkFeedingStage)(nil)
)

// TestStageNamesAreStableAndUnique guards the advisory-lock identity of each
// stage: the supervisor identifies and locks stages by Name(), so a duplicate or
// empty name would collide two stages onto one advisory lock.
func TestStageNamesAreStableAndUnique(t *testing.T) {
	deps := Deps{}
	names := []string{
		NewInventoryBatchReconcilerStage(deps, "tenant").Name(),
		NewProcessedEventSweeperStage(deps, "tenant").Name(),
		NewIdempotencyKeySweeperStage(deps, "tenant").Name(),
		(&ObligationSweeperStage{}).Name(),
		(&OutboxRelayStage{}).Name(),
		(&NotificationDispatcherStage{}).Name(),
		(&VaccinationGenerationStage{}).Name(),
		(&SopSubmissionFanoutRetryStage{}).Name(),
		(&SopReviewFanoutRetryStage{}).Name(),
		(&DomainConsumerStage{}).Name(),
		(&FeedTransportStage{}).Name(),
		(&FeedDirectionLifecycleStage{}).Name(),
		(&MilkFeedingStage{}).Name(),
	}
	seen := map[string]bool{}
	for _, name := range names {
		if name == "" {
			t.Fatal("stage name must not be empty")
		}
		if seen[name] {
			t.Fatalf("duplicate stage name %q would collide on the supervisor advisory lock", name)
		}
		seen[name] = true
	}
}

// TestDomainConsumerConfigFromEnvDisabledWhenUnset confirms the kernel worker can
// boot without Pub/Sub (local/dev): neither project nor subscription set means
// the continuous consumer is simply disabled, not a hard error.
func TestDomainConsumerConfigFromEnvDisabledWhenUnset(t *testing.T) {
	t.Setenv("GOATOS_PUBSUB_PROJECT_ID", "")
	t.Setenv("GOOGLE_CLOUD_PROJECT", "")
	t.Setenv("GOATOS_DOMAIN_EVENTS_SUBSCRIPTION_ID", "")
	t.Setenv("GOATOS_PUBSUB_SUBSCRIPTION_ID", "")

	_, enabled, err := DomainConsumerConfigFromEnv()
	if err != nil {
		t.Fatalf("expected no error when Pub/Sub is unconfigured, got: %v", err)
	}
	if enabled {
		t.Fatal("expected the domain consumer to be disabled when Pub/Sub is unconfigured")
	}
}

// TestDomainConsumerConfigFromEnvHalfConfiguredFailsFast confirms a partial
// Pub/Sub config (project without subscription) is rejected rather than silently
// half-running.
func TestDomainConsumerConfigFromEnvHalfConfiguredFailsFast(t *testing.T) {
	t.Setenv("GOATOS_PUBSUB_PROJECT_ID", "goatos-dev")
	t.Setenv("GOOGLE_CLOUD_PROJECT", "")
	t.Setenv("GOATOS_DOMAIN_EVENTS_SUBSCRIPTION_ID", "")
	t.Setenv("GOATOS_PUBSUB_SUBSCRIPTION_ID", "")

	_, enabled, err := DomainConsumerConfigFromEnv()
	if !enabled {
		t.Fatal("expected enabled=true when a Pub/Sub value is present")
	}
	if err == nil {
		t.Fatal("expected an error when only the project is configured")
	}
}
