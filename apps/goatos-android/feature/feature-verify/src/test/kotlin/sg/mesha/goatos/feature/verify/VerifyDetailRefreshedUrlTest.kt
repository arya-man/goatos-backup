package sg.mesha.goatos.feature.verify

import androidx.media3.common.Player
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The verifier's report, 2026-08-07: "first time we can see, second time it is blank", and
 * "when I open fullscreen I can see video, on normal view it is pending".
 *
 * The media was never the problem -- the signed URL served HTTP 206, video/mp4, 9,995,790 bytes of
 * valid MP4. What broke is that the queue re-signs every GCS URL on each fetch (15-minute TTL, new
 * X-Goog-Date and X-Goog-Signature) while this screen refreshes on open and in the background, so
 * the SAME clip keeps arriving as a different string. Player state was keyed on that string, so it
 * was torn down and rebuilt behind a PlayerView that was only ever bound in the AndroidView
 * factory -- which runs once. The view kept holding a released player and rendered nothing.
 *
 * Two fixes, and this covers the second: key the player on the PROOF so the swap stops happening,
 * and rebind the view in `update` so it survives one if it ever does. [shouldAdoptRefreshedUrl] is
 * how the fresh signature still reaches a clip that has not started yet, without interrupting one
 * that has.
 */
class VerifyDetailRefreshedUrlTest {

    private val old = "https://storage.googleapis.com/bucket/proof-1?X-Goog-Date=20260807T080000Z&X-Goog-Signature=aaa"
    private val fresh = "https://storage.googleapis.com/bucket/proof-1?X-Goog-Date=20260807T084500Z&X-Goog-Signature=bbb"

    @Test
    fun `untouched row adopts a re-signed url so its first prepare uses a full window`() {
        assertTrue(
            shouldAdoptRefreshedUrl(
                playbackState = Player.STATE_IDLE,
                armed = false,
                currentUri = old,
                refreshedUri = fresh,
            ),
        )
    }

    @Test
    fun `a clip being watched is never interrupted by a background re-sign`() {
        // The core regression: re-watching a 3-second proof is the verifier's job. A refresh
        // landing mid-watch must not restart it from zero.
        for (state in listOf(Player.STATE_BUFFERING, Player.STATE_READY, Player.STATE_ENDED)) {
            assertFalse(
                "playbackState=$state must not adopt a refreshed url mid-watch",
                shouldAdoptRefreshedUrl(
                    playbackState = state,
                    armed = true,
                    currentUri = old,
                    refreshedUri = fresh,
                ),
            )
        }
    }

    @Test
    fun `an armed clip sitting at IDLE is left alone`() {
        // `armed` means the verifier already tapped play; the row owns a decoder and a playhead
        // even when a stop() has dropped it back to IDLE (that is exactly what the fullscreen
        // button does). Swapping the media item here would silently reset her position.
        assertFalse(
            shouldAdoptRefreshedUrl(
                playbackState = Player.STATE_IDLE,
                armed = true,
                currentUri = old,
                refreshedUri = fresh,
            ),
        )
    }

    @Test
    fun `an unchanged url is not re-adopted`() {
        // Room re-emits on every cache write, so this runs far more often than the URL actually
        // changes. Re-setting an identical media item would drop the playhead for nothing.
        assertFalse(
            shouldAdoptRefreshedUrl(
                playbackState = Player.STATE_IDLE,
                armed = false,
                currentUri = old,
                refreshedUri = old,
            ),
        )
    }

    @Test
    fun `a blank url is never adopted`() {
        // An empty media item renders nothing -- the precise failure this area exists to prevent.
        assertFalse(
            shouldAdoptRefreshedUrl(
                playbackState = Player.STATE_IDLE,
                armed = false,
                currentUri = old,
                refreshedUri = "",
            ),
        )
    }

    @Test
    fun `a player with no media item yet adopts the url`() {
        assertTrue(
            shouldAdoptRefreshedUrl(
                playbackState = Player.STATE_IDLE,
                armed = false,
                currentUri = null,
                refreshedUri = fresh,
            ),
        )
    }
}
