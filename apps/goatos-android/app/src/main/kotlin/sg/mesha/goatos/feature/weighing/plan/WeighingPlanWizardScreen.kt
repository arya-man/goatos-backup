package sg.mesha.goatos.feature.weighing.plan

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Text
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.feature.weighing.component.WeighingSearchField
import sg.mesha.goatos.feature.weighing.component.WeighingStepper
import sg.mesha.goatos.feature.weighing.component.WeighingWizardActionBar
import sg.mesha.goatos.feature.weighing.component.WeighingWizardGhostButton
import sg.mesha.goatos.feature.weighing.component.WeighingWizardPrimaryButton

// telemetry:exempt Task authoring is tracked by the weighing create/publish write, not by the
// steps a planner walks through before committing to it.

private const val INDIVIDUAL_CATEGORY = "individual_animal"
private const val LUMP_SUM_CATEGORY = "per_shed_partition"

/**
 * Authoring one weighing task in five steps: date, park, shed buckets, configure, review.
 *
 * One hosted destination with the step held in the ViewModel, not five routes: the answers belong
 * to one unsaved task, and Back must walk the steps backwards rather than throw the task away.
 */
@Composable
fun WeighingPlanWizardScreen(
    state: WeighingWizardUiState,
    onBack: () -> Unit,
    onSelectDate: (String) -> Unit,
    onSelectPark: (String) -> Unit,
    onBucketQuery: (String) -> Unit,
    onBucketFilter: (WeighingBucketFilter) -> Unit,
    onToggleBucket: (String) -> Unit,
    onAddAllBuckets: () -> Unit,
    onClearBuckets: () -> Unit,
    onLoadMoreBuckets: () -> Unit,
    onConfigQuery: (String) -> Unit,
    onToggleConfigSearch: () -> Unit,
    onLoadMoreConfigRows: () -> Unit,
    onBucketCategory: (String, String) -> Unit,
    onBucketOperator: (String, String) -> Unit,
    onToggleConfigPick: (String) -> Unit,
    onPickAllShown: () -> Unit,
    onClearPicks: () -> Unit,
    onApplyBulk: (String?, String?) -> Unit,
    onSplitEvenly: () -> Unit,
    onContinue: () -> Unit,
    onCommit: (Boolean) -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .fillMaxSize()
            .background(MeshaColors.PageBg),
    ) {
        MeshaScreenHeader(
            title = "New weighing task",
            eyebrow = "WEIGHING",
            eyebrowColor = MeshaColors.BrandD,
            subtitle = stepEyebrow(state.step),
            onBack = onBack,
        )
        WeighingStepper(stepCount = state.stepCount, currentIndex = state.step.ordinal)

        val listState = rememberLazyListState()
        LaunchedEffect(state.step) { listState.scrollToItem(0) }

        LazyColumn(
            state = listState,
            modifier = Modifier
                .weight(1f)
                .fillMaxWidth()
                .padding(horizontal = 16.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item(key = "step-title") { StepTitle(state) }
            state.message?.let { text ->
                item(key = "step-message") { WizardBanner(text = text, fg = MeshaColors.Warn, bg = MeshaColors.WarnX) }
            }
            when (state.step) {
                WeighingWizardStep.DATE -> dateStep(state, onSelectDate)
                WeighingWizardStep.PARK -> parkStep(state, onSelectPark)
                WeighingWizardStep.BUCKETS -> bucketStep(
                    state = state,
                    onBucketQuery = onBucketQuery,
                    onBucketFilter = onBucketFilter,
                    onToggleBucket = onToggleBucket,
                    onAddAllBuckets = onAddAllBuckets,
                    onClearBuckets = onClearBuckets,
                    onLoadMoreBuckets = onLoadMoreBuckets,
                )
                WeighingWizardStep.CONFIGURE -> configureStep(
                    state = state,
                    onConfigQuery = onConfigQuery,
                    onToggleConfigSearch = onToggleConfigSearch,
                    onLoadMoreConfigRows = onLoadMoreConfigRows,
                    onBucketCategory = onBucketCategory,
                    onBucketOperator = onBucketOperator,
                    onToggleConfigPick = onToggleConfigPick,
                    onPickAllShown = onPickAllShown,
                    onClearPicks = onClearPicks,
                    onApplyBulk = onApplyBulk,
                    onSplitEvenly = onSplitEvenly,
                )
                WeighingWizardStep.REVIEW -> reviewStep(state)
            }
            item(key = "tail") { Spacer(Modifier.height(16.dp)) }
        }

        WeighingWizardActionBar(contextLine = state.contextLine) {
            WeighingWizardGhostButton(
                label = if (state.step == WeighingWizardStep.DATE) "Cancel" else "Back",
                enabled = !state.busy,
                onClick = onBack,
                modifier = Modifier.weight(1f),
            )
            if (state.step == WeighingWizardStep.REVIEW) {
                WeighingWizardGhostButton(
                    label = "Save draft",
                    enabled = !state.busy,
                    onClick = { onCommit(false) },
                    modifier = Modifier.weight(1f),
                )
                WeighingWizardPrimaryButton(
                    label = "Publish",
                    enabled = !state.busy,
                    onClick = { onCommit(true) },
                    modifier = Modifier.weight(1f),
                )
            } else {
                WeighingWizardPrimaryButton(
                    label = "Continue",
                    enabled = state.canContinue && !state.busy,
                    onClick = onContinue,
                    modifier = Modifier.weight(1f),
                )
            }
        }
    }
}

private fun stepEyebrow(step: WeighingWizardStep): String = when (step) {
    WeighingWizardStep.DATE -> "Step 1 of 5 · Date"
    WeighingWizardStep.PARK -> "Step 2 of 5 · Park"
    WeighingWizardStep.BUCKETS -> "Step 3 of 5 · Shed buckets"
    WeighingWizardStep.CONFIGURE -> "Step 4 of 5 · Configure"
    WeighingWizardStep.REVIEW -> "Step 5 of 5 · Review"
}

@Composable
private fun StepTitle(state: WeighingWizardUiState) {
    val title = when (state.step) {
        WeighingWizardStep.DATE -> "Select date"
        WeighingWizardStep.PARK -> "Select park"
        WeighingWizardStep.BUCKETS -> "Select shed buckets"
        WeighingWizardStep.CONFIGURE -> "Configure shed buckets"
        WeighingWizardStep.REVIEW -> "Review & publish"
    }
    val subtitle = when (state.step) {
        WeighingWizardStep.DATE -> "Today or a future date."
        WeighingWizardStep.PARK -> if (state.loading) "Loading parks for ${state.dateLabel}…" else state.dateLabel
        WeighingWizardStep.BUCKETS ->
            "${state.allCount} in ${state.parkName} · ${state.dateLabel}"
        WeighingWizardStep.CONFIGURE -> "${state.addedCount} selected"
        WeighingWizardStep.REVIEW -> ""
    }
    Column(
        modifier = Modifier.padding(top = 6.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        Text(text = title, color = MeshaColors.Ink, style = MeshaType.screenTitle)
        if (subtitle.isNotBlank()) {
            Text(text = subtitle, color = MeshaColors.Muted, style = MeshaType.cardSubtitle)
        }
    }
}

// ---- step 1: date ------------------------------------------------------------------------

private fun androidx.compose.foundation.lazy.LazyListScope.dateStep(
    state: WeighingWizardUiState,
    onSelectDate: (String) -> Unit,
) {
    items(state.dateOptions, key = { it.isoDate }) { option ->
        WizardOptionRow(
            title = option.label,
            subtitle = option.note,
            selected = option.selected,
            radio = true,
            onClick = { onSelectDate(option.isoDate) },
        )
    }
}

// ---- step 2: park ------------------------------------------------------------------------

private fun androidx.compose.foundation.lazy.LazyListScope.parkStep(
    state: WeighingWizardUiState,
    onSelectPark: (String) -> Unit,
) {
    if (state.parkOptions.isEmpty()) {
        item(key = "parks-empty") {
            WizardEmptyCard(
                text = if (state.loading) {
                    "Loading parks for ${state.dateLabel}…"
                } else {
                    "No parks are available for ${state.dateLabel}."
                },
            )
        }
        return
    }
    items(state.parkOptions, key = { it.parkId }) { option ->
        WizardOptionRow(
            title = option.name,
            subtitle = option.subtitle,
            selected = option.selected,
            radio = true,
            onClick = { onSelectPark(option.parkId) },
        )
    }
}

// ---- step 3: shed buckets ----------------------------------------------------------------

private fun androidx.compose.foundation.lazy.LazyListScope.bucketStep(
    state: WeighingWizardUiState,
    onBucketQuery: (String) -> Unit,
    onBucketFilter: (WeighingBucketFilter) -> Unit,
    onToggleBucket: (String) -> Unit,
    onAddAllBuckets: () -> Unit,
    onClearBuckets: () -> Unit,
    onLoadMoreBuckets: () -> Unit,
) {
    item(key = "bucket-controls") {
        Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
            WeighingSearchField(
                value = state.bucketQuery,
                placeholder = "Search shed bucket",
                onValueChange = onBucketQuery,
            )
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                WizardChip(
                    label = "Available ${state.availableCount}",
                    selected = state.bucketFilter == WeighingBucketFilter.AVAILABLE,
                    onClick = { onBucketFilter(WeighingBucketFilter.AVAILABLE) },
                )
                WizardChip(
                    label = "Already scheduled ${state.takenCount}",
                    selected = state.bucketFilter == WeighingBucketFilter.TAKEN,
                    onClick = { onBucketFilter(WeighingBucketFilter.TAKEN) },
                )
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                WizardChip(
                    label = "Added ${state.addedCount}",
                    selected = state.bucketFilter == WeighingBucketFilter.ADDED,
                    onClick = { onBucketFilter(WeighingBucketFilter.ADDED) },
                )
                WizardChip(
                    label = "All ${state.allCount}",
                    selected = state.bucketFilter == WeighingBucketFilter.ALL,
                    onClick = { onBucketFilter(WeighingBucketFilter.ALL) },
                )
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                WeighingWizardGhostButton(
                    label = if (state.bucketsAddable > 0) "Add all ${state.bucketsAddable}" else "Nothing to add",
                    enabled = state.bucketsAddable > 0,
                    onClick = onAddAllBuckets,
                    modifier = Modifier.weight(1f),
                )
                WeighingWizardGhostButton(
                    label = "Clear all",
                    enabled = state.addedCount > 0,
                    onClick = onClearBuckets,
                    modifier = Modifier.weight(1f),
                )
            }
            if (state.addedTray.isNotEmpty()) {
                Text(
                    text = "Added: " + state.addedTray.joinToString(" · ") +
                        if (state.addedTrayMore > 0) " +${state.addedTrayMore} more" else "",
                    color = MeshaColors.BrandD,
                    style = MeshaType.cardSubtitle,
                )
            }
            Text(
                text = "${state.bucketShownCount} of ${state.bucketTotalCount}",
                color = MeshaColors.Muted,
                style = MeshaType.sectionLabel,
            )
        }
    }
    if (state.bucketRows.isEmpty()) {
        item(key = "buckets-empty") {
            WizardEmptyCard(
                text = when {
                    state.loading -> "Loading shed buckets…"
                    state.bucketQuery.isNotBlank() -> "Nothing matches “${state.bucketQuery}”."
                    state.bucketFilter == WeighingBucketFilter.ADDED -> "No shed buckets added yet."
                    state.bucketFilter == WeighingBucketFilter.TAKEN ->
                        "Nothing else is scheduled on ${state.dateLabel}."
                    else -> "No available shed buckets left for ${state.dateLabel}."
                },
            )
        }
        return
    }
    itemsIndexed(state.bucketRows, key = { _, row -> row.locationId }) { index, row ->
        LaunchedEffect(row.locationId, index, state.bucketRows.size) {
            if (index >= state.bucketRows.size - 3) onLoadMoreBuckets()
        }
        WizardOptionRow(
            title = row.name,
            subtitle = row.reason,
            selected = row.added,
            radio = false,
            enabled = !row.taken,
            trailing = when {
                row.taken -> "taken" to (MeshaColors.Warn to MeshaColors.WarnX)
                row.added -> "added" to (MeshaColors.Ok to MeshaColors.OkX)
                else -> null
            },
            onClick = { onToggleBucket(row.locationId) },
        )
    }
}

// ---- step 4: configure -------------------------------------------------------------------

private fun androidx.compose.foundation.lazy.LazyListScope.configureStep(
    state: WeighingWizardUiState,
    onConfigQuery: (String) -> Unit,
    onToggleConfigSearch: () -> Unit,
    onLoadMoreConfigRows: () -> Unit,
    onBucketCategory: (String, String) -> Unit,
    onBucketOperator: (String, String) -> Unit,
    onToggleConfigPick: (String) -> Unit,
    onPickAllShown: () -> Unit,
    onClearPicks: () -> Unit,
    onApplyBulk: (String?, String?) -> Unit,
    onSplitEvenly: () -> Unit,
) {
    item(key = "configure-bulk") {
        ConfigureBulkBar(
            state = state,
            onToggleConfigSearch = onToggleConfigSearch,
            onConfigQuery = onConfigQuery,
            onPickAllShown = onPickAllShown,
            onClearPicks = onClearPicks,
            onApplyBulk = onApplyBulk,
            onSplitEvenly = onSplitEvenly,
        )
    }
    item(key = "configure-count") {
        Text(
            text = "${state.configShownCount} of ${state.configTotalCount}" +
                if (state.tickedCount > 0) " · ${state.tickedCount} ticked" else "",
            color = MeshaColors.Muted,
            style = MeshaType.sectionLabel,
        )
    }
    if (state.configRows.isEmpty()) {
        item(key = "configure-empty") {
            WizardEmptyCard(text = "Nothing matches “${state.configQuery}”.")
        }
        return
    }
    itemsIndexed(state.configRows, key = { _, row -> row.locationId }) { index, row ->
        LaunchedEffect(row.locationId, index, state.configRows.size) {
            if (index >= state.configRows.size - 3) onLoadMoreConfigRows()
        }
        ConfigureRow(
            row = row,
            operators = state.operators,
            onTick = { onToggleConfigPick(row.locationId) },
            onCategory = { onBucketCategory(row.locationId, it) },
            onOperator = { onBucketOperator(row.locationId, it) },
        )
    }
}

@Composable
private fun ConfigureBulkBar(
    state: WeighingWizardUiState,
    onToggleConfigSearch: () -> Unit,
    onConfigQuery: (String) -> Unit,
    onPickAllShown: () -> Unit,
    onClearPicks: () -> Unit,
    onApplyBulk: (String?, String?) -> Unit,
    onSplitEvenly: () -> Unit,
) {
    var bulkCategory by remember { mutableStateOf<String?>(null) }
    var bulkOperator by remember { mutableStateOf<String?>(null) }
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(18.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(18.dp))
            .padding(12.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
            WizardSelect(
                label = bulkCategory?.let(::categoryLabel) ?: "Mode…",
                options = listOf(INDIVIDUAL_CATEGORY to "Individual", LUMP_SUM_CATEGORY to "Lump-sum"),
                onSelect = { bulkCategory = it },
                modifier = Modifier.weight(1f),
            )
            WizardSelect(
                label = state.operators.firstOrNull { it.userId == bulkOperator }?.displayName ?: "Operator…",
                options = state.operators.map { it.userId to it.displayName },
                onSelect = { bulkOperator = it },
                modifier = Modifier.weight(1f),
            )
            WeighingWizardGhostButton(
                label = "Apply",
                enabled = bulkCategory != null || bulkOperator != null,
                onClick = { onApplyBulk(bulkCategory, bulkOperator) },
            )
        }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            WizardChip(
                label = if (state.tickedCount > 0) "Untick ${state.tickedCount}" else "Select all ${state.configTotalCount}",
                selected = state.tickedCount > 0,
                onClick = { if (state.tickedCount > 0) onClearPicks() else onPickAllShown() },
            )
            WizardChip(label = "Split evenly", selected = false, onClick = onSplitEvenly)
            WizardChip(label = "Search", selected = state.configSearchOpen, onClick = onToggleConfigSearch)
        }
        if (state.configSearchOpen) {
            WeighingSearchField(
                value = state.configQuery,
                placeholder = "Find a shed bucket",
                onValueChange = onConfigQuery,
            )
        }
        Text(
            text = "Applies to " +
                (if (state.tickedCount > 0) "the ${state.tickedCount} ticked" else "all ${state.configTotalCount} shown") +
                " · " + state.configSummary,
            color = MeshaColors.Muted,
            style = MeshaType.caption,
        )
    }
}

@Composable
private fun ConfigureRow(
    row: WeighingWizardConfigRow,
    operators: List<WeighingWizardOperatorOption>,
    onTick: () -> Unit,
    onCategory: (String) -> Unit,
    onOperator: (String) -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(18.dp))
            .background(MeshaColors.Surf)
            .border(
                1.dp,
                if (row.ticked) MeshaColors.Brand else MeshaColors.Hair,
                RoundedCornerShape(18.dp),
            )
            .padding(12.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .clickable(role = Role.Checkbox, onClick = onTick),
            horizontalArrangement = Arrangement.spacedBy(10.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            WizardTick(checked = row.ticked)
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = row.name,
                    color = MeshaColors.Ink,
                    style = MeshaType.cardTitle,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                if (row.estimateLabel.isNotBlank()) {
                    Text(text = row.estimateLabel, color = MeshaColors.Faint, style = MeshaType.caption)
                }
            }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            WizardSelect(
                label = categoryLabel(row.category),
                options = listOf(INDIVIDUAL_CATEGORY to "Individual", LUMP_SUM_CATEGORY to "Lump-sum"),
                onSelect = onCategory,
                modifier = Modifier.weight(1f),
            )
            WizardSelect(
                label = row.operatorLabel.ifBlank { "Choose operator" },
                options = operators.map { it.userId to it.displayName },
                onSelect = onOperator,
                modifier = Modifier.weight(1f),
            )
        }
    }
}

// ---- step 5: review ----------------------------------------------------------------------

private fun androidx.compose.foundation.lazy.LazyListScope.reviewStep(state: WeighingWizardUiState) {
    item(key = "review-fields") {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(18.dp))
                .background(MeshaColors.Surf)
                .border(1.dp, MeshaColors.Hair, RoundedCornerShape(18.dp))
                .padding(14.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            WizardField(key = "Date", value = state.dateLabel)
            WizardField(key = "Park", value = state.parkName)
            WizardField(key = "Shed buckets", value = state.addedCount.toString())
            WizardField(key = "Operators", value = state.reviewOperatorLabel)
        }
    }
    state.lopsidedOperatorLabel?.let { operator ->
        item(key = "review-lopsided") {
            WizardBanner(
                text = "All ${state.addedCount} shed buckets go to $operator.",
                fg = MeshaColors.Warn,
                bg = MeshaColors.WarnX,
            )
        }
    }
    item(key = "review-heading") {
        Text(text = "Per shed bucket", color = MeshaColors.Muted, style = MeshaType.sectionLabel)
    }
    items(state.reviewRows, key = { it.locationId }) { row ->
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(14.dp))
                .background(MeshaColors.Surf2)
                .padding(horizontal = 12.dp, vertical = 10.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = row.name,
                color = MeshaColors.Ink,
                style = MeshaType.listTitle,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            WizardPill(
                label = row.categoryLabel,
                fg = if (row.categoryLabel == "Individual") MeshaColors.BrandD else MeshaColors.Purple,
                bg = if (row.categoryLabel == "Individual") MeshaColors.Surf3 else MeshaColors.PurpleX,
            )
            Text(text = "→", color = MeshaColors.Faint, style = MeshaType.caption)
            Text(
                text = row.operatorLabel,
                color = MeshaColors.Ink,
                style = MeshaType.cta,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

// ---- shared pieces -----------------------------------------------------------------------

@Composable
private fun WizardOptionRow(
    title: String,
    subtitle: String,
    selected: Boolean,
    radio: Boolean,
    onClick: () -> Unit,
    enabled: Boolean = true,
    trailing: Pair<String, Pair<Color, Color>>? = null,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(16.dp))
            .background(MeshaColors.Surf)
            .border(
                1.dp,
                if (selected) MeshaColors.Brand else MeshaColors.Hair,
                RoundedCornerShape(16.dp),
            )
            .clickable(enabled = enabled, role = if (radio) Role.RadioButton else Role.Checkbox, onClick = onClick)
            .padding(14.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (radio) WizardRadio(selected = selected) else WizardTick(checked = selected)
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = title,
                color = if (enabled) MeshaColors.Ink else MeshaColors.Faint,
                style = MeshaType.cardTitle,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            if (subtitle.isNotBlank()) {
                Text(
                    text = subtitle,
                    color = MeshaColors.Muted,
                    style = MeshaType.cardSubtitle,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
        trailing?.let { (label, tone) ->
            WizardPill(label = label, fg = tone.first, bg = tone.second)
        }
    }
}

@Composable
private fun WizardRadio(selected: Boolean) {
    Box(
        modifier = Modifier
            .size(20.dp)
            .clip(RoundedCornerShape(999.dp))
            .background(if (selected) MeshaColors.Brand else Color.Transparent)
            .border(
                1.dp,
                if (selected) MeshaColors.Brand else MeshaColors.Hair,
                RoundedCornerShape(999.dp),
            ),
    )
}

@Composable
private fun WizardTick(checked: Boolean) {
    Box(
        modifier = Modifier
            .size(20.dp)
            .clip(RoundedCornerShape(6.dp))
            .background(if (checked) MeshaColors.Brand else Color.Transparent)
            .border(
                1.dp,
                if (checked) MeshaColors.Brand else MeshaColors.Hair,
                RoundedCornerShape(6.dp),
            ),
        contentAlignment = Alignment.Center,
    ) {
        if (checked) {
            Text(text = "✓", color = MeshaColors.OnBrand, fontSize = 12.sp, fontWeight = FontWeight.W800)
        }
    }
}

@Composable
private fun WizardChip(label: String, selected: Boolean, onClick: () -> Unit) {
    Text(
        text = label,
        color = if (selected) MeshaColors.OnBrand else MeshaColors.Ink,
        style = MeshaType.pill,
        maxLines = 1,
        textAlign = TextAlign.Center,
        modifier = Modifier
            .minimumInteractiveComponentSize()
            .clip(RoundedCornerShape(999.dp))
            .background(if (selected) MeshaColors.Brand else MeshaColors.Surf2)
            .border(
                1.dp,
                if (selected) MeshaColors.Brand else MeshaColors.Hair,
                RoundedCornerShape(999.dp),
            )
            .clickable(role = Role.Button, onClick = onClick)
            .padding(horizontal = 12.dp, vertical = 8.dp),
    )
}

@Composable
private fun WizardSelect(
    label: String,
    options: List<Pair<String, String>>,
    onSelect: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    var expanded by remember { mutableStateOf(false) }
    Box(modifier = modifier) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(12.dp))
                .background(MeshaColors.Surf2)
                .border(1.dp, MeshaColors.Hair, RoundedCornerShape(12.dp))
                .clickable(enabled = options.isNotEmpty(), role = Role.DropdownList) { expanded = true }
                .padding(horizontal = 10.dp, vertical = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Text(
                text = label,
                color = if (options.isEmpty()) MeshaColors.Faint else MeshaColors.Ink,
                style = MeshaType.cta,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            Text(text = "▾", color = MeshaColors.Muted, style = MeshaType.caption)
        }
        DropdownMenu(expanded = expanded, onDismissRequest = { expanded = false }) {
            options.forEach { (value, text) ->
                DropdownMenuItem(
                    text = { Text(text = text, color = MeshaColors.Ink, style = MeshaType.body) },
                    onClick = {
                        expanded = false
                        onSelect(value)
                    },
                )
            }
        }
    }
}

@Composable
private fun WizardPill(label: String, fg: Color, bg: Color) {
    Text(
        text = label,
        color = fg,
        style = MeshaType.pill,
        maxLines = 1,
        modifier = Modifier
            .clip(RoundedCornerShape(9.dp))
            .background(bg)
            .padding(horizontal = 9.dp, vertical = 5.dp),
    )
}

@Composable
private fun WizardBanner(text: String, fg: Color, bg: Color) {
    Text(
        text = text,
        color = fg,
        style = MeshaType.cardSubtitle,
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(bg)
            .padding(12.dp),
    )
}

@Composable
private fun WizardField(key: String, value: String) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(text = key, color = MeshaColors.Muted, style = MeshaType.fieldLabel, modifier = Modifier.weight(1f))
        Text(
            text = value.ifBlank { "—" },
            color = MeshaColors.Ink,
            style = MeshaType.bodyStrong,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

@Composable
private fun WizardEmptyCard(text: String) {
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(18.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(18.dp))
            .padding(18.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(text = text, color = MeshaColors.Muted, style = MeshaType.cardSubtitle)
    }
}

private fun categoryLabel(category: String): String = when (category) {
    INDIVIDUAL_CATEGORY -> "Individual"
    LUMP_SUM_CATEGORY -> "Lump-sum"
    else -> "Mode"
}
