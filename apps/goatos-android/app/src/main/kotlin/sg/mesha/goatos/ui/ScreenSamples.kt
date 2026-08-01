package sg.mesha.goatos.ui

import sg.mesha.goatos.feature.calendar.CalendarItem
import sg.mesha.goatos.feature.calendar.CalendarSegment
import sg.mesha.goatos.feature.calendar.CalendarSegmentKind
import sg.mesha.goatos.feature.calendar.CalendarTone
import sg.mesha.goatos.feature.calendar.CalendarUiState
import sg.mesha.goatos.feature.calendar.CalendarWeekDay
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
import sg.mesha.goatos.feature.sheds.ShedDayTab
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
import sg.mesha.goatos.feature.weighing.WeighingAssignmentUiRow
import sg.mesha.goatos.feature.weighing.WeighingDraftUiRow
import sg.mesha.goatos.feature.weighing.WeighingRosterUiRow
import sg.mesha.goatos.feature.weighing.WeighingUiState

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
    ),
    selectedSegmentId = "week",
    // Calendar chips must stay readable on real phones; use short weekday labels,
    // never one-letter narrow labels that collapse Tue/Thu and Sat/Sun.
    weekDays = listOf(
        CalendarWeekDay("d6", "Mon", "6", "", hasWork = false),
        CalendarWeekDay("d7", "Tue", "7", "", hasWork = true, isToday = true, isSelected = true),
        CalendarWeekDay("d8", "Wed", "8", "", hasWork = true),
        CalendarWeekDay("d9", "Thu", "9", "", hasWork = false),
        CalendarWeekDay("d10", "Fri", "10", "", hasWork = true),
        CalendarWeekDay("d11", "Sat", "11", "", hasWork = false),
        CalendarWeekDay("d12", "Sun", "12", "", hasWork = false),
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
)

fun sampleShedsState(): ShedsUiState = ShedsUiState(
    moduleLabel = "Vaccination",
    scopeLabel = "",
    title = "Vaccination sheds",
    date = "Today · Wed 22 Jul",
    window = "Wed 22 Jul → Tue 28 Jul",
    shedCountLabel = "20 sheds",
    dueLabel = "147 due",
    dayProgressLabel = "42%",
    dayProgressFraction = 0.42f,
    daySummary = "54 / 147 done",
    shedCount = 20,
    dueCount = 93,
    doneCount = 54,
    dayTabs = listOf(
        ShedDayTab("2026-07-22", "WED", "22", "147", isSelected = true),
        ShedDayTab("2026-07-23", "THU", "23", "100", isSelected = false),
        ShedDayTab("2026-07-24", "FRI", "24", "102", isSelected = false),
        ShedDayTab("2026-07-25", "SAT", "25", "70", isSelected = false),
        ShedDayTab("2026-07-26", "SUN", "26", "101", isSelected = false),
        ShedDayTab("2026-07-27", "MON", "27", "81", isSelected = false),
        ShedDayTab("2026-07-28", "TUE", "28", "97", isSelected = false),
    ),
    rows = listOf(
        ShedRow(
            id = "s1", name = "Gandhi 1", animalStage = "Adult",
            operatorName = "Amit Kumar", physicalShed = "Gandhi", partition = "1",
            status = ShedStatus.DONE, statusLabel = "Done",
            vaccineGroups = listOf(sg.mesha.goatos.feature.sheds.VaccineGroup("ET+TT · Dose 2", "40/40", true)),
            inShed = "40", due = "0", done = "40", progressLabel = "100%", progressFraction = 1f,
        ),
        ShedRow(
            id = "s2", name = "Sumathi 1", animalStage = "Adult",
            operatorName = "Darshan Talwar", physicalShed = "Sumathi", partition = "1",
            status = ShedStatus.DELAYED, statusLabel = "Delayed · not started",
            vaccineGroups = listOf(sg.mesha.goatos.feature.sheds.VaccineGroup("Blue Tongue", "0/32", false)),
            inShed = "32", due = "32", done = "0", progressLabel = "0%", progressFraction = 0f,
            actionLabel = "Chase the team ›",
        ),
    ),
)

fun sampleScanState(): ScanUiState = ScanUiState(
    shedLabel = "Vaccination · Gandhi 1",
    cohortLabel = "Gandhi 1 Scan",
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

/**
 * The weighing PLANNER surface as this screen still owns it: the "Plan" header and no operator
 * work list. The week strip, park card, shed/category picker and operator vocabulary moved to the
 * planner surface (WeighingTasksScreen + the create wizard), so this sample no longer carries
 * fixture data for them.
 */
fun sampleWeighingPlanState(): WeighingUiState = WeighingUiState(
    plannerMode = true,
)

fun sampleWeighingOperatorState(): WeighingUiState = WeighingUiState(
    title = "Gandhi 1",
    scopeLabel = "Weighing · Week 31 · Individual",
    hasScope = true,
    category = "individual_animal",
    totalExpected = 78,
    selectedAnimalId = "goat-078",
    selectedAnimalLabel = "RFID 004821 · Kid 078",
    scanInput = "RFID004821",
    weightInput = "18.4",
    individualDrafts = listOf(
        WeighingDraftUiRow("draft-1", "goat-078", "RFID 004821 · 18.4 kg", proofReady = true, readyToSubmit = true),
        WeighingDraftUiRow("draft-2", "goat-079", "RFID 004839 · 17.9 kg", proofReady = false, readyToSubmit = false),
    ),
    visibleRows = listOf(
        WeighingRosterUiRow(
            id = "row-1",
            animalId = "goat-078",
            displayAnimalId = "RFID 004821",
            expectedLocationLabel = "Gandhi 1",
            actualLocationLabel = "Gandhi 1",
            status = "Accepted",
            availabilityStatus = null,
            wrongShed = false,
        ),
        WeighingRosterUiRow(
            id = "row-2",
            animalId = "goat-079",
            displayAnimalId = "RFID 004839",
            expectedLocationLabel = "Gandhi 1",
            actualLocationLabel = "Castro 2",
            status = "Wrong shed",
            availabilityStatus = "Available",
            wrongShed = true,
        ),
        WeighingRosterUiRow(
            id = "row-3",
            animalId = "goat-080",
            displayAnimalId = "RFID 004847",
            expectedLocationLabel = "Gandhi 1",
            actualLocationLabel = null,
            status = "Pending",
            availabilityStatus = "Unavailable",
            wrongShed = false,
        ),
        WeighingRosterUiRow(
            id = "row-4",
            animalId = "goat-081",
            displayAnimalId = "RFID 005001",
            expectedLocationLabel = "Gandhi 1",
            actualLocationLabel = "Gandhi 1",
            status = "Pending",
            availabilityStatus = null,
            wrongShed = false,
        ),
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
    proofSummaryTitle = "Goat camera proof",
    proofSummarySyncedLabel = "38 of 40 goats synced",
    proofSummaryFinalizeHint = "Finalize checks the synced records. It does not upload files.",
    proofTotal = 40,
    proofSynced = 38,
    proofUploading = 2,
    attemptCount = 0,
    maxAttempts = 0,
    lastError = null,
    isLoadingTask = false,
    isNoTaskAssigned = false,
    isTaskLoadFailed = false,
    isQueueFailed = false,
    isRetryFailed = false,
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
        SettingRow(SettingKind.SIGN_OUT, "Sign out"),
    ),
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
        weekEmptyLabel = message,
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

// MOB-005: Submit's loading / no-task / task-load-failed states previously fell back to
// sampleSubmitState() directly, leaking "Gandhi 1" / "Milking does" / "Thu 9 Jul" farm identity
// and sample proof counters onto a medical recording screen whenever the real task hadn't loaded
// (or failed to). Only stable chrome survives here — every operational identity/count field is
// cleared, matching every other honest placeholder above.
fun submitPlaceholder(): SubmitUiState = sampleSubmitState().copy(
    eyebrow = "",
    title = "",
    shed = "",
    cohort = "",
    date = "",
    groups = emptyList(),
    formRunner = null,
    syncState = SyncState.DRAFT,
    syncLabel = "",
    canSubmit = false,
    syncProgress = 0f,
    proofSummaryTitle = "",
    proofSummarySyncedLabel = "",
    proofSummaryFinalizeHint = "",
    proofTotal = 0,
    proofSynced = 0,
    proofUploading = 0,
    proofFailed = 0,
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
