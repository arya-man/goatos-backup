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
}
