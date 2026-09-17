package sg.mesha.goatos.feature.weighing

import org.junit.Assert.assertEquals
import org.junit.Test

/** Realme E2E 2026-09-17: the whole-pen button said "Capture group video" over an authored slot title. */
class WholePenPrimaryCaptureLabelTest {
    @Test
    fun `the first capture of an authored slot uses the SOP's own title`() {
        val sop = WeighingCaptureSopUi(primaryPenSlotTitle = "Whole pen on the scale", primaryPenSlotKind = "video")
        assertEquals(WholePenCaptureLabel.Authored("Whole pen on the scale"), wholePenPrimaryCaptureLabel(sop, captured = 0))
        val photo = WeighingCaptureSopUi(primaryPenSlotTitle = "Scale display photo", primaryPenSlotKind = "photo")
        assertEquals(WholePenCaptureLabel.Authored("Scale display photo"), wholePenPrimaryCaptureLabel(photo, captured = 0))
    }

    @Test
    fun `a seeded pen without a title and later captures keep today's labels`() {
        assertEquals(WholePenCaptureLabel.GroupVideo, wholePenPrimaryCaptureLabel(WeighingCaptureSopUi(), captured = 0))
        val sop = WeighingCaptureSopUi(primaryPenSlotTitle = "Whole pen on the scale")
        assertEquals(WholePenCaptureLabel.AddAnotherVideo, wholePenPrimaryCaptureLabel(sop, captured = 1))
        val photo = WeighingCaptureSopUi(primaryPenSlotTitle = "Scale display photo", primaryPenSlotKind = "photo")
        assertEquals(WholePenCaptureLabel.TakePhoto, wholePenPrimaryCaptureLabel(photo, captured = 1))
    }
}
