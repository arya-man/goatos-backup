package sg.mesha.goatos.feature.feed

import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.runtime.Immutable
import androidx.compose.ui.res.stringResource
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons

/**
 * FEED SOP (maintainer decision 2026-09-16, docs/decisions/feed-sop.md): the CARD a feed stage is
 * proven against, as the phone renders it -- the instruction, the capture slots in card order and
 * the questions -- plus this phone's capture state per slot and the operator's answers. Shared by
 * the packing, wastage and transport screens (the distribution screen carries the same fields on
 * its own state and renders through the same [feedSopCardItems]), so every stage follows the
 * authored card without a build.
 */
@Immutable
data class FeedSopCardUi(
    val instruction: String = "",
    val slots: List<FeedDistributionSlotUi> = emptyList(),
    val questions: List<FeedDistributionQuestionUi> = emptyList(),
    val answers: Map<String, String> = emptyMap(),
) {
    val anyCaptured: Boolean get() = slots.any { it.captured }
    val anyCapturing: Boolean get() = slots.any { it.isCapturing }

    /** Every compulsory slot is captured. Optional slots never gate the submit. */
    val compulsorySlotsFilled: Boolean
        get() = slots.isNotEmpty() && slots.filter { it.required }.all { it.captured }

    fun appliesTo(q: FeedDistributionQuestionUi): Boolean =
        q.onlyIfQuestion.isBlank() || answers[q.onlyIfQuestion] == q.onlyIfValue

    val requiredAnswersGiven: Boolean
        get() = questions.filter { it.required && appliesTo(it) }.all { !answers[it.id].isNullOrBlank() }

    /** Compulsory slots captured and durable-or-uploading, no captured slot failed, questions answered. */
    val readyToSubmit: Boolean
        get() = slots.isNotEmpty() &&
            slots.filter { it.required }.all { it.readyForSubmit } &&
            slots.filter { it.captured }.all { it.status.isQueuedForSubmit() } &&
            requiredAnswersGiven

    fun slot(slotKey: String): FeedDistributionSlotUi? = slots.firstOrNull { it.slotKey == slotKey }
}

/**
 * The card's slot rows and question cards, one item each, in card order. Words are the card's,
 * verbs are the app's; an `either` slot offers both. [locked] hides every action (the work is
 * submitted or decided elsewhere) while the captures stay visible.
 */
fun LazyListScope.feedSopCardItems(
    card: FeedSopCardUi,
    locked: Boolean,
    onCapture: (slotKey: String, kind: String?) -> Unit,
    onPlaybackFailed: (slotKey: String) -> Unit,
    onPreviewAction: (slotKey: String, action: String) -> Unit,
    onAnswer: (questionId: String, value: String) -> Unit,
) {
    items(card.slots, key = { "slot:" + it.slotKey }) { slot ->
        val isPhoto = slot.captureKind == FeedSlotCaptureKind.PHOTO ||
            (slot.captureKind == FeedSlotCaptureKind.EITHER && slot.capturedKind == FeedSlotCaptureKind.PHOTO && slot.captured)
        val captureVerb = when (slot.captureKind) {
            FeedSlotCaptureKind.PHOTO -> stringResource(R.string.feed_slot_take_photo)
            else -> stringResource(R.string.feed_slot_record_video)
        }
        FeedDistProofAction(
            title = captureVerb + " · " + slot.title,
            subtitle = listOf(
                slot.hint,
                if (slot.required) stringResource(R.string.feed_slot_required) else stringResource(R.string.feed_slot_optional),
            ).filter { it.isNotBlank() }.joinToString(" · "),
            icon = if (isPhoto) MeshaIcons.Plus else MeshaIcons.Video,
            captured = slot.captured,
            status = slot.status,
            previewPath = slot.previewPath,
            previewIdentity = slot.previewIdentity,
            previewKind = if (isPhoto) FeedDistPreviewKind.Photo else FeedDistPreviewKind.Video,
            capturedLabel = proofLabel(slot.status, slot.title),
            loading = slot.isCapturing,
            loadingLabel = stringResource(R.string.feed_dist_video_uploading),
            retryLabel = stringResource(R.string.feed_slot_retry, slot.title),
            replaceLabel = if (isPhoto) stringResource(R.string.feed_proof_recapture) else stringResource(R.string.feed_proof_rerecord),
            enabled = slot.captureEnabled && !locked,
            message = slot.message,
            onClick = { onCapture(slot.slotKey, null) },
            remotePreviewUrl = slot.remoteUrl,
            playbackEnabled = !card.anyCapturing,
            onPlaybackFailure = { onPlaybackFailed(slot.slotKey) },
            showAction = !locked,
            secondaryLabel = if (slot.captureKind == FeedSlotCaptureKind.EITHER && !locked) stringResource(R.string.feed_slot_take_photo) else null,
            onSecondaryClick = { onCapture(slot.slotKey, FeedSlotCaptureKind.PHOTO) },
            onPreviewAction = { action -> onPreviewAction(slot.slotKey, action) },
        )
    }
    val visibleQuestions = card.questions.filter { card.appliesTo(it) }
    items(visibleQuestions, key = { "q:" + it.id }) { q ->
        FeedSopQuestionCard(
            question = q,
            answer = card.answers[q.id].orEmpty(),
            otherText = card.answers[q.id + "_other"].orEmpty(),
            enabled = !locked,
            onAnswer = { v -> onAnswer(q.id, v) },
            onOther = { v -> onAnswer(q.id + "_other", v) },
        )
    }
}
