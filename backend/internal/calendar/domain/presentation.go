package domain

type ownerPresentationConfig struct {
	key          string
	label        string
	scopeLabel   string
	color        string
	rhythmTitle  string
	rhythmNote   string
	rhythmDays   []CalendarRhythmDay
	workstreams  []CalendarPresentationTab
	weekTitle    string
	monthScope   string
	scopeMessage string
}

// CalendarPresentationForQuery returns the backend-owned screen metadata for the
// Calendar slice. Frontends render this contract instead of hardcoding tab order,
// labels, colors, rhythm chips, and user-facing copy.
func CalendarPresentationForQuery(ownerKey string) CalendarPresentation {
	if ownerKey == "" {
		ownerKey = OwnerAll
	}
	active, ok := ownerPresentationByKey(ownerKey)
	if !ok {
		active, _ = ownerPresentationByKey(OwnerAll)
		ownerKey = OwnerAll
	}

	return CalendarPresentation{
		PageTitle:              "Calendar",
		PageSubtitle:           "Scheduled work by vertical, module, park, and date.",
		ViewTabs:               calendarViewTabs(),
		OwnerTabs:              calendarOwnerTabs(ownerKey),
		WorkstreamTabs:         active.workstreams,
		Rhythm:                 CalendarRhythmPresentation{Title: active.rhythmTitle, Note: active.rhythmNote, Days: active.rhythmDays},
		Week:                   calendarWeekPresentation(active),
		Month:                  calendarMonthPresentation(active),
		NewEvent:               CalendarActionPresentation{Label: "New event", Enabled: false, DisabledReason: "Calendar events are generated from configured obligations. Create a campaign/catch-up via Config or the Preventive Care (PC) catch-up path - not a free-form Calendar entry."},
		EmptyState:             CalendarEmptyStatePresentation{OkMessage: "Park vaccination drives and review queues will appear here when scheduled.", ErrorMessage: "Calendar is unavailable - resolve the error above, then reload.", PrimaryLabel: "Vaccination", SecondaryLabel: "Calendar"},
		EventTypes:             calendarEventTypeLabels(),
		ActiveOwnerKey:         ownerKey,
		ActiveOwnerLabel:       active.label,
		ActiveOwnerScopeLabel:  active.scopeLabel,
		ActiveOwnerColor:       active.color,
		AllOwnersSelectedLabel: "all verticals",
	}
}

func calendarViewTabs() []CalendarPresentationTab {
	return []CalendarPresentationTab{
		{Key: "week", Label: "Week", Enabled: true, Query: map[string]string{"view": ""}},
		{Key: "month", Label: "Month", Enabled: true, Query: map[string]string{"view": "month", "day": ""}},
		{Key: "history", Label: "History", Enabled: true, Query: map[string]string{"status": StatusCompleted}},
	}
}

func calendarOwnerTabs(activeOwnerKey string) []CalendarOwnerPresentationTab {
	out := make([]CalendarOwnerPresentationTab, 0, len(ownerPresentationOrder))
	for _, key := range ownerPresentationOrder {
		cfg, _ := ownerPresentationByKey(key)
		query := map[string]string{"owner_key": ""}
		if key != OwnerAll {
			query["owner_key"] = key
		}
		out = append(out, CalendarOwnerPresentationTab{
			Key:        key,
			Label:      cfg.label,
			ScopeLabel: cfg.scopeLabel,
			Color:      cfg.color,
			Active:     key == activeOwnerKey,
			Enabled:    true,
			Query:      query,
		})
	}
	return out
}

func calendarWeekPresentation(active ownerPresentationConfig) CalendarViewPresentation {
	return CalendarViewPresentation{
		Title:                active.weekTitle,
		ScopeLabel:           active.scopeLabel,
		ScopeOnlyMessage:     active.scopeMessage,
		ClearScopeLabel:      "all verticals",
		WholePeriodMessage:   "Showing whole week.",
		AllDaysSelectedLabel: "all days selected",
		ClearDayLabel:        "whole week",
		EmptyMessage:         "No drives scheduled",
		ReminderTitle:        "Reminders & escalation",
		ReminderEmptyMessage: "No reminders scheduled in this scope.",
		ReminderNote:         "Reminders, nudges, snoozes, and escalations are durable backend kernel state. Open an event to act.",
	}
}

func calendarMonthPresentation(active ownerPresentationConfig) CalendarViewPresentation {
	return CalendarViewPresentation{
		ScopeLabel: active.monthScope,
		AsOfHint:   "month follows the top-bar as-of date",
		CellNote:   "Each cell shows that day's configured vaccination due work. Tap an event for its rich detail.",
	}
}

func ownerPresentationByKey(key string) (ownerPresentationConfig, bool) {
	for _, cfg := range ownerPresentationConfigs {
		if cfg.key == key {
			return cfg, true
		}
	}
	return ownerPresentationConfig{}, false
}

func rhythmDay(day, label, tone string) CalendarRhythmDay {
	return CalendarRhythmDay{Day: day, Label: label, Tone: tone, Enabled: true, Query: map[string]string{"day": day}}
}

func activeWorkstream(key, label, hint string) CalendarPresentationTab {
	return CalendarPresentationTab{Key: key, Label: label, Active: true, Enabled: true, DisabledReason: hint, Query: emptyQuery()}
}

func disabledWorkstream(key, label, reason string) CalendarPresentationTab {
	return CalendarPresentationTab{Key: key, Label: label, Enabled: false, DisabledReason: reason, Query: map[string]string{"workstream_key": key}}
}

func emptyQuery() map[string]string {
	return map[string]string{}
}

func calendarEventTypeLabels() []CalendarKeyLabel {
	return []CalendarKeyLabel{
		{Key: EventVaccinationDoseDue, Label: "Dose due"},
		{Key: EventVaccinationDrive, Label: "Vaccination drive"},
		{Key: EventVaccinationCampaign, Label: "Campaign / catch-up"},
		{Key: EventVaccinationBoosterDue, Label: "Booster due"},
		{Key: EventVaccinationDeferReview, Label: "Defer / waiver review"},
		{Key: EventVaccinationEvidenceReview, Label: "HF / historical evidence review"},
		{Key: EventVaccinationProofVerification, Label: "Proof verification"},
		{Key: EventVaccinationReworkDue, Label: "Rework due"},
		{Key: EventVaccineStockReadiness, Label: "Stock readiness"},
		{Key: EventVaccineColdChainCheck, Label: "Cold-chain check"},
		{Key: EventVaccineReorderExpiryGRN, Label: "Reorder / expiry / GRN"},
		{Key: EventPCStockAntiMisuse, Label: "Stock anti-misuse"},
		{Key: EventVaccinationConfigActivationReview, Label: "Config activation review"},
	}
}

var ownerPresentationOrder = []string{OwnerAll, OwnerPC, OwnerInventory, OwnerAdminDataOps}

var ownerPresentationConfigs = []ownerPresentationConfig{
	{
		key:          OwnerAll,
		label:        "All",
		scopeLabel:   "All verticals",
		color:        "var(--brand)",
		rhythmTitle:  "Vaccination operating rhythm",
		rhythmNote:   "from the SOP handbook - all owner lanes",
		rhythmDays:   []CalendarRhythmDay{rhythmDay("Mon", "PLAN", "plan"), rhythmDay("Tue", "LOGISTICS", "log"), rhythmDay("Wed", "EXECUTE", "exec"), rhythmDay("Thu", "EXECUTE", "exec"), rhythmDay("Fri", "EXECUTE", "exec"), rhythmDay("Sat", "EXECUTE", "exec"), rhythmDay("Sun", "REST", "rest")},
		workstreams:  []CalendarPresentationTab{activeWorkstream("vaccination", "Vaccination", "Calendar is currently showing the Preventive Care (PC) vaccination module.")},
		weekTitle:    "This week",
		monthScope:   "All verticals",
		scopeMessage: "",
	},
	{
		key:          OwnerPC,
		label:        "Preventive Care (PC)",
		scopeLabel:   "Preventive Care (PC)",
		color:        "var(--brand)",
		rhythmTitle:  "Preventive Care (PC) vaccination rhythm",
		rhythmNote:   "plan drives - prepare teams - execute proof",
		rhythmDays:   []CalendarRhythmDay{rhythmDay("Mon", "PLAN", "plan"), rhythmDay("Tue", "PREP", "log"), rhythmDay("Wed", "DRIVE", "exec"), rhythmDay("Thu", "DRIVE", "exec"), rhythmDay("Fri", "VERIFY", "exec"), rhythmDay("Sat", "CATCH-UP", "exec"), rhythmDay("Sun", "REST", "rest")},
		workstreams:  []CalendarPresentationTab{activeWorkstream("vaccination", "Vaccination", "Calendar is currently showing the Preventive Care (PC) vaccination module.")},
		weekTitle:    "Preventive Care (PC) this week",
		monthScope:   "Preventive Care (PC)",
		scopeMessage: "Showing the Vaccination module inside Preventive Care (PC).",
	},
	{
		key:          OwnerInventory,
		label:        "Inventory / Stock",
		scopeLabel:   "Inventory / Stock",
		color:        "var(--amber)",
		rhythmTitle:  "Inventory / Stock operating rhythm",
		rhythmNote:   "readiness - cold chain - replenishment",
		rhythmDays:   []CalendarRhythmDay{rhythmDay("Mon", "PLAN", "plan"), rhythmDay("Tue", "CHECK", "log"), rhythmDay("Wed", "READY", "exec"), rhythmDay("Thu", "READY", "exec"), rhythmDay("Fri", "REPLENISH", "exec"), rhythmDay("Sat", "AUDIT", "exec"), rhythmDay("Sun", "REST", "rest")},
		workstreams:  []CalendarPresentationTab{activeWorkstream("all_inventory_stock", "All Inventory / Stock", "Calendar can host inventory / stock work when this slice is enabled.")},
		weekTitle:    "Inventory / Stock this week",
		monthScope:   "Inventory / Stock",
		scopeMessage: "Showing Inventory / Stock work only.",
	},
	{
		key:          OwnerAdminDataOps,
		label:        "Admin / Data Ops",
		scopeLabel:   "Admin / Data Ops",
		color:        "var(--purple)",
		rhythmTitle:  "Admin / Data Ops operating rhythm",
		rhythmNote:   "review - verify - reconcile",
		rhythmDays:   []CalendarRhythmDay{rhythmDay("Mon", "PLAN", "plan"), rhythmDay("Tue", "REVIEW", "log"), rhythmDay("Wed", "VERIFY", "exec"), rhythmDay("Thu", "VERIFY", "exec"), rhythmDay("Fri", "RECONCILE", "exec"), rhythmDay("Sat", "FOLLOW-UP", "exec"), rhythmDay("Sun", "REST", "rest")},
		workstreams:  []CalendarPresentationTab{activeWorkstream("all_admin_data_ops", "All Admin / Data Ops", "Calendar can host Admin / Data Ops work when this slice is enabled.")},
		weekTitle:    "Admin / Data Ops this week",
		monthScope:   "Admin / Data Ops",
		scopeMessage: "Showing Admin / Data Ops work only.",
	},
}
