package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.data.weighing.WeighingCapabilities
import sg.mesha.goatos.core.data.weighing.WeighingTask
import sg.mesha.goatos.core.data.weighing.WeighingTaskBucketCache
import sg.mesha.goatos.core.data.weighing.WeighingTaskShed
import sg.mesha.goatos.feature.weighing.WeighingTaskShedUiRow

/**
 * The CEO task-detail screen must not contradict itself, and it must never render a share of a
 * total weighing does not have.
 *
 * Live shape this is built from: one CBE task, four individual_animal buckets split two-and-two
 * between two operators. One operator finished BOTH of his buckets (each carrying five captured
 * weights); the other has not started either of his.
 */
class WeighingTaskDetailCountsTest {

    private fun bucket(
        name: String,
        operator: String,
        status: String,
        weighed: Int,
        submitted: Int = weighed,
    ) = WeighingTaskShed(
        campaignShedId = "bucket-$name",
        locationId = "loc-$name",
        displayName = name,
        category = "individual_animal",
        operatorUserId = operator,
        operatorDisplayName = operator,
        status = status,
        pendingVerificationCount = if (status == "completed") submitted else 0,
        reworkCount = 0,
        readyToClose = false,
        animalsWeighedCount = weighed,
        animalsSubmittedCount = submitted,
    )

    private fun task() = WeighingTask(
        campaignId = "task-1",
        tenantId = "tenant-1",
        parkId = "park-cbe",
        parkName = "CBE",
        weighDate = "2026-08-03",
        status = "in_progress",
        sheds = listOf(
            bucket("Gandhi 1", "Dinakar", "pending", 0),
            // MID-SHIFT: three animals weighed, NONE submitted. This is the state that used to be
            // invisible -- the bucket read as a bare "3" that meant something different on the
            // task detail than on the Operators screen.
            bucket("Gandhi 2", "Dinakar", "in_progress", weighed = 3, submitted = 0),
            bucket("Godel 1", "Pramod", "completed", 5),
            bucket("Yashoda 1", "Pramod", "completed", 5),
        ),
    )

    private fun state() = task().toTaskDetailUiState(
        campaignId = "task-1",
        selectedOperatorId = null,
        operatorNames = emptyMap(),
        capabilities = WeighingCapabilities(canEnd = true),
        buckets = WeighingTaskBucketCache(),
        loading = false,
        busy = false,
        staleNotice = "",
    )

    /**
     * DEFECT 1. Two of the four buckets were submitted and their cards say so
     * ("waiting for verifier"). The close button counted every bucket that was not yet ACCEPTED,
     * so it read "4 still open" beside two cards that plainly were not.
     */
    @Test
    fun `close button counts only buckets with no submitted work`() {
        assertEquals(2, state().openBucketCount)
    }

    /**
     * DEFECT 1, other half. The two submitted buckets are still unaccepted work the task carries;
     * they must be NAMED, not folded into "open", so the number and the cards agree.
     */
    @Test
    fun `submitted-but-unverified buckets are counted and named separately`() {
        assertEquals(2, state().awaitingVerificationBucketCount)
    }

    /**
     * DEFECT 2. Weighing is free-flow: migration 000079 dropped the expected-animal roster and
     * expected_animal_count is a fixed bucket-grain 1, so ANY fraction rendered for a bucket is a
     * share of a total that does not exist. A bucket card may carry no fractional fill at all.
     */
    @Test
    fun `a bucket card carries no fractional progress`() {
        val fractional = WeighingTaskShedUiRow::class.java.declaredFields
            .filter { it.type == java.lang.Float.TYPE || it.type == java.lang.Double.TYPE }
            .map { it.name }
        assertTrue(
            "a free-flow bucket card must carry no fractional fill, found $fractional",
            fractional.isEmpty(),
        )
    }

    /**
     * DEFECT 2, the replacement. What the bucket ACTUALLY holds is a plain backend-owned count,
     * reported as-is. Five weights captured reads as five, never as a percentage of anything.
     */
    @Test
    fun `a bucket reports the animals it actually holds`() {
        val godel = state().sheds.first { it.shedName == "Godel 1" }
        assertEquals(5, godel.animalsWeighedCount)
        assertEquals(5, godel.animalsSubmittedCount)
    }

    /**
     * THE MID-SHIFT CASE. Three animals weighed, none submitted. Both facts must survive to the
     * card as SEPARATE numbers: collapsing them into one count is what made an operator who had
     * weighed 3 and pressed nothing read as "3" on one screen and "0" on another, at the same
     * second, in the same app.
     */
    @Test
    fun `a mid-shift bucket reports weighed and submitted separately`() {
        val gandhi2 = state().sheds.first { it.shedName == "Gandhi 2" }
        assertEquals(3, gandhi2.animalsWeighedCount)
        assertEquals(0, gandhi2.animalsSubmittedCount)
    }

    /**
     * An operator chip must carry the name and the count as SEPARATE facts, so the screen can say
     * what the number counts. Pre-joining them rendered "Dinakar 2", which reads as part of a
     * person's name rather than as the two sheds he owns.
     */
    @Test
    fun `an operator chip carries name and shed count separately`() {
        val dinakar = state().operatorFilters.first { it.name == "Dinakar" }
        assertEquals(2, dinakar.shedCount)
    }
}
