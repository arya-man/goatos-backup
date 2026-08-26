package sg.mesha.goatos.ui

import java.io.File
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class PcCareCopyAndProofLayoutContractTest {
    @Test
    fun pcCareDoesNotInventSubmittedForReviewCopy() {
        val files = listOf(
            "../feature/feature-pccare/src/main/kotlin/sg/mesha/goatos/feature/pccare/PcCareModels.kt",
            "../feature/feature-pccare/src/main/kotlin/sg/mesha/goatos/feature/pccare/PcCareTaskScreen.kt",
            "../feature/feature-pccare/src/main/kotlin/sg/mesha/goatos/feature/pccare/PcCareWorklistScreen.kt",
            "src/main/kotlin/sg/mesha/goatos/viewmodel/PcCareTaskViewModel.kt",
            "src/main/kotlin/sg/mesha/goatos/viewmodel/PcCareWorklistViewModel.kt",
        ).map(::File)

        files.forEach { file ->
            val source = file.readText()
            assertFalse(
                "${file.name} must use existing review language like In review/Submitted.",
                source.contains("Sent for checking"),
            )
        }
    }

    @Test
    fun stockTaskProofUiKeepsPhotoAndVideoEvidenceSeparate() {
        val source = File(
            "../feature/feature-pccare/src/main/kotlin/sg/mesha/goatos/feature/pccare/PcCareTaskScreen.kt",
        ).readText()

        assertTrue(source.contains("Photo evidence"))
        assertTrue(source.contains("Video evidence"))
        assertTrue(source.contains("taskProofPhotoSlot"))
        assertTrue(source.contains("taskProofVideoSlot"))
        assertTrue(source.contains("PcCareTaskProofAction"))
        assertTrue(source.contains("heightIn(min = 82.dp)"))
        assertTrue(source.contains("RoundedCornerShape(18.dp)"))
    }

    @Test
    fun drivesListDoesNotExposeCalendarFilterChrome() {
        val source = File(
            "../feature/feature-calendar/src/main/kotlin/sg/mesha/goatos/feature/calendar/CalendarScreen.kt",
        ).readText()

        assertTrue(source.contains("presentation != CalendarPresentation.DriveList"))
        assertTrue(source.contains("onOpenFilters"))
    }
}
