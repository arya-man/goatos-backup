package sg.mesha.goatos.ui

import sg.mesha.goatos.feature.calendar.CalendarItem
import sg.mesha.goatos.feature.calendar.CalendarSegment
import sg.mesha.goatos.feature.calendar.CalendarSegmentKind
import sg.mesha.goatos.feature.calendar.CalendarTone
import sg.mesha.goatos.feature.calendar.CalendarUiState
import sg.mesha.goatos.feature.calendar.CalendarWeekDay
import sg.mesha.goatos.feature.leadership.BacklogRow
import sg.mesha.goatos.feature.leadership.CoverageHeroState
import sg.mesha.goatos.feature.leadership.DataGapPill
import sg.mesha.goatos.feature.leadership.DateOption
import sg.mesha.goatos.feature.leadership.DecisionRow
import sg.mesha.goatos.feature.leadership.KpiTile
import sg.mesha.goatos.feature.leadership.LeadershipUiState
import sg.mesha.goatos.feature.leadership.OverdueClassification
import sg.mesha.goatos.feature.leadership.OverdueLegendItem
import sg.mesha.goatos.feature.leadership.OverdueRow
import sg.mesha.goatos.feature.leadership.OverdueUiState
import sg.mesha.goatos.feature.leadership.ParkCoverageRow
import sg.mesha.goatos.feature.leadership.RescheduleSegment
import sg.mesha.goatos.feature.leadership.RescheduleUiState
import sg.mesha.goatos.feature.leadership.ScopePill
import sg.mesha.goatos.feature.leadership.ShedSummary
import sg.mesha.goatos.feature.leadership.Tone
import sg.mesha.goatos.feature.leadership.VaccineGroupChip
import sg.mesha.goatos.feature.profile.AlertChannel
import sg.mesha.goatos.feature.profile.AlertChannelTone
import sg.mesha.goatos.feature.profile.AlertRow
import sg.mesha.goatos.feature.profile.AlertTone
import sg.mesha.goatos.feature.profile.AlertsUiState
import sg.mesha.goatos.feature.profile.ProfileUiState
import sg.mesha.goatos.feature.profile.RfidConnectionState
import sg.mesha.goatos.feature.profile.RfidDetailStatus
import sg.mesha.goatos.feature.profile.RfidUiState
import sg.mesha.goatos.feature.profile.SettingKind
import sg.mesha.goatos.feature.profile.SettingRow
import sg.mesha.goatos.feature.record.RecordUiState
import sg.mesha.goatos.feature.record.VaccineGroupRow
import sg.mesha.goatos.feature.scan.ScanReaderConnection
import sg.mesha.goatos.feature.scan.ScanTileLabels
import sg.mesha.goatos.feature.scan.ScanUiState
import sg.mesha.goatos.feature.sheds.ShedRow
import sg.mesha.goatos.feature.sheds.ShedStatus
import sg.mesha.goatos.feature.sheds.ShedsUiState
import sg.mesha.goatos.feature.submit.FieldKindUi
import sg.mesha.goatos.feature.submit.FormFieldUi
import sg.mesha.goatos.feature.submit.FormRunnerState
import sg.mesha.goatos.feature.submit.SubmitUiState
import sg.mesha.goatos.feature.submit.SyncState
import sg.mesha.goatos.feature.timetable.PositionTier
import sg.mesha.goatos.feature.timetable.TimetableRow
import sg.mesha.goatos.feature.timetable.TimetableUiState
import sg.mesha.goatos.core.ui.CoverageBannerUiState

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
    tapHint = "Hold the Bluetooth reader near the goat tag. A known tag is marked Done; an unknown tag is marked Skipped.",
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
    isRefreshing = false,
    lastSyncedAt = System.currentTimeMillis(),
    isOffline = false,
    readerConnection = ScanReaderConnection(
        readerName = "IDT RHLS-3",
        statusLabel = "Reader disconnected",
        connected = false,
        actionLabel = "Reconnect",
    ),
)

fun sampleSubmitState(): SubmitUiState = SubmitUiState(
    eyebrow = "Vaccination",
    title = "Shed record",
    shed = "Gandhi 1",
    cohort = "Milking does",
    date = "Thu 9 Jul",
    dueSectionLabel = "Due in this shed",
    groups = listOf(
        sg.mesha.goatos.feature.submit.VaccineGroup("PPR", 40, 40, "1 ml S/C"),
        sg.mesha.goatos.feature.submit.VaccineGroup("FMD", 38, 40, "2 ml I/M"),
    ),
    syncState = SyncState.SYNCING,
    syncLabel = "",
    submitLabel = "Submit",
    canSubmit = true,
    syncProgress = 0.66f,
    goatProofTotal = 40,
    goatProofSynced = 38,
    goatProofUploading = 2,
    attemptCount = 0,
    maxAttempts = 0,
    lastError = null,
    isLoadingTask = false,
    isNoTaskAssigned = false,
    isTaskLoadFailed = false,
    isQueueFailed = false,
    isRetryFailed = false,
)

// Mock-exact snapshot of mock/vaccination-mobile-mock.html #v-dhome (Director role, "All
// parks" scope): hero %, both KPI tiles, every SHEDS[] row rendered by renderTodaySheds(),
// every vaxr backlog row, the one needsdec row, and the CBE/CPT coverage-by-park rows from
// the scope sheet.
fun sampleLeadershipState(): LeadershipUiState = LeadershipUiState(
    eyebrow = "Vaccination · Director",
    title = "Overview",
    avatarInitial = "A",
    hero = CoverageHeroState(
        coverageLabel = "Dose coverage · all vaccines",
        coveragePercent = 48,
        dosesLine = "4,650 of 9,698 scheduled doses given",
        dosesTrend = listOf(12f, 18f, 15f, 24f, 30f, 28f, 39f, 46f, 48f),
        scopePill = ScopePill("All parks · 2"),
        animalsLabel = "1,312 animals",
        dataGapPill = DataGapPill("no data gaps", hasGaps = false),
    ),
    kpis = listOf(
        KpiTile("given", "4,650", "Doses given · tap", Tone.OK),
        KpiTile("pending", "5,048", "Pending · tap", Tone.WARN),
    ),
    todayShedsTitle = "Today's sheds · live",
    todaySheds = listOf(
        ShedSummary(
            shedId = "gandhi1", name = "Gandhi 1", park = "CBE", cohort = "Milking does",
            coveragePercent = 0, done = 0, total = 11,
            vaccineGroups = listOf(
                VaccineGroupChip("FMD + HS", 0, 8),
                VaccineGroupChip("ET + TT", 0, 3),
            ),
            assignLabel = "Assign team",
        ),
        ShedSummary(
            shedId = "castro1", name = "Castro 1", park = "CBE", cohort = "Breeding does",
            coveragePercent = 35, done = 6, total = 17,
            vaccineGroups = listOf(
                VaccineGroupChip("PPR · Booster", 6, 12),
                VaccineGroupChip("Goat Pox", 0, 5),
            ),
            assignLabel = "Assign team",
        ),
        ShedSummary(
            shedId = "mandela1", name = "Mandela 1", park = "CBE", cohort = "K2 kids",
            coveragePercent = 100, done = 40, total = 40,
            vaccineGroups = listOf(VaccineGroupChip("FMD + HS", 40, 40)),
            assignLabel = "Assign team",
        ),
        ShedSummary(
            shedId = "sumathi1", name = "Sumathi 1", park = "CBE", cohort = "Pregnant does",
            coveragePercent = 0, done = 0, total = 9,
            vaccineGroups = listOf(VaccineGroupChip("ET + TT · Booster", 0, 9)),
            assignLabel = "Assign team",
        ),
        ShedSummary(
            shedId = "sumathi2", name = "Sumathi 2", park = "CPT", cohort = "Yearling does",
            coveragePercent = 0, done = 0, total = 38,
            vaccineGroups = listOf(VaccineGroupChip("PPR · Booster", 0, 38)),
            assignLabel = "Assign team",
        ),
        ShedSummary(
            shedId = "godel1", name = "Godel 1", park = "CPT", cohort = "Dry does",
            coveragePercent = 0, done = 0, total = 16,
            vaccineGroups = listOf(
                VaccineGroupChip("Goat Pox", 0, 10),
                VaccineGroupChip("HS", 0, 6),
            ),
            assignLabel = "Assign team",
        ),
    ),
    backlogTitle = "Backlog by vaccine · pending doses",
    backlog = listOf(
        BacklogRow("PPR", "largest backlog", "989", Tone.DANGER, 25),
        BacklogRow("FMD", "first + booster", "1,406", Tone.DANGER, 46),
        BacklogRow("ET + TT", "first + booster", "726", Tone.WARN, 72),
        BacklogRow("HS", null, "542", Tone.WARN, 59),
        BacklogRow("Goat Pox", "goat-only", "417", Tone.WARN, 47),
        BacklogRow("Sheep Pox", "sheep-only", "447", Tone.DANGER, 14),
        BacklogRow("Blue Tongue", "sheep-only", "521", Tone.DANGER, 0),
    ),
    needsDecisionTitle = "Needs a decision",
    needsDecision = listOf(
        DecisionRow("d1", "Goat Pox · overdue", "Yashoda 5 · 5 days late", "Reschedule", Tone.DANGER),
    ),
    coverageByParkTitle = "Coverage by park",
    coverageByPark = listOf(
        ParkCoverageRow("CBE", "Coimbatore", "716 animals · dose coverage", "52%", Tone.OK),
        ParkCoverageRow("CPT", "Channapatna", "596 animals · dose coverage", "42%", Tone.WARN),
    ),
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
    detailStatus = RfidDetailStatus.READY,
    connectionState = RfidConnectionState.CONNECTED,
    readerName = "Chainway R3",
    pairedLabel = "Paired · battery 84%",
)

// Mock-exact snapshot of mock/vaccination-mobile-mock.html #v-alerts `.notif` cards.
fun sampleAlertsState(): AlertsUiState = AlertsUiState(
    title = "Alerts",
    rows = listOf(
        AlertRow(
            id = "a1",
            title = "Vaccination drive in 2 days",
            body = "FMD + HS · Thu 9 Jul · Sheds Gandhi 1, Castro 1, Mandela 1 · 118 animals. Confirm stock & staffing.",
            timeLabel = "now",
            tone = AlertTone.INFO,
            unread = true,
            channels = listOf(
                AlertChannel("Call", AlertChannelTone.SENT),
                AlertChannel("Push", AlertChannelTone.PENDING),
                AlertChannel("Slack", AlertChannelTone.TEAL),
                AlertChannel("Email", AlertChannelTone.PENDING),
            ),
        ),
        AlertRow(
            id = "a2",
            title = "Submit today's drive",
            body = "Shed Gandhi 1 · 50/50 done but not submitted. Reminder call in 30 min if not sent.",
            timeLabel = "8:00",
            tone = AlertTone.WARN,
            unread = true,
        ),
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
        SettingRow(SettingKind.TIMETABLE, "Timetable", subtitle = "Shift roster (read-only)"),
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
            scopePill = null,
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

// MOB-005: Submit's loading / no-task / task-load-failed states previously fell back to
// sampleSubmitState() directly, leaking "Gandhi 1" / "Milking does" / "Thu 9 Jul" farm identity
// onto a medical recording screen whenever the real task hadn't loaded (or failed to). Only
// stable chrome (eyebrow/title/submit label) survives here — every operational identity field
// is cleared, matching every other honest placeholder above.
fun submitPlaceholder(): SubmitUiState = sampleSubmitState().copy(
    shed = "",
    cohort = "",
    date = "",
    groups = emptyList(),
    formRunner = null,
    syncState = SyncState.DRAFT,
    syncLabel = "",
    canSubmit = false,
    syncProgress = 0f,
    attemptCount = 0,
    maxAttempts = 0,
    lastError = null,
    isLoadingTask = false,
    isNoTaskAssigned = false,
    isTaskLoadFailed = false,
    isQueueFailed = false,
    isRetryFailed = false,
)

// Timetable (HRMS shift roster, docs/hr/roster-rbac-design.md) — mirrors the mock's People ->
// Timetable rows (mock/goatos-dashboard-mock.html data-sub="timetable"), minus the shift-TIME
// field the EnrichedPosition contract does not expose (see TimetableViewModel's KDoc). The
// seat holder's real name IS modeled (person_display_name) — never a raw UUID fragment.
fun sampleTimetableState(): TimetableUiState = TimetableUiState(
    title = "Timetable",
    rows = listOf(
        TimetableRow(
            id = "p1",
            positionLabel = "Feeding AM1",
            tier = PositionTier.ASSISTANT,
            holderName = "Arun Kumar",
            weekOffLabel = "Mon",
            backupLabel = "Backup AM1",
            statusLabel = "Active",
            isActive = true,
        ),
        TimetableRow(
            id = "p2",
            positionLabel = "Health/Kidding AM1",
            tier = PositionTier.ASSISTANT,
            holderName = "Priya S",
            weekOffLabel = "Wed",
            backupLabel = "Backup AM2",
            statusLabel = "Active",
            isActive = true,
        ),
        TimetableRow(
            id = "p3",
            positionLabel = "Preventive Care Manager",
            tier = PositionTier.MANAGER,
            holderName = null,
            weekOffLabel = "—",
            backupLabel = "Backup Manager",
            statusLabel = "Active",
            isActive = true,
        ),
    ),
)

// Calendar + coverage banner demo state — a separate sample (not folded into
// sampleCalendarState()) so the existing "calendar" Paparazzi golden is untouched; only the
// dedicated "calendar_coverage_banner" test uses this one.
fun sampleCalendarWithCoverageState(): CalendarUiState = sampleCalendarState().copy(
    coverageBanner = CoverageBannerUiState(text = "Covering Vaccination until Fri 11 Jul"),
)

/** Mock-accurate drive form (mirrors the backend `goatos.sop-form.v1` fixture): a vaccine-lot
 *  picker, cold-chain toggle, goat scan, dose, and the administration video — video not yet
 *  captured, so submit is blocked with the reason. Shared by the runner Paparazzi golden. */
fun sampleFormRunnerState(): FormRunnerState = FormRunnerState(
    title = "Record drive",
    subtitle = "Gandhi 1 · CBE · FMD + HS",
    fields = listOf(
        FormFieldUi("vaccine_lot_id", "Vaccine lot", FieldKindUi.PICKER, required = true, selectedLabel = "FMD-2026-014"),
        FormFieldUi("cold_chain_verified", "Cold chain verified", FieldKindUi.BOOLEAN, required = true, checked = true),
        FormFieldUi("goat_ids", "Goats", FieldKindUi.GOAT_SCAN, required = true, scannedCount = 11),
        FormFieldUi("dose_ml_given", "Dose (ml)", FieldKindUi.NUMBER, required = true, text = "2"),
        FormFieldUi("administration_video", "Administration video", FieldKindUi.VIDEO_PROOF, required = true, proofCaptured = false),
    ),
    submitLabel = "Submit drive",
    blockedReason = "Record the administration video before submitting.",
)
