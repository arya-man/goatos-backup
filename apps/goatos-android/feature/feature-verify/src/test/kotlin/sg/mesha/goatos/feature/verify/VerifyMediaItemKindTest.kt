package sg.mesha.goatos.feature.verify

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The player a proof opens in is decided by the BACKEND mime alone. A blank or non-media mime is a
 * proof whose kind nobody could tell (an `either` SOP slot the proof register could not type): it
 * is UNKNOWN and gets a tap-armed tile, never a guessed video player.
 *
 * telemetry:exempt pure mapping unit test — renders no surface.
 */
class VerifyMediaItemKindTest {
    private fun item(mime: String) = VerifyMediaItem(signedUrl = "https://x/p", mimeType = mime, proofSubject = "p")

    @Test
    fun `image mimes are photos and keep isPhoto`() {
        assertEquals(VerifyMediaKind.PHOTO, item("image/jpeg").kind)
        assertEquals(VerifyMediaKind.PHOTO, item("IMAGE/PNG").kind)
        assertTrue(item("image/jpeg").isPhoto)
    }

    @Test
    fun `video mimes are videos`() {
        assertEquals(VerifyMediaKind.VIDEO, item("video/mp4").kind)
        assertFalse(item("video/mp4").isPhoto)
    }

    @Test
    fun `blank or non-media mimes are unknown, never a guessed video`() {
        assertEquals(VerifyMediaKind.UNKNOWN, item("").kind)
        assertEquals(VerifyMediaKind.UNKNOWN, item("application/octet-stream").kind)
        assertFalse(item("").isPhoto)
    }
}
