package sg.mesha.goatos.core.ui.sop

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * Realme E2E 2026-09-17: a feed SOP card said "upload is in progress" while nothing was uploading
 * -- a REQUIRED capture had simply not been recorded yet. The status line must name what is still
 * needed, by the card's own title, and keep the in-progress words for a real upload.
 */
class SopCardProgressTest {
    private fun slot(key: String, title: String, captured: Boolean, status: SopProofStatus, kind: String = SopSlotCaptureKind.VIDEO) =
        SopSlotUi(slotKey = key, title = title, captureKind = kind, captured = captured, status = status)

    @Test
    fun `missing scale photo with the packing video ready names the photo and the unanswered question`() {
        val card = SopCardUi(
            slots = listOf(
                slot("packing_video", "Packing proof video", captured = true, status = SopProofStatus.SYNCED),
                slot("scale_photo", "Scale reading photo", captured = false, status = SopProofStatus.EMPTY, kind = SopSlotCaptureKind.PHOTO),
            ),
            questions = listOf(SopQuestionUi(id = "bags", kind = "number", title = "Bags packed", required = true)),
        )
        assertEquals(
            SopCardProgress.StillNeeded(listOf("Scale reading photo", "Bags packed")),
            card.progress(submitEnabled = false),
        )
    }

    @Test
    fun `a capture still missing while another operator's captures are ready is named, not called an upload`() {
        val card = SopCardUi(
            slots = listOf(
                slot("weight_photo", "Feed weight photo", captured = true, status = SopProofStatus.SYNCED, kind = SopSlotCaptureKind.PHOTO),
                slot("feed_video", "Feed video", captured = false, status = SopProofStatus.EMPTY),
                slot("water_video", "Water video", captured = true, status = SopProofStatus.SYNCED),
            ),
        )
        assertEquals(SopCardProgress.StillNeeded(listOf("Feed video")), card.progress(submitEnabled = false))
    }

    @Test
    fun `every capture recorded and one still uploading keeps the in-progress words`() {
        val card = SopCardUi(
            slots = listOf(
                slot("packing_video", "Packing proof video", captured = true, status = SopProofStatus.UPLOADING),
                slot("scale_photo", "Scale reading photo", captured = true, status = SopProofStatus.SYNCED, kind = SopSlotCaptureKind.PHOTO),
            ),
        )
        assertEquals(SopCardProgress.Uploading, card.progress(submitEnabled = false))
    }

    @Test
    fun `the seeded single-video card keeps its wording before and after capture`() {
        val empty = SopCardUi(slots = listOf(slot("v", "Packing video", captured = false, status = SopProofStatus.EMPTY)))
        assertEquals(SopCardProgress.NothingCaptured, empty.progress(submitEnabled = false))
        val uploading = SopCardUi(slots = listOf(slot("v", "Packing video", captured = true, status = SopProofStatus.QUEUED)))
        assertEquals(SopCardProgress.Uploading, uploading.progress(submitEnabled = false))
        assertEquals(SopCardProgress.Ready, uploading.progress(submitEnabled = true))
    }

    @Test
    fun `every capture recorded but a required question unanswered names the question`() {
        val card = SopCardUi(
            slots = listOf(slot("v", "Packing proof video", captured = true, status = SopProofStatus.SYNCED)),
            questions = listOf(SopQuestionUi(id = "bags", kind = "number", title = "Bags packed", required = true)),
        )
        assertEquals(SopCardProgress.StillNeeded(listOf("Bags packed")), card.progress(submitEnabled = false))
    }
}
