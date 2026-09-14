package sg.mesha.goatos.core.data.sync

import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.data.MarketRepository

private val marketFailureHookJson = Json { ignoreUnknownKeys = true }

fun marketSurveyRecordFailureHook(repository: MarketRepository): PostTerminalFailureHook =
    PostTerminalFailureHook { payloadJson ->
        // exception:exempt corrupt terminal-failure payload cannot identify a day; refresh falls back to today's market alias.
        val payload = runCatching {
            marketFailureHookJson.decodeFromString<MarketSurveyRecordPayload>(payloadJson)
        }.getOrNull()
        repository.refreshDayAndTodayAliasIfCurrent(payload?.businessDate.orEmpty())
    }
