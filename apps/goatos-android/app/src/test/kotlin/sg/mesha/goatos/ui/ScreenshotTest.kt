package sg.mesha.goatos.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import app.cash.paparazzi.DeviceConfig
import app.cash.paparazzi.Paparazzi
import org.junit.Rule
import org.junit.Test
import sg.mesha.goatos.core.designsystem.locale.ProvideAppLocale
import sg.mesha.goatos.core.designsystem.nav.LocalIsTopLevelRoot
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.feature.auth.LoginScreen
import sg.mesha.goatos.feature.calendar.CalendarScreen
import sg.mesha.goatos.feature.profile.AlertsScreen
import sg.mesha.goatos.feature.profile.ProfileScreen
import sg.mesha.goatos.feature.profile.RfidScreen
import sg.mesha.goatos.feature.record.RecordScreen
import sg.mesha.goatos.feature.sheds.ShedsScreen
import sg.mesha.goatos.feature.submit.FormRunner
import sg.mesha.goatos.feature.submit.SubmitScreen
import sg.mesha.goatos.feature.scan.ScanScreen
import sg.mesha.goatos.feature.scan.ScanListSheet
import sg.mesha.goatos.feature.scan.ProofUploadStatus
import sg.mesha.goatos.feature.scan.RosterRow
import sg.mesha.goatos.feature.scan.ScanStatus
import sg.mesha.goatos.feature.timetable.TimetableScreen
import sg.mesha.goatos.feature.verify.VerificationQueueRow
import sg.mesha.goatos.feature.verify.VerifyCategoryOption
import sg.mesha.goatos.feature.verify.VerifyStatusOption
import sg.mesha.goatos.feature.verify.VerifyQueueScreen
import sg.mesha.goatos.feature.verify.VerifyQueueUiState
import sg.mesha.goatos.feature.verify.VerifyTone
import sg.mesha.goatos.feature.weighing.WeighingScreen
import androidx.compose.runtime.CompositionLocalProvider
import androidx.paging.PagingData
import androidx.paging.compose.collectAsLazyPagingItems
import kotlinx.coroutines.flow.flowOf
import sg.mesha.goatos.feature.counts.ApprovalRowUi
import sg.mesha.goatos.feature.counts.ApprovalScreen
import sg.mesha.goatos.feature.counts.ApprovalUiState
import sg.mesha.goatos.feature.counts.BIRTH_ID_KIND_PERMANENT
import sg.mesha.goatos.feature.counts.BIRTH_ID_KIND_TEMPORARY
import sg.mesha.goatos.feature.counts.BirthDeathField
import sg.mesha.goatos.feature.counts.BirthDeathMode
import sg.mesha.goatos.feature.counts.BirthDeathScreen
import sg.mesha.goatos.feature.counts.BirthDeathUiState
import sg.mesha.goatos.feature.counts.CountsFilterOptionUi
import sg.mesha.goatos.feature.counts.MilkPreparationCardBucket
import sg.mesha.goatos.feature.counts.MilkPreparationCardUi
import sg.mesha.goatos.feature.counts.MilkPreparationChipUi
import sg.mesha.goatos.feature.counts.MilkPreparationListScreen
import sg.mesha.goatos.feature.counts.MilkPreparationListUiState
import sg.mesha.goatos.feature.counts.MilkPreparationScreen
import sg.mesha.goatos.feature.counts.MilkPreparationStepUi
import sg.mesha.goatos.feature.counts.MilkPreparationUiState
import sg.mesha.goatos.feature.counts.RfidPromoteField
import sg.mesha.goatos.feature.counts.RfidPromoteScreen
import sg.mesha.goatos.feature.counts.RfidPromoteUiState
import sg.mesha.goatos.feature.counts.ShiftingExecuteAnimalUi
import sg.mesha.goatos.feature.counts.ShiftingExecuteScreen
import sg.mesha.goatos.feature.counts.ShiftingExecuteUiState
import sg.mesha.goatos.feature.counts.ShiftingActionsScreen
import sg.mesha.goatos.feature.counts.ShiftingPendingRowUi
import sg.mesha.goatos.feature.counts.ShiftingPendingStatusUi
import sg.mesha.goatos.feature.counts.ShiftingPendingUiState
import sg.mesha.goatos.feature.counts.ShiftingStateTone
import sg.mesha.goatos.feature.counts.ShiftingPreviousDateUi
import sg.mesha.goatos.feature.counts.ShiftingAnimalUi
import sg.mesha.goatos.feature.counts.ShiftingParkUi
import sg.mesha.goatos.feature.counts.ShiftingScreen
import sg.mesha.goatos.feature.counts.ShiftingShedUi
import sg.mesha.goatos.feature.counts.ShiftingUiState
import sg.mesha.goatos.feature.counts.WorkflowActionSection
import sg.mesha.goatos.feature.counts.WorkflowActionUi
import sg.mesha.goatos.feature.counts.WorkflowCardBucket
import sg.mesha.goatos.feature.counts.WorkflowCardUi
import sg.mesha.goatos.feature.counts.WorkflowChipUi
import sg.mesha.goatos.feature.counts.WorkflowDetailScreen
import sg.mesha.goatos.feature.counts.WorkflowDetailUiState
import sg.mesha.goatos.feature.counts.WorkflowListScreen
import sg.mesha.goatos.feature.counts.WorkflowListUiState
import sg.mesha.goatos.feature.counts.WorkflowModuleUi
import sg.mesha.goatos.feature.counts.WorkflowOverdueDateUi
import sg.mesha.goatos.feature.counts.WorkflowStatusTone
import sg.mesha.goatos.feature.feed.FeedDirectionRowUi
import sg.mesha.goatos.feature.feed.FeedDirectionScreen
import sg.mesha.goatos.feature.feed.FeedDirectionUiState
import sg.mesha.goatos.feature.feed.FeedDistributionCompleteScreen
import sg.mesha.goatos.feature.feed.FeedDistributionUiState
import sg.mesha.goatos.feature.feed.FeedDropdownOption
import sg.mesha.goatos.feature.feed.FeedFilterUi
import sg.mesha.goatos.feature.feed.FeedItemQtyUi
import sg.mesha.goatos.feature.feed.FeedPackingRowUi
import sg.mesha.goatos.feature.feed.FeedPackingScreen
import sg.mesha.goatos.feature.feed.FeedPackingUiState
import sg.mesha.goatos.feature.feed.FeedTransportCaptureScreen
import sg.mesha.goatos.feature.feed.FeedTransportCaptureUiState
import sg.mesha.goatos.feature.feed.FeedTransportFilterUi
import sg.mesha.goatos.feature.feed.FeedTransportRowUi
import sg.mesha.goatos.feature.feed.FeedTransportScreen
import sg.mesha.goatos.feature.feed.FeedTransportUiState

/**
 * Screenshot tests for every screen in the gallery (item 7). Each test renders the EXACT same
 * screen + mock-accurate sample state GalleryActivity uses for manual device capture
 * (adb shell am start -n sg.mesha.goatos.dev/sg.mesha.goatos.GalleryActivity -e screen <name>),
 * so there is one source of truth for "what does mock-accurate data look like" (ScreenSamples.kt)
 * shared by manual device QA and this automated Paparazzi suite.
 *
 * Run: ./gradlew :app:recordPaparazziDevDebug   (first run / after an intentional UI change —
 *   writes golden PNGs to app/src/test/snapshots/images)
 *      ./gradlew :app:verifyPaparazziDevDebug   (CI — fails on any pixel diff from the goldens)
 *
 * Gallery: tools/android/build-screenshot-gallery.py assembles every golden PNG produced here
 * into a single static HTML page for GitHub Pages / CI artifact review.
 */
class ScreenshotTest {

    @get:Rule
    val paparazzi = Paparazzi(deviceConfig = DeviceConfig.PIXEL_6)

    private fun shot(name: String, content: @androidx.compose.runtime.Composable () -> Unit) {
        paparazzi.snapshot(name = name) {
            GoatOsTheme {
                ProvideAppLocale {
                    Box(androidx.compose.ui.Modifier.fillMaxSize().background(MeshaColors.Bg)) {
                        content()
                    }
                }
            }
        }
    }

    @Test
    fun login() = shot("login") { LoginScreen(onSignInEmail = { _, _ -> }, onGoogle = {}, onForgotPassword = {}) }

    @Test
    fun calendar() = shot("calendar") { CalendarScreen(state = sampleCalendarState()) }

    @Test
    fun sheds() = shot("sheds") {
        CompositionLocalProvider(LocalIsTopLevelRoot provides true) {
            ShedsScreen(state = sampleShedsState())
        }
    }

    @Test
    fun vaccination_sheds_initial_loading_draws_nothing() = shot("vaccination_sheds_initial_loading_draws_nothing") {
        CompositionLocalProvider(LocalIsTopLevelRoot provides true) {
            ShedsScreen(
                state = sampleShedsState().copy(
                    date = "",
                    window = "",
                    shedCountLabel = "",
                    dueLabel = "",
                    dayProgressLabel = "",
                    dayProgressFraction = 0f,
                    daySummary = "",
                    shedCount = 0,
                    dueCount = 0,
                    doneCount = 0,
                    caption = "Loading…",
                    rows = emptyList(),
                    rosterChanges = emptyList(),
                    kernelInfo = null,
                    lastSyncedAt = null,
                    isInitialLoading = true,
                    isRefreshing = true,
                ),
            )
        }
    }

    @Test
    fun scan() = shot("scan") { ScanScreen(state = sampleScanState()) }

    @Test
    fun weighing_plan() = shot("weighing_plan") { WeighingScreen(state = sampleWeighingPlanState()) }

    @Test
    fun weighing_operator_capture() = shot("weighing_operator_capture") {
        WeighingScreen(state = sampleWeighingOperatorState())
    }

    @Test
    fun vaccination_scan_loading_state() = shot("vaccination_scan_loading_state") {
        ScanScreen(
            state = sampleScanState().copy(
                shedLabel = "",
                cohortLabel = "",
                listTitle = "",
                submitLabel = "",
                feed = emptyList(),
                roster = emptyList(),
                lastSyncedAt = null,
                isRefreshing = true,
                scanEnabled = false,
                canSubmit = false,
            ),
        )
    }

    @Test
    fun submit() = shot("submit") { SubmitScreen(state = sampleSubmitState()) }

    @Test
    fun vaccination_submit_unanswered_draft_has_no_empty_sync_banner() =
        shot("vaccination_submit_unanswered_draft_has_no_empty_sync_banner") {
            SubmitScreen(
                state = sampleSubmitState().copy(
                    syncState = sg.mesha.goatos.feature.submit.SyncState.DRAFT,
                    canSubmit = false,
                    syncLabel = "",
                    groups = emptyList(),
                    proofSummaryTitle = "",
                    proofSummarySyncedLabel = "",
                    proofSummaryFinalizeHint = "",
                    proofTotal = 0,
                ),
            )
        }

    @Test
    fun vaccination_shed_completion_summary() = shot("vaccination_shed_completion_summary") {
        // Read-only shed acknowledgement summary (no manual medical form): human shed + drive
        // name, expected/handled/proof-ready counts, per-vaccine breakdown, and the Submit button
        // gated on backend submit_enabled.
        SubmitScreen(
            state = sampleSubmitState().copy(
                syncState = sg.mesha.goatos.feature.submit.SyncState.DRAFT,
                syncLabel = "",
                shed = "",
                cohort = "",
                date = "",
                summaryItems = emptyList(),
                groups = emptyList(),
                proofSummaryTitle = "",
                proofSummarySyncedLabel = "",
                proofSummaryFinalizeHint = "",
                proofTotal = 0,
                formRunner = null,
                submitLabel = "Submit",
                canSubmit = true,
                shedCompletionSummary = sg.mesha.goatos.feature.submit.ShedCompletionSummary(
                    taskId = "task-shed",
                    shedName = "Shed A — Weaners",
                    driveName = "Vaccination · July 2026",
                    expectedCount = 50,
                    handledCount = 50,
                    proofReadyCount = 1,
                    proofMode = "shed_level_video",
                    submitState = "draft",
                ),
                vaccineBreakdown = listOf(
                    sg.mesha.goatos.feature.submit.VaccineSummaryItem("FMD", 50),
                    sg.mesha.goatos.feature.submit.VaccineSummaryItem("PPR", 48),
                ),
                blockingReason = null,
            ),
        )
    }

    @Test
    fun record() = shot("record") { RecordScreen(state = sampleRecordState()) }

    @Test
    fun rfid() = shot("rfid") { RfidScreen(state = sampleRfidState()) }

    @Test
    fun alerts() = shot("alerts") { AlertsScreen(state = sampleAlertsState()) }

    @Test
    fun you() = shot("you") { ProfileScreen(state = sampleProfileState()) }

    @Test
    fun timetable() = shot("timetable") { TimetableScreen(state = sampleTimetableState()) }

    @Test
    fun calendar_coverage_banner() = shot("calendar_coverage_banner") {
        CalendarScreen(state = sampleCalendarWithCoverageState())
    }

    @Test
    fun feed_distribution_capture_reference() = shot("feed_distribution_capture_reference") {
        FeedDistributionCompleteScreen(
            state = FeedDistributionUiState(
                shedLabel = "Gandhi 1",
                sessionLabel = "Morning session",
                workflowLabel = "Normal",
            ),
        )
    }

    @Test
    fun feed_transport_capture_matches_distribution_anatomy() =
        shot("feed_transport_capture_matches_distribution_anatomy") {
            FeedTransportCaptureScreen(
                state = FeedTransportCaptureUiState(shedLabel = "Gandhi 1"),
                onEvent = {},
            )
        }

    @Test
    fun feed_transport_task_list_matches_distribution_anatomy() =
        shot("feed_transport_task_list_matches_distribution_anatomy") {
            FeedTransportScreen(
                state = FeedTransportUiState(
                    date = "2026-07-29",
                    today = "2026-07-29",
                    filters = FeedTransportFilterUi(
                        parks = listOf(
                            FeedDropdownOption("park-1", "Channapatna"),
                            FeedDropdownOption("park-2", "Coimbatore"),
                        ),
                        selectedParkId = "park-1",
                        selectedParkLabel = "Channapatna",
                        sheds = listOf(
                            FeedDropdownOption("shed-1", "Gandhi 1"),
                            FeedDropdownOption("shed-2", "Gandhi 2"),
                            FeedDropdownOption("shed-3", "Godel 1"),
                        ),
                    ),
                    rows = listOf(
                        FeedTransportRowUi(
                            taskId = "task-1",
                            parkId = "park-1",
                            shedId = "shed-1",
                            shedLabel = "Gandhi 1",
                            parkLabel = "Channapatna",
                            status = "due",
                            reworkReason = null,
                        ),
                        FeedTransportRowUi(
                            taskId = "task-2",
                            parkId = "park-1",
                            shedId = "shed-2",
                            shedLabel = "Gandhi 2",
                            parkLabel = "Channapatna",
                            status = "verification_due",
                            reworkReason = null,
                        ),
                        FeedTransportRowUi(
                            taskId = "task-3",
                            parkId = "park-1",
                            shedId = "shed-3",
                            shedLabel = "Godel 1",
                            parkLabel = "Channapatna",
                            status = "rework",
                            reworkReason = "Transport path is not visible",
                        ),
                    ),
                ),
                onEvent = {},
            )
        }

    @Test
    fun offline_banner() = shot("offline_banner") { OfflineBanner(visible = true, onOpenDetails = {}) }

    @Test
    fun form_runner() = shot("form_runner") {
        FormRunner(
            state = sampleFormRunnerState(),
            onToggle = { _, _ -> }, onText = { _, _ -> }, onScan = {}, onPick = { _, _ -> }, onCaptureVideo = { _, _ -> }, onSubmit = {},
        )
    }

    @Test
    fun vaccination_scan_row_proof() = shot("vaccination_scan_row_proof") {
        ScanListSheet(
            title = "Gandhi 1 · goat proof",
            rows = listOf(
                RosterRow(
                    primaryTag = "RFID 004821",
                    secondaryTag = "Ear tag 118",
                    vaccineLabel = "PPR · FMD",
                    status = ScanStatus.DONE,
                    goatId = "goat-1",
                    proofClipCount = 2,
                    proofUploadStatus = ProofUploadStatus.SYNCED,
                ),
                RosterRow(
                    primaryTag = "RFID 004839",
                    secondaryTag = "Ear tag 126",
                    vaccineLabel = "PPR",
                    status = ScanStatus.DONE,
                    goatId = "goat-2",
                    proofClipCount = 1,
                    proofUploadStatus = ProofUploadStatus.UPLOADING,
                ),
                RosterRow(
                    primaryTag = "RFID 004847",
                    secondaryTag = "Ear tag 132",
                    vaccineLabel = "Enterotoxaemia · Tetanus toxoid",
                    status = ScanStatus.DONE,
                    goatId = "goat-3",
                    proofClipCount = 0,
                    proofUploadStatus = ProofUploadStatus.MISSING,
                ),
                RosterRow(
                    primaryTag = "RFID 004851",
                    secondaryTag = "Ear tag 137",
                    vaccineLabel = "PPR booster",
                    status = ScanStatus.DONE,
                    goatId = "goat-4",
                    proofClipCount = 1,
                    proofUploadStatus = ProofUploadStatus.FAILED,
                ),
            ),
            captureEnabled = true,
        )
    }

    @Test
    fun vaccination_shed_review() = shot("vaccination_shed_review") {
        SubmitScreen(
            state = sampleSubmitState().copy(
                syncState = sg.mesha.goatos.feature.submit.SyncState.ACKED,
                syncProgress = 1f,
                submitLabel = "Finalize shed",
                proofSummarySyncedLabel = "40 of 40 goats synced",
                proofSynced = 40,
                proofUploading = 0,
            ),
        )
    }

    @Test
    fun vaccination_verifier_queue() = shot("vaccination_verifier_queue") {
        VerifyQueueScreen(
            state = VerifyQueueUiState(
                moduleKey = "vaccination",
                moduleLabel = "Vaccination",
                selectedCategory = "vaccination_proof",
                categoryOptions = listOf(VerifyCategoryOption("vaccination_proof", "Vaccination")),
                statusOptions = listOf(
                    VerifyStatusOption("pending", "Due"),
                    VerifyStatusOption("approved", "Approved"),
                    VerifyStatusOption("rejected", "Rejected"),
                ),
                selectedBusinessDate = "2026-07-30",
                hasMissed = true,
                rows = listOf(
                    VerificationQueueRow(
                        id = "proof-1",
                        category = "vaccination_proof",
                        categoryLabel = "Vaccination",
                        title = "Goat CBE-0412 · Gandhi 1",
                        subtitle = "Coimbatore · 2 camera clips · Arun",
                        statusTone = VerifyTone.PENDING,
                    ),
                    VerificationQueueRow(
                        id = "proof-2",
                        category = "vaccination_proof",
                        categoryLabel = "Vaccination",
                        title = "Goat CBE-0419 · Gandhi 1",
                        subtitle = "Coimbatore · 1 camera clip · Arun",
                        statusTone = VerifyTone.PENDING,
                    ),
                ),
                lastSyncedAt = System.currentTimeMillis(),
            ),
        )
    }

    @Test
    fun weighing_verifier_queue() = shot("weighing_verifier_queue") {
        VerifyQueueScreen(
            state = VerifyQueueUiState(
                moduleKey = "weighing",
                moduleLabel = "Weighing",
                selectedCategory = "weighing_proof",
                categoryOptions = listOf(VerifyCategoryOption("weighing_proof", "Weighing")),
                statusOptions = listOf(
                    VerifyStatusOption("pending", "Due"),
                    VerifyStatusOption("approved", "Approved"),
                    VerifyStatusOption("rejected", "Rejected"),
                ),
                selectedBusinessDate = "2026-07-30",
                rows = listOf(
                    VerificationQueueRow(
                        id = "weighing-1",
                        category = "weighing_proof",
                        categoryLabel = "Weighing",
                        title = "Kid Shed B · 5 animal videos",
                        subtitle = "Coimbatore · Pramod",
                        statusTone = VerifyTone.PENDING,
                    ),
                    VerificationQueueRow(
                        id = "weighing-2",
                        category = "weighing_proof",
                        categoryLabel = "Weighing",
                        title = "Kid Shed C · lump-sum videos",
                        subtitle = "Coimbatore · Pramod",
                        statusTone = VerifyTone.PENDING,
                    ),
                ),
                lastSyncedAt = System.currentTimeMillis(),
            ),
        )
    }

    @Test
    fun shifting_add_locks_current_farm() = shot("shifting_add_locks_current_farm") {
        val animal = ShiftingAnimalUi(
            goatId = "d8337607-6e21-41c9-a703-a7b73ae4e545",
            displayId = "G-000326",
            tag = "CBE-ASSUMED-RFID-00002",
            parkId = "00000000-0000-4000-8000-000000003001",
            shedId = "43071c6e-3b00-47a9-860c-1bbacb570575",
            parkName = "Coimbatore",
            shedName = "Castro 1",
            lifecycleStatus = "alive",
        )
        ShiftingScreen(
            state = ShiftingUiState(
                animalQuery = animal.tag,
                animalMatches = listOf(animal),
                selectedAnimal = animal,
                destinationParks = listOf(
                    ShiftingParkUi(
                        parkId = animal.parkId,
                        name = animal.parkName,
                        sheds = listOf(
                            ShiftingShedUi("43071c6e-3b00-47a9-860c-1bbacb570576", "Castro 2"),
                        ),
                    ),
                ),
                destinationParkId = animal.parkId,
            ),
        )
    }

    @Test
    fun shifting_pending() = shot("shifting_pending") {
        val rows = flowOf(
            PagingData.from(
                listOf(
                    ShiftingPendingRowUi(
                        shiftingEventId = "move-1",
                        sourceLabel = "Shed A · Weaners",
                        destinationLabel = "Shed C · Growers",
                        priority = "High",
                        category = "Growth",
                        animalCount = 12,
                        approvedAtLabel = "2026-07-22",
                        actionStateLabel = "Approved",
                        actionStateTone = ShiftingStateTone.Neutral,
                        primaryActionKey = "execute",
                    ),
                    ShiftingPendingRowUi(
                        shiftingEventId = "move-2",
                        sourceLabel = "Shed B · Kids",
                        destinationLabel = "Shed A · Weaners",
                        priority = "Low",
                        category = "Health",
                        animalCount = 1,
                        approvedAtLabel = "2026-07-22",
                        actionStateLabel = "Completed",
                        actionStateTone = ShiftingStateTone.Done,
                        primaryActionKey = "none",
                    ),
                    // An UNAPPROVED movement (approve-first, maintainer decision 2026-08-09). The
                    // golden had no such row, so the state this screen now spends most of its time
                    // showing a raiser was never rendered: not tappable, no video progress, and the
                    // waiting pill carrying the whole reason the card does not respond to a tap.
                    ShiftingPendingRowUi(
                        shiftingEventId = "move-3",
                        sourceLabel = "Shed A · Weaners",
                        destinationLabel = "Shed B · Kids",
                        priority = "High",
                        category = "Breeding",
                        animalCount = 4,
                        approvedAtLabel = "",
                        actionStateLabel = "Awaiting Park Head approval",
                        actionStateTone = ShiftingStateTone.Waiting,
                        primaryActionKey = "none",
                    ),
                ),
            ),
        ).collectAsLazyPagingItems()
        ShiftingActionsScreen(
            state = ShiftingPendingUiState(
                dateIso = "2026-07-22",
                dateLabel = "Today · 22 Jul",
                statuses = listOf(
                    ShiftingPendingStatusUi("all", "All", true),
                    ShiftingPendingStatusUi("pending", "Pending", false),
                    ShiftingPendingStatusUi("authorized", "Approved", false),
                    ShiftingPendingStatusUi("rework", "Rework", false),
                    ShiftingPendingStatusUi("completed", "Completed", false),
                ),
                previousDates = listOf(
                    ShiftingPreviousDateUi("2026-07-21", "21 Jul", 3),
                    ShiftingPreviousDateUi("2026-07-20", "20 Jul", 1),
                ),
                lastSyncedAt = null,
            ),
            rows = rows,
        )
    }

    /**
     * The breed vocabulary and the park -> shed destination catalog the Record screen actually
     * renders. Both are backend-supplied and Room-cached; leaving them empty (as these fixtures
     * did until now) rendered the form with "Select a breed" and "Select a farm" placeholders and
     * no placement section filled, so the golden showed an EMPTY form rather than the screen an
     * operator sees mid-entry.
     */
    private fun birthBreedOptions() = listOf(
        CountsFilterOptionUi(key = "boer", label = "Boer", count = 412),
        CountsFilterOptionUi(key = "sirohi", label = "Sirohi", count = 288),
        CountsFilterOptionUi(key = "jamnapari", label = "Jamnapari", count = 96),
    )

    private fun birthDestinationParks() = listOf(
        ShiftingParkUi(
            parkId = "park-cpt",
            name = "Channapatna",
            sheds = listOf(
                ShiftingShedUi(
                    shedId = "shed-castro",
                    name = "Castro",
                    partitionLabel = "2",
                    operationalLocationDisplay = "Castro - 2",
                ),
                ShiftingShedUi(
                    shedId = "shed-gandhi-1",
                    name = "Gandhi 1",
                    partitionLabel = null,
                    operationalLocationDisplay = "Gandhi 1",
                ),
            ),
        ),
        ShiftingParkUi(parkId = "park-cbe", name = "Coimbatore", sheds = emptyList()),
    )

    /** Record a birth on the TEMPORARY-tag path, filled in as an operator leaves it before saving. */
    @Test
    fun birth_temporary_tag() = shot("birth_temporary_tag") {
        BirthDeathScreen(
            state = BirthDeathUiState(
                mode = BirthDeathMode.BIRTH,
                idKind = BIRTH_ID_KIND_TEMPORARY,
                tag = "TEMP-42",
                species = "goat",
                sex = "female",
                breed = "boer",
                breedOptions = birthBreedOptions(),
                dob = "2026-08-06",
                entryDate = "2026-08-06",
                destinationParks = birthDestinationParks(),
                parkId = "park-cpt",
                shedId = "shed-castro",
                partitionLabel = "2",
                damId = "982000123456789",
                canSubmit = true,
            ),
        )
    }

    /**
     * Record a DEATH. This path had no golden at all, so the only "Record birth or death" images
     * in the repo were the birth half of a two-mode screen -- the tag/RFID animal search, the
     * selected animal and the reason field were never captured.
     */
    @Test
    fun death_record() = shot("death_record") {
        BirthDeathScreen(
            state = BirthDeathUiState(
                mode = BirthDeathMode.DEATH,
                animalQuery = "982000123456789",
                selectedAnimal = ShiftingAnimalUi(
                    goatId = "goat-1",
                    displayId = "CPT-10199",
                    tag = "982000123456789",
                    parkId = "park-cpt",
                    shedId = "shed-mandela-1",
                    parkName = "Channapatna",
                    shedName = "Mandela 1",
                    partitionLabel = "Part 2",
                    locationLabel = "Mandela 1 - Part 2",
                ),
                reason = "Found down in the morning round; not responding to treatment.",
                entryDate = "2026-08-06",
                canSubmit = true,
            ),
        )
    }

    @Test
    fun milk_preparation_worklist() = shot("milk_preparation_worklist") {
        MilkPreparationListScreen(
            state = MilkPreparationListUiState(
                subtitle = "2 farms · 2 need action",
                dateLabel = "Today · 29 Jul",
                feedingDateLabel = "Tomorrow · 30 Jul",
                chips = listOf(
                    MilkPreparationChipUi("all", "All", 2),
                    MilkPreparationChipUi("to_prepare", "To prepare", 2),
                    MilkPreparationChipUi("in_review", "In review", 0),
                    MilkPreparationChipUi("completed", "Completed", 0),
                    MilkPreparationChipUi("rework", "Rework", 0),
                ),
                cards = listOf(
                    MilkPreparationCardUi("park-cbe", "CBE", "park-cbe", "", 4, 32, "38 L", "209 g", "To prepare", "Prepare", "", MilkPreparationCardBucket.TO_PREPARE),
                    MilkPreparationCardUi("park-cpt", "CPT", "park-cpt", "", 2, 52, "41.6 L", "228.8 g", "To prepare", "Prepare", "", MilkPreparationCardBucket.TO_PREPARE),
                ),
            ),
            onEvent = {},
        )
    }

    @Test
    fun milk_preparation_farm_execution() = shot("milk_preparation_farm_execution") {
        MilkPreparationScreen(
            state = MilkPreparationUiState(
                preparationDate = "Today · 29 Jul",
                feedingDate = "Tomorrow · 30 Jul",
                selectedParkId = "park-1",
                parkLabel = "Channapatna",
                cohortCount = 1,
                headCount = 28,
                totalMilkLabel = "22.4 L",
                citricAcidLabel = "123.2 g",
                steps = listOf(
                    MilkPreparationStepUi("goat_milk_quantity", "Measure goat milk", captured = true),
                    MilkPreparationStepUi("boiling_temperature", "Record boiling temperature"),
                    MilkPreparationStepUi("cooled_temperature", "Record cooled temperature"),
                    MilkPreparationStepUi("uht_milk_quantity", "Measure UHT milk"),
                    MilkPreparationStepUi("citric_acid_mixing", "Mix citric acid"),
                ),
            ),
            onEvent = {},
        )
    }

    /** The permanent-RFID path: both identifiers carry the Bluetooth scan toggle, the primary one
     *  mid-scan (brand-filled chip + listening caption). */
    @Test
    fun birth_permanent_rfid_scanning() = shot("birth_permanent_rfid_scanning") {
        BirthDeathScreen(
            state = BirthDeathUiState(
                mode = BirthDeathMode.BIRTH,
                idKind = BIRTH_ID_KIND_PERMANENT,
                // First tag captured, second field now listening -- a newborn given two ear tags.
                // The old fixture left both blank with nothing selected below, so it showed an
                // empty form rather than the mid-scan state its name promises.
                tag = "982000123456789",
                tag2 = "",
                scanningField = BirthDeathField.TAG2,
                species = "goat",
                sex = "male",
                breed = "sirohi",
                breedOptions = birthBreedOptions(),
                dob = "2026-08-06",
                entryDate = "2026-08-06",
                destinationParks = birthDestinationParks(),
                parkId = "park-cpt",
                shedId = "shed-gandhi-1",
                damId = "982000987654321",
            ),
        )
    }

    /** Birth -> Tag the kid: the temp tag is retired by a scanned (or typed) permanent RFID. */
    @Test
    fun rfid_promote_scanning() = shot("rfid_promote_scanning") {
        RfidPromoteScreen(
            state = RfidPromoteUiState(
                goatId = "g-1",
                loading = false,
                displayId = "G-77",
                temporaryIdentifier = "TEMP-42",
                locationDisplay = "North Park / Shed A",
                rfidInput = "982000123456789",
                scanningField = RfidPromoteField.SECONDARY,
                canSubmit = true,
            ),
        )
    }

    @Test
    fun shifting_execute() = shot("shifting_execute") {
        ShiftingExecuteScreen(
            state = ShiftingExecuteUiState(
                shiftingEventId = "move-1",
                loading = false,
                sourceLabel = "Shed A · Weaners",
                destinationLabel = "Shed C · Growers",
                priority = "High",
                category = "Growth",
                animalCount = 3,
                animals = listOf(
                    ShiftingExecuteAnimalUi("g1", "CBE-0412", "RFID 004821"),
                    ShiftingExecuteAnimalUi("g2", "CBE-0419", "RFID 004839"),
                    ShiftingExecuteAnimalUi("g3", "CBE-0421", null),
                ),
                videoCaptured = true,
                videoMessage = "Video saved on this phone. It will upload automatically.",
                canComplete = true,
            ),
        )
    }

    // -----------------------------------------------------------------------
    // Colostrum (docs/decisions/colostrum-milk-module.md)
    // -----------------------------------------------------------------------

    private fun colostrumCard(
        id: String,
        displayId: String,
        done: Int,
        total: Int,
        next: String,
        due: String,
        overdue: Boolean,
        bucket: WorkflowCardBucket,
        meta: String,
    ) = WorkflowCardUi(
        workflowId = id,
        displayId = displayId,
        roleLabel = "Kid",
        metaLine = meta,
        actionsDone = done,
        actionsTotal = total,
        nextKindLabel = if (bucket == WorkflowCardBucket.COMPLETED) "Done" else "Next",
        nextTitle = next,
        dueLabel = due,
        overdue = overdue,
        bucket = bucket,
    )

    // ---------------------------------------------------------------------------------------
    // Birth and Death LANDING screens.
    //
    // These were the coverage gap behind a real documentation defect: the only birth/death
    // goldens were birth_temporary_tag / birth_permanent_rfid_scanning, which are the ADD FORM
    // at Routes.COUNTS_BIRTH_ADD -- reached only from the ＋ button. The Birth and Death TABS
    // themselves (Routes.COUNTS_BIRTH / COUNTS_DEATH) render WorkflowListDestination, i.e. the
    // per-goat outstanding-action work list from the 2026-07-27 birth/death-workflows decision,
    // and had no golden at all. Anyone reading the snapshot set concluded the tab was the form.
    // ---------------------------------------------------------------------------------------

    private fun workflowCard(
        id: String,
        displayId: String,
        role: String,
        done: Int,
        total: Int,
        next: String,
        due: String,
        overdue: Boolean,
        bucket: WorkflowCardBucket,
        meta: String,
    ) = WorkflowCardUi(
        workflowId = id,
        displayId = displayId,
        roleLabel = role,
        metaLine = meta,
        actionsDone = done,
        actionsTotal = total,
        nextKindLabel = if (bucket == WorkflowCardBucket.COMPLETED) "Done" else "Next",
        nextTitle = next,
        dueLabel = due,
        overdue = overdue,
        bucket = bucket,
    )

    /** The Birth tab as an operator actually lands on it: a work list, with ＋ to record a new one. */
    @Test
    fun birth_work_list() = shot("birth_work_list") {
        val rows = flowOf(
            PagingData.from(
                listOf(
                    workflowCard(
                        id = "b-1", displayId = "CPT-10234", role = "Kid", done = 1, total = 4,
                        next = "Weigh the kid", due = "2h late", overdue = true,
                        bucket = WorkflowCardBucket.OVERDUE, meta = "Born 6 Aug 05:40 · Castro 2 · Boer",
                    ),
                    workflowCard(
                        id = "b-2", displayId = "CPT-10235", role = "Kid", done = 0, total = 4,
                        next = "Fit the ear tag", due = "15:00", overdue = false,
                        bucket = WorkflowCardBucket.DUE, meta = "Born 5 Aug 23:10 · Gandhi 1 · Sirohi",
                    ),
                    workflowCard(
                        id = "b-3", displayId = "CPT-10231", role = "Kid", done = 4, total = 4,
                        next = "All actions done", due = "", overdue = false,
                        bucket = WorkflowCardBucket.COMPLETED, meta = "Born 5 Aug 07:20 · Yashoda · Boer",
                    ),
                ),
            ),
        ).collectAsLazyPagingItems()
        WorkflowListScreen(
            state = WorkflowListUiState(
                module = WorkflowModuleUi.BIRTH,
                subtitle = "3 births to finish",
                dateIso = "2026-08-06",
                dateLabel = "Today · 6 Aug",
                isToday = true,
                chips = listOf(
                    WorkflowChipUi("all", "All", 3),
                    WorkflowChipUi("overdue", "Overdue", 1),
                    WorkflowChipUi("due", "Due", 1),
                    WorkflowChipUi("completed", "Completed", 1),
                ),
                selectedFilter = "all",
            ),
            rows = rows,
        )
    }

    /** The Death tab: same work-list anatomy, different module copy. */
    @Test
    fun death_work_list() = shot("death_work_list") {
        val rows = flowOf(
            PagingData.from(
                listOf(
                    workflowCard(
                        id = "d-1", displayId = "CPT-10199", role = "Adult", done = 1, total = 3,
                        next = "Record the post-mortem note", due = "1d late", overdue = true,
                        bucket = WorkflowCardBucket.OVERDUE, meta = "Died 5 Aug · Mandela 1 · Boer",
                    ),
                    workflowCard(
                        id = "d-2", displayId = "CPT-10204", role = "Adult", done = 2, total = 3,
                        next = "Attach the disposal photo", due = "18:00", overdue = false,
                        bucket = WorkflowCardBucket.DUE, meta = "Died 6 Aug · Godel 1 · Sirohi",
                    ),
                ),
            ),
        ).collectAsLazyPagingItems()
        WorkflowListScreen(
            state = WorkflowListUiState(
                module = WorkflowModuleUi.DEATH,
                subtitle = "2 deaths to finish",
                dateIso = "2026-08-06",
                dateLabel = "Today · 6 Aug",
                isToday = true,
                chips = listOf(
                    WorkflowChipUi("all", "All", 2),
                    WorkflowChipUi("overdue", "Overdue", 1),
                    WorkflowChipUi("due", "Due", 1),
                    WorkflowChipUi("completed", "Completed", 0),
                ),
                selectedFilter = "all",
            ),
            rows = rows,
        )
    }

    // ---------------------------------------------------------------------------------------
    // Feed Direction and Feed Packing landing screens -- the first two tabs of the Feed bar.
    // Only Feed Transport and the two capture screens had goldens, so two thirds of the feed
    // chain was invisible in the snapshot set.
    // ---------------------------------------------------------------------------------------

    private fun feedItems() = listOf(
        FeedItemQtyUi(feedItem = "Maize", quantityKg = "12.4", blocked = false, blockedReason = ""),
        FeedItemQtyUi(feedItem = "Soya", quantityKg = "4.8", blocked = false, blockedReason = ""),
        FeedItemQtyUi(feedItem = "Mineral mix", quantityKg = "0.6", blocked = false, blockedReason = ""),
    )

    private fun feedFilters() = FeedFilterUi(
        parks = listOf(
            FeedDropdownOption("park-1", "Channapatna"),
            FeedDropdownOption("park-2", "Coimbatore"),
        ),
        selectedParkId = "park-1",
        selectedParkLabel = "Channapatna",
        sheds = listOf(
            FeedDropdownOption("shed-1", "Gandhi 1"),
            FeedDropdownOption("shed-2", "Godel 1"),
        ),
    )

    /** The day's dispatch sheet: which shed gets what, and how much. */
    @Test
    fun feed_direction_sheet() = shot("feed_direction_sheet") {
        val rows = flowOf(
            PagingData.from(
                listOf(
                    FeedDirectionRowUi(
                        grainKey = "row-1", parkId = "park-1", shedId = "shed-1", sessionNo = 1,
                        shedLabel = "Gandhi 1", shedTag = "Adult", breed = "Boer",
                        partitionLabel = "",
                        rationGroup = "Milking does", experimentArm = "",
                        sessionLabel = "Morning session", headCount = 40,
                        headCountInformational = false, workflow = "normal",
                        items = feedItems(), sessionTotalKg = "17.8",
                        blocked = false, overduePending = false, completed = false,
                        lifecycleStatus = "pending",
                    ),
                    FeedDirectionRowUi(
                        grainKey = "row-2", parkId = "park-1", shedId = "shed-2", sessionNo = 1,
                        shedLabel = "Godel 1 - Part 3", shedTag = "Kid", breed = "Sirohi",
                        partitionLabel = "Part 3",
                        rationGroup = "Weaners", experimentArm = "",
                        sessionLabel = "Morning session", headCount = 18,
                        headCountInformational = false, workflow = "normal",
                        items = feedItems(), sessionTotalKg = "8.1",
                        blocked = false, overduePending = false, completed = true,
                        lifecycleStatus = "completed",
                    ),
                ),
            ),
        ).collectAsLazyPagingItems()
        FeedDirectionScreen(
            state = FeedDirectionUiState(
                title = "Feed Direction",
                targetDateLabel = "2026-07-29",
                today = "2026-07-29",
                canCapture = true,
                filters = feedFilters(),
            ),
            rows = rows,
        )
    }

    /**
     * The packing worklist: bags to make up today for tomorrow's feed.
     *
     * ONE CARD PER PEN PER SESSION (maintainer decision 2026-08-11, reverting the 2026-08-10 pen-day
     * card). Castro - 2 therefore appears TWICE, Morning and Evening, and that repetition is the
     * shape the golden exists to hold: two bags, two cards, two videos.
     *
     * The session labels are the farm's real authored names. A fixture that asserts a shape the farm
     * does not have is a defect even when it passes -- it teaches every later reader that the shape
     * is real.
     */
    @Test
    fun feed_packing_worklist() = shot("feed_packing_worklist") {
        val rows = flowOf(
            PagingData.from(
                listOf(
                    // Castro - 2's TWO bags, both sent BACK by the afternoon feed correction
                    // (maintainer decision 2026-08-10): animals shifted in after they were packed and
                    // filmed, so the quantities on both cards are no longer the ones the operator
                    // packed to. BOTH sessions are here because head count scales the morning and the
                    // evening ration alike -- reopening only one would leave a bag packed for a head
                    // count the farm no longer has.
                    //
                    // FIRST in the list deliberately: the reopen copy must be ABOVE THE FOLD, or the
                    // golden renders an image that would be byte-identical with that copy deleted and
                    // proves nothing. lifecycleStatus is "pending" -- IDENTICAL to Gandhi 1 below,
                    // which nobody has packed at all -- so the chip cannot tell the two apart and this
                    // golden is what proves an operator can.
                    FeedPackingRowUi(
                        grainKey = "pack-3", parkId = "park-1", shedId = "shed-3", sessionNo = 1,
                        shedLabel = "Castro - 2",
                        partitionLabel = "2", sessionLabel = "Morning",
                        workflow = "normal", experimentArm = "", headCount = 52,
                        items = feedItems(), totalKg = "17.8",
                        status = "ready", completed = false, lifecycleStatus = "pending",
                        reworkReason = "Animals moved in or out of this pen, so the feed " +
                            "quantities changed. Pack the new amounts and record a new video.",
                    ),
                    FeedPackingRowUi(
                        grainKey = "pack-4", parkId = "park-1", shedId = "shed-3", sessionNo = 2,
                        shedLabel = "Castro - 2",
                        partitionLabel = "2", sessionLabel = "Evening",
                        workflow = "normal", experimentArm = "", headCount = 52,
                        items = feedItems(), totalKg = "17.8",
                        status = "ready", completed = false, lifecycleStatus = "pending",
                        reworkReason = "Animals moved in or out of this pen, so the feed " +
                            "quantities changed. Pack the new amounts and record a new video.",
                    ),
                    FeedPackingRowUi(
                        grainKey = "pack-1", parkId = "park-1", shedId = "shed-1", sessionNo = 1,
                        shedLabel = "Gandhi 1",
                        partitionLabel = "", sessionLabel = "Morning",
                        workflow = "normal", experimentArm = "", headCount = 40,
                        items = feedItems(), totalKg = "17.8",
                        status = "ready", completed = false, lifecycleStatus = "pending",
                    ),
                    // A PARTITIONED pen, so the golden also shows shed and pen rendering together.
                    FeedPackingRowUi(
                        grainKey = "pack-2", parkId = "park-1", shedId = "shed-2", sessionNo = 1,
                        shedLabel = "Godel 1 - Part 3",
                        partitionLabel = "Part 3", sessionLabel = "Morning",
                        workflow = "normal", experimentArm = "", headCount = 18,
                        items = feedItems(), totalKg = "8.1",
                        status = "ready", completed = false,
                        lifecycleStatus = "pending_verification",
                    ),
                ),
            ),
        ).collectAsLazyPagingItems()
        FeedPackingScreen(
            state = FeedPackingUiState(
                title = "Feed Packing",
                targetDateLabel = "2026-07-29",
                feedForDateLabel = "2026-07-30",
                today = "2026-07-29",
                canCapture = true,
                filters = feedFilters(),
            ),
            rows = rows,
        )
    }

    /**
     * The birth/death/shifting APPROVAL queue -- the surface a counts_approver or the CEO decides
     * on. It had no golden, so documentation reached for the Shifting ACTIONS list instead, which
     * is a different screen belonging to a different job (the operator carrying a move out).
     *
     * The row copy is backend-composed on purpose: `raisedBy` is a resolved NAME and `summaryLine`
     * already has every id turned into a shed name. Both once rendered raw uuids at an approver,
     * so this golden also pins that the screen shows farm language, never identifiers.
     */
    @Test
    fun approval_queue() = shot("approval_queue") {
        val rows = flowOf(
            PagingData.from(
                listOf(
                    ApprovalRowUi(
                        requestId = "req-1",
                        typeLabel = "Shifting",
                        requestType = "shifting",
                        raisedBy = "Darshan Talwar",
                        raisedAt = "Today · 09:12",
                        summaryLine = "12 animals · Gandhi 1 → Gandhi 2 · Routine",
                    ),
                    ApprovalRowUi(
                        requestId = "req-2",
                        typeLabel = "Birth",
                        requestType = "birth",
                        raisedBy = "Amit Kumar",
                        raisedAt = "Today · 07:40",
                        summaryLine = "1 kid · Castro 2 · Boer · Female",
                    ),
                    ApprovalRowUi(
                        requestId = "req-3",
                        typeLabel = "Death",
                        requestType = "death",
                        raisedBy = "Sagar Mahoor",
                        raisedAt = "Yesterday · 17:05",
                        summaryLine = "1 adult · Mandela 1 · Sirohi",
                    ),
                ),
            ),
        ).collectAsLazyPagingItems()
        ApprovalScreen(state = ApprovalUiState(), rows = rows)
    }

    /** The Colostrum work list: bell + date bar + day-scoped chips, and NO + button. */
    @Test
    fun colostrumList() = shot("colostrum-list") {
        val rows = flowOf(
            PagingData.from(
                listOf(
                    colostrumCard(
                        id = "wf-1", displayId = "CPT-10234", done = 2, total = 5,
                        next = "4th Colostrum", due = "2h late", overdue = true,
                        bucket = WorkflowCardBucket.OVERDUE, meta = "Born 6 Aug 05:40 · Castro 2 · Boer",
                    ),
                    colostrumCard(
                        id = "wf-2", displayId = "CPT-10235", done = 0, total = 5,
                        next = "6th Colostrum", due = "15:00", overdue = false,
                        bucket = WorkflowCardBucket.DUE, meta = "Born 5 Aug 23:10 · Gandhi 1 · Sirohi",
                    ),
                    colostrumCard(
                        id = "wf-3", displayId = "CPT-10231", done = 5, total = 5,
                        next = "All feeds done", due = "", overdue = false,
                        bucket = WorkflowCardBucket.COMPLETED, meta = "Born 5 Aug 07:20 · Yashoda · Boer",
                    ),
                ),
            ),
        ).collectAsLazyPagingItems()
        WorkflowListScreen(
            state = WorkflowListUiState(
                module = WorkflowModuleUi.COLOSTRUM,
                subtitle = "3 kids to feed",
                dateIso = "2026-08-06",
                dateLabel = "Today · 6 Aug",
                isToday = true,
                chips = listOf(
                    WorkflowChipUi("all", "All", 3),
                    WorkflowChipUi("overdue", "Overdue", 1),
                    WorkflowChipUi("due", "Due", 1),
                    WorkflowChipUi("completed", "Completed", 1),
                ),
                overdueDates = listOf(
                    WorkflowOverdueDateUi("2026-08-05", "Wed, 5 Aug", 2),
                    WorkflowOverdueDateUi("2026-08-04", "Tue, 4 Aug", 1),
                ),
                selectedFilter = "all",
                lastSyncedAt = null,
            ),
            rows = rows,
        )
    }

    /** A past day reached through the date bar: the amber "past day" note is visible. */
    @Test
    fun colostrumListPastDay() = shot("colostrum-list-past-day") {
        val rows = flowOf(
            PagingData.from(
                listOf(
                    colostrumCard(
                        id = "wf-9", displayId = "CPT-10228", done = 3, total = 6,
                        next = "5th Colostrum", due = "1d late", overdue = true,
                        bucket = WorkflowCardBucket.OVERDUE, meta = "Born 5 Aug 06:10 · Castro 1 · Boer",
                    ),
                ),
            ),
        ).collectAsLazyPagingItems()
        WorkflowListScreen(
            state = WorkflowListUiState(
                module = WorkflowModuleUi.COLOSTRUM,
                subtitle = "1 kid to feed",
                dateIso = "2026-08-05",
                dateLabel = "5 Aug",
                isToday = false,
                chips = listOf(
                    WorkflowChipUi("all", "All", 1),
                    WorkflowChipUi("overdue", "Overdue", 1),
                    WorkflowChipUi("due", "Due", 0),
                    WorkflowChipUi("completed", "Completed", 0),
                ),
                selectedFilter = "overdue",
                lastSyncedAt = null,
            ),
            rows = rows,
        )
    }

    /** Nothing born in the last two days — the day is legitimately empty. */
    @Test
    fun colostrumListEmpty() = shot("colostrum-list-empty") {
        val rows = flowOf(PagingData.from(emptyList<WorkflowCardUi>())).collectAsLazyPagingItems()
        WorkflowListScreen(
            state = WorkflowListUiState(
                module = WorkflowModuleUi.COLOSTRUM,
                dateIso = "2026-08-06",
                dateLabel = "Today · 6 Aug",
                isToday = true,
                chips = listOf(
                    WorkflowChipUi("all", "All", 0),
                    WorkflowChipUi("overdue", "Overdue", 0),
                    WorkflowChipUi("due", "Due", 0),
                    WorkflowChipUi("completed", "Completed", 0),
                ),
                emptyMessage = "No colostrum feeds for this day.",
                lastSyncedAt = null,
            ),
            rows = rows,
        )
    }

    private fun colostrumFeed(
        id: String,
        title: String,
        detail: String,
        status: String,
        tone: WorkflowStatusTone,
        section: WorkflowActionSection,
        canRecord: Boolean,
    ) = WorkflowActionUi(
        actionId = id,
        actionKey = id,
        title = title,
        detail = detail,
        typeLabel = "Do & confirm",
        glyph = if (tone == WorkflowStatusTone.DONE) "✓" else "▣",
        requiresVideo = true,
        options = emptyList(),
        statusLabel = status,
        statusTone = tone,
        section = section,
        canAnswer = false,
        canComplete = false,
        canRecordVideo = canRecord,
        opensPromote = false,
        footer = if (tone == WorkflowStatusTone.DONE) "Recorded by Amit Kumar" else "",
        answerValue = null,
    )

    /** The drill-in: ONLY that date's feeds, counted at the day grain (2/5, not the kid's 13). */
    @Test
    fun colostrumDetail() = shot("colostrum-detail") {
        WorkflowDetailScreen(
            state = WorkflowDetailUiState(
                workflowId = "wf-1",
                loading = false,
                displayId = "CPT-10234",
                roleLabel = "Kid",
                templateLine = "Colostrum · Today · 6 Aug",
                facts = listOf(
                    "Born" to "06 Aug 2026 · 05:40",
                    "Park" to "Channapatna",
                    "Shed" to "Castro 2",
                    "Mother RFID" to "982000123456789",
                ),
                actionsDone = 2,
                actionsTotal = 5,
                actions = listOf(
                    colostrumFeed(
                        "f-0700", "2nd Colostrum", "Feed colostrum at the 07:00 session on the birth day.",
                        "Done", WorkflowStatusTone.DONE, WorkflowActionSection.COMPLETED, canRecord = false,
                    ),
                    colostrumFeed(
                        "f-1100", "3rd Colostrum", "Feed colostrum at the 11:00 session on the birth day.",
                        "Done", WorkflowStatusTone.DONE, WorkflowActionSection.COMPLETED, canRecord = false,
                    ),
                    colostrumFeed(
                        "f-1500", "4th Colostrum", "Feed colostrum at the 15:00 session on the birth day.",
                        "2h late", WorkflowStatusTone.OVERDUE, WorkflowActionSection.OVERDUE, canRecord = true,
                    ),
                    colostrumFeed(
                        "f-1830", "5th Colostrum", "Feed colostrum at the 18:30 session on the birth day.",
                        "18:30", WorkflowStatusTone.SCHEDULED, WorkflowActionSection.SCHEDULED, canRecord = false,
                    ),
                    colostrumFeed(
                        "f-2200", "6th Colostrum", "Feed colostrum at the 22:00 session on the birth day.",
                        "22:00", WorkflowStatusTone.SCHEDULED, WorkflowActionSection.SCHEDULED, canRecord = false,
                    ),
                ),
            ),
        )
    }

    /**
     * The honest-blocking case: 1st Colostrum is gated by four earlier BIRTH steps this screen does
     * not show, so the backend sends a reason instead of letting the operator tap into a 409.
     */
    @Test
    fun colostrumDetailBlockedByEarlierBirthSteps() = shot("colostrum-detail-blocked") {
        WorkflowDetailScreen(
            state = WorkflowDetailUiState(
                workflowId = "wf-2",
                loading = false,
                displayId = "CPT-10235",
                roleLabel = "Kid",
                templateLine = "Colostrum · Today · 6 Aug",
                facts = listOf(
                    "Born" to "06 Aug 2026 · 05:55",
                    "Park" to "Channapatna",
                    "Shed" to "Gandhi 1",
                ),
                actionsDone = 0,
                actionsTotal = 5,
                actions = listOf(
                    colostrumFeed(
                        "f-first", "1st Colostrum",
                        "Feed the first colostrum and record a video using the in-app camera.",
                        "Blocked", WorkflowStatusTone.BLOCKED, WorkflowActionSection.OVERDUE, canRecord = false,
                    ).copy(footer = "Finish the earlier birth steps for this kid first."),
                    colostrumFeed(
                        "f-1100b", "2nd Colostrum", "Feed colostrum at the 11:00 session on the birth day.",
                        "11:00", WorkflowStatusTone.SCHEDULED, WorkflowActionSection.SCHEDULED, canRecord = false,
                    ),
                ),
            ),
        )
    }

}
