package sg.mesha.goatos.ui

import androidx.compose.runtime.Immutable
import sg.mesha.goatos.feature.counts.WorkflowDetailUiState

/**
 * The ONE answer to "which Sales writes may this phone START for this person", read off the
 * backend's bootstrap flags and nowhere else. Two authorities, because the server has two:
 *
 *  - [canRecord] -- `sales_write` (sales.write): record a sale, a receipt, a status, a lead, evidence.
 *  - [canTag]    -- `sales_tag` (sales.allocate_animals): tag the animals a sale is made of.
 *
 * An ABSENT flag is a server that predates it, so the write stays offered and the server's own 403
 * is still the gate; only an explicit false withholds it. Every write ENTRY honours this: the
 * buttons that open a flow, the hosted routes themselves (a deep link or a stale back stack must
 * not mount a form whose save the server refuses), and the sale workflow's tagging step.
 */
@Immutable
data class SalesWriteAccess(
    val canRecord: Boolean = true,
    val canTag: Boolean = true,
) {
    fun allows(entry: SalesWriteEntry): Boolean = when (entry) {
        SalesWriteEntry.RECORD -> canRecord
        SalesWriteEntry.TAG -> canTag
    }

    companion object {
        fun fromFeatureFlags(flags: Map<String, Boolean>): SalesWriteAccess = SalesWriteAccess(
            canRecord = flags["sales_write"] != false,
            canTag = flags["sales_tag"] != false,
        )
    }
}

/** The authority a hosted Sales write route needs. */
enum class SalesWriteEntry { RECORD, TAG }

/**
 * Which authority each hosted Sales route that STARTS a write needs; null for a read route. Exact
 * route patterns, never a prefix: `/sales/sale/{id}` is a read, `/sales/sale/{id}/tag` a write.
 */
fun salesWriteEntryFor(route: String?): SalesWriteEntry? = when (route) {
    Routes.SALE_NEW -> SalesWriteEntry.RECORD
    Routes.SALE_TAG_ANIMALS -> SalesWriteEntry.TAG
    else -> null
}

/**
 * The sale workflow's "Tag the animals sold" step opens the tagging screen. For a person who may
 * not tag, the step stays on the card READ-ONLY -- its title, owner and status still say what is
 * owed -- but it no longer opens a flow the server would refuse.
 */
fun WorkflowDetailUiState.withSaleTaggingFor(access: SalesWriteAccess): WorkflowDetailUiState =
    if (access.canTag || actions.none { it.opensSaleTagging }) {
        this
    } else {
        copy(actions = actions.map { if (it.opensSaleTagging) it.copy(opensSaleTagging = false) else it })
    }

/** Farm copy for a Sales write route this person cannot use. */
fun salesWriteUnavailableCopy(entry: SalesWriteEntry): Pair<String, String> = when (entry) {
    SalesWriteEntry.RECORD -> "Record sale" to "You can view sales but not record them. Ask your manager if you need to record a sale."
    SalesWriteEntry.TAG -> "Tag animals" to "You can view this sale but not tag its animals. Ask your manager if you need to tag them."
}
