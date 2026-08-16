package sg.mesha.goatos.core.data.capture

import android.graphics.Bitmap
import java.io.File
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * INVARIANT regression (field bug 2026-08-15, twice): a freshly processed photo — exactly as the
 * processor produces it (JPEG on disk, URI from File.toURI() = "file:/" SINGLE slash) — must pass
 * validation. Two past regressions silently pushed every compressed+overlaid photo onto the
 * raw-original fallback (uncompressed, overlay-free files reached the server):
 *  1) the video duration probe was applied to JPEGs;
 *  2) a naive "file://" prefix strip left the scheme in the path, so the file read as missing.
 * If any assertion here fails, processed photos are being thrown away again. Do not weaken.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class ProcessedPhotoValidatorRegressionTest {
    private val validator = FileSystemProofArtifactValidator()

    private fun writeJpeg(): File {
        val file = File.createTempFile("proof-processed", ".jpg")
        val bitmap = Bitmap.createBitmap(320, 240, Bitmap.Config.ARGB_8888)
        file.outputStream().use { bitmap.compress(Bitmap.CompressFormat.JPEG, 88, it) }
        bitmap.recycle()
        return file
    }

    @Test
    fun `processed photo with File-toURI single-slash uri passes mime-aware validation`() {
        val file = writeJpeg()
        val uri = file.toURI().toString()
        assertTrue("processor emits file:/ single-slash URIs", uri.startsWith("file:/") && !uri.startsWith("file://"))
        val result = validator.validateProcessedArtifact(uri, "image/jpeg")
        assertTrue("processed JPEG must validate, got: ${result.reason}", result.isValid)
    }

    @Test
    fun `processed photo with double-slash uri also passes`() {
        val file = writeJpeg()
        val result = validator.validateProcessedArtifact("file://${file.absolutePath}", "image/jpeg")
        assertTrue("double-slash URI must validate, got: ${result.reason}", result.isValid)
    }

    @Test
    fun `image mime never routed through video probe`() {
        // A valid JPEG has no video duration; if the video probe ran it would reject.
        val file = writeJpeg()
        val result = validator.validateProcessedArtifact(file.toURI().toString(), "image/jpeg")
        assertTrue(result.isValid)
    }

    @Test
    fun `missing or empty photo still rejected`() {
        val missing = validator.validateProcessedArtifact("file:/nonexistent/proof.jpg", "image/jpeg")
        assertFalse(missing.isValid)
        val empty = File.createTempFile("proof-empty", ".jpg")
        assertFalse(validator.validateProcessedArtifact(empty.toURI().toString(), "image/jpeg").isValid)
    }

    // NOTE: corrupt-bytes rejection is not testable under Robolectric (its BitmapFactory shadow
    // reports fake bounds for any bytes); real-device decode covers it.
}
