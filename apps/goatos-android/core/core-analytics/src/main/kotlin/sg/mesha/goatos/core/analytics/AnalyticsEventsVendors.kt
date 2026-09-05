package sg.mesha.goatos.core.analytics

/**
 * Vendors-module analytics constants (maintainer decision 2026-09-03): the vendor register and the
 * feed purchase ledger on the phone. Separate object for the [AnalyticsEventsToxin] reason; every
 * name is `snake_case` and `vendors_`-prefixed.
 */
object AnalyticsEventsVendors {
    /** The register list (L0) was opened or resumed. */
    const val VENDORS_LIST_VIEWED = "vendors_list_viewed"

    /** The register was searched or narrowed by a status chip; reason carries the chip key. */
    const val VENDORS_LIST_FILTERED = "vendors_list_filtered"

    /** A vendor row was opened. */
    const val VENDORS_VENDOR_OPENED = "vendors_vendor_opened"

    /** The add-vendor wizard was opened. */
    const val VENDORS_ADD_OPENED = "vendors_add_opened"

    /** A voice note was recorded and durably captured (before the vendor write drains). */
    const val VENDORS_VOICE_NOTE_CAPTURED = "vendors_voice_note_captured"

    /** A vendor write was durably queued on the outbox. */
    const val VENDORS_VENDOR_QUEUED = "vendors_vendor_queued"

    /** The feed purchase ledger (L0) was opened or resumed. */
    const val VENDORS_PURCHASES_VIEWED = "vendors_purchases_viewed"

    /** A purchase row was opened. */
    const val VENDORS_PURCHASE_OPENED = "vendors_purchase_opened"

    /** A feed purchase write was durably queued on the outbox. */
    const val VENDORS_PURCHASE_QUEUED = "vendors_purchase_queued"

    /**
     * The sales ledger (L0) was opened or resumed. The wire name keeps its original spelling after
     * the ledger moved into its own Sales module (2026-09-05): renaming a shipped event name breaks
     * every dashboard already reading it, and the event means the same thing it always did.
     */
    const val VENDORS_SALES_VIEWED = "vendors_sales_viewed"

    /** A sale row was opened. */
    const val VENDORS_SALE_OPENED = "vendors_sale_opened"

    /** A sale write was durably queued on the outbox. */
    const val VENDORS_SALE_QUEUED = "vendors_sale_queued"

    /** The tag-animals flow was opened for a sale. */
    const val VENDORS_TAG_ANIMALS_OPENED = "vendors_tag_animals_opened"

    // Editing a recorded sale, and the pipeline/evidence panels (maintainer instruction 2026-09-04).
    const val VENDORS_SALE_PAYMENT_OPENED = "vendors_sale_payment_opened"
    const val VENDORS_SALE_EDITED = "vendors_sale_edited"
    const val VENDORS_PIPELINE_OPENED = "vendors_pipeline_opened"
    const val VENDORS_PIPELINE_QUEUED = "vendors_pipeline_queued"
    const val VENDORS_PURCHASE_EDIT_OPENED = "vendors_purchase_edit_opened"
    const val VENDORS_PURCHASE_EDITED = "vendors_purchase_edited"

    /** Animals were marked sold against a sale; reason carries the count. */
    const val VENDORS_TAG_ANIMALS_CONFIRMED = "vendors_tag_animals_confirmed"

    /** Any failure on a Vendors surface; reason carries the bounded cause. */
    const val VENDORS_FAILURE = "vendors_failure"

    object Params {
        const val STEP = "step"
    }
}
