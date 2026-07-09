package sg.mesha.goatos.ui

import sg.mesha.goatos.feature.calendar.CalendarItem
import sg.mesha.goatos.feature.calendar.CalendarSegment
import sg.mesha.goatos.feature.calendar.CalendarSegmentKind
import sg.mesha.goatos.feature.calendar.CalendarTone
import sg.mesha.goatos.feature.calendar.CalendarUiState
import sg.mesha.goatos.feature.calendar.CalendarWeekDay
import sg.mesha.goatos.feature.leadership.CoverageHeroState
import sg.mesha.goatos.feature.leadership.DataGapPill
import sg.mesha.goatos.feature.leadership.LeadershipUiState
import sg.mesha.goatos.feature.profile.ProfileUiState
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
    eyebrow = "Mesha",
    title = "Calendar",
    selectedDateLabel = "Thu 9 Jul",
    segments = listOf(
        CalendarSegment("week", "Week", CalendarSegmentKind.Week),
        CalendarSegment("month", "Month", CalendarSegmentKind.Month),
        CalendarSegment("history", "History", CalendarSegmentKind.History),
    ),
    selectedSegmentId = "week",
    weekDays = listOf(
        CalendarWeekDay("d1", "MON", "7", "2 due", true),
        CalendarWeekDay("d2", "TUE", "8", "", false, isToday = true),
        CalendarWeekDay("d3", "WED", "9", "1 due", true),
    ),
    weekItems = listOf(
        CalendarItem("i1", "Gandhi 1 · Milking", "PPR + FMD", "3 sheds due", CalendarTone.Warn, ctaLabel = "Open drive", target = "/vaccination"),
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
