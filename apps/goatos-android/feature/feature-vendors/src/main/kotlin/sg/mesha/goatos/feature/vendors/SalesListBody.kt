package sg.mesha.goatos.feature.vendors

import androidx.paging.LoadState

/**
 * What the sales ledger shows in place of rows, decided from what is cached and what the last
 * load did. "No sales recorded yet" is a statement about the SERVER's ledger, so it is shown only
 * after a load actually succeeded and came back empty -- never while the first page is still on
 * its way, and never when the phone could not reach the server at all.
 */
sealed interface SalesListBody {
    /** Nothing cached and the first page is loading. */
    data object Loading : SalesListBody

    /** Nothing cached and the load failed: say so, and offer to try again. */
    data object Unreachable : SalesListBody

    /** A load succeeded and the ledger (for this filter) holds nothing. */
    data class Empty(val message: String) : SalesListBody

    /** Rows are on screen. [stale] when the latest refresh failed and they are the saved copy. */
    data class Rows(val stale: Boolean) : SalesListBody
}

fun salesListBody(itemCount: Int, refresh: LoadState, emptyMessage: String?): SalesListBody = when {
    itemCount > 0 -> SalesListBody.Rows(stale = refresh is LoadState.Error)
    refresh is LoadState.Loading -> SalesListBody.Loading
    refresh is LoadState.Error -> SalesListBody.Unreachable
    else -> SalesListBody.Empty(emptyMessage.orEmpty())
}

const val SALES_LOADING = "Loading sales…"
const val SALES_UNREACHABLE = "Could not load sales"
const val SALES_UNREACHABLE_HINT = "Check the connection and try again."
const val SALES_TRY_AGAIN = "Try again"
