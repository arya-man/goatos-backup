package app

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/inventory/domain"
	"github.com/vgoats/goatos/backend/internal/inventory/ports"
)

func TestConsumeForBatchFailsWhenReservationIsShort(t *testing.T) {
	repo := &consumeRepo{
		lot: domain.StockLot{
			StockID:          "lot-1",
			ItemID:           "item-1",
			LocationID:       "loc-1",
			QuantityReserved: "1",
			QuantityUnit:     "dose",
		},
	}
	svc := NewService(repo)

	err := svc.ConsumeForBatch(context.Background(), "tenant-1", "batch-1", "lot-1", "consume-key", 2)
	if !errors.Is(err, ErrReservationMismatch) {
		t.Fatalf("ConsumeForBatch err = %v, want ErrReservationMismatch", err)
	}
	if len(repo.movements) != 0 {
		t.Fatalf("consume should not record a capped movement, got %#v", repo.movements)
	}
}

func TestReleaseForBatchCapsNoShowRemainder(t *testing.T) {
	repo := &consumeRepo{
		lot: domain.StockLot{
			StockID:          "lot-1",
			ItemID:           "item-1",
			LocationID:       "loc-1",
			QuantityReserved: "1",
			QuantityUnit:     "dose",
		},
	}
	svc := NewService(repo)

	if err := svc.ReleaseForBatch(context.Background(), "tenant-1", "batch-1", "lot-1", "release-key", 2); err != nil {
		t.Fatalf("ReleaseForBatch: %v", err)
	}
	if len(repo.movements) != 1 {
		t.Fatalf("release movements=%d, want 1", len(repo.movements))
	}
	if repo.movements[0].Quantity != "1" || repo.reservedDelta != "-1" || repo.inDelta != "0" {
		t.Fatalf("release movement=%#v inDelta=%s reservedDelta=%s", repo.movements[0], repo.inDelta, repo.reservedDelta)
	}
}

func TestConsumeForBatchRecordsExactReservedDose(t *testing.T) {
	repo := &consumeRepo{
		lot: domain.StockLot{
			StockID:          "lot-1",
			ItemID:           "item-1",
			LocationID:       "loc-1",
			QuantityReserved: "2",
			QuantityUnit:     "dose",
		},
	}
	svc := NewService(repo)

	if err := svc.ConsumeForBatch(context.Background(), "tenant-1", "batch-1", "lot-1", "consume-key", 2); err != nil {
		t.Fatalf("ConsumeForBatch: %v", err)
	}
	if len(repo.movements) != 1 {
		t.Fatalf("consume movements=%d, want 1", len(repo.movements))
	}
	if repo.movements[0].Quantity != "2" || repo.reservedDelta != "-2" || repo.inDelta != "-2" {
		t.Fatalf("consume movement=%#v inDelta=%s reservedDelta=%s", repo.movements[0], repo.inDelta, repo.reservedDelta)
	}
}

func TestConsumeForBatchReplaySkipsReservationCheck(t *testing.T) {
	repo := &consumeRepo{
		lot: domain.StockLot{
			StockID:          "lot-1",
			ItemID:           "item-1",
			LocationID:       "loc-1",
			QuantityReserved: "0",
			QuantityUnit:     "dose",
		},
		movementExists: true,
	}
	svc := NewService(repo)

	if err := svc.ConsumeForBatch(context.Background(), "tenant-1", "batch-1", "lot-1", "consume-key", 1); err != nil {
		t.Fatalf("ConsumeForBatch replay: %v", err)
	}
	if len(repo.movements) != 0 {
		t.Fatalf("replay should not record another movement, got %#v", repo.movements)
	}
}

type consumeRepo struct {
	lot            domain.StockLot
	movements      []domain.Movement
	movementExists bool
	inDelta        string
	reservedDelta  string
}

var _ ports.Repository = (*consumeRepo)(nil)

func (r *consumeRepo) Ping(context.Context) error { return nil }

func (r *consumeRepo) CreateItem(context.Context, domain.NewItem) (string, error) {
	return "", nil
}

func (r *consumeRepo) GetItem(context.Context, string, string) (domain.Item, error) {
	return domain.Item{}, nil
}

func (r *consumeRepo) CreateStockLot(context.Context, domain.NewStockLot) (string, error) {
	return "", nil
}

func (r *consumeRepo) GetStockLot(context.Context, string, string) (domain.StockLot, error) {
	return r.lot, nil
}

func (r *consumeRepo) PickFEFOLot(context.Context, string, string, string) (domain.FEFOPick, error) {
	return domain.FEFOPick{}, nil
}

func (r *consumeRepo) ResolveStockLocation(context.Context, string, string, string) (string, error) {
	return "", nil
}

func (r *consumeRepo) ReserveForBatch(context.Context, string, string, string, string, int64) error {
	return nil
}

func (r *consumeRepo) ReleaseBatchReconcileRemainders(context.Context, string, int) (domain.BatchStockReconcileSummary, error) {
	return domain.BatchStockReconcileSummary{}, nil
}

func (r *consumeRepo) RecordMovement(context.Context, domain.Movement) (string, bool, error) {
	return "", false, nil
}

func (r *consumeRepo) MovementExists(context.Context, string, string) (bool, error) {
	return r.movementExists, nil
}

func (r *consumeRepo) RecordMovementAndAdjustBalances(_ context.Context, m domain.Movement, inDelta, reservedDelta string) (string, bool, error) {
	r.movements = append(r.movements, m)
	r.inDelta = inDelta
	r.reservedDelta = reservedDelta
	return "movement-1", true, nil
}

func (r *consumeRepo) AdjustBalances(context.Context, string, string, string, string) error {
	return nil
}
