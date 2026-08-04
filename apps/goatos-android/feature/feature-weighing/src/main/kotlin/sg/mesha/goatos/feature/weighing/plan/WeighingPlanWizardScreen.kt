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
import androidx.compose.ui.res.stringResource
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.feature.weighing.component.WeighingSearchField
import sg.mesha.goatos.feature.weighing.component.WeighingStepper
import sg.mesha.goatos.feature.weighing.component.WeighingWizardActionBar
import sg.mesha.goatos.feature.weighing.component.WeighingWizardGhostButton
import sg.mesha.goatos.feature.weighing.component.WeighingWizardPrimaryButton
import sg.mesha.goatos.feature.weighing.R

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
            title = stringResource(R.string.weighing_wizard_title),
            eyebrow = stringResource(R.string.weighing_eyebrow),
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
            state.repeatSourceLabel?.let { label ->
                item(key = "repeat-source") {
                    WizardBanner(text = label, fg = MeshaColors.BrandD, bg = MeshaColors.Surf3)
                }
            }
            if (state.repeatDroppedCount > 0) {
                item(key = "repeat-dropped") {
                    WizardBanner(
                        text = if (state.repeatDroppedCount == 1) {
                            stringResource(
                                R.string.weighing_wizard_dropped_one,
                                state.repeatDroppedCount,
                                state.dateLabel,
                            )
                        } else {
                            stringResource(
                                R.string.weighing_wizard_dropped_other,
                                state.repeatDroppedCount,
                                state.dateLabel,
                            )
                        },
                        fg = MeshaColors.Warn,
                        bg = MeshaColors.WarnX,
                    )
                }
            }
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
                label = if (state.step == WeighingWizardStep.DATE) {
                    stringResource(R.string.weighing_wizard_cancel)
                } else {
                    stringResource(R.string.weighing_wizard_back)
                },
                enabled = !state.busy,
                onClick = onBack,
                modifier = Modifier.weight(1f),
            )
            if (state.step == WeighingWizardStep.REVIEW) {
                WeighingWizardGhostButton(
                    label = stringResource(R.string.weighing_wizard_save_draft),
                    enabled = !state.busy,
                    onClick = { onCommit(false) },
                    modifier = Modifier.weight(1f),
                )
                WeighingWizardPrimaryButton(
                    label = stringResource(R.string.weighing_wizard_publish),
                    enabled = !state.busy,
                    onClick = { onCommit(true) },
                    modifier = Modifier.weight(1f),
                )
            } else {
                WeighingWizardPrimaryButton(
                    label = stringResource(R.string.weighing_wizard_continue),
                    enabled = state.canContinue && !state.busy,
                    onClick = onContinue,
                    modifier = Modifier.weight(1f),
                )
            }
        }
    }
}

@Composable
private fun stepEyebrow(step: WeighingWizardStep): String = when (step) {
    WeighingWizardStep.DATE -> stringResource(R.string.weighing_wizard_step_date)
    WeighingWizardStep.PARK -> stringResource(R.string.weighing_wizard_step_park)
    WeighingWizardStep.BUCKETS -> stringResource(R.string.weighing_wizard_step_buckets)
    WeighingWizardStep.CONFIGURE -> stringResource(R.string.weighing_wizard_step_configure)
    WeighingWizardStep.REVIEW -> stringResource(R.string.weighing_wizard_step_review)
}

@Composable
private fun StepTitle(state: WeighingWizardUiState) {
    val title = when (state.step) {
        WeighingWizardStep.DATE -> stringResource(R.string.weighing_wizard_title_date)
        WeighingWizardStep.PARK -> stringResource(R.string.weighing_wizard_title_park)
        WeighingWizardStep.BUCKETS -> stringResource(R.string.weighing_wizard_title_buckets)
        WeighingWizardStep.CONFIGURE -> stringResource(R.string.weighing_wizard_title_configure)
        WeighingWizardStep.REVIEW -> stringResource(R.string.weighing_wizard_title_review)
    }
    val subtitle = when (state.step) {
        WeighingWizardStep.DATE -> stringResource(R.string.weighing_wizard_sub_date)
        WeighingWizardStep.PARK -> if (state.loading) {
            stringResource(R.string.weighing_wizard_loading_parks_fmt, state.dateLabel)
        } else {
            state.dateLabel
        }
        WeighingWizardStep.BUCKETS ->
            stringResource(
                R.string.weighing_wizard_sub_buckets_fmt,
                state.allCount,
                state.parkName,
                state.dateLabel,
            )
        WeighingWizardStep.CONFIGURE ->
            stringResource(R.string.weighing_wizard_sub_configure_fmt, state.addedCount)
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
                    stringResource(R.string.weighing_wizard_loading_parks_fmt, state.dateLabel)
                } else {
                    stringResource(R.string.weighing_wizard_no_parks_fmt, state.dateLabel)
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
                placeholder = stringResource(R.string.weighing_wizard_search_bucket),
                onValueChange = onBucketQuery,
            )
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                WizardChip(
                    label = stringResource(R.string.weighing_wizard_filter_available_fmt, state.availableCount),
                    selected = state.bucketFilter == WeighingBucketFilter.AVAILABLE,
                    onClick = { onBucketFilter(WeighingBucketFilter.AVAILABLE) },
                )
                WizardChip(
                    label = stringResource(R.string.weighing_wizard_filter_taken_fmt, state.takenCount),
                    selected = state.bucketFilter == WeighingBucketFilter.TAKEN,
                    onClick = { onBucketFilter(WeighingBucketFilter.TAKEN) },
                )
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                WizardChip(
                    label = stringResource(R.string.weighing_wizard_filter_added_fmt, state.addedCount),
                    selected = state.bucketFilter == WeighingBucketFilter.ADDED,
                    onClick = { onBucketFilter(WeighingBucketFilter.ADDED) },
                )
                WizardChip(
                    label = stringResource(R.string.weighing_wizard_filter_all_fmt, state.allCount),
                    selected = state.bucketFilter == WeighingBucketFilter.ALL,
                    onClick = { onBucketFilter(WeighingBucketFilter.ALL) },
                )
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                WeighingWizardGhostButton(
                    // "Add all N" once something is already added collides with the "Added N"
                    // chip beside it: the same number, meaning opposite things (2 already in vs
                    // 2 still out). Once the tray is non-empty the button says REMAINING.
                    label = when {
                        state.bucketsAddable == 0 -> stringResource(R.string.weighing_wizard_nothing_to_add)
                        state.addedCount > 0 ->
                            stringResource(R.string.weighing_wizard_add_remaining_fmt, state.bucketsAddable)
                        else -> stringResource(R.string.weighing_wizard_add_all_fmt, state.bucketsAddable)
                    },
                    enabled = state.bucketsAddable > 0,
                    onClick = onAddAllBuckets,
                    modifier = Modifier.weight(1f),
                )
                WeighingWizardGhostButton(
                    label = stringResource(R.string.weighing_wizard_clear_all),
                    enabled = state.addedCount > 0,
                    onClick = onClearBuckets,
                    modifier = Modifier.weight(1f),
                )
            }
            if (state.addedTray.isNotEmpty()) {
                Text(
                    text = stringResource(
                        R.string.weighing_wizard_added_tray_fmt,
                        state.addedTray.joinToString(" · "),
                    ) + if (state.addedTrayMore > 0) {
                        stringResource(R.string.weighing_wizard_tray_more_fmt, state.addedTrayMore)
                    } else {
                        ""
                    },
                    color = MeshaColors.BrandD,
                    style = MeshaType.cardSubtitle,
                )
            }
            Text(
                text = stringResource(
                    R.string.weighing_wizard_shown_of_fmt,
                    state.bucketShownCount,
                    state.bucketTotalCount,
                ),
                color = MeshaColors.Muted,
                style = MeshaType.sectionLabel,
            )
        }
    }
    if (state.bucketRows.isEmpty()) {
        item(key = "buckets-empty") {
            WizardEmptyCard(
                text = when {
                    state.loading -> stringResource(R.string.weighing_wizard_loading_buckets)
                    state.bucketQuery.isNotBlank() ->
                        stringResource(R.string.weighing_wizard_no_match_fmt, state.bucketQuery)
                    state.bucketFilter == WeighingBucketFilter.ADDED ->
                        stringResource(R.string.weighing_wizard_no_buckets_added)
                    state.bucketFilter == WeighingBucketFilter.TAKEN ->
                        stringResource(R.string.weighing_wizard_nothing_else_scheduled_fmt, state.dateLabel)
                    else ->
                        stringResource(R.string.weighing_wizard_no_available_buckets_fmt, state.dateLabel)
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
                row.taken ->
                    stringResource(R.string.weighing_wizard_state_taken) to
                        (MeshaColors.Warn to MeshaColors.WarnX)
                row.added ->
                    stringResource(R.string.weighing_wizard_state_added) to
                        (MeshaColors.Ok to MeshaColors.OkX)
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
            text = stringResource(
                R.string.weighing_wizard_shown_of_fmt,
                state.configShownCount,
                state.configTotalCount,
            ) + if (state.tickedCount > 0) {
                stringResource(R.string.weighing_wizard_ticked_suffix_fmt, state.tickedCount)
            } else {
                ""
            },
            color = MeshaColors.Muted,
            style = MeshaType.sectionLabel,
        )
    }
    if (state.configRows.isEmpty()) {
        item(key = "configure-empty") {
            WizardEmptyCard(text = stringResource(R.string.weighing_wizard_no_match_fmt, state.configQuery))
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
                label = bulkCategory?.let { categoryLabel(it) }
                    ?: stringResource(R.string.weighing_wizard_mode_prompt),
                options = categoryOptions(),
                onSelect = { bulkCategory = it },
                modifier = Modifier.weight(1f),
            )
            WizardSelect(
                label = state.operators.firstOrNull { it.userId == bulkOperator }?.displayName ?: stringResource(R.string.weighing_wizard_operator_prompt),
                options = state.operators.map { it.userId to it.displayName },
                onSelect = { bulkOperator = it },
                modifier = Modifier.weight(1f),
            )
            WeighingWizardGhostButton(
                label = stringResource(R.string.weighing_wizard_apply),
                enabled = bulkCategory != null || bulkOperator != null,
                onClick = { onApplyBulk(bulkCategory, bulkOperator) },
            )
        }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            WizardChip(
                label = if (state.tickedCount > 0) {
                    stringResource(R.string.weighing_wizard_untick_fmt, state.tickedCount)
                } else {
                    stringResource(R.string.weighing_wizard_select_all_fmt, state.configTotalCount)
                },
                selected = state.tickedCount > 0,
                onClick = { if (state.tickedCount > 0) onClearPicks() else onPickAllShown() },
            )
            WizardChip(
                label = stringResource(R.string.weighing_wizard_split_evenly),
                selected = false,
                onClick = onSplitEvenly,
            )
            WizardChip(
                label = stringResource(R.string.weighing_wizard_search),
                selected = state.configSearchOpen,
                onClick = onToggleConfigSearch,
            )
        }
        if (state.configSearchOpen) {
            WeighingSearchField(
                value = state.configQuery,
                placeholder = stringResource(R.string.weighing_wizard_find_bucket),
                onValueChange = onConfigQuery,
            )
        }
        Text(
            text = stringResource(
                R.string.weighing_wizard_applies_to_fmt,
                if (state.tickedCount > 0) {
                    stringResource(R.string.weighing_wizard_applies_to_ticked_fmt, state.tickedCount)
                } else {
                    stringResource(R.string.weighing_wizard_applies_to_all_fmt, state.configTotalCount)
                },
                // Operator loads name their UNIT: "Dinakar · 2 sheds", never "Dinakar 2".
                // The same screen shows shed counts and animal counts, so a bare number
                // beside a name is genuinely ambiguous to the reader.
                (
                    listOf(
                        stringResource(
                            R.string.weighing_wizard_summary_modes_fmt,
                            state.configIndividualCount,
                            state.configLumpSumCount,
                        ),
                    ) + state.configPerOperator.map { load ->
                        stringResource(
                            R.string.weighing_wizard_summary_operator_fmt,
                            load.displayName,
                            wizardShedNoun(load.shedCount),
                        )
                    }
                    ).joinToString(" · "),
            ),
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
                options = categoryOptions(),
                onSelect = onCategory,
                modifier = Modifier.weight(1f),
            )
            WizardSelect(
                label = row.operatorLabel.ifBlank { stringResource(R.string.weighing_wizard_choose_operator) },
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
            WizardField(key = stringResource(R.string.weighing_wizard_field_date), value = state.dateLabel)
            WizardField(key = stringResource(R.string.weighing_wizard_field_park), value = state.parkName)
            WizardField(
                key = stringResource(R.string.weighing_wizard_field_buckets),
                value = state.addedCount.toString(),
            )
            WizardField(
                key = stringResource(R.string.weighing_wizard_field_operators),
                value = state.reviewOperatorLabel,
            )
        }
    }
    state.lopsidedOperatorLabel?.let { operator ->
        item(key = "review-lopsided") {
            WizardBanner(
                text = stringResource(R.string.weighing_wizard_lopsided_fmt, state.addedCount, operator),
                fg = MeshaColors.Warn,
                bg = MeshaColors.WarnX,
            )
        }
    }
    item(key = "review-heading") {
        Text(
            text = stringResource(R.string.weighing_wizard_per_bucket),
            color = MeshaColors.Muted,
            style = MeshaType.sectionLabel,
        )
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
            text = value.ifBlank { stringResource(R.string.weighing_wizard_field_empty) },
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

@Composable
private fun categoryLabel(category: String): String = when (category) {
    INDIVIDUAL_CATEGORY -> stringResource(R.string.weighing_category_individual_title)
    LUMP_SUM_CATEGORY -> stringResource(R.string.weighing_category_lump_sum_title)
    else -> stringResource(R.string.weighing_wizard_mode_fallback)
}

/** The two weighing modes, as the planner picks them. */
@Composable
private fun categoryOptions(): List<Pair<String, String>> = listOf(
    INDIVIDUAL_CATEGORY to stringResource(R.string.weighing_category_individual_title),
    LUMP_SUM_CATEGORY to stringResource(R.string.weighing_category_lump_sum_title),
)

/** "1 shed" / "N sheds" in the reader's language — the unit for an operator's bucket load. */
@Composable
private fun wizardShedNoun(count: Int): String = if (count == 1) {
    stringResource(R.string.weighing_shed_one, count)
} else {
    stringResource(R.string.weighing_shed_other, count)
}
