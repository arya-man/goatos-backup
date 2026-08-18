package sg.mesha.goatos.core.data.sync

import sg.mesha.goatos.core.data.HealthFilters
import sg.mesha.goatos.core.data.HealthRepository

fun healthCaseOpenRefreshHook(repository: HealthRepository): PostSuccessRefreshHook =
    PostSuccessRefreshHook { payloadJson ->
        val payload = syncJson.decodeFromString<HealthCaseOpenPayload>(payloadJson)
        repository.refreshWorkItems(
            HealthFilters(ageBand = payload.ageBand, date = payload.startDate),
        ).getOrThrow()
        if (payload.diseaseKey.isNotBlank()) {
            repository.refreshWorkItems(
                HealthFilters(
                    ageBand = payload.ageBand,
                    date = payload.startDate,
                    diseaseKey = payload.diseaseKey,
                ),
            ).getOrThrow()
        }
    }

fun healthTreatmentCompleteRefreshHook(repository: HealthRepository): PostSuccessRefreshHook =
    PostSuccessRefreshHook { payloadJson ->
        val payload = syncJson.decodeFromString<HealthTreatmentCompletePayload>(payloadJson)
        repository.reconcileSuccessfulTreatmentCompletion(payload.healthSessionId).getOrThrow()
    }

fun healthTreatmentCompleteFailureHook(repository: HealthRepository): PostTerminalFailureHook =
    PostTerminalFailureHook { payloadJson ->
        val payload = syncJson.decodeFromString<HealthTreatmentCompletePayload>(payloadJson)
        repository.reconcileRejectedTreatmentCompletion(payload.healthSessionId).getOrThrow()
    }
