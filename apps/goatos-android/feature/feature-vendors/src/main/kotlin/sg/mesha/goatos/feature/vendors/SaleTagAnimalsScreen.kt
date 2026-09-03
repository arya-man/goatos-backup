package sg.mesha.goatos.feature.vendors

// telemetry:exempt pure stateless renderer; SaleTagAnimalsViewModel (in :app) owns the vendors_*
// AnalyticsEventsVendors + CrashReporter wiring for every read, preview and confirm.

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
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CheckboxDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaDimens
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone

/**
 * Tag animals to a sale (L2 drill from the sale): pick → review → done, the web allocation
 * drawer's three steps. Pick: park, pen, search by RFID or animal id, tick animals. Review: the
 * server's pen groups and the animals it refused. Confirm marks them sold.
 */
@Composable
fun SaleTagAnimalsScreen(
    state: SaleTagAnimalsUiState,
    onEvent: (SaleTagAnimalsEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = TITLE,
            subtitle = state.saleLine.ifBlank { null },
            onBack = { onEvent(if (state.step == SaleTagStep.REVIEW) SaleTagAnimalsEvent.BackToPick else SaleTagAnimalsEvent.Back) },
        )
        VendorsStepper(stepCount = 3, currentIndex = state.step.ordinal, caption = STEP_CAPTIONS[state.step.ordinal])
        when (state.step) {
            SaleTagStep.PICK -> PickStep(state, onEvent)
            SaleTagStep.REVIEW -> ReviewStep(state, onEvent)
            SaleTagStep.DONE -> DoneStep(state, onEvent)
        }
    }
}

@Composable
private fun androidx.compose.foundation.layout.ColumnScope.PickStep(state: SaleTagAnimalsUiState, onEvent: (SaleTagAnimalsEvent) -> Unit) {
    LazyColumn(
        modifier = Modifier.weight(1f),
        contentPadding = PaddingValues(horizontal = MeshaDimens.gutter, vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        state.message?.let { message -> item(key = "message") { VendorsResultBanner(status = VendorsWriteStatus.FAILED, message = message) } }
        item(key = "where") {
            VendorsFormGroup(title = LABEL_WHERE) {
                Text(text = LABEL_PARK, color = MeshaColors.Muted, style = MeshaType.fieldLabel)
                VendorsSegmented(options = state.parks, selectedValue = state.selectedParkId, onSelect = { onEvent(SaleTagAnimalsEvent.SelectPark(it)) })
                VendorsDropdownField(LABEL_PEN, state.selectedPenKey, state.pens, { onEvent(SaleTagAnimalsEvent.SelectPen(it)) }, placeholder = ALL_PENS, allowClear = true, clearLabel = ALL_PENS)
                VendorsSearchField(value = state.search, placeholder = HINT_SEARCH, onValueChange = { onEvent(SaleTagAnimalsEvent.SearchChanged(it)) })
            }
        }
        item(key = "counter") {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(text = "${state.selectedCount} $SELECTED", color = MeshaColors.Ink, style = MeshaType.rowValue, modifier = Modifier.weight(1f))
                if (state.pickLine.isNotBlank()) VendorsChip(label = state.pickLine, tone = if (state.pickLine.startsWith("All")) VendorsTone.OK else VendorsTone.INFO)
            }
        }
        if (state.candidates.isEmpty() && !state.candidatesLoading && state.candidatesEmptyMessage.isNotBlank()) {
            item(key = "empty") { EmptyState(title = state.candidatesEmptyMessage, modifier = Modifier.fillMaxWidth(), icon = MeshaIcons.Goat, tone = EmptyTone.Neutral) }
        }
        items(count = state.candidates.size, key = { state.candidates[it].goatId }) { index ->
            CandidateRow(state.candidates[index]) { onEvent(SaleTagAnimalsEvent.ToggleAnimal(it)) }
        }
        if (state.candidatesLoading) {
            item(key = "loading") {
                Box(Modifier.fillMaxWidth().padding(vertical = 12.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator(color = MeshaColors.BrandD) }
            }
        } else if (state.hasMore) {
            item(key = "more") { VendorsGhostButton(label = LOAD_MORE, onClick = { onEvent(SaleTagAnimalsEvent.LoadMore) }, modifier = Modifier.fillMaxWidth()) }
        }
    }
    VendorsWizardBar(contextLine = "") {
        VendorsPrimaryButton(label = REVIEW, enabled = state.selectedCount > 0 && !state.reviewInFlight, onClick = { onEvent(SaleTagAnimalsEvent.Review) }, modifier = Modifier.weight(1f))
    }
}

@Composable
private fun CandidateRow(candidate: SaleCandidateUi, onToggle: (String) -> Unit) {
    val enabled = candidate.sellable
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(MeshaDimens.radiusCard))
            .background(if (candidate.selected) MeshaColors.OkX else MeshaColors.Surf)
            .clickable(enabled = enabled, role = Role.Checkbox) { onToggle(candidate.goatId) }
            .padding(horizontal = 12.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Checkbox(
            checked = candidate.selected,
            onCheckedChange = if (enabled) ({ onToggle(candidate.goatId) }) else null,
            enabled = enabled,
            colors = CheckboxDefaults.colors(checkedColor = MeshaColors.Brand, uncheckedColor = MeshaColors.Muted),
        )
        Column(Modifier.weight(1f)) {
            Text(text = candidate.tag, color = if (enabled) MeshaColors.Ink else MeshaColors.Muted, style = MeshaType.listTitle, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(text = candidate.detailLine, color = MeshaColors.Muted, style = MeshaType.caption, maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (!enabled && candidate.blockedReason.isNotBlank()) {
                Text(text = candidate.blockedReason, color = MeshaColors.Warn, style = MeshaType.caption, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
        }
        Text(text = candidate.location, color = MeshaColors.Muted, style = MeshaType.caption, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

@Composable
private fun androidx.compose.foundation.layout.ColumnScope.ReviewStep(state: SaleTagAnimalsUiState, onEvent: (SaleTagAnimalsEvent) -> Unit) {
    LazyColumn(
        modifier = Modifier.weight(1f),
        contentPadding = PaddingValues(horizontal = MeshaDimens.gutter, vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        state.message?.let { message -> item(key = "message") { VendorsResultBanner(status = VendorsWriteStatus.FAILED, message = message) } }
        item(key = "line") { Text(text = state.reviewLine, color = MeshaColors.Ink, style = MeshaType.rowValue) }
        item(key = "hint") { Text(text = HINT_REVIEW, color = MeshaColors.Muted, style = MeshaType.caption) }
        items(count = state.reviewGroups.size, key = { "group_${state.reviewGroups[it].location}" }) { index ->
            ReviewGroupCard(state.reviewGroups[index])
        }
        if (state.reviewBlocked.isNotEmpty()) {
            item(key = "blocked_title") { Text(text = CANNOT_SELL, color = MeshaColors.Warn, style = MeshaType.sectionLabel) }
            items(count = state.reviewBlocked.size, key = { "blocked_${state.reviewBlocked[it].goatId}" }) { index ->
                val c = state.reviewBlocked[index]
                Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(MeshaDimens.radiusInput)).background(MeshaColors.WarnX).padding(12.dp)) {
                    Text(text = c.tag, color = MeshaColors.Ink, style = MeshaType.body)
                    Text(text = c.blockedReason, color = MeshaColors.Warn, style = MeshaType.caption)
                }
            }
        }
    }
    VendorsWizardBar(contextLine = "") {
        VendorsGhostButton(label = BACK_TO_PICK, onClick = { onEvent(SaleTagAnimalsEvent.BackToPick) })
        VendorsPrimaryButton(label = CONFIRM, enabled = state.reviewGroups.isNotEmpty() && !state.confirmInFlight, onClick = { onEvent(SaleTagAnimalsEvent.Confirm) }, modifier = Modifier.weight(1f))
    }
}

@Composable
private fun androidx.compose.foundation.layout.ColumnScope.DoneStep(state: SaleTagAnimalsUiState, onEvent: (SaleTagAnimalsEvent) -> Unit) {
    Column(Modifier.weight(1f).padding(MeshaDimens.gutter), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        VendorsResultBanner(status = VendorsWriteStatus.SYNCED, message = state.doneLine)
        Spacer(Modifier.height(4.dp))
        state.reviewGroups.forEach { group -> ReviewGroupCard(group) }
    }
    VendorsWizardBar(contextLine = "") {
        VendorsPrimaryButton(label = DONE, enabled = true, onClick = { onEvent(SaleTagAnimalsEvent.Done) }, modifier = Modifier.weight(1f))
    }
}

@Composable
private fun ReviewGroupCard(group: SaleShedGroupUi) {
    Column(
        modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(MeshaDimens.radiusCard)).background(MeshaColors.Surf).padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(text = group.location, color = MeshaColors.Ink, style = MeshaType.listTitle, modifier = Modifier.weight(1f))
            VendorsChip(label = "${group.animals} ${if (group.animals == 1) "animal" else "animals"}", tone = VendorsTone.OK)
        }
        Text(text = group.tags.ifBlank { "—" }, color = MeshaColors.Muted, style = MeshaType.caption)
    }
}

private const val TITLE = "Tag animals to sale"
private val STEP_CAPTIONS = listOf("Pick the animals", "Review", "Marked sold")
private const val LABEL_WHERE = "Where are they"
private const val LABEL_PARK = "Park"
private const val LABEL_PEN = "Pen"
private const val ALL_PENS = "All pens"
private const val HINT_SEARCH = "RFID or animal ID"
private const val SELECTED = "selected"
private const val LOAD_MORE = "Show more animals"
private const val REVIEW = "Review"
private const val HINT_REVIEW = "These animals will be marked sold and leave the herd. Check the pens and tags before confirming."
private const val CANNOT_SELL = "CANNOT BE SOLD YET"
private const val BACK_TO_PICK = "Back"
private const val CONFIRM = "Confirm and mark sold"
private const val DONE = "Done"
