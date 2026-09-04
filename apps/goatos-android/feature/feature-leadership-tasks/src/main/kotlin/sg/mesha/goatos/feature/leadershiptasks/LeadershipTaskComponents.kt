package sg.mesha.goatos.feature.leadershiptasks

// telemetry:exempt pure stateless renderers; the @HiltViewModels in :app own the
// leadership_task_* AnalyticsEventsLeadershipTasks + CrashReporter wiring.

import android.media.MediaPlayer
import android.util.Log
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType

private const val LOG_TAG = "LeadershipTasksUi"

/** Backend-composed status chip, rendered VERBATIM. Blank copy renders nothing at all. */
@Composable
internal fun LeadershipStatusChip(label: String, status: String = "", modifier: Modifier = Modifier) {
    if (label.isBlank()) return
    // One colour per status (maintainer instruction 2026-09-04): amber while it waits, blue while
    // it is being worked, green when finished, muted when withdrawn. The LABEL stays backend copy.
    val accent = leadershipStatusAccent(status)
    Box(
        modifier = modifier
            .clip(RoundedCornerShape(8.dp))
            .background(accent.copy(alpha = 0.16f))
            .padding(horizontal = 8.dp, vertical = 3.dp),
    ) {
        Text(text = label, color = accent, style = MeshaType.pillStrong)
    }
}

/** Card surface shared by the list rows, the detail sections and the compose form. */
@Composable
internal fun leadershipCardModifier(enabled: Boolean = true, onClick: (() -> Unit)? = null): Modifier {
    var base = Modifier
        .fillMaxWidth()
        .padding(horizontal = 16.dp)
        .clip(RoundedCornerShape(16.dp))
        .background(MeshaColors.Surf)
        .border(1.dp, MeshaColors.Hair, RoundedCornerShape(16.dp))
    if (onClick != null) {
        base = base.clickable(enabled = enabled, onClick = onClick)
    }
    return base.padding(16.dp)
}

/** Primary (filled) action. Disabled renders dead, never hidden. */
@Composable
internal fun LeadershipPrimaryButton(
    label: String,
    enabled: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Box(
        modifier = modifier
            .heightIn(min = 48.dp)
            .clip(RoundedCornerShape(14.dp))
            .background(if (enabled) MeshaColors.Brand else MeshaColors.Surf3)
            .clickable(enabled = enabled, role = Role.Button, onClick = onClick)
            .padding(horizontal = 16.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = label,
            color = if (enabled) MeshaColors.OnBrand else MeshaColors.Faint,
            style = MeshaType.button,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

/** Secondary (ghost) action. */
@Composable
internal fun LeadershipGhostButton(
    label: String,
    enabled: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    icon: ImageVector? = null,
    danger: Boolean = false,
) {
    val tint = when {
        !enabled -> MeshaColors.Faint
        danger -> MeshaColors.Danger
        else -> MeshaColors.Ink
    }
    Row(
        modifier = modifier
            .heightIn(min = 48.dp)
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Surf2)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(14.dp))
            .clickable(enabled = enabled, role = Role.Button, onClick = onClick)
            .padding(horizontal = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.CenterHorizontally),
    ) {
        if (icon != null) {
            Icon(imageVector = icon, contentDescription = null, tint = tint, modifier = Modifier.size(18.dp))
        }
        Text(text = label, color = tint, style = MeshaType.button, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

/** The glyph an attachment kind wears, everywhere it is listed. */
internal fun LeadershipAttachmentKind.icon(): ImageVector = when (this) {
    LeadershipAttachmentKind.AUDIO -> MeshaIcons.Mic
    LeadershipAttachmentKind.VIDEO -> MeshaIcons.Video
    LeadershipAttachmentKind.PHOTO -> MeshaIcons.Photo
    LeadershipAttachmentKind.FILE -> MeshaIcons.Document
}

/** A small square glyph tile beside an attachment row. */
@Composable
internal fun LeadershipAttachmentGlyph(kind: LeadershipAttachmentKind, modifier: Modifier = Modifier) {
    Box(
        modifier = modifier
            .size(40.dp)
            .clip(RoundedCornerShape(12.dp))
            .background(MeshaColors.Surf2),
        contentAlignment = Alignment.Center,
    ) {
        Icon(imageVector = kind.icon(), contentDescription = null, tint = MeshaColors.Muted, modifier = Modifier.size(20.dp))
    }
}

/**
 * One in-place voice-note player: play/pause and elapsed over total. The player lives only while
 * this row is composed and is released on dispose, so leaving the screen stops the audio.
 */
@Composable
internal fun LeadershipAudioPlayerRow(
    localPath: String,
    modifier: Modifier = Modifier,
) {
    var player by remember(localPath) { mutableStateOf<MediaPlayer?>(null) }
    var playing by remember(localPath) { mutableStateOf(false) }
    var positionMs by remember(localPath) { mutableIntStateOf(0) }
    var durationMs by remember(localPath) { mutableIntStateOf(0) }
    DisposableEffect(localPath) {
        // exception:exempt a clip that will not open renders a dead player rather than a crash;
        // the file is one the app itself just wrote or fetched, so the failure is not actionable here
        val created = runCatching {
            MediaPlayer().apply {
                setDataSource(localPath)
                prepare()
                setOnCompletionListener {
                    playing = false
                    positionMs = 0
                }
            }
        }.getOrNull()
        player = created
        durationMs = created?.duration?.coerceAtLeast(0) ?: 0
        onDispose {
            // exception:exempt best-effort MediaPlayer cleanup while leaving the screen; nothing
            // user-actionable remains once the composable is gone, and the row recreates cleanly.
            runCatching { created?.release() }
                .onFailure { Log.w(LOG_TAG, "leadership_task_audio_release_failed", it) }
            player = null
        }
    }
    LaunchedEffect(playing, localPath) {
        while (playing) {
            // exception:exempt transient MediaPlayer progress read; the UI falls back to 0 and
            // keeps the already-visible play control rather than interrupting attachment review.
            positionMs = runCatching { player?.currentPosition ?: 0 }
                .onFailure {
                    Log.w(LOG_TAG, "leadership_task_audio_position_failed", it)
                    playing = false
                }
                .getOrDefault(0)
            delay(250L)
        }
    }
    val ready = player != null
    Row(
        modifier = modifier.fillMaxWidth(),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Box(
            modifier = Modifier
                .size(48.dp)
                .clip(RoundedCornerShape(24.dp))
                .background(if (ready) MeshaColors.Brand else MeshaColors.Surf3)
                .clickable(enabled = ready, role = Role.Button) {
                    val current = player ?: return@clickable
                    if (playing) {
                        current.pause()
                        playing = false
                    } else {
                        current.start()
                        playing = true
                    }
                },
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                imageVector = if (playing) MeshaIcons.Pause else MeshaIcons.Play,
                contentDescription = stringResource(
                    if (playing) R.string.leadership_tasks_audio_pause else R.string.leadership_tasks_audio_play,
                ),
                tint = if (ready) MeshaColors.OnBrand else MeshaColors.Faint,
                modifier = Modifier.size(20.dp),
            )
        }
        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .height(4.dp)
                    .clip(RoundedCornerShape(20.dp))
                    .background(MeshaColors.Surf3),
            ) {
                val fraction = if (durationMs > 0) (positionMs.toFloat() / durationMs).coerceIn(0f, 1f) else 0f
                if (fraction > 0f) {
                    Box(modifier = Modifier.weight(fraction).height(4.dp).background(MeshaColors.Brand))
                }
                if (fraction < 1f) {
                    Box(modifier = Modifier.weight(1f - fraction).height(4.dp))
                }
            }
            Text(
                text = "${formatClock(positionMs.toLong())} / ${formatClock(durationMs.toLong())}",
                color = MeshaColors.Muted,
                style = MeshaType.caption,
            )
        }
    }
}

/** A centered small spinner for an attachment still being fetched. */
@Composable
internal fun LeadershipInlineSpinner(modifier: Modifier = Modifier) {
    CircularProgressIndicator(color = MeshaColors.BrandD, strokeWidth = 2.dp, modifier = modifier.size(18.dp))
}

/** Millis -> `m:ss` (or `h:mm:ss` past an hour). A clock, not a sentence. */
fun formatClock(ms: Long): String {
    val totalSeconds = (ms.coerceAtLeast(0L) + 500L) / 1000L
    val hours = totalSeconds / 3600L
    val minutes = (totalSeconds % 3600L) / 60L
    val seconds = totalSeconds % 60L
    return if (hours > 0L) {
        "%d:%02d:%02d".format(hours, minutes, seconds)
    } else {
        "%d:%02d".format(minutes, seconds)
    }
}

/** Bytes -> "820 KB" / "1.2 MB". A number, not a sentence; blank for an unknown size. */
fun formatBytes(bytes: Long): String {
    if (bytes <= 0L) return ""
    val kb = bytes / 1024.0
    if (kb < 1024.0) return "${kb.coerceAtLeast(1.0).toInt()} KB"
    val mb = kb / 1024.0
    if (mb < 1024.0) return "%.1f MB".format(mb)
    return "%.1f GB".format(mb / 1024.0)
}

/** The "+" that raises a task: a 48dp brand-green disc, the one filled control in the header. */
@Composable
internal fun LeadershipRaiseButton(contentDescription: String, onClick: () -> Unit, modifier: Modifier = Modifier) {
    Box(
        modifier = modifier
            .size(48.dp)
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Brand)
            .clickable(role = Role.Button, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Icon(
            imageVector = MeshaIcons.Plus,
            contentDescription = contentDescription,
            tint = MeshaColors.OnBrand,
            modifier = Modifier.size(22.dp),
        )
    }
}

/**
 * The assignee's status control: the current status as a coloured pill with a chevron, and the
 * backend's options in a menu (maintainer instruction 2026-09-04: "a dropdown with doing and done").
 */
@Composable
internal fun LeadershipStatusDropdown(
    currentLabel: String,
    currentStatus: String,
    options: List<LeadershipStatusOptionUi>,
    enabled: Boolean,
    onSelect: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    var open by remember { mutableStateOf(false) }
    val accent = leadershipStatusAccent(currentStatus)
    Box(modifier = modifier.fillMaxWidth(0.62f)) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(12.dp))
                .background(accent.copy(alpha = 0.16f))
                .clickable(enabled = enabled && options.isNotEmpty(), role = Role.DropdownList) { open = true }
                .padding(start = 14.dp, end = 10.dp, top = 11.dp, bottom = 11.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Text(text = currentLabel, color = accent, style = MeshaType.bodyStrong, modifier = Modifier.weight(1f))
            Icon(
                imageVector = MeshaIcons.ChevronDown,
                contentDescription = null,
                tint = accent,
                modifier = Modifier.size(16.dp),
            )
        }
        DropdownMenu(
            expanded = open,
            onDismissRequest = { open = false },
            // A lifted, bordered sheet: on the dark card the popup was invisible without it.
            containerColor = MeshaColors.Surf3,
            shape = RoundedCornerShape(12.dp),
            shadowElevation = 12.dp,
            border = BorderStroke(1.dp, MeshaColors.Hair),
            modifier = Modifier.fillMaxWidth(0.62f),
        ) {
            options.forEach { option -> // compose-guard:ignore: at most three backend status options
                DropdownMenuItem(
                    text = { Text(text = option.label, color = leadershipStatusAccent(option.key), style = MeshaType.bodyStrong) },
                    onClick = {
                        open = false
                        onSelect(option.key)
                    },
                )
            }
        }
    }
}

internal fun leadershipStatusAccent(status: String) = when (status) {
    "open" -> MeshaColors.Warn
    "in_progress" -> MeshaColors.Info
    "done" -> MeshaColors.Ok
    else -> MeshaColors.Muted
}
