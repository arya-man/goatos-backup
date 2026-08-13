package sg.mesha.goatos.core.permissions

import android.app.Activity
import android.content.Context
import android.content.pm.PackageManager
import androidx.core.app.ActivityCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.content.ContextCompat

/**
 * Thin `ContextCompat`/`ActivityCompat` wrappers kept here (not in feature code) so any
 * feature can query current grant state / rationale-eligibility without re-importing
 * `androidx.core.*` at every call site.
 */
fun isPermissionGranted(context: Context, permission: AppPermission): Boolean =
    ContextCompat.checkSelfPermission(context, permission.manifestPermission) ==
        PackageManager.PERMISSION_GRANTED

fun shouldShowRationale(activity: Activity, permission: AppPermission): Boolean =
    ActivityCompat.shouldShowRequestPermissionRationale(activity, permission.manifestPermission)

/**
 * Whether this phone will actually SHOW what we send it.
 *
 * This is deliberately NOT the same question as `isPermissionGranted(context,
 * AppPermission.NOTIFICATIONS)`. The runtime permission is only one of the ways alerts get
 * switched off: notifications can also be turned off for the whole app in system settings (any OS
 * version, including below Android 13 where there is no runtime permission at all), and a channel
 * can be blocked. `NotificationManagerCompat.areNotificationsEnabled()` is the OS's own single
 * answer that covers all of them.
 *
 * It matters beyond the UI: FCM happily accepts a message for a phone whose notifications are off,
 * reports it delivered, and the OS drops it silently. This value is reported to the backend on
 * device register and on every heartbeat so a push-muted device is never addressed and a dropped
 * push is never counted as a delivery.
 */
fun areNotificationsEnabled(context: Context): Boolean =
    NotificationManagerCompat.from(context).areNotificationsEnabled()
