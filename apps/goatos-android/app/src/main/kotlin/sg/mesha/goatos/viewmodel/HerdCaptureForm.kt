package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import sg.mesha.goatos.capture.PhotoCaptureSource
import sg.mesha.goatos.capture.ProofCaptureContext
import sg.mesha.goatos.capture.ProofCapturePrompt
import sg.mesha.goatos.capture.sopSlotPhotoContext
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.data.CaptureDraftRepository
import sg.mesha.goatos.core.data.CountsCaptureCardRepository
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofFlow
import sg.mesha.goatos.core.data.capture.ProofIdentity
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.forms.ProofPolicy
import sg.mesha.goatos.core.data.sync.CountsCapturePayload
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.CountsCaptureCardResponseDto
import sg.mesha.goatos.core.ui.sop.SopCardUi

/**
 * The SOP CAPTURE CARD on the Add birth / Add death forms (maintainer decisions 4 and 7,
 * 2026-09-16): the authored photos, videos and questions a report carries, rendered from the
 * Room-cached card and captured through the shared [SopSlotController].
 *
 * DEPLOY-DAY RULE: until someone publishes a capture card for the kind, [card] is empty, [active]
 * is false and [payload] is null -- the form renders and submits exactly as it did before this
 * existed, and the request carries no `sop_capture`.
 *
 * One capture draft per report: its proofs share a draft group key persisted in the saved state
 * (so a clip survives process death) and [reset] closes the controller, forgets that draft's saved
 * slot keys and starts a fresh group for the next report. The proof uploads drain on their own
 * group; the report's dispatch waits for each one to finish before resolving its proof id.
 */
internal class HerdCaptureForm(
    private val kind: String,
    private val scope: CoroutineScope,
    private val savedStateHandle: SavedStateHandle,
    private val syncRepository: SyncRepository,
    private val proofCaptureSource: ProofCaptureSource,
    private val photoCaptureSource: PhotoCaptureSource,
    private val proofCaptureRepository: ProofCaptureRepository,
    private val captureCards: CountsCaptureCardRepository,
    private val captureDrafts: CaptureDraftRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    private val proofSubject: ProofSubject,
    private val proofScopeType: String,
    private val prompt: ProofCapturePrompt,
    /** The capture's subject at capture time (the pen for a birth, the animal for a death). */
    private val subjectId: () -> String,
    private val locked: () -> Boolean,
    private val onChanged: () -> Unit,
) {
    private val stageKey = "counts.$kind.capture"
    private val group = DraftIdempotencyKey(savedStateHandle, "$stageKey.group", "counts-$kind-capture")
    private var loaded: CountsCaptureCardResponseDto? = null
    private val answeredOnce = mutableSetOf<String>() // mobile-guard:ignore: bounded by the card's question count
    private var controller: SopSlotController = newController()
    private var observeJob: Job? = null

    /** The card as rendered; empty when no capture card is published for this kind. */
    val card: SopCardUi get() = if (active) controller.state.value else SopCardUi()

    /** True once a non-empty published card has been loaded for this kind. */
    val active: Boolean
        get() = loaded?.card?.let { it.proofs.isNotEmpty() || it.questions.isNotEmpty() } == true

    /** The form may submit: no card, or every compulsory slot/question of the card is done. */
    val ready: Boolean get() = !active || controller.state.value.readyAsForm

    fun start() {
        controller.start()
        observeJob = scope.launch {
            captureCards.observeCard(kind).collect { response ->
                loaded = response
                applyLoaded()
                onChanged()
            }
        }
    }

    /** Re-reads the published card (every form open); a failure keeps whatever Room holds. */
    fun refresh() {
        scope.launch {
            captureCards.refreshCard(kind).onFailure { error ->
                crashReporter.recordException(error, "counts $kind capture card refresh failed")
                analytics.track(
                    AnalyticsEvents.COUNTS_CAPTURE_FAILURE,
                    mapOf(
                        AnalyticsEvents.Params.KIND to kind,
                        AnalyticsEvents.Params.REASON to "card_refresh_failed",
                    ),
                )
            }
        }
    }

    fun capture(slotKey: String, requestedKind: String?) = controller.captureSlot(slotKey, requestedKind)

    fun answer(questionId: String, value: String) {
        controller.answer(questionId, value)
        if (answeredOnce.add(questionId)) {
            analytics.track(AnalyticsEvents.COUNTS_CAPTURE_ANSWERED, props(null, "answered") + ("question_id" to questionId))
        }
    }

    /**
     * The submit's capture extras, or null when this form carries none: no card published (the
     * older-app shape the server still accepts), or a compulsory slot not yet captured.
     */
    fun payload(): CountsCapturePayload? {
        if (!active) return null
        val refs = controller.formSlotRefs() ?: return null
        return CountsCapturePayload(
            sopVersionId = loaded?.sopVersionId?.takeIf { it.isNotBlank() },
            slotProofs = refs,
            answers = controller.answersJson(),
        )
    }

    /** Starts a fresh capture draft for the next report (the previous one's uploads keep draining). */
    fun reset() {
        controller.close()
        savedStateHandle.keys().filter { it.startsWith("$stageKey.") }.forEach { savedStateHandle.remove<Any>(it) }
        answeredOnce.clear()
        controller = newController()
        controller.start()
        applyLoaded()
        onChanged()
    }

    /**
     * The capture's SUBJECT changed (a death report's selected animal was switched): every proof
     * recorded so far is filed under the previous subject, so this draft drops them and starts a
     * fresh proof group -- they can never be sent for the new subject. Answers describe the report,
     * not the animal's proof, and are kept.
     */
    fun rebindSubject() {
        val answers = controller.state.value.answers
        val answered = answeredOnce.toSet()
        reset()
        answeredOnce += answered
        controller.seedAnswers(answers)
    }

    fun close() {
        observeJob?.cancel()
        controller.close()
    }

    private fun applyLoaded() {
        val response = loaded ?: return
        controller.applyCard(
            version = response.sopVersionId?.hashCode() ?: 0,
            instruction = response.card.instruction,
            proofs = response.card.proofs,
            questions = response.card.questions,
            rank = SopSlotController.CARD_RANK_CACHED,
            source = "room",
            allowNoProofs = true,
        )
    }

    private fun props(slotKey: String?, action: String): Map<String, String> = buildMap {
        put(AnalyticsEvents.Params.KIND, kind)
        put("action", action)
        slotKey?.let { put("slot_key", it) }
        loaded?.sopVersionId?.takeIf { it.isNotBlank() }?.let { put("sop_version_id", it) }
    }

    private fun newController(): SopSlotController {
        val groupKey = group.current()
        return SopSlotController(
            scope = scope,
            savedStateHandle = savedStateHandle,
            syncRepository = syncRepository,
            proofCaptureSource = proofCaptureSource,
            photoCaptureSource = photoCaptureSource,
            proofCaptureRepository = proofCaptureRepository,
            analytics = analytics,
            crashReporter = crashReporter,
            stageKey = stageKey,
            groupKey = groupKey,
            durableDrafts = captureDrafts,
            durableFlowKey = "counts_$kind",
            shedId = "",
            evidenceIdentity = ProofIdentity(flow = ProofFlow.GENERIC_SUBMIT, taskId = groupKey),
            proofPolicy = { source ->
                ProofPolicy.Default.copy(
                    proofMode = if (proofSubject == ProofSubject.SHED) "shed_level_video" else "per_goat_video",
                    subjectScope = proofSubject.wireValue,
                    expectedSubjects = listOf(proofSubject.wireValue),
                    captureSource = source,
                    maximumCountPerField = 1,
                )
            },
            caption = { title -> title },
            videoContext = { title -> ProofCaptureContext(title = title, primaryTag = title, prompt = prompt) },
            photoContext = { slot -> sopSlotPhotoContext(title = slot.title, hint = slot.hint, prompt = prompt) },
            events = SopSlotController.Events(
                captureTapped = AnalyticsEvents.COUNTS_CAPTURE_TAPPED,
                captured = AnalyticsEvents.COUNTS_CAPTURE_SLOT_CAPTURED,
                uploadSynced = AnalyticsEvents.COUNTS_CAPTURE_UPLOAD_SYNCED,
                failure = AnalyticsEvents.COUNTS_CAPTURE_FAILURE,
                reuploadTapped = AnalyticsEvents.COUNTS_CAPTURE_RETAKE_TAPPED,
                cardApplied = AnalyticsEvents.COUNTS_CAPTURE_CARD_APPLIED,
            ),
            baseProps = ::props,
            locked = locked,
            onChanged = onChanged,
            proofSubject = proofSubject,
            proofScopeType = proofScopeType,
            subjectIdProvider = subjectId,
        )
    }
}
