package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import sg.mesha.goatos.core.network.OwnedModuleDto

/**
 * GET /app/config -> AppConfigResponse.
 * Mobile live-config bundle: nav labels, feature flags, runtime knobs, kill-switches.
 * Supports conditional GET via If-None-Match header (304 means unchanged).
 */
@Serializable
data class AppClientRuntimeConfigDto(
    @SerialName("pageSizeDefault") val pageSizeDefault: Int = 50,
    @SerialName("syncBackoffBaseMs") val syncBackoffBaseMs: Int = 1000,
    @SerialName("syncBackoffMaxMs") val syncBackoffMaxMs: Int = 60000,
    @SerialName("refreshCadenceSec") val refreshCadenceSec: Int = 300,
    @SerialName("cacheTtlSec") val cacheTtlSec: Int = 3600,
    @SerialName("jankSamplingRate") val jankSamplingRate: Float = 0.1f,
)

@Serializable
data class AppCachePolicyDto(
    @SerialName("etag") val etag: String = "",
    @SerialName("inProcessTtlSec") val inProcessTtlSec: Int = 0,
    @SerialName("redisTtlHintSec") val redisTtlHintSec: Int = 0,
    @SerialName("revisionSource") val revisionSource: String = "",
)

@Serializable
data class AppConfigResponseDto(
    @SerialName("source") val source: String = "api",
    @SerialName("revision") val revision: String = "",
    @SerialName("cachePolicy") val cachePolicy: AppCachePolicyDto = AppCachePolicyDto(),
    @SerialName("featureFlags") val featureFlags: Map<String, Boolean> = emptyMap(),
    @SerialName("ownedModules") val ownedModules: List<OwnedModuleDto> = emptyList(),
    @SerialName("clientRuntimeConfig") val clientRuntimeConfig: AppClientRuntimeConfigDto = AppClientRuntimeConfigDto(),
    @SerialName("policyRevision") val policyRevision: String = "",
)
