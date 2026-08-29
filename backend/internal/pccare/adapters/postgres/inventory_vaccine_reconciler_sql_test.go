package postgres

import (
	"os"
	"strings"
	"testing"
)

func TestInventoryVaccineLiveTasksIncludesInsertedRows(t *testing.T) {
	src, err := os.ReadFile("inventory_vaccine_reconciler.go")
	if err != nil {
		t.Fatalf("read reconciler source: %v", err)
	}
	sql := string(src)
	liveIdx := strings.Index(sql, "live_tasks AS (")
	if liveIdx < 0 {
		t.Fatal("live_tasks CTE not found")
	}
	insertedIdx := strings.Index(sql[liveIdx:], "FROM inserted_tasks")
	tableReadIdx := strings.Index(sql[liveIdx:], "FROM pc_care_tasks t")
	if insertedIdx < 0 {
		t.Fatal("live_tasks must include inserted_tasks so first-pass reconciliation populates new task assignees and requirements")
	}
	if tableReadIdx < 0 {
		t.Fatal("live_tasks existing-task table read not found")
	}
	if insertedIdx > tableReadIdx {
		t.Fatal("live_tasks must union inserted_tasks before reading existing pc_care_tasks")
	}
}
