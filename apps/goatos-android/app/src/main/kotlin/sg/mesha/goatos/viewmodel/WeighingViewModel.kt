package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.job
import kotlinx.coroutines.launch
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.capture.ProofCaptureContext
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.capture.CaptureSyncStatus
import sg.mesha.goatos.core.data.capture.ScanCaptureRepository
import sg.mesha.goatos.core.data.forms.ProofPolicy
import sg.mesha.goatos.core.data.weighing.IndividualWeighingCapture
import sg.mesha.goatos.core.data.weighing.ShedPartitionWeighingCapture
import sg.mesha.goatos.core.data.weighing.WeighingAssignment
import sg.mesha.goatos.core.data.weighing.WeighingCapabilities
import sg.mesha.goatos.core.data.weighing.WeighingPlanDraft
import sg.mesha.goatos.core.data.weighing.WeighingPlannerCatalog
import sg.mesha.goatos.core.data.weighing.WeighingRepository
import sg.mesha.goatos.core.data.weighing.WeighingRosterRowEntity
import sg.mesha.goatos.core.data.weighing.WeighingScopeState
import sg.mesha.goatos.core.data.weighing.weighingCacheAgeNotice
import sg.mesha.goatos.core.data.weighing.WEIGHING_LEADERSHIP_MAX_WINDOW
import sg.mesha.goatos.core.data.weighing.WEIGHING_LEADERSHIP_PAGE_SIZE
import sg.mesha.goatos.core.data.weighing.WeighingTask
import sg.mesha.goatos.core.data.weighing.WeighingTaskBucketCache
import sg.mesha.goatos.core.data.weighing.WeighingTaskListCache
import sg.mesha.goatos.core.data.weighing.WeighingTaskLookup
import sg.mesha.goatos.core.data.weighing.WeighingTaskShed
import sg.mesha.goatos.core.data.weighing.weighingScopeKey
import sg.mesha.goatos.feature.weighing.WeighingAssignmentUiRow
import sg.mesha.goatos.feature.weighing.WeighingDraftUiRow
import sg.mesha.goatos.feature.weighing.WeighingParkFilterUiRow
import sg.mesha.goatos.feature.weighing.WeighingProofUiRow
import sg.mesha.goatos.feature.weighing.WeighingRosterUiRow
import sg.mesha.goatos.feature.weighing.WeighingTaskDetailUiState
import sg.mesha.goatos.feature.weighing.WeighingTaskOperatorFilterUiRow
import sg.mesha.goatos.feature.weighing.WeighingTaskShedUiRow
import sg.mesha.goatos.feature.weighing.WeighingTaskUiRow
import sg.mesha.goatos.feature.weighing.WeighingTasksTab
import sg.mesha.goatos.feature.weighing.WeighingTasksUiState
import sg.mesha.goatos.feature.weighing.WeighingUiState
import sg.mesha.goatos.feature.weighing.plan.WeighingRepeatBucket
import sg.mesha.goatos.feature.weighing.plan.WeighingRepeatSeed
import sg.mesha.goatos.feature.weighing.plan.WeighingRepeatSeedStore
import sg.mesha.goatos.core.network.MAX_SCOPE_HYDRATION_ROWS
import sg.mesha.goatos.core.network.WEIGHING_SCOPE_ALL
import sg.mesha.goatos.core.network.WEIGHING_SCOPE_MINE
import sg.mesha.goatos.core.network.WEIGHING_SCOPE_OPERATORS
import sg.mesha.goatos.rfid.RfidReaderPort
import sg.mesha.goatos.rfid.RfidReaderStatus
import sg.mesha.goatos.feature.scan.ScanReaderConnection
import sg.mesha.goatos.ui.Routes
import java.time.Instant
import java.time.DayOfWeek
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.temporal.WeekFields
import java.util.Locale
import javax.inject.Inject

@HiltViewModel
class WeighingViewModel @Inject constructor(
    private val repository: WeighingRepository,
    private val bootstrapRepository: BootstrapRepository,
    private val reader: RfidReaderPort,
    private val proofCaptureRepository: ProofCaptureRepository,
    private val scanCaptureRepository: ScanCaptureRepository,
    private val proofCaptureSource: ProofCaptureSource,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    private val repeatSeedStore: WeighingRepeatSeedStore,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {
    private val campaignId = savedStateHandle.get<String>(Routes.WEIGHING_CAMPAIGN_ARG).orEmpty()
    private val workGroupId = savedStateHandle.get<String>(Routes.WEIGHING_WORK_GROUP_ARG).orEmpty()
    private val campaignShedId = savedStateHandle.get<String>(Routes.WEIGHING_CAMPAIGN_SHED_ARG).orEmpty()
    private val category = normalizeWeighingCategory(
        savedStateHandle.get<String>(Routes.WEIGHING_CATEGORY_ARG).orEmpty(),
    )
    private val tenantId = savedStateHandle.get<String>(Routes.WEIGHING_TENANT_ARG).orEmpty()
    private val expectedLocationId = savedStateHandle.get<String>(Routes.WEIGHING_EXPECTED_LOCATION_ARG).orEmpty()
    private val expectedLocationLabel = savedStateHandle.get<String>(Routes.WEIGHING_EXPECTED_LOCATION_LABEL_ARG).orEmpty()
    private val routeTitle = savedStateHandle.get<String>(Routes.EXECUTION_SCAN_TITLE_ARG).orEmpty()

    /**
     * Which weighing surface this destination renders. The route declares it, so neither the
     * screen nor the fetch has to infer the surface from the viewer's roles -- the defect that
     * showed a director every shed in every park with a live scan action.
     */
    private val surface = savedStateHandle.get<String>(Routes.WEIGHING_SURFACE_ARG)
        ?.takeIf { it.isNotBlank() }
        ?: WEIGHING_SCOPE_MINE
    private val scopeKey = listOf(campaignId, workGroupId, campaignShedId)
        .takeIf { parts -> parts.all { it.isNotBlank() } }
        ?.let { weighingScopeKey(campaignId, workGroupId, campaignShedId) }
    private val scanInput = MutableStateFlow("")
    private val weightInput = MutableStateFlow("")
    private val animalCountInput = MutableStateFlow("")
    private val animalWeightInputs = MutableStateFlow<Map<String, String>>(emptyMap())
    private val selectedRow = MutableStateFlow<WeighingRosterRowEntity?>(null)
    private val scannedRows = MutableStateFlow<List<WeighingRosterRowEntity>>(emptyList())
    private val autoProofs = MutableStateFlow<Map<String, ProofCaptureRow>>(emptyMap())
    private val proofReplacementAnimalId = MutableStateFlow<String?>(null)
    private val observedProofs = MutableStateFlow<List<ProofCaptureRow>>(emptyList())
    private val rawProofs = MutableStateFlow<List<ProofCaptureRow>>(emptyList())
    private val sessionProofIds = MutableStateFlow<Set<String>>(emptySet())
    private var currentPrincipalId: String? = null
    private val assignments = MutableStateFlow<List<WeighingAssignment>>(emptyList())
    private val assignmentsNextCursor = MutableStateFlow<String?>(null)
    private val appendingAssignments = MutableStateFlow(false)
    private val plannerMode = MutableStateFlow(false)
    private val plannerCatalog = MutableStateFlow<WeighingPlannerCatalog?>(null)

    private val selectedAssignmentParkId = MutableStateFlow<String?>(null)

    /**
     * Park chips for the assignment list are built from every park seen so far, NOT from the
     * current page. `listAssignments(parkId = ...)` re-fetches server-filtered rows, so once a
     * park is selected `assignments` collapses to that one park -- deriving the chip list (incl.
     * "All parks") from that same collapsed list left no way back (A22). This mirrors
     * [knownTaskParks] below, which already solves the identical problem for the planner tab.
     */
    private val knownAssignmentParks = MutableStateFlow<Map<String, String>>(emptyMap())

    /**
     * What the viewer may do to the ASSIGNMENT rows, as the backend states it on the same read.
     *
     * Server truth, never a role guess on the client: a Growth Director holds the monitor
     * authority AND executes their own sheds, so the oversight actions are decided by these flags
     * rather than by which screen happens to be on top.
     */
    private val assignmentCapabilities = MutableStateFlow(WeighingCapabilities())
    private val tasksLoading = MutableStateFlow(false)
    private val tasksAppending = MutableStateFlow(false)

    /**
     * How many cached tasks the list observes. Grows ONE page at a time on scroll, bounded by the
     * cache's own ceiling, and drops back to one page on a refresh so the observed window and the
     * cached rows always agree.
     */
    private val taskWindow = MutableStateFlow(WEIGHING_LEADERSHIP_PAGE_SIZE)

    /** How many cached buckets the task DETAIL observes, on the same page-at-a-time contract. */
    private val taskBucketWindow = MutableStateFlow(WEIGHING_LEADERSHIP_PAGE_SIZE)

    /** How many extra pages a tab switch may pull before the user's own scrolling takes over. */
    private var tabRefillBudget = 0

    /**
     * The single-task read behind a deep link, and the id it has already been attempted for.
     *
     * A notification opened cold pushes the detail destination with a campaign id the list has
     * never loaded. This used to be answered by walking a few keyset pages, which spent its budget
     * on appends that early-returned while the cold-start refresh was still in flight -- the
     * advertised three pages were often zero. GET /app/weighing/campaigns/{id} answers it in ONE
     * call, so there is nothing left to budget: one attempt per selected task, then the honest
     * not-found state.
     */
    private var deepLinkResolveJob: Job? = null
    private var deepLinkAttemptedTaskId: String? = null

    /**
     * What the backend said THIS viewer may do to the DEEP-LINKED task, when the list never
     * answered for it. Null means "no single-task answer", and the list read's flags stand.
     */
    private val deepLinkCapabilities = MutableStateFlow<WeighingCapabilities?>(null)
    private val tasksTab = MutableStateFlow(WeighingTasksTab.ACTIVE)

    /**
     * What the SIGNED-IN viewer may actually do to a task, as the backend states it.
     *
     * Publishing needs the planning permission and ending needs the monitoring one, and a real
     * role (growth director) holds the second without the first -- so gating these on task status
     * alone rendered a live button that came back refused.
     */
    /**
     * Park chips are built from every park seen so far, not from the current page: filtering by
     * park re-queries the server, so deriving the chip list from the filtered rows would leave the
     * user with a single chip and no way back.
     */
    private val knownTaskParks = MutableStateFlow<Map<String, String>>(emptyMap())

    /**
     * The task list AS ROOM HOLDS IT: a bounded window of cached tasks plus the WHOLE-SCOPE tab
     * tallies and capability flags the backend answered with. The screen renders this, so a failed
     * refresh leaves the cached list up instead of blanking it, and re-entry is instant.
     */
    @OptIn(ExperimentalCoroutinesApi::class)
    private val taskCache: StateFlow<WeighingTaskListCache> =
        if (scopeKey != null) {
            flowOf(WeighingTaskListCache())
        } else {
            combine(selectedAssignmentParkId, taskWindow) { parkId, window -> parkId to window }
                .flatMapLatest { (parkId, window) -> repository.observeTaskList(surface, parkId, window) }
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), WeighingTaskListCache())

    private val tasks: StateFlow<List<WeighingTask>> = taskCache
        .map { it.items }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), emptyList())

    /** Quiet staleness note: the last task-list refresh did not land. Blank when the cache is current. */
    private val tasksStale = MutableStateFlow("")

    /**
     * Which task the detail screen is showing. The detail destination is always pushed from the
     * task list and reads the list's ViewModel, so the task it needs is already cached -- there is
     * no single-task endpoint, and paging the whole list looking for one campaign would be a drain
     * loop.
     */
    private val selectedTaskId = MutableStateFlow<String?>(null)

    /**
     * The last task the cache resolved for [selectedTaskId], kept so the detail screen survives a
     * refresh that re-reads only page 1. Exactly ONE task, replaced not accumulated.
     */
    private val selectedTaskSnapshot = MutableStateFlow<WeighingTask?>(null)

    /**
     * ONE task's shed buckets AS ROOM HOLDS THEM: a bounded window of cached buckets plus the
     * WHOLE-TASK bucket count, so the header does not move while the reader scrolls. The list the
     * task read embeds is no longer what this screen pages through.
     */
    @OptIn(ExperimentalCoroutinesApi::class)
    private val taskBucketCache: StateFlow<WeighingTaskBucketCache> =
        combine(selectedTaskId, taskBucketWindow) { taskId, window -> taskId to window }
            .flatMapLatest { (taskId, window) ->
                if (taskId.isNullOrBlank()) {
                    flowOf(WeighingTaskBucketCache())
                } else {
                    repository.observeTaskBuckets(taskId, window)
                }
            }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), WeighingTaskBucketCache())
    private val message = MutableStateFlow<String?>(null)
    private val actionInFlight = MutableStateFlow(false)

    // ---- The ONE open camera, and which animal it is pointed at -----------------------------
    //
    // The camera is a single physical device the operator is holding in front of ONE animal. These
    // fields track whose video is currently being recorded so that a scan of a DIFFERENT animal,
    // arriving while that recording is still open, retargets the camera instead of being dropped.
    // Without them the animal was bound in the launched coroutine's closure and a scan of the next
    // animal was silently swallowed by the busy gate, so footage shot at the second animal was
    // saved under the FIRST animal's tag and the weight typed next landed there too.
    private var proofCaptureAnimalId: String? = null
    private var proofCaptureJob: Job? = null
    // Flips true the instant captureVideo() RETURNS a real recording for the in-flight animal.
    // Past that point the capture must NEVER be cancelled by a later scan — that would throw away
    // a finished field recording. A later scan is refused with a visible reason instead.
    private var proofCaptureVideoCaptured = false

    private val updatingWeightAnimalIds = MutableStateFlow<Set<String>>(emptySet())
    private val loadingAssignments = MutableStateFlow(false)

    // The task list and the planner catalog are two INDEPENDENT reads that both run on the planner
    // surface. They used to share one in-flight flag, so whichever started first made the other
    // return early and never refresh at all; and they shared one message slot, so a success from one
    // wiped the failure of the other off the screen. Each read now owns its own flag and its own
    // error, and the banner shows whichever error is still outstanding.
    private val loadingPlanner = MutableStateFlow(false)
    private val selectedOperatorFilter = MutableStateFlow<String?>(null)
    private val assignmentsError = MutableStateFlow<String?>(null)
    private val plannerError = MutableStateFlow<String?>(null)
    private val plannerWeek = WeighingWeek.current()

    /** One collector for the cached planner catalog; started on first planner refresh. */
    private var observePlannerJob: Job? = null
    private var parkVocabularyJob: Job? = null
    private var readerRefreshJob: Job? = null

    @OptIn(ExperimentalCoroutinesApi::class)
    private val scopeState: StateFlow<WeighingScopeState?> =
        flowOf(scopeKey).flatMapLatest { key ->
            if (key == null) {
                flowOf(null)
            } else {
                repository.observeScope(key, ROSTER_WINDOW_SIZE)
            }
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), null)

    private val weightState: StateFlow<WeighingWeightState> =
        combine(weightInput, animalCountInput, animalWeightInputs, updatingWeightAnimalIds) {
                weight,
                animalCount,
                animalWeights,
                updatingAnimalIds,
            ->
            WeighingWeightState(weight, animalCount, animalWeights, updatingAnimalIds)
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), WeighingWeightState())

    private val formState: StateFlow<WeighingFormState> =
        combine(scanInput, weightState, selectedRow, message, actionInFlight) { scan, weights, selected, currentMessage, busy ->
            WeighingFormState(
                scan,
                weights.weight,
                weights.animalCount,
                weights.animalWeights,
                weights.updatingAnimalIds,
                selected,
                currentMessage,
                busy,
            )
        }
            .let { form ->
                combine(form, proofReplacementAnimalId) { currentForm, replacementAnimalId ->
                    currentForm.copy(replacementAnimalId = replacementAnimalId)
                }
            }
            .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), WeighingFormState())

    private val rootState: StateFlow<WeighingRootState> =
        combine(assignments, selectedAssignmentParkId, appendingAssignments, knownAssignmentParks) {
                availableAssignments,
                selectedParkId,
                appending,
                knownParks,
            ->
            AssignmentParkSelection(availableAssignments, selectedParkId, appending, knownParks)
        }.let { assignmentSelection ->
            combine(
                assignmentSelection,
                loadingAssignments,
                plannerMode,
                assignmentCapabilities,
            ) { selection, loading, isPlanner, capabilities ->
                WeighingRootState(
                    assignments = selection.assignments,
                    loading = loading,
                    plannerMode = isPlanner,
                    selectedParkId = selection.selectedParkId,
                    appendingAssignments = selection.appending,
                    knownParks = selection.knownParks,
                    capabilities = capabilities,
                )
            }
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), WeighingRootState())

    private val readerConnectionState: StateFlow<ScanReaderConnection> =
        combine(reader.status, reader.devices) { readerStatus, devices ->
            readerStatus.toScanReaderConnection(devices.firstOrNull()?.name)
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), RfidReaderStatus.NOT_PAIRED.toScanReaderConnection(null))

    private val captureState: StateFlow<WeighingCaptureState> =
        combine(scannedRows, observedProofs, readerConnectionState) { scans, proofs, readerConnection ->
            WeighingCaptureState(scans, proofs, readerConnection)
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), WeighingCaptureState())

    /**
     * The planner task list state. Deliberately a SEPARATE stream from [state]: the task list is a
     * task-grain read with its own paging cursor and its own tabs, and folding it into the capture
     * screen's state machine is what produced the shed-grain task list in the first place.
     */
    val tasksState: StateFlow<WeighingTasksUiState> =
        combine(taskCache, tasksTab, knownTaskParks, selectedAssignmentParkId) {
                cached,
                tab,
                parks,
                parkId,
            ->
            val loadedTasks = cached.items
            WeighingTasksUiState(
                tab = tab,
                // WHOLE-SCOPE tallies as the backend answered them, cached beside the rows. Never
                // counted from the page on screen, or the tab numbers would move as pages land.
                activeCount = cached.activeCount,
                completedCount = cached.completedCount,
                tasks = loadedTasks.filter { it.matchesTab(tab) }.map { it.toTaskUiRow() },
                repeatCandidate = loadedTasks
                    .lastOrNull { it.matchesTab(WeighingTasksTab.ACTIVE) && it.isRepeatable() }
                    ?.toTaskUiRow()
                    ?.takeIf { tab == WeighingTasksTab.ACTIVE },
                parkFilters = parks.entries
                    .sortedBy { it.value }
                    .map { WeighingParkFilterUiRow(parkId = it.key, label = it.value, selected = it.key == parkId) },
                todayLabel = LocalDate.now(ZoneId.of(WEIGHING_BUSINESS_ZONE)).format(weighingTodayFormatter),
            )
        }.let { base ->
            combine(base, tasksLoading, tasksAppending, tasksStale, taskCache) { current, loading, appending, stale, cached ->
                // Two independent staleness signals: a refresh that failed in THIS session, and the
                // AGE of the cached answer (an offline cold start has no failure to report).
                current.copy(
                    loading = loading,
                    loadingMore = appending,
                    staleNotice = listOf(stale, weighingCacheAgeNotice(cached.cachedAt))
                        .filter { it.isNotBlank() }
                        .joinToString(" "),
                )
            }
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), WeighingTasksUiState())

    /**
     * The task the detail screen renders: the cached row when the window still holds it, otherwise
     * the last one it resolved.
     *
     * A refresh re-reads only page 1, so a task the planner opened from page 3 would otherwise
     * vanish out from under its own screen the moment that screen resumed. There is no single-task
     * read to fall back on, and paging the list hunting for one campaign would be a drain loop.
     */
    private val activeTask: StateFlow<WeighingTask?> =
        combine(tasks, selectedTaskId, selectedTaskSnapshot) { loadedTasks, taskId, snapshot ->
            val id = taskId ?: return@combine null
            loadedTasks.firstOrNull { it.campaignId == id } ?: snapshot?.takeIf { it.campaignId == id }
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), null)

    /**
     * The task DETAIL state: one task, its buckets grouped by the operator who owns them.
     *
     * Operator display names come from the planner catalog. An id the catalog does not know is
     * never rendered raw and never given an invented name.
     */
    val taskDetailState: StateFlow<WeighingTaskDetailUiState> =
        combine(
            activeTask,
            selectedTaskId,
            combine(plannerCatalog, deepLinkCapabilities) { catalog, caps -> catalog to caps },
            tasksLoading,
            actionInFlight,
        ) { task, taskId, (catalog, deepLinkCaps), loading, busy ->
            val operatorNames = catalog?.operators.orEmpty()
                .filter { it.userId.isNotBlank() && it.displayName.isNotBlank() }
                .associate { it.userId to it.displayName }
            TaskDetailInputs(task, taskId.orEmpty(), operatorNames, loading, busy, deepLinkCaps)
        }.let { base ->
            combine(base, taskCache, taskBucketCache, tasksStale, selectedOperatorFilter) { inputs, listCache, buckets, stale, operatorFilter ->
                inputs.task.toTaskDetailUiState(
                    campaignId = inputs.campaignId,
                    selectedOperatorId = operatorFilter,
                    operatorNames = inputs.operatorNames,
                    // The single-task read's own answer wins when the LIST never answered for
                    // this task: a deep link opened cold has no list page behind it, and an
                    // all-false default would hide actions the viewer actually holds.
                    capabilities = inputs.deepLinkCapabilities ?: listCache.capabilities,
                    buckets = buckets,
                    loading = inputs.loading,
                    busy = inputs.busy,
                    // Refresh failure in this session PLUS the age of the cached answer.
                    staleNotice = listOf(stale, weighingCacheAgeNotice(buckets.cachedAt))
                        .filter { it.isNotBlank() }
                        .joinToString(" "),
                )
            }
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), WeighingTaskDetailUiState())

    init {
        // Remember the task the cache just resolved for the detail screen. Exactly ONE task is
        // held, replaced each time the cache answers, so a page-1 refresh cannot blank a detail
        // screen opened from a deeper page.
        viewModelScope.launch {
            combine(tasks, selectedTaskId) { loadedTasks, taskId ->
                taskId?.let { id -> loadedTasks.firstOrNull { it.campaignId == id } }
            }.collect { resolved ->
                if (resolved != null) selectedTaskSnapshot.value = resolved else resolveDeepLinkedTask()
            }
        }
    }

    /**
     * Resolves a deep-linked task the cached list does not hold, with ONE single-task read.
     *
     * Exactly one attempt per selected task: a 404 is the backend's final answer (not yours, not
     * there -- it does not say which, and neither does this), so retrying it would only repeat a
     * refusal. A transport failure leaves the quiet stale notice up and the cached list on screen.
     */
    private fun resolveDeepLinkedTask() {
        val id = selectedTaskId.value?.takeIf { it.isNotBlank() } ?: return
        if (selectedTaskSnapshot.value?.campaignId == id) return
        if (tasks.value.any { it.campaignId == id }) return
        if (deepLinkAttemptedTaskId == id) return
        if (deepLinkResolveJob?.isActive == true) return
        deepLinkAttemptedTaskId = id
        deepLinkResolveJob = viewModelScope.launch {
            when (val resolved = repository.getTask(id)) {
                is AppResult.Ok -> {
                    // The screen may have moved on while the read was in flight.
                    if (selectedTaskId.value != id) return@launch
                    when (val lookup = resolved.value) {
                        is WeighingTaskLookup.Found -> {
                            selectedTaskSnapshot.value = lookup.task
                            deepLinkCapabilities.value = lookup.capabilities
                        }
                        // found = false is the truth, not a loading artefact.
                        WeighingTaskLookup.NotFound -> Unit
                    }
                }
                is AppResult.Err -> tasksStale.value = STALE_NOTICE_PREFIX + resolved.message
            }
        }
    }

    /** The operator chip the detail screen is filtered by, or null for all operators. */
    fun selectTaskOperator(operatorId: String?) {
        selectedOperatorFilter.value = operatorId?.takeIf { it.isNotBlank() }
    }

    /** Names the task the detail screen is on. Safe to call on every recomposition. */
    fun selectTask(campaignId: String) {
        val normalized = campaignId.takeIf { it.isNotBlank() }
        if (selectedTaskId.value == normalized) return
        // Cleared BEFORE the id is published: writing selectedTaskId resumes the collector in
        // `init` synchronously, which resolves the new id straight away -- clearing afterwards
        // wiped the attempt marker it had just set and issued the single-task read twice.
        selectedTaskSnapshot.value = null
        deepLinkCapabilities.value = null
        deepLinkAttemptedTaskId = null
        deepLinkResolveJob?.cancel()
        selectedTaskId.value = normalized
        taskBucketWindow.value = WEIGHING_LEADERSHIP_PAGE_SIZE
        refreshTaskBuckets(reset = true)
        resolveDeepLinkedTask()
    }

    /**
     * Fetches ONE page of the selected task's buckets into Room. The cached buckets stay on screen
     * while it runs and stay on screen if it fails.
     */
    private fun refreshTaskBuckets(reset: Boolean) {
        val campaignId = selectedTaskId.value?.takeIf { it.isNotBlank() } ?: return
        viewModelScope.launch {
            when (val loaded = repository.refreshTaskBuckets(campaignId, reset = reset)) {
                is AppResult.Ok -> tasksStale.value = ""
                is AppResult.Err -> tasksStale.value = STALE_NOTICE_PREFIX + loaded.message
            }
        }
    }

    /**
     * Scroll-driven prefetch for the task detail's bucket list: one page per trigger, tail window
     * only, and it grows the observed Room window with the network page.
     */
    fun onTaskBucketRowVisible(index: Int) {
        val loaded = taskBucketCache.value.items.size
        if (loaded == 0 || index < loaded - LIST_PREFETCH_DISTANCE) return
        if (taskBucketWindow.value < WEIGHING_LEADERSHIP_MAX_WINDOW) {
            taskBucketWindow.value =
                (taskBucketWindow.value + WEIGHING_LEADERSHIP_PAGE_SIZE).coerceAtMost(WEIGHING_LEADERSHIP_MAX_WINDOW)
        }
        if (!taskBucketCache.value.canLoadMore) return
        refreshTaskBuckets(reset = false)
    }

    /**
     * Publishes the task the detail screen is on, when it is still a draft.
     *
     * This is the SAME publish call the authoring wizard runs; a draft left behind at step 5 is
     * otherwise unreachable, because nothing else in the app can move it out of draft.
     */
    fun publishTask() {
        val task = selectedTask() ?: return
        if (actionInFlight.value) return
        if (!task.status.equalsWeighingStatus("draft")) return
        if (!taskCache.value.capabilities.canPublish) return
        actionInFlight.value = true
        viewModelScope.launch {
            try {
                when (val published = repository.publishCampaign(task.campaignId)) {
                    is AppResult.Ok -> {
                        message.value = "Published for ${task.weighDateLabel()}."

                        refreshTasks()
                    }
                    is AppResult.Err -> message.value = published.message
                }
            } finally {
                actionInFlight.value = false
            }
        }
    }

    /**
     * Ends the whole task and every bucket still open under it.
     *
     * There is ONE write here, not two: ending a task whose buckets were all accepted and ending
     * one that still has open work are the same server operation, and the reason recorded is what
     * distinguishes them. The screen names the button for the case it is in; it does not invent a
     * second endpoint to match the two labels.
     */
    fun closeTask() {
        val task = selectedTask() ?: return
        if (actionInFlight.value) return
        if (!taskCache.value.capabilities.canEnd) return
        val normalized = task.status.trim().lowercase()
        if (normalized == "draft" || normalized == "closed" || normalized == "completed") return
        val openBuckets = task.sheds.count { !it.status.equalsWeighingStatus("closed") }
        // A CODE, not a sentence. The reason is kept forever on the task, so the backend authors
        // the wording; the phone only says which of the two cases this is.
        val reason = if (openBuckets == 0) CLOSE_REASON_ALL_ACCEPTED else CLOSE_REASON_OPEN_BUCKETS
        actionInFlight.value = true
        viewModelScope.launch {
            try {
                when (val closed = repository.closeCampaign(task.campaignId, reason)) {
                    is AppResult.Ok -> {
                        message.value = if (openBuckets == 0) {
                            "Task complete · every shed bucket accepted."
                        } else {
                            "Task closed · $openBuckets shed ${if (openBuckets == 1) "bucket" else "buckets"} " +
                                "stayed not accepted."
                        }
                        refreshTasks()
                    }
                    is AppResult.Err -> message.value = closed.message
                }
            } finally {
                actionInFlight.value = false
            }
        }
    }

    /**
     * Hands the selected task's park, buckets, modes and operators to the authoring wizard.
     *
     * Nothing is written here and no campaign is copied: the wizard opens on its DATE step with
     * these answers prefilled, and the ordinary create-then-publish path — with the server's own
     * availability check on whichever date is chosen — decides what survives.
     *
     * Returns the source task id when a seed was staged, so the caller can navigate; null means
     * there was nothing to carry over and the caller must not pretend otherwise.
     */
    fun stageRepeatOfTask(campaignId: String): String? {
        val task = tasks.value.firstOrNull { it.campaignId == campaignId }
            ?: selectedTaskSnapshot.value?.takeIf { it.campaignId == campaignId }
            ?: return null
        val buckets = task.sheds
            .filter { it.locationId.isNotBlank() }
            .map {
                WeighingRepeatBucket(
                    locationId = it.locationId,
                    category = it.category,
                    operatorUserId = it.operatorUserId,
                )
            }
        if (task.parkId.isBlank() || buckets.isEmpty()) {
            // Nothing to carry over. Say so rather than swallowing the tap: the caller navigates
            // only on a non-null id, so a silent null is a dead button.
            message.value = REPEAT_BLOCKED_REASON
            return null
        }
        repeatSeedStore.stage(
            sourceCampaignId = campaignId,
            seed = WeighingRepeatSeed(
                parkId = task.parkId,
                parkName = task.parkName.ifBlank { task.parkId },
                sourceDateLabel = runCatching {
                    LocalDate.parse(task.weighDate, weighingIsoDateFormatter).format(weighingTodayFormatter)
                }.getOrDefault(task.weighDate),
                buckets = buckets,
            ),
        )
        return campaignId
    }

    private fun selectedTask(): WeighingTask? {
        if (scopeKey != null) return null
        return activeTask.value
    }

    val state: StateFlow<WeighingUiState> =
        combine(scopeState, formState, rootState, captureState) { scope, form, root, capture ->
            scope.toUiState(
                scan = form.scan,
                weight = form.weight,
                animalCount = form.animalCount,
                animalWeights = form.animalWeights,
                updatingAnimalIds = form.updatingAnimalIds,
                selected = form.selected,
                currentMessage = form.message,
                busy = form.busy,
                replacementAnimalId = form.replacementAnimalId,
                availableAssignments = root.assignments,
                knownParks = root.knownParks,
                capabilities = root.capabilities,
                loading = root.loading,
                appendingAssignments = root.appendingAssignments,
                isPlanner = root.plannerMode,
                selectedParkId = root.selectedParkId,
                localScans = capture.scans,
                proofs = capture.proofs,
                readerConnection = capture.readerConnection,
            )
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), WeighingUiState())

    init {
        analytics.track(
            AnalyticsEvents.WEIGHING_VIEWED,
            buildMap {
                put(AnalyticsEvents.Params.CATEGORY, category.ifBlank { "root" })
                scopeKey?.let { put(AnalyticsEvents.Params.ITEM_ID, it) }
            },
        )
        if (scopeKey != null) {
            refreshScope()
            viewModelScope.launch {
                val profile = runCatching { bootstrapRepository.operatorProfile() }.getOrNull()
                currentPrincipalId = profile?.operatorId?.takeIf { it.isNotBlank() }
                restoreLumpSumInputDraft()
            }
            viewModelScope.launch {
                scanCaptureRepository.observeScannedTags(scopeKey, WEIGHING_SCAN_FIELD_KEY).collect { scans ->
                    scannedRows.value = scans.map { unknownWeighingRow(scopeKey, it.tag, it.capturedAtMs) }
                }
            }
            viewModelScope.launch {
                reader.reads.collect { read -> matchTag(read.tag) }
            }
            viewModelScope.launch {
                proofCaptureRepository.observeProofs(scopeKey).collect { proofs ->
                    rawProofs.value = proofs
                    publishActiveProofs(scopeKey, proofs)
                }
            }
            viewModelScope.launch {
                scopeState.collect {
                    publishActiveProofs(scopeKey, rawProofs.value)
                }
            }
        } else {
            viewModelScope.launch {
                // The planner belongs to the planner SURFACE. This used to ask whether the viewer
                // had an operator profile, which is true for every seeded user -- so plannerMode was
                // never true and the whole planning UI (week strip, shed/category picking, the
                // New task CTA) was unreachable for everyone. The route already declares which
                // surface it is; use that.
                val isPlannerSurface = surface == WEIGHING_SCOPE_ALL
                plannerMode.value = isPlannerSurface
                refreshAssignments()
                if (isPlannerSurface) {
                    refreshPlanner()
                    refreshTasks()
                }
                // The oversight surface's park chips want the WHOLE park list rather than the parks
                // that happen to be on the loaded assignment pages. Its only source is the PLANNER
                // catalog, which is gated on weighing.plan -- authority a Growth Director does not
                // hold, so this read is a 403 for exactly the viewer this surface exists for. It is
                // now issued only when the server's own can_publish flag (that same permission, see
                // the weighing handler's capabilities block) says the call can succeed; everyone
                // else keeps the paged park fallback instead of a guaranteed-forbidden request.
            }
        }
    }

    fun refresh() {
        if (scopeKey == null) {
            if (plannerMode.value) {
                refreshPlanner()
                refreshTasks()
                refreshTaskBuckets(reset = true)
            } else {
                refreshAssignments()
            }
        } else {
            refreshScope()
        }
    }

    fun refreshAssignments() {
        if (scopeKey != null) return
        if (loadingAssignments.value) return
        // Unconditionally, and BEFORE the list answers. The list can refuse to answer at all until
        // a park is NAMED (park_selection_required for a multi-park viewer), so a vocabulary that
        // waited for a successful list read would be missing in precisely that case.
        refreshParkVocabulary()
        loadingAssignments.value = true
        viewModelScope.launch {
            try {
                when (val loaded = repository.listAssignments(cursor = null, scope = surface, parkId = selectedAssignmentParkId.value)) {
                    is AppResult.Ok -> {
                        assignments.value = loaded.value.items
                        assignmentsNextCursor.value = loaded.value.nextCursor
                        assignmentsError.value = null
                        assignmentCapabilities.value = loaded.value.capabilities
                        rememberAssignmentParks(loaded.value.items)
                        // Do not clear a failure the planner read is still reporting.
                        message.value = plannerError.value
                    }
                    is AppResult.Err -> {
                        assignmentsError.value = loaded.message.toWeighingReadMessage()
                        reportReadFailure(loaded.message)
                    }
                }
            } finally {
                loadingAssignments.value = false
            }
        }
    }

    /**
     * Scroll-driven prefetch: the list tells us which row it just composed, and only a row inside
     * the tail window of the loaded page asks for the next page. One page per trigger, never a
     * drain loop, and never a tappable load-more row.
     *
     * NOTE: Park filtering is now server-side, so assignments are pre-filtered by parkId and this
     * method sees only the selected park's rows. No client-side filtering needed.
     */
    fun onAssignmentRowVisible(index: Int) {
        if (scopeKey != null) return
        val loaded = assignments.value
        if (loaded.isEmpty()) return
        if (index < loaded.size - LIST_PREFETCH_DISTANCE) return
        appendAssignments()
    }

    private fun appendAssignments() {
        if (scopeKey != null) return
        val cursor = assignmentsNextCursor.value?.takeIf { it.isNotBlank() } ?: return
        if (loadingAssignments.value || appendingAssignments.value) return
        appendingAssignments.value = true
        viewModelScope.launch {
            try {
                when (val loaded = repository.listAssignments(cursor = cursor, scope = surface, parkId = selectedAssignmentParkId.value)) {
                    is AppResult.Ok -> {
                        val known = assignments.value.map { it.campaignShedId }.toSet()
                        assignments.value = assignments.value + loaded.value.items.filter { it.campaignShedId !in known }
                        assignmentsNextCursor.value = loaded.value.nextCursor?.takeIf { it.isNotBlank() && it != cursor }
                        assignmentsError.value = null
                        assignmentCapabilities.value = loaded.value.capabilities
                        rememberAssignmentParks(loaded.value.items)
                        message.value = plannerError.value
                    }
                    is AppResult.Err -> {
                        assignmentsError.value = loaded.message.toWeighingReadMessage()
                        reportReadFailure(loaded.message)
                    }
                }
            } finally {
                appendingAssignments.value = false
            }
        }
    }

    /** The planner task list, at TASK grain: one card per park per weigh date. */
    fun refreshTasks() {
        if (scopeKey != null) return
        if (tasksLoading.value) return
        refreshParkVocabulary()
        tasksLoading.value = true
        // Back to ONE page: the refresh re-reads page 1 into Room and drops the filter's stale
        // deeper pages, so the observed window must come back with it or the list would render a
        // window larger than the rows behind it. Scrolling re-earns the deeper pages, and the task
        // DETAIL is protected separately by its own snapshot rather than by keeping pages alive.
        taskWindow.value = WEIGHING_LEADERSHIP_PAGE_SIZE
        viewModelScope.launch {
            try {
                when (
                    val loaded = repository.refreshTaskList(
                        scope = surface,
                        parkId = selectedAssignmentParkId.value,
                        reset = true,
                    )
                ) {
                    is AppResult.Ok -> tasksStale.value = ""
                    // Room keeps what it had: the cached list stays on screen with a quiet note
                    // rather than being cleared or replaced by an error page.
                    is AppResult.Err -> tasksStale.value = STALE_NOTICE_PREFIX + loaded.message
                }
            } finally {
                tasksLoading.value = false
                rememberTaskParks(tasks.value)
                appendTasksIfTabUnderfilled()
            }
        }
    }

    /**
     * Scroll-driven prefetch for the task list. Same contract as [onAssignmentRowVisible]: one page
     * per trigger, tail window only, no tappable load-more row.
     */
    fun onTaskRowVisible(index: Int) {
        if (scopeKey != null) return
        val loaded = tasks.value.size
        if (loaded == 0) return
        if (index < loaded - LIST_PREFETCH_DISTANCE) return
        appendTasks()
    }

    /**
     * Widens the observed Room window by ONE page, up to the cache's ceiling.
     *
     * Paging binds BOTH layers: the network page and the window the screen renders from grow
     * together, so the over-fetch cannot move from the network into the database.
     */
    private fun growTaskWindow() {
        if (taskWindow.value >= WEIGHING_LEADERSHIP_MAX_WINDOW) return
        taskWindow.value =
            (taskWindow.value + WEIGHING_LEADERSHIP_PAGE_SIZE).coerceAtMost(WEIGHING_LEADERSHIP_MAX_WINDOW)
    }

    fun selectTaskTab(tab: WeighingTasksTab) {
        if (tasksTab.value == tab) return
        tasksTab.value = tab
        // A tab is a view over the SAME keyset, so switching to a tab whose rows all sit further
        // down the list should keep paging rather than show a false empty state -- but ONE page,
        // not a drain.
        //
        // The server page is not tab-scoped, so on a tenant whose completed tasks sit far down the
        // keyset an unbounded refill walks the entire campaign history into an in-heap accumulator,
        // page after page, back to back. That is the mobile over-fetch rule inverted. One extra
        // page per tab selection; after that the user's own scrolling drives paging, exactly as it
        // does on the active tab.
        tabRefillBudget = 1
        appendTasksIfTabUnderfilled()
    }

    fun selectTaskPark(parkId: String?) {
        val normalized = parkId?.takeIf { it.isNotBlank() }
        if (selectedAssignmentParkId.value == normalized) return
        selectedAssignmentParkId.value = normalized
        // A different park is a different keyset, and the cache keys on it -- the observed stream
        // re-points at the new filter's own rows rather than merging them behind the old park's.
        taskWindow.value = WEIGHING_LEADERSHIP_PAGE_SIZE
        refreshTasks()
    }

    private fun appendTasks() {
        if (scopeKey != null) return
        if (!taskCache.value.canLoadMore) return
        if (tasksLoading.value || tasksAppending.value) return
        tasksAppending.value = true
        growTaskWindow()
        viewModelScope.launch {
            try {
                when (
                    val loaded = repository.refreshTaskList(
                        scope = surface,
                        parkId = selectedAssignmentParkId.value,
                        reset = false,
                    )
                ) {
                    is AppResult.Ok -> tasksStale.value = ""
                    is AppResult.Err -> tasksStale.value = STALE_NOTICE_PREFIX + loaded.message
                }
            } finally {
                tasksAppending.value = false
                rememberTaskParks(tasks.value)
                appendTasksIfTabUnderfilled()
            }
        }
    }

    private fun appendTasksIfTabUnderfilled() {
        if (tabRefillBudget <= 0) return
        if (!taskCache.value.canLoadMore) return
        val visible = tasks.value.count { it.matchesTab(tasksTab.value) }
        if (visible >= LIST_PREFETCH_DISTANCE) return
        tabRefillBudget -= 1
        appendTasks()
    }

    private fun rememberAssignmentParks(loaded: List<WeighingAssignment>) {
        if (loaded.isEmpty()) return
        knownAssignmentParks.value = knownAssignmentParks.value + loaded
            .filter { it.parkId.isNotBlank() }
            .associate { it.parkId to it.parkName.ifBlank { it.parkId } }
    }

    private fun rememberTaskParks(loaded: List<WeighingTask>) {
        if (loaded.isEmpty()) return
        knownTaskParks.value = knownTaskParks.value + loaded
            .filter { it.parkId.isNotBlank() }
            .associate { it.parkId to it.parkName.ifBlank { it.parkId } }
    }

    /**
     * Reopens a shed bucket with the caller's OWN reason.
     *
     * [reason] is required: it is written to the audit trail and kept, so the phone must not
     * author it. This used to send a fixed sentence nobody wrote.
     */
    fun reopenAssignment(row: WeighingAssignmentUiRow, reason: String) {
        if (scopeKey != null || actionInFlight.value || !row.isClosed) return
        if (!assignmentCapabilities.value.canReopen) {
            message.value = "You do not have permission to reopen weighing work."
            return
        }
        val authored = reason.trim()
        if (authored.isBlank()) {
            message.value = "A reason is required to reopen weighing work."
            return
        }
        actionInFlight.value = true
        viewModelScope.launch {
            try {
                when (val reopened = repository.reopenScope(row.campaignId, row.campaignShedId, authored)) {
                    is AppResult.Ok -> {
                        message.value = "${row.label} reopened."
                        refreshAssignments()
                        refreshPlanner()
                    }
                    is AppResult.Err -> message.value = reopened.message
                }
            } finally {
                actionInFlight.value = false
            }
        }
    }

    fun closeShedCampaign(row: WeighingAssignmentUiRow, reason: String) {
        if (scopeKey != null || actionInFlight.value || row.isClosed) return
        if (!assignmentCapabilities.value.canEnd) {
            message.value = "You do not have permission to close weighing work."
            return
        }
        actionInFlight.value = true
        viewModelScope.launch {
            try {
                when (val closed = repository.closeShedCampaign(row.campaignId, row.campaignShedId, reason)) {
                    is AppResult.Ok -> {
                        message.value = "${row.label} closed."
                        refreshAssignments()
                        refreshPlanner()
                    }
                    is AppResult.Err -> message.value = closed.message
                }
            } finally {
                actionInFlight.value = false
            }
        }
    }

    /**
     * Ends a shed scope that will never be completed. Guarded by the SERVER's own capability flag
     * and refused on an already-closed bucket, so the action is only offered where it can succeed.
     */
    fun abandonAssignment(row: WeighingAssignmentUiRow, reason: String) {
        if (scopeKey != null || actionInFlight.value || row.isClosed) return
        if (!assignmentCapabilities.value.canEnd) {
            message.value = "You do not have permission to abandon weighing work."
            return
        }
        actionInFlight.value = true
        viewModelScope.launch {
            try {
                when (val abandoned = repository.abandonScope(row.campaignId, row.campaignShedId, reason)) {
                    is AppResult.Ok -> {
                        message.value = "${row.label} abandoned."
                        refreshAssignments()
                    }
                    is AppResult.Err -> message.value = abandoned.message
                }
            } finally {
                actionInFlight.value = false
            }
        }
    }

    fun closeCampaign(reason: String) {
        if (scopeKey != null || actionInFlight.value) return
        if (assignments.value.isEmpty()) {
            message.value = "No assignments to close."
            return
        }
        val campaignId = assignments.value.firstOrNull()?.campaignId ?: return
        actionInFlight.value = true
        viewModelScope.launch {
            try {
                when (val closed = repository.closeCampaign(campaignId, reason)) {
                    is AppResult.Ok -> {
                        message.value = "Campaign closed."
                        refreshAssignments()
                        refreshPlanner()
                    }
                    is AppResult.Err -> message.value = closed.message
                }
            } finally {
                actionInFlight.value = false
            }
        }
    }

    fun selectAssignmentPark(parkId: String?) {
        val normalized = parkId?.takeIf { it.isNotBlank() }
        if (selectedAssignmentParkId.value == normalized) return
        selectedAssignmentParkId.value = normalized
        // A different park is a different keyset: reset the cursor and reload page 1.
        assignmentsNextCursor.value = null
        refreshAssignments()
    }

    /**
     * The planner catalog behind the operator NAMES this surface renders.
     *
     * Rendered from Room like every other leadership read: the observed window below feeds the
     * catalog and the refresh only writes into it, so a failed refresh leaves the cached operator
     * vocabulary in place instead of stripping names off the task detail.
     */
    private fun refreshPlanner(quiet: Boolean = false) {
        if (scopeKey != null) return
        if (observePlannerJob == null) {
            observePlannerJob = viewModelScope.launch {
                repository.observePlannerCatalog(plannerWeek.startDate)
                    .collect { cached ->
                        if (!cached.hasCache && cached.catalog.parks.isEmpty()) return@collect
                        plannerCatalog.value = cached.catalog
                        rememberCatalogParks(cached.catalog)
                    }
            }
        }
        if (loadingPlanner.value) return
        loadingPlanner.value = true
        viewModelScope.launch {
            try {
                when (val loaded = repository.refreshPlannerCatalog(plannerWeek.startDate)) {
                    is AppResult.Ok -> {
                        plannerError.value = null
                        // Do not clear a failure the task-list read is still reporting.
                        if (!quiet) message.value = assignmentsError.value
                    }
                    is AppResult.Err -> {
                        // A quiet read is chip vocabulary only. Its failure must not take over the
                        // banner of a list that loaded perfectly well from its own endpoint.
                        if (!quiet) {
                            plannerError.value = loaded.message.toWeighingReadMessage()
                            reportReadFailure(loaded.message)
                        }
                    }
                }
            } finally {
                loadingPlanner.value = false
            }
        }
    }

    /**
     * The AUTHORITATIVE park vocabulary behind the chips.
     *
     * The catalog is a park-grain read with no cursor precisely so a picker can offer every park
     * (see WeighingPlannerCatalogResponseDto). Merged rather than assigned: the paged rows remain a
     * fallback for a viewer whose catalog read is unavailable.
     */
    private fun rememberCatalogParks(catalog: WeighingPlannerCatalog) {
        rememberParks(
            catalog.parks
                .filter { it.parkId.isNotBlank() }
                .associate { it.parkId to it.name.ifBlank { it.parkId } },
        )
    }

    /**
     * Loads the AUTHORITATIVE park vocabulary behind the oversight chips.
     *
     * GET /app/weighing/parks, not the planner catalog: the catalog is gated on the PLANNING
     * permission, which a Growth Director does not hold, so this used to be skipped for exactly
     * the viewer who needed it and the chips fell back to whichever parks the loaded rows happened
     * to carry. A park whose first row sits on page 3 then had no chip -- and selecting that park
     * was the only way to load its rows. That circle is what this read breaks.
     *
     * Quiet: chip vocabulary must never take the banner of a list that loaded fine.
     */
    private fun refreshParkVocabulary() {
        if (scopeKey != null) return
        if (surface != WEIGHING_SCOPE_OPERATORS && surface != WEIGHING_SCOPE_ALL) return
        if (parkVocabularyJob?.isActive == true) return
        parkVocabularyJob = viewModelScope.launch {
            when (val parks = repository.listParks()) {
                is AppResult.Ok -> rememberParks(
                    parks.value.associate { it.parkId to it.name.ifBlank { it.parkId } },
                )
                is AppResult.Err -> Unit
            }
        }
    }

    /** Merges a park vocabulary into BOTH chip rows. Never assigns: a park is never un-learned. */
    private fun rememberParks(parks: Map<String, String>) {
        if (parks.isEmpty()) return
        knownAssignmentParks.value = knownAssignmentParks.value + parks
        knownTaskParks.value = knownTaskParks.value + parks
    }

    private fun refreshScope() {
        viewModelScope.launch {
            when (val refreshed = repository.refreshScope(campaignId, workGroupId, campaignShedId, ROSTER_SYNC_MAX_ROWS)) {
                is AppResult.Ok -> {
                    if (refreshed.value == 0 && category != PER_SHED_PARTITION_CATEGORY) {
                        message.value = "No animals are assigned to this weighing scope."
                    }
                }
                is AppResult.Err -> reportReadFailure(refreshed.message)
            }
        }
    }

    fun setCaptureActive(active: Boolean) {
        reader.setCaptureEnabled(active)
        if (active) {
            reader.refreshStatus()
            if (readerRefreshJob?.isActive == true) return
            readerRefreshJob = viewModelScope.launch {
                while (true) {
                    reader.refreshStatus()
                    delay(READER_REFRESH_MS)
                }
            }
        } else {
            readerRefreshJob?.cancel()
            readerRefreshJob = null
        }
    }

    fun setCompletionKeySwallowActive(active: Boolean) {
        reader.setCompletionKeySwallowEnabled(active)
    }

    fun setWeightEntryActive(active: Boolean) {
        val captureEnabled = !active && category != PER_SHED_PARTITION_CATEGORY
        reader.setCompletionKeySwallowEnabled(captureEnabled)
        reader.setCaptureEnabled(captureEnabled)
    }

    fun onScanInputChange(value: String) {
        scanInput.value = value
    }

    fun onWeightInputChange(value: String) {
        weightInput.value = sanitizeWeighingWeightInput(value)
        saveLumpSumInputDraft()
    }

    fun onAnimalCountInputChange(value: String) {
        animalCountInput.value = sanitizeWeighingAnimalCountInput(value)
        saveLumpSumInputDraft()
    }

    fun onAnimalWeightInputChange(animalId: String, value: String) {
        if (animalId.isBlank()) return
        val filtered = sanitizeWeighingWeightInput(value)
        animalWeightInputs.value = animalWeightInputs.value + (animalId to filtered)
    }

    fun selectAnimal(animalId: String) {
        if (scopeKey == null || animalId.isBlank()) return
        val row = scopeState.value
            ?.rosterWindow
            ?.firstOrNull { it.animalId == animalId || it.id == animalId }
            ?: return
        selectedRow.value = row
        scanInput.value = row.primaryTag
        message.value = "Selected ${row.displayAnimalId}. Enter weight, then capture video."
    }

    fun submitTypedScan() {
        val tag = scanInput.value
        if (tag.isNotBlank()) matchTag(tag)
    }

    fun recordIndividual() {
        val key = scopeKey ?: return
        val row = selectedRow.value ?: return
        recordIndividualRow(key, row)
    }

    fun recordIndividual(animalId: String, rawWeight: String) {
        val key = scopeKey ?: return
        val sanitizedWeight = sanitizeWeighingWeightInput(rawWeight)
        if (parsePositiveWeighingWeight(sanitizedWeight) == null) return
        if (animalId in updatingWeightAnimalIds.value) return
        val row = scannedRows.value.firstOrNull { it.animalId == animalId }
            ?: scopeState.value?.rosterWindow?.firstOrNull { it.animalId == animalId }
            ?: unknownWeighingRow(
                key = key,
                tag = animalId,
                capturedAtMs = System.currentTimeMillis(),
            )
        animalWeightInputs.value = animalWeightInputs.value + (animalId to sanitizedWeight)
        selectedRow.value = row
        recordIndividualRow(key, row, useGlobalBusyGate = false)
    }

    fun submitIndividualScope(onSubmitted: () -> Unit) {
        if (category == PER_SHED_PARTITION_CATEGORY || actionInFlight.value) return
        val drafts = scopeState.value?.individualDrafts.orEmpty()
        val scannedIdentifiers = scannedRows.value
            .map { it.animalId }
            .distinct()
        val pairedDrafts = drafts
            .filter { draft ->
                draft.readyToSubmit &&
                    scannedIdentifiers.contains(draft.scannedIdentifier)
            }
        val submittedIdentifiers = pairedDrafts.mapNotNull { draft ->
            val proofReady = proofForAnimal(draft.scannedIdentifier)?.let {
                it.syncStatus == CaptureSyncStatus.SYNCED &&
                    !it.serverProofId.isNullOrBlank()
            } == true || !draft.serverProofId.isNullOrBlank()
            if (proofReady) {
                draft.scannedIdentifier.takeIf { it.isNotBlank() }
            } else {
                null
            }
        }.distinct()
        if (
            scannedIdentifiers.isEmpty() ||
            submittedIdentifiers.size != scannedIdentifiers.size ||
            submittedIdentifiers.size != pairedDrafts.size
        ) {
            message.value = "Every scanned RFID in this shed needs saved weight and synced video before submit."
            return
        }
        actionInFlight.value = true
        viewModelScope.launch {
            try {
                when (
                    repository.submitIndividualScope(
                        campaignId,
                        campaignShedId,
                        submittedIdentifiers,
                    )
                ) {
                    is AppResult.Ok -> onSubmitted()
                    is AppResult.Err -> message.value = "Couldn't submit this shed. Try again."
                }
            } finally {
                actionInFlight.value = false
            }
        }
    }

    private fun recordIndividualRow(
        key: String,
        row: WeighingRosterRowEntity,
        useGlobalBusyGate: Boolean = true,
    ) {
        val weightKg = parsePositiveWeighingWeight(animalWeightInputs.value[row.animalId])
            ?: parsePositiveWeighingWeight(weightInput.value)
            ?: return
        if (useGlobalBusyGate && actionInFlight.value) return
        if (row.animalId in updatingWeightAnimalIds.value) return
        analytics.track(AnalyticsEvents.WEIGHING_CAPTURE_ATTEMPT, weighingCaptureProps(INDIVIDUAL_ANIMAL_CATEGORY))
        if (useGlobalBusyGate) actionInFlight.value = true
        updatingWeightAnimalIds.value = updatingWeightAnimalIds.value + row.animalId
        viewModelScope.launch {
            try {
                when (val recorded = repository.recordIndividual(
                    IndividualWeighingCapture(
                        tenantId = row.tenantId,
                        campaignId = campaignId,
                        workGroupId = workGroupId,
                        campaignShedId = campaignShedId,
                        // The identity of this weight is the ROW's own tag, never the shared scan
                        // box. scanInput is one ViewModel-wide field that every scan and the typed-
                        // scan box overwrite; the per-row save path runs WITHOUT the global busy
                        // gate (useGlobalBusyGate = false), so nothing holds it still while this
                        // save is dispatched. Reading it here meant "scan the next animal, then
                        // save the previous row's weight" shipped that weight under the OTHER
                        // animal's tag -- and free-flow gives the backend nothing to catch it with
                        // (RecordAnimalObservation clears AnimalID and takes scanned_identifier
                        // verbatim, service.go:419-426), so the client binding IS the record.
                        // For every path that reaches here the two agree when they are correct:
                        // matchTag sets selectedRow and scanInput from the SAME scanned tag, and
                        // selectAnimal sets scanInput = row.primaryTag. Only the divergent case
                        // was ever wrong.
                        scannedIdentifier = row.primaryTag.ifBlank { row.animalId },
                        weightKg = weightKg,
                    ),
                )) {
                    is AppResult.Ok -> {
                        val proof = proofForAnimal(row.animalId)
                        if (proof != null) {
                            repository.attachIndividualProof(key, row.animalId, proof.id, proof.serverProofId)
                        } else {
                            val restoredProofCaptureId = recorded.value.proofCaptureId
                            val restoredServerProofId = recorded.value.serverProofId
                            if (!restoredProofCaptureId.isNullOrBlank() && !restoredServerProofId.isNullOrBlank()) {
                            repository.attachIndividualProof(
                                key,
                                row.animalId,
                                restoredProofCaptureId,
                                restoredServerProofId,
                            )
                            }
                        }
                            message.value = if (proof == null) {
                                "Weight saved. Video is still required."
                            } else {
                                "Weight saved. Video continues syncing in the background."
                            }
                            analytics.track(
                                AnalyticsEvents.WEIGHING_CAPTURE_SUCCESS,
                                weighingCaptureProps(INDIVIDUAL_ANIMAL_CATEGORY),
                            )
                            scanCaptureRepository.markLocalScanSynced(
                                taskId = key,
                                fieldKey = WEIGHING_SCAN_FIELD_KEY,
                                tag = row.animalId,
                            )
                            weightInput.value = ""
                            animalWeightInputs.value = animalWeightInputs.value - row.animalId
                            scanInput.value = ""
                        recorded.value
                    }
                    is AppResult.Err -> {
                        updatingWeightAnimalIds.value = updatingWeightAnimalIds.value - row.animalId
                        message.value = recorded.message
                        reportCaptureFailure(INDIVIDUAL_ANIMAL_CATEGORY, recorded.message)
                    }
                }
            } finally {
                updatingWeightAnimalIds.value = updatingWeightAnimalIds.value - row.animalId
                if (useGlobalBusyGate) actionInFlight.value = false
            }
        }
    }

    fun recordShedPartition(onSubmitted: () -> Unit = {}) {
        val key = scopeKey ?: return
        val weightKg = parsePositiveWeighingWeight(weightInput.value) ?: return
        val animalCount = parsePositiveWeighingAnimalCount(animalCountInput.value) ?: return
        val averageWeightKg = weightKg / animalCount
        val activeProofs = activeWeighingProofs(observedProofs.value, scopeState.value)
        val syncedProof = activeProofs
            .filter { it.fieldKey == SHED_PARTITION_PROOF_FIELD_KEY }
            .filter { it.subjectId == expectedLocationId }
            .filter { it.syncStatus == CaptureSyncStatus.SYNCED && !it.serverProofId.isNullOrBlank() }
            .minByOrNull { it.capturedAtMs }
        val syncedProofIds = syncedShedProofIds(activeProofs)
        if (syncedProof == null) {
            message.value = "Capture and sync at least one group video before submitting."
            return
        }
        if (actionInFlight.value) return
        if (category != PER_SHED_PARTITION_CATEGORY) {
            message.value = "This weighing scope expects animal RFID scans."
            return
        }
        if (tenantId.isBlank() || expectedLocationId.isBlank()) {
            message.value = "Shed / partition assignment is missing location details."
            return
        }
        analytics.track(AnalyticsEvents.WEIGHING_CAPTURE_ATTEMPT, weighingCaptureProps(PER_SHED_PARTITION_CATEGORY))
        actionInFlight.value = true
        viewModelScope.launch {
            try {
                val resultJson = buildJsonObject {
                    put("total_weight_kg", weightKg)
                    put("animal_count", animalCount)
                    put("average_weight_kg", averageWeightKg)
                    put("category", PER_SHED_PARTITION_CATEGORY)
                    put("expected_location_id", expectedLocationId)
                    put("expected_location_label", expectedLocationLabel.ifBlank { routeTitle })
                }.toString()
                when (val recorded = repository.recordShedPartition(
                    ShedPartitionWeighingCapture(
                        tenantId = tenantId,
                        campaignId = campaignId,
                        workGroupId = workGroupId,
                        campaignShedId = campaignShedId,
                        expectedLocationId = expectedLocationId,
                        expectedLocationLabel = expectedLocationLabel.ifBlank { routeTitle },
                        resultJson = resultJson,
                        proofArtifactIds = syncedProofIds,
                    ),
                )) {
                    is AppResult.Ok -> {
                        repository.attachShedPartitionProof(key, syncedProof.id, syncedProof.serverProofId, syncedProofIds)
                        lumpSumDrafts.remove(lumpSumDraftKey(key))
                        message.value = "Lump-sum weighing submitted."
                        analytics.track(
                            AnalyticsEvents.WEIGHING_CAPTURE_SUCCESS,
                            weighingCaptureProps(PER_SHED_PARTITION_CATEGORY),
                        )
                        onSubmitted()
                        recorded.value
                    }
                    is AppResult.Err -> {
                        message.value = recorded.message
                        reportCaptureFailure(PER_SHED_PARTITION_CATEGORY, recorded.message)
                    }
                }
            } finally {
                actionInFlight.value = false
            }
        }
    }

    private fun syncedShedProofIds(proofs: List<ProofCaptureRow>): List<String> =
        proofs
            .asSequence()
            .filter { it.fieldKey == SHED_PARTITION_PROOF_FIELD_KEY }
            .filter { it.subjectId == expectedLocationId }
            .filter { it.syncStatus == CaptureSyncStatus.SYNCED }
            .sortedBy { it.capturedAtMs }
            .mapNotNull { it.serverProofId?.takeIf(String::isNotBlank) }
            .distinct()
            .take(5)
            .toList()

    fun captureShedVideo() = captureShedVideo(replacingProofId = null)

    fun retryShedVideo(proofId: String) {
        val key = scopeKey ?: return
        if (category != PER_SHED_PARTITION_CATEGORY || actionInFlight.value) return
        actionInFlight.value = true
        viewModelScope.launch {
            try {
                when (val retried = proofCaptureRepository.retryUpload(key, proofId)) {
                    is AppResult.Ok -> message.value = "Group video retry queued."
                    is AppResult.Err -> message.value = retried.message
                }
            } finally {
                actionInFlight.value = false
            }
        }
    }

    fun removeShedVideo(proofId: String) {
        val key = scopeKey ?: return
        if (category != PER_SHED_PARTITION_CATEGORY || actionInFlight.value) return
        actionInFlight.value = true
        viewModelScope.launch {
            try {
                when (val removed = proofCaptureRepository.remove(key, proofId)) {
                    is AppResult.Ok -> message.value = "Group video removed."
                    is AppResult.Err -> message.value = removed.message
                }
            } finally {
                actionInFlight.value = false
            }
        }
    }

    fun replaceShedVideo(proofId: String) = captureShedVideo(replacingProofId = proofId)

    private fun saveLumpSumInputDraft() {
        val key = scopeKey ?: return
        if (category != PER_SHED_PARTITION_CATEGORY) return
        val weight = weightInput.value
        val animalCount = animalCountInput.value
        val draftKey = lumpSumDraftKey(key)
        if (weight.isBlank() && animalCount.isBlank()) {
            lumpSumDrafts.remove(draftKey)
        } else {
            lumpSumDrafts[draftKey] = LumpSumInputDraft(weight, animalCount)
        }
    }

    private fun restoreLumpSumInputDraft() {
        val key = scopeKey ?: return
        if (category != PER_SHED_PARTITION_CATEGORY) return
        val draft = lumpSumDrafts[lumpSumDraftKey(key)] ?: return
        if (weightInput.value.isBlank()) weightInput.value = draft.weightInput
        if (animalCountInput.value.isBlank()) animalCountInput.value = draft.animalCountInput
    }

    private fun lumpSumDraftKey(scope: String): String =
        listOf(tenantId.ifBlank { "unknown_tenant" }, currentPrincipalId ?: "unknown_principal", scope).joinToString(":")

    private fun captureShedVideo(replacingProofId: String?) {
        val key = scopeKey ?: return
        if (category != PER_SHED_PARTITION_CATEGORY || actionInFlight.value) return
        val shedProofs = activeWeighingProofs(observedProofs.value, scopeState.value)
            .filter { it.fieldKey == SHED_PARTITION_PROOF_FIELD_KEY && it.subjectId == expectedLocationId }
            .sortedBy { it.capturedAtMs }
        val existing = shedProofs.size
        val replacingIndex = replacingProofId?.let { id -> shedProofs.indexOfFirst { it.id == id } } ?: -1
        if (replacingProofId == null && existing >= MAX_SHED_GROUP_VIDEOS) {
            message.value = "Maximum 5 group videos reached."
            return
        }
        if (replacingProofId != null && replacingIndex < 0) {
            message.value = "Video proof not found."
            return
        }
        actionInFlight.value = true
        viewModelScope.launch {
            try {
                val slotNumber = if (replacingIndex >= 0) replacingIndex + 1 else existing + 1
                val captured = proofCaptureSource.captureVideo(
                    ProofCaptureContext(
                        title = "Lump-sum weighing proof",
                        primaryTag = expectedLocationLabel.ifBlank { routeTitle },
                        secondaryTag = null,
                        workLabel = "Group video $slotNumber of 5",
                    ),
                ) ?: return@launch
                val principalId = currentPrincipalId
                    ?: runCatching { bootstrapRepository.operatorProfile()?.operatorId }.getOrNull()
                        ?.takeIf { it.isNotBlank() }
                        ?.also { currentPrincipalId = it }
                    ?: return@launch
                when (
                    val proof = proofCaptureRepository.capture(
                        taskId = key,
                        fieldKey = SHED_PARTITION_PROOF_FIELD_KEY,
                        subject = ProofSubject.SHED,
                        subjectId = expectedLocationId,
                        localUri = captured.localUri,
                        mimeType = captured.mimeType,
                        caption = "Lump-sum group video $slotNumber",
                        scopeType = "shed",
                        scopeId = expectedLocationId,
                        capturedStartMs = captured.startedAtMs,
                        capturedEndMs = captured.endedAtMs,
                        capturedByPrincipalId = principalId,
                        proofPolicy = ProofPolicy.Default.copy(
                            proofMode = "shed_level_video",
                            subjectScope = "shed",
                            expectedSubjects = listOf("shed"),
                            minimumCount = 1,
                            maximumCount = MAX_SHED_GROUP_VIDEOS,
                            maximumCountPerSubject = MAX_SHED_GROUP_VIDEOS,
                        ),
                    )
                ) {
                    is AppResult.Ok -> {
                        sessionProofIds.value = sessionProofIds.value + proof.value.id
                        if (replacingProofId != null) {
                            when (val removed = proofCaptureRepository.remove(key, replacingProofId)) {
                                is AppResult.Ok -> Unit
                                is AppResult.Err -> {
                                    message.value = removed.message
                                    return@launch
                                }
                            }
                        }
                        message.value = if (replacingProofId == null) {
                            "Group video saved locally."
                        } else {
                            "Group video replaced."
                        }
                    }
                    is AppResult.Err -> message.value = proof.message
                }
            } finally {
                actionInFlight.value = false
            }
        }
    }

    private fun reportReadFailure(reason: String) {
        message.value = reason.toWeighingReadMessage()
        crashReporter.recordException(IllegalStateException(reason), "weighing refresh failed")
        analytics.track(
            AnalyticsEvents.WEIGHING_READ_FAILURE,
            buildMap {
                put(AnalyticsEvents.Params.REASON, reason.take(MAX_ANALYTICS_REASON_CHARS))
                put(AnalyticsEvents.Params.CATEGORY, category.ifBlank { "root" })
            },
        )
    }

    private fun reportCaptureFailure(category: String, reason: String) {
        crashReporter.recordException(IllegalStateException(reason), "weighing capture failed")
        analytics.track(
            AnalyticsEvents.WEIGHING_CAPTURE_FAILURE,
            weighingCaptureProps(category) + (AnalyticsEvents.Params.REASON to reason.take(MAX_ANALYTICS_REASON_CHARS)),
        )
    }

    private fun weighingCaptureProps(captureCategory: String): Map<String, String> =
        buildMap {
            put(AnalyticsEvents.Params.CATEGORY, captureCategory)
            put(AnalyticsEvents.Params.ITEM_ID, scopeKey.orEmpty())
            put(AnalyticsEvents.Params.SHED_ID, campaignShedId)
        }

    override fun onCleared() {
        readerRefreshJob?.cancel()
        reader.setCompletionKeySwallowEnabled(false)
        reader.setCaptureEnabled(false)
    }

    private fun matchTag(tag: String) {
        val key = scopeKey ?: return
        val normalizedTag = normalizeFreeFlowTag(tag)
        if (normalizedTag.isBlank()) return
        // A scan that arrives while a VIDEO is being recorded must still be handled — the operator
        // has physically moved to the next animal, and dropping the scan is what let a recording
        // land under the previous animal. Other busy work (saving, submitting, closing) still
        // holds scans off, as before.
        if (actionInFlight.value && proofCaptureAnimalId == null) return
        viewModelScope.launch {
            val existingRow = scannedRows.value.firstOrNull {
                normalizeFreeFlowTag(it.animalId) == normalizedTag ||
                    normalizeFreeFlowTag(it.displayAnimalId) == normalizedTag
            }
            if (existingRow != null && proofReplacementAnimalId.value == existingRow.animalId) {
                proofReplacementAnimalId.value = null
                message.value = null
                captureVideoForRow(key, existingRow)
                return@launch
            }
            if (existingRow != null && proofForAnimal(existingRow.animalId) == null) {
                message.value = null
                captureVideoForRow(key, existingRow)
                return@launch
            }
            val alreadyRecorded = scopeState.value?.individualDrafts.orEmpty().any { draft ->
                normalizeFreeFlowTag(draft.scannedIdentifier) == normalizedTag
            }
            if (alreadyRecorded || existingRow != null) {
                scanInput.value = normalizedTag
                message.value = "Already scanned · $normalizedTag"
                return@launch
            }
            val inserted = scanCaptureRepository.recordLocalScanIfAbsent(
                taskId = key,
                fieldKey = WEIGHING_SCAN_FIELD_KEY,
                tag = normalizedTag,
            )
            scanInput.value = normalizedTag
            if (!inserted) {
                message.value = "Already scanned · $normalizedTag"
                return@launch
            }
            val row = unknownWeighingRow(key, normalizedTag)
            selectedRow.value = row
            message.value = "RFID captured. Record video, then enter weight."
            captureVideoForRow(key, row)
        }
    }

    fun retryVideo(animalId: String) {
        val key = scopeKey ?: return
        val proof = proofForAnimal(animalId)
        if (proof?.syncStatus != CaptureSyncStatus.FAILED) return
        viewModelScope.launch {
            when (val result = proofCaptureRepository.retryUpload(key, proof.id)) {
                is AppResult.Ok -> message.value = "Video retry queued."
                is AppResult.Err -> message.value = result.message
            }
        }
    }

    fun reuploadVideo(animalId: String) {
        val row = scannedRows.value.firstOrNull { it.animalId == animalId } ?: return
        proofReplacementAnimalId.value = animalId
        message.value = null
    }

    /** Opens the video camera for [row]. Only one animal's video can be RECORDING at a time — the
     *  camera is one physical device pointed at one animal.
     *
     *  A scan of a DIFFERENT animal while the current animal's video is still being recorded (i.e.
     *  the camera has not returned a recording yet, so nothing has been written) CLOSES that
     *  window rather than dropping the scan: the still-open camera is cancelled and a fresh one
     *  opens for the newly scanned animal, and the operator is told the first animal still needs
     *  its video. Cancelling is only safe before a recording exists — once one does
     *  ([proofCaptureVideoCaptured]) the new scan is refused with a visible reason so a finished
     *  recording is never thrown away. */
    private fun captureVideoForRow(key: String, row: WeighingRosterRowEntity) {
        val strandedAnimalId = proofCaptureAnimalId
        if (strandedAnimalId != null) {
            if (strandedAnimalId == row.animalId || proofCaptureVideoCaptured) {
                // The same animal was re-scanned mid-recording, or the open capture already has a
                // finished recording being saved. Nothing safe to cancel in either case.
                message.value = "Finish the current animal's video first."
                return
            }
            proofCaptureJob?.cancel()
            proofCaptureJob = null
            proofCaptureAnimalId = null
            proofCaptureVideoCaptured = false
            actionInFlight.value = false
            message.value = "$strandedAnimalId still needs its video."
        } else if (actionInFlight.value) {
            return
        }
        actionInFlight.value = true
        proofCaptureAnimalId = row.animalId
        proofCaptureVideoCaptured = false
        // LAZY so `proofCaptureJob` is installed BEFORE the body can run: the `finally` below
        // compares job identity, and a body that completed before the assignment would compare
        // against the previous job and skip its own cleanup.
        val job = viewModelScope.launch(start = CoroutineStart.LAZY) {
            try {
                when (val proof = captureProofForRow(key, row)) {
                    is AppResult.Ok -> {
                        sessionProofIds.value = sessionProofIds.value + proof.value.id
                        autoProofs.value = autoProofs.value + (row.animalId to proof.value)
                        message.value = "Video saved for ${row.displayAnimalId}. Enter weight."
                    }
                    is AppResult.Err -> {
                        message.value = "RFID captured. Video proof is still required."
                        reportCaptureFailure(INDIVIDUAL_ANIMAL_CATEGORY, proof.message)
                    }
                }
            } finally {
                // Only the job that still OWNS the open camera may clear it. Guard on JOB
                // identity, not animal identity: an A -> B -> A rescan makes an animal-id guard
                // pass again for the DEAD first job, so that stale job would tear down the live
                // second capture of the same animal — clearing `actionInFlight` (unblocking
                // concurrent saves), orphaning the live camera, and leaving the row stuck
                // uploading. Job identity is unique per capture and cannot be aliased by a rescan.
                if (proofCaptureJob === coroutineContext.job) {
                    actionInFlight.value = false
                    proofCaptureAnimalId = null
                    proofCaptureJob = null
                    proofCaptureVideoCaptured = false
                }
            }
        }
        proofCaptureJob = job
        job.start()
    }

    private suspend fun captureProofForRow(key: String, row: WeighingRosterRowEntity): AppResult<ProofCaptureRow> {
        val captured = proofCaptureSource.captureVideo(
            ProofCaptureContext(
                title = "Weighing proof",
                primaryTag = row.displayAnimalId,
                secondaryTag = null,
                workLabel = "Weight needed",
            ),
        )
        if (captured == null) {
            return AppResult.Err("missing_video")
        }
        // A real, complete recording now exists for this animal. From here on a later scan may no
        // longer cancel this capture — see [captureVideoForRow].
        proofCaptureVideoCaptured = true
        val principalId = currentPrincipalId
            ?: runCatching { bootstrapRepository.operatorProfile()?.operatorId }.getOrNull()
                ?.takeIf { it.isNotBlank() }
                ?.also { currentPrincipalId = it }
            ?: return AppResult.Err("missing_operator")
        return proofCaptureRepository.capture(
            taskId = key,
            fieldKey = INDIVIDUAL_PROOF_FIELD_KEY,
            subject = ProofSubject.OTHER,
            subjectId = null,
            localUri = captured.localUri,
            mimeType = captured.mimeType,
            caption = row.animalId,
            scopeType = "shed",
            scopeId = row.expectedLocationId,
            capturedStartMs = captured.startedAtMs,
            capturedEndMs = captured.endedAtMs,
            capturedByPrincipalId = principalId,
            proofPolicy = ProofPolicy.Default.copy(
                proofMode = "free_flow_video",
                subjectScope = "other",
                expectedSubjects = listOf("other"),
                maximumCount = MAX_PROOFS_PER_WEIGHING_SCOPE,
                maximumCountPerSubject = MAX_PROOFS_PER_WEIGHING_SCOPE,
            ),
        )
    }

    private fun unknownWeighingRow(key: String, tag: String, capturedAtMs: Long = System.currentTimeMillis()): WeighingRosterRowEntity {
        val normalizedTag = tag.trim()
        val label = normalizedTag.ifBlank { "Unidentified animal" }
        return WeighingRosterRowEntity(
            id = "$key:$label",
            scopeKey = key,
            tenantId = tenantId,
            campaignId = campaignId,
            workGroupId = workGroupId,
            campaignShedId = campaignShedId,
            expectedLocationId = expectedLocationId.ifBlank { campaignShedId },
            expectedLocationLabel = expectedLocationLabel.ifBlank { routeTitle.ifBlank { "Assigned shed" } },
            actualLocationId = expectedLocationId.takeIf { it.isNotBlank() },
            actualLocationLabel = expectedLocationLabel.takeIf { it.isNotBlank() },
            animalId = label,
            displayAnimalId = label,
            primaryTag = label,
            secondaryTag = null,
            normalizedPrimaryTag = normalizedTag.lowercase(),
            normalizedSecondaryTag = null,
            status = "pending",
            availabilityStatus = null,
            seq = Long.MAX_VALUE,
            updatedAt = capturedAtMs,
        )
    }

    private fun WeighingScopeState?.toUiState(
        scan: String,
        weight: String,
        animalCount: String,
        animalWeights: Map<String, String>,
        updatingAnimalIds: Set<String>,
        selected: WeighingRosterRowEntity?,
        currentMessage: String?,
        busy: Boolean,
        replacementAnimalId: String?,
        availableAssignments: List<WeighingAssignment>,
        knownParks: Map<String, String>,
        capabilities: WeighingCapabilities,
        loading: Boolean,
        appendingAssignments: Boolean,
        isPlanner: Boolean,
        selectedParkId: String?,
        localScans: List<WeighingRosterRowEntity>,
        proofs: List<ProofCaptureRow>,
        readerConnection: ScanReaderConnection?,
    ): WeighingUiState {
        if (this == null && scopeKey != null) {
            return WeighingUiState(
                title = routeTitle.ifBlank { "Weighing" },
                scopeLabel = expectedLocationLabel
                    .ifBlank { routeTitle }
                    .ifBlank { if (category == PER_SHED_PARTITION_CATEGORY) "Shed / partition weighing" else "Animal weighing" },
                hasScope = true,
                scanInput = scan,
                weightInput = weight,
                animalCountInput = animalCount,
                selectedAnimalId = selected?.animalId,
                selectedAnimalLabel = selected?.displayAnimalId,
                message = currentMessage,
                actionInFlight = busy,
                loading = true,
                category = category,
                visibleRows = localScans.toUiRows(
                    proofs = proofs,
                    replacementAnimalId = replacementAnimalId,
                ),
                shedProofs = proofs.toShedProofUiRows(),
                readerConnection = readerConnection,
            )
        }
        val scope = this ?: return WeighingUiState(
            scanInput = scan,
            weightInput = weight,
            animalCountInput = animalCount,
            message = currentMessage,
            assignments = availableAssignments
                .filter { selectedParkId == null || it.parkId == selectedParkId }
                .map { it.toUiRow() },
            // Built from the authoritative park catalog merged with every park seen so far
            // (knownParks), never from the current page -- see [knownAssignmentParks]. Sourcing it
            // from loaded rows meant a park absent from page one had no chip, and selecting a park
            // collapsed this to one chip with no way back to "All parks".
            parkFilters = knownParks.toParkFilters(selectedParkId),
            // Server truth about this viewer's oversight authority, so the screen offers close /
            // reopen / abandon exactly where the write would be accepted.
            canEndWeighing = capabilities.canEnd,
            canReopenWeighing = capabilities.canReopen,
            loading = loading,
            assignmentsLoadingMore = appendingAssignments,
            category = category,
            plannerMode = isPlanner,
            readerConnection = readerConnection,
            shedProofs = proofs.toShedProofUiRows(),
        )
        val effectiveScans = (
            localScans + scope.individualDrafts.map { draft ->
                unknownWeighingRow(
                    key = scopeKey.orEmpty(),
                    tag = draft.scannedIdentifier,
                    capturedAtMs = draft.capturedAtMs,
                )
            }
        )
            .distinctBy { it.id }
            .sortedWith(
                compareByDescending<WeighingRosterRowEntity> { it.updatedAt }
                    .thenByDescending { it.animalId },
            )
        return WeighingUiState(
            title = routeTitle.ifBlank { "Weighing" },
            scopeLabel = expectedLocationLabel
                .ifBlank { routeTitle }
                .ifBlank { if (category == PER_SHED_PARTITION_CATEGORY) "Shed / partition weighing" else "Animal weighing" },
            hasScope = true,
            totalExpected = scope.totalExpected,
            selectedAnimalId = selected?.animalId,
            selectedAnimalLabel = selected?.displayAnimalId,
            scanInput = scan,
            weightInput = weight,
            animalCountInput = animalCount,
            message = currentMessage,
            actionInFlight = busy,
            category = category,
            readerConnection = readerConnection,
            shedProofs = proofs.toShedProofUiRows(),
            visibleRows = effectiveScans.toUiRows(
                animalWeights = animalWeights,
                updatingAnimalIds = updatingAnimalIds,
                busy = busy,
                drafts = scope.individualDrafts,
                proofs = proofs,
                replacementAnimalId = replacementAnimalId,
            ),
            individualDrafts = scope.individualDrafts.map { draft ->
                val animalLabel = scope.rosterWindow
                    .firstOrNull { it.animalId == draft.scannedIdentifier }
                    ?.displayAnimalId
                    ?: draft.scannedIdentifier
                WeighingDraftUiRow(
                    id = draft.observationId,
                    animalId = draft.scannedIdentifier,
                    label = "$animalLabel - ${draft.weightKg} kg",
                    proofReady = draft.proofReady,
                    readyToSubmit = draft.readyToSubmit,
                    syncedToBackend = draft.syncedToBackend,
                )
            },
            shedDrafts = scope.shedDrafts.map { draft ->
                WeighingDraftUiRow(
                    id = draft.shedObservationId,
                    label = "Shed / partition result",
                    proofReady = draft.proofReady,
                    readyToSubmit = draft.readyToSubmit,
                )
            },
        )
    }

    private fun List<WeighingRosterRowEntity>.toUiRows(
        animalWeights: Map<String, String> = emptyMap(),
        updatingAnimalIds: Set<String> = emptySet(),
        busy: Boolean = false,
        drafts: List<sg.mesha.goatos.core.data.weighing.IndividualWeighingDraft> = emptyList(),
        proofs: List<ProofCaptureRow> = emptyList(),
        replacementAnimalId: String? = null,
    ): List<WeighingRosterUiRow> =
        map { row ->
            val draft = drafts.firstOrNull { it.scannedIdentifier == row.primaryTag.ifBlank { row.animalId } }
            val savedWeight = draft?.weightKg?.toString()
            val weight = animalWeights[row.animalId] ?: savedWeight.orEmpty()
            val draftProofId = draft?.proofCaptureId?.takeIf { it.isNotBlank() }
            val proof = proofs
                .filter { it.fieldKey == INDIVIDUAL_PROOF_FIELD_KEY }
                .filter { it.matchesAnimalProof(row.animalId, draftProofId) }
                .maxByOrNull { it.capturedAtMs }
                ?: autoProofs.value[row.animalId]
            val proofStatus = when {
                proof == null && !draft?.proofCaptureId.isNullOrBlank() && draft?.syncedToBackend == true ->
                    sg.mesha.goatos.feature.scan.ProofUploadStatus.SYNCED
                else -> when (proof?.syncStatus) {
                CaptureSyncStatus.SYNCED -> sg.mesha.goatos.feature.scan.ProofUploadStatus.SYNCED
                CaptureSyncStatus.FAILED -> sg.mesha.goatos.feature.scan.ProofUploadStatus.FAILED
                CaptureSyncStatus.PENDING, CaptureSyncStatus.IN_FLIGHT -> sg.mesha.goatos.feature.scan.ProofUploadStatus.UPLOADING
                null -> sg.mesha.goatos.feature.scan.ProofUploadStatus.MISSING
                }
            }
            val weightSaved = draft != null
            WeighingRosterUiRow(
                id = row.id,
                animalId = row.animalId,
                displayAnimalId = row.displayAnimalId,
                status = if (draft?.syncedToBackend == true) "Completed" else "Scanned",
                scannedAtLabel = scanTimeLabel(row.updatedAt),
                weightInput = weight,
                savedWeightLabel = savedWeight,
                canSaveWeight = row.animalId !in updatingAnimalIds &&
                    weight.toDoubleOrNull()?.let { it > 0.0 } == true,
                weightSaved = weightSaved,
                proofCaptureId = proof?.id ?: draft?.proofCaptureId,
                proofUploadStatus = proofStatus,
                proofStatusLabel = proof?.proofStatusLabel()
                    ?: if (draft?.syncedToBackend == true) "Video synced ${timeOnlyLabel(draft.capturedAtMs)}" else null,
                backendSynced = draft?.syncedToBackend == true,
                weightUpdating = row.animalId in updatingAnimalIds,
                reuploadRequested = row.animalId == replacementAnimalId,
            )
        }

    private fun scanTimeLabel(epochMs: Long): String =
        runCatching {
            "Scanned " + WEIGHING_SCAN_TIME_FORMATTER.format(Instant.ofEpochMilli(epochMs))
        }.getOrDefault("just now")

    private fun List<ProofCaptureRow>.toShedProofUiRows(): List<WeighingProofUiRow> =
        filter {
            it.fieldKey == SHED_PARTITION_PROOF_FIELD_KEY &&
                it.subjectId == expectedLocationId
        }
            .sortedBy { it.capturedAtMs }
            .mapIndexed { index, proof ->
                WeighingProofUiRow(
                    id = proof.id,
                    label = when (proof.syncStatus) {
                        CaptureSyncStatus.PENDING, CaptureSyncStatus.IN_FLIGHT -> "uploading"
                        CaptureSyncStatus.SYNCED -> "synced"
                        CaptureSyncStatus.FAILED -> "upload failed · retry"
                    },
                    status = when (proof.syncStatus) {
                        CaptureSyncStatus.PENDING, CaptureSyncStatus.IN_FLIGHT ->
                            sg.mesha.goatos.feature.scan.ProofUploadStatus.UPLOADING
                        CaptureSyncStatus.SYNCED -> sg.mesha.goatos.feature.scan.ProofUploadStatus.SYNCED
                        CaptureSyncStatus.FAILED -> sg.mesha.goatos.feature.scan.ProofUploadStatus.FAILED
                    },
                )
            }

    private fun ProofCaptureRow.proofStatusLabel(): String = when (syncStatus) {
        CaptureSyncStatus.PENDING, CaptureSyncStatus.IN_FLIGHT ->
            "Video uploading ${timeOnlyLabel(capturedAtMs)}"
        CaptureSyncStatus.SYNCED -> "Video synced ${timeOnlyLabel(capturedAtMs)}"
        CaptureSyncStatus.FAILED -> "Upload failed ${timeOnlyLabel(capturedAtMs)}"
    }

    private fun timeOnlyLabel(epochMs: Long): String =
        WEIGHING_TIME_ONLY_FORMATTER.format(Instant.ofEpochMilli(epochMs))

    private fun activeWeighingProofs(
        proofs: List<ProofCaptureRow>,
        scope: WeighingScopeState?,
    ): List<ProofCaptureRow> {
        val activeIds = sessionProofIds.value.toMutableSet()
        scope?.individualDrafts.orEmpty()
            .mapNotNullTo(activeIds) { it.proofCaptureId?.takeIf(String::isNotBlank) }
        return proofs.filter { proof ->
            proof.syncStatus != CaptureSyncStatus.SYNCED || proof.id in activeIds
        }
    }

    private suspend fun publishActiveProofs(scope: String, proofs: List<ProofCaptureRow>) {
        val activeProofs = activeWeighingProofs(proofs, scopeState.value)
        observedProofs.value = activeProofs
        val shedProofIds = syncedShedProofIds(activeProofs)
        activeProofs.forEach { proof ->
            val serverProofId = proof.serverProofId?.takeIf { it.isNotBlank() } ?: return@forEach
            when (proof.fieldKey) {
                INDIVIDUAL_PROOF_FIELD_KEY -> {
                    val animalId = proof.caption?.takeIf { it.isNotBlank() }
                        ?: proof.subjectId?.takeIf { it.isNotBlank() }
                        ?: return@forEach
                    repository.attachIndividualProof(scope, animalId, proof.id, serverProofId)
                }
                SHED_PARTITION_PROOF_FIELD_KEY -> repository.attachShedPartitionProof(
                    scope,
                    proof.id,
                    serverProofId,
                    shedProofIds,
                )
            }
        }
    }

    private fun proofForAnimal(animalId: String): ProofCaptureRow? {
        val draftProofId = scopeState.value?.individualDrafts
            ?.firstOrNull { it.scannedIdentifier == animalId }
            ?.proofCaptureId
            ?.takeIf { it.isNotBlank() }
        return observedProofs.value
            .filter { it.fieldKey == INDIVIDUAL_PROOF_FIELD_KEY }
            .filter { it.matchesAnimalProof(animalId, draftProofId) }
            .maxByOrNull { it.capturedAtMs }
            ?: autoProofs.value[animalId]
    }

    private fun ProofCaptureRow.matchesAnimalProof(animalId: String, draftProofId: String?): Boolean =
        if (draftProofId != null) {
            id == draftProofId
        } else {
            syncStatus != CaptureSyncStatus.SYNCED && (caption == animalId || subjectId == animalId)
        }

    private fun RfidReaderStatus.toScanReaderConnection(readerName: String?): ScanReaderConnection =
        ScanReaderConnection(
            readerName = readerName ?: "RFID reader",
            statusLabel = when (this) {
                RfidReaderStatus.READY -> "Reader connected"
                RfidReaderStatus.PAIRED_NOT_READY -> "Reader disconnected"
                RfidReaderStatus.NOT_PAIRED -> "Reader not paired"
                RfidReaderStatus.PERMISSION_NEEDED -> "Bluetooth permission needed"
                RfidReaderStatus.BLUETOOTH_OFF -> "Bluetooth off"
            },
            connected = this == RfidReaderStatus.READY,
            actionLabel = "Reconnect",
        )

    private companion object {
        const val ROSTER_WINDOW_SIZE = 20
        const val LIST_PREFETCH_DISTANCE = 3
        const val ROSTER_SYNC_MAX_ROWS = MAX_SCOPE_HYDRATION_ROWS
        const val MAX_PROOFS_PER_WEIGHING_SCOPE = 100 // Free-flow scope constraint, independent of sync page size
        const val READER_REFRESH_MS = 5_000L
        const val INDIVIDUAL_PROOF_FIELD_KEY = "weighing_individual_video"
        const val WEIGHING_SCAN_FIELD_KEY = "weighing_free_flow_scan"
        const val SHED_PARTITION_PROOF_FIELD_KEY = "weighing_shed_partition_video"
        const val INDIVIDUAL_ANIMAL_CATEGORY = "individual_animal"
        const val PER_SHED_PARTITION_CATEGORY = "per_shed_partition"
        const val MAX_ANALYTICS_REASON_CHARS = 96
        const val MAX_SHED_GROUP_VIDEOS = 5
        const val DEFAULT_PLANNED_CAP_PER_DAY = 100
        val WEIGHING_SCAN_TIME_FORMATTER: DateTimeFormatter =
            DateTimeFormatter.ofPattern("d MMM, h:mm a 'IST'", Locale.ENGLISH)
                .withZone(ZoneId.of("Asia/Kolkata"))
        val WEIGHING_TIME_ONLY_FORMATTER: DateTimeFormatter =
            DateTimeFormatter.ofPattern("h:mm a 'IST'", Locale.ENGLISH)
                .withZone(ZoneId.of("Asia/Kolkata"))
    }
}

private fun normalizeWeighingCategory(raw: String): String =
    when (raw.trim().lowercase(Locale.ROOT)) {
        "per shed partition", "per-shed-partition", "per_shed_partition" -> "per_shed_partition"
        "individual animal", "individual-animal", "individual_animal" -> "individual_animal"
        else -> raw.trim()
    }

private fun WeighingAssignment.toUiRow(): WeighingAssignmentUiRow =
    WeighingAssignmentUiRow(
        campaignId = campaignId,
        tenantId = tenantId,
        parkId = parkId,
        parkLabel = parkName.ifBlank { parkId.take(8) },
        workGroupId = workGroupId,
        campaignShedId = campaignShedId,
        expectedLocationId = expectedLocationId,
        expectedLocationLabel = expectedLocationLabel,
        label = label,
        category = category,
        status = status.readableWeighingStatus(),
        periodLabel = periodLabel.readableWeighingPeriodLabel(),
        readyToClose = readyToClose,
        pendingVerificationCount = pendingVerificationCount,
    )

private fun Map<String, String>.toParkFilters(selectedParkId: String?): List<WeighingParkFilterUiRow> =
    entries
        .filter { it.key.isNotBlank() }
        .sortedBy { it.value }
        .map {
            WeighingParkFilterUiRow(
                parkId = it.key,
                label = it.value.ifBlank { it.key.take(8) },
                selected = it.key == selectedParkId,
            )
        }

private fun String.readableWeighingPeriodLabel(): String {
    val parts = split(" - ")
    if (parts.size != 2) return this
    return runCatching {
        val start = LocalDate.parse(parts[0], DateTimeFormatter.ISO_LOCAL_DATE)
        val end = LocalDate.parse(parts[1], DateTimeFormatter.ISO_LOCAL_DATE)
        val week = start.get(WeekFields.ISO.weekOfWeekBasedYear())
        val shortFormatter = DateTimeFormatter.ofPattern("d MMM", Locale.ENGLISH)
        "Week $week - ${start.format(shortFormatter)}-${end.format(shortFormatter)}"
    }.getOrDefault(this)
}

private fun String.readableWeighingStatus(): String = when (trim().lowercase()) {
    "draft" -> "Draft"
    "published" -> "Published"
    "scheduled" -> "Scheduled"
    "pending" -> "Pending"
    "in_progress" -> "In progress"
    "needs_review" -> "Needs review"
    "accepted" -> "Accepted"
    "completed" -> "Completed"
    "cancelled", "canceled" -> "Cancelled"
    else -> replace('_', ' ')
        .split(' ')
        .filter { it.isNotBlank() }
        .joinToString(" ") { part -> part.replaceFirstChar { char -> char.uppercase() } }
        .ifBlank { "Not started" }
}

private fun String.toWeighingReadMessage(): String {
    val normalized = lowercase()
    return when {
        normalized.contains("failed to connect") ||
            normalized.contains("unable to resolve host") ||
        normalized.contains("timeout") ||
        normalized.contains("timed out") ||
            normalized.contains("unexpected end of stream") ||
            normalized.contains("unexpected eof") ||
            normalized.contains("connection reset") ||
            normalized.contains("connection refused") ->
            "Couldn't load weighing. Check the laptop backend or network, then refresh."
        isBlank() -> "Couldn't load weighing. Pull to refresh or try again."
        else -> this
    }
}

private data class WeighingFormState(
    val scan: String = "",
    val weight: String = "",
    val animalCount: String = "",
    val animalWeights: Map<String, String> = emptyMap(),
    val updatingAnimalIds: Set<String> = emptySet(),
    val selected: WeighingRosterRowEntity? = null,
    val message: String? = null,
    val busy: Boolean = false,
    val replacementAnimalId: String? = null,
)

private data class WeighingWeightState(
    val weight: String = "",
    val animalCount: String = "",
    val animalWeights: Map<String, String> = emptyMap(),
    val updatingAnimalIds: Set<String> = emptySet(),
)

private data class WeighingRootState(
    val assignments: List<WeighingAssignment> = emptyList(),
    val loading: Boolean = false,
    val plannerMode: Boolean = false,
    val selectedParkId: String? = null,
    val appendingAssignments: Boolean = false,
    // Every park seen across every fetch, NOT just the current (possibly park-filtered) page --
    // see [knownAssignmentParks]. Keeps the "All parks" chip and every other park chip reachable
    // after the user selects a park (A22).
    val knownParks: Map<String, String> = emptyMap(),
    /** Backend-stated oversight authority for these rows. See [WeighingViewModel] capabilities. */
    val capabilities: WeighingCapabilities = WeighingCapabilities(),
)

private data class AssignmentParkSelection(
    val assignments: List<WeighingAssignment>,
    val selectedParkId: String?,
    val appending: Boolean = false,
    val knownParks: Map<String, String> = emptyMap(),
)

private data class WeighingCaptureState(
    val scans: List<WeighingRosterRowEntity> = emptyList(),
    val proofs: List<ProofCaptureRow> = emptyList(),
    val readerConnection: ScanReaderConnection? = null,
)

internal fun sanitizeWeighingWeightInput(value: String): String =
    value.take(8).takeIf { candidate ->
        candidate.all { it.isDigit() || it == '.' } && candidate.count { it == '.' } <= 1
    }.orEmpty()

internal fun sanitizeWeighingAnimalCountInput(value: String): String =
    value.take(6).takeIf { candidate -> candidate.all(Char::isDigit) }.orEmpty()

internal fun parsePositiveWeighingWeight(value: String?): Double? =
    value?.toDoubleOrNull()?.takeIf { it.isFinite() && it > 0.0 }

internal fun parsePositiveWeighingAnimalCount(value: String?): Int? =
    value?.toIntOrNull()?.takeIf { it > 0 }

private fun normalizeFreeFlowTag(tag: String): String =
    tag.filter { it.isLetterOrDigit() }.lowercase()

private data class LumpSumInputDraft(
    val weightInput: String,
    val animalCountInput: String,
)

private val lumpSumDrafts = mutableMapOf<String, LumpSumInputDraft>()

/**
 * The business week the planner catalog is read for.
 *
 * Only the Monday start date survives: it is the key the catalog read is cached under. The week
 * LABEL, period label and day tabs belonged to the old in-screen week strip, which the planner
 * surface replaced -- nothing renders them, so nothing computes them.
 */
private data class WeighingWeek(
    val startDate: String,
) {
    companion object {
        private val isoFormatter = DateTimeFormatter.ISO_LOCAL_DATE

        fun current(today: LocalDate = LocalDate.now(ZoneId.of("Asia/Kolkata"))): WeighingWeek =
            WeighingWeek(startDate = today.with(DayOfWeek.MONDAY).format(isoFormatter))
    }
}

private const val WEIGHING_BUSINESS_ZONE = "Asia/Kolkata"

/** Weigh dates are business DATES, so they are formatted as a day, never as a clock time. */
private val weighingTodayFormatter = DateTimeFormatter.ofPattern("EEE d MMM", Locale.ENGLISH)
private val weighingIsoDateFormatter = DateTimeFormatter.ISO_LOCAL_DATE
private val weighingMonthFormatter = DateTimeFormatter.ofPattern("MMM yyyy", Locale.ENGLISH)

/** Whole-filter tab tallies as the backend computed them. Never counted from a loaded page. */
private data class WeighingTaskTabCounts(
    val active: Int = 0,
    val completed: Int = 0,
)

/**
 * Server-owned split, mirrored exactly so the rows and the tab numbers cannot disagree:
 * a task is Completed when it is completed or closed, and Active in every other live status.
 * A canceled task belongs to neither tab.
 */
private fun WeighingTask.matchesTab(tab: WeighingTasksTab): Boolean {
    val normalized = status.trim().lowercase()
    if (normalized == "canceled" || normalized == "cancelled") return false
    val completed = normalized == "completed" || normalized == "closed"
    return if (tab == WeighingTasksTab.COMPLETED) completed else !completed
}

private fun String.equalsWeighingStatus(other: String): Boolean =
    trim().equals(other, ignoreCase = true)

/**
 * Prefix for the quiet staleness note: cached rows stay on screen and this says the last refresh
 * did not land, instead of clearing the list or throwing the reader to an error page.
 */
private const val STALE_NOTICE_PREFIX = "Showing the last saved list. "


/** Buckets shown per operator group before the "+N more" tail. Same page size as every list. */
private const val WEIGHING_GROUP_BUCKET_CAP = 20

private fun WeighingTask?.toTaskDetailUiState(
    campaignId: String,
    selectedOperatorId: String?,
    operatorNames: Map<String, String>,
    capabilities: WeighingCapabilities,
    buckets: WeighingTaskBucketCache,
    loading: Boolean,
    busy: Boolean,
    staleNotice: String,
): WeighingTaskDetailUiState {
    if (this == null) {
        return WeighingTaskDetailUiState(campaignId = campaignId, found = false, loading = loading, busy = busy)
    }
    val date = runCatching { LocalDate.parse(weighDate, weighingIsoDateFormatter) }.getOrNull()
    val normalizedStatus = status.trim().lowercase()
    // The RENDERED buckets are the cached keyset page, not the whole set the task record embeds:
    // one park can hold 76+ sheds, and the reader is shown a page at a time.
    val pagedSheds = if (buckets.hasCache) buckets.items else sheds
    // Operator is a FILTER, not a grouping. A sectioned list cannot paginate: a ~20-row keyset page
    // splits mid-operator, so a header would show part of someone's sheds with the rest arriving
    // pages later. One flat list behind a chip pages the same however many operators a task has.
    //
    // The chip LABEL is the name the bucket itself carries; the planner catalog is only a fallback
    // for a bucket read that predates that field, because resolving from the catalog alone left
    // every bucket past the catalog's first page nameless.
    fun labelFor(operatorUserId: String, rows: List<WeighingTaskShed>): String = when {
        operatorUserId.isBlank() -> "Not assigned yet"
        else -> rows.firstNotNullOfOrNull { it.operatorDisplayName.ifBlank { null } }
            ?: operatorNames[operatorUserId]
            ?: "Operator"
    }
    // Chip counts range over the WHOLE task, not the cached page, so they do not move as the
    // reader scrolls.
    //
    // The count is handed over as a NUMBER, never folded into the label here: the screen owns the
    // noun, so the unit gets named ("2 sheds") and translates with the rest of the chrome. Building
    // "Dinakar 2" in this layer left an unlabelled number on a screen whose cards count animals.
    val operatorFilters = sheds
        .groupBy { it.operatorUserId }
        .map { (operatorUserId, rows) ->
            val id = operatorUserId.ifBlank { "unassigned" }
            WeighingTaskOperatorFilterUiRow(
                id = id,
                operatorLabel = labelFor(operatorUserId, rows),
                shedCount = rows.size,
                selected = selectedOperatorId == id,
            )
        }
        .sortedBy { it.operatorLabel }
    val visibleSheds = pagedSheds
        .filter { selectedOperatorId == null || it.operatorUserId.ifBlank { "unassigned" } == selectedOperatorId }
        .map { it.toTaskShedUiRow(this, labelFor(it.operatorUserId, listOf(it))) }
    return WeighingTaskDetailUiState(
        campaignId = this.campaignId,
        found = true,
        parkName = parkName.ifBlank { parkId },
        dateLabel = date?.format(weighingTodayFormatter) ?: weighDate,
        status = status,
        statusLabel = normalizedStatus.ifBlank { "scheduled" }.replace('_', ' '),
        // WHOLE-TASK bucket count as the backend answered it, so the header does not move while
        // the reader scrolls the page. Falls back to the task record's own set before the bucket
        // page has been cached.
        bucketCount = if (buckets.hasCache) buckets.totalCount else sheds.size,
        // Whole-task facts stay on the task record, which carries every bucket; only the rendered
        // list is paged. Counting the page here would understate the task.
        operatorCount = sheds.map { it.operatorUserId }.filter { it.isNotBlank() }.distinct().size,
        isDraft = normalizedStatus == "draft",
        isClosed = normalizedStatus == "closed",
        isCompleted = normalizedStatus == "completed",
        closedReason = closeReason,
        // A bucket is settled once its work has been accepted; everything else is still open work
        // this task is carrying.
        openBucketCount = sheds.count { !it.status.equalsWeighingStatus("closed") },
        // Status is only HALF the gate: the viewer must also hold the permission the write needs.
        canPublish = normalizedStatus == "draft" && capabilities.canPublish,
        canEnd = capabilities.canEnd &&
            normalizedStatus != "draft" &&
            normalizedStatus != "closed" &&
            normalizedStatus != "completed" &&
            normalizedStatus != "canceled" &&
            normalizedStatus != "cancelled",
        canRepeat = isRepeatable(),
        repeatBlockedReason = REPEAT_BLOCKED_REASON,
        operatorFilters = operatorFilters,
        selectedOperatorId = selectedOperatorId,
        sheds = visibleSheds,
        loading = loading,
        busy = busy,
        staleNotice = staleNotice,
    )
}

private fun WeighingTaskShed.toTaskShedUiRow(task: WeighingTask, operatorLabel: String): WeighingTaskShedUiRow {
    val normalized = status.trim().lowercase()
    val reworked = reworkCount > 0
    return WeighingTaskShedUiRow(
        campaignId = task.campaignId,
        campaignShedId = campaignShedId,
        tenantId = task.tenantId,
        locationId = locationId,
        shedName = displayName.ifBlank { locationId },
        category = category,
        operatorLabel = operatorLabel,
        status = status,
        // The task payload carries bucket STATE, not a record count, so the line says what state
        // the bucket is in. It never guesses how many animals were captured.
        captureSummary = when {
            reworked -> "Sent back to the operator"
            normalized == "closed" -> "Accepted"
            normalized == "completed" -> "Submitted · waiting for verifier"
            normalized == "in_progress" -> "Capture started"
            else -> "Nothing captured yet"
        },
        // Position on the bucket's own state ladder. NOT a share of animals: weighing has no
        // expected-animal roster, so an animal denominator would be invented.
        progress = when {
            reworked -> 0.25f
            normalized == "closed" -> 1f
            normalized == "completed" -> 0.7f
            normalized == "in_progress" -> 0.35f
            else -> 0f
        },
        // Leadership can pull a bucket back once the operator has submitted it, or after it was
        // accepted or bounced for rework.
        canReopen = reworked || normalized == "completed" || normalized == "closed",
    )
}

private fun WeighingTask.toTaskUiRow(): WeighingTaskUiRow {
    val bucketCount = sheds.size
    // "Accepted" is a bucket whose evidence a verifier has cleared, or one leadership has closed.
    val accepted = sheds.count { it.readyToClose || it.status.equals("closed", ignoreCase = true) }
    val date = runCatching { LocalDate.parse(weighDate, weighingIsoDateFormatter) }.getOrNull()
    return WeighingTaskUiRow(
        campaignId = campaignId,
        parkId = parkId,
        parkName = parkName.ifBlank { parkId },
        status = status,
        dateLabel = date?.format(weighingTodayFormatter) ?: weighDate,
        monthLabel = date?.format(weighingMonthFormatter).orEmpty(),
        bucketCount = bucketCount,
        shedNames = sheds.take(2).map { it.displayName }.filter { it.isNotBlank() },
        moreShedCount = (bucketCount - 2).coerceAtLeast(0),
        individualCount = sheds.count { !it.category.equals("per_shed_partition", ignoreCase = true) },
        lumpSumCount = sheds.count { it.category.equals("per_shed_partition", ignoreCase = true) },
        operatorCount = sheds.map { it.operatorUserId }.filter { it.isNotBlank() }.distinct().size,
        // A bucket is "to verify" only while it still has unlooked-at evidence: the rework subset
        // is reported separately because that work is back with the operator, not with a verifier.
        toVerifyCount = sheds.count { (it.pendingVerificationCount - it.reworkCount) > 0 },
        reworkCount = sheds.count { it.reworkCount > 0 },
        acceptedCount = accepted,
    )
}

/** Inputs the task-detail state is derived from, so the capability stream can join them. */
private data class TaskDetailInputs(
    val task: WeighingTask?,
    val campaignId: String,
    val operatorNames: Map<String, String>,
    val loading: Boolean,
    val busy: Boolean,
    /** The single-task read's capability answer, or null when only the list answered. */
    val deepLinkCapabilities: WeighingCapabilities? = null,
)

/** The weigh date as a person reads it, never the machine form. */
private fun WeighingTask.weighDateLabel(): String = runCatching {
    LocalDate.parse(weighDate, weighingIsoDateFormatter).format(weighingTodayFormatter)
}.getOrDefault(weighDate)

/**
 * Whether this task can be staged into the authoring wizard at all.
 *
 * It needs a park and at least one bucket that names a real shed; without those there is nothing
 * to place on another date, so the action must render disabled rather than tap to nothing.
 */
internal fun WeighingTask.isRepeatable(): Boolean =
    parkId.isNotBlank() && sheds.any { it.locationId.isNotBlank() }

internal const val REPEAT_BLOCKED_REASON =
    "This task has no shed bucket that can be placed on another date"

/** Reason CODES for ending a task. The backend owns the sentence that is recorded. */
private const val CLOSE_REASON_ALL_ACCEPTED = "all_buckets_accepted"
private const val CLOSE_REASON_OPEN_BUCKETS = "open_buckets_closed"
