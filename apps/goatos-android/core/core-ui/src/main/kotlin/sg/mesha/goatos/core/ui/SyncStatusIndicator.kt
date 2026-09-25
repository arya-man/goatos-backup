package sg.mesha.goatos.core.ui

import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import kotlinx.coroutines.delay
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaDimens
import sg.mesha.goatos.core.designsystem.theme.MeshaType

/**
 * The one shared sync/stale affordance for every offline-first read screen
 * (docs/decisions/android-offline-first.md). Room is the UI's single source of truth: a
 * screen renders cached data immediately and refreshes in the background
 * (stale-while-revalidate). This composable is the small "syncing…" / "updated Xm ago" /
 * "offline" line that reports the state of that background refresh — it is NEVER a
 * blank/loading wall, and it renders nothing at all while there is no cached data yet
 * ([hasData] false): a cold-start/empty/error placeholder is the screen's own job, this
 * indicator only annotates a screen that already has something on it.
 *
 * Reused by every offline-first screen (Calendar first; Control Tower, Sheds/Execution,
 * Adherence, and Insights next per docs/decisions/android-offline-first.md) — keep this the
 * ONE indicator so all five read screens look and behave identically. Callers wire it from
 * their repository's `Resource<T>` (`sg.mesha.goatos.core.common.Resource`) plus the
 * ViewModel's own in-flight [isRefreshing] flag: `hasData = resource.hasData`,
 * `lastSyncedAt = resource.lastSyncedAt`, `isOffline = resource.error != null`.
 */
@Composable
fun SyncStatusIndicator(
    isRefreshing: Boolean,
    lastSyncedAt: Long?,
    hasData: Boolean,
    isOffline: Boolean = false,
    modifier: Modifier = Modifier,
    /**
     * The screen's own words for "the last refresh did not reach the server, so what you see is
     * the saved copy" ("Couldn't refresh · showing saved tasks"). Null when the last refresh
     * succeeded. Outranked only by [isRefreshing]: a screen must never read "Up to date" over a
     * refresh that just failed.
     */
    refreshFailedLabel: String? = null,
    nowMillis: () -> Long = { System.currentTimeMillis() },
) {
    // Nothing cached yet: the screen's own loading/empty/error state owns this moment, so a
    // second "syncing"/"offline" line here would be redundant chrome, not information.
    if (!hasData) return

    // The relative label ("just now", "3m ago") is a statement about the CLOCK, not about the
    // inputs: without a tick it froze at whatever it read on the last recomposition, so a screen
    // left open said "just now" minutes after its last sync. Re-read the clock at the next minute
    // boundary of the label for as long as it is on screen.
    var now by remember { mutableLongStateOf(nowMillis()) }
    LaunchedEffect(lastSyncedAt) {
        while (true) {
            now = nowMillis()
            val synced = lastSyncedAt ?: break
            delay(nextSyncLabelChangeInMillis(synced, now))
        }
    }

    val tint: Color
    val label: String
    when {
        isRefreshing -> {
            tint = MeshaColors.Muted
            label = "Syncing…"
        }
        refreshFailedLabel != null -> {
            tint = MeshaColors.Warn
            label = refreshFailedLabel
        }
        isOffline -> {
            tint = MeshaColors.Warn
            label = lastSyncedAt?.let { "Offline · updated ${relativeSyncLabel(it, now)}" } ?: "Offline"
        }
        else -> {
            tint = MeshaColors.Faint
            label = lastSyncedAt?.let { "Updated ${relativeSyncLabel(it, now)}" } ?: "Up to date"
        }
    }

    Row(verticalAlignment = Alignment.CenterVertically, modifier = modifier) {
        if (isRefreshing) {
            CircularProgressIndicator(
                modifier = Modifier.size(MeshaDimens.iconSm - 3.dp),
                color = tint,
                strokeWidth = 1.5.dp,
            )
        } else {
            Icon(
                imageVector = if (isOffline || refreshFailedLabel != null) MeshaIcons.Warn else MeshaIcons.Clock,
                contentDescription = null,
                tint = tint,
                modifier = Modifier.size(MeshaDimens.iconSm - 3.dp),
            )
        }
        Spacer(Modifier.width(MeshaDimens.space1))
        Text(text = label, style = MeshaType.caption, color = tint)
    }
}

/** "just now" / "Xm ago" / "Xh ago" / "Xd ago" — small, dependency-free relative clock so
 *  every offline-first screen renders the same wording for "how stale is this cache". */
internal fun relativeSyncLabel(epochMillis: Long, nowMillis: Long): String {
    val deltaSeconds = ((nowMillis - epochMillis) / 1000).coerceAtLeast(0)
    return when {
        deltaSeconds < 60 -> "just now"
        deltaSeconds < 3_600 -> "${deltaSeconds / 60}m ago"
        deltaSeconds < 86_400 -> "${deltaSeconds / 3_600}h ago"
        else -> "${deltaSeconds / 86_400}d ago"
    }
}

/**
 * Milliseconds until [relativeSyncLabel] would read differently: the next whole minute while under
 * an hour, the next whole hour under a day, else the next day. Never less than a second, so a
 * clock that jumps backwards cannot spin the ticker.
 */
internal fun nextSyncLabelChangeInMillis(epochMillis: Long, nowMillis: Long): Long {
    val delta = (nowMillis - epochMillis).coerceAtLeast(0)
    val unit = when {
        delta < 3_600_000L -> 60_000L
        delta < 86_400_000L -> 3_600_000L
        else -> 86_400_000L
    }
    return (unit - delta % unit).coerceAtLeast(1_000L)
}
