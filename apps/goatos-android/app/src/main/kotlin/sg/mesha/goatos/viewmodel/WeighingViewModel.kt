package sg.mesha.goatos.viewmodel

import sg.mesha.goatos.BuildConfig

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import kotlinx.coroutines.flow.first
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
import sg.mesha.goatos.core.analytics.AnalyticsEventsWeighing
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
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.weighing.IndividualWeighingCapture
import sg.mesha.goatos.core.data.weighing.WeighingCsvExportRow
import sg.mesha.goatos.core.data.weighing.parseWeighingExportCsv
import sg.mesha.goatos.core.data.weighing.ShedPartitionWeighingCapture
import sg.mesha.goatos.core.data.weighing.WeighingAssignment
import sg.mesha.goatos.core.data.weighing.WeighingCapabilities
import sg.mesha.goatos.core.data.weighing.WeighingOperatorSummary
import sg.mesha.goatos.core.data.weighing.WeighingPlanDraft
import sg.mesha.goatos.core.data.weighing.WeighingPlannerCatalog
import sg.mesha.goatos.core.data.weighing.WeighingRepository
import sg.mesha.goatos.core.data.weighing.WeighingRosterRowEntity
import sg.mesha.goatos.core.data.weighing.WeighingScopeState
import sg.mesha.goatos.core.data.weighing.weighingCacheAgeNotice
import sg.mesha.goatos.core.data.weighing.WeighingTask
import sg.mesha.goatos.core.data.weighing.WeighingTaskBucketCache
import sg.mesha.goatos.core.data.weighing.WeighingTaskListCache
import sg.mesha.goatos.core.data.weighing.WeighingTaskLookup
import sg.mesha.goatos.core.data.weighing.WeighingTaskShed
import sg.mesha.goatos.core.data.weighing.weighingScopeKey
import sg.mesha.goatos.feature.weighing.WEIGHING_BUCKET_LADDER_STEPS
import sg.mesha.goatos.feature.weighing.WeighingAssignmentUiRow
import sg.mesha.goatos.feature.weighing.WeighingDraftUiRow
import sg.mesha.goatos.feature.weighing.WeighingExportPreviewRowUi
import sg.mesha.goatos.feature.weighing.WeighingExportPreviewShedUi
import sg.mesha.goatos.feature.weighing.WeighingExportPreviewUiState
import sg.mesha.goatos.feature.weighing.WeighingOperatorFilterUiRow
import sg.mesha.goatos.feature.weighing.WeighingOperatorUiRow
import sg.mesha.goatos.feature.weighing.WeighingParkFilterUiRow
import sg.mesha.goatos.feature.weighing.WeighingProofUiRow
import sg.mesha.goatos.feature.weighing.WeighingRosterUiRow
import sg.mesha.goatos.feature.weighing.WeighingTaskDetailUiState
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
    // Optional for the same reason WeighingRepository takes it optionally: a unit test that
    // exercises capture logic has no queue to observe, and the 90-method port is not worth a fake
    // per test. Hilt always supplies it in the app, so the conflict watch below is live in
    // production and simply absent in tests that never enqueue.
    private val syncRepository: SyncRepository? = null,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    private val repeatSeedStore: WeighingRepeatSeedStore,
    private val exportFileWriter: sg.mesha.goatos.export.WeighingExportFileWriter,
    private val savedStateHandle: SavedStateHandle,
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
    private val parkLabel = savedStateHandle.get<String>(Routes.WEIGHING_PARK_LABEL_ARG).orEmpty()
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
    // Restored from SavedStateHandle so typed-but-unsubmitted per-animal weights survive a
    // process death mid-scan -- see KEY_ANIMAL_WEIGHT_INPUT_IDS/VALUES in the companion object
    // for why SavedStateHandle (not Room) is the right durability layer for this map.
    private val animalWeightInputs = MutableStateFlow(restoreAnimalWeightInputs(savedStateHandle))
    // The row itself (rosterWindow/scannedRows data) is NOT Bundle-safe, so only the animal id is
    // persisted; it is re-resolved against scopeState/scannedRows once those are live again (see
    // the restoreSelectedRow() call in init below).
    private val selectedRow = MutableStateFlow<WeighingRosterRowEntity?>(null)
    private val scannedRows = MutableStateFlow<List<WeighingRosterRowEntity>>(emptyList())
    // Track which animals have experienced server-side weight write conflicts. Used to display
    // an error message to the operator instead of the false "saved" message.
    // mobile-guard:ignore: bounded by one scope's captured animals (typically <100 per session).
    private val conflictedAnimalIds = MutableStateFlow<Set<String>>(emptySet())
    private val autoProofs = MutableStateFlow<Map<String, ProofCaptureRow>>(emptyMap())
    private val proofReplacementAnimalId = MutableStateFlow<String?>(null)
    private val observedProofs = MutableStateFlow<List<ProofCaptureRow>>(emptyList())
    private val rawProofs = MutableStateFlow<List<ProofCaptureRow>>(emptyList())
    private val sessionProofIds = MutableStateFlow<Set<String>>(emptySet())
    // De-duplication for proof-upload telemetry: Room re-emits the same failed row on every
    // observation pass, so without these a single stuck upload would spam the funnel. Bounded by
    // the number of proofs one scope can hold (<= 5 shed videos + the per-animal captures).
    // mobile-guard:ignore: bounded by ONE scope's proofs. This ViewModel is constructed per
    // (campaignId, workGroupId, campaignShedId) — see `scopeKey` above — so it is destroyed when
    // the operator leaves the bucket, and these never outlive a single shed's capture session
    // (<= 5 shed videos, or that shed's per-animal captures). They do NOT accumulate across a
    // shift; a new bucket gets a new ViewModel and new empty collections.
    private val reportedProofUploadTrouble = mutableSetOf<String>() // mobile-guard:ignore: per-scope ViewModel, dies with the bucket; <= one shed's captures

    // mobile-guard:ignore: same per-scope lifetime as reportedProofUploadTrouble above.
    private val proofUploadAttempts = mutableMapOf<String, Int>() // mobile-guard:ignore: per-scope ViewModel, dies with the bucket; <= one shed's captures
    private var currentPrincipalId: String? = null
    private val assignments = MutableStateFlow<List<WeighingAssignment>>(emptyList())
    private val assignmentsNextCursor = MutableStateFlow<String?>(null)

    /**
     * The backend's OPERATOR-grain roll-up for the CURRENT park filter.
     *
     * Held separately from [assignments] on purpose: [assignments] is a growing keyset page, and
     * anything counted from it would describe how far the reader has scrolled rather than what a
     * person actually did. Only a whole-filter read replaces this, so appending a page leaves it
     * untouched.
     */
    private val operatorSummaries = MutableStateFlow<List<WeighingOperatorSummary>>(emptyList())
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

    // Set the first time an ASSIGNMENTS fetch COMPLETES, whatever it returned. `loadingAssignments`
    // flips on every later refresh too, so the screen used to gate on that instead and either wedged
    // on a spinner forever or yanked already-drawn rows away mid-refresh. A throw before `finally` in
    // [refreshAssignments] leaves this false forever and wedges the empty state on a spinner.
    private val _hasLoadedOnce = MutableStateFlow(false)
    /** Which assignments query the marker belongs to; a different park filter has not been read yet. */
    private var loadedAssignmentsScopeKey: String? = null

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
     *
     * The observed window is a FIXED page size — it never grows. Scrolling appends the next keyset
     * page into Room via [WeighingRepository.appendTaskList] instead, so the cache can hold far more
     * than one page without the observed window (and its `LIMIT`) ever capping what is reachable.
     */
    @OptIn(ExperimentalCoroutinesApi::class)
    private val taskCache: StateFlow<WeighingTaskListCache> =
        if (scopeKey != null) {
            flowOf(WeighingTaskListCache())
        } else {
            selectedAssignmentParkId.flatMapLatest { parkId -> repository.observeTaskList(surface, parkId) }
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
        selectedTaskId
            .flatMapLatest { taskId ->
                if (taskId.isNullOrBlank()) {
                    flowOf(WeighingTaskBucketCache())
                } else {
                    repository.observeTaskBuckets(taskId)
                }
            }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), WeighingTaskBucketCache())
    private val message = MutableStateFlow<String?>(null)
    private val actionInFlight = MutableStateFlow(false)
    private val showSubmitConfirmation = MutableStateFlow(false)

    // ---- Task-detail CSV export (leadership-only, weighing.monitor) -------------------------
    private val exportingCsv = MutableStateFlow(false)

    /**
     * A file the export just wrote, waiting for the screen to hand it to a share sheet.
     *
     * State, not a one-shot event channel: this ViewModel already renders every other action's
     * result through [taskDetailState] the same way, so a second signalling mechanism just for
     * this one action would be a second pattern to remember. [consumeExportReadyFile] clears it
     * once the screen has launched the intent, so a configuration change cannot re-fire it.
     */
    private val exportReadyFile = MutableStateFlow<android.net.Uri?>(null)
    val exportReadyFileUri: StateFlow<android.net.Uri?> = exportReadyFile

    // ---- Export PREVIEW (see the sheet before any share sheet fires) ------------------------
    //
    // The maintainer's ask: tapping the export icon used to download the CSV AND immediately fire
    // ACTION_SEND, with no way to see what was actually in the file first. This is a SEPARATE
    // fetch from [exportTaskCsv] -- the preview parses the bytes into rows for on-screen reading
    // and never writes them to disk or a share intent; sharing from the preview reuses
    // [exportTaskCsv] unchanged, which still does its own disk write + share hand-off.
    //
    // [exportPreviewState] itself is declared further down, AFTER [activeTask]: Kotlin initialises
    // property initialisers in declaration order, and it reads [activeTask] for the park/date
    // label, so it cannot be declared above that property.
    private val exportPreviewLoading = MutableStateFlow(false)
    private val exportPreviewError = MutableStateFlow("")
    // Set the first time an export-preview fetch for the CURRENT task COMPLETES. NOTE: this marker
    // is published to [exportPreviewState] but the screen keeps `loading` in its own gate --
    // [loadExportPreview] returns early (before the try) when [selectedTask] has not resolved yet
    // or the viewer's capability has not landed yet, and neither path ever reaches the `finally`
    // that sets this true. Dropping `loading` there would wedge the screen blank whenever the
    // deep-linked task or capability flag is still in flight when the screen first opens.
    private val _exportPreviewHasLoadedOnce = MutableStateFlow(false)
    /** Which task's export the marker belongs to; a different task has not been read yet. */
    private var loadedExportPreviewCampaignId: String? = null
    // Grouped by shed and parsed ONCE per fetch, off the main thread -- see [loadExportPreview].
    // A real park export is up to ~8,000 CSV rows; parsing and grouping that on every recomposition
    // (the old shape, done inline inside the [exportPreviewState] combine) is exactly the "throwing
    // up entirely at once" the maintainer flagged. This holds the already-computed result so combine
    // only re-packages it, never re-parses it.
    private val exportPreviewSheds = MutableStateFlow<List<WeighingExportPreviewShedUi>>(emptyList())
    private var exportPreviewJob: Job? = null

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
    // The ONE row queued behind an in-flight capture for a DIFFERENT animal. An in-flight capture
    // is NEVER cancelled by a later scan — cancelling an active recording destroys unrecoverable
    // field footage (the confirmed shed defect: an RFID scan mid-recording stops recording and
    // exits the camera). The newly scanned animal is queued here instead and [captureVideoForRow]
    // opens its camera automatically once the in-flight job completes. Only the LAST queued row
    // survives a later scan.
    private var pendingProofRow: WeighingRosterRowEntity? = null

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

    /**
     * True once THIS scope's own submit has reached the outbox and not (yet, or ever) failed --
     * i.e. QUEUED, IN_FLIGHT or SUCCEEDED. Backed by the durable outbox row `submitIndividualScope`
     * itself enqueues (see [sg.mesha.goatos.core.data.weighing.WeighingRepository.findPendingSubmit],
     * which resolves the outbox by (groupKey, opType) identity -- NOT by re-deriving today's
     * idempotency key, which rotates the moment this same row reaches SUCCEEDED and would then
     * miss it), never a transient
     * VM-only flag -- so a fresh VM re-entering an already-submitted scope (killed process, or the
     * operator simply navigating back in) renders read-only from a Room read, not from something
     * this VM instance remembered doing itself. A FAILED submit is intentionally NOT read-only: the
     * operator can still fix and retry it.
     *
     * Nullable to avoid race: initialized as null, set to true/false after first refreshScopeSubmitted()
     * completes. The UI state only sets isReadOnly = true when this is non-null and true, preventing
     * the transient "editable" state that occurred when the screen rendered before the async refresh
     * completed. This also prevents the hole where outbox row pruning (if it occurs after SUCCEEDED)
     * would revert scopeSubmitted to false even though the backend knows the scope is submitted.
     */
    private val scopeSubmitted = MutableStateFlow<Boolean?>(null)

    private suspend fun refreshScopeSubmitted() {
        if (scopeKey == null) {
            scopeSubmitted.value = false  // Not a scoped session; mark refreshed
            return
        }
        val result = repository.findPendingSubmit(campaignId, campaignShedId)
        val status = (result as? AppResult.Ok)?.value?.status
        scopeSubmitted.value = status == SyncItemStatus.SUCCEEDED ||
            status == SyncItemStatus.IN_FLIGHT ||
            status == SyncItemStatus.QUEUED
    }

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
        combine(assignments, selectedAssignmentParkId, appendingAssignments, knownAssignmentParks, operatorSummaries) {
                availableAssignments,
                selectedParkId,
                appending,
                knownParks,
                summaries,
            ->
            AssignmentParkSelection(availableAssignments, selectedParkId, appending, knownParks, summaries)
        }.let { assignmentSelection ->
            combine(
                assignmentSelection,
                loadingAssignments,
                plannerMode,
                assignmentCapabilities,
                _hasLoadedOnce,
            ) { selection, loading, isPlanner, capabilities, hasLoadedOnce ->
                WeighingRootState(
                    assignments = selection.assignments,
                    loading = loading,
                    plannerMode = isPlanner,
                    selectedParkId = selection.selectedParkId,
                    appendingAssignments = selection.appending,
                    knownParks = selection.knownParks,
                    capabilities = capabilities,
                    operatorSummaries = selection.operatorSummaries,
                    hasLoadedOnce = hasLoadedOnce,
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
     * The export PREVIEW state: [activeTask]'s park/date plus the parsed CSV rows -- see the
     * fields declared next to [exportPreviewLoading] above for why this reads [activeTask].
     */
    val exportPreviewState: StateFlow<WeighingExportPreviewUiState> = combine(
        activeTask,
        exportPreviewLoading,
        exportPreviewError,
        exportPreviewSheds,
        exportingCsv,
    ) { task, loading, error, sheds, sharing ->
        WeighingExportPreviewUiState(
            campaignId = task?.campaignId.orEmpty(),
            parkName = task?.parkName?.ifBlank { task.parkId }.orEmpty(),
            dateLabel = task?.weighDate?.let { weighDate ->
                runCatching {
                    // exception:exempt date display; unparseable date shows raw ISO string
                    LocalDate.parse(weighDate, weighingIsoDateFormatter)
                }
                    .getOrNull()
                    ?.format(weighingTodayFormatter)
                    ?: weighDate
            }.orEmpty(),
            loading = loading,
            error = error,
            sheds = sheds,
            // The grouping already walks every row once; re-summing here is O(sheds), not
            // O(rows-again), and keeps [WeighingExportPreviewShedUi] the single source of truth.
            totalRowCount = sheds.sumOf { it.rows.size },
            sharing = sharing,
        )
    }.let { base ->
        combine(base, _exportPreviewHasLoadedOnce) { uiState, hasLoadedOnce ->
            uiState.copy(hasLoadedOnce = hasLoadedOnce)
        }
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), WeighingExportPreviewUiState())

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
            combine(plannerCatalog, deepLinkCapabilities, exportingCsv) { catalog, caps, exporting -> Triple(catalog, caps, exporting) },
            tasksLoading,
            actionInFlight,
        ) { task, taskId, (catalog, deepLinkCaps, exporting), loading, busy ->
            val operatorNames = catalog?.operators.orEmpty()
                .filter { it.userId.isNotBlank() && it.displayName.isNotBlank() }
                .associate { it.userId to it.displayName }
            TaskDetailInputs(task, taskId.orEmpty(), operatorNames, loading, busy, deepLinkCaps, exporting)
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
                    exportingCsv = inputs.exportingCsv,
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
        refreshTaskBuckets(reset = true)
        resolveDeepLinkedTask()
    }

    /**
     * Fetches ONE page of the selected task's buckets into Room. [reset] true re-reads page 1 and
     * drops the task's stale rows; false appends the next keyset page using the stored cursor. The
     * cached buckets stay on screen while it runs and stay on screen if it fails.
     */
    private fun refreshTaskBuckets(reset: Boolean) {
        val campaignId = selectedTaskId.value?.takeIf { it.isNotBlank() } ?: return
        viewModelScope.launch {
            val loaded = if (reset) {
                repository.refreshTaskBuckets(campaignId, reset = true)
            } else {
                repository.appendTaskBuckets(campaignId)
            }
            when (loaded) {
                is AppResult.Ok -> tasksStale.value = ""
                is AppResult.Err -> tasksStale.value = STALE_NOTICE_PREFIX + loaded.message
            }
        }
    }

    /**
     * Scroll-driven prefetch for the task detail's bucket list: one page per trigger, tail window
     * only. The observed Room window stays a FIXED page size; only the cursor advances.
     */
    fun onTaskBucketRowVisible(index: Int) {
        val loaded = taskBucketCache.value.items.size
        if (loaded == 0 || index < loaded - LIST_PREFETCH_DISTANCE) return
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
     * Downloads the selected task's CSV export and hands the file to the screen for a share/open
     * intent.
     *
     * Gated on [WeighingCapabilities.canExportCsv], the SAME weighing.monitor permission the
     * backend checks: a viewer without it never sees the button (see
     * [sg.mesha.goatos.feature.weighing.WeighingTaskDetailUiState.canExportCsv]), so reaching this
     * function without the capability would already be a UI bug, not a legitimate 403 to surface.
     * Every failure path -- the network read AND the on-device file write -- reports through
     * [message] and [crashReporter], matching every other write in this ViewModel; nothing here is
     * caught and dropped.
     */
    fun exportTaskCsv() {
        val task = selectedTask() ?: return
        if (exportingCsv.value) return
        if (!taskCache.value.capabilities.canExportCsv) return
        exportingCsv.value = true
        viewModelScope.launch {
            try {
                when (val exported = repository.exportCampaignCsv(task.campaignId)) {
                    is AppResult.Ok -> {
                        try {
                            exportReadyFile.value = exportFileWriter.write(
                                exported.value.bytes,
                                exported.value.suggestedFileName,
                            )
                        } catch (writeFailure: Exception) {
                            message.value = "Could not save the export file."
                            crashReporter.recordException(writeFailure, "weighing csv export write failed")
                        }
                    }
                    is AppResult.Err -> {
                        message.value = exported.message
                        crashReporter.recordException(
                            IllegalStateException(exported.message),
                            "weighing csv export failed",
                        )
                    }
                }
            } finally {
                exportingCsv.value = false
            }
        }
    }

    /** The screen calls this once it has launched the share/open intent for [exportReadyFileUri]. */
    fun consumeExportReadyFile() {
        exportReadyFile.value = null
    }

    /**
     * Downloads and parses the selected task's CSV for on-screen reading only.
     *
     * This is the fetch behind [exportPreviewState] -- it never writes a file and never launches
     * a share intent, which is exactly the behaviour change the maintainer asked for: tapping the
     * export icon now opens this preview, and sharing is a SEPARATE action the preview screen
     * offers via [exportTaskCsv]. Same gate as export ([WeighingCapabilities.canExportCsv]) since
     * this reads the same backend endpoint. Both the network read and a malformed response are
     * reported through [exportPreviewError] and [crashReporter] -- nothing here fails silently.
     *
     * Parsing AND grouping run on [Dispatchers.Default], not [viewModelScope]'s main dispatcher --
     * a real park export is up to ~8,000 rows, and walking that many characters/rows on the main
     * thread is exactly the freeze the maintainer reported. This is a SINGLE whole-file parse: the
     * backend returns the full CSV in one response body regardless, so a lazy per-shed parse would
     * still need the whole byte array in memory first for no main-thread win -- what actually
     * mattered was moving the CPU work off the UI thread and doing it ONCE, which this does.
     *
     * A refresh does NOT clear [exportPreviewSheds] on failure -- the previous good preview stays
     * on screen (see [WeighingExportPreviewScreen]'s no-flicker rule) and only [exportPreviewError]
     * carries the new failure.
     */
    fun loadExportPreview() {
        val task = selectedTask() ?: return
        if (!taskCache.value.capabilities.canExportCsv) return
        // The marker belongs to the TASK, not the screen: a different campaign id is a different
        // export, and leaving the marker true would let a previous task's answer stand in for one
        // never read. Keyed rather than blindly reset: re-running preview for the SAME task must
        // not blank a legitimately empty sheet list while it re-reads.
        if (loadedExportPreviewCampaignId != task.campaignId) _exportPreviewHasLoadedOnce.value = false
        exportPreviewJob?.cancel()
        exportPreviewJob = viewModelScope.launch {
            exportPreviewLoading.value = true
            exportPreviewError.value = ""
            try {
                when (val exported = repository.exportCampaignCsv(task.campaignId)) {
                    is AppResult.Ok -> {
                        try {
                            val sheds = withContext(Dispatchers.Default) {
                                parseWeighingExportCsv(exported.value.bytes).toExportPreviewSheds()
                            }
                            exportPreviewSheds.value = sheds
                        } catch (parseFailure: Exception) {
                            exportPreviewError.value = "Could not read the export file."
                            crashReporter.recordException(parseFailure, "weighing csv export parse failed")
                        }
                    }
                    is AppResult.Err -> {
                        exportPreviewError.value = exported.message
                        crashReporter.recordException(
                            IllegalStateException(exported.message),
                            "weighing csv export preview fetch failed",
                        )
                    }
                }
            } catch (t: Throwable) {
                    // A cancelled scope is not a failure. Catching Throwable without letting
                    // CancellationException through breaks structured concurrency: rotating the
                    // screen or navigating away would be reported as an error and would publish
                    // state after the scope had already been cancelled.
                    if (t is kotlinx.coroutines.CancellationException) throw t
                // A repository throw must not escape viewModelScope.launch and crash the app --
                // same defect shape fixed in SessionViewModel's dev-session bring-up and in
                // VerifyQueueViewModel.refresh() (see the catch there). Record it and resolve to
                // an honest error state instead of propagating; hasLoadedOnce still flips in
                // `finally` below so the screen never wedges on the empty-preview spinner.
                exportPreviewError.value = "Could not read the export file."
                runCatching { crashReporter.recordException(t, "weighing csv export preview fetch failed") }
            } finally {
                exportPreviewLoading.value = false
                // In FINALLY, not after the result: a throw on the way here would leave this false
                // forever and wedge the empty-preview card on a spinner over a blank sheet list.
                _exportPreviewHasLoadedOnce.value = true
                loadedExportPreviewCampaignId = task.campaignId
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
                    partitionLabel = it.partitionLabel,
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
                    // exception:exempt date display; unparseable date shows raw ISO string
                    LocalDate.parse(task.weighDate, weighingIsoDateFormatter).format(weighingTodayFormatter)
                }.getOrDefault(task.weighDate),
                buckets = buckets,
            ),
        )
        return campaignId
    }

    /**
     * Hands the selected task's park, weigh date, buckets, modes and operators to the authoring
     * wizard for an IN-PLACE change, rather than a new task.
     *
     * Unlike [stageRepeatOfTask], the weigh date travels with the seed and is applied immediately:
     * an edit changes what a task holds, never which day it runs on. The wizard's own shed-picker
     * excludes this campaign id from the server's availability check, so this task's own sheds
     * never come back as "already scheduled" against themselves. Saving from edit mode calls
     * [WeighingRepository.updatePlan] against this campaign id -- the create-then-publish path
     * never runs, so a second task is never fabricated.
     *
     * Returns the source task id when a seed was staged, so the caller can navigate; null means
     * there was nothing to carry over and the caller must not pretend otherwise.
     */
    fun stageEditOfTask(campaignId: String): String? {
        val task = tasks.value.firstOrNull { it.campaignId == campaignId }
            ?: selectedTaskSnapshot.value?.takeIf { it.campaignId == campaignId }
            ?: return null
        val buckets = task.sheds
            .filter { it.locationId.isNotBlank() }
            .map {
                WeighingRepeatBucket(
                    locationId = it.locationId,
                    partitionLabel = it.partitionLabel,
                    category = it.category,
                    operatorUserId = it.operatorUserId,
                )
            }
        if (task.parkId.isBlank() || buckets.isEmpty() || task.weighDate.isBlank()) {
            message.value = REPEAT_BLOCKED_REASON
            return null
        }
        repeatSeedStore.stage(
            sourceCampaignId = campaignId,
            seed = WeighingRepeatSeed(
                parkId = task.parkId,
                parkName = task.parkName.ifBlank { task.parkId },
                sourceDateLabel = runCatching {
                    // exception:exempt date display; unparseable date shows raw ISO string
                    LocalDate.parse(task.weighDate, weighingIsoDateFormatter).format(weighingTodayFormatter)
                }.getOrDefault(task.weighDate),
                buckets = buckets,
                editCampaignId = campaignId,
                editWeighDate = task.weighDate,
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
                operatorSummaries = root.operatorSummaries,
                loading = root.loading,
                appendingAssignments = root.appendingAssignments,
                isPlanner = root.plannerMode,
                selectedParkId = root.selectedParkId,
                hasLoadedOnce = root.hasLoadedOnce,
                localScans = capture.scans,
                proofs = capture.proofs,
                readerConnection = capture.readerConnection,
            )
        }
            .let { base ->
                // The confirmation flag MUST be folded in here. It was a private flow nobody read:
                // submitIndividualScope set it true, the screen renders its dialog on
                // state.showSubmitConfirmation, and that field stayed false forever -- so Submit
                // passed every gate, armed the pending identifiers, showed no dialog, and
                // confirmSubmitIndividualScope was never reached. The tap did nothing at all,
                // with no message to say why.
                combine(base, showSubmitConfirmation) { uiState, confirming ->
                    uiState.copy(showSubmitConfirmation = confirming)
                }
            }
            .let { base ->
                // isReadOnly folded in the SAME way showSubmitConfirmation is above: it is a
                // durable, Room-observed signal (the outbox row behind the scope's own submit --
                // see refreshScopeSubmitted()), not a transient VM flag, so a re-entered screen
                // (fresh VM, fresh process) renders read-only from the FIRST emission rather than
                // only after some later user action re-derives it. scopeSubmitted is nullable to
                // avoid the race where the screen renders editable before refreshScopeSubmitted()
                // completes: we only update isReadOnly once submitted is non-null (after first refresh).
                combine(base, scopeSubmitted) { uiState, submitted ->
                    if (submitted != null) {
                        uiState.copy(isReadOnly = submitted)
                    } else {
                        uiState
                    }
                }
            }
            .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), WeighingUiState())

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
            viewModelScope.launch { refreshScopeSubmitted() }
            viewModelScope.launch {
                val profile = runCatching {
                    // exception:exempt cached profile fetch; best-effort, null is acceptable fallback
                    bootstrapRepository.operatorProfile()
                }.getOrNull()
                currentPrincipalId = profile?.operatorId?.takeIf { it.isNotBlank() }
                restoreLumpSumInputDraft()
            }
            // A recreated process (killed while the submit confirmation was showing) restores the
            // dialog too, instead of leaving it silently gone with no way back short of re-scanning
            // every animal. confirmSubmitIndividualScope() recomputes identifiers fresh either way
            // (see its comment), so re-arming here only needs to reopen the gate.
            if (savedStateHandle.get<ArrayList<String>>(KEY_SUBMIT_PENDING_IDENTIFIERS) != null) {
                showSubmitConfirmation.value = true
            }
            viewModelScope.launch {
                scanCaptureRepository.observeScannedTags(scopeKey, WEIGHING_SCAN_FIELD_KEY).collect { scans ->
                    scannedRows.value = scans.map { unknownWeighingRow(scopeKey, it.tag, it.capturedAtMs) }
                    restoreSelectedRowIfNeeded()
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
                    restoreSelectedRowIfNeeded()
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
        // Observe sync status for weight write conflicts. When the server rejects a
        // WEIGHING_ANIMAL_OBSERVATION write with 409, mark that animal as having a conflict so the
        // UI can show an error instead of the false success message.
        // Join the failed write back to the animal on the IDEMPOTENCY KEY, which is the only field
        // that identifies the capture. The row's own id is a uuid and groupKey is the shed scope,
        // so neither can name the animal -- an earlier attempt parsed the tag out of `item.id` and
        // therefore matched nothing, leaving the operator with the same false "saved" it was
        // written to prevent. The key is carried verbatim from the capture, so the match is exact.
        val syncStatuses = syncRepository?.observeStatus()
        if (syncStatuses != null) viewModelScope.launch {
            syncStatuses.collect { status ->
                // Live re-check when THIS scope's own submit row changed status (not on every
                // outbox emission app-wide -- that fired a Room query on every unrelated write in
                // every other feature's queue). Still reactive to a reconcile/retry/failure that
                // lands while this screen sits open, just scoped to the row that can actually
                // change scopeSubmitted's answer.
                if (scopeKey != null &&
                    status.items.any { it.opType == WEIGHING_SCOPE_SUBMIT_OP && it.groupKey == campaignShedId }
                ) {
                    refreshScopeSubmitted()
                }
                val conflictedKeys = status.items
                    .filter { it.opType == WEIGHING_ANIMAL_OBSERVATION_OP && it.conflict }
                    .map { it.idempotencyKey }
                    .toSet()
                if (conflictedKeys.isEmpty()) return@collect
                val affected = scopeState.value?.individualDrafts.orEmpty()
                    .filter { it.idempotencyKey in conflictedKeys }
                    .map { it.scannedIdentifier }
                    .toSet()
                val newlyConflicted = affected - conflictedAnimalIds.value
                if (newlyConflicted.isEmpty()) return@collect
                // ASK THE SERVER WHAT ACTUALLY HAPPENED. A refused write is not automatically lost
                // work: the two conflicts this path sees mean opposite things. `duplicate_scan`
                // means the weight is ALREADY STORED and the phone simply re-posted it -- there is
                // nothing to redo. `weighing_rejected_proof_reuse` means the verifier sent this
                // animal back and the old video cannot stand. Telling an operator to "record the
                // animal again" in the first case sends him to repeat work that is already saved,
                // the same class of lie as a rejection banner that outlives its re-shoot.
                //
                // The outbox stores only the server's display text, not its code, so the code is
                // not reliably available here -- and string-matching copy is not a contract. The
                // refetch settles it from the record instead, which also removes the need to leave
                // the screen and come back for it to look right.
                // AWAIT the refetch. refresh() -> refreshScope() only LAUNCHES a coroutine, so
                // calling it and reading scopeState on the next line classifies against the
                // PRE-refresh draft. That fails in exactly the case this exists for: a sent-back
                // animal whose local draft has not yet been updated to `rework` reads as "already
                // stored", stillNeedsWork comes out empty, and the operator never sees the banner
                // telling him to record it again -- the silent-failure this block was written to
                // remove, reintroduced one line below it.
                // A FAILED refresh is not an answer. refreshScope returns AppResult, so a server
                // error comes back as Err rather than a throw -- runCatching alone treats that as
                // success and the code below then reads whatever Room already held, which is
                // exactly the stale "pending" that swallows a real rejection. Only an Ok refresh
                // licenses trusting the snapshot.
                val refreshed = runCatching {
                    repository.refreshScope(campaignId, workGroupId, campaignShedId, ROSTER_SYNC_MAX_ROWS)
                }.onFailure { error ->
                    crashReporter.recordException(error, "weighing conflict reclassify refresh failed")
                }.getOrNull() is AppResult.Ok
                // Read the refreshed record DIRECTLY, do not infer freshness from the stream.
                //
                // Four review rounds died on proxies here: awaiting a fire-and-forget refresh, then
                // waiting for scopeState to emit, then for the tag to be present (it always was --
                // newlyConflicted is derived from these very drafts), then for a non-null
                // verificationStatus (a phone that had refreshed earlier already holds "pending",
                // so a fresh rejection was still swallowed). Every one of them was "almost right,
                // stale in one plausible path".
                //
                // refreshScope writes Room inside its suspend call, so a one-shot read after it
                // returns sees THIS refresh's answer by construction. No emission to wait for and
                // no freshness heuristic to get wrong.
                //
                // Without a successful refresh the draft set is treated as EMPTY, which drops every
                // conflicted tag into the "record it again" branch below. That is the safe
                // direction: an unnecessary re-record is recoverable, a swallowed rejection is not.
                val activeScopeKey = scopeKey ?: return@collect
                val afterRefresh = if (refreshed) {
                    runCatching { repository.individualDraftsSnapshot(activeScopeKey) }.getOrElse { emptyList() }
                } else {
                    emptyList()
                }
                val stillNeedsWork = newlyConflicted.filter { tag ->
                    val draft = afterRefresh.firstOrNull { normalizeFreeFlowTag(it.scannedIdentifier) == normalizeFreeFlowTag(tag) }
                    draft == null || draft.verificationStatus == WEIGHING_VERIFICATION_REWORK
                }.toSet()
                conflictedAnimalIds.value = conflictedAnimalIds.value + stillNeedsWork
                if (stillNeedsWork.isEmpty()) {
                    // Already recorded on the server. Say nothing alarming and leave the row alone.
                    return@collect
                }
                stillNeedsWork.forEach { _ ->
                    analytics.track(
                        AnalyticsEvents.WEIGHING_CAPTURE_CONFLICT,
                        weighingCaptureProps(INDIVIDUAL_ANIMAL_CATEGORY),
                    )
                }
                // Only reached when the record itself says this animal still owes work.
                message.value = "This animal was sent back. Record it again."
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
        // The marker belongs to the QUERY (surface + park filter), not the screen. Selecting a
        // different park starts a brand-new read, and leaving the marker true let the previous
        // park's answer stand in for the new one -- the empty-work card rendered over a park
        // nothing had been read for yet. Keyed rather than blindly reset: a pull-to-refresh on the
        // SAME park must not blank a legitimately empty list while it re-reads.
        val assignmentsScopeKey = selectedAssignmentParkId.value.toString()
        if (loadedAssignmentsScopeKey != assignmentsScopeKey) _hasLoadedOnce.value = false
        loadingAssignments.value = true
        viewModelScope.launch {
            try {
                when (val loaded = repository.listAssignments(cursor = null, scope = surface, parkId = selectedAssignmentParkId.value)) {
                    is AppResult.Ok -> {
                        assignments.value = loaded.value.items
                        assignmentsNextCursor.value = loaded.value.nextCursor
                        // Whole-filter truth: replaced only by a fresh read, never accumulated.
                        operatorSummaries.value = loaded.value.operatorSummaries
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
            } catch (t: Throwable) {
                    // A cancelled scope is not a failure. Catching Throwable without letting
                    // CancellationException through breaks structured concurrency: rotating the
                    // screen or navigating away would be reported as an error and would publish
                    // state after the scope had already been cancelled.
                    if (t is kotlinx.coroutines.CancellationException) throw t
                // A repository throw must not escape viewModelScope.launch and crash the app --
                // same defect shape fixed in SessionViewModel's dev-session bring-up and in
                // VerifyQueueViewModel.refresh() (see the catch there). Record it and resolve to
                // an honest error state instead of propagating; hasLoadedOnce still flips in
                // `finally` below so the screen never wedges on the empty-work spinner.
                val reason = t.message ?: "Unknown error"
                assignmentsError.value = reason.toWeighingReadMessage()
                reportReadFailure(reason, t)
            } finally {
                loadingAssignments.value = false
                // In FINALLY, not after the result: a throw on the way here would leave this false
                // forever and wedge the empty-work card on a spinner over a blank list.
                _hasLoadedOnce.value = true
                loadedAssignmentsScopeKey = assignmentsScopeKey
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
                        val known = assignments.value.map { it.assignmentIdentityKey() }.toSet()
                        assignments.value = assignments.value + loaded.value.items.filter { it.assignmentIdentityKey() !in known }
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
        refreshTasks()
    }

    private fun appendTasks() {
        if (scopeKey != null) return
        if (!taskCache.value.canLoadMore) return
        if (tasksLoading.value || tasksAppending.value) return
        tasksAppending.value = true
        viewModelScope.launch {
            try {
                when (
                    val loaded = repository.appendTaskList(
                        scope = surface,
                        parkId = selectedAssignmentParkId.value,
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
                // Weighing is FREE-FLOW: any scanned tag is accepted, and nothing is gated on a
                // roster. The scope sync only pre-warms labels, so an empty window is ordinary --
                // a shed whose animals were never enrolled weighs exactly the same as one whose
                // were. This used to raise "No animals are assigned to this weighing scope.",
                // which invented an assignment rule the module does not have (and told the
                // operator to stop when there was nothing to stop for).
                is AppResult.Ok -> Unit
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
        setAnimalWeightInput(animalId, filtered)
    }

    /** Sets (or, when [value] is blank, effectively leaves) a typed weight and persists it. */
    private fun setAnimalWeightInput(animalId: String, value: String) {
        animalWeightInputs.value = animalWeightInputs.value + (animalId to value)
        persistAnimalWeightInputs()
    }

    /** Removes a typed weight (after it has been committed to Room) and persists the removal. */
    private fun clearAnimalWeightInput(animalId: String) {
        animalWeightInputs.value = animalWeightInputs.value - animalId
        persistAnimalWeightInputs()
    }

    /**
     * Mirrors [animalWeightInputs] into [savedStateHandle] as parallel id/value lists -- see
     * [restoreAnimalWeightInputs] for why parallel lists rather than a single Map entry.
     */
    private fun persistAnimalWeightInputs() {
        val snapshot = animalWeightInputs.value
        savedStateHandle[KEY_ANIMAL_WEIGHT_INPUT_IDS] = ArrayList(snapshot.keys)
        savedStateHandle[KEY_ANIMAL_WEIGHT_INPUT_VALUES] = ArrayList(snapshot.values)
    }

    private fun persistSelectedRow(animalId: String?) {
        if (animalId.isNullOrBlank()) {
            savedStateHandle.remove<String>(KEY_SELECTED_ROW_ANIMAL_ID)
        } else {
            savedStateHandle[KEY_SELECTED_ROW_ANIMAL_ID] = animalId
        }
    }

    /**
     * Re-selects the row a prior process held selected, once durable roster/scan state
     * ([scopeState]/[scannedRows]) is live again -- [selectedRow] itself cannot be put in
     * SavedStateHandle (not Bundle-safe), only the id survives, so this re-resolves the full row.
     * Safe to call repeatedly; it is a no-op once a row is already selected or nothing was saved.
     */
    private fun restoreSelectedRowIfNeeded() {
        if (selectedRow.value != null) return
        val savedAnimalId = savedStateHandle.get<String>(KEY_SELECTED_ROW_ANIMAL_ID) ?: return
        val row = scopeState.value?.rosterWindow?.firstOrNull {
            it.animalId == savedAnimalId || it.id == savedAnimalId
        } ?: scannedRows.value.firstOrNull { it.animalId == savedAnimalId }
        if (row != null) selectedRow.value = row
    }

    fun selectAnimal(animalId: String) {
        if (scopeKey == null || animalId.isBlank()) return
        val row = scopeState.value
            ?.rosterWindow
            ?.firstOrNull { it.animalId == animalId || it.id == animalId }
            ?: return
        selectedRow.value = row
        persistSelectedRow(row.animalId)
        scanInput.value = row.primaryTag
        message.value = "Selected ${row.displayAnimalId}. Enter weight, then capture video."
    }

    fun submitTypedScan() {
        val tag = scanInput.value.trim()
        if (tag.isBlank()) return
        matchTag(tag, fromTypedEntry = true)
        scanInput.value = ""
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
        setAnimalWeightInput(animalId, sanitizedWeight)
        selectedRow.value = row
        persistSelectedRow(row.animalId)
        recordIndividualRow(key, row, useGlobalBusyGate = false)
    }

    /**
     * The scope's ready-to-submit identifiers, recomputed FRESH from durable, Room-observed
     * state (`scopeState`/`scannedRows`/`state.visibleRows`) every time it's called -- never
     * cached. This is what [confirmSubmitIndividualScope] now calls instead of trusting a
     * previously-stashed list, so a confirm can never silently no-op just because a stashed
     * value went missing (see that function's comment for why that used to happen).
     *
     * Returns null when the scope is not submittable.
     */
    private fun computeSubmittableIdentifiers(): List<String>? {
        val drafts = scopeState.value?.individualDrafts.orEmpty()
        val scannedIdentifiers = scannedRows.value
            .map { it.animalId }
            .distinct()
        val readyVisibleRows = state.value.visibleRows
            .filter { row ->
                scannedIdentifiers.contains(row.animalId) &&
                    row.weightSaved &&
                    row.proofUploadStatus == sg.mesha.goatos.feature.scan.ProofUploadStatus.SYNCED
            }
            .map { row -> row.animalId }
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
        }.plus(readyVisibleRows).distinct()
        val ready = scannedIdentifiers.isNotEmpty() &&
            submittedIdentifiers.size == scannedIdentifiers.size &&
            submittedIdentifiers.size == readyVisibleRows.size &&
            submittedIdentifiers.none { it !in scannedIdentifiers }
        return submittedIdentifiers.takeIf { ready }
    }

    fun submitIndividualScope(onSubmitted: () -> Unit) {
        if (category == PER_SHED_PARTITION_CATEGORY || actionInFlight.value || scopeSubmitted.value == true) return
        val submittedIdentifiers = computeSubmittableIdentifiers()
        if (submittedIdentifiers == null) {
            message.value = "Every scanned RFID in this shed needs saved weight and synced video before submit."
            analytics.track(
                AnalyticsEvents.SUBMIT_BLOCKED,
                weighingCaptureProps(INDIVIDUAL_ANIMAL_CATEGORY) +
                    (AnalyticsEvents.Params.REASON to "scope_incomplete"),
            )
            return
        }
        // Show confirmation dialog instead of submitting directly
        showSubmitConfirmation.value = true
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_SUBMIT_CONFIRMATION_OPENED,
            weighingCaptureProps(INDIVIDUAL_ANIMAL_CATEGORY),
        )
        // Durable across process death via SavedStateHandle -- kept ONLY as a hint for
        // `confirmSubmitIndividualScope` to prefer (skipping a redundant recompute); it is never
        // the sole source of truth the way it used to be (see that function).
        savedStateHandle[KEY_SUBMIT_PENDING_IDENTIFIERS] = ArrayList(submittedIdentifiers)
        // NOT durable: a lambda cannot survive process death (SavedStateHandle only stores Bundle-
        // compatible values). `confirmSubmitIndividualScope` no longer treats its absence as a
        // reason to silently skip the submit -- see the comment there.
        submitPendingCallback = onSubmitted
    }

    private var submitPendingCallback: (() -> Unit)? = null

    fun confirmSubmitIndividualScope() {
        // ONLY proceeds when the gate is actually open -- mirrors SubmitViewModel.confirmSubmit's
        // gate-first contract instead of trusting a stashed identifier list to prove it.
        if (!showSubmitConfirmation.value) return
        // RECOMPUTE, never trust a stash. The identifiers used to live ONLY in a plain `var`
        // (`submitPendingIdentifiers`) set once in submitIndividualScope() and read here with
        // `?: return` -- silently doing nothing if it had gone missing. A killed-and-recreated
        // process is exactly the case where that happens: the VM comes back with
        // `showSubmitConfirmation` reset and no memory of which identifiers were armed, so if the
        // recomposed dialog's Confirm ever reached this function the tap did precisely nothing --
        // no error, no submit, no sign anything was wrong. Recomputing from the same durable,
        // Room-observed state `submitIndividualScope` used closes that gap: the identifiers can
        // never be null while the gate is legitimately open.
        val identifiers = computeSubmittableIdentifiers()
        if (identifiers == null) {
            // The scope stopped being submittable between arm and confirm (e.g. a proof upload
            // regressed) -- close the dialog and say so, rather than either submitting a stale
            // list or silently doing nothing.
            showSubmitConfirmation.value = false
            savedStateHandle.remove<ArrayList<String>>(KEY_SUBMIT_PENDING_IDENTIFIERS)
            message.value = "Every scanned RFID in this shed needs saved weight and synced video before submit."
            return
        }
        val callback = submitPendingCallback
        showSubmitConfirmation.value = false
        savedStateHandle.remove<ArrayList<String>>(KEY_SUBMIT_PENDING_IDENTIFIERS)
        submitPendingCallback = null
        actionInFlight.value = true
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_SUBMIT_CONFIRMATION_CONFIRMED,
            weighingCaptureProps(INDIVIDUAL_ANIMAL_CATEGORY),
        )
        analytics.track(
            AnalyticsEvents.WEIGHING_SUBMIT_ATTEMPT,
            weighingCaptureProps(INDIVIDUAL_ANIMAL_CATEGORY),
        )
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_SUBMIT_ATTEMPTED,
            weighingCaptureProps(INDIVIDUAL_ANIMAL_CATEGORY),
        )
        viewModelScope.launch {
            try {
                when (
                    val submitted = repository.submitIndividualScope(
                        campaignId,
                        campaignShedId,
                        identifiers,
                    )
                ) {
                    is AppResult.Ok -> {
                        refreshScopeSubmitted()
                        analytics.track(
                            AnalyticsEvents.WEIGHING_SUBMIT_SUCCESS,
                            weighingCaptureProps(INDIVIDUAL_ANIMAL_CATEGORY),
                        )
                        analytics.track(
                            AnalyticsEventsWeighing.WEIGHING_SUBMIT_SUCCEEDED,
                            weighingCaptureProps(INDIVIDUAL_ANIMAL_CATEGORY),
                        )
                        // The write is now DURABLY QUEUED (outbox), not yet server-confirmed --
                        // WeighingRepository.submitIndividualScope enqueues instead of calling the
                        // network directly, so the sync engine retries it independently even if
                        // this VM/process dies before it drains. If the navigation callback itself
                        // didn't survive (process death between arm and confirm), the submit still
                        // happened; only the auto-navigate is skipped, never the write.
                        if (callback != null) {
                            callback()
                        } else {
                            crashReporter.recordException(
                                IllegalStateException("weighing submit confirmed with no navigation callback"),
                                "weighing individual scope submit succeeded without a live callback",
                            )
                        }
                    }
                    is AppResult.Err -> {
                        // Show the SERVER's reason when it has one. A rework bounce ("a video was
                        // sent back, re-record that animal first") is a 409 the operator can act
                        // on; replacing it with "Try again" hands them advice that can never work
                        // and leaves the shed stuck with no on-screen explanation.
                        message.value = submitted.message.takeIf { it.isNotBlank() }
                            ?: "Couldn't submit this shed. Try again."
                        analytics.track(
                            AnalyticsEvents.WEIGHING_SUBMIT_FAILURE,
                            weighingCaptureProps(INDIVIDUAL_ANIMAL_CATEGORY) +
                                (AnalyticsEvents.Params.REASON to submitted.message.take(MAX_ANALYTICS_REASON_CHARS)),
                        )
                        analytics.track(
                            AnalyticsEventsWeighing.WEIGHING_SUBMIT_FAILED,
                            weighingCaptureProps(INDIVIDUAL_ANIMAL_CATEGORY) +
                                (AnalyticsEvents.Params.REASON to submitted.message.take(MAX_ANALYTICS_REASON_CHARS)),
                        )
                        crashReporter.recordException(
                            submitted.cause ?: IllegalStateException(submitted.message),
                            "weighing individual scope submit failed",
                        )
                    }
                }
            } finally {
                actionInFlight.value = false
            }
        }
    }

    fun dismissSubmitConfirmation() {
        showSubmitConfirmation.value = false
        savedStateHandle.remove<ArrayList<String>>(KEY_SUBMIT_PENDING_IDENTIFIERS)
        submitPendingCallback = null
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_SUBMIT_CONFIRMATION_CANCELLED,
            weighingCaptureProps(INDIVIDUAL_ANIMAL_CATEGORY),
        )
    }

    private fun recordIndividualRow(
        key: String,
        row: WeighingRosterRowEntity,
        useGlobalBusyGate: Boolean = true,
    ) {
        // Defense in depth behind the UI's own canRecordIndividual/canRecordShedPartition gate
        // (WeighingScreen.kt): the durable read-only signal is checked here too, so a stray call
        // reaching this function some other way (e.g. a queued composable callback) cannot write
        // into an already-submitted scope.
        if (scopeSubmitted.value == true) return
        val weightKg = parsePositiveWeighingWeight(animalWeightInputs.value[row.animalId])
            ?: parsePositiveWeighingWeight(weightInput.value)
            ?: return
        if (useGlobalBusyGate && actionInFlight.value) return
        if (row.animalId in updatingWeightAnimalIds.value) return
        val proof = proofForAnimal(row.animalId)
        val weightProps = weighingAnimalProps(row, weightKg, proof)
        analytics.track(AnalyticsEvents.WEIGHING_CAPTURE_ATTEMPT, weightProps)
        analytics.track(AnalyticsEvents.WEIGHING_WEIGHT_CAPTURE_ATTEMPT, weightProps)
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
                        // Clear any previous conflict for this animal (re-capture after failure)
                        conflictedAnimalIds.value = conflictedAnimalIds.value - row.animalId

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
                        // Do NOT show the success message yet - it's only queued locally. The outbox
                        // may reject it with 409 conflict. Instead, show a generic "recording" message
                        // or nothing, and let the sync status tell the truth when the write is accepted
                        // or rejected. For now, show "syncing" to align with the video upload behavior.
                        message.value = if (proof == null) {
                            "Weight queued. Video is still required."
                        } else {
                            "Weight queued. Video continues syncing in the background."
                        }
                        analytics.track(
                            AnalyticsEvents.WEIGHING_CAPTURE_SUCCESS,
                            weightProps,
                        )
                        analytics.track(
                            AnalyticsEvents.WEIGHING_WEIGHT_CAPTURE_SUCCESS,
                            weightProps,
                        )
                        scanCaptureRepository.markLocalScanSynced(
                            taskId = key,
                            fieldKey = WEIGHING_SCAN_FIELD_KEY,
                            tag = row.animalId,
                        )
                        weightInput.value = ""
                        clearAnimalWeightInput(row.animalId)
                        scanInput.value = ""
                        recorded.value
                    }
                    is AppResult.Err -> {
                        updatingWeightAnimalIds.value = updatingWeightAnimalIds.value - row.animalId
                        message.value = recorded.message
                        analytics.track(
                            AnalyticsEvents.WEIGHING_WEIGHT_CAPTURE_FAILURE,
                            weightProps +
                                (AnalyticsEvents.Params.REASON to recorded.message.take(MAX_ANALYTICS_REASON_CHARS)),
                        )
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
        val lumpSumProps = weighingCaptureProps(PER_SHED_PARTITION_CATEGORY) +
            mapOf(
                AnalyticsEvents.Params.WEIGHT_KG to weightKg.toString(),
                AnalyticsEvents.Params.ANIMAL_COUNT to animalCount.toString(),
                AnalyticsEvents.Params.PROOF_CAPTURED to "true",
                AnalyticsEvents.Params.PROOF_UPLOADED to "true",
                AnalyticsEvents.Params.PROOF_ID to syncedProof.id,
            )
        analytics.track(AnalyticsEvents.WEIGHING_CAPTURE_ATTEMPT, lumpSumProps)
        analytics.track(AnalyticsEvents.WEIGHING_WEIGHT_CAPTURE_ATTEMPT, lumpSumProps)
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
                        clearLumpSumInputDraft(lumpSumDraftKey(key))
                        message.value = "Lump-sum weighing submitted."
                        analytics.track(
                            AnalyticsEvents.WEIGHING_CAPTURE_SUCCESS,
                            lumpSumProps,
                        )
                        analytics.track(
                            AnalyticsEvents.WEIGHING_WEIGHT_CAPTURE_SUCCESS,
                            lumpSumProps,
                        )
                        onSubmitted()
                        recorded.value
                    }
                    is AppResult.Err -> {
                        message.value = recorded.message
                        analytics.track(
                            AnalyticsEvents.WEIGHING_WEIGHT_CAPTURE_FAILURE,
                            lumpSumProps +
                                (AnalyticsEvents.Params.REASON to recorded.message.take(MAX_ANALYTICS_REASON_CHARS)),
                        )
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
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_SHED_VIDEO_ACTION_ATTEMPTED,
            shedVideoActionProps(SHED_VIDEO_ACTION_RETRY, proofId),
        )
        viewModelScope.launch {
            try {
                when (val retried = proofCaptureRepository.retryUpload(key, proofId)) {
                    is AppResult.Ok -> {
                        message.value = "Group video retry queued."
                        analytics.track(
                            AnalyticsEventsWeighing.WEIGHING_SHED_VIDEO_ACTION_SUCCEEDED,
                            shedVideoActionProps(SHED_VIDEO_ACTION_RETRY, proofId),
                        )
                    }
                    is AppResult.Err -> {
                        message.value = retried.message
                        analytics.track(
                            AnalyticsEventsWeighing.WEIGHING_SHED_VIDEO_ACTION_FAILED,
                            shedVideoActionProps(SHED_VIDEO_ACTION_RETRY, proofId) +
                                (AnalyticsEvents.Params.REASON to retried.message.take(MAX_ANALYTICS_REASON_CHARS)),
                        )
                    }
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
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_SHED_VIDEO_ACTION_ATTEMPTED,
            shedVideoActionProps(SHED_VIDEO_ACTION_REMOVE, proofId),
        )
        viewModelScope.launch {
            try {
                when (val removed = proofCaptureRepository.remove(key, proofId)) {
                    is AppResult.Ok -> {
                        message.value = "Group video removed."
                        analytics.track(
                            AnalyticsEventsWeighing.WEIGHING_SHED_VIDEO_ACTION_SUCCEEDED,
                            shedVideoActionProps(SHED_VIDEO_ACTION_REMOVE, proofId),
                        )
                    }
                    is AppResult.Err -> {
                        message.value = removed.message
                        analytics.track(
                            AnalyticsEventsWeighing.WEIGHING_SHED_VIDEO_ACTION_FAILED,
                            shedVideoActionProps(SHED_VIDEO_ACTION_REMOVE, proofId) +
                                (AnalyticsEvents.Params.REASON to removed.message.take(MAX_ANALYTICS_REASON_CHARS)),
                        )
                    }
                }
            } finally {
                actionInFlight.value = false
            }
        }
    }

    fun replaceShedVideo(proofId: String) = captureShedVideo(replacingProofId = proofId)

    // Durable across process death via SavedStateHandle -- NOT the file-scope `mutableMapOf` this
    // replaced. A process-wide static map neither survives a killed process (the operator's
    // half-typed lump-sum weight/count silently vanished) nor scopes cleanly to one VM instance
    // (every WeighingViewModel in the process shared the same map, so its entries outlived the
    // screen that wrote them). SavedStateHandle is this VM's own Bundle -- durable across process
    // death, and gone for good once this destination is popped, exactly matching the draft's
    // actual lifetime.
    private fun saveLumpSumInputDraft() {
        val key = scopeKey ?: return
        if (category != PER_SHED_PARTITION_CATEGORY) return
        val weight = weightInput.value
        val animalCount = animalCountInput.value
        val draftKey = lumpSumDraftKey(key)
        if (weight.isBlank() && animalCount.isBlank()) {
            clearLumpSumInputDraft(draftKey)
        } else {
            savedStateHandle[lumpSumWeightKey(draftKey)] = weight
            savedStateHandle[lumpSumCountKey(draftKey)] = animalCount
        }
    }

    private fun restoreLumpSumInputDraft() {
        val key = scopeKey ?: return
        if (category != PER_SHED_PARTITION_CATEGORY) return
        val draftKey = lumpSumDraftKey(key)
        val draftWeight = savedStateHandle.get<String>(lumpSumWeightKey(draftKey))
        val draftCount = savedStateHandle.get<String>(lumpSumCountKey(draftKey))
        if (draftWeight == null && draftCount == null) return
        if (weightInput.value.isBlank()) weightInput.value = draftWeight.orEmpty()
        if (animalCountInput.value.isBlank()) animalCountInput.value = draftCount.orEmpty()
    }

    private fun clearLumpSumInputDraft(draftKey: String) {
        savedStateHandle.remove<String>(lumpSumWeightKey(draftKey))
        savedStateHandle.remove<String>(lumpSumCountKey(draftKey))
    }

    private fun lumpSumDraftKey(scope: String): String =
        listOf(tenantId.ifBlank { "unknown_tenant" }, currentPrincipalId ?: "unknown_principal", scope).joinToString(":")

    private fun lumpSumWeightKey(draftKey: String): String = "weighing.lumpSum.weight:$draftKey"
    private fun lumpSumCountKey(draftKey: String): String = "weighing.lumpSum.count:$draftKey"

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
        val shedVideoAction = if (replacingProofId == null) SHED_VIDEO_ACTION_CAPTURE else SHED_VIDEO_ACTION_REPLACE
        analytics.track(
            AnalyticsEvents.WEIGHING_PROOF_CAPTURE_ATTEMPT,
            weighingCaptureProps(PER_SHED_PARTITION_CATEGORY) +
                mapOf(
                    AnalyticsEvents.Params.OUTCOME to "attempt",
                    AnalyticsEvents.Params.PROOF_ID to replacingProofId.orEmpty(),
                    AnalyticsEvents.Params.PROOF_CAPTURED to "false",
                    AnalyticsEvents.Params.PROOF_UPLOADED to "false",
                ),
        )
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_SHED_VIDEO_ACTION_ATTEMPTED,
            shedVideoActionProps(shedVideoAction, replacingProofId),
        )
        viewModelScope.launch {
            try {
                val slotNumber = if (replacingIndex >= 0) replacingIndex + 1 else existing + 1
                val captured = proofCaptureSource.captureVideo(
                    ProofCaptureContext(
                        title = weighingLumpSumProofTitle(),
                        primaryTag = expectedLocationLabel.ifBlank { routeTitle },
                        secondaryTag = null,
                        workLabel = "Group video $slotNumber of 5",
                    ),
                ) ?: run {
                    analytics.track(
                        AnalyticsEvents.WEIGHING_PROOF_CAPTURE_CANCELLED,
                        weighingCaptureProps(PER_SHED_PARTITION_CATEGORY) +
                            mapOf(
                                AnalyticsEvents.Params.OUTCOME to "cancelled",
                                AnalyticsEvents.Params.REASON to "camera_cancelled",
                            ),
                    )
                    return@launch
                }
                val principalId = currentPrincipalId
                    ?: runCatching {
                        // exception:exempt cached profile fetch; best-effort, null triggers early return
                        bootstrapRepository.operatorProfile()?.operatorId
                    }.getOrNull()
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
                        caption = weighingLumpSumProofCaption(slotNumber),
                        rfidTag = null,
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
                                    analytics.track(
                                        AnalyticsEventsWeighing.WEIGHING_SHED_VIDEO_ACTION_FAILED,
                                        shedVideoActionProps(shedVideoAction, replacingProofId) +
                                            (
                                                AnalyticsEvents.Params.REASON to
                                                    removed.message.take(MAX_ANALYTICS_REASON_CHARS)
                                                ),
                                    )
                                    return@launch
                                }
                            }
                        }
                        message.value = if (replacingProofId == null) {
                            "Group video saved locally."
                        } else {
                            "Group video replaced."
                        }
                        analytics.track(
                            AnalyticsEvents.WEIGHING_PROOF_CAPTURE_SUCCESS,
                            weighingCaptureProps(PER_SHED_PARTITION_CATEGORY) +
                                mapOf(
                                    AnalyticsEvents.Params.OUTCOME to "success",
                                    AnalyticsEvents.Params.PROOF_ID to proof.value.id,
                                    AnalyticsEvents.Params.PROOF_CAPTURED to "true",
                                    AnalyticsEvents.Params.PROOF_UPLOADED to (proof.value.syncStatus == CaptureSyncStatus.SYNCED).toString(),
                                ),
                        )
                        analytics.track(
                            AnalyticsEventsWeighing.WEIGHING_SHED_VIDEO_ACTION_SUCCEEDED,
                            shedVideoActionProps(shedVideoAction, replacingProofId ?: proof.value.id),
                        )
                    }
                    is AppResult.Err -> {
                        message.value = proof.message
                        analytics.track(
                            AnalyticsEvents.WEIGHING_PROOF_CAPTURE_FAILURE,
                            weighingCaptureProps(PER_SHED_PARTITION_CATEGORY) +
                                mapOf(
                                    AnalyticsEvents.Params.OUTCOME to "failure",
                                    AnalyticsEvents.Params.REASON to proof.message.take(MAX_ANALYTICS_REASON_CHARS),
                                ),
                        )
                        analytics.track(
                            AnalyticsEventsWeighing.WEIGHING_SHED_VIDEO_ACTION_FAILED,
                            shedVideoActionProps(shedVideoAction, replacingProofId) +
                                (AnalyticsEvents.Params.REASON to proof.message.take(MAX_ANALYTICS_REASON_CHARS)),
                        )
                    }
                }
            } finally {
                actionInFlight.value = false
            }
        }
    }

    private fun reportReadFailure(reason: String, throwable: Throwable? = null) {
        message.value = reason.toWeighingReadMessage()
        throwable?.let { crashReporter.recordException(it, "weighing refresh failed") }
        analytics.track(
            AnalyticsEvents.WEIGHING_READ_FAILURE,
            buildMap {
                put(AnalyticsEvents.Params.REASON, reason.take(MAX_ANALYTICS_REASON_CHARS))
                put(AnalyticsEvents.Params.CATEGORY, category.ifBlank { "root" })
            },
        )
    }

    private fun reportCaptureFailure(category: String, reason: String) {
        if (!reason.isExpectedWeighingCaptureState()) {
            crashReporter.recordException(IllegalStateException(reason), "weighing capture failed")
        }
        analytics.track(
            AnalyticsEvents.WEIGHING_CAPTURE_FAILURE,
            weighingCaptureProps(category) + (AnalyticsEvents.Params.REASON to reason.take(MAX_ANALYTICS_REASON_CHARS)),
        )
    }

    /**
     * Makes a struggling proof upload VISIBLE.
     *
     * The 2026-08-03 phone-QA blocker (a Growth Director's shed proof 403'd on every
     * `POST /app/proofs/uploads`, so Submit stayed disabled forever) produced ZERO app-side log
     * lines across 6000 lines of logcat: the retry loop lived entirely inside the outbox, and the
     * only UI was the word "uploading". Diagnosis needed the server log and manual DB forensics.
     *
     * A proof row that is still non-terminal but already carries a `lastError` IS a retry — that
     * is the signal that was invisible. Emitting it (once per DISTINCT failure, keyed by proof id
     * + message, so a Room re-emission of the same state does not inflate the funnel) plus a
     * Crashlytics non-fatal on the terminal FAILED state gives enough context to diagnose from a
     * dashboard: which lane (shed vs per-animal), which campaign shed, which attempt, what cause.
     *
     * Goat identifiers are livestock data and are safe to carry; no token or credential is ever
     * put in props, and the reason string is truncated like every other reason field here.
     */
    private fun reportProofUploadTrouble(proofs: List<ProofCaptureRow>) {
        proofs.forEach { proof ->
            val reason = proof.lastError?.takeIf { it.isNotBlank() } ?: return@forEach
            val terminal = proof.syncStatus == CaptureSyncStatus.FAILED
            val signature = "${proof.id}|$reason|$terminal"
            if (!reportedProofUploadTrouble.add(signature)) return@forEach
            val attempt = proofUploadAttempts.merge(proof.id, 1, Int::plus) ?: 1
            val props = buildMap {
                put(AnalyticsEvents.Params.PROOF_ID, proof.id)
                put(
                    AnalyticsEvents.Params.SUBJECT_TYPE,
                    if (proof.fieldKey == SHED_PARTITION_PROOF_FIELD_KEY) "shed" else "other",
                )
                put(AnalyticsEvents.Params.SHED_ID, campaignShedId)
                put(AnalyticsEvents.Params.ITEM_ID, scopeKey.orEmpty())
                put(AnalyticsEvents.Params.ATTEMPT, attempt.toString())
                put(AnalyticsEvents.Params.REASON, reason.take(MAX_ANALYTICS_REASON_CHARS))
            }
            if (terminal) {
                crashReporter.recordException(
                    IllegalStateException(reason),
                    "weighing proof upload failed",
                )
                analytics.track(AnalyticsEvents.WEIGHING_PROOF_UPLOAD_FAILED, props)
            } else {
                analytics.track(AnalyticsEvents.WEIGHING_PROOF_UPLOAD_RETRY, props)
            }
        }
    }

    private fun weighingCaptureProps(captureCategory: String): Map<String, String> =
        buildMap {
            put(AnalyticsEvents.Params.CATEGORY, captureCategory)
            put(AnalyticsEvents.Params.ITEM_ID, scopeKey.orEmpty())
            put(AnalyticsEvents.Params.CAMPAIGN_ID, campaignId)
            put(AnalyticsEvents.Params.CAMPAIGN_SHED_ID, campaignShedId)
            put(AnalyticsEvents.Params.SHED_ID, campaignShedId)
            expectedLocationId.takeIf(String::isNotBlank)?.let { put(AnalyticsEvents.Params.PARTITION_ID, it) }
            expectedLocationLabel.takeIf(String::isNotBlank)?.let { put(AnalyticsEvents.Params.PARTITION_LABEL, it) }
        }

    private fun weighingAnimalProps(
        row: WeighingRosterRowEntity,
        weightKg: Double,
        proof: ProofCaptureRow?,
    ): Map<String, String> =
        weighingCaptureProps(INDIVIDUAL_ANIMAL_CATEGORY) +
            mapOf(
                AnalyticsEvents.Params.RFID to row.primaryTag.ifBlank { row.animalId },
                AnalyticsEvents.Params.GOAT_ID to row.animalId,
                AnalyticsEvents.Params.WEIGHT_KG to weightKg.toString(),
                AnalyticsEvents.Params.PROOF_CAPTURED to (proof != null).toString(),
                AnalyticsEvents.Params.PROOF_UPLOADED to (proof?.syncStatus == CaptureSyncStatus.SYNCED).toString(),
            ) +
            (proof?.id?.let { mapOf(AnalyticsEvents.Params.PROOF_ID to it) } ?: emptyMap())

    private fun weighingProofProps(row: WeighingRosterRowEntity, proof: ProofCaptureRow?): Map<String, String> =
        weighingCaptureProps(INDIVIDUAL_ANIMAL_CATEGORY) +
            mapOf(
                AnalyticsEvents.Params.RFID to row.primaryTag.ifBlank { row.animalId },
                AnalyticsEvents.Params.GOAT_ID to row.animalId,
                AnalyticsEvents.Params.PROOF_CAPTURED to (proof != null).toString(),
                AnalyticsEvents.Params.PROOF_UPLOADED to (proof?.syncStatus == CaptureSyncStatus.SYNCED).toString(),
            ) +
            (proof?.id?.let { mapOf(AnalyticsEvents.Params.PROOF_ID to it) } ?: emptyMap())

    private fun trackWeighingScan(
        rfid: String,
        row: WeighingRosterRowEntity?,
        outcome: String,
        reason: String,
    ) {
        analytics.track(
            AnalyticsEvents.WEIGHING_SCAN,
            weighingCaptureProps(INDIVIDUAL_ANIMAL_CATEGORY) +
                buildMap {
                    put(AnalyticsEvents.Params.RFID, rfid)
                    row?.animalId?.takeIf(String::isNotBlank)?.let { put(AnalyticsEvents.Params.GOAT_ID, it) }
                    put(AnalyticsEvents.Params.OUTCOME, outcome)
                    put(AnalyticsEvents.Params.REASON, reason)
                    put(AnalyticsEvents.Params.PROOF_CAPTURED, (row?.let { proofForAnimal(it.animalId) } != null).toString())
                    put(
                        AnalyticsEvents.Params.PROOF_UPLOADED,
                        (row?.let { proofForAnimal(it.animalId)?.syncStatus } == CaptureSyncStatus.SYNCED).toString(),
                    )
                },
        )
    }

    private fun shedVideoActionProps(action: String, proofId: String?): Map<String, String> =
        buildMap {
            put(AnalyticsEvents.Params.CATEGORY, action)
            put(AnalyticsEvents.Params.ITEM_ID, scopeKey.orEmpty())
            put(AnalyticsEvents.Params.SHED_ID, campaignShedId)
            proofId?.let { put(AnalyticsEvents.Params.PROOF_ID, it) }
        }

    override fun onCleared() {
        readerRefreshJob?.cancel()
        reader.setCompletionKeySwallowEnabled(false)
        reader.setCaptureEnabled(false)
    }

    /**
     * Namespaces a scanned tag to THIS shed, in dev builds only.
     *
     * A tester has a handful of physical tags and many sheds to walk, so the same tag is read in
     * every one of them. Weighing accepts that by design -- its duplicate rule is scoped per shed
     * (migration 000073: "a goat is not fenced to one shed by this table") -- but everything
     * downstream then sees ONE animal weighed several times a day, which is exactly how a 15 kg
     * reading in one shed and an 11 kg reading in another became a herd growth figure.
     *
     * Prefixing with the shed's own location id keeps the physical tag readable at the end while
     * making each shed's read a distinct identifier, so 5 tags behave like 5 animals PER shed.
     * Gated on the dev flavour's BuildConfig field, so stg and prod never compile it in and real
     * scans are never rewritten.
     */
    private fun scopedScanIdentifier(tag: String): String {
        if (!(scanScopePrefixOverride ?: BuildConfig.SCAN_SCOPE_PREFIX)) return tag
        val shed = expectedLocationId.takeIf { it.isNotBlank() } ?: return tag
        val trimmed = tag.trim()
        if (trimmed.isEmpty()) return tag
        val namespace = shed.filter { it.isLetterOrDigit() }.takeLast(6).uppercase()
        if (namespace.isEmpty() || trimmed.startsWith("$namespace-")) return trimmed
        return "$namespace-$trimmed"
    }

    private fun matchTag(rawTag: String, fromTypedEntry: Boolean = false) {
        val tag = scopedScanIdentifier(rawTag)
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
                trackWeighingScan(normalizedTag, existingRow, "accepted", "proof_replace_requested")
                proofReplacementAnimalId.value = null
                message.value = null
                captureVideoForRow(key, existingRow)
                return@launch
            }
            // A video that already reached the SERVER is evidence, even when this phone no longer
            // holds the local capture row.
            //
            // proofForAnimal() reads only local proof captures, so anything that clears local state
            // -- reinstall, app-data clear, cache eviction -- made every already-submitted animal
            // read as "no video" and sent the operator straight back into the camera. Re-recording
            // then REPLACED a video that was already submitted and waiting on the verifier, and
            // flipped that animal back to unsubmitted. Absence of the local cache is not absence of
            // evidence; the draft carries the server proof id precisely so this is answerable
            // offline.
            val serverProofForTag = scopeState.value?.individualDrafts.orEmpty()
                .firstOrNull { normalizeFreeFlowTag(it.scannedIdentifier) == normalizedTag }
                ?.serverProofId
                ?.takeIf { it.isNotBlank() }
            if (existingRow != null && serverProofForTag == null && proofForAnimal(existingRow.animalId) == null) {
                trackWeighingScan(normalizedTag, existingRow, "accepted", "proof_rescan")
                message.value = null
                captureVideoForRow(key, existingRow)
                return@launch
            }
            // A SENT-BACK animal is work, not a duplicate.
            //
            // The verifier rejecting a weighing proof puts that observation into rework and hands
            // the shed back to the operator to redo. The local draft still exists and still carries
            // the tag, so this guard -- which looked only at the tag -- refused the re-scan with
            // "Already scanned" and left the operator with no way to record the new video. The tag
            // is genuinely the same animal; that is the point of a re-shoot.
            //
            // Same rule vaccination already settled (ExecutionRepository.isServerDone): the STATUS
            // decides, not the fact that a scan once happened. A rejected capture keeps its history
            // forever and must never read as done.
            val drafts = scopeState.value?.individualDrafts.orEmpty()
            val sentBackDraft = drafts.firstOrNull { draft ->
                normalizeFreeFlowTag(draft.scannedIdentifier) == normalizedTag &&
                    draft.verificationStatus == WEIGHING_VERIFICATION_REWORK
            }
            if (sentBackDraft != null) {
                if (!fromTypedEntry) scanInput.value = normalizedTag
                message.value = null
                val reworkRow = unknownWeighingRow(key, normalizedTag)
                trackWeighingScan(normalizedTag, reworkRow, "accepted", "rework_rescan")
                selectedRow.value = reworkRow
                persistSelectedRow(reworkRow.animalId)
                captureVideoForRow(key, reworkRow)
                return@launch
            }
            val alreadyRecorded = drafts.any { draft ->
                normalizeFreeFlowTag(draft.scannedIdentifier) == normalizedTag
            }
            if (alreadyRecorded || existingRow != null) {
                // Only a READER read echoes into the field. A typed entry was already cleared
                // synchronously by submitTypedScan; re-clearing here lands AFTER a suspending DB
                // write and would wipe whatever the operator has since typed for the NEXT animal.
                if (!fromTypedEntry) scanInput.value = normalizedTag
                message.value = "Already scanned · $normalizedTag"
                trackWeighingScan(normalizedTag, existingRow, "duplicate", "already_scanned")
                return@launch
            }
            val inserted = scanCaptureRepository.recordLocalScanIfAbsent(
                taskId = key,
                fieldKey = WEIGHING_SCAN_FIELD_KEY,
                tag = normalizedTag,
            )
            // A reader read echoes the tag so the operator can see what the gun picked up. A TYPED
            // entry is left alone: submitTypedScan already cleared the field synchronously, and
            // assigning here -- after a suspending DB write -- would overwrite the keystrokes the
            // operator has since typed for the next animal.
            if (!fromTypedEntry) scanInput.value = normalizedTag
            if (!inserted) {
                message.value = "Already scanned · $normalizedTag"
                trackWeighingScan(normalizedTag, null, "duplicate", "local_scan_exists")
                return@launch
            }
            val row = unknownWeighingRow(key, normalizedTag)
            selectedRow.value = row
            persistSelectedRow(row.animalId)
            message.value = "RFID captured. Record video, then enter weight."
            trackWeighingScan(normalizedTag, row, "accepted", "accepted")
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
        // Actually open the camera. Marking the row as awaiting a replacement changed a flag and
        // nothing else, so Re-upload was an inert button: the operator tapped it, saw no camera and
        // no message, and had no way to replace a video the verifier had sent back. Recording the
        // new video IS the action the control names.
        val key = scopeKey ?: return
        captureVideoForRow(key, row)
    }

    /** Opens the video camera for [row]. Only one animal's video can be RECORDING at a time — the
     *  camera is one physical device pointed at one animal.
     *
     *  A scan of a DIFFERENT animal while the current animal's video is still being recorded must
     *  NEVER cancel that recording — that destroys unrecoverable field footage, the confirmed shed
     *  defect ("RFID scan mid-recording stops recording and exits the camera"). Instead the new
     *  animal is QUEUED in [pendingProofRow]: its camera opens automatically once the in-flight
     *  job's `finally` runs (capture saved or failed), so the operator can scan ahead without
     *  losing the current animal's video or having to remember to rescan. Only the LAST queued
     *  animal survives a later scan — an earlier queued-but-not-yet-opened one is overtaken and
     *  its camera will never open, which is a genuine drop and is surfaced, never silent. */
    private fun captureVideoForRow(key: String, row: WeighingRosterRowEntity) {
        val strandedAnimalId = proofCaptureAnimalId
        if (strandedAnimalId != null) {
            if (strandedAnimalId == row.animalId) {
                // The same animal was re-scanned mid-recording — nothing to do.
                message.value = "Finish the current animal's video first."
                return
            }
            // A DIFFERENT animal was scanned while a capture is still in flight for another one.
            // The camera can never be cancelled to serve this new scan, so queue it instead.
            val replaced = pendingProofRow
            if (replaced != null && replaced.animalId != row.animalId) {
                analytics.track(
                    AnalyticsEvents.PROOF_CAPTURE_SCAN_DROPPED,
                    mapOf(AnalyticsEvents.Params.KIND to "weighing"),
                )
                message.value = "${replaced.displayAnimalId} still needs its video — dropped for ${row.displayAnimalId}."
            } else {
                message.value = "${row.displayAnimalId} queued — camera opens once $strandedAnimalId's video is saved."
            }
            pendingProofRow = row
            analytics.track(
                AnalyticsEvents.PROOF_CAPTURE_SCAN_DEFERRED,
                mapOf(
                    AnalyticsEvents.Params.KIND to "weighing",
                    AnalyticsEvents.Params.REASON to "recording_in_progress",
                ),
            )
            return
        } else if (actionInFlight.value) {
            return
        }
        actionInFlight.value = true
        proofCaptureAnimalId = row.animalId
        proofCaptureVideoCaptured = false
        analytics.track(
            AnalyticsEvents.WEIGHING_PROOF_CAPTURE_ATTEMPT,
            weighingProofProps(row, null) +
                (AnalyticsEvents.Params.OUTCOME to "attempt"),
        )
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
                        analytics.track(
                            AnalyticsEvents.WEIGHING_PROOF_CAPTURE_SUCCESS,
                            weighingProofProps(row, proof.value) +
                                (AnalyticsEvents.Params.OUTCOME to "success"),
                        )
                    }
                    is AppResult.Err -> {
                        message.value = "RFID captured. Video proof is still required."
                        analytics.track(
                            if (proof.message == "missing_video") {
                                AnalyticsEvents.WEIGHING_PROOF_CAPTURE_CANCELLED
                            } else {
                                AnalyticsEvents.WEIGHING_PROOF_CAPTURE_FAILURE
                            },
                            weighingProofProps(row, null) +
                                mapOf(
                                    AnalyticsEvents.Params.OUTCOME to if (proof.message == "missing_video") "cancelled" else "failure",
                                    AnalyticsEvents.Params.REASON to proof.message.take(MAX_ANALYTICS_REASON_CHARS),
                                ),
                        )
                        if (proof.message != "missing_video") {
                            reportCaptureFailure(INDIVIDUAL_ANIMAL_CATEGORY, proof.message)
                        }
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
                    // The camera just freed up — open a queued animal's camera now instead of
                    // stranding it until another scan happens to arrive.
                    val queued = pendingProofRow
                    pendingProofRow = null
                    if (queued != null) {
                        captureVideoForRow(key, queued)
                    }
                }
            }
        }
        proofCaptureJob = job
        job.start()
    }

    private suspend fun captureProofForRow(key: String, row: WeighingRosterRowEntity): AppResult<ProofCaptureRow> {
        val captured = proofCaptureSource.captureVideo(
            ProofCaptureContext(
                title = weighingIndividualProofTitle(row),
                primaryTag = row.primaryTag.ifBlank { row.displayAnimalId },
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
            ?: runCatching {
                // exception:exempt cached profile fetch; best-effort, null triggers error response
                bootstrapRepository.operatorProfile()?.operatorId
            }.getOrNull()
                ?.takeIf { it.isNotBlank() }
                ?.also { currentPrincipalId = it }
            ?: return AppResult.Err("missing_operator")
        return proofCaptureRepository.capture(
            taskId = key,
            fieldKey = INDIVIDUAL_PROOF_FIELD_KEY,
            subject = ProofSubject.OTHER,
            // Free-flow weighing rows do not carry a backend goat UUID. The scanned RFID is kept
            // in the burned overlay, caption, analytics, and proof metadata; sending it as
            // subject_id makes /app/proofs/uploads reject the already-processed file as invalid.
            subjectId = null,
            localUri = captured.localUri,
            mimeType = captured.mimeType,
            caption = weighingIndividualProofCaption(row),
            rfidTag = row.primaryTag.takeIf { it.isNotBlank() },
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
        operatorSummaries: List<WeighingOperatorSummary>,
        loading: Boolean,
        appendingAssignments: Boolean,
        isPlanner: Boolean,
        selectedParkId: String?,
        hasLoadedOnce: Boolean,
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
            // reopen exactly where the write would be accepted.
            canEndWeighing = capabilities.canEnd,
            canReopenWeighing = capabilities.canReopen,
            // Backend-owned per-person tallies, handed to the screen untouched. Deliberately NOT
            // rebuilt from `availableAssignments`: that list is one keyset page.
            operatorSummaries = operatorSummaries.map { it.toUiRow() },
            loading = loading,
            assignmentsLoadingMore = appendingAssignments,
            category = category,
            plannerMode = isPlanner,
            readerConnection = readerConnection,
            shedProofs = proofs.toShedProofUiRows(),
            hasLoadedOnce = hasLoadedOnce,
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
                sentBack = draft?.verificationStatus == WEIGHING_VERIFICATION_REWORK,
                sentBackReason = draft?.reworkReason,
                weightSyncConflict = row.animalId in conflictedAnimalIds.value,
            )
        }

    private fun scanTimeLabel(epochMs: Long): String =
        runCatching {
            // exception:exempt timestamp display fallback; format failure shows generic label
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
        // A shed video is revived only while this scope still holds an OPEN round. Reviving
        // every synced shed proof brought back ones a reopen had superseded, which filled the
        // 5-video cap with dead clips and blocked the operator from filming the new one.
        val hasOpenShedRound = scope?.shedDrafts.orEmpty().isNotEmpty()
        return proofs.filter { proof ->
            proof.syncStatus != CaptureSyncStatus.SYNCED ||
                proof.id in activeIds ||
                (hasOpenShedRound && proof.belongsToThisShedScope())
        }
    }

    /**
     * A shed/lump-sum video belongs to this scope on its own evidence, not on session memory.
     *
     * sessionProofIds is in-heap and individualDrafts only ever carries INDIVIDUAL proof ids, so a
     * shed video that finished syncing was dropped from the active list the moment the process
     * restarted. Nothing repopulated it: the screen said "No video added yet" and
     * recordShedPartition refused to submit, while the video sat in Room SYNCED with a
     * serverProofId and its file was already on the server. The operator's proof was unreachable
     * with no way to recover short of filming it again.
     */
    /** Caller gates this on an open round; see [activeWeighingProofs]. */
    private fun ProofCaptureRow.belongsToThisShedScope(): Boolean =
        fieldKey == SHED_PARTITION_PROOF_FIELD_KEY &&
            subjectId == expectedLocationId &&
            expectedLocationId.isNotBlank()

    private suspend fun publishActiveProofs(scope: String, proofs: List<ProofCaptureRow>) {
        val activeProofs = activeWeighingProofs(proofs, scopeState.value)
        reportProofUploadTrouble(activeProofs)
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

    // A proof belongs to an animal because it NAMES that animal -- not because it happens to be
    // mid-upload. The fallback branch used to require syncStatus != SYNCED, so a video matched its
    // row only while it was still uploading: the instant the upload finished the row stopped
    // seeing its own proof and went back to "Proof required", with the file sitting completed on
    // the server. The operator is then told to re-shoot a video that already exists -- observed on
    // 2026-08-08 with three proof_artifacts at upload_state='completed' against a row asking for
    // proof.
    //
    // Ownership is the caption/subject match. Upload status is rendered separately (uploading /
    // synced / failed) and must not decide whether the proof is FOUND.
    private fun ProofCaptureRow.matchesAnimalProof(animalId: String, draftProofId: String?): Boolean =
        if (draftProofId != null) {
            id == draftProofId
        } else {
            rfidTag == animalId || caption == animalId || caption?.endsWith(" · $animalId") == true || subjectId == animalId
        }

    private fun weighingIndividualProofCaption(row: WeighingRosterRowEntity): String =
        weighingIndividualProofTitle(row)

    private fun weighingIndividualProofTitle(row: WeighingRosterRowEntity): String =
        listOf(
            "Weighing",
            weighingOverlayParkLabel().takeIf { it.isNotBlank() },
            row.expectedLocationLabel.ifBlank { expectedLocationLabel.ifBlank { routeTitle } }.takeIf { it.isNotBlank() },
        )
            .filterNotNull()
            .joinToString(" . ")

    private fun weighingLumpSumProofCaption(slotNumber: Int): String =
        listOf(
            weighingLumpSumProofTitle(),
            "video $slotNumber",
        )
            .filterNotNull()
            .joinToString(" · ")

    private fun weighingLumpSumProofTitle(): String =
        listOf(
            "Weighing",
            weighingOverlayParkLabel().takeIf { it.isNotBlank() },
            expectedLocationLabel.ifBlank { routeTitle }.takeIf { it.isNotBlank() },
        )
            .filterNotNull()
            .joinToString(" . ")

    private fun weighingOverlayParkLabel(): String =
        parkLabel
            .ifBlank { knownAssignmentParks.value[selectedAssignmentParkId.value].orEmpty() }
            .ifBlank { knownTaskParks.value[selectedAssignmentParkId.value].orEmpty() }

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

    internal companion object {
        /**
         * Test-only override for the dev-flavour scan namespacing in [scopedScanIdentifier].
         *
         * A companion field, NOT a constructor parameter: this ViewModel is built by Hilt via
         * @Inject and Hilt has no binding for a bare Boolean, so a constructor flag breaks the
         * Dagger build. Unit tests run on the DEV variant where the namespacing is on, so
         * without this they assert the test-rig-prefixed identifier instead of the real one.
         */
        @JvmStatic
        internal var scanScopePrefixOverride: Boolean? = null

        const val KEY_SUBMIT_PENDING_IDENTIFIERS = "weighing.submitPendingIdentifiers"
        // Durable across process death via SavedStateHandle -- same reasoning as the lump-sum
        // draft above: the roster window this map can hold typed-but-unsubmitted weights for is
        // capped at ROSTER_WINDOW_SIZE (20) visible rows, and every entry is removed from the map
        // the moment recordIndividualRow() successfully commits it to Room (see that function),
        // so the steady-state size is a handful of in-progress entries, not the whole scope's
        // roster. That keeps it well within Bundle size limits, unlike the Room-backed
        // individualDrafts table which stores the COMMITTED, already-submitted captures.
        const val KEY_ANIMAL_WEIGHT_INPUT_IDS = "weighing.animalWeightInputs.ids"
        const val KEY_ANIMAL_WEIGHT_INPUT_VALUES = "weighing.animalWeightInputs.values"
        const val KEY_SELECTED_ROW_ANIMAL_ID = "weighing.selectedRow.animalId"
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
        const val SHED_VIDEO_ACTION_RETRY = "retry"
        const val SHED_VIDEO_ACTION_REMOVE = "remove"
        const val SHED_VIDEO_ACTION_CAPTURE = "capture"
        const val SHED_VIDEO_ACTION_REPLACE = "replace"
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

private fun WeighingOperatorSummary.toUiRow(): WeighingOperatorUiRow =
    WeighingOperatorUiRow(
        operatorUserId = operatorUserId,
        name = operatorDisplayName,
        shedCount = shedCount,
        animalsWeighed = animalsWeighed,
        animalsSubmitted = animalsSubmitted,
        notStarted = notStarted,
        capturing = capturing,
        submitted = submitted,
        accepted = accepted,
        rework = rework,
    )

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
        operatorName = operatorDisplayName,
        backendStatus = status,
        status = status.readableWeighingStatus(),
        periodLabel = periodLabel.readableWeighingPeriodLabel(),
        readyToClose = readyToClose,
        pendingVerificationCount = pendingVerificationCount,
        reworkCount = reworkCount,
        latestReworkReason = latestReworkReason,
        plannedBusinessDate = plannedBusinessDate,
        dueBusinessDate = dueBusinessDate,
    )

private fun WeighingAssignment.assignmentIdentityKey(): String =
    listOf(campaignId, workGroupId, campaignShedId, category, periodLabel)
        .joinToString(":")

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
        // exception:exempt period label display; parsing failure shows raw input
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
    /** Backend-owned per-person tallies for the current park filter. Never page-derived. */
    val operatorSummaries: List<WeighingOperatorSummary> = emptyList(),
    /** True once an assignments fetch for the CURRENT park filter has completed at least once. */
    val hasLoadedOnce: Boolean = false,
)

private data class AssignmentParkSelection(
    val assignments: List<WeighingAssignment>,
    val selectedParkId: String?,
    val appending: Boolean = false,
    val knownParks: Map<String, String> = emptyMap(),
    val operatorSummaries: List<WeighingOperatorSummary> = emptyList(),
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

/**
 * Reconstructs the typed-but-unsubmitted per-animal weight map from [SavedStateHandle], parallel
 * ArrayList<String> under [WeighingViewModel.Companion.KEY_ANIMAL_WEIGHT_INPUT_IDS] /
 * [WeighingViewModel.Companion.KEY_ANIMAL_WEIGHT_INPUT_VALUES] rather than a single Bundle of
 * Map<String, String> -- SavedStateHandle/Bundle has no direct Map<String, String> putter, and
 * parallel lists round-trip through Bundle without a custom Parcelable.
 */
internal fun restoreAnimalWeightInputs(savedStateHandle: SavedStateHandle): Map<String, String> {
    val ids = savedStateHandle.get<ArrayList<String>>(WeighingViewModel.KEY_ANIMAL_WEIGHT_INPUT_IDS).orEmpty()
    val values = savedStateHandle.get<ArrayList<String>>(WeighingViewModel.KEY_ANIMAL_WEIGHT_INPUT_VALUES).orEmpty()
    if (ids.isEmpty() || ids.size != values.size) return emptyMap()
    return ids.zip(values).toMap()
}

private fun normalizeFreeFlowTag(tag: String): String =
    tag.filter { it.isLetterOrDigit() }.lowercase()


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

// Mirrors OutboxOpType.WEIGHING_ANIMAL_OBSERVATION.name. Compared as a string deliberately: the
// sync port hands ViewModels a String opType precisely so `:app` never depends on core-database's
// Room types (module boundary: feature-*/:app -> core-*, never straight to Room).
private const val WEIGHING_ANIMAL_OBSERVATION_OP = "WEIGHING_ANIMAL_OBSERVATION"
private const val WEIGHING_SCOPE_SUBMIT_OP = "WEIGHING_SCOPE_SUBMIT"


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

internal fun WeighingTask?.toTaskDetailUiState(
    campaignId: String,
    selectedOperatorId: String?,
    operatorNames: Map<String, String>,
    capabilities: WeighingCapabilities,
    buckets: WeighingTaskBucketCache,
    loading: Boolean,
    busy: Boolean,
    staleNotice: String,
    exportingCsv: Boolean = false,
): WeighingTaskDetailUiState {
    if (this == null) {
        return WeighingTaskDetailUiState(campaignId = campaignId, found = false, loading = loading, busy = busy)
    }
    val date = runCatching {
        // exception:exempt date parsing for display; null fallback shows raw date
        LocalDate.parse(weighDate, weighingIsoDateFormatter)
    }.getOrNull()
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
            // Name and count travel SEPARATELY: the screen owns the words, so it can say what
            // the number is a count OF. Pre-joining them rendered as "Dinakar 2", which reads as
            // part of a person's name rather than as the two sheds he owns.
            WeighingOperatorFilterUiRow(
                id = id,
                name = labelFor(operatorUserId, rows),
                shedCount = rows.size,
                selected = selectedOperatorId == id,
            )
        }
        .sortedBy { it.name }
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
        // "Open" is work the FARM still owes: a bucket nobody has submitted yet. A bucket the
        // operator submitted is not open work — it is waiting on a verifier, a different queue —
        // so it is counted and NAMED separately. Folding the two together is what made the close
        // button read "4 still open" beside two cards that plainly said "waiting for verifier".
        // Both numbers are buckets, never animals.
        // A bucket a verifier bounced back counts as OPEN, not as awaiting a verifier: the work is
        // sitting with the operator again, which is exactly what its card says.
        openBucketCount = sheds.count {
            !it.status.equalsWeighingStatus("closed") &&
                (!it.status.equalsWeighingStatus("completed") || it.reworkCount > 0)
        },
        awaitingVerificationBucketCount = sheds.count {
            it.status.equalsWeighingStatus("completed") && it.reworkCount == 0
        },
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
        canExportCsv = capabilities.canExportCsv,
        exportingCsv = exportingCsv,
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
        reworked = reworked,
        // The TWO named backend facts: animals put on the scale, and the subset of those actually
        // submitted for verification. Both reported as-is and neither is ever a numerator:
        // weighing is free-flow, so there is no expected-animal total a share could be taken of,
        // and one is never divided by the other.
        animalsWeighedCount = animalsWeighedCount,
        animalsSubmittedCount = animalsSubmittedCount,
        // How far along the bucket's own state ladder it stands, as a STEP out of
        // [WEIGHING_BUCKET_LADDER_STEPS] discrete states — not a fraction. A part-filled bar was
        // read on the farm as "70% of the animals done", which is a number weighing cannot have.
        ladderStep = when {
            // Bounced work is back at the capture rung, which is where its card says it is.
            reworked -> 1
            normalized == "closed" -> WEIGHING_BUCKET_LADDER_STEPS
            normalized == "completed" -> 2
            normalized == "in_progress" -> 1
            else -> 0
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
    val date = runCatching {
        // exception:exempt date parsing for display; null fallback shows raw date
        LocalDate.parse(weighDate, weighingIsoDateFormatter)
    }.getOrNull()
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
    /** True while the CSV export download is in flight. */
    val exportingCsv: Boolean = false,
)

/** The weigh date as a person reads it, never the machine form. */
private fun WeighingTask.weighDateLabel(): String = runCatching {
    // exception:exempt date display formatter; unparseable date shows raw ISO string
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

private fun String.isExpectedWeighingCaptureState(): Boolean =
    equals("missing_video", ignoreCase = true) ||
        equals("cancelled", ignoreCase = true) ||
        equals("capture_cancelled", ignoreCase = true)

/** Reason CODES for ending a task. The backend owns the sentence that is recorded. */
private const val CLOSE_REASON_ALL_ACCEPTED = "all_buckets_accepted"
private const val CLOSE_REASON_OPEN_BUCKETS = "open_buckets_closed"

/**
 * Groups the export's flat row list by shed, PRESERVING the backend's own row order within and
 * across sheds -- the CSV is already ordered by park/shed, and re-sorting here would make the
 * preview disagree with the file a planner can also open directly.
 */
private fun List<WeighingCsvExportRow>.toExportPreviewSheds(): List<WeighingExportPreviewShedUi> {
    val order = LinkedHashMap<String, MutableList<WeighingCsvExportRow>>() // mobile-guard:ignore: function-local, bounded by the CSV row list passed into this call and discarded on return
    for (row in this) {
        // Key by (park, shedId) — shedId is globally unique; shedName repeats across parks.
        val key = "${row.park}|${row.shedId}"
        order.getOrPut(key) { mutableListOf() }.add(row)
    }
    return order.entries.map { (key, rows) ->
        val first = rows.first()
        WeighingExportPreviewShedUi(
            shedKey = key,
            park = first.park,
            shedName = first.shedName,
            shedStatus = first.shedStatus,
            rows = rows.map { it.toPreviewRowUi() },
        )
    }
}

private fun WeighingCsvExportRow.toPreviewRowUi(): WeighingExportPreviewRowUi = WeighingExportPreviewRowUi(
    type = type,
    scannedIdentifier = scannedIdentifier,
    weightKg = weightKg,
    averageWeightKg = averageWeightKg,
    animalCount = animalCount,
    verificationStatus = verificationStatus,
    proofReferenceType = proofReferenceType,
    proofReference = proofReference,
    recordedAt = recordedAt,
    dateIst = dateIst,
    timeIst = timeIst,
)


// The verifier verdict that means "this animal must be captured again". Matching the wire
// value in one place keeps the row banner and the submit refusal talking about the same state.
private const val WEIGHING_VERIFICATION_REWORK = "rework"
