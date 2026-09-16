package sg.mesha.goatos.ui

import java.nio.file.Path
import kotlin.io.path.readText
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * SOP CAPTURE CARD (2026-09-16): Add birth and Add death render the published card's slots — photo,
 * video or either. The capture sources are delegates bound per route; an unbound source answers null
 * at once, so every tap read "Couldn't save that proof" (found on the Realme, 2026-09-17: the routes
 * bound NEITHER delegate). Bound without the permission gate, as on the shifting raise route, so a
 * form whose card asks for no capture never meets a camera prompt.
 */
class HerdCaptureRouteBindingTest {
    private val navHost = Path.of("src/main/kotlin/sg/mesha/goatos/ui/AppNavHost.kt").readText()

    private fun routeBlock(routeMarker: String, screenCall: String): String =
        navHost.substringAfter(routeMarker).substringBefore(screenCall)

    @Test
    fun `add birth route binds both the video and the photo capture delegates`() {
        val block = routeBlock("composable(Routes.COUNTS_BIRTH_ADD)", "AddBirthScreen(")
        assertTrue("video delegate unbound on Add birth", block.contains("BindVideoCaptureSource(rememberDelegatingProofCaptureSource())"))
        assertTrue("photo delegate unbound on Add birth", block.contains("BindPhotoCaptureSource(rememberDelegatingPhotoCaptureSource())"))
        assertTrue("Add birth must not gate a card-less form on camera permission", !block.contains("CaptureAccessGate"))
    }

    @Test
    fun `add death route binds both the video and the photo capture delegates`() {
        val block = routeBlock("route = Routes.COUNTS_DEATH_ADD,", "AddDeathScreen(")
        assertTrue("video delegate unbound on Add death", block.contains("BindVideoCaptureSource(rememberDelegatingProofCaptureSource())"))
        assertTrue("photo delegate unbound on Add death", block.contains("BindPhotoCaptureSource(rememberDelegatingPhotoCaptureSource())"))
        assertTrue("Add death must not gate a card-less form on camera permission", !block.contains("CaptureAccessGate"))
    }
}
