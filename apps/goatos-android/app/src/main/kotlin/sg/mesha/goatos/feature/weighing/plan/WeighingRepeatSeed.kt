package sg.mesha.goatos.feature.weighing.plan

import javax.inject.Inject
import javax.inject.Singleton

/** One shed bucket carried over from the task being repeated. */
data class WeighingRepeatBucket(
    val locationId: String,
    val category: String,
    val operatorUserId: String,
    val partitionLabel: String? = null,
)

/**
 * The answers carried over when a planner starts a new task FROM an existing one, or opens an
 * existing task to CHANGE it.
 *
 * [editCampaignId] is what tells the two apart. Null means an ordinary repeat: there is no
 * campaign id and no status, the wizard opens on its DATE step with nothing chosen, and the
 * ordinary create-then-publish path decides what survives on the new day. Set means an EDIT of
 * that exact campaign: the wizard opens already ON [editWeighDate] -- a task cannot change which
 * day it is by being edited -- and saving calls the update write against [editCampaignId] instead
 * of creating a new task. Either way nothing here is a copy of the task's current status; the
 * server's own rules (duplicate-shed block, park scope) still decide what is accepted.
 */
data class WeighingRepeatSeed(
    val parkId: String,
    val parkName: String,
    val sourceDateLabel: String,
    val buckets: List<WeighingRepeatBucket>,
    val editCampaignId: String? = null,
    val editWeighDate: String? = null,
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
