package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonPrimitive
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsHrms
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.datetime.GoatOsDates
import sg.mesha.goatos.core.data.DisciplineRepository
import sg.mesha.goatos.core.data.HrmsAccessDenied
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.EnquiryDetailDto
import sg.mesha.goatos.core.network.dto.EnquiryDto
import sg.mesha.goatos.core.network.dto.EnquiryPenaltyDto
import sg.mesha.goatos.core.network.dto.RecordViolationRequestDto
import sg.mesha.goatos.core.network.dto.SubmitEnquiryRequestDto
import sg.mesha.goatos.core.network.dto.ViolationDto
import sg.mesha.goatos.core.network.dto.ViolationPersonOptionDto
import sg.mesha.goatos.core.network.dto.ViolationTypeOptionDto
import sg.mesha.goatos.feature.penvisits.ChoiceUi
import sg.mesha.goatos.feature.penvisits.EnquiryCardUi
import sg.mesha.goatos.feature.penvisits.EnquiryQuestionUi
import sg.mesha.goatos.feature.penvisits.EnquiryReportEvent
import sg.mesha.goatos.feature.penvisits.EnquiryReportUiState
import sg.mesha.goatos.feature.penvisits.ForMeHrmsUi
import sg.mesha.goatos.feature.penvisits.PenaltyDraftUi
import sg.mesha.goatos.feature.penvisits.RecordViolationEvent
import sg.mesha.goatos.feature.penvisits.RecordViolationUiState
import sg.mesha.goatos.feature.penvisits.RecordedViolationUi
import sg.mesha.goatos.feature.penvisits.ViolationRowUi
import sg.mesha.goatos.feature.penvisits.ViolationsListEvent
import sg.mesha.goatos.feature.penvisits.ViolationsListUiState
import java.time.LocalDate
import java.time.ZoneId
import java.util.UUID
import javax.inject.Inject

/**
 * HRMS on the phone (maintainer decisions 2026-09-30): a park head's open enquiries on the Tasks
 * "For me" tab, the enquiry report, and recording a violation. WHO sees which part is the server's
 * answer -- a 403 hides a part -- never a role string here. Writes go through the durable outbox
 * under keys minted once and persisted in [SavedStateHandle], so a retry replays.
 */

/** Messages the screens show for local validation, resolved from resources by the host. */
enum class HrmsLocalError { ANSWER_REQUIRED, PENALTY_INCOMPLETE, PENALTY_TWICE }

internal fun EnquiryDto.toCardUi(): EnquiryCardUi = EnquiryCardUi(
    listKey = "$enquiryId:$rowVersion",
    enquiryId = enquiryId,
    title = title,
    subjectLabel = subjectLabel,
    parkLabel = parkLabel,
    dueLabel = dueAtLabel,
    statusLabel = statusLabel,
    overdue = overdue,
)

internal fun ViolationPersonOptionDto.toChoice(): ChoiceUi =
    ChoiceUi(key = personId, label = name, detail = listOf(designation, parkLabel).filter { it.isNotBlank() }.joinToString(" · "))

// A type carries no money (maintainer, 2026-09-30): its choice shows the mistake only.
internal fun ViolationTypeOptionDto.toChoice(): ChoiceUi = ChoiceUi(key = key, label = title)

/** The HRMS part of the For me tab. */
@HiltViewModel
class ForMeHrmsViewModel @Inject constructor(
    private val repo: DisciplineRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    private val _state = MutableStateFlow(ForMeHrmsUi())
    val state: StateFlow<ForMeHrmsUi> = _state.asStateFlow()
    private var loading: Job? = null

    fun refresh() {
        if (loading?.isActive == true) return
        loading = viewModelScope.launch {
            repo.fetchOpenEnquiries()
                .onSuccess { page ->
                    _state.update { it.copy(showEnquiries = true, enquiries = page.items.map { e -> e.toCardUi() }) }
                    analytics.track(AnalyticsEventsHrms.ENQUIRIES_VIEWED, mapOf(AnalyticsEvents.Params.COUNT to page.items.size.toString()))
                }
                .onFailure { failure -> onReadFailure(failure, "enquiries") { _state.update { it.copy(showEnquiries = false, enquiries = emptyList()) } } }
            repo.fetchViolations()
                .onSuccess { _state.update { it.copy(showViolations = true) } }
                .onFailure { failure -> onReadFailure(failure, "violations") { _state.update { it.copy(showViolations = false) } } }
        }
    }

    /** The pen-visit pager was refused (403): the tab shows the HRMS part alone. */
    fun onPenVisitsDenied() {
        _state.update { it.copy(penVisitsDenied = true) }
    }

    private fun onReadFailure(failure: Throwable, what: String, onDenied: () -> Unit) {
        if (failure is HrmsAccessDenied) {
            onDenied()
            return
        }
        // Offline with nothing cached: keep whatever is on screen; the next resume retries.
        crashReporter.recordException(failure, "hrms $what read failed")
        analytics.track(AnalyticsEventsHrms.FAILURE, mapOf(AnalyticsEvents.Params.REASON to (failure.message ?: what).take(MAX_REASON)))
    }
}

/** The enquiry report (route `/pen-visits/enquiries/{enquiry_id}`). */
@HiltViewModel
class EnquiryReportViewModel @Inject constructor(
    private val repo: DisciplineRepository,
    private val syncRepository: SyncRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    private val savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val enquiryId: String = savedStateHandle.get<String>(ARG_ENQUIRY_ID).orEmpty()
    private val _state = MutableStateFlow(EnquiryReportUiState())
    val state: StateFlow<EnquiryReportUiState> = _state.asStateFlow()
    private val _localError = MutableStateFlow<HrmsLocalError?>(null)

    /** A validation problem the host renders from resources; null when none. */
    val localError: StateFlow<HrmsLocalError?> = _localError.asStateFlow()
    private var rowVersion = 0
    private var watch: Job? = null

    init {
        analytics.track(AnalyticsEventsHrms.ENQUIRY_OPENED, mapOf("enquiry_id" to enquiryId))
        load()
        savedStateHandle.get<String>(KEY_OUTBOX_ITEM)?.let { watchOutbox(it) }
    }

    fun onEvent(event: EnquiryReportEvent) {
        if (event !is EnquiryReportEvent.Submit && event !is EnquiryReportEvent.Retry) _localError.value = null
        when (event) {
            EnquiryReportEvent.Back -> Unit
            EnquiryReportEvent.Retry -> load()
            is EnquiryReportEvent.AnswerText -> editQuestion(event.questionId) { it.copy(text = event.value) }
            is EnquiryReportEvent.AnswerYesNo -> editQuestion(event.questionId) { it.copy(yes = event.value) }
            EnquiryReportEvent.AddPerson -> _state.update { it.copy(penalties = it.penalties + PenaltyDraftUi(), message = "") }
            is EnquiryReportEvent.RemovePerson -> _state.update { s -> s.copy(penalties = s.penalties.filterIndexed { i, _ -> i != event.index }, message = "") }
            is EnquiryReportEvent.SetPerson -> editPenalty(event.index) { it.copy(personId = event.personId) }
            // The violation and its fine are separate choices: picking one never fills the other.
            is EnquiryReportEvent.SetType -> editPenalty(event.index) { it.copy(typeKey = event.typeKey) }
            is EnquiryReportEvent.SetFine -> editPenalty(event.index) { it.copy(fine = event.value) }
            is EnquiryReportEvent.SetNote -> editPenalty(event.index) { it.copy(note = event.value) }
            EnquiryReportEvent.Submit -> submit()
        }
    }

    private fun editQuestion(id: String, change: (EnquiryQuestionUi) -> EnquiryQuestionUi) =
        _state.update { s -> s.copy(questions = s.questions.map { if (it.id == id) change(it) else it }, message = "") }

    private fun editPenalty(index: Int, change: (PenaltyDraftUi) -> PenaltyDraftUi) =
        _state.update { s -> s.copy(penalties = s.penalties.mapIndexed { i, p -> if (i == index) change(p) else p }, message = "") }

    private fun load() {
        _state.update { it.copy(loading = it.title.isBlank(), unavailable = false) }
        viewModelScope.launch {
            repo.fetchEnquiry(enquiryId)
                .onSuccess { apply(it) }
                .onFailure { failure ->
                    crashReporter.recordException(failure, "hrms enquiry read failed")
                    analytics.track(AnalyticsEventsHrms.FAILURE, mapOf(AnalyticsEvents.Params.REASON to (failure.message ?: "enquiry").take(MAX_REASON)))
                    _state.update { it.copy(loading = false, unavailable = it.title.isBlank()) }
                }
        }
    }

    private fun apply(dto: EnquiryDetailDto) {
        val e = dto.enquiry
        rowVersion = e.rowVersion
        val submitted = !dto.canSubmit
        _state.update { previous ->
            val drafts = previous.questions.associateBy { it.id }
            EnquiryReportUiState(
                loading = false,
                title = e.title,
                subjectLabel = e.subjectLabel,
                parkLabel = e.parkLabel,
                happenedLabel = e.occurredAtLabel,
                openedLabel = e.openedAtLabel,
                dueLabel = e.dueAtLabel,
                statusLabel = e.statusLabel,
                overdue = e.overdue,
                submitted = submitted,
                submittedLine = if (e.status == "submitted") listOf(e.submittedByName, e.submittedAtLabel).filter { it.isNotBlank() }.joinToString(" · ") else "",
                questions = dto.questions.map { q ->
                    val stored = dto.answers[q.id]?.jsonPrimitive
                    val draft = drafts[q.id]
                    EnquiryQuestionUi(
                        id = q.id,
                        kind = q.kind,
                        title = q.title,
                        required = q.required,
                        // A submitted report shows the server's answers; an open one keeps the draft.
                        text = if (submitted) stored?.takeIf { it.isString }?.contentOrNull.orEmpty() else draft?.text.orEmpty(),
                        yes = if (submitted) stored?.booleanOrNull else draft?.yes,
                    )
                },
                recorded = dto.violations.map {
                    RecordedViolationUi(listKey = it.violationId, personName = it.personName, typeLabel = it.typeLabel, fineLabel = it.fineLabel, note = it.note)
                },
                people = dto.people.map { it.toChoice() },
                types = dto.types.map { it.toChoice() },
                penalties = if (submitted) emptyList() else previous.penalties,
                submitting = previous.submitting && !submitted,
                queuedOffline = previous.queuedOffline && !submitted,
                // A refusal re-reads the enquiry; the server's words must survive that re-read.
                message = if (submitted) "" else previous.message,
            )
        }
    }

    private fun submit() {
        val s = _state.value
        if (s.submitting || s.submitted) return
        val problem = when {
            s.questions.any { q -> q.required && (if (q.kind == "yes_no") q.yes == null else q.text.isBlank()) } -> HrmsLocalError.ANSWER_REQUIRED
            s.penalties.any { it.personId.isBlank() || it.typeKey.isBlank() } -> HrmsLocalError.PENALTY_INCOMPLETE
            s.penalties.map { it.personId }.toSet().size != s.penalties.size -> HrmsLocalError.PENALTY_TWICE
            else -> null
        }
        if (problem != null) {
            _localError.value = problem
            return
        }
        val answers = JsonObject(
            s.questions.mapNotNull { q ->
                when {
                    q.kind == "yes_no" -> q.yes?.let { q.id to JsonPrimitive(it) }
                    q.text.isNotBlank() -> q.id to JsonPrimitive(q.text.trim())
                    else -> null
                }
            }.toMap(),
        )
        val request = SubmitEnquiryRequestDto(
            answers = answers,
            penalties = s.penalties.map { EnquiryPenaltyDto(personId = it.personId, typeKey = it.typeKey, fineRupees = it.fine.toIntOrNull(), note = it.note.trim()) },
            rowVersion = rowVersion,
        )
        _state.update { it.copy(submitting = true, message = "") }
        viewModelScope.launch {
            when (val result = repo.submitEnquiry(enquiryId, submissionKey(), request)) {
                is AppResult.Ok -> {
                    analytics.track(AnalyticsEventsHrms.ENQUIRY_SUBMITTED, mapOf(AnalyticsEvents.Params.COUNT to request.penalties.size.toString()))
                    savedStateHandle[KEY_OUTBOX_ITEM] = result.value
                    watchOutbox(result.value)
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "hrms enquiry enqueue failed") }
                    analytics.track(AnalyticsEventsHrms.FAILURE, mapOf(AnalyticsEvents.Params.REASON to result.message.take(MAX_REASON)))
                    _state.update { it.copy(submitting = false, message = result.message) }
                }
            }
        }
    }

    /**
     * Hold the form while an ONLINE drain answers, so a refusal is read HERE. Past the grace the
     * report is durable on the outbox: say so, and keep listening so it flips once it lands.
     */
    private fun watchOutbox(itemId: String) {
        watch?.cancel()
        watch = viewModelScope.launch {
            val settled = withTimeoutOrNull(DRAIN_GRACE_MS) { settle(itemId) }
            val final = settled ?: run {
                _state.update { it.copy(submitting = false, queuedOffline = true) }
                settle(itemId)
            }
            savedStateHandle.remove<String>(KEY_OUTBOX_ITEM)
            if (final.status == SyncItemStatus.SUCCEEDED) {
                savedStateHandle.remove<String>(KEY_IDEMPOTENCY)
                repo.fetchEnquiry(enquiryId).onSuccess { apply(it) }
            } else {
                // The key is spent; the next attempt mints a fresh one.
                savedStateHandle.remove<String>(KEY_IDEMPOTENCY)
                analytics.track(AnalyticsEventsHrms.FAILURE, mapOf(AnalyticsEvents.Params.REASON to (final.lastErrorCode ?: final.lastError ?: "rejected").take(MAX_REASON)))
                _state.update { it.copy(submitting = false, queuedOffline = false, message = final.lastError.orEmpty()) }
                // A report refused because the enquiry moved on: re-read the server truth.
                repo.fetchEnquiry(enquiryId).onSuccess { apply(it) }
            }
        }
    }

    private suspend fun settle(itemId: String): SyncQueueItem =
        syncRepository.observeItem(itemId).filterNotNull().first { it.status == SyncItemStatus.SUCCEEDED || it.isTerminalFailure }

    private fun submissionKey(): String {
        savedStateHandle.get<String>(KEY_IDEMPOTENCY)?.let { return it }
        val minted = "enquiry-submit:$enquiryId:${UUID.randomUUID()}"
        savedStateHandle[KEY_IDEMPOTENCY] = minted
        return minted
    }

    companion object {
        const val ARG_ENQUIRY_ID = "enquiry_id"
        private const val KEY_IDEMPOTENCY = "enquiry.idempotencyKey"
        private const val KEY_OUTBOX_ITEM = "enquiry.outboxItemId"
    }
}

/** Record a violation (route `/pen-visits/violations/new`). */
@HiltViewModel
class RecordViolationViewModel @Inject constructor(
    private val repo: DisciplineRepository,
    private val syncRepository: SyncRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    private val savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val _state = MutableStateFlow(RecordViolationUiState(day = today().toString(), days = days()))
    val state: StateFlow<RecordViolationUiState> = _state.asStateFlow()
    private var watch: Job? = null

    init {
        analytics.track(AnalyticsEventsHrms.VIOLATION_FORM_OPENED)
        load()
        savedStateHandle.get<String>(KEY_OUTBOX_ITEM)?.let { watchOutbox(it) }
    }

    fun onEvent(event: RecordViolationEvent) {
        when (event) {
            RecordViolationEvent.Back -> Unit
            RecordViolationEvent.Retry -> load()
            is RecordViolationEvent.SetPerson -> _state.update { it.copy(personId = event.personId, message = "") }
            is RecordViolationEvent.SetType -> _state.update { it.copy(typeKey = event.typeKey, message = "") }
            is RecordViolationEvent.SetFine -> _state.update { it.copy(fine = event.value, message = "") }
            is RecordViolationEvent.SetDay -> _state.update { it.copy(day = event.day, message = "") }
            is RecordViolationEvent.SetNote -> _state.update { it.copy(note = event.value, message = "") }
            RecordViolationEvent.Submit -> submit()
            RecordViolationEvent.RecordAnother -> _state.update {
                it.copy(personId = "", typeKey = "", fine = "", note = "", day = today().toString(), done = false, queuedOffline = false, message = "")
            }
        }
    }

    private fun load() {
        _state.update { it.copy(loading = it.people.isEmpty() && it.types.isEmpty(), unavailable = false) }
        viewModelScope.launch {
            repo.fetchViolations()
                .onSuccess { page ->
                    _state.update { it.copy(loading = false, people = page.people.map { p -> p.toChoice() }, types = page.types.map { t -> t.toChoice() }) }
                }
                .onFailure { failure ->
                    crashReporter.recordException(failure, "hrms violation form read failed")
                    analytics.track(AnalyticsEventsHrms.FAILURE, mapOf(AnalyticsEvents.Params.REASON to (failure.message ?: "violations").take(MAX_REASON)))
                    _state.update { it.copy(loading = false, unavailable = it.people.isEmpty()) }
                }
        }
    }

    private fun submit() {
        val s = _state.value
        if (!s.canSubmit) return
        val request = RecordViolationRequestDto(
            personId = s.personId,
            typeKey = s.typeKey,
            fineRupees = s.fine.toIntOrNull(),
            occurredOn = s.day,
            note = s.note.trim(),
        )
        _state.update { it.copy(submitting = true, message = "") }
        viewModelScope.launch {
            when (val result = repo.recordViolation(submissionKey(), request)) {
                is AppResult.Ok -> {
                    analytics.track(AnalyticsEventsHrms.VIOLATION_RECORDED)
                    savedStateHandle[KEY_OUTBOX_ITEM] = result.value
                    watchOutbox(result.value)
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "hrms violation enqueue failed") }
                    analytics.track(AnalyticsEventsHrms.FAILURE, mapOf(AnalyticsEvents.Params.REASON to result.message.take(MAX_REASON)))
                    _state.update { it.copy(submitting = false, message = result.message) }
                }
            }
        }
    }

    private fun watchOutbox(itemId: String) {
        watch?.cancel()
        watch = viewModelScope.launch {
            val settled = withTimeoutOrNull(DRAIN_GRACE_MS) {
                syncRepository.observeItem(itemId).filterNotNull().first { it.status == SyncItemStatus.SUCCEEDED || it.isTerminalFailure }
            }
            savedStateHandle.remove<String>(KEY_OUTBOX_ITEM)
            savedStateHandle.remove<String>(KEY_IDEMPOTENCY)
            when {
                settled == null -> _state.update { it.copy(submitting = false, done = true, queuedOffline = true) }
                settled.status == SyncItemStatus.SUCCEEDED -> _state.update { it.copy(submitting = false, done = true, queuedOffline = false) }
                else -> {
                    analytics.track(AnalyticsEventsHrms.FAILURE, mapOf(AnalyticsEvents.Params.REASON to (settled.lastErrorCode ?: settled.lastError ?: "rejected").take(MAX_REASON)))
                    _state.update { it.copy(submitting = false, message = settled.lastError.orEmpty()) }
                }
            }
        }
    }

    private fun submissionKey(): String {
        savedStateHandle.get<String>(KEY_IDEMPOTENCY)?.let { return it }
        val minted = "violation-record:${UUID.randomUUID()}"
        savedStateHandle[KEY_IDEMPOTENCY] = minted
        return minted
    }

    private companion object {
        const val KEY_IDEMPOTENCY = "violation.idempotencyKey"
        const val KEY_OUTBOX_ITEM = "violation.outboxItemId"

        /** A violation is recorded for today or one of the six days before it. */
        const val DAYS_OFFERED = 7L

        fun today(): LocalDate = LocalDate.now(ZoneId.of("Asia/Kolkata"))

        fun days(): List<ChoiceUi> = (0 until DAYS_OFFERED).map { back ->
            val day = today().minusDays(back)
            ChoiceUi(key = day.toString(), label = GoatOsDates.weekdayDate(day))
        }
    }
}

/**
 * The park head's violations (route `/pen-visits/violations/all`): one month at a time, first page
 * Room-cached by the repository, later pages appended as the reader scrolls. Every word on a row is
 * backend copy; the month's totals are the backend's whole-month summary, never summed here.
 */
@HiltViewModel
class ViolationsListViewModel @Inject constructor(
    private val repo: DisciplineRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    private val _state = MutableStateFlow(ViolationsListUiState())
    val state: StateFlow<ViolationsListUiState> = _state.asStateFlow()
    private var month: String? = null
    private var nextCursor = ""
    private var loading: Job? = null

    init {
        analytics.track(AnalyticsEventsHrms.VIOLATIONS_VIEWED)
    }

    fun onEvent(event: ViolationsListEvent) {
        when (event) {
            ViolationsListEvent.Refresh -> load(reset = false)
            ViolationsListEvent.LoadMore -> loadMore()
            is ViolationsListEvent.SelectMonth -> if (event.key != _state.value.month) {
                month = event.key
                _state.update { it.copy(month = event.key, rows = emptyList()) }
                load(reset = true)
            }
            ViolationsListEvent.Back, ViolationsListEvent.Record -> Unit
        }
    }

    private fun load(reset: Boolean) {
        loading?.cancel()
        _state.update { it.copy(isRefreshing = true, loading = reset || (it.loading && it.rows.isEmpty())) }
        loading = viewModelScope.launch {
            repo.fetchViolations(month = month)
                .onSuccess { page ->
                    nextCursor = page.nextCursor
                    _state.update {
                        it.copy(
                            loading = false,
                            unavailable = false,
                            isRefreshing = false,
                            refreshFailed = false,
                            lastSyncedAt = System.currentTimeMillis(),
                            months = page.months.map { m -> ChoiceUi(key = m.key, label = m.label) },
                            month = page.month,
                            count = page.summary.count,
                            fineLabel = page.summary.fineLabel,
                            people = page.summary.people,
                            rows = page.items.map { v -> v.toRowUi() },
                            canRecord = page.types.isNotEmpty() && page.people.isNotEmpty(),
                        )
                    }
                }
                .onFailure { failure ->
                    crashReporter.recordException(failure, "hrms violations read failed")
                    analytics.track(AnalyticsEventsHrms.FAILURE, mapOf(AnalyticsEvents.Params.REASON to (failure.message ?: "violations").take(MAX_REASON)))
                    _state.update { it.copy(loading = false, isRefreshing = false, refreshFailed = true, unavailable = it.rows.isEmpty() && it.months.isEmpty()) }
                }
        }
    }

    private fun loadMore() {
        if (nextCursor.isBlank() || _state.value.loadingMore || loading?.isActive == true) return
        val cursor = nextCursor
        _state.update { it.copy(loadingMore = true) }
        loading = viewModelScope.launch {
            repo.fetchViolations(month = month, cursor = cursor)
                .onSuccess { page ->
                    nextCursor = page.nextCursor
                    _state.update { s ->
                        val seen = s.rows.map { it.listKey }.toSet()
                        s.copy(loadingMore = false, rows = s.rows + page.items.map { it.toRowUi() }.filter { it.listKey !in seen })
                    }
                }
                .onFailure { failure ->
                    crashReporter.recordException(failure, "hrms violations page failed")
                    _state.update { it.copy(loadingMore = false) }
                }
        }
    }
}

internal fun ViolationDto.toRowUi(): ViolationRowUi = ViolationRowUi(
    listKey = violationId,
    personName = personName,
    designation = designation,
    typeLabel = typeLabel,
    fineLabel = fineLabel,
    dateLabel = occurredOnLabel,
    sourceLabel = sourceLabel,
    recordedByName = recordedByName,
    note = note,
    statusLabel = statusLabel,
    withdrawn = status == "withdrawn",
    withdrawReason = withdrawReason,
)

private const val DRAIN_GRACE_MS = 6_000L
private const val MAX_REASON = 120
