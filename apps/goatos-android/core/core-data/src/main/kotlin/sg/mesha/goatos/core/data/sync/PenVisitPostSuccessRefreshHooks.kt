package sg.mesha.goatos.core.data.sync

import sg.mesha.goatos.core.data.PenVisitsRepository

/**
 * Pen-visit terminal-failure reconcile (registered per-[sg.mesha.goatos.core.database.outbox.OutboxOpType]
 * at the DI layer like the Health/Workflow hooks — never in a ViewModel): a submit the server
 * definitively refused (`422 invalid_proof` / `proof_required`, a second `409 stale_task`, a
 * cancelled task) leaves the refusal's farm copy on the outbox row's lastError, and re-reading
 * the task puts the server's own chip and `can_submit` back on screen so the card shows why.
 * [PenVisitsRepository.refreshVisit] absorbs its own network failures, so this hook cannot fail
 * a drain pass. The SUCCESS path needs no hook: the engine writes the returned task directly
 * through [PenVisitsRepository.persistServerDetail].
 */
fun penVisitSubmitFailureHook(repository: PenVisitsRepository): PostTerminalFailureHook =
    PostTerminalFailureHook { payloadJson ->
        val payload = syncJson.decodeFromString<PenVisitSubmitPayload>(payloadJson)
        repository.refreshVisit(payload.taskId)
    }
