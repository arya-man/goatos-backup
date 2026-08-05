package sg.mesha.goatos.feature.auth

import android.app.Activity
import android.content.Intent
import android.net.Uri
import android.provider.Settings
import androidx.activity.compose.LocalActivityResultRegistryOwner
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.ActivityResultLauncher
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.component.MeshaCard
import sg.mesha.goatos.core.designsystem.component.MeshaPrimaryButton
import sg.mesha.goatos.core.designsystem.component.MeshaSectionLabel
import sg.mesha.goatos.core.designsystem.component.MeshaStatusPill
import sg.mesha.goatos.core.designsystem.component.MeshaTone
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaDimens
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.permissions.AppPermission
import sg.mesha.goatos.core.permissions.PermissionGrantResolver
import sg.mesha.goatos.core.permissions.PermissionGrantState
import sg.mesha.goatos.core.permissions.isPermissionGranted
import sg.mesha.goatos.core.permissions.shouldShowRationale

/**
 * Login-time, OS-version-aware device-permission gate
 * (docs/mobile/rfid-keyboard-reader.md permission matrix +
 * docs/mobile/trd-operator-mobile.md §7). This is an optional readiness card only;
 * mandatory operator capture checks live on the scan/submit route after role resolution.
 *
 * This is DEVICE permission UX only. Server-authoritative RBAC (TRD §7/§14) is
 * completely unaffected — no permission state here changes what the backend allows.
 *
 * Renders nothing once every OS-required permission is already granted, so a returning
 * operator's login screen stays uncluttered (no dead/static UI — repo standing rule).
 *
 * This composable owns no analytics client (no Hilt in this module — see feature-auth's
 * `build.gradle.kts`): [onGateShown]/[onPermissionAnswered] report every render-with-a-gap and
 * every grant/deny to the host (`LoginScreen` -> `MainActivity`, which holds the injected
 * [sg.mesha.goatos.core.analytics.AnalyticsPort]), the same pattern
 * [RoleBasedPermissionGate] already uses for the mandatory gate. A denied camera permission here
 * is exactly why an operator "can't record" later, and today nothing at all surfaces that.
 */
@Composable
fun PermissionGateCard(
    modifier: Modifier = Modifier,
    onGateShown: (missingPermissions: List<String>) -> Unit = {},
    onPermissionAnswered: (permission: String, granted: Boolean) -> Unit = { _, _ -> },
) {
    val context = LocalContext.current
    val activity = context as? Activity
    val required = remember { AppPermission.requiredForSdkInt() }
    if (required.isEmpty()) return

    // Only knowable as "permanently denied" after repeated real requests — before the
    // first, shouldShowRationale is also false but just means "never asked", and after a
    // single denial some phones report it false too (see PermissionGrantResolver's kdoc).
    var requestRounds by rememberSaveable { mutableStateOf(0) }
    var grantedSnapshot by remember {
        mutableStateOf(required.associateWith { isPermissionGranted(context, it) })
    }

    // rememberLauncherForActivityResult needs a real ActivityResultRegistryOwner (the
    // running app Activity). Non-Activity composition hosts — a Paparazzi screenshot
    // test or a static Studio @Preview — don't provide one; in the real app this is
    // never null, so the interactive grant flow always runs there. Falling back to a
    // read-only matrix (no launcher) keeps those hosts from crashing instead of hiding
    // the gate outright.
    val registryOwner = LocalActivityResultRegistryOwner.current
    val launcher: ActivityResultLauncher<Array<String>>? = if (registryOwner != null) {
        rememberLauncherForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) { results ->
            requestRounds += 1
            grantedSnapshot = required.associateWith { permission -> isPermissionGranted(context, permission) }
            // Report every answer the OS actually returned — only the still-missing permissions
            // were launched, so this never reports one already granted in an earlier round.
            // `results` keys are already manifest permission strings (RequestMultiplePermissions'
            // contract), not [AppPermission].
            results.forEach { (manifestPermission, granted) -> onPermissionAnswered(manifestPermission, granted) }
        }
    } else {
        null
    }

    val statuses = required.map { permission ->
        val granted = grantedSnapshot[permission] ?: isPermissionGranted(context, permission)
        val rationaleOk = activity?.let { shouldShowRationale(it, permission) } ?: true
        permission to PermissionGrantResolver.resolve(
            isGranted = granted,
            requestCount = requestRounds,
            shouldShowRationale = rationaleOk,
        )
    }

    if (statuses.all { it.second == PermissionGrantState.GRANTED }) return

    val needsRequest = statuses.filter { it.second == PermissionGrantState.DENIED }.map { it.first }
    val stillMissing = statuses.filter { it.second != PermissionGrantState.GRANTED }.map { it.first.manifestPermission }

    // Fire once per DISTINCT missing set, not on every recomposition — keyed on the resolved
    // list so a parent recompose that leaves the same gap in place never double-counts it.
    LaunchedEffect(stillMissing) {
        if (stillMissing.isNotEmpty()) onGateShown(stillMissing)
    }

    MeshaCard(modifier = modifier) {
        MeshaSectionLabel(text = stringResource(R.string.perm_gate_title))
        Spacer(Modifier.height(MeshaDimens.space2))
        Text(
            text = stringResource(R.string.perm_gate_subtitle),
            color = MeshaColors.Muted,
            style = MeshaType.cardSubtitle,
        )
        Spacer(Modifier.height(MeshaDimens.space4))

        statuses.forEachIndexed { index, (permission, state) ->
            PermissionRow(
                permission = permission,
                state = state,
                onOpenSettings = { context.startActivity(appSettingsIntent(context.packageName)) },
            )
            if (index != statuses.lastIndex) Spacer(Modifier.height(MeshaDimens.space3))
        }

        if (needsRequest.isNotEmpty() && launcher != null) {
            Spacer(Modifier.height(MeshaDimens.space4))
            MeshaPrimaryButton(
                text = stringResource(R.string.perm_gate_grant_access),
                enabled = true,
                onClick = { launcher.launch(needsRequest.map { it.manifestPermission }.toTypedArray()) },
            )
        }

        Spacer(Modifier.height(MeshaDimens.space3))
        Text(
            text = stringResource(R.string.perm_gate_footer),
            color = MeshaColors.Faint,
            style = MeshaType.caption,
        )
    }
}

@Composable
private fun PermissionRow(
    permission: AppPermission,
    state: PermissionGrantState,
    onOpenSettings: () -> Unit,
) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Icon(
            iconFor(permission),
            contentDescription = null,
            tint = MeshaColors.Muted,
            modifier = Modifier.size(MeshaDimens.iconMd),
        )
        Spacer(Modifier.width(10.dp))
        Column(modifier = Modifier.weight(1f)) {
            Text(text = labelFor(permission), color = MeshaColors.Ink, style = MeshaType.bodyStrong)
            Text(text = rationaleFor(permission), color = MeshaColors.Faint, style = MeshaType.caption)
        }
        Spacer(Modifier.width(8.dp))
        when (state) {
            PermissionGrantState.GRANTED ->
                MeshaStatusPill(label = stringResource(R.string.perm_status_granted), tone = MeshaTone.Ok)
            PermissionGrantState.DENIED ->
                MeshaStatusPill(label = stringResource(R.string.perm_status_needed), tone = MeshaTone.Warn)
            PermissionGrantState.PERMANENTLY_DENIED ->
                Text(
                    text = stringResource(R.string.perm_status_open_settings),
                    color = MeshaColors.Danger,
                    style = MeshaType.pill,
                    modifier = Modifier
                        .minimumInteractiveComponentSize()
                        .clickable(onClick = onOpenSettings),
                )
        }
    }
}

private fun appSettingsIntent(packageName: String): Intent =
    Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS).apply {
        data = Uri.fromParts("package", packageName, null)
    }

@Composable
private fun iconFor(permission: AppPermission): ImageVector = when (permission) {
    AppPermission.CAMERA -> MeshaIcons.Video
    AppPermission.BLUETOOTH_CONNECT -> MeshaIcons.Bluetooth
    AppPermission.NOTIFICATIONS -> MeshaIcons.Bell
    AppPermission.LOCATION -> MeshaIcons.Home
}

@Composable
private fun labelFor(permission: AppPermission): String = when (permission) {
    AppPermission.CAMERA -> stringResource(R.string.perm_label_camera)
    AppPermission.BLUETOOTH_CONNECT -> stringResource(R.string.perm_label_bluetooth)
    AppPermission.NOTIFICATIONS -> stringResource(R.string.perm_label_notifications)
    AppPermission.LOCATION -> stringResource(R.string.perm_label_location)
}

@Composable
private fun rationaleFor(permission: AppPermission): String = when (permission) {
    AppPermission.CAMERA -> stringResource(R.string.perm_rationale_camera)
    AppPermission.BLUETOOTH_CONNECT -> stringResource(R.string.perm_rationale_bluetooth)
    AppPermission.NOTIFICATIONS -> stringResource(R.string.perm_rationale_notifications)
    AppPermission.LOCATION -> stringResource(R.string.perm_rationale_location)
}
