package sg.mesha.goatos.capture

import android.Manifest
import android.app.Activity
import android.os.Build
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
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.component.MeshaPrimaryButton
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors

/**
 * MANDATORY, blocking permission gate for the capture/submit surface
 * (docs/mobile/proof-capture-sync-and-e2e.md §4). Login is role-neutral and never asks
 * verifiers or leaders for capture access. Operator capture entry points request camera,
 * Bluetooth, notifications, and pre-Android-12 location when the RFID stack requires it.
 * There is no degraded operator capture path: if any required permission is denied,
 * [content] never composes.
 */
private val MANDATORY_CAPTURE_PERMISSIONS: List<String> = buildList {
    add(Manifest.permission.CAMERA)
    add(Manifest.permission.POST_NOTIFICATIONS) // no-op pre-33; harmless to request always.
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
        add(Manifest.permission.BLUETOOTH_CONNECT)
    } else {
        add(Manifest.permission.ACCESS_FINE_LOCATION) // Bluetooth dependency on pre-12.
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
            "Camera, RFID reader, and background upload access are required",
            color = MeshaColors.Ink,
            fontSize = 16.sp,
            fontWeight = FontWeight.W700,
        )
        Spacer(Modifier.height(8.dp))
        Text(
            "This work can't be scanned or proven without these. Grant the access below to continue.",
            color = MeshaColors.Muted,
            fontSize = 13.sp,
        )
        Spacer(Modifier.height(20.dp))
        grantedSnapshot.forEach { (permission, granted) ->
            Row(Modifier.fillMaxWidth().padding(vertical = 4.dp), horizontalArrangement = Arrangement.Center) {
                Text(
                    text = "${permissionLabel(permission)}: ${if (granted) "granted" else "needed"}",
                    color = if (granted) MeshaColors.Brand else MeshaColors.Warn,
                    fontSize = 12.sp,
                )
            }
        }
        Spacer(Modifier.height(20.dp))
        MeshaPrimaryButton(
            text = "Grant access",
            enabled = true,
            onClick = { launcher.launch(MANDATORY_CAPTURE_PERMISSIONS.toTypedArray()) },
        )
        if (hasRequestedOnce && !allGranted) {
            Spacer(Modifier.height(10.dp))
            Text(
                "If a permission is permanently denied, open Settings to grant it.",
                color = MeshaColors.Faint,
                fontSize = 11.5.sp,
            )
        }
    }
}

private fun permissionLabel(permission: String): String = when (permission) {
    Manifest.permission.CAMERA -> "Camera"
    Manifest.permission.BLUETOOTH_CONNECT -> "Bluetooth (RFID reader)"
    Manifest.permission.ACCESS_FINE_LOCATION -> "Location (Bluetooth dependency)"
    Manifest.permission.POST_NOTIFICATIONS -> "Notifications"
    else -> permission
}
