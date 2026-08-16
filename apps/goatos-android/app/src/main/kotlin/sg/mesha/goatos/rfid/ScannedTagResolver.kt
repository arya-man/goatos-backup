package sg.mesha.goatos.rfid

/**
 * Debug-only fixture hook. [sg.mesha.goatos.viewmodel.ScanViewModel.onTagRead] calls this on the
 * already-normalized tag BEFORE any roster/validation logic (`repo.findScanRosterByTag`, obligation
 * matching, etc.) touches it. The returned value replaces the tag used for everything downstream.
 *
 * Release builds always resolve to [PassthroughScannedTagResolver] (identity) via the @Provides in
 * app/src/release/kotlin/sg/mesha/goatos/rfid/ReleaseScannedTagResolverModule.kt — zero behavior
 * change in production. The real fixture, `DebugSampleTagAliaser`, is bound only in the debug build
 * type (app/src/debug/kotlin/sg/mesha/goatos/rfid/DebugScannedTagResolverModule.kt) and lets a
 * handful of physical RFID cards carrying fixed sample EPCs stand in for any number of real seeded
 * animals during phone QA.
 */
interface ScannedTagResolver {
    /**
     * @param rawTag the tag exactly as read off hardware (pre-[sg.mesha.goatos.rfid.RfidInputTransform]).
     * @param normalizedTag the tag after the SAME normalization roster matching uses
     *   (`normalize()` in ScanViewModel.kt: letters+digits only, lowercased).
     * @param shedId the active shed for this scan screen.
     * @param taskId the active task, or null when there is no task/roster context (e.g. free-flow
     *   weighing) — implementations MUST pass the tag through unchanged in that case.
     * @param partitionLabel the active partition, if any.
     * @return the normalized tag to use for roster lookup and everything downstream.
     */
    suspend fun resolve(
        rawTag: String,
        normalizedTag: String,
        shedId: String,
        taskId: String?,
        partitionLabel: String?,
    ): String
}

/** Identity resolver — always returns [normalizedTag] unchanged. The only binding release builds ever see. */
object PassthroughScannedTagResolver : ScannedTagResolver {
    override suspend fun resolve(
        rawTag: String,
        normalizedTag: String,
        shedId: String,
        taskId: String?,
        partitionLabel: String?,
    ): String = normalizedTag
}
