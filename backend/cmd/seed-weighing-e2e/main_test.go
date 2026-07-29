package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

func TestMain(m *testing.M) {
	pgtest.RunMain(m)
}

func TestDryRunValidatesRepositoryFixture(t *testing.T) {
	fixturePath := filepath.Join("..", "..", "..", "fixtures", "weighing-e2e-2026-07-29", "weighing-seed.json")
	var out bytes.Buffer
	if err := run([]string{"-dry-run", "-fixture", fixturePath}, &out); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"weighing E2E seed dry-run ok",
		"scopes=3",
		"animals=7",
		"proofs=6",
		"observations=4",
		"duplicate_scans=1",
		"shed_observations=1",
		"lump_sum_assignments=1",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("dry-run output missing %q: %s", want, got)
		}
	}
}

func TestShedCLumpSumAssignmentRemainsPendingForAmit(t *testing.T) {
	fixturePath := filepath.Join("..", "..", "..", "fixtures", "weighing-e2e-2026-07-29", "weighing-seed.json")
	fx, err := loadFixture(fixturePath)
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	if fx.Campaign.OperatorCode != "amit_operator" {
		t.Fatalf("operator=%q, want amit_operator", fx.Campaign.OperatorCode)
	}
	for _, scope := range fx.SelectedScopes {
		if scope.DisplayName != "Kid Shed C Lumpsum" {
			continue
		}
		if scope.WeighingCategory != "per_shed_partition" {
			t.Fatalf("Shed C category=%q, want per_shed_partition", scope.WeighingCategory)
		}
		if got := scopeStatus(scope, fx); got != "pending" {
			t.Fatalf("Shed C status=%q, want pending", got)
		}
		return
	}
	t.Fatal("Shed C lump-sum scope is missing")
}

func TestImportLeavesShedCLumpSumAvailableToAmit(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	fixturePath := filepath.Join("..", "..", "..", "fixtures", "weighing-e2e-2026-07-29", "weighing-seed.json")
	fx, err := loadFixture(fixturePath)
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	if err := importFixture(ctx, pool, fx); err != nil {
		t.Fatalf("import fixture: %v", err)
	}

	var category, status, operatorID string
	var observationCount int
	if err := pool.QueryRow(ctx, `
SELECT
  cs.weighing_category,
  cs.status,
  wc.operator_user_id::text,
  (
    SELECT count(*)
    FROM weighing_shed_observations wso
    WHERE wso.tenant_id=cs.tenant_id
      AND wso.campaign_shed_id=cs.campaign_shed_id
  )
FROM weighing_campaign_sheds cs
JOIN weighing_campaigns wc
  ON wc.tenant_id=cs.tenant_id
 AND wc.campaign_id=cs.campaign_id
WHERE cs.tenant_id=$1::uuid
  AND cs.display_name='Kid Shed C Lumpsum'`, fx.TenantID).
		Scan(&category, &status, &operatorID, &observationCount); err != nil {
		t.Fatalf("read imported Shed C assignment: %v", err)
	}
	if category != "per_shed_partition" || status != "pending" || operatorID != personaID("amit_operator") || observationCount != 0 {
		t.Fatalf("Shed C assignment=(%s,%s,%s,%d), want (per_shed_partition,pending,Amit,0)", category, status, operatorID, observationCount)
	}
}

func TestPartitionScopesMapToPenForDatabase(t *testing.T) {
	if got := dbLocationType("partition"); got != "pen" {
		t.Fatalf("partition mapped to %q, want pen", got)
	}
}

func TestFixtureDisplayIDsMapToDatabaseDisplayIDs(t *testing.T) {
	animal := animalFixture{
		AnimalID:  "77777777-0001-4777-8777-777777777771",
		DisplayID: "KID-A-001",
	}
	if got := dbDisplayID(animal); got != "G-777771" {
		t.Fatalf("dbDisplayID = %q, want G-777771", got)
	}
}
