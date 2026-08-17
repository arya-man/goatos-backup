package postgres

import (
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
)

func TestValidateStoredRowSeqsRejectsDistinctRowsSharingSequence(t *testing.T) {
	t.Parallel()
	cells := domain.FlattenRows([]domain.DirectionRow{
		{ShedID: "mandela-1", PartitionLabel: "Part 6", ShedTag: "Non-Pregnant", Breed: "Beetal", RationGroup: "Beetal/Sirohi", SessionNo: 1, Workflow: domain.WorkflowNormal, SessionTotalKg: "4.400",
			Items: []domain.ItemQuantity{{FeedItem: "Concentrate", Status: domain.QuantityResolved, QuantityKg: strptr("4.400")}}},
		{ShedID: "mandela-1", PartitionLabel: "Part 7", ShedTag: "Non-Pregnant", Breed: "Beetal", RationGroup: "Beetal/Sirohi", SessionNo: 1, Workflow: domain.WorkflowNormal, SessionTotalKg: "15.700",
			Items: []domain.ItemQuantity{{FeedItem: "Concentrate", Status: domain.QuantityResolved, QuantityKg: strptr("15.700")}}},
	})
	for i := range cells {
		cells[i].RowSeq = 58
	}

	err := validateStoredRowSeqs(cells)
	if err == nil || !strings.Contains(err.Error(), "duplicate row_seq 58") {
		t.Fatalf("validateStoredRowSeqs err = %v, want duplicate row_seq error", err)
	}
}

func TestValidateStoredRowSeqsAllowsItemsOfSameRowSharingSequence(t *testing.T) {
	t.Parallel()
	cells := domain.FlattenRows([]domain.DirectionRow{{
		ShedID: "mandela-1", PartitionLabel: "Part 7", ShedTag: "Non-Pregnant", Breed: "Beetal", RationGroup: "Beetal/Sirohi", SessionNo: 1, Workflow: domain.WorkflowNormal, SessionTotalKg: "15.700",
		Items: []domain.ItemQuantity{
			{FeedItem: "Concentrate", Status: domain.QuantityResolved, QuantityKg: strptr("3.800")},
			{FeedItem: "Dry Masoor Bhusa", Status: domain.QuantityResolved, QuantityKg: strptr("11.400")},
		},
	}})

	if err := validateStoredRowSeqs(cells); err != nil {
		t.Fatalf("validateStoredRowSeqs returned %v for cells from one row", err)
	}
}

func strptr(s string) *string { return &s }
