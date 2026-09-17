package sg.mesha.goatos.feature.counts

import org.junit.Assert.assertEquals
import org.junit.Test

/** Realme E2E 2026-09-17: a photo slot's "Take live photo" button showed a video camera. */
class ShiftingSlotButtonGlyphTest {
    @Test
    fun `a photo slot's button shows the photo icon`() {
        assertEquals(ShiftingCaptureGlyph.PHOTO, shiftingSlotButtonGlyph(kind = "photo", captured = false, capturedPhoto = false))
        assertEquals(ShiftingCaptureGlyph.PHOTO, shiftingSlotButtonGlyph(kind = "photo", captured = true, capturedPhoto = true))
    }

    @Test
    fun `an either slot retaking a photo shows the photo icon, recording video shows video`() {
        assertEquals(ShiftingCaptureGlyph.PHOTO, shiftingSlotButtonGlyph(kind = "either", captured = true, capturedPhoto = true))
        assertEquals(ShiftingCaptureGlyph.VIDEO, shiftingSlotButtonGlyph(kind = "either", captured = false, capturedPhoto = false))
        assertEquals(ShiftingCaptureGlyph.VIDEO, shiftingSlotButtonGlyph(kind = "video", captured = true, capturedPhoto = false))
    }
}
