package sg.mesha.goatos.feature.auth

import android.app.Activity
import android.content.Intent
import android.os.Build
import android.provider.Settings
import androidx.activity.compose.LocalActivityResultRegistryOwner
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.ActivityResultLauncher
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.height
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import sg.mesha.goatos.core.designsystem.component.MeshaCard
import sg.mesha.goatos.core.designsystem.component.MeshaPrimaryButton
import sg.mesha.goatos.core.designsystem.component.MeshaSectionLabel
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaDimens
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.permissions.AppPermission
import sg.mesha.goatos.core.permissions.PermissionGrantResolver
import sg.mesha.goatos.core.permissions.PermissionGrantState
import sg.mesha.goatos.core.permissions.areNotificationsEnabled
import sg.mesha.goatos.core.permissions.isPermissionGranted
import sg.mesha.goatos.core.permissions.shouldShowRationale

/**
 * The alerts gate every signed-in person passes, whatever their work is.
 *
 * WHY THIS IS NOT ON A CAPTURE SCREEN. [PermissionGateCard] and the capture-screen gates only run
 * where someone scans or records — so a director, a park head or the CEO, who never opens a scan
 * screen, was never asked for notification access at all. On Android 13+ that permission starts
 * DENIED, so their phone held a perfectly valid push token, the push gateway accepted every alert
 * and reported it delivered, and the OS threw it away. The people whose alerts matter most were
 * exactly the people who never got one. This composable therefore lives in the app shell, above
 * whatever screen the person's role lands on (see GoatOsShell), which is the only surface every
 * role has in common after sign-in.
 *
 * It is a banner, never a wall: nobody is blocked from working because alerts are off, and there
 * is never a dead end — if the OS will no longer show the permission prompt, the card switches to
 * a "open phone settings" action rather than a button that does nothing.
 *
 * [onAlertsTurnedOn] fires when access flips from off to on (either from the prompt or from the
 * person returning from system settings), so the device's push state can be re-reported to the
 * backend immediately instead of waiting for the next app launch.
 *
 * Renders nothing while alerts are on — a person who is already reachable never sees this.
 */
@Composable
fun NotificationAlertsGate(
    modifier: Modifier = Modifier,
    onAlertsTurnedOn: () -> Unit = {},
) {
    val context = LocalContext.current
    val activity = context as? Activity

    // The OS's own answer, which covers the runtime permission AND an app-wide switch-off in
    // system settings (possible on every OS version, including below Android 13 where there is no
    // runtime permission to request).
    var alertsOn by remember { mutableStateOf(areNotificationsEnabled(context)) }
    var hasRequestedOnce by rememberSaveable { mutableStateOf(false) }

    // Coming back from system settings is the ONLY signal we get that someone switched alerts on
    // there — no callback, no broadcast. Re-read on every resume so the card disappears by itself
    // and the backend hears about it.
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) {
        val now = areNotificationsEnabled(context)
        if (now != alertsOn) {
            alertsOn = now
            if (now) onAlertsTurnedOn()
        }
    }

    if (alertsOn) return

    // Same non-Activity composition-host caveat as PermissionGateCard: a screenshot test or a
    // static preview has no ActivityResultRegistryOwner. Falling back to the settings path keeps
    // those hosts rendering instead of crashing, and in the real app the prompt is always there.
    val registryOwner = LocalActivityResultRegistryOwner.current
    val launcher: ActivityResultLauncher<String>? = if (registryOwner != null) {
        rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) {
            hasRequestedOnce = true
            val now = areNotificationsEnabled(context)
            alertsOn = now
            if (now) onAlertsTurnedOn()
        }
    } else {
        null
    }

    val permission = AppPermission.NOTIFICATIONS
    val requestable = Build.VERSION.SDK_INT >= permission.minSdkInt && launcher != null
    val grantState = PermissionGrantResolver.resolve(
        isGranted = isPermissionGranted(context, permission),
        hasRequestedOnce = hasRequestedOnce,
        shouldShowRationale = activity?.let { shouldShowRationale(it, permission) } ?: true,
    )

    // Ask the OS only while the OS will still show the prompt. Once it stops asking — or on a
    // phone where alerts were switched off in settings rather than refused at a prompt — the only
    // honest action left is to walk the person to settings.
    val canPrompt = requestable && grantState == PermissionGrantState.DENIED

    MeshaCard(modifier = modifier) {
        MeshaSectionLabel(text = stringResource(R.string.alerts_off_title))
        Spacer(Modifier.height(MeshaDimens.space2))
        Text(
            text = stringResource(R.string.alerts_off_body),
            color = MeshaColors.Muted,
            style = MeshaType.cardSubtitle,
        )
        Spacer(Modifier.height(MeshaDimens.space4))
        if (canPrompt) {
            MeshaPrimaryButton(
                text = stringResource(R.string.alerts_off_turn_on),
                enabled = true,
                onClick = { launcher?.launch(permission.manifestPermission) },
            )
        } else {
            Text(
                text = stringResource(R.string.alerts_off_settings_help),
                color = MeshaColors.Faint,
                style = MeshaType.caption,
            )
            Spacer(Modifier.height(MeshaDimens.space3))
            MeshaPrimaryButton(
                text = stringResource(R.string.alerts_off_open_settings),
                enabled = true,
                onClick = { context.startActivity(notificationSettingsIntent(context.packageName)) },
            )
        }
    }
}

/**
 * Lands directly on this app's own notification settings — the exact screen holding the switch,
 * not a general settings page the person then has to hunt through. Available on every OS version
 * this app supports (minSdk 29).
 */
private fun notificationSettingsIntent(packageName: String): Intent =
    Intent(Settings.ACTION_APP_NOTIFICATION_SETTINGS)
        .putExtra(Settings.EXTRA_APP_PACKAGE, packageName)
