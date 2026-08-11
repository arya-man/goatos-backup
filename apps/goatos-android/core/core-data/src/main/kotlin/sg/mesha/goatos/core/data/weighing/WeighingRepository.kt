package sg.mesha.goatos.core.data.weighing

import androidx.room.withTransaction
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.coroutines.flow.map
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlinx.coroutines.flow.map
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.doubleOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.GoatDatabase
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.appApiStatusCode
import sg.mesha.goatos.core.network.userFacingMessage
import sg.mesha.goatos.core.network.WeightHistoryResponseDto
import sg.mesha.goatos.core.network.dto.WeighingAcceptedObservationDto
import sg.mesha.goatos.core.network.dto.WeighingAnimalObservationRequestDto
import sg.mesha.goatos.core.network.dto.WeighingCampaignDto
import sg.mesha.goatos.core.network.dto.WeighingCampaignShedDto
import sg.mesha.goatos.core.network.dto.WeighingObservationDto
import sg.mesha.goatos.core.network.dto.WeighingOperatorSummaryDto
import sg.mesha.goatos.core.network.dto.WeighingPlannerOperatorDto
import sg.mesha.goatos.core.network.dto.WeighingPlannerShedDto
import sg.mesha.goatos.core.network.dto.WeighingCampaignSummaryDto
import sg.mesha.goatos.core.network.dto.WeighingCreateCampaignRequestDto
import sg.mesha.goatos.core.network.dto.WeighingCreateCampaignShedDto
import sg.mesha.goatos.core.network.dto.WeighingLeadershipShedVideosDto
import sg.mesha.goatos.core.network.dto.WeighingShedObservationRequestDto
import sg.mesha.goatos.core.network.dto.WeighingScopeCloseRequestDto
import sg.mesha.goatos.core.network.dto.WeighingScopeReopenRequestDto
import sg.mesha.goatos.core.network.dto.WeighingScopeSubmitRequestDto
import sg.mesha.goatos.core.network.WEIGHING_PAGE_SIZE
import sg.mesha.goatos.core.network.WEIGHING_SCOPE_MINE
import sg.mesha.goatos.core.network.WEIGHING_SCOPE_ALL
import sg.mesha.goatos.core.network.WEIGHING_SCOPE_OPERATORS
import sg.mesha.goatos.core.network.MAX_OBSERVED_WINDOW
import sg.mesha.goatos.core.network.MAX_SCOPE_HYDRATION_ROWS
import java.util.UUID
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter

data class WeighingScanMatch(
    val row: WeighingRosterRowEntity?,
    val outcome: String,
    val expectedLocationLabel: String?,
    val actualLocationLabel: String?,
)

data class IndividualWeighingDraft(
    val observationId: String,
    /** Free-flow identity: the scanned tag. There is no animal id anywhere in weighing. */
    val scannedIdentifier: String,
    val weightKg: Double,
    val capturedAtMs: Long,
    val proofCaptureId: String?,
    val proofReady: Boolean,
    val readyToSubmit: Boolean,
    val syncedToBackend: Boolean,
    val idempotencyKey: String,
    val serverProofId: String? = null,
    /** Verifier verdict for this capture: "rework" is the one the operator must act on. */
    val verificationStatus: String? = null,
    val reworkReason: String? = null,
)

data class ShedWeighingDraft(
    val shedObservationId: String,
    val resultJson: String,
    val proofReady: Boolean,
    val readyToSubmit: Boolean,
    val idempotencyKey: String,
)

data class WeighingScopeState(
    val rosterWindow: List<WeighingRosterRowEntity>,
    val individualDrafts: List<IndividualWeighingDraft>,
    val shedDrafts: List<ShedWeighingDraft>,
    val totalExpected: Int,
)

data class WeighingAssignment(
    val campaignId: String,
    val tenantId: String,
    val parkId: String,
    val parkName: String,
    val workGroupId: String,
    val campaignShedId: String,
    val expectedLocationId: String,
    val expectedLocationLabel: String,
    val label: String,
    val category: String,
    val operatorUserId: String,
    /**
     * Backend-resolved assignee NAME, carried on the bucket itself.
     *
     * The oversight surface renders who did the work, so the name has to travel WITH the row.
     * Blank with a non-blank [operatorUserId] is a roster gap, not "not assigned yet"; a user id is
     * never rendered in its place, and the client never joins the row against a separately paged
     * operator vocabulary to find it.
     */
    val operatorDisplayName: String = "",
    val status: String,
    // No expectedCount / denominator here: weighing is free-flow, so there is no expected-animal
    // list to count against. See docs/decisions/mobile-data-fetch-anti-patterns.md.
    val periodLabel: String,
    val readyToClose: Boolean = false,
    val pendingVerificationCount: Int = 0,
)

/**
 * ONE weighing task: one park on ONE weigh date, holding its shed buckets.
 *
 * This is the grain the planner's task list renders. It is deliberately SEPARATE from
 * [WeighingAssignment], which stays the per-shed executable grain the operator work list and the
 * oversight surface need. A campaign is never flattened for this list: flattening is what made the
 * task list show one card per shed instead of one card per task.
 */
data class WeighingTask(
    val campaignId: String,
    val tenantId: String,
    val parkId: String,
    val parkName: String,
    /** The Asia/Kolkata business DATE this task is weighed on. One task = one date. */
    val weighDate: String,
    val status: String,
    /**
     * The backend-recorded sentence for WHY this task was ended. Blank while it is still live.
     * Clients render it; they never author it.
     */
    val closeReason: String = "",
    val sheds: List<WeighingTaskShed>,
)

data class WeighingTaskShed(
    val campaignShedId: String,
    val locationId: String,
    val displayName: String,
    val partitionLabel: String? = null,
    val category: String,
    val operatorUserId: String,
    /**
     * Backend-resolved assignee name, carried ON the bucket. Blank WITH a non-blank
     * [operatorUserId] is a roster gap, not "not assigned". Resolving this by joining against the
     * separately paged planner catalog left buckets past the catalog's first page nameless.
     */
    val operatorDisplayName: String = "",
    val status: String,
    val pendingVerificationCount: Int,
    val reworkCount: Int,
    val readyToClose: Boolean,
    /**
     * FACT 1 of 2. Backend-owned count of the ANIMALS this bucket has a recorded weight for,
     * submitted or not. A plain count, never divided by anything: weighing is free-flow, so there
     * is no expected-animal total that a share could be taken of.
     */
    val animalsWeighedCount: Int = 0,
    /**
     * FACT 2 of 2. The subset of [animalsWeighedCount] SUBMITTED for verification. Shown WITH the
     * weighed count ("N weighed · N submitted"), never on its own.
     */
    val animalsSubmittedCount: Int = 0,
)

/**
 * One keyset page of tasks plus the WHOLE-FILTER tab tallies.
 *
 * [activeCount] and [completedCount] are backend-owned and range over the entire scope, never over
 * [items]: counting the page would make the tab numbers jump on every scroll.
 */
data class WeighingTaskPage(
    val items: List<WeighingTask> = emptyList(),
    val nextCursor: String? = null,
    val activeCount: Int = 0,
    val completedCount: Int = 0,
    val capabilities: WeighingCapabilities = WeighingCapabilities(),
)

/**
 * Which task-level writes the signed-in viewer may attempt, as the backend states them.
 *
 * Publishing and ending a task are held by DIFFERENT permissions, so status alone is not a gate:
 * a viewer who may monitor but not plan must see publish disabled, not live-and-then-refused.
 * Everything defaults to false so a server that does not answer offers nothing.
 */
data class WeighingCapabilities(
    val canPublish: Boolean = false,
    val canEnd: Boolean = false,
    val canReopen: Boolean = false,
) {
    /**
     * The CSV export shares the SAME backend permission (weighing.monitor) as ending a task -- the
     * contract carries no separate export flag, so there is nothing to gain by adding one on the
     * client. Named distinctly so a caller reads "may this viewer export" rather than having to
     * remember which unrelated action [canEnd] otherwise names.
     */
    val canExportCsv: Boolean get() = canEnd
}

/**
 * The bytes of ONE task's CSV export, downloaded straight from the backend.
 *
 * [suggestedFileName] mirrors the backend-documented `Content-Disposition` filename
 * (`weighing-export-<campaign_id>.csv`) -- the repository builds it from the same campaign id the
 * caller already has rather than parsing the header back out of the response, since [AppApi]'s
 * download method hands back the body only, not the wrapping HTTP response.
 */
data class WeighingCsvExport(
    val bytes: ByteArray,
    val suggestedFileName: String,
) {
    override fun equals(other: Any?): Boolean =
        this === other || (other is WeighingCsvExport && suggestedFileName == other.suggestedFileName && bytes.contentEquals(other.bytes))

    override fun hashCode(): Int = 31 * suggestedFileName.hashCode() + bytes.contentHashCode()
}

/**
 * One park the viewer may filter weighing by, as the BACKEND scopes it. Identity only.
 *
 * Deliberately not [WeighingPlannerPark]: that grain carries date-scoped counts and lives behind
 * the planning permission, which the leadership roles that need these chips do not hold.
 */
data class WeighingParkRef(
    val parkId: String,
    val name: String,
)

/**
 * The answer to "resolve THIS task id".
 *
 * [NotFound] is the single answer for every refusal the backend makes -- not yours, not there,
 * wrong park. The backend deliberately does not distinguish them, so neither does this.
 */
sealed interface WeighingTaskLookup {
    data class Found(val task: WeighingTask, val capabilities: WeighingCapabilities) : WeighingTaskLookup
    data object NotFound : WeighingTaskLookup
}

data class WeighingLeadershipVideo(
    val proofId: String,
    val downloadUrl: String,
)

data class WeighingLeadershipAnimal(
    /** The backend's stable id for this captured record. The list keys on it. */
    val observationId: String,
    val rfid: String,
    val weightKg: Double,
    val acceptedAt: String,
    val videos: List<WeighingLeadershipVideo>,
)

data class WeighingLeadershipShed(
    /** Stable Room/backend bucket key: campaignId:campaignShedId. */
    val shedKey: String,
    val campaignId: String,
    val campaignShedId: String,
    val shedName: String,
    /** The partition label (e.g., "Part 3"), or empty/null for undivided sheds. */
    val partitionLabel: String? = null,
    /** Backend-composed display string (e.g., "Godel 1 - Part 3"). Prefer this over hand-rolling. */
    val operationalLocationDisplay: String = "",
    /** The park this bucket's task belongs to, as the shed read itself answers it. */
    val parkName: String = "",
    /** The task's Asia/Kolkata business DATE. Never a timestamp. */
    val weighDate: String = "",
    /** Assigned operator id. Blank is the only thing that means nobody is assigned yet. */
    val operatorUserId: String,
    /**
     * Backend-resolved assignee name. Blank WITH a non-blank [operatorUserId] is a roster gap, not
     * "not assigned yet".
     */
    val operatorDisplayName: String = "",
    val category: String,
    val status: String,
    val periodLabel: String,
    /** Backend herd estimate for the shed. A coverage hint, never a completeness denominator. */
    val estimatedAnimalCount: Int,
    /** Backend group-video allowance behind the "N of M" reading. */
    val maxShedVideos: Int,
    val animals: List<WeighingLeadershipAnimal>,
    val animalCount: Int?,
    val totalWeightKg: Double?,
    val averageWeightKg: Double?,
    val videos: List<WeighingLeadershipVideo>,
)

/**
 * The PARK-grain planner vocabulary for ONE weigh date: every park the planner may pick, plus the
 * operator picker. It carries NO shed rows — those page per park through
 * [WeighingRepository.observePlannerParkBuckets].
 */
data class WeighingPlannerCatalog(
    val parks: List<WeighingPlannerPark>,
    val operators: List<WeighingPlannerOperator>,
)

data class WeighingPlannerPark(
    val parkId: String,
    val name: String,
    val kidCount: Int,
    /**
     * The park's own active-shed total, as the BACKEND counted it over that park's children. Never
     * a count of cached or fetched shed rows: a bucket page holds ~20 of a 76-shed park.
     */
    val shedCount: Int,
    val existingCampaign: WeighingCampaignSummary?,
)

data class WeighingPlannerShed(
    val locationId: String,
    val name: String,
    val partitionLabel: String? = null,
    val kidCount: Int,
    val category: String = "per_shed_partition",
    val operatorUserId: String = "",
    /**
     * Server-answered availability for the weigh date the catalog was asked for: true when an open
     * weighing task already holds this shed on that date. Duplicate work is refused at the write
     * too, so this only exists to explain the block BEFORE a planner spends five steps on it.
     */
    val scheduled: Boolean = false,
    val scheduledStatus: String = "",
    val scheduledOperatorDisplayName: String = "",
    val scheduledCategory: String = "",
    /**
     * The operator user id CURRENTLY assigned to this bucket, when it already sits on a campaign --
     * including the wizard's OWN campaign in edit mode, where [scheduled] reads false because that
     * campaign is excluded from the "already taken" check. This is the live server answer for
     * "what is this bucket's assignment right now", never a snapshot: the edit wizard's own carried
     * seed is captured once when the wizard was staged and goes stale the moment anything on the
     * campaign changes after that, while this field is re-read on every catalog refresh.
     */
    val scheduledOperatorUserId: String = "",
)

data class WeighingCampaignSummary(
    val campaignId: String,
    val status: String,
    val periodStartDate: String,
    val periodEndDate: String,
    val startBusinessDate: String,
    val operatorUserId: String,
    val shedCount: Int,
)

data class WeighingPlannerOperator(
    val userId: String,
    val displayName: String,
    val displayCode: String,
    /**
     * Parks this person may be assigned weighing work in. EMPTY means every park.
     *
     * The picker filters on this so a planner cannot hand a bucket to someone from another park;
     * the write re-checks it, because a client is not a permission boundary.
     */
    val parkIds: List<String> = emptyList(),
)

data class WeighingPlanDraft(
    val parkId: String,
    val periodStartDate: String,
    val periodEndDate: String,
    val startBusinessDate: String,
    val plannedCapPerDay: Int,
    val operatorUserId: String,
    val sheds: List<WeighingPlannerShed>,
)

data class IndividualWeighingCapture(
    val tenantId: String,
    val campaignId: String,
    val workGroupId: String,
    val campaignShedId: String,
    /** Free-flow identity: the scanned tag. There is no animal id anywhere in weighing. */
    val scannedIdentifier: String,
    val weightKg: Double,
    val capturedAtMs: Long? = null,
)

data class ShedPartitionWeighingCapture(
    val tenantId: String,
    val campaignId: String,
    val workGroupId: String,
    val campaignShedId: String,
    val expectedLocationId: String,
    val expectedLocationLabel: String,
    val resultJson: String,
    val proofArtifactIds: List<String> = emptyList(),
    val capturedAtMs: Long? = null,
)

/**
 * One keyset page of a weighing list read. [nextCursor] is null or blank on the last page, so a
 * caller stops appending as soon as it is not a usable cursor. Pagination is app-owned viewport
 * behaviour: one page is one screen of work, never the whole list.
 */
data class WeighingPage<T>(
    val items: List<T> = emptyList(),
    val nextCursor: String? = null,
    /**
     * What the SIGNED-IN viewer may do to the rows on this page, as the backend states it.
     *
     * The assignment read used to drop this, so the only surface that knew a viewer held the
     * monitor authority was the planner task list -- which is why close/reopen were
     * unreachable from every assignment surface even for the role that owns them.
     */
    val capabilities: WeighingCapabilities = WeighingCapabilities(),
    /**
     * The backend's OPERATOR-grain roll-up for this request, carried beside the paged rows.
     *
     * It is deliberately NOT derived from [items]: [items] is one keyset page, so anything counted
     * from it would describe the page rather than the person and would change as the reader
     * scrolls. This list is whole-filter truth the server computed.
     */
    val operatorSummaries: List<WeighingOperatorSummary> = emptyList(),
)

/**
 * What ONE person's weighing work adds up to, as the backend counts it.
 *
 * Plain counts only. [notStarted] + [capturing] + [submitted] + [accepted] == [shedCount], which is
 * why the oversight screen can draw a discrete state ladder; nothing here is ever a numerator,
 * because free-flow weighing has no expected-animal total to divide by.
 */
data class WeighingOperatorSummary(
    /** Blank on the "nobody is assigned yet" row. */
    val operatorUserId: String,
    /** Backend-resolved name. Blank with a non-blank id is a roster gap, not "unassigned". */
    val operatorDisplayName: String,
    val shedCount: Int,
    val notStarted: Int,
    val capturing: Int,
    val submitted: Int,
    val accepted: Int,
    /** Buckets a verifier bounced back. Overlaps the four state counts; never added to them. */
    val rework: Int,
    /** FACT 1 of 2: animals this person has a recorded weight for, submitted or not. */
    val animalsWeighed: Int,
    /** FACT 2 of 2: the subset of [animalsWeighed] submitted for verification. */
    val animalsSubmitted: Int,
)


/**
 * The task list AS ROOM HOLDS IT: a bounded window of cached tasks plus the whole-scope tallies the
 * backend answered with. [canLoadMore] is the cached cursor's own state, so a screen knows there is
 * another page without asking the network first.
 */
data class WeighingTaskListCache(
    val items: List<WeighingTask> = emptyList(),
    val activeCount: Int = 0,
    val completedCount: Int = 0,
    val capabilities: WeighingCapabilities = WeighingCapabilities(),
    val canLoadMore: Boolean = false,
    /** True once a refresh has written this scope at least once. Distinguishes empty from unread. */
    val hasCache: Boolean = false,
    /**
     * When this scope was last written, in epoch millis. 0 means never.
     *
     * A cache bounded only by ROW COUNT has no age: an offline cold start renders month-old rows
     * with no signal at all, because a refresh FAILURE is the only staleness a screen can otherwise
     * see. Screens read this to say how old the answer is.
     */
    val cachedAt: Long = 0L,
)

/**
 * ONE task's shed buckets as Room holds them. [totalCount] is the WHOLE-TASK bucket count the
 * backend answered with — never the size of [items], so the header does not move while scrolling.
 */
data class WeighingTaskBucketCache(
    val items: List<WeighingTaskShed> = emptyList(),
    val totalCount: Int = 0,
    val canLoadMore: Boolean = false,
    val hasCache: Boolean = false,
    /** When this task's buckets were last written, in epoch millis. See [WeighingTaskListCache.cachedAt]. */
    val cachedAt: Long = 0L,
)

/** ONE shed bucket plus a bounded window of its captured records, as Room holds them. */
data class WeighingLeadershipShedCache(
    val shed: WeighingLeadershipShed? = null,
    val canLoadMoreRecords: Boolean = false,
    /** When this bucket was last written, in epoch millis. See [WeighingTaskListCache.cachedAt]. */
    val cachedAt: Long = 0L,
)

/**
 * The PARK-grain planner catalog as Room holds it.
 *
 * There is no `canLoadMore`: the park read has no cursor, because a park picker that pages cannot
 * offer the parks it has not reached.
 */
data class WeighingPlannerCatalogCache(
    val catalog: WeighingPlannerCatalog = WeighingPlannerCatalog(emptyList(), emptyList()),
    val hasCache: Boolean = false,
    /** When this catalog date was last written, in epoch millis. See [WeighingTaskListCache.cachedAt]. */
    val cachedAt: Long = 0L,
)

/** ONE park's cached shed buckets for ONE weigh date, as a bounded keyset window. */
data class WeighingPlannerParkBucketsCache(
    val parkId: String = "",
    val sheds: List<WeighingPlannerShed> = emptyList(),
    val canLoadMore: Boolean = false,
    val hasCache: Boolean = false,
    val cachedAt: Long = 0L,
)

interface WeighingRepository {
    fun observeScope(scopeKey: String, windowSize: Int): Flow<WeighingScopeState>

    /** One-shot snapshot of a scope's individual drafts, for a caller that has just awaited a
     *  refresh and needs THAT refresh's answer rather than whatever the observed stream currently
     *  holds. See WeighingObservationDao.listForScope for why the Flow cannot serve this. */
    suspend fun individualDraftsSnapshot(scopeKey: String): List<IndividualWeighingDraft>
    /**
     * Lists weighing assignments for ONE weighing surface.
     *
     * [scope] names the surface the caller renders rather than letting the server infer it from
     * the viewer's roles: [WEIGHING_SCOPE_MINE] is the caller's own assigned sheds and is the only
     * executable list, [WEIGHING_SCOPE_ALL] is the planner's flat all-tasks list, and
     * [WEIGHING_SCOPE_OPERATORS] is read-only oversight of other people's work.
     */
    suspend fun listAssignments(cursor: String? = null, scope: String = WEIGHING_SCOPE_MINE, parkId: String? = null): AppResult<WeighingPage<WeighingAssignment>>
    // --- Leadership reads: Room-backed, observed, keyset-paged -------------------------
    //
    // Every one of these is a PAIR: an `observe*` that renders from Room, and a `refresh*` that
    // asks the backend for one page and UPSERTS it. Nothing here returns network rows to a caller;
    // the observed Flow is the only way the data reaches a screen, so a failed refresh leaves the
    // cached answer on screen instead of blanking it.

    /**
     * The task list from Room, as a BOUNDED window of [windowSize] rows.
     *
     * [scope] and [parkId] together name the filter; two filters are two independent keyset streams.
     */
    fun observeTaskList(
        scope: String = WEIGHING_SCOPE_ALL,
        parkId: String? = null,
        windowSize: Int = WEIGHING_LEADERSHIP_PAGE_SIZE,
    ): Flow<WeighingTaskListCache>

    /**
     * Fetches ONE page of the task list into Room.
     *
     * [reset] true re-reads page 1 and drops the filter's stale rows; false appends the next keyset
     * page. Returns the number of rows written, or an error with the cache left untouched.
     */
    suspend fun refreshTaskList(
        scope: String = WEIGHING_SCOPE_ALL,
        parkId: String? = null,
        reset: Boolean = true,
    ): AppResult<Int>

    /**
     * Resolves ONE task by id, in ONE call.
     *
     * This is what a notification deep link asks: the task list is a keyset page with no id
     * filter, so a task further down the keyset used to be hunted by walking pages. A refusal of
     * ANY kind comes back as [WeighingTaskLookup.NotFound]; the caller must not report which kind.
     */
    suspend fun getTask(campaignId: String): AppResult<WeighingTaskLookup>

    /**
     * The parks whose weighing this viewer may look at, straight from the backend.
     *
     * Unpaged and capability-scoped by the server. It replaces deriving chips from loaded rows:
     * a vocabulary built from filtered data loses a park the moment that park's rows page out, and
     * selecting the park was the only way to load them -- a circle with no way in.
     */
    suspend fun listParks(): AppResult<List<WeighingParkRef>>

    /** ONE task's shed buckets from Room, as a BOUNDED window. */
    fun observeTaskBuckets(campaignId: String, windowSize: Int = WEIGHING_LEADERSHIP_PAGE_SIZE): Flow<WeighingTaskBucketCache>

    /** Fetches ONE page of a task's shed buckets into Room. */
    suspend fun refreshTaskBuckets(campaignId: String, reset: Boolean = true): AppResult<Int>

    /** Appends the next page of task list using the stored cursor. */
    suspend fun appendTaskList(scope: String, parkId: String?): AppResult<Int>

    /** Appends the next page of task buckets using the stored cursor. */
    suspend fun appendTaskBuckets(campaignId: String): AppResult<Int>

    /**
     * Downloads the task's full CSV export (every shed, including ones with nothing captured).
     *
     * A FILE, not a cached read: the export is a leadership snapshot taken at request time, so it
     * is never written into Room and never observed -- each call is a fresh network round trip,
     * same as [publishCampaign] or [closeCampaign]. Requires permission weighing.monitor.
     */
    suspend fun exportCampaignCsv(campaignId: String): AppResult<WeighingCsvExport>

    /**
     * ONE shed bucket from Room — its own context plus a bounded window of its captured records.
     *
     * The park name, weigh date and assignee name come from the cached shed row, so a cold deep
     * link renders them without the caller passing them in.
     */
    fun observeLeadershipShed(
        campaignId: String,
        campaignShedId: String,
        windowSize: Int = WEIGHING_LEADERSHIP_PAGE_SIZE,
    ): Flow<WeighingLeadershipShedCache>

    /**
     * Fetches ONE page of a shed bucket's captured records into Room, along with the shed's own
     * context. The lump-sum row is a single latest read and always refreshes with page 1.
     */
    suspend fun refreshLeadershipShed(campaignId: String, campaignShedId: String, reset: Boolean = true): AppResult<Int>

    /** Appends the next page of records for a shed using the stored cursor. */
    suspend fun appendLeadershipShed(campaignId: String, campaignShedId: String): AppResult<Int>

    /** The leadership videos gallery from Room, as a BOUNDED window of shed buckets. */
    fun observeLeadershipVideos(windowSize: Int = WEIGHING_LEADERSHIP_PAGE_SIZE): Flow<List<WeighingLeadershipShed>>

    /** Fetches ONE page of the videos gallery — one task page, then each of its buckets. */
    suspend fun refreshLeadershipVideos(reset: Boolean = true): AppResult<Int>

    /** Appends the next page of videos gallery using the stored cursor. */
    suspend fun appendLeadershipVideos(): AppResult<Int>

    /**
     * The PARK-grain planner catalog from Room: EVERY park the planner may use on that date.
     *
     * Bounded by a sanity cap on the park count, not paged — a park picker that paged could not
     * offer the parks it had not reached, which is exactly the defect this split removes.
     */
    fun observePlannerCatalog(periodStartDate: String): Flow<WeighingPlannerCatalogCache>

    /** Fetches the whole park-grain catalog into Room. One call; there is no park cursor. */
    suspend fun refreshPlannerCatalog(periodStartDate: String): AppResult<Int>

    /**
     * ONE park's shed buckets from Room, as a BOUNDED keyset window.
     *
     * This is the many side: a real park holds 76+ sheds, so it pages ~20 at a time and the
     * observed Room read is bounded to the same window the screen has actually asked for.
     */
    fun observePlannerParkBuckets(
        periodStartDate: String,
        parkId: String,
        windowSize: Int = WEIGHING_LEADERSHIP_PAGE_SIZE,
        // The task being EDITED, so its own sheds are never cached back as "taken" against
        // themselves. Also separates the edit wizard's cache scope from the create wizard's, so
        // the two never share (and clobber) one another's availability rows for the same park/date.
        excludeCampaignId: String? = null,
    ): Flow<WeighingPlannerParkBucketsCache>

    /**
     * Re-reads the AVAILABILITY of the pages already cached for this park/date, in place.
     *
     * Availability is a live fact owned by other people's tasks -- a shed can be taken, or
     * finish and become free, while the planner is mid-wizard. This exists because the two
     * existing modes both do the wrong thing when the planner simply RE-ENTERS the park:
     * reset=true deletes the cached pages and re-inserts only page 1, silently dropping any
     * bucket the planner already picked from a later page, and reset=false only appends the
     * NEXT page, refreshing nothing. This refreshes what is already there and adds nothing.
     */
    suspend fun refreshPlannerParkBucketAvailability(
        periodStartDate: String,
        parkId: String,
        pages: Int,
        excludeCampaignId: String? = null,
    ): AppResult<Int>

    /** Fetches ONE keyset page of ONE park's shed buckets into Room. */
    suspend fun refreshPlannerParkBuckets(
        periodStartDate: String,
        parkId: String,
        reset: Boolean = true,
        excludeCampaignId: String? = null,
    ): AppResult<Int>

    /** Appends the next page of park buckets using the stored cursor. */
    suspend fun appendPlannerParkBuckets(
        periodStartDate: String,
        parkId: String,
        excludeCampaignId: String? = null,
    ): AppResult<Int>

    suspend fun createAndPublishPlan(draft: WeighingPlanDraft): AppResult<WeighingAssignment?>

    /**
     * Creates ONE weighing task and returns its id.
     *
     * A created task is a DRAFT — a real state a planner can leave a task in. [publish] then runs
     * the SAME publish call the planner would run later, so a published task is never fabricated by
     * writing a status: it goes draft -> published through the one path that enforces the rules.
     */
    suspend fun createPlan(draft: WeighingPlanDraft, publish: Boolean): AppResult<String>
    suspend fun updatePlan(campaignId: String, draft: WeighingPlanDraft): AppResult<WeighingAssignment?>

    /**
     * Publishes a task that was left as a DRAFT, through the SAME publish call the authoring flow
     * runs. A draft is a real state a planner can come back to, so it needs a way out that is not
     * "author the whole task again"; nothing here writes a status directly.
     */
    suspend fun publishCampaign(campaignId: String): AppResult<Unit>
    suspend fun refreshScope(campaignId: String, workGroupId: String, campaignShedId: String, maxRows: Int = WEIGHING_PAGE_SIZE): AppResult<Int>
    suspend fun replaceRoster(scopeKey: String, rows: List<WeighingRosterRowEntity>)
    suspend fun matchTag(scopeKey: String, scannedTag: String): WeighingScanMatch
    suspend fun recordIndividual(capture: IndividualWeighingCapture): AppResult<IndividualWeighingDraft>
    suspend fun attachIndividualProof(scopeKey: String, scannedIdentifier: String, proofCaptureId: String, serverProofId: String?)
    suspend fun recordShedPartition(capture: ShedPartitionWeighingCapture): AppResult<ShedWeighingDraft>
    suspend fun attachShedPartitionProof(
        scopeKey: String,
        proofCaptureId: String,
        serverProofId: String?,
        serverProofIds: List<String> = emptyList(),
    )
    suspend fun discardEditableIndividual(scopeKey: String, scannedIdentifier: String)
    suspend fun submitIndividualScope(
        campaignId: String,
        campaignShedId: String,
        scannedIdentifiers: List<String>,
    ): AppResult<Unit>
    suspend fun reopenScope(
        campaignId: String,
        campaignShedId: String,
        reason: String = "",
    ): AppResult<Unit>
    suspend fun closeShedCampaign(
        campaignId: String,
        campaignShedId: String,
        reason: String = "",
    ): AppResult<Unit>
    suspend fun closeCampaign(
        campaignId: String,
        reason: String = "",
    ): AppResult<Unit>
    /** [parkId]/[campaignShedId] narrow the query server-side (both null = the caller's full
     *  authorized scope) — see `AppApi.getWeightHistory`. */
    suspend fun fetchWeightHistory(
        parkId: String? = null,
        campaignShedId: String? = null,
    ): AppResult<sg.mesha.goatos.core.network.WeightHistoryResponseDto>

    /** Leadership growth (ADG). parkId null = every park the caller may see. */
    suspend fun fetchGrowthSummary(
        parkId: String?,
        from: String?,
        to: String?,
    ): AppResult<sg.mesha.goatos.core.network.GrowthSummaryDto>
}

class DefaultWeighingRepository(
    private val api: AppApi? = null,
    private val tenantId: String = "",
    private val rosterDao: WeighingRosterDao,
    private val observationDao: WeighingObservationDao,
    private val shedObservationDao: WeighingShedObservationDao,
    private val database: GoatDatabase? = null,
    private val syncRepository: SyncRepository? = null,
    private val appScope: CoroutineScope? = null,
    private val clock: () -> Long = System::currentTimeMillis,
    private val idGenerator: () -> String = { UUID.randomUUID().toString() },
) : WeighingRepository {
    /**
     * Room's SSOT for the leadership reads. A repository built WITHOUT a database (narrow unit
     * tests of the capture path) has no cache: the observe* Flows stay empty and refresh* says so
     * rather than pretending a network answer is cached state.
     */
    private val taskDao: WeighingTaskDao? = database?.weighingTaskDao()
    private val taskKeyDao: WeighingTaskRemoteKeyDao? = database?.weighingTaskRemoteKeyDao()
    private val bucketDao: WeighingTaskBucketDao? = database?.weighingTaskBucketDao()
    private val bucketKeyDao: WeighingTaskBucketRemoteKeyDao? = database?.weighingTaskBucketRemoteKeyDao()
    private val leadershipShedDao: WeighingLeadershipShedDao? = database?.weighingLeadershipShedDao()
    private val leadershipRecordDao: WeighingLeadershipRecordDao? = database?.weighingLeadershipRecordDao()
    private val leadershipRecordKeyDao: WeighingLeadershipRecordRemoteKeyDao? =
        database?.weighingLeadershipRecordRemoteKeyDao()
    private val galleryKeyDao: WeighingLeadershipGalleryRemoteKeyDao? =
        database?.weighingLeadershipGalleryRemoteKeyDao()
    private val plannerDao: WeighingPlannerCatalogDao? = database?.weighingPlannerCatalogDao()
    private val plannerKeyDao: WeighingPlannerRemoteKeyDao? = database?.weighingPlannerRemoteKeyDao()

    private val cacheJson = Json { ignoreUnknownKeys = true; encodeDefaults = true }

    /**
     * One idempotency EPOCH per scope, rotated after every state transition that landed.
     *
     * The backend replays a close/reopen whose key it has already recorded and returns the
     * ORIGINAL result without touching state (weighing_idempotency_records). A key fixed per scope
     * therefore made `close -> reopen -> close` report success while the bucket stayed open: the
     * second close was answered from the first one's snapshot. Rotating on success -- and only on
     * success -- keeps the property idempotency exists for: retrying the SAME attempt after an
     * unknown outcome (timeout, dropped socket) still sends the SAME key and is deduplicated, while
     * a genuinely NEW transition after a landed one carries a new key and is really applied.
     */
    private val epochDao: WeighingTransitionEpochDao? = database?.weighingTransitionEpochDao()

    /**
     * The in-heap epoch store, used ONLY where there is no database (unit fakes constructed without
     * one). With a database present Room is the SSOT and this is never read.
     */
    private val inMemoryTransitionEpochs = java.util.concurrent.ConcurrentHashMap<String, String>()

    private suspend fun transitionIdempotencyKey(transition: String, rawScopeId: String): String {
        // The epoch is keyed by (transition, scope), never by scope alone. `update` and
        // `close-campaign` both scope to the bare campaignId, so a shared counter let a
        // landed close rotate the epoch out from under an in-flight update: the update's
        // retry then computed a DIFFERENT key, the server no longer recognised it as the
        // same attempt, and re-applied it -- the exact double-apply this mechanism exists
        // to prevent, crossing transition types instead of repeating within one.
        val scopeId = "$transition:$rawScopeId"
        val dao = epochDao ?: return "weighing:$transition:$scopeId:" +
            inMemoryTransitionEpochs.getOrPut(scopeId) { idGenerator() }
        // Claim-then-read: IGNORE on conflict means a concurrent attempt on the same scope loses
        // the write and then reads the winner's epoch, so both send the SAME key.
        dao.insertIfAbsent(
            WeighingTransitionEpochEntity(scopeId = scopeId, epoch = idGenerator(), updatedAt = clock()),
        )
        val epoch = dao.get(scopeId) ?: idGenerator()
        return "weighing:$transition:$scopeId:$epoch"
    }

    /** Called only after the server confirmed the transition, so a failed attempt stays retryable. */
    /** [transition] MUST match the one passed to [transitionIdempotencyKey]; the epoch is per pair. */
    private suspend fun advanceTransitionEpoch(transition: String, rawScopeId: String) {
        val scopeId = "$transition:$rawScopeId"
        val dao = epochDao ?: run {
            inMemoryTransitionEpochs[scopeId] = idGenerator()
            return
        }
        dao.upsert(WeighingTransitionEpochEntity(scopeId = scopeId, epoch = idGenerator(), updatedAt = clock()))
        dao.pruneOutsideNewest(WEIGHING_CACHED_TRANSITION_SCOPES)
    }

    init {
        startProofReadyReconciler()
    }

    override suspend fun individualDraftsSnapshot(scopeKey: String): List<IndividualWeighingDraft> =
        observationDao.listForScope(scopeKey).map { it.toDraft() }

    override fun observeScope(scopeKey: String, windowSize: Int): Flow<WeighingScopeState> =
        combine(
            rosterDao.observeWindow(scopeKey, windowSize.coerceIn(1, MAX_OBSERVED_WINDOW)),
            rosterDao.observeScopeTotal(scopeKey),
            observationDao.observeForScope(scopeKey),
            shedObservationDao.observeForScope(scopeKey),
        ) { roster, total, observations, shedObservations ->
            WeighingScopeState(
                rosterWindow = roster,
                totalExpected = total,
                individualDrafts = observations.map { it.toDraft() },
                shedDrafts = shedObservations.map { it.toDraft() },
            )
        }.flowOn(Dispatchers.Default)

    override suspend fun replaceRoster(scopeKey: String, rows: List<WeighingRosterRowEntity>) {
        rosterDao.replaceScope(scopeKey, rows)
    }

    override suspend fun listAssignments(cursor: String?, scope: String, parkId: String?): AppResult<WeighingPage<WeighingAssignment>> = withContext(Dispatchers.IO) {
        val client = api ?: return@withContext AppResult.Err("Weighing assignments are not configured.")
        val requestCursor = cursor?.takeIf { it.isNotBlank() }
        runCatching {
            val response = client.listWeighingCampaigns(scope = scope, cursor = requestCursor, limit = WEIGHING_PAGE_SIZE, parkId = parkId)
            val assignments = response.items.flatMap { it.toAssignments(scope) }
            AppResult.Ok(
                WeighingPage(
                    items = assignments,
                    nextCursor = response.nextCursor.nextWeighingCursorAfter(requestCursor),
                    capabilities = WeighingCapabilities(
                        canPublish = response.capabilities.canPublish,
                        canEnd = response.capabilities.canEnd,
                        canReopen = response.capabilities.canReopen,
                    ),
                    operatorSummaries = response.operatorSummaries.map { it.toOperatorSummary() },
                ),
            )
        }.getOrElse { AppResult.Err(it.userFacingMessage("Could not load weighing assignments.")) }
    }

    // --- Leadership reads: Room is the SSOT, the network only writes into it -----------

    override fun observeTaskList(scope: String, parkId: String?, windowSize: Int): Flow<WeighingTaskListCache> {
        val rows = taskDao ?: return kotlinx.coroutines.flow.flowOf(WeighingTaskListCache())
        val keys = taskKeyDao ?: return kotlinx.coroutines.flow.flowOf(WeighingTaskListCache())
        val key = taskListQueryKey(scope, parkId)
        return combine(
            rows.observeWindow(key, windowSize.coerceIn(1, WEIGHING_LEADERSHIP_MAX_WINDOW)),
            keys.observe(key),
        ) { cached, remoteKey ->
            WeighingTaskListCache(
                items = cached.map { cacheJson.decodeFromString<WeighingCampaignDto>(it.dtoJson).toTask() },
                activeCount = remoteKey?.activeCount ?: 0,
                completedCount = remoteKey?.completedCount ?: 0,
                capabilities = WeighingCapabilities(
                    canPublish = remoteKey?.canPublish ?: false,
                    canEnd = remoteKey?.canEnd ?: false,
                    canReopen = remoteKey?.canReopen ?: false,
                ),
                canLoadMore = remoteKey?.endReached == false && !remoteKey.nextCursor.isNullOrBlank(),
                hasCache = remoteKey != null,
                cachedAt = remoteKey?.updatedAt ?: 0L,
            )
        // Decode off Main: the JSON parse happens here, once, not on the UI thread.
        }.flowOn(Dispatchers.Default)
    }

    override suspend fun refreshTaskList(scope: String, parkId: String?, reset: Boolean): AppResult<Int> =
        withContext(Dispatchers.IO) {
            val client = api ?: return@withContext AppResult.Err("Weighing tasks are not configured.")
            val db = database ?: return@withContext AppResult.Err("Weighing tasks are not configured.")
            val rows = taskDao ?: return@withContext AppResult.Err("Weighing tasks are not configured.")
            val keys = taskKeyDao ?: return@withContext AppResult.Err("Weighing tasks are not configured.")
            val key = taskListQueryKey(scope, parkId)
            val cursor = if (reset) null else keys.get(key)?.takeIf { !it.endReached }?.nextCursor?.takeIf { it.isNotBlank() }
                ?: return@withContext AppResult.Ok(0)
            runCatching {
                val response = client.listWeighingCampaigns(
                    scope = scope,
                    cursor = cursor,
                    limit = WEIGHING_LEADERSHIP_PAGE_SIZE,
                    parkId = parkId?.takeIf { it.isNotBlank() },
                )
                val nextCursor = response.nextCursor.nextWeighingCursorAfter(cursor)
                val now = clock()
                // Rows and their cursor commit TOGETHER: a crash between them would leave the cursor
                // pointing past rows that were never stored, silently losing a page.
                db.withTransaction {
                    val startIndex = if (reset) {
                        rows.deleteQuery(key)
                        0
                    } else {
                        rows.nextSortIndex(key)
                    }
                    rows.upsertAll(
                        response.items.mapIndexed { index, item ->
                            WeighingTaskRowEntity(
                                queryKey = key,
                                campaignId = item.campaignId,
                                sortIndex = startIndex + index,
                                dtoJson = cacheJson.encodeToString(item),
                                updatedAt = now,
                            )
                        },
                    )
                    keys.upsert(
                        WeighingTaskRemoteKeyEntity(
                            queryKey = key,
                            nextCursor = nextCursor,
                            endReached = nextCursor.isNullOrBlank(),
                            activeCount = response.counts.active,
                            completedCount = response.counts.completed,
                            canPublish = response.capabilities.canPublish,
                            canEnd = response.capabilities.canEnd,
                            canReopen = response.capabilities.canReopen,
                            updatedAt = now,
                        ),
                    )
                    if (reset) {
                        rows.pruneOutsideNewestQueries(WEIGHING_CACHED_TASK_FILTERS)
                        keys.pruneOutsideNewestQueries(WEIGHING_CACHED_TASK_FILTERS)
                    }
                }
                AppResult.Ok(response.items.size)
            // Room keeps whatever it already had: a failed page leaves the cached list on screen.
            }.getOrElse { AppResult.Err(it.userFacingMessage("Could not load weighing tasks.")) }
        }

    override suspend fun getTask(campaignId: String): AppResult<WeighingTaskLookup> =
        withContext(Dispatchers.IO) {
            val client = api ?: return@withContext AppResult.Err("This task is not configured.")
            val id = campaignId.takeIf { it.isNotBlank() }
                ?: return@withContext AppResult.Ok(WeighingTaskLookup.NotFound)
            runCatching {
                val response = client.getWeighingCampaign(id)
                AppResult.Ok<WeighingTaskLookup>(
                    WeighingTaskLookup.Found(
                        task = response.campaign.toTask(),
                        capabilities = WeighingCapabilities(
                            canPublish = response.capabilities.canPublish,
                            canEnd = response.capabilities.canEnd,
                            canReopen = response.capabilities.canReopen,
                        ),
                    ),
                )
            }.getOrElse { failure ->
                // 404 is an ANSWER, not a failure: the backend refuses "not yours" and "not there"
                // identically so that a refusal cannot be used to probe for tasks. A read error
                // (offline, 5xx) is a different thing and must stay retryable.
                if (failure.appApiStatusCode() == 404) {
                    AppResult.Ok(WeighingTaskLookup.NotFound)
                } else {
                    AppResult.Err(failure.message ?: "Could not open this task.")
                }
            }
        }

    override suspend fun exportCampaignCsv(campaignId: String): AppResult<WeighingCsvExport> =
        withContext(Dispatchers.IO) {
            val client = api ?: return@withContext AppResult.Err("This task's export is not configured.")
            val id = campaignId.takeIf { it.isNotBlank() }
                ?: return@withContext AppResult.Err("This task's export is not configured.")
            runCatching {
                // [AppApi.exportWeighingCampaignCsv] already reads and closes the underlying
                // OkHttp body inside core-network; this module only ever sees the plain bytes, so
                // it stays free of any HTTP-client type. A task's CSV covers one park's sheds for
                // one weigh date -- tens of KB at most -- so holding it whole is bounded, unlike
                // the roster/bucket reads elsewhere in this file, which stay keyset-paged because
                // they can run to thousands of rows.
                val bytes = client.exportWeighingCampaignCsv(id)
                AppResult.Ok(
                    WeighingCsvExport(
                        bytes = bytes,
                        suggestedFileName = "weighing-export-$id.csv",
                    ),
                )
            }.getOrElse { failure ->
                AppResult.Err(failure.userFacingMessage("Could not export this task."))
            }
        }

    override suspend fun listParks(): AppResult<List<WeighingParkRef>> =
        withContext(Dispatchers.IO) {
            val client = api ?: return@withContext AppResult.Err("Weighing parks are not configured.")
            runCatching {
                AppResult.Ok(
                    client.listWeighingParks().parks
                        .filter { it.parkId.isNotBlank() }
                        .map { WeighingParkRef(parkId = it.parkId, name = it.name.ifBlank { it.parkId }) },
                )
            }.getOrElse { AppResult.Err(it.message ?: "Could not load weighing parks.") }
        }

    override fun observeTaskBuckets(campaignId: String, windowSize: Int): Flow<WeighingTaskBucketCache> {
        val rows = bucketDao ?: return kotlinx.coroutines.flow.flowOf(WeighingTaskBucketCache())
        val keys = bucketKeyDao ?: return kotlinx.coroutines.flow.flowOf(WeighingTaskBucketCache())
        return combine(
            rows.observeWindow(campaignId, windowSize.coerceIn(1, WEIGHING_LEADERSHIP_MAX_WINDOW)),
            keys.observe(campaignId),
        ) { cached, remoteKey ->
            // Canceled buckets are dropped here for the same reason `toTask()` drops them from
            // the card: an edit that removes a shed leaves the row behind as status='canceled'
            // rather than deleting it. Rendering those made the task DETAIL header read
            // "4 shed buckets" while the card read 3 and the detail's own action line read
            // "3 not submitted" -- three counts of one task on one screen. totalCount is
            // corrected by the same removal so the header cannot disagree with the list.
            val visible = cached
                .map { cacheJson.decodeFromString<WeighingCampaignShedDto>(it.dtoJson) }
                .filter { it.status.lowercase() !in setOf("canceled", "cancelled") }
            val dropped = cached.size - visible.size
            WeighingTaskBucketCache(
                items = visible.map { it.toTaskShed() },
                totalCount = ((remoteKey?.totalCount ?: 0) - dropped).coerceAtLeast(visible.size),
                canLoadMore = remoteKey?.endReached == false && !remoteKey.nextCursor.isNullOrBlank(),
                hasCache = remoteKey != null,
                cachedAt = remoteKey?.updatedAt ?: 0L,
            )
        }.flowOn(Dispatchers.Default)
    }

    override suspend fun refreshTaskBuckets(campaignId: String, reset: Boolean): AppResult<Int> =
        withContext(Dispatchers.IO) {
            val client = api ?: return@withContext AppResult.Err("This task is not configured.")
            val db = database ?: return@withContext AppResult.Err("This task is not configured.")
            val rows = bucketDao ?: return@withContext AppResult.Err("This task is not configured.")
            val keys = bucketKeyDao ?: return@withContext AppResult.Err("This task is not configured.")
            val cursor = if (reset) null else keys.get(campaignId)?.takeIf { !it.endReached }?.nextCursor?.takeIf { it.isNotBlank() }
                ?: return@withContext AppResult.Ok(0)
            runCatching {
                val response = client.listWeighingCampaignSheds(
                    campaignId = campaignId,
                    cursor = cursor,
                    limit = WEIGHING_LEADERSHIP_PAGE_SIZE,
                )
                val nextCursor = response.nextCursor.nextWeighingCursorAfter(cursor)
                val now = clock()
                db.withTransaction {
                    val startIndex = if (reset) {
                        rows.deleteQuery(campaignId)
                        0
                    } else {
                        rows.nextSortIndex(campaignId)
                    }
                    rows.upsertAll(
                        response.items.mapIndexed { index, item ->
                            WeighingTaskBucketRowEntity(
                                campaignId = campaignId,
                                campaignShedId = item.campaignShedId,
                                sortIndex = startIndex + index,
                                dtoJson = cacheJson.encodeToString(item),
                                updatedAt = now,
                            )
                        },
                    )
                    keys.upsert(
                        WeighingTaskBucketRemoteKeyEntity(
                            campaignId = campaignId,
                            nextCursor = nextCursor,
                            endReached = nextCursor.isNullOrBlank(),
                            totalCount = response.totalCount,
                            updatedAt = now,
                        ),
                    )
                    if (reset) {
                        rows.pruneOutsideNewestQueries(WEIGHING_CACHED_TASKS)
                        keys.pruneOutsideNewestQueries(WEIGHING_CACHED_TASKS)
                    }
                }
                AppResult.Ok(response.items.size)
            }.getOrElse { AppResult.Err(it.userFacingMessage("Could not load this task.")) }
        }

    override suspend fun appendTaskList(scope: String, parkId: String?): AppResult<Int> =
        withContext(Dispatchers.IO) {
            val client = api ?: return@withContext AppResult.Err("Weighing tasks are not configured.")
            val db = database ?: return@withContext AppResult.Err("Weighing tasks are not configured.")
            val rows = taskDao ?: return@withContext AppResult.Err("Weighing tasks are not configured.")
            val keys = taskKeyDao ?: return@withContext AppResult.Err("Weighing tasks are not configured.")
            val key = taskListQueryKey(scope, parkId)
            val cursor = keys.get(key)?.nextCursor?.takeIf { it.isNotBlank() }
                ?: return@withContext AppResult.Ok(0)
            runCatching {
                val response = client.listWeighingCampaigns(
                    scope = scope,
                    cursor = cursor,
                    limit = WEIGHING_LEADERSHIP_PAGE_SIZE,
                    parkId = parkId?.takeIf { it.isNotBlank() },
                )
                val nextCursor = response.nextCursor.nextWeighingCursorAfter(cursor)
                val now = clock()
                db.withTransaction {
                    val startIndex = rows.nextSortIndex(key)
                    rows.upsertAll(
                        response.items.mapIndexed { index, item ->
                            WeighingTaskRowEntity(
                                queryKey = key,
                                campaignId = item.campaignId,
                                sortIndex = startIndex + index,
                                dtoJson = cacheJson.encodeToString(item),
                                updatedAt = now,
                            )
                        },
                    )
                    keys.upsert(
                        WeighingTaskRemoteKeyEntity(
                            queryKey = key,
                            nextCursor = nextCursor,
                            endReached = nextCursor.isNullOrBlank(),
                            activeCount = response.counts.active,
                            completedCount = response.counts.completed,
                            canPublish = response.capabilities.canPublish,
                            canEnd = response.capabilities.canEnd,
                            canReopen = response.capabilities.canReopen,
                            updatedAt = now,
                        ),
                    )
                    // Prune overflow without clearing the query since we're appending
                    rows.pruneOutsideNewestQueries(WEIGHING_CACHED_TASK_FILTERS)
                    keys.pruneOutsideNewestQueries(WEIGHING_CACHED_TASK_FILTERS)
                }
                AppResult.Ok(response.items.size)
            }.getOrElse { AppResult.Err(it.userFacingMessage("Could not fetch the next weighing tasks.")) }
        }

    override suspend fun appendTaskBuckets(campaignId: String): AppResult<Int> =
        withContext(Dispatchers.IO) {
            val client = api ?: return@withContext AppResult.Err("This task is not configured.")
            val db = database ?: return@withContext AppResult.Err("This task is not configured.")
            val rows = bucketDao ?: return@withContext AppResult.Err("This task is not configured.")
            val keys = bucketKeyDao ?: return@withContext AppResult.Err("This task is not configured.")
            val cursor = keys.get(campaignId)?.nextCursor?.takeIf { it.isNotBlank() }
                ?: return@withContext AppResult.Ok(0)
            runCatching {
                val response = client.listWeighingCampaignSheds(
                    campaignId = campaignId,
                    cursor = cursor,
                    limit = WEIGHING_LEADERSHIP_PAGE_SIZE,
                )
                val nextCursor = response.nextCursor.nextWeighingCursorAfter(cursor)
                val now = clock()
                db.withTransaction {
                    val startIndex = rows.nextSortIndex(campaignId)
                    rows.upsertAll(
                        response.items.mapIndexed { index, item ->
                            WeighingTaskBucketRowEntity(
                                campaignId = campaignId,
                                campaignShedId = item.campaignShedId,
                                sortIndex = startIndex + index,
                                dtoJson = cacheJson.encodeToString(item),
                                updatedAt = now,
                            )
                        },
                    )
                    keys.upsert(
                        WeighingTaskBucketRemoteKeyEntity(
                            campaignId = campaignId,
                            nextCursor = nextCursor,
                            endReached = nextCursor.isNullOrBlank(),
                            totalCount = response.totalCount,
                            updatedAt = now,
                        ),
                    )
                    // Prune overflow without clearing the query since we're appending
                    rows.pruneOutsideNewestQueries(WEIGHING_CACHED_TASKS)
                    keys.pruneOutsideNewestQueries(WEIGHING_CACHED_TASKS)
                }
                AppResult.Ok(response.items.size)
            }.getOrElse { AppResult.Err(it.userFacingMessage("Could not fetch the next records for this task.")) }
        }

    override fun observeLeadershipShed(
        campaignId: String,
        campaignShedId: String,
        windowSize: Int,
    ): Flow<WeighingLeadershipShedCache> {
        val sheds = leadershipShedDao ?: return kotlinx.coroutines.flow.flowOf(WeighingLeadershipShedCache())
        val records = leadershipRecordDao ?: return kotlinx.coroutines.flow.flowOf(WeighingLeadershipShedCache())
        val keys = leadershipRecordKeyDao ?: return kotlinx.coroutines.flow.flowOf(WeighingLeadershipShedCache())
        val shedKey = weighingShedKey(campaignId, campaignShedId)
        return combine(
            sheds.observe(shedKey),
            records.observeWindow(shedKey, windowSize.coerceIn(1, WEIGHING_LEADERSHIP_MAX_WINDOW)),
            keys.observe(shedKey),
        ) { shed, cachedRecords, remoteKey ->
            WeighingLeadershipShedCache(
                shed = shed?.toLeadershipShed(cachedRecords, cacheJson),
                canLoadMoreRecords = remoteKey?.endReached == false && !remoteKey.nextCursor.isNullOrBlank(),
                cachedAt = shed?.updatedAt ?: 0L,
            )
        }.flowOn(Dispatchers.Default)
    }

    override suspend fun refreshLeadershipShed(
        campaignId: String,
        campaignShedId: String,
        reset: Boolean,
    ): AppResult<Int> = withContext(Dispatchers.IO) {
        val client = api ?: return@withContext AppResult.Err("Weighing is not configured.")
        val db = database ?: return@withContext AppResult.Err("Weighing is not configured.")
        val keys = leadershipRecordKeyDao ?: return@withContext AppResult.Err("Weighing is not configured.")
        val shedKey = weighingShedKey(campaignId, campaignShedId)
        val cursor = if (reset) null else keys.get(shedKey)?.takeIf { !it.endReached }?.nextCursor?.takeIf { it.isNotBlank() }
            ?: return@withContext AppResult.Ok(0)
        runCatching {
            val response = client.getWeighingLeadershipShedVideos(
                campaignId = campaignId,
                campaignShedId = campaignShedId,
                cursor = cursor,
                limit = WEIGHING_LEADERSHIP_PAGE_SIZE,
            ).shed
            db.withTransaction {
                writeLeadershipShedPage(
                    shedKey = shedKey,
                    dto = response,
                    clearRecords = reset,
                    periodLabel = response.periodLabel,
                    galleryQueryKey = null,
                    gallerySortIndex = null,
                )
                if (reset) {
                    leadershipShedDao?.pruneOutsideNewest(WEIGHING_CACHED_SHEDS)
                    leadershipRecordDao?.pruneOutsideNewestQueries(WEIGHING_CACHED_SHEDS)
                    keys.pruneOutsideNewestQueries(WEIGHING_CACHED_SHEDS)
                    leadershipRecordDao?.pruneOrphans()
                    keys.pruneOrphans()
                }
            }
            AppResult.Ok(response.individual.size)
        }.getOrElse { AppResult.Err(it.userFacingMessage("Could not load this shed.")) }
    }

    override suspend fun appendLeadershipShed(
        campaignId: String,
        campaignShedId: String,
    ): AppResult<Int> = withContext(Dispatchers.IO) {
        val client = api ?: return@withContext AppResult.Err("Weighing is not configured.")
        val db = database ?: return@withContext AppResult.Err("Weighing is not configured.")
        val keys = leadershipRecordKeyDao ?: return@withContext AppResult.Err("Weighing is not configured.")
        val shedKey = weighingShedKey(campaignId, campaignShedId)
        val cursor = keys.get(shedKey)?.nextCursor?.takeIf { it.isNotBlank() }
            ?: return@withContext AppResult.Ok(0)
        runCatching {
            val response = client.getWeighingLeadershipShedVideos(
                campaignId = campaignId,
                campaignShedId = campaignShedId,
                cursor = cursor,
                limit = WEIGHING_LEADERSHIP_PAGE_SIZE,
            ).shed
            db.withTransaction {
                writeLeadershipShedPage(
                    shedKey = shedKey,
                    dto = response,
                    clearRecords = false,
                    periodLabel = response.periodLabel,
                    galleryQueryKey = null,
                    gallerySortIndex = null,
                )
                leadershipShedDao?.pruneOutsideNewest(WEIGHING_CACHED_SHEDS)
                leadershipRecordDao?.pruneOutsideNewestQueries(WEIGHING_CACHED_SHEDS)
                keys.pruneOutsideNewestQueries(WEIGHING_CACHED_SHEDS)
                leadershipRecordDao?.pruneOrphans()
                keys.pruneOrphans()
            }
            AppResult.Ok(response.individual.size)
        }.getOrElse { AppResult.Err(it.userFacingMessage("Could not fetch the next records for this shed.")) }
    }

    @OptIn(ExperimentalCoroutinesApi::class)
    override fun observeLeadershipVideos(windowSize: Int): Flow<List<WeighingLeadershipShed>> {
        val sheds = leadershipShedDao ?: return kotlinx.coroutines.flow.flowOf(emptyList())
        val records = leadershipRecordDao ?: return kotlinx.coroutines.flow.flowOf(emptyList())
        val bounded = windowSize.coerceIn(1, WEIGHING_LEADERSHIP_MAX_WINDOW)
        return sheds.observeGalleryWindow(WEIGHING_VIDEOS_QUERY_KEY, bounded)
            .flatMapLatest { rows ->
                val shedKeys = rows.map { it.shedKey }
                if (shedKeys.isEmpty()) {
                    kotlinx.coroutines.flow.flowOf(emptyList())
                } else {
                    // The gallery renders an individual bucket's captured animals, so it needs the
                    // SAME cached records the shed detail reads -- bounded PER SHED to one page,
                    // never one unbounded read across the whole gallery page.
                    records.observeWindowForSheds(shedKeys, WEIGHING_LEADERSHIP_PAGE_SIZE)
                        .map { cached ->
                            val byShed = cached.groupBy { it.shedKey }
                            rows.map { it.toLeadershipShed(byShed[it.shedKey].orEmpty(), cacheJson) }
                        }
                }
            }.flowOn(Dispatchers.Default)
    }

    override suspend fun refreshLeadershipVideos(reset: Boolean): AppResult<Int> = withContext(Dispatchers.IO) {
        val client = api ?: return@withContext AppResult.Err("Weighing videos are not configured.")
        val db = database ?: return@withContext AppResult.Err("Weighing videos are not configured.")
        val sheds = leadershipShedDao ?: return@withContext AppResult.Err("Weighing videos are not configured.")
        val keys = galleryKeyDao ?: return@withContext AppResult.Err("Weighing videos are not configured.")
        val cursor = if (reset) null else keys.get(WEIGHING_VIDEOS_QUERY_KEY)?.takeIf { !it.endReached }
            ?.nextCursor?.takeIf { it.isNotBlank() }
            ?: return@withContext AppResult.Ok(0)
        runCatching {
            // ONE request for the whole page. This used to fetch a page of TASKS, expand every
            // embedded bucket, and then call the single-bucket read once per bucket -- about 1,500
            // sequential round trips on a 76-shed park, on every RefreshOnResume. A per-call limit
            // bounds each response, not the number of calls; only a page at BUCKET grain does.
            val response = client.listWeighingLeadershipSheds(
                cursor = cursor,
                limit = WEIGHING_LEADERSHIP_PAGE_SIZE,
            )
            val nextCursor = response.nextCursor.nextWeighingCursorAfter(cursor)
            val now = clock()
            db.withTransaction {
                val startIndex = if (reset) {
                    // Clears MEMBERSHIP only: a bucket also opened on its own detail screen keeps
                    // its cached context and its records.
                    sheds.clearGallery(WEIGHING_VIDEOS_QUERY_KEY)
                    0
                } else {
                    sheds.nextGallerySortIndex(WEIGHING_VIDEOS_QUERY_KEY)
                }
                response.items.forEachIndexed { index, dto ->
                    writeLeadershipShedPage(
                        shedKey = weighingShedKey(dto.campaignId, dto.campaignShedId),
                        dto = dto,
                        clearRecords = false,
                        periodLabel = dto.periodLabel,
                        galleryQueryKey = WEIGHING_VIDEOS_QUERY_KEY,
                        gallerySortIndex = startIndex + index,
                        keepDeeperRecords = true,
                    )
                }
                keys.upsert(
                    WeighingLeadershipGalleryRemoteKeyEntity(
                        queryKey = WEIGHING_VIDEOS_QUERY_KEY,
                        nextCursor = nextCursor,
                        endReached = nextCursor.isNullOrBlank(),
                        updatedAt = now,
                    ),
                )
                if (reset) {
                    // All three tables are bounded together. Pruning only the bucket rows left the
                    // records of every evicted bucket behind forever -- the reads were bounded, the
                    // writes were not.
                    sheds.pruneOutsideNewest(WEIGHING_CACHED_SHEDS)
                    leadershipRecordDao?.pruneOrphans()
                    leadershipRecordKeyDao?.pruneOrphans()
                }
            }
            AppResult.Ok(response.items.size)
        }.getOrElse { AppResult.Err(it.userFacingMessage("Could not load weighing videos.")) }
    }

    override suspend fun appendLeadershipVideos(): AppResult<Int> = withContext(Dispatchers.IO) {
        val client = api ?: return@withContext AppResult.Err("Weighing videos are not configured.")
        val db = database ?: return@withContext AppResult.Err("Weighing videos are not configured.")
        val sheds = leadershipShedDao ?: return@withContext AppResult.Err("Weighing videos are not configured.")
        val keys = galleryKeyDao ?: return@withContext AppResult.Err("Weighing videos are not configured.")
        val cursor = keys.get(WEIGHING_VIDEOS_QUERY_KEY)?.nextCursor?.takeIf { it.isNotBlank() }
            ?: return@withContext AppResult.Ok(0)
        runCatching {
            val response = client.listWeighingLeadershipSheds(
                cursor = cursor,
                limit = WEIGHING_LEADERSHIP_PAGE_SIZE,
            )
            val nextCursor = response.nextCursor.nextWeighingCursorAfter(cursor)
            val now = clock()
            db.withTransaction {
                val startIndex = sheds.nextGallerySortIndex(WEIGHING_VIDEOS_QUERY_KEY)
                response.items.forEachIndexed { index, dto ->
                    writeLeadershipShedPage(
                        shedKey = weighingShedKey(dto.campaignId, dto.campaignShedId),
                        dto = dto,
                        clearRecords = false,
                        periodLabel = dto.periodLabel,
                        galleryQueryKey = WEIGHING_VIDEOS_QUERY_KEY,
                        gallerySortIndex = startIndex + index,
                        keepDeeperRecords = true,
                    )
                }
                keys.upsert(
                    WeighingLeadershipGalleryRemoteKeyEntity(
                        queryKey = WEIGHING_VIDEOS_QUERY_KEY,
                        nextCursor = nextCursor,
                        endReached = nextCursor.isNullOrBlank(),
                        updatedAt = now,
                    ),
                )
                // Prune overflow without clearing the query since we're appending, not resetting
                sheds.pruneOutsideNewest(WEIGHING_CACHED_SHEDS)
                leadershipRecordDao?.pruneOrphans()
                leadershipRecordKeyDao?.pruneOrphans()
            }
            AppResult.Ok(response.items.size)
        }.getOrElse { AppResult.Err(it.userFacingMessage("Could not fetch the next weighing videos.")) }
    }

    override fun observePlannerCatalog(periodStartDate: String): Flow<WeighingPlannerCatalogCache> {
        val catalog = plannerDao ?: return kotlinx.coroutines.flow.flowOf(WeighingPlannerCatalogCache())
        return combine(
            // Bounded by the PARK cap, not by a page size: the step must offer every park, and
            // parks are few. There is no park cursor to advance.
            catalog.observeParks(periodStartDate, WEIGHING_MAX_PLANNER_PARKS),
            catalog.observeOperators(periodStartDate, WEIGHING_LEADERSHIP_MAX_WINDOW),
        ) { parkRows, operatorRows ->
            WeighingPlannerCatalogCache(
                catalog = WeighingPlannerCatalog(
                    parks = parkRows.map { it.toPlannerPark(cacheJson) },
                    operators = operatorRows.map {
                        val dto = cacheJson.decodeFromString<WeighingPlannerOperatorDto>(it.dtoJson)
                        WeighingPlannerOperator(dto.userId, dto.displayName, dto.displayCode, dto.parkIds)
                    },
                ),
                hasCache = parkRows.isNotEmpty(),
                cachedAt = parkRows.maxOfOrNull { it.updatedAt } ?: 0L,
            )
        }.flowOn(Dispatchers.Default)
    }

    override suspend fun refreshPlannerCatalog(periodStartDate: String): AppResult<Int> =
        withContext(Dispatchers.IO) {
            val client = api ?: return@withContext AppResult.Err("Weighing planner is not configured.")
            val db = database ?: return@withContext AppResult.Err("Weighing planner is not configured.")
            val catalog = plannerDao ?: return@withContext AppResult.Err("Weighing planner is not configured.")
            runCatching {
                val response = client.getWeighingPlannerCatalog(periodStartDate = periodStartDate)
                val now = clock()
                db.withTransaction {
                    // The whole park list arrives in ONE response, so it replaces the date's cached
                    // parks wholesale. Nothing to merge and no cursor to carry.
                    catalog.deleteParks(periodStartDate)
                    catalog.deleteOperators(periodStartDate)
                    catalog.upsertParks(
                        response.parks.mapIndexed { index, park ->
                            WeighingPlannerParkRowEntity(
                                queryKey = periodStartDate,
                                parkId = park.parkId,
                                sortIndex = index,
                                name = park.name,
                                kidCount = park.kidCount,
                                // The backend's park-grain total, stored as sent. Never recomputed
                                // from cached shed rows, which only ever hold a page of a park.
                                shedCount = park.shedCount,
                                existingCampaignJson = park.existingCampaign?.let { cacheJson.encodeToString(it) },
                                updatedAt = now,
                            )
                        },
                    )
                    catalog.upsertOperators(
                        response.operators.mapIndexed { index, operator ->
                            WeighingPlannerOperatorRowEntity(
                                queryKey = periodStartDate,
                                userId = operator.userId,
                                sortIndex = index,
                                dtoJson = cacheJson.encodeToString(operator),
                                updatedAt = now,
                            )
                        },
                    )
                    catalog.pruneParksOutsideNewestQueries(WEIGHING_CACHED_CATALOG_DATES)
                    catalog.pruneOperatorsOutsideNewestQueries(WEIGHING_CACHED_CATALOG_DATES)
                }
                AppResult.Ok(response.parks.size)
            }.getOrElse { AppResult.Err(it.userFacingMessage("Could not load weighing planner.")) }
        }

    override fun observePlannerParkBuckets(
        periodStartDate: String,
        parkId: String,
        windowSize: Int,
        excludeCampaignId: String?,
    ): Flow<WeighingPlannerParkBucketsCache> {
        val catalog = plannerDao ?: return kotlinx.coroutines.flow.flowOf(WeighingPlannerParkBucketsCache())
        val keys = plannerKeyDao ?: return kotlinx.coroutines.flow.flowOf(WeighingPlannerParkBucketsCache())
        val queryKey = plannerBucketQueryKey(periodStartDate, parkId, excludeCampaignId)
        val bounded = windowSize.coerceIn(1, WEIGHING_LEADERSHIP_MAX_WINDOW)
        return combine(
            catalog.observeShedWindow(queryKey, bounded),
            keys.observe(queryKey),
        ) { shedRows, remoteKey ->
            WeighingPlannerParkBucketsCache(
                parkId = parkId,
                sheds = shedRows.map { cacheJson.decodeFromString<WeighingPlannerShedDto>(it.shedJson).toPlannerShed() },
                canLoadMore = remoteKey?.endReached == false && !remoteKey.nextCursor.isNullOrBlank(),
                hasCache = remoteKey != null,
                cachedAt = remoteKey?.updatedAt ?: 0L,
            )
        }.flowOn(Dispatchers.Default)
    }

    override suspend fun refreshPlannerParkBucketAvailability(
        periodStartDate: String,
        parkId: String,
        pages: Int,
        excludeCampaignId: String?,
    ): AppResult<Int> = withContext(Dispatchers.IO) {
        val client = api ?: return@withContext AppResult.Err("Weighing planner is not configured.")
        val db = database ?: return@withContext AppResult.Err("Weighing planner is not configured.")
        val catalog = plannerDao ?: return@withContext AppResult.Err("Weighing planner is not configured.")
        if (parkId.isBlank()) return@withContext AppResult.Ok(0)
        val queryKey = plannerBucketQueryKey(periodStartDate, parkId, excludeCampaignId)
        // Walk only as many pages as the wizard is actually showing, never the whole park.
        val pageCount = pages.coerceIn(1, WEIGHING_LEADERSHIP_MAX_WINDOW / WEIGHING_LEADERSHIP_PAGE_SIZE)
        runCatching {
            var cursor: String? = null
            var refreshed = 0
            var sortIndex = 0
            for (page in 0 until pageCount) {
                val response = client.getWeighingPlannerParkBuckets(
                    parkId = parkId,
                    periodStartDate = periodStartDate,
                    cursor = cursor,
                    limit = WEIGHING_LEADERSHIP_PAGE_SIZE,
                    excludeCampaignId = excludeCampaignId,
                )
                val now = clock()
                val startIndex = sortIndex
                db.withTransaction {
                    // UPSERT, never delete: the row key is (queryKey, locationId), so this
                    // rewrites the availability of rows the planner may already have picked
                    // without removing any page from the observed window.
                    catalog.upsertSheds(
                        response.sheds.mapIndexed { offset, shed ->
                            WeighingPlannerShedRowEntity(
                                queryKey = queryKey,
                                locationId = shed.locationId,
                                partitionKey = shed.partitionLabel.normalizedWeighingPartitionKey(),
                                parkId = parkId,
                                parkName = "",
                                sortIndex = startIndex + offset,
                                shedJson = cacheJson.encodeToString(shed),
                                existingCampaignJson = null,
                                updatedAt = now,
                            )
                        },
                    )
                }
                refreshed += response.sheds.size
                sortIndex += response.sheds.size
                cursor = response.nextCursor.nextWeighingCursorAfter(cursor)
                if (cursor.isNullOrBlank()) break
            }
            refreshed
        }.fold(
            onSuccess = { AppResult.Ok(it) },
            onFailure = { AppResult.Err(it.message ?: "Could not refresh shed availability.") },
        )
    }

    override suspend fun refreshPlannerParkBuckets(
        periodStartDate: String,
        parkId: String,
        reset: Boolean,
        excludeCampaignId: String?,
    ): AppResult<Int> = withContext(Dispatchers.IO) {
        val client = api ?: return@withContext AppResult.Err("Weighing planner is not configured.")
        val db = database ?: return@withContext AppResult.Err("Weighing planner is not configured.")
        val catalog = plannerDao ?: return@withContext AppResult.Err("Weighing planner is not configured.")
        val keys = plannerKeyDao ?: return@withContext AppResult.Err("Weighing planner is not configured.")
        if (parkId.isBlank()) return@withContext AppResult.Ok(0)
        val queryKey = plannerBucketQueryKey(periodStartDate, parkId, excludeCampaignId)
        val cursor = if (reset) {
            null
        } else {
            keys.get(queryKey)?.takeIf { !it.endReached }?.nextCursor?.takeIf { it.isNotBlank() }
                ?: return@withContext AppResult.Ok(0)
        }
        runCatching {
            val response = client.getWeighingPlannerParkBuckets(
                parkId = parkId,
                periodStartDate = periodStartDate,
                cursor = cursor,
                limit = WEIGHING_LEADERSHIP_PAGE_SIZE,
                excludeCampaignId = excludeCampaignId,
            )
            val nextCursor = response.nextCursor.nextWeighingCursorAfter(cursor)
            val now = clock()
            db.withTransaction {
                val startIndex = if (reset) {
                    catalog.deleteSheds(queryKey)
                    0
                } else {
                    catalog.nextShedSortIndex(queryKey)
                }
                catalog.upsertSheds(
                    response.sheds.mapIndexed { offset, shed ->
                        WeighingPlannerShedRowEntity(
                            queryKey = queryKey,
                            locationId = shed.locationId,
                            partitionKey = shed.partitionLabel.normalizedWeighingPartitionKey(),
                            parkId = parkId,
                            parkName = "",
                            sortIndex = startIndex + offset,
                            shedJson = cacheJson.encodeToString(shed),
                            existingCampaignJson = null,
                            updatedAt = now,
                        )
                    },
                )
                keys.upsert(
                    WeighingPlannerRemoteKeyEntity(
                        queryKey = queryKey,
                        nextCursor = nextCursor,
                        endReached = nextCursor.isNullOrBlank(),
                        updatedAt = now,
                    ),
                )
                if (reset) {
                    // Keyed per PARK now, so a handful of park streams stay cached rather than a
                    // handful of dates. Switching park in the wizard must not evict the date.
                    catalog.pruneShedsOutsideNewestQueries(WEIGHING_CACHED_BUCKET_PARKS)
                    keys.pruneOutsideNewestQueries(WEIGHING_CACHED_BUCKET_PARKS)
                }
            }
            AppResult.Ok(response.sheds.size)
        }.getOrElse { AppResult.Err(it.userFacingMessage("Could not load weighing planner.")) }
    }

    override suspend fun appendPlannerParkBuckets(
        periodStartDate: String,
        parkId: String,
        excludeCampaignId: String?,
    ): AppResult<Int> = withContext(Dispatchers.IO) {
        val client = api ?: return@withContext AppResult.Err("Weighing planner is not configured.")
        val db = database ?: return@withContext AppResult.Err("Weighing planner is not configured.")
        val catalog = plannerDao ?: return@withContext AppResult.Err("Weighing planner is not configured.")
        val keys = plannerKeyDao ?: return@withContext AppResult.Err("Weighing planner is not configured.")
        if (parkId.isBlank()) return@withContext AppResult.Ok(0)
        val queryKey = plannerBucketQueryKey(periodStartDate, parkId, excludeCampaignId)
        val cursor = keys.get(queryKey)?.nextCursor?.takeIf { it.isNotBlank() }
            ?: return@withContext AppResult.Ok(0)
        runCatching {
            val response = client.getWeighingPlannerParkBuckets(
                parkId = parkId,
                periodStartDate = periodStartDate,
                cursor = cursor,
                limit = WEIGHING_LEADERSHIP_PAGE_SIZE,
                excludeCampaignId = excludeCampaignId,
            )
            val nextCursor = response.nextCursor.nextWeighingCursorAfter(cursor)
            val now = clock()
            db.withTransaction {
                val startIndex = catalog.nextShedSortIndex(queryKey)
                catalog.upsertSheds(
                    response.sheds.mapIndexed { offset, shed ->
                        WeighingPlannerShedRowEntity(
                            queryKey = queryKey,
                            locationId = shed.locationId,
                            partitionKey = shed.partitionLabel.normalizedWeighingPartitionKey(),
                            parkId = parkId,
                            parkName = "",
                            sortIndex = startIndex + offset,
                            shedJson = cacheJson.encodeToString(shed),
                            existingCampaignJson = null,
                            updatedAt = now,
                        )
                    },
                )
                keys.upsert(
                    WeighingPlannerRemoteKeyEntity(
                        queryKey = queryKey,
                        nextCursor = nextCursor,
                        endReached = nextCursor.isNullOrBlank(),
                        updatedAt = now,
                    ),
                )
                // Prune overflow without clearing the query since we're appending
                catalog.pruneShedsOutsideNewestQueries(WEIGHING_CACHED_BUCKET_PARKS)
                keys.pruneOutsideNewestQueries(WEIGHING_CACHED_BUCKET_PARKS)
            }
            AppResult.Ok(response.sheds.size)
        }.getOrElse { AppResult.Err(it.userFacingMessage("Could not fetch the next planner buckets.")) }
    }

    /**
     * Writes ONE shed read into Room: the bucket's own context, plus this page of its records.
     *
     * Called inside the caller's transaction so the shed row and its records land together.
     */
    private suspend fun writeLeadershipShedPage(
        shedKey: String,
        dto: WeighingLeadershipShedVideosDto,
        clearRecords: Boolean,
        periodLabel: String,
        galleryQueryKey: String?,
        gallerySortIndex: Int?,
        // The GALLERY carries page 1 of every bucket it lists. A bucket the reader has already
        // paged deep on its own detail screen must not be truncated back to that first page and
        // have its cursor rewound: both surfaces share one record set. So the gallery writes
        // records only into a bucket that has none cached, and never downgrades a cursor.
        keepDeeperRecords: Boolean = false,
    ) {
        val sheds = leadershipShedDao ?: return
        val records = leadershipRecordDao ?: return
        val keys = leadershipRecordKeyDao ?: return
        val now = clock()
        val existing = sheds.get(shedKey)
        sheds.upsert(
            WeighingLeadershipShedEntity(
                shedKey = shedKey,
                campaignId = dto.campaignId,
                campaignShedId = dto.campaignShedId,
                shedName = dto.shedName,
                parkName = dto.parkName,
                weighDate = dto.weighDate,
                operatorUserId = dto.operatorUserId,
                operatorDisplayName = dto.operatorDisplayName,
                category = dto.weighingCategory,
                status = dto.status,
                // The shed read carries no weigh PERIOD, so a caller that knows it supplies it; an
                // empty label never overwrites one already cached.
                periodLabel = periodLabel.ifBlank { existing?.periodLabel.orEmpty() },
                estimatedAnimalCount = dto.estimatedAnimalCount,
                maxShedVideos = dto.maxShedVideos,
                lumpSumJson = dto.lumpSum?.let { cacheJson.encodeToString(it) },
                galleryQueryKey = galleryQueryKey ?: existing?.galleryQueryKey,
                gallerySortIndex = gallerySortIndex ?: existing?.gallerySortIndex,
                updatedAt = now,
            ),
        )
        val startIndex = if (clearRecords) {
            records.deleteQuery(shedKey)
            0
        } else {
            records.nextSortIndex(shedKey)
        }
        if (keepDeeperRecords && startIndex > 0) return
        records.upsertAll(
            dto.individual.mapIndexed { index, observation ->
                WeighingLeadershipRecordEntity(
                    shedKey = shedKey,
                    observationId = observation.observationId,
                    sortIndex = startIndex + index,
                    dtoJson = cacheJson.encodeToString(observation),
                    updatedAt = now,
                )
            },
        )
        val nextCursor = dto.nextIndividualCursor?.takeIf { it.isNotBlank() }
        keys.upsert(
            WeighingLeadershipRecordRemoteKeyEntity(
                shedKey = shedKey,
                nextCursor = nextCursor,
                endReached = nextCursor.isNullOrBlank(),
                updatedAt = now,
            ),
        )
    }

    override suspend fun createAndPublishPlan(draft: WeighingPlanDraft): AppResult<WeighingAssignment?> = withContext(Dispatchers.IO) {
        val client = api ?: return@withContext AppResult.Err("Weighing planner is not configured.")
        if (draft.sheds.isEmpty()) return@withContext AppResult.Err("Select at least one kid shed.")
        runCatching {
            val createIdem = "weighing:create:${draft.periodStartDate}:${draft.parkId}:" +
                weighingBucketSetDigest(draft.sheds.map { "${it.locationId}:${it.partitionLabel.normalizedWeighingPartitionKey()}" })
            val created = client.createWeighingCampaign(
                idempotencyKey = createIdem,
                request = draft.toCreateRequest(),
            ).campaign
            val publishIdem = "weighing:publish:${created.campaignId}"
            val published = client.publishWeighingCampaign(created.campaignId, publishIdem).campaign
            AppResult.Ok(published.toAssignments(WEIGHING_SCOPE_MINE).firstOrNull())
        }.getOrElse { AppResult.Err(it.userFacingMessage("Could not publish weighing plan.")) }
    }

    override suspend fun createPlan(draft: WeighingPlanDraft, publish: Boolean): AppResult<String> = withContext(Dispatchers.IO) {
        val client = api ?: return@withContext AppResult.Err("Weighing planner is not configured.")
        if (draft.sheds.isEmpty()) return@withContext AppResult.Err("Add at least one shed bucket.")
        runCatching {
            // The key names the WORK, not the attempt: the same date, park and bucket set is the
            // same task, so a retry after a dropped response cannot create a second one.
            val createIdem = "weighing:create:${draft.startBusinessDate}:${draft.parkId}:" +
                weighingBucketSetDigest(draft.sheds.map { "${it.locationId}:${it.partitionLabel.normalizedWeighingPartitionKey()}:${it.category}:${it.operatorUserId}" })
            val created = client.createWeighingCampaign(
                idempotencyKey = createIdem,
                request = draft.toCreateRequest(),
            ).campaign
            if (publish && created.status == "draft") {
                client.publishWeighingCampaign(created.campaignId, "weighing:publish:${created.campaignId}")
            }
            AppResult.Ok(created.campaignId)
        }.getOrElse { AppResult.Err(it.userFacingMessage("Could not save this weighing task.")) }
    }

    override suspend fun updatePlan(campaignId: String, draft: WeighingPlanDraft): AppResult<WeighingAssignment?> = withContext(Dispatchers.IO) {
        val client = api ?: return@withContext AppResult.Err("Weighing planner is not configured.")
        if (campaignId.isBlank()) return@withContext AppResult.Err("Existing weighing task is missing.")
        if (draft.sheds.isEmpty()) return@withContext AppResult.Err("Select at least one kid shed.")
        runCatching {
            // The key names the ATTEMPT, not the shed set. It used to be derived from
            // (date, locationId, category) only, which made two different edits collide:
            // an operator-only change produced a byte-identical key and was answered from
            // the previous edit's snapshot, so the planner's reassignment was silently
            // dropped while the app reported success. Reverting a bucket to a combination
            // this campaign had already been through failed the same way. This is the same
            // failure the close/reopen epoch above exists to prevent, so it uses the same
            // mechanism: the epoch rotates only after the server confirms, so retrying an
            // unknown-outcome attempt still deduplicates while a genuinely new edit applies.
            val updateIdem = transitionIdempotencyKey("update", campaignId)
            val updated = client.updateWeighingCampaign(campaignId, updateIdem, draft.toCreateRequest()).campaign
            val visible = if (updated.status == "draft") {
                val publishIdem = "weighing:publish:$campaignId"
                client.publishWeighingCampaign(campaignId, publishIdem).campaign
            } else {
                updated
            }
            advanceTransitionEpoch("update", campaignId)
            AppResult.Ok(visible.toAssignments(WEIGHING_SCOPE_MINE).firstOrNull())
        }.getOrElse { AppResult.Err(it.userFacingMessage("Could not update weighing plan.")) }
    }

    override suspend fun publishCampaign(campaignId: String): AppResult<Unit> = withContext(Dispatchers.IO) {
        val client = api ?: return@withContext AppResult.Err("Weighing planner is not configured.")
        if (campaignId.isBlank()) return@withContext AppResult.Err("This weighing task is missing.")
        runCatching {
            // The key names the TASK, not the attempt, so a retry after a dropped response cannot
            // publish twice. Same key the authoring flow uses for the same campaign.
            client.publishWeighingCampaign(campaignId, "weighing:publish:$campaignId")
            AppResult.Ok(Unit)
        }.getOrElse { AppResult.Err(it.userFacingMessage("Could not publish this weighing task."), it) }
    }

    override suspend fun refreshScope(
        campaignId: String,
        workGroupId: String,
        campaignShedId: String,
        maxRows: Int,
    ): AppResult<Int> = withContext(Dispatchers.IO) {
        val client = api ?: return@withContext AppResult.Err("Weighing roster sync is not configured.")
        val key = weighingScopeKey(campaignId, workGroupId, campaignShedId)
        runCatching {
            val safetyLimit = maxRows.coerceIn(1, MAX_SCOPE_HYDRATION_ROWS)
            val accepted = linkedMapOf<String, WeighingAcceptedObservationDto>() // mobile-guard:ignore: bounded by safetyLimit within one refreshScope call, then discarded
            // FREE-FLOW: there is no expected-animal roster to sync. `weighing_expected_animals`
            // is gone (000079) and the scope read's `items` array is permanently empty, so the
            // roster leg of this loop -- its own cursor, its `include_roster` gate and the
            // `rosterDao.replaceScope` write -- was wiping the scope's Room rows and replacing
            // them with nothing on every refresh. Only the bucket's accepted observations are
            // real, and only their cursor is drained here, bounded by safetyLimit.
            var observationsCursor: String? = null
            do {
                val remainingObservations = safetyLimit - accepted.size
                val response = client.getWeighingRoster(
                    campaignId = campaignId,
                    campaignShedId = campaignShedId,
                    observationsCursor = observationsCursor,
                    limit = minOf(WEIGHING_PAGE_SIZE, remainingObservations),
                )
                response.observations.forEach { observation ->
                    accepted[observation.observationId] = observation
                }
                // A cursor that does not strictly change is treated as exhausted, so a
                // server that echoes the same cursor cannot spin this loop forever.
                observationsCursor = response.nextObservationsCursor?.takeIf { it.isNotBlank() && it != observationsCursor }
                if (accepted.size >= safetyLimit) observationsCursor = null
            } while (observationsCursor != null)
            val publishAccepted: suspend () -> Unit = {
                val activeAcceptedIds = accepted.keys.toList()
                if (activeAcceptedIds.isEmpty()) {
                    observationDao.deleteAcceptedForScope(key)
                } else {
                    observationDao.deleteAcceptedNotIn(key, activeAcceptedIds)
                }
                shedObservationDao.deleteAcceptedForScope(key)
                accepted.values.forEach { observation ->
                    // The scanned tag is the whole identity of a free-flow capture. The old guard
                    // required a non-empty animalId, which the backend has not sent since 000078 --
                    // so NO accepted observation was ever restored to the device.
                    val scannedIdentifier = observation.scannedIdentifier.trim()
                    if (scannedIdentifier.isNotEmpty() && observation.weightKg > 0.0 && observation.proofArtifactId.isNotBlank()) {
                        observationDao.restoreAccepted(
                            WeighingObservationEntity(
                                observationId = observation.observationId,
                                scopeKey = key,
                                tenantId = tenantId,
                                campaignId = campaignId,
                                workGroupId = workGroupId,
                                campaignShedId = campaignShedId,
                                expectedLocationId = observation.expectedLocationId,
                                expectedLocationLabel = "",
                                actualLocationId = null,
                                actualLocationLabel = null,
                                scannedIdentifier = scannedIdentifier,
                                weightKg = observation.weightKg,
                                proofCaptureId = observation.proofArtifactId,
                                serverProofId = observation.proofArtifactId,
                                verificationStatus = observation.verificationStatus,
                                reworkReason = observation.reworkReason,
                                syncStatus = WeighingSyncStatus.ACCEPTED.name,
                                idempotencyKey = "weighing:server:${observation.observationId}",
                                capturedAtMs = observation.acceptedAt.toEpochMillisOrNow(),
                                lastError = null,
                            ),
                        )
                    }
                }
            }
            database?.withTransaction {
                publishAccepted()
            } ?: publishAccepted()
            AppResult.Ok(accepted.size)
        }.getOrElse { AppResult.Err(it.userFacingMessage("Could not refresh weighing roster.")) }
    }

    override suspend fun matchTag(scopeKey: String, scannedTag: String): WeighingScanMatch = withContext(Dispatchers.IO) {
        val normalized = normalizeWeighingTag(scannedTag)
        if (normalized.isBlank()) {
            return@withContext WeighingScanMatch(null, "unknown", null, null)
        }
        val row = rosterDao.findByTag(scopeKey, normalized)
        if (row == null) {
            WeighingScanMatch(null, "unknown", null, null)
        } else {
            val mismatch = !row.actualLocationId.isNullOrBlank() &&
                row.actualLocationId != row.expectedLocationId
            WeighingScanMatch(
                row = row,
                outcome = if (mismatch) "wrong_shed" else "expected",
                expectedLocationLabel = row.expectedLocationLabel,
                actualLocationLabel = row.actualLocationLabel ?: row.expectedLocationLabel,
            )
        }
    }

    override suspend fun submitIndividualScope(
        campaignId: String,
        campaignShedId: String,
        scannedIdentifiers: List<String>,
    ): AppResult<Unit> =
        withContext(Dispatchers.IO) {
            val service = api ?: return@withContext AppResult.Err("Weighing service is unavailable.")
            try {
                // The key names the ATTEMPT, not the shed's contents. It used to hash only
                // (campaignId, campaignShedId, scannedIdentifiers), which is byte-identical
                // when a verifier REJECTS a shed and the operator re-submits the same tags:
                // the server replayed the first submission's stored response, returned 200,
                // and wrote nothing, while the phone navigated away as if it had worked. The
                // operator's rework was silently lost -- the same failure the close/reopen
                // epoch below exists to prevent. The epoch rotates only after the server
                // confirms, so retrying an unknown outcome still deduplicates.
                val scopeId = "$campaignId:$campaignShedId"
                val idempotencyKey = transitionIdempotencyKey("submit", scopeId)
                service.submitWeighingScope(
                    campaignId,
                    campaignShedId,
                    idempotencyKey,
                    WeighingScopeSubmitRequestDto(scannedIdentifiers),
                )
                advanceTransitionEpoch("submit", scopeId)
                AppResult.Ok(Unit)
            } catch (error: Throwable) {
                AppResult.Err(error.userFacingMessage("Couldn't submit weighing shed."), error)
            }
        }

    override suspend fun reopenScope(
        campaignId: String,
        campaignShedId: String,
        reason: String,
    ): AppResult<Unit> =
        withContext(Dispatchers.IO) {
            val service = api ?: return@withContext AppResult.Err("Weighing service is unavailable.")
            try {
                val scopeId = "$campaignId:$campaignShedId"
                val idempotencyKey = transitionIdempotencyKey("reopen", scopeId)
                service.reopenWeighingScope(
                    campaignId = campaignId,
                    campaignShedId = campaignShedId,
                    idempotencyKey = idempotencyKey,
                    request = WeighingScopeReopenRequestDto(reason = reason),
                )
                advanceTransitionEpoch("reopen", scopeId)
                AppResult.Ok(Unit)
            } catch (error: Throwable) {
                AppResult.Err(error.userFacingMessage("Couldn't reopen weighing shed."), error)
            }
        }

    override suspend fun closeShedCampaign(
        campaignId: String,
        campaignShedId: String,
        reason: String,
    ): AppResult<Unit> =
        withContext(Dispatchers.IO) {
            val service = api ?: return@withContext AppResult.Err("Weighing service is unavailable.")
            try {
                val scopeId = "$campaignId:$campaignShedId"
                val idempotencyKey = transitionIdempotencyKey("close-shed", scopeId)
                service.closeShedWeighingCampaign(
                    campaignId = campaignId,
                    campaignShedId = campaignShedId,
                    idempotencyKey = idempotencyKey,
                    request = WeighingScopeCloseRequestDto(reason = reason, idempotencyKey = idempotencyKey),
                )
                advanceTransitionEpoch("close-shed", scopeId)
                AppResult.Ok(Unit)
            } catch (error: Throwable) {
                AppResult.Err(error.userFacingMessage("Couldn't close weighing shed."), error)
            }
        }

    override suspend fun closeCampaign(
        campaignId: String,
        reason: String,
    ): AppResult<Unit> =
        withContext(Dispatchers.IO) {
            val service = api ?: return@withContext AppResult.Err("Weighing service is unavailable.")
            try {
                val idempotencyKey = transitionIdempotencyKey("close-campaign", campaignId)
                service.closeWeighingCampaign(
                    campaignId = campaignId,
                    idempotencyKey = idempotencyKey,
                    request = WeighingScopeCloseRequestDto(reason = reason, idempotencyKey = idempotencyKey),
                )
                advanceTransitionEpoch("close-campaign", campaignId)
                AppResult.Ok(Unit)
            } catch (error: Throwable) {
                AppResult.Err(error.userFacingMessage("Couldn't close weighing campaign."), error)
            }
        }

    override suspend fun fetchWeightHistory(
        parkId: String?,
        campaignShedId: String?,
    ): AppResult<WeightHistoryResponseDto> =
        withContext(Dispatchers.IO) {
            val service = api ?: return@withContext AppResult.Err("Weighing service is unavailable.")
            try {
                val response = service.getWeightHistory(parkId = parkId, campaignShedId = campaignShedId)
                AppResult.Ok(response)
            } catch (error: Throwable) {
                AppResult.Err(error.userFacingMessage("Couldn't fetch weight history."), error)
            }
        }

    override suspend fun fetchGrowthSummary(
        parkId: String?,
        from: String?,
        to: String?,
    ): AppResult<sg.mesha.goatos.core.network.GrowthSummaryDto> =
        withContext(Dispatchers.IO) {
            val service = api ?: return@withContext AppResult.Err("Weighing service is unavailable.")
            try {
                AppResult.Ok(service.getWeighingGrowth(parkId, from, to))
            } catch (error: Throwable) {
                AppResult.Err(error.userFacingMessage("Couldn't fetch growth."), error)
            }
        }

    override suspend fun recordIndividual(capture: IndividualWeighingCapture): AppResult<IndividualWeighingDraft> =
        withContext(Dispatchers.IO) {
            if (capture.weightKg <= 0.0) return@withContext AppResult.Err("Weight must be greater than 0 kg.")
            val scopeKey = weighingScopeKey(capture.campaignId, capture.workGroupId, capture.campaignShedId)
            // Keyed on the scanned tag: free-flow weighing has no expected-animal list and no
            // animal identity, so the scanned tag is the only identity a capture carries. The
            // roster table is local-scan state only; a miss simply falls back to scope defaults.
            val rosterRow = rosterDao.findByTag(scopeKey, normalizeWeighingTag(capture.scannedIdentifier))
            val observationId = idGenerator()
            val existing = observationDao.findByScannedIdentifier(scopeKey, capture.scannedIdentifier)
            if (existing != null && existing.matchesDraft(capture) && existing.proofCaptureId.isNullOrBlank()) {
                return@withContext AppResult.Ok(existing.toDraft())
            }
            if (existing?.syncStatus == WeighingSyncStatus.ACCEPTED.name) {
                val revision = existing.copy(
                    scannedIdentifier = capture.scannedIdentifier,
                    weightKg = capture.weightKg,
                    syncStatus = WeighingSyncStatus.READY_TO_SUBMIT.name,
                    idempotencyKey = individualIdempotencyKey(
                        campaignId = capture.campaignId,
                        workGroupId = capture.workGroupId,
                        campaignShedId = capture.campaignShedId,
                        scannedIdentifier = capture.scannedIdentifier,
                        observationId = observationId,
                    ),
                    capturedAtMs = capture.capturedAtMs?.takeIf { it > 0L } ?: clock(),
                    lastError = null,
                )
                observationDao.update(revision)
                return@withContext AppResult.Ok(revision.toDraft())
            }
            if (existing != null) {
                cancelCancellableOutbox(existing.idempotencyKey)
                observationDao.deleteEditable(existing.observationId)
            }
            val idempotencyKey = individualIdempotencyKey(
                campaignId = capture.campaignId,
                workGroupId = capture.workGroupId,
                campaignShedId = capture.campaignShedId,
                scannedIdentifier = capture.scannedIdentifier,
                observationId = observationId,
            )
            val entity = WeighingObservationEntity(
                observationId = observationId,
                scopeKey = scopeKey,
                tenantId = capture.tenantId,
                campaignId = capture.campaignId,
                workGroupId = capture.workGroupId,
                campaignShedId = capture.campaignShedId,
                expectedLocationId = rosterRow?.expectedLocationId ?: capture.campaignShedId,
                expectedLocationLabel = rosterRow?.expectedLocationLabel ?: "Assigned shed",
                actualLocationId = rosterRow?.actualLocationId,
                actualLocationLabel = rosterRow?.actualLocationLabel,
                scannedIdentifier = capture.scannedIdentifier,
                weightKg = capture.weightKg,
                proofCaptureId = null,
                serverProofId = null,
                syncStatus = WeighingSyncStatus.PENDING_LOCAL.name,
                idempotencyKey = idempotencyKey,
                capturedAtMs = capture.capturedAtMs?.takeIf { it > 0L } ?: clock(),
                lastError = null,
            )
            observationDao.insert(entity)
            AppResult.Ok(entity.toDraft())
        }

    override suspend fun attachIndividualProof(
        scopeKey: String,
        scannedIdentifier: String,
        proofCaptureId: String,
        serverProofId: String?,
    ) = withContext(Dispatchers.IO) {
        val row = observationDao.findByScannedIdentifier(scopeKey, scannedIdentifier) ?: return@withContext
        // SAME EVIDENCE, NOT A NEW ONE. Identity of a proof is the id the SERVER gave it, never
        // the id of the local capture file that produced it.
        //
        // This guard used to also demand `row.proofCaptureId == proofCaptureId`, which a row
        // restored from the server can never satisfy: refreshScope writes proofCaptureId =
        // proofArtifactId (the server's id), while the capture screen replays the LOCAL capture
        // row's id. So every restored capture looked like a re-shot video. attachProof then did
        // what attaching a new video is supposed to do -- cleared verificationStatus/reworkReason,
        // dropped the row back to READY_TO_SUBMIT and re-posted it under a fresh `:proof:` key --
        // wiping the verifier's verdict a fraction of a second after the refresh delivered it.
        // Two animals the verifier had sent back therefore rendered with no state at all: not
        // finished (the verdict said rework), not sent back (the field had just been nulled), just
        // an ordinary unfinished row for work that was in fact already recorded and rejected.
        //
        // If the server already holds this exact proof for this capture, there is nothing to
        // attach, whatever the local file is called.
        if (row.syncStatus == WeighingSyncStatus.ACCEPTED.name && !serverProofId.isNullOrBlank() &&
            row.serverProofId == serverProofId
        ) {
            return@withContext
        }
        // REDELIVERY, NOT A REVISION. This method has two callers for the same row: the
        // capture screen attaches the proof as soon as the upload completes, and the
        // observeReadyProofs() reconciler independently replays every ready proof (that
        // replay is deliberate -- it is how a capture survives the screen being closed
        // mid-upload). When the replay carries the SAME serverProofId for a row whose write
        // is ALREADY queued, nothing about the capture changed: same tag, same weight, same
        // proof. Minting a `:proof:` key for it posts the identical capture a second time
        // under a second idempotency key, and the backend -- which can only compare keys --
        // classifies it as an EDIT, withdrawing the pending verification item and raising a
        // fresh round. The first real device run produced 20 verification items and 20
        // accepted events for 10 captures this way.
        //
        // The `:proof:` suffix stays for what it was built for: a row whose base key changed
        // (a weight correction on an already-accepted capture) or a genuinely re-shot proof.
        // Both of those still have no queued write under the row's CURRENT key, so both still
        // enqueue below.
        //
        // "Already queued" must mean the write is STILL GOING TO REACH THE SERVER -- not merely
        // that a row exists under the key. `findOutboxItemByIdempotencyKey` has no status
        // predicate, so it also matches TERMINAL rows: a dead-lettered `conflict` row and an
        // attempt-exhausted FAILED row are both present and both permanently unclaimable
        // (OutboxDao.eligibleForDrain requires `attemptCount < maxAttempts AND conflict = 0`).
        // Suppressing on those strands the capture forever -- and that is not hypothetical: 403
        // stays RETRYABLE (core-network/RetryClassification.kt), so the growth-director
        // permission failure burned the whole retry budget and left exactly such a corpse. The
        // `:proof:` re-mint is the RECOVERY path for it. A SUCCEEDED row, by contrast, did reach
        // the server, so re-posting it under a second key would be the very fan-out above.
        val alreadyQueuedForThisProof = row.serverProofId == serverProofId &&
            !serverProofId.isNullOrBlank() &&
            (syncRepository?.findOutboxItemByIdempotencyKey(row.idempotencyKey) as? AppResult.Ok)
                ?.value
                ?.let { it.isActive || it.status == SyncItemStatus.SUCCEEDED } == true
        if (alreadyQueuedForThisProof) {
            return@withContext
        }
        val isProofRevision = !row.serverProofId.isNullOrBlank()
        val revisionIdempotencyKey = if (isProofRevision && !serverProofId.isNullOrBlank()) {
            "${row.idempotencyKey.substringBefore(":proof:")}:proof:$serverProofId"
        } else {
            row.idempotencyKey
        }
        observationDao.attachProof(
            observationId = row.observationId,
            proofCaptureId = proofCaptureId,
            serverProofId = serverProofId,
            idempotencyKey = revisionIdempotencyKey,
            syncStatus = if (serverProofId.isNullOrBlank()) {
                WeighingSyncStatus.PROOF_UPLOADING.name
            } else {
                WeighingSyncStatus.READY_TO_SUBMIT.name
            },
        )
        if (!serverProofId.isNullOrBlank()) {
            syncRepository?.enqueueWeighingAnimalObservation(
                campaignId = row.campaignId,
                groupKey = row.scopeKey,
                idempotencyKey = revisionIdempotencyKey,
                request = WeighingAnimalObservationRequestDto(
                    campaignShedId = row.campaignShedId,
                    scannedIdentifier = row.scannedIdentifier,
                    weightKg = row.weightKg,
                    proofArtifactId = serverProofId,
                    actualLocationId = row.actualLocationId ?: row.expectedLocationId,
                ),
            )
        }
    }

    override suspend fun recordShedPartition(capture: ShedPartitionWeighingCapture): AppResult<ShedWeighingDraft> =
        withContext(Dispatchers.IO) {
            if (!capture.resultJson.trim().startsWith("{")) {
                return@withContext AppResult.Err("Shed/partition weighing result must be structured JSON.")
            }
            val scopeKey = weighingScopeKey(capture.campaignId, capture.workGroupId, capture.campaignShedId)
            val shedObservationId = idGenerator()
            val existing = shedObservationDao.findByScope(scopeKey)
            if (existing?.syncStatus == WeighingSyncStatus.ACCEPTED.name) {
                return@withContext AppResult.Err("This shed / partition result has already synced.")
            }
            if (existing != null && existing.resultJson == capture.resultJson && existing.proofCaptureId.isNullOrBlank()) {
                return@withContext AppResult.Ok(existing.toDraft())
            }
            if (existing != null) {
                cancelCancellableOutbox(existing.idempotencyKey)
                shedObservationDao.deleteEditable(existing.shedObservationId)
            }
            val idempotencyKey = shedIdempotencyKey(
                capture.campaignId,
                capture.workGroupId,
                capture.campaignShedId,
                shedObservationId,
            )
            val entity = WeighingShedObservationEntity(
                shedObservationId = shedObservationId,
                scopeKey = scopeKey,
                tenantId = capture.tenantId,
                campaignId = capture.campaignId,
                workGroupId = capture.workGroupId,
                campaignShedId = capture.campaignShedId,
                expectedLocationId = capture.expectedLocationId,
                expectedLocationLabel = capture.expectedLocationLabel,
                resultJson = capture.resultJson,
                proofCaptureId = null,
                serverProofId = null,
                syncStatus = WeighingSyncStatus.PENDING_LOCAL.name,
                idempotencyKey = idempotencyKey,
                capturedAtMs = capture.capturedAtMs?.takeIf { it > 0L } ?: clock(),
                lastError = null,
            )
            shedObservationDao.insert(entity)
            val proofBundle = normalizedProofArtifactIds(null, capture.proofArtifactIds)
            if (proofBundle.isNotEmpty()) {
                val result = weighingShedResultValues(capture.resultJson)
                if (result != null) {
                    syncRepository?.enqueueWeighingShedObservation(
                        campaignId = capture.campaignId,
                        groupKey = scopeKey,
                        idempotencyKey = idempotencyKey,
                        request = WeighingShedObservationRequestDto(
                            campaignShedId = capture.campaignShedId,
                            weightKg = result.totalWeightKg,
                            animalCount = result.animalCount,
                            averageWeightKg = result.averageWeightKg,
                            proofArtifactId = proofBundle.first(),
                            proofArtifactIds = proofBundle,
                        ),
                    )
                }
            }
            AppResult.Ok(entity.toDraft())
        }

    override suspend fun attachShedPartitionProof(
        scopeKey: String,
        proofCaptureId: String,
        serverProofId: String?,
        serverProofIds: List<String>,
    ) = withContext(Dispatchers.IO) {
        val existing = shedObservationDao.findByScope(scopeKey) ?: return@withContext
        val proofBundle = normalizedProofArtifactIds(serverProofId, serverProofIds)
        if (
            existing.syncStatus == WeighingSyncStatus.ACCEPTED.name &&
            existing.proofCaptureId == proofCaptureId &&
            existing.serverProofId == serverProofId
        ) {
            return@withContext
        }
        shedObservationDao.attachProof(
            shedObservationId = existing.shedObservationId,
            proofCaptureId = proofCaptureId,
            serverProofId = serverProofId,
            syncStatus = if (serverProofId.isNullOrBlank()) {
                WeighingSyncStatus.PROOF_UPLOADING.name
            } else {
                WeighingSyncStatus.READY_TO_SUBMIT.name
            },
        )
        val result = weighingShedResultValues(existing.resultJson)
        if (proofBundle.isNotEmpty() && result != null) {
            cancelCancellableOutbox(existing.idempotencyKey)
            syncRepository?.enqueueWeighingShedObservation(
                campaignId = existing.campaignId,
                groupKey = existing.scopeKey,
                idempotencyKey = existing.idempotencyKey,
                request = WeighingShedObservationRequestDto(
                    campaignShedId = existing.campaignShedId,
                    weightKg = result.totalWeightKg,
                    animalCount = result.animalCount,
                    averageWeightKg = result.averageWeightKg,
                    proofArtifactId = proofBundle.first(),
                    proofArtifactIds = proofBundle,
                ),
            )
        }
    }

    override suspend fun discardEditableIndividual(scopeKey: String, scannedIdentifier: String) = withContext(Dispatchers.IO) {
        val row = observationDao.findByScannedIdentifier(scopeKey, scannedIdentifier) ?: return@withContext
        cancelCancellableOutbox(row.idempotencyKey)
        observationDao.deleteEditable(row.observationId)
    }

    internal suspend fun reconcileReadyProofsOnce() {
        observationDao.listReadyProofs().forEach { ready ->
            attachIndividualProof(
                scopeKey = ready.scopeKey,
                scannedIdentifier = ready.scannedIdentifier,
                proofCaptureId = ready.proofCaptureId,
                serverProofId = ready.serverProofId,
            )
        }
        shedObservationDao.listReadyProofs().forEach { ready ->
            attachShedPartitionProof(
                scopeKey = ready.scopeKey,
                proofCaptureId = ready.proofCaptureId,
                serverProofId = ready.serverProofId,
            )
        }
    }

    private fun startProofReadyReconciler() {
        val scope = appScope ?: return
        if (syncRepository == null) return
        scope.launch(Dispatchers.IO) {
            reconcileReadyProofsOnce()
        }
        scope.launch(Dispatchers.IO) {
            observationDao.observeReadyProofs().collect { readyRows ->
                readyRows.forEach { ready ->
                    attachIndividualProof(
                        scopeKey = ready.scopeKey,
                        scannedIdentifier = ready.scannedIdentifier,
                        proofCaptureId = ready.proofCaptureId,
                        serverProofId = ready.serverProofId,
                    )
                }
            }
        }
        scope.launch(Dispatchers.IO) {
            shedObservationDao.observeReadyProofs().collect { readyRows ->
                readyRows.forEach { ready ->
                    attachShedPartitionProof(
                        scopeKey = ready.scopeKey,
                        proofCaptureId = ready.proofCaptureId,
                        serverProofId = ready.serverProofId,
                    )
                }
            }
        }
    }

    private suspend fun cancelCancellableOutbox(idempotencyKey: String) {
        val sync = syncRepository ?: return
        val existing = when (val found = sync.findOutboxItemByIdempotencyKey(idempotencyKey)) {
            is AppResult.Ok -> found.value
            is AppResult.Err -> null
        } ?: return
        if (existing.status == SyncItemStatus.QUEUED || existing.status == SyncItemStatus.FAILED) {
            sync.cancelOutboxItemIfPending(existing.id)
        }
    }

    private companion object {
    }
}

private fun WeighingObservationEntity.matchesDraft(capture: IndividualWeighingCapture): Boolean =
    tenantId == capture.tenantId &&
        campaignId == capture.campaignId &&
        workGroupId == capture.workGroupId &&
        campaignShedId == capture.campaignShedId &&
        scannedIdentifier == capture.scannedIdentifier &&
        weightKg == capture.weightKg

private fun WeighingObservationEntity.toDraft(): IndividualWeighingDraft =
    IndividualWeighingDraft(
        observationId = observationId,
        scannedIdentifier = scannedIdentifier,
        weightKg = weightKg,
        capturedAtMs = capturedAtMs,
        proofCaptureId = proofCaptureId,
        proofReady = !proofCaptureId.isNullOrBlank(),
        readyToSubmit = syncStatus == WeighingSyncStatus.READY_TO_SUBMIT.name ||
            syncStatus == WeighingSyncStatus.ACCEPTED.name,
        syncedToBackend = syncStatus == WeighingSyncStatus.ACCEPTED.name,
        idempotencyKey = idempotencyKey,
        serverProofId = serverProofId,
        verificationStatus = verificationStatus,
        reworkReason = reworkReason,
    )

private fun WeighingShedObservationEntity.toDraft(): ShedWeighingDraft =
    ShedWeighingDraft(
        shedObservationId = shedObservationId,
        resultJson = resultJson,
        proofReady = !proofCaptureId.isNullOrBlank(),
        readyToSubmit = syncStatus == WeighingSyncStatus.READY_TO_SUBMIT.name ||
            syncStatus == WeighingSyncStatus.ACCEPTED.name,
        idempotencyKey = idempotencyKey,
    )

/**
 * A cursor is only usable when the backend returned a non-blank value that actually advanced past
 * the cursor we just sent, so a repeated cursor terminates instead of looping on the same page.
 */
private fun String?.nextWeighingCursorAfter(requestCursor: String?): String? =
    this?.trim()?.takeIf { it.isNotEmpty() && it != requestCursor }

private fun String.toEpochMillisOrNow(): Long =
    runCatching {
        // exception:exempt timestamp fallback; unparseable timestamp uses current time
        Instant.parse(this).toEpochMilli()
    }.getOrDefault(System.currentTimeMillis())

private fun WeighingPlannerShedDto.toPlannerShed(): WeighingPlannerShed =
    WeighingPlannerShed(
        locationId = locationId,
        name = operationalLocationDisplay.ifBlank { operationalWeighingLocationLabel(name, partitionLabel) },
        partitionLabel = partitionLabel?.takeIf { it.isNotBlank() },
        kidCount = kidCount,
        scheduled = scheduled,
        scheduledStatus = scheduledStatus,
        scheduledOperatorDisplayName = scheduledOperatorDisplayName,
        scheduledCategory = scheduledWeighingCategory,
        scheduledOperatorUserId = scheduledOperatorUserId,
    )

private fun WeighingCampaignSummaryDto.toCampaignSummary(): WeighingCampaignSummary =
    WeighingCampaignSummary(
        campaignId = campaignId,
        status = status,
        periodStartDate = periodStartDate,
        periodEndDate = periodEndDate,
        startBusinessDate = startBusinessDate,
        operatorUserId = operatorUserId,
        shedCount = shedCount,
    )

/** One cached park row back into the park-grain answer. [shedCount] is read back as stored. */
private fun WeighingPlannerParkRowEntity.toPlannerPark(json: Json): WeighingPlannerPark =
    WeighingPlannerPark(
        parkId = parkId,
        name = name,
        kidCount = kidCount,
        shedCount = shedCount,
        existingCampaign = existingCampaignJson?.let {
            json.decodeFromString<WeighingCampaignSummaryDto>(it).toCampaignSummary()
        },
    )

/**
 * One park's bucket stream is its own cache scope: `<weigh date>|<park id>`.
 *
 * Two parks are two independent keyset streams and must never interleave in one scope — the
 * flattened all-parks page they used to share is what made a 76-shed park swallow page one.
 */
private fun plannerBucketQueryKey(periodStartDate: String, parkId: String, excludeCampaignId: String? = null): String =
    "${periodStartDate.trim()}|${parkId.trim()}" +
        (excludeCampaignId?.trim()?.takeIf { it.isNotBlank() }?.let { "|edit:$it" } ?: "")

/** How many task-list FILTERS keep their cached rows. Bounds the task tables. */
private const val WEIGHING_CACHED_TASK_FILTERS = 4

/** How many TASKS keep their cached bucket rows. */
private const val WEIGHING_CACHED_TASKS = 4

/** How many shed BUCKETS keep their cached context and records. */
private const val WEIGHING_CACHED_SHEDS = 40

/** How many weigh DATES keep a cached planner PARK list. */
private const val WEIGHING_CACHED_CATALOG_DATES = 2

/** How many (date, park) bucket streams keep their cached shed rows and cursor. */
private const val WEIGHING_CACHED_BUCKET_PARKS = 4

/**
 * How many weighing scopes keep an idempotency epoch on disk.
 *
 * A replay window, not a cache: only the scopes a person has recently acted on can still have a
 * request in flight worth deduplicating. Bounded so the table cannot grow with every bucket ever
 * closed on the device.
 */
private const val WEIGHING_CACHED_TRANSITION_SCOPES = 50

/**
 * Sanity ceiling on the park picker, matching the backend's own cap. Parks are few — this bounds
 * the read, it is NOT a page size, and there is no park cursor behind it.
 */
private const val WEIGHING_MAX_PLANNER_PARKS = 100

/** The one query key the leadership videos gallery pages under. */
private const val WEIGHING_VIDEOS_QUERY_KEY = "weighing-videos"

/** Two filters are two independent keyset streams and must never interleave in one cache scope. */
private fun taskListQueryKey(scope: String, parkId: String?): String =
    "weighing-tasks:${scope.trim()}:${parkId?.trim().orEmpty()}"

private fun WeighingCampaignShedDto.toTaskShed(): WeighingTaskShed =
    WeighingTaskShed(
        campaignShedId = campaignShedId,
        locationId = locationId,
        displayName = operationalLocationDisplay.ifBlank { operationalWeighingLocationLabel(displayName, partitionLabel) },
        partitionLabel = partitionLabel?.takeIf { it.isNotBlank() },
        category = weighingCategory,
        operatorUserId = operatorUserId,
        operatorDisplayName = operatorDisplayName,
        status = status,
        pendingVerificationCount = pendingVerificationCount,
        reworkCount = reworkCount,
        readyToClose = readyToClose,
        animalsWeighedCount = animalsWeighedCount,
        animalsSubmittedCount = animalsSubmittedCount,
    )


/**
 * The cached shed row plus its cached records, as ONE screen model.
 *
 * The same mapping serves the detail screen and the gallery card, so the two surfaces cannot drift
 * into showing different answers for the same bucket.
 */
private fun WeighingLeadershipShedEntity.toLeadershipShed(
    records: List<WeighingLeadershipRecordEntity>,
    json: Json,
): WeighingLeadershipShed {
    val lump = lumpSumJson?.let { json.decodeFromString<WeighingObservationDto>(it) }
    return WeighingLeadershipShed(
        shedKey = shedKey,
        campaignId = campaignId,
        campaignShedId = campaignShedId,
        shedName = shedName,
        parkName = parkName,
        weighDate = weighDate,
        operatorUserId = operatorUserId,
        operatorDisplayName = operatorDisplayName,
        category = category,
        status = status,
        periodLabel = periodLabel,
        estimatedAnimalCount = estimatedAnimalCount,
        maxShedVideos = maxShedVideos,
        animals = records.map { record ->
            val observation = json.decodeFromString<WeighingObservationDto>(record.dtoJson)
            WeighingLeadershipAnimal(
                observationId = observation.observationId,
                rfid = observation.scannedIdentifier?.trim().orEmpty(),
                weightKg = observation.weightKg,
                acceptedAt = observation.acceptedAt,
                videos = observation.media.map { WeighingLeadershipVideo(it.proofId, it.downloadUrl) },
            )
        },
        animalCount = lump?.animalCount,
        totalWeightKg = lump?.weightKg,
        averageWeightKg = lump?.averageWeightKg,
        videos = lump?.media.orEmpty().map { WeighingLeadershipVideo(it.proofId, it.downloadUrl) },
    )
}

private fun WeighingPlanDraft.toCreateRequest(): WeighingCreateCampaignRequestDto =
    WeighingCreateCampaignRequestDto(
        parkId = parkId,
        periodStartDate = periodStartDate,
        periodEndDate = periodEndDate,
        startBusinessDate = startBusinessDate,
        plannedCapPerDay = plannedCapPerDay,
        operatorUserId = operatorUserId,
        sheds = sheds.map {
            WeighingCreateCampaignShedDto(
                locationId = it.locationId,
                locationType = "shed",
                displayName = it.name,
                partitionLabel = it.partitionLabel,
                weighingCategory = it.category,
                operatorUserId = it.operatorUserId.ifBlank { operatorUserId },
            )
        },
    )

/**
 * Task-grain projection of one campaign. Unlike [toAssignments] this does NOT drop the campaign:
 * a draft task is a real task the planner must still see, and a canceled bucket is dropped from
 * the bucket list without dropping the task itself.
 */
private fun WeighingCampaignDto.toTask(): WeighingTask =
    WeighingTask(
        campaignId = campaignId,
        tenantId = tenantId,
        parkId = parkId,
        parkName = parkName,
        weighDate = startBusinessDate.ifBlank { periodStartDate },
        status = status,
        closeReason = closeReason,
        sheds = sheds
            .filter { it.status.lowercase() !in setOf("canceled", "cancelled") }
            .map { shed ->
                WeighingTaskShed(
                    campaignShedId = shed.campaignShedId,
                    locationId = shed.locationId,
                    displayName = shed.operationalLocationDisplay.ifBlank { operationalWeighingLocationLabel(shed.displayName, shed.partitionLabel) },
                    partitionLabel = shed.partitionLabel?.takeIf { it.isNotBlank() },
                    category = shed.weighingCategory,
                    operatorUserId = shed.operatorUserId.ifBlank { operatorUserId },
                    status = shed.status,
                    pendingVerificationCount = shed.pendingVerificationCount,
                    reworkCount = shed.reworkCount,
                    readyToClose = shed.readyToClose,
                    animalsWeighedCount = shed.animalsWeighedCount,
                    animalsSubmittedCount = shed.animalsSubmittedCount,
                )
            },
    )

private fun WeighingOperatorSummaryDto.toOperatorSummary(): WeighingOperatorSummary =
    WeighingOperatorSummary(
        operatorUserId = operatorUserId,
        operatorDisplayName = operatorDisplayName,
        shedCount = shedCount,
        notStarted = notStartedCount,
        capturing = capturingCount,
        submitted = submittedCount,
        accepted = acceptedCount,
        rework = reworkCount,
        animalsWeighed = animalsWeighedCount,
        animalsSubmitted = animalsSubmittedCount,
    )

private fun WeighingCampaignDto.toAssignments(scope: String): List<WeighingAssignment> =
    sheds
        .filter { shed ->
            // The operator surface (WEIGHING_SCOPE_MINE) drops closed buckets: once closed, a
            // bucket has no open work for the operator to execute. The leadership/oversight surface
            // (WEIGHING_SCOPE_OPERATORS) needs the OPPOSITE: closed buckets must reach it so history
            // is visible and LeadershipWeighingScreen's tap-to-reopen (row.isClosed) has rows to act
            // on -- excluding them here made reopen permanently unreachable dead code (A23). Plain
            // completed/submitted buckets are verifier history, not operator My work.
            val historyAllowed = scope == WEIGHING_SCOPE_OPERATORS || scope == WEIGHING_SCOPE_ALL
            val shedStatus = shed.status.lowercase()
            status in setOf("published", "in_progress", "delayed", "completed", "closed") &&
                shedStatus !in setOf("canceled", "cancelled") &&
                (historyAllowed || shedStatus !in setOf("closed", "completed"))
        }
        .map { shed ->
            WeighingAssignment(
                campaignId = campaignId,
                tenantId = tenantId,
                parkId = parkId,
                parkName = parkName,
                workGroupId = shed.campaignShedId,
                campaignShedId = shed.campaignShedId,
                expectedLocationId = shed.locationId,
                expectedLocationLabel = shed.displayName,
                label = shed.displayName,
                category = shed.weighingCategory,
                operatorUserId = shed.operatorUserId.ifBlank { operatorUserId },
                operatorDisplayName = shed.operatorDisplayName,
                status = shed.status,
                periodLabel = listOf(periodStartDate, periodEndDate)
                    .filter { it.isNotBlank() }
                    .joinToString(" - "),
                readyToClose = shed.readyToClose,
                pendingVerificationCount = shed.pendingVerificationCount,
            )
        }

/**
 * Collapses a campaign's bucket set into a fixed-width digest.
 *
 * The campaign-create key names the WORK ("same date, park and bucket set is the same task"), so
 * it embedded the full bucket list verbatim. That makes the key grow without bound: the first real
 * device run produced create keys of 249 and 431 characters. The domain-event envelope caps
 * idempotency_key at 320 and trace_id at 200, so those events were written and then permanently
 * rejected by the relay as invalid_event_envelope -- marked failed on attempt 1, never retried,
 * never dead-lettered. Two of two campaign_created events died that way.
 *
 * A digest keeps the identity the comment promises (same bucket set -> same key) at a constant 64
 * characters. Sorting first fixes a second, quieter bug in the same line: the list was in UI order,
 * so selecting the same sheds in a different order produced a DIFFERENT key and could create a
 * duplicate campaign.
 *
 * Note for rollout: this changes the key VALUE, so an in-flight create issued by the previous build
 * and retried by this one would not deduplicate against it.
 */
internal fun weighingBucketSetDigest(buckets: List<String>): String {
    val canonical = buckets.sorted().joinToString(",")
    val digest = java.security.MessageDigest.getInstance("SHA-256").digest(canonical.toByteArray())
    return digest.joinToString("") { "%02x".format(it) }
}

private fun String?.normalizedWeighingPartitionKey(): String =
    this?.trim()?.lowercase().orEmpty()

fun individualIdempotencyKey(
    campaignId: String,
    workGroupId: String,
    campaignShedId: String,
    scannedIdentifier: String,
    observationId: String,
): String = "weighing:individual:$campaignId:$workGroupId:$campaignShedId:$scannedIdentifier:$observationId"

fun shedIdempotencyKey(campaignId: String, workGroupId: String, campaignShedId: String, shedObservationId: String): String =
    "weighing:shed:$campaignId:$workGroupId:$campaignShedId:$shedObservationId"

private fun weighingSubmitIdempotencyKey(
    campaignId: String,
    campaignShedId: String,
    scannedIdentifiers: List<String>,
): String {
    val scopeHash = scannedIdentifiers
        .map { it.trim() }
        .filter { it.isNotEmpty() }
        .distinct()
        .sorted()
        .joinToString("|")
        .hashCode()
        .toUInt()
        .toString(16)
    return "weighing:submit:$campaignId:$campaignShedId:$scopeHash"
}

fun weighingShedResult(weightKg: Double, unit: String = "kg"): JsonObject = buildJsonObject {
    put("weight", weightKg)
    put("unit", unit)
}

private data class WeighingShedResultValues(
    val totalWeightKg: Double,
    val animalCount: Int,
    val averageWeightKg: Double,
)

private fun weighingShedResultValues(resultJson: String): WeighingShedResultValues? =
    runCatching {
        // exception:exempt JSON parse fallback; malformed result returns null for null-coalescing
        val result = Json.parseToJsonElement(resultJson).jsonObject
        val total = (result["total_weight_kg"] ?: result["weight"])?.jsonPrimitive?.doubleOrNull ?: return@runCatching null
        val count = result["animal_count"]?.jsonPrimitive?.content?.toIntOrNull() ?: 1
        if (total <= 0 || count <= 0) return@runCatching null
        WeighingShedResultValues(total, count, total / count)
    }.getOrNull()

private fun normalizedProofArtifactIds(primary: String?, ids: List<String>): List<String> =
    buildList {
        fun addProofId(id: String?) {
            val normalized = id?.trim().orEmpty()
            if (normalized.isNotEmpty() && normalized !in this) add(normalized)
        }
        addProofId(primary)
        ids.forEach(::addProofId)
    }.take(5)

/**
 * ONE shed-grain leadership read, mapped once.
 *
 * The list and the single-shed read hit the SAME backend contract, so they must produce the same
 * shape: two hand-written copies is how one surface silently loses a field the other shows.
 * [periodLabel] is the only thing the caller supplies, because the shed read does not carry the
 * task's weigh period.
 */
private fun WeighingLeadershipShedVideosDto.toLeadershipShed(periodLabel: String): WeighingLeadershipShed {
    val lump = lumpSum
    return WeighingLeadershipShed(
        shedKey = weighingShedKey(campaignId, campaignShedId),
        campaignId = campaignId,
        campaignShedId = campaignShedId,
        shedName = shedName,
        partitionLabel = partitionLabel,
        operationalLocationDisplay = operationalLocationDisplay,
        parkName = parkName,
        weighDate = weighDate,
        operatorUserId = operatorUserId,
        operatorDisplayName = operatorDisplayName,
        category = weighingCategory,
        status = status,
        periodLabel = periodLabel,
        estimatedAnimalCount = estimatedAnimalCount,
        maxShedVideos = maxShedVideos,
        animals = individual.map { observation ->
            WeighingLeadershipAnimal(
                observationId = observation.observationId,
                rfid = observation.scannedIdentifier?.trim().orEmpty(),
                weightKg = observation.weightKg,
                acceptedAt = observation.acceptedAt,
                videos = observation.media.map { WeighingLeadershipVideo(it.proofId, it.downloadUrl) },
            )
        },
        animalCount = lump?.animalCount,
        totalWeightKg = lump?.weightKg,
        averageWeightKg = lump?.averageWeightKg,
        videos = lump?.media.orEmpty().map { WeighingLeadershipVideo(it.proofId, it.downloadUrl) },
    )
}

/**
 * The staleness sentence for a cached leadership answer, or blank while it is from today.
 *
 * Age is measured in Asia/Kolkata BUSINESS DAYS, never in hours: weighing work is planned and read
 * by the day, so "yesterday's answer" is the thing a reader has to be told about. A row-count bound
 * is not an age bound — without this, an offline cold start renders a month-old bucket exactly like
 * a fresh one, because a refresh FAILURE was the only staleness a screen could see.
 */
fun weighingCacheAgeNotice(cachedAt: Long, now: Long = System.currentTimeMillis()): String {
    if (cachedAt <= 0L) return ""
    val zone = ZoneId.of(WEIGHING_CACHE_BUSINESS_ZONE)
    val cachedDay = Instant.ofEpochMilli(cachedAt).atZone(zone).toLocalDate()
    val today = Instant.ofEpochMilli(now).atZone(zone).toLocalDate()
    if (!cachedDay.isBefore(today)) return ""
    return "Saved on ${cachedDay.format(WEIGHING_CACHE_DAY_FORMAT)}."
}

private fun operationalWeighingLocationLabel(shedName: String?, partitionLabel: String?): String {
    val shed = shedName?.trim().orEmpty()
    val partition = partitionLabel?.trim().orEmpty()
    if (partition.isBlank() || partition.equals("whole", ignoreCase = true)) return shed
    if (shed.isBlank()) return partition
    if (shed.endsWith(" - $partition", ignoreCase = true)) return shed
    return "$shed - $partition"
}

/** Weighing is planned, executed and read on the Asia/Kolkata business day. */
private const val WEIGHING_CACHE_BUSINESS_ZONE = "Asia/Kolkata"

private val WEIGHING_CACHE_DAY_FORMAT: DateTimeFormatter = DateTimeFormatter.ofPattern("d MMM")
