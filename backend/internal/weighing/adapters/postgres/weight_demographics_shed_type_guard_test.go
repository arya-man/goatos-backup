package postgres

import (
	"os"
	"strings"
	"testing"
)

func TestWeightDemographicsShedTypeNameFallbackIncludesManjuSheds(t *testing.T) {
	text, err := os.ReadFile("weight_demographics.go")
	if err != nil {
		t.Fatalf("read weight_demographics.go: %v", err)
	}
	query := string(text)
	for _, name := range []string{"gandhi", "castro", "ho chi minh", "old yashoda", "yashoda old"} {
		if !strings.Contains(query, name) {
			t.Fatalf("ground shed fallback is missing %q", name)
		}
	}
	for _, name := range []string{"mandela", "godel", "sumathi", "new yashoda", "yashoda new"} {
		if !strings.Contains(query, name) {
			t.Fatalf("elevated shed fallback is missing %q", name)
		}
	}
}
