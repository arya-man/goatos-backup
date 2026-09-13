package sg.mesha.goatos.feature.vendors

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaDimens
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone
import sg.mesha.goatos.core.ui.ProofMediaPreview
import sg.mesha.goatos.core.ui.ProofMediaPreviewKind
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton

/**
 * One recorded animal (L2 drill under the load): the server's decision beside the inspector's own
 * verdict, then every SOP answer section by section in the order the form asked them, then every
 * photo and video by slot. Every word is the backend's — the animal title, the section names, the
 * question text, the rendered answers, the slot titles and the copy map — so the phone and the
 * CEO's web review read the same record. Read-only: the decision is the CEO's and is cast on web.
 */
@Composable
fun AnimalPurchaseAnimalDetailScreen(
    state: AnimalPurchaseAnimalDetailUiState,
    onEvent: (AnimalPurchaseAnimalDetailEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(AnimalPurchaseAnimalDetailEvent.Refresh) }
    Box(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        Column(Modifier.fillMaxSize()) {
            MeshaScreenHeader(
                title = state.title,
                subtitle = state.loadTitle.ifBlank { null },
                onBack = { onEvent(AnimalPurchaseAnimalDetailEvent.Back) },
                below = {
                    Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
                        if (state.decisionLabel.isNotBlank()) VendorsChip(label = state.decisionLabel, tone = state.decisionTone)
                        if (state.fieldVerdictLabel.isNotBlank()) VendorsChip(label = state.fieldVerdictLabel, tone = state.fieldVerdictTone)
                    }
                },
                actions = { SyncIconButton(isSyncing = state.isRefreshing, onSync = { onEvent(AnimalPurchaseAnimalDetailEvent.Refresh) }) },
            )
            LazyColumn(
                modifier = Modifier.fillMaxSize(),
                contentPadding = PaddingValues(top = 4.dp, bottom = 32.dp),
                verticalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                if (state.decidedByLine.isNotBlank() || state.decisionNote.isNotBlank()) {
                    item(key = "decision") {
                        VendorsCard(onClick = null) {
                            if (state.decidedByLine.isNotBlank()) {
                                Text(text = state.decidedByLine, color = MeshaColors.Muted, style = MeshaType.rowCaption)
                            }
                            if (state.decisionNote.isNotBlank()) {
                                Text(text = state.decisionNote, color = MeshaColors.Ink, style = MeshaType.rowLabel)
                            }
                        }
                    }
                }
                if (state.sections.isEmpty() && state.mediaSlots.isEmpty() && state.emptyMessage != null) {
                    item(key = "empty") {
                        EmptyState(
                            title = state.emptyMessage,
                            modifier = Modifier.fillMaxWidth().padding(horizontal = MeshaDimens.gutter),
                            icon = MeshaIcons.Goat,
                            tone = EmptyTone.Neutral,
                        )
                    }
                }
                if (state.sections.isNotEmpty() && state.answersTitle.isNotBlank()) {
                    item(key = "answers_title") { AnimalPurchaseDetailSectionLabel(state.answersTitle) }
                }
                // One card per SOP section, its rows in the order the form asked them. The key is
                // the section index: two sections may share a title, never a position.
                state.sections.forEachIndexed { index, section ->
                    item(key = "section:$index") {
                        VendorsCard(onClick = null) {
                            if (section.title.isNotBlank()) {
                                Text(text = section.title, color = MeshaColors.Ink, style = MeshaType.listTitle)
                                Spacer(Modifier.height(2.dp))
                            }
                            section.rows.forEach { row ->
                                AnimalPurchaseAnswerRow(row = row, attentionLabel = state.attentionLabel)
                            }
                        }
                    }
                }
                if (state.mediaSlots.isNotEmpty() && state.mediaTitle.isNotBlank()) {
                    item(key = "media_title") { AnimalPurchaseDetailSectionLabel(state.mediaTitle) }
                }
                state.mediaSlots.forEach { slot ->
                    item(key = "slot:" + slot.slot) {
                        VendorsCard(onClick = null) {
                            Text(text = slot.title, color = MeshaColors.Ink, style = MeshaType.listTitle)
                            slot.items.forEach { media ->
                                ProofMediaPreview(
                                    path = media.url,
                                    kind = if (media.isPhoto) ProofMediaPreviewKind.Photo else ProofMediaPreviewKind.Video,
                                    mediaIdentity = media.proofRef,
                                    modifier = Modifier.fillMaxWidth(),
                                    onPreviewAction = { onEvent(AnimalPurchaseAnimalDetailEvent.PreviewAction(it)) },
                                    // The person tapped this one animal open; its handful of photos show inline.
                                    inlineRemotePhoto = true,
                                )
                            }
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun AnimalPurchaseDetailSectionLabel(text: String) {
    Text(
        text = text,
        color = MeshaColors.Muted,
        style = MeshaType.sectionLabel,
        modifier = Modifier.padding(horizontal = MeshaDimens.gutter, vertical = 4.dp),
    )
}

@Composable
private fun AnimalPurchaseAnswerRow(row: AnimalPurchaseAnswerRowUi, attentionLabel: String) {
    Column(Modifier.fillMaxWidth().padding(vertical = 4.dp)) {
        Text(text = row.question, color = MeshaColors.Muted, style = MeshaType.rowCaption)
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(
                text = row.answer,
                color = if (row.attention) MeshaColors.Warn else MeshaColors.Ink,
                style = MeshaType.rowValue,
                modifier = Modifier.weight(1f, fill = false),
            )
            if (row.attention && attentionLabel.isNotBlank()) VendorsChip(label = attentionLabel, tone = VendorsTone.WARN)
        }
    }
}
