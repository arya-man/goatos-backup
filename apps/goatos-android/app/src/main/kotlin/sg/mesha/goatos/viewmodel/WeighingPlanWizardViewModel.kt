package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.weighing.WeighingPlanDraft
import sg.mesha.goatos.core.data.weighing.WeighingPlannerCatalog
import sg.mesha.goatos.core.data.weighing.WeighingPlannerPark
import sg.mesha.goatos.core.data.weighing.WeighingPlannerOperator
import sg.mesha.goatos.core.data.weighing.WeighingPlannerShed
import sg.mesha.goatos.core.data.weighing.WEIGHING_LEADERSHIP_MAX_WINDOW
import sg.mesha.goatos.core.data.weighing.WEIGHING_LEADERSHIP_PAGE_SIZE
import sg.mesha.goatos.core.data.weighing.WeighingRepository
import sg.mesha.goatos.core.network.WEIGHING_PAGE_SIZE
import sg.mesha.goatos.feature.weighing.plan.WeighingBucketFilter
import sg.mesha.goatos.feature.weighing.plan.WeighingRepeatSeed
import sg.mesha.goatos.feature.weighing.plan.WeighingRepeatSeedStore
import sg.mesha.goatos.feature.weighing.plan.WeighingWizardBucketRow
import sg.mesha.goatos.feature.weighing.plan.WeighingWizardConfigRow
import sg.mesha.goatos.feature.weighing.plan.WeighingWizardDateOption
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
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    /**
     * The answers carried over when the planner started this task from an existing one.
     *
     * Consumed ONCE, at construction. The wizard still opens on the DATE step with no date chosen:
     * a repeat is a NEW task on a new day, and its buckets are re-checked for availability against
     * whichever date is picked. Nothing is copied from the source campaign row.
     */
    private val repeatSeed: WeighingRepeatSeed? =
        savedStateHandle.get<String>(Routes.WEIGHING_REPEAT_OF_ARG)
            ?.takeIf { it.isNotBlank() }
            ?.let { repeatSeedStore.take(it) }

    private val raw = MutableStateFlow(WizardRaw(repeat = repeatSeed))

    /**
     * How many cached SHED rows of the CHOSEN park the bucket step observes. Sheds, not parks: the
     * park read has no cursor and offers every park, while a single park holds 76+ sheds and is the
     * only side that pages. Bounded by the cache ceiling.
     */
    private val bucketWindow = MutableStateFlow(WEIGHING_LEADERSHIP_PAGE_SIZE)
    private var observeCatalogJob: Job? = null
    private var observeBucketsJob: Job? = null

    /** The chosen park's cached cursor state. Stops the scroll prefetch at the end of that park. */
    private var bucketsEndReached = false

    val state: StateFlow<WeighingWizardUiState> = raw
        .map { it.toUiState() }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), WizardRaw().toUiState())

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
    }

    /**
     * Steps backwards inside the wizard. Returns false only on the first step, where the caller
     * leaves the screen — so Back never drops a half-built task by accident mid-flow.
     */
    fun back(): Boolean {
        val current = raw.value
        val previous = current.step.previous() ?: return false
        raw.value = current.copy(step = previous)
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
        val today = LocalDate.now(ZoneId.of(WEIGHING_WIZARD_ZONE))
        val parsed = runCatching { LocalDate.parse(isoDate, ISO_DATE) }.getOrNull() ?: return
        if (parsed.isBefore(today)) return
        val current = raw.value
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
        if (current.parkId == parkId) return
        val date = current.date ?: return
        raw.value = current.copy(
            parkId = parkId,
            buckets = emptyList(),
            bucketsParkId = null,
            selections = emptyMap(),
            picked = emptySet(),
            bucketQuery = "",
            bucketCap = WEIGHING_PAGE_SIZE,
        )
        loadParkBuckets(date, parkId)
    }

    // ---- step 3: shed buckets ------------------------------------------------------------

    fun setBucketQuery(query: String) {
        raw.value = raw.value.copy(bucketQuery = query, bucketCap = WEIGHING_PAGE_SIZE)
    }

    fun setBucketFilter(filter: WeighingBucketFilter) {
        raw.value = raw.value.copy(bucketFilter = filter, bucketCap = WEIGHING_PAGE_SIZE)
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
            return
        }
        if (date == null || parkId == null || bucketsEndReached) return
        if (bucketWindow.value < WEIGHING_LEADERSHIP_MAX_WINDOW) {
            bucketWindow.value =
                (bucketWindow.value + WEIGHING_LEADERSHIP_PAGE_SIZE).coerceAtMost(WEIGHING_LEADERSHIP_MAX_WINDOW)
        }
        raw.value = current.copy(bucketCap = current.bucketCap + WEIGHING_PAGE_SIZE)
        refreshParkBuckets(date, parkId, reset = false)
    }

    /**
     * Adds or removes a bucket. A bucket already scheduled that day is refused HERE, with its
     * reason, rather than at publish — the block is visible before any configuration is spent.
     */
    fun toggleBucket(locationId: String) {
        val current = raw.value
        val shed = current.shedsInPark().firstOrNull { it.locationId == locationId } ?: return
        if (shed.scheduled) return
        val selections = current.selections.toMutableMap()
        if (selections.remove(locationId) == null) {
            selections[locationId] = WizardSelection(
                category = PER_SHED_PARTITION_CATEGORY,
                operatorUserId = current.defaultOperatorId(),
            )
        }
        raw.value = current.copy(selections = selections, picked = current.picked - locationId)
    }

    fun addAllVisibleBuckets() {
        val current = raw.value
        val selections = current.selections.toMutableMap()
        current.filteredBuckets()
            .filterNot { it.scheduled || selections.containsKey(it.locationId) }
            .forEach { shed ->
                selections[shed.locationId] = WizardSelection(
                    category = PER_SHED_PARTITION_CATEGORY,
                    operatorUserId = current.defaultOperatorId(),
                )
            }
        raw.value = current.copy(selections = selections)
    }

    fun clearAllBuckets() {
        raw.value = raw.value.copy(selections = emptyMap(), picked = emptySet())
    }

    // ---- step 4: configure ---------------------------------------------------------------

    fun setConfigQuery(query: String) {
        raw.value = raw.value.copy(configQuery = query, configCap = WEIGHING_PAGE_SIZE)
    }

    fun toggleConfigSearch() {
        val current = raw.value
        raw.value = current.copy(
            configSearchOpen = !current.configSearchOpen,
            configQuery = if (current.configSearchOpen) "" else current.configQuery,
        )
    }

    fun loadMoreConfigRows() {
        val current = raw.value
        if (current.configCap >= current.filteredSelections().size) return
        raw.value = current.copy(configCap = current.configCap + WEIGHING_PAGE_SIZE)
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
    }

    fun toggleConfigPick(locationId: String) {
        val current = raw.value
        raw.value = current.copy(
            picked = if (locationId in current.picked) current.picked - locationId else current.picked + locationId,
        )
    }

    fun pickAllShownConfigRows() {
        val current = raw.value
        raw.value = current.copy(picked = current.filteredSelections().map { it.first }.toSet())
    }

    fun clearConfigPicks() {
        raw.value = raw.value.copy(picked = emptySet())
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
    }

    // ---- step 5: commit ------------------------------------------------------------------

    /**
     * Saves the task. [publish] runs the real publish call after the create; without it the task
     * stays a draft, which is a state the planner can come back to.
     */
    fun commit(publish: Boolean) {
        val current = raw.value
        if (current.busy || current.savedCampaignId != null) return
        val date = current.date ?: return
        val park = current.park() ?: return
        val rows = current.orderedSelections()
        if (rows.isEmpty()) return
        if (rows.any { it.second.operatorUserId.isBlank() }) {
            raw.value = current.copy(message = "Every shed bucket needs one operator before this task can be saved.")
            return
        }
        raw.value = current.copy(busy = true, message = null)
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
                sheds = rows.map { (locationId, selection) ->
                    val shed = current.shedsInPark().first { it.locationId == locationId }
                    WeighingPlannerShed(
                        locationId = shed.locationId,
                        name = shed.name,
                        kidCount = shed.kidCount,
                        category = selection.category,
                        operatorUserId = selection.operatorUserId,
                    )
                },
            )
            when (val result = repository.createPlan(draft, publish)) {
                is AppResult.Ok -> raw.value = raw.value.copy(busy = false, savedCampaignId = result.value)
                is AppResult.Err -> raw.value = raw.value.copy(busy = false, message = result.message)
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
        if (raw.value.loading) return
        raw.value = raw.value.copy(loading = true)
        viewModelScope.launch {
            when (val result = repository.refreshPlannerCatalog(isoDate)) {
                is AppResult.Ok -> raw.value = raw.value.copy(loading = false)
                // The cached park list stays on screen; the wizard says what did not land rather
                // than dropping the planner back to an empty picker.
                is AppResult.Err -> raw.value = raw.value.copy(loading = false, message = result.message)
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
    @OptIn(ExperimentalCoroutinesApi::class)
    private fun loadParkBuckets(isoDate: String, parkId: String) {
        bucketWindow.value = WEIGHING_LEADERSHIP_PAGE_SIZE
        bucketsEndReached = false
        observeBucketsJob?.cancel()
        observeBucketsJob = viewModelScope.launch {
            bucketWindow.flatMapLatest { window ->
                repository.observePlannerParkBuckets(isoDate, parkId, window)
            }.collect { cached ->
                // A stale emission for a park the planner has already moved off must not repopulate
                // the list under the new park.
                if (raw.value.parkId != parkId) return@collect
                if (!cached.hasCache && cached.sheds.isEmpty()) return@collect
                bucketsEndReached = !cached.canLoadMore
                raw.value = raw.value
                    .copy(buckets = cached.sheds, bucketsParkId = parkId)
                    .withRepeatBucketsApplied()
            }
        }
        refreshParkBuckets(isoDate, parkId, reset = true)
    }

    private fun refreshParkBuckets(isoDate: String, parkId: String, reset: Boolean) {
        if (raw.value.loading) return
        raw.value = raw.value.copy(loading = true)
        viewModelScope.launch {
            when (val result = repository.refreshPlannerParkBuckets(isoDate, parkId, reset = reset)) {
                is AppResult.Ok -> raw.value = raw.value.copy(loading = false)
                is AppResult.Err -> raw.value = raw.value.copy(loading = false, message = result.message)
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
    val shedsById = buckets.associateBy { it.locationId }
    val operatorIds = operatorsForPark().map { it.userId }.toSet()
    val carried = seed.buckets.mapNotNull { bucket ->
        val shed = shedsById[bucket.locationId] ?: return@mapNotNull null
        if (shed.scheduled) return@mapNotNull null
        val category = when (bucket.category.trim().lowercase()) {
            INDIVIDUAL_ANIMAL_CATEGORY -> INDIVIDUAL_ANIMAL_CATEGORY
            else -> PER_SHED_PARTITION_CATEGORY
        }
        bucket.locationId to WizardSelection(
            category = category,
            // An operator the catalog no longer offers for this date is not carried over, and no
            // stand-in is invented either: the catalog's operator list is tenant-wide, so "the
            // first one" could be someone who does not work this park. The bucket comes across
            // UNASSIGNED, which the configure step shows and the save gate refuses until the
            // planner picks somebody.
            operatorUserId = bucket.operatorUserId.takeIf { it in operatorIds }.orEmpty(),
        )
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

private fun WizardRaw.canContinue(): Boolean = when (step) {
    WeighingWizardStep.DATE -> date != null
    WeighingWizardStep.PARK -> parkId != null
    WeighingWizardStep.BUCKETS -> selections.isNotEmpty()
    WeighingWizardStep.CONFIGURE -> selections.isNotEmpty()
    WeighingWizardStep.REVIEW -> false
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
                WeighingBucketFilter.ADDED -> selections.containsKey(shed.locationId)
                WeighingBucketFilter.ALL -> true
            }
        }
}

/** Selected buckets in the park's own order, so the configure list never reshuffles under a tap. */
private fun WizardRaw.orderedSelections(): List<Pair<String, WizardSelection>> =
    shedsInPark().mapNotNull { shed -> selections[shed.locationId]?.let { shed.locationId to it } }

private fun WizardRaw.filteredSelections(): List<Pair<String, WizardSelection>> {
    val query = configQuery.trim().lowercase()
    if (query.isBlank()) return orderedSelections()
    val names = shedsInPark().associate { it.locationId to it.name.lowercase() }
    return orderedSelections().filter { (locationId, _) -> names[locationId]?.contains(query) == true }
}

private fun WizardRaw.toUiState(): WeighingWizardUiState {
    val today = LocalDate.now(ZoneId.of(WEIGHING_WIZARD_ZONE))
    val sheds = shedsInPark()
    val shedsById = sheds.associateBy { it.locationId }
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
        runCatching { LocalDate.parse(iso, ISO_DATE).format(WIZARD_DAY) }.getOrDefault(iso)
    }.orEmpty()

    return WeighingWizardUiState(
        step = step,
        stepCount = WeighingWizardStep.entries.size,
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
            WeighingWizardBucketRow(
                locationId = shed.locationId,
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
                    selections.containsKey(shed.locationId) -> "added to this task"
                    else -> "available"
                },
                taken = shed.scheduled,
                added = selections.containsKey(shed.locationId),
            )
        },
        bucketShownCount = shownBuckets.size,
        bucketTotalCount = filteredBuckets.size,
        bucketsAddable = filteredBuckets.count { !it.scheduled && !selections.containsKey(it.locationId) },
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
        configSummary = buildString {
            append(individualCount)
            append(" individual · ")
            append(ordered.size - individualCount)
            append(" lump-sum")
            perOperator.entries
                .filter { it.key.isNotBlank() }
                .forEach { (userId, count) ->
                    append(" · ")
                    append(operatorNames[userId] ?: "Operator")
                    append(' ')
                    append(count)
                }
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

private fun categoryLabel(category: String): String = when (category.trim().lowercase()) {
    INDIVIDUAL_ANIMAL_CATEGORY -> "Individual"
    PER_SHED_PARTITION_CATEGORY -> "Lump-sum"
    else -> ""
}
