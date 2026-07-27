package sg.mesha.goatos.ui

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.nio.file.Path
import kotlin.io.path.readText

class WeighingRouteIdentityTest {
    @Test
    fun `weighing scan route carries campaign work group and selected shed identity`() {
        val route = Routes.weighingScanRoute(
            campaignId = "campaign A",
            workGroupId = "group B",
            campaignShedId = "shed C",
            category = "per_shed_partition",
            tenantId = "tenant T",
            expectedLocationId = "location L",
            expectedLocationLabel = "Gandhi 1 - Part 3",
            scanTitle = "Gandhi 1 - Part 3",
        )

        assertTrue(route.startsWith("/weighing/scan?"))
        assertTrue(route.contains("campaignId=campaign%20A"))
        assertTrue(route.contains("workGroupId=group%20B"))
        assertTrue(route.contains("campaignShedId=shed%20C"))
        assertTrue(route.contains("weighingCategory=per_shed_partition"))
        assertTrue(route.contains("tenantId=tenant%20T"))
        assertTrue(route.contains("expectedLocationId=location%20L"))
        assertTrue(route.contains("expectedLocationLabel=Gandhi%201%20-%20Part%203"))
        assertTrue(route.contains("scanTitle=Gandhi%201%20-%20Part%203"))
    }

    @Test
    fun `RFID completion key swallowing is scoped to active weighing scan route`() {
        assertTrue(routeAcceptsWeighingRfid("/weighing/scan?campaignId=c&workGroupId=g&campaignShedId=s"))
        assertTrue(routeAcceptsWeighingRfid("/weighing/scan?weighingCategory=individual_animal"))
        assertFalse(routeAcceptsWeighingRfid("/weighing/scan?weighingCategory=per_shed_partition"))
        assertFalse(routeAcceptsWeighingRfid("/weighing"))
        assertFalse(routeAcceptsWeighingRfid("/scan?shedId=shed-1"))
        assertFalse(routeAcceptsWeighingRfid(null))
    }

    @Test
    fun `weighing routes are registered as real navigation destinations`() {
        val navHost = Path.of("src/main/kotlin/sg/mesha/goatos/ui/AppNavHost.kt").readText()

        assertTrue(navHost.contains("composable(Routes.WEIGHING)"))
        assertTrue(navHost.contains("route = \"${'$'}{Routes.WEIGHING_SCAN}?"))
        assertTrue(navHost.contains("WeighingScreen("))
        assertTrue(navHost.contains("vm.setCaptureActive(rfidCaptureEnabled)"))
        assertTrue(navHost.contains("vm.setCompletionKeySwallowActive(rfidCaptureEnabled)"))
    }
}
