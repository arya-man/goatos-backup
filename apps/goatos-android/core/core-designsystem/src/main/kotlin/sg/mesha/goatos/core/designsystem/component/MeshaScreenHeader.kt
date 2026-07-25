package sg.mesha.goatos.core.designsystem.component

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.R
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.nav.LocalDrawerOpener
import sg.mesha.goatos.core.designsystem.nav.LocalIsTopLevelRoot
import sg.mesha.goatos.core.designsystem.theme.MeshaColors

/**
 * The one screen header every destination renders — a port of the mock's `.vhead`
 * (`mock/vaccination-mobile-mock.html`):
 *
 * ```text
 * [ leading ] [ eyebrow / title / subtitle ] ......... [ actions ]
 *             [ below: freshness, banners, … ]
 * ```
 *
 * ### Why the leading affordance is NOT a parameter
 *
 * The drawer hamburger used to be opt-in: each screen read `LocalDrawerOpener` and drew its
 * own menu button. Two screens did; every other L0 root either drew nothing (Counts, Alerts,
 * You) or drew a Back arrow on a root route (Vaccination/Sheds). An operator who opened one
 * of those had no way back to another module — the exact defect this component removes.
 *
 * So the leading slot is *derived*, never passed:
 *
 * | shell state                                   | leading      |
 * |-----------------------------------------------|--------------|
 * | L0 root + EXPANDED chrome (opener non-null)    | drawer/menu  |
 * | L1+ drill (opener null) and [onBack] supplied  | Up/Back      |
 * | otherwise                                      | nothing      |
 *
 * A screen therefore cannot forget its drawer, and — because the shell nulls the opener off
 * L0 — a drill cannot accidentally grow one. A dual-identity screen (one composable hosted
 * at both an L0 route and an L1 drill route, like Sheds at `/vaccination` and
 * `/calendar/drive`) simply passes [onBack] and gets the right affordance on each.
 *
 * Membership derives entirely from the backend-composed nav (`BootstrapResponse.modules` ->
 * `nav_items`), so a new module inherits correct chrome with no client change. See
 * `docs/decisions/android-navigation-stack.md`.
 *
 * @param title the screen title. Backend-owned copy renders verbatim; fixed chrome titles
 *   are localized by the caller via `stringResource`.
 * @param onBack Up handler for a hosted (L1+) destination. Ignored while a drawer is
 *   available, so an L0 root never shows Back in place of its drawer.
 * @param below content under the title block — the offline-first freshness indicator
 *   (`SyncStatusIndicator`), coverage banners, and similar per-screen status.
 * @param actions trailing icon buttons (Refresh, Alerts, avatar, …). Use [MeshaIconButton]
 *   so every screen's actions share the mock's `.ib` shape.
 */
@Composable
fun MeshaScreenHeader(
    title: String,
    modifier: Modifier = Modifier,
    eyebrow: String? = null,
    subtitle: String? = null,
    onBack: (() -> Unit)? = null,
    titleColor: Color = MeshaColors.Ink,
    eyebrowColor: Color = MeshaColors.Faint,
    contentPadding: PaddingValues = PaddingValues(horizontal = 16.dp, vertical = 12.dp),
    below: @Composable ColumnScope.() -> Unit = {},
    actions: @Composable RowScope.() -> Unit = {},
) {
    // Shell-owned, route-derived. Non-null ⇒ this destination is an exact L0 root whose
    // module drawer can be opened; null ⇒ hosted drill or single-module (MINIMAL) chrome.
    val openDrawer = LocalDrawerOpener.current
    val isTopLevelRoot = LocalIsTopLevelRoot.current

    Row(
        modifier = modifier.fillMaxWidth().padding(contentPadding),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        when {
            openDrawer != null -> {
                MeshaIconButton(
                    icon = MeshaIcons.Menu,
                    // Client-static nav chrome, so it lives in this module's strings.xml
                    // (alongside nav_modules/nav_soon) and follows the app locale. It names
                    // the drawer's own group header — "Modules" — so the spoken label and
                    // the surface it opens agree in every language.
                    contentDescription = stringResource(R.string.nav_open_modules),
                    onClick = openDrawer,
                )
                Spacer(Modifier.width(12.dp))
            }
            onBack != null && !isTopLevelRoot -> {
                MeshaIconButton(
                    icon = MeshaIcons.ChevronLeft,
                    contentDescription = stringResource(R.string.nav_back),
                    onClick = onBack,
                )
                Spacer(Modifier.width(12.dp))
            }
        }

        Column(modifier = Modifier.weight(1f)) {
            if (!eyebrow.isNullOrBlank()) {
                Text(
                    text = eyebrow,
                    color = eyebrowColor,
                    fontSize = 10.5.sp,
                    fontWeight = FontWeight.W700,
                    letterSpacing = 0.6.sp,
                )
            }
            Text(
                text = title,
                color = titleColor,
                fontSize = 22.sp,
                fontWeight = FontWeight.W700,
            )
            if (!subtitle.isNullOrBlank()) {
                Text(
                    text = subtitle,
                    color = MeshaColors.Muted,
                    fontSize = 12.5.sp,
                    fontWeight = FontWeight.W600,
                    modifier = Modifier.padding(top = 4.dp),
                )
            }
            below()
        }

        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            content = actions,
        )
    }
}
