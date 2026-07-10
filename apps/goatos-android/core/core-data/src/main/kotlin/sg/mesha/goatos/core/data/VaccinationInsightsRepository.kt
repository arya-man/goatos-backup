package sg.mesha.goatos.core.data

import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.VaccinationCoverageResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationGapsResponseDto

/**
 * Leadership drill overlays: live vaccination data gaps and per-vaccine coverage.
 * Thin pass-through over [AppApi]; DTO -> overlay rows stays in :app.
 */
interface VaccinationInsightsRepository {
    suspend fun gaps(
        parkId: String? = null,
        limit: Int? = null,
        cursor: String? = null,
    ): VaccinationGapsResponseDto

    suspend fun coverage(
        parkId: String? = null,
        asOf: String? = null,
        dueBefore: String? = null,
        limit: Int? = null,
    ): VaccinationCoverageResponseDto
}

class DefaultVaccinationInsightsRepository(
    private val api: AppApi,
) : VaccinationInsightsRepository {
    override suspend fun gaps(
        parkId: String?,
        limit: Int?,
        cursor: String?,
    ): VaccinationGapsResponseDto = api.getVaccinationGaps(parkId, limit, cursor)

    override suspend fun coverage(
        parkId: String?,
        asOf: String?,
        dueBefore: String?,
        limit: Int?,
    ): VaccinationCoverageResponseDto = api.getVaccinationCoverage(parkId, asOf, dueBefore, limit)
}
