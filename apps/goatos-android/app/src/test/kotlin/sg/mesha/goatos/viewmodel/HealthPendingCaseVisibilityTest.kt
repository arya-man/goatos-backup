package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.data.HealthFilters
import sg.mesha.goatos.core.data.sync.PendingHealthCaseOpen
import sg.mesha.goatos.core.data.sync.SyncItemStatus

class HealthPendingCaseVisibilityTest {
    private val adultReport = PendingHealthCaseOpen(
        outboxItemId = "outbox-health-1",
        goatId = "goat-1",
        goatDisplayId = "G-000327",
        diseaseKey = "fever",
        diseaseName = "Fever",
        ageBand = "adult",
        startDate = "2026-07-30",
        syncStatus = SyncItemStatus.QUEUED,
        lastError = null,
    )

    @Test
    fun `new offline report is visible on matching Health day without becoming a server action`() {
        val visible = visiblePendingHealthCases(
            filters = HealthFilters(ageBand = "adult", date = "2026-07-30"),
            pending = listOf(adultReport),
        )

        assertEquals(1, visible.size)
        assertEquals("G-000327", visible.single().goatDisplayId)
        assertEquals("Fever", visible.single().diseaseName)
        assertEquals("Waiting to sync", visible.single().statusLabel)
    }

    @Test
    fun `pending report follows date disease and backend-owned scope filters`() {
        assertTrue(
            visiblePendingHealthCases(
                HealthFilters(ageBand = "kid", date = "2026-07-30"),
                listOf(adultReport),
            ).isEmpty(),
        )
        assertTrue(
            visiblePendingHealthCases(
                HealthFilters(ageBand = "adult", date = "2026-07-30", diseaseKey = "diarrhea"),
                listOf(adultReport),
            ).isEmpty(),
        )
        assertTrue(
            visiblePendingHealthCases(
                HealthFilters(ageBand = "adult", date = "2026-07-30", shedId = "shed-1"),
                listOf(adultReport),
            ).isEmpty(),
        )
    }
}
