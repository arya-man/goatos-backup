package sg.mesha.goatos.ui

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class WeighingRouteIdentityTest {
    @Test
    fun `weighing scan route carries campaign work group and selected shed identity`() {
        val route = Routes.weighingScanRoute(
            campaignId = "campaign A",
            workGroupId = "group B",
            campaignShedId = "shed C",
            scanTitle = "Gandhi 1 - Part 3",
        )

        assertTrue(route.startsWith("/weighing/scan?"))
        assertTrue(route.contains("campaignId=campaign%20A"))
        assertTrue(route.contains("workGroupId=group%20B"))
        assertTrue(route.contains("campaignShedId=shed%20C"))
        assertTrue(route.contains("scanTitle=Gandhi%201%20-%20Part%203"))
    }

    @Test
    fun `RFID completion key swallowing is scoped to active weighing scan route`() {
        assertTrue(routeAcceptsWeighingRfid("/weighing/scan?campaignId=c&workGroupId=g&campaignShedId=s"))
        assertFalse(routeAcceptsWeighingRfid("/weighing"))
        assertFalse(routeAcceptsWeighingRfid("/scan?shedId=shed-1"))
        assertFalse(routeAcceptsWeighingRfid(null))
    }
}
