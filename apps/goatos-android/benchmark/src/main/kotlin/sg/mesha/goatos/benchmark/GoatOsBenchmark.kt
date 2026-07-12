package sg.mesha.goatos.benchmark

import androidx.benchmark.macro.CompilationMode
import androidx.benchmark.macro.FrameTimingMetric
import androidx.benchmark.macro.MacrobenchmarkScope
import androidx.benchmark.macro.StartupMode
import androidx.benchmark.macro.StartupTimingMetric
import androidx.benchmark.macro.junit4.BaselineProfileRule
import androidx.benchmark.macro.junit4.MacrobenchmarkRule
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.filters.LargeTest
import androidx.test.uiautomator.By
import androidx.test.uiautomator.Until
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

private const val TARGET_PACKAGE = "sg.mesha.goatos.dev"
private const val UI_TIMEOUT_MS = 20_000L

@LargeTest
@RunWith(AndroidJUnit4::class)
class GoatOsMacrobenchmark {
    @get:Rule
    val benchmarkRule = MacrobenchmarkRule()

    @Test
    fun coldStartup() = benchmarkRule.measureRepeated(
        packageName = TARGET_PACKAGE,
        metrics = listOf(StartupTimingMetric()),
        compilationMode = CompilationMode.Partial(),
        startupMode = StartupMode.COLD,
        iterations = 5,
        setupBlock = { pressHome() },
        measureBlock = {
            startActivityAndWait()
            awaitTopLevelNavigation()
        },
    )

    @Test
    fun primaryNavigationFrames() = benchmarkRule.measureRepeated(
        packageName = TARGET_PACKAGE,
        metrics = listOf(FrameTimingMetric()),
        compilationMode = CompilationMode.Partial(),
        startupMode = StartupMode.WARM,
        iterations = 5,
        setupBlock = {
            pressHome()
            startActivityAndWait()
            awaitTopLevelNavigation()
        },
        measureBlock = { navigatePrimarySurfaces() },
    )
}

@RunWith(AndroidJUnit4::class)
class GoatOsBaselineProfileGenerator {
    @get:Rule
    val baselineProfileRule = BaselineProfileRule()

    @Test
    fun startupAndPrimaryNavigation() = baselineProfileRule.collect(
        packageName = TARGET_PACKAGE,
        includeInStartupProfile = true,
    ) {
        pressHome()
        startActivityAndWait()
        awaitTopLevelNavigation()
        navigatePrimarySurfaces()
    }
}

private fun MacrobenchmarkScope.awaitTopLevelNavigation() {
    check(device.wait(Until.hasObject(By.desc("Calendar")), UI_TIMEOUT_MS)) {
        "Calendar navigation did not become ready; benchmark requires the live CI bootstrap"
    }
}

private fun MacrobenchmarkScope.navigatePrimarySurfaces() {
    tapNavigation("Vaccination")
    tapNavigation("Calendar")
    tapNavigation("You")
    tapNavigation("Calendar")
}

private fun MacrobenchmarkScope.tapNavigation(contentDescription: String) {
    val selector = By.desc(contentDescription)
    check(device.wait(Until.hasObject(selector), UI_TIMEOUT_MS)) {
        "$contentDescription navigation is missing from the backend bootstrap"
    }
    device.findObject(selector).click()
    device.waitForIdle()
}
