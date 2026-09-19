package sg.mesha.goatos.feature.vendors

// telemetry:exempt pure stateless renderer; MarketCityEntryViewModel (in :app) owns the market_*
// AnalyticsEventsMarket + CrashReporter wiring for the queued write.

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaDimens
import sg.mesha.goatos.core.designsystem.theme.MeshaType

/**
 * One city's morning prices (L1 drill under the Market tab, maintainer decision 2026-09-14):
 * every configured question as a decimal field labelled with its backend-owned unit, prefilled
 * with whatever was already recorded today, and ONE save. A field left blank is simply not
 * sent -- a card may be answered in two sittings -- so only the figures typed are written.
 */
@Composable
fun MarketCityEntryScreen(
    state: MarketCityEntryUiState,
    onEvent: (MarketCityEntryEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    val locked = state.writeStatus == VendorsWriteStatus.QUEUED || state.writeStatus == VendorsWriteStatus.SYNCED
    LaunchedEffect(state.closeAfterSave) {
        if (state.closeAfterSave) {
            delay(CLOSE_AFTER_SAVE_MS)
            onEvent(MarketCityEntryEvent.Back)
        }
    }
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg).imePadding()) {
        MeshaScreenHeader(title = state.cityName.ifBlank { TITLE }, subtitle = state.dateLine.ifBlank { null }, onBack = { onEvent(MarketCityEntryEvent.Back) })
        LazyColumn(
            modifier = Modifier.weight(1f),
            contentPadding = PaddingValues(horizontal = MeshaDimens.gutter, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item(key = "result") { VendorsResultBanner(status = state.writeStatus, message = state.writeMessage) }
            if (state.missing) {
                item(key = "missing") { Text(MISSING, color = MeshaColors.Muted, style = MeshaType.body) }
            }
            if (!state.canRecord && !state.missing) {
                item(key = "read_only") { Text(READ_ONLY, color = MeshaColors.Muted, style = MeshaType.caption) }
            }
            item(key = "fields") {
                VendorsFormGroup(title = GROUP_TITLE) {
                    state.fields.forEach { field ->
                        VendorsTextField(
                            value = field.value,
                            onValueChange = { onEvent(MarketCityEntryEvent.PriceChanged(field.questionId, it)) },
                            label = field.label,
                            keyboard = KeyboardType.Decimal,
                            error = field.error,
                            supporting = field.unitLabel,
                            readOnly = locked || !state.canRecord,
                        )
                    }
                }
            }
        }
        VendorsWizardBar(contextLine = state.fields.count { it.value.isNotBlank() }.let { "$it of ${state.fields.size} $ENTERED" }) {
            if (locked) {
                val saved = state.writeStatus == VendorsWriteStatus.SYNCED
                VendorsPrimaryButton(label = if (saved) SAVED else SAVING, enabled = saved, onClick = {}, modifier = Modifier.weight(1f))
            } else {
                VendorsPrimaryButton(
                    label = SAVE,
                    enabled = !state.submitInFlight && state.canRecord && !state.missing,
                    onClick = { onEvent(MarketCityEntryEvent.Save) },
                    modifier = Modifier.weight(1f),
                )
            }
        }
    }
}

private const val TITLE = "Market prices"
private const val GROUP_TITLE = "Today's prices"
private const val SAVE = "Save prices"
private const val SAVING = "Saving…"
private const val SAVED = "Saved"
private const val ENTERED = "entered"
private const val MISSING = "This city is not on today's call list any more. Go back and refresh."
private const val READ_ONLY = "You can see today's prices here; recording them is the market reporter's job."
private const val CLOSE_AFTER_SAVE_MS = 1_500L
