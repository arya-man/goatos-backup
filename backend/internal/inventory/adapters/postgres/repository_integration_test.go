package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/inventory/domain"
	"github.com/vgoats/goatos/backend/internal/inventory/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const (
	inventoryTestTenant = "00000000-0000-4000-8000-000000000001"
	inventoryTestPark   = "00000000-0000-4000-8000-000000003001"
)

func TestMovementExistsByIdempotencyKey(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	itemID, err := repo.CreateItem(ctx, domain.NewItem{
		TenantID: inventoryTestTenant,
		ItemCode: "movement-exists-vaccine",
		Name:     "Movement Exists Vaccine",
		Category: "vaccine",
		BaseUnit: "dose",
		Status:   "active",
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}
	lotID, err := repo.CreateStockLot(ctx, domain.NewStockLot{
		TenantID:         inventoryTestTenant,
		ItemID:           itemID,
		LocationID:       inventoryTestPark,
		LotCode:          "move-exists-lot",
		QuantityInStock:  "5",
		QuantityReserved: "0",
		QuantityUnit:     "dose",
		Status:           "active",
	})
	if err != nil {
		t.Fatalf("create lot: %v", err)
	}
	const key = "movement-exists-key"
	exists, err := repo.MovementExists(ctx, inventoryTestTenant, key)
	if err != nil {
		t.Fatalf("movement exists before insert: %v", err)
	}
	if exists {
		t.Fatal("movement should not exist before insert")
	}
	if _, applied, err := repo.RecordMovement(ctx, domain.Movement{
		TenantID:       inventoryTestTenant,
		LotID:          lotID,
		ItemID:         itemID,
		LocationID:     inventoryTestPark,
		MovementType:   "adjust",
		Quantity:       "1",
		QuantityUnit:   "dose",
		Reason:         "movement exists test",
		IdempotencyKey: key,
	}); err != nil {
		t.Fatalf("record movement: %v", err)
	} else if !applied {
		t.Fatal("record movement applied=false, want true")
	}
	exists, err = repo.MovementExists(ctx, inventoryTestTenant, key)
	if err != nil {
		t.Fatalf("movement exists after insert: %v", err)
	}
	if !exists {
		t.Fatal("movement should exist after insert")
	}
}

func TestRecordMovementRejectsSameKeyDifferentPayload(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	itemID, err := repo.CreateItem(ctx, domain.NewItem{
		TenantID: inventoryTestTenant,
		ItemCode: "movement-conflict-vaccine",
		Name:     "Movement Conflict Vaccine",
		Category: "vaccine",
		BaseUnit: "dose",
		Status:   "active",
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}
	lotID, err := repo.CreateStockLot(ctx, domain.NewStockLot{
		TenantID:         inventoryTestTenant,
		ItemID:           itemID,
		LocationID:       inventoryTestPark,
		LotCode:          "move-conflict-lot",
		QuantityInStock:  "5",
		QuantityReserved: "0",
		QuantityUnit:     "dose",
		Status:           "active",
	})
	if err != nil {
		t.Fatalf("create lot: %v", err)
	}
	movement := domain.Movement{
		TenantID:       inventoryTestTenant,
		LotID:          lotID,
		ItemID:         itemID,
		LocationID:     inventoryTestPark,
		MovementType:   "adjust",
		Quantity:       "1",
		QuantityUnit:   "dose",
		Reason:         "movement conflict test",
		IdempotencyKey: "movement-conflict-key",
	}
	if _, applied, err := repo.RecordMovement(ctx, movement); err != nil || !applied {
		t.Fatalf("record movement: applied=%v err=%v", applied, err)
	}
	if _, applied, err := repo.RecordMovement(ctx, movement); err != nil || applied {
		t.Fatalf("same movement replay: applied=%v err=%v", applied, err)
	}
	movement.Quantity = "2"
	if _, _, err := repo.RecordMovement(ctx, movement); !errors.Is(err, ports.ErrMovementIdempotencyConflict) {
		t.Fatalf("different movement replay err=%v, want movement idempotency conflict", err)
	}
}
