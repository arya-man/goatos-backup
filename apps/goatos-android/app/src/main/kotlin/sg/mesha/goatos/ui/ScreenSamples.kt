package sg.mesha.goatos.ui

import sg.mesha.goatos.feature.calendar.CalendarItem
import sg.mesha.goatos.feature.calendar.CalendarSegment
import sg.mesha.goatos.feature.calendar.CalendarSegmentKind
import sg.mesha.goatos.feature.calendar.CalendarTone
import sg.mesha.goatos.feature.calendar.CalendarUiState
import sg.mesha.goatos.feature.calendar.CalendarWeekDay
import sg.mesha.goatos.feature.leadership.CoverageHeroState
import sg.mesha.goatos.feature.leadership.DataGapPill
import sg.mesha.goatos.feature.leadership.DateOption
import sg.mesha.goatos.feature.leadership.LeadershipUiState
import sg.mesha.goatos.feature.leadership.OverdueClassification
import sg.mesha.goatos.feature.leadership.OverdueLegendItem
import sg.mesha.goatos.feature.leadership.OverdueRow
import sg.mesha.goatos.feature.leadership.OverdueUiState
import sg.mesha.goatos.feature.leadership.RescheduleSegment
import sg.mesha.goatos.feature.leadership.RescheduleUiState
import sg.mesha.goatos.feature.profile.AlertRow
import sg.mesha.goatos.feature.profile.AlertTone
import sg.mesha.goatos.feature.profile.AlertsUiState
import sg.mesha.goatos.feature.profile.ProfileUiState
import sg.mesha.goatos.feature.profile.RfidConnectionState
import sg.mesha.goatos.feature.profile.RfidUiState
import sg.mesha.goatos.feature.profile.SettingKind
import sg.mesha.goatos.feature.profile.SettingRow
import sg.mesha.goatos.feature.record.RecordUiState
import sg.mesha.goatos.feature.record.VaccineGroupRow
import sg.mesha.goatos.feature.scan.ScanTileLabels
import sg.mesha.goatos.feature.scan.ScanUiState
import sg.mesha.goatos.feature.sheds.ShedRow
import sg.mesha.goatos.feature.sheds.ShedStatus
import sg.mesha.goatos.feature.sheds.ShedsUiState
import sg.mesha.goatos.feature.submit.SubmitUiState
import sg.mesha.goatos.feature.submit.SyncState

// Interim sample states so the nav host renders the real screens end-to-end. Each
// screen's ViewModel will replace these with live /app/bootstrap-driven data — the
// screens are already stateless (dumb-renderer), so only the source changes.

fun sampleCalendarState(): CalendarUiState = CalendarUiState(
    eyebrow = "Vaccination",
    title = "Calendar",
    selectedDateLabel = "Today · Tue 7 Jul",
    segments = listOf(
        CalendarSegment("week", "Week", CalendarSegmentKind.Week),
        CalendarSegment("month", "Month", CalendarSegmentKind.Month),
        CalendarSegment("history", "History", CalendarSegmentKind.History),
    ),
    selectedSegmentId = "week",
    // Mock #weekStrip: Mon–Sun, single-letter day, date, dot on work days; today = Tue 7.
    weekDays = listOf(
        CalendarWeekDay("d6", "M", "6", "", hasWork = false),
        CalendarWeekDay("d7", "T", "7", "", hasWork = true, isToday = true, isSelected = true),
        CalendarWeekDay("d8", "W", "8", "", hasWork = true),
        CalendarWeekDay("d9", "T", "9", "", hasWork = false),
        CalendarWeekDay("d10", "F", "10", "", hasWork = true),
        CalendarWeekDay("d11", "S", "11", "", hasWork = false),
        CalendarWeekDay("d12", "S", "12", "", hasWork = false),
    ),
    weekItems = listOf(
        CalendarItem(
            id = "i1",
            title = "Today · 4 sheds",
            subtitle = "77 animals due · CBE · Gandhi 1 · Castro 1 · Mandela 1 · Sumathi 1",
            statusLabel = "Due now",
            statusTone = CalendarTone.Ok,
            categoryLabel = "Vaccination",
            ctaLabel = "Open drive",
            target = "/vaccination",
        ),
    ),
    weekEmptyLabel = "No work today",
    monthLabel = "July 2026",
    monthWeekdayLabels = listOf("S", "M", "T", "W", "T", "F", "S"),
    historyLabel = "Recent records",
    historyEmptyLabel = "No records yet",
)

fun sampleShedsState(): ShedsUiState = ShedsUiState(
    moduleLabel = "Vaccination",
    scopeLabel = "CBE · all sheds",
    title = "Today's sheds",
    date = "Thu 9 Jul",
    window = "6:00–11:00",
    shedCountLabel = "4 sheds",
    dueLabel = "128 due",
    dayProgressLabel = "42%",
    dayProgressFraction = 0.42f,
    daySummary = "54 / 128 done",
    rows = listOf(
        ShedRow(
            id = "s1", name = "Gandhi 1", cohort = "Milking does",
            status = ShedStatus.DONE, statusLabel = "Done",
            vaccineGroups = listOf(sg.mesha.goatos.feature.sheds.VaccineGroup("PPR", "40/40", true)),
            inShed = "40", due = "40", done = "40", progressLabel = "100%", progressFraction = 1f,
        ),
        ShedRow(
            id = "s2", name = "Sumathi 1", cohort = "Kids",
            status = ShedStatus.DELAYED, statusLabel = "Delayed · not started",
            vaccineGroups = listOf(sg.mesha.goatos.feature.sheds.VaccineGroup("FMD", "0/32", false)),
            inShed = "32", due = "32", done = "0", progressLabel = "0%", progressFraction = 0f,
            actionLabel = "Chase the team ›",
        ),
    ),
)

fun sampleScanState(): ScanUiState = ScanUiState(
    shedLabel = "Vaccination · Gandhi 1",
    cohortLabel = "Milking does",
    ringDone = 12,
    ringTotal = 40,
    ringUnitLabel = "vaccinated",
    tapHint = "Tap reader to animal — reader shows its due vaccine",
    vaccineGroups = emptyList(),
    doneCount = 12,
    pendingCount = 26,
    skippedCount = 2,
    tileLabels = ScanTileLabels("Done", "Pending", "Skipped"),
    feed = emptyList(),
    roster = emptyList(),
    listTitle = "Shed roster",
    submitLabel = "Submit shed record",
    canSubmit = false,
    scanEnabled = true,
)

fun sampleSubmitState(): SubmitUiState = SubmitUiState(
    eyebrow = "Vaccination",
    title = "Shed record",
    shed = "Gandhi 1",
    cohort = "Milking does",
    date = "Thu 9 Jul",
    dueSectionLabel = "Due in this shed",
    groups = listOf(
        sg.mesha.goatos.feature.submit.VaccineGroup("PPR", 40, 40, "1 ml S/C", false),
        sg.mesha.goatos.feature.submit.VaccineGroup("FMD", 38, 40, "2 ml I/M", true),
    ),
    syncState = SyncState.SYNCING,
    syncLabel = "Syncing 1 record…",
    submitLabel = "Submit",
    canSubmit = true,
    syncProgress = 0.66f,
)

fun sampleLeadershipState(): LeadershipUiState = LeadershipUiState(
    eyebrow = "Mesha · Leadership",
    title = "Overview",
    avatarInitial = "R",
    hero = CoverageHeroState(
        coverageLabel = "Dose coverage",
        coveragePercent = 78,
        dosesLine = "1,842 doses this week",
        dosesTrend = listOf(0.4f, 0.5f, 0.55f, 0.62f, 0.7f, 0.78f),
        animalsLabel = "2,567 animals",
        dataGapPill = DataGapPill("2 data gaps", true),
    ),
    kpis = emptyList(),
    todayShedsTitle = "Today's sheds",
    todaySheds = emptyList(),
    backlogTitle = "Backlog by vaccine",
    backlog = emptyList(),
    needsDecisionTitle = "Needs a decision",
    needsDecision = emptyList(),
)

fun sampleRecordState(): RecordUiState = RecordUiState(
    title = "Gandhi 1 · shed record",
    subtitle = "Thu 9 Jul · Done",
    groups = listOf(
        VaccineGroupRow("PPR", 40, 40, "1 ml S/C"),
        VaccineGroupRow("FMD", 38, 40, "2 ml I/M"),
    ),
    countLabel = "78 doses",
    statusLabel = "Done",
)

fun sampleRfidState(): RfidUiState = RfidUiState(
    title = "RFID reader",
    statusLabel = "Paired",
    connectionState = RfidConnectionState.CONNECTED,
    readerName = "Chainway R3",
    readerDetail = "Paired · battery 84%",
    primaryActionLabel = "Disconnect",
    testLabel = "Test read",
)

fun sampleAlertsState(): AlertsUiState = AlertsUiState(
    title = "Alerts",
    rows = listOf(
        AlertRow("a1", "Overdue: Castro 2 not started", "Drive due 6:00 — no scans yet", "2h ago", AlertTone.CRITICAL, unread = true),
        AlertRow("a2", "Coverage below target", "PPR at 78% vs 90% target", "5h ago", AlertTone.WARN, unread = false),
    ),
    emptyLabel = "No alerts",
    markAllLabel = "Mark all read",
)

fun sampleProfileState(): ProfileUiState = ProfileUiState(
    name = "Ravi",
    roleLabel = "Leadership",
    scopeLabel = "All parks",
    initials = "R",
    rows = listOf(
        SettingRow(SettingKind.LANGUAGE, "Language", value = "English"),
        SettingRow(SettingKind.RFID, "RFID reader", subtitle = "Chainway R3", value = "Paired", valueEmphasis = true),
        SettingRow(SettingKind.NOTIFICATIONS, "Notifications", toggleOn = true),
        SettingRow(SettingKind.SIGN_OUT, "Sign out"),
    ),
)

// Overdue + Reschedule live in feature-leadership; their in-module samples are
// `internal`, so :app carries its own interim seeds for OverdueViewModel /
// RescheduleViewModel until the leadership scope_token reads land.

fun sampleOverdueState(): OverdueUiState = OverdueUiState(
    eyebrow = "Vaccination",
    title = "Overdue · 38 animals",
    sectionTitle = "Needs rescheduling",
    legend = listOf(
        OverdueLegendItem(OverdueClassification.MISSED, "Missed · past buffer"),
        OverdueLegendItem(OverdueClassification.IN_BUFFER, "In buffer · recoverable"),
    ),
    rows = listOf(
        OverdueRow("o1", "Goat Pox · Yashoda 5", "CBE · 9 days late · past buffer · 24 animals", "Missed", OverdueClassification.MISSED),
        OverdueRow("o2", "Sheep Pox · Castro 1", "CPT · 3 days late · in buffer · 8 animals", "In buffer", OverdueClassification.IN_BUFFER),
        OverdueRow("o3", "FMD · Booster · Mandela 1", "CBE · 2 days late · in buffer · 6 animals", "In buffer", OverdueClassification.IN_BUFFER),
    ),
    explainerTitle = "What the colours mean",
    explainer = "Red · Missed — the dose window closed and the animal wasn't recovered into a " +
        "compatible drive in time. Amber · In buffer — overdue but still recoverable by reschedule " +
        "into a compatible drive, no dose missed. In/out-of-buffer is computed by backend policy.",
)

fun sampleRescheduleState(): RescheduleUiState = RescheduleUiState(
    eyebrow = "Vaccination",
    title = "Goat Pox · overdue",
    bufferMessage = "Reschedule into a compatible drive while the animal is still recoverable. " +
        "The in-buffer window is computed by backend policy; past it the dose is missed.",
    actionTitle = "Action",
    segments = listOf(
        RescheduleSegment("reschedule", "Reschedule"),
        RescheduleSegment("mark", "Mark scheduled"),
    ),
    selectedSegmentId = "reschedule",
    dateFieldLabel = "New date",
    dateOptionsTitle = "Within the backend-computed buffer window from the due date",
    dateOptions = listOf(
        DateOption("d8", "8", "Wed, 8 Jul", "Tomorrow · in buffer", inBuffer = true),
        DateOption("d9", "9", "Thu, 9 Jul", "In buffer", inBuffer = true),
        DateOption("d10", "10", "Fri, 10 Jul", "In buffer", inBuffer = true),
        DateOption("d11", "11", "Sat, 11 Jul", "Last day in buffer", inBuffer = true),
        DateOption("d13", "13", "Mon, 13 Jul", "Out of buffer · counts as missed", inBuffer = false),
    ),
    selectedDateId = null,
    assignFieldLabel = "Assign to",
    assignPrimary = "Arun Kumar",
    assignBackupLabel = "+ backup Indradev",
    assignEnabled = true,
    confirmLabel = "Confirm — notify team",
    confirmEnabled = false,
    channelsNote = "Team gets a phone call, push, Slack alert and email — 2 days before, and again the morning of.",
)

// ---------------------------------------------------------------------------
// Honest load / empty / error placeholders.
//
// These carry NO fabricated operational data (no fake rows, counts, coverage, or
// day-progress). A ViewModel shows a placeholder while loading, on an empty real
// response, and on failure — so a backend outage or empty data can never masquerade
// as valid field data. Only stable chrome labels (screen title) survive; every
// operational field is cleared and the caller-supplied [message] explains the state.
// ---------------------------------------------------------------------------

fun alertsPlaceholder(message: String): AlertsUiState =
    sampleAlertsState().copy(rows = emptyList(), emptyLabel = message, markAllLabel = null)

fun calendarPlaceholder(message: String): CalendarUiState =
    sampleCalendarState().copy(
        weekDays = emptyList(),
        weekItems = emptyList(),
        historyRows = emptyList(),
        weekEmptyLabel = message,
        historyEmptyLabel = message,
    )

fun shedsPlaceholder(message: String): ShedsUiState =
    sampleShedsState().copy(
        scopeLabel = "",
        date = "",
        window = "",
        shedCountLabel = "",
        dueLabel = "",
        dayProgressLabel = "",
        dayProgressFraction = 0f,
        daySummary = "",
        caption = message,
        roleNote = null,
        rows = emptyList(),
        rosterChanges = emptyList(),
        kernelInfo = null,
    )

fun leadershipPlaceholder(message: String): LeadershipUiState {
    val base = sampleLeadershipState()
    return base.copy(
        hero = base.hero.copy(
            coverageLabel = "Process integrity",
            coveragePercent = 0,
            dosesLine = message,
            dosesTrend = emptyList(),
            animalsLabel = "",
            dataGapPill = DataGapPill("", hasGaps = false),
        ),
        kpis = emptyList(),
        todaySheds = emptyList(),
        backlog = emptyList(),
        needsDecision = emptyList(),
        coverageByParkTitle = null,
        coverageByPark = emptyList(),
    )
}

fun overduePlaceholder(message: String): OverdueUiState =
    sampleOverdueState().copy(
        title = "Overdue",
        sectionTitle = message,
        rows = emptyList(),
    )
