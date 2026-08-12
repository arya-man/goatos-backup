package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsWeighing
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.weighing.WeighingPlanDraft
import sg.mesha.goatos.core.data.weighing.WeighingPlannerCatalog
import sg.mesha.goatos.core.data.weighing.WeighingPlannerPark
import sg.mesha.goatos.core.data.weighing.WeighingPlannerOperator
import sg.mesha.goatos.core.data.weighing.WeighingPlannerShed
import sg.mesha.goatos.core.data.weighing.WEIGHING_LEADERSHIP_PAGE_SIZE
import sg.mesha.goatos.core.data.weighing.WeighingRepository
import sg.mesha.goatos.core.network.WEIGHING_PAGE_SIZE
import sg.mesha.goatos.feature.weighing.plan.WeighingBucketFilter
import sg.mesha.goatos.feature.weighing.plan.WeighingRepeatBucket
import sg.mesha.goatos.feature.weighing.plan.WeighingRepeatSeed
import sg.mesha.goatos.feature.weighing.plan.WeighingRepeatSeedStore
import sg.mesha.goatos.feature.weighing.plan.WeighingWizardBucketRow
import sg.mesha.goatos.feature.weighing.plan.WeighingWizardConfigRow
import sg.mesha.goatos.feature.weighing.plan.WeighingWizardDateOption
import sg.mesha.goatos.feature.weighing.plan.WeighingWizardOperatorLoad
import sg.mesha.goatos.feature.weighing.plan.WeighingWizardOperatorOption
import sg.mesha.goatos.feature.weighing.plan.WeighingWizardParkOption
import sg.mesha.goatos.feature.weighing.plan.WeighingWizardReviewRow
import sg.mesha.goatos.feature.weighing.plan.WeighingWizardStep
import sg.mesha.goatos.feature.weighing.plan.WeighingWizardUiState
import sg.mesha.goatos.ui.Routes
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale
import javax.inject.Inject

/**
 * Authoring one weighing task: date -> park -> shed buckets -> configure -> review & publish.
 *
 * Its OWN ViewModel rather than more state on the task list's: authoring is a write flow with a
 * draft the user is still editing, and a list is a read. Nothing here writes until the review step,
 * and the write itself creates a DRAFT and then runs the real publish call — a published task is
 * never fabricated by asserting a status.
 *
 * Availability ("this shed is already scheduled that day") is answered by the server for the
 * chosen date, never inferred from whatever tasks this device happens to have loaded.
 */
@HiltViewModel
class WeighingPlanWizardViewModel @Inject constructor(
    private val repository: WeighingRepository,
    repeatSeedStore: WeighingRepeatSeedStore,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    /**
     * The route argument naming the task this wizard was started FROM -- a repeat OR an edit; the
     * two are told apart only by the SEED's own [WeighingRepeatSeed.editCampaignId], which arrives
     * separately, in-process, through [repeatSeedStore]. The route arg alone cannot say which.
     */
    private val repeatOfArg: String? =
        savedStateHandle.get<String>(Routes.WEIGHING_REPEAT_OF_ARG)?.takeIf { it.isNotBlank() }

    private val repeatSeed: WeighingRepeatSeed? = repeatOfArg?.let { repeatSeedStore.take(it) }

    /**
     * Set when this wizard instance is EDITING an existing task rather than authoring a new one.
     *
     * Carried on [WizardRaw] (not just read once here) so every place that reads or writes the
     * campaign -- the bucket-availability calls that must exclude it, and [commit] -- reaches it
     * off the same state the screen renders.
     */
    private val editCampaignId: String? = repeatSeed?.editCampaignId

    /**
     * True when the route says this wizard was opened FROM another task (repeat or edit) but the
     * in-process handoff that would say which is gone.
     *
     * [WeighingRepeatSeedStore] is a one-shot, in-memory map by design (see its own doc) -- it does
     * NOT survive process death, while the route argument does, via [SavedStateHandle]. Without this
     * flag, a process death between staging an edit and this ViewModel's construction left
     * [editCampaignId] null and the wizard silently reopened as a brand-new, empty CREATE wizard --
     * the planner's edit intent (and the fact they were changing an EXISTING task) vanished with no
     * sign anything had gone wrong. This is not recoverable from disk, so instead of guessing, the
     * wizard opens refusing to continue at all and says why, so the planner goes back and reopens
     * the task instead of unknowingly authoring a second one.
     */
    private val seedLost: Boolean = repeatOfArg != null && repeatSeed == null

    private val raw = MutableStateFlow(
        WizardRaw(
            repeat = repeatSeed,
            editCampaignId = editCampaignId,
            seedLost = seedLost,
            // An edit cannot change which day OR which park the task runs on -- only its shed
            // buckets, their operators and their mode -- so both the date and the park are
            // pre-selected, never chosen, and the catalog for it starts loading immediately
            // rather than waiting for taps this flow never asks for.
            date = repeatSeed?.editWeighDate?.takeIf { editCampaignId != null },
            // The MAINTAINER DECISION: an edit must never be able to change the task's date or
            // park, so neither step is just pre-filled -- both are UNREACHABLE. An edit opens
            // straight on BUCKETS -- the first step that can still change -- and [back] refuses to
            // walk past it into PARK or DATE. To move a task to another day, or another park, the
            // planner closes it and starts a new one.
            step = if (editCampaignId != null) WeighingWizardStep.BUCKETS else WeighingWizardStep.DATE,
            message = if (seedLost) SEED_LOST_MESSAGE else null,
        ),
    )

    private var observeCatalogJob: Job? = null
    private var observeBucketsJob: Job? = null
    private var bucketSearchJob: Job? = null

    /** The chosen park's cached cursor state. Stops the scroll prefetch at the end of that park. */
    private var bucketsEndReached = false

    /**
     * IN-FLIGHT guards for the two network calls that both surface through [WizardRaw.loading].
     *
     * Kept SEPARATE from that shared UI flag on purpose. The edit wizard pre-hydrates its date and
     * fires the catalog refresh and the first park-bucket refresh back to back, before either
     * network call has returned -- so a single shared "is anything loading" boolean made the
     * bucket refresh see the catalog refresh's in-flight flag and silently bail out via its own
     * dedupe guard, permanently leaving the bucket step empty (nothing re-triggers it afterward).
     * Each call now dedupes against its OWN flag; [WizardRaw.loading] keeps driving the UI text
     * for whichever fetch is actually running.
     */
    private var catalogRefreshInFlight = false
    private var bucketsRefreshInFlight = false

    val state: StateFlow<WeighingWizardUiState> = raw
        .map { it.toUiState() }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), WizardRaw().toUiState())

    /**
     * Step this wizard instance has already emitted a [AnalyticsEventsWeighing.WEIGHING_PLAN_WIZARD_STEP_REACHED]
     * for, so a re-render or a re-pick of the SAME step (e.g. re-selecting the already-chosen park
     * in [selectPark]) never double-logs. Also doubles as the LAST step reached, which
     * [onCleared] reports if the wizard is abandoned without saving.
     */
    private var lastTrackedStep: WeighingWizardStep? = null

    /** The step this instance opened on — DATE for a create, BUCKETS for an edit (see [raw]'s
     *  init). An abandonment that never leaves this step is nothing started, not something given
     *  up on, so [onCleared] only reports past it. */
    private val openingStep: WeighingWizardStep = raw.value.step

    init {
        analytics.track(AnalyticsEvents.WEIGHING_PLAN_VIEWED)
        trackStepReached(raw.value.step)
        // Editing pre-selects its date (see [raw]'s init above), so the catalog for it starts
        // loading now rather than waiting for a DATE-step tap this flow never asks for.
        raw.value.date?.takeIf { editCampaignId != null }?.let { loadCatalog(it) }
    }

    /** Emits the step-reached event ONCE per distinct step this wizard instance visits. */
    private fun trackStepReached(step: WeighingWizardStep) {
        if (lastTrackedStep == step) return
        lastTrackedStep = step
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_PLAN_WIZARD_STEP_REACHED,
            mapOf(
                AnalyticsEventsWeighing.Params.WIZARD_STEP to step.name.lowercase(),
                AnalyticsEvents.Params.CATEGORY to if (raw.value.editCampaignId != null) "edit" else "create",
            ),
        )
    }

    private fun wizardMode(raw: WizardRaw = this.raw.value): String =
        if (raw.editCampaignId != null) "edit" else "create"

    private fun baseWizardProps(raw: WizardRaw = this.raw.value): Map<String, String> =
        mapOf(
            AnalyticsEvents.Params.CATEGORY to wizardMode(raw),
            AnalyticsEventsWeighing.Params.WIZARD_STEP to raw.step.name.lowercase(),
            AnalyticsEventsWeighing.Params.PAGE_SIZE to WEIGHING_PAGE_SIZE.toString(),
            AnalyticsEventsWeighing.Params.SELECTION_COUNT to raw.selections.size.toString(),
        )

    private fun searchProps(query: String): Map<String, String> =
        mapOf(
            AnalyticsEventsWeighing.Params.QUERY_STATE to if (query.isBlank()) "blank" else "set",
            AnalyticsEventsWeighing.Params.QUERY to query.trim().take(40),
        )

    private fun trackBucketSearchResults(raw: WizardRaw = this.raw.value) {
        if (raw.bucketQuery.isBlank()) return
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_PLAN_BUCKET_SEARCH_RESULTS,
            baseWizardProps(raw) + searchProps(raw.bucketQuery) + mapOf(
                AnalyticsEventsWeighing.Params.RESULT_COUNT to raw.filteredBuckets().size.toString(),
                AnalyticsEvents.Params.ROW_COUNT to raw.buckets.size.toString(),
            ),
        )
    }

    /**
     * The wizard's ViewModel is gone — the planner left mid-flow (back, app switch, process
     * death). Real, unsaved progress (past the OPENING step, and never saved) is reported so a
     * funnel can tell where planners actually give up; a wizard nobody touched, or one that
     * already saved, reports nothing.
     */
    override fun onCleared() {
        val step = lastTrackedStep ?: return
        if (raw.value.savedCampaignId != null) return
        if (step == openingStep) return
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_PLAN_WIZARD_ABANDONED,
            mapOf(
                AnalyticsEventsWeighing.Params.WIZARD_STEP to step.name.lowercase(),
                AnalyticsEvents.Params.CATEGORY to if (raw.value.editCampaignId != null) "edit" else "create",
            ),
        )
    }

    // ---- step movement -------------------------------------------------------------------

    /** Advances one step. Blocked steps cannot advance, so a later step never sees a hole. */
    fun next() {
        val current = raw.value
        if (!current.canContinue()) return
        val nextStep = current.step.next() ?: return
        raw.value = when (nextStep) {
            // Entering the bucket step is the moment the date and park are both known, so it is
            // the moment availability can be asked for.
            WeighingWizardStep.BUCKETS -> current.copy(step = nextStep, bucketCap = WEIGHING_PAGE_SIZE, bucketQuery = "")
            WeighingWizardStep.CONFIGURE -> current.copy(step = nextStep, configCap = WEIGHING_PAGE_SIZE, configQuery = "")
            else -> current.copy(step = nextStep)
        }
        trackStepReached(nextStep)
    }

    /**
     * Steps backwards inside the wizard. Returns false only on the first step, where the caller
     * leaves the screen — so Back never drops a half-built task by accident mid-flow.
     *
     * An edit's first step is BUCKETS, not DATE (see [raw]): both DATE and PARK are locked, so
     * Back from BUCKETS in edit mode must leave the screen exactly like Back from DATE does in
     * create mode -- never quietly land on a step editing can never use.
     */
    fun back(): Boolean {
        val current = raw.value
        if (current.editCampaignId != null && current.step == WeighingWizardStep.BUCKETS) return false
        val previous = current.step.previous() ?: return false
        if (current.editCampaignId != null &&
            (previous == WeighingWizardStep.DATE || previous == WeighingWizardStep.PARK)
        ) {
            return false
        }
        raw.value = current.copy(step = previous)
        trackStepReached(previous)
        return true
    }

    fun dismissMessage() {
        raw.value = raw.value.copy(message = null)
    }

    // ---- step 1: date --------------------------------------------------------------------

    /**
     * Picks the weigh DATE. A weigh date is a business day in the farm's own zone, never a clock
     * offset, and the past is not offerable: work cannot be planned into a day already spent.
     */
    fun selectDate(isoDate: String) {
        val current = raw.value
        // MAINTAINER DECISION: an edit locks its date. This is a refusal, not a validation --
        // the DATE step is unreachable in edit mode (see [raw]), so this only guards a caller
        // that reaches straight into the ViewModel bypassing the screen.
        if (current.editCampaignId != null) return
        val today = LocalDate.now(ZoneId.of(WEIGHING_WIZARD_ZONE))
        val parsed = runCatching {
            // exception:exempt date validation; unparseable date rejects state change
            LocalDate.parse(isoDate, ISO_DATE)
        }.getOrNull() ?: return
        if (parsed.isBefore(today)) return
        if (current.date == isoDate) return
        // Changing the date invalidates every downstream answer: availability, and with it the
        // bucket set, is date-scoped.
        raw.value = current.copy(
            date = isoDate,
            catalog = null,
            parkId = null,
            buckets = emptyList(),
            bucketsParkId = null,
            selections = emptyMap(),
            picked = emptySet(),
            repeatDropped = 0,
        )
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_PLAN_DATE_SELECTED,
            baseWizardProps(raw.value) + mapOf(AnalyticsEvents.Params.ACTION to "set"),
        )
        loadCatalog(isoDate)
    }

    // ---- step 2: park --------------------------------------------------------------------

    /**
     * Picks the park whose buckets the next step pages.
     *
     * A different park is a different keyset stream, so the shed paging is reset cleanly: the
     * window shrinks back to one page, the cursor state is cleared, and page one of THAT park is
     * fetched. Selections are dropped with the park because a task is ONE park — a selection made
     * in another park could not be published on this one.
     */
    fun selectPark(parkId: String) {
        val current = raw.value
        // MAINTAINER DECISION: a campaign belongs to ONE park; an edit locks it just like it
        // locks the date. The PARK step is unreachable in edit mode (see [raw]), so this only
        // guards a caller that reaches straight into the ViewModel bypassing the screen.
        if (current.editCampaignId != null) return
        val date = current.date ?: return
        // Re-picking the SAME park is not a no-op. Availability is a live, date-scoped fact
        // owned by other people's tasks: a shed can be taken, or finish and become free
        // again, between two visits to this step. Returning early here meant a retained
        // wizard kept showing the availability it saw the first time -- observed on device
        // as "Available 0" for sheds whose weighing had just completed, until the app was
        // killed. Re-entering the park re-reads it; the planner's own answers are kept.
        if (current.parkId == parkId) {
            // Re-read availability IN PLACE. loadParkBuckets would reset the keyset window
            // to page 1 and the repository's reset path deletes the cached pages, which
            // silently drops every bucket the planner already picked from a later page --
            // out of the tray, out of configure/review, and out of the published task.
            // This refreshes the pages already on screen and adds none.
            analytics.track(
                AnalyticsEventsWeighing.WEIGHING_PLAN_PARK_SELECTED,
                baseWizardProps(current) + mapOf(AnalyticsEvents.Params.ACTION to "refresh"),
            )
            refreshBucketAvailability(date, parkId)
            return
        }
        raw.value = current.copy(
            parkId = parkId,
            buckets = emptyList(),
            bucketsParkId = null,
            selections = emptyMap(),
            picked = emptySet(),
            bucketQuery = "",
            bucketCap = WEIGHING_PAGE_SIZE,
        )
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_PLAN_PARK_SELECTED,
            baseWizardProps(raw.value) + mapOf(AnalyticsEvents.Params.ACTION to "set"),
        )
        loadParkBuckets(date, parkId)
    }

    // ---- step 3: shed buckets ------------------------------------------------------------

    fun setBucketQuery(query: String) {
        bucketSearchJob?.cancel()
        val next = raw.value.copy(bucketQuery = query, bucketCap = WEIGHING_PAGE_SIZE)
        raw.value = next
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_PLAN_BUCKET_SEARCH_CHANGED,
            baseWizardProps(next) + searchProps(query) + mapOf(
                AnalyticsEvents.Params.ACTION to if (query.isBlank()) "cleared" else "set",
                AnalyticsEvents.Params.ROW_COUNT to next.filteredBuckets().size.toString(),
                AnalyticsEventsWeighing.Params.RESULT_COUNT to next.filteredBuckets().size.toString(),
            ),
        )
        val date = next.date
        val parkId = next.parkId
        if (date == null || parkId == null) return
        val search = query.trim().takeIf { it.isNotBlank() }
        if (search == null) {
            loadParkBuckets(date, parkId, search = null)
            return
        }
        bucketSearchJob = viewModelScope.launch {
            delay(BUCKET_SEARCH_DEBOUNCE_MS)
            val latest = raw.value
            if (latest.date == date && latest.parkId == parkId && latest.bucketQuery == query) {
                loadParkBuckets(date, parkId, search = search)
            }
        }
    }

    fun setBucketFilter(filter: WeighingBucketFilter) {
        raw.value = raw.value.copy(bucketFilter = filter, bucketCap = WEIGHING_PAGE_SIZE)
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_PLAN_BUCKET_FILTER_CHANGED,
            baseWizardProps(raw.value) + mapOf(
                AnalyticsEvents.Params.ACTION to filter.name.lowercase(),
                AnalyticsEventsWeighing.Params.RESULT_COUNT to raw.value.filteredBuckets().size.toString(),
            ),
        )
    }

    /**
     * One more page of buckets, on scroll-end.
     *
     * Grows the rendered page AND, when the rendered rows have caught up with the cache, the
     * observed Room window plus the next server page. The list never loads the whole park at once:
     * a park can hold 76+ sheds and the catalog is keyset-paged for exactly that reason.
     */
    fun loadMoreBuckets() {
        val current = raw.value
        val date = current.date
        val parkId = current.parkId
        if (current.bucketCap < current.filteredBuckets().size) {
            raw.value = current.copy(bucketCap = current.bucketCap + WEIGHING_PAGE_SIZE)
            analytics.track(
                AnalyticsEventsWeighing.WEIGHING_PLAN_BUCKET_PAGE_COMPLETED,
                baseWizardProps(raw.value) + mapOf(
                    AnalyticsEvents.Params.OUTCOME to "local_window",
                    AnalyticsEventsWeighing.Params.RESULT_COUNT to raw.value.filteredBuckets().size.toString(),
                    AnalyticsEvents.Params.ROW_COUNT to raw.value.buckets.size.toString(),
                ),
            )
            return
        }
        if (date == null || parkId == null || bucketsEndReached) return
        if (bucketsRefreshInFlight) return
        bucketsRefreshInFlight = true
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_PLAN_BUCKET_PAGE_ATTEMPTED,
            baseWizardProps(current) + mapOf(
                AnalyticsEvents.Params.ROW_COUNT to current.buckets.size.toString(),
                AnalyticsEventsWeighing.Params.RESULT_COUNT to current.filteredBuckets().size.toString(),
                AnalyticsEvents.Params.KIND to if (current.bucketQuery.isBlank()) "normal_page" else "search_page",
            ) + searchProps(current.bucketQuery),
        )
        raw.value = current.copy(bucketCap = current.bucketCap + WEIGHING_PAGE_SIZE, loading = true)
        viewModelScope.launch {
            try {
                when (
                    val result = repository.appendPlannerParkBuckets(
                        date,
                        parkId,
                        current.editCampaignId,
                        search = current.bucketQuery.trim().takeIf { it.isNotBlank() },
                    )
                ) {
                    is AppResult.Ok -> {
                        raw.value = raw.value.copy(message = null)
                        analytics.track(
                            AnalyticsEventsWeighing.WEIGHING_PLAN_BUCKET_PAGE_COMPLETED,
                            baseWizardProps(raw.value) + mapOf(
                                AnalyticsEvents.Params.OUTCOME to "success",
                                AnalyticsEvents.Params.ROW_COUNT to raw.value.buckets.size.toString(),
                                AnalyticsEventsWeighing.Params.RESULT_COUNT to raw.value.filteredBuckets().size.toString(),
                                AnalyticsEvents.Params.KIND to if (raw.value.bucketQuery.isBlank()) "normal_page" else "search_page",
                            ) + searchProps(raw.value.bucketQuery),
                        )
                        trackBucketSearchResults()
                    }
                    is AppResult.Err -> {
                        raw.value = raw.value.copy(message = result.message)
                        analytics.track(
                            AnalyticsEventsWeighing.WEIGHING_PLAN_BUCKET_PAGE_COMPLETED,
                            baseWizardProps(raw.value) + mapOf(
                                AnalyticsEvents.Params.OUTCOME to "failure",
                                AnalyticsEvents.Params.REASON to result.message.take(80),
                                AnalyticsEvents.Params.ROW_COUNT to raw.value.buckets.size.toString(),
                                AnalyticsEventsWeighing.Params.RESULT_COUNT to raw.value.filteredBuckets().size.toString(),
                                AnalyticsEvents.Params.KIND to if (raw.value.bucketQuery.isBlank()) "normal_page" else "search_page",
                            ) + searchProps(raw.value.bucketQuery),
                        )
                    }
                }
            } finally {
                bucketsRefreshInFlight = false
                raw.value = raw.value.copy(loading = false)
            }
        }
    }

    /**
     * Adds or removes a bucket. A bucket already scheduled that day is refused HERE, with its
     * reason, rather than at publish — the block is visible before any configuration is spent.
     */
    fun toggleBucket(locationId: String) {
        val current = raw.value
        val shed = current.shedsInPark().firstOrNull { it.operationalKey() == locationId } ?: return
        if (shed.scheduled) return
        val selections = current.selections.toMutableMap()
        if (selections.remove(locationId) == null) {
            selections[locationId] = current.seededSelection(shed)
        }
        raw.value = current.copy(selections = selections, picked = current.picked - locationId)
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_PLAN_BUCKET_TOGGLED,
            baseWizardProps(raw.value) + mapOf(
                AnalyticsEvents.Params.ACTION to if (locationId in selections) "selected" else "removed",
            ),
        )
    }

    fun addAllVisibleBuckets() {
        val current = raw.value
        val selections = current.selections.toMutableMap()
        current.filteredBuckets()
            .filterNot { it.scheduled || selections.containsKey(it.operationalKey()) }
            .forEach { shed ->
                selections[shed.operationalKey()] = current.seededSelection(shed)
            }
        raw.value = current.copy(selections = selections)
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_PLAN_BUCKET_BULK_ACTION,
            baseWizardProps(raw.value) + mapOf(
                AnalyticsEvents.Params.ACTION to "add_all_visible",
                AnalyticsEventsWeighing.Params.RESULT_COUNT to current.filteredBuckets().size.toString(),
            ),
        )
    }

    fun clearAllBuckets() {
        raw.value = raw.value.copy(selections = emptyMap(), picked = emptySet())
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_PLAN_BUCKET_BULK_ACTION,
            baseWizardProps(raw.value) + mapOf(AnalyticsEvents.Params.ACTION to "clear_all"),
        )
    }

    // ---- step 4: configure ---------------------------------------------------------------

    fun setConfigQuery(query: String) {
        raw.value = raw.value.copy(configQuery = query, configCap = WEIGHING_PAGE_SIZE)
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_PLAN_CONFIG_SEARCH_CHANGED,
            baseWizardProps(raw.value) + mapOf(
                AnalyticsEvents.Params.ACTION to if (query.isBlank()) "cleared" else "set",
                AnalyticsEventsWeighing.Params.QUERY_STATE to if (query.isBlank()) "blank" else "set",
                AnalyticsEventsWeighing.Params.RESULT_COUNT to raw.value.filteredSelections().size.toString(),
            ),
        )
    }

    fun toggleConfigSearch() {
        val current = raw.value
        raw.value = current.copy(
            configSearchOpen = !current.configSearchOpen,
            configQuery = if (current.configSearchOpen) "" else current.configQuery,
        )
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_PLAN_CONFIG_SEARCH_CHANGED,
            baseWizardProps(raw.value) + mapOf(
                AnalyticsEvents.Params.ACTION to if (raw.value.configSearchOpen) "opened" else "closed",
                AnalyticsEventsWeighing.Params.QUERY_STATE to if (raw.value.configQuery.isBlank()) "blank" else "set",
                AnalyticsEventsWeighing.Params.RESULT_COUNT to raw.value.filteredSelections().size.toString(),
            ),
        )
    }

    fun loadMoreConfigRows() {
        val current = raw.value
        if (current.configCap >= current.filteredSelections().size) return
        raw.value = current.copy(configCap = current.configCap + WEIGHING_PAGE_SIZE)
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_PLAN_CONFIG_PAGE_CHANGED,
            baseWizardProps(raw.value) + mapOf(
                AnalyticsEvents.Params.ACTION to "load_more",
                AnalyticsEventsWeighing.Params.RESULT_COUNT to raw.value.filteredSelections().size.toString(),
            ),
        )
    }

    fun setBucketCategory(locationId: String, category: String) {
        if (category != INDIVIDUAL_ANIMAL_CATEGORY &&
            category != PER_SHED_PARTITION_CATEGORY
        ) {
            return
        }
        val current = raw.value
        val selection = current.selections[locationId] ?: return
        raw.value = current.copy(
            selections = current.selections + (locationId to selection.copy(category = category)),
        )
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_PLAN_BUCKET_CATEGORY_SET,
            baseWizardProps(raw.value) + mapOf(AnalyticsEvents.Params.ACTION to category),
        )
    }

    /** ONE bucket, exactly ONE operator. There is no "shared" bucket and no unassigned bucket. */
    fun setBucketOperator(locationId: String, operatorUserId: String) {
        val current = raw.value
        val selection = current.selections[locationId] ?: return
        // Refuse a pick that is not valid for the chosen park, even if the id is real.
        if (current.operatorsForPark().none { it.userId == operatorUserId }) return
        raw.value = current.copy(
            selections = current.selections + (locationId to selection.copy(operatorUserId = operatorUserId)),
        )
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_PLAN_BUCKET_OPERATOR_SET,
            baseWizardProps(raw.value) + mapOf(AnalyticsEvents.Params.ACTION to "set"),
        )
    }

    fun toggleConfigPick(locationId: String) {
        val current = raw.value
        raw.value = current.copy(
            picked = if (locationId in current.picked) current.picked - locationId else current.picked + locationId,
        )
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_PLAN_CONFIG_PICK_TOGGLED,
            baseWizardProps(raw.value) + mapOf(
                AnalyticsEvents.Params.ACTION to if (locationId in raw.value.picked) "picked" else "unpicked",
            ),
        )
    }

    fun pickAllShownConfigRows() {
        val current = raw.value
        raw.value = current.copy(picked = current.filteredSelections().map { it.first }.toSet())
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_PLAN_CONFIG_BULK_ACTION,
            baseWizardProps(raw.value) + mapOf(
                AnalyticsEvents.Params.ACTION to "pick_all_shown",
                AnalyticsEventsWeighing.Params.RESULT_COUNT to current.filteredSelections().size.toString(),
            ),
        )
    }

    fun clearConfigPicks() {
        raw.value = raw.value.copy(picked = emptySet())
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_PLAN_CONFIG_BULK_ACTION,
            baseWizardProps(raw.value) + mapOf(AnalyticsEvents.Params.ACTION to "clear_picks"),
        )
    }

    /** Applies a mode and/or an operator to the ticked buckets, or to everything shown if none. */
    fun applyBulk(category: String?, operatorUserId: String?) {
        val current = raw.value
        if (category == null && operatorUserId == null) return
        val targets = current.picked.ifEmpty { current.filteredSelections().map { it.first }.toSet() }
        val selections = current.selections.toMutableMap()
        targets.forEach { locationId ->
            val selection = selections[locationId] ?: return@forEach
            selections[locationId] = selection.copy(
                category = category ?: selection.category,
                operatorUserId = operatorUserId ?: selection.operatorUserId,
            )
        }
        raw.value = current.copy(selections = selections)
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_PLAN_CONFIG_BULK_ACTION,
            baseWizardProps(raw.value) + mapOf(
                AnalyticsEvents.Params.ACTION to "apply_bulk",
                AnalyticsEventsWeighing.Params.TARGET to if (current.picked.isEmpty()) "visible" else "picked",
                AnalyticsEvents.Params.KIND to (category ?: "unchanged"),
            ),
        )
    }

    /** Deals the buckets round-robin across the park's operators, in a stable order. */
    fun splitEvenly() {
        val current = raw.value
        val operators = current.operatorsForPark()
        if (operators.isEmpty()) return
        val ordered = current.orderedSelections().map { it.first }
        val selections = current.selections.toMutableMap()
        ordered.forEachIndexed { index, locationId ->
            val selection = selections[locationId] ?: return@forEachIndexed
            selections[locationId] = selection.copy(operatorUserId = operators[index % operators.size].userId)
        }
        raw.value = current.copy(selections = selections)
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_PLAN_CONFIG_BULK_ACTION,
            baseWizardProps(raw.value) + mapOf(AnalyticsEvents.Params.ACTION to "split_evenly"),
        )
    }

    // ---- step 5: commit ------------------------------------------------------------------

    /**
     * Saves the task. [publish] runs the real publish call after the create; without it the task
     * stays a draft, which is a state the planner can come back to.
     */
    fun commit(publish: Boolean) {
        val current = raw.value
        if (current.busy || current.savedCampaignId != null) return
        // A lost seed (see [seedLost]) must never fall through into an ordinary create -- that is
        // exactly the silent-downgrade bug this guards against.
        if (current.seedLost) return
        val date = current.date ?: return
        val park = current.park() ?: return
        val rows = current.orderedSelections()
        if (rows.isEmpty()) {
            raw.value = current.copy(message = "Add at least one shed bucket before saving this task.")
            return
        }
        if (rows.any { it.second.operatorUserId.isBlank() }) {
            raw.value = current.copy(message = "Every shed bucket needs one operator before this task can be saved.")
            return
        }
        raw.value = current.copy(busy = true, message = null)
        analytics.track(
            AnalyticsEvents.WEIGHING_PLAN_SAVE_ATTEMPTED,
            baseWizardProps(current) + mapOf(
                AnalyticsEvents.Params.ACTION to if (publish) "publish" else "draft",
                AnalyticsEvents.Params.ROW_COUNT to rows.size.toString(),
            ),
        )
        viewModelScope.launch {
            // A weighing task is ONE park on ONE weigh date, so the period start, period end and
            // weigh date are the same business day. They are not a range.
            val draft = WeighingPlanDraft(
                parkId = park.parkId,
                periodStartDate = date,
                periodEndDate = date,
                startBusinessDate = date,
                plannedCapPerDay = DEFAULT_PLANNED_CAP_PER_DAY,
                operatorUserId = rows.first().second.operatorUserId,
                sheds = rows.map { (bucketKey, selection) ->
                    val shed = current.shedsInPark().first { it.operationalKey() == bucketKey }
                    WeighingPlannerShed(
                        locationId = shed.locationId,
                        name = shed.name,
                        partitionLabel = shed.partitionLabel,
                        kidCount = shed.kidCount,
                        category = selection.category,
                        operatorUserId = selection.operatorUserId,
                    )
                },
            )
            val editCampaignId = current.editCampaignId
            if (editCampaignId != null) {
                // Editing writes to the SAME campaign through the existing update call, which
                // republishes on its own when the task is still a draft -- the create-then-publish
                // path never runs, so an edit can never fabricate a second task.
                when (val result = repository.updatePlan(editCampaignId, draft)) {
                    is AppResult.Ok -> {
                        raw.value = raw.value.copy(busy = false, savedCampaignId = editCampaignId)
                        analytics.track(
                            AnalyticsEvents.WEIGHING_PLAN_SAVE_SUCCEEDED,
                            baseWizardProps(raw.value) + mapOf(
                                AnalyticsEvents.Params.ACTION to "edit",
                                AnalyticsEvents.Params.ROW_COUNT to rows.size.toString(),
                            ),
                        )
                    }
                    is AppResult.Err -> {
                        raw.value = raw.value.copy(busy = false, message = result.message)
                        analytics.track(
                            AnalyticsEvents.WEIGHING_PLAN_SAVE_FAILED,
                            baseWizardProps(raw.value) + mapOf(
                                AnalyticsEvents.Params.ACTION to "edit",
                                AnalyticsEvents.Params.REASON to result.message.take(80),
                                AnalyticsEvents.Params.ROW_COUNT to rows.size.toString(),
                            ),
                        )
                        crashReporter.recordException(
                            result.cause ?: IllegalStateException(result.message),
                            "weighing plan edit save failed"
                        )
                    }
                }
                return@launch
            }
            when (val result = repository.createPlan(draft, publish)) {
                is AppResult.Ok -> {
                    raw.value = raw.value.copy(busy = false, savedCampaignId = result.value)
                    analytics.track(
                        AnalyticsEvents.WEIGHING_PLAN_SAVE_SUCCEEDED,
                        baseWizardProps(raw.value) + mapOf(
                            AnalyticsEvents.Params.ACTION to if (publish) "publish" else "draft",
                            AnalyticsEvents.Params.ROW_COUNT to rows.size.toString(),
                        ),
                    )
                }
                is AppResult.Err -> {
                    raw.value = raw.value.copy(busy = false, message = result.message)
                    analytics.track(
                        AnalyticsEvents.WEIGHING_PLAN_SAVE_FAILED,
                        baseWizardProps(raw.value) + mapOf(
                            AnalyticsEvents.Params.ACTION to if (publish) "publish" else "draft",
                            AnalyticsEvents.Params.REASON to result.message.take(80),
                            AnalyticsEvents.Params.ROW_COUNT to rows.size.toString(),
                        ),
                    )
                    crashReporter.recordException(
                        result.cause ?: IllegalStateException(result.message),
                        "weighing plan save failed"
                    )
                }
            }
        }
    }

    // ---- loading -------------------------------------------------------------------------

    /**
     * Points the wizard at ONE weigh date's catalog.
     *
     * The catalog the screen renders comes from ROOM: the collector below observes a bounded window
     * of cached shed rows and feeds the wizard's answers, while the network refresh only writes
     * into Room. A park can hold 76+ sheds, so this is a real keyset page, not a whole-park read --
     * and a failed refresh leaves the cached catalog on screen instead of emptying the picker.
     */
    private fun loadCatalog(isoDate: String) {
        observeCatalogJob?.cancel()
        observeBucketsJob?.cancel()
        observeCatalogJob = viewModelScope.launch {
            repository.observePlannerCatalog(isoDate).collect { cached ->
                // Only replace the answers once the cache actually holds this date, so an empty
                // first emission does not blank a park list the planner is already working in.
                if (!cached.hasCache && cached.catalog.parks.isEmpty()) return@collect
                val seeded = raw.value.copy(catalog = cached.catalog).withRepeatParkApplied()
                raw.value = seeded
                // A repeated task names its park, so its buckets can start loading before the
                // planner reaches the bucket step.
                val seededPark = seeded.parkId
                if (seededPark != null && seeded.bucketsParkId == null && observeBucketsJob == null) {
                    loadParkBuckets(isoDate, seededPark)
                }
            }
        }
        refreshCatalog(isoDate)
    }

    private fun refreshCatalog(isoDate: String) {
        // Deduped against its OWN in-flight flag, not [WizardRaw.loading] -- see
        // [catalogRefreshInFlight]'s doc for why the two must never share one guard.
        if (catalogRefreshInFlight) return
        catalogRefreshInFlight = true
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_PLAN_DATA_LOAD_ATTEMPTED,
            baseWizardProps() + mapOf(
                AnalyticsEvents.Params.ACTION to "catalog_refresh",
                AnalyticsEvents.Params.KIND to "catalog",
            ),
        )
        raw.value = raw.value.copy(loading = true)
        viewModelScope.launch {
            try {
                when (val result = repository.refreshPlannerCatalog(isoDate)) {
                    is AppResult.Ok -> {
                        raw.value = raw.value.copy(loading = false)
                        analytics.track(
                            AnalyticsEventsWeighing.WEIGHING_PLAN_DATA_LOAD_COMPLETED,
                            baseWizardProps() + mapOf(
                                AnalyticsEvents.Params.ACTION to "catalog_refresh",
                                AnalyticsEvents.Params.KIND to "catalog",
                                AnalyticsEvents.Params.OUTCOME to "success",
                            ),
                        )
                    }
                    // The cached park list stays on screen; the wizard says what did not land rather
                    // than dropping the planner back to an empty picker.
                    is AppResult.Err -> {
                        raw.value = raw.value.copy(loading = false, message = result.message)
                        analytics.track(
                            AnalyticsEventsWeighing.WEIGHING_PLAN_DATA_LOAD_COMPLETED,
                            baseWizardProps() + mapOf(
                                AnalyticsEvents.Params.ACTION to "catalog_refresh",
                                AnalyticsEvents.Params.KIND to "catalog",
                                AnalyticsEvents.Params.OUTCOME to "failure",
                                AnalyticsEvents.Params.REASON to result.message.take(80),
                            ),
                        )
                        crashReporter.recordException(
                            result.cause ?: IllegalStateException(result.message),
                            "weighing planner catalog load failed"
                        )
                    }
                }
            } finally {
                catalogRefreshInFlight = false
            }
        }
    }

    /**
     * Points the bucket step at ONE park's sheds on the chosen date.
     *
     * The rows the screen renders come from ROOM: this collector observes a bounded window of that
     * park's cached shed rows, and the network refresh only writes into Room. A park can hold 76+
     * sheds, so this is a real keyset page -- and a failed refresh leaves the cached buckets on
     * screen instead of emptying the picker.
     */
    private fun loadParkBuckets(isoDate: String, parkId: String, search: String? = null) {
        bucketsEndReached = false
        observeBucketsJob?.cancel()
        val excludeCampaignId = raw.value.editCampaignId
        observeBucketsJob = viewModelScope.launch {
            repository.observePlannerParkBuckets(isoDate, parkId, Int.MAX_VALUE, excludeCampaignId, search)
                .collect { cached ->
                // A stale emission for a park the planner has already moved off must not repopulate
                // the list under the new park.
                if (raw.value.parkId != parkId) return@collect
                if (!cached.hasCache && cached.sheds.isEmpty()) return@collect
                bucketsEndReached = !cached.canLoadMore
                val updated = raw.value
                    .copy(buckets = cached.sheds, bucketsParkId = parkId)
                    .withRepeatBucketsApplied()
                raw.value = updated
                trackBucketSearchResults(updated)
            }
        }
        refreshParkBuckets(isoDate, parkId, reset = true, search = search)
    }

    /**
     * Re-reads availability for the pages the wizard is currently showing.
     *
     * Deliberately NOT gated on [WizardRaw.loading]: that flag is owned by paging, and a
     * skipped refresh here is exactly the stale-availability bug this exists to fix. It
     * writes no loading state of its own, so it cannot fight the pager.
     */
    private fun refreshBucketAvailability(isoDate: String, parkId: String) {
        val pages = ((raw.value.buckets.size + WEIGHING_LEADERSHIP_PAGE_SIZE - 1) / WEIGHING_LEADERSHIP_PAGE_SIZE)
            .coerceAtLeast(1)
        val excludeCampaignId = raw.value.editCampaignId
        viewModelScope.launch {
            when (
                val result = repository.refreshPlannerParkBucketAvailability(
                    isoDate,
                    parkId,
                    pages,
                    excludeCampaignId,
                )
            ) {
                is AppResult.Ok -> Unit
                is AppResult.Err -> raw.value = raw.value.copy(message = result.message)
            }
        }
    }

    private fun refreshParkBuckets(isoDate: String, parkId: String, reset: Boolean, search: String? = null) {
        // Deduped against its OWN in-flight flag, not [WizardRaw.loading]. The edit wizard fires
        // this right after the catalog refresh, before either network call has returned; sharing
        // one flag meant this call saw the catalog refresh's flag still up and bailed out for
        // good, since nothing else ever re-triggers the first park-bucket load.
        if (bucketsRefreshInFlight) return
        bucketsRefreshInFlight = true
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_PLAN_DATA_LOAD_ATTEMPTED,
            baseWizardProps() + mapOf(
                AnalyticsEvents.Params.ACTION to if (reset) "buckets_refresh_reset" else "buckets_refresh",
                AnalyticsEvents.Params.KIND to "buckets",
                AnalyticsEvents.Params.ROW_COUNT to raw.value.buckets.size.toString(),
            ) + searchProps(search.orEmpty()),
        )
        raw.value = raw.value.copy(loading = true)
        val excludeCampaignId = raw.value.editCampaignId
        viewModelScope.launch {
            try {
                when (
                    val result = repository.refreshPlannerParkBuckets(
                        isoDate,
                        parkId,
                        reset = reset,
                        excludeCampaignId = excludeCampaignId,
                        search = search,
                    )
                ) {
                    is AppResult.Ok -> {
                        raw.value = raw.value.copy(loading = false)
                        analytics.track(
                            AnalyticsEventsWeighing.WEIGHING_PLAN_DATA_LOAD_COMPLETED,
                            baseWizardProps() + mapOf(
                                AnalyticsEvents.Params.ACTION to if (reset) "buckets_refresh_reset" else "buckets_refresh",
                                AnalyticsEvents.Params.KIND to "buckets",
                                AnalyticsEvents.Params.OUTCOME to "success",
                                AnalyticsEvents.Params.ROW_COUNT to raw.value.buckets.size.toString(),
                                AnalyticsEventsWeighing.Params.RESULT_COUNT to raw.value.filteredBuckets().size.toString(),
                            ) + searchProps(search.orEmpty()),
                        )
                    }
                    is AppResult.Err -> {
                        raw.value = raw.value.copy(loading = false, message = result.message)
                        analytics.track(
                            AnalyticsEventsWeighing.WEIGHING_PLAN_DATA_LOAD_COMPLETED,
                            baseWizardProps() + mapOf(
                                AnalyticsEvents.Params.ACTION to if (reset) "buckets_refresh_reset" else "buckets_refresh",
                                AnalyticsEvents.Params.KIND to "buckets",
                                AnalyticsEvents.Params.OUTCOME to "failure",
                                AnalyticsEvents.Params.REASON to result.message.take(80),
                                AnalyticsEvents.Params.ROW_COUNT to raw.value.buckets.size.toString(),
                                AnalyticsEventsWeighing.Params.RESULT_COUNT to raw.value.filteredBuckets().size.toString(),
                            ) + searchProps(search.orEmpty()),
                        )
                        crashReporter.recordException(
                            result.cause ?: IllegalStateException(result.message),
                            "weighing planner park buckets load failed"
                        )
                    }
                }
            } finally {
                bucketsRefreshInFlight = false
            }
        }
    }
}

private const val WEIGHING_WIZARD_ZONE = "Asia/Kolkata"

// The two capture modes a shed bucket can be scheduled in, as the backend names them.
private const val INDIVIDUAL_ANIMAL_CATEGORY = "individual_animal"
private const val PER_SHED_PARTITION_CATEGORY = "per_shed_partition"
private const val DEFAULT_PLANNED_CAP_PER_DAY = 100
private const val WEIGHING_WIZARD_DATE_OPTIONS = 14
private const val WEIGHING_WIZARD_TRAY_CAP = 12
private const val BUCKET_SEARCH_DEBOUNCE_MS = 300L
private const val SEED_LOST_MESSAGE =
	"This task's details were lost when the app restarted. Go back and open it again."

private val ISO_DATE: DateTimeFormatter = DateTimeFormatter.ISO_LOCAL_DATE
private val WIZARD_DAY: DateTimeFormatter = DateTimeFormatter.ofPattern("EEE d MMM yyyy", Locale.ENGLISH)

private data class WizardSelection(
    val category: String,
    val operatorUserId: String,
)

/**
 * The wizard's raw answers. Everything the screen reads is DERIVED from this, so a filter or a
 * page cap can never disagree with the set of buckets actually being scheduled.
 */
private data class WizardRaw(
    val step: WeighingWizardStep = WeighingWizardStep.DATE,
    val date: String? = null,
    val parkId: String? = null,
    val catalog: WeighingPlannerCatalog? = null,
    /** The CHOSEN park's cached shed buckets, as far as the wizard has paged them. */
    val buckets: List<WeighingPlannerShed> = emptyList(),
    /** Which park [buckets] belongs to, so a stale page can never be read as another park's. */
    val bucketsParkId: String? = null,
    val selections: Map<String, WizardSelection> = emptyMap(),
    val picked: Set<String> = emptySet(),
    val bucketQuery: String = "",
    val bucketFilter: WeighingBucketFilter = WeighingBucketFilter.AVAILABLE,
    val bucketCap: Int = WEIGHING_PAGE_SIZE,
    val configQuery: String = "",
    val configSearchOpen: Boolean = false,
    val configCap: Int = WEIGHING_PAGE_SIZE,
    val loading: Boolean = false,
    val busy: Boolean = false,
    val message: String? = null,
    val savedCampaignId: String? = null,
    /** Answers carried over from an existing task, applied once the chosen date's catalog lands. */
    val repeat: WeighingRepeatSeed? = null,
    val repeatDropped: Int = 0,
    /** Carried-over buckets are applied ONCE, so a later page never overwrites the planner's edits. */
    val repeatApplied: Boolean = false,
    /**
     * Set when this wizard is EDITING that exact campaign rather than authoring a new one.
     *
     * Threaded through every bucket-availability read so this task's OWN sheds are excluded from
     * the server's "already scheduled" check -- otherwise every bucket this task already holds
     * would read back as taken against itself. [commit] branches on this to call the update write
     * instead of create.
     */
    val editCampaignId: String? = null,
    /**
     * True when the route named a task this wizard should have opened FROM, but the in-process
     * seed that would say what it was is gone (process death). See [WeighingPlanWizardViewModel.seedLost].
     * Blocks [canContinue] and [WeighingPlanWizardViewModel.commit] outright rather than letting
     * the wizard fall through to an ordinary, unrelated create.
     */
    val seedLost: Boolean = false,
)

/**
 * Names the park the repeated task ran in, as soon as the park list lands.
 *
 * Only the park: its buckets are a separate, paged read, so carrying them over waits for
 * [withRepeatBucketsApplied]. A park the planner may no longer use on this date drops the whole
 * carry-over and says how many buckets went with it.
 */
private fun WizardRaw.withRepeatParkApplied(): WizardRaw {
    val seed = repeat ?: return this
    if (repeatApplied || parkId != null) return this
    val catalog = catalog ?: return this
    val park = catalog.parks.firstOrNull { it.parkId == seed.parkId }
        ?: return copy(repeatApplied = true, repeatDropped = seed.buckets.size)
    return copy(parkId = park.parkId, selections = emptyMap(), picked = emptySet())
}

/**
 * Prefills the buckets from the repeated task, for the date the planner just chose.
 *
 * A carried-over bucket is added ONLY if this park's loaded buckets still have it and the SERVER
 * says it is free on that date; anything else is counted as dropped and reported. The taken ones
 * stay visible on the bucket step under the availability filter, with the server's own reason --
 * the duplicate block is never routed around.
 *
 * Applied ONCE, on the first page of the seeded park, so paging further never re-writes the
 * planner's own edits. A carried bucket sitting on a later page of a 76-shed park is reported as
 * dropped rather than silently added behind the planner's back.
 */
private fun WizardRaw.withRepeatBucketsApplied(): WizardRaw {
    val seed = repeat ?: return this
    if (repeatApplied) return this
    val catalog = catalog ?: return this
    if (parkId != seed.parkId || bucketsParkId != seed.parkId) return this
    if (buckets.isEmpty()) return this
    val shedsById = buckets.associateBy { it.operationalKey() }
    val carried = seed.buckets.mapNotNull { bucket ->
        val shed = shedsById[bucket.operationalKey()] ?: return@mapNotNull null
        if (shed.scheduled) return@mapNotNull null
        shed.operationalKey() to selectionFor(shed, bucket)
    }
    return copy(
        selections = carried.toMap(),
        picked = emptySet(),
        repeatApplied = true,
        repeatDropped = seed.buckets.size - carried.size,
    )
}

private fun WeighingWizardStep.next(): WeighingWizardStep? =
    WeighingWizardStep.entries.getOrNull(ordinal + 1)

private fun WeighingWizardStep.previous(): WeighingWizardStep? =
    WeighingWizardStep.entries.getOrNull(ordinal - 1)

private fun WizardRaw.park(): WeighingPlannerPark? =
    catalog?.parks?.firstOrNull { it.parkId == parkId }

/**
 * The CHOSEN park's loaded shed buckets. Empty until the park is picked and its first page lands --
 * the park read carries no shed rows at all.
 */
private fun WizardRaw.shedsInPark(): List<WeighingPlannerShed> =
    if (bucketsParkId != null && bucketsParkId == parkId) buckets else emptyList()

/**
 * The people who may be assigned work in the CHOSEN park.
 *
 * A person with no park list is tenant-scoped and may work anywhere; everyone else must name this
 * park. Without this the picker offered the whole roster, defaulted a CBE task to the CPT operator,
 * and the write accepted it -- so a park-scoped operator could see and weigh another park's shed.
 */
private fun WizardRaw.operatorsForPark(): List<WeighingPlannerOperator> {
    val park = parkId ?: return emptyList()
    return catalog?.operators.orEmpty()
        .filter { it.parkIds.isEmpty() || park in it.parkIds }
}

private fun WizardRaw.defaultOperatorId(): String =
    operatorsForPark().firstOrNull()?.userId.orEmpty()

/**
 * The mode/operator a bucket comes back with when it is (re-)added on the BUCKETS step.
 *
 * A bucket already on this campaign keeps the mode and operator it currently has, even after being
 * removed and re-added within the same edit session -- silently defaulting it back to lump-sum
 * would flip a live individual-mode assignment out from under its operator with no prompt. A
 * bucket neither the catalog nor the seed has ever heard of (a genuinely new addition, or an
 * ordinary create-mode wizard with no seed at all) falls back to the existing default: lump-sum,
 * with this park's default operator. See [selectionFor] for which of the two sources wins when a
 * shed IS found in the catalog.
 */
private fun WizardRaw.seededSelection(shed: WeighingPlannerShed): WizardSelection {
    val seeded = repeat?.buckets?.firstOrNull { it.operationalKey() == shed.operationalKey() }
    return selectionFor(shed, seeded)
}

/**
 * Resolves the mode/operator a bucket already on this campaign should carry, preferring the LIVE
 * catalog read over the wizard's own carried seed.
 *
 * [WeighingRepeatSeed.buckets] is captured ONCE, the moment this wizard was staged from the task
 * list's then-cached state -- a snapshot that goes stale the instant anything on the live campaign
 * changes afterward (another edit, a re-assignment from the operator app, or simply the planner's
 * own earlier answer on THIS wizard's Configure step, which the seed never learns about). The park
 * bucket catalog, by contrast, is re-read from the server on every refresh and carries this exact
 * bucket's CURRENT category/operator on [WeighingPlannerShed.scheduledCategory] /
 * [WeighingPlannerShed.scheduledOperatorUserId] even when [WeighingPlannerShed.scheduled] itself
 * reads false because this wizard's own campaign is excluded from the "already taken" check. The
 * seed is used only as a fallback -- e.g. before the catalog page carrying this shed has loaded --
 * so a bucket is never silently dropped to defaults while the network is still in flight.
 */
private fun WizardRaw.selectionFor(
    shed: WeighingPlannerShed?,
    seeded: WeighingRepeatBucket?,
): WizardSelection {
    val operatorIds = operatorsForPark().map { it.userId }.toSet()
    val liveCategory = shed?.scheduledCategory.orEmpty()
    val liveOperatorId = shed?.scheduledOperatorUserId.orEmpty()
    if (liveCategory.isNotBlank() || liveOperatorId.isNotBlank()) {
        return WizardSelection(
            category = when (liveCategory.trim().lowercase()) {
                INDIVIDUAL_ANIMAL_CATEGORY -> INDIVIDUAL_ANIMAL_CATEGORY
                else -> PER_SHED_PARTITION_CATEGORY
            },
            operatorUserId = liveOperatorId.takeIf { it in operatorIds }.orEmpty(),
        )
    }
    if (seeded != null) {
        return WizardSelection(
            category = when (seeded.category.trim().lowercase()) {
                INDIVIDUAL_ANIMAL_CATEGORY -> INDIVIDUAL_ANIMAL_CATEGORY
                else -> PER_SHED_PARTITION_CATEGORY
            },
            // An operator the catalog no longer offers for this date is not carried over, and no
            // stand-in is invented either: the catalog's operator list is tenant-wide, so "the
            // first one" could be someone who does not work this park. The bucket comes across
            // UNASSIGNED, which the configure step shows and the save gate refuses until the
            // planner picks somebody.
            operatorUserId = seeded.operatorUserId.takeIf { it in operatorIds }.orEmpty(),
        )
    }
    return WizardSelection(category = PER_SHED_PARTITION_CATEGORY, operatorUserId = defaultOperatorId())
}

private fun WizardRaw.canContinue(): Boolean {
    if (seedLost) return false
    return when (step) {
        WeighingWizardStep.DATE -> date != null
        WeighingWizardStep.PARK -> parkId != null
        WeighingWizardStep.BUCKETS -> selections.isNotEmpty()
        WeighingWizardStep.CONFIGURE -> selections.isNotEmpty()
        WeighingWizardStep.REVIEW -> false
    }
}

/** The park's buckets under the current search and filter, before paging. */
private fun WizardRaw.filteredBuckets(): List<WeighingPlannerShed> {
    val query = bucketQuery.trim().lowercase()
    return shedsInPark()
        .filter { query.isBlank() || it.name.lowercase().contains(query) }
        .filter { shed ->
            when (bucketFilter) {
                WeighingBucketFilter.AVAILABLE -> !shed.scheduled
                WeighingBucketFilter.TAKEN -> shed.scheduled
                WeighingBucketFilter.ADDED -> selections.containsKey(shed.operationalKey())
                WeighingBucketFilter.ALL -> true
            }
        }
}

/** Selected buckets in the park's own order, so the configure list never reshuffles under a tap. */
private fun WizardRaw.orderedSelections(): List<Pair<String, WizardSelection>> =
    shedsInPark().mapNotNull { shed -> selections[shed.operationalKey()]?.let { shed.operationalKey() to it } }

private fun WizardRaw.filteredSelections(): List<Pair<String, WizardSelection>> {
    val query = configQuery.trim().lowercase()
    if (query.isBlank()) return orderedSelections()
    val names = shedsInPark().associate { it.operationalKey() to it.name.lowercase() }
    return orderedSelections().filter { (locationId, _) -> names[locationId]?.contains(query) == true }
}

private fun WizardRaw.toUiState(): WeighingWizardUiState {
    val today = LocalDate.now(ZoneId.of(WEIGHING_WIZARD_ZONE))
    val sheds = shedsInPark()
    val shedsById = sheds.associateBy { it.operationalKey() }
    val ordered = orderedSelections()
    val filteredBuckets = filteredBuckets()
    val shownBuckets = filteredBuckets.take(bucketCap)
    val filteredConfig = filteredSelections()
    val shownConfig = filteredConfig.take(configCap)
    val addedNames = ordered.mapNotNull { shedsById[it.first]?.name }
    val operatorNames = catalog?.operators.orEmpty().associate { it.userId to it.displayName }
    val perOperator = ordered.groupingBy { it.second.operatorUserId }.eachCount()
    val individualCount = ordered.count { it.second.category == INDIVIDUAL_ANIMAL_CATEGORY }
    val dateLabel = date?.let { iso ->
        runCatching {
            // exception:exempt date display fallback; unparseable date shows raw ISO string
            LocalDate.parse(iso, ISO_DATE).format(WIZARD_DAY)
        }.getOrDefault(iso)
    }.orEmpty()

    // MAINTAINER DECISION: an edit can only add/remove buckets, change a bucket's operator, and
    // change a bucket's mode -- it can never touch date or park, so those two steps are not just
    // locked, they are never advertised. Create mode is 5 steps (Date, Park, Buckets, Configure,
    // Review); edit mode is effectively 3 (Buckets, Configure, Review), and the stepper/eyebrow
    // must count and index against THAT, never the full 5-step enum's raw ordinal.
    val editing = editCampaignId != null
    val effectiveStepCount = if (editing) 3 else WeighingWizardStep.entries.size
    val effectiveStepIndex = if (!editing) {
        step.ordinal
    } else {
        when (step) {
            WeighingWizardStep.BUCKETS -> 0
            WeighingWizardStep.CONFIGURE -> 1
            WeighingWizardStep.REVIEW -> 2
            // Unreachable in edit mode; kept exhaustive rather than throwing on a step this flow
            // can never actually be on.
            WeighingWizardStep.DATE, WeighingWizardStep.PARK -> 0
        }
    }
    val firstStep = if (editing) WeighingWizardStep.BUCKETS else WeighingWizardStep.DATE

    return WeighingWizardUiState(
        step = step,
        stepCount = effectiveStepCount,
        stepDisplayIndex = effectiveStepIndex,
        isFirstStep = step == firstStep,
        loading = loading,
        busy = busy,
        message = message,
        savedCampaignId = savedCampaignId,
        canContinue = canContinue(),
        contextLine = contextLine(dateLabel, ordered.size),
        dateOptions = (0 until WEIGHING_WIZARD_DATE_OPTIONS).map { offset ->
            val day = today.plusDays(offset.toLong())
            WeighingWizardDateOption(
                isoDate = day.format(ISO_DATE),
                label = day.format(WIZARD_DAY),
                note = if (offset == 0) "today" else "upcoming",
                selected = date == day.format(ISO_DATE),
            )
        },
        selectedDate = date,
        dateLabel = dateLabel,
        parkOptions = catalog?.parks.orEmpty().map { park ->
            WeighingWizardParkOption(
                parkId = park.parkId,
                name = park.name,
                // The park's OWN shed total, as the backend counted it. Never a count of loaded
                // rows: the park read sends none, and a bucket page holds ~20 of a 76-shed park.
                subtitle = "${park.shedCount} ${bucketWord(park.shedCount)}",
                selected = parkId == park.parkId,
            )
        },
        selectedParkId = parkId,
        parkName = park()?.name.orEmpty(),
        bucketQuery = bucketQuery,
        bucketFilter = bucketFilter,
        availableCount = sheds.count { !it.scheduled },
        takenCount = sheds.count { it.scheduled },
        addedCount = ordered.size,
        allCount = sheds.size,
        bucketRows = shownBuckets.map { shed ->
            val bucketKey = shed.operationalKey()
            WeighingWizardBucketRow(
                locationId = bucketKey,
                name = shed.name,
                // A taken bucket says WHO holds it and in what state, so a planner can act on it
                // instead of guessing. An available one says nothing more than that.
                reason = when {
                    shed.scheduled -> buildString {
                        append("Already scheduled ")
                        append(dateLabel.ifBlank { date.orEmpty() })
                        val operator = shed.scheduledOperatorDisplayName
                        if (operator.isNotBlank()) {
                            append(" · ")
                            append(operator)
                        }
                        val category = categoryLabel(shed.scheduledCategory)
                        if (category.isNotBlank()) {
                            append(" · ")
                            append(category)
                        }
                        val status = shed.scheduledStatus.trim().replace('_', ' ')
                        if (status.isNotBlank()) {
                            append(" · ")
                            append(status)
                        }
                    }
                    selections.containsKey(bucketKey) -> "added to this task"
                    else -> "available"
                },
                taken = shed.scheduled,
                added = selections.containsKey(bucketKey),
            )
        },
        bucketShownCount = shownBuckets.size,
        bucketTotalCount = filteredBuckets.size,
        bucketsAddable = filteredBuckets.count { !it.scheduled && !selections.containsKey(it.operationalKey()) },
        addedTray = addedNames.take(WEIGHING_WIZARD_TRAY_CAP),
        addedTrayMore = (addedNames.size - WEIGHING_WIZARD_TRAY_CAP).coerceAtLeast(0),
        configQuery = configQuery,
        configSearchOpen = configSearchOpen,
        configRows = shownConfig.map { (locationId, selection) ->
            val shed = shedsById[locationId]
            WeighingWizardConfigRow(
                locationId = locationId,
                name = shed?.name.orEmpty(),
                // A faint estimate of herd size, never a target and never a denominator: weighing
                // is free-flow and has no expected-animal roster to measure against.
                estimateLabel = shed?.kidCount?.takeIf { it > 0 }?.let { "est. ~$it" }.orEmpty(),
                category = selection.category,
                operatorUserId = selection.operatorUserId,
                operatorLabel = operatorNames[selection.operatorUserId].orEmpty(),
                ticked = locationId in picked,
            )
        },
        configShownCount = shownConfig.size,
        configTotalCount = filteredConfig.size,
        tickedCount = picked.size,
        // Only people who work THIS park (plus anyone tenant-scoped).
        operators = operatorsForPark().map {
            WeighingWizardOperatorOption(userId = it.userId, displayName = it.displayName)
        },
        // Counts stay NUMBERS. The screen names their unit and joins them, in the reader's
        // own language -- a sentence built here can only ever be English.
        configIndividualCount = individualCount,
        configLumpSumCount = ordered.size - individualCount,
        configPerOperator = perOperator.entries
            .filter { it.key.isNotBlank() }
            .map { (userId, count) ->
                WeighingWizardOperatorLoad(
                    displayName = operatorNames[userId].orEmpty(),
                    shedCount = count,
                )
            },
        repeatSourceLabel = repeat?.let { "From ${it.parkName} · ${it.sourceDateLabel}" },
        repeatDroppedCount = repeatDropped,
        reviewRows = ordered.map { (locationId, selection) ->
            WeighingWizardReviewRow(
                locationId = locationId,
                name = shedsById[locationId]?.name.orEmpty(),
                categoryLabel = categoryLabel(selection.category),
                operatorLabel = operatorNames[selection.operatorUserId] ?: "Operator",
            )
        },
        reviewOperatorLabel = perOperator.keys
            .filter { it.isNotBlank() }
            .joinToString(", ") { operatorNames[it] ?: "Operator" },
        // One operator carrying every bucket on a multi-bucket task is legal but rarely intended,
        // so the review says so rather than blocking it.
        lopsidedOperatorLabel = perOperator.entries
            .firstOrNull { it.value == ordered.size && ordered.size > 1 }
            ?.let { operatorNames[it.key] ?: "Operator" },
        isEditing = editCampaignId != null,
    )
}

private fun WizardRaw.contextLine(dateLabel: String, addedCount: Int): String = when (step) {
    WeighingWizardStep.DATE -> if (date != null) dateLabel else "Pick a date to continue"
    WeighingWizardStep.PARK -> park()?.let { "${it.name} · ${it.shedCount} ${bucketWord(it.shedCount)}" }
        ?: "Pick a park to continue"
    WeighingWizardStep.BUCKETS -> if (addedCount > 0) {
        "$addedCount ${bucketWord(addedCount)} added"
    } else {
        "Add at least one shed bucket"
    }
    WeighingWizardStep.CONFIGURE -> "$addedCount ${bucketWord(addedCount)} configured"
    WeighingWizardStep.REVIEW -> "$dateLabel · ${park()?.name.orEmpty()} · $addedCount ${bucketWord(addedCount)}"
}

private fun bucketWord(count: Int): String = if (count == 1) "shed bucket" else "shed buckets"

private fun WeighingPlannerShed.operationalKey(): String =
    listOfNotNull(locationId.takeIf { it.isNotBlank() }, partitionLabel?.takeIf { it.isNotBlank() })
        .joinToString("|")

private fun WeighingRepeatBucket.operationalKey(): String =
    listOfNotNull(locationId.takeIf { it.isNotBlank() }, partitionLabel?.takeIf { it.isNotBlank() })
        .joinToString("|")

private fun categoryLabel(category: String): String = when (category.trim().lowercase()) {
    INDIVIDUAL_ANIMAL_CATEGORY -> "Individual"
    PER_SHED_PARTITION_CATEGORY -> "Lump-sum"
    else -> ""
}
