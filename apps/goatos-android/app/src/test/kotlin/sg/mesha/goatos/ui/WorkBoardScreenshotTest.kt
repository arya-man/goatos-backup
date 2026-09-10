package sg.mesha.goatos.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.paging.PagingData
import androidx.paging.compose.collectAsLazyPagingItems
import app.cash.paparazzi.DeviceConfig
import app.cash.paparazzi.Paparazzi
import kotlinx.coroutines.flow.flowOf
import org.junit.Rule
import org.junit.Test
import sg.mesha.goatos.core.designsystem.locale.ProvideAppLocale
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.feature.workboard.WorkBoardChipUi
import sg.mesha.goatos.feature.workboard.WorkBoardDetailScreen
import sg.mesha.goatos.feature.workboard.WorkBoardDetailUiState
import sg.mesha.goatos.feature.workboard.WorkBoardEmptyMessage
import sg.mesha.goatos.feature.workboard.WorkBoardOwnerState
import sg.mesha.goatos.feature.workboard.WorkBoardRowUi
import sg.mesha.goatos.feature.workboard.WorkBoardScreen
import sg.mesha.goatos.feature.workboard.WorkBoardSeverity
import sg.mesha.goatos.feature.workboard.WorkBoardUiState

/**
 * Goldens for the Work Board's My Work screen (maintainer decision 2026-09-10): loading, empty
 * (own rows), populated, offline, and one row's detail. Every business sentence on a card is the
 * backend's, rendered verbatim, so the fixtures carry farm copy the server would compose.
 */
class WorkBoardScreenshotTest {
    @get:Rule
    val paparazzi = Paparazzi(deviceConfig = DeviceConfig.PIXEL_2.copy(screenWidth = 720, screenHeight = 1280))

    @Test
    fun loading() {
        paparazzi.snapshot(name = "work_board_loading") {
            board(state = baseState(hasSummary = false, emptyMessage = WorkBoardEmptyMessage.LOADING), rows = emptyList())
        }
    }

    @Test
    fun emptyOwnRows() {
        paparazzi.snapshot(name = "work_board_empty_own") {
            board(
                state = baseState(
                    hasSummary = true,
                    emptyMessage = WorkBoardEmptyMessage.EMPTY_OWN,
                    ownRowsOnly = true,
                    lanes = lanes(total = 0),
                    modules = emptyList(),
                ),
                rows = emptyList(),
            )
        }
    }

    @Test
    fun populated() {
        paparazzi.snapshot(name = "work_board_populated") {
            board(state = baseState(hasSummary = true), rows = sampleRows())
        }
    }

    @Test
    fun offlineWithCachedRows() {
        paparazzi.snapshot(name = "work_board_offline") {
            board(
                state = baseState(hasSummary = true, isOffline = true),
                rows = sampleRows(),
            )
        }
    }

    @Test
    fun detail() {
        paparazzi.snapshot(name = "work_board_detail") {
            GoatOsTheme {
                ProvideAppLocale {
                    Box(Modifier.fillMaxSize().background(MeshaColors.Bg)) {
                        WorkBoardDetailScreen(
                            state = WorkBoardDetailUiState(loading = false, row = sampleRows()[1], canOpen = true),
                        )
                    }
                }
            }
        }
    }

    @Composable
    private fun board(state: WorkBoardUiState, rows: List<WorkBoardRowUi>) {
        GoatOsTheme {
            ProvideAppLocale {
                val paged = flowOf(PagingData.from(rows)).collectAsLazyPagingItems()
                Box(Modifier.fillMaxSize().background(MeshaColors.Bg)) {
                    WorkBoardScreen(state = state, rows = paged)
                }
            }
        }
    }

    private fun lanes(total: Int = 9, todo: Int = 4, inProgress: Int = 2, inReview: Int = 1, done: Int = 2) = listOf(
        WorkBoardChipUi(key = "", count = total, selected = true),
        WorkBoardChipUi(key = "todo", count = todo, selected = false),
        WorkBoardChipUi(key = "in_progress", count = inProgress, selected = false),
        WorkBoardChipUi(key = "in_review", count = inReview, selected = false),
        WorkBoardChipUi(key = "done", count = done, selected = false),
    )

    private fun baseState(
        hasSummary: Boolean,
        emptyMessage: WorkBoardEmptyMessage? = null,
        ownRowsOnly: Boolean = false,
        isOffline: Boolean = false,
        lastSyncedAt: Long? = null,
        lanes: List<WorkBoardChipUi> = lanes(),
        modules: List<WorkBoardChipUi> = listOf(
            WorkBoardChipUi(key = "", count = 9, selected = true),
            WorkBoardChipUi(key = "feed", count = 4, selected = false),
            WorkBoardChipUi(key = "vaccination", count = 3, selected = false),
            WorkBoardChipUi(key = "weighing", count = 2, selected = false),
        ),
    ) = WorkBoardUiState(
        dateIso = "2026-09-10",
        dateLabel = "Thu, 10 Sep",
        isToday = true,
        lanes = if (hasSummary) lanes else lanes.map { it.copy(count = 0) },
        modules = if (hasSummary) modules else emptyList(),
        doneCount = 2,
        pendingCount = 7,
        needsAttentionCount = 1,
        hasSummary = hasSummary,
        isRefreshing = false,
        lastSyncedAt = lastSyncedAt,
        isOffline = isOffline,
        emptyMessage = emptyMessage,
        ownRowsOnly = ownRowsOnly,
    )

    private fun sampleRows(): List<WorkBoardRowUi> = listOf(
        WorkBoardRowUi(
            rowKey = "feed|feed_packing_completion|a1",
            module = "feed",
            title = "Pack Godel 1 - Part 3 · Morning",
            subtitle = "12.4 kg concentrate, 6 kg green fodder",
            penLabel = "Godel 1 - Part 3",
            parkName = "Channapatna",
            businessDate = "2026-09-10",
            clockLabel = "Due by 3:00 pm",
            workState = "due",
            lane = "todo",
            severity = WorkBoardSeverity.WATCH,
            ownerName = "Amit Kumar",
            ownerState = WorkBoardOwnerState.ASSIGNED,
            done = 0,
            pending = 1,
            href = "/feed-packing",
        ),
        WorkBoardRowUi(
            rowKey = "vaccination|sop_task|b2",
            module = "vaccination",
            title = "ET+TT · Castro 2",
            subtitle = "48 animals, 2 pens in this drive",
            penLabel = "Castro 2",
            parkName = "Channapatna",
            businessDate = "2026-09-10",
            clockLabel = "Overdue since yesterday",
            workState = "overdue",
            lane = "todo",
            severity = WorkBoardSeverity.BROKEN,
            ownerName = "Darshan Talwar",
            ownerState = WorkBoardOwnerState.ASSIGNED,
            done = 31,
            pending = 17,
            needsAttention = 1,
            href = "/vaccination",
        ),
        WorkBoardRowUi(
            rowKey = "weighing|weighing_campaign_shed|c3",
            module = "weighing",
            title = "Weigh Mandela 1 - Part 2",
            subtitle = "Whole pen, one video",
            penLabel = "Mandela 1 - Part 2",
            parkName = "Channapatna",
            businessDate = "2026-09-10",
            clockLabel = "Video in review",
            workState = "verification_pending",
            lane = "in_review",
            severity = WorkBoardSeverity.OK,
            ownerName = "",
            ownerState = WorkBoardOwnerState.POOL,
            done = 1,
            pending = 0,
        ),
        WorkBoardRowUi(
            rowKey = "feed|feed_distribution_completion|d4",
            module = "feed",
            title = "Feed Gandhi 3 · Evening",
            subtitle = "",
            penLabel = "Gandhi 3",
            parkName = "Channapatna",
            businessDate = "2026-09-10",
            clockLabel = "Done at 4:10 pm",
            workState = "completed",
            lane = "done",
            severity = WorkBoardSeverity.OK,
            ownerName = "",
            ownerState = WorkBoardOwnerState.MISSING,
            done = 1,
            pending = 0,
        ),
    )
}
