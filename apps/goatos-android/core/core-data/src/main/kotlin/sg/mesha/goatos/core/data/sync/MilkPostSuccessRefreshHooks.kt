package sg.mesha.goatos.core.data.sync

import sg.mesha.goatos.core.data.MilkFeedingRepository
import sg.mesha.goatos.core.data.MilkPreparationRepository

/**
 * Factories for the two Milk [PostSuccessRefreshHook]s — kept in `core-data` (not the app's DI
 * module) because they decode [MilkFeedingSubmitPayload]/[MilkPreparationSubmitPayload] via the
 * module-internal [syncJson], and derive exactly the cache key each repository's own `refresh`
 * already uses (see [sg.mesha.goatos.core.data.DefaultMilkFeedingRepository] /
 * [sg.mesha.goatos.core.data.DefaultMilkPreparationRepository]) — the AppModule only wires the
 * repository instance in, it never touches the payload shape.
 */
fun milkFeedingSubmitRefreshHook(repository: MilkFeedingRepository): PostSuccessRefreshHook =
    PostSuccessRefreshHook { payloadJson ->
        val payload = syncJson.decodeFromString<MilkFeedingSubmitPayload>(payloadJson)
        repository.refresh(
            feedingDate = payload.feedingDate,
            parkId = payload.parkId,
            sessionNo = payload.sessionNo,
        ).getOrThrow()
    }

fun milkPreparationSubmitRefreshHook(repository: MilkPreparationRepository): PostSuccessRefreshHook =
    PostSuccessRefreshHook { payloadJson ->
        val payload = syncJson.decodeFromString<MilkPreparationSubmitPayload>(payloadJson)
        repository.refresh(preparationDate = payload.preparationDate).getOrThrow()
    }
