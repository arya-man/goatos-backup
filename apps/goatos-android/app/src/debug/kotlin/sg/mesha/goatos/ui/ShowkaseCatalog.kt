package sg.mesha.goatos.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import com.airbnb.android.showkase.annotation.ShowkaseComposable
import com.airbnb.android.showkase.annotation.ShowkaseRoot
import com.airbnb.android.showkase.annotation.ShowkaseRootModule
import sg.mesha.goatos.core.designsystem.locale.ProvideAppLocale
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.feature.scan.ProofUploadStatus
import sg.mesha.goatos.feature.scan.RosterRow
import sg.mesha.goatos.feature.scan.ScanError
import sg.mesha.goatos.feature.scan.ScanFeedEntry
import sg.mesha.goatos.feature.scan.ScanFeedTone
import sg.mesha.goatos.feature.scan.ScanListSheet
import sg.mesha.goatos.feature.scan.ScanReaderConnection
import sg.mesha.goatos.feature.scan.ScanScreen
import sg.mesha.goatos.feature.scan.ScanStatus
import sg.mesha.goatos.feature.scan.ScanTileLabels
import sg.mesha.goatos.feature.scan.ScanUiState
import sg.mesha.goatos.feature.scan.VaccineGroup
import sg.mesha.goatos.feature.weighing.WeighingDraftUiRow
import sg.mesha.goatos.feature.weighing.WeighingRosterUiRow
import sg.mesha.goatos.feature.weighing.WeighingScreen
import sg.mesha.goatos.feature.weighing.WeighingUiState

@ShowkaseRoot
class GoatOsShowkaseRoot : ShowkaseRootModule

@ShowkaseComposable(
    name = "Scan ready empty",
    group = "Vaccination / Scan",
    widthDp = 393,
    heightDp = 852,
)
@Composable
internal fun ShowkaseVaccinationScanReady() = CatalogFrame {
    ScanScreen(state = scanBaseState())
}

@ShowkaseComposable(
    name = "Duplicate RFID",
    group = "Vaccination / Scan",
    widthDp = 393,
    heightDp = 852,
)
@Composable
internal fun ShowkaseVaccinationDuplicateTag() = CatalogFrame {
    ScanScreen(
        state = scanBaseState().copy(
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
            ) + scanAcceptedFeed(),
        ),
    )
}

@ShowkaseComposable(
    name = "Unknown RFID",
    group = "Vaccination / Scan",
    widthDp = 393,
    heightDp = 852,
)
@Composable
internal fun ShowkaseVaccinationUnknownTag() = CatalogFrame {
    ScanScreen(
        state = scanBaseState().copy(
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
            ) + scanAcceptedFeed(),
        ),
    )
}

@ShowkaseComposable(
    name = "Wrong shed RFID",
    group = "Vaccination / Scan",
    widthDp = 393,
    heightDp = 852,
)
@Composable
internal fun ShowkaseVaccinationWrongShed() = CatalogFrame {
    ScanScreen(
        state = scanBaseState().copy(
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
            ) + scanAcceptedFeed(),
        ),
    )
}

@ShowkaseComposable(
    name = "Pending sheet two tags two vaccines",
    group = "Vaccination / Bottom Sheets",
    widthDp = 393,
    heightDp = 852,
)
@Composable
internal fun ShowkaseVaccinationPendingTwoTagsTwoVaccines() = CatalogFrame {
    ScanListSheet(
        title = "Pending",
        rows = listOf(
            RosterRow("901007000504377", "901007000504378", "ET+TT · PPR", ScanStatus.PENDING),
            RosterRow("901007000504382", "901007000504383", "Goat Pox · FMD", ScanStatus.PENDING),
            RosterRow("901007000504390", null, "ET+TT", ScanStatus.PENDING),
        ),
    )
}

@ShowkaseComposable(
    name = "Skipped sheet",
    group = "Vaccination / Bottom Sheets",
    widthDp = 393,
    heightDp = 852,
)
@Composable
internal fun ShowkaseVaccinationSkippedSheet() = CatalogFrame {
    ScanListSheet(
        title = "Skipped",
        rows = listOf(
            RosterRow("901007000504379", null, "unknown tag · not in this shed", ScanStatus.SKIPPED),
            RosterRow("901007000504390", null, "wrong shed · expected Shed 1 · actual Shed 2", ScanStatus.SKIPPED),
        ),
    )
}

@ShowkaseComposable(
    name = "Proof missing upload retry",
    group = "Vaccination / Proof",
    widthDp = 393,
    heightDp = 852,
)
@Composable
internal fun ShowkaseVaccinationProofStates() = CatalogFrame {
    ScanScreen(
        state = scanBaseState().copy(
            ringDone = 4,
            doneCount = 4,
            pendingCount = 0,
            proofActionNeeded = proofRows(
                ProofUploadStatus.MISSING,
                ProofUploadStatus.UPLOADING,
                ProofUploadStatus.FAILED,
                ProofUploadStatus.SYNCED,
            ),
            feed = emptyList(),
            canSubmit = false,
            footNote = "Resolve failed or missing videos before finalizing",
        ),
    )
}

@ShowkaseComposable(
    name = "Offline queued",
    group = "Vaccination / Sync",
    widthDp = 393,
    heightDp = 852,
)
@Composable
internal fun ShowkaseVaccinationOfflineQueued() = CatalogFrame {
    ScanScreen(
        state = scanBaseState().copy(
            ringDone = 2,
            doneCount = 2,
            pendingCount = 3,
            isOffline = true,
            feed = scanAcceptedFeed().take(2),
        ),
    )
}

@ShowkaseComposable(
    name = "Planner duplicate task guard",
    group = "Weighing / Planner",
    widthDp = 393,
    heightDp = 852,
)
@Composable
internal fun ShowkaseWeighingPlannerDuplicateGuard() = CatalogFrame {
    WeighingScreen(state = sampleWeighingPlanState())
}

@ShowkaseComposable(
    name = "Individual scan proof weight",
    group = "Weighing / Execution",
    widthDp = 393,
    heightDp = 852,
)
@Composable
internal fun ShowkaseWeighingIndividualExecution() = CatalogFrame {
    WeighingScreen(state = sampleWeighingOperatorState())
}

@ShowkaseComposable(
    name = "Lumpsum shed proof",
    group = "Weighing / Execution",
    widthDp = 393,
    heightDp = 852,
)
@Composable
internal fun ShowkaseWeighingLumpsumExecution() = CatalogFrame {
    WeighingScreen(state = sampleWeighingLumpsumState())
}

@Composable
private fun CatalogFrame(content: @Composable () -> Unit) {
    GoatOsTheme {
        ProvideAppLocale {
            Box(Modifier.fillMaxSize().background(MeshaColors.Bg)) {
                content()
            }
        }
    }
}

private fun scanBaseState() = ScanUiState(
    shedLabel = "Vaccination",
    cohortLabel = "Shed 1 Scan",
    ringDone = 0,
    ringTotal = 5,
    ringUnitLabel = "vaccinated",
    tapHint = "Hold the Bluetooth reader near the goat tag. A known tag is marked Done; an unknown tag is marked Skipped.",
    vaccineGroups = emptyList(),
    doneCount = 0,
    pendingCount = 5,
    skippedCount = 0,
    tileLabels = ScanTileLabels("Done", "Pending", "Skipped"),
    feed = emptyList(),
    roster = listOf(
        RosterRow("901007000503785", null, "ET+TT", ScanStatus.DONE, scannedAtLabel = "Scanned Tue, 4:52 PM IST"),
        RosterRow("901007000504377", "901007000504378", "ET+TT · PPR", ScanStatus.PENDING),
        RosterRow("901007000504382", "901007000504383", "Goat Pox · FMD", ScanStatus.PENDING),
        RosterRow("901007000504379", null, "unknown tag · not in this shed", ScanStatus.SKIPPED),
    ),
    listTitle = "Scanned goats",
    submitLabel = "Finalize shed",
    canSubmit = false,
    scanEnabled = true,
    lastSyncedAt = 0L,
    readerConnection = ScanReaderConnection(
        readerName = "RFID reader",
        statusLabel = "Ready for keyboard-wedge scans",
        connected = true,
        actionLabel = "Reconnect",
    ),
)

private fun scanAcceptedFeed() = listOf(
    ScanFeedEntry("901007000503785", null, "ET+TT", ScanStatus.DONE, "Scanned Tue, 4:52 PM IST"),
    ScanFeedEntry("901007000504377", "901007000504378", "ET+TT · PPR", ScanStatus.DONE, "Scanned Tue, 4:51 PM IST"),
    ScanFeedEntry("901007000504382", "901007000504383", "Goat Pox · FMD", ScanStatus.DONE, "Scanned Tue, 4:50 PM IST"),
)

private fun proofRows(vararg statuses: ProofUploadStatus): List<RosterRow> =
    statuses.mapIndexed { index, status ->
        val tag = 901007000503785L + index
        RosterRow(
            primaryTag = tag.toString(),
            secondaryTag = if (index % 2 == 0) null else (tag + 1000).toString(),
            vaccineLabel = when (index) {
                1 -> "ET+TT · PPR"
                2 -> "Goat Pox · FMD"
                else -> "ET+TT"
            },
            status = ScanStatus.DONE,
            scannedAtLabel = "Scanned Tue, 4:${52 - index} PM IST",
            goatId = "goat-$index",
            proofClipCount = if (status == ProofUploadStatus.MISSING) 0 else 1,
            proofUploadStatus = status,
        )
    }

private fun sampleWeighingLumpsumState(): WeighingUiState = WeighingUiState(
    title = "Castro 1",
    scopeLabel = "Weighing · Week 31 · Lumpsum",
    hasScope = true,
    category = "per_shed_partition",
    totalExpected = 80,
    weightInput = "1248.5",
    shedDrafts = listOf(
        WeighingDraftUiRow("shed-draft-1", "shed-castro-1", "Castro 1 · 1248.5 kg", proofReady = true, readyToSubmit = true),
    ),
    visibleRows = listOf(
        WeighingRosterUiRow(
            id = "shed-row-1",
            animalId = "",
            displayAnimalId = "Castro 1",
            expectedLocationLabel = "Castro 1",
            actualLocationLabel = "Castro 1",
            status = "Result recorded",
            availabilityStatus = "80 kids in scope",
            wrongShed = false,
        ),
    ),
)
