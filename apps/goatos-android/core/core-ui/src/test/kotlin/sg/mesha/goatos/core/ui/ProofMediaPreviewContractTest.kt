package sg.mesha.goatos.core.ui

import java.io.File
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ProofMediaPreviewContractTest {
    @Test
    fun videoPreviewPlaybackControlDoesNotCoverEvidence() {
        val source = File("src/main/kotlin/sg/mesha/goatos/core/ui/ProofMediaPreview.kt").readText()

        assertFalse(
            "Video proof preview must not use a large centered text control over the evidence.",
            source.contains("text = if (isPlaying) \"Pause\" else \"Play\""),
        )
        assertTrue(
            "Video proof preview playback control should stay in a corner so the evidence remains inspectable.",
            source.contains(".align(Alignment.BottomEnd)") || source.contains(".align(Alignment.TopEnd)"),
        )
    }

    @Test
    fun videoPreviewUsesTheSharedProofPlayerFactory() {
        val source = File("src/main/kotlin/sg/mesha/goatos/core/ui/ProofMediaPreview.kt").readText()

        assertTrue(source.contains("LocalProofPlayerFactory.current"))
        assertTrue(source.contains("playerFactory.create(context)"))
        assertFalse(
            "Stock proof preview must use the telemetry-safe proof player factory, not raw media3.",
            source.contains("ExoPlayer.Builder(context).build()"),
        )
    }
}
