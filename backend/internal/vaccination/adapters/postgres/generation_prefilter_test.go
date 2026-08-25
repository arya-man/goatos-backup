package postgres

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestCompiledGenerationPrefilterIncludesProcurementPurpose(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller unavailable")
	}
	sourcePath := strings.TrimSuffix(file, "generation_prefilter_test.go") + "repository.go"
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read repository source: %v", err)
	}
	sql := string(source)
	if !strings.Contains(sql, "prd.procurement_purpose = 'all'") ||
		!strings.Contains(sql, "lower(prd.procurement_purpose) = lower(COALESCE(proc.procurement_purpose, ''))") {
		t.Fatal("compiled generation prefilter must constrain procurement_purpose before Go-side eligibility checks")
	}
}
