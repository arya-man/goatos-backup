package sg.mesha.goatos.feature.vendors

// telemetry:exempt pure stateless renderer; SaleTaggingViewModel (in :app) owns the vendors_*
// AnalyticsEventsVendors + CrashReporter wiring for every scan, lookup and submit.

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaDimens
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.LoadingSkeletonList

/**
 * Tagging one sale from the pen (hosted drill under the tag-only queue; maintainer decision
 * 2026-09-11). Scan a tag with the Bluetooth reader or type it, the animal lands in the basket,
 * a weight and a rate are typed against it, and Submit is offered once the sale is filled exactly.
 *
 * The basket is the whole screen: no park or pen picker (the park is the caller's own, clamped on
 * the server) and no ledger, buyer or money anywhere.
 */
@Composable
fun SaleTaggingScreen(
    state: SaleTaggingUiState,
    onEvent: (SaleTaggingEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = TITLE,
            subtitle = state.saleLine.ifBlank { null },
            onBack = { onEvent(SaleTaggingEvent.Back) },
        )
        if (state.isLoading) {
            LoadingSkeletonList(modifier = Modifier.padding(MeshaDimens.gutter))
            return@Column
        }
        if (state.done) {
            DoneBody(state, onEvent)
            return@Column
        }
        LazyColumn(
            modifier = Modifier.weight(1f),
            contentPadding = PaddingValues(horizontal = MeshaDimens.gutter, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            state.message?.let { message -> item(key = "message") { VendorsResultBanner(status = VendorsWriteStatus.FAILED, message = message) } }
            item(key = "progress") {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text(text = state.progressLine, color = MeshaColors.Ink, style = MeshaType.rowValue, modifier = Modifier.weight(1f))
                }
            }
            item(key = "lookup") {
                VendorsFormGroup(title = LABEL_FIND) {
                    VendorsRfidField(
                        value = state.tagInput,
                        onValueChange = { onEvent(SaleTaggingEvent.TagInputChanged(it)) },
                        label = LABEL_TAG,
                        scanning = state.scanning,
                        onToggleScan = { onEvent(SaleTaggingEvent.ToggleScan) },
                        supporting = if (state.scanning) HINT_SCANNING else HINT_TAG,
                    )
                    VendorsGhostButton(
                        label = if (state.lookupInFlight) FINDING else FIND,
                        onClick = { onEvent(SaleTaggingEvent.Lookup) },
                        enabled = state.tagInput.isNotBlank() && !state.lookupInFlight,
                        modifier = Modifier.fillMaxWidth(),
                    )
                    state.lookupMessage?.let { Text(text = it, color = MeshaColors.Warn, style = MeshaType.caption) }
                }
            }
            items(count = state.matches.size, key = { "match_${state.matches[it].goatId}" }) { index ->
                MatchRow(state.matches[index]) { onEvent(SaleTaggingEvent.AddMatch(it)) }
            }
            if (state.basket.isNotEmpty()) {
                item(key = "basket_title") {
                    Text(text = LABEL_BASKET, color = MeshaColors.Ink, style = MeshaType.sectionLabel)
                }
                items(count = state.basket.size, key = { "basket_${state.basket[it].goatId}" }) { index ->
                    BasketRow(state.basket[index], onEvent)
                }
            }
            if (state.alreadyTagged.isNotEmpty()) {
                item(key = "done_title") {
                    Column {
                        Spacer(Modifier.height(4.dp))
                        Text(text = LABEL_ALREADY, color = MeshaColors.Muted, style = MeshaType.sectionLabel)
                    }
                }
                items(count = state.alreadyTagged.size, key = { "done_${it}_${state.alreadyTagged[it].tag}" }) { index ->
                    val a = state.alreadyTagged[index]
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        Column(Modifier.weight(1f)) {
                            Text(text = a.tag, color = MeshaColors.Muted, style = MeshaType.body, maxLines = 1, overflow = TextOverflow.Ellipsis)
                            Text(text = a.location, color = MeshaColors.Muted, style = MeshaType.caption, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        }
                        Text(text = a.figures, color = MeshaColors.Muted, style = MeshaType.caption)
                    }
                }
            }
        }
        VendorsWizardBar(contextLine = state.submitHint) {
            VendorsPrimaryButton(
                label = state.submitLabel,
                enabled = state.canSubmit && !state.submitInFlight,
                onClick = { onEvent(SaleTaggingEvent.Submit) },
                modifier = Modifier.weight(1f),
            )
        }
    }
}

@Composable
private fun MatchRow(match: SaleTaggingMatchUi, onAdd: (String) -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(MeshaDimens.radiusCard))
            .background(MeshaColors.Surf)
            .clickable(enabled = match.sellable, role = Role.Button) { onAdd(match.goatId) }
            .padding(horizontal = 12.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Column(Modifier.weight(1f)) {
            Text(text = match.tag, color = if (match.sellable) MeshaColors.Ink else MeshaColors.Muted, style = MeshaType.listTitle, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(text = match.detailLine, color = MeshaColors.Muted, style = MeshaType.caption, maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (!match.sellable && match.blockedReason.isNotBlank()) {
                Text(text = match.blockedReason, color = MeshaColors.Warn, style = MeshaType.caption, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
        }
        Text(text = match.location, color = MeshaColors.Muted, style = MeshaType.caption, maxLines = 1, overflow = TextOverflow.Ellipsis)
        if (match.sellable) Text(text = ADD, color = MeshaColors.BrandD, style = MeshaType.caption)
    }
}

@Composable
private fun BasketRow(animal: SaleTaggingBasketAnimalUi, onEvent: (SaleTaggingEvent) -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(MeshaDimens.radiusCard))
            .background(if (animal.blockedReason.isNotBlank()) MeshaColors.WarnX else MeshaColors.Surf)
            .padding(12.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Column(Modifier.weight(1f)) {
                Text(text = animal.tag, color = MeshaColors.Ink, style = MeshaType.listTitle, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(text = animal.location, color = MeshaColors.Muted, style = MeshaType.caption, maxLines = 1, overflow = TextOverflow.Ellipsis)
                if (animal.blockedReason.isNotBlank()) {
                    Text(text = animal.blockedReason, color = MeshaColors.Warn, style = MeshaType.caption, maxLines = 2, overflow = TextOverflow.Ellipsis)
                }
            }
            Box(
                modifier = Modifier
                    .size(MeshaDimens.minTapRow)
                    .clip(RoundedCornerShape(MeshaDimens.radiusInput))
                    .clickable(role = Role.Button) { onEvent(SaleTaggingEvent.Remove(animal.goatId)) },
                contentAlignment = Alignment.Center,
            ) {
                Icon(MeshaIcons.Close, contentDescription = REMOVE, tint = MeshaColors.Muted, modifier = Modifier.size(MeshaDimens.iconMd))
            }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            VendorsTextField(
                animal.weight,
                { onEvent(SaleTaggingEvent.WeightChanged(animal.goatId, it)) },
                LABEL_WEIGHT,
                required = true,
                keyboard = KeyboardType.Decimal,
                error = animal.weightError.ifBlank { null },
                modifier = Modifier.weight(1f),
            )
            VendorsTextField(
                animal.rate,
                { onEvent(SaleTaggingEvent.RateChanged(animal.goatId, it)) },
                LABEL_RATE,
                required = true,
                keyboard = KeyboardType.Decimal,
                error = animal.rateError.ifBlank { null },
                modifier = Modifier.weight(1f),
            )
        }
    }
}

@Composable
private fun androidx.compose.foundation.layout.ColumnScope.DoneBody(state: SaleTaggingUiState, onEvent: (SaleTaggingEvent) -> Unit) {
    Column(Modifier.weight(1f).padding(MeshaDimens.gutter), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        VendorsResultBanner(status = VendorsWriteStatus.SYNCED, message = state.doneLine)
        state.alreadyTagged.forEach { a ->
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Column(Modifier.weight(1f)) {
                    Text(text = a.tag, color = MeshaColors.Ink, style = MeshaType.body, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    Text(text = a.location, color = MeshaColors.Muted, style = MeshaType.caption, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
                Text(text = a.figures, color = MeshaColors.Muted, style = MeshaType.caption)
            }
        }
    }
    VendorsWizardBar(contextLine = "") {
        VendorsPrimaryButton(label = DONE, enabled = true, onClick = { onEvent(SaleTaggingEvent.Done) }, modifier = Modifier.weight(1f))
    }
}

private const val TITLE = "Tag animals"
private const val LABEL_FIND = "Find the animal"
private const val LABEL_TAG = "RFID or tag number"
private const val HINT_TAG = "Tap the reader icon to scan, or type the number"
private const val HINT_SCANNING = "Reader on — scan the animal's tag"
private const val FIND = "Find animal"
private const val FINDING = "Finding…"
private const val ADD = "Add"
private const val LABEL_BASKET = "TAGGED IN THIS SESSION"
private const val LABEL_ALREADY = "ALREADY TAGGED"
private const val LABEL_WEIGHT = "Weight (kg)"
private const val LABEL_RATE = "Rate (₹)"
private const val REMOVE = "Remove"
private const val DONE = "Done"
