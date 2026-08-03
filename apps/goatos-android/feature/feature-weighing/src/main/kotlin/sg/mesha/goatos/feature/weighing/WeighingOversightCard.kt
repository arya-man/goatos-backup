package sg.mesha.goatos.feature.weighing

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.clickable
import androidx.compose.material3.Text
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.res.stringResource
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType

// telemetry:exempt This card renders backend read-model state; the close/reopen/abandon writes it
// offers are instrumented by the weighing service that owns them.

/**
 * One weighing shed row WITH the oversight writes the backend says this viewer may make.
 *
 * [canEnd] and [canReopen] are the server's own capability flags, not a role guess and not a
 * property of which screen is hosting the card: the Growth Director oversees from the Operators
 * surface while still executing their own sheds elsewhere, so every surface that can show these
 * rows asks the same two flags rather than one screen owning the authority.
 */
@Composable
internal fun WeighingOversightCard(
    row: WeighingAssignmentUiRow,
    canEnd: Boolean = false,
    canReopen: Boolean = false,
    onReopen: () -> Unit = {},
    onClose: (String) -> Unit = {},
    /**
     * REQUESTS an abandon; it does not perform one. Abandon emits weighing.shed.abandoned and has
     * no inverse, so the reason belongs to the person ending the work -- the card cannot invent it
     * and must not fire the write straight off a tap. The host collects both.
     */
    onAbandon: () -> Unit = {},
) {
    val complete = row.isClosed
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(8.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(8.dp))
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = row.label,
                    color = MeshaColors.Ink,
                    style = MeshaType.cardTitle,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
                if (complete) {
                    if (canReopen) {
                        Text(
                            text = stringResource(R.string.weighing_leadership_tap_to_reopen),
                            color = MeshaColors.BrandD,
                            fontSize = 11.sp,
                            fontWeight = FontWeight.SemiBold,
                            modifier = Modifier
                                .padding(top = 4.dp)
                                .minimumInteractiveComponentSize()
                                .clip(RoundedCornerShape(4.dp))
                                .clickable(
                                    role = Role.Button,
                                    onClick = onReopen,
                                ),
                        )
                    }
                } else {
                    if (canEnd && row.canClose) {
                        Text(
                            text = stringResource(R.string.weighing_leadership_close),
                            color = MeshaColors.BrandD,
                            fontSize = 11.sp,
                            fontWeight = FontWeight.SemiBold,
                            modifier = Modifier
                                .padding(top = 4.dp)
                                .minimumInteractiveComponentSize()
                                .clip(RoundedCornerShape(4.dp))
                                .clickable(
                                    role = Role.Button,
                                    onClick = { onClose("closed from mobile leadership") },
                                ),
                        )
                    } else {
                        Text(
                            text = if (row.pendingVerificationCount > 0) {
                                stringResource(
                                    R.string.weighing_leadership_awaiting_verification,
                                    row.pendingVerificationCount,
                                )
                            } else {
                                stringResource(R.string.weighing_leadership_close_pending)
                            },
                            color = MeshaColors.Muted,
                            fontSize = 11.sp,
                            fontWeight = FontWeight.SemiBold,
                            modifier = Modifier.padding(top = 4.dp),
                        )
                    }
                    // Abandon is NOT gated on readyToClose: it exists precisely for work that will
                    // never reach that state (the animals moved, the day was rained off). Close and
                    // abandon record different outcomes, so they are offered as separate actions.
                    if (canEnd) {
                        Text(
                            text = stringResource(R.string.weighing_leadership_abandon),
                            color = MeshaColors.Danger,
                            fontSize = 11.sp,
                            fontWeight = FontWeight.SemiBold,
                            modifier = Modifier
                                .padding(top = 4.dp)
                                .minimumInteractiveComponentSize()
                                .clip(RoundedCornerShape(4.dp))
                                .clickable(
                                    role = Role.Button,
                                    onClick = onAbandon,
                                ),
                        )
                    }
                }
            }
            Text(
                text = row.status.ifBlank { stringResource(R.string.weighing_status_scheduled) },
                color = if (complete) MeshaColors.Ok else MeshaColors.BrandD,
                fontSize = 12.sp,
                fontWeight = FontWeight.Bold,
                modifier = Modifier.padding(start = 12.dp),
            )
        }
        Text(
            text = if (row.category.equals("per_shed_partition", ignoreCase = true)) {
                stringResource(R.string.weighing_lump_sum_weighing)
            } else {
                stringResource(R.string.weighing_individual_weighing)
            },
            color = MeshaColors.Muted,
            style = MeshaType.cardSubtitle,
        )
        Box(
            modifier = Modifier
                .fillMaxWidth()
                .height(7.dp)
                .clip(RoundedCornerShape(4.dp))
                .background(MeshaColors.Bg),
        ) {
            if (complete) {
                Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .height(7.dp)
                        .background(MeshaColors.Brand),
                )
            }
        }
        Text(
            text = if (complete) stringResource(R.string.weighing_status_completed) else stringResource(R.string.weighing_status_scheduled),
            color = MeshaColors.Muted,
            style = MeshaType.cardSubtitle,
        )
    }
}
