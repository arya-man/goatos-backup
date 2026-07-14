package sg.mesha.goatos.ui

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.R
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaDimens
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import androidx.compose.ui.res.stringResource

/**
 * The force-update gate (`v-force-update`). A non-dismissible full-screen state shown
 * when the build is below the server-declared minimum supported version: the app is
 * unusable until the operator installs the latest build (this app is distributed via
 * Firebase App Distribution, not the Play Store).
 *
 * Anatomy ported from the mock's centered hero-state (`.device2 .big` tile + heading +
 * body + `.btn`). Back is swallowed so there is no way past it, and there is no dismiss
 * affordance by design.
 *
 * @param updateUrl the install link the CTA opens; blank ⇒ CTA is disabled-with-reason.
 * @param installedVersionName the running build's versionName, shown for support/context.
 * @param onUpdate invoked with a non-blank [updateUrl] when the CTA is tapped.
 */
@Composable
fun ForceUpdateScreen(
    updateUrl: String,
    installedVersionName: String,
    onUpdate: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    // Swallow the system back gesture/button — the gate has no way past it.
    BackHandler(enabled = true) {}

    val hasLink = updateUrl.isNotBlank()

    Column(
        modifier = modifier
            .fillMaxSize()
            .background(MeshaColors.PageBg)
            .verticalScroll(rememberScrollState())
            .windowInsetsPadding(WindowInsets.safeDrawing)
            .padding(horizontal = MeshaDimens.gutter),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Spacer(Modifier.height(MeshaDimens.space8))

        Eyebrow(stringResource(R.string.force_update_eyebrow))
        Spacer(Modifier.height(MeshaDimens.space6))

        HeroIconTile()
        Spacer(Modifier.height(MeshaDimens.space6))

        Text(
            text = stringResource(R.string.force_update_title),
            color = MeshaColors.Ink,
            style = MeshaType.screenTitle,
            textAlign = TextAlign.Center,
        )
        Spacer(Modifier.height(MeshaDimens.space3))

        Text(
            text = stringResource(R.string.force_update_message),
            color = MeshaColors.Muted,
            style = MeshaType.body,
            textAlign = TextAlign.Center,
            lineHeight = 21.sp,
        )
        Spacer(Modifier.height(MeshaDimens.space5))

        InstalledVersionChip(installedVersionName)
        Spacer(Modifier.height(MeshaDimens.space7))

        UpdatePrimaryButton(
            text = stringResource(R.string.force_update_cta),
            enabled = hasLink,
            onClick = { if (hasLink) onUpdate(updateUrl) },
        )

        if (!hasLink) {
            Spacer(Modifier.height(MeshaDimens.space3))
            Text(
                text = stringResource(R.string.force_update_no_link),
                color = MeshaColors.Faint,
                style = MeshaType.caption,
                textAlign = TextAlign.Center,
            )
        }

        Spacer(Modifier.height(MeshaDimens.space8))
    }
}

@Composable
private fun Eyebrow(text: String) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Box(
            modifier = Modifier
                .size(8.dp)
                .clip(RoundedCornerShape(999.dp))
                .background(MeshaColors.Brand),
        )
        Spacer(Modifier.width(9.dp))
        Text(
            text = text.uppercase(),
            color = MeshaColors.Brand2,
            style = MeshaType.eyebrow,
        )
    }
}

/** The mock's `.device2 .big` hero tile — 80dp, soft-brand fill, hairline, brand icon. */
@Composable
private fun HeroIconTile() {
    Box(
        modifier = Modifier
            .size(80.dp)
            .clip(RoundedCornerShape(MeshaDimens.radiusHero))
            .background(MeshaColors.BrandGradientSoft)
            .border(MeshaDimens.hairline, MeshaColors.Hair, RoundedCornerShape(MeshaDimens.radiusHero)),
        contentAlignment = Alignment.Center,
    ) {
        Icon(
            imageVector = MeshaIcons.Download,
            contentDescription = null,
            tint = MeshaColors.Brand,
            modifier = Modifier.size(36.dp),
        )
    }
}

@Composable
private fun InstalledVersionChip(versionName: String) {
    Text(
        text = stringResource(R.string.force_update_installed, versionName),
        color = MeshaColors.Muted,
        style = MeshaType.pill,
        modifier = Modifier
            .clip(MeshaDimens.pill)
            .background(MeshaColors.Surf2)
            .border(MeshaDimens.hairline, MeshaColors.Hair, MeshaDimens.pill)
            .padding(horizontal = 12.dp, vertical = 6.dp),
    )
}

@Composable
private fun UpdatePrimaryButton(text: String, enabled: Boolean, onClick: () -> Unit) {
    val shape = RoundedCornerShape(MeshaDimens.radiusButton)
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .heightIn(min = MeshaDimens.minTapButton)
            .clip(shape)
            .alpha(if (enabled) 1f else 0.5f)
            .background(MeshaColors.BrandGradient)
            .clickable(enabled = enabled, onClick = onClick)
            .padding(15.dp),
        contentAlignment = Alignment.Center,
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Icon(
                imageVector = MeshaIcons.Download,
                contentDescription = null,
                tint = MeshaColors.OnBrand,
                modifier = Modifier.size(MeshaDimens.iconMd),
            )
            Spacer(Modifier.width(9.dp))
            Text(text = text, color = MeshaColors.OnBrand, style = MeshaType.button)
        }
    }
}

@Preview(name = "Force update", showBackground = true, backgroundColor = 0xFF0A0F0C, widthDp = 380, heightDp = 820)
@Composable
private fun ForceUpdatePreview() {
    GoatOsTheme {
        ForceUpdateScreen(
            updateUrl = "https://appdistribution.firebase.dev/i/abc123",
            installedVersionName = "0.1.0",
            onUpdate = {},
        )
    }
}

@Preview(name = "Force update - no link", showBackground = true, backgroundColor = 0xFF0A0F0C, widthDp = 380, heightDp = 820)
@Composable
private fun ForceUpdateNoLinkPreview() {
    GoatOsTheme {
        ForceUpdateScreen(
            updateUrl = "",
            installedVersionName = "0.1.0",
            onUpdate = {},
        )
    }
}
