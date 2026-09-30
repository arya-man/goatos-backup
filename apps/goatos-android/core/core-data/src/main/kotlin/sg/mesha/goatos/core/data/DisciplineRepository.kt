package sg.mesha.goatos.core.data

import kotlinx.coroutines.flow.firstOrNull
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.cache.HrmsBlobCacheDao
import sg.mesha.goatos.core.data.cache.HrmsBlobCacheEntity
import sg.mesha.goatos.core.data.cache.enforceCacheBounds
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.CallFailure
import sg.mesha.goatos.core.network.classifyCallFailure
import sg.mesha.goatos.core.network.dto.EnquiryDetailDto
import sg.mesha.goatos.core.network.dto.EnquiryPageDto
import sg.mesha.goatos.core.network.dto.RecordViolationRequestDto
import sg.mesha.goatos.core.network.dto.SubmitEnquiryRequestDto
import sg.mesha.goatos.core.network.dto.ViolationsPageDto

/**
 * HRMS enquiries + violations on the phone (maintainer decisions 2026-09-30): the park head's
 * open enquiries on the Tasks "For me" tab, one enquiry's report, and recording a violation
 * against someone in the parks they head.
 *
 * Reads are network-first with a Room blob write on success and a cache fallback on a TRANSIENT
 * failure, so a park head re-entering the tab offline sees the last list instead of a blank wall.
 * A 403 is NOT served from cache: it is the server's definitive "this is not yours", and
 * [HrmsAccessDenied] tells the screen to hide the section rather than show stale work. Writes go
 * through the durable outbox under keys the ViewModel mints once and persists.
 */
interface DisciplineRepository {
    suspend fun fetchOpenEnquiries(): Result<EnquiryPageDto>

    suspend fun fetchEnquiry(enquiryId: String): Result<EnquiryDetailDto>

    /**
     * One page of a month's violations in the caller's parks (plus the record form's options).
     * [month] null = the current month; the FIRST page of each month is Room-cached.
     */
    suspend fun fetchViolations(month: String? = null, cursor: String? = null): Result<ViolationsPageDto>

    suspend fun submitEnquiry(enquiryId: String, idempotencyKey: String, request: SubmitEnquiryRequestDto): AppResult<String>

    suspend fun recordViolation(idempotencyKey: String, request: RecordViolationRequestDto): AppResult<String>
}

/** The server said 403: the caller holds no HRMS authority for this read. Never a cache miss. */
class HrmsAccessDenied(cause: Throwable) : Exception("hrms access denied", cause)

class DefaultDisciplineRepository(
    private val api: AppApi,
    private val dao: HrmsBlobCacheDao,
    private val syncRepository: SyncRepository,
    private val json: Json = Json { ignoreUnknownKeys = true },
) : DisciplineRepository {

    override suspend fun fetchOpenEnquiries(): Result<EnquiryPageDto> = // offline-first-guard:ignore: network-first with Room blob-cache write on success and cache fallback on failure via the upsert()/readBlob() helpers
        cached(OPEN_ENQUIRIES_KEY, EnquiryPageDto.serializer()) { api.listEnquiries(status = "open", limit = PAGE_SIZE) }

    override suspend fun fetchEnquiry(enquiryId: String): Result<EnquiryDetailDto> = // offline-first-guard:ignore: network-first with Room blob-cache write on success and cache fallback on failure via the upsert()/readBlob() helpers
        cached("enquiry:$enquiryId", EnquiryDetailDto.serializer()) { api.getEnquiry(enquiryId) }

    override suspend fun fetchViolations(month: String?, cursor: String?): Result<ViolationsPageDto> { // offline-first-guard:ignore: network-first with Room blob-cache write on success and cache fallback on failure via the upsert()/readBlob() helpers
        // Only a month's FIRST page is cached -- the offline fallback is the list as last seen,
        // never an accumulation of every page ever fetched.
        if (!cursor.isNullOrBlank()) return runCatching { api.listViolations(month = month, limit = PAGE_SIZE, cursor = cursor) }
        return cached("$VIOLATIONS_KEY:${month.orEmpty()}", ViolationsPageDto.serializer()) { api.listViolations(month = month, limit = PAGE_SIZE) }
    }

    override suspend fun submitEnquiry(enquiryId: String, idempotencyKey: String, request: SubmitEnquiryRequestDto): AppResult<String> =
        syncRepository.enqueueEnquirySubmit(enquiryId = enquiryId, idempotencyKey = idempotencyKey, request = request)

    override suspend fun recordViolation(idempotencyKey: String, request: RecordViolationRequestDto): AppResult<String> =
        syncRepository.enqueueViolationRecord(idempotencyKey = idempotencyKey, request = request.copy(idempotencyKey = idempotencyKey))

    private suspend fun <T> cached(
        cacheKey: String,
        serializer: kotlinx.serialization.KSerializer<T>,
        fetch: suspend () -> T,
    ): Result<T> = runCatching { fetch() }
        .onSuccess { dto -> upsert(cacheKey, json.encodeToString(serializer, dto)) }
        .recoverCatching { failure ->
            if (failure.classifyCallFailure() is CallFailure.Denied) {
                // A stale blob from when the person still held the authority must not resurface.
                dao.delete(cacheKey)
                throw HrmsAccessDenied(failure)
            }
            readBlob(cacheKey, serializer) ?: throw failure
        }

    private suspend fun upsert(cacheKey: String, blob: String) {
        dao.upsert(HrmsBlobCacheEntity(cacheKey = cacheKey, dtoJson = blob, updatedAt = System.currentTimeMillis()))
        dao.enforceCacheBounds()
    }

    private suspend fun <T> readBlob(cacheKey: String, serializer: kotlinx.serialization.KSerializer<T>): T? {
        val blob = dao.observe(cacheKey).firstOrNull()?.dtoJson ?: return null
        // exception:exempt corrupt cached blob is a cache miss, not a crash; caller rethrows the network failure.
        return runCatching { json.decodeFromString(serializer, blob) }.getOrNull()
    }

    private companion object {
        const val OPEN_ENQUIRIES_KEY = "enquiries:open"
        const val VIOLATIONS_KEY = "violations"

        /** Mobile page size (docs/decisions/mobile-data-fetch-anti-patterns.md): ~20, never more. */
        const val PAGE_SIZE = 20
    }
}
