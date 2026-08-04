package sg.mesha.goatos.feature.weighing.plan

import javax.inject.Inject
import javax.inject.Singleton

/** One shed bucket carried over from the task being repeated. */
data class WeighingRepeatBucket(
    val locationId: String,
    val category: String,
    val operatorUserId: String,
)

/**
 * The answers carried over when a planner starts a new task FROM an existing one.
 *
 * Deliberately NOT a copy of the task: there is no campaign id here and no status. Repeating goes
 * through the ordinary create-then-publish path with a date the planner has to choose, so a
 * published row is never cloned and the server's duplicate block still decides what is allowed.
 */
data class WeighingRepeatSeed(
    val parkId: String,
    val parkName: String,
    val sourceDateLabel: String,
    val buckets: List<WeighingRepeatBucket>,
)

/**
 * A ONE-SHOT handoff of a repeat seed between the task list and the authoring wizard.
 *
 * The two screens have separate ViewModels by design, and a seed is a set of shed rows -- putting
 * it in the route would push a whole bucket list through a URL. The seed is consumed exactly once;
 * if the process died in between there is simply no seed and the wizard opens empty, which is
 * honest rather than half-filled.
 */
@Singleton
class WeighingRepeatSeedStore @Inject constructor() {

    private val pending = mutableMapOf<String, WeighingRepeatSeed>()

    @Synchronized
    fun stage(sourceCampaignId: String, seed: WeighingRepeatSeed) {
        if (sourceCampaignId.isBlank() || seed.buckets.isEmpty()) return
        // Only the most recent request can be acted on, so an abandoned one is never left behind
        // to prefill some later, unrelated wizard.
        pending.clear()
        pending[sourceCampaignId] = seed
    }

    @Synchronized
    fun take(sourceCampaignId: String): WeighingRepeatSeed? = pending.remove(sourceCampaignId)
}
