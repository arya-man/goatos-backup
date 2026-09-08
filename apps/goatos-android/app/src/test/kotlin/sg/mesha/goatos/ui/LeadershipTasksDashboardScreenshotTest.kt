package sg.mesha.goatos.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.unit.dp
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.paging.PagingData
import androidx.paging.compose.collectAsLazyPagingItems
import app.cash.paparazzi.DeviceConfig
import app.cash.paparazzi.Paparazzi
import kotlinx.coroutines.flow.flowOf
import org.junit.Rule
import org.junit.Test
import sg.mesha.goatos.core.designsystem.locale.ProvideAppLocale
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.feature.leadershiptasks.LeadershipAttachmentKind
import sg.mesha.goatos.feature.leadershiptasks.LeadershipAttachmentUi
import sg.mesha.goatos.feature.leadershiptasks.LeadershipStatusOptionUi
import sg.mesha.goatos.feature.leadershiptasks.LeadershipTaskCardUi
import sg.mesha.goatos.feature.leadershiptasks.LeadershipTaskDetailScreen
import sg.mesha.goatos.feature.leadershiptasks.LeadershipTaskDetailUiState
import sg.mesha.goatos.feature.leadershiptasks.LeadershipTaskFilterUi
import sg.mesha.goatos.feature.leadershiptasks.LeadershipTaskListScreen
import sg.mesha.goatos.feature.leadershiptasks.LeadershipTaskListUiState
import sg.mesha.goatos.feature.leadershiptasks.LeadershipTaskScopeUi

class LeadershipTasksDashboardScreenshotTest {
    @get:Rule
    val paparazzi = Paparazzi(deviceConfig = DeviceConfig.PIXEL_2.copy(screenWidth = 720, screenHeight = 1280))

    @Test
    fun ceoTeamProgressBoard() {
        paparazzi.snapshot(name = "leadership_tasks_ceo_team_progress_board") {
            GoatOsTheme {
                ProvideAppLocale {
                    val rows = flowOf(PagingData.from(taskRows())).collectAsLazyPagingItems()
                    Box(androidx.compose.ui.Modifier.fillMaxSize().background(MeshaColors.Bg)) {
                        LeadershipTaskListScreen(
                            state = LeadershipTaskListUiState(
                                title = "Tasks",
                                canRaise = true,
                                scopes = listOf(
                                    LeadershipTaskScopeUi("assigned_to_me", "Assigned to me", 2, false),
                                    LeadershipTaskScopeUi("assigned_by_me", "Assigned by me", 7, false),
                                    LeadershipTaskScopeUi("team_progress", "Team progress", 18, true),
                                ),
                                filters = listOf(
                                    LeadershipTaskFilterUi("all", "All", 18, true),
                                    LeadershipTaskFilterUi("open", "Open", 6, false),
                                    LeadershipTaskFilterUi("in_progress", "Doing", 5, false),
                                    LeadershipTaskFilterUi("done", "Done", 7, false),
                                ),
                            ),
                            rows = rows,
                        )
                    }
                }
            }
        }
    }

    @Test
    fun ceoAssigneePickerShowsDirectorParkHeadEmployee() {
        paparazzi.snapshot(name = "leadership_tasks_ceo_assignee_picker") {
            GoatOsTheme {
                ProvideAppLocale {
                    AssigneePickerPreview()
                }
            }
        }
    }

    @Test
    fun directorAssignedTaskDetail() {
        paparazzi.snapshot(name = "leadership_tasks_director_assigned_detail") {
            taskDetail("Assigned by Manju · 08/09/2026", "Feed Director", "Video evidence attached")
        }
    }

    @Test
    fun parkHeadAssignedTaskDetail() {
        paparazzi.snapshot(name = "leadership_tasks_park_head_assigned_detail") {
            taskDetail("Assigned by Ravi · 08/09/2026", "Park Head", "Completion video expected in comment")
        }
    }

    @Composable
    private fun taskDetail(meta: String, titleSuffix: String, comment: String) {
        GoatOsTheme {
            ProvideAppLocale {
                Box(androidx.compose.ui.Modifier.fillMaxSize().background(MeshaColors.Bg)) {
                    LeadershipTaskDetailScreen(
                        state = LeadershipTaskDetailUiState(
                            loading = false,
                            numberLabel = "#18",
                            statusChip = "Open",
                            status = "open",
                            title = "Check CPT west fence repair - $titleSuffix",
                            body = "Take this from the Tasks app, attach the repair proof, and move it to Done once checked.",
                            metaLine = meta,
                            attachments = listOf(
                                LeadershipAttachmentUi("a1", "p1", LeadershipAttachmentKind.VIDEO, "repair-before.mp4", "4.2 MB", "0:18"),
                                LeadershipAttachmentUi("a2", "p2", LeadershipAttachmentKind.AUDIO, "voice-note.m4a", "380 KB", "0:22"),
                            ),
                            statusOptions = listOf(
                                LeadershipStatusOptionUi("in_progress", "Doing"),
                                LeadershipStatusOptionUi("done", "Done"),
                            ),
                            canComment = true,
                            commentDraft = comment,
                        ),
                    )
                }
            }
        }
    }

    @Composable
    private fun AssigneePickerPreview() {
        Column(Modifier.fillMaxSize().background(MeshaColors.PageBg)) {
            MeshaScreenHeader(title = "New task")
            Column(Modifier.fillMaxWidth().padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Text("For", color = MeshaColors.Faint, style = MeshaType.sectionLabel)
                Row(
                    Modifier
                        .fillMaxWidth()
                        .clip(RoundedCornerShape(14.dp))
                        .background(MeshaColors.Surf)
                        .border(1.dp, MeshaColors.BrandD, RoundedCornerShape(14.dp))
                        .padding(horizontal = 16.dp, vertical = 14.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text("Choose director, park head, or employee", color = MeshaColors.Muted, style = MeshaType.bodyStrong, modifier = Modifier.weight(1f))
                    Icon(MeshaIcons.ChevronDown, contentDescription = null, tint = MeshaColors.Muted, modifier = Modifier.size(18.dp))
                }
                Column(
                    Modifier
                        .fillMaxWidth()
                        .clip(RoundedCornerShape(12.dp))
                        .background(MeshaColors.Surf3)
                        .border(1.dp, MeshaColors.Hair, RoundedCornerShape(12.dp)),
                ) {
                    listOf("Manohar - Feed Director", "Satish - Park Head", "Prakash - Employee").forEach { name ->
                        Text(name, color = MeshaColors.Ink, style = MeshaType.bodyStrong, modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 14.dp))
                    }
                }
            }
        }
    }

    private fun taskRows(): List<LeadershipTaskCardUi> = listOf(
        LeadershipTaskCardUi("1", "1", "#18", "Doing", "in_progress", "Check CPT west fence repair before evening close", "For Satish · 08/09/2026", 3),
        LeadershipTaskCardUi("2", "2", "#17", "Open", "open", "Confirm director handoff for feed unloading delay", "For Manohar · 08/09/2026", 2),
        LeadershipTaskCardUi("3", "3", "#16", "Done", "done", "Send Borewell-2 motor reading after restart", "For Prakash · 07/09/2026", 4),
    )
}
