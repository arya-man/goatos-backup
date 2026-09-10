package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class VerifyQueueThumbnailTest {
    @Test
    fun `queue row carries backend thumbnail without using full media url`() {
        val source = java.io.File("src/main/kotlin/sg/mesha/goatos/viewmodel/VerifyQueueViewModel.kt").readText()
        val screen = java.io.File("../feature/feature-verify/src/main/kotlin/sg/mesha/goatos/feature/verify/VerifyQueueScreen.kt").readText()

        assertTrue(source.contains("thumbnailUrl = media.firstNotNullOfOrNull"))
        assertTrue(screen.contains("AsyncImage("))
        assertTrue(screen.contains("model = row.thumbnailUrl"))
        assertFalse(screen.contains("model = row.downloadUrl"))
    }
}
