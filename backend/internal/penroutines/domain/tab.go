package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// PHONE TABS (maintainer instruction 2026-10-01, docs/decisions/simple-task-phone-tabs.md): a
// simple task -- "fumigation, just two videos" -- is a routine, and a phone tab is WHERE it
// appears: a label, the phone module whose bottom bar carries it, an icon, and which list
// filters the tab offers. Every key below is a closed vocabulary because each one is rendered
// by code the phone and the web already ship: an icon key the phone cannot draw, or a module it
// does not know, would reach the bar as a blank.

// Tab status.
const (
	TabStatusActive  = "active"
	TabStatusRetired = "retired"
)

// Tab filters: which controls the tab's list offers. Status is the Pending / Completed pills,
// date the due-date window, pen the pen picker.
const (
	TabFilterStatus = "status"
	TabFilterDate   = "date"
	TabFilterPen    = "pen"
)

// TabFilters is the closed filter vocabulary, in display order.
var TabFilters = []string{TabFilterStatus, TabFilterDate, TabFilterPen}

var tabFilterLabels = map[string]string{
	TabFilterStatus: "Pending / Completed",
	TabFilterDate:   "Date",
	TabFilterPen:    "Pen",
}

// TabFilterLabel is the web's word for a filter key.
func TabFilterLabel(key string) string { return tabFilterLabels[key] }

// TabModule is one phone module a tab may sit in. The key is the bootstrap module key
// (workforce/app moduleNavRegistry); the label is the module's own English name.
type TabModule struct {
	Key   string
	Label string
}

// TabModules is the closed set of phone modules a tab may sit in, in display order: the modules
// that have an SOP page (a tab is born from a phone-task SOP on that page), plus Routines, the
// fallback bar for a person not served the module.
var TabModules = []TabModule{
	{Key: "pen_routines", Label: "Routines"},
	{Key: "pc_care", Label: "Preventive Care"},
	{Key: "feed_direction", Label: "Feed"},
	{Key: "weighing", Label: "Weighing"},
	{Key: "counts", Label: "Herd Operations"},
	{Key: "milk", Label: "Milk"},
	{Key: "vendors", Label: "Procurement"},
	{Key: "sales", Label: "Sales"},
}

// TabModuleLabel names a module key, "" when unknown.
func TabModuleLabel(key string) string {
	for _, m := range TabModules {
		if m.Key == key {
			return m.Label
		}
	}
	return ""
}

// TabIcon is one icon a tab may carry. The phone maps every key to a glyph it ships
// (MeshaIcons.forTabIcon), pinned by a test on each side; the label is the web picker's word.
type TabIcon struct {
	Key   string
	Label string
}

// TabIcons is the closed icon set, in picker order.
var TabIcons = []TabIcon{
	{Key: "routine", Label: "Routine"},
	{Key: "clipboard_check", Label: "Checklist"},
	{Key: "check_circle", Label: "Tick"},
	{Key: "camera", Label: "Camera"},
	{Key: "video", Label: "Video"},
	{Key: "photo", Label: "Photo"},
	{Key: "calendar", Label: "Calendar"},
	{Key: "clock", Label: "Clock"},
	{Key: "bell", Label: "Bell"},
	{Key: "eye", Label: "Inspection"},
	{Key: "home", Label: "Pen"},
	{Key: "pen_visit", Label: "Pen visit"},
	{Key: "goat", Label: "Animal"},
	{Key: "health", Label: "Health"},
	{Key: "pc_care", Label: "Preventive care"},
	{Key: "syringe", Label: "Injection"},
	{Key: "vaccine", Label: "Vaccine"},
	{Key: "deworming", Label: "Deworming"},
	{Key: "anti_protozoan", Label: "Anti protozoan"},
	{Key: "tick", Label: "Ticks"},
	{Key: "hoof_trimming", Label: "Hoof trimming"},
	{Key: "hair_trimming", Label: "Hair trimming"},
	{Key: "fumigation", Label: "Fumigation"},
	{Key: "feed", Label: "Feed"},
	{Key: "water", Label: "Water"},
	{Key: "package", Label: "Package"},
	{Key: "truck", Label: "Transport"},
	{Key: "store", Label: "Store"},
	{Key: "milk", Label: "Milk"},
	{Key: "breeding", Label: "Breeding"},
	{Key: "birth", Label: "Birth"},
	{Key: "bar_chart", Label: "Chart"},
	{Key: "document", Label: "Document"},
	{Key: "tasks", Label: "Tasks"},
	{Key: "warn", Label: "Warning"},
}

func isTabIcon(key string) bool {
	for _, i := range TabIcons {
		if i.Key == key {
			return true
		}
	}
	return false
}

// MaxTabLabelLength keeps a label inside one bottom-bar slot.
const MaxTabLabelLength = 24

// MaxRoutinesPerTab bounds the routines a tab write names (a handful per park).
const MaxRoutinesPerTab = 50

// Tab is one authored phone tab.
type Tab struct {
	TabID     string
	TenantID  string
	Key       string
	Label     string
	ModuleKey string
	IconKey   string
	Filters   []string
	Status    string
	// RoutineIDs are the routines placed on the tab. On write they REPLACE the tab's routines; a
	// routine named here leaves whichever tab it was on before (a routine sits on one tab).
	RoutineIDs []string
	// Routines is the read's preview of those routines (name + park), ignored on write.
	Routines   []TabRoutine
	RowVersion int
	UpdatedAt  time.Time
}

// TabRoutine names one routine on a tab.
type TabRoutine struct {
	RoutineID string
	Name      string
	ParkName  string
}

// PhoneTab is a tab as one person's bottom bar carries it.
type PhoneTab struct {
	Key       string
	Label     string
	ModuleKey string
	IconKey   string
}

// PhoneTabHrefPrefix is the phone route family of every tab; the key follows it.
const PhoneTabHrefPrefix = "/pen-routines/tab/"

// Href is the tab's phone route.
func (t PhoneTab) Href() string { return PhoneTabHrefPrefix + t.Key }

// NavKey is the tab's bar item key; the phone draws it with the tab's own icon.
func (t PhoneTab) NavKey() string { return "routine_tab_" + t.Key }

// ErrInvalidTab is every authoring refusal on a tab; its message is farm-worded.
var ErrInvalidTab = errors.New("pen routine: the phone tab is not valid")

// NormalizeTab trims and dedupes, keeping the closed vocabularies' own order for filters.
func NormalizeTab(t Tab) Tab {
	t.Label = strings.Join(strings.Fields(t.Label), " ")
	t.ModuleKey = strings.TrimSpace(t.ModuleKey)
	t.IconKey = strings.TrimSpace(t.IconKey)
	picked := map[string]bool{}
	for _, f := range t.Filters {
		picked[strings.TrimSpace(f)] = true
	}
	filters := make([]string, 0, len(TabFilters))
	for _, f := range TabFilters {
		if picked[f] {
			filters = append(filters, f)
			delete(picked, f)
		}
	}
	// An unknown filter is kept so ValidateTab refuses it rather than silently dropping it.
	for f := range picked {
		if f != "" {
			filters = append(filters, f)
		}
	}
	t.Filters = filters
	t.RoutineIDs = dedupe(t.RoutineIDs)
	return t
}

// ValidateTab is the authoring gate on a tab.
func ValidateTab(t Tab) error {
	if t.Label == "" || len([]rune(t.Label)) > MaxTabLabelLength {
		return fmt.Errorf("%w: a tab name of at most %d characters is required", ErrInvalidTab, MaxTabLabelLength)
	}
	if TabModuleLabel(t.ModuleKey) == "" {
		return fmt.Errorf("%w: choose which module the tab sits in", ErrInvalidTab)
	}
	if !isTabIcon(t.IconKey) {
		return fmt.Errorf("%w: choose an icon", ErrInvalidTab)
	}
	for _, f := range t.Filters {
		if tabFilterLabels[f] == "" {
			return fmt.Errorf("%w: %q is not a filter the phone offers", ErrInvalidTab, f)
		}
	}
	if len(t.RoutineIDs) > MaxRoutinesPerTab {
		return fmt.Errorf("%w: a tab holds at most %d routines", ErrInvalidTab, MaxRoutinesPerTab)
	}
	return nil
}

// TabKeyBase derives a stable route key from the label: lowercase letters, digits and
// underscores, starting with a letter ("Fumigation" -> "fumigation", "Pen wash 2" ->
// "pen_wash_2"). A label with no such characters (a label written in another script) falls back
// to "tab". The key is fixed at create: renaming a tab never moves its route.
func TabKeyBase(label string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range strings.ToLower(label) {
		switch {
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			if b.Len() == 0 && unicode.IsDigit(r) {
				b.WriteString("tab_")
			}
			b.WriteRune(r)
			lastUnderscore = false
		case b.Len() > 0 && !lastUnderscore:
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	key := strings.TrimRight(b.String(), "_")
	if len(key) > 32 {
		key = strings.TrimRight(key[:32], "_")
	}
	if len(key) < 2 {
		return "tab"
	}
	return key
}
