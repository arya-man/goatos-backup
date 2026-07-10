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
    @SerialName("page_size_default") val pageSizeDefault: Int = 50,
    @SerialName("sync_backoff_base_ms") val syncBackoffBaseMs: Int = 1000,
    @SerialName("sync_backoff_max_ms") val syncBackoffMaxMs: Int = 60000,
    @SerialName("refresh_cadence_sec") val refreshCadenceSec: Int = 300,
    @SerialName("cache_ttl_sec") val cacheTtlSec: Int = 3600,
    @SerialName("jank_sampling_rate") val jankSamplingRate: Float = 0.1f,
)

@Serializable
data class AppPresentationConfigDto(
    @SerialName("feature_flags") val featureFlags: Map<String, Boolean> = emptyMap(),
    @SerialName("owned_modules") val ownedModules: List<OwnedModuleDto> = emptyList(),
    @SerialName("admin_ui_config") val adminUiConfig: Map<String, String> = emptyMap(),
)

@Serializable
data class AppConfigResponseDto(
    @SerialName("source") val source: String = "api",
    @SerialName("presentation_config") val presentationConfig: AppPresentationConfigDto = AppPresentationConfigDto(),
    @SerialName("client_runtime_config") val clientRuntimeConfig: AppClientRuntimeConfigDto = AppClientRuntimeConfigDto(),
    @SerialName("revision") val revision: Int = 0,
    @SerialName("etag") val etag: String? = null,
    @SerialName("policy_revision") val policyRevision: String? = null,
)
