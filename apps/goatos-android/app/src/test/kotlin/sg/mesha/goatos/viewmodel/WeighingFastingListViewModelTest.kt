package sg.mesha.goatos.viewmodel

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import sg.mesha.goatos.core.data.weighing.WeighingFastingCard
import sg.mesha.goatos.core.network.dto.WeighingFastingShedCardDto

@RunWith(RobolectricTestRunner::class)
class WeighingFastingListViewModelTest {

    private val context: Context = ApplicationProvider.getApplicationContext()

    @Test
    fun `open removal card title shows only the shed once`() {
        val row = fastingCard(
            status = "open",
            subjectLabel = "Remove feed & water · Yashoda 10",
            shedLabel = "Yashoda 10",
        ).toVisibleUiRow(context)

        assertEquals("Yashoda 10", row?.title)
        assertEquals(true, row?.openable)
    }

    @Test
    fun `submitted and completed removal cards disappear from operator worklist`() {
        assertNull(fastingCard(status = "pending_verification").toVisibleUiRow(context))
        assertNull(fastingCard(status = "completed").toVisibleUiRow(context))
    }

    @Test
    fun `sent back removal card stays visible for retry`() {
        val row = fastingCard(status = "rework", reworkReason = "Water proof is unclear").toVisibleUiRow(context)

        assertEquals("Water proof is unclear", row?.reworkReason)
        assertEquals(true, row?.openable)
    }

    private fun fastingCard(
        status: String,
        subjectLabel: String = "Remove feed & water · Castro 2",
        shedLabel: String = "Castro 2",
        reworkReason: String? = null,
    ) = WeighingFastingCard(
        dto = WeighingFastingShedCardDto(
            fastingTaskId = "fasting-1",
            campaignShedId = "campaign-shed-1",
            shedLabel = shedLabel,
            subjectLabel = subjectLabel,
            status = status,
            reworkReason = reworkReason,
            removalBusinessDate = "2026-09-07",
        ),
    )
}
