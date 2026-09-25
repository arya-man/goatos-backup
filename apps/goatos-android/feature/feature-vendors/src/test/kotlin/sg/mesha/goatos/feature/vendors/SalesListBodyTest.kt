package sg.mesha.goatos.feature.vendors

import androidx.paging.LoadState
import org.junit.Assert.assertEquals
import org.junit.Test

/** The ledger never says "No sales recorded yet" while it is loading or cannot reach the server. */
class SalesListBodyTest {
    private val empty = "No sales recorded yet"

    @Test
    fun `nothing cached and still loading shows loading, not an empty ledger`() {
        assertEquals(SalesListBody.Loading, salesListBody(0, LoadState.Loading, empty))
    }

    @Test
    fun `nothing cached and the load failed shows the unreachable state, not an empty ledger`() {
        assertEquals(SalesListBody.Unreachable, salesListBody(0, LoadState.Error(java.io.IOException("offline")), empty))
    }

    @Test
    fun `a successful load with no rows is the empty ledger`() {
        assertEquals(SalesListBody.Empty(empty), salesListBody(0, LoadState.NotLoading(endOfPaginationReached = true), empty))
    }

    @Test
    fun `cached rows stay on screen when a refresh fails and are marked stale`() {
        assertEquals(SalesListBody.Rows(stale = true), salesListBody(3, LoadState.Error(java.io.IOException("offline")), empty))
        assertEquals(SalesListBody.Rows(stale = false), salesListBody(3, LoadState.NotLoading(false), empty))
        assertEquals(SalesListBody.Rows(stale = false), salesListBody(3, LoadState.Loading, empty))
    }
}
