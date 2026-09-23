package sg.mesha.goatos.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.paging.PagingData
import androidx.paging.compose.collectAsLazyPagingItems
import app.cash.paparazzi.DeviceConfig
import app.cash.paparazzi.Paparazzi
import kotlinx.coroutines.flow.flowOf
import org.junit.Rule
import org.junit.Test
import sg.mesha.goatos.core.designsystem.locale.ProvideAppLocale
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.feature.counts.PenReconciliationParkUi
import sg.mesha.goatos.feature.counts.PenReconciliationRowUi
import sg.mesha.goatos.feature.counts.PenReconciliationScreen
import sg.mesha.goatos.feature.counts.PenReconciliationStatusUi
import sg.mesha.goatos.feature.counts.PenReconciliationTone
import sg.mesha.goatos.feature.counts.PenReconciliationUiState

/**
 * Reconcile tab park bar + per-card park badge (maintainer request 2026-09-15): a CXO sees
 * "All parks" plus one chip per park and a CBE/CPT badge on every card; a one-park operator
 * sees only their park, already selected.
 */
class PenReconciliationParkScreenshotTest {
    @get:Rule
    val paparazzi = Paparazzi(deviceConfig = DeviceConfig.PIXEL_2.copy(screenWidth = 720, screenHeight = 1280))

    private val cbe = PenReconciliationParkUi(parkId = "park-cbe", label = "Coimbatore", code = "CBE", selected = false)
    private val cpt = PenReconciliationParkUi(parkId = "park-cpt", label = "Channapatna", code = "CPT", selected = false)

    private fun statuses(all: Int, open: Int) = listOf(
        PenReconciliationStatusUi("all", "All", true, all),
        PenReconciliationStatusUi("open", "Open", false, open),
        PenReconciliationStatusUi("pending_verification", "In review", false, 0),
        PenReconciliationStatusUi("rework", "Rework", false, 0),
        PenReconciliationStatusUi("completed", "Completed", false, 0),
    )

    private fun row(id: String, tag: String, found: String, belongs: String, park: String) = PenReconciliationRowUi(
        cardId = id,
        scannedIdentifier = tag,
        goatDisplayId = "G-$id",
        foundLabel = found,
        belongsLabel = belongs,
        statusLabel = "Ready to return",
        statusTone = PenReconciliationTone.Action,
        primaryActionKey = "execute",
        raisedAtLabel = "22/09/2026",
        parkCode = park,
    )

    @Composable
    private fun frame(state: PenReconciliationUiState, rows: List<PenReconciliationRowUi>) {
        GoatOsTheme {
            ProvideAppLocale {
                val items = flowOf(PagingData.from(rows)).collectAsLazyPagingItems()
                Box(androidx.compose.ui.Modifier.fillMaxSize().background(MeshaColors.Bg)) {
                    PenReconciliationScreen(state = state, rows = items)
                }
            }
        }
    }

    @Test
    fun ceoAllParks() {
        paparazzi.snapshot(name = "reconcile_ceo_all_parks") {
            frame(
                PenReconciliationUiState(
                    parks = listOf(PenReconciliationParkUi("", "All parks", "", true), cbe, cpt),
                    statuses = statuses(all = 74, open = 74),
                    lastSyncedAt = System.currentTimeMillis(),
                ),
                listOf(
                    row("1041", "940 000 123 456 781", "Castro 2", "Castro 1", "CBE"),
                    row("2207", "940 000 123 456 902", "Mandela 1 - Part 3", "Mandela 1 - Part 10", "CPT"),
                    row("1188", "940 000 123 457 015", "Yashoda 5", "Godel 2 - Part 1", "CBE"),
                ),
            )
        }
    }

    @Test
    fun ceoPickedChannapatna() {
        paparazzi.snapshot(name = "reconcile_ceo_picked_cpt") {
            frame(
                PenReconciliationUiState(
                    parks = listOf(PenReconciliationParkUi("", "All parks", "", false), cbe, cpt.copy(selected = true)),
                    statuses = statuses(all = 49, open = 49),
                    lastSyncedAt = System.currentTimeMillis(),
                ),
                listOf(
                    row("2207", "940 000 123 456 902", "Mandela 1 - Part 3", "Mandela 1 - Part 10", "CPT"),
                    row("2231", "940 000 123 458 330", "Castro 2", "Castro 1", "CPT"),
                ),
            )
        }
    }

    @Test
    fun oneParkOperator() {
        paparazzi.snapshot(name = "reconcile_one_park_operator") {
            frame(
                PenReconciliationUiState(
                    parks = listOf(cbe.copy(selected = true)),
                    statuses = statuses(all = 25, open = 25),
                    lastSyncedAt = System.currentTimeMillis(),
                ),
                listOf(
                    row("1041", "940 000 123 456 781", "Castro 2", "Castro 1", "CBE"),
                    row("1188", "940 000 123 457 015", "Yashoda 5", "Godel 2 - Part 1", "CBE"),
                ),
            )
        }
    }
}
