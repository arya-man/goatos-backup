package sg.mesha.goatos.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import app.cash.paparazzi.DeviceConfig
import app.cash.paparazzi.Paparazzi
import org.junit.Rule
import org.junit.Test
import sg.mesha.goatos.core.designsystem.locale.ProvideAppLocale
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.feature.scan.ProofUploadStatus
import sg.mesha.goatos.feature.scan.RosterRow
import sg.mesha.goatos.feature.scan.ScanError
import sg.mesha.goatos.feature.scan.ScanFeedEntry
import sg.mesha.goatos.feature.scan.ScanFeedTone
import sg.mesha.goatos.feature.scan.ScanReaderConnection
import sg.mesha.goatos.feature.scan.ScanListSheet
import sg.mesha.goatos.feature.scan.ScanScreen
import sg.mesha.goatos.feature.scan.ScanStatus
import sg.mesha.goatos.feature.scan.VaccineGroup

class ScanEdgeCaseScreenshotTest {

    @get:Rule
    val paparazzi = Paparazzi(deviceConfig = DeviceConfig.PIXEL_6)

    @Test
    fun scanReadyEmpty() = shot("scan_case_01_ready_empty") {
        ScanScreen(state = baseState())
    }

    @Test
    fun scanAcceptedFeed() = shot("scan_case_02_accepted_feed") {
        ScanScreen(
            state = baseState().copy(
                ringDone = 3,
                doneCount = 3,
                pendingCount = 2,
                feed = acceptedFeed(),
                canSubmit = false,
            ),
        )
    }

    @Test
    fun scanDuplicateTag() = shot("scan_case_03_duplicate_tag") {
        ScanScreen(
            state = baseState().copy(
                ringDone = 3,
                doneCount = 3,
                pendingCount = 2,
                duplicateNotice = "Already scanned · ET+TT",
                feed = listOf(
                    ScanFeedEntry(
                        primaryTag = "901007000503785",
                        secondaryTag = null,
                        vaccineLabel = "already scanned · ET+TT",
                        status = ScanStatus.DONE,
                        scannedAtLabel = "Scanned Tue, 4:55 PM IST",
                        tone = ScanFeedTone.DUPLICATE,
                    ),
                ) + acceptedFeed(),
            ),
        )
    }

    @Test
    fun scanUnknownTag() = shot("scan_case_04_unknown_tag") {
        ScanScreen(
            state = baseState().copy(
                ringDone = 3,
                doneCount = 3,
                pendingCount = 2,
                skippedCount = 1,
                error = ScanError(message = "Unknown tag · not in this shed", tag = "901007000504379"),
                feed = listOf(
                    ScanFeedEntry(
                        primaryTag = "901007000504379",
                        secondaryTag = null,
                        vaccineLabel = "unknown tag · not in this shed",
                        status = ScanStatus.SKIPPED,
                        scannedAtLabel = "Scanned Tue, 4:56 PM IST",
                        tone = ScanFeedTone.REJECTED,
                    ),
                ) + acceptedFeed(),
            ),
        )
    }

    @Test
    fun scanWrongShedVisible() = shot("scan_case_05_wrong_shed_visible") {
        ScanScreen(
            state = baseState().copy(
                ringDone = 3,
                doneCount = 3,
                pendingCount = 2,
                skippedCount = 1,
                error = ScanError(message = "Wrong shed · expected Shed 1, now in Shed 2", tag = "901007000504382"),
                feed = listOf(
                    ScanFeedEntry(
                        primaryTag = "901007000504382",
                        secondaryTag = "901007000504383",
                        vaccineLabel = "wrong shed · expected Shed 1 · actual Shed 2",
                        status = ScanStatus.SKIPPED,
                        scannedAtLabel = "Scanned Tue, 4:57 PM IST",
                        tone = ScanFeedTone.REJECTED,
                    ),
                ) + acceptedFeed(),
            ),
        )
    }

    @Test
    fun scanMultipleVaccines() = shot("scan_case_06_multiple_vaccines") {
        ScanScreen(
            state = baseState().copy(
                ringDone = 4,
                doneCount = 4,
                pendingCount = 1,
                vaccineGroups = listOf(
                    VaccineGroup("et-tt", "ET+TT", done = 2, due = 3, active = true),
                    VaccineGroup("ppr", "PPR", done = 1, due = 1, active = false),
                    VaccineGroup("fmd", "FMD", done = 1, due = 1, active = false),
                ),
                feed = listOf(
                    ScanFeedEntry("901007000503790", null, "ET+TT · PPR · FMD", ScanStatus.DONE, "Scanned Tue, 4:58 PM IST"),
                    ScanFeedEntry("901007000503789", "901007000503788", "ET+TT · PPR", ScanStatus.DONE, "Scanned Tue, 4:57 PM IST"),
                    ScanFeedEntry("901007000503785", null, "ET+TT", ScanStatus.DONE, "Scanned Tue, 4:56 PM IST"),
                ),
            ),
        )
    }

    @Test
    fun scanProofNeededEqualRows() = shot("scan_case_07_proof_needed_equal_rows") {
        ScanScreen(
            state = baseState().copy(
                ringDone = 3,
                doneCount = 3,
                pendingCount = 0,
                proofActionNeeded = proofRows(ProofUploadStatus.MISSING, ProofUploadStatus.MISSING, ProofUploadStatus.MISSING),
                feed = emptyList(),
                canSubmit = false,
                footNote = "3 animals need proof video before submit",
            ),
        )
    }

    @Test
    fun scanProofUploadAndRetryStates() = shot("scan_case_08_proof_upload_retry") {
        ScanScreen(
            state = baseState().copy(
                ringDone = 4,
                doneCount = 4,
                pendingCount = 0,
                proofActionNeeded = proofRows(
                    ProofUploadStatus.UPLOADING,
                    ProofUploadStatus.FAILED,
                    ProofUploadStatus.MISSING,
                    ProofUploadStatus.SYNCED,
                ),
                feed = emptyList(),
                canSubmit = false,
                footNote = "Resolve failed or missing videos before finalizing",
            ),
        )
    }

    @Test
    fun scanProofNeededSameGoatMultipleObligations() = shot("scan_case_08b_proof_same_goat_multi_obligation") {
        ScanScreen(
            state = baseState().copy(
                ringDone = 2,
                doneCount = 2,
                pendingCount = 0,
                proofActionNeeded = sameGoatMultiObligationProofRows(),
                feed = emptyList(),
                canSubmit = false,
                footNote = "One proof row for one scanned goat with both due vaccines",
            ),
        )
    }

    @Test
    fun scanPendingOverlay() = shot("scan_case_09_pending_overlay") {
        ScanScreen(
            state = baseState().copy(
                selectedFilter = ScanStatus.PENDING,
                roster = rosterRows(),
            ),
        )
    }

    @Test
    fun scanSkippedOverlay() = shot("scan_case_10_skipped_overlay") {
        ScanScreen(
            state = baseState().copy(
                selectedFilter = ScanStatus.SKIPPED,
                roster = rosterRows(),
                skippedCount = 2,
            ),
        )
    }

    @Test
    fun doneBottomSheetContent() = shot("scan_case_11_done_sheet_content") {
        ScanListSheet(
            title = "Done",
            rows = rosterRows().filter { it.status == ScanStatus.DONE },
            captureEnabled = true,
        )
    }

    @Test
    fun pendingBottomSheetContent() = shot("scan_case_12_pending_sheet_content") {
        ScanListSheet(
            title = "Pending",
            rows = rosterRows().filter { it.status == ScanStatus.PENDING },
            captureEnabled = true,
        )
    }

    @Test
    fun pendingBottomSheetTwoTagsAndTwoVaccines() = shot("scan_case_12b_pending_two_tags_two_vaccines") {
        ScanListSheet(
            title = "Pending",
            rows = listOf(
                RosterRow(
                    primaryTag = "901007000504377",
                    secondaryTag = "901007000504378",
                    vaccineLabel = "ET+TT · PPR",
                    status = ScanStatus.PENDING,
                ),
                RosterRow(
                    primaryTag = "901007000504382",
                    secondaryTag = "901007000504383",
                    vaccineLabel = "Goat Pox · FMD",
                    status = ScanStatus.PENDING,
                ),
                RosterRow(
                    primaryTag = "901007000504390",
                    secondaryTag = null,
                    vaccineLabel = "ET+TT",
                    status = ScanStatus.PENDING,
                ),
            ),
            captureEnabled = true,
        )
    }

    @Test
    fun skippedBottomSheetContent() = shot("scan_case_13_skipped_sheet_content") {
        ScanListSheet(
            title = "Skipped",
            rows = rosterRows().filter { it.status == ScanStatus.SKIPPED },
            captureEnabled = true,
        )
    }

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
}

private fun baseState() = sampleScanState().copy(
    shedLabel = "Vaccination",
    cohortLabel = "Shed 1 Scan",
    ringDone = 0,
    ringTotal = 5,
    doneCount = 0,
    pendingCount = 5,
    skippedCount = 0,
    listTitle = "Scanned goats",
    submitLabel = "Finalize shed",
    canSubmit = false,
    scanEnabled = true,
    lastSyncedAt = null,
    readerConnection = ScanReaderConnection(
        readerName = "RFID reader",
        statusLabel = "Ready for keyboard-wedge scans",
        connected = true,
        actionLabel = "Reconnect",
    ),
)

private fun acceptedFeed() = listOf(
    ScanFeedEntry(
        primaryTag = "901007000503785",
        secondaryTag = null,
        vaccineLabel = "ET+TT",
        status = ScanStatus.DONE,
        scannedAtLabel = "Scanned Tue, 4:52 PM IST",
    ),
    ScanFeedEntry(
        primaryTag = "901007000504377",
        secondaryTag = "901007000504378",
        vaccineLabel = "ET+TT",
        status = ScanStatus.DONE,
        scannedAtLabel = "Scanned Tue, 4:51 PM IST",
    ),
    ScanFeedEntry(
        primaryTag = "901007000504382",
        secondaryTag = "901007000504383",
        vaccineLabel = "ET+TT",
        status = ScanStatus.DONE,
        scannedAtLabel = "Scanned Tue, 4:50 PM IST",
    ),
)

private fun proofRows(vararg statuses: ProofUploadStatus): List<RosterRow> =
    statuses.mapIndexed { index, status ->
        val tag = 901007000503785L + index
        RosterRow(
            primaryTag = tag.toString(),
            secondaryTag = if (index % 2 == 0) null else (tag + 1000).toString(),
            vaccineLabel = when (index) {
                1 -> "ET+TT · PPR"
                2 -> "ET+TT · FMD"
                else -> "ET+TT"
            },
            status = ScanStatus.DONE,
            scannedAtLabel = "Scanned Tue, 4:${52 - index} PM IST",
            goatId = "goat-$index",
            obligationId = "obligation-$index",
            proofClipCount = if (status == ProofUploadStatus.MISSING) 0 else 1,
            proofUploadStatus = status,
        )
    }

private fun sameGoatMultiObligationProofRows() = listOf(
    RosterRow(
        primaryTag = "901007000503785",
        secondaryTag = null,
        vaccineLabel = "ET+TT · PPR",
        status = ScanStatus.DONE,
        scannedAtLabel = "Scanned Tue, 4:52 PM IST",
        goatId = "goat-same",
        obligationId = "obligation-et-tt|obligation-ppr",
        proofUploadStatus = ProofUploadStatus.MISSING,
    ),
)

private fun rosterRows() = listOf(
    RosterRow("901007000503785", null, "ET+TT", ScanStatus.DONE, scannedAtLabel = "Scanned Tue, 4:52 PM IST"),
    RosterRow("901007000504377", "901007000504378", "ET+TT", ScanStatus.DONE, scannedAtLabel = "Scanned Tue, 4:51 PM IST"),
    RosterRow("901007000504382", "901007000504383", "ET+TT", ScanStatus.PENDING),
    RosterRow("901007000504379", null, "unknown tag · not in this shed", ScanStatus.SKIPPED),
    RosterRow("901007000504390", null, "wrong shed · expected Shed 1 · actual Shed 2", ScanStatus.SKIPPED),
)
