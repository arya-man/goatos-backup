package sg.mesha.goatos.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import app.cash.paparazzi.DeviceConfig
import app.cash.paparazzi.Paparazzi
import org.junit.Rule
import org.junit.Test
import sg.mesha.goatos.core.designsystem.locale.ProvideAppLocale
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.feature.pccare.PcCareInventoryRequirementUi
import sg.mesha.goatos.feature.pccare.PcCareSlotChipUi
import sg.mesha.goatos.feature.pccare.PcCareSlotState
import sg.mesha.goatos.feature.pccare.PcCareTaskScreen
import sg.mesha.goatos.feature.pccare.PcCareTaskUiState

class PcCareInventoryTaskScreenshotTest {
    @get:Rule
    val paparazzi = Paparazzi(deviceConfig = DeviceConfig.PIXEL_2.copy(screenWidth = 720, screenHeight = 1280))

    @Test
    fun inventoryVaccineTaskLongRequirements() {
        paparazzi.snapshot(name = "pc_care_inventory_vaccine_long_requirements") {
            GoatOsTheme {
                ProvideAppLocale {
                    Box(androidx.compose.ui.Modifier.fillMaxSize().background(MeshaColors.Bg)) {
                        PcCareTaskScreen(
                            state = PcCareTaskUiState(
                                title = "Vaccine stock",
                                locationDisplay = "Mandela 1 - Part 7",
                                parkLabel = "CPT",
                                dateLabel = "2026-08-26",
                                assigneeLine = "Assigned to Chandrakant",
                                inventoryRequirements = listOf(
                                    PcCareInventoryRequirementUi("ET+TT", "10 doses"),
                                    PcCareInventoryRequirementUi("PPR", "25 doses"),
                                    PcCareInventoryRequirementUi(
                                        vaccineLabel = "Enterotoxaemia + tetanus booster reserve stock",
                                        requiredDosesLabel = "125 doses",
                                    ),
                                ),
                                taskProofSlot = PcCareSlotChipUi(
                                    fieldKey = "stock_fridge_video",
                                    label = "Fridge stock proof",
                                    state = PcCareSlotState.EMPTY,
                                    statusLabel = "Not recorded",
                                    canRecord = true,
                                    description = "Take a photo or video of the vaccine stock inside the fridge.",
                                ),
                                submitEnabled = false,
                                submitBlockedReason = "Record the fridge stock proof first",
                            ),
                        )
                    }
                }
            }
        }
    }
}
