package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * Counts vertical wire DTOs — the census READ models and the three count-moving WRITE bodies.
 *
 * Field names are taken verbatim from `contracts/openapi/app-api.yaml`
 * (`HerdRegisterSummaryResponse`, `CountsBreakdownResponse`) and from the backend app-write
 * handler (`backend/internal/counts/adapters/http/app_write_handler.go`) for the three
 * `/app/counts/…-events` routes. Nothing here invents a shape: the two read schemas use
 * different casing conventions on the wire (herd-register is camelCase, counts/breakdown is
 * snake_case) and both are mirrored exactly rather than normalized, because the backend owns
 * the contract.
 *
 * Every field carries a default so a contract addition never breaks decode of an already-cached
 * Room row (the same lenient-decode rule every other read model here follows).
 */

// ---------------------------------------------------------------------------
// READ — GET /herd-register/summary
// ---------------------------------------------------------------------------

/** One scoped census row. NOTE: camelCase on the wire — see `HerdRegisterSummaryCounts`. */
@Serializable
data class HerdRegisterSummaryCountsDto(
    @SerialName("parkId") val parkId: String? = null,
    @SerialName("farmId") val farmId: String? = null,
    @SerialName("currentLocationId") val currentLocationId: String? = null,
    @SerialName("breed") val breed: String? = null,
    @SerialName("sex") val sex: String = "",
    @SerialName("lifecycleStatus") val lifecycleStatus: String = "",
    @SerialName("activeCount") val activeCount: Int = 0,
    @SerialName("adultCount") val adultCount: Int = 0,
    @SerialName("kidCount") val kidCount: Int = 0,
    @SerialName("untaggedKidCount") val untaggedKidCount: Int = 0,
    @SerialName("projectedAt") val projectedAt: String = "",
)

@Serializable
data class HerdRegisterSummaryResponseDto(
    @SerialName("items") val items: List<HerdRegisterSummaryCountsDto> = emptyList(),
)

// ---------------------------------------------------------------------------
// READ — GET /counts/breakdown
// ---------------------------------------------------------------------------

/**
 * One aggregated census grain (farm x stage x breed x sex x shed).
 *
 * `management_stage` is raw source text with no controlled vocabulary — near-duplicate labels
 * are reported verbatim by the backend and must be rendered verbatim here, never normalized
 * on device.
 */
@Serializable
data class CountsBreakdownRowDto(
    @SerialName("park_id") val parkId: String? = null,
    @SerialName("park_label") val parkLabel: String = "",
    @SerialName("shed_id") val shedId: String? = null,
    @SerialName("shed_label") val shedLabel: String = "",
    @SerialName("management_stage") val managementStage: String = "",
    @SerialName("breed") val breed: String = "",
    @SerialName("sex") val sex: String = "",
    @SerialName("count") val count: Int = 0,
) {
    /**
     * Stable identity of this aggregated grain. Used as the Room primary key and as the
     * LazyColumn item key so a re-page never reorders or duplicates a row. Derived from the
     * grouping columns the backend aggregates on — NOT from list position.
     */
    val grainKey: String
        get() = listOf(
            parkId.orEmpty(),
            shedId.orEmpty(),
            managementStage,
            breed,
            sex,
        ).joinToString("|")
}

@Serializable
data class CountsBreakdownSeriesPointDto(
    @SerialName("key") val key: String = "",
    @SerialName("label") val label: String = "",
    @SerialName("count") val count: Int = 0,
)

@Serializable
data class CountsBreakdownChartsDto(
    @SerialName("breed") val breed: List<CountsBreakdownSeriesPointDto> = emptyList(),
    @SerialName("stage") val stage: List<CountsBreakdownSeriesPointDto> = emptyList(),
    @SerialName("sex") val sex: List<CountsBreakdownSeriesPointDto> = emptyList(),
    @SerialName("shed") val shed: List<CountsBreakdownSeriesPointDto> = emptyList(),
)

/**
 * One shed the census may be filtered to.
 *
 * Identical to [CountsBreakdownSeriesPointDto] plus [parkId], and that extra field is the whole
 * point: 66 of the ~154 shed NAMES exist in BOTH parks, so a flat shed vocabulary is genuinely
 * ambiguous to read and impossible to cascade. [key] is the shed's own id (what a filter sends);
 * [parkId] is the park it belongs to (what narrows the dropdown once a park is chosen).
 *
 * Defaults everywhere, per this file's lenient-decode rule, so a backend that has not yet shipped
 * the `sheds` facet decodes to an empty list and the shed filter degrades to disabled rather than
 * crashing — and so an entry that arrives without a park attribution is simply not offered under
 * any park, instead of being offered under the wrong one.
 */
@Serializable
data class CountsBreakdownShedFacetDto(
    @SerialName("key") val key: String = "",
    @SerialName("label") val label: String = "",
    @SerialName("count") val count: Int = 0,
    @SerialName("park_id") val parkId: String = "",
)

@Serializable
data class CountsBreakdownFacetsDto(
    // Distinct lifecycle_status values present in the WHOLE tenant herd (alive/dead/sold/
    // culled/transferred), independent of the currently-selected lifecycle filter — backs the
    // Live/Sold/Culled/Dead/Transferred filter dimension. Default empty list, per this file's
    // lenient-decode rule, degrades the lifecycle filter to "unsupported" on an older backend.
    @SerialName("lifecycle") val lifecycle: List<CountsBreakdownSeriesPointDto> = emptyList(),
    @SerialName("stages") val stages: List<CountsBreakdownSeriesPointDto> = emptyList(),
    @SerialName("breeds") val breeds: List<CountsBreakdownSeriesPointDto> = emptyList(),
    @SerialName("parks") val parks: List<CountsBreakdownSeriesPointDto> = emptyList(),
    @SerialName("sheds") val sheds: List<CountsBreakdownShedFacetDto> = emptyList(),
) {
    /**
     * True once ANY dimension has arrived. Used to decide whether a freshly-emitted envelope
     * carries a usable vocabulary or is an empty placeholder that must not overwrite the one the
     * filter bar is already showing.
     */
    val hasAnyDimension: Boolean
        get() = lifecycle.isNotEmpty() || stages.isNotEmpty() || breeds.isNotEmpty() ||
            parks.isNotEmpty() || sheds.isNotEmpty()
}

/**
 * `total_*` and every `charts` series are rolled up over the FULL filtered result set and are
 * therefore independent of `limit`/`offset` — only [items] is a page. That is why the mobile
 * cache stores the totals/charts/facets envelope separately from the paged rows: a KPI must
 * never be re-derived by summing the ~20 rows currently in memory.
 */
@Serializable
data class CountsBreakdownResponseDto(
    @SerialName("items") val items: List<CountsBreakdownRowDto> = emptyList(),
    @SerialName("total_rows") val totalRows: Int = 0,
    @SerialName("total_count") val totalCount: Int = 0,
    @SerialName("total_kids") val totalKids: Int = 0,
    @SerialName("total_adults") val totalAdults: Int = 0,
    @SerialName("charts") val charts: CountsBreakdownChartsDto = CountsBreakdownChartsDto(),
    @SerialName("facets") val facets: CountsBreakdownFacetsDto = CountsBreakdownFacetsDto(),
    @SerialName("projected_at") val projectedAt: String = "",
)

// ---------------------------------------------------------------------------
// WRITE — POST /app/counts/shifting-events
// ---------------------------------------------------------------------------

/**
 * READ — `GET /app/counts/shifting/destinations`.
 *
 * The destination catalog behind the shifting screen's two cascading dropdowns: every park the
 * caller may move animals INTO, each carrying its own sheds. Permission `counts.write` — this is
 * "where may I move animals to", so it is scoped to the write authority, not to plain read access.
 *
 * Bounded and rarely-changing by construction (order-of two parks, ~154 sheds), which is why it is
 * fetched whole rather than paged: it is a picker VOCABULARY, not a screen list. It is still a
 * screen-facing read, so it is persisted to Room and observed as a Flow like every other read model
 * (docs/decisions/android-offline-first.md).
 *
 * [CountsDestinationShedDto.shedId] is the dropdown's identity. Shed NAMES repeat across parks
 * ("Castro 1" exists in more than one), so a name-keyed entry would collapse two real sheds into
 * one and silently move animals to the wrong park.
 */
@Serializable
data class CountsDestinationShedDto(
    @SerialName("shed_id") val shedId: String = "",
    @SerialName("name") val name: String = "",
)

@Serializable
data class CountsDestinationParkDto(
    @SerialName("park_id") val parkId: String = "",
    @SerialName("name") val name: String = "",
    @SerialName("sheds") val sheds: List<CountsDestinationShedDto> = emptyList(),
)

@Serializable
data class CountsShiftingDestinationsResponseDto(
    @SerialName("parks") val parks: List<CountsDestinationParkDto> = emptyList(),
)

/**
 * An operator-REPORTED movement of ONE animal between sheds. The backend records it with
 * `authorization_state=pending` / `verification_state=unverified`: a field operator reports a
 * movement, they never self-authorize it.
 *
 * [goatIds] is REQUIRED by `RecordShiftingEventRequest` and is load-bearing, not metadata:
 * approving the request relocates EXACTLY these animals to the destination shed and re-scopes their
 * shed-scoped vaccination obligations. The simplified operator flow submits exactly ONE animal per
 * movement, but the field stays a LIST because that is the backend's contract shape — the client
 * does not narrow a server contract it does not own.
 *
 * Deliberately NOT sent any more (both were removed with the screen's simplification):
 *  - `impacts` — the aggregate breed/stage head-count model. The backend now DERIVES the impact
 *    from the single selected animal's own canonical breed/stage, which is authoritative; an
 *    operator hand-typing a breed next to an animal the server already knows the breed of was a
 *    second, contradictable source of truth for the same fact.
 *  - `effective_at` — the backend stamps the recording time. An optional free-text instant on a
 *    phone form is a data-quality liability with no operator use case behind it.
 *  - `source_park_id` / `source_shed_id` — the animal's CURRENT location is fetched with the
 *    animal and shown read-only, so there is nothing for the operator to type and nothing for the
 *    client to assert. The server reads the source from the animal itself.
 *
 * `priority` and `category` are now always sent EXPLICITLY (`normal`/`emergency` and
 * `routine`/`pregnancy`/`medical`/`quarantine`), because the screen defaults them visibly. A
 * present-but-invalid value is rejected server-side rather than silently rewritten.
 */
@Serializable
data class CountsShiftingEventRequestDto(
    @SerialName("destination_park_id") val destinationParkId: String,
    @SerialName("destination_shed_id") val destinationShedId: String,
    @SerialName("priority") val priority: String = "",
    @SerialName("category") val category: String = "",
    @SerialName("proof_ref") val proofRef: String? = null,
    @SerialName("goat_ids") val goatIds: List<String> = emptyList(),
)

@Serializable
data class CountsShiftingEventResponseDto(
    @SerialName("shifting_event_id") val shiftingEventId: String = "",
    @SerialName("idempotent_replay") val idempotentReplay: Boolean = false,
)

// ---------------------------------------------------------------------------
// WRITE — shared evidence envelope (identity's EvidenceRef)
// ---------------------------------------------------------------------------

/**
 * Provenance for a lifecycle write. Identity REQUIRES at least one evidence ref on both goat
 * creation and the critical-death exit (`validateEvidenceRefs(refs, requireNonEmpty = true)`),
 * so neither write below treats this as optional.
 *
 * [evidenceType] must be one of identity's supported types — a mobile-recorded event uses
 * `source_record`, whose [evidenceId] is the write's own stable idempotency key. That makes the
 * server-side row traceable back to the exact operator submission that produced it, including
 * across a retry (the key never changes).
 */
@Serializable
data class CountsEvidenceRefDto(
    @SerialName("evidence_type") val evidenceType: String = SOURCE_RECORD,
    @SerialName("evidence_id") val evidenceId: String,
    @SerialName("source_system") val sourceSystem: String? = MOBILE_SOURCE_SYSTEM,
    @SerialName("description") val description: String? = null,
) {
    companion object {
        const val SOURCE_RECORD = "source_record"
        const val MOBILE_SOURCE_SYSTEM = "goatos_android"
    }
}

// ---------------------------------------------------------------------------
// WRITE — POST /app/counts/birth-events
// ---------------------------------------------------------------------------

/**
 * A birth IS goat creation: the body is identity's `CreateAdminGoatRequest`, so identity keeps
 * ownership of every rule (dob required, `dob <= entry_date`, identifier uniqueness, location
 * resolution). `origin_type` is deliberately NOT sent — the backend pins it to `birth` and
 * rejects a present value that disagrees, so the app route can never create a procured animal.
 *
 * Dates are `YYYY-MM-DD` strings, matching identity's wire contract.
 */
@Serializable
data class CountsBirthEventRequestDto(
    @SerialName("animal_identifier_1") val animalIdentifier1: String,
    @SerialName("animal_identifier_2") val animalIdentifier2: String? = null,
    @SerialName("species") val species: String,
    @SerialName("park_id") val parkId: String? = null,
    @SerialName("shed_id") val shedId: String? = null,
    @SerialName("breed") val breed: String? = null,
    @SerialName("sex") val sex: String,
    @SerialName("dob") val dob: String,
    @SerialName("entry_date") val entryDate: String,
    @SerialName("dam_id") val damId: String? = null,
    @SerialName("sire_or_lot") val sireOrLot: String? = null,
    @SerialName("weight_kg") val weightKg: Double? = null,
    @SerialName("management_stage") val managementStage: String? = null,
    @SerialName("evidence_refs") val evidenceRefs: List<CountsEvidenceRefDto> = emptyList(),
)

// ---------------------------------------------------------------------------
// WRITE — POST /app/counts/death-events
// ---------------------------------------------------------------------------

/**
 * A death recorded through identity's guardrailed critical-death exit.
 *
 * The `lifecycle_status="dead"` + `exit_reason="died"` pairing is a SERVER-enforced medical
 * guardrail (`validateCriticalDeathExit`) — the constants below exist so a caller cannot
 * hand-type a weaker pairing, but the backend is still the authority that rejects one. The
 * ordinary `/exit` path refuses death entirely; this route emits `goat.exited`, which
 * auto-cancels the animal's open obligations.
 */
@Serializable
data class CountsDeathEventRequestDto(
    @SerialName("goat_id") val goatId: String,
    @SerialName("lifecycle_status") val lifecycleStatus: String = DEATH_LIFECYCLE_STATUS,
    @SerialName("exit_reason") val exitReason: String = DEATH_EXIT_REASON,
    @SerialName("reason") val reason: String,
    @SerialName("occurred_at") val occurredAt: String? = null,
    @SerialName("evidence_refs") val evidenceRefs: List<CountsEvidenceRefDto> = emptyList(),
    @SerialName("row_version") val rowVersion: Int,
) {
    companion object {
        /** The only lifecycle status the critical-death exit accepts. */
        const val DEATH_LIFECYCLE_STATUS = "dead"

        /** The only exit reason the critical-death exit accepts, and it must match the status. */
        const val DEATH_EXIT_REASON = "died"
    }
}

/**
 * Lenient response envelope shared by the birth and death routes (identity's
 * `AdminGoatResponse`). Only the fields the app actually surfaces are bound — the full response
 * also carries identifiers, decision, events, and idempotency metadata that mobile does not
 * render.
 */
@Serializable
data class CountsGoatLifecycleResponseDto(
    @SerialName("goat") val goat: CountsGoatSummaryDto = CountsGoatSummaryDto(),
    @SerialName("generation_status") val generationStatus: String = "",
    @SerialName("trace_id") val traceId: String = "",
)

/**
 * The bound slice of `GoatSummary`. Deliberately NOT carrying `row_version`: the contract's
 * `GoatSummary` does not expose one, so the death write's optimistic-concurrency guard cannot be
 * round-tripped from a previous response and must be supplied by the caller.
 */
@Serializable
data class CountsGoatSummaryDto(
    @SerialName("goat_id") val goatId: String = "",
    @SerialName("display_id") val displayId: String = "",
    @SerialName("lifecycle_status") val lifecycleStatus: String = "",
)
