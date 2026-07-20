package app

import (
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/sop/domain"
)

func TestPresentAppTaskLocalizesSupportedLocalesWithoutInternalIDs(t *testing.T) {
	rawID := "00000000-0000-4000-8000-000000003001"
	dueAt := "2026-07-20T00:00:00Z"
	want := map[string][3]string{
		"en": {"Vaccination", "Shed record", "Due date"},
		"hi": {"टीकाकरण", "शेड रिकॉर्ड", "नियत तारीख"},
		"kn": {"ಲಸಿಕೆ", "ಶೆಡ್ ದಾಖಲೆ", "ಅಂತಿಮ ದಿನಾಂಕ"},
		"te": {"టీకా", "షెడ్ రికార్డు", "గడువు తేదీ"},
	}
	for locale, labels := range want {
		t.Run(locale, func(t *testing.T) {
			got := presentAppTask(domain.TaskSummary{
				TaskType:   "vaccination",
				Title:      "Vaccination drive " + rawID,
				ScopeID:    rawID,
				ScopeLabel: "Gandhi 1",
				DueAt:      &dueAt,
			}, locale)
			if got.Presentation == nil {
				t.Fatal("presentation is nil")
			}
			if got.Presentation.Eyebrow != labels[0] || got.Presentation.Title != "Gandhi 1" {
				t.Fatalf("presentation=%+v want eyebrow=%q resolved shed title=%q", got.Presentation, labels[0], "Gandhi 1")
			}
			if len(got.Presentation.SummaryItems) != 1 || got.Presentation.SummaryItems[0].Label != labels[2] {
				t.Fatalf("summary_items=%+v want localized date label %q", got.Presentation.SummaryItems, labels[2])
			}
			if strings.Contains(got.Title, rawID) || strings.Contains(got.Presentation.Title, rawID) {
				t.Fatalf("internal UUID leaked into user-visible copy: %+v", got)
			}
		})
	}
}
