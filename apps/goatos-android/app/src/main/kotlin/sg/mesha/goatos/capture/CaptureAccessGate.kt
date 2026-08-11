package sg.mesha.goatos.capture

import android.Manifest
import android.app.Activity
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.provider.Settings
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.component.MeshaPrimaryButton
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType

/**
 * MANDATORY, blocking permission gate for the capture/submit surface
 * (docs/mobile/proof-capture-sync-and-e2e.md §4). Login is role-neutral and never asks
 * verifiers or leaders for capture access. Operator capture entry points request camera,
 * microphone, precise location, OS-applicable notifications, and Android-12+ Bluetooth.
 * There is no degraded operator capture path: if any required
 * permission is denied, [content] never composes. Runtime notification permission exists
 * only on Android 13+, so Android 12 must never include it in the all-granted check.
 *
 * The microphone is on that blocking list deliberately (2026-08-08): a proof clip without the
 * operator's voice is a weaker proof, so audio is compulsory rather than best-effort — see
 * InAppVideoRecorder's `withAudioEnabled`. `internal` rather than `private` so
 * ProofAudioCaptureTest can assert the mic never falls back out of the mandatory set.
 */
internal val MANDATORY_CAPTURE_PERMISSIONS: List<String> =
    mandatoryCapturePermissionsForSdk(Build.VERSION.SDK_INT)

internal fun mandatoryCapturePermissionsForSdk(sdkInt: Int): List<String> = buildList {
    add(Manifest.permission.CAMERA)
    add(Manifest.permission.RECORD_AUDIO)
    add(Manifest.permission.ACCESS_FINE_LOCATION)
    if (sdkInt >= Build.VERSION_CODES.TIRAMISU) {
        add(Manifest.permission.POST_NOTIFICATIONS)
    }
    if (sdkInt >= Build.VERSION_CODES.S) {
        add(Manifest.permission.BLUETOOTH_CONNECT)
    }
}

@Composable
fun CaptureAccessGate(
    modifier: Modifier = Modifier,
    content: @Composable () -> Unit,
) {
    val context = LocalContext.current
    val activity = context as? Activity
    var hasRequestedOnce by rememberSaveable { mutableStateOf(false) }
    var grantedSnapshot by remember {
        mutableStateOf(MANDATORY_CAPTURE_PERMISSIONS.associateWith { android.content.pm.PackageManager.PERMISSION_GRANTED == context.checkSelfPermission(it) })
    }

    val launcher = rememberLauncherForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) {
        hasRequestedOnce = true
        grantedSnapshot = MANDATORY_CAPTURE_PERMISSIONS.associateWith { permission ->
            android.content.pm.PackageManager.PERMISSION_GRANTED == context.checkSelfPermission(permission)
        }
    }

    val allGranted = grantedSnapshot.values.all { it }
    if (allGranted) {
        content()
        return
    }
    val deniedPermissions = grantedSnapshot.filterValues { !it }.keys
    val shouldOpenSettings = hasRequestedOnce &&
        activity != null &&
        deniedPermissions.any { permission ->
            !activity.shouldShowRequestPermissionRationale(permission)
        }

    Column(
        modifier = modifier
            .fillMaxSize()
            .background(MeshaColors.Bg)
            .padding(24.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Icon(MeshaIcons.Video, contentDescription = null, tint = MeshaColors.Brand, modifier = Modifier.size(40.dp))
        Spacer(Modifier.height(16.dp))
        Text(
            "Upload access is required",
            color = MeshaColors.Ink,
            style = MeshaType.headerTitle,
        )
        Spacer(Modifier.height(8.dp))
        Text(
            "Allow camera, microphone, and precise location to continue.",
            color = MeshaColors.Muted,
            style = MeshaType.rowLabel,
        )
        Spacer(Modifier.height(20.dp))
        grantedSnapshot.forEach { (permission, granted) ->
            Row(Modifier.fillMaxWidth().padding(vertical = 4.dp), horizontalArrangement = Arrangement.Center) {
                Text(
                    text = "${permissionLabel(permission)}: ${if (granted) "granted" else "needed"}",
                    color = if (granted) MeshaColors.Brand else MeshaColors.Warn,
                    style = MeshaType.cardSubtitle,
                )
            }
        }
        Spacer(Modifier.height(20.dp))
        MeshaPrimaryButton(
            text = if (shouldOpenSettings) "Open Settings" else "Grant access",
            enabled = true,
            onClick = {
                if (shouldOpenSettings) {
                    context.startActivity(
                        Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS).apply {
                            data = Uri.fromParts("package", context.packageName, null)
                        },
                    )
                } else {
                    launcher.launch(MANDATORY_CAPTURE_PERMISSIONS.toTypedArray())
                }
            },
        )
        if (hasRequestedOnce && !allGranted) {
            Spacer(Modifier.height(10.dp))
            Text(
                if (shouldOpenSettings) {
                    "Enable the missing permissions in Settings, then return here."
                } else {
                    "If access is denied again, you may need to enable it from Settings."
                },
                color = MeshaColors.Faint,
                style = MeshaType.eyebrow,
            )
        }
    }
}

internal fun permissionLabel(permission: String): String = when (permission) {
    Manifest.permission.CAMERA -> "Camera"
    Manifest.permission.RECORD_AUDIO -> "Microphone"
    Manifest.permission.BLUETOOTH_CONNECT -> "Bluetooth (RFID reader)"
    Manifest.permission.ACCESS_FINE_LOCATION -> "Precise location"
    Manifest.permission.POST_NOTIFICATIONS -> "Notifications"
    else -> permission
}
