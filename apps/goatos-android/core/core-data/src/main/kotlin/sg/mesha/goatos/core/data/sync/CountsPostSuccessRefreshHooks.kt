package sg.mesha.goatos.core.data.sync

import sg.mesha.goatos.core.data.CountsApprovalRepository
import sg.mesha.goatos.core.data.CountsRepository
import sg.mesha.goatos.core.data.ShiftingPendingRepository

/**
 * Factories for the Counts-family [PostSuccessRefreshHook]s — kept in `core-data` (not the app's DI
 * module) because they decode payloads via the module-internal [syncJson], and derive exactly the
 * cache keys each repository's own refresh/forget methods already use (see
 * [sg.mesha.goatos.core.data.DefaultCountsRepository] /
 * [sg.mesha.goatos.core.data.DefaultCountsApprovalRepository] /
 * [sg.mesha.goatos.core.data.DefaultShiftingPendingRepository]) — the AppModule only wires the
 * repository instance in, it never touches the payload shape.
 *
 * These operations (birth, death, shifting, approval, etc.) affect whole-page caches
 * (herd summary, breakdown, approvals list, shifting pending list) that have no server-truth row
 * to write directly into a Room table. The correct reconcile is "refresh the affected pages".
 */

fun countsShiftingRefreshHook(repository: CountsRepository): PostSuccessRefreshHook =
    PostSuccessRefreshHook { payloadJson ->
        // A shifting write affects the destination catalog (which parks/sheds are available to
        // move into) and the herd summary (counts changed).
        repository.refreshShiftingDestinations().getOrThrow()
        repository.refreshHerdSummary().getOrThrow()
    }

fun countsBirthRefreshHook(repository: CountsRepository): PostSuccessRefreshHook =
    PostSuccessRefreshHook { payloadJson ->
        // A birth adds a new animal to the herd, affecting:
        // - herd summary (total count incremented)
        // - birth breeds vocabulary (new breed may now appear)
        // - breakdown totals (age/stage/sex distribution changed)
        repository.refreshHerdSummary().getOrThrow()
        repository.refreshBirthBreeds().getOrThrow()
        // The breakdown's totals/charts/facets are affected, but individual pages stay cached
        // until the next page load/refresh — a full page-by-page crawl is not needed here.
    }

fun countsDeathRefreshHook(repository: CountsRepository): PostSuccessRefreshHook =
    PostSuccessRefreshHook { payloadJson ->
        // A death removes an animal from the herd, affecting:
        // - herd summary (total count decremented)
        // The breakdown's totals/charts are affected, but individual pages stay cached until
        // the next page load.
        repository.refreshHerdSummary().getOrThrow()
    }

fun countsApprovalApproveRefreshHook(repository: CountsApprovalRepository): PostSuccessRefreshHook =
    PostSuccessRefreshHook { payloadJson ->
        val payload = syncJson.decodeFromString<CountsApprovalDecisionPayload>(payloadJson)
        // Forget the decided request from all cached scopes (pending, approved, rejected)
        // so it never re-appears in the UI as an untappable ghost.
        repository.forgetDecided(payload.requestId)
    }

fun countsApprovalRejectRefreshHook(repository: CountsApprovalRepository): PostSuccessRefreshHook =
    PostSuccessRefreshHook { payloadJson ->
        val payload = syncJson.decodeFromString<CountsApprovalDecisionPayload>(payloadJson)
        // Forget the decided request from all cached scopes so it never re-appears in the UI.
        repository.forgetDecided(payload.requestId)
    }

fun shiftingCompleteRefreshHook(repository: ShiftingPendingRepository): PostSuccessRefreshHook =
    PostSuccessRefreshHook { payloadJson ->
        val payload = syncJson.decodeFromString<ShiftingCompletePayload>(payloadJson)
        // Forget the executed movement from the cached Pending list so it never re-appears
        // in the UI as an untappable ghost waiting to be acted on again.
        repository.forgetExecuted(payload.shiftingEventId)
    }

fun shiftingCancelRefreshHook(repository: ShiftingPendingRepository): PostSuccessRefreshHook =
    PostSuccessRefreshHook { payloadJson ->
        val payload = syncJson.decodeFromString<ShiftingCancelPayload>(payloadJson)
        // Forget the cancelled movement from the cached Pending list so it never re-appears.
        repository.forgetExecuted(payload.shiftingEventId)
    }

fun countsPromoteIdentifierRefreshHook(repository: CountsRepository): PostSuccessRefreshHook =
    PostSuccessRefreshHook { payloadJson ->
        // A permanent RFID assignment may update the animal's identity state (e.g., moving it
        // from a "awaiting_id" to "identified" lifecycle stage), affecting the herd summary.
        repository.refreshHerdSummary().getOrThrow()
    }
