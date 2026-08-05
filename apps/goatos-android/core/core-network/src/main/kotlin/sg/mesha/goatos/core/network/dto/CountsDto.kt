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
    @SerialName("partition_label") val partitionLabel: String? = null,
    @SerialName("operational_location_display") val operationalLocationDisplay: String = "",
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

/**
 * Response of `GET /app/counts/breeds` — the breed vocabulary the operator birth form's breed
 * picker renders. Same shape as the Counts Breakdown `breeds` facet ([key]=[label]=`goats.breed`,
 * with a herd head [count]), but served on the operator (CountsWrite) surface: the read-only Counts
 * Breakdown that also exposes breeds is CountsRead, which a field operator does not hold, so the
 * birth form must NOT source its breeds from `/counts/breakdown`. Default empty list per this file's
 * lenient-decode rule, so an older backend degrades the picker to disabled rather than crashing.
 */
@Serializable
data class CountsBreedsResponseDto(
    @SerialName("breeds") val breeds: List<CountsBreakdownSeriesPointDto> = emptyList(),
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
    @SerialName("partition_label") val partitionLabel: String? = null,
    @SerialName("operational_location_display") val operationalLocationDisplay: String = "",
    @SerialName("management_stages") val managementStages: List<String> = emptyList(),
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
    @SerialName("management_stages") val managementStages: List<String> = emptyList(),
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
 *  - `management_stage_mode` / `target_management_stage` — the movement adopts the DESTINATION
 *    SHED's cohort, resolved server-side at raise time. The operator is not asked, so the client
 *    sends nothing. These are not optional-and-ignored: the server rejects them as unknown fields.
 *
 * `priority` and `category` are now always sent EXPLICITLY (`normal`/`emergency` and
 * `routine`/`pregnancy`/`medical`/`quarantine`), because the screen defaults them visibly. A
 * present-but-invalid value is rejected server-side rather than silently rewritten.
 */
@Serializable
data class CountsShiftingEventRequestDto(
    @SerialName("destination_park_id") val destinationParkId: String,
    @SerialName("destination_shed_id") val destinationShedId: String,
    @SerialName("destination_partition_label") val destinationPartitionLabel: String? = null,
    @SerialName("priority") val priority: String = "",
    @SerialName("category") val category: String = "",
    @SerialName("proof_ref") val proofRef: String? = null,
    /**
     * The raiser's optional note on why the animals are moving. Null (not "") when the operator
     * wrote nothing: the field is omitempty on the wire, and sending an empty string would make
     * "left blank" and "typed then cleared" two different requests for the same intent — which
     * would also change the idempotency fingerprint of an otherwise identical resubmission.
     */
    @SerialName("comment") val comment: String? = null,
    @SerialName("goat_ids") val goatIds: List<String> = emptyList(),
)

@Serializable
data class CountsShiftingEventResponseDto(
    @SerialName("shifting_event_id") val shiftingEventId: String = "",
    @SerialName("idempotent_replay") val idempotentReplay: Boolean = false,
)

// ---------------------------------------------------------------------------
// READ — GET /app/counts/shifting-events/pending-execution
// ---------------------------------------------------------------------------

/**
 * One raised/authorized/evidence-rework movement — a row of the operator's Actions screen.
 *
 * Field names are verbatim from `contracts/openapi/app-api.yaml`
 * (`CountsShiftingPendingExecutionItem` / the backend's `appShiftingPendingExecutionItem`). Every
 * field carries a default so a contract addition never breaks decode of an already-cached Room row.
 *
 * [animalCount] is the FULL size of the movement; [animals] is a bounded preview of at most 5. The
 * client renders the count as truth and the preview for recognition — it never treats the preview
 * length as the movement size (that is [animalsTruncated]'s job).
 */
@Serializable
data class CountsShiftingPendingExecutionItemDto(
    @SerialName("shifting_event_id") val shiftingEventId: String = "",
    @SerialName("event_status") val eventStatus: String = "",
    @SerialName("verification_state") val verificationState: String = "unverified",
    @SerialName("primary_action_key") val primaryActionKey: String = "none",
    @SerialName("priority") val priority: String = "",
    @SerialName("category") val category: String = "",
    @SerialName("source_park_id") val sourceParkId: String? = null,
    @SerialName("source_park_name") val sourceParkName: String? = null,
    @SerialName("source_shed_id") val sourceShedId: String? = null,
    @SerialName("source_shed_name") val sourceShedName: String? = null,
    @SerialName("source_partition_label") val sourcePartitionLabel: String? = null,
    @SerialName("destination_park_id") val destinationParkId: String = "",
    @SerialName("destination_park_name") val destinationParkName: String = "",
    @SerialName("destination_shed_id") val destinationShedId: String = "",
    @SerialName("destination_shed_name") val destinationShedName: String = "",
    @SerialName("destination_partition_label") val destinationPartitionLabel: String? = null,
    @SerialName("approved_by_user_id") val approvedByUserId: String? = null,
    @SerialName("approved_at") val approvedAt: String? = null,
    @SerialName("approved_at_ist") val approvedAtIst: String? = null,
    @SerialName("raised_by_user_id") val raisedByUserId: String = "",
    @SerialName("raised_at") val raisedAt: String = "",
    @SerialName("raised_at_ist") val raisedAtIst: String = "",
    @SerialName("effective_at") val effectiveAt: String = "",
    @SerialName("animal_count") val animalCount: Int = 0,
    @SerialName("animals_truncated") val animalsTruncated: Boolean = false,
    @SerialName("animals") val animals: List<CountsShiftingPendingExecutionAnimalDto> = emptyList(),
    @SerialName("feed_requirement") val feedRequirement: CountsShiftingFeedRequirementDto? = null,
)

@Serializable
data class CountsShiftingFeedRequirementDto(
    val status: String = "blocked",
    @SerialName("blocked_reason") val blockedReason: String? = null,
    val fingerprint: String? = null,
    @SerialName("target_management_stage") val targetManagementStage: String? = null,
    @SerialName("animal_count") val animalCount: Int = 0,
    val items: List<CountsShiftingFeedRequirementItemDto> = emptyList(),
)

@Serializable
data class CountsShiftingFeedRequirementItemDto(
    @SerialName("feed_item_label") val feedItemLabel: String = "",
    @SerialName("quantity_grams") val quantityGrams: String = "0",
)

/** One animal preview on a pending-execution row: enough to find it in a shed. */
@Serializable
data class CountsShiftingPendingExecutionAnimalDto(
    @SerialName("goat_id") val goatId: String = "",
    @SerialName("display_id") val displayId: String = "",
    @SerialName("tag") val tag: String? = null,
)

/**
 * One keyset page of the Pending tab. [nextCursor] is absent on the last page. Keyset, not offset:
 * the queue is drained by several operators at once, so an offset page would skip or repeat rows.
 */
@Serializable
data class CountsShiftingPendingExecutionResponseDto(
    @SerialName("items") val items: List<CountsShiftingPendingExecutionItemDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String? = null,
    @SerialName("status_counts") val statusCounts: CountsShiftingActionStatusCountsDto = CountsShiftingActionStatusCountsDto(),
    @SerialName("previous_dates") val previousDates: List<CountsShiftingPreviousDateDto> = emptyList(),
)

@Serializable data class CountsShiftingActionStatusCountsDto(
    val all: Int = 0, val pending: Int = 0, val authorized: Int = 0,
    val rework: Int = 0, val completed: Int = 0,
)
@Serializable data class CountsShiftingPreviousDateDto(
    val date: String = "", @SerialName("action_count") val actionCount: Int = 0,
)

// ---------------------------------------------------------------------------
// WRITE — POST /app/counts/shifting-events/{id}/complete  and  /cancel
// ---------------------------------------------------------------------------

/**
 * The complete/cancel outcome (`CountsShiftingExecutionResponse` / the backend's
 * `appShiftingExecutionResponse`). THIS is the response for the "Mark done" action that relocates
 * the animals.
 *
 * [idempotentReplay] is true when the result came from a previous identical completion rather than
 * a new relocation — the outbox replays under one stable key on every retry, so this is the normal
 * outcome of a resend, not an error. A movement moved once and confirmed twice returns the original
 * result; it never moves the herd onward.
 */
@Serializable
data class CountsShiftingExecutionResponseDto(
    @SerialName("shifting_event_id") val shiftingEventId: String = "",
    @SerialName("event_status") val eventStatus: String = "",
    @SerialName("destination_park_id") val destinationParkId: String = "",
    @SerialName("destination_shed_id") val destinationShedId: String = "",
    @SerialName("destination_partition_label") val destinationPartitionLabel: String? = null,
    @SerialName("source_park_id") val sourceParkId: String = "",
    @SerialName("source_shed_id") val sourceShedId: String = "",
    @SerialName("source_partition_label") val sourcePartitionLabel: String? = null,
    @SerialName("moved_goat_ids") val movedGoatIds: List<String> = emptyList(),
    @SerialName("moved_count") val movedCount: Int = 0,
    @SerialName("applied_at") val appliedAt: String? = null,
    @SerialName("applied_at_ist") val appliedAtIst: String? = null,
    @SerialName("applied_by") val appliedBy: String? = null,
    @SerialName("canceled_at") val canceledAt: String? = null,
    @SerialName("canceled_at_ist") val canceledAtIst: String? = null,
    @SerialName("canceled_by") val canceledBy: String? = null,
    @SerialName("cancel_reason") val cancelReason: String? = null,
    @SerialName("idempotent_replay") val idempotentReplay: Boolean = false,
)

/** The optional cancel body (`reason` is REQUIRED server-side). */
@Serializable
data class CountsShiftingCancelRequestDto(
    @SerialName("reason") val reason: String? = null,
)

/**
 * The optional complete body. `destination_tag` is only consulted when the destination shed is
 * empty; for an occupied shed the server derives the cohort and a supplied value must agree. The
 * mobile operator flow leaves it null and lets the server derive it.
 */
@Serializable
data class CountsShiftingCompleteRequestDto(
    /**
     * MANDATORY (maintainer decision, 2026-07-26): the proof_artifact id of the operator's video.
     * Approval and completion are independent gates; the second gate applies the move. Verification
     * reviews this video afterward. A blank/absent value is rejected 422 proof_required.
     */
    @SerialName("proof_ref") val proofRef: String,
    @SerialName("feed_packing_proof_ref") val feedPackingProofRef: String? = null,
    @SerialName("feed_given_proof_ref") val feedGivenProofRef: String? = null,
    @SerialName("feed_config_fingerprint") val feedConfigFingerprint: String? = null,
    @SerialName("destination_tag") val destinationTag: String? = null,
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
    // Exactly one of animal_identifier_1 (permanent RFID) or temporary_identifier (provisional tag)
    // is sent; the backend rejects both-or-neither. Nullable so a temporary-tagged newborn omits it.
    @SerialName("animal_identifier_1") val animalIdentifier1: String? = null,
    @SerialName("temporary_identifier") val temporaryIdentifier: String? = null,
    @SerialName("animal_identifier_2") val animalIdentifier2: String? = null,
    @SerialName("species") val species: String,
    @SerialName("park_id") val parkId: String? = null,
    @SerialName("shed_id") val shedId: String? = null,
    @SerialName("breed") val breed: String? = null,
    @SerialName("sex") val sex: String,
    @SerialName("dob") val dob: String,
    /**
     * Optional birth time (`HH:MM`, 24-hour, IST wall clock — docs/decisions/birth-death-workflows.md).
     * Stored as `goats.time_of_birth` and used by the birth follow-up workflow to anchor its
     * time-offset steps; absent means unknown and the backend falls back to 07:00 IST.
     */
    @SerialName("time_of_birth") val timeOfBirth: String? = null,
    @SerialName("entry_date") val entryDate: String,
    /** Mother RFID on first submit; the server resolves and stores the canonical mother goat id. */
    @SerialName("dam_id") val damId: String,
    /** Number born in this delivery. One submit creates this many distinct canonical children. */
    @SerialName("litter_size") val litterSize: Int,
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
 * Result of raising a birth/death approval request. Birth submission creates canonical children
 * immediately and returns them in [children], while approval controls only their count eligibility.
 * Death continues to apply its lifecycle exit only after approval and therefore returns no children.
 */
@Serializable
data class CountsApprovalSubmitResponseDto(
    @SerialName("approval_request_id") val approvalRequestId: String,
    @SerialName("request_type") val requestType: String,
    @SerialName("status") val status: String,
    @SerialName("raised_at") val raisedAt: String,
    @SerialName("idempotent_replay") val idempotentReplay: Boolean,
    @SerialName("children") val children: List<CountsBirthChildResultDto> = emptyList(),
)

/** One canonical child created by a birth submission. */
@Serializable
data class CountsBirthChildResultDto(
    @SerialName("goat_id") val goatId: String,
    @SerialName("temporary_identifier") val temporaryIdentifier: String,
    @SerialName("child_ordinal") val childOrdinal: Int,
)

// ---------------------------------------------------------------------------
// READ — GET /app/counts/goats/temporary-tagged  ("Awaiting RFID" list)
// ---------------------------------------------------------------------------

/**
 * One goat awaiting a permanent RFID: it still carries an active temporary tag. Every field has a
 * default so a later contract addition never breaks decode of an already-cached Room row.
 *
 * [rowVersion] is the goat's optimistic-concurrency token; it is echoed back verbatim in the promote
 * call so a stale in-hand row is rejected instead of clobbering a newer change.
 */
@Serializable
data class TemporaryTaggedGoatDto(
    @SerialName("goat_id") val goatId: String = "",
    @SerialName("display_id") val displayId: String = "",
    @SerialName("temporary_identifier") val temporaryIdentifier: String = "",
    @SerialName("location_display") val locationDisplay: String = "",
    @SerialName("row_version") val rowVersion: Int = 0,
)

/**
 * One keyset page of the "Awaiting RFID" list. [nextCursor] is absent on the last page; it is the
 * last row's display_id. Keyset, not offset: several operators may retag at once.
 */
@Serializable
data class TemporaryTaggedGoatsResponseDto(
    @SerialName("items") val items: List<TemporaryTaggedGoatDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String? = null,
)

// ---------------------------------------------------------------------------
// WRITE — POST /app/counts/goats/{goat_id}/promote-identifier
// ---------------------------------------------------------------------------

/**
 * Assign a permanent RFID to a temporary-tagged goat. The temporary tag to retire is found
 * server-side, so only the permanent RFID and the goat's current [rowVersion] are sent.
 */
@Serializable
data class CountsPromoteIdentifierRequestDto(
    @SerialName("permanent_identifier") val permanentIdentifier: String,
    // Optional second permanent RFID (animal_identifier_2), like the birth flow. Null/absent = only
    // the primary is attached.
    @SerialName("animal_identifier_2") val animalIdentifier2: String? = null,
    @SerialName("row_version") val rowVersion: Int,
)

/**
 * The promote outcome. [idempotentReplay] is true when the result came from a previous identical
 * promotion rather than a new one — the outbox replays under one stable key on every retry, so this
 * is the normal outcome of a resend, not an error.
 */
@Serializable
data class CountsPromoteIdentifierResponseDto(
    @SerialName("goat_id") val goatId: String = "",
    @SerialName("idempotent_replay") val idempotentReplay: Boolean = false,
)
