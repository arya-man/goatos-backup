package sg.mesha.goatos.core.analytics

/**
 * Animal-purchases analytics event/param constants (maintainer decision 2026-09-13 —
 * `docs/decisions/animal-purchases.md`).
 *
 * Kept in a SEPARATE object for the same reason [AnalyticsEventsToxin] is: the slice can grow its
 * own instrumentation without touching the shared file every other module's telemetry lives in.
 * Every name is `snake_case` and `animal_purchase_`-prefixed, and every param reuses an existing
 * key from [AnalyticsEvents.Params] wherever one fits.
 *
 * Why these exist: a load is recorded, its animals are filmed one by one, and the CEO decides
 * each on another surface. A load that stalls at "recorded but no animals" or an animal whose
 * video never reached the outbox is invisible from any single screen — only this funnel says
 * where the buying desk's work stopped.
 */
object AnalyticsEventsAnimalPurchase {
    /** The load list (the tab's L0 route) was opened or resumed. */
    const val LIST_VIEWED = "animal_purchase_list_viewed"

    /** A load row was tapped and the load screen opened. */
    const val LOAD_OPENED = "animal_purchase_load_opened"

    /** The add-load form was opened. */
    const val LOAD_ADD_OPENED = "animal_purchase_load_add_opened"

    /** A load was durably queued on the outbox — recorded even offline. */
    const val LOAD_QUEUED = "animal_purchase_load_queued"

    /** The add-animal form was opened. */
    const val ANIMAL_ADD_OPENED = "animal_purchase_animal_add_opened"

    /** The animal's video was recorded and durably captured — before the create drains. */
    const val ANIMAL_VIDEO_CAPTURED = "animal_purchase_animal_video_captured"

    /** An animal was durably queued on the outbox, referencing its own proof row. */
    const val ANIMAL_QUEUED = "animal_purchase_animal_queued"

    /** A queued load/animal write reached a terminal outcome (saved, still queued, rejected). */
    const val WRITE_OUTCOME = "animal_purchase_write_outcome"

    /** Local playback/share/fullscreen actions for a recorded animal video preview. */
    const val VIDEO_PREVIEW_ACTION = "animal_purchase_video_preview_action"

    /** The person tapped retry on an animal whose video upload had given up. */
    const val ANIMAL_RETRY = "animal_purchase_animal_retry"

    /** A recorded animal's card was tapped and its full record (answers + media) opened. */
    const val ANIMAL_OPENED = "animal_purchase_animal_opened"

    /**
     * Any animal-purchase capture/queue/read path failed. [AnalyticsEvents.Params.REASON] carries
     * the real message the repository/capture layer returned, truncated like every other reason
     * field — never a fabricated code. Paired with a `CrashReporter.recordException` on the path.
     */
    const val FAILURE = "animal_purchase_failure"

    object Params {
        /** The load a load-scoped event refers to. */
        const val LOAD_ID = "load_id"

        /** The candidate an animal-scoped event refers to. */
        const val CANDIDATE_ID = "candidate_id"

        /** The preview action taken (play, pause, fullscreen, share, failure). */
        const val ACTION = "action"
    }
}
