package sg.mesha.goatos.feature.counts

// telemetry:exempt pure stateless renderer; ApprovalViewModel owns the counts_approval_*
// analytics events and the CrashReporter non-fatal on every queue-load and decision failure.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.paging.LoadState
import androidx.paging.compose.LazyPagingItems
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.RefreshOnResume

/**
 * The approver's pending-decision queue (`/counts/approvals`) — the single L0 root of the
 * APPROVALS module.
 *
 * Its own module, not a Counts tab (maintainer decision 2026-08-05, superseding the 2026-07-21
 * removal of approvals from mobile). Counts stays capture-only for the operators who record
 * births, deaths and shifts; approving is a different job held by different people, so it gets its
 * own drawer entry gated on its own authority. The route keeps the `/counts/...` path because the
 * backing API is still `/app/counts/approvals`.
 *
 * ### What this screen is
 * Birth, death, and shifting are RAISED by field operators and applied only when approved. So this
 * queue is where the herd actually changes: approving a birth creates the kid and generates its
 * vaccination obligations, approving a death exits the animal and cancels its open obligations, and
 * approving a shifting relocates the named animals and re-scopes their shed-scoped obligations.
 * That is why a rejection REQUIRES a reason — the operator who raised it has to know what to fix.
 *
 * ### What this screen does NOT decide
 * Authority. The backend returns only the request types this caller may decide (a park_head sees
 * shifting; a ceo_internal sees birth and death), and re-checks that per row on decide. This screen
 * renders whatever arrives and never inspects a role — a client-side authority check would be both
 * the banned `role ==` gate and a second source of truth that could drift from the server's.
 *
 * Rows arrive as a Room-backed [LazyPagingItems] window (~20/page, keyset), so the list is bounded
 * on both the network and DB sides and re-entering the tab renders the cached queue immediately.
 */

@Immutable
data class ApprovalRowUi(
    val requestId: String,
    /** Backend-composed request-type label, rendered verbatim. */
    val typeLabel: String,
    val requestType: String,
    /**
     * Who raised it, as a NAME the backend resolved. Blank when the backend could not resolve one,
     * in which case the row omits the line entirely.
     *
     * Never a user id. This field used to carry `raised_by_user_id` straight through, so the screen
     * rendered "Raised by 7f3a91c2-4d18-…" at an approver — a copy-firewall violation and useless
     * to the person deciding. The name is composed server-side (golden frontend rule: the label is
     * backend-owned), and an unresolvable raiser drops the line rather than falling back to the id.
     */
    val raisedBy: String,
    /** When, already formatted for display. */
    val raisedAt: String,
    /**
     * A short, farm-readable line describing what the request contains, COMPOSED BY THE BACKEND
     * with every id already resolved to a name ("12 animals · Gandhi 1 → Gandhi 2 · Routine").
     * Rendered verbatim; blank when the payload held nothing nameable, and then omitted.
     *
     * This used to be built here from the raw payload, which is how "to shed 0b4e-…" reached an
     * approver: the phone has no name source for a shed id. See [raisedBy].
     */
    val summaryLine: String,
)

@Immutable
data class ApprovalUiState(
    /** The row whose reject sheet is open; null when no rejection is being composed. */
    val rejectingRequestId: String? = null,
    val rejectReason: String = "",
    /** Set while a decision is being queued, so a double-tap cannot enqueue twice. */
    val decidingRequestId: String? = null,
    val message: String? = null,
    val isError: Boolean = false,
)

sealed interface ApprovalEvent {
    data class Approve(val requestId: String) : ApprovalEvent
    data class OpenReject(val requestId: String) : ApprovalEvent
    data class EditRejectReason(val value: String) : ApprovalEvent
    data object CancelReject : ApprovalEvent
    data object ConfirmReject : ApprovalEvent
    data object Refresh : ApprovalEvent
}

@Composable
fun ApprovalScreen(
    state: ApprovalUiState,
    rows: LazyPagingItems<ApprovalRowUi>,
    onEvent: (ApprovalEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    // Refresh-on-open (docs/decisions/android-offline-first.md, mandatory for every read screen):
    // the cached queue renders instantly from Room and a background refresh fires every time the
    // approver lands on or returns to this tab. A retained ViewModel on the backstack must never
    // show a queue that was fetched once at creation -- this is the one surface where minutes-old
    // data actively misleads, because a request raised while the approver was elsewhere would be
    // invisible, and one they already decided elsewhere would still look actionable.
    RefreshOnResume { onEvent(ApprovalEvent.Refresh) }

    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = stringResource(R.string.counts_approval_title),
            subtitle = stringResource(R.string.counts_approval_subtitle),
        )
        LazyColumn(
            modifier = Modifier.fillMaxSize().padding(horizontal = 16.dp),
            contentPadding = PaddingValues(top = 8.dp, bottom = 28.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            state.message?.let { message ->
                item(key = "message") {
                    ApprovalBanner(message = message, isError = state.isError)
                }
            }

            // A page-load failure is reported NEXT TO the cached rows, never as a wipe: an
            // approver offline in a shed still sees the queue they last synced.
            val refreshError = rows.loadState.refresh as? LoadState.Error
            if (refreshError != null && rows.itemCount > 0) {
                item(key = "stale") {
                    ApprovalBanner(
                        message = stringResource(R.string.counts_approval_stale),
                        isError = false,
                    )
                }
            }

            if (rows.itemCount == 0 && rows.loadState.refresh !is LoadState.Loading) {
                item(key = "empty") {
                    ApprovalEmptyState(
                        isError = refreshError != null,
                        onRetry = { onEvent(ApprovalEvent.Refresh) },
                    )
                }
            }

            items(
                count = rows.itemCount,
                // Stable business identity, never the list index: a refresh that reorders the
                // queue must not make Compose reuse one request's row for another.
                key = { index -> rows.peek(index)?.requestId ?: "placeholder-$index" },
            ) { index ->
                val row = rows[index] ?: return@items
                ApprovalCard(
                    row = row,
                    busy = state.decidingRequestId == row.requestId,
                    rejecting = state.rejectingRequestId == row.requestId,
                    rejectReason = state.rejectReason,
                    onEvent = onEvent,
                )
            }

            if (rows.loadState.append is LoadState.Loading) {
                item(key = "appending") {
                    Text(
                        text = stringResource(R.string.counts_approval_loading_more),
                        color = MeshaColors.Faint,
                        style = MeshaType.sectionLabel,
                        modifier = Modifier.padding(vertical = 8.dp),
                    )
                }
            }

            item(key = "offline-note") {
                Text(
                    text = stringResource(R.string.counts_approval_offline_note),
                    color = MeshaColors.Faint,
                    style = MeshaType.sectionLabel,
                    modifier = Modifier.padding(top = 4.dp),
                )
            }
        }
    }
}

@Composable
private fun ApprovalCard(
    row: ApprovalRowUi,
    busy: Boolean,
    rejecting: Boolean,
    rejectReason: String,
    onEvent: (ApprovalEvent) -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(14.dp))
            .padding(12.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Icon(
                imageVector = MeshaIcons.forNavKey(row.requestType),
                contentDescription = null,
                tint = MeshaColors.Brand,
                modifier = Modifier.size(16.dp),
            )
            Text(row.typeLabel, color = MeshaColors.Ink, style = MeshaType.cardTitle)
        }
        // Who raised it / what it is / when — the three facts an approver needs to decide.
        // Each is omitted when the backend had nothing to say, so a row never shows a label with
        // an empty or id-shaped value after it.
        if (row.raisedBy.isNotBlank()) {
            Text(
                text = stringResource(R.string.counts_approval_raised_by, row.raisedBy),
                color = MeshaColors.Muted,
                style = MeshaType.cardSubtitle,
            )
        }
        Text(text = row.raisedAt, color = MeshaColors.Faint, style = MeshaType.sectionLabel)
        if (row.summaryLine.isNotBlank()) {
            Text(text = row.summaryLine, color = MeshaColors.Ink, style = MeshaType.cardSubtitle)
        }

        if (rejecting) {
            // A rejection is not actionable without a reason, so the reason is composed inline
            // and Confirm stays disabled until one is entered.
            CountsTextField(
                value = rejectReason,
                onValueChange = { onEvent(ApprovalEvent.EditRejectReason(it)) },
                label = stringResource(R.string.counts_approval_reject_reason),
                required = true,
                supporting = stringResource(R.string.counts_approval_reject_reason_hint),
            )
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Box(Modifier.weight(1f)) {
                    ApprovalAction(
                        label = stringResource(R.string.counts_approval_cancel),
                        tone = MeshaColors.Muted,
                        enabled = !busy,
                        onClick = { onEvent(ApprovalEvent.CancelReject) },
                    )
                }
                Box(Modifier.weight(1f)) {
                    ApprovalAction(
                        label = stringResource(R.string.counts_approval_confirm_reject),
                        tone = MeshaColors.Danger,
                        enabled = !busy && rejectReason.isNotBlank(),
                        onClick = { onEvent(ApprovalEvent.ConfirmReject) },
                    )
                }
            }
        } else {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Box(Modifier.weight(1f)) {
                    ApprovalAction(
                        label = stringResource(R.string.counts_approval_reject),
                        tone = MeshaColors.Danger,
                        enabled = !busy,
                        onClick = { onEvent(ApprovalEvent.OpenReject(row.requestId)) },
                    )
                }
                Box(Modifier.weight(1f)) {
                    ApprovalAction(
                        label = stringResource(R.string.counts_approval_approve),
                        tone = MeshaColors.Brand,
                        enabled = !busy,
                        onClick = { onEvent(ApprovalEvent.Approve(row.requestId)) },
                    )
                }
            }
        }
    }
}

@Composable
private fun ApprovalAction(
    label: String,
    tone: androidx.compose.ui.graphics.Color,
    enabled: Boolean,
    onClick: () -> Unit,
) {
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(if (enabled) tone else MeshaColors.Surf3)
            .clickable(enabled = enabled, onClick = onClick)
            .padding(vertical = 12.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = label,
            color = if (enabled) MeshaColors.OnBrand else MeshaColors.Faint,
            style = MeshaType.listTitle,
        )
    }
}

@Composable
private fun ApprovalBanner(message: String, isError: Boolean) {
    val (fg, bg) = if (isError) MeshaColors.Danger to MeshaColors.DangerX else MeshaColors.Warn to MeshaColors.WarnX
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(bg)
            .padding(horizontal = 12.dp, vertical = 11.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Icon(
            imageVector = if (isError) MeshaIcons.Warn else MeshaIcons.Check,
            contentDescription = null,
            tint = fg,
            modifier = Modifier.size(16.dp),
        )
        // Verbatim: a backend rejection/conflict reason is the server's own copy.
        Text(text = message, color = fg, style = MeshaType.cardSubtitle)
    }
}

@Composable
private fun ApprovalEmptyState(isError: Boolean, onRetry: () -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(14.dp))
            .padding(20.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(
            text = stringResource(
                if (isError) R.string.counts_approval_error else R.string.counts_approval_empty,
            ),
            color = if (isError) MeshaColors.Danger else MeshaColors.Muted,
            style = MeshaType.listTitle,
        )
        // Offered on the EMPTY state too, not just on error. An approver looking at "nothing
        // waiting on you" has no other way to ask whether that is still true, and a queue is
        // exactly the surface where a person needs to re-check on demand.
        ApprovalAction(
            label = stringResource(
                if (isError) R.string.counts_approval_retry else R.string.counts_approval_check_again,
            ),
            tone = MeshaColors.Brand,
            enabled = true,
            onClick = onRetry,
        )
    }
}
