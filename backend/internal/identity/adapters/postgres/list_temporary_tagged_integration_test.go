package postgres

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestListTemporaryTaggedGoatsWithDockerPostgres proves the operator "Awaiting RFID" list against
// real Postgres: only goats with an ACTIVE temporary tag appear (a permanent-RFID goat is
// excluded), each row carries the temp tag value + a resolved location + row_version, the page is
// keyset-ordered by display_id, and next_cursor pages forward correctly.
func TestListTemporaryTaggedGoatsWithDockerPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)

	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()
	seedAdminCreateLocations(t, pool)

	// Two goats born with ONLY a temporary tag: they should both appear on the list.
	tempA := adminGoatCreateCommand(t, "idem-list-temp-a", "unused-a1", "unused-a2")
	tempA.Identifiers = []ports.AdminGoatCreateIdentifier{{
		IdentifierType: "temporary_tag", IdentifierValue: "TEMP-LIST-A", NormalizedValue: "TEMP-LIST-A", ScopeKey: "global", IsPrimary: true,
	}}
	createdA, err := repo.CreateAdminGoat(ctx, tempA)
	if err != nil {
		t.Fatalf("create temp goat A: %v", err)
	}

	tempB := adminGoatCreateCommand(t, "idem-list-temp-b", "unused-b1", "unused-b2")
	tempB.Identifiers = []ports.AdminGoatCreateIdentifier{{
		IdentifierType: "temporary_tag", IdentifierValue: "TEMP-LIST-B", NormalizedValue: "TEMP-LIST-B", ScopeKey: "global", IsPrimary: true,
	}}
	createdB, err := repo.CreateAdminGoat(ctx, tempB)
	if err != nil {
		t.Fatalf("create temp goat B: %v", err)
	}

	// A permanent-RFID goat: it must NOT appear on the awaiting-RFID list.
	permanent := adminGoatCreateCommand(t, "idem-list-permanent", "RFID-LIST-PERM", "")
	createdPerm, err := repo.CreateAdminGoat(ctx, permanent)
	if err != nil {
		t.Fatalf("create permanent goat: %v", err)
	}

	tempIDs := map[string]bool{createdA.Goat.GoatID: true, createdB.Goat.GoatID: true}

	// Full page: exactly the two temp-tagged goats, permanent excluded.
	items, next, err := repo.ListTemporaryTaggedGoats(ctx, ports.ListTemporaryTaggedGoatsParams{TenantID: meshaTenant, Limit: 20})
	if err != nil {
		t.Fatalf("ListTemporaryTaggedGoats: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 temp-tagged goats, got %d: %+v", len(items), items)
	}
	if next != nil {
		t.Fatalf("expected no next_cursor on a full page, got %q", *next)
	}
	byValue := map[string]string{}
	for _, it := range items {
		if !tempIDs[it.GoatID] {
			t.Fatalf("unexpected goat on awaiting-RFID list: %+v", it)
		}
		if it.GoatID == createdPerm.Goat.GoatID {
			t.Fatalf("permanent goat leaked onto awaiting-RFID list")
		}
		if it.RowVersion < 1 {
			t.Fatalf("row_version must be >=1, got %d", it.RowVersion)
		}
		if it.LocationDisplay == "" || it.LocationDisplay == "Unknown location" {
			t.Fatalf("expected a resolved location, got %q", it.LocationDisplay)
		}
		byValue[it.TemporaryIdentifier] = it.DisplayID
	}
	if _, ok := byValue["TEMP-LIST-A"]; !ok {
		t.Fatalf("TEMP-LIST-A missing from list: %+v", items)
	}
	if _, ok := byValue["TEMP-LIST-B"]; !ok {
		t.Fatalf("TEMP-LIST-B missing from list: %+v", items)
	}
	// Ordered by display_id ascending.
	if items[0].DisplayID >= items[1].DisplayID {
		t.Fatalf("expected ascending display_id order, got %q then %q", items[0].DisplayID, items[1].DisplayID)
	}

	// Keyset paging: page_size 1 yields the first row plus a cursor equal to its display_id.
	page1, next1, err := repo.ListTemporaryTaggedGoats(ctx, ports.ListTemporaryTaggedGoatsParams{TenantID: meshaTenant, Limit: 1})
	if err != nil {
		t.Fatalf("keyset page 1: %v", err)
	}
	if len(page1) != 1 || next1 == nil || *next1 != page1[0].DisplayID {
		t.Fatalf("expected 1 row + cursor=%q, got rows=%d cursor=%v", page1[0].DisplayID, len(page1), next1)
	}
	page2, _, err := repo.ListTemporaryTaggedGoats(ctx, ports.ListTemporaryTaggedGoatsParams{TenantID: meshaTenant, Limit: 1, Cursor: next1})
	if err != nil {
		t.Fatalf("keyset page 2: %v", err)
	}
	if len(page2) != 1 {
		t.Fatalf("expected 1 row on keyset page 2, got %d", len(page2))
	}
	if page2[0].DisplayID == page1[0].DisplayID {
		t.Fatalf("keyset page 2 repeated page 1 row %q", page1[0].DisplayID)
	}
}
