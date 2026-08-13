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
import sg.mesha.goatos.feature.sheds.CarryVaccine
import sg.mesha.goatos.feature.sheds.DayCarry
import sg.mesha.goatos.feature.sheds.ShedDayTab
import sg.mesha.goatos.feature.sheds.ShedRow
import sg.mesha.goatos.feature.sheds.ShedStatus
import sg.mesha.goatos.feature.sheds.ShedStatusChip
import sg.mesha.goatos.feature.sheds.ShedStatusChipKey
import sg.mesha.goatos.feature.sheds.ShedStatusTone
import sg.mesha.goatos.feature.sheds.ShedsScreen
import sg.mesha.goatos.feature.sheds.ShedsUiState
import sg.mesha.goatos.feature.sheds.VaccineGroup

class ShedsVaccinationCarryScreenshotTest {

    @get:Rule
    val paparazzi = Paparazzi(deviceConfig = DeviceConfig.PIXEL_6)

    @Test
    fun currentTwoVaccinesNoMl() = shot("sheds_vaccines_to_carry_current_two_vaccines_no_ml") {
        ShedsScreen(state = channapatnaState(includeMl = false, perShedBothVaccines = false))
    }

    @Test
    fun proposalCarryShowsMl() = shot("sheds_vaccines_to_carry_proposal_carry_ml") {
        ShedsScreen(state = channapatnaState(includeMl = true, perShedBothVaccines = false))
    }

    @Test
    fun proposalShedRowsShowBothVaccines() = shot("sheds_vaccines_to_carry_proposal_shed_rows_both_vaccines") {
        ShedsScreen(state = channapatnaState(includeMl = true, perShedBothVaccines = true))
    }

    private fun shot(name: String, content: @androidx.compose.runtime.Composable () -> Unit) {
        paparazzi.snapshot(name = name) {
            GoatOsTheme {
                ProvideAppLocale {
                    Box(androidx.compose.ui.Modifier.fillMaxSize().background(MeshaColors.Bg)) {
                        content()
                    }
                }
            }
        }
    }
}

private fun channapatnaState(includeMl: Boolean, perShedBothVaccines: Boolean): ShedsUiState =
    ShedsUiState(
        moduleLabel = "Vaccination",
        scopeLabel = "",
        title = "Vaccination sheds",
        date = "Today · Mon 10 Aug",
        window = "Mon 10 Aug",
        shedCountLabel = "11 assignments",
        dueLabel = "290 doses",
        dayProgressLabel = "0%",
        dayProgressFraction = 0f,
        daySummary = "0 / 290 done",
        shedCount = 11,
        dueCount = 290,
        doneCount = 0,
        lastSyncedAt = System.currentTimeMillis(),
        dayTabs = listOf(
            ShedDayTab("2026-08-09", "SUN", "9", "", isSelected = false),
            ShedDayTab("2026-08-10", "MON", "10", "290", isSelected = true),
            ShedDayTab("2026-08-11", "TUE", "11", "", isSelected = false),
            ShedDayTab("2026-08-12", "WED", "12", "", isSelected = false),
            ShedDayTab("2026-08-13", "THU", "13", "", isSelected = false),
            ShedDayTab("2026-08-14", "FRI", "14", "", isSelected = false),
            ShedDayTab("2026-08-15", "SAT", "15", "", isSelected = false),
        ),
        carry = DayCarry(
            totalRemaining = 290,
            vaccines = listOf(
                CarryVaccine(if (includeMl) "Blue Tongue · 2 ml" else "Blue Tongue", 145),
                CarryVaccine(if (includeMl) "Sheep Pox · 1 ml" else "Sheep Pox", 145),
            ),
        ),
        rows = listOf(
            shedRow("godel-1-1", "Godel 1 - Part 1", "Godel 1", "Part 1", 30, includeMl, perShedBothVaccines),
            shedRow("godel-1-2", "Godel 1 - Part 2", "Godel 1", "Part 2", 30, includeMl, perShedBothVaccines),
            shedRow("godel-1-3", "Godel 1 - Part 3", "Godel 1", "Part 3", 30, includeMl, perShedBothVaccines),
            shedRow("godel-1-4", "Godel 1 - Part 4", "Godel 1", "Part 4", 30, includeMl, perShedBothVaccines),
            shedRow("mandela-2-7", "Mandela 2 - Part 7", "Mandela 2", "Part 7", 13, includeMl, perShedBothVaccines),
        ),
    )

private fun shedRow(
    id: String,
    name: String,
    physicalShed: String,
    partition: String,
    animals: Int,
    includeMl: Boolean,
    bothVaccines: Boolean,
): ShedRow {
    val blueLabel = if (includeMl) "Blue Tongue · 2 ml" else "Blue Tongue"
    val sheepLabel = if (includeMl) "Sheep Pox · 1 ml" else "Sheep Pox"
    return ShedRow(
        id = id,
        name = name,
        animalStage = "Non-pregnant",
        operatorName = "Sagar Mahoor",
        physicalShed = physicalShed,
        partition = partition,
        scheduleDateKey = "2026-08-10",
        scheduleDateLabel = "Mon 10 Aug",
        status = ShedStatus.PENDING,
        statusLabel = "In progress",
        statusChips = listOf(ShedStatusChip(ShedStatusChipKey.IN_PROGRESS, ShedStatusTone.WARN)),
        vaccineGroups = if (bothVaccines) {
            listOf(
                VaccineGroup(blueLabel, "0/$animals"),
                VaccineGroup(sheepLabel, "0/$animals"),
            )
        } else {
            listOf(VaccineGroup(blueLabel, "0/$animals"))
        },
        inShed = animals.toString(),
        due = (animals * if (bothVaccines) 2 else 1).toString(),
        done = "0",
        progressLabel = "0%",
        progressFraction = 0f,
    )
}
