package sg.mesha.goatos.push

/**
 * Canonical FCM data-payload / notification-tap intent-extra keys. Shared by every push
 * read/write site so a typo in an inline key string can never silently break deep-linking:
 *  - [GoatOsMessagingService.onMessageReceived] reads the incoming [com.google.firebase.messaging.RemoteMessage.getData] map.
 *  - [PushNotifications] writes the same map onto the tap [android.app.PendingIntent]'s intent extras.
 *  - [sg.mesha.goatos.MainActivity] (`onCreate`/`onNewIntent`) reads those extras back out.
 *  - [resolvePushRoute] maps the payload to an app route.
 *
 * Payload shape (coordinate with the backend FCM slice — the sibling `goatos-fcm-notif`
 * worktree): `type`, `obligation_id`, `park_id`, `shed_id`, optional `task_id`, and either `target`/`href` (a
 * backend deep-link, reusing the SAME shape Calendar items already carry) or `screen` (an
 * explicit named screen). `title`/`body` are display-only fallbacks used when the message has
 * no `notification` block (a pure data message).
 */
object PushExtras {
    const val TYPE = "type"
    const val OBLIGATION_ID = "obligation_id"
    const val PARK_ID = "park_id"
    const val SHED_ID = "shed_id"
    const val TASK_ID = "task_id"
    const val ITEM_ID = "item_id"
    const val CATEGORY = "category"
    const val TARGET = "target"
    const val HREF = "href"
    const val SCREEN = "screen"
    const val TITLE = "title"
    const val BODY = "body"

    /** The subset carried through to the tap intent / route resolver — [TITLE]/[BODY] are
     *  display-only and never affect routing. */
    val ROUTE_KEYS = listOf(TYPE, OBLIGATION_ID, PARK_ID, SHED_ID, TASK_ID, ITEM_ID, CATEGORY, TARGET, HREF, SCREEN)
}
