package postgres

import (
	"context"
	"errors"
	"fmt"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	procurementapp "github.com/vgoats/goatos/backend/internal/procurement/app"
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

// Legacy APKs resent a previous Other explanation even after the form no longer
// offered Other. The old server persisted that request identity before filtering
// was introduced. A lost response must remain safely replayable after upgrade.
type legacySidecarFormSource struct{}

func (legacySidecarFormSource) PublishedVendorForm(ctx context.Context, tenant, code string, catalog []domain.VendorCatalogEntry) (domain.VendorForm, error) {
	return legacySidecarFormSource{}.VendorFormVersion(ctx, tenant, code, 2, catalog)
}
func (legacySidecarFormSource) VendorFormVersion(context.Context, string, string, int, []domain.VendorCatalogEntry) (domain.VendorForm, error) {
	return domain.VendorForm{Version: 2, Pages: []domain.VendorFormPage{{Questions: []domain.VendorQuestion{{ID: "transport", Kind: domain.VendorQuestionChoice, Title: "Transport", Options: []domain.VendorQuestionOpt{{Value: "yes", Label: "Yes"}, {Value: "no", Label: "No"}}}, {ID: "city", Kind: domain.VendorQuestionText, Title: "Conditional city", OnlyIf: &domain.VendorQuestionOnlyIf{QuestionID: "transport", Value: "no"}}}}}}, nil
}
func TestVendorLegacyStaleSidecarCommittedReplay(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	w := domain.VendorWrite{RecordType: "Goat Stockist", BusinessName: "Sidecar replay", Status: "Active", State: "MP", SOPAnswers: map[string]string{"transport": "yes", "transport_other": "old text", "city": "Legacy hidden city"}, QuestionnaireVersion: 2}
	created, err := repo.CreateVendor(ctx, testTenant, w, "")
	if err != nil {
		t.Fatal(err)
	}
	w.City = "Bhopal"
	key := "old-sidecar-edit"
	legacy, extras := domain.ApplyVendorAnswers(w, w.SOPAnswers)
	legacy.SOPAnswers = extras
	updated, err := repo.UpdateVendor(ctx, testTenant, created.VendorID, legacy, created.RowVersion, "", key, false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `UPDATE idempotency_keys SET request_hash=$1 WHERE idempotency_key=$2`, legacyVendorUpdateFingerprint(created.VendorID, legacy.Normalize(), created.RowVersion, false), idemScopedKey(testTenant, idemScopeVendorUpdate, key))
	if err != nil {
		t.Fatal(err)
	}
	svc := procurementapp.NewVendorService(repo).WithVendorFormSource(legacySidecarFormSource{})
	replay, err := svc.UpdateVendor(ctx, testTenant, created.VendorID, w, created.RowVersion, "", key, true)
	if err != nil {
		t.Fatalf("old committed sidecar edit must replay: %v", err)
	}
	if replay.RowVersion != updated.RowVersion {
		t.Fatal("replay wrote a second update")
	}

	if replay.City == nil || *replay.City != "Legacy hidden city" {
		t.Fatal("replay did not return legacy typed mapping")
	}
	for _, mutation := range []string{"answer", "typed", "version", "code", "sidecar", "row_version"} {
		changed := w
		changed.SOPAnswers = map[string]string{}
		for k, v := range w.SOPAnswers {
			changed.SOPAnswers[k] = v
		}
		rowVersion := created.RowVersion
		switch mutation {
		case "answer":
			changed.SOPAnswers["transport"] = "no"
		case "typed":
			changed.BusinessName = "Another vendor"
		case "version":
			changed.QuestionnaireVersion = 3
		case "code":
			changed.QuestionnaireSOPCode = domain.SOPCodeProcurementVendor
		case "sidecar":
			changed.SOPAnswers["transport_other"] = "different old text"
		case "row_version":
			rowVersion++
		}
		if _, err := svc.UpdateVendor(ctx, testTenant, created.VendorID, changed, rowVersion, "", key, true); !errors.Is(err, ports.ErrIdempotencyConflict) {
			t.Fatalf("changed %s must conflict: %v", mutation, err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE idempotency_keys SET status='started' WHERE idempotency_key=$1`, idemScopedKey(testTenant, idemScopeVendorUpdate, key)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateVendor(ctx, testTenant, created.VendorID, w, created.RowVersion, "", key, true); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("incomplete legacy reservation must not use alternative: %v", err)
	}
	fresh, err := svc.UpdateVendor(ctx, testTenant, created.VendorID, w, updated.RowVersion, "", "new-filtered-edit", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := fresh.SOPAnswers["transport_other"]; exists {
		t.Fatal("new reservation persisted stale sidecar")
	}
	if fresh.City == nil || *fresh.City != "Bhopal" {
		t.Fatal("new reservation did not use projected typed mapping")
	}
	var hash string
	if err := pool.QueryRow(ctx, `SELECT request_hash FROM idempotency_keys WHERE idempotency_key=$1`, idemScopedKey(testTenant, idemScopeVendorUpdate, "new-filtered-edit")).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	canonical := w
	canonical.SOPAnswers = map[string]string{"transport": "yes"}
	if hash != vendorUpdateFingerprint(created.VendorID, canonical.Normalize(), updated.RowVersion, false) {
		t.Fatal("new reservation did not store canonical hash")
	}
	w.SOPAnswers["transport"] = "no"
	if _, err := svc.UpdateVendor(ctx, testTenant, created.VendorID, w, created.RowVersion, "", key, true); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("changed answer must conflict: %v", err)
	}
}
