package e2e

import (
	"strings"
	"testing"
	"time"
)

func TestReportCertificationCompletenessRendersAndCountsMissingSurface(t *testing.T) {
	report := &Report{GeneratedAt: time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC), Stories: []StoryResult{
		{ID: "declared", Title: "Declared", Certification: " backend kernel + HTTP ", Pass: true},
		{ID: "missing", Title: "Missing", Certification: "   ", Pass: true},
	}}

	view := report.snapshot()
	if view.CertificationCount != 1 || view.CertificationMissingCount != 1 {
		t.Fatalf("certification counts = declared:%d missing:%d, want 1/1", view.CertificationCount, view.CertificationMissingCount)
	}
	if got := view.Stories[0].Certification; got != "backend kernel + HTTP" {
		t.Fatalf("trimmed declared certification = %q", got)
	}
	if got := view.Stories[1].Certification; got != undeclaredCertification {
		t.Fatalf("missing certification display = %q, want %q", got, undeclaredCertification)
	}
	if view.Stories[1].Pass || view.PassCount != 1 || view.FailCount != 1 {
		t.Fatalf("missing certification did not fail rendered story: story_pass=%t pass=%d fail=%d",
			view.Stories[1].Pass, view.PassCount, view.FailCount)
	}
	declared, missing := report.CertificationCompleteness()
	if declared != 1 || missing != 1 {
		t.Fatalf("raw completeness = declared:%d missing:%d, want 1/1", declared, missing)
	}

	var rendered strings.Builder
	if err := reportTemplate.Execute(&rendered, view); err != nil {
		t.Fatalf("render report: %v", err)
	}
	if !strings.Contains(rendered.String(), "1/2</span>certification surfaces declared") ||
		!strings.Contains(rendered.String(), "Certified surface: UNDECLARED") {
		t.Fatalf("rendered report omitted certification completeness: %s", rendered.String())
	}
}
