package sg.mesha.goatos.feature.auth

import android.app.Activity
import android.Manifest
import android.content.Context
import android.content.ContextWrapper
import android.content.Intent
import android.content.pm.PackageManager
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
 * until all required permissions are granted. Once the phone has genuinely stopped showing
 * prompts, it offers the settings route instead — but only after
 * [PermissionGrantResolver.MIN_REQUESTS_BEFORE_BLOCKED] real asks. A single "no" is always
 * retryable in place: this gate previously trusted `shouldShowRequestPermissionRationale`
 * after one ask, and worse, ran that classification on the FIRST ON_RESUME before anything
 * had ever been asked — which sent Xiaomi/MIUI operators straight to a settings dead end
 * for permissions the OS had never marked blocked.
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

    // How many times this gate has actually put the OS prompt in front of the person. One
    // "no" is not proof the phone has stopped asking (see PermissionGrantResolver), so the
    // settings route only appears once this reaches MIN_REQUESTS_BEFORE_BLOCKED.
    //
    // rememberSaveable, not a persisted value: it must survive a rotation or the OS killing
    // the app while the permission prompt is on top (routine on low-memory MIUI phones,
    // where losing it mid-flow would silently restart the count). It deliberately does NOT
    // survive a cold start — a fresh launch re-asking once is harmless, whereas a stale
    // saved count could send someone to settings for a permission they can still be asked
    // for. The failure direction is always "ask again", never "dead end".
    var requestRounds by rememberSaveable { mutableStateOf(0) }

    fun readGranted(): Set<String> = requiredPermissions.filter { perm ->
        context.checkSelfPermission(perm) == PackageManager.PERMISSION_GRANTED
    }.toSet()

    // Re-check permissions on resume (returning from settings)
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) {
        grantedPermissions = readGranted()

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
            requestRounds += 1

            // Report each answer
            results.forEach { (perm, granted) -> onPermissionAnswered(perm, granted) }

            // Re-read from the OS rather than trusting `results`: only the still-missing
            // permissions were launched, so `results` alone would drop the ones granted in
            // an earlier round and leave this gate stuck on a fully-granted phone.
            grantedPermissions = readGranted()

            // Auto-dismiss if all granted
            if (grantedPermissions.size == requiredPermissions.size && requiredPermissions.isNotEmpty()) {
                onAllPermissionsGranted()
            }
        }

    // Initial permission check
    val initialGrants = remember {
        requiredPermissions.filter { perm ->
            context.checkSelfPermission(perm) == PackageManager.PERMISSION_GRANTED
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

    // Determine if we should show the prompt or the settings redirect. `activity` is null
    // until the composable is attached to one (and in previews/tests); treating "no
    // activity" as blocked would strand the operator on the settings screen, so only
    // classify a denial as blocked when we can actually ask.
    val host = activity
    val canPrompt = host == null || missingPermissions.none { perm ->
        PermissionGrantResolver.resolve(
            isGranted = false,
            requestCount = requestRounds,
            shouldShowRationale = host.shouldShowRequestPermissionRationale(perm),
        ) == PermissionGrantState.PERMANENTLY_DENIED
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
                } else if (requestRounds > 0) {
                    // Asked before and still missing, but the phone will still ask again.
                    // Say so, so the button does not look like it did nothing.
                    Spacer(Modifier.height(MeshaDimens.space3))
                    Text(
                        text = stringResource(R.string.mandatory_permissions_retry_help),
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
                        permissionLauncher.launch(requestPermissionsFor(missingPermissions).toTypedArray())
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
 * Operator: the full proof/scan bundle up front: camera, microphone, precise
 * location, BLE/Nearby Devices, notifications.
 * Verifier/Director/CEO: notifications only (for now)
 *
 * Uses module availability as the source, not hardcoded role strings.
 */
fun deriveRequiredPermissions(navState: NavState): List<String> {
    // Detect operator by the presence of capture/scanning modules (vaccination_execute, weighing_execute)
    val isOperator = navState.featureFlags["vaccination_execute"] == true ||
            navState.featureFlags["weighing_execute"] == true

    val required = mutableListOf<String>() // mobile-guard:ignore: function-local, at most 6 permission names, discarded on return

    // Always require notifications
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
        required.add(AppPermission.NOTIFICATIONS.manifestPermission)
    }

    if (isOperator) {
        // Operators record proof from the field. Ask for the same mandatory bundle the
        // capture gate enforces, before the operator enters Feed/Vaccination/Weighing.
        required.add(Manifest.permission.CAMERA)
        required.add(Manifest.permission.RECORD_AUDIO)
        required.add(Manifest.permission.ACCESS_FINE_LOCATION)

        // Operator requires Android 12+ Nearby Devices permissions for RFID reader readiness and
        // future scan/pairing affordances.
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            required.add(AppPermission.BLUETOOTH_CONNECT.manifestPermission)
            required.add(AppPermission.BLUETOOTH_SCAN.manifestPermission)
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

internal fun requestPermissionsFor(missingPermissions: Collection<String>): List<String> = buildList {
    missingPermissions.forEach { permission ->
        if (permission == Manifest.permission.ACCESS_FINE_LOCATION) {
            add(Manifest.permission.ACCESS_COARSE_LOCATION)
        }
        add(permission)
    }
}.distinct()
