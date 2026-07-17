package postgres

import (
	"os"
	"strings"
	"testing"
)

func TestScheduleProjectionRebuildsDoNotOverrideCallerDeadline(t *testing.T) {
	src, err := os.ReadFile("repository.go")
	if err != nil {
		t.Fatalf("read repository.go: %v", err)
	}
	text := string(src)
	for _, fn := range []string{
		"RebuildVaccinationScheduleWindow",
		"RebuildDirtyVaccinationScheduleWindows",
	} {
		body := functionBody(t, text, fn)
		if strings.Contains(body, "context.WithTimeout") || strings.Contains(body, "30*time.Second") {
			t.Fatalf("%s must use the caller context/deadline; found an internal timeout override", fn)
		}
	}
}

func functionBody(t *testing.T, src, name string) string {
	t.Helper()
	start := strings.Index(src, "func (r *Repository) "+name+"(")
	if start < 0 {
		t.Fatalf("function %s not found", name)
	}
	open := strings.Index(src[start:], "{")
	if open < 0 {
		t.Fatalf("function %s body not found", name)
	}
	open += start
	depth := 0
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[open : i+1]
			}
		}
	}
	t.Fatalf("function %s body did not close", name)
	return ""
}
