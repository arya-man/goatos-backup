package sg.mesha.goatos.ui

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
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
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
import app.cash.paparazzi.DeviceConfig
import app.cash.paparazzi.Paparazzi
import org.junit.Rule
import org.junit.Test
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.locale.ProvideAppLocale
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.feature.scan.ScanError
import sg.mesha.goatos.feature.scan.ScanFeedEntry
import sg.mesha.goatos.feature.scan.ScanFeedTone
import sg.mesha.goatos.feature.scan.ScanScreen
import sg.mesha.goatos.feature.scan.ScanStatus
import sg.mesha.goatos.feature.scan.VaccineGroup

class ScanAutoProofFlowScreenshotTest {

    @get:Rule
    val paparazzi = Paparazzi(deviceConfig = DeviceConfig.PIXEL_6)

    @Test
    fun validScanAutoCameraOpening() = shot("scan_auto_proof_01_valid_scan_camera_opening") {
        AutoProofCameraFrame(
            title = "Recording proof",
            stateLabel = "RFID accepted · camera opened automatically",
            primaryTag = "901007000503785",
            secondaryTag = null,
            vaccineLabel = "ET+TT",
            hint = "Keep the animal tag and vaccination visible. Stop when the proof is clear.",
        )
    }

    @Test
    fun twoTagAnimalCameraFrame() = shot("scan_auto_proof_02_two_tags_camera_frame") {
        AutoProofCameraFrame(
            title = "Recording proof",
            stateLabel = "RFID accepted · 2 tags matched",
            primaryTag = "901007000504377",
            secondaryTag = "901007000504378",
            vaccineLabel = "ET+TT",
            hint = "Both tags are stamped on this clip.",
        )
    }

    @Test
    fun multipleVaccinesCameraFrame() = shot("scan_auto_proof_03_multiple_vaccines_camera_frame") {
        AutoProofCameraFrame(
            title = "Recording proof",
            stateLabel = "RFID accepted · 3 due vaccines",
            primaryTag = "901007000503790",
            secondaryTag = null,
            vaccineLabel = "ET+TT · PPR · FMD",
            hint = "One continuous clip covers all due vaccines for this animal.",
        )
    }

    @Test
    fun operatorReadyToSubmitClip() = shot("scan_auto_proof_04_operator_ready_to_submit_clip") {
        AutoProofCameraFrame(
            title = "Review proof",
            stateLabel = "Operator stopped recording",
            primaryTag = "901007000503785",
            secondaryTag = null,
            vaccineLabel = "ET+TT",
            hint = "Submit queues the clip locally and upload starts in background.",
            primaryButtonLabel = "Submit proof",
            footer = "Proof uploads in background",
        )
    }

    @Test
    fun autoClosedUploading() = shot("scan_auto_proof_05_auto_closed_uploading") {
        ScanScreen(
            state = baseState().copy(
                ringDone = 1,
                doneCount = 1,
                pendingCount = 4,
                feed = listOf(
                    ScanFeedEntry(
                        primaryTag = "901007000503785",
                        secondaryTag = null,
                        vaccineLabel = "ET+TT · proof uploading",
                        status = ScanStatus.DONE,
                        scannedAtLabel = "Proof queued · uploading now",
                    ),
                ),
                footNote = "Proof is uploading in background. Keep scanning.",
            ),
        )
    }

    @Test
    fun uploadFailedRescanToReplace() = shot("scan_auto_proof_06_upload_failed_rescan_to_replace") {
        ScanScreen(
            state = baseState().copy(
                ringDone = 1,
                doneCount = 1,
                pendingCount = 4,
                duplicateNotice = "1 proof failed · auto retrying in background",
                feed = listOf(
                    ScanFeedEntry(
                        primaryTag = "901007000503785",
                        secondaryTag = null,
                        vaccineLabel = "ET+TT · upload failed · auto retry",
                        status = ScanStatus.DONE,
                        scannedAtLabel = "Proof preserved locally",
                        tone = ScanFeedTone.DUPLICATE,
                    ),
                ),
                footNote = "No per-row retry buttons. Outbox retries automatically; rescan this RFID only to replace the clip.",
            ),
        )
    }

    @Test
    fun rescanFailedAnimalReopensCamera() = shot("scan_auto_proof_07_rescan_failed_reopens_camera") {
        AutoProofCameraFrame(
            title = "Replacing proof",
            stateLabel = "Same RFID scanned · previous upload failed",
            primaryTag = "901007000503785",
            secondaryTag = null,
            vaccineLabel = "ET+TT",
            hint = "New clip replaces the failed one after submit.",
        )
    }

    @Test
    fun duplicateDoesNotOpenCamera() = shot("scan_auto_proof_08_duplicate_no_camera") {
        ScanScreen(
            state = baseState().copy(
                ringDone = 1,
                doneCount = 1,
                pendingCount = 4,
                duplicateNotice = "Already scanned · proof ready · ET+TT",
                feed = listOf(
                    ScanFeedEntry(
                        primaryTag = "901007000503785",
                        secondaryTag = null,
                        vaccineLabel = "already scanned · proof ready · ET+TT",
                        status = ScanStatus.DONE,
                        scannedAtLabel = "Scanned Tue, 5:02 PM IST",
                        tone = ScanFeedTone.DUPLICATE,
                    ),
                ),
                footNote = "Camera does not open for duplicate/proof-ready scans.",
            ),
        )
    }

    @Test
    fun unknownDoesNotOpenCamera() = shot("scan_auto_proof_09_unknown_no_camera") {
        ScanScreen(
            state = baseState().copy(
                error = ScanError("Unknown tag · not in this shed", "901007000504379"),
                skippedCount = 1,
                feed = listOf(
                    ScanFeedEntry(
                        primaryTag = "901007000504379",
                        secondaryTag = null,
                        vaccineLabel = "unknown tag · no proof recorded",
                        status = ScanStatus.SKIPPED,
                        scannedAtLabel = "Scanned Tue, 5:03 PM IST",
                    ),
                ),
                footNote = "Camera opens only after a valid due animal match.",
            ),
        )
    }

    @Test
    fun wrongShedDoesNotOpenCamera() = shot("scan_auto_proof_10_wrong_shed_no_camera") {
        ScanScreen(
            state = baseState().copy(
                error = ScanError("Wrong shed · expected Shed 1, now in Shed 2", "901007000504382"),
                skippedCount = 1,
                feed = listOf(
                    ScanFeedEntry(
                        primaryTag = "901007000504382",
                        secondaryTag = "901007000504383",
                        vaccineLabel = "wrong shed · no proof recorded",
                        status = ScanStatus.SKIPPED,
                        scannedAtLabel = "Scanned Tue, 5:04 PM IST",
                    ),
                ),
                footNote = "No camera for wrong shed scans. Keep expected/current shed visible.",
            ),
        )
    }

    private fun shot(name: String, content: @Composable () -> Unit) {
        paparazzi.snapshot(name = name) {
            GoatOsTheme {
                ProvideAppLocale {
                    Box(Modifier.fillMaxSize().background(MeshaColors.Bg)) {
                        content()
                    }
                }
            }
        }
    }
}

@Composable
private fun AutoProofCameraFrame(
    title: String,
    stateLabel: String,
    primaryTag: String,
    secondaryTag: String?,
    vaccineLabel: String,
    hint: String,
    primaryButtonLabel: String = "Stop recording",
    footer: String = "Proof uploads in background",
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
                CameraGrid()
                Box(
                    Modifier
                        .align(Alignment.TopCenter)
                        .padding(top = 18.dp)
                        .clip(RoundedCornerShape(999.dp))
                        .background(Color(0xAA102018))
                        .padding(horizontal = 14.dp, vertical = 7.dp),
                ) {
                    Text("LIVE PROOF · AUTO RECORDING", color = MeshaColors.Brand, fontSize = 11.sp, fontWeight = FontWeight.W800)
                }
                AnimalStamp(
                    primaryTag = primaryTag,
                    secondaryTag = secondaryTag,
                    vaccineLabel = vaccineLabel,
                    modifier = Modifier.align(Alignment.BottomStart).padding(16.dp),
                )
                RecordingDot(Modifier.align(Alignment.TopEnd).padding(18.dp))
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
                Text(primaryButtonLabel, fontSize = 15.sp, fontWeight = FontWeight.W800)
            }
            Spacer(Modifier.height(8.dp))
            Text(footer, color = MeshaColors.Faint, fontSize = 11.sp, textAlign = TextAlign.Center, modifier = Modifier.fillMaxWidth())
        }
    }
}

@Composable
private fun AnimalStamp(primaryTag: String, secondaryTag: String?, vaccineLabel: String, modifier: Modifier = Modifier) {
    Column(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(18.dp))
            .background(Color(0xD9111F18))
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(18.dp))
            .padding(14.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(primaryTag, color = MeshaColors.Ink, fontSize = 18.sp, fontWeight = FontWeight.W800, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (secondaryTag != null) {
                Spacer(Modifier.width(8.dp))
                Text("2 tags", color = MeshaColors.Muted, fontSize = 11.sp, fontWeight = FontWeight.W800, modifier = Modifier.clip(RoundedCornerShape(8.dp)).background(MeshaColors.Surf2).padding(horizontal = 7.dp, vertical = 3.dp))
            }
        }
        secondaryTag?.let {
            Spacer(Modifier.height(4.dp))
            Text(it, color = MeshaColors.Muted, fontSize = 13.sp, fontFamily = FontFamily.Monospace)
        }
        Spacer(Modifier.height(8.dp))
        Text(vaccineLabel, color = MeshaColors.Brand, fontSize = 14.sp, fontWeight = FontWeight.W800)
    }
}

@Composable
private fun RecordingDot(modifier: Modifier = Modifier) {
    Row(modifier.clip(RoundedCornerShape(999.dp)).background(Color(0xAA2A1111)).padding(horizontal = 10.dp, vertical = 6.dp), verticalAlignment = Alignment.CenterVertically) {
        Box(Modifier.size(8.dp).clip(CircleShape).background(MeshaColors.Danger))
        Spacer(Modifier.width(6.dp))
        Text("REC", color = MeshaColors.Danger, fontSize = 11.sp, fontWeight = FontWeight.W800)
    }
}

@Composable
private fun CameraGrid() {
    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        CircularProgressIndicator(color = Color(0x333E8B57), strokeWidth = 2.dp, modifier = Modifier.size(220.dp))
        Icon(MeshaIcons.Video, contentDescription = null, tint = Color(0x2257D06D), modifier = Modifier.size(74.dp))
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
    vaccineGroups = listOf(VaccineGroup("et-tt", "ET+TT", 0, 5, true)),
    listTitle = "Scanned goats",
    submitLabel = "Finalize shed",
    canSubmit = false,
    scanEnabled = true,
    lastSyncedAt = null,
)
