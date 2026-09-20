package postgres

import (
	"context"
	"errors"
	"fmt"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
	"testing"
	"time"
)

// Frozen pre-provenance request identity, used to simulate an already committed
// phone edit whose response was lost before the server upgrade.
func legacyVendorUpdateFingerprint(vendorID string, w domain.VendorWrite, rowVersion int64, preserveFinance bool) string {
	preserve := "false"
	if preserveFinance {
		preserve = "true"
	}
	return requestFingerprint(
		vendorID, fmt.Sprintf("%d", rowVersion), preserve,
		w.RecordType, w.BusinessName, w.ContactPersonName, w.PhoneNumber,
		w.Breed, w.Feed, w.Status, fpInt(w.FilteredStock), fpString(w.PricePerGoat),
		w.ReadyToFiltered, fpInt(w.ETAAfterOrderDays), w.Details, w.State, w.City,
		w.BankName, w.AccountNo, w.IFSCCode, w.UPIID, w.PANNumber, w.Comments,
		fpString(w.CapacityQuantity), w.CapacityUnit, w.SupplyFrequency, w.VoiceNoteProofRef,
		fpString(w.AverageAnimalWeightKg),
		string(vendorAnswersJSON(w.SOPAnswers)), fmt.Sprintf("%d", w.QuestionnaireVersion),
	)
}

func TestVendorPreUpgradeEditReplaysAfterProvenance(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	for _, code := range []string{"", domain.SOPCodeVendor} {
		t.Run("code="+code, func(t *testing.T) {
			write := domain.VendorWrite{RecordType: "Goat Stockist", BusinessName: "Replay " + code, Status: "Active", State: "MP", QuestionnaireSOPCode: code}
			if code != "" {
				write.SOPAnswers = map[string]string{"transport": "yes"}
				write.QuestionnaireVersion = 2
			}
			created, err := repo.CreateVendor(ctx, testTenant, write, "")
			if err != nil {
				t.Fatal(err)
			}
			write.City = "Bhopal"
			key := "old-edit-" + code
			updated, err := repo.UpdateVendor(ctx, testTenant, created.VendorID, write, created.RowVersion, "", key, false)
			if err != nil {
				t.Fatal(err)
			}
			_, err = pool.Exec(ctx, `UPDATE idempotency_keys SET request_hash=$1 WHERE idempotency_key=$2`, legacyVendorUpdateFingerprint(created.VendorID, write.Normalize(), created.RowVersion, false), idemScopedKey(testTenant, idemScopeVendorUpdate, key))
			if err != nil {
				t.Fatal(err)
			}
			replay, err := repo.UpdateVendor(ctx, testTenant, created.VendorID, write, created.RowVersion, "", key, false)
			if err != nil {
				t.Fatalf("pre-upgrade committed edit must replay: %v", err)
			}
			if replay.RowVersion != updated.RowVersion {
				t.Fatalf("replay repeated the edit: %d != %d", replay.RowVersion, updated.RowVersion)
			}
			if code != "" {
				write.QuestionnaireSOPCode = domain.SOPCodeProcurementVendor
				_, err = repo.UpdateVendor(ctx, testTenant, created.VendorID, write, created.RowVersion, "", key, false)
				if !errors.Is(err, ports.ErrIdempotencyConflict) {
					t.Fatalf("different form must conflict, got %v", err)
				}
			}
		})
	}
}
