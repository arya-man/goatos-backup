package sg.mesha.goatos.rfid

import java.util.concurrent.ConcurrentHashMap
import javax.inject.Inject
import javax.inject.Singleton
import sg.mesha.goatos.core.data.ExecutionRepository
import sg.mesha.goatos.core.data.cache.ScanRosterRowEntity

/**
 * Debug-only fixture: the maintainer owns 5 physical RFID cards with fixed sample EPCs
 * (normalized forms of TEMP-CPT-CASTRO1-001..005 — see [SAMPLE_NORMALIZED_TAGS]). When a scanned
 * tag's normalized form matches one of these, this resolver remaps it — BEFORE any roster/
 * validation logic runs, per [ScannedTagResolver] contract — to a REAL animal tag drawn from the
 * currently open task's roster, so the 5 physical cards can mimic scanning any number of real
 * seeded animals.
 *
 * Mapping contract:
 *  - Cards 1-3 -> first 3 OPEN (pending/due/in_progress) animals of the ACTIVE partition's roster,
 *    sorted by normalized primary tag (deterministic).
 *  - Cards 4-5 -> first 2 open animals of a NEIGHBORING partition of the same shed (exercises the
 *    neighbor-yellow flow); falls back to a different shed's open animals (cross-shed reject case)
 *    if no sibling partition with open animals is locally cached.
 *  - Stable within a scan session: once card N resolves to animal X for a given taskId, repeated
 *    scans of card N return X again (keeps double-scan detection testable). Cached per taskId, for
 *    the process lifetime of this singleton.
 *  - No roster/task context (e.g. free-flow weighing, which has no roster) -> passthrough, raw tag
 *    unchanged.
 *
 * Bound to [ScannedTagResolver] only in this debug build-type source set — see
 * DebugScannedTagResolverModule.kt. Never present in a release build.
 */
@Singleton
class DebugSampleTagAliaser @Inject constructor(
    private val repo: ExecutionRepository,
) : ScannedTagResolver {

    // taskId -> (card index 1..5 -> resolved normalized tag). Read-only against Room; this map is
    // the only mutable state, and it only ever grows for the life of the process.
    private val sessionCache = ConcurrentHashMap<String, ConcurrentHashMap<Int, String>>()

    override suspend fun resolve(
        rawTag: String,
        normalizedTag: String,
        shedId: String,
        taskId: String?,
        partitionLabel: String?,
    ): String {
        val cardIndex = SAMPLE_NORMALIZED_TAGS.indexOf(normalizedTag).takeIf { it >= 0 }?.plus(1) ?: return normalizedTag
        if (taskId == null) return normalizedTag // no roster context (e.g. free-flow weighing): passthrough

        val taskCache = sessionCache.getOrPut(taskId) { ConcurrentHashMap() }
        taskCache[cardIndex]?.let { return it }

        val resolved = resolveForCard(cardIndex, shedId, taskId, partitionLabel) ?: normalizedTag
        // Only cache a real remap; if resolution failed (no open animals found) let a later scan
        // retry rather than sticking the raw sample tag in the session cache forever.
        if (resolved != normalizedTag) taskCache[cardIndex] = resolved
        return resolved
    }

    private suspend fun resolveForCard(cardIndex: Int, shedId: String, taskId: String, partitionLabel: String?): String? =
        if (cardIndex <= 3) {
            repo.openScanRosterRows(shedId, taskId, partitionLabel)
                .dedupedOpenOrder()
                .getOrNull(cardIndex - 1)
                ?.normalizedPrimaryTag
        } else {
            val neighborIndex = cardIndex - 4 // cards 4,5 -> index 0,1
            val sibling = repo.siblingPartitionOpenRows(shedId, taskId, partitionLabel).dedupedOpenOrder()
            val chosen = sibling.getOrNull(neighborIndex)
                ?: repo.otherShedOpenRows(shedId, taskId).dedupedOpenOrder().getOrNull(neighborIndex)
            chosen?.normalizedPrimaryTag
        }

    /** One row per goat (a goat may have several vaccine-obligation rows), deterministic order. */
    private fun List<ScanRosterRowEntity>.dedupedOpenOrder(): List<ScanRosterRowEntity> =
        distinctBy { it.goatId }.sortedWith(compareBy({ it.normalizedPrimaryTag }, { it.goatId }))

    companion object {
        /** Normalized forms of the 5 physical sample cards (TEMP-CPT-CASTRO1-001..005), matching
         *  the SAME normalization ScanViewModel.normalize() applies (letters+digits only, lowercased). */
        val SAMPLE_NORMALIZED_TAGS: List<String> = listOf(
            "tempcptcastro1001",
            "tempcptcastro1002",
            "tempcptcastro1003",
            "tempcptcastro1004",
            "tempcptcastro1005",
        )
    }
}
