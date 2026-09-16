package sg.mesha.goatos.core.data.sync

import sg.mesha.goatos.core.data.PenRoutinesRepository

/**
 * Pen-routine terminal-failure reconcile (registered per-[sg.mesha.goatos.core.database.outbox.OutboxOpType]
 * at the DI layer like the pen-visit hook — never in a ViewModel): a presence punch or a submit
 * the server definitively refused (`422 presence_missing` / `proof_count` / `answer_invalid` /
 * `invalid_proof`, a second `409 stale_task`, `404 task_not_found`) leaves the refusal's farm
 * copy on the outbox row's lastError, and re-reading the task puts the server's own chip,
 * `in_pen` and `can_submit` back on screen so the form re-opens beside the reason.
 * [PenRoutinesRepository.refreshTask] absorbs its own network failures, so this hook cannot fail
 * a drain pass. The SUCCESS path needs no hook: the engine writes the returned task directly
 * through [PenRoutinesRepository.persistServerDetail].
 */
fun penRoutinePresenceFailureHook(repository: PenRoutinesRepository): PostTerminalFailureHook =
    PostTerminalFailureHook { payloadJson ->
        val payload = syncJson.decodeFromString<PenRoutinePresencePayload>(payloadJson)
        repository.refreshTask(payload.taskId)
    }

fun penRoutineSubmitFailureHook(repository: PenRoutinesRepository): PostTerminalFailureHook =
    PostTerminalFailureHook { payloadJson ->
        val payload = syncJson.decodeFromString<PenRoutineSubmitPayload>(payloadJson)
        repository.refreshTask(payload.taskId)
    }
