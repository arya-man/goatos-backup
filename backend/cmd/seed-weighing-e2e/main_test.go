package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

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
		"shed_observations=1",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("dry-run output missing %q: %s", want, got)
		}
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
