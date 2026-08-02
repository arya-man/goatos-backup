package sg.mesha.goatos.feature.auth

import android.app.Activity
import android.content.Context
import android.content.ContextWrapper
import android.content.Intent
import android.os.Build
import android.provider.Settings
import androidx.activity.compose.LocalActivityResultRegistryOwner
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.ActivityResultLauncher
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.AlertDialog
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
import sg.mesha.goatos.core.designsystem.component.MeshaPrimaryButton
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaDimens
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.model.nav.NavModule
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.permissions.AppPermission
import sg.mesha.goatos.core.permissions.PermissionGrantResolver
import sg.mesha.goatos.core.permissions.PermissionGrantState
import sg.mesha.goatos.core.permissions.isPermissionGranted
import sg.mesha.goatos.core.permissions.shouldShowRationale
import androidx.compose.ui.window.DialogProperties

// telemetry:exempt this composable owns no analytics client by design — it reports every prompt and every grant/deny through the onPermissionPrompted / onPermissionAnswered callbacks, which GoatOsShell wires to ShellModuleViewModel analytics. Adding a second tracker here would double-count.

/**
 * Role-based mandatory permission gate. Shown before any work screen is accessible.
 *
 * Requirements are derived from the backend-composed module list and the person's role:
 * - operator: location, camera, BLE (BLUETOOTH_SCAN/BLUETOOTH_CONNECT), notifications
 * - verifier/director/CEO: notifications + any permissions their workflows require
 *
 * The gate is NON-DISMISSIBLE: no back press, outside tap, or close button. App unusable
 * until all required permissions are granted. If the OS stops showing prompts
 * (shouldShowRequestPermissionRationale = false), redirects to app settings.
 *
 * On resume (returning from settings), re-checks and auto-dismisses when the full set
 * is granted.
 */
@Composable
fun RoleBasedPermissionGate(
    navState: NavState,
    onAllPermissionsGranted: () -> Unit = {},
    onPermissionPrompted: () -> Unit = {},
    onPermissionAnswered: (permission: String, granted: Boolean) -> Unit = { _, _ -> },
) {
    val context = LocalContext.current
    // LocalContext inside Compose is a ContextThemeWrapper, NOT the Activity, so a plain
    // `context as? Activity` is always null here. That null silently disabled the
    // "Don't ask again" -> settings branch (and previously crashed on `activity!!`).
    val activity = remember(context) { context.findActivity() }

    // Derive required permissions from the module list and role
    val requiredPermissions = remember(navState) {
        deriveRequiredPermissions(navState)
    }

    // Track which permissions are granted
    var grantedPermissions by remember { mutableStateOf(setOf<String>()) }
    var deniedPermanently by rememberSaveable { mutableStateOf(setOf<String>()) }

    // Re-check permissions on resume (returning from settings)
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) {
        val nowGranted = requiredPermissions.filter { perm ->
            isPermissionGranted(context, AppPermission.entries.first { it.manifestPermission == perm })
        }.toSet()
        grantedPermissions = nowGranted
        // `activity` is null until the composable is attached to one (and in previews/tests).
        // Treating "no activity" as "permanently denied" would strand the operator on the
        // settings screen, so only classify a denial as permanent when we can actually ask.
        val host = activity
        val permanentlyDenied = if (host == null) {
            emptySet()
        } else {
            requiredPermissions.filter { perm ->
                val appPermission = AppPermission.entries.first { it.manifestPermission == perm }
                !isPermissionGranted(context, appPermission) && !shouldShowRationale(host, appPermission)
            }.toSet()
        }
        deniedPermanently = permanentlyDenied

        // Auto-dismiss if all granted
        if (grantedPermissions.size == requiredPermissions.size && requiredPermissions.isNotEmpty()) {
            onAllPermissionsGranted()
        }
    }

    // Launcher for permission requests. This MUST be created unconditionally: Compose forbids
    // calling a @Composable inside an `if`, and gating it on a nullable registry owner produced a
    // dead "Grant permissions" button that silently did nothing when the owner was absent.
    val permissionLauncher: ActivityResultLauncher<Array<String>> =
        rememberLauncherForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) { results ->
            val granted = results.filter { it.value }.keys
            val denied = results.filter { !it.value }.keys
            grantedPermissions = granted.toSet()

            // Report each answer
            denied.forEach { perm -> onPermissionAnswered(perm, false) }
            granted.forEach { perm -> onPermissionAnswered(perm, true) }

            // Mark permanently denied permissions
            val host = activity
            deniedPermanently = if (host == null) {
                emptySet()
            } else {
                denied.filter { perm ->
                    !shouldShowRationale(host, AppPermission.entries.first { it.manifestPermission == perm })
                }.toSet()
            }

            // Auto-dismiss if all granted
            if (granted.size == requiredPermissions.size && requiredPermissions.isNotEmpty()) {
                onAllPermissionsGranted()
            }
        }

    // Initial permission check
    val initialGrants = remember {
        requiredPermissions.filter { perm ->
            isPermissionGranted(context, AppPermission.entries.first { it.manifestPermission == perm })
        }.toSet()
    }
    remember { grantedPermissions = initialGrants }

    // If all permissions are already granted, dismiss
    if (requiredPermissions.isEmpty() || grantedPermissions.size == requiredPermissions.size) {
        onAllPermissionsGranted()
        return
    }

    // Missing permissions
    val missingPermissions = requiredPermissions - grantedPermissions

    // Determine if we should show the prompt or the settings redirect
    val canPrompt = missingPermissions.all { perm ->
        deniedPermanently.contains(perm).not()
    }

    AlertDialog(
        onDismissRequest = { /* Non-dismissible */ },
        title = {
            Text(
                text = stringResource(R.string.mandatory_permissions_title),
                style = MeshaType.screenTitle,
            )
        },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(MeshaDimens.space2)) {
                Text(
                    text = stringResource(R.string.mandatory_permissions_body),
                    style = MeshaType.body,
                    color = MeshaColors.Muted,
                )
                Spacer(Modifier.height(MeshaDimens.space2))
                Text(
                    text = stringResource(R.string.mandatory_permissions_why),
                    style = MeshaType.caption,
                    color = MeshaColors.Faint,
                )
                if (!canPrompt) {
                    Spacer(Modifier.height(MeshaDimens.space3))
                    Text(
                        text = stringResource(R.string.mandatory_permissions_settings_help),
                        style = MeshaType.caption,
                        color = MeshaColors.Faint,
                    )
                }
            }
        },
        confirmButton = {
            MeshaPrimaryButton(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = MeshaDimens.space2),
                text = if (canPrompt) {
                    stringResource(R.string.mandatory_permissions_grant)
                } else {
                    stringResource(R.string.mandatory_permissions_settings)
                },
                enabled = true,
                onClick = {
                    if (canPrompt) {
                        onPermissionPrompted()
                        permissionLauncher.launch(missingPermissions.toTypedArray())
                    } else {
                        context.startActivity(appPermissionSettingsIntent(context.packageName))
                    }
                },
            )
        },
        dismissButton = null, // No cancel button — non-dismissible
        properties = DialogProperties(
            dismissOnBackPress = false,
            dismissOnClickOutside = false,
        ),
    )
}

/**
 * Derive required permissions from the NavState and modules.
 *
 * Operator: location, camera, BLE (BLUETOOTH_CONNECT on API 31+), notifications
 * Verifier/Director/CEO: notifications only (for now)
 *
 * Uses module availability as the source, not hardcoded role strings.
 */
fun deriveRequiredPermissions(navState: NavState): List<String> {
    // Detect operator by the presence of capture/scanning modules (vaccination_execute, weighing_execute)
    val isOperator = navState.featureFlags["vaccination_execute"] == true ||
            navState.featureFlags["weighing_execute"] == true

    val required = mutableListOf<String>() // mobile-guard:ignore: function-local, at most 4 permission names, discarded on return

    // Always require notifications
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
        required.add(AppPermission.NOTIFICATIONS.manifestPermission)
    }

    if (isOperator) {
        // Location is only declared (and therefore only grantable) up to API 30 — the manifest
        // caps ACCESS_FINE_LOCATION at maxSdkVersion 30 because BLUETOOTH_SCAN is
        // neverForLocation from Android 12. Requesting it on a newer phone can never succeed and
        // would strand the operator behind this mandatory gate, so honour the SDK window.
        if (AppPermission.LOCATION in AppPermission.requiredForSdkInt()) {
            required.add(AppPermission.LOCATION.manifestPermission)
        }

        // Operator requires camera for proof capture
        required.add(AppPermission.CAMERA.manifestPermission)

        // Operator requires Bluetooth for RFID reader (API 31+)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            required.add(AppPermission.BLUETOOTH_CONNECT.manifestPermission)
        }
    }

    return required
}

/**
 * Lands directly on this app's full permission settings page — the app details screen
 * where all permissions are visible and toggleable. Available on every OS version
 * this app supports (minSdk 29). Used when the OS stops showing permission prompts
 * ("Don't ask again" was clicked), and the user must manually enable permissions
 * in system settings.
 */
private fun appPermissionSettingsIntent(packageName: String): Intent =
    Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS).apply {
        data = android.net.Uri.fromParts("package", packageName, null)
    }

private tailrec fun Context.findActivity(): Activity? = when (this) {
    is Activity -> this
    is ContextWrapper -> baseContext.findActivity()
    else -> null
}
