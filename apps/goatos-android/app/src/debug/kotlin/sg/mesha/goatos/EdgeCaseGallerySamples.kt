package sg.mesha.goatos

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.feature.scan.ProofUploadStatus
import sg.mesha.goatos.feature.scan.RosterRow
import sg.mesha.goatos.feature.scan.ScanError
import sg.mesha.goatos.feature.scan.ScanFeedEntry
import sg.mesha.goatos.feature.scan.ScanFeedTone
import sg.mesha.goatos.feature.scan.ScanListSheet
import sg.mesha.goatos.feature.scan.ScanReaderConnection
import sg.mesha.goatos.feature.scan.ScanStatus
import sg.mesha.goatos.feature.scan.ScanTileLabels
import sg.mesha.goatos.feature.scan.ScanUiState
import sg.mesha.goatos.feature.scan.VaccineGroup
import sg.mesha.goatos.feature.submit.SubmitUiState
import sg.mesha.goatos.feature.submit.SyncState
import sg.mesha.goatos.feature.weighing.WeighingDraftUiRow
import sg.mesha.goatos.feature.weighing.WeighingRosterUiRow
import sg.mesha.goatos.feature.weighing.WeighingUiState
import sg.mesha.goatos.ui.sampleScanState
import sg.mesha.goatos.ui.sampleSubmitState
import sg.mesha.goatos.ui.sampleWeighingOperatorState

data class GalleryCase(
    val key: String,
    val label: String,
    val group: String,
)

fun edgeCaseGalleryCases(): List<GalleryCase> = listOf(
    GalleryCase("scan_two_tags_two_vaccines", "2 tags + 2 vaccines", "Vaccination scan"),
    GalleryCase("scan_duplicate", "Duplicate tag", "Vaccination scan"),
    GalleryCase("scan_unknown", "Unknown tag", "Vaccination scan"),
    GalleryCase("scan_wrong_shed", "Wrong shed", "Vaccination scan"),
    GalleryCase("scan_proof_missing", "Proof missing", "Vaccination proof"),
    GalleryCase("scan_proof_uploading", "Proof uploading", "Vaccination proof"),
    GalleryCase("scan_proof_failed", "Proof failed", "Vaccination proof"),
    GalleryCase("scan_proof_synced", "Proof synced", "Vaccination proof"),
    GalleryCase("scan_offline_queued", "Offline queued", "Vaccination sync"),
    GalleryCase("scan_font_overflow", "Long text stress", "Vaccination layout"),
    GalleryCase("scan_pending_sheet", "Pending sheet", "Vaccination bottom sheet"),
    GalleryCase("scan_done_sheet", "Done sheet", "Vaccination bottom sheet"),
    GalleryCase("scan_skipped_sheet", "Skipped sheet", "Vaccination bottom sheet"),
    GalleryCase("scan_camera_one_vaccine", "Camera proof · one vaccine", "Vaccination camera"),
    GalleryCase("scan_camera_two_tags_two_vaccines", "Camera proof · two tags", "Vaccination camera"),
    GalleryCase("submit_offline_queued", "Submit offline queued", "Vaccination submit"),
    GalleryCase("weighing_individual", "Individual weight + proof", "Weighing"),
    GalleryCase("weighing_lumpsum", "Lumpsum shed proof", "Weighing"),
    GalleryCase("weighing_wrong_shed", "Weighing wrong shed", "Weighing"),
    GalleryCase("weighing_offline_queued", "Weighing offline queued", "Weighing"),
)

fun galleryScanTwoTagsTwoVaccines(): ScanUiState = galleryScanBase().copy(
    ringDone = 1,
    doneCount = 1,
    pendingCount = 4,
    vaccineGroups = listOf(
        VaccineGroup("et-tt", "ET+TT", 1, 5, true),
        VaccineGroup("ppr", "PPR", 1, 2, false),
    ),
    feed = listOf(
        ScanFeedEntry(
            primaryTag = "901007000504407",
            secondaryTag = "901007000504407B",
            vaccineLabel = "ET+TT · PPR",
            status = ScanStatus.DONE,
            scannedAtLabel = "Scanned now",
        ),
    ),
    footNote = "Camera opens automatically after a valid scan.",
)

fun galleryScanDuplicateTag(): ScanUiState = galleryScanTwoTagsTwoVaccines().copy(
    duplicateNotice = "Already scanned · proof ready · ET+TT · PPR",
    feed = listOf(
        ScanFeedEntry(
            primaryTag = "901007000504407",
            secondaryTag = "901007000504407B",
            vaccineLabel = "already scanned · proof ready · ET+TT · PPR",
            status = ScanStatus.DONE,
            scannedAtLabel = "Scanned earlier today",
            tone = ScanFeedTone.DUPLICATE,
        ),
    ),
)

fun galleryScanUnknownTag(): ScanUiState = galleryScanBase().copy(
    skippedCount = 1,
    pendingCount = 5,
    error = ScanError("Unknown tag · not in this shed", "901007000599999"),
    feed = listOf(
        ScanFeedEntry(
            primaryTag = "901007000599999",
            secondaryTag = null,
            vaccineLabel = "unknown tag · not in this shed",
            status = ScanStatus.SKIPPED,
            scannedAtLabel = "Scanned now",
            tone = ScanFeedTone.REJECTED,
        ),
    ),
)

fun galleryScanWrongShed(): ScanUiState = galleryScanBase().copy(
    skippedCount = 1,
    pendingCount = 5,
    error = ScanError("Wrong shed · expected Kid Shed A, now in Kid Shed B", "901007000504392"),
    feed = listOf(
        ScanFeedEntry(
            primaryTag = "901007000504392",
            secondaryTag = null,
            vaccineLabel = "wrong shed · expected Kid Shed A · actual Kid Shed B",
            status = ScanStatus.SKIPPED,
            scannedAtLabel = "Scanned now",
            tone = ScanFeedTone.REJECTED,
        ),
    ),
)

fun galleryScanProofMissing(): ScanUiState = galleryProofState(
    ProofUploadStatus.MISSING,
    "1 animal needs a proof video before submit",
)

fun galleryScanProofUploading(): ScanUiState = galleryProofState(
    ProofUploadStatus.UPLOADING,
    "Proof uploading. Keep scanning.",
)

fun galleryScanProofFailed(): ScanUiState = galleryProofState(
    ProofUploadStatus.FAILED,
    "Proof failed. Auto retry is running; rescan this tag to replace the clip.",
)

fun galleryScanProofSynced(): ScanUiState = galleryProofState(
    ProofUploadStatus.SYNCED,
    "Proof ready.",
).copy(canSubmit = true, submitLabel = "Finalize shed")

fun galleryScanOfflineQueued(): ScanUiState = galleryScanTwoTagsTwoVaccines().copy(
    isOffline = true,
    footNote = "Saved on this phone. It will sync when network returns.",
    feed = listOf(
        ScanFeedEntry(
            primaryTag = "901007000504418",
            secondaryTag = null,
            vaccineLabel = "ET+TT · queued",
            status = ScanStatus.DONE,
            scannedAtLabel = "Saved offline",
        ),
    ),
    proofActionNeeded = listOf(galleryProofRow(ProofUploadStatus.UPLOADING, unsynced = true)),
)

fun galleryScanFontOverflowStress(): ScanUiState = galleryScanBase().copy(
    shedLabel = "Vaccination · Channapatna / God el 2 · Part 8 · Very long shed name",
    cohortLabel = "Long vaccine copy stress",
    ringDone = 2,
    doneCount = 2,
    pendingCount = 3,
    vaccineGroups = listOf(
        VaccineGroup("long", "Enterotoxaemia + Tetanus · Booster", 1, 3, true),
        VaccineGroup("long2", "Peste des Petits Ruminants", 1, 2, false),
    ),
    feed = listOf(
        ScanFeedEntry(
            primaryTag = "901007000504407",
            secondaryTag = "901007000504407B",
            vaccineLabel = "Enterotoxaemia + Tetanus · Peste des Petits Ruminants",
            status = ScanStatus.DONE,
            scannedAtLabel = "Scanned Tue, 7:18 PM IST",
        ),
    ),
)

fun galleryBottomSheetRows(kind: String): List<RosterRow> = when (kind) {
    "done" -> listOf(
        RosterRow("901007000504407", "901007000504407B", "ET+TT · PPR", ScanStatus.DONE, scannedAtLabel = "Scanned now"),
        RosterRow("901007000504418", null, "ET+TT", ScanStatus.DONE, scannedAtLabel = "Scanned Tue, 7:16 PM"),
    )
    "skipped" -> listOf(
        RosterRow("901007000599999", null, "unknown tag · not in this shed", ScanStatus.SKIPPED),
        RosterRow("901007000504392", null, "wrong shed · expected Kid Shed A · actual Kid Shed B", ScanStatus.SKIPPED),
    )
    else -> listOf(
        RosterRow("901007000504407", "901007000504407B", "ET+TT · PPR", ScanStatus.PENDING),
        RosterRow("901007000504419", null, "ET+TT", ScanStatus.PENDING),
        RosterRow("901007000504392", null, "Goat Pox · FMD", ScanStatus.PENDING),
    )
}

fun gallerySubmitOfflineQueued(): SubmitUiState = sampleSubmitState().copy(
    syncState = SyncState.SYNCING,
    syncLabel = "Saved on this phone",
    canSubmit = false,
    proofSummaryTitle = "Goat camera proof",
    proofSummarySyncedLabel = "3 ready · 2 waiting",
    proofSummaryFinalizeHint = "Finalize is enabled after pending videos sync.",
    proofTotal = 5,
    proofSynced = 3,
    proofUploading = 2,
)

fun galleryWeighingIndividualProof(): WeighingUiState = sampleWeighingOperatorState().copy(
    title = "Kid Shed A / Part 1",
    scopeLabel = "WEIGHING · Individual",
    category = "individual_animal",
    totalExpected = 5,
    selectedAnimalId = "goat-901007000504407",
    selectedAnimalLabel = "901007000504407 · 2 tags",
    scanInput = "901007000504407",
    weightInput = "18.4",
    individualDrafts = listOf(
        WeighingDraftUiRow("w1", "goat-901007000504407", "901007000504407 · 18.4 kg · proof uploading", proofReady = false, readyToSubmit = false),
        WeighingDraftUiRow("w2", "goat-901007000504418", "901007000504418 · 19.1 kg · proof ready", proofReady = true, readyToSubmit = true),
    ),
    visibleRows = weighingRows(wrongShed = false),
)

fun galleryWeighingLumpsumProof(): WeighingUiState = sampleWeighingOperatorState().copy(
    title = "Kid Shed B / Part 1",
    scopeLabel = "WEIGHING · Lumpsum",
    category = "per_shed_partition",
    totalExpected = 2,
    scanInput = "",
    selectedAnimalId = null,
    selectedAnimalLabel = null,
    weightInput = "74.5",
    individualDrafts = emptyList(),
    shedDrafts = listOf(
        WeighingDraftUiRow("shed-proof", label = "Kid Shed B / Part 1 · 74.5 kg · shed proof ready", proofReady = true, readyToSubmit = true),
    ),
    visibleRows = emptyList(),
)

fun galleryWeighingWrongShed(): WeighingUiState = galleryWeighingIndividualProof().copy(
    message = "Wrong shed · expected Kid Shed A, now in Kid Shed B",
    visibleRows = weighingRows(wrongShed = true),
)

fun galleryWeighingOfflineQueued(): WeighingUiState = galleryWeighingIndividualProof().copy(
    message = "Saved on this phone. It will sync when network returns.",
    individualDrafts = listOf(
        WeighingDraftUiRow("w1", "goat-901007000504407", "901007000504407 · 18.4 kg · queued", proofReady = false, readyToSubmit = false),
        WeighingDraftUiRow("w2", "goat-901007000504418", "901007000504418 · 19.1 kg · queued", proofReady = false, readyToSubmit = false),
    ),
)

@Composable
fun GalleryBottomSheetCase(title: String, rows: List<RosterRow>) {
    ScanListSheet(title = title, rows = rows, captureEnabled = true)
}

@Composable
fun GalleryAutoProofCameraFrame(
    title: String,
    stateLabel: String,
    primaryTag: String,
    secondaryTag: String?,
    vaccineLabel: String,
    hint: String,
) {
    Surface(color = MeshaColors.Bg, modifier = Modifier.fillMaxSize()) {
        Column(Modifier.fillMaxSize().padding(18.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Box(
                    Modifier
                        .size(48.dp)
                        .clip(RoundedCornerShape(14.dp))
                        .background(MeshaColors.Surf2),
                    contentAlignment = Alignment.Center,
                ) {
                    Icon(MeshaIcons.Video, contentDescription = null, tint = MeshaColors.Brand, modifier = Modifier.size(24.dp))
                }
                Spacer(Modifier.width(12.dp))
                Column(Modifier.weight(1f)) {
                    Text(title, color = MeshaColors.Ink, fontSize = 24.sp, fontWeight = FontWeight.W800)
                    Text(stateLabel, color = MeshaColors.Brand, fontSize = 12.sp, fontWeight = FontWeight.W700)
                }
            }
            Spacer(Modifier.height(18.dp))
            Box(
                modifier = Modifier
                    .fillMaxWidth()
                    .weight(1f)
                    .clip(RoundedCornerShape(22.dp))
                    .background(Color(0xFF07110C))
                    .border(1.dp, MeshaColors.Hair, RoundedCornerShape(22.dp)),
            ) {
                Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                    CircularProgressIndicator(color = Color(0x333E8B57), strokeWidth = 2.dp, modifier = Modifier.size(220.dp))
                    Icon(MeshaIcons.Video, contentDescription = null, tint = Color(0x2257D06D), modifier = Modifier.size(74.dp))
                }
                GalleryAnimalStamp(
                    primaryTag = primaryTag,
                    secondaryTag = secondaryTag,
                    vaccineLabel = vaccineLabel,
                    modifier = Modifier.align(Alignment.BottomStart).padding(16.dp),
                )
                Row(
                    modifier = Modifier
                        .align(Alignment.TopEnd)
                        .padding(18.dp)
                        .clip(RoundedCornerShape(999.dp))
                        .background(Color(0xAA2A1111))
                        .padding(horizontal = 10.dp, vertical = 6.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Box(Modifier.size(8.dp).clip(CircleShape).background(MeshaColors.Danger))
                    Spacer(Modifier.width(6.dp))
                    Text("REC", color = MeshaColors.Danger, fontSize = 11.sp, fontWeight = FontWeight.W800)
                }
            }
            Spacer(Modifier.height(14.dp))
            Text(hint, color = MeshaColors.Muted, fontSize = 13.sp, lineHeight = 18.sp, textAlign = TextAlign.Center, modifier = Modifier.fillMaxWidth())
            Spacer(Modifier.height(10.dp))
            Button(
                onClick = {},
                modifier = Modifier.fillMaxWidth().height(54.dp),
                colors = ButtonDefaults.buttonColors(containerColor = MeshaColors.Brand, contentColor = MeshaColors.OnBrand),
                shape = RoundedCornerShape(999.dp),
            ) {
                Icon(MeshaIcons.Video, contentDescription = null, modifier = Modifier.size(18.dp))
                Spacer(Modifier.width(8.dp))
                Text("Stop recording", fontSize = 15.sp, fontWeight = FontWeight.W800)
            }
            Spacer(Modifier.height(8.dp))
            Text("Proof uploads in background", color = MeshaColors.Faint, fontSize = 11.sp, textAlign = TextAlign.Center, modifier = Modifier.fillMaxWidth())
        }
    }
}

@Composable
private fun GalleryAnimalStamp(primaryTag: String, secondaryTag: String?, vaccineLabel: String, modifier: Modifier = Modifier) {
    Column(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(18.dp))
            .background(Color(0xD9111F18))
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(18.dp))
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(primaryTag, color = MeshaColors.Ink, fontSize = 18.sp, fontWeight = FontWeight.W800, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (secondaryTag != null) {
                Spacer(Modifier.width(8.dp))
                Text("2 tags", color = MeshaColors.Muted, fontSize = 11.sp, fontWeight = FontWeight.W800, modifier = Modifier.clip(RoundedCornerShape(8.dp)).background(MeshaColors.Surf2).padding(horizontal = 7.dp, vertical = 3.dp))
            }
        }
        secondaryTag?.let { Text(it, color = MeshaColors.Muted, fontSize = 13.sp, fontFamily = FontFamily.Monospace) }
        Text(vaccineLabel, color = MeshaColors.Brand, fontSize = 14.sp, fontWeight = FontWeight.W800)
    }
}

private fun galleryScanBase(): ScanUiState = sampleScanState().copy(
    shedLabel = "Vaccination · Kid Shed A / Part 1",
    cohortLabel = "Shed scan",
    ringDone = 0,
    ringTotal = 5,
    doneCount = 0,
    pendingCount = 5,
    skippedCount = 0,
    tileLabels = ScanTileLabels("Done", "Pending", "Skipped"),
    vaccineGroups = listOf(VaccineGroup("et-tt", "ET+TT", 0, 5, true)),
    feed = emptyList(),
    roster = galleryBottomSheetRows("pending"),
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

private fun galleryProofState(status: ProofUploadStatus, footNote: String): ScanUiState = galleryScanBase().copy(
    ringDone = 1,
    doneCount = 1,
    pendingCount = 4,
    proofActionNeeded = listOf(galleryProofRow(status)),
    feed = emptyList(),
    footNote = footNote,
)

private fun galleryProofRow(status: ProofUploadStatus, unsynced: Boolean = false): RosterRow = RosterRow(
    primaryTag = "901007000504407",
    secondaryTag = "901007000504407B",
    vaccineLabel = "ET+TT · PPR",
    status = ScanStatus.DONE,
    unsynced = unsynced,
    scannedAtLabel = "Scanned now",
    goatId = "goat-901007000504407",
    proofClipCount = if (status == ProofUploadStatus.MISSING) 0 else 1,
    proofUploadStatus = status,
)

private fun weighingRows(wrongShed: Boolean): List<WeighingRosterUiRow> = listOf(
    WeighingRosterUiRow(
        id = "row-1",
        animalId = "goat-901007000504407",
        displayAnimalId = "901007000504407",
        expectedLocationLabel = "Kid Shed A",
        actualLocationLabel = if (wrongShed) "Kid Shed B" else "Kid Shed A",
        status = if (wrongShed) "Wrong shed" else "Accepted",
        availabilityStatus = null,
        wrongShed = wrongShed,
    ),
    WeighingRosterUiRow(
        id = "row-2",
        animalId = "goat-901007000504418",
        displayAnimalId = "901007000504418",
        expectedLocationLabel = "Kid Shed A",
        actualLocationLabel = null,
        status = "Pending",
        availabilityStatus = null,
        wrongShed = false,
    ),
)
