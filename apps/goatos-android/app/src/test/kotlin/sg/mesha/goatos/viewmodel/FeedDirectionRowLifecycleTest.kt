package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.feature.feed.FeedDirectionRowUi
import sg.mesha.goatos.feature.feed.FeedStatus

class FeedDirectionRowLifecycleTest {
    @Test
    fun `only pending feed direction rows can open capture`() {
        assertTrue(feedRow("").canOpenCapture)
        assertTrue(feedRow(FeedStatus.PENDING).canOpenCapture)
        assertFalse(feedRow(FeedStatus.AWAITING).canOpenCapture)
        assertFalse(feedRow(FeedStatus.COMPLETED).canOpenCapture)
    }

    private fun feedRow(lifecycleStatus: String): FeedDirectionRowUi = FeedDirectionRowUi(
        grainKey = "row-1",
        parkId = "park-1",
        parkLabel = "CPT",
        shedId = "shed-1",
        sessionNo = 1,
        shedLabel = "Godel 1",
        partitionLabel = "",
        shedTag = "",
        breed = "",
        rationGroup = "",
        experimentArm = "",
        sessionLabel = "Morning",
        headCount = 0,
        headCountInformational = false,
        workflow = "normal",
        items = emptyList(),
        sessionTotalKg = "0",
        blocked = false,
        overduePending = false,
        completed = lifecycleStatus == FeedStatus.COMPLETED,
        lifecycleStatus = lifecycleStatus,
    )
}
