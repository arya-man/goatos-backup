package app

import (
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/localization"
	"github.com/vgoats/goatos/backend/internal/sop/domain"
)

type taskPresentationCopy struct {
	vaccination string
	shedRecord  string
	date        string
}

var taskPresentationCatalog = map[string]taskPresentationCopy{
	"en": {vaccination: "Vaccination", shedRecord: "Pen record", date: "Due date"},
	"hi": {vaccination: "टीकाकरण", shedRecord: "शेड रिकॉर्ड", date: "नियत तारीख"},
	"kn": {vaccination: "ಲಸಿಕೆ", shedRecord: "ಶೆಡ್ ದಾಖಲೆ", date: "ಅಂತಿಮ ದಿನಾಂಕ"},
	"te": {vaccination: "టీకా", shedRecord: "షెడ్ రికార్డు", date: "గడువు తేదీ"},
}

func presentAppTask(task domain.TaskSummary, localeTag string) domain.TaskSummary {
	if !strings.EqualFold(strings.TrimSpace(task.TaskType), "vaccination") {
		return task
	}
	copy := taskPresentationCatalog[localization.Normalize(localeTag)]
	items := make([]domain.TaskPresentationItem, 0, 1)
	if formatted := formatTaskDate(task.DueAt); formatted != "" {
		items = append(items, domain.TaskPresentationItem{Key: "date", Label: copy.date, Value: formatted})
	}
	title := strings.TrimSpace(task.ScopeLabel)
	if title == "" {
		title = copy.shedRecord
	}
	task.Presentation = &domain.TaskPresentation{
		Eyebrow:      copy.vaccination,
		Title:        title,
		SummaryItems: items,
	}
	// Keep older app builds safe too: the legacy title must never contain a scope UUID.
	task.Title = title
	task.Description = ""
	return task
}

func formatTaskDate(raw *string) string {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return ""
	}
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(*raw))
	if err != nil {
		return ""
	}
	india := time.FixedZone("Asia/Kolkata", 5*60*60+30*60)
	return parsed.In(india).Format("02/01/2006")
}
