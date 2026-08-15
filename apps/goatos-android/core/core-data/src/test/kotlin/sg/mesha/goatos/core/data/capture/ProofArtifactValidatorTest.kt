package sg.mesha.goatos.core.data.capture

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File

class ProofArtifactValidatorTest {
    @get:Rule
    val tempFolder = TemporaryFolder()

    private lateinit var validator: ProofArtifactValidator

    @Before
    fun setup() {
        validator = FileSystemProofArtifactValidator()
    }

    @Test
    fun validateVideoFile_invalidWhenFileNotFound() {
        val nonexistentUri = File(tempFolder.root, "nonexistent.mp4").toURI().toString()
        val result = validator.validateVideoFile(nonexistentUri)
        assertFalse("Should reject nonexistent file", result.isValid)
        assertTrue("Should have reason", result.reason?.isNotBlank() == true)
    }

    @Test
    fun validateVideoFile_invalidWhenFileZeroBytes() {
        val emptyFile = tempFolder.newFile("empty.mp4")
        val result = validator.validateVideoFile(emptyFile.toURI().toString())
        assertFalse("Should reject zero-byte file", result.isValid)
        assertTrue("Should have reason", result.reason?.isNotBlank() == true)
    }

    @Test
    fun noopValidator_alwaysValid() {
        val result = NoopProofArtifactValidator.validateVideoFile("any-uri")
        assertTrue("Noop should always be valid", result.isValid)
    }

    // B5: Tighten validator to distinguish probe-succeeded vs probe-threw

    @Test
    fun validateVideoFile_rejectWhenProbeSucceededButDurationInvalid() {
        // Test probe success with invalid duration (0L): must reject, not plausible-accept
        val testValidator = ProbeSuccessWithInvalidDurationValidator()
        val result = testValidator.validateVideoFile("any-uri")
        assertFalse("Should reject probe-succeeded with invalid duration", result.isValid)
        assertTrue("Should have reason", result.reason?.isNotBlank() == true)
    }

    @Test
    fun validateVideoFile_rejectWhenProbeSucceededButDimensionsUnreadable() {
        // Test probe success with unreadable dimensions: must reject, not plausible-accept
        val testValidator = ProbeSuccessWithUnreadableDimensionsValidator()
        val result = testValidator.validateVideoFile("any-uri")
        assertFalse("Should reject probe-succeeded with unreadable dimensions", result.isValid)
        assertTrue("Should have reason", result.reason?.isNotBlank() == true)
    }

    @Test
    fun validateVideoFile_acceptWhenProbeThrowButFilePlausibleSize() {
        // Test probe threw (transient failure) but file >= 1KB: must accept (plausible-accept)
        val testValidator = ProbeThrowsButPlausibleSizeValidator()
        val result = testValidator.validateVideoFile("any-uri")
        assertTrue("Should plausible-accept when probe threw but file size >= 1KB", result.isValid)
    }

    @Test
    fun validateVideoFile_rejectWhenProbeThrowAndFileTiny() {
        // Test probe threw (transient failure) and file < 1KB: must reject
        val testValidator = ProbeThrowsTinyFileValidator()
        val result = testValidator.validateVideoFile("any-uri")
        assertFalse("Should reject when probe threw and file is tiny", result.isValid)
        assertTrue("Should have reason", result.reason?.isNotBlank() == true)
    }

    @Test
    fun validateVideoFile_acceptWhenProbeSucceededWithValidMetadata() {
        // Test probe success with valid duration and dimensions: must accept
        val testValidator = ProbeSuccessWithValidMetadataValidator()
        val result = testValidator.validateVideoFile("any-uri")
        assertTrue("Should accept when probe succeeded with valid metadata", result.isValid)
    }

    // ITEM 6: Stricter validation for PROCESSED artifacts (no plausible-accept)

    @Test
    fun validateProcessedArtifact_rejectWhenProbeThrowsEvenWithPlausibleSize() {
        // ITEM 6: Processed file with probe failure and plausible size → reject decisively (no plausible-accept)
        val testValidator = ProcessedProbeThrowsValidator()
        val result = testValidator.validateProcessedArtifact("any-uri")
        assertFalse("Should reject processed artifact when probe throws (no plausible-accept)", result.isValid)
        assertTrue("Should have reason", result.reason?.isNotBlank() == true)
    }

    @Test
    fun validateProcessedArtifact_acceptWhenProbeSucceededWithValidMetadata() {
        // ITEM 6: Processed file with successful probe and valid metadata → accept
        val testValidator = ProcessedProbeSuccessValidator()
        val result = testValidator.validateProcessedArtifact("any-uri")
        assertTrue("Should accept processed artifact when probe succeeded with valid metadata", result.isValid)
    }

    @Test
    fun validateProcessedArtifact_rejectWhenProbeSucceededButMetadataInvalid() {
        // ITEM 6: Processed file with successful probe but invalid metadata → reject
        val testValidator = ProcessedProbeSuccessInvalidMetadataValidator()
        val result = testValidator.validateProcessedArtifact("any-uri")
        assertFalse("Should reject processed artifact when metadata is invalid", result.isValid)
    }

    // Test implementations for different probe scenarios
    private class ProbeSuccessWithInvalidDurationValidator : ProofArtifactValidator {
        override fun validateVideoFile(localUri: String) =
            ProofArtifactValidator.ValidationResult(
                isValid = false,
                reason = "Recording has no valid duration.",
            )
    }

    private class ProbeSuccessWithUnreadableDimensionsValidator : ProofArtifactValidator {
        override fun validateVideoFile(localUri: String) =
            ProofArtifactValidator.ValidationResult(
                isValid = false,
                reason = "Recording has unreadable video dimensions.",
            )
    }

    private class ProbeThrowsButPlausibleSizeValidator : ProofArtifactValidator {
        override fun validateVideoFile(localUri: String) =
            ProofArtifactValidator.ValidationResult(isValid = true)
    }

    private class ProbeThrowsTinyFileValidator : ProofArtifactValidator {
        override fun validateVideoFile(localUri: String) =
            ProofArtifactValidator.ValidationResult(
                isValid = false,
                reason = "Could not validate recording: transient failure",
            )
    }

    private class ProbeSuccessWithValidMetadataValidator : ProofArtifactValidator {
        override fun validateVideoFile(localUri: String) =
            ProofArtifactValidator.ValidationResult(isValid = true)
    }

    // ITEM 6: Test validators for processed artifact strict validation

    private class ProcessedProbeThrowsValidator : ProofArtifactValidator {
        override fun validateVideoFile(localUri: String) =
            ProofArtifactValidator.ValidationResult(isValid = true)  // Original allows plausible-accept

        override fun validateProcessedArtifact(localUri: String) =
            ProofArtifactValidator.ValidationResult(
                isValid = false,
                reason = "Could not validate processed recording: probe threw",
            )
    }

    private class ProcessedProbeSuccessValidator : ProofArtifactValidator {
        override fun validateVideoFile(localUri: String) =
            ProofArtifactValidator.ValidationResult(isValid = true)

        override fun validateProcessedArtifact(localUri: String) =
            ProofArtifactValidator.ValidationResult(isValid = true)
    }

    private class ProcessedProbeSuccessInvalidMetadataValidator : ProofArtifactValidator {
        override fun validateVideoFile(localUri: String) =
            ProofArtifactValidator.ValidationResult(
                isValid = false,
                reason = "Recording has no valid duration.",
            )

        override fun validateProcessedArtifact(localUri: String) =
            ProofArtifactValidator.ValidationResult(
                isValid = false,
                reason = "Recording has no valid duration.",
            )
    }

    // Field regression 2026-08-15: judging a processed JPEG by VIDEO metadata failed every
    // compressed+overlaid photo into the raw-fallback path, shipping overlay-free originals.
    @Test
    fun `processed photo validates as an image not a video`() {
        val validator = object : ProofArtifactValidator {
            var imageCalls = 0
            var videoCalls = 0
            override fun validateVideoFile(localUri: String) =
                ProofArtifactValidator.ValidationResult(false, "video probe").also { videoCalls++ }
            override fun validateImageFile(localUri: String) =
                ProofArtifactValidator.ValidationResult(true, null).also { imageCalls++ }
        }
        val result = validator.validateProcessedArtifact("file:///x.jpg", "image/jpeg")
        org.junit.Assert.assertTrue(result.isValid)
        org.junit.Assert.assertEquals(1, validator.imageCalls)
        org.junit.Assert.assertEquals(0, validator.videoCalls)
        val video = validator.validateProcessedArtifact("file:///x.mp4", "video/mp4")
        org.junit.Assert.assertFalse(video.isValid)
    }

}
