package sg.mesha.goatos.capture

import java.io.File
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ProofCameraSurfaceContractTest {
    @Test
    fun cameraSurfaceDoesNotOwnCapturedProofPreviewOrAcceptance() {
        val cameraFiles = listOf(
            "src/main/kotlin/sg/mesha/goatos/capture/PhotoCaptureLauncher.kt",
            "src/main/kotlin/sg/mesha/goatos/capture/InAppVideoRecorder.kt",
        ).map(::File)

        cameraFiles.forEach { file ->
            val source = file.readText()
            assertFalse(
                "${file.name} must stay camera-only. Captured proof preview belongs on the feature task screen.",
                source.contains("ProofMediaPreview"),
            )
            assertFalse(
                "${file.name} must not add an accept/review step inside camera.",
                source.contains("Use proof"),
            )
            assertFalse(
                "${file.name} must not render captured-media review UI inside camera.",
                source.contains("CapturedPhotoReview") || source.contains("CapturedVideoReview"),
            )
            assertFalse(
                "${file.name} must not render feature submit/replace actions inside camera.",
                source.contains("Submit task") || source.contains("Replace photo") || source.contains("Replace video"),
            )
        }
    }

    @Test
    fun inventoryVaccineStockHasDedicatedCameraPromptSource() {
        val source = File("src/main/kotlin/sg/mesha/goatos/capture/ProofCaptureSource.kt").readText()
        val viewModel = File("src/main/kotlin/sg/mesha/goatos/viewmodel/PcCareTaskViewModel.kt").readText()

        assertTrue(source.contains("INVENTORY_VACCINE_STOCK"))
        assertTrue(viewModel.contains("ProofCapturePrompt.INVENTORY_VACCINE_STOCK"))
    }
}
