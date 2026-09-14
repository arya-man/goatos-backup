package sg.mesha.goatos.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import app.cash.paparazzi.DeviceConfig
import app.cash.paparazzi.Paparazzi
import org.junit.Rule
import org.junit.Test
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.locale.ProvideAppLocale
import sg.mesha.goatos.feature.vendors.MarketCityCardUi
import sg.mesha.goatos.feature.vendors.MarketCityEntryScreen
import sg.mesha.goatos.feature.vendors.MarketCityEntryUiState
import sg.mesha.goatos.feature.vendors.MarketQuestionFieldUi
import sg.mesha.goatos.feature.vendors.MarketSurveyScreen
import sg.mesha.goatos.feature.vendors.MarketSurveyUiState
import sg.mesha.goatos.feature.vendors.VendorsTone
import sg.mesha.goatos.feature.vendors.VendorsWriteStatus

/**
 * Market survey phone screens (maintainer decision 2026-09-14): the day's city cards, the closed
 * state before the configured call time, and one city's entry form. Every label on them is
 * backend config rendered verbatim, so the fixtures carry the six seeded questions.
 */
class MarketSurveyScreenshotTest {
    @get:Rule
    val paparazzi = Paparazzi(deviceConfig = DeviceConfig.PIXEL_2.copy(screenWidth = 720, screenHeight = 1280))

    @Composable
    private fun frame(content: @Composable () -> Unit) {
        GoatOsTheme {
            ProvideAppLocale {
                Box(androidx.compose.ui.Modifier.fillMaxSize().background(MeshaColors.Bg)) { content() }
            }
        }
    }

    @Test
    fun dayCards() {
        paparazzi.snapshot(name = "market_day_cards") {
            frame {
                MarketSurveyScreen(
                    state = MarketSurveyUiState(
                        title = "Market",
                        dateLine = "14-09-2026",
                        summaryLine = "1 of 3 cities done",
                        canRecord = true,
                        lastSyncedAt = System.currentTimeMillis(),
                        cards = listOf(
                            MarketCityCardUi("c1", "Chennai", "done", "Done", VendorsTone.OK, "6 of 6 answered"),
                            MarketCityCardUi("c2", "Salem", "pending", "Pending", VendorsTone.WARN, "4 of 6 answered"),
                            MarketCityCardUi("c3", "Madurai", "pending", "Pending", VendorsTone.WARN, "0 of 6 answered"),
                        ),
                    ),
                )
            }
        }
    }

    @Test
    fun beforeCallTime() {
        paparazzi.snapshot(name = "market_before_call_time") {
            frame {
                MarketSurveyScreen(
                    state = MarketSurveyUiState(
                        title = "Market",
                        dateLine = "14-09-2026",
                        summaryLine = "3 cities to call",
                        canRecord = true,
                        emptyMessage = "Today's market calls open at 08:00",
                    ),
                )
            }
        }
    }

    @Test
    fun cityEntryForm() {
        paparazzi.snapshot(name = "market_city_entry") {
            frame {
                MarketCityEntryScreen(
                    state = MarketCityEntryUiState(
                        cityName = "Salem",
                        dateLine = "14-09-2026",
                        canRecord = true,
                        fields = listOf(
                            MarketQuestionFieldUi("q1", "Goat live price", "₹/kg", "620"),
                            MarketQuestionFieldUi("q2", "Sheep live price", "₹/kg", "560"),
                            MarketQuestionFieldUi("q3", "Goat carcass price", "₹/kg", "900"),
                            MarketQuestionFieldUi("q4", "Sheep carcass price", "₹/kg", "820"),
                            MarketQuestionFieldUi("q5", "Goat offals price", "₹/kg", ""),
                            MarketQuestionFieldUi("q6", "Sheep offals price", "₹/kg", ""),
                        ),
                        writeStatus = VendorsWriteStatus.IDLE,
                    ),
                )
            }
        }
    }
}
