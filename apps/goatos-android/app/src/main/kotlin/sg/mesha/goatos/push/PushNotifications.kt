package sg.mesha.goatos.push

import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import androidx.core.app.NotificationCompat
import dagger.hilt.android.qualifiers.ApplicationContext
import sg.mesha.goatos.MainActivity
import sg.mesha.goatos.R
import sg.mesha.goatos.core.designsystem.R as DesignSystemR
import javax.inject.Inject
import javax.inject.Singleton
import kotlin.random.Random

/**
 * Creates the FCM notification channels + builds/posts the system notification for an incoming
 * [com.google.firebase.messaging.RemoteMessage] ([GoatOsMessagingService.onMessageReceived]).
 * Mirrors [sg.mesha.goatos.sync.UploadSyncNotifications]'s shape — the OTHER
 * notification-posting surface in this app — for consistency (channel-ensure-on-app-start,
 * `NotificationCompat.Builder`, best-effort `notify`).
 *
 * Two channels (docs: FCM push slice):
 *  - `push_channel_vaccination_id` (`IMPORTANCE_HIGH`) — obligation/escalation/reschedule
 *    alerts, the time-sensitive operational category.
 *  - `push_channel_general_id` (`IMPORTANCE_DEFAULT`, and the manifest's
 *    `default_notification_channel_id`) — everything else.
 *
 * The full data [payload] rides along on the tap [PendingIntent]'s intent extras
 * ([PushExtras.ROUTE_KEYS]) so [MainActivity] can resolve + navigate to the right screen on tap
 * (see [resolvePushRoute] / [PendingNavigation]).
 */
@Singleton
class PushNotifications @Inject constructor(
    @ApplicationContext private val context: Context,
) {
    private val manager = context.getSystemService(NotificationManager::class.java)

    fun ensureChannels() {
        val vaccination = NotificationChannel(
            vaccinationChannelId(),
            context.getString(DesignSystemR.string.push_channel_vaccination_name),
            NotificationManager.IMPORTANCE_HIGH,
        ).apply {
            description = context.getString(DesignSystemR.string.push_channel_vaccination_description)
        }
        val general = NotificationChannel(
            generalChannelId(),
            context.getString(DesignSystemR.string.push_channel_general_name),
            NotificationManager.IMPORTANCE_DEFAULT,
        ).apply {
            description = context.getString(DesignSystemR.string.push_channel_general_description)
        }
        manager?.createNotificationChannels(listOf(vaccination, general))
    }

    /** Builds + posts a system notification for [payload] (an FCM message's data map — see
     *  [PushExtras]), on the channel [channelFor] resolves from `payload[type]`. Called for
     *  BOTH a notification+data message and a pure data message
     *  ([GoatOsMessagingService.onMessageReceived]'s KDoc explains why foreground display needs
     *  this explicit path). */
    fun show(title: String, body: String, payload: Map<String, String>) {
        val channelId = channelFor(payload[PushExtras.TYPE])
        val notification = NotificationCompat.Builder(context, channelId)
            .setSmallIcon(R.drawable.ic_notification_push)
            .setColor(context.getColor(R.color.push_notification_accent))
            .setContentTitle(title)
            .setContentText(body)
            .setAutoCancel(true)
            .setContentIntent(tapPendingIntent(payload))
            .setPriority(priorityFor(channelId))
            .build()
        runCatching { manager?.notify(notificationId(payload), notification) }
    }

    private fun channelFor(type: String?): String =
        if (type?.lowercase() in VACCINATION_TYPES) vaccinationChannelId() else generalChannelId()

    private fun priorityFor(channelId: String): Int =
        if (channelId == vaccinationChannelId()) NotificationCompat.PRIORITY_HIGH else NotificationCompat.PRIORITY_DEFAULT

    /** Opens [MainActivity] (already `launchMode="singleTop"` — see AndroidManifest) with the
     *  routing subset of [payload] as raw intent extras — [MainActivity.onNewIntent]/`onCreate`
     *  reads them and resolves the route itself via [resolvePushRoute], exactly like it would
     *  for a background/killed-app FCM auto-display tap (where the SYSTEM builds the launch
     *  intent, not this class, so a precomputed route extra would only cover the foreground
     *  path — resolving from the raw payload is the one mechanism that works for both). */
    private fun tapPendingIntent(payload: Map<String, String>): PendingIntent {
        val intent = Intent(context, MainActivity::class.java).apply {
            flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP
            PushExtras.ROUTE_KEYS.forEach { key -> payload[key]?.let { putExtra(key, it) } }
        }
        return PendingIntent.getActivity(
            context,
            notificationId(payload),
            intent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
    }

    /** A stable-ish id per notification so multiple pushes don't collide into one slot, while
     *  still bounded (no unbounded growth — [Random] within a fixed range, matching the "small
     *  fixed id space" shape [sg.mesha.goatos.sync.UploadForegroundService] uses for its own
     *  fixed notification ids). Prefers `obligation_id`/`shed_id` hashCode so repeat pushes
     *  about the SAME obligation/shed update the same slot instead of stacking duplicates. */
    private fun notificationId(payload: Map<String, String>): Int {
        val stableKey = payload[PushExtras.ITEM_ID] ?: payload[PushExtras.OBLIGATION_ID] ?: payload[PushExtras.SHED_ID]
        return stableKey?.hashCode() ?: (BASE_NOTIFICATION_ID + Random.nextInt(1000))
    }

    private fun vaccinationChannelId(): String = context.getString(DesignSystemR.string.push_channel_vaccination_id)
    private fun generalChannelId(): String = context.getString(DesignSystemR.string.push_channel_general_id)

    private companion object {
        const val BASE_NOTIFICATION_ID = 5000
        val VACCINATION_TYPES = setOf(
            "reminder", "vaccination_reminder", "reschedule", "record", "verification", "verification_pending",
            "verification_approved", "verification_closed", "verify", "rework",
        )
    }
}
