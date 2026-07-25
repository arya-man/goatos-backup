package sg.mesha.goatos.core.ui

import androidx.compose.runtime.Composable
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect

/**
 * Offline-first "refresh-on-open" (stale-while-revalidate) trigger.
 *
 * Every read screen shows its Room-backed cache instantly and should auto-refresh in the
 * background whenever the user lands on or returns to the screen — never requiring a manual
 * "sync" tap. Call this once near the top of a read screen's composable body, passing the
 * screen's existing background refresh entrypoint (e.g. `onEvent(XEvent.Refresh)`).
 *
 * The refresh is expected to be non-blocking: it upserts Room on success and leaves the
 * cached data visible on failure. Do not use this on screens with in-progress user input
 * (scan-capture, forms) where a resume-triggered refresh could disrupt the user.
 *
 * See docs/decisions/android-offline-first.md.
 */
@Composable
fun RefreshOnResume(onRefresh: () -> Unit) {
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) {
        onRefresh()
    }
}
