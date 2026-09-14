package sg.mesha.goatos.core.data.sync

import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.data.MarketRepository

private val marketFailureHookJson = Json { ignoreUnknownKeys = true }

fun marketSurveyRecordFailureHook(repository: MarketRepository): PostTerminalFailureHook =
    PostTerminalFailureHook { payloadJson ->
        val payload = runCatching {
            marketFailureHookJson.decodeFromString<MarketSurveyRecordPayload>(payloadJson)
        }.getOrNull()
        repository.refreshDayAndTodayAliasIfCurrent(payload?.businessDate.orEmpty())
    }
