package sg.mesha.goatos.core.data.sync

import sg.mesha.goatos.core.data.WorkflowsRepository

fun workflowActionAnswerFailureHook(repository: WorkflowsRepository): PostTerminalFailureHook =
    PostTerminalFailureHook { payloadJson ->
        val payload = syncJson.decodeFromString<WorkflowActionAnswerPayload>(payloadJson)
        repository.rollbackAction(payload.workflowId, payload.actionId)
    }

fun workflowActionCompleteFailureHook(repository: WorkflowsRepository): PostTerminalFailureHook =
    PostTerminalFailureHook { payloadJson ->
        val payload = syncJson.decodeFromString<WorkflowActionCompletePayload>(payloadJson)
        repository.rollbackAction(payload.workflowId, payload.actionId)
    }
