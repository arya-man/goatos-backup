package sg.mesha.goatos.core.ui.sop

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.ProofMediaPreview
import sg.mesha.goatos.core.ui.ProofMediaPreviewKind
import sg.mesha.goatos.core.ui.R

/*
 * THE SOP CARD (SOP → operator parity, maintainer decisions 2026-09-16).
 *
 * The capture slots + questions a module's authored SOP card asks for, as the phone renders them,
 * shared by every module that runs an authored card: the feed stages (feed.direction / packing /
 * transport, where these types were born and still carry typealiases) and the herd operations
 * capture forms (Add birth / Add death). Moved here unchanged so one card renders the same way on
 * every screen; the words are always the card's, the verbs the app's.
 */

enum class SopProofStatus { EMPTY, QUEUED, UPLOADING, SYNCED, FAILED }

fun SopProofStatus.isQueuedForSubmit(): Boolean =
    this == SopProofStatus.QUEUED ||
        this == SopProofStatus.UPLOADING ||
        this == SopProofStatus.SYNCED

/** What a slot accepts, from the card: video | photo | either. */
object SopSlotCaptureKind {
    const val VIDEO = "video"
    const val PHOTO = "photo"
    const val EITHER = "either"
}

/**
 * One capture slot of the card, with this phone's view of it. Backend-owned words (title, hint,
 * kind, required) come from the card; the rest is capture/upload state.
 */
@androidx.compose.runtime.Immutable
data class SopSlotUi(
    /** The card's slot key = the proof register field_key stamped on the upload. */
    val slotKey: String,
    val title: String,
    val hint: String = "",
    /** video | photo | either -- what the slot accepts, from the card. */
    val captureKind: String = SopSlotCaptureKind.VIDEO,
    /** False for an optional capture the crew may leave empty. */
    val required: Boolean = true,
    /** What was actually captured here (an `either` slot records the pick). */
    val capturedKind: String = SopSlotCaptureKind.VIDEO,
    val isCapturing: Boolean = false,
    val captured: Boolean = false,
    val status: SopProofStatus = SopProofStatus.EMPTY,
    val message: String? = null,
    val previewPath: String? = null,
    val previewIdentity: String = "feed-distribution:$slotKey",
    val remoteUrl: String? = null,
) {
    /**
     * A slot is enabled by ITS OWN state alone -- never by a sibling's. That independence is the
     * collaboration contract (docs/product/feed-proof-collaboration.md) and is machine-checked.
     */
    val captureEnabled: Boolean
        get() = !isCapturing

    /** Ready to be named on the submit: captured and durable, or still uploading (the outbox waits). */
    val readyForSubmit: Boolean
        get() = captured && status.isQueuedForSubmit()
}

/** One authored question of the card, rendered verbatim; the answer is the operator's. */
@androidx.compose.runtime.Immutable
data class SopQuestionUi(
    val id: String,
    val kind: String,
    val title: String,
    val hint: String = "",
    val required: Boolean = false,
    val options: List<Pair<String, String>> = emptyList(),
    val allowOther: Boolean = false,
    val unit: String = "",
    /** A number question's authored bounds, inclusive; null = unbounded on that side. */
    val min: Double? = null,
    val max: Double? = null,
    /** Hidden unless an earlier pick-one holds this value. */
    val onlyIfQuestion: String = "",
    val onlyIfValue: String = "",
)

@Immutable
data class SopCardUi(
    val instruction: String = "",
    val slots: List<SopSlotUi> = emptyList(),
    val questions: List<SopQuestionUi> = emptyList(),
    val answers: Map<String, String> = emptyMap(),
) {
    val anyCaptured: Boolean get() = slots.any { it.captured }
    val anyCapturing: Boolean get() = slots.any { it.isCapturing }

    /** Every compulsory slot is captured. Optional slots never gate the submit. */
    val compulsorySlotsFilled: Boolean
        get() = slots.isNotEmpty() && slots.filter { it.required }.all { it.captured }

    /**
     * The questions this card asks right now. A conditional question applies only when its parent
     * APPLIES and holds the value -- the same chain the server walks, so a follow-up whose parent
     * was hidden by an earlier change of mind is hidden too, even if its own parent still holds
     * the old pick.
     */
    private val applicableIds: Set<String> by lazy {
        val out = HashSet<String>(questions.size) // mobile-guard:ignore: bounded by the card's question count, rebuilt per card
        questions.forEach { q ->
            if (q.onlyIfQuestion.isBlank() ||
                (q.onlyIfQuestion in out && answers[q.onlyIfQuestion].orEmpty().trim() == q.onlyIfValue)
            ) {
                out += q.id
            }
        }
        out
    }

    fun appliesTo(q: SopQuestionUi): Boolean = q.id in applicableIds

    /**
     * The first answer the server would refuse, judged exactly as `sop/authored` ValidateAnswers
     * judges it; null when the answers would be accepted. Cards without questions never have one.
     */
    val answerProblem: SopAnswerProblem? by lazy { questions.firstNotNullOfOrNull { q -> checkAnswer(q) } }

    /** Every answer the server would refuse right now, in card order (questions that apply only). */
    val answerProblems: List<SopAnswerProblem> by lazy { questions.mapNotNull { q -> checkAnswer(q) } }

    /** Every question the card asks is answered the way the server will accept. */
    val requiredAnswersGiven: Boolean
        get() = answerProblem == null

    private fun checkAnswer(q: SopQuestionUi): SopAnswerProblem? {
        if (!appliesTo(q)) return null
        fun problem(kind: SopAnswerProblemKind) = SopAnswerProblem(q.id, q.title, kind, q.min, q.max)
        val raw = answers[q.id].orEmpty()
        val offered = q.options.map { it.first }.toSet()
        return when (q.kind) {
            "choice" -> {
                val v = raw.trim()
                when {
                    v.isEmpty() -> if (q.required) problem(SopAnswerProblemKind.ANSWER) else null
                    v !in offered -> problem(SopAnswerProblemKind.PICK_OFFERED)
                    v == "other" && q.allowOther && answers[q.id + "_other"].orEmpty().isBlank() ->
                        problem(SopAnswerProblemKind.WRITE_OTHER)
                    else -> null
                }
            }
            "multi" -> {
                val picked = raw.split(",").map { it.trim() }.filter { it.isNotEmpty() }
                when {
                    picked.isEmpty() -> if (q.required) problem(SopAnswerProblemKind.TICK_ONE) else null
                    picked.any { it !in offered } -> problem(SopAnswerProblemKind.PICK_ONLY_OFFERED)
                    else -> null
                }
            }
            "number" -> {
                val v = raw.trim()
                val n = v.toDoubleOrNull()
                when {
                    v.isEmpty() -> if (q.required) problem(SopAnswerProblemKind.ENTER) else null
                    n == null || n.isNaN() || n.isInfinite() -> problem(SopAnswerProblemKind.ENTER_NUMBER)
                    (q.min != null && n < q.min) || (q.max != null && n > q.max) -> problem(SopAnswerProblemKind.OUT_OF_RANGE)
                    else -> null
                }
            }
            "text" -> when {
                q.required && raw.isBlank() -> problem(SopAnswerProblemKind.ENTER)
                raw.toByteArray(Charsets.UTF_8).size > SOP_ANSWER_MAX_TEXT_BYTES -> problem(SopAnswerProblemKind.TOO_LONG)
                else -> null
            }
            else -> null
        }
    }

    /** Compulsory slots captured and durable-or-uploading, no captured slot failed, questions answered. */
    val readyToSubmit: Boolean
        get() = slots.isNotEmpty() &&
            slots.filter { it.required }.all { it.readyForSubmit } &&
            slots.filter { it.captured }.all { it.status.isQueuedForSubmit() } &&
            requiredAnswersGiven

    fun slot(slotKey: String): SopSlotUi? = slots.firstOrNull { it.slotKey == slotKey }

    /** The card asks nothing (no captures, no questions): the plain form. */
    val isEmpty: Boolean get() = slots.isEmpty() && questions.isEmpty()

    /**
     * A FORM's readiness (herd capture cards): like [readyToSubmit] but a card with no capture
     * slots -- questions only -- is ready once its compulsory questions are answered (maintainer
     * decision 6: a form may ask questions and no media).
     */
    val readyAsForm: Boolean
        get() = slots.filter { it.required }.all { it.readyForSubmit } &&
            slots.filter { it.captured }.all { it.status.isQueuedForSubmit() } &&
            requiredAnswersGiven
}

/**
 * What a card's status line says before submit (Realme E2E 2026-09-17). The line used to read
 * "upload is in progress" as soon as ANY capture existed, so a card still owed a required capture
 * -- nothing uploading at all -- told the operator to wait for an upload that would never come.
 * A missing capture or answer is now NAMED by the card's own title; the in-progress words are
 * kept for the one case they are true: everything recorded and answered, submit still pending.
 */
sealed interface SopCardProgress {
    /** Submit is available. */
    data object Ready : SopCardProgress

    /** Nothing recorded yet: the card's generic "record every required capture" words. */
    data object NothingCaptured : SopCardProgress

    /** Every required capture and answer is in; an upload (or the completion) is still pending. */
    data object Uploading : SopCardProgress

    /** Required captures / answers still owed, by the card's own titles, in card order. */
    data class StillNeeded(val titles: List<String>) : SopCardProgress
}

fun SopCardUi.progress(submitEnabled: Boolean): SopCardProgress {
    if (submitEnabled) return SopCardProgress.Ready
    val owed = slots.filter { it.required && !it.captured }.map { it.title } + answerProblems.map { it.title }
    if (owed.isEmpty()) return if (anyCaptured) SopCardProgress.Uploading else SopCardProgress.NothingCaptured
    // Before anything is recorded the card keeps its generic words (the seeded single-video card
    // said exactly this and still does); a card with no authored titles falls back to them too.
    val titles = owed.filter { it.isNotBlank() }.distinct()
    if (!anyCaptured || titles.isEmpty()) return SopCardProgress.NothingCaptured
    return SopCardProgress.StillNeeded(titles)
}

/** The server's limit on a written answer (`sop/authored` MaxTextLength, bytes). */
const val SOP_ANSWER_MAX_TEXT_BYTES = 2000

/** Why the server would refuse an answer -- one per rule of `sop/authored` ValidateAnswers. */
enum class SopAnswerProblemKind { ANSWER, PICK_OFFERED, WRITE_OTHER, TICK_ONE, PICK_ONLY_OFFERED, ENTER, ENTER_NUMBER, OUT_OF_RANGE, TOO_LONG }

/**
 * One answer the server would refuse, naming the question. [message] carries the same words the
 * server sends for the same refusal, so a phone check and a server refusal read alike.
 */
@Immutable
data class SopAnswerProblem(
    val questionId: String,
    val title: String,
    val kind: SopAnswerProblemKind,
    val min: Double? = null,
    val max: Double? = null,
) {
    val message: String
        get() = when (kind) {
            SopAnswerProblemKind.ANSWER -> "Answer: $title"
            SopAnswerProblemKind.PICK_OFFERED -> "Pick one of the offered answers for: $title"
            SopAnswerProblemKind.WRITE_OTHER -> "Write the other answer for: $title"
            SopAnswerProblemKind.TICK_ONE -> "Tick at least one for: $title"
            SopAnswerProblemKind.PICK_ONLY_OFFERED -> "Pick only the offered answers for: $title"
            SopAnswerProblemKind.ENTER -> "Enter: $title"
            SopAnswerProblemKind.ENTER_NUMBER -> "Enter a number for: $title"
            SopAnswerProblemKind.TOO_LONG -> "Write a shorter answer for: $title"
            SopAnswerProblemKind.OUT_OF_RANGE -> when {
                min != null && max != null -> "Enter a value between ${plain(min)} and ${plain(max)} for: $title"
                min != null -> "Enter a value of at least ${plain(min)} for: $title"
                else -> "Enter a value of at most ${plain(max ?: 0.0)} for: $title"
            }
        }

    private fun plain(n: Double): String =
        if (n == Math.floor(n) && !n.isInfinite() && kotlin.math.abs(n) < 1e15) n.toLong().toString() else n.toString()
}

/**
 * The card's slot rows and question cards, one item each, in card order. Words are the card's,
 * verbs are the app's; an `either` slot offers both. [locked] hides every action (the work is
 * submitted or decided elsewhere) while the captures stay visible.
 */
fun LazyListScope.sopCardItems(
    card: SopCardUi,
    locked: Boolean,
    onCapture: (slotKey: String, kind: String?) -> Unit,
    onPlaybackFailed: (slotKey: String) -> Unit,
    onPreviewAction: (slotKey: String, action: String) -> Unit,
    onAnswer: (questionId: String, value: String) -> Unit,
) {
    items(card.slots, key = { "slot:" + it.slotKey }) { slot ->
        val isPhoto = slot.captureKind == SopSlotCaptureKind.PHOTO ||
            (slot.captureKind == SopSlotCaptureKind.EITHER && slot.capturedKind == SopSlotCaptureKind.PHOTO && slot.captured)
        val captureVerb = when (slot.captureKind) {
            SopSlotCaptureKind.PHOTO -> stringResource(R.string.sop_slot_take_photo)
            else -> stringResource(R.string.sop_slot_record_video)
        }
        SopProofAction(
            title = captureVerb + " · " + slot.title,
            subtitle = listOf(
                slot.hint,
                if (slot.required) stringResource(R.string.sop_slot_required) else stringResource(R.string.sop_slot_optional),
            ).filter { it.isNotBlank() }.joinToString(" · "),
            icon = if (isPhoto) MeshaIcons.Plus else MeshaIcons.Video,
            captured = slot.captured,
            status = slot.status,
            previewPath = slot.previewPath,
            previewIdentity = slot.previewIdentity,
            previewKind = if (isPhoto) ProofMediaPreviewKind.Photo else ProofMediaPreviewKind.Video,
            // The slot keeps its NAME once captured: "Carcass photo · Proof ready", never a bare
            // "Proof ready" that leaves the operator guessing which capture it is.
            capturedLabel = sopSlotStatusLine(slot.title, sopProofLabel(slot.status, slot.title)),
            loading = slot.isCapturing,
            loadingLabel = sopSlotStatusLine(slot.title, stringResource(R.string.sop_video_uploading)),
            retryLabel = stringResource(R.string.sop_slot_retry, slot.title),
            replaceLabel = if (isPhoto) stringResource(R.string.sop_proof_recapture) else stringResource(R.string.sop_proof_rerecord),
            enabled = slot.captureEnabled && !locked,
            message = slot.message,
            onClick = { onCapture(slot.slotKey, null) },
            remotePreviewUrl = slot.remoteUrl,
            playbackEnabled = !card.anyCapturing,
            onPlaybackFailure = { onPlaybackFailed(slot.slotKey) },
            showAction = !locked,
            secondaryLabel = if (slot.captureKind == SopSlotCaptureKind.EITHER && !locked) stringResource(R.string.sop_slot_take_photo) else null,
            onSecondaryClick = { onCapture(slot.slotKey, SopSlotCaptureKind.PHOTO) },
            onPreviewAction = { action -> onPreviewAction(slot.slotKey, action) },
        )
    }
    val visibleQuestions = card.questions.filter { card.appliesTo(it) }
    items(visibleQuestions, key = { "q:" + it.id }) { q ->
        SopQuestionCard(
            question = q,
            answer = card.answers[q.id].orEmpty(),
            otherText = card.answers[q.id + "_other"].orEmpty(),
            enabled = !locked,
            onAnswer = { v -> onAnswer(q.id, v) },
            onOther = { v -> onAnswer(q.id + "_other", v) },
        )
    }
}

/** "<slot title> · <status>", or just the status when it already is the title (an empty slot). */
fun sopSlotStatusLine(title: String, status: String): String = when {
    title.isBlank() || status == title -> status
    status.isBlank() -> title
    else -> "$title · $status"
}

@Composable
fun sopProofLabel(status: SopProofStatus, recordedLabel: String): String = when (status) {
    SopProofStatus.SYNCED -> stringResource(R.string.sop_proof_synced)
    SopProofStatus.UPLOADING -> stringResource(R.string.sop_proof_uploading)
    SopProofStatus.QUEUED -> stringResource(R.string.sop_proof_waiting)
    SopProofStatus.FAILED -> stringResource(R.string.sop_proof_failed)
    SopProofStatus.EMPTY -> recordedLabel
}


@Composable
fun SopProofAction(
    title: String,
    subtitle: String,
    icon: ImageVector,
    captured: Boolean,
    status: SopProofStatus,
    previewPath: String?,
    previewIdentity: String,
    previewKind: ProofMediaPreviewKind,
    capturedLabel: String,
    loading: Boolean,
    loadingLabel: String,
    retryLabel: String,
    replaceLabel: String,
    enabled: Boolean,
    message: String?,
    onClick: () -> Unit,
    remotePreviewUrl: String? = null,
    playbackEnabled: Boolean = true,
    onPlaybackFailure: () -> Unit = {},
    showAction: Boolean = true,
    /** A second verb for an `either` slot ("Take photo" beside "Record video"). */
    secondaryLabel: String? = null,
    onSecondaryClick: () -> Unit = {},
    onPreviewAction: (String) -> Unit = {},
) {
    val failed = status == SopProofStatus.FAILED
    val synced = status == SopProofStatus.SYNCED
    val uploading = loading || status == SopProofStatus.UPLOADING
    val border = when {
        failed -> MeshaColors.Danger
        synced -> MeshaColors.Ok
        captured -> MeshaColors.Brand.copy(alpha = 0.5f)
        else -> MeshaColors.Hair
    }
    val iconBg = when {
        failed -> MeshaColors.Danger.copy(alpha = 0.14f)
        synced -> MeshaColors.Ok.copy(alpha = 0.14f)
        captured -> MeshaColors.Brand.copy(alpha = 0.14f)
        else -> MeshaColors.Surf2
    }
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .heightIn(min = 82.dp)
            .clip(RoundedCornerShape(18.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, border, RoundedCornerShape(18.dp))
            .clickable(enabled = enabled && !captured && !uploading, onClick = onClick)
            .padding(14.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(
            modifier = Modifier.size(42.dp).clip(RoundedCornerShape(14.dp)).background(iconBg),
            contentAlignment = Alignment.Center,
        ) {
            when {
                uploading -> CircularProgressIndicator(modifier = Modifier.size(18.dp), strokeWidth = 2.dp, color = MeshaColors.Brand)
                failed -> Icon(MeshaIcons.Warn, contentDescription = null, tint = MeshaColors.Danger, modifier = Modifier.size(22.dp))
                synced -> Icon(MeshaIcons.Check, contentDescription = null, tint = MeshaColors.Ok, modifier = Modifier.size(22.dp))
                captured -> Icon(icon, contentDescription = null, tint = MeshaColors.BrandD, modifier = Modifier.size(22.dp))
                else -> Icon(icon, contentDescription = null, tint = MeshaColors.Muted, modifier = Modifier.size(22.dp))
            }
        }
        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
            Text(
                text = when {
                    uploading -> loadingLabel
                    failed -> retryLabel
                    captured -> capturedLabel
                    else -> title
                },
                color = if (failed) MeshaColors.Danger else if (enabled || captured || uploading) MeshaColors.Ink else MeshaColors.Faint,
                style = MeshaType.cardTitle,
            )
            Text(text = subtitle, color = MeshaColors.Muted, style = MeshaType.cardSubtitle)
            var localPreviewFailed by remember(previewPath) { mutableStateOf(false) }
            val previewToShow = if (!previewPath.isNullOrBlank() && !localPreviewFailed) {
                previewPath
            } else {
                remotePreviewUrl ?: previewPath
            }
            if (!previewToShow.isNullOrBlank()) {
                SopProofPreview(
                    path = previewToShow,
                    kind = previewKind,
                    mediaIdentity = previewIdentity,
                    playbackEnabled = playbackEnabled,
                    onPlaybackFailure = {
                        if (previewToShow == previewPath) {
                            localPreviewFailed = true
                        }
                        onPlaybackFailure()
                    },
                    onPreviewAction = onPreviewAction,
                )
                if (showAction) {
                    SopRetryButton(label = if (failed) retryLabel else replaceLabel, enabled = enabled, onClick = onClick)
                }
            } else if (!uploading && showAction) {
                SopRetryButton(label = if (captured || failed) replaceLabel else title, enabled = enabled, onClick = onClick)
            }
            if (secondaryLabel != null && !uploading && showAction) {
                SopRetryButton(label = secondaryLabel, enabled = enabled, onClick = onSecondaryClick)
            }
            if (!message.isNullOrBlank()) {
                Text(text = message, color = MeshaColors.Faint, style = MeshaType.caption)
            }
        }
    }
}

@Composable
private fun SopProofPreview(
    path: String,
    kind: ProofMediaPreviewKind,
    mediaIdentity: String,
    playbackEnabled: Boolean = true,
    onPlaybackFailure: () -> Unit = {},
    onPreviewAction: (String) -> Unit = {},
) {
    ProofMediaPreview(
        path = path,
        kind = kind,
        mediaIdentity = mediaIdentity,
        onPlaybackFailure = onPlaybackFailure,
        expandable = true,
        onPreviewAction = onPreviewAction,
        playbackEnabled = playbackEnabled,
        inlineRemotePhoto = kind == ProofMediaPreviewKind.Photo,
    )
}

@Composable
fun SopRetryButton(label: String, enabled: Boolean = true, onClick: () -> Unit) {
    Text(
        text = label,
        color = if (enabled) MeshaColors.OnBrand else MeshaColors.Muted,
        style = MeshaType.cta,
        modifier = Modifier
            .minimumInteractiveComponentSize()
            .clip(RoundedCornerShape(14.dp))
            .background(if (enabled) MeshaColors.Brand else MeshaColors.Surf2)
            .clickable(enabled = enabled, onClick = onClick)
            .padding(horizontal = 12.dp, vertical = 13.dp),
    )
}

/**
 * One authored question of the card. Pick-one renders its choices as tappable chips; pick-many
 * toggles; number and text are a single field. Words are the card's, verbatim.
 */
@Composable
fun SopQuestionCard(
    question: SopQuestionUi,
    answer: String,
    otherText: String,
    enabled: Boolean,
    onAnswer: (String) -> Unit,
    onOther: (String) -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(18.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(18.dp))
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(
            text = question.title + if (question.required) " *" else "",
            color = MeshaColors.Ink,
            style = MeshaType.cardTitle,
        )
        if (question.hint.isNotBlank()) {
            Text(text = question.hint, color = MeshaColors.Muted, style = MeshaType.cardSubtitle)
        }
        when (question.kind) {
            "choice", "multi" -> {
                val picked = if (question.kind == "multi") answer.split(",").filter { it.isNotBlank() }.toSet() else setOf(answer)
                androidx.compose.foundation.layout.FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    question.options.forEach { (value, label) ->
                        val on = value in picked
                        Text(
                            text = label,
                            color = if (on) MeshaColors.OnBrand else MeshaColors.Ink,
                            style = MeshaType.cta,
                            modifier = Modifier
                                .minimumInteractiveComponentSize()
                                .clip(RoundedCornerShape(14.dp))
                                .background(if (on) MeshaColors.Brand else MeshaColors.Surf2)
                                .clickable(enabled = enabled) {
                                    if (question.kind == "multi") {
                                        val next = if (on) picked - value else picked + value
                                        onAnswer(question.options.map { it.first }.filter { it in next }.joinToString(","))
                                    } else {
                                        onAnswer(if (on) "" else value)
                                    }
                                }
                                .padding(horizontal = 12.dp, vertical = 10.dp),
                        )
                    }
                }
                if (question.kind == "choice" && question.allowOther && answer == "other") {
                    androidx.compose.material3.OutlinedTextField(
                        value = otherText,
                        onValueChange = onOther,
                        enabled = enabled,
                        singleLine = true,
                        label = { Text(stringResource(R.string.sop_question_other_label)) },
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
            }
            "number" -> androidx.compose.material3.OutlinedTextField(
                value = answer,
                onValueChange = { v -> if (v.isEmpty() || v.matches(Regex("^-?\\d*(\\.\\d*)?$"))) onAnswer(v) },
                enabled = enabled,
                singleLine = true,
                suffix = if (question.unit.isNotBlank()) ({ Text(question.unit) }) else null,
                keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(keyboardType = androidx.compose.ui.text.input.KeyboardType.Decimal),
                modifier = Modifier.fillMaxWidth(),
            )
            else -> androidx.compose.material3.OutlinedTextField(
                value = answer,
                onValueChange = onAnswer,
                enabled = enabled,
                modifier = Modifier.fillMaxWidth(),
            )
        }
    }
}
