package sg.mesha.goatos.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
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

class ProofProcessingShowcaseScreenshotTest {

    @get:Rule
    val paparazzi = Paparazzi(deviceConfig = DeviceConfig.PIXEL_6)

    @Test
    fun vaccinationScanFeature() = shot("proof_processing_10_vaccination_scan") {
        CameraScanShowcase(
            title = "Vaccination scan",
            subtitle = "RFID scan opens camera automatically for each goat proof",
            subject = "RFID 004821",
            meta = "ET+TT · Gandhi 1",
            phase = ProofUiPhase.COMPRESSING,
            overlayLines = listOf(
                "Aug 11, 2026 2:48 PM",
                "Operator: Raviteja",
                "RFID: 004821",
                "Channapatna - Sathanur Road",
                "Banduru Varagarahalli, Karnataka",
            ),
            queueRows = listOf(
                Triple("RFID 004821", "ET+TT · current capture", ProofUiPhase.COMPRESSING),
                Triple("RFID 004822", "PPR · waiting next scan", ProofUiPhase.PREPARING),
                Triple("RFID 004823", "FMD · uploaded", ProofUiPhase.UPLOADED),
                Triple("RFID 004824", "ET+TT · retry original", ProofUiPhase.RETRY_ORIGINAL),
            ),
        )
    }

    @Test
    fun vaccinationSubmitFormFeature() = shot("proof_processing_11_vaccination_submit_form") {
        VaccinationShedVideoShowcase()
    }

    @Test
    fun weighingIndividualFeature() = shot("proof_processing_12_weighing_individual") {
        WeighingIndividualShowcase()
    }

    @Test
    fun weighingShedFeature() = shot("proof_processing_13_weighing_shed") {
        WeighingLumpSumShowcase()
    }

    @Test
    fun birthDeathWorkflowFeature() = shot("proof_processing_14_birth_death_workflow") {
        FeatureShowcase("Birth / death workflow", "Action rows that require video evidence") {
            ProofStepCard("Birth workflow", "Kid evidence video", ProofUiPhase.PREPARING)
            ProofStepCard("Birth workflow", "Delivery/birth action video", ProofUiPhase.COMPRESSING)
            ProofStepCard("Death workflow", "Death evidence video", ProofUiPhase.UPLOADING)
            ProofStepCard("Death workflow", "Upload failed; retry original", ProofUiPhase.RETRY_ORIGINAL)
            ProofStepCard("Death workflow", "Needs fresh recording", ProofUiPhase.RECORD_AGAIN)
        }
    }

    @Test
    fun shiftingFeature() = shot("proof_processing_15_shifting") {
        FeatureShowcase("Shifting execute", "Movement proof plus high-priority feed proof") {
            ProofStepCard("Movement", "Destination shed video", ProofUiPhase.PREPARING)
            ProofStepCard("Movement", "Destination shed video", ProofUiPhase.COMPRESSING)
            ProofStepCard("High priority", "Feed packing video", ProofUiPhase.UPLOADING)
            ProofStepCard("High priority", "Feed given video", ProofUiPhase.UPLOADED)
            ProofStepCard("High priority", "Retry original proof upload", ProofUiPhase.RETRY_ORIGINAL)
        }
    }

    @Test
    fun feedDistributionFeature() = shot("proof_processing_16_feed_distribution") {
        FeatureShowcase("Feed distribution", "Mandatory feed video, water proof may be video") {
            ProofStepCard("Feed distribution", "Feed video", ProofUiPhase.PREPARING)
            ProofStepCard("Feed distribution", "Feed video", ProofUiPhase.COMPRESSING)
            ProofStepCard("Feed distribution", "Water video", ProofUiPhase.UPLOADING)
            ProofStepCard("Feed distribution", "Water video", ProofUiPhase.UPLOADED)
            ProofStepCard("Feed distribution", "Water video retry", ProofUiPhase.RETRYING)
        }
    }

    @Test
    fun legacyFeedCompleteFeature() = shot("proof_processing_17_feed_complete_optional") {
        FeatureShowcase("Feed complete", "Optional video on the older feed completion path") {
            ProofStepCard("Feed complete", "Optional feeding video", ProofUiPhase.PREPARING)
            ProofStepCard("Feed complete", "Optional feeding video", ProofUiPhase.COMPRESSING)
            ProofStepCard("Feed complete", "Optional feeding video", ProofUiPhase.UPLOADING)
            ProofStepCard("Feed complete", "Optional feeding video", ProofUiPhase.UPLOADED)
        }
    }

    @Test
    fun feedPackingFeature() = shot("proof_processing_18_feed_packing") {
        FeatureShowcase("Feed packing", "Mandatory packing video") {
            ProofStepCard("Feed packing", "Packing video", ProofUiPhase.PREPARING)
            ProofStepCard("Feed packing", "Packing video", ProofUiPhase.COMPRESSING)
            ProofStepCard("Feed packing", "Packing video", ProofUiPhase.UPLOADING)
            ProofStepCard("Feed packing", "Packing video", ProofUiPhase.UPLOADED)
            ProofStepCard("Feed packing", "Packing video retry", ProofUiPhase.RETRY_ORIGINAL)
        }
    }

    @Test
    fun feedTransportFeature() = shot("proof_processing_19_feed_transport") {
        FeatureShowcase("Feed transport", "Fresh live-camera transport video") {
            ProofStepCard("Feed transport", "Truck/loading handoff video", ProofUiPhase.PREPARING)
            ProofStepCard("Feed transport", "Truck/loading handoff video", ProofUiPhase.COMPRESSING)
            ProofStepCard("Feed transport", "Truck/loading handoff video", ProofUiPhase.UPLOADING)
            ProofStepCard("Feed transport", "Truck/loading handoff video", ProofUiPhase.UPLOADED)
            ProofStepCard("Feed transport", "Retry original upload", ProofUiPhase.RETRY_ORIGINAL)
        }
    }

    @Test
    fun milkPreparationFeature() = shot("proof_processing_20_milk_preparation") {
        FeatureShowcase("Milk preparation", "Multiple preparation step videos") {
            ProofStepCard("Milk preparation", "Boil milk video", ProofUiPhase.PREPARING)
            ProofStepCard("Milk preparation", "Mix citric acid video", ProofUiPhase.COMPRESSING)
            ProofStepCard("Milk preparation", "Storage video", ProofUiPhase.UPLOADING)
            ProofStepCard("Milk preparation", "Feeding-ready proof", ProofUiPhase.UPLOADED)
            ProofStepCard("Milk preparation", "Retry original upload", ProofUiPhase.RETRY_ORIGINAL)
        }
    }

    @Test
    fun milkFeedingFeature() = shot("proof_processing_21_milk_feeding") {
        FeatureShowcase("Milk feeding", "Mandatory feeding proof videos") {
            ProofStepCard("Milk feeding", "Cow milk attempt video", ProofUiPhase.PREPARING)
            ProofStepCard("Milk feeding", "Udder milk attempt video", ProofUiPhase.COMPRESSING)
            ProofStepCard("Milk feeding", "ORS feeding video", ProofUiPhase.UPLOADING)
            ProofStepCard("Milk feeding", "Proof uploaded", ProofUiPhase.UPLOADED)
            ProofStepCard("Milk feeding", "Record fresh evidence", ProofUiPhase.RECORD_AGAIN)
        }
    }

    private fun shot(name: String, content: @Composable () -> Unit) {
        paparazzi.snapshot(name = name) {
            GoatOsTheme {
                ProvideAppLocale {
                    Box(Modifier.fillMaxSize().background(MeshaColors.PageBg)) {
                        content()
                    }
                }
            }
        }
    }
}

@Suppress("unused")
private val RequiredProofVideoSurfaceIds = listOf(
    "vaccination_scan",
    "vaccination_shed",
    "weighing_individual",
    "weighing_lumpsum",
    "birth_death_workflow",
    "shifting",
    "feed_distribution",
    "feed_complete",
    "feed_packing",
    "feed_transport",
    "milk_preparation",
    "milk_feeding",
)

private enum class ProofUiPhase(
    val label: String,
    val tone: ProofTone,
) {
    PREPARING("Preparing proof...", ProofTone.WORKING),
    COMPRESSING("Compressing proof...", ProofTone.WORKING),
    UPLOADING("Uploading proof...", ProofTone.WORKING),
    UPLOADING_ORIGINAL("Uploading original proof...", ProofTone.WARNING),
    UPLOADED("Proof uploaded", ProofTone.OK),
    RETRYING("Upload failed. Retrying", ProofTone.WARNING),
    RETRY_ORIGINAL("Retrying original proof upload...", ProofTone.WARNING),
    RECORD_AGAIN("Record again", ProofTone.DANGER),
}

private enum class ProofTone { WORKING, OK, WARNING, DANGER }

@Composable
private fun ShowcaseScreen(
    title: String,
    subtitle: String,
    content: @Composable ColumnScope.() -> Unit,
) {
    Column(
        Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Text(title, color = MeshaColors.Ink, fontSize = 22.sp, fontWeight = FontWeight.W900)
        Text(subtitle, color = MeshaColors.Muted, fontSize = 13.sp, fontWeight = FontWeight.W600)
        Spacer(Modifier.size(2.dp))
        content()
    }
}

@Composable
private fun FeatureShowcase(
    title: String,
    subtitle: String,
    content: @Composable ColumnScope.() -> Unit,
) {
    ShowcaseScreen(title, subtitle) {
        ProofFlowLegend()
        content()
    }
}

@Composable
private fun CameraScanShowcase(
    title: String,
    subtitle: String,
    subject: String,
    meta: String,
    phase: ProofUiPhase,
    overlayLines: List<String>,
    queueRows: List<Triple<String, String, ProofUiPhase>>,
) {
    ShowcaseScreen(title, subtitle) {
        Box(
            Modifier
                .fillMaxWidth()
                .height(740.dp)
                .clip(RoundedCornerShape(18.dp))
                .background(Color(0xFF111915)),
        ) {
            Column(Modifier.fillMaxSize()) {
                Box(
                    Modifier
                        .fillMaxWidth()
                        .height(245.dp)
                        .background(Color(0xFF2F3F34)),
                ) {
                    Box(
                        Modifier
                            .align(Alignment.Center)
                            .width(210.dp)
                            .height(92.dp)
                            .clip(RoundedCornerShape(10.dp))
                            .background(Color(0xFF0C0F0D))
                            .border(1.dp, Color.White.copy(alpha = 0.22f), RoundedCornerShape(10.dp)),
                        contentAlignment = Alignment.Center,
                    ) {
                        Text(
                            "19.1 kg",
                            color = Color(0xFFE9F6EA),
                            fontSize = 40.sp,
                            fontWeight = FontWeight.W900,
                        )
                    }
                    Box(
                        Modifier
                            .align(Alignment.BottomEnd)
                            .clip(RoundedCornerShape(topStart = 8.dp))
                            .background(Color.Black.copy(alpha = 0.62f))
                            .padding(horizontal = 8.dp, vertical = 6.dp),
                    ) {
                        Column(horizontalAlignment = Alignment.End, verticalArrangement = Arrangement.spacedBy(1.dp)) {
                            overlayLines.forEach { line ->
                                Text(line, color = Color.White, fontSize = 10.sp, fontWeight = FontWeight.W700)
                            }
                        }
                    }
                    Row(
                        Modifier
                            .align(Alignment.TopStart)
                            .padding(12.dp)
                            .clip(RoundedCornerShape(999.dp))
                            .background(Color.Black.copy(alpha = 0.58f))
                            .padding(horizontal = 10.dp, vertical = 6.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(7.dp),
                    ) {
                        Icon(MeshaIcons.Video, contentDescription = null, tint = Color.White, modifier = Modifier.size(15.dp))
                        Text("Camera recording", color = Color.White, fontSize = 12.sp, fontWeight = FontWeight.W900)
                    }
                }
                Column(
                    Modifier
                        .fillMaxWidth()
                        .padding(12.dp),
                    verticalArrangement = Arrangement.spacedBy(10.dp),
                ) {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                        ProofStatusIcon(phase)
                        Column(Modifier.weight(1f)) {
                            Text(subject, color = MeshaColors.Ink, fontSize = 15.sp, fontWeight = FontWeight.W900)
                            Text(meta, color = MeshaColors.Muted, fontSize = 12.sp, fontWeight = FontWeight.W700)
                        }
                        Text(
                            phase.label,
                            color = phase.textColor(),
                            fontSize = 11.sp,
                            fontWeight = FontWeight.W900,
                            modifier = Modifier
                                .clip(RoundedCornerShape(999.dp))
                                .background(phase.bgColor())
                                .padding(horizontal = 8.dp, vertical = 5.dp),
                        )
                    }
                    ProofFlowLegend()
                    queueRows.forEach { row ->
                        ProofScanRow(row.first, row.second, row.third)
                    }
                }
            }
        }
    }
}

@Composable
private fun WeighingIndividualShowcase() {
    ShowcaseScreen("WEIGHING", "Kid Shed A") {
        HeaderRow("Kid Shed A")
        ReaderCard("IDT RHLS-3", "Reader connected")
        Text("4 animals captured", color = MeshaColors.Ink, fontSize = 18.sp, fontWeight = FontWeight.W900)
        WeighingAnimalCard(
            tag = "901007000504407",
            weight = "15.0 kg",
            phase = ProofUiPhase.UPLOADED,
            status = "Weight saved · Video synced 2:21 PM IST",
        )
        WeighingAnimalCard(
            tag = "901007000504332",
            weight = "12.0 kg",
            phase = ProofUiPhase.COMPRESSING,
            status = "Weight saved · Compressing proof...",
        )
        WeighingAnimalCard(
            tag = "901007000504418",
            weight = "19.1 kg",
            phase = ProofUiPhase.UPLOADING,
            status = "Weight saved · Uploading proof...",
        )
        WeighingAnimalCard(
            tag = "901007000504419",
            weight = "20.2 kg",
            phase = ProofUiPhase.RETRY_ORIGINAL,
            status = "Weight saved · Retrying original proof upload...",
        )
    }
}

@Composable
private fun WeighingLumpSumShowcase() {
    ShowcaseScreen("WEIGHING", "Kid Shed C") {
        HeaderRow("Kid Shed C")
        Column(
            Modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(14.dp))
                .background(MeshaColors.Surf)
                .border(1.dp, MeshaColors.Brand.copy(alpha = 0.20f), RoundedCornerShape(14.dp))
                .padding(14.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            FieldBox("Total weight", "100", "kg")
            FieldBox("Animal count", "10", "")
            Text("Average 10.00 kg per animal", color = MeshaColors.Brand, fontSize = 16.sp, fontWeight = FontWeight.W900)
            Text("2 / 5 videos uploaded", color = MeshaColors.Muted, fontSize = 16.sp, fontWeight = FontWeight.W900)
            LumpVideoRow("Video 1 · mandatory", ProofUiPhase.UPLOADED)
            LumpVideoRow("Video 2 · optional", ProofUiPhase.UPLOADED)
            LumpVideoRow("Video 3 · optional", ProofUiPhase.COMPRESSING)
            LumpVideoRow("Video 4 · optional", ProofUiPhase.UPLOADING)
            LumpVideoRow("Video 5 · optional", ProofUiPhase.RETRY_ORIGINAL)
            Text(
                "Add another video",
                color = MeshaColors.Muted,
                fontSize = 14.sp,
                fontWeight = FontWeight.W900,
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(12.dp))
                    .background(Color.Black.copy(alpha = 0.28f))
                    .border(1.dp, MeshaColors.Brand.copy(alpha = 0.20f), RoundedCornerShape(12.dp))
                    .padding(vertical = 14.dp),
            )
        }
    }
}

@Composable
private fun VaccinationShedVideoShowcase() {
    ShowcaseScreen("Vaccination", "Shed 1 proof videos") {
        HeaderRow("Shed 1 Scan")
        ReaderCard("IDT RHLS-3", "Reader connected")
        Column(
            Modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(14.dp))
                .background(MeshaColors.Surf)
                .border(1.dp, MeshaColors.Brand.copy(alpha = 0.20f), RoundedCornerShape(14.dp))
                .padding(14.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text("Per-shed video proof", color = MeshaColors.Ink, fontSize = 20.sp, fontWeight = FontWeight.W900)
                    Text("1 mandatory · 4 optional · max 5 videos", color = MeshaColors.Muted, fontSize = 13.sp, fontWeight = FontWeight.W800)
                }
                Text("2 / 5", color = MeshaColors.Brand, fontSize = 16.sp, fontWeight = FontWeight.W900)
            }
            LumpVideoRow("Video 1 · mandatory", ProofUiPhase.UPLOADED)
            LumpVideoRow("Video 2 · optional", ProofUiPhase.COMPRESSING)
            LumpVideoRow("Video 3 · optional", ProofUiPhase.UPLOADING)
            LumpVideoRow("Video 4 · optional", ProofUiPhase.RETRY_ORIGINAL)
            LumpVideoRow("Video 5 · optional", ProofUiPhase.PREPARING)
            Text(
                "Add another video",
                color = MeshaColors.Muted,
                fontSize = 14.sp,
                fontWeight = FontWeight.W900,
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(12.dp))
                    .background(Color.Black.copy(alpha = 0.28f))
                    .border(1.dp, MeshaColors.Brand.copy(alpha = 0.20f), RoundedCornerShape(12.dp))
                    .padding(vertical = 14.dp),
            )
        }
    }
}

@Composable
private fun HeaderRow(title: String) {
    Row(
        Modifier.fillMaxWidth(),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        Box(
            Modifier
                .size(54.dp)
                .clip(RoundedCornerShape(14.dp))
                .background(MeshaColors.Surf2),
            contentAlignment = Alignment.Center,
        ) {
            Text("‹", color = MeshaColors.Ink, fontSize = 34.sp, fontWeight = FontWeight.W500)
        }
        Column(Modifier.weight(1f)) {
            Text(title, color = MeshaColors.Ink, fontSize = 28.sp, fontWeight = FontWeight.W900)
            Text(title, color = MeshaColors.Muted, fontSize = 17.sp, fontWeight = FontWeight.W800)
        }
    }
}

@Composable
private fun ReaderCard(title: String, body: String) {
    Row(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(MeshaColors.BrandTint)
            .border(1.dp, MeshaColors.Brand, RoundedCornerShape(12.dp))
            .padding(16.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text("⌁", color = MeshaColors.Brand, fontSize = 24.sp, fontWeight = FontWeight.W900)
        Column {
            Text(title, color = MeshaColors.Ink, fontSize = 19.sp, fontWeight = FontWeight.W900)
            Text(body, color = MeshaColors.Brand, fontSize = 14.sp, fontWeight = FontWeight.W800)
        }
    }
}

@Composable
private fun WeighingAnimalCard(tag: String, weight: String, phase: ProofUiPhase, status: String) {
    Column(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(MeshaColors.BrandTint)
            .border(1.dp, phase.borderColor(), RoundedCornerShape(12.dp))
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(5.dp)) {
                Text(tag, color = MeshaColors.Ink, fontSize = 18.sp, fontWeight = FontWeight.W900)
                Text("Scanned 29 Jul, 2:21 PM IST", color = MeshaColors.Muted, fontSize = 13.sp, fontWeight = FontWeight.W700)
                Text(status, color = phase.textColor(), fontSize = 13.sp, fontWeight = FontWeight.W900)
            }
            ProofStatusIcon(phase)
        }
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text("Weight $weight", color = MeshaColors.Ink, fontSize = 18.sp, fontWeight = FontWeight.W900, modifier = Modifier.weight(1f))
            Text("Edit weight", color = MeshaColors.Muted, fontSize = 13.sp, fontWeight = FontWeight.W900, modifier = Modifier.clip(RoundedCornerShape(14.dp)).background(MeshaColors.Surf2).padding(horizontal = 14.dp, vertical = 10.dp))
        }
        Text(
            if (phase == ProofUiPhase.UPLOADED) "Re-upload" else phase.label,
            color = phase.textColor(),
            fontSize = 14.sp,
            fontWeight = FontWeight.W900,
            modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(14.dp)).background(Color.Black.copy(alpha = 0.28f)).padding(vertical = 12.dp),
        )
    }
}

@Composable
private fun FieldBox(label: String, value: String, suffix: String) {
    Column(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .border(1.dp, MeshaColors.Brand.copy(alpha = 0.20f), RoundedCornerShape(12.dp))
            .padding(14.dp),
    ) {
        Text(label, color = MeshaColors.Muted, fontSize = 14.sp, fontWeight = FontWeight.W800)
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(value, color = MeshaColors.Ink, fontSize = 20.sp, fontWeight = FontWeight.W700, modifier = Modifier.weight(1f))
            if (suffix.isNotBlank()) Text(suffix, color = MeshaColors.Muted, fontSize = 18.sp, fontWeight = FontWeight.W800)
        }
    }
}

@Composable
private fun LumpVideoRow(label: String, phase: ProofUiPhase) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(
            Modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(10.dp))
                .background(Color.Black.copy(alpha = 0.36f))
                .padding(horizontal = 14.dp, vertical = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            ProofStatusIcon(phase, small = true)
            Text(label, color = MeshaColors.Ink, fontSize = 17.sp, fontWeight = FontWeight.W900, modifier = Modifier.weight(1f))
            Text(if (phase == ProofUiPhase.UPLOADED) "synced" else phase.label, color = phase.textColor(), fontSize = 13.sp, fontWeight = FontWeight.W900)
        }
        Row(horizontalArrangement = Arrangement.spacedBy(10.dp), modifier = Modifier.fillMaxWidth()) {
            Text("↻ Replace", color = Color(0xFF72B7FF), fontSize = 13.sp, fontWeight = FontWeight.W900, modifier = Modifier.weight(1f).clip(RoundedCornerShape(10.dp)).background(Color(0xFF17304A)).border(1.dp, Color(0xFF72B7FF), RoundedCornerShape(10.dp)).padding(vertical = 10.dp))
            Text("× Remove", color = Color(0xFFFF7C74), fontSize = 13.sp, fontWeight = FontWeight.W900, modifier = Modifier.weight(1f).clip(RoundedCornerShape(10.dp)).background(Color(0xFF462824)).border(1.dp, Color(0xFFFF7C74), RoundedCornerShape(10.dp)).padding(vertical = 10.dp))
        }
    }
}

@Composable
private fun ProofFlowLegend() {
    Column(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Surf2)
            .padding(12.dp),
        verticalArrangement = Arrangement.spacedBy(5.dp),
    ) {
        Text("Normal: prepare → compress → upload → uploaded", color = MeshaColors.Ink, fontSize = 12.sp, fontWeight = FontWeight.W800)
        Text("Compression exception: upload original, then retry original only", color = MeshaColors.Muted, fontSize = 11.sp, fontWeight = FontWeight.W600)
    }
}

@Composable
private fun SectionTitle(text: String) {
    Text(
        text,
        color = MeshaColors.Faint,
        fontSize = 11.sp,
        fontWeight = FontWeight.W900,
        modifier = Modifier.padding(top = 8.dp),
    )
}

@Composable
private fun ProofScanRow(id: String, meta: String, phase: ProofUiPhase) {
    Row(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, phase.borderColor(), RoundedCornerShape(14.dp))
            .padding(12.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        ProofStatusIcon(phase)
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
            Text(id, color = MeshaColors.Ink, fontSize = 14.sp, fontWeight = FontWeight.W800)
            Text(meta, color = MeshaColors.Faint, fontSize = 11.sp, fontWeight = FontWeight.W600)
        }
        Text(
            phase.label,
            color = phase.textColor(),
            fontSize = 11.sp,
            fontWeight = FontWeight.W800,
            modifier = Modifier
                .clip(RoundedCornerShape(999.dp))
                .background(phase.bgColor())
                .padding(horizontal = 8.dp, vertical = 5.dp),
        )
    }
}

@Composable
private fun ProofCard(title: String, body: String, phase: ProofUiPhase) {
    Column(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, phase.borderColor(), RoundedCornerShape(14.dp))
            .padding(13.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            ProofStatusIcon(phase)
            Column(Modifier.weight(1f)) {
                Text(title, color = MeshaColors.Ink, fontSize = 15.sp, fontWeight = FontWeight.W900)
                Text(body, color = MeshaColors.Muted, fontSize = 12.sp, fontWeight = FontWeight.W600)
            }
        }
        ProofProgressLine(phase)
    }
}

@Composable
private fun ProofStepCard(module: String, step: String, phase: ProofUiPhase) {
    Column(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, phase.borderColor(), RoundedCornerShape(14.dp))
            .padding(12.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            Box(
                Modifier
                    .size(34.dp)
                    .clip(RoundedCornerShape(10.dp))
                    .background(MeshaColors.Surf2),
                contentAlignment = Alignment.Center,
            ) {
                Icon(MeshaIcons.Video, contentDescription = null, tint = phase.textColor(), modifier = Modifier.size(17.dp))
            }
            Column(Modifier.weight(1f)) {
                Text(module, color = MeshaColors.Faint, fontSize = 11.sp, fontWeight = FontWeight.W700)
                Text(step, color = MeshaColors.Ink, fontSize = 14.sp, fontWeight = FontWeight.W900)
            }
            Text(
                if (phase == ProofUiPhase.UPLOADED) "Done" else "Video",
                color = phase.textColor(),
                fontSize = 10.sp,
                fontWeight = FontWeight.W900,
                modifier = Modifier.clip(RoundedCornerShape(999.dp)).background(phase.bgColor()).padding(horizontal = 7.dp, vertical = 3.dp),
            )
        }
        ProofProgressLine(phase)
    }
}

@Composable
private fun ProofProgressLine(phase: ProofUiPhase) {
    Row(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(10.dp))
            .background(phase.bgColor())
            .padding(horizontal = 10.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        ProofStatusIcon(phase, small = true)
        Text(phase.label, color = phase.textColor(), fontSize = 12.sp, fontWeight = FontWeight.W800)
    }
}

@Composable
private fun ProofStatusIcon(phase: ProofUiPhase, small: Boolean = false) {
    val size = if (small) 14.dp else 24.dp
    if (phase.tone == ProofTone.WORKING || phase == ProofUiPhase.UPLOADING_ORIGINAL) {
        CircularProgressIndicator(
            modifier = Modifier.size(size),
            strokeWidth = if (small) 2.dp else 3.dp,
            color = phase.textColor(),
        )
        return
    }
    Box(
        Modifier
            .size(size)
            .clip(CircleShape)
            .background(phase.textColor()),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = when (phase.tone) {
                ProofTone.OK -> "✓"
                ProofTone.WARNING -> "!"
                ProofTone.DANGER -> "!"
                ProofTone.WORKING -> ""
            },
            color = Color.White,
            fontSize = if (small) 9.sp else 13.sp,
            fontWeight = FontWeight.W900,
        )
    }
}

@Composable private fun ProofUiPhase.textColor(): Color = when (tone) {
    ProofTone.WORKING -> MeshaColors.BrandD
    ProofTone.OK -> MeshaColors.Ok
    ProofTone.WARNING -> MeshaColors.Warn
    ProofTone.DANGER -> MeshaColors.Danger
}

@Composable private fun ProofUiPhase.bgColor(): Color = when (tone) {
    ProofTone.WORKING -> MeshaColors.BrandTint
    ProofTone.OK -> MeshaColors.OkX
    ProofTone.WARNING -> MeshaColors.WarnX
    ProofTone.DANGER -> MeshaColors.Danger.copy(alpha = 0.10f)
}

@Composable private fun ProofUiPhase.borderColor(): Color = when (tone) {
    ProofTone.WORKING -> MeshaColors.Brand.copy(alpha = 0.25f)
    ProofTone.OK -> MeshaColors.Ok.copy(alpha = 0.35f)
    ProofTone.WARNING -> MeshaColors.Warn.copy(alpha = 0.35f)
    ProofTone.DANGER -> MeshaColors.Danger.copy(alpha = 0.35f)
}
