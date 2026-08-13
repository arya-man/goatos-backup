package postgres

import (
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/verification/domain"
)

func TestVerificationEventPayloadsPreservePartitionLabel(t *testing.T) {
	partitionLabel := "Part 2"

	pending := verificationItemPendingPayload("item-1", domain.CreateItem{
		PartitionLabel: &partitionLabel,
		CapturedAt:     time.Unix(0, 0),
	})
	if got := pending["partition_label"]; got != partitionLabel {
		t.Fatalf("pending partition_label = %v, want %q", got, partitionLabel)
	}

	verdict := verificationVerdictPayload(domain.Item{PartitionLabel: &partitionLabel})
	if got := verdict["partition_label"]; got != partitionLabel {
		t.Fatalf("verdict partition_label = %v, want %q", got, partitionLabel)
	}
}

func TestVerificationEventPayloadsUseEmptyPartitionForWholeShed(t *testing.T) {
	pending := verificationItemPendingPayload("item-1", domain.CreateItem{CapturedAt: time.Unix(0, 0)})
	if got := pending["partition_label"]; got != "" {
		t.Fatalf("pending partition_label = %v, want empty for whole shed", got)
	}

	verdict := verificationVerdictPayload(domain.Item{})
	if got := verdict["partition_label"]; got != "" {
		t.Fatalf("verdict partition_label = %v, want empty for whole shed", got)
	}
}
