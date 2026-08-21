package sg.mesha.goatos.core.data.sync

import sg.mesha.goatos.core.data.PcCareRepository

/**
 * PC Care slot-register reconcile (registered per-[sg.mesha.goatos.core.database.outbox.OutboxOpType]
 * at the DI layer like the Milk/Counts/Health hooks — never in a ViewModel): a successful
 * registration re-polls the task's captures so the SERVER's per-slot truth (proof ref, "Captured
 * by X" attribution) lands back in the same Room rows the screens observe. [PcCareRepository.pollTaskOnce]
 * writes through Room and absorbs its own network failures, so this hook cannot fail a drain pass.
 */
fun pcCareSlotRegisterRefreshHook(repository: PcCareRepository): PostSuccessRefreshHook =
    PostSuccessRefreshHook { payloadJson ->
        val payload = syncJson.decodeFromString<PcCareSlotRegisterPayload>(payloadJson)
        repository.pollTaskOnce(payload.taskId)
    }
