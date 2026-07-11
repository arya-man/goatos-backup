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
		PageSubtitle:           "Vaccination due work by time — configured obligations, drives, boosters, proof/rework, defer reviews, and the stock / config tasks that gate them. Open a drive to page through eligible goats (10 per page).",
		ViewTabs:               calendarViewTabs(),
		OwnerTabs:              calendarOwnerTabs(ownerKey),
		WorkstreamTabs:         active.workstreams,
		Rhythm:                 CalendarRhythmPresentation{Title: active.rhythmTitle, Note: active.rhythmNote, Days: active.rhythmDays},
		Week:                   calendarWeekPresentation(active),
		Month:                  calendarMonthPresentation(active),
		NewEvent:               CalendarActionPresentation{Label: "New event", Enabled: false, DisabledReason: "Calendar events are generated from configured obligations. Create a campaign/catch-up via Config or the Preventive Care (PC) catch-up path - not a free-form Calendar entry."},
		EmptyState:             CalendarEmptyStatePresentation{OkMessage: "Configured vaccination drives, boosters, proof/rework, and stock gates will appear here when due.", ErrorMessage: "Calendar is unavailable - resolve the error above, then reload.", PrimaryLabel: "Config", SecondaryLabel: "Vaccination"},
		EventTypes:             calendarEventTypeLabels(),
		ActiveOwnerKey:         ownerKey,
		ActiveOwnerLabel:       active.label,
		ActiveOwnerScopeLabel:  active.scopeLabel,
		ActiveOwnerColor:       active.color,
		AllOwnersSelectedLabel: "all owner lanes",
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
		ClearScopeLabel:      "all owner lanes",
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
		{Key: EventVaccinationDrive, Label: "Shed / cohort drive"},
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
		scopeLabel:   "All owner lanes",
		color:        "var(--brand)",
		rhythmTitle:  "Vaccination operating rhythm",
		rhythmNote:   "from the SOP handbook - all owner lanes",
		rhythmDays:   []CalendarRhythmDay{rhythmDay("Mon", "PLAN", "plan"), rhythmDay("Tue", "LOGISTICS", "log"), rhythmDay("Wed", "EXECUTE", "exec"), rhythmDay("Thu", "EXECUTE", "exec"), rhythmDay("Fri", "EXECUTE", "exec"), rhythmDay("Sat", "EXECUTE", "exec"), rhythmDay("Sun", "REST", "rest")},
		workstreams:  []CalendarPresentationTab{activeWorkstream("vaccination", "Vaccination", "Calendar is currently showing the Preventive Care (PC) vaccination module.")},
		weekTitle:    "This week",
		monthScope:   "All owner lanes",
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
		scopeMessage: "Showing Preventive Care (PC) work only.",
	},
	{
		key:         OwnerInventory,
		label:       "Inventory / Stock",
		scopeLabel:  "Inventory / Stock",
		color:       "var(--amber)",
		rhythmTitle: "Vaccination stock readiness rhythm",
		rhythmNote:  "stock, FEFO, cold-chain, reorder, GRN",
		rhythmDays: []CalendarRhythmDay{
			rhythmDay("Mon", "COUNT", "plan"),
			rhythmDay("Tue", "FEFO", "log"),
			rhythmDay("Wed", "ISSUE", "exec"),
			rhythmDay("Thu", "MONITOR", "exec"),
			rhythmDay("Fri", "REORDER", "log"),
			rhythmDay("Sat", "CLOSE", "exec"),
			rhythmDay("Sun", "REST", "rest"),
		},
		workstreams: []CalendarPresentationTab{
			activeWorkstream("all_inventory_stock", "All Inventory / Stock", ""),
			disabledWorkstream("stock_readiness", "Stock readiness", "Sub-workstream filters need backend event_type support; this row is the module context."),
			disabledWorkstream("cold_chain", "Cold chain", "Sub-workstream filters need backend event_type support; this row is the module context."),
			disabledWorkstream("reorder_expiry", "Reorder / expiry", "Sub-workstream filters need backend event_type support; this row is the module context."),
			disabledWorkstream("grn_fefo", "GRN / FEFO", "Sub-workstream filters need backend event_type support; this row is the module context."),
		},
		weekTitle:    "Inventory / Stock this week",
		monthScope:   "Inventory / Stock",
		scopeMessage: "Showing Inventory / Stock work only.",
	},
	{
		key:         OwnerAdminDataOps,
		label:       "Admin / Data Ops",
		scopeLabel:  "Admin / Data Ops",
		color:       "var(--purple)",
		rhythmTitle: "Vaccination governance rhythm",
		rhythmNote:  "source review, config approval, import, audit follow-up",
		rhythmDays: []CalendarRhythmDay{
			rhythmDay("Mon", "REVIEW", "plan"),
			rhythmDay("Tue", "CONFIG", "log"),
			rhythmDay("Wed", "IMPORT", "exec"),
			rhythmDay("Thu", "AUDIT", "exec"),
			rhythmDay("Fri", "APPROVE", "log"),
			rhythmDay("Sat", "FOLLOW-UP", "exec"),
			rhythmDay("Sun", "REST", "rest"),
		},
		workstreams: []CalendarPresentationTab{
			activeWorkstream("all_admin_data_ops", "All Admin / Data Ops", ""),
			disabledWorkstream("source_review", "Source review", "Sub-workstream filters need backend event_type support; this row is the module context."),
			disabledWorkstream("config_approval", "Config approval", "Sub-workstream filters need backend event_type support; this row is the module context."),
			disabledWorkstream("import_replay", "Import / replay", "Sub-workstream filters need backend event_type support; this row is the module context."),
			disabledWorkstream("audit_follow_up", "Audit follow-up", "Sub-workstream filters need backend event_type support; this row is the module context."),
		},
		weekTitle:    "Admin / Data Ops this week",
		monthScope:   "Admin / Data Ops",
		scopeMessage: "Showing Admin / Data Ops work only.",
	},
}
