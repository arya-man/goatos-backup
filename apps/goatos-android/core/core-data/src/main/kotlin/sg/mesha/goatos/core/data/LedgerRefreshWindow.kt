package sg.mesha.goatos.core.data

import androidx.paging.LoadType
import androidx.paging.PagingConfig

/**
 * The two pieces that keep a person's place in an OFFSET-paged ledger across a refresh, shared by
 * the Sales ledger and the Feed Purchases ledger (Sales phone E2E 2026-09-26; Feed Purchases the
 * same day). A pull-to-refresh, or coming back from a row's detail, must bring back the row the
 * person was looking at -- not start the ledger again from row 0.
 */

/** A ledger refresh re-reads at most this many pages, so a refresh never walks the whole ledger. */
internal const val LEDGER_REFRESH_MAX_PAGES = 10

/**
 * The pager config for such a ledger. Placeholders keep every position ABSOLUTE: without them,
 * once the 60-row window dropped its first page the pager counted the person's place from the
 * first row it still held, so a refresh at the bottom reloaded the wrong rows and the list jumped
 * towards the top. The rows themselves stay bounded by maxSize; a placeholder is only a count.
 */
internal fun ledgerPagingConfig(): PagingConfig = PagingConfig(
    pageSize = VENDORS_PAGE_SIZE,
    initialLoadSize = VENDORS_PAGE_SIZE,
    prefetchDistance = 3,
    enablePlaceholders = true,
    maxSize = VENDORS_PAGE_SIZE * 3,
)

/** What one mediator load read: the rows, where the next page starts, and the last page itself. */
internal class OffsetWindow<P, T>(val rows: List<T>, val nextOffset: Int, val total: Int, val lastPage: P) {
    val endReached: Boolean get() = rows.isEmpty() || nextOffset >= total
}

/**
 * Reads one mediator load. An APPEND is one ordinary page. A REFRESH re-reads every page up to
 * where the person is, one ordinary page per request: re-reading page one alone wiped the rows
 * below it, so a refresh at the bottom threw the person back to the top. The Room source reloads
 * around the anchor, so the window it needs is the anchor plus half a page, capped at
 * [LEDGER_REFRESH_MAX_PAGES].
 */
internal suspend fun <P, T> readOffsetWindow(
    loadType: LoadType,
    anchorPosition: Int?,
    offset: Int,
    fetch: suspend (offset: Int) -> P,
    rowsOf: (P) -> List<T>,
    totalOf: (P) -> Int,
): OffsetWindow<P, T> {
    val rowsWanted = if (loadType == LoadType.REFRESH) {
        ((anchorPosition ?: 0) + VENDORS_PAGE_SIZE / 2 + 1).coerceAtMost(VENDORS_PAGE_SIZE * LEDGER_REFRESH_MAX_PAGES)
    } else {
        1
    }
    val rows = mutableListOf<T>() // mobile-guard:ignore: local to one mediator load, at most LEDGER_REFRESH_MAX_PAGES pages of 20
    var nextOffset = offset
    var page: P
    var total: Int
    do {
        page = fetch(nextOffset)
        val got = rowsOf(page)
        rows += got
        nextOffset += got.size
        total = totalOf(page)
        val pageEnded = got.isEmpty() || nextOffset >= total
    } while (!pageEnded && nextOffset - offset < rowsWanted)
    return OffsetWindow(rows, nextOffset, total, page)
}
